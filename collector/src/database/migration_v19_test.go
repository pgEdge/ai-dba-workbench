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

// The v19 migration lets the alerts table hold system alerts, which
// concern the alerter itself and so have no connection; see GitHub issue
// #582. The SQL lives in named constants because the Codacy/Semgrep
// go_sql_rule-concat-sqli rule flags inline multi-line SQL.

const v19InsertConnectionSQL = `
	INSERT INTO connections (name, host, database_name, username, owner_username)
	VALUES ('v19-conn', 'db.example.com', 'postgres', 'postgres', 'test-user')
	RETURNING id
`

const v19InsertSystemAlertSQL = `
	INSERT INTO alerts (alert_type, connection_id, metric_name, severity,
	                    title, description, status)
	VALUES ('system', NULL, $1, 'warning', 'Provider failing', 'details', $2)
`

const v19InsertAlertSQL = `
	INSERT INTO alerts (alert_type, connection_id, severity, title,
	                    description, status)
	VALUES ($1, $2, 'warning', 'Some alert', 'details', 'active')
`

const v19ColumnCommentSQL = `
	SELECT COALESCE(col_description('alerts'::regclass, attnum), '')
	FROM pg_attribute
	WHERE attrelid = 'alerts'::regclass
	  AND attname = $1
`

// migrationV19 returns the registered version 19 migration.
func migrationV19(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 19 {
			return m
		}
	}
	t.Fatal("migration version 19 is not registered")
	return Migration{}
}

// TestMigrationV19_Registered verifies the migration is wired into the
// schema manager.
func TestMigrationV19_Registered(t *testing.T) {
	if got := NewSchemaManager().LatestVersion(); got < 19 {
		t.Errorf("LatestVersion() = %d, want at least 19", got)
	}
	if desc := migrationV19(t).Description; desc == "" {
		t.Error("migration 19 has an empty description")
	}
}

// v19Pool migrates a clean schema and returns a pool, or nil when no test
// database is configured.
func v19Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return nil
	}
	t.Cleanup(pool.Close)
	t.Cleanup(conn.Release)

	cleanupTestSchema(t, pool)
	t.Cleanup(func() { cleanupTestSchema(t, pool) })

	if err := NewSchemaManager().Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}
	return pool
}

