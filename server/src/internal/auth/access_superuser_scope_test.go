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
// Superuser token scope tests (issue #471)
//
// A superuser's API token used to bypass every scope check, so a token
// minted for one narrow job could reach the whole installation. These
// tests pin the corrected rule at each of the six access-control entry
// points: a narrowed scope binds the token, an absent or wildcard scope
// leaves it unrestricted, a superuser session is untouched, and a scope
// that cannot be read denies rather than widens.
// =============================================================================

// superuserScopeFixture holds a checker, a superuser user and a token
// of that user whose scope each test narrows as it needs.
type superuserScopeFixture struct {
	store   *AuthStore
	checker *RBACChecker
	userID  int64
	tokenID int64
}

// newSuperuserScopeFixture builds a store holding one user with a token
// and two registered tools, and returns it with a cleanup function.
func newSuperuserScopeFixture(t *testing.T) (*superuserScopeFixture, func()) {
	t.Helper()

	store, cleanup := createTestAuthStoreForAccess(t)

	if err := store.CreateUser("root", "Password1234", "Root", "", ""); err != nil {
		cleanup()
		t.Fatalf("Failed to create user: %v", err)
	}
	userID, err := store.GetUserID("root")
	if err != nil {
		cleanup()
		t.Fatalf("Failed to read user id: %v", err)
	}
	if _, err := store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool,
		"Tool A", false); err != nil {
		cleanup()
		t.Fatalf("Failed to register tool_a: %v", err)
	}
	if _, err := store.RegisterMCPPrivilege("tool_b", MCPPrivilegeTypeTool,
		"Tool B", false); err != nil {
		cleanup()
		t.Fatalf("Failed to register tool_b: %v", err)
	}
	_, token, err := store.CreateToken("root", "superuser token", nil)
	if err != nil {
		cleanup()
		t.Fatalf("Failed to create token: %v", err)
	}

	return &superuserScopeFixture{
		store:   store,
		checker: NewRBACChecker(store),
		userID:  userID,
		tokenID: token.ID,
	}, cleanup
}

// tokenCtx presents the fixture's token as a superuser API token.
func (f *superuserScopeFixture) tokenCtx() context.Context {
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, true)
	ctx = context.WithValue(ctx, UserIDContextKey, f.userID)
	ctx = context.WithValue(ctx, IsAPITokenContextKey, true)
	return context.WithValue(ctx, TokenIDContextKey, f.tokenID)
}

// sessionCtx presents the same user as a superuser session, which
// carries no token at all.
func (f *superuserScopeFixture) sessionCtx() context.Context {
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, true)
	return context.WithValue(ctx, UserIDContextKey, f.userID)
}

// unidentifiedTokenCtx claims API-token authentication without carrying
// the token id, which is the context a route mounted outside the auth
// wrapper would produce.
func unidentifiedTokenCtx() context.Context {
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, true)
	return context.WithValue(ctx, IsAPITokenContextKey, true)
}

// dropScopeTable removes one of the scope tables so that the matching
// lookup fails, which is how these tests reach the fail-closed
// branches without closing the whole store.
func (f *superuserScopeFixture) dropScopeTable(t *testing.T, table string) {
	t.Helper()

	dropAuthTable(t, f.store.db, table)
}

// setAdminScope narrows the fixture token's admin scope.
func (f *superuserScopeFixture) setAdminScope(t *testing.T, perms []string) {
	t.Helper()

	if err := f.store.SetTokenAdminScope(f.tokenID, perms); err != nil {
		t.Fatalf("Failed to set admin scope: %v", err)
	}
}

// setConnectionScope narrows the fixture token's connection scope.
func (f *superuserScopeFixture) setConnectionScope(t *testing.T,
	conns []ScopedConnection) {

	t.Helper()

	if err := f.store.SetTokenConnectionScope(f.tokenID, conns); err != nil {
		t.Fatalf("Failed to set connection scope: %v", err)
	}
}

// setMCPScope narrows the fixture token's MCP scope by identifier.
func (f *superuserScopeFixture) setMCPScope(t *testing.T, names []string) {
	t.Helper()

	if err := f.store.SetTokenMCPScopeByNames(f.tokenID, names); err != nil {
		t.Fatalf("Failed to set MCP scope: %v", err)
	}
}

// -----------------------------------------------------------------------------
// IsSuperuser
// -----------------------------------------------------------------------------

