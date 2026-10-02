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
	"testing"
)

// =============================================================================
// Grant checks bounded by the acting token's connection scope (issue #471)
// =============================================================================

// grantScopeFixture holds a store with a token scoped to connection 5
// at read_write and connection 7 read-only, plus a user and a group
// that the tests give grants to.
type grantScopeFixture struct {
	store   *AuthStore
	checker *RBACChecker
	tokenID int64
	userID  int64
	groupID int64

	// lister enumerates one shared connection, 5, which the token's
	// scope covers at read_write, so that a user's unrestricted reach
	// is in scope unless a test widens it.
	lister *stubVisibilityLister
}

func newGrantScopeFixture(t *testing.T) (*grantScopeFixture, func()) {
	t.Helper()

	store, cleanup := createTestAuthStoreForAccess(t)
	fail := func(format string, args ...any) {
		cleanup()
		t.Fatalf(format, args...)
	}

	if err := store.CreateServiceAccount("svc-actor", "", "", ""); err != nil {
		fail("CreateServiceAccount failed: %v", err)
	}
	_, token, err := store.CreateToken("svc-actor", "grant scope", nil)
	if err != nil {
		fail("CreateToken failed: %v", err)
	}
	if err := store.SetTokenConnectionScope(token.ID, []ScopedConnection{
		{ConnectionID: 5, AccessLevel: AccessLevelReadWrite},
		{ConnectionID: 7, AccessLevel: AccessLevelRead},
	}); err != nil {
		fail("SetTokenConnectionScope failed: %v", err)
	}

	if err := store.CreateUser("target", "Password1234", "", "", ""); err != nil {
		fail("CreateUser failed: %v", err)
	}
	userID, err := store.GetUserID("target")
	if err != nil {
		fail("GetUserID failed: %v", err)
	}
	groupID, err := store.CreateGroup("target-group", "")
	if err != nil {
		fail("CreateGroup failed: %v", err)
	}
	if err := store.AddUserToGroup(groupID, userID); err != nil {
		fail("AddUserToGroup failed: %v", err)
	}

	return &grantScopeFixture{
		store:   store,
		checker: NewRBACChecker(store),
		tokenID: token.ID,
		userID:  userID,
		groupID: groupID,
		lister: &stubVisibilityLister{connections: []ConnectionVisibilityInfo{
			{ID: 5, IsShared: true},
		}},
	}, cleanup
}

// tokenCtx presents the fixture's token as a superuser API token.
func (f *grantScopeFixture) tokenCtx() context.Context {
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, true)
	ctx = context.WithValue(ctx, IsAPITokenContextKey, true)
	return context.WithValue(ctx, TokenIDContextKey, f.tokenID)
}

func (f *grantScopeFixture) grant(t *testing.T, connectionID int, level string) {
	t.Helper()
	if err := f.store.GrantConnectionPrivilege(f.groupID, connectionID, level); err != nil {
		t.Fatalf("GrantConnectionPrivilege failed: %v", err)
	}
}

func TestCanGrantConnectionInTokenScope(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()

	session := context.WithValue(context.Background(), IsSuperuserContextKey, true)
	incomplete := context.WithValue(context.Background(), IsAPITokenContextKey, true)

	tests := []struct {
		name  string
		ctx   context.Context
		conn  int
		level string
		want  bool
	}{
		{"in scope at read_write", f.tokenCtx(), 5, AccessLevelReadWrite, true},
		{"in scope at read", f.tokenCtx(), 5, AccessLevelRead, true},
		{"read-only entry, read", f.tokenCtx(), 7, AccessLevelRead, true},
		{"read-only entry, read_write", f.tokenCtx(), 7, AccessLevelReadWrite, false},
		{"out of scope", f.tokenCtx(), 6, AccessLevelRead, false},
		{"every connection", f.tokenCtx(), ConnectionIDAll, AccessLevelRead, false},
		{"unknown level", f.tokenCtx(), 5, "owner", false},
		{"empty level", f.tokenCtx(), 5, AccessLevelNone, false},
		{"token context missing its id", incomplete, 5, AccessLevelRead, false},
		{"session", session, 6, AccessLevelReadWrite, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.checker.CanGrantConnectionInTokenScope(tt.ctx, tt.conn,
				tt.level); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	if NewRBACChecker(nil).CanGrantConnectionInTokenScope(f.tokenCtx(), 5,
		AccessLevelRead) {
		t.Error("A checker with no store should refuse every grant")
	}
}

func TestUserAndGroupWithinTokenScope(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()

	// No grants at all: nothing to exceed.
	if !f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
		t.Error("A user with no access should be within scope")
	}
	if !f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("A group with no access should be within scope")
	}

	f.grant(t, 5, AccessLevelReadWrite)
	f.grant(t, 7, AccessLevelRead)
	if !f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
		t.Error("A user whose access matches the scope should be within it")
	}
	if !f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("A group whose access matches the scope should be within it")
	}

	// read_write on the read-only entry goes beyond the scope.
	f.grant(t, 7, AccessLevelReadWrite)
	if f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
		t.Error("A user with read_write on a read-only entry should be out of scope")
	}
	if f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("A group with read_write on a read-only entry should be out of scope")
	}

	// A session caller is never bounded.
	session := context.WithValue(context.Background(), IsSuperuserContextKey, true)
	if !f.checker.UserWithinTokenScope(session, f.userID, f.lister) ||
		!f.checker.GroupWithinTokenScope(session, f.groupID) {
		t.Error("A session caller should always be within scope")
	}
}

