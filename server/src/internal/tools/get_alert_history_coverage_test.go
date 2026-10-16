/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Result-rendering coverage for get_alert_history: rows, empty results
// and scan failures in both single-connection and all-connections modes.
// TestGetAlertHistoryTool_Coverage in rbac_tools_coverage_test.go covers
// argument validation and the access checks.

// alertHistorySchema adds the alert tables to the schema built by
// newToolsTestPool. alerts.description is nullable, unlike production,
// so that a NULL can be seeded to make the row scan fail.
const alertHistorySchema = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;

CREATE TABLE alerts (
    id BIGSERIAL PRIMARY KEY,
    alert_type TEXT NOT NULL DEFAULT 'threshold',
    connection_id INTEGER,
    rule_id BIGINT,
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    severity TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    metric_name TEXT,
    metric_value DOUBLE PRECISION,
    threshold_value DOUBLE PRECISION,
    operator TEXT,
    status TEXT NOT NULL,
    cleared_at TIMESTAMPTZ
);

CREATE TABLE alert_acknowledgments (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL,
    acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_by TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    false_positive BOOLEAN NOT NULL DEFAULT FALSE
);
`

// newAlertHistoryPool returns a pool on the tools test schema plus
// alertHistorySchema, and drops both when the test ends.
func newAlertHistoryPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, _, cleanup := newToolsTestPool(t)
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			"DROP TABLE IF EXISTS alert_acknowledgments, alerts CASCADE"); err != nil {
			t.Logf("alert history teardown failed: %v", err)
		}
		cleanup()
	})
	if _, err := pool.Exec(context.Background(), alertHistorySchema); err != nil {
		t.Fatalf("failed to create alert history schema: %v", err)
	}
	return pool
}

// execSQL runs a statement on pool and fails the test on error.
func execSQL(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q failed: %v", sql, err)
	}
}

func TestGetAlertHistoryTool_Results(t *testing.T) {
	pool := newAlertHistoryPool(t)
	connID := seedBaselineConnection(t, pool, "history-conn", true, "")
	emptyID := seedBaselineConnection(t, pool, "quiet-conn", true, "")

	// One cleared, acknowledged alert with every optional column set and
	// one active alert with none of them.
	var clearedID int64
	if err := pool.QueryRow(context.Background(), `
        INSERT INTO alerts (connection_id, rule_id, triggered_at, severity, title,
            description, metric_name, metric_value, threshold_value, operator,
            status, cleared_at)
        VALUES ($1, 3, NOW() - INTERVAL '2 hours', 'warning', 'High	backends',
            'Backend count high', 'numbackends', 120, 100, '>',
            'cleared', NOW() - INTERVAL '1 hour')
        RETURNING id`, connID).Scan(&clearedID); err != nil {
		t.Fatalf("failed to seed cleared alert: %v", err)
	}
	execSQL(t, pool, `
        INSERT INTO alert_acknowledgments (alert_id, acknowledged_by, message, false_positive)
        VALUES ($1, 'dba', 'Known
issue', true)`, clearedID)
	execSQL(t, pool, `
        INSERT INTO alerts (connection_id, severity, title, description, status)
        VALUES ($1, 'critical', 'Server down', 'Unreachable', 'active')`, connID)

	conn := float64(connID)
	empty := float64(emptyID)
	tool := GetAlertHistoryTool(pool, testRBACChecker(t), sharedLister(connID))
	runToolCases(t, tool, []toolCase{
		{name: "single connection rows", args: map[string]any{"connection_id": conn},
			want: "(2 rows)"},
		{name: "single connection escapes notes", args: map[string]any{"connection_id": conn},
			want: `Known\nissue`},
		{name: "single connection filters", args: map[string]any{
			"connection_id": conn, "rule_id": float64(3), "metric_name": "numbackends",
			"status": "cleared", "time_start": "1d"},
			want: "(1 rows)"},
		{name: "single connection no active alerts", args: map[string]any{
			"connection_id": empty, "status": "active"},
			want: "No active alerts for connection"},
		{name: "single connection no alerts", args: map[string]any{"connection_id": empty},
			want: "No alerts found for connection"},
		{name: "all connections rows", want: "(2 rows)"},
		{name: "all connections names the connection", want: "history-conn"},
		{name: "restricted caller sees shared rows", args: map[string]any{
			"__context": restrictedUserContext(), "status": "acknowledged"},
			want: "No alerts found across accessible connections with status='acknowledged'"},
		{name: "all connections no active alerts", args: map[string]any{
			"status": "active", "rule_id": float64(999)},
			want: "No active alerts across accessible connections."},
	})

	// A NULL description cannot be scanned into a string.
	execSQL(t, pool, `
        INSERT INTO alerts (connection_id, severity, title, description, status)
        VALUES ($1, 'info', 'Broken', NULL, 'active')`, connID)
	runToolCases(t, tool, []toolCase{
		{name: "single connection scan failure", args: map[string]any{"connection_id": conn},
			wantErr: true, want: "Failed to scan row"},
		{name: "all connections scan failure", wantErr: true, want: "Failed to scan row"},
	})
}
