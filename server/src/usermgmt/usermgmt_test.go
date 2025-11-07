/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package usermgmt

import (
    "context"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

// skipIfNoDatabase skips the test if SKIP_DB_TESTS is set or if no database connection is available
func skipIfNoDatabase(t *testing.T) *pgxpool.Pool {
    if os.Getenv("SKIP_DB_TESTS") != "" {
        t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
    }

    // Get database connection string from environment or use default
    connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
    if connStr == "" {
        t.Skip("Skipping database test (TEST_AI_WORKBENCH_SERVER not set)")
    }

    ctx := context.Background()
    pool, err := pgxpool.New(ctx, connStr)
    if err != nil {
        t.Skipf("Skipping database test (could not connect: %v)", err)
    }

    // Verify connection works
    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        t.Skipf("Skipping database test (ping failed: %v)", err)
    }

    return pool
}

// TestCreateUserTokenNonInteractive_Validation tests parameter validation
func TestCreateUserTokenNonInteractive_Validation(t *testing.T) {
    pool := skipIfNoDatabase(t)
    defer pool.Close()

    ctx := context.Background()

    // Create a test user first
    testUsername := "testuser_token_create"
    testEmail := "testtoken@example.com"
    testFullName := "Test Token User"
    testPassword := "TestPassword123!"

    // Clean up any existing test user
    _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", testUsername) //nolint:errcheck // Cleanup code, failure is acceptable

    _, err := CreateUserNonInteractive(pool, testUsername, testEmail, testFullName, testPassword, false, nil)
    if err != nil {
        t.Fatalf("Failed to create test user: %v", err)
    }
    defer func() {
        _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", testUsername) //nolint:errcheck // Cleanup code, failure is acceptable
    }()

    t.Run("ValidToken_WithMaxLifetime", func(t *testing.T) {
        name := "test-token"
        maxLifetime := 90
        lifetime := 30

        message, token, err := CreateUserTokenNonInteractive(pool, testUsername, &name, lifetime, maxLifetime, nil)
        if err != nil {
            t.Errorf("Expected success, got error: %v", err)
        }
        if message == "" {
            t.Error("Expected non-empty message")
        }
        if token == "" {
            t.Error("Expected non-empty token")
        }
        if !strings.Contains(message, "30 days") {
            t.Errorf("Expected message to mention 30 days, got: %s", message)
        }
    })

    t.Run("ValidToken_IndefiniteLifetime", func(t *testing.T) {
        name := "indefinite-token"
        maxLifetime := 0 // Indefinite allowed
        lifetime := 0    // Indefinite

        message, token, err := CreateUserTokenNonInteractive(pool, testUsername, &name, lifetime, maxLifetime, nil)
        if err != nil {
            t.Errorf("Expected success, got error: %v", err)
        }
        if token == "" {
            t.Error("Expected non-empty token")
        }
        if !strings.Contains(message, "no expiration") {
            t.Errorf("Expected message to mention no expiration, got: %s", message)
        }
    })

    t.Run("InvalidLifetime_ExceedsMax", func(t *testing.T) {
        name := "invalid-token"
        maxLifetime := 90
        lifetime := 100 // Exceeds max

        _, _, err := CreateUserTokenNonInteractive(pool, testUsername, &name, lifetime, maxLifetime, nil)
        if err == nil {
            t.Error("Expected error for lifetime exceeding maximum")
        }
        if !strings.Contains(err.Error(), "between 1 and 90") {
            t.Errorf("Expected error about max lifetime, got: %v", err)
        }
    })

    t.Run("InvalidLifetime_NegativeWithMax", func(t *testing.T) {
        maxLifetime := 90
        lifetime := -1

        _, _, err := CreateUserTokenNonInteractive(pool, testUsername, nil, lifetime, maxLifetime, nil)
        if err == nil {
            t.Error("Expected error for negative lifetime with max configured")
        }
    })

    t.Run("InvalidUser", func(t *testing.T) {
        _, _, err := CreateUserTokenNonInteractive(pool, "nonexistent_user", nil, 30, 90, nil)
        if err == nil {
            t.Error("Expected error for nonexistent user")
        }
        if !strings.Contains(err.Error(), "does not exist") {
            t.Errorf("Expected error about nonexistent user, got: %v", err)
        }
    })
}

