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
	"context"
	"os"
	"testing"
)

// createTestAuthStoreForAccess creates a temporary auth store for testing
func createTestAuthStoreForAccess(t *testing.T) (*AuthStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "auth-access-test-*")
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
// RBACChecker Tests
// =============================================================================

func TestRBACCheckerAuthDisabled(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	// Create checker with auth disabled
	checker := NewRBACChecker(store, false)
	ctx := context.Background()

	// Should act as superuser when auth disabled
	if !checker.IsSuperuser(ctx) {
		t.Error("Expected superuser access when auth disabled")
	}

	// Should have access to all MCP items
	if !checker.CanAccessMCPItem(ctx, "any_tool") {
		t.Error("Expected MCP access when auth disabled")
	}

	// Should have full access to all connections
	canAccess, level := checker.CanAccessConnection(ctx, 99)
	if !canAccess || level != AccessLevelReadWrite {
		t.Error("Expected read_write connection access when auth disabled")
	}
}

func TestRBACCheckerSuperuserBypass(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create a restricted privilege
	store.RegisterMCPPrivilege("restricted_tool", MCPPrivilegeTypeTool, "Restricted")
	groupID, _ := store.CreateGroup("restricted-group", "Restricted group")
	store.GrantMCPPrivilegeByName(groupID, "restricted_tool")
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelRead)

	// Create context with superuser
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, true)

	// Superuser should bypass all checks
	if !checker.IsSuperuser(ctx) {
		t.Error("Expected superuser to return true")
	}

	if !checker.CanAccessMCPItem(ctx, "restricted_tool") {
		t.Error("Expected superuser to access restricted tool")
	}

	canAccess, level := checker.CanAccessConnection(ctx, 1)
	if !canAccess || level != AccessLevelReadWrite {
		t.Error("Expected superuser to have read_write access")
	}
}

func TestRBACCheckerUnrestrictedAccess(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Register a privilege but don't assign it to any group
	store.RegisterMCPPrivilege("public_tool", MCPPrivilegeTypeTool, "Public")

	// Create a non-superuser context
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, int64(1))

	// Unrestricted item should be accessible
	if !checker.CanAccessMCPItem(ctx, "public_tool") {
		t.Error("Expected access to unrestricted tool")
	}

	// Unrestricted connection should be accessible with full rights
	canAccess, level := checker.CanAccessConnection(ctx, 99)
	if !canAccess || level != AccessLevelReadWrite {
		t.Error("Expected read_write access to unrestricted connection")
	}
}

func TestRBACCheckerRestrictedAccess(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user, group, and privileges
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test group")
	store.AddUserToGroup(groupID, userID)

	// Create and grant privileges
	store.RegisterMCPPrivilege("restricted_tool", MCPPrivilegeTypeTool, "Restricted")
	store.GrantMCPPrivilegeByName(groupID, "restricted_tool")
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelRead)

	// Create context with user
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// User should have access to granted privilege
	if !checker.CanAccessMCPItem(ctx, "restricted_tool") {
		t.Error("Expected user to access granted tool")
	}

	// User should have read access to granted connection
	canAccess, level := checker.CanAccessConnection(ctx, 1)
	if !canAccess {
		t.Error("Expected user to access granted connection")
	}
	if level != AccessLevelRead {
		t.Errorf("Expected read access, got %s", level)
	}
}

func TestRBACCheckerDeniedAccess(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user without any group membership
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")

	// Create restricted privilege (assigned to a different group)
	otherGroupID, _ := store.CreateGroup("other-group", "Other group")
	store.RegisterMCPPrivilege("restricted_tool", MCPPrivilegeTypeTool, "Restricted")
	store.GrantMCPPrivilegeByName(otherGroupID, "restricted_tool")
	store.GrantConnectionPrivilege(otherGroupID, 1, AccessLevelReadWrite)

	// Create context with user
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// User should NOT have access to privilege granted to other group
	if checker.CanAccessMCPItem(ctx, "restricted_tool") {
		t.Error("Expected user to be denied access to restricted tool")
	}

	// User should NOT have access to connection granted to other group
	canAccess, _ := checker.CanAccessConnection(ctx, 1)
	if canAccess {
		t.Error("Expected user to be denied access to restricted connection")
	}
}

