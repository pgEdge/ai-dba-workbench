/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package auth

import (
	"context"
)

// DatabaseAccessChecker handles database access control based on authentication context
// With single database support, this is simplified to just check authentication
type DatabaseAccessChecker struct {
	tokenStore  *TokenStore
	authEnabled bool
}

// NewDatabaseAccessChecker creates a new database access checker
func NewDatabaseAccessChecker(tokenStore *TokenStore, authEnabled, _ bool) *DatabaseAccessChecker {
	return &DatabaseAccessChecker{
		tokenStore:  tokenStore,
		authEnabled: authEnabled,
	}
}

// CanAccessDatabase checks if the current request context has access to the database
// With single database, we just check that authentication is valid
func (dac *DatabaseAccessChecker) CanAccessDatabase(ctx context.Context) bool {
	// Auth disabled - database accessible
	if !dac.authEnabled {
		return true
	}

	// Check if API token
	if IsAPITokenFromContext(ctx) {
		return true
	}

	// Session user - check if authenticated
	username := GetUsernameFromContext(ctx)
	return username != ""
}

// GetBoundDatabase returns the database name that an API token is bound to
// Returns empty string - with single database, tokens are not bound to specific databases
func (dac *DatabaseAccessChecker) GetBoundDatabase(_ context.Context) string {
	return ""
}

// =============================================================================
// RBAC Access Control
// =============================================================================

// RBACChecker handles role-based access control checks
type RBACChecker struct {
	authStore   *AuthStore
	authEnabled bool
}

// NewRBACChecker creates a new RBAC checker
func NewRBACChecker(authStore *AuthStore, authEnabled bool) *RBACChecker {
	return &RBACChecker{
		authStore:   authStore,
		authEnabled: authEnabled,
	}
}

// IsSuperuser checks if the current context has superuser privileges
// Superusers bypass all privilege checks
func (rc *RBACChecker) IsSuperuser(ctx context.Context) bool {
	// Auth disabled - treat as superuser (full access)
	if !rc.authEnabled || rc.authStore == nil {
		return true
	}

	return IsSuperuserFromContext(ctx)
}

// CanAccessMCPItem checks if the current context can access a specific MCP item
// Returns true if:
// - Auth is disabled
// - User/token is a superuser
// - The item is not restricted (not assigned to any group)
// - User has the privilege through their group memberships
// - Token is scoped and includes this privilege (checked in token scope phase)
func (rc *RBACChecker) CanAccessMCPItem(ctx context.Context, identifier string) bool {
	// Auth disabled - full access
	if !rc.authEnabled || rc.authStore == nil {
		return true
	}

	// Superuser bypass
	if IsSuperuserFromContext(ctx) {
		return true
	}

	// Check if the privilege is restricted (assigned to any group)
	isRestricted, err := rc.authStore.IsPrivilegeAssignedToAnyGroup(identifier)
	if err != nil {
		// On error, deny access for safety
		return false
	}

	// If not restricted, grant access
	if !isRestricted {
		return true
	}

	// Get user ID from context
	userID := GetUserIDFromContext(ctx)
	if userID == 0 {
		// No user ID - check if it's an API token without user ownership
		// Service tokens without superuser flag can't access restricted items
		return false
	}

	// Get user's privileges through group membership
	privileges, err := rc.authStore.GetUserMCPPrivileges(userID)
	if err != nil {
		return false
	}

	// Check if user has this privilege
	if !privileges[identifier] {
		return false
	}

	// Check token scoping (if applicable)
	tokenID := GetTokenIDFromContext(ctx)
	if tokenID > 0 {
		// Token-based access - check if token is scoped
		inScope, err := rc.authStore.IsMCPItemInTokenScope(tokenID, identifier)
		if err != nil {
			return false
		}
		if !inScope {
			return false
		}
	}

	return true
}

// CanAccessConnection checks if the current context can access a specific database connection
// Returns (canAccess bool, accessLevel string) where accessLevel is "read" or "read_write"
func (rc *RBACChecker) CanAccessConnection(ctx context.Context, connectionID int) (bool, string) {
	// Auth disabled - full access
	if !rc.authEnabled || rc.authStore == nil {
		return true, AccessLevelReadWrite
	}

	// Superuser bypass
	if IsSuperuserFromContext(ctx) {
		return true, AccessLevelReadWrite
	}

	// Check if the connection is restricted (assigned to any group)
	isRestricted, err := rc.authStore.IsConnectionAssignedToAnyGroup(connectionID)
	if err != nil {
		// On error, deny access for safety
		return false, ""
	}

	// If not restricted, grant full access
	if !isRestricted {
		return true, AccessLevelReadWrite
	}

	// Get user ID from context
	userID := GetUserIDFromContext(ctx)
	if userID == 0 {
		// No user ID - service tokens without superuser flag can't access restricted connections
		return false, ""
	}

	// Get user's connection privileges through group membership
	privileges, err := rc.authStore.GetUserConnectionPrivileges(userID)
	if err != nil {
		return false, ""
	}

	// Check if user has access to this connection (specific or via "all connections")
	accessLevel, hasAccess := privileges[connectionID]
	allLevel, hasAll := privileges[ConnectionIDAll]
	if !hasAccess && !hasAll {
		return false, ""
	}
	// Use the higher of the two access levels
	if hasAll && (!hasAccess || allLevel == AccessLevelReadWrite) {
		accessLevel = allLevel
	}

	// Check token scoping (if applicable)
	tokenID := GetTokenIDFromContext(ctx)
	if tokenID > 0 {
		// Token-based access - check if token is scoped to this connection
		inScope, err := rc.authStore.IsConnectionInTokenScope(tokenID, connectionID)
		if err != nil {
			return false, ""
		}
		if !inScope {
			return false, ""
		}
	}

	return true, accessLevel
}

