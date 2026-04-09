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
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/pkg/embedding"
)

// -------------------------------------------------------------------------
// OpenAI Client
// -------------------------------------------------------------------------

// openaiClient implements LLMClient for OpenAI GPT models
type openaiClient struct {
	apiKey                 string
	model                  string
	maxTokens              int
	temperature            float64
	debug                  bool
	baseURL                string
	useCompactDescriptions bool
	customHeaders          map[string]string
	client                 *http.Client
}

// NewOpenAIClient creates a new OpenAI client
func NewOpenAIClient(apiKey, model string, maxTokens int, temperature float64, debug bool, baseURL string, useCompactDescriptions bool, customHeaders map[string]string) LLMClient {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &openaiClient{
		apiKey:                 apiKey,
		model:                  model,
		maxTokens:              maxTokens,
		temperature:            temperature,
		debug:                  debug,
		baseURL:                baseURL,
		useCompactDescriptions: useCompactDescriptions,
		customHeaders:          customHeaders,
		client:                 sharedHTTPClient,
	}
}

type openaiMessage struct {
	Role       string `json:"role"`
	Content    any    `json:"content,omitempty"`
	ToolCalls  any    `json:"tool_calls,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type openaiRequest struct {
	Model               string          `json:"model"`
	Messages            []openaiMessage `json:"messages"`
	Tools               any             `json:"tools,omitempty"`
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Temperature         float64         `json:"temperature,omitempty"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openaiChoice struct {
	Index        int           `json:"index"`
	Message      openaiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openaiResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openaiChoice `json:"choices"`
	Usage   openaiUsage    `json:"usage"`
}

type openaiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// extractOpenAIError extracts an error message from OpenAI's JSON error response.
func extractOpenAIError(body []byte) string {
	var errResp openaiErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		return errResp.Error.Message
	}
	return ""
}

