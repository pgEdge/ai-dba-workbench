/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/ollama/ollama/api"
)

// LLMClient defines the interface for LLM clients
type LLMClient interface {
	// Chat sends a message to the LLM and returns the response
	// If mcpClient is provided, the LLM can execute MCP tools
	Chat(ctx context.Context, messages []Message, tools []Tool, resources []Resource, mcpClient *MCPClient) (string, error)
}

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"` // The message content
}

// Tool represents an MCP tool that the LLM can use
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// Resource represents an MCP resource that the LLM can read
type Resource struct {
	URI         string      `json:"uri"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	MimeType    string      `json:"mimeType"`
	Data        interface{} `json:"data,omitempty"` // Actual resource data if fetched
}

// createDiscoveryTools creates virtual tools for tool discovery
func createDiscoveryTools() map[string]Tool {
	return map[string]Tool{
		"list_available_tools": {
			Name:        "list_available_tools",
			Description: "Lists all available MCP tools with their names and brief descriptions. Use this to discover what tools are available before calling them.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			},
		},
		"get_tool_schema": {
			Name:        "get_tool_schema",
			Description: "Gets the full schema for a specific tool by name. Use this to learn what parameters a tool requires before calling it.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tool_name": map[string]interface{}{
						"type":        "string",
						"description": "The name of the tool to get the schema for",
					},
				},
				"required": []string{"tool_name"},
			},
		},
	}
}

// handleDiscoveryToolCall handles calls to virtual discovery tools
// Returns (result, toolWasDiscovery, error)
func handleDiscoveryToolCall(toolName string, args map[string]interface{}, fullCatalog map[string]Tool, availableTools map[string]Tool) (interface{}, bool, error) {
	switch toolName {
	case "list_available_tools":
		// Return list of all tools in catalog
		var toolList []map[string]string
		for name, tool := range fullCatalog {
			// Get first line of description for brevity
			desc := tool.Description
			if idx := strings.Index(desc, "\n"); idx > 0 {
				desc = desc[:idx]
			}
			toolList = append(toolList, map[string]string{
				"name":        name,
				"description": desc,
			})
		}
		return map[string]interface{}{
			"tools": toolList,
		}, true, nil

	case "get_tool_schema":
		// Get tool name from args
		toolNameArg, ok := args["tool_name"].(string)
		if !ok {
			return nil, true, fmt.Errorf("tool_name parameter is required and must be a string")
		}

		// Look up tool in catalog
		tool, exists := fullCatalog[toolNameArg]
		if !exists {
			return nil, true, fmt.Errorf("tool not found: %s", toolNameArg)
		}

		// Add tool to available tools for future use
		availableTools[toolNameArg] = tool

		// Return full tool schema
		return map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.InputSchema,
		}, true, nil

	default:
		// Not a discovery tool
		return nil, false, nil
	}
}

// LLMConfig holds the configuration for LLM clients
type LLMConfig struct {
	Provider       string // "anthropic" or "ollama"
	AnthropicKey   string
	AnthropicModel string
	OllamaURL      string
	OllamaModel    string
}

// NewLLMConfig creates a new LLM configuration from environment variables
func NewLLMConfig() *LLMConfig {
	config := &LLMConfig{
		AnthropicKey:   os.Getenv("AI_CLI_ANTHROPIC_API_KEY"),
		AnthropicModel: os.Getenv("AI_CLI_ANTHROPIC_MODEL"),
		OllamaURL:      os.Getenv("AI_CLI_OLLAMA_URL"),
		OllamaModel:    os.Getenv("AI_CLI_OLLAMA_MODEL"),
	}

	// Set defaults
	if config.AnthropicModel == "" {
		config.AnthropicModel = "claude-sonnet-4-5"
	}
	if config.OllamaURL == "" {
		config.OllamaURL = "http://localhost:11434"
	}
	if config.OllamaModel == "" {
		// gpt-oss:20b has excellent function calling support
		// Note: qwen models (qwen3-coder, qwen2.5-coder) don't work reliably
		// with Ollama's function calling implementation - they return empty
		// responses regardless of tool count
		config.OllamaModel = "gpt-oss:20b"
	}

	// Prefer Anthropic if API key is set
	if config.AnthropicKey != "" {
		config.Provider = "anthropic"
	} else {
		config.Provider = "ollama"
	}

	return config
}

// NewLLMClient creates a new LLM client based on the configuration
func NewLLMClient(config *LLMConfig) (LLMClient, error) {
	switch config.Provider {
	case "anthropic":
		return NewAnthropicClient(config)
	case "ollama":
		return NewOllamaClient(config)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", config.Provider)
	}
}

// AnthropicClient implements the LLMClient interface using Anthropic's API
type AnthropicClient struct {
	apiKey string
	model  string
}

// NewAnthropicClient creates a new Anthropic client
func NewAnthropicClient(config *LLMConfig) (*AnthropicClient, error) {
	if config.AnthropicKey == "" {
		return nil, fmt.Errorf("Anthropic API key is required. Set it using:\n  - AI_CLI_ANTHROPIC_API_KEY environment variable\n  - Config file: ./ai-cli set-anthropic-key <key>")
	}

	return &AnthropicClient{
		apiKey: config.AnthropicKey,
		model:  config.AnthropicModel,
	}, nil
}

// Chat implements the LLMClient interface for Anthropic with tool execution support
func (a *AnthropicClient) Chat(ctx context.Context, messages []Message, tools []Tool, resources []Resource, mcpClient *MCPClient) (string, error) {
	const maxIterations = 10 // Prevent infinite loops

	// Store full tool catalog for discovery (map by name for fast lookup)
	fullToolCatalog := make(map[string]Tool)
	for _, tool := range tools {
		fullToolCatalog[tool.Name] = tool
	}

	// Create virtual discovery tools
	discoveryTools := createDiscoveryTools()

	// Essential tools to include upfront (most commonly used)
	essentialToolNames := map[string]bool{
		"execute_query":        true,
		"set_database_context": true,
		"get_database_context": true,
		"read_resource":        true,
	}

	// Track which tools are currently available to the LLM
	availableTools := make(map[string]Tool)

	// Add discovery tools (always available)
	for name, tool := range discoveryTools {
		availableTools[name] = tool
	}

	// Add essential tools from the full catalog
	for name := range essentialToolNames {
		if tool, exists := fullToolCatalog[name]; exists {
			availableTools[name] = tool
		}
	}

	// Convert available tools to Anthropic format
	buildAnthropicTools := func() []map[string]interface{} {
		var anthropicTools []map[string]interface{}
		for _, tool := range availableTools {
			anthropicTools = append(anthropicTools, map[string]interface{}{
				"name":         tool.Name,
				"description":  tool.Description,
				"input_schema": tool.InputSchema,
			})
		}
		return anthropicTools
	}

	// Build system message
	systemMessage := "You are a helpful AI assistant with access to MCP tools for managing PostgreSQL databases.\n\n"
	systemMessage += "IMPORTANT: When presenting query results or data to the user:\n"
	systemMessage += "- Format data in clear, readable tables or lists\n"
	systemMessage += "- Do NOT return raw JSON - convert it to human-readable format\n"
	systemMessage += "- Use proper formatting with headers and aligned columns\n"
	systemMessage += "- Provide a brief summary before showing detailed results\n"
	if len(resources) > 0 {
		// Only include resource metadata (URIs and descriptions), not actual data
		// The LLM can use read_resource tool to fetch data when needed
		systemMessage += "\n\nAvailable MCP Resources (use read_resource tool to fetch data):\n"
		for _, resource := range resources {
			systemMessage += fmt.Sprintf("- %s", resource.URI)
			if resource.Name != "" {
				systemMessage += fmt.Sprintf(" (%s)", resource.Name)
			}
			if resource.Description != "" {
				systemMessage += fmt.Sprintf(": %s", resource.Description)
			}
			systemMessage += "\n"
		}
	}

	// Convert messages to Anthropic format
	var anthropicMessages []map[string]interface{}
	for _, msg := range messages {
		anthropicMessages = append(anthropicMessages, map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	// Agentic loop - continue until we get a text-only response
	for iteration := 0; iteration < maxIterations; iteration++ {
		// Create request body
		requestBody := map[string]interface{}{
			"model":      a.model,
			"max_tokens": 4096,
			"system":     systemMessage,
			"messages":   anthropicMessages,
		}

		// Add tools if MCP client is available
		anthropicTools := buildAnthropicTools()
		if mcpClient != nil && len(anthropicTools) > 0 {
			requestBody["tools"] = anthropicTools
		}

		// Marshal request
		jsonData, err := json.Marshal(requestBody)
		if err != nil {
			return "", fmt.Errorf("failed to marshal request: %w", err)
		}

		// Create HTTP request
		req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonData))
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		// Set headers
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", a.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		// Send request
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to send request: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		// Read response
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
		}

		// Parse response
		var response struct {
			Content []struct {
				Type  string                 `json:"type"`
				Text  string                 `json:"text,omitempty"`
				ID    string                 `json:"id,omitempty"`
				Name  string                 `json:"name,omitempty"`
				Input map[string]interface{} `json:"input,omitempty"`
			} `json:"content"`
			StopReason string `json:"stop_reason"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return "", fmt.Errorf("failed to parse response: %w", err)
		}

		// Check if we got a final text response
		if response.StopReason == "end_turn" {
			var result string
			for _, block := range response.Content {
				if block.Type == "text" {
					result += block.Text
				}
			}
			return result, nil
		}

		// Check for tool use
		if response.StopReason == "tool_use" {
			if mcpClient == nil {
				return "", fmt.Errorf("LLM requested tool execution but no MCP client provided")
			}

			// Add assistant's response to conversation (including tool_use blocks)
			assistantContent := []map[string]interface{}{}
			var textParts []string

			for _, block := range response.Content {
				if block.Type == "text" {
					textParts = append(textParts, block.Text)
					assistantContent = append(assistantContent, map[string]interface{}{
						"type": "text",
						"text": block.Text,
					})
				} else if block.Type == "tool_use" {
					assistantContent = append(assistantContent, map[string]interface{}{
						"type":  "tool_use",
						"id":    block.ID,
						"name":  block.Name,
						"input": block.Input,
					})
				}
			}

			anthropicMessages = append(anthropicMessages, map[string]interface{}{
				"role":    "assistant",
				"content": assistantContent,
			})

			// Show user what tool is being called (only if there was text)
			if len(textParts) > 0 {
				fmt.Fprintf(os.Stderr, "\n%s\n", strings.Join(textParts, "\n"))
			}

			// Execute tools and collect results
			toolResults := []map[string]interface{}{}
			for _, block := range response.Content {
				if block.Type == "tool_use" {
					fmt.Fprintf(os.Stderr, "\033[31m→ Calling tool: %s\033[0m\n", block.Name)

					// Check if this is a discovery tool call
					result, isDiscovery, err := handleDiscoveryToolCall(block.Name, block.Input, fullToolCatalog, availableTools)

					if isDiscovery {
						// Handle discovery tool locally
						if err != nil {
							toolResults = append(toolResults, map[string]interface{}{
								"type":       "tool_result",
								"tool_use_id": block.ID,
								"content":    fmt.Sprintf("Error: %v", err),
								"is_error":   true,
							})
							continue
						}

						// Format discovery result
						resultBytes, err := json.Marshal(result)
						if err != nil {
							toolResults = append(toolResults, map[string]interface{}{
								"type":       "tool_result",
								"tool_use_id": block.ID,
								"content":    fmt.Sprintf("Error marshaling result: %v", err),
								"is_error":   true,
							})
							continue
						}
						toolResults = append(toolResults, map[string]interface{}{
							"type":       "tool_result",
							"tool_use_id": block.ID,
							"content":    string(resultBytes),
						})
					} else {
						// Execute the tool via MCP
						result, err := mcpClient.CallTool(block.Name, block.Input)
						if err != nil {
							// Return error as tool result
							toolResults = append(toolResults, map[string]interface{}{
								"type":       "tool_result",
								"tool_use_id": block.ID,
								"content":    fmt.Sprintf("Error: %v", err),
								"is_error":   true,
							})
							continue
						}

						// Extract text from tool result
						resultText := formatToolResult(result)
						toolResults = append(toolResults, map[string]interface{}{
							"type":       "tool_result",
							"tool_use_id": block.ID,
							"content":    resultText,
						})
					}
				}
			}

			// Add tool results to conversation
			anthropicMessages = append(anthropicMessages, map[string]interface{}{
				"role":    "user",
				"content": toolResults,
			})

			// Continue loop to get next response from Claude
			continue
		}

		// Unknown stop reason - return what we have
		var result string
		for _, block := range response.Content {
			if block.Type == "text" {
				result += block.Text
			}
		}
		return result, nil
	}

	return "", fmt.Errorf("exceeded maximum iterations (%d) in agentic loop", maxIterations)
}

