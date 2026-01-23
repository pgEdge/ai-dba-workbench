/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	ollamaDefaultBaseURL        = "http://localhost:11434"
	ollamaDefaultEmbeddingModel = "nomic-embed-text"
	ollamaDefaultReasoningModel = "llama3.2"
)

// OllamaEmbedding implements EmbeddingProvider using Ollama's local API.
type OllamaEmbedding struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllamaEmbedding creates a new Ollama embedding provider.
func NewOllamaEmbedding(baseURL, model string) *OllamaEmbedding {
	if baseURL == "" {
		baseURL = ollamaDefaultBaseURL
	}
	if model == "" {
		model = ollamaDefaultEmbeddingModel
	}
	return &OllamaEmbedding{
		baseURL: baseURL,
		model:   model,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// GenerateEmbedding generates an embedding using Ollama's API.
func (o *OllamaEmbedding) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	requestBody := ollamaEmbeddingRequest{
		Model:  o.model,
		Prompt: text,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/api/embeddings", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrContextCanceled
		}
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama API error (status %d): %s", resp.StatusCode, string(body))
	}

	var response ollamaEmbeddingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Embedding) == 0 {
		return nil, ErrInvalidResponse
	}

	// Convert float64 to float32
	embedding := make([]float32, len(response.Embedding))
	for i, val := range response.Embedding {
		embedding[i] = float32(val)
	}

	// Resize to standard dimension (Ollama models may have different dimensions)
	if len(embedding) != EmbeddingDimension {
		embedding = resizeEmbedding(embedding, EmbeddingDimension)
	}

	// Normalize the embedding
	embedding = normalizeEmbedding(embedding)

	return embedding, nil
}

// ModelName returns the model name.
func (o *OllamaEmbedding) ModelName() string {
	return o.model
}

// OllamaReasoning implements ReasoningProvider using Ollama's local API.
type OllamaReasoning struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllamaReasoning creates a new Ollama reasoning provider.
func NewOllamaReasoning(baseURL, model string) *OllamaReasoning {
	if baseURL == "" {
		baseURL = ollamaDefaultBaseURL
	}
	if model == "" {
		model = ollamaDefaultReasoningModel
	}
	return &OllamaReasoning{
		baseURL: baseURL,
		model:   model,
		client: &http.Client{
			Timeout: 120 * time.Second, // Longer timeout for reasoning
		},
	}
}

// Classify classifies an anomaly using Ollama's generate API.
func (o *OllamaReasoning) Classify(ctx context.Context, prompt string) (string, error) {
	// Build the full prompt with system instructions
	fullPrompt := classificationSystemPrompt + "\n\n" + prompt

	requestBody := ollamaGenerateRequest{
		Model:  o.model,
		Prompt: fullPrompt,
		Stream: false,
		Options: map[string]interface{}{
			"temperature": 0.1,
			"num_predict": 500,
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/api/generate", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ErrContextCanceled
		}
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama API error (status %d): %s", resp.StatusCode, string(body))
	}

	var response ollamaGenerateResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return response.Response, nil
}

// ModelName returns the model name.
func (o *OllamaReasoning) ModelName() string {
	return o.model
}

// Ollama API types
type ollamaEmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbeddingResponse struct {
	Embedding []float64 `json:"embedding"`
}

type ollamaGenerateRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Stream  bool                   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}
