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

// The connections the agreement tests reason about, one of each kind of
// sharing and ownership as seen by the user "caller".
const (
	agreeSharedOther   = 1 // shared, owned by another user
	agreeUnsharedOther = 2 // unshared, owned by another user
	agreeUnsharedOwn   = 3 // unshared, owned by the caller
	agreeSharedOwn     = 4 // shared, owned by the caller
)

// agreementConnections is the connection list, with sharing metadata,
// that both CanAccessConnection (through the sharing lookup) and
// VisibleConnectionIDs (through the lister) read.
var agreementConnections = []ConnectionVisibilityInfo{
	{ID: agreeSharedOther, IsShared: true, OwnerUsername: "other"},
	{ID: agreeUnsharedOther, IsShared: false, OwnerUsername: "other"},
	{ID: agreeUnsharedOwn, IsShared: false, OwnerUsername: "caller"},
	{ID: agreeSharedOwn, IsShared: true, OwnerUsername: "caller"},
}

// agreementLookup serves agreementConnections to CanAccessConnection.
func agreementLookup(_ context.Context, connectionID int) (bool, string, error) {
	for _, c := range agreementConnections {
		if c.ID == connectionID {
			return c.IsShared, c.OwnerUsername, nil
		}
	}
	return false, "", errors.New("connection not found")
}

// agreementGrant is one group connection grant in a scenario.
type agreementGrant struct {
	callerGroup bool // granted to the caller's group, else another group
	connID      int
	level       string
}

