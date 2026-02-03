/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
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
	voyageBaseURL      = "https://api.voyageai.com/v1"
	voyageDefaultModel = "voyage-3-lite"
)

// VoyageEmbedding implements EmbeddingProvider using Voyage AI's API.
type VoyageEmbedding struct {
	apiKey string
	model  string
	client *http.Client
}

// NewVoyageEmbedding creates a new Voyage AI embedding provider.
func NewVoyageEmbedding(apiKey, model string) *VoyageEmbedding {
	if model == "" {
		model = voyageDefaultModel
	}
	return &VoyageEmbedding{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GenerateEmbedding generates an embedding using Voyage AI's API.
func (v *VoyageEmbedding) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	requestBody := voyageEmbeddingRequest{
		Model: v.model,
		Input: []string{text},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", voyageBaseURL+"/embeddings", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.apiKey)

	resp, err := v.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("voyage API error (status %d): %s", resp.StatusCode, string(body))
	}

	var response voyageEmbeddingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Data) == 0 {
		return nil, ErrInvalidResponse
	}

	// Convert float64 to float32
	embedding := make([]float32, len(response.Data[0].Embedding))
	for i, val := range response.Data[0].Embedding {
		embedding[i] = float32(val)
	}

	// Resize to standard dimension if needed
	if len(embedding) != EmbeddingDimension {
		embedding = resizeEmbedding(embedding, EmbeddingDimension)
	}

	return embedding, nil
}

// ModelName returns the model name.
func (v *VoyageEmbedding) ModelName() string {
	return v.model
}

// doRequestWithRetry executes an HTTP request with retry on rate limiting.
func (v *VoyageEmbedding) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	return doRequestWithRetry(ctx, v.client, req)
}

// Voyage AI API types
type voyageEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type voyageEmbeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}
