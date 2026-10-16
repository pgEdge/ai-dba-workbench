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

	"github.com/jackc/pgx/v5/pgxpool"
)

// The v18 migration adds alerts.reevaluation_fingerprint, which the
// alerter uses to skip re-evaluating an acknowledged anomaly alert whose
// prompt inputs have not changed; see GitHub issue #575.

// fingerprintCommentSQL reads the comment on alerts.reevaluation_fingerprint.
// It is a named constant because the Codacy/Semgrep go_sql_rule-concat-sqli
// rule flags inline multi-line SQL.
const fingerprintCommentSQL = `
	SELECT COALESCE(col_description('alerts'::regclass, attnum), '')
	FROM pg_attribute
	WHERE attrelid = 'alerts'::regclass
	  AND attname = 'reevaluation_fingerprint'
`

// migrationV18 returns the registered version 18 migration.
func migrationV18(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 18 {
			return m
		}
	}
	t.Fatal("migration version 18 is not registered")
	return Migration{}
}

// assertFingerprintColumn checks the column exists as text and is
// documented.
func assertFingerprintColumn(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	got, ok := columnType(ctx, t, pool, "public", "alerts", "reevaluation_fingerprint")
	if !ok {
		t.Fatal("alerts.reevaluation_fingerprint missing after migration v18")
	}
	if got != "text" {
		t.Errorf("reevaluation_fingerprint type = %q, want %q", got, "text")
	}

	var comment string
	if err := pool.QueryRow(ctx, fingerprintCommentSQL).Scan(&comment); err != nil {
		t.Fatalf("read reevaluation_fingerprint comment: %v", err)
	}
	if !strings.Contains(comment, "SHA-256") {
		t.Errorf("reevaluation_fingerprint comment = %q, want it to describe the hash", comment)
	}
}

// TestMigrationV18_Registered verifies the migration is wired into the
// schema manager and will be reached by an upgrade.
func TestMigrationV18_Registered(t *testing.T) {
	sm := NewSchemaManager()
	if got := sm.LatestVersion(); got < 18 {
		t.Errorf("LatestVersion() = %d, want at least 18", got)
	}
	if desc := migrationV18(t).Description; desc == "" {
		t.Error("migration 18 has an empty description")
	}
}

// TestMigrationV18_FingerprintColumn verifies a fresh install carries the
// documented column, and that an upgrade from a pre-v18 datastore adds it
// with existing alerts left at NULL.
func TestMigrationV18_FingerprintColumn(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}
	assertFingerprintColumn(ctx, t, pool)

	// Rewind to a pre-v18 state holding an alert, then upgrade.
	if _, err := pool.Exec(ctx, `
		ALTER TABLE alerts DROP COLUMN reevaluation_fingerprint;

		INSERT INTO connections (name, host, database_name, username, owner_username)
		VALUES ('v18-conn', 'db.example.com', 'postgres', 'postgres', 'test-user');

		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status)
		SELECT 'anomaly', id, 'warning', 't', 'd', 'acknowledged'
		FROM connections WHERE name = 'v18-conn';

		DELETE FROM schema_version WHERE version >= 18;
	`); err != nil {
		t.Fatalf("rewind to a pre-v18 state: %v", err)
	}

	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("re-apply migrate: %v", err)
	}
	assertFingerprintColumn(ctx, t, pool)

	var nonNull int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alerts WHERE reevaluation_fingerprint IS NOT NULL`).Scan(&nonNull); err != nil {
		t.Fatalf("count fingerprints: %v", err)
	}
	if nonNull != 0 {
		t.Errorf("%d existing alerts gained a fingerprint, want 0", nonNull)
	}
}

// TestMigrationV18_ReportsStatementFailure drives the migration's error
// path by hiding the table it alters, inside a transaction that is rolled
// back afterwards.
func TestMigrationV18_ReportsStatementFailure(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			t.Logf("rollback failed: %v", err)
		}
	}()

	if _, err := tx.Exec(ctx, `ALTER TABLE alerts RENAME TO alerts_hidden`); err != nil {
		t.Fatalf("failed to hide alerts: %v", err)
	}

	err = migrationV18(t).Up(tx)
	if err == nil {
		t.Fatal("migration 18 succeeded without an alerts table")
	}
	if !strings.Contains(err.Error(), "alerts.reevaluation_fingerprint") {
		t.Errorf("migration 18 error = %q, want it to name the failure", err.Error())
	}
}
