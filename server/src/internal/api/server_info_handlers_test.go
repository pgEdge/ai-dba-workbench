/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pgllm "github.com/pgEdge/pgedge-go-llm-lib/llm"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/llmproxy"
)

// thinkingBlock is the content-block type a reasoning model emits for its
// chain of thought. The library has no named constant for it, so the tests
// use the wire value directly to reproduce the issue #399 response shape.
const thinkingBlock pgllm.ContentBlockType = "thinking"

func TestPgEncodingName(t *testing.T) {
	tests := []struct {
		encoding int
		expected string
	}{
		{0, "SQL_ASCII"},
		{6, "UTF8"},
		{8, "LATIN1"},
		{39, "KOI8R"},
		{999, "encoding_999"},
	}

	for _, tt := range tests {
		result := pgEncodingName(tt.encoding)
		if result != tt.expected {
			t.Errorf("pgEncodingName(%d) = %q, want %q",
				tt.encoding, result, tt.expected)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}

	for _, tt := range tests {
		result := formatBytes(tt.bytes)
		if result != tt.expected {
			t.Errorf("formatBytes(%d) = %q, want %q",
				tt.bytes, result, tt.expected)
		}
	}
}

func TestBuildDatabaseAnalysisPrompt(t *testing.T) {
	size := int64(1073741824)
	databases := []DatabaseInfo{
		{
			Name:       "myapp",
			SizeBytes:  &size,
			Extensions: []string{"pg_stat_statements", "pgcrypto"},
		},
		{
			Name:       "analytics",
			SizeBytes:  nil,
			Extensions: []string{},
		},
	}

	prompt := buildDatabaseAnalysisPrompt(databases)

	if prompt == "" {
		t.Error("buildDatabaseAnalysisPrompt returned empty string")
	}

	// Check that database names appear in the prompt
	if !containsString(prompt, "myapp") {
		t.Error("prompt does not contain database name 'myapp'")
	}
	if !containsString(prompt, "analytics") {
		t.Error("prompt does not contain database name 'analytics'")
	}
	// Check that extensions appear
	if !containsString(prompt, "pg_stat_statements") {
		t.Error("prompt does not contain extension 'pg_stat_statements'")
	}
	// Check that size is formatted
	if !containsString(prompt, "1.0 GB") {
		t.Error("prompt does not contain formatted size '1.0 GB'")
	}
	// Check that unknown size is shown for nil
	if !containsString(prompt, "unknown size") {
		t.Error("prompt does not contain 'unknown size' for nil size")
	}
}

func TestParseDatabaseAnalysisResponse(t *testing.T) {
	databases := []DatabaseInfo{
		{Name: "myapp"},
		{Name: "analytics"},
	}

	resp := &pgllm.ChatResponse{
		Content: []pgllm.ContentBlock{
			{
				Type: pgllm.BlockText,
				Text: "myapp: A web application database.\nanalytics: A data warehouse for reporting.\n",
			},
		},
	}

	result, err := parseDatabaseAnalysisResponse(resp, databases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 entries, got %d", len(result))
	}
	if result["myapp"] != "A web application database." {
		t.Errorf("unexpected myapp description: %q", result["myapp"])
	}
	if result["analytics"] != "A data warehouse for reporting." {
		t.Errorf("unexpected analytics description: %q", result["analytics"])
	}
}

func TestParseDatabaseAnalysisResponseHandlesMarkdownFormatting(t *testing.T) {
	databases := []DatabaseInfo{
		{Name: "myapp"},
		{Name: "analytics"},
		{Name: "warehouse"},
		{Name: "logs"},
		{Name: "metrics"},
	}

	resp := &pgllm.ChatResponse{
		Content: []pgllm.ContentBlock{
			{
				Type: pgllm.BlockText,
				Text: "- myapp: A web application database.\n" +
					"* analytics: A data warehouse for reporting.\n" +
					"**warehouse**: A storage system.\n" +
					"`logs`: A logging database.\n" +
					"1. metrics: A metrics store.\n",
			},
		},
	}

	result, err := parseDatabaseAnalysisResponse(resp, databases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 5 {
		t.Errorf("expected 5 entries, got %d", len(result))
	}
	if result["myapp"] != "A web application database." {
		t.Errorf("unexpected myapp description: %q", result["myapp"])
	}
	if result["analytics"] != "A data warehouse for reporting." {
		t.Errorf("unexpected analytics description: %q", result["analytics"])
	}
	if result["warehouse"] != "A storage system." {
		t.Errorf("unexpected warehouse description: %q", result["warehouse"])
	}
	if result["logs"] != "A logging database." {
		t.Errorf("unexpected logs description: %q", result["logs"])
	}
	if result["metrics"] != "A metrics store." {
		t.Errorf("unexpected metrics description: %q", result["metrics"])
	}
}

func TestParseDatabaseAnalysisResponseIgnoresUnknownDatabases(t *testing.T) {
	databases := []DatabaseInfo{
		{Name: "myapp"},
	}

	resp := &pgllm.ChatResponse{
		Content: []pgllm.ContentBlock{
			{
				Type: pgllm.BlockText,
				Text: "myapp: A web app.\nunknown_db: Should be ignored.\n",
			},
		},
	}

	result, err := parseDatabaseAnalysisResponse(resp, databases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 entry, got %d", len(result))
	}
	if _, ok := result["unknown_db"]; ok {
		t.Error("unknown_db should not be in result")
	}
}

func TestParseDatabaseAnalysisResponseConcatenatesTextBlocksOnly(t *testing.T) {
	databases := []DatabaseInfo{
		{Name: "testdb"},
	}

	// Non-text blocks must be ignored; only BlockText contributes to the
	// parsed text. Two text blocks are concatenated in order.
	resp := &pgllm.ChatResponse{
		Content: []pgllm.ContentBlock{
			{Type: pgllm.BlockToolUse, Text: "ignored tool block"},
			{Type: pgllm.BlockText, Text: "testdb: A test "},
			{Type: pgllm.BlockText, Text: "database.\n"},
		},
	}

	result, err := parseDatabaseAnalysisResponse(resp, databases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["testdb"] != "A test database." {
		t.Errorf("unexpected testdb description: %q", result["testdb"])
	}
}

// TestParseDatabaseAnalysisResponseNoTextContent covers the responses that
// carry no usable text: a nil response, a response whose only block is a
// reasoning/thinking block (the issue #399 shape produced when the model
// spends its whole output budget thinking), and whitespace-only text. Each
// must report llmproxy.ErrNoTextContent rather than an empty analysis that
// would be cached and rendered as a blank panel.
func TestParseDatabaseAnalysisResponseNoTextContent(t *testing.T) {
	databases := []DatabaseInfo{{Name: "testdb"}}

	tests := []struct {
		name string
		resp *pgllm.ChatResponse
	}{
		{
			name: "nil response",
			resp: nil,
		},
		{
			name: "no content blocks",
			resp: &pgllm.ChatResponse{},
		},
		{
			name: "reasoning block only",
			resp: &pgllm.ChatResponse{
				Content: []pgllm.ContentBlock{
					{Type: thinkingBlock, Text: "let me consider each database"},
				},
			},
		},
		{
			name: "whitespace-only text",
			resp: &pgllm.ChatResponse{
				Content: []pgllm.ContentBlock{
					{Type: pgllm.BlockText, Text: " \n\t "},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseDatabaseAnalysisResponse(tc.resp, databases)
			if !errors.Is(err, llmproxy.ErrNoTextContent) {
				t.Fatalf("expected ErrNoTextContent, got %v", err)
			}
			if result != nil {
				t.Errorf("expected nil result on error, got %v", result)
			}
		})
	}
}

func TestServerInfoAttachExtensionsToDatabases(t *testing.T) {
	h := &ServerInfoHandler{}

	t.Run("extensions grouped by database", func(t *testing.T) {
		databases := []DatabaseInfo{
			{Name: "app", Extensions: []string{}},
			{Name: "analytics", Extensions: []string{}},
		}
		extensions := []ExtensionInfo{
			{Name: "pgcrypto", Database: "app"},
			{Name: "pg_stat_statements", Database: "app"},
			{Name: "hstore", Database: "analytics"},
		}

		h.attachExtensionsToDatabases(extensions, databases)

		if len(databases[0].Extensions) != 2 {
			t.Errorf("expected 2 extensions for app, got %d",
				len(databases[0].Extensions))
		}
		if databases[0].Extensions[0] != "pgcrypto" {
			t.Errorf("expected first extension 'pgcrypto', got %q",
				databases[0].Extensions[0])
		}
		if databases[0].Extensions[1] != "pg_stat_statements" {
			t.Errorf("expected second extension 'pg_stat_statements', got %q",
				databases[0].Extensions[1])
		}
		if len(databases[1].Extensions) != 1 {
			t.Errorf("expected 1 extension for analytics, got %d",
				len(databases[1].Extensions))
		}
		if databases[1].Extensions[0] != "hstore" {
			t.Errorf("expected extension 'hstore', got %q",
				databases[1].Extensions[0])
		}
	})

	t.Run("database with no extensions keeps empty slice", func(t *testing.T) {
		databases := []DatabaseInfo{
			{Name: "app", Extensions: []string{}},
			{Name: "empty_db", Extensions: []string{}},
		}
		extensions := []ExtensionInfo{
			{Name: "pgcrypto", Database: "app"},
		}

		h.attachExtensionsToDatabases(extensions, databases)

		if len(databases[1].Extensions) != 0 {
			t.Errorf("expected 0 extensions for empty_db, got %d",
				len(databases[1].Extensions))
		}
	})

	t.Run("empty inputs handled", func(t *testing.T) {
		databases := []DatabaseInfo{}
		extensions := []ExtensionInfo{}

		// Should not panic with empty slices
		h.attachExtensionsToDatabases(extensions, databases)

		if len(databases) != 0 {
			t.Errorf("expected empty databases, got %d", len(databases))
		}
	})

	t.Run("nil inputs handled", func(t *testing.T) {
		// Should not panic with nil slices
		h.attachExtensionsToDatabases(nil, nil)
	})

	t.Run("extension for unknown database is ignored", func(t *testing.T) {
		databases := []DatabaseInfo{
			{Name: "app", Extensions: []string{}},
		}
		extensions := []ExtensionInfo{
			{Name: "pgcrypto", Database: "app"},
			{Name: "hstore", Database: "nonexistent"},
		}

		h.attachExtensionsToDatabases(extensions, databases)

		if len(databases[0].Extensions) != 1 {
			t.Errorf("expected 1 extension for app, got %d",
				len(databases[0].Extensions))
		}
	})
}

func TestServerInfoCreateLLMClient(t *testing.T) {
	t.Run("anthropic provider creates client", func(t *testing.T) {
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{
				Provider:        "anthropic",
				AnthropicAPIKey: "test-key",
				Model:           "claude-3-haiku-20240307",
			},
		}

		client, err := h.createLLMClient()
		if err != nil {
			t.Fatalf("expected no error for anthropic provider, got %v", err)
		}
		if client == nil {
			t.Error("expected non-nil client for anthropic provider")
		}
	})

	t.Run("openai provider creates client", func(t *testing.T) {
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{
				Provider:     "openai",
				OpenAIAPIKey: "test-key",
				Model:        "gpt-4",
			},
		}

		client, err := h.createLLMClient()
		if err != nil {
			t.Fatalf("expected no error for openai provider, got %v", err)
		}
		if client == nil {
			t.Error("expected non-nil client for openai provider")
		}
	})

	t.Run("gemini provider creates client", func(t *testing.T) {
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{
				Provider:     "gemini",
				GeminiAPIKey: "test-key",
				Model:        "gemini-2.5-flash",
			},
		}

		client, err := h.createLLMClient()
		if err != nil {
			t.Fatalf("expected no error for gemini provider, got %v", err)
		}
		if client == nil {
			t.Error("expected non-nil client for gemini provider")
		}
	})

	t.Run("ollama provider creates client", func(t *testing.T) {
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{
				Provider:  "ollama",
				OllamaURL: "http://localhost:11434",
				Model:     "llama2",
			},
		}

		client, err := h.createLLMClient()
		if err != nil {
			t.Fatalf("expected no error for ollama provider, got %v", err)
		}
		if client == nil {
			t.Error("expected non-nil client for ollama provider")
		}
	})

	t.Run("positive timeout honored", func(t *testing.T) {
		// A positive TimeoutSeconds on the underlying config must produce a
		// usable client; the library defers credential validation so a valid
		// provider yields a non-nil client without error.
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{
				Provider:  "ollama",
				OllamaURL: "http://localhost:11434",
				Model:     "llama2",
				LLMConfig: &config.LLMConfig{
					TimeoutSeconds: 45,
				},
			},
		}

		client, err := h.createLLMClient()
		if err != nil {
			t.Fatalf("expected no error with positive timeout, got %v", err)
		}
		if client == nil {
			t.Error("expected non-nil client when timeout is configured")
		}
	})

	t.Run("empty provider returns error", func(t *testing.T) {
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{
				Provider: "",
			},
		}

		client, err := h.createLLMClient()
		if err == nil {
			t.Error("expected an error for empty provider")
		}
		if client != nil {
			t.Error("expected nil client for empty provider")
		}
	})

	t.Run("unknown provider returns error", func(t *testing.T) {
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{
				Provider: "unsupported-provider",
			},
		}

		client, err := h.createLLMClient()
		if err == nil {
			t.Error("expected an error for unknown provider")
		}
		if client != nil {
			t.Error("expected nil client for unknown provider")
		}
	})
}

