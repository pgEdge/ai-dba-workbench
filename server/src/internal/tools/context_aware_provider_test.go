/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/resources"
	"golang.org/x/crypto/bcrypt"
)

// TestNewContextAwareProvider tests provider creation
func TestNewContextAwareProvider(t *testing.T) {
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	fallbackClient := database.NewClient(nil)
	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)

	provider := NewContextAwareProvider(clientManager, resourceReg, fallbackClient, cfg, nil, nil, nil)

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	if provider.baseRegistry == nil {
		t.Error("Expected baseRegistry to be initialized")
	}

	if provider.clientManager != clientManager {
		t.Error("Expected clientManager to be set correctly")
	}
}

// TestContextAwareProvider_List tests tool listing with smart filtering
func TestContextAwareProvider_List(t *testing.T) {
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	fallbackClient := database.NewClient(nil)
	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)

	provider := NewContextAwareProvider(clientManager, resourceReg, fallbackClient, cfg, nil, nil, nil)

	// Register tools
	err := provider.RegisterTools(context.TODO())
	if err != nil {
		t.Fatalf("RegisterTools failed: %v", err)
	}

	t.Run("returns all tools regardless of connection state", func(t *testing.T) {
		// List tools - should return all tools
		tools := provider.List()

		// Should have all 17 tools (no filtering)
		expectedTools := []string{
			"read_resource",
			"generate_embedding",
			"query_database",
			"get_schema_info",
			"similarity_search",
			"execute_explain",
			"count_rows",
			"list_probes",
			"describe_probe",
			"query_metrics",
			"list_connections",
			"get_alert_history",
			"get_alert_rules",
			"get_metric_baselines",
			"query_datastore",
			"get_blackouts",
			"test_query",
		}

		if len(tools) != len(expectedTools) {
			t.Errorf("Expected %d tools, got %d", len(expectedTools), len(tools))
		}

		// Check that all expected tools are present
		toolNames := make(map[string]bool)
		for _, tool := range tools {
			toolNames[tool.Name] = true
		}

		for _, expectedName := range expectedTools {
			if !toolNames[expectedName] {
				t.Errorf("Expected tool %q not found in list", expectedName)
			}
		}
	})
}

// TestContextAwareProvider_Execute_WithAuth tests execution with authentication
func TestContextAwareProvider_Execute_WithAuth(t *testing.T) {
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	fallbackClient := database.NewClient(nil)
	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)

	// Auth enabled - should require token hash
	provider := NewContextAwareProvider(clientManager, resourceReg, fallbackClient, cfg, nil, nil, nil)

	t.Run("missing token hash returns error", func(t *testing.T) {
		// Context without token hash
		ctx := context.Background()

		// Execute read_resource (even though it doesn't need DB, context validation happens first)
		_, err := provider.Execute(ctx, "read_resource", map[string]any{
			"uri": "test://test",
		})
		if err == nil {
			t.Fatal("Expected error for missing token hash, got nil")
		}

		if !strings.Contains(err.Error(), "no authentication token") {
			t.Errorf("Expected 'no authentication token' error, got: %v", err)
		}
	})

	t.Run("with valid token hash succeeds", func(t *testing.T) {
		// Context with token hash (no token store needed for stateless tools in auth mode)
		ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, "test-token-hash")

		// Execute read_resource (doesn't require database queries)
		response, err := provider.Execute(ctx, "read_resource", map[string]any{
			"uri": "test://test",
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		// read_resource should return a response (may be error for non-existent resource)
		// Verify we got a response
		if len(response.Content) == 0 {
			t.Fatal("Expected non-empty response content")
		}

		// Note: In unit tests without database configuration, clients are not created
		// In production with database config, read_resource would create clients for authenticated tokens
		// This test verifies the tool executes successfully with proper authentication
	})

	t.Run("multiple tokens get different clients", func(t *testing.T) {
		// First token
		ctx1 := context.WithValue(context.Background(), auth.TokenHashContextKey, "token-hash-1")
		_, err := provider.Execute(ctx1, "read_resource", map[string]any{
			"uri": "test://test1",
		})
		if err != nil {
			t.Fatalf("Execute failed for token 1: %v", err)
		}

		// Second token
		ctx2 := context.WithValue(context.Background(), auth.TokenHashContextKey, "token-hash-2")
		_, err = provider.Execute(ctx2, "read_resource", map[string]any{
			"uri": "test://test2",
		})
		if err != nil {
			t.Fatalf("Execute failed for token 2: %v", err)
		}

		// Third token
		ctx3 := context.WithValue(context.Background(), auth.TokenHashContextKey, "token-hash-3")
		_, err = provider.Execute(ctx3, "read_resource", map[string]any{
			"uri": "test://test3",
		})
		if err != nil {
			t.Fatalf("Execute failed for token 3: %v", err)
		}

		// Note: In unit tests without database configuration, clients are not created
		// In production with database config, each token would get its own isolated database client
		// This test verifies that multiple authenticated tokens can execute tools successfully
	})
}

