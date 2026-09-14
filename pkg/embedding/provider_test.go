/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package embedding

import (
	"testing"
)

// TestNewProvider_OpenAI_BaseURLNoKey verifies that the OpenAI branch accepts
// an empty API key when a custom base URL is supplied (the local
// OpenAI-compatible case).
func TestNewProvider_OpenAI_BaseURLNoKey(t *testing.T) {
	cfg := Config{
		Provider:      "openai",
		Model:         "text-embedding-3-small",
		OpenAIBaseURL: "http://localhost:8080/v1",
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("empty API key should be allowed with custom base URL: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

// TestNewProvider_DefaultModel verifies the per-provider default-model lookup
// when Config.Model is empty.
func TestNewProvider_DefaultModel(t *testing.T) {
	cfg := Config{Provider: "ollama"}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.ModelName() != "nomic-embed-text" {
		t.Errorf("expected default model 'nomic-embed-text', got %q", provider.ModelName())
	}
}

// TestNewProvider_CustomBaseURLs exercises each provider's base-URL plumbing
// through the public constructor surface.
func TestNewProvider_CustomBaseURLs(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantPrv string
	}{
		{
			name: "openai",
			cfg: Config{
				Provider:      "openai",
				Model:         "text-embedding-3-small",
				OpenAIAPIKey:  "test-api-key-12345678",
				OpenAIBaseURL: "https://custom.openai.example.com/v1",
			},
			wantPrv: "openai",
		},
		{
			name: "voyage",
			cfg: Config{
				Provider:      "voyage",
				Model:         "voyage-3-lite",
				VoyageAPIKey:  "pa-test-key-12345678",
				VoyageBaseURL: "https://custom.voyageai.com/v1/embeddings",
			},
			wantPrv: "voyage",
		},
		{
			name: "gemini",
			cfg: Config{
				Provider:      "gemini",
				Model:         "gemini-embedding-001",
				GeminiAPIKey:  "AIza-test-key-12345678",
				GeminiBaseURL: "https://custom.example.com",
			},
			wantPrv: "gemini",
		},
		{
			name: "ollama",
			cfg: Config{
				Provider:  "ollama",
				Model:     "nomic-embed-text",
				OllamaURL: "http://custom-ollama.local:11434",
			},
			wantPrv: "ollama",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			provider, err := NewProvider(c.cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if provider == nil {
				t.Fatal("expected non-nil provider")
			}
			if provider.ProviderName() != c.wantPrv {
				t.Errorf("expected provider name %q, got %q", c.wantPrv, provider.ProviderName())
			}
		})
	}
}

// TestNewProvider_Unsupported verifies the error message for an unsupported
// provider name.
func TestNewProvider_Unsupported(t *testing.T) {
	cfg := Config{Provider: "unsupported"}

	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	want := "unsupported embedding provider: unsupported (supported: voyage, openai, gemini, ollama)"
	if err.Error() != want {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestNewProvider_EmptyProvider verifies that an empty provider name is
// rejected.
func TestNewProvider_EmptyProvider(t *testing.T) {
	cfg := Config{Provider: ""}

	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for empty provider")
	}
}

// TestConfigStruct verifies that the Config fields round-trip as set.
func TestConfigStruct(t *testing.T) {
	cfg := Config{
		Provider:     "voyage",
		Model:        "voyage-3",
		VoyageAPIKey: "voyage-key",
		OpenAIAPIKey: "openai-key",
		OllamaURL:    "http://localhost:11434",
	}

	if cfg.Provider != "voyage" {
		t.Errorf("expected provider 'voyage', got %q", cfg.Provider)
	}
	if cfg.Model != "voyage-3" {
		t.Errorf("expected model 'voyage-3', got %q", cfg.Model)
	}
	if cfg.VoyageAPIKey != "voyage-key" {
		t.Errorf("expected VoyageAPIKey 'voyage-key', got %q", cfg.VoyageAPIKey)
	}
	if cfg.OpenAIAPIKey != "openai-key" {
		t.Errorf("expected OpenAIAPIKey 'openai-key', got %q", cfg.OpenAIAPIKey)
	}
	if cfg.OllamaURL != "http://localhost:11434" {
		t.Errorf("expected OllamaURL 'http://localhost:11434', got %q", cfg.OllamaURL)
	}
}
