/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package auth

import (
	"os"
	"testing"
)

// createTestAuthStoreForPrivileges creates a temporary auth store for testing
func createTestAuthStoreForPrivileges(t *testing.T) (*AuthStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "auth-privileges-test-*")
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
// MCP Privilege Registration Tests
// =============================================================================

func TestRegisterMCPPrivilege(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Register a tool privilege
	id, err := store.RegisterMCPPrivilege("query_database", MCPPrivilegeTypeTool, "Execute read-only SQL queries")
	if err != nil {
		t.Fatalf("Failed to register MCP privilege: %v", err)
	}

	if id <= 0 {
		t.Errorf("Expected positive privilege ID, got %d", id)
	}

	// Verify the privilege exists
	priv, err := store.GetMCPPrivilege("query_database")
	if err != nil {
		t.Fatalf("Failed to get privilege: %v", err)
	}

	if priv == nil {
		t.Fatal("Expected privilege to exist")
	}

	if priv.Identifier != "query_database" {
		t.Errorf("Expected identifier 'query_database', got %q", priv.Identifier)
	}

	if priv.ItemType != MCPPrivilegeTypeTool {
		t.Errorf("Expected item_type 'tool', got %q", priv.ItemType)
	}

	if priv.Description != "Execute read-only SQL queries" {
		t.Errorf("Expected description 'Execute read-only SQL queries', got %q", priv.Description)
	}
}

func TestRegisterMCPPrivilegeDuplicate(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Register first privilege
	id1, err := store.RegisterMCPPrivilege("test_tool", MCPPrivilegeTypeTool, "First description")
	if err != nil {
		t.Fatalf("Failed to register first privilege: %v", err)
	}

	// Register duplicate (should return existing ID)
	id2, err := store.RegisterMCPPrivilege("test_tool", MCPPrivilegeTypeTool, "Second description")
	if err != nil {
		t.Fatalf("Failed to register duplicate privilege: %v", err)
	}

	if id1 != id2 {
		t.Errorf("Expected same ID for duplicate, got %d and %d", id1, id2)
	}
}

func TestRegisterMCPPrivilegeInvalidType(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	_, err := store.RegisterMCPPrivilege("test", "invalid", "Description")
	if err == nil {
		t.Error("Expected error for invalid item type")
	}
}

func TestGetMCPPrivilegeByID(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Register a privilege
	id, _ := store.RegisterMCPPrivilege("test_resource", MCPPrivilegeTypeResource, "Test resource")

	// Get by ID
	priv, err := store.GetMCPPrivilegeByID(id)
	if err != nil {
		t.Fatalf("Failed to get privilege by ID: %v", err)
	}

	if priv == nil {
		t.Fatal("Expected privilege to exist")
	}

	if priv.Identifier != "test_resource" {
		t.Errorf("Expected identifier 'test_resource', got %q", priv.Identifier)
	}

	// Get non-existent ID
	priv, err = store.GetMCPPrivilegeByID(99999)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if priv != nil {
		t.Error("Expected nil for non-existent privilege")
	}
}

func TestListMCPPrivileges(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Register multiple privileges
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	store.RegisterMCPPrivilege("resource_b", MCPPrivilegeTypeResource, "Resource B")
	store.RegisterMCPPrivilege("prompt_c", MCPPrivilegeTypePrompt, "Prompt C")

	// List all privileges
	privileges, err := store.ListMCPPrivileges()
	if err != nil {
		t.Fatalf("Failed to list privileges: %v", err)
	}

	if len(privileges) != 3 {
		t.Errorf("Expected 3 privileges, got %d", len(privileges))
	}
}

func TestListMCPPrivilegesByType(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Register multiple privileges
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B")
	store.RegisterMCPPrivilege("resource_c", MCPPrivilegeTypeResource, "Resource C")

	// List only tools
	tools, err := store.ListMCPPrivilegesByType(MCPPrivilegeTypeTool)
	if err != nil {
		t.Fatalf("Failed to list tools: %v", err)
	}

	if len(tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(tools))
	}

	// List resources
	resources, err := store.ListMCPPrivilegesByType(MCPPrivilegeTypeResource)
	if err != nil {
		t.Fatalf("Failed to list resources: %v", err)
	}

	if len(resources) != 1 {
		t.Errorf("Expected 1 resource, got %d", len(resources))
	}
}

// =============================================================================
// MCP Privilege Grant Tests
// =============================================================================

