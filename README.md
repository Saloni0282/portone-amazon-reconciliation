# Amazon Payments vs Settlement Reconciliation Tool

A Go-based reconciliation tool that ingests Amazon Payments CSV and Amazon Settlements TSV reports for a settlement period, maps and reconciles transactions using config-driven logic in PostgreSQL, and generates an audit-friendly Excel workbook.

---

## Tech Stack
- **Language**: Golang (Go 1.20+)
- **Database**: PostgreSQL (v16)
- **Core Packages**:
  - `github.com/lib/pq`: PostgreSQL driver
  - `github.com/xuri/excelize/v2`: Excel spreadsheet generator

---

## Directory Structure
- `db/migrations.sql`: Schema definitions for raw ingestion, config mappings, and audit trails.
- `db/MAPPING_FIXES.sql`: SQL corrections for identified Amazon config rules defects.
- `src/`: Go codebase containing ingestion, rule mapping, key template matching, and Excel output builders.
- `reconciler.exe`: Compiled executable binary.
- `db_dump.sql`: Full database backup including raw records, mapping configurations, and output reconciliations.

---

## Database Setup & Initialization

1. Connect to PostgreSQL on port `5432` and create a database named `amazon_reconciliation`.
2. Apply the table schemas:
   ```bash
   psql -h localhost -p 5432 -U postgres -d amazon_reconciliation -f db/migrations.sql
   ```

---

## How to Build and Run

1. **Compile the binary**:
   ```bash
   go build -o reconciler.exe ./src/
   ```

2. **Run the entire pipeline (End-to-End)**:
   This runs configuration loading, full raw data ingestion, reconciliation mapping, and output report generation:
   ```bash
   ./reconciler.exe
   ```

3. **Incremental runs (Skip Ingestion)**:
   If raw data is already ingested, run using skip flags to bypass ingestion and reload steps:
   ```bash
   ./reconciler.exe -skip-ingest -skip-config
   ```

### Command Flags
- `-payments`: Path to raw Payments CSV (default: `inputs/amazon_payments_data.csv`)
- `-settlements`: Path to raw Settlements TSV (default: `inputs/amazon_settlements_data.txt`)
- `-pay-config`: Path to Payments configs CSV (default: `inputs/amazon_payment_configs_au_old.csv`)
- `-set-config`: Path to Settlements configs CSV (default: `inputs/amazon_settlement_configs_au.csv`)
- `-output`: Location of output report (default: `amazon_reconciliation_after_fix.xlsx`)
- `-settlement-id`: Target settlement ID (default: `12395580393`)

---

## How the Reconciliation Works

### 1. Ingestion
We ingest **all raw lines** from both files into `ingested_records` with an `idempotency_key` constraint to ensure complete source data auditability without duplicate records.

### 2. Reconciliation Eligibility
Payments rows with status `Deferred` are excluded from active reconciliation because they represent future holds not settled in the current cash-flow period.

### 3. Matching Precedence
Mapping is config-driven and resolved in Go based on the following priority list:
* **Payments Matching**:
  1. Exact match on transaction type and description.
  2. Wildcard match on description (`*` or `ANY`).
  3. Prefix matching restricted exclusively to transfer payout rules (`TRANSFER` matching `TO_ACCOUNT_ENDING` or `TO_YOUR_ACCOUNT_ENDING`).
  4. Fallback rules (empty transaction type catch-all).
* **Settlements Matching**:
  1. Specific match on transaction type, amount type, and description.
  2. Fallback rules (empty transaction type catch-all).

### 4. Excel Structure & Auditability
The generated reports are:
- **`amazon_reconciliation_before_fix.xlsx`**: Generated using original config files showing mismatches.
- **`amazon_reconciliation_after_fix.xlsx`**: Generated using the corrected database mapping configuration, showing $0.00 variance.
- **`before_fix_mismatches.csv`**: Summary of mismatches prior to applying config fixes.
- **`mismatch_investigation.md`**: Investigation details for each mapping defect.
- **`Consolidated Data` Sheet**: Lists raw fields side-by-side alongside summary allocations and differences calculated via live cell formulas. Row display is grouped sequentially under each `record_ref` to preserve full granularity.

---

## Mapping Defect Log (`db/MAPPING_FIXES.sql`)
The configurations provided contained several discrepancies which have been corrected entirely via database queries:
- **Defects #1 & #2**: Removed low-value goods and tax allocations that double-routed orders to shipping.
- **Defect #3**: Updated the refund tax routing rule (previously had empty destination fields) to route to `refunded_expenses`.
- **Defects #4, #5, #6, & #7**: Re-routed GST-inclusive Australia marketplace shipping taxes, giftwrap taxes, tax discounts, and withheld low-value goods taxes to `sales_product_charges` to align Settlements with Payments.

---

## Verification
The pipeline is verified using a automated test harness:
- **Zero Variance**: Accounting assertions verify a perfect $0.00 variance across all 9 summary categories.
- **Leakage Prevention**: Checks verify that Payments columns derive exclusively from Payments source records, and Settlements columns from Settlements source records.
- **Many-to-Many Aggregation**: Database-level tests verify correct summation of multiple split rows under a shared `record_ref`.
- **Auditable Granularity**: The sequential row grouping on the `Consolidated Data` sheet retains every raw file line number and amount, preserving full audit capability for finance users.
