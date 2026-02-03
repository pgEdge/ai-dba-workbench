/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package auth

import (
	"os"
	"testing"
	"time"
)

// createTestAuthStoreForStore creates a temporary auth store for testing
func createTestAuthStoreForStore(t *testing.T) (*AuthStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "auth-store-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	store, err := NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create auth store: %v", err)
	}

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

// =============================================================================
// AuthStore Creation Tests
// =============================================================================

func TestNewAuthStore(t *testing.T) {
	t.Run("creates store with valid directory", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "auth-store-new-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		store, err := NewAuthStore(tmpDir, 30, 5)
		if err != nil {
			t.Fatalf("Failed to create auth store: %v", err)
		}
		defer store.Close()

		if store == nil {
			t.Fatal("Expected non-nil store")
		}
	})

	t.Run("creates directory if it does not exist", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "auth-store-parent-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		newDir := tmpDir + "/subdir"
		store, err := NewAuthStore(newDir, 0, 0)
		if err != nil {
			t.Fatalf("Failed to create auth store: %v", err)
		}
		defer store.Close()

		if _, err := os.Stat(newDir); os.IsNotExist(err) {
			t.Error("Expected directory to be created")
		}
	})
}

// =============================================================================
// User Management Extended Tests
// =============================================================================

func TestCreateUserWithAllFields(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	err := store.CreateUser("testuser", "password123", "Test annotation", "Test User", "test@example.com")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	user, err := store.GetUser("testuser")
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}

	if user.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got %q", user.Username)
	}

	if user.Annotation != "Test annotation" {
		t.Errorf("Expected annotation 'Test annotation', got %q", user.Annotation)
	}

	if user.DisplayName != "Test User" {
		t.Errorf("Expected display name 'Test User', got %q", user.DisplayName)
	}

	if user.Email != "test@example.com" {
		t.Errorf("Expected email 'test@example.com', got %q", user.Email)
	}

	if !user.Enabled {
		t.Error("Expected user to be enabled by default")
	}

	if user.IsSuperuser {
		t.Error("Expected user to not be superuser by default")
	}
}

func TestGetUserByID(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("testuser", "password123", "Test", "Display", "email@test.com")
	user, _ := store.GetUser("testuser")

	t.Run("retrieves existing user", func(t *testing.T) {
		fetched, err := store.GetUserByID(user.ID)
		if err != nil {
			t.Fatalf("Failed to get user by ID: %v", err)
		}

		if fetched.Username != "testuser" {
			t.Errorf("Expected username 'testuser', got %q", fetched.Username)
		}
	})

	t.Run("returns nil for non-existent ID", func(t *testing.T) {
		fetched, err := store.GetUserByID(99999)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if fetched != nil {
			t.Error("Expected nil for non-existent user")
		}
	})
}

func TestUpdateUserDisplayName(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("testuser", "password", "annotation", "Original", "email@test.com")

	err := store.UpdateUserDisplayName("testuser", "Updated Name")
	if err != nil {
		t.Fatalf("Failed to update display name: %v", err)
	}

	user, _ := store.GetUser("testuser")
	if user.DisplayName != "Updated Name" {
		t.Errorf("Expected 'Updated Name', got %q", user.DisplayName)
	}
}

func TestUpdateUserDisplayNameNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	err := store.UpdateUserDisplayName("nonexistent", "Name")
	if err == nil {
		t.Error("Expected error for non-existent user")
	}
}

func TestUpdateUserEmail(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("testuser", "password", "annotation", "Name", "old@test.com")

	err := store.UpdateUserEmail("testuser", "new@test.com")
	if err != nil {
		t.Fatalf("Failed to update email: %v", err)
	}

	user, _ := store.GetUser("testuser")
	if user.Email != "new@test.com" {
		t.Errorf("Expected 'new@test.com', got %q", user.Email)
	}
}

func TestUpdateUserEmailNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	err := store.UpdateUserEmail("nonexistent", "email@test.com")
	if err == nil {
		t.Error("Expected error for non-existent user")
	}
}

// =============================================================================
// Authentication Tests
// =============================================================================

func TestAuthenticateUserSuccessUpdatesLastLogin(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("testuser", "password123", "", "", "")

	user, _ := store.GetUser("testuser")
	if user.LastLogin != nil {
		t.Error("Expected nil last login before authentication")
	}

	_, _, err := store.AuthenticateUser("testuser", "password123")
	if err != nil {
		t.Fatalf("Authentication failed: %v", err)
	}

	user, _ = store.GetUser("testuser")
	if user.LastLogin == nil {
		t.Error("Expected last login to be set after authentication")
	}
}