func TestServerInfoGetAIAnalysis(t *testing.T) {
	t.Run("returns nil when llmConfig is nil", func(t *testing.T) {
		h := &ServerInfoHandler{
			llmConfig: nil,
			cache:     make(map[int]*aiCacheEntry),
		}

		result, err := h.getAIAnalysis(
			context.Background(), 1, []DatabaseInfo{{Name: "db"}}, nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Error("expected nil when llmConfig is nil")
		}
	})

	t.Run("returns nil when provider is empty", func(t *testing.T) {
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{Provider: ""},
			cache:     make(map[int]*aiCacheEntry),
		}

		result, err := h.getAIAnalysis(
			context.Background(), 1, []DatabaseInfo{{Name: "db"}}, nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Error("expected nil when provider is empty")
		}
	})

	t.Run("returns nil when databases slice is empty", func(t *testing.T) {
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{Provider: "anthropic"},
			cache:     make(map[int]*aiCacheEntry),
		}

		result, err := h.getAIAnalysis(
			context.Background(), 1, []DatabaseInfo{}, nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Error("expected nil when databases is empty")
		}
	})

	t.Run("returns cached result when not expired", func(t *testing.T) {
		now := time.Now().UTC()
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{Provider: "anthropic"},
			cache: map[int]*aiCacheEntry{
				1: {
					analysis:    map[string]string{"mydb": "A cached description."},
					generatedAt: now,
					expiresAt:   now.Add(5 * time.Minute),
				},
			},
		}

		result, err := h.getAIAnalysis(
			context.Background(), 1,
			[]DatabaseInfo{{Name: "mydb"}}, nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil cached result")
		}
		if result.Databases["mydb"] != "A cached description." {
			t.Errorf("unexpected cached description: %q",
				result.Databases["mydb"])
		}
		if !result.GeneratedAt.Equal(now) {
			t.Errorf("expected GeneratedAt %v, got %v",
				now, result.GeneratedAt)
		}
	})

	t.Run("skips expired cache entry", func(t *testing.T) {
		past := time.Now().UTC().Add(-10 * time.Minute)
		h := &ServerInfoHandler{
			// Use empty provider so it returns nil after skipping cache
			// rather than trying to call a real LLM
			llmConfig: &llmproxy.Config{Provider: ""},
			cache: map[int]*aiCacheEntry{
				1: {
					analysis:    map[string]string{"mydb": "Old."},
					generatedAt: past,
					expiresAt:   past.Add(5 * time.Minute),
				},
			},
		}

		result, err := h.getAIAnalysis(
			context.Background(), 1,
			[]DatabaseInfo{{Name: "mydb"}}, nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// With empty provider, getAIAnalysis returns nil early
		if result != nil {
			t.Error("expected nil for empty provider after expired cache")
		}
	})

	t.Run("cache miss for different connection ID", func(t *testing.T) {
		now := time.Now().UTC()
		h := &ServerInfoHandler{
			// Use empty provider so it returns nil after cache miss
			llmConfig: &llmproxy.Config{Provider: ""},
			cache: map[int]*aiCacheEntry{
				1: {
					analysis:    map[string]string{"mydb": "Cached."},
					generatedAt: now,
					expiresAt:   now.Add(5 * time.Minute),
				},
			},
		}

		result, err := h.getAIAnalysis(
			context.Background(), 2,
			[]DatabaseInfo{{Name: "mydb"}}, nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Error("expected nil for cache miss on different connection ID")
		}
	})

	t.Run("returns nil when client construction fails", func(t *testing.T) {
		// A non-empty but unsupported provider passes the empty-provider
		// gate yet makes createLLMClient fail; getAIAnalysis must return
		// nil rather than panic on the nil client.
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{Provider: "unsupported-provider"},
			cache:     make(map[int]*aiCacheEntry),
		}

		result, err := h.getAIAnalysis(
			context.Background(), 1,
			[]DatabaseInfo{{Name: "mydb"}}, nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Error("expected nil when client construction fails")
		}
	})

	t.Run("returns analysis and caches on successful LLM call", func(t *testing.T) {
		// Drive the full happy path: an OpenAI-compatible stub returns a
		// per-database description, which getAIAnalysis must parse, return,
		// and cache for the connection ID.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/chat/completions" {
				http.Error(w, "unexpected path", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"choices": [{"message": {"role": "assistant", "content": "mydb: A primary application store.\n"}, "finish_reason": "stop"}],
				"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
			}`))
		}))
		defer srv.Close()

		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{
				Provider:      "openai",
				Model:         "gpt-4o",
				OpenAIAPIKey:  "test-key",
				OpenAIBaseURL: srv.URL,
			},
			cache: make(map[int]*aiCacheEntry),
		}

		result, err := h.getAIAnalysis(
			context.Background(), 7,
			[]DatabaseInfo{{Name: "mydb"}}, nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil analysis on successful LLM call")
		}
		if result.Databases["mydb"] != "A primary application store." {
			t.Errorf("unexpected description: %q", result.Databases["mydb"])
		}

		// The result must be cached under the connection ID.
		h.cacheMu.RLock()
		entry, ok := h.cache[7]
		h.cacheMu.RUnlock()
		if !ok {
			t.Fatal("expected cache entry for connection 7")
		}
		if entry.analysis["mydb"] != "A primary application store." {
			t.Errorf("unexpected cached description: %q", entry.analysis["mydb"])
		}
	})

	t.Run("returns nil when LLM call fails", func(t *testing.T) {
		// A non-retryable 400 makes Chat fail fast; getAIAnalysis logs and
		// returns nil without caching.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad request", http.StatusBadRequest)
		}))
		defer srv.Close()

		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{
				Provider:      "openai",
				Model:         "gpt-4o",
				OpenAIAPIKey:  "test-key",
				OpenAIBaseURL: srv.URL,
			},
			cache: make(map[int]*aiCacheEntry),
		}

		result, err := h.getAIAnalysis(
			context.Background(), 8,
			[]DatabaseInfo{{Name: "mydb"}}, nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Error("expected nil when the LLM call fails")
		}
		h.cacheMu.RLock()
		_, ok := h.cache[8]
		h.cacheMu.RUnlock()
		if ok {
			t.Error("expected no cache entry when the LLM call fails")
		}
	})

	t.Run("reports an error when the model returns no text", func(t *testing.T) {
		// The issue #399 shape: the provider answers successfully but the
		// message carries no text because the reasoning consumed the whole
		// output budget. getAIAnalysis must report it and cache nothing.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"choices": [{"message": {"role": "assistant", "content": ""}, "finish_reason": "length"}]
			}`))
		}))
		defer srv.Close()

		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{
				Provider:      "openai",
				Model:         "gpt-4o",
				OpenAIAPIKey:  "test-key",
				OpenAIBaseURL: srv.URL,
			},
			cache: make(map[int]*aiCacheEntry),
		}

		result, err := h.getAIAnalysis(
			context.Background(), 9,
			[]DatabaseInfo{{Name: "mydb"}}, nil,
		)
		if !errors.Is(err, llmproxy.ErrNoTextContent) {
			t.Fatalf("expected ErrNoTextContent, got %v", err)
		}
		if result != nil {
			t.Errorf("expected nil analysis on error, got %v", result)
		}
		h.cacheMu.RLock()
		_, ok := h.cache[9]
		h.cacheMu.RUnlock()
		if ok {
			t.Error("expected no cache entry when the model returns no text")
		}
	})
}

