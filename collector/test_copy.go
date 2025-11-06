/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package main provides a test utility for PostgreSQL COPY functionality
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/lib/pq"
)

func main() {
	// Connect to database
	connStr := "host=localhost dbname=ai_workbench user=dpage sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = db.Close()
	}()

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Connected to database")

	// Check what database we're connected to
	var dbname string
	err = db.QueryRow("SELECT current_database()").Scan(&dbname)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Current database: %s\n", dbname)

	// Check if table exists
	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_tables WHERE schemaname='metrics' AND tablename='pg_stat_activity')").Scan(&exists)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Table metrics.pg_stat_activity exists: %v\n", exists)

	// Try CopyIn
	ctx := context.Background()
	txn, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = txn.Rollback()
	}()

	//Test 1: Try with a temporary table (non-partitioned)
	_, err = txn.Exec("CREATE TEMP TABLE test_copy_table (connection_id int, collected_at timestamp, datid oid, datname text)")
	if err != nil {
		log.Fatalf("Failed to create temp table: %v", err)
	}
	fmt.Println("Created temp table")

	columns := []string{"connection_id", "collected_at", "datid", "datname"}
	stmt, err := txn.PrepareContext(ctx, pq.CopyIn("test_copy_table", columns...))
	if err != nil {
		log.Fatalf("PrepareContext failed: %v", err)
	}
	defer func() {
		_ = stmt.Close()
	}()

	fmt.Println("CopyIn prepared successfully!")

	// Execute with a row
	_, err = stmt.ExecContext(ctx, 999, "2025-11-04 15:30:00", 16384, "copytest")
	if err != nil {
		log.Fatalf("ExecContext failed: %v", err)
	}

	// Complete the copy
	_, err = stmt.ExecContext(ctx)
	if err != nil {
		log.Fatalf("Final ExecContext failed: %v", err)
	}

	// Commit
	if err := txn.Commit(); err != nil {
		log.Fatalf("Commit failed: %v", err)
	}

	fmt.Println("COPY completed successfully!")
}