// formatToolResult converts MCP tool result to string for Claude
func formatToolResult(result interface{}) string {
	// Handle the MCP tool result format
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		// Fallback: JSON encode the result
		bytes, err := json.Marshal(result)
		if err != nil {
			return fmt.Sprintf("%v", result)
		}
		return string(bytes)
	}

	// Extract content from MCP response
	content, ok := resultMap["content"].([]interface{})
	if !ok || len(content) == 0 {
		bytes, err := json.Marshal(result)
		if err != nil {
			return fmt.Sprintf("%v", result)
		}
		return string(bytes)
	}

	// Collect all text from content blocks
	var textParts []string
	for _, item := range content {
		if contentMap, ok := item.(map[string]interface{}); ok {
			if text, ok := contentMap["text"].(string); ok {
				textParts = append(textParts, text)
			}
		}
	}

	if len(textParts) > 0 {
		return strings.Join(textParts, "\n")
	}

	// Fallback: JSON encode
	bytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("%v", result)
	}
	return string(bytes)
}

// formatJSONResponse detects and formats JSON responses into readable text
func formatJSONResponse(response string) string {
	// Try to find JSON object in the response (looking for {...} pattern)
	startIdx := strings.Index(response, "\n{")
	if startIdx == -1 {
		startIdx = strings.Index(response, "{")
		if startIdx == -1 {
			// No JSON found, return as-is
			return response
		}
	} else {
		// Skip the newline
		startIdx++
	}

	// Find the matching closing brace
	braceCount := 0
	endIdx := -1
	for i := startIdx; i < len(response); i++ {
		if response[i] == '{' {
			braceCount++
		} else if response[i] == '}' {
			braceCount--
			if braceCount == 0 {
				endIdx = i + 1
				break
			}
		}
	}

	if endIdx == -1 {
		// No complete JSON found
		return response
	}

	// Extract the JSON portion
	jsonStr := response[startIdx:endIdx]

	// Try to parse as JSON
	var data interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		// Not valid JSON, return as-is
		return response
	}

	// Check if it's a query result format (has "columns" and "rows")
	if dataMap, ok := data.(map[string]interface{}); ok {
		if columns, hasColumns := dataMap["columns"].([]interface{}); hasColumns {
			if rows, hasRows := dataMap["rows"].([]interface{}); hasRows {
				// Format the table
				formattedTable := formatQueryResultTable(columns, rows, dataMap)

				// Replace the JSON in the original response with the formatted table
				return response[:startIdx] + "\n" + formattedTable
			}
		}
	}

	// For other JSON, just return as-is
	return response
}

