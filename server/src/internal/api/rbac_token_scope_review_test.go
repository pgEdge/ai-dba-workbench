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
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Follow-ups to issue #471 from review
//
// An MCP scope naming an unregistered identifier used to insert nothing
// for that name, so a list of typos stored no scope at all and left the
// token unrestricted. Updating and deleting a connection were gated on
// ownership and manage_connections alone, neither of which says which
// connections a token was issued for, so a token scoped to one
// connection could change or delete another.
// =============================================================================

// TestSetTokenScopeRejectsUnknownMCPPrivilege checks that an MCP scope
// naming an unregistered identifier is refused with 400, and that the
// token's existing scope, of every kind, is left as it was.
func TestSetTokenScopeRejectsUnknownMCPPrivilege(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	if _, err := store.RegisterMCPPrivilege("known_tool",
		auth.MCPPrivilegeTypeTool, "known", false); err != nil {
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}
	target := mustCreateScopedToken(t, store, "svc-mcp-typo",
		[]string{auth.PermManageUsers})
	if err := store.AsActor(auth.SystemActor()).SetTokenMCPScopeByNames(target, []string{"known_tool"}, true); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
	}

	body := strings.NewReader(
		`{"mcp_privileges":["no_such_tool"],"admin_permissions":["*"]}`)
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/tokens/"+strconv.FormatInt(target, 10)+"/scope", body)
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()
	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for an unknown MCP privilege, got %d: %s",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no_such_tool") {
		t.Errorf("Expected the refusal to name the identifier, got %s",
			rec.Body.String())
	}

	scope, err := store.GetTokenScope(target)
	if err != nil {
		t.Fatalf("GetTokenScope failed: %v", err)
	}
	if scope == nil || len(scope.MCPPrivileges) != 1 {
		t.Fatalf("Expected the MCP scope to be untouched, got %+v", scope)
	}
	if len(scope.AdminPermissions) != 1 ||
		scope.AdminPermissions[0] != auth.PermManageUsers {
		t.Errorf("Expected the admin scope to be untouched, got %v",
			scope.AdminPermissions)
	}
}

// TestSetTokenScopeRejectsInvalidAccessLevelBeforeWriting checks that a
// connection entry with an unknown access level is refused with 400
// before any scope kind is written, so a valid MCP scope in the same
// request does not land on its own.
func TestSetTokenScopeRejectsInvalidAccessLevelBeforeWriting(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	for _, name := range []string{"known_tool", "other_tool"} {
		if _, err := store.RegisterMCPPrivilege(name,
			auth.MCPPrivilegeTypeTool, name, false); err != nil {
			t.Fatalf("RegisterMCPPrivilege failed: %v", err)
		}
	}
	target := mustCreateScopedToken(t, store, "svc-bad-level",
		[]string{auth.PermManageUsers})
	if err := store.AsActor(auth.SystemActor()).SetTokenMCPScopeByNames(target, []string{"known_tool"}, true); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
	}

	body := strings.NewReader(`{"mcp_privileges":["other_tool"],` +
		`"connections":[{"connection_id":3,"access_level":"write"}]}`)
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/tokens/"+strconv.FormatInt(target, 10)+"/scope", body)
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()
	handler.handleTokenSubpath(rec, req)

	if rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "invalid access level") {
		t.Fatalf("Expected 400 for an invalid access level, got %d: %s",
			rec.Code, rec.Body.String())
	}

	names, err := store.GetTokenMCPScope(target)
	if err != nil {
		t.Fatalf("GetTokenMCPScope failed: %v", err)
	}
	if len(names) != 1 || names[0] != "known_tool" {
		t.Errorf("Expected the MCP scope to be untouched, got %v", names)
	}
}

// connectionMutationRequest issues a PUT or DELETE against a connection
// as the given API token, authenticated as the REST auth wrapper does.
func connectionMutationRequest(t *testing.T, h *ConnectionHandler,
	store *auth.AuthStore, method, rawToken string, connID int) *httptest.ResponseRecorder {

	t.Helper()
	req := httptest.NewRequest(method,
		"/api/v1/connections/"+strconv.Itoa(connID),
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req = asWrapped(t, store, withBearer(req, rawToken))
	rec := httptest.NewRecorder()
	if method == http.MethodDelete {
		h.deleteConnection(rec, req, connID)
	} else {
		h.updateConnection(rec, req, connID)
	}
	return rec
}

// TestConnectionMutationHonoursTokenConnectionScope checks that a token
// scoped to one connection is refused when it updates or deletes
// another, leaving that connection in place, whilst the connection
// inside its scope may still be updated and deleted.
func TestConnectionMutationHonoursTokenConnectionScope(t *testing.T) {
	ds, pool, cleanupDS := newIssue269ConnectionDatastore(t)
	defer cleanupDS()

	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	// The token's owner is a superuser, so its connection scope is the
	// only thing standing between it and connections it does not own.
	const owner = "conn-scope-owner"
	if err := store.CreateUser(owner, "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := store.AsActor(auth.SystemActor()).SetUserSuperuser(owner, true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	rawToken, token, err := store.AsActor(auth.SystemActor()).CreateToken(owner, "connection scope test", nil, true)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	const inScope, outOfScope = 7471, 7472
	seedIssue269Connection(t, pool, inScope, "someone-else", "in-scope")
	seedIssue269Connection(t, pool, outOfScope, "someone-else", "out-of-scope")
	if err := store.AsActor(auth.SystemActor()).SetTokenConnectionScope(token.ID, []auth.ScopedConnection{
		{ConnectionID: inScope, AccessLevel: auth.AccessLevelReadWrite},
	}, true); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}

	handler := NewConnectionHandlerWithSecurity(ds, store,
		auth.NewRBACChecker(store), false, nil, nil)

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		rec := connectionMutationRequest(t, handler, store, method, rawToken,
			outOfScope)
		if rec.Code != http.StatusForbidden ||
			!strings.Contains(rec.Body.String(), connectionOutOfTokenScope) {
			t.Errorf("%s out of scope: expected the 403 scope refusal, got %d: %s",
				method, rec.Code, rec.Body.String())
		}
	}
	if _, err := ds.GetConnection(context.Background(), outOfScope); err != nil {
		t.Errorf("Out-of-scope connection should still exist: %v", err)
	}

	if rec := connectionMutationRequest(t, handler, store, http.MethodPut, rawToken,
		inScope); rec.Code != http.StatusOK {
		t.Errorf("PUT in scope: expected 200, got %d: %s", rec.Code,
			rec.Body.String())
	}
	if rec := connectionMutationRequest(t, handler, store, http.MethodDelete, rawToken,
		inScope); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE in scope: expected 204, got %d: %s", rec.Code,
			rec.Body.String())
	}
}

// TestConnectionInTokenScopeFailsClosed checks the helper's other
// branches: a session is always in scope, whilst a checker without a
// store and a token context that has lost its id are out of scope.
func TestConnectionInTokenScopeFailsClosed(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	checker := auth.NewRBACChecker(store)
	session := withSuperuser(httptest.NewRequest(http.MethodGet, "/", nil))
	if !checker.ConnectionInTokenScope(session.Context(), 9) {
		t.Error("Expected a session to be in scope")
	}
	if auth.NewRBACChecker(nil).ConnectionInTokenScope(session.Context(), 9) {
		t.Error("Expected a checker without a store to refuse the caller")
	}
	lost := withSuperuserToken(httptest.NewRequest(http.MethodGet, "/", nil), 0)
	if checker.ConnectionInTokenScope(lost.Context(), 9) {
		t.Error("Expected a token context without an id to be out of scope")
	}
}
