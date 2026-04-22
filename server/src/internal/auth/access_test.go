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
	"fmt"
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

func TestRBACCheckerSuperuserBypass(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Create a restricted privilege
	store.RegisterMCPPrivilege("restricted_tool", MCPPrivilegeTypeTool, "Restricted", false)
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

func TestRBACCheckerPublicAccess(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Register a public privilege (accessible without group membership)
	store.RegisterMCPPrivilege("public_tool", MCPPrivilegeTypeTool, "Public", true)

	// Create a non-superuser context
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, int64(1))

	// Public item should be accessible without group membership
	if !checker.CanAccessMCPItem(ctx, "public_tool") {
		t.Error("Expected access to public tool")
	}

	// Unrestricted connection should be accessible with full rights
	canAccess, level := checker.CanAccessConnection(ctx, 99)
	if !canAccess || level != AccessLevelReadWrite {
		t.Error("Expected read_write access to unrestricted connection")
	}
}

func TestRBACCheckerUnregisteredToolDenied(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Create a non-superuser context
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, int64(1))

	// Unregistered tool should be denied (fail-safe)
	if checker.CanAccessMCPItem(ctx, "unregistered_tool") {
		t.Error("Expected denial for unregistered tool")
	}
}

func TestRBACCheckerRestrictedAccess(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Create user, group, and privileges
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test group")
	store.AddUserToGroup(groupID, userID)

	// Create and grant privileges
	store.RegisterMCPPrivilege("restricted_tool", MCPPrivilegeTypeTool, "Restricted", false)
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

	checker := NewRBACChecker(store)

	// Create user without any group membership
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")

	// Create restricted privilege (assigned to a different group)
	otherGroupID, _ := store.CreateGroup("other-group", "Other group")
	store.RegisterMCPPrivilege("restricted_tool", MCPPrivilegeTypeTool, "Restricted", false)
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

	checker := NewRBACChecker(store)

	// Create user and hierarchical groups
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	parentGroupID, _ := store.CreateGroup("parent-group", "Parent")
	childGroupID, _ := store.CreateGroup("child-group", "Child")

	// Add child to parent, user to child
	store.AddGroupToGroup(parentGroupID, childGroupID)
	store.AddUserToGroup(childGroupID, userID)

	// Grant privilege to parent
	store.RegisterMCPPrivilege("parent_tool", MCPPrivilegeTypeTool, "Parent Tool", false)
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

	checker := NewRBACChecker(store)

	// Create restricted privilege
	groupID, _ := store.CreateGroup("some-group", "Some group")
	store.RegisterMCPPrivilege("restricted_tool", MCPPrivilegeTypeTool, "Restricted", false)
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

	checker := NewRBACChecker(store)

	// Create user with privileges
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant multiple privileges
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B", false)
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

	checker := NewRBACChecker(store)
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, true)

	// Superuser always returns true regardless of permission
	if !checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("Expected superuser to have admin permission")
	}
}

