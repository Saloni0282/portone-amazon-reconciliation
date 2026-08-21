package main

import (
	"flag"
	"log"
	"time"
)

func main() {
	// 1. Define command-line flags
	paymentsPath := flag.String("payments", "inputs/amazon_payments_data.csv", "Path to raw Payments CSV file")
	settlementsPath := flag.String("settlements", "inputs/amazon_settlements_data.txt", "Path to raw Settlements TSV file")
	payConfigPath := flag.String("pay-config", "inputs/amazon_payment_configs_au_old.csv", "Path to Payments mapping configs CSV")
	setConfigPath := flag.String("set-config", "inputs/amazon_settlement_configs_au.csv", "Path to Settlements mapping configs CSV")
	outputPath := flag.String("output", "amazon_reconciliation_after_fix.xlsx", "Path to output Excel report")
	settlementID := flag.String("settlement-id", "12395580393", "Target Settlement ID to reconcile")
	skipIngest := flag.Bool("skip-ingest", false, "Skip raw data ingestion if already loaded")
	skipConfig := flag.Bool("skip-config", false, "Skip config loading if already loaded")

	flag.Parse()

	log.Println("Starting Amazon Reconciliation Pipeline...")
	start := time.Now()

	// 2. Connect to database
	db, err := ConnectDB()
	if err != nil {
		log.Fatalf("Fatal error connecting to database: %v", err)
	}
	defer db.Close()
	log.Println("Successfully connected to PostgreSQL database.")

	// 3. Load Configurations
	if !*skipConfig {
		log.Printf("Loading Payments configurations from %s...", *payConfigPath)
		err = LoadPaymentConfigs(db, *payConfigPath)
		if err != nil {
			log.Fatalf("Fatal error loading Payments configurations: %v", err)
		}
		log.Println("Payments configurations loaded successfully.")

		log.Printf("Loading Settlements configurations from %s...", *setConfigPath)
		err = LoadSettlementConfigs(db, *setConfigPath)
		if err != nil {
			log.Fatalf("Fatal error loading Settlements configurations: %v", err)
		}
		log.Println("Settlements configurations loaded successfully.")
	} else {
		log.Println("Skipping configuration reloading.")
	}

	// 4. Ingest Raw Datasets
	if !*skipIngest {
		log.Printf("Ingesting Payments CSV from %s...", *paymentsPath)
		payCount, err := IngestPayments(db, *paymentsPath)
		if err != nil {
			log.Fatalf("Fatal error ingesting Payments CSV: %v", err)
		}
		log.Printf("Payments ingestion finished. Ingested/Checked %d new rows.", payCount)

		log.Printf("Ingesting Settlements TSV from %s...", *settlementsPath)
		setCount, err := IngestSettlements(db, *settlementsPath)
		if err != nil {
			log.Fatalf("Fatal error ingesting Settlements TSV: %v", err)
		}
		log.Printf("Settlements ingestion finished. Ingested/Checked %d new rows.", setCount)
	} else {
		log.Println("Skipping raw data ingestion.")
	}

	// 5. Map Ingested Records
	log.Printf("Running reconciliation mapping engine for Settlement ID %s...", *settlementID)
	mappedCount, deferredCount, err := MapAllRecords(db, *settlementID)
	if err != nil {
		log.Fatalf("Fatal error during record mapping: %v", err)
	}
	log.Printf("Record mapping completed. Created %d mapped transaction allocations. Excluded %d deferred payments.", mappedCount, deferredCount)

	// 6. Generate Excel Report
	log.Printf("Generating Excel reconciliation report at %s...", *outputPath)
	err = GenerateReport(db, *settlementID, *outputPath)
	if err != nil {
		log.Fatalf("Fatal error generating Excel report: %v", err)
	}
	log.Printf("Excel report generated successfully. Saved to: %s", *outputPath)

	log.Printf("Pipeline finished successfully in %v.", time.Since(start))
}

// placeholder mapping to prevent package-level error
var _ = ParsePaymentsDate
