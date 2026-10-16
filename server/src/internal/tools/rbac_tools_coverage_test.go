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
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// Branch coverage for the datastore tools whose access checks changed in
// issue #561: argument validation on either side of the check, the
// restricted (non-superuser) visibility paths and visibility failures.

// coverageSchema adds the alert rule tables and two probe tables to the
// schema built by newToolsTestPool. The alerts and blackouts tables are
// deliberately absent: the tests for those tools only need to show that
// a request got past validation, which the query failure proves.
const coverageSchema = `
DROP TABLE IF EXISTS alert_thresholds CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;

CREATE TABLE alert_rules (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    category TEXT NOT NULL,
    metric_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    default_operator TEXT NOT NULL,
    default_threshold DOUBLE PRECISION NOT NULL,
    default_severity TEXT NOT NULL,
    default_enabled BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE alert_thresholds (
    id BIGSERIAL PRIMARY KEY,
    rule_id BIGINT NOT NULL,
    connection_id INTEGER,
    operator TEXT,
    threshold DOUBLE PRECISION,
    severity TEXT,
    enabled BOOLEAN
);

CREATE TABLE metrics.cov_probe (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    database_name TEXT,
    value_count BIGINT,
    PRIMARY KEY (connection_id, collected_at)
);

CREATE TABLE metrics.cov_empty (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (connection_id, collected_at)
);
`

// newCoverageToolsPool returns a pool on the tools test schema plus
// coverageSchema, and drops both when the test ends.
func newCoverageToolsPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, _, cleanup := newToolsTestPool(t)
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			"DROP TABLE IF EXISTS alert_thresholds, alert_rules CASCADE"); err != nil {
			t.Logf("coverage teardown failed: %v", err)
		}
		cleanup()
	})
	if _, err := pool.Exec(context.Background(), coverageSchema); err != nil {
		t.Fatalf("failed to create coverage schema: %v", err)
	}
	return pool
}

// restrictedUserContext returns the context of a signed-in user who is
// not a superuser and holds no grants.
func restrictedUserContext() context.Context {
	ctx := context.WithValue(context.Background(), auth.UserIDContextKey, int64(4242))
	return context.WithValue(ctx, auth.UsernameContextKey, "restricted")
}

// privateChecker returns a checker to which every connection is private
// to another user, so only a superuser may reach one.
func privateChecker(t *testing.T) *auth.RBACChecker {
	t.Helper()
	checker := testRBACChecker(t)
	checker.SetConnectionSharingLookup(func(context.Context, int) (bool, string, error) {
		return false, "someone-else", nil
	})
	return checker
}

// sharedLister makes the given connection IDs visible to every user.
func sharedLister(ids ...int) *stubVisibilityLister {
	l := &stubVisibilityLister{}
	for _, id := range ids {
		l.connections = append(l.connections, auth.ConnectionVisibilityInfo{ID: id, IsShared: true})
	}
	return l
}

// failingLister fails every visibility lookup.
func failingLister() *stubVisibilityLister {
	return &stubVisibilityLister{err: errors.New("lister unavailable")}
}

// toolCase is one call to a tool handler. Unless raw is set the call runs
// as a superuser when args carries no "__context"; raw calls the handler
// unwrapped, so a missing "__context" falls back to context.Background.
type toolCase struct {
	name    string
	args    map[string]any
	raw     bool
	wantErr bool
	want    string
}

// runToolCases runs each case against tool and checks the response text
// contains want and has the expected error flag.
func runToolCases(t *testing.T, tool Tool, cases []toolCase) {
	t.Helper()
	wrapped := asSuperuser(tool)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.args == nil {
				tc.args = map[string]any{}
			}
			handler := wrapped.Handler
			if tc.raw {
				handler = tool.Handler
			}
			resp, err := handler(tc.args)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if len(resp.Content) == 0 {
				t.Fatalf("empty response: %+v", resp)
			}
			text := resp.Content[0].Text
			if resp.IsError != tc.wantErr {
				t.Errorf("expected IsError=%v, got %v: %s", tc.wantErr, resp.IsError, text)
			}
			if !strings.Contains(text, tc.want) {
				t.Errorf("expected %q in response, got: %s", tc.want, text)
			}
		})
	}
}

