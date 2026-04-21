/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

func TestHandleHealthCheck(t *testing.T) {
	tools := &mockToolProvider{}
	server := NewServer(tools)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealthCheck(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}

	body := w.Body.String()
	if body == "" {
		t.Error("expected non-empty response body")
	}

	// Parse response
	var response map[string]string
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", response["status"])
	}
	if response["server"] != ServerName {
		t.Errorf("expected server %q, got %q", ServerName, response["server"])
	}
	if response["version"] != ServerVersion {
		t.Errorf("expected version %q, got %q", ServerVersion, response["version"])
	}
}

func TestHandleHTTPRequest_MethodNotAllowed(t *testing.T) {
	tools := &mockToolProvider{}
	server := NewServer(tools)

	// Test GET request (should be rejected)
	req := httptest.NewRequest(http.MethodGet, "/mcp/v1", nil)
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandleHTTPRequest_InvalidJSON(t *testing.T) {
	tools := &mockToolProvider{}
	server := NewServer(tools)

	req := httptest.NewRequest(http.MethodPost, "/mcp/v1",
		bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 (JSON-RPC errors use 200), got %d", w.Code)
	}

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == nil {
		t.Fatal("expected error response")
	}
	if response.Error.Code != -32700 {
		t.Errorf("expected parse error code -32700, got %d", response.Error.Code)
	}
}

func TestHandleInitializeHTTP(t *testing.T) {
	tools := &mockToolProvider{}
	server := NewServer(tools)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0-beta1",
			},
		},
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error != nil {
		t.Fatalf("unexpected error: %v", response.Error)
	}

	result, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result to be a map, got %T", response.Result)
	}

	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatal("expected serverInfo in result")
	}

	if serverInfo["name"] != ServerName {
		t.Errorf("expected server name %q, got %q", ServerName, serverInfo["name"])
	}
}

func TestHandleInitializeHTTP_WithProviders(t *testing.T) {
	tools := &mockToolProvider{}
	server := NewServer(tools)
	server.SetResourceProvider(&mockResourceProvider{})
	server.SetPromptProvider(&mockPromptProvider{})

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	result, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatal("expected result to be a map")
	}

	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatal("expected capabilities in result")
	}

	// Should have all capabilities
	if _, ok := capabilities["tools"]; !ok {
		t.Error("expected tools capability")
	}
	if _, ok := capabilities["resources"]; !ok {
		t.Error("expected resources capability")
	}
	if _, ok := capabilities["prompts"]; !ok {
		t.Error("expected prompts capability")
	}
}

func TestHandleToolsListHTTP(t *testing.T) {
	tools := &mockToolProvider{
		tools: []Tool{
			{Name: "tool1", Description: "First tool"},
			{Name: "tool2", Description: "Second tool"},
		},
	}
	server := NewServer(tools)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error != nil {
		t.Fatalf("unexpected error: %v", response.Error)
	}

	result, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result to be a map, got %T", response.Result)
	}

	toolsList, ok := result["tools"].([]any)
	if !ok {
		t.Fatal("expected tools array in result")
	}

	if len(toolsList) != 2 {
		t.Errorf("expected 2 tools, got %d", len(toolsList))
	}
}

func TestHandleToolCallHTTP_Success(t *testing.T) {
	tools := &mockToolProvider{
		executeFunc: func(ctx context.Context, name string, args map[string]any) (ToolResponse, error) {
			return NewToolSuccess("executed " + name)
		},
	}
	server := NewServer(tools)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      "test_tool",
			"arguments": map[string]any{"key": "value"},
		},
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error != nil {
		t.Fatalf("unexpected error: %v", response.Error)
	}
}

func TestHandleToolCallHTTP_ExecutionError(t *testing.T) {
	tools := &mockToolProvider{
		executeFunc: func(ctx context.Context, name string, args map[string]any) (ToolResponse, error) {
			return ToolResponse{}, errors.New("execution failed")
		},
	}
	server := NewServer(tools)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]any{
			"name": "failing_tool",
		},
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == nil {
		t.Fatal("expected error response")
	}
	if response.Error.Code != -32603 {
		t.Errorf("expected internal error code -32603, got %d", response.Error.Code)
	}
}

