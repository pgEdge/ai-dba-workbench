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
)

// createTestAuthStoreForTokenScope creates a temporary auth store for testing
func createTestAuthStoreForTokenScope(t *testing.T) (*AuthStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "auth-token-scope-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	store, err := NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create auth store: %v", err)
	}

	// Create a default user for token tests
	if err := store.CreateUser("testuser", "Password1", "", "", ""); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create test user: %v", err)
	}

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

// =============================================================================
// Token Connection Scope Tests
// =============================================================================

func TestSetTokenConnectionScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	// Create a token
	_, storedToken, err := store.CreateToken("testuser", "Test token", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	// Set connection scope
	err = store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelReadWrite},
		{ConnectionID: 2, AccessLevel: AccessLevelRead},
		{ConnectionID: 3, AccessLevel: AccessLevelReadWrite},
	})
	if err != nil {
		t.Fatalf("Failed to set connection scope: %v", err)
	}

	// Verify scope
	connections, err := store.GetTokenConnectionScope(storedToken.ID)
	if err != nil {
		t.Fatalf("Failed to get connection scope: %v", err)
	}

	if len(connections) != 3 {
		t.Errorf("Expected 3 connections in scope, got %d", len(connections))
	}
}

func TestSetTokenConnectionScopeClear(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	// Create a token
	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)

	// Set connection scope
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelReadWrite},
		{ConnectionID: 2, AccessLevel: AccessLevelRead},
		{ConnectionID: 3, AccessLevel: AccessLevelReadWrite},
	})

	// Clear scope by setting empty list
	err := store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{})
	if err != nil {
		t.Fatalf("Failed to clear connection scope: %v", err)
	}

	// Verify scope is empty
	connections, _ := store.GetTokenConnectionScope(storedToken.ID)
	if len(connections) != 0 {
		t.Errorf("Expected 0 connections after clear, got %d", len(connections))
	}
}

func TestIsConnectionInTokenScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	// Create a token
	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)

	// No scope defined - should return true for any connection with empty access level
	inScope, accessLevel, err := store.IsConnectionInTokenScope(storedToken.ID, 99)
	if err != nil {
		t.Fatalf("Failed to check scope: %v", err)
	}
	if !inScope {
		t.Error("Expected connection to be in scope when no scope defined")
	}
	if accessLevel != "" {
		t.Errorf("Expected empty access level for unrestricted, got %q", accessLevel)
	}

	// Set specific scope with different access levels
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelReadWrite},
		{ConnectionID: 2, AccessLevel: AccessLevelRead},
		{ConnectionID: 3, AccessLevel: AccessLevelReadWrite},
	})

	// Connection in scope with read access
	inScope, accessLevel, _ = store.IsConnectionInTokenScope(storedToken.ID, 2)
	if !inScope {
		t.Error("Expected connection 2 to be in scope")
	}
	if accessLevel != AccessLevelRead {
		t.Errorf("Expected access level %q, got %q", AccessLevelRead, accessLevel)
	}

	// Connection in scope with read_write access
	inScope, accessLevel, _ = store.IsConnectionInTokenScope(storedToken.ID, 1)
	if !inScope {
		t.Error("Expected connection 1 to be in scope")
	}
	if accessLevel != AccessLevelReadWrite {
		t.Errorf("Expected access level %q, got %q", AccessLevelReadWrite, accessLevel)
	}

	// Connection not in scope
	inScope, _, _ = store.IsConnectionInTokenScope(storedToken.ID, 99)
	if inScope {
		t.Error("Expected connection 99 to NOT be in scope")
	}
}

// =============================================================================
// Token MCP Scope Tests
// =============================================================================

func TestSetTokenMCPScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	// Create token and privileges
	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)
	priv1ID, _ := store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)
	priv2ID, _ := store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B", false)

	// Set MCP scope
	err := store.SetTokenMCPScope(storedToken.ID, []int64{priv1ID, priv2ID})
	if err != nil {
		t.Fatalf("Failed to set MCP scope: %v", err)
	}

	// Verify scope
	identifiers, err := store.GetTokenMCPScope(storedToken.ID)
	if err != nil {
		t.Fatalf("Failed to get MCP scope: %v", err)
	}

	if len(identifiers) != 2 {
		t.Errorf("Expected 2 privileges in scope, got %d", len(identifiers))
	}
}

func TestSetTokenMCPScopeByNames(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	// Create token and privileges
	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B", false)

	// Set MCP scope by names
	err := store.SetTokenMCPScopeByNames(storedToken.ID, []string{"tool_a", "tool_b"})
	if err != nil {
		t.Fatalf("Failed to set MCP scope by names: %v", err)
	}

	// Verify scope
	identifiers, _ := store.GetTokenMCPScope(storedToken.ID)
	if len(identifiers) != 2 {
		t.Errorf("Expected 2 privileges in scope, got %d", len(identifiers))
	}
}

func TestIsMCPItemInTokenScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	// Create token and privileges
	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B", false)

	// No scope defined - should return true for any item
	inScope, err := store.IsMCPItemInTokenScope(storedToken.ID, "tool_a")
	if err != nil {
		t.Fatalf("Failed to check scope: %v", err)
	}
	if !inScope {
		t.Error("Expected item to be in scope when no scope defined")
	}

	// Set specific scope
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"tool_a"})

	// Item in scope
	inScope, _ = store.IsMCPItemInTokenScope(storedToken.ID, "tool_a")
	if !inScope {
		t.Error("Expected tool_a to be in scope")
	}

	// Item not in scope
	inScope, _ = store.IsMCPItemInTokenScope(storedToken.ID, "tool_b")
	if inScope {
		t.Error("Expected tool_b to NOT be in scope")
	}
}

// =============================================================================
// Token Scope Management Tests
// =============================================================================

func TestGetTokenScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	// Create token
	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)

	// No scope defined - should return nil
	scope, err := store.GetTokenScope(storedToken.ID)
	if err != nil {
		t.Fatalf("Failed to get token scope: %v", err)
	}
	if scope != nil {
		t.Error("Expected nil scope when none defined")
	}

	// Set both scopes
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelReadWrite},
		{ConnectionID: 2, AccessLevel: AccessLevelRead},
	})
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"tool_a"})

	// Get complete scope
	scope, err = store.GetTokenScope(storedToken.ID)
	if err != nil {
		t.Fatalf("Failed to get token scope: %v", err)
	}

	if scope == nil {
		t.Fatal("Expected scope to be defined")
	}

	if len(scope.Connections) != 2 {
		t.Errorf("Expected 2 connections, got %d", len(scope.Connections))
	}

	if len(scope.MCPPrivileges) != 1 {
		t.Errorf("Expected 1 MCP privilege, got %d", len(scope.MCPPrivileges))
	}
}

func TestClearTokenScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	// Create token with scope
	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelReadWrite},
		{ConnectionID: 2, AccessLevel: AccessLevelRead},
	})
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"tool_a"})

	// Clear all scope
	err := store.ClearTokenScope(storedToken.ID)
	if err != nil {
		t.Fatalf("Failed to clear token scope: %v", err)
	}

	// Verify cleared
	scope, _ := store.GetTokenScope(storedToken.ID)
	if scope != nil {
		t.Error("Expected nil scope after clear")
	}
}

// =============================================================================
// Wildcard MCP Scope Tests
// =============================================================================

func TestSetTokenMCPScopeByNamesWildcard(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	// Create token and privileges
	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B", false)

	// Set MCP scope with wildcard
	err := store.SetTokenMCPScopeByNames(storedToken.ID, []string{"*"})
	if err != nil {
		t.Fatalf("Failed to set wildcard MCP scope: %v", err)
	}

	// GetTokenMCPScope should return ["*"]
	identifiers, err := store.GetTokenMCPScope(storedToken.ID)
	if err != nil {
		t.Fatalf("Failed to get MCP scope: %v", err)
	}
	if len(identifiers) != 1 || identifiers[0] != "*" {
		t.Errorf("Expected [\"*\"], got %v", identifiers)
	}

	// IsMCPItemInTokenScope should return true for any item
	inScope, err := store.IsMCPItemInTokenScope(storedToken.ID, "tool_a")
	if err != nil {
		t.Fatalf("Failed to check scope: %v", err)
	}
	if !inScope {
		t.Error("Expected wildcard to match tool_a")
	}

	inScope, err = store.IsMCPItemInTokenScope(storedToken.ID, "tool_b")
	if err != nil {
		t.Fatalf("Failed to check scope: %v", err)
	}
	if !inScope {
		t.Error("Expected wildcard to match tool_b")
	}

	inScope, err = store.IsMCPItemInTokenScope(storedToken.ID, "nonexistent_tool")
	if err != nil {
		t.Fatalf("Failed to check scope: %v", err)
	}
	if !inScope {
		t.Error("Expected wildcard to match nonexistent_tool")
	}
}

func TestSetTokenMCPScopeByNamesWildcardSkipsRemainder(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	// Create token and privileges
	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)

	// Set MCP scope with wildcard before other identifiers
	err := store.SetTokenMCPScopeByNames(storedToken.ID, []string{"*", "tool_a"})
	if err != nil {
		t.Fatalf("Failed to set wildcard MCP scope: %v", err)
	}

	// Should only have the wildcard entry, not tool_a separately
	identifiers, _ := store.GetTokenMCPScope(storedToken.ID)
	if len(identifiers) != 1 || identifiers[0] != "*" {
		t.Errorf("Expected only wildcard, got %v", identifiers)
	}
}

// =============================================================================
// Wildcard Admin Scope Tests
// =============================================================================

