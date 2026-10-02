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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Grants bounded by every kind of the acting token's scope (issue #471)
//
// What a token grants is bounded by the token's own scope in all three
// kinds. These tests cover the routes the 29 September review found
// open: creating a user who reaches unrestricted connections or public
// MCP items beyond the scope, resetting or deleting an account that
// reaches beyond it, granting an MCP item outside it, joining a group
// whose MCP or admin grants exceed it, and writing a token scope that
// exceeds it in the MCP or admin kind.
// =============================================================================

// reachFixture extends grantFixture with tokens bounded by MCP scope
// and by admin scope, and two registered MCP tools.
type reachFixture struct {
	*grantFixture
	mcpNarrowed   scopeCaller
	adminNarrowed scopeCaller
}

const (
	reachToolInScope  = "list_connections"
	reachToolOutScope = "query_datastore"
)

func newReachFixture(t *testing.T) (*reachFixture, func()) {
	t.Helper()
	gf, cleanup := newGrantFixture(t)
	for _, name := range []string{reachToolInScope, reachToolOutScope} {
		if _, err := gf.store.RegisterMCPPrivilege(name, auth.MCPPrivilegeTypeTool,
			name, false); err != nil {
			cleanup()
			t.Fatalf("RegisterMCPPrivilege failed: %v", err)
		}
	}

	mcpID := mustCreateScopedToken(t, gf.store, "svc-mcp-narrowed", nil)
	if err := gf.store.SetTokenMCPScopeByNames(mcpID,
		[]string{reachToolInScope}); err != nil {
		cleanup()
		t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
	}
	adminID := mustCreateScopedToken(t, gf.store, "svc-admin-narrowed", []string{
		auth.PermManageUsers, auth.PermManageGroups,
		auth.PermManagePermissions, auth.PermManageTokenScopes,
	})

	return &reachFixture{
		grantFixture: gf,
		mcpNarrowed: scopeCaller{name: "mcpNarrowed",
			wrap: func(r *http.Request) *http.Request {
				return withSuperuserToken(r, mcpID)
			}},
		adminNarrowed: scopeCaller{name: "adminNarrowed",
			wrap: func(r *http.Request) *http.Request {
				return withSuperuserToken(r, adminID)
			}},
	}, cleanup
}

// listConnections replaces the connections the handler's lister reports.
func (f *reachFixture) listConnections(items ...database.ConnectionListItem) {
	f.lister.ConnectionVisibilityLister = database.NewSliceVisibilityLister(items)
	f.h.SetConnectionLister(f.lister)
}

// mcpGroup creates a group holding the named MCP privilege.
func (f *reachFixture) mcpGroup(t *testing.T, name, privilege string) int64 {
	t.Helper()
	id := f.group(t, name, nil)
	if err := f.store.GrantMCPPrivilegeByName(id, privilege); err != nil {
		t.Fatalf("GrantMCPPrivilegeByName failed: %v", err)
	}
	return id
}

func userBody(name string) string {
	return fmt.Sprintf(`{"username":%q,"password":"Password1234!x"}`, name)
}

// TestCreateUserRespectsUnrestrictedReach covers the review's first
// reproduction: a new user, who belongs to no group, still reaches
// every shared connection no group restricts, so a token bounded to
// some connections may not create one while such a connection lies
// outside its scope.
func TestCreateUserRespectsUnrestrictedReach(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	f.listConnections(
		database.ConnectionListItem{ID: 5, IsShared: true},
		database.ConnectionListItem{ID: 3, IsShared: true},
	)

	assertGrantRefused(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/users",
		userBody("eve")))
	if u, _ := f.store.GetUser("eve"); u != nil {
		t.Fatal("Expected no user to be created on refusal")
	}
	for i, caller := range f.open() {
		assertStatus(t, f.do(caller, http.MethodPost, "/api/v1/rbac/users",
			userBody(fmt.Sprintf("open-%d", i))), http.StatusCreated)
	}

	// A group grant on connection 3 restricts it, so a new user no
	// longer reaches it.
	f.group(t, "holders", map[int]string{3: auth.AccessLevelRead})
	assertStatus(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/users",
		userBody("eve")), http.StatusCreated)
}

// TestCreateUserFailsClosedWithoutLister checks that a connection-bounded
// token cannot create a user when the connections cannot be enumerated.
func TestCreateUserFailsClosedWithoutLister(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	f.h.SetConnectionLister(nil)

	assertGrantRefused(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/users",
		userBody("eve")))
	assertStatus(t, f.do(f.mcpNarrowed, http.MethodPost, "/api/v1/rbac/users",
		userBody("eve")), http.StatusCreated)
}

