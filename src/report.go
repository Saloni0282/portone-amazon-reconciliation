package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// SummaryCategory maps a summary field name to its Excel row in Summary sheet
// and its 1-based column indices in Consolidated Data sheet.
type SummaryCategory struct {
	Label      string
	Row        int
	ColPayIdx  int // 1-based index in Consolidated Data
	ColSetIdx  int // 1-based index in Consolidated Data
	FieldNames []string
}

var categories = []SummaryCategory{
	{"Product Charges", 5, 26, 71, []string{"sales_product_charges"}},
	{"Tax", 6, 27, 72, []string{"sales_tax"}},
	{"Shipping", 7, 28, 73, []string{"sales_shipping"}},
	{"Amazon fees", 8, 29, 74, []string{"sales_amazon_fees"}},
	{"Inventory Reimbursements", 9, 30, 75, []string{"sales_inventory_reimbursements"}},
	{"Cross-account Debt Adjustment", 10, 31, 76, []string{"beginning_balance"}},
	{"Other", 11, 32, 77, []string{"sales_other"}},
	{"FBA Fees", 12, 33, 78, []string{"sales_fba_fees"}},
	{"Micro Deposit (Failed)", 13, 34, 79, []string{"total_micro_deposits_failed_amt"}},

	{"Refund expenses", 16, 35, 80, []string{"refunded_expenses"}},
	{"Refunded sales", 17, 36, 81, []string{"refunded_sales"}},

	{"Promo rebates", 20, 37, 82, []string{"expenses_promotional_rebates"}},
	{"FBA fees", 21, 38, 83, []string{"expenses_fba_fees"}},
	{"Cost of Advertising", 22, 39, 84, []string{"expenses_cost_of_advertising"}},
	{"Shipping Charges", 23, 40, 85, []string{"expenses_shipping_charges"}},
	{"Amazon fees", 24, 41, 86, []string{"expenses_amazon_fees"}},
	{"Reversed Reimbursements", 25, 42, 87, []string{"expenses_reversed_reimbursements"}},
	{"Cross-account Debt Adjustment", 26, 43, 88, []string{"current_reserve_amount", "amazon_carried_forward"}},
	{"Other", 27, 44, 89, []string{"expenses_other"}},
	{"Micro Deposit", 28, 45, 90, []string{"bank_account_transfer_round_off", "total_micro_deposits_amt"}},

	{"Paid To Amazon", 32, 46, 91, []string{"paid_to_amazon", "total_paid_to_amazon_amt"}},
}

