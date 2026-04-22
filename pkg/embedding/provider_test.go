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

func TestNewProvider_Voyage(t *testing.T) {
    t.Run("valid config", func(t *testing.T) {
        cfg := Config{
            Provider:     "voyage",
            Model:        "voyage-3-lite",
            VoyageAPIKey: "test-api-key-12345678",
        }

        provider, err := NewProvider(cfg)
        if err != nil {
            t.Fatalf("unexpected error: %v", err)
        }
        if provider == nil {
            t.Fatal("expected non-nil provider")
        }
        if provider.ProviderName() != "voyage" {
            t.Errorf("expected provider name 'voyage', got %q", provider.ProviderName())
        }
    })

    t.Run("missing API key", func(t *testing.T) {
        cfg := Config{
            Provider: "voyage",
            Model:    "voyage-3-lite",
        }

        _, err := NewProvider(cfg)
        if err == nil {
            t.Fatal("expected error for missing API key")
        }
    })
}

func TestNewProvider_OpenAI(t *testing.T) {
    t.Run("valid config", func(t *testing.T) {
        cfg := Config{
            Provider:     "openai",
            Model:        "text-embedding-3-small",
            OpenAIAPIKey: "test-api-key-12345678",
        }

        provider, err := NewProvider(cfg)
        if err != nil {
            t.Fatalf("unexpected error: %v", err)
        }
        if provider == nil {
            t.Fatal("expected non-nil provider")
        }
        if provider.ProviderName() != "openai" {
            t.Errorf("expected provider name 'openai', got %q", provider.ProviderName())
        }
    })

    t.Run("missing API key without base URL", func(t *testing.T) {
        cfg := Config{
            Provider: "openai",
            Model:    "text-embedding-3-small",
        }

        _, err := NewProvider(cfg)
        if err == nil {
            t.Fatal("expected error for missing API key without custom base URL")
        }
    })

    t.Run("missing API key with custom base URL", func(t *testing.T) {
        cfg := Config{
            Provider:      "openai",
            Model:         "text-embedding-3-small",
            OpenAIBaseURL: "http://localhost:8080/v1",
        }

        provider, err := NewProvider(cfg)
        if err != nil {
            t.Fatalf("unexpected error: empty API key should be allowed with custom base URL: %v", err)
        }
        if provider == nil {
            t.Fatal("expected non-nil provider")
        }
    })
}

func TestNewProvider_Ollama(t *testing.T) {
    t.Run("valid config", func(t *testing.T) {
        cfg := Config{
            Provider:  "ollama",
            Model:     "nomic-embed-text",
            OllamaURL: "http://localhost:11434",
        }

        provider, err := NewProvider(cfg)
        if err != nil {
            t.Fatalf("unexpected error: %v", err)
        }
        if provider == nil {
            t.Fatal("expected non-nil provider")
        }
        if provider.ProviderName() != "ollama" {
            t.Errorf("expected provider name 'ollama', got %q", provider.ProviderName())
        }
    })

    t.Run("with defaults", func(t *testing.T) {
        cfg := Config{
            Provider: "ollama",
        }

        provider, err := NewProvider(cfg)
        if err != nil {
            t.Fatalf("unexpected error: %v", err)
        }
        if provider == nil {
            t.Fatal("expected non-nil provider")
        }
        // Should use default model
        if provider.ModelName() != "nomic-embed-text" {
            t.Errorf("expected default model 'nomic-embed-text', got %q", provider.ModelName())
        }
    })
}

func TestNewProvider_Unsupported(t *testing.T) {
    cfg := Config{
        Provider: "unsupported",
    }

    _, err := NewProvider(cfg)
    if err == nil {
        t.Fatal("expected error for unsupported provider")
    }
    if err.Error() != "unsupported embedding provider: unsupported (supported: voyage, openai, gemini, ollama)" {
        t.Errorf("unexpected error message: %v", err)
    }
}

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

