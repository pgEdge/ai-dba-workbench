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
	"fmt"
	"reflect"
)

// DatabaseAccessChecker handles database access control based on authentication context
// With single database support, this is simplified to just check authentication
type DatabaseAccessChecker struct{}

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

// IsSuperuser reports whether the current context may pass a blanket
// superuser gate, one that names no particular permission and so
// grants everything at once, such as requireSuperuser on the audit
// endpoint.
//
// A superuser's API token is not automatically a stand-in for its
// owner. A token whose admin scope has been narrowed, meaning it names
// specific permissions rather than being empty or holding the "*"
// wildcard, is an explicit statement that the token was issued for one
// job, so it cannot pass a gate that would hand it every job: see
// issue #471, where such a token could read the whole installation's
// audit log. Session callers carry no token and are unaffected.
//
// This is deliberately stricter than the permission-by-permission
// checks in this file, and the difference is not an inconsistency to
// be tidied away. HasAdminPermission, CanAccessConnection and
// CanAccessMCPItem each name the thing being reached, so they can
// intersect the owner's superuser rights with the token's scope and
// still allow what the scope names; a blanket gate names nothing to
// intersect against, so the only safe answer for a narrowed token is
// no.
func (rc *RBACChecker) IsSuperuser(ctx context.Context) bool {
	// Nil store - treat as superuser (full access)
	if rc.authStore == nil {
		return true
	}

	if !IsSuperuserFromContext(ctx) {
		return false
	}

	return rc.tokenCarriesSuperuser(ctx)
}

// tokenCarriesSuperuser reports whether the acting API token, if there
// is one, leaves its owner's superuser status intact.
//
// It fails closed in two cases. A context that claims API-token
// authentication but carries no token id cannot have its scope read at
// all, and a scope lookup that fails says nothing about what the token
// may do; in neither case may the caller be handed superuser rights,
// because a lost id or a database error must never widen access.
func (rc *RBACChecker) tokenCarriesSuperuser(ctx context.Context) bool {
	if tokenContextIncomplete(ctx) {
		return false
	}

	tokenID := GetTokenIDFromContext(ctx)
	if tokenID <= 0 {
		// Session caller: no token, so no scope to narrow.
		return true
	}

	scope, err := rc.authStore.GetTokenAdminScope(tokenID)
	if err != nil {
		return false
	}
	if len(scope) == 0 {
		// No admin scope at all is unrestricted by design.
		return true
	}
	for _, permission := range scope {
		if permission == AdminPermissionWildcard {
			return true
		}
	}

	return false
}

// tokenContextIncomplete reports whether the context claims API-token
// authentication but carries no token ID.
//
// Every scope check in this file treats a zero token ID as "not a token
// call, so no scope to apply", which is a permissive default. That
// default is only safe for session callers, who have no scope in the
// first place. A context that says it is an API token but has lost its
// ID cannot have its scope evaluated at all, so it fails closed rather
// than falling through to unrestricted access.
func tokenContextIncomplete(ctx context.Context) bool {
	return IsAPITokenFromContext(ctx) && GetTokenIDFromContext(ctx) <= 0
}

