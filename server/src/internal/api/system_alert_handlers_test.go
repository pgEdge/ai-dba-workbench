/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// System alerts (GitHub issue #582) at the HTTP boundary. A system alert
// belongs to no connection, so every caller with alert access sees it and
// may act on it whatever their connection grants, and a caller whose
// identity cannot be resolved sees and changes nothing.
// =============================================================================

// systemAlertHandlerSchema holds the connections columns the visibility
// lister reads and the collector migration v19 shape of the alerts table.
const systemAlertHandlerSchema = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    host VARCHAR(255) NOT NULL DEFAULT 'db.example.com',
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL DEFAULT 'postgres',
    owner_username VARCHAR(255),
    is_monitored BOOLEAN NOT NULL DEFAULT TRUE,
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    membership_source VARCHAR(16) NOT NULL DEFAULT 'auto',
    cluster_id INTEGER
);

CREATE TABLE alert_rules (
    id BIGSERIAL PRIMARY KEY,
    metric_unit TEXT
);

CREATE TABLE alerts (
    id BIGSERIAL PRIMARY KEY,
    alert_type TEXT NOT NULL
        CHECK (alert_type IN ('threshold', 'anomaly', 'connection', 'system')),
    rule_id BIGINT REFERENCES alert_rules(id) ON DELETE SET NULL,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
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
    ai_analysis_metric_value REAL,
    CONSTRAINT alerts_system_connection_check
        CHECK ((alert_type = 'system') = (connection_id IS NULL))
);

CREATE TABLE alert_acknowledgments (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    acknowledged_by TEXT NOT NULL,
    acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    acknowledge_type TEXT NOT NULL
        CHECK (acknowledge_type IN ('acknowledge', 'dismiss', 'false_positive')),
    message TEXT NOT NULL DEFAULT '',
    false_positive BOOLEAN NOT NULL DEFAULT FALSE
);
`

const systemAlertHandlerTeardown = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
`

// systemAlertHandlerFixture is an alert handler over a real datastore
// holding alice's private connection with one alert, and one system
// alert. bob has an account but no grant on alice's connection.
type systemAlertHandlerFixture struct {
	handler    *AlertHandler
	store      *auth.AuthStore
	pool       *pgxpool.Pool
	privConn   int
	connAlert  int64
	systemID   int64
	bobID      int64
	aliceTitle string
}

func newSystemAlertHandlerFixture(t *testing.T) *systemAlertHandlerFixture {
	t.Helper()
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping system alert handler test")
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
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), systemAlertHandlerTeardown); err != nil {
			t.Logf("system alert handler teardown: %v", err)
		}
		pool.Close()
	})
	if _, err := pool.Exec(ctx, systemAlertHandlerSchema); err != nil {
		t.Fatalf("create system alert handler schema: %v", err)
	}

	_, store, authCleanup := createTestRBACHandler(t)
	t.Cleanup(authCleanup)

	f := &systemAlertHandlerFixture{pool: pool, store: store, aliceTitle: "alice alert"}
	if err := pool.QueryRow(ctx, `
		INSERT INTO connections (name, owner_username) VALUES ('alice-db', 'alice')
		RETURNING id`).Scan(&f.privConn); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	f.connAlert = f.insertAlert(t, "threshold", &f.privConn, f.aliceTitle)
	f.systemID = f.insertAlert(t, database.AlertTypeSystem, nil, "provider failing")
	f.bobID = newTestUser(t, store, "bob")

	ds := database.NewTestDatastore(pool)
	f.handler = NewAlertHandler(ds, store, auth.NewRBACCheckerForDatastore(store, ds))
	return f
}

