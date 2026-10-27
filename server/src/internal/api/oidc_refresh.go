/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/logging"
)

// errFederatedLoginOff refuses a refresh while federated login is
// switched off, which a reload can do to a running server.
var errFederatedLoginOff = errors.New("federated login is switched off")

// RefreshFederatedSession implements auth.FederatedSessionRefresher. The
// auth store calls it when a federated session's ID token has expired,
// and ends the session on any error (issue #742).
//
// The identity provider decides first: it refuses the refresh token once
// the user has been removed or disabled there, or the grant revoked.
// When it agrees and returns a new ID token, that token must name the
// same subject at the same issuer the account is linked to and pass the
// same email domain policy a login does, so that a refresh can never
// confer what a fresh login would not. The account must still exist, be
// enabled, and be federated.
//
// Groups and the superuser flag are reconciled from the new ID token only
// when it actually carries the configured groups claim. Many providers
// leave that claim out of a refreshed ID token, and some send no ID token
// on refresh at all; reading either silence as "the user is in no groups"
// would strip every mapped membership an hour into each session. In both
// cases the memberships and flag set at login stand until the provider
// says otherwise, or refuses the refresh outright.
//
// Its errors are logged by the store; none of them carries the refresh
// token, and a provider's own error text is cleaned for the log there.
func (h *OIDCHandler) RefreshFederatedSession(ctx context.Context,
	username, refreshToken string) (auth.FederatedRefresh, error) {

	cfg := h.currentConfig()
	if !h.enabledUnder(cfg) {
		return auth.FederatedRefresh{}, errFederatedLoginOff
	}

	// The account is checked before the provider is asked, so that a
	// session whose account has gone or been disabled here ends without
	// a call to the provider.
	user, err := h.authStore.GetUser(username)
	if err != nil {
		return auth.FederatedRefresh{}, fmt.Errorf("the account could not be read: %w", err)
	}
	if user == nil {
		return auth.FederatedRefresh{}, errors.New("the account no longer exists")
	}
	if !user.Enabled {
		return auth.FederatedRefresh{}, errors.New("the account is disabled")
	}
	if user.AuthSource != auth.AuthSourceOIDC || user.IsServiceAccount {
		return auth.FederatedRefresh{}, errors.New("the account is no longer federated")
	}

	tokens, err := h.provider.Refresh(ctx, refreshToken)
	if err != nil {
		return auth.FederatedRefresh{}, err
	}

	if identity := tokens.Identity; identity != nil {
		logIdentityDiagnostics(identity)
		if auth.ExternalSubjectKey(identity.Issuer, identity.Subject) != user.ExternalSubject {
			return auth.FederatedRefresh{}, fmt.Errorf(
				"the refreshed ID token names subject %s, which is not the one the account is linked to",
				logging.SanitizeForLog(identity.Subject))
		}
		if !emailDomainAllowed(cfg, identity) {
			return auth.FederatedRefresh{}, errors.New(
				"the refreshed ID token's email address is not in an allowed domain")
		}
		if identity.GroupsClaimPresent {
			if err := h.authStore.ReconcileFederatedGroups(user.ID,
				federatedIdentity(identity), federationOptions(cfg)); err != nil {
				return auth.FederatedRefresh{}, fmt.Errorf("group reconciliation failed: %w", err)
			}
		}
	}

	return auth.FederatedRefresh{
		RefreshToken: tokens.RefreshToken,
		ExpiresAt:    tokens.Expiry,
	}, nil
}
