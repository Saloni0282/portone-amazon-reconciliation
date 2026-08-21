package main

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
)

// LoadPaymentConfigs reads payment config CSV and inserts it into database.
func LoadPaymentConfigs(db *sql.DB, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("unable to open payments config: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	// Read header
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("error reading payments config header: %w", err)
	}

	// Truncate existing configs
	_, err = db.Exec("TRUNCATE TABLE payment_configs RESTART IDENTITY CASCADE")
	if err != nil {
		return fmt.Errorf("failed to truncate payment_configs: %w", err)
	}

	// Prepare insert statement
	stmt, err := db.Prepare(`
		INSERT INTO payment_configs (
			transaction_type, description, amount_field, record_ref,
			to_summary_field_when_positive_amount, to_summary_field_when_negative_amount
		) VALUES ($1, $2, $3, $4, $5, $6)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare payment_config insert: %w", err)
	}
	defer stmt.Close()

	// Map headers to column indices
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[h] = i
	}

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading payment config row: %w", err)
		}

		txType := record[colIdx["transaction_type"]]
		desc := record[colIdx["description"]]
		amtField := record[colIdx["amount_field"]]
		recRef := record[colIdx["record_ref"]]
		posField := record[colIdx["to_summary_field_when_positive_amount"]]
		negField := record[colIdx["to_summary_field_when_negative_amount"]]

		_, err = stmt.Exec(txType, desc, amtField, recRef, posField, negField)
		if err != nil {
			return fmt.Errorf("failed to insert payment config row: %w", err)
		}
	}

	return nil
}

// LoadSettlementConfigs reads settlement config CSV and inserts it into database.
func LoadSettlementConfigs(db *sql.DB, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("unable to open settlements config: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	// Read header
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("error reading settlements config header: %w", err)
	}

	// Truncate existing configs
	_, err = db.Exec("TRUNCATE TABLE settlement_configs RESTART IDENTITY CASCADE")
	if err != nil {
		return fmt.Errorf("failed to truncate settlement_configs: %w", err)
	}

	// Prepare insert statement
	stmt, err := db.Prepare(`
		INSERT INTO settlement_configs (
			transaction_type, amount_type, amount_description, record_ref,
			to_summary_field_when_positive_amount, to_summary_field_when_negative_amount
		) VALUES ($1, $2, $3, $4, $5, $6)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare settlement_config insert: %w", err)
	}
	defer stmt.Close()

	// Map headers to column indices
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[h] = i
	}

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading settlement config row: %w", err)
		}

		txType := record[colIdx["transaction_type"]]
		amtType := record[colIdx["amount_type"]]
		amtDesc := record[colIdx["amount_description"]]
		recRef := record[colIdx["record_ref"]]
		posField := record[colIdx["to_summary_field_when_positive_amount"]]
		negField := record[colIdx["to_summary_field_when_negative_amount"]]

		_, err = stmt.Exec(txType, amtType, amtDesc, recRef, posField, negField)
		if err != nil {
			return fmt.Errorf("failed to insert settlement config row: %w", err)
		}
	}

	return nil
}
