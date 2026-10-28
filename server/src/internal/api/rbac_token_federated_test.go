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
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// TestCreateTokenForFederatedOwner checks that, whilst
// http.auth.oidc.allow_api_tokens is off, the REST route refuses to mint
// a token for an account that signs in through an identity provider,
// even for a superuser, with a 403 that says why and a denial in the
// audit log; and that switching the setting on lets it through at once
// (issue #742).
func TestCreateTokenForFederatedOwner(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	if err := store.CreateUser("fed", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("fed", "https://idp.example.com",
		"subject-1", false); err != nil {
		t.Fatalf("LinkFederatedIdentity failed: %v", err)
	}
	allowed := false
	store.SetFederatedAPITokenPolicy(func() bool { return allowed })

	rec := tokenRouteRequest(handler, http.MethodPost, "/api/v1/rbac/tokens/",
		`{"owner_username":"fed"}`, withSuperuserSession)
	assertRefusedWith(t, rec, auth.FederatedAPITokenRefusalReason)

	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Outcome: string(auth.OutcomeDenied)})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 1 || events[0].Error != auth.FederatedAPITokenRefusalReason {
		t.Fatalf("Expected one denial recording the refusal, got %+v", events)
	}
	tokens, err := store.ListUserTokens("fed")
	if err != nil {
		t.Fatalf("ListUserTokens failed: %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("Expected no token for fed, got %d", len(tokens))
	}

	allowed = true
	assertStatus(t, tokenRouteRequest(handler, http.MethodPost,
		"/api/v1/rbac/tokens/", `{"owner_username":"fed"}`,
		withSuperuserSession), http.StatusCreated)
}
