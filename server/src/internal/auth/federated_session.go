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
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/logging"
)

// A federated session (issue #742) runs only as long as the identity
// provider keeps vouching for its user. It expires when the ID token the
// login produced expires, rather than after the fixed DefaultSessionExpiry
// a local session gets, and the first request after that redeems the
// refresh token the provider issued with it. The provider refuses that
// once the user has been removed or disabled there, and the session then
// ends; when it agrees, the session runs on to the new ID token's expiry,
// and the federated login handler reconciles the user's groups and
// superuser status from the new ID token on the way.
//
// The refresh token is a long-lived credential, so it is held here, in
// memory, with the session it belongs to, and nowhere else: it is never
// written to auth.db, never logged, and never put into an error.

// federatedRefreshTimeout bounds one refresh, which is a request to the
// identity provider's token endpoint and possibly its JWKS endpoint made
// whilst the request that found the session expired waits.
const federatedRefreshTimeout = 15 * time.Second

// federatedRefreshWindow is how long after its expiry a federated
// session holding a refresh token is kept for a refresh to find. It
// bounds how long an idle federated session lingers in memory, since a
// refresh happens only when a request arrives; a request after the
// window has to log in again.
const federatedRefreshWindow = DefaultSessionExpiry

// errSessionEnded is what ValidateSessionToken reports for a federated
// session that could not be refreshed. It reads exactly as any other
// invalid session does, so that a caller learns nothing about why.
var errSessionEnded = errors.New("invalid session token")

// FederatedRefresh is what a successful refresh of a federated session
// yields.
type FederatedRefresh struct {
	// RefreshToken is the refresh token to redeem next time. Empty means
	// the provider did not rotate it, and the redeemed one is kept.
	RefreshToken string

	// ExpiresAt is when the refreshed session expires: the new ID
	// token's expiry, or the access token's when no ID token came back.
	ExpiresAt time.Time
}

// FederatedSessionRefresher redeems a federated session's refresh token
// and re-applies the identity provider's view of the user. The federated
// login handler implements it, since that is where the provider and the
// login policy live; the store only decides when to call it, and keeps
// the session and its refresh token.
//
// It is called without any store lock held, and may call back into the
// store (to reconcile the user's groups, for one). Any error ends the
// session. ctx carries the refresh timeout. The refresh token must not
// appear in a returned error.
type FederatedSessionRefresher interface {
	RefreshFederatedSession(ctx context.Context, username,
		refreshToken string) (FederatedRefresh, error)
}

// refresherBox lets the refresher, an interface, sit behind an
// atomic.Pointer.
type refresherBox struct {
	refresher FederatedSessionRefresher
}

// SetFederatedSessionRefresher registers the refresher expired federated
// sessions are offered to. Passing nil removes it, after which an expired
// federated session simply ends.
func (s *AuthStore) SetFederatedSessionRefresher(refresher FederatedSessionRefresher) {
	if refresher == nil {
		s.refresher.Store(nil)
		return
	}
	s.refresher.Store(&refresherBox{refresher: refresher})
}

// currentRefresher returns the registered refresher, or nil.
func (s *AuthStore) currentRefresher() FederatedSessionRefresher {
	if box := s.refresher.Load(); box != nil {
		return box.refresher
	}
	return nil
}

// federatedSession is the part of a federated session that survives a
// refresh. The SessionInfo values before and after a refresh share one,
// so that its mutex lets only one refresh of the session run at a time.
type federatedSession struct {
	// mu is held for the whole of a refresh, so that one session never
	// has two in flight. Providers commonly rotate the refresh token on
	// every use and treat the reuse of a rotated-out one as theft,
	// revoking the whole grant, so two concurrent requests that each
	// redeemed the same token would log the user out.
	mu sync.Mutex

	// refreshToken is the token to redeem next, or empty when the
	// provider issued none. Guarded by mu.
	refreshToken string
}

// CreateFederatedSession creates a session for a user who has just
// completed a federated login. The session expires at expiresAt, the ID
// token's expiry, capped at DefaultSessionExpiry from now; refreshToken,
// which may be empty, is kept in memory with it so that a request after
// that expiry can extend it.
//
// It applies every check CreateSessionForUser does, and its errors need
// the same care: they are for the server log only.
func (s *AuthStore) CreateFederatedSession(username, refreshToken string,
	expiresAt time.Time) (string, time.Time, error) {

	now := time.Now()
	if !expiresAt.After(now) {
		return "", time.Time{}, fmt.Errorf("federated session would already have expired")
	}
	if ceiling := now.Add(DefaultSessionExpiry); expiresAt.After(ceiling) {
		expiresAt = ceiling
	}

	token, _, err := s.CreateSessionForUser(username)
	if err != nil {
		return "", time.Time{}, err
	}

	// Swap the session just minted for its federated form, but only if
	// it is still the one stored: a credential change invalidating the
	// user's sessions in between must not be undone by storing it again.
	tokenHash := GetTokenHashByRawToken(token)
	minted, ok := s.sessions.Load(tokenHash)
	if !ok || !s.sessions.CompareAndSwap(tokenHash, minted, &SessionInfo{
		Username:  username,
		ExpiresAt: expiresAt,
		federated: &federatedSession{refreshToken: refreshToken},
	}) {
		return "", time.Time{}, fmt.Errorf("session for %s was invalidated whilst it was being created", username)
	}

	return token, expiresAt, nil
}