func TestUserAndGroupWithinTokenScopeRefuseAdminReach(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()

	if err := f.store.GrantAdminPermission(f.groupID, PermManageBlackouts); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}
	if f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
		t.Error("A user holding an admin permission should be out of scope")
	}
	if f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("A group holding an admin permission should be out of scope")
	}
}

func TestUserWithinTokenScopeRefusesSuperuserAndUnknown(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()

	if f.checker.UserWithinTokenScope(ctx, 99999, f.lister) {
		t.Error("An unknown user should be out of scope")
	}
	if err := f.store.SetUserSuperuser("target", true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	if f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
		t.Error("A superuser should be out of a bounded token's scope")
	}
}

func TestGrantScopeChecksFailClosed(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()

	// A closed store makes every lookup fail, which must refuse.
	f.store.Close()
	if f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
		t.Error("UserWithinTokenScope should refuse when lookups fail")
	}
	if f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("GroupWithinTokenScope should refuse when lookups fail")
	}
}

func TestScopedConnectionsInTokenScope(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()

	if !f.checker.ScopedConnectionsInTokenScope(ctx, []ScopedConnection{
		{ConnectionID: 5, AccessLevel: AccessLevelReadWrite},
		{ConnectionID: 7, AccessLevel: AccessLevelRead},
	}) {
		t.Error("A scope within the acting token's should pass")
	}
	if f.checker.ScopedConnectionsInTokenScope(ctx, []ScopedConnection{
		{ConnectionID: 5, AccessLevel: AccessLevelReadWrite},
		{ConnectionID: 6, AccessLevel: AccessLevelRead},
	}) {
		t.Error("A scope naming a connection outside the acting token's should fail")
	}
}

