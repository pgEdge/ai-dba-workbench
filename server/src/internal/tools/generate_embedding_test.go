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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

func TestGenerateEmbeddingTool_Definition(t *testing.T) {
	cfg := &config.Config{}
	tool := GenerateEmbeddingTool(cfg)

	if tool.Definition.Name != "generate_embedding" {
		t.Errorf("expected name 'generate_embedding', got %q", tool.Definition.Name)
	}

	if tool.Definition.Description == "" {
		t.Error("expected non-empty description")
	}

	// Check required parameters
	if len(tool.Definition.InputSchema.Required) != 1 {
		t.Errorf("expected 1 required parameter, got %d", len(tool.Definition.InputSchema.Required))
	}

	if tool.Definition.InputSchema.Required[0] != "text" {
		t.Errorf("expected 'text' to be required, got %q", tool.Definition.InputSchema.Required[0])
	}
}

func TestGenerateEmbeddingTool_NotEnabled(t *testing.T) {
	cfg := &config.Config{
		Embedding: config.EmbeddingConfig{
			Enabled: false,
		},
	}
	tool := GenerateEmbeddingTool(cfg)

	args := map[string]any{
		"text": "test text",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !response.IsError {
		t.Error("expected error response when embedding is disabled")
	}

	if len(response.Content) == 0 {
		t.Fatal("expected error message in response")
	}

	if !strings.Contains(response.Content[0].Text, "not enabled") {
		t.Errorf("expected 'not enabled' in error message, got: %s", response.Content[0].Text)
	}
}

func TestGenerateEmbeddingTool_MissingText(t *testing.T) {
	cfg := &config.Config{
		Embedding: config.EmbeddingConfig{
			Enabled: true,
		},
	}
	tool := GenerateEmbeddingTool(cfg)

	args := map[string]any{}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !response.IsError {
		t.Error("expected error response when text is missing")
	}

	if len(response.Content) == 0 {
		t.Fatal("expected error message in response")
	}

	if !strings.Contains(response.Content[0].Text, "Missing or invalid 'text'") {
		t.Errorf("expected 'Missing or invalid' error, got: %s", response.Content[0].Text)
	}
}

func TestGenerateEmbeddingTool_EmptyText(t *testing.T) {
	cfg := &config.Config{
		Embedding: config.EmbeddingConfig{
			Enabled: true,
		},
	}
	tool := GenerateEmbeddingTool(cfg)

	args := map[string]any{
		"text": "",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !response.IsError {
		t.Error("expected error response when text is empty")
	}
}

func TestGenerateEmbeddingTool_WhitespaceOnlyText(t *testing.T) {
	cfg := &config.Config{
		Embedding: config.EmbeddingConfig{
			Enabled: true,
		},
	}
	tool := GenerateEmbeddingTool(cfg)

	args := map[string]any{
		"text": "   \t\n   ",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !response.IsError {
		t.Error("expected error response when text is whitespace only")
	}

	if len(response.Content) == 0 {
		t.Fatal("expected error message in response")
	}

	if !strings.Contains(response.Content[0].Text, "empty or whitespace-only") {
		t.Errorf("expected 'empty or whitespace-only' error, got: %s", response.Content[0].Text)
	}
}

func TestGenerateEmbeddingTool_InvalidTextType(t *testing.T) {
	cfg := &config.Config{
		Embedding: config.EmbeddingConfig{
			Enabled: true,
		},
	}
	tool := GenerateEmbeddingTool(cfg)

	args := map[string]any{
		"text": 123, // Wrong type
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !response.IsError {
		t.Error("expected error response when text has wrong type")
	}
}

func TestGenerateEmbeddingTool_InvalidProvider(t *testing.T) {
	cfg := &config.Config{
		Embedding: config.EmbeddingConfig{
			Enabled:  true,
			Provider: "invalid_provider",
		},
	}
	tool := GenerateEmbeddingTool(cfg)

	args := map[string]any{
		"text": "test text",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !response.IsError {
		t.Error("expected error response for invalid provider")
	}

	if len(response.Content) == 0 {
		t.Fatal("expected error message in response")
	}

	// Should fail to initialize the embedding provider
	if !strings.Contains(response.Content[0].Text, "Failed to initialize") {
		t.Errorf("expected 'Failed to initialize' error, got: %s", response.Content[0].Text)
	}
}

// TestGenerateEmbeddingTool_GeminiMissingAPIKey verifies that the
// factory rejects a Gemini configuration that lacks an API key, which
// confirms the new config plumbing reaches the embedding provider.
func TestGenerateEmbeddingTool_GeminiMissingAPIKey(t *testing.T) {
	cfg := &config.Config{
		Embedding: config.EmbeddingConfig{
			Enabled:  true,
			Provider: "gemini",
			Model:    "text-embedding-004",
		},
	}
	tool := GenerateEmbeddingTool(cfg)

	response, err := tool.Handler(map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected error response when Gemini API key is missing")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response")
	}
	if !strings.Contains(response.Content[0].Text, "Gemini API key is required") {
		t.Errorf("expected 'Gemini API key is required' error, got: %s",
			response.Content[0].Text)
	}
}

// TestGenerateEmbeddingTool_GeminiSuccess exercises the full Gemini
// happy path through the new config plumbing, including GeminiAPIKey
// and GeminiBaseURL. A local httptest server stands in for the Gemini
// embedContent endpoint, returning a fixed vector that the tool must
// echo back in its success response.
func TestGenerateEmbeddingTool_GeminiSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v1beta/models/text-embedding-004:embedContent") {
			t.Errorf("unexpected request path %q", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-gemini-key" {
			t.Errorf("expected API key in query string, got %q", r.URL.Query().Get("key"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// 768-element response would be ideal but a short one suffices to
		// exercise the decoder and the success-path formatting.
		_, _ = w.Write([]byte(`{"embedding":{"values":[0.1,0.2,0.3]}}`))
	}))
	defer srv.Close()

	cfg := &config.Config{
		Embedding: config.EmbeddingConfig{
			Enabled:       true,
			Provider:      "gemini",
			Model:         "text-embedding-004",
			GeminiAPIKey:  "test-gemini-key",
			GeminiBaseURL: srv.URL,
		},
	}
	tool := GenerateEmbeddingTool(cfg)

	response, err := tool.Handler(map[string]any{"text": "hello gemini"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if response.IsError {
		t.Fatalf("expected success response, got error: %s",
			response.Content[0].Text)
	}
	if len(response.Content) == 0 {
		t.Fatal("expected non-empty response content")
	}
	body := response.Content[0].Text
	if !strings.Contains(body, "Provider: gemini") {
		t.Errorf("expected 'Provider: gemini' in response, got: %s", body)
	}
	if !strings.Contains(body, "Model: text-embedding-004") {
		t.Errorf("expected 'Model: text-embedding-004' in response, got: %s", body)
	}
	if !strings.Contains(body, "0.1") || !strings.Contains(body, "0.3") {
		t.Errorf("expected embedding vector in response, got: %s", body)
	}
}