// TestListUserTokens tests listing user tokens
func TestListUserTokens(t *testing.T) {
    pool := skipIfNoDatabase(t)
    defer pool.Close()

    ctx := context.Background()

    // Create a test user
    testUsername := "testuser_token_list"
    testEmail := "testlist@example.com"
    testFullName := "Test List User"
    testPassword := "TestPassword123!"

    // Clean up any existing test user
    _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", testUsername) //nolint:errcheck // Cleanup code, failure is acceptable

    _, err := CreateUserNonInteractive(pool, testUsername, testEmail, testFullName, testPassword, false, nil)
    if err != nil {
        t.Fatalf("Failed to create test user: %v", err)
    }
    defer func() {
        _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", testUsername) //nolint:errcheck // Cleanup code, failure is acceptable
    }()

    // Create a few tokens
    name1 := "token-one"
    name2 := "token-two"
    note := "Test note"

    _, _, err = CreateUserTokenNonInteractive(pool, testUsername, &name1, 30, 90, nil)
    if err != nil {
        t.Fatalf("Failed to create first token: %v", err)
    }

    _, _, err = CreateUserTokenNonInteractive(pool, testUsername, &name2, 0, 0, &note)
    if err != nil {
        t.Fatalf("Failed to create second token: %v", err)
    }

    // List tokens
    tokens, err := ListUserTokens(pool, testUsername)
    if err != nil {
        t.Fatalf("Failed to list tokens: %v", err)
    }

    if len(tokens) < 2 {
        t.Errorf("Expected at least 2 tokens, got %d", len(tokens))
    }

    // Verify token structure
    for _, token := range tokens {
        if token["id"] == nil {
            t.Error("Token missing id field")
        }
        if token["created_at"] == nil {
            t.Error("Token missing created_at field")
        }
        // is_expired should be present
        if _, ok := token["is_expired"]; !ok {
            t.Error("Token missing is_expired field")
        }
    }

    // Test with nonexistent user
    _, err = ListUserTokens(pool, "nonexistent_user")
    if err == nil {
        t.Error("Expected error for nonexistent user")
    }
}

// TestDeleteUserTokenNonInteractive tests deleting user tokens
func TestDeleteUserTokenNonInteractive(t *testing.T) {
    pool := skipIfNoDatabase(t)
    defer pool.Close()

    ctx := context.Background()

    // Create a test user
    testUsername := "testuser_token_delete"
    testEmail := "testdelete@example.com"
    testFullName := "Test Delete User"
    testPassword := "TestPassword123!"

    // Clean up any existing test user
    _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", testUsername) //nolint:errcheck // Cleanup code, failure is acceptable

    _, err := CreateUserNonInteractive(pool, testUsername, testEmail, testFullName, testPassword, false, nil)
    if err != nil {
        t.Fatalf("Failed to create test user: %v", err)
    }
    defer func() {
        _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", testUsername) //nolint:errcheck // Cleanup code, failure is acceptable
    }()

    // Create a token
    name := "token-to-delete"
    _, _, err = CreateUserTokenNonInteractive(pool, testUsername, &name, 30, 90, nil)
    if err != nil {
        t.Fatalf("Failed to create token: %v", err)
    }

    // Get the token ID
    tokens, err := ListUserTokens(pool, testUsername)
    if err != nil {
        t.Fatalf("Failed to list tokens: %v", err)
    }
    if len(tokens) == 0 {
        t.Fatal("No tokens found")
    }

    tokenID := tokens[0]["id"].(int) //nolint:errcheck // Test code, type assertion expected to succeed

    // Delete the token
    message, err := DeleteUserTokenNonInteractive(pool, testUsername, tokenID)
    if err != nil {
        t.Errorf("Failed to delete token: %v", err)
    }
    if !strings.Contains(message, "deleted successfully") {
        t.Errorf("Expected success message, got: %s", message)
    }

    // Verify token is deleted
    tokens, err = ListUserTokens(pool, testUsername)
    if err != nil {
        t.Fatalf("Failed to list tokens after deletion: %v", err)
    }
    for _, token := range tokens {
        if token["id"].(int) == tokenID { //nolint:errcheck // Test code, type assertion expected to succeed
            t.Error("Token was not deleted")
        }
    }

    // Try to delete nonexistent token
    _, err = DeleteUserTokenNonInteractive(pool, testUsername, 999999)
    if err == nil {
        t.Error("Expected error for nonexistent token")
    }

    // Try to delete with nonexistent user
    _, err = DeleteUserTokenNonInteractive(pool, "nonexistent_user", tokenID)
    if err == nil {
        t.Error("Expected error for nonexistent user")
    }
}

