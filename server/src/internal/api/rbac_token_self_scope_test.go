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
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// A token may narrow, but not widen, the scope that bounds it
//
// manage_token_scopes is a real permission a token may legitimately
// hold. A scope change it makes, to its own scope or another token's,
// may grant only what lies within its own access (issue #471, ruling of
// 6 October 2026), so a token cannot widen or clear its own scope, but
// may narrow it.
// =============================================================================

// scopeRequest issues a scope change with the given body against the
// given token id, as the given acting token.
func scopeRequest(h *RBACHandler, method string, targetID,
	actingID int64, body string) *httptest.ResponseRecorder {

	req := httptest.NewRequest(method,
		"/api/v1/rbac/tokens/"+strconv.FormatInt(targetID, 10)+"/scope",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuserToken(req, actingID)
	rec := httptest.NewRecorder()
	h.handleTokenSubpath(rec, req)
	return rec
}

// mustSuperuserScopedToken is mustCreateScopedToken for a superuser
// owner, as the superuser token context the requests carry implies.
func mustSuperuserScopedToken(t *testing.T, store *auth.AuthStore,
	username string, scope []string) int64 {

	t.Helper()
	id := mustCreateScopedToken(t, store, username, scope)
	if err := store.SetUserSuperuser(username, true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	return id
}

// adminScopeOf returns a token's stored admin scope.
func adminScopeOf(t *testing.T, store *auth.AuthStore, id int64) []string {
	t.Helper()
	scope, err := store.GetTokenScope(id)
	if err != nil {
		t.Fatalf("GetTokenScope failed: %v", err)
	}
	if scope == nil {
		return nil
	}
	return scope.AdminPermissions
}

// TestSetTokenScopeSelfTarget checks that a token scoped to
// manage_token_scopes cannot widen its own scope but may narrow it.
func TestSetTokenScopeSelfTarget(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	acting := mustSuperuserScopedToken(t, store, "svc-scope-manager",
		[]string{auth.PermManageTokenScopes, auth.PermManageUsers})

	rec := scopeRequest(handler, http.MethodPut, acting, acting,
		`{"admin_permissions":["*"]}`)
	assertRefusedWith(t, rec, refuseTokenScope)
	rec = scopeRequest(handler, http.MethodPut, acting, acting,
		`{"admin_permissions":["manage_token_scopes","manage_probes"]}`)
	assertRefusedWith(t, rec, refuseTokenScope)
	if got := adminScopeOf(t, store, acting); len(got) != 2 {
		t.Fatalf("Expected the acting token's scope to be untouched, got %v", got)
	}

	rec = scopeRequest(handler, http.MethodPut, acting, acting,
		`{"admin_permissions":["manage_token_scopes"]}`)
	assertStatus(t, rec, http.StatusNoContent)
	if got := adminScopeOf(t, store, acting); len(got) != 1 ||
		got[0] != auth.PermManageTokenScopes {
		t.Errorf("Expected the scope to be narrowed, got %v", got)
	}
}

// TestSetTokenScopeOtherTarget checks that the same rule governs
// another token's scope: what the acting token holds may be granted,
// what it does not may not. The other token's owner is not a superuser;
// a superuser's token is for a superuser alone to change (see
// TestSuperuserOwnedTokenNeedsSuperuser).
func TestSetTokenScopeOtherTarget(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	acting := mustSuperuserScopedToken(t, store, "svc-scope-manager",
		[]string{auth.PermManageTokenScopes})
	other := mustCreateScopedToken(t, store, "svc-other",
		[]string{auth.PermManageUsers})

	assertRefusedWith(t, scopeRequest(handler, http.MethodPut, other, acting,
		`{"admin_permissions":["*"]}`), refuseTokenScope)
	assertStatus(t, scopeRequest(handler, http.MethodPut, other, acting,
		`{"admin_permissions":["manage_users","manage_token_scopes"]}`),
		http.StatusNoContent)
}

// TestClearTokenScopeSelfTarget covers the DELETE route, which would
// hand the token its superuser owner's whole access.
func TestClearTokenScopeSelfTarget(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	acting := mustSuperuserScopedToken(t, store, "svc-scope-clearer",
		[]string{auth.PermManageTokenScopes})

	assertRefusedWith(t, scopeRequest(handler, http.MethodDelete, acting,
		acting, ""), refuseTokenScope)
	if got := adminScopeOf(t, store, acting); len(got) != 1 {
		t.Errorf("Expected the scope to survive the refusal, got %v", got)
	}

	// The refusal is audited like every other RBAC denial.
	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Outcome: string(auth.OutcomeDenied),
	})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) == 0 {
		t.Error("Expected the refusal to be recorded")
	}
}

// TestSessionMayClearAnyTokenScope checks that a session caller, which
// has no acting token, is not caught by the guard.
func TestSessionMayClearAnyTokenScope(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	target := mustCreateScopedToken(t, store, "svc-session-target",
		[]string{auth.PermManageUsers})

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/tokens/"+strconv.FormatInt(target, 10)+"/scope", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()
	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}
