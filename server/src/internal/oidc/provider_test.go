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
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/oidc/oidctest"
)

// newTestProvider builds a Provider pointed at the fake identity
// provider, filling in the issuer, client credentials and redirect URL
// so that each test only has to state the claim configuration it cares
// about.
func newTestProvider(t *testing.T, idp *oidctest.FakeIDP, cfg config.OIDCConfig) *Provider {
	t.Helper()

	cfg.Enabled = true
	cfg.Issuer = idp.Issuer()
	cfg.ClientID = idp.ClientID()
	if cfg.ClientSecret == "" {
		cfg.ClientSecret = "test-client-secret"
	}
	if cfg.RedirectURL == "" {
		cfg.RedirectURL = "https://workbench.example.com/api/auth/oidc/callback"
	}

	provider, err := NewProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return provider
}

// newTestLoginState returns a fresh LoginState, failing the test if the
// system random source is unavailable.
func newTestLoginState(t *testing.T) *LoginState {
	t.Helper()

	state, err := NewLoginState("/")
	if err != nil {
		t.Fatalf("NewLoginState: %v", err)
	}
	return state
}

// exchangeMustFail runs an exchange that is expected to fail and asserts
// on what the error says, so that a test named for one rejection cannot
// quietly pass because a different thing broke.
func exchangeMustFail(t *testing.T, provider *Provider, state *LoginState, want string) {
	t.Helper()

	identity, err := provider.Exchange(context.Background(), "code", state)
	if err == nil {
		t.Fatalf("expected an error mentioning %q, got identity %+v", want, identity)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to mention %q", err, want)
	}
}

func TestExchangeReturnsIdentityFromClaims(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{
		UsernameClaim: "email", DisplayNameClaim: "name", GroupsClaim: "groups",
	})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"sub":    "subject-1",
		"email":  "jane.doe@example.com",
		"name":   "Jane Doe",
		"groups": []string{"idp-database-admins"},
		"nonce":  state.Nonce,
	}))

	identity, err := provider.Exchange(context.Background(), "code", state)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if identity.Issuer != idp.Issuer() {
		t.Errorf("issuer = %q, want %q", identity.Issuer, idp.Issuer())
	}
	if identity.Subject != "subject-1" {
		t.Errorf("subject = %q, want subject-1", identity.Subject)
	}
	if identity.Username != "jane.doe@example.com" {
		t.Errorf("username = %q", identity.Username)
	}
	if identity.DisplayName != "Jane Doe" {
		t.Errorf("display name = %q", identity.DisplayName)
	}
	if identity.Email != "jane.doe@example.com" {
		t.Errorf("email = %q", identity.Email)
	}
	if !reflect.DeepEqual(identity.Groups, []string{"idp-database-admins"}) {
		t.Errorf("groups = %v", identity.Groups)
	}
	if identity.SkippedGroups != 0 || identity.UnexpectedGroupsClaimShape != "" {
		t.Errorf("unexpected groups diagnostics: %d skipped, shape %q",
			identity.SkippedGroups, identity.UnexpectedGroupsClaimShape)
	}
}

// TestExchangeSendsThePKCECodeVerifier proves the code exchange presents
// the verifier held in the login state, without which the code
// challenge sent at the start of the login proves nothing. The
// authorization server is what actually enforces the pairing, so what
// matters here is that we send the verifier we committed to.
func TestExchangeSendsThePKCECodeVerifier(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"email": "jane.doe@example.com",
		"nonce": state.Nonce,
	}))

	if _, err := provider.Exchange(context.Background(), "the-code", state); err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	form := idp.LastTokenRequestForm()
	if got := form.Get("code_verifier"); got != state.CodeVerifier {
		t.Errorf("code_verifier = %q, want %q", got, state.CodeVerifier)
	}
	if got := form.Get("code"); got != "the-code" {
		t.Errorf("code = %q, want the-code", got)
	}
}

func TestExchangeRejectsNonceMismatch(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"sub": "subject-1", "email": "jane.doe@example.com", "nonce": "not-the-nonce",
	}))

	exchangeMustFail(t, provider, state, "nonce does not match")
}

