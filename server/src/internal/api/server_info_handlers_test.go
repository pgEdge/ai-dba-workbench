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
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/chat"
	"github.com/pgedge/ai-workbench/server/internal/llmproxy"
)

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

	resp := chat.LLMResponse{
		Content: []interface{}{
			chat.TextContent{
				Text: "myapp: A web application database.\nanalytics: A data warehouse for reporting.\n",
			},
		},
	}

	result := parseDatabaseAnalysisResponse(resp, databases)

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

	resp := chat.LLMResponse{
		Content: []interface{}{
			chat.TextContent{
				Text: "- myapp: A web application database.\n" +
					"* analytics: A data warehouse for reporting.\n" +
					"**warehouse**: A storage system.\n" +
					"`logs`: A logging database.\n" +
					"1. metrics: A metrics store.\n",
			},
		},
	}

	result := parseDatabaseAnalysisResponse(resp, databases)

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

	resp := chat.LLMResponse{
		Content: []interface{}{
			chat.TextContent{
				Text: "myapp: A web app.\nunknown_db: Should be ignored.\n",
			},
		},
	}

	result := parseDatabaseAnalysisResponse(resp, databases)

	if len(result) != 1 {
		t.Errorf("expected 1 entry, got %d", len(result))
	}
	if _, ok := result["unknown_db"]; ok {
		t.Error("unknown_db should not be in result")
	}
}

func TestParseDatabaseAnalysisResponseHandlesMapContent(t *testing.T) {
	databases := []DatabaseInfo{
		{Name: "testdb"},
	}

	resp := chat.LLMResponse{
		Content: []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "testdb: A test database.\n",
			},
		},
	}

	result := parseDatabaseAnalysisResponse(resp, databases)

	if result["testdb"] != "A test database." {
		t.Errorf("unexpected testdb description: %q", result["testdb"])
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

		client := h.createLLMClient()
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

		client := h.createLLMClient()
		if client == nil {
			t.Error("expected non-nil client for openai provider")
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

		client := h.createLLMClient()
		if client == nil {
			t.Error("expected non-nil client for ollama provider")
		}
	})

	t.Run("empty provider returns nil", func(t *testing.T) {
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{
				Provider: "",
			},
		}

		client := h.createLLMClient()
		if client != nil {
			t.Error("expected nil client for empty provider")
		}
	})

	t.Run("unknown provider returns nil", func(t *testing.T) {
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{
				Provider: "unsupported-provider",
			},
		}

		client := h.createLLMClient()
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

		result := h.getAIAnalysis(
			context.Background(), 1, []DatabaseInfo{{Name: "db"}}, nil,
		)
		if result != nil {
			t.Error("expected nil when llmConfig is nil")
		}
	})

	t.Run("returns nil when provider is empty", func(t *testing.T) {
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{Provider: ""},
			cache:     make(map[int]*aiCacheEntry),
		}

		result := h.getAIAnalysis(
			context.Background(), 1, []DatabaseInfo{{Name: "db"}}, nil,
		)
		if result != nil {
			t.Error("expected nil when provider is empty")
		}
	})

	t.Run("returns nil when databases slice is empty", func(t *testing.T) {
		h := &ServerInfoHandler{
			llmConfig: &llmproxy.Config{Provider: "anthropic"},
			cache:     make(map[int]*aiCacheEntry),
		}

		result := h.getAIAnalysis(
			context.Background(), 1, []DatabaseInfo{}, nil,
		)
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

		result := h.getAIAnalysis(
			context.Background(), 1,
			[]DatabaseInfo{{Name: "mydb"}}, nil,
		)
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

		result := h.getAIAnalysis(
			context.Background(), 1,
			[]DatabaseInfo{{Name: "mydb"}}, nil,
		)
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

		result := h.getAIAnalysis(
			context.Background(), 2,
			[]DatabaseInfo{{Name: "mydb"}}, nil,
		)
		if result != nil {
			t.Error("expected nil for cache miss on different connection ID")
		}
	})
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