// TestConnectionAccessAndVisibilityAgree runs every combination of
// ownership, sharing, per-connection grant and "all connections" grant
// through both CanAccessConnection and VisibleConnectionIDs, and checks
// that a connection is listed exactly when it can be opened, at the
// expected level (issue #592). The rules under test:
//
//   - only a grant naming a connection restricts it; an "all
//     connections" grant restricts none;
//   - the owner always has read_write on their own connection;
//   - a grant elsewhere does not hide a shared, unrestricted connection.
func TestConnectionAccessAndVisibilityAgree(t *testing.T) {
	rw, r := AccessLevelReadWrite, AccessLevelRead
	tests := []struct {
		name   string
		grants []agreementGrant
		// scope, when set, is the acting token's connection scope.
		scope []ScopedConnection
		// want maps each connection the caller may open to its level;
		// every other connection is denied and must not be listed.
		want map[int]string
		// wantAll is the allConnections result VisibleConnectionIDs
		// should give.
		wantAll bool
	}{
		{
			name: "no grants anywhere",
			want: map[int]string{agreeSharedOther: rw, agreeUnsharedOwn: rw,
				agreeSharedOwn: rw},
		},
		{
			name: "another group holds an all-connections grant",
			grants: []agreementGrant{
				{callerGroup: false, connID: ConnectionIDAll, level: r},
			},
			want: map[int]string{agreeSharedOther: rw, agreeUnsharedOwn: rw,
				agreeSharedOwn: rw},
		},
		{
			name: "another group holds an all-connections read_write grant",
			grants: []agreementGrant{
				{callerGroup: false, connID: ConnectionIDAll, level: rw},
			},
			want: map[int]string{agreeSharedOther: rw, agreeUnsharedOwn: rw,
				agreeSharedOwn: rw},
		},
		{
			name: "another group's grants restrict each connection",
			grants: []agreementGrant{
				{callerGroup: false, connID: agreeSharedOther, level: r},
				{callerGroup: false, connID: agreeUnsharedOther, level: r},
				{callerGroup: false, connID: agreeUnsharedOwn, level: r},
				{callerGroup: false, connID: agreeSharedOwn, level: r},
			},
			want: map[int]string{agreeUnsharedOwn: rw, agreeSharedOwn: rw},
		},
		{
			name: "a grant elsewhere keeps shared connections visible",
			grants: []agreementGrant{
				{callerGroup: true, connID: agreeUnsharedOther, level: r},
			},
			want: map[int]string{agreeSharedOther: rw, agreeUnsharedOther: r,
				agreeUnsharedOwn: rw, agreeSharedOwn: rw},
		},
		{
			name: "the caller's own grant on a shared connection restricts it",
			grants: []agreementGrant{
				{callerGroup: true, connID: agreeSharedOther, level: r},
			},
			want: map[int]string{agreeSharedOther: r, agreeUnsharedOwn: rw,
				agreeSharedOwn: rw},
		},
		{
			name: "the owner's read grant does not lower ownership",
			grants: []agreementGrant{
				{callerGroup: true, connID: agreeUnsharedOwn, level: r},
			},
			want: map[int]string{agreeSharedOther: rw, agreeUnsharedOwn: rw,
				agreeSharedOwn: rw},
		},
		{
			name: "the caller's all-connections grant",
			grants: []agreementGrant{
				{callerGroup: true, connID: ConnectionIDAll, level: r},
			},
			want: map[int]string{agreeSharedOther: rw, agreeUnsharedOther: r,
				agreeUnsharedOwn: rw, agreeSharedOwn: rw},
			wantAll: true,
		},
		{
			name: "the caller's all-connections grant on a restricted connection",
			grants: []agreementGrant{
				{callerGroup: true, connID: ConnectionIDAll, level: r},
				{callerGroup: false, connID: agreeSharedOther, level: rw},
			},
			want: map[int]string{agreeSharedOther: r, agreeUnsharedOther: r,
				agreeUnsharedOwn: rw, agreeSharedOwn: rw},
			wantAll: true,
		},
		{
			name: "a token scoped to one owned connection",
			scope: []ScopedConnection{
				{ConnectionID: agreeUnsharedOwn, AccessLevel: r},
			},
			want: map[int]string{agreeUnsharedOwn: r},
		},
		{
			name: "a token scoped to a connection another group restricts",
			grants: []agreementGrant{
				{callerGroup: false, connID: agreeSharedOther, level: rw},
			},
			scope: []ScopedConnection{
				{ConnectionID: agreeSharedOther, AccessLevel: rw},
				{ConnectionID: agreeSharedOwn, AccessLevel: rw},
			},
			want: map[int]string{agreeSharedOwn: rw},
		},
		{
			name: "a token with the wildcard scope at read",
			grants: []agreementGrant{
				{callerGroup: false, connID: ConnectionIDAll, level: rw},
			},
			scope: []ScopedConnection{
				{ConnectionID: ConnectionIDAll, AccessLevel: r},
			},
			want: map[int]string{agreeSharedOther: r, agreeUnsharedOwn: r,
				agreeSharedOwn: r},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAccess(t)
			defer cleanup()

			ctx := agreementContext(t, store, tt.grants, tt.scope)
			checker := NewRBACCheckerWithSharing(store, agreementLookup)

			ids, all, err := checker.VisibleConnectionIDs(ctx,
				&stubVisibilityLister{connections: agreementConnections})
			if err != nil {
				t.Fatalf("VisibleConnectionIDs failed: %v", err)
			}
			if all != tt.wantAll {
				t.Errorf("allConnections = %v, want %v", all, tt.wantAll)
			}
			visible := idSet(ids)

			for _, c := range agreementConnections {
				ok, level := checker.CanAccessConnection(ctx, c.ID)
				listed := all || visible[c.ID]
				if ok != listed {
					t.Errorf("connection %d: CanAccessConnection = %v but listed = %v",
						c.ID, ok, listed)
				}
				wantLevel, wantOK := tt.want[c.ID]
				if ok != wantOK || (ok && level != wantLevel) {
					t.Errorf("connection %d: got (%v, %q), want (%v, %q)",
						c.ID, ok, level, wantOK, wantLevel)
				}
			}
		})
	}
}

