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
	"fmt"
	"log"
	"strconv"

	"github.com/pgedge/ai-workbench/server/internal/logging"
)

// An API token is checked against the server's own database alone, so a
// token owned by a user who signs in through an identity provider would
// outlive that user's removal at the provider, which a federated session
// does not (see federated_session.go). Unless the http.auth.oidc
// allow_api_tokens setting says otherwise, accounts whose auth_source is
// not local therefore may not own API tokens: none can be created for
// them, and any they already own, including one created before a local
// account was linked to an identity provider, is refused when presented.
// Automation should use a local service account instead, which this does
// not affect (issue #742).

// ErrFederatedAPITokensDisabled reports a token refused because its owner
// signs in through an identity provider and allow_api_tokens is off.
var ErrFederatedAPITokensDisabled = errors.New(
	"API tokens are disabled for federated logins in this installation")

// FederatedAPITokenRefusalReason is the reason recorded on, and returned
// with, a refusal for ErrFederatedAPITokensDisabled.
const FederatedAPITokenRefusalReason = "Permission denied: API tokens are " +
	"disabled for federated logins in this installation. Contact your " +
	"system administrator."

// tokenUseAction is the audit action a refused presentation of a token
// is recorded under.
const tokenUseAction = "token.use"

// SetFederatedAPITokenPolicy registers the function that reports whether
// accounts signing in through an identity provider may own API tokens.
// It is called on every token creation and validation, so that a
// configuration reload takes effect at once; it must be cheap, safe for
// concurrent use and must not call back into the store. nil, the
// default, means they may not.
func (s *AuthStore) SetFederatedAPITokenPolicy(allowed func() bool) {
	if allowed == nil {
		s.federatedAPITokens.Store(nil)
		return
	}
	s.federatedAPITokens.Store(&allowed)
}

// FederatedAPITokensAllowed reports whether accounts signing in through
// an identity provider may currently own API tokens.
func (s *AuthStore) FederatedAPITokensAllowed() bool {
	allowed := s.federatedAPITokens.Load()
	return allowed != nil && (*allowed)()
}

// APITokensAllowedFor reports whether an account with this auth_source
// may currently own API tokens: a local account always may, and any
// other only while allow_api_tokens is on.
func (s *AuthStore) APITokensAllowedFor(authSource string) bool {
	return authSource == AuthSourceLocal || s.FederatedAPITokensAllowed()
}

// UserMayOwnAPITokens reports whether the named user may currently own
// API tokens. An unknown user may not.
func (s *AuthStore) UserMayOwnAPITokens(username string) (bool, error) {
	user, err := s.GetUser(username)
	if err != nil {
		return false, err
	}
	if user == nil {
		return false, nil
	}
	return s.APITokensAllowedFor(user.AuthSource), nil
}

// federatedTokenRefusal identifies a token refused because its owner is
// federated, so that the refusal can be audited once the store's read
// lock has been released.
type federatedTokenRefusal struct {
	tokenID int64
	owner   string
}

// ValidateToken checks if a token is valid, returning the token's info
// if it is. A token owned by an account that signs in through an
// identity provider is refused, and the refusal audited as a denial of
// token.use, while allow_api_tokens is off.
func (s *AuthStore) ValidateToken(rawToken string) (*StoredToken, error) {
	token, refusal, err := s.validateToken(rawToken)
	if refusal != nil {
		s.recordFederatedTokenRefusal(*refusal)
	}
	return token, err
}

// recordFederatedTokenRefusal audits a refused presentation of a token
// owned by a federated account. The denial coalescer keeps a client
// retrying such a token from flooding the log.
func (s *AuthStore) recordFederatedTokenRefusal(refusal federatedTokenRefusal) {
	tokenID := refusal.tokenID
	actor := Actor{Type: ActorToken, ID: &tokenID, Name: refusal.owner}
	if err := s.RecordCoalescedDenial(actor, tokenUseAction,
		"tokens/"+strconv.FormatInt(tokenID, 10),
		FederatedAPITokenRefusalReason); err != nil {
		//nolint:gosec // G706: error text passed through logging.SanitizeForLog
		log.Printf("[AUTH] Failed to audit a refused token owned by a federated account: %s",
			logging.SanitizeForLog(err.Error()))
	}
}

// checkTokenOwner reads the owner of a token being validated, refusing a
// missing or disabled owner and, while allow_api_tokens is off, one that
// signs in through an identity provider. The caller holds s.mu for
// reading.
func (s *AuthStore) checkTokenOwner(token *StoredToken) (*federatedTokenRefusal, error) {
	var enabled bool
	var authSource, owner string
	err := s.db.QueryRow(
		"SELECT enabled, auth_source, username FROM users WHERE id = ?",
		token.OwnerID).Scan(&enabled, &authSource, &owner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		//nolint:gosec // G706: error text passed through logging.SanitizeForLog
		log.Printf("[AUTH] Token validation failed: the token owner could not be read: %s",
			logging.SanitizeForLog(err.Error()))
		return nil, fmt.Errorf("token validation error: %w: %w", ErrAuthStoreUnavailable, err)
	}
	if err != nil || !enabled {
		return nil, fmt.Errorf("token owner is disabled")
	}
	if !s.APITokensAllowedFor(authSource) {
		return &federatedTokenRefusal{tokenID: token.ID, owner: owner},
			fmt.Errorf("token refused: %w", ErrFederatedAPITokensDisabled)
	}
	return nil, nil
}
