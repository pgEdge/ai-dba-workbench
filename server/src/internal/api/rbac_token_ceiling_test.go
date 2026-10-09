/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// The per-connection token ceiling (issue #471, ruling of 6 October 2026)
//
// A token may hand out, or take away, only what lies within its own
// effective access: its owner's privileges narrowed by its scope. These
// tests drive each RBAC write route with a caller that may do some of
// what the route offers but not all of it, so that each fails if its
// gate is reverted to a blanket refusal or removed.
// =============================================================================

// ceilingCaller is a superuser's token scoped to connection 5 at
// read_write and connection 7 read-only.
func ceilingCaller(t *testing.T, store *auth.AuthStore) scopeCaller {
	t.Helper()
	id := mustCreateScopedToken(t, store, "svc-ceiling", nil)
	if err := store.SetTokenConnectionScope(id, []auth.ScopedConnection{
		{ConnectionID: 5, AccessLevel: auth.AccessLevelReadWrite},
		{ConnectionID: 7, AccessLevel: auth.AccessLevelRead},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	return scopeCaller{name: "ceiling", wrap: func(r *http.Request) *http.Request {
		return withSuperuserToken(r, id)
	}}
}

// ownerTokenCaller is an unscoped token owned by a non-superuser, so
// that its owner's privileges alone bound it.
func ownerTokenCaller(t *testing.T, store *auth.AuthStore, username string,
	userID int64) scopeCaller {

	t.Helper()
	_, token, err := store.CreateToken(username, "owner token", nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	return scopeCaller{name: username, wrap: func(r *http.Request) *http.Request {
		ctx := context.WithValue(r.Context(), auth.IsAPITokenContextKey, true)
		ctx = context.WithValue(ctx, auth.TokenIDContextKey, token.ID)
		ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
		ctx = context.WithValue(ctx, auth.UsernameContextKey, username)
		return r.WithContext(ctx)
	}}
}

// TestConnectionGrantIsBoundedPerLevel checks that a token holding a
// connection read-only may grant read on it but not read_write.
func TestConnectionGrantIsBoundedPerLevel(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	caller := ceilingCaller(t, f.store)
	groupID := f.group(t, "grantees", nil)
	path := fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/connections", groupID)
	body := func(conn int, level string) string {
		return fmt.Sprintf(`{"connection_id":%d,"access_level":%q}`, conn, level)
	}

	assertRefusedWith(t, f.do(caller, http.MethodPost, path,
		body(7, auth.AccessLevelReadWrite)), refuseConnectionGrant)
	assertStatus(t, f.do(caller, http.MethodPost, path,
		body(7, auth.AccessLevelRead)), http.StatusNoContent)
	assertStatus(t, f.do(caller, http.MethodPost, path,
		body(5, auth.AccessLevelReadWrite)), http.StatusNoContent)
}

// TestConnectionGrantIsBoundedByOwner checks that an unscoped token
// owned by a non-superuser grants no more than its owner holds.
func TestConnectionGrantIsBoundedByOwner(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	ops := f.group(t, "ops", map[int]string{5: auth.AccessLevelRead})
	if err := f.store.GrantAdminPermission(ops, auth.PermManagePermissions); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}
	ownerID := f.user(t, "operator", ops)
	caller := ownerTokenCaller(t, f.store, "operator", ownerID)
	// Connection 6 is restricted to a group the operator is not in.
	f.group(t, "elsewhere", map[int]string{6: auth.AccessLevelReadWrite})

	groupID := f.group(t, "grantees", nil)
	path := fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/connections", groupID)
	body := func(conn int, level string) string {
		return fmt.Sprintf(`{"connection_id":%d,"access_level":%q}`, conn, level)
	}
	assertRefusedWith(t, f.do(caller, http.MethodPost, path,
		body(5, auth.AccessLevelReadWrite)), refuseConnectionGrant)
	assertRefusedWith(t, f.do(caller, http.MethodPost, path,
		body(6, auth.AccessLevelRead)), refuseConnectionGrant)
	assertStatus(t, f.do(caller, http.MethodPost, path,
		body(5, auth.AccessLevelRead)), http.StatusNoContent)
}

// TestConnectionRevokeNeedsReadWriteToLift checks the revoke rule: read
// on the connection suffices unless the grant is the connection's last,
// whose removal opens it to every user and so needs read_write.
func TestConnectionRevokeNeedsReadWriteToLift(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	caller := ceilingCaller(t, f.store)
	groupID := f.group(t, "revokees", map[int]string{
		5: auth.AccessLevelReadWrite,
		7: auth.AccessLevelReadWrite,
	})
	path := func(conn int) string {
		return fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/connections/%d",
			groupID, conn)
	}

	// The token holds 7 read-only and the grant is its last.
	assertRefusedWith(t, f.do(caller, http.MethodDelete, path(7), ""),
		refuseConnectionRevoke)
	// Another group's grant keeps 7 restricted, so read suffices.
	f.group(t, "keeps-7", map[int]string{7: auth.AccessLevelRead})
	assertStatus(t, f.do(caller, http.MethodDelete, path(7), ""),
		http.StatusNoContent)
	// The token holds 5 at read_write, so lifting it is within reach.
	assertStatus(t, f.do(caller, http.MethodDelete, path(5), ""),
		http.StatusNoContent)
}

// TestConnectionRevokeHidesConnectionExistence checks that revoking a
// grant on a connection the token cannot see is refused exactly as one
// on a connection that does not exist (issue #574).
func TestConnectionRevokeHidesConnectionExistence(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	caller := ceilingCaller(t, f.store)
	groupID := f.group(t, "hidden", map[int]string{9: auth.AccessLevelRead})
	path := func(conn int) string {
		return fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/connections/%d",
			groupID, conn)
	}

	hidden := f.do(caller, http.MethodDelete, path(9), "")
	missing := f.do(caller, http.MethodDelete, path(999), "")
	assertRefusedWith(t, hidden, refuseConnectionRevoke)
	if hidden.Code != missing.Code || hidden.Body.String() != missing.Body.String() {
		t.Errorf("Expected identical refusals, got %d %s and %d %s",
			hidden.Code, hidden.Body.String(), missing.Code, missing.Body.String())
	}
}

