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
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/pkg/crypto"
)

// None of the tests in this file that swap randRead or marshalJSON may
// run under t.Parallel(): both are package-global mutable state shared
// by every test in this package, and a parallel test would race another
// test's use of the real implementation.

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func TestSealOpenRoundTrip(t *testing.T) {
	key := testKey(t)
	original, err := NewLoginState("/dashboard")
	if err != nil {
		t.Fatalf("NewLoginState: %v", err)
	}

	sealed, err := SealState(key, original)
	if err != nil {
		t.Fatalf("SealState: %v", err)
	}
	opened, err := OpenState(key, sealed)
	if err != nil {
		t.Fatalf("OpenState: %v", err)
	}

	if opened.State != original.State || opened.Nonce != original.Nonce ||
		opened.CodeVerifier != original.CodeVerifier ||
		opened.ReturnPath != original.ReturnPath {
		t.Fatalf("round trip lost data: %+v vs %+v", opened, original)
	}
	if !opened.IssuedAt.Equal(original.IssuedAt) {
		t.Fatalf("round trip lost IssuedAt: %v vs %v", opened.IssuedAt, original.IssuedAt)
	}
}

// TestOpenStateRejectsTamperedValue flips a single bit inside the
// ciphertext body (not the nonce, and not the trailing authentication
// tag) and checks OpenState refuses the result. A body flip is the more
// convincing tamper test for AES-GCM: it is a counter-mode cipher, so a
// flipped ciphertext bit flips the corresponding plaintext bit directly,
// and the resulting authentication-tag mismatch is what actually catches
// it. An earlier version of this test instead replaced the trailing two
// characters of the base64 value with "AA", which only ever touches the
// tag and, whenever the original value already ended in "AA", silently
// tampered with nothing at all.
func TestOpenStateRejectsTamperedValue(t *testing.T) {
	key := testKey(t)
	state, err := NewLoginState("/")
	if err != nil {
		t.Fatalf("NewLoginState: %v", err)
	}
	sealed, err := SealState(key, state)
	if err != nil {
		t.Fatalf("SealState: %v", err)
	}

	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatalf("failed to decode sealed value: %v", err)
	}

	// pkg/crypto's EncryptGCM prepends a 12-byte nonce (the standard
	// AES-GCM nonce size) to the ciphertext body; flip the first byte of
	// the body itself, immediately after the nonce.
	const nonceSize = 12
	if len(raw) <= nonceSize {
		t.Fatalf("sealed value too short to flip a ciphertext body bit: %d bytes", len(raw))
	}
	raw[nonceSize] ^= 0x01

	tampered := base64.RawURLEncoding.EncodeToString(raw)
	if _, err := OpenState(key, tampered); err == nil {
		t.Fatal("expected a state cookie with a corrupted ciphertext body to be refused")
	}
}

func TestOpenStateRejectsWrongKey(t *testing.T) {
	state, err := NewLoginState("/")
	if err != nil {
		t.Fatalf("NewLoginState: %v", err)
	}
	sealed, err := SealState(testKey(t), state)
	if err != nil {
		t.Fatalf("SealState: %v", err)
	}

	other := make([]byte, 32)
	other[0] = 1
	if _, err := OpenState(other, sealed); err == nil {
		t.Fatal("expected a state cookie sealed with another key to be refused")
	}
}

func TestOpenStateRejectsExpiredValue(t *testing.T) {
	key := testKey(t)
	state, err := NewLoginState("/")
	if err != nil {
		t.Fatalf("NewLoginState: %v", err)
	}
	state.IssuedAt = time.Now().Add(-StateTTL - time.Minute)
	sealed, err := SealState(key, state)
	if err != nil {
		t.Fatalf("SealState: %v", err)
	}

	if _, err := OpenState(key, sealed); err == nil {
		t.Fatal("expected an expired state cookie to be refused")
	}
}

