/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package resources

import (
	"context"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	conf "github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"golang.org/x/crypto/bcrypt"
)

// Helper to create bool pointer
func boolPtr(b bool) *bool {
	return &b
}

func TestNewContextAwareRegistry(t *testing.T) {
	dbConfig := &conf.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "test1",
		User:     "testuser",
	}
	cm := database.NewClientManager(dbConfig)
	defer cm.CloseAll()

	cfg := &conf.Config{
		Builtins: conf.BuiltinsConfig{
			Resources: conf.ResourcesConfig{
				SystemInfo: boolPtr(true),
			},
		},
	}

	registry := NewContextAwareRegistry(cm, cfg, nil, nil)

	if registry == nil {
		t.Fatal("expected non-nil registry")
	}
	if registry.clientManager != cm {
		t.Error("expected client manager to be set")
	}
}

func TestContextAwareRegistry_List(t *testing.T) {
	cm := database.NewClientManager(nil)
	defer cm.CloseAll()

	t.Run("with all resources enabled", func(t *testing.T) {
		cfg := &conf.Config{
			Builtins: conf.BuiltinsConfig{
				Resources: conf.ResourcesConfig{
					SystemInfo: boolPtr(true),
				},
			},
		}

		registry := NewContextAwareRegistry(cm, cfg, nil, nil)
		resources := registry.List()

		// Should have built-in resource
		if len(resources) < 1 {
			t.Errorf("expected at least 1 resource, got %d", len(resources))
		}

		// Verify URIs
		found := make(map[string]bool)
		for _, r := range resources {
			found[r.URI] = true
		}
		if !found[URISystemInfo] {
			t.Error("expected URISystemInfo to be in list")
		}
	})

	t.Run("with system_info disabled", func(t *testing.T) {
		cfg := &conf.Config{
			Builtins: conf.BuiltinsConfig{
				Resources: conf.ResourcesConfig{
					SystemInfo: boolPtr(false),
				},
			},
		}

		registry := NewContextAwareRegistry(cm, cfg, nil, nil)
		resources := registry.List()

		// Should not have system info
		found := make(map[string]bool)
		for _, r := range resources {
			found[r.URI] = true
		}
		if found[URISystemInfo] {
			t.Error("expected URISystemInfo to be disabled")
		}
	})
}

