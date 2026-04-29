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

// ---------------------------------------------------------------------------
// NewClientFromLLMConfig tests
// ---------------------------------------------------------------------------

func TestNewClientFromLLMConfig_DefaultsFromConfig(t *testing.T) {
	cfg := LLMConfig{
		Provider:               "anthropic",
		Model:                  "claude-default",
		AnthropicAPIKey:        "anth-key",
		MaxTokens:              1024,
		Temperature:            0.7,
		UseCompactDescriptions: true,
	}
	client, err := NewClientFromLLMConfig(cfg, LLMOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClientFromLLMConfig_OverridesWin(t *testing.T) {
	// Empty/zero LLMConfig fields cannot supply credentials; provide
	// the API key on the config and verify per-call overrides win.
	cfg := LLMConfig{
		Provider:        "anthropic",
		Model:           "default-model",
		AnthropicAPIKey: "anth-key",
		MaxTokens:       100,
		Temperature:     0.1,
	}
	headers := map[string]string{"X-Custom": "value"}
	client, err := NewClientFromLLMConfig(cfg, LLMOptions{
		Provider:    "anthropic",
		Model:       "override-model",
		MaxTokens:   2048,
		Temperature: 0.9,
		Debug:       true,
		Headers:     headers,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClientFromLLMConfig_OverrideProvider(t *testing.T) {
	// LLMConfig defaults to anthropic but lacks an OpenAI key. Override
	// to openai with a base URL so the OpenAI branch's no-key path is
	// satisfied.
	cfg := LLMConfig{
		Provider:        "anthropic",
		Model:           "claude-default",
		AnthropicAPIKey: "anth-key",
		OpenAIBaseURL:   "http://localhost:8080",
	}
	client, err := NewClientFromLLMConfig(cfg, LLMOptions{
		Provider: "openai",
		Model:    "gpt-x",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClientFromLLMConfig_PropagatesValidationError(t *testing.T) {
	// Anthropic without API key must surface the validation error
	// from NewClientFromConfig.
	cfg := LLMConfig{
		Provider: "anthropic",
		Model:    "claude-3",
	}
	client, err := NewClientFromLLMConfig(cfg, LLMOptions{})
	if err == nil {
		t.Fatal("expected validation error for missing API key")
	}
	if client != nil {
		t.Fatal("expected nil client on error")
	}
	if !strings.Contains(err.Error(), "AnthropicAPIKey") {
		t.Errorf("expected AnthropicAPIKey error, got %v", err)
	}
}

func TestNewClientFromLLMConfig_PropagatesEmptyProvider(t *testing.T) {
	cfg := LLMConfig{
		Model: "some-model",
	}
	client, err := NewClientFromLLMConfig(cfg, LLMOptions{})
	if err == nil {
		t.Fatal("expected error for empty provider")
	}
	if client != nil {
		t.Fatal("expected nil client on error")
	}
	if !strings.Contains(err.Error(), "provider is required") {
		t.Errorf("expected 'provider is required', got %v", err)
	}
}

func TestNewClientFromLLMConfig_OllamaUsesConfigURL(t *testing.T) {
	cfg := LLMConfig{
		Provider:  "ollama",
		Model:     "llama3",
		OllamaURL: "http://localhost:11434",
	}
	client, err := NewClientFromLLMConfig(cfg, LLMOptions{Debug: true})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClientFromLLMConfig_GeminiBranch(t *testing.T) {
	cfg := LLMConfig{
		Provider:     "gemini",
		Model:        "gemini-pro",
		GeminiAPIKey: "gem-key",
		MaxTokens:    256,
		Temperature:  0.5,
	}
	client, err := NewClientFromLLMConfig(cfg, LLMOptions{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
