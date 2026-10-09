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
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/logging"
)

// =============================================================================
// Group Privileges
// =============================================================================

func (h *RBACHandler) handleGroupPrivileges(w http.ResponseWriter, r *http.Request, groupID int64, remaining []string) {
	if !h.requirePermission(w, r, auth.PermManagePermissions) {
		return
	}

	if len(remaining) == 0 {
		http.NotFound(w, r)
		return
	}

	switch remaining[0] {
	case "mcp":
		h.handleGroupMCPPrivileges(w, r, groupID, remaining[1:])
	case "connections":
		h.handleGroupConnectionPrivileges(w, r, groupID, remaining[1:])
	default:
		http.NotFound(w, r)
	}
}

func (h *RBACHandler) handleGroupMCPPrivileges(w http.ResponseWriter, r *http.Request, groupID int64, remaining []string) {
	// POST /api/v1/rbac/groups/{id}/privileges/mcp
	// DELETE /api/v1/rbac/groups/{id}/privileges/mcp?name=<privilege>
	if len(remaining) == 0 {
		switch r.Method {
		case http.MethodPost:
			var req struct {
				Privilege string `json:"privilege"`
			}
			if !DecodeJSONBody(w, r, &req) {
				return
			}
			if req.Privilege == "" {
				RespondError(w, http.StatusBadRequest, "Privilege is required")
				return
			}

			// A token may grant only the MCP items it may call itself,
			// and the wildcard only when it reaches every item, or it
			// could hand a group, and so itself through membership, a
			// tool such as query_datastore that it may not call (issue
			// #471). Revoking an item narrows access and needs nothing.
			if !h.requireGrantInTokenScope(w, r,
				h.rbacChecker.CanGrantMCPItem(r.Context(), req.Privilege),
				refuseMCPGrant) {
				return
			}

			if err := h.actorStore(r).GrantMCPPrivilegeByName(groupID, req.Privilege); err != nil {
				log.Printf("[ERROR] Failed to grant MCP privilege %s to group %d: %v", logging.SanitizeForLog(req.Privilege), groupID, err) //nolint:gosec // G706: privilege passed through logging.SanitizeForLog
				RespondError(w, http.StatusInternalServerError, "Failed to grant MCP privilege")
				return
			}
			w.WriteHeader(http.StatusNoContent)

		case http.MethodDelete:
			privilege := r.URL.Query().Get("name")
			if privilege == "" {
				RespondError(w, http.StatusBadRequest, "Query parameter 'name' is required")
				return
			}

			if err := h.actorStore(r).RevokeMCPPrivilegeByName(groupID, privilege); err != nil {
				log.Printf("[ERROR] Failed to revoke MCP privilege %s from group %d: %v", logging.SanitizeForLog(privilege), groupID, err) //nolint:gosec // G706: privilege passed through logging.SanitizeForLog
				RespondError(w, http.StatusInternalServerError, "Failed to revoke MCP privilege")
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			w.Header().Set("Allow", "POST, DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.NotFound(w, r)
}

func (h *RBACHandler) handleGroupConnectionPrivileges(w http.ResponseWriter, r *http.Request, groupID int64, remaining []string) {
	// POST /api/v1/rbac/groups/{id}/privileges/connections
	if len(remaining) == 0 {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			ConnectionID int    `json:"connection_id"`
			AccessLevel  string `json:"access_level"`
		}
		if !DecodeJSONBody(w, r, &req) {
			return
		}
		if req.ConnectionID < 0 {
			RespondError(w, http.StatusBadRequest, "Valid connection_id is required")
			return
		}
		if req.AccessLevel != "read" && req.AccessLevel != "read_write" {
			RespondError(w, http.StatusBadRequest,
				"access_level must be 'read' or 'read_write'")
			return
		}

		// A token may grant a level on a connection only when it holds
		// that level or higher on the connection itself, and the "all
		// connections" grant only when it holds every connection at
		// that level (issue #471).
		if !h.requireGrantInTokenScope(w, r,
			h.rbacChecker.CanGrantConnection(r.Context(),
				req.ConnectionID, req.AccessLevel), refuseConnectionGrant) {
			return
		}

		if err := h.actorStore(r).GrantConnectionPrivilege(groupID, req.ConnectionID, req.AccessLevel); err != nil {
			log.Printf("[ERROR] Failed to grant connection privilege for conn %d to group %d: %v", req.ConnectionID, groupID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to grant connection privilege")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// DELETE /api/v1/rbac/groups/{id}/privileges/connections/{connection_id}
	if len(remaining) == 1 {
		if r.Method != http.MethodDelete {
			w.Header().Set("Allow", "DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		connID, err := strconv.Atoi(remaining[0])
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid connection ID")
			return
		}

		// A token needs read on the connection to revoke a grant on
		// it, and is refused in the same words whether or not the
		// connection exists. Revoking the last group grant on a
		// connection (an "all connections" grant restricts none, issue
		// #592) lifts its group restriction, which opens a shared
		// connection to every user, so that revoke needs read_write
		// (issue #471); the store decides that in the revoke's own
		// transaction, so that a concurrent revoke cannot slip past it.
		if !h.requireGrantInTokenScope(w, r,
			h.rbacChecker.CanRevokeConnection(r.Context(), groupID, connID),
			refuseConnectionRevoke) {
			return
		}

		guard := h.rbacChecker.ConnectionLiftGuard(r.Context(), connID)
		err = h.actorStore(r).RevokeConnectionPrivilegeGuarded(groupID, connID,
			guard)
		if errors.Is(err, auth.ErrRevokeLiftsRestriction) {
			h.requireGrantInTokenScope(w, r, false, refuseConnectionRevoke)
			return
		}
		if err != nil {
			log.Printf("[ERROR] Failed to revoke connection privilege for conn %d from group %d: %v", connID, groupID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to revoke connection privilege")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	http.NotFound(w, r)
}

// =============================================================================
// Admin Permissions
// =============================================================================

func (h *RBACHandler) handleGroupPermissions(w http.ResponseWriter, r *http.Request, groupID int64, remaining []string) {
	if !h.requireSuperuser(w, r) {
		return
	}

	// GET or POST /api/v1/rbac/groups/{id}/permissions
	if len(remaining) == 0 {
		switch r.Method {
		case http.MethodGet:
			h.listGroupPermissions(w, r, groupID)
		case http.MethodPost:
			h.grantGroupPermission(w, r, groupID)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// DELETE /api/v1/rbac/groups/{id}/permissions/{permission}
	if len(remaining) == 1 {
		if r.Method != http.MethodDelete {
			w.Header().Set("Allow", "DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.revokeGroupPermission(w, r, groupID, remaining[0])
		return
	}

	http.NotFound(w, r)
}

func (h *RBACHandler) listGroupPermissions(w http.ResponseWriter, r *http.Request, groupID int64) {
	perms, err := h.authStore.ListGroupAdminPermissions(groupID)
	if err != nil {
		log.Printf("[ERROR] Failed to list permissions for group %d: %v", groupID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to list permissions")
		return
	}
	if perms == nil {
		perms = []string{}
	}

	RespondJSON(w, http.StatusOK, map[string]any{
		"group_id":    groupID,
		"permissions": perms,
	})
}

func (h *RBACHandler) grantGroupPermission(w http.ResponseWriter, r *http.Request, groupID int64) {
	var req struct {
		Permission string `json:"permission"`
	}
	if !DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Permission == "" {
		RespondError(w, http.StatusBadRequest, "Permission is required")
		return
	}

	// A token may grant only an admin permission it holds itself, and,
	// since an admin permission acts across the whole estate, only when
	// it holds every connection at read_write. Several permissions let a
	// holder acquire every MCP item and admin permission, so granting
	// one of those needs a token that reaches them all: gating on less
	// let an MCP-bounded token plant manage_permissions on a group it
	// could then take over (issue #471). Revoking a permission narrows
	// access and needs nothing.
	if !h.requireGrantInTokenScope(w, r,
		h.rbacChecker.CanGrantAdminPermission(r.Context(), req.Permission),
		refuseAdminGrant) {
		return
	}

	if err := h.actorStore(r).GrantAdminPermission(groupID, req.Permission); err != nil {
		log.Printf("[ERROR] Failed to grant permission %s to group %d: %v", logging.SanitizeForLog(req.Permission), groupID, err) //nolint:gosec // G706: permission passed through logging.SanitizeForLog
		RespondError(w, http.StatusInternalServerError, "Failed to grant permission")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *RBACHandler) revokeGroupPermission(w http.ResponseWriter, r *http.Request, groupID int64, permission string) {
	if err := h.actorStore(r).RevokeAdminPermission(groupID, permission); err != nil {
		log.Printf("[ERROR] Failed to revoke permission %s from group %d: %v", logging.SanitizeForLog(permission), groupID, err) //nolint:gosec // G706: permission passed through logging.SanitizeForLog
		RespondError(w, http.StatusInternalServerError, "Failed to revoke permission")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
