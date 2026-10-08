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
	"errors"
)

// The checks in this file bound what an API token may hand out. An
// admin permission such as manage_permissions or manage_users says
// nothing about which connections, MCP items or admin permissions a
// token may reach, so without them a token scoped to one connection
// could grant a group access to every connection, create a user who
// reaches every shared connection, or hand a group an MCP tool the
// token itself may not call (issue #471).
//
// The admin permission is the first gate, checked by the handler. The
// checks here are the second: what a token grants must lie within its
// ceiling, the access the token itself has, which is its owner's
// privileges narrowed by its scope (see tokenCeiling). That is judged
// per connection and per level, so a token with read on a connection
// may grant read on it and no more, and one with read_write may grant
// either. Taking access away needs only read on the connection
// concerned, and is refused in the same words whether or not the
// connection exists, so that a refusal never reveals one the token
// cannot see. A token with read on a connection and manage_permissions
// may therefore revoke another group's read_write on it; that is the
// accepted cost of letting a token narrow what it can see.
//
// Session callers are bounded by their admin permissions alone and
// pass every check. Like the other scope checks these fail closed: a
// checker with no auth store, a token context missing its id, and a
// lookup that fails all refuse.

// mcpScopeWildcard is the name GetTokenMCPScope and
// GetGroupEffectiveMCPPrivileges report for the "all MCP items" grant,
// and the name an HTTP request uses to ask for it.
const mcpScopeWildcard = "*"

