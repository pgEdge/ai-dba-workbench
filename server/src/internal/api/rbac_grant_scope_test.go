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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Grants bounded by the acting token's connection scope (issue #471)
//
// manage_users, manage_permissions, manage_groups and manage_token_scopes
// say nothing about which connections a token was issued for. These
// tests check that a superuser's token narrowed to connections 5 and 7
// (or to connection 5 read-only) cannot hand a user, a group or another
// token access beyond that, whilst sessions, unscoped tokens and
// wildcard tokens keep the access they had.
// =============================================================================

// grantFixture holds the handler, the store and the callers the grant
// tests share.
type grantFixture struct {
	h        *RBACHandler
	store    *auth.AuthStore
	session  scopeCaller
	unscoped scopeCaller
	wildcard scopeCaller
	narrowed scopeCaller
	readOnly scopeCaller
}

func newGrantFixture(t *testing.T) (*grantFixture, func()) {
	t.Helper()
	h, store, cleanup := createTestRBACHandler(t)
	// Connections 5 and 7 are shared and lie inside the narrowed scope
	// at read_write, so a new user's reach is in scope until a test
	// lists a connection beyond it.
	h.SetConnectionLister(database.NewSliceVisibilityLister(
		[]database.ConnectionListItem{
			{ID: 5, IsShared: true},
			{ID: 7, IsShared: true},
		}))
	f := &grantFixture{h: h, store: store}
	f.session, f.unscoped, f.wildcard, f.narrowed, f.readOnly = scopedCallers(t, store)
	return f, cleanup
}

// open lists the callers that are never bounded.
func (f *grantFixture) open() []scopeCaller {
	return []scopeCaller{f.session, f.unscoped, f.wildcard}
}

// do sends a request through the RBAC router that owns the path.
func (f *grantFixture) do(caller scopeCaller, method, path,
	body string) *httptest.ResponseRecorder {

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req = caller.wrap(req)
	rec := httptest.NewRecorder()

	switch {
	case path == "/api/v1/rbac/users":
		f.h.handleUsers(rec, req)
	case strings.HasPrefix(path, "/api/v1/rbac/users/"):
		f.h.handleUserSubpath(rec, req)
	case strings.HasPrefix(path, "/api/v1/rbac/groups/"):
		f.h.handleGroupSubpath(rec, req)
	case path == "/api/v1/rbac/tokens":
		f.h.handleTokens(rec, req)
	default:
		f.h.handleTokenSubpath(rec, req)
	}
	return rec
}

// group creates a group holding the given connection grants.
func (f *grantFixture) group(t *testing.T, name string,
	grants map[int]string) int64 {

	t.Helper()
	id, err := f.store.CreateGroup(name, "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	for conn, level := range grants {
		if err := f.store.GrantConnectionPrivilege(id, conn, level); err != nil {
			t.Fatalf("GrantConnectionPrivilege failed: %v", err)
		}
	}
	return id
}

// user creates a local user, optionally in a group, and returns its id.
func (f *grantFixture) user(t *testing.T, name string, groupID int64) int64 {
	t.Helper()
	if err := f.store.CreateUser(name, "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	id, err := f.store.GetUserID(name)
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}
	if groupID > 0 {
		if err := f.store.AddUserToGroup(groupID, id); err != nil {
			t.Fatalf("AddUserToGroup failed: %v", err)
		}
	}
	return id
}

// assertGrantRefused fails unless the response is the grant refusal.
func assertGrantRefused(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), grantOutOfTokenScope) {
		t.Errorf("Expected the grant scope refusal, got %s", rec.Body.String())
	}
}

