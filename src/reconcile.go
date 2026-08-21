package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type PayConfig struct {
	ID       int
	TxType   string
	Desc     string
	AmtField string
	RecRef   string
	PosField string
	NegField string
}

type SetConfig struct {
	ID       int
	TxType   string
	AmtType  string
	AmtDesc  string
	RecRef   string
	PosField string
	NegField string
}

// MapAllRecords performs the mapping engine logic on ingested_records and writes to mapped_records.
func MapAllRecords(db *sql.DB, targetSettlementID string) (int, int, error) {
	// 1. Load Configurations
	payConfigs, err := loadPayConfigs(db)
	if err != nil {
		return 0, 0, fmt.Errorf("error loading pay configs: %w", err)
	}

	setConfigs, err := loadSetConfigs(db)
	if err != nil {
		return 0, 0, fmt.Errorf("error loading set configs: %w", err)
	}

	// Truncate existing mapped_records
	_, err = db.Exec("TRUNCATE TABLE mapped_records RESTART IDENTITY")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to truncate mapped_records: %w", err)
	}

	// 2. Select raw records
	rows, err := db.Query(`
		SELECT id, source_file, line_number, settlement_id, transaction_status, raw_payload
		FROM ingested_records
		WHERE settlement_id = $1
		ORDER BY source_file, line_number
	`, targetSettlementID)
	if err != nil {
		return 0, 0, fmt.Errorf("error querying ingested_records: %w", err)
	}
	defer rows.Close()

	// Prepared insert statement for mapped_records
	insertStmt, err := db.Prepare(`
		INSERT INTO mapped_records (
			ingested_record_id, config_type, config_id, record_ref, amount, summary_field, mapping_status
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to prepare mapped_records insert statement: %w", err)
	}
	defer insertStmt.Close()

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	mappedCount := 0
	deferredCount := 0

	for rows.Next() {
		var rID int
		var sourceFile, status, settlementID string
		var lineNum int
		var rawPayloadBytes []byte

		err := rows.Scan(&rID, &sourceFile, &lineNum, &settlementID, &status, &rawPayloadBytes)
		if err != nil {
			return 0, 0, fmt.Errorf("error scanning raw record: %w", err)
		}

		var payload map[string]string
		err = json.Unmarshal(rawPayloadBytes, &payload)
		if err != nil {
			return 0, 0, fmt.Errorf("error parsing raw payload JSON: %w", err)
		}

		if sourceFile == "payments" {
			// Check if Deferred
			if status == "Deferred" {
				// Record trace but mark as excluded
				_, err = tx.Stmt(insertStmt).Exec(rID, "payments", nil, "", 0.0, "", "excluded_deferred")
				if err != nil {
					return 0, 0, fmt.Errorf("error inserting deferred payments trace: %w", err)
				}
				deferredCount++
				continue
			}

			rowType := Normalize(payload["type"])
			rowDesc := Normalize(payload["description"])

			// Date parsing
			dateStr := payload["Transaction Release Date"]
			if dateStr == "" {
				dateStr = payload["date/time"]
			}
			rowDate, err := ParsePaymentsDate(dateStr)
			if err != nil {
				// Fallback to empty date if parsing fails (should not fail for AU data)
				rowDate = time.Time{}
			}

			// Precedence rules matching
			var specificMatches []PayConfig
			for _, cfg := range payConfigs {
				if cfg.TxType == "" {
					continue // Skip fallback rules in the first pass
				}
				if MatchRulePayments(rowType, rowDesc, cfg.TxType, cfg.Desc) {
					specificMatches = append(specificMatches, cfg)
				}
			}

			var appliedConfigs []PayConfig
			if len(specificMatches) > 0 {
				appliedConfigs = specificMatches
			} else {
				// Fallback rules
				for _, cfg := range payConfigs {
					if cfg.TxType != "" {
						continue
					}
					if MatchRulePayments(rowType, rowDesc, cfg.TxType, cfg.Desc) {
						appliedConfigs = append(appliedConfigs, cfg)
					}
				}
			}

			for _, cfg := range appliedConfigs {
				amtVal := getPaymentsAmount(payload, cfg.AmtField)
				if amtVal == 0.0 {
					continue
				}

				targetField := cfg.PosField
				if amtVal < 0 {
					targetField = cfg.NegField
				}

				// If target is empty, we don't route to summary, but we still write to audit mapped_records
				recRef := evaluatePaymentsRef(payload, cfg.RecRef, rowDate)

				_, err = tx.Stmt(insertStmt).Exec(rID, "payments", cfg.ID, recRef, amtVal, targetField, "reconciled_eligible")
				if err != nil {
					return 0, 0, fmt.Errorf("error inserting payments mapped record: %w", err)
				}
				mappedCount++
			}

		} else if sourceFile == "settlements" {
			// Skip metadata/header row (empty transaction-type)
			if payload["transaction-type"] == "" {
				continue
			}

			rowType := Normalize(payload["transaction-type"])
			rowAmtType := Normalize(payload["amount-type"])
			rowAmtDesc := Normalize(payload["amount-description"])

			rowDate, _ := ParseSettlementsDate(payload["posted-date-time"])
			amtVal, _ := strconv.ParseFloat(strings.ReplaceAll(payload["amount"], ",", ""), 64)
			if amtVal == 0.0 {
				continue
			}

			// Check matches using two-pass precedence rules
			var specificMatches []SetConfig
			for _, cfg := range setConfigs {
				if cfg.TxType == "" {
					continue // Skip fallback rules in the first pass
				}
				if MatchRuleSettlements(rowType, rowAmtType, rowAmtDesc, cfg.TxType, cfg.AmtType, cfg.AmtDesc) {
					specificMatches = append(specificMatches, cfg)
				}
			}

			var appliedConfigs []SetConfig
			if len(specificMatches) > 0 {
				appliedConfigs = specificMatches
			} else {
				// Fallback rules (empty transaction-type in config)
				for _, cfg := range setConfigs {
					if cfg.TxType != "" {
						continue
					}
					if MatchRuleSettlements(rowType, rowAmtType, rowAmtDesc, cfg.TxType, cfg.AmtType, cfg.AmtDesc) {
						appliedConfigs = append(appliedConfigs, cfg)
					}
				}
			}

			if len(appliedConfigs) > 0 {
				matchedCfg := &appliedConfigs[0]
				targetField := matchedCfg.PosField
				if amtVal < 0 {
					targetField = matchedCfg.NegField
				}

				recRef := evaluateSettlementsRef(payload, matchedCfg.RecRef, rowDate)

				_, err = tx.Stmt(insertStmt).Exec(rID, "settlements", matchedCfg.ID, recRef, amtVal, targetField, "reconciled_eligible")
				if err != nil {
					return 0, 0, fmt.Errorf("error inserting settlements mapped record: %w", err)
				}
				mappedCount++
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to commit mapped_records: %w", err)
	}

	return mappedCount, deferredCount, nil
}

func loadPayConfigs(db *sql.DB) ([]PayConfig, error) {
	rows, err := db.Query(`
		SELECT id, transaction_type, description, amount_field, record_ref,
		       to_summary_field_when_positive_amount, to_summary_field_when_negative_amount
		FROM payment_configs
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []PayConfig
	for rows.Next() {
		var c PayConfig
		err := rows.Scan(&c.ID, &c.TxType, &c.Desc, &c.AmtField, &c.RecRef, &c.PosField, &c.NegField)
		if err != nil {
			return nil, err
		}
		c.TxType = Normalize(c.TxType)
		c.Desc = Normalize(c.Desc)
		list = append(list, c)
	}
	return list, nil
}