// formatQueryResultTable formats query results into a readable table
func formatQueryResultTable(columns []interface{}, rows []interface{}, data map[string]interface{}) string {
	var result strings.Builder

	result.WriteString("Query Results:\n")
	result.WriteString("==============\n\n")

	// Show row count
	rowCount := len(rows)
	result.WriteString(fmt.Sprintf("Found %d row(s)\n\n", rowCount))

	if rowCount == 0 {
		return result.String()
	}

	// Convert columns to strings
	colNames := make([]string, len(columns))
	for i, col := range columns {
		if colStr, ok := col.(string); ok {
			colNames[i] = colStr
		} else {
			colNames[i] = fmt.Sprintf("%v", col)
		}
	}

	// Calculate column widths
	colWidths := make([]int, len(colNames))
	for i, name := range colNames {
		colWidths[i] = len(name)
	}

	// Convert rows and calculate max widths
	stringRows := make([][]string, len(rows))
	for i, row := range rows {
		if rowSlice, ok := row.([]interface{}); ok {
			stringRow := make([]string, len(rowSlice))
			for j, val := range rowSlice {
				valStr := fmt.Sprintf("%v", val)
				if val == nil {
					valStr = "NULL"
				}
				stringRow[j] = valStr
				if len(valStr) > colWidths[j] {
					colWidths[j] = len(valStr)
				}
			}
			stringRows[i] = stringRow
		}
	}

	// Print header
	for i, name := range colNames {
		result.WriteString(fmt.Sprintf("%-*s", colWidths[i]+2, name))
	}
	result.WriteString("\n")

	// Print separator
	for _, width := range colWidths {
		result.WriteString(strings.Repeat("-", width+2))
	}
	result.WriteString("\n")

	// Print rows
	for _, row := range stringRows {
		for i, val := range row {
			result.WriteString(fmt.Sprintf("%-*s", colWidths[i]+2, val))
		}
		result.WriteString("\n")
	}

	// Check if truncated
	if truncated, ok := data["truncated"].(bool); ok && truncated {
		result.WriteString("\n⚠️  Results truncated\n")
	}

	return result.String()
}