func TestHasAdminPermissionGranted(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Create user in group with permission
	store.CreateUser("testuser", "Password1", "Test user", "", "")
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

	checker := NewRBACChecker(store)

	// Create user in group without the required permission
	store.CreateUser("testuser", "Password1", "Test user", "", "")
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

	checker := NewRBACChecker(store)

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

	checker := NewRBACChecker(store)

	// Create user with privileges
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant privileges
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B", false)
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

	checker := NewRBACChecker(store)

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

	checker := NewRBACChecker(store)

	// Create user with mixed access levels
	store.CreateUser("testuser", "Password1", "Test user", "", "")
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

// =============================================================================
// DatabaseAccessChecker Tests
// =============================================================================

func TestNewDatabaseAccessChecker(t *testing.T) {
	checker := NewDatabaseAccessChecker()

	if checker == nil {
		t.Fatal("Expected non-nil checker")
	}
}

func TestDatabaseAccessChecker_APIToken(t *testing.T) {
	checker := NewDatabaseAccessChecker()

	ctx := context.WithValue(context.Background(), IsAPITokenContextKey, true)

	if !checker.CanAccessDatabase(ctx) {
		t.Error("Expected database access with API token")
	}
}

func TestDatabaseAccessChecker_SessionUser(t *testing.T) {
	checker := NewDatabaseAccessChecker()

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
	checker := NewDatabaseAccessChecker()
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
	checker := NewRBACChecker(nil)
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

	checker := NewRBACChecker(store)

	// Create user with "all connections" privilege
	store.CreateUser("testuser", "Password1", "Test user", "", "")
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

	checker := NewRBACChecker(store)

	// Create user with mixed privileges
	store.CreateUser("testuser", "Password1", "Test user", "", "")
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

	checker := NewRBACChecker(store)

	// Create user with privileges
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant multiple privileges
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B", false)
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

	checker := NewRBACChecker(store)

	// Create user with privileges
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant multiple privileges
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B", false)
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

	checker := NewRBACChecker(store)

	// Create user with admin permissions
	store.CreateUser("testuser", "Password1", "Test user", "", "")
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

	checker := NewRBACChecker(store)

	// Create user with connection privileges
	store.CreateUser("testuser", "Password1", "Test user", "", "")
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

	checker := NewRBACChecker(store)

	// Create user with privileges
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B", false)
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

	checker := NewRBACChecker(store)

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

	checker := NewRBACChecker(store)

	// Create user and group hierarchy
	store.CreateUser("testuser", "Password1", "Test user", "", "")
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

// =============================================================================
// Wildcard MCP Privilege with Unregistered Tool Tests (Issue #28)
// =============================================================================

func TestRBACCheckerPublicToolWithWildcardGrant(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Register store_memory as a public tool (like in production)
	store.RegisterMCPPrivilege("store_memory", MCPPrivilegeTypeTool, "Store memory", true)

	// Create a group with wildcard MCP privilege (All MCP Privileges)
	store.CreateUser("wildcarduser", "Password1", "Wildcard user", "", "")
	wildcardUserID, _ := store.GetUserID("wildcarduser")
	groupID, _ := store.CreateGroup("wildcard-group", "Wildcard group")
	store.AddUserToGroup(groupID, wildcardUserID)
	store.GrantMCPPrivilegeByName(groupID, "*")

	// Create a user NOT in any group
	store.CreateUser("nogroup", "Password1", "No group user", "", "")
	noGroupUserID, _ := store.GetUserID("nogroup")

	// User in wildcarded group should access the public tool
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, wildcardUserID)

	if !checker.CanAccessMCPItem(ctx, "store_memory") {
		t.Error("Expected user in wildcarded group to access public tool 'store_memory'")
	}

	// User NOT in any group should ALSO access the public tool (it's public!)
	ctx2 := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx2 = context.WithValue(ctx2, UserIDContextKey, noGroupUserID)

	if !checker.CanAccessMCPItem(ctx2, "store_memory") {
		t.Error("Expected user without group to access public tool 'store_memory'")
	}

	// But user without group should be denied unregistered tools (fail-safe)
	if checker.CanAccessMCPItem(ctx2, "completely_unknown_tool") {
		t.Error("Expected user without group to be denied access to unregistered tool")
	}
}

func TestRBACCheckerUnregisteredToolDeniedByDefault(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Create a user (no groups have any MCP grants at all)
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// With the new "restrictive by default" RBAC model, unregistered tools
	// should be DENIED (fail-safe for unknown tools - Issue #28 fix)
	if checker.CanAccessMCPItem(ctx, "completely_unknown_tool") {
		t.Error("Expected denial for unregistered tool (fail-safe behavior)")
	}

	// But registered PUBLIC tools should still be accessible without group membership
	store.RegisterMCPPrivilege("store_memory", MCPPrivilegeTypeTool, "Store memory", true)
	if !checker.CanAccessMCPItem(ctx, "store_memory") {
		t.Error("Expected access to registered public tool even without group membership")
	}
}

func TestRBACCheckerRegisteredToolWithWildcardGrant(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Register a tool
	store.RegisterMCPPrivilege("query_database", MCPPrivilegeTypeTool, "Execute queries", false)

	// Create a group with wildcard MCP privilege
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("wildcard-group", "Wildcard group")
	store.AddUserToGroup(groupID, userID)
	store.GrantMCPPrivilegeByName(groupID, "*")

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// User in wildcarded group should access registered restricted tools
	if !checker.CanAccessMCPItem(ctx, "query_database") {
		t.Error("Expected user in wildcarded group to access registered tool 'query_database'")
	}
}

func TestGetUserMCPPrivilegesIncludesWildcard(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	// Create user with wildcard MCP access via a group
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("wildcard-group", "Wildcard group")
	store.AddUserToGroup(groupID, userID)
	store.GrantMCPPrivilegeByName(groupID, "*")

	// GetUserMCPPrivileges should return "*": true
	privileges, err := store.GetUserMCPPrivileges(userID)
	if err != nil {
		t.Fatalf("Failed to get user MCP privileges: %v", err)
	}

	if !privileges["*"] {
		t.Error("Expected wildcard '*' in user MCP privileges when group has wildcard grant")
	}
}

// mockSharingLookup returns a ConnectionSharingLookupFunc that serves
// a fixed map of connection sharing info.
func mockSharingLookup(info map[int]struct {
	isShared      bool
	ownerUsername string
}) ConnectionSharingLookupFunc {
	return func(_ context.Context, connectionID int) (bool, string, error) {
		if entry, ok := info[connectionID]; ok {
			return entry.isShared, entry.ownerUsername, nil
		}
		return false, "", fmt.Errorf("connection %d not found", connectionID)
	}
}

func TestCanAccessConnection_UnsharedOwnerOnly(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Connection 10 is not shared and owned by "alice"
	checker.SetConnectionSharingLookup(mockSharingLookup(map[int]struct {
		isShared      bool
		ownerUsername string
	}{
		10: {isShared: false, ownerUsername: "alice"},
	}))

	// Alice should have access
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, int64(1))
	ctx = context.WithValue(ctx, UsernameContextKey, "alice")

	canAccess, level := checker.CanAccessConnection(ctx, 10)
	if !canAccess {
		t.Error("Expected owner alice to access her own unshared connection")
	}
	if level != AccessLevelReadWrite {
		t.Errorf("Expected read_write, got %s", level)
	}

	// Bob should NOT have access
	ctx2 := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx2 = context.WithValue(ctx2, UserIDContextKey, int64(2))
	ctx2 = context.WithValue(ctx2, UsernameContextKey, "bob")

	canAccess, _ = checker.CanAccessConnection(ctx2, 10)
	if canAccess {
		t.Error("Expected non-owner bob to be denied access to unshared connection")
	}
}

func TestCanAccessConnection_SharedAccessForAll(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Connection 20 is shared and owned by "alice"
	checker.SetConnectionSharingLookup(mockSharingLookup(map[int]struct {
		isShared      bool
		ownerUsername string
	}{
		20: {isShared: true, ownerUsername: "alice"},
	}))

	// Bob should have access because the connection is shared
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, int64(2))
	ctx = context.WithValue(ctx, UsernameContextKey, "bob")

	canAccess, level := checker.CanAccessConnection(ctx, 20)
	if !canAccess {
		t.Error("Expected bob to access shared connection")
	}
	if level != AccessLevelReadWrite {
		t.Errorf("Expected read_write, got %s", level)
	}
}

func TestCanAccessConnection_SuperuserBypassesSharing(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Connection 30 is not shared
	checker.SetConnectionSharingLookup(mockSharingLookup(map[int]struct {
		isShared      bool
		ownerUsername string
	}{
		30: {isShared: false, ownerUsername: "alice"},
	}))

	// Superuser should always have access
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, true)
	ctx = context.WithValue(ctx, UserIDContextKey, int64(99))
	ctx = context.WithValue(ctx, UsernameContextKey, "admin")

	canAccess, level := checker.CanAccessConnection(ctx, 30)
	if !canAccess {
		t.Error("Expected superuser to access unshared connection")
	}
	if level != AccessLevelReadWrite {
		t.Errorf("Expected read_write, got %s", level)
	}
}

