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
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRefresher is a FederatedSessionRefresher whose answer each test
// chooses, and which records how it was called.
type fakeRefresher struct {
	calls atomic.Int32

	mu     sync.Mutex
	tokens []string

	// respond produces the answer; nil means success with a rotated
	// token and an hour's expiry.
	respond func(ctx context.Context, username, refreshToken string) (FederatedRefresh, error)
}

func (f *fakeRefresher) RefreshFederatedSession(ctx context.Context, username,
	refreshToken string) (FederatedRefresh, error) {

	f.calls.Add(1)
	f.mu.Lock()
	f.tokens = append(f.tokens, refreshToken)
	f.mu.Unlock()
	if f.respond != nil {
		return f.respond(ctx, username, refreshToken)
	}
	return FederatedRefresh{RefreshToken: "rotated-" + refreshToken,
		ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (f *fakeRefresher) presented() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tokens...)
}

// newFederatedSessionStore returns a store with one enabled user, alice,
// who may hold sessions.
func newFederatedSessionStore(t *testing.T) *AuthStore {
	t.Helper()

	store, cleanup := createTestAuthStoreForStore(t)
	t.Cleanup(cleanup)
	if err := store.CreateUser("alice", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return store
}

// sessionFor returns the stored session for a raw session token.
func sessionFor(t *testing.T, store *AuthStore, rawToken string) *SessionInfo {
	t.Helper()

	value, ok := store.sessions.Load(GetTokenHashByRawToken(rawToken))
	if !ok {
		return nil
	}
	session, ok := value.(*SessionInfo)
	if !ok {
		t.Fatalf("session value has type %T", value)
	}
	return session
}

// setSessionExpiry moves a stored session's expiry, keeping the rest of
// it, including the federated state it shares with its successors.
func setSessionExpiry(t *testing.T, store *AuthStore, rawToken string, expiresAt time.Time) {
	t.Helper()

	session := sessionFor(t, store, rawToken)
	if session == nil {
		t.Fatal("no session to move the expiry of")
	}
	moved := *session
	moved.ExpiresAt = expiresAt
	store.sessions.Store(GetTokenHashByRawToken(rawToken), &moved)
}

// mustFederatedSession creates a federated session for alice holding
// refreshToken, then expires it a minute ago.
func mustExpiredFederatedSession(t *testing.T, store *AuthStore, refreshToken string) string {
	t.Helper()

	token, _, err := store.CreateFederatedSession("alice", refreshToken, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateFederatedSession: %v", err)
	}
	setSessionExpiry(t, store, token, time.Now().Add(-time.Minute))
	return token
}

func TestCreateFederatedSession(t *testing.T) {
	store := newFederatedSessionStore(t)

	t.Run("expires with the ID token", func(t *testing.T) {
		want := time.Now().Add(10 * time.Minute)
		token, expiresAt, err := store.CreateFederatedSession("alice", "rt-1", want)
		if err != nil {
			t.Fatalf("CreateFederatedSession: %v", err)
		}
		if !expiresAt.Equal(want) {
			t.Fatalf("expiresAt = %v, want %v", expiresAt, want)
		}
		session := sessionFor(t, store, token)
		if session == nil || session.federated == nil {
			t.Fatal("the session was not stored in its federated form")
		}
		if session.federated.refreshToken != "rt-1" {
			t.Fatal("the refresh token was not kept with the session")
		}
		if username, err := store.ValidateSessionToken(token); err != nil || username != "alice" {
			t.Fatalf("ValidateSessionToken = %q, %v", username, err)
		}
	})

	t.Run("is capped at the local session lifetime", func(t *testing.T) {
		_, expiresAt, err := store.CreateFederatedSession("alice", "", time.Now().Add(72*time.Hour))
		if err != nil {
			t.Fatalf("CreateFederatedSession: %v", err)
		}
		if expiresAt.After(time.Now().Add(DefaultSessionExpiry)) {
			t.Fatalf("expiresAt %v is past the cap", expiresAt)
		}
	})

	t.Run("refuses an expiry already past", func(t *testing.T) {
		if _, _, err := store.CreateFederatedSession("alice", "rt", time.Now().Add(-time.Second)); err == nil {
			t.Fatal("a session that had already expired was created")
		}
	})

	t.Run("applies the session checks", func(t *testing.T) {
		if _, _, err := store.CreateFederatedSession("nobody", "rt", time.Now().Add(time.Hour)); err == nil {
			t.Fatal("a session was created for an unknown user")
		}
	})
}

func TestFederatedSessionRefreshSucceeds(t *testing.T) {
	store := newFederatedSessionStore(t)
	refresher := &fakeRefresher{}
	store.SetFederatedSessionRefresher(refresher)

	token := mustExpiredFederatedSession(t, store, "rt-1")

	username, err := store.ValidateSessionToken(token)
	if err != nil || username != "alice" {
		t.Fatalf("ValidateSessionToken = %q, %v", username, err)
	}
	session := sessionFor(t, store, token)
	if session == nil || !session.ExpiresAt.After(time.Now().Add(50*time.Minute)) {
		t.Fatal("the session was not extended to the refreshed expiry")
	}

	// A second expiry redeems the rotated token, not the original.
	setSessionExpiry(t, store, token, time.Now().Add(-time.Minute))
	if _, err := store.ValidateSessionToken(token); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	got := refresher.presented()
	if len(got) != 2 || got[0] != "rt-1" || got[1] != "rotated-rt-1" {
		t.Fatalf("refresh tokens presented = %q", got)
	}
}

func TestFederatedSessionRefreshKeepsUnrotatedToken(t *testing.T) {
	store := newFederatedSessionStore(t)
	refresher := &fakeRefresher{respond: func(context.Context, string, string) (FederatedRefresh, error) {
		return FederatedRefresh{ExpiresAt: time.Now().Add(time.Hour)}, nil
	}}
	store.SetFederatedSessionRefresher(refresher)

	token := mustExpiredFederatedSession(t, store, "rt-keep")
	if _, err := store.ValidateSessionToken(token); err != nil {
		t.Fatalf("ValidateSessionToken: %v", err)
	}
	if got := sessionFor(t, store, token).federated.refreshToken; got != "rt-keep" {
		t.Fatalf("refresh token = %q, want the unrotated one kept", got)
	}
}

func TestFederatedSessionRefreshCapsExpiry(t *testing.T) {
	store := newFederatedSessionStore(t)
	store.SetFederatedSessionRefresher(&fakeRefresher{
		respond: func(context.Context, string, string) (FederatedRefresh, error) {
			return FederatedRefresh{ExpiresAt: time.Now().Add(30 * 24 * time.Hour)}, nil
		}})

	token := mustExpiredFederatedSession(t, store, "rt")
	if _, err := store.ValidateSessionToken(token); err != nil {
		t.Fatalf("ValidateSessionToken: %v", err)
	}
	if sessionFor(t, store, token).ExpiresAt.After(time.Now().Add(DefaultSessionExpiry)) {
		t.Fatal("a refreshed session ran past the cap")
	}
}

// A federated session ends DefaultSessionExpiry after its login however
// often the provider refreshes it: a refresh is capped at that deadline,
// and a session that has reached it is ended without being offered to
// the provider. A provider that leaves the groups claim out of every
// refresh would otherwise let a session keep groups removed at the
// provider for ever.
func TestFederatedSessionEndsAtItsAbsoluteLifetime(t *testing.T) {
	store := newFederatedSessionStore(t)
	refresher := &fakeRefresher{respond: func(_ context.Context, _, refreshToken string) (FederatedRefresh, error) {
		return FederatedRefresh{RefreshToken: refreshToken, ExpiresAt: time.Now().Add(30 * 24 * time.Hour)}, nil
	}}
	store.SetFederatedSessionRefresher(refresher)

	before := time.Now()
	token, _, err := store.CreateFederatedSession("alice", "rt", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateFederatedSession: %v", err)
	}
	fs := sessionFor(t, store, token).federated
	if fs.deadline.Before(before.Add(DefaultSessionExpiry)) || fs.deadline.After(time.Now().Add(DefaultSessionExpiry)) {
		t.Fatalf("deadline = %v, want DefaultSessionExpiry after the login", fs.deadline)
	}

	// Two hours before the deadline, a refresh runs only to the deadline,
	// not to the provider's expiry or a full DefaultSessionExpiry on.
	fs.deadline = time.Now().Add(2 * time.Hour)
	setSessionExpiry(t, store, token, time.Now().Add(-time.Minute))
	if _, err := store.ValidateSessionToken(token); err != nil {
		t.Fatalf("refresh before the deadline: %v", err)
	}
	if got := sessionFor(t, store, token).ExpiresAt; !got.Equal(fs.deadline) {
		t.Fatalf("refreshed expiry = %v, want the deadline %v", got, fs.deadline)
	}

	// Once the deadline has passed the session ends, and the provider is
	// not asked again.
	fs.deadline = time.Now().Add(-time.Second)
	setSessionExpiry(t, store, token, time.Now().Add(-time.Minute))
	if _, err := store.ValidateSessionToken(token); !errors.Is(err, errSessionEnded) {
		t.Fatalf("ValidateSessionToken error = %v, want the session ended", err)
	}
	if sessionFor(t, store, token) != nil {
		t.Fatal("a session past its absolute lifetime is still stored")
	}
	if got := refresher.calls.Load(); got != 1 {
		t.Fatalf("refresher called %d times, want once", got)
	}
}

// Every way an expired federated session can fail to be refreshed ends
// it, and answers as an ordinary invalid session does.
func TestFederatedSessionEndsWhenNotRefreshed(t *testing.T) {
	refused := func(context.Context, string, string) (FederatedRefresh, error) {
		return FederatedRefresh{}, errors.New("invalid_grant")
	}
	pastExpiry := func(context.Context, string, string) (FederatedRefresh, error) {
		return FederatedRefresh{RefreshToken: "rt-new", ExpiresAt: time.Now().Add(-time.Second)}, nil
	}

	tests := []struct {
		name         string
		refreshToken string
		expiredAgo   time.Duration
		refresher    *fakeRefresher
		wantCalls    int32
	}{
		{"provider refuses", "rt", time.Minute, &fakeRefresher{respond: refused}, 1},
		{"refreshed expiry already past", "rt", time.Minute, &fakeRefresher{respond: pastExpiry}, 1},
		{"no refresh token", "", time.Minute, &fakeRefresher{}, 0},
		{"idle past the refresh window", "rt", federatedRefreshWindow + time.Minute, &fakeRefresher{}, 0},
		{"no refresher registered", "rt", time.Minute, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFederatedSessionStore(t)
			if tt.refresher != nil {
				store.SetFederatedSessionRefresher(tt.refresher)
			}
			token, _, err := store.CreateFederatedSession("alice", tt.refreshToken, time.Now().Add(time.Hour))
			if err != nil {
				t.Fatalf("CreateFederatedSession: %v", err)
			}
			fs := sessionFor(t, store, token).federated
			setSessionExpiry(t, store, token, time.Now().Add(-tt.expiredAgo))

			_, err = store.ValidateSessionToken(token)
			if !errors.Is(err, errSessionEnded) {
				t.Fatalf("ValidateSessionToken error = %v, want the session ended", err)
			}
			if sessionFor(t, store, token) != nil {
				t.Fatal("the ended session is still stored")
			}
			if tt.refresher != nil && tt.refresher.calls.Load() != tt.wantCalls {
				t.Fatalf("refresher called %d times, want %d", tt.refresher.calls.Load(), tt.wantCalls)
			}
			if tt.name == "provider refuses" && fs.refreshToken != "" {
				t.Fatal("a refused refresh token was kept")
			}
		})
	}
}

