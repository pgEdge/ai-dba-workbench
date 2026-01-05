/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package privileges

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// skipIfNoDatabase skips the test if SKIP_DB_TESTS is set
// and returns a connection pool to the test database
func skipIfNoDatabase(t *testing.T) *pgxpool.Pool {
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}

	// Get connection string from environment or use default
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		connStr = "postgres://postgres@localhost:5432/postgres"
	}

	ctx := context.Background()

	// Connect to default database to create test database
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Create test database name with timestamp
	testDBName := fmt.Sprintf("ai_workbench_test_%s_%d",
		time.Now().Format("20060102_150405"),
		time.Now().UnixNano()%1000000)

	// Create test database
	_, err = pool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", testDBName))
	if err != nil {
		pool.Close()
		t.Fatalf("Failed to create test database: %v", err)
	}

	pool.Close()

	// Connect to test database
	testConnStr := fmt.Sprintf("postgres://postgres@localhost:5432/%s", testDBName)
	testPool, err := pgxpool.New(ctx, testConnStr)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Run migrations to set up schema
	// Note: For now we'll assume schema exists. In production we'd run migrations here.
	// For testing purposes, we can create the minimal schema we need inline or skip if schema doesn't exist.

	// Register cleanup
	t.Cleanup(func() {
		testPool.Close()

		// Reconnect to default database to drop test database
		cleanupPool, err := pgxpool.New(ctx, connStr)
		if err != nil {
			t.Logf("Warning: Failed to connect for cleanup: %v", err)
			return
		}
		defer cleanupPool.Close()

		_, err = cleanupPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", testDBName))
		if err != nil {
			t.Logf("Warning: Failed to drop test database: %v", err)
		}
	})

	return testPool
}

func TestGetUserGroups_Empty(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	ctx := context.Background()

	// Create a test user
	var userID int
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts (username, email, full_name, password_hash)
		VALUES ('testuser', 'test@example.com', 'Test User', 'hash')
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// User is not in any groups
	groups, err := GetUserGroups(ctx, pool, userID)
	if err != nil {
		t.Fatalf("GetUserGroups failed: %v", err)
	}

	if len(groups) != 0 {
		t.Errorf("Expected 0 groups, got %d", len(groups))
	}

	// Cleanup
	_, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE id = $1", userID) //nolint:errcheck // Cleanup code, failure is acceptable
}

