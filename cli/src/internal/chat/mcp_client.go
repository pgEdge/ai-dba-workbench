/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
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

	"github.com/pgedge/ai-workbench/cli/internal/mcp"
)

// ClientVersion is the version of the CLI/chat client
const ClientVersion = "1.0.0-beta1"

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

	// Close cleans up resources
	Close() error
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
		client:    &http.Client{},
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
