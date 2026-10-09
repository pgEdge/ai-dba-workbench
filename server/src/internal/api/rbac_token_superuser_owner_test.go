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
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// refuseNotSuperuser is requireSuperuser's refusal.
const refuseNotSuperuser = "Permission denied: requires superuser privileges"

// tokenRouteRequest issues a request against the token routes, with the
// caller set by as.
func tokenRouteRequest(h *RBACHandler, method, path, body string,
	as func(*http.Request) *http.Request) *httptest.ResponseRecorder {

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req = as(req)
	rec := httptest.NewRecorder()
	h.handleTokenSubpath(rec, req)
	return rec
}

// renameUsersTable makes every user lookup fail, whilst token lookups,
// which read the tokens table alone, still succeed.
func renameUsersTable(t *testing.T, dataDir string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "auth.db"))
	if err != nil {
		t.Fatalf("Failed to open the auth database: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("ALTER TABLE users RENAME TO users_gone"); err != nil {
		t.Fatalf("Failed to rename the users table: %v", err)
	}
}

// mustCreateSuperuser creates a superuser account.
func mustCreateSuperuser(t *testing.T, store *auth.AuthStore, username string) {
	t.Helper()
	if err := store.CreateUser(username, "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := store.SetUserSuperuser(username, true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
}

// scopeAdminCallers returns a session and a token, neither a
// superuser's, that both hold manage_token_scopes.
func scopeAdminCallers(t *testing.T, store *auth.AuthStore) map[string]func(*http.Request) *http.Request {
	t.Helper()
	userID := setupUserWithPermission(t, store, "scope-admin",
		auth.PermManageTokenScopes)
	_, token, err := store.CreateToken("scope-admin", "scope admin", nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	return map[string]func(*http.Request) *http.Request{
		"session": func(r *http.Request) *http.Request { return withUser(r, userID) },
		"token": func(r *http.Request) *http.Request {
			return withToken(r, userID, token.ID)
		},
	}
}

// TestCreateTokenForSuperuserNeedsSuperuser checks that only a
// superuser may mint a superuser's token: a session holding
// manage_token_scopes is not bounded by a token ceiling, so it could
// otherwise mint itself an unscoped superuser token.
func TestCreateTokenForSuperuserNeedsSuperuser(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	mustCreateSuperuser(t, store, "root")
	if err := store.CreateUser("alice", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	callers := scopeAdminCallers(t, store)

	for name, as := range callers {
		t.Run(name, func(t *testing.T) {
			rec := tokenRouteRequest(handler, http.MethodPost,
				"/api/v1/rbac/tokens/", `{"owner_username":"root"}`, as)
			assertRefusedWith(t, rec, refuseNotSuperuser)
		})
	}
	tokens, err := store.ListUserTokens("root")
	if err != nil {
		t.Fatalf("ListUserTokens failed: %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("Expected no token for root, got %d", len(tokens))
	}

	assertStatus(t, tokenRouteRequest(handler, http.MethodPost,
		"/api/v1/rbac/tokens/", `{"owner_username":"alice"}`,
		callers["session"]), http.StatusCreated)
	assertStatus(t, tokenRouteRequest(handler, http.MethodPost,
		"/api/v1/rbac/tokens/", `{"owner_username":"root"}`,
		withSuperuserSession), http.StatusCreated)
}

// TestCreateTokenOwnerLookupFails checks that a failing owner lookup is
// refused rather than taken to mean the owner is not a superuser.
func TestCreateTokenOwnerLookupFails(t *testing.T) {
	handler, store, dir, cleanup := createTestRBACHandlerWithDir(t)
	defer cleanup()
	mustCreateSuperuser(t, store, "root")
	renameUsersTable(t, dir)

	rec := tokenRouteRequest(handler, http.MethodPost, "/api/v1/rbac/tokens/",
		`{"owner_username":"root"}`, withSuperuserSession)
	assertError(t, rec, http.StatusInternalServerError, "Failed to create token")
}

// TestSuperuserOwnedTokenNeedsSuperuser checks that only a superuser may
// change the scope of, clear, or delete a superuser's token, whether the
// caller is a session or a token, and that a superuser still may.
func TestSuperuserOwnedTokenNeedsSuperuser(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	target := mustSuperuserScopedToken(t, store, "svc-root",
		[]string{auth.PermManageUsers})
	tokenPath := "/api/v1/rbac/tokens/" + strconv.FormatInt(target, 10)

	requests := []struct {
		name, method, path, body string
		success                  int
	}{
		{"narrow scope", http.MethodPut, tokenPath + "/scope",
			`{"admin_permissions":["manage_users"]}`, http.StatusNoContent},
		{"widen scope", http.MethodPut, tokenPath + "/scope",
			`{"admin_permissions":["*"]}`, http.StatusNoContent},
		{"clear scope", http.MethodDelete, tokenPath + "/scope", "",
			http.StatusNoContent},
		{"delete token", http.MethodDelete, tokenPath, "", http.StatusNoContent},
	}
	for name, as := range scopeAdminCallers(t, store) {
		for _, rq := range requests {
			t.Run(name+" "+rq.name, func(t *testing.T) {
				rec := tokenRouteRequest(handler, rq.method, rq.path, rq.body, as)
				assertRefusedWith(t, rec, refuseNotSuperuser)
			})
		}
	}
	if got := adminScopeOf(t, store, target); len(got) != 1 ||
		got[0] != auth.PermManageUsers {
		t.Fatalf("Expected the target's scope to be untouched, got %v", got)
	}

	for _, rq := range requests {
		assertStatus(t, tokenRouteRequest(handler, rq.method, rq.path, rq.body,
			withSuperuserSession), rq.success)
	}
	token, err := store.GetTokenByID(target)
	if err != nil || token != nil {
		t.Errorf("Expected the superuser to delete the token, got %v, %v",
			token, err)
	}
}

// TestNonSuperuserOwnedTokenSessionEdit checks that the superuser gate
// leaves a token whose owner is not a superuser to the usual rules.
func TestNonSuperuserOwnedTokenSessionEdit(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	target := mustCreateScopedToken(t, store, "svc-plain",
		[]string{auth.PermManageUsers})
	as := scopeAdminCallers(t, store)["session"]
	tokenPath := "/api/v1/rbac/tokens/" + strconv.FormatInt(target, 10)

	assertStatus(t, tokenRouteRequest(handler, http.MethodPut,
		tokenPath+"/scope", `{"admin_permissions":["*"]}`, as),
		http.StatusNoContent)
	assertStatus(t, tokenRouteRequest(handler, http.MethodDelete,
		tokenPath, "", as), http.StatusNoContent)
	// A token that does not exist is left to the operation to report.
	rec := tokenRouteRequest(handler, http.MethodDelete, tokenPath, "", as)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusNoContent {
		t.Errorf("Expected the delete of a missing token to fail, got %d",
			rec.Code)
	}
}

// TestSuperuserOwnedTokenSelfDelete checks that a superuser's narrowed
// token may still delete itself, as it may narrow itself.
func TestSuperuserOwnedTokenSelfDelete(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	self := mustSuperuserScopedToken(t, store, "svc-self",
		[]string{auth.PermManageTokenScopes})

	rec := tokenRouteRequest(handler, http.MethodDelete,
		"/api/v1/rbac/tokens/"+strconv.FormatInt(self, 10), "",
		func(r *http.Request) *http.Request { return withSuperuserToken(r, self) })
	assertStatus(t, rec, http.StatusNoContent)
}

// TestSuperuserOwnedTokenLookupFails checks that the gate fails closed
// when the token or its owner cannot be read.
func TestSuperuserOwnedTokenLookupFails(t *testing.T) {
	for _, broken := range []string{"tokens", "users"} {
		t.Run(broken, func(t *testing.T) {
			handler, store, dir, cleanup := createTestRBACHandlerWithDir(t)
			defer cleanup()
			target := mustSuperuserScopedToken(t, store, "svc-root",
				[]string{auth.PermManageUsers})
			if broken == "users" {
				renameUsersTable(t, dir)
			} else {
				dropAuthTable(t, dir, "tokens")
			}
			rec := tokenRouteRequest(handler, http.MethodDelete,
				"/api/v1/rbac/tokens/"+strconv.FormatInt(target, 10)+"/scope",
				"", withSuperuserSession)
			assertError(t, rec, http.StatusInternalServerError,
				"Failed to get token")
		})
	}
}

// TestSetTokenScopeRejectsMalformedConnectionScope checks that a
// connection scope mixing the "all connections" entry with particular
// connections, or naming one twice, is refused with 400.
func TestSetTokenScopeRejectsMalformedConnectionScope(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	target := mustCreateScopedToken(t, store, "svc-mixed", nil)
	path := "/api/v1/rbac/tokens/" + strconv.FormatInt(target, 10) + "/scope"

	for _, body := range []string{
		`{"connections":[{"connection_id":0,"access_level":"read_write"},` +
			`{"connection_id":5,"access_level":"read"}]}`,
		`{"connections":[{"connection_id":5,"access_level":"read"},` +
			`{"connection_id":5,"access_level":"read_write"}]}`,
	} {
		rec := tokenRouteRequest(handler, http.MethodPut, path, body,
			withSuperuserSession)
		if rec.Code != http.StatusBadRequest ||
			!strings.Contains(rec.Body.String(), "invalid connection scope") {
			t.Errorf("Expected 400 for %s, got %d: %s", body, rec.Code,
				rec.Body.String())
		}
	}
	scope, err := store.GetTokenScope(target)
	if err != nil || scope != nil {
		t.Errorf("Expected the token to stay unscoped, got %+v, %v", scope, err)
	}
}

// TestSetTokenScopeRejectsUnknownAdminPermission checks that an admin
// scope naming a permission that does not exist is refused with 400
// before any scope kind is written.
func TestSetTokenScopeRejectsUnknownAdminPermission(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	if _, err := store.RegisterMCPPrivilege("known_tool",
		auth.MCPPrivilegeTypeTool, "known", false); err != nil {
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}
	target := mustCreateScopedToken(t, store, "svc-admin-typo",
		[]string{auth.PermManageUsers})

	rec := tokenRouteRequest(handler, http.MethodPut,
		"/api/v1/rbac/tokens/"+strconv.FormatInt(target, 10)+"/scope",
		`{"mcp_privileges":["known_tool"],"admin_permissions":["manage_everything"]}`,
		withSuperuserSession)
	if rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "unknown admin permission") ||
		!strings.Contains(rec.Body.String(), "manage_everything") {
		t.Fatalf("Expected 400 naming the permission, got %d: %s", rec.Code,
			rec.Body.String())
	}
	scope, err := store.GetTokenScope(target)
	if err != nil {
		t.Fatalf("GetTokenScope failed: %v", err)
	}
	if scope == nil || len(scope.MCPPrivileges) != 0 ||
		len(scope.AdminPermissions) != 1 {
		t.Errorf("Expected the scope to be untouched, got %+v", scope)
	}
}