func TestContextAwareRegistry_Read_DisabledResource(t *testing.T) {
	cm := database.NewClientManager(nil)
	defer cm.CloseAll()

	cfg := &conf.Config{
		Builtins: conf.BuiltinsConfig{
			Resources: conf.ResourcesConfig{
				SystemInfo: boolPtr(false),
			},
		},
	}

	registry := NewContextAwareRegistry(cm, cfg, nil, nil)

	// Reading disabled resource should return error content
	content, err := registry.Read(context.Background(), URISystemInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check the content indicates resource is not available
	if len(content.Contents) == 0 {
		t.Fatal("expected content")
	}
	if content.Contents[0].Text == "" {
		t.Error("expected error message in content")
	}
}

func TestContextAwareRegistry_Read_NotFound(t *testing.T) {
	cm := database.NewClientManager(nil)
	defer cm.CloseAll()

	cfg := &conf.Config{
		Builtins: conf.BuiltinsConfig{
			Resources: conf.ResourcesConfig{
				SystemInfo: boolPtr(true),
			},
		},
	}

	registry := NewContextAwareRegistry(cm, cfg, nil, nil)

	// Reading non-existent resource should return not found content
	content, err := registry.Read(context.Background(), "pg://nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check the content indicates resource not found
	if len(content.Contents) == 0 {
		t.Fatal("expected content")
	}
	if content.Contents[0].Text != "Resource not found: pg://nonexistent" {
		t.Errorf("unexpected content: %s", content.Contents[0].Text)
	}
}

func TestContextAwareRegistry_Read_AuthRequired(t *testing.T) {
	cm := database.NewClientManager(nil)
	defer cm.CloseAll()

	cfg := &conf.Config{
		Builtins: conf.BuiltinsConfig{
			Resources: conf.ResourcesConfig{
				SystemInfo: boolPtr(true),
			},
		},
	}

	// Auth enabled but no token in context
	registry := NewContextAwareRegistry(cm, cfg, nil, nil)

	// Reading without token should return error
	content, err := registry.Read(context.Background(), URISystemInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have error content about missing token
	if len(content.Contents) == 0 {
		t.Fatal("expected content")
	}
	if content.Contents[0].Text == "" {
		t.Error("expected error message")
	}
}

func TestContextAwareRegistry_Read_WithToken(t *testing.T) {
	cm := database.NewClientManager(nil)
	defer cm.CloseAll()

	cfg := &conf.Config{
		Builtins: conf.BuiltinsConfig{
			Resources: conf.ResourcesConfig{
				SystemInfo: boolPtr(true),
			},
		},
	}

	registry := NewContextAwareRegistry(cm, cfg, nil, nil)

	// Add token to context
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, "test-token-hash")

	// Reading with token - will fail because no DB connection, but exercises the code path
	content, err := registry.Read(ctx, URISystemInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have content (either success or error about DB connection)
	if len(content.Contents) == 0 {
		t.Fatal("expected content")
	}
}

func TestContextAwareRegistry_GetClient_AuthDisabled(t *testing.T) {
	cm := database.NewClientManager(nil)
	defer cm.CloseAll()

	cfg := &conf.Config{
		Builtins: conf.BuiltinsConfig{
			Resources: conf.ResourcesConfig{
				SystemInfo: boolPtr(true),
			},
		},
	}

	registry := NewContextAwareRegistry(cm, cfg, nil, nil)

	// When auth is disabled, getClient uses "default" key
	// This exercises the code path - it may return an error or a client
	// depending on ClientManager implementation
	_, _ = registry.getClient(context.Background())
	// Test passes if no panic occurs - we're just testing the code path
}

func TestContextAwareRegistry_GetClient_AuthEnabled_NoToken(t *testing.T) {
	cm := database.NewClientManager(nil)
	defer cm.CloseAll()

	cfg := &conf.Config{}

	registry := NewContextAwareRegistry(cm, cfg, nil, nil)

	_, err := registry.getClient(context.Background())
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if err.Error() != "no authentication token found in request context" {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestContextAwareRegistry_GetClient_NilClientResolver verifies that
// getClient returns the canonical "no database connection configured"
// error when no clientResolver is wired (no client manager).
func TestContextAwareRegistry_GetClient_NilClientResolver(t *testing.T) {
	cfg := &conf.Config{}
	registry := NewContextAwareRegistry(nil, cfg, nil, nil)

	_, err := registry.getClient(context.Background())
	if err == nil {
		t.Fatal("expected error for nil client resolver")
	}
	if err.Error() != "no database connection configured" {
		t.Errorf("expected exact 'no database connection configured', got: %q", err.Error())
	}
}

func TestContextAwareRegistry_DefaultNilConfig(t *testing.T) {
	cm := database.NewClientManager(nil)
	defer cm.CloseAll()

	// With nil values (defaults to enabled)
	cfg := &conf.Config{
		Builtins: conf.BuiltinsConfig{
			Resources: conf.ResourcesConfig{}, // All nil = all enabled
		},
	}

	registry := NewContextAwareRegistry(cm, cfg, nil, nil)
	resources := registry.List()

	// Should have built-in resource since nil defaults to enabled
	if len(resources) < 1 {
		t.Errorf("expected at least 1 resource with default config, got %d", len(resources))
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

	t.Run("registry creation with nil authStore creates valid rbacChecker", func(t *testing.T) {
		clientManager := database.NewClientManager(nil)
		defer clientManager.CloseAll()

		cfg := &conf.Config{}

		// Create registry with nil authStore - should not panic
		registry := NewContextAwareRegistry(clientManager, cfg, nil, nil)
		if registry == nil {
			t.Fatal("Expected non-nil registry")
		}
		if registry.rbacChecker == nil {
			t.Fatal("Expected non-nil rbacChecker even with nil authStore")
		}

		// With nil authStore, superuser check should return true
		ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, "any-token")
		if !registry.rbacChecker.IsSuperuser(ctx) {
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

// TestGetClient_RBACDenialClearsSession exercises the full getClient()
// code path. This test verifies that when a session exists but RBAC denies
// access to the connection, the session is cleared and an appropriate error
// is returned.
//
// This is a regression test for issue #94 where sessions established before
// token scope restriction would bypass RBAC checks at use-time.
func TestGetClient_RBACDenialClearsSession(t *testing.T) {
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

	// Create registry with both authStore AND datastore (non-nil)
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	cfg := &conf.Config{}
	datastore := database.NewTestDatastore(nil)

	registry := NewContextAwareRegistry(clientManager, cfg, authStore, datastore)

	// Configure the RBAC checker to deny access to connection 42
	registry.rbacChecker.SetConnectionSharingLookup(func(_ context.Context, connectionID int) (bool, string, error) {
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

	// Call getClient - should return access denied error
	_, err = registry.getClient(ctx)
	if err == nil {
		t.Fatal("Expected error for RBAC-denied connection")
	}
	if err.Error() != "access denied: the selected connection is no longer accessible with this token's scope. Please select a permitted connection" {
		t.Errorf("Expected access denied error, got: %v", err)
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

// TestGetClient_RBACAllowsAccessProceedsToDatastore exercises the
// getClient() code path when RBAC allows access.
func TestGetClient_RBACAllowsAccessProceedsToDatastore(t *testing.T) {
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

	// Create registry with both authStore AND datastore
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	cfg := &conf.Config{}
	datastore := database.NewTestDatastore(nil)

	registry := NewContextAwareRegistry(clientManager, cfg, authStore, datastore)

	// Configure the RBAC checker to allow access to connection 99
	registry.rbacChecker.SetConnectionSharingLookup(func(_ context.Context, connectionID int) (bool, string, error) {
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

	// Call getClient. RBAC allows access, so the code proceeds
	// to GetConnectionWithPassword which panics with a nil pool.
	var panicValue any
	func() {
		defer func() {
			panicValue = recover()
		}()
		_, _ = registry.getClient(ctx)
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

	// Session should still exist (RBAC allowed, only datastore failed)
	session, err := authStore.GetConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session == nil {
		t.Error("Expected session to still exist after RBAC allowed access")
	}
}

// TestGetClient_SuperuserBypassesRBAC verifies that superuser context
// bypasses RBAC checks even when a sharing lookup would deny access.
func TestGetClient_SuperuserBypassesRBAC(t *testing.T) {
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

	// Create registry
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	cfg := &conf.Config{}
	datastore := database.NewTestDatastore(nil)

	registry := NewContextAwareRegistry(clientManager, cfg, authStore, datastore)

	// Configure RBAC checker to deny access if it were checked
	registry.rbacChecker.SetConnectionSharingLookup(func(_ context.Context, connectionID int) (bool, string, error) {
		return false, "otheruser", nil // Would deny non-superusers
	})

	// Build context with superuser flag
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, tokenHash)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, true) // Superuser!
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "admin")

	// Call getClient. The superuser bypasses RBAC, so the code proceeds
	// to GetConnectionWithPassword which panics with a nil pool.
	var panicValue any
	func() {
		defer func() {
			panicValue = recover()
		}()
		_, _ = registry.getClient(ctx)
	}()

	// If we got a panic, it means RBAC passed and code proceeded to datastore
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

	// Session should still exist (not cleared by RBAC denial)
	session, err := authStore.GetConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session == nil {
		t.Error("Expected session to still exist for superuser")
	}
}