func TestCanAccessConnection_NoLookupFuncAllowsAccess(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)
	// No sharing lookup function set - backward compatibility

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, int64(1))

	// Unrestricted connection without sharing lookup should be accessible
	canAccess, level := checker.CanAccessConnection(ctx, 99)
	if !canAccess {
		t.Error("Expected access when no sharing lookup is configured")
	}
	if level != AccessLevelReadWrite {
		t.Errorf("Expected read_write, got %s", level)
	}
}

// =============================================================================
// VisibleConnectionIDs Tests (regression coverage for issue #35)
// =============================================================================

// stubVisibilityLister is a testing implementation of
// ConnectionVisibilityLister that returns a fixed list of connections.
type stubVisibilityLister struct {
	connections []ConnectionVisibilityInfo
	err         error
}

func (s *stubVisibilityLister) GetAllConnections(_ context.Context) ([]ConnectionVisibilityInfo, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.connections, nil
}

// idSet converts a slice of IDs into a set for order-independent assertions.
func idSet(ids []int) map[int]bool {
	out := make(map[int]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func TestVisibleConnectionIDs_Superuser_AllConnections(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, true)

	ids, all, err := checker.VisibleConnectionIDs(ctx, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !all {
		t.Error("Expected allConnections=true for superuser")
	}
	if ids != nil {
		t.Errorf("Expected nil ids for superuser, got %v", ids)
	}
}

func TestVisibleConnectionIDs_WildcardTokenScope_AllConnections(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	// Create a user and a group with a wildcard connection grant.
	if err := store.CreateUser("wc", "Password1", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, _ := store.GetUserID("wc")
	groupID, err := store.CreateGroup("wc_group", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.AddUserToGroup(groupID, userID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if err := store.GrantConnectionPrivilege(groupID, ConnectionIDAll, AccessLevelReadWrite); err != nil {
		t.Fatalf("GrantConnectionPrivilege: %v", err)
	}

	checker := NewRBACChecker(store)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, UsernameContextKey, "wc")

	ids, all, err := checker.VisibleConnectionIDs(ctx, &stubVisibilityLister{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !all {
		t.Error("Expected allConnections=true for wildcard group grant")
	}
	if ids != nil {
		t.Errorf("Expected nil ids for wildcard, got %v", ids)
	}
}

func TestVisibleConnectionIDs_OwnerSeesOwnUnshared(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	if err := store.CreateUser("alice", "Password1", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, _ := store.GetUserID("alice")

	checker := NewRBACChecker(store)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, UsernameContextKey, "alice")

	lister := &stubVisibilityLister{
		connections: []ConnectionVisibilityInfo{
			{ID: 1, IsShared: false, OwnerUsername: "alice"},
			{ID: 2, IsShared: false, OwnerUsername: "bob"},
		},
	}

	ids, all, err := checker.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if all {
		t.Error("Expected allConnections=false for non-superuser")
	}
	set := idSet(ids)
	if !set[1] {
		t.Error("Expected alice to see her own unshared connection 1")
	}
	if set[2] {
		t.Error("Expected alice NOT to see bob's unshared connection 2")
	}
}

func TestVisibleConnectionIDs_NonOwnerDeniedUnshared(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	if err := store.CreateUser("bob", "Password1", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, _ := store.GetUserID("bob")

	checker := NewRBACChecker(store)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, UsernameContextKey, "bob")

	lister := &stubVisibilityLister{
		connections: []ConnectionVisibilityInfo{
			{ID: 1, IsShared: false, OwnerUsername: "alice"},
			{ID: 2, IsShared: false, OwnerUsername: "carol"},
		},
	}

	// Bob has zero explicit grants and does not own either connection;
	// he must see neither.
	ids, all, err := checker.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if all {
		t.Error("Expected allConnections=false")
	}
	if len(ids) != 0 {
		t.Errorf("Expected empty visible set, got %v", ids)
	}
}

func TestVisibleConnectionIDs_SharedVisibleWithoutGrant(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	if err := store.CreateUser("bob", "Password1", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, _ := store.GetUserID("bob")

	checker := NewRBACChecker(store)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, UsernameContextKey, "bob")

	lister := &stubVisibilityLister{
		connections: []ConnectionVisibilityInfo{
			{ID: 1, IsShared: true, OwnerUsername: "alice"},
			{ID: 2, IsShared: false, OwnerUsername: "alice"},
		},
	}

	ids, all, err := checker.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if all {
		t.Error("Expected allConnections=false")
	}
	set := idSet(ids)
	if !set[1] {
		t.Error("Expected bob to see shared connection 1")
	}
	if set[2] {
		t.Error("Expected bob NOT to see unshared connection 2")
	}
}

func TestVisibleConnectionIDs_ExplicitGrantIntersectsSharedVisibility(t *testing.T) {
	// When a user has explicit group/token grants, those grants act as an
	// allow-list that further restricts shared-connection visibility.
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	if err := store.CreateUser("carol", "Password1", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, _ := store.GetUserID("carol")
	groupID, err := store.CreateGroup("carol_group", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.AddUserToGroup(groupID, userID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	// Grant only connection 5. Connection 1 is shared but not granted.
	if err := store.GrantConnectionPrivilege(groupID, 5, AccessLevelRead); err != nil {
		t.Fatalf("GrantConnectionPrivilege: %v", err)
	}

	checker := NewRBACChecker(store)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, UsernameContextKey, "carol")

	lister := &stubVisibilityLister{
		connections: []ConnectionVisibilityInfo{
			{ID: 1, IsShared: true, OwnerUsername: "alice"},
			{ID: 5, IsShared: false, OwnerUsername: "alice"},
			{ID: 7, IsShared: false, OwnerUsername: "alice"},
		},
	}

	ids, all, err := checker.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if all {
		t.Error("Expected allConnections=false")
	}
	set := idSet(ids)
	if !set[5] {
		t.Error("Expected carol to see explicitly granted connection 5")
	}
	if set[1] {
		t.Error("Explicit grants should restrict shared visibility: conn 1 should be hidden")
	}
	if set[7] {
		t.Error("Expected carol NOT to see unshared connection 7")
	}
}

func TestVisibleConnectionIDs_NilStore_AllConnections(t *testing.T) {
	checker := NewRBACChecker(nil)

	ids, all, err := checker.VisibleConnectionIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !all {
		t.Error("Expected allConnections=true for nil store")
	}
	if ids != nil {
		t.Errorf("Expected nil ids, got %v", ids)
	}
}

func TestVisibleConnectionIDs_ListerError_Propagates(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	if err := store.CreateUser("dave", "Password1", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, _ := store.GetUserID("dave")

	checker := NewRBACChecker(store)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, UsernameContextKey, "dave")

	lister := &stubVisibilityLister{err: fmt.Errorf("boom")}

	_, _, err := checker.VisibleConnectionIDs(ctx, lister)
	if err == nil {
		t.Fatal("Expected error from lister to propagate, got nil")
	}
}

// =============================================================================
// GetEffectivePrivileges Scoped Token Intersection Tests (Issue #83)
//
// Issue #83: GET /api/v1/connections returned an empty array when a
// scoped token's connection list intersected with the user's access
// that came through a ConnectionIDAll wildcard grant. The intersection
// in GetEffectivePrivileges ignored the wildcard entry and silently
// dropped every scoped connection. These tests pin the corrected
// behavior: a scoped token keeps its connection IDs whenever the user
// has access via either a specific grant OR the wildcard, honoring the
// "token scope can restrict but not elevate" rule.
// =============================================================================

func TestGetEffectivePrivilegesScopedTokenWithUserWildcard(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// User has access to all connections via the ConnectionIDAll
	// wildcard at read_write. Prior to the fix, a scoped token that
	// named a specific connection would be intersected against the
	// ConnectionPrivileges map and the scoped entry would be dropped
	// because only ConnectionIDAll (0) was present.
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)
	store.GrantConnectionPrivilege(groupID, ConnectionIDAll, AccessLevelReadWrite)

	// Token scoped to connection 11 with read-only access.
	_, storedToken, _ := store.CreateToken("testuser", "Scoped token", nil)
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: 11, AccessLevel: AccessLevelRead},
	})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	privs := checker.GetEffectivePrivileges(ctx)

	if len(privs.ConnectionPrivileges) != 1 {
		t.Fatalf("Expected exactly 1 connection privilege, got %d: %+v",
			len(privs.ConnectionPrivileges), privs.ConnectionPrivileges)
	}
	level, ok := privs.ConnectionPrivileges[11]
	if !ok {
		t.Fatalf("Expected connection 11 in privileges, got %+v",
			privs.ConnectionPrivileges)
	}
	if level != AccessLevelRead {
		t.Errorf("Expected read for connection 11 (token ceiling), got %q",
			level)
	}
	// ConnectionIDAll must not survive a non-wildcard token scope.
	if _, hasAll := privs.ConnectionPrivileges[ConnectionIDAll]; hasAll {
		t.Error("Expected ConnectionIDAll to be removed by non-wildcard scope")
	}
}

func TestGetEffectivePrivilegesScopedTokenIntersectsSpecificGrant(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// User has access ONLY to connection 11 (no wildcard). A token
	// scoped to {11, 12} must keep 11 and drop 12 because 12 is not in
	// the user's grant map.
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)
	store.GrantConnectionPrivilege(groupID, 11, AccessLevelReadWrite)

	_, storedToken, _ := store.CreateToken("testuser", "Scoped token", nil)
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: 11, AccessLevel: AccessLevelRead},
		{ConnectionID: 12, AccessLevel: AccessLevelRead},
	})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	privs := checker.GetEffectivePrivileges(ctx)

	if len(privs.ConnectionPrivileges) != 1 {
		t.Fatalf("Expected exactly 1 connection privilege, got %d: %+v",
			len(privs.ConnectionPrivileges), privs.ConnectionPrivileges)
	}
	level, ok := privs.ConnectionPrivileges[11]
	if !ok {
		t.Fatalf("Expected connection 11 in privileges, got %+v",
			privs.ConnectionPrivileges)
	}
	if level != AccessLevelRead {
		t.Errorf("Expected read for connection 11 (token ceiling), got %q",
			level)
	}
	if _, has12 := privs.ConnectionPrivileges[12]; has12 {
		t.Error("Expected connection 12 to be dropped (no user grant)")
	}
}