func TestRBACCheckerInheritedPrivileges(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user and hierarchical groups
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	parentGroupID, _ := store.CreateGroup("parent-group", "Parent")
	childGroupID, _ := store.CreateGroup("child-group", "Child")

	// Add child to parent, user to child
	store.AddGroupToGroup(parentGroupID, childGroupID)
	store.AddUserToGroup(childGroupID, userID)

	// Grant privilege to parent
	store.RegisterMCPPrivilege("parent_tool", MCPPrivilegeTypeTool, "Parent Tool")
	store.GrantMCPPrivilegeByName(parentGroupID, "parent_tool")
	store.GrantConnectionPrivilege(parentGroupID, 1, AccessLevelReadWrite)

	// Create context with user
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// User should inherit privilege from parent group
	if !checker.CanAccessMCPItem(ctx, "parent_tool") {
		t.Error("Expected user to inherit parent_tool privilege")
	}

	// User should inherit connection access from parent group
	canAccess, level := checker.CanAccessConnection(ctx, 1)
	if !canAccess || level != AccessLevelReadWrite {
		t.Error("Expected user to inherit connection privilege from parent")
	}
}

func TestRBACCheckerNoUserID(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create restricted privilege
	groupID, _ := store.CreateGroup("some-group", "Some group")
	store.RegisterMCPPrivilege("restricted_tool", MCPPrivilegeTypeTool, "Restricted")
	store.GrantMCPPrivilegeByName(groupID, "restricted_tool")

	// Create context without user ID (e.g., service token)
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)

	// Should be denied access to restricted items without user ID
	if checker.CanAccessMCPItem(ctx, "restricted_tool") {
		t.Error("Expected denial without user ID")
	}
}

func TestRBACCheckerTokenScoping(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user with privileges
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant multiple privileges
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B")
	store.GrantMCPPrivilegeByName(groupID, "tool_a")
	store.GrantMCPPrivilegeByName(groupID, "tool_b")
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelReadWrite)
	store.GrantConnectionPrivilege(groupID, 2, AccessLevelReadWrite)

	// Create token for user
	_, storedToken, _ := store.CreateToken("testuser", "User token", nil)

	// Scope token to only tool_a and connection 1
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"tool_a"})
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelReadWrite},
	})

	// Create context with user and scoped token
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	// tool_a should be accessible (in scope)
	if !checker.CanAccessMCPItem(ctx, "tool_a") {
		t.Error("Expected access to tool_a (in scope)")
	}

	// tool_b should NOT be accessible (not in scope)
	if checker.CanAccessMCPItem(ctx, "tool_b") {
		t.Error("Expected denial for tool_b (not in scope)")
	}

	// Connection 1 should be accessible (in scope)
	canAccess, _ := checker.CanAccessConnection(ctx, 1)
	if !canAccess {
		t.Error("Expected access to connection 1 (in scope)")
	}

	// Connection 2 should NOT be accessible (not in scope)
	canAccess, _ = checker.CanAccessConnection(ctx, 2)
	if canAccess {
		t.Error("Expected denial for connection 2 (not in scope)")
	}
}

// =============================================================================
// HasAdminPermission Tests
// =============================================================================

func TestHasAdminPermissionSuperuser(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, true)

	// Superuser always returns true regardless of permission
	if !checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("Expected superuser to have admin permission")
	}
}

func TestHasAdminPermissionAuthDisabled(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, false)
	ctx := context.Background()

	// Auth disabled returns true for any permission
	if !checker.HasAdminPermission(ctx, PermManageConnections) {
		t.Error("Expected true when auth disabled")
	}
}

func TestHasAdminPermissionGranted(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user in group with permission
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("admin-group", "Admin group")
	store.AddUserToGroup(groupID, userID)
	store.GrantAdminPermission(groupID, PermManageUsers)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	if !checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("Expected user with granted permission to return true")
	}
}

func TestHasAdminPermissionNotGranted(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user in group without the required permission
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("limited-group", "Limited group")
	store.AddUserToGroup(groupID, userID)
	store.GrantAdminPermission(groupID, PermManageConnections)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// User has manage_connections but not manage_users
	if checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("Expected false for permission not granted to user")
	}
}

func TestHasAdminPermissionNoUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Context without user ID
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)

	if checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("Expected false when no user in context")
	}
}

// =============================================================================
// GetEffectivePrivileges Tests
// =============================================================================

