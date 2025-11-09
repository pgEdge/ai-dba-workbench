/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
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
	Chat(ctx context.Context, messages []Message, tools []Tool, resources []Resource) (string, error)
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
		AnthropicKey:   os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicModel: os.Getenv("ANTHROPIC_MODEL"),
		OllamaURL:      os.Getenv("OLLAMA_URL"),
		OllamaModel:    os.Getenv("OLLAMA_MODEL"),
	}

	// Set defaults
	if config.AnthropicModel == "" {
		config.AnthropicModel = "claude-sonnet-4-5"
	}
	if config.OllamaURL == "" {
		config.OllamaURL = "http://localhost:11434"
	}
	if config.OllamaModel == "" {
		config.OllamaModel = "llama2"
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
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is required")
	}

	return &AnthropicClient{
		apiKey: config.AnthropicKey,
		model:  config.AnthropicModel,
	}, nil
}

// Chat implements the LLMClient interface for Anthropic
func (a *AnthropicClient) Chat(ctx context.Context, messages []Message, tools []Tool, resources []Resource) (string, error) {
	userMessage := messages[len(messages)-1].Content

	// Build system message with tool and resource information
	systemMessage := ""
	if len(tools) > 0 || len(resources) > 0 {
		var parts []string

		if len(tools) > 0 {
			toolsJSON, err := json.MarshalIndent(tools, "", "  ")
			if err == nil {
				parts = append(parts, fmt.Sprintf("MCP Tools:\n%s", string(toolsJSON)))
			}
		}

		if len(resources) > 0 {
			resourcesJSON, err := json.MarshalIndent(resources, "", "  ")
			if err == nil {
				parts = append(parts, fmt.Sprintf("MCP Resources:\n%s", string(resourcesJSON)))
			}
		}

		if len(parts) > 0 {
			systemMessage = fmt.Sprintf(`You are an AI assistant with access to the following MCP capabilities:

%s

Note: Direct tool execution is not yet implemented in this basic version. Describe what you would do to help the user.`, strings.Join(parts, "\n\n"))
		}
	}

	// Create request body
	requestBody := map[string]interface{}{
		"model":      a.model,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": userMessage,
			},
		},
	}

	if systemMessage != "" {
		requestBody["system"] = systemMessage
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
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract text
	var result string
	for _, block := range response.Content {
		if block.Type == "text" {
			result += block.Text
		}
	}

	return result, nil
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

// Chat implements the LLMClient interface for Ollama
func (o *OllamaClient) Chat(ctx context.Context, messages []Message, tools []Tool, resources []Resource) (string, error) {
	// Convert messages to Ollama format
	var ollamaMessages []api.Message
	for _, msg := range messages {
		ollamaMessages = append(ollamaMessages, api.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Build system prompt with tools and resources
	if len(tools) > 0 || len(resources) > 0 {
		var parts []string

		if len(tools) > 0 {
			toolsBytes, err := json.MarshalIndent(tools, "", "  ")
			if err == nil {
				parts = append(parts, fmt.Sprintf("MCP Tools:\n%s", string(toolsBytes)))
			}
		}

		if len(resources) > 0 {
			resourcesBytes, err := json.MarshalIndent(resources, "", "  ")
			if err == nil {
				parts = append(parts, fmt.Sprintf("MCP Resources:\n%s", string(resourcesBytes)))
			}
		}

		if len(parts) > 0 {
			systemPrompt := fmt.Sprintf(`You are an AI assistant with access to the following MCP capabilities:

%s

When you need to use a tool, respond with a JSON object in this format:
{"tool": "tool_name", "arguments": {"arg1": "value1", "arg2": "value2"}}

The tool results will be provided back to you.`, strings.Join(parts, "\n\n"))

			ollamaMessages = append([]api.Message{{
				Role:    "system",
				Content: systemPrompt,
			}}, ollamaMessages...)
		}
	}

	// Create the chat request
	req := &api.ChatRequest{
		Model:    o.model,
		Messages: ollamaMessages,
	}

	// Send the chat request
	var response string
	err := o.client.Chat(ctx, req, func(resp api.ChatResponse) error {
		response += resp.Message.Content
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to chat with Ollama: %w", err)
	}

	return response, nil
}