func TestConnectionReadableInTokenScope(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()

	if !f.checker.ConnectionReadableInTokenScope(ctx, 5) ||
		!f.checker.ConnectionReadableInTokenScope(ctx, 7) {
		t.Error("Connections named in the scope at either level should be readable")
	}
	if f.checker.ConnectionReadableInTokenScope(ctx, 6) {
		t.Error("A connection outside the scope should not be readable")
	}
	if f.checker.ConnectionReadableInTokenScope(ctx, ConnectionIDAll) {
		t.Error("A scope without the wildcard should not cover every connection")
	}

	if err := f.store.SetTokenConnectionScope(f.tokenID, []ScopedConnection{
		{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelRead},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	if !f.checker.ConnectionReadableInTokenScope(ctx, ConnectionIDAll) {
		t.Error("The wildcard at read should cover every connection")
	}
	if f.checker.AllConnectionsInTokenScope(ctx) {
		t.Error("The wildcard at read should not pass the read_write check")
	}
}

// sessionCtx presents a superuser's session, which carries no token.
func sessionCtx() context.Context {
	return context.WithValue(context.Background(), IsSuperuserContextKey, true)
}

// incompleteTokenCtx presents an API token context that has lost its id.
func incompleteTokenCtx() context.Context {
	return context.WithValue(context.Background(), IsAPITokenContextKey, true)
}

func (f *grantScopeFixture) setMCPScope(t *testing.T, names ...string) {
	t.Helper()
	if err := f.store.SetTokenMCPScopeByNames(f.tokenID, names); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
	}
}

func (f *grantScopeFixture) setAdminScope(t *testing.T, perms ...string) {
	t.Helper()
	if err := f.store.SetTokenAdminScope(f.tokenID, perms); err != nil {
		t.Fatalf("SetTokenAdminScope failed: %v", err)
	}
}

func (f *grantScopeFixture) registerMCP(t *testing.T, name string, public bool) {
	t.Helper()
	if _, err := f.store.RegisterMCPPrivilege(name, MCPPrivilegeTypeTool,
		name, public); err != nil {
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}
}

// TestGrantScopeChecksRefuseWithoutStore checks that every grant check
// fails closed on a checker with no auth store, which cannot read any
// scope, whoever the caller is.
func TestGrantScopeChecksRefuseWithoutStore(t *testing.T) {
	rc := NewRBACChecker(nil)
	lister := &stubVisibilityLister{}
	for name, ctx := range map[string]context.Context{
		"session": sessionCtx(),
		"token": context.WithValue(context.WithValue(context.Background(),
			IsAPITokenContextKey, true), TokenIDContextKey, int64(1)),
	} {
		t.Run(name, func(t *testing.T) {
			checks := map[string]bool{
				"CanGrantConnection": rc.CanGrantConnectionInTokenScope(ctx, 1,
					AccessLevelRead),
				"ConnectionReadable":  rc.ConnectionReadableInTokenScope(ctx, 1),
				"CanGrantMCP":         rc.CanGrantMCPInTokenScope(ctx, "x"),
				"Unrestricted":        rc.TokenScopeUnrestricted(ctx),
				"UserWithin":          rc.UserWithinTokenScope(ctx, 1, lister),
				"NewUserWithin":       rc.NewUserWithinTokenScope(ctx, "x", lister),
				"GroupWithin":         rc.GroupWithinTokenScope(ctx, 1),
				"ScopedConnections":   rc.ScopedConnectionsInTokenScope(ctx, nil),
				"TokenScopeWithin":    rc.TokenScopeWithinTokenScope(ctx, 1, GrantedTokenScope{}, lister),
				"ConnectionInScope":   rc.ConnectionInTokenScope(ctx, 1),
				"AllConnectionsScope": rc.AllConnectionsInTokenScope(ctx),
			}
			for check, got := range checks {
				if got {
					t.Errorf("%s admitted the caller without a store", check)
				}
			}
		})
	}
}

// TestGrantScopeChecksRefuseIncompleteToken checks that a token context
// that has lost its id is out of scope for every grant check.
func TestGrantScopeChecksRefuseIncompleteToken(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := incompleteTokenCtx()

	if f.checker.CanGrantMCPInTokenScope(ctx, "x") ||
		f.checker.TokenScopeUnrestricted(ctx) ||
		f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) ||
		f.checker.NewUserWithinTokenScope(ctx, "new", f.lister) ||
		f.checker.GroupWithinTokenScope(ctx, f.groupID) ||
		f.checker.ScopedConnectionsInTokenScope(ctx, nil) ||
		f.checker.TokenScopeWithinTokenScope(ctx, f.userID,
			GrantedTokenScope{}, f.lister) {
		t.Error("A token context without an id should be out of scope")
	}
}