// TestUserTokenCascadeDelete tests that user tokens are deleted when user is deleted
func TestUserTokenCascadeDelete(t *testing.T) {
    pool := skipIfNoDatabase(t)
    defer pool.Close()

    ctx := context.Background()

    // Create a test user
    testUsername := "testuser_cascade"
    testEmail := "testcascade@example.com"
    testFullName := "Test Cascade User"
    testPassword := "TestPassword123!"

    // Clean up any existing test user
    _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", testUsername) //nolint:errcheck // Cleanup code, failure is acceptable

    _, err := CreateUserNonInteractive(pool, testUsername, testEmail, testFullName, testPassword, false, nil)
    if err != nil {
        t.Fatalf("Failed to create test user: %v", err)
    }

    // Create a token
    name := "cascade-token"
    _, _, err = CreateUserTokenNonInteractive(pool, testUsername, &name, 30, 90, nil)
    if err != nil {
        t.Fatalf("Failed to create token: %v", err)
    }

    // Verify token exists
    tokens, err := ListUserTokens(pool, testUsername)
    if err != nil {
        t.Fatalf("Failed to list tokens: %v", err)
    }
    if len(tokens) == 0 {
        t.Fatal("No tokens found")
    }

    tokenID := tokens[0]["id"].(int) //nolint:errcheck // Test code, type assertion expected to succeed

    // Delete the user
    _, err = DeleteUserNonInteractive(pool, testUsername)
    if err != nil {
        t.Fatalf("Failed to delete user: %v", err)
    }

    // Verify token was deleted via cascade
    var count int
    err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_tokens WHERE id = $1", tokenID).Scan(&count)
    if err != nil {
        t.Fatalf("Failed to check token count: %v", err)
    }
    if count != 0 {
        t.Error("Token was not cascade deleted when user was deleted")
    }
}

// TestUserTokenExpiration tests token expiration logic
func TestUserTokenExpiration(t *testing.T) {
    pool := skipIfNoDatabase(t)
    defer pool.Close()

    ctx := context.Background()

    // Create a test user
    testUsername := "testuser_expiry"
    testEmail := "testexpiry@example.com"
    testFullName := "Test Expiry User"
    testPassword := "TestPassword123!"

    // Clean up any existing test user
    _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", testUsername) //nolint:errcheck // Cleanup code, failure is acceptable

    _, err := CreateUserNonInteractive(pool, testUsername, testEmail, testFullName, testPassword, false, nil)
    if err != nil {
        t.Fatalf("Failed to create test user: %v", err)
    }
    defer func() {
        _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", testUsername) //nolint:errcheck // Cleanup code, failure is acceptable
    }()

    // Create an expired token by manually setting expires_at in the past
    name := "expired-token"
    _, _, err = CreateUserTokenNonInteractive(pool, testUsername, &name, 1, 90, nil)
    if err != nil {
        t.Fatalf("Failed to create token: %v", err)
    }

    // Get the token ID
    tokens, err := ListUserTokens(pool, testUsername)
    if err != nil {
        t.Fatalf("Failed to list tokens: %v", err)
    }
    if len(tokens) == 0 {
        t.Fatal("No tokens found")
    }

    tokenID := tokens[0]["id"].(int) //nolint:errcheck // Test code, type assertion expected to succeed

    // Manually set the expiration to the past
    pastTime := time.Now().Add(-1 * time.Hour)
    _, err = pool.Exec(ctx, "UPDATE user_tokens SET expires_at = $1 WHERE id = $2", pastTime, tokenID)
    if err != nil {
        t.Fatalf("Failed to set expiration: %v", err)
    }

    // List tokens again and verify is_expired is true
    tokens, err = ListUserTokens(pool, testUsername)
    if err != nil {
        t.Fatalf("Failed to list tokens: %v", err)
    }

    found := false
    for _, token := range tokens {
        if token["id"].(int) == tokenID { //nolint:errcheck // Test code, type assertion expected to succeed
            found = true
            if expired, ok := token["is_expired"].(bool); !ok || !expired {
                t.Error("Expected token to be marked as expired")
            }
        }
    }
    if !found {
        t.Error("Token not found in list")
    }
}

