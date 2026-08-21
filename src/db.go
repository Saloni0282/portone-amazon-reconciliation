package main

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// ConnectDB initializes a connection to the PostgreSQL database.
func ConnectDB() (*sql.DB, error) {
	connStr := "host=localhost port=5432 user=postgres password=postgres dbname=amazon_reconciliation sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("error opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("error pinging database: %w", err)
	}

	return db, nil
}
