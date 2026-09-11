/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package resources

import (
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// Handler is a function that reads a resource
type Handler func() (mcp.ResourceContent, error)

// Resource represents a registered MCP resource
type Resource struct {
	Definition mcp.Resource
	Handler    Handler
}
