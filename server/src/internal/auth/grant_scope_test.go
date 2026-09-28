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

	if !NewRBACChecker(nil).CanGrantConnectionInTokenScope(f.tokenCtx(), 6,
		AccessLevelReadWrite) {
		t.Error("A checker with no store should admit every grant")
	}
}

func TestUserAndGroupWithinTokenScope(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()

	// No grants at all: nothing to exceed.
	if !f.checker.UserWithinTokenScope(ctx, f.userID) {
		t.Error("A user with no access should be within scope")
	}
	if !f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("A group with no access should be within scope")
	}

	f.grant(t, 5, AccessLevelReadWrite)
	f.grant(t, 7, AccessLevelRead)
	if !f.checker.UserWithinTokenScope(ctx, f.userID) {
		t.Error("A user whose access matches the scope should be within it")
	}
	if !f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("A group whose access matches the scope should be within it")
	}

	// read_write on the read-only entry goes beyond the scope.
	f.grant(t, 7, AccessLevelReadWrite)
	if f.checker.UserWithinTokenScope(ctx, f.userID) {
		t.Error("A user with read_write on a read-only entry should be out of scope")
	}
	if f.checker.GroupWithinTokenScope(ctx, f.groupID) {
		t.Error("A group with read_write on a read-only entry should be out of scope")
	}

	// A session caller is never bounded.
	session := context.WithValue(context.Background(), IsSuperuserContextKey, true)
	if !f.checker.UserWithinTokenScope(session, f.userID) ||
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
	if f.checker.UserWithinTokenScope(ctx, f.userID) {
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

	if f.checker.UserWithinTokenScope(ctx, 99999) {
		t.Error("An unknown user should be out of scope")
	}
	if err := f.store.SetUserSuperuser("target", true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	if f.checker.UserWithinTokenScope(ctx, f.userID) {
		t.Error("A superuser should be out of a bounded token's scope")
	}
}

func TestGrantScopeChecksFailClosed(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()

	// A closed store makes every lookup fail, which must refuse.
	f.store.Close()
	if f.checker.UserWithinTokenScope(ctx, f.userID) {
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
