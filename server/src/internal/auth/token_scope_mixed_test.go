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

// insertLegacyConnectionScope replaces a token's connection scope with
// the given rows written directly, bypassing ValidateScopedConnections,
// so that tests can hold a scope stored before that validator refused
// mixed scopes.
func insertLegacyConnectionScope(t *testing.T, s *AuthStore, tokenID int64,
	rows []ScopedConnection) {

	t.Helper()
	if _, err := s.db.Exec(
		`DELETE FROM token_connection_scope WHERE token_id = ?`,
		tokenID); err != nil {
		t.Fatalf("Failed to clear the connection scope: %v", err)
	}
	for _, r := range rows {
		if _, err := s.db.Exec(`INSERT INTO token_connection_scope
			(token_id, connection_id, access_level) VALUES (?, ?, ?)`,
			tokenID, r.ConnectionID, r.AccessLevel); err != nil {
			t.Fatalf("Failed to insert a connection scope row: %v", err)
		}
	}
}

// TestValidateScopedConnections checks that a connection scope must be
// a plain list of distinct connections, or the "all connections" entry
// alone.
func TestValidateScopedConnections(t *testing.T) {
	rw := AccessLevelReadWrite
	r := AccessLevelRead
	sc := func(id int, level string) ScopedConnection {
		return ScopedConnection{ConnectionID: id, AccessLevel: level}
	}
	tests := []struct {
		name    string
		scope   []ScopedConnection
		wantErr error
	}{
		{"empty", nil, nil},
		{"particular connections", []ScopedConnection{sc(5, r), sc(6, rw)}, nil},
		{"all connections alone", []ScopedConnection{sc(ConnectionIDAll, rw)}, nil},
		{"bad level", []ScopedConnection{sc(5, "write")}, ErrInvalidAccessLevel},
		{"negative ID", []ScopedConnection{sc(-1, r)}, ErrInvalidConnectionScope},
		{"duplicate", []ScopedConnection{sc(5, r), sc(5, rw)},
			ErrInvalidConnectionScope},
		{"duplicate all connections", []ScopedConnection{
			sc(ConnectionIDAll, r), sc(ConnectionIDAll, r)},
			ErrInvalidConnectionScope},
		{"all connections first, then one", []ScopedConnection{
			sc(ConnectionIDAll, rw), sc(5, r)}, ErrInvalidConnectionScope},
		{"one, then all connections", []ScopedConnection{
			sc(5, r), sc(ConnectionIDAll, rw)}, ErrInvalidConnectionScope},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateScopedConnections(tt.scope)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Expected no error, got %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

// TestSetTokenConnectionScopeRefusesMixedScope checks that the store,
// which the CLI writes through, refuses a mixed scope and leaves the
// stored scope as it was.
func TestSetTokenConnectionScopeRefusesMixedScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	_, token, err := store.AsActor(systemActor).CreateToken("testuser", "Test token", nil, true)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	if err := store.AsActor(systemActor).SetTokenConnectionScope(token.ID, []ScopedConnection{
		{ConnectionID: 5, AccessLevel: AccessLevelRead},
	}, true); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}

	err = store.AsActor(systemActor).SetTokenConnectionScope(token.ID, []ScopedConnection{
		{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelReadWrite},
		{ConnectionID: 5, AccessLevel: AccessLevelRead},
	}, true)

	if !errors.Is(err, ErrInvalidConnectionScope) {
		t.Fatalf("Expected ErrInvalidConnectionScope, got %v", err)
	}

	scope, err := store.GetTokenScope(token.ID)
	if err != nil {
		t.Fatalf("GetTokenScope failed: %v", err)
	}
	if scope == nil || len(scope.Connections) != 1 ||
		scope.Connections[0].ConnectionID != 5 {
		t.Errorf("Expected the stored scope to be unchanged, got %+v", scope)
	}
}

