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
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// An acting token deleted part-way through a request (6 October review)
//
// A token with no rows in a scope kind is unrestricted in it, and
// deleting a token removes its scope rows by the cascade. A token that
// deleted itself after the handler's permission check, which runs before
// the body is read, but before the ceiling check, which runs after, was
// therefore read as unrestricted by every check made after the delete.
// These tests delete the token as the handler starts to read the body,
// which is the point the review stalled the request at, and expect the
// request to be refused as it is without the delete.
// =============================================================================

// deleteOnReadBody is a request body that runs onFirstRead before the
// handler sees the first byte of it.
type deleteOnReadBody struct {
	io.Reader
	onFirstRead func()
}

func (b *deleteOnReadBody) Read(p []byte) (int, error) {
	if b.onFirstRead != nil {
		b.onFirstRead()
		b.onFirstRead = nil
	}
	return b.Reader.Read(p)
}

// doDeletingToken sends body to the RBAC router as caller, deleting
// tokenID once the handler has passed its permission check and starts
// to read the body.
func (f *grantFixture) doDeletingToken(t *testing.T, caller scopeCaller,
	tokenID int64, method, path, body string) *httptest.ResponseRecorder {

	t.Helper()
	req := httptest.NewRequest(method, path, &deleteOnReadBody{
		Reader: strings.NewReader(body),
		onFirstRead: func() {
			if err := f.store.AsActor(auth.SystemActor()).DeleteToken(strconv.FormatInt(tokenID, 10), true); err != nil {
				t.Errorf("DeleteToken failed: %v", err)
			}
		},
	})
	req.Header.Set("Content-Type", "application/json")
	req = caller.wrap(req)
	rec := httptest.NewRecorder()
	switch path {
	case "/api/v1/rbac/users":
		f.h.handleUsers(rec, req)
	default:
		f.h.handleGroupSubpath(rec, req)
	}
	if tok, err := f.store.GetTokenByID(tokenID); err != nil || tok != nil {
		t.Fatalf("Expected token %d to be deleted mid-request, got %v (%v)",
			tokenID, tok, err)
	}
	return rec
}

// TestDeletedActingTokenCannotCreateSuperuser covers the review's live
// reproduction: a superuser's token with a narrowed admin scope is
// refused a superuser account, and deleting itself between the
// permission check and the superuser gate must not change that.
func TestDeletedActingTokenCannotCreateSuperuser(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()
	body := `{"username":"would-be-root","password":"Password1234!x","is_superuser":true}`

	assertStatus(t, f.do(f.adminNarrowed, http.MethodPost, "/api/v1/rbac/users",
		body), http.StatusForbidden)

	tokenID := mustCreateScopedToken(t, f.store, "svc-self-deleting",
		[]string{auth.PermManageUsers, auth.PermManageTokenScopes})
	if err := f.store.AsActor(auth.SystemActor()).SetTokenMCPScopeByNames(tokenID,
		[]string{reachToolInScope}, true); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
	}
	if err := f.store.AsActor(auth.SystemActor()).SetTokenConnectionScope(tokenID, []auth.ScopedConnection{
		{ConnectionID: 5, AccessLevel: auth.AccessLevelReadWrite},
	}, true); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	caller := scopeCaller{name: "selfDeleting",
		wrap: func(r *http.Request) *http.Request {
			return withSuperuserToken(r, tokenID)
		}}

	assertStatus(t, f.doDeletingToken(t, caller, tokenID, http.MethodPost,
		"/api/v1/rbac/users", body), http.StatusForbidden)
	if u, _ := f.store.GetUser("would-be-root"); u != nil {
		t.Fatal("Expected no superuser to be created by a deleted token")
	}
}

// TestDeletedActingTokenCannotGrantMCPItem covers the review's
// non-superuser variant: a token whose MCP scope is list_connections,
// owned by a user who holds query_datastore and manage_permissions,
// may not grant a group query_datastore, and deleting itself between
// the permission check and the ceiling check must not change that.
func TestDeletedActingTokenCannotGrantMCPItem(t *testing.T) {
	f, cleanup := newReachFixture(t)
	defer cleanup()

	ownerID := setupUserWithPermission(t, f.store, "grantor",
		auth.PermManagePermissions)
	if err := f.store.AddUserToGroup(
		f.mcpGroup(t, "datastore", reachToolOutScope), ownerID); err != nil {
		t.Fatalf("AddUserToGroup failed: %v", err)
	}
	newToken := func() (int64, scopeCaller) {
		_, token, err := f.store.AsActor(auth.SystemActor()).CreateToken("grantor", "mid-request delete", nil, true)
		if err != nil {
			t.Fatalf("CreateToken failed: %v", err)
		}
		if err := f.store.AsActor(auth.SystemActor()).SetTokenMCPScopeByNames(token.ID,
			[]string{reachToolInScope}, true); err != nil {
			t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
		}
		return token.ID, scopeCaller{name: "grantor",
			wrap: func(r *http.Request) *http.Request {
				return withToken(r, ownerID, token.ID)
			}}
	}
	groupID := f.group(t, "grantees", nil)
	path := fmt.Sprintf("/api/v1/rbac/groups/%d/privileges/mcp", groupID)
	body := fmt.Sprintf(`{"privilege":%q}`, reachToolOutScope)

	_, caller := newToken()
	assertGrantRefused(t, f.do(caller, http.MethodPost, path, body))

	tokenID, caller := newToken()
	assertGrantRefused(t, f.doDeletingToken(t, caller, tokenID,
		http.MethodPost, path, body))
	if privs, err := f.store.GetGroupEffectiveMCPPrivileges(groupID); err != nil ||
		len(privs) != 0 {
		t.Fatalf("Expected no MCP grant to be written, got %v (%v)", privs, err)
	}
}

// TestGetTokenScopeMissingToken checks that the scope of a token that
// does not exist is reported as not found, now that the store no longer
// reads a missing token's scope as empty.
func TestGetTokenScopeMissingToken(t *testing.T) {
	f, cleanup := newGrantFixture(t)
	defer cleanup()
	assertError(t, f.do(f.session, http.MethodGet,
		"/api/v1/rbac/tokens/999999/scope", ""), http.StatusNotFound,
		"Token not found")
}
