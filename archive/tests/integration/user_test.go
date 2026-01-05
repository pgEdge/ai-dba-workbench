/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package integration

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/pgedge/ai-workbench/tests/testutil"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

const (
    testServerPort = 18080
    testServerURL  = "http://localhost:18080"
)

// TestEnvironment holds all test infrastructure
type TestEnvironment struct {
    DB        *testutil.TestDatabase
    Collector *testutil.Service
    Server    *testutil.Service
    CLI       *testutil.CLIClient
    Config    string
    AdminToken string
}

// SetupTestEnvironment initializes the test environment
func SetupTestEnvironment(t *testing.T) *TestEnvironment {
    // Create logs directory
    logsDir := filepath.Join("..", "logs")
    if err := os.MkdirAll(logsDir, 0755); err != nil {
        t.Fatalf("Failed to create logs directory: %v", err)
    }

    // Create test database
    db, err := testutil.NewTestDatabase()
    require.NoError(t, err, "Failed to create test database")

    // Run schema migrations
    err = runSchemaMigrations(db)
    require.NoError(t, err, "Failed to run schema migrations")

    // Create admin user in database
    adminToken, err := createAdminUser(db)
    require.NoError(t, err, "Failed to create admin user")

    // Create test configuration
    configPath, err := testutil.CreateTestConfig(db.Name)
    require.NoError(t, err, "Failed to create test config")

    // Note: We don't start the collector for user management tests
    // The collector is for monitoring PostgreSQL servers and would conflict
    // with our manual schema setup. For user management, we only need the MCP server.

    // Start MCP server
    server, err := testutil.StartMCPServer(configPath, testServerPort)
    require.NoError(t, err, "Failed to start MCP server")

    // Create CLI client
    cli, err := testutil.NewCLIClient(testServerURL)
    require.NoError(t, err, "Failed to create CLI client")

    // Set admin token
    cli.SetToken(adminToken)

    // Wait for services to be fully ready
    time.Sleep(3 * time.Second)

    // Verify server is responding
    err = cli.Ping()
    require.NoError(t, err, "Server ping failed")

    return &TestEnvironment{
        DB:         db,
        Collector:  nil, // Not needed for user management tests
        Server:     server,
        CLI:        cli,
        Config:     configPath,
        AdminToken: adminToken,
    }
}

// TeardownTestEnvironment cleans up the test environment
func (env *TestEnvironment) TeardownTestEnvironment(t *testing.T) {
    if env.Server != nil {
        if err := env.Server.Stop(); err != nil {
            t.Logf("Warning: Failed to stop MCP server: %v", err)
        }
    }

    // Collector is not started for user management tests
    // if env.Collector != nil {
    //     if err := env.Collector.Stop(); err != nil {
    //         t.Logf("Warning: Failed to stop collector: %v", err)
    //     }
    // }

    if env.Config != "" {
        if err := testutil.CleanupTestConfig(env.Config); err != nil {
            t.Logf("Warning: Failed to cleanup config: %v", err)
        }
    }

    if env.DB != nil {
        if err := env.DB.Close(); err != nil {
            t.Logf("Warning: Failed to close database: %v", err)
        }
    }
}

