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

// Migration 29 adds connections.deleted_at, which marks a connection
// the server is purging in the background (issue #669).

// migrationV29 returns the registered version 29 migration.
func migrationV29(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 29 {
			return m
		}
	}
	t.Fatal("migration version 29 is not registered")
	return Migration{}
}

// deletedAtState reports whether connections.deleted_at and its partial
// index exist, and whether both carry a comment.
func deletedAtState(t *testing.T, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (column, columnComment, index, indexComment bool) {
	t.Helper()
	if err := q.QueryRow(context.Background(), `
		SELECT
		    EXISTS (SELECT 1 FROM information_schema.columns
		            WHERE table_name = 'connections'
		              AND column_name = 'deleted_at'
		              AND data_type = 'timestamp with time zone'),
		    COALESCE(col_description('connections'::regclass,
		        (SELECT attnum FROM pg_attribute
		         WHERE attrelid = 'connections'::regclass
		           AND attname = 'deleted_at')), '') <> '',
		    to_regclass('idx_connections_deleted_at') IS NOT NULL,
		    COALESCE(obj_description(to_regclass('idx_connections_deleted_at'),
		        'pg_class'), '') <> ''
	`).Scan(&column, &columnComment, &index, &indexComment); err != nil {
		t.Fatalf("failed to read the deleted_at state: %v", err)
	}
	return column, columnComment, index, indexComment
}

// TestMigrationV29_Registered verifies the migration is wired into the
// schema manager with a description.
func TestMigrationV29_Registered(t *testing.T) {
	if got := NewSchemaManager().LatestVersion(); got < 29 {
		t.Errorf("LatestVersion() = %d, want at least 29", got)
	}
	if desc := migrationV29(t).Description; desc == "" {
		t.Error("migration 29 has an empty description")
	}
}

// TestMigrationV29_FreshInstall verifies a freshly migrated datastore
// has the column and the index, both commented, and that existing and
// new rows start live.
func TestMigrationV29_FreshInstall(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	column, columnComment, index, indexComment := deletedAtState(t, tx)
	if !column || !columnComment || !index || !indexComment {
		t.Fatalf("deleted_at column=%v comment=%v index=%v comment=%v, want all true",
			column, columnComment, index, indexComment)
	}
	var deletedAtNull bool
	if err := tx.QueryRow(context.Background(), `
		INSERT INTO connections (name, host, port, database_name, username, password_encrypted, owner_username)
		VALUES ('v29-test', '192.0.2.1', 5432, 'postgres', 'postgres', '', 'v29-test-owner')
		RETURNING deleted_at IS NULL
	`).Scan(&deletedAtNull); err != nil {
		t.Fatalf("failed to insert a connection: %v", err)
	}
	if !deletedAtNull {
		t.Error("a new connection starts with deleted_at set, want NULL")
	}
}

// TestMigrationV29_UpgradeAndRerun drops what the migration adds, runs
// it as an upgrade would, and runs it again to prove it is idempotent.
func TestMigrationV29_UpgradeAndRerun(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	if _, err := tx.Exec(context.Background(), `
		DROP INDEX idx_connections_deleted_at;
		ALTER TABLE connections DROP COLUMN deleted_at;
	`); err != nil {
		t.Fatalf("failed to undo migration 29: %v", err)
	}
	if column, _, index, _ := deletedAtState(t, tx); column || index {
		t.Fatal("deleted_at survived being dropped")
	}
	for run := 1; run <= 2; run++ {
		if err := migrationV29(t).Up(tx); err != nil {
			t.Fatalf("migration 29 run %d failed: %v", run, err)
		}
		column, columnComment, index, indexComment := deletedAtState(t, tx)
		if !column || !columnComment || !index || !indexComment {
			t.Fatalf("after run %d: column=%v comment=%v index=%v comment=%v, want all true",
				run, column, columnComment, index, indexComment)
		}
	}
}

// TestMigrationV29_ReportsFailure drives the error path by moving the
// connections table out of the way, so the ALTER TABLE fails.
func TestMigrationV29_ReportsFailure(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	if _, err := tx.Exec(context.Background(), `
		ALTER TABLE connections RENAME TO connections_moved;
	`); err != nil {
		t.Fatalf("failed to move connections aside: %v", err)
	}
	err := migrationV29(t).Up(tx)
	if err == nil {
		t.Fatal("migration 29 succeeded without a connections table")
	}
	if !strings.Contains(err.Error(), "connections.deleted_at") {
		t.Errorf("migration 29 error = %q, want it to name the column", err.Error())
	}
}
