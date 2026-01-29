/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package api

import (
	"fmt"
	"net/http"
	"strconv"
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
	mux.HandleFunc("/api/v1/rbac/tokens/", authWrapper(h.handleTokenSubpath))
}

// =============================================================================
// Users
// =============================================================================

// handleUsers handles GET /api/v1/rbac/users
func (h *RBACHandler) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.requirePermission(w, r, auth.PermManageUsers) {
		return
	}

	users, err := h.authStore.ListUsers()
	if err != nil {
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to list users: %v", err))
		return
	}

	// Build response without password hashes
	type userResponse struct {
		ID          int64  `json:"id"`
		Username    string `json:"username"`
		Enabled     bool   `json:"enabled"`
		IsSuperuser bool   `json:"is_superuser"`
		Annotation  string `json:"annotation,omitempty"`
	}

	result := make([]userResponse, len(users))
	for i, u := range users {
		result[i] = userResponse{
			ID:          u.ID,
			Username:    u.Username,
			Enabled:     u.Enabled,
			IsSuperuser: u.IsSuperuser,
			Annotation:  u.Annotation,
		}
	}

	RespondJSON(w, http.StatusOK, result)
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

// =============================================================================
// Groups
// =============================================================================

// handleGroups handles GET/POST /api/v1/rbac/groups
func (h *RBACHandler) handleGroups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listGroups(w, r)
	case http.MethodPost:
		h.createGroup(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *RBACHandler) listGroups(w http.ResponseWriter, r *http.Request) {
	if !h.requirePermission(w, r, auth.PermManageGroups) {
		return
	}

	groups, err := h.authStore.ListGroups()
	if err != nil {
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to list groups: %v", err))
		return
	}

	RespondJSON(w, http.StatusOK, groups)
}

func (h *RBACHandler) createGroup(w http.ResponseWriter, r *http.Request) {
	if !h.requirePermission(w, r, auth.PermManageGroups) {
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	groupID, err := h.authStore.CreateGroup(req.Name, req.Description)
	if err != nil {
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to create group: %v", err))
		return
	}

	RespondJSON(w, http.StatusCreated, map[string]interface{}{
		"id":   groupID,
		"name": req.Name,
	})
}

// handleGroupSubpath handles /api/v1/rbac/groups/{id}/...
func (h *RBACHandler) handleGroupSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/rbac/groups/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	groupID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid group ID")
		return
	}

	// /api/v1/rbac/groups/{id}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.getGroup(w, r, groupID)
		case http.MethodPut:
			h.updateGroup(w, r, groupID)
		case http.MethodDelete:
			h.deleteGroup(w, r, groupID)
		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/v1/rbac/groups/{id}/members
	if parts[1] == "members" {
		h.handleGroupMembers(w, r, groupID, parts[2:])
		return
	}

	// /api/v1/rbac/groups/{id}/privileges/mcp or /privileges/connections
	if parts[1] == "privileges" {
		h.handleGroupPrivileges(w, r, groupID, parts[2:])
		return
	}

	// /api/v1/rbac/groups/{id}/permissions
	if parts[1] == "permissions" {
		h.handleGroupPermissions(w, r, groupID, parts[2:])
		return
	}

	http.NotFound(w, r)
}

