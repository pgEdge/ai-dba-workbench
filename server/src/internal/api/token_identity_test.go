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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// Issue #668: the connection and cluster-group write handlers resolved
// the caller with a session-only lookup, so an API token that the auth
// wrapper had already accepted was answered with 401. These tests drive
// the handlers with an API token alone, authenticated the way
// createAuthWrapper does it, and check that the token owner is the
// identity the handlers act as.

// asWrapped authenticates req with auth.AuthenticateRequest, exactly as
// the REST auth wrapper does, and returns it carrying the resulting
// context. The test fails if the credential is refused.
func asWrapped(t *testing.T, store *auth.AuthStore, req *http.Request) *http.Request {
	t.Helper()
	ctx, err := auth.AuthenticateRequest(req, store)
	if err != nil {
		t.Fatalf("AuthenticateRequest: %v", err)
	}
	return req.WithContext(ctx)
}

// withSession sets token as the bearer and puts the session's username
// in the request context, as the REST auth wrapper does for a browser
// session. The write handlers read the caller from the context alone, so
// a test that drives one directly needs both.
func withSession(t *testing.T, store *auth.AuthStore, req *http.Request, token string) *http.Request {
	t.Helper()
	username, err := store.ValidateSessionToken(token)
	if err != nil {
		t.Fatalf("ValidateSessionToken: %v", err)
	}
	return withUsername(withBearer(req, token), username)
}

