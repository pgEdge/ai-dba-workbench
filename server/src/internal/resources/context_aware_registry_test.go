/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package resources

import (
	"context"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	conf "github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
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

	registry := NewContextAwareRegistry(cm, false, cfg)

	if registry == nil {
		t.Fatal("expected non-nil registry")
	}
	if registry.clientManager != cm {
		t.Error("expected client manager to be set")
	}
	if registry.authEnabled {
		t.Error("expected authEnabled to be false")
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

		registry := NewContextAwareRegistry(cm, false, cfg)
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

		registry := NewContextAwareRegistry(cm, false, cfg)
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

	registry := NewContextAwareRegistry(cm, false, cfg)

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

	registry := NewContextAwareRegistry(cm, false, cfg)

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
	registry := NewContextAwareRegistry(cm, true, cfg)

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

	registry := NewContextAwareRegistry(cm, true, cfg)

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

	registry := NewContextAwareRegistry(cm, false, cfg)

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

	registry := NewContextAwareRegistry(cm, true, cfg)

	_, err := registry.getClient(context.Background())
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if err.Error() != "no authentication token found in request context" {
		t.Errorf("unexpected error: %v", err)
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

	registry := NewContextAwareRegistry(cm, false, cfg)
	resources := registry.List()

	// Should have built-in resource since nil defaults to enabled
	if len(resources) < 1 {
		t.Errorf("expected at least 1 resource with default config, got %d", len(resources))
	}
}