// TestUserTokenPermissions_RegularUser tests token operations for regular users
func TestUserTokenPermissions_RegularUser(t *testing.T) {
    pool := skipIfNoDatabase(t)
    defer pool.Close()

    ctx := context.Background()

    // Create two regular (non-superuser) test users
    user1Username := "regular_user_1"
    user1Email := "user1@example.com"
    user2Username := "regular_user_2"
    user2Email := "user2@example.com"
    testPassword := "TestPassword123!"

    // Clean up
    _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username IN ($1, $2)", user1Username, user2Username) //nolint:errcheck // Cleanup code, failure is acceptable

    // Create both users as regular users (not superusers)
    _, err := CreateUserNonInteractive(pool, user1Username, user1Email, "User One", testPassword, false, nil)
    if err != nil {
        t.Fatalf("Failed to create user1: %v", err)
    }
    defer func() {
        _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", user1Username) //nolint:errcheck // Cleanup code, failure is acceptable
    }()

    _, err = CreateUserNonInteractive(pool, user2Username, user2Email, "User Two", testPassword, false, nil)
    if err != nil {
        t.Fatalf("Failed to create user2: %v", err)
    }
    defer func() {
        _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", user2Username) //nolint:errcheck // Cleanup code, failure is acceptable
    }()

    t.Run("RegularUser_CanCreateOwnToken", func(t *testing.T) {
        name := "user1-token"
        _, token, err := CreateUserTokenNonInteractive(pool, user1Username, &name, 30, 90, nil)
        if err != nil {
            t.Errorf("Regular user should be able to create their own token: %v", err)
        }
        if token == "" {
            t.Error("Expected non-empty token")
        }
    })

    t.Run("RegularUser_CanListOwnTokens", func(t *testing.T) {
        tokens, err := ListUserTokens(pool, user1Username)
        if err != nil {
            t.Errorf("Regular user should be able to list their own tokens: %v", err)
        }
        if len(tokens) == 0 {
            t.Error("Expected at least one token")
        }
    })

    t.Run("RegularUser_CanDeleteOwnToken", func(t *testing.T) {
        // Get token ID
        tokens, err := ListUserTokens(pool, user1Username)
        if err != nil {
            t.Fatalf("Failed to list tokens: %v", err)
        }
        if len(tokens) == 0 {
            t.Fatal("No tokens to delete")
        }

        tokenID := tokens[0]["id"].(int) //nolint:errcheck // Test code, type assertion expected to succeed
        _, err = DeleteUserTokenNonInteractive(pool, user1Username, tokenID)
        if err != nil {
            t.Errorf("Regular user should be able to delete their own token: %v", err)
        }
    })
}