// runSchemaMigrations runs database schema migrations
func runSchemaMigrations(db *testutil.TestDatabase) error {
    // We need to run the schema migrations from the collector package
    // For now, we'll run SQL directly
    ctx := context.Background()

    // Create schema_version table
    _, err := db.Pool.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS schema_version (
            version INTEGER PRIMARY KEY,
            description TEXT NOT NULL,
            applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
        )
    `)
    if err != nil {
        return fmt.Errorf("failed to create schema_version table: %w", err)
    }

    // Create user_accounts table
    _, err = db.Pool.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS user_accounts (
            id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
            username TEXT NOT NULL UNIQUE,
            email TEXT NOT NULL,
            is_superuser BOOLEAN NOT NULL DEFAULT FALSE,
            full_name TEXT NOT NULL,
            password_hash TEXT NOT NULL,
            password_expiry TIMESTAMP,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            CONSTRAINT chk_username_not_empty CHECK (username <> ''),
            CONSTRAINT chk_email_not_empty CHECK (email <> ''),
            CONSTRAINT chk_password_hash_not_empty CHECK (password_hash <> '')
        )
    `)
    if err != nil {
        return fmt.Errorf("failed to create user_accounts table: %w", err)
    }

    // Create service_tokens table
    _, err = db.Pool.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS service_tokens (
            id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
            name TEXT NOT NULL UNIQUE,
            token_hash TEXT NOT NULL UNIQUE,
            is_superuser BOOLEAN NOT NULL DEFAULT FALSE,
            note TEXT,
            expires_at TIMESTAMP,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            CONSTRAINT chk_name_not_empty CHECK (name <> ''),
            CONSTRAINT chk_token_hash_not_empty CHECK (token_hash <> '')
        )
    `)
    if err != nil {
        return fmt.Errorf("failed to create service_tokens table: %w", err)
    }

    // Create user_tokens table
    _, err = db.Pool.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS user_tokens (
            id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
            user_id INTEGER NOT NULL,
            token_hash TEXT NOT NULL UNIQUE,
            expires_at TIMESTAMP NOT NULL,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            CONSTRAINT chk_token_hash_not_empty CHECK (token_hash <> ''),
            CONSTRAINT chk_expires_at_future CHECK (expires_at > created_at)
        );

        ALTER TABLE user_tokens
            ADD CONSTRAINT fk_user_tokens_user_id
            FOREIGN KEY (user_id) REFERENCES user_accounts(id) ON DELETE CASCADE
    `)
    if err != nil {
        return fmt.Errorf("failed to create user_tokens table: %w", err)
    }

    // Create connections table
    _, err = db.Pool.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS connections (
            id SERIAL PRIMARY KEY,
            owner_username VARCHAR(255),
            owner_token VARCHAR(255),
            is_shared BOOLEAN NOT NULL DEFAULT FALSE,
            is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
            name VARCHAR(255) NOT NULL,
            host VARCHAR(255) NOT NULL,
            hostaddr VARCHAR(255),
            port INTEGER NOT NULL DEFAULT 5432,
            database_name VARCHAR(255) NOT NULL,
            username VARCHAR(255) NOT NULL,
            password_encrypted TEXT,
            sslmode VARCHAR(50),
            sslcert TEXT,
            sslkey TEXT,
            sslrootcert TEXT,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            CONSTRAINT chk_owner CHECK (
                (owner_username IS NOT NULL AND owner_token IS NULL) OR
                (owner_username IS NULL AND owner_token IS NOT NULL)
            ),
            CONSTRAINT chk_port CHECK (port > 0 AND port <= 65535)
        );

        ALTER TABLE connections
            ADD CONSTRAINT fk_connections_owner_username
            FOREIGN KEY (owner_username) REFERENCES user_accounts(username)
            ON UPDATE CASCADE ON DELETE RESTRICT;

        ALTER TABLE connections
            ADD CONSTRAINT fk_connections_owner_token
            FOREIGN KEY (owner_token) REFERENCES service_tokens(name)
            ON UPDATE CASCADE ON DELETE RESTRICT
    `)
    if err != nil {
        return fmt.Errorf("failed to create connections table: %w", err)
    }

    // Create user_sessions table
    _, err = db.Pool.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS user_sessions (
            session_token TEXT PRIMARY KEY,
            username TEXT NOT NULL,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            expires_at TIMESTAMP NOT NULL,
            last_used_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            CONSTRAINT fk_username FOREIGN KEY (username)
                REFERENCES user_accounts(username) ON DELETE CASCADE
        )
    `)
    if err != nil {
        return fmt.Errorf("failed to create user_sessions table: %w", err)
    }

    // Create Migration 6 tables: User Groups and Privilege Management
    _, err = db.Pool.Exec(ctx, `
        -- User Groups
        CREATE TABLE IF NOT EXISTS user_groups (
            id SERIAL PRIMARY KEY,
            name VARCHAR(255) NOT NULL UNIQUE,
            description TEXT,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
        );

        -- Group Memberships
        CREATE TABLE IF NOT EXISTS group_memberships (
            id SERIAL PRIMARY KEY,
            parent_group_id INTEGER NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
            member_user_id INTEGER REFERENCES user_accounts(id) ON DELETE CASCADE,
            member_group_id INTEGER REFERENCES user_groups(id) ON DELETE CASCADE,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

            CONSTRAINT member_type_check CHECK (
                (member_user_id IS NOT NULL AND member_group_id IS NULL) OR
                (member_user_id IS NULL AND member_group_id IS NOT NULL)
            ),
            CONSTRAINT unique_user_membership UNIQUE (parent_group_id, member_user_id),
            CONSTRAINT unique_group_membership UNIQUE (parent_group_id, member_group_id),
            CONSTRAINT no_self_reference CHECK (parent_group_id != member_group_id)
        );

        CREATE INDEX IF NOT EXISTS idx_group_memberships_parent ON group_memberships(parent_group_id);
        CREATE INDEX IF NOT EXISTS idx_group_memberships_user ON group_memberships(member_user_id);
        CREATE INDEX IF NOT EXISTS idx_group_memberships_group ON group_memberships(member_group_id);

        -- Connection Privileges
        CREATE TABLE IF NOT EXISTS connection_privileges (
            id SERIAL PRIMARY KEY,
            group_id INTEGER NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
            connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
            access_level VARCHAR(20) NOT NULL CHECK (access_level IN ('read', 'read_write')),
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            CONSTRAINT unique_group_connection UNIQUE (group_id, connection_id)
        );

        CREATE INDEX IF NOT EXISTS idx_connection_privileges_group ON connection_privileges(group_id);
        CREATE INDEX IF NOT EXISTS idx_connection_privileges_connection ON connection_privileges(connection_id);

        -- MCP Privilege Identifiers
        CREATE TABLE IF NOT EXISTS mcp_privilege_identifiers (
            id SERIAL PRIMARY KEY,
            identifier VARCHAR(255) NOT NULL UNIQUE,
            item_type VARCHAR(20) NOT NULL CHECK (item_type IN ('tool', 'resource', 'prompt')),
            description TEXT,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
        );

        CREATE INDEX IF NOT EXISTS idx_mcp_privilege_identifiers_type ON mcp_privilege_identifiers(item_type);

        -- Group MCP Privileges
        CREATE TABLE IF NOT EXISTS group_mcp_privileges (
            id SERIAL PRIMARY KEY,
            group_id INTEGER NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
            privilege_identifier_id INTEGER NOT NULL REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            CONSTRAINT unique_group_privilege UNIQUE (group_id, privilege_identifier_id)
        );

        CREATE INDEX IF NOT EXISTS idx_group_mcp_privileges_group ON group_mcp_privileges(group_id);
        CREATE INDEX IF NOT EXISTS idx_group_mcp_privileges_privilege ON group_mcp_privileges(privilege_identifier_id);

        -- Token Connection Scope
        CREATE TABLE IF NOT EXISTS token_connection_scope (
            id SERIAL PRIMARY KEY,
            token_id INTEGER NOT NULL,
            token_type VARCHAR(20) NOT NULL CHECK (token_type IN ('user', 'service')),
            connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            CONSTRAINT unique_token_connection UNIQUE (token_id, token_type, connection_id)
        );

        CREATE INDEX IF NOT EXISTS idx_token_connection_scope_token ON token_connection_scope(token_id, token_type);
        CREATE INDEX IF NOT EXISTS idx_token_connection_scope_connection ON token_connection_scope(connection_id);

        -- Token MCP Scope
        CREATE TABLE IF NOT EXISTS token_mcp_scope (
            id SERIAL PRIMARY KEY,
            token_id INTEGER NOT NULL,
            token_type VARCHAR(20) NOT NULL CHECK (token_type IN ('user', 'service')),
            privilege_identifier_id INTEGER NOT NULL REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            CONSTRAINT unique_token_privilege UNIQUE (token_id, token_type, privilege_identifier_id)
        );

        CREATE INDEX IF NOT EXISTS idx_token_mcp_scope_token ON token_mcp_scope(token_id, token_type);
        CREATE INDEX IF NOT EXISTS idx_token_mcp_scope_privilege ON token_mcp_scope(privilege_identifier_id)
    `)
    if err != nil {
        return fmt.Errorf("failed to create Migration 6 tables: %w", err)
    }

    return nil
}

