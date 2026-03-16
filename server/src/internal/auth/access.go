/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package auth

import (
	"context"
)

// DatabaseAccessChecker handles database access control based on authentication context
// With single database support, this is simplified to just check authentication
type DatabaseAccessChecker struct{}

// NewDatabaseAccessChecker creates a new database access checker
func NewDatabaseAccessChecker() *DatabaseAccessChecker {
	return &DatabaseAccessChecker{}
}

// CanAccessDatabase checks if the current request context has access to the database
// With single database, we just check that authentication is valid
func (dac *DatabaseAccessChecker) CanAccessDatabase(ctx context.Context) bool {
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

// ConnectionSharingLookupFunc returns the sharing status and owner
// username for a given connection ID.
type ConnectionSharingLookupFunc func(ctx context.Context, connectionID int) (isShared bool, ownerUsername string, err error)

// RBACChecker handles role-based access control checks
type RBACChecker struct {
	authStore           *AuthStore
	connSharingLookupFn ConnectionSharingLookupFunc
}

// NewRBACChecker creates a new RBAC checker
func NewRBACChecker(authStore *AuthStore) *RBACChecker {
	return &RBACChecker{
		authStore: authStore,
	}
}

// SetConnectionSharingLookup sets the function used to look up
// connection sharing information. This must be called before
// CanAccessConnection is used for non-superuser access checks.
func (rc *RBACChecker) SetConnectionSharingLookup(fn ConnectionSharingLookupFunc) {
	rc.connSharingLookupFn = fn
}

// IsSuperuser checks if the current context has superuser privileges
// Superusers bypass all privilege checks
func (rc *RBACChecker) IsSuperuser(ctx context.Context) bool {
	// Nil store - treat as superuser (full access)
	if rc.authStore == nil {
		return true
	}

	return IsSuperuserFromContext(ctx)
}

// CanAccessMCPItem checks if the current context can access a specific MCP item
// Returns true if:
// - User/token is a superuser
// - The item is not restricted (not assigned to any group)
// - User has the privilege through their group memberships
// - Token is scoped and includes this privilege (checked in token scope phase)
func (rc *RBACChecker) CanAccessMCPItem(ctx context.Context, identifier string) bool {
	// Nil store - full access
	if rc.authStore == nil {
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
		// Defensive check - all tokens now have owners
		return false
	}

	// Get user's privileges through group membership
	privileges, err := rc.authStore.GetUserMCPPrivileges(userID)
	if err != nil {
		return false
	}

	// Check if user has this privilege (specific or wildcard)
	if !privileges[identifier] && !privileges["*"] {
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
	// Nil store - full access
	if rc.authStore == nil {
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

	// If not restricted by group assignment, check sharing status
	if !isRestricted {
		// If we have a sharing lookup function, check is_shared
		if rc.connSharingLookupFn != nil {
			isShared, ownerUsername, lookupErr := rc.connSharingLookupFn(ctx, connectionID)
			if lookupErr != nil {
				// On error, deny access for safety
				return false, ""
			}
			if !isShared {
				// Not shared: only the owner gets access
				username := GetUsernameFromContext(ctx)
				if ownerUsername == "" || username != ownerUsername {
					return false, ""
				}
			}
		}
		return true, AccessLevelReadWrite
	}

	// Get user ID from context
	userID := GetUserIDFromContext(ctx)
	if userID == 0 {
		// Defensive check - all tokens now have owners
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
		inScope, scopeAccessLevel, err := rc.authStore.IsConnectionInTokenScope(tokenID, connectionID)
		if err != nil {
			return false, ""
		}
		if !inScope {
			return false, ""
		}
		// Apply minimum access level: token scope can restrict but not elevate
		if scopeAccessLevel != "" && scopeAccessLevel == AccessLevelRead {
			accessLevel = AccessLevelRead
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

	// Nil store - return empty (no restrictions means full access)
	if rc.authStore == nil {
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
			if len(scope.Connections) > 0 {
				// Check for wildcard connection (connection_id = 0)
				hasWildcard := false
				wildcardLevel := ""
				for _, sc := range scope.Connections {
					if sc.ConnectionID == ConnectionIDAll {
						hasWildcard = true
						wildcardLevel = sc.AccessLevel
						break
					}
				}

				if hasWildcard {
					// Wildcard: keep all user connections but apply the
					// wildcard access level as a ceiling
					if wildcardLevel == AccessLevelRead {
						for connID := range result.ConnectionPrivileges {
							result.ConnectionPrivileges[connID] = AccessLevelRead
						}
					}
					// read_write wildcard: no further filtering needed
				} else {
					scopedConnPrivs := make(map[int]string)
					for _, sc := range scope.Connections {
						if userLevel, ok := result.ConnectionPrivileges[sc.ConnectionID]; ok {
							// Take minimum access level
							if sc.AccessLevel == AccessLevelRead || userLevel == AccessLevelRead {
								scopedConnPrivs[sc.ConnectionID] = AccessLevelRead
							} else {
								scopedConnPrivs[sc.ConnectionID] = userLevel
							}
						}
					}
					result.ConnectionPrivileges = scopedConnPrivs
				}
			}

			// If token has MCP scope, filter MCP privileges
			if len(scope.MCPPrivileges) > 0 {
				// Check for wildcard sentinel (privilege_identifier_id = 0)
				hasWildcard := false
				for _, privID := range scope.MCPPrivileges {
					if privID == MCPPrivilegeIDWildcard {
						hasWildcard = true
						break
					}
				}

				if !hasWildcard {
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
				// Wildcard: keep all user MCP privileges unfiltered
			}

			// If token has admin scope, filter admin permissions
			if len(scope.AdminPermissions) > 0 {
				// Check for wildcard ("*")
				hasWildcard := false
				for _, perm := range scope.AdminPermissions {
					if perm == AdminPermissionWildcard {
						hasWildcard = true
						break
					}
				}

				if !hasWildcard {
					scopedAdminPerms := make(map[string]bool)
					for _, perm := range scope.AdminPermissions {
						if result.AdminPermissions[perm] {
							scopedAdminPerms[perm] = true
						}
					}
					result.AdminPermissions = scopedAdminPerms
				}
				// Wildcard: keep all user admin permissions unfiltered
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
	// Nil store - full access
	if rc.authStore == nil {
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

	// Check if user has the permission via groups (specific or wildcard)
	if !perms[permission] && !perms[AdminPermissionWildcard] {
		return false
	}

	// Check token scoping (if applicable)
	tokenID := GetTokenIDFromContext(ctx)
	if tokenID > 0 {
		inScope, scopeErr := rc.authStore.IsAdminPermissionInTokenScope(tokenID, permission)
		if scopeErr != nil {
			return false
		}
		return inScope
	}

	return true
}

// HasWriteAccess checks if the current context has write access to a connection
func (rc *RBACChecker) HasWriteAccess(ctx context.Context, connectionID int) bool {
	canAccess, accessLevel := rc.CanAccessConnection(ctx, connectionID)
	return canAccess && accessLevel == AccessLevelReadWrite
}