func TestHandleResourcesListHTTP_NoProvider(t *testing.T) {
	tools := &mockToolProvider{}
	server := NewServer(tools)
	// No resource provider set

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "resources/list",
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == nil {
		t.Fatal("expected error response")
	}
}

func TestHandleResourcesListHTTP_Success(t *testing.T) {
	tools := &mockToolProvider{}
	resources := &mockResourceProvider{
		resources: []Resource{
			{URI: "pg://schema", Name: "schema"},
		},
	}
	server := NewServer(tools)
	server.SetResourceProvider(resources)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "resources/list",
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error != nil {
		t.Fatalf("unexpected error: %v", response.Error)
	}
}

func TestHandleResourceReadHTTP_Success(t *testing.T) {
	tools := &mockToolProvider{}
	resources := &mockResourceProvider{
		readFunc: func(ctx context.Context, uri string) (ResourceContent, error) {
			return NewResourceSuccess(uri, "application/json", `{"data": "test"}`)
		},
	}
	server := NewServer(tools)
	server.SetResourceProvider(resources)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "resources/read",
		Params: map[string]any{
			"uri": "pg://schema",
		},
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error != nil {
		t.Fatalf("unexpected error: %v", response.Error)
	}
}

func TestHandleResourceReadHTTP_Error(t *testing.T) {
	tools := &mockToolProvider{}
	resources := &mockResourceProvider{
		readFunc: func(ctx context.Context, uri string) (ResourceContent, error) {
			return ResourceContent{}, errors.New("resource not found")
		},
	}
	server := NewServer(tools)
	server.SetResourceProvider(resources)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "resources/read",
		Params: map[string]any{
			"uri": "pg://invalid",
		},
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == nil {
		t.Fatal("expected error response")
	}
}

func TestHandlePromptsListHTTP_NoProvider(t *testing.T) {
	tools := &mockToolProvider{}
	server := NewServer(tools)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "prompts/list",
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == nil {
		t.Fatal("expected error response")
	}
}

func TestHandlePromptsListHTTP_Success(t *testing.T) {
	tools := &mockToolProvider{}
	prompts := &mockPromptProvider{
		prompts: []Prompt{
			{Name: "prompt1", Description: "First prompt"},
		},
	}
	server := NewServer(tools)
	server.SetPromptProvider(prompts)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "prompts/list",
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error != nil {
		t.Fatalf("unexpected error: %v", response.Error)
	}
}

func TestHandlePromptGetHTTP_Success(t *testing.T) {
	tools := &mockToolProvider{}
	prompts := &mockPromptProvider{
		executeFunc: func(name string, args map[string]string) (PromptResult, error) {
			return PromptResult{
				Description: "Test prompt",
				Messages: []PromptMessage{
					{Role: "user", Content: ContentItem{Type: "text", Text: "Hello"}},
				},
			}, nil
		},
	}
	server := NewServer(tools)
	server.SetPromptProvider(prompts)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "prompts/get",
		Params: map[string]any{
			"name":      "test_prompt",
			"arguments": map[string]string{"key": "value"},
		},
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error != nil {
		t.Fatalf("unexpected error: %v", response.Error)
	}
}

func TestHandlePromptGetHTTP_Error(t *testing.T) {
	tools := &mockToolProvider{}
	prompts := &mockPromptProvider{
		executeFunc: func(name string, args map[string]string) (PromptResult, error) {
			return PromptResult{}, errors.New("prompt not found")
		},
	}
	server := NewServer(tools)
	server.SetPromptProvider(prompts)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "prompts/get",
		Params: map[string]any{
			"name": "invalid",
		},
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == nil {
		t.Fatal("expected error response")
	}
}

func TestHandleNotificationsInitialized(t *testing.T) {
	tools := &mockToolProvider{}
	server := NewServer(tools)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "notifications/initialized",
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should return empty response without error
	if response.Error != nil {
		t.Fatalf("unexpected error: %v", response.Error)
	}
}

func TestHandleUnknownMethod(t *testing.T) {
	tools := &mockToolProvider{}
	server := NewServer(tools)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "unknown/method",
	}

	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == nil {
		t.Fatal("expected error response")
	}
	if response.Error.Code != -32601 {
		t.Errorf("expected method not found error -32601, got %d", response.Error.Code)
	}
}