// TestServerInfoAIAnalysisMaxTokensHonorsConfig verifies that the analysis
// chat request carries the operator-configured llm.max_tokens, falling back
// to llmproxy.DefaultAnalysisMaxTokens when the setting is unset or
// non-positive. Regression cover for issue #399, where a hardcoded
// 512-token cap starved reasoning models of output budget.
func TestServerInfoAIAnalysisMaxTokensHonorsConfig(t *testing.T) {
	tests := []struct {
		name      string
		maxTokens int
		want      float64
	}{
		{"configured value used", 8192, 8192},
		{"unset falls back", 0, llmproxy.DefaultAnalysisMaxTokens},
		{"negative falls back", -3, llmproxy.DefaultAnalysisMaxTokens},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Errorf("failed to decode request body: %v", err)
				}
				got = payload["max_tokens"]
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{
					"choices": [{"message": {"role": "assistant", "content": "mydb: A store.\n"}, "finish_reason": "stop"}]
				}`)
			}))
			defer srv.Close()

			h := &ServerInfoHandler{
				llmConfig: &llmproxy.Config{
					Provider:      "openai",
					Model:         "gpt-4o",
					OpenAIAPIKey:  "test-key",
					OpenAIBaseURL: srv.URL,
					MaxTokens:     tc.maxTokens,
				},
				cache: make(map[int]*aiCacheEntry),
			}

			if _, err := h.getAIAnalysis(
				context.Background(), 1,
				[]DatabaseInfo{{Name: "mydb"}}, nil,
			); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			num, ok := got.(float64)
			if !ok {
				t.Fatalf("max_tokens missing or not a number in request: %#v", got)
			}
			if num != tc.want {
				t.Errorf("max_tokens: got %v, want %v", num, tc.want)
			}
		})
	}
}

// unreachablePool returns a lazily-created pool aimed at a closed port.
// pgxpool does not dial until a query runs, so every query fails with a
// connection error instead of panicking; that lets the handler-routing
// tests exercise the full request path without a live datastore.
func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(),
		"postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestHandleServerInfoAI covers the AI-analysis route: method and
// connection-ID validation, and the delegation to the analysis response
// writer. A nil auth store makes the RBAC checker grant access, and the
// unreachable pool makes the database queries fail benignly so the handler
// reaches the analysis step with no databases.
func TestHandleServerInfoAI(t *testing.T) {
	h := &ServerInfoHandler{
		datastore:   database.NewTestDatastore(unreachablePool(t)),
		rbacChecker: auth.NewRBACChecker(nil),
		llmConfig:   &llmproxy.Config{Provider: "openai", OpenAIAPIKey: "k"},
		cache:       make(map[int]*aiCacheEntry),
	}

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "non-GET is rejected",
			method:     http.MethodPost,
			path:       "/api/v1/server-info/1/ai-analysis",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "missing connection ID",
			method:     http.MethodGet,
			path:       "/api/v1/server-info/ai-analysis",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "non-numeric connection ID",
			method:     http.MethodGet,
			path:       "/api/v1/server-info/abc/ai-analysis",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "no databases yields a null body",
			method:     http.MethodGet,
			path:       "/api/v1/server-info/1/ai-analysis",
			wantStatus: http.StatusOK,
			wantBody:   "null",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			h.handleServerInfoRouting(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("status: got %d, want %d (%s)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(w.Body.String(), tc.wantBody) {
				t.Errorf("body %q does not contain %q", w.Body.String(), tc.wantBody)
			}
		})
	}

	t.Run("RBAC denial is a 403", func(t *testing.T) {
		// A real auth store plus a sharing lookup that reports the
		// connection as private makes the RBAC check deny an
		// unauthenticated caller before any analysis happens.
		tmpDir := t.TempDir()
		store, err := auth.NewAuthStore(tmpDir, 0, 0)
		if err != nil {
			t.Fatalf("failed to create auth store: %v", err)
		}
		defer store.Close()

		sharing := func(context.Context, int) (bool, string, error) {
			return false, "someone-else", nil
		}

		denied := &ServerInfoHandler{
			datastore:   h.datastore,
			rbacChecker: auth.NewRBACCheckerWithSharing(store, sharing),
			llmConfig:   h.llmConfig,
			cache:       make(map[int]*aiCacheEntry),
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/server-info/1/ai-analysis", nil)
		w := httptest.NewRecorder()
		denied.handleServerInfoRouting(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("status: got %d, want %d (%s)", w.Code, http.StatusForbidden, w.Body.String())
		}
	})
}

// TestWriteAIAnalysisResponse verifies the three response shapes the
// endpoint can produce: a 502 with an actionable message when the model
// returned nothing usable, a 200 null body when analysis is simply
// unavailable, and a 200 with the analysis on success.
func TestWriteAIAnalysisResponse(t *testing.T) {
	tests := []struct {
		name         string
		analysis     *AIAnalysisInfo
		err          error
		wantStatus   int
		wantContains string
	}{
		{
			name:         "no usable text is reported as 502",
			err:          llmproxy.ErrNoTextContent,
			wantStatus:   http.StatusBadGateway,
			wantContains: "llm.max_tokens",
		},
		{
			name:         "unavailable analysis is a null 200",
			wantStatus:   http.StatusOK,
			wantContains: "null",
		},
		{
			name: "analysis is returned on success",
			analysis: &AIAnalysisInfo{
				Databases: map[string]string{"mydb": "A store."},
			},
			wantStatus:   http.StatusOK,
			wantContains: "A store.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeAIAnalysisResponse(w, 1, tc.analysis, tc.err)
			if w.Code != tc.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tc.wantStatus)
			}
			if !strings.Contains(w.Body.String(), tc.wantContains) {
				t.Errorf("body %q does not contain %q", w.Body.String(), tc.wantContains)
			}
		})
	}
}

func TestServerInfoInvalidateCache(t *testing.T) {
	now := time.Now().UTC()
	h := &ServerInfoHandler{
		llmConfig: &llmproxy.Config{Provider: "anthropic"},
		cache: map[int]*aiCacheEntry{
			1: {
				analysis:    map[string]string{"db1": "Analysis 1"},
				generatedAt: now,
				expiresAt:   now.Add(5 * time.Minute),
			},
			2: {
				analysis:    map[string]string{"db2": "Analysis 2"},
				generatedAt: now,
				expiresAt:   now.Add(5 * time.Minute),
			},
		},
	}

	// Verify cache is populated
	if len(h.cache) != 2 {
		t.Fatalf("expected 2 cache entries, got %d", len(h.cache))
	}

	// Invalidate
	h.InvalidateCache()

	// Verify cache is empty
	if len(h.cache) != 0 {
		t.Errorf("expected 0 cache entries after invalidation, got %d",
			len(h.cache))
	}

	// Verify cache still works after invalidation (not nil)
	h.cacheMu.RLock()
	_, ok := h.cache[1]
	h.cacheMu.RUnlock()
	if ok {
		t.Error("expected entry 1 to not exist after invalidation")
	}
}

func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && // avoid trivial matches
		len(s) >= len(substr) &&
		indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
