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

// Migration 22 adds the tier3_source CHECK NOT VALID, so that adding it
// does not scan anomaly_candidates under an ACCESS EXCLUSIVE lock, and
// migration 23 validates it in a transaction of its own.

// migrationV23 returns the registered version 23 migration.
func migrationV23(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 23 {
			return m
		}
	}
	t.Fatal("migration version 23 is not registered")
	return Migration{}
}

// tier3SourceCheckValidated reports whether the tier3_source CHECK
// exists and is marked validated.
func tier3SourceCheckValidated(t *testing.T, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) bool {
	t.Helper()
	var validated bool
	if err := q.QueryRow(context.Background(), `
		SELECT convalidated FROM pg_constraint
		WHERE conrelid = 'anomaly_candidates'::regclass
		  AND conname = 'anomaly_candidates_tier3_source_check'
	`).Scan(&validated); err != nil {
		t.Fatalf("failed to read the tier3_source check: %v", err)
	}
	return validated
}

// beginV23Tx opens a transaction on a fully migrated schema that the
// caller's test rolls back.
func beginV23Tx(t *testing.T) (pgx.Tx, func()) {
	t.Helper()
	pool, cleanup := migrateV21TestSchema(t)
	if pool == nil {
		return nil, cleanup
	}
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		cleanup()
		t.Fatalf("failed to begin transaction: %v", err)
	}
	return tx, func() {
		if err := tx.Rollback(ctx); err != nil &&
			!strings.Contains(err.Error(), "closed") {
			t.Logf("rollback failed: %v", err)
		}
		cleanup()
	}
}

// TestMigrationV23_Registered verifies the migration is wired into the
// schema manager with a description.
func TestMigrationV23_Registered(t *testing.T) {
	if got := NewSchemaManager().LatestVersion(); got < 23 {
		t.Errorf("LatestVersion() = %d, want at least 23", got)
	}
	if desc := migrationV23(t).Description; desc == "" {
		t.Error("migration 23 has an empty description")
	}
}

// TestMigrationV23_FreshInstallValidated verifies that a freshly
// migrated datastore ends with the check validated.
func TestMigrationV23_FreshInstallValidated(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}
	if !tier3SourceCheckValidated(t, pool) {
		t.Error("tier3_source check is not validated after migration")
	}
}

// TestMigrationV22_AddsCheckNotValid verifies that migration 22 adds
// the check NOT VALID, and that migration 23 then validates it.
func TestMigrationV22_AddsCheckNotValid(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	ctx := context.Background()
	if _, err := tx.Exec(ctx, `
		ALTER TABLE anomaly_candidates
			DROP CONSTRAINT anomaly_candidates_tier3_source_check
	`); err != nil {
		t.Fatalf("failed to drop the check: %v", err)
	}
	if err := migrationV22(t).Up(tx); err != nil {
		t.Fatalf("migration 22 failed: %v", err)
	}
	if tier3SourceCheckValidated(t, tx) {
		t.Error("migration 22 added the check validated, want NOT VALID")
	}
	if err := migrationV23(t).Up(tx); err != nil {
		t.Fatalf("migration 23 failed: %v", err)
	}
	if !tier3SourceCheckValidated(t, tx) {
		t.Error("migration 23 did not validate the check")
	}
}

// TestMigrationV23_Idempotent re-runs the migration on a datastore
// whose check is already validated.
func TestMigrationV23_Idempotent(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	if err := migrationV23(t).Up(tx); err != nil {
		t.Fatalf("re-running migration 23 failed: %v", err)
	}
	if !tier3SourceCheckValidated(t, tx) {
		t.Error("tier3_source check is not validated after a re-run")
	}
}

// TestMigrationV23_RejectsInvalidRow proves validation scans the
// table: a row that slipped in whilst the check was NOT VALID fails the
// migration.
func TestMigrationV23_RejectsInvalidRow(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	ctx := context.Background()
	if _, err := tx.Exec(ctx, `
		ALTER TABLE anomaly_candidates
			DROP CONSTRAINT anomaly_candidates_tier3_source_check
	`); err != nil {
		t.Fatalf("failed to drop the check: %v", err)
	}
	var connectionID int
	if err := tx.QueryRow(ctx, `
		INSERT INTO connections (name, host, port, database_name, username, password_encrypted, owner_username)
		VALUES ('v23-test', '127.0.0.1', 5432, 'postgres', 'postgres', '', 'v23-test-owner')
		RETURNING id
	`).Scan(&connectionID); err != nil {
		t.Fatalf("failed to seed connection: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO anomaly_candidates
		    (connection_id, metric_name, metric_value, z_score,
		     detected_at, context, tier3_source)
		VALUES ($1, 'm', 1, 3, now(), '{}', 'guess')
	`, connectionID); err != nil {
		t.Fatalf("failed to seed an invalid row: %v", err)
	}
	if err := migrationV22(t).Up(tx); err != nil {
		t.Fatalf("migration 22 failed over an existing invalid row: %v", err)
	}
	err := migrationV23(t).Up(tx)
	if err == nil {
		t.Fatal("migration 23 validated a check that an existing row violates")
	}
	if !strings.Contains(err.Error(), "validate the Tier 3 source check") {
		t.Errorf("migration 23 error = %q, want it to name the failure", err.Error())
	}
}

// TestMigrationV22_ReportsConstraintFailure drives the error path of
// adding the check by giving tier3_source a type the check cannot
// compare.
func TestMigrationV22_ReportsConstraintFailure(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	if _, err := tx.Exec(context.Background(), `
		ALTER TABLE anomaly_candidates
			DROP CONSTRAINT anomaly_candidates_tier3_source_check,
			ALTER COLUMN tier3_source TYPE integer USING NULL
	`); err != nil {
		t.Fatalf("failed to retype tier3_source: %v", err)
	}
	err := migrationV22(t).Up(tx)
	if err == nil {
		t.Fatal("migration 22 added a text check to an integer column")
	}
	if !strings.Contains(err.Error(), "anomaly_candidates_tier3_source_check") {
		t.Errorf("migration 22 error = %q, want it to name the check", err.Error())
	}
}
