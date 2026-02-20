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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Section 1: Shared Test Helpers
// =============================================================================

// withToken injects token-based auth context values into a request.
func withToken(req *http.Request, userID int64, tokenID int64) *http.Request {
	ctx := context.WithValue(req.Context(), auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.TokenIDContextKey, tokenID)
	ctx = context.WithValue(ctx, auth.IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	return req.WithContext(ctx)
}

// setupUserWithPermission creates a user, a group, grants one admin
// permission to the group, and adds the user to the group.
// Returns the user ID.
func setupUserWithPermission(t *testing.T, store *auth.AuthStore, username string, permission string) int64 {
	t.Helper()
	return setupUserWithPermissions(t, store, username, []string{permission})
}

// setupUserWithPermissions creates a user, a group, grants multiple admin
// permissions to the group, and adds the user to the group.
// Returns the user ID.
func setupUserWithPermissions(t *testing.T, store *auth.AuthStore, username string, permissions []string) int64 {
	t.Helper()

	if err := store.CreateUser(username, "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user %s: %v", username, err)
	}
	userID, err := store.GetUserID(username)
	if err != nil {
		t.Fatalf("Failed to get user ID for %s: %v", username, err)
	}

	groupName := username + "_group"
	groupID, err := store.CreateGroup(groupName, "Group for "+username)
	if err != nil {
		t.Fatalf("Failed to create group %s: %v", groupName, err)
	}
	if err := store.AddUserToGroup(groupID, userID); err != nil {
		t.Fatalf("Failed to add user to group: %v", err)
	}

	for _, perm := range permissions {
		if err := store.GrantAdminPermission(groupID, perm); err != nil {
			t.Fatalf("Failed to grant permission %s: %v", perm, err)
		}
	}

	return userID
}

// createTokenForUser creates an API token for the given user.
// Returns (tokenID, rawToken).
func createTokenForUser(t *testing.T, store *auth.AuthStore, userID int64, tokenName string) (int64, string) {
	t.Helper()

	user, err := store.GetUserByID(userID)
	if err != nil || user == nil {
		t.Fatalf("Failed to get user by ID %d: %v", userID, err)
	}

	rawToken, storedToken, err := store.CreateToken(user.Username, tokenName, nil)
	if err != nil {
		t.Fatalf("Failed to create token for user %s: %v", user.Username, err)
	}

	return storedToken.ID, rawToken
}

// requireStatus is an assertion helper that fails the test if the
// recorded HTTP status code does not match the expected value.
func requireStatus(t *testing.T, rec *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if rec.Code != expected {
		t.Errorf("Expected status %d, got %d. Body: %s",
			expected, rec.Code, rec.Body.String())
	}
}

// =============================================================================
// Section 2: Admin Permission Enforcement Matrix
// =============================================================================

