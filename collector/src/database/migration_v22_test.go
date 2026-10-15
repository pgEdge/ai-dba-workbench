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
)

// The v22 migration adds tier3_source and tier3_model to
// anomaly_candidates, recording which kind of model produced each Tier 3
// verdict (GitHub issue #626).

// migrationV22 returns the registered version 22 migration.
func migrationV22(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 22 {
			return m
		}
	}
	t.Fatal("migration version 22 is not registered")
	return Migration{}
}

// TestMigrationV22_Registered verifies the migration is wired into the
// schema manager and will be reached by an upgrade.
func TestMigrationV22_Registered(t *testing.T) {
	if got := NewSchemaManager().LatestVersion(); got < 22 {
		t.Errorf("LatestVersion() = %d, want at least 22", got)
	}
	if desc := migrationV22(t).Description; desc == "" {
		t.Error("migration 22 has an empty description")
	}
}

// TestMigrationV22_FreshInstall verifies that a freshly migrated
// datastore has both columns, documented, and that tier3_source accepts
// only the two documented values or NULL.
func TestMigrationV22_FreshInstall(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}
	ctx := context.Background()

	for _, column := range []string{"tier3_source", "tier3_model"} {
		var comment *string
		if err := pool.QueryRow(ctx, `
			SELECT col_description('anomaly_candidates'::regclass, attnum)
			FROM pg_attribute
			WHERE attrelid = 'anomaly_candidates'::regclass AND attname = $1
		`, column).Scan(&comment); err != nil {
			t.Fatalf("failed to read comment on %s: %v", column, err)
		}
		if comment == nil || *comment == "" {
			t.Errorf("anomaly_candidates.%s has no COMMENT", column)
		}
	}

	var connectionID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO connections (name, host, port, database_name, username, password_encrypted, owner_username)
		VALUES ('v22-test', '127.0.0.1', 5432, 'postgres', 'postgres', '', 'v22-test-owner')
		RETURNING id
	`).Scan(&connectionID); err != nil {
		t.Fatalf("failed to seed connection: %v", err)
	}

	insert := func(source *string) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO anomaly_candidates
			    (connection_id, metric_name, metric_value, z_score,
			     detected_at, context, tier3_source, tier3_model)
			VALUES ($1, 'm', 1, 3, now(), '{}', $2, 'typesafe/jev-latest')
		`, connectionID, source)
		return err
	}
	for _, source := range []string{"decision", "reasoning"} {
		if err := insert(&source); err != nil {
			t.Errorf("tier3_source %q rejected: %v", source, err)
		}
	}
	if err := insert(nil); err != nil {
		t.Errorf("NULL tier3_source rejected: %v", err)
	}
	bogus := "guess"
	if err := insert(&bogus); err == nil ||
		!strings.Contains(err.Error(), "anomaly_candidates_tier3_source_check") {
		t.Errorf("tier3_source %q: err = %v, want a check violation", bogus, err)
	}
}

// TestMigrationV22_Idempotent re-runs the migration on a datastore that
// already has the columns, as a partly applied upgrade would.
func TestMigrationV22_Idempotent(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil &&
			!strings.Contains(err.Error(), "closed") {
			t.Logf("rollback failed: %v", err)
		}
	}()
	if err := migrationV22(t).Up(tx); err != nil {
		t.Fatalf("re-running migration 22 failed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit migration 22: %v", err)
	}
}

// TestMigrationV22_ReportsStatementFailure drives the migration's error
// path by renaming the table it alters inside a transaction that is
// rolled back afterwards.
func TestMigrationV22_ReportsStatementFailure(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			t.Logf("rollback failed: %v", err)
		}
	}()

	if _, err := tx.Exec(ctx,
		`ALTER TABLE anomaly_candidates RENAME TO anomaly_candidates_hidden`); err != nil {
		t.Fatalf("failed to hide anomaly_candidates: %v", err)
	}

	err = migrationV22(t).Up(tx)
	if err == nil {
		t.Fatal("migration 22 succeeded without an anomaly_candidates table")
	}
	if !strings.Contains(err.Error(), "Tier 3 source columns") {
		t.Errorf("migration 22 error = %q, want it to name the failure",
			err.Error())
	}
}
