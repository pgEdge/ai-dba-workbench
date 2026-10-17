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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Connection-access refusals are audited (issue #675)
// =============================================================================

// recordedDenial is one refusal as the RBAC handler's recorder would
// write it: the action and target deniedRoute derives from the request,
// and the reason.
type recordedDenial struct {
	action, target, reason string
}

// targetCapture records refusals the way RBACHandler.RecordDenial sees
// them, without an auth store.
type targetCapture struct {
	denials []recordedDenial
}

func (c *targetCapture) record(r *http.Request, reason string) {
	action, target := deniedRoute(r)
	c.denials = append(c.denials, recordedDenial{action, target, reason})
}

// TestDeniedRouteHonorsAttachedTarget checks that a target attached by
// the refusing handler takes precedence over the path mapping, and that
// a request without one still maps by path.
func TestDeniedRouteHonorsAttachedTarget(t *testing.T) {
	r := httptest.NewRequest(http.MethodPut, "/api/v1/connections/7", nil)
	if action, target := deniedRoute(r); action != "connection.update" ||
		target != "connections/7" {
		t.Fatalf("Expected the path mapping, got %q %q", action, target)
	}

	r = withDeniedTarget(r, deniedActionConnectionRead, "connections/9")
	if action, target := deniedRoute(r); action != deniedActionConnectionRead ||
		target != "connections/9" {
		t.Errorf("Expected the attached target, got %q %q", action, target)
	}
}

// TestDenialTargetNames checks the target names, including that an id
// that is not positive leaves only the kind.
func TestDenialTargetNames(t *testing.T) {
	cases := []struct{ got, want string }{
		{connectionDenialTarget(42), "connections/42"},
		{connectionDenialTarget(0), "connections"},
		{connectionDenialTarget(-3), "connections"},
		{alertDenialTarget(17), "alerts/17"},
		{alertDenialTarget(0), "alerts"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("Expected %q, got %q", c.want, c.got)
		}
	}
}

// TestRefuseTarget checks that refuseTarget answers 403 with the
// message, records the fixed reason with the attached action and
// target, and still refuses with no recorder.
func TestRefuseTarget(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/query", nil)

	capture := &targetCapture{}
	rec := httptest.NewRecorder()
	denialRecorder(capture.record).refuseConnection(rec, r,
		deniedActionMetricsRead, 5, "shown to the client")
	assertForbiddenMessage(t, rec, "shown to the client")
	want := recordedDenial{deniedActionMetricsRead, "connections/5",
		connectionAccessDenied}
	if len(capture.denials) != 1 || capture.denials[0] != want {
		t.Errorf("Expected %v, got %v", want, capture.denials)
	}

	rec = httptest.NewRecorder()
	denialRecorder(nil).refuseConnection(rec, r, deniedActionMetricsRead,
		5, "shown to the client")
	assertForbiddenMessage(t, rec, "shown to the client")
}

// assertForbiddenMessage fails unless rec is a 403 whose error is want.
func assertForbiddenMessage(t *testing.T, rec *httptest.ResponseRecorder,
	want string) {

	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Failed to decode %s: %v", rec.Body.String(), err)
	}
	if body.Error != want {
		t.Errorf("Expected error %q, got %q", want, body.Error)
	}
}

// accessRefusalCase is one connection-access refusal: the handler that
// makes it, the request that reaches it, and what it must record.
type accessRefusalCase struct {
	name    string
	handler interface {
		SetDenialRecorder(func(*http.Request, string))
	}
	serve   func(http.ResponseWriter, *http.Request)
	method  string
	path    string
	body    string
	message string
	want    recordedDenial
}