// TestExchangeRejectsMissingNonce covers the token that carries no nonce
// at all, which must be refused for the same reason a mismatched one is.
func TestExchangeRejectsMissingNonce(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"email": "jane.doe@example.com",
	}))

	exchangeMustFail(t, provider, state, "nonce does not match")
}

// TestExchangeRejectsAnEmptyStateNonce is the belt to OpenState's
// braces. subtle.ConstantTimeCompare returns 1 for two zero-length
// slices, so a login state whose nonce is empty would accept a token
// with no nonce claim, which is precisely the replay the nonce exists to
// stop. Exchange refuses the state outright rather than relying on a
// length check enforced in another file for another reason.
func TestExchangeRejectsAnEmptyStateNonce(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	state.Nonce = ""
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"email": "jane.doe@example.com",
	}))

	exchangeMustFail(t, provider, state, "carries no nonce")
}

func TestExchangeRejectsWrongAudience(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"aud": "some-other-client", "email": "jane.doe@example.com", "nonce": state.Nonce,
	}))

	exchangeMustFail(t, provider, state, "audience")
}

func TestExchangeRejectsExpiredToken(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"exp":   time.Now().Add(-time.Hour).Unix(),
		"email": "jane.doe@example.com",
		"nonce": state.Nonce,
	}))

	exchangeMustFail(t, provider, state, "expired")
}

func TestExchangeRejectsTokenSignedByAnotherKey(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.SignWithForeignKey(t, map[string]any{
		"email": "jane.doe@example.com", "nonce": state.Nonce,
	}))

	exchangeMustFail(t, provider, state, "signature")
}

func TestExchangeRejectsWrongIssuer(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"iss":   "https://other.example.com",
		"email": "jane.doe@example.com",
		"nonce": state.Nonce,
	}))

	exchangeMustFail(t, provider, state, "issued by a different provider")
}

func TestExchangeRejectsMissingUsernameClaim(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"sub": "subject-1", "nonce": state.Nonce,
	}))

	exchangeMustFail(t, provider, state, `no usable "email" claim`)
}

// TestExchangeRejectsEmptyUsernameClaim covers the claim that is present
// but holds only whitespace, which is as unusable as an absent one.
func TestExchangeRejectsEmptyUsernameClaim(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"email": "   ", "nonce": state.Nonce,
	}))

	exchangeMustFail(t, provider, state, `no usable "email" claim`)
}

// TestExchangeRejectsAUsernameTheLocalStoreWouldRefuse proves a
// federated login cannot mint a username that "-add-user" would have
// turned away: the same validator governs both.
func TestExchangeRejectsAUsernameTheLocalStoreWouldRefuse(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	cases := map[string]string{
		"leading punctuation": "-jane.doe@example.com",
		"invalid character":   "jane doe@example.com",
		"too long":            strings.Repeat("a", 129),
	}

	for name, username := range cases {
		t.Run(name, func(t *testing.T) {
			idp := oidctest.NewFakeIDP(t)
			provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

			state := newTestLoginState(t)
			idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
				"email": username, "nonce": state.Nonce,
			}))

			exchangeMustFail(t, provider, state, "not a usable username")
		})
	}

	// A plain address remains acceptable, so the validator has not been
	// tightened into rejecting the normal case.
	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"email": "jane.doe@example.com", "nonce": state.Nonce,
	}))
	if _, err := provider.Exchange(context.Background(), "code", state); err != nil {
		t.Fatalf("Exchange with an ordinary address: %v", err)
	}
}

