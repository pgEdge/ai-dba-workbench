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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Alert mutation refusals (issues #675 and #677)
// =============================================================================

// alertRouteCase is one alert mutation request: the handler method that
// serves it and the request that reaches it.
type alertRouteCase struct {
	name   string
	serve  func(*AlertHandler, http.ResponseWriter, *http.Request)
	method string
	path   string
	body   string
	action string
}

// alertRouteCases returns the three alert mutations, each naming alert
// 11 and the action the audit log records for it.
func alertRouteCases() []alertRouteCase {
	return []alertRouteCase{
		{"acknowledge", (*AlertHandler).handleAcknowledge,
			http.MethodPost, "/api/v1/alerts/acknowledge", `{"alert_id":11}`,
			deniedActionAlertAcknowledge},
		{"unacknowledge", (*AlertHandler).handleAcknowledge,
			http.MethodDelete, "/api/v1/alerts/acknowledge?alert_id=11", "",
			deniedActionAlertUnacknowledge},
		{"save analysis", (*AlertHandler).handleSaveAnalysis,
			http.MethodPut, "/api/v1/alerts/analysis",
			`{"alert_id":11,"analysis":"x"}`, deniedActionAlertAnalysisUpdate},
	}
}

// serveAlertRoute sends the case's request, as wrapped by as, to h.
func serveAlertRoute(h *AlertHandler, c alertRouteCase,
	as func(*http.Request) *http.Request) *httptest.ResponseRecorder {

	r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c.serve(h, rec, as(r))
	return rec
}

// TestAlertRefusalsAreAudited checks that the refusal of an alert the
// caller can see but may not change answers 403 and records one denial
// naming the alert and the action, through the target refuseTarget
// attaches rather than the path fallback.
//
// A plain user sees system alerts but may not manage them, so the
// acknowledge routes record the manage_alert_rules reason; saving an
// analysis on a visible system alert is a 400 about the alert rather
// than the caller, and records nothing. A connection alert is made
// visible but refused by a lister that reports the connection shared
// whilst the access check reports it private; the shared access rule
// does not produce that split today, so this refusal only guards
// against a future change to it.
func TestAlertRefusalsAreAudited(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	bobID := newTestUser(t, store, "bob")
	asBob := func(r *http.Request) *http.Request {
		return withUsername(withUser(r, bobID), "bob")
	}

	systemAlerts := NewAlertHandler(nil, store, auth.NewRBACChecker(store))
	systemAlerts.setAlertResolver(&fakeAlertResolver{system: true})

	connAlerts := NewAlertHandler(nil, store,
		mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false))
	connAlerts.setAlertResolver(&fakeAlertResolver{connID: rbacUnsharedConnID})
	connAlerts.setVisibilityLister(singleConnectionLister(
		rbacUnsharedConnID, "alice", true))

	for _, c := range alertRouteCases() {
		t.Run("system alert "+c.name, func(t *testing.T) {
			capture := &routedCapture{}
			systemAlerts.SetDenialRecorder(capture.record)
			rec := serveAlertRoute(systemAlerts, c, asBob)
			if c.action == deniedActionAlertAnalysisUpdate {
				expectStatus(t, rec, http.StatusBadRequest)
				capture.expect(t)
				return
			}
			assertForbiddenMessage(t, rec, "Access denied")
			capture.expect(t, routedDenial{c.action, alertDenialTarget(11),
				systemAlertManageDenied})
		})
		t.Run("connection alert "+c.name, func(t *testing.T) {
			capture := &routedCapture{}
			connAlerts.SetDenialRecorder(capture.record)
			rec := serveAlertRoute(connAlerts, c, asBob)
			assertForbiddenMessage(t, rec, "Access denied")
			capture.expect(t, routedDenial{c.action, alertDenialTarget(11),
				alertAccessDenied})
		})
		t.Run("without recorder "+c.name, func(t *testing.T) {
			connAlerts.SetDenialRecorder(nil)
			rec := serveAlertRoute(connAlerts, c, asBob)
			assertForbiddenMessage(t, rec, "Access denied")
		})
	}
}

// TestHiddenAlertIsNotAudited checks that an alert the caller cannot
// see, whether on a connection or a system alert, answers exactly as a
// missing one and records no refusal, since a recorded 403 would itself
// confirm that the alert exists (issue #677). The handlers are built on
// a nil auth store, whose checker shows the caller nothing.
func TestHiddenAlertIsNotAudited(t *testing.T) {
	denyAll := auth.NewRBACChecker(nil)
	resolvers := map[string]*fakeAlertResolver{
		"connection alert": {connID: 7},
		"system alert":     {system: true},
	}
	none := func(r *http.Request) *http.Request { return r }
	for kind, resolver := range resolvers {
		for _, c := range alertRouteCases() {
			t.Run(kind+" "+c.name, func(t *testing.T) {
				h := NewAlertHandler(nil, nil, denyAll)
				h.setAlertResolver(resolver)
				capture := &routedCapture{}
				h.SetDenialRecorder(capture.record)

				assertError(t, serveAlertRoute(h, c, none),
					http.StatusNotFound, "Alert not found")
				capture.expect(t)
			})
		}
	}
}