func TestGrantMCPPrivilege(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Create group and privilege
	groupID, _ := store.CreateGroup("test-group", "Test group")
	privID, _ := store.RegisterMCPPrivilege("test_tool", MCPPrivilegeTypeTool, "Test tool")

	// Grant privilege
	err := store.GrantMCPPrivilege(groupID, privID)
	if err != nil {
		t.Fatalf("Failed to grant privilege: %v", err)
	}

	// Verify grant
	privileges, err := store.ListGroupMCPPrivileges(groupID)
	if err != nil {
		t.Fatalf("Failed to list group privileges: %v", err)
	}

	if len(privileges) != 1 {
		t.Errorf("Expected 1 privilege, got %d", len(privileges))
	}

	if privileges[0].Identifier != "test_tool" {
		t.Errorf("Expected 'test_tool', got %q", privileges[0].Identifier)
	}
}

func TestGrantMCPPrivilegeByName(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Create group and privilege
	groupID, _ := store.CreateGroup("test-group", "Test group")
	store.RegisterMCPPrivilege("test_tool", MCPPrivilegeTypeTool, "Test tool")

	// Grant by name
	err := store.GrantMCPPrivilegeByName(groupID, "test_tool")
	if err != nil {
		t.Fatalf("Failed to grant privilege by name: %v", err)
	}

	// Verify grant
	privileges, err := store.ListGroupMCPPrivileges(groupID)
	if err != nil {
		t.Fatalf("Failed to list group privileges: %v", err)
	}

	if len(privileges) != 1 {
		t.Errorf("Expected 1 privilege, got %d", len(privileges))
	}
}

func TestGrantMCPPrivilegeByNameNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("test-group", "Test group")

	err := store.GrantMCPPrivilegeByName(groupID, "nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent privilege")
	}
}

func TestRevokeMCPPrivilege(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Setup
	groupID, _ := store.CreateGroup("test-group", "Test group")
	privID, _ := store.RegisterMCPPrivilege("test_tool", MCPPrivilegeTypeTool, "Test tool")
	store.GrantMCPPrivilege(groupID, privID)

	// Revoke privilege
	err := store.RevokeMCPPrivilege(groupID, privID)
	if err != nil {
		t.Fatalf("Failed to revoke privilege: %v", err)
	}

	// Verify revocation
	privileges, _ := store.ListGroupMCPPrivileges(groupID)
	if len(privileges) != 0 {
		t.Errorf("Expected 0 privileges after revocation, got %d", len(privileges))
	}
}

func TestRevokeMCPPrivilegeNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("test-group", "Test group")
	privID, _ := store.RegisterMCPPrivilege("test_tool", MCPPrivilegeTypeTool, "Test tool")

	// Try to revoke non-existent grant
	err := store.RevokeMCPPrivilege(groupID, privID)
	if err == nil {
		t.Error("Expected error when revoking non-existent grant")
	}
}

func TestRevokeMCPPrivilegeByName(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Setup
	groupID, _ := store.CreateGroup("test-group", "Test group")
	store.RegisterMCPPrivilege("test_tool", MCPPrivilegeTypeTool, "Test tool")
	store.GrantMCPPrivilegeByName(groupID, "test_tool")

	// Revoke by name
	err := store.RevokeMCPPrivilegeByName(groupID, "test_tool")
	if err != nil {
		t.Fatalf("Failed to revoke privilege by name: %v", err)
	}

	// Verify revocation
	privileges, _ := store.ListGroupMCPPrivileges(groupID)
	if len(privileges) != 0 {
		t.Errorf("Expected 0 privileges after revocation, got %d", len(privileges))
	}
}

// =============================================================================
// Connection Privilege Tests
// =============================================================================

func TestGrantConnectionPrivilege(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("test-group", "Test group")

	// Grant read access
	err := store.GrantConnectionPrivilege(groupID, 1, AccessLevelRead)
	if err != nil {
		t.Fatalf("Failed to grant connection privilege: %v", err)
	}

	// Verify grant
	priv, err := store.GetConnectionPrivilege(groupID, 1)
	if err != nil {
		t.Fatalf("Failed to get connection privilege: %v", err)
	}

	if priv == nil {
		t.Fatal("Expected connection privilege to exist")
	}

	if priv.AccessLevel != AccessLevelRead {
		t.Errorf("Expected access level 'read', got %q", priv.AccessLevel)
	}
}

func TestGrantConnectionPrivilegeUpgrade(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("test-group", "Test group")

	// Grant read access
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelRead)

	// Upgrade to read_write
	err := store.GrantConnectionPrivilege(groupID, 1, AccessLevelReadWrite)
	if err != nil {
		t.Fatalf("Failed to upgrade connection privilege: %v", err)
	}

	// Verify upgrade
	priv, _ := store.GetConnectionPrivilege(groupID, 1)
	if priv.AccessLevel != AccessLevelReadWrite {
		t.Errorf("Expected access level 'read_write', got %q", priv.AccessLevel)
	}
}