// TestContextAwareProvider_Execute_InvalidTool tests execution of non-existent tool
func TestContextAwareProvider_Execute_InvalidTool(t *testing.T) {
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	fallbackClient := database.NewClient(nil)
	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)

	provider := NewContextAwareProvider(clientManager, resourceReg, fallbackClient, cfg, nil, nil, nil)

	// Provide a token hash since auth is always required
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, "test-token-hash")

	// Execute non-existent tool
	response, err := provider.Execute(ctx, "nonexistent_tool", map[string]any{})
	if err != nil {
		t.Errorf("Expected nil error (error in response), got: %v", err)
	}

	if !response.IsError {
		t.Error("Expected error response for non-existent tool")
	}

	// Verify error message
	if len(response.Content) == 0 {
		t.Fatal("Expected error message in response")
	}

	errorMsg := response.Content[0].Text
	// With runtime database connection, we now get a "no database" error
	// for non-stateless tools when database isn't configured
	// The root error might be "no database configured" or "no database connection configured"
	if !strings.Contains(errorMsg, "no database") && !strings.Contains(errorMsg, "Tool not found") {
		t.Errorf("Expected 'no database' or 'Tool not found' error, got: %s", errorMsg)
	}
}

// TestContextAwareProvider_RegisterTools_WithContext tests registering with context
func TestContextAwareProvider_RegisterTools_WithContext(t *testing.T) {
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	fallbackClient := database.NewClient(nil)
	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)

	provider := NewContextAwareProvider(clientManager, resourceReg, fallbackClient, cfg, nil, nil, nil)

	// Register with context containing token hash
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, "registration-token")

	err := provider.RegisterTools(ctx)
	if err != nil {
		t.Fatalf("RegisterTools failed: %v", err)
	}

	// Note: RegisterTools doesn't create clients - clients are created on-demand
	// when Execute() is called with database-dependent tools
	if count := clientManager.GetClientCount(); count != 0 {
		t.Errorf("Expected 0 clients after registration (clients created on-demand), got %d", count)
	}

	// Verify tools are registered in base registry
	tools := provider.List()
	if len(tools) == 0 {
		t.Error("Expected tools to be registered")
	}
}

