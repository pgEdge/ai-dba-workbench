/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// createTestRBACHandler creates an RBACHandler with a temp auth store for testing
func createTestRBACHandler(t *testing.T, authEnabled bool) (*RBACHandler, *auth.AuthStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "rbac-handler-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	store, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create auth store: %v", err)
	}

	checker := auth.NewRBACChecker(store, authEnabled)
	handler := NewRBACHandler(store, checker)

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return handler, store, cleanup
}

// withSuperuser adds superuser context values to a request
func withSuperuser(req *http.Request) *http.Request {
	ctx := context.WithValue(req.Context(), auth.IsSuperuserContextKey, true)
	return req.WithContext(ctx)
}

// withUser adds user context values to a request
func withUser(req *http.Request, userID int64) *http.Request {
	ctx := context.WithValue(req.Context(), auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	return req.WithContext(ctx)
}

// =============================================================================
// Group Creation and Listing Tests
// =============================================================================

func TestRBACHandler_CreateAndListGroups(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	// Grant manage_groups to a group and assign user
	store.CreateUser("admin", "password", "Admin user")
	userID, _ := store.GetUserID("admin")
	groupID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(groupID, userID)
	store.GrantAdminPermission(groupID, auth.PermManageGroups)

	// Create a group
	body, _ := json.Marshal(map[string]string{
		"name":        "developers",
		"description": "Developer group",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleGroups(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusCreated, rec.Code, rec.Body.String())
	}

	// List groups
	req = httptest.NewRequest(http.MethodGet, "/api/v1/rbac/groups", nil)
	req = withUser(req, userID)
	rec = httptest.NewRecorder()

	handler.handleGroups(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestRBACHandler_CreateGroup_MissingName(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password", "Admin")
	userID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, userID)
	store.GrantAdminPermission(gID, auth.PermManageGroups)

	body := `{"description": "No name"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/groups",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleGroups(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// =============================================================================
// Group Member Tests
// =============================================================================

func TestRBACHandler_AddAndRemoveMembers(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	// Setup admin user with manage_groups permission
	store.CreateUser("admin", "password", "Admin")
	adminID, _ := store.GetUserID("admin")
	adminGroupID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(adminGroupID, adminID)
	store.GrantAdminPermission(adminGroupID, auth.PermManageGroups)

	// Create target group and user
	targetGroupID, _ := store.CreateGroup("devs", "Developers")
	store.CreateUser("dev1", "password", "Developer")
	devID, _ := store.GetUserID("dev1")

	// Add user to group
	body, _ := json.Marshal(map[string]int64{"user_id": devID})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/rbac/groups/"+itoa(targetGroupID)+"/members",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.addGroupMember(rec, req, targetGroupID)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}

	// Remove user from group
	req = httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/groups/"+itoa(targetGroupID)+"/members/user/"+itoa64(devID),
		nil)
	req = withUser(req, adminID)
	rec = httptest.NewRecorder()

	handler.removeGroupMember(rec, req, targetGroupID, "user", devID)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}
}

// =============================================================================
// Admin Permission Grant/Revoke Tests
// =============================================================================

func TestRBACHandler_GrantAdminPermission_SuperuserRequired(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")

	// Grant permission as superuser
	body, _ := json.Marshal(map[string]string{
		"permission": auth.PermManageConnections,
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/rbac/groups/1/permissions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.grantGroupPermission(rec, req, groupID)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}

	// Verify the permission was granted
	perms, _ := store.ListGroupAdminPermissions(groupID)
	if len(perms) != 1 || perms[0] != auth.PermManageConnections {
		t.Errorf("Expected [%s], got %v", auth.PermManageConnections, perms)
	}
}

func TestRBACHandler_RevokeAdminPermission(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")
	store.GrantAdminPermission(groupID, auth.PermManageUsers)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/groups/1/permissions/manage_users", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.revokeGroupPermission(rec, req, groupID, auth.PermManageUsers)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}

	// Verify revoked
	perms, _ := store.ListGroupAdminPermissions(groupID)
	if len(perms) != 0 {
		t.Errorf("Expected 0 permissions after revocation, got %d", len(perms))
	}
}

func TestRBACHandler_PermissionEnforcement_NonSuperuser403(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	// Create non-superuser without any admin permissions
	store.CreateUser("normie", "password", "Normal user")
	userID, _ := store.GetUserID("normie")
	store.CreateGroup("target", "Target group")

	// Try to grant permission as non-superuser (requires superuser)
	body, _ := json.Marshal(map[string]string{
		"permission": auth.PermManageConnections,
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/rbac/groups/1/permissions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleGroupPermissions(rec, req, 1, []string{})

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_ListGroupPermissions(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	groupID, _ := store.CreateGroup("target", "Target group")
	store.GrantAdminPermission(groupID, auth.PermManageConnections)
	store.GrantAdminPermission(groupID, auth.PermManageUsers)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/groups/1/permissions", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.listGroupPermissions(rec, req, groupID)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	perms, ok := resp["permissions"].([]interface{})
	if !ok {
		t.Fatal("Expected permissions array in response")
	}

	if len(perms) != 2 {
		t.Errorf("Expected 2 permissions, got %d", len(perms))
	}
}

// =============================================================================
// User Listing Tests
// =============================================================================

func TestRBACHandler_ListUsers(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	// Create admin with manage_users permission
	store.CreateUser("admin", "password", "Admin")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	// Create another user
	store.CreateUser("user2", "password", "User 2")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp struct {
		Users []map[string]interface{} `json:"users"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should have at least the 2 users we created
	if len(resp.Users) < 2 {
		t.Errorf("Expected at least 2 users, got %d", len(resp.Users))
	}
}

func TestRBACHandler_ListUsers_PermissionDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	// Create user without manage_users permission
	store.CreateUser("normie", "password", "Normal")
	userID, _ := store.GetUserID("normie")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestRBACHandler_HandleGroups_MethodNotAllowed(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/rbac/groups", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleGroups(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d",
			http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, POST" {
		t.Errorf("Expected Allow header 'GET, POST', got %q", allowed)
	}
}

func TestRBACHandler_HandleUsers_MethodNotAllowed(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/rbac/users", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d",
			http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, POST" {
		t.Errorf("Expected Allow header 'GET, POST', got %q", allowed)
	}
}

// =============================================================================
// Helpers
// =============================================================================

func itoa(n int64) string {
	return itoa64(n)
}

func itoa64(n int64) string {
	return fmt.Sprintf("%d", n)
}