func TestRBACEnforcement_AdminPermissions(t *testing.T) {
	type endpointDef struct {
		name       string
		method     string
		url        string
		permission string
		handler    func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc
	}

	endpoints := []endpointDef{
		{
			name:       "RBACHandler.handleUsers_GET",
			method:     http.MethodGet,
			url:        "/api/v1/rbac/users",
			permission: auth.PermManageUsers,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewRBACHandler(store, checker)
				return h.handleUsers
			},
		},
		{
			name:       "RBACHandler.handleUsers_POST",
			method:     http.MethodPost,
			url:        "/api/v1/rbac/users",
			permission: auth.PermManageUsers,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewRBACHandler(store, checker)
				return h.handleUsers
			},
		},
		{
			name:       "RBACHandler.handleGroups_GET",
			method:     http.MethodGet,
			url:        "/api/v1/rbac/groups",
			permission: auth.PermManageGroups,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewRBACHandler(store, checker)
				return h.handleGroups
			},
		},
		{
			name:       "RBACHandler.handleGroups_POST",
			method:     http.MethodPost,
			url:        "/api/v1/rbac/groups",
			permission: auth.PermManageGroups,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewRBACHandler(store, checker)
				return h.handleGroups
			},
		},
		{
			name:       "RBACHandler.handleTokens_GET",
			method:     http.MethodGet,
			url:        "/api/v1/rbac/tokens",
			permission: auth.PermManageTokenScopes,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewRBACHandler(store, checker)
				return h.handleTokens
			},
		},
		{
			name:       "RBACHandler.handleTokens_POST",
			method:     http.MethodPost,
			url:        "/api/v1/rbac/tokens",
			permission: auth.PermManageTokenScopes,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewRBACHandler(store, checker)
				return h.handleTokens
			},
		},
		{
			name:       "AlertRuleHandler.handleAlertRuleSubpath_PUT",
			method:     http.MethodPut,
			url:        "/api/v1/alert-rules/1",
			permission: auth.PermManageAlertRules,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewAlertRuleHandler(nil, store, checker)
				return h.handleAlertRuleSubpath
			},
		},
		{
			name:       "ProbeConfigHandler.handleProbeConfigSubpath_PUT",
			method:     http.MethodPut,
			url:        "/api/v1/probe-configs/1",
			permission: auth.PermManageProbes,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewProbeConfigHandler(nil, store, checker)
				return h.handleProbeConfigSubpath
			},
		},
		{
			name:       "BlackoutHandler.handleBlackouts_POST",
			method:     http.MethodPost,
			url:        "/api/v1/blackouts",
			permission: auth.PermManageBlackouts,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewBlackoutHandler(nil, store, checker)
				return h.handleBlackouts
			},
		},
		{
			name:       "BlackoutHandler.handleBlackoutSubpath_PUT",
			method:     http.MethodPut,
			url:        "/api/v1/blackouts/1",
			permission: auth.PermManageBlackouts,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewBlackoutHandler(nil, store, checker)
				return h.handleBlackoutSubpath
			},
		},
		{
			name:       "BlackoutHandler.handleBlackoutSubpath_DELETE",
			method:     http.MethodDelete,
			url:        "/api/v1/blackouts/1",
			permission: auth.PermManageBlackouts,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewBlackoutHandler(nil, store, checker)
				return h.handleBlackoutSubpath
			},
		},
		{
			name:       "NotificationChannelHandler.handleChannels_POST",
			method:     http.MethodPost,
			url:        "/api/v1/notification-channels",
			permission: auth.PermManageNotificationChannels,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewNotificationChannelHandler(nil, store, checker)
				return h.handleChannels
			},
		},
		{
			name:       "NotificationChannelHandler.handleChannelSubpath_PUT",
			method:     http.MethodPut,
			url:        "/api/v1/notification-channels/1",
			permission: auth.PermManageNotificationChannels,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewNotificationChannelHandler(nil, store, checker)
				return h.handleChannelSubpath
			},
		},
		{
			name:       "NotificationChannelHandler.handleChannelSubpath_DELETE",
			method:     http.MethodDelete,
			url:        "/api/v1/notification-channels/1",
			permission: auth.PermManageNotificationChannels,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewNotificationChannelHandler(nil, store, checker)
				return h.handleChannelSubpath
			},
		},
		{
			name:       "AlertOverrideHandler.handleAlertOverrides_PUT",
			method:     http.MethodPut,
			url:        "/api/v1/alert-overrides/server/1/1",
			permission: auth.PermManageAlertRules,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewAlertOverrideHandler(nil, store, checker)
				return h.handleAlertOverrides
			},
		},
		{
			name:       "ProbeOverrideHandler.handleProbeOverrides_PUT",
			method:     http.MethodPut,
			url:        "/api/v1/probe-overrides/server/1/some_probe",
			permission: auth.PermManageProbes,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewProbeOverrideHandler(nil, store, checker)
				return h.handleProbeOverrides
			},
		},
		{
			name:       "ChannelOverrideHandler.handleChannelOverrides_PUT",
			method:     http.MethodPut,
			url:        "/api/v1/channel-overrides/server/1/1",
			permission: auth.PermManageNotificationChannels,
			handler: func(store *auth.AuthStore, checker *auth.RBACChecker) http.HandlerFunc {
				h := NewChannelOverrideHandler(nil, store, checker)
				return h.handleChannelOverrides
			},
		},
	}

	// pickWrongPermission returns a permission that is different from the required one.
	pickWrongPermission := func(required string) string {
		candidates := []string{
			auth.PermManageUsers,
			auth.PermManageGroups,
			auth.PermManageBlackouts,
			auth.PermManageAlertRules,
			auth.PermManageProbes,
			auth.PermManageNotificationChannels,
			auth.PermManageTokenScopes,
		}
		for _, c := range candidates {
			if c != required {
				return c
			}
		}
		return auth.PermManageConnections
	}

	for _, ep := range endpoints {
		ep := ep
		t.Run(ep.name, func(t *testing.T) {
			// Build a minimal JSON body for POST/PUT requests.
			var body []byte
			if ep.method == http.MethodPost || ep.method == http.MethodPut {
				body, _ = json.Marshal(map[string]string{"name": "test", "description": "test"})
			}

			// Sub-test 1: Superuser is not denied.
			// Handlers with nil datastores may panic after the permission
			// check passes; a panic means the security gate was cleared.
			t.Run("Superuser_NotDenied", func(t *testing.T) {
				_, store, cleanup := createTestRBACHandler(t)
				defer cleanup()
				checker := auth.NewRBACChecker(store)
				handlerFn := ep.handler(store, checker)

				req := httptest.NewRequest(ep.method, ep.url, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req = withSuperuser(req)
				rec := httptest.NewRecorder()

				func() {
					defer func() { recover() }() //nolint:errcheck // nil-datastore panic is expected
					handlerFn(rec, req)
				}()

				if rec.Code == http.StatusForbidden {
					t.Errorf("Superuser should not get 403, got %d. Body: %s",
						rec.Code, rec.Body.String())
				}
			})

			// Sub-test 2: User with correct permission is not denied.
			t.Run("CorrectPermission_NotDenied", func(t *testing.T) {
				_, store, cleanup := createTestRBACHandler(t)
				defer cleanup()
				checker := auth.NewRBACChecker(store)
				handlerFn := ep.handler(store, checker)

				userID := setupUserWithPermission(t, store, "permitted_user", ep.permission)
				req := httptest.NewRequest(ep.method, ep.url, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req = withUser(req, userID)
				rec := httptest.NewRecorder()

				func() {
					defer func() { recover() }() //nolint:errcheck // nil-datastore panic is expected
					handlerFn(rec, req)
				}()

				if rec.Code == http.StatusForbidden {
					t.Errorf("User with %s should not get 403, got %d. Body: %s",
						ep.permission, rec.Code, rec.Body.String())
				}
			})

			// Sub-test 3: User without any permission gets 403.
			t.Run("NoPermission_403", func(t *testing.T) {
				_, store, cleanup := createTestRBACHandler(t)
				defer cleanup()
				checker := auth.NewRBACChecker(store)
				handlerFn := ep.handler(store, checker)

				if err := store.CreateUser("noperm", "Password1", "", "", ""); err != nil {
					t.Fatalf("Failed to create user: %v", err)
				}
				userID, _ := store.GetUserID("noperm")
				req := httptest.NewRequest(ep.method, ep.url, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req = withUser(req, userID)
				rec := httptest.NewRecorder()
				handlerFn(rec, req)

				requireStatus(t, rec, http.StatusForbidden)
			})

			// Sub-test 4: User with wrong permission gets 403.
			t.Run("WrongPermission_403", func(t *testing.T) {
				_, store, cleanup := createTestRBACHandler(t)
				defer cleanup()
				checker := auth.NewRBACChecker(store)
				handlerFn := ep.handler(store, checker)

				wrongPerm := pickWrongPermission(ep.permission)
				userID := setupUserWithPermission(t, store, "wrongperm_user", wrongPerm)
				req := httptest.NewRequest(ep.method, ep.url, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req = withUser(req, userID)
				rec := httptest.NewRecorder()
				handlerFn(rec, req)

				requireStatus(t, rec, http.StatusForbidden)
			})
		})
	}
}

// =============================================================================
// Section 3: Token Admin Scope Enforcement
// =============================================================================

func TestRBACEnforcement_TokenAdminScope(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// Create user with wildcard admin permissions.
	userID := setupUserWithPermission(t, store, "admin_user", "*")

	// Create a token scoped to manage_users and manage_groups only.
	tokenID, _ := createTokenForUser(t, store, userID, "scoped_token")
	if err := store.SetTokenAdminScope(tokenID, []string{
		auth.PermManageUsers,
		auth.PermManageGroups,
	}); err != nil {
		t.Fatalf("Failed to set token admin scope: %v", err)
	}

	checker := auth.NewRBACChecker(store)
	rbacHandler := NewRBACHandler(store, checker)
	blackoutHandler := NewBlackoutHandler(nil, store, checker)
	alertRuleHandler := NewAlertRuleHandler(nil, store, checker)

	t.Run("TokenScope_UserList_Allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
		req = withToken(req, userID, tokenID)
		rec := httptest.NewRecorder()
		rbacHandler.handleUsers(rec, req)

		if rec.Code == http.StatusForbidden {
			t.Errorf("Token with manage_users scope should access user list, got %d", rec.Code)
		}
	})

	t.Run("TokenScope_GroupList_Allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/groups", nil)
		req = withToken(req, userID, tokenID)
		rec := httptest.NewRecorder()
		rbacHandler.handleGroups(rec, req)

		if rec.Code == http.StatusForbidden {
			t.Errorf("Token with manage_groups scope should access group list, got %d", rec.Code)
		}
	})

	t.Run("TokenScope_BlackoutCreate_Denied", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"scope": "global", "reason": "test"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/blackouts", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withToken(req, userID, tokenID)
		rec := httptest.NewRecorder()
		blackoutHandler.handleBlackouts(rec, req)

		requireStatus(t, rec, http.StatusForbidden)
	})

	t.Run("TokenScope_AlertRuleUpdate_Denied", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"name": "test"})
		req := httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules/1", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withToken(req, userID, tokenID)
		rec := httptest.NewRecorder()
		alertRuleHandler.handleAlertRuleSubpath(rec, req)

		requireStatus(t, rec, http.StatusForbidden)
	})

	// Create a second token with wildcard admin scope.
	wildcardTokenID, _ := createTokenForUser(t, store, userID, "wildcard_token")
	if err := store.SetTokenAdminScope(wildcardTokenID, []string{"*"}); err != nil {
		t.Fatalf("Failed to set wildcard token admin scope: %v", err)
	}

	t.Run("WildcardTokenScope_UserList_Allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
		req = withToken(req, userID, wildcardTokenID)
		rec := httptest.NewRecorder()
		rbacHandler.handleUsers(rec, req)

		if rec.Code == http.StatusForbidden {
			t.Errorf("Wildcard token should access user list, got %d", rec.Code)
		}
	})

	t.Run("WildcardTokenScope_BlackoutCreate_Allowed", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"scope": "global", "reason": "test"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/blackouts", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withToken(req, userID, wildcardTokenID)
		rec := httptest.NewRecorder()
		blackoutHandler.handleBlackouts(rec, req)

		// Should not be 403 (may be other error due to nil datastore).
		if rec.Code == http.StatusForbidden {
			t.Errorf("Wildcard token should not get 403, got %d. Body: %s",
				rec.Code, rec.Body.String())
		}
	})

	t.Run("WildcardTokenScope_AlertRuleUpdate_Allowed", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"name": "test"})
		req := httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules/1", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withToken(req, userID, wildcardTokenID)
		rec := httptest.NewRecorder()

		func() {
			defer func() { recover() }() //nolint:errcheck // nil-datastore panic is expected
			alertRuleHandler.handleAlertRuleSubpath(rec, req)
		}()

		if rec.Code == http.StatusForbidden {
			t.Errorf("Wildcard token should not get 403, got %d. Body: %s",
				rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// Section 4: MCP Tool Privilege Enforcement
// =============================================================================

func TestRBACEnforcement_MCPToolAccess(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	checker := auth.NewRBACChecker(store)

	// Register two MCP privileges.
	toolAID, err := store.RegisterMCPPrivilege("tool_a", "tool", "Tool A")
	if err != nil {
		t.Fatalf("Failed to register tool_a: %v", err)
	}
	toolBID, err := store.RegisterMCPPrivilege("tool_b", "tool", "Tool B")
	if err != nil {
		t.Fatalf("Failed to register tool_b: %v", err)
	}
	// Register a tool that will not be assigned to any group (unrestricted).
	_, err = store.RegisterMCPPrivilege("tool_unrestricted", "tool", "Unrestricted Tool")
	if err != nil {
		t.Fatalf("Failed to register tool_unrestricted: %v", err)
	}

	// Create a group and grant tool_a to it.
	groupID, err := store.CreateGroup("mcp_group", "MCP Group")
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}
	if err := store.GrantMCPPrivilege(groupID, toolAID); err != nil {
		t.Fatalf("Failed to grant MCP privilege: %v", err)
	}
	// Also grant tool_b to the group.
	if err := store.GrantMCPPrivilege(groupID, toolBID); err != nil {
		t.Fatalf("Failed to grant MCP privilege tool_b: %v", err)
	}

	// Create user in the group.
	if err := store.CreateUser("mcpuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	mcpUserID, _ := store.GetUserID("mcpuser")
	if err := store.AddUserToGroup(groupID, mcpUserID); err != nil {
		t.Fatalf("Failed to add user to group: %v", err)
	}

	// Create a user NOT in the group.
	if err := store.CreateUser("outsider", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create outsider user: %v", err)
	}
	outsiderID, _ := store.GetUserID("outsider")

	// Create a superuser.
	if err := store.CreateUser("superadmin", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create superadmin: %v", err)
	}
	if err := store.SetUserSuperuser("superadmin", true); err != nil {
		t.Fatalf("Failed to set superuser: %v", err)
	}

	t.Run("UnrestrictedTool_AccessibleByAll", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, outsiderID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		if !checker.CanAccessMCPItem(ctx, "tool_unrestricted") {
			t.Error("Unrestricted tool should be accessible by any user")
		}
	})

	t.Run("RestrictedTool_UserInGroup_Accessible", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, mcpUserID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		if !checker.CanAccessMCPItem(ctx, "tool_a") {
			t.Error("User in granted group should access restricted tool")
		}
	})

	t.Run("RestrictedTool_UserNotInGroup_Denied", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, outsiderID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		if checker.CanAccessMCPItem(ctx, "tool_a") {
			t.Error("User not in granted group should be denied restricted tool")
		}
	})

	t.Run("Superuser_AlwaysAccessible", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, true)
		if !checker.CanAccessMCPItem(ctx, "tool_a") {
			t.Error("Superuser should always access any tool")
		}
	})

	// Create a token scoped to tool_a only.
	tokenID, _ := createTokenForUser(t, store, mcpUserID, "mcp_token")
	if err := store.SetTokenMCPScope(tokenID, []int64{toolAID}); err != nil {
		t.Fatalf("Failed to set token MCP scope: %v", err)
	}

	t.Run("Token_MCPScope_IncludingTool_Accessible", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, mcpUserID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		ctx = context.WithValue(ctx, auth.TokenIDContextKey, tokenID)
		if !checker.CanAccessMCPItem(ctx, "tool_a") {
			t.Error("Token with tool_a in scope should access tool_a")
		}
	})

	t.Run("Token_MCPScope_ExcludingTool_Denied", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, mcpUserID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		ctx = context.WithValue(ctx, auth.TokenIDContextKey, tokenID)
		if checker.CanAccessMCPItem(ctx, "tool_b") {
			t.Error("Token without tool_b in scope should be denied tool_b")
		}
	})

	// Create a token with wildcard MCP scope.
	wildcardTokenID, _ := createTokenForUser(t, store, mcpUserID, "wildcard_mcp_token")
	if err := store.SetTokenMCPScope(wildcardTokenID, []int64{auth.MCPPrivilegeIDWildcard}); err != nil {
		t.Fatalf("Failed to set wildcard token MCP scope: %v", err)
	}

	t.Run("Token_WildcardMCPScope_AllAccessible", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, mcpUserID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		ctx = context.WithValue(ctx, auth.TokenIDContextKey, wildcardTokenID)

		if !checker.CanAccessMCPItem(ctx, "tool_a") {
			t.Error("Wildcard MCP scope should allow tool_a")
		}
		if !checker.CanAccessMCPItem(ctx, "tool_b") {
			t.Error("Wildcard MCP scope should allow tool_b")
		}
	})
}