// TestUserTokenPermissions_Superuser tests token operations for superusers
func TestUserTokenPermissions_Superuser(t *testing.T) {
    pool := skipIfNoDatabase(t)
    defer pool.Close()

    ctx := context.Background()

    // Create a superuser and a regular user
    superUsername := "super_admin"
    superEmail := "admin@example.com"
    regularUsername := "regular_user_priv"
    regularEmail := "regularpriv@example.com"
    testPassword := "TestPassword123!"

    // Clean up
    _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username IN ($1, $2)", superUsername, regularUsername) //nolint:errcheck // Cleanup code, failure is acceptable

    // Create superuser
    _, err := CreateUserNonInteractive(pool, superUsername, superEmail, "Super Admin", testPassword, true, nil)
    if err != nil {
        t.Fatalf("Failed to create superuser: %v", err)
    }
    defer func() {
        _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", superUsername) //nolint:errcheck // Cleanup code, failure is acceptable
    }()

    // Create regular user
    _, err = CreateUserNonInteractive(pool, regularUsername, regularEmail, "Regular User", testPassword, false, nil)
    if err != nil {
        t.Fatalf("Failed to create regular user: %v", err)
    }
    defer func() {
        _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", regularUsername) //nolint:errcheck // Cleanup code, failure is acceptable
    }()

    t.Run("Superuser_CanCreateTokenForOtherUser", func(t *testing.T) {
        name := "admin-created-token"
        note := "Created by admin for regular user"
        _, token, err := CreateUserTokenNonInteractive(pool, regularUsername, &name, 7, 90, &note)
        if err != nil {
            t.Errorf("Superuser should be able to create token for any user: %v", err)
        }
        if token == "" {
            t.Error("Expected non-empty token")
        }

        // Verify the token was created for the regular user
        tokens, err := ListUserTokens(pool, regularUsername)
        if err != nil {
            t.Fatalf("Failed to list tokens: %v", err)
        }

        found := false
        for _, tok := range tokens {
            if n, ok := tok["name"].(string); ok && n == name {
                found = true
                if nt, ok := tok["note"].(string); ok {
                    if nt != note {
                        t.Errorf("Expected note '%s', got '%s'", note, nt)
                    }
                }
            }
        }
        if !found {
            t.Error("Token created by superuser not found for regular user")
        }
    })

    t.Run("Superuser_CanListOtherUsersTokens", func(t *testing.T) {
        tokens, err := ListUserTokens(pool, regularUsername)
        if err != nil {
            t.Errorf("Superuser should be able to list any user's tokens: %v", err)
        }
        if len(tokens) == 0 {
            t.Error("Expected at least one token for regular user")
        }
    })

    t.Run("Superuser_CanDeleteOtherUsersTokens", func(t *testing.T) {
        tokens, err := ListUserTokens(pool, regularUsername)
        if err != nil {
            t.Fatalf("Failed to list tokens: %v", err)
        }
        if len(tokens) == 0 {
            t.Fatal("No tokens to delete")
        }

        tokenID := tokens[0]["id"].(int) //nolint:errcheck // Test code, type assertion expected to succeed
        _, err = DeleteUserTokenNonInteractive(pool, regularUsername, tokenID)
        if err != nil {
            t.Errorf("Superuser should be able to delete any user's tokens: %v", err)
        }

        // Verify deletion
        tokensAfter, err := ListUserTokens(pool, regularUsername)
        if err != nil {
            t.Fatalf("Failed to list tokens after deletion: %v", err)
        }

        for _, tok := range tokensAfter {
            if tok["id"].(int) == tokenID { //nolint:errcheck // Test code, type assertion expected to succeed
                t.Error("Token should have been deleted")
            }
        }
    })

    t.Run("Superuser_TokenInheritsSuperuserFlag", func(t *testing.T) {
        // Create a token for the superuser
        name := "super-token"
        _, token, err := CreateUserTokenNonInteractive(pool, superUsername, &name, 30, 90, nil)
        if err != nil {
            t.Fatalf("Failed to create superuser token: %v", err)
        }

        // Hash the token and verify it has superuser privileges
        tokenHash := HashPassword(token)
        var username string
        var isSuperuser bool
        err = pool.QueryRow(ctx, `
            SELECT ua.username, ua.is_superuser
            FROM user_tokens ut
            JOIN user_accounts ua ON ut.user_id = ua.id
            WHERE ut.token_hash = $1
        `, tokenHash).Scan(&username, &isSuperuser)

        if err != nil {
            t.Fatalf("Failed to query token: %v", err)
        }

        if username != superUsername {
            t.Errorf("Expected token owner to be '%s', got '%s'", superUsername, username)
        }

        if !isSuperuser {
            t.Error("Token for superuser should inherit superuser flag")
        }
    })

    t.Run("RegularUser_TokenDoesNotHaveSuperuserFlag", func(t *testing.T) {
        // Create a token for the regular user
        name := "regular-token"
        _, token, err := CreateUserTokenNonInteractive(pool, regularUsername, &name, 30, 90, nil)
        if err != nil {
            t.Fatalf("Failed to create regular user token: %v", err)
        }

        // Hash the token and verify it doesn't have superuser privileges
        tokenHash := HashPassword(token)
        var username string
        var isSuperuser bool
        err = pool.QueryRow(ctx, `
            SELECT ua.username, ua.is_superuser
            FROM user_tokens ut
            JOIN user_accounts ua ON ut.user_id = ua.id
            WHERE ut.token_hash = $1
        `, tokenHash).Scan(&username, &isSuperuser)

        if err != nil {
            t.Fatalf("Failed to query token: %v", err)
        }

        if username != regularUsername {
            t.Errorf("Expected token owner to be '%s', got '%s'", regularUsername, username)
        }

        if isSuperuser {
            t.Error("Token for regular user should not have superuser flag")
        }
    })
}