// TestUnrestrictedReachInTokenScope covers the connections a user
// reaches without a group grant: shared unrestricted connections, and
// unshared ones the user's name owns.
func TestUnrestrictedReachInTokenScope(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()

	// Connection 6 is outside the token's scope.
	shared := &stubVisibilityLister{connections: []ConnectionVisibilityInfo{
		{ID: 5, IsShared: true},
		{ID: 6, IsShared: true},
	}}
	if f.checker.NewUserWithinTokenScope(ctx, "eve", shared) {
		t.Error("A new user reaching a shared connection outside the scope should be refused")
	}
	if f.checker.UserWithinTokenScope(ctx, f.userID, shared) {
		t.Error("A user reaching a shared connection outside the scope should be refused")
	}
	if !f.checker.NewUserWithinTokenScope(sessionCtx(), "eve", shared) {
		t.Error("A session should be unbounded")
	}

	// Connection 7 is in scope, but only read-only, and an unrestricted
	// connection is open at read_write.
	readOnly := &stubVisibilityLister{connections: []ConnectionVisibilityInfo{
		{ID: 7, IsShared: true},
	}}
	if f.checker.NewUserWithinTokenScope(ctx, "eve", readOnly) {
		t.Error("A shared connection the scope holds read-only should be refused")
	}

	// A group grant on 6 restricts it, so a user without that grant no
	// longer reaches it.
	other, err := f.store.CreateGroup("other", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if err := f.store.GrantConnectionPrivilege(other, 6, AccessLevelRead); err != nil {
		t.Fatalf("GrantConnectionPrivilege failed: %v", err)
	}
	if !f.checker.NewUserWithinTokenScope(ctx, "eve", shared) {
		t.Error("A restricted connection should not count towards a new user's reach")
	}

	// Without a lister the connections cannot be enumerated.
	if f.checker.NewUserWithinTokenScope(ctx, "eve", nil) {
		t.Error("A missing lister should fail closed")
	}
	failing := &stubVisibilityLister{err: errors.New("boom")}
	if f.checker.NewUserWithinTokenScope(ctx, "eve", failing) {
		t.Error("A failing lister should fail closed")
	}

	// A token covering every connection needs no enumeration.
	if err := f.store.SetTokenConnectionScope(f.tokenID, []ScopedConnection{
		{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelReadWrite},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	if !f.checker.NewUserWithinTokenScope(ctx, "eve", nil) {
		t.Error("A token covering every connection should not need a lister")
	}
}

// TestUnrestrictedReachFollowsOwnership checks that, with sharing known,
// an unshared connection counts only towards its owner's reach, and
// that ownership is matched by name, so a new user taking an existing
// owner's name reaches that owner's connection.
func TestUnrestrictedReachFollowsOwnership(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	rc := NewRBACCheckerWithSharing(f.store,
		func(context.Context, int) (bool, string, error) { return false, "", nil })
	ctx := f.tokenCtx()

	lister := &stubVisibilityLister{connections: []ConnectionVisibilityInfo{
		{ID: 5, IsShared: true},
		{ID: 6, IsShared: false, OwnerUsername: "bob"},
	}}
	if !rc.NewUserWithinTokenScope(ctx, "eve", lister) {
		t.Error("Another user's unshared connection should not count")
	}
	if rc.NewUserWithinTokenScope(ctx, "bob", lister) {
		t.Error("A new user named after an owner reaches the owner's connection")
	}
	if !rc.UserWithinTokenScope(ctx, f.userID, lister) {
		t.Error("The target user owns nothing outside the scope")
	}

	// The update and delete handlers admit a connection's owner whatever
	// groups restrict it, so an owned connection counts towards reach
	// even when restricted, and needs read_write in the token's scope.
	other, err := f.store.CreateGroup("prod-dba", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if err := f.store.GrantConnectionPrivilege(other, 8, AccessLevelRead); err != nil {
		t.Fatalf("GrantConnectionPrivilege failed: %v", err)
	}
	owned := &stubVisibilityLister{connections: []ConnectionVisibilityInfo{
		{ID: 5, IsShared: true},
		{ID: 8, IsShared: true, OwnerUsername: "target"},
	}}
	if rc.UserWithinTokenScope(ctx, f.userID, owned) {
		t.Error("A user owning a restricted connection outside the scope should be refused")
	}
	if rc.NewUserWithinTokenScope(ctx, "target", owned) {
		t.Error("A new user named after the owner of a restricted connection should be refused")
	}
	if !rc.NewUserWithinTokenScope(ctx, "eve", owned) {
		t.Error("A restricted connection owned by someone else should not count")
	}
	readOnlyOwned := &stubVisibilityLister{connections: []ConnectionVisibilityInfo{
		{ID: 7, IsShared: false, OwnerUsername: "target"},
	}}
	if rc.UserWithinTokenScope(ctx, f.userID, readOnlyOwned) {
		t.Error("An owned connection the scope holds read-only should be refused")
	}
	ownedInScope := &stubVisibilityLister{connections: []ConnectionVisibilityInfo{
		{ID: 5, IsShared: false, OwnerUsername: "target"},
	}}
	if !rc.UserWithinTokenScope(ctx, f.userID, ownedInScope) {
		t.Error("An owned connection the scope holds at read_write should be allowed")
	}

	broken := &stubVisibilityLister{connections: []ConnectionVisibilityInfo{
		{ID: 5, IsShared: true},
	}}
	f.store.Close()
	if rc.NewUserWithinTokenScope(ctx, "eve", broken) {
		t.Error("A failing restriction lookup should fail closed")
	}
}

// TestCanGrantMCPInTokenScope covers the MCP grant bound.
func TestCanGrantMCPInTokenScope(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()
	f.registerMCP(t, "list_things", false)
	f.registerMCP(t, "run_things", false)

	if !f.checker.CanGrantMCPInTokenScope(ctx, "run_things") ||
		!f.checker.CanGrantMCPInTokenScope(ctx, mcpScopeWildcard) {
		t.Error("A token with no MCP scope should grant any item")
	}

	f.setMCPScope(t, "list_things")
	if !f.checker.CanGrantMCPInTokenScope(ctx, "list_things") {
		t.Error("An item in the MCP scope should be grantable")
	}
	if f.checker.CanGrantMCPInTokenScope(ctx, "run_things") {
		t.Error("An item outside the MCP scope should be refused")
	}
	if f.checker.CanGrantMCPInTokenScope(ctx, mcpScopeWildcard) {
		t.Error("The wildcard should be refused to an MCP-bounded token")
	}
	if !f.checker.CanGrantMCPInTokenScope(sessionCtx(), "run_things") {
		t.Error("A session should be unbounded")
	}

	f.setMCPScope(t, mcpScopeWildcard)
	if !f.checker.CanGrantMCPInTokenScope(ctx, "run_things") {
		t.Error("The MCP wildcard should grant any item")
	}

	f.store.Close()
	if f.checker.CanGrantMCPInTokenScope(ctx, "list_things") {
		t.Error("An unreadable MCP scope should fail closed")
	}
}

// TestActorMCPScopeFailsClosedOnOrphanedRows checks that a token whose
// only MCP scope row names a since-deleted identifier is treated as
// restricted to nothing, as IsMCPItemInTokenScope treats it, rather than
// as unrestricted because GetTokenMCPScope's join drops the row.
func TestActorMCPScopeFailsClosedOnOrphanedRows(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()
	f.registerMCP(t, "ghost_tool", false)
	f.registerMCP(t, "run_things", false)
	f.setMCPScope(t, "ghost_tool")
	mustExec(t, f.store, "DELETE FROM mcp_privilege_identifiers WHERE identifier = 'ghost_tool'")

	names, err := f.store.GetTokenMCPScope(f.tokenID)
	if err != nil || len(names) != 0 {
		t.Fatalf("Expected the orphaned row to be dropped by the join, got %v, %v", names, err)
	}
	if inScope, err := f.store.IsMCPItemInTokenScope(f.tokenID, "run_things"); err != nil || inScope {
		t.Fatalf("Expected the runtime check to refuse every item, got %v, %v", inScope, err)
	}

	if f.checker.CanGrantMCPInTokenScope(ctx, "run_things") {
		t.Error("An orphaned MCP scope should not let the token grant an item")
	}
	if f.checker.CanGrantMCPInTokenScope(ctx, mcpScopeWildcard) {
		t.Error("An orphaned MCP scope should not let the token grant the wildcard")
	}
	if f.checker.TokenScopeUnrestricted(ctx) {
		t.Error("A token with an orphaned MCP scope is not unrestricted")
	}
}

// TestHasTokenMCPScope covers the raw MCP scope row check and its
// failure.
func TestHasTokenMCPScope(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()

	if has, err := f.store.HasTokenMCPScope(f.tokenID); err != nil || has {
		t.Errorf("Expected no MCP scope, got %v, %v", has, err)
	}
	f.registerMCP(t, "run_things", false)
	f.setMCPScope(t, "run_things")
	if has, err := f.store.HasTokenMCPScope(f.tokenID); err != nil || !has {
		t.Errorf("Expected an MCP scope, got %v, %v", has, err)
	}

	dropScopeTable(t, f.store, "token_mcp_scope")
	if _, err := f.store.HasTokenMCPScope(f.tokenID); err == nil {
		t.Error("Expected an error once the MCP scope table is gone")
	}
}

// TestTokenScopeUnrestricted checks that only a token unrestricted in
// all three kinds passes.
func TestTokenScopeUnrestricted(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()
	f.registerMCP(t, "list_things", false)

	if f.checker.TokenScopeUnrestricted(ctx) {
		t.Error("A connection-bounded token should be restricted")
	}
	if !f.checker.TokenScopeUnrestricted(sessionCtx()) {
		t.Error("A session should be unrestricted")
	}

	if err := f.store.ClearTokenScope(f.tokenID); err != nil {
		t.Fatalf("ClearTokenScope failed: %v", err)
	}
	if !f.checker.TokenScopeUnrestricted(ctx) {
		t.Error("An unscoped token should be unrestricted")
	}
	f.setMCPScope(t, "list_things")
	if f.checker.TokenScopeUnrestricted(ctx) {
		t.Error("An MCP-bounded token should be restricted")
	}
	if err := f.store.ClearTokenScope(f.tokenID); err != nil {
		t.Fatalf("ClearTokenScope failed: %v", err)
	}
	f.setAdminScope(t, PermManageUsers)
	if f.checker.TokenScopeUnrestricted(ctx) {
		t.Error("An admin-bounded token should be restricted")
	}
	f.setAdminScope(t, AdminPermissionWildcard)
	f.setMCPScope(t, mcpScopeWildcard)
	if !f.checker.TokenScopeUnrestricted(ctx) {
		t.Error("Wildcards in every kind should be unrestricted")
	}
}

// TestReachMCPInTokenScope covers the MCP half of a user's and a
// group's reach.
func TestReachMCPInTokenScope(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()
	f.registerMCP(t, "list_things", false)
	f.registerMCP(t, "run_things", false)
	f.setMCPScope(t, "list_things")

	if !f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) ||
		!f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("No MCP grants should be within the scope")
	}
	if err := f.store.GrantMCPPrivilegeByName(f.groupID, "list_things"); err != nil {
		t.Fatalf("GrantMCPPrivilegeByName failed: %v", err)
	}
	if !f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) ||
		!f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("An MCP grant inside the scope should be within it")
	}
	if err := f.store.GrantMCPPrivilegeByName(f.groupID, "run_things"); err != nil {
		t.Fatalf("GrantMCPPrivilegeByName failed: %v", err)
	}
	if f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) ||
		f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("An MCP grant outside the scope should be refused")
	}
	if err := f.store.RevokeMCPPrivilegeByName(f.groupID, "run_things"); err != nil {
		t.Fatalf("RevokeMCPPrivilegeByName failed: %v", err)
	}
	if err := f.store.GrantMCPPrivilegeByName(f.groupID, mcpScopeWildcard); err != nil {
		t.Fatalf("GrantMCPPrivilegeByName failed: %v", err)
	}
	if f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("The MCP wildcard should be refused")
	}
}

