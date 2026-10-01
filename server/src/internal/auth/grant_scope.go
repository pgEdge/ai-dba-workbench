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

import "context"

// The checks in this file bound what an API token may hand out. An
// admin permission such as manage_permissions or manage_users says
// nothing about which connections, MCP items or admin permissions a
// token was issued for, so without them a token scoped to one
// connection could grant a group access to every connection, create a
// user who reaches every shared connection, or hand a group an MCP tool
// the token itself may not call (issue #471).
//
// What a token grants is bounded by the token's own scope, in all three
// kinds: connections, MCP items and admin permissions. Each check
// passes for a session caller, which has no token, and for a token that
// is unrestricted in the kinds the access being granted touches;
// otherwise that access must fall inside the acting token's scope.
// Like the other scope checks they fail closed: a checker with no auth
// store, a token context missing its id, and a lookup that fails are
// all out of scope.

// mcpScopeWildcard is the name GetTokenMCPScope and
// GetGroupEffectiveMCPPrivileges report for the "all MCP items" grant,
// and the name an HTTP request uses to ask for it.
const mcpScopeWildcard = "*"

// GrantedTokenScope is a token's scope as it will stand after a change,
// given by kind. A kind with no entries is unrestricted, so the token
// reaches whatever its owner holds in that kind.
type GrantedTokenScope struct {
	Connections      []ScopedConnection
	MCPPrivileges    []string
	AdminPermissions []string
}

// actingToken reports how a grant check should treat the caller: as a
// session (no token, so nothing to bound), as a token to check, or as
// out of scope outright because the checker has no store or the token
// context has lost its id.
func (rc *RBACChecker) actingToken(ctx context.Context) (tokenID int64, ok bool) {
	if rc.authStore == nil || tokenContextIncomplete(ctx) {
		return 0, false
	}
	return GetTokenIDFromContext(ctx), true
}

// namedScope turns the entries of one named scope kind into a set. It
// reports restricted as false when there are no entries or they include
// the wildcard, since either leaves that kind unrestricted.
func namedScope(entries []string, wildcard string) (set map[string]bool,
	restricted bool) {

	if len(entries) == 0 {
		return nil, false
	}
	set = make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry == wildcard {
			return nil, false
		}
		set[entry] = true
	}
	return set, true
}

// actorMCPScope reads the acting token's MCP scope. ok is false when it
// cannot be read, and a session caller is unrestricted.
func (rc *RBACChecker) actorMCPScope(ctx context.Context) (
	set map[string]bool, restricted, ok bool) {

	tokenID, ok := rc.actingToken(ctx)
	if !ok {
		return nil, false, false
	}
	if tokenID <= 0 {
		return nil, false, true
	}
	names, err := rc.authStore.GetTokenMCPScope(tokenID)
	if err != nil {
		return nil, false, false
	}
	set, restricted = namedScope(names, mcpScopeWildcard)
	if restricted || len(names) > 0 {
		return set, restricted, true
	}
	// GetTokenMCPScope joins the identifiers, so a scope whose every row
	// names a since-deleted identifier comes back empty and would read as
	// unrestricted. IsMCPItemInTokenScope counts the raw rows and treats
	// such a token as restricted to nothing, so the raw count decides
	// here too, and the actor fails closed with an empty set.
	hasRows, err := rc.authStore.HasTokenMCPScope(tokenID)
	if err != nil {
		return nil, false, false
	}
	if hasRows {
		return map[string]bool{}, true, true
	}
	return nil, false, true
}

// actorAdminScope reads the acting token's admin scope, on the same
// terms as actorMCPScope.
func (rc *RBACChecker) actorAdminScope(ctx context.Context) (
	set map[string]bool, restricted, ok bool) {

	tokenID, ok := rc.actingToken(ctx)
	if !ok {
		return nil, false, false
	}
	if tokenID <= 0 {
		return nil, false, true
	}
	perms, err := rc.authStore.GetTokenAdminScope(tokenID)
	if err != nil {
		return nil, false, false
	}
	set, restricted = namedScope(perms, AdminPermissionWildcard)
	return set, restricted, true
}

