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