func TestCreateErrorResponse(t *testing.T) {
	tests := []struct {
		name    string
		id      any
		code    int
		message string
		data    any
	}{
		{
			name:    "basic error",
			id:      1,
			code:    -32600,
			message: "Invalid Request",
			data:    nil,
		},
		{
			name:    "error with data",
			id:      "request-1",
			code:    -32603,
			message: "Internal error",
			data:    "additional details",
		},
		{
			name:    "nil id",
			id:      nil,
			code:    -32700,
			message: "Parse error",
			data:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := createErrorResponse(tt.id, tt.code, tt.message, tt.data)

			if resp.JSONRPC != "2.0" {
				t.Errorf("expected jsonrpc '2.0', got %q", resp.JSONRPC)
			}
			if resp.ID != tt.id {
				t.Errorf("expected id %v, got %v", tt.id, resp.ID)
			}
			if resp.Error == nil {
				t.Fatal("expected error to be set")
			}
			if resp.Error.Code != tt.code {
				t.Errorf("expected code %d, got %d", tt.code, resp.Error.Code)
			}
			if resp.Error.Message != tt.message {
				t.Errorf("expected message %q, got %q", tt.message, resp.Error.Message)
			}
		})
	}
}

func TestHTTPConfigStruct(t *testing.T) {
	config := HTTPConfig{
		Addr:      ":8080",
		TLSEnable: true,
		CertFile:  "/path/to/cert.pem",
		KeyFile:   "/path/to/key.pem",
		ChainFile: "/path/to/chain.pem",
		Debug:     true,
	}

	if config.Addr != ":8080" {
		t.Errorf("expected addr ':8080', got %q", config.Addr)
	}
	if !config.TLSEnable {
		t.Error("expected TLSEnable=true")
	}
	if config.CertFile != "/path/to/cert.pem" {
		t.Errorf("expected CertFile '/path/to/cert.pem', got %q", config.CertFile)
	}
	if config.KeyFile != "/path/to/key.pem" {
		t.Errorf("expected KeyFile '/path/to/key.pem', got %q", config.KeyFile)
	}
	if config.ChainFile != "/path/to/chain.pem" {
		t.Errorf("expected ChainFile '/path/to/chain.pem', got %q", config.ChainFile)
	}
	if !config.Debug {
		t.Error("expected Debug=true")
	}
}

func TestRunHTTP_NilConfig(t *testing.T) {
	tools := &mockToolProvider{}
	server := NewServer(tools)

	err := server.RunHTTP(nil)
	if err == nil {
		t.Error("expected error for nil config")
	}
}

// TestValidateCORSOrigin exercises the pure CORS validation helper used by
// RunHTTP. Each case covers one branch: empty (same-origin), explicit URL
// (with and without auth), wildcard with and without auth, and a malformed
// URL. The wildcard+auth case is the fix for issue #81: browsers silently
// reject Access-Control-Allow-Origin: * paired with
// Access-Control-Allow-Credentials: true, so the server must fail fast.
func TestValidateCORSOrigin(t *testing.T) {
	tests := []struct {
		name        string
		origin      string
		authEnabled bool
		wantErr     bool
		wantSubstr  []string // substrings expected in the error message
	}{
		{
			name:        "empty origin with auth",
			origin:      "",
			authEnabled: true,
			wantErr:     false,
		},
		{
			name:        "empty origin without auth",
			origin:      "",
			authEnabled: false,
			wantErr:     false,
		},
		{
			name:        "explicit origin with auth",
			origin:      "https://dba.example.com",
			authEnabled: true,
			wantErr:     false,
		},
		{
			name:        "explicit origin without auth",
			origin:      "https://dba.example.com",
			authEnabled: false,
			wantErr:     false,
		},
		{
			name:        "wildcard with auth enabled rejected",
			origin:      "*",
			authEnabled: true,
			wantErr:     true,
			wantSubstr: []string{
				`cors_origin "*"`,
				"authentication is enabled",
				"Access-Control-Allow-Origin: *",
				"Access-Control-Allow-Credentials: true",
				"Fetch spec",
				"https://dba.example.com",
				"same-origin",
			},
		},
		{
			name:        "wildcard without auth allowed",
			origin:      "*",
			authEnabled: false,
			wantErr:     false,
		},
		{
			name:        "invalid origin rejected",
			origin:      "not a url",
			authEnabled: true,
			wantErr:     true,
			wantSubstr:  []string{"invalid CORS origin"},
		},
		{
			name:        "invalid origin rejected without auth",
			origin:      "not a url",
			authEnabled: false,
			wantErr:     true,
			wantSubstr:  []string{"invalid CORS origin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCORSOrigin(tt.origin, tt.authEnabled)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("validateCORSOrigin(%q, %v): expected error, got nil",
						tt.origin, tt.authEnabled)
				}
				for _, sub := range tt.wantSubstr {
					if !strings.Contains(err.Error(), sub) {
						t.Errorf("error message %q does not contain %q",
							err.Error(), sub)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("validateCORSOrigin(%q, %v): unexpected error: %v",
						tt.origin, tt.authEnabled, err)
				}
			}
		})
	}
}

