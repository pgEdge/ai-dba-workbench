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
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/pkg/crypto"
)

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

func TestOpenStateRejectsTamperedValue(t *testing.T) {
	key := testKey(t)
	state, _ := NewLoginState("/")
	sealed, _ := SealState(key, state)

	tampered := sealed[:len(sealed)-2] + "AA"
	if _, err := OpenState(key, tampered); err == nil {
		t.Fatal("expected a tampered state cookie to be refused")
	}
}

func TestOpenStateRejectsWrongKey(t *testing.T) {
	state, _ := NewLoginState("/")
	sealed, _ := SealState(testKey(t), state)

	other := make([]byte, 32)
	other[0] = 1
	if _, err := OpenState(other, sealed); err == nil {
		t.Fatal("expected a state cookie sealed with another key to be refused")
	}
}

func TestOpenStateRejectsExpiredValue(t *testing.T) {
	key := testKey(t)
	state, _ := NewLoginState("/")
	state.IssuedAt = time.Now().Add(-StateTTL - time.Minute)
	sealed, _ := SealState(key, state)

	if _, err := OpenState(key, sealed); err == nil {
		t.Fatal("expected an expired state cookie to be refused")
	}
}

func TestOpenStateRejectsFutureValue(t *testing.T) {
	key := testKey(t)
	state, _ := NewLoginState("/")
	state.IssuedAt = time.Now().Add(10 * time.Minute)
	sealed, _ := SealState(key, state)

	if _, err := OpenState(key, sealed); err == nil {
		t.Fatal("expected an implausibly future state cookie to be refused")
	}
}

func TestOpenStateAcceptsSmallClockSkew(t *testing.T) {
	key := testKey(t)
	state, _ := NewLoginState("/")
	state.IssuedAt = time.Now().Add(30 * time.Second)
	sealed, _ := SealState(key, state)

	if _, err := OpenState(key, sealed); err != nil {
		t.Fatalf("expected a state cookie within clock skew tolerance to be accepted: %v", err)
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

func TestSealStateEncryptFailure(t *testing.T) {
	// A key that is not exactly 32 bytes makes the underlying AES-256-GCM
	// setup fail, exercising SealState's encryption error path without
	// needing to swap the encryptGCM hook.
	badKey := []byte("too-short")
	if _, err := SealState(badKey, &LoginState{}); err == nil {
		t.Fatal("expected an error when encryption fails")
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
	}
	for input, want := range cases {
		if got := SanitiseReturnPath(input); got != want {
			t.Errorf("SanitiseReturnPath(%q) = %q, want %q", input, got, want)
		}
	}
}