// GenerateReport pulls data from mapped_records and generates the final Excel report.
func GenerateReport(db *sql.DB, targetSettlementID string, outputPath string) error {
	f := excelize.NewFile()
	defer f.Close()

	// 1. Build Consolidated Data Sheet
	constDataSheet := "Consolidated Data"
	_, err := f.NewSheet(constDataSheet)
	if err != nil {
		return fmt.Errorf("failed to create Consolidated Data sheet: %w", err)
	}

	// Set grid lines visible
	showGridLinesConst := true
	_ = f.SetSheetView(constDataSheet, -1, &excelize.ViewOptions{
		ShowGridLines: &showGridLinesConst,
	})

	// Write Headers for Consolidated Data
	// Row 9: Categories, Row 10: Column names
	writeConsolidatedHeaders(f, constDataSheet)

	// Fetch all unique raw rows that are reconciled
	rawRowsQuery, err := db.Query(`
		SELECT 
			r.id,
			r.source_file,
			r.line_number,
			r.raw_payload,
			m.record_ref
		FROM ingested_records r
		JOIN mapped_records m ON m.ingested_record_id = r.id
		WHERE m.mapping_status = 'reconciled_eligible'
		GROUP BY r.id, r.source_file, r.line_number, r.raw_payload, m.record_ref
	`)
	if err != nil {
		return fmt.Errorf("error querying raw rows: %w", err)
	}
	defer rawRowsQuery.Close()

	type RawRow struct {
		ID         int
		SourceFile string
		LineNumber int
		Payload    []byte
		RecordRef  string
	}

	var rawRows []RawRow
	groups := make(map[string][]RawRow)
	var recordRefs []string

	for rawRowsQuery.Next() {
		var r RawRow
		if err := rawRowsQuery.Scan(&r.ID, &r.SourceFile, &r.LineNumber, &r.Payload, &r.RecordRef); err == nil {
			rawRows = append(rawRows, r)
			if len(groups[r.RecordRef]) == 0 {
				recordRefs = append(recordRefs, r.RecordRef)
			}
			groups[r.RecordRef] = append(groups[r.RecordRef], r)
		}
	}

	// Fetch all mapped allocations in memory
	allocRows, err := db.Query(`
		SELECT ingested_record_id, summary_field, amount
		FROM mapped_records
		WHERE mapping_status = 'reconciled_eligible'
	`)
	if err != nil {
		return fmt.Errorf("error querying mapped allocations: %w", err)
	}
	defer allocRows.Close()

	type Alloc struct {
		Field  string
		Amount float64
	}
	allocMap := make(map[int][]Alloc)
	for allocRows.Next() {
		var recID int
		var a Alloc
		if err := allocRows.Scan(&recID, &a.Field, &a.Amount); err == nil {
			allocMap[recID] = append(allocMap[recID], a)
		}
	}

	// Helper to calculate priority
	getPriority := func(rows []RawRow) int {
		payCnt, setCnt := 0, 0
		for _, r := range rows {
			if r.SourceFile == "payments" {
				payCnt++
			} else if r.SourceFile == "settlements" {
				setCnt++
			}
		}
		if payCnt > 0 && setCnt > 0 {
			return 1 // Match
		}
		if payCnt > 0 {
			return 2 // Payment-Only
		}
		return 3 // Settlement-Only
	}

	// Sort recordRefs by priority, then alphabetically
	sort.Slice(recordRefs, func(i, j int) bool {
		refI := recordRefs[i]
		refJ := recordRefs[j]
		priI := getPriority(groups[refI])
		priJ := getPriority(groups[refJ])
		if priI != priJ {
			return priI < priJ
		}
		return refI < refJ
	})

	startRow := 11
	currentRow := startRow

	// Style definitions
	borderStyle, _ := f.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{Type: "left", Color: "D9D9D9", Style: 1},
			{Type: "right", Color: "D9D9D9", Style: 1},
			{Type: "top", Color: "D9D9D9", Style: 1},
			{Type: "bottom", Color: "D9D9D9", Style: 1},
		},
	})

	for _, ref := range recordRefs {
		refRows := groups[ref]

		// Separate payments and settlements raw rows to preserve order
		var payRows []RawRow
		var setRows []RawRow
		for _, r := range refRows {
			if r.SourceFile == "payments" {
				payRows = append(payRows, r)
			} else if r.SourceFile == "settlements" {
				setRows = append(setRows, r)
			}
		}

		// Calculate overall status for the record_ref
		statusVal := "Reconciled"
		payBucketsOverall := make(map[string]float64)
		setBucketsOverall := make(map[string]float64)

		for _, r := range payRows {
			for _, a := range allocMap[r.ID] {
				payBucketsOverall[a.Field] += a.Amount
			}
		}
		for _, r := range setRows {
			for _, a := range allocMap[r.ID] {
				setBucketsOverall[a.Field] += a.Amount
			}
		}

		if len(payRows) > 0 && len(setRows) == 0 {
			statusVal = "Unreconciled Payment"
		} else if len(setRows) > 0 && len(payRows) == 0 {
			statusVal = "Unreconciled Settlement"
		} else {
			// Check if all buckets match down to 0.01
			hasBucketDiff := false
			for _, cat := range categories {
				pVal := 0.0
				for _, fld := range cat.FieldNames {
					pVal += payBucketsOverall[fld]
				}
				sVal := 0.0
				for _, fld := range cat.FieldNames {
					sVal += setBucketsOverall[fld]
				}
				if abs(pVal-sVal) > 0.01 {
					hasBucketDiff = true
					break
				}
			}
			if hasBucketDiff {
				statusVal = "Unreconciled"
			}
		}

		// 1. Write all Payment rows
		for _, r := range payRows {
			var payMap map[string]string
			_ = json.Unmarshal(r.Payload, &payMap)
			writePaymentsRaw(f, constDataSheet, currentRow, payMap, r.LineNumber)

			rowAllocations := make(map[string]float64)
			for _, a := range allocMap[r.ID] {
				rowAllocations[a.Field] += a.Amount
			}

			// Write Payments allocations (Cols 26-46)
			for _, cat := range categories {
				sumVal := 0.0
				for _, fld := range cat.FieldNames {
					sumVal += rowAllocations[fld]
				}
				if sumVal != 0.0 {
					colLetter := getColumnLetter(cat.ColPayIdx)
					_ = f.SetCellValue(constDataSheet, fmt.Sprintf("%s%d", colLetter, currentRow), sumVal)
				}
			}

			// Write Difference formulas (Cols 92-112)
			for i, cat := range categories {
				colLetter := getColumnLetter(92 + i)
				payLetter := getColumnLetter(cat.ColPayIdx)
				setLetter := getColumnLetter(cat.ColSetIdx)
				formula := fmt.Sprintf("%s%d-%s%d", payLetter, currentRow, setLetter, currentRow)
				_ = f.SetCellFormula(constDataSheet, fmt.Sprintf("%s%d", colLetter, currentRow), formula)
			}

			// Write audit columns
			_ = f.SetCellValue(constDataSheet, fmt.Sprintf("DI%d", currentRow), ref)
			_ = f.SetCellValue(constDataSheet, fmt.Sprintf("DJ%d", currentRow), statusVal)

			// Borders
			for c := 1; c <= 116; c++ {
				cellRef := fmt.Sprintf("%s%d", getColumnLetter(c), currentRow)
				_ = f.SetCellStyle(constDataSheet, cellRef, cellRef, borderStyle)
			}
			currentRow++
		}

		// 2. Write all Settlement rows
		for _, r := range setRows {
			var setMap map[string]string
			_ = json.Unmarshal(r.Payload, &setMap)
			writeSettlementsRaw(f, constDataSheet, currentRow, setMap, r.LineNumber)

			rowAllocations := make(map[string]float64)
			for _, a := range allocMap[r.ID] {
				rowAllocations[a.Field] += a.Amount
			}

			// Write Settlements allocations (Cols 71-91)
			for _, cat := range categories {
				sumVal := 0.0
				for _, fld := range cat.FieldNames {
					sumVal += rowAllocations[fld]
				}
				if sumVal != 0.0 {
					colLetter := getColumnLetter(cat.ColSetIdx)
					_ = f.SetCellValue(constDataSheet, fmt.Sprintf("%s%d", colLetter, currentRow), sumVal)
				}
			}

			// Write Difference formulas (Cols 92-112)
			for i, cat := range categories {
				colLetter := getColumnLetter(92 + i)
				payLetter := getColumnLetter(cat.ColPayIdx)
				setLetter := getColumnLetter(cat.ColSetIdx)
				formula := fmt.Sprintf("%s%d-%s%d", payLetter, currentRow, setLetter, currentRow)
				_ = f.SetCellFormula(constDataSheet, fmt.Sprintf("%s%d", colLetter, currentRow), formula)
			}

			// Write audit columns
			_ = f.SetCellValue(constDataSheet, fmt.Sprintf("DI%d", currentRow), ref)
			_ = f.SetCellValue(constDataSheet, fmt.Sprintf("DJ%d", currentRow), statusVal)

			// Borders
			for c := 1; c <= 116; c++ {
				cellRef := fmt.Sprintf("%s%d", getColumnLetter(c), currentRow)
				_ = f.SetCellStyle(constDataSheet, cellRef, cellRef, borderStyle)
			}
			currentRow++
		}
	}

	endRow := currentRow - 1

	// Delete default Sheet1
	_ = f.DeleteSheet("Sheet1")

	// 2. Build Summary Sheet
	summarySheet := "Summary"
	_, _ = f.NewSheet(summarySheet)

	showGridLinesSummary := true
	_ = f.SetSheetView(summarySheet, -1, &excelize.ViewOptions{
		ShowGridLines: &showGridLinesSummary,
	})

	// Headers
	_ = f.SetCellValue(summarySheet, "C1", "Payments")
	_ = f.SetCellValue(summarySheet, "D1", "Settlements")
	_ = f.SetCellValue(summarySheet, "E1", "Payments - Settlements")

	// Apply Styles on Summary
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"366092"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	_ = f.SetCellStyle(summarySheet, "C1", "E1", headerStyle)

	// Styles for data rows
	labelStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 11},
		Alignment: &excelize.Alignment{Horizontal: "left"},
	})
	numStyle, _ := f.NewStyle(&excelize.Style{
		NumFmt: 4, // currency format `$#,##0.00`
	})

	boldSubtotalStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 1},
			{Type: "bottom", Color: "000000", Style: 1},
		},
		NumFmt: 4,
	})

	// Populate categories with formulas
	for _, cat := range categories {
		_ = f.SetCellValue(summarySheet, fmt.Sprintf("B%d", cat.Row), cat.Label)
		_ = f.SetCellStyle(summarySheet, fmt.Sprintf("B%d", cat.Row), fmt.Sprintf("B%d", cat.Row), labelStyle)

		// Payments sum formula
		payCol := getColumnLetter(cat.ColPayIdx)
		payFormula := fmt.Sprintf("SUM('%s'!%s%d:%s%d)", constDataSheet, payCol, startRow, payCol, endRow)
		_ = f.SetCellFormula(summarySheet, fmt.Sprintf("C%d", cat.Row), payFormula)
		_ = f.SetCellStyle(summarySheet, fmt.Sprintf("C%d", cat.Row), fmt.Sprintf("C%d", cat.Row), numStyle)

		// Settlements sum formula
		setCol := getColumnLetter(cat.ColSetIdx)
		setFormula := fmt.Sprintf("SUM('%s'!%s%d:%s%d)", constDataSheet, setCol, startRow, setCol, endRow)
		_ = f.SetCellFormula(summarySheet, fmt.Sprintf("D%d", cat.Row), setFormula)
		_ = f.SetCellStyle(summarySheet, fmt.Sprintf("D%d", cat.Row), fmt.Sprintf("D%d", cat.Row), numStyle)

		// Diff formula
		diffFormula := fmt.Sprintf("C%d-D%d", cat.Row, cat.Row)
		_ = f.SetCellFormula(summarySheet, fmt.Sprintf("E%d", cat.Row), diffFormula)
		_ = f.SetCellStyle(summarySheet, fmt.Sprintf("E%d", cat.Row), fmt.Sprintf("E%d", cat.Row), numStyle)
	}

	// Add Subtotal Labels & Formulas
	subtotals := []struct {
		Label    string
		Row      int
		StartRow int
		EndRow   int
	}{
		{"Sales", 4, 5, 13},
		{"Refunds", 15, 16, 17},
		{"Expenses", 19, 20, 28},
	}

	for _, sub := range subtotals {
		_ = f.SetCellValue(summarySheet, fmt.Sprintf("B%d", sub.Row), sub.Label)
		_ = f.SetCellStyle(summarySheet, fmt.Sprintf("B%d", sub.Row), fmt.Sprintf("B%d", sub.Row), labelStyle)

		// Payments Subtotal
		_ = f.SetCellFormula(summarySheet, fmt.Sprintf("C%d", sub.Row), fmt.Sprintf("SUM(C%d:C%d)", sub.StartRow, sub.EndRow))
		_ = f.SetCellStyle(summarySheet, fmt.Sprintf("C%d", sub.Row), fmt.Sprintf("C%d", sub.Row), boldSubtotalStyle)

		// Settlements Subtotal
		_ = f.SetCellFormula(summarySheet, fmt.Sprintf("D%d", sub.Row), fmt.Sprintf("SUM(D%d:D%d)", sub.StartRow, sub.EndRow))
		_ = f.SetCellStyle(summarySheet, fmt.Sprintf("D%d", sub.Row), fmt.Sprintf("D%d", sub.Row), boldSubtotalStyle)

		// Diff Subtotal
		_ = f.SetCellFormula(summarySheet, fmt.Sprintf("E%d", sub.Row), fmt.Sprintf("C%d-D%d", sub.Row, sub.Row))
		_ = f.SetCellStyle(summarySheet, fmt.Sprintf("E%d", sub.Row), fmt.Sprintf("E%d", sub.Row), boldSubtotalStyle)
	}

	// Paid to Amazon formatting
	_ = f.SetCellStyle(summarySheet, "B32", "E32", boldSubtotalStyle)

	// Set column widths in Summary
	_ = f.SetColWidth(summarySheet, "B", "B", 35)
	_ = f.SetColWidth(summarySheet, "C", "E", 22)

	// Auto-fit column widths in Consolidated Data
	for c := 1; c <= 116; c++ {
		colLetter := getColumnLetter(c)
		_ = f.SetColWidth(constDataSheet, colLetter, colLetter, 15)
	}
	// Specifically widen columns containing record_ref and text descriptions
	_ = f.SetColWidth(constDataSheet, "F", "F", 35)   // Payments desc
	_ = f.SetColWidth(constDataSheet, "BH", "BH", 35) // Settlements desc
	_ = f.SetColWidth(constDataSheet, "DI", "DI", 45) // record_ref
	_ = f.SetColWidth(constDataSheet, "DJ", "DJ", 22) // status

	// Save file
	err = f.SaveAs(outputPath)
	if err != nil {
		return fmt.Errorf("failed to save Excel file: %w", err)
	}

	return nil
}