func TestGetAlertHistoryTool_Coverage(t *testing.T) {
	runToolCases(t, GetAlertHistoryTool(nil, testRBACChecker(t), nil), []toolCase{
		{name: "nil pool", wantErr: true, want: "Datastore not configured"},
	})

	pool := newCoverageToolsPool(t)

	// With no connections at all the error names none.
	runToolCases(t, GetAlertHistoryTool(pool, testRBACChecker(t), nil), []toolCase{
		{name: "unknown connection, none exist", args: map[string]any{"connection_id": float64(999)},
			wantErr: true, want: "connection not found or not accessible. Use list_connections to see the connections you can access."},
	})

	connID := seedBaselineConnection(t, pool, "alerts-conn", true, "")
	checker := testRBACChecker(t)
	tool := GetAlertHistoryTool(pool, checker, sharedLister(connID))
	restricted := restrictedUserContext()
	runToolCases(t, tool, []toolCase{
		{name: "no context", raw: true, wantErr: true, want: "Failed to query alerts"},
		{name: "invalid connection_id", args: map[string]any{"connection_id": "x"},
			wantErr: true, want: "Invalid 'connection_id'"},
		{name: "unknown connection lists visible IDs", args: map[string]any{"connection_id": float64(999)},
			wantErr: true, want: "Connections you can access include: "},
		{name: "single connection", args: map[string]any{"connection_id": float64(connID)},
			wantErr: true, want: "Failed to query alerts"},
		{name: "restricted all connections", args: map[string]any{"__context": restricted},
			wantErr: true, want: "Failed to query alerts"},
		{name: "invalid rule_id", args: map[string]any{"rule_id": "x"},
			wantErr: true, want: "Invalid 'rule_id'"},
		{name: "invalid status", args: map[string]any{"status": "bogus"},
			wantErr: true, want: "Invalid 'status'"},
		{name: "active status ignores time", args: map[string]any{
			"status": "ACTIVE", "rule_id": float64(1), "metric_name": "m", "time_start": "garbage"},
			wantErr: true, want: "Failed to query alerts"},
		{name: "relative time_start", args: map[string]any{"time_start": "24h"},
			wantErr: true, want: "Failed to query alerts"},
		{name: "absolute time_start", args: map[string]any{"time_start": "2026-01-01T00:00:00Z"},
			wantErr: true, want: "Failed to query alerts"},
		{name: "invalid time_start", args: map[string]any{"time_start": "garbage"},
			wantErr: true, want: "Invalid 'time_start'"},
		{name: "invalid limit", args: map[string]any{"limit": float64(0)},
			wantErr: true, want: "Invalid 'limit'"},
		{name: "valid limit and offset", args: map[string]any{"limit": float64(10), "offset": float64(5)},
			wantErr: true, want: "Failed to query alerts"},
		{name: "invalid offset", args: map[string]any{"offset": float64(-1)},
			wantErr: true, want: "Invalid 'offset'"},
	})

	runToolCases(t, GetAlertHistoryTool(pool, checker, failingLister()), []toolCase{
		{name: "visibility failure", args: map[string]any{"__context": restricted},
			wantErr: true, want: "Failed to resolve accessible connections"},
	})
	runToolCases(t, GetAlertHistoryTool(pool, privateChecker(t), sharedLister()), []toolCase{
		// A signed-in user who sees no connection may still see system
		// alerts (GitHub issue #582), so the query runs rather than
		// short-circuiting; this pool has no alerts table.
		{name: "no visible connections still queries system alerts",
			args:    map[string]any{"__context": restricted},
			wantErr: true, want: "Failed to query alerts"},
		{name: "private connection denied", args: map[string]any{
			"__context": restricted, "connection_id": float64(connID)},
			wantErr: true, want: "connection not found or not accessible."},
	})
}