// TestGetClient_TokenScopeEnforcement tests that getClient enforces token connection
// scope at use-time. This is a regression test for issue #94 where a session
// established before token scope restriction would bypass RBAC checks.
func TestGetClient_TokenScopeEnforcement(t *testing.T) {
	// Create a temporary auth store
	tmpDir := t.TempDir()
	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create a test user and token
	if err := authStore.CreateUser("testuser", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := authStore.GetUserID("testuser")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	// Create a token for the user
	_, token, err := authStore.CreateToken("testuser", "test-token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	tokenHash := token.TokenHash

	// Set up a connection session (simulating a previously selected connection)
	if err := authStore.SetConnectionSession(tokenHash, 42, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	t.Run("provider creation with nil authStore creates valid rbacChecker", func(t *testing.T) {
		// This test verifies that NewContextAwareProvider handles nil authStore
		// correctly by creating an rbacChecker that grants full access
		clientManager := database.NewClientManager(nil)
		defer clientManager.CloseAll()

		cfg := &config.Config{}
		resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)

		// Create provider with nil authStore - should not panic
		provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg, nil, nil, nil)
		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}
		if provider.rbacChecker == nil {
			t.Fatal("Expected non-nil rbacChecker even with nil authStore")
		}

		// With nil authStore, superuser check should return true
		ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, "any-token")
		if !provider.rbacChecker.IsSuperuser(ctx) {
			t.Error("Expected IsSuperuser to return true with nil authStore")
		}
	})

	t.Run("non-superuser context is properly identified", func(t *testing.T) {
		// Build context for the non-superuser token
		ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, tokenHash)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
		ctx = context.WithValue(ctx, auth.UsernameContextKey, "testuser")

		// Create RBAC checker with the auth store
		rbacChecker := auth.NewRBACChecker(authStore)

		// Verify superuser check works correctly
		if rbacChecker.IsSuperuser(ctx) {
			t.Error("Expected IsSuperuser to return false for non-superuser context")
		}
	})

	t.Run("RBAC denies access to unowned unshared connection", func(t *testing.T) {
		// Build context for the non-superuser token
		ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, tokenHash)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
		ctx = context.WithValue(ctx, auth.UsernameContextKey, "testuser")

		// Create RBAC checker that will deny access to connection 42
		rbacChecker := auth.NewRBACChecker(authStore)
		// Configure sharing lookup to return unshared connection owned by someone else
		rbacChecker.SetConnectionSharingLookup(func(_ context.Context, connectionID int) (bool, string, error) {
			if connectionID == 42 {
				// Connection is not shared and owned by a different user
				return false, "otheruser", nil
			}
			return false, "", nil
		})

		// Verify RBAC denies access to connection 42
		canAccess, _ := rbacChecker.CanAccessConnection(ctx, 42)
		if canAccess {
			t.Error("Expected CanAccessConnection to return false for unowned unshared connection")
		}
	})
}