// =============================================================================
// Section 5: Connection Access and Token Scoping
// =============================================================================

func TestRBACEnforcement_ConnectionAccess(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	checker := auth.NewRBACChecker(store)

	// Create a group granting connection 1 (read_write) and connection 2 (read).
	groupID, err := store.CreateGroup("conn_group", "Connection group")
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}
	if err := store.GrantConnectionPrivilege(groupID, 1, auth.AccessLevelReadWrite); err != nil {
		t.Fatalf("Failed to grant connection 1: %v", err)
	}
	if err := store.GrantConnectionPrivilege(groupID, 2, auth.AccessLevelRead); err != nil {
		t.Fatalf("Failed to grant connection 2: %v", err)
	}

	// Create user in the group.
	if err := store.CreateUser("connuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	connUserID, _ := store.GetUserID("connuser")
	if err := store.AddUserToGroup(groupID, connUserID); err != nil {
		t.Fatalf("Failed to add user to group: %v", err)
	}

	// Create user without any connection grants.
	if err := store.CreateUser("noconn", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create no-conn user: %v", err)
	}
	noConnUserID, _ := store.GetUserID("noconn")

	// Create a superuser.
	if err := store.CreateUser("superconn", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create superconn user: %v", err)
	}
	if err := store.SetUserSuperuser("superconn", true); err != nil {
		t.Fatalf("Failed to set superuser: %v", err)
	}

	t.Run("UserWithGroup_Connection1_ReadWrite", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, connUserID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)

		canAccess, level := checker.CanAccessConnection(ctx, 1)
		if !canAccess {
			t.Error("User in group should access connection 1")
		}
		if level != auth.AccessLevelReadWrite {
			t.Errorf("Expected read_write, got %s", level)
		}
	})

	t.Run("UserWithGroup_Connection2_Read", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, connUserID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)

		canAccess, level := checker.CanAccessConnection(ctx, 2)
		if !canAccess {
			t.Error("User in group should access connection 2")
		}
		if level != auth.AccessLevelRead {
			t.Errorf("Expected read, got %s", level)
		}
	})

	t.Run("UserWithoutGrant_Denied", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, noConnUserID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)

		canAccess, _ := checker.CanAccessConnection(ctx, 1)
		if canAccess {
			t.Error("User without connection grant should be denied")
		}
	})

	// Token scoped to connection 1 only.
	tokenID, _ := createTokenForUser(t, store, connUserID, "conn_token")
	if err := store.SetTokenConnectionScope(tokenID, []auth.ScopedConnection{
		{ConnectionID: 1, AccessLevel: auth.AccessLevelReadWrite},
	}); err != nil {
		t.Fatalf("Failed to set token connection scope: %v", err)
	}

	t.Run("Token_ScopedToConn1_CanAccessConn1", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, connUserID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		ctx = context.WithValue(ctx, auth.TokenIDContextKey, tokenID)

		canAccess, level := checker.CanAccessConnection(ctx, 1)
		if !canAccess {
			t.Error("Token scoped to connection 1 should access it")
		}
		if level != auth.AccessLevelReadWrite {
			t.Errorf("Expected read_write, got %s", level)
		}
	})

	t.Run("Token_ScopedToConn1_CannotAccessConn2", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, connUserID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		ctx = context.WithValue(ctx, auth.TokenIDContextKey, tokenID)

		canAccess, _ := checker.CanAccessConnection(ctx, 2)
		if canAccess {
			t.Error("Token scoped to connection 1 should not access connection 2")
		}
	})

	// Token that downgrades access level.
	downgradeTokenID, _ := createTokenForUser(t, store, connUserID, "downgrade_token")
	if err := store.SetTokenConnectionScope(downgradeTokenID, []auth.ScopedConnection{
		{ConnectionID: 1, AccessLevel: auth.AccessLevelRead},
	}); err != nil {
		t.Fatalf("Failed to set downgrade token scope: %v", err)
	}

	t.Run("Token_DowngradesAccessLevel", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, connUserID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		ctx = context.WithValue(ctx, auth.TokenIDContextKey, downgradeTokenID)

		canAccess, level := checker.CanAccessConnection(ctx, 1)
		if !canAccess {
			t.Error("Token should still access connection 1")
		}
		if level != auth.AccessLevelRead {
			t.Errorf("Token scope should downgrade to read, got %s", level)
		}
	})

	// Token with wildcard connection scope (read).
	wildcardReadTokenID, _ := createTokenForUser(t, store, connUserID, "wildcard_read_token")
	if err := store.SetTokenConnectionScope(wildcardReadTokenID, []auth.ScopedConnection{
		{ConnectionID: auth.ConnectionIDAll, AccessLevel: auth.AccessLevelRead},
	}); err != nil {
		t.Fatalf("Failed to set wildcard read token scope: %v", err)
	}

	t.Run("Token_WildcardConnectionScope_Read_CapsAtRead", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, connUserID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		ctx = context.WithValue(ctx, auth.TokenIDContextKey, wildcardReadTokenID)

		canAccess, level := checker.CanAccessConnection(ctx, 1)
		if !canAccess {
			t.Error("Wildcard read token should access connection 1")
		}
		if level != auth.AccessLevelRead {
			t.Errorf("Wildcard read token should cap at read, got %s", level)
		}
	})

	// Token with wildcard connection scope (read_write).
	wildcardRWTokenID, _ := createTokenForUser(t, store, connUserID, "wildcard_rw_token")
	if err := store.SetTokenConnectionScope(wildcardRWTokenID, []auth.ScopedConnection{
		{ConnectionID: auth.ConnectionIDAll, AccessLevel: auth.AccessLevelReadWrite},
	}); err != nil {
		t.Fatalf("Failed to set wildcard rw token scope: %v", err)
	}

	t.Run("Token_WildcardConnectionScope_ReadWrite_FullAccess", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.UserIDContextKey, connUserID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		ctx = context.WithValue(ctx, auth.TokenIDContextKey, wildcardRWTokenID)

		canAccess, level := checker.CanAccessConnection(ctx, 1)
		if !canAccess {
			t.Error("Wildcard read_write token should access connection 1")
		}
		if level != auth.AccessLevelReadWrite {
			t.Errorf("Expected read_write, got %s", level)
		}
	})

	t.Run("Superuser_AlwaysFullAccess", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, true)

		canAccess, level := checker.CanAccessConnection(ctx, 999)
		if !canAccess {
			t.Error("Superuser should always have access")
		}
		if level != auth.AccessLevelReadWrite {
			t.Errorf("Superuser should get read_write, got %s", level)
		}
	})
}