// TestCreateUserRespectsMCPAndAdminScope checks that a superuser is
// refused to any bounded token, and that a public MCP item outside an
// MCP-bounded token's scope counts towards a new user's reach.
func TestCreateUserRespectsMCPAndAdminScope(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()

	// The admin-narrowed token is no longer a superuser, so the
	// superuser gate itself refuses it; the MCP-narrowed one keeps
	// superuser status, and the scope gate refuses it.
	body := `{"username":"would-be-root","password":"Password1234!x","is_superuser":true}`
	assertGrantRefused(t, f.do(f.mcpNarrowed, http.MethodPost, "/api/v1/rbac/users", body))
	assertStatus(t, f.do(f.adminNarrowed, http.MethodPost, "/api/v1/rbac/users", body),
		http.StatusForbidden)
	if u, _ := f.store.GetUser("would-be-root"); u != nil {
		t.Fatal("Expected no superuser to be created on refusal")
	}

	assertStatus(t, f.do(f.mcpNarrowed, http.MethodPost, "/api/v1/rbac/users",
		userBody("plain")), http.StatusCreated)
	if _, err := f.store.RegisterMCPPrivilege("public_tool",
		auth.MCPPrivilegeTypeTool, "public", true); err != nil {
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}
	assertGrantRefused(t, f.do(f.mcpNarrowed, http.MethodPost, "/api/v1/rbac/users",
		userBody("eve")))
}

// TestUpdateUserRespectsMCPAndUnrestrictedReach covers the password
// reset and re-enable takeover for the reach the connection-only check
// missed: unrestricted connections and MCP grants.
func TestUpdateUserRespectsMCPAndUnrestrictedReach(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	bob := f.user(t, "bob", f.mcpGroup(t, "datastore", reachToolOutScope))
	path := fmt.Sprintf("/api/v1/rbac/users/%d", bob)
	reset := `{"password":"Another-Password-9"}`

	assertGrantRefused(t, f.do(f.mcpNarrowed, http.MethodPut, path, reset))
	assertStatus(t, f.do(f.narrowed, http.MethodPut, path, reset), http.StatusOK)

	f.listConnections(database.ConnectionListItem{ID: 3, IsShared: true})
	assertGrantRefused(t, f.do(f.narrowed, http.MethodPut, path, reset))
	assertGrantRefused(t, f.do(f.narrowed, http.MethodPut, path, `{"enabled":true}`))
	assertStatus(t, f.do(f.session, http.MethodPut, path, reset), http.StatusOK)

	// Promotion needs every kind unrestricted.
	assertGrantRefused(t, f.do(f.mcpNarrowed, http.MethodPut, path,
		`{"is_superuser":true}`))
}

// TestDeleteUserRespectsTokenScope covers delete-and-recreate: deleting
// an account a bounded token could not take over is refused, including
// one that owns an unshared connection, since ownership is matched by
// name and would pass to whoever recreates it.
func TestDeleteUserRespectsTokenScope(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()

	// A sharing-aware checker, so that an unshared connection counts
	// only for its owner, as CanAccessConnection treats it.
	sharing := auth.NewRBACCheckerWithSharing(f.store,
		func(context.Context, int) (bool, string, error) { return false, "", nil })
	f.h = NewRBACHandler(f.store, sharing)
	f.listConnections(
		database.ConnectionListItem{ID: 5, IsShared: true},
		database.ConnectionListItem{ID: 8, IsShared: false, OwnerUsername: "bob"},
	)

	bob := f.user(t, "bob", 0)
	eve := f.user(t, "eve", 0)
	beyond := f.user(t, "beyond",
		f.group(t, "beyond", map[int]string{6: auth.AccessLevelRead}))
	datastore := f.user(t, "datastore", f.mcpGroup(t, "ds", reachToolOutScope))
	path := func(id int64) string { return fmt.Sprintf("/api/v1/rbac/users/%d", id) }

	assertGrantRefused(t, f.do(f.narrowed, http.MethodDelete, path(bob), ""))
	assertGrantRefused(t, f.do(f.narrowed, http.MethodDelete, path(beyond), ""))
	assertGrantRefused(t, f.do(f.mcpNarrowed, http.MethodDelete, path(datastore), ""))
	if u, _ := f.store.GetUser("bob"); u == nil {
		t.Fatal("Expected bob to survive the refusal")
	}

	// A new user named bob would reach bob's unshared connection.
	assertStatus(t, f.do(f.narrowed, http.MethodDelete, path(eve), ""),
		http.StatusNoContent)
	assertStatus(t, f.do(f.session, http.MethodDelete, path(bob), ""),
		http.StatusNoContent)
	assertGrantRefused(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/users",
		userBody("bob")))
	assertStatus(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/users",
		userBody("eve")), http.StatusCreated)
	assertStatus(t, f.do(f.unscoped, http.MethodDelete, path(beyond), ""),
		http.StatusNoContent)
}