// applyConnectionTokenScope intersects an already-granted access level
// for one connection with the caller's token connection scope, and is
// the single place that intersection happens.
//
// It returns the level unchanged for callers with no token ID (session
// callers), because a session is bounded by its user's group grants
// alone. For a token it denies outright when the connection falls
// outside the scope, and otherwise lowers the level to the scope's
// ceiling; a token with no connection scope at all reports every
// connection in scope at AccessLevelNone, which applyTokenCeiling passes
// through unchanged.
func (rc *RBACChecker) applyConnectionTokenScope(
	ctx context.Context, connectionID int, level string,
) (bool, string) {
	tokenID := GetTokenIDFromContext(ctx)
	if tokenID <= 0 {
		return true, level
	}

	inScope, scopeLevel, err := rc.authStore.IsConnectionInTokenScope(tokenID, connectionID)
	if err != nil {
		// On error, deny access for safety
		return false, AccessLevelNone
	}
	if !inScope {
		return false, AccessLevelNone
	}

	// Token scope can restrict but not elevate.
	return true, applyTokenCeiling(scopeLevel, level)
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

	// An API token whose ID did not survive into the context cannot have
	// its scope checked, so deny rather than treat it as unscoped.
	if tokenContextIncomplete(ctx) {
		return false
	}

	// Superuser bypass, intersected with the token's MCP scope. The
	// raw context flag is read here rather than IsSuperuser, because
	// this branch names the item being reached and so can intersect:
	// a superuser holds every MCP privilege, and intersecting that
	// with the token's MCP scope yields the scope itself. An admin
	// scope, which restricts a different surface, must not silently
	// withdraw the tools the token was issued for.
	if IsSuperuserFromContext(ctx) {
		if tokenID := GetTokenIDFromContext(ctx); tokenID > 0 {
			inScope, scopeErr := rc.authStore.IsMCPItemInTokenScope(tokenID, identifier)
			if scopeErr != nil {
				// A scope that cannot be read cannot be shown to
				// include this item, so deny rather than assume.
				return false
			}
			return inScope
		}
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

	// An API token whose ID did not survive into the context cannot have
	// its scope checked, so deny rather than treat it as unscoped.
	if tokenContextIncomplete(ctx) {
		return false, AccessLevelNone
	}

	// Superuser bypass, intersected with the token's connection scope:
	// applyConnectionTokenScope returns the level unchanged for a
	// session caller and confines a scoped token to its own
	// connections, at the level the scope records. The raw context
	// flag is read here rather than IsSuperuser for the reason given
	// on CanAccessMCPItem: this branch names the connection, so it
	// intersects rather than refusing outright.
	if IsSuperuserFromContext(ctx) {
		return rc.applyConnectionTokenScope(ctx, connectionID, AccessLevelReadWrite)
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
		// The connection is unrestricted by group membership, but a
		// scoped token is still confined to its scope: ownership and
		// sharing decide who may reach a connection, not which subset of
		// them a given token was issued for.
		return rc.applyConnectionTokenScope(ctx, connectionID, AccessLevelReadWrite)
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
	return rc.applyConnectionTokenScope(ctx, connectionID, accessLevel)
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

// scopeHasConnectionWildcard reports whether a token's connection scope
// holds the ConnectionIDAll wildcard row, which admits every
// connection and so restricts nothing.
func scopeHasConnectionWildcard(scope *TokenScope) bool {
	for _, sc := range scope.Connections {
		if sc.ConnectionID == ConnectionIDAll {
			return true
		}
	}
	return false
}

// scopeHasMCPWildcard reports whether a token's MCP scope holds the
// wildcard sentinel, which admits every MCP privilege.
func scopeHasMCPWildcard(scope *TokenScope) bool {
	for _, privID := range scope.MCPPrivileges {
		if privID == MCPPrivilegeIDWildcard {
			return true
		}
	}
	return false
}

// scopeHasAdminWildcard reports whether a token's admin scope holds the
// "*" wildcard, which admits every admin permission.
func scopeHasAdminWildcard(scope *TokenScope) bool {
	for _, permission := range scope.AdminPermissions {
		if permission == AdminPermissionWildcard {
			return true
		}
	}
	return false
}

// applySuperuserTokenScope records a superuser token's own scope in the
// effective privileges.
//
// The superuser path used to return empty maps, which every consumer
// reads as "unrestricted", so a client holding a scoped token was told
// it could do anything. Each scope kind is reported on its own
// surface, matching what the checks in this file will actually allow:
// the connection, MCP and admin scopes each narrow their own map, and
// a kind the token does not scope, or scopes with a wildcard, leaves
// its map empty, which is the unchanged "superuser, no limits"
// answer.
func (rc *RBACChecker) applySuperuserTokenScope(
	ctx context.Context, result *EffectivePrivileges,
) {
	tokenID := GetTokenIDFromContext(ctx)
	if tokenID <= 0 {
		return
	}

	scope, err := rc.authStore.GetTokenScope(tokenID)
	if err != nil {
		// Nothing can be claimed about a scope that could not be read,
		// so the error travels with the result and the privilege maps
		// are left empty rather than filled in optimistically.
		result.TokenScopeError = err
		return
	}
	if scope == nil {
		return
	}
	result.TokenScope = scope

	if len(scope.Connections) > 0 && !scopeHasConnectionWildcard(scope) {
		for _, sc := range scope.Connections {
			// The stored access level is constrained to read or
			// read_write, and a superuser's own ceiling is read_write,
			// so the scope's level is the effective one.
			result.ConnectionPrivileges[sc.ConnectionID] = sc.AccessLevel
		}
	}

	if len(scope.MCPPrivileges) > 0 && !scopeHasMCPWildcard(scope) {
		for _, privID := range scope.MCPPrivileges {
			priv, privErr := rc.authStore.GetMCPPrivilegeByID(privID)
			if privErr == nil && priv != nil {
				result.MCPPrivileges[priv.Identifier] = true
			}
		}
	}

	if len(scope.AdminPermissions) > 0 && !scopeHasAdminWildcard(scope) {
		// The owner holds every admin permission, so the scope is the
		// intersection, and it is reported here even though a narrowed
		// admin scope also sets IsSuperuser false: HasAdminPermission
		// will allow exactly these, and the report must say so.
		for _, permission := range scope.AdminPermissions {
			result.AdminPermissions[permission] = true
		}
	}
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

	// A superuser's privileges come from its own rights intersected
	// with its token's scope, not from group membership, so the
	// superuser path reports the scope rather than an unqualified "no
	// limits". IsSuperuser in the result answers the narrower
	// question of whether a blanket superuser gate would admit the
	// caller, which a narrowed admin scope withdraws.
	if IsSuperuserFromContext(ctx) {
		result.IsSuperuser = rc.IsSuperuser(ctx)
		rc.applySuperuserTokenScope(ctx, result)
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
		result.TokenScopeError = err
		if err == nil && scope != nil {
			result.TokenScope = scope

			// If token has connection scope, filter connection privileges
			if len(scope.Connections) > 0 {
				// Check for wildcard connection (connection_id = 0)
				hasWildcard := scopeHasConnectionWildcard(scope)
				wildcardLevel := AccessLevelNone
				for _, sc := range scope.Connections {
					if sc.ConnectionID == ConnectionIDAll {
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
				if !scopeHasMCPWildcard(scope) {
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
				if !scopeHasAdminWildcard(scope) {
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

// superuserVisibleConnectionIDs resolves visibility for a superuser
// caller. A session sees every connection, as it always has, and so
// does a token with no connection scope or one holding the
// ConnectionIDAll wildcard. A token narrowed to specific connections
// sees exactly those, because the scope names the connections the
// token was issued for and enumerating the rest would disclose them.
//
// A scope that cannot be read is an error rather than a silent
// "everything", for the same reason CanAccessConnection denies on a
// failed lookup: a database failure must not widen access.
func (rc *RBACChecker) superuserVisibleConnectionIDs(
	ctx context.Context,
) (ids []int, allConnections bool, err error) {
	tokenID := GetTokenIDFromContext(ctx)
	if tokenID <= 0 {
		return nil, true, nil
	}

	scope, err := rc.authStore.GetTokenScope(tokenID)
	if err != nil {
		return nil, false, fmt.Errorf(
			"token %d: connection scope could not be read: %w", tokenID, err)
	}
	if scope == nil || len(scope.Connections) == 0 ||
		scopeHasConnectionWildcard(scope) {
		return nil, true, nil
	}

	// token_connection_scope is unique on (token_id, connection_id), so
	// the rows need no deduplication here.
	ids = make([]int, 0, len(scope.Connections))
	for _, sc := range scope.Connections {
		ids = append(ids, sc.ConnectionID)
	}

	return ids, false, nil
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

	// An API token whose ID did not survive into the context cannot have
	// its scope checked, so show nothing rather than treat it as unscoped.
	if tokenContextIncomplete(ctx) {
		return nil, false, nil
	}

	// Superuser bypass, intersected with the acting token's connection
	// scope so that a token issued for one connection does not
	// enumerate every connection in the installation. As elsewhere on
	// the connection surface, the raw context flag is read and the
	// intersection is with the connection scope alone.
	if IsSuperuserFromContext(ctx) {
		return rc.superuserVisibleConnectionIDs(ctx)
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

	// The owner and shared branches above admit connections on identity
	// alone, so a scoped token has to be intersected against its scope
	// after them; otherwise a token issued for one connection enumerates
	// every connection its owner happens to have. The scope was already
	// read by GetEffectivePrivileges, so it is intersected from there
	// rather than with one query per visible connection. A token whose
	// scope could not be read is shown nothing, for the same reason
	// CanAccessConnection denies it; a token with no scope at all
	// (privs.TokenScope nil, no error) is unrestricted.
	if tokenID := GetTokenIDFromContext(ctx); tokenID > 0 {
		if privs.TokenScopeError != nil {
			return nil, false, fmt.Errorf("token %d: connection scope could not be read: %w",
				tokenID, privs.TokenScopeError)
		}
		if privs.TokenScope != nil {
			for connID := range seen {
				if !privs.TokenScope.InScope(connID) {
					delete(seen, connID)
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

	// An API token whose ID did not survive into the context cannot have
	// its scope checked, so deny rather than treat it as unscoped.
	if tokenContextIncomplete(ctx) {
		return false
	}

	// Superuser bypass, intersected with the token's admin scope. A
	// superuser holds every admin permission, so the intersection is
	// the scope itself: a token scoped to one permission may exercise
	// that permission, and nothing else, without its owner needing a
	// group grant of its own. IsAdminPermissionInTokenScope also
	// reports true for a token with no admin scope and for one holding
	// the wildcard, which are unrestricted by design. A scope that
	// cannot be read denies, because a database error must not widen
	// access.
	if IsSuperuserFromContext(ctx) {
		tokenID := GetTokenIDFromContext(ctx)
		if tokenID <= 0 {
			return true
		}
		inScope, scopeErr := rc.authStore.IsAdminPermissionInTokenScope(
			tokenID, permission)
		if scopeErr != nil {
			return false
		}
		return inScope
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