// tokenRequestFor builds a JSON request bearing only rawToken.
func tokenRequestFor(method, path, rawToken string, body any) *http.Request {
	var payload string
	if body != nil {
		data, _ := json.Marshal(body)
		payload = string(data)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	return withBearer(req, rawToken)
}

func TestAuthenticatedUsername(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	userID := newTestUser(t, store, "ident-user")
	session, _, err := store.AuthenticateUser("ident-user", "Password1234")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	_, rawToken := createTokenForUser(t, store, userID, "ident token")

	newReq := func(bearer string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if bearer != "" {
			req = withBearer(req, bearer)
		}
		return req
	}

	t.Run("wrapped API token resolves to its owner", func(t *testing.T) {
		got, err := authenticatedUsername(asWrapped(t, store, newReq(rawToken)))
		if err != nil || got != "ident-user" {
			t.Fatalf("got (%q, %v), want (\"ident-user\", nil)", got, err)
		}
	})

	t.Run("wrapped session resolves to its user", func(t *testing.T) {
		got, err := authenticatedUsername(asWrapped(t, store, newReq(session)))
		if err != nil || got != "ident-user" {
			t.Fatalf("got (%q, %v), want (\"ident-user\", nil)", got, err)
		}
	})

	// A request that skipped the wrapper has a context carrying none of
	// a token's scope, so a bearer alone, of either kind, is refused
	// rather than authenticated in the handler.
	for name, bearer := range map[string]string{
		"unwrapped API token": rawToken,
		"unwrapped session":   session,
		"no credential":       "",
	} {
		t.Run(name+" is refused", func(t *testing.T) {
			_, err := authenticatedUsername(newReq(bearer))
			if !errors.Is(err, errNoAuthenticatedUser) {
				t.Fatalf("err = %v, want errNoAuthenticatedUser", err)
			}
		})
	}
}

// TestConnectionWrites_AcceptAPIToken is the regression test for issue
// #668: create, update and delete accept an API token, a created
// connection is owned by the token's owner, and the token's admin and
// connection scopes still gate every write.
func TestConnectionWrites_AcceptAPIToken(t *testing.T) {
	h := newConnCoverageHarness(t)

	_, adminToken := createTokenForUser(t, h.store, h.admin.id, "admin script")
	_, ownerToken := createTokenForUser(t, h.store, h.owner.id, "owner script")
	scopedID, scopedToken := createTokenForUser(t, h.store, h.admin.id, "probes only")
	if err := h.store.AsActor(auth.SystemActor()).SetTokenAdminScope(
		scopedID, []string{auth.PermManageProbes}, true); err != nil {
		t.Fatalf("SetTokenAdminScope: %v", err)
	}
	confinedID, confinedToken := createTokenForUser(t, h.store, h.admin.id, "one server")
	if err := h.store.AsActor(auth.SystemActor()).SetTokenConnectionScope(
		confinedID, []auth.ScopedConnection{
			{ConnectionID: covConnID, AccessLevel: auth.AccessLevelReadWrite},
		}, true); err != nil {
		t.Fatalf("SetTokenConnectionScope: %v", err)
	}
	readOnlyID, readOnlyToken := createTokenForUser(t, h.store, h.owner.id, "owner read")
	if err := h.store.AsActor(auth.SystemActor()).SetTokenConnectionScope(
		readOnlyID, []auth.ScopedConnection{
			{ConnectionID: covConnID, AccessLevel: auth.AccessLevelRead},
		}, true); err != nil {
		t.Fatalf("SetTokenConnectionScope: %v", err)
	}

	body := map[string]any{
		"name":          "token-created",
		"host":          covAllowed,
		"port":          5432,
		"database_name": "postgres",
		"username":      "postgres",
		"password":      "secret",
		"is_shared":     true,
	}

	t.Run("create as token owner", func(t *testing.T) {
		req := asWrapped(t, h.store, tokenRequestFor(http.MethodPost,
			"/api/v1/connections", adminToken, body))
		rec := h.serve(req)
		assertStatus(t, rec, http.StatusCreated)

		var created struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var owner string
		if err := h.pool.QueryRow(context.Background(),
			`SELECT owner_username FROM connections WHERE id = $1`,
			created.ID).Scan(&owner); err != nil {
			t.Fatalf("read owner: %v", err)
		}
		if owner != covAdmin {
			t.Fatalf("owner_username = %q, want %q", owner, covAdmin)
		}
	})

	t.Run("create refused outside the token's admin scope", func(t *testing.T) {
		req := asWrapped(t, h.store, tokenRequestFor(http.MethodPost,
			"/api/v1/connections", scopedToken, body))
		assertStatus(t, h.serve(req), http.StatusForbidden)
	})

	t.Run("create refused to a token confined to one connection", func(t *testing.T) {
		req := asWrapped(t, h.store, tokenRequestFor(http.MethodPost,
			"/api/v1/connections", confinedToken, body))
		rec := h.serve(req)
		assertStatus(t, rec, http.StatusForbidden)
		assertErrorMessage(t, rec, targetOutOfTokenScope)
	})

	t.Run("read-only owner token may not update", func(t *testing.T) {
		req := asWrapped(t, h.store, tokenRequestFor(http.MethodPut,
			"/api/v1/connections/8100", readOnlyToken,
			map[string]any{"description": "updated by read token"}))
		assertStatus(t, h.serve(req), http.StatusForbidden)
	})

	t.Run("read-only owner token may not delete", func(t *testing.T) {
		req := asWrapped(t, h.store, tokenRequestFor(http.MethodDelete,
			"/api/v1/connections/8100", readOnlyToken, nil))
		assertStatus(t, h.serve(req), http.StatusForbidden)
	})

	t.Run("owner token updates its connection", func(t *testing.T) {
		req := asWrapped(t, h.store, tokenRequestFor(http.MethodPut,
			"/api/v1/connections/8100", ownerToken,
			map[string]any{"description": "updated by token"}))
		assertStatus(t, h.serve(req), http.StatusOK)
	})

	t.Run("owner token deletes its connection", func(t *testing.T) {
		req := asWrapped(t, h.store, tokenRequestFor(http.MethodDelete,
			"/api/v1/connections/8100", ownerToken, nil))
		assertStatus(t, h.serve(req), http.StatusNoContent)
	})
}

// TestClusterGroupWrites_AcceptAPIToken covers the cluster-group half of
// issue #668: an API token can create a group, which is recorded as
// owned by the token's owner, and can then rename and delete it.
func TestClusterGroupWrites_AcceptAPIToken(t *testing.T) {
	ds, pool, cleanupDS := newTestDatastore(t)
	defer cleanupDS()

	handler, store, userID, _, cleanupStore := setupGroupUpdateHandler(t, ds)
	defer cleanupStore()
	_, rawToken := createTokenForUser(t, store, userID, "group script")

	req := asWrapped(t, store, tokenRequestFor(http.MethodPost,
		"/api/v1/cluster-groups", rawToken,
		map[string]any{"name": "Token Group"}))
	rec := httptest.NewRecorder()
	handler.createClusterGroup(rec, req)
	assertStatus(t, rec, http.StatusCreated)

	var created struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var owner string
	if err := pool.QueryRow(context.Background(),
		`SELECT owner_username FROM cluster_groups WHERE id = $1`,
		created.ID).Scan(&owner); err != nil {
		t.Fatalf("read owner: %v", err)
	}
	if owner != "group_updater" {
		t.Fatalf("owner_username = %q, want %q", owner, "group_updater")
	}

	req = asWrapped(t, store, tokenRequestFor(http.MethodPut,
		"/api/v1/cluster-groups/1", rawToken,
		map[string]any{"name": "Token Group Renamed"}))
	rec = httptest.NewRecorder()
	handler.updateClusterGroup(rec, req, created.ID)
	assertStatus(t, rec, http.StatusOK)

	// Delete answers 404 for a group with no member the caller can see,
	// which an empty group has for anyone short of a superuser, so the
	// owner is promoted to reach the write. The token still has to be
	// accepted as its owner first.
	if err := store.AsActor(auth.SystemActor()).SetUserSuperuser(
		"group_updater", true); err != nil {
		t.Fatalf("SetUserSuperuser: %v", err)
	}
	req = asWrapped(t, store, tokenRequestFor(http.MethodDelete,
		"/api/v1/cluster-groups/1", rawToken, nil))
	rec = httptest.NewRecorder()
	handler.deleteClusterGroup(rec, req, created.ID)
	assertStatus(t, rec, http.StatusNoContent)
}