func TestGetEffectivePrivileges(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user with privileges
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant privileges
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B")
	store.GrantMCPPrivilegeByName(groupID, "tool_a")
	store.GrantMCPPrivilegeByName(groupID, "tool_b")
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelRead)
	store.GrantConnectionPrivilege(groupID, 2, AccessLevelReadWrite)

	// Create context with user
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// Get effective privileges
	privs := checker.GetEffectivePrivileges(ctx)

	if privs.IsSuperuser {
		t.Error("Expected non-superuser")
	}

	if len(privs.MCPPrivileges) != 2 {
		t.Errorf("Expected 2 MCP privileges, got %d", len(privs.MCPPrivileges))
	}

	if !privs.MCPPrivileges["tool_a"] || !privs.MCPPrivileges["tool_b"] {
		t.Error("Expected tool_a and tool_b in privileges")
	}

	if len(privs.ConnectionPrivileges) != 2 {
		t.Errorf("Expected 2 connection privileges, got %d", len(privs.ConnectionPrivileges))
	}

	if privs.ConnectionPrivileges[1] != AccessLevelRead {
		t.Errorf("Expected read for connection 1, got %s", privs.ConnectionPrivileges[1])
	}

	if privs.ConnectionPrivileges[2] != AccessLevelReadWrite {
		t.Errorf("Expected read_write for connection 2, got %s", privs.ConnectionPrivileges[2])
	}
}

func TestGetEffectivePrivilegesSuperuser(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create superuser context
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, true)

	// Get effective privileges
	privs := checker.GetEffectivePrivileges(ctx)

	if !privs.IsSuperuser {
		t.Error("Expected superuser flag to be true")
	}
}

// =============================================================================
// Helper Method Tests
// =============================================================================

func TestHasWriteAccess(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user with mixed access levels
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelRead)
	store.GrantConnectionPrivilege(groupID, 2, AccessLevelReadWrite)

	// Create context
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// Read-only connection
	if checker.HasWriteAccess(ctx, 1) {
		t.Error("Expected no write access to read-only connection")
	}

	// Read-write connection
	if !checker.HasWriteAccess(ctx, 2) {
		t.Error("Expected write access to read-write connection")
	}
}

func TestGetAccessibleConnections(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user with connection access
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelRead)
	store.GrantConnectionPrivilege(groupID, 2, AccessLevelReadWrite)

	// Create context
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// Get accessible connections
	connections := checker.GetAccessibleConnections(ctx)
	if len(connections) != 2 {
		t.Errorf("Expected 2 connections, got %d", len(connections))
	}

	// Superuser should return nil (all connections)
	superCtx := context.WithValue(context.Background(), IsSuperuserContextKey, true)
	connections = checker.GetAccessibleConnections(superCtx)
	if connections != nil {
		t.Error("Expected nil for superuser (all connections)")
	}
}

// =============================================================================
// DatabaseAccessChecker Tests
// =============================================================================

func TestNewDatabaseAccessChecker(t *testing.T) {
	tokenStore := InitializeTokenStore()
	checker := NewDatabaseAccessChecker(tokenStore, true, false)

	if checker == nil {
		t.Fatal("Expected non-nil checker")
	}
}

func TestDatabaseAccessChecker_AuthDisabled(t *testing.T) {
	tokenStore := InitializeTokenStore()
	checker := NewDatabaseAccessChecker(tokenStore, false, false)
	ctx := context.Background()

	if !checker.CanAccessDatabase(ctx) {
		t.Error("Expected database access when auth disabled")
	}
}

func TestDatabaseAccessChecker_APIToken(t *testing.T) {
	tokenStore := InitializeTokenStore()
	checker := NewDatabaseAccessChecker(tokenStore, true, false)

	ctx := context.WithValue(context.Background(), IsAPITokenContextKey, true)

	if !checker.CanAccessDatabase(ctx) {
		t.Error("Expected database access with API token")
	}
}

func TestDatabaseAccessChecker_SessionUser(t *testing.T) {
	tokenStore := InitializeTokenStore()
	checker := NewDatabaseAccessChecker(tokenStore, true, false)

	// With username in context
	ctx := context.WithValue(context.Background(), UsernameContextKey, "testuser")
	if !checker.CanAccessDatabase(ctx) {
		t.Error("Expected database access with session user")
	}

	// Without username in context
	ctx = context.Background()
	if checker.CanAccessDatabase(ctx) {
		t.Error("Expected no database access without authentication")
	}
}

func TestDatabaseAccessChecker_GetBoundDatabase(t *testing.T) {
	tokenStore := InitializeTokenStore()
	checker := NewDatabaseAccessChecker(tokenStore, true, false)
	ctx := context.Background()

	// Should always return empty string (single database mode)
	if db := checker.GetBoundDatabase(ctx); db != "" {
		t.Errorf("Expected empty string, got %q", db)
	}
}