func abs(val float64) float64 {
	if val < 0 {
		return -val
	}
	return val
}

// getColumnLetter returns Excel column name from index (e.g. 1 -> A, 28 -> AB)
func getColumnLetter(col int) string {
	name := ""
	for col > 0 {
		col--
		name = string(rune('A'+col%26)) + name
		col /= 26
	}
	return name
}

func writeConsolidatedHeaders(f *excelize.File, sheet string) {
	// Let's write headers and style them nicely
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"366092"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})

	// Row 9 is Category grouping
	_ = f.MergeCell(sheet, "A9", "Y9")
	_ = f.SetCellValue(sheet, "A9", "Payments Raw Data")
	_ = f.MergeCell(sheet, "Z9", "AT9")
	_ = f.SetCellValue(sheet, "Z9", "Payments Summary Allocations")
	_ = f.MergeCell(sheet, "AU9", "BH9")
	_ = f.SetCellValue(sheet, "AU9", "Settlements Raw Data")
	_ = f.MergeCell(sheet, "BI9", "CC9")
	_ = f.SetCellValue(sheet, "BI9", "Settlements Summary Allocations")
	_ = f.MergeCell(sheet, "CD9", "CX9")
	_ = f.SetCellValue(sheet, "CD9", "Variance (Payments - Settlements)")

	_ = f.SetCellStyle(sheet, "A9", "CX9", headerStyle)

	// Row 10 is Column Names
	headers10 := make([]string, 116)
	// Payments raw (Cols 1-25)
	payRaw := []string{
		"date/time", "settlement ID", "type", "order ID", "sku", "description", "quantity",
		"marketplace", "fulfilment", "order city", "order state", "order postal", "product sales",
		"shipping credits", "gift wrap credits", "promotional rebates", "sales tax collected",
		"low value goods", "selling fees", "fulfilment by amazon fees", "other transaction fees",
		"other", "total", "Transaction status", "Transaction Release Date",
	}
	copy(headers10[0:25], payRaw)

	// Payments Summary (Cols 26-46)
	for i, cat := range categories {
		headers10[25+i] = cat.Label
	}

	// Settlements raw (Cols 47-70)
	setRaw := []string{
		"settlement-id", "settlement-start-date", "settlement-end-date", "deposit-date", "total-amount",
		"currency", "transaction-type", "order-id", "merchant-order-id", "adjustment-id", "shipment-id",
		"marketplace-name", "amount-type", "amount-description", "amount", "fulfillment-id", "posted-date",
		"posted-date-time", "order-item-code", "merchant-order-item-id", "merchant-adjustment-item-id",
		"sku", "quantity-purchased", "promotion-id",
	}
	copy(headers10[46:70], setRaw)

	// Settlements Summary (Cols 71-91)
	for i, cat := range categories {
		headers10[70+i] = cat.Label
	}

	// Differences (Cols 92-112)
	for i, cat := range categories {
		headers10[91+i] = "Diff " + cat.Label
	}

	// Audit fields
	headers10[112] = "record_ref"
	headers10[113] = "reconciliation_status"
	headers10[114] = "payments_line_number"
	headers10[115] = "settlements_line_number"

	for c, h := range headers10 {
		colLetter := getColumnLetter(c + 1)
		_ = f.SetCellValue(sheet, fmt.Sprintf("%s10", colLetter), h)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("%s10", colLetter), fmt.Sprintf("%s10", colLetter), headerStyle)
	}
}

