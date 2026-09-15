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
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// CreateSessionForUser mints a session for an already-authenticated user. It is
// the single place a session comes into existence, so the per-user cap, the
// expiry and the eviction of the oldest session behave identically however the
// user proved their identity. Deliberately not gated on auth_source: both
// local and federated logins mint sessions through this function.
//
// Callers must map every error this returns to a single opaque response
// before it reaches a client; the distinct error text below is for the
// server log only, so that an operator can see the real reason, and must
// never be relayed verbatim to an API caller.
func (s *AuthStore) CreateSessionForUser(username string) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user StoredUser
	err := s.db.QueryRow(
		"SELECT id, username, enabled, is_service_account FROM users WHERE username = ?",
		username).Scan(&user.ID, &user.Username, &user.Enabled, &user.IsServiceAccount)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("looking up user: %w", err)
	}
	if !user.Enabled {
		return "", time.Time{}, fmt.Errorf("account is disabled: %s", username)
	}
	if user.IsServiceAccount {
		return "", time.Time{}, fmt.Errorf("service account cannot hold a session: %s", username)
	}

	return s.createSessionForUserLocked(user.Username, user.ID)
}

// createSessionForUserLocked does the work with s.mu already held. Callers are
// responsible for having established that the user exists and is enabled.
func (s *AuthStore) createSessionForUserLocked(username string, userID int64) (string, time.Time, error) {
	// Generate session token
	tokenBytes := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to generate session token: %w", err)
	}
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	// Set expiration
	expiration := time.Now().Add(DefaultSessionExpiry)

	// Enforce per-user session limit by evicting the oldest session when
	// the user has reached maxSessionsPerUser active sessions.
	type sessionEntry struct {
		key       string
		expiresAt time.Time
	}
	var userSessions []sessionEntry
	s.sessions.Range(func(key, value any) bool {
		session, ok := value.(*SessionInfo)
		if !ok || session.Username != username {
			return true
		}
		keyStr, ok := key.(string)
		if !ok {
			return true
		}
		userSessions = append(userSessions, sessionEntry{
			key:       keyStr,
			expiresAt: session.ExpiresAt,
		})
		return true
	})
	if len(userSessions) >= maxSessionsPerUser {
		// Find and evict the oldest session
		oldest := userSessions[0]
		for _, entry := range userSessions[1:] {
			if entry.expiresAt.Before(oldest.expiresAt) {
				oldest = entry
			}
		}
		s.sessions.Delete(oldest.key)
	}

	// Store session in memory using hashed token to prevent timing attacks
	// The hash operation is constant-time with respect to the token content,
	// preventing attackers from inferring valid tokens via response timing
	tokenHash := GetTokenHashByRawToken(token)
	s.sessions.Store(tokenHash, &SessionInfo{
		Username:  username,
		ExpiresAt: expiration,
	})

	// Update last login and reset failed attempts (best effort, non-critical)
	now := time.Now()
	//nolint:errcheck // Best effort update, login already succeeded
	s.db.Exec("UPDATE users SET last_login = ?, failed_attempts = 0 WHERE id = ?", now, userID)

	return token, expiration, nil
}