func TestOpenStateRejectsFutureValue(t *testing.T) {
	key := testKey(t)
	state, err := NewLoginState("/")
	if err != nil {
		t.Fatalf("NewLoginState: %v", err)
	}
	state.IssuedAt = time.Now().Add(10 * time.Minute)
	sealed, err := SealState(key, state)
	if err != nil {
		t.Fatalf("SealState: %v", err)
	}

	if _, err := OpenState(key, sealed); err == nil {
		t.Fatal("expected an implausibly future state cookie to be refused")
	}
}

func TestOpenStateAcceptsSmallClockSkew(t *testing.T) {
	key := testKey(t)
	state, err := NewLoginState("/")
	if err != nil {
		t.Fatalf("NewLoginState: %v", err)
	}
	state.IssuedAt = time.Now().Add(30 * time.Second)
	sealed, err := SealState(key, state)
	if err != nil {
		t.Fatalf("SealState: %v", err)
	}

	if _, err := OpenState(key, sealed); err != nil {
		t.Fatalf("expected a state cookie within clock skew tolerance to be accepted: %v", err)
	}
}

// TestValidateIssuedAtWorstCaseIsTTLPlusSkew exercises the documented
// worst case on StateTTL directly, using synthetic times rather than
// waiting for real time to elapse: a state issued clockSkew in the
// future (accepted, since that is within the future-skew tolerance)
// remains valid until StateTTL past that future-dated issuedAt, i.e.
// clockSkew plus StateTTL after the true moment it was issued. This is
// deliberate (see StateTTL's doc comment on why it is not itself the
// authoritative bound) rather than a bug: a state cookie's IssuedAt is
// part of the encrypted, authenticated payload, so it can only be ahead
// of another server's clock through legitimate clock drift between
// trusted server instances, never through attacker manipulation.
func TestValidateIssuedAtWorstCaseIsTTLPlusSkew(t *testing.T) {
	trueNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	issuedAt := trueNow.Add(clockSkew) // the most future-dated value still accepted

	// Just under clockSkew + StateTTL after the true moment of issuance:
	// still valid.
	stillValid := trueNow.Add(clockSkew + StateTTL - time.Second)
	if err := validateIssuedAt(issuedAt, stillValid); err != nil {
		t.Fatalf("expected acceptance just under the clockSkew+StateTTL worst-case boundary: %v", err)
	}

	// Just past clockSkew + StateTTL after the true moment of issuance:
	// expired.
	expired := trueNow.Add(clockSkew + StateTTL + time.Second)
	if err := validateIssuedAt(issuedAt, expired); err == nil {
		t.Fatal("expected the state to have expired at the documented clockSkew+StateTTL worst case")
	}
}

func TestNewLoginStateRandFailure(t *testing.T) {
	original := randRead
	t.Cleanup(func() { randRead = original })

	wantErr := errors.New("simulated rand failure")
	randRead = func(_ io.Reader, _ []byte) (int, error) {
		return 0, wantErr
	}

	state, err := NewLoginState("/")
	if err == nil {
		t.Fatal("expected an error when the random source fails")
	}
	if state != nil {
		t.Fatalf("expected a nil state on failure, got %+v", state)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped %q, got %v", wantErr, err)
	}
}

func TestSealStateMarshalFailure(t *testing.T) {
	original := marshalJSON
	t.Cleanup(func() { marshalJSON = original })

	wantErr := errors.New("simulated marshal failure")
	marshalJSON = func(_ any) ([]byte, error) {
		return nil, wantErr
	}

	if _, err := SealState(testKey(t), &LoginState{}); err == nil {
		t.Fatal("expected an error when marshaling fails")
	} else if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped %q, got %v", wantErr, err)
	}
}

func TestSealStateRejectsShortKey(t *testing.T) {
	badKey := []byte("too-short")
	if _, err := SealState(badKey, &LoginState{}); err == nil {
		t.Fatal("expected SealState to reject a key that is not exactly 32 bytes")
	}
}

