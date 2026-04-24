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

	"golang.org/x/crypto/bcrypt"
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
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

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

	t.Run("sets Created to true for new database", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "auth-store-created-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		store, err := NewAuthStore(tmpDir, 0, 0)
		if err != nil {
			t.Fatalf("Failed to create auth store: %v", err)
		}
		defer store.Close()

		if !store.Created {
			t.Error("Expected Created to be true for new database")
		}
	})

	t.Run("sets Created to false for existing database", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "auth-store-existing-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		// Create and close the first store to establish the database
		store1, err := NewAuthStore(tmpDir, 0, 0)
		if err != nil {
			t.Fatalf("Failed to create initial auth store: %v", err)
		}
		store1.Close()

		// Open the existing database
		store2, err := NewAuthStore(tmpDir, 0, 0)
		if err != nil {
			t.Fatalf("Failed to open existing auth store: %v", err)
		}
		defer store2.Close()

		if store2.Created {
			t.Error("Expected Created to be false for existing database")
		}
	})
}

func TestAuthStorePath(t *testing.T) {
	t.Run("returns correct database path", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "auth-store-path-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		store, err := NewAuthStore(tmpDir, 0, 0)
		if err != nil {
			t.Fatalf("Failed to create auth store: %v", err)
		}
		defer store.Close()

		expectedPath := tmpDir + "/auth.db"
		if store.Path() != expectedPath {
			t.Errorf("Expected path %q, got %q", expectedPath, store.Path())
		}
	})
}

// TestSetBcryptCostForTesting exercises the test-only hook that lowers
// the per-store bcrypt cost. The coverage target is the two observable
// behaviors: (1) a non-nil testing.T updates bcryptCost so subsequent
// hashing calls use the override, and (2) a nil testing.T panics with
// the documented message so accidental production use is caught
// immediately. Coverage of the happy path is also exercised indirectly
// by every other test that calls createTestAuthStoreForStore, but an
// explicit test pins the behavior so a future regression cannot silently
// reintroduce the baseline-measure no-op.
func TestSetBcryptCostForTesting(t *testing.T) {
	t.Run("overrides the per-store cost", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		// createTestAuthStoreForStore already lowered the cost to
		// bcrypt.MinCost, but assert it explicitly so the contract is
		// part of the test output rather than implied.
		store.mu.RLock()
		got := store.bcryptCost
		store.mu.RUnlock()
		if got != bcrypt.MinCost {
			t.Errorf("Expected bcryptCost=%d, got %d", bcrypt.MinCost, got)
		}

		// Override again to a different legal cost and verify it sticks.
		store.SetBcryptCostForTesting(t, bcrypt.MinCost+1)
		store.mu.RLock()
		got = store.bcryptCost
		store.mu.RUnlock()
		if got != bcrypt.MinCost+1 {
			t.Errorf("Expected bcryptCost=%d after override, got %d",
				bcrypt.MinCost+1, got)
		}
	})

	t.Run("panics when t is nil", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("Expected panic when SetBcryptCostForTesting " +
					"is called with nil *testing.T")
			}
		}()

		// The interface argument is typed, so we pass an explicit
		// nil via the interface to trigger the gate. This simulates
		// accidental production use.
		store.SetBcryptCostForTesting(nil, bcrypt.MinCost)
	})
}

// =============================================================================
// User Management Extended Tests
// =============================================================================

func TestCreateUserWithAllFields(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	err := store.CreateUser("testuser", "Password123", "Test annotation", "Test User", "test@example.com")
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

	if err := store.CreateUser("testuser", "Password123", "Test", "Display", "email@test.com"); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	user, err := store.GetUser("testuser")
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}

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

	if err := store.CreateUser("testuser", "Password1", "annotation", "Original", "email@test.com"); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

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

	if err := store.CreateUser("testuser", "Password1", "annotation", "Name", "old@test.com"); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

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

