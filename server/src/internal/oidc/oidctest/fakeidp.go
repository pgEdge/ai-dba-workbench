/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package oidctest provides an in-process OpenID Connect provider for
// tests: it serves a discovery document, a JWKS endpoint and a token
// endpoint from an httptest.Server, and mints RSA-signed ID tokens with
// caller-controlled claims. Tests of the OIDC login flow can therefore
// run the whole round trip without touching the network.
//
// It is a normal (non-test) package rather than a _test.go file because
// both internal/oidc and internal/api need the same fake, and keeping
// two copies in step is a reliable source of drift. It imports testing
// and must never be imported from production code; nothing outside a
// _test.go file may reference it.
package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// DefaultClientID is the OAuth 2.0 client identifier the fake provider
// expects, and the value MintIDToken uses for the "aud" claim unless the
// caller supplies one.
const DefaultClientID = "workbench-test-client"

// signingKeyID is the "kid" published in the JWKS and stamped into the
// header of every token signed with the published key. Tokens from
// SignWithForeignKey deliberately carry a different one.
const signingKeyID = "fake-idp-key"

// foreignKeyID is the "kid" stamped into tokens signed with a key the
// published JWKS does not list.
const foreignKeyID = "fake-idp-foreign-key"

// Generating 2048-bit RSA keys is slow enough to dominate the runtime of
// a package whose tests each stand up a provider, so both keys are
// generated once and shared by every FakeIDP in the process. They are
// test-only keys and never leave it.
var (
	keysOnce   sync.Once
	sharedKey  *rsa.PrivateKey
	foreignKey *rsa.PrivateKey
	keysErr    error
)

func testKeys(t *testing.T) (signing, foreign *rsa.PrivateKey) {
	t.Helper()

	keysOnce.Do(func() {
		sharedKey, keysErr = rsa.GenerateKey(rand.Reader, 2048)
		if keysErr != nil {
			return
		}
		foreignKey, keysErr = rsa.GenerateKey(rand.Reader, 2048)
	})
	if keysErr != nil {
		t.Fatalf("failed to generate test RSA keys: %v", keysErr)
	}
	return sharedKey, foreignKey
}

// FakeIDP is an in-process OpenID Connect provider.
type FakeIDP struct {
	server  *httptest.Server
	signing *rsa.PrivateKey
	foreign *rsa.PrivateKey

	mu          sync.Mutex
	nextIDToken string
	omitIDToken bool
	lastForm    url.Values

	// issuedRefreshToken is what a code exchange returns as its
	// refresh_token, and is empty when the exchange should return none.
	issuedRefreshToken string

	// validRefreshToken is the one refresh token the provider will
	// currently redeem. A rotation replaces it and a revocation empties
	// it, so that presenting an old one is refused as reuse is by a
	// real provider.
	validRefreshToken string

	// refresh is the response every successful refresh grant gives, and
	// refreshCount counts refresh grant requests, successful or not.
	refresh      RefreshResponse
	refreshCount int
}

// RefreshResponse is what the fake provider answers a refresh grant
// with.
type RefreshResponse struct {
	// IDToken is returned as id_token, and is omitted when empty, as a
	// provider is entitled to do on a refresh.
	IDToken string

	// RotatedRefreshToken, when set, is returned as the new
	// refresh_token and becomes the only one the provider will redeem.
	// When empty the response carries no refresh_token and the redeemed
	// one stays valid.
	RotatedRefreshToken string

	// ExpiresIn is the access token's lifetime in seconds. Zero means an
	// hour; a negative value omits expires_in altogether.
	ExpiresIn int

	// Delay holds the response back, so that a test can make concurrent
	// requests overlap a refresh in flight.
	Delay time.Duration
}

// NewFakeIDP starts a fake identity provider and registers its shutdown
// with t.Cleanup.
func NewFakeIDP(t *testing.T) *FakeIDP {
	t.Helper()

	signing, foreign := testKeys(t)
	idp := &FakeIDP{signing: signing, foreign: foreign}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", idp.handleDiscovery)
	mux.HandleFunc("/jwks", idp.handleJWKS)
	mux.HandleFunc("/token", idp.handleToken)
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, _ *http.Request) {
		// Never exercised by the tests: the browser, not the server,
		// visits the authorization endpoint. It exists so that the
		// discovery document advertises a URL that resolves.
		w.WriteHeader(http.StatusNoContent)
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)

	return idp
}

// Issuer returns the provider's issuer URL, which is also the base URL of
// its HTTP endpoints.
func (f *FakeIDP) Issuer() string {
	return f.server.URL
}

// ClientID returns the client identifier the provider expects.
func (f *FakeIDP) ClientID() string {
	return DefaultClientID
}

// SetNextIDToken makes the next code exchange return the supplied ID
// token verbatim.
func (f *FakeIDP) SetNextIDToken(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextIDToken = token
	f.omitIDToken = false
}

// SetNextResponseOmitsIDToken makes the next code exchange succeed but
// answer without an id_token member, as a plain OAuth 2.0 token
// endpoint (rather than an OpenID Connect one) would.
func (f *FakeIDP) SetNextResponseOmitsIDToken() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextIDToken = ""
	f.omitIDToken = true
}