// =============================================================================
// RBACChecker Nil Store Tests
// =============================================================================

func TestRBACCheckerNilStore(t *testing.T) {
	checker := NewRBACChecker(nil, true)
	ctx := context.Background()

	// Should act as superuser when store is nil
	if !checker.IsSuperuser(ctx) {
		t.Error("Expected superuser when store is nil")
	}

	if !checker.CanAccessMCPItem(ctx, "any_tool") {
		t.Error("Expected MCP access when store is nil")
	}

	canAccess, level := checker.CanAccessConnection(ctx, 99)
	if !canAccess || level != AccessLevelReadWrite {
		t.Error("Expected full connection access when store is nil")
	}

	if !checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("Expected admin permission when store is nil")
	}
}

// =============================================================================
// Connection Access with "All Connections" Grant Tests
// =============================================================================

func TestRBACCheckerAllConnectionsGrant(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user with "all connections" privilege
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant access to all connections (connection ID 0 = all)
	store.GrantConnectionPrivilege(groupID, ConnectionIDAll, AccessLevelRead)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// Should have access to any connection through "all" grant
	canAccess, level := checker.CanAccessConnection(ctx, 999)
	if !canAccess {
		t.Error("Expected access through 'all connections' grant")
	}
	if level != AccessLevelRead {
		t.Errorf("Expected 'read' access level, got %q", level)
	}
}

func TestRBACCheckerAllConnectionsHigherLevel(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user with mixed privileges
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant read to all connections, read_write to specific connection
	store.GrantConnectionPrivilege(groupID, ConnectionIDAll, AccessLevelRead)
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelReadWrite)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// Connection 1 should have read_write (specific grant wins)
	canAccess, level := checker.CanAccessConnection(ctx, 1)
	if !canAccess || level != AccessLevelReadWrite {
		t.Errorf("Expected read_write for connection 1, got canAccess=%v level=%q", canAccess, level)
	}

	// Other connections should have read (from "all" grant)
	canAccess, level = checker.CanAccessConnection(ctx, 2)
	if !canAccess || level != AccessLevelRead {
		t.Errorf("Expected read for connection 2, got canAccess=%v level=%q", canAccess, level)
	}
}

// =============================================================================
// GetEffectivePrivileges with Token Scoping Tests
// =============================================================================

func TestGetEffectivePrivilegesWithTokenScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user with privileges
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant multiple privileges
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B")
	store.GrantMCPPrivilegeByName(groupID, "tool_a")
	store.GrantMCPPrivilegeByName(groupID, "tool_b")
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelReadWrite)
	store.GrantConnectionPrivilege(groupID, 2, AccessLevelReadWrite)

	// Create token with scope limiting to tool_a and connection 1
	_, storedToken, _ := store.CreateToken("testuser", "Scoped token", nil)
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"tool_a"})
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelReadWrite},
	})

	// Create context with user and scoped token
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	privs := checker.GetEffectivePrivileges(ctx)

	// Should only have tool_a (scoped)
	if len(privs.MCPPrivileges) != 1 || !privs.MCPPrivileges["tool_a"] {
		t.Error("Expected only tool_a in scoped privileges")
	}

	// Should only have connection 1 (scoped)
	if len(privs.ConnectionPrivileges) != 1 {
		t.Errorf("Expected 1 connection in scoped privileges, got %d", len(privs.ConnectionPrivileges))
	}
}

// =============================================================================
// Wildcard Token Scope Tests
// =============================================================================

func TestRBACCheckerTokenScopingMCPWildcard(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user with privileges
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant multiple privileges
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B")
	store.GrantMCPPrivilegeByName(groupID, "tool_a")
	store.GrantMCPPrivilegeByName(groupID, "tool_b")

	// Create token with wildcard MCP scope
	_, storedToken, _ := store.CreateToken("testuser", "Wildcard token", nil)
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"*"})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	// Both tools should be accessible with wildcard
	if !checker.CanAccessMCPItem(ctx, "tool_a") {
		t.Error("Expected wildcard to allow access to tool_a")
	}
	if !checker.CanAccessMCPItem(ctx, "tool_b") {
		t.Error("Expected wildcard to allow access to tool_b")
	}
}

