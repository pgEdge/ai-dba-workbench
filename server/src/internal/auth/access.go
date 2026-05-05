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
	"reflect"
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

// NewRBACCheckerWithSharing creates an RBACChecker and wires the
// connection-sharing lookup in one step. If sharingLookup is nil the
// checker behaves as if no sharing information is available.
func NewRBACCheckerWithSharing(store *AuthStore, sharingLookup ConnectionSharingLookupFunc) *RBACChecker {
	checker := NewRBACChecker(store)
	if sharingLookup != nil {
		checker.SetConnectionSharingLookup(sharingLookup)
	}
	return checker
}

// DatastoreSharingLookup is the minimal datastore surface that
// NewRBACCheckerForDatastore needs. *database.Datastore satisfies it via
// its GetConnectionSharingInfo method. Callers pass a typed nil
// pointer or an actual datastore; NewRBACCheckerForDatastore handles
// both.
type DatastoreSharingLookup interface {
	GetConnectionSharingInfo(ctx context.Context, connectionID int) (isShared bool, ownerUsername string, err error)
}

// NewRBACCheckerForDatastore builds an RBACChecker that uses the
// supplied datastore as its connection-sharing lookup source. A nil
// datastore - including a typed nil that satisfies the interface
// without a concrete value - yields a checker with no sharing lookup
// wired, matching the callers' previous "skip if datastore is nil"
// behavior.
func NewRBACCheckerForDatastore(store *AuthStore, ds DatastoreSharingLookup) *RBACChecker {
	if isNilDatastore(ds) {
		return NewRBACCheckerWithSharing(store, nil)
	}
	return NewRBACCheckerWithSharing(store, ds.GetConnectionSharingInfo)
}

// isNilDatastore reports whether ds is nil either as an interface or as
// a typed nil pointer wrapped in the interface. Callers commonly pass
// `(*database.Datastore)(nil)` directly; the standard equality check
// reports non-nil for that case because the interface still carries a
// concrete type, so a reflect-based check is required.
func isNilDatastore(ds DatastoreSharingLookup) bool {
	if ds == nil {
		return true
	}
	v := reflect.ValueOf(ds)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Chan,
		reflect.Func, reflect.Map, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
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
// - The privilege is registered as public (is_public = true)
// - User has the privilege through their group memberships (specific or wildcard)
// - Token scope includes this privilege (if token-based access)
//
// Returns false if:
// - The privilege is not registered (fail-safe for unknown tools)
// - The privilege is registered but not public and user lacks group membership
func (rc *RBACChecker) CanAccessMCPItem(ctx context.Context, identifier string) bool {
	// Nil store - full access
	if rc.authStore == nil {
		return true
	}

	// Superuser bypass
	if IsSuperuserFromContext(ctx) {
		return true
	}

	// Check if the privilege is registered and whether it's public
	isPublic, isRegistered, err := rc.authStore.IsPrivilegePublic(identifier)
	if err != nil {
		// On error, deny access for safety
		return false
	}

	// If the privilege is not registered, deny access (fail-safe for unknown tools)
	if !isRegistered {
		return false
	}

	// If the privilege is public, grant access without group membership check
	if isPublic {
		return true
	}

	// Privilege is registered and not public - require group membership
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
		return false, AccessLevelNone
	}

	// If not restricted by group assignment, check sharing status
	if !isRestricted {
		// If we have a sharing lookup function, check is_shared
		if rc.connSharingLookupFn != nil {
			isShared, ownerUsername, lookupErr := rc.connSharingLookupFn(ctx, connectionID)
			if lookupErr != nil {
				// On error, deny access for safety
				return false, AccessLevelNone
			}
			if !isShared {
				// Not shared: only the owner gets access
				username := GetUsernameFromContext(ctx)
				if ownerUsername == "" || username != ownerUsername {
					return false, AccessLevelNone
				}
			}
		}
		return true, AccessLevelReadWrite
	}

	// Get user ID from context
	userID := GetUserIDFromContext(ctx)
	if userID == 0 {
		// Defensive check - all tokens now have owners
		return false, AccessLevelNone
	}

	// Get user's connection privileges through group membership
	privileges, err := rc.authStore.GetUserConnectionPrivileges(userID)
	if err != nil {
		return false, AccessLevelNone
	}

	// Check if user has access to this connection (specific or via "all
	// connections"), preferring the higher of the two levels when both
	// are present.
	accessLevel, hasAccess := resolveConnectionAccess(privileges, connectionID)
	if !hasAccess {
		return false, AccessLevelNone
	}

	// Check token scoping (if applicable)
	tokenID := GetTokenIDFromContext(ctx)
	if tokenID > 0 {
		// Token-based access - check if token is scoped to this connection
		inScope, scopeAccessLevel, err := rc.authStore.IsConnectionInTokenScope(tokenID, connectionID)
		if err != nil {
			return false, AccessLevelNone
		}
		if !inScope {
			return false, AccessLevelNone
		}
		// Apply minimum access level: token scope can restrict but not elevate
		accessLevel = applyTokenCeiling(scopeAccessLevel, accessLevel)
	}

	return true, accessLevel
}

// resolveConnectionAccess returns the user's effective access level for a
// specific connection ID given their raw ConnectionPrivileges map,
// preferring a specific grant over ConnectionIDAll but elevating when the
// wildcard grants ReadWrite. Returns ("", false) when the user has no
// access. Mirrors the lookup semantics used by CanAccessConnection.
func resolveConnectionAccess(privs map[int]string, connID int) (string, bool) {
	specificLevel, hasSpecific := privs[connID]
	wildcardLevel, hasWildcard := privs[ConnectionIDAll]
	if !hasSpecific && !hasWildcard {
		return AccessLevelNone, false
	}

	// If only one side is present, use it directly.
	if !hasSpecific {
		return wildcardLevel, true
	}
	if !hasWildcard {
		return specificLevel, true
	}

	// Both are present: prefer the higher of the two access levels. The
	// lattice is {Read, ReadWrite}, so any ReadWrite wins.
	if wildcardLevel == AccessLevelReadWrite {
		return AccessLevelReadWrite, true
	}
	return specificLevel, true
}