// TestGroupMCPGrantRespectsTokenScope covers the review's MCP route: a
// token whose MCP scope is list_connections may not grant a group
// query_datastore, or the wildcard.
func TestGroupMCPGrantRespectsTokenScope(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	groupID := f.group(t, "grantees", nil)
	path := fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/mcp", groupID)
	body := func(name string) string { return fmt.Sprintf(`{"privilege":%q}`, name) }

	for _, name := range []string{reachToolOutScope, "*"} {
		assertGrantRefused(t, f.do(f.mcpNarrowed, http.MethodPost, path, body(name)))
	}
	if privs, err := f.store.GetGroupEffectiveMCPPrivileges(groupID); err != nil ||
		len(privs) != 0 {
		t.Fatalf("Expected no MCP grant to be written, got %v (%v)", privs, err)
	}
	assertStatus(t, f.do(f.mcpNarrowed, http.MethodPost, path, body(reachToolInScope)),
		http.StatusNoContent)
	// The connection kind has no bearing on an MCP grant.
	assertStatus(t, f.do(f.narrowed, http.MethodPost, path, body(reachToolOutScope)),
		http.StatusNoContent)
	for _, caller := range f.open() {
		assertStatus(t, f.do(caller, http.MethodPost, path, body("*")),
			http.StatusNoContent)
	}
}

// TestGroupMembershipRespectsMCPAndAdminScope covers the second half of
// the MCP route, joining a group that holds query_datastore, and the
// admin kind of the same check.
func TestGroupMembershipRespectsMCPAndAdminScope(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	datastore := f.mcpGroup(t, "datastore", reachToolOutScope)
	listers := f.mcpGroup(t, "listers", reachToolInScope)
	probers := f.group(t, "probers", nil)
	if err := f.store.GrantAdminPermission(probers, auth.PermManageProbes); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}
	userID := f.user(t, "joiner", 0)
	members := func(id int64) string {
		return fmt.Sprintf("/api/v1/rbac/groups/%d/members", id)
	}
	body := fmt.Sprintf(`{"user_id":%d}`, userID)

	assertGrantRefused(t, f.do(f.mcpNarrowed, http.MethodPost, members(datastore), body))
	assertGrantRefused(t, f.do(f.adminNarrowed, http.MethodPost, members(probers), body))
	if groups, err := f.store.GetUserGroups(userID); err != nil || len(groups) != 0 {
		t.Fatalf("Expected no membership to be written, got %v (%v)", groups, err)
	}
	assertStatus(t, f.do(f.mcpNarrowed, http.MethodPost, members(listers), body),
		http.StatusNoContent)
	assertStatus(t, f.do(f.adminNarrowed, http.MethodPost, members(datastore), body),
		http.StatusNoContent)
}

// TestAccountTakeoverPermissionsReachEverything covers the 1 October
// review: manage_users, manage_groups, manage_permissions and
// manage_token_scopes each let their holder acquire any MCP item or
// admin permission, so a token bounded in either kind may not add
// someone to a group holding one, nor reset the password of a user who
// already holds one, even when its admin scope names the permission.
func TestAccountTakeoverPermissionsReachEverything(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	members := func(id int64) string {
		return fmt.Sprintf("/api/v1/rbac/groups/%d/members", id)
	}
	reset := `{"password":"Another-Password-9"}`

	for _, perm := range []string{
		auth.PermManageUsers, auth.PermManageGroups,
		auth.PermManagePermissions, auth.PermManageTokenScopes,
	} {
		holders := f.group(t, "holders-"+perm, nil)
		if err := f.store.GrantAdminPermission(holders, perm); err != nil {
			t.Fatalf("GrantAdminPermission failed: %v", err)
		}
		joiner := f.user(t, "joiner-"+perm, 0)
		holder := f.user(t, "holder-"+perm, holders)
		body := fmt.Sprintf(`{"user_id":%d}`, joiner)
		userPath := fmt.Sprintf("/api/v1/rbac/users/%d", holder)

		for _, caller := range []scopeCaller{f.mcpNarrowed, f.adminNarrowed} {
			assertGrantRefused(t, f.do(caller, http.MethodPost, members(holders), body))
			assertGrantRefused(t, f.do(caller, http.MethodPut, userPath, reset))
		}
		if groups, err := f.store.GetUserGroups(joiner); err != nil || len(groups) != 0 {
			t.Fatalf("Expected no membership to be written, got %v (%v)", groups, err)
		}
		for _, caller := range f.open() {
			assertStatus(t, f.do(caller, http.MethodPut, userPath, reset), http.StatusOK)
		}
	}
}

