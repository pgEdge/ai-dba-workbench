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
)

// =============================================================================
// Blackout, override, probe config and cluster-group refusals are
// audited (issue #589)
// =============================================================================

// routedDenial is one refusal as the audit log would name it.
type routedDenial struct {
	action, target, reason string
}

// routedCapture records each refusal passed to a denial recorder,
// together with the action and target deniedRoute derives from the
// request, so that a test can check both the wiring and the vocabulary
// the row would carry.
type routedCapture struct {
	denials []routedDenial
}

func (c *routedCapture) record(r *http.Request, reason string) {
	action, target := deniedRoute(r)
	c.denials = append(c.denials, routedDenial{action, target, reason})
}

// expect fails unless exactly the given refusals were recorded.
func (c *routedCapture) expect(t *testing.T, want ...routedDenial) {
	t.Helper()
	if len(c.denials) != len(want) {
		t.Fatalf("Expected %d recorded denials %v, got %v", len(want), want,
			c.denials)
	}
	for i := range want {
		if c.denials[i] != want[i] {
			t.Errorf("Denial %d: expected %+v, got %+v", i, want[i],
				c.denials[i])
		}
	}
}

// serveRoutes routes one request through the routes register installs,
// as the server does, with an identity auth wrapper.
func serveRoutes(register func(*http.ServeMux, func(http.HandlerFunc) http.HandlerFunc),
	caller scopeCaller, method, path, body string) *httptest.ResponseRecorder {

	mux := http.NewServeMux()
	register(mux, func(next http.HandlerFunc) http.HandlerFunc { return next })

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, caller.wrap(req))
	return rec
}

// TestTargetScopeGatesAudit checks that the blackout and override
// target gates record a refusal through the recorder they are given,
// record nothing when they admit the request, and still refuse with no
// recorder.
func TestTargetScopeGatesAudit(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	rc := auth.NewRBACChecker(store)
	session, _, wildcard, narrowed, _ := scopedCallers(t, store)

	gates := []struct {
		name string
		gate func(http.ResponseWriter, *http.Request, denialRecorder) bool
	}{
		{"override target", func(w http.ResponseWriter, r *http.Request, rec denialRecorder) bool {
			return requireTargetInTokenScope(w, r, rc, rec, "estate", nil)
		}},
		{"blackout target", func(w http.ResponseWriter, r *http.Request, rec denialRecorder) bool {
			return requireBlackoutTargetInTokenScope(w, r, rc, rec, "estate", nil)
		}},
	}
	for _, g := range gates {
		t.Run(g.name, func(t *testing.T) {
			capture := &denialCapture{}
			rec := httptest.NewRecorder()
			if g.gate(rec, newScopeRequest(narrowed, http.MethodPut, ""), capture.record) {
				t.Fatal("Expected the narrowed token to be refused")
			}
			assertOutOfTokenScope(t, rec)
			capture.expect(t, targetOutOfTokenScope)

			rec = httptest.NewRecorder()
			if g.gate(rec, newScopeRequest(narrowed, http.MethodPut, ""), nil) {
				t.Fatal("Expected a refusal with no recorder")
			}
			assertOutOfTokenScope(t, rec)

			for _, caller := range []scopeCaller{session, wildcard} {
				capture := &denialCapture{}
				rec := httptest.NewRecorder()
				if !g.gate(rec, newScopeRequest(caller, http.MethodPut, ""), capture.record) {
					t.Fatalf("%s: expected the gate to pass, got %d", caller.name, rec.Code)
				}
				capture.expect(t)
			}
		})
	}
}