// Many requests finding the session expired at once redeem its refresh
// token exactly once, and all see the refreshed session.
func TestFederatedSessionRefreshIsSingleFlight(t *testing.T) {
	for _, succeed := range []bool{true, false} {
		name := "refused"
		if succeed {
			name = "succeeds"
		}
		t.Run(name, func(t *testing.T) {
			store := newFederatedSessionStore(t)
			release := make(chan struct{})
			refresher := &fakeRefresher{respond: func(context.Context, string, string) (FederatedRefresh, error) {
				<-release
				if !succeed {
					return FederatedRefresh{}, errors.New("invalid_grant")
				}
				return FederatedRefresh{RefreshToken: "rt-2", ExpiresAt: time.Now().Add(time.Hour)}, nil
			}}
			store.SetFederatedSessionRefresher(refresher)
			token := mustExpiredFederatedSession(t, store, "rt-1")

			const requests = 16
			var wg sync.WaitGroup
			errs := make(chan error, requests)
			for range requests {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := store.ValidateSessionToken(token)
					errs <- err
				}()
			}
			// Let every request reach the refresh before it completes.
			deadline := time.Now().Add(5 * time.Second)
			for refresher.calls.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			time.Sleep(20 * time.Millisecond)
			close(release)
			wg.Wait()
			close(errs)

			for err := range errs {
				if succeed && err != nil {
					t.Fatalf("a request failed alongside a successful refresh: %v", err)
				}
				if !succeed && err == nil {
					t.Fatal("a request succeeded after the refresh was refused")
				}
			}
			if got := refresher.calls.Load(); got != 1 {
				t.Fatalf("refresh token redeemed %d times, want once", got)
			}
		})
	}
}

