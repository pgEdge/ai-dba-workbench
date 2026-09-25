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
// A token may not rewrite the scope that bounds it
//
// manage_token_scopes is a real permission a token may legitimately
// hold, but a token holding it could call the scope endpoint on its own
// id and widen or clear its own scope, which would undo every other
// scope gate. The guard refuses only that self-reference; managing
// another token's scope is unaffected.
// =============================================================================

// scopeRequest issues a scope change against the given token id, as the
// given acting token.
func scopeRequest(h *RBACHandler, method string, targetID,
	actingID int64) *httptest.ResponseRecorder {

	body := strings.NewReader(`{"admin_permissions":["*"]}`)
	req := httptest.NewRequest(method,
		"/api/v1/rbac/tokens/"+strconv.FormatInt(targetID, 10)+"/scope", body)
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuserToken(req, actingID)
	rec := httptest.NewRecorder()
	h.handleTokenSubpath(rec, req)
	return rec
}

// TestSetTokenScopeRefusesSelfTarget checks that a token scoped to
// manage_token_scopes cannot widen its own scope, whilst the same
// token may still set another token's scope.
func TestSetTokenScopeRefusesSelfTarget(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	acting := mustCreateScopedToken(t, store, "svc-scope-manager",
		[]string{auth.PermManageTokenScopes})
	other := mustCreateScopedToken(t, store, "svc-other",
		[]string{auth.PermManageUsers})

	rec := scopeRequest(handler, http.MethodPut, acting, acting)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 for a self-targeted scope change, got %d: %s",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "its own scope") {
		t.Errorf("Expected the refusal to say why, got %s", rec.Body.String())
	}

	// The scope must be unchanged, so the token still cannot pass a
	// blanket superuser gate.
	scope, err := store.GetTokenScope(acting)
	if err != nil {
		t.Fatalf("GetTokenScope failed: %v", err)
	}
	if scope == nil || len(scope.AdminPermissions) != 1 ||
		scope.AdminPermissions[0] != auth.PermManageTokenScopes {
		t.Errorf("Expected the acting token's scope to be untouched, got %+v",
			scope)
	}

	if rec := scopeRequest(handler, http.MethodPut, other, acting); rec.Code != http.StatusNoContent {
		t.Errorf("Expected another token's scope to remain manageable, got %d: %s",
			rec.Code, rec.Body.String())
	}
}

// TestClearTokenScopeRefusesSelfTarget covers the DELETE route, which
// would otherwise remove the scope altogether.
func TestClearTokenScopeRefusesSelfTarget(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	acting := mustCreateScopedToken(t, store, "svc-scope-clearer",
		[]string{auth.PermManageTokenScopes})

	rec := scopeRequest(handler, http.MethodDelete, acting, acting)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 for a self-targeted clear, got %d: %s", rec.Code,
			rec.Body.String())
	}

	scope, err := store.GetTokenScope(acting)
	if err != nil {
		t.Fatalf("GetTokenScope failed: %v", err)
	}
	if scope == nil || len(scope.AdminPermissions) != 1 {
		t.Errorf("Expected the scope to survive the refusal, got %+v", scope)
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