func writePaymentsRaw(f *excelize.File, sheet string, row int, m map[string]string, lineNum int) {
	_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), m["date/time"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row), m["settlement ID"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", row), m["type"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", row), m["order ID"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("E%d", row), m["sku"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("F%d", row), m["description"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("G%d", row), m["quantity"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("H%d", row), m["marketplace"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("I%d", row), m["fulfilment"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("J%d", row), m["order city"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("K%d", row), m["order state"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("L%d", row), m["order postal"])

	writeFloat(f, sheet, fmt.Sprintf("M%d", row), m["product sales"])
	writeFloat(f, sheet, fmt.Sprintf("N%d", row), m["shipping credits"])
	writeFloat(f, sheet, fmt.Sprintf("O%d", row), m["gift wrap credits"])
	writeFloat(f, sheet, fmt.Sprintf("P%d", row), m["promotional rebates"])
	writeFloat(f, sheet, fmt.Sprintf("Q%d", row), m["sales tax collected"])
	writeFloat(f, sheet, fmt.Sprintf("R%d", row), m["low value goods"])
	writeFloat(f, sheet, fmt.Sprintf("S%d", row), m["selling fees"])
	writeFloat(f, sheet, fmt.Sprintf("T%d", row), m["fulfilment by amazon fees"])
	writeFloat(f, sheet, fmt.Sprintf("U%d", row), m["other transaction fees"])
	writeFloat(f, sheet, fmt.Sprintf("V%d", row), m["other"])
	writeFloat(f, sheet, fmt.Sprintf("W%d", row), m["total"])

	_ = f.SetCellValue(sheet, fmt.Sprintf("X%d", row), m["Transaction status"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("Y%d", row), m["Transaction Release Date"])

	// Col 115: payments_line_number
	_ = f.SetCellValue(sheet, fmt.Sprintf("DK%d", row), lineNum)
}

func writeSettlementsRaw(f *excelize.File, sheet string, row int, m map[string]string, lineNum int) {
	_ = f.SetCellValue(sheet, fmt.Sprintf("AU%d", row), m["settlement-id"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("AV%d", row), m["settlement-start-date"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("AW%d", row), m["settlement-end-date"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("AX%d", row), m["deposit-date"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("AY%d", row), m["total-amount"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("AZ%d", row), m["currency"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BA%d", row), m["transaction-type"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BB%d", row), m["order-id"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BC%d", row), m["merchant-order-id"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BD%d", row), m["adjustment-id"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BE%d", row), m["shipment-id"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BF%d", row), m["marketplace-name"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BG%d", row), m["amount-type"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BH%d", row), m["amount-description"])

	writeFloat(f, sheet, fmt.Sprintf("BI%d", row), m["amount"])

	_ = f.SetCellValue(sheet, fmt.Sprintf("BJ%d", row), m["fulfillment-id"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BK%d", row), m["posted-date"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BL%d", row), m["posted-date-time"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BM%d", row), m["order-item-code"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BN%d", row), m["merchant-order-item-id"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BO%d", row), m["merchant-adjustment-item-id"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BP%d", row), m["sku"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BQ%d", row), m["quantity-purchased"])
	_ = f.SetCellValue(sheet, fmt.Sprintf("BR%d", row), m["promotion-id"])

	// Col 116: settlements_line_number
	_ = f.SetCellValue(sheet, fmt.Sprintf("DL%d", row), lineNum)
}

func writeFloat(f *excelize.File, sheet string, cell string, valStr string) {
	if valStr == "" {
		return
	}
	val, err := strconv.ParseFloat(strings.ReplaceAll(valStr, ",", ""), 64)
	if err == nil {
		_ = f.SetCellValue(sheet, cell, val)
	} else {
		_ = f.SetCellValue(sheet, cell, valStr)
	}
}
