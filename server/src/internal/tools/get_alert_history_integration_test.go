/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Integration tests for the get_alert_history tool against the alerts
// table in its collector migration v18 shape, in which a system alert
// (GitHub issue #582) has a NULL connection_id. They reuse the pool and
// connections table from tools_integration_test.go and skip when
// TEST_AI_WORKBENCH_SERVER is unset.

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// systemAlertHistorySchema adds the alert tables the tool reads, limited
// to the columns it references, in their migration v18 shape.
const systemAlertHistorySchema = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;

CREATE TABLE alerts (
    id BIGSERIAL PRIMARY KEY,
    alert_type TEXT NOT NULL
        CHECK (alert_type IN ('threshold', 'anomaly', 'connection', 'system')),
    rule_id BIGINT,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    metric_name TEXT,
    metric_value REAL,
    threshold_value REAL,
    operator TEXT,
    severity TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    status TEXT NOT NULL,
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    cleared_at TIMESTAMPTZ,
    CONSTRAINT alerts_system_connection_check
        CHECK ((alert_type = 'system') = (connection_id IS NULL))
);

CREATE TABLE alert_acknowledgments (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    acknowledged_by TEXT NOT NULL,
    acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    message TEXT NOT NULL DEFAULT '',
    false_positive BOOLEAN NOT NULL DEFAULT FALSE
);
`

const systemAlertHistoryTeardown = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
`

// alertHistoryFixture holds the rows every test in this file starts
// with: a shared and a private connection, one active alert on each and
// one active system alert.
type alertHistoryFixture struct {
	pool       *pgxpool.Pool
	ds         *database.Datastore
	sharedConn int
	privConn   int
}

// newAlertHistoryFixture provisions the schema and seed rows, and
// registers the teardown with t.Cleanup.
func newAlertHistoryFixture(t *testing.T) *alertHistoryFixture {
	t.Helper()
	pool, ds, cleanup := newToolsTestPool(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, systemAlertHistorySchema); err != nil {
		t.Fatalf("create alert history schema: %v", err)
	}
	// Registered after the pool cleanup, so it runs first.
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), systemAlertHistoryTeardown); err != nil {
			t.Logf("alert history teardown failed: %v", err)
		}
	})

	f := &alertHistoryFixture{
		pool:       pool,
		ds:         ds,
		sharedConn: seedBaselineConnection(t, pool, "shared-conn", true, "alice"),
		privConn:   seedBaselineConnection(t, pool, "alice-private", false, "alice"),
	}
	f.insertAlert(t, "threshold", &f.sharedConn, "Shared connection alert")
	f.insertAlert(t, "threshold", &f.privConn, "Private connection alert")
	f.insertAlert(t, "system", nil, "Tier 2 embedding provider failing")
	return f
}