func TestIsSuperuserHonoursTokenAdminScope(t *testing.T) {
	tests := []struct {
		name  string
		scope []string
		want  bool
	}{
		{"narrowed scope withdraws superuser", []string{PermManageUsers}, false},
		{"wildcard scope keeps superuser", []string{AdminPermissionWildcard}, true},
		{"no scope keeps superuser", nil, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, cleanup := newSuperuserScopeFixture(t)
			defer cleanup()

			if len(tc.scope) > 0 {
				f.setAdminScope(t, tc.scope)
			}

			if got := f.checker.IsSuperuser(f.tokenCtx()); got != tc.want {
				t.Errorf("IsSuperuser = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsSuperuserSessionUnaffectedByScopeRule(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	// The user's own token is narrowed, which must not reach a session
	// that carries no token.
	f.setAdminScope(t, []string{PermManageUsers})

	if !f.checker.IsSuperuser(f.sessionCtx()) {
		t.Error("Expected a superuser session to remain a superuser")
	}
}

func TestIsSuperuserFailsClosedOnScopeLookupError(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.dropScopeTable(t, "token_admin_scope")

	if f.checker.IsSuperuser(f.tokenCtx()) {
		t.Error("Expected an unreadable admin scope to deny superuser status")
	}
}

func TestIsSuperuserFailsClosedOnUnidentifiedToken(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	if f.checker.IsSuperuser(unidentifiedTokenCtx()) {
		t.Error("Expected a token with no id to be denied superuser status")
	}
}

func TestIsSuperuserFalseWithoutContextFlag(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	if f.checker.IsSuperuser(context.Background()) {
		t.Error("Expected a context with no superuser flag to report false")
	}
}

// -----------------------------------------------------------------------------
// CanAccessMCPItem
// -----------------------------------------------------------------------------

func TestCanAccessMCPItemSuperuserHonoursTokenScope(t *testing.T) {
	tests := []struct {
		name      string
		mcpScope  []string
		wantToolA bool
		wantToolB bool
	}{
		{"narrowed scope binds the token", []string{"tool_a"}, true, false},
		{"wildcard scope leaves it open", []string{mcpWildcardIdentifier}, true, true},
		{"no scope leaves it open", nil, true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, cleanup := newSuperuserScopeFixture(t)
			defer cleanup()

			if len(tc.mcpScope) > 0 {
				f.setMCPScope(t, tc.mcpScope)
			}

			ctx := f.tokenCtx()
			if got := f.checker.CanAccessMCPItem(ctx, "tool_a"); got != tc.wantToolA {
				t.Errorf("tool_a: got %v, want %v", got, tc.wantToolA)
			}
			if got := f.checker.CanAccessMCPItem(ctx, "tool_b"); got != tc.wantToolB {
				t.Errorf("tool_b: got %v, want %v", got, tc.wantToolB)
			}
		})
	}
}

func TestCanAccessMCPItemSuperuserSessionUnaffected(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.setMCPScope(t, []string{"tool_a"})

	if !f.checker.CanAccessMCPItem(f.sessionCtx(), "tool_b") {
		t.Error("Expected a superuser session to reach every tool")
	}
}

func TestCanAccessMCPItemSuperuserDeniesOnScopeLookupError(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	// The admin scope still reads, so the caller keeps superuser
	// status and the MCP scope lookup is what fails.
	f.dropScopeTable(t, "token_mcp_scope")

	if f.checker.CanAccessMCPItem(f.tokenCtx(), "tool_a") {
		t.Error("Expected an unreadable MCP scope to deny access")
	}
}

// -----------------------------------------------------------------------------
// CanAccessConnection
// -----------------------------------------------------------------------------

func TestCanAccessConnectionSuperuserHonoursTokenScope(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.setConnectionScope(t, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelRead},
	})

	ctx := f.tokenCtx()

	canAccess, level := f.checker.CanAccessConnection(ctx, 1)
	if !canAccess || level != AccessLevelRead {
		t.Errorf("Connection 1: got (%v, %q), want (true, %q)", canAccess, level,
			AccessLevelRead)
	}

	if canAccess, _ := f.checker.CanAccessConnection(ctx, 2); canAccess {
		t.Error("Expected connection 2, outside the token scope, to be denied")
	}
}

func TestCanAccessConnectionSuperuserUnscopedAndWildcard(t *testing.T) {
	tests := []struct {
		name  string
		scope []ScopedConnection
	}{
		{"no scope", nil},
		{"wildcard scope", []ScopedConnection{
			{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelReadWrite},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, cleanup := newSuperuserScopeFixture(t)
			defer cleanup()

			if len(tc.scope) > 0 {
				f.setConnectionScope(t, tc.scope)
			}

			canAccess, level := f.checker.CanAccessConnection(f.tokenCtx(), 7)
			if !canAccess || level != AccessLevelReadWrite {
				t.Errorf("got (%v, %q), want (true, %q)", canAccess, level,
					AccessLevelReadWrite)
			}
		})
	}
}

func TestCanAccessConnectionSuperuserSessionUnaffected(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.setConnectionScope(t, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelRead},
	})

	canAccess, level := f.checker.CanAccessConnection(f.sessionCtx(), 2)
	if !canAccess || level != AccessLevelReadWrite {
		t.Errorf("Session: got (%v, %q), want (true, %q)", canAccess, level,
			AccessLevelReadWrite)
	}
}

