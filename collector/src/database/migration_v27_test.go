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

// The v27 migration masks passwords in the descriptions of connection
// alerts raised before the alerter masked them, as described in GitHub
// issue #679.

// migrationV27 returns the registered version 27 migration.
func migrationV27(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 27 {
			return m
		}
	}
	t.Fatal("migration version 27 is not registered")
	return Migration{}
}

// TestMigrationV27_Registered verifies the migration is wired into the
// schema manager and will be reached by an upgrade.
func TestMigrationV27_Registered(t *testing.T) {
	if got := NewSchemaManager().LatestVersion(); got < 27 {
		t.Errorf("LatestVersion() = %d, want at least 27", got)
	}
	if desc := migrationV27(t).Description; desc == "" {
		t.Error("migration 27 has an empty description")
	}
}

// migrateV27TestSchema brings a clean schema up to the latest version
// and returns a pool on it with a cleanup function, or a nil pool when
// no test database is configured.
func migrateV27TestSchema(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return nil, func() {}
	}
	cleanupTestSchema(t, pool)
	if err := NewSchemaManager().Migrate(conn); err != nil {
		conn.Release()
		cleanupTestSchema(t, pool)
		pool.Close()
		t.Fatalf("failed to migrate test schema: %v", err)
	}
	conn.Release()
	return pool, func() {
		cleanupTestSchema(t, pool)
		pool.Close()
	}
}

// runMigrationV27 runs the migration in its own transaction and returns
// its error, after rolling back on failure.
func runMigrationV27(t *testing.T, pool *pgxpool.Pool, setup string) error {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	if setup != "" {
		if _, err := tx.Exec(ctx, setup); err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				t.Logf("rollback failed: %v", rbErr)
			}
			t.Fatalf("failed to set up: %v", err)
		}
	}
	if err := migrationV27(t).Up(tx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			t.Logf("rollback failed: %v", rbErr)
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit migration 27: %v", err)
	}
	return nil
}

// insertV27Alert adds an alert of the given type and status with the
// given description to connID and returns its id.
func insertV27Alert(t *testing.T, pool *pgxpool.Pool, connID int, alertType, status, description string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status)
		VALUES ($1, $2, 'critical', 'Connection error', $3, $4) RETURNING id`,
		alertType, connID, description, status).Scan(&id); err != nil {
		t.Fatalf("failed to insert alert: %v", err)
	}
	return id
}

// TestMigrationV27_MasksConnectionAlerts checks that every connection
// alert holding a password is masked, whatever its status, and that
// other alerts and masked text are left alone.
func TestMigrationV27_MasksConnectionAlerts(t *testing.T) {
	pool, cleanup := migrateV27TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}
	ctx := context.Background()

	var connID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO connections (name, host, port, database_name, username, owner_username)
		VALUES ('v27', '192.0.2.1', 5432, 'db', 'app', 'owner') RETURNING id`).Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}

	raw := `could not connect: host=192.0.2.1 password=example-secret dbname=db`
	masked := `could not connect: host=192.0.2.1 password=******** dbname=db`
	want := map[int64]string{}
	for _, status := range []string{"active", "acknowledged", "cleared"} {
		want[insertV27Alert(t, pool, connID, "connection", status, raw)] = masked
	}
	want[insertV27Alert(t, pool, connID, "connection", "cleared", "connection refused")] = "connection refused"
	want[insertV27Alert(t, pool, connID, "threshold", "cleared", raw)] = raw

	if err := runMigrationV27(t, pool, ""); err != nil {
		t.Fatalf("migration 27 failed: %v", err)
	}
	for id, w := range want {
		var got string
		if err := pool.QueryRow(ctx, `SELECT description FROM alerts WHERE id = $1`, id).Scan(&got); err != nil {
			t.Fatalf("failed to read alert %d: %v", id, err)
		}
		if got != w {
			t.Errorf("alert %d: description = %q, want %q", id, got, w)
		}
	}
}

// TestMigrationV27_ReportsFailures drives the migration's error paths:
// no alerts table, a description that cannot be scanned, and an update
// that is refused.
func TestMigrationV27_ReportsFailures(t *testing.T) {
	pool, cleanup := migrateV27TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}
	ctx := context.Background()

	var connID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO connections (name, host, port, database_name, username, owner_username)
		VALUES ('v27-fail', '192.0.2.1', 5432, 'db', 'app', 'owner') RETURNING id`).Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}
	insertV27Alert(t, pool, connID, "connection", "cleared", `password=example-secret`)

	for name, tc := range map[string]struct{ setup, want string }{
		"no table": {`ALTER TABLE alerts RENAME TO alerts_hidden`, "failed to read connection alerts"},
		"NULL description": {`ALTER TABLE alerts ALTER COLUMN description DROP NOT NULL;
			INSERT INTO alerts (alert_type, connection_id, severity, title, status)
			SELECT 'connection', connection_id, 'critical', 't', 'cleared' FROM alerts LIMIT 1`,
			"failed to read connection alerts"},
		"refused update": {`CREATE FUNCTION refuse_v27() RETURNS trigger LANGUAGE plpgsql AS
			$$BEGIN RAISE EXCEPTION 'refused'; END$$;
			CREATE TRIGGER refuse_v27 BEFORE UPDATE ON alerts
			FOR EACH ROW EXECUTE FUNCTION refuse_v27()`, "failed to mask connection alert"},
	} {
		err := runMigrationV27(t, pool, tc.setup)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to contain %q", name, err, tc.want)
		}
	}
}
