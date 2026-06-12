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
	"context"
	"fmt"

	"github.com/pgEdge/pgedge-go-llm-lib/llm"
	_ "github.com/pgEdge/pgedge-go-llm-lib/llm/all" // register providers
)

// defaultModels preserves the Workbench default model per provider when the
// caller leaves Config.Model empty.
var defaultModels = map[string]string{
	"openai": "text-embedding-3-small",
	"voyage": "voyage-3-lite",
	"gemini": "gemini-embedding-001",
	"ollama": "nomic-embed-text",
}

// libProvider adapts an llm.Client to the embedding.Provider interface.
type libProvider struct {
	client llm.Client
}

func (p *libProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	return p.client.Embed(ctx, text)
}

func (p *libProvider) ModelName() string    { return p.client.Model() }
func (p *libProvider) ProviderName() string { return p.client.Provider() }

func newLibProvider(provider, apiKey, model, baseURL string) (Provider, error) {
	if model == "" {
		model = defaultModels[provider]
	}
	client, err := llm.NewClient(provider, llm.Options{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: baseURL,
	})
	if err != nil {
		return nil, fmt.Errorf("create %s embedding client: %w", provider, err)
	}
	return &libProvider{client: client}, nil
}

// NewOpenAIProvider constructs an OpenAI-backed embedding provider.
func NewOpenAIProvider(apiKey, model, baseURL string) (Provider, error) {
	return newLibProvider("openai", apiKey, model, baseURL)
}

// NewVoyageProvider constructs a Voyage-backed embedding provider.
func NewVoyageProvider(apiKey, model, baseURL string) (Provider, error) {
	return newLibProvider("voyage", apiKey, model, baseURL)
}

// NewGeminiProvider constructs a Gemini-backed embedding provider.
func NewGeminiProvider(apiKey, model, baseURL string) (Provider, error) {
	return newLibProvider("gemini", apiKey, model, baseURL)
}

// NewOllamaProvider constructs an Ollama-backed embedding provider.
func NewOllamaProvider(baseURL, model string) (Provider, error) {
	return newLibProvider("ollama", "", model, baseURL)
}

// NewProvider builds an embedding provider from Config, preserving the
// validation behaviour of the previous implementation.
func NewProvider(cfg Config) (Provider, error) {
	switch cfg.Provider {
	case "openai":
		if cfg.OpenAIAPIKey == "" && cfg.OpenAIBaseURL == "" {
			return nil, fmt.Errorf("OpenAI API key is required when provider is 'openai'")
		}
		return NewOpenAIProvider(cfg.OpenAIAPIKey, cfg.Model, cfg.OpenAIBaseURL)
	case "voyage":
		if cfg.VoyageAPIKey == "" {
			return nil, fmt.Errorf("voyage AI API key is required when provider is 'voyage'")
		}
		return NewVoyageProvider(cfg.VoyageAPIKey, cfg.Model, cfg.VoyageBaseURL)
	case "gemini":
		if cfg.GeminiAPIKey == "" {
			return nil, fmt.Errorf("gemini API key is required when provider is 'gemini'")
		}
		return NewGeminiProvider(cfg.GeminiAPIKey, cfg.Model, cfg.GeminiBaseURL)
	case "ollama":
		return NewOllamaProvider(cfg.OllamaURL, cfg.Model)
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s (supported: voyage, openai, gemini, ollama)", cfg.Provider)
	}
}
