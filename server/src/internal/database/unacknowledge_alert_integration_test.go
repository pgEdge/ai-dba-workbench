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

// unackAlertTestSchema creates the minimum set of tables the
// UnacknowledgeAlert and GetAlerts paths touch. It mirrors the
// production schema in collector/src/database/schema.go, limited to
// columns the exercised code paths actually reference. alert_rules is
// present so the LATERAL/LEFT JOIN on r.metric_unit in GetAlerts
// resolves without errors.
const unackAlertTestSchema = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE alert_rules (
    id BIGSERIAL PRIMARY KEY,
    metric_unit TEXT
);

CREATE TABLE alerts (
    id BIGSERIAL PRIMARY KEY,
    alert_type TEXT NOT NULL CHECK (alert_type IN ('threshold', 'anomaly', 'connection')),
    rule_id BIGINT REFERENCES alert_rules(id) ON DELETE SET NULL,
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
    anomaly_details JSONB,
    ai_analysis TEXT,
    ai_analysis_metric_value REAL
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

const unackAlertTestTeardown = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
`

// newUnackAlertTestDatastore wires up a *Datastore against the
// TEST_AI_WORKBENCH_SERVER Postgres instance with only the tables the
// tested code paths need. The caller receives a cleanup that drops the
// schema and closes the pool.
func newUnackAlertTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping unacknowledge alert integration test")
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

	if _, err := pool.Exec(ctx, unackAlertTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create unack alert test schema: %v", err)
	}

	ds := NewTestDatastore(pool)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), unackAlertTestTeardown); err != nil {
			t.Logf("unack alert teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// insertUnackTestConnection inserts a connection row and returns its id.
func insertUnackTestConnection(t *testing.T, pool *pgxpool.Pool, name string) int {
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

// insertUnackAcknowledgedAlert creates an alert in the acknowledged
// state with a matching alert_acknowledgments row, matching the shape
// the UI sees after a user acknowledges an alert.
func insertUnackAcknowledgedAlert(t *testing.T, pool *pgxpool.Pool, connID int) int64 {
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

	if _, err := pool.Exec(ctx, `
		INSERT INTO alert_acknowledgments (
			alert_id, acknowledged_by, acknowledge_type, message, false_positive
		) VALUES ($1, 'tester', 'acknowledge', 'reviewing', FALSE)
	`, alertID); err != nil {
		t.Fatalf("Failed to insert acknowledgment row: %v", err)
	}

	return alertID
}

// TestUnacknowledgeAlert_ClearsAcknowledgment verifies the fix for the
// parallel server-side bug in issue #64: UnacknowledgeAlert must not
// only flip alerts.status back to 'active', it must also remove the
// alert_acknowledgments rows referenced by the LATERAL join in
// GetAlerts. Without this, the UI keeps rendering the alert as
// acknowledged even though the status column says active.
func TestUnacknowledgeAlert_ClearsAcknowledgment(t *testing.T) {
	ds, pool, cleanup := newUnackAlertTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertUnackTestConnection(t, pool, "unack-test")
	alertID := insertUnackAcknowledgedAlert(t, pool, connID)

	if err := ds.UnacknowledgeAlert(ctx, alertID); err != nil {
		t.Fatalf("UnacknowledgeAlert returned error: %v", err)
	}

	// Verify alerts.status is back to 'active'.
	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM alerts WHERE id = $1`, alertID).Scan(&status); err != nil {
		t.Fatalf("Failed to read alert status: %v", err)
	}
	if status != "active" {
		t.Errorf("status after unacknowledge = %q, want \"active\"", status)
	}

	// Verify alert_acknowledgments row is gone.
	var ackCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alert_acknowledgments WHERE alert_id = $1`,
		alertID).Scan(&ackCount); err != nil {
		t.Fatalf("Failed to count ack rows: %v", err)
	}
	if ackCount != 0 {
		t.Errorf("ack rows after unacknowledge = %d, want 0", ackCount)
	}

	// Verify the GetAlerts path (which is what the UI actually
	// consumes) returns acknowledged_at = nil. This is the end-to-end
	// behavior issue #64 cares about.
	active := "active"
	result, err := ds.GetAlerts(ctx, AlertListFilter{Status: &active})
	if err != nil {
		t.Fatalf("GetAlerts returned error: %v", err)
	}
	var found *Alert
	for i := range result.Alerts {
		if result.Alerts[i].ID == alertID {
			found = &result.Alerts[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("GetAlerts did not return alert %d", alertID)
	}
	if found.AcknowledgedAt != nil {
		t.Errorf("GetAlerts returned acknowledged_at = %v, want nil "+
			"(the stale ack row would leave the UI showing the alert "+
			"as acknowledged)", *found.AcknowledgedAt)
	}
	if found.Status != "active" {
		t.Errorf("GetAlerts returned status = %q, want \"active\"",
			found.Status)
	}
}

// TestUnacknowledgeAlert_NotAcknowledgedErrors verifies
// UnacknowledgeAlert rejects alerts that aren't in the acknowledged
// state, so we don't clobber ack history for alerts in unrelated
// states.
func TestUnacknowledgeAlert_NotAcknowledgedErrors(t *testing.T) {
	ds, pool, cleanup := newUnackAlertTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertUnackTestConnection(t, pool, "already-active")

	var alertID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO alerts (
			alert_type, connection_id, severity, title, description,
			status, triggered_at
		) VALUES (
			'threshold', $1, 'warning', 'already active',
			'already active', 'active', CURRENT_TIMESTAMP
		) RETURNING id
	`, connID).Scan(&alertID)
	if err != nil {
		t.Fatalf("Failed to insert alert: %v", err)
	}

	if err := ds.UnacknowledgeAlert(ctx, alertID); err == nil {
		t.Errorf("UnacknowledgeAlert on already-active alert returned nil; want error")
	}
}