// CanGrantConnectionInTokenScope reports whether the acting API token's
// connection scope admits connectionID at the given access level, so
// that the token may grant that level on that connection to someone
// else. ConnectionIDAll asks about every connection, which only a token
// with no connection scope, or the wildcard at that level, covers.
func (rc *RBACChecker) CanGrantConnectionInTokenScope(ctx context.Context,
	connectionID int, level string) bool {

	if _, ok := rc.actingToken(ctx); !ok {
		return false
	}
	if !knownAccessLevel(level) || level == AccessLevelNone {
		return false
	}
	inScope, granted := rc.applyConnectionTokenScope(ctx, connectionID, level)
	return inScope && granted == level
}

// ConnectionReadableInTokenScope reports whether the acting API token's
// connection scope names connectionID at any access level, read
// included. Writes that change only Workbench metadata about a
// connection, such as its blackouts, need no more than read access to
// the monitored server, so they use this in place of
// ConnectionInTokenScope. ConnectionIDAll asks about every connection,
// which only a token with no connection scope, or the wildcard at
// either level, covers.
func (rc *RBACChecker) ConnectionReadableInTokenScope(ctx context.Context,
	connectionID int) bool {

	return rc.CanGrantConnectionInTokenScope(ctx, connectionID, AccessLevelRead)
}

// CanGrantMCPInTokenScope reports whether the acting API token's MCP
// scope covers the named MCP item, so that the token may grant it to a
// group. The wildcard "*" asks about every item, which only a token
// with no MCP scope, or the wildcard, covers.
func (rc *RBACChecker) CanGrantMCPInTokenScope(ctx context.Context,
	identifier string) bool {

	set, restricted, ok := rc.actorMCPScope(ctx)
	if !ok {
		return false
	}
	if !restricted {
		return true
	}
	return identifier != mcpScopeWildcard && set[identifier]
}

// TokenScopeUnrestricted reports whether the acting API token is
// unrestricted in all three scope kinds: every connection at
// read_write, and no MCP or admin scope, or the wildcard in each. That
// is what creating or promoting a superuser needs, since a superuser
// reaches everything. A session caller always passes.
func (rc *RBACChecker) TokenScopeUnrestricted(ctx context.Context) bool {
	if !rc.AllConnectionsInTokenScope(ctx) {
		return false
	}
	_, mcpRestricted, mcpOK := rc.actorMCPScope(ctx)
	_, adminRestricted, adminOK := rc.actorAdminScope(ctx)
	return mcpOK && adminOK && !mcpRestricted && !adminRestricted
}

// connectionGrantsInTokenScope reports whether every connection
// privilege in privs could be granted by the acting token.
func (rc *RBACChecker) connectionGrantsInTokenScope(ctx context.Context,
	privs map[int]string) bool {

	for connectionID, level := range privs {
		if !rc.CanGrantConnectionInTokenScope(ctx, connectionID, level) {
			return false
		}
	}
	return true
}

// principalReach is what a user, or membership of a group, reaches.
// asUser marks a user's reach, which also takes in what every user
// holds without a grant: the unrestricted connections the user may
// open, by sharing or ownership, and the public MCP items.
type principalReach struct {
	asUser    bool
	username  string
	superuser bool
	conns     map[int]string
	mcp       map[string]bool
	admin     map[string]bool
}

// loadUserReach reads what the given user reaches through the groups
// they belong to. ok is false when the user is unknown or a lookup
// fails.
func (rc *RBACChecker) loadUserReach(userID int64) (*principalReach, bool) {
	user, err := rc.authStore.GetUserByID(userID)
	if err != nil || user == nil {
		return nil, false
	}
	conns, err := rc.authStore.GetUserConnectionPrivileges(userID)
	if err != nil {
		return nil, false
	}
	mcp, err := rc.authStore.GetUserMCPPrivileges(userID)
	if err != nil {
		return nil, false
	}
	admin, err := rc.authStore.GetUserAdminPermissions(userID)
	if err != nil {
		return nil, false
	}
	return &principalReach{
		asUser:    true,
		username:  user.Username,
		superuser: user.IsSuperuser,
		conns:     conns,
		mcp:       mcp,
		admin:     admin,
	}, true
}

