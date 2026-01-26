/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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
	"os"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/cli/internal/mcp"
	"github.com/pgedge/ai-workbench/pkg/embedding"
)

// Message represents a chat message
type Message struct {
	Role         string                 `json:"role"`
	Content      interface{}            `json:"content"`
	CacheControl map[string]interface{} `json:"cache_control,omitempty"`
}

// ToolUse represents a tool invocation in a message
type ToolUse struct {
	Type  string                 `json:"type"`
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

// TextContent represents text content in a message
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Type      string      `json:"type"`
	ToolUseID string      `json:"tool_use_id"`
	Content   interface{} `json:"content"`
	IsError   bool        `json:"is_error,omitempty"`
}

// LLMResponse represents a response from the LLM
type LLMResponse struct {
	Content    []interface{} // Can be TextContent or ToolUse
	StopReason string
	TokenUsage *TokenUsage `json:"token_usage,omitempty"`
}

// TokenUsage holds token usage information for debug purposes
type TokenUsage struct {
	Provider               string  `json:"provider"`
	PromptTokens           int     `json:"prompt_tokens,omitempty"`
	CompletionTokens       int     `json:"completion_tokens,omitempty"`
	TotalTokens            int     `json:"total_tokens,omitempty"`
	CacheCreationTokens    int     `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens        int     `json:"cache_read_tokens,omitempty"`
	CacheSavingsPercentage float64 `json:"cache_savings_percentage,omitempty"`
}

// LLMClient provides a unified interface for different LLM providers
type LLMClient interface {
	// Chat sends messages and available tools to the LLM and returns the response
	Chat(ctx context.Context, messages []Message, tools interface{}) (LLMResponse, error)

	// ListModels returns a list of available models from the provider
	ListModels(ctx context.Context) ([]string, error)
}

// proxyClient implements LLMClient by calling the server's LLM proxy endpoints
type proxyClient struct {
	baseURL  string // Base URL of the server (e.g., "http://localhost:8080")
	token    string // Authentication token
	provider string // LLM provider to use (anthropic, openai, ollama)
	model    string // Model to use
	debug    bool   // Enable debug mode
	client   *http.Client
}

// NewProxyClient creates a new proxy client that uses the server's LLM proxy
func NewProxyClient(baseURL, token, provider, model string, debug bool) LLMClient {
	// Remove /mcp/v1 suffix if present to get base URL
	baseURL = strings.TrimSuffix(baseURL, "/mcp/v1")
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &proxyClient{
		baseURL:  baseURL,
		token:    token,
		provider: provider,
		model:    model,
		debug:    debug,
		client:   &http.Client{},
	}
}

// proxyRequest represents the request body for POST /api/v1/llm/chat
type proxyRequest struct {
	Messages []Message   `json:"messages"`
	Tools    interface{} `json:"tools"`
	Provider string      `json:"provider,omitempty"`
	Model    string      `json:"model,omitempty"`
	Debug    bool        `json:"debug,omitempty"`
}

// proxyResponse represents the response body from POST /api/v1/llm/chat
type proxyResponse struct {
	Content    []map[string]interface{} `json:"content"`
	StopReason string                   `json:"stop_reason"`
	TokenUsage *TokenUsage              `json:"token_usage,omitempty"`
}

func (c *proxyClient) Chat(ctx context.Context, messages []Message, tools interface{}) (LLMResponse, error) {
	startTime := time.Now()
	operation := "chat"
	url := c.baseURL + "/api/v1/llm/chat"

	embedding.LogLLMCallDetails(c.provider, c.model, operation, url, len(messages))

	// Convert tools to the format expected by the proxy
	var proxyTools interface{}
	if tools != nil {
		// Convert to []mcp.Tool first to ensure correct format
		var mcpTools []mcp.Tool
		toolsJSON, err := json.Marshal(tools)
		if err != nil {
			return LLMResponse{}, fmt.Errorf("failed to marshal tools: %w", err)
		}
		if err := json.Unmarshal(toolsJSON, &mcpTools); err != nil {
			return LLMResponse{}, fmt.Errorf("failed to unmarshal tools: %w", err)
		}

		// Convert to proxy format (same structure but without cache_control)
		proxyToolList := make([]map[string]interface{}, len(mcpTools))
		for i, tool := range mcpTools {
			proxyToolList[i] = map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			}
		}
		proxyTools = proxyToolList
	}

	// Build request
	req := proxyRequest{
		Messages: messages,
		Tools:    proxyTools,
		Provider: c.provider,
		Model:    c.model,
		Debug:    c.debug,
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	embedding.LogLLMRequestTrace(c.provider, c.model, operation, string(reqData))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqData))
	if err != nil {
		return LLMResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		embedding.LogConnectionError(c.provider, url, err)
		duration := time.Since(startTime)
		embedding.LogLLMCall(c.provider, c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			duration := time.Since(startTime)
			readErr := fmt.Errorf("LLM proxy error %d (failed to read body: %w)", resp.StatusCode, err)
			embedding.LogLLMCall(c.provider, c.model, operation, 0, 0, duration, readErr)
			return LLMResponse{}, readErr
		}

		// Check for rate limit
		if resp.StatusCode == 429 {
			embedding.LogRateLimitError(c.provider, c.model, resp.StatusCode, string(body))
		}

		duration := time.Since(startTime)
		apiErr := fmt.Errorf("LLM proxy error (%d): %s", resp.StatusCode, string(body))
		embedding.LogLLMCall(c.provider, c.model, operation, 0, 0, duration, apiErr)
		return LLMResponse{}, apiErr
	}

	var proxyResp proxyResponse
	if err := json.NewDecoder(resp.Body).Decode(&proxyResp); err != nil {
		duration := time.Since(startTime)
		embedding.LogLLMCall(c.provider, c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert response content to typed structs
	content := make([]interface{}, 0, len(proxyResp.Content))
	for _, item := range proxyResp.Content {
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
	embedding.LogLLMResponseTrace(c.provider, c.model, operation, resp.StatusCode, proxyResp.StopReason)

	// Log token usage if available
	promptTokens := 0
	completionTokens := 0
	if proxyResp.TokenUsage != nil {
		promptTokens = proxyResp.TokenUsage.PromptTokens
		completionTokens = proxyResp.TokenUsage.CompletionTokens
	}
	embedding.LogLLMCall(c.provider, c.model, operation, promptTokens, completionTokens, duration, nil)

	// Log debug info if enabled
	if c.debug && proxyResp.TokenUsage != nil {
		tu := proxyResp.TokenUsage
		if tu.CacheCreationTokens > 0 || tu.CacheReadTokens > 0 {
			fmt.Fprintf(os.Stderr, "\r\n[LLM] [DEBUG] %s - Prompt Cache: Created %d tokens, Read %d tokens (saved ~%.0f%% on input)\n",
				tu.Provider, tu.CacheCreationTokens, tu.CacheReadTokens, tu.CacheSavingsPercentage)
			fmt.Fprintf(os.Stderr, "\r[LLM] [DEBUG] %s - Tokens: Input %d, Output %d, Total %d\n",
				tu.Provider, tu.PromptTokens, tu.CompletionTokens, tu.TotalTokens)
		} else {
			fmt.Fprintf(os.Stderr, "\r\n[LLM] [DEBUG] %s - Tokens: Input %d, Output %d, Total %d\n",
				tu.Provider, tu.PromptTokens, tu.CompletionTokens, tu.TotalTokens)
		}
	}

	return LLMResponse{
		Content:    content,
		StopReason: proxyResp.StopReason,
		TokenUsage: proxyResp.TokenUsage,
	}, nil
}

// ListModels returns available models from the server's LLM proxy
func (c *proxyClient) ListModels(ctx context.Context) ([]string, error) {
	url := c.baseURL + "/api/v1/llm/models?provider=" + c.provider

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Include auth token for consistency
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck // Error response body read is best effort
		return nil, fmt.Errorf("LLM proxy error (%d): %s", resp.StatusCode, string(body))
	}

	// Parse response: {"models": [{"name": "model-name", ...}, ...]}
	var response struct {
		Models []struct {
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]string, 0, len(response.Models))
	for _, model := range response.Models {
		models = append(models, model.Name)
	}

	return models, nil
}