func TestGetBlackoutsTool_Coverage(t *testing.T) {
	runToolCases(t, GetBlackoutsTool(nil, testRBACChecker(t), nil), []toolCase{
		{name: "nil pool", wantErr: true, want: "Datastore not configured"},
	})

	pool := newCoverageToolsPool(t)

	runToolCases(t, GetBlackoutsTool(pool, testRBACChecker(t), nil), []toolCase{
		{name: "unknown connection, none exist", args: map[string]any{"connection_id": float64(999)},
			wantErr: true, want: "connection not found or not accessible. Use list_connections to see the connections you can access."},
	})

	connID := seedBaselineConnection(t, pool, "blackouts-conn", true, "")
	checker := testRBACChecker(t)
	tool := GetBlackoutsTool(pool, checker, sharedLister(connID))
	runToolCases(t, tool, []toolCase{
		{name: "no context", raw: true, wantErr: true, want: "Failed to query blackouts"},
		{name: "invalid connection_id", args: map[string]any{"connection_id": "x"},
			wantErr: true, want: "Invalid 'connection_id'"},
		{name: "unknown connection lists visible IDs", args: map[string]any{"connection_id": float64(999)},
			wantErr: true, want: "Connections you can access include: "},
		{name: "single connection", args: map[string]any{"connection_id": float64(connID)},
			wantErr: true, want: "Failed to query blackouts"},
		{name: "restricted all connections", args: map[string]any{
			"__context": restrictedUserContext(), "active_only": true, "include_schedules": true},
			wantErr: true, want: "Failed to query blackouts"},
		{name: "invalid limit", args: map[string]any{"limit": float64(51)},
			wantErr: true, want: "Invalid 'limit'"},
		{name: "valid limit", args: map[string]any{"limit": float64(10)},
			wantErr: true, want: "Failed to query blackouts"},
	})

	runToolCases(t, GetBlackoutsTool(pool, checker, failingLister()), []toolCase{
		{name: "visibility failure", args: map[string]any{"__context": restrictedUserContext()},
			wantErr: true, want: "Failed to resolve accessible connections"},
	})
	runToolCases(t, GetBlackoutsTool(pool, privateChecker(t), sharedLister()), []toolCase{
		{name: "no visible connections", args: map[string]any{"__context": restrictedUserContext()},
			want: "You do not have access to any connections"},
		{name: "private connection denied", args: map[string]any{
			"__context": restrictedUserContext(), "connection_id": float64(connID)},
			wantErr: true, want: "connection not found or not accessible."},
	})
}

