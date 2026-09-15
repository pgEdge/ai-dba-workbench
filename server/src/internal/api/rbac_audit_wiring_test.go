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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// withActor returns a request carrying the context values that
// auth.ActorFromContext reads, so that handler tests can assert on the
// actor recorded in the audit log.
func withActor(req *http.Request, username string, userID int64,
	ip string, superuser bool) *http.Request {

	ctx := context.WithValue(req.Context(), auth.UsernameContextKey, username)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.IsAPITokenContextKey, false)
	ctx = context.WithValue(ctx, auth.IPAddressContextKey, ip)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, superuser)
	return req.WithContext(ctx)
}

// latestAuditEvent returns the most recent audit event, failing the test
// when the log is empty.
func latestAuditEvent(t *testing.T, store *auth.AuthStore) auth.AuditEvent {
	t.Helper()
	events, _, err := store.ListAuditEvents(auth.AuditFilter{Limit: 1})
	if err != nil {
		t.Fatalf("failed to list audit events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one audit event, got none")
	}
	return events[0]
}

func TestDeleteGroupRecordsActor(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, err := store.CreateGroup("doomed", "Group to delete")
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/groups/"+itoa(groupID), nil)
	req = withActor(req, "alice", 1, "192.0.2.5", true)
	rec := httptest.NewRecorder()
	handler.handleGroupSubpath(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}

	ev := latestAuditEvent(t, store)
	if ev.Action != "group.delete" {
		t.Errorf("action: got %q, want %q", ev.Action, "group.delete")
	}
	if ev.ActorName != "alice" {
		t.Errorf("actor name: got %q, want %q", ev.ActorName, "alice")
	}
	if ev.ActorType != auth.ActorUser {
		t.Errorf("actor type: got %q, want %q", ev.ActorType, auth.ActorUser)
	}
	if ev.ActorIP != "192.0.2.5" {
		t.Errorf("actor ip: got %q, want %q", ev.ActorIP, "192.0.2.5")
	}
	if ev.ActorID == nil || *ev.ActorID != 1 {
		t.Errorf("actor id: got %v, want 1", ev.ActorID)
	}
}