func TestNewProvider_Gemini(t *testing.T) {
    t.Run("valid config", func(t *testing.T) {
        cfg := Config{
            Provider:     "gemini",
            Model:        "text-embedding-004",
            GeminiAPIKey: "AIza-test-key-12345678",
        }

        provider, err := NewProvider(cfg)
        if err != nil {
            t.Fatalf("unexpected error: %v", err)
        }
        if provider == nil {
            t.Fatal("expected non-nil provider")
        }
        if provider.ProviderName() != "gemini" {
            t.Errorf("expected provider name 'gemini', got %q", provider.ProviderName())
        }
    })

    t.Run("missing API key", func(t *testing.T) {
        cfg := Config{
            Provider: "gemini",
            Model:    "text-embedding-004",
        }

        _, err := NewProvider(cfg)
        if err == nil {
            t.Fatal("expected error for missing API key")
        }
    })

    t.Run("with custom base URL", func(t *testing.T) {
        cfg := Config{
            Provider:      "gemini",
            Model:         "text-embedding-004",
            GeminiAPIKey:  "AIza-test-key-12345678",
            GeminiBaseURL: "https://custom.example.com",
        }

        provider, err := NewProvider(cfg)
        if err != nil {
            t.Fatalf("unexpected error: %v", err)
        }
        if provider == nil {
            t.Fatal("expected non-nil provider")
        }
        // Verify the custom base URL was propagated to the provider
        geminiProvider, ok := provider.(*GeminiProvider)
        if !ok {
            t.Fatal("expected provider to be *GeminiProvider")
        }
        if geminiProvider.baseURL != "https://custom.example.com" {
            t.Errorf("expected base URL 'https://custom.example.com', got %q",
                geminiProvider.baseURL)
        }
    })
}

func TestNewProvider_Voyage_CustomBaseURL(t *testing.T) {
    cfg := Config{
        Provider:      "voyage",
        Model:         "voyage-3-lite",
        VoyageAPIKey:  "pa-test-key-12345678",
        VoyageBaseURL: "https://custom.voyageai.com/v1/embeddings",
    }

    provider, err := NewProvider(cfg)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if provider == nil {
        t.Fatal("expected non-nil provider")
    }
    // Verify the custom base URL was propagated to the provider
    voyageProvider, ok := provider.(*VoyageProvider)
    if !ok {
        t.Fatal("expected provider to be *VoyageProvider")
    }
    if voyageProvider.baseURL != "https://custom.voyageai.com/v1/embeddings" {
        t.Errorf("expected base URL 'https://custom.voyageai.com/v1/embeddings', got %q",
            voyageProvider.baseURL)
    }
}

func TestNewProvider_EmptyProvider(t *testing.T) {
    cfg := Config{
        Provider: "",
    }

    _, err := NewProvider(cfg)
    if err == nil {
        t.Fatal("expected error for empty provider")
    }
}

func TestProviderInterface(t *testing.T) {
    // Verify that all providers implement the Provider interface
    var _ Provider = (*VoyageProvider)(nil)
    var _ Provider = (*OpenAIProvider)(nil)
    var _ Provider = (*GeminiProvider)(nil)
    var _ Provider = (*OllamaProvider)(nil)
}

func TestNewProvider_OpenAI_CustomBaseURL(t *testing.T) {
    cfg := Config{
        Provider:      "openai",
        Model:         "text-embedding-3-small",
        OpenAIAPIKey:  "test-api-key-12345678",
        OpenAIBaseURL: "https://custom.openai.example.com/v1",
    }

    provider, err := NewProvider(cfg)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if provider == nil {
        t.Fatal("expected non-nil provider")
    }
    // Verify the custom base URL was propagated to the provider
    openaiProvider, ok := provider.(*OpenAIProvider)
    if !ok {
        t.Fatal("expected provider to be *OpenAIProvider")
    }
    if openaiProvider.baseURL != "https://custom.openai.example.com/v1" {
        t.Errorf("expected base URL 'https://custom.openai.example.com/v1', got %q",
            openaiProvider.baseURL)
    }
}

func TestNewProvider_Ollama_CustomBaseURL(t *testing.T) {
    cfg := Config{
        Provider:  "ollama",
        Model:     "nomic-embed-text",
        OllamaURL: "http://custom-ollama.local:11434",
    }

    provider, err := NewProvider(cfg)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if provider == nil {
        t.Fatal("expected non-nil provider")
    }
    // Verify the custom base URL was propagated to the provider
    ollamaProvider, ok := provider.(*OllamaProvider)
    if !ok {
        t.Fatal("expected provider to be *OllamaProvider")
    }
    if ollamaProvider.baseURL != "http://custom-ollama.local:11434" {
        t.Errorf("expected base URL 'http://custom-ollama.local:11434', got %q",
            ollamaProvider.baseURL)
    }
}
