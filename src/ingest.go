package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// hashPayload computes a SHA-256 hash of the JSON payload.
func hashPayload(payload []byte) string {
	h := sha256.New()
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

// IngestPayments reads payments CSV and inserts it into database.
func IngestPayments(db *sql.DB, filePath string) (int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("unable to open payments file: %w", err)
	}
	defer file.Close()

	// Read lines to skip metadata
	// The first 9 lines are metadata. Line 10 is the header.
	// We'll read line-by-line using a scanner or custom reader.
	// Since CSV reader expects headers at the very start, we must skip the first 9 lines.
	var lineNum int
	for lineNum = 1; lineNum <= 9; lineNum++ {
		var discardLine []byte
		// We'll read until newline
		_, err := fmt.Fscanln(file, &discardLine)
		if err != nil && err != io.EOF {
			// If Fscanln fails due to spacing, read byte-by-byte
			// Let's use a simpler byte-by-byte reader for skipping
			break
		}
	}

	// Reset file pointer and do robust skip
	_, _ = file.Seek(0, io.SeekStart)
	buf := make([]byte, 1)
	skippedLines := 0
	for skippedLines < 9 {
		_, err := file.Read(buf)
		if err != nil {
			return 0, fmt.Errorf("error skipping metadata: %w", err)
		}
		if buf[0] == '\n' {
			skippedLines++
		}
	}

	// Now reader is at the start of line 10 (the CSV header)
	csvReader := csvNewReader(file, ',')
	header, err := csvReader.Read()
	if err != nil {
		return 0, fmt.Errorf("error reading payments header: %w", err)
	}

	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[h] = i
	}

	// Check required columns exist
	reqCols := []string{"settlement ID", "Transaction status"}
	for _, col := range reqCols {
		if _, ok := colIdx[col]; !ok {
			return 0, fmt.Errorf("missing required column in payments file: %s", col)
		}
	}

	// Prepare insert statement
	stmt, err := db.Prepare(`
		INSERT INTO ingested_records (
			source_file, line_number, settlement_id, transaction_status, raw_payload, idempotency_key
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (idempotency_key) DO NOTHING
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare payments insert statement: %w", err)
	}
	defer stmt.Close()

	fileLine := 10 // Line 10 was the header
	insertedCount := 0

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("error reading payments record at line %d: %w", fileLine+1, err)
		}
		fileLine++

		// Map to a JSON dictionary
		payloadMap := make(map[string]string)
		for i, val := range record {
			if i < len(header) {
				payloadMap[header[i]] = val
			}
		}

		payloadBytes, err := json.Marshal(payloadMap)
		if err != nil {
			return 0, fmt.Errorf("failed to marshal payments record: %w", err)
		}

		settlementID := payloadMap["settlement ID"]
		status := payloadMap["Transaction status"]
		payloadHash := hashPayload(payloadBytes)
		idempotencyKey := fmt.Sprintf("payments:%d:%s", fileLine, payloadHash)

		res, err := stmt.Exec("payments", fileLine, settlementID, status, payloadBytes, idempotencyKey)
		if err != nil {
			return 0, fmt.Errorf("failed to insert payments record: %w", err)
		}

		affected, _ := res.RowsAffected()
		if affected > 0 {
			insertedCount++
		}
	}

	return insertedCount, nil
}

// IngestSettlements reads settlements TSV and inserts it into database.
func IngestSettlements(db *sql.DB, filePath string) (int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("unable to open settlements file: %w", err)
	}
	defer file.Close()

	// Settlements file starts directly with the header row
	tsvReader := csvNewReader(file, '\t')
	header, err := tsvReader.Read()
	if err != nil {
		return 0, fmt.Errorf("error reading settlements header: %w", err)
	}

	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[h] = i
	}

	// Check required columns exist
	if _, ok := colIdx["settlement-id"]; !ok {
		return 0, fmt.Errorf("missing required column in settlements file: settlement-id")
	}

	stmt, err := db.Prepare(`
		INSERT INTO ingested_records (
			source_file, line_number, settlement_id, transaction_status, raw_payload, idempotency_key
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (idempotency_key) DO NOTHING
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare settlements insert statement: %w", err)
	}
	defer stmt.Close()

	fileLine := 1 // Line 1 was the header
	insertedCount := 0

	for {
		record, err := tsvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("error reading settlements record at line %d: %w", fileLine+1, err)
		}
		fileLine++

		payloadMap := make(map[string]string)
		for i, val := range record {
			if i < len(header) {
				payloadMap[header[i]] = val
			}
		}

		payloadBytes, err := json.Marshal(payloadMap)
		if err != nil {
			return 0, fmt.Errorf("failed to marshal settlements record: %w", err)
		}

		settlementID := payloadMap["settlement-id"]
		payloadHash := hashPayload(payloadBytes)
		idempotencyKey := fmt.Sprintf("settlements:%d:%s", fileLine, payloadHash)

		res, err := stmt.Exec("settlements", fileLine, settlementID, "", payloadBytes, idempotencyKey)
		if err != nil {
			return 0, fmt.Errorf("failed to insert settlements record: %w", err)
		}

		affected, _ := res.RowsAffected()
		if affected > 0 {
			insertedCount++
		}
	}

	return insertedCount, nil
}

// csvNewReader creates a CSV reader with custom separator and flexible column counts
func csvNewReader(r io.Reader, comma rune) *csv.Reader {
	reader := csv.NewReader(r)
	reader.Comma = comma
	reader.FieldsPerRecord = -1 // Allow flexible number of columns per row
	reader.LazyQuotes = true    // Allow lazy quotes in CSV parsing
	return reader
}