func TestGetAlertRulesTool_Coverage(t *testing.T) {
	runToolCases(t, GetAlertRulesTool(nil, testRBACChecker(t)), []toolCase{
		{name: "nil pool", wantErr: true, want: "Datastore not configured"},
	})

	pool := newCoverageToolsPool(t)
	ctx := context.Background()
	var ruleID int64
	if err := pool.QueryRow(ctx, `
        INSERT INTO alert_rules (name, category, metric_name, description,
            default_operator, default_threshold, default_severity)
        VALUES ('Too many backends', 'connections', 'numbackends', 'Backend count',
            '>', 100, 'warning') RETURNING id`).Scan(&ruleID); err != nil {
		t.Fatalf("failed to seed alert rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `
        INSERT INTO alert_rules (name, category, metric_name,
            default_operator, default_threshold, default_severity, default_enabled)
        VALUES ('Disabled rule', 'storage', 'disk_used', '>', 90, 'critical', false)`); err != nil {
		t.Fatalf("failed to seed disabled alert rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `
        INSERT INTO alert_thresholds (rule_id, connection_id, threshold, severity, enabled)
        VALUES ($1, 7, 50, 'critical', true)`, ruleID); err != nil {
		t.Fatalf("failed to seed alert threshold: %v", err)
	}

	runToolCases(t, GetAlertRulesTool(pool, privateChecker(t)), []toolCase{
		{name: "no context denies a connection", raw: true, args: map[string]any{"connection_id": float64(7)},
			wantErr: true, want: "connection not found or not accessible"},
		{name: "restricted caller denied", args: map[string]any{
			"__context": restrictedUserContext(), "connection_id": float64(7)},
			wantErr: true, want: "connection not found or not accessible"},
		{name: "invalid connection_id", args: map[string]any{"connection_id": "x"},
			wantErr: true, want: "Invalid 'connection_id'"},
		{name: "invalid category", args: map[string]any{"category": "bogus"},
			wantErr: true, want: "Invalid 'category'"},
		{name: "defaults, enabled only", want: "Too many backends"},
		{name: "effective thresholds for a connection", args: map[string]any{
			"connection_id": float64(7), "category": "connections"},
			want: "effective thresholds for connection 7"},
		{name: "include disabled", args: map[string]any{"enabled_only": false}, want: "(2 rules)"},
		{name: "no matching rules", args: map[string]any{"category": "wal"}, want: "No alert rules found"},
	})
}

// TestGetAlertRulesTool_QueryFailure uses the base schema, which has no
// alert_rules table.
func TestGetAlertRulesTool_QueryFailure(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()
	runToolCases(t, GetAlertRulesTool(pool, testRBACChecker(t)), []toolCase{
		{name: "query failure", wantErr: true, want: "Failed to query alert rules"},
	})
}

func TestQueryMetricsTool_Coverage(t *testing.T) {
	runToolCases(t, QueryMetricsTool(nil, testRBACChecker(t)), []toolCase{
		{name: "nil pool", wantErr: true, want: "Datastore not configured"},
	})

	pool := newCoverageToolsPool(t)
	connID := seedBaselineConnection(t, pool, "metrics-conn", true, "")
	if _, err := pool.Exec(context.Background(), `
        INSERT INTO metrics.cov_probe (connection_id, collected_at, database_name, value_count)
        VALUES ($1, NOW() - INTERVAL '10 minutes', 'postgres', 5),
               ($1, NOW() - INTERVAL '5 minutes', 'postgres', 9)`, connID); err != nil {
		t.Fatalf("failed to seed probe rows: %v", err)
	}

	conn := float64(connID)
	withProbe := func(extra map[string]any) map[string]any {
		args := map[string]any{"probe_name": "cov_probe", "connection_id": conn}
		for k, v := range extra {
			args[k] = v
		}
		return args
	}
	runToolCases(t, QueryMetricsTool(pool, privateChecker(t)), []toolCase{
		{name: "missing probe_name", wantErr: true, want: "Missing or invalid 'probe_name'"},
		{name: "invalid probe_name", args: map[string]any{"probe_name": "bad-name"},
			wantErr: true, want: "Invalid probe name"},
		{name: "missing connection_id", args: map[string]any{"probe_name": "cov_probe"},
			wantErr: true, want: "Missing or invalid 'connection_id'"},
		{name: "no context denies", raw: true, args: withProbe(nil),
			wantErr: true, want: "connection not found or not accessible"},
		{name: "unknown connection", args: map[string]any{"probe_name": "cov_probe", "connection_id": float64(999)},
			wantErr: true, want: "connection not found or not accessible"},
		{name: "invalid time range", args: withProbe(map[string]any{"time_start": "garbage"}),
			wantErr: true, want: "Invalid time range"},
		{name: "invalid buckets", args: withProbe(map[string]any{"buckets": float64(0)}),
			wantErr: true, want: "Invalid 'buckets'"},
		{name: "invalid aggregation", args: withProbe(map[string]any{"aggregation": "median"}),
			wantErr: true, want: "Invalid 'aggregation'"},
		{name: "unknown probe", args: map[string]any{"probe_name": "no_such_probe", "connection_id": conn},
			wantErr: true, want: "Probe 'no_such_probe' not found"},
		{name: "invalid metric name", args: withProbe(map[string]any{"metrics": "bad-name"}),
			wantErr: true, want: "Invalid metric name"},
		{name: "unknown metric", args: withProbe(map[string]any{"metrics": "missing"}),
			wantErr: true, want: "Metric 'missing' not found"},
		{name: "probe without metrics", args: map[string]any{"probe_name": "cov_empty", "connection_id": conn},
			wantErr: true, want: "No numeric metrics found"},
		{name: "filter on a missing column fails the query", args: withProbe(map[string]any{
			"schema_name": "public", "table_name": "t"}),
			wantErr: true, want: "Failed to query metrics"},
		{name: "success", args: withProbe(map[string]any{
			"metrics": "value_count", "aggregation": "MAX", "buckets": float64(4),
			"database_name": "postgres", "time_start": "1h"}),
			want: "Probe: cov_probe"},
	})
}

func TestListConnectionsTool_Coverage(t *testing.T) {
	runToolCases(t, ListConnectionsTool(nil, testRBACChecker(t), nil), []toolCase{
		{name: "nil pool", wantErr: true, want: "Datastore not configured"},
	})
	runToolCases(t, ListConnectionsTool(dummyPool(t), testRBACChecker(t), nil), []toolCase{
		{name: "query failure", wantErr: true, want: "Failed to query connections"},
	})

	pool := newCoverageToolsPool(t)
	checker := testRBACChecker(t)
	runToolCases(t, ListConnectionsTool(pool, checker, nil), []toolCase{
		{name: "no connections", want: "No database connections found"},
	})

	visible := seedBaselineConnection(t, pool, "visible-conn", true, "")
	seedBaselineConnection(t, pool, "hidden-conn", false, "someone-else")
	if _, err := pool.Exec(context.Background(),
		"UPDATE connections SET is_monitored = false WHERE id = $1", visible); err != nil {
		t.Fatalf("failed to update connection: %v", err)
	}

	runToolCases(t, ListConnectionsTool(pool, checker, sharedLister(visible)), []toolCase{
		{name: "superuser sees all", want: "Found 2 connections (1 monitored)"},
		{name: "restricted sees shared only", args: map[string]any{"__context": restrictedUserContext()},
			want: "Found 1 connections (0 monitored)"},
		{name: "no context sees shared only", raw: true, want: "Found 1 connections (0 monitored)"},
	})

	// A visibility failure denies rather than returning the unfiltered
	// list.
	runToolCases(t, ListConnectionsTool(pool, checker, failingLister()), []toolCase{
		{name: "visibility failure", args: map[string]any{"__context": restrictedUserContext()},
			wantErr: true, want: "Failed to resolve accessible connections"},
	})
}

func TestResolveTimelineAccessibleIDs(t *testing.T) {
	checker := testRBACChecker(t)
	restricted := restrictedUserContext()

	t.Run("single connection", func(t *testing.T) {
		ids, all, resp, err := resolveTimelineAccessibleIDs(restricted, true, checker, failingLister())
		if ids != nil || !all || resp != nil || err != nil {
			t.Errorf("expected (nil, true, nil, nil), got (%v, %v, %v, %v)", ids, all, resp, err)
		}
	})

	t.Run("visibility failure", func(t *testing.T) {
		_, all, resp, err := resolveTimelineAccessibleIDs(restricted, false, checker, failingLister())
		if err != nil || all || resp == nil || !resp.IsError ||
			!strings.Contains(resp.Content[0].Text, "Failed to resolve accessible connections") {
			t.Errorf("expected a resolve error response, got all=%v resp=%+v err=%v", all, resp, err)
		}
	})

	t.Run("no visible connections", func(t *testing.T) {
		_, all, resp, err := resolveTimelineAccessibleIDs(restricted, false, checker, sharedLister())
		if err != nil || all || resp == nil || resp.IsError ||
			!strings.Contains(resp.Content[0].Text, "do not have access to any connections") {
			t.Errorf("expected the no-access response, got all=%v resp=%+v err=%v", all, resp, err)
		}
	})

	t.Run("visible connections", func(t *testing.T) {
		ids, all, resp, err := resolveTimelineAccessibleIDs(restricted, false, checker, sharedLister(3, 5))
		if err != nil || all || resp != nil || len(ids) != 2 {
			t.Errorf("expected two IDs, got ids=%v all=%v resp=%+v err=%v", ids, all, resp, err)
		}
	})

	t.Run("superuser", func(t *testing.T) {
		ids, all, resp, err := resolveTimelineAccessibleIDs(superuserContext(), false, checker, nil)
		if err != nil || !all || resp != nil || ids != nil {
			t.Errorf("expected all connections, got ids=%v all=%v resp=%+v err=%v", ids, all, resp, err)
		}
	})
}
