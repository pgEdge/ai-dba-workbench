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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// groupRequest sends a superuser session request through the group
// router.
func groupRequest(h *RBACHandler, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req = withSuperuserSession(req)
	rec := httptest.NewRecorder()
	if path == "/api/v1/rbac/groups" {
		h.handleGroups(rec, req)
	} else {
		h.handleGroupSubpath(rec, req)
	}
	return rec
}

// TestGroupSubpathRouting covers the group routes a session reaches,
// including their method, id and path refusals.
func TestGroupSubpathRouting(t *testing.T) {
	h, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	parent, err := store.CreateGroup("parent", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	child, err := store.CreateGroup("child", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if err := store.GrantConnectionPrivilege(parent, 5, auth.AccessLevelRead); err != nil {
		t.Fatalf("GrantConnectionPrivilege failed: %v", err)
	}
	if err := store.GrantAdminPermission(parent, auth.PermManageBlackouts); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}
	if err := store.CreateUser("member", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	userID, err := store.GetUserID("member")
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}
	if err := store.AddUserToGroup(parent, userID); err != nil {
		t.Fatalf("AddUserToGroup failed: %v", err)
	}
	if err := store.AddGroupToGroup(parent, child); err != nil {
		t.Fatalf("AddGroupToGroup failed: %v", err)
	}

	g := func(format string, args ...any) string {
		return "/api/v1/rbac/groups/" + fmt.Sprintf(format, args...)
	}

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
		expect string
	}{
		{"get group", http.MethodGet, g("%d", parent), "", http.StatusOK, `"user_members":["member"]`},
		{"get missing group", http.MethodGet, g("999"), "", http.StatusNotFound, "Group not found"},
		{"group bad method", http.MethodPatch, g("%d", parent), "", http.StatusMethodNotAllowed, ""},
		{"empty id", http.MethodGet, g(""), "", http.StatusNotFound, ""},
		{"invalid id", http.MethodGet, g("abc"), "", http.StatusBadRequest, "Invalid group ID"},
		{"unknown subpath", http.MethodGet, g("%d/nothing", parent), "", http.StatusNotFound, ""},
		{"effective privileges", http.MethodGet, g("%d/effective-privileges", child), "",
			http.StatusOK, `"admin_permissions":["manage_blackouts"]`},
		{"effective privileges of an empty group", http.MethodGet,
			g("%d/effective-privileges", parent), "", http.StatusOK, `"mcp_privileges":[]`},
		{"effective privileges missing", http.MethodGet, g("999/effective-privileges"), "",
			http.StatusNotFound, "Group not found"},
		{"effective privileges bad method", http.MethodPost,
			g("%d/effective-privileges", parent), "", http.StatusMethodNotAllowed, ""},
		{"members bad method", http.MethodGet, g("%d/members", parent), "",
			http.StatusMethodNotAllowed, ""},
		{"member removal bad method", http.MethodGet, g("%d/members/user/%d", parent, userID), "",
			http.StatusMethodNotAllowed, ""},
		{"member removal bad id", http.MethodDelete, g("%d/members/user/abc", parent), "",
			http.StatusBadRequest, "Invalid member ID"},
		{"member removal bad type", http.MethodDelete, g("%d/members/role/1", parent), "",
			http.StatusBadRequest, "Invalid member type"},
		{"members unknown subpath", http.MethodDelete, g("%d/members/user", parent), "",
			http.StatusNotFound, ""},
		{"add member without id", http.MethodPost, g("%d/members", parent), `{}`,
			http.StatusBadRequest, "Either user_id or group_id is required"},
		{"add member with both ids", http.MethodPost, g("%d/members", parent),
			`{"user_id":1,"group_id":2}`, http.StatusBadRequest, "Only one of"},
		{"add member bad body", http.MethodPost, g("%d/members", parent), `{`,
			http.StatusBadRequest, ""},
		{"remove user", http.MethodDelete, g("%d/members/user/%d", parent, userID), "",
			http.StatusNoContent, ""},
		{"remove group", http.MethodDelete, g("%d/members/group/%d", parent, child), "",
			http.StatusNoContent, ""},
		{"privileges without kind", http.MethodGet, g("%d/privileges", parent), "",
			http.StatusNotFound, ""},
		{"privileges unknown kind", http.MethodGet, g("%d/privileges/other", parent), "",
			http.StatusNotFound, ""},
		{"grant permission without name", http.MethodPost, g("%d/permissions", parent),
			`{}`, http.StatusBadRequest, "Permission is required"},
		{"list permissions", http.MethodGet, g("%d/permissions", parent), "",
			http.StatusOK, "manage_blackouts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := groupRequest(h, tt.method, tt.path, tt.body)
			if rec.Code != tt.want {
				t.Fatalf("Expected %d, got %d: %s", tt.want, rec.Code, rec.Body.String())
			}
			if tt.expect != "" && !strings.Contains(rec.Body.String(), tt.expect) {
				t.Errorf("Expected %q in %s", tt.expect, rec.Body.String())
			}
		})
	}
}

