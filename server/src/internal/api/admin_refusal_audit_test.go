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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Refusals for a missing admin permission are audited (issue #648)
// =============================================================================

// permissionRefusal is the reason requireAdminPermission gives when the
// caller lacks the named permission.
func permissionRefusal(permission string) string {
	return "Permission denied: requires " + permission + " permission"
}

// unreachableDatastore returns a datastore over a pool that has never
// connected and could not if asked, since nothing listens on the port.
// The permission gates under test refuse before any query, so a handler
// built on it answers 403 from the gate or fails loudly.
func unreachableDatastore(t *testing.T) *database.Datastore {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(
		"postgres://nobody@127.0.0.1:1/unreachable?connect_timeout=1")
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	t.Cleanup(pool.Close)
	return database.NewTestDatastore(pool)
}

// plainCaller creates a session user with no admin permission.
func plainCaller(t *testing.T, store *auth.AuthStore, username string) scopeCaller {
	t.Helper()
	id := newTestUser(t, store, username)
	return scopeCaller{name: username, wrap: func(r *http.Request) *http.Request {
		return withUsername(withUser(r, id), username)
	}}
}

// TestRequireAdminPermissionAudits checks that the gate records a
// refusal through the recorder installed on its auditor, including one
// installed after the gate was built, still refuses without an auditor
// or a recorder, and admits a permitted caller without a record.
func TestRequireAdminPermissionAudits(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	rc := auth.NewRBACChecker(store)
	plain := plainCaller(t, store, "gate_plain")
	reason := permissionRefusal(auth.PermManageBlackouts)

	refuses := func(t *testing.T, gate func(http.ResponseWriter, *http.Request) bool) {
		t.Helper()
		rec := httptest.NewRecorder()
		if gate(rec, newScopeRequest(plain, http.MethodPost, "")) {
			t.Fatal("Expected the plain user to be refused")
		}
		assertError(t, rec, http.StatusForbidden, reason)
	}

	t.Run("recorder installed after the gate is built", func(t *testing.T) {
		var auditor denialAuditor
		gate := requireAdminPermission(rc, &auditor, auth.PermManageBlackouts)
		capture := &denialCapture{}
		auditor.SetDenialRecorder(capture.record)
		refuses(t, gate)
		capture.expect(t, reason)
	})

	t.Run("auditor with no recorder", func(t *testing.T) {
		refuses(t, requireAdminPermission(rc, &denialAuditor{},
			auth.PermManageBlackouts))
	})

	t.Run("nil auditor", func(t *testing.T) {
		refuses(t, requireAdminPermission(rc, nil, auth.PermManageBlackouts))
	})

	t.Run("nil checker", func(t *testing.T) {
		var auditor denialAuditor
		capture := &denialCapture{}
		auditor.SetDenialRecorder(capture.record)
		refuses(t, requireAdminPermission(nil, &auditor, auth.PermManageBlackouts))
		capture.expect(t, reason)
	})

	t.Run("permitted caller", func(t *testing.T) {
		var auditor denialAuditor
		capture := &denialCapture{}
		auditor.SetDenialRecorder(capture.record)
		gate := requireAdminPermission(rc, &auditor, auth.PermManageBlackouts)
		rec := httptest.NewRecorder()
		if !gate(rec, withSuperuser(httptest.NewRequest(http.MethodPost, "/", nil))) {
			t.Fatalf("Expected a superuser to pass, got %d", rec.Code)
		}
		capture.expect(t)
	})
}

// TestAdminPermissionRefusalsAreAudited drives every route gated by
// requireAdminPermission, including each one the issue reported, with
// a session user who holds no admin permission, and checks the refusal
// is recorded under the resource's own action and target.
func TestAdminPermissionRefusalsAreAudited(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	ds := unreachableDatastore(t)
	rc := auth.NewRBACChecker(store)
	plain := plainCaller(t, store, "admin_refusal_plain")

	blackout := NewBlackoutHandler(ds, store, rc)
	alertOverride := NewAlertOverrideHandler(ds, store, rc)
	probeOverride := NewProbeOverrideHandler(ds, store, rc)
	chanOverride := NewChannelOverrideHandler(ds, store, rc)
	probeConfig := NewProbeConfigHandler(ds, store, rc)
	alertRule := NewAlertRuleHandler(ds, store, rc)
	channel := NewNotificationChannelHandlerWithSecurity(ds, store, rc,
		false, nil, nil)

	capture := &routedCapture{}
	for _, h := range []interface {
		SetDenialRecorder(func(*http.Request, string))
	}{
		blackout, alertOverride, probeOverride, chanOverride, probeConfig,
		alertRule, channel,
	} {
		h.SetDenialRecorder(capture.record)
	}

	type register = func(*http.ServeMux, func(http.HandlerFunc) http.HandlerFunc)
	cases := []struct {
		register   register
		permission string
		method     string
		path       string
		body       string
		action     string
		target     string
	}{
		// The routes issue #648 reported.
		{blackout.RegisterRoutes, auth.PermManageBlackouts, http.MethodPost,
			"/api/v1/blackouts", `{}`, "blackout.create", "blackouts"},
		{blackout.RegisterRoutes, auth.PermManageBlackouts, http.MethodPut,
			"/api/v1/blackouts/3", `{}`, "blackout.update", "blackouts/3"},
		{blackout.RegisterRoutes, auth.PermManageBlackouts, http.MethodPost,
			"/api/v1/blackout-schedules", `{}`, "blackout_schedule.create",
			"blackout-schedules"},
		{alertOverride.RegisterRoutes, auth.PermManageAlertRules, http.MethodPut,
			"/api/v1/alert-overrides/server/5/1", `{}`, "alert_override.update",
			"alert-overrides/server/5"},
		{alertOverride.RegisterRoutes, auth.PermManageAlertRules, http.MethodDelete,
			"/api/v1/alert-overrides/server/5/1", "", "alert_override.delete",
			"alert-overrides/server/5"},
		{probeOverride.RegisterRoutes, auth.PermManageProbes, http.MethodPut,
			"/api/v1/probe-overrides/cluster/1/pg_stat_activity", `{}`,
			"probe_override.update", "probe-overrides/cluster/1"},
		{chanOverride.RegisterRoutes, auth.PermManageNotificationChannels,
			http.MethodPut, "/api/v1/channel-overrides/group/1/1", `{}`,
			"channel_override.update", "channel-overrides/group/1"},
		{probeConfig.RegisterRoutes, auth.PermManageProbes, http.MethodPut,
			"/api/v1/probe-configs/34", `{}`, "probe_config.update",
			"probe-configs/34"},

		// The other routes behind the same gate.
		{blackout.RegisterRoutes, auth.PermManageBlackouts, http.MethodDelete,
			"/api/v1/blackouts/3", "", "blackout.delete", "blackouts/3"},
		{blackout.RegisterRoutes, auth.PermManageBlackouts, http.MethodPost,
			"/api/v1/blackouts/3/stop", "", "blackout.update", "blackouts/3"},
		{blackout.RegisterRoutes, auth.PermManageBlackouts, http.MethodPut,
			"/api/v1/blackout-schedules/4", `{}`, "blackout_schedule.update",
			"blackout-schedules/4"},
		{blackout.RegisterRoutes, auth.PermManageBlackouts, http.MethodDelete,
			"/api/v1/blackout-schedules/4", "", "blackout_schedule.delete",
			"blackout-schedules/4"},
		{alertOverride.RegisterRoutes, auth.PermManageAlertRules, http.MethodGet,
			"/api/v1/alert-overrides/context/5/1", "", "alert_override.read",
			"alert-overrides/server/5"},
		{probeOverride.RegisterRoutes, auth.PermManageProbes, http.MethodDelete,
			"/api/v1/probe-overrides/server/6/pg_stat_activity", "",
			"probe_override.delete", "probe-overrides/server/6"},
		{chanOverride.RegisterRoutes, auth.PermManageNotificationChannels,
			http.MethodDelete, "/api/v1/channel-overrides/cluster/2/1", "",
			"channel_override.delete", "channel-overrides/cluster/2"},
		{alertRule.RegisterRoutes, auth.PermManageAlertRules, http.MethodPut,
			"/api/v1/alert-rules/2", `{}`, "alert_rule.update", "alert-rules/2"},
		{channel.RegisterRoutes, auth.PermManageNotificationChannels,
			http.MethodGet, "/api/v1/notification-channels", "",
			"notification_channel.read", "notification-channels"},
		{channel.RegisterRoutes, auth.PermManageNotificationChannels,
			http.MethodPost, "/api/v1/notification-channels", `{}`,
			"notification_channel.create", "notification-channels"},
		{channel.RegisterRoutes, auth.PermManageNotificationChannels,
			http.MethodGet, "/api/v1/notification-channels/3", "",
			"notification_channel.read", "notification-channels/3"},
		{channel.RegisterRoutes, auth.PermManageNotificationChannels,
			http.MethodPut, "/api/v1/notification-channels/3", `{}`,
			"notification_channel.update", "notification-channels/3"},
		{channel.RegisterRoutes, auth.PermManageNotificationChannels,
			http.MethodDelete, "/api/v1/notification-channels/3", "",
			"notification_channel.delete", "notification-channels/3"},
		{channel.RegisterRoutes, auth.PermManageNotificationChannels,
			http.MethodGet, "/api/v1/notification-channels/3/recipients", "",
			"notification_channel.read", "notification-channels/3"},
		{channel.RegisterRoutes, auth.PermManageNotificationChannels,
			http.MethodPost, "/api/v1/notification-channels/3/recipients", `{}`,
			"notification_channel.update", "notification-channels/3"},
		{channel.RegisterRoutes, auth.PermManageNotificationChannels,
			http.MethodPut, "/api/v1/notification-channels/3/recipients/4", `{}`,
			"notification_channel.update", "notification-channels/3"},
		{channel.RegisterRoutes, auth.PermManageNotificationChannels,
			http.MethodDelete, "/api/v1/notification-channels/3/recipients/4", "",
			"notification_channel.update", "notification-channels/3"},
		{channel.RegisterRoutes, auth.PermManageNotificationChannels,
			http.MethodPost, "/api/v1/notification-channels/3/test", "",
			"notification_channel.update", "notification-channels/3"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			capture.denials = nil
			reason := permissionRefusal(tc.permission)
			rec := serveRoutes(tc.register, plain, tc.method, tc.path, tc.body)
			assertError(t, rec, http.StatusForbidden, reason)
			capture.expect(t, routedDenial{tc.action, tc.target, reason})
		})
	}

	t.Run("recorded in the audit log", func(t *testing.T) {
		blackout.SetDenialRecorder(NewRBACHandler(store, rc).RecordDenial)
		reason := permissionRefusal(auth.PermManageBlackouts)
		rec := serveRoutes(blackout.RegisterRoutes, plain, http.MethodPost,
			"/api/v1/blackouts", `{}`)
		assertError(t, rec, http.StatusForbidden, reason)
		assertDenialRecorded(t, store, "blackout.create", reason)
	})
}

