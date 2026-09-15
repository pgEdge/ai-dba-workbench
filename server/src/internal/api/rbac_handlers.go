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
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// RBACHandler handles REST API requests for RBAC management
type RBACHandler struct {
	authStore   *auth.AuthStore
	rbacChecker *auth.RBACChecker
}

// NewRBACHandler creates a new RBAC handler
func NewRBACHandler(authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *RBACHandler {
	return &RBACHandler{
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
}

// RegisterRoutes registers RBAC management routes on the mux
func (h *RBACHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/api/v1/rbac/users", authWrapper(h.handleUsers))
	mux.HandleFunc("/api/v1/rbac/users/", authWrapper(h.handleUserSubpath))
	mux.HandleFunc("/api/v1/rbac/groups", authWrapper(h.handleGroups))
	mux.HandleFunc("/api/v1/rbac/groups/", authWrapper(h.handleGroupSubpath))
	mux.HandleFunc("/api/v1/rbac/privileges/mcp", authWrapper(h.handleMCPPrivileges))
	mux.HandleFunc("/api/v1/rbac/tokens", authWrapper(h.handleTokens))
	mux.HandleFunc("/api/v1/rbac/tokens/", authWrapper(h.handleTokenSubpath))
	mux.HandleFunc("/api/v1/rbac/audit", authWrapper(h.handleAudit))
}

// =============================================================================
// Permission Helpers
// =============================================================================

// actorStore returns a view of the auth store that attributes every
// audited change to the principal that made the request, so that the
// audit log names the acting user or token rather than the server.
func (h *RBACHandler) actorStore(r *http.Request) *auth.ActorStore {
	return h.authStore.AsActor(auth.ActorFromContext(r.Context()))
}

// recordDenial writes a denied audit event for the request. A recording
// failure is logged and otherwise ignored: the caller is about to
// respond 403 either way, and an audit error must not change what the
// client sees.
func (h *RBACHandler) recordDenial(r *http.Request, reason string) {
	if h.authStore == nil {
		return
	}
	if err := h.authStore.RecordDenied(auth.ActorFromContext(r.Context()),
		deniedAction(r), reason); err != nil {
		log.Printf("[ERROR] Failed to record RBAC denial for %s %s: %v",
			r.Method, r.URL.Path, err)
	}
}

// requirePermission checks that the caller has the specified admin permission.
// Returns false and sends an error response if access is denied.
func (h *RBACHandler) requirePermission(w http.ResponseWriter, r *http.Request, permission string) bool {
	if !h.rbacChecker.HasAdminPermission(r.Context(), permission) {
		reason := fmt.Sprintf("Permission denied: requires %s permission",
			permission)
		h.recordDenial(r, reason)
		RespondError(w, http.StatusForbidden, reason)
		return false
	}
	return true
}

// requireSuperuser checks that the caller is a superuser.
// Returns false and sends an error response if access is denied.
func (h *RBACHandler) requireSuperuser(w http.ResponseWriter, r *http.Request) bool {
	if !h.rbacChecker.IsSuperuser(r.Context()) {
		const reason = "Permission denied: requires superuser privileges"
		h.recordDenial(r, reason)
		RespondError(w, http.StatusForbidden, reason)
		return false
	}
	return true
}

// =============================================================================
// Denial Action Mapping
// =============================================================================

// rbacPathPrefix is the common prefix of every RBAC REST route.
const rbacPathPrefix = "/api/v1/rbac/"

// deniedAction maps a refused request to the audit action name it would
// have recorded had it been allowed, so that a denial and the change it
// was refused share a vocabulary. A request that matches no known route
// shape records "rbac.<method>" in lower case, which keeps the event
// rather than dropping it.
func deniedAction(r *http.Request) string {
	fallback := "rbac." + strings.ToLower(r.Method)

	if !strings.HasPrefix(r.URL.Path, rbacPathPrefix) {
		return fallback
	}

	parts := strings.Split(strings.Trim(
		strings.TrimPrefix(r.URL.Path, rbacPathPrefix), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return fallback
	}

	switch parts[0] {
	case "users":
		if action := deniedUserAction(r.Method, parts); action != "" {
			return action
		}
	case "groups":
		if action := deniedGroupAction(r.Method, parts); action != "" {
			return action
		}
	case "tokens":
		if action := deniedTokenAction(r.Method, parts); action != "" {
			return action
		}
	case "audit":
		if len(parts) == 1 && r.Method == http.MethodGet {
			return "audit.read"
		}
	}

	return fallback
}

// deniedUserAction maps the /users routes; it returns an empty string
// when the request is not a known mutation.
func deniedUserAction(method string, parts []string) string {
	switch {
	case len(parts) == 1 && method == http.MethodPost:
		return "user.create"
	case len(parts) == 2 && method == http.MethodPut:
		return "user.update"
	case len(parts) == 2 && method == http.MethodDelete:
		return "user.delete"
	}
	return ""
}

// deniedGroupAction maps the /groups routes and their members,
// privileges and permissions sub-resources.
func deniedGroupAction(method string, parts []string) string {
	if len(parts) == 1 && method == http.MethodPost {
		return "group.create"
	}
	if len(parts) == 2 {
		switch method {
		case http.MethodPut:
			return "group.update"
		case http.MethodDelete:
			return "group.delete"
		}
		return ""
	}
	if len(parts) < 3 {
		return ""
	}

	switch parts[2] {
	case "members":
		switch {
		case len(parts) == 3 && method == http.MethodPost:
			return "group.member.add"
		case len(parts) == 5 && method == http.MethodDelete:
			return "group.member.remove"
		}
	case "permissions":
		switch {
		case len(parts) == 3 && method == http.MethodPost:
			return "permission.admin.grant"
		case len(parts) == 4 && method == http.MethodDelete:
			return "permission.admin.revoke"
		}
	case "privileges":
		return deniedGroupPrivilegeAction(method, parts)
	}

	return ""
}

// deniedGroupPrivilegeAction maps the /groups/{id}/privileges routes.
func deniedGroupPrivilegeAction(method string, parts []string) string {
	if len(parts) < 4 {
		return ""
	}

	switch parts[3] {
	case "mcp":
		if len(parts) != 4 {
			return ""
		}
		switch method {
		case http.MethodPost:
			return "privilege.mcp.grant"
		case http.MethodDelete:
			return "privilege.mcp.revoke"
		}
	case "connections":
		switch {
		case len(parts) == 4 && method == http.MethodPost:
			return "privilege.connection.grant"
		case len(parts) == 5 && method == http.MethodDelete:
			return "privilege.connection.revoke"
		}
	}

	return ""
}

// deniedTokenAction maps the /tokens routes and the scope sub-resource.
func deniedTokenAction(method string, parts []string) string {
	switch {
	case len(parts) == 1 && method == http.MethodPost:
		return "token.create"
	case len(parts) == 2 && method == http.MethodDelete:
		return "token.delete"
	case len(parts) == 3 && parts[2] == "scope" && method == http.MethodPut:
		return "token.scope.set"
	case len(parts) == 3 && parts[2] == "scope" && method == http.MethodDelete:
		return "token.scope.clear"
	}
	return ""
}