// IssueRefreshToken makes every later code exchange return token as its
// refresh_token, and makes token the one refresh token the provider will
// redeem.
func (f *FakeIDP) IssueRefreshToken(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issuedRefreshToken = token
	f.validRefreshToken = token
}

// SetRefreshResponse sets what every later successful refresh grant
// answers with.
func (f *FakeIDP) SetRefreshResponse(response RefreshResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refresh = response
}

// RevokeRefreshTokens makes the provider refuse every refresh token, as
// a real one does once the user has been removed or disabled there.
func (f *FakeIDP) RevokeRefreshTokens() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.validRefreshToken = ""
}

// RefreshCount returns how many refresh grant requests the token
// endpoint has received.
func (f *FakeIDP) RefreshCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshCount
}

// LastTokenRequestForm returns the form values of the most recent request
// to the token endpoint, so a test can assert on, for example, the PKCE
// code verifier the client sent. It returns nil if the token endpoint has
// not been called.
func (f *FakeIDP) LastTokenRequestForm() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastForm
}

// MintIDToken signs a JWT with the published key, defaulting the "iss",
// "aud", "sub", "iat" and "exp" claims when the caller leaves them unset.
func (f *FakeIDP) MintIDToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	return f.sign(t, f.signing, signingKeyID, claims)
}

// SignWithForeignKey signs a JWT with a key the published JWKS does not
// list, so that verification of the signature must fail.
func (f *FakeIDP) SignWithForeignKey(t *testing.T, claims map[string]any) string {
	t.Helper()
	return f.sign(t, f.foreign, foreignKeyID, claims)
}

// sign builds and signs an RS256 JWT over the supplied claims, filling in
// the registered claims the caller omitted. The caller's map is never
// modified.
func (f *FakeIDP) sign(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()

	payload := map[string]any{
		"iss": f.Issuer(),
		"aud": DefaultClientID,
		"sub": "subject-1",
		"iat": time.Now().Add(-time.Minute).Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	for name, value := range claims {
		payload[name] = value
	}

	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid}

	signingInput := encodeSegment(t, header) + "." + encodeSegment(t, payload)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("failed to sign test ID token: %v", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func encodeSegment(t *testing.T, value map[string]any) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("failed to marshal test ID token segment: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func (f *FakeIDP) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                f.Issuer(),
		"authorization_endpoint":                f.Issuer() + "/authorize",
		"token_endpoint":                        f.Issuer() + "/token",
		"jwks_uri":                              f.Issuer() + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (f *FakeIDP) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	public := &f.signing.PublicKey
	writeJSON(w, http.StatusOK, map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"alg": "RS256",
			"use": "sig",
			"kid": signingKeyID,
			"n":   base64.RawURLEncoding.EncodeToString(public.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(public.E)).Bytes()),
		}},
	})
}

func (f *FakeIDP) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	if r.PostForm.Get("grant_type") == "refresh_token" {
		f.handleRefresh(w, r.PostForm)
		return
	}

	f.mu.Lock()
	f.lastForm = r.PostForm
	idToken := f.nextIDToken
	omitIDToken := f.omitIDToken
	refreshToken := f.issuedRefreshToken
	f.mu.Unlock()

	if omitIDToken {
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": "fake-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
		return
	}

	if idToken == "" {
		// No token was staged: report the OAuth 2.0 error the real thing
		// would return for an authorization code it cannot redeem.
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}

	body := map[string]any{
		"access_token": "fake-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     idToken,
	}
	if refreshToken != "" {
		body["refresh_token"] = refreshToken
	}
	writeJSON(w, http.StatusOK, body)
}

// handleRefresh answers a refresh grant: invalid_grant for any token but
// the one currently valid, and otherwise the staged RefreshResponse,
// rotating the valid token when that response says to.
func (f *FakeIDP) handleRefresh(w http.ResponseWriter, form url.Values) {
	f.mu.Lock()
	f.lastForm = form
	f.refreshCount++
	response := f.refresh
	valid := f.validRefreshToken != "" &&
		form.Get("refresh_token") == f.validRefreshToken
	if valid && response.RotatedRefreshToken != "" {
		f.validRefreshToken = response.RotatedRefreshToken
	}
	f.mu.Unlock()

	if response.Delay > 0 {
		time.Sleep(response.Delay)
	}

	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}

	body := map[string]any{
		"access_token": "fake-refreshed-access-token",
		"token_type":   "Bearer",
	}
	switch {
	case response.ExpiresIn == 0:
		body["expires_in"] = 3600
	case response.ExpiresIn > 0:
		body["expires_in"] = response.ExpiresIn
	}
	if response.IDToken != "" {
		body["id_token"] = response.IDToken
	}
	if response.RotatedRefreshToken != "" {
		body["refresh_token"] = response.RotatedRefreshToken
	}
	writeJSON(w, http.StatusOK, body)
}

func writeJSON(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// An encoding failure here could only be a bug in this file's own
	// literals, and a handler has nowhere to report it.
	_ = json.NewEncoder(w).Encode(body)
}
