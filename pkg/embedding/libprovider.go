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
	"strings"

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

// supportedEmbeddingModels is a fail-fast allow-list of known-good embedding
// models per provider. Construction rejects any model not listed here, which
// guards against silently producing vectors of an unexpected dimension that
// would be incompatible with the stored knowledge-base vectors. Providers
// absent from this map (for example Ollama, which discovers models at
// runtime) are not validated and accept any model name.
var supportedEmbeddingModels = map[string][]string{
	"openai": {"text-embedding-3-large", "text-embedding-3-small", "text-embedding-ada-002"},
	"voyage": {"voyage-3", "voyage-3-lite", "voyage-2", "voyage-2-lite"},
	"gemini": {"gemini-embedding-001", "gemini-embedding-2", "gemini-embedding-2-preview"},
}

// providerDisplayNames maps an internal provider key to the human-readable
// name used verbatim in the unsupported-model error messages.
var providerDisplayNames = map[string]string{
	"openai": "OpenAI",
	"voyage": "Voyage AI",
	"gemini": "Gemini",
}

// validateEmbeddingModel enforces the per-provider allow-list. It returns nil
// for providers that are not in the allow-list (they accept any model) and
// for models that appear in the provider's supported set.
func validateEmbeddingModel(provider, model string) error {
	models, guarded := supportedEmbeddingModels[provider]
	if !guarded {
		return nil
	}
	for _, m := range models {
		if m == model {
			return nil
		}
	}
	return fmt.Errorf("unsupported %s model: %s (supported: %s)",
		providerDisplayNames[provider], model, strings.Join(models, ", "))
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
	if err := validateEmbeddingModel(provider, model); err != nil {
		return nil, err
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
