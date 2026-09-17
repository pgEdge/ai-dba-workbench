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
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
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

func TestAuthenticateRequest_APITokenSetsTokenContext(t *testing.T) {
	store := newAuthenticateTestStore(t)

	if err := store.CreateUser("tokenuser", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	rawToken, stored, err := store.CreateToken("tokenuser", "test token", nil)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	ctx, err := AuthenticateRequest(req, store)
	if err != nil {
		t.Fatalf("AuthenticateRequest: %v", err)
	}
	if !IsAPITokenFromContext(ctx) {
		t.Error("IsAPITokenFromContext = false, want true")
	}
	if got := GetTokenIDFromContext(ctx); got != stored.ID {
		t.Errorf("GetTokenIDFromContext = %d, want %d", got, stored.ID)
	}
	if got := GetUsernameFromContext(ctx); got != "tokenuser" {
		t.Errorf("username = %q, want %q", got, "tokenuser")
	}
}

func TestAuthenticateRequest_SessionTokenIsNotAPIToken(t *testing.T) {
	store := newAuthenticateTestStore(t)

	if err := store.CreateUser("sessionuser", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	sessionToken, _, err := store.AuthenticateUser("sessionuser", "Testpass1234")
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)

	ctx, err := AuthenticateRequest(req, store)
	if err != nil {
		t.Fatalf("AuthenticateRequest: %v", err)
	}
	if IsAPITokenFromContext(ctx) {
		t.Error("IsAPITokenFromContext = true, want false for a session token")
	}
	// The key must be present and false, not merely absent, so that the
	// middleware and the direct call agree on the full key set.
	if v, ok := ctx.Value(IsAPITokenContextKey).(bool); !ok || v {
		t.Errorf("IsAPITokenContextKey = (%v, %v), want (false, true)", v, ok)
	}
	if got := GetTokenIDFromContext(ctx); got != 0 {
		t.Errorf("GetTokenIDFromContext = %d, want 0 for a session token", got)
	}
}

// TestAuthenticateRequest_UnresolvableOwnerRejected locks in the guard
// that makes UsernameContextKey non-empty on every success: a credential
// whose identity does not resolve must be rejected outright, because
// downstream ownership comparisons match on the username and an empty
// one would compare equal to an unowned connection. ValidateToken checks
// that the owning row exists and is enabled, so the state is
// manufactured by blanking the username directly.
func TestAuthenticateRequest_UnresolvableOwnerRejected(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "authn-orphan-test-*")
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

	if err := store.CreateUser("ghost", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	rawToken, _, err := store.CreateToken("ghost", "test token", nil)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	db, err := sql.Open("sqlite", filepath.Join(tmpDir, "auth.db"))
	if err != nil {
		t.Fatalf("failed to open auth database: %v", err)
	}
	defer db.Close()
	// Couples to the users.username column. AuthenticateRequest takes a
	// concrete *AuthStore with no seam to stub, so this is the only way
	// in; if the schema moves, give it a narrow user-lookup interface and
	// stub it here rather than deleting the test.
	if _, err := db.Exec("UPDATE users SET username = '' WHERE username = ?", "ghost"); err != nil {
		t.Fatalf("failed to blank the owner's username: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	ctx, err := AuthenticateRequest(req, store)
	if ctx != nil {
		t.Error("expected nil context for an unresolvable owner, got non-nil")
	}
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

// TestAuthenticateRequest_SessionWithMissingUserRejected is the session
// counterpart to TestAuthenticateRequest_UnresolvableOwnerRejected: a
// session whose user cannot be loaded must be rejected outright rather
// than returned as a context carrying a username but no user ID, which
// would still satisfy the ownership comparison in CanAccessConnection
// and so authenticate a caller whose privileges were never verified.
func TestAuthenticateRequest_SessionWithMissingUserRejected(t *testing.T) {
	store := newAuthenticateTestStore(t)

	if err := store.CreateUser("vanishing", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	sessionToken, _, err := store.AuthenticateUser("vanishing", "Testpass1234")
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}
	if err := store.DeleteUser("vanishing"); err != nil {
		t.Fatalf("failed to delete user: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)

	ctx, err := AuthenticateRequest(req, store)
	if ctx != nil {
		t.Error("expected nil context for a session with no user, got non-nil")
	}
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

// TestAuthenticateRequest_SessionAlwaysCarriesUserID locks in the
// guarantee the guard exists to provide: a successful session
// authentication never yields a context missing the RBAC keys.
func TestAuthenticateRequest_SessionAlwaysCarriesUserID(t *testing.T) {
	store := newAuthenticateTestStore(t)

	if err := store.CreateUser("present", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	sessionToken, _, err := store.AuthenticateUser("present", "Testpass1234")
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)

	ctx, err := AuthenticateRequest(req, store)
	if err != nil {
		t.Fatalf("AuthenticateRequest: %v", err)
	}
	if got := GetUserIDFromContext(ctx); got <= 0 {
		t.Errorf("user ID = %d, want a positive value on every success", got)
	}
	if _, ok := ctx.Value(IsSuperuserContextKey).(bool); !ok {
		t.Error("IsSuperuserContextKey missing from a successful session context")
	}
}

// TestAuthenticateRequest_StoreFailureIsDistinguishedFromABadToken
// covers the difference between a deleted account and an unreadable
// database. Both are refused with ErrInvalidToken, because a session
// whose privileges cannot be verified must not be let through, but only
// the database failure also carries ErrAuthStoreUnavailable, so that a
// lock timeout that logs everyone out can be told apart from a run of
// genuinely bad tokens.
func TestAuthenticateRequest_StoreFailureIsDistinguishedFromABadToken(t *testing.T) {
	store := newAuthenticateTestStore(t)

	if err := store.CreateUser("present", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	sessionToken, _, err := store.AuthenticateUser("present", "Testpass1234")
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}

	// A deleted account is a plain bad credential.
	if err := store.DeleteUser("present"); err != nil {
		t.Fatalf("failed to delete user: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	_, err = AuthenticateRequest(req, store)
	if !errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrAuthStoreUnavailable) {
		t.Errorf("deleted account: err = %v, want ErrInvalidToken without ErrAuthStoreUnavailable", err)
	}

	// An unreadable users table is a store failure, for both credential
	// kinds. The API token is validated against its own table, which is
	// intact, and only then fails to load its owner; the session fails
	// inside ValidateSessionToken, when it re-reads the account.
	if err := store.CreateUser("present", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to recreate user: %v", err)
	}
	sessionToken, _, err = store.AuthenticateUser("present", "Testpass1234")
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}
	rawToken, _, err := store.CreateToken("present", "test token", nil)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	if _, err := store.db.Exec("ALTER TABLE users RENAME TO users_unreadable"); err != nil {
		t.Fatalf("failed to rename the users table: %v", err)
	}
	for name, token := range map[string]string{"session": sessionToken, "API token": rawToken} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		ctx, err := AuthenticateRequest(req, store)
		if ctx != nil {
			t.Errorf("%s: expected a nil context when the store is unreadable", name)
		}
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("%s: err = %v, want ErrInvalidToken so that callers still answer 401", name, err)
		}
		if !errors.Is(err, ErrAuthStoreUnavailable) {
			t.Errorf("%s: err = %v, want ErrAuthStoreUnavailable as well", name, err)
		}
	}

	// And a tokens table that cannot be read is reported the same way,
	// without falling through to the session path.
	if _, err := store.db.Exec("ALTER TABLE tokens RENAME TO tokens_unreadable"); err != nil {
		t.Fatalf("failed to rename the tokens table: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	if _, err := AuthenticateRequest(req, store); !errors.Is(err, ErrAuthStoreUnavailable) || !errors.Is(err, ErrInvalidToken) {
		t.Errorf("unreadable tokens table: err = %v, want ErrInvalidToken and ErrAuthStoreUnavailable", err)
	}
}

// TestRejectUnloadableUser covers the helper directly, because the
// store-failure branch is not reachable through AuthenticateRequest in a
// test: the store's own validation reads the account first and reports
// an unreadable database before the helper is consulted.
func TestRejectUnloadableUser(t *testing.T) {
	err := rejectUnloadableUser("session", errors.New("database is locked"))
	if !errors.Is(err, ErrInvalidToken) || !errors.Is(err, ErrAuthStoreUnavailable) {
		t.Errorf("store failure: err = %v, want ErrInvalidToken and ErrAuthStoreUnavailable", err)
	}

	err = rejectUnloadableUser("API token", nil)
	if !errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrAuthStoreUnavailable) {
		t.Errorf("missing account: err = %v, want ErrInvalidToken alone", err)
	}
}

// TestValidateSessionTokenRefusesAnAccountRemovedBehindItsBack covers a
// session whose account row disappeared without the store being told,
// which is what another process sharing auth.db can do. The session is
// refused as a plain bad credential, not as a store failure.
func TestValidateSessionTokenRefusesAnAccountRemovedBehindItsBack(t *testing.T) {
	store := newAuthenticateTestStore(t)

	if err := store.CreateUser("removed", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	sessionToken, _, err := store.AuthenticateUser("removed", "Testpass1234")
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}
	if _, err := store.db.Exec("DELETE FROM users WHERE username = ?", "removed"); err != nil {
		t.Fatalf("failed to delete the account row: %v", err)
	}

	if _, err := store.ValidateSessionToken(sessionToken); err == nil {
		t.Fatal("a session for a removed account was accepted")
	} else if errors.Is(err, ErrAuthStoreUnavailable) {
		t.Errorf("a removed account was reported as a store failure: %v", err)
	}
}

// TestValidateSessionTokenRefusesDisabledAndCorruptSessions covers the
// two remaining refusals: an account disabled behind the store's back,
// which another process sharing auth.db can do, and a session entry that
// is not a *SessionInfo at all, which nothing writes today and the check
// exists to keep true.
func TestValidateSessionTokenRefusesDisabledAndCorruptSessions(t *testing.T) {
	store := newAuthenticateTestStore(t)

	if err := store.CreateUser("switched-off", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	sessionToken, _, err := store.AuthenticateUser("switched-off", "Testpass1234")
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}
	if _, err := store.db.Exec("UPDATE users SET enabled = FALSE WHERE username = ?", "switched-off"); err != nil {
		t.Fatalf("failed to disable the account row: %v", err)
	}
	if _, err := store.ValidateSessionToken(sessionToken); err == nil {
		t.Error("a session for a disabled account was accepted")
	}

	store.sessions.Store(GetTokenHashByRawToken("corrupt"), "not a session")
	if _, err := store.ValidateSessionToken("corrupt"); err == nil || !strings.Contains(err.Error(), "invalid session data") {
		t.Errorf("corrupt session entry: err = %v, want 'invalid session data'", err)
	}
}