// refreshFederatedSession handles a session found expired. A local
// session, or a federated one with no refresh token, past its refresh
// window or with no refresher to offer it to, is removed and refused. A
// federated one is otherwise refreshed, once, however many requests find
// it expired at the same moment: the first does the refresh while the
// rest wait on the session's mutex and then use its result.
//
// The refresh runs with no store lock held, on a context of its own
// rather than the request's, so that a client giving up on one request
// cannot cut a refresh short and end the session for every other tab;
// federatedRefreshTimeout bounds it instead.
func (s *AuthStore) refreshFederatedSession(tokenHash string,
	expired *SessionInfo) (*SessionInfo, error) {

	fs := expired.federated
	if fs == nil {
		s.sessions.CompareAndDelete(tokenHash, expired)
		return nil, errSessionEnded
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Another request may have refreshed or ended the session whilst
	// this one waited for the mutex.
	current := s.loadFederatedSession(tokenHash, fs)
	if current == nil {
		return nil, errSessionEnded
	}
	if current.ExpiresAt.After(time.Now()) {
		return current, nil
	}

	refresher := s.currentRefresher()
	if reason := unrefreshableReason(current, refresher, time.Now()); reason != "" {
		return nil, s.endFederatedSession(tokenHash, current, reason)
	}

	ctx, cancel := context.WithTimeout(context.Background(), federatedRefreshTimeout)
	defer cancel()

	refreshed, err := refresher.RefreshFederatedSession(ctx, current.Username, fs.refreshToken)
	if err != nil {
		// The refresh token is spent or refused either way; drop it so
		// that nothing holding this session can present it again.
		fs.refreshToken = ""
		return nil, s.endFederatedSession(tokenHash, current,
			"the identity provider did not refresh it: "+err.Error())
	}
	if refreshed.RefreshToken != "" {
		fs.refreshToken = refreshed.RefreshToken
	}

	expiresAt, ok := cappedRefreshExpiry(refreshed.ExpiresAt, time.Now())
	if !ok {
		return nil, s.endFederatedSession(tokenHash, current,
			"the identity provider refreshed it with an expiry already past")
	}

	next := &SessionInfo{Username: current.Username, ExpiresAt: expiresAt, federated: fs}
	if !s.sessions.CompareAndSwap(tokenHash, current, next) {
		// The session was invalidated (a logout, or a change to the
		// account) whilst the refresh was in flight; the refresh must
		// not bring it back.
		return nil, errSessionEnded
	}

	//nolint:gosec // G706: username passed through logging.SanitizeForLog
	log.Printf("[AUTH] Refreshed federated session for user %s",
		logging.SanitizeForLog(current.Username))
	return next, nil
}

// loadFederatedSession returns the session stored under tokenHash, or
// nil when it has gone or is no longer the federated session fs belongs
// to.
func (s *AuthStore) loadFederatedSession(tokenHash string, fs *federatedSession) *SessionInfo {
	value, ok := s.sessions.Load(tokenHash)
	if !ok {
		return nil
	}
	current, ok := value.(*SessionInfo)
	if !ok || current.federated != fs {
		return nil
	}
	return current
}

// unrefreshableReason explains why an expired federated session cannot
// be offered to the identity provider, or returns "" when it can. The
// caller holds the session's mutex, which guards its refresh token.
func unrefreshableReason(session *SessionInfo, refresher FederatedSessionRefresher,
	now time.Time) string {

	switch {
	case session.federated.refreshToken == "":
		return "it reached its ID token's expiry and the identity provider issued no refresh token"
	case now.After(session.ExpiresAt.Add(federatedRefreshWindow)):
		return "it was idle for longer than the refresh window"
	case refresher == nil:
		return "federated login is not configured, so it cannot be refreshed"
	}
	return ""
}

// cappedRefreshExpiry caps a refreshed session's expiry at a local
// session's lifetime from now, and reports false when the provider gave
// an expiry that has already passed.
func cappedRefreshExpiry(expiresAt, now time.Time) (time.Time, bool) {
	if !expiresAt.After(now) {
		return time.Time{}, false
	}
	if ceiling := now.Add(DefaultSessionExpiry); expiresAt.After(ceiling) {
		return ceiling, true
	}
	return expiresAt, true
}

// endFederatedSession removes a federated session that could not be
// refreshed, logs why, and returns the error to refuse the request
// with.
func (s *AuthStore) endFederatedSession(tokenHash string, session *SessionInfo,
	reason string) error {

	s.sessions.CompareAndDelete(tokenHash, session)
	//nolint:gosec // G706: both values passed through logging.SanitizeForLog
	log.Printf("[AUTH] Federated session for user %s ended: %s",
		logging.SanitizeForLog(session.Username), logging.SanitizeForLog(reason))
	return errSessionEnded
}

// sweepExpiredSessions removes the sessions that have expired as of now.
// A federated session holding a refresh token is kept until its refresh
// window has passed too, since a request may yet arrive to refresh it,
// and so is one whose refresh is in flight.
func (s *AuthStore) sweepExpiredSessions(now time.Time) {
	s.sessions.Range(func(key, value any) bool {
		session, ok := value.(*SessionInfo)
		if !ok || !session.ExpiresAt.Before(now) {
			return true
		}
		if fs := session.federated; fs != nil {
			if !fs.mu.TryLock() {
				return true
			}
			refreshable := fs.refreshToken != "" &&
				now.Before(session.ExpiresAt.Add(federatedRefreshWindow))
			fs.mu.Unlock()
			if refreshable {
				return true
			}
		}
		s.sessions.CompareAndDelete(key, session)
		return true
	})
}
