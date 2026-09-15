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
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// auditTestActor is the actor credited with the events that the audit
// handler tests seed.
func auditTestActor() auth.Actor {
	return auth.Actor{Type: auth.ActorUser, Name: "seeder"}
}

// seedAuditEvents creates a handful of audit events of known shape: two
// successful user creations, one failed user update and one successful
// admin permission grant. It returns the handler under test.
func seedAuditEvents(t *testing.T, store *auth.AuthStore) {
	t.Helper()

	as := store.AsActor(auditTestActor())
	if err := as.CreateUser("alice", "Password1234", "", "", ""); err != nil {
		t.Fatalf("Failed to create alice: %v", err)
	}
	if err := as.CreateUser("bob", "Password1234", "", "", ""); err != nil {
		t.Fatalf("Failed to create bob: %v", err)
	}
	if err := as.UpdateUser("nobody", "", "gone", "", ""); err == nil {
		t.Fatal("Expected UpdateUser on a missing user to fail")
	}

	groupID, err := store.CreateGroup("auditors", "Auditors")
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}
	if err := as.GrantAdminPermission(groupID, auth.PermManageUsers); err != nil {
		t.Fatalf("Failed to grant permission: %v", err)
	}
}

// auditRequest issues a GET against the audit handler as a superuser and
// returns the recorder.
func auditRequest(h *RBACHandler, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/audit"+query, nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()
	h.handleAudit(rec, req)
	return rec
}

// decodeAuditEvents decodes a successful audit response body.
func decodeAuditEvents(t *testing.T, rec *httptest.ResponseRecorder) []auth.AuditEvent {
	t.Helper()

	var events []auth.AuditEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("Failed to decode body %q: %v", rec.Body.String(), err)
	}
	return events
}

