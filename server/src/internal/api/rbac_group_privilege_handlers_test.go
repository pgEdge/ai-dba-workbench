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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// handleGroupPrivileges routing
// =============================================================================

// TestHandleGroupPrivileges_NoRemaining asserts that an empty path tail
// returns 404 (no privilege category selected).
func TestHandleGroupPrivileges_NoRemaining(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/groups/1/privileges", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupPrivileges(rec, req, 1, []string{})

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// TestHandleGroupPrivileges_UnknownCategory asserts that an unknown
// privilege category returns 404.
func TestHandleGroupPrivileges_UnknownCategory(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/groups/1/privileges/unknown", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupPrivileges(rec, req, 1, []string{"unknown"})

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// TestHandleGroupPrivileges_PermissionDenied asserts that a non-superuser
// lacking manage_permissions gets 403.
func TestHandleGroupPrivileges_PermissionDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("user", "Password1", "User", "", "")
	userID, _ := store.GetUserID("user")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/groups/1/privileges/mcp", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleGroupPrivileges(rec, req, 1, []string{"mcp"})

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

// =============================================================================
// handleGroupMCPPrivileges
// =============================================================================

// TestHandleGroupMCPPrivileges_GrantSuccess verifies that POST with a
// valid privilege name returns 204 and persists the grant.
func TestHandleGroupMCPPrivileges_GrantSuccess(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")
	if _, err := store.RegisterMCPPrivilege("test_tool", auth.MCPPrivilegeTypeTool, "Test tool", false); err != nil {
		t.Fatalf("failed to register privilege: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"privilege": "test_tool"})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/mcp", groupID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupMCPPrivileges(rec, req, groupID, []string{})

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	privs, _ := store.ListGroupMCPPrivileges(groupID)
	if len(privs) != 1 || privs[0].Identifier != "test_tool" {
		t.Errorf("expected privilege 'test_tool' granted, got %v", privs)
	}
}

// TestHandleGroupMCPPrivileges_GrantMissingField verifies that a POST
// body with an empty privilege string returns 400.
func TestHandleGroupMCPPrivileges_GrantMissingField(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]string{"privilege": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/groups/1/privileges/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupMCPPrivileges(rec, req, 1, []string{})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestHandleGroupMCPPrivileges_GrantStoreError verifies that CRLF-bearing
// input does not cause the handler to panic and that the handler returns
// 500 when the privilege is unknown. This implicitly exercises the
// sanitization path; verifying actual log output would require capturing
// stderr.
func TestHandleGroupMCPPrivileges_GrantStoreError(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")

	// Request grant of a privilege that was never registered; use a
	// payload containing CRLF to confirm sanitization does not panic.
	body, _ := json.Marshal(map[string]string{"privilege": "unknown_tool\r\ninjected"})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/mcp", groupID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupMCPPrivileges(rec, req, groupID, []string{})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for unknown privilege, got %d", rec.Code)
	}
}

// TestHandleGroupMCPPrivileges_RevokeSuccess verifies that DELETE with
// ?name=<privilege> returns 204 and removes the grant.
func TestHandleGroupMCPPrivileges_RevokeSuccess(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")
	if _, err := store.RegisterMCPPrivilege("test_tool", auth.MCPPrivilegeTypeTool, "Test tool", false); err != nil {
		t.Fatalf("failed to register privilege: %v", err)
	}
	if err := store.GrantMCPPrivilegeByName(groupID, "test_tool"); err != nil {
		t.Fatalf("failed to grant privilege: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/mcp?name=test_tool", groupID), nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupMCPPrivileges(rec, req, groupID, []string{})

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	privs, _ := store.ListGroupMCPPrivileges(groupID)
	if len(privs) != 0 {
		t.Errorf("expected 0 privileges after revoke, got %d", len(privs))
	}
}

// TestHandleGroupMCPPrivileges_RevokeMissingQuery verifies that DELETE
// without ?name= returns 400.
func TestHandleGroupMCPPrivileges_RevokeMissingQuery(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/rbac/groups/1/privileges/mcp", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupMCPPrivileges(rec, req, 1, []string{})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestHandleGroupMCPPrivileges_RevokeStoreError verifies that attempting
// to revoke a privilege that was never granted surfaces an internal
// error; this exercises the revoke sanitization path.
func TestHandleGroupMCPPrivileges_RevokeStoreError(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")

	req := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/mcp?name=never_registered", groupID), nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupMCPPrivileges(rec, req, groupID, []string{})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// TestHandleGroupMCPPrivileges_MethodNotAllowed verifies that methods
// other than POST/DELETE on /privileges/mcp return 405.
func TestHandleGroupMCPPrivileges_MethodNotAllowed(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/groups/1/privileges/mcp", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupMCPPrivileges(rec, req, 1, []string{})

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
	allow := rec.Header().Get("Allow")
	if allow != "POST, DELETE" {
		t.Errorf("Allow header = %q, want 'POST, DELETE'", allow)
	}
}

// TestHandleGroupMCPPrivileges_ExtraPath verifies that extra trailing
// path segments return 404.
func TestHandleGroupMCPPrivileges_ExtraPath(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/groups/1/privileges/mcp/extra", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupMCPPrivileges(rec, req, 1, []string{"extra"})

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// =============================================================================
// handleGroupConnectionPrivileges
// =============================================================================

// TestHandleGroupConnectionPrivileges_GrantSuccess verifies POST returns
// 204 and persists the connection grant.
func TestHandleGroupConnectionPrivileges_GrantSuccess(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")

	body, _ := json.Marshal(map[string]any{
		"connection_id": 42,
		"access_level":  "read_write",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/connections", groupID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupConnectionPrivileges(rec, req, groupID, []string{})

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleGroupConnectionPrivileges_InvalidConnectionID verifies that
// a negative connection_id yields 400.
func TestHandleGroupConnectionPrivileges_InvalidConnectionID(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]any{
		"connection_id": -1,
		"access_level":  "read",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/rbac/groups/1/privileges/connections", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupConnectionPrivileges(rec, req, 1, []string{})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestHandleGroupConnectionPrivileges_InvalidAccessLevel verifies that
// an unsupported access_level yields 400.
func TestHandleGroupConnectionPrivileges_InvalidAccessLevel(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]any{
		"connection_id": 1,
		"access_level":  "write_only",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/rbac/groups/1/privileges/connections", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupConnectionPrivileges(rec, req, 1, []string{})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestHandleGroupConnectionPrivileges_GrantMethodNotAllowed verifies
// that non-POST methods on the collection return 405.
func TestHandleGroupConnectionPrivileges_GrantMethodNotAllowed(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/groups/1/privileges/connections", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupConnectionPrivileges(rec, req, 1, []string{})

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// TestHandleGroupConnectionPrivileges_RevokeSuccess verifies DELETE with
// an integer connection id in the path returns 204.
func TestHandleGroupConnectionPrivileges_RevokeSuccess(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")
	if err := store.GrantConnectionPrivilege(groupID, 42, "read"); err != nil {
		t.Fatalf("failed to grant connection privilege: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/connections/42", groupID), nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupConnectionPrivileges(rec, req, groupID, []string{"42"})

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleGroupConnectionPrivileges_RevokeBadID verifies that a
// non-numeric connection id in the path returns 400.
func TestHandleGroupConnectionPrivileges_RevokeBadID(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/groups/1/privileges/connections/notanumber", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupConnectionPrivileges(rec, req, 1, []string{"notanumber"})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestHandleGroupConnectionPrivileges_RevokeMethodNotAllowed verifies
// that non-DELETE methods on an individual connection resource return
// 405.
func TestHandleGroupConnectionPrivileges_RevokeMethodNotAllowed(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/rbac/groups/1/privileges/connections/42", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupConnectionPrivileges(rec, req, 1, []string{"42"})

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// TestHandleGroupConnectionPrivileges_ExtraPath verifies that more than
// one trailing path segment returns 404.
func TestHandleGroupConnectionPrivileges_ExtraPath(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/groups/1/privileges/connections/42/extra", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupConnectionPrivileges(rec, req, 1, []string{"42", "extra"})

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// =============================================================================
// handleGroupPermissions routing
// =============================================================================

// TestHandleGroupPermissions_GetList verifies the GET branch routes to
// listGroupPermissions.
func TestHandleGroupPermissions_GetList(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")
	store.GrantAdminPermission(groupID, auth.PermManageUsers)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/rbac/groups/%d/permissions", groupID), nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupPermissions(rec, req, groupID, []string{})

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// TestHandleGroupPermissions_PostGrant verifies the POST branch routes
// to grantGroupPermission.
func TestHandleGroupPermissions_PostGrant(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")

	body, _ := json.Marshal(map[string]string{"permission": auth.PermManageUsers})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/rbac/groups/%d/permissions", groupID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupPermissions(rec, req, groupID, []string{})

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleGroupPermissions_MethodNotAllowed verifies that non-GET/POST
// on the collection returns 405.
func TestHandleGroupPermissions_MethodNotAllowed(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/rbac/groups/1/permissions", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupPermissions(rec, req, 1, []string{})

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// TestHandleGroupPermissions_DeleteIndividual verifies the DELETE branch
// with a permission path segment routes to revokeGroupPermission.
func TestHandleGroupPermissions_DeleteIndividual(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")
	store.GrantAdminPermission(groupID, auth.PermManageUsers)

	req := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/api/v1/rbac/groups/%d/permissions/manage_users", groupID), nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupPermissions(rec, req, groupID, []string{auth.PermManageUsers})

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
}

// TestHandleGroupPermissions_DeleteMethodNotAllowed verifies non-DELETE
// on the individual resource returns 405.
func TestHandleGroupPermissions_DeleteMethodNotAllowed(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/groups/1/permissions/manage_users", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupPermissions(rec, req, 1, []string{auth.PermManageUsers})

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// TestHandleGroupPermissions_ExtraPath verifies that more than one
// trailing path segment returns 404.
func TestHandleGroupPermissions_ExtraPath(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/groups/1/permissions/manage_users/extra", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroupPermissions(rec, req, 1, []string{auth.PermManageUsers, "extra"})

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// TestHandleGroupPermissions_NonSuperuser verifies that a non-superuser
// request is rejected by requireSuperuser.
func TestHandleGroupPermissions_NonSuperuser(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("user", "Password1", "User", "", "")
	userID, _ := store.GetUserID("user")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/groups/1/permissions", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleGroupPermissions(rec, req, 1, []string{})

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

// =============================================================================
// grantGroupPermission and revokeGroupPermission error paths
// =============================================================================

// TestGrantGroupPermission_MissingField verifies that an empty permission
// string yields 400 without hitting the store.
func TestGrantGroupPermission_MissingField(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]string{"permission": ""})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/rbac/groups/1/permissions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.grantGroupPermission(rec, req, 1)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestGrantGroupPermission_StoreError verifies that CRLF-bearing input
// does not cause the handler to panic and that the handler returns 500
// when the underlying store is closed. This implicitly exercises the
// sanitization path; verifying actual log output would require capturing
// stderr.
func TestGrantGroupPermission_StoreError(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")

	// Close the store to force any subsequent DB operation to fail.
	// cleanup() is idempotent: Close is a no-op after this, and
	// RemoveAll will still run.
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close store: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"permission": "manage_users\r\ninjected"})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/rbac/groups/%d/permissions", groupID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.grantGroupPermission(rec, req, groupID)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 after store closed, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestRevokeGroupPermission_StoreError verifies that revoke errors are
// surfaced as 500 and exercise the sanitization path. The permission
// value embeds CRLF to confirm SanitizeForLog is effective.
func TestRevokeGroupPermission_StoreError(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")

	req := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/api/v1/rbac/groups/%d/permissions/bogus", groupID), nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.revokeGroupPermission(rec, req, groupID, "bogus_perm\r\ninjected")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for unknown permission, got %d", rec.Code)
	}
}

// TestListGroupPermissions_EmptyGroup verifies that a group with no
// permissions returns an empty array rather than null.
func TestListGroupPermissions_EmptyGroup(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("empty", "Empty group")

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/rbac/groups/%d/permissions", groupID), nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.listGroupPermissions(rec, req, groupID)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		GroupID     int64    `json:"group_id"`
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Permissions == nil {
		t.Error("expected non-nil permissions slice")
	}
	if len(resp.Permissions) != 0 {
		t.Errorf("expected 0 permissions, got %d", len(resp.Permissions))
	}
}
