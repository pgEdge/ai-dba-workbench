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

// Migration 32 marks server-level probe_configs rows edited through the
// API as user overrides, so the collector's inherited-settings sync
// (issue #810) does not overwrite them on upgrade.

// migrationV32 returns the registered version 32 migration.
func migrationV32(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 32 {
			return m
		}
	}
	t.Fatal("migration version 32 is not registered")
	return Migration{}
}

// TestMigrationV32_Registered verifies the migration is wired into the
// schema manager with a description.
func TestMigrationV32_Registered(t *testing.T) {
	if got := NewSchemaManager().LatestVersion(); got < 32 {
		t.Errorf("LatestVersion() = %d, want at least 32", got)
	}
	if desc := migrationV32(t).Description; desc == "" {
		t.Error("migration 32 has an empty description")
	}
}

// TestMigrationV32_MarksEditedServerRows seeds a server row as the
// collector creates it, a server row edited afterwards, and an edited
// global row, then runs the migration twice: only the edited server row
// becomes an override, and the second run changes nothing.
func TestMigrationV32_MarksEditedServerRows(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	ctx := context.Background()

	var connID int
	if err := tx.QueryRow(ctx, `
		INSERT INTO connections (name, host, port, database_name, username, owner_username)
		VALUES ('v32-test', 'localhost', 5432, 'postgres', 'postgres', 'tester')
		RETURNING id`).Scan(&connID); err != nil {
		t.Fatalf("failed to seed connection: %v", err)
	}

	insertServer := func(name, updatedAt string) int {
		t.Helper()
		var id int
		if err := tx.QueryRow(ctx, `
			INSERT INTO probe_configs (name, description, scope, connection_id, created_at, updated_at)
			VALUES ($1, 'v32', 'server', $2, NOW(), NOW() + $3::interval)
			RETURNING id`, name, connID, updatedAt).Scan(&id); err != nil {
			t.Fatalf("failed to seed server row %s: %v", name, err)
		}
		return id
	}
	copied := insertServer("v32_copied", "0 seconds")
	edited := insertServer("v32_edited", "1 second")

	if _, err := tx.Exec(ctx, `
		UPDATE probe_configs SET updated_at = created_at + interval '1 second'
		WHERE scope = 'global'`); err != nil {
		t.Fatalf("failed to age global rows: %v", err)
	}

	for run := 1; run <= 2; run++ {
		if err := migrationV32(t).Up(tx); err != nil {
			t.Fatalf("migration 32 run %d failed: %v", run, err)
		}
		if userModifiedOf(t, tx, copied) {
			t.Errorf("run %d: a server row the collector created was marked user_modified", run)
		}
		if !userModifiedOf(t, tx, edited) {
			t.Errorf("run %d: a server row edited after creation was not marked user_modified", run)
		}
		var globals int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM probe_configs WHERE scope = 'global' AND user_modified`).Scan(&globals); err != nil {
			t.Fatalf("failed to count global overrides: %v", err)
		}
		if globals != 0 {
			t.Errorf("run %d: %d global rows were marked user_modified, want 0", run, globals)
		}
	}

	var comment string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(col_description('probe_configs'::regclass,
		    (SELECT attnum FROM pg_attribute
		     WHERE attrelid = 'probe_configs'::regclass AND attname = 'user_modified')), '')`).Scan(&comment); err != nil {
		t.Fatalf("failed to read the column comment: %v", err)
	}
	if !strings.Contains(comment, "explicit override") {
		t.Errorf("user_modified comment = %q, want the override description", comment)
	}
}

// TestMigrationV32_ReportsFailure drives the error path by moving the
// table out of the way, so the UPDATE fails.
func TestMigrationV32_ReportsFailure(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	if _, err := tx.Exec(context.Background(),
		`ALTER TABLE probe_configs RENAME TO probe_configs_moved`); err != nil {
		t.Fatalf("failed to move probe_configs aside: %v", err)
	}
	err := migrationV32(t).Up(tx)
	if err == nil {
		t.Fatal("migration 32 succeeded without a probe_configs table")
	}
	if !strings.Contains(err.Error(), "server probe configs") {
		t.Errorf("migration 32 error = %q, want it to name the probe configs", err.Error())
	}
}

// userModifiedOf reads probe_configs.user_modified for one row.
func userModifiedOf(t *testing.T, tx pgx.Tx, id int) bool {
	t.Helper()
	var v bool
	if err := tx.QueryRow(context.Background(),
		`SELECT user_modified FROM probe_configs WHERE id = $1`, id).Scan(&v); err != nil {
		t.Fatalf("failed to read user_modified for %d: %v", id, err)
	}
	return v
}
