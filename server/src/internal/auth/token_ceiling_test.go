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

// ownerFixture is a non-superuser, "owner", whose token the ceiling
// tests act through, so that the owner's privileges and the token's
// scope both bound what the token may grant.
type ownerFixture struct {
	userID  int64
	groupID int64
	tokenID int64
}

// newOwner creates the owner, gives the owner's group the given
// connection grants, and issues the owner a token with the given
// connection scope (nil leaves it unscoped).
func (f *grantScopeFixture) newOwner(t *testing.T, grants []ScopedConnection,
	scope []ScopedConnection) *ownerFixture {

	t.Helper()
	if err := f.store.CreateUser("owner", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	userID, err := f.store.GetUserID("owner")
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}
	groupID, err := f.store.CreateGroup("owner-group", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if err := f.store.AddUserToGroup(groupID, userID); err != nil {
		t.Fatalf("AddUserToGroup failed: %v", err)
	}
	for _, g := range grants {
		if err := f.store.GrantConnectionPrivilege(groupID, g.ConnectionID,
			g.AccessLevel); err != nil {
			t.Fatalf("GrantConnectionPrivilege failed: %v", err)
		}
	}
	_, token, err := f.store.CreateToken("owner", "owner token", nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	if scope != nil {
		if err := f.store.SetTokenConnectionScope(token.ID, scope); err != nil {
			t.Fatalf("SetTokenConnectionScope failed: %v", err)
		}
	}
	return &ownerFixture{userID: userID, groupID: groupID, tokenID: token.ID}
}

// ctx presents the owner's token as the API token of a non-superuser.
func (o *ownerFixture) ctx() context.Context {
	ctx := context.WithValue(context.Background(), IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, TokenIDContextKey, o.tokenID)
	ctx = context.WithValue(ctx, UserIDContextKey, o.userID)
	return context.WithValue(ctx, UsernameContextKey, "owner")
}

// TestCeilingIntersectsOwnerAndScope checks that a token owned by a
// non-superuser may grant a connection only at a level both its owner
// and its scope hold.
func TestCeilingIntersectsOwnerAndScope(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()

	// Connection 7 is restricted to the target group, so the owner does
	// not reach it; 8 the owner holds but the scope leaves out.
	f.grant(t, 7, AccessLevelReadWrite)
	o := f.newOwner(t, []ScopedConnection{
		{ConnectionID: 5, AccessLevel: AccessLevelRead},
		{ConnectionID: 6, AccessLevel: AccessLevelReadWrite},
		{ConnectionID: 8, AccessLevel: AccessLevelReadWrite},
	}, []ScopedConnection{
		{ConnectionID: 5, AccessLevel: AccessLevelReadWrite},
		{ConnectionID: 6, AccessLevel: AccessLevelRead},
		{ConnectionID: 7, AccessLevel: AccessLevelReadWrite},
	})
	ctx := o.ctx()

	tests := []struct {
		name  string
		conn  int
		level string
		want  bool
	}{
		{"owner read, scope read_write: read", 5, AccessLevelRead, true},
		{"owner read, scope read_write: read_write", 5, AccessLevelReadWrite, false},
		{"owner read_write, scope read: read", 6, AccessLevelRead, true},
		{"owner read_write, scope read: read_write", 6, AccessLevelReadWrite, false},
		{"scoped but owner has no access", 7, AccessLevelRead, false},
		{"owner holds but out of scope", 8, AccessLevelRead, false},
		{"all connections", ConnectionIDAll, AccessLevelRead, false},
		{"unknown level", 5, "bogus", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.checker.CanGrantConnection(ctx, tt.conn, tt.level); got != tt.want {
				t.Errorf("CanGrantConnection(%d, %s) = %v, want %v",
					tt.conn, tt.level, got, tt.want)
			}
		})
	}
}

// TestCeilingUnscopedTokenBoundByOwner checks that a token with no
// scope at all is still bounded by its owner's privileges.
func TestCeilingUnscopedTokenBoundByOwner(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	o := f.newOwner(t, []ScopedConnection{
		{ConnectionID: 5, AccessLevel: AccessLevelRead},
	}, nil)
	ctx := o.ctx()

	if !f.checker.CanGrantConnection(ctx, 5, AccessLevelRead) {
		t.Error("The owner's read should be grantable")
	}
	if f.checker.CanGrantConnection(ctx, 5, AccessLevelReadWrite) {
		t.Error("An unscoped token must not grant beyond its owner's read")
	}
	if f.checker.TokenHoldsEverything(ctx) {
		t.Error("A non-superuser's token must never hold everything")
	}
}

// TestCeilingAllConnections checks the "all connections" level for each
// combination of owner and scope.
func TestCeilingAllConnections(t *testing.T) {
	tests := []struct {
		name      string
		ownerAll  string // the owner group's "all connections" grant
		superuser bool
		scope     []ScopedConnection
		wantRead  bool
		wantWrite bool
	}{
		{"non-superuser all read_write, unscoped", AccessLevelReadWrite, false,
			nil, true, true},
		{"non-superuser all read, unscoped", AccessLevelRead, false,
			nil, true, false},
		{"non-superuser all read_write, scope all read", AccessLevelReadWrite,
			false, []ScopedConnection{{ConnectionID: ConnectionIDAll,
				AccessLevel: AccessLevelRead}}, true, false},
		{"non-superuser no all grant", "", false, nil, false, false},
		{"non-superuser all read_write, scope names one connection",
			AccessLevelReadWrite, false, []ScopedConnection{{ConnectionID: 5,
				AccessLevel: AccessLevelReadWrite}}, false, false},
		{"superuser unscoped", "", true, nil, true, true},
		{"superuser scope all read", "", true, []ScopedConnection{{
			ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelRead}},
			true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			var grants []ScopedConnection
			if tt.ownerAll != "" {
				grants = []ScopedConnection{{ConnectionID: ConnectionIDAll,
					AccessLevel: tt.ownerAll}}
			}
			o := f.newOwner(t, grants, tt.scope)
			ctx := o.ctx()
			if tt.superuser {
				ctx = context.WithValue(ctx, IsSuperuserContextKey, true)
			}
			if got := f.checker.CanGrantConnection(ctx, ConnectionIDAll,
				AccessLevelRead); got != tt.wantRead {
				t.Errorf("all connections read = %v, want %v", got, tt.wantRead)
			}
			if got := f.checker.CanGrantConnection(ctx, ConnectionIDAll,
				AccessLevelReadWrite); got != tt.wantWrite {
				t.Errorf("all connections read_write = %v, want %v", got,
					tt.wantWrite)
			}
		})
	}
}

