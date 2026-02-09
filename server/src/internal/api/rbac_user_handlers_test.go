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
// User Listing Tests
// =============================================================================

func TestRBACHandler_ListUsers_WithAdmin(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	// Create admin with manage_users permission
	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	// Create additional users
	store.CreateUser("user2", "password1", "User Two", "user2@example.com", "")
	store.CreateUser("user3", "password1", "User Three", "", "")

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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("user1", "password1", "User One", "", "")

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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	// Create user without manage_users permission
	store.CreateUser("normie", "password1", "Normal", "", "")
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	body, _ := json.Marshal(map[string]string{
		"username":     "newuser",
		"password":     "securepassword",
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	body := `{"password": "secret123"}`
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
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

	expected := "Password must be at least 8 characters"
	if response.Error != expected {
		t.Errorf("Expected %q, got %q", expected, response.Error)
	}
}

func TestRBACHandler_CreateUser_DuplicateUsername(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	// Create a user first
	store.CreateUser("existing", "password1", "Existing User", "", "")

	// Try to create a user with the same username
	body, _ := json.Marshal(map[string]string{
		"username": "existing",
		"password": "password1234",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, adminID)
	rec := httptest.NewRecorder()

	handler.handleUsers(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusInternalServerError, rec.Code, rec.Body.String())
	}
}

func TestRBACHandler_CreateUser_PermissionDenied(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	// Create user without manage_users permission
	store.CreateUser("normie", "password1", "Normal", "", "")
	userID, _ := store.GetUserID("normie")

	body, _ := json.Marshal(map[string]string{
		"username": "newuser",
		"password": "securepassword",
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	enabled := false
	body, _ := json.Marshal(map[string]interface{}{
		"username": "disableduser",
		"password": "securepassword",
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	isServiceAccount := true
	body, _ := json.Marshal(map[string]interface{}{
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	// Service account should not require a password
	isServiceAccount := true
	body, _ := json.Marshal(map[string]interface{}{
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

// =============================================================================
// User Update Tests
// =============================================================================

func TestRBACHandler_UpdateUser_PasswordChange(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "oldpassword", "Target user", "", "")
	targetID, _ := store.GetUserID("target")

	newPassword := "newpassword123"
	body, _ := json.Marshal(map[string]interface{}{
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "password1", "Target user", "", "")
	targetID, _ := store.GetUserID("target")

	shortPw := "short"
	body, _ := json.Marshal(map[string]interface{}{
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

	expected := "Password must be at least 8 characters"
	if response.Error != expected {
		t.Errorf("Expected %q, got %q", expected, response.Error)
	}
}

func TestRBACHandler_UpdateUser_EnableDisable(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "password1", "Target user", "", "")
	targetID, _ := store.GetUserID("target")

	// Disable the user
	disabled := false
	body, _ := json.Marshal(map[string]interface{}{
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
	body, _ = json.Marshal(map[string]interface{}{
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "password1", "Old Name", "old@example.com", "")
	targetID, _ := store.GetUserID("target")

	newName := "New Name"
	newEmail := "new@example.com"
	body, _ := json.Marshal(map[string]interface{}{
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	newName := "Updated Name"
	body, _ := json.Marshal(map[string]interface{}{
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("normie", "password1", "Normal", "", "")
	userID, _ := store.GetUserID("normie")

	store.CreateUser("target", "password1", "Target", "", "")
	targetID, _ := store.GetUserID("target")

	newName := "Hacked Name"
	body, _ := json.Marshal(map[string]interface{}{
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "password1", "Target", "", "")
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "password1", "Target user", "", "")
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("normie", "password1", "Normal", "", "")
	userID, _ := store.GetUserID("normie")

	store.CreateUser("target", "password1", "Target", "", "")
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "password1", "Target", "", "")
	targetID, _ := store.GetUserID("target")

	newName := "Updated Name"
	body, _ := json.Marshal(map[string]interface{}{
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
	adminID, _ := store.GetUserID("admin")
	gID, _ := store.CreateGroup("admins", "Admins")
	store.AddUserToGroup(gID, adminID)
	store.GrantAdminPermission(gID, auth.PermManageUsers)

	store.CreateUser("target", "password1", "Target", "", "")
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
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

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["username"] != "admin" {
		t.Errorf("Expected username 'admin', got %v", resp["username"])
	}

	groups, ok := resp["groups"].([]interface{})
	if !ok {
		t.Fatal("Expected 'groups' array in response")
	}
	if len(groups) < 1 {
		t.Error("Expected at least 1 group for admin user")
	}
}

func TestRBACHandler_GetUserPrivileges_NotFound(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("admin", "password1", "Admin", "", "")
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
	handler, store, cleanup := createTestRBACHandler(t, true)
	defer cleanup()

	store.CreateUser("normie", "password1", "Normal", "", "")
	userID, _ := store.GetUserID("normie")

	store.CreateUser("target", "password1", "Target", "", "")
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