// TestUserTokenWithChangingPermissions tests that token permissions change with user permissions
func TestUserTokenWithChangingPermissions(t *testing.T) {
    pool := skipIfNoDatabase(t)
    defer pool.Close()

    ctx := context.Background()

    // Create a regular user
    testUsername := "permission_test_user"
    testEmail := "permtest@example.com"
    testPassword := "TestPassword123!"

    // Clean up
    _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", testUsername) //nolint:errcheck // Cleanup code, failure is acceptable

    _, err := CreateUserNonInteractive(pool, testUsername, testEmail, "Permission Test", testPassword, false, nil)
    if err != nil {
        t.Fatalf("Failed to create test user: %v", err)
    }
    defer func() {
        _, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username = $1", testUsername) //nolint:errcheck // Cleanup code, failure is acceptable
    }()

    // Create a token
    name := "dynamic-permission-token"
    _, token, err := CreateUserTokenNonInteractive(pool, testUsername, &name, 30, 90, nil)
    if err != nil {
        t.Fatalf("Failed to create token: %v", err)
    }

    tokenHash := HashPassword(token)

    // Verify token has no superuser privileges
    var isSuperuser bool
    err = pool.QueryRow(ctx, `
        SELECT ua.is_superuser
        FROM user_tokens ut
        JOIN user_accounts ua ON ut.user_id = ua.id
        WHERE ut.token_hash = $1
    `, tokenHash).Scan(&isSuperuser)
    if err != nil {
        t.Fatalf("Failed to query token: %v", err)
    }
    if isSuperuser {
        t.Error("Token should not have superuser privileges initially")
    }

    // Promote user to superuser
    _, err = pool.Exec(ctx, "UPDATE user_accounts SET is_superuser = TRUE WHERE username = $1", testUsername)
    if err != nil {
        t.Fatalf("Failed to update user: %v", err)
    }

    // Verify token now has superuser privileges
    err = pool.QueryRow(ctx, `
        SELECT ua.is_superuser
        FROM user_tokens ut
        JOIN user_accounts ua ON ut.user_id = ua.id
        WHERE ut.token_hash = $1
    `, tokenHash).Scan(&isSuperuser)
    if err != nil {
        t.Fatalf("Failed to query token after promotion: %v", err)
    }
    if !isSuperuser {
        t.Error("Token should inherit superuser privileges after user promotion")
    }

    // Demote user back to regular user
    _, err = pool.Exec(ctx, "UPDATE user_accounts SET is_superuser = FALSE WHERE username = $1", testUsername)
    if err != nil {
        t.Fatalf("Failed to demote user: %v", err)
    }

    // Verify token no longer has superuser privileges
    err = pool.QueryRow(ctx, `
        SELECT ua.is_superuser
        FROM user_tokens ut
        JOIN user_accounts ua ON ut.user_id = ua.id
        WHERE ut.token_hash = $1
    `, tokenHash).Scan(&isSuperuser)
    if err != nil {
        t.Fatalf("Failed to query token after demotion: %v", err)
    }
    if isSuperuser {
        t.Error("Token should lose superuser privileges after user demotion")
    }
}