// TestCeilingAllConnectionsFailsClosed checks that a failing lookup of
// the owner's grants leaves the token with no "all connections" level.
func TestCeilingAllConnectionsFailsClosed(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	o := f.newOwner(t, []ScopedConnection{{ConnectionID: ConnectionIDAll,
		AccessLevel: AccessLevelReadWrite}}, nil)
	dropAuthTable(t, f.store.db, "connection_privileges")
	if f.checker.CanGrantConnection(o.ctx(), ConnectionIDAll, AccessLevelRead) {
		t.Error("A failing lookup should fail closed")
	}
}

// TestRevokeLiftsConnectionRestriction covers the store query behind
// the revoke rule.
func TestRevokeLiftsConnectionRestriction(t *testing.T) {
	tests := []struct {
		name       string
		own        []ScopedConnection // the target group's grants
		other      []ScopedConnection // another group's grants
		conn       int
		wholeGroup bool
		want       bool
	}{
		{"last grant", []ScopedConnection{{ConnectionID: 5,
			AccessLevel: AccessLevelRead}}, nil, 5, false, true},
		{"another group grants it", []ScopedConnection{{ConnectionID: 5,
			AccessLevel: AccessLevelRead}}, []ScopedConnection{{ConnectionID: 5,
			AccessLevel: AccessLevelRead}}, 5, false, false},
		{"another group grants all connections", []ScopedConnection{{
			ConnectionID: 5, AccessLevel: AccessLevelRead}},
			[]ScopedConnection{{ConnectionID: ConnectionIDAll,
				AccessLevel: AccessLevelRead}}, 5, false, false},
		{"not restricted at all", nil, nil, 5, false, false},
		{"own all-connections grant survives a single revoke",
			[]ScopedConnection{{ConnectionID: 5, AccessLevel: AccessLevelRead},
				{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelRead}},
			nil, 5, false, false},
		{"whole group takes both", []ScopedConnection{{ConnectionID: 5,
			AccessLevel: AccessLevelRead}, {ConnectionID: ConnectionIDAll,
			AccessLevel: AccessLevelRead}}, nil, 5, true, true},
		{"last all-connections grant", []ScopedConnection{{
			ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelRead}},
			nil, ConnectionIDAll, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			for _, g := range tt.own {
				f.grant(t, g.ConnectionID, g.AccessLevel)
			}
			if tt.other != nil {
				f.newOwner(t, tt.other, nil)
			}
			got, err := f.store.RevokeLiftsConnectionRestriction(f.groupID,
				tt.conn, tt.wholeGroup)
			if err != nil {
				t.Fatalf("RevokeLiftsConnectionRestriction failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRevokeLiftsConnectionRestrictionFailsClosed checks the error path.
func TestRevokeLiftsConnectionRestrictionFailsClosed(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	dropAuthTable(t, f.store.db, "connection_privileges")
	if _, err := f.store.RevokeLiftsConnectionRestriction(f.groupID, 5,
		false); err == nil {
		t.Error("Expected an error from a missing table")
	}
	if _, err := f.store.RevokeLiftsConnectionRestriction(f.groupID, 5,
		true); err == nil {
		t.Error("Expected an error from a missing table")
	}
	if f.checker.CanRevokeConnection(f.tokenCtx(), f.groupID, 5) {
		t.Error("A failing lookup should refuse the revoke")
	}
}

// TestCanRevokeConnection covers the revoke rule: read on the
// connection, and read_write when the revoke lifts its restriction.
// The fixture's token holds 5 at read_write and 7 read-only.
func TestCanRevokeConnection(t *testing.T) {
	tests := []struct {
		name   string
		own    []ScopedConnection
		shared bool // another group also grants the connection
		conn   int
		want   bool
	}{
		{"last grant, token read_write", []ScopedConnection{{ConnectionID: 5,
			AccessLevel: AccessLevelReadWrite}}, false, 5, true},
		{"last grant, token read only", []ScopedConnection{{ConnectionID: 7,
			AccessLevel: AccessLevelReadWrite}}, false, 7, false},
		{"not the last grant, token read only", []ScopedConnection{{
			ConnectionID: 7, AccessLevel: AccessLevelReadWrite}}, true, 7, true},
		{"connection out of scope", []ScopedConnection{{ConnectionID: 9,
			AccessLevel: AccessLevelRead}}, true, 9, false},
		{"connection that does not exist", nil, false, 999, false},
		{"last all-connections grant", []ScopedConnection{{
			ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelRead}},
			false, ConnectionIDAll, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			for _, g := range tt.own {
				f.grant(t, g.ConnectionID, g.AccessLevel)
			}
			if tt.shared {
				f.newOwner(t, []ScopedConnection{{ConnectionID: tt.conn,
					AccessLevel: AccessLevelRead}}, nil)
			}
			if got := f.checker.CanRevokeConnection(f.tokenCtx(), f.groupID,
				tt.conn); got != tt.want {
				t.Errorf("CanRevokeConnection = %v, want %v", got, tt.want)
			}
			if !f.checker.CanRevokeConnection(sessionCtx(), f.groupID, tt.conn) {
				t.Error("A session should always pass")
			}
		})
	}
}

// TestCanRemoveMemberAndDeleteGroup covers removal: read on every
// connection membership confers, and, for a delete, read_write on each
// grant that is a connection's last.
func TestCanRemoveMemberAndDeleteGroup(t *testing.T) {
	tests := []struct {
		name       string
		grants     []ScopedConnection
		wantRemove bool
		wantDelete bool
	}{
		{"no grants", nil, true, true},
		{"last grant within read_write", []ScopedConnection{{ConnectionID: 5,
			AccessLevel: AccessLevelReadWrite}}, true, true},
		{"last grant on a read-only connection", []ScopedConnection{{
			ConnectionID: 7, AccessLevel: AccessLevelRead}}, true, false},
		{"grant out of scope", []ScopedConnection{{ConnectionID: 9,
			AccessLevel: AccessLevelRead}}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			for _, g := range tt.grants {
				f.grant(t, g.ConnectionID, g.AccessLevel)
			}
			ctx := f.tokenCtx()
			if got := f.checker.CanRemoveGroupMember(ctx, f.groupID); got != tt.wantRemove {
				t.Errorf("CanRemoveGroupMember = %v, want %v", got, tt.wantRemove)
			}
			if got := f.checker.CanDeleteGroup(ctx, f.groupID); got != tt.wantDelete {
				t.Errorf("CanDeleteGroup = %v, want %v", got, tt.wantDelete)
			}
			if !f.checker.CanRemoveGroupMember(sessionCtx(), f.groupID) ||
				!f.checker.CanDeleteGroup(sessionCtx(), f.groupID) {
				t.Error("A session should always pass")
			}
		})
	}
}

// TestCanRemoveMemberCountsParentGroups checks that what a parent group
// confers counts, since removal from the child takes it away too.
func TestCanRemoveMemberCountsParentGroups(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	parent, err := f.store.CreateGroup("parent", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if err := f.store.AddGroupToGroup(parent, f.groupID); err != nil {
		t.Fatalf("AddGroupToGroup failed: %v", err)
	}
	if err := f.store.GrantConnectionPrivilege(parent, 9,
		AccessLevelRead); err != nil {
		t.Fatalf("GrantConnectionPrivilege failed: %v", err)
	}
	if f.checker.CanRemoveGroupMember(f.tokenCtx(), f.groupID) {
		t.Error("A parent's out-of-scope grant should refuse the removal")
	}
}

// TestCanDeleteGroupFailsClosed checks the lookup failures.
func TestCanDeleteGroupFailsClosed(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	dropAuthTable(t, f.store.db, "connection_privileges")
	if f.checker.CanDeleteGroup(f.tokenCtx(), f.groupID) {
		t.Error("A failing lookup should refuse the delete")
	}
	if f.checker.CanRemoveGroupMember(f.tokenCtx(), f.groupID) {
		t.Error("A failing lookup should refuse the removal")
	}
}

// TestCanGrantAdminPermission covers admin grants: the permission
// itself, every connection at read_write, and for a permission that
// reaches everything, every MCP item and admin permission too.
func TestCanGrantAdminPermission(t *testing.T) {
	tests := []struct {
		name       string
		conns      bool // leave the token's connection scope in place
		mcpScope   []string
		adminScope []string
		perm       string
		want       bool
	}{
		{"unscoped, ordinary", false, nil, nil, PermManageBlackouts, true},
		{"unscoped, reaches everything", false, nil, nil, PermManagePermissions, true},
		{"unscoped, wildcard", false, nil, nil, AdminPermissionWildcard, true},
		{"connection scope", true, nil, nil, PermManageBlackouts, false},
		{"MCP scope, ordinary", false, []string{"list_things"}, nil,
			PermManageBlackouts, true},
		{"MCP scope, reaches everything", false, []string{"list_things"}, nil,
			PermManagePermissions, false},
		{"admin scope, held", false, nil, []string{PermManageBlackouts},
			PermManageBlackouts, true},
		{"admin scope, not held", false, nil, []string{PermManageBlackouts},
			PermManageUsers, false},
		{"admin scope, wildcard", false, nil, []string{PermManagePermissions},
			AdminPermissionWildcard, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			f.registerMCP(t, "list_things", false)
			if !tt.conns {
				if err := f.store.SetTokenConnectionScope(f.tokenID, nil); err != nil {
					t.Fatalf("SetTokenConnectionScope failed: %v", err)
				}
			}
			if tt.mcpScope != nil {
				f.setMCPScope(t, tt.mcpScope...)
			}
			if tt.adminScope != nil {
				f.setAdminScope(t, tt.adminScope...)
			}
			if got := f.checker.CanGrantAdminPermission(f.tokenCtx(),
				tt.perm); got != tt.want {
				t.Errorf("CanGrantAdminPermission(%s) = %v, want %v", tt.perm,
					got, tt.want)
			}
		})
	}
}

// TestCanGrantAdminPermissionNonSuperuserOwner checks that a token
// grants only the admin permissions its owner holds.
func TestCanGrantAdminPermissionNonSuperuserOwner(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	o := f.newOwner(t, []ScopedConnection{{ConnectionID: ConnectionIDAll,
		AccessLevel: AccessLevelReadWrite}}, nil)
	if err := f.store.GrantAdminPermission(o.groupID,
		PermManageBlackouts); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}
	ctx := o.ctx()
	if !f.checker.CanGrantAdminPermission(ctx, PermManageBlackouts) {
		t.Error("The owner's own permission should be grantable")
	}
	if f.checker.CanGrantAdminPermission(ctx, PermManageUsers) {
		t.Error("A permission the owner lacks must be refused")
	}
	if f.checker.CanGrantAdminPermission(ctx, AdminPermissionWildcard) {
		t.Error("The wildcard needs an owner who holds it")
	}
}

// TestCanGrantMCPItemNonSuperuserOwner checks that a token grants only
// the MCP items its owner may call.
func TestCanGrantMCPItemNonSuperuserOwner(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	f.registerMCP(t, "held", false)
	f.registerMCP(t, "other", false)
	o := f.newOwner(t, nil, nil)
	if err := f.store.GrantMCPPrivilegeByName(o.groupID, "held"); err != nil {
		t.Fatalf("GrantMCPPrivilegeByName failed: %v", err)
	}
	ctx := o.ctx()
	if !f.checker.CanGrantMCPItem(ctx, "held") {
		t.Error("The owner's own item should be grantable")
	}
	if f.checker.CanGrantMCPItem(ctx, "other") {
		t.Error("An item the owner lacks must be refused")
	}
	if f.checker.CanGrantMCPItem(ctx, mcpScopeWildcard) {
		t.Error("The wildcard needs an owner who holds it")
	}
}

// targetToken issues the fixture's target user a token, so that the
// scope-edit tests have one to change.
func (f *grantScopeFixture) targetToken(t *testing.T) int64 {
	t.Helper()
	_, token, err := f.store.CreateToken("target", "target token", nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	return token.ID
}

// TestTokenScopeChangeConnections covers connection scope edits. The
// acting token holds 5 at read_write and 7 read-only.
func TestTokenScopeChangeConnections(t *testing.T) {
	rw := AccessLevelReadWrite
	r := AccessLevelRead
	sc := func(id int, level string) ScopedConnection {
		return ScopedConnection{ConnectionID: id, AccessLevel: level}
	}
	tests := []struct {
		name   string
		stored []ScopedConnection
		change []ScopedConnection
		want   bool
	}{
		{"narrow an unscoped token to a held connection", nil,
			[]ScopedConnection{sc(5, rw)}, true},
		{"narrow an unscoped token to a read-only connection at read_write",
			nil, []ScopedConnection{sc(7, rw)}, true},
		{"narrow an unscoped token to an unseen connection", nil,
			[]ScopedConnection{sc(9, r)}, false},
		{"keep read_write on a read-only connection",
			[]ScopedConnection{sc(7, rw)}, []ScopedConnection{sc(7, rw)}, true},
		{"widen read to read_write on a read-only connection",
			[]ScopedConnection{sc(7, r)}, []ScopedConnection{sc(7, rw)}, false},
		{"widen read to read_write on a held connection",
			[]ScopedConnection{sc(5, r)}, []ScopedConnection{sc(5, rw)}, true},
		{"add an unseen connection", []ScopedConnection{sc(5, r)},
			[]ScopedConnection{sc(5, r), sc(9, r)}, false},
		{"drop an unseen connection", []ScopedConnection{sc(9, r)},
			[]ScopedConnection{sc(5, rw)}, false},
		{"drop a readable connection", []ScopedConnection{sc(7, r), sc(5, r)},
			[]ScopedConnection{sc(5, r)}, true},
		{"add all connections", []ScopedConnection{sc(5, r)},
			[]ScopedConnection{sc(ConnectionIDAll, r)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			target := f.targetToken(t)
			if tt.stored != nil {
				if err := f.store.SetTokenConnectionScope(target, tt.stored); err != nil {
					t.Fatalf("SetTokenConnectionScope failed: %v", err)
				}
			}
			got, err := f.checker.TokenScopeChangeWithinCeiling(f.tokenCtx(),
				target, TokenScopeChange{Connections: tt.change})
			if err != nil {
				t.Fatalf("TokenScopeChangeWithinCeiling failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTokenScopeChangeNamedKinds covers MCP and admin scope edits. The
// acting token's MCP scope is list_things and its admin scope
// manage_blackouts.
func TestTokenScopeChangeNamedKinds(t *testing.T) {
	tests := []struct {
		name        string
		storedMCP   []string
		storedAdmin []string
		change      TokenScopeChange
		want        bool
	}{
		{"narrow an unscoped MCP kind to anything", nil, nil,
			TokenScopeChange{MCPPrivileges: []string{"query_things"}}, true},
		{"add a held MCP item", []string{"query_things"}, nil,
			TokenScopeChange{MCPPrivileges: []string{"list_things"}}, true},
		{"keep an MCP item already in scope", []string{"query_things"}, nil,
			TokenScopeChange{MCPPrivileges: []string{"query_things"}}, true},
		{"add an MCP item not held", []string{"list_things"}, nil,
			TokenScopeChange{MCPPrivileges: []string{"query_things"}}, false},
		{"widen MCP to the wildcard", []string{"list_things"}, nil,
			TokenScopeChange{MCPPrivileges: []string{mcpScopeWildcard}}, false},
		{"add a held admin permission", nil, []string{PermManageUsers},
			TokenScopeChange{AdminPermissions: []string{PermManageBlackouts}},
			true},
		{"add an admin permission not held", nil, []string{PermManageBlackouts},
			TokenScopeChange{AdminPermissions: []string{PermManageUsers}}, false},
		{"widen admin to the wildcard", nil, []string{PermManageBlackouts},
			TokenScopeChange{AdminPermissions: []string{AdminPermissionWildcard}},
			false},
		{"narrow an unscoped admin kind", nil, nil,
			TokenScopeChange{AdminPermissions: []string{PermManageUsers}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			f.registerMCP(t, "list_things", false)
			f.registerMCP(t, "query_things", false)
			f.setMCPScope(t, "list_things")
			f.setAdminScope(t, PermManageBlackouts)
			target := f.targetToken(t)
			if tt.storedMCP != nil {
				if err := f.store.SetTokenMCPScopeByNames(target,
					tt.storedMCP); err != nil {
					t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
				}
			}
			if tt.storedAdmin != nil {
				if err := f.store.SetTokenAdminScope(target,
					tt.storedAdmin); err != nil {
					t.Fatalf("SetTokenAdminScope failed: %v", err)
				}
			}
			got, err := f.checker.TokenScopeChangeWithinCeiling(f.tokenCtx(),
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

// TestTokenScopeChangeSelf checks that a token may narrow its own scope
// but not widen it.
func TestTokenScopeChangeSelf(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	ctx := f.tokenCtx()
	narrow, err := f.checker.TokenScopeChangeWithinCeiling(ctx, f.tokenID,
		TokenScopeChange{Connections: []ScopedConnection{{ConnectionID: 7,
			AccessLevel: AccessLevelRead}}})
	if err != nil || !narrow {
		t.Errorf("Narrowing its own scope should be allowed, got %v (%v)",
			narrow, err)
	}
	widen, err := f.checker.TokenScopeChangeWithinCeiling(ctx, f.tokenID,
		TokenScopeChange{Connections: []ScopedConnection{{ConnectionID: 7,
			AccessLevel: AccessLevelReadWrite}}})
	if err != nil || widen {
		t.Errorf("Widening its own scope should be refused, got %v (%v)",
			widen, err)
	}
	// Clearing hands the token its owner's whole access, which for a
	// superuser is everything; make the stored owner match the context.
	if err := f.store.SetUserSuperuser("svc-actor", true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	clear, err := f.checker.TokenScopeClearWithinCeiling(ctx, f.tokenID,
		f.lister)
	if err != nil || clear {
		t.Errorf("Clearing its own scope should be refused, got %v (%v)",
			clear, err)
	}
}

// TestTokenScopeChangeOrphanedMCPRows checks that MCP scope rows that
// name only deleted items still restrict the target, to nothing, so
// adding an item needs the ceiling to cover it.
func TestTokenScopeChangeOrphanedMCPRows(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	f.registerMCP(t, "list_things", false)
	f.registerMCP(t, "gone", false)
	f.setMCPScope(t, "list_things")
	target := f.targetToken(t)
	if err := f.store.SetTokenMCPScopeByNames(target, []string{"gone"}); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
	}
	mustExec(t, f.store,
		"DELETE FROM mcp_privilege_identifiers WHERE identifier = 'gone'")

	got, err := f.checker.TokenScopeChangeWithinCeiling(f.tokenCtx(), target,
		TokenScopeChange{MCPPrivileges: []string{"other"}})
	if err != nil || got {
		t.Errorf("An uncovered item should be refused, got %v (%v)", got, err)
	}
	// The target's owner holds no MCP item, so clearing hands it
	// nothing and is allowed.
	got, err = f.checker.TokenScopeClearWithinCeiling(f.tokenCtx(), target,
		f.lister)
	if err != nil || !got {
		t.Errorf("Clearing should be allowed, got %v (%v)", got, err)
	}
}

// TestTokenScopeChangeMissingAndUnreadable checks a token that does not
// exist, a session, and every table whose loss makes the stored scope
// unreadable.
func TestTokenScopeChangeMissingAndUnreadable(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	got, err := f.checker.TokenScopeChangeWithinCeiling(f.tokenCtx(), 9999,
		TokenScopeChange{})
	if err != nil || got {
		t.Errorf("A missing token should be refused, got %v (%v)", got, err)
	}
	got, err = f.checker.TokenScopeClearWithinCeiling(f.tokenCtx(), 9999,
		f.lister)
	if err != nil || got {
		t.Errorf("A missing token should be refused, got %v (%v)", got, err)
	}
	for _, check := range []func() (bool, error){
		func() (bool, error) {
			return f.checker.TokenScopeChangeWithinCeiling(sessionCtx(), 9999,
				TokenScopeChange{})
		},
		func() (bool, error) {
			return f.checker.TokenScopeClearWithinCeiling(sessionCtx(), 9999,
				nil)
		},
	} {
		if got, err := check(); err != nil || !got {
			t.Errorf("A session should pass, got %v (%v)", got, err)
		}
	}

	for _, table := range []string{"token_connection_scope", "token_mcp_scope",
		"token_admin_scope"} {
		t.Run(table, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			target := f.targetToken(t)
			dropAuthTable(t, f.store.db, table)
			ok, err := f.checker.TokenScopeChangeWithinCeiling(f.tokenCtx(),
				target, TokenScopeChange{})
			if ok || !errors.Is(err, ErrTokenScopeUnreadable) {
				t.Errorf("got %v (%v), want ErrTokenScopeUnreadable", ok, err)
			}
			ok, err = f.checker.TokenScopeClearWithinCeiling(f.tokenCtx(),
				target, f.lister)
			if ok || !errors.Is(err, ErrTokenScopeUnreadable) {
				t.Errorf("clear: got %v (%v), want ErrTokenScopeUnreadable",
					ok, err)
			}
		})
	}
}

// TestTokenScopeClearWithinCeiling covers clearing a scope, which hands
// the token its owner's whole access in each kind it was restricted in.
func TestTokenScopeClearWithinCeiling(t *testing.T) {
	tests := []struct {
		name        string
		ownerGrants []ScopedConnection
		ownerMCP    []string
		scopeConns  []ScopedConnection
		scopeMCP    []string
		scopeAdmin  []string
		want        bool
	}{
		{"unscoped target", []ScopedConnection{{ConnectionID: 9,
			AccessLevel: AccessLevelReadWrite}}, nil, nil, nil, nil, true},
		{"owner within the ceiling", []ScopedConnection{{ConnectionID: 5,
			AccessLevel: AccessLevelReadWrite}}, nil, []ScopedConnection{{
			ConnectionID: 5, AccessLevel: AccessLevelRead}}, nil, nil, true},
		{"owner beyond the ceiling", []ScopedConnection{{ConnectionID: 7,
			AccessLevel: AccessLevelReadWrite}}, nil, []ScopedConnection{{
			ConnectionID: 7, AccessLevel: AccessLevelRead}}, nil, nil, false},
		{"dropped entry unseen", nil, nil, []ScopedConnection{{
			ConnectionID: 9, AccessLevel: AccessLevelRead}}, nil, nil, false},
		{"owner MCP within the ceiling", nil, []string{"list_things"}, nil,
			[]string{"list_things"}, nil, true},
		{"owner MCP beyond the ceiling", nil, []string{"query_things"}, nil,
			[]string{"list_things"}, nil, false},
		{"owner admin within the ceiling", nil, nil, nil, nil,
			[]string{PermManageBlackouts}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			f.registerMCP(t, "list_things", false)
			f.registerMCP(t, "query_things", false)
			f.setMCPScope(t, "list_things")
			for _, g := range tt.ownerGrants {
				f.grant(t, g.ConnectionID, g.AccessLevel)
			}
			for _, name := range tt.ownerMCP {
				if err := f.store.GrantMCPPrivilegeByName(f.groupID, name); err != nil {
					t.Fatalf("GrantMCPPrivilegeByName failed: %v", err)
				}
			}
			target := f.targetToken(t)
			if tt.scopeConns != nil {
				if err := f.store.SetTokenConnectionScope(target,
					tt.scopeConns); err != nil {
					t.Fatalf("SetTokenConnectionScope failed: %v", err)
				}
			}
			if tt.scopeMCP != nil {
				if err := f.store.SetTokenMCPScopeByNames(target,
					tt.scopeMCP); err != nil {
					t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
				}
			}
			if tt.scopeAdmin != nil {
				if err := f.store.SetTokenAdminScope(target,
					tt.scopeAdmin); err != nil {
					t.Fatalf("SetTokenAdminScope failed: %v", err)
				}
			}
			got, err := f.checker.TokenScopeClearWithinCeiling(f.tokenCtx(),
				target, f.lister)
			if err != nil {
				t.Fatalf("TokenScopeClearWithinCeiling failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTokenScopeClearOwnerReachUnreadable checks that an owner whose
// reach cannot be read is reported as an unreadable scope.
func TestTokenScopeClearOwnerReachUnreadable(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	target := f.targetToken(t)
	dropAuthTable(t, f.store.db, "group_mcp_privileges")
	got, err := f.checker.TokenScopeClearWithinCeiling(f.tokenCtx(), target,
		f.lister)
	if got || !errors.Is(err, ErrTokenScopeUnreadable) {
		t.Errorf("got %v (%v), want ErrTokenScopeUnreadable", got, err)
	}
}