// TestNewUserReachesPublicMCPItems checks that a public MCP item, which
// every user may call, counts towards a new user's reach.
func TestNewUserReachesPublicMCPItems(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()
	f.registerMCP(t, "list_things", false)
	f.registerMCP(t, "public_thing", true)
	f.setMCPScope(t, "list_things")

	if f.checker.NewUserWithinTokenScope(ctx, "eve", f.lister) {
		t.Error("A public item outside the MCP scope should be refused")
	}
	if !f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("Public items should not count towards a group's reach")
	}
	f.setMCPScope(t, "list_things", "public_thing")
	if !f.checker.NewUserWithinTokenScope(ctx, "eve", f.lister) {
		t.Error("Public items inside the MCP scope should be within it")
	}
	if err := f.store.SetUserSuperuser("target", true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	if f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
		t.Error("A superuser should be outside an MCP-bounded scope")
	}
}

// TestReachAdminInTokenScope covers the admin half of a group's reach
// under a token whose connection scope covers every connection, so
// that the admin scope alone decides.
func TestReachAdminInTokenScope(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()
	if err := f.store.ClearTokenScope(f.tokenID); err != nil {
		t.Fatalf("ClearTokenScope failed: %v", err)
	}
	f.setAdminScope(t, PermManageBlackouts)

	if err := f.store.GrantAdminPermission(f.groupID, PermManageBlackouts); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}
	if !f.checker.GroupWithinTokenScope(ctx, f.groupID) ||
		!f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
		t.Error("An admin permission inside the scope should be within it")
	}
	if err := f.store.GrantAdminPermission(f.groupID, PermManageUsers); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}
	if f.checker.GroupWithinTokenScope(ctx, f.groupID) ||
		f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
		t.Error("An admin permission outside the scope should be refused")
	}
	if err := f.store.SetUserSuperuser("target", true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	if f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
		t.Error("A superuser should be outside an admin-bounded scope")
	}

	f.store.Close()
	if f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("An unreadable admin scope should fail closed")
	}
}