func TestGroupConnectionGrantRespectsTokenScope(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	groupID := f.group(t, "grantees", nil)
	path := fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/connections", groupID)
	body := func(conn int, level string) string {
		return fmt.Sprintf(`{"connection_id":%d,"access_level":%q}`, conn, level)
	}

	refused := []struct {
		caller scopeCaller
		conn   int
		level  string
	}{
		{f.narrowed, 6, auth.AccessLevelRead},
		{f.narrowed, auth.ConnectionIDAll, auth.AccessLevelRead},
		{f.readOnly, 5, auth.AccessLevelReadWrite},
	}
	for _, tc := range refused {
		assertGrantRefused(t, f.do(tc.caller, http.MethodPost, path,
			body(tc.conn, tc.level)))
	}
	if privs, err := f.store.ListGroupConnectionPrivileges(groupID); err != nil ||
		len(privs) != 0 {
		t.Fatalf("Expected no grant to be written, got %v (%v)", privs, err)
	}

	assertStatus(t, f.do(f.narrowed, http.MethodPost, path,
		body(5, auth.AccessLevelReadWrite)), http.StatusNoContent)
	assertStatus(t, f.do(f.readOnly, http.MethodPost, path,
		body(5, auth.AccessLevelRead)), http.StatusNoContent)
	for _, caller := range f.open() {
		assertStatus(t, f.do(caller, http.MethodPost, path,
			body(auth.ConnectionIDAll, auth.AccessLevelReadWrite)),
			http.StatusNoContent)
	}
}

func TestGroupConnectionRevokeRespectsTokenScope(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	groupID := f.group(t, "revokees", map[int]string{
		5: auth.AccessLevelRead,
		6: auth.AccessLevelRead,
	})
	path := func(conn int) string {
		return fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/connections/%d",
			groupID, conn)
	}

	assertGrantRefused(t, f.do(f.narrowed, http.MethodDelete, path(6), ""))
	assertGrantRefused(t, f.do(f.readOnly, http.MethodDelete, path(5), ""))
	assertStatus(t, f.do(f.narrowed, http.MethodDelete, path(5), ""),
		http.StatusNoContent)
	assertStatus(t, f.do(f.session, http.MethodDelete, path(6), ""),
		http.StatusNoContent)
}

func TestGroupAdminPermissionGrantRespectsTokenScope(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	groupID := f.group(t, "admins", nil)
	path := fmt.Sprintf("/api/v1/rbac/groups/%d/permissions", groupID)
	body := `{"permission":"manage_blackouts"}`

	assertGrantRefused(t, f.do(f.narrowed, http.MethodPost, path, body))
	if perms, err := f.store.ListGroupAdminPermissions(groupID); err != nil ||
		len(perms) != 0 {
		t.Fatalf("Expected no permission to be written, got %v (%v)", perms, err)
	}
	for _, caller := range f.open() {
		assertStatus(t, f.do(caller, http.MethodPost, path, body),
			http.StatusNoContent)
	}
}

func TestGroupMembershipRespectsTokenScope(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()

	inScope := f.group(t, "in-scope", map[int]string{5: auth.AccessLevelReadWrite})
	beyond := f.group(t, "beyond", map[int]string{6: auth.AccessLevelRead})
	admins := f.group(t, "admins", nil)
	if err := f.store.GrantAdminPermission(admins, auth.PermManageProbes); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}
	// A group inherits what its parents hold, so joining the child of a
	// group beyond the scope reaches beyond it too.
	child := f.group(t, "child-of-beyond", nil)
	if err := f.store.AddGroupToGroup(beyond, child); err != nil {
		t.Fatalf("AddGroupToGroup failed: %v", err)
	}

	userID := f.user(t, "joiner", 0)
	nested := f.group(t, "nested", nil)
	members := func(groupID int64) string {
		return fmt.Sprintf("/api/v1/rbac/groups/%d/members", groupID)
	}
	userBody := fmt.Sprintf(`{"user_id":%d}`, userID)
	groupBody := fmt.Sprintf(`{"group_id":%d}`, nested)

	for _, groupID := range []int64{beyond, admins, child} {
		assertGrantRefused(t, f.do(f.narrowed, http.MethodPost, members(groupID), userBody))
		assertGrantRefused(t, f.do(f.narrowed, http.MethodPost, members(groupID), groupBody))
	}
	assertGrantRefused(t, f.do(f.readOnly, http.MethodPost, members(inScope), userBody))
	if groups, err := f.store.GetUserGroups(userID); err != nil || len(groups) != 0 {
		t.Fatalf("Expected no membership to be written, got %v (%v)", groups, err)
	}

	assertStatus(t, f.do(f.narrowed, http.MethodPost, members(inScope), userBody),
		http.StatusNoContent)
	assertStatus(t, f.do(f.narrowed, http.MethodPost, members(inScope), groupBody),
		http.StatusNoContent)
	for i, caller := range f.open() {
		id := f.user(t, fmt.Sprintf("open-joiner-%d", i), 0)
		assertStatus(t, f.do(caller, http.MethodPost, members(admins),
			fmt.Sprintf(`{"user_id":%d}`, id)), http.StatusNoContent)
	}
}