// loadGroupReach reads what membership of the given group confers,
// through its own grants and those of every group it belongs to.
func (rc *RBACChecker) loadGroupReach(groupID int64) (*principalReach, bool) {
	connPrivs, err := rc.authStore.GetGroupEffectiveConnectionPrivileges(groupID)
	if err != nil {
		return nil, false
	}
	mcpNames, err := rc.authStore.GetGroupEffectiveMCPPrivileges(groupID)
	if err != nil {
		return nil, false
	}
	adminPerms, err := rc.authStore.GetGroupEffectiveAdminPermissions(groupID)
	if err != nil {
		return nil, false
	}
	reach := &principalReach{
		conns: make(map[int]string, len(connPrivs)),
		mcp:   make(map[string]bool, len(mcpNames)),
		admin: make(map[string]bool, len(adminPerms)),
	}
	for _, cp := range connPrivs {
		reach.conns[cp.ConnectionID] = cp.AccessLevel
	}
	for _, name := range mcpNames {
		reach.mcp[name] = true
	}
	for _, perm := range adminPerms {
		reach.admin[perm] = true
	}
	return reach, true
}

// reachConnectionsInScope reports whether the connections p reaches all
// fall inside the acting token's connection scope.
//
// A superuser reaches every connection, and an admin permission acts
// across the whole estate, so either is only within a token that covers
// every connection. Otherwise each group connection grant must be
// grantable by the token, and, for a user, so must every unrestricted
// connection the user may open (see unrestrictedReachInTokenScope).
func (rc *RBACChecker) reachConnectionsInScope(ctx context.Context,
	p *principalReach, lister ConnectionVisibilityLister) bool {

	if rc.AllConnectionsInTokenScope(ctx) {
		return true
	}
	if p.superuser || len(p.admin) > 0 {
		return false
	}
	if !rc.connectionGrantsInTokenScope(ctx, p.conns) {
		return false
	}
	if !p.asUser {
		return true
	}
	return rc.unrestrictedReachInTokenScope(ctx, p.username, lister)
}

// OwnedClusterGroupLister enumerates the member connections of the
// cluster groups a user owns. The cluster group update and delete
// handlers admit a group's owner as they admit a holder of
// manage_connections, and either change reaches every member through
// the group-scope blackouts, overrides and settings it rewrites or
// drops, so those members are part of the owner's reach. The
// connection lister passed to the grant checks must implement it as
// well; one that does not fails the check closed.
type OwnedClusterGroupLister interface {
	GetOwnedClusterGroupConnectionIDs(ctx context.Context,
		username string) ([]int, error)
}

// ownedClusterGroupsInTokenScope reports whether every member connection
// of every cluster group username owns lies inside the acting token's
// connection scope at read_write. A lister that cannot enumerate the
// groups, or fails to, is out of scope.
func (rc *RBACChecker) ownedClusterGroupsInTokenScope(ctx context.Context,
	username string, lister ConnectionVisibilityLister) bool {

	if username == "" {
		return true
	}
	groups, ok := lister.(OwnedClusterGroupLister)
	if !ok {
		return false
	}
	members, err := groups.GetOwnedClusterGroupConnectionIDs(ctx, username)
	if err != nil {
		return false
	}
	for _, id := range members {
		if !rc.CanGrantConnectionInTokenScope(ctx, id, AccessLevelReadWrite) {
			return false
		}
	}
	return true
}