func (f *systemAlertHandlerFixture) insertAlert(t *testing.T, alertType string, connID *int, title string) int64 {
	t.Helper()
	var id int64
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status)
		VALUES ($1, $2, 'warning', $3, 'test', 'active') RETURNING id`,
		alertType, connID, title).Scan(&id); err != nil {
		t.Fatalf("insert alert %q: %v", title, err)
	}
	return id
}

// bob returns req carrying bob's session identity.
func (f *systemAlertHandlerFixture) bob(req *http.Request) *http.Request {
	return withUsername(withUser(req, f.bobID), "bob")
}

// withIncompleteToken marks req as an API token request with no token
// ID, the shape the RBAC checker must refuse outright.
func withIncompleteToken(req *http.Request) *http.Request {
	ctx := context.WithValue(req.Context(), auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.IsAPITokenContextKey, true)
	return req.WithContext(ctx)
}

func (f *systemAlertHandlerFixture) listAlerts(t *testing.T, req *http.Request) database.AlertListResult {
	t.Helper()
	rec := httptest.NewRecorder()
	f.handler.handleAlerts(rec, req)
	requireStatus(t, rec, http.StatusOK)
	var body database.AlertListResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode alerts: %v", err)
	}
	return body
}

func (f *systemAlertHandlerFixture) counts(t *testing.T, req *http.Request) database.AlertCountsResult {
	t.Helper()
	rec := httptest.NewRecorder()
	f.handler.handleAlertCounts(rec, req)
	requireStatus(t, rec, http.StatusOK)
	var body database.AlertCountsResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode counts: %v", err)
	}
	return body
}

// TestAlertHandler_HandleAlerts_SystemAlertVisibility proves who gets the
// system alert from the alert list, and that it never brings a private
// connection's alerts with it.
func TestAlertHandler_HandleAlerts_SystemAlertVisibility(t *testing.T) {
	f := newSystemAlertHandlerFixture(t)
	get := func(query string) *http.Request {
		return httptest.NewRequest(http.MethodGet, "/api/v1/alerts"+query, nil)
	}

	t.Run("zero-grant user sees only the system alert", func(t *testing.T) {
		body := f.listAlerts(t, f.bob(get("")))
		if len(body.Alerts) != 1 || body.Total != 1 {
			t.Fatalf("want exactly the system alert, got %+v", body)
		}
		if a := body.Alerts[0]; a.ID != f.systemID || a.ConnectionID != nil || a.ServerName != "" {
			t.Errorf("unexpected alert %+v", a)
		}
	})

	t.Run("superuser sees both", func(t *testing.T) {
		body := f.listAlerts(t, withSuperuser(get("")))
		if body.Total != 2 {
			t.Errorf("want 2 alerts, got %+v", body)
		}
	})

	t.Run("connection filter leaves system alerts out", func(t *testing.T) {
		body := f.listAlerts(t, withSuperuser(get("?connection_id="+strconv.Itoa(f.privConn))))
		if len(body.Alerts) != 1 || body.Alerts[0].ID != f.connAlert {
			t.Errorf("want only the connection alert, got %+v", body)
		}
	})

	t.Run("incomplete token sees nothing", func(t *testing.T) {
		body := f.listAlerts(t, withIncompleteToken(get("")))
		if len(body.Alerts) != 0 || body.Total != 0 {
			t.Errorf("incomplete token saw %+v", body)
		}
	})
}

// TestAlertHandler_HandleAlertCounts_SystemAlerts proves the counts carry
// system alerts in their own field for a zero-grant user, and nothing for
// an unresolvable caller.
func TestAlertHandler_HandleAlertCounts_SystemAlerts(t *testing.T) {
	f := newSystemAlertHandlerFixture(t)
	get := func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "/api/v1/alerts/counts", nil)
	}

	bob := f.counts(t, f.bob(get()))
	if bob.Total != 1 || bob.System != 1 || len(bob.ByServer) != 0 {
		t.Errorf("zero-grant user counts = %+v, want only the system alert", bob)
	}

	su := f.counts(t, withSuperuser(get()))
	if su.Total != 2 || su.System != 1 || su.ByServer[f.privConn] != 1 {
		t.Errorf("superuser counts = %+v", su)
	}

	none := f.counts(t, withIncompleteToken(get()))
	if none.Total != 0 || none.System != 0 || len(none.ByServer) != 0 {
		t.Errorf("incomplete token counts = %+v", none)
	}
}

// TestAlertHandler_Mutation_SystemAlert proves who may act on a system
// alert: acknowledging and restoring one needs a superuser or a
// manage_alert_rules holder, bounded by a token's admin scope, whilst
// saving AI analysis on one is refused to everybody with a 400. It also
// proves a zero-grant user is still refused on the private connection's
// alert.
func TestAlertHandler_Mutation_SystemAlert(t *testing.T) {
	f := newSystemAlertHandlerFixture(t)
	ack := func(h *AlertHandler, w http.ResponseWriter, r *http.Request) { h.acknowledgeAlert(w, r) }
	unack := func(h *AlertHandler, w http.ResponseWriter, r *http.Request) { h.unacknowledgeAlert(w, r) }
	save := func(h *AlertHandler, w http.ResponseWriter, r *http.Request) { h.handleSaveAnalysis(w, r) }
	sys := strconv.FormatInt(f.systemID, 10)

	holderID := setupUserWithPermission(t, f.store, "rules_admin", auth.PermManageAlertRules)
	newToken := func(owner string, adminScope []string) int64 {
		t.Helper()
		_, token, err := f.store.CreateToken(owner, owner+" token", nil)
		if err != nil {
			t.Fatalf("CreateToken: %v", err)
		}
		if adminScope != nil {
			if err := f.store.SetTokenAdminScope(token.ID, adminScope); err != nil {
				t.Fatalf("SetTokenAdminScope: %v", err)
			}
		}
		return token.ID
	}
	grantedToken := newToken("rules_admin", []string{auth.PermManageAlertRules})
	withoutGrantToken := newToken("rules_admin", []string{auth.PermManageUsers})
	noAuth := NewAlertHandler(f.handler.datastore, nil, auth.NewRBACChecker(nil))

	tests := []struct {
		name      string
		handler   *AlertHandler
		as        func(*http.Request) *http.Request
		ackStatus int
		// saveStatus is 400 for a caller who can see system alerts and
		// 403 for one who cannot.
		saveStatus int
	}{
		{"superuser", f.handler, withSuperuser, http.StatusOK, http.StatusBadRequest},
		{"manage_alert_rules holder", f.handler, func(r *http.Request) *http.Request {
			return withUsername(withUser(r, holderID), "rules_admin")
		}, http.StatusOK, http.StatusBadRequest},
		{"token scoped to the grant", f.handler, func(r *http.Request) *http.Request {
			return withToken(r, holderID, grantedToken)
		}, http.StatusOK, http.StatusBadRequest},
		{"token scoped without the grant", f.handler, func(r *http.Request) *http.Request {
			return withToken(r, holderID, withoutGrantToken)
		}, http.StatusForbidden, http.StatusBadRequest},
		{"plain user", f.handler, f.bob, http.StatusForbidden, http.StatusBadRequest},
		// A checker without an auth store fails closed (issue #477),
		// even for a superuser.
		{"no auth store", noAuth, withSuperuser, http.StatusForbidden, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := func(invoke alertMutationInvoker, method, url, body string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, url, bytes.NewReader([]byte(body)))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				invoke(tt.handler, rec, tt.as(req))
				return rec
			}
			requireStatus(t, call(ack, http.MethodPost, "/api/v1/alerts/acknowledge",
				`{"alert_id": `+sys+`, "message": "seen"}`), tt.ackStatus)
			requireStatus(t, call(unack, http.MethodDelete,
				"/api/v1/alerts/acknowledge?alert_id="+sys, ""), tt.ackStatus)
			requireStatus(t, call(save, http.MethodPut, "/api/v1/alerts/analysis",
				`{"alert_id": `+sys+`, "analysis": "provider down"}`), tt.saveStatus)
		})
	}

	var analysis *string
	var status string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT ai_analysis, status FROM alerts WHERE id = $1`, f.systemID).Scan(&analysis, &status); err != nil {
		t.Fatalf("read alert: %v", err)
	}
	if analysis != nil {
		t.Errorf("analysis = %q, want none saved on a system alert", *analysis)
	}
	if status != "active" {
		t.Errorf("status = %q, want active after every acknowledgement was restored", status)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/acknowledge",
		bytes.NewReader([]byte(`{"alert_id": `+strconv.FormatInt(f.connAlert, 10)+`}`)))
	rec := httptest.NewRecorder()
	f.handler.acknowledgeAlert(rec, f.bob(req))
	requireStatus(t, rec, http.StatusForbidden)
}