func TestGroupDeleteRespectsTokenScope(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()

	beyond := f.group(t, "beyond", map[int]string{6: auth.AccessLevelRead})
	inScope := f.group(t, "in-scope", map[int]string{5: auth.AccessLevelRead})
	path := func(id int64) string {
		return fmt.Sprintf("/api/v1/rbac/groups/%d", id)
	}

	assertGrantRefused(t, f.do(f.narrowed, http.MethodDelete, path(beyond), ""))
	if g, err := f.store.GetGroup(beyond); err != nil || g == nil {
		t.Fatalf("Expected the group to survive the refusal (%v)", err)
	}
	assertStatus(t, f.do(f.narrowed, http.MethodDelete, path(inScope), ""),
		http.StatusNoContent)
	assertStatus(t, f.do(f.unscoped, http.MethodDelete, path(beyond), ""),
		http.StatusNoContent)
}

func TestCreateSuperuserRespectsTokenScope(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	body := func(name string, superuser bool) string {
		return fmt.Sprintf(`{"username":%q,"password":"Password1234!x","is_superuser":%t}`,
			name, superuser)
	}

	assertGrantRefused(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/users",
		body("would-be-root", true)))
	if u, _ := f.store.GetUser("would-be-root"); u != nil {
		t.Fatal("Expected no user to be created on refusal")
	}

	assertStatus(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/users",
		body("plain-user", false)), http.StatusCreated)
	for i, caller := range f.open() {
		assertStatus(t, f.do(caller, http.MethodPost, "/api/v1/rbac/users",
			body(fmt.Sprintf("root-%d", i), true)), http.StatusCreated)
	}
}

func TestUpdateUserRespectsTokenScope(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()

	inScope := f.user(t, "in-scope-user",
		f.group(t, "in-scope", map[int]string{5: auth.AccessLevelRead}))
	beyond := f.user(t, "beyond-user",
		f.group(t, "beyond", map[int]string{6: auth.AccessLevelRead}))
	path := func(id int64) string {
		return fmt.Sprintf("/api/v1/rbac/users/%d", id)
	}

	for _, body := range []string{
		`{"password":"Another-Password-9"}`,
		`{"enabled":true}`,
	} {
		assertGrantRefused(t, f.do(f.narrowed, http.MethodPut, path(beyond), body))
		assertStatus(t, f.do(f.narrowed, http.MethodPut, path(inScope), body),
			http.StatusOK)
		assertStatus(t, f.do(f.session, http.MethodPut, path(beyond), body),
			http.StatusOK)
	}

	// Superuser status reaches every connection, whoever the target is.
	assertGrantRefused(t, f.do(f.narrowed, http.MethodPut, path(inScope),
		`{"is_superuser":true}`))
	if u, err := f.store.GetUserByID(inScope); err != nil || u.IsSuperuser {
		t.Fatalf("Expected the user to stay a non-superuser (%v)", err)
	}
	assertStatus(t, f.do(f.unscoped, http.MethodPut, path(inScope),
		`{"is_superuser":true}`), http.StatusOK)

	// Changes that hand over no access are unaffected.
	assertStatus(t, f.do(f.narrowed, http.MethodPut, path(beyond),
		`{"display_name":"Renamed","enabled":false}`), http.StatusOK)
}

