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
	anthropicBaseURL      = "https://api.anthropic.com/v1"
	anthropicDefaultModel = "claude-3-haiku-20240307"
	anthropicAPIVersion   = "2023-06-01"
)

// AnthropicReasoning implements ReasoningProvider using Anthropic's Claude API.
type AnthropicReasoning struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// NewAnthropicReasoning creates a new Anthropic reasoning provider.
func NewAnthropicReasoning(apiKey, model, baseURL string) *AnthropicReasoning {
	if model == "" {
		model = anthropicDefaultModel
	}
	if baseURL == "" {
		baseURL = anthropicBaseURL
	}
	return &AnthropicReasoning{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Classify classifies an anomaly using Anthropic's Claude API.
func (a *AnthropicReasoning) Classify(ctx context.Context, prompt string) (string, error) {
	requestBody := anthropicRequest{
		Model:     a.model,
		MaxTokens: 500,
		System:    classificationSystemPrompt,
		Messages: []anthropicMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := a.doRequestWithRetry(ctx, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(body))
	}

	var response anthropicResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Content) == 0 {
		return "", ErrInvalidResponse
	}

	// Find the text content block
	for _, block := range response.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}

	return "", ErrInvalidResponse
}

// ModelName returns the model name.
func (a *AnthropicReasoning) ModelName() string {
	return a.model
}

// doRequestWithRetry executes an HTTP request with retry on rate limiting.
func (a *AnthropicReasoning) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	return doRequestWithRetry(ctx, a.client, req)
}

// Anthropic API types
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