// accessRefusalCases builds every connection-access refusal reachable
// without a database. Each handler is built on a nil auth store, so its
// RBAC checker refuses every connection and every alert.
func accessRefusalCases() []accessRefusalCase {
	ds := database.NewTestDatastore(nil)
	denyAll := auth.NewRBACChecker(nil)

	metrics := NewMetricsHandler(ds, nil)
	latest := NewLatestSnapshotHandler(ds, nil)
	perf := NewPerfSummaryHandler(ds, nil)
	info := NewServerInfoHandler(ds, nil, denyAll, nil)
	conn := NewConnectionHandlerWithSecurity(ds, nil, denyAll, true, nil, nil)
	alerts := NewAlertHandler(nil, nil, denyAll)
	alerts.alertResolver = &fakeAlertResolver{connID: 7}
	systemAlerts := NewAlertHandler(nil, nil, denyAll)
	systemAlerts.alertResolver = &fakeAlertResolver{system: true}

	byID := func(id int) string {
		return "Permission denied: you do not have access to connection " +
			strings.TrimPrefix(connectionDenialTarget(id), "connections/")
	}
	read := func(action string, id int) recordedDenial {
		return recordedDenial{action, connectionDenialTarget(id),
			connectionAccessDenied}
	}
	alert := func(action string, id int64) recordedDenial {
		return recordedDenial{action, alertDenialTarget(id), alertAccessDenied}
	}
	systemAlert := func(action string, id int64) recordedDenial {
		return recordedDenial{action, alertDenialTarget(id),
			systemAlertManageDenied}
	}
	withID := func(serve func(http.ResponseWriter, *http.Request, int)) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) { serve(w, r, 7) }
	}

	return []accessRefusalCase{
		{"metrics query", metrics, metrics.handleMetricsQuery,
			http.MethodGet, "/api/v1/metrics/query?connection_ids=7,8", "",
			byID(7), read(deniedActionMetricsRead, 7)},
		{"metrics baselines", metrics, metrics.handleMetricsBaselines,
			http.MethodGet, "/api/v1/metrics/baselines?connection_id=7", "",
			byID(7), read(deniedActionMetricsRead, 7)},
		{"latest snapshot", latest, latest.handleLatestSnapshot,
			http.MethodGet, "/api/v1/metrics/latest?connection_id=7", "",
			byID(7), read(deniedActionMetricsRead, 7)},
		{"performance summary", perf, perf.handlePerfSummary,
			http.MethodGet, "/api/v1/metrics/performance-summary?connection_ids=7,8", "",
			byID(7), read(deniedActionMetricsRead, 7)},
		{"database summaries", perf, perf.handleDatabaseSummaries,
			http.MethodGet, "/api/v1/metrics/database-summaries?connection_id=7", "",
			byID(7), read(deniedActionMetricsRead, 7)},
		{"top queries", perf, perf.handleTopQueries,
			http.MethodGet, "/api/v1/metrics/top-queries?connection_id=7", "",
			byID(7), read(deniedActionMetricsRead, 7)},
		{"query stats", perf, perf.handleQueryStats,
			http.MethodGet, "/api/v1/metrics/query-stats?connection_id=7", "",
			byID(7), read(deniedActionMetricsRead, 7)},
		{"connection groups", perf, perf.handleConnectionGroups,
			http.MethodGet, "/api/v1/metrics/connection-groups?connection_id=7", "",
			byID(7), read(deniedActionMetricsRead, 7)},
		{"server info", info, info.handleServerInfoRouting,
			http.MethodGet, "/api/v1/server-info/7", "",
			connectionAccessDenied, read(deniedActionServerInfoRead, 7)},
		{"server info analysis", info, info.handleServerInfoRouting,
			http.MethodGet, "/api/v1/server-info/7/ai-analysis", "",
			connectionAccessDenied, read(deniedActionServerInfoRead, 7)},
		{"execute query", conn, withID(conn.executeQuery),
			http.MethodPost, "/api/v1/connections/7/query", "",
			connectionAccessDenied, read(deniedActionConnectionQuery, 7)},
		{"validate query", conn, withID(conn.validateQuery),
			http.MethodPost, "/api/v1/connections/7/query/validate", "",
			connectionAccessDenied, read(deniedActionConnectionQueryValidate, 7)},
		{"get connection", conn, withID(conn.getConnection),
			http.MethodGet, "/api/v1/connections/7", "",
			"Access denied", read(deniedActionConnectionRead, 7)},
		{"list databases", conn, withID(conn.listDatabases),
			http.MethodGet, "/api/v1/connections/7/databases", "",
			"Access denied", read(deniedActionConnectionRead, 7)},
		{"connection context", conn, withID(conn.getConnectionContext),
			http.MethodGet, "/api/v1/connections/7/context", "",
			"Access denied", read(deniedActionConnectionRead, 7)},
		{"connection cluster", conn, withID(conn.handleGetConnectionCluster),
			http.MethodGet, "/api/v1/connections/7/cluster", "",
			"Access denied", read(deniedActionConnectionRead, 7)},
		{"move connection cluster", conn, withID(conn.handleUpdateConnectionCluster),
			http.MethodPut, "/api/v1/connections/7/cluster", "",
			"Access denied", read(deniedActionConnectionClusterUpdate, 7)},
		{"select current connection", conn, func(w http.ResponseWriter, r *http.Request) {
			conn.setCurrentConnection(w, r, "token-hash")
		}, http.MethodPost, "/api/v1/connections/current", `{"connection_id":7}`,
			"Access denied", read(deniedActionConnectionCurrentUpdate, 7)},
		{"acknowledge alert", alerts, alerts.handleAcknowledge,
			http.MethodPost, "/api/v1/alerts/acknowledge", `{"alert_id":11}`,
			"Access denied", alert(deniedActionAlertAcknowledge, 11)},
		{"unacknowledge alert", alerts, alerts.handleAcknowledge,
			http.MethodDelete, "/api/v1/alerts/acknowledge?alert_id=11", "",
			"Access denied", alert(deniedActionAlertUnacknowledge, 11)},
		{"acknowledge system alert", systemAlerts, systemAlerts.handleAcknowledge,
			http.MethodPost, "/api/v1/alerts/acknowledge", `{"alert_id":12}`,
			"Access denied", systemAlert(deniedActionAlertAcknowledge, 12)},
		{"unacknowledge system alert", systemAlerts, systemAlerts.handleAcknowledge,
			http.MethodDelete, "/api/v1/alerts/acknowledge?alert_id=12", "",
			"Access denied", systemAlert(deniedActionAlertUnacknowledge, 12)},
		{"save alert analysis", alerts, alerts.handleSaveAnalysis,
			http.MethodPut, "/api/v1/alerts/analysis", `{"alert_id":11,"analysis":"x"}`,
			"Access denied", alert(deniedActionAlertAnalysisUpdate, 11)},
		{"save system alert analysis", systemAlerts, systemAlerts.handleSaveAnalysis,
			http.MethodPut, "/api/v1/alerts/analysis", `{"alert_id":12,"analysis":"x"}`,
			"Access denied", alert(deniedActionAlertAnalysisUpdate, 12)},
	}
}

