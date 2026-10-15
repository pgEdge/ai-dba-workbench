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
	"reflect"
	"testing"
)

// ownerGuardFixture is a store holding a superuser "root", an ordinary
// account "owner" with one restricted token, and a second ordinary
// account "other" that does not own it.
type ownerGuardFixture struct {
	store   *AuthStore
	tokenID int64
	ownerID int64
	otherID int64
}

// restrictedChange is the scope newOwnerGuardFixture gives owner's
// token: restricted in every kind.
func restrictedChange() TokenScopeChange {
	return TokenScopeChange{
		Connections: []ScopedConnection{
			{ConnectionID: 5, AccessLevel: AccessLevelReadWrite},
			{ConnectionID: 7, AccessLevel: AccessLevelRead},
		},
		MCPPrivileges:    []string{"tool_a", "tool_b"},
		AdminPermissions: []string{PermManageUsers, PermManageGroups},
	}
}

func newOwnerGuardFixture(t *testing.T) (*ownerGuardFixture, func()) {
	t.Helper()
	store, tokenID, cleanup := newOwnedTokenStore(t)
	fail := func(format string, args ...any) {
		cleanup()
		t.Fatalf(format, args...)
	}
	if err := store.CreateUser("other", "Password1234", "", "", ""); err != nil {
		fail("CreateUser failed: %v", err)
	}
	for _, name := range []string{"tool_a", "tool_b", "tool_c"} {
		if _, err := store.RegisterMCPPrivilege(name, "tool", "", false); err != nil {
			fail("RegisterMCPPrivilege failed: %v", err)
		}
	}
	if err := store.AsActor(systemActor).SetTokenScope(tokenID,
		restrictedChange(), true, AnyTokenOwner(), nil); err != nil {
		fail("SetTokenScope failed: %v", err)
	}
	owner, err := store.GetUser("owner")
	if err != nil || owner == nil {
		fail("GetUser(owner) failed: %v", err)
	}
	other, err := store.GetUser("other")
	if err != nil || other == nil {
		fail("GetUser(other) failed: %v", err)
	}
	return &ownerGuardFixture{store: store, tokenID: tokenID,
		ownerID: owner.ID, otherID: other.ID}, cleanup
}

// scope returns the token's stored scope.
func (f *ownerGuardFixture) scope(t *testing.T) *TokenScope {
	t.Helper()
	scope, err := f.store.GetTokenScope(f.tokenID)
	if err != nil {
		t.Fatalf("GetTokenScope failed: %v", err)
	}
	return scope
}