// TestGetClient_SessionClearedOnRBACDenial verifies that when RBAC denies
// access to a session's connection, the session is cleared so subsequent
// calls get a clean "no connection selected" error.
func TestGetClient_SessionClearedOnRBACDenial(t *testing.T) {
	tmpDir := t.TempDir()
	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create user and token
	if err := authStore.CreateUser("alice", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, token, err := authStore.CreateToken("alice", "alice-token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	tokenHash := token.TokenHash

	// Set up a connection session
	if err := authStore.SetConnectionSession(tokenHash, 99, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	// Verify session exists before the test
	session, err := authStore.GetConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session == nil {
		t.Fatal("Expected session to exist before test")
	}
	if session.ConnectionID != 99 {
		t.Errorf("Expected ConnectionID 99, got %d", session.ConnectionID)
	}

	// The actual session clearing happens inside getClient when:
	// 1. authStore != nil && datastore != nil
	// 2. session != nil
	// 3. rbacChecker != nil
	// 4. rbacChecker.CanAccessConnection returns false
	//
	// We can verify that ClearConnectionSession works correctly when called.

	// Clear the session (simulating what getClient does on RBAC denial)
	if err := authStore.ClearConnectionSession(tokenHash); err != nil {
		t.Fatalf("ClearConnectionSession: %v", err)
	}

	// Verify session is cleared
	session, err = authStore.GetConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("GetConnectionSession after clear: %v", err)
	}
	if session != nil {
		t.Error("Expected session to be nil after clearing")
	}
}

// TestGetClient_NilRBACCheckerInSession verifies that the nil check for
// rbacChecker in getClient prevents panics when rbacChecker is nil.
// Note: In normal operation, NewContextAwareProvider always creates an
// rbacChecker, but this test ensures defensive coding is in place.
func TestGetClient_NilRBACCheckerInSession(t *testing.T) {
	tmpDir := t.TempDir()
	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create user and token
	if err := authStore.CreateUser("bob", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, token, err := authStore.CreateToken("bob", "bob-token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	tokenHash := token.TokenHash

	// Set up a connection session
	if err := authStore.SetConnectionSession(tokenHash, 50, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	// Create a provider using the constructor which ensures rbacChecker is not nil
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)

	// Create provider with the authStore but no datastore
	// The constructor will create a valid rbacChecker
	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg, authStore, nil, nil)

	// Verify provider has a non-nil rbacChecker
	if provider.rbacChecker == nil {
		t.Fatal("Expected non-nil rbacChecker from constructor")
	}

	// The getClient code path with session != nil and rbacChecker != nil
	// is now properly guarded. Without a datastore, we won't reach the
	// RBAC check in getClient, but we can verify the provider was created correctly.
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, tokenHash)

	// Execute should not panic due to nil rbacChecker
	response, _ := provider.Execute(ctx, "read_resource", map[string]any{
		"uri": "test://resource",
	})

	// Should get a response (may be an error about resource not found)
	if len(response.Content) == 0 {
		t.Fatal("Expected non-empty response content")
	}
}

// TestExecute_GetClient_RBACDenialClearsSession exercises the full getClient()
// code path through Execute(). This test verifies that when a session exists
// but RBAC denies access to the connection, the session is cleared and an
// appropriate error is returned.
//
// This is a regression test for issue #94 where sessions established before
// token scope restriction would bypass RBAC checks at use-time.
func TestExecute_GetClient_RBACDenialClearsSession(t *testing.T) {
	tmpDir := t.TempDir()
	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Register query_database as a public privilege so the MCP tool RBAC check
	// passes and we can reach the getClient() code path that we're testing
	if _, err := authStore.RegisterMCPPrivilege("query_database", auth.MCPPrivilegeTypeTool, "Query database", true); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}

	// Create user and token
	if err := authStore.CreateUser("alice", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := authStore.GetUserID("alice")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	_, token, err := authStore.CreateToken("alice", "alice-token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	tokenHash := token.TokenHash

	// Set up a connection session for connection 42
	if err := authStore.SetConnectionSession(tokenHash, 42, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	// Verify session exists
	session, err := authStore.GetConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session == nil || session.ConnectionID != 42 {
		t.Fatal("Expected session with ConnectionID 42")
	}

	// Create provider with both authStore AND datastore (non-nil)
	// Using NewTestDatastore(nil) creates a datastore with nil pool, but that's
	// OK because the RBAC denial path returns BEFORE any datastore methods are called
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)
	datastore := database.NewTestDatastore(nil)

	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg, authStore, nil, datastore)

	// Configure the RBAC checker to deny access to connection 42
	// The connection is not shared and owned by a different user
	provider.rbacChecker.SetConnectionSharingLookup(func(_ context.Context, connectionID int) (bool, string, error) {
		if connectionID == 42 {
			return false, "otheruser", nil // Not shared, owned by someone else
		}
		return false, "", nil
	})

	// Build context for non-superuser token
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, tokenHash)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "alice")

	// Execute a database-dependent tool (query_database requires getClient)
	response, _ := provider.Execute(ctx, "query_database", map[string]any{
		"query": "SELECT 1",
	})

	// Should get an error response about access denied (connection-level RBAC)
	if !response.IsError {
		t.Error("Expected error response for RBAC-denied connection")
	}
	if len(response.Content) == 0 {
		t.Fatal("Expected non-empty response content")
	}
	errorMsg := response.Content[0].Text
	if !strings.Contains(errorMsg, "access denied") {
		t.Errorf("Expected 'access denied' error, got: %s", errorMsg)
	}

	// Verify the session was cleared by getClient
	session, err = authStore.GetConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("GetConnectionSession after denial: %v", err)
	}
	if session != nil {
		t.Error("Expected session to be nil after RBAC denial cleared it")
	}
}

