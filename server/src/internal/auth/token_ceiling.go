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

// tokenCeiling is the access an acting API token may hand to someone
// else: its effective access, which is its owner's privileges narrowed
// by its scope, judged one connection, MCP item or admin permission at
// a time. Every grant check in grant_scope.go compares what is being
// granted against it, so that the comparison is made in one place.
//
// A session caller has no token, and the ruling of 6 October 2026
// leaves sessions bounded by their admin permissions alone, so a
// session's ceiling covers everything.
type tokenCeiling struct {
	rc      *RBACChecker
	ctx     context.Context
	session bool
}

// actorCeiling returns the acting caller's ceiling. ok is false, and
// every check built on it fails closed, for a nil checker, one with no
// auth store, and a token context that has lost its id.
func (rc *RBACChecker) actorCeiling(ctx context.Context) (*tokenCeiling, bool) {
	tokenID, ok := rc.actingToken(ctx)
	if !ok {
		return nil, false
	}
	return &tokenCeiling{rc: rc, ctx: ctx, session: tokenID <= 0}, true
}

// accessLevelRank orders the connection access levels, so that
// read_write covers read. A level this code does not understand ranks
// with none, so that a stray stored value can never cover anything.
func accessLevelRank(level string) int {
	switch level {
	case AccessLevelRead:
		return 1
	case AccessLevelReadWrite:
		return 2
	default:
		return 0
	}
}

// connectionLevel is the access the acting token has to one connection.
// ConnectionIDAll asks about every connection at once (see
// allConnectionsLevel).
func (c *tokenCeiling) connectionLevel(connectionID int) string {
	if c.session {
		return AccessLevelReadWrite
	}
	if connectionID == ConnectionIDAll {
		return c.allConnectionsLevel()
	}
	ok, level := c.rc.CanAccessConnection(c.ctx, connectionID)
	if !ok {
		return AccessLevelNone
	}
	return level
}

// allConnectionsLevel is the access the acting token has to every
// connection, present and future: the owner's "all connections" grant,
// which a superuser holds at read_write, narrowed by the token's own
// "all connections" scope entry. An owner with no such grant, or a
// token whose scope names only particular connections, holds none.
func (c *tokenCeiling) allConnectionsLevel() string {
	ownerLevel := AccessLevelNone
	if IsSuperuserFromContext(c.ctx) {
		ownerLevel = AccessLevelReadWrite
	} else if userID := GetUserIDFromContext(c.ctx); userID != 0 {
		privs, err := c.rc.authStore.GetUserConnectionPrivileges(userID)
		if err != nil {
			return AccessLevelNone
		}
		ownerLevel = privs[ConnectionIDAll]
	}
	if accessLevelRank(ownerLevel) == 0 {
		return AccessLevelNone
	}
	inScope, level := c.rc.applyConnectionTokenScope(c.ctx, ConnectionIDAll,
		ownerLevel)
	if !inScope {
		return AccessLevelNone
	}
	return level
}

// coversConnection reports whether the acting token holds connectionID
// at level or higher.
func (c *tokenCeiling) coversConnection(connectionID int, level string) bool {
	want := accessLevelRank(level)
	return want > 0 && accessLevelRank(c.connectionLevel(connectionID)) >= want
}

// ownerHolds reports whether the token's owner holds the wildcard of a
// named kind, as a superuser holds every wildcard; holds reads the
// owner's grants for that kind.
func (c *tokenCeiling) ownerHolds(wildcard string,
	holds func(userID int64) (map[string]bool, error)) bool {

	if IsSuperuserFromContext(c.ctx) {
		return true
	}
	userID := GetUserIDFromContext(c.ctx)
	if userID == 0 {
		return false
	}
	granted, err := holds(userID)
	return err == nil && granted[wildcard]
}

// holdsAllMCP reports whether the acting token reaches every MCP item,
// present and future: its owner holds the wildcard and its MCP scope is
// unrestricted.
func (c *tokenCeiling) holdsAllMCP() bool {
	if c.session {
		return true
	}
	_, restricted, ok := c.rc.actorMCPScope(c.ctx)
	return ok && !restricted &&
		c.ownerHolds(mcpScopeWildcard, c.rc.authStore.GetUserMCPPrivileges)
}

// holdsAllAdmin reports whether the acting token holds every admin
// permission, on the same terms as holdsAllMCP.
func (c *tokenCeiling) holdsAllAdmin() bool {
	if c.session {
		return true
	}
	_, restricted, ok := c.rc.actorAdminScope(c.ctx)
	return ok && !restricted &&
		c.ownerHolds(AdminPermissionWildcard, c.rc.authStore.GetUserAdminPermissions)
}

// coversMCP reports whether the acting token may call the named MCP
// item. The wildcard asks about every item.
func (c *tokenCeiling) coversMCP(identifier string) bool {
	if c.session {
		return true
	}
	if identifier == mcpScopeWildcard {
		return c.holdsAllMCP()
	}
	return c.rc.CanAccessMCPItem(c.ctx, identifier)
}

// coversAdmin reports whether the acting token holds the named admin
// permission. The wildcard asks about every permission.
func (c *tokenCeiling) coversAdmin(permission string) bool {
	if c.session {
		return true
	}
	if permission == AdminPermissionWildcard {
		return c.holdsAllAdmin()
	}
	return c.rc.HasAdminPermission(c.ctx, permission)
}

