# Progress Log

## Work Accomplished

### 1. Initial Investigation (Python Prototype)
- Built a Python prototype in the `scratch/` directory to quickly explore raw datasets, test date-parsing routines, and analyze matched key densities.
- Used the prototype solely for investigation, variance analysis, and validation.
- Identified that Australia marketplace order tax, shipping tax, and giftwrap tax are GST-inclusive and must map to product charges.
- Identified that deferred status payments must be filtered out for matching.

### 2. Database & Schema Initialization (Phase 1)
- Initialized Go module `portone-amazon-reconciliation`.
- Created PostgreSQL tables `ingested_records`, `payment_configs`, `settlement_configs`, and `mapped_records`.
- Enforced a unique `idempotency_key` constraint to ensure multiple runs do not duplicate ingested data.
- Setup helper connection logic in `src/db.go`.

### 3. Config & Raw Ingestion Modules (Phase 1 & 2)
- Built the configuration CSV loaders in `src/config.go` with auto-clearing logic.
- Built the raw CSV/TSV ingesters in `src/ingest.go` to parse and insert raw lines as JSON payloads, retaining all line details for auditing.
- Implemented UTC conversions and date parsers in `src/engine.go` (regex-based timezone extractor for Payments, and standard formatter for Settlements).

### 4. Reconciliation Mapping & Matchers (Phase 3 & 4)
- Programmed prefix-based matching for payouts and wildcard configurations in `src/engine.go`.
- Implemented rule matching precedence (Exact -> Wildcards -> Prefix -> Fallbacks) in `src/reconcile.go`.
- Wrote the Excel reporter using `excelize` in `src/report.go` to construct side-by-side reconciliation grids and dynamically link Summary lines with cell formulas.

### 5. Config Corrections (Phase 5)
- Documented all 7 category-routing defects in `db/MAPPING_FIXES.sql`.
- Applied the SQL script to PostgreSQL, correcting routing paths in the database configuration tables.

### 6. Verification & Final Execution (Phase 6)
- Compiled the Go program into `reconciler.exe`.
- Generated `amazon_settlement_reconciliation_report.xlsx` with clean cell formulas.
- Wrote automated Go unit tests covering date parsing, normalizations, and rule matches.
- Wrote accounting assertions validating zero variance, no database leakage, and matching totals.
- Generated `db_dump.sql` containing full schema and data.

---

## Final Verification Results
All Go unit tests and accounting assertions have **passed**. The final reconciliation numbers show a perfect $0.00 variance:

```bash
=== RUN   TestAccountingAssertions
    accounting_test.go:74: Reconciled: expenses_amazon_fees           | Pay:-132593.41 | Set:-132593.41 | Diff: 0.00
    accounting_test.go:74: Reconciled: expenses_fba_fees              | Pay:     -0.41 | Set:     -0.41 | Diff: 0.00
    accounting_test.go:74: Reconciled: expenses_promotional_rebates   | Pay: -11273.53 | Set: -11273.53 | Diff: 0.00
    accounting_test.go:74: Reconciled: refunded_expenses              | Pay:    216.28 | Set:    216.28 | Diff: 0.00
    accounting_test.go:74: Reconciled: refunded_sales                 | Pay:  -2031.37 | Set:  -2031.37 | Diff: 0.00
    accounting_test.go:74: Reconciled: sales_inventory_reimbursements | Pay:    679.69 | Set:    679.69 | Diff: 0.00
    accounting_test.go:74: Reconciled: sales_other                    | Pay:     11.61 | Set:     11.61 | Diff: 0.00
    accounting_test.go:74: Reconciled: sales_product_charges          | Pay: 348815.93 | Set: 348815.93 | Diff: 0.00
    accounting_test.go:74: Reconciled: sales_shipping                 | Pay:   8294.16 | Set:   8294.16 | Diff: 0.00
--- PASS: TestAccountingAssertions (0.51s)
PASS
```