func TestGetEffectivePrivilegesScopedTokenWildcardUserReadOnly(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// User's wildcard grant is read-only. A token scope that requests
	// read_write for connection 11 must be capped to read; the user's
	// access is the ceiling, not the token's scope.
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)
	store.GrantConnectionPrivilege(groupID, ConnectionIDAll, AccessLevelRead)

	_, storedToken, _ := store.CreateToken("testuser", "Scoped token", nil)
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: 11, AccessLevel: AccessLevelReadWrite},
	})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	privs := checker.GetEffectivePrivileges(ctx)

	if len(privs.ConnectionPrivileges) != 1 {
		t.Fatalf("Expected exactly 1 connection privilege, got %d: %+v",
			len(privs.ConnectionPrivileges), privs.ConnectionPrivileges)
	}
	level, ok := privs.ConnectionPrivileges[11]
	if !ok {
		t.Fatalf("Expected connection 11 in privileges, got %+v",
			privs.ConnectionPrivileges)
	}
	if level != AccessLevelRead {
		t.Errorf("Expected read for connection 11 (user ceiling), got %q",
			level)
	}
}

// =============================================================================
// GetEffectivePrivileges MCP/Admin Wildcard Scope Tests (Issue #96)
//
// Issue #96: GetEffectivePrivileges ignored user wildcard grants ("*") when
// intersecting with a scoped token. If a user's ONLY MCP or admin access came
// through a wildcard group grant, a token scoped to specific privileges would
// silently drop those privileges. These tests pin the corrected behavior.
// =============================================================================