// TestGroupHandlersReportStoreFailures covers the group routes that
// answer 500 when the store cannot complete the change.
func TestGroupHandlersReportStoreFailures(t *testing.T) {
	tests := []struct {
		name    string
		table   string
		method  string
		path    string
		body    string
		message string
	}{
		{"list groups", "group_memberships", http.MethodGet, "", "", "Failed to list groups"},
		{"delete group", "group_memberships", http.MethodDelete, "%d", "", "Failed to delete group"},
		{"add user", "group_memberships", http.MethodPost, "%d/members",
			`{"user_id":1}`, "Failed to add user to group"},
		{"add group", "group_memberships", http.MethodPost, "%d/members",
			`{"group_id":1}`, "Failed to add group to group"},
		{"remove user", "group_memberships", http.MethodDelete, "%d/members/user/1", "",
			"Failed to remove user from group"},
		{"remove group", "group_memberships", http.MethodDelete, "%d/members/group/1", "",
			"Failed to remove group from group"},
		{"grant connection", "connection_privileges", http.MethodPost,
			"%d/privileges/connections", `{"connection_id":5,"access_level":"read"}`,
			"Failed to grant connection privilege"},
		{"revoke connection", "connection_privileges", http.MethodDelete,
			"%d/privileges/connections/5", "", "Failed to revoke connection privilege"},
		{"list permissions", "group_admin_permissions", http.MethodGet, "%d/permissions", "",
			"Failed to list permissions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, store, dir, cleanup := createTestRBACHandlerWithDir(t)
			defer cleanup()
			groupID, err := store.CreateGroup("doomed", "")
			if err != nil {
				t.Fatalf("CreateGroup failed: %v", err)
			}
			dropAuthTable(t, dir, tt.table)

			path := "/api/v1/rbac/groups"
			if tt.path != "" {
				path += "/" + strings.Replace(tt.path, "%d", fmt.Sprint(groupID), 1)
			}
			rec := groupRequest(h, tt.method, path, tt.body)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("Expected 500, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.message) {
				t.Errorf("Expected %q, got %s", tt.message, rec.Body.String())
			}
		})
	}
}

// TestGroupGrantChecksFailClosed checks that a bounded token is refused
// when the store cannot say what a group holds, rather than being let
// through on a failed lookup.
func TestGroupGrantChecksFailClosed(t *testing.T) {
	h, store, dir, cleanup := createTestRBACHandlerWithDir(t)
	defer cleanup()
	_, _, _, narrowed, _ := scopedCallers(t, store)
	groupID, err := store.CreateGroup("unreadable", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	dropAuthTable(t, dir, "connection_privileges")

	req := narrowed.wrap(httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/api/v1/rbac/groups/%d", groupID), nil))
	rec := httptest.NewRecorder()
	h.handleGroupSubpath(rec, req)
	assertGrantRefused(t, rec)
}