// TestExchangeDropsUnsafeDisplayNames proves a value that is too long or
// carries control or format characters never reaches the Identity, and
// that the login still succeeds: the display name is not load bearing.
func TestExchangeDropsUnsafeDisplayNames(t *testing.T) {
	cases := map[string]string{
		"embedded newline":    "Jane\nDoe",
		"carriage return":     "Jane\rDoe",
		"NUL byte":            "Jane\x00Doe",
		"DEL":                 "Jane\x7fDoe",
		"C1 control":          "Jane\u0085Doe",
		"line separator":      "Jane\u2028Doe",
		"paragraph separator": "Jane\u2029Doe",
		"over the length cap": strings.Repeat("x", maxClaimValueLength+1),
	}

	for name, displayName := range cases {
		t.Run(name, func(t *testing.T) {
			idp := oidctest.NewFakeIDP(t)
			provider := newTestProvider(t, idp, config.OIDCConfig{
				UsernameClaim: "email", DisplayNameClaim: "name",
			})

			state := newTestLoginState(t)
			idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
				"email": "jane.doe@example.com",
				"name":  displayName,
				"nonce": state.Nonce,
			}))

			identity, err := provider.Exchange(context.Background(), "code", state)
			if err != nil {
				t.Fatalf("Exchange: %v", err)
			}
			if identity.DisplayName != "" {
				t.Errorf("display name = %q, want it dropped", identity.DisplayName)
			}
			if !slices.Contains(identity.DroppedClaims, "name") {
				t.Errorf("DroppedClaims = %v, want it to name the display name claim",
					identity.DroppedClaims)
			}
			for _, claimName := range identity.DroppedClaims {
				if strings.Contains(claimName, "Jane") || strings.Contains(claimName, "x") {
					t.Errorf("DroppedClaims leaked a value: %q", claimName)
				}
			}
		})
	}
}

// TestExchangeKeepsDisplayNamesWithFormatCharacters is the other half of
// the rule: a zero-width joiner, and a bidirectional mark, appear in
// names that are legitimately spelled that way, and a cosmetic field
// must never be the reason a login fails. Format characters cannot
// inject a log line, so they survive intact in a display name. The
// username is the exception and is governed by auth.ValidateUsername.
func TestExchangeKeepsDisplayNamesWithFormatCharacters(t *testing.T) {
	cases := map[string]string{
		"zero-width joiner":     "Jane\u200dDoe",
		"zero-width non-joiner": "Jane\u200cDoe",
		"left-to-right mark":    "Jane\u200eDoe",
	}

	for name, displayName := range cases {
		t.Run(name, func(t *testing.T) {
			idp := oidctest.NewFakeIDP(t)
			provider := newTestProvider(t, idp, config.OIDCConfig{
				UsernameClaim: "email", DisplayNameClaim: "name",
			})

			state := newTestLoginState(t)
			idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
				"email": "jane.doe@example.com",
				"name":  displayName,
				"nonce": state.Nonce,
			}))

			identity, err := provider.Exchange(context.Background(), "code", state)
			if err != nil {
				t.Fatalf("Exchange: %v", err)
			}
			if identity.DisplayName != displayName {
				t.Errorf("display name = %q, want it kept intact as %q",
					identity.DisplayName, displayName)
			}
		})
	}
}

// TestExchangeRejectsAFailedCodeRedemption covers the token endpoint
// refusing the authorization code, which the fake does whenever no ID
// token has been staged.
func TestExchangeRejectsAFailedCodeRedemption(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	exchangeMustFail(t, provider, newTestLoginState(t), "failed to exchange authorization code")
}

// TestExchangeRejectsAResponseWithNoIDToken covers a token endpoint that
// answers successfully but omits the ID token entirely, which a plain
// OAuth 2.0 (rather than OpenID Connect) endpoint would do.
func TestExchangeRejectsAResponseWithNoIDToken(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	idp.SetNextResponseOmitsIDToken()

	exchangeMustFail(t, provider, newTestLoginState(t), "did not include an ID token")
}

func TestAuthCodeURLCarriesStateNonceAndPKCE(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	parsed, err := url.Parse(provider.AuthCodeURL(state))
	if err != nil {
		t.Fatalf("AuthCodeURL: %v", err)
	}

	query := parsed.Query()
	if query.Get("state") != state.State {
		t.Errorf("state = %q, want %q", query.Get("state"), state.State)
	}
	if query.Get("nonce") != state.Nonce {
		t.Errorf("nonce = %q, want %q", query.Get("nonce"), state.Nonce)
	}
	if query.Get("code_challenge") != state.CodeChallenge() {
		t.Errorf("code_challenge = %q, want %q",
			query.Get("code_challenge"), state.CodeChallenge())
	}
	if query.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256",
			query.Get("code_challenge_method"))
	}
	if query.Get("client_id") != idp.ClientID() {
		t.Errorf("client_id = %q, want %q", query.Get("client_id"), idp.ClientID())
	}
	if query.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", query.Get("response_type"))
	}
}

