/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package rbac

import (
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/tools"
)

// RegisterTools registers all RBAC tools into the provided registry.
// This function is designed to be passed as a callback to
// tools.SetRBACToolRegistration to avoid import cycles.
func RegisterTools(registry *tools.Registry, cfg *config.Config, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) {
	// Group management tools
	if cfg.Builtins.Tools.IsToolEnabled("create_group") {
		registry.Register("create_group", CreateGroupTool(authStore, rbacChecker))
	}
	if cfg.Builtins.Tools.IsToolEnabled("update_group") {
		registry.Register("update_group", UpdateGroupTool(authStore, rbacChecker))
	}
	if cfg.Builtins.Tools.IsToolEnabled("delete_group") {
		registry.Register("delete_group", DeleteGroupTool(authStore, rbacChecker))
	}
	if cfg.Builtins.Tools.IsToolEnabled("list_groups") {
		registry.Register("list_groups", ListGroupsTool(authStore, rbacChecker))
	}
	if cfg.Builtins.Tools.IsToolEnabled("add_group_member") {
		registry.Register("add_group_member", AddGroupMemberTool(authStore, rbacChecker))
	}
	if cfg.Builtins.Tools.IsToolEnabled("remove_group_member") {
		registry.Register("remove_group_member", RemoveGroupMemberTool(authStore, rbacChecker))
	}

	// Privilege management tools
	if cfg.Builtins.Tools.IsToolEnabled("grant_mcp_privilege") {
		registry.Register("grant_mcp_privilege", GrantMCPPrivilegeTool(authStore, rbacChecker))
	}
	if cfg.Builtins.Tools.IsToolEnabled("revoke_mcp_privilege") {
		registry.Register("revoke_mcp_privilege", RevokeMCPPrivilegeTool(authStore, rbacChecker))
	}
	if cfg.Builtins.Tools.IsToolEnabled("grant_connection_privilege") {
		registry.Register("grant_connection_privilege", GrantConnectionPrivilegeTool(authStore, rbacChecker))
	}
	if cfg.Builtins.Tools.IsToolEnabled("revoke_connection_privilege") {
		registry.Register("revoke_connection_privilege", RevokeConnectionPrivilegeTool(authStore, rbacChecker))
	}
	if cfg.Builtins.Tools.IsToolEnabled("list_privileges") {
		registry.Register("list_privileges", ListPrivilegesTool(authStore, rbacChecker))
	}

	// User management tools
	if cfg.Builtins.Tools.IsToolEnabled("list_users") {
		registry.Register("list_users", ListUsersTool(authStore, rbacChecker))
	}
	if cfg.Builtins.Tools.IsToolEnabled("get_user_privileges") {
		registry.Register("get_user_privileges", GetUserPrivilegesTool(authStore, rbacChecker))
	}
	if cfg.Builtins.Tools.IsToolEnabled("set_superuser") {
		registry.Register("set_superuser", SetSuperuserTool(authStore))
	}

	// Token scope tools
	if cfg.Builtins.Tools.IsToolEnabled("set_token_scope") {
		registry.Register("set_token_scope", SetTokenScopeTool(authStore, rbacChecker))
	}
	if cfg.Builtins.Tools.IsToolEnabled("get_token_scope") {
		registry.Register("get_token_scope", GetTokenScopeTool(authStore, rbacChecker))
	}
	if cfg.Builtins.Tools.IsToolEnabled("clear_token_scope") {
		registry.Register("clear_token_scope", ClearTokenScopeTool(authStore, rbacChecker))
	}
}
