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
	pkgmcp "github.com/pgedge/ai-workbench/pkg/mcp"
)

// Re-export helper functions from the shared MCP package

// NewToolError creates a standardized error response for tools
func NewToolError(message string) (ToolResponse, error) {
	return pkgmcp.NewToolError(message)
}

// NewToolSuccess creates a standardized success response for tools
func NewToolSuccess(message string) (ToolResponse, error) {
	return pkgmcp.NewToolSuccess(message)
}

// NewResourceError creates a standardized error response for resources
func NewResourceError(uri string, message string) (ResourceContent, error) {
	return pkgmcp.NewResourceError(uri, message)
}

// NewResourceSuccess creates a standardized success response for resources
func NewResourceSuccess(uri string, mimeType string, content string) (ResourceContent, error) {
	return pkgmcp.NewResourceSuccess(uri, mimeType, content)
}

// Re-export error constants from the shared MCP package

// DatabaseNotReadyError is a standard error message for when database is still initializing
const DatabaseNotReadyError = pkgmcp.DatabaseNotReadyError

// DatabaseNotReadyErrorShort is a shorter version for resources
const DatabaseNotReadyErrorShort = pkgmcp.DatabaseNotReadyErrorShort
