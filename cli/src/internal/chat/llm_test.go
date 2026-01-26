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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgedge/ai-workbench/cli/internal/mcp"
)

func TestProxyClient_TextResponse(t *testing.T) {
	// Create test server that mimics the LLM proxy endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request path
		if r.URL.Path != "/api/v1/llm/chat" {
			t.Errorf("Expected path '/api/v1/llm/chat', got '%s'", r.URL.Path)
		}

		// Verify method
		if r.Method != "POST" {
			t.Errorf("Expected POST method, got '%s'", r.Method)
		}

		// Verify auth header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Expected Authorization 'Bearer test-token', got '%s'", auth)
		}

		// Verify request body
		var req proxyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if req.Provider != "anthropic" {
			t.Errorf("Expected provider 'anthropic', got '%s'", req.Provider)
		}
		if req.Model != "claude-test" {
			t.Errorf("Expected model 'claude-test', got '%s'", req.Model)
		}

		// Send response
		resp := proxyResponse{
			Content: []map[string]interface{}{
				{
					"type": "text",
					"text": "This is a test response",
				},
			},
			StopReason: "end_turn",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create proxy client
	client := NewProxyClient(server.URL, "test-token", "anthropic", "claude-test", false)

	// Test chat
	ctx := context.Background()
	messages := []Message{
		{
			Role:    "user",
			Content: "Hello",
		},
	}

	response, err := client.Chat(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if response.StopReason != "end_turn" {
		t.Errorf("Expected stop reason 'end_turn', got '%s'", response.StopReason)
	}

	if len(response.Content) != 1 {
		t.Fatalf("Expected 1 content item, got %d", len(response.Content))
	}

	textContent, ok := response.Content[0].(TextContent)
	if !ok {
		t.Fatalf("Expected TextContent, got %T", response.Content[0])
	}

	if textContent.Text != "This is a test response" {
		t.Errorf("Expected text 'This is a test response', got '%s'", textContent.Text)
	}
}

func TestProxyClient_ToolUseResponse(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request body has tools
		var req proxyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if req.Tools == nil {
			t.Error("Expected tools in request")
		}

		// Send tool use response
		resp := proxyResponse{
			Content: []map[string]interface{}{
				{
					"type": "text",
					"text": "I'll help you with that",
				},
				{
					"type":  "tool_use",
					"id":    "tool_1",
					"name":  "test_tool",
					"input": map[string]interface{}{"param": "value"},
				},
			},
			StopReason: "tool_use",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create proxy client
	client := NewProxyClient(server.URL, "test-token", "anthropic", "claude-test", false)

	// Test chat with tools
	ctx := context.Background()
	messages := []Message{
		{
			Role:    "user",
			Content: "Execute test tool",
		},
	}
	tools := []mcp.Tool{
		{
			Name:        "test_tool",
			Description: "A test tool",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"param": map[string]interface{}{
						"type":        "string",
						"description": "A parameter",
					},
				},
			},
		},
	}

	response, err := client.Chat(ctx, messages, tools)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if response.StopReason != "tool_use" {
		t.Errorf("Expected stop reason 'tool_use', got '%s'", response.StopReason)
	}

	if len(response.Content) != 2 {
		t.Fatalf("Expected 2 content items, got %d", len(response.Content))
	}

	// Check text content
	textContent, ok := response.Content[0].(TextContent)
	if !ok {
		t.Fatalf("Expected TextContent, got %T", response.Content[0])
	}
	if textContent.Text != "I'll help you with that" {
		t.Errorf("Expected text 'I'll help you with that', got '%s'", textContent.Text)
	}

	// Check tool use
	toolUse, ok := response.Content[1].(ToolUse)
	if !ok {
		t.Fatalf("Expected ToolUse, got %T", response.Content[1])
	}
	if toolUse.Name != "test_tool" {
		t.Errorf("Expected tool name 'test_tool', got '%s'", toolUse.Name)
	}
}