func TestOpenStateRejectsShortKey(t *testing.T) {
	badKey := []byte("too-short")
	if _, err := OpenState(badKey, "irrelevant"); err == nil {
		t.Fatal("expected OpenState to reject a key that is not exactly 32 bytes")
	}
}

func TestOpenStateRejectsInvalidJSON(t *testing.T) {
	key := testKey(t)
	ciphertext, err := crypto.EncryptGCM(key, []byte("not json"))
	if err != nil {
		t.Fatalf("crypto.EncryptGCM: %v", err)
	}
	sealed := base64.RawURLEncoding.EncodeToString(ciphertext)

	if _, err := OpenState(key, sealed); err == nil {
		t.Fatal("expected invalid JSON in the decrypted payload to be refused")
	}
}

// TestOpenStateRejectsIncompleteState checks that a structurally
// incomplete LoginState (empty State, Nonce or CodeVerifier) is
// rejected, rather than opening successfully with those fields blank.
// Without this check, a caller that compares the callback's "state"
// query parameter against an opened state with an empty State field
// would treat an attacker-supplied "?state=" as a match.
func TestOpenStateRejectsIncompleteState(t *testing.T) {
	key := testKey(t)
	valid, err := NewLoginState("/")
	if err != nil {
		t.Fatalf("NewLoginState: %v", err)
	}

	cases := map[string]*LoginState{
		"empty state": {
			State: "", Nonce: valid.Nonce, CodeVerifier: valid.CodeVerifier,
			IssuedAt: valid.IssuedAt,
		},
		"empty nonce": {
			State: valid.State, Nonce: "", CodeVerifier: valid.CodeVerifier,
			IssuedAt: valid.IssuedAt,
		},
		"empty code verifier": {
			State: valid.State, Nonce: valid.Nonce, CodeVerifier: "",
			IssuedAt: valid.IssuedAt,
		},
		"short code verifier": {
			State: valid.State, Nonce: valid.Nonce, CodeVerifier: "too-short",
			IssuedAt: valid.IssuedAt,
		},
		"everything empty": {IssuedAt: valid.IssuedAt},
	}

	for name, state := range cases {
		t.Run(name, func(t *testing.T) {
			sealed, err := SealState(key, state)
			if err != nil {
				t.Fatalf("SealState: %v", err)
			}
			if _, err := OpenState(key, sealed); err == nil {
				t.Fatal("expected an incomplete login state to be refused")
			}
		})
	}
}

func TestOpenStateRejectsMalformedBase64(t *testing.T) {
	if _, err := OpenState(testKey(t), "not valid base64!!"); err == nil {
		t.Fatal("expected malformed base64 to be refused")
	}
}

func TestOpenStateRejectsTruncatedCiphertext(t *testing.T) {
	sealed := base64.RawURLEncoding.EncodeToString([]byte("short"))
	if _, err := OpenState(testKey(t), sealed); err == nil {
		t.Fatal("expected truncated ciphertext to be refused")
	}
}

func TestNewLoginStateProducesDistinctRandomValues(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 100; i++ {
		state, err := NewLoginState("/")
		if err != nil {
			t.Fatalf("NewLoginState: %v", err)
		}
		for _, value := range []string{state.State, state.Nonce, state.CodeVerifier} {
			if len(value) < 32 {
				t.Fatalf("value %q is too short to be unguessable", value)
			}
			if _, dup := seen[value]; dup {
				t.Fatalf("value %q repeated across states", value)
			}
			seen[value] = struct{}{}
		}
	}
}

func TestNewLoginStateSanitisesReturnPath(t *testing.T) {
	state, err := NewLoginState("https://evil.example.com")
	if err != nil {
		t.Fatalf("NewLoginState: %v", err)
	}
	if state.ReturnPath != "/" {
		t.Fatalf("ReturnPath = %q, want %q", state.ReturnPath, "/")
	}
}