func TestGetEffectivePrivilegesScopedTokenWithUserMCPWildcard(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// User has access to all MCP privileges via the wildcard "*" group grant.
	// Prior to the fix, a scoped token that named specific MCP privileges
	// would be intersected against the MCPPrivileges map and the scoped
	// entries would be dropped because only "*" was present.
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Register MCP privileges and grant wildcard access
	store.RegisterMCPPrivilege("tool_alpha", MCPPrivilegeTypeTool, "Tool Alpha", false)
	store.RegisterMCPPrivilege("tool_beta", MCPPrivilegeTypeTool, "Tool Beta", false)
	store.GrantMCPPrivilegeByName(groupID, "*") // Wildcard grant ONLY

	// Token scoped to tool_alpha only
	_, storedToken, _ := store.CreateToken("testuser", "Scoped token", nil)
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"tool_alpha"})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	privs := checker.GetEffectivePrivileges(ctx)

	// Should have tool_alpha (in token scope and user has wildcard access)
	if len(privs.MCPPrivileges) != 1 {
		t.Fatalf("Expected exactly 1 MCP privilege, got %d: %+v",
			len(privs.MCPPrivileges), privs.MCPPrivileges)
	}
	if !privs.MCPPrivileges["tool_alpha"] {
		t.Fatalf("Expected tool_alpha in privileges, got %+v",
			privs.MCPPrivileges)
	}
	// Wildcard "*" must not survive a non-wildcard token scope
	if privs.MCPPrivileges["*"] {
		t.Error("Expected '*' to be removed by non-wildcard scope")
	}
	// tool_beta should NOT be present (not in token scope)
	if privs.MCPPrivileges["tool_beta"] {
		t.Error("Expected tool_beta to be excluded (not in token scope)")
	}
}

func TestGetEffectivePrivilegesScopedTokenWithUserAdminWildcard(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// User has access to all admin permissions via the wildcard "*" group grant.
	// Prior to the fix, a scoped token that named specific admin permissions
	// would be intersected against the AdminPermissions map and the scoped
	// entries would be dropped because only "*" was present.
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant wildcard admin access ONLY (no specific permissions)
	store.GrantAdminPermission(groupID, AdminPermissionWildcard)

	// Token scoped to manage_users only
	_, storedToken, _ := store.CreateToken("testuser", "Scoped token", nil)
	store.SetTokenAdminScope(storedToken.ID, []string{PermManageUsers})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	privs := checker.GetEffectivePrivileges(ctx)

	// Should have manage_users (in token scope and user has wildcard access)
	if len(privs.AdminPermissions) != 1 {
		t.Fatalf("Expected exactly 1 admin permission, got %d: %+v",
			len(privs.AdminPermissions), privs.AdminPermissions)
	}
	if !privs.AdminPermissions[PermManageUsers] {
		t.Fatalf("Expected %s in permissions, got %+v",
			PermManageUsers, privs.AdminPermissions)
	}
	// Wildcard "*" must not survive a non-wildcard token scope
	if privs.AdminPermissions[AdminPermissionWildcard] {
		t.Error("Expected '*' to be removed by non-wildcard scope")
	}
	// manage_connections should NOT be present (not in token scope)
	if privs.AdminPermissions[PermManageConnections] {
		t.Error("Expected manage_connections to be excluded (not in token scope)")
	}
}

func TestGetEffectivePrivilegesMCPWildcardUserMultipleScopedPrivileges(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// User has wildcard MCP access only. Token scopes to multiple privileges.
	// All scoped privileges should resolve successfully.
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Register several MCP privileges
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B", false)
	store.RegisterMCPPrivilege("tool_c", MCPPrivilegeTypeTool, "Tool C", false)
	store.GrantMCPPrivilegeByName(groupID, "*") // Wildcard grant ONLY

	// Token scoped to tool_a and tool_b
	_, storedToken, _ := store.CreateToken("testuser", "Scoped token", nil)
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"tool_a", "tool_b"})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	privs := checker.GetEffectivePrivileges(ctx)

	// Should have both tool_a and tool_b
	if len(privs.MCPPrivileges) != 2 {
		t.Fatalf("Expected exactly 2 MCP privileges, got %d: %+v",
			len(privs.MCPPrivileges), privs.MCPPrivileges)
	}
	if !privs.MCPPrivileges["tool_a"] {
		t.Error("Expected tool_a in privileges")
	}
	if !privs.MCPPrivileges["tool_b"] {
		t.Error("Expected tool_b in privileges")
	}
	// tool_c should NOT be present (not in token scope)
	if privs.MCPPrivileges["tool_c"] {
		t.Error("Expected tool_c to be excluded (not in token scope)")
	}
}

func TestGetEffectivePrivilegesAdminWildcardUserMultipleScopedPermissions(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// User has wildcard admin access only. Token scopes to multiple permissions.
	// All scoped permissions should resolve successfully.
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant wildcard admin access ONLY
	store.GrantAdminPermission(groupID, AdminPermissionWildcard)

	// Token scoped to manage_users and manage_connections
	_, storedToken, _ := store.CreateToken("testuser", "Scoped token", nil)
	store.SetTokenAdminScope(storedToken.ID, []string{PermManageUsers, PermManageConnections})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	privs := checker.GetEffectivePrivileges(ctx)

	// Should have both manage_users and manage_connections
	if len(privs.AdminPermissions) != 2 {
		t.Fatalf("Expected exactly 2 admin permissions, got %d: %+v",
			len(privs.AdminPermissions), privs.AdminPermissions)
	}
	if !privs.AdminPermissions[PermManageUsers] {
		t.Error("Expected manage_users in permissions")
	}
	if !privs.AdminPermissions[PermManageConnections] {
		t.Error("Expected manage_connections in permissions")
	}
	// manage_groups should NOT be present (not in token scope)
	if privs.AdminPermissions[PermManageGroups] {
		t.Error("Expected manage_groups to be excluded (not in token scope)")
	}
}

// =============================================================================
// AccessLevelNone Constant Tests (Issue #96)
// =============================================================================

