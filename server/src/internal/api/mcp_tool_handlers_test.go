/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// mockToolProvider implements ContextAwareToolProvider for testing.
type mockToolProvider struct {
	tools      []mcp.Tool
	response   mcp.ToolResponse
	executeErr error
	lastCtx    context.Context
	lastName   string
	lastArgs   map[string]any
}

func (m *mockToolProvider) ListForContext(ctx context.Context) []mcp.Tool {
	m.lastCtx = ctx
	return m.tools
}

func (m *mockToolProvider) Execute(ctx context.Context, name string, args map[string]any) (mcp.ToolResponse, error) {
	m.lastCtx = ctx
	m.lastName = name
	m.lastArgs = args
	return m.response, m.executeErr
}

func TestNewMCPToolHandler(t *testing.T) {
	provider := &mockToolProvider{}
	handler := NewMCPToolHandler(provider)
	if handler == nil {
		t.Fatal("NewMCPToolHandler returned nil")
	}
	if handler.provider != provider {
		t.Error("Expected provider to be set")
	}
}

func TestMCPToolHandler_ListTools(t *testing.T) {
	expectedTools := []mcp.Tool{
		{
			Name:        "query_database",
			Description: "Execute a SQL query",
			InputSchema: mcp.InputSchema{
				Type:       "object",
				Properties: map[string]any{"query": map[string]any{"type": "string"}},
				Required:   []string{"query"},
			},
		},
		{
			Name:        "get_schema_info",
			Description: "Get database schema information",
			InputSchema: mcp.InputSchema{
				Type:       "object",
				Properties: map[string]any{},
			},
		},
	}

	provider := &mockToolProvider{tools: expectedTools}
	handler := NewMCPToolHandler(provider)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/tools", nil)
	rec := httptest.NewRecorder()

	handler.handleListTools(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp toolListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(resp.Tools))
	}

	if resp.Tools[0].Name != "query_database" {
		t.Errorf("Expected first tool name 'query_database', got %q", resp.Tools[0].Name)
	}
}

func TestMCPToolHandler_ListTools_EmptyList(t *testing.T) {
	provider := &mockToolProvider{tools: nil}
	handler := NewMCPToolHandler(provider)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/tools", nil)
	rec := httptest.NewRecorder()

	handler.handleListTools(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp toolListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Tools == nil {
		t.Error("Expected empty slice, got nil")
	}
	if len(resp.Tools) != 0 {
		t.Errorf("Expected 0 tools, got %d", len(resp.Tools))
	}
}

func TestMCPToolHandler_ListTools_MethodNotAllowed(t *testing.T) {
	provider := &mockToolProvider{}
	handler := NewMCPToolHandler(provider)

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/v1/mcp/tools", nil)
			rec := httptest.NewRecorder()

			handler.handleListTools(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d for %s, got %d",
					http.StatusMethodNotAllowed, method, rec.Code)
			}
		})
	}
}

func TestMCPToolHandler_CallTool(t *testing.T) {
	expectedResponse := mcp.ToolResponse{
		Content: []mcp.ContentItem{
			{Type: "text", Text: "Query executed successfully"},
		},
	}

	provider := &mockToolProvider{response: expectedResponse}
	handler := NewMCPToolHandler(provider)

	body := toolCallRequest{
		Name:      "query_database",
		Arguments: map[string]any{"query": "SELECT 1"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/tools/call",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.handleCallTool(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if provider.lastName != "query_database" {
		t.Errorf("Expected tool name 'query_database', got %q", provider.lastName)
	}

	var resp mcp.ToolResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Content) != 1 {
		t.Fatalf("Expected 1 content item, got %d", len(resp.Content))
	}

	if resp.Content[0].Text != "Query executed successfully" {
		t.Errorf("Expected content text 'Query executed successfully', got %q",
			resp.Content[0].Text)
	}
}

func TestMCPToolHandler_CallTool_ErrorResponse(t *testing.T) {
	errorResponse := mcp.ToolResponse{
		Content: []mcp.ContentItem{
			{Type: "text", Text: "Access denied"},
		},
		IsError: true,
	}

	provider := &mockToolProvider{response: errorResponse}
	handler := NewMCPToolHandler(provider)

	body := toolCallRequest{
		Name:      "query_database",
		Arguments: map[string]any{"query": "DROP TABLE users"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/tools/call",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.handleCallTool(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d (tool errors are returned in response body)",
			http.StatusOK, rec.Code)
	}

	var resp mcp.ToolResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !resp.IsError {
		t.Error("Expected IsError to be true")
	}
}

func TestMCPToolHandler_CallTool_ExecutionError(t *testing.T) {
	provider := &mockToolProvider{
		executeErr: errors.New("internal provider error"),
	}
	handler := NewMCPToolHandler(provider)

	body := toolCallRequest{
		Name:      "query_database",
		Arguments: map[string]any{"query": "SELECT 1"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/tools/call",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.handleCallTool(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d",
			http.StatusInternalServerError, rec.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}

	if resp.Error != "Tool execution failed" {
		t.Errorf("Expected error 'Tool execution failed', got %q", resp.Error)
	}
}

func TestMCPToolHandler_CallTool_MissingName(t *testing.T) {
	provider := &mockToolProvider{}
	handler := NewMCPToolHandler(provider)

	body := toolCallRequest{
		Name:      "",
		Arguments: map[string]any{"query": "SELECT 1"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/tools/call",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.handleCallTool(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}

	if resp.Error != "Tool name is required" {
		t.Errorf("Expected error 'Tool name is required', got %q", resp.Error)
	}
}

func TestMCPToolHandler_CallTool_InvalidBody(t *testing.T) {
	provider := &mockToolProvider{}
	handler := NewMCPToolHandler(provider)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/tools/call",
		bytes.NewReader([]byte("not valid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.handleCallTool(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestMCPToolHandler_CallTool_MethodNotAllowed(t *testing.T) {
	provider := &mockToolProvider{}
	handler := NewMCPToolHandler(provider)

	methods := []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/v1/mcp/tools/call", nil)
			rec := httptest.NewRecorder()

			handler.handleCallTool(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d for %s, got %d",
					http.StatusMethodNotAllowed, method, rec.Code)
			}
		})
	}
}

func TestMCPToolHandler_CallTool_NilArguments(t *testing.T) {
	expectedResponse := mcp.ToolResponse{
		Content: []mcp.ContentItem{
			{Type: "text", Text: "Tool ran with no args"},
		},
	}

	provider := &mockToolProvider{response: expectedResponse}
	handler := NewMCPToolHandler(provider)

	body := toolCallRequest{
		Name: "list_probes",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/tools/call",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.handleCallTool(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if provider.lastName != "list_probes" {
		t.Errorf("Expected tool name 'list_probes', got %q", provider.lastName)
	}
}

func TestMCPToolHandler_RegisterRoutes(t *testing.T) {
	provider := &mockToolProvider{}
	handler := NewMCPToolHandler(provider)

	mux := http.NewServeMux()
	passthrough := func(h http.HandlerFunc) http.HandlerFunc { return h }

	// Verify RegisterRoutes does not panic
	handler.RegisterRoutes(mux, passthrough)

	// Verify the routes are registered by making test requests
	tests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{"list tools", http.MethodGet, "/api/v1/mcp/tools", http.StatusOK},
		{"call tool missing body", http.MethodPost, "/api/v1/mcp/tools/call", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.status {
				t.Errorf("Expected status %d, got %d", tt.status, rec.Code)
			}
		})
	}
}