func TestGroupsClaimAcceptsTheShapesProvidersActuallySend(t *testing.T) {
	cases := map[string]struct {
		claim         any
		want          []string
		wantSkipped   int
		wantShapeHint string
	}{
		// Note that "array of strings" reaches the provider as an array
		// of any: the claim goes through a JSON round trip. The native
		// []string branch is covered directly by
		// TestStringsClaimAcceptsANativeStringSlice.
		"array of strings": {claim: []string{"a", "b"}, want: []string{"a", "b"}},
		"array of any":     {claim: []any{"a", "b"}, want: []string{"a", "b"}},
		"single string":    {claim: "a", want: []string{"a"}},
		"absent":           {claim: nil},
		"mixed array":      {claim: []any{"a", 42, "b"}, want: []string{"a", "b"}, wantSkipped: 1},
		"unsafe member":    {claim: []any{"a", "b\nc"}, want: []string{"a"}, wantSkipped: 1},
		"wrong shape":      {claim: map[string]any{"a": true}, wantShapeHint: "map["},
		"empty array":      {claim: []any{}},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			// A fresh fake per subtest, so that staging a token is never
			// a race even if these subtests later run in parallel.
			idp := oidctest.NewFakeIDP(t)
			provider := newTestProvider(t, idp, config.OIDCConfig{
				UsernameClaim: "email", GroupsClaim: "groups",
			})

			state := newTestLoginState(t)
			claims := map[string]any{
				"email": "jane.doe@example.com",
				"nonce": state.Nonce,
			}
			if testCase.claim != nil {
				claims["groups"] = testCase.claim
			}
			idp.SetNextIDToken(idp.MintIDToken(t, claims))

			identity, err := provider.Exchange(context.Background(), "code", state)
			if err != nil {
				t.Fatalf("Exchange: %v", err)
			}
			if !slices.Equal(identity.Groups, testCase.want) {
				t.Errorf("groups = %#v, want %#v", identity.Groups, testCase.want)
			}
			if identity.SkippedGroups != testCase.wantSkipped {
				t.Errorf("skipped = %d, want %d", identity.SkippedGroups, testCase.wantSkipped)
			}
			if testCase.wantShapeHint == "" {
				if identity.UnexpectedGroupsClaimShape != "" {
					t.Errorf("shape = %q, want none", identity.UnexpectedGroupsClaimShape)
				}
			} else if !strings.Contains(identity.UnexpectedGroupsClaimShape, testCase.wantShapeHint) {
				t.Errorf("shape = %q, want it to mention %q",
					identity.UnexpectedGroupsClaimShape, testCase.wantShapeHint)
			}
		})
	}
}

// TestGroupsClaimNotConfigured proves that leaving groups_claim unset
// means "do not extract groups", even when the token carries a claim
// called "groups".
func TestGroupsClaimNotConfigured(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"email":  "jane.doe@example.com",
		"groups": []string{"idp-database-admins"},
		"nonce":  state.Nonce,
	}))

	identity, err := provider.Exchange(context.Background(), "code", state)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if identity.Groups != nil {
		t.Errorf("groups = %v, want none", identity.Groups)
	}
	if identity.SkippedGroups != 0 {
		t.Errorf("skipped = %d, want 0", identity.SkippedGroups)
	}
}