func TestAccessLevelNoneConstant(t *testing.T) {
	// Verify AccessLevelNone equals empty string for backward compatibility
	if AccessLevelNone != "" {
		t.Errorf("Expected AccessLevelNone to equal empty string, got %q", AccessLevelNone)
	}

	// Verify AccessLevelNone is distinct from Read and ReadWrite
	if AccessLevelNone == AccessLevelRead {
		t.Error("AccessLevelNone should not equal AccessLevelRead")
	}
	if AccessLevelNone == AccessLevelReadWrite {
		t.Error("AccessLevelNone should not equal AccessLevelReadWrite")
	}
}

func TestCanAccessConnectionReturnsAccessLevelNone(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Create user without any group membership
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")

	// Create restricted connection (assigned to another group)
	otherGroupID, _ := store.CreateGroup("other-group", "Other group")
	store.GrantConnectionPrivilege(otherGroupID, 1, AccessLevelReadWrite)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// User should be denied with AccessLevelNone
	canAccess, level := checker.CanAccessConnection(ctx, 1)
	if canAccess {
		t.Error("Expected access to be denied")
	}
	if level != AccessLevelNone {
		t.Errorf("Expected AccessLevelNone when denied, got %q", level)
	}
}

func TestResolveConnectionAccessReturnsAccessLevelNone(t *testing.T) {
	// Test that resolveConnectionAccess returns AccessLevelNone when no access
	privs := make(map[int]string)

	level, hasAccess := resolveConnectionAccess(privs, 1)
	if hasAccess {
		t.Error("Expected no access for empty privs map")
	}
	if level != AccessLevelNone {
		t.Errorf("Expected AccessLevelNone, got %q", level)
	}
}

// =============================================================================
// Coverage Gap Tests - CanAccessConnection
// =============================================================================

func TestCanAccessConnection_EmptyOwnerUsername(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Connection 10 is not shared and has empty owner username
	// This should deny access because we cannot determine ownership
	checker.SetConnectionSharingLookup(mockSharingLookup(map[int]struct {
		isShared      bool
		ownerUsername string
	}{
		10: {isShared: false, ownerUsername: ""},
	}))

	// User tries to access connection with empty owner
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, int64(1))
	ctx = context.WithValue(ctx, UsernameContextKey, "alice")

	canAccess, level := checker.CanAccessConnection(ctx, 10)
	if canAccess {
		t.Error("Expected access to be denied when owner is empty string")
	}
	if level != AccessLevelNone {
		t.Errorf("Expected AccessLevelNone, got %q", level)
	}
}

func TestCanAccessConnection_SharingLookupError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Set up a sharing lookup function that returns an error
	checker.SetConnectionSharingLookup(func(_ context.Context, _ int) (bool, string, error) {
		return false, "", fmt.Errorf("database connection failed")
	})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, int64(1))
	ctx = context.WithValue(ctx, UsernameContextKey, "alice")

	// Should deny access when sharing lookup returns error
	canAccess, level := checker.CanAccessConnection(ctx, 99)
	if canAccess {
		t.Error("Expected access to be denied when sharing lookup returns error")
	}
	if level != AccessLevelNone {
		t.Errorf("Expected AccessLevelNone, got %q", level)
	}
}

// =============================================================================
// Coverage Gap Tests - HasAdminPermission with Token Scoping
// =============================================================================

func TestHasAdminPermissionTokenScopeDenied(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Create user with both manage_users and manage_connections
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("admin-group", "Admin group")
	store.AddUserToGroup(groupID, userID)
	store.GrantAdminPermission(groupID, PermManageUsers)
	store.GrantAdminPermission(groupID, PermManageConnections)

	// Create token scoped to ONLY manage_connections (not manage_users)
	_, storedToken, _ := store.CreateToken("testuser", "Scoped admin token", nil)
	store.SetTokenAdminScope(storedToken.ID, []string{PermManageConnections})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	// User has manage_users via group but token scope excludes it
	// This should return false because token scope restricts access
	if checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("Expected manage_users to be denied due to token scope restriction")
	}

	// manage_connections should still work (in token scope)
	if !checker.HasAdminPermission(ctx, PermManageConnections) {
		t.Error("Expected manage_connections to be allowed (in token scope)")
	}
}

// =============================================================================
// Coverage Gap Tests - CanAccessMCPItem with Token Scoping
// =============================================================================

func TestCanAccessMCPItemTokenScopeDenied(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Create user with privileges
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant multiple MCP privileges
	store.RegisterMCPPrivilege("tool_x", MCPPrivilegeTypeTool, "Tool X", false)
	store.RegisterMCPPrivilege("tool_y", MCPPrivilegeTypeTool, "Tool Y", false)
	store.GrantMCPPrivilegeByName(groupID, "tool_x")
	store.GrantMCPPrivilegeByName(groupID, "tool_y")

	// Create token scoped to ONLY tool_x
	_, storedToken, _ := store.CreateToken("testuser", "Scoped MCP token", nil)
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"tool_x"})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	// tool_x should be accessible (in scope)
	if !checker.CanAccessMCPItem(ctx, "tool_x") {
		t.Error("Expected tool_x to be accessible (in token scope)")
	}

	// tool_y should be denied (user has it but token scope excludes it)
	if checker.CanAccessMCPItem(ctx, "tool_y") {
		t.Error("Expected tool_y to be denied due to token scope restriction")
	}
}

// =============================================================================
// Coverage Gap Tests - GetEffectivePrivileges MCP Scope with Invalid IDs
// =============================================================================

func TestGetEffectivePrivilegesMCPScopeWithNonexistentPrivilegeID(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Create user with MCP privileges
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Register and grant MCP privileges
	store.RegisterMCPPrivilege("tool_valid", MCPPrivilegeTypeTool, "Valid Tool", false)
	store.GrantMCPPrivilegeByName(groupID, "tool_valid")

	// Create token with a valid MCP scope entry
	_, storedToken, _ := store.CreateToken("testuser", "Scoped token", nil)
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"tool_valid"})

	// Now manually insert an invalid privilege ID into token_mcp_scope
	// This exercises the GetMCPPrivilegeByID error/nil path in GetEffectivePrivileges
	store.db.Exec(
		"INSERT INTO token_mcp_scope (token_id, privilege_identifier_id) VALUES (?, ?)",
		storedToken.ID, 99999, // Non-existent privilege ID
	)

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	privs := checker.GetEffectivePrivileges(ctx)

	// Should still return the valid tool_valid privilege
	if !privs.MCPPrivileges["tool_valid"] {
		t.Error("Expected tool_valid in effective privileges")
	}
	// The invalid ID 99999 should be silently skipped (GetMCPPrivilegeByID returns nil)
	// Total privileges should be 1 (only tool_valid)
	if len(privs.MCPPrivileges) != 1 {
		t.Errorf("Expected 1 MCP privilege, got %d: %+v",
			len(privs.MCPPrivileges), privs.MCPPrivileges)
	}
}