// actingToken reports how a grant check should treat the caller: as a
// session (no token, so nothing to bound), as a token to check, or as
// out of scope outright because the checker is nil or has no store, or
// the token context has lost its id.
func (rc *RBACChecker) actingToken(ctx context.Context) (tokenID int64, ok bool) {
	if rc == nil || rc.authStore == nil || tokenContextIncomplete(ctx) {
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

// reachEverythingAdminPermissions are the admin permissions that let
// their holder acquire any MCP item or admin permission for themselves:
// manage_users takes over any account by setting its password,
// manage_groups joins any group, manage_permissions grants any MCP item
// or admin permission to a group the holder belongs to, and
// manage_token_scopes widens any token's scope. The admin wildcard
// includes all four.
var reachEverythingAdminPermissions = []string{
	PermManageUsers,
	PermManageGroups,
	PermManagePermissions,
	PermManageTokenScopes,
	AdminPermissionWildcard,
}

// reachesEverything reports whether p reaches every MCP item and admin
// permission, being a superuser or holding an admin permission that can
// acquire them (see reachEverythingAdminPermissions). A name-by-name
// comparison would miss what such a holder can grant themselves, so
// only a token unrestricted in the kind covers it.
func (p *principalReach) reachesEverything() bool {
	if p.superuser {
		return true
	}
	for _, perm := range reachEverythingAdminPermissions {
		if p.admin[perm] {
			return true
		}
	}
	return false
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

// ErrTokenScopeUnreadable reports that the scope or owner of the token
// whose scope is being changed could not be read, so the change could
// not be judged at all. The handler answers it as a server error rather
// than a refusal.
var ErrTokenScopeUnreadable = errors.New("token scope could not be read")

// TokenScopeChange is a change to a token's scope, by kind. A nil kind
// is left as it is; a kind with entries replaces what is stored.
type TokenScopeChange struct {
	Connections      []ScopedConnection
	MCPPrivileges    []string
	AdminPermissions []string
}

// ConnectionReadableInTokenScope reports whether the acting API token's
// connection scope names connectionID at any access level, read
// included. Writes that change only Workbench metadata about a
// connection, such as its blackouts, need no more than read access to
// the monitored server, so they use this in place of
// ConnectionInTokenScope. ConnectionIDAll asks about every connection,
// which only a token with no connection scope, or the wildcard at
// either level, covers. Unlike the grant checks it reads the scope
// alone, since those handlers decide the owner's side themselves.
func (rc *RBACChecker) ConnectionReadableInTokenScope(ctx context.Context,
	connectionID int) bool {

	if _, ok := rc.actingToken(ctx); !ok {
		return false
	}
	inScope, level := rc.applyConnectionTokenScope(ctx, connectionID,
		AccessLevelRead)
	return inScope && level == AccessLevelRead
}

// CanGrantConnection reports whether the acting caller may grant level
// on connectionID: a token needs that level or higher on the connection
// itself. ConnectionIDAll asks about the "all connections" grant, which
// needs the token to hold every connection at that level.
func (rc *RBACChecker) CanGrantConnection(ctx context.Context,
	connectionID int, level string) bool {

	c, ok := rc.actorCeiling(ctx)
	return ok && c.coversConnection(connectionID, level)
}

// CanRevokeConnection reports whether the acting caller may remove the
// group's grant on connectionID. A token needs read on the connection,
// so that it cannot learn whether a connection it may not see exists.
// When the grant is the connection's last, removing it lifts the group
// restriction and opens a shared connection to every user, so the token
// then needs read_write, as granting that would; whether the grant is
// the last can change under a concurrent revoke, so the store decides
// that in the revoke's own transaction, from ConnectionLiftGuard.
func (rc *RBACChecker) CanRevokeConnection(ctx context.Context,
	groupID int64, connectionID int) bool {

	c, ok := rc.actorCeiling(ctx)
	if !ok {
		return false
	}
	return c.session || c.coversConnection(connectionID, AccessLevelRead)
}

// ConnectionLiftGuard returns the guard a revoke or group delete passes
// to the store: a session may lift the restriction on any connection,
// and a token only on those of connectionIDs it holds at read_write.
// A caller whose ceiling cannot be read may lift none.
func (rc *RBACChecker) ConnectionLiftGuard(ctx context.Context,
	connectionIDs ...int) *LiftGuard {

	c, ok := rc.actorCeiling(ctx)
	if !ok {
		return AllowLifts()
	}
	if c.session {
		return AllowAllLifts()
	}
	var allowed []int
	for _, id := range connectionIDs {
		if c.coversConnection(id, AccessLevelReadWrite) {
			allowed = append(allowed, id)
		}
	}
	return AllowLifts(allowed...)
}

// connectionsReadable reports whether the ceiling holds read on every
// connection membership of the group confers, through its own grants
// and those of every group it belongs to.
func (c *tokenCeiling) connectionsReadable(groupID int64) bool {
	privs, err := c.rc.authStore.GetGroupEffectiveConnectionPrivileges(groupID)
	if err != nil {
		return false
	}
	for _, cp := range privs {
		if !c.coversConnection(cp.ConnectionID, AccessLevelRead) {
			return false
		}
	}
	return true
}

// CanRemoveGroupMember reports whether the acting caller may remove a
// user or a group from the group. Removal takes away the connections
// membership confers, so, as for a revoke, a token needs read on each
// of them. The MCP items and admin permissions removal takes away need
// nothing, as revoking them directly needs nothing.
func (rc *RBACChecker) CanRemoveGroupMember(ctx context.Context,
	groupID int64) bool {

	c, ok := rc.actorCeiling(ctx)
	if !ok {
		return false
	}
	return c.session || c.connectionsReadable(groupID)
}

// CanDeleteGroup reports whether the acting caller may delete the
// group. Deleting it takes away what membership conferred, so a token
// needs read on each of those connections. It also drops the group's
// own grants, and each that is a connection's last needs read_write, as
// revoking it alone would; the store decides that in the delete's own
// transaction, from GroupDeleteLiftGuard.
func (rc *RBACChecker) CanDeleteGroup(ctx context.Context, groupID int64) bool {
	c, ok := rc.actorCeiling(ctx)
	if !ok {
		return false
	}
	return c.session || c.connectionsReadable(groupID)
}

// GroupDeleteLiftGuard returns the guard a group delete passes to the
// store, covering the connections of the group's own grants. A grant
// added after this lookup is not in the guard, so the store refuses the
// delete if dropping it would lift a restriction, which fails closed.
func (rc *RBACChecker) GroupDeleteLiftGuard(ctx context.Context,
	groupID int64) *LiftGuard {

	own, err := rc.authStore.ListGroupConnectionPrivileges(groupID)
	if err != nil {
		return AllowLifts()
	}
	ids := make([]int, 0, len(own))
	for _, cp := range own {
		ids = append(ids, cp.ConnectionID)
	}
	return rc.ConnectionLiftGuard(ctx, ids...)
}

// CanGrantMCPItem reports whether the acting caller may grant the named
// MCP item to a group: a token must be able to call it itself. The
// wildcard "*" needs a token that reaches every item.
func (rc *RBACChecker) CanGrantMCPItem(ctx context.Context,
	identifier string) bool {

	c, ok := rc.actorCeiling(ctx)
	return ok && c.coversMCP(identifier)
}

// CanGrantAdminPermission reports whether the acting caller may grant
// the named admin permission to a group, judged as what membership of a
// group holding only that permission would reach. A token must hold the
// permission, and, since an admin permission acts across the whole
// estate, every connection at read_write. manage_users, manage_groups,
// manage_permissions, manage_token_scopes and the wildcard each let a
// holder acquire every MCP item and admin permission (see
// reachEverythingAdminPermissions), so granting one needs a token that
// reaches every item and permission as well; otherwise a token bounded
// only in its MCP scope could plant manage_permissions on a group and,
// through a member's session, grant the items it was refused.
func (rc *RBACChecker) CanGrantAdminPermission(ctx context.Context,
	permission string) bool {

	c, ok := rc.actorCeiling(ctx)
	if !ok {
		return false
	}
	return c.coversReach(&principalReach{
		admin: map[string]bool{permission: true},
	}, nil)
}

// TokenHoldsEverything reports whether the acting caller reaches
// everything a superuser does, so that it may create or promote a
// superuser: a session, or a superuser's token that is unrestricted in
// all three scope kinds. A token owned by any other user holds only
// what its owner does, however wide its scope.
func (rc *RBACChecker) TokenHoldsEverything(ctx context.Context) bool {
	c, ok := rc.actorCeiling(ctx)
	return ok && c.holdsSuperuser()
}

// UserWithinTokenScope reports whether everything the given user can
// reach lies within the acting API token's ceiling, in all three kinds.
// It decides whether the token may change what is effectively the
// user's access, such as setting the user's password, re-enabling or
// deleting the account, or minting a token that acts as the user.
//
// The user's reach is their group grants, plus the unrestricted
// connections they may open and the public MCP items; lister
// enumerates the connections for the former, and a nil lister fails a
// token that does not hold every connection.
func (rc *RBACChecker) UserWithinTokenScope(ctx context.Context,
	userID int64, lister ConnectionVisibilityLister) bool {

	c, ok := rc.actorCeiling(ctx)
	if !ok {
		return false
	}
	if c.session {
		return true
	}
	reach, ok := rc.loadUserReach(userID)
	return ok && c.coversReach(reach, lister)
}

// NewUserWithinTokenScope reports whether a new user called username,
// who belongs to no group, would reach only what lies within the acting
// API token's ceiling. Such a user still reaches the unrestricted
// connections open to every user, any connection already owned by that
// name, and the public MCP items.
func (rc *RBACChecker) NewUserWithinTokenScope(ctx context.Context,
	username string, lister ConnectionVisibilityLister) bool {

	c, ok := rc.actorCeiling(ctx)
	if !ok {
		return false
	}
	return c.coversReach(&principalReach{asUser: true, username: username},
		lister)
}

// GroupWithinTokenScope reports whether everything membership of the
// given group confers, through its own grants and those of every group
// it belongs to, lies within the acting API token's ceiling in all
// three kinds. Adding a member grants that whole set, so it decides
// whether the token may add a user or a group to the group.
func (rc *RBACChecker) GroupWithinTokenScope(ctx context.Context,
	groupID int64) bool {

	c, ok := rc.actorCeiling(ctx)
	if !ok {
		return false
	}
	if c.session {
		return true
	}
	reach, ok := rc.loadGroupReach(groupID)
	return ok && c.coversReach(reach, nil)
}

// storedTokenScope is a token's scope as stored, kind by kind, with
// whether each kind is restricted.
type storedTokenScope struct {
	ownerID         int64
	ownerSuperuser  bool
	conns           map[int]string
	connsRestricted bool
	mcp             map[string]bool
	mcpRestricted   bool
	admin           map[string]bool
	adminRestricted bool
}

// loadStoredTokenScope reads the scope stored for tokenID. found is
// false for a token that does not exist; err is ErrTokenScopeUnreadable
// when a lookup fails.
func (rc *RBACChecker) loadStoredTokenScope(tokenID int64) (
	scope *storedTokenScope, found bool, err error) {

	token, err := rc.authStore.GetTokenByID(tokenID)
	if err != nil {
		return nil, false, ErrTokenScopeUnreadable
	}
	if token == nil {
		return nil, false, nil
	}
	owner, err := rc.authStore.GetUserByID(token.OwnerID)
	if err != nil || owner == nil {
		return nil, false, ErrTokenScopeUnreadable
	}
	scope = &storedTokenScope{ownerID: token.OwnerID,
		ownerSuperuser: owner.IsSuperuser, conns: map[int]string{}}

	conns, err := rc.authStore.GetTokenScope(tokenID)
	if err != nil {
		return nil, false, ErrTokenScopeUnreadable
	}
	if conns != nil {
		for _, sc := range conns.Connections {
			scope.conns[sc.ConnectionID] = sc.AccessLevel
		}
	}
	scope.connsRestricted = len(scope.conns) > 0

	names, err := rc.authStore.GetTokenMCPScope(tokenID)
	if err != nil {
		return nil, false, ErrTokenScopeUnreadable
	}
	scope.mcp, scope.mcpRestricted = namedScope(names, mcpScopeWildcard)
	if !scope.mcpRestricted && len(names) == 0 {
		// See actorMCPScope: rows naming only deleted identifiers still
		// restrict the token, to nothing.
		hasRows, err := rc.authStore.HasTokenMCPScope(tokenID)
		if err != nil {
			return nil, false, ErrTokenScopeUnreadable
		}
		if hasRows {
			scope.mcp, scope.mcpRestricted = map[string]bool{}, true
		}
	}

	perms, err := rc.authStore.GetTokenAdminScope(tokenID)
	if err != nil {
		return nil, false, ErrTokenScopeUnreadable
	}
	scope.admin, scope.adminRestricted = namedScope(perms,
		AdminPermissionWildcard)
	return scope, true, nil
}

// connectionLevel is the level the stored scope allows on connectionID,
// as IsConnectionInTokenScope reads it: an entry for the connection
// itself, else the "all connections" entry, and read_write when the
// kind is unrestricted, since the owner's level then decides.
//
// ConnectionIDAll asks about every connection at once, which a legacy
// mixed scope holds only at the lowest level any of its entries records:
// {all: read_write, 5: read} holds connection 5 at read, so it holds
// every connection at read, as allConnectionsLevel reads the acting
// token's own scope.
func (s *storedTokenScope) connectionLevel(connectionID int) string {
	if !s.connsRestricted {
		return AccessLevelReadWrite
	}
	if connectionID == ConnectionIDAll {
		level, ok := s.conns[ConnectionIDAll]
		if !ok {
			return AccessLevelNone
		}
		for _, entry := range s.conns {
			if accessLevelRank(entry) < accessLevelRank(level) {
				level = entry
			}
		}
		return level
	}
	if level, ok := s.conns[connectionID]; ok {
		return level
	}
	return s.conns[ConnectionIDAll]
}

// connectionsChangeCovered judges a new connection scope for a token.
// Each entry is either granted, and so must be within the ceiling, or
// keeps or narrows what the token already allows, which needs only
// read on the connection, as a revoke does. Each entry the change drops
// is a narrowing too, and needs read in the same way.
//
// The "all connections" entry is judged against the level the stored
// scope holds every connection at (see storedTokenScope.connectionLevel),
// so writing {all: read_write} over a legacy {all: read_write, 5: read}
// raises connection 5 and needs every connection at read_write.
func (c *tokenCeiling) connectionsChangeCovered(stored *storedTokenScope,
	entries []ScopedConnection) bool {

	kept := make(map[int]bool, len(entries))
	for _, sc := range entries {
		kept[sc.ConnectionID] = true
		if c.coversConnection(sc.ConnectionID, sc.AccessLevel) {
			continue
		}
		if !c.coversConnection(sc.ConnectionID, AccessLevelRead) ||
			accessLevelRank(sc.AccessLevel) >
				accessLevelRank(stored.connectionLevel(sc.ConnectionID)) {
			return false
		}
	}
	for connectionID := range stored.conns {
		if !kept[connectionID] &&
			!c.coversConnection(connectionID, AccessLevelRead) {
			return false
		}
	}
	return true
}

// widens reports whether change gives the token anything its stored
// scope does not: a connection entry above the level the stored scope
// allows on that connection, or, in a restricted named kind, an entry
// the token does not already hold. Narrowing an unrestricted kind never
// widens it.
func (s *storedTokenScope) widens(change TokenScopeChange) bool {
	for _, sc := range change.Connections {
		if accessLevelRank(sc.AccessLevel) >
			accessLevelRank(s.connectionLevel(sc.ConnectionID)) {
			return true
		}
	}
	return namesWiden(change.MCPPrivileges, s.mcp, s.mcpRestricted) ||
		namesWiden(change.AdminPermissions, s.admin, s.adminRestricted)
}

// namesWiden reports whether entries name anything a restricted named
// scope kind does not already hold. The wildcard is never in current,
// since namedScope reports a kind holding it as unrestricted, so adding
// it to a restricted kind widens it.
func namesWiden(entries []string, current map[string]bool,
	restricted bool) bool {

	if !restricted {
		return false
	}
	for _, entry := range entries {
		if !current[entry] {
			return true
		}
	}
	return false
}

// namesChangeCovered judges a new named scope kind for a token. Narrowing
// an unrestricted kind takes nothing from the ceiling; otherwise each
// entry the token does not already hold must be covered.
func namesChangeCovered(entries []string, wildcard string,
	current map[string]bool, restricted bool, covers func(string) bool) bool {

	if !restricted {
		return true
	}
	for _, entry := range entries {
		if entry != wildcard && current[entry] {
			continue
		}
		if !covers(entry) {
			return false
		}
	}
	return true
}

// TokenScopeChangeWithinCeiling reports whether the acting caller may
// change the scope of token tokenID as given, the acting token's own
// scope included. Only the kinds the change supplies are judged: what
// it grants beyond the token's current scope must lie within the acting
// token's ceiling, and what it narrows needs read on each connection
// concerned (see connectionsChangeCovered). A token that does not exist
// is refused; err is ErrTokenScopeUnreadable when the stored scope
// cannot be read.
func (rc *RBACChecker) TokenScopeChangeWithinCeiling(ctx context.Context,
	tokenID int64, change TokenScopeChange) (bool, error) {

	c, ok := rc.actorCeiling(ctx)
	if !ok {
		return false, nil
	}
	if c.session {
		return true, nil
	}
	stored, found, err := rc.loadStoredTokenScope(tokenID)
	if err != nil || !found {
		return false, err
	}
	// A superuser's token is bounded by its scope alone, so widening it
	// hands out superuser access, which only a token that may make
	// someone a superuser holds to give (see holdsSuperuser).
	if stored.ownerSuperuser && !c.holdsSuperuser() && stored.widens(change) {
		return false, nil
	}
	if change.Connections != nil &&
		!c.connectionsChangeCovered(stored, change.Connections) {
		return false, nil
	}
	if change.MCPPrivileges != nil &&
		!namesChangeCovered(change.MCPPrivileges, mcpScopeWildcard,
			stored.mcp, stored.mcpRestricted, c.coversMCP) {
		return false, nil
	}
	if change.AdminPermissions != nil &&
		!namesChangeCovered(change.AdminPermissions, AdminPermissionWildcard,
			stored.admin, stored.adminRestricted, c.coversAdmin) {
		return false, nil
	}
	return true, nil
}

// TokenScopeClearWithinCeiling reports whether the acting caller may
// clear the scope of token tokenID, which leaves it with its owner's
// whole access. Each kind the token is restricted in today must then
// have the owner's whole reach in that kind within the acting token's
// ceiling, and each connection entry dropped needs read, as any
// narrowing does. Errors are as for TokenScopeChangeWithinCeiling.
func (rc *RBACChecker) TokenScopeClearWithinCeiling(ctx context.Context,
	tokenID int64, lister ConnectionVisibilityLister) (bool, error) {

	c, ok := rc.actorCeiling(ctx)
	if !ok {
		return false, nil
	}
	if c.session {
		return true, nil
	}
	stored, found, err := rc.loadStoredTokenScope(tokenID)
	if err != nil || !found {
		return false, err
	}
	owner, ok := rc.loadUserReach(stored.ownerID)
	if !ok {
		return false, ErrTokenScopeUnreadable
	}
	// Clearing any restriction of a superuser's token widens it towards
	// superuser access, as in TokenScopeChangeWithinCeiling.
	if owner.superuser && !c.holdsSuperuser() &&
		(stored.connsRestricted || stored.mcpRestricted ||
			stored.adminRestricted) {
		return false, nil
	}
	if stored.connsRestricted &&
		(!c.connectionsChangeCovered(stored, nil) ||
			!c.coversReachConnections(owner, lister)) {
		return false, nil
	}
	if stored.mcpRestricted && !c.coversReachMCP(owner) {
		return false, nil
	}
	if stored.adminRestricted && !c.coversReachAdmin(owner) {
		return false, nil
	}
	return true, nil
}
