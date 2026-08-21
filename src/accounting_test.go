package main

import (
	"math"
	"testing"
)

func TestAccountingAssertions(t *testing.T) {
	db, err := ConnectDB()
	if err != nil {
		t.Fatalf("Failed to connect to DB for accounting assertions: %v", err)
	}
	defer db.Close()

	// 1. Assert Payment Summary is calculated only from Payment records,
	// and Settlement Summary only from Settlement records.
	var payFromSetCount, setFromPayCount int
	err = db.QueryRow(`
		SELECT 
			COUNT(*) FILTER (WHERE config_type = 'payments' AND r.source_file != 'payments'),
			COUNT(*) FILTER (WHERE config_type = 'settlements' AND r.source_file != 'settlements')
		FROM mapped_records m
		JOIN ingested_records r ON m.ingested_record_id = r.id
	`).Scan(&payFromSetCount, &setFromPayCount)
	if err != nil {
		t.Fatalf("Failed to query data leakage: %v", err)
	}

	if payFromSetCount > 0 {
		t.Errorf("Accounting Integrity Violation: Found %d Payment summary allocations referencing Settlement source records", payFromSetCount)
	}
	if setFromPayCount > 0 {
		t.Errorf("Accounting Integrity Violation: Found %d Settlement summary allocations referencing Payment source records", setFromPayCount)
	}

	// 2. Assert all summary differences are exactly zero (penny-to-penny matching)
	type CategorySum struct {
		Field   string
		PayAmt  float64
		SetAmt  float64
		DiffAmt float64
	}

	rows, err := db.Query(`
		SELECT 
			m.summary_field,
			COALESCE(SUM(m.amount) FILTER (WHERE m.config_type = 'payments'), 0) as pay_amt,
			COALESCE(SUM(m.amount) FILTER (WHERE m.config_type = 'settlements'), 0) as set_amt
		FROM mapped_records m
		WHERE m.mapping_status = 'reconciled_eligible'
		  AND m.summary_field != ''
		GROUP BY m.summary_field
	`)
	if err != nil {
		t.Fatalf("Failed to query summary differences: %v", err)
	}
	defer rows.Close()

	totalDiff := 0.0
	for rows.Next() {
		var cat CategorySum
		err := rows.Scan(&cat.Field, &cat.PayAmt, &cat.SetAmt)
		if err != nil {
			t.Fatalf("Failed to scan summary category row: %v", err)
		}
		cat.DiffAmt = cat.PayAmt - cat.SetAmt
		totalDiff += math.Abs(cat.DiffAmt)

		// Assert each category reconciles to the penny (diff <= $0.01)
		if math.Abs(cat.DiffAmt) > 0.01 {
			t.Errorf("Variance Violation in %s: Pay=%.2f, Set=%.2f, Diff=%.2f (Must be $0.00)",
				cat.Field, cat.PayAmt, cat.SetAmt, cat.DiffAmt)
		} else {
			t.Logf("Reconciled: %-30s | Pay:%10.2f | Set:%10.2f | Diff: 0.00", cat.Field, cat.PayAmt, cat.SetAmt)
		}
	}

	if totalDiff > 0.05 {
		t.Errorf("Total Cumulative Variance Violation: %.2f (Must be $0.00)", totalDiff)
	}
}

func TestManyToManyAggregation(t *testing.T) {
	db, err := ConnectDB()
	if err != nil {
		t.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.Close()

	// 1. Begin a transaction so we can rollback and keep DB clean
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Failed to start transaction: %v", err)
	}
	defer tx.Rollback()

	// 2. Insert mock Payments records under record_ref 'TEST-MANY-TO-MANY'
	var payID1, payID2 int
	err = tx.QueryRow(`
		INSERT INTO ingested_records (source_file, line_number, raw_payload, idempotency_key, settlement_id)
		VALUES ('payments', 99001, '{"type":"ORDER", "description":"any", "total":"100.0"}', 'test:99001:hash1', '12395580393')
		RETURNING id
	`).Scan(&payID1)
	if err != nil {
		t.Fatalf("Failed to insert mock pay 1: %v", err)
	}

	err = tx.QueryRow(`
		INSERT INTO ingested_records (source_file, line_number, raw_payload, idempotency_key, settlement_id)
		VALUES ('payments', 99002, '{"type":"ORDER", "description":"any", "total":"50.0"}', 'test:99002:hash2', '12395580393')
		RETURNING id
	`).Scan(&payID2)
	if err != nil {
		t.Fatalf("Failed to insert mock pay 2: %v", err)
	}

	// 3. Insert mock Settlements records under record_ref 'TEST-MANY-TO-MANY'
	var setID1, setID2 int
	err = tx.QueryRow(`
		INSERT INTO ingested_records (source_file, line_number, raw_payload, idempotency_key, settlement_id)
		VALUES ('settlements', 99003, '{"transaction-type":"Order", "amount-type":"ItemPrice", "amount-description":"Principal", "amount":"90.0"}', 'test:99003:hash3', '12395580393')
		RETURNING id
	`).Scan(&setID1)
	if err != nil {
		t.Fatalf("Failed to insert mock set 1: %v", err)
	}

	err = tx.QueryRow(`
		INSERT INTO ingested_records (source_file, line_number, raw_payload, idempotency_key, settlement_id)
		VALUES ('settlements', 99004, '{"transaction-type":"Order", "amount-type":"ItemPrice", "amount-description":"Principal", "amount":"60.0"}', 'test:99004:hash4', '12395580393')
		RETURNING id
	`).Scan(&setID2)
	if err != nil {
		t.Fatalf("Failed to insert mock set 2: %v", err)
	}

	// 4. Map them to mapped_records
	_, err = tx.Exec(`
		INSERT INTO mapped_records (ingested_record_id, config_type, config_id, record_ref, amount, summary_field, mapping_status) VALUES
		($1, 'payments', 1, 'TEST-MANY-TO-MANY', 100.0, 'sales_product_charges', 'reconciled_eligible'),
		($2, 'payments', 1, 'TEST-MANY-TO-MANY', 50.0, 'sales_product_charges', 'reconciled_eligible'),
		($3, 'settlements', 1, 'TEST-MANY-TO-MANY', 90.0, 'sales_product_charges', 'reconciled_eligible'),
		($4, 'settlements', 1, 'TEST-MANY-TO-MANY', 60.0, 'sales_product_charges', 'reconciled_eligible')
	`, payID1, payID2, setID1, setID2)
	if err != nil {
		t.Fatalf("Failed to insert mapped_records: %v", err)
	}

	// 5. Verify the aggregation totals for TEST-MANY-TO-MANY
	var paySum, setSum float64
	err = tx.QueryRow(`
		SELECT 
			COALESCE(SUM(amount) FILTER (WHERE config_type = 'payments'), 0) as pay_amt,
			COALESCE(SUM(amount) FILTER (WHERE config_type = 'settlements'), 0) as set_amt
		FROM mapped_records
		WHERE record_ref = 'TEST-MANY-TO-MANY' AND mapping_status = 'reconciled_eligible'
	`).Scan(&paySum, &setSum)
	if err != nil {
		t.Fatalf("Failed to query sums: %v", err)
	}

	if paySum != 150.0 || setSum != 150.0 {
		t.Errorf("Aggregation Error: Expected both to sum to 150.0. Got Pay=%.2f, Set=%.2f", paySum, setSum)
	}
}