func TestCanAccessConnectionSuperuserDeniesOnScopeLookupError(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.dropScopeTable(t, "token_connection_scope")

	if canAccess, _ := f.checker.CanAccessConnection(f.tokenCtx(), 1); canAccess {
		t.Error("Expected an unreadable connection scope to deny access")
	}
}

// -----------------------------------------------------------------------------
// HasAdminPermission
// -----------------------------------------------------------------------------

// TestHasAdminPermissionSuperuserHonoursTokenScope pins the
// intersection rule: the fixture's owner holds no group grant at all,
// so everything allowed here comes from its superuser status, bounded
// by the token's admin scope.
func TestHasAdminPermissionSuperuserHonoursTokenScope(t *testing.T) {
	tests := []struct {
		name       string
		scope      []string
		permission string
		want       bool
	}{
		{"scoped permission", []string{PermManageUsers},
			PermManageUsers, true},
		{"permission outside the scope", []string{PermManageUsers},
			PermManageGroups, false},
		{"one of several scoped permissions",
			[]string{PermManageUsers, PermManageGroups},
			PermManageGroups, true},
		{"wildcard scope", []string{AdminPermissionWildcard},
			PermManageUsers, true},
		{"no scope", nil, PermManageUsers, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, cleanup := newSuperuserScopeFixture(t)
			defer cleanup()

			if len(tc.scope) > 0 {
				f.setAdminScope(t, tc.scope)
			}

			got := f.checker.HasAdminPermission(f.tokenCtx(), tc.permission)
			if got != tc.want {
				t.Errorf("HasAdminPermission(%s) = %v, want %v",
					tc.permission, got, tc.want)
			}
		})
	}
}

// TestHasAdminPermissionScopedTokenDeniedWithoutSuperuser confirms the
// intersection needs both halves: strip the superuser flag and the
// same scoped token falls back on group grants, of which the fixture
// user has none.
func TestHasAdminPermissionScopedTokenDeniedWithoutSuperuser(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.setAdminScope(t, []string{PermManageUsers})

	ctx := context.WithValue(context.Background(), UserIDContextKey, f.userID)
	ctx = context.WithValue(ctx, IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, TokenIDContextKey, f.tokenID)

	if f.checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("Expected a non-superuser token to need a group grant")
	}
}

// TestHasAdminPermissionUnidentifiedTokenDenied keeps the
// fail-closed rule for an API token context carrying no token id.
func TestHasAdminPermissionUnidentifiedTokenDenied(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	if f.checker.HasAdminPermission(unidentifiedTokenCtx(), PermManageUsers) {
		t.Error("Expected an unidentified token to be refused")
	}
}

func TestHasAdminPermissionSuperuserSessionUnaffected(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.setAdminScope(t, []string{PermManageUsers})

	if !f.checker.HasAdminPermission(f.sessionCtx(), PermManageGroups) {
		t.Error("Expected a superuser session to hold every admin permission")
	}
}

func TestHasAdminPermissionSuperuserDeniesOnScopeLookupError(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.dropScopeTable(t, "token_admin_scope")

	if f.checker.HasAdminPermission(f.tokenCtx(), PermManageUsers) {
		t.Error("Expected an unreadable admin scope to deny the permission")
	}
}

// -----------------------------------------------------------------------------
// One scope kind must not narrow another
// -----------------------------------------------------------------------------

