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
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Regression coverage for GitHub issue #471
//
// A superuser's API token used to carry its owner's superuser status
// into every gate, so a token minted for one narrow job could reach
// every superuser-only endpoint. The owner's rights are now
// intersected with the token's admin scope, and these tests pin the
// effect at the HTTP boundary for both gate shapes: requireSuperuser,
// which names no permission and so refuses a narrowed token outright,
// and requirePermission, which names one and so admits it when the
// scope names it too.
// =============================================================================

// groupPermissionsRequest issues a GET against a group's permissions,
// which is gated by requireSuperuser, as the given superuser token.
func groupPermissionsRequest(h *RBACHandler, tokenID int64) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/groups/1/permissions", nil)
	if tokenID > 0 {
		req = withSuperuserToken(req, tokenID)
	} else {
		req = withSuperuser(req)
	}
	rec := httptest.NewRecorder()
	h.handleGroupPermissions(rec, req, 1, nil)
	return rec
}

// TestRequireSuperuserRejectsScopedSuperuserToken checks that the
// superuser gate on a non-audit endpoint refuses a token whose admin
// scope has been narrowed, and still admits an unscoped token, a
// wildcard token and a session.
func TestRequireSuperuserRejectsScopedSuperuserToken(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	narrowed := mustCreateScopedToken(t, store, "svc-471-narrow",
		[]string{auth.PermManageGroups})
	unscoped := mustCreateScopedToken(t, store, "svc-471-open", nil)
	wildcard := mustCreateScopedToken(t, store, "svc-471-wild",
		[]string{auth.AdminPermissionWildcard})

	if rec := groupPermissionsRequest(handler, narrowed); rec.Code != http.StatusForbidden {
		t.Errorf("Narrowed token: expected 403, got %d: %s", rec.Code,
			rec.Body.String())
	}
	for name, tokenID := range map[string]int64{
		"unscoped token": unscoped,
		"wildcard token": wildcard,
		"session":        0,
	} {
		if rec := groupPermissionsRequest(handler, tokenID); rec.Code == http.StatusForbidden {
			t.Errorf("%s: expected the request to pass the superuser gate, got %s",
				name, rec.Body.String())
		}
	}
}

// TestRequirePermissionHonoursScopedSuperuserToken checks the
// finer-grained gate on GET /api/v1/rbac/users, which requires
// manage_users. Each token's owner is a service account with no group
// grant of its own, so whatever passes here comes from the owner's
// superuser status bounded by the token's admin scope: the token
// scoped to manage_users may list users, the one scoped to
// manage_groups may not, and an unscoped token is unrestricted.
func TestRequirePermissionHonoursScopedSuperuserToken(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	matching := mustCreateScopedToken(t, store, "svc-471-users",
		[]string{auth.PermManageUsers})
	other := mustCreateScopedToken(t, store, "svc-471-groups",
		[]string{auth.PermManageGroups})
	unscoped := mustCreateScopedToken(t, store, "svc-471-users-open", nil)

	listUsers := func(tokenID int64) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
		req = withSuperuserToken(req, tokenID)
		rec := httptest.NewRecorder()
		handler.handleUsers(rec, req)
		return rec
	}

	if rec := listUsers(other); rec.Code != http.StatusForbidden {
		t.Errorf("Token scoped elsewhere: expected 403, got %d: %s", rec.Code,
			rec.Body.String())
	}
	for name, tokenID := range map[string]int64{
		"token scoped to manage_users": matching,
		"unscoped token":               unscoped,
	} {
		if rec := listUsers(tokenID); rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d: %s", name, rec.Code,
				rec.Body.String())
		}
	}
}