// TestUpdateUser exercises the legacy (non-atomic) UpdateUser method
// end to end. The method hashes passwords through the per-store
// bcryptCost, so this test guards the path that was previously a 0%
// coverage hole and pins the behavior that SetBcryptCostForTesting now
// controls. It also exercises the non-password update branch and the
// password-change session invalidation.
func TestUpdateUser(t *testing.T) {
	t.Run("updates password and invalidates sessions", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		if err := store.CreateUser("alice", "Password1", "original",
			"Alice", "alice@example.com"); err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}

		// Establish a session so InvalidateUserSessions has something
		// to evict; this exercises the password-change branch end to
		// end.
		if _, _, err := store.AuthenticateUser("alice", "Password1"); err != nil {
			t.Fatalf("Initial authentication failed: %v", err)
		}

		err := store.UpdateUser("alice", "NewPassword9",
			"updated annotation", "Alice A", "alice.a@example.com")
		if err != nil {
			t.Fatalf("UpdateUser returned error: %v", err)
		}

		// The old password must no longer work; the new one must.
		if _, _, err := store.AuthenticateUser("alice", "Password1"); err == nil {
			t.Error("Old password still authenticates after UpdateUser")
		}
		if _, _, err := store.AuthenticateUser("alice", "NewPassword9"); err != nil {
			t.Errorf("New password does not authenticate: %v", err)
		}

		user, err := store.GetUser("alice")
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		if user.Annotation != "updated annotation" {
			t.Errorf("Annotation not updated: %q", user.Annotation)
		}
		if user.DisplayName != "Alice A" {
			t.Errorf("Display name not updated: %q", user.DisplayName)
		}
		if user.Email != "alice.a@example.com" {
			t.Errorf("Email not updated: %q", user.Email)
		}
	})

	t.Run("updates metadata without touching password", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		if err := store.CreateUser("bob", "Password1", "",
			"Bob", "bob@example.com"); err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}

		// Empty newPassword should leave the password alone.
		if err := store.UpdateUser("bob", "", "new note",
			"Bob B", "bob.b@example.com"); err != nil {
			t.Fatalf("UpdateUser returned error: %v", err)
		}

		// Original password must still work.
		if _, _, err := store.AuthenticateUser("bob", "Password1"); err != nil {
			t.Errorf("Password was unexpectedly changed: %v", err)
		}

		user, err := store.GetUser("bob")
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		if user.Annotation != "new note" {
			t.Errorf("Annotation not updated: %q", user.Annotation)
		}
	})

	t.Run("rejects invalid password", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		if err := store.CreateUser("carol", "Password1", "",
			"Carol", "carol@example.com"); err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}

		// "short" fails the complexity requirements (length, missing
		// character classes), so ValidatePassword must reject it and
		// UpdateUser must surface the error without mutating state.
		if err := store.UpdateUser("carol", "short", "", "", ""); err == nil {
			t.Error("Expected error for invalid password, got nil")
		}
	})
}

// =============================================================================
// Authentication Tests
// =============================================================================

