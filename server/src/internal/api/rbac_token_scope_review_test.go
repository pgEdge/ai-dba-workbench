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
	if err := store.SetTokenMCPScopeByNames(target,
		[]string{"known_tool"}); err != nil {
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

// connectionMutationRequest issues a PUT or DELETE against a connection
// with the given session bearer, acting as the given superuser API
// token.
func connectionMutationRequest(t *testing.T, h *ConnectionHandler,
	method, rawToken string, tokenID int64, connID int) *httptest.ResponseRecorder {

	t.Helper()
	req := httptest.NewRequest(method,
		"/api/v1/connections/"+strconv.Itoa(connID),
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, rawToken)
	req = withSuperuserToken(req, tokenID)
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

	// These handlers identify the caller from a session bearer, so an
	// API token is refused with 401 before reaching them today. The
	// scope check reads the acting token from the context instead, so
	// the test pairs a session bearer with a token context, which pins
	// the check independently of how the caller was identified.
	const owner = "conn-scope-owner"
	if err := store.CreateUser(owner, "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	rawToken, _, err := store.AuthenticateUser(owner, "Password1234")
	if err != nil {
		t.Fatalf("AuthenticateUser failed: %v", err)
	}
	_, token, err := store.CreateToken(owner, "connection scope test", nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	const inScope, outOfScope = 7471, 7472
	seedIssue269Connection(t, pool, inScope, "someone-else", "in-scope")
	seedIssue269Connection(t, pool, outOfScope, "someone-else", "out-of-scope")
	if err := store.SetTokenConnectionScope(token.ID, []auth.ScopedConnection{
		{ConnectionID: inScope, AccessLevel: auth.AccessLevelReadWrite},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}

	handler := NewConnectionHandlerWithSecurity(ds, store,
		auth.NewRBACChecker(store), false, nil, nil)

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		rec := connectionMutationRequest(t, handler, method, rawToken,
			token.ID, outOfScope)
		if rec.Code != http.StatusForbidden ||
			!strings.Contains(rec.Body.String(), connectionOutOfTokenScope) {
			t.Errorf("%s out of scope: expected the 403 scope refusal, got %d: %s",
				method, rec.Code, rec.Body.String())
		}
	}
	if _, err := ds.GetConnection(context.Background(), outOfScope); err != nil {
		t.Errorf("Out-of-scope connection should still exist: %v", err)
	}

	if rec := connectionMutationRequest(t, handler, http.MethodPut, rawToken,
		token.ID, inScope); rec.Code != http.StatusOK {
		t.Errorf("PUT in scope: expected 200, got %d: %s", rec.Code,
			rec.Body.String())
	}
	if rec := connectionMutationRequest(t, handler, http.MethodDelete, rawToken,
		token.ID, inScope); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE in scope: expected 204, got %d: %s", rec.Code,
			rec.Body.String())
	}
}

// TestConnectionInTokenScopeFailsClosed checks the helper's other
// branches: a session is always in scope, a checker without a store
// admits everything, and a token context that has lost its id is out
// of scope.
func TestConnectionInTokenScopeFailsClosed(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	checker := auth.NewRBACChecker(store)
	session := withSuperuser(httptest.NewRequest(http.MethodGet, "/", nil))
	if !checker.ConnectionInTokenScope(session.Context(), 9) {
		t.Error("Expected a session to be in scope")
	}
	if !auth.NewRBACChecker(nil).ConnectionInTokenScope(session.Context(), 9) {
		t.Error("Expected a checker without a store to admit the caller")
	}
	lost := withSuperuserToken(httptest.NewRequest(http.MethodGet, "/", nil), 0)
	if checker.ConnectionInTokenScope(lost.Context(), 9) {
		t.Error("Expected a token context without an id to be out of scope")
	}
}