func (h *RBACHandler) getGroup(w http.ResponseWriter, r *http.Request, groupID int64) {
	if !h.requirePermission(w, r, auth.PermManageGroups) {
		return
	}

	group, err := h.authStore.GetGroup(groupID)
	if err != nil || group == nil {
		RespondError(w, http.StatusNotFound, "Group not found")
		return
	}

	members, membersErr := h.authStore.GetGroupMembers(groupID)
	privs, privsErr := h.authStore.GetGroupWithPrivileges(groupID)
	perms, permsErr := h.authStore.ListGroupAdminPermissions(groupID)

	type groupDetail struct {
		*auth.UserGroup
		UserMembers          []string                   `json:"user_members,omitempty"`
		GroupMembers         []string                   `json:"group_members,omitempty"`
		MCPPrivileges        []auth.MCPPrivilege        `json:"mcp_privileges,omitempty"`
		ConnectionPrivileges []auth.ConnectionPrivilege `json:"connection_privileges,omitempty"`
		AdminPermissions     []string                   `json:"admin_permissions,omitempty"`
	}

	detail := groupDetail{UserGroup: group}
	if membersErr == nil && members != nil {
		detail.UserMembers = members.UserMembers
		detail.GroupMembers = members.GroupMembers
	}
	if privsErr == nil && privs != nil {
		detail.MCPPrivileges = privs.MCPPrivileges
		detail.ConnectionPrivileges = privs.ConnectionPrivileges
	}
	if permsErr == nil && perms != nil {
		detail.AdminPermissions = perms
	}

	RespondJSON(w, http.StatusOK, detail)
}