// unrestrictedReachInTokenScope reports whether every connection that
// username may open or change without a group grant lies inside the
// acting token's connection scope at read_write.
//
// CanAccessConnection admits any user to a connection no group holds a
// grant on, at read_write, when it is shared, and its owner when it is
// not; with no sharing lookup wired it admits any user to every such
// connection. The connection update and delete handlers go further and
// admit the owner whatever groups restrict the connection, so every
// connection a user owns is part of their reach, restricted or not, and
// is checked before the restriction is looked at. Ownership is matched
// by username, as those handlers match it, so a name that already owns
// a connection reaches it as soon as an account of that name exists.
// The cluster groups the user owns count in the same way (see
// OwnedClusterGroupLister). With no lister the connections cannot be
// enumerated, so the check fails closed.
func (rc *RBACChecker) unrestrictedReachInTokenScope(ctx context.Context,
	username string, lister ConnectionVisibilityLister) bool {

	if lister == nil {
		return false
	}
	if !rc.ownedClusterGroupsInTokenScope(ctx, username, lister) {
		return false
	}
	conns, err := lister.GetAllConnections(ctx)
	if err != nil {
		return false
	}
	sharingKnown := rc.connSharingLookupFn != nil
	for i := range conns {
		info := &conns[i]
		if username != "" && info.OwnerUsername == username {
			if !rc.CanGrantConnectionInTokenScope(ctx, info.ID, AccessLevelReadWrite) {
				return false
			}
			continue
		}
		if sharingKnown && !info.IsShared {
			continue
		}
		restricted, err := rc.authStore.IsConnectionAssignedToAnyGroup(info.ID)
		if err != nil {
			return false
		}
		if restricted {
			continue
		}
		if !rc.CanGrantConnectionInTokenScope(ctx, info.ID, AccessLevelReadWrite) {
			return false
		}
	}
	return true
}

// reachMCPInScope reports whether the MCP items p reaches all fall
// inside the acting token's MCP scope. A superuser, and a holder of the
// wildcard, reach every item; a user also reaches every public item.
func (rc *RBACChecker) reachMCPInScope(ctx context.Context,
	p *principalReach) bool {

	scope, restricted, ok := rc.actorMCPScope(ctx)
	if !ok {
		return false
	}
	if !restricted {
		return true
	}
	if p.superuser || p.mcp[mcpScopeWildcard] {
		return false
	}
	for name := range p.mcp {
		if !scope[name] {
			return false
		}
	}
	if !p.asUser {
		return true
	}
	privileges, err := rc.authStore.ListMCPPrivileges()
	if err != nil {
		return false
	}
	for _, priv := range privileges {
		if priv.IsPublic && !scope[priv.Identifier] {
			return false
		}
	}
	return true
}

// reachAdminInScope reports whether the admin permissions p holds all
// fall inside the acting token's admin scope. A superuser holds every
// permission.
func (rc *RBACChecker) reachAdminInScope(ctx context.Context,
	p *principalReach) bool {

	scope, restricted, ok := rc.actorAdminScope(ctx)
	if !ok {
		return false
	}
	if !restricted {
		return true
	}
	if p.superuser {
		return false
	}
	for perm := range p.admin {
		if !scope[perm] {
			return false
		}
	}
	return true
}

// reachInTokenScope checks all three kinds of p's reach.
func (rc *RBACChecker) reachInTokenScope(ctx context.Context,
	p *principalReach, lister ConnectionVisibilityLister) bool {

	return rc.reachConnectionsInScope(ctx, p, lister) &&
		rc.reachMCPInScope(ctx, p) &&
		rc.reachAdminInScope(ctx, p)
}

// UserWithinTokenScope reports whether everything the given user can
// reach falls inside the acting API token's scope, in all three kinds.
// It decides whether the token may change what is effectively the
// user's access, such as setting the user's password, re-enabling or
// deleting the account, or minting a token that acts as the user.
//
// The user's reach is their group grants, plus the unrestricted
// connections they may open and the public MCP items; lister
// enumerates the connections for the former, and a nil lister fails a
// token bounded by connection.
func (rc *RBACChecker) UserWithinTokenScope(ctx context.Context,
	userID int64, lister ConnectionVisibilityLister) bool {

	tokenID, ok := rc.actingToken(ctx)
	if !ok {
		return false
	}
	if tokenID <= 0 {
		return true
	}
	reach, ok := rc.loadUserReach(userID)
	if !ok {
		return false
	}
	return rc.reachInTokenScope(ctx, reach, lister)
}