// holdsEverything reports whether the acting token reaches everything a
// superuser does: every connection at read_write, every MCP item and
// every admin permission, now and later.
func (c *tokenCeiling) holdsEverything() bool {
	return c.coversConnection(ConnectionIDAll, AccessLevelReadWrite) &&
		c.holdsAllMCP() && c.holdsAllAdmin()
}

// holdsSuperuser reports whether the acting token may make someone a
// superuser: it must be a superuser's token that reaches everything.
func (c *tokenCeiling) holdsSuperuser() bool {
	return c.session || (IsSuperuserFromContext(c.ctx) && c.holdsEverything())
}

// coversReach reports whether everything p reaches lies within the
// ceiling, in all three kinds. It is the one comparison behind every
// check that hands a principal, or a token, a set of access at once.
func (c *tokenCeiling) coversReach(p *principalReach,
	lister ConnectionVisibilityLister) bool {

	if p.superuser && !c.holdsSuperuser() {
		return false
	}
	return c.coversReachConnections(p, lister) && c.coversReachMCP(p) &&
		c.coversReachAdmin(p)
}

// coversReachConnections reports whether the connections p reaches lie
// within the ceiling.
//
// A superuser reaches every connection, and an admin permission acts
// across the whole estate, so either needs every connection at
// read_write. Otherwise each connection grant must be covered at its
// level, and, for a user, so must every connection the user may open or
// change without a grant (see unrestrictedReachCovered).
func (c *tokenCeiling) coversReachConnections(p *principalReach,
	lister ConnectionVisibilityLister) bool {

	if c.coversConnection(ConnectionIDAll, AccessLevelReadWrite) {
		return true
	}
	if p.superuser || len(p.admin) > 0 {
		return false
	}
	for connectionID, level := range p.conns {
		if !c.coversConnection(connectionID, level) {
			return false
		}
	}
	if !p.asUser {
		return true
	}
	return c.unrestrictedReachCovered(p.username, lister)
}

// coversReachMCP reports whether the MCP items p reaches lie within the
// ceiling. A holder of the wildcard reaches every item, as does anyone
// p.reachesEverything covers; a user also reaches every public item.
func (c *tokenCeiling) coversReachMCP(p *principalReach) bool {
	if c.holdsAllMCP() {
		return true
	}
	if p.reachesEverything() || p.mcp[mcpScopeWildcard] {
		return false
	}
	for name := range p.mcp {
		if !c.coversMCP(name) {
			return false
		}
	}
	if !p.asUser {
		return true
	}
	privileges, err := c.rc.authStore.ListMCPPrivileges()
	if err != nil {
		return false
	}
	for _, priv := range privileges {
		if priv.IsPublic && !c.coversMCP(priv.Identifier) {
			return false
		}
	}
	return true
}

// coversReachAdmin reports whether the admin permissions p holds lie
// within the ceiling. A superuser holds every permission, and a holder
// of one that can acquire the rest reaches them (see reachesEverything).
func (c *tokenCeiling) coversReachAdmin(p *principalReach) bool {
	if c.holdsAllAdmin() {
		return true
	}
	if p.reachesEverything() {
		return false
	}
	for perm := range p.admin {
		if !c.coversAdmin(perm) {
			return false
		}
	}
	return true
}

// ownedClusterGroupsCovered reports whether every member connection of
// every cluster group username owns is within the ceiling at
// read_write. A lister that cannot enumerate the groups, or fails to,
// is not.
func (c *tokenCeiling) ownedClusterGroupsCovered(username string,
	lister ConnectionVisibilityLister) bool {

	if username == "" {
		return true
	}
	groups, ok := lister.(OwnedClusterGroupLister)
	if !ok {
		return false
	}
	members, err := groups.GetOwnedClusterGroupConnectionIDs(c.ctx, username)
	if err != nil {
		return false
	}
	for _, id := range members {
		if !c.coversConnection(id, AccessLevelReadWrite) {
			return false
		}
	}
	return true
}

// unrestrictedReachCovered reports whether every connection username may
// open or change without a group grant is within the ceiling at
// read_write.
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
func (c *tokenCeiling) unrestrictedReachCovered(username string,
	lister ConnectionVisibilityLister) bool {

	if lister == nil {
		return false
	}
	if !c.ownedClusterGroupsCovered(username, lister) {
		return false
	}
	conns, err := lister.GetAllConnections(c.ctx)
	if err != nil {
		return false
	}
	sharingKnown := c.rc.connSharingLookupFn != nil
	for i := range conns {
		info := &conns[i]
		if username != "" && info.OwnerUsername == username {
			if !c.coversConnection(info.ID, AccessLevelReadWrite) {
				return false
			}
			continue
		}
		if sharingKnown && !info.IsShared {
			continue
		}
		restricted, err := c.rc.authStore.IsConnectionAssignedToAnyGroup(info.ID)
		if err != nil {
			return false
		}
		if restricted {
			continue
		}
		if !c.coversConnection(info.ID, AccessLevelReadWrite) {
			return false
		}
	}
	return true
}
