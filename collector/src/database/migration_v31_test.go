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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Migration 31 adds alerter_settings.tier1_scoring, where the alerter
// publishes its effective Tier 1 scoring settings for the server to read
// (issue #348).

// migrationV31 returns the registered version 31 migration.
func migrationV31(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 31 {
			return m
		}
	}
	t.Fatal("migration version 31 is not registered")
	return Migration{}
}

// tier1ScoringState reports whether alerter_settings.tier1_scoring exists
// as JSONB and whether it carries a comment.
func tier1ScoringState(t *testing.T, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (column, columnComment bool) {
	t.Helper()
	if err := q.QueryRow(context.Background(), `
		SELECT
		    EXISTS (SELECT 1 FROM information_schema.columns
		            WHERE table_name = 'alerter_settings'
		              AND column_name = 'tier1_scoring'
		              AND data_type = 'jsonb'),
		    COALESCE(col_description('alerter_settings'::regclass,
		        (SELECT attnum FROM pg_attribute
		         WHERE attrelid = 'alerter_settings'::regclass
		           AND attname = 'tier1_scoring')), '') <> ''
	`).Scan(&column, &columnComment); err != nil {
		t.Fatalf("failed to read the tier1_scoring state: %v", err)
	}
	return column, columnComment
}

// TestMigrationV31_Registered verifies the migration is wired into the
// schema manager with a description.
func TestMigrationV31_Registered(t *testing.T) {
	if got := NewSchemaManager().LatestVersion(); got < 31 {
		t.Errorf("LatestVersion() = %d, want at least 31", got)
	}
	if desc := migrationV31(t).Description; desc == "" {
		t.Error("migration 31 has an empty description")
	}
}

// TestMigrationV31_FreshInstall verifies a freshly migrated datastore
// has the commented column, NULL on the singleton row.
func TestMigrationV31_FreshInstall(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	column, columnComment := tier1ScoringState(t, tx)
	if !column || !columnComment {
		t.Fatalf("tier1_scoring column=%v comment=%v, want both true", column, columnComment)
	}
	var isNull bool
	if err := tx.QueryRow(context.Background(),
		`SELECT tier1_scoring IS NULL FROM alerter_settings WHERE id = 1`).Scan(&isNull); err != nil {
		t.Fatalf("failed to read the settings row: %v", err)
	}
	if !isNull {
		t.Error("tier1_scoring is set on a fresh install, want NULL")
	}
}

// TestMigrationV31_UpgradeAndRerun drops the column, runs the migration
// as an upgrade would, and runs it again to prove it is idempotent.
func TestMigrationV31_UpgradeAndRerun(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	if _, err := tx.Exec(context.Background(),
		`ALTER TABLE alerter_settings DROP COLUMN tier1_scoring`); err != nil {
		t.Fatalf("failed to undo migration 31: %v", err)
	}
	if column, _ := tier1ScoringState(t, tx); column {
		t.Fatal("tier1_scoring survived being dropped")
	}
	for run := 1; run <= 2; run++ {
		if err := migrationV31(t).Up(tx); err != nil {
			t.Fatalf("migration 31 run %d failed: %v", run, err)
		}
		column, columnComment := tier1ScoringState(t, tx)
		if !column || !columnComment {
			t.Fatalf("after run %d: column=%v comment=%v, want both true", run, column, columnComment)
		}
	}
}

// TestMigrationV31_ReportsFailure drives the error path by moving the
// table out of the way, so the ALTER TABLE fails.
func TestMigrationV31_ReportsFailure(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	if _, err := tx.Exec(context.Background(),
		`ALTER TABLE alerter_settings RENAME TO alerter_settings_moved`); err != nil {
		t.Fatalf("failed to move alerter_settings aside: %v", err)
	}
	err := migrationV31(t).Up(tx)
	if err == nil {
		t.Fatal("migration 31 succeeded without an alerter_settings table")
	}
	if !strings.Contains(err.Error(), "alerter_settings.tier1_scoring") {
		t.Errorf("migration 31 error = %q, want it to name the column", err.Error())
	}
}
