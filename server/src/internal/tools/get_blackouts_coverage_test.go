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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Result-rendering coverage for get_blackouts: blackouts and schedules
// at every scope, empty results, and query and scan failures, in both
// single-connection and all-connections modes.
// TestGetBlackoutsTool_Coverage in rbac_tools_coverage_test.go covers
// argument validation and the access checks.

// blackoutsSchema adds the cluster and blackout tables to the schema
// built by newToolsTestPool. blackouts.reason and blackout_schedules.name
// are nullable, unlike production, so that a NULL can be seeded to make
// the row scan fail.
const blackoutsSchema = `
DROP TABLE IF EXISTS blackout_schedules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;

CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    group_id INTEGER
);

CREATE TABLE blackouts (
    id BIGSERIAL PRIMARY KEY,
    scope TEXT NOT NULL,
    connection_id INTEGER,
    cluster_id INTEGER,
    group_id INTEGER,
    reason TEXT,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    created_by TEXT NOT NULL DEFAULT 'admin'
);

CREATE TABLE blackout_schedules (
    id BIGSERIAL PRIMARY KEY,
    scope TEXT NOT NULL,
    connection_id INTEGER,
    cluster_id INTEGER,
    group_id INTEGER,
    name TEXT,
    cron_expression TEXT NOT NULL,
    duration_minutes INTEGER NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    reason TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

// newBlackoutsPool returns a pool on the tools test schema plus
// blackoutsSchema, and drops both when the test ends.
func newBlackoutsPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, _, cleanup := newToolsTestPool(t)
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			"DROP TABLE IF EXISTS blackout_schedules, blackouts, clusters CASCADE"); err != nil {
			t.Logf("blackouts teardown failed: %v", err)
		}
		cleanup()
	})
	if _, err := pool.Exec(context.Background(), blackoutsSchema); err != nil {
		t.Fatalf("failed to create blackouts schema: %v", err)
	}
	return pool
}

// seedBlackoutFixtures puts a connection in a cluster and group, and
// gives it one active and one past blackout plus a schedule at each
// scope. It returns the connection's ID and that of a connection with
// nothing but estate-wide entries.
func seedBlackoutFixtures(t *testing.T, pool *pgxpool.Pool) (int, int) {
	t.Helper()
	connID := seedBaselineConnection(t, pool, "blackout-conn", true, "")
	quietID := seedBaselineConnection(t, pool, "quiet-conn", true, "")
	execSQL(t, pool, "INSERT INTO clusters (id, group_id) VALUES (10, 20)")
	execSQL(t, pool, "UPDATE connections SET cluster_id = 10 WHERE id = $1", connID)
	execSQL(t, pool, `
        INSERT INTO blackouts (scope, connection_id, cluster_id, group_id, reason, start_time, end_time)
        VALUES ('server', $1, NULL, NULL, 'Patch	window', NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 hour'),
               ('cluster', NULL, 10, NULL, 'Cluster upgrade', NOW() - INTERVAL '3 days', NOW() - INTERVAL '2 days'),
               ('group', NULL, NULL, 20, 'Group move', NOW() - INTERVAL '5 days', NOW() - INTERVAL '4 days')`,
		connID)
	execSQL(t, pool, `
        INSERT INTO blackout_schedules (scope, connection_id, cluster_id, group_id, name,
            cron_expression, duration_minutes, reason, enabled)
        VALUES ('server', $1, NULL, NULL, 'Nightly', '0 2 * * *', 30, 'Backups', true),
               ('cluster', NULL, 10, NULL, 'Weekly', '0 3 * * 0', 60, 'Vacuum', false),
               ('group', NULL, NULL, 20, 'Monthly', '0 4 1 * *', 90, 'Reindex', true)`,
		connID)
	return connID, quietID
}

func TestGetBlackoutsTool_Results(t *testing.T) {
	pool := newBlackoutsPool(t)
	connID, quietID := seedBlackoutFixtures(t, pool)

	conn := float64(connID)
	quiet := float64(quietID)
	tool := GetBlackoutsTool(pool, testRBACChecker(t), sharedLister(connID))
	runToolCases(t, tool, []toolCase{
		{name: "single connection every scope", args: map[string]any{"connection_id": conn},
			want: "(3 rows)"},
		{name: "single connection escapes reason", args: map[string]any{"connection_id": conn},
			want: `Patch\twindow`},
		{name: "single connection active only", args: map[string]any{
			"connection_id": conn, "active_only": true},
			want: "Filter: active only"},
		{name: "single connection no active blackouts", args: map[string]any{
			"connection_id": quiet, "active_only": true},
			want: "(no active blackouts)"},
		{name: "single connection no blackouts", args: map[string]any{"connection_id": quiet},
			want: "(no blackouts found)"},
		{name: "single connection schedules", args: map[string]any{
			"connection_id": conn, "include_schedules": true},
			want: "--- Blackout Schedules ---"},
		{name: "single connection no schedules", args: map[string]any{
			"connection_id": quiet, "include_schedules": true},
			want: "(no blackout schedules found)"},
		{name: "all connections", want: "(3 rows)"},
		{name: "all connections names the server", want: "blackout-conn"},
		{name: "restricted caller", args: map[string]any{
			"__context": restrictedUserContext(), "active_only": true, "include_schedules": true},
			want: "Blackouts | All accessible connections | Filter: active only"},
		{name: "all connections schedules", args: map[string]any{"include_schedules": true},
			want: "Nightly"},
	})

	// Counts are checked directly because the section order matters.
	for name, args := range map[string]map[string]any{
		"single": {"connection_id": conn, "include_schedules": true},
		"all":    {"include_schedules": true},
	} {
		t.Run(name+" schedule count", func(t *testing.T) {
			text := mustSuccess(t, asSuperuser(tool), args)
			_, schedules, found := strings.Cut(text, "--- Blackout Schedules ---")
			if !found || !strings.Contains(schedules, "(3 rows)") {
				t.Errorf("expected three schedules, got: %s", text)
			}
		})
	}

	// With every blackout and schedule removed, all-connections mode
	// reports the empty markers.
	execSQL(t, pool, "DELETE FROM blackouts")
	execSQL(t, pool, "DELETE FROM blackout_schedules")
	runToolCases(t, tool, []toolCase{
		{name: "all connections no blackouts", args: map[string]any{"include_schedules": true},
			want: "(no blackout schedules found)"},
		{name: "all connections no active blackouts", args: map[string]any{"active_only": true},
			want: "(no active blackouts)"},
		{name: "all connections empty", want: "(no blackouts found)"},
	})
}

func TestGetBlackoutsTool_ScanFailures(t *testing.T) {
	pool := newBlackoutsPool(t)
	connID, _ := seedBlackoutFixtures(t, pool)
	conn := float64(connID)
	tool := GetBlackoutsTool(pool, testRBACChecker(t), nil)

	// A NULL schedule name cannot be scanned into a string.
	execSQL(t, pool, `
        INSERT INTO blackout_schedules (scope, name, cron_expression, duration_minutes)
        VALUES ('estate', NULL, '* * * * *', 1)`)
	runToolCases(t, tool, []toolCase{
		{name: "all connections schedule scan failure", args: map[string]any{"include_schedules": true},
			wantErr: true, want: "failed to scan schedule row"},
	})
	resp, err := asSuperuser(tool).Handler(map[string]any{"connection_id": conn, "include_schedules": true})
	if err == nil || !strings.Contains(err.Error(), "failed to scan schedule row") {
		t.Errorf("expected a schedule scan error, got resp=%+v err=%v", resp, err)
	}

	// A NULL reason cannot be scanned into a string.
	execSQL(t, pool, `
        INSERT INTO blackouts (scope, reason, start_time, end_time)
        VALUES ('estate', NULL, NOW(), NOW())`)
	runToolCases(t, tool, []toolCase{
		{name: "single connection scan failure", args: map[string]any{"connection_id": conn},
			wantErr: true, want: "Failed to scan row"},
		{name: "all connections scan failure", wantErr: true, want: "Failed to scan row"},
	})
}

// TestGetBlackoutsTool_ScheduleQueryFailure drops the schedules table so
// the second query fails after the blackouts have been read. Single
// connection mode returns the failure as a Go error, whereas
// all-connections mode returns an error response.
func TestGetBlackoutsTool_ScheduleQueryFailure(t *testing.T) {
	pool := newBlackoutsPool(t)
	connID, _ := seedBlackoutFixtures(t, pool)
	execSQL(t, pool, "DROP TABLE blackout_schedules")
	tool := GetBlackoutsTool(pool, testRBACChecker(t), nil)

	runToolCases(t, tool, []toolCase{
		{name: "all connections", args: map[string]any{"include_schedules": true},
			wantErr: true, want: "Failed to query blackout schedules"},
	})
	resp, err := asSuperuser(tool).Handler(map[string]any{
		"connection_id": float64(connID), "include_schedules": true})
	if err == nil || !strings.Contains(err.Error(), "failed to query blackout schedules") {
		t.Errorf("expected a schedule query error, got resp=%+v err=%v", resp, err)
	}
}