func TestAuthenticateUserResetsFailedAttempts(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-failed-attempts-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 5)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer store.Close()

	store.CreateUser("testuser", "password123", "", "", "")

	// Failed login attempt
	_, _, _ = store.AuthenticateUser("testuser", "wrongpassword")

	user, _ := store.GetUser("testuser")
	if user.FailedAttempts != 1 {
		t.Errorf("Expected 1 failed attempt, got %d", user.FailedAttempts)
	}

	// Successful login
	_, _, err = store.AuthenticateUser("testuser", "password123")
	if err != nil {
		t.Fatalf("Authentication failed: %v", err)
	}

	user, _ = store.GetUser("testuser")
	if user.FailedAttempts != 0 {
		t.Errorf("Expected 0 failed attempts after successful login, got %d", user.FailedAttempts)
	}
}

func TestAuthenticateUserAccountLockout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-lockout-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	maxAttempts := 3
	store, err := NewAuthStore(tmpDir, 0, maxAttempts)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer store.Close()

	store.CreateUser("testuser", "password123", "", "", "")

	// Max failed attempts
	for i := 0; i < maxAttempts; i++ {
		_, _, _ = store.AuthenticateUser("testuser", "wrongpassword")
	}

	// Verify account is locked
	user, _ := store.GetUser("testuser")
	if user.Enabled {
		t.Error("Expected account to be disabled after max failed attempts")
	}

	// Attempt with correct password should fail
	_, _, err = store.AuthenticateUser("testuser", "password123")
	if err == nil {
		t.Error("Expected error for locked account")
	}
}

func TestAuthenticateUserDisabledAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("testuser", "password123", "", "", "")
	store.DisableUser("testuser")

	_, _, err := store.AuthenticateUser("testuser", "password123")
	if err == nil {
		t.Error("Expected error for disabled account")
	}
}

func TestAuthenticateUserWrongPassword(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("testuser", "password123", "", "", "")

	_, _, err := store.AuthenticateUser("testuser", "wrongpassword")
	if err == nil {
		t.Error("Expected error for wrong password")
	}
}

func TestAuthenticateUserNonExistent(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	_, _, err := store.AuthenticateUser("nonexistent", "password")
	if err == nil {
		t.Error("Expected error for non-existent user")
	}
}

// =============================================================================
// Session Token Tests
// =============================================================================

func TestValidateSessionTokenExpired(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("testuser", "password123", "", "", "")
	token, _, _ := store.AuthenticateUser("testuser", "password123")

	// Get the session and manually expire it
	tokenHash := GetTokenHashByRawToken(token)
	value, _ := store.sessions.Load(tokenHash)
	session := value.(*SessionInfo)
	session.ExpiresAt = time.Now().Add(-1 * time.Hour)

	// Validation should fail
	_, err := store.ValidateSessionToken(token)
	if err == nil {
		t.Error("Expected error for expired session")
	}
}

func TestValidateSessionTokenDisabledUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("testuser", "password123", "", "", "")
	token, _, _ := store.AuthenticateUser("testuser", "password123")

	// Disable user after login
	store.DisableUser("testuser")

	// Validation should fail
	_, err := store.ValidateSessionToken(token)
	if err == nil {
		t.Error("Expected error for disabled user session")
	}
}

func TestInvalidateSession(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("testuser", "password123", "", "", "")
	token, _, _ := store.AuthenticateUser("testuser", "password123")

	// Invalidate the session
	store.InvalidateSession(token)

	// Validation should fail
	_, err := store.ValidateSessionToken(token)
	if err == nil {
		t.Error("Expected error for invalidated session")
	}
}

// =============================================================================
// Service Token Tests
// =============================================================================

func TestCreateServiceToken(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	rawToken, storedToken, err := store.CreateServiceToken("Test token", nil, "testdb", true)
	if err != nil {
		t.Fatalf("Failed to create service token: %v", err)
	}

	if rawToken == "" {
		t.Error("Expected non-empty raw token")
	}

	if storedToken.TokenType != TokenTypeService {
		t.Errorf("Expected token type 'service', got %q", storedToken.TokenType)
	}

	if storedToken.Database != "testdb" {
		t.Errorf("Expected database 'testdb', got %q", storedToken.Database)
	}

	if !storedToken.IsSuperuser {
		t.Error("Expected superuser flag to be true")
	}

	if storedToken.OwnerID != nil {
		t.Error("Expected nil owner ID for service token")
	}
}