func TestCodeChallengeIsS256OfVerifier(t *testing.T) {
	state := &LoginState{CodeVerifier: "test-verifier"}
	sum := sha256.Sum256([]byte("test-verifier"))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got := state.CodeChallenge(); got != want {
		t.Fatalf("CodeChallenge() = %q, want %q", got, want)
	}
}

// TestStateCookieNames pins the literal cookie name strings so a rename
// is a deliberate, reviewed act rather than an accidental refactor.
func TestStateCookieNames(t *testing.T) {
	if StateCookieName != "oidc_login_state" {
		t.Errorf("StateCookieName = %q, want %q", StateCookieName, "oidc_login_state")
	}
	if SecureStateCookieName != "__Host-oidc_login_state" {
		t.Errorf("SecureStateCookieName = %q, want %q", SecureStateCookieName, "__Host-oidc_login_state")
	}
}

func TestStateCookieNameFor(t *testing.T) {
	if got := StateCookieNameFor(true); got != SecureStateCookieName {
		t.Errorf("StateCookieNameFor(true) = %q, want %q", got, SecureStateCookieName)
	}
	if got := StateCookieNameFor(false); got != StateCookieName {
		t.Errorf("StateCookieNameFor(false) = %q, want %q", got, StateCookieName)
	}
	if !strings.HasPrefix(SecureStateCookieName, "__Host-") {
		t.Errorf("SecureStateCookieName %q does not carry the __Host- prefix", SecureStateCookieName)
	}
}

func TestSanitiseReturnPath(t *testing.T) {
	cases := map[string]string{
		"/dashboard":               "/dashboard",
		"/a/b?c=d":                 "/a/b?c=d",
		"":                         "/",
		"//evil.example.com":       "/",
		"/\\evil.example.com":      "/",
		"https://evil.example.com": "/",
		"http://evil.example.com":  "/",
		"relative/path":            "/",
		"javascript:alert(1)":      "/",
		"/path\nInjected: header":  "/",
		"/path\ttab":               "/",
		"/path\rcarriage":          "/",
		"/\x7fdel":                 "/",
		"/%2f%2fevil.example.com":  "/",
		"/%5cevil.example.com":     "/",
		"/%0ainjected":             "/",
		"/@evil.example.com":       "/@evil.example.com",
		"/foo/\\evil.example.com":  "/foo/\\evil.example.com",
		"/path?return=%2F%2Fx":     "/path?return=%2F%2Fx",
		"/%zz":                     "/",

		// Further backslash- and slash-prefix bypass attempts.
		"///evil.example.com":  "/",
		"/\\/evil.example.com": "/",
		"\\evil.example.com":   "/",

		// A dot-segment escape attempt. url.Parse does not resolve ".."
		// segments (that only happens when a browser resolves a relative
		// reference against a base URL), and since the value is used as
		// a path on this origin rather than resolved against one, this
		// stays a same-origin path and is accepted unchanged. This case
		// exists to document that intentionally: dot-segment removal
		// cannot be used to escape the origin here.
		"/..//evil.example.com": "/..//evil.example.com",
	}
	for input, want := range cases {
		if got := SanitiseReturnPath(input); got != want {
			t.Errorf("SanitiseReturnPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSanitiseReturnPathRejectsOverlongValue(t *testing.T) {
	overlong := "/" + strings.Repeat("a", maxReturnPathLength)
	if got := SanitiseReturnPath(overlong); got != "/" {
		t.Errorf("SanitiseReturnPath(<%d bytes>) = %q, want %q", len(overlong), got, "/")
	}

	// One byte under the limit (accounting for the leading "/") is still
	// accepted.
	atLimit := "/" + strings.Repeat("a", maxReturnPathLength-1)
	if got := SanitiseReturnPath(atLimit); got != atLimit {
		t.Errorf("SanitiseReturnPath(<%d bytes>) unexpectedly rejected", len(atLimit))
	}
}
