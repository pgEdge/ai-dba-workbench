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
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// -------------------------------------------------------------------------
// Ollama Client
// -------------------------------------------------------------------------

// ollamaClient implements LLMClient for Ollama
type ollamaClient struct {
	baseURL                string
	model                  string
	debug                  bool
	useCompactDescriptions bool
	client                 *http.Client
}

// NewOllamaClient creates a new Ollama client
func NewOllamaClient(baseURL, model string, debug bool, useCompactDescriptions bool) LLMClient {
	return &ollamaClient{
		baseURL:                baseURL,
		model:                  model,
		debug:                  debug,
		useCompactDescriptions: useCompactDescriptions,
		client:                 sharedHTTPClient,
	}
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type ollamaResponse struct {
	Model   string        `json:"model"`
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
}

// toolCallRequest represents a tool call parsed from Ollama's response
type toolCallRequest struct {
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments"`
}

type ollamaErrorResponse struct {
	Error string `json:"error"`
}

// extractOllamaError extracts an error message from Ollama's JSON error response.
func extractOllamaError(body []byte) string {
	var errResp ollamaErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		return errResp.Error
	}
	return ""
}

// extractJSONFromText attempts to extract a JSON object from text that may contain
// additional explanation or commentary around the JSON
func extractJSONFromText(text string) string {
	// Find the first '{' and last '}' to extract the JSON object
	firstBrace := strings.Index(text, "{")
	if firstBrace == -1 {
		return ""
	}

	// Find the matching closing brace by counting braces
	braceCount := 0
	lastBrace := -1
	for i := firstBrace; i < len(text); i++ {
		if text[i] == '{' {
			braceCount++
		} else if text[i] == '}' {
			braceCount--
			if braceCount == 0 {
				lastBrace = i
				break
			}
		}
	}

	if lastBrace == -1 {
		return ""
	}

	return text[firstBrace : lastBrace+1]
}

// ollamaToolInstructions contains the Ollama-specific instructions for tool
// calling via JSON, appended after the shared system prompt and tool list.
const ollamaToolInstructions = `
IMPORTANT INSTRUCTIONS:
1. When you need to use a tool, respond with ONLY a JSON object - no other text before or after:
{
    "tool": "tool_name",
    "arguments": {
        "param1": "value1",
        "param2": "value2"
    }
}

2. After calling a tool, you will receive actual results from the database.
3. You MUST base your response ONLY on the actual tool results provided - never make up or guess data.
4. If you receive tool results, format them clearly for the user.
5. Only use tools when necessary to answer the user's question.
6. Be concise and direct - show results without explaining your methodology unless specifically asked.
7. For historical trends, use datastore tools. For live queries, use monitored database tools.`

// ollamaSystemPromptWithTools returns the system prompt with tool information for Ollama.
// Since Ollama doesn't have native function calling, we include tool descriptions in the prompt
// and append JSON-based tool calling instructions.
func ollamaSystemPromptWithTools(toolsContext string) string {
	return systemPrompt + "\n\nYou have access to the following tools:\n\n" +
		toolsContext + "\n" + ollamaToolInstructions
}

