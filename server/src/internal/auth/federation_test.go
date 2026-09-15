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
	if _, _, err := store.CreateSessionForUser("stamped"); err != nil {
		t.Fatalf("CreateSessionForUser: %v", err)
	}

	var lastLogin sql.NullTime
	if err := store.db.QueryRow(
		"SELECT last_login FROM users WHERE username = ?", "stamped").Scan(&lastLogin); err != nil {
		t.Fatalf("last_login: %v", err)
	}
	if !lastLogin.Valid {
		t.Fatal("last_login was not set")
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

	if _, _, err := store.CreateSessionForUser("malformed"); err != nil {
		t.Fatalf("CreateSessionForUser: %v", err)
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
