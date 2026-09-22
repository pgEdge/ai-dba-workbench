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
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// User Listing Tests
// =============================================================================

func TestRBACHandler_ListUsers_WithAdmin(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// Create admin with manage_users permission
	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	// Create additional users
	store.CreateUser("user2", "Password1234", "User Two", "user2@example.com", "")
	store.CreateUser("user3", "Password1234", "User Three", "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should have at least the 3 users we created
	if len(resp.Users) < 3 {
		t.Errorf("Expected at least 3 users, got %d", len(resp.Users))
	}

	// Verify user fields are present and password hash is not exposed
	for _, u := range resp.Users {
		if _, ok := u["id"]; !ok {
			t.Error("Expected 'id' field in user response")
		}
		if _, ok := u["username"]; !ok {
			t.Error("Expected 'username' field in user response")
		}
		if _, ok := u["password_hash"]; ok {
			t.Error("Password hash should not be in user response")
		}
		if _, ok := u["password"]; ok {
			t.Error("Password should not be in user response")
		}
	}
}

func TestRBACHandler_ListUsers_Superuser(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("user1", "Password1234", "User One", "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_ListUsers_NormalUser_PermissionDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// Create user without manage_users permission
	store.CreateUser("normie", "Password1234", "Normal", "", "")
	userID, _ := store.GetUserID("normie")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

// =============================================================================
// User Creation Tests
// =============================================================================

func TestRBACHandler_CreateUser_Valid(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	body, _ := json.Marshal(map[string]string{
		"username":     "newuser",
		"password":     "Securepassword1",
		"display_name": "New User",
		"email":        "new@example.com",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusCreated, rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["message"] != "User created" {
		t.Errorf("Expected message 'User created', got %q", resp["message"])
	}

	// Verify user was actually created
	newUserID, err := store.GetUserID("newuser")
	if err != nil {
		t.Fatalf("Expected user to exist, got error: %v", err)
	}
	if newUserID == 0 {
		t.Error("Expected non-zero user ID for created user")
	}
}

func TestRBACHandler_CreateUser_MissingUsername_Handler(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	body := `{"password": "Secret123456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Username is required" {
		t.Errorf("Expected 'Username is required', got %q", response.Error)
	}
}

func TestRBACHandler_CreateUser_MissingPassword_Handler(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	body := `{"username": "newuser"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Password is required" {
		t.Errorf("Expected 'Password is required', got %q", response.Error)
	}
}

func TestRBACHandler_CreateUser_ShortPassword(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	body, _ := json.Marshal(map[string]string{
		"username": "newuser",
		"password": "short",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expectedPrefix := "Password does not meet length requirements:"
	if !strings.Contains(response.Error, expectedPrefix) {
		t.Errorf("Expected error containing %q, got %q", expectedPrefix, response.Error)
	}
}

func TestRBACHandler_CreateUser_DuplicateUsername(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	// Create a user first
	store.CreateUser("existing", "Password1234", "Existing User", "", "")

	// Try to create a user with the same username
	body, _ := json.Marshal(map[string]string{
		"username": "existing",
		"password": "Password1234",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusConflict, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if response.Error != "Username already taken" {
		t.Errorf("Expected 'Username already taken', got %q", response.Error)
	}
}

func TestRBACHandler_CreateUser_PermissionDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// Create user without manage_users permission
	store.CreateUser("normie", "Password1234", "Normal", "", "")
	userID, _ := store.GetUserID("normie")

	body, _ := json.Marshal(map[string]string{
		"username": "newuser",
		"password": "Securepassword1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_CreateUser_InvalidBody(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_CreateUser_WithDisabledFlag(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	enabled := false
	body, _ := json.Marshal(map[string]any{
		"username": "disableduser",
		"password": "Securepassword1",
		"enabled":  enabled,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusCreated, rec.Code, rec.Body.String())
	}

	// Verify user was created and is disabled
	newUserID, _ := store.GetUserID("disableduser")
	user, err := store.GetUserByID(newUserID)
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}
	if user.Enabled {
		t.Error("Expected user to be disabled")
	}
}

// =============================================================================
// Service Account Creation Tests
// =============================================================================

func TestRBACHandler_CreateServiceAccount(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	isServiceAccount := true
	body, _ := json.Marshal(map[string]any{
		"username":           "svc-account",
		"annotation":         "CI/CD service account",
		"is_service_account": isServiceAccount,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusCreated, rec.Code, rec.Body.String())
	}

	// Verify the service account was created
	svcID, err := store.GetUserID("svc-account")
	if err != nil {
		t.Fatalf("Expected service account to exist, got error: %v", err)
	}
	svcUser, err := store.GetUserByID(svcID)
	if err != nil {
		t.Fatalf("Failed to get service account: %v", err)
	}
	if !svcUser.IsServiceAccount {
		t.Error("Expected user to be a service account")
	}
}

func TestRBACHandler_CreateServiceAccount_NoPasswordRequired(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	// Service account should not require a password
	isServiceAccount := true
	body, _ := json.Marshal(map[string]any{
		"username":           "svc-nopw",
		"is_service_account": isServiceAccount,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusCreated, rec.Code, rec.Body.String())
	}
}

// adminRBACHandler builds an RBAC handler with an admin user that holds the
// manage_users permission. It returns the handler, the store, the admin's
// user ID, and a cleanup func, removing the boilerplate repeated across the
// validation tests below.
func adminRBACHandler(t *testing.T) (*RBACHandler, *auth.AuthStore, int64, func()) {
	t.Helper()
	handler, store, cleanup := createTestRBACHandler(t)
	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)
	return handler, store, adminID, cleanup
}

// postUser issues a POST /api/v1/rbac/users request as the supplied user and
// returns the recorder for assertions.
func postUser(handler *RBACHandler, adminID int64, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()
	handler.handleUsers(rec, req)
	return rec
}

func TestRBACHandler_CreateUser_InvalidUsername(t *testing.T) {
	handler, _, adminID, cleanup := adminRBACHandler(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]string{
		"username": "<>!@#$%",
		"password": "Securepassword1",
	})
	rec := postUser(handler, adminID, body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	// The handler surfaces auth.ValidateUsername's message, capitalised.
	expected := capitalizeFirst(auth.ValidateUsername("<>!@#$%").Error())
	if response.Error != expected {
		t.Errorf("Expected %q, got %q", expected, response.Error)
	}
}

func TestRBACHandler_CreateUser_InvalidEmail_NoAt(t *testing.T) {
	handler, _, adminID, cleanup := adminRBACHandler(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]string{
		"username": "newuser",
		"password": "Securepassword1",
		"email":    "notanemail",
	})
	rec := postUser(handler, adminID, body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if response.Error != "Please enter a valid email address" {
		t.Errorf("Expected 'Please enter a valid email address', got %q", response.Error)
	}
}

func TestRBACHandler_CreateUser_InvalidEmail_NoDomainDot(t *testing.T) {
	handler, _, adminID, cleanup := adminRBACHandler(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]string{
		"username": "newuser",
		"password": "Securepassword1",
		"email":    "test@",
	})
	rec := postUser(handler, adminID, body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if response.Error != "Please enter a valid email address" {
		t.Errorf("Expected 'Please enter a valid email address', got %q", response.Error)
	}
}

func TestRBACHandler_CreateServiceAccount_InvalidUsername(t *testing.T) {
	handler, _, adminID, cleanup := adminRBACHandler(t)
	defer cleanup()

	isServiceAccount := true
	body, _ := json.Marshal(map[string]any{
		"username":           "bad name!",
		"is_service_account": isServiceAccount,
	})
	rec := postUser(handler, adminID, body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_CreateServiceAccount_InvalidEmail(t *testing.T) {
	handler, _, adminID, cleanup := adminRBACHandler(t)
	defer cleanup()

	isServiceAccount := true
	body, _ := json.Marshal(map[string]any{
		"username":           "svc-bademail",
		"email":              "notanemail",
		"is_service_account": isServiceAccount,
	})
	rec := postUser(handler, adminID, body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if response.Error != "Please enter a valid email address" {
		t.Errorf("Expected 'Please enter a valid email address', got %q", response.Error)
	}
}

func TestRBACHandler_CreateServiceAccount_DuplicateUsername(t *testing.T) {
	handler, store, adminID, cleanup := adminRBACHandler(t)
	defer cleanup()

	store.CreateServiceAccount("svc-dup", "first", "", "")

	isServiceAccount := true
	body, _ := json.Marshal(map[string]any{
		"username":           "svc-dup",
		"is_service_account": isServiceAccount,
	})
	rec := postUser(handler, adminID, body)

	if rec.Code != http.StatusConflict {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusConflict, rec.Code, rec.Body.String())
	}
	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if response.Error != "Username already taken" {
		t.Errorf("Expected 'Username already taken', got %q", response.Error)
	}
}

// =============================================================================
// User Update Tests
// =============================================================================

// TestRBACHandler_UpdateUser_FederatedPasswordRejected covers the case
// with a safety consequence rather than merely an ugly error. This
// endpoint applies the password, enabled and superuser changes as one
// transaction, so an administrator disabling a federated user whilst
// also filling in the password field would have the whole update rolled
// back by the store's password guard, and could reasonably read the
// generic failure as the account having been disabled. The request is
// refused up front instead, with an explanation and with nothing
// written.
func TestRBACHandler_UpdateUser_FederatedPasswordRejected(t *testing.T) {
	handler, store, adminID, cleanup := adminRBACHandler(t)
	defer cleanup()

	if err := store.CreateUser("federated", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("federated", "https://idp.example.com", "subject-1", false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	targetID, _ := store.GetUserID("federated")

	stillEnabled := false
	body, _ := json.Marshal(map[string]any{
		"password": "An0ther-Str0ng-Pass!",
		"enabled":  stillEnabled,
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/"+strconv.FormatInt(targetID, 10),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.updateUser(rec, req, targetID)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if !strings.Contains(response.Error, "identity provider") {
		t.Errorf("Expected the refusal to explain why, got %q", response.Error)
	}

	// The administrator must be able to see that nothing was applied,
	// rather than being left to assume the account is now disabled.
	user, err := store.GetUserByID(targetID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if !user.Enabled {
		t.Error("the refused request disabled the account anyway")
	}
}

// TestRBACHandler_UpdateUser_FederatedWithoutPasswordSucceeds checks the
// refusal is narrow: everything except the password may still be changed
// on a federated account, which is the route the administrator above is
// told to take.
func TestRBACHandler_UpdateUser_FederatedWithoutPasswordSucceeds(t *testing.T) {
	handler, store, adminID, cleanup := adminRBACHandler(t)
	defer cleanup()

	if err := store.CreateUser("federated-ok", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("federated-ok", "https://idp.example.com", "subject-2", false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	targetID, _ := store.GetUserID("federated-ok")

	body, _ := json.Marshal(map[string]any{"enabled": false})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/"+strconv.FormatInt(targetID, 10),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.updateUser(rec, req, targetID)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	user, err := store.GetUserByID(targetID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if user.Enabled {
		t.Error("expected the account to have been disabled")
	}
}

func TestRBACHandler_UpdateUser_InvalidEmail(t *testing.T) {
	handler, store, adminID, cleanup := adminRBACHandler(t)
	defer cleanup()

	store.CreateUser("target", "Password1234", "Target user", "old@example.com", "")
	targetID, _ := store.GetUserID("target")

	body, _ := json.Marshal(map[string]string{
		"email": "notanemail",
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/"+strconv.FormatInt(targetID, 10),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.updateUser(rec, req, targetID)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if response.Error != "Please enter a valid email address" {
		t.Errorf("Expected 'Please enter a valid email address', got %q", response.Error)
	}
}

func TestRBACHandler_UpdateUser_PasswordChange(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "Oldpassword1", "Target user", "", "")
	targetID, _ := store.GetUserID("target")

	newPassword := "Newpassword123"
	body, _ := json.Marshal(map[string]any{
		"password": newPassword,
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/"+itoa(targetID),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.updateUser(rec, req, targetID)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["message"] != "User updated" {
		t.Errorf("Expected message 'User updated', got %q", resp["message"])
	}
}

func TestRBACHandler_UpdateUser_ShortPassword(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "Password1234", "Target user", "", "")
	targetID, _ := store.GetUserID("target")

	shortPw := "short"
	body, _ := json.Marshal(map[string]any{
		"password": shortPw,
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/"+itoa(targetID),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.updateUser(rec, req, targetID)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expectedPrefix := "Password does not meet length requirements:"
	if !strings.Contains(response.Error, expectedPrefix) {
		t.Errorf("Expected error containing %q, got %q", expectedPrefix, response.Error)
	}
}

func TestRBACHandler_UpdateUser_EnableDisable(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "Password1234", "Target user", "", "")
	targetID, _ := store.GetUserID("target")

	// Disable the user
	disabled := false
	body, _ := json.Marshal(map[string]any{
		"enabled": disabled,
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/"+itoa(targetID),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.updateUser(rec, req, targetID)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	// Verify user is disabled
	user, _ := store.GetUserByID(targetID)
	if user.Enabled {
		t.Error("Expected user to be disabled after update")
	}

	// Re-enable the user
	enabled := true
	body, _ = json.Marshal(map[string]any{
		"enabled": enabled,
	})
	req = httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/"+itoa(targetID),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec = httptest.NewRecorder()

	handler.updateUser(rec, req, targetID)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	// Verify user is enabled
	user, _ = store.GetUserByID(targetID)
	if !user.Enabled {
		t.Error("Expected user to be enabled after update")
	}
}

func TestRBACHandler_UpdateUser_DisplayNameAndEmail(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "Password1234", "Old Name", "old@example.com", "")
	targetID, _ := store.GetUserID("target")

	newName := "New Name"
	newEmail := "new@example.com"
	body, _ := json.Marshal(map[string]any{
		"display_name": newName,
		"email":        newEmail,
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/"+itoa(targetID),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.updateUser(rec, req, targetID)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	// Verify the updates
	user, _ := store.GetUserByID(targetID)
	if user.DisplayName != newName {
		t.Errorf("Expected display_name %q, got %q", newName, user.DisplayName)
	}
	if user.Email != newEmail {
		t.Errorf("Expected email %q, got %q", newEmail, user.Email)
	}
}

func TestRBACHandler_UpdateUser_NotFound(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	newName := "Updated Name"
	body, _ := json.Marshal(map[string]any{
		"display_name": newName,
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/99999",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.updateUser(rec, req, 99999)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNotFound, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "User not found" {
		t.Errorf("Expected 'User not found', got %q", response.Error)
	}
}

func TestRBACHandler_UpdateUser_PermissionDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("normie", "Password1234", "Normal", "", "")
	userID, _ := store.GetUserID("normie")

	store.CreateUser("target", "Password1234", "Target", "", "")
	targetID, _ := store.GetUserID("target")

	newName := "Hacked Name"
	body, _ := json.Marshal(map[string]any{
		"display_name": newName,
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/"+itoa(targetID),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.updateUser(rec, req, targetID)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_UpdateUser_InvalidBody(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "Password1234", "Target", "", "")
	targetID, _ := store.GetUserID("target")

	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/"+itoa(targetID),
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.updateUser(rec, req, targetID)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

// =============================================================================
// User Deletion Tests
// =============================================================================

func TestRBACHandler_DeleteUser_Valid(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "Password1234", "Target user", "", "")
	targetID, _ := store.GetUserID("target")

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/users/"+itoa(targetID), nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.deleteUser(rec, req, targetID)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}

	// Verify user was deleted
	user, _ := store.GetUserByID(targetID)
	if user != nil {
		t.Error("Expected user to be deleted")
	}
}

func TestRBACHandler_DeleteUser_SelfDeletion(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	// Attempt to delete self; the current handler allows this operation
	// and returns 204 No Content (self-deletion is not rejected at the
	// handler level)
	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/users/"+itoa(adminID), nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.deleteUser(rec, req, adminID)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_DeleteUser_NonExistent(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/users/99999", nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.deleteUser(rec, req, 99999)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNotFound, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "User not found" {
		t.Errorf("Expected 'User not found', got %q", response.Error)
	}
}

func TestRBACHandler_DeleteUser_PermissionDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("normie", "Password1234", "Normal", "", "")
	userID, _ := store.GetUserID("normie")

	store.CreateUser("target", "Password1234", "Target", "", "")
	targetID, _ := store.GetUserID("target")

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/users/"+itoa(targetID), nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.deleteUser(rec, req, targetID)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

// =============================================================================
// User Subpath Routing Tests (via handleUserSubpath)
// =============================================================================

func TestRBACHandler_UserSubpath_PutRoutesToUpdate(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "Password1234", "Target", "", "")
	targetID, _ := store.GetUserID("target")

	newName := "Updated Name"
	body, _ := json.Marshal(map[string]any{
		"display_name": newName,
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/"+itoa(targetID),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUserSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_UserSubpath_DeleteRoutesToDelete(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "Password1234", "Target", "", "")
	targetID, _ := store.GetUserID("target")

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/users/"+itoa(targetID), nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUserSubpath(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}
}

// =============================================================================
// User Privileges Tests
// =============================================================================

func TestRBACHandler_GetUserPrivileges(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)
	store.GrantAdminPermission(gID, auth.PermManageConnections)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/users/"+itoa(adminID)+"/privileges", nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.getUserPrivileges(rec, req, adminID)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["username"] != "admin" {
		t.Errorf("Expected username 'admin', got %v", resp["username"])
	}

	groups, ok := resp["groups"].([]any)
	if !ok {
		t.Fatal("Expected 'groups' array in response")
	}
	if len(groups) < 1 {
		t.Error("Expected at least 1 group for admin user")
	}
}

func TestRBACHandler_GetUserPrivileges_NotFound(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("admin", "Password1234", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/users/99999/privileges", nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.getUserPrivileges(rec, req, 99999)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_GetUserPrivileges_PermissionDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.CreateUser("normie", "Password1234", "Normal", "", "")
	userID, _ := store.GetUserID("normie")

	store.CreateUser("target", "Password1234", "Target", "", "")
	targetID, _ := store.GetUserID("target")

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/users/"+itoa(targetID)+"/privileges", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.getUserPrivileges(rec, req, targetID)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

// =============================================================================
// Authentication Source Tests
// =============================================================================

// TestDescribeAuthSource covers the mapping from a stored row to the pair of
// fields every user object carries, including the two rows a handler test
// cannot easily produce: one with no auth_source at all, and a federated one
// whose stored identity does not parse.
func TestDescribeAuthSource(t *testing.T) {
	tests := []struct {
		name       string
		user       auth.StoredUser
		wantSource string
		wantIssuer string
	}{
		{
			name:       "local account",
			user:       auth.StoredUser{AuthSource: auth.AuthSourceLocal},
			wantSource: auth.AuthSourceLocal,
		},
		{
			name:       "empty auth source reports as local",
			user:       auth.StoredUser{},
			wantSource: auth.AuthSourceLocal,
		},
		{
			name: "a local row never reports an issuer",
			user: auth.StoredUser{
				AuthSource:      auth.AuthSourceLocal,
				ExternalSubject: auth.ExternalSubjectKey("https://idp.example.com", "subject-1"),
			},
			wantSource: auth.AuthSourceLocal,
		},
		{
			name: "federated account reports its issuer",
			user: auth.StoredUser{
				AuthSource:      auth.AuthSourceOIDC,
				ExternalSubject: auth.ExternalSubjectKey("https://idp.example.com", "subject-1"),
			},
			wantSource: auth.AuthSourceOIDC,
			wantIssuer: "https://idp.example.com",
		},
		{
			name: "unparsable identity yields no issuer",
			user: auth.StoredUser{
				AuthSource:      auth.AuthSourceOIDC,
				ExternalSubject: "nonsense",
			},
			wantSource: auth.AuthSourceOIDC,
		},
		{
			name: "missing identity yields no issuer",
			user: auth.StoredUser{
				AuthSource: auth.AuthSourceOIDC,
			},
			wantSource: auth.AuthSourceOIDC,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := tt.user
			source, issuer := describeAuthSource(&user)
			if source != tt.wantSource || issuer != tt.wantIssuer {
				t.Fatalf("describeAuthSource = (%q, %q), want (%q, %q)",
					source, issuer, tt.wantSource, tt.wantIssuer)
			}
		})
	}
}

// findUserInList picks one user out of a listUsers response body.
func findUserInList(t *testing.T, rec *httptest.ResponseRecorder, username string) map[string]any {
	t.Helper()

	var resp struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	for _, u := range resp.Users {
		if u["username"] == username {
			return u
		}
	}
	t.Fatalf("user %q is not in the listing", username)
	return nil
}

// TestRBACHandler_ListUsers_ReportsAuthSource checks an administrator can tell
// a federated account from a local one, and that the listing names the issuer
// without ever naming the provider subject.
func TestRBACHandler_ListUsers_ReportsAuthSource(t *testing.T) {
	handler, store, adminID, cleanup := adminRBACHandler(t)
	defer cleanup()

	const issuer = "https://idp.example.com"
	const subject = "subject-483"

	if err := store.CreateUser("localuser", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.CreateUser("feduser", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("feduser", issuer, subject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.listUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	rec.Body = bytes.NewBufferString(body)

	local := findUserInList(t, rec, "localuser")
	if local["auth_source"] != auth.AuthSourceLocal {
		t.Errorf("local auth_source = %v, want %q", local["auth_source"], auth.AuthSourceLocal)
	}
	if _, ok := local["auth_issuer"]; ok {
		t.Errorf("local account carries an auth_issuer: %v", local["auth_issuer"])
	}

	rec.Body = bytes.NewBufferString(body)
	federated := findUserInList(t, rec, "feduser")
	if federated["auth_source"] != auth.AuthSourceOIDC {
		t.Errorf("federated auth_source = %v, want %q", federated["auth_source"], auth.AuthSourceOIDC)
	}
	if federated["auth_issuer"] != issuer {
		t.Errorf("federated auth_issuer = %v, want %q", federated["auth_issuer"], issuer)
	}

	// The provider subject is an identifier the response has no business
	// carrying, in either half of the stored key.
	if strings.Contains(body, subject) {
		t.Errorf("the listing leaked the provider subject: %s", body)
	}
}

// TestRBACHandler_GetUserPrivileges_ReportsAuthSource checks the privileges
// view names the identity provider too, since a superuser flag that keeps
// reverting is usually the provider reconciling the account.
func TestRBACHandler_GetUserPrivileges_ReportsAuthSource(t *testing.T) {
	handler, store, adminID, cleanup := adminRBACHandler(t)
	defer cleanup()

	const issuer = "https://idp.example.com"
	const subject = "subject-privileges"

	if err := store.CreateUser("fedpriv", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("fedpriv", issuer, subject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	targetID, _ := store.GetUserID("fedpriv")

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/users/"+strconv.FormatInt(targetID, 10)+"/privileges", nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.getUserPrivileges(rec, req, targetID)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["auth_source"] != auth.AuthSourceOIDC {
		t.Errorf("auth_source = %v, want %q", resp["auth_source"], auth.AuthSourceOIDC)
	}
	if resp["auth_issuer"] != issuer {
		t.Errorf("auth_issuer = %v, want %q", resp["auth_issuer"], issuer)
	}
	if strings.Contains(body, subject) {
		t.Errorf("the privileges view leaked the provider subject: %s", body)
	}
}

// TestRBACHandler_GetUserPrivileges_LocalUser checks the local case reports a
// source and omits the issuer, so the field is never blank and never invented.
func TestRBACHandler_GetUserPrivileges_LocalUser(t *testing.T) {
	handler, store, adminID, cleanup := adminRBACHandler(t)
	defer cleanup()

	if err := store.CreateUser("localpriv", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	targetID, _ := store.GetUserID("localpriv")

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/users/"+strconv.FormatInt(targetID, 10)+"/privileges", nil)
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.getUserPrivileges(rec, req, targetID)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["auth_source"] != auth.AuthSourceLocal {
		t.Errorf("auth_source = %v, want %q", resp["auth_source"], auth.AuthSourceLocal)
	}
	if _, ok := resp["auth_issuer"]; ok {
		t.Errorf("local account carries an auth_issuer: %v", resp["auth_issuer"])
	}
}

// TestRBACHandler_ListUsers_StoreFailure covers the error branch the listing
// takes when the auth store cannot be read, which must report a generic
// failure rather than anything about the underlying store.
func TestRBACHandler_ListUsers_StoreFailure(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// A superuser passes the permission gate on the request context alone,
	// so closing the store leaves the listing as the first thing to fail.
	store.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d. Body: %s",
			http.StatusInternalServerError, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Failed to list users") {
		t.Fatalf("body %q does not report the failure", rec.Body.String())
	}
}