func (f *alertHistoryFixture) insertAlert(t *testing.T, alertType string, connID *int, title string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status)
		VALUES ($1, $2, 'warning', $3, 'test alert', 'active')`,
		alertType, connID, title); err != nil {
		t.Fatalf("insert alert %q: %v", title, err)
	}
}

// bobContext creates a non-superuser who owns nothing, so he may see the
// shared connection only.
func bobContext(t *testing.T, store *auth.AuthStore) context.Context {
	t.Helper()
	if err := store.CreateUser("bob", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	return nonSuperuserContextInt(userID, "bob")
}

// superuserTool returns the tool behind a real RBAC checker, run as a
// superuser unless a call carries its own "__context". A nil checker
// denies everything (GitHub issue #561), so tests that need to get past
// the access checks use this rather than passing nil.
func (f *alertHistoryFixture) superuserTool(t *testing.T) Tool {
	t.Helper()
	store, authCleanup := newRBACTestStore(t)
	t.Cleanup(authCleanup)
	return asSuperuser(GetAlertHistoryTool(f.pool,
		auth.NewRBACCheckerForDatastore(store, f.ds), database.NewVisibilityLister(f.ds)))
}

// TestGetAlertHistorySystemAlertsNilCheckerIntegration proves a nil
// checker sees neither connection nor system alerts, since every
// RBACChecker method, CanSeeSystemAlerts included, denies on a nil
// receiver.
func TestGetAlertHistorySystemAlertsNilCheckerIntegration(t *testing.T) {
	f := newAlertHistoryFixture(t)
	tool := GetAlertHistoryTool(f.pool, nil, nil)

	body := mustSuccess(t, tool, map[string]any{"__context": superuserContext(), "status": "active"})
	if !strings.Contains(body, "You do not have access to any connections") {
		t.Errorf("expected the no-access answer:\n%s", body)
	}
	if strings.Contains(body, "Tier 2 embedding") {
		t.Errorf("system alert listed to a nil checker:\n%s", body)
	}
}

// TestGetAlertHistorySystemAlertsSuperuserIntegration proves a superuser
// sees every connection's alerts and the system alert, the system alert
// with an empty connection_id and the alerter's label.
func TestGetAlertHistorySystemAlertsSuperuserIntegration(t *testing.T) {
	f := newAlertHistoryFixture(t)
	tool := f.superuserTool(t)

	body := mustSuccess(t, tool, map[string]any{"status": "active"})
	for _, want := range []string{
		"Shared connection alert",
		"Private connection alert",
		"\t" + systemAlertConnectionName + "\t",
		"Tier 2 embedding provider failing",
		"(3 rows)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("output lacks %q:\n%s", want, body)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "Tier 2 embedding") && !strings.HasPrefix(line, "\t") {
			t.Errorf("system alert row has a connection_id: %q", line)
		}
	}
}

// TestGetAlertHistorySystemAlertsScopedUserIntegration proves a user
// with a limited connection view sees the system alert beside the
// alerts of the connections he may see, and nothing else.
func TestGetAlertHistorySystemAlertsScopedUserIntegration(t *testing.T) {
	f := newAlertHistoryFixture(t)
	store, authCleanup := newRBACTestStore(t)
	defer authCleanup()
	ctx := bobContext(t, store)

	tool := GetAlertHistoryTool(f.pool, auth.NewRBACCheckerForDatastore(store, f.ds),
		database.NewVisibilityLister(f.ds))
	body := mustSuccess(t, tool, map[string]any{"__context": ctx})
	if !strings.Contains(body, "Shared connection alert") ||
		!strings.Contains(body, "Tier 2 embedding provider failing") {
		t.Errorf("expected the shared and system alerts:\n%s", body)
	}
	if strings.Contains(body, "Private connection alert") {
		t.Errorf("RBAC leak: private connection alert listed:\n%s", body)
	}
}

// TestGetAlertHistorySystemAlertsNoConnectionsIntegration proves a
// caller who sees no connection still sees the system alert, and only
// that.
func TestGetAlertHistorySystemAlertsNoConnectionsIntegration(t *testing.T) {
	f := newAlertHistoryFixture(t)
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE connections SET is_shared = FALSE`); err != nil {
		t.Fatalf("unshare connections: %v", err)
	}
	store, authCleanup := newRBACTestStore(t)
	defer authCleanup()
	ctx := bobContext(t, store)

	tool := GetAlertHistoryTool(f.pool, auth.NewRBACCheckerForDatastore(store, f.ds),
		database.NewVisibilityLister(f.ds))
	body := mustSuccess(t, tool, map[string]any{"__context": ctx, "status": "active"})
	if !strings.Contains(body, "Tier 2 embedding provider failing") || !strings.Contains(body, "(1 rows)") {
		t.Errorf("expected only the system alert:\n%s", body)
	}
}

