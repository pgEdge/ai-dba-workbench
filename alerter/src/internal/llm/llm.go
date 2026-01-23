/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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

	"github.com/pgedge/ai-workbench/alerter/internal/config"
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

// NewEmbeddingProvider creates an embedding provider based on configuration.
// Returns nil and no error if embedding is disabled or provider is not configured.
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
		return NewOpenAIEmbedding(apiKey, cfg.LLM.OpenAI.EmbeddingModel), nil

	case "voyage":
		apiKey := cfg.GetVoyageAPIKey()
		if apiKey == "" {
			return nil, fmt.Errorf("voyage: %w", ErrAPIKeyMissing)
		}
		return NewVoyageEmbedding(apiKey, cfg.LLM.Voyage.EmbeddingModel), nil

	case "ollama":
		baseURL := cfg.LLM.Ollama.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		model := cfg.LLM.Ollama.EmbeddingModel
		if model == "" {
			model = "nomic-embed-text"
		}
		return NewOllamaEmbedding(baseURL, model), nil

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
		return NewOpenAIReasoning(apiKey, cfg.LLM.OpenAI.ReasoningModel), nil

	case "anthropic":
		apiKey := cfg.GetAnthropicAPIKey()
		if apiKey == "" {
			return nil, fmt.Errorf("anthropic: %w", ErrAPIKeyMissing)
		}
		return NewAnthropicReasoning(apiKey, cfg.LLM.Anthropic.ReasoningModel), nil

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

	magnitude := float32(1.0 / sqrt(sumSquares))
	result := make([]float32, len(embedding))
	for i, v := range embedding {
		result[i] = v * magnitude
	}
	return result
}

// sqrt calculates the square root using Newton-Raphson method
func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}
