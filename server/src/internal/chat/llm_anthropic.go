/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pgedge/ai-workbench/pkg/embedding"
)

// -------------------------------------------------------------------------
// Anthropic Client
// -------------------------------------------------------------------------

// anthropicClient implements LLMClient for Anthropic Claude
type anthropicClient struct {
	apiKey      string
	model       string
	maxTokens   int
	temperature float64
	debug       bool
	baseURL     string
	client      *http.Client
}

// NewAnthropicClient creates a new Anthropic client
func NewAnthropicClient(apiKey, model string, maxTokens int, temperature float64, debug bool, baseURL string) LLMClient {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &anthropicClient{
		apiKey:      apiKey,
		model:       model,
		maxTokens:   maxTokens,
		temperature: temperature,
		debug:       debug,
		baseURL:     baseURL,
		client:      sharedHTTPClient,
	}
}

type anthropicRequest struct {
	Model       string                   `json:"model"`
	MaxTokens   int                      `json:"max_tokens"`
	Messages    []Message                `json:"messages"`
	Tools       []map[string]interface{} `json:"tools,omitempty"`
	Temperature float64                  `json:"temperature,omitempty"`
	System      []map[string]interface{} `json:"system,omitempty"` // Support for system messages with caching
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

type anthropicResponse struct {
	ID         string                   `json:"id"`
	Type       string                   `json:"type"`
	Role       string                   `json:"role"`
	Content    []map[string]interface{} `json:"content"`
	StopReason string                   `json:"stop_reason"`
	Usage      anthropicUsage           `json:"usage"`
}

type anthropicErrorResponse struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// extractAnthropicError extracts an error message from Anthropic's JSON error response.
func extractAnthropicError(body []byte) string {
	var errResp anthropicErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		return errResp.Error.Message
	}
	return ""
}

func (c *anthropicClient) Chat(ctx context.Context, messages []Message, tools interface{}) (LLMResponse, error) {
	startTime := time.Now()
	operation := "chat"
	url := c.baseURL + "/messages"

	embedding.LogLLMCallDetails("anthropic", c.model, operation, url, len(messages))

	// Convert interface{} tools to []mcp.Tool
	mcpTools, err := convertToMCPTools(tools)
	if err != nil {
		return LLMResponse{}, err
	}

	// Convert MCP tools to Anthropic format with caching
	anthropicTools := make([]map[string]interface{}, 0, len(mcpTools))
	for i, tool := range mcpTools {
		toolDef := map[string]interface{}{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": tool.InputSchema,
		}

		// Add cache_control to the last tool definition to cache all tools
		// This caches the entire tools array (must be on the last item)
		if i == len(mcpTools)-1 {
			toolDef["cache_control"] = map[string]interface{}{
				"type": "ephemeral",
			}
		}

		anthropicTools = append(anthropicTools, toolDef)
	}

	// Create system message for better UX
	systemMessage := []map[string]interface{}{
		{
			"type": "text",
			"text": systemPrompt,
		},
	}

	req := anthropicRequest{
		Model:       c.model,
		MaxTokens:   c.maxTokens,
		Messages:    messages,
		Tools:       anthropicTools,
		Temperature: c.temperature,
		System:      systemMessage,
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	embedding.LogLLMRequestTrace("anthropic", c.model, operation, string(reqData))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqData))
	if err != nil {
		return LLMResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		embedding.LogConnectionError("anthropic", url, err)
		duration := time.Since(startTime)
		embedding.LogLLMCall("anthropic", c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			duration := time.Since(startTime)
			readErr := fmt.Errorf("API error %d (failed to read body: %w)", resp.StatusCode, err)
			embedding.LogLLMCall("anthropic", c.model, operation, 0, 0, duration, readErr)
			return LLMResponse{}, readErr
		}

		// Check if this is a rate limit error
		if resp.StatusCode == 429 {
			embedding.LogRateLimitError("anthropic", c.model, resp.StatusCode, string(body))
		}

		// Extract user-friendly error message from Anthropic's error response
		userFriendlyMsg := extractErrorMessage(resp.StatusCode, body, "API error", extractAnthropicError)

		duration := time.Since(startTime)
		apiErr := fmt.Errorf("%s", userFriendlyMsg)
		embedding.LogLLMCall("anthropic", c.model, operation, 0, 0, duration, apiErr)
		return LLMResponse{}, apiErr
	}

	var anthropicResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		duration := time.Since(startTime)
		embedding.LogLLMCall("anthropic", c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert response content to typed structs
	content := make([]interface{}, 0, len(anthropicResp.Content))
	for _, item := range anthropicResp.Content {
		itemType, ok := item["type"].(string)
		if !ok {
			continue
		}
		switch itemType {
		case "text":
			text, ok := item["text"].(string)
			if !ok {
				continue
			}
			content = append(content, TextContent{
				Type: "text",
				Text: text,
			})
		case "tool_use":
			id, ok := item["id"].(string)
			if !ok {
				continue
			}
			name, ok := item["name"].(string)
			if !ok {
				continue
			}
			input, ok := item["input"].(map[string]interface{})
			if !ok {
				input = make(map[string]interface{})
			}
			content = append(content, ToolUse{
				Type:  "tool_use",
				ID:    id,
				Name:  name,
				Input: input,
			})
		}
	}

	duration := time.Since(startTime)
	embedding.LogLLMResponseTrace("anthropic", c.model, operation, resp.StatusCode, anthropicResp.StopReason)
	embedding.LogLLMCall("anthropic", c.model, operation, anthropicResp.Usage.InputTokens, anthropicResp.Usage.OutputTokens, duration, nil)

	// Build token usage for debug
	var tokenUsage *TokenUsage
	if c.debug {
		totalInput := anthropicResp.Usage.InputTokens + anthropicResp.Usage.CacheReadInputTokens
		savePercent := 0.0
		if totalInput > 0 {
			savePercent = float64(anthropicResp.Usage.CacheReadInputTokens) / float64(totalInput) * 100
		}

		tokenUsage = &TokenUsage{
			Provider:               "anthropic",
			PromptTokens:           anthropicResp.Usage.InputTokens,
			CompletionTokens:       anthropicResp.Usage.OutputTokens,
			TotalTokens:            anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
			CacheCreationTokens:    anthropicResp.Usage.CacheCreationInputTokens,
			CacheReadTokens:        anthropicResp.Usage.CacheReadInputTokens,
			CacheSavingsPercentage: savePercent,
		}

		logTokenUsage("Anthropic",
			anthropicResp.Usage.InputTokens,
			anthropicResp.Usage.OutputTokens,
			anthropicResp.Usage.InputTokens+anthropicResp.Usage.OutputTokens,
			anthropicResp.Usage.CacheCreationInputTokens,
			anthropicResp.Usage.CacheReadInputTokens,
			savePercent)
	}

	return LLMResponse{
		Content:    content,
		StopReason: anthropicResp.StopReason,
		TokenUsage: tokenUsage,
	}, nil
}

// ListModels returns available Anthropic Claude models from the API
func (c *anthropicClient) ListModels(ctx context.Context) ([]string, error) {
	url := c.baseURL + "/models"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck // Error response body read is best effort
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	// Parse response: {"data": [{"id": "claude-3-opus-20240229", "type": "model", ...}, ...]}
	var response struct {
		Data []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]string, 0, len(response.Data))
	for _, model := range response.Data {
		// Only include models (not other types if any)
		if model.Type == "model" {
			models = append(models, model.ID)
		}
	}

	return models, nil
}