// A logout or credential change during a refresh is not undone by the
// refresh completing.
func TestFederatedSessionRefreshDoesNotResurrectInvalidatedSession(t *testing.T) {
	store := newFederatedSessionStore(t)
	store.SetFederatedSessionRefresher(&fakeRefresher{
		respond: func(context.Context, string, string) (FederatedRefresh, error) {
			store.InvalidateUserSessions("alice")
			return FederatedRefresh{ExpiresAt: time.Now().Add(time.Hour)}, nil
		}})

	token := mustExpiredFederatedSession(t, store, "rt")
	if _, err := store.ValidateSessionToken(token); !errors.Is(err, errSessionEnded) {
		t.Fatalf("ValidateSessionToken error = %v, want the session ended", err)
	}
	if sessionFor(t, store, token) != nil {
		t.Fatal("the refresh brought an invalidated session back")
	}
}

// A request still holding a session that has since been replaced, or
// removed, does not refresh it.
func TestRefreshFederatedSessionStaleView(t *testing.T) {
	store := newFederatedSessionStore(t)
	refresher := &fakeRefresher{}
	store.SetFederatedSessionRefresher(refresher)

	token := mustExpiredFederatedSession(t, store, "rt")
	tokenHash := GetTokenHashByRawToken(token)
	stale := sessionFor(t, store, token)

	// Replaced by a session with federated state of its own.
	store.sessions.Store(tokenHash, &SessionInfo{Username: "alice",
		ExpiresAt: stale.ExpiresAt, federated: &federatedSession{refreshToken: "other"}})
	if _, err := store.refreshFederatedSession(tokenHash, stale); !errors.Is(err, errSessionEnded) {
		t.Fatalf("replaced session: error = %v", err)
	}

	// Removed altogether.
	store.sessions.Delete(tokenHash)
	if _, err := store.refreshFederatedSession(tokenHash, stale); !errors.Is(err, errSessionEnded) {
		t.Fatalf("removed session: error = %v", err)
	}
	if refresher.calls.Load() != 0 {
		t.Fatal("a stale view of a session was refreshed")
	}
}

