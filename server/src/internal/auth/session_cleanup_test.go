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
	"testing"
	"time"
)

func TestStartSessionCleanupSweepsOnlyExpiredSessions(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	now := time.Now()
	store.sessions.Store("expired", &SessionInfo{
		Username: "gone", ExpiresAt: now.Add(-time.Minute)})
	store.sessions.Store("live", &SessionInfo{
		Username: "kept", ExpiresAt: now.Add(time.Hour)})
	// A value that is not a *SessionInfo is left for the readers, which
	// already refuse it, rather than being guessed at by the sweeper.
	store.sessions.Store("corrupt", "not a session")

	store.StartSessionCleanup(5 * time.Millisecond)
	first := store.sessionCleanupStop
	if first == nil {
		t.Fatal("StartSessionCleanup did not record a stop channel")
	}

	// A second start while one is running must not spawn a second
	// sweeper or replace the stop channel the first one listens on.
	store.StartSessionCleanup(5 * time.Millisecond)
	if store.sessionCleanupStop != first {
		t.Error("a second StartSessionCleanup replaced the running sweeper")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := store.sessions.Load("expired"); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the expired session was not swept within 5s")
		}
		time.Sleep(5 * time.Millisecond)
	}

	store.StopSessionCleanup()
	if store.sessionCleanupStop != nil {
		t.Error("StopSessionCleanup left the stop channel set")
	}
	// Stopping again, as Close does, must be harmless.
	store.StopSessionCleanup()

	if _, ok := store.sessions.Load("live"); !ok {
		t.Error("the sweeper removed a session that had not expired")
	}
	if _, ok := store.sessions.Load("corrupt"); !ok {
		t.Error("the sweeper removed an entry that was not a *SessionInfo")
	}

	// Once stopped, the sweeper can be started again.
	store.StartSessionCleanup(time.Hour)
	if store.sessionCleanupStop == nil {
		t.Error("StartSessionCleanup after a stop did not start a sweeper")
	}
}