// TestCeilingLegacyMixedScope checks that a mixed scope stored before
// the validator refused one is read at its lowest level: {all:
// read_write, 5: read} holds connection 5 at read only, so the token
// holds every connection at no more than read.
func TestCeilingLegacyMixedScope(t *testing.T) {
	mixed := []ScopedConnection{
		{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelReadWrite},
		{ConnectionID: 5, AccessLevel: AccessLevelRead},
	}

	t.Run("superuser token", func(t *testing.T) {
		f, cleanup := newGrantScopeFixture(t)
		defer cleanup()
		insertLegacyConnectionScope(t, f.store, f.tokenID, mixed)
		ctx := f.tokenCtx()

		if f.checker.CanGrantConnection(ctx, ConnectionIDAll,
			AccessLevelReadWrite) {
			t.Error("A token holding connection 5 at read must not grant " +
				"every connection at read_write")
		}
		if !f.checker.CanGrantConnection(ctx, ConnectionIDAll, AccessLevelRead) {
			t.Error("The token should still grant every connection at read")
		}
		if f.checker.CanGrantConnection(ctx, 5, AccessLevelReadWrite) {
			t.Error("Connection 5 must not be grantable above read")
		}
		if f.checker.TokenHoldsEverything(ctx) {
			t.Error("A token holding connection 5 at read must not hold " +
				"everything")
		}
	})

	t.Run("non-superuser owner", func(t *testing.T) {
		f, cleanup := newGrantScopeFixture(t)
		defer cleanup()
		o := f.newOwner(t, []ScopedConnection{{ConnectionID: ConnectionIDAll,
			AccessLevel: AccessLevelReadWrite}}, nil)
		insertLegacyConnectionScope(t, f.store, o.tokenID, mixed)

		if f.checker.CanGrantConnection(o.ctx(), ConnectionIDAll,
			AccessLevelReadWrite) {
			t.Error("A token holding connection 5 at read must not grant " +
				"every connection at read_write")
		}
		if !f.checker.CanGrantConnection(o.ctx(), ConnectionIDAll,
			AccessLevelRead) {
			t.Error("The token should still grant every connection at read")
		}
	})

	t.Run("unreadable scope", func(t *testing.T) {
		f, cleanup := newGrantScopeFixture(t)
		defer cleanup()
		insertLegacyConnectionScope(t, f.store, f.tokenID, mixed)
		c := &tokenCeiling{ctx: f.tokenCtx(), rc: f.checker}
		dropAuthTable(t, f.store.db, "token_connection_scope")
		if got := c.lowestScopedLevel(AccessLevelReadWrite); got != AccessLevelNone {
			t.Errorf("An unreadable scope should give none, got %q", got)
		}
	})

	t.Run("session and unscoped token", func(t *testing.T) {
		f, cleanup := newGrantScopeFixture(t)
		defer cleanup()
		session := &tokenCeiling{ctx: context.Background(), rc: f.checker}
		if got := session.lowestScopedLevel(AccessLevelReadWrite); got != AccessLevelReadWrite {
			t.Errorf("A session should keep its level, got %q", got)
		}
		_, token, err := f.store.AsActor(systemActor).CreateToken("target", "unscoped", nil, true)
		if err != nil {
			t.Fatalf("CreateToken failed: %v", err)
		}
		ctx := context.WithValue(context.Background(), TokenIDContextKey,
			token.ID)
		unscoped := &tokenCeiling{ctx: ctx, rc: f.checker}
		if got := unscoped.lowestScopedLevel(AccessLevelRead); got != AccessLevelRead {
			t.Errorf("An unscoped token should keep its level, got %q", got)
		}
		if got := unscoped.lowestScopedLevel(AccessLevelNone); got != AccessLevelNone {
			t.Errorf("A level of none should stay none, got %q", got)
		}
	})
}

// TestEffectivePrivilegesLegacyMixedScope checks that the privilege
// report reads a legacy mixed scope as IsConnectionInTokenScope does,
// with the entry for a particular connection overriding the "all
// connections" entry.
func TestEffectivePrivilegesLegacyMixedScope(t *testing.T) {
	mixed := []ScopedConnection{
		{ConnectionID: ConnectionIDAll, AccessLevel: AccessLevelReadWrite},
		{ConnectionID: 5, AccessLevel: AccessLevelRead},
	}

	t.Run("superuser token", func(t *testing.T) {
		f, cleanup := newGrantScopeFixture(t)
		defer cleanup()
		insertLegacyConnectionScope(t, f.store, f.tokenID, mixed)

		got := f.checker.GetEffectivePrivileges(f.tokenCtx())
		if got.ConnectionPrivileges[5] != AccessLevelRead {
			t.Errorf("Expected connection 5 at read, got %q",
				got.ConnectionPrivileges[5])
		}
		if got.ConnectionPrivileges[ConnectionIDAll] != AccessLevelReadWrite {
			t.Errorf("Expected all connections at read_write, got %q",
				got.ConnectionPrivileges[ConnectionIDAll])
		}
	})

	t.Run("superuser token, all connections alone", func(t *testing.T) {
		f, cleanup := newGrantScopeFixture(t)
		defer cleanup()
		insertLegacyConnectionScope(t, f.store, f.tokenID, mixed[:1])

		got := f.checker.GetEffectivePrivileges(f.tokenCtx())
		if len(got.ConnectionPrivileges) != 0 {
			t.Errorf("Expected no connection limits, got %v",
				got.ConnectionPrivileges)
		}
	})

	t.Run("non-superuser owner", func(t *testing.T) {
		f, cleanup := newGrantScopeFixture(t)
		defer cleanup()
		o := f.newOwner(t, []ScopedConnection{
			{ConnectionID: 5, AccessLevel: AccessLevelReadWrite},
			{ConnectionID: 6, AccessLevel: AccessLevelReadWrite},
		}, nil)
		insertLegacyConnectionScope(t, f.store, o.tokenID,
			append(mixed, ScopedConnection{ConnectionID: 9,
				AccessLevel: AccessLevelRead}))

		got := f.checker.GetEffectivePrivileges(o.ctx())
		if got.ConnectionPrivileges[5] != AccessLevelRead {
			t.Errorf("Expected connection 5 at read, got %q",
				got.ConnectionPrivileges[5])
		}
		if got.ConnectionPrivileges[6] != AccessLevelReadWrite {
			t.Errorf("Expected connection 6 at read_write, got %q",
				got.ConnectionPrivileges[6])
		}
		if level, ok := got.ConnectionPrivileges[9]; ok {
			t.Errorf("The owner holds no connection 9, got %q", level)
		}
	})
}