func TestSetTokenAdminScopeWildcard(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)

	// Set admin scope with wildcard
	err := store.SetTokenAdminScope(storedToken.ID, []string{"*"})
	if err != nil {
		t.Fatalf("Failed to set wildcard admin scope: %v", err)
	}

	// GetTokenAdminScope should return ["*"]
	perms, err := store.GetTokenAdminScope(storedToken.ID)
	if err != nil {
		t.Fatalf("Failed to get admin scope: %v", err)
	}
	if len(perms) != 1 || perms[0] != "*" {
		t.Errorf("Expected [\"*\"], got %v", perms)
	}

	// IsAdminPermissionInTokenScope should return true for any permission
	inScope, err := store.IsAdminPermissionInTokenScope(storedToken.ID, PermManageUsers)
	if err != nil {
		t.Fatalf("Failed to check admin scope: %v", err)
	}
	if !inScope {
		t.Error("Expected wildcard to match manage_users")
	}

	inScope, err = store.IsAdminPermissionInTokenScope(storedToken.ID, PermManageConnections)
	if err != nil {
		t.Fatalf("Failed to check admin scope: %v", err)
	}
	if !inScope {
		t.Error("Expected wildcard to match manage_connections")
	}
}

func TestSetTokenAdminScopeWildcardSkipsRemainder(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)

	// Set admin scope with wildcard before other permissions
	err := store.SetTokenAdminScope(storedToken.ID, []string{"*", PermManageUsers})
	if err != nil {
		t.Fatalf("Failed to set wildcard admin scope: %v", err)
	}

	// Should only have the wildcard entry
	perms, _ := store.GetTokenAdminScope(storedToken.ID)
	if len(perms) != 1 || perms[0] != "*" {
		t.Errorf("Expected only wildcard, got %v", perms)
	}
}

// =============================================================================
// Wildcard Connection Scope Tests
// =============================================================================

func TestIsConnectionInTokenScopeWildcard(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)

	// Set connection scope with wildcard (connection_id = 0 = ConnectionIDAll)
	err := store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelReadWrite},
	})
	if err != nil {
		t.Fatalf("Failed to set wildcard connection scope: %v", err)
	}

	// Any connection should be in scope
	inScope, accessLevel, err := store.IsConnectionInTokenScope(storedToken.ID, 1)
	if err != nil {
		t.Fatalf("Failed to check scope: %v", err)
	}
	if !inScope {
		t.Error("Expected wildcard to match connection 1")
	}
	if accessLevel != AccessLevelReadWrite {
		t.Errorf("Expected read_write, got %q", accessLevel)
	}

	inScope, accessLevel, err = store.IsConnectionInTokenScope(storedToken.ID, 999)
	if err != nil {
		t.Fatalf("Failed to check scope: %v", err)
	}
	if !inScope {
		t.Error("Expected wildcard to match connection 999")
	}
	if accessLevel != AccessLevelReadWrite {
		t.Errorf("Expected read_write, got %q", accessLevel)
	}
}

func TestIsConnectionInTokenScopeWildcardReadOnly(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)

	// Set wildcard connection scope with read-only
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelRead},
	})

	// Any connection should be in scope with read access
	inScope, accessLevel, _ := store.IsConnectionInTokenScope(storedToken.ID, 42)
	if !inScope {
		t.Error("Expected wildcard to match connection 42")
	}
	if accessLevel != AccessLevelRead {
		t.Errorf("Expected read, got %q", accessLevel)
	}
}

func TestIsConnectionInTokenScopeSpecificOverridesWildcard(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)

	// Set both specific and wildcard connection scope
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelRead},
		{ConnectionID: 1, AccessLevel: AccessLevelReadWrite},
	})

	// Specific connection should use its own access level
	inScope, accessLevel, _ := store.IsConnectionInTokenScope(storedToken.ID, 1)
	if !inScope {
		t.Error("Expected connection 1 to be in scope")
	}
	if accessLevel != AccessLevelReadWrite {
		t.Errorf("Expected read_write for specific connection, got %q", accessLevel)
	}

	// Other connections should fall back to wildcard
	inScope, accessLevel, _ = store.IsConnectionInTokenScope(storedToken.ID, 99)
	if !inScope {
		t.Error("Expected wildcard to match connection 99")
	}
	if accessLevel != AccessLevelRead {
		t.Errorf("Expected read for wildcard connection, got %q", accessLevel)
	}
}

func TestHasTokenScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	// Create token
	_, storedToken, _ := store.CreateToken("testuser", "Test token", nil)

	// No scope
	hasScope, err := store.HasTokenScope(storedToken.ID)
	if err != nil {
		t.Fatalf("Failed to check scope: %v", err)
	}
	if hasScope {
		t.Error("Expected no scope initially")
	}

	// Add connection scope
	store.SetTokenConnectionScope(storedToken.ID, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelReadWrite},
	})

	hasScope, _ = store.HasTokenScope(storedToken.ID)
	if !hasScope {
		t.Error("Expected scope after adding connections")
	}

	// Clear and add MCP scope
	store.ClearTokenScope(storedToken.ID)
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A", false)
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"tool_a"})

	hasScope, _ = store.HasTokenScope(storedToken.ID)
	if !hasScope {
		t.Error("Expected scope after adding MCP privileges")
	}
}