// applyTokenCeiling returns the minimum of the token's scoped access level
// and the user's effective level over the {Read, ReadWrite} lattice. An
// empty tokenLevel means the token scope did not specify a level and only
// the user's level applies.
//
// userLevel must be a valid access level (AccessLevelRead or
// AccessLevelReadWrite); callers must verify that the user has access
// (e.g. via resolveConnectionAccess) before calling this function. If
// userLevel is empty the result is undefined.
func applyTokenCeiling(tokenLevel, userLevel string) string {
	if tokenLevel == AccessLevelRead || userLevel == AccessLevelRead {
		return AccessLevelRead
	}
	return userLevel
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
				wildcardLevel := AccessLevelNone
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
					// Non-wildcard token scope: intersect the token's
					// explicit connection IDs against the user's group
					// grants. The user's grants may come from a specific
					// row OR from the ConnectionIDAll wildcard; see issue
					// #83: a prior implementation ignored the wildcard
					// and silently dropped scoped connections whose
					// access arrived via ConnectionIDAll, producing an
					// empty list. resolveConnectionAccess mirrors the
					// lookup semantics used by CanAccessConnection.
					scopedConnPrivs := make(map[int]string)
					for _, sc := range scope.Connections {
						userLevel, userHasAccess := resolveConnectionAccess(
							result.ConnectionPrivileges, sc.ConnectionID)
						if !userHasAccess {
							// No group grant for this connection via
							// either path: drop it from the scoped set.
							continue
						}
						// Token scope can restrict but not elevate.
						scopedConnPrivs[sc.ConnectionID] = applyTokenCeiling(
							sc.AccessLevel, userLevel)
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
							// Check if user has this privilege (specific or via wildcard "*")
							if result.MCPPrivileges[priv.Identifier] || result.MCPPrivileges["*"] {
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
						// Check if user has this permission (specific or via wildcard "*")
						if result.AdminPermissions[perm] || result.AdminPermissions[AdminPermissionWildcard] {
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

// ConnectionVisibilityLister returns the list of all connections with
// their sharing metadata. It is implemented by *database.Datastore via
// GetAllConnections and is used by VisibleConnectionIDs to avoid N+1
// lookups of sharing info.
type ConnectionVisibilityLister interface {
	GetAllConnections(ctx context.Context) ([]ConnectionVisibilityInfo, error)
}

// ConnectionVisibilityInfo holds the minimum data VisibleConnectionIDs
// needs to reason about visibility: the connection's id, its sharing
// flag, and its owner username.
type ConnectionVisibilityInfo struct {
	ID            int
	IsShared      bool
	OwnerUsername string
}

// VisibleConnectionIDs returns the set of connection IDs the caller may
// see.
//
// If allConnections is true, ids is nil and the caller has visibility to
// every connection (superuser, or a token/user with the ConnectionIDAll
// wildcard granting at least read access). Otherwise ids is an explicit
// slice combining:
//
//   - connections owned by the caller;
//   - connections with is_shared=true that are not excluded by group or
//     token restrictions;
//   - connections explicitly granted via group or token scope.
//
// The caller provides a lister that enumerates connections with their
// sharing metadata; this allows the function to apply visibility rules
// without performing an N+1 per-connection lookup.
func (rc *RBACChecker) VisibleConnectionIDs(ctx context.Context, lister ConnectionVisibilityLister) (ids []int, allConnections bool, err error) {
	// Nil store - full access.
	if rc.authStore == nil {
		return nil, true, nil
	}

	// Superuser bypass.
	if IsSuperuserFromContext(ctx) {
		return nil, true, nil
	}

	privs := rc.GetEffectivePrivileges(ctx)

	// Check for the ConnectionIDAll wildcard in the effective privileges.
	// Token scoping is already applied by GetEffectivePrivileges; a
	// wildcard survives only if both group grants and token scope allow
	// it.
	if _, hasWildcard := privs.ConnectionPrivileges[ConnectionIDAll]; hasWildcard {
		return nil, true, nil
	}

	// Accumulate the explicit ID set. Use a map to deduplicate.
	seen := make(map[int]bool)
	for connID := range privs.ConnectionPrivileges {
		seen[connID] = true
	}

	// Determine ownership and shared-visibility additions. A nil lister
	// means the caller is unable to enumerate connections; in that case
	// we return only the group/token-granted IDs.
	if lister != nil {
		username := GetUsernameFromContext(ctx)
		all, listErr := lister.GetAllConnections(ctx)
		if listErr != nil {
			return nil, false, listErr
		}

		// If the caller has zero explicit grants, there are no
		// group-based restrictions to honor; shared and owned
		// connections are visible. If the caller DOES have explicit
		// grants, those define an allow-list that further restricts
		// shared connections to entries that appear in the allow-list.
		hasExplicitGrants := len(privs.ConnectionPrivileges) > 0

		for i := range all {
			info := &all[i]
			// Owner always sees their own connection.
			if username != "" && info.OwnerUsername == username {
				seen[info.ID] = true
				continue
			}
			if info.IsShared {
				if !hasExplicitGrants || seen[info.ID] {
					seen[info.ID] = true
				}
			}
		}
	}

	if len(seen) == 0 {
		return nil, false, nil
	}
	result := make([]int, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	return result, false, nil
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