// TestStringsClaimAcceptsANativeStringSlice covers the []string branch
// of stringsClaim directly: a JSON decode into map[string]any never
// produces one, so only a caller holding claims built in Go can.
func TestStringsClaimAcceptsANativeStringSlice(t *testing.T) {
	claims := map[string]any{"groups": []string{"a", " ", "b"}}

	groups, skipped, shape := stringsClaim(claims, "groups")
	if !slices.Equal(groups, []string{"a", "b"}) {
		t.Errorf("groups = %#v, want [a b]", groups)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if shape != "" {
		t.Errorf("shape = %q, want none", shape)
	}

	if groups, _, _ := stringsClaim(claims, ""); groups != nil {
		t.Errorf("unconfigured claim name yielded %#v, want nil", groups)
	}
	if groups, _, _ := stringsClaim(map[string]any{"groups": nil}, "groups"); groups != nil {
		t.Errorf("null claim yielded %#v, want nil", groups)
	}
}

// TestStringClaimIgnoresNonStringValues proves a username claim holding
// a number is treated as absent rather than coerced into a username the
// Workbench invented.
func TestStringClaimIgnoresNonStringValues(t *testing.T) {
	claims := map[string]any{"email": 42}
	if got := stringClaim(claims, "email", maxClaimValueLength); got != "" {
		t.Errorf("stringClaim = %q, want empty", got)
	}
	if got := stringClaim(claims, "", maxClaimValueLength); got != "" {
		t.Errorf("unconfigured claim name yielded %q, want empty", got)
	}

	// A claim that is absent, or present but not a string, is not
	// "rejected": only a string this package refused counts as that.
	if _, rejected := takeStringClaim(claims, "email", maxClaimValueLength); rejected {
		t.Error("a non-string claim must not be reported as rejected")
	}
	if _, rejected := takeStringClaim(claims, "absent", maxClaimValueLength); rejected {
		t.Error("an absent claim must not be reported as rejected")
	}
}

// TestSafeClaimValueRejectsInvalidUTF8 covers the branch a JSON decode
// cannot reach, since encoding/json replaces bad bytes on the way in.
func TestSafeClaimValueRejectsInvalidUTF8(t *testing.T) {
	if got := safeClaimValue("Jane\xffDoe", maxClaimValueLength); got != "" {
		t.Errorf("safeClaimValue = %q, want empty", got)
	}
}

func TestNewProviderRejectsIncompleteConfiguration(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)

	cases := map[string]struct {
		cfg  config.OIDCConfig
		want string
	}{
		"no issuer":    {config.OIDCConfig{ClientID: "client"}, "issuer is not configured"},
		"no client ID": {config.OIDCConfig{Issuer: idp.Issuer()}, "client ID is not configured"},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewProvider(context.Background(), testCase.cfg)
			if err == nil {
				t.Fatal("expected incomplete configuration to be refused")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %q, want it to mention %q", err, testCase.want)
			}
		})
	}
}

func TestNewProviderFailsWhenDiscoveryFails(t *testing.T) {
	// A server that answers the discovery request with something that is
	// not a discovery document.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no such thing here", http.StatusNotFound)
	}))
	defer server.Close()

	_, err := NewProvider(context.Background(), config.OIDCConfig{
		Issuer:   server.URL,
		ClientID: "client",
	})
	if err == nil {
		t.Fatal("expected discovery against a non-provider to fail")
	}
	if !strings.Contains(err.Error(), "discovery") {
		t.Errorf("error = %q, want it to mention discovery", err)
	}
}

// TestNewProviderDefaultsTheUsernameClaim covers the backstop that keeps
// a hand-made configuration with no username claim working, and the
// scope handling that guarantees "openid" is requested.
func TestNewProviderDefaultsTheUsernameClaim(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)

	provider, err := NewProvider(context.Background(), config.OIDCConfig{
		Issuer:   idp.Issuer(),
		ClientID: idp.ClientID(),
		Scopes:   []string{"profile"},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if provider.usernameClaim != defaultUsernameClaim {
		t.Errorf("usernameClaim = %q, want %q", provider.usernameClaim, defaultUsernameClaim)
	}
	if !slices.Equal(provider.oauth2Config.Scopes, []string{"openid", "profile"}) {
		t.Errorf("scopes = %#v, want [openid profile]", provider.oauth2Config.Scopes)
	}
}

// TestNewProviderKeepsExplicitOpenIDScope proves a configuration that
// already lists "openid" is not given a second copy.
func TestNewProviderKeepsExplicitOpenIDScope(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)

	provider := newTestProvider(t, idp, config.OIDCConfig{
		UsernameClaim: "email",
		Scopes:        []string{"openid", "email"},
	})
	if !slices.Equal(provider.oauth2Config.Scopes, []string{"openid", "email"}) {
		t.Errorf("scopes = %#v, want [openid email]", provider.oauth2Config.Scopes)
	}
}