func TestGrantConnectionPrivilegeInvalidLevel(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("test-group", "Test group")

	err := store.GrantConnectionPrivilege(groupID, 1, "invalid")
	if err == nil {
		t.Error("Expected error for invalid access level")
	}
}

func TestRevokeConnectionPrivilege(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("test-group", "Test group")
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelRead)

	// Revoke
	err := store.RevokeConnectionPrivilege(groupID, 1)
	if err != nil {
		t.Fatalf("Failed to revoke connection privilege: %v", err)
	}

	// Verify revocation
	priv, _ := store.GetConnectionPrivilege(groupID, 1)
	if priv != nil {
		t.Error("Expected connection privilege to be revoked")
	}
}

func TestRevokeConnectionPrivilegeNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("test-group", "Test group")

	err := store.RevokeConnectionPrivilege(groupID, 99)
	if err == nil {
		t.Error("Expected error when revoking non-existent privilege")
	}
}

func TestListGroupConnectionPrivileges(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("test-group", "Test group")

	// Grant multiple connections
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelRead)
	store.GrantConnectionPrivilege(groupID, 2, AccessLevelReadWrite)
	store.GrantConnectionPrivilege(groupID, 3, AccessLevelRead)

	// List privileges
	privileges, err := store.ListGroupConnectionPrivileges(groupID)
	if err != nil {
		t.Fatalf("Failed to list connection privileges: %v", err)
	}

	if len(privileges) != 3 {
		t.Errorf("Expected 3 privileges, got %d", len(privileges))
	}
}

// =============================================================================
// Group With Privileges Tests
// =============================================================================

func TestGetGroupWithPrivileges(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Create group
	groupID, _ := store.CreateGroup("test-group", "Test group")

	// Add MCP privileges
	privID, _ := store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	store.GrantMCPPrivilege(groupID, privID)

	// Add connection privileges
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelRead)
	store.GrantConnectionPrivilege(groupID, 2, AccessLevelReadWrite)

	// Get group with privileges
	result, err := store.GetGroupWithPrivileges(groupID)
	if err != nil {
		t.Fatalf("Failed to get group with privileges: %v", err)
	}

	if result.Group.Name != "test-group" {
		t.Errorf("Expected group name 'test-group', got %q", result.Group.Name)
	}

	if len(result.MCPPrivileges) != 1 {
		t.Errorf("Expected 1 MCP privilege, got %d", len(result.MCPPrivileges))
	}

	if len(result.ConnectionPrivileges) != 2 {
		t.Errorf("Expected 2 connection privileges, got %d", len(result.ConnectionPrivileges))
	}
}

func TestGetGroupWithPrivilegesNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	_, err := store.GetGroupWithPrivileges(99999)
	if err == nil {
		t.Error("Expected error for non-existent group")
	}
}

// =============================================================================
// User Privilege Lookup Tests
// =============================================================================

func TestGetUserMCPPrivileges(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Create user and group
	store.CreateUser("testuser", "password", "Test user")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test group")

	// Add user to group
	store.AddUserToGroup(groupID, userID)

	// Grant privileges to group
	priv1ID, _ := store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	priv2ID, _ := store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B")
	store.GrantMCPPrivilege(groupID, priv1ID)
	store.GrantMCPPrivilege(groupID, priv2ID)

	// Get user privileges
	privileges, err := store.GetUserMCPPrivileges(userID)
	if err != nil {
		t.Fatalf("Failed to get user MCP privileges: %v", err)
	}

	if len(privileges) != 2 {
		t.Errorf("Expected 2 privileges, got %d", len(privileges))
	}

	if !privileges["tool_a"] {
		t.Error("Expected user to have 'tool_a' privilege")
	}

	if !privileges["tool_b"] {
		t.Error("Expected user to have 'tool_b' privilege")
	}
}

func TestGetUserMCPPrivilegesInherited(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Create user and groups (parent -> child)
	store.CreateUser("testuser", "password", "Test user")
	userID, _ := store.GetUserID("testuser")
	parentID, _ := store.CreateGroup("parent-group", "Parent")
	childID, _ := store.CreateGroup("child-group", "Child")

	// Add child to parent, user to child
	store.AddGroupToGroup(parentID, childID)
	store.AddUserToGroup(childID, userID)

	// Grant privilege to parent only
	privID, _ := store.RegisterMCPPrivilege("parent_tool", MCPPrivilegeTypeTool, "Parent Tool")
	store.GrantMCPPrivilege(parentID, privID)

	// User should inherit privilege through group hierarchy
	privileges, err := store.GetUserMCPPrivileges(userID)
	if err != nil {
		t.Fatalf("Failed to get user MCP privileges: %v", err)
	}

	if !privileges["parent_tool"] {
		t.Error("Expected user to inherit 'parent_tool' privilege from parent group")
	}
}

