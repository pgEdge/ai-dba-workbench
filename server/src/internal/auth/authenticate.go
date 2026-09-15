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
	"net/http"
)

// ErrMissingCredentials is returned by AuthenticateRequest when the
// request carries no bearer token or session cookie.
var ErrMissingCredentials = errors.New("missing or invalid authentication credentials")

// ErrInvalidToken is returned by AuthenticateRequest when the supplied
// token validates neither as an API token nor as a session token.
var ErrInvalidToken = errors.New("invalid or expired token")

// AuthenticateRequest validates the bearer/session credentials on r and
// returns a context enriched with the authenticated identity. It is the
// single source of truth for HTTP authentication, shared by the
// createAuthWrapper middleware and the LLM proxy's Authorize hook so
// that both paths apply byte-identical validation semantics.
//
// On success the returned context carries:
//
//   - TokenHashContextKey   (always; used for connection isolation/tracing)
//   - IsAPITokenContextKey   (always; true for API tokens, false for
//     session tokens)
//   - TokenIDContextKey      (API tokens only; drives token scoping)
//   - UserIDContextKey       (always for API tokens; for session tokens
//     when the owning user is resolved)
//   - IsSuperuserContextKey  (same)
//   - UsernameContextKey     (always, and never empty: a credential
//     whose identity does not resolve to a username is rejected as
//     ErrInvalidToken rather than returned as an anonymous context)
//
// Validation order is API token first, then session token, matching the
// historical createAuthWrapper behavior exactly. A missing credential
// yields ErrMissingCredentials; a credential that validates as neither
// kind yields ErrInvalidToken. Callers map both to HTTP 401.
func AuthenticateRequest(r *http.Request, store *AuthStore) (context.Context, error) {
	token := ExtractBearerToken(r)
	if token == "" {
		return nil, ErrMissingCredentials
	}

	// Token present - add token hash to context for tracing and isolation.
	tokenHash := GetTokenHashByRawToken(token)
	ctx := context.WithValue(r.Context(), TokenHashContextKey, tokenHash)

	// Try API token first, then session token. Populate RBAC context
	// values (UserID, IsSuperuser, Username) for permission checks.
	storedToken, err := store.ValidateToken(token)
	if err == nil && storedToken != nil {
		ctx = context.WithValue(ctx, IsAPITokenContextKey, true)
		ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)
		ctx = context.WithValue(ctx, UserIDContextKey, storedToken.OwnerID)
		// Look up user to determine superuser status and username. A
		// token whose owner does not resolve to a username is treated as
		// invalid: the identity it would authenticate as is unknown, and
		// downstream ownership comparisons would silently match the
		// empty string.
		user, userErr := store.GetUserByID(storedToken.OwnerID)
		if userErr != nil || user == nil || user.Username == "" {
			return nil, ErrInvalidToken
		}
		ctx = context.WithValue(ctx, IsSuperuserContextKey, user.IsSuperuser)
		ctx = context.WithValue(ctx, UsernameContextKey, user.Username)
		return ctx, nil
	}

	// Try session token.
	username, sessionErr := store.ValidateSessionToken(token)
	if sessionErr != nil || username == "" {
		return nil, ErrInvalidToken
	}
	ctx = context.WithValue(ctx, UsernameContextKey, username)
	ctx = context.WithValue(ctx, IsAPITokenContextKey, false)
	// Get user ID and superuser status for RBAC.
	user, userErr := store.GetUser(username)
	if userErr == nil && user != nil {
		ctx = context.WithValue(ctx, UserIDContextKey, user.ID)
		ctx = context.WithValue(ctx, IsSuperuserContextKey, user.IsSuperuser)
	}
	return ctx, nil
}