func TestGetUserGroups_DirectMembership(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	ctx := context.Background()

	// Create test user
	var userID int
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts (username, email, full_name, password_hash)
		VALUES ('testuser', 'test@example.com', 'Test User', 'hash')
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create test groups
	var group1ID, group2ID int
	err = pool.QueryRow(ctx, `
		INSERT INTO user_groups (name, description)
		VALUES ('group1', 'Test Group 1')
		RETURNING id
	`).Scan(&group1ID)
	if err != nil {
		t.Fatalf("Failed to create group 1: %v", err)
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO user_groups (name, description)
		VALUES ('group2', 'Test Group 2')
		RETURNING id
	`).Scan(&group2ID)
	if err != nil {
		t.Fatalf("Failed to create group 2: %v", err)
	}

	// Add user to both groups
	_, err = pool.Exec(ctx, `
		INSERT INTO group_memberships (parent_group_id, member_user_id)
		VALUES ($1, $2), ($3, $2)
	`, group1ID, userID, group2ID)
	if err != nil {
		t.Fatalf("Failed to add user to groups: %v", err)
	}

	// Get user groups
	groups, err := GetUserGroups(ctx, pool, userID)
	if err != nil {
		t.Fatalf("GetUserGroups failed: %v", err)
	}

	if len(groups) != 2 {
		t.Errorf("Expected 2 groups, got %d", len(groups))
	}

	// Verify group IDs
	foundGroup1 := false
	foundGroup2 := false
	for _, gid := range groups {
		if gid == group1ID {
			foundGroup1 = true
		}
		if gid == group2ID {
			foundGroup2 = true
		}
	}

	if !foundGroup1 || !foundGroup2 {
		t.Errorf("User groups missing expected groups. Got: %v", groups)
	}

	// Cleanup
	_, _ = pool.Exec(ctx, "DELETE FROM user_groups WHERE id IN ($1, $2)", group1ID, group2ID)         //nolint:errcheck // Cleanup code, failure is acceptable
	_, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE id = $1", userID)                          //nolint:errcheck // Cleanup code, failure is acceptable
	_, _ = pool.Exec(ctx, "DELETE FROM group_memberships WHERE member_user_id = $1", userID)          //nolint:errcheck // Cleanup code, failure is acceptable
	_, _ = pool.Exec(ctx, "DELETE FROM group_memberships WHERE parent_group_id IN ($1, $2)", group1ID, group2ID) //nolint:errcheck // Cleanup code, failure is acceptable
}

func TestGetUserGroups_NestedMembership(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	ctx := context.Background()

	// Create test user
	var userID int
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts (username, email, full_name, password_hash)
		VALUES ('testuser', 'test@example.com', 'Test User', 'hash')
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create nested group hierarchy: group3 -> group2 -> group1 -> user
	var group1ID, group2ID, group3ID int
	err = pool.QueryRow(ctx, "INSERT INTO user_groups (name) VALUES ('group1') RETURNING id").Scan(&group1ID)
	if err != nil {
		t.Fatalf("Failed to create group 1: %v", err)
	}
	err = pool.QueryRow(ctx, "INSERT INTO user_groups (name) VALUES ('group2') RETURNING id").Scan(&group2ID)
	if err != nil {
		t.Fatalf("Failed to create group 2: %v", err)
	}
	err = pool.QueryRow(ctx, "INSERT INTO user_groups (name) VALUES ('group3') RETURNING id").Scan(&group3ID)
	if err != nil {
		t.Fatalf("Failed to create group 3: %v", err)
	}

	// User is in group1
	_, err = pool.Exec(ctx, "INSERT INTO group_memberships (parent_group_id, member_user_id) VALUES ($1, $2)", group1ID, userID)
	if err != nil {
		t.Fatalf("Failed to add user to group1: %v", err)
	}

	// group1 is in group2
	_, err = pool.Exec(ctx, "INSERT INTO group_memberships (parent_group_id, member_group_id) VALUES ($1, $2)", group2ID, group1ID)
	if err != nil {
		t.Fatalf("Failed to add group1 to group2: %v", err)
	}

	// group2 is in group3
	_, err = pool.Exec(ctx, "INSERT INTO group_memberships (parent_group_id, member_group_id) VALUES ($1, $2)", group3ID, group2ID)
	if err != nil {
		t.Fatalf("Failed to add group2 to group3: %v", err)
	}

	// Get user groups - should include all 3 groups
	groups, err := GetUserGroups(ctx, pool, userID)
	if err != nil {
		t.Fatalf("GetUserGroups failed: %v", err)
	}

	if len(groups) != 3 {
		t.Errorf("Expected 3 groups, got %d: %v", len(groups), groups)
	}

	// Cleanup
	_, _ = pool.Exec(ctx, "DELETE FROM group_memberships WHERE member_user_id = $1", userID)                                         //nolint:errcheck // Cleanup code, failure is acceptable
	_, _ = pool.Exec(ctx, "DELETE FROM group_memberships WHERE member_group_id IN ($1, $2, $3)", group1ID, group2ID, group3ID)      //nolint:errcheck // Cleanup code, failure is acceptable
	_, _ = pool.Exec(ctx, "DELETE FROM user_groups WHERE id IN ($1, $2, $3)", group1ID, group2ID, group3ID)                          //nolint:errcheck // Cleanup code, failure is acceptable
	_, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE id = $1", userID)                                                         //nolint:errcheck // Cleanup code, failure is acceptable
}

func TestCanAccessConnection_Superuser(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	ctx := context.Background()

	// Create superuser
	var userID int
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts (username, email, full_name, password_hash, is_superuser)
		VALUES ('superuser', 'super@example.com', 'Super User', 'hash', true)
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create superuser: %v", err)
	}

	// Create a connection
	var connID int
	err = pool.QueryRow(ctx, `
		INSERT INTO connections (name, host, database_name, username, is_shared, port)
		VALUES ('testconn', 'localhost', 'testdb', 'testuser', true, 5432)
		RETURNING id
	`).Scan(&connID)
	if err != nil {
		t.Fatalf("Failed to create connection: %v", err)
	}

	// Superuser should have access
	canAccess, err := CanAccessConnection(ctx, pool, userID, connID, AccessLevelReadWrite)
	if err != nil {
		t.Fatalf("CanAccessConnection failed: %v", err)
	}

	if !canAccess {
		t.Error("Superuser should have access to all connections")
	}

	// Cleanup
	_, _ = pool.Exec(ctx, "DELETE FROM connections WHERE id = $1", connID) //nolint:errcheck // Cleanup code, failure is acceptable
	_, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE id = $1", userID) //nolint:errcheck // Cleanup code, failure is acceptable
}

func TestCanAccessConnection_SharedNoGroups(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	ctx := context.Background()

	// Create regular user
	var userID int
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts (username, email, full_name, password_hash)
		VALUES ('regularuser', 'user@example.com', 'Regular User', 'hash')
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Create shared connection with no groups assigned
	var connID int
	err = pool.QueryRow(ctx, `
		INSERT INTO connections (name, host, database_name, username, is_shared, port)
		VALUES ('testconn', 'localhost', 'testdb', 'testuser', true, 5432)
		RETURNING id
	`).Scan(&connID)
	if err != nil {
		t.Fatalf("Failed to create connection: %v", err)
	}

	// Any user should have access to shared connections with no groups
	canAccess, err := CanAccessConnection(ctx, pool, userID, connID, AccessLevelRead)
	if err != nil {
		t.Fatalf("CanAccessConnection failed: %v", err)
	}

	if !canAccess {
		t.Error("User should have access to shared connection with no groups assigned")
	}

	// Cleanup
	_, _ = pool.Exec(ctx, "DELETE FROM connections WHERE id = $1", connID) //nolint:errcheck // Cleanup code, failure is acceptable
	_, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE id = $1", userID) //nolint:errcheck // Cleanup code, failure is acceptable
}

func TestValidateGroupHierarchy_SelfReference(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	ctx := context.Background()

	// Create a group
	var groupID int
	err := pool.QueryRow(ctx, "INSERT INTO user_groups (name) VALUES ('testgroup') RETURNING id").Scan(&groupID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Try to add group as member of itself
	err = ValidateGroupHierarchy(ctx, pool, groupID, groupID)
	if err == nil {
		t.Error("Expected error for self-reference, got nil")
	}

	// Cleanup
	_, _ = pool.Exec(ctx, "DELETE FROM user_groups WHERE id = $1", groupID) //nolint:errcheck // Cleanup code, failure is acceptable
}

func TestValidateGroupHierarchy_CircularReference(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	ctx := context.Background()

	// Create groups: group1 -> group2 -> group3
	var group1ID, group2ID, group3ID int
	err := pool.QueryRow(ctx, "INSERT INTO user_groups (name) VALUES ('group1') RETURNING id").Scan(&group1ID)
	if err != nil {
		t.Fatalf("Failed to create group1: %v", err)
	}
	err = pool.QueryRow(ctx, "INSERT INTO user_groups (name) VALUES ('group2') RETURNING id").Scan(&group2ID)
	if err != nil {
		t.Fatalf("Failed to create group2: %v", err)
	}
	err = pool.QueryRow(ctx, "INSERT INTO user_groups (name) VALUES ('group3') RETURNING id").Scan(&group3ID)
	if err != nil {
		t.Fatalf("Failed to create group3: %v", err)
	}

	// group2 is member of group1
	_, err = pool.Exec(ctx, "INSERT INTO group_memberships (parent_group_id, member_group_id) VALUES ($1, $2)", group1ID, group2ID)
	if err != nil {
		t.Fatalf("Failed to add group2 to group1: %v", err)
	}

	// group3 is member of group2
	_, err = pool.Exec(ctx, "INSERT INTO group_memberships (parent_group_id, member_group_id) VALUES ($1, $2)", group2ID, group3ID)
	if err != nil {
		t.Fatalf("Failed to add group3 to group2: %v", err)
	}

	// Try to add group1 as member of group3 (would create a cycle)
	err = ValidateGroupHierarchy(ctx, pool, group3ID, group1ID)
	if err == nil {
		t.Error("Expected error for circular reference, got nil")
	}

	// Cleanup
	_, _ = pool.Exec(ctx, "DELETE FROM group_memberships WHERE parent_group_id IN ($1, $2, $3)", group1ID, group2ID, group3ID) //nolint:errcheck // Cleanup code, failure is acceptable
	_, _ = pool.Exec(ctx, "DELETE FROM user_groups WHERE id IN ($1, $2, $3)", group1ID, group2ID, group3ID)                     //nolint:errcheck // Cleanup code, failure is acceptable
}

func TestCanAccessMCPItem_Superuser(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	ctx := context.Background()

	// Create superuser
	var userID int
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts (username, email, full_name, password_hash, is_superuser)
		VALUES ('superuser', 'super@example.com', 'Super User', 'hash', true)
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create superuser: %v", err)
	}

	// Superuser should have access to any MCP item
	canAccess, err := CanAccessMCPItem(ctx, pool, userID, "any_tool")
	if err != nil {
		t.Fatalf("CanAccessMCPItem failed: %v", err)
	}

	if !canAccess {
		t.Error("Superuser should have access to all MCP items")
	}

	// Cleanup
	_, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE id = $1", userID) //nolint:errcheck // Cleanup code, failure is acceptable
}

func TestCanAccessMCPItem_NoPrivilegeIdentifier(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	ctx := context.Background()

	// Create regular user
	var userID int
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts (username, email, full_name, password_hash)
		VALUES ('regularuser', 'user@example.com', 'Regular User', 'hash')
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// User should have access to items not in privilege system (backwards compatibility)
	canAccess, err := CanAccessMCPItem(ctx, pool, userID, "nonexistent_tool")
	if err != nil {
		t.Fatalf("CanAccessMCPItem failed: %v", err)
	}

	if !canAccess {
		t.Error("User should have access to items not yet in privilege system")
	}

	// Cleanup
	_, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE id = $1", userID) //nolint:errcheck // Cleanup code, failure is acceptable
}