func TestRBACCheckerTokenScopingAdminWildcard(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user with admin permissions
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)
	store.GrantAdminPermission(groupID, PermManageUsers)
	store.GrantAdminPermission(groupID, PermManageConnections)

	// Create token with wildcard admin scope
	_, storedToken, _ := store.CreateToken("testuser", "Wildcard token", nil)
	store.SetTokenAdminScope(storedToken.ID, []string{"*"})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	// Both admin permissions should be accessible with wildcard
	if !checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("Expected wildcard to allow manage_users")
	}
	if !checker.HasAdminPermission(ctx, PermManageConnections) {
		t.Error("Expected wildcard to allow manage_connections")
	}
}

func TestRBACCheckerTokenScopingConnectionWildcard(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user with connection privileges
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelReadWrite)
	store.GrantConnectionPrivilege(groupID, 2, AccessLevelReadWrite)

	// Create token with wildcard connection scope
	_, storedToken, _ := store.CreateToken("testuser", "Wildcard token", nil)
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelReadWrite},
	})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	// Both connections should be accessible with wildcard
	canAccess, level := checker.CanAccessConnection(ctx, 1)
	if !canAccess || level != AccessLevelReadWrite {
		t.Errorf("Expected read_write for connection 1, got canAccess=%v level=%q", canAccess, level)
	}

	canAccess, level = checker.CanAccessConnection(ctx, 2)
	if !canAccess || level != AccessLevelReadWrite {
		t.Errorf("Expected read_write for connection 2, got canAccess=%v level=%q", canAccess, level)
	}
}

func TestGetEffectivePrivilegesWithMCPWildcard(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user with privileges
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B")
	store.GrantMCPPrivilegeByName(groupID, "tool_a")
	store.GrantMCPPrivilegeByName(groupID, "tool_b")
	store.GrantAdminPermission(groupID, PermManageUsers)
	store.GrantAdminPermission(groupID, PermManageConnections)

	// Create token with wildcard MCP and admin scopes
	_, storedToken, _ := store.CreateToken("testuser", "Wildcard token", nil)
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"*"})
	store.SetTokenAdminScope(storedToken.ID, []string{"*"})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	privs := checker.GetEffectivePrivileges(ctx)

	// Wildcard MCP scope should keep all user's MCP privileges
	if len(privs.MCPPrivileges) != 2 {
		t.Errorf("Expected 2 MCP privileges with wildcard, got %d", len(privs.MCPPrivileges))
	}
	if !privs.MCPPrivileges["tool_a"] || !privs.MCPPrivileges["tool_b"] {
		t.Error("Expected both tool_a and tool_b with wildcard")
	}

	// Wildcard admin scope should keep all user's admin permissions
	if len(privs.AdminPermissions) != 2 {
		t.Errorf("Expected 2 admin permissions with wildcard, got %d", len(privs.AdminPermissions))
	}
	if !privs.AdminPermissions[PermManageUsers] || !privs.AdminPermissions[PermManageConnections] {
		t.Error("Expected both manage_users and manage_connections with wildcard")
	}
}

// =============================================================================
// GetEffectivePrivileges No User Tests
// =============================================================================

func TestGetEffectivePrivilegesNoUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Context without user ID
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)

	privs := checker.GetEffectivePrivileges(ctx)

	if privs.IsSuperuser {
		t.Error("Expected non-superuser")
	}

	if len(privs.MCPPrivileges) != 0 {
		t.Errorf("Expected 0 MCP privileges without user, got %d", len(privs.MCPPrivileges))
	}

	if len(privs.ConnectionPrivileges) != 0 {
		t.Errorf("Expected 0 connection privileges without user, got %d", len(privs.ConnectionPrivileges))
	}
}

// =============================================================================
// Admin Permission Edge Cases
// =============================================================================

func TestHasAdminPermissionInheritedFromNestedGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store, true)

	// Create user and group hierarchy
	store.CreateUser("testuser", "password", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	parentID, _ := store.CreateGroup("parent-group", "Parent")
	childID, _ := store.CreateGroup("child-group", "Child")
	grandchildID, _ := store.CreateGroup("grandchild-group", "Grandchild")

	// Build hierarchy: parent -> child -> grandchild -> user
	store.AddGroupToGroup(parentID, childID)
	store.AddGroupToGroup(childID, grandchildID)
	store.AddUserToGroup(grandchildID, userID)

	// Grant permission to parent only
	store.GrantAdminPermission(parentID, PermManageUsers)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// User should inherit permission through nested group hierarchy
	if !checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("Expected user to inherit admin permission from parent through nested groups")
	}
}