// agreementContext creates the user "caller", in a group of its own,
// and the user "other", in another group, applies the scenario's
// grants, and returns the caller's request context: a session, or an
// API token carrying scope when scope is set.
func agreementContext(t *testing.T, store *AuthStore, grants []agreementGrant,
	scope []ScopedConnection) context.Context {

	t.Helper()

	groups := make(map[bool]int64, 2)
	for _, u := range []struct {
		name   string
		caller bool
	}{{"caller", true}, {"other", false}} {
		if err := store.CreateUser(u.name, "Password1234", "", "", ""); err != nil {
			t.Fatalf("CreateUser %s: %v", u.name, err)
		}
		userID, err := store.GetUserID(u.name)
		if err != nil {
			t.Fatalf("GetUserID %s: %v", u.name, err)
		}
		groupID, err := store.CreateGroup(u.name+"-group", "")
		if err != nil {
			t.Fatalf("CreateGroup %s: %v", u.name, err)
		}
		if err := store.AddUserToGroup(groupID, userID); err != nil {
			t.Fatalf("AddUserToGroup %s: %v", u.name, err)
		}
		groups[u.caller] = groupID
	}
	for _, g := range grants {
		if err := store.GrantConnectionPrivilege(groups[g.callerGroup],
			g.connID, g.level); err != nil {
			t.Fatalf("GrantConnectionPrivilege %d: %v", g.connID, err)
		}
	}

	userID, err := store.GetUserID("caller")
	if err != nil {
		t.Fatalf("GetUserID caller: %v", err)
	}
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, userID)
	ctx = context.WithValue(ctx, UsernameContextKey, "caller")
	if scope == nil {
		return ctx
	}

	_, stored, err := store.CreateToken("caller", "agreement token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if err := store.SetTokenConnectionScope(stored.ID, scope); err != nil {
		t.Fatalf("SetTokenConnectionScope: %v", err)
	}
	ctx = context.WithValue(ctx, IsAPITokenContextKey, true)
	return context.WithValue(ctx, TokenIDContextKey, stored.ID)
}

// TestVisibleConnectionIDsRestrictedLookupFails checks that a failing
// read of the restricted connections is an error rather than a wider
// list.
func TestVisibleConnectionIDsRestrictedLookupFails(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	ctx := agreementContext(t, store, nil, nil)
	checker := NewRBACCheckerWithSharing(store, agreementLookup)
	if _, err := store.db.Exec(
		"ALTER TABLE connection_privileges RENAME TO cp_gone"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	ids, all, err := checker.VisibleConnectionIDs(ctx,
		&stubVisibilityLister{connections: agreementConnections})
	if err == nil {
		t.Fatal("Expected an error when the restricted connections cannot be read")
	}
	if all || len(ids) != 0 {
		t.Errorf("Expected nothing visible on error, got ids=%v all=%v", ids, all)
	}
}

// TestRestrictedConnectionIDs checks that only grants naming a
// connection make it restricted.
func TestRestrictedConnectionIDs(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	groupID, err := store.CreateGroup("restricting", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	for _, id := range []int{ConnectionIDAll, 5, 9} {
		if err := store.GrantConnectionPrivilege(groupID, id,
			AccessLevelRead); err != nil {
			t.Fatalf("GrantConnectionPrivilege %d: %v", id, err)
		}
	}

	got, err := store.RestrictedConnectionIDs()
	if err != nil {
		t.Fatalf("RestrictedConnectionIDs: %v", err)
	}
	if len(got) != 2 || !got[5] || !got[9] {
		t.Errorf("got %v, want {5, 9}", got)
	}
	for _, id := range []int{1, 5} {
		restricted, err := store.IsConnectionAssignedToAnyGroup(id)
		if err != nil {
			t.Fatalf("IsConnectionAssignedToAnyGroup %d: %v", id, err)
		}
		if restricted != (id == 5) {
			t.Errorf("connection %d restricted = %v", id, restricted)
		}
	}

	// SQLite keeps a value that is not an integer as text, so a corrupt
	// row fails the scan rather than being read as some connection.
	if _, err := store.db.Exec(
		`INSERT INTO connection_privileges (group_id, connection_id, access_level)
         VALUES (?, 'corrupt', 'read')`, groupID); err != nil {
		t.Fatalf("insert corrupt row: %v", err)
	}
	if _, err := store.RestrictedConnectionIDs(); err == nil {
		t.Error("Expected an error from a row that cannot be scanned")
	}

	if _, err := store.db.Exec(
		"ALTER TABLE connection_privileges RENAME TO cp_gone"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := store.RestrictedConnectionIDs(); err == nil {
		t.Error("Expected an error from a missing table")
	}
}