// TestOverrideAndProbeConfigRefusalsAreAudited drives each override and
// probe config write route with a token whose connection scope does not
// cover the target, and checks the refusal is recorded under the
// resource's own action and target.
func TestOverrideAndProbeConfigRefusalsAreAudited(t *testing.T) {
	const alertBody = `{"operator":">","threshold":1,"severity":"warning","enabled":true}`
	const probeBody = `{"is_enabled":false,"collection_interval_seconds":30,"retention_days":7}`

	g := newEstateGates(t)
	narrowed, readOnly := g.callers["narrowed"], g.callers["readOnly"]
	capture := &routedCapture{}
	g.alertOverride.SetDenialRecorder(capture.record)
	g.probeOverride.SetDenialRecorder(capture.record)
	g.chanOverride.SetDenialRecorder(capture.record)
	g.probeConfig.SetDenialRecorder(capture.record)

	cases := []struct {
		name     string
		register func(*http.ServeMux, func(http.HandlerFunc) http.HandlerFunc)
		method   string
		path     string
		body     string
		want     routedDenial
		// caller defaults to the narrowed token.
		caller *scopeCaller
	}{
		{"alert override upsert", g.alertOverride.RegisterRoutes, http.MethodPut,
			"/api/v1/alert-overrides/group/1/1", alertBody,
			routedDenial{"alert_override.update", "alert-overrides/group/1", targetOutOfTokenScope}, nil},
		{"alert override delete", g.alertOverride.RegisterRoutes, http.MethodDelete,
			"/api/v1/alert-overrides/cluster/1/1", "",
			routedDenial{"alert_override.delete", "alert-overrides/cluster/1", targetOutOfTokenScope}, nil},
		{"probe override upsert", g.probeOverride.RegisterRoutes, http.MethodPut,
			"/api/v1/probe-overrides/cluster/1/pg_stat_activity", probeBody,
			routedDenial{"probe_override.update", "probe-overrides/cluster/1", targetOutOfTokenScope}, nil},
		// A read-only token sees connection 5 but may not change it.
		{"probe override delete", g.probeOverride.RegisterRoutes, http.MethodDelete,
			"/api/v1/probe-overrides/server/5/pg_stat_activity", "",
			routedDenial{"probe_override.delete", "probe-overrides/server/5", targetOutOfTokenScope},
			&readOnly},
		{"channel override upsert", g.chanOverride.RegisterRoutes, http.MethodPut,
			"/api/v1/channel-overrides/group/1/1", `{"enabled":false}`,
			routedDenial{"channel_override.update", "channel-overrides/group/1", targetOutOfTokenScope}, nil},
		{"channel override delete", g.chanOverride.RegisterRoutes, http.MethodDelete,
			"/api/v1/channel-overrides/group/1/1", "",
			routedDenial{"channel_override.delete", "channel-overrides/group/1", targetOutOfTokenScope}, nil},
		{"probe config update", g.probeConfig.RegisterRoutes, http.MethodPut,
			"/api/v1/probe-configs/1", `{"is_enabled":false}`,
			routedDenial{"probe_config.update", "probe-configs/1", targetOutOfTokenScope}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture.denials = nil
			caller := narrowed
			if tc.caller != nil {
				caller = *tc.caller
			}
			rec := serveRoutes(tc.register, caller, tc.method, tc.path, tc.body)
			assertOutOfTokenScope(t, rec)
			capture.expect(t, tc.want)
		})
	}

	// A connection outside the token's scope is hidden from it, so the
	// write is answered as missing and no refusal is recorded, since a
	// recorded 403 would itself confirm the target exists (issue #672).
	t.Run("hidden target records nothing", func(t *testing.T) {
		capture.denials = nil
		rec := serveRoutes(g.probeOverride.RegisterRoutes, narrowed,
			http.MethodDelete, "/api/v1/probe-overrides/server/6/pg_stat_activity", "")
		expectStatus(t, rec, http.StatusNotFound)
		capture.expect(t)
	})

	t.Run("admitted write records nothing", func(t *testing.T) {
		capture.denials = nil
		rec := serveRoutes(g.alertOverride.RegisterRoutes, narrowed,
			http.MethodPut, "/api/v1/alert-overrides/server/5/1", alertBody)
		expectStatus(t, rec, http.StatusOK)
		capture.expect(t)
	})
}