// TestCreateTokenRespectsMCPScope checks that a token bounded by MCP
// scope may not mint a token for an owner who holds an MCP item beyond
// it.
func TestCreateTokenRespectsMCPScope(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	f.user(t, "bob", f.mcpGroup(t, "datastore", reachToolOutScope))
	f.user(t, "lister", f.mcpGroup(t, "listers", reachToolInScope))
	body := func(owner string) string { return fmt.Sprintf(`{"owner_username":%q}`, owner) }

	assertGrantRefused(t, f.do(f.mcpNarrowed, http.MethodPost, "/api/v1/rbac/tokens",
		body("bob")))
	assertStatus(t, f.do(f.mcpNarrowed, http.MethodPost, "/api/v1/rbac/tokens",
		body("lister")), http.StatusCreated)
}

// TestSetTokenScopeComparesEveryKind covers the review's last route:
// a scope write is judged on its MCP and admin kinds as well as its
// connections.
func TestSetTokenScopeComparesEveryKind(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	f.user(t, "bob", f.mcpGroup(t, "datastore", reachToolOutScope))
	_, target, err := f.store.CreateToken("bob", "target", nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	path := fmt.Sprintf("/api/v1/rbac/tokens/%d/scope", target.ID)

	refused := []struct {
		caller scopeCaller
		body   string
	}{
		{f.mcpNarrowed, fmt.Sprintf(`{"mcp_privileges":[%q]}`, reachToolOutScope)},
		{f.mcpNarrowed, `{"mcp_privileges":["*"]}`},
		// Leaving the MCP kind alone keeps the owner's query_datastore.
		{f.mcpNarrowed, `{"connections":[{"connection_id":5,"access_level":"read"}]}`},
		{f.adminNarrowed, `{"admin_permissions":["manage_probes"]}`},
	}
	for _, tc := range refused {
		assertGrantRefused(t, f.do(tc.caller, http.MethodPut, path, tc.body))
	}
	if scope, err := f.store.GetTokenScope(target.ID); err != nil || scope != nil {
		t.Fatalf("Expected target to stay unscoped, got %+v (%v)", scope, err)
	}

	assertStatus(t, f.do(f.mcpNarrowed, http.MethodPut, path,
		fmt.Sprintf(`{"mcp_privileges":[%q]}`, reachToolInScope)), http.StatusNoContent)
	assertStatus(t, f.do(f.adminNarrowed, http.MethodPut, path,
		`{"admin_permissions":["manage_users"]}`), http.StatusNoContent)

	// Clearing would hand the token its owner's query_datastore again.
	assertGrantRefused(t, f.do(f.mcpNarrowed, http.MethodDelete, path, ""))
	assertStatus(t, f.do(f.session, http.MethodDelete, path, ""), http.StatusNoContent)
}

// TestSetTokenScopeFailsClosedOnUnreadableScope checks that a scope
// write by a bounded token reports a failure when the target's stored
// MCP scope cannot be read. (Dropping the admin scope table would
// refuse the caller's own manage_token_scopes check first.)
func TestSetTokenScopeFailsClosedOnUnreadableScope(t *testing.T) {
	for _, table := range []string{"token_mcp_scope"} {
		t.Run(table, func(t *testing.T) {
			h, store, dir, cleanup := createTestRBACHandlerWithDir(t)
			defer cleanup()
			_, _, _, narrowed, _ := scopedCallers(t, store)
			target := mustCreateScopedToken(t, store, "svc-target", nil)
			dropAuthTable(t, dir, table)

			req := narrowed.wrap(httptest.NewRequest(http.MethodPut,
				fmt.Sprintf("/api/v1/rbac/tokens/%d/scope", target),
				strings.NewReader(`{"connections":[{"connection_id":5,"access_level":"read"}]}`)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.handleTokenSubpath(rec, req)
			assertStatus(t, rec, http.StatusInternalServerError)
		})
	}
}

// =============================================================================
// Security audit fixes (issue #471, audit of 7fa4cca1)
// =============================================================================

// TestOwnedRestrictedConnectionCountsTowardsReach covers the audit's
// takeover: alice owns connection 9, which a group restricts, and is no
// longer in that group. updateConnection and deleteConnection admit an
// owner whatever restricts the connection, so a token whose scope does
// not cover connection 9 may not reset her password, delete her, or
// recreate her account.
func TestOwnedRestrictedConnectionCountsTowardsReach(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	f.group(t, "prod-dba", map[int]string{9: auth.AccessLevelReadWrite})
	f.listConnections(
		database.ConnectionListItem{ID: 5, IsShared: true},
		database.ConnectionListItem{ID: 9, IsShared: true, OwnerUsername: "alice"},
	)
	alice := f.user(t, "alice", 0)
	path := fmt.Sprintf("/api/v1/rbac/users/%d", alice)

	assertGrantRefused(t, f.do(f.narrowed, http.MethodPut, path,
		`{"password":"Another-Password-9"}`))
	assertGrantRefused(t, f.do(f.narrowed, http.MethodDelete, path, ""))
	if u, _ := f.store.GetUser("alice"); u == nil {
		t.Fatal("Expected alice to survive the refusal")
	}

	assertStatus(t, f.do(f.session, http.MethodDelete, path, ""), http.StatusNoContent)
	assertGrantRefused(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/users",
		userBody("alice")))
	if u, _ := f.store.GetUser("alice"); u != nil {
		t.Fatal("Expected alice not to be recreated on refusal")
	}
	assertStatus(t, f.do(f.wildcard, http.MethodPost, "/api/v1/rbac/users",
		userBody("alice")), http.StatusCreated)
}

// TestRemoveGroupMemberRespectsTokenScope checks that a bounded token
// may not remove a user or a group from a group whose grants its scope
// does not cover, and may from one whose grants it does.
func TestRemoveGroupMemberRespectsTokenScope(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	prod := f.group(t, "prod-dba", map[int]string{9: auth.AccessLevelReadWrite})
	child := f.group(t, "prod-child", nil)
	if err := f.store.AddGroupToGroup(prod, child); err != nil {
		t.Fatalf("AddGroupToGroup failed: %v", err)
	}
	alice := f.user(t, "alice", prod)
	staff := f.group(t, "staff", map[int]string{5: auth.AccessLevelRead})
	bob := f.user(t, "bob", staff)
	member := func(group int64, kind string, id int64) string {
		return fmt.Sprintf("/api/v1/rbac/groups/%d/members/%s/%d", group, kind, id)
	}

	assertGrantRefused(t, f.do(f.narrowed, http.MethodDelete, member(prod, "user", alice), ""))
	assertGrantRefused(t, f.do(f.narrowed, http.MethodDelete, member(prod, "group", child), ""))
	if groups, err := f.store.GetUserGroups(alice); err != nil || len(groups) != 1 {
		t.Fatalf("Expected alice to stay in prod-dba, got %v (%v)", groups, err)
	}
	assertStatus(t, f.do(f.narrowed, http.MethodDelete, member(staff, "user", bob), ""),
		http.StatusNoContent)
	assertStatus(t, f.do(f.session, http.MethodDelete, member(prod, "user", alice), ""),
		http.StatusNoContent)
	assertStatus(t, f.do(f.unscoped, http.MethodDelete, member(prod, "group", child), ""),
		http.StatusNoContent)
}

// TestRenameGroupRespectsTokenScope checks that a bounded token may
// rename only a group whose grants its scope covers, and never a group
// the OIDC group map names, or to such a name, since federation matches
// groups by name.
func TestRenameGroupRespectsTokenScope(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	f.h.SetFederatedGroupMap(map[string]string{
		"idp-admins":  "wb-admins",
		"idp-ops":     "wb-ops",
		"idp-ignored": "",
	})
	prod := f.group(t, "prod-dba", map[int]string{9: auth.AccessLevelReadWrite})
	federated := f.group(t, "wb-admins", nil)
	plain := f.group(t, "plain", nil)
	path := func(id int64) string { return fmt.Sprintf("/api/v1/rbac/groups/%d", id) }
	rename := func(name string) string { return fmt.Sprintf(`{"name":%q}`, name) }

	assertGrantRefused(t, f.do(f.narrowed, http.MethodPut, path(prod), rename("prod-dba-2")))
	assertGrantRefused(t, f.do(f.narrowed, http.MethodPut, path(federated), rename("wb-other")))
	assertGrantRefused(t, f.do(f.narrowed, http.MethodPut, path(plain), rename("wb-ops")))
	if g, err := f.store.GetGroup(prod); err != nil || g.Name != "prod-dba" {
		t.Fatalf("Expected prod-dba to keep its name, got %+v (%v)", g, err)
	}

	// A description-only update, or one that keeps the name, renames
	// nothing.
	assertStatus(t, f.do(f.narrowed, http.MethodPut, path(prod),
		`{"description":"production"}`), http.StatusOK)
	assertStatus(t, f.do(f.narrowed, http.MethodPut, path(federated),
		`{"name":"wb-admins","description":"admins"}`), http.StatusOK)
	assertStatus(t, f.do(f.narrowed, http.MethodPut, path(plain), rename("plain-2")),
		http.StatusOK)
	assertStatus(t, f.do(f.narrowed, http.MethodPut, path(9999), rename("ghost")),
		http.StatusNotFound)
	for i, caller := range f.open() {
		assertStatus(t, f.do(caller, http.MethodPut, path(federated),
			rename(fmt.Sprintf("wb-admins-%d", i))), http.StatusOK)
	}
}

// TestRenameGroupFailsClosedOnLookupError checks that a bounded token's
// rename is refused, and the refusal audited, when the group cannot be
// read.
func TestRenameGroupFailsClosedOnLookupError(t *testing.T) {
	h, store, dir, cleanup := createTestRBACHandlerWithDir(t)
	defer cleanup()
	_, _, _, narrowed, _ := scopedCallers(t, store)
	groupID, err := store.CreateGroup("doomed", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	dropAuthTable(t, dir, "user_groups")

	req := narrowed.wrap(httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/api/v1/rbac/groups/%d", groupID),
		strings.NewReader(`{"name":"renamed"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.handleGroupSubpath(rec, req)
	assertStatus(t, rec, http.StatusInternalServerError)
	assertDenialRecorded(t, store, "group.update", "Failed to get group")
}

// TestSetTokenScopeRecordsUnreadableScopeDenial checks that the 500
// returned when the target's stored scope cannot be read is audited.
func TestSetTokenScopeRecordsUnreadableScopeDenial(t *testing.T) {
	h, store, dir, cleanup := createTestRBACHandlerWithDir(t)
	defer cleanup()
	_, _, _, narrowed, _ := scopedCallers(t, store)
	target := mustCreateScopedToken(t, store, "svc-target", nil)
	dropAuthTable(t, dir, "token_mcp_scope")

	req := narrowed.wrap(httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/api/v1/rbac/tokens/%d/scope", target),
		strings.NewReader(`{"connections":[{"connection_id":5,"access_level":"read"}]}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.handleTokenSubpath(rec, req)
	assertStatus(t, rec, http.StatusInternalServerError)
	assertDenialRecorded(t, store, "token.scope.set", "Failed to get token scope")
}

// assertDenialRecorded checks that exactly one denied event with the
// given action and reason was written, and that it names its target.
func assertDenialRecorded(t *testing.T, store *auth.AuthStore, action, reason string) {
	t.Helper()
	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Action: action, Outcome: string(auth.OutcomeDenied)})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 %s denial, got %d", action, len(events))
	}
	if events[0].Error != reason {
		t.Errorf("Expected reason %q, got %q", reason, events[0].Error)
	}
	var details struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(events[0].Details, &details); err != nil {
		t.Fatalf("Failed to decode details %s: %v", events[0].Details, err)
	}
	if details.Target == "" || details.Target == unmatchedDenialTarget {
		t.Errorf("Expected the denial to name its target, got %s", events[0].Details)
	}
}

// TestConnectionScopeRefusalIsAudited checks that the connection
// handler's token-scope refusals on update and delete are written to
// the audit log through the RBAC handler's denial recorder. Both
// handlers accept only a session bearer today (getUserInfoCompat
// answers 401 to an API token), so no real token reaches these gates;
// the test pairs a session bearer with an incomplete token context to
// exercise the defensive check that stands ready should that change.
// TestConnectionClusterMoveScopeRefusalIsAudited covers the refusal a
// real token does reach.
func TestConnectionScopeRefusalIsAudited(t *testing.T) {
	rbac, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	if err := store.CreateUser("conn-owner", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	session, _, err := store.AuthenticateUser("conn-owner", "Password1234")
	if err != nil {
		t.Fatalf("AuthenticateUser failed: %v", err)
	}
	h := &ConnectionHandler{authStore: store, rbacChecker: auth.NewRBACChecker(store)}
	h.SetDenialRecorder(rbac.RecordDenial)

	for _, tc := range []struct {
		method, action string
		serve          func(http.ResponseWriter, *http.Request, int)
	}{
		{http.MethodPut, "connection.update", h.updateConnection},
		{http.MethodDelete, "connection.delete", h.deleteConnection},
	} {
		req := withBearer(httptest.NewRequest(tc.method, "/api/v1/connections/9", nil), session)
		req = withIncompleteToken(req)
		rec := httptest.NewRecorder()
		tc.serve(rec, req, 9)
		assertError(t, rec, http.StatusForbidden, connectionOutOfTokenScope)
		assertDenialRecorded(t, store, tc.action, connectionOutOfTokenScope)
	}
}

// TestConnectionClusterMoveScopeRefusalIsAudited checks that a token
// whose connection scope names a connection only at read, and so can
// see it, is refused moving it between clusters with an audited
// connection.cluster.update denial.
func TestConnectionClusterMoveScopeRefusalIsAudited(t *testing.T) {
	rbac, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	if err := store.CreateServiceAccount("svc-mover", "", "", ""); err != nil {
		t.Fatalf("CreateServiceAccount failed: %v", err)
	}
	_, token, err := store.CreateToken("svc-mover", "cluster move", nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	if err := store.SetTokenConnectionScope(token.ID, []auth.ScopedConnection{
		{ConnectionID: 7, AccessLevel: auth.AccessLevelRead},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	h := &ConnectionHandler{authStore: store, rbacChecker: auth.NewRBACChecker(store)}
	h.SetDenialRecorder(rbac.RecordDenial)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/connections/7/cluster",
		strings.NewReader(`{"cluster_id":1}`))
	req = withSuperuserToken(req, token.ID)
	rec := httptest.NewRecorder()
	h.handleUpdateConnectionCluster(rec, req, 7)
	assertError(t, rec, http.StatusForbidden, connectionOutOfTokenScope)
	assertDenialRecorded(t, store, "connection.cluster.update", connectionOutOfTokenScope)
}

// =============================================================================
// Security audit fixes (issue #471, audit of the denial coalescing)
// =============================================================================

// TestOwnedClusterGroupCountsTowardsReach covers VULN-103: alice owns a
// cluster group whose members are connections 5 and 9, and the cluster
// group handlers admit an owner, so the group's members are part of her
// reach. A token whose scope covers 5 but not 9 may not reset her
// password, delete or recreate her, mint her a token, or set or clear
// the scope of a token she owns.
func TestOwnedClusterGroupCountsTowardsReach(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	alice := f.user(t, "alice", 0)
	_, aliceToken, err := f.store.CreateToken("alice", "alice's token", nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	userPath := fmt.Sprintf("/api/v1/rbac/users/%d", alice)
	scopePath := fmt.Sprintf("/api/v1/rbac/tokens/%d/scope", aliceToken.ID)
	reset := `{"password":"Another-Password-9"}`
	mint := `{"owner_username":"alice"}`
	// A scope that leaves the connection kind alone keeps the owner's
	// whole connection reach.
	narrow := fmt.Sprintf(`{"mcp_privileges":[%q]}`, reachToolInScope)

	// Every member inside the scope: each route is allowed.
	f.lister.owned["alice"] = []int{5, 7}
	assertStatus(t, f.do(f.narrowed, http.MethodPut, userPath, reset), http.StatusOK)
	assertStatus(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/tokens", mint),
		http.StatusCreated)
	assertStatus(t, f.do(f.narrowed, http.MethodPut, scopePath, narrow),
		http.StatusNoContent)
	assertStatus(t, f.do(f.narrowed, http.MethodDelete, scopePath, ""),
		http.StatusNoContent)

	// A member outside the scope: each route is refused.
	f.lister.owned["alice"] = []int{5, 9}
	assertGrantRefused(t, f.do(f.narrowed, http.MethodPut, userPath, reset))
	assertGrantRefused(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/tokens", mint))
	assertGrantRefused(t, f.do(f.narrowed, http.MethodPut, scopePath, narrow))
	assertGrantRefused(t, f.do(f.narrowed, http.MethodDelete, userPath, ""))
	if u, _ := f.store.GetUser("alice"); u == nil {
		t.Fatal("Expected alice to survive the refusal")
	}
	assertStatus(t, f.do(f.session, http.MethodPut, scopePath, narrow), http.StatusNoContent)
	assertGrantRefused(t, f.do(f.narrowed, http.MethodDelete, scopePath, ""))

	// Ownership is matched by name, so a recreated alice owns the group.
	assertStatus(t, f.do(f.session, http.MethodDelete, userPath, ""), http.StatusNoContent)
	assertGrantRefused(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/users",
		userBody("alice")))
	assertStatus(t, f.do(f.wildcard, http.MethodPost, "/api/v1/rbac/users",
		userBody("alice")), http.StatusCreated)

	// A lookup that fails is refused, and open callers are unaffected.
	f.lister.err = errors.New("lookup failed")
	assertGrantRefused(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/users",
		userBody("eve")))
	for i, caller := range f.open() {
		assertStatus(t, f.do(caller, http.MethodPost, "/api/v1/rbac/users",
			userBody(fmt.Sprintf("open-%d", i))), http.StatusCreated)
	}
}

// TestOwnedClusterGroupFailsClosedWithoutGroupLister checks that a
// connection lister unable to enumerate owned cluster groups refuses a
// bounded token.
func TestOwnedClusterGroupFailsClosedWithoutGroupLister(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	f.h.SetConnectionLister(database.NewSliceVisibilityLister(
		[]database.ConnectionListItem{{ID: 5, IsShared: true}}))

	assertGrantRefused(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/users",
		userBody("eve")))
	assertStatus(t, f.do(f.session, http.MethodPost, "/api/v1/rbac/users",
		userBody("eve")), http.StatusCreated)
}

// TestFederatedGroupCreateAndDeleteRespectTokenScope covers VULN-104:
// federation finds a group by name, so a bounded token may neither
// create nor delete a group the OIDC group map names, and each refusal
// is audited.
func TestFederatedGroupCreateAndDeleteRespectTokenScope(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	f.h.SetFederatedGroupMap(map[string]string{"idp-admins": "wb-admins"})
	create := func(name string) string { return fmt.Sprintf(`{"name":%q}`, name) }
	path := func(id int64) string { return fmt.Sprintf("/api/v1/rbac/groups/%d", id) }

	assertGrantRefused(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/groups",
		create("wb-admins")))
	assertDenialRecorded(t, f.store, "group.create", grantOutOfTokenScope)
	if groups, err := f.store.ListGroups(); err != nil || len(groups) != 0 {
		t.Fatalf("Expected no group to be created, got %v (%v)", groups, err)
	}
	assertStatus(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/groups",
		create("plain")), http.StatusCreated)

	federated := f.group(t, "wb-admins", nil)
	plain, err := f.store.GetGroupByName("plain")
	if err != nil || plain == nil {
		t.Fatalf("GetGroupByName failed: %v", err)
	}
	assertGrantRefused(t, f.do(f.narrowed, http.MethodDelete, path(federated), ""))
	assertDenialRecorded(t, f.store, "group.delete", grantOutOfTokenScope)
	if g, err := f.store.GetGroup(federated); err != nil || g == nil {
		t.Fatalf("Expected wb-admins to survive, got %+v (%v)", g, err)
	}
	assertStatus(t, f.do(f.narrowed, http.MethodDelete, path(plain.ID), ""),
		http.StatusNoContent)
	// A group that does not exist is left to the delete to report.
	if rec := f.do(f.narrowed, http.MethodDelete, path(9999), ""); rec.Code == http.StatusForbidden {
		t.Errorf("Expected a missing group not to be refused on scope, got %s", rec.Body)
	}

	for i, caller := range f.open() {
		assertStatus(t, f.do(caller, http.MethodDelete, path(federated), ""),
			http.StatusNoContent)
		assertStatus(t, f.do(caller, http.MethodPost, "/api/v1/rbac/groups",
			create("wb-admins")), http.StatusCreated)
		g, err := f.store.GetGroupByName("wb-admins")
		if err != nil || g == nil {
			t.Fatalf("caller %d: GetGroupByName failed: %v", i, err)
		}
		federated = g.ID
	}
}

// TestDeleteGroupFailsClosedOnLookupError checks that a bounded token's
// delete is refused with 500, and audited, when the group cannot be read
// to see whether federation names it.
func TestDeleteGroupFailsClosedOnLookupError(t *testing.T) {
	h, store, dir, cleanup := createTestRBACHandlerWithDir(t)
	defer cleanup()
	h.SetFederatedGroupMap(map[string]string{"idp-admins": "wb-admins"})
	_, _, _, narrowed, _ := scopedCallers(t, store)
	groupID, err := store.CreateGroup("doomed", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	dropAuthTable(t, dir, "user_groups")

	req := narrowed.wrap(httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/api/v1/rbac/groups/%d", groupID), nil))
	rec := httptest.NewRecorder()
	h.handleGroupSubpath(rec, req)
	assertStatus(t, rec, http.StatusInternalServerError)
	assertDenialRecorded(t, store, "group.delete", "Failed to get group")
}
