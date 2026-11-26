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

import "slices"

// ProtocolVersion is the latest MCP protocol revision the server
// supports, and the one it offers a client asking for any other.
const ProtocolVersion = "2025-11-25"

// supportedProtocolVersions lists the handshake-based revisions the
// server implements, newest first.
var supportedProtocolVersions = []string{
	ProtocolVersion,
	"2025-06-18",
	"2025-03-26",
	"2024-11-05",
}

// SupportedProtocolVersions returns the supported protocol revisions,
// newest first.
func SupportedProtocolVersions() []string {
	return append([]string(nil), supportedProtocolVersions...)
}

// IsSupportedProtocolVersion reports whether version is one the server
// implements.
func IsSupportedProtocolVersion(version string) bool {
	return slices.Contains(supportedProtocolVersions, version)
}

// NegotiateProtocolVersion returns the version to answer an initialize
// request with: the requested one when supported, otherwise the latest.
func NegotiateProtocolVersion(requested string) string {
	if IsSupportedProtocolVersion(requested) {
		return requested
	}
	return ProtocolVersion
}

// Scanner buffer size constants for JSON-RPC message processing
const (
	// ScannerInitialBufferSize is the initial buffer size (64KB)
	// This should be large enough for most MCP messages
	ScannerInitialBufferSize = 64 * 1024

	// ScannerMaxBufferSize is the maximum buffer size (1MB)
	// This prevents unbounded memory growth from malicious or malformed messages
	ScannerMaxBufferSize = 1024 * 1024
)
