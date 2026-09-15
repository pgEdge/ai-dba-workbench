/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package oidctest

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// getJSON fetches url and decodes the JSON body into a map.
func getJSON(t *testing.T, target string) map[string]any {
	t.Helper()

	response, err := http.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", target, response.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode %s: %v", target, err)
	}
	return body
}

// decodeSegment decodes one base64url JWT segment into a map.
func decodeSegment(t *testing.T, segment string) map[string]any {
	t.Helper()

	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		t.Fatalf("decode JWT segment: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal JWT segment: %v", err)
	}
	return decoded
}

func TestDiscoveryDocumentDescribesTheFakeProvider(t *testing.T) {
	idp := NewFakeIDP(t)

	document := getJSON(t, idp.Issuer()+"/.well-known/openid-configuration")
	if document["issuer"] != idp.Issuer() {
		t.Errorf("issuer = %v, want %v", document["issuer"], idp.Issuer())
	}
	if document["token_endpoint"] != idp.Issuer()+"/token" {
		t.Errorf("token_endpoint = %v", document["token_endpoint"])
	}
	if document["jwks_uri"] != idp.Issuer()+"/jwks" {
		t.Errorf("jwks_uri = %v", document["jwks_uri"])
	}
	if idp.ClientID() != DefaultClientID {
		t.Errorf("ClientID() = %q, want %q", idp.ClientID(), DefaultClientID)
	}

	// The authorization endpoint exists only so that the advertised URL
	// resolves; nothing in these tests drives a browser to it.
	response, err := http.Get(idp.Issuer() + "/authorize")
	if err != nil {
		t.Fatalf("GET authorize: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusNoContent {
		t.Errorf("authorize status = %d, want 204", response.StatusCode)
	}
}

func TestJWKSPublishesTheSigningKey(t *testing.T) {
	idp := NewFakeIDP(t)

	document := getJSON(t, idp.Issuer()+"/jwks")
	keys, ok := document["keys"].([]any)
	if !ok || len(keys) != 1 {
		t.Fatalf("keys = %v, want exactly one", document["keys"])
	}

	key, ok := keys[0].(map[string]any)
	if !ok {
		t.Fatalf("key is %T, want an object", keys[0])
	}
	if key["kid"] != signingKeyID {
		t.Errorf("kid = %v, want %v", key["kid"], signingKeyID)
	}
	if key["kty"] != "RSA" || key["alg"] != "RS256" {
		t.Errorf("kty/alg = %v/%v, want RSA/RS256", key["kty"], key["alg"])
	}
	if key["n"] == "" || key["e"] == "" {
		t.Error("the published key is missing its modulus or exponent")
	}
}

func TestMintIDTokenDefaultsTheRegisteredClaims(t *testing.T) {
	idp := NewFakeIDP(t)

	supplied := map[string]any{"email": "jane.doe@example.com"}
	token := idp.MintIDToken(t, supplied)

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}

	header := decodeSegment(t, parts[0])
	if header["alg"] != "RS256" || header["kid"] != signingKeyID {
		t.Errorf("header = %v", header)
	}

	payload := decodeSegment(t, parts[1])
	if payload["iss"] != idp.Issuer() {
		t.Errorf("iss = %v, want %v", payload["iss"], idp.Issuer())
	}
	if payload["aud"] != DefaultClientID || payload["sub"] != "subject-1" {
		t.Errorf("aud/sub = %v/%v", payload["aud"], payload["sub"])
	}
	if payload["iat"] == nil || payload["exp"] == nil {
		t.Error("iat and exp should both be defaulted")
	}
	if payload["email"] != "jane.doe@example.com" {
		t.Errorf("email = %v", payload["email"])
	}

	// The caller's map must be left alone, so a test can reuse it.
	if len(supplied) != 1 {
		t.Errorf("MintIDToken modified the caller's claims: %v", supplied)
	}
}

func TestMintIDTokenHonoursSuppliedClaims(t *testing.T) {
	idp := NewFakeIDP(t)

	payload := decodeSegment(t, strings.Split(
		idp.MintIDToken(t, map[string]any{"iss": "https://other.example.com"}), ".")[1])
	if payload["iss"] != "https://other.example.com" {
		t.Errorf("iss = %v, want the supplied value", payload["iss"])
	}
}

func TestSignWithForeignKeyUsesAnUnpublishedKey(t *testing.T) {
	idp := NewFakeIDP(t)

	header := decodeSegment(t, strings.Split(idp.SignWithForeignKey(t, nil), ".")[0])
	if header["kid"] != foreignKeyID {
		t.Errorf("kid = %v, want %v", header["kid"], foreignKeyID)
	}
}

func TestTokenEndpointReturnsTheStagedIDToken(t *testing.T) {
	idp := NewFakeIDP(t)

	if idp.LastTokenRequestForm() != nil {
		t.Error("no token request has been made yet")
	}

	idp.SetNextIDToken("a-staged-token")
	response, err := http.PostForm(idp.Issuer()+"/token", url.Values{
		"code":          {"the-code"},
		"code_verifier": {"the-verifier"},
	})
	if err != nil {
		t.Fatalf("POST token: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if body["id_token"] != "a-staged-token" {
		t.Errorf("id_token = %v", body["id_token"])
	}
	if body["token_type"] != "Bearer" {
		t.Errorf("token_type = %v, want Bearer", body["token_type"])
	}

	form := idp.LastTokenRequestForm()
	if form.Get("code") != "the-code" || form.Get("code_verifier") != "the-verifier" {
		t.Errorf("recorded form = %v", form)
	}
}

func TestTokenEndpointRefusesTheCodeWhenNoTokenIsStaged(t *testing.T) {
	idp := NewFakeIDP(t)

	response, err := http.PostForm(idp.Issuer()+"/token", url.Values{"code": {"the-code"}})
	if err != nil {
		t.Fatalf("POST token: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", response.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if body["error"] != "invalid_grant" {
		t.Errorf("error = %v, want invalid_grant", body["error"])
	}
}

func TestTokenEndpointRejectsAnUnparsableForm(t *testing.T) {
	idp := NewFakeIDP(t)

	response, err := http.Post(idp.Issuer()+"/token",
		"application/x-www-form-urlencoded", strings.NewReader("%zz"))
	if err != nil {
		t.Fatalf("POST token: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", response.StatusCode)
	}
}

func TestTokenEndpointCanOmitTheIDToken(t *testing.T) {
	idp := NewFakeIDP(t)

	idp.SetNextIDToken("a-staged-token")
	idp.SetNextResponseOmitsIDToken()

	response, err := http.PostForm(idp.Issuer()+"/token", url.Values{"code": {"the-code"}})
	if err != nil {
		t.Fatalf("POST token: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if _, present := body["id_token"]; present {
		t.Errorf("id_token should be absent, got %v", body["id_token"])
	}
	if body["access_token"] != "fake-access-token" {
		t.Errorf("access_token = %v", body["access_token"])
	}
}