func TestRBACHandlerAuditListsNewestFirst(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	seedAuditEvents(t, store)

	rec := auditRequest(handler, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	events := decodeAuditEvents(t, rec)
	if len(events) != 4 {
		t.Fatalf("Expected 4 events, got %d", len(events))
	}
	for i := 1; i < len(events); i++ {
		if events[i-1].ID <= events[i].ID {
			t.Errorf("Expected newest first, got id %d before id %d",
				events[i-1].ID, events[i].ID)
		}
	}
	if events[0].Action != "permission.admin.grant" {
		t.Errorf("Expected newest event permission.admin.grant, got %q",
			events[0].Action)
	}
	if got := rec.Header().Get(headerTotalCount); got != "4" {
		t.Errorf("Expected %s of 4, got %q", headerTotalCount, got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Expected JSON content type, got %q", got)
	}
}

func TestRBACHandlerAuditEmptyResultIsArray(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	rec := auditRequest(handler, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("Expected an empty JSON array, got %q", body)
	}
	if got := rec.Header().Get(headerTotalCount); got != "0" {
		t.Errorf("Expected %s of 0, got %q", headerTotalCount, got)
	}
}

func TestRBACHandlerAuditRequiresSuperuser(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	if err := store.CreateUser("plain", "Password1234", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	userID, err := store.GetUserID("plain")
	if err != nil {
		t.Fatalf("Failed to read user id: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/audit", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleAudit(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

func TestRBACHandlerAuditMethodNotAllowed(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/audit", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleAudit(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET" {
		t.Errorf("Expected Allow: GET, got %q", got)
	}
}

func TestRBACHandlerAuditInvalidParameters(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	seedAuditEvents(t, store)

	tests := []struct {
		name  string
		query string
	}{
		{"since not RFC 3339", "?since=yesterday"},
		{"since date only", "?since=2026-09-15"},
		{"until not RFC 3339", "?until=not-a-time"},
		{"target id not an integer", "?target_id=abc"},
		{"limit not an integer", "?limit=lots"},
		{"limit negative", "?limit=-1"},
		{"offset not an integer", "?offset=none"},
		{"offset negative", "?offset=-5"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := auditRequest(handler, tc.query)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d. Body: %s",
					http.StatusBadRequest, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRBACHandlerAuditAcceptsRFC3339Range(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	seedAuditEvents(t, store)

	rec := auditRequest(handler,
		"?since=2000-01-01T00:00:00Z&until=2100-01-01T00:00:00Z")
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	if len(decodeAuditEvents(t, rec)) != 4 {
		t.Errorf("Expected the whole range to match every event")
	}

	rec = auditRequest(handler, "?until=2000-01-01T00:00:00Z")
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if got := len(decodeAuditEvents(t, rec)); got != 0 {
		t.Errorf("Expected no events before 2000, got %d", got)
	}
}

func TestRBACHandlerAuditFiltersByActionAndOutcome(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	seedAuditEvents(t, store)

	rec := auditRequest(handler, "?action=user.create")
	events := decodeAuditEvents(t, rec)
	if len(events) != 2 {
		t.Fatalf("Expected 2 user.create events, got %d", len(events))
	}
	for _, ev := range events {
		if ev.Action != "user.create" {
			t.Errorf("Expected only user.create, got %q", ev.Action)
		}
	}

	rec = auditRequest(handler, "?outcome=failure")
	events = decodeAuditEvents(t, rec)
	if len(events) != 1 {
		t.Fatalf("Expected 1 failed event, got %d", len(events))
	}
	if events[0].Outcome != auth.OutcomeFailure || events[0].Action != "user.update" {
		t.Errorf("Expected a failed user.update, got %q/%q",
			events[0].Action, events[0].Outcome)
	}
}

func TestRBACHandlerAuditFiltersByActorTargetAndPage(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	seedAuditEvents(t, store)

	aliceID, err := store.GetUserID("alice")
	if err != nil {
		t.Fatalf("Failed to read alice's id: %v", err)
	}

	rec := auditRequest(handler, "?actor=seeder&actor_type=user")
	if got := len(decodeAuditEvents(t, rec)); got != 4 {
		t.Errorf("Expected all 4 events for the seeding actor, got %d", got)
	}

	rec = auditRequest(handler,
		"?target_type=user&target_id="+strconv.FormatInt(aliceID, 10))
	events := decodeAuditEvents(t, rec)
	if len(events) != 1 {
		t.Fatalf("Expected 1 event targeting alice, got %d", len(events))
	}
	if events[0].TargetID == nil || *events[0].TargetID != aliceID {
		t.Errorf("Expected target id %d, got %v", aliceID, events[0].TargetID)
	}

	rec = auditRequest(handler, "?limit=1&offset=1")
	events = decodeAuditEvents(t, rec)
	if len(events) != 1 {
		t.Fatalf("Expected a single-event page, got %d", len(events))
	}
	if events[0].Action != "user.update" {
		t.Errorf("Expected the second-newest event, got %q", events[0].Action)
	}
	if got := rec.Header().Get(headerTotalCount); got != "4" {
		t.Errorf("Expected the unpaged total of 4, got %q", got)
	}
}

// seedManyAuditEvents records 506 events by granting and revoking the
// same admin permission 253 times, which is more than both the default
// page size of 50 and the 500-row page cap. It returns the number of
// events recorded.
func seedManyAuditEvents(t *testing.T, store *auth.AuthStore) int {
	t.Helper()

	as := store.AsActor(auditTestActor())
	groupID, err := store.CreateGroup("bulk", "Bulk")
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}
	const cycles = 253
	for i := 0; i < cycles; i++ {
		if err := as.GrantAdminPermission(groupID, auth.PermManageUsers); err != nil {
			t.Fatalf("Failed to grant permission on cycle %d: %v", i, err)
		}
		if err := as.RevokeAdminPermission(groupID, auth.PermManageUsers); err != nil {
			t.Fatalf("Failed to revoke permission on cycle %d: %v", i, err)
		}
	}
	return cycles * 2
}

func TestRBACHandlerAuditDefaultPageSize(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	total := seedManyAuditEvents(t, store)

	// An absent limit and an explicit limit of zero both mean "use the
	// default", which the store pins at 50.
	for _, query := range []string{"", "?limit=0"} {
		rec := auditRequest(handler, query)
		if rec.Code != http.StatusOK {
			t.Fatalf("Query %q: expected status %d, got %d",
				query, http.StatusOK, rec.Code)
		}
		if got := len(decodeAuditEvents(t, rec)); got != 50 {
			t.Errorf("Query %q: expected the default page of 50, got %d",
				query, got)
		}
		if got := rec.Header().Get(headerTotalCount); got != strconv.Itoa(total) {
			t.Errorf("Query %q: expected the unpaged total of %d, got %q",
				query, total, got)
		}
	}
}

func TestRBACHandlerAuditLimitIsCapped(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	total := seedManyAuditEvents(t, store)

	rec := auditRequest(handler, "?limit=1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
	events := decodeAuditEvents(t, rec)
	if len(events) != 500 {
		t.Errorf("Expected the page to be capped at 500, got %d", len(events))
	}
	if got := rec.Header().Get(headerTotalCount); got != strconv.Itoa(total) {
		t.Errorf("Expected the unpaged total of %d, got %q", total, got)
	}
}

func TestRBACHandlerAuditStoreError(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// Closing the store makes every subsequent query fail, which is the
	// only way to reach the handler's internal-error branch.
	store.Close()

	rec := auditRequest(handler, "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusInternalServerError, rec.Code, rec.Body.String())
	}
}

func TestRBACHandlerAuditRouteIsRegistered(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, func(next http.HandlerFunc) http.HandlerFunc {
		return next
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/audit", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected the audit route to be registered, got status %d",
			rec.Code)
	}
}

func TestBuildOpenAPISpecRBACAuditPath(t *testing.T) {
	spec := BuildOpenAPISpec()

	item, ok := spec.Paths["/rbac/audit"]
	if !ok {
		t.Fatal("Expected /rbac/audit to be defined")
	}
	if item.Get == nil {
		t.Fatal("Expected a GET operation on /rbac/audit")
	}

	for _, code := range []string{"200", "400", "401", "403"} {
		if _, ok := item.Get.Responses[code]; !ok {
			t.Errorf("Expected a %s response on /rbac/audit", code)
		}
	}
	if _, ok := item.Get.Responses["200"].Headers["X-Total-Count"]; !ok {
		t.Error("Expected the 200 response to document X-Total-Count")
	}

	wanted := map[string]bool{
		"actor": false, "actor_type": false, "action": false,
		"target_type": false, "target_id": false, "outcome": false,
		"since": false, "until": false, "limit": false, "offset": false,
	}
	for _, p := range item.Get.Parameters {
		if _, ok := wanted[p.Name]; ok {
			wanted[p.Name] = true
		}
	}
	for name, seen := range wanted {
		if !seen {
			t.Errorf("Expected query parameter %q on /rbac/audit", name)
		}
	}

	if _, ok := spec.Components.Schemas["AuditEvent"]; !ok {
		t.Error("Expected the AuditEvent schema to be defined")
	}
}
