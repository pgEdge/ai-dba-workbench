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
		log.Printf("[ERROR] Failed to list groups: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to list groups")
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
		log.Printf("[ERROR] Failed to create group %s: %v", req.Name, err)
		RespondError(w, http.StatusInternalServerError, "Failed to create group")
		return
	}

	RespondJSON(w, http.StatusCreated, map[string]any{
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
		log.Printf("[ERROR] Failed to update group %d: %v", groupID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to update group")
		return
	}

	group, err := h.authStore.GetGroup(groupID)
	if err != nil {
		log.Printf("[ERROR] Group %d updated but failed to retrieve: %v", groupID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to retrieve updated group")
		return
	}
	RespondJSON(w, http.StatusOK, group)
}

func (h *RBACHandler) deleteGroup(w http.ResponseWriter, r *http.Request, groupID int64) {
	if !h.requirePermission(w, r, auth.PermManageGroups) {
		return
	}

	if err := h.authStore.DeleteGroup(groupID); err != nil {
		log.Printf("[ERROR] Failed to delete group %d: %v", groupID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to delete group")
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
			log.Printf("[ERROR] Failed to add user %d to group %d: %v", *req.UserID, groupID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to add user to group")
			return
		}
	} else {
		if err := h.authStore.AddGroupToGroup(groupID, *req.GroupID); err != nil {
			log.Printf("[ERROR] Failed to add group %d to group %d: %v", *req.GroupID, groupID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to add group to group")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *RBACHandler) removeGroupMember(w http.ResponseWriter, r *http.Request, groupID int64, memberType string, memberID int64) {
	switch memberType {
	case "user":
		if err := h.authStore.RemoveUserFromGroup(groupID, memberID); err != nil {
			log.Printf("[ERROR] Failed to remove user %d from group %d: %v", memberID, groupID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to remove user from group")
			return
		}
	case "group":
		if err := h.authStore.RemoveGroupFromGroup(groupID, memberID); err != nil {
			log.Printf("[ERROR] Failed to remove group %d from group %d: %v", memberID, groupID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to remove group from group")
			return
		}
	default:
		RespondError(w, http.StatusBadRequest,
			"Invalid member type: must be 'user' or 'group'")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