func (c *openaiClient) Chat(ctx context.Context, messages []Message, tools any, customSystemPrompt string) (LLMResponse, error) {
	startTime := time.Now()
	operation := "chat"
	url := c.baseURL + "/chat/completions"

	embedding.LogLLMCallDetails("openai", c.model, operation, url, len(messages))

	// Convert any tools to []mcp.Tool
	mcpTools, err := convertToMCPTools(tools)
	if err != nil {
		return LLMResponse{}, err
	}

	// Convert MCP tools to OpenAI format
	var openaiTools []map[string]any
	if len(mcpTools) > 0 {
		for _, tool := range mcpTools {
			desc := tool.Description
			if c.useCompactDescriptions && tool.CompactDescription != "" {
				desc = tool.CompactDescription
			}
			openaiTools = append(openaiTools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool.Name,
					"description": desc,
					"parameters":  tool.InputSchema,
				},
			})
		}
	}

	// Convert messages to OpenAI format
	// Start with system message; use custom prompt if provided, otherwise default
	activePrompt := SystemPrompt
	if customSystemPrompt != "" {
		activePrompt = customSystemPrompt
	}
	openaiMessages := make([]openaiMessage, 0, len(messages)+1)
	openaiMessages = append(openaiMessages, openaiMessage{
		Role:    "system",
		Content: activePrompt,
	})

	for _, msg := range messages {
		openaiMsg := openaiMessage{
			Role: msg.Role,
		}

		// Handle different content types
		switch content := msg.Content.(type) {
		case string:
			openaiMsg.Content = content
		case []ToolResult:
			// Handle []ToolResult directly
			for _, v := range content {
				contentStr := extractTextFromContent(v.Content)
				if contentStr == "" {
					contentStr = "{}"
				}
				openaiMessages = append(openaiMessages, openaiMessage{
					Role:       "tool",
					Content:    contentStr,
					ToolCallID: v.ToolUseID,
				})
			}
			// Don't add the parent message
			continue
		case []any:
			// Handle complex content (text, tool use, and tool results)
			var toolCalls []map[string]any
			for _, item := range content {
				// Handle typed structs (when messages are passed directly)
				switch v := item.(type) {
				case TextContent:
					openaiMsg.Content = v.Text
				case ToolUse:
					// Convert ToolUse to OpenAI tool_calls format
					argsJSON, err := json.Marshal(v.Input)
					if err != nil {
						argsJSON = []byte("{}")
					}
					toolCalls = append(toolCalls, map[string]any{
						"id":   v.ID,
						"type": "function",
						"function": map[string]any{
							"name":      v.Name,
							"arguments": string(argsJSON),
						},
					})
				case ToolResult:
					// ToolResult - send as separate tool message
					// Extract text from result content
					contentStr := extractTextFromContent(v.Content)
					if contentStr == "" {
						contentStr = "{}"
					}

					openaiMessages = append(openaiMessages, openaiMessage{
						Role:       "tool",
						Content:    contentStr,
						ToolCallID: v.ToolUseID,
					})
				default:
					// Handle map[string]any (when items are unmarshaled from JSON)
					itemMap, ok := item.(map[string]any)
					if !ok {
						continue
					}

					itemType, ok := itemMap["type"].(string)
					if !ok {
						continue
					}
					switch itemType {
					case "text":
						// TextContent
						if text, ok := itemMap["text"].(string); ok {
							openaiMsg.Content = text
						}
					case "tool_use":
						// ToolUse - convert to OpenAI tool_calls format
						id, ok1 := itemMap["id"].(string)
						name, ok2 := itemMap["name"].(string)
						input, ok3 := itemMap["input"].(map[string]any)
						if !ok1 || !ok2 || !ok3 {
							continue
						}

						argsJSON, err := json.Marshal(input)
						if err != nil {
							argsJSON = []byte("{}")
						}
						toolCalls = append(toolCalls, map[string]any{
							"id":   id,
							"type": "function",
							"function": map[string]any{
								"name":      name,
								"arguments": string(argsJSON),
							},
						})
					case "tool_result":
						// ToolResult - send as separate tool message
						toolUseID, ok := itemMap["tool_use_id"].(string)
						if !ok {
							continue
						}
						resultContent := itemMap["content"]

						// Extract text from result content
						contentStr := extractTextFromContent(resultContent)
						if contentStr == "" {
							contentStr = "{}"
						}

						openaiMessages = append(openaiMessages, openaiMessage{
							Role:       "tool",
							Content:    contentStr,
							ToolCallID: toolUseID,
						})
					}
				}
			}
			// If we found tool calls, set them on the message
			if len(toolCalls) > 0 {
				openaiMsg.ToolCalls = toolCalls
			}
		}

		// Only add the message if it has content or tool calls
		// Skip empty assistant messages (shouldn't happen, but be safe)
		if openaiMsg.Content != nil || openaiMsg.ToolCalls != nil {
			openaiMessages = append(openaiMessages, openaiMsg)
		}
	}

	// Build request
	reqData := openaiRequest{
		Model:    c.model,
		Messages: openaiMessages,
	}

	// Use max_completion_tokens for newer models (gpt-5, o1-*, etc.)
	// Use max_tokens for older models (gpt-4, gpt-3.5, etc.)
	// GPT-5 and o-series models don't support custom temperature (only default of 1)
	isNewModel := strings.HasPrefix(c.model, "gpt-5") || strings.HasPrefix(c.model, "o1-") || strings.HasPrefix(c.model, "o3-")

	if isNewModel {
		reqData.MaxCompletionTokens = c.maxTokens
		// GPT-5 only supports temperature=1 (default), so don't set it
	} else {
		reqData.MaxTokens = c.maxTokens
		reqData.Temperature = c.temperature
	}

	if len(openaiTools) > 0 {
		reqData.Tools = openaiTools
	}

	reqJSON, err := json.Marshal(reqData)
	if err != nil {
		duration := time.Since(startTime)
		embedding.LogLLMCall("openai", c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	embedding.LogLLMRequestTrace("openai", c.model, operation, string(reqJSON))

	// Make request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqJSON))
	if err != nil {
		duration := time.Since(startTime)
		embedding.LogLLMCall("openai", c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Apply custom headers
	for k, v := range c.customHeaders {
		req.Header.Set(k, v)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		duration := time.Since(startTime)
		embedding.LogConnectionError("openai", url, err)
		embedding.LogLLMCall("openai", c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		duration := time.Since(startTime)
		readErr := fmt.Errorf("failed to read response body: %w", err)
		embedding.LogLLMCall("openai", c.model, operation, 0, 0, duration, readErr)
		return LLMResponse{}, readErr
	}

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		// Check if this is a rate limit error
		if resp.StatusCode == 429 {
			embedding.LogRateLimitError("openai", c.model, resp.StatusCode, string(body))
		}

		// Extract user-friendly error message from OpenAI's error response
		userFriendlyMsg := extractErrorMessage(resp.StatusCode, body, "API error", extractOpenAIError)

		duration := time.Since(startTime)
		apiErr := fmt.Errorf("%s", userFriendlyMsg)
		embedding.LogLLMCall("openai", c.model, operation, 0, 0, duration, apiErr)
		return LLMResponse{}, apiErr
	}

	var openaiResp openaiResponse
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		duration := time.Since(startTime)
		embedding.LogLLMCall("openai", c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(openaiResp.Choices) == 0 {
		duration := time.Since(startTime)
		err := fmt.Errorf("no choices in response")
		embedding.LogLLMCall("openai", c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, err
	}

	choice := openaiResp.Choices[0]
	duration := time.Since(startTime)

	// Check if there are tool calls
	if choice.Message.ToolCalls != nil {
		toolCalls, ok := choice.Message.ToolCalls.([]any)
		if ok && len(toolCalls) > 0 {
			embedding.LogLLMResponseTrace("openai", c.model, operation, resp.StatusCode, "tool_calls")
			embedding.LogLLMCall("openai", c.model, operation, openaiResp.Usage.PromptTokens, openaiResp.Usage.CompletionTokens, duration, nil)

			// Build token usage for debug
			var tokenUsage *TokenUsage
			if c.debug {
				tokenUsage = &TokenUsage{
					Provider:         "openai",
					PromptTokens:     openaiResp.Usage.PromptTokens,
					CompletionTokens: openaiResp.Usage.CompletionTokens,
					TotalTokens:      openaiResp.Usage.TotalTokens,
				}

				logTokenUsage("OpenAI",
					openaiResp.Usage.PromptTokens,
					openaiResp.Usage.CompletionTokens,
					openaiResp.Usage.TotalTokens,
					0, 0, 0)
			}

			// Convert tool calls to our format
			content := make([]any, 0, len(toolCalls))
			for _, tc := range toolCalls {
				toolCall, ok := tc.(map[string]any)
				if !ok {
					continue
				}

				function, ok := toolCall["function"].(map[string]any)
				if !ok {
					continue
				}

				name, ok := function["name"].(string)
				if !ok {
					continue
				}
				argsStr, ok := function["arguments"].(string)
				if !ok {
					continue
				}

				var args map[string]any
				if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
					args = map[string]any{}
				}

				id, ok := toolCall["id"].(string)
				if !ok {
					continue
				}

				content = append(content, ToolUse{
					Type:  "tool_use",
					ID:    id,
					Name:  name,
					Input: args,
				})
			}

			return LLMResponse{
				Content:    content,
				StopReason: "tool_use",
				TokenUsage: tokenUsage,
			}, nil
		}
	}

	// It's a text response
	messageContent := ""
	if choice.Message.Content != nil {
		if contentStr, ok := choice.Message.Content.(string); ok {
			messageContent = contentStr
		}
	}

	embedding.LogLLMResponseTrace("openai", c.model, operation, resp.StatusCode, choice.FinishReason)
	embedding.LogLLMCall("openai", c.model, operation, openaiResp.Usage.PromptTokens, openaiResp.Usage.CompletionTokens, duration, nil)

	// Build token usage for debug
	var tokenUsage *TokenUsage
	if c.debug {
		tokenUsage = &TokenUsage{
			Provider:         "openai",
			PromptTokens:     openaiResp.Usage.PromptTokens,
			CompletionTokens: openaiResp.Usage.CompletionTokens,
			TotalTokens:      openaiResp.Usage.TotalTokens,
		}

		logTokenUsage("OpenAI",
			openaiResp.Usage.PromptTokens,
			openaiResp.Usage.CompletionTokens,
			openaiResp.Usage.TotalTokens,
			0, 0, 0)
	}

	return LLMResponse{
		Content: []any{
			TextContent{
				Type: "text",
				Text: messageContent,
			},
		},
		StopReason: "end_turn",
		TokenUsage: tokenUsage,
	}, nil
}

// ListModels returns available models from OpenAI
// Filters out embedding, audio, and image models
func (c *openaiClient) ListModels(ctx context.Context) ([]string, error) {
	url := c.baseURL + "/models"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Apply custom headers
	for k, v := range c.customHeaders {
		req.Header.Set(k, v)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck // Error response body read is best effort
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	// Parse response: {"data": [{"id": "gpt-5-main", ...}, ...]}
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]string, 0, len(response.Data))
	for _, model := range response.Data {
		id := model.ID

		// Exclude embedding models
		if strings.Contains(id, "embedding") {
			continue
		}

		// Exclude audio/speech models
		if strings.Contains(id, "whisper") ||
			strings.Contains(id, "tts") ||
			strings.Contains(id, "audio") {
			continue
		}

		// Exclude image models
		if strings.Contains(id, "dall-e") {
			continue
		}

		// Include only chat-capable models (gpt-*, o1-*, o3-*)
		if strings.Contains(id, "gpt") ||
			strings.HasPrefix(id, "o1-") ||
			strings.HasPrefix(id, "o3-") {
			models = append(models, id)
		}
	}

	return models, nil
}