// TestAlertHandler_Mutation_SystemAlert_IncompleteToken_403 proves every
// mutation handler refuses a system alert to a caller whose token context
// is incomplete. The nil datastore would panic past the gate.
func TestAlertHandler_Mutation_SystemAlert_IncompleteToken_403(t *testing.T) {
	for _, tc := range alertMutationHandlers {
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			handler := NewAlertHandler(nil, store, auth.NewRBACChecker(store))
			resolver := &fakeAlertResolver{system: true}
			handler.setAlertResolver(resolver)

			req := httptest.NewRequest(tc.method, tc.url, bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			tc.invoker(handler, rec, withIncompleteToken(req))

			requireStatus(t, rec, http.StatusForbidden)
			if resolver.calls != 1 {
				t.Errorf("resolver ran %d times, want 1", resolver.calls)
			}
		})
	}
}

// TestAlertHandler_HandleAlerts_FiltersAndGrants covers the query
// parameters and the connection allow-list intersection for a user with
// a group grant.
func TestAlertHandler_HandleAlerts_FiltersAndGrants(t *testing.T) {
	f := newSystemAlertHandlerFixture(t)
	carolID := newGroupGrantedUser(t, f.store, "carol", f.privConn, auth.AccessLevelRead)
	carol := func(query string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts"+query, nil)
		return withUsername(withUser(req, carolID), "carol")
	}
	priv := strconv.Itoa(f.privConn)

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"unfiltered sees granted and system alerts", "", 2},
		{"granted single connection", "?connection_id=" + priv, 1},
		{"intersected connection list", "?connection_ids=" + priv + ",99999", 1},
		{"connection list with no visible ID", "?connection_ids=99999", 0},
		{"status, severity and type filters",
			"?status=active&severity=warning&alert_type=system&exclude_cleared=true", 1},
		{"time window", "?start_time=2000-01-01T00:00:00Z&end_time=2999-01-01T00:00:00Z&limit=5&offset=0", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := f.listAlerts(t, carol(tt.query))
			if len(body.Alerts) != tt.want {
				t.Errorf("got %d alerts, want %d: %+v", len(body.Alerts), tt.want, body.Alerts)
			}
		})
	}

	counts := f.counts(t, withUsername(withUser(
		httptest.NewRequest(http.MethodGet, "/api/v1/alerts/counts", nil), carolID), "carol"))
	if counts.Total != 2 || counts.System != 1 || counts.ByServer[f.privConn] != 1 {
		t.Errorf("granted user counts = %+v", counts)
	}
}