// TestAdminScopeDoesNotNarrowOtherSurfaces proves that withdrawing a
// superuser's blanket status, which a narrowed admin scope does, does
// not drop the caller onto its owner's group grants on the connection
// and MCP surfaces: the fixture owner has no grants at all, and the
// token scopes neither connections nor tools, so both stay open.
func TestAdminScopeDoesNotNarrowOtherSurfaces(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.setAdminScope(t, []string{PermManageUsers})
	ctx := f.tokenCtx()

	if f.checker.IsSuperuser(ctx) {
		t.Fatal("Expected the narrowed admin scope to withdraw blanket status")
	}
	canAccess, level := f.checker.CanAccessConnection(ctx, 7)
	if !canAccess || level != AccessLevelReadWrite {
		t.Errorf("CanAccessConnection = (%v, %q), want (true, %q)",
			canAccess, level, AccessLevelReadWrite)
	}
	if !f.checker.CanAccessMCPItem(ctx, "tool_a") {
		t.Error("Expected an unscoped MCP surface to stay open")
	}

	ids, all, err := f.checker.VisibleConnectionIDs(ctx, superuserVisibilityLister())
	if err != nil {
		t.Fatalf("VisibleConnectionIDs returned %v", err)
	}
	if !all || ids != nil {
		t.Errorf("Expected every connection to stay visible, got %v (all=%v)",
			ids, all)
	}
}

// TestConnectionAndMCPScopesRemainReachable pins the motivating case
// for the other two surfaces: a superuser-owned token scoped to one
// connection, and one scoped to one tool, still reach what they name
// although the owner holds no group grant.
func TestConnectionAndMCPScopesRemainReachable(t *testing.T) {
	t.Run("connection scope", func(t *testing.T) {
		f, cleanup := newSuperuserScopeFixture(t)
		defer cleanup()

		f.setConnectionScope(t, []ScopedConnection{
			{ConnectionID: 2, AccessLevel: AccessLevelRead},
		})

		canAccess, level := f.checker.CanAccessConnection(f.tokenCtx(), 2)
		if !canAccess || level != AccessLevelRead {
			t.Errorf("CanAccessConnection(2) = (%v, %q), want (true, %q)",
				canAccess, level, AccessLevelRead)
		}
		if canAccess, _ := f.checker.CanAccessConnection(f.tokenCtx(), 3); canAccess {
			t.Error("Expected connection 3, outside the token scope, to be denied")
		}
	})

	t.Run("MCP scope", func(t *testing.T) {
		f, cleanup := newSuperuserScopeFixture(t)
		defer cleanup()

		f.setMCPScope(t, []string{"tool_a"})

		if !f.checker.CanAccessMCPItem(f.tokenCtx(), "tool_a") {
			t.Error("Expected the scoped tool to remain reachable")
		}
		if f.checker.CanAccessMCPItem(f.tokenCtx(), "tool_b") {
			t.Error("Expected a tool outside the scope to be refused")
		}
	})
}

// -----------------------------------------------------------------------------
// VisibleConnectionIDs
// -----------------------------------------------------------------------------

// superuserVisibilityLister enumerates three connections owned by
// somebody else, so that only the scope rules decide what is returned.
func superuserVisibilityLister() *stubVisibilityLister {
	return &stubVisibilityLister{connections: []ConnectionVisibilityInfo{
		{ID: 1, IsShared: false, OwnerUsername: "someone"},
		{ID: 2, IsShared: true, OwnerUsername: "someone"},
		{ID: 3, IsShared: false, OwnerUsername: "someone"},
	}}
}

func TestVisibleConnectionIDsSuperuserHonoursTokenScope(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.setConnectionScope(t, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelRead},
		{ConnectionID: 3, AccessLevel: AccessLevelReadWrite},
	})

	ids, all, err := f.checker.VisibleConnectionIDs(f.tokenCtx(),
		superuserVisibilityLister())
	if err != nil {
		t.Fatalf("VisibleConnectionIDs returned an error: %v", err)
	}
	if all {
		t.Fatal("Expected a scoped token not to see every connection")
	}
	got := idSet(ids)
	if len(got) != 2 || !got[1] || !got[3] {
		t.Errorf("Expected connections 1 and 3, got %v", ids)
	}
}