// TestBlackoutRefusalsAreAudited checks that the blackout handler's
// token-scope refusals are recorded, both the gate on the requested
// target and refuseOutOfScope's 403 for a stored record, and that
// refuseOutOfScope's 404 for a record the token cannot see is not.
func TestBlackoutRefusalsAreAudited(t *testing.T) {
	const estateSchedule = `{"scope":"estate","name":"n",` +
		`"cron_expression":"0 * * * *","duration_minutes":30,"reason":"r"}`

	h, _, callers := newBlackoutCoverageHandler(t)
	narrowed := callers["narrowed"]
	capture := &routedCapture{}
	h.SetDenialRecorder(capture.record)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
		want   []routedDenial
	}{
		{"create estate schedule", http.MethodPost, "/api/v1/blackout-schedules",
			estateSchedule, http.StatusForbidden, []routedDenial{
				{"blackout_schedule.create", "blackout-schedules", targetOutOfTokenScope}}},
		{"update visible cluster blackout", http.MethodPut, "/api/v1/blackouts/3",
			`{"reason":"x"}`, http.StatusForbidden, []routedDenial{
				{"blackout.update", "blackouts/3", targetOutOfTokenScope}}},
		{"delete visible group schedule", http.MethodDelete,
			"/api/v1/blackout-schedules/4", "", http.StatusForbidden, []routedDenial{
				{"blackout_schedule.delete", "blackout-schedules/4", targetOutOfTokenScope}}},
		// Blackout 2 is on a connection the token cannot see, so it is
		// answered as though it did not exist, and nothing is recorded.
		{"stop hidden blackout", http.MethodPost, "/api/v1/blackouts/2/stop",
			"", http.StatusNotFound, nil},
		// The stored schedule is checked before the body, so a schedule
		// the token cannot see, or one that does not exist, answers 404
		// without a row even when the body names an out-of-scope target.
		{"update hidden schedule", http.MethodPut,
			"/api/v1/blackout-schedules/2", estateSchedule,
			http.StatusNotFound, nil},
		{"update missing schedule", http.MethodPut,
			"/api/v1/blackout-schedules/999999", estateSchedule,
			http.StatusNotFound, nil},
		{"update visible schedule to estate", http.MethodPut,
			"/api/v1/blackout-schedules/1", estateSchedule,
			http.StatusForbidden, []routedDenial{
				{"blackout_schedule.update", "blackout-schedules/1", targetOutOfTokenScope}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture.denials = nil
			rec := serveRoutes(h.RegisterRoutes, narrowed, tc.method, tc.path, tc.body)
			expectStatus(t, rec, tc.status)
			if tc.status == http.StatusForbidden {
				assertOutOfTokenScope(t, rec)
			}
			capture.expect(t, tc.want...)
		})
	}
}

// TestClusterGroupRefusalsAreAudited checks that the ownership and
// permission refusals on the cluster-group routes are recorded, and
// that one reaches the audit log through the RBAC handler's recorder
// with the cluster group named as its target.
func TestClusterGroupRefusalsAreAudited(t *testing.T) {
	const manageConnections = "Permission denied: requires manage_connections permission"

	f := newClusterCovFixture(t)
	capture := &routedCapture{}
	f.handler.SetDenialRecorder(capture.record)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   routedDenial
	}{
		{"create group", http.MethodPost, "/api/v1/cluster-groups",
			`{"name":"Refused"}`,
			routedDenial{"cluster_group.create", "cluster-groups", manageConnections}},
		{"update group not owned", http.MethodPut, "/api/v1/cluster-groups/2",
			`{"name":"Refused"}`,
			routedDenial{"cluster_group.update", "cluster-groups/2",
				"You do not have permission to update this cluster group"}},
		{"delete group not owned", http.MethodDelete, "/api/v1/cluster-groups/2", "",
			routedDenial{"cluster_group.delete", "cluster-groups/2",
				"You do not have permission to delete this cluster group"}},
		{"create cluster in group", http.MethodPost,
			"/api/v1/cluster-groups/2/clusters", `{"name":"Refused"}`,
			routedDenial{"cluster_group.update", "cluster-groups/2", manageConnections}},
		{"rename auto-detected group", http.MethodPut,
			"/api/v1/cluster-groups/group-auto", `{"name":"Refused"}`,
			routedDenial{"cluster_group.update", "cluster-groups",
				"Permission denied: you do not have permission to rename auto-detected groups"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture.denials = nil
			rec := clusterCovServe(f.handler, f.plain, tc.method, tc.path, tc.body)
			assertError(t, rec, http.StatusForbidden, tc.want.reason)
			capture.expect(t, tc.want)
		})
	}

	t.Run("recorded in the audit log", func(t *testing.T) {
		f.handler.SetDenialRecorder(NewRBACHandler(f.store,
			auth.NewRBACChecker(f.store)).RecordDenial)
		const reason = "You do not have permission to delete this cluster group"
		rec := clusterCovServe(f.handler, f.plain, http.MethodDelete,
			"/api/v1/cluster-groups/2", "")
		assertError(t, rec, http.StatusForbidden, reason)
		assertDenialRecorded(t, f.store, "cluster_group.delete", reason)

		events, _, err := f.store.ListAuditEvents(auth.AuditFilter{
			Action: "cluster_group.delete", Outcome: string(auth.OutcomeDenied)})
		if err != nil {
			t.Fatalf("ListAuditEvents failed: %v", err)
		}
		var details struct {
			Target string `json:"target"`
		}
		if err := json.Unmarshal(events[0].Details, &details); err != nil {
			t.Fatalf("Failed to decode details %s: %v", events[0].Details, err)
		}
		if details.Target != "cluster-groups/2" {
			t.Errorf("Expected target cluster-groups/2, got %q", details.Target)
		}
	})
}

