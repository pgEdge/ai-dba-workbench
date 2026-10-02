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
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/resources"
	"github.com/pgedge/ai-workbench/server/internal/tracing"
)

// Branch coverage for ContextAwareProvider.Execute, whose connection
// injection check changed in issue #561: hidden tools, tools switched off
// in the configuration, the connection_id fallback when no client can be
// resolved, and the result logging done when tracing is enabled.

// newExecuteProvider returns a provider with a real auth store, no client
// manager and no datastore, so getClient always fails.
func newExecuteProvider(t *testing.T, cfg *config.Config) *ContextAwareProvider {
	t.Helper()
	authStore, cleanup := newRBACTestStore(t)
	t.Cleanup(cleanup)
	resourceReg := resources.NewContextAwareRegistry(nil, cfg, authStore, nil)
	return NewContextAwareProvider(nil, resourceReg, nil, cfg, authStore, nil, nil)
}

// superuserTokenContext returns a superuser context carrying a token hash.
func superuserTokenContext() context.Context {
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey, "execute-token-hash")
	return context.WithValue(ctx, auth.IsSuperuserContextKey, true)
}

// hiddenTool returns a tool whose handler answers with the given content.
func hiddenTool(content ...mcp.ContentItem) Tool {
	return Tool{
		Definition: mcp.Tool{Name: "hidden"},
		Handler: func(map[string]any) (mcp.ToolResponse, error) {
			return mcp.ToolResponse{Content: content}, nil
		},
	}
}

// enableTracing turns on the process-wide tracer, writing to a temporary
// file. The tracer can be initialized only once per process, so the test
// is skipped when an earlier initialization left it disabled.
func enableTracing(t *testing.T) {
	t.Helper()
	if err := tracing.Initialize(filepath.Join(t.TempDir(), "trace.jsonl")); err != nil {
		t.Skipf("tracing unavailable: %v", err)
	}
	if !tracing.IsEnabled() {
		t.Skip("tracing was initialized disabled earlier in this process")
	}
}

func TestExecute_HiddenToolBypassesAuth(t *testing.T) {
	provider := newExecuteProvider(t, &config.Config{})
	provider.hiddenRegistry.Register("hidden_ping",
		hiddenTool(mcp.ContentItem{Type: "text", Text: "pong"}))

	// No token hash: a hidden tool must run without authentication.
	resp, err := provider.Execute(context.Background(), "hidden_ping", map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if resp.IsError || len(resp.Content) != 1 || resp.Content[0].Text != "pong" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestExecute_DisabledTool(t *testing.T) {
	disabled := false
	cfg := &config.Config{}
	cfg.Builtins.Tools.QueryDatabase = &disabled
	provider := newExecuteProvider(t, cfg)

	resp, err := provider.Execute(superuserTokenContext(), "query_database", map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !resp.IsError || len(resp.Content) == 0 ||
		!strings.Contains(resp.Content[0].Text, "Tool 'query_database' is not available") {
		t.Fatalf("expected tool-not-available error, got: %+v", resp)
	}
}

func TestExecute_ConnectionIDFallsThroughWithoutClient(t *testing.T) {
	provider := newExecuteProvider(t, &config.Config{})

	// Without connection_id the missing client is reported directly.
	resp, err := provider.Execute(superuserTokenContext(), "query_database",
		map[string]any{"query": "SELECT 1"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !resp.IsError || len(resp.Content) == 0 ||
		!strings.Contains(resp.Content[0].Text, "Database connection error") {
		t.Fatalf("expected database connection error, got: %+v", resp)
	}

	// With connection_id the call reaches the base registry, whose tool
	// resolves the connection itself; it must not report the client error.
	resp, err = provider.Execute(superuserTokenContext(), "query_database",
		map[string]any{"query": "SELECT 1", "connection_id": float64(1)})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(resp.Content) == 0 {
		t.Fatalf("expected a response, got: %+v", resp)
	}
	if strings.Contains(resp.Content[0].Text, "Database connection error") {
		t.Fatalf("connection_id call did not fall through: %s", resp.Content[0].Text)
	}
}

func TestExecute_TracingLogsResults(t *testing.T) {
	enableTracing(t)
	provider := newExecuteProvider(t, &config.Config{})

	tests := []struct {
		name    string
		content []mcp.ContentItem
	}{
		{name: "no content"},
		{name: "one text", content: []mcp.ContentItem{{Type: "text", Text: "one"}}},
		{name: "several texts", content: []mcp.ContentItem{
			{Type: "text", Text: "one"},
			{Type: "image"},
			{Type: "text", Text: "two"},
		}},
		{name: "no text", content: []mcp.ContentItem{{Type: "image"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			provider.hiddenRegistry.Register("hidden_trace", hiddenTool(tc.content...))
			resp, err := provider.Execute(context.Background(), "hidden_trace", map[string]any{})
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if len(resp.Content) != len(tc.content) {
				t.Fatalf("expected %d content items, got %d", len(tc.content), len(resp.Content))
			}
		})
	}
}