// TestNewProviderUsesTheEffectiveClientSecret proves a secret supplied
// through client_secret_file reaches the OAuth 2.0 client. Such a secret
// is deliberately not stored in cfg.ClientSecret, so reading that field
// directly would silently send an empty secret to the token endpoint.
func TestNewProviderUsesTheEffectiveClientSecret(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)

	dir := t.TempDir()
	secretFile := filepath.Join(dir, "oidc-client-secret")
	if err := os.WriteFile(secretFile, []byte("secret-from-a-file\n"), 0o600); err != nil {
		t.Fatalf("failed to write the secret file: %v", err)
	}

	// The configuration has to be loaded through config.LoadConfig,
	// because that is what resolves client_secret_file into the
	// unexported field EffectiveClientSecret reads. Its validation
	// insists on an https issuer, so the fake provider's http issuer is
	// substituted afterwards; the resolved secret is unaffected.
	configFile := filepath.Join(dir, "server.yaml")
	configBody := "http:\n  auth:\n    oidc:\n      enabled: true\n" +
		"      issuer: https://idp.example.com\n" +
		"      client_id: " + idp.ClientID() + "\n" +
		"      client_secret_file: " + secretFile + "\n" +
		"      redirect_url: https://workbench.example.com/api/auth/oidc/callback\n"
	if err := os.WriteFile(configFile, []byte(configBody), 0o600); err != nil {
		t.Fatalf("failed to write the config file: %v", err)
	}

	cfg, err := config.LoadConfig(configFile, config.CLIFlags{ConfigFileSet: true})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.HTTP.Auth.OIDC.ClientSecret != "" {
		t.Fatal("a file-sourced secret must not be stored in ClientSecret")
	}

	oidcConfig := cfg.HTTP.Auth.OIDC
	oidcConfig.Issuer = idp.Issuer()

	provider, err := NewProvider(context.Background(), oidcConfig)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if provider.oauth2Config.ClientSecret != "secret-from-a-file" {
		t.Errorf("client secret = %q, want the file's contents",
			provider.oauth2Config.ClientSecret)
	}
}

// TestExchangeDistinguishesARejectedEmailFromAnAbsentOne is the guard on
// the allowed_email_domains gate: a user who controls their own profile
// on a shared provider must not be able to empty Identity.Email by
// sending something unusable and have that read as "no restriction".
func TestExchangeDistinguishesARejectedEmailFromAnAbsentOne(t *testing.T) {
	cases := map[string]struct {
		claims        map[string]any
		wantEmail     string
		wantRejected  bool
		wantInDropped bool
	}{
		"absent": {
			claims: map[string]any{"user": "jane.doe"},
		},
		"over the ceiling": {
			claims: map[string]any{
				"user":  "jane.doe",
				"email": strings.Repeat("a", maxEmailClaimLength) + "@example.com",
			},
			wantRejected:  true,
			wantInDropped: true,
		},
		"embedded newline": {
			claims: map[string]any{
				"user":  "jane.doe",
				"email": "jane.doe@example.com\nevil@example.net",
			},
			wantRejected:  true,
			wantInDropped: true,
		},
		"long but legal": {
			// RFC 5321 allows a 64-octet local part and a 255-octet
			// domain, so an address longer than the general claim
			// ceiling is still a real address.
			claims: map[string]any{
				"user": "jane.doe",
				"email": strings.Repeat("a", 64) + "@" +
					strings.Repeat("b", 60) + "." + strings.Repeat("c", 60) + ".example.com",
			},
			wantEmail: strings.Repeat("a", 64) + "@" +
				strings.Repeat("b", 60) + "." + strings.Repeat("c", 60) + ".example.com",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			idp := oidctest.NewFakeIDP(t)
			provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "user"})

			state := newTestLoginState(t)
			claims := map[string]any{"nonce": state.Nonce}
			for claimName, value := range testCase.claims {
				claims[claimName] = value
			}
			idp.SetNextIDToken(idp.MintIDToken(t, claims))

			identity, err := provider.Exchange(context.Background(), "code", state)
			if err != nil {
				t.Fatalf("Exchange: %v", err)
			}
			if identity.Email != testCase.wantEmail {
				t.Errorf("email = %q, want %q", identity.Email, testCase.wantEmail)
			}
			if identity.EmailRejected != testCase.wantRejected {
				t.Errorf("EmailRejected = %v, want %v",
					identity.EmailRejected, testCase.wantRejected)
			}
			if got := slices.Contains(identity.DroppedClaims, "email"); got != testCase.wantInDropped {
				t.Errorf("DroppedClaims = %v, want it to mention email: %v",
					identity.DroppedClaims, testCase.wantInDropped)
			}
		})
	}
}