// TestAlertHandler_SystemAlert_DatastoreErrors proves each handler maps
// a datastore failure to a 500 without leaking the error.
func TestAlertHandler_SystemAlert_DatastoreErrors(t *testing.T) {
	f := newSystemAlertHandlerFixture(t)
	sys := strconv.FormatInt(f.systemID, 10)

	// A second acknowledgement of the same alert fails in the datastore.
	ackBody := `{"alert_id": ` + sys + `}`
	for i, want := range []int{http.StatusOK, http.StatusInternalServerError} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/acknowledge",
			bytes.NewReader([]byte(ackBody)))
		rec := httptest.NewRecorder()
		f.handler.handleAcknowledge(rec, withSuperuser(req))
		if rec.Code != want {
			t.Errorf("acknowledge #%d: status %d, want %d", i+1, rec.Code, want)
		}
	}

	if _, err := f.pool.Exec(context.Background(),
		`ALTER TABLE alerts DROP COLUMN ai_analysis`); err != nil {
		t.Fatalf("drop ai_analysis: %v", err)
	}
	rec := httptest.NewRecorder()
	conn := strconv.FormatInt(f.connAlert, 10)
	f.handler.handleSaveAnalysis(rec, withSuperuser(httptest.NewRequest(http.MethodPut,
		"/api/v1/alerts/analysis", bytes.NewReader([]byte(`{"alert_id": `+conn+`, "analysis": "x"}`)))))
	requireStatus(t, rec, http.StatusInternalServerError)

	// The list query selects the dropped column, so it fails too.
	rec = httptest.NewRecorder()
	f.handler.handleAlerts(rec, f.bob(httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)))
	requireStatus(t, rec, http.StatusInternalServerError)

	if _, err := f.pool.Exec(context.Background(), `DROP TABLE alerts CASCADE`); err != nil {
		t.Fatalf("drop alerts: %v", err)
	}
	rec = httptest.NewRecorder()
	f.handler.handleAlertCounts(rec, f.bob(httptest.NewRequest(http.MethodGet, "/api/v1/alerts/counts", nil)))
	requireStatus(t, rec, http.StatusInternalServerError)
}

// TestAlertHandler_RegisterRoutes_WithDatastore proves the configured
// handler routes to its methods rather than the not-configured stub.
func TestAlertHandler_RegisterRoutes_WithDatastore(t *testing.T) {
	f := newSystemAlertHandlerFixture(t)
	mux := http.NewServeMux()
	f.handler.RegisterRoutes(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withSuperuser(httptest.NewRequest(http.MethodGet, "/api/v1/alerts/counts", nil)))
	requireStatus(t, rec, http.StatusOK)
}
