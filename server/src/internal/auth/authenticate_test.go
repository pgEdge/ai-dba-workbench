/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// newAuthenticateTestStore builds a throwaway SQLite-backed auth store
// for AuthenticateRequest tests.
func newAuthenticateTestStore(t *testing.T) *AuthStore {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "authn-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store, err := NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create auth store: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)
	t.Cleanup(func() {
		store.Close()
		os.RemoveAll(tmpDir)
	})
	return store
}

func TestAuthenticateRequest_MissingCredentials(t *testing.T) {
	store := newAuthenticateTestStore(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	ctx, err := AuthenticateRequest(req, store)
	if ctx != nil {
		t.Errorf("expected nil context on missing credentials, got non-nil")
	}
	if !errors.Is(err, ErrMissingCredentials) {
		t.Errorf("expected ErrMissingCredentials, got %v", err)
	}
}

func TestAuthenticateRequest_InvalidToken(t *testing.T) {
	store := newAuthenticateTestStore(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	ctx, err := AuthenticateRequest(req, store)
	if ctx != nil {
		t.Errorf("expected nil context on invalid token, got non-nil")
	}
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestAuthenticateRequest_ValidSessionToken(t *testing.T) {
	store := newAuthenticateTestStore(t)

	if err := store.CreateUser("alice", "Testpass1234", "Alice note", "Alice", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	sessionToken, _, err := store.AuthenticateUser("alice", "Testpass1234")
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)

	ctx, err := AuthenticateRequest(req, store)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if ctx == nil {
		t.Fatal("expected enriched context, got nil")
	}
	if got := GetUsernameFromContext(ctx); got != "alice" {
		t.Errorf("expected username 'alice', got %q", got)
	}
	if got := GetUserIDFromContext(ctx); got <= 0 {
		t.Errorf("expected positive user ID, got %d", got)
	}
	if got := GetTokenHashFromContext(ctx); got == "" {
		t.Error("expected token hash to be set")
	}
	if IsSuperuserFromContext(ctx) {
		t.Error("expected non-superuser for a standard user")
	}
}

func TestAuthenticateRequest_ValidSessionTokenSuperuser(t *testing.T) {
	store := newAuthenticateTestStore(t)

	if err := store.CreateUser("root", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := store.SetUserSuperuser("root", true); err != nil {
		t.Fatalf("failed to set superuser: %v", err)
	}
	sessionToken, _, err := store.AuthenticateUser("root", "Testpass1234")
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)

	ctx, err := AuthenticateRequest(req, store)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !IsSuperuserFromContext(ctx) {
		t.Error("expected superuser flag in context")
	}
}

func TestAuthenticateRequest_ValidAPIToken(t *testing.T) {
	store := newAuthenticateTestStore(t)

	if err := store.CreateUser("svc", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	rawToken, _, err := store.CreateToken("svc", "test token", nil)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	ctx, err := AuthenticateRequest(req, store)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got := GetUserIDFromContext(ctx); got <= 0 {
		t.Errorf("expected positive user ID for API token, got %d", got)
	}
	// API token validation resolves the owner, so username is set too.
	if got := GetUsernameFromContext(ctx); got != "svc" {
		t.Errorf("expected username 'svc', got %q", got)
	}
}
