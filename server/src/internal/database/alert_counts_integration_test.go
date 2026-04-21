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

// alertCountsTestSchema mirrors the minimum columns GetAlertCounts
// touches. The filterless and filtered branches both read from the
// alerts table and group by connection_id.
const alertCountsTestSchema = `
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE alerts (
    id BIGSERIAL PRIMARY KEY,
    alert_type TEXT NOT NULL,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    severity TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    status TEXT NOT NULL,
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const alertCountsTestTeardown = `
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
`

// newAlertCountsTestDatastore wires up a *Datastore against the
// TEST_AI_WORKBENCH_SERVER Postgres instance with just the tables the
// GetAlertCounts path needs. Tests skip when no DB is configured.
func newAlertCountsTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping alert counts integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	if _, err := pool.Exec(ctx, alertCountsTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create alert counts test schema: %v", err)
	}

	ds := NewTestDatastore(pool)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), alertCountsTestTeardown); err != nil {
			t.Logf("alert counts teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

func insertAlertCountsConn(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var id int
	err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name) VALUES ($1) RETURNING id`,
		name).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert connection %q: %v", name, err)
	}
	return id
}

func insertAlertCountsAlert(t *testing.T, pool *pgxpool.Pool, connID int, status string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO alerts (
			alert_type, connection_id, severity, title, description, status
		) VALUES ('threshold', $1, 'warning', 'test', 'desc', $2)
	`, connID, status)
	if err != nil {
		t.Fatalf("Failed to insert alert: %v", err)
	}
}

// TestGetAlertCounts_NoFilter exercises the branch that queries every
// active alert in the database. It confirms the unfiltered total and
// the per-connection breakdown match the inserted rows.
func TestGetAlertCounts_NoFilter(t *testing.T) {
	ds, pool, cleanup := newAlertCountsTestDatastore(t)
	defer cleanup()

	connA := insertAlertCountsConn(t, pool, "a")
	connB := insertAlertCountsConn(t, pool, "b")
	insertAlertCountsAlert(t, pool, connA, "active")
	insertAlertCountsAlert(t, pool, connA, "active")
	insertAlertCountsAlert(t, pool, connB, "active")
	insertAlertCountsAlert(t, pool, connB, "cleared") // must be excluded

	result, err := ds.GetAlertCounts(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetAlertCounts: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("Total = %d, want 3", result.Total)
	}
	if result.ByServer[connA] != 2 {
		t.Errorf("ByServer[%d] = %d, want 2", connA, result.ByServer[connA])
	}
	if result.ByServer[connB] != 1 {
		t.Errorf("ByServer[%d] = %d, want 1", connB, result.ByServer[connB])
	}
}

// TestGetAlertCounts_WithFilter exercises the branch that restricts the
// counts to a caller-supplied allow-list. The filter must exclude alerts
// for connections not in the slice even if their status is 'active'.
func TestGetAlertCounts_WithFilter(t *testing.T) {
	ds, pool, cleanup := newAlertCountsTestDatastore(t)
	defer cleanup()

	connA := insertAlertCountsConn(t, pool, "a")
	connB := insertAlertCountsConn(t, pool, "b")
	insertAlertCountsAlert(t, pool, connA, "active")
	insertAlertCountsAlert(t, pool, connA, "active")
	insertAlertCountsAlert(t, pool, connB, "active")

	// Allow-list only connection A.
	result, err := ds.GetAlertCounts(context.Background(), []int{connA})
	if err != nil {
		t.Fatalf("GetAlertCounts: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
	if result.ByServer[connA] != 2 {
		t.Errorf("ByServer[%d] = %d, want 2", connA, result.ByServer[connA])
	}
	if _, ok := result.ByServer[connB]; ok {
		t.Errorf("ByServer unexpectedly contains filtered connection %d", connB)
	}
}
