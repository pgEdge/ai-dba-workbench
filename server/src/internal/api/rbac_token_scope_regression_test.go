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
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

// The audit log names the API token that made a REST request by reading
// auth.TokenIDContextKey, the same key auth.RBACChecker reads to decide
// whether to intersect the user's privileges with the token's scope.
// One key carries both facts on purpose: a second, attribution-only key
// would let a request reach a handler with the token known to the log
// but withheld from the scope check, and an earlier draft of this
// feature did exactly that on the strength of the REST mux being
// wrapped by auth.AuthMiddleware, which set the scoping key on its own.
// That is not a property worth building on: auth.AuthenticateRequest is
// also mounted on paths the middleware does not wrap (the LLM proxy's
// Authorize hook), and a change to the middleware must not be able to
// switch scope enforcement off for every REST endpoint.
//
// These tests therefore pin that the scoping key is set on every path
// that authenticates an API token, both through the served stack and
// through auth.AuthenticateRequest alone, and that a scoped token is
// denied on each, whilst a session for the same user is allowed through
// her own grant so that the denial is shown to come from the scope.

// scopedTokenFixture builds a user who may read connection 1 through a
// group grant, plus an API token belonging to that user whose
// connection scope names only connection 2. With token scope enforced
// the token cannot read connection 1; with the scoping key absent the
// user's own grant decides and it can.
func scopedTokenFixture(t *testing.T) (*auth.AuthStore, *auth.RBACChecker, string) {
	t.Helper()

	store, err := auth.NewAuthStore(t.TempDir(), 0, 0)
	if err != nil {
		t.Fatalf("failed to create auth store: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)
	t.Cleanup(func() { store.Close() })

	if err := store.CreateUser("scoped", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	userID, err := store.GetUserID("scoped")
	if err != nil {
		t.Fatalf("failed to read user id: %v", err)
	}
	groupID, err := store.CreateGroup("scoped-group", "Grants connection 1")
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}
	if err := store.AddUserToGroup(groupID, userID); err != nil {
		t.Fatalf("failed to add user to group: %v", err)
	}
	if err := store.GrantConnectionPrivilege(groupID, 1, "read"); err != nil {
		t.Fatalf("failed to grant connection privilege: %v", err)
	}

	rawToken, stored, err := store.CreateToken("scoped", "scoped token", nil)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	if err := store.SetTokenConnectionScope(stored.ID, []auth.ScopedConnection{
		{ConnectionID: 2, AccessLevel: "read"},
	}); err != nil {
		t.Fatalf("failed to scope token: %v", err)
	}

	return store, auth.NewRBACChecker(store), rawToken
}

// serveThroughStack runs one request through the middleware composition
// the server actually uses, auth.AuthMiddleware wrapping a mux whose
// handler calls auth.AuthenticateRequest, and hands the resulting
// context to the caller.
func serveThroughStack(t *testing.T, store *auth.AuthStore,
	rawToken string) context.Context {

	t.Helper()

	var inner context.Context
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/connections/1",
		func(_ http.ResponseWriter, r *http.Request) {
			ctx, err := auth.AuthenticateRequest(r, store)
			if err != nil {
				t.Errorf("expected authentication to succeed, got %v", err)
				return
			}
			inner = ctx
		})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/1", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	auth.AuthMiddleware(store, true)(mux).
		ServeHTTP(httptest.NewRecorder(), req)

	if inner == nil {
		t.Fatal("the handler was never reached")
	}

	return inner
}

// authenticateOnly returns the context auth.AuthenticateRequest builds
// on its own, without the middleware that wraps it in the served stack;
// this is the composition the LLM proxy runs.
func authenticateOnly(t *testing.T, store *auth.AuthStore,
	rawToken string) context.Context {

	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/1", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	ctx, err := auth.AuthenticateRequest(req, store)
	if err != nil {
		t.Fatalf("expected authentication to succeed, got %v", err)
	}

	return ctx
}

// sessionFor signs the fixture user in and returns her session token,
// so that the same identity can be presented without any token scope.
func sessionFor(t *testing.T, store *auth.AuthStore) string {
	t.Helper()

	sessionToken, _, err := store.AuthenticateUser("scoped", "Testpass1234")
	if err != nil {
		t.Fatalf("failed to sign the fixture user in: %v", err)
	}

	return sessionToken
}

// TestTokenScopeEnforcedOnEveryAuthenticatedPath pins where the scoping
// key comes from: auth.AuthenticateRequest sets it for an API token on
// its own, so the served stack and the bare call agree, and a scoped
// token is confined to its scope on both. A session for the same user
// carries no token id and is allowed through her own grant, which is
// what shows the two denials come from the scope rather than from a
// user who never held the privilege.
func TestTokenScopeEnforcedOnEveryAuthenticatedPath(t *testing.T) {
	store, checker, rawToken := scopedTokenFixture(t)
	sessionToken := sessionFor(t, store)

	cases := []struct {
		name        string
		ctx         func() context.Context
		wantTokenID bool
		wantAccess  bool
	}{
		{
			name:        "TokenThroughServedStack",
			ctx:         func() context.Context { return serveThroughStack(t, store, rawToken) },
			wantTokenID: true,
			wantAccess:  false,
		},
		{
			name:        "TokenThroughAuthenticateRequestAlone",
			ctx:         func() context.Context { return authenticateOnly(t, store, rawToken) },
			wantTokenID: true,
			wantAccess:  false,
		},
		{
			name:        "SessionThroughServedStack",
			ctx:         func() context.Context { return serveThroughStack(t, store, sessionToken) },
			wantTokenID: false,
			wantAccess:  true,
		},
		{
			name:        "SessionThroughAuthenticateRequestAlone",
			ctx:         func() context.Context { return authenticateOnly(t, store, sessionToken) },
			wantTokenID: false,
			wantAccess:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.ctx()

			tokenID := auth.GetTokenIDFromContext(ctx)
			if tc.wantTokenID && tokenID == 0 {
				t.Error("expected the token id to be set; if it is no " +
					"longer set on this path, every API-token request " +
					"through it has just become unscoped and unattributed")
			}
			if !tc.wantTokenID && tokenID != 0 {
				t.Errorf("token id: got %d, want 0 for a session", tokenID)
			}

			// Attribution reads the same key, so a token is named
			// exactly when its scope is enforced.
			actor := auth.ActorFromContext(ctx)
			if tc.wantTokenID && (actor.Type != auth.ActorToken ||
				actor.ID == nil || *actor.ID != tokenID) {
				t.Errorf("expected the audit actor to be token %d, got %+v",
					tokenID, actor)
			}
			if !tc.wantTokenID && actor.Type != auth.ActorUser {
				t.Errorf("expected the audit actor to be the user, got %+v", actor)
			}

			canAccess, _ := checker.CanAccessConnection(ctx, 1)
			if canAccess != tc.wantAccess {
				t.Errorf("access to connection 1: got %v, want %v",
					canAccess, tc.wantAccess)
			}
		})
	}
}