// GetEffectivePrivileges returns all effective privileges for the current context
// This computes the full set of accessible items and connections
func (rc *RBACChecker) GetEffectivePrivileges(ctx context.Context) *EffectivePrivileges {
	result := &EffectivePrivileges{
		MCPPrivileges:        make(map[string]bool),
		ConnectionPrivileges: make(map[int]string),
		AdminPermissions:     make(map[string]bool),
	}

	// Auth disabled - return empty (no restrictions means full access)
	if !rc.authEnabled || rc.authStore == nil {
		result.IsSuperuser = true
		return result
	}

	// Check superuser status
	result.IsSuperuser = IsSuperuserFromContext(ctx)
	if result.IsSuperuser {
		return result
	}

	// Get user ID
	userID := GetUserIDFromContext(ctx)
	if userID == 0 {
		// No user - return empty privileges
		return result
	}

	// Get MCP privileges
	mcpPrivs, err := rc.authStore.GetUserMCPPrivileges(userID)
	if err == nil {
		result.MCPPrivileges = mcpPrivs
	}

	// Get connection privileges
	connPrivs, err := rc.authStore.GetUserConnectionPrivileges(userID)
	if err == nil {
		result.ConnectionPrivileges = connPrivs
	}

	// Get admin permissions
	adminPerms, err := rc.authStore.GetUserAdminPermissions(userID)
	if err == nil {
		result.AdminPermissions = adminPerms
	}

	// Apply token scoping if applicable
	tokenID := GetTokenIDFromContext(ctx)
	if tokenID > 0 {
		scope, err := rc.authStore.GetTokenScope(tokenID)
		if err == nil && scope != nil {
			result.TokenScope = scope

			// If token has connection scope, filter connection privileges
			if len(scope.ConnectionIDs) > 0 {
				scopedConnPrivs := make(map[int]string)
				for _, connID := range scope.ConnectionIDs {
					if level, ok := result.ConnectionPrivileges[connID]; ok {
						scopedConnPrivs[connID] = level
					}
				}
				result.ConnectionPrivileges = scopedConnPrivs
			}

			// If token has MCP scope, filter MCP privileges
			if len(scope.MCPPrivileges) > 0 {
				scopedMCPPrivs := make(map[string]bool)
				for _, privID := range scope.MCPPrivileges {
					priv, err := rc.authStore.GetMCPPrivilegeByID(privID)
					if err == nil && priv != nil {
						if result.MCPPrivileges[priv.Identifier] {
							scopedMCPPrivs[priv.Identifier] = true
						}
					}
				}
				result.MCPPrivileges = scopedMCPPrivs
			}
		}
	}

	return result
}

// GetAccessibleConnections returns a list of connection IDs the current context can access
// This is useful for filtering connection lists in the UI
func (rc *RBACChecker) GetAccessibleConnections(ctx context.Context) []int {
	privs := rc.GetEffectivePrivileges(ctx)
	if privs.IsSuperuser {
		// Superusers have access to all - return nil to indicate "all"
		return nil
	}

	var connections []int
	for connID := range privs.ConnectionPrivileges {
		connections = append(connections, connID)
	}
	return connections
}

// HasAdminPermission checks if the current context has a specific admin permission
func (rc *RBACChecker) HasAdminPermission(ctx context.Context, permission string) bool {
	// Auth disabled - full access
	if !rc.authEnabled || rc.authStore == nil {
		return true
	}

	// Superuser bypass
	if IsSuperuserFromContext(ctx) {
		return true
	}

	// Get user ID from context
	userID := GetUserIDFromContext(ctx)
	if userID == 0 {
		return false
	}

	// Get user's admin permissions through group membership
	perms, err := rc.authStore.GetUserAdminPermissions(userID)
	if err != nil {
		return false
	}

	return perms[permission]
}

// HasWriteAccess checks if the current context has write access to a connection
func (rc *RBACChecker) HasWriteAccess(ctx context.Context, connectionID int) bool {
	canAccess, accessLevel := rc.CanAccessConnection(ctx, connectionID)
	return canAccess && accessLevel == AccessLevelReadWrite
}