func TestVisibleConnectionIDsSuperuserUnscopedSeesAll(t *testing.T) {
	tests := []struct {
		name  string
		scope []ScopedConnection
	}{
		{"no scope", nil},
		{"wildcard scope", []ScopedConnection{
			{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelReadWrite},
		}},
		// An admin-only scope leaves the connection scope empty, so
		// connection visibility is untouched by it.
		{"other scope kinds only", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, cleanup := newSuperuserScopeFixture(t)
			defer cleanup()

			if len(tc.scope) > 0 {
				f.setConnectionScope(t, tc.scope)
			} else {
				f.setAdminScope(t, []string{AdminPermissionWildcard})
			}

			ids, all, err := f.checker.VisibleConnectionIDs(f.tokenCtx(),
				superuserVisibilityLister())
			if err != nil {
				t.Fatalf("VisibleConnectionIDs returned an error: %v", err)
			}
			if !all || ids != nil {
				t.Errorf("Expected (nil, true), got (%v, %v)", ids, all)
			}
		})
	}
}

func TestVisibleConnectionIDsSuperuserSessionUnaffected(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.setConnectionScope(t, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelRead},
	})

	ids, all, err := f.checker.VisibleConnectionIDs(f.sessionCtx(),
		superuserVisibilityLister())
	if err != nil {
		t.Fatalf("VisibleConnectionIDs returned an error: %v", err)
	}
	if !all || ids != nil {
		t.Errorf("Expected a session to see everything, got (%v, %v)", ids, all)
	}
}

func TestVisibleConnectionIDsSuperuserScopeLookupErrors(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.dropScopeTable(t, "token_connection_scope")

	ids, all, err := f.checker.VisibleConnectionIDs(f.tokenCtx(),
		superuserVisibilityLister())
	if err == nil {
		t.Fatal("Expected an unreadable connection scope to return an error")
	}
	if all || ids != nil {
		t.Errorf("Expected no visibility on error, got (%v, %v)", ids, all)
	}
}

// -----------------------------------------------------------------------------
// GetEffectivePrivileges
// -----------------------------------------------------------------------------

func TestGetEffectivePrivilegesSuperuserReportsTokenScope(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.setConnectionScope(t, []ScopedConnection{
		{ConnectionID: 4, AccessLevel: AccessLevelRead},
	})
	f.setMCPScope(t, []string{"tool_a"})

	privs := f.checker.GetEffectivePrivileges(f.tokenCtx())

	if !privs.IsSuperuser {
		t.Error("Expected the caller to remain a superuser")
	}
	if privs.TokenScopeError != nil {
		t.Fatalf("Unexpected scope error: %v", privs.TokenScopeError)
	}
	if privs.TokenScope == nil {
		t.Fatal("Expected the token scope to be reported")
	}
	if level := privs.ConnectionPrivileges[4]; level != AccessLevelRead {
		t.Errorf("Expected connection 4 at read, got %q", level)
	}
	if len(privs.ConnectionPrivileges) != 1 {
		t.Errorf("Expected only the scoped connection, got %v",
			privs.ConnectionPrivileges)
	}
	if !privs.MCPPrivileges["tool_a"] || privs.MCPPrivileges["tool_b"] {
		t.Errorf("Expected only tool_a in scope, got %v", privs.MCPPrivileges)
	}
}

func TestGetEffectivePrivilegesSuperuserWildcardScopesUnrestricted(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.setConnectionScope(t, []ScopedConnection{
		{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelReadWrite},
	})
	f.setMCPScope(t, []string{mcpWildcardIdentifier})

	privs := f.checker.GetEffectivePrivileges(f.tokenCtx())

	if !privs.IsSuperuser || privs.TokenScope == nil {
		t.Fatalf("Expected a superuser with a reported scope, got %+v", privs)
	}
	if len(privs.ConnectionPrivileges) != 0 || len(privs.MCPPrivileges) != 0 {
		t.Errorf("Expected a wildcard scope to report no restriction, got %+v",
			privs)
	}
}

func TestGetEffectivePrivilegesSuperuserUnscopedTokenAndSession(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	for name, ctx := range map[string]context.Context{
		"unscoped token": f.tokenCtx(),
		"session":        f.sessionCtx(),
	} {
		privs := f.checker.GetEffectivePrivileges(ctx)
		if !privs.IsSuperuser {
			t.Errorf("%s: expected a superuser", name)
		}
		if privs.TokenScope != nil || privs.TokenScopeError != nil {
			t.Errorf("%s: expected no scope to report, got %+v", name, privs)
		}
		if len(privs.ConnectionPrivileges) != 0 || len(privs.MCPPrivileges) != 0 {
			t.Errorf("%s: expected unrestricted privileges, got %+v", name, privs)
		}
	}
}

