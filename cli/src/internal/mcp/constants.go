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

// Re-export constants from the shared MCP package

// ProtocolVersion is the MCP protocol version
const ProtocolVersion = pkgmcp.ProtocolVersion

// Scanner buffer size constants for JSON-RPC message processing
const (
	// ScannerInitialBufferSize is the initial buffer size (64KB)
	// This should be large enough for most MCP messages
	ScannerInitialBufferSize = pkgmcp.ScannerInitialBufferSize

	// ScannerMaxBufferSize is the maximum buffer size (1MB)
	// This prevents unbounded memory growth from malicious or malformed messages
	ScannerMaxBufferSize = pkgmcp.ScannerMaxBufferSize
)