// TestTokenScopeWithinTokenScope covers the bound on a token's scope
// once it is changed, kind by kind.
func TestTokenScopeWithinTokenScope(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()
	f.registerMCP(t, "list_things", false)
	f.registerMCP(t, "run_things", false)
	f.setMCPScope(t, "list_things")
	f.setAdminScope(t, PermManageBlackouts)

	inScope := []ScopedConnection{{ConnectionID: 5, AccessLevel: AccessLevelRead}}
	outScope := []ScopedConnection{{ConnectionID: 6, AccessLevel: AccessLevelRead}}

	tests := []struct {
		name  string
		scope GrantedTokenScope
		want  bool
	}{
		{"owner reach within every kind", GrantedTokenScope{}, true},
		{"explicit kinds within", GrantedTokenScope{Connections: inScope,
			MCPPrivileges:    []string{"list_things"},
			AdminPermissions: []string{PermManageBlackouts}}, true},
		{"connection outside", GrantedTokenScope{Connections: outScope}, false},
		{"MCP outside", GrantedTokenScope{MCPPrivileges: []string{"run_things"}}, false},
		{"MCP wildcard falls back to owner", GrantedTokenScope{
			MCPPrivileges: []string{mcpScopeWildcard}}, true},
		{"admin outside", GrantedTokenScope{
			AdminPermissions: []string{PermManageUsers}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.checker.TokenScopeWithinTokenScope(ctx, f.userID,
				tt.scope, f.lister); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	// The owner's own reach decides a kind the scope leaves open.
	if err := f.store.GrantMCPPrivilegeByName(f.groupID, "run_things"); err != nil {
		t.Fatalf("GrantMCPPrivilegeByName failed: %v", err)
	}
	if f.checker.TokenScopeWithinTokenScope(ctx, f.userID,
		GrantedTokenScope{}, f.lister) {
		t.Error("An owner reaching an MCP item outside the scope should be refused")
	}
	if !f.checker.TokenScopeWithinTokenScope(ctx, f.userID, GrantedTokenScope{
		MCPPrivileges: []string{"list_things"}}, f.lister) {
		t.Error("An explicit MCP scope should bound the owner's reach")
	}
	f.grant(t, 6, AccessLevelRead)
	if f.checker.TokenScopeWithinTokenScope(ctx, f.userID, GrantedTokenScope{
		MCPPrivileges: []string{"list_things"}}, f.lister) {
		t.Error("An owner reaching a connection outside the scope should be refused")
	}
	if err := f.store.GrantAdminPermission(f.groupID, PermManageUsers); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}
	if f.checker.TokenScopeWithinTokenScope(ctx, f.userID, GrantedTokenScope{
		Connections: inScope, MCPPrivileges: []string{"list_things"}}, f.lister) {
		t.Error("An owner holding an admin permission outside the scope should be refused")
	}

	if !f.checker.TokenScopeWithinTokenScope(sessionCtx(), f.userID,
		GrantedTokenScope{}, f.lister) {
		t.Error("A session should be unbounded")
	}
	if f.checker.TokenScopeWithinTokenScope(ctx, 99999, GrantedTokenScope{},
		f.lister) {
		t.Error("An unknown owner should be out of scope")
	}
}

// TestTokenScopeWithinTokenScopeFailsClosed checks the lookup failures.
func TestTokenScopeWithinTokenScopeFailsClosed(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()
	f.store.Close()
	if f.checker.TokenScopeWithinTokenScope(ctx, f.userID,
		GrantedTokenScope{}, f.lister) {
		t.Error("A failing lookup should fail closed")
	}
	if f.checker.NewUserWithinTokenScope(ctx, "eve", f.lister) {
		t.Error("A failing lookup should fail closed")
	}
}

// TestReachLookupsFailClosed drops each table a reach check reads, so
// that every lookup failure is shown to refuse.
func TestReachLookupsFailClosed(t *testing.T) {
	for _, table := range []string{
		"connection_privileges",
		"group_mcp_privileges",
		"group_admin_permissions",
		"token_admin_scope",
		"mcp_privilege_identifiers",
	} {
		t.Run(table, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			ctx := f.tokenCtx()
			f.registerMCP(t, "list_things", false)
			f.setMCPScope(t, "list_things")
			f.setAdminScope(t, PermManageBlackouts)

			dropAuthTable(t, f.store.db, table)
			if f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
				t.Error("UserWithinTokenScope should refuse")
			}
			if f.checker.GroupWithinTokenScope(ctx, f.groupID) {
				t.Error("GroupWithinTokenScope should refuse")
			}
			if table == "mcp_privilege_identifiers" &&
				f.checker.NewUserWithinTokenScope(ctx, "eve", f.lister) {
				t.Error("NewUserWithinTokenScope should refuse")
			}
		})
	}
}