// TestRunHTTP_WildcardCORSWithAuthRejected verifies that RunHTTP returns an
// error (before binding any listener) when CORSOrigin == "*" and an
// AuthStore is configured. This ensures the fail-fast behavior is wired
// into the startup path, not only the pure helper.
func TestRunHTTP_WildcardCORSWithAuthRejected(t *testing.T) {
	tools := &mockToolProvider{}
	server := NewServer(tools)

	config := &HTTPConfig{
		Addr:       ":0",
		CORSOrigin: "*",
		AuthStore:  &auth.AuthStore{}, // non-nil => auth enabled in this path
	}

	err := server.RunHTTP(config)
	if err == nil {
		t.Fatal("expected RunHTTP to reject wildcard CORS with auth enabled")
	}

	msg := err.Error()
	wantSubstrs := []string{
		`cors_origin "*"`,
		"authentication is enabled",
		"Fetch spec",
	}
	for _, sub := range wantSubstrs {
		if !strings.Contains(msg, sub) {
			t.Errorf("error message %q does not contain %q", msg, sub)
		}
	}
}

// TestRunHTTP_InvalidCORSOriginRejected verifies that RunHTTP still rejects
// a malformed non-wildcard origin before binding any listener, preserving
// the pre-existing validation behavior.
func TestRunHTTP_InvalidCORSOriginRejected(t *testing.T) {
	tools := &mockToolProvider{}
	server := NewServer(tools)

	config := &HTTPConfig{
		Addr:       ":0",
		CORSOrigin: "not a url",
		AuthStore:  &auth.AuthStore{},
	}

	err := server.RunHTTP(config)
	if err == nil {
		t.Fatal("expected RunHTTP to reject invalid CORS origin")
	}
	if !strings.Contains(err.Error(), "invalid CORS origin") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	// Create a simple handler that returns 200 OK
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with security headers middleware (HSTS enabled)
	handler := SecurityHeadersMiddleware(true)(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify all security headers are set
	tests := []struct {
		header   string
		expected string
	}{
		{"Strict-Transport-Security", "max-age=31536000; includeSubDomains"},
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"X-XSS-Protection", "1; mode=block"},
		{"Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self' data:"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			got := w.Header().Get(tt.header)
			if got != tt.expected {
				t.Errorf("expected %s=%q, got %q", tt.header, tt.expected, got)
			}
		})
	}

	// Verify the response status
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestSecurityHeadersMiddleware_PreservesExistingHeaders(t *testing.T) {
	// Create a handler that sets its own headers
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "custom-value")
		w.WriteHeader(http.StatusOK)
	})

	handler := SecurityHeadersMiddleware(false)(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Verify security headers are set
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("expected X-Frame-Options header to be set")
	}

	// Verify inner handler headers are preserved
	if w.Header().Get("Content-Type") != "application/json" {
		t.Error("expected Content-Type header to be preserved")
	}
	if w.Header().Get("X-Custom-Header") != "custom-value" {
		t.Error("expected X-Custom-Header to be preserved")
	}
}