// =============================================================================
// Coverage Gap Tests - CanAccessConnection Token Scope Error Paths
// =============================================================================

func TestCanAccessConnectionTokenScopeNotInScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Create user with connection privileges
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)

	// Grant access to connections 1 and 2 via group
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelReadWrite)
	store.GrantConnectionPrivilege(groupID, 2, AccessLevelReadWrite)

	// Create token scoped to ONLY connection 1
	_, storedToken, _ := store.CreateToken("testuser", "Scoped token", nil)
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelReadWrite},
	})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	// Connection 1 should be accessible (in token scope)
	canAccess, level := checker.CanAccessConnection(ctx, 1)
	if !canAccess {
		t.Error("Expected connection 1 to be accessible (in token scope)")
	}
	if level != AccessLevelReadWrite {
		t.Errorf("Expected read_write for connection 1, got %q", level)
	}

	// Connection 2 should be DENIED (user has group access but token scope excludes it)
	canAccess, level = checker.CanAccessConnection(ctx, 2)
	if canAccess {
		t.Error("Expected connection 2 to be denied due to token scope restriction")
	}
	if level != AccessLevelNone {
		t.Errorf("Expected AccessLevelNone for denied connection, got %q", level)
	}
}

// =============================================================================
// Coverage Gap Tests - GetEffectivePrivileges Connection Wildcard Scope
// =============================================================================

func TestGetEffectivePrivilegesConnectionWildcardReadCeiling(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// User has read_write to multiple connections
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelReadWrite)
	store.GrantConnectionPrivilege(groupID, 2, AccessLevelReadWrite)

	// Token with wildcard connection scope at read level
	// This should cap all connections to read access
	_, storedToken, _ := store.CreateToken("testuser", "Wildcard read token", nil)
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelRead},
	})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	privs := checker.GetEffectivePrivileges(ctx)

	// Both connections should be capped to read
	if privs.ConnectionPrivileges[1] != AccessLevelRead {
		t.Errorf("Expected read for connection 1 (wildcard ceiling), got %q",
			privs.ConnectionPrivileges[1])
	}
	if privs.ConnectionPrivileges[2] != AccessLevelRead {
		t.Errorf("Expected read for connection 2 (wildcard ceiling), got %q",
			privs.ConnectionPrivileges[2])
	}
}

// =============================================================================
// Coverage Gap Tests - resolveConnectionAccess edge cases
// =============================================================================

func TestResolveConnectionAccessWildcardReadWriteElevates(t *testing.T) {
	// Test that when both specific and wildcard are present,
	// and wildcard is ReadWrite, the result is ReadWrite
	privs := map[int]string{
		1:               AccessLevelRead,      // Specific grant is read
		ConnectionIDAll: AccessLevelReadWrite, // Wildcard is read_write
	}

	level, hasAccess := resolveConnectionAccess(privs, 1)
	if !hasAccess {
		t.Error("Expected access to be granted")
	}
	// Wildcard read_write should elevate the result
	if level != AccessLevelReadWrite {
		t.Errorf("Expected read_write (wildcard elevates), got %q", level)
	}
}

func TestResolveConnectionAccessSpecificOnlyNoWildcard(t *testing.T) {
	// Test when only specific grant exists (no wildcard)
	privs := map[int]string{
		1: AccessLevelRead,
	}

	level, hasAccess := resolveConnectionAccess(privs, 1)
	if !hasAccess {
		t.Error("Expected access to be granted")
	}
	if level != AccessLevelRead {
		t.Errorf("Expected read (specific grant), got %q", level)
	}

	// Connection 2 should have no access
	level, hasAccess = resolveConnectionAccess(privs, 2)
	if hasAccess {
		t.Error("Expected no access for ungrant connection")
	}
}

func TestResolveConnectionAccessWildcardOnlyNoSpecific(t *testing.T) {
	// Test when only wildcard grant exists (no specific)
	privs := map[int]string{
		ConnectionIDAll: AccessLevelRead,
	}

	level, hasAccess := resolveConnectionAccess(privs, 999)
	if !hasAccess {
		t.Error("Expected access via wildcard")
	}
	if level != AccessLevelRead {
		t.Errorf("Expected read (wildcard grant), got %q", level)
	}
}

func TestResolveConnectionAccessBothPresentWildcardRead(t *testing.T) {
	// Test when both specific and wildcard are present,
	// and wildcard is Read (not ReadWrite)
	privs := map[int]string{
		1:               AccessLevelReadWrite, // Specific is read_write
		ConnectionIDAll: AccessLevelRead,      // Wildcard is read
	}

	level, hasAccess := resolveConnectionAccess(privs, 1)
	if !hasAccess {
		t.Error("Expected access to be granted")
	}
	// Should use specific level (wildcard is not ReadWrite)
	if level != AccessLevelReadWrite {
		t.Errorf("Expected read_write (specific grant), got %q", level)
	}
}

// =============================================================================
// Coverage Gap Tests - CanAccessConnection with restricted connection, no userID
// =============================================================================

func TestCanAccessConnectionRestrictedNoUserID(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Create a group and assign connection 1 to it (makes it restricted)
	groupID, _ := store.CreateGroup("some-group", "Group with connection access")
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelReadWrite)

	// Context without user ID but connection is restricted (assigned to group)
	// This exercises the userID == 0 check in the restricted path
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	// Deliberately NOT setting UserIDContextKey

	canAccess, level := checker.CanAccessConnection(ctx, 1)
	if canAccess {
		t.Error("Expected access to be denied when userID is 0 and connection is restricted")
	}
	if level != AccessLevelNone {
		t.Errorf("Expected AccessLevelNone, got %q", level)
	}
}

