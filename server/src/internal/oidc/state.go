/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package oidc holds the state carried across an OIDC login round trip.
// It knows nothing about HTTP or the identity provider protocol: it only
// seals and opens the short-lived cookie value, and validates the
// post-login return path against an open-redirect attack.
package oidc

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/pkg/crypto"
)

// Package-level function variables wrap external dependencies so tests
// can swap them to exercise otherwise-unreachable error paths (a failed
// system random source, or JSON marshaling failure). Production
// callers see the standard library / pkg/crypto behavior.
var (
	randRead    = io.ReadFull
	marshalJSON = json.Marshal
	encryptGCM  = crypto.EncryptGCM
)

// StateCookieName is the name of the cookie that carries the sealed
// LoginState across the round trip to the identity provider.
const StateCookieName = "oidc_login_state"

// StateTTL is how long a sealed LoginState remains valid after it was
// issued. It is exported so that the HTTP handlers (added in a later
// task, in package internal/api) can use it to set the cookie's MaxAge.
const StateTTL = 10 * time.Minute

// clockSkew is the amount of clock skew tolerated for a LoginState whose
// IssuedAt appears to be in the future.
const clockSkew = time.Minute

// randomValueLength is the number of random bytes drawn for each of the
// state, nonce and code verifier values.
const randomValueLength = 32

// LoginState holds the state, nonce and PKCE code verifier for a single
// in-flight OIDC login, along with the path to return the user to once
// login completes.
type LoginState struct {
	State        string    `json:"state"`
	Nonce        string    `json:"nonce"`
	CodeVerifier string    `json:"code_verifier"`
	ReturnPath   string    `json:"return_path"`
	IssuedAt     time.Time `json:"issued_at"`
}

// NewLoginState creates a new LoginState with freshly generated random
// state, nonce and code verifier values, and the current time as
// IssuedAt. returnPath is sanitized with SanitiseReturnPath before being
// stored.
func NewLoginState(returnPath string) (*LoginState, error) {
	// Draw the three random values from a single read of the system
	// random source rather than one read each, so there is one error
	// path to handle (and to test) instead of three.
	buf := make([]byte, 3*randomValueLength)
	if _, err := randRead(rand.Reader, buf); err != nil {
		return nil, fmt.Errorf("failed to generate random values: %w", err)
	}

	return &LoginState{
		State:        base64.RawURLEncoding.EncodeToString(buf[0:randomValueLength]),
		Nonce:        base64.RawURLEncoding.EncodeToString(buf[randomValueLength : 2*randomValueLength]),
		CodeVerifier: base64.RawURLEncoding.EncodeToString(buf[2*randomValueLength : 3*randomValueLength]),
		ReturnPath:   SanitiseReturnPath(returnPath),
		IssuedAt:     time.Now().UTC(),
	}, nil
}

// CodeChallenge returns the PKCE S256 code challenge derived from the
// state's CodeVerifier.
func (s *LoginState) CodeChallenge() string {
	sum := sha256.Sum256([]byte(s.CodeVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// SealState encrypts state with key using AES-256-GCM and encodes the
// result with base64.RawURLEncoding so it is safe to use as a cookie
// value.
func SealState(key []byte, state *LoginState) (string, error) {
	plaintext, err := marshalJSON(state)
	if err != nil {
		return "", fmt.Errorf("failed to marshal login state: %w", err)
	}

	ciphertext, err := encryptGCM(key, plaintext)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt login state: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// OpenState reverses SealState, decrypting sealed with key and returning
// the resulting LoginState. It returns an error if sealed is malformed,
// if it was not produced with key, or if its IssuedAt indicates it has
// expired (more than StateTTL old) or is implausibly far in the future
// (more than one minute of clock skew).
func OpenState(key []byte, sealed string) (*LoginState, error) {
	ciphertext, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		return nil, fmt.Errorf("failed to decode login state: %w", err)
	}

	plaintext, err := crypto.DecryptGCM(key, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt login state: %w", err)
	}

	var state LoginState
	if err := json.Unmarshal(plaintext, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal login state: %w", err)
	}

	now := time.Now().UTC()
	if state.IssuedAt.After(now.Add(clockSkew)) {
		return nil, fmt.Errorf("login state issued in the future")
	}
	if now.After(state.IssuedAt.Add(StateTTL)) {
		return nil, fmt.Errorf("login state has expired")
	}

	return &state, nil
}

// SanitiseReturnPath returns raw unchanged if it is safe to redirect the
// browser to after login, and "/" otherwise. This is the open-redirect
// guard: raw comes from a query parameter an attacker fully controls, so
// anything not certainly a same-origin absolute path is rejected.
//
// A safe value:
//   - begins with a single "/" (not "//" or "/\", both of which some
//     browsers treat as a scheme-relative or absolute URL to another
//     host)
//   - contains no control characters (which could be used to smuggle
//     extra header lines or otherwise be misinterpreted before the
//     browser gets to it)
//   - parses with url.Parse
//   - has an empty Scheme and Host once parsed, so it can only ever
//     resolve to a path on this origin
func SanitiseReturnPath(raw string) string {
	const fallback = "/"

	if raw == "" {
		return fallback
	}
	if !strings.HasPrefix(raw, "/") {
		return fallback
	}
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") {
		return fallback
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return fallback
		}
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fallback
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		return fallback
	}

	// url.Parse leaves percent-encoding in Path decoded, so a raw value
	// such as "/%2f%2fevil.example.com" or "/%5cevil.example.com" would
	// pass the prefix checks above on the raw string but still decode to
	// a scheme-relative or backslash-prefixed path that a browser could
	// treat as pointing at another host. Re-run the same checks against
	// the decoded form.
	if strings.HasPrefix(parsed.Path, "//") || strings.HasPrefix(parsed.Path, "/\\") {
		return fallback
	}
	for _, r := range parsed.Path {
		if r < 0x20 || r == 0x7f {
			return fallback
		}
	}

	return raw
}