// plainVisibilityLister enumerates connections but cannot list the
// cluster groups a user owns.
type plainVisibilityLister struct {
	connections []ConnectionVisibilityInfo
}

func (p plainVisibilityLister) GetAllConnections(context.Context) ([]ConnectionVisibilityInfo, error) {
	return p.connections, nil
}

// TestUnrestrictedReachCountsOwnedClusterGroups checks that the member
// connections of a cluster group a user owns count towards the user's
// reach, and that the check fails closed when the groups cannot be
// listed.
func TestUnrestrictedReachCountsOwnedClusterGroups(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()
	shared := []ConnectionVisibilityInfo{{ID: 5, IsShared: true}}

	tests := []struct {
		name   string
		lister ConnectionVisibilityLister
		want   bool
	}{
		{"no groups owned", &stubVisibilityLister{connections: shared}, true},
		{"members in scope at read_write", &stubVisibilityLister{
			connections: shared,
			ownedGroups: map[string][]int{"target": {5}},
		}, true},
		{"a member outside the scope", &stubVisibilityLister{
			connections: shared,
			ownedGroups: map[string][]int{"target": {5, 9}},
		}, false},
		{"a member the scope holds read-only", &stubVisibilityLister{
			connections: shared,
			ownedGroups: map[string][]int{"target": {7}},
		}, false},
		{"another user's group", &stubVisibilityLister{
			connections: shared,
			ownedGroups: map[string][]int{"bob": {9}},
		}, true},
		{"lookup failure", &stubVisibilityLister{
			connections: shared,
			ownedErr:    errors.New("boom"),
		}, false},
		{"lister without group listing", plainVisibilityLister{connections: shared}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.checker.UserWithinTokenScope(ctx, f.userID, tt.lister); got != tt.want {
				t.Errorf("UserWithinTokenScope = %v, want %v", got, tt.want)
			}
			if got := f.checker.NewUserWithinTokenScope(ctx, "target", tt.lister); got != tt.want {
				t.Errorf("NewUserWithinTokenScope = %v, want %v", got, tt.want)
			}
		})
	}

	if !f.checker.ownedClusterGroupsInTokenScope(ctx, "", plainVisibilityLister{}) {
		t.Error("An empty username owns no groups")
	}
}

