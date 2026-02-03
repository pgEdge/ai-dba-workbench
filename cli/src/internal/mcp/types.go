/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package mcp provides MCP protocol types and helpers for the CLI.
// This package re-exports types from the shared pkg/mcp package.
package mcp

import (
	pkgmcp "github.com/pgedge/ai-workbench/pkg/mcp"
)

// Re-export all types from the shared MCP package

// JSONRPCRequest represents an incoming JSON-RPC 2.0 request
type JSONRPCRequest = pkgmcp.JSONRPCRequest

// JSONRPCResponse represents an outgoing JSON-RPC 2.0 response
type JSONRPCResponse = pkgmcp.JSONRPCResponse

// RPCError represents a JSON-RPC error
type RPCError = pkgmcp.RPCError

// InitializeParams represents the parameters for the initialize request
type InitializeParams = pkgmcp.InitializeParams

// ClientInfo contains information about the MCP client
type ClientInfo = pkgmcp.ClientInfo

// Implementation contains server implementation details
type Implementation = pkgmcp.Implementation

// InitializeResult is the response to an initialize request
type InitializeResult = pkgmcp.InitializeResult

// Tool represents an MCP tool definition
type Tool = pkgmcp.Tool

// InputSchema defines the JSON schema for tool input
type InputSchema = pkgmcp.InputSchema

// ToolCallParams represents parameters for calling a tool
type ToolCallParams = pkgmcp.ToolCallParams

// ToolResponse represents the response from a tool execution
type ToolResponse = pkgmcp.ToolResponse

// ContentItem represents a piece of content in a tool response
type ContentItem = pkgmcp.ContentItem

// Resource represents an MCP resource definition
type Resource = pkgmcp.Resource

// ResourceReadParams represents parameters for reading a resource
type ResourceReadParams = pkgmcp.ResourceReadParams

// ResourceContent represents the content of a resource
type ResourceContent = pkgmcp.ResourceContent

// ToolsListResult represents the result of tools/list request
type ToolsListResult = pkgmcp.ToolsListResult

// ResourcesListResult represents the result of resources/list request
type ResourcesListResult = pkgmcp.ResourcesListResult

// Prompt represents an MCP prompt definition
type Prompt = pkgmcp.Prompt

// PromptArgument represents an argument for a prompt
type PromptArgument = pkgmcp.PromptArgument

// PromptGetParams represents parameters for getting a prompt
type PromptGetParams = pkgmcp.PromptGetParams

// PromptResult represents the result of getting a prompt
type PromptResult = pkgmcp.PromptResult

// PromptMessage represents a message in a prompt template
type PromptMessage = pkgmcp.PromptMessage

// PromptsListResult represents the result of prompts/list request
type PromptsListResult = pkgmcp.PromptsListResult