// =============================================================================
// Coverage Gap Tests - CanAccessMCPItem without userID for non-public tool
// =============================================================================

func TestCanAccessMCPItemNoUserIDForNonPublicTool(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Register a non-public tool
	store.RegisterMCPPrivilege("private_tool", MCPPrivilegeTypeTool, "Private Tool", false)

	// Grant it to a group (so it's restricted)
	groupID, _ := store.CreateGroup("tool-group", "Group with tool access")
	store.GrantMCPPrivilegeByName(groupID, "private_tool")

	// Context without user ID
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	// Deliberately NOT setting UserIDContextKey

	// The tool is registered and not public, so it should hit userID == 0 check
	if checker.CanAccessMCPItem(ctx, "private_tool") {
		t.Error("Expected denial when userID is 0 for non-public registered tool")
	}
}

// =============================================================================
// Coverage Gap Tests - HasAdminPermission token scope returns false
// =============================================================================

func TestHasAdminPermissionTokenScopeReturnsFalse(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Create user with admin permissions
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("admin-group", "Admin group")
	store.AddUserToGroup(groupID, userID)

	// Grant wildcard admin permission to user via group
	store.GrantAdminPermission(groupID, AdminPermissionWildcard)

	// Create token with scope to only manage_connections
	_, storedToken, _ := store.CreateToken("testuser", "Limited admin token", nil)
	store.SetTokenAdminScope(storedToken.ID, []string{PermManageConnections})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	// User has wildcard admin via group, but token scope restricts to manage_connections
	// manage_users should be denied
	if checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("Expected manage_users to be denied (not in token admin scope)")
	}

	// manage_connections should be allowed
	if !checker.HasAdminPermission(ctx, PermManageConnections) {
		t.Error("Expected manage_connections to be allowed (in token admin scope)")
	}
}

// =============================================================================
// Coverage Gap Tests - GetEffectivePrivileges admin scope filtering
// =============================================================================

func TestGetEffectivePrivilegesAdminScopeFiltering(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)

	// Create user with admin permissions
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("admin-group", "Admin group")
	store.AddUserToGroup(groupID, userID)

	// Grant multiple admin permissions
	store.GrantAdminPermission(groupID, PermManageUsers)
	store.GrantAdminPermission(groupID, PermManageConnections)
	store.GrantAdminPermission(groupID, PermManageGroups)

	// Create token with scope to only manage_users
	_, storedToken, _ := store.CreateToken("testuser", "Limited admin token", nil)
	store.SetTokenAdminScope(storedToken.ID, []string{PermManageUsers})

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)

	privs := checker.GetEffectivePrivileges(ctx)

	// Should only have manage_users (the only one in token scope)
	if len(privs.AdminPermissions) != 1 {
		t.Errorf("Expected 1 admin permission after scope filtering, got %d: %+v",
			len(privs.AdminPermissions), privs.AdminPermissions)
	}
	if !privs.AdminPermissions[PermManageUsers] {
		t.Error("Expected manage_users in scoped admin permissions")
	}
	if privs.AdminPermissions[PermManageConnections] {
		t.Error("Expected manage_connections to be filtered out by token scope")
	}
	if privs.AdminPermissions[PermManageGroups] {
		t.Error("Expected manage_groups to be filtered out by token scope")
	}
}

// =============================================================================
// Coverage Gap Tests - Database Error Paths (using closed DB)
// =============================================================================

func TestCanAccessMCPItemDatabaseError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)

	checker := NewRBACChecker(store)

	// Register a non-public privilege before closing
	store.RegisterMCPPrivilege("test_tool", MCPPrivilegeTypeTool, "Test Tool", false)

	// Create user and give them access
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)
	store.GrantMCPPrivilegeByName(groupID, "test_tool")

	// Close the database to trigger errors
	cleanup()

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// Should return false due to database error in IsPrivilegePublic
	if checker.CanAccessMCPItem(ctx, "test_tool") {
		t.Error("Expected false when database is closed")
	}
}

func TestHasAdminPermissionDatabaseError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)

	checker := NewRBACChecker(store)

	// Create user and give them admin permission
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("admin-group", "Admin")
	store.AddUserToGroup(groupID, userID)
	store.GrantAdminPermission(groupID, PermManageUsers)

	// Close the database to trigger errors
	cleanup()

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// Should return false due to database error in GetUserAdminPermissions
	if checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("Expected false when database is closed")
	}
}

func TestCanAccessConnectionDatabaseError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)

	checker := NewRBACChecker(store)

	// Create user and give them connection access
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "Test")
	store.AddUserToGroup(groupID, userID)
	store.GrantConnectionPrivilege(groupID, 1, AccessLevelReadWrite)

	// Close the database to trigger errors
	cleanup()

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)

	// Should return false due to database error in IsConnectionAssignedToAnyGroup
	canAccess, level := checker.CanAccessConnection(ctx, 1)
	if canAccess {
		t.Error("Expected false when database is closed")
	}
	if level != AccessLevelNone {
		t.Errorf("Expected AccessLevelNone, got %q", level)
	}
}

func TestHasAdminPermissionTokenScopeDatabaseError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)

	checker := NewRBACChecker(store)

	// Create user with admin permission and token
	store.CreateUser("testuser", "Password1", "Test user", "", "")
	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("admin-group", "Admin")
	store.AddUserToGroup(groupID, userID)
	store.GrantAdminPermission(groupID, PermManageUsers)

	// Create token with admin scope
	_, storedToken, _ := store.CreateToken("testuser", "Admin token", nil)
	store.SetTokenAdminScope(storedToken.ID, []string{PermManageUsers})

	// Store the token ID before closing
	tokenID := storedToken.ID

	// Close the database to trigger errors
	cleanup()

	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, TokenIDContextKey, tokenID)

	// Should return false due to database error in GetUserAdminPermissions
	// (the first DB call in the function after nil and superuser checks)
	if checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("Expected false when database is closed")
	}
}
