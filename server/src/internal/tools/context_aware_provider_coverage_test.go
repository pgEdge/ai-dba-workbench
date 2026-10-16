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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/resources"
)

// Coverage for the parts of ContextAwareProvider not reached by
// provider_execute_coverage_test.go: knowledgebase registration, the
// accessors, RBAC-filtered listing and the per-client registry cache.

// toolNames returns the names of the tools in reg's listing.
func toolNames(reg *Registry) map[string]bool {
	names := map[string]bool{}
	for _, tool := range reg.List() {
		names[tool.Name] = true
	}
	return names
}

func TestContextAwareProvider_KnowledgebaseRegistration(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "kb.db")
	if err := os.WriteFile(existing, nil, 0o600); err != nil {
		t.Fatalf("failed to create knowledgebase file: %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "file present", path: existing, want: true},
		{name: "file missing", path: filepath.Join(t.TempDir(), "missing.db"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Knowledgebase.Enabled = true
			cfg.Knowledgebase.DatabasePath = tc.path
			provider := newExecuteProvider(t, cfg)
			if got := toolNames(provider.GetBaseRegistry())["search_knowledgebase"]; got != tc.want {
				t.Errorf("expected search_knowledgebase registered=%v, got %v", tc.want, got)
			}
		})
	}
}

func TestContextAwareProvider_Accessors(t *testing.T) {
	provider := newExecuteProvider(t, &config.Config{})

	if provider.GetBaseRegistry() != provider.baseRegistry {
		t.Error("GetBaseRegistry did not return the base registry")
	}
	// Memory needs a datastore, which this provider lacks.
	if provider.GetMemoryStore() != nil {
		t.Error("expected no memory store without a datastore")
	}

	adapter := provider.createResourceAdapter()
	if len(adapter.List()) == 0 {
		t.Error("expected the resource adapter to list the built-in resources")
	}
	// A caller with no privileges is refused by the resource registry.
	content, err := adapter.Read(restrictedUserContext(), resources.URISystemInfo)
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if len(content.Contents) == 0 || !strings.Contains(content.Contents[0].Text, "Access denied") {
		t.Errorf("expected an access-denied resource, got: %+v", content)
	}
}

func TestContextAwareProvider_ListForContext(t *testing.T) {
	provider := newExecuteProvider(t, &config.Config{})
	all := provider.List()

	if got := provider.ListForContext(superuserContext()); len(got) != len(all) {
		t.Errorf("expected a superuser to see all %d tools, got %d", len(all), len(got))
	}

	// A user with no privileges sees only the RBAC-exempt tools.
	filtered := provider.ListForContext(restrictedUserContext())
	if len(filtered) != len(rbacExemptTools) {
		t.Fatalf("expected %d exempt tools, got %+v", len(rbacExemptTools), filtered)
	}
	for _, tool := range filtered {
		if !rbacExemptTools[tool.Name] {
			t.Errorf("non-exempt tool %q visible to an unprivileged user", tool.Name)
		}
	}
}

func TestContextAwareProvider_GetOrCreateRegistryForClient(t *testing.T) {
	provider := newExecuteProvider(t, &config.Config{})

	if provider.getOrCreateRegistryForClient(nil) != provider.baseRegistry {
		t.Error("expected the base registry for a nil client")
	}

	client := database.NewClient(nil)
	defer client.Close()
	first := provider.getOrCreateRegistryForClient(client)
	if first == provider.baseRegistry {
		t.Fatal("expected a registry of the client's own")
	}
	if !toolNames(first)["query_database"] {
		t.Error("expected the client registry to hold the database tools")
	}
	if provider.getOrCreateRegistryForClient(client) != first {
		t.Error("expected the cached registry on the second call")
	}
}

// TestContextAwareProvider_ExecuteWithClient resolves a client for the
// token through the client manager, so a database tool runs from that
// client's cached registry.
func TestContextAwareProvider_ExecuteWithClient(t *testing.T) {
	authStore, cleanup := newRBACTestStore(t)
	defer cleanup()

	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()
	const tokenHash = "client-token-hash"
	client := database.NewClient(nil)
	if err := clientManager.SetClient(tokenHash, client); err != nil {
		t.Fatalf("SetClient: %v", err)
	}

	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, authStore, nil)
	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg, authStore, nil, nil)

	ctx := context.WithValue(superuserContext(), auth.TokenHashContextKey, tokenHash)
	for i := 0; i < 2; i++ {
		resp, err := provider.Execute(ctx, "get_schema_info", map[string]any{})
		if err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
		if len(resp.Content) == 0 {
			t.Fatalf("expected a response, got: %+v", resp)
		}
		if strings.Contains(resp.Content[0].Text, "Database connection error") {
			t.Fatalf("client was not resolved: %s", resp.Content[0].Text)
		}
	}
	provider.mu.RLock()
	_, cached := provider.clientRegistries[client]
	provider.mu.RUnlock()
	if !cached {
		t.Error("expected the client's registry to be cached")
	}
}
