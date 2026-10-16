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
	"testing"
)

// TestMigration_PgSettings tests the pg_settings table creation in the squashed migration
func TestMigration_PgSettings(t *testing.T) {
	// Run in the package's own per-run database rather than the one
	// TEST_AI_WORKBENCH_SERVER names, and start from the full schema
	// cleanup the other migration tests use. The database the URL names
	// is shared with the server and alerter suites, which leave their own
	// tables behind; an alerter-shaped anomaly_candidates there made the
	// migration's CREATE TABLE IF NOT EXISTS a no-op and its index on
	// detected_at fail (issue #486).
	pool, conn := getTestConnection(t)
	defer pool.Close()
	defer conn.Release()
	cleanupTestSchema(t, pool)

	ctx := context.Background()

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
	err := conn.QueryRow(ctx, "SELECT MAX(version) FROM schema_version").Scan(&version)
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
