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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Token-scope refusals outside the RBAC handler are audited (issue #471)
// =============================================================================

// denialCapture records the reasons passed to a denial recorder.
type denialCapture struct {
	reasons []string
}

func (c *denialCapture) record(_ *http.Request, reason string) {
	c.reasons = append(c.reasons, reason)
}

// expect fails unless exactly the given reasons were recorded.
func (c *denialCapture) expect(t *testing.T, want ...string) {
	t.Helper()
	if len(c.reasons) != len(want) {
		t.Fatalf("Expected %d recorded denials %v, got %v", len(want), want, c.reasons)
	}
	for i := range want {
		if c.reasons[i] != want[i] {
			t.Errorf("Denial %d: expected %q, got %q", i, want[i], c.reasons[i])
		}
	}
}

// TestRequireConnectionsInTokenScopeAudits checks that the two
// connection-scope gates record a refusal through the recorder they are
// given, record nothing when they admit the request, and still refuse
// with no recorder.
func TestRequireConnectionsInTokenScopeAudits(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	rc := auth.NewRBACChecker(store)
	session, _, wildcard, narrowed, _ := scopedCallers(t, store)

	gates := []struct {
		name string
		gate func(http.ResponseWriter, *http.Request, denialRecorder) bool
		in   []scopeCaller
		out  []scopeCaller
	}{
		{"all connections", func(w http.ResponseWriter, r *http.Request, rec denialRecorder) bool {
			return requireAllConnectionsInTokenScope(w, r, rc, rec)
		}, []scopeCaller{session, wildcard}, []scopeCaller{narrowed}},
		{"listed connections", func(w http.ResponseWriter, r *http.Request, rec denialRecorder) bool {
			return requireConnectionsInTokenScope(w, r, rc, rec, 5, 9)
		}, []scopeCaller{session, wildcard}, []scopeCaller{narrowed}},
	}
	for _, g := range gates {
		t.Run(g.name, func(t *testing.T) {
			for _, caller := range g.out {
				capture := &denialCapture{}
				rec := httptest.NewRecorder()
				if g.gate(rec, newScopeRequest(caller, http.MethodPut, ""), capture.record) {
					t.Fatalf("%s: expected a refusal", caller.name)
				}
				assertOutOfTokenScope(t, rec)
				capture.expect(t, targetOutOfTokenScope)

				rec = httptest.NewRecorder()
				if g.gate(rec, newScopeRequest(caller, http.MethodPut, ""), nil) {
					t.Fatalf("%s: expected a refusal with no recorder", caller.name)
				}
				assertOutOfTokenScope(t, rec)
			}
			for _, caller := range g.in {
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

// TestClusterScopeRefusalIsAudited checks, end to end, that the cluster
// handler's token-scope refusal reaches the audit log through the RBAC
// handler's recorder, and that a handler with no recorder still refuses.
func TestClusterScopeRefusalIsAudited(t *testing.T) {
	f := newClusterCovFixture(t)

	rec := clusterCovServe(f.handler, f.narrowed, http.MethodPost,
		"/api/v1/clusters", `{"name":"Refused","group_id":3}`)
	assertOutOfTokenScope(t, rec)

	f.handler.SetDenialRecorder(NewRBACHandler(f.store, auth.NewRBACChecker(f.store)).RecordDenial)
	rec = clusterCovServe(f.handler, f.narrowed, http.MethodPost,
		"/api/v1/clusters", `{"name":"Refused","group_id":3}`)
	assertOutOfTokenScope(t, rec)
	assertDenialRecorded(t, f.store, "cluster.create", targetOutOfTokenScope)
	f.expectValue(t, "clusters named", "Refused", "0")
}

// TestAlertRuleScopeRefusalIsAudited checks that the alert rule
// handler's token-scope refusal is recorded.
func TestAlertRuleScopeRefusalIsAudited(t *testing.T) {
	g := newEstateGates(t)
	capture := &denialCapture{}
	g.alertRule.SetDenialRecorder(capture.record)

	rec := httptest.NewRecorder()
	g.alertRule.updateAlertRule(rec, newScopeRequest(g.callers["narrowed"],
		http.MethodPut, `{"default_threshold":20}`), 1)
	assertOutOfTokenScope(t, rec)
	capture.expect(t, targetOutOfTokenScope)
}

// TestNotificationChannelScopeRefusalIsAudited checks that the
// notification channel handler's token-scope refusal is recorded.
func TestNotificationChannelScopeRefusalIsAudited(t *testing.T) {
	f := newChannelScopeFixture(t)
	capture := &denialCapture{}
	f.h.SetDenialRecorder(capture.record)

	rec := httptest.NewRecorder()
	f.h.deleteChannel(rec, newScopeRequest(f.callers["narrowed"],
		http.MethodDelete, ""), f.channelID)
	assertOutOfTokenScope(t, rec)
	capture.expect(t, targetOutOfTokenScope)
}

// TestUpdateConnectionClusterPermissionRefusalIsAudited checks that the
// manage_connections refusal on PUT /connections/{id}/cluster is
// recorded, and still answered without a recorder.
func TestUpdateConnectionClusterPermissionRefusalIsAudited(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	const reason = "Permission denied: requires manage_connections permission"
	h := &ConnectionHandler{authStore: store, rbacChecker: auth.NewRBACChecker(store)}
	request := func() *http.Request {
		return httptest.NewRequest(http.MethodPut, "/api/v1/connections/5/cluster", nil)
	}

	rec := httptest.NewRecorder()
	h.handleUpdateConnectionCluster(rec, request(), 5)
	assertError(t, rec, http.StatusForbidden, reason)

	capture := &denialCapture{}
	h.SetDenialRecorder(capture.record)
	rec = httptest.NewRecorder()
	h.handleUpdateConnectionCluster(rec, request(), 5)
	assertError(t, rec, http.StatusForbidden, reason)
	capture.expect(t, reason)
}