// TestGetAlertHistorySystemAlertsDeniedIntegration proves a caller the
// system alert gate refuses, here one with no user ID, keeps the
// no-access answer rather than seeing system alerts.
func TestGetAlertHistorySystemAlertsDeniedIntegration(t *testing.T) {
	f := newAlertHistoryFixture(t)
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE connections SET is_shared = FALSE`); err != nil {
		t.Fatalf("unshare connections: %v", err)
	}
	store, authCleanup := newRBACTestStore(t)
	defer authCleanup()

	ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, false)
	tool := GetAlertHistoryTool(f.pool, auth.NewRBACCheckerForDatastore(store, f.ds),
		database.NewVisibilityLister(f.ds))
	body := mustSuccess(t, tool, map[string]any{"__context": ctx})
	if !strings.Contains(body, "You do not have access to any connections") {
		t.Errorf("expected the no-access answer:\n%s", body)
	}
}

// TestGetAlertHistorySingleConnectionOmitsSystemIntegration proves a
// connection_id filter never returns system alerts.
func TestGetAlertHistorySingleConnectionOmitsSystemIntegration(t *testing.T) {
	f := newAlertHistoryFixture(t)
	tool := f.superuserTool(t)

	body := mustSuccess(t, tool, map[string]any{"connection_id": f.sharedConn})
	if !strings.Contains(body, "Shared connection alert") || !strings.Contains(body, "(1 rows)") {
		t.Errorf("expected the one shared connection alert:\n%s", body)
	}
	if strings.Contains(body, "Tier 2 embedding") {
		t.Errorf("system alert listed for a single connection:\n%s", body)
	}
}

// TestGetAlertHistoryEmptyResultsIntegration covers the no-rows answers
// of both modes.
func TestGetAlertHistoryEmptyResultsIntegration(t *testing.T) {
	f := newAlertHistoryFixture(t)
	tool := f.superuserTool(t)

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"all connections cleared", map[string]any{"status": "cleared"},
			"No alerts found across accessible connections with status='cleared'"},
		{"all connections active rule", map[string]any{"status": "active", "rule_id": 999},
			"No active alerts across accessible connections."},
		{"single connection cleared", map[string]any{"connection_id": f.sharedConn, "status": "cleared"},
			"No alerts found for connection"},
		{"single connection active rule", map[string]any{"connection_id": f.sharedConn, "status": "active", "rule_id": 999},
			"No active alerts for connection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if body := mustSuccess(t, tool, tt.args); !strings.Contains(body, tt.want) {
				t.Errorf("want %q, got:\n%s", tt.want, body)
			}
		})
	}
}

// TestGetAlertHistoryArgumentsIntegration covers the argument handling
// in front of both queries.
func TestGetAlertHistoryArgumentsIntegration(t *testing.T) {
	f := newAlertHistoryFixture(t)
	tool := f.superuserTool(t)

	errorCases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"connection_id not an integer", map[string]any{"connection_id": "x"}, "Invalid 'connection_id'"},
		{"unknown connection", map[string]any{"connection_id": 99999}, "Valid connection IDs are"},
		{"rule_id not an integer", map[string]any{"rule_id": "x"}, "Invalid 'rule_id'"},
		{"time_start unparseable", map[string]any{"time_start": "soon"}, "Invalid 'time_start'"},
		{"limit out of range", map[string]any{"limit": 500}, "Invalid 'limit'"},
		{"offset negative", map[string]any{"offset": -1}, "Invalid 'offset'"},
	}
	for _, tt := range errorCases {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tool.Handler(tt.args)
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if !resp.IsError || len(resp.Content) == 0 || !strings.Contains(resp.Content[0].Text, tt.want) {
				t.Errorf("want error %q, got %+v", tt.want, resp.Content)
			}
		})
	}

	okCases := []struct {
		name string
		args map[string]any
	}{
		{"relative time_start", map[string]any{"time_start": "24h", "limit": 10, "offset": 0}},
		{"ISO time_start", map[string]any{"time_start": "2020-01-01T00:00:00Z"}},
		{"metric_name filter", map[string]any{"metric_name": "none", "status": "all"}},
	}
	for _, tt := range okCases {
		t.Run(tt.name, func(t *testing.T) {
			mustSuccess(t, tool, tt.args)
		})
	}

	t.Run("unknown connection with none to list", func(t *testing.T) {
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM connections`); err != nil {
			t.Fatalf("delete connections: %v", err)
		}
		resp, err := tool.Handler(map[string]any{"connection_id": 99999})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if !resp.IsError || !strings.Contains(resp.Content[0].Text, "Use list_connections to see available connections") {
			t.Errorf("unexpected response %+v", resp.Content)
		}
	})
}

// TestGetAlertHistorySingleConnectionDeniedIntegration proves the
// single-connection gate refuses a connection the caller cannot see.
func TestGetAlertHistorySingleConnectionDeniedIntegration(t *testing.T) {
	f := newAlertHistoryFixture(t)
	store, authCleanup := newRBACTestStore(t)
	defer authCleanup()
	ctx := bobContext(t, store)

	tool := GetAlertHistoryTool(f.pool, auth.NewRBACCheckerForDatastore(store, f.ds),
		database.NewVisibilityLister(f.ds))
	resp, err := tool.Handler(map[string]any{"__context": ctx, "connection_id": f.privConn})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !resp.IsError || !strings.Contains(resp.Content[0].Text, "Access denied") {
		t.Errorf("expected access denied, got %+v", resp.Content)
	}
}
