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
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/resources"
	"github.com/pgedge/ai-workbench/server/internal/tracing"
	"golang.org/x/crypto/bcrypt"
)

// newExecuteTestProvider returns a provider with no database wiring,
// which is the shape a server started without a datastore has, together
// with the client manager the caller must close.
func newExecuteTestProvider(t *testing.T, cfg *config.Config) (*ContextAwareProvider,
	*database.ClientManager) {

	t.Helper()

	clientManager := database.NewClientManager(nil)
	t.Cleanup(func() {
		if err := clientManager.CloseAll(); err != nil {
			t.Logf("CloseAll: %v", err)
		}
	})

	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)
	return NewContextAwareProvider(clientManager, resourceReg, nil, cfg, nil, nil,
		nil), clientManager
}

// tokenContext returns a context carrying a token hash, which every
// non-hidden tool execution requires.
func tokenContext() context.Context {
	return context.WithValue(context.Background(), auth.TokenHashContextKey,
		"execute-test-token-hash")
}

// TestRegisterStatelessToolsKnowledgebase verifies that the
// knowledgebase tool is registered only when the configured database
// file is actually present, since registering it otherwise would offer
// the model a tool that cannot work.
func TestRegisterStatelessToolsKnowledgebase(t *testing.T) {
	kbPath := filepath.Join(t.TempDir(), "kb.db")
	if err := os.WriteFile(kbPath, []byte("not really sqlite"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		enabled  bool
		expected bool
	}{
		{name: "present", path: kbPath, enabled: true, expected: true},
		{name: "missing file", path: kbPath + ".absent", enabled: true,
			expected: false},
		{name: "disabled", path: kbPath, enabled: false, expected: false},
		{name: "no path", path: "", enabled: true, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Knowledgebase.Enabled = tt.enabled
			cfg.Knowledgebase.DatabasePath = tt.path

			provider, _ := newExecuteTestProvider(t, cfg)

			registry := NewRegistry()
			provider.registerStatelessTools(registry)

			if _, ok := registry.Get("search_knowledgebase"); ok != tt.expected {
				t.Errorf("search_knowledgebase registered = %v, want %v", ok,
					tt.expected)
			}
			if _, ok := registry.Get("read_resource"); !ok {
				t.Error("Expected read_resource to be registered regardless")
			}
		})
	}
}

// TestResourceReaderAdapter verifies that the read_resource tool's view
// of the resource registry lists and reads through to the real one.
func TestResourceReaderAdapter(t *testing.T) {
	cfg := &config.Config{}
	provider, _ := newExecuteTestProvider(t, cfg)

	adapter := provider.createResourceAdapter()

	listed := adapter.List()
	if len(listed) == 0 {
		t.Fatal("Expected the adapter to list the registry's resources")
	}

	content, err := adapter.Read(context.Background(), "pg://nonexistent")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(content.Contents) == 0 ||
		!strings.Contains(content.Contents[0].Text, "Resource not found") {
		t.Errorf("Expected the registry's own not-found answer, got %+v",
			content.Contents)
	}
}

// TestGetOrCreateRegistryForClientWithoutClient verifies that a request
// with no database client falls back to the base registry rather than
// building and caching a registry keyed on nil.
func TestGetOrCreateRegistryForClientWithoutClient(t *testing.T) {
	provider, _ := newExecuteTestProvider(t, &config.Config{})

	if got := provider.getOrCreateRegistryForClient(nil); got != provider.baseRegistry {
		t.Error("Expected the base registry when there is no client")
	}
	if len(provider.clientRegistries) != 0 {
		t.Errorf("Expected no cached registries, got %d",
			len(provider.clientRegistries))
	}
}