// =============================================================================
// Section 6: Effective Privileges Endpoint
// =============================================================================

func TestRBACEnforcement_GetUserPrivileges(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// Create a superuser target.
	if err := store.CreateUser("superpriv", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create superpriv: %v", err)
	}
	if err := store.SetUserSuperuser("superpriv", true); err != nil {
		t.Fatalf("Failed to set superuser: %v", err)
	}
	superUserID, _ := store.GetUserID("superpriv")

	// Create a regular user with specific grants.
	if err := store.CreateUser("regular", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create regular user: %v", err)
	}
	regularUserID, _ := store.GetUserID("regular")

	groupID, err := store.CreateGroup("priv_group", "Privileges test group")
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}
	if err := store.AddUserToGroup(groupID, regularUserID); err != nil {
		t.Fatalf("Failed to add user to group: %v", err)
	}
	if err := store.GrantAdminPermission(groupID, auth.PermManageUsers); err != nil {
		t.Fatalf("Failed to grant admin permission: %v", err)
	}
	if err := store.GrantConnectionPrivilege(groupID, 1, auth.AccessLevelReadWrite); err != nil {
		t.Fatalf("Failed to grant connection privilege: %v", err)
	}

	toolID, err := store.RegisterMCPPrivilege("priv_tool", "tool", "Privilege test tool")
	if err != nil {
		t.Fatalf("Failed to register MCP privilege: %v", err)
	}
	if err := store.GrantMCPPrivilege(groupID, toolID); err != nil {
		t.Fatalf("Failed to grant MCP privilege: %v", err)
	}

	// Create a caller with manage_users permission to access the endpoint.
	callerID := setupUserWithPermission(t, store, "caller", auth.PermManageUsers)

	// Create a user without manage_users for the 403 test.
	if err := store.CreateUser("nocall", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create nocall user: %v", err)
	}
	noCallUserID, _ := store.GetUserID("nocall")

	t.Run("SuperuserTarget_WildcardPrivileges", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/rbac/users/"+itoa(superUserID)+"/privileges", nil)
		req = withUser(req, callerID)
		rec := httptest.NewRecorder()
		handler.handleUserSubpath(rec, req)

		requireStatus(t, rec, http.StatusOK)

		var resp struct {
			IsSuperuser          bool              `json:"is_superuser"`
			MCPPrivileges        []string          `json:"mcp_privileges"`
			ConnectionPrivileges map[string]string `json:"connection_privileges"`
			AdminPermissions     []string          `json:"admin_permissions"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if !resp.IsSuperuser {
			t.Error("Expected is_superuser=true")
		}
		if len(resp.MCPPrivileges) != 1 || resp.MCPPrivileges[0] != "*" {
			t.Errorf("Expected mcp_privileges=[*], got %v", resp.MCPPrivileges)
		}
		if resp.ConnectionPrivileges["0"] != "read_write" {
			t.Errorf("Expected connection_privileges={0: read_write}, got %v", resp.ConnectionPrivileges)
		}
		if len(resp.AdminPermissions) != 1 || resp.AdminPermissions[0] != "*" {
			t.Errorf("Expected admin_permissions=[*], got %v", resp.AdminPermissions)
		}
	})

	t.Run("RegularUser_SpecificPrivileges", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/rbac/users/"+itoa(regularUserID)+"/privileges", nil)
		req = withUser(req, callerID)
		rec := httptest.NewRecorder()
		handler.handleUserSubpath(rec, req)

		requireStatus(t, rec, http.StatusOK)

		var resp struct {
			IsSuperuser          bool              `json:"is_superuser"`
			MCPPrivileges        []string          `json:"mcp_privileges"`
			ConnectionPrivileges map[string]string `json:"connection_privileges"`
			AdminPermissions     []string          `json:"admin_permissions"`
			Groups               []string          `json:"groups"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp.IsSuperuser {
			t.Error("Regular user should not be superuser")
		}

		// Verify MCP privileges include priv_tool.
		foundTool := false
		for _, p := range resp.MCPPrivileges {
			if p == "priv_tool" {
				foundTool = true
			}
		}
		if !foundTool {
			t.Errorf("Expected priv_tool in mcp_privileges, got %v", resp.MCPPrivileges)
		}

		// Verify connection privileges.
		if resp.ConnectionPrivileges["1"] != "read_write" {
			t.Errorf("Expected connection 1 with read_write, got %v", resp.ConnectionPrivileges)
		}

		// Verify admin permissions.
		foundManageUsers := false
		for _, p := range resp.AdminPermissions {
			if p == auth.PermManageUsers {
				foundManageUsers = true
			}
		}
		if !foundManageUsers {
			t.Errorf("Expected manage_users in admin_permissions, got %v", resp.AdminPermissions)
		}

		// Verify groups.
		foundGroup := false
		for _, g := range resp.Groups {
			if g == "priv_group" {
				foundGroup = true
			}
		}
		if !foundGroup {
			t.Errorf("Expected priv_group in groups, got %v", resp.Groups)
		}
	})

	t.Run("CallerWithoutPermission_403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/rbac/users/"+itoa(regularUserID)+"/privileges", nil)
		req = withUser(req, noCallUserID)
		rec := httptest.NewRecorder()
		handler.handleUserSubpath(rec, req)

		requireStatus(t, rec, http.StatusForbidden)
	})

	t.Run("NonExistentUser_404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/rbac/users/99999/privileges", nil)
		req = withUser(req, callerID)
		rec := httptest.NewRecorder()
		handler.handleUserSubpath(rec, req)

		requireStatus(t, rec, http.StatusNotFound)
	})
}