// TestDeniedScopedRoute covers the override route shapes deniedRoute
// maps, including those it leaves to the fallback.
func TestDeniedScopedRoute(t *testing.T) {
	tests := []struct {
		method, path, action, target string
	}{
		{http.MethodPut, "/api/v1/alert-overrides/server/5/3",
			"alert_override.update", "alert-overrides/server/5"},
		{http.MethodDelete, "/api/v1/probe-overrides/group/007/pg_stat_activity",
			"probe_override.delete", "probe-overrides/group/7"},
		{http.MethodPut, "/api/v1/channel-overrides/cluster/2/9",
			"channel_override.update", "channel-overrides/cluster/2"},
		// A scope outside the vocabulary, or an id that does not
		// parse, leaves the kind alone as the target.
		{http.MethodPut, "/api/v1/alert-overrides/" + strings.Repeat("x", 300) + "/5/3",
			"alert_override.update", "alert-overrides"},
		{http.MethodPut, "/api/v1/alert-overrides/server/abc/3",
			"alert_override.update", "alert-overrides"},
		{http.MethodPut, "/api/v1/alert-overrides/server/0/3",
			"alert_override.update", "alert-overrides"},
		// Anything other than a write on a single override falls back.
		{http.MethodGet, "/api/v1/alert-overrides/server/5",
			"rbac.get", unmatchedDenialTarget},
		{http.MethodPut, "/api/v1/alert-overrides/server/5",
			"rbac.put", unmatchedDenialTarget},
		{http.MethodPost, "/api/v1/alert-overrides/server/5/3",
			"rbac.post", unmatchedDenialTarget},
		// A read of one connection's override context names that
		// connection as the server scope (issue #648).
		{http.MethodGet, "/api/v1/alert-overrides/context/5/3",
			"alert_override.read", "alert-overrides/server/5"},
		{http.MethodGet, "/api/v1/alert-overrides/context/x/3",
			"alert_override.read", "alert-overrides"},
		// The blackout and probe config routes take the generic shape.
		{http.MethodPost, "/api/v1/blackouts", "blackout.create", "blackouts"},
		{http.MethodPost, "/api/v1/blackouts/4/stop", "blackout.update", "blackouts/4"},
		{http.MethodDelete, "/api/v1/blackout-schedules/4",
			"blackout_schedule.delete", "blackout-schedules/4"},
		{http.MethodPut, "/api/v1/probe-configs/2", "probe_config.update", "probe-configs/2"},
		// Reads, and the connection create, are named too (issue #648).
		{http.MethodGet, "/api/v1/notification-channels",
			"notification_channel.read", "notification-channels"},
		{http.MethodGet, "/api/v1/notification-channels/3/recipients",
			"notification_channel.read", "notification-channels/3"},
		{http.MethodPost, "/api/v1/connections", "connection.create", "connections"},
		{http.MethodGet, "/api/v1/connections", "rbac.get", unmatchedDenialTarget},
		{http.MethodHead, "/api/v1/blackouts", "rbac.head", unmatchedDenialTarget},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		action, target := deniedRoute(req)
		if action != tt.action || target != tt.target {
			t.Errorf("deniedRoute(%s %.60s) = (%q, %q), want (%q, %q)", tt.method,
				tt.path, action, target, tt.action, tt.target)
		}
	}
}
