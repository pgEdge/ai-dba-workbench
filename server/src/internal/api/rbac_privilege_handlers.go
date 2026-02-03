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
	"log"
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// MCP Privileges (top-level listing)
// =============================================================================

// handleMCPPrivileges handles GET /api/v1/rbac/privileges/mcp
func (h *RBACHandler) handleMCPPrivileges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.requirePermission(w, r, auth.PermManagePermissions) {
		return
	}

	privileges, err := h.authStore.ListMCPPrivileges()
	if err != nil {
		log.Printf("[ERROR] Failed to list MCP privileges: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to list MCP privileges")
		return
	}

	RespondJSON(w, http.StatusOK, privileges)
}
