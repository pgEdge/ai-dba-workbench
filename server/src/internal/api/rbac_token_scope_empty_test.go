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
// An empty array on the scope PUT (issue #471 review)
//
// Each scope kind is stored as rows in its own table, and a kind with no
// rows is unrestricted. Writing an empty array for a kind therefore
// deleted its rows and lifted the restriction, so a request that read as
// "this token may use no admin permissions" did the opposite. The PUT now
// refuses an empty array for every kind; DELETE /scope is the way to lift
// a token's scope.
// =============================================================================

// seedEmptyArrayScopeTarget creates a token restricted in all three
// scope kinds and returns its id.
func seedEmptyArrayScopeTarget(t *testing.T, store *auth.AuthStore) int64 {
	t.Helper()

	if _, err := store.RegisterMCPPrivilege("known_tool",
		auth.MCPPrivilegeTypeTool, "known", false); err != nil {
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}
	target := mustCreateScopedToken(t, store, "svc-empty-array",
		[]string{auth.PermManageUsers})
	if err := store.SetTokenMCPScopeByNames(target,
		[]string{"known_tool"}); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
	}
	if err := store.SetTokenConnectionScope(target, []auth.ScopedConnection{
		{ConnectionID: 5, AccessLevel: auth.AccessLevelRead},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	return target
}

// putTokenScope issues PUT /api/v1/rbac/tokens/{id}/scope as a
// superuser session with the given JSON body.
func putTokenScope(t *testing.T, handler *RBACHandler, tokenID int64,
	body string) *httptest.ResponseRecorder {

	t.Helper()
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/tokens/"+strconv.FormatInt(tokenID, 10)+"/scope",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()
	handler.handleTokenSubpath(rec, req)
	return rec
}

// assertEmptyArrayScopeUntouched checks that the token seeded by
// seedEmptyArrayScopeTarget still carries all three restrictions.
func assertEmptyArrayScopeUntouched(t *testing.T, store *auth.AuthStore,
	target int64) {

	t.Helper()
	scope, err := store.GetTokenScope(target)
	if err != nil {
		t.Fatalf("GetTokenScope failed: %v", err)
	}
	if scope == nil {
		t.Fatal("Expected the token to remain scoped, got no scope")
	}
	if len(scope.AdminPermissions) != 1 ||
		scope.AdminPermissions[0] != auth.PermManageUsers {
		t.Errorf("Expected the admin scope untouched, got %v",
			scope.AdminPermissions)
	}
	if len(scope.MCPPrivileges) != 1 {
		t.Errorf("Expected the MCP scope untouched, got %v",
			scope.MCPPrivileges)
	}
	if len(scope.Connections) != 1 || scope.Connections[0].ConnectionID != 5 {
		t.Errorf("Expected the connection scope untouched, got %+v",
			scope.Connections)
	}
}

// TestSetTokenScopeRejectsEmptyArray checks that an empty array for any
// of the three scope kinds is refused with 400 naming the kind, and that
// nothing is written, including valid kinds in the same request.
func TestSetTokenScopeRejectsEmptyArray(t *testing.T) {
	cases := []struct {
		name string
		body string
		kind string
	}{
		{"admin permissions", `{"admin_permissions":[]}`, "admin_permissions"},
		{"MCP privileges", `{"mcp_privileges":[]}`, "mcp_privileges"},
		{"connections", `{"connections":[]}`, "connections"},
		{
			"empty kind alongside valid kinds",
			`{"connections":[{"connection_id":7,"access_level":"read"}],` +
				`"mcp_privileges":["*"],"admin_permissions":[]}`,
			"admin_permissions",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler, store, cleanup := createTestRBACHandler(t)
			defer cleanup()
			target := seedEmptyArrayScopeTarget(t, store)

			rec := putTokenScope(t, handler, target, tc.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("Expected 400 for an empty %s array, got %d: %s",
					tc.kind, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.kind) {
				t.Errorf("Expected the refusal to name %s, got %s",
					tc.kind, rec.Body.String())
			}
			assertEmptyArrayScopeUntouched(t, store, target)
		})
	}
}

// TestSetTokenScopeTreatsNullAsOmitted checks that null for a kind, as
// the web client may send for a category it leaves alone, is the same
// as leaving the key out: that kind is untouched and the others are
// written.
func TestSetTokenScopeTreatsNullAsOmitted(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	target := seedEmptyArrayScopeTarget(t, store)

	rec := putTokenScope(t, handler, target,
		`{"connections":null,"mcp_privileges":null,`+
			`"admin_permissions":["`+auth.PermManageGroups+`"]}`)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	scope, err := store.GetTokenScope(target)
	if err != nil {
		t.Fatalf("GetTokenScope failed: %v", err)
	}
	if scope == nil || len(scope.AdminPermissions) != 1 ||
		scope.AdminPermissions[0] != auth.PermManageGroups {
		t.Fatalf("Expected the admin scope replaced, got %+v", scope)
	}
	if len(scope.MCPPrivileges) != 1 || len(scope.Connections) != 1 {
		t.Errorf("Expected the null kinds untouched, got %+v", scope)
	}
}