func TestCreateServiceTokenWithExpiry(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	expiry := time.Now().Add(24 * time.Hour)
	rawToken, storedToken, err := store.CreateServiceToken("Test token", &expiry, "", false)
	if err != nil {
		t.Fatalf("Failed to create service token: %v", err)
	}

	if storedToken.ExpiresAt == nil {
		t.Fatal("Expected expiry to be set")
	}

	// Validate the token
	validated, err := store.ValidateToken(rawToken)
	if err != nil {
		t.Fatalf("Failed to validate token: %v", err)
	}

	if validated.ID != storedToken.ID {
		t.Errorf("Expected ID %d, got %d", storedToken.ID, validated.ID)
	}
}

func TestValidateTokenExpired(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	expiry := time.Now().Add(-1 * time.Hour) // Already expired
	rawToken, _, err := store.CreateServiceToken("Test token", &expiry, "", false)
	if err != nil {
		t.Fatalf("Failed to create service token: %v", err)
	}

	_, err = store.ValidateToken(rawToken)
	if err == nil {
		t.Error("Expected error for expired token")
	}
}

func TestValidateTokenInvalid(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	_, err := store.ValidateToken("invalid-token-123")
	if err == nil {
		t.Error("Expected error for invalid token")
	}
}

// =============================================================================
// User Token Tests
// =============================================================================

func TestCreateUserToken(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("testuser", "password", "", "", "")

	rawToken, storedToken, err := store.CreateUserToken("testuser", "My API token", 30)
	if err != nil {
		t.Fatalf("Failed to create user token: %v", err)
	}

	if rawToken == "" {
		t.Error("Expected non-empty raw token")
	}

	if storedToken.TokenType != TokenTypeUser {
		t.Errorf("Expected token type 'user', got %q", storedToken.TokenType)
	}

	if storedToken.OwnerID == nil {
		t.Fatal("Expected non-nil owner ID for user token")
	}

	if storedToken.Annotation != "My API token" {
		t.Errorf("Expected annotation 'My API token', got %q", storedToken.Annotation)
	}
}

func TestCreateUserTokenNonExistentUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	_, _, err := store.CreateUserToken("nonexistent", "Token", 30)
	if err == nil {
		t.Error("Expected error for non-existent user")
	}
}

func TestCreateUserTokenWithMaxDays(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-max-days-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	maxDays := 30
	store, err := NewAuthStore(tmpDir, maxDays, 0)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer store.Close()

	store.CreateUser("testuser", "password", "", "", "")

	// Request 90 days, should be limited to 30
	_, storedToken, err := store.CreateUserToken("testuser", "Token", 90)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	if storedToken.ExpiresAt == nil {
		t.Fatal("Expected expiry to be set")
	}

	// Check expiry is approximately maxDays from now
	expectedExpiry := time.Now().AddDate(0, 0, maxDays)
	diff := storedToken.ExpiresAt.Sub(expectedExpiry)
	if diff < -time.Minute || diff > time.Minute {
		t.Errorf("Expiry should be around %d days, got difference of %v", maxDays, diff)
	}
}

func TestValidateUserTokenDisabledOwner(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("testuser", "password", "", "", "")
	rawToken, _, err := store.CreateUserToken("testuser", "Token", 30)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	// Disable the owner
	store.DisableUser("testuser")

	// Validation should fail
	_, err = store.ValidateToken(rawToken)
	if err == nil {
		t.Error("Expected error when owner is disabled")
	}
}

func TestListUserTokens(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("testuser", "password", "", "", "")
	store.CreateUserToken("testuser", "Token 1", 30)
	store.CreateUserToken("testuser", "Token 2", 30)

	tokens, err := store.ListUserTokens("testuser")
	if err != nil {
		t.Fatalf("Failed to list tokens: %v", err)
	}

	if len(tokens) != 2 {
		t.Errorf("Expected 2 tokens, got %d", len(tokens))
	}
}

func TestDeleteUserToken(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("testuser", "password", "", "", "")
	_, storedToken, _ := store.CreateUserToken("testuser", "Token", 30)

	err := store.DeleteUserToken("testuser", storedToken.ID)
	if err != nil {
		t.Fatalf("Failed to delete token: %v", err)
	}

	tokens, _ := store.ListUserTokens("testuser")
	if len(tokens) != 0 {
		t.Errorf("Expected 0 tokens after deletion, got %d", len(tokens))
	}
}

