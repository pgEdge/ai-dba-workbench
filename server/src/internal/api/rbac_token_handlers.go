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
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Token Scopes
// =============================================================================

// handleTokens handles GET/POST /api/v1/rbac/tokens
func (h *RBACHandler) handleTokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listTokens(w, r)
	case http.MethodPost:
		h.createToken(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listTokens returns all tokens with their scope information.
func (h *RBACHandler) listTokens(w http.ResponseWriter, r *http.Request) {
	if !h.requirePermission(w, r, auth.PermManageTokenScopes) {
		return
	}

	tokens, err := h.authStore.ListAllTokens()
	if err != nil {
		log.Printf("[ERROR] Failed to list tokens: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to list tokens")
		return
	}

	type tokenConnectionScope struct {
		ConnectionID int    `json:"connection_id"`
		AccessLevel  string `json:"access_level"`
	}

	type tokenScope struct {
		Scoped           bool                   `json:"scoped"`
		Connections      []tokenConnectionScope `json:"connections,omitempty"`
		MCPPrivileges    []int64                `json:"mcp_privileges,omitempty"`
		AdminPermissions []string               `json:"admin_permissions,omitempty"`
	}

	type tokenResponse struct {
		ID               int64       `json:"id"`
		Name             string      `json:"name"`
		TokenPrefix      string      `json:"token_prefix"`
		UserID           int64       `json:"user_id"`
		Username         string      `json:"username,omitempty"`
		IsServiceAccount bool        `json:"is_service_account"`
		IsSuperuser      bool        `json:"is_superuser"`
		ExpiresAt        *time.Time  `json:"expires_at"`
		Scope            *tokenScope `json:"scope,omitempty"`
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
			UserID:      t.OwnerID,
			ExpiresAt:   t.ExpiresAt,
		}
		user, err := h.authStore.GetUserByID(t.OwnerID)
		if err == nil && user != nil {
			tr.Username = user.Username
			tr.IsServiceAccount = user.IsServiceAccount
			tr.IsSuperuser = user.IsSuperuser
		}
		scope, err := h.authStore.GetTokenScope(t.ID)
		if err == nil && scope != nil {
			conns := make([]tokenConnectionScope, len(scope.Connections))
			for i, c := range scope.Connections {
				conns[i] = tokenConnectionScope{
					ConnectionID: c.ConnectionID,
					AccessLevel:  c.AccessLevel,
				}
			}
			tr.Scope = &tokenScope{
				Scoped:           true,
				Connections:      conns,
				MCPPrivileges:    scope.MCPPrivileges,
				AdminPermissions: scope.AdminPermissions,
			}
		}
		result = append(result, tr)
	}

	RespondJSON(w, http.StatusOK, map[string]any{"tokens": result})
}

