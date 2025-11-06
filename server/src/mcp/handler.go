/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/pgEdge/ai-workbench/server/src/logger"
)

// Handler processes MCP requests
type Handler struct {
	serverName    string
	serverVersion string
	initialized   bool
}

// NewHandler creates a new MCP handler
func NewHandler(serverName, serverVersion string) *Handler {
	return &Handler{
		serverName:    serverName,
		serverVersion: serverVersion,
		initialized:   false,
	}
}

// HandleRequest processes an MCP request and returns a response
func (h *Handler) HandleRequest(data []byte) (*Response, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		logger.Errorf("Failed to unmarshal request: %v", err)
		return NewErrorResponse(nil, ParseError, "Parse error", err.Error()),
			nil
	}

	logger.Infof("Handling MCP request: method=%s, id=%v", req.Method,
		req.ID)

	// Validate JSON-RPC version
	if req.JSONRPC != JSONRPCVersion {
		return NewErrorResponse(req.ID, InvalidRequest,
			"Invalid JSON-RPC version", nil), nil
	}

	// Route to appropriate handler based on method
	switch req.Method {
	case "initialize":
		return h.handleInitialize(req)
	case "ping":
		return h.handlePing(req)
	default:
		logger.Errorf("Method not found: %s", req.Method)
		return NewErrorResponse(req.ID, MethodNotFound, "Method not found",
			nil), nil
	}
}

// handleInitialize processes the initialize request
func (h *Handler) handleInitialize(req Request) (*Response, error) {
	var params InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		logger.Errorf("Failed to unmarshal initialize params: %v", err)
		return NewErrorResponse(req.ID, InvalidParams, "Invalid parameters",
			err.Error()), nil
	}

	logger.Infof("Initialize request from client: %s v%s",
		params.ClientInfo.Name, params.ClientInfo.Version)

	// Mark as initialized
	h.initialized = true

	// Build response with server capabilities (empty for now, no tools/resources)
	result := InitializeResult{
		ProtocolVersion: "2024-11-05", // MCP protocol version
		Capabilities:    make(map[string]interface{}),
		ServerInfo: ServerInfo{
			Name:    h.serverName,
			Version: h.serverVersion,
		},
	}

	logger.Info("Server initialized successfully")
	return NewResponse(req.ID, result), nil
}

// handlePing processes the ping request
func (h *Handler) handlePing(req Request) (*Response, error) {
	result := map[string]interface{}{
		"status": "ok",
	}
	return NewResponse(req.ID, result), nil
}

// IsInitialized returns whether the handler has been initialized
func (h *Handler) IsInitialized() bool {
	return h.initialized
}

// FormatResponse formats a response as JSON
func FormatResponse(resp *Response) ([]byte, error) {
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return data, nil
}
