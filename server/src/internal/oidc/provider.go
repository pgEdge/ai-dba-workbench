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
	"time"

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

	// DisplayName is the configured display name claim, or empty when
	// the claim is absent, unconfigured or was dropped. It is cosmetic:
	// nothing may key an authorization decision on it.
	DisplayName string

	// Email is the standard "email" claim. An empty value means the
	// address is unknown, either because the provider sent none or
	// because the one it sent was unusable (EmailRejected then says
	// which). A consumer that gates on the address, such as the
	// allowed_email_domains check, must treat empty as "no permitted
	// domain" and refuse, never as "no restriction applies": a user who
	// controls their own profile on a shared provider could otherwise
	// empty the field deliberately to slip past the check.
	Email string

	// EmailRejected is true when the provider sent an email claim that
	// this package refused (too long, or carrying a forbidden
	// character), as opposed to sending none at all.
	EmailRejected bool

	// EmailVerified is the standard "email_verified" claim, and is false
	// whenever the provider did not send it. A consumer that gates on
	// the address, such as the allowed_email_domains check, must require
	// it: on a provider where users type their own address, an
	// unverified "someone@yourcompany.example" is an assertion by the
	// attacker, not by the identity provider, so honoring it would turn
	// the domain allowlist into a formality.
	EmailVerified bool

	// Groups holds the configured groups claim, normalized to a slice of
	// strings. It is nil when the claim is absent or not configured.
	Groups []string

	// GroupsClaimPresent reports whether the token carried the configured
	// groups claim at all, with a non-null value of any shape. It is what
	// tells "the provider sent an empty list" (the user is in no groups)
	// apart from "the provider left the claim out" (it said nothing about
	// groups), which Groups alone cannot: both leave Groups nil. A session
	// refresh relies on the distinction, because many providers omit the
	// groups claim from a refreshed ID token, and that silence must not
	// be read as the user having left every group. It is always false
	// when no groups claim is configured.
	GroupsClaimPresent bool

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

	// DroppedClaims lists, by NAME, the string claims the provider sent
	// that had to be dropped. It never holds the values themselves,
	// which are the untrusted part; the caller logs the names so that an
	// operator can see which claim its provider is sending in a form the
	// Workbench cannot use.
	DroppedClaims []string
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
// and "offline_access" appended, each only if the operator left it out.
//
// Without "openid" the provider is under no obligation to return an ID
// token at all, and the whole flow depends on one. "offline_access" asks
// for a refresh token, which is what lets a federated session outlive
// its first ID token: the session ends when that ID token expires, and
// is extended only by redeeming the refresh token, which the provider
// refuses once the user has been removed or disabled there (issue #742).
// A provider that will not issue one simply returns none, and the
// session then ends at the ID token's expiry.
func requestedScopes(configured []string) []string {
	scopes := slices.Clone(configured)
	if !slices.Contains(scopes, oidc.ScopeOpenID) {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}
	if !slices.Contains(scopes, oidc.ScopeOfflineAccess) {
		scopes = append(scopes, oidc.ScopeOfflineAccess)
	}
	return scopes
}

// Tokens is what a code exchange or a refresh yields: the verified
// identity, if the provider sent an ID token, the refresh token to
// redeem next, and when the session they support expires.
//
// RefreshToken is a long-lived credential. It must be held in memory
// only, never written to disk, and never logged; nothing in this package
// formats it into an error.
type Tokens struct {
	// Identity is the verified end user. Exchange always sets it; Refresh
	// sets it only when the provider returned an ID token, which it is
	// not obliged to do on a refresh.
	Identity *Identity

	// RefreshToken is empty when the provider issued none. After a
	// refresh that did not rotate it, it holds the token that was
	// redeemed, which remains valid.
	RefreshToken string

	// Expiry is the ID token's expiry when the response carried one, and
	// otherwise the access token's. It is never zero on success.
	Expiry time.Time
}

