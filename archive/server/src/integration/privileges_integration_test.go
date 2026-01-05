/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package integration

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgEdge/ai-workbench/server/src/config"
	"github.com/pgEdge/ai-workbench/server/src/database"
	"github.com/pgEdge/ai-workbench/server/src/groupmgmt"
	"github.com/pgEdge/ai-workbench/server/src/privileges"
)

// skipIfNoDatabase skips the test if SKIP_DB_TESTS is set or if database connection fails
func skipIfNoDatabase(t *testing.T) *pgxpool.Pool {
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}

	// Get database connection from environment or use default
	cfg := config.NewConfig()
	cfg.PgHost = os.Getenv("TEST_DB_HOST")
	if cfg.PgHost == "" {
		cfg.PgHost = "localhost"
	}
	cfg.PgPort = 5432
	cfg.PgDatabase = os.Getenv("TEST_DB_NAME")
	if cfg.PgDatabase == "" {
		cfg.PgDatabase = "postgres"
	}
	cfg.PgUsername = os.Getenv("TEST_DB_USER")
	if cfg.PgUsername == "" {
		cfg.PgUsername = "postgres"
	}
	cfg.PgPassword = os.Getenv("TEST_DB_PASSWORD")

	pool, err := database.Connect(cfg)
	if err != nil {
		t.Skipf("Skipping database test (cannot connect): %v", err)
	}

	return pool
}

// TestGroupHierarchyAndInheritance tests nested group membership and privilege inheritance
func TestGroupHierarchyAndInheritance(t *testing.T) {
	pool := skipIfNoDatabase(t)
	ctx := context.Background()

	// Clean up test data
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM user_groups WHERE name LIKE 'test_%'") //nolint:errcheck // Cleanup code
		_, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username LIKE 'test_%'") //nolint:errcheck // Cleanup code
	}()

	// Create test users
	var user1ID, user2ID int
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts (username, email, full_name, password_hash, is_superuser)
		VALUES ('test_user1', 'user1@test.com', 'Test User 1', 'hash1', false)
		RETURNING id
	`).Scan(&user1ID)
	if err != nil {
		t.Fatalf("Failed to create test_user1: %v", err)
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO user_accounts (username, email, full_name, password_hash, is_superuser)
		VALUES ('test_user2', 'user2@test.com', 'Test User 2', 'hash2', false)
		RETURNING id
	`).Scan(&user2ID)
	if err != nil {
		t.Fatalf("Failed to create test_user2: %v", err)
	}

	// Create group hierarchy: parent_group -> child_group
	parentGroupID, err := groupmgmt.CreateUserGroup(ctx, pool, "test_parent_group", "Parent test group")
	if err != nil {
		t.Fatalf("Failed to create parent group: %v", err)
	}

	childGroupID, err := groupmgmt.CreateUserGroup(ctx, pool, "test_child_group", "Child test group")
	if err != nil {
		t.Fatalf("Failed to create child group: %v", err)
	}

	// Add child group as member of parent group
	err = groupmgmt.AddGroupMember(ctx, pool, parentGroupID, nil, &childGroupID)
	if err != nil {
		t.Fatalf("Failed to add child group to parent: %v", err)
	}

	// Add user1 to child group
	err = groupmgmt.AddGroupMember(ctx, pool, childGroupID, &user1ID, nil)
	if err != nil {
		t.Fatalf("Failed to add user1 to child group: %v", err)
	}

	// Add user2 directly to parent group
	err = groupmgmt.AddGroupMember(ctx, pool, parentGroupID, &user2ID, nil)
	if err != nil {
		t.Fatalf("Failed to add user2 to parent group: %v", err)
	}

	// Verify user1 belongs to both child and parent groups (via inheritance)
	groups, err := privileges.GetUserGroups(ctx, pool, user1ID)
	if err != nil {
		t.Fatalf("Failed to get user1 groups: %v", err)
	}

	if len(groups) != 2 {
		t.Errorf("Expected user1 to belong to 2 groups, got %d", len(groups))
	}

	hasChild := false
	hasParent := false
	for _, gid := range groups {
		if gid == childGroupID {
			hasChild = true
		}
		if gid == parentGroupID {
			hasParent = true
		}
	}

	if !hasChild {
		t.Error("User1 should belong to child group")
	}
	if !hasParent {
		t.Error("User1 should inherit membership from parent group")
	}

	// Verify user2 belongs only to parent group
	groups, err = privileges.GetUserGroups(ctx, pool, user2ID)
	if err != nil {
		t.Fatalf("Failed to get user2 groups: %v", err)
	}

	if len(groups) != 1 {
		t.Errorf("Expected user2 to belong to 1 group, got %d", len(groups))
	}

	if groups[0] != parentGroupID {
		t.Error("User2 should belong to parent group")
	}
}