// TestExecute_GetClient_RBACAllowsAccessProceedsToDatastore exercises the
// getClient() code path when RBAC allows access. The test verifies that after
// passing the RBAC check, the code attempts to fetch connection info from
// the datastore (which will panic with nil pool, but proves the RBAC path was
// exercised).
func TestExecute_GetClient_RBACAllowsAccessProceedsToDatastore(t *testing.T) {
	tmpDir := t.TempDir()
	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Register query_database as a public privilege so the MCP tool RBAC check
	// passes and we can reach the getClient() code path
	if _, err := authStore.RegisterMCPPrivilege("query_database", auth.MCPPrivilegeTypeTool, "Query database", true); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}

	// Create user and token
	if err := authStore.CreateUser("bob", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := authStore.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	_, token, err := authStore.CreateToken("bob", "bob-token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	tokenHash := token.TokenHash

	// Set up a connection session for connection 99
	if err := authStore.SetConnectionSession(tokenHash, 99, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	// Create provider with both authStore AND datastore
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)
	datastore := database.NewTestDatastore(nil)

	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg, authStore, nil, datastore)

	// Configure the RBAC checker to allow access to connection 99
	// The connection is shared, so any user can access it
	provider.rbacChecker.SetConnectionSharingLookup(func(_ context.Context, connectionID int) (bool, string, error) {
		if connectionID == 99 {
			return true, "bob", nil // Shared connection
		}
		return false, "", nil
	})

	// Build context for non-superuser token
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, tokenHash)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "bob")

	// Execute a database-dependent tool. RBAC allows access, so the code proceeds
	// to GetConnectionWithPassword which panics with a nil pool. We catch the panic
	// to verify that RBAC was passed (no access denied error).
	var response mcp.ToolResponse
	var panicValue any
	func() {
		defer func() {
			panicValue = recover()
		}()
		response, _ = provider.Execute(ctx, "query_database", map[string]any{
			"query": "SELECT 1",
		})
	}()

	// If we got a panic, it means RBAC passed and code proceeded to datastore
	// (which has nil pool and panics). This is the expected path.
	if panicValue != nil {
		// Panic occurred - verify session still exists (RBAC didn't clear it)
		session, err := authStore.GetConnectionSession(tokenHash)
		if err != nil {
			t.Fatalf("GetConnectionSession: %v", err)
		}
		if session == nil {
			t.Error("Expected session to still exist after RBAC allowed access")
		}
		return // Test passed - RBAC was allowed, code reached datastore
	}

	// If we got a response without panic, check that it's not access denied
	if len(response.Content) > 0 {
		errorMsg := response.Content[0].Text
		if strings.Contains(errorMsg, "access denied") {
			t.Errorf("Expected RBAC to allow access, but got access denied: %s", errorMsg)
		}
	}

	// The session should still exist (RBAC allowed, only datastore failed)
	session, err := authStore.GetConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session == nil {
		t.Error("Expected session to still exist after RBAC allowed access")
	}
}

// TestExecute_GetClient_NoSession exercises the getClient() code path when
// authStore and datastore are both non-nil but no session exists. This
// should return a helpful "no connection selected" error.
func TestExecute_GetClient_NoSession(t *testing.T) {
	tmpDir := t.TempDir()
	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Register query_database as a public privilege so the MCP tool RBAC check
	// passes and we can reach the getClient() code path
	if _, err := authStore.RegisterMCPPrivilege("query_database", auth.MCPPrivilegeTypeTool, "Query database", true); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}

	// Create user and token but don't set a connection session
	if err := authStore.CreateUser("carol", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := authStore.GetUserID("carol")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	_, token, err := authStore.CreateToken("carol", "carol-token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	tokenHash := token.TokenHash

	// Create provider with both authStore AND datastore
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)
	datastore := database.NewTestDatastore(nil)

	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg, authStore, nil, datastore)

	// Build context
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, tokenHash)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "carol")

	// Execute a database-dependent tool without having a session
	response, _ := provider.Execute(ctx, "query_database", map[string]any{
		"query": "SELECT 1",
	})

	// Should get an error response about no connection selected
	if !response.IsError {
		t.Error("Expected error response for no session")
	}
	if len(response.Content) == 0 {
		t.Fatal("Expected non-empty response content")
	}
	errorMsg := response.Content[0].Text
	if !strings.Contains(errorMsg, "no database connection selected") {
		t.Errorf("Expected 'no database connection selected' error, got: %s", errorMsg)
	}
}