func TestDeleteUserTokenNotOwned(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("user1", "password", "", "", "")
	store.CreateUser("user2", "password", "", "", "")
	_, storedToken, _ := store.CreateUserToken("user1", "Token", 30)

	// Try to delete user1's token as user2
	err := store.DeleteUserToken("user2", storedToken.ID)
	if err == nil {
		t.Error("Expected error when deleting token not owned by user")
	}
}

// =============================================================================
// Service Token Management Tests
// =============================================================================

func TestListServiceTokens(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateServiceToken("Token 1", nil, "", false)
	store.CreateServiceToken("Token 2", nil, "", false)
	store.CreateUser("testuser", "password", "", "", "")
	store.CreateUserToken("testuser", "User Token", 30)

	tokens, err := store.ListServiceTokens()
	if err != nil {
		t.Fatalf("Failed to list service tokens: %v", err)
	}

	// Should only include service tokens, not user tokens
	if len(tokens) != 2 {
		t.Errorf("Expected 2 service tokens, got %d", len(tokens))
	}
}

func TestListAllTokens(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateServiceToken("Service Token", nil, "", false)
	store.CreateUser("testuser", "password", "", "", "")
	store.CreateUserToken("testuser", "User Token", 30)

	tokens, err := store.ListAllTokens()
	if err != nil {
		t.Fatalf("Failed to list all tokens: %v", err)
	}

	if len(tokens) != 2 {
		t.Errorf("Expected 2 tokens, got %d", len(tokens))
	}
}

func TestDeleteServiceTokenByID(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	_, storedToken, _ := store.CreateServiceToken("Token", nil, "", false)

	// Deletion by ID requires the ID as a string - this test just confirms the code path
	_ = store.DeleteServiceToken("1")
	_ = storedToken // Use the variable
}

func TestDeleteServiceTokenByHashPrefix(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	_, storedToken, _ := store.CreateServiceToken("Token", nil, "", false)

	// Use first 8 characters of hash
	prefix := storedToken.TokenHash[:8]
	err := store.DeleteServiceToken(prefix)
	if err != nil {
		t.Fatalf("Failed to delete token by hash prefix: %v", err)
	}

	tokens, _ := store.ListServiceTokens()
	if len(tokens) != 0 {
		t.Errorf("Expected 0 tokens after deletion, got %d", len(tokens))
	}
}

func TestDeleteServiceTokenNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	err := store.DeleteServiceToken("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent token")
	}
}

// =============================================================================
// Token Cleanup Tests
// =============================================================================

func TestCleanupExpiredTokensStore(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	// Create expired token
	expiredTime := time.Now().Add(-1 * time.Hour)
	store.CreateServiceToken("Expired", &expiredTime, "", false)

	// Create valid token
	validTime := time.Now().Add(24 * time.Hour)
	store.CreateServiceToken("Valid", &validTime, "", false)

	// Create never-expiring token
	store.CreateServiceToken("Never Expires", nil, "", false)

	count, hashes := store.CleanupExpiredTokens()
	if count != 1 {
		t.Errorf("Expected 1 token cleaned up, got %d", count)
	}

	if len(hashes) != 1 {
		t.Errorf("Expected 1 hash returned, got %d", len(hashes))
	}

	tokens, _ := store.ListServiceTokens()
	if len(tokens) != 2 {
		t.Errorf("Expected 2 tokens remaining, got %d", len(tokens))
	}
}

// =============================================================================
// Connection Session Tests
// =============================================================================

func TestConnectionSession(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	tokenHash := "test-token-hash-123"
	dbName := "testdb"

	// Set session
	err := store.SetConnectionSession(tokenHash, 1, &dbName)
	if err != nil {
		t.Fatalf("Failed to set connection session: %v", err)
	}

	// Get session
	session, err := store.GetConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("Failed to get connection session: %v", err)
	}

	if session == nil {
		t.Fatal("Expected session to exist")
	}

	if session.ConnectionID != 1 {
		t.Errorf("Expected connection ID 1, got %d", session.ConnectionID)
	}

	if session.DatabaseName == nil || *session.DatabaseName != "testdb" {
		t.Error("Expected database name 'testdb'")
	}
}

func TestConnectionSessionUpdate(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	tokenHash := "test-token-hash-123"
	db1 := "db1"
	db2 := "db2"

	store.SetConnectionSession(tokenHash, 1, &db1)
	store.SetConnectionSession(tokenHash, 2, &db2)

	session, _ := store.GetConnectionSession(tokenHash)
	if session.ConnectionID != 2 {
		t.Errorf("Expected updated connection ID 2, got %d", session.ConnectionID)
	}
}

func TestConnectionSessionNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	session, err := store.GetConnectionSession("nonexistent")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if session != nil {
		t.Error("Expected nil for non-existent session")
	}
}

func TestClearConnectionSession(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	tokenHash := "test-token-hash-123"
	store.SetConnectionSession(tokenHash, 1, nil)

	err := store.ClearConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("Failed to clear session: %v", err)
	}

	session, _ := store.GetConnectionSession(tokenHash)
	if session != nil {
		t.Error("Expected session to be cleared")
	}
}

func TestClearAllConnectionSessions(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.SetConnectionSession("hash1", 1, nil)
	store.SetConnectionSession("hash2", 2, nil)

	err := store.ClearAllConnectionSessions()
	if err != nil {
		t.Fatalf("Failed to clear all sessions: %v", err)
	}

	session1, _ := store.GetConnectionSession("hash1")
	session2, _ := store.GetConnectionSession("hash2")

	if session1 != nil || session2 != nil {
		t.Error("Expected all sessions to be cleared")
	}
}

// =============================================================================
// Count Helper Tests
// =============================================================================

func TestUserCount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if count := store.UserCount(); count != 0 {
		t.Errorf("Expected 0 users initially, got %d", count)
	}

	store.CreateUser("user1", "pass", "", "", "")
	store.CreateUser("user2", "pass", "", "", "")

	if count := store.UserCount(); count != 2 {
		t.Errorf("Expected 2 users, got %d", count)
	}
}

func TestTokenCount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if count := store.TokenCount(); count != 0 {
		t.Errorf("Expected 0 tokens initially, got %d", count)
	}

	store.CreateServiceToken("Token 1", nil, "", false)
	store.CreateServiceToken("Token 2", nil, "", false)

	if count := store.TokenCount(); count != 2 {
		t.Errorf("Expected 2 tokens, got %d", count)
	}
}

func TestServiceTokenCount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateServiceToken("Service 1", nil, "", false)
	store.CreateServiceToken("Service 2", nil, "", false)
	store.CreateUser("testuser", "password", "", "", "")
	store.CreateUserToken("testuser", "User Token", 30)

	if count := store.ServiceTokenCount(); count != 2 {
		t.Errorf("Expected 2 service tokens, got %d", count)
	}
}

func TestGetCounts(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("user1", "pass", "", "", "")
	store.CreateServiceToken("Token", nil, "", false)

	users, tokens := store.GetCounts()
	if users != 1 {
		t.Errorf("Expected 1 user, got %d", users)
	}
	if tokens != 1 {
		t.Errorf("Expected 1 token, got %d", tokens)
	}
}

// =============================================================================
// GetTokenHashByRawToken Tests
// =============================================================================

func TestGetTokenHashByRawToken(t *testing.T) {
	t.Run("produces consistent hashes", func(t *testing.T) {
		token := "test-token-123"
		hash1 := GetTokenHashByRawToken(token)
		hash2 := GetTokenHashByRawToken(token)

		if hash1 != hash2 {
			t.Error("Expected consistent hash for same token")
		}
	})

	t.Run("produces different hashes for different tokens", func(t *testing.T) {
		hash1 := GetTokenHashByRawToken("token1")
		hash2 := GetTokenHashByRawToken("token2")

		if hash1 == hash2 {
			t.Error("Expected different hashes for different tokens")
		}
	})

	t.Run("produces SHA256 hex string", func(t *testing.T) {
		hash := GetTokenHashByRawToken("test")
		// SHA256 produces 64 hex characters
		if len(hash) != 64 {
			t.Errorf("Expected 64 character hash, got %d", len(hash))
		}
	})
}

// =============================================================================
// ResetFailedAttempts Tests
// =============================================================================

func TestResetFailedAttemptsStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-reset-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 5)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer store.Close()

	store.CreateUser("testuser", "password", "", "", "")

	// Generate failed attempts
	store.AuthenticateUser("testuser", "wrong")
	store.AuthenticateUser("testuser", "wrong")

	user, _ := store.GetUser("testuser")
	if user.FailedAttempts != 2 {
		t.Errorf("Expected 2 failed attempts, got %d", user.FailedAttempts)
	}

	// Reset
	err = store.ResetFailedAttempts("testuser")
	if err != nil {
		t.Fatalf("Failed to reset: %v", err)
	}

	user, _ = store.GetUser("testuser")
	if user.FailedAttempts != 0 {
		t.Errorf("Expected 0 failed attempts after reset, got %d", user.FailedAttempts)
	}
}