// TestClusterPermissionRefusalsAreAudited checks that the cluster and
// relationship routes record their manage_connections refusals.
func TestClusterPermissionRefusalsAreAudited(t *testing.T) {
	manageConnections := permissionRefusal(auth.PermManageConnections)

	f := newClusterCovFixture(t)
	capture := &routedCapture{}
	f.handler.SetDenialRecorder(capture.record)

	cases := []struct {
		method string
		path   string
		body   string
		want   routedDenial
	}{
		{http.MethodPost, "/api/v1/clusters", `{"name":"Refused"}`,
			routedDenial{"cluster.create", "clusters", manageConnections}},
		{http.MethodPut, "/api/v1/clusters/1", `{"name":"Refused"}`,
			routedDenial{"cluster.update", "clusters/1", manageConnections}},
		{http.MethodDelete, "/api/v1/clusters/1", "",
			routedDenial{"cluster.delete", "clusters/1", manageConnections}},
		{http.MethodPut, "/api/v1/clusters/cluster-spock-auto1", `{"name":"Refused"}`,
			routedDenial{"cluster.update", "clusters",
				"Permission denied: you do not have permission to modify auto-detected clusters"}},
		{http.MethodDelete, "/api/v1/clusters/cluster-spock-auto1", "",
			routedDenial{"cluster.delete", "clusters",
				"Permission denied: you do not have permission to delete auto-detected clusters"}},
		{http.MethodPost, "/api/v1/clusters/1/servers", `{"connection_id":5}`,
			routedDenial{"cluster.update", "clusters/1", manageConnections}},
		{http.MethodDelete, "/api/v1/clusters/1/servers/6", "",
			routedDenial{"cluster.update", "clusters/1", manageConnections}},
		{http.MethodPut, "/api/v1/clusters/1/connections/5/relationships",
			`{"relationships":[]}`,
			routedDenial{"cluster.update", "clusters/1", manageConnections}},
		{http.MethodDelete, "/api/v1/clusters/1/relationships/1", "",
			routedDenial{"cluster.update", "clusters/1", manageConnections}},
		{http.MethodDelete, "/api/v1/clusters/1/connections/5/relationships", "",
			routedDenial{"cluster.update", "clusters/1", manageConnections}},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			capture.denials = nil
			rec := clusterCovServe(f.handler, f.plain, tc.method, tc.path, tc.body)
			assertError(t, rec, http.StatusForbidden, tc.want.reason)
			capture.expect(t, tc.want)
		})
	}
}