func TestProxyClient_ErrorResponse(t *testing.T) {
	// Create test server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Invalid request"))
	}))
	defer server.Close()

	// Create proxy client
	client := NewProxyClient(server.URL, "test-token", "anthropic", "claude-test", false)

	// Test chat
	ctx := context.Background()
	messages := []Message{
		{
			Role:    "user",
			Content: "Hello",
		},
	}

	_, err := client.Chat(ctx, messages, nil)
	if err == nil {
		t.Error("Expected error for bad response")
	}
}

func TestProxyClient_ListModels(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request path
		if r.URL.Path != "/api/v1/llm/models" {
			t.Errorf("Expected path '/api/v1/llm/models', got '%s'", r.URL.Path)
		}

		// Verify query parameter
		provider := r.URL.Query().Get("provider")
		if provider != "anthropic" {
			t.Errorf("Expected provider query param 'anthropic', got '%s'", provider)
		}

		// Send response
		resp := struct {
			Models []struct {
				Name        string `json:"name"`
				Description string `json:"description,omitempty"`
			} `json:"models"`
		}{
			Models: []struct {
				Name        string `json:"name"`
				Description string `json:"description,omitempty"`
			}{
				{Name: "claude-sonnet-4-5-20250929"},
				{Name: "claude-3-haiku-20240307"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create proxy client
	client := NewProxyClient(server.URL, "test-token", "anthropic", "claude-test", false)

	// Test list models
	ctx := context.Background()
	models, err := client.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("Expected 2 models, got %d", len(models))
	}

	if models[0] != "claude-sonnet-4-5-20250929" {
		t.Errorf("Expected first model 'claude-sonnet-4-5-20250929', got '%s'", models[0])
	}
}

func TestProxyClient_URLNormalization(t *testing.T) {
	tests := []struct {
		name        string
		inputURL    string
		expectedURL string
	}{
		{
			name:        "Plain URL",
			inputURL:    "http://localhost:8080",
			expectedURL: "http://localhost:8080",
		},
		{
			name:        "URL with trailing slash",
			inputURL:    "http://localhost:8080/",
			expectedURL: "http://localhost:8080",
		},
		{
			name:        "URL with /mcp/v1 suffix",
			inputURL:    "http://localhost:8080/mcp/v1",
			expectedURL: "http://localhost:8080",
		},
		{
			name:        "URL with /mcp/v1 and trailing slash",
			inputURL:    "http://localhost:8080/mcp/v1/",
			expectedURL: "http://localhost:8080/mcp/v1", // Only removes trailing slash
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewProxyClient(tt.inputURL, "token", "anthropic", "model", false)
			proxyC := client.(*proxyClient)
			if proxyC.baseURL != tt.expectedURL {
				t.Errorf("Expected baseURL '%s', got '%s'", tt.expectedURL, proxyC.baseURL)
			}
		})
	}
}

func TestProxyClient_TokenUsage(t *testing.T) {
	// Create test server that returns token usage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := proxyResponse{
			Content: []map[string]interface{}{
				{
					"type": "text",
					"text": "Response with token usage",
				},
			},
			StopReason: "end_turn",
			TokenUsage: &TokenUsage{
				Provider:         "anthropic",
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create proxy client
	client := NewProxyClient(server.URL, "test-token", "anthropic", "claude-test", false)

	// Test chat
	ctx := context.Background()
	messages := []Message{
		{
			Role:    "user",
			Content: "Hello",
		},
	}

	response, err := client.Chat(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if response.TokenUsage == nil {
		t.Fatal("Expected token usage in response")
	}

	if response.TokenUsage.PromptTokens != 100 {
		t.Errorf("Expected prompt tokens 100, got %d", response.TokenUsage.PromptTokens)
	}
	if response.TokenUsage.CompletionTokens != 50 {
		t.Errorf("Expected completion tokens 50, got %d", response.TokenUsage.CompletionTokens)
	}
	if response.TokenUsage.TotalTokens != 150 {
		t.Errorf("Expected total tokens 150, got %d", response.TokenUsage.TotalTokens)
	}
}
