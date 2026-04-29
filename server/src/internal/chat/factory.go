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

import "fmt"

// ClientConfig holds the configuration needed to create an LLM client.
// Callers populate fields relevant to their chosen provider; the factory
// validates that required credentials are present before construction.
type ClientConfig struct {
	Provider         string
	AnthropicAPIKey  string
	AnthropicBaseURL string
	OpenAIAPIKey     string
	OpenAIBaseURL    string
	GeminiAPIKey     string
	GeminiBaseURL    string
	OllamaURL        string
	Model            string
	MaxTokens        int
	Temperature      float64
	Debug            bool
	UseCompactDescs  bool
	Headers          map[string]string
}

// LLMConfig carries the per-provider credentials and default tuning
// parameters that callers pull from server configuration. It is a plain
// data struct so the chat package does not have to depend on the config
// package; callers populate the fields they care about and pass the
// value to NewClientFromLLMConfig.
type LLMConfig struct {
	Provider               string
	Model                  string
	AnthropicAPIKey        string
	AnthropicBaseURL       string
	OpenAIAPIKey           string
	OpenAIBaseURL          string
	GeminiAPIKey           string
	GeminiBaseURL          string
	OllamaURL              string
	MaxTokens              int
	Temperature            float64
	UseCompactDescriptions bool
}

// LLMOptions overrides selected fields from an LLMConfig for a single
// client construction. Zero values fall back to the LLMConfig values;
// non-zero values win. Headers are passed through unchanged because
// header maps are looked up per-provider by the caller.
type LLMOptions struct {
	// Model overrides LLMConfig.Model when non-empty.
	Model string
	// Provider overrides LLMConfig.Provider when non-empty.
	Provider string
	// MaxTokens overrides LLMConfig.MaxTokens when non-zero.
	MaxTokens int
	// Temperature overrides LLMConfig.Temperature when non-zero.
	Temperature float64
	// Debug enables provider debug logging. Always honored as supplied.
	Debug bool
	// Headers passes provider-specific custom headers through to the
	// underlying transport. Always honored as supplied.
	Headers map[string]string
}

// NewClientFromLLMConfig builds an LLMClient by merging LLMConfig with
// per-call LLMOptions and delegating to NewClientFromConfig. It is the
// shared factory used by the LLM proxy and the overview generator so
// that the 14-field translation from server config to ClientConfig is
// expressed in exactly one place.
func NewClientFromLLMConfig(cfg LLMConfig, opts LLMOptions) (LLMClient, error) {
	provider := opts.Provider
	if provider == "" {
		provider = cfg.Provider
	}
	model := opts.Model
	if model == "" {
		model = cfg.Model
	}
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = cfg.MaxTokens
	}
	temperature := opts.Temperature
	if temperature == 0 {
		temperature = cfg.Temperature
	}
	return NewClientFromConfig(ClientConfig{
		Provider:         provider,
		AnthropicAPIKey:  cfg.AnthropicAPIKey,
		AnthropicBaseURL: cfg.AnthropicBaseURL,
		OpenAIAPIKey:     cfg.OpenAIAPIKey,
		OpenAIBaseURL:    cfg.OpenAIBaseURL,
		GeminiAPIKey:     cfg.GeminiAPIKey,
		GeminiBaseURL:    cfg.GeminiBaseURL,
		OllamaURL:        cfg.OllamaURL,
		Model:            model,
		MaxTokens:        maxTokens,
		Temperature:      temperature,
		Debug:            opts.Debug,
		UseCompactDescs:  cfg.UseCompactDescriptions,
		Headers:          opts.Headers,
	})
}

// NewClientFromConfig creates an LLMClient for the specified provider using
// the supplied configuration. It validates that required credentials are
// present and returns an error if the provider is unknown or misconfigured.
//
// Validation rules by provider:
//   - anthropic: requires AnthropicAPIKey != ""
//   - openai: requires OpenAIAPIKey != "" OR OpenAIBaseURL != ""
//   - gemini: requires GeminiAPIKey != ""
//   - ollama: requires OllamaURL != ""
func NewClientFromConfig(cfg ClientConfig) (LLMClient, error) {
	switch cfg.Provider {
	case "anthropic":
		if cfg.AnthropicAPIKey == "" {
			return nil, fmt.Errorf("anthropic provider requires AnthropicAPIKey")
		}
		return NewAnthropicClient(
			cfg.AnthropicAPIKey,
			cfg.Model,
			cfg.MaxTokens,
			cfg.Temperature,
			cfg.Debug,
			cfg.AnthropicBaseURL,
			cfg.UseCompactDescs,
			cfg.Headers,
		), nil

	case "openai":
		if cfg.OpenAIAPIKey == "" && cfg.OpenAIBaseURL == "" {
			return nil, fmt.Errorf("openai provider requires OpenAIAPIKey or OpenAIBaseURL")
		}
		return NewOpenAIClient(
			cfg.OpenAIAPIKey,
			cfg.Model,
			cfg.MaxTokens,
			cfg.Temperature,
			cfg.Debug,
			cfg.OpenAIBaseURL,
			cfg.UseCompactDescs,
			cfg.Headers,
		), nil

	case "gemini":
		if cfg.GeminiAPIKey == "" {
			return nil, fmt.Errorf("gemini provider requires GeminiAPIKey")
		}
		return NewGeminiClient(
			cfg.GeminiAPIKey,
			cfg.Model,
			cfg.MaxTokens,
			cfg.Temperature,
			cfg.Debug,
			cfg.GeminiBaseURL,
			cfg.UseCompactDescs,
			cfg.Headers,
		), nil

	case "ollama":
		if cfg.OllamaURL == "" {
			return nil, fmt.Errorf("ollama provider requires OllamaURL")
		}
		return NewOllamaClient(
			cfg.OllamaURL,
			cfg.Model,
			cfg.Debug,
			cfg.UseCompactDescs,
			cfg.Headers,
		), nil

	case "":
		return nil, fmt.Errorf("provider is required")

	default:
		return nil, fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
}