func TestAuthenticateUserSuccessUpdatesLastLogin(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("testuser", "Password123", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	user, _ := store.GetUser("testuser")
	if user.LastLogin != nil {
		t.Error("Expected nil last login before authentication")
	}

	_, _, err := store.AuthenticateUser("testuser", "Password123")
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
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	if err := store.CreateUser("testuser", "Password123", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Failed login attempt
	_, _, _ = store.AuthenticateUser("testuser", "Wrongpassword1")

	user, _ := store.GetUser("testuser")
	if user.FailedAttempts != 1 {
		t.Errorf("Expected 1 failed attempt, got %d", user.FailedAttempts)
	}

	// Successful login
	_, _, err = store.AuthenticateUser("testuser", "Password123")
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
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	if err := store.CreateUser("testuser", "Password123", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Max failed attempts
	for i := 0; i < maxAttempts; i++ {
		_, _, _ = store.AuthenticateUser("testuser", "Wrongpassword1")
	}

	// Verify account is locked
	user, _ := store.GetUser("testuser")
	if user.Enabled {
		t.Error("Expected account to be disabled after max failed attempts")
	}

	// Attempt with correct password should fail
	_, _, err = store.AuthenticateUser("testuser", "Password123")
	if err == nil {
		t.Error("Expected error for locked account")
	}
}

func TestAuthenticateUserDisabledAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("testuser", "Password123", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	if err := store.DisableUser("testuser"); err != nil {
		t.Fatalf("Failed to disable user: %v", err)
	}

	_, _, err := store.AuthenticateUser("testuser", "Password123")
	if err == nil {
		t.Error("Expected error for disabled account")
	}
}

func TestAuthenticateUserWrongPassword(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("testuser", "Password123", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	_, _, err := store.AuthenticateUser("testuser", "Wrongpassword1")
	if err == nil {
		t.Error("Expected error for wrong password")
	}
}

func TestAuthenticateUserNonExistent(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	_, _, err := store.AuthenticateUser("nonexistent", "Password1")
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

	if err := store.CreateUser("testuser", "Password123", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	token, _, err := store.AuthenticateUser("testuser", "Password123")
	if err != nil {
		t.Fatalf("Failed to authenticate user: %v", err)
	}

	// Get the session and manually expire it
	tokenHash := GetTokenHashByRawToken(token)
	value, _ := store.sessions.Load(tokenHash)
	session := value.(*SessionInfo)
	session.ExpiresAt = time.Now().Add(-1 * time.Hour)

	// Validation should fail
	_, err = store.ValidateSessionToken(token)
	if err == nil {
		t.Error("Expected error for expired session")
	}
}

func TestValidateSessionTokenDisabledUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("testuser", "Password123", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	token, _, err := store.AuthenticateUser("testuser", "Password123")
	if err != nil {
		t.Fatalf("Failed to authenticate user: %v", err)
	}

	// Disable user after login
	if err := store.DisableUser("testuser"); err != nil {
		t.Fatalf("Failed to disable user: %v", err)
	}

	// Validation should fail
	_, err = store.ValidateSessionToken(token)
	if err == nil {
		t.Error("Expected error for disabled user session")
	}
}

func TestInvalidateSession(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("testuser", "Password123", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	token, _, err := store.AuthenticateUser("testuser", "Password123")
	if err != nil {
		t.Fatalf("Failed to authenticate user: %v", err)
	}

	// Invalidate the session
	store.InvalidateSession(token)

	// Validation should fail
	_, err = store.ValidateSessionToken(token)
	if err == nil {
		t.Error("Expected error for invalidated session")
	}
}

// =============================================================================
// Token Tests
// =============================================================================

func TestCreateToken(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("testuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	rawToken, storedToken, err := store.CreateToken("testuser", "Test token", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	if rawToken == "" {
		t.Error("Expected non-empty raw token")
	}

	if storedToken.OwnerID == 0 {
		t.Error("Expected non-zero owner ID")
	}

	if storedToken.Annotation != "Test token" {
		t.Errorf("Expected annotation 'Test token', got %q", storedToken.Annotation)
	}
}

func TestCreateTokenWithExpiry(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("testuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	expiry := time.Now().Add(24 * time.Hour)
	rawToken, storedToken, err := store.CreateToken("testuser", "Test token", &expiry)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
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

	if err := store.CreateUser("testuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	expiry := time.Now().Add(-1 * time.Hour) // Already expired
	rawToken, _, err := store.CreateToken("testuser", "Test token", &expiry)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
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

func TestCreateTokenNonExistentUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	_, _, err := store.CreateToken("nonexistent", "Token", nil)
	if err == nil {
		t.Error("Expected error for non-existent user")
	}
}

func TestCreateTokenWithMaxDays(t *testing.T) {
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
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	if err := store.CreateUser("testuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Non-superuser with nil expiry should get maxDays applied
	_, storedToken, err := store.CreateToken("testuser", "Token", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	if storedToken.ExpiresAt == nil {
		t.Fatal("Expected expiry to be set for non-superuser")
	}

	// Check expiry is approximately maxDays from now
	expectedExpiry := time.Now().AddDate(0, 0, maxDays)
	diff := storedToken.ExpiresAt.Sub(expectedExpiry)
	if diff < -time.Minute || diff > time.Minute {
		t.Errorf("Expiry should be around %d days, got difference of %v", maxDays, diff)
	}
}

func TestCreateTokenSuperuserNoExpiry(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("superuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	if err := store.SetUserSuperuser("superuser", true); err != nil {
		t.Fatalf("Failed to set superuser: %v", err)
	}

	// Superuser with nil expiry should get no expiry
	_, storedToken, err := store.CreateToken("superuser", "Token", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	if storedToken.ExpiresAt != nil {
		t.Error("Expected no expiry for superuser token with nil requestedExpiry")
	}
}

func TestValidateTokenDisabledOwner(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("testuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	rawToken, _, err := store.CreateToken("testuser", "Token", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	// Disable the owner
	if err := store.DisableUser("testuser"); err != nil {
		t.Fatalf("Failed to disable user: %v", err)
	}

	// Validation should fail
	_, err = store.ValidateToken(rawToken)
	if err == nil {
		t.Error("Expected error when owner is disabled")
	}
}

func TestListUserTokens(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("testuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	if _, _, err := store.CreateToken("testuser", "Token 1", nil); err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}
	if _, _, err := store.CreateToken("testuser", "Token 2", nil); err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

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

	if err := store.CreateUser("testuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	_, storedToken, err := store.CreateToken("testuser", "Token", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	err = store.DeleteUserToken("testuser", storedToken.ID)
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

	if err := store.CreateUser("user1", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user1: %v", err)
	}
	if err := store.CreateUser("user2", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user2: %v", err)
	}
	_, storedToken, err := store.CreateToken("user1", "Token", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	// Try to delete user1's token as user2
	err = store.DeleteUserToken("user2", storedToken.ID)
	if err == nil {
		t.Error("Expected error when deleting token not owned by user")
	}
}

// =============================================================================
// Token Management Tests
// =============================================================================

func TestListAllTokens(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("testuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	if _, _, err := store.CreateToken("testuser", "Token 1", nil); err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}
	if _, _, err := store.CreateToken("testuser", "Token 2", nil); err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	tokens, err := store.ListAllTokens()
	if err != nil {
		t.Fatalf("Failed to list all tokens: %v", err)
	}

	if len(tokens) != 2 {
		t.Errorf("Expected 2 tokens, got %d", len(tokens))
	}
}

func TestDeleteTokenByID(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("testuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	_, storedToken, err := store.CreateToken("testuser", "Token", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	// Deletion by ID requires the ID as a string
	_ = store.DeleteToken("1")
	_ = storedToken // Use the variable
}

func TestDeleteTokenByHashPrefix(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("testuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	_, storedToken, err := store.CreateToken("testuser", "Token", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	// Use first 8 characters of hash
	prefix := storedToken.TokenHash[:8]
	err = store.DeleteToken(prefix)
	if err != nil {
		t.Fatalf("Failed to delete token by hash prefix: %v", err)
	}

	tokens, _ := store.ListAllTokens()
	if len(tokens) != 0 {
		t.Errorf("Expected 0 tokens after deletion, got %d", len(tokens))
	}
}

func TestDeleteTokenNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	err := store.DeleteToken("nonexistent")
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

	if err := store.CreateUser("testuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Create expired token
	expiredTime := time.Now().Add(-1 * time.Hour)
	store.CreateToken("testuser", "Expired", &expiredTime)

	// Create valid token
	validTime := time.Now().Add(24 * time.Hour)
	store.CreateToken("testuser", "Valid", &validTime)

	// Create never-expiring token
	store.CreateToken("testuser", "Never Expires", nil)

	count, hashes := store.CleanupExpiredTokens()
	if count != 1 {
		t.Errorf("Expected 1 token cleaned up, got %d", count)
	}

	if len(hashes) != 1 {
		t.Errorf("Expected 1 hash returned, got %d", len(hashes))
	}

	tokens, _ := store.ListAllTokens()
	if len(tokens) != 2 {
		t.Errorf("Expected 2 tokens remaining, got %d", len(tokens))
	}
}

// =============================================================================
// Service Account Tests
// =============================================================================

func TestCreateServiceAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	err := store.CreateServiceAccount("svc-bot", "CI/CD bot", "Bot Account", "bot@example.com")
	if err != nil {
		t.Fatalf("Failed to create service account: %v", err)
	}

	user, err := store.GetUser("svc-bot")
	if err != nil {
		t.Fatalf("Failed to get service account: %v", err)
	}

	if !user.IsServiceAccount {
		t.Error("Expected IsServiceAccount to be true")
	}

	if !user.Enabled {
		t.Error("Expected service account to be enabled")
	}

	if user.PasswordHash != "" {
		t.Error("Expected empty password hash for service account")
	}
}

func TestServiceAccountCannotLogin(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateServiceAccount("svc-bot", "CI/CD bot", "", ""); err != nil {
		t.Fatalf("Failed to create service account: %v", err)
	}

	// Service accounts cannot authenticate with password
	_, _, err := store.AuthenticateUser("svc-bot", "")
	if err == nil {
		t.Error("Expected error when authenticating service account")
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

	store.CreateUser("user1", "Password1", "", "", "")
	store.CreateUser("user2", "Password1", "", "", "")

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

	store.CreateUser("testuser", "Password1", "", "", "")
	store.CreateToken("testuser", "Token 1", nil)
	store.CreateToken("testuser", "Token 2", nil)

	if count := store.TokenCount(); count != 2 {
		t.Errorf("Expected 2 tokens, got %d", count)
	}
}

func TestGetCounts(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.CreateUser("user1", "Password1", "", "", "")
	store.CreateToken("user1", "Token", nil)

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
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	if err := store.CreateUser("testuser", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

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

// =============================================================================
// UpdateUserAtomic Tests
// =============================================================================

func TestUpdateUserAtomic_AllFields(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	// Create a user
	err := store.CreateUser("testuser", "Oldpass1", "old annotation", "Old Name", "old@example.com")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Update all fields atomically
	newPass := "Newpass123"
	newAnnotation := "new annotation"
	newDisplayName := "New Name"
	newEmail := "new@example.com"
	enabled := false
	superuser := true

	update := UserUpdate{
		Password:    &newPass,
		Annotation:  &newAnnotation,
		DisplayName: &newDisplayName,
		Email:       &newEmail,
		Enabled:     &enabled,
		IsSuperuser: &superuser,
	}

	err = store.UpdateUserAtomic("testuser", update)
	if err != nil {
		t.Fatalf("Failed to update user atomically: %v", err)
	}

	// Verify all fields were updated
	user, err := store.GetUser("testuser")
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}

	if user.Annotation != newAnnotation {
		t.Errorf("Expected annotation %q, got %q", newAnnotation, user.Annotation)
	}
	if user.DisplayName != newDisplayName {
		t.Errorf("Expected display name %q, got %q", newDisplayName, user.DisplayName)
	}
	if user.Email != newEmail {
		t.Errorf("Expected email %q, got %q", newEmail, user.Email)
	}
	if user.Enabled != false {
		t.Error("Expected user to be disabled")
	}
	if user.IsSuperuser != true {
		t.Error("Expected user to be superuser")
	}

	// Verify password was changed by authenticating with new password
	_, _, err = store.AuthenticateUser("testuser", "Newpass123")
	// Authentication should fail because user is disabled
	if err == nil {
		t.Error("Expected authentication to fail for disabled user")
	}

	// Enable user and try again
	store.EnableUser("testuser")
	_, _, err = store.AuthenticateUser("testuser", "Newpass123")
	if err != nil {
		t.Errorf("Expected authentication with new password to succeed: %v", err)
	}
}

func TestUpdateUserAtomic_PartialUpdate(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	// Create a user with all fields
	err := store.CreateUser("testuser", "Password1", "original annotation", "Original Name", "original@example.com")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Update only annotation
	newAnnotation := "updated annotation"
	update := UserUpdate{
		Annotation: &newAnnotation,
	}

	err = store.UpdateUserAtomic("testuser", update)
	if err != nil {
		t.Fatalf("Failed to update user: %v", err)
	}

	user, _ := store.GetUser("testuser")
	if user.Annotation != newAnnotation {
		t.Errorf("Expected annotation %q, got %q", newAnnotation, user.Annotation)
	}
	// Other fields should remain unchanged
	if user.DisplayName != "Original Name" {
		t.Errorf("Expected display name to remain 'Original Name', got %q", user.DisplayName)
	}
	if user.Email != "original@example.com" {
		t.Errorf("Expected email to remain 'original@example.com', got %q", user.Email)
	}
}

func TestUpdateUserAtomic_OnlyEnabledStatus(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	err := store.CreateUser("testuser", "Password1", "", "", "")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Disable user
	enabled := false
	update := UserUpdate{
		Enabled: &enabled,
	}

	err = store.UpdateUserAtomic("testuser", update)
	if err != nil {
		t.Fatalf("Failed to update user: %v", err)
	}

	user, _ := store.GetUser("testuser")
	if user.Enabled {
		t.Error("Expected user to be disabled")
	}

	// Re-enable user
	enabled = true
	update = UserUpdate{
		Enabled: &enabled,
	}

	err = store.UpdateUserAtomic("testuser", update)
	if err != nil {
		t.Fatalf("Failed to update user: %v", err)
	}

	user, _ = store.GetUser("testuser")
	if !user.Enabled {
		t.Error("Expected user to be enabled")
	}
}

func TestUpdateUserAtomic_OnlySuperuserStatus(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	err := store.CreateUser("testuser", "Password1", "", "", "")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Make superuser
	superuser := true
	update := UserUpdate{
		IsSuperuser: &superuser,
	}

	err = store.UpdateUserAtomic("testuser", update)
	if err != nil {
		t.Fatalf("Failed to update user: %v", err)
	}

	user, _ := store.GetUser("testuser")
	if !user.IsSuperuser {
		t.Error("Expected user to be superuser")
	}

	// Remove superuser
	superuser = false
	update = UserUpdate{
		IsSuperuser: &superuser,
	}

	err = store.UpdateUserAtomic("testuser", update)
	if err != nil {
		t.Fatalf("Failed to update user: %v", err)
	}

	user, _ = store.GetUser("testuser")
	if user.IsSuperuser {
		t.Error("Expected user to not be superuser")
	}
}

func TestUpdateUserAtomic_EmptyUpdate(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	err := store.CreateUser("testuser", "Password1", "annotation", "Name", "email@example.com")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Empty update (no fields set)
	update := UserUpdate{}

	err = store.UpdateUserAtomic("testuser", update)
	if err != nil {
		t.Fatalf("Empty update should succeed: %v", err)
	}

	// Verify nothing changed
	user, _ := store.GetUser("testuser")
	if user.Annotation != "annotation" {
		t.Errorf("Expected annotation unchanged, got %q", user.Annotation)
	}
	if user.DisplayName != "Name" {
		t.Errorf("Expected display name unchanged, got %q", user.DisplayName)
	}
}

func TestUpdateUserAtomic_NonExistentUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	newAnnotation := "new annotation"
	update := UserUpdate{
		Annotation: &newAnnotation,
	}

	// Update non-existent user - should fail when trying to get current values
	err := store.UpdateUserAtomic("nonexistent", update)
	if err == nil {
		t.Error("Expected error when updating non-existent user")
	}
}

// TestForeignKeyPragmaIsEnabled asserts that NewAuthStore opens the
// database with PRAGMA foreign_keys = ON. Without the pragma, every
// ON DELETE CASCADE in the schema is a silent no-op, which is the
// root cause of the orphan-row bug class addressed alongside GitHub
// issue #51.
func TestForeignKeyPragmaIsEnabled(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	var fkEnabled int
	if err := store.db.QueryRow("PRAGMA foreign_keys").Scan(&fkEnabled); err != nil {
		t.Fatalf("Failed to read foreign_keys pragma: %v", err)
	}
	if fkEnabled != 1 {
		t.Errorf("Expected foreign_keys=1, got %d", fkEnabled)
	}

	// Cross-check using the pragma_foreign_keys virtual table, which
	// surfaces the same setting through a regular SELECT. Reading it
	// both ways guards against driver quirks that might let one form
	// succeed while the other does not.
	var viaVirtualTable int
	err := store.db.QueryRow(
		"SELECT foreign_keys FROM pragma_foreign_keys",
	).Scan(&viaVirtualTable)
	if err != nil {
		t.Fatalf("Failed to read pragma_foreign_keys virtual table: %v", err)
	}
	if viaVirtualTable != 1 {
		t.Errorf("Expected pragma_foreign_keys.foreign_keys=1, got %d", viaVirtualTable)
	}
}

// TestDeleteUserCleansUpDependentRows verifies that deleting a user
// removes every row that references the user: the user's tokens and
// all of their per-token scope rows, connection_sessions rows keyed on
// those tokens, and group memberships that reference the user.
// Regression test for the orphan-rows bug identified as part of the
// issue #51 follow-up: previously the handler relied on ON DELETE
// CASCADE foreign keys that SQLite does not enforce, so orphan tokens
// and memberships accumulated after every user deletion.
func TestDeleteUserCleansUpDependentRows(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	// Create the target user plus an unrelated second user whose rows
	// must remain untouched.
	if err := store.CreateUser("alice", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create target user: %v", err)
	}
	if err := store.CreateUser("bob", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create second user: %v", err)
	}
	aliceID, err := store.GetUserID("alice")
	if err != nil {
		t.Fatalf("Failed to get alice's ID: %v", err)
	}
	bobID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("Failed to get bob's ID: %v", err)
	}

	// Create a group and add both users so we can verify that alice's
	// membership row is removed while bob's remains.
	groupID, err := store.CreateGroup("team", "Test team")
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}
	if err := store.AddUserToGroup(groupID, aliceID); err != nil {
		t.Fatalf("Failed to add alice to group: %v", err)
	}
	if err := store.AddUserToGroup(groupID, bobID); err != nil {
		t.Fatalf("Failed to add bob to group: %v", err)
	}

	// Give alice two tokens (so we exercise deletion of multiple
	// tokens) and bob one token (to verify his rows survive).
	_, aliceToken1, err := store.CreateToken("alice", "alice token 1", nil)
	if err != nil {
		t.Fatalf("Failed to create alice's first token: %v", err)
	}
	_, aliceToken2, err := store.CreateToken("alice", "alice token 2", nil)
	if err != nil {
		t.Fatalf("Failed to create alice's second token: %v", err)
	}
	_, bobToken, err := store.CreateToken("bob", "bob token", nil)
	if err != nil {
		t.Fatalf("Failed to create bob's token: %v", err)
	}

	// Register an MCP privilege so we can populate token_mcp_scope.
	privID, err := store.RegisterMCPPrivilege(
		"tool.test", MCPPrivilegeTypeTool, "Test tool", false,
	)
	if err != nil {
		t.Fatalf("Failed to register MCP privilege: %v", err)
	}

	// Populate per-token scope rows for each of alice's tokens and for
	// bob's token.
	for _, tok := range []*StoredToken{aliceToken1, aliceToken2, bobToken} {
		if err := store.SetTokenConnectionScope(tok.ID, []ScopedConnection{
			{ConnectionID: 42, AccessLevel: "read_write"},
		}); err != nil {
			t.Fatalf("Failed to set connection scope for token %d: %v", tok.ID, err)
		}
		if err := store.SetTokenMCPScope(tok.ID, []int64{privID}); err != nil {
			t.Fatalf("Failed to set MCP scope for token %d: %v", tok.ID, err)
		}
		if err := store.SetTokenAdminScope(tok.ID, []string{"manage_users"}); err != nil {
			t.Fatalf("Failed to set admin scope for token %d: %v", tok.ID, err)
		}
	}

	// Populate connection_sessions rows for every token. These rows
	// are keyed on token_hash and have no FK, so they must be cleaned
	// up explicitly by DeleteUser.
	for _, tok := range []*StoredToken{aliceToken1, aliceToken2, bobToken} {
		if err := store.SetConnectionSession(tok.TokenHash, 42, nil); err != nil {
			t.Fatalf("Failed to set connection session for token %d: %v", tok.ID, err)
		}
	}

	// Capture alice's token IDs and hashes for post-deletion queries.
	aliceTokenIDs := []int64{aliceToken1.ID, aliceToken2.ID}
	aliceTokenHashes := []string{aliceToken1.TokenHash, aliceToken2.TokenHash}

	// Delete alice.
	if err := store.DeleteUser("alice"); err != nil {
		t.Fatalf("Failed to delete user: %v", err)
	}

	// Helper to assert a zero row count for a specific query.
	assertNoRows := func(label, query string, args ...any) {
		t.Helper()
		var count int
		if err := store.db.QueryRow(query, args...).Scan(&count); err != nil {
			t.Fatalf("Failed to query %s: %v", label, err)
		}
		if count != 0 {
			t.Errorf("Expected 0 orphan rows in %s, got %d", label, count)
		}
	}

	// The user row itself must be gone.
	assertNoRows("users", "SELECT COUNT(*) FROM users WHERE id = ?", aliceID)

	// No tokens owned by alice may remain. Check both by owner_id and
	// by the captured IDs (which catches cases where owner_id is
	// still populated but pointing at a now-nonexistent user).
	assertNoRows("tokens (owner)", "SELECT COUNT(*) FROM tokens WHERE owner_id = ?", aliceID)
	for _, tid := range aliceTokenIDs {
		assertNoRows("tokens (id)", "SELECT COUNT(*) FROM tokens WHERE id = ?", tid)
	}

	// No per-token scope rows may reference alice's former token IDs.
	for _, tid := range aliceTokenIDs {
		assertNoRows(
			"token_connection_scope",
			"SELECT COUNT(*) FROM token_connection_scope WHERE token_id = ?", tid,
		)
		assertNoRows(
			"token_mcp_scope",
			"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ?", tid,
		)
		assertNoRows(
			"token_admin_scope",
			"SELECT COUNT(*) FROM token_admin_scope WHERE token_id = ?", tid,
		)
	}

	// No connection_sessions rows may reference alice's former token
	// hashes.
	for _, hash := range aliceTokenHashes {
		assertNoRows(
			"connection_sessions",
			"SELECT COUNT(*) FROM connection_sessions WHERE token_hash = ?", hash,
		)
	}

	// Alice's group membership must be gone; bob's must survive.
	assertNoRows(
		"group_memberships (alice)",
		"SELECT COUNT(*) FROM group_memberships WHERE member_user_id = ?", aliceID,
	)

	var bobMembershipCount int
	err = store.db.QueryRow(
		"SELECT COUNT(*) FROM group_memberships WHERE member_user_id = ?", bobID,
	).Scan(&bobMembershipCount)
	if err != nil {
		t.Fatalf("Failed to query bob's memberships: %v", err)
	}
	if bobMembershipCount != 1 {
		t.Errorf("Expected bob to retain 1 membership, got %d", bobMembershipCount)
	}

	// Bob's token and all of his dependent rows must survive unchanged.
	var bobTokenCount int
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM tokens WHERE id = ?", bobToken.ID,
	).Scan(&bobTokenCount); err != nil {
		t.Fatalf("Failed to query bob's token: %v", err)
	}
	if bobTokenCount != 1 {
		t.Errorf("Expected bob's token to survive, got %d rows", bobTokenCount)
	}
	var bobScopeCount int
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM token_connection_scope WHERE token_id = ?", bobToken.ID,
	).Scan(&bobScopeCount); err != nil {
		t.Fatalf("Failed to query bob's token connection scope: %v", err)
	}
	if bobScopeCount != 1 {
		t.Errorf("Expected bob's connection scope to survive, got %d rows", bobScopeCount)
	}
	var bobSessionCount int
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM connection_sessions WHERE token_hash = ?", bobToken.TokenHash,
	).Scan(&bobSessionCount); err != nil {
		t.Fatalf("Failed to query bob's connection session: %v", err)
	}
	if bobSessionCount != 1 {
		t.Errorf("Expected bob's connection session to survive, got %d rows", bobSessionCount)
	}
}

// TestDeleteTokenCleansUpDependentRows verifies that DeleteToken (the
// admin-scoped variant that deletes by ID or hash prefix) removes
// every row that references the deleted token: its connection scope,
// MCP scope, admin scope, and the connection_sessions row keyed on
// its hash.
func TestDeleteTokenCleansUpDependentRows(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("alice", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Create two tokens; we'll delete the first and verify the
	// second's dependent rows are untouched.
	_, target, err := store.CreateToken("alice", "target", nil)
	if err != nil {
		t.Fatalf("Failed to create target token: %v", err)
	}
	_, survivor, err := store.CreateToken("alice", "survivor", nil)
	if err != nil {
		t.Fatalf("Failed to create survivor token: %v", err)
	}

	privID, err := store.RegisterMCPPrivilege(
		"tool.test", MCPPrivilegeTypeTool, "Test tool", false,
	)
	if err != nil {
		t.Fatalf("Failed to register MCP privilege: %v", err)
	}

	// Populate dependent rows for both tokens.
	for _, tok := range []*StoredToken{target, survivor} {
		if err := store.SetTokenConnectionScope(tok.ID, []ScopedConnection{
			{ConnectionID: 1, AccessLevel: "read"},
		}); err != nil {
			t.Fatalf("Failed to set connection scope: %v", err)
		}
		if err := store.SetTokenMCPScope(tok.ID, []int64{privID}); err != nil {
			t.Fatalf("Failed to set MCP scope: %v", err)
		}
		if err := store.SetTokenAdminScope(tok.ID, []string{"manage_users"}); err != nil {
			t.Fatalf("Failed to set admin scope: %v", err)
		}
		if err := store.SetConnectionSession(tok.TokenHash, 1, nil); err != nil {
			t.Fatalf("Failed to set connection session: %v", err)
		}
	}

	// Delete the target token by hash prefix. The first 8 characters
	// of the hash are unique enough for this synthetic fixture.
	prefix := target.TokenHash[:8]
	if err := store.DeleteToken(prefix); err != nil {
		t.Fatalf("Failed to delete token: %v", err)
	}

	assertNoRows := func(label, query string, args ...any) {
		t.Helper()
		var count int
		if err := store.db.QueryRow(query, args...).Scan(&count); err != nil {
			t.Fatalf("Failed to query %s: %v", label, err)
		}
		if count != 0 {
			t.Errorf("Expected 0 orphan rows in %s, got %d", label, count)
		}
	}

	assertNoRows("tokens", "SELECT COUNT(*) FROM tokens WHERE id = ?", target.ID)
	assertNoRows(
		"token_connection_scope",
		"SELECT COUNT(*) FROM token_connection_scope WHERE token_id = ?", target.ID,
	)
	assertNoRows(
		"token_mcp_scope",
		"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ?", target.ID,
	)
	assertNoRows(
		"token_admin_scope",
		"SELECT COUNT(*) FROM token_admin_scope WHERE token_id = ?", target.ID,
	)
	assertNoRows(
		"connection_sessions",
		"SELECT COUNT(*) FROM connection_sessions WHERE token_hash = ?", target.TokenHash,
	)

	// Survivor's rows must remain intact.
	assertRowCount := func(label, query string, want int, args ...any) {
		t.Helper()
		var count int
		if err := store.db.QueryRow(query, args...).Scan(&count); err != nil {
			t.Fatalf("Failed to query %s: %v", label, err)
		}
		if count != want {
			t.Errorf("Expected %d rows in %s, got %d", want, label, count)
		}
	}
	assertRowCount("tokens (survivor)",
		"SELECT COUNT(*) FROM tokens WHERE id = ?", 1, survivor.ID)
	assertRowCount("token_connection_scope (survivor)",
		"SELECT COUNT(*) FROM token_connection_scope WHERE token_id = ?", 1, survivor.ID)
	assertRowCount("token_mcp_scope (survivor)",
		"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ?", 1, survivor.ID)
	assertRowCount("token_admin_scope (survivor)",
		"SELECT COUNT(*) FROM token_admin_scope WHERE token_id = ?", 1, survivor.ID)
	assertRowCount("connection_sessions (survivor)",
		"SELECT COUNT(*) FROM connection_sessions WHERE token_hash = ?", 1, survivor.TokenHash)
}

// TestDeleteUserTokenCleansUpDependentRows verifies that the owner-
// scoped DeleteUserToken path removes the same dependent rows as
// DeleteToken. The only difference between the two paths is the
// "owned by user" guard, so we still assert the dependents here
// because the two functions have independent SQL.
func TestDeleteUserTokenCleansUpDependentRows(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("alice", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	_, target, err := store.CreateToken("alice", "target", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	privID, err := store.RegisterMCPPrivilege(
		"tool.test", MCPPrivilegeTypeTool, "Test tool", false,
	)
	if err != nil {
		t.Fatalf("Failed to register MCP privilege: %v", err)
	}

	if err := store.SetTokenConnectionScope(target.ID, []ScopedConnection{
		{ConnectionID: 7, AccessLevel: "read_write"},
	}); err != nil {
		t.Fatalf("Failed to set connection scope: %v", err)
	}
	if err := store.SetTokenMCPScope(target.ID, []int64{privID}); err != nil {
		t.Fatalf("Failed to set MCP scope: %v", err)
	}
	if err := store.SetTokenAdminScope(target.ID, []string{"manage_users"}); err != nil {
		t.Fatalf("Failed to set admin scope: %v", err)
	}
	if err := store.SetConnectionSession(target.TokenHash, 7, nil); err != nil {
		t.Fatalf("Failed to set connection session: %v", err)
	}

	if err := store.DeleteUserToken("alice", target.ID); err != nil {
		t.Fatalf("Failed to delete user token: %v", err)
	}

	assertNoRows := func(label, query string, args ...any) {
		t.Helper()
		var count int
		if err := store.db.QueryRow(query, args...).Scan(&count); err != nil {
			t.Fatalf("Failed to query %s: %v", label, err)
		}
		if count != 0 {
			t.Errorf("Expected 0 orphan rows in %s, got %d", label, count)
		}
	}

	assertNoRows("tokens", "SELECT COUNT(*) FROM tokens WHERE id = ?", target.ID)
	assertNoRows(
		"token_connection_scope",
		"SELECT COUNT(*) FROM token_connection_scope WHERE token_id = ?", target.ID,
	)
	assertNoRows(
		"token_mcp_scope",
		"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ?", target.ID,
	)
	assertNoRows(
		"token_admin_scope",
		"SELECT COUNT(*) FROM token_admin_scope WHERE token_id = ?", target.ID,
	)
	assertNoRows(
		"connection_sessions",
		"SELECT COUNT(*) FROM connection_sessions WHERE token_hash = ?", target.TokenHash,
	)
}
