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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// MCPClient represents a client for interacting with an MCP server
type MCPClient struct {
	serverURL   string
	httpClient  *http.Client
	requestID   int
	bearerToken string
}

// MCPRequest represents an MCP JSON-RPC request
type MCPRequest struct {
	JSONRPCVersion string      `json:"jsonrpc"`
	ID             int         `json:"id"`
	Method         string      `json:"method"`
	Params         interface{} `json:"params,omitempty"`
}

// MCPResponse represents an MCP JSON-RPC response
type MCPResponse struct {
	JSONRPCVersion string          `json:"jsonrpc"`
	ID             int             `json:"id"`
	Result         json.RawMessage `json:"result,omitempty"`
	Error          *MCPError       `json:"error,omitempty"`
}

// MCPError represents an MCP error
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// NewMCPClient creates a new MCP client
func NewMCPClient(serverURL string) *MCPClient {
	return &MCPClient{
		serverURL:   serverURL,
		httpClient:  &http.Client{},
		requestID:   1,
		bearerToken: "",
	}
}

// SetBearerToken sets the bearer token for authentication
func (c *MCPClient) SetBearerToken(token string) {
	c.bearerToken = token
}

// CallTool calls an MCP tool
func (c *MCPClient) CallTool(toolName string, arguments map[string]interface{}) (interface{}, error) {
	params := map[string]interface{}{
		"name":      toolName,
		"arguments": arguments,
	}

	return c.makeRequest("tools/call", params)
}

// ReadResource reads an MCP resource
func (c *MCPClient) ReadResource(uri string) (interface{}, error) {
	params := map[string]interface{}{
		"uri": uri,
	}

	return c.makeRequest("resources/read", params)
}

// Ping pings the MCP server
func (c *MCPClient) Ping() (interface{}, error) {
	return c.makeRequest("ping", nil)
}

// ListResources lists available MCP resources
func (c *MCPClient) ListResources() (interface{}, error) {
	return c.makeRequest("resources/list", nil)
}

// ListTools lists available MCP tools
func (c *MCPClient) ListTools() (interface{}, error) {
	return c.makeRequest("tools/list", nil)
}

// ListPrompts lists available MCP prompts
func (c *MCPClient) ListPrompts() (interface{}, error) {
	return c.makeRequest("prompts/list", nil)
}

// makeRequest makes an MCP JSON-RPC request
func (c *MCPClient) makeRequest(method string, params interface{}) (interface{}, error) {
	// Create request
	req := MCPRequest{
		JSONRPCVersion: "2.0",
		ID:             c.requestID,
		Method:         method,
		Params:         params,
	}
	c.requestID++

	// Marshal request
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	httpReq, err := http.NewRequest("POST", c.serverURL+"/mcp", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Add Authorization header if bearer token is set
	if c.bearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse MCP response
	var mcpResp MCPResponse
	if err := json.Unmarshal(respBody, &mcpResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for MCP error
	if mcpResp.Error != nil {
		// Include data field if present, as it contains the actual error details
		if mcpResp.Error.Data != nil {
			return nil, fmt.Errorf("MCP error %d: %s - %v", mcpResp.Error.Code, mcpResp.Error.Message, mcpResp.Error.Data)
		}
		return nil, fmt.Errorf("MCP error %d: %s", mcpResp.Error.Code, mcpResp.Error.Message)
	}

	// Parse result
	var result interface{}
	if err := json.Unmarshal(mcpResp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return result, nil
}