// TestReadTokenMayRevokeReadWrite documents the accepted trade-off: a
// token holding a connection read-only may still remove another group's
// read_write on it, since a revoke only narrows.
func TestReadTokenMayRevokeReadWrite(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	caller := ceilingCaller(t, f.store)
	groupID := f.group(t, "writers", map[int]string{7: auth.AccessLevelReadWrite})
	f.group(t, "readers", map[int]string{7: auth.AccessLevelRead})
	assertStatus(t, f.do(caller, http.MethodDelete,
		fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/connections/7", groupID), ""),
		http.StatusNoContent)
}

// TestGroupDeleteNeedsReadWriteToLift checks that deleting a group whose
// grant is a connection's last needs read_write on it, and that read
// suffices once another group keeps the connection restricted.
func TestGroupDeleteNeedsReadWriteToLift(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	caller := ceilingCaller(t, f.store)
	groupID := f.group(t, "doomed", map[int]string{7: auth.AccessLevelRead})
	path := fmt.Sprintf("/api/v1/rbac/groups/%d", groupID)

	assertRefusedWith(t, f.do(caller, http.MethodDelete, path, ""),
		refuseGroupRemoval)
	if g, err := f.store.GetGroup(groupID); err != nil || g == nil {
		t.Fatalf("Expected the group to survive the refusal (%v)", err)
	}
	f.group(t, "keeps-7", map[int]string{7: auth.AccessLevelRead})
	assertStatus(t, f.do(caller, http.MethodDelete, path, ""),
		http.StatusNoContent)
}

// TestGroupMemberRemovalNeedsRead checks that a token may remove a
// member from a group whose connections it can read, even read-only,
// but not from one reaching a connection it cannot see.
func TestGroupMemberRemovalNeedsRead(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	caller := ceilingCaller(t, f.store)
	readable := f.group(t, "readable", map[int]string{7: auth.AccessLevelReadWrite})
	hidden := f.group(t, "hidden", map[int]string{9: auth.AccessLevelRead})
	alice := f.user(t, "alice", readable)
	bob := f.user(t, "bob", hidden)
	member := func(group, id int64) string {
		return fmt.Sprintf("/api/v1/rbac/groups/%d/members/user/%d", group, id)
	}

	assertRefusedWith(t, f.do(caller, http.MethodDelete, member(hidden, bob), ""),
		refuseGroupRemoval)
	assertStatus(t, f.do(caller, http.MethodDelete, member(readable, alice), ""),
		http.StatusNoContent)
}

// TestGroupMembershipAddIsBoundedPerLevel checks that adding a member is
// allowed only when everything the group confers fits the ceiling, at
// each connection's level.
func TestGroupMembershipAddIsBoundedPerLevel(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	caller := ceilingCaller(t, f.store)
	readers := f.group(t, "readers", map[int]string{7: auth.AccessLevelRead})
	writers := f.group(t, "writers", map[int]string{7: auth.AccessLevelReadWrite})
	joiner := f.user(t, "joiner", 0)
	body := fmt.Sprintf(`{"user_id":%d}`, joiner)
	members := func(id int64) string {
		return fmt.Sprintf("/api/v1/rbac/groups/%d/members", id)
	}

	assertRefusedWith(t, f.do(caller, http.MethodPost, members(writers), body),
		refuseGroupAccess)
	assertStatus(t, f.do(caller, http.MethodPost, members(readers), body),
		http.StatusNoContent)
}

// TestSuperuserCreationNeedsUnrestrictedSuperuserToken checks that only
// a superuser's token unrestricted in every kind may make a superuser,
// whilst an ordinary user stays within reach of a bounded one.
func TestSuperuserCreationNeedsUnrestrictedSuperuserToken(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	body := func(name string, superuser bool) string {
		return fmt.Sprintf(`{"username":%q,"password":"Password1234!x","is_superuser":%t}`,
			name, superuser)
	}
	// narrowed holds both shared connections a new user reaches at
	// read_write, so an ordinary user is within its reach.
	assertRefusedWith(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/users",
		body("root2", true)), refuseSuperuser)
	assertStatus(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/users",
		body("plain", false)), http.StatusCreated)
	// ceiling holds 7 read-only, so a new user would exceed it.
	assertRefusedWith(t, f.do(ceilingCaller(t, f.store), http.MethodPost,
		"/api/v1/rbac/users", body("plain2", false)), refuseNewUserAccess)
	assertStatus(t, f.do(f.unscoped, http.MethodPost, "/api/v1/rbac/users",
		body("root2", true)), http.StatusCreated)
}