func loadSetConfigs(db *sql.DB) ([]SetConfig, error) {
	rows, err := db.Query(`
		SELECT id, transaction_type, amount_type, amount_description, record_ref,
		       to_summary_field_when_positive_amount, to_summary_field_when_negative_amount
		FROM settlement_configs
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []SetConfig
	for rows.Next() {
		var c SetConfig
		err := rows.Scan(&c.ID, &c.TxType, &c.AmtType, &c.AmtDesc, &c.RecRef, &c.PosField, &c.NegField)
		if err != nil {
			return nil, err
		}
		c.TxType = Normalize(c.TxType)
		c.AmtType = Normalize(c.AmtType)
		c.AmtDesc = Normalize(c.AmtDesc)
		list = append(list, c)
	}
	return list, nil
}

// getPaymentsAmount extracts numeric amount from JSON payload matching config amount field
func getPaymentsAmount(payload map[string]string, amountField string) float64 {
	normField := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(amountField), "_", ""), " ", "")
	if normField == "fbafees" {
		normField = "fulfilmentbyamazonfees"
	}

	for k, v := range payload {
		normK := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(k), "_", ""), " ", ""), "/", "")
		if normK == normField {
			amt, _ := strconv.ParseFloat(strings.ReplaceAll(v, ",", ""), 64)
			return amt
		}
	}
	return 0.0
}

// evaluatePaymentsRef substitutes record_ref template values
func evaluatePaymentsRef(row map[string]string, template string, rowDate time.Time) string {
	parts := strings.Split(template, "+")
	var resParts []string
	for _, p := range parts {
		pStrip := strings.TrimSpace(p)
		switch pStrip {
		case "txn_ref":
			resParts = append(resParts, row["order ID"])
		case "sku":
			resParts = append(resParts, row["sku"])
		case "date":
			if !rowDate.IsZero() {
				resParts = append(resParts, rowDate.Format("2006-01-02"))
			} else {
				resParts = append(resParts, "")
			}
		case "settlement_id":
			resParts = append(resParts, row["settlement ID"])
		case "description":
			resParts = append(resParts, row["description"])
		default:
			resParts = append(resParts, pStrip)
		}
	}
	return strings.Join(resParts, "+")
}

// evaluateSettlementsRef substitutes record_ref template values for Settlements
func evaluateSettlementsRef(row map[string]string, template string, rowDate time.Time) string {
	parts := strings.Split(template, "+")
	var resParts []string
	for _, p := range parts {
		pStrip := strings.TrimSpace(p)
		switch pStrip {
		case "txn_ref":
			resParts = append(resParts, row["order-id"])
		case "sku":
			resParts = append(resParts, row["sku"])
		case "date":
			if !rowDate.IsZero() {
				resParts = append(resParts, rowDate.Format("2006-01-02"))
			} else {
				resParts = append(resParts, "")
			}
		case "settlement_id":
			resParts = append(resParts, row["settlement-id"])
		case "merchant_order_id":
			resParts = append(resParts, row["merchant-order-id"])
		case "shipment_id":
			resParts = append(resParts, row["shipment-id"])
		case "record_type":
			resParts = append(resParts, Normalize(row["transaction-type"]))
		default:
			resParts = append(resParts, pStrip)
		}
	}
	return strings.Join(resParts, "+")
}