// TestCircularReferencePrevent tests that circular group memberships are prevented
func TestCircularReferencePrevention(t *testing.T) {
	pool := skipIfNoDatabase(t)
	ctx := context.Background()

	// Clean up test data
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM user_groups WHERE name LIKE 'test_%'") //nolint:errcheck // Cleanup code
	}()

	// Create three groups: A -> B -> C
	groupAID, err := groupmgmt.CreateUserGroup(ctx, pool, "test_group_a", "Group A")
	if err != nil {
		t.Fatalf("Failed to create group A: %v", err)
	}

	groupBID, err := groupmgmt.CreateUserGroup(ctx, pool, "test_group_b", "Group B")
	if err != nil {
		t.Fatalf("Failed to create group B: %v", err)
	}

	groupCID, err := groupmgmt.CreateUserGroup(ctx, pool, "test_group_c", "Group C")
	if err != nil {
		t.Fatalf("Failed to create group C: %v", err)
	}

	// Create chain: A -> B -> C
	err = groupmgmt.AddGroupMember(ctx, pool, groupAID, nil, &groupBID)
	if err != nil {
		t.Fatalf("Failed to add B to A: %v", err)
	}

	err = groupmgmt.AddGroupMember(ctx, pool, groupBID, nil, &groupCID)
	if err != nil {
		t.Fatalf("Failed to add C to B: %v", err)
	}

	// Attempt to create circular reference: C -> A (should fail)
	err = groupmgmt.AddGroupMember(ctx, pool, groupCID, nil, &groupAID)
	if err == nil {
		t.Error("Expected error when creating circular reference, got nil")
	}

	// Attempt to create self-reference: A -> A (should fail)
	err = groupmgmt.AddGroupMember(ctx, pool, groupAID, nil, &groupAID)
	if err == nil {
		t.Error("Expected error when creating self-reference, got nil")
	}
}

// TestMCPPrivilegeEnforcement tests MCP privilege granting and enforcement
func TestMCPPrivilegeEnforcement(t *testing.T) {
	pool := skipIfNoDatabase(t)
	ctx := context.Background()

	// Clean up test data
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM user_groups WHERE name LIKE 'test_%'") //nolint:errcheck // Cleanup code
		_, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username LIKE 'test_%'") //nolint:errcheck // Cleanup code
	}()

	// Create test user (non-superuser)
	var userID int
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts (username, email, full_name, password_hash, is_superuser)
		VALUES ('test_privilege_user', 'privuser@test.com', 'Test Privilege User', 'hash', false)
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create superuser for comparison
	var superuserID int
	err = pool.QueryRow(ctx, `
		INSERT INTO user_accounts (username, email, full_name, password_hash, is_superuser)
		VALUES ('test_superuser', 'super@test.com', 'Test Superuser', 'hash', true)
		RETURNING id
	`).Scan(&superuserID)
	if err != nil {
		t.Fatalf("Failed to create superuser: %v", err)
	}

	// Test 1: User without privilege should not have access
	canAccess, err := privileges.CanAccessMCPItem(ctx, pool, userID, "create_user")
	if err != nil {
		t.Fatalf("Failed to check MCP privilege: %v", err)
	}
	if canAccess {
		t.Error("User without privilege should not have access to create_user")
	}

	// Test 2: Superuser should always have access
	canAccess, err = privileges.CanAccessMCPItem(ctx, pool, superuserID, "create_user")
	if err != nil {
		t.Fatalf("Failed to check superuser privilege: %v", err)
	}
	if !canAccess {
		t.Error("Superuser should always have access to create_user")
	}

	// Test 3: Grant privilege via group membership
	groupID, err := groupmgmt.CreateUserGroup(ctx, pool, "test_privilege_group", "Test privilege group")
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Add user to group
	err = groupmgmt.AddGroupMember(ctx, pool, groupID, &userID, nil)
	if err != nil {
		t.Fatalf("Failed to add user to group: %v", err)
	}

	// Grant create_user privilege to group
	err = groupmgmt.GrantMCPPrivilege(ctx, pool, groupID, "create_user")
	if err != nil {
		t.Fatalf("Failed to grant MCP privilege: %v", err)
	}

	// Test 4: User should now have access via group membership
	canAccess, err = privileges.CanAccessMCPItem(ctx, pool, userID, "create_user")
	if err != nil {
		t.Fatalf("Failed to check MCP privilege after grant: %v", err)
	}
	if !canAccess {
		t.Error("User should have access to create_user via group membership")
	}

	// Test 5: Revoke privilege
	err = groupmgmt.RevokeMCPPrivilege(ctx, pool, groupID, "create_user")
	if err != nil {
		t.Fatalf("Failed to revoke MCP privilege: %v", err)
	}

	// Test 6: User should no longer have access
	canAccess, err = privileges.CanAccessMCPItem(ctx, pool, userID, "create_user")
	if err != nil {
		t.Fatalf("Failed to check MCP privilege after revoke: %v", err)
	}
	if canAccess {
		t.Error("User should not have access to create_user after privilege revoked")
	}
}

