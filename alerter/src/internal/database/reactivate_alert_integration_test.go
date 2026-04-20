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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// reactivateAlertTestSchema creates the minimum set of tables required
// to exercise ReactivateAlert. The alerts and alert_acknowledgments
// definitions mirror the production migrations in
// collector/src/database/schema.go (only the columns and constraints
// referenced by the reactivation path are included). The connections
// table exists only to satisfy the alerts.connection_id foreign key.
const reactivateAlertTestSchema = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE alerts (
    id BIGSERIAL PRIMARY KEY,
    alert_type TEXT NOT NULL CHECK (alert_type IN ('threshold', 'anomaly', 'connection')),
    rule_id BIGINT,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    probe_name TEXT,
    metric_name TEXT,
    metric_value REAL,
    threshold_value REAL,
    operator TEXT,
    severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    object_name TEXT,
    correlation_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('active', 'cleared', 'acknowledged')),
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    cleared_at TIMESTAMPTZ,
    last_updated TIMESTAMPTZ,
    anomaly_score REAL,
    anomaly_details JSONB
);

CREATE TABLE alert_acknowledgments (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    acknowledged_by TEXT NOT NULL,
    acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    acknowledge_type TEXT NOT NULL CHECK (acknowledge_type IN ('acknowledge', 'dismiss', 'false_positive')),
    message TEXT NOT NULL DEFAULT '',
    false_positive BOOLEAN NOT NULL DEFAULT FALSE
);
`

const reactivateAlertTestTeardown = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
`

// newReactivateAlertTestDatastore returns a Datastore wired to the test
// database with the minimal alerts/acknowledgments schema installed.
// The test is skipped when no test database is available.
func newReactivateAlertTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping reactivate alert integration test")
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

	if _, err := pool.Exec(ctx, reactivateAlertTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create reactivate alert test schema: %v", err)
	}

	ds := &Datastore{pool: pool, config: nil}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), reactivateAlertTestTeardown); err != nil {
			t.Logf("reactivate alert teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// insertReactivateTestConnection inserts a connection and returns its id.
func insertReactivateTestConnection(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var id int
	err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name, enabled) VALUES ($1, TRUE) RETURNING id`,
		name).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert connection %q: %v", name, err)
	}
	return id
}

// insertAcknowledgedAlert inserts an alert in the 'acknowledged' state
// with a matching alert_acknowledgments row. It returns the alert id.
// This models the production state that issue #64 describes: an alert
// that was acknowledged, then had its severity change, which should
// trigger reactivation on the alerter side.
func insertAcknowledgedAlert(t *testing.T, pool *pgxpool.Pool, connID int) int64 {
	t.Helper()

	ctx := context.Background()
	var alertID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO alerts (
			alert_type, connection_id, severity, title, description,
			status, triggered_at
		) VALUES (
			'threshold', $1, 'warning', 'test alert', 'test description',
			'acknowledged', CURRENT_TIMESTAMP
		) RETURNING id
	`, connID).Scan(&alertID)
	if err != nil {
		t.Fatalf("Failed to insert acknowledged alert: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO alert_acknowledgments (
			alert_id, acknowledged_by, acknowledge_type, message, false_positive
		) VALUES ($1, 'tester', 'acknowledge', 'reviewing', FALSE)
	`, alertID)
	if err != nil {
		t.Fatalf("Failed to insert acknowledgment row: %v", err)
	}

	return alertID
}

// insertActiveAlert inserts an alert in the 'active' state with no
// acknowledgment history. Used to verify ReactivateAlert is a no-op when
// the alert was never acknowledged.
func insertActiveAlert(t *testing.T, pool *pgxpool.Pool, connID int) int64 {
	t.Helper()

	var alertID int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO alerts (
			alert_type, connection_id, severity, title, description,
			status, triggered_at
		) VALUES (
			'threshold', $1, 'warning', 'test active alert',
			'test description', 'active', CURRENT_TIMESTAMP
		) RETURNING id
	`, connID).Scan(&alertID)
	if err != nil {
		t.Fatalf("Failed to insert active alert: %v", err)
	}
	return alertID
}

