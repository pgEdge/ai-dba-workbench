/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigration_PgSettings tests the pg_settings table creation in the squashed migration
func TestMigration_PgSettings(t *testing.T) {
	// Skip if SKIP_DB_TESTS is set
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}

	// Get test database URL
	dbURL := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if dbURL == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set")
	}

	// Connect to database
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire connection: %v", err)
	}
	defer conn.Release()

	// Drop existing tables to start fresh
	_, err = conn.Exec(ctx, `
		DROP SCHEMA IF EXISTS metrics CASCADE;
		DROP TABLE IF EXISTS schema_version CASCADE;
		DROP TABLE IF EXISTS probe_configs CASCADE;
		DROP TABLE IF EXISTS connections CASCADE;
	`)
	if err != nil {
		t.Fatalf("Failed to drop existing tables: %v", err)
	}

	// Create schema manager and run all migrations
	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Verify schema version matches the latest registered migration
	expectedVersion := 0
	for _, m := range sm.migrations {
		if m.Version > expectedVersion {
			expectedVersion = m.Version
		}
	}

	var version int
	err = conn.QueryRow(ctx, "SELECT MAX(version) FROM schema_version").Scan(&version)
	if err != nil {
		t.Fatalf("Failed to query schema version: %v", err)
	}
	if version != expectedVersion {
		t.Errorf("Expected schema version %d, got %d", expectedVersion, version)
	}

	// Verify pg_settings table exists
	var tableExists bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_tables
			WHERE schemaname = 'metrics'
			  AND tablename = 'pg_settings'
		)
	`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("Failed to check pg_settings table existence: %v", err)
	}
	if !tableExists {
		t.Error("pg_settings table was not created")
	}

	// Verify pg_settings table is partitioned
	var isPartitioned bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'metrics'
			  AND c.relname = 'pg_settings'
			  AND c.relkind = 'p'
		)
	`).Scan(&isPartitioned)
	if err != nil {
		t.Fatalf("Failed to check pg_settings partitioning: %v", err)
	}
	if !isPartitioned {
		t.Error("pg_settings table is not partitioned")
	}

	// Verify foreign key constraint exists
	var fkExists bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conname = 'fk_pg_settings_connection_id'
		)
	`).Scan(&fkExists)
	if err != nil {
		t.Fatalf("Failed to check foreign key constraint: %v", err)
	}
	if !fkExists {
		t.Error("pg_settings foreign key constraint was not created")
	}

	// Verify probe configuration was inserted
	var configExists bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM probe_configs
			WHERE name = 'pg_settings'
			  AND connection_id IS NULL
			  AND is_enabled = TRUE
			  AND collection_interval_seconds = 3600
			  AND retention_days = 365
		)
	`).Scan(&configExists)
	if err != nil {
		t.Fatalf("Failed to check probe configuration: %v", err)
	}
	if !configExists {
		t.Error("pg_settings probe configuration was not inserted correctly")
	}

	// Verify all expected columns exist
	expectedColumns := []string{
		"connection_id", "name", "setting", "unit", "category",
		"short_desc", "extra_desc", "context", "vartype", "source",
		"min_val", "max_val", "enumvals", "boot_val", "reset_val",
		"sourcefile", "sourceline", "pending_restart", "collected_at",
	}

	for _, column := range expectedColumns {
		var colExists bool
		err = conn.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'metrics'
				  AND table_name = 'pg_settings'
				  AND column_name = $1
			)
		`, column).Scan(&colExists)
		if err != nil {
			t.Fatalf("Failed to check column %s: %v", column, err)
		}
		if !colExists {
			t.Errorf("Column %s does not exist in pg_settings table", column)
		}
	}
}