// TestValidateAdminPermissions checks that an admin scope may name only
// known admin permissions and the wildcard, and that the store, which
// the CLI writes through, refuses an unknown one.
func TestValidateAdminPermissions(t *testing.T) {
	for _, ok := range [][]string{nil, {AdminPermissionWildcard},
		{PermManageUsers, PermStoreSystemMemory, PermManageTokenScopes}} {
		if err := ValidateAdminPermissions(ok); err != nil {
			t.Errorf("Expected %v to be valid, got %v", ok, err)
		}
	}
	for _, bad := range [][]string{{"users.read"}, {PermManageUsers, ""},
		{"MANAGE_USERS"}} {
		if err := ValidateAdminPermissions(bad); !errors.Is(err,
			ErrUnknownAdminPermission) {
			t.Errorf("Expected %v to be refused, got %v", bad, err)
		}
	}

	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()
	_, token, err := store.AsActor(systemActor).CreateToken("testuser", "Test token", nil, true)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	if err := store.AsActor(systemActor).SetTokenAdminScope(token.ID, []string{PermManageUsers, "manage_everything"}, true); !errors.Is(err,
		ErrUnknownAdminPermission) {
		t.Fatalf("Expected ErrUnknownAdminPermission, got %v", err)
	}
	if perms, err := store.GetTokenAdminScope(token.ID); err != nil ||
		len(perms) != 0 {
		t.Errorf("Expected no admin scope to be stored, got %v, %v", perms, err)
	}
}

// TestScopeChangeOverLegacyMixedScope checks a connection scope edit,
// by a token holding every connection at read, of a token whose legacy
// scope is {all: read_write, 5: read}. That scope holds connection 5 at
// read, so writing {all: read_write} over it raises connection 5 and
// needs every connection at read_write, although the "all connections"
// entry itself looks kept (6 October review of PR #528).
func TestScopeChangeOverLegacyMixedScope(t *testing.T) {
	rw := AccessLevelReadWrite
	r := AccessLevelRead
	sc := func(id int, level string) ScopedConnection {
		return ScopedConnection{ConnectionID: id, AccessLevel: level}
	}
	mixed := []ScopedConnection{sc(ConnectionIDAll, rw), sc(5, r)}
	tests := []struct {
		name   string
		stored []ScopedConnection
		change []ScopedConnection
		want   bool
	}{
		{"raise connection 5 through the all entry", mixed,
			[]ScopedConnection{sc(ConnectionIDAll, rw)}, false},
		{"narrow the all entry to read", mixed,
			[]ScopedConnection{sc(ConnectionIDAll, r)}, true},
		{"keep a plain all entry at read_write",
			[]ScopedConnection{sc(ConnectionIDAll, rw)},
			[]ScopedConnection{sc(ConnectionIDAll, rw)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, cleanup := newGrantScopeFixture(t)
			defer cleanup()
			actor := f.newOwner(t, []ScopedConnection{sc(ConnectionIDAll, r)},
				nil)
			target := f.targetToken(t)
			insertLegacyConnectionScope(t, f.store, target, tt.stored)

			got, err := f.checker.TokenScopeChangeWithinCeiling(actor.ctx(),
				target, TokenScopeChange{Connections: tt.change})
			if err != nil {
				t.Fatalf("TokenScopeChangeWithinCeiling failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("Expected %v, got %v", tt.want, got)
			}
		})
	}
}

// TestStoredScopeAllConnectionsLevel checks the level a stored scope
// holds every connection at: the lowest entry beside the "all
// connections" entry, none without that entry, and read_write when the
// kind is unrestricted.
func TestStoredScopeAllConnectionsLevel(t *testing.T) {
	tests := []struct {
		name  string
		scope storedTokenScope
		want  string
	}{
		{"unrestricted", storedTokenScope{}, AccessLevelReadWrite},
		{"plain all entry", storedTokenScope{connsRestricted: true,
			conns: map[int]string{ConnectionIDAll: AccessLevelReadWrite}},
			AccessLevelReadWrite},
		{"legacy mixed scope", storedTokenScope{connsRestricted: true,
			conns: map[int]string{ConnectionIDAll: AccessLevelReadWrite,
				5: AccessLevelRead}}, AccessLevelRead},
		{"no all entry", storedTokenScope{connsRestricted: true,
			conns: map[int]string{5: AccessLevelReadWrite}}, AccessLevelNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.connectionLevel(ConnectionIDAll); got != tt.want {
				t.Errorf("Expected %q, got %q", tt.want, got)
			}
		})
	}
}
