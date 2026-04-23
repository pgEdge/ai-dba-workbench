/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package chat

import (
	"strings"
	"testing"
)

func TestNewClientFromConfig_Anthropic(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := ClientConfig{
			Provider:        "anthropic",
			AnthropicAPIKey: "test-key",
			Model:           "claude-3-opus",
			MaxTokens:       1024,
			Temperature:     0.7,
		}
		client, err := NewClientFromConfig(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("missing API key", func(t *testing.T) {
		cfg := ClientConfig{
			Provider: "anthropic",
			Model:    "claude-3-opus",
		}
		client, err := NewClientFromConfig(cfg)
		if err == nil {
			t.Fatal("expected error for missing API key")
		}
		if client != nil {
			t.Fatal("expected nil client on error")
		}
		if !strings.Contains(err.Error(), "AnthropicAPIKey") {
			t.Errorf("expected error to mention AnthropicAPIKey, got %v", err)
		}
	})
}

func TestNewClientFromConfig_OpenAI(t *testing.T) {
	t.Run("valid config with API key", func(t *testing.T) {
		cfg := ClientConfig{
			Provider:     "openai",
			OpenAIAPIKey: "test-key",
			Model:        "gpt-4",
			MaxTokens:    2048,
			Temperature:  0.5,
		}
		client, err := NewClientFromConfig(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("valid config with base URL only", func(t *testing.T) {
		cfg := ClientConfig{
			Provider:      "openai",
			OpenAIBaseURL: "http://localhost:8080",
			Model:         "local-model",
		}
		client, err := NewClientFromConfig(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("missing API key and base URL", func(t *testing.T) {
		cfg := ClientConfig{
			Provider: "openai",
			Model:    "gpt-4",
		}
		client, err := NewClientFromConfig(cfg)
		if err == nil {
			t.Fatal("expected error for missing credentials")
		}
		if client != nil {
			t.Fatal("expected nil client on error")
		}
		if !strings.Contains(err.Error(), "OpenAIAPIKey") {
			t.Errorf("expected error to mention OpenAIAPIKey, got %v", err)
		}
	})
}

func TestNewClientFromConfig_Gemini(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := ClientConfig{
			Provider:     "gemini",
			GeminiAPIKey: "test-key",
			Model:        "gemini-pro",
			MaxTokens:    1024,
			Temperature:  0.3,
		}
		client, err := NewClientFromConfig(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("missing API key", func(t *testing.T) {
		cfg := ClientConfig{
			Provider: "gemini",
			Model:    "gemini-pro",
		}
		client, err := NewClientFromConfig(cfg)
		if err == nil {
			t.Fatal("expected error for missing API key")
		}
		if client != nil {
			t.Fatal("expected nil client on error")
		}
		if !strings.Contains(err.Error(), "GeminiAPIKey") {
			t.Errorf("expected error to mention GeminiAPIKey, got %v", err)
		}
	})
}

func TestNewClientFromConfig_Ollama(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := ClientConfig{
			Provider:  "ollama",
			OllamaURL: "http://localhost:11434",
			Model:     "llama3",
			Debug:     true,
		}
		client, err := NewClientFromConfig(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("missing URL", func(t *testing.T) {
		cfg := ClientConfig{
			Provider: "ollama",
			Model:    "llama3",
		}
		client, err := NewClientFromConfig(cfg)
		if err == nil {
			t.Fatal("expected error for missing URL")
		}
		if client != nil {
			t.Fatal("expected nil client on error")
		}
		if !strings.Contains(err.Error(), "OllamaURL") {
			t.Errorf("expected error to mention OllamaURL, got %v", err)
		}
	})
}

func TestNewClientFromConfig_UnsupportedProvider(t *testing.T) {
	cfg := ClientConfig{
		Provider: "unknown-provider",
		Model:    "some-model",
	}
	client, err := NewClientFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if client != nil {
		t.Fatal("expected nil client on error")
	}
	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("expected error to mention unsupported provider, got %v", err)
	}
}

func TestNewClientFromConfig_EmptyProvider(t *testing.T) {
	cfg := ClientConfig{
		Model: "some-model",
	}
	client, err := NewClientFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty provider")
	}
	if client != nil {
		t.Fatal("expected nil client on error")
	}
	if !strings.Contains(err.Error(), "provider is required") {
		t.Errorf("expected error to mention provider is required, got %v", err)
	}
}

func TestNewClientFromConfig_WithHeaders(t *testing.T) {
	headers := map[string]string{
		"X-Custom-Header": "test-value",
		"Authorization":   "Bearer custom-token",
	}
	cfg := ClientConfig{
		Provider:        "anthropic",
		AnthropicAPIKey: "test-key",
		Model:           "claude-3-opus",
		Headers:         headers,
	}
	client, err := NewClientFromConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClientFromConfig_WithCompactDescriptions(t *testing.T) {
	cfg := ClientConfig{
		Provider:        "anthropic",
		AnthropicAPIKey: "test-key",
		Model:           "claude-3-opus",
		UseCompactDescs: true,
	}
	client, err := NewClientFromConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
