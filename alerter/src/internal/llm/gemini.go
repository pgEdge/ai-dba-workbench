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
	geminiBaseURL      = "https://generativelanguage.googleapis.com"
	geminiDefaultModel = "gemini-2.0-flash"
)

// GeminiReasoning implements ReasoningProvider using Google's Gemini API.
type GeminiReasoning struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// NewGeminiReasoning creates a new Gemini reasoning provider.
func NewGeminiReasoning(apiKey, model, baseURL string) *GeminiReasoning {
	if model == "" {
		model = geminiDefaultModel
	}
	if baseURL == "" {
		baseURL = geminiBaseURL
	}
	return &GeminiReasoning{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Classify classifies an anomaly using Google's Gemini API.
func (g *GeminiReasoning) Classify(ctx context.Context, prompt string) (string, error) {
	combinedPrompt := classificationSystemPrompt + "\n\n" + prompt

	requestBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: combinedPrompt},
				},
			},
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s",
		g.baseURL, g.model, g.apiKey)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := g.doRequestWithRetry(ctx, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, string(body))
	}

	var response geminiResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Candidates) == 0 {
		return "", ErrInvalidResponse
	}

	// Find the text content from the first candidate
	candidate := response.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		return "", ErrInvalidResponse
	}

	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			return part.Text, nil
		}
	}

	return "", ErrInvalidResponse
}

// ModelName returns the model name.
func (g *GeminiReasoning) ModelName() string {
	return g.model
}

// doRequestWithRetry executes an HTTP request with retry on rate limiting.
func (g *GeminiReasoning) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	return doRequestWithRetry(ctx, g.client, req)
}

// Gemini API types
type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}