// TestConnectionPermissionRefusalsAreAudited checks that the connection
// routes record a refusal for a missing manage_connections permission
// or a connection the caller does not own.
func TestConnectionPermissionRefusalsAreAudited(t *testing.T) {
	manageConnections := permissionRefusal(auth.PermManageConnections)

	f := newConnCovFixture(t, "")
	plain := f.newCaller(t, "conn_refusal_plain")
	other := f.seed(t, "other", "someone_else", true)
	own := f.seed(t, "own", plain.username, false)

	capture := &routedCapture{}
	f.handler.SetDenialRecorder(capture.record)

	t.Run("create", func(t *testing.T) {
		capture.denials = nil
		rec := httptest.NewRecorder()
		f.handler.handleConnections(rec, asCaller(connCovRequest(t,
			http.MethodPost, "/api/v1/connections", `{}`), plain))
		assertError(t, rec, http.StatusForbidden, manageConnections)
		capture.expect(t, routedDenial{"connection.create", "connections",
			manageConnections})
	})

	cases := []struct {
		name   string
		method string
		id     int
		suffix string
		body   string
		reason string
		action string
	}{
		{"update not owned", http.MethodPut, other, "", `{}`,
			"Permission denied: you must be the owner or have the manage_connections permission to update this connection",
			"connection.update"},
		{"delete not owned", http.MethodDelete, other, "", "",
			"Permission denied: you must be the owner or have the manage_connections permission to delete this connection",
			"connection.delete"},
		{"share own", http.MethodPut, own, "", `{"is_shared":true}`,
			"Permission denied: you do not have permission to make connections shared",
			"connection.update"},
		{"move to cluster", http.MethodPut, other, "cluster", `{}`,
			manageConnections, "connection.cluster.update"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture.denials = nil
			var body any
			if tc.body != "" {
				body = tc.body
			}
			rec := f.serve(asCaller(connCovRequest(t, tc.method,
				connCovPath(tc.id, tc.suffix), body), plain))
			assertError(t, rec, http.StatusForbidden, tc.reason)
			capture.expect(t, routedDenial{tc.action,
				fmt.Sprintf("connections/%d", tc.id), tc.reason})
		})
	}
}