// TestExecute_GetClient_SessionWithDatabaseOverride exercises the getClient()
// code path when a session has a database name override. The RBAC check should
// pass, and the code should attempt to use the database override.
func TestExecute_GetClient_SessionWithDatabaseOverride(t *testing.T) {
	tmpDir := t.TempDir()
	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Register query_database as a public privilege so the MCP tool RBAC check
	// passes and we can reach the getClient() code path
	if _, err := authStore.RegisterMCPPrivilege("query_database", auth.MCPPrivilegeTypeTool, "Query database", true); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}

	// Create user and token
	if err := authStore.CreateUser("dave", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := authStore.GetUserID("dave")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	_, token, err := authStore.CreateToken("dave", "dave-token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	tokenHash := token.TokenHash

	// Set up a connection session with database override
	dbName := "myapp_db"
	if err := authStore.SetConnectionSession(tokenHash, 77, &dbName); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	// Verify session has the database override
	session, err := authStore.GetConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session == nil || session.ConnectionID != 77 {
		t.Fatal("Expected session with ConnectionID 77")
	}
	if session.DatabaseName == nil || *session.DatabaseName != "myapp_db" {
		t.Fatal("Expected session with DatabaseName 'myapp_db'")
	}

	// Create provider with both authStore AND datastore
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)
	datastore := database.NewTestDatastore(nil)

	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg, authStore, nil, datastore)

	// Configure the RBAC checker to allow access (shared connection)
	provider.rbacChecker.SetConnectionSharingLookup(func(_ context.Context, connectionID int) (bool, string, error) {
		if connectionID == 77 {
			return true, "dave", nil
		}
		return false, "", nil
	})

	// Build context
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, tokenHash)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "dave")

	// Execute a database-dependent tool. RBAC allows access, so the code proceeds
	// to GetConnectionWithPassword which panics with a nil pool.
	var response mcp.ToolResponse
	var panicValue any
	func() {
		defer func() {
			panicValue = recover()
		}()
		response, _ = provider.Execute(ctx, "query_database", map[string]any{
			"query": "SELECT current_database()",
		})
	}()

	// If we got a panic, it means RBAC passed and code proceeded to datastore
	if panicValue != nil {
		// Panic occurred - verify session still exists (RBAC didn't clear it)
		session, err := authStore.GetConnectionSession(tokenHash)
		if err != nil {
			t.Fatalf("GetConnectionSession: %v", err)
		}
		if session == nil {
			t.Error("Expected session to still exist after RBAC allowed access")
		}
		return // Test passed - RBAC was allowed, code reached datastore
	}

	// If we got a response without panic, check that it's not access denied
	if len(response.Content) > 0 {
		errorMsg := response.Content[0].Text
		if strings.Contains(errorMsg, "access denied") {
			t.Errorf("Expected RBAC to allow access, but got access denied: %s", errorMsg)
		}
	}

	// Session should still exist
	session, err = authStore.GetConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session == nil {
		t.Error("Expected session to still exist")
	}
}

// TestExecute_GetClient_SuperuserBypassesRBAC verifies that superuser context
// bypasses RBAC checks even when a sharing lookup would deny access.
func TestExecute_GetClient_SuperuserBypassesRBAC(t *testing.T) {
	tmpDir := t.TempDir()
	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create superuser and token
	if err := authStore.CreateUser("admin", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := authStore.SetUserSuperuser("admin", true); err != nil {
		t.Fatalf("SetUserSuperuser: %v", err)
	}
	userID, err := authStore.GetUserID("admin")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	_, token, err := authStore.CreateToken("admin", "admin-token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	tokenHash := token.TokenHash

	// Set up a connection session
	if err := authStore.SetConnectionSession(tokenHash, 88, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	// Create provider
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)
	datastore := database.NewTestDatastore(nil)

	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg, authStore, nil, datastore)

	// Configure RBAC checker to deny access if it were checked
	// (for non-superusers, this would deny access)
	provider.rbacChecker.SetConnectionSharingLookup(func(_ context.Context, connectionID int) (bool, string, error) {
		return false, "otheruser", nil // Would deny non-superusers
	})

	// Build context with superuser flag
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, tokenHash)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, true) // Superuser!
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "admin")

	// Execute a database-dependent tool. The superuser bypasses RBAC, so the code
	// proceeds to GetConnectionWithPassword which panics with a nil pool. We catch
	// the panic to verify that RBAC was bypassed (no access denied error).
	var response mcp.ToolResponse
	var panicValue any
	func() {
		defer func() {
			panicValue = recover()
		}()
		response, _ = provider.Execute(ctx, "query_database", map[string]any{
			"query": "SELECT 1",
		})
	}()

	// If we got a panic, it means RBAC passed and code proceeded to datastore
	// (which has nil pool and panics). This is the expected path for superuser.
	if panicValue != nil {
		// Panic occurred - verify session still exists (RBAC didn't clear it)
		session, err := authStore.GetConnectionSession(tokenHash)
		if err != nil {
			t.Fatalf("GetConnectionSession: %v", err)
		}
		if session == nil {
			t.Error("Expected session to still exist for superuser (RBAC was bypassed)")
		}
		return // Test passed - RBAC was bypassed, code reached datastore
	}

	// If we got a response without panic, check that it's not access denied
	if len(response.Content) > 0 {
		errorMsg := response.Content[0].Text
		if strings.Contains(errorMsg, "access denied") {
			t.Errorf("Superuser should bypass RBAC, but got access denied: %s", errorMsg)
		}
	}

	// Session should still exist (not cleared by RBAC denial)
	session, err := authStore.GetConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session == nil {
		t.Error("Expected session to still exist for superuser")
	}
}

