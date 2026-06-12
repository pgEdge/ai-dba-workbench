/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package llm provides interfaces and implementations for LLM-based
// embedding generation and reasoning for anomaly detection.
package llm

import (
	"context"
	"errors"
	"fmt"

	"github.com/pgEdge/pgedge-go-llm-lib/llm/vec"
	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/pkg/embedding"
)

// EmbeddingDimension is the standard dimension for embeddings (1536 for OpenAI/Voyage)
const EmbeddingDimension = 1536

// Common errors
var (
	ErrAPIKeyMissing   = errors.New("API key is required but not configured")
	ErrInvalidResponse = errors.New("invalid response from LLM provider")
	ErrRateLimited     = errors.New("rate limited by LLM provider")
	ErrContextCanceled = errors.New("context canceled")
)

// EmbeddingProvider generates vector embeddings from text.
type EmbeddingProvider interface {
	// GenerateEmbedding generates a vector embedding for the given text.
	// The returned embedding has EmbeddingDimension (1536) dimensions.
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)

	// ModelName returns the name of the embedding model being used.
	ModelName() string
}

// ReasoningProvider classifies anomalies using LLM reasoning.
type ReasoningProvider interface {
	// Classify analyzes the given prompt and returns a classification response.
	// The response should contain either "alert" or "suppress" along with reasoning.
	Classify(ctx context.Context, prompt string) (string, error)

	// ModelName returns the name of the reasoning model being used.
	ModelName() string
}

// classificationSystemPrompt is the system prompt for anomaly classification.
const classificationSystemPrompt = `You are a database monitoring expert analyzing anomaly candidates.

Your task is to determine whether a detected anomaly is a real issue that requires attention (alert) or a false positive that should be suppressed.

Respond with a JSON object containing:
1. "decision": either "alert" or "suppress"
2. "confidence": a number from 0 to 1 indicating your confidence
3. "reasoning": a brief explanation of your decision

Consider:
- Is the value significantly outside normal operating parameters?
- Could this be expected behavior (e.g., maintenance windows, backups)?
- Are there similar past anomalies that were false positives?
- What is the potential impact if this is a real issue?`

// embeddingAdapter wraps pkg/embedding.Provider to implement the alerter's
// EmbeddingProvider interface. It converts between float64 (pkg/embedding)
// and float32 (alerter) and handles dimension normalization.
type embeddingAdapter struct {
	provider embedding.Provider
}

// GenerateEmbedding generates a vector embedding for the given text.
// It converts the float64 embedding from pkg/embedding to float32 and
// normalizes the dimensions to EmbeddingDimension if needed.
func (a *embeddingAdapter) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	emb64, err := a.provider.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	emb32 := vec.Float64ToFloat32(emb64)
	if len(emb32) != EmbeddingDimension {
		emb32 = vec.Resize(emb32, EmbeddingDimension)
	}
	return vec.Normalize(emb32), nil
}

// ModelName returns the name of the embedding model being used.
func (a *embeddingAdapter) ModelName() string {
	return a.provider.ModelName()
}

// NewEmbeddingProvider creates an embedding provider based on configuration.
// Returns nil and no error if embedding is disabled or provider is not configured.
// This function uses pkg/embedding for the underlying implementation.
func NewEmbeddingProvider(cfg *config.Config) (EmbeddingProvider, error) {
	if cfg == nil {
		return nil, nil
	}

	switch cfg.LLM.EmbeddingProvider {
	case "openai":
		apiKey := cfg.GetOpenAIAPIKey()
		if apiKey == "" && cfg.LLM.OpenAI.BaseURL == "" {
			return nil, fmt.Errorf("openai: %w", ErrAPIKeyMissing)
		}
		provider, err := embedding.NewOpenAIProvider(apiKey, cfg.LLM.OpenAI.EmbeddingModel, cfg.LLM.OpenAI.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("openai: %w", err)
		}
		return &embeddingAdapter{provider: provider}, nil

	case "voyage":
		apiKey := cfg.GetVoyageAPIKey()
		if apiKey == "" {
			return nil, fmt.Errorf("voyage: %w", ErrAPIKeyMissing)
		}
		provider, err := embedding.NewVoyageProvider(apiKey, cfg.LLM.Voyage.EmbeddingModel, cfg.LLM.Voyage.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("voyage: %w", err)
		}
		return &embeddingAdapter{provider: provider}, nil

	case "gemini":
		apiKey := cfg.GetGeminiAPIKey()
		if apiKey == "" {
			return nil, fmt.Errorf("gemini: %w", ErrAPIKeyMissing)
		}
		provider, err := embedding.NewGeminiProvider(apiKey, cfg.LLM.Gemini.EmbeddingModel, cfg.LLM.Gemini.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("gemini: %w", err)
		}
		return &embeddingAdapter{provider: provider}, nil

	case "ollama":
		baseURL := cfg.LLM.Ollama.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		model := cfg.LLM.Ollama.EmbeddingModel
		if model == "" {
			model = "nomic-embed-text"
		}
		provider, err := embedding.NewOllamaProvider(baseURL, model)
		if err != nil {
			return nil, fmt.Errorf("ollama: %w", err)
		}
		return &embeddingAdapter{provider: provider}, nil

	case "", "none", "disabled":
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown embedding provider: %s", cfg.LLM.EmbeddingProvider)
	}
}

// NewReasoningProvider creates a reasoning provider based on configuration.
// Returns nil and no error if reasoning is disabled or provider is not configured.
func NewReasoningProvider(cfg *config.Config) (ReasoningProvider, error) {
	if cfg == nil {
		return nil, nil
	}

	switch cfg.LLM.ReasoningProvider {
	case "openai":
		apiKey := cfg.GetOpenAIAPIKey()
		if apiKey == "" && cfg.LLM.OpenAI.BaseURL == "" {
			return nil, fmt.Errorf("openai: %w", ErrAPIKeyMissing)
		}
		return newLibReasoning("openai", apiKey, cfg.LLM.OpenAI.ReasoningModel, cfg.LLM.OpenAI.BaseURL)

	case "anthropic":
		apiKey := cfg.GetAnthropicAPIKey()
		if apiKey == "" {
			return nil, fmt.Errorf("anthropic: %w", ErrAPIKeyMissing)
		}
		return newLibReasoning("anthropic", apiKey, cfg.LLM.Anthropic.ReasoningModel, cfg.LLM.Anthropic.BaseURL)

	case "gemini":
		apiKey := cfg.GetGeminiAPIKey()
		if apiKey == "" {
			return nil, fmt.Errorf("gemini: %w", ErrAPIKeyMissing)
		}
		return newLibReasoning("gemini", apiKey, cfg.LLM.Gemini.ReasoningModel, cfg.LLM.Gemini.BaseURL)

	case "ollama":
		baseURL := cfg.LLM.Ollama.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		return newLibReasoning("ollama", "", cfg.LLM.Ollama.ReasoningModel, baseURL)

	case "", "none", "disabled":
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown reasoning provider: %s", cfg.LLM.ReasoningProvider)
	}
}
