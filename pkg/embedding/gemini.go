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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// GeminiHTTPTimeout is the HTTP client timeout for Gemini API requests
	GeminiHTTPTimeout = 30 * time.Second
)

// GeminiProvider implements embedding generation using Google's Gemini API
type GeminiProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// geminiEmbeddingRequest represents a request to Gemini's embedContent API
type geminiEmbeddingRequest struct {
	Model   string             `json:"model"`
	Content geminiContentBlock `json:"content"`
}

// geminiContentBlock represents a content block in a Gemini request
type geminiContentBlock struct {
	Parts []geminiPart `json:"parts"`
}

// geminiPart represents a part within a Gemini content block
type geminiPart struct {
	Text string `json:"text"`
}

// geminiEmbeddingResponse represents a response from Gemini's embedContent API
type geminiEmbeddingResponse struct {
	Embedding struct {
		Values []float64 `json:"values"`
	} `json:"embedding"`
}

// Model dimensions for Gemini embedding models
var geminiModelDimensions = map[string]int{
	"text-embedding-004": 768,
	"embedding-001":      768,
}

// NewGeminiProvider creates a new Gemini embedding provider.
// If baseURL is empty, the default Google Generative Language API endpoint is used.
func NewGeminiProvider(apiKey, model, baseURL string) (*GeminiProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Gemini API key cannot be empty")
	}

	// Default to text-embedding-004 if no model specified
	if model == "" {
		model = "text-embedding-004"
	}

	// Validate model is supported
	if _, ok := geminiModelDimensions[model]; !ok {
		return nil, fmt.Errorf("unsupported Gemini model: %s (supported: text-embedding-004, embedding-001)", model)
	}

	// Default base URL if not provided
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}

	// Mask the API key for logging (show only first/last few characters)
	maskedKey := "(redacted)"
	if len(apiKey) > 8 {
		maskedKey = apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
	}

	LogProviderInit("gemini", model, map[string]string{
		"api_key":  maskedKey,
		"base_url": baseURL,
	})

	return &GeminiProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: GeminiHTTPTimeout,
		},
	}, nil
}

// Embed generates an embedding vector for the given text
func (p *GeminiProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	startTime := time.Now()
	textLen := len(text)

	if text == "" {
		return nil, fmt.Errorf("text cannot be empty")
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:embedContent?key=%s", p.baseURL, p.model, p.apiKey)
	// Log with masked URL to avoid leaking API key
	maskedURL := fmt.Sprintf("%s/v1beta/models/%s:embedContent?key=***", p.baseURL, p.model)
	LogAPICallDetails("gemini", p.model, maskedURL, textLen)
	LogRequestTrace("gemini", p.model, text)

	reqBody := geminiEmbeddingRequest{
		Model: "models/" + p.model,
		Content: geminiContentBlock{
			Parts: []geminiPart{
				{Text: text},
			},
		},
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		LogConnectionError("gemini", maskedURL, err)
		duration := time.Since(startTime)
		LogAPICall("gemini", p.model, textLen, duration, 0, err)
		return nil, fmt.Errorf("failed to make API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			duration := time.Since(startTime)
			err := fmt.Errorf("API request failed with status %d (error reading response body: %w)", resp.StatusCode, readErr)
			LogAPICall("gemini", p.model, textLen, duration, 0, err)
			return nil, err
		}

		// Check if this is a rate limit error
		if resp.StatusCode == 429 {
			LogRateLimitError("gemini", p.model, resp.StatusCode, string(body))
		}

		duration := time.Since(startTime)
		err := fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
		LogAPICall("gemini", p.model, textLen, duration, 0, err)
		return nil, err
	}

	var embResp geminiEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		duration := time.Since(startTime)
		LogAPICall("gemini", p.model, textLen, duration, 0, err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(embResp.Embedding.Values) == 0 {
		duration := time.Since(startTime)
		err := fmt.Errorf("received empty embedding from API")
		LogAPICall("gemini", p.model, textLen, duration, 0, err)
		return nil, err
	}

	duration := time.Since(startTime)
	dimensions := len(embResp.Embedding.Values)
	LogResponseTrace("gemini", p.model, resp.StatusCode, dimensions)
	LogAPICall("gemini", p.model, textLen, duration, dimensions, nil)

	return embResp.Embedding.Values, nil
}

// Dimensions returns the number of dimensions for this model
func (p *GeminiProvider) Dimensions() int {
	return geminiModelDimensions[p.model]
}

// ModelName returns the model name
func (p *GeminiProvider) ModelName() string {
	return p.model
}

// ProviderName returns "gemini"
func (p *GeminiProvider) ProviderName() string {
	return "gemini"
}