// NewUserWithinTokenScope reports whether a new user called username,
// who belongs to no group, would reach only what the acting API token's
// scope covers. Such a user still reaches the unrestricted connections
// open to every user, any unshared connection already owned by that
// name, and the public MCP items, so a token bounded in either kind may
// create a user only when those fall inside it.
func (rc *RBACChecker) NewUserWithinTokenScope(ctx context.Context,
	username string, lister ConnectionVisibilityLister) bool {

	tokenID, ok := rc.actingToken(ctx)
	if !ok {
		return false
	}
	if tokenID <= 0 {
		return true
	}
	return rc.reachInTokenScope(ctx, &principalReach{
		asUser:   true,
		username: username,
	}, lister)
}

// GroupWithinTokenScope reports whether everything membership of the
// given group confers, through its own grants and those of every group
// it belongs to, falls inside the acting API token's scope in all three
// kinds. It decides whether the token may add a user or a group to the
// group.
func (rc *RBACChecker) GroupWithinTokenScope(ctx context.Context,
	groupID int64) bool {

	tokenID, ok := rc.actingToken(ctx)
	if !ok {
		return false
	}
	if tokenID <= 0 {
		return true
	}
	reach, ok := rc.loadGroupReach(groupID)
	if !ok {
		return false
	}
	return rc.reachInTokenScope(ctx, reach, nil)
}

// ScopedConnectionsInTokenScope reports whether every entry of a
// connection scope being written to another token could be granted by
// the acting token, so that the token being changed ends up no wider
// than the one changing it.
func (rc *RBACChecker) ScopedConnectionsInTokenScope(ctx context.Context,
	connections []ScopedConnection) bool {

	if _, ok := rc.actingToken(ctx); !ok {
		return false
	}
	for _, sc := range connections {
		if !rc.CanGrantConnectionInTokenScope(ctx, sc.ConnectionID, sc.AccessLevel) {
			return false
		}
	}
	return true
}

// namesWithinScope judges one named kind of a token's resulting scope
// against the acting token's scope for that kind. Explicit entries must
// each be in the acting token's set; no entries, or the wildcard, leave
// the token unrestricted in that kind, so ownerInScope, the owner's
// whole reach in it, decides.
func namesWithinScope(entries []string, wildcard string,
	scope map[string]bool, ownerInScope func() bool) bool {

	set, restricted := namedScope(entries, wildcard)
	if !restricted {
		return ownerInScope()
	}
	for entry := range set {
		if !scope[entry] {
			return false
		}
	}
	return true
}

// TokenScopeWithinTokenScope reports whether a token owned by ownerID,
// once its scope is the given one, reaches no further than the acting
// token, kind by kind. A token's reach is its owner's narrowed by its
// scope, so a kind with entries must itself fall inside the acting
// token's scope, and a kind left unrestricted needs the owner's whole
// reach in that kind to. Creating a token, which starts with no scope,
// and clearing one are the case where every kind is unrestricted.
func (rc *RBACChecker) TokenScopeWithinTokenScope(ctx context.Context,
	ownerID int64, scope GrantedTokenScope,
	lister ConnectionVisibilityLister) bool {

	tokenID, ok := rc.actingToken(ctx)
	if !ok {
		return false
	}
	if tokenID <= 0 {
		return true
	}
	owner, ok := rc.loadUserReach(ownerID)
	if !ok {
		return false
	}

	if len(scope.Connections) > 0 {
		if !rc.ScopedConnectionsInTokenScope(ctx, scope.Connections) {
			return false
		}
	} else if !rc.reachConnectionsInScope(ctx, owner, lister) {
		return false
	}

	mcpScope, mcpRestricted, ok := rc.actorMCPScope(ctx)
	if !ok {
		return false
	}
	if mcpRestricted && !namesWithinScope(scope.MCPPrivileges, mcpScopeWildcard,
		mcpScope, func() bool { return rc.reachMCPInScope(ctx, owner) }) {
		return false
	}

	adminScope, adminRestricted, ok := rc.actorAdminScope(ctx)
	if !ok {
		return false
	}
	if adminRestricted && !namesWithinScope(scope.AdminPermissions,
		AdminPermissionWildcard, adminScope,
		func() bool { return rc.reachAdminInScope(ctx, owner) }) {
		return false
	}
	return true
}