// AuthCodeURL returns the URL to send the browser to in order to start a
// login, carrying the state's opaque state value, its nonce, and the
// PKCE S256 code challenge derived from its code verifier.
//
// extra holds the operator's http.auth.oidc.auth_params, passed per call
// rather than fixed at construction so that a reload applies them.
// Configuration validation already refuses a parameter the Workbench
// sets itself; any that arrives here regardless is dropped, so that no
// path through the configuration can replace the state, nonce or PKCE
// challenge. They are added in sorted order, so the URL is stable.
func (p *Provider) AuthCodeURL(state *LoginState, extra map[string]string) string {
	names := make([]string, 0, len(extra))
	for name := range extra {
		if !config.IsReservedOIDCAuthParam(name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)

	opts := make([]oauth2.AuthCodeOption, 0, len(names)+3)
	for _, name := range names {
		opts = append(opts, oauth2.SetAuthURLParam(name, extra[name]))
	}
	opts = append(opts,
		oidc.Nonce(state.Nonce),
		oauth2.SetAuthURLParam("code_challenge", state.CodeChallenge()),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	return p.oauth2Config.AuthCodeURL(state.State, opts...)
}

// Exchange redeems an authorization code for tokens and returns the
// verified Identity it describes, together with any refresh token the
// provider issued and the ID token's expiry.
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
// No error returned by Exchange may reach a browser. Every one of them
// can carry provider-supplied text: a token endpoint failure wraps an
// *oauth2.RetrieveError embedding the raw response body, the
// verification failures quote claim values back (the audience and issuer
// this token actually carried), and the username failure describes the
// offending value. The handler must render a fixed, generic
// login-failed message and log the detail.
//
// That the log is where these errors end up is also why the
// control-character rejection in safeClaimValue is load bearing rather
// than cosmetic: an error message carrying provider-influenced text goes
// into the server log, and a value with an interior newline in it would
// write what looks like a second log record.
func (p *Provider) Exchange(ctx context.Context, code string, state *LoginState) (*Tokens, error) {
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

	identity, err := p.identityFromIDToken(idToken)
	if err != nil {
		return nil, err
	}

	return &Tokens{
		Identity:     identity,
		RefreshToken: token.RefreshToken,
		Expiry:       idToken.Expiry,
	}, nil
}

// Refresh redeems a refresh token and returns what the provider sent
// back: the refresh token to use next (rotated or not), the new expiry,
// and the verified identity when the response carried an ID token.
//
// An ID token returned by a refresh is verified exactly as one from a
// code exchange is (signature, issuer, audience, expiry) except for the
// nonce, which a refresh request does not carry. The caller must still
// check that its issuer and subject are the ones the session belongs to,
// as OpenID Connect Core 1.0 section 12.2 requires; Refresh has no
// session to compare against.
//
// A provider that answers without an ID token leaves the session's
// expiry to the access token's, and a response carrying neither expiry
// is refused, because a session must end at some point the provider
// chose.
//
// As with Exchange, the caller must pass a context carrying a timeout,
// and no error returned here may reach a browser: a token endpoint
// failure embeds the provider's response body. None of them carries the
// refresh token.
func (p *Provider) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("no refresh token to redeem")
	}

	// A token with no access token is never valid, so the token source
	// goes straight to the refresh grant. When the provider does not
	// rotate the refresh token, oauth2 carries the redeemed one over
	// into the result.
	token, err := p.oauth2Config.TokenSource(ctx,
		&oauth2.Token{RefreshToken: refreshToken}).Token()
	if err != nil {
		return nil, fmt.Errorf("failed to redeem refresh token: %w", err)
	}

	result := &Tokens{RefreshToken: token.RefreshToken, Expiry: token.Expiry}

	if rawIDToken, ok := token.Extra("id_token").(string); ok && rawIDToken != "" {
		idToken, err := p.verifier.Verify(ctx, rawIDToken)
		if err != nil {
			return nil, fmt.Errorf("failed to verify refreshed ID token: %w", err)
		}
		identity, err := p.identityFromIDToken(idToken)
		if err != nil {
			return nil, err
		}
		result.Identity = identity
		result.Expiry = idToken.Expiry
	}

	if result.Expiry.IsZero() {
		return nil, fmt.Errorf("identity provider refresh response carried no expiry")
	}
	return result, nil
}

// identityFromIDToken decodes a verified ID token's claims into an
// Identity.
func (p *Provider) identityFromIDToken(idToken *oidc.IDToken) (*Identity, error) {
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to decode ID token claims: %w", err)
	}

	return p.identityFromClaims(idToken.Issuer, idToken.Subject, claims)
}