// TestConnectionPrivileges tests connection privilege granting and enforcement
func TestConnectionPrivileges(t *testing.T) {
	pool := skipIfNoDatabase(t)
	ctx := context.Background()

	// Clean up test data
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM user_groups WHERE name LIKE 'test_%'") //nolint:errcheck // Cleanup code
		_, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username LIKE 'test_%'") //nolint:errcheck // Cleanup code
		_, _ = pool.Exec(ctx, "DELETE FROM connections WHERE name LIKE 'test_%'") //nolint:errcheck // Cleanup code
	}()

	// Create test user
	var userID int
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts (username, email, full_name, password_hash, is_superuser)
		VALUES ('test_conn_user', 'connuser@test.com', 'Test Connection User', 'hash', false)
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create test connection
	var connectionID int
	err = pool.QueryRow(ctx, `
		INSERT INTO connections (name, host, port, database_name, username, owner_username, is_shared)
		VALUES ('test_connection', 'localhost', 5432, 'testdb', 'testuser', 'test_conn_user', true)
		RETURNING id
	`).Scan(&connectionID)
	if err != nil {
		t.Fatalf("Failed to create test connection: %v", err)
	}

	// Test 1: User without privilege should not have access to shared connection
	canAccess, err := privileges.CanAccessConnection(ctx, pool, userID, connectionID, "read")
	if err != nil {
		t.Fatalf("Failed to check connection access: %v", err)
	}
	if canAccess {
		t.Error("User should not have access to connection without privilege")
	}

	// Test 2: Grant read access via group
	groupID, err := groupmgmt.CreateUserGroup(ctx, pool, "test_conn_group", "Connection test group")
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	err = groupmgmt.AddGroupMember(ctx, pool, groupID, &userID, nil)
	if err != nil {
		t.Fatalf("Failed to add user to group: %v", err)
	}

	err = groupmgmt.GrantConnectionPrivilege(ctx, pool, groupID, connectionID, "read")
	if err != nil {
		t.Fatalf("Failed to grant connection privilege: %v", err)
	}

	// Test 3: User should have read access
	canAccess, err = privileges.CanAccessConnection(ctx, pool, userID, connectionID, "read")
	if err != nil {
		t.Fatalf("Failed to check read access: %v", err)
	}
	if !canAccess {
		t.Error("User should have read access via group membership")
	}

	// Test 4: User should not have read_write access
	canAccess, err = privileges.CanAccessConnection(ctx, pool, userID, connectionID, "read_write")
	if err != nil {
		t.Fatalf("Failed to check read_write access: %v", err)
	}
	if canAccess {
		t.Error("User should not have read_write access with only read privilege")
	}

	// Test 5: Upgrade to read_write
	err = groupmgmt.GrantConnectionPrivilege(ctx, pool, groupID, connectionID, "read_write")
	if err != nil {
		t.Fatalf("Failed to upgrade connection privilege: %v", err)
	}

	canAccess, err = privileges.CanAccessConnection(ctx, pool, userID, connectionID, "read_write")
	if err != nil {
		t.Fatalf("Failed to check upgraded access: %v", err)
	}
	if !canAccess {
		t.Error("User should have read_write access after upgrade")
	}

	// Test 6: Revoke privilege
	err = groupmgmt.RevokeConnectionPrivilege(ctx, pool, groupID, connectionID)
	if err != nil {
		t.Fatalf("Failed to revoke connection privilege: %v", err)
	}

	canAccess, err = privileges.CanAccessConnection(ctx, pool, userID, connectionID, "read")
	if err != nil {
		t.Fatalf("Failed to check access after revoke: %v", err)
	}
	if canAccess {
		t.Error("User should not have access after privilege revoked")
	}
}