func TestCreateTokenRespectsTokenScope(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()

	f.user(t, "in-scope-owner",
		f.group(t, "in-scope", map[int]string{5: auth.AccessLevelReadWrite}))
	f.user(t, "beyond-owner",
		f.group(t, "beyond", map[int]string{6: auth.AccessLevelRead}))
	f.user(t, "root-owner", 0)
	if err := f.store.SetUserSuperuser("root-owner", true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	body := func(owner string) string {
		return fmt.Sprintf(`{"owner_username":%q}`, owner)
	}

	for _, owner := range []string{"beyond-owner", "no-such-owner", "root-owner"} {
		assertGrantRefused(t, f.do(f.narrowed, http.MethodPost,
			"/api/v1/rbac/tokens", body(owner)))
	}
	assertStatus(t, f.do(f.narrowed, http.MethodPost, "/api/v1/rbac/tokens",
		body("in-scope-owner")), http.StatusCreated)
	for _, caller := range f.open() {
		assertStatus(t, f.do(caller, http.MethodPost, "/api/v1/rbac/tokens",
			body("beyond-owner")), http.StatusCreated)
	}
}

func TestTokenScopeChangesRespectTokenScope(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()

	// target is owned by a user reaching beyond the narrowed scope;
	// bounded already carries a connection scope inside it.
	f.user(t, "beyond-owner",
		f.group(t, "beyond", map[int]string{6: auth.AccessLevelRead}))
	_, target, err := f.store.CreateToken("beyond-owner", "target", nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	_, bounded, err := f.store.CreateToken("beyond-owner", "bounded", nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	if err := f.store.SetTokenConnectionScope(bounded.ID, []auth.ScopedConnection{
		{ConnectionID: 5, AccessLevel: auth.AccessLevelRead},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	scopePath := func(id int64) string {
		return fmt.Sprintf("/api/v1/rbac/tokens/%d/scope", id)
	}

	refused := []struct {
		id   int64
		body string
	}{
		{target.ID, `{"connections":[{"connection_id":6,"access_level":"read"}]}`},
		{target.ID, `{"connections":[{"connection_id":0,"access_level":"read"}]}`},
		// Leaving the connections alone keeps target unrestricted, so
		// its owner's reach decides.
		{target.ID, `{"admin_permissions":["manage_blackouts"]}`},
	}
	for _, tc := range refused {
		assertGrantRefused(t, f.do(f.narrowed, http.MethodPut, scopePath(tc.id), tc.body))
	}
	if scope, err := f.store.GetTokenScope(target.ID); err != nil || scope != nil {
		t.Fatalf("Expected target to stay unscoped, got %+v (%v)", scope, err)
	}
	assertGrantRefused(t, f.do(f.narrowed, http.MethodDelete, scopePath(bounded.ID), ""))
	assertGrantRefused(t, f.do(f.readOnly, http.MethodPut, scopePath(target.ID),
		`{"connections":[{"connection_id":5,"access_level":"read_write"}]}`))

	assertStatus(t, f.do(f.narrowed, http.MethodPut, scopePath(bounded.ID),
		`{"admin_permissions":["manage_blackouts"]}`), http.StatusNoContent)
	assertStatus(t, f.do(f.narrowed, http.MethodPut, scopePath(target.ID),
		`{"connections":[{"connection_id":5,"access_level":"read_write"}]}`),
		http.StatusNoContent)
	for _, caller := range f.open() {
		assertStatus(t, f.do(caller, http.MethodDelete, scopePath(bounded.ID), ""),
			http.StatusNoContent)
	}
}

// TestTokenScopeGrantChecksFailClosed checks the token-scope gates
// refuse, or report a failure, when the store cannot say what the
// target token or its owner holds.
func TestTokenScopeGrantChecksFailClosed(t *testing.T) {
	t.Run("unknown target token", func(t *testing.T) {
		f, cleanup := newGrantFixture(t)
		defer cleanup()
		assertGrantRefused(t, f.do(f.narrowed, http.MethodDelete,
			"/api/v1/rbac/tokens/99999/scope", ""))
	})

	t.Run("stored scope unreadable", func(t *testing.T) {
		h, store, dir, cleanup := createTestRBACHandlerWithDir(t)
		defer cleanup()
		_, _, _, narrowed, _ := scopedCallers(t, store)
		target := mustCreateScopedToken(t, store, "svc-target", nil)
		dropAuthTable(t, dir, "token_connection_scope")

		req := narrowed.wrap(httptest.NewRequest(http.MethodPut,
			fmt.Sprintf("/api/v1/rbac/tokens/%d/scope", target),
			strings.NewReader(`{"admin_permissions":["manage_blackouts"]}`)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.handleTokenSubpath(rec, req)
		assertStatus(t, rec, http.StatusInternalServerError)
		if !strings.Contains(rec.Body.String(), "Failed to get token scope") {
			t.Errorf("Unexpected body: %s", rec.Body.String())
		}
	})
}