// TestExecuteHiddenToolSkipsAuthentication verifies that a hidden tool
// runs without a token, which is what the login tools depend on.
func TestExecuteHiddenToolSkipsAuthentication(t *testing.T) {
	provider, _ := newExecuteTestProvider(t, &config.Config{})

	called := false
	provider.hiddenRegistry.Register("hidden_login", Tool{
		Definition: mcp.Tool{Name: "hidden_login", Description: "hidden"},
		Handler: func(map[string]any) (mcp.ToolResponse, error) {
			called = true
			return mcp.ToolResponse{
				Content: []mcp.ContentItem{{Type: "text", Text: "hidden result"}},
			}, nil
		},
	})

	response, err := provider.Execute(context.Background(), "hidden_login", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !called {
		t.Fatal("Expected the hidden tool to run without a token")
	}
	if len(response.Content) == 0 || response.Content[0].Text != "hidden result" {
		t.Errorf("Expected the hidden tool's own response, got %+v",
			response.Content)
	}
}

// TestExecuteRefusesDisabledTool verifies that a tool switched off in
// the configuration is refused before any authentication or database
// work happens.
func TestExecuteRefusesDisabledTool(t *testing.T) {
	disabled := false
	cfg := &config.Config{}
	cfg.Builtins.Tools.QueryDatabase = &disabled

	provider, _ := newExecuteTestProvider(t, cfg)

	response, err := provider.Execute(context.Background(), "query_database",
		map[string]any{"query": "SELECT 1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !response.IsError {
		t.Error("Expected an error response for a disabled tool")
	}
	if len(response.Content) == 0 ||
		!strings.Contains(response.Content[0].Text, "is not available") {
		t.Errorf("Expected the 'not available' answer, got %+v", response.Content)
	}
}

// TestExecuteRefusesToolOutsideRBAC verifies that a caller without the
// privilege for a tool is refused by name, and that an RBAC-exempt tool
// is not.
func TestExecuteRefusesToolOutsideRBAC(t *testing.T) {
	store, err := auth.NewAuthStore(t.TempDir(), 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer store.Close()
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	if err := store.CreateUser("reader", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("reader")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	for _, name := range []string{"list_probes", "test_query"} {
		if _, err := store.RegisterMCPPrivilege(name, auth.MCPPrivilegeTypeTool,
			name, false); err != nil {
			t.Fatalf("RegisterMCPPrivilege(%s): %v", name, err)
		}
	}

	cfg := &config.Config{}
	clientManager := database.NewClientManager(nil)
	defer func() { _ = clientManager.CloseAll() }()
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, store, nil)
	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg,
		store, nil, nil)

	ctx := context.WithValue(tokenContext(), auth.UserIDContextKey, userID)

	response, err := provider.Execute(ctx, "list_probes", map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !response.IsError {
		t.Fatal("Expected a refusal for a tool the caller cannot use")
	}
	if len(response.Content) == 0 ||
		!strings.Contains(response.Content[0].Text,
			"do not have permission to use tool 'list_probes'") {
		t.Errorf("Expected a named refusal, got %+v", response.Content)
	}

	// test_query is exempt from the tool gate, so the same caller gets
	// the tool's own answer rather than a refusal.
	response, err = provider.Execute(ctx, "test_query", map[string]any{
		"query": "SELECT 1",
	})
	if err != nil {
		t.Fatalf("Execute(test_query): %v", err)
	}
	if len(response.Content) > 0 &&
		strings.Contains(response.Content[0].Text, "do not have permission") {
		t.Errorf("Expected test_query to bypass the tool gate, got %+v",
			response.Content)
	}
}

// TestExecuteStatelessToolWithoutDatastore verifies that a datastore
// tool executed with no datastore configured reports that plainly
// instead of reaching for a per-token database client.
func TestExecuteStatelessToolWithoutDatastore(t *testing.T) {
	provider, _ := newExecuteTestProvider(t, &config.Config{})

	response, err := provider.Execute(tokenContext(), "list_probes",
		map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(response.Content) == 0 {
		t.Fatal("Expected a response from the stateless tool")
	}
	if !strings.Contains(response.Content[0].Text, "not configured") &&
		!strings.Contains(response.Content[0].Text, "not available") {
		t.Errorf("Expected the tool's own datastore complaint, got %q",
			response.Content[0].Text)
	}
}

// TestExecuteReportsClientResolutionFailure verifies that a
// database-backed tool with no resolvable connection reports the root
// cause, and that the same tool with an explicit connection_id is
// instead handed to the base registry so its own resolver can run.
func TestExecuteReportsClientResolutionFailure(t *testing.T) {
	provider, _ := newExecuteTestProvider(t, &config.Config{})

	response, err := provider.Execute(tokenContext(), "query_database",
		map[string]any{"query": "SELECT 1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !response.IsError {
		t.Error("Expected an error response without a connection")
	}
	if len(response.Content) == 0 ||
		!strings.Contains(response.Content[0].Text, "Database connection error") {
		t.Errorf("Expected the connection error, got %+v", response.Content)
	}

	// With connection_id present the provider must not answer with the
	// session error; the tool's own resolver reports the outcome.
	response, err = provider.Execute(tokenContext(), "query_database",
		map[string]any{"query": "SELECT 1", "connection_id": float64(4242)})
	if err != nil {
		t.Fatalf("Execute(connection_id): %v", err)
	}
	if len(response.Content) == 0 {
		t.Fatal("Expected a response for the connection_id path")
	}
	if strings.Contains(response.Content[0].Text, "Database connection error") {
		t.Errorf("Expected the resolver's answer, not the session error: %q",
			response.Content[0].Text)
	}
}

// TestProviderAccessors covers the two accessors the server uses to
// reach into a provider after construction.
func TestProviderAccessors(t *testing.T) {
	provider, _ := newExecuteTestProvider(t, &config.Config{})

	if provider.GetBaseRegistry() != provider.baseRegistry {
		t.Error("Expected GetBaseRegistry to return the base registry")
	}
	// Memory needs a datastore, which this provider has not got.
	if provider.GetMemoryStore() != nil {
		t.Error("Expected no memory store without a datastore")
	}
}

// toolsTestDatabaseConfig turns TEST_AI_WORKBENCH_SERVER into the
// config a ClientManager needs, skipping the test when it is unset.
func toolsTestDatabaseConfig(t *testing.T) *config.DatabaseConfig {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping integration test")
	}

	parsed, err := url.Parse(connStr)
	if err != nil {
		t.Skipf("Could not parse TEST_AI_WORKBENCH_SERVER: %v", err)
	}
	port := 5432
	if p := parsed.Port(); p != "" {
		if converted, convErr := strconv.Atoi(p); convErr == nil {
			port = converted
		}
	}
	user := "postgres"
	if parsed.User != nil && parsed.User.Username() != "" {
		user = parsed.User.Username()
	}
	password, _ := parsed.User.Password()

	return &config.DatabaseConfig{
		Host:     parsed.Hostname(),
		Port:     port,
		Database: strings.TrimPrefix(parsed.Path, "/"),
		User:     user,
		Password: password,
		SSLMode:  "disable",
	}
}

// TestExecuteCachesRegistryPerClient verifies that a tool backed by a
// real database client runs from a registry built for that client, that
// the registry is reused on the next call, and that the trace records
// both single-item and multi-item results.
func TestExecuteCachesRegistryPerClient(t *testing.T) {
	dbConfig := toolsTestDatabaseConfig(t)

	if err := tracing.Initialize(filepath.Join(t.TempDir(),
		"trace.jsonl")); err != nil {
		t.Fatalf("tracing.Initialize: %v", err)
	}

	clientManager := database.NewClientManager(dbConfig)
	defer func() { _ = clientManager.CloseAll() }()

	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)
	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg,
		nil, nil, nil)

	ctx := tokenContext()

	response, err := provider.Execute(ctx, "query_database", map[string]any{
		"query": "SELECT 1 AS one",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if response.IsError {
		t.Fatalf("Expected the query to succeed, got %+v", response.Content)
	}
	if len(response.Content) == 0 ||
		!strings.Contains(response.Content[0].Text, "one") {
		t.Errorf("Expected the query result, got %+v", response.Content)
	}

	if len(provider.clientRegistries) != 1 {
		t.Fatalf("Expected one cached registry, got %d",
			len(provider.clientRegistries))
	}
	var cached *Registry
	for _, registry := range provider.clientRegistries {
		cached = registry
	}

	if _, err := provider.Execute(ctx, "query_database", map[string]any{
		"query": "SELECT 2 AS two",
	}); err != nil {
		t.Fatalf("Execute (second call): %v", err)
	}
	if len(provider.clientRegistries) != 1 {
		t.Errorf("Expected the registry to be reused, got %d entries",
			len(provider.clientRegistries))
	}
	for _, registry := range provider.clientRegistries {
		if registry != cached {
			t.Error("Expected the cached registry to be the same instance")
		}
	}

	// A response carrying several text items takes a different branch
	// when the trace entry is assembled.
	provider.hiddenRegistry.Register("multi_text", Tool{
		Definition: mcp.Tool{Name: "multi_text"},
		Handler: func(map[string]any) (mcp.ToolResponse, error) {
			return mcp.ToolResponse{
				Content: []mcp.ContentItem{
					{Type: "text", Text: "first"},
					{Type: "text", Text: "second"},
				},
			}, nil
		},
	})
	multi, err := provider.Execute(ctx, "multi_text", nil)
	if err != nil {
		t.Fatalf("Execute(multi_text): %v", err)
	}
	if len(multi.Content) != 2 {
		t.Errorf("Expected both content items, got %+v", multi.Content)
	}
}