// TestScopedTokenDeniedThroughConnectionHandler checks the same thing
// at the handler's gate rather than at the checker, so that a change to
// how the gate reads the context is caught too. The token's scope
// excludes connection 1, so the served stack must refuse the request
// before it reaches the nil datastore that would otherwise panic.
func TestScopedTokenDeniedThroughConnectionHandler(t *testing.T) {
	store, checker, rawToken := scopedTokenFixture(t)
	handler := NewConnectionHandlerWithSecurity(nil, store, checker,
		true, nil, nil)

	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/connections/1",
		func(w http.ResponseWriter, r *http.Request) {
			ctx, err := auth.AuthenticateRequest(r, store)
			if err != nil {
				t.Errorf("expected authentication to succeed, got %v", err)
				return
			}
			handler.getConnection(w, r.WithContext(ctx), 1)
		})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/1", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	auth.AuthMiddleware(store, true)(mux).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d; a token whose scope excludes "+
			"connection 1 must not read it through the REST path",
			rec.Code, http.StatusForbidden)
	}
}

// TestTokenScopeIsGenuinelyRestrictive checks the fixture itself: the
// denials above must come from the token's scope rather than from a
// user who never had the privilege in the first place, so the same
// user's session must be allowed at the level her group grants.
func TestTokenScopeIsGenuinelyRestrictive(t *testing.T) {
	store, checker, rawToken := scopedTokenFixture(t)

	sessionCtx := authenticateOnly(t, store, sessionFor(t, store))
	if canAccess, level := checker.CanAccessConnection(sessionCtx, 1); !canAccess ||
		level != auth.AccessLevelRead {
		t.Fatalf("the user's own grant should allow connection 1: "+
			"canAccess=%v level=%q", canAccess, level)
	}

	tokenCtx := authenticateOnly(t, store, rawToken)
	if canAccess, _ := checker.CanAccessConnection(tokenCtx, 1); canAccess {
		t.Error("expected the token scope to deny connection 1; the " +
			"fixture is not restrictive")
	}
}
