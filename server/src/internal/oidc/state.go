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
// system random source, or JSON marshaling failure). Production callers
// see the standard library's behavior. Neither variable is safe for a
// test that swaps it to run under t.Parallel(): they are package-global
// mutable state shared by every test in this package, and a parallel
// test would race another test's use of the real implementation.
var (
	randRead    = io.ReadFull
	marshalJSON = json.Marshal
)

// StateCookieName is the name of the cookie that carries the sealed
// LoginState across the round trip to the identity provider, for a
// plain-HTTP deployment (local development only). See
// SecureStateCookieName and StateCookieNameFor.
const StateCookieName = "oidc_login_state"

// SecureStateCookieName is StateCookieName's "__Host-" prefixed form,
// used whenever the request arrived over HTTPS. A browser refuses to
// set a "__Host-" cookie unless it is Secure, has Path=/ and carries no
// Domain attribute, which stops a compromised or attacker-controlled
// sibling subdomain from overwriting this cookie with one matching an
// attacker's own in-flight login: without the prefix, an attacker who
// can set cookies on a sibling subdomain could make both the cookie and
// the "state" query parameter agree, silently logging the victim into
// the attacker's identity.
const SecureStateCookieName = "__Host-oidc_login_state"

// StateCookieNameFor returns the cookie name to use for the current
// request: SecureStateCookieName when secure is true (the request
// arrived over HTTPS), and StateCookieName otherwise. The "__Host-"
// prefix requires the Secure attribute, so it cannot be used on a
// plain-HTTP deployment; callers (added in a later task, in package
// internal/api) should pass whether the request that set the cookie was
// itself over HTTPS.
func StateCookieNameFor(secure bool) string {
	if secure {
		return SecureStateCookieName
	}
	return StateCookieName
}

// StateTTL is how long a sealed LoginState remains valid after it was
// issued. It is exported so that the HTTP handlers (added in a later
// task, in package internal/api) can use it to set the cookie's MaxAge.
//
// StateTTL is not the authoritative bound on a state cookie's validity:
// OpenState also tolerates up to clockSkew of future-dated IssuedAt (to
// absorb minor clock drift between the server instance that issued the
// cookie and the one that later opens it), and that tolerance stacks
// with StateTTL rather than being absorbed by it. The worst case, a
// state cookie issued by a server clock running clockSkew fast, remains
// valid for StateTTL plus clockSkew — 11 minutes, not 10 — measured
// from another server's accurate clock. IssuedAt is part of the
// encrypted, authenticated payload, so this reflects only legitimate
// clock drift between trusted server instances, never a value an
// attacker can forge or choose.
const StateTTL = 10 * time.Minute

// clockSkew is the amount of clock skew tolerated for a LoginState whose
// IssuedAt appears to be in the future.
const clockSkew = time.Minute

// randomValueLength is the number of random bytes drawn for each of the
// state, nonce and code verifier values.
const randomValueLength = 32

// keySize is the required length, in bytes, of the key passed to
// SealState and OpenState. pkg/crypto's EncryptGCM and DecryptGCM accept
// any key aes.NewCipher will take (16, 24 or 32 bytes), silently
// downgrading to AES-128 or AES-192 rather than failing; this package
// promises AES-256, so it enforces the length itself at both boundaries.
const keySize = 32

// encodedValueLength is the exact base64.RawURLEncoding length of a
// randomValueLength-byte value: 43 characters for 32 bytes. OpenState
// requires the State, Nonce and CodeVerifier fields to be exactly this
// long, both to reject a structurally incomplete LoginState (one with
// an empty field that could spuriously match an attacker-supplied empty
// query parameter) and, for CodeVerifier specifically, because it is
// exactly the length RFC 7636 requires (43 to 128 characters).
var encodedValueLength = base64.RawURLEncoding.EncodedLen(randomValueLength)

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
	if len(key) != keySize {
		return "", fmt.Errorf("encryption key must be %d bytes, got %d", keySize, len(key))
	}

	plaintext, err := marshalJSON(state)
	if err != nil {
		return "", fmt.Errorf("failed to marshal login state: %w", err)
	}

	ciphertext, err := crypto.EncryptGCM(key, plaintext)
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
	if len(key) != keySize {
		return nil, fmt.Errorf("encryption key must be %d bytes, got %d", keySize, len(key))
	}

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

	if len(state.State) != encodedValueLength || len(state.Nonce) != encodedValueLength ||
		len(state.CodeVerifier) != encodedValueLength {
		return nil, fmt.Errorf("login state is incomplete")
	}

	if err := validateIssuedAt(state.IssuedAt, time.Now().UTC()); err != nil {
		return nil, err
	}

	return &state, nil
}

// validateIssuedAt checks issuedAt against now under the future-skew and
// StateTTL rules documented on StateTTL. It is a pure function of its
// two time.Time arguments (rather than reading time.Now() itself) so
// tests can exercise the boundary precisely, without needing real time
// to elapse.
//
// An issuedAt more than clockSkew in the future is rejected outright;
// otherwise the state expires StateTTL after issuedAt, exactly as
// documented on StateTTL (including the worst case where issuedAt
// itself is clockSkew ahead of another server's clock).
func validateIssuedAt(issuedAt, now time.Time) error {
	if issuedAt.After(now.Add(clockSkew)) {
		return fmt.Errorf("login state issued in the future")
	}
	if now.After(issuedAt.Add(StateTTL)) {
		return fmt.Errorf("login state has expired")
	}
	return nil
}

// maxReturnPathLength bounds the length of a value SanitiseReturnPath
// will accept. AES-GCM ciphertext is the same length as its plaintext,
// so an unbounded return path would produce a sealed cookie value that
// can exceed a browser's ~4096-byte cookie size limit; the browser then
// discards the cookie silently and the subsequent login fails in a way
// that gives no hint the return path was the cause. 1024 bytes is far
// more than any real route in this application needs.
const maxReturnPathLength = 1024

// SanitiseReturnPath returns raw unchanged if it is safe to redirect the
// browser to after login, and "/" otherwise. This is the open-redirect
// guard: raw comes from a query parameter an attacker fully controls, so
// anything not certainly a same-origin absolute path is rejected.
//
// A safe value:
//   - is no longer than maxReturnPathLength
//   - begins with a single "/" (not "//" or "/\", both of which some
//     browsers treat as a scheme-relative or absolute URL to another
//     host)
//   - contains no control characters (which could be used to smuggle
//     extra header lines or otherwise be misinterpreted before the
//     browser gets to it)
//   - parses with url.Parse
//   - has an empty Scheme and Host once parsed, so it can only ever
//     resolve to a path on this origin
//
// The returned value is safe to place directly in a Location header or
// an HTML redirect; it is NOT safe to decode further (for example with
// url.PathUnescape) before use, since that would reintroduce exactly
// the percent-encoded bypasses this function already rejects.
func SanitiseReturnPath(raw string) string {
	const fallback = "/"

	if raw == "" || len(raw) > maxReturnPathLength {
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
