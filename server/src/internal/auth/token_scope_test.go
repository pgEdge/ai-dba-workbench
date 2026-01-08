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

	// Create a service token
	_, storedToken, err := store.CreateServiceToken("Test token", nil, "", false)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	// Set connection scope
	err = store.SetTokenConnectionScope(storedToken.ID, []int{1, 2, 3})
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

	// Create a service token
	_, storedToken, _ := store.CreateServiceToken("Test token", nil, "", false)

	// Set connection scope
	store.SetTokenConnectionScope(storedToken.ID, []int{1, 2, 3})

	// Clear scope by setting empty list
	err := store.SetTokenConnectionScope(storedToken.ID, []int{})
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

	// Create a service token
	_, storedToken, _ := store.CreateServiceToken("Test token", nil, "", false)

	// No scope defined - should return true for any connection
	inScope, err := store.IsConnectionInTokenScope(storedToken.ID, 99)
	if err != nil {
		t.Fatalf("Failed to check scope: %v", err)
	}
	if !inScope {
		t.Error("Expected connection to be in scope when no scope defined")
	}

	// Set specific scope
	store.SetTokenConnectionScope(storedToken.ID, []int{1, 2, 3})

	// Connection in scope
	inScope, _ = store.IsConnectionInTokenScope(storedToken.ID, 2)
	if !inScope {
		t.Error("Expected connection 2 to be in scope")
	}

	// Connection not in scope
	inScope, _ = store.IsConnectionInTokenScope(storedToken.ID, 99)
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
	_, storedToken, _ := store.CreateServiceToken("Test token", nil, "", false)
	priv1ID, _ := store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	priv2ID, _ := store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B")

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
	_, storedToken, _ := store.CreateServiceToken("Test token", nil, "", false)
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B")

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
	_, storedToken, _ := store.CreateServiceToken("Test token", nil, "", false)
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool, "Tool B")

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
	_, storedToken, _ := store.CreateServiceToken("Test token", nil, "", false)

	// No scope defined - should return nil
	scope, err := store.GetTokenScope(storedToken.ID)
	if err != nil {
		t.Fatalf("Failed to get token scope: %v", err)
	}
	if scope != nil {
		t.Error("Expected nil scope when none defined")
	}

	// Set both scopes
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	store.SetTokenConnectionScope(storedToken.ID, []int{1, 2})
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"tool_a"})

	// Get complete scope
	scope, err = store.GetTokenScope(storedToken.ID)
	if err != nil {
		t.Fatalf("Failed to get token scope: %v", err)
	}

	if scope == nil {
		t.Fatal("Expected scope to be defined")
	}

	if len(scope.ConnectionIDs) != 2 {
		t.Errorf("Expected 2 connections, got %d", len(scope.ConnectionIDs))
	}

	if len(scope.MCPPrivileges) != 1 {
		t.Errorf("Expected 1 MCP privilege, got %d", len(scope.MCPPrivileges))
	}
}

func TestClearTokenScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	// Create token with scope
	_, storedToken, _ := store.CreateServiceToken("Test token", nil, "", false)
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	store.SetTokenConnectionScope(storedToken.ID, []int{1, 2})
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

func TestHasTokenScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	// Create token
	_, storedToken, _ := store.CreateServiceToken("Test token", nil, "", false)

	// No scope
	hasScope, err := store.HasTokenScope(storedToken.ID)
	if err != nil {
		t.Fatalf("Failed to check scope: %v", err)
	}
	if hasScope {
		t.Error("Expected no scope initially")
	}

	// Add connection scope
	store.SetTokenConnectionScope(storedToken.ID, []int{1})

	hasScope, _ = store.HasTokenScope(storedToken.ID)
	if !hasScope {
		t.Error("Expected scope after adding connections")
	}

	// Clear and add MCP scope
	store.ClearTokenScope(storedToken.ID)
	store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool, "Tool A")
	store.SetTokenMCPScopeByNames(storedToken.ID, []string{"tool_a"})

	hasScope, _ = store.HasTokenScope(storedToken.ID)
	if !hasScope {
		t.Error("Expected scope after adding MCP privileges")
	}
}
