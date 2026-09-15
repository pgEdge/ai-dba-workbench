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
	"testing"
	"time"
)

// sessionFarFuture is used by malformed-session-entry tests so the entries
// never look expired regardless of when the test runs.
var sessionFarFuture = time.Now().Add(24 * time.Hour)

func TestCreateSessionForUserEnforcesSessionCap(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("capped", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	tokens := make([]string, 0, maxSessionsPerUser+1)
	for i := 0; i < maxSessionsPerUser+1; i++ {
		token, _, err := store.CreateSessionForUser("capped")
		if err != nil {
			t.Fatalf("CreateSessionForUser %d: %v", i, err)
		}
		tokens = append(tokens, token)
	}

	live := 0
	for _, token := range tokens {
		if _, err := store.ValidateSessionToken(token); err == nil {
			live++
		}
	}
	if live != maxSessionsPerUser {
		t.Fatalf("live sessions = %d, want %d", live, maxSessionsPerUser)
	}
}

func TestCreateSessionForUserUpdatesLastLogin(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("stamped", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Give the account a nonzero failed_attempts count first, so the
	// assertion below proves CreateSessionForUser actually resets it
	// rather than merely observing a column that was already zero.
	if _, err := store.db.Exec(
		"UPDATE users SET failed_attempts = 3 WHERE username = ?", "stamped"); err != nil {
		t.Fatalf("seed failed_attempts: %v", err)
	}
	if _, _, err := store.CreateSessionForUser("stamped"); err != nil {
		t.Fatalf("CreateSessionForUser: %v", err)
	}

	var lastLogin sql.NullTime
	var failedAttempts int
	if err := store.db.QueryRow(
		"SELECT last_login, failed_attempts FROM users WHERE username = ?", "stamped").
		Scan(&lastLogin, &failedAttempts); err != nil {
		t.Fatalf("last_login: %v", err)
	}
	if !lastLogin.Valid {
		t.Fatal("last_login was not set")
	}
	if failedAttempts != 0 {
		t.Fatalf("failed_attempts = %d, want 0", failedAttempts)
	}
}

func TestCreateSessionForUserRejectsServiceAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateServiceAccount("svc", "", "", ""); err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	if _, _, err := store.CreateSessionForUser("svc"); err == nil {
		t.Fatal("expected a service account to be refused a session")
	}
}

func TestCreateSessionForUserRejectsUnknownUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if _, _, err := store.CreateSessionForUser("nobody"); err == nil {
		t.Fatal("expected an error for an unknown user")
	}
}

func TestCreateSessionForUserLookupError(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("broken", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Close the underlying database directly (bypassing AuthStore.Close, which
	// also stops the session-cleanup goroutine) so the lookup query in
	// CreateSessionForUser fails with something other than sql.ErrNoRows.
	if err := store.db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}

	if _, _, err := store.CreateSessionForUser("broken"); err == nil {
		t.Fatal("expected a database error to surface")
	}
}

// TestCreateSessionForUserForLockedSkipsMalformedSessionEntries exercises the
// defensive type-assertion branches in createSessionForUserLocked's eviction
// scan. Nothing in the public API can put a malformed entry into s.sessions,
// so this test does it directly, in-package, to prove those branches are
// skipped safely rather than panicking.
func TestCreateSessionForUserForLockedSkipsMalformedSessionEntries(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("malformed", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// A value that isn't a *SessionInfo.
	store.sessions.Store("not-a-session-info", "garbage")
	// A key that isn't a string.
	store.sessions.Store(42, &SessionInfo{Username: "malformed", ExpiresAt: sessionFarFuture})

	token, _, err := store.CreateSessionForUser("malformed")
	if err != nil {
		t.Fatalf("CreateSessionForUser: %v", err)
	}

	gotUsername, err := store.ValidateSessionToken(token)
	if err != nil {
		t.Fatalf("ValidateSessionToken: %v", err)
	}
	if gotUsername != "malformed" {
		t.Fatalf("ValidateSessionToken username = %q, want %q", gotUsername, "malformed")
	}

	if _, ok := store.sessions.Load("not-a-session-info"); !ok {
		t.Fatal("malformed-value entry was evicted, want it left alone")
	}
	if _, ok := store.sessions.Load(42); !ok {
		t.Fatal("malformed-key entry was evicted, want it left alone")
	}
}

func TestCreateSessionForUserRejectsDisabledUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("off", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.DisableUser("off"); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}
	if _, _, err := store.CreateSessionForUser("off"); err == nil {
		t.Fatal("expected a disabled account to be refused a session")
	}
}
