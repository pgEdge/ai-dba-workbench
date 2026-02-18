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
	"sync"
	"time"

	"github.com/pgedge/ai-workbench/cli/internal/mcp"
)

// ClientVersion is the version of the CLI/chat client
const ClientVersion = "1.0.0-alpha1"

// MCPClient provides an interface for communicating with MCP servers via HTTP
type MCPClient interface {
	// Initialize establishes connection and performs handshake
	Initialize(ctx context.Context) error

	// GetServerInfo returns the server name and version from initialization
	GetServerInfo() (name string, version string)

	// ListTools returns available tools from the server
	ListTools(ctx context.Context) ([]mcp.Tool, error)

	// ListResources returns available resources from the server
	ListResources(ctx context.Context) ([]mcp.Resource, error)

	// ListPrompts returns available prompts from the server
	ListPrompts(ctx context.Context) ([]mcp.Prompt, error)

	// CallTool executes a tool with the given arguments
	CallTool(ctx context.Context, name string, args map[string]interface{}) (mcp.ToolResponse, error)

	// ReadResource reads a resource by URI
	ReadResource(ctx context.Context, uri string) (mcp.ResourceContent, error)

	// GetPrompt executes a prompt with the given arguments
	GetPrompt(ctx context.Context, name string, args map[string]string) (mcp.PromptResult, error)

	// ListConnections returns available database connections from the datastore
	ListConnections(ctx context.Context) ([]ConnectionListItem, error)

	// ListDatabases returns databases on a specific connection
	ListDatabases(ctx context.Context, connectionID int) ([]DatabaseInfo, error)

	// GetCurrentConnection returns the currently selected connection
	GetCurrentConnection(ctx context.Context) (*CurrentConnection, error)

	// SetCurrentConnection selects a connection and optionally a database
	SetCurrentConnection(ctx context.Context, connectionID int, databaseName *string) (*CurrentConnection, error)

	// ClearCurrentConnection clears the current connection selection
	ClearCurrentConnection(ctx context.Context) error

	// Close cleans up resources
	Close() error
}

// ConnectionListItem represents a connection in the list
type ConnectionListItem struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	DatabaseName string `json:"database_name"`
	IsMonitored  bool   `json:"is_monitored"`
}

// DatabaseInfo represents a database on a connection
type DatabaseInfo struct {
	Name     string `json:"name"`
	Owner    string `json:"owner"`
	Encoding string `json:"encoding"`
	Size     string `json:"size"`
}

// CurrentConnection represents the currently selected connection
type CurrentConnection struct {
	ConnectionID int     `json:"connection_id"`
	DatabaseName *string `json:"database_name,omitempty"`
	Host         string  `json:"host"`
	Port         int     `json:"port"`
	Name         string  `json:"name"`
}

// httpClient implements MCPClient for HTTP communication
type httpClient struct {
	url        string
	token      string
	client     *http.Client
	requestID  int
	mu         sync.Mutex
	serverInfo mcp.Implementation
}

// NewHTTPClient creates a new HTTP-based MCP client
func NewHTTPClient(url, token string) MCPClient {
	return &httpClient{
		url:       url,
		token:     token,
		client:    &http.Client{Timeout: 30 * time.Second},
		requestID: 0,
	}
}