// TestMigrationV19_SystemAlertShape verifies that a system alert with no
// connection is accepted, that the connection/type pairing is enforced in
// both directions, that other alert types still need a connection, and
// that only one open system alert may exist per key.
func TestMigrationV19_SystemAlertShape(t *testing.T) {
	ctx := context.Background()
	pool := v19Pool(t)
	if pool == nil {
		return
	}

	var connID int
	if err := pool.QueryRow(ctx, v19InsertConnectionSQL).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	if _, err := pool.Exec(ctx, v19InsertSystemAlertSQL,
		"llm_provider_health.tier2.openai", "active"); err != nil {
		t.Fatalf("insert system alert: %v", err)
	}

	// A second open alert with the same key is refused ...
	if _, err := pool.Exec(ctx, v19InsertSystemAlertSQL,
		"llm_provider_health.tier2.openai", "acknowledged"); err == nil {
		t.Error("second open system alert for one key succeeded, want a unique violation")
	}
	// ... but a cleared one, or an open one for another key, is not.
	if _, err := pool.Exec(ctx, v19InsertSystemAlertSQL,
		"llm_provider_health.tier2.openai", "cleared"); err != nil {
		t.Errorf("cleared system alert for an open key: %v", err)
	}
	if _, err := pool.Exec(ctx, v19InsertSystemAlertSQL,
		"llm_provider_health.tier3.openai", "active"); err != nil {
		t.Errorf("system alert for a second key: %v", err)
	}

	tests := []struct {
		name      string
		alertType string
		connID    any
		wantErr   bool
	}{
		{"threshold with connection", "threshold", connID, false},
		{"anomaly with connection", "anomaly", connID, false},
		{"connection alert with connection", "connection", connID, false},
		{"threshold without connection", "threshold", nil, true},
		{"system with connection", "system", connID, true},
		{"unknown alert type", "bogus", connID, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, v19InsertAlertSQL, tt.alertType, tt.connID)
			if (err != nil) != tt.wantErr {
				t.Errorf("insert error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}

	for col, want := range map[string]string{
		"connection_id": "NULL exactly when alert_type is system",
		"alert_type":    "system",
	} {
		var comment string
		if err := pool.QueryRow(ctx, v19ColumnCommentSQL, col).Scan(&comment); err != nil {
			t.Fatalf("read %s comment: %v", col, err)
		}
		if !strings.Contains(comment, want) {
			t.Errorf("alerts.%s comment = %q, want it to mention %q", col, comment, want)
		}
	}
}

// TestMigrationV19_UpgradeKeepsExistingAlerts rewinds to a pre-v19 shape
// whose alert_type CHECK carries a non-default name, holding an existing
// alert, then re-applies the migration and checks the old constraint was
// replaced and the row survived.
func TestMigrationV19_UpgradeKeepsExistingAlerts(t *testing.T) {
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
		t.Fatalf("baseline migrate: %v", err)
	}

	var connID int
	if err := pool.QueryRow(ctx, v19InsertConnectionSQL).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	if _, err := pool.Exec(ctx, v19InsertAlertSQL, "anomaly", connID); err != nil {
		t.Fatalf("insert existing alert: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		DROP INDEX IF EXISTS idx_alerts_system_open;
		ALTER TABLE alerts DROP CONSTRAINT alerts_system_connection_check;
		ALTER TABLE alerts DROP CONSTRAINT alerts_alert_type_check;
		ALTER TABLE alerts ADD CONSTRAINT legacy_alert_type_name
			CHECK (alert_type IN ('threshold', 'anomaly', 'connection'));
		ALTER TABLE alerts ALTER COLUMN connection_id SET NOT NULL;
		DELETE FROM schema_version WHERE version >= 19;
	`); err != nil {
		t.Fatalf("rewind to a pre-v19 state: %v", err)
	}

	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("re-apply migrate: %v", err)
	}

	var legacy int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conrelid = 'alerts'::regclass AND conname = 'legacy_alert_type_name'
	`).Scan(&legacy); err != nil {
		t.Fatalf("count legacy constraint: %v", err)
	}
	if legacy != 0 {
		t.Error("the pre-v19 alert_type CHECK survived the migration")
	}

	var kept int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alerts WHERE connection_id = $1`, connID).Scan(&kept); err != nil {
		t.Fatalf("count existing alerts: %v", err)
	}
	if kept != 1 {
		t.Errorf("existing alerts after upgrade = %d, want 1", kept)
	}

	if _, err := pool.Exec(ctx, v19InsertSystemAlertSQL,
		"llm_provider_health.tier3.anthropic", "active"); err != nil {
		t.Errorf("insert system alert after upgrade: %v", err)
	}

	// Cascading delete of the connection still removes its alerts.
	if _, err := pool.Exec(ctx, `DELETE FROM connections WHERE id = $1`, connID); err != nil {
		t.Fatalf("delete connection: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alerts WHERE alert_type <> 'system'`).Scan(&kept); err != nil {
		t.Fatalf("count remaining alerts: %v", err)
	}
	if kept != 0 {
		t.Errorf("connection alerts after deleting the connection = %d, want 0", kept)
	}
}

// TestMigrationV19_ReportsStatementFailure drives the migration's error
// path by hiding the alerts table inside a rolled-back transaction.
func TestMigrationV19_ReportsStatementFailure(t *testing.T) {
	ctx := context.Background()
	pool := v19Pool(t)
	if pool == nil {
		return
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

	err = migrationV19(t).Up(tx)
	if err == nil {
		t.Fatal("migration 19 succeeded without an alerts table")
	}
	if !strings.Contains(err.Error(), "system alerts") {
		t.Errorf("migration 19 error = %q, want it to name the failure", err.Error())
	}
}
