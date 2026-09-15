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

// The audit log needs to name the API token that made a REST request,
// but the obvious way to carry the token id, auth.TokenIDContextKey,
// is load-bearing for authorisation: auth.RBACChecker treats a non-zero
// value as "intersect this user's privileges with the token's scope".
// The REST middleware has never set it, so REST requests are not
// scope-limited, and auth.AuthenticateRequest therefore sets the
// attribution-only auth.AuditTokenIDContextKey instead.
//
// These tests pin that distinction down. If someone later sets
// TokenIDContextKey on the REST path for attribution, REST
// authorisation changes for every endpoint that consults a connection,
// MCP tool or admin permission, and these tests fail.

// scopedTokenFixture builds a user who may read connection 1 through a
// group grant, plus an API token belonging to that user whose
// connection scope names only connection 2. Under token-scope
// enforcement the token could not read connection 1; on the REST path
// it still can, because REST does not apply token scope.
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

func TestRESTTokenScopeUnchangedForConnectionAccess(t *testing.T) {
	store, checker, rawToken := scopedTokenFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/1", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	ctx, err := auth.AuthenticateRequest(req, store)
	if err != nil {
		t.Fatalf("expected authentication to succeed, got %v", err)
	}

	// The token's connection scope excludes connection 1. REST has
	// never applied that scope, so access must still be granted.
	canAccess, level := checker.CanAccessConnection(ctx, 1)
	if !canAccess {
		t.Fatal("REST access to connection 1 was denied: the token's " +
			"connection scope is being applied on the REST path, which " +
			"changes authorisation for every REST endpoint")
	}
	if level != auth.AccessLevelRead {
		t.Errorf("access level: got %q, want %q", level, auth.AccessLevelRead)
	}

	// The scoping key must remain unset, whilst the attribution key
	// carries the token id for the audit log.
	if got := auth.GetTokenIDFromContext(ctx); got != 0 {
		t.Errorf("scoping token id: got %d, want 0 on the REST path", got)
	}
	if got := auth.GetAuditTokenIDFromContext(ctx); got == 0 {
		t.Error("expected the audit token id to be set for attribution")
	}
}

func TestRESTTokenScopeUnchangedThroughConnectionHandler(t *testing.T) {
	store, checker, rawToken := scopedTokenFixture(t)

	// A nil datastore means the handler panics after the gate, so a
	// 403 here would be the gate itself denying the request.
	handler := NewConnectionHandlerWithSecurity(nil, store, checker,
		true, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/1", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	ctx, err := auth.AuthenticateRequest(req, store)
	if err != nil {
		t.Fatalf("expected authentication to succeed, got %v", err)
	}
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	assertGatePassed(t, rec, func() {
		handler.getConnection(rec, req, 1)
	})
}

func TestRESTTokenScopeStillAppliesOnTheMCPPath(t *testing.T) {
	store, checker, rawToken := scopedTokenFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/1", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	ctx, err := auth.AuthenticateRequest(req, store)
	if err != nil {
		t.Fatalf("expected authentication to succeed, got %v", err)
	}

	// The MCP middleware sets TokenIDContextKey, which is what turns
	// scope enforcement on. Simulating it here shows the scope is
	// genuinely restrictive, so the REST result above is not merely an
	// empty scope that would have allowed the request anyway.
	tokenID := auth.GetAuditTokenIDFromContext(ctx)
	if tokenID == 0 {
		t.Fatal("expected an audit token id to reuse as the scoping id")
	}
	scopedCtx := context.WithValue(ctx, auth.TokenIDContextKey, tokenID)

	if canAccess, _ := checker.CanAccessConnection(scopedCtx, 1); canAccess {
		t.Error("expected the token scope to deny connection 1 when " +
			"scope enforcement is on; the fixture is not restrictive")
	}
}