// TestUsernameRejectionNamesTheClaimAndTheCharacterClass proves the
// refusal is diagnostic enough for an operator to act on: it says which
// claim was read, what was wrong with the value, and which setting to
// change. It must not quote the offending character, since the message
// goes into the server log.
func TestUsernameRejectionNamesTheClaimAndTheCharacterClass(t *testing.T) {
	cases := map[string]struct {
		username string
		want     string
	}{
		"plus sign":     {"jane+doe@example.com", "a symbol character"},
		"space":         {"jane doe@example.com", "a whitespace character"},
		"format char":   {"jane\u200ddoe@example.com", "a Unicode format character"},
		"leading dash":  {"-jane.doe@example.com", "starts with a punctuation character"},
		"over the cap":  {strings.Repeat("a", 129), "over the 128-character limit"},
		"leading digit": {"1jane.doe@example.com", ""},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			idp := oidctest.NewFakeIDP(t)
			provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "preferred_username"})

			state := newTestLoginState(t)
			idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
				"preferred_username": testCase.username,
				"nonce":              state.Nonce,
			}))

			identity, err := provider.Exchange(context.Background(), "code", state)
			if testCase.want == "" {
				// A leading digit is permitted, so this one succeeds.
				if err != nil {
					t.Fatalf("Exchange: %v", err)
				}
				if identity.Username != testCase.username {
					t.Errorf("username = %q, want %q", identity.Username, testCase.username)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected %q to be refused, got %+v", testCase.username, identity)
			}
			message := err.Error()
			for _, want := range []string{
				`"preferred_username"`,
				testCase.want,
				"http.auth.oidc.username_claim",
			} {
				if !strings.Contains(message, want) {
					t.Errorf("error = %q, want it to mention %q", message, want)
				}
			}
		})
	}
}

// TestDescribeUsernameProblemFallsBack covers the phrase returned when
// auth.ValidateUsername and this package's explanation ever disagree:
// the explanation gives up rather than asserting something wrong.
func TestDescribeUsernameProblemFallsBack(t *testing.T) {
	if got := describeUsernameProblem("jane.doe"); got != "does not meet the username rules" {
		t.Errorf("describeUsernameProblem = %q, want the fallback", got)
	}
	if got := runeClass('\u00a5'); got != "a symbol character" {
		t.Errorf("runeClass = %q, want a symbol character", got)
	}
	if got := runeClass('\u0001'); got != "a control character" {
		t.Errorf("runeClass = %q, want a control character", got)
	}
	if got := runeClass('\u4e00'); got != "a character" {
		t.Errorf("runeClass = %q, want the generic phrase", got)
	}
}

// TestIdentityReportsWhetherTheEmailAddressWasVerified covers the
// "email_verified" claim, which the allowed_email_domains check gates
// on: an address the provider has not verified is the user's own
// assertion about themselves, so anything short of a clear affirmative
// must read as false.
func TestIdentityReportsWhetherTheEmailAddressWasVerified(t *testing.T) {
	cases := map[string]struct {
		claim any
		want  bool
	}{
		"JSON true":       {claim: true, want: true},
		"the string true": {claim: "true", want: true},
		"JSON false":      {claim: false},
		"the string yes":  {claim: "yes"},
		"a number":        {claim: float64(1)},
		"absent":          {claim: nil},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			idp := oidctest.NewFakeIDP(t)
			provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})
			state := newTestLoginState(t)

			claims := map[string]any{
				"sub":   "subject-1",
				"email": "jane.doe@example.com",
				"nonce": state.Nonce,
			}
			if testCase.claim != nil {
				claims["email_verified"] = testCase.claim
			}
			idp.SetNextIDToken(idp.MintIDToken(t, claims))

			identity, err := provider.Exchange(context.Background(), "code", state)
			if err != nil {
				t.Fatalf("Exchange: %v", err)
			}
			if identity.EmailVerified != testCase.want {
				t.Errorf("EmailVerified = %v, want %v", identity.EmailVerified, testCase.want)
			}
		})
	}
}
