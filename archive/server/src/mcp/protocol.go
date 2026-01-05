/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package mcp implements the Model Context Protocol for the MCP server
package mcp

import (
	"encoding/json"
)

// JSONRPCVersion is the JSON-RPC version used by MCP
const JSONRPCVersion = "2.0"

// Request represents an MCP JSON-RPC request
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents an MCP JSON-RPC response
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

// Error represents an MCP JSON-RPC error
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error codes as defined by JSON-RPC 2.0 specification
const (
	ParseError     = -32700 // Invalid JSON was received
	InvalidRequest = -32600 // The JSON sent is not a valid Request object
	MethodNotFound = -32601 // The method does not exist or is not available
	InvalidParams  = -32602 // Invalid method parameter(s)
	InternalError  = -32603 // Internal JSON-RPC error
)

// NewRequest creates a new MCP request
func NewRequest(method string, params interface{}) (*Request, error) {
	var paramsJSON json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		paramsJSON = data
	}

	return &Request{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  paramsJSON,
	}, nil
}

// NewResponse creates a new MCP success response
func NewResponse(id interface{}, result interface{}) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  result,
	}
}

// NewErrorResponse creates a new MCP error response
func NewErrorResponse(id interface{}, code int, message string,
	data interface{}) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// ServerInfo represents information about the MCP server
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeParams represents the parameters for the initialize method
type InitializeParams struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	ClientInfo      ClientInfo             `json:"clientInfo"`
}

// ClientInfo represents information about the MCP client
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult represents the result of the initialize method
type InitializeResult struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	ServerInfo      ServerInfo             `json:"serverInfo"`
}
