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
	"math"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/pkg/embedding"
)

// EmbeddingDimension is the standard dimension for embeddings (1536 for OpenAI/Voyage)
const EmbeddingDimension = 1536

// Common errors
var (
	ErrProviderNotConfigured = errors.New("LLM provider not configured")
	ErrAPIKeyMissing         = errors.New("API key is required but not configured")
	ErrInvalidResponse       = errors.New("invalid response from LLM provider")
	ErrRateLimited           = errors.New("rate limited by LLM provider")
	ErrContextCanceled       = errors.New("context canceled")
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
	// Call the underlying provider
	emb64, err := a.provider.Embed(ctx, text)
	if err != nil {
		return nil, err
	}

	// Convert float64 to float32
	emb32 := make([]float32, len(emb64))
	for i, v := range emb64 {
		emb32[i] = float32(v)
	}

	// Resize to standard dimension if needed
	if len(emb32) != EmbeddingDimension {
		emb32 = resizeEmbedding(emb32, EmbeddingDimension)
	}

	// Normalize the embedding
	emb32 = normalizeEmbedding(emb32)

	return emb32, nil
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
		if apiKey == "" {
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
		if apiKey == "" {
			return nil, fmt.Errorf("openai: %w", ErrAPIKeyMissing)
		}
		return NewOpenAIReasoning(apiKey, cfg.LLM.OpenAI.ReasoningModel, cfg.LLM.OpenAI.BaseURL), nil

	case "anthropic":
		apiKey := cfg.GetAnthropicAPIKey()
		if apiKey == "" {
			return nil, fmt.Errorf("anthropic: %w", ErrAPIKeyMissing)
		}
		return NewAnthropicReasoning(apiKey, cfg.LLM.Anthropic.ReasoningModel, cfg.LLM.Anthropic.BaseURL), nil

	case "ollama":
		baseURL := cfg.LLM.Ollama.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		model := cfg.LLM.Ollama.ReasoningModel
		if model == "" {
			model = "llama3.2"
		}
		return NewOllamaReasoning(baseURL, model), nil

	case "", "none", "disabled":
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown reasoning provider: %s", cfg.LLM.ReasoningProvider)
	}
}

// resizeEmbedding resizes an embedding to the target dimension.
// If the source embedding is larger, it truncates.
// If smaller, it pads with zeros.
func resizeEmbedding(embedding []float32, targetDim int) []float32 {
	if len(embedding) == targetDim {
		return embedding
	}

	result := make([]float32, targetDim)
	if len(embedding) > targetDim {
		// Truncate
		copy(result, embedding[:targetDim])
	} else {
		// Pad with zeros
		copy(result, embedding)
	}
	return result
}

// normalizeEmbedding normalizes a vector to unit length (L2 normalization).
func normalizeEmbedding(embedding []float32) []float32 {
	var sumSquares float64
	for _, v := range embedding {
		sumSquares += float64(v) * float64(v)
	}

	if sumSquares == 0 {
		return embedding
	}

	magnitude := float32(1.0 / math.Sqrt(sumSquares))
	result := make([]float32, len(embedding))
	for i, v := range embedding {
		result[i] = v * magnitude
	}
	return result
}
