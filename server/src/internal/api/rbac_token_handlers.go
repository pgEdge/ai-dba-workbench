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
	"strconv"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Token Scopes
// =============================================================================

// handleTokens handles GET /api/v1/rbac/tokens
func (h *RBACHandler) handleTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.requirePermission(w, r, auth.PermManageTokenScopes) {
		return
	}

	tokens, err := h.authStore.ListAllTokens()
	if err != nil {
		log.Printf("[ERROR] Failed to list tokens: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to list tokens")
		return
	}

	type tokenScope struct {
		Scoped        bool    `json:"scoped"`
		ConnectionIDs []int   `json:"connection_ids,omitempty"`
		MCPPrivileges []int64 `json:"mcp_privileges,omitempty"`
	}

	type tokenResponse struct {
		ID          int64       `json:"id"`
		Name        string      `json:"name"`
		TokenPrefix string      `json:"token_prefix"`
		TokenType   string      `json:"token_type"`
		UserID      *int64      `json:"user_id,omitempty"`
		Username    string      `json:"username,omitempty"`
		Scope       *tokenScope `json:"scope,omitempty"`
	}

	result := make([]tokenResponse, 0, len(tokens))
	for _, t := range tokens {
		prefix := t.TokenHash
		if len(prefix) > 12 {
			prefix = prefix[:12]
		}
		tr := tokenResponse{
			ID:          t.ID,
			Name:        t.Annotation,
			TokenPrefix: prefix,
			TokenType:   t.TokenType,
			UserID:      t.OwnerID,
		}
		if t.OwnerID != nil {
			user, err := h.authStore.GetUserByID(*t.OwnerID)
			if err == nil && user != nil {
				tr.Username = user.Username
			}
		}
		scope, err := h.authStore.GetTokenScope(t.ID)
		if err == nil && scope != nil {
			tr.Scope = &tokenScope{
				Scoped:        true,
				ConnectionIDs: scope.ConnectionIDs,
				MCPPrivileges: scope.MCPPrivileges,
			}
		}
		result = append(result, tr)
	}

	RespondJSON(w, http.StatusOK, map[string]any{"tokens": result})
}

// handleTokenSubpath handles /api/v1/rbac/tokens/{id}/scope
func (h *RBACHandler) handleTokenSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/rbac/tokens/")
	if path == "" {
		h.handleTokens(w, r)
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "scope" {
		http.NotFound(w, r)
		return
	}

	tokenID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid token ID")
		return
	}

	if !h.requirePermission(w, r, auth.PermManageTokenScopes) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getTokenScope(w, r, tokenID)
	case http.MethodPut:
		h.setTokenScope(w, r, tokenID)
	case http.MethodDelete:
		h.clearTokenScope(w, r, tokenID)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *RBACHandler) getTokenScope(w http.ResponseWriter, r *http.Request, tokenID int64) {
	scope, err := h.authStore.GetTokenScope(tokenID)
	if err != nil {
		log.Printf("[ERROR] Failed to get token scope for token %d: %v", tokenID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to get token scope")
		return
	}

	if scope == nil {
		RespondJSON(w, http.StatusOK, map[string]interface{}{
			"token_id": tokenID,
			"scoped":   false,
		})
		return
	}

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"token_id":       tokenID,
		"scoped":         true,
		"connection_ids": scope.ConnectionIDs,
		"mcp_privileges": scope.MCPPrivileges,
	})
}

func (h *RBACHandler) setTokenScope(w http.ResponseWriter, r *http.Request, tokenID int64) {
	var req struct {
		ConnectionIDs []int    `json:"connection_ids"`
		MCPPrivileges []string `json:"mcp_privileges"`
	}
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.ConnectionIDs != nil {
		if err := h.authStore.SetTokenConnectionScope(tokenID, req.ConnectionIDs); err != nil {
			log.Printf("[ERROR] Failed to set connection scope for token %d: %v", tokenID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to set connection scope")
			return
		}
	}

	if req.MCPPrivileges != nil {
		if err := h.authStore.SetTokenMCPScopeByNames(tokenID, req.MCPPrivileges); err != nil {
			log.Printf("[ERROR] Failed to set MCP scope for token %d: %v", tokenID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to set MCP scope")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *RBACHandler) clearTokenScope(w http.ResponseWriter, r *http.Request, tokenID int64) {
	if err := h.authStore.ClearTokenScope(tokenID); err != nil {
		log.Printf("[ERROR] Failed to clear token scope for token %d: %v", tokenID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to clear token scope")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