func (c *ollamaClient) Chat(ctx context.Context, messages []Message, tools interface{}, customSystemPrompt string) (LLMResponse, error) {
	startTime := time.Now()
	operation := "chat"
	url := c.baseURL + "/api/chat"

	embedding.LogLLMCallDetails("ollama", c.model, operation, url, len(messages))

	// Convert interface{} tools to []mcp.Tool
	mcpTools, err := convertToMCPTools(tools)
	if err != nil {
		return LLMResponse{}, err
	}

	// Format tools for Ollama
	toolsContext := c.formatToolsForOllama(mcpTools)

	// Create system message with tool information; use custom prompt if provided
	var systemMessage string
	if customSystemPrompt != "" {
		// When a custom system prompt is provided, use it directly with tool context appended
		if toolsContext != "" {
			systemMessage = customSystemPrompt + "\n\nYou have access to the following tools:\n\n" + toolsContext
		} else {
			systemMessage = customSystemPrompt
		}
	} else {
		systemMessage = ollamaSystemPromptWithTools(toolsContext)
	}

	// Convert messages to Ollama format
	ollamaMessages := []ollamaMessage{
		{
			Role:    "system",
			Content: systemMessage,
		},
	}

	for _, msg := range messages {
		switch content := msg.Content.(type) {
		case string:
			ollamaMessages = append(ollamaMessages, ollamaMessage{
				Role:    msg.Role,
				Content: content,
			})
		case []interface{}:
			// Handle tool results
			var parts []string
			for _, item := range content {
				if tr, ok := item.(ToolResult); ok {
					contentStr := ""
					switch c := tr.Content.(type) {
					case string:
						contentStr = c
					case []mcp.ContentItem:
						var texts []string
						for _, ci := range c {
							texts = append(texts, ci.Text)
						}
						contentStr = strings.Join(texts, "\n")
					default:
						data, err := json.Marshal(c)
						if err != nil {
							contentStr = fmt.Sprintf("%v", c)
						} else {
							contentStr = string(data)
						}
					}
					parts = append(parts, fmt.Sprintf("Tool result:\n%s", contentStr))
				}
			}
			if len(parts) > 0 {
				ollamaMessages = append(ollamaMessages, ollamaMessage{
					Role:    msg.Role,
					Content: strings.Join(parts, "\n\n"),
				})
			}
		}
	}

	req := ollamaRequest{
		Model:    c.model,
		Messages: ollamaMessages,
		Stream:   false,
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewBuffer(reqData))
	if err != nil {
		return LLMResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		embedding.LogConnectionError("ollama", url, err)
		duration := time.Since(startTime)
		embedding.LogLLMCall("ollama", c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			duration := time.Since(startTime)
			readErr := fmt.Errorf("API error %d (failed to read body: %w)", resp.StatusCode, err)
			embedding.LogLLMCall("ollama", c.model, operation, 0, 0, duration, readErr)
			return LLMResponse{}, readErr
		}

		// Extract user-friendly error message from Ollama's error response
		userFriendlyMsg := extractErrorMessage(resp.StatusCode, body, "Ollama error", extractOllamaError)

		duration := time.Since(startTime)
		apiErr := fmt.Errorf("%s", userFriendlyMsg)
		embedding.LogLLMCall("ollama", c.model, operation, 0, 0, duration, apiErr)
		return LLMResponse{}, apiErr
	}

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		duration := time.Since(startTime)
		embedding.LogLLMCall("ollama", c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}

	content := ollamaResp.Message.Content

	// Try to parse as tool call
	// First try direct parsing (if the model behaved correctly)
	var toolCall toolCallRequest
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &toolCall); err == nil && toolCall.Tool != "" {
		// It's a tool call
		duration := time.Since(startTime)
		embedding.LogLLMResponseTrace("ollama", c.model, operation, resp.StatusCode, "tool_use")
		embedding.LogLLMCall("ollama", c.model, operation, 0, 0, duration, nil) // Ollama doesn't provide token counts

		// Build token usage for debug (Ollama doesn't provide counts)
		var tokenUsage *TokenUsage
		if c.debug {
			tokenUsage = &TokenUsage{
				Provider: "ollama",
			}
			logTokenUsage("Ollama", 0, 0, 0, 0, 0, 0)
		}

		return LLMResponse{
			Content: []interface{}{
				ToolUse{
					Type:  "tool_use",
					ID:    "ollama-tool-1", // Ollama doesn't provide IDs, so we generate one
					Name:  toolCall.Tool,
					Input: toolCall.Arguments,
				},
			},
			StopReason: "tool_use",
			TokenUsage: tokenUsage,
		}, nil
	}

	// If direct parsing failed, try to extract JSON from surrounding text
	// This handles cases where the model adds explanation around the JSON
	if extractedJSON := extractJSONFromText(content); extractedJSON != "" {
		if err := json.Unmarshal([]byte(extractedJSON), &toolCall); err == nil && toolCall.Tool != "" {
			// Successfully extracted and parsed tool call
			duration := time.Since(startTime)
			embedding.LogLLMResponseTrace("ollama", c.model, operation, resp.StatusCode, "tool_use")
			embedding.LogLLMCall("ollama", c.model, operation, 0, 0, duration, nil)

			// Build token usage for debug
			var tokenUsage *TokenUsage
			if c.debug {
				tokenUsage = &TokenUsage{
					Provider: "ollama",
				}
				logTokenUsage("Ollama", 0, 0, 0, 0, 0, 0)
			}

			return LLMResponse{
				Content: []interface{}{
					ToolUse{
						Type:  "tool_use",
						ID:    "ollama-tool-1",
						Name:  toolCall.Tool,
						Input: toolCall.Arguments,
					},
				},
				StopReason: "tool_use",
				TokenUsage: tokenUsage,
			}, nil
		}
	}

	// It's a text response
	duration := time.Since(startTime)
	embedding.LogLLMResponseTrace("ollama", c.model, operation, resp.StatusCode, "end_turn")
	embedding.LogLLMCall("ollama", c.model, operation, 0, 0, duration, nil) // Ollama doesn't provide token counts

	// Build token usage for debug (Ollama doesn't provide counts)
	var tokenUsage *TokenUsage
	if c.debug {
		tokenUsage = &TokenUsage{
			Provider: "ollama",
		}
		logTokenUsage("Ollama", 0, 0, 0, 0, 0, 0)
	}

	return LLMResponse{
		Content: []interface{}{
			TextContent{
				Type: "text",
				Text: content,
			},
		},
		StopReason: "end_turn",
		TokenUsage: tokenUsage,
	}, nil
}

func (c *ollamaClient) formatToolsForOllama(tools []mcp.Tool) string {
	var toolDescriptions []string
	for _, tool := range tools {
		desc := tool.Description
		if c.useCompactDescriptions && tool.CompactDescription != "" {
			desc = tool.CompactDescription
		}
		toolDesc := fmt.Sprintf("- %s: %s", tool.Name, desc)

		// Add parameter info if available
		if len(tool.InputSchema.Properties) > 0 {
			var params []string
			for paramName, paramInfo := range tool.InputSchema.Properties {
				paramMap, ok := paramInfo.(map[string]interface{})
				if !ok {
					continue
				}
				paramType, _ := paramMap["type"].(string)        //nolint:errcheck // Optional field, default to empty
				paramDesc, _ := paramMap["description"].(string) //nolint:errcheck // Optional field, default to empty
				if paramType == "" {
					paramType = "any"
				}
				params = append(params, fmt.Sprintf("%s (%s): %s", paramName, paramType, paramDesc))
			}
			if len(params) > 0 {
				toolDesc += "\n  Parameters:\n    " + strings.Join(params, "\n    ")
			}
		}

		toolDescriptions = append(toolDescriptions, toolDesc)
	}

	return strings.Join(toolDescriptions, "\n")
}

// ListModels returns available models from the Ollama server
func (c *ollamaClient) ListModels(ctx context.Context) ([]string, error) {
	url := c.baseURL + "/api/tags"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck // Error response body read is best effort
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	// Parse response: {"models": [{"name": "llama3", ...}, ...]}
	var response struct {
		Models []struct {
			Name string `json:"name"`
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