// auditCount returns the number of audit rows recorded so far.
func (f *ownerGuardFixture) auditCount(t *testing.T) int {
	t.Helper()
	_, total, err := f.store.ListAuditEvents(AuditFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	return total
}

// TestTokenOwnerGuardNarrowOnly checks, kind by kind, that a caller
// who does not own a token may write only a scope that narrows it
// (issue #522), and that a refused write changes nothing and leaves no
// audit row, the handler's denial being the only record of it.
func TestTokenOwnerGuardNarrowOnly(t *testing.T) {
	read := AccessLevelRead
	readWrite := AccessLevelReadWrite
	cases := []struct {
		name   string
		change TokenScopeChange
		widens bool
	}{
		{"same scope", restrictedChange(), false},
		{"lower a connection", TokenScopeChange{Connections: []ScopedConnection{
			{ConnectionID: 5, AccessLevel: read}}}, false},
		{"drop a connection", TokenScopeChange{Connections: []ScopedConnection{
			{ConnectionID: 7, AccessLevel: read}}}, false},
		{"raise a connection", TokenScopeChange{Connections: []ScopedConnection{
			{ConnectionID: 7, AccessLevel: readWrite}}}, true},
		{"add a connection", TokenScopeChange{Connections: []ScopedConnection{
			{ConnectionID: 9, AccessLevel: read}}}, true},
		{"all connections", TokenScopeChange{Connections: []ScopedConnection{
			{ConnectionID: ConnectionIDAll, AccessLevel: read}}}, true},
		{"empty connections", TokenScopeChange{
			Connections: []ScopedConnection{}}, true},
		{"subset of MCP", TokenScopeChange{
			MCPPrivileges: []string{"tool_a"}}, false},
		{"add an MCP item", TokenScopeChange{
			MCPPrivileges: []string{"tool_a", "tool_c"}}, true},
		{"MCP wildcard", TokenScopeChange{
			MCPPrivileges: []string{"*"}}, true},
		{"empty MCP", TokenScopeChange{MCPPrivileges: []string{}}, true},
		{"subset of admin", TokenScopeChange{
			AdminPermissions: []string{PermManageUsers}}, false},
		{"add an admin permission", TokenScopeChange{
			AdminPermissions: []string{PermManageUsers,
				PermManageConnections}}, true},
		{"admin wildcard", TokenScopeChange{
			AdminPermissions: []string{AdminPermissionWildcard}}, true},
		{"empty admin", TokenScopeChange{AdminPermissions: []string{}}, true},
		{"narrow one kind, widen another", TokenScopeChange{
			Connections: []ScopedConnection{
				{ConnectionID: 5, AccessLevel: read}},
			AdminPermissions: []string{PermManageConnections}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, cleanup := newOwnerGuardFixture(t)
			defer cleanup()
			before, rows := f.scope(t), f.auditCount(t)

			err := f.store.AsActor(systemActor).SetTokenScope(f.tokenID,
				tc.change, true, TokensOwnedBy(f.otherID), nil)
			if !tc.widens {
				if err != nil {
					t.Fatalf("Expected the narrowing to succeed, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrTokenNotOwned) {
				t.Fatalf("Expected ErrTokenNotOwned, got %v", err)
			}
			if after := f.scope(t); !reflect.DeepEqual(before, after) {
				t.Errorf("The refused write changed the scope: %+v to %+v",
					before, after)
			}
			if got := f.auditCount(t); got != rows {
				t.Errorf("The refused write added %d audit rows", got-rows)
			}

			// The owner, a superuser and the CLI may all widen it.
			for name, guard := range map[string]TokenOwnerGuard{
				"owner": TokensOwnedBy(f.ownerID),
				"any":   AnyTokenOwner(),
			} {
				if err := f.store.AsActor(systemActor).SetTokenScope(
					f.tokenID, tc.change, true, guard, nil); err != nil {
					t.Errorf("Expected %s to widen the token, got %v",
						name, err)
				}
			}
		})
	}
}

// TestTokenOwnerGuardUnrestrictedKinds checks that restricting a kind
// the token does not restrict narrows it, whoever owns the token.
func TestTokenOwnerGuardUnrestrictedKinds(t *testing.T) {
	f, cleanup := newOwnerGuardFixture(t)
	defer cleanup()
	if err := f.store.AsActor(systemActor).ClearTokenScope(f.tokenID, true,
		AnyTokenOwner(), nil); err != nil {
		t.Fatalf("ClearTokenScope failed: %v", err)
	}
	if err := f.store.AsActor(systemActor).SetTokenScope(f.tokenID,
		restrictedChange(), true, TokensOwnedBy(f.otherID), nil); err != nil {
		t.Fatalf("Expected restricting an unrestricted token to succeed, got %v",
			err)
	}
}

// TestTokenOwnerGuardOrphanedMCPRows checks that MCP scope rows naming
// only deleted identifiers restrict the token to nothing, so naming any
// item widens it.
func TestTokenOwnerGuardOrphanedMCPRows(t *testing.T) {
	f, cleanup := newOwnerGuardFixture(t)
	defer cleanup()
	if _, err := f.store.db.Exec(
		`DELETE FROM mcp_privilege_identifiers WHERE identifier IN ('tool_a', 'tool_b')`,
	); err != nil {
		t.Fatalf("Failed to delete the identifiers: %v", err)
	}
	err := f.store.AsActor(systemActor).SetTokenScope(f.tokenID,
		TokenScopeChange{MCPPrivileges: []string{"tool_c"}}, true,
		TokensOwnedBy(f.otherID), nil)
	if !errors.Is(err, ErrTokenNotOwned) {
		t.Fatalf("Expected ErrTokenNotOwned, got %v", err)
	}
}

// TestTokenOwnerGuardClear checks that clearing a token's scope, which
// always widens it, is refused to anyone but its owner, a superuser and
// the CLI, and that the zero guard owns no token.
func TestTokenOwnerGuardClear(t *testing.T) {
	cases := []struct {
		name    string
		guard   func(f *ownerGuardFixture) TokenOwnerGuard
		refused bool
	}{
		{"other", func(f *ownerGuardFixture) TokenOwnerGuard {
			return TokensOwnedBy(f.otherID)
		}, true},
		{"zero guard", func(*ownerGuardFixture) TokenOwnerGuard {
			return TokenOwnerGuard{}
		}, true},
		{"owner", func(f *ownerGuardFixture) TokenOwnerGuard {
			return TokensOwnedBy(f.ownerID)
		}, false},
		{"any owner", func(*ownerGuardFixture) TokenOwnerGuard {
			return AnyTokenOwner()
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, cleanup := newOwnerGuardFixture(t)
			defer cleanup()
			rows := f.auditCount(t)
			err := f.store.AsActor(systemActor).ClearTokenScope(f.tokenID,
				true, tc.guard(f), nil)
			if !tc.refused {
				if err != nil {
					t.Fatalf("Expected the clear to succeed, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrTokenNotOwned) {
				t.Fatalf("Expected ErrTokenNotOwned, got %v", err)
			}
			if got := f.auditCount(t); got != rows {
				t.Errorf("The refused clear added %d audit rows", got-rows)
			}
			if scope := f.scope(t); len(scope.AdminPermissions) != 2 {
				t.Errorf("The refused clear changed the scope: %+v", scope)
			}
		})
	}
}

// TestTokenOwnerGuardZeroGuardNarrows checks that the zero guard, which
// owns no token, may still narrow one.
func TestTokenOwnerGuardZeroGuardNarrows(t *testing.T) {
	f, cleanup := newOwnerGuardFixture(t)
	defer cleanup()
	if err := f.store.AsActor(systemActor).SetTokenScope(f.tokenID,
		TokenScopeChange{AdminPermissions: []string{PermManageUsers}}, true,
		TokenOwnerGuard{}, nil); err != nil {
		t.Fatalf("Expected the narrowing to succeed, got %v", err)
	}
}

// TestTokenOwnerGuardMissingToken checks that a write to a token that
// does not exist is left for the write to report, not taken for a
// refusal by the owner rule.
func TestTokenOwnerGuardMissingToken(t *testing.T) {
	f, cleanup := newOwnerGuardFixture(t)
	defer cleanup()
	guard := TokensOwnedBy(f.otherID)
	writes := map[string]func() error{
		"set": func() error {
			return f.store.AsActor(systemActor).SetTokenScope(99999,
				TokenScopeChange{AdminPermissions: []string{"*"}}, true, guard, nil)
		},
		"clear": func() error {
			return f.store.AsActor(systemActor).ClearTokenScope(99999, true,
				guard, nil)
		},
	}
	for name, write := range writes {
		if err := write(); errors.Is(err, ErrTokenNotOwned) {
			t.Errorf("%s: expected the missing token not to be refused as unowned",
				name)
		}
	}
}

// renameToGone holds, for each table a test breaks by renaming it, the
// literal statement that does so, so that no SQL is built from a
// variable.
var renameToGone = map[string]string{
	"tokens":                 "ALTER TABLE tokens RENAME TO gone",
	"users":                  "ALTER TABLE users RENAME TO gone",
	"token_connection_scope": "ALTER TABLE token_connection_scope RENAME TO gone",
	"token_mcp_scope":        "ALTER TABLE token_mcp_scope RENAME TO gone",
	"token_admin_scope":      "ALTER TABLE token_admin_scope RENAME TO gone",
}

// TestTokenOwnerGuardFailsClosed checks that a scope the guard cannot
// read refuses the write, rather than letting it through as a
// narrowing.
func TestTokenOwnerGuardFailsClosed(t *testing.T) {
	for _, table := range []string{"token_connection_scope",
		"token_mcp_scope", "token_admin_scope"} {
		t.Run(table, func(t *testing.T) {
			f, cleanup := newOwnerGuardFixture(t)
			defer cleanup()
			if _, err := f.store.db.Exec(renameToGone[table]); err != nil {
				t.Fatalf("Failed to rename %s: %v", table, err)
			}
			err := f.store.AsActor(systemActor).SetTokenScope(f.tokenID,
				TokenScopeChange{AdminPermissions: []string{PermManageUsers}},
				true, TokensOwnedBy(f.otherID), nil)
			if err == nil {
				t.Fatal("Expected the write to fail")
			}
		})
	}
}

// TestLoadStoredTokenScopeLookupFailures checks that the ceiling's read
// of a target token's scope reports ErrTokenScopeUnreadable, rather
// than a missing token or an empty scope, when the token, its owner or
// a scope kind cannot be looked up.
func TestLoadStoredTokenScopeLookupFailures(t *testing.T) {
	for _, table := range []string{"tokens", "users",
		"token_connection_scope", "token_mcp_scope", "token_admin_scope"} {
		t.Run(table, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			target := f.targetToken(t)
			if _, err := f.store.db.Exec(renameToGone[table]); err != nil {
				t.Fatalf("Failed to rename %s: %v", table, err)
			}
			stored, found, err := f.checker.loadStoredTokenScope(target)
			if stored != nil || found ||
				!errors.Is(err, ErrTokenScopeUnreadable) {
				t.Errorf("got %v, %v (%v), want ErrTokenScopeUnreadable",
					stored, found, err)
			}
		})
	}
}