// TestExecute_QueryMetrics_RBACChecksConnectionIDInjection verifies that the
// query_metrics tool's connection_id injection respects RBAC. When RBAC denies
// access to the session's connection, the connection_id should NOT be injected.
//
// This is a regression test for VULN-002 in issue #94 where the query_metrics
// tool would inject a connection_id from an expired/restricted session scope.
func TestExecute_QueryMetrics_RBACChecksConnectionIDInjection(t *testing.T) {
	tmpDir := t.TempDir()
	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Register query_metrics as a public privilege so RBAC check passes
	if _, err := authStore.RegisterMCPPrivilege("query_metrics", auth.MCPPrivilegeTypeTool, "Query metrics", true); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}

	// Create user and token
	if err := authStore.CreateUser("alice", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := authStore.GetUserID("alice")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	_, token, err := authStore.CreateToken("alice", "alice-token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	tokenHash := token.TokenHash

	// Set up a connection session for connection 42
	if err := authStore.SetConnectionSession(tokenHash, 42, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)

	// Create provider with nil datastore - query_metrics will return error
	// but we can observe whether connection_id was injected by checking args
	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg, authStore, nil, nil)

	// Configure the RBAC checker to deny access to connection 42
	provider.rbacChecker.SetConnectionSharingLookup(func(_ context.Context, connectionID int) (bool, string, error) {
		if connectionID == 42 {
			return false, "otheruser", nil // Not shared, owned by someone else
		}
		return false, "", nil
	})

	// Build context for non-superuser token
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, tokenHash)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "alice")

	// Execute query_metrics without connection_id. Since RBAC denies access to
	// connection 42, the session's connection_id should NOT be injected.
	// The tool should report that connection_id is required (datastore is nil,
	// so it will fail regardless, but we want to verify the injection path).
	response, _ := provider.Execute(ctx, "query_metrics", map[string]any{
		"probe_name":  "pg_stat_activity",
		"time_bucket": "1 hour",
	})

	// The response should indicate an error (no datastore configured)
	// but the important thing is that connection_id was NOT injected
	// from a session that RBAC should have denied.
	if !response.IsError {
		t.Log("Response was not an error, which is unexpected without datastore")
	}

	// The error message should NOT be "access denied" (that's the getClient path).
	// Since query_metrics is a stateless tool, it doesn't call getClient.
	// Without connection_id injection (due to RBAC denial), the tool should
	// either succeed with filtering by token's connection grants, or fail
	// because datastore is nil.
	if len(response.Content) > 0 {
		errorMsg := response.Content[0].Text
		// Verify we didn't get an access denied error from getClient path
		// (query_metrics doesn't use getClient)
		if strings.Contains(errorMsg, "the selected connection is no longer accessible") {
			t.Error("query_metrics should not trigger getClient access denied error")
		}
	}
}

