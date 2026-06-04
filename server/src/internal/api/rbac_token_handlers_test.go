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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Token Creation Tests
// =============================================================================

func TestRBACHandler_CreateToken_Valid(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// Setup admin with manage_token_scopes permission
	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	// Create a target user for the token
	store.CreateUser("tokenowner", "Password1234", "Token Owner", "", "")

	body, _ := json.Marshal(map[string]string{
		"owner_username": "tokenowner",
		"annotation":     "Test token",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/tokens",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokens(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusCreated, rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if _, ok := resp["token"]; !ok {
		t.Error("Expected 'token' in response")
	}
	if _, ok := resp["id"]; !ok {
		t.Error("Expected 'id' in response")
	}
	if resp["owner"] != "tokenowner" {
		t.Errorf("Expected owner 'tokenowner', got %v", resp["owner"])
	}
	if resp["message"] == nil {
		t.Error("Expected 'message' in response")
	}
}

func TestRBACHandler_CreateToken_WithExpiry(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	store.CreateUser("tokenowner", "Password1234", "Token Owner", "", "")

	body, _ := json.Marshal(map[string]string{
		"owner_username": "tokenowner",
		"annotation":     "Expiring token",
		"expires_in":     "30d",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/tokens",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokens(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusCreated, rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["expires_at"] == nil {
		t.Error("Expected 'expires_at' to be set for token with expiry")
	}
}

func TestRBACHandler_CreateToken_NeverExpiry(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	store.CreateUser("tokenowner", "Password1234", "Token Owner", "", "")

	body, _ := json.Marshal(map[string]string{
		"owner_username": "tokenowner",
		"annotation":     "Never-expiring token",
		"expires_in":     "never",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/tokens",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokens(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusCreated, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_CreateToken_MissingOwner(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	body, _ := json.Marshal(map[string]string{
		"annotation": "Token without owner",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/tokens",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokens(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Owner username is required" {
		t.Errorf("Expected 'Owner username is required', got %q",
			response.Error)
	}
}

func TestRBACHandler_CreateToken_InvalidExpiry(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	store.CreateUser("tokenowner", "Password1234", "Token Owner", "", "")

	body, _ := json.Marshal(map[string]string{
		"owner_username": "tokenowner",
		"annotation":     "Bad expiry token",
		"expires_in":     "invalid",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/tokens",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokens(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_CreateToken_PermissionDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// Create user without manage_token_scopes permission
	store.CreateUser("normie", "Password1234", "Normal user", "", "")
	userID, _ := store.GetUserID("normie")

	body, _ := json.Marshal(map[string]string{
		"owner_username": "normie",
		"annotation":     "Unauthorized token",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/tokens",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleTokens(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_CreateToken_InvalidBody(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/tokens",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokens(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

// postToken posts a token-create request for the given owner and annotation
// as the supplied admin user; it does not perform any user or group setup,
// which is the caller's responsibility (see tokenAdminSetup). It returns the
// recorder so the caller can assert on status and body.
func postToken(t *testing.T, handler *RBACHandler, store *auth.AuthStore,
	adminID int64, owner, annotation string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"owner_username": owner,
		"annotation":     annotation,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/tokens",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()
	handler.handleTokens(rec, req)
	return rec
}

func tokenAdminSetup(t *testing.T) (*RBACHandler, *auth.AuthStore, int64, func()) {
	t.Helper()
	handler, store, cleanup := createTestRBACHandler(t)
	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)
	store.CreateUser("tokenowner", "Password1234", "Token Owner", "", "")
	return handler, store, adminID, cleanup
}

func TestRBACHandler_CreateToken_InvalidCharacters(t *testing.T) {
	handler, store, adminID, cleanup := tokenAdminSetup(t)
	defer cleanup()

	rec := postToken(t, handler, store, adminID, "tokenowner", "bad/name!")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	want := "Name may only contain letters, numbers, spaces, and the characters . _ - ( )"
	if resp.Error != want {
		t.Errorf("Expected %q, got %q", want, resp.Error)
	}
}

func TestRBACHandler_CreateToken_TooLong(t *testing.T) {
	handler, store, adminID, cleanup := tokenAdminSetup(t)
	defer cleanup()

	long := ""
	for i := 0; i < 256; i++ {
		long += "a"
	}
	rec := postToken(t, handler, store, adminID, "tokenowner", long)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp.Error != "Name must be 255 characters or fewer" {
		t.Errorf("Expected length message, got %q", resp.Error)
	}
}

// TestRBACHandler_CreateToken_EmptyNameAllowed confirms that an empty or
// whitespace-only annotation is permitted; an unnamed token is valid, so the
// handler must not reject it. Validation runs only when an annotation is
// supplied.
func TestRBACHandler_CreateToken_EmptyNameAllowed(t *testing.T) {
	handler, store, adminID, cleanup := tokenAdminSetup(t)
	defer cleanup()

	rec := postToken(t, handler, store, adminID, "tokenowner", "   ")

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusCreated, rec.Code, rec.Body.String())
	}
}

// TestRBACHandler_CreateToken_DuplicateNamesAllowed confirms that, unlike
// group names, token names (stored in the annotation field) are not
// unique; creating two tokens with the same name must both succeed.
func TestRBACHandler_CreateToken_DuplicateNamesAllowed(t *testing.T) {
	handler, store, adminID, cleanup := tokenAdminSetup(t)
	defer cleanup()

	first := postToken(t, handler, store, adminID, "tokenowner", "Shared Name")
	if first.Code != http.StatusCreated {
		t.Fatalf("First token: expected %d, got %d. Body: %s",
			http.StatusCreated, first.Code, first.Body.String())
	}

	second := postToken(t, handler, store, adminID, "tokenowner", "Shared Name")
	if second.Code != http.StatusCreated {
		t.Fatalf("Second token with duplicate name: expected %d, got %d. Body: %s",
			http.StatusCreated, second.Code, second.Body.String())
	}
}

// TestRBACHandler_CreateToken_UnknownOwner exercises the generic error
// path: a syntactically valid token name with an owner that does not exist
// makes CreateToken fail, which the handler maps to a 500.
func TestRBACHandler_CreateToken_UnknownOwner(t *testing.T) {
	handler, store, adminID, cleanup := tokenAdminSetup(t)
	defer cleanup()

	rec := postToken(t, handler, store, adminID, "no-such-user", "Valid Name")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusInternalServerError, rec.Code, rec.Body.String())
	}
}

// =============================================================================
// Token Listing Tests
// =============================================================================

func TestRBACHandler_ListTokens_Admin(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// Setup admin with manage_token_scopes permission
	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	// Create tokens for listing
	store.CreateUser("user1", "Password1234", "User One", "", "")
	store.CreateToken("admin", "Admin token", nil)
	store.CreateToken("user1", "User1 token", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/tokens", nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokens(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp struct {
		Tokens []map[string]any `json:"tokens"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Tokens) < 2 {
		t.Errorf("Expected at least 2 tokens, got %d", len(resp.Tokens))
	}

	// Verify token fields are present
	for _, tok := range resp.Tokens {
		if _, ok := tok["id"]; !ok {
			t.Error("Expected 'id' field in token response")
		}
		if _, ok := tok["name"]; !ok {
			t.Error("Expected 'name' field in token response")
		}
		if _, ok := tok["token_prefix"]; !ok {
			t.Error("Expected 'token_prefix' field in token response")
		}
		if _, ok := tok["user_id"]; !ok {
			t.Error("Expected 'user_id' field in token response")
		}
	}
}

func TestRBACHandler_ListTokens_NormalUser_PermissionDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// Create user without manage_token_scopes permission
	store.CreateUser("normie", "Password1234", "Normal user", "", "")
	userID, _ := store.GetUserID("normie")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/tokens", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleTokens(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_ListTokens_Superuser(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("user1", "Password1234", "User One", "", "")
	store.CreateToken("user1", "Token 1", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/tokens", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleTokens(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_ListTokens_IncludesUsername(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	store.CreateToken("admin", "Admin token", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/tokens", nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokens(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp struct {
		Tokens []map[string]any `json:"tokens"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Tokens) < 1 {
		t.Fatal("Expected at least 1 token")
	}

	found := false
	for _, tok := range resp.Tokens {
		if tok["username"] == "admin" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected token listing to include 'username' field with value 'admin'")
	}
}

// =============================================================================
// Token Deletion Tests
// =============================================================================

func TestRBACHandler_DeleteToken_Valid(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	store.CreateUser("user1", "Password1234", "User One", "", "")
	_, storedToken, _ := store.CreateToken("user1", "Deletable token", nil)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/tokens/"+itoa(storedToken.ID), nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_DeleteToken_NonExistent(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/tokens/99999", nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusInternalServerError, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_DeleteToken_PermissionDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("normie", "Password1234", "Normal user", "", "")
	userID, _ := store.GetUserID("normie")
	_, storedToken, _ := store.CreateToken("normie", "My token", nil)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/tokens/"+itoa(storedToken.ID), nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_DeleteToken_AdminCanDeleteOtherUsersToken(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// Setup admin with manage_token_scopes permission
	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	// Create another user's token
	store.CreateUser("other", "Password1234", "Other user", "", "")
	_, otherToken, _ := store.CreateToken("other", "Other's token", nil)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/tokens/"+itoa(otherToken.ID), nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}
}

// =============================================================================
// Token Scope Tests
// =============================================================================

func TestRBACHandler_GetTokenScope_NoScope(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	_, storedToken, _ := store.CreateToken("admin", "Unscoped token", nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/tokens/"+itoa(storedToken.ID)+"/scope", nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	scoped, ok := resp["scoped"].(bool)
	if !ok || scoped {
		t.Error("Expected 'scoped' to be false for unscoped token")
	}
}

func TestRBACHandler_SetTokenScope_Connections(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	_, storedToken, _ := store.CreateToken("admin", "Scoped token", nil)

	body, _ := json.Marshal(map[string]any{
		"connections": []map[string]any{
			{"connection_id": 1, "access_level": "read"},
			{"connection_id": 2, "access_level": "read_write"},
		},
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/tokens/"+itoa(storedToken.ID)+"/scope",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}

	// Verify scope was set by getting it back
	req = httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/tokens/"+itoa(storedToken.ID)+"/scope", nil)
	req = withUser(req, adminID)
	rec = httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	scoped, ok := resp["scoped"].(bool)
	if !ok || !scoped {
		t.Error("Expected 'scoped' to be true after setting scope")
	}

	conns, ok := resp["connections"].([]any)
	if !ok {
		t.Fatal("Expected 'connections' array in response")
	}
	if len(conns) != 2 {
		t.Errorf("Expected 2 connections in scope, got %d", len(conns))
	}
}

func TestRBACHandler_SetTokenScope_AdminPermissions(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	_, storedToken, _ := store.CreateToken("admin", "Admin scoped token", nil)

	body, _ := json.Marshal(map[string]any{
		"admin_permissions": []string{
			auth.PermManageUsers,
			auth.PermManageConnections,
		},
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/tokens/"+itoa(storedToken.ID)+"/scope",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}

	// Verify the admin permissions scope was set
	req = httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/tokens/"+itoa(storedToken.ID)+"/scope", nil)
	req = withUser(req, adminID)
	rec = httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	scoped, ok := resp["scoped"].(bool)
	if !ok || !scoped {
		t.Error("Expected 'scoped' to be true after setting admin permissions scope")
	}

	perms, ok := resp["admin_permissions"].([]any)
	if !ok {
		t.Fatal("Expected 'admin_permissions' array in response")
	}
	if len(perms) != 2 {
		t.Errorf("Expected 2 admin permissions in scope, got %d", len(perms))
	}
}

func TestRBACHandler_ClearTokenScope(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	_, storedToken, _ := store.CreateToken("admin", "Scoped token", nil)

	// Set a scope first
	store.SetTokenConnectionScope(storedToken.ID, []auth.ScopedConnection{
		{ConnectionID: 1, AccessLevel: "read"},
	})

	// Clear the scope
	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/tokens/"+itoa(storedToken.ID)+"/scope", nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}

	// Verify scope is cleared
	req = httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/tokens/"+itoa(storedToken.ID)+"/scope", nil)
	req = withUser(req, adminID)
	rec = httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	scoped, ok := resp["scoped"].(bool)
	if !ok || scoped {
		t.Error("Expected 'scoped' to be false after clearing scope")
	}
}

func TestRBACHandler_GetTokenScope_PermissionDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("normie", "Password1234", "Normal user", "", "")
	userID, _ := store.GetUserID("normie")
	_, storedToken, _ := store.CreateToken("normie", "My token", nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/tokens/"+itoa(storedToken.ID)+"/scope", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_SetTokenScope_PermissionDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("normie", "Password1234", "Normal user", "", "")
	userID, _ := store.GetUserID("normie")
	_, storedToken, _ := store.CreateToken("normie", "My token", nil)

	body, _ := json.Marshal(map[string]any{
		"connections": []map[string]any{
			{"connection_id": 1, "access_level": "read"},
		},
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/tokens/"+itoa(storedToken.ID)+"/scope",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_SetTokenScope_InvalidBody(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageTokenScopes)

	_, storedToken, _ := store.CreateToken("admin", "Token", nil)

	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/tokens/"+itoa(storedToken.ID)+"/scope",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

// =============================================================================
// Token Expiry Parsing Tests
// =============================================================================

func TestParseTokenExpiry(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{"valid hours", "24h", false},
		{"valid days", "30d", false},
		{"valid weeks", "4w", false},
		{"valid months", "6m", false},
		{"valid years", "1y", false},
		{"too short", "h", true},
		{"invalid number", "abch", true},
		{"negative value", "-1d", true},
		{"zero value", "0d", true},
		{"invalid unit", "30x", true},
		{"exceeds max days", "3651d", true},
		{"exceeds max hours", "87601h", true},
		{"exceeds max years", "11y", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseTokenExpiry(tt.input)
			if tt.expectErr && err == nil {
				t.Errorf("Expected error for input %q, got nil", tt.input)
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error for input %q: %v", tt.input, err)
			}
		})
	}
}
