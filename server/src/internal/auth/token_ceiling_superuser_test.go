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
	"errors"
	"testing"
)

// everythingOwner gives the fixture's owner, who is not a superuser,
// every connection at read_write and the MCP and admin wildcards, so
// that the owner's unscoped token holds everything a superuser does
// without being a superuser's token.
func (f *grantScopeFixture) everythingOwner(t *testing.T) *ownerFixture {
	t.Helper()
	o := f.newOwner(t, []ScopedConnection{{ConnectionID: ConnectionIDAll,
		AccessLevel: AccessLevelReadWrite}}, nil)
	if err := f.store.GrantMCPPrivilegeByName(o.groupID,
		mcpScopeWildcard); err != nil {
		t.Fatalf("GrantMCPPrivilegeByName failed: %v", err)
	}
	if err := f.store.GrantAdminPermission(o.groupID,
		AdminPermissionWildcard); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}
	c, ok := f.checker.actorCeiling(o.ctx())
	if !ok || !c.holdsEverything() || c.holdsSuperuser() {
		t.Fatal("Expected the owner's token to hold everything without " +
			"holding superuser")
	}
	return o
}

// superuserToken issues a token to a new superuser, scoped to manage_users,
// connection 5 at read and the MCP item list_things.
func (f *grantScopeFixture) superuserToken(t *testing.T) int64 {
	t.Helper()
	if err := f.store.CreateUser("root", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := f.store.SetUserSuperuser("root", true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	_, token, err := f.store.CreateToken("root", "root token", nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	f.registerMCP(t, "list_things", false)
	if err := f.store.SetTokenAdminScope(token.ID,
		[]string{PermManageUsers}); err != nil {
		t.Fatalf("SetTokenAdminScope failed: %v", err)
	}
	if err := f.store.SetTokenConnectionScope(token.ID, []ScopedConnection{
		{ConnectionID: 5, AccessLevel: AccessLevelRead}}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	if err := f.store.SetTokenMCPScopeByNames(token.ID,
		[]string{"list_things"}); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
	}
	return token.ID
}

// TestTokenScopeChangeSuperuserOwnedTarget checks that a token holding
// everything, but not a superuser's, may narrow a superuser's token but
// never widen it in any kind: a superuser's token is bounded by its scope
// alone, so widening it hands out superuser access.
func TestTokenScopeChangeSuperuserOwnedTarget(t *testing.T) {
	tests := []struct {
		name   string
		change TokenScopeChange
		want   bool
	}{
		{"admin wildcard", TokenScopeChange{
			AdminPermissions: []string{AdminPermissionWildcard}}, false},
		{"another admin permission", TokenScopeChange{
			AdminPermissions: []string{PermManageUsers, PermManageGroups}}, false},
		{"connection to read_write", TokenScopeChange{
			Connections: []ScopedConnection{{ConnectionID: 5,
				AccessLevel: AccessLevelReadWrite}}}, false},
		{"all connections", TokenScopeChange{
			Connections: []ScopedConnection{{ConnectionID: ConnectionIDAll,
				AccessLevel: AccessLevelRead}}}, false},
		{"MCP wildcard", TokenScopeChange{
			MCPPrivileges: []string{mcpScopeWildcard}}, false},
		{"keep the admin scope", TokenScopeChange{
			AdminPermissions: []string{PermManageUsers}}, true},
		{"keep connection 5 at read", TokenScopeChange{
			Connections: []ScopedConnection{{ConnectionID: 5,
				AccessLevel: AccessLevelRead}}}, true},
		{"keep the MCP scope", TokenScopeChange{
			MCPPrivileges: []string{"list_things"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			o := f.everythingOwner(t)
			target := f.superuserToken(t)

			got, err := f.checker.TokenScopeChangeWithinCeiling(o.ctx(),
				target, tt.change)
			if err != nil {
				t.Fatalf("TokenScopeChangeWithinCeiling failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTokenScopeChangeNonSuperuserTargetStillWidens checks that the
// superuser rule leaves a token holding everything free to widen the
// token of an owner who is not a superuser.
func TestTokenScopeChangeNonSuperuserTargetStillWidens(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	o := f.everythingOwner(t)
	target := f.targetToken(t)
	if err := f.store.SetTokenAdminScope(target,
		[]string{PermManageUsers}); err != nil {
		t.Fatalf("SetTokenAdminScope failed: %v", err)
	}

	got, err := f.checker.TokenScopeChangeWithinCeiling(o.ctx(), target,
		TokenScopeChange{AdminPermissions: []string{AdminPermissionWildcard}})
	if err != nil || !got {
		t.Errorf("Expected the widening to be allowed, got %v, %v", got, err)
	}
	got, err = f.checker.TokenScopeClearWithinCeiling(o.ctx(), target, nil)
	if err != nil || !got {
		t.Errorf("Expected the clear to be allowed, got %v, %v", got, err)
	}
}

// TestTokenScopeClearSuperuserOwnedTarget checks that a token holding
// everything, but not a superuser's, may not clear a superuser token's
// scope, whilst a superuser's token holding everything may.
func TestTokenScopeClearSuperuserOwnedTarget(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	o := f.everythingOwner(t)
	target := f.superuserToken(t)

	got, err := f.checker.TokenScopeClearWithinCeiling(o.ctx(), target, nil)
	if err != nil {
		t.Fatalf("TokenScopeClearWithinCeiling failed: %v", err)
	}
	if got {
		t.Error("A token that is not a superuser's must not clear a " +
			"superuser token's scope")
	}

	if err := f.store.ClearTokenScope(f.tokenID); err != nil {
		t.Fatalf("ClearTokenScope failed: %v", err)
	}
	got, err = f.checker.TokenScopeClearWithinCeiling(f.tokenCtx(), target, nil)
	if err != nil || !got {
		t.Errorf("Expected an unscoped superuser token to clear it, got %v, %v",
			got, err)
	}
}

// TestTokenScopeChangeOwnerUnreadable checks that a token whose owner
// cannot be read is refused with ErrTokenScopeUnreadable, since whether
// the owner is a superuser decides the rule.
func TestTokenScopeChangeOwnerUnreadable(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	o := f.everythingOwner(t)
	target := f.targetToken(t)
	mustExec(t, f.store, "ALTER TABLE users RENAME TO users_gone")

	_, err := f.checker.TokenScopeChangeWithinCeiling(o.ctx(), target,
		TokenScopeChange{AdminPermissions: []string{PermManageUsers}})
	if !errors.Is(err, ErrTokenScopeUnreadable) {
		t.Errorf("Expected ErrTokenScopeUnreadable, got %v", err)
	}
}
