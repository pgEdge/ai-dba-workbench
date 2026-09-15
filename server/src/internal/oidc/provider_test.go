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
}

// TestExchangeSendsThePKCECodeVerifier proves the code exchange presents
// the verifier held in the login state, without which the code
// challenge sent at the start of the login proves nothing.
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

	if _, err := provider.Exchange(context.Background(), "code", state); err == nil {
		t.Fatal("expected a nonce mismatch to be refused")
	}
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

	if _, err := provider.Exchange(context.Background(), "code", state); err == nil {
		t.Fatal("expected a token with no nonce to be refused")
	}
}

func TestExchangeRejectsWrongAudience(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"aud": "some-other-client", "email": "jane.doe@example.com", "nonce": state.Nonce,
	}))

	if _, err := provider.Exchange(context.Background(), "code", state); err == nil {
		t.Fatal("expected a token addressed to another client to be refused")
	}
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

	if _, err := provider.Exchange(context.Background(), "code", state); err == nil {
		t.Fatal("expected an expired token to be refused")
	}
}

func TestExchangeRejectsTokenSignedByAnotherKey(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.SignWithForeignKey(t, map[string]any{
		"email": "jane.doe@example.com", "nonce": state.Nonce,
	}))

	if _, err := provider.Exchange(context.Background(), "code", state); err == nil {
		t.Fatal("expected a token signed by an unpublished key to be refused")
	}
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

	if _, err := provider.Exchange(context.Background(), "code", state); err == nil {
		t.Fatal("expected a token from another issuer to be refused")
	}
}

func TestExchangeRejectsMissingUsernameClaim(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	state := newTestLoginState(t)
	idp.SetNextIDToken(idp.MintIDToken(t, map[string]any{
		"sub": "subject-1", "nonce": state.Nonce,
	}))

	if _, err := provider.Exchange(context.Background(), "code", state); err == nil {
		t.Fatal("expected a token with no username claim to be refused")
	}
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

	if _, err := provider.Exchange(context.Background(), "code", state); err == nil {
		t.Fatal("expected a whitespace-only username claim to be refused")
	}
}

// TestExchangeRejectsAFailedCodeRedemption covers the token endpoint
// refusing the authorization code, which the fake does whenever no ID
// token has been staged.
func TestExchangeRejectsAFailedCodeRedemption(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	if _, err := provider.Exchange(context.Background(), "code", newTestLoginState(t)); err == nil {
		t.Fatal("expected a refused authorization code to be an error")
	}
}

// TestExchangeRejectsAResponseWithNoIDToken covers a token endpoint that
// answers successfully but omits the ID token entirely, which an OAuth
// 2.0 (rather than OpenID Connect) endpoint would do.
func TestExchangeRejectsAResponseWithNoIDToken(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{UsernameClaim: "email"})

	// Point the provider's token endpoint at a server that answers
	// without an id_token member.
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"a","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenServer.Close()
	provider.oauth2Config.Endpoint.TokenURL = tokenServer.URL

	if _, err := provider.Exchange(context.Background(), "code", newTestLoginState(t)); err == nil {
		t.Fatal("expected a response with no ID token to be an error")
	}
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
		claim any
		want  []string
	}{
		"array of strings": {[]string{"a", "b"}, []string{"a", "b"}},
		"array of any":     {[]any{"a", "b"}, []string{"a", "b"}},
		"single string":    {"a", []string{"a"}},
		"absent":           {nil, nil},
		"mixed array":      {[]any{"a", 42, "b"}, []string{"a", "b"}},
		"wrong shape":      {map[string]any{"a": true}, nil},
		"empty array":      {[]any{}, nil},
	}

	idp := oidctest.NewFakeIDP(t)
	provider := newTestProvider(t, idp, config.OIDCConfig{
		UsernameClaim: "email", GroupsClaim: "groups",
	})

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
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
}

// TestStringsClaimAcceptsANativeStringSlice covers the []string branch
// of stringsClaim directly: a JSON decode into map[string]any never
// produces one, so only a caller holding claims built in Go can.
func TestStringsClaimAcceptsANativeStringSlice(t *testing.T) {
	claims := map[string]any{"groups": []string{"a", " ", "b"}}
	if got := stringsClaim(claims, "groups"); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("stringsClaim = %#v, want [a b]", got)
	}
	if got := stringsClaim(claims, ""); got != nil {
		t.Errorf("unconfigured claim name yielded %#v, want nil", got)
	}
}

// TestStringClaimIgnoresNonStringValues proves a username claim holding
// a number is treated as absent rather than coerced into a username the
// Workbench invented.
func TestStringClaimIgnoresNonStringValues(t *testing.T) {
	claims := map[string]any{"email": 42}
	if got := stringClaim(claims, "email"); got != "" {
		t.Errorf("stringClaim = %q, want empty", got)
	}
}

func TestNewProviderRejectsIncompleteConfiguration(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)

	cases := map[string]config.OIDCConfig{
		"no issuer":    {ClientID: "client"},
		"no client ID": {Issuer: idp.Issuer()},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewProvider(context.Background(), cfg); err == nil {
				t.Fatal("expected incomplete configuration to be refused")
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