func TestGetEffectivePrivilegesSuperuserNarrowedAdminScope(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.setAdminScope(t, []string{PermManageUsers})

	privs := f.checker.GetEffectivePrivileges(f.tokenCtx())
	if privs.IsSuperuser {
		t.Error("Expected a narrowed admin scope to withdraw superuser status")
	}
	if !privs.AdminPermissions[PermManageUsers] {
		t.Errorf("Expected the scoped permission to be reported, got %+v",
			privs.AdminPermissions)
	}
	if privs.AdminPermissions[PermManageGroups] {
		t.Error("Expected only the scoped permission to be reported")
	}
	// The report must agree with what the check will allow.
	if !f.checker.HasAdminPermission(f.tokenCtx(), PermManageUsers) {
		t.Error("Expected the reported permission to be allowed")
	}
}

func TestGetEffectivePrivilegesSuperuserReportsScopeLookupError(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	// GetTokenScope reads the MCP scope too, so dropping that table
	// fails the whole lookup whilst leaving superuser status intact.
	f.dropScopeTable(t, "token_mcp_scope")

	privs := f.checker.GetEffectivePrivileges(f.tokenCtx())
	// Every check in access.go denies on an unreadable scope, so the
	// report must not claim unrestricted superuser rights either.
	if privs.IsSuperuser {
		t.Error("Expected an unreadable scope to withdraw superuser status")
	}
	if privs.TokenScopeError == nil {
		t.Error("Expected the scope failure to be reported")
	}
	if privs.TokenScope != nil {
		t.Errorf("Expected no scope to be claimed, got %+v", privs.TokenScope)
	}
}

// TestConnectionInTokenScope checks the check that handlers gated on
// ownership or an admin permission apply to a token's connection scope:
// a narrowed scope admits only its connections, a session and an
// unscoped token admit any, and an incomplete token context or an
// unreadable scope denies.
func TestConnectionInTokenScope(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	if !f.checker.ConnectionInTokenScope(f.tokenCtx(), 2) {
		t.Error("Expected an unscoped token to admit any connection")
	}

	f.setConnectionScope(t, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelRead},
	})
	if !f.checker.ConnectionInTokenScope(f.tokenCtx(), 1) {
		t.Error("Expected the scoped connection to be admitted")
	}
	if f.checker.ConnectionInTokenScope(f.tokenCtx(), 2) {
		t.Error("Expected a connection outside the scope to be refused")
	}
	if !f.checker.ConnectionInTokenScope(f.sessionCtx(), 2) {
		t.Error("Expected a session to be unaffected by token scope")
	}
	if f.checker.ConnectionInTokenScope(unidentifiedTokenCtx(), 1) {
		t.Error("Expected an incomplete token context to be refused")
	}
	if !NewRBACChecker(nil).ConnectionInTokenScope(f.tokenCtx(), 2) {
		t.Error("Expected a checker without a store to admit the caller")
	}

	f.dropScopeTable(t, "token_connection_scope")
	if f.checker.ConnectionInTokenScope(f.tokenCtx(), 1) {
		t.Error("Expected an unreadable scope to be refused")
	}
}

// TestSetTokenMCPScopeByNamesRejectsUnknownIdentifier checks that an
// unregistered identifier is refused with ErrUnknownMCPPrivilege and
// that the token's existing MCP scope is left as it was.
func TestSetTokenMCPScopeByNamesRejectsUnknownIdentifier(t *testing.T) {
	f, cleanup := newSuperuserScopeFixture(t)
	defer cleanup()

	f.setMCPScope(t, []string{"tool_a"})
	err := f.store.SetTokenMCPScopeByNames(f.tokenID,
		[]string{"tool_b", "no_such_tool"})
	if !errors.Is(err, ErrUnknownMCPPrivilege) {
		t.Fatalf("Expected ErrUnknownMCPPrivilege, got %v", err)
	}
	scope, err := f.store.GetTokenScope(f.tokenID)
	if err != nil {
		t.Fatalf("GetTokenScope failed: %v", err)
	}
	if scope == nil || len(scope.MCPPrivileges) != 1 {
		t.Errorf("Expected the MCP scope to be untouched, got %+v", scope)
	}
}