// OllamaClient implements the LLMClient interface using Ollama
type OllamaClient struct {
	client *api.Client
	model  string
}

// NewOllamaClient creates a new Ollama client
func NewOllamaClient(config *LLMConfig) (*OllamaClient, error) {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("failed to create Ollama client: %w", err)
	}

	return &OllamaClient{
		client: client,
		model:  config.OllamaModel,
	}, nil
}

// Chat implements the LLMClient interface for Ollama with tool execution support
func (o *OllamaClient) Chat(ctx context.Context, messages []Message, tools []Tool, resources []Resource, mcpClient *MCPClient) (string, error) {
	const maxIterations = 10 // Prevent infinite loops

	// Store full tool catalog for discovery (map by name for fast lookup)
	fullToolCatalog := make(map[string]Tool)
	for _, tool := range tools {
		fullToolCatalog[tool.Name] = tool
	}

	// Create virtual discovery tools
	discoveryTools := createDiscoveryTools()

	// Essential tools to include upfront (most commonly used)
	essentialToolNames := map[string]bool{
		"execute_query":        true,
		"set_database_context": true,
		"get_database_context": true,
		"read_resource":        true,
	}

	// Track which tools are currently available to the LLM
	availableTools := make(map[string]Tool)

	// Add discovery tools (always available)
	for name, tool := range discoveryTools {
		availableTools[name] = tool
	}

	// Add essential tools from the full catalog
	for name := range essentialToolNames {
		if tool, exists := fullToolCatalog[name]; exists {
			availableTools[name] = tool
		}
	}

	// Convert available tools to Ollama format
	buildOllamaTools := func() []api.Tool {
		var ollamaTools []api.Tool
		for _, tool := range availableTools {
			// Simplify description - keep only first line/sentence
			description := tool.Description
			if idx := strings.Index(description, "\n"); idx > 0 {
				description = description[:idx]
			}
			if len(description) > 200 {
				description = description[:200] + "..."
			}

			// Convert InputSchema to ToolFunctionParameters
			schemaBytes, err := json.Marshal(tool.InputSchema)
			if err != nil {
				continue
			}
			var params api.ToolFunctionParameters
			if err := json.Unmarshal(schemaBytes, &params); err != nil {
				continue
			}

			ollamaTools = append(ollamaTools, api.Tool{
				Type: "function",
				Function: api.ToolFunction{
					Name:        tool.Name,
					Description: description,
					Parameters:  params,
				},
			})
		}
		return ollamaTools
	}

	// Build system message
	systemMessage := "You are a helpful AI assistant with access to MCP tools for managing PostgreSQL databases.\n\n"
	systemMessage += "IMPORTANT: When presenting query results or data to the user:\n"
	systemMessage += "- Format data in clear, readable tables or lists\n"
	systemMessage += "- Do NOT return raw JSON - convert it to human-readable format\n"
	systemMessage += "- Use proper formatting with headers and aligned columns\n"
	systemMessage += "- Provide a brief summary before showing detailed results\n"
	if len(resources) > 0 {
		// Only include resource metadata (URIs and descriptions), not actual data
		// The LLM can use read_resource tool to fetch data when needed
		systemMessage += "\n\nAvailable MCP Resources (use read_resource tool to fetch data):\n"
		for _, resource := range resources {
			systemMessage += fmt.Sprintf("- %s", resource.URI)
			if resource.Name != "" {
				systemMessage += fmt.Sprintf(" (%s)", resource.Name)
			}
			if resource.Description != "" {
				systemMessage += fmt.Sprintf(": %s", resource.Description)
			}
			systemMessage += "\n"
		}
	}

	// Convert messages to Ollama format
	var ollamaMessages []api.Message
	ollamaMessages = append(ollamaMessages, api.Message{
		Role:    "system",
		Content: systemMessage,
	})
	for _, msg := range messages {
		ollamaMessages = append(ollamaMessages, api.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Agentic loop - continue until we get a text-only response
	var lastToolResults []string
	toolsExecuted := false

	for iteration := 0; iteration < maxIterations; iteration++ {
		// Create request
		req := &api.ChatRequest{
			Model:    o.model,
			Messages: ollamaMessages,
		}

		// Add tools if MCP client is available
		ollamaTools := buildOllamaTools()
		if mcpClient != nil && len(ollamaTools) > 0 {
			req.Tools = ollamaTools
		}

		// Send request and collect response
		var lastMessage api.Message
		err := o.client.Chat(ctx, req, func(resp api.ChatResponse) error {
			lastMessage = resp.Message
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("failed to chat with Ollama: %w", err)
		}

		// Add assistant's response to conversation
		ollamaMessages = append(ollamaMessages, lastMessage)

		// If there are no tool calls, return the text response
		if len(lastMessage.ToolCalls) == 0 {
			if lastMessage.Content == "" {
				// If tools were executed but model didn't provide summary, return last tool results
				if toolsExecuted && len(lastToolResults) > 0 {
					fmt.Fprintf(os.Stderr, "\n\033[31mNote: Model completed tool execution but didn't provide a summary. Showing raw tool results:\033[0m\n\n")
					// Format JSON responses into readable tables
					return formatJSONResponse(strings.Join(lastToolResults, "\n\n")), nil
				}
				return "", fmt.Errorf("Ollama returned empty response with no tool calls")
			}
			// Format JSON responses into readable tables
			return formatJSONResponse(lastMessage.Content), nil
		}

		// Execute tools and collect results
		if mcpClient == nil {
			return "", fmt.Errorf("LLM requested tool execution but no MCP client provided")
		}

		// Show any text that came with the tool calls
		if lastMessage.Content != "" {
			fmt.Fprintf(os.Stderr, "\n%s\n", lastMessage.Content)
		}

		// Clear previous tool results for this iteration
		lastToolResults = []string{}

		// Execute each tool call
		for _, toolCall := range lastMessage.ToolCalls {
			fmt.Fprintf(os.Stderr, "\033[31m→ Calling tool: %s\033[0m\n", toolCall.Function.Name)

			// Arguments are already in map format
			args := map[string]interface{}(toolCall.Function.Arguments)

			// Check if this is a discovery tool call
			result, isDiscovery, err := handleDiscoveryToolCall(toolCall.Function.Name, args, fullToolCatalog, availableTools)

			if isDiscovery {
				// Handle discovery tool locally
				if err != nil {
					errMsg := fmt.Sprintf("Error: %v", err)
					ollamaMessages = append(ollamaMessages, api.Message{
						Role:    "tool",
						Content: errMsg,
					})
					lastToolResults = append(lastToolResults, fmt.Sprintf("Tool %s failed: %s", toolCall.Function.Name, errMsg))
					continue
				}

				// Format discovery result
				resultBytes, err := json.Marshal(result)
				if err != nil {
					errMsg := fmt.Sprintf("Error marshaling result: %v", err)
					ollamaMessages = append(ollamaMessages, api.Message{
						Role:    "tool",
						Content: errMsg,
					})
					lastToolResults = append(lastToolResults, fmt.Sprintf("Tool %s failed: %s", toolCall.Function.Name, errMsg))
					continue
				}
				ollamaMessages = append(ollamaMessages, api.Message{
					Role:    "tool",
					Content: string(resultBytes),
				})
				lastToolResults = append(lastToolResults, fmt.Sprintf("Tool %s result:\n%s", toolCall.Function.Name, string(resultBytes)))
				toolsExecuted = true
			} else {
				// Execute the tool via MCP
				result, err := mcpClient.CallTool(toolCall.Function.Name, args)
				if err != nil {
					// Return error as tool result
					errMsg := fmt.Sprintf("Error: %v", err)
					ollamaMessages = append(ollamaMessages, api.Message{
						Role:    "tool",
						Content: errMsg,
					})
					lastToolResults = append(lastToolResults, fmt.Sprintf("Tool %s failed: %s", toolCall.Function.Name, errMsg))
					continue
				}

				// Format and add tool result
				resultText := formatToolResult(result)
				ollamaMessages = append(ollamaMessages, api.Message{
					Role:    "tool",
					Content: resultText,
				})
				lastToolResults = append(lastToolResults, fmt.Sprintf("Tool %s result:\n%s", toolCall.Function.Name, resultText))
				toolsExecuted = true
			}
		}

		// Continue loop to get next response from Ollama
	}

	// If we hit max iterations but tools were executed, return the last results
	if toolsExecuted && len(lastToolResults) > 0 {
		fmt.Fprintf(os.Stderr, "\n\033[31mNote: Reached maximum iterations. Showing last tool results:\033[0m\n\n")
		// Format JSON responses into readable tables
		return formatJSONResponse(strings.Join(lastToolResults, "\n\n")), nil
	}

	return "", fmt.Errorf("exceeded maximum iterations (%d) in agentic loop", maxIterations)
}
