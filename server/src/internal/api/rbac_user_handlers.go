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
// Users
// =============================================================================

// handleUsers handles GET/POST /api/v1/rbac/users
func (h *RBACHandler) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listUsers(w, r)
	case http.MethodPost:
		h.createUser(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *RBACHandler) listUsers(w http.ResponseWriter, r *http.Request) {
	if !h.requirePermission(w, r, auth.PermManageUsers) {
		return
	}

	users, err := h.authStore.ListUsers()
	if err != nil {
		log.Printf("[ERROR] Failed to list users: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to list users")
		return
	}

	// Build response without password hashes
	type userResponse struct {
		ID          int64  `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Enabled     bool   `json:"enabled"`
		IsSuperuser bool   `json:"is_superuser"`
		Annotation  string `json:"annotation,omitempty"`
	}

	result := make([]userResponse, len(users))
	for i, u := range users {
		result[i] = userResponse{
			ID:          u.ID,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Email:       u.Email,
			Enabled:     u.Enabled,
			IsSuperuser: u.IsSuperuser,
			Annotation:  u.Annotation,
		}
	}

	RespondJSON(w, http.StatusOK, map[string]any{"users": result})
}

func (h *RBACHandler) createUser(w http.ResponseWriter, r *http.Request) {
	if !h.requirePermission(w, r, auth.PermManageUsers) {
		return
	}

	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Annotation  string `json:"annotation"`
		Enabled     *bool  `json:"enabled"`
		IsSuperuser *bool  `json:"is_superuser"`
	}
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.Username == "" {
		RespondError(w, http.StatusBadRequest, "Username is required")
		return
	}
	if req.Password == "" {
		RespondError(w, http.StatusBadRequest, "Password is required")
		return
	}

	if err := h.authStore.CreateUser(req.Username, req.Password, req.Annotation, req.DisplayName, req.Email); err != nil {
		log.Printf("[ERROR] Failed to create user %s: %v", req.Username, err)
		RespondError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	if req.Enabled != nil && !*req.Enabled {
		if err := h.authStore.DisableUser(req.Username); err != nil {
			log.Printf("[ERROR] Failed to disable user %s: %v", req.Username, err)
			RespondError(w, http.StatusInternalServerError, "Failed to disable user")
			return
		}
	}

	if req.IsSuperuser != nil && *req.IsSuperuser {
		if err := h.authStore.SetUserSuperuser(req.Username, true); err != nil {
			log.Printf("[ERROR] Failed to set superuser status for %s: %v", req.Username, err)
			RespondError(w, http.StatusInternalServerError, "Failed to set superuser status")
			return
		}
	}

	RespondJSON(w, http.StatusCreated, map[string]string{
		"message": "User created",
	})
}

func (h *RBACHandler) updateUser(w http.ResponseWriter, r *http.Request, userID int64) {
	if !h.requirePermission(w, r, auth.PermManageUsers) {
		return
	}

	var req struct {
		Password    *string `json:"password"`
		DisplayName *string `json:"display_name"`
		Email       *string `json:"email"`
		Annotation  *string `json:"annotation"`
		Enabled     *bool   `json:"enabled"`
		IsSuperuser *bool   `json:"is_superuser"`
	}
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	user, err := h.authStore.GetUserByID(userID)
	if err != nil || user == nil {
		RespondError(w, http.StatusNotFound, "User not found")
		return
	}

	// Use atomic update to ensure all changes succeed or fail together
	update := auth.UserUpdate{
		Password:    req.Password,
		Annotation:  req.Annotation,
		DisplayName: req.DisplayName,
		Email:       req.Email,
		Enabled:     req.Enabled,
		IsSuperuser: req.IsSuperuser,
	}

	if err := h.authStore.UpdateUserAtomic(user.Username, update); err != nil {
		log.Printf("[ERROR] Failed to update user %s: %v", user.Username, err)
		RespondError(w, http.StatusInternalServerError, "Failed to update user")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "User updated",
	})
}

func (h *RBACHandler) deleteUser(w http.ResponseWriter, r *http.Request, userID int64) {
	if !h.requirePermission(w, r, auth.PermManageUsers) {
		return
	}

	user, err := h.authStore.GetUserByID(userID)
	if err != nil || user == nil {
		RespondError(w, http.StatusNotFound, "User not found")
		return
	}

	if err := h.authStore.DeleteUser(user.Username); err != nil {
		log.Printf("[ERROR] Failed to delete user %s: %v", user.Username, err)
		RespondError(w, http.StatusInternalServerError, "Failed to delete user")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleUserSubpath handles /api/v1/rbac/users/{id}/...
func (h *RBACHandler) handleUserSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/rbac/users/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	userID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// /api/v1/rbac/users/{id}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodPut:
			h.updateUser(w, r, userID)
		case http.MethodDelete:
			h.deleteUser(w, r, userID)
		default:
			w.Header().Set("Allow", "PUT, DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if len(parts) == 2 && parts[1] == "privileges" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getUserPrivileges(w, r, userID)
		return
	}

	http.NotFound(w, r)
}

// getUserPrivileges handles GET /api/v1/rbac/users/{id}/privileges
func (h *RBACHandler) getUserPrivileges(w http.ResponseWriter, r *http.Request, userID int64) {
	if !h.requirePermission(w, r, auth.PermManageUsers) {
		return
	}

	user, err := h.authStore.GetUserByID(userID)
	if err != nil || user == nil {
		RespondError(w, http.StatusNotFound, "User not found")
		return
	}

	type privilegeResponse struct {
		Username             string         `json:"username"`
		IsSuperuser          bool           `json:"is_superuser"`
		Groups               []string       `json:"groups"`
		MCPPrivileges        []string       `json:"mcp_privileges"`
		ConnectionPrivileges map[int]string `json:"connection_privileges"`
		AdminPermissions     []string       `json:"admin_permissions"`
	}

	resp := privilegeResponse{
		Username:    user.Username,
		IsSuperuser: user.IsSuperuser,
	}

	// Get groups
	groups, err := h.authStore.GetGroupsForUser(user.ID)
	if err == nil {
		for _, g := range groups {
			resp.Groups = append(resp.Groups, g.Name)
		}
	}
	if resp.Groups == nil {
		resp.Groups = []string{}
	}

	// Get MCP privileges
	mcpPrivs, err := h.authStore.GetUserMCPPrivileges(user.ID)
	if err == nil {
		for identifier := range mcpPrivs {
			resp.MCPPrivileges = append(resp.MCPPrivileges, identifier)
		}
	}
	if resp.MCPPrivileges == nil {
		resp.MCPPrivileges = []string{}
	}

	// Get connection privileges
	connPrivs, err := h.authStore.GetUserConnectionPrivileges(user.ID)
	if err == nil {
		resp.ConnectionPrivileges = connPrivs
	}
	if resp.ConnectionPrivileges == nil {
		resp.ConnectionPrivileges = map[int]string{}
	}

	// Get admin permissions
	adminPerms, err := h.authStore.GetUserAdminPermissions(user.ID)
	if err == nil {
		for perm := range adminPerms {
			resp.AdminPermissions = append(resp.AdminPermissions, perm)
		}
	}
	if resp.AdminPermissions == nil {
		resp.AdminPermissions = []string{}
	}

	RespondJSON(w, http.StatusOK, resp)
}