func (c *httpClient) Initialize(ctx context.Context) error {
	params := mcp.InitializeParams{
		ProtocolVersion: mcp.ProtocolVersion,
		Capabilities:    map[string]interface{}{},
		ClientInfo: mcp.ClientInfo{
			Name:    "pgedge-nla-cli",
			Version: ClientVersion,
		},
	}

	var result mcp.InitializeResult
	if err := c.sendRequest(ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	// Store server info
	c.serverInfo = result.ServerInfo

	return nil
}

func (c *httpClient) GetServerInfo() (string, string) {
	return c.serverInfo.Name, c.serverInfo.Version
}

func (c *httpClient) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	var result mcp.ToolsListResult
	if err := c.sendRequest(ctx, "tools/list", nil, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (c *httpClient) ListResources(ctx context.Context) ([]mcp.Resource, error) {
	var result mcp.ResourcesListResult
	if err := c.sendRequest(ctx, "resources/list", nil, &result); err != nil {
		return nil, err
	}
	return result.Resources, nil
}

func (c *httpClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (mcp.ToolResponse, error) {
	params := mcp.ToolCallParams{
		Name:      name,
		Arguments: args,
	}

	var result mcp.ToolResponse
	if err := c.sendRequest(ctx, "tools/call", params, &result); err != nil {
		return mcp.ToolResponse{}, err
	}
	return result, nil
}

func (c *httpClient) ReadResource(ctx context.Context, uri string) (mcp.ResourceContent, error) {
	params := mcp.ResourceReadParams{
		URI: uri,
	}

	var result mcp.ResourceContent
	if err := c.sendRequest(ctx, "resources/read", params, &result); err != nil {
		return mcp.ResourceContent{}, err
	}
	return result, nil
}

func (c *httpClient) ListPrompts(ctx context.Context) ([]mcp.Prompt, error) {
	var result mcp.PromptsListResult
	if err := c.sendRequest(ctx, "prompts/list", nil, &result); err != nil {
		return nil, err
	}
	return result.Prompts, nil
}

func (c *httpClient) GetPrompt(ctx context.Context, name string, args map[string]string) (mcp.PromptResult, error) {
	params := mcp.PromptGetParams{
		Name:      name,
		Arguments: args,
	}

	var result mcp.PromptResult
	if err := c.sendRequest(ctx, "prompts/get", params, &result); err != nil {
		return mcp.PromptResult{}, err
	}
	return result, nil
}

func (c *httpClient) Close() error {
	return nil
}

func (c *httpClient) sendRequest(ctx context.Context, method string, params interface{}, result interface{}) error {
	c.mu.Lock()
	c.requestID++
	id := c.requestID
	c.mu.Unlock()

	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewBuffer(reqData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("HTTP error %d (failed to read body: %w)", resp.StatusCode, err)
		}
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	var jsonResp mcp.JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if jsonResp.Error != nil {
		if jsonResp.Error.Data != nil {
			return fmt.Errorf("RPC error %d: %s: %v", jsonResp.Error.Code, jsonResp.Error.Message, jsonResp.Error.Data)
		}
		return fmt.Errorf("RPC error %d: %s", jsonResp.Error.Code, jsonResp.Error.Message)
	}

	// Marshal and unmarshal to convert to target type
	resultData, err := json.Marshal(jsonResp.Result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	if err := json.Unmarshal(resultData, result); err != nil {
		return fmt.Errorf("failed to unmarshal result: %w", err)
	}

	return nil
}

// getBaseURL extracts the base URL from the MCP endpoint URL
func (c *httpClient) getBaseURL() string {
	// The MCP URL is typically like "http://localhost:8080/mcp/v1"
	// We need "http://localhost:8080" for REST API calls
	url := c.url
	// Remove /mcp/v1 suffix if present
	if len(url) > 7 && url[len(url)-7:] == "/mcp/v1" {
		url = url[:len(url)-7]
	} else if len(url) > 4 && url[len(url)-4:] == "/mcp" {
		// Also handle /mcp suffix for backwards compatibility
		url = url[:len(url)-4]
	}
	return url
}

// sendRESTRequest sends a REST API request (not JSON-RPC)
func (c *httpClient) sendRESTRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	baseURL := c.getBaseURL()
	fullURL := baseURL + path

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(data)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Handle 204 No Content
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	// Handle 404 Not Found specially for GetCurrentConnection
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("not found")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

func (c *httpClient) ListConnections(ctx context.Context) ([]ConnectionListItem, error) {
	var result []ConnectionListItem
	if err := c.sendRESTRequest(ctx, "GET", "/api/v1/connections", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *httpClient) ListDatabases(ctx context.Context, connectionID int) ([]DatabaseInfo, error) {
	var result []DatabaseInfo
	path := fmt.Sprintf("/api/v1/connections/%d/databases", connectionID)
	if err := c.sendRESTRequest(ctx, "GET", path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *httpClient) GetCurrentConnection(ctx context.Context) (*CurrentConnection, error) {
	var result CurrentConnection
	if err := c.sendRESTRequest(ctx, "GET", "/api/v1/connections/current", nil, &result); err != nil {
		if err.Error() == "not found" {
			return nil, nil // No connection selected
		}
		return nil, err
	}
	return &result, nil
}

func (c *httpClient) SetCurrentConnection(ctx context.Context, connectionID int, databaseName *string) (*CurrentConnection, error) {
	body := map[string]interface{}{
		"connection_id": connectionID,
	}
	if databaseName != nil {
		body["database_name"] = *databaseName
	}

	var result CurrentConnection
	if err := c.sendRESTRequest(ctx, "POST", "/api/v1/connections/current", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *httpClient) ClearCurrentConnection(ctx context.Context) error {
	return c.sendRESTRequest(ctx, "DELETE", "/api/v1/connections/current", nil, nil)
}