// createToken creates a new token for the specified user.
func (h *RBACHandler) createToken(w http.ResponseWriter, r *http.Request) {
	if !h.requirePermission(w, r, auth.PermManageTokenScopes) {
		return
	}

	var req struct {
		OwnerUsername string  `json:"owner_username"`
		Annotation    string  `json:"annotation"`
		ExpiresIn     *string `json:"expires_in"`
	}
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.OwnerUsername == "" {
		RespondError(w, http.StatusBadRequest, "Owner username is required")
		return
	}

	var expiry *time.Time
	if req.ExpiresIn != nil && *req.ExpiresIn != "" && *req.ExpiresIn != "never" {
		duration, err := parseTokenExpiry(*req.ExpiresIn)
		if err != nil {
			RespondError(w, http.StatusBadRequest,
				fmt.Sprintf("Invalid expires_in: %s", err.Error()))
			return
		}
		exp := time.Now().Add(duration)
		expiry = &exp
	}

	rawToken, storedToken, err := h.authStore.CreateToken(
		req.OwnerUsername, req.Annotation, expiry)
	if err != nil {
		log.Printf("[ERROR] Failed to create token for %s: %v",
			req.OwnerUsername, err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to create token")
		return
	}

	RespondJSON(w, http.StatusCreated, map[string]any{
		"token":      rawToken,
		"id":         storedToken.ID,
		"owner":      req.OwnerUsername,
		"annotation": storedToken.Annotation,
		"expires_at": storedToken.ExpiresAt,
		"message":    "Token created. Save this token securely - it will not be shown again.",
	})
}

// parseTokenExpiry parses a human-readable expiry string into a duration.
// Supported formats: "24h", "30d", "4w", "6m", "1y".
// Maximum allowed duration is 10 years (3650 days equivalent).
func parseTokenExpiry(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid format")
	}
	numStr := s[:len(s)-1]
	unit := s[len(s)-1:]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", numStr)
	}
	if num <= 0 {
		return 0, fmt.Errorf("value must be positive")
	}

	// Convert to days equivalent for bounds checking (max 3650 days = ~10 years)
	var days int
	switch unit {
	case "h":
		// Allow up to 3650*24 = 87600 hours
		if num > 87600 {
			return 0, fmt.Errorf("value exceeds maximum of 87600 hours (~10 years)")
		}
		return time.Duration(num) * time.Hour, nil
	case "d":
		days = num
	case "w":
		days = num * 7
	case "m":
		days = num * 30
	case "y":
		days = num * 365
	default:
		return 0, fmt.Errorf("invalid unit: %s (use h, d, w, m, y)", unit)
	}

	if days > 3650 {
		return 0, fmt.Errorf("value exceeds maximum of 3650 days (~10 years)")
	}
	return time.Duration(days) * 24 * time.Hour, nil
}

// handleTokenSubpath handles /api/v1/rbac/tokens/{id} and
// /api/v1/rbac/tokens/{id}/scope
func (h *RBACHandler) handleTokenSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/rbac/tokens/")
	if path == "" {
		h.handleTokens(w, r)
		return
	}
	parts := strings.Split(path, "/")

	tokenID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid token ID")
		return
	}

	if !h.requirePermission(w, r, auth.PermManageTokenScopes) {
		return
	}

	// /api/v1/rbac/tokens/{id}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodDelete:
			h.deleteToken(w, r, tokenID)
		default:
			w.Header().Set("Allow", "DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/v1/rbac/tokens/{id}/scope
	if len(parts) == 2 && parts[1] == "scope" {
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
		return
	}

	http.NotFound(w, r)
}

// deleteToken deletes a token by its ID.
func (h *RBACHandler) deleteToken(w http.ResponseWriter, r *http.Request, tokenID int64) {
	if err := h.authStore.DeleteToken(strconv.FormatInt(tokenID, 10)); err != nil {
		log.Printf("[ERROR] Failed to delete token %d: %v", tokenID, err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to delete token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *RBACHandler) getTokenScope(w http.ResponseWriter, r *http.Request, tokenID int64) {
	scope, err := h.authStore.GetTokenScope(tokenID)
	if err != nil {
		log.Printf("[ERROR] Failed to get token scope for token %d: %v", tokenID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to get token scope")
		return
	}

	if scope == nil {
		RespondJSON(w, http.StatusOK, map[string]any{
			"token_id": tokenID,
			"scoped":   false,
		})
		return
	}

	RespondJSON(w, http.StatusOK, map[string]any{
		"token_id":          tokenID,
		"scoped":            true,
		"connections":       scope.Connections,
		"mcp_privileges":    scope.MCPPrivileges,
		"admin_permissions": scope.AdminPermissions,
	})
}

func (h *RBACHandler) setTokenScope(w http.ResponseWriter, r *http.Request, tokenID int64) {
	var req struct {
		Connections      []auth.ScopedConnection `json:"connections"`
		MCPPrivileges    []string                `json:"mcp_privileges"`
		AdminPermissions []string                `json:"admin_permissions"`
	}
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.Connections != nil {
		if err := h.authStore.SetTokenConnectionScope(tokenID, req.Connections); err != nil {
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

	if req.AdminPermissions != nil {
		if err := h.authStore.SetTokenAdminScope(tokenID, req.AdminPermissions); err != nil {
			log.Printf("[ERROR] Failed to set admin scope for token %d: %v", tokenID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to set admin scope")
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