// alertStatusAndAckCount reads the current status of an alert and the
// number of acknowledgment rows referencing it. This is the shape the
// server's LATERAL join in ListAlerts cares about: status = 'active'
// plus zero ack rows means the UI will show the alert as active with no
// acknowledged_at.
func alertStatusAndAckCount(t *testing.T, pool *pgxpool.Pool, alertID int64) (string, int, *time.Time) {
	t.Helper()

	ctx := context.Background()
	var status string
	var lastUpdated *time.Time
	err := pool.QueryRow(ctx,
		`SELECT status, last_updated FROM alerts WHERE id = $1`,
		alertID).Scan(&status, &lastUpdated)
	if err != nil {
		t.Fatalf("Failed to read alert %d: %v", alertID, err)
	}

	var ackCount int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alert_acknowledgments WHERE alert_id = $1`,
		alertID).Scan(&ackCount)
	if err != nil {
		t.Fatalf("Failed to count ack rows for alert %d: %v", alertID, err)
	}

	return status, ackCount, lastUpdated
}

// TestReactivateAlert_ClearsAcknowledgment verifies the fix for issue
// #64: when a severity change on an acknowledged alert triggers
// ReactivateAlert, the alert_acknowledgments row must be removed so the
// LATERAL join in the server's ListAlerts query no longer reports a
// stale acknowledged_at.
func TestReactivateAlert_ClearsAcknowledgment(t *testing.T) {
	ds, pool, cleanup := newReactivateAlertTestDatastore(t)
	defer cleanup()

	connID := insertReactivateTestConnection(t, pool, "reactivate-test")
	alertID := insertAcknowledgedAlert(t, pool, connID)

	// Sanity check: the test fixture really does start in the
	// acknowledged state with one ack row. If this fails, the bug under
	// test isn't being exercised.
	status, ackCount, _ := alertStatusAndAckCount(t, pool, alertID)
	if status != "acknowledged" {
		t.Fatalf("precondition: expected status 'acknowledged', got %q", status)
	}
	if ackCount != 1 {
		t.Fatalf("precondition: expected 1 ack row, got %d", ackCount)
	}

	if err := ds.ReactivateAlert(context.Background(), alertID); err != nil {
		t.Fatalf("ReactivateAlert returned error: %v", err)
	}

	status, ackCount, lastUpdated := alertStatusAndAckCount(t, pool, alertID)
	if status != "active" {
		t.Errorf("status after reactivate = %q, want \"active\"", status)
	}
	if ackCount != 0 {
		t.Errorf("ack rows after reactivate = %d, want 0 "+
			"(stale ack row would make the UI show the alert as acknowledged)",
			ackCount)
	}
	if lastUpdated == nil {
		t.Errorf("last_updated after reactivate is nil; " +
			"the UI relies on this timestamp to surface the reactivation")
	}
}

// TestReactivateAlert_NeverAcknowledgedIsNoOp verifies that calling
// ReactivateAlert on an alert that is already active (and therefore has
// no ack rows to clear) does not error and does not touch any rows.
// The guard on status = 'acknowledged' in the UPDATE means the DELETE
// must be skipped so we don't accidentally wipe future ack rows that
// might be inserted by a concurrent acknowledgment.
func TestReactivateAlert_NeverAcknowledgedIsNoOp(t *testing.T) {
	ds, pool, cleanup := newReactivateAlertTestDatastore(t)
	defer cleanup()

	connID := insertReactivateTestConnection(t, pool, "never-acked")
	alertID := insertActiveAlert(t, pool, connID)

	if err := ds.ReactivateAlert(context.Background(), alertID); err != nil {
		t.Fatalf("ReactivateAlert on already-active alert returned error: %v", err)
	}

	status, ackCount, lastUpdated := alertStatusAndAckCount(t, pool, alertID)
	if status != "active" {
		t.Errorf("status = %q, want \"active\"", status)
	}
	if ackCount != 0 {
		t.Errorf("ack rows = %d, want 0", ackCount)
	}
	// The UPDATE was gated on status = 'acknowledged', so it matched
	// zero rows and last_updated must remain nil.
	if lastUpdated != nil {
		t.Errorf("last_updated = %v, want nil "+
			"(no-op reactivate must not touch the row)", *lastUpdated)
	}
}

// TestReactivateAlert_ActivePreservesExistingAcknowledgments verifies
// that when ReactivateAlert is called on an alert that is NOT in the
// acknowledged state, any acknowledgment rows referencing that alert
// are preserved. A history row from a prior acknowledge/unacknowledge
// cycle must not be destroyed by a subsequent no-op reactivation.
func TestReactivateAlert_ActivePreservesExistingAcknowledgments(t *testing.T) {
	ds, pool, cleanup := newReactivateAlertTestDatastore(t)
	defer cleanup()

	connID := insertReactivateTestConnection(t, pool, "preserve-ack-history")
	// Insert an active alert.
	alertID := insertActiveAlert(t, pool, connID)

	// Simulate an historical ack row that somehow survived on an active
	// alert (for example because a future feature retains audit history
	// even after reactivation). The fix must not regress into deleting
	// these rows for alerts that are already active.
	_, err := pool.Exec(context.Background(), `
		INSERT INTO alert_acknowledgments (
			alert_id, acknowledged_by, acknowledge_type, message, false_positive
		) VALUES ($1, 'historical', 'acknowledge', 'past ack', FALSE)
	`, alertID)
	if err != nil {
		t.Fatalf("Failed to insert historical ack row: %v", err)
	}

	if err := ds.ReactivateAlert(context.Background(), alertID); err != nil {
		t.Fatalf("ReactivateAlert returned error: %v", err)
	}

	// Status unchanged, historical ack preserved.
	status, ackCount, _ := alertStatusAndAckCount(t, pool, alertID)
	if status != "active" {
		t.Errorf("status = %q, want \"active\"", status)
	}
	if ackCount != 1 {
		t.Errorf("historical ack rows = %d, want 1 "+
			"(no-op reactivate must not delete unrelated ack history)",
			ackCount)
	}
}