func TestCreateUserRecordsActor(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	body := strings.NewReader(
		`{"username":"bob","password":"Password1234","annotation":"n"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users", body)
	req.Header.Set("Content-Type", "application/json")
	req = withActor(req, "alice", 1, "192.0.2.5", true)
	rec := httptest.NewRecorder()
	handler.handleUsers(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}

	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Action: "user.create",
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("failed to list audit events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected a user.create audit event, got none")
	}
	if events[0].ActorName != "alice" || events[0].ActorIP != "192.0.2.5" {
		t.Errorf("unexpected actor on user.create: %+v", events[0])
	}
}

func TestRequirePermissionRecordsDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, err := store.CreateGroup("safe", "Group that survives")
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/groups/"+itoa(groupID), nil)
	req = withActor(req, "mallory", 9, "203.0.113.7", false)
	rec := httptest.NewRecorder()
	handler.handleGroupSubpath(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	ev := latestAuditEvent(t, store)
	if ev.Outcome != auth.OutcomeDenied {
		t.Errorf("outcome: got %q, want %q", ev.Outcome, auth.OutcomeDenied)
	}
	if ev.Action != "group.delete" {
		t.Errorf("action: got %q, want %q", ev.Action, "group.delete")
	}
	if !strings.Contains(ev.Error, auth.PermManageGroups) {
		t.Errorf("error %q does not mention %q", ev.Error, auth.PermManageGroups)
	}
	if ev.ActorName != "mallory" || ev.ActorIP != "203.0.113.7" {
		t.Errorf("unexpected denial actor: %+v", ev)
	}
}

func TestRequireSuperuserRecordsDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, err := store.CreateGroup("perms", "Group with permissions")
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}

	body := strings.NewReader(`{"permission":"manage_users"}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/rbac/groups/"+itoa(groupID)+"/permissions", body)
	req.Header.Set("Content-Type", "application/json")
	req = withActor(req, "mallory", 9, "203.0.113.7", false)
	rec := httptest.NewRecorder()
	handler.handleGroupSubpath(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	ev := latestAuditEvent(t, store)
	if ev.Outcome != auth.OutcomeDenied {
		t.Errorf("outcome: got %q, want %q", ev.Outcome, auth.OutcomeDenied)
	}
	if ev.Action != "permission.admin.grant" {
		t.Errorf("action: got %q, want %q", ev.Action, "permission.admin.grant")
	}
	if !strings.Contains(ev.Error, "superuser") {
		t.Errorf("error %q does not mention superuser", ev.Error)
	}
}

func TestDeniedAction(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodPost, "/api/v1/rbac/users", "user.create"},
		{http.MethodPut, "/api/v1/rbac/users/3", "user.update"},
		{http.MethodDelete, "/api/v1/rbac/users/3", "user.delete"},
		{http.MethodGet, "/api/v1/rbac/users", "rbac.get"},
		{http.MethodGet, "/api/v1/rbac/users/3/privileges", "rbac.get"},
		{http.MethodPost, "/api/v1/rbac/groups", "group.create"},
		{http.MethodPut, "/api/v1/rbac/groups/7", "group.update"},
		{http.MethodDelete, "/api/v1/rbac/groups/7", "group.delete"},
		{http.MethodGet, "/api/v1/rbac/groups/7", "rbac.get"},
		{http.MethodPost, "/api/v1/rbac/groups/7/members", "group.member.add"},
		{http.MethodDelete, "/api/v1/rbac/groups/7/members/user/3",
			"group.member.remove"},
		{http.MethodPost, "/api/v1/rbac/groups/7/privileges/mcp",
			"privilege.mcp.grant"},
		{http.MethodDelete, "/api/v1/rbac/groups/7/privileges/mcp",
			"privilege.mcp.revoke"},
		{http.MethodPost, "/api/v1/rbac/groups/7/privileges/connections",
			"privilege.connection.grant"},
		{http.MethodDelete, "/api/v1/rbac/groups/7/privileges/connections/2",
			"privilege.connection.revoke"},
		{http.MethodPost, "/api/v1/rbac/groups/7/permissions",
			"permission.admin.grant"},
		{http.MethodDelete, "/api/v1/rbac/groups/7/permissions/manage_users",
			"permission.admin.revoke"},
		{http.MethodGet, "/api/v1/rbac/groups/7/permissions", "rbac.get"},
		{http.MethodPost, "/api/v1/rbac/tokens", "token.create"},
		{http.MethodDelete, "/api/v1/rbac/tokens/5", "token.delete"},
		{http.MethodPut, "/api/v1/rbac/tokens/5/scope", "token.scope.set"},
		{http.MethodDelete, "/api/v1/rbac/tokens/5/scope", "token.scope.clear"},
		{http.MethodGet, "/api/v1/rbac/tokens/5/scope", "rbac.get"},
		{http.MethodGet, "/api/v1/rbac/audit", "audit.read"},
		{http.MethodPatch, "/api/v1/rbac/groups/7", "rbac.patch"},
		{http.MethodGet, "/api/v1/rbac/privileges/mcp", "rbac.get"},
		{http.MethodPost, "/api/v1/rbac/unknown/thing", "rbac.post"},
		{http.MethodGet, "/somewhere/else", "rbac.get"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if got := deniedAction(req); got != tt.want {
				t.Errorf("deniedAction: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeniedActionEdgeCases(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodPost, "/api/v1/rbac/", "rbac.post"},
		{http.MethodPost, "/api/v1/rbac/groups/7/privileges",
			"rbac.post"},
		{http.MethodPost, "/api/v1/rbac/groups/7/privileges/mcp/extra",
			"rbac.post"},
		{http.MethodPost, "/api/v1/rbac/groups/7/privileges/other",
			"rbac.post"},
		{http.MethodPost, "/api/v1/rbac/audit", "rbac.post"},
		{http.MethodDelete, "/api/v1/rbac/groups/7/members", "rbac.delete"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if got := deniedAction(req); got != tt.want {
				t.Errorf("deniedAction: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRecordDenialWithoutStore(t *testing.T) {
	handler := &RBACHandler{}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/rbac/groups/7", nil)

	// Must not panic when the handler has no auth store to record into.
	handler.recordDenial(req, "no store")
}

func TestRecordDenialLogsStoreError(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// Closing the store makes every audit write fail, which exercises
	// the error path: the denial is logged and otherwise ignored.
	store.Close()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/rbac/groups/7", nil)
	req = withActor(req, "mallory", 9, "203.0.113.7", false)
	handler.recordDenial(req, "closed store")
}
