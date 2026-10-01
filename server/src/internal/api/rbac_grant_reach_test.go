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
	f.h.SetConnectionLister(database.NewSliceVisibilityLister(items))
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
