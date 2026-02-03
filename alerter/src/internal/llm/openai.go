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
	openaiBaseURL               = "https://api.openai.com/v1"
	openaiDefaultReasoningModel = "gpt-4o-mini"
)

// OpenAIReasoning implements ReasoningProvider using OpenAI's API.
type OpenAIReasoning struct {
	apiKey string
	model  string
	client *http.Client
}

// NewOpenAIReasoning creates a new OpenAI reasoning provider.
func NewOpenAIReasoning(apiKey, model string) *OpenAIReasoning {
	if model == "" {
		model = openaiDefaultReasoningModel
	}
	return &OpenAIReasoning{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Classify classifies an anomaly using OpenAI's chat API.
func (o *OpenAIReasoning) Classify(ctx context.Context, prompt string) (string, error) {
	requestBody := openaiChatRequest{
		Model: o.model,
		Messages: []openaiMessage{
			{
				Role:    "system",
				Content: classificationSystemPrompt,
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: 0.1,
		MaxTokens:   500,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openaiBaseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.doRequestWithRetry(ctx, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai API error (status %d): %s", resp.StatusCode, string(body))
	}

	var response openaiChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", ErrInvalidResponse
	}

	return response.Choices[0].Message.Content, nil
}

// ModelName returns the model name.
func (o *OpenAIReasoning) ModelName() string {
	return o.model
}

// doRequestWithRetry executes an HTTP request with retry on rate limiting.
func (o *OpenAIReasoning) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	return doRequestWithRetry(ctx, o.client, req)
}

// OpenAI API types
type openaiChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiChatResponse struct {
	Choices []struct {
		Message openaiMessage `json:"message"`
	} `json:"choices"`
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