// oneRowCase is one refusal TestRefusalWritesOneAuditRow sends, with
// the reason, action and target its single audit row must carry.
type oneRowCase struct {
	name   string
	serve  func() *httptest.ResponseRecorder
	reason string
	action string
	target string
}

// TestRefusalWritesOneAuditRow checks, against the real denial
// recorder, that one refusal on each route that both this gate and the
// connection-access auditing (issue #675) touch writes exactly one
// audit row naming its action and target, rather than one from each.
func TestRefusalWritesOneAuditRow(t *testing.T) {
	f := newConnCovFixture(t, "")
	plain := f.newCaller(t, "one_row_plain")
	session := scopeCaller{name: plain.username,
		wrap: func(r *http.Request) *http.Request { return asCaller(r, plain) }}
	other := f.seed(t, "one-row-other", "someone_else", true)

	rc := auth.NewRBACChecker(f.store)
	record := NewRBACHandler(f.store, rc).RecordDenial
	f.handler.SetDenialRecorder(record)

	alerts := NewAlertHandler(nil, f.store, rc)
	alerts.setAlertResolver(&fakeAlertResolver{system: true})
	alerts.SetDenialRecorder(record)

	blackout := NewBlackoutHandler(unreachableDatastore(t), f.store, rc)
	blackout.SetDenialRecorder(record)

	// A connection alert the caller can see but may not change: the
	// lister reports it shared whilst the access check reports it
	// private (see TestAlertRefusalsAreAudited).
	connAlerts := NewAlertHandler(nil, f.store,
		mockSharingChecker(t, f.store, other, "someone_else", false))
	connAlerts.setAlertResolver(&fakeAlertResolver{connID: other})
	connAlerts.setVisibilityLister(singleConnectionLister(other,
		"someone_else", true))
	connAlerts.SetDenialRecorder(record)

	// The override writes check visibility before the token scope
	// (issue #672), so a token that sees the target only at read is the
	// caller that still draws a refusal.
	readOnlyID := mustCreateScopedToken(t, f.store, "one_row_read_only", nil)
	if err := f.store.AsActor(auth.SystemActor()).SetTokenConnectionScope(
		readOnlyID, []auth.ScopedConnection{
			{ConnectionID: other, AccessLevel: auth.AccessLevelRead},
		}, true); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	readOnly := scopeCaller{name: "one_row_read_only",
		wrap: func(r *http.Request) *http.Request {
			return withSuperuserToken(r, readOnlyID)
		}}
	alertOverride := NewAlertOverrideHandler(f.ds, f.store, rc)
	probeOverride := NewProbeOverrideHandler(f.ds, f.store, rc)
	chanOverride := NewChannelOverrideHandler(f.ds, f.store, rc)
	for _, h := range []interface {
		SetDenialRecorder(func(*http.Request, string))
	}{alertOverride, probeOverride, chanOverride} {
		h.SetDenialRecorder(record)
	}
	overrideCase := func(name string,
		register func(*http.ServeMux, func(http.HandlerFunc) http.HandlerFunc),
		method, kind, suffix, body, action string) oneRowCase {
		return oneRowCase{name, func() *httptest.ResponseRecorder {
			return serveRoutes(register, readOnly, method,
				fmt.Sprintf("/api/v1/%s/server/%d/%s", kind, other, suffix), body)
		}, targetOutOfTokenScope, action,
			fmt.Sprintf("%s/server/%d", kind, other)}
	}
	alertCase := func(name string, h *AlertHandler,
		serve func(*AlertHandler, http.ResponseWriter, *http.Request),
		method, path, body, reason, action string, alertID int64) oneRowCase {
		return oneRowCase{name, func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			serve(h, rec, asCaller(connCovRequest(t, method, path, body), plain))
			return rec
		}, reason, action, alertDenialTarget(alertID)}
	}

	manageConnections := permissionRefusal(auth.PermManageConnections)
	cases := []oneRowCase{
		{"connection cluster move", func() *httptest.ResponseRecorder {
			return f.serve(asCaller(connCovRequest(t, http.MethodPut,
				connCovPath(other, "cluster"), `{}`), plain))
		}, manageConnections, deniedActionConnectionClusterUpdate,
			connectionDenialTarget(other)},
		{"connection create", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			f.handler.handleConnections(rec, asCaller(connCovRequest(t,
				http.MethodPost, "/api/v1/connections", `{}`), plain))
			return rec
		}, manageConnections, "connection.create", "connections"},
		{"system alert acknowledge", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			alerts.acknowledgeAlert(rec, asCaller(connCovRequest(t,
				http.MethodPost, "/api/v1/alerts/acknowledge",
				`{"alert_id": 9}`), plain))
			return rec
		}, systemAlertManageDenied, deniedActionAlertAcknowledge,
			alertDenialTarget(9)},
		{"blackout create", func() *httptest.ResponseRecorder {
			return serveRoutes(blackout.RegisterRoutes, session,
				http.MethodPost, "/api/v1/blackouts", `{}`)
		}, permissionRefusal(auth.PermManageBlackouts), "blackout.create",
			"blackouts"},
		alertCase("system alert unacknowledge", alerts,
			(*AlertHandler).handleAcknowledge, http.MethodDelete,
			"/api/v1/alerts/acknowledge?alert_id=9", "",
			systemAlertManageDenied, deniedActionAlertUnacknowledge, 9),
		alertCase("connection alert acknowledge", connAlerts,
			(*AlertHandler).handleAcknowledge, http.MethodPost,
			"/api/v1/alerts/acknowledge", `{"alert_id": 10}`,
			alertAccessDenied, deniedActionAlertAcknowledge, 10),
		alertCase("connection alert unacknowledge", connAlerts,
			(*AlertHandler).handleAcknowledge, http.MethodDelete,
			"/api/v1/alerts/acknowledge?alert_id=10", "",
			alertAccessDenied, deniedActionAlertUnacknowledge, 10),
		alertCase("connection alert analysis", connAlerts,
			(*AlertHandler).handleSaveAnalysis, http.MethodPut,
			"/api/v1/alerts/analysis", `{"alert_id": 10, "analysis": "x"}`,
			alertAccessDenied, deniedActionAlertAnalysisUpdate, 10),
		overrideCase("alert override upsert", alertOverride.RegisterRoutes,
			http.MethodPut, "alert-overrides", "1", `{}`,
			"alert_override.update"),
		overrideCase("alert override delete", alertOverride.RegisterRoutes,
			http.MethodDelete, "alert-overrides", "1", "",
			"alert_override.delete"),
		overrideCase("probe override upsert", probeOverride.RegisterRoutes,
			http.MethodPut, "probe-overrides", "pg_stat_activity", `{}`,
			"probe_override.update"),
		overrideCase("probe override delete", probeOverride.RegisterRoutes,
			http.MethodDelete, "probe-overrides", "pg_stat_activity", "",
			"probe_override.delete"),
		overrideCase("channel override upsert", chanOverride.RegisterRoutes,
			http.MethodPut, "channel-overrides", "1", `{}`,
			"channel_override.update"),
		overrideCase("channel override delete", chanOverride.RegisterRoutes,
			http.MethodDelete, "channel-overrides", "1", "",
			"channel_override.delete"),
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := tc.serve(); rec.Code != http.StatusForbidden {
				t.Fatalf("Expected 403, got %d: %s", rec.Code, rec.Body)
			}
			events, _, err := f.store.ListAuditEvents(auth.AuditFilter{
				Outcome: string(auth.OutcomeDenied)})
			if err != nil {
				t.Fatalf("ListAuditEvents failed: %v", err)
			}
			if len(events) != i+1 {
				t.Fatalf("Expected %d denied rows after this refusal, got %d",
					i+1, len(events))
			}
			matched := 0
			for _, e := range events {
				var details struct {
					Target string `json:"target"`
				}
				if err := json.Unmarshal(e.Details, &details); err != nil {
					t.Fatalf("Failed to decode details %s: %v", e.Details, err)
				}
				if e.Action == tc.action && details.Target == tc.target {
					matched++
					if e.Error != tc.reason {
						t.Errorf("Expected reason %q, got %q", tc.reason, e.Error)
					}
				}
			}
			if matched != 1 {
				t.Errorf("Expected exactly 1 %s row on %s, got %d", tc.action,
					tc.target, matched)
			}
		})
	}

	// A target the caller cannot see answers as missing and writes no
	// row, since a row would itself confirm the target exists (issues
	// #672 and #677).
	t.Run("hidden targets write no row", func(t *testing.T) {
		hidden := f.seed(t, "one-row-hidden", "someone_else", true)
		hiddenAlerts := NewAlertHandler(nil, f.store,
			mockSharingChecker(t, f.store, hidden, "someone_else", false))
		hiddenAlerts.setAlertResolver(&fakeAlertResolver{connID: hidden})
		hiddenAlerts.setVisibilityLister(&stubVisibilityLister{})
		hiddenAlerts.SetDenialRecorder(record)

		denied := func() int {
			events, _, err := f.store.ListAuditEvents(auth.AuditFilter{
				Outcome: string(auth.OutcomeDenied)})
			if err != nil {
				t.Fatalf("ListAuditEvents failed: %v", err)
			}
			return len(events)
		}
		before := denied()

		rec := serveRoutes(probeOverride.RegisterRoutes, readOnly,
			http.MethodDelete, fmt.Sprintf(
				"/api/v1/probe-overrides/server/%d/pg_stat_activity", hidden), "")
		expectStatus(t, rec, http.StatusNotFound)
		rec = httptest.NewRecorder()
		hiddenAlerts.acknowledgeAlert(rec, asCaller(connCovRequest(t,
			http.MethodPost, "/api/v1/alerts/acknowledge",
			`{"alert_id": 9}`), plain))
		expectStatus(t, rec, http.StatusNotFound)

		if after := denied(); after != before {
			t.Errorf("Expected no new denied rows, got %d", after-before)
		}
	})
}
