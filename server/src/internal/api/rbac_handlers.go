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
	mux.HandleFunc("/api/v1/rbac/tokens", authWrapper(h.handleTokens))
	mux.HandleFunc("/api/v1/rbac/tokens/", authWrapper(h.handleTokenSubpath))
}

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
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to list users: %v", err))
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
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to create user: %v", err))
		return
	}

	if req.Enabled != nil && !*req.Enabled {
		if err := h.authStore.DisableUser(req.Username); err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to disable user: %v", err))
			return
		}
	}

	if req.IsSuperuser != nil && *req.IsSuperuser {
		if err := h.authStore.SetUserSuperuser(req.Username, true); err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to set superuser status: %v", err))
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

	if req.Password != nil || req.Annotation != nil || req.DisplayName != nil || req.Email != nil {
		newPassword := ""
		if req.Password != nil {
			newPassword = *req.Password
		}
		newAnnotation := user.Annotation
		if req.Annotation != nil {
			newAnnotation = *req.Annotation
		}
		newDisplayName := user.DisplayName
		if req.DisplayName != nil {
			newDisplayName = *req.DisplayName
		}
		newEmail := user.Email
		if req.Email != nil {
			newEmail = *req.Email
		}
		if err := h.authStore.UpdateUser(user.Username, newPassword, newAnnotation, newDisplayName, newEmail); err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to update user: %v", err))
			return
		}
	}

	if req.Enabled != nil {
		if *req.Enabled {
			err = h.authStore.EnableUser(user.Username)
		} else {
			err = h.authStore.DisableUser(user.Username)
		}
		if err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to update user enabled status: %v", err))
			return
		}
	}

	if req.IsSuperuser != nil {
		if err := h.authStore.SetUserSuperuser(user.Username, *req.IsSuperuser); err != nil {
			RespondError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to update user superuser status: %v", err))
			return
		}
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
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to delete user: %v", err))
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

	groups, err := h.authStore.ListGroupsWithMemberCount()
	if err != nil {
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to list groups: %v", err))
		return
	}

	RespondJSON(w, http.StatusOK, map[string]any{"groups": groups})
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

	// /api/v1/rbac/groups/{id}/effective-privileges
	if parts[1] == "effective-privileges" && len(parts) == 2 {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getGroupEffectivePrivileges(w, r, groupID)
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

// getGroupEffectivePrivileges handles GET /api/v1/rbac/groups/{id}/effective-privileges
func (h *RBACHandler) getGroupEffectivePrivileges(w http.ResponseWriter, r *http.Request, groupID int64) {
	if !h.requirePermission(w, r, auth.PermManagePermissions) {
		return
	}

	group, err := h.authStore.GetGroup(groupID)
	if err != nil || group == nil {
		RespondError(w, http.StatusNotFound, "Group not found")
		return
	}

	type connPriv struct {
		ConnectionID int    `json:"connection_id"`
		AccessLevel  string `json:"access_level"`
	}

	type effectiveResponse struct {
		GroupName            string     `json:"group_name"`
		MCPPrivileges        []string   `json:"mcp_privileges"`
		ConnectionPrivileges []connPriv `json:"connection_privileges"`
		AdminPermissions     []string   `json:"admin_permissions"`
	}

	resp := effectiveResponse{
		GroupName: group.Name,
	}

	mcpPrivs, err := h.authStore.GetGroupEffectiveMCPPrivileges(groupID)
	if err == nil {
		resp.MCPPrivileges = mcpPrivs
	}
	if resp.MCPPrivileges == nil {
		resp.MCPPrivileges = []string{}
	}

	connPrivs, err := h.authStore.GetGroupEffectiveConnectionPrivileges(groupID)
	if err == nil {
		for _, cp := range connPrivs {
			resp.ConnectionPrivileges = append(resp.ConnectionPrivileges, connPriv{
				ConnectionID: cp.ConnectionID,
				AccessLevel:  cp.AccessLevel,
			})
		}
	}
	if resp.ConnectionPrivileges == nil {
		resp.ConnectionPrivileges = []connPriv{}
	}

	adminPerms, err := h.authStore.GetGroupEffectiveAdminPermissions(groupID)
	if err == nil {
		resp.AdminPermissions = adminPerms
	}
	if resp.AdminPermissions == nil {
		resp.AdminPermissions = []string{}
	}

	RespondJSON(w, http.StatusOK, resp)
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

			if err := h.authStore.GrantMCPPrivilegeByName(groupID, req.Privilege); err != nil {
				RespondError(w, http.StatusInternalServerError,
					fmt.Sprintf("Failed to grant MCP privilege: %v", err))
				return
			}
			w.WriteHeader(http.StatusNoContent)

		case http.MethodDelete:
			privilege := r.URL.Query().Get("name")
			if privilege == "" {
				RespondError(w, http.StatusBadRequest, "Query parameter 'name' is required")
				return
			}

			if err := h.authStore.RevokeMCPPrivilegeByName(groupID, privilege); err != nil {
				RespondError(w, http.StatusInternalServerError,
					fmt.Sprintf("Failed to revoke MCP privilege: %v", err))
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

	if !h.requirePermission(w, r, auth.PermManagePermissions) {
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
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to list tokens: %v", err))
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