func (h *RBACHandler) updateGroup(w http.ResponseWriter, r *http.Request, groupID int64) {
	if !h.requirePermission(w, r, auth.PermManageGroups) {
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.Name == "" && req.Description == "" {
		RespondError(w, http.StatusBadRequest,
			"At least one of name or description is required")
		return
	}

	if err := h.authStore.UpdateGroup(groupID, req.Name, req.Description); err != nil {
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to update group: %v", err))
		return
	}

	group, err := h.authStore.GetGroup(groupID)
	if err != nil {
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Group updated but failed to retrieve: %v", err))
		return
	}
	RespondJSON(w, http.StatusOK, group)
}

func (h *RBACHandler) deleteGroup(w http.ResponseWriter, r *http.Request, groupID int64) {
	if !h.requirePermission(w, r, auth.PermManageGroups) {
		return
	}

	if err := h.authStore.DeleteGroup(groupID); err != nil {
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to delete group: %v", err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// =============================================================================
// Group Members
// =============================================================================

func (h *RBACHandler) handleGroupMembers(w http.ResponseWriter, r *http.Request, groupID int64, remaining []string) {
	if !h.requirePermission(w, r, auth.PermManageGroups) {
		return
	}

	// POST /api/v1/rbac/groups/{id}/members
	if len(remaining) == 0 {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.addGroupMember(w, r, groupID)
		return
	}

	// DELETE /api/v1/rbac/groups/{id}/members/{type}/{member_id}
	if len(remaining) == 2 {
		if r.Method != http.MethodDelete {
			w.Header().Set("Allow", "DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		memberType := remaining[0]
		memberID, err := strconv.ParseInt(remaining[1], 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid member ID")
			return
		}
		h.removeGroupMember(w, r, groupID, memberType, memberID)
		return
	}

	http.NotFound(w, r)
}

func (h *RBACHandler) addGroupMember(w http.ResponseWriter, r *http.Request, groupID int64) {
	var req struct {
		UserID  *int64 `json:"user_id,omitempty"`
		GroupID *int64 `json:"group_id,omitempty"`
	}
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.UserID == nil && req.GroupID == nil {
		RespondError(w, http.StatusBadRequest,
			"Either user_id or group_id is required")
		return
	}
	if req.UserID != nil && req.GroupID != nil {
		RespondError(w, http.StatusBadRequest,
			"Only one of user_id or group_id may be specified")
		return
	}

	if req.UserID != nil {
		if err := h.authStore.AddUserToGroup(groupID, *req.UserID); err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to add user to group: %v", err))
			return
		}
	} else {
		if err := h.authStore.AddGroupToGroup(groupID, *req.GroupID); err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to add group to group: %v", err))
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *RBACHandler) removeGroupMember(w http.ResponseWriter, r *http.Request, groupID int64, memberType string, memberID int64) {
	switch memberType {
	case "user":
		if err := h.authStore.RemoveUserFromGroup(groupID, memberID); err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to remove user from group: %v", err))
			return
		}
	case "group":
		if err := h.authStore.RemoveGroupFromGroup(groupID, memberID); err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to remove group from group: %v", err))
			return
		}
	default:
		RespondError(w, http.StatusBadRequest,
			"Invalid member type: must be 'user' or 'group'")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// =============================================================================
// Group Privileges
// =============================================================================

func (h *RBACHandler) handleGroupPrivileges(w http.ResponseWriter, r *http.Request, groupID int64, remaining []string) {
	if !h.requirePermission(w, r, auth.PermManagePrivileges) {
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
	if len(remaining) == 0 {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

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

		if err := h.authStore.GrantMCPPrivilegeByName(groupID, req.Privilege); err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to grant MCP privilege: %v", err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// DELETE /api/v1/rbac/groups/{id}/privileges/mcp/{privilege}
	if len(remaining) == 1 {
		if r.Method != http.MethodDelete {
			w.Header().Set("Allow", "DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		privilege := remaining[0]
		if err := h.authStore.RevokeMCPPrivilegeByName(groupID, privilege); err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to revoke MCP privilege: %v", err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
		if req.ConnectionID <= 0 {
			RespondError(w, http.StatusBadRequest, "Valid connection_id is required")
			return
		}
		if req.AccessLevel != "read" && req.AccessLevel != "read_write" {
			RespondError(w, http.StatusBadRequest,
				"access_level must be 'read' or 'read_write'")
			return
		}

		if err := h.authStore.GrantConnectionPrivilege(groupID, req.ConnectionID, req.AccessLevel); err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to grant connection privilege: %v", err))
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

		if err := h.authStore.RevokeConnectionPrivilege(groupID, connID); err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to revoke connection privilege: %v", err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	http.NotFound(w, r)
}

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

	if !h.requirePermission(w, r, auth.PermManagePrivileges) {
		return
	}

	privileges, err := h.authStore.ListMCPPrivileges()
	if err != nil {
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to list MCP privileges: %v", err))
		return
	}

	RespondJSON(w, http.StatusOK, privileges)
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
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to list permissions: %v", err))
		return
	}
	if perms == nil {
		perms = []string{}
	}

	RespondJSON(w, http.StatusOK, map[string]interface{}{
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

	if err := h.authStore.GrantAdminPermission(groupID, req.Permission); err != nil {
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to grant permission: %v", err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *RBACHandler) revokeGroupPermission(w http.ResponseWriter, r *http.Request, groupID int64, permission string) {
	if err := h.authStore.RevokeAdminPermission(groupID, permission); err != nil {
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to revoke permission: %v", err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// =============================================================================
// Token Scopes
// =============================================================================

// handleTokenSubpath handles /api/v1/rbac/tokens/{id}/scope
func (h *RBACHandler) handleTokenSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/rbac/tokens/")
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
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to get token scope: %v", err))
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
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to set connection scope: %v", err))
			return
		}
	}

	if req.MCPPrivileges != nil {
		if err := h.authStore.SetTokenMCPScopeByNames(tokenID, req.MCPPrivileges); err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to set MCP scope: %v", err))
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *RBACHandler) clearTokenScope(w http.ResponseWriter, r *http.Request, tokenID int64) {
	if err := h.authStore.ClearTokenScope(tokenID); err != nil {
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to clear token scope: %v", err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// =============================================================================
// Permission Helpers
// =============================================================================

// requirePermission checks that the caller has the specified admin permission.
// Returns false and sends an error response if access is denied.
func (h *RBACHandler) requirePermission(w http.ResponseWriter, r *http.Request, permission string) bool {
	if !h.rbacChecker.HasAdminPermission(r.Context(), permission) {
		RespondError(w, http.StatusForbidden,
			fmt.Sprintf("Permission denied: requires %s permission", permission))
		return false
	}
	return true
}

// requireSuperuser checks that the caller is a superuser.
// Returns false and sends an error response if access is denied.
func (h *RBACHandler) requireSuperuser(w http.ResponseWriter, r *http.Request) bool {
	if !h.rbacChecker.IsSuperuser(r.Context()) {
		RespondError(w, http.StatusForbidden,
			"Permission denied: requires superuser privileges")
		return false
	}
	return true
}