// TestCascadeDeletes tests that deleting groups cascades properly
func TestCascadeDeletes(t *testing.T) {
	pool := skipIfNoDatabase(t)
	ctx := context.Background()

	// Clean up test data
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM user_groups WHERE name LIKE 'test_%'") //nolint:errcheck // Cleanup code
		_, _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE username LIKE 'test_%'") //nolint:errcheck // Cleanup code
	}()

	// Create test user
	var userID int
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts (username, email, full_name, password_hash, is_superuser)
		VALUES ('test_cascade_user', 'cascade@test.com', 'Test Cascade User', 'hash', false)
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create group with member and privileges
	groupID, err := groupmgmt.CreateUserGroup(ctx, pool, "test_cascade_group", "Cascade test group")
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Add user to group
	err = groupmgmt.AddGroupMember(ctx, pool, groupID, &userID, nil)
	if err != nil {
		t.Fatalf("Failed to add user to group: %v", err)
	}

	// Grant privilege
	err = groupmgmt.GrantMCPPrivilege(ctx, pool, groupID, "create_user")
	if err != nil {
		t.Fatalf("Failed to grant privilege: %v", err)
	}

	// Verify user has access
	canAccess, err := privileges.CanAccessMCPItem(ctx, pool, userID, "create_user")
	if err != nil {
		t.Fatalf("Failed to check access: %v", err)
	}
	if !canAccess {
		t.Error("User should have access before group deletion")
	}

	// Delete group (should CASCADE delete memberships and privileges)
	err = groupmgmt.DeleteUserGroup(ctx, pool, groupID)
	if err != nil {
		t.Fatalf("Failed to delete group: %v", err)
	}

	// Verify user no longer has access
	canAccess, err = privileges.CanAccessMCPItem(ctx, pool, userID, "create_user")
	if err != nil {
		t.Fatalf("Failed to check access after delete: %v", err)
	}
	if canAccess {
		t.Error("User should not have access after group deletion")
	}

	// Verify memberships were deleted
	var count int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM group_memberships WHERE parent_group_id = $1
	`, groupID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check memberships: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 memberships after group deletion, got %d", count)
	}

	// Verify privileges were deleted
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM group_mcp_privileges WHERE group_id = $1
	`, groupID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check privileges: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 privileges after group deletion, got %d", count)
	}
}

