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
)

// Provider generates embedding vectors via an underlying LLM provider.
type Provider interface {
	// Embed generates an embedding vector for the given text.
	Embed(ctx context.Context, text string) ([]float64, error)
	// ModelName returns the model in use (for provenance).
	ModelName() string
	// ProviderName returns the provider name (e.g. "voyage", "openai").
	ProviderName() string
}

// Config holds configuration for embedding providers
type Config struct {
	Provider string // "voyage", "ollama", "openai", or "gemini"
	Model    string // Model name (provider-specific)

	// Voyage AI-specific
	VoyageAPIKey  string
	VoyageBaseURL string // Base URL for Voyage AI API (default: https://api.voyageai.com/v1)

	// OpenAI-specific
	OpenAIAPIKey  string
	OpenAIBaseURL string // Base URL for OpenAI API (default: https://api.openai.com/v1)

	// Gemini-specific
	GeminiAPIKey  string
	GeminiBaseURL string // Base URL for Gemini API (default: https://generativelanguage.googleapis.com)

	// Ollama-specific
	OllamaURL string
}