// TestConnectionAccessRefusalsAreAudited checks that every
// connection-access refusal on the read routes, and the access refusals
// on the query and alert routes, answers 403 with its usual message and
// records one denial naming the object, with a reason that does not.
func TestConnectionAccessRefusalsAreAudited(t *testing.T) {
	for _, c := range accessRefusalCases() {
		t.Run(c.name, func(t *testing.T) {
			capture := &targetCapture{}
			c.handler.SetDenialRecorder(capture.record)

			var body *strings.Reader
			if c.body != "" {
				body = strings.NewReader(c.body)
			} else {
				body = strings.NewReader("")
			}
			r := httptest.NewRequest(c.method, c.path, body)
			rec := httptest.NewRecorder()
			c.serve(rec, r)

			assertForbiddenMessage(t, rec, c.message)
			if len(capture.denials) != 1 || capture.denials[0] != c.want {
				t.Errorf("Expected %v, got %v", c.want, capture.denials)
			}
		})
	}
}

// TestConnectionAccessRefusalsWithoutRecorder checks that each refusal
// still answers 403 when no recorder is wired, as when the server runs
// without an auth store.
func TestConnectionAccessRefusalsWithoutRecorder(t *testing.T) {
	for _, c := range accessRefusalCases() {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(c.method, c.path,
				strings.NewReader(c.body))
			rec := httptest.NewRecorder()
			c.serve(rec, r)
			assertForbiddenMessage(t, rec, c.message)
		})
	}
}

// TestConnectionAccessRefusalReachesAuditLog checks, end to end, that a
// connection-access refusal is written to the audit log through the
// RBAC handler's recorder, and that a caller probing several connection
// ids opens one coalesced window rather than a row per id.
func TestConnectionAccessRefusalReachesAuditLog(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	h := NewMetricsHandler(database.NewTestDatastore(nil), nil)
	h.SetDenialRecorder(NewRBACHandler(store, auth.NewRBACChecker(store)).RecordDenial)

	for _, id := range []string{"7", "8", "9"} {
		r := withUser(httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/baselines?connection_id="+id, nil), 1)
		rec := httptest.NewRecorder()
		h.handleMetricsBaselines(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("Expected 403 for connection %s, got %d", id, rec.Code)
		}
	}

	assertDenialRecorded(t, store, deniedActionMetricsRead,
		connectionAccessDenied)
	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Action: deniedActionMetricsRead, Outcome: string(auth.OutcomeDenied)})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if !strings.Contains(string(events[0].Details), `"connections/7"`) {
		t.Errorf("Expected the first denial to name connections/7, got %s",
			events[0].Details)
	}
}

// TestQueryWriteRefusalReachesAuditLog checks, end to end, that a
// caller with only read access to a connection who confirms a write
// statement is refused, and that the refusal is written to the audit
// log as connection.query.write on the connection.
func TestQueryWriteRefusalReachesAuditLog(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	const connID = 675
	userID := newGroupGrantedUser(t, store, "issue675_reader", connID,
		auth.AccessLevelRead)

	h := NewConnectionHandlerWithSecurity(nil, store,
		auth.NewRBACChecker(store), false, nil, nil)
	h.SetDenialRecorder(NewRBACHandler(store, auth.NewRBACChecker(store)).RecordDenial)

	r := withUser(httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/675/query",
		strings.NewReader(`{"query":"DELETE FROM t","confirmed":true}`)),
		userID)
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.executeQuery(rec, r, connID)
	assertForbiddenMessage(t, rec, connectionWriteDenied)

	assertDenialRecorded(t, store, deniedActionConnectionQueryWrite,
		connectionWriteDenied)
	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Action:  deniedActionConnectionQueryWrite,
		Outcome: string(auth.OutcomeDenied)})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	var details struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(events[0].Details, &details); err != nil {
		t.Fatalf("Failed to decode details %s: %v", events[0].Details, err)
	}
	if want := connectionDenialTarget(connID); details.Target != want {
		t.Errorf("Expected target %q, got %q", want, details.Target)
	}
}
