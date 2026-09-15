/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package oidc

import (
	"context"
	"crypto/subtle"
	"fmt"
	"slices"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// Identity is a verified end user, as described by the claims of an ID
// token the configured identity provider signed. It is the only thing
// the rest of the Workbench sees of the OIDC protocol: everything
// downstream (user provisioning, group mapping, session creation) works
// from this struct rather than from raw claims.
type Identity struct {
	// Issuer and Subject together are the identity provider's permanent,
	// unique handle for this user. Neither is safe to use on its own:
	// Subject is only unique within an issuer.
	Issuer  string
	Subject string

	// Username is the value of the configured username claim. It is
	// never empty: Exchange fails rather than return an Identity with
	// nothing to key a Workbench user on.
	Username string

	DisplayName string
	Email       string

	// Groups holds the configured groups claim, normalized to a slice of
	// strings. It is nil when the claim is absent or not configured.
	Groups []string

	// SkippedGroups counts the values in the groups claim that could not
	// be used: elements that were not strings, and strings that were too
	// long or carried control characters. A non-zero count is not an
	// error (see stringsClaim), but it means the user is being
	// authorized on fewer groups than the provider sent, so the caller
	// should log it once per login.
	SkippedGroups int

	// UnexpectedGroupsClaimShape names the Go type of a groups claim
	// that was neither an array nor a string, and is empty whenever the
	// shape was one this package understands. Like SkippedGroups it is
	// diagnostic: the login proceeds with no groups.
	UnexpectedGroupsClaimShape string
}

// Provider wraps the OpenID Connect protocol for one configured identity
// provider: discovery, the authorization URL, the authorization code
// exchange, ID token verification, and the mapping from verified claims
// to an Identity.
//
// A Provider is safe for concurrent use and is intended to be
// constructed once, at server start-up, and shared.
type Provider struct {
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier

	usernameClaim    string
	displayNameClaim string
	groupsClaim      string
}

// defaultUsernameClaim is used when the configuration leaves the
// username claim unset. Configuration defaults normally supply it; this
// is a backstop so that a Provider built from a hand-made config cannot
// silently fail every login for want of a claim name. The display name
// and groups claims have no such backstop: leaving either unset means
// "do not extract this", which is a legitimate choice.
const defaultUsernameClaim = "email"

// NewProvider performs OpenID Connect discovery against cfg.Issuer and
// returns a Provider ready to drive logins.
//
// Discovery is a network call made with the supplied context, and the
// underlying library issues it through http.DefaultClient, which has no
// timeout of its own. The caller must therefore pass a context carrying
// an explicit timeout: without one, an identity provider that accepts
// the connection and then says nothing stalls server start-up
// indefinitely.
//
// Discovery being a network call is also why NewProvider must not be
// called at package initialization. The server constructs it during start-up and
// treats a failure as fatal, in the same way it treats an unusable TLS
// certificate: an identity provider that cannot be reached at start-up
// means no one can log in, and failing loudly beats serving a login page
// whose button does not work.
func NewProvider(ctx context.Context, cfg config.OIDCConfig) (*Provider, error) {
	if cfg.Issuer == "" {
		return nil, fmt.Errorf("OIDC issuer is not configured")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("OIDC client ID is not configured")
	}

	discovered, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery against %s failed: %w", cfg.Issuer, err)
	}

	usernameClaim := cfg.UsernameClaim
	if usernameClaim == "" {
		usernameClaim = defaultUsernameClaim
	}

	return &Provider{
		oauth2Config: oauth2.Config{
			ClientID: cfg.ClientID,
			// A secret supplied through client_secret_file does not live
			// in cfg.ClientSecret, so that saving the configuration
			// cannot write it back to disk in plaintext; the effective
			// value has to be read through this accessor.
			ClientSecret: cfg.EffectiveClientSecret(),
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     discovered.Endpoint(),
			Scopes:       requestedScopes(cfg.Scopes),
		},
		verifier: discovered.Verifier(&oidc.Config{ClientID: cfg.ClientID}),

		usernameClaim:    usernameClaim,
		displayNameClaim: cfg.DisplayNameClaim,
		groupsClaim:      cfg.GroupsClaim,
	}, nil
}

// requestedScopes returns the configured scopes with "openid" prepended
// if the operator left it out. Without that scope the provider is under
// no obligation to return an ID token at all, and the whole flow depends
// on one.
func requestedScopes(configured []string) []string {
	if slices.Contains(configured, oidc.ScopeOpenID) {
		return slices.Clone(configured)
	}
	return append([]string{oidc.ScopeOpenID}, configured...)
}

// AuthCodeURL returns the URL to send the browser to in order to start a
// login, carrying the state's opaque state value, its nonce, and the
// PKCE S256 code challenge derived from its code verifier.
func (p *Provider) AuthCodeURL(state *LoginState) string {
	return p.oauth2Config.AuthCodeURL(
		state.State,
		oidc.Nonce(state.Nonce),
		oauth2.SetAuthURLParam("code_challenge", state.CodeChallenge()),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

// Exchange redeems an authorization code for tokens and returns the
// verified Identity it describes.
//
// It proves four separate things before returning: that the provider
// will redeem the code against the PKCE code verifier held in state (so
// the code cannot be replayed by whoever intercepted the redirect); that
// the ID token carries a valid signature from a key the provider
// publishes, was issued by the configured issuer, is addressed to this
// client and has not expired (all of which the verifier checks); that
// its nonce matches the one this login started with (so an ID token
// captured from another login cannot be injected into this one); and
// that it carries a usable username.
//
// Both the token request and any JWKS fetch go through
// http.DefaultClient, which has no timeout, so the caller must pass a
// context carrying an explicit timeout; otherwise a slow or hostile
// provider holds the callback goroutine open for as long as it likes.
//
// The returned error must never be shown to a browser. A token endpoint
// failure wraps an *oauth2.RetrieveError, which embeds the provider's
// raw response body; that belongs in the server log, whilst the user
// belongs on a generic login-failed page.
func (p *Provider) Exchange(ctx context.Context, code string, state *LoginState) (*Identity, error) {
	token, err := p.oauth2Config.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", state.CodeVerifier))
	if err != nil {
		return nil, fmt.Errorf("failed to exchange authorization code: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, fmt.Errorf("identity provider response did not include an ID token")
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	// A zero-length nonce would make the comparison below succeed
	// against a token carrying no nonce claim at all, since
	// ConstantTimeCompare returns 1 for two empty slices: exactly the
	// replay this check exists to stop. OpenState already refuses a
	// state whose nonce is not the full encoded length, but that is a
	// different file enforcing it for a different stated reason, so the
	// invariant is restated here, next to the check that depends on it.
	if state.Nonce == "" {
		return nil, fmt.Errorf("login state carries no nonce")
	}

	// Constant time, not "==": the nonce is a secret this server issued,
	// and comparing it with an early-exit comparison leaks how much of a
	// guess was correct to anyone who can time the response.
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(state.Nonce)) != 1 {
		return nil, fmt.Errorf("ID token nonce does not match the login request")
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to decode ID token claims: %w", err)
	}

	return p.identityFromClaims(idToken.Issuer, idToken.Subject, claims)
}
