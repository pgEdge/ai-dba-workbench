/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"log"
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// ContextAwareToolProvider defines the interface for a tool provider that
// supports RBAC-filtered listing and context-aware execution.
type ContextAwareToolProvider interface {
	ListForContext(ctx context.Context) []mcp.Tool
	Execute(ctx context.Context, name string, args map[string]any) (mcp.ToolResponse, error)
}

// MCPToolHandler handles REST API requests that bridge to the MCP tool system.
type MCPToolHandler struct {
	provider ContextAwareToolProvider
}

// NewMCPToolHandler creates a new MCP tool handler with the given provider.
func NewMCPToolHandler(provider ContextAwareToolProvider) *MCPToolHandler {
	return &MCPToolHandler{
		provider: provider,
	}
}

// RegisterRoutes registers MCP tool REST routes on the mux.
func (h *MCPToolHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/api/v1/mcp/tools", authWrapper(h.handleListTools))
	mux.HandleFunc("/api/v1/mcp/tools/call", authWrapper(h.handleCallTool))
}

// toolCallRequest represents the request body for calling an MCP tool.
type toolCallRequest struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// toolListResponse represents the response body for listing MCP tools.
type toolListResponse struct {
	Tools []mcp.Tool `json:"tools"`
}

// handleListTools handles GET /api/v1/mcp/tools.
// It returns the tools available to the current user based on RBAC filtering.
func (h *MCPToolHandler) handleListTools(w http.ResponseWriter, r *http.Request) {
	if !RequireGET(w, r) {
		return
	}

	tools := h.provider.ListForContext(r.Context())
	if tools == nil {
		tools = []mcp.Tool{}
	}

	RespondJSON(w, http.StatusOK, toolListResponse{Tools: tools})
}

// handleCallTool handles POST /api/v1/mcp/tools/call.
// It executes the named tool with the provided arguments and returns the result.
func (h *MCPToolHandler) handleCallTool(w http.ResponseWriter, r *http.Request) {
	if !RequirePOST(w, r) {
		return
	}

	var req toolCallRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Tool name is required")
		return
	}

	response, err := h.provider.Execute(r.Context(), req.Name, req.Arguments)
	if err != nil {
		log.Printf("[ERROR] MCP tool execution failed for '%s': %v", req.Name, err)
		RespondError(w, http.StatusInternalServerError, "Tool execution failed")
		return
	}

	RespondJSON(w, http.StatusOK, response)
}
