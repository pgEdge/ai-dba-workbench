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
	"log"
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/logging"
)

// ErrMissingCredentials is returned by AuthenticateRequest when the
// request carries no bearer token or session cookie.
var ErrMissingCredentials = errors.New("missing or invalid authentication credentials")

// ErrInvalidToken is returned by AuthenticateRequest when the supplied
// token validates neither as an API token nor as a session token.
var ErrInvalidToken = errors.New("invalid or expired token")

// ErrAuthStoreUnavailable is returned, wrapped together with
// ErrInvalidToken, when a credential could not be checked because the
// authentication database could not be read. Callers keep mapping it to
// 401, because a caller whose privileges cannot be verified must not be
// let through, but errors.Is can tell it from a plainly bad token, and
// every place that produces it logs the underlying error, so that a run
// of logged-out users can be traced to the database failure that caused
// it rather than looking like a run of bad tokens.
var ErrAuthStoreUnavailable = errors.New("authentication store unavailable")

// rejectUnloadableUser returns the error for a credential that validated
// but whose account could not be loaded. A store error is logged and
// reported as both ErrInvalidToken and ErrAuthStoreUnavailable; an
// account that simply is not there, which is what a deleted user looks
// like, is a plain ErrInvalidToken, logged at the same place so the two
// are told apart in the server log as well as by errors.Is.
func rejectUnloadableUser(kind string, loadErr error) error {
	if loadErr != nil {
		//nolint:gosec // G706: error text passed through logging.SanitizeForLog
		log.Printf("[AUTH] Rejecting %s: the authentication database could not be read: %s",
			kind, logging.SanitizeForLog(loadErr.Error()))
		return errors.Join(ErrInvalidToken, ErrAuthStoreUnavailable)
	}
	log.Printf("[AUTH] Rejecting %s: its account no longer exists", kind)
	return ErrInvalidToken
}

// rejectFailedValidation returns the error for a credential the store
// refused to validate: ErrInvalidToken, joined with ErrAuthStoreUnavailable
// when the store reported that it could not be read. The store has
// already logged that case.
func rejectFailedValidation(validationErr error) error {
	if errors.Is(validationErr, ErrAuthStoreUnavailable) {
		return errors.Join(ErrInvalidToken, ErrAuthStoreUnavailable)
	}
	return ErrInvalidToken
}

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
//   - UserIDContextKey       (always)
//   - IsSuperuserContextKey  (always)
//   - UsernameContextKey     (always, and never empty: a credential
//     whose identity does not resolve to a username is rejected as
//     ErrInvalidToken rather than returned as an anonymous context)
//
// Validation order is API token first, then session token, matching the
// historical createAuthWrapper behavior exactly. A missing credential
// yields ErrMissingCredentials; a credential that validates as neither
// kind yields ErrInvalidToken; a credential that validated but whose
// account could not be read yields ErrInvalidToken joined with
// ErrAuthStoreUnavailable. Callers map all of them to HTTP 401.
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
	if errors.Is(err, ErrAuthStoreUnavailable) {
		// The session path would read the same database, so there is
		// nothing to be gained by trying it, and the failure should be
		// reported for what it is.
		return nil, rejectFailedValidation(err)
	}
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
			return nil, rejectUnloadableUser("API token", userErr)
		}
		ctx = context.WithValue(ctx, IsSuperuserContextKey, user.IsSuperuser)
		ctx = context.WithValue(ctx, UsernameContextKey, user.Username)
		return ctx, nil
	}

	// Try session token.
	username, sessionErr := store.ValidateSessionToken(token)
	if sessionErr != nil || username == "" {
		return nil, rejectFailedValidation(sessionErr)
	}
	ctx = context.WithValue(ctx, UsernameContextKey, username)
	ctx = context.WithValue(ctx, IsAPITokenContextKey, false)

	// Get user ID and superuser status for RBAC. A session whose user
	// does not load is rejected rather than returned as a partial
	// context: a username with no user ID still satisfies the ownership
	// comparison in CanAccessConnection, so degrading here would
	// authenticate a caller whose privileges were never verified. The
	// cost of that choice is that a database failure logs sessions out
	// instead of quietly narrowing what they can do.
	user, userErr := store.GetUser(username)
	if userErr != nil || user == nil {
		return nil, rejectUnloadableUser("session", userErr)
	}
	ctx = context.WithValue(ctx, UserIDContextKey, user.ID)
	ctx = context.WithValue(ctx, IsSuperuserContextKey, user.IsSuperuser)
	return ctx, nil
}