// A local session still ends at its expiry, and is never offered to the
// refresher.
func TestLocalSessionExpiryUnchanged(t *testing.T) {
	store := newFederatedSessionStore(t)
	refresher := &fakeRefresher{}
	store.SetFederatedSessionRefresher(refresher)

	token, expiresAt, err := store.CreateSessionForUser("alice")
	if err != nil {
		t.Fatalf("CreateSessionForUser: %v", err)
	}
	if got := time.Until(expiresAt); got < DefaultSessionExpiry-time.Minute {
		t.Fatalf("local session expires in %v, want %v", got, DefaultSessionExpiry)
	}
	if sessionFor(t, store, token).federated != nil {
		t.Fatal("a local session was given federated state")
	}

	setSessionExpiry(t, store, token, time.Now().Add(-time.Second))
	if _, err := store.ValidateSessionToken(token); err == nil {
		t.Fatal("an expired local session was accepted")
	}
	if sessionFor(t, store, token) != nil {
		t.Fatal("the expired local session is still stored")
	}
	if refresher.calls.Load() != 0 {
		t.Fatal("a local session was offered for refresh")
	}
}

func TestSetFederatedSessionRefresherNil(t *testing.T) {
	store := newFederatedSessionStore(t)
	store.SetFederatedSessionRefresher(&fakeRefresher{})
	store.SetFederatedSessionRefresher(nil)
	if store.currentRefresher() != nil {
		t.Fatal("the refresher was not removed")
	}
}

func TestSweepExpiredSessions(t *testing.T) {
	store := newFederatedSessionStore(t)
	now := time.Now()

	put := func(key string, info *SessionInfo) { store.sessions.Store(key, info) }
	deadline := now.Add(time.Hour)
	busy := &federatedSession{refreshToken: "rt", deadline: deadline}
	busy.mu.Lock()
	defer busy.mu.Unlock()

	put("live-local", &SessionInfo{Username: "alice", ExpiresAt: now.Add(time.Hour)})
	put("expired-local", &SessionInfo{Username: "alice", ExpiresAt: now.Add(-time.Hour)})
	put("refreshable", &SessionInfo{Username: "alice", ExpiresAt: now.Add(-time.Hour),
		federated: &federatedSession{refreshToken: "rt", deadline: deadline}})
	put("no-refresh-token", &SessionInfo{Username: "alice", ExpiresAt: now.Add(-time.Hour),
		federated: &federatedSession{deadline: deadline}})
	put("past-window", &SessionInfo{Username: "alice",
		ExpiresAt: now.Add(-federatedRefreshWindow - time.Hour),
		federated: &federatedSession{refreshToken: "rt", deadline: deadline}})
	put("past-deadline", &SessionInfo{Username: "alice", ExpiresAt: now.Add(-time.Minute),
		federated: &federatedSession{refreshToken: "rt", deadline: now.Add(-time.Second)}})
	put("refreshing", &SessionInfo{Username: "alice",
		ExpiresAt: now.Add(-federatedRefreshWindow - time.Hour), federated: busy})
	store.sessions.Store("not-a-session", "unexpected")

	store.sweepExpiredSessions(now)

	want := map[string]bool{
		"live-local":       true,
		"expired-local":    false,
		"refreshable":      true,
		"no-refresh-token": false,
		"past-window":      false,
		"past-deadline":    false,
		"refreshing":       true,
		"not-a-session":    true,
	}
	for key, kept := range want {
		if _, ok := store.sessions.Load(key); ok != kept {
			t.Errorf("%s: kept = %v, want %v", key, ok, kept)
		}
	}
}