// TestTokenScoping tests token scope restrictions for connections and MCP items
func TestTokenScoping(t *testing.T) {
	pool := skipIfNoDatabase(t)
	ctx := context.Background()

	// Clean up test data
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM service_tokens WHERE name LIKE 'test_%'") //nolint:errcheck // Cleanup code
		_, _ = pool.Exec(ctx, "DELETE FROM connections WHERE name LIKE 'test_%'") //nolint:errcheck // Cleanup code
	}()

	// Create test connections
	var conn1ID, conn2ID int
	err := pool.QueryRow(ctx, `
		INSERT INTO connections (name, host, port, database_name, username, owner_token, is_shared)
		VALUES ('test_conn1', 'localhost', 5432, 'testdb1', 'testuser', 'test_scoped_token', true)
		RETURNING id
	`).Scan(&conn1ID)
	if err != nil {
		t.Fatalf("Failed to create test_conn1: %v", err)
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO connections (name, host, port, database_name, username, owner_token, is_shared)
		VALUES ('test_conn2', 'localhost', 5432, 'testdb2', 'testuser', 'test_scoped_token', true)
		RETURNING id
	`).Scan(&conn2ID)
	if err != nil {
		t.Fatalf("Failed to create test_conn2: %v", err)
	}

	// Create a service token
	var tokenID int
	err = pool.QueryRow(ctx, `
		INSERT INTO service_tokens (name, token_hash, is_superuser)
		VALUES ('test_scoped_token', 'hash123', false)
		RETURNING id
	`).Scan(&tokenID)
	if err != nil {
		t.Fatalf("Failed to create service token: %v", err)
	}

	// Test 1: Set connection scope to only conn1
	connectionIDs := []int{conn1ID}
	err = groupmgmt.SetTokenConnectionScope(ctx, pool, tokenID, "service", connectionIDs)
	if err != nil {
		t.Fatalf("Failed to set token connection scope: %v", err)
	}

	// Verify connection scope was set
	var count int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM token_connection_scope WHERE token_id = $1 AND token_type = 'service'
	`, tokenID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query connection scope: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 connection in scope, got %d", count)
	}

	// Test 2: Set MCP scope to specific tools
	mcpIdentifiers := []string{"create_user", "list_user_groups"}
	err = groupmgmt.SetTokenMCPScope(ctx, pool, tokenID, "service", mcpIdentifiers)
	if err != nil {
		t.Fatalf("Failed to set token MCP scope: %v", err)
	}

	// Verify MCP scope was set
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = $1 AND token_type = 'service'
	`, tokenID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query MCP scope: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 MCP items in scope, got %d", count)
	}

	// Test 3: Get token scope
	scope, err := groupmgmt.GetTokenScope(ctx, pool, tokenID, "service")
	if err != nil {
		t.Fatalf("Failed to get token scope: %v", err)
	}

	connections, ok := scope["connections"].([]map[string]interface{})
	if !ok {
		t.Fatal("Failed to get connections from scope")
	}
	if len(connections) != 1 {
		t.Errorf("Expected 1 connection in scope, got %d", len(connections))
	}
	if len(connections) > 0 {
		connID, ok := connections[0]["connection_id"].(int)
		if !ok {
			t.Error("Failed to get connection_id from connection scope")
		} else if connID != conn1ID {
			t.Errorf("Expected connection ID %d in scope, got %d", conn1ID, connID)
		}
	}

	mcpItems, ok := scope["mcp_items"].([]map[string]interface{})
	if !ok {
		t.Fatal("Failed to get mcp_items from scope")
	}
	if len(mcpItems) != 2 {
		t.Errorf("Expected 2 MCP items in scope, got %d", len(mcpItems))
	}

	// Test 4: Update connection scope to include both connections
	connectionIDs = []int{conn1ID, conn2ID}
	err = groupmgmt.SetTokenConnectionScope(ctx, pool, tokenID, "service", connectionIDs)
	if err != nil {
		t.Fatalf("Failed to update token connection scope: %v", err)
	}

	// Verify connection scope was updated
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM token_connection_scope WHERE token_id = $1 AND token_type = 'service'
	`, tokenID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query updated connection scope: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 connections in scope after update, got %d", count)
	}

	// Test 5: Clear token scope
	err = groupmgmt.ClearTokenScope(ctx, pool, tokenID, "service")
	if err != nil {
		t.Fatalf("Failed to clear token scope: %v", err)
	}

	// Verify connection scope was cleared
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM token_connection_scope WHERE token_id = $1 AND token_type = 'service'
	`, tokenID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query connection scope after clear: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 connections in scope after clear, got %d", count)
	}

	// Verify MCP scope was cleared
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = $1 AND token_type = 'service'
	`, tokenID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query MCP scope after clear: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 MCP items in scope after clear, got %d", count)
	}

	// Test 6: Verify GetTokenScope returns empty slices after clear
	scope, err = groupmgmt.GetTokenScope(ctx, pool, tokenID, "service")
	if err != nil {
		t.Fatalf("Failed to get token scope after clear: %v", err)
	}

	connections, ok = scope["connections"].([]map[string]interface{})
	if !ok {
		t.Fatal("Failed to get connections from scope after clear")
	}
	if len(connections) != 0 {
		t.Errorf("Expected 0 connections in scope after clear, got %d", len(connections))
	}

	mcpItems, ok = scope["mcp_items"].([]map[string]interface{})
	if !ok {
		t.Fatal("Failed to get mcp_items from scope after clear")
	}
	if len(mcpItems) != 0 {
		t.Errorf("Expected 0 MCP items in scope after clear, got %d", len(mcpItems))
	}
}
