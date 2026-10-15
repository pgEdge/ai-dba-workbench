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