// createAdminUser creates an admin user and returns a session token
func createAdminUser(db *testutil.TestDatabase) (string, error) {
    ctx := context.Background()

    // Hash password (using SHA256)
    passwordHash := "8c6976e5b5410415bde908bd4dee15dfb167a9c873fc4bb8a81f6f2ab448a918" // "admin"

    // Insert admin user
    var userID int
    err := db.Pool.QueryRow(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash, is_superuser)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id
    `, "admin", "admin@test.com", "Test Admin", passwordHash, true).Scan(&userID)
    if err != nil {
        return "", fmt.Errorf("failed to create admin user: %w", err)
    }

    // Generate session token
    token := fmt.Sprintf("test-admin-token-%d", time.Now().Unix())

    // Create session in user_sessions table
    expiresAt := time.Now().Add(24 * time.Hour)
    _, err = db.Pool.Exec(ctx, `
        INSERT INTO user_sessions (session_token, username, expires_at)
        VALUES ($1, $2, $3)
    `, token, "admin", expiresAt)
    if err != nil {
        return "", fmt.Errorf("failed to create session token: %w", err)
    }

    return token, nil
}

// TestUserCRUD tests create, read, update, and delete operations for users
func TestUserCRUD(t *testing.T) {
    if os.Getenv("SKIP_INTEGRATION_TESTS") == "1" {
        t.Skip("Skipping integration tests")
    }

    env := SetupTestEnvironment(t)
    defer env.TeardownTestEnvironment(t)

    // Test 1: Create a new user
    t.Run("CreateUser", func(t *testing.T) {
        input := map[string]interface{}{
            "username":       "testuser1",
            "email":          "testuser1@test.com",
            "fullName":       "Test User One",
            "password":       "password123",
            "isSuperuser":    false,
            "passwordExpiry": nil,
        }

        result, err := env.CLI.RunTool("create_user", input)
        require.NoError(t, err, "Failed to create user")

        // Verify result contains success message
        content, ok := result["content"].([]interface{})
        require.True(t, ok, "Expected content array in result")
        require.Greater(t, len(content), 0, "Expected non-empty content")

        firstContent, ok := content[0].(map[string]interface{})
        require.True(t, ok, "Expected map in first content")

        text, ok := firstContent["text"].(string)
        require.True(t, ok, "Expected text in first content")
        assert.Contains(t, text, "created successfully", "Expected success message")
    })

    // Test 2: List users
    t.Run("ListUsers", func(t *testing.T) {
        result, err := env.CLI.ReadResource("ai-workbench://users")
        require.NoError(t, err, "Failed to list users")

        contents, ok := result["contents"].([]interface{})
        require.True(t, ok, "Expected contents array in result")
        require.Greater(t, len(contents), 0, "Expected non-empty contents")

        // Convert all user text entries to a single string for easier checking
        var allUsers string
        for _, item := range contents {
            itemMap, ok := item.(map[string]interface{})
            require.True(t, ok, "Expected map in contents item")
            text, ok := itemMap["text"].(string)
            require.True(t, ok, "Expected text in contents item")
            allUsers += text
        }

        // Should contain both admin and testuser1
        assert.Contains(t, allUsers, "admin", "Expected admin user in list")
        assert.Contains(t, allUsers, "testuser1", "Expected testuser1 in list")
    })

    // Test 3: Update user
    t.Run("UpdateUser", func(t *testing.T) {
        input := map[string]interface{}{
            "username": "testuser1",
            "fullName": "Updated Test User",
            "email":    "updated@test.com",
        }

        result, err := env.CLI.RunTool("update_user", input)
        require.NoError(t, err, "Failed to update user")

        content, ok := result["content"].([]interface{})
        require.True(t, ok, "Expected content array in result")
        require.Greater(t, len(content), 0, "Expected non-empty content")

        firstContent, ok := content[0].(map[string]interface{})
        require.True(t, ok, "Expected map in first content")

        text, ok := firstContent["text"].(string)
        require.True(t, ok, "Expected text in first content")
        assert.Contains(t, text, "updated successfully", "Expected success message")
    })

    // Test 4: Verify update by listing
    t.Run("VerifyUpdate", func(t *testing.T) {
        result, err := env.CLI.ReadResource("ai-workbench://users")
        require.NoError(t, err, "Failed to list users")

        contents, ok := result["contents"].([]interface{})
        require.True(t, ok, "Expected contents array in result")

        var allUsers string
        for _, item := range contents {
            itemMap, ok := item.(map[string]interface{})
            require.True(t, ok, "Expected map in contents item")
            text, ok := itemMap["text"].(string)
            require.True(t, ok, "Expected text in contents item")
            allUsers += text
        }

        assert.Contains(t, allUsers, "Updated Test User", "Expected updated full name")
    })

    // Test 5: Delete user
    t.Run("DeleteUser", func(t *testing.T) {
        input := map[string]interface{}{
            "username": "testuser1",
        }

        result, err := env.CLI.RunTool("delete_user", input)
        require.NoError(t, err, "Failed to delete user")

        content, ok := result["content"].([]interface{})
        require.True(t, ok, "Expected content array in result")

        firstContent, ok := content[0].(map[string]interface{})
        require.True(t, ok, "Expected map in first content")

        text, ok := firstContent["text"].(string)
        require.True(t, ok, "Expected text in first content")
        assert.Contains(t, text, "deleted successfully", "Expected success message")
    })

    // Test 6: Verify deletion
    t.Run("VerifyDeletion", func(t *testing.T) {
        result, err := env.CLI.ReadResource("ai-workbench://users")
        require.NoError(t, err, "Failed to list users")

        contents, ok := result["contents"].([]interface{})
        require.True(t, ok, "Expected contents array in result")

        var allUsers string
        for _, item := range contents {
            itemMap, ok := item.(map[string]interface{})
            require.True(t, ok, "Expected map in contents item")
            text, ok := itemMap["text"].(string)
            require.True(t, ok, "Expected text in contents item")
            allUsers += text
        }

        assert.NotContains(t, allUsers, "testuser1", "Expected testuser1 to be deleted")
    })
}

// TestPasswordExpiry tests password expiry functionality
func TestPasswordExpiry(t *testing.T) {
    if os.Getenv("SKIP_INTEGRATION_TESTS") == "1" {
        t.Skip("Skipping integration tests")
    }

    env := SetupTestEnvironment(t)
    defer env.TeardownTestEnvironment(t)

    // Create user with expired password
    t.Run("CreateUserWithExpiredPassword", func(t *testing.T) {
        expiredTime := time.Now().Add(-1 * time.Hour).Format("2006-01-02")

        input := map[string]interface{}{
            "username":       "expireduser",
            "email":          "expired@test.com",
            "fullName":       "Expired User",
            "password":       "password123",
            "isSuperuser":    false,
            "passwordExpiry": expiredTime,
        }

        result, err := env.CLI.RunTool("create_user", input)
        require.NoError(t, err, "Failed to create user with expired password")

        content, ok := result["content"].([]interface{})
        require.True(t, ok, "Expected content array in result")

        firstContent, ok := content[0].(map[string]interface{})
        require.True(t, ok, "Expected map in first content")

        text, ok := firstContent["text"].(string)
        require.True(t, ok, "Expected text in first content")
        assert.Contains(t, text, "created successfully", "Expected success message")
    })

    // Try to authenticate with expired password
    t.Run("AuthenticateWithExpiredPassword", func(t *testing.T) {
        // Create a new CLI client without token
        cli, err := testutil.NewCLIClient(testServerURL)
        require.NoError(t, err, "Failed to create CLI client")

        _, err = cli.Authenticate("expireduser", "password123")
        require.Error(t, err, "Expected authentication to fail with expired password")
        assert.Contains(t, err.Error(), "password has expired", "Expected password expiry error")
    })

    // Update password expiry to future
    t.Run("UpdatePasswordExpiry", func(t *testing.T) {
        futureTime := time.Now().Add(24 * time.Hour).Format("2006-01-02")

        input := map[string]interface{}{
            "username":       "expireduser",
            "passwordExpiry": futureTime,
        }

        result, err := env.CLI.RunTool("update_user", input)
        require.NoError(t, err, "Failed to update password expiry")

        content, ok := result["content"].([]interface{})
        require.True(t, ok, "Expected content array in result")

        firstContent, ok := content[0].(map[string]interface{})
        require.True(t, ok, "Expected map in first content")

        text, ok := firstContent["text"].(string)
        require.True(t, ok, "Expected text in first content")
        assert.Contains(t, text, "updated successfully", "Expected success message")
    })

    // Try to authenticate again - should succeed now
    t.Run("AuthenticateAfterPasswordUpdate", func(t *testing.T) {
        cli, err := testutil.NewCLIClient(testServerURL)
        require.NoError(t, err, "Failed to create CLI client")

        token, err := cli.Authenticate("expireduser", "password123")
        require.NoError(t, err, "Expected authentication to succeed with valid password")
        assert.NotEmpty(t, token, "Expected non-empty token")
    })

    // Cleanup
    t.Run("Cleanup", func(t *testing.T) {
        input := map[string]interface{}{
            "username": "expireduser",
        }
        _, err := env.CLI.RunTool("delete_user", input)
        require.NoError(t, err, "Failed to delete test user")
    })
}

// TestSuperuserFlag tests is_superuser flag enforcement
func TestSuperuserFlag(t *testing.T) {
    if os.Getenv("SKIP_INTEGRATION_TESTS") == "1" {
        t.Skip("Skipping integration tests")
    }

    env := SetupTestEnvironment(t)
    defer env.TeardownTestEnvironment(t)

    // Create a regular (non-superuser) user
    t.Run("CreateRegularUser", func(t *testing.T) {
        input := map[string]interface{}{
            "username":    "regularuser",
            "email":       "regular@test.com",
            "fullName":    "Regular User",
            "password":    "password123",
            "isSuperuser": false,
        }

        result, err := env.CLI.RunTool("create_user", input)
        require.NoError(t, err, "Failed to create regular user")

        content, ok := result["content"].([]interface{})
        require.True(t, ok, "Expected content array in result")

        firstContent, ok := content[0].(map[string]interface{})
        require.True(t, ok, "Expected map in first content")

        text, ok := firstContent["text"].(string)
        require.True(t, ok, "Expected text in first content")
        assert.Contains(t, text, "created successfully", "Expected success message")
    })

    // Authenticate as regular user
    var regularUserToken string
    t.Run("AuthenticateAsRegularUser", func(t *testing.T) {
        cli, err := testutil.NewCLIClient(testServerURL)
        require.NoError(t, err, "Failed to create CLI client")

        token, err := cli.Authenticate("regularuser", "password123")
        require.NoError(t, err, "Expected authentication to succeed")
        assert.NotEmpty(t, token, "Expected non-empty token")
        regularUserToken = token
    })

    // Try to create another user as regular user (should fail)
    t.Run("RegularUserCannotCreateUser", func(t *testing.T) {
        cli, err := testutil.NewCLIClient(testServerURL)
        require.NoError(t, err, "Failed to create CLI client")
        cli.SetToken(regularUserToken)

        input := map[string]interface{}{
            "username":    "anotheruser",
            "email":       "another@test.com",
            "fullName":    "Another User",
            "password":    "password123",
            "isSuperuser": false,
        }

        _, err = cli.RunTool("create_user", input)
        require.Error(t, err, "Expected create_user to fail for regular user")
        assert.Contains(t, err.Error(), "permission denied", "Expected permission denied error")
    })

    // Try to list users as regular user (should work - reading resources doesn't require superuser)
    t.Run("RegularUserCanListUsers", func(t *testing.T) {
        cli, err := testutil.NewCLIClient(testServerURL)
        require.NoError(t, err, "Failed to create CLI client")
        cli.SetToken(regularUserToken)

        result, err := cli.ReadResource("ai-workbench://users")
        require.NoError(t, err, "Expected read resource to succeed for authenticated user")

        contents, ok := result["contents"].([]interface{})
        require.True(t, ok, "Expected contents array in result")
        require.Greater(t, len(contents), 0, "Expected non-empty contents")
    })

    // Try to update another user as regular user (should fail)
    t.Run("RegularUserCannotUpdateOtherUser", func(t *testing.T) {
        cli, err := testutil.NewCLIClient(testServerURL)
        require.NoError(t, err, "Failed to create CLI client")
        cli.SetToken(regularUserToken)

        input := map[string]interface{}{
            "username": "admin",
            "fullName": "Hacked Admin",
        }

        _, err = cli.RunTool("update_user", input)
        require.Error(t, err, "Expected update_user to fail for regular user")
        assert.Contains(t, err.Error(), "permission denied", "Expected permission denied error")
    })

    // Try to delete another user as regular user (should fail)
    t.Run("RegularUserCannotDeleteUser", func(t *testing.T) {
        cli, err := testutil.NewCLIClient(testServerURL)
        require.NoError(t, err, "Failed to create CLI client")
        cli.SetToken(regularUserToken)

        input := map[string]interface{}{
            "username": "admin",
        }

        _, err = cli.RunTool("delete_user", input)
        require.Error(t, err, "Expected delete_user to fail for regular user")
        assert.Contains(t, err.Error(), "permission denied", "Expected permission denied error")
    })

    // Create a superuser
    t.Run("CreateSuperuser", func(t *testing.T) {
        input := map[string]interface{}{
            "username":    "superuser2",
            "email":       "super2@test.com",
            "fullName":    "Superuser Two",
            "password":    "password123",
            "isSuperuser": true,
        }

        result, err := env.CLI.RunTool("create_user", input)
        require.NoError(t, err, "Failed to create superuser")

        content, ok := result["content"].([]interface{})
        require.True(t, ok, "Expected content array in result")

        firstContent, ok := content[0].(map[string]interface{})
        require.True(t, ok, "Expected map in first content")

        text, ok := firstContent["text"].(string)
        require.True(t, ok, "Expected text in first content")
        assert.Contains(t, text, "created successfully", "Expected success message")
    })

    // Authenticate as superuser
    var superuserToken string
    t.Run("AuthenticateAsSuperuser", func(t *testing.T) {
        cli, err := testutil.NewCLIClient(testServerURL)
        require.NoError(t, err, "Failed to create CLI client")

        token, err := cli.Authenticate("superuser2", "password123")
        require.NoError(t, err, "Expected authentication to succeed")
        assert.NotEmpty(t, token, "Expected non-empty token")
        superuserToken = token
    })

    // Verify superuser can create users
    t.Run("SuperuserCanCreateUser", func(t *testing.T) {
        cli, err := testutil.NewCLIClient(testServerURL)
        require.NoError(t, err, "Failed to create CLI client")
        cli.SetToken(superuserToken)

        input := map[string]interface{}{
            "username":    "testuser3",
            "email":       "test3@test.com",
            "fullName":    "Test User Three",
            "password":    "password123",
            "isSuperuser": false,
        }

        result, err := cli.RunTool("create_user", input)
        require.NoError(t, err, "Expected create_user to succeed for superuser")

        content, ok := result["content"].([]interface{})
        require.True(t, ok, "Expected content array in result")

        firstContent, ok := content[0].(map[string]interface{})
        require.True(t, ok, "Expected map in first content")

        text, ok := firstContent["text"].(string)
        require.True(t, ok, "Expected text in first content")
        assert.Contains(t, text, "created successfully", "Expected success message")
    })

    // Cleanup
    t.Run("Cleanup", func(t *testing.T) {
        usernames := []string{"regularuser", "superuser2", "testuser3"}
        for _, username := range usernames {
            input := map[string]interface{}{
                "username": username,
            }
            _, err := env.CLI.RunTool("delete_user", input)
            require.NoError(t, err, "Failed to delete test user: %s", username)
        }
    })
}