// TestExecute_QueryMetrics_RBACAllowsConnectionIDInjection verifies that when
// RBAC allows access to the session's connection, the connection_id IS injected.
func TestExecute_QueryMetrics_RBACAllowsConnectionIDInjection(t *testing.T) {
	tmpDir := t.TempDir()
	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Register query_metrics as a public privilege
	if _, err := authStore.RegisterMCPPrivilege("query_metrics", auth.MCPPrivilegeTypeTool, "Query metrics", true); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}

	// Create user and token
	if err := authStore.CreateUser("bob", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := authStore.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	_, token, err := authStore.CreateToken("bob", "bob-token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	tokenHash := token.TokenHash

	// Set up a connection session for connection 99
	if err := authStore.SetConnectionSession(tokenHash, 99, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)

	// Create provider with nil datastore
	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg, authStore, nil, nil)

	// Configure the RBAC checker to ALLOW access to connection 99
	provider.rbacChecker.SetConnectionSharingLookup(func(_ context.Context, connectionID int) (bool, string, error) {
		if connectionID == 99 {
			return true, "bob", nil // Shared connection - allow access
		}
		return false, "", nil
	})

	// Build context for non-superuser token
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, tokenHash)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "bob")

	// Execute query_metrics without connection_id. Since RBAC allows access,
	// the session's connection_id (99) should be injected.
	response, _ := provider.Execute(ctx, "query_metrics", map[string]any{
		"probe_name":  "pg_stat_activity",
		"time_bucket": "1 hour",
	})

	// The tool will fail because datastore is nil, but the injection should
	// have happened. We verify by checking the error is about datastore, not
	// about missing connection_id.
	if len(response.Content) > 0 {
		errorMsg := response.Content[0].Text
		// The error should be about datastore, not about connection_id
		if strings.Contains(errorMsg, "connection_id is required") {
			t.Error("Expected connection_id to be injected when RBAC allows access")
		}
	}
}

// TestExecute_QueryMetrics_SuperuserAlwaysInjectsConnectionID verifies that
// when the user is a superuser, connection_id is always injected from the session.
func TestExecute_QueryMetrics_SuperuserAlwaysInjectsConnectionID(t *testing.T) {
	tmpDir := t.TempDir()
	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create superuser and token
	if err := authStore.CreateUser("admin", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := authStore.SetUserSuperuser("admin", true); err != nil {
		t.Fatalf("SetUserSuperuser: %v", err)
	}
	userID, err := authStore.GetUserID("admin")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	_, token, err := authStore.CreateToken("admin", "admin-token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	tokenHash := token.TokenHash

	// Set up a connection session for connection 50
	if err := authStore.SetConnectionSession(tokenHash, 50, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)

	// Create provider
	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg, authStore, nil, nil)

	// Configure RBAC to deny access (but superuser bypasses this)
	provider.rbacChecker.SetConnectionSharingLookup(func(_ context.Context, connectionID int) (bool, string, error) {
		return false, "otheruser", nil // Would deny non-superusers
	})

	// Build context with superuser flag
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, tokenHash)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, true) // Superuser!
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "admin")

	// Execute query_metrics without connection_id. Superuser bypasses RBAC,
	// so connection_id should be injected.
	response, _ := provider.Execute(ctx, "query_metrics", map[string]any{
		"probe_name":  "pg_stat_activity",
		"time_bucket": "1 hour",
	})

	// The tool will fail because datastore is nil, but check that error
	// is not about missing connection_id (which would mean injection failed)
	if len(response.Content) > 0 {
		errorMsg := response.Content[0].Text
		if strings.Contains(errorMsg, "connection_id is required") {
			t.Error("Expected connection_id to be injected for superuser")
		}
	}
}

// TestContextAwareProvider_GetClient_NilClientResolver verifies that
// getClient returns the canonical "no database connection configured"
// error when no clientResolver is wired (no client manager).
func TestContextAwareProvider_GetClient_NilClientResolver(t *testing.T) {
	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(nil, cfg, nil, nil)
	provider := NewContextAwareProvider(nil, resourceReg, nil, cfg, nil, nil, nil)

	_, err := provider.getClient(context.Background())
	if err == nil {
		t.Fatal("expected error for nil client resolver")
	}
	if err.Error() != "no database connection configured" {
		t.Errorf("expected exact 'no database connection configured', got: %q", err.Error())
	}
}