// TestReachEverythingAdminPermissions checks that an admin permission
// able to acquire any MCP item or admin permission counts as reaching
// all of them, so that a token bounded in either kind may neither add
// someone to a group holding it nor take over a user who does.
func TestReachEverythingAdminPermissions(t *testing.T) {
	for _, perm := range reachEverythingAdminPermissions {
		t.Run(perm, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			ctx := f.tokenCtx()
			f.registerMCP(t, "list_things", false)
			if err := f.store.ClearTokenScope(f.tokenID); err != nil {
				t.Fatalf("ClearTokenScope failed: %v", err)
			}
			if err := f.store.GrantAdminPermission(f.groupID, perm); err != nil {
				t.Fatalf("GrantAdminPermission failed: %v", err)
			}

			if !f.checker.GroupWithinTokenScope(ctx, f.groupID) ||
				!f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
				t.Fatal("An unrestricted token should cover the holder")
			}

			f.setMCPScope(t, "list_things")
			if f.checker.GroupWithinTokenScope(ctx, f.groupID) ||
				f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
				t.Error("The holder should be outside an MCP-bounded scope")
			}

			if err := f.store.ClearTokenScope(f.tokenID); err != nil {
				t.Fatalf("ClearTokenScope failed: %v", err)
			}
			named := perm
			if named == AdminPermissionWildcard {
				named = PermManageUsers
			}
			f.setAdminScope(t, named, PermManageBlackouts)
			if f.checker.GroupWithinTokenScope(ctx, f.groupID) ||
				f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
				t.Error("The holder should be outside an admin-bounded " +
					"scope, even one naming the permission")
			}
		})
	}
}

// TestReachOrdinaryAdminPermissionUnderMCPScope is the control for
// TestReachEverythingAdminPermissions: an admin permission that cannot
// acquire MCP items leaves an MCP-bounded token free to add a holder.
func TestReachOrdinaryAdminPermissionUnderMCPScope(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()
	f.registerMCP(t, "list_things", false)
	if err := f.store.ClearTokenScope(f.tokenID); err != nil {
		t.Fatalf("ClearTokenScope failed: %v", err)
	}
	f.setMCPScope(t, "list_things")
	f.setAdminScope(t, PermManageBlackouts)
	if err := f.store.GrantAdminPermission(f.groupID, PermManageBlackouts); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}
	if !f.checker.GroupWithinTokenScope(ctx, f.groupID) ||
		!f.checker.UserWithinTokenScope(ctx, f.userID, f.lister) {
		t.Error("manage_blackouts should stay within a scope naming it")
	}
}