func TestGetUserMCPPrivilegesNoGroups(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	store.CreateUser("testuser", "password", "Test user")
	userID, _ := store.GetUserID("testuser")

	// User not in any groups
	privileges, err := store.GetUserMCPPrivileges(userID)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(privileges) != 0 {
		t.Errorf("Expected 0 privileges, got %d", len(privileges))
	}
}

func TestGetUserConnectionPrivileges(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Create user and group
	store.CreateUser("testuser", "password", "Test user")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test group")

	// Add user to group
	store.AddUserToGroup(groupID, userID)

	// Grant connection privileges to group
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelRead)
	store.GrantConnectionPrivilege(groupID, 2, AccessLevelReadWrite)

	// Get user connection privileges
	privileges, err := store.GetUserConnectionPrivileges(userID)
	if err != nil {
		t.Fatalf("Failed to get user connection privileges: %v", err)
	}

	if len(privileges) != 2 {
		t.Errorf("Expected 2 privileges, got %d", len(privileges))
	}

	if privileges[1] != AccessLevelRead {
		t.Errorf("Expected 'read' for connection 1, got %q", privileges[1])
	}

	if privileges[2] != AccessLevelReadWrite {
		t.Errorf("Expected 'read_write' for connection 2, got %q", privileges[2])
	}
}

func TestGetUserConnectionPrivilegesHighestWins(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Create user and two groups
	store.CreateUser("testuser", "password", "Test user")
	userID, _ := store.GetUserID("testuser")
	group1ID, _ := store.CreateGroup("group1", "Group 1")
	group2ID, _ := store.CreateGroup("group2", "Group 2")

	// Add user to both groups
	store.AddUserToGroup(group1ID, userID)
	store.AddUserToGroup(group2ID, userID)

	// Grant read to group1, read_write to group2 for same connection
	store.GrantConnectionPrivilege(group1ID, 1, AccessLevelRead)
	store.GrantConnectionPrivilege(group2ID, 1, AccessLevelReadWrite)

	// Get user connection privileges - should have read_write (highest)
	privileges, err := store.GetUserConnectionPrivileges(userID)
	if err != nil {
		t.Fatalf("Failed to get user connection privileges: %v", err)
	}

	if privileges[1] != AccessLevelReadWrite {
		t.Errorf("Expected 'read_write' (highest level), got %q", privileges[1])
	}
}

// =============================================================================
// Privilege Assignment Check Tests
// =============================================================================

func TestIsPrivilegeAssignedToAnyGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Register privilege but don't assign
	store.RegisterMCPPrivilege("unassigned_tool", MCPPrivilegeTypeTool, "Unassigned")

	// Check - should not be assigned
	assigned, err := store.IsPrivilegeAssignedToAnyGroup("unassigned_tool")
	if err != nil {
		t.Fatalf("Failed to check assignment: %v", err)
	}
	if assigned {
		t.Error("Expected privilege to not be assigned")
	}

	// Now assign it
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.GrantMCPPrivilegeByName(groupID, "unassigned_tool")

	// Check again - should be assigned
	assigned, err = store.IsPrivilegeAssignedToAnyGroup("unassigned_tool")
	if err != nil {
		t.Fatalf("Failed to check assignment: %v", err)
	}
	if !assigned {
		t.Error("Expected privilege to be assigned after grant")
	}
}

func TestIsConnectionAssignedToAnyGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	// Check unassigned connection
	assigned, err := store.IsConnectionAssignedToAnyGroup(1)
	if err != nil {
		t.Fatalf("Failed to check assignment: %v", err)
	}
	if assigned {
		t.Error("Expected connection 1 to not be assigned")
	}

	// Assign it
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelRead)

	// Check again
	assigned, err = store.IsConnectionAssignedToAnyGroup(1)
	if err != nil {
		t.Fatalf("Failed to check assignment: %v", err)
	}
	if !assigned {
		t.Error("Expected connection 1 to be assigned after grant")
	}
}

// =============================================================================
// Helper Tests
// =============================================================================

func TestMCPPrivilegeCount(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	if count := store.MCPPrivilegeCount(); count != 0 {
		t.Errorf("Expected 0 privileges initially, got %d", count)
	}

	store.RegisterMCPPrivilege("tool1", MCPPrivilegeTypeTool, "")
	store.RegisterMCPPrivilege("tool2", MCPPrivilegeTypeTool, "")

	if count := store.MCPPrivilegeCount(); count != 2 {
		t.Errorf("Expected 2 privileges, got %d", count)
	}
}
