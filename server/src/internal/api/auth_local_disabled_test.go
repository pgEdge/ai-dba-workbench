/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

// newLocalLoginTestStore builds an auth store holding a single known-good
// local account, so a test can present credentials that would certainly
// authenticate were local login switched on.
func newLocalLoginTestStore(t *testing.T) *auth.AuthStore {
	t.Helper()

	store, err := auth.NewAuthStore(t.TempDir(), 30, 5)
	if err != nil {
		t.Fatalf("creating auth store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	if err := store.CreateUser("testuser", "Testpass1234", "Test user", "", ""); err != nil {
		t.Fatalf("creating test user: %v", err)
	}
	return store
}

// postLogin sends one login request through the handler and returns the
// recorder, so the caller can compare whole responses rather than fields.
func postLogin(t *testing.T, handler *AuthHandler, req LoginRequest) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshaling login request: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.handleLogin(w, r)
	return w
}

// TestLoginRefusedWhenLocalAuthDisabled is the test that locks in the
// security control http.auth.local.enabled is documented to be. An
// operator who has completed a migration to an identity provider and set
// it false expects that no password in the store opens a session,
// including the break-glass administrator's.
func TestLoginRefusedWhenLocalAuthDisabled(t *testing.T) {
	store := newLocalLoginTestStore(t)

	handler := NewAuthHandler(store, nil, nil, false, false)
	defer handler.Close()

	w := postLogin(t, handler, LoginRequest{Username: "testuser", Password: "Testpass1234"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("correct credentials must be refused when local login is disabled: got status %d, body %s",
			w.Code, w.Body.String())
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == SessionCookieName && cookie.Value != "" {
			t.Fatal("a session cookie was issued despite local login being disabled")
		}
	}
}

// TestLoginRefusalMatchesTheBadPasswordResponse checks the property the
// refusal actually has: its body, status and headers are the
// wrong-password answer exactly, so it discloses nothing further about
// the credential that was presented or about whether the account
// exists. The comparison is over the whole response, so an added header
// or a differently worded message fails here.
//
// It is not a timing or a shape equivalence, and does not need to be:
// this path skips the bcrypt comparison and so answers much faster, and
// with local login off a malformed body is answered 401 rather than
// 400. The flag is public - the capabilities endpoint reports
// local_enabled so the login screen knows whether to draw the password
// form - so nothing is lost by a probe being able to work out that
// local login is switched off.
func TestLoginRefusalMatchesTheBadPasswordResponse(t *testing.T) {
	store := newLocalLoginTestStore(t)

	enabled := NewAuthHandler(store, nil, nil, false, true)
	defer enabled.Close()
	disabled := NewAuthHandler(store, nil, nil, false, false)
	defer disabled.Close()

	wrongPassword := postLogin(t, enabled, LoginRequest{Username: "testuser", Password: "Wrongpass1234"})
	if wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("expected the wrong-password path to answer 401, got %d", wrongPassword.Code)
	}

	// The same correct credentials, against a handler with local login
	// switched off.
	refused := postLogin(t, disabled, LoginRequest{Username: "testuser", Password: "Testpass1234"})

	if refused.Code != wrongPassword.Code {
		t.Errorf("status differs: disabled gave %d, wrong password gave %d",
			refused.Code, wrongPassword.Code)
	}
	if refused.Body.String() != wrongPassword.Body.String() {
		t.Errorf("body differs:\n disabled: %s\n wrong password: %s",
			refused.Body.String(), wrongPassword.Body.String())
	}
	if diff := headerDifference(refused.Result().Header, wrongPassword.Result().Header); diff != "" {
		t.Errorf("headers differ: %s", diff)
	}
}

// headerDifference reports the first header that distinguishes two
// responses, or the empty string when they carry the same headers.
func headerDifference(got, want http.Header) string {
	if len(got) != len(want) {
		return "header counts differ"
	}
	for name, wantValues := range want {
		gotValues, ok := got[name]
		if !ok {
			return name + " is missing"
		}
		if len(gotValues) != len(wantValues) {
			return name + " has a different number of values"
		}
		for i := range wantValues {
			if gotValues[i] != wantValues[i] {
				return name + " differs"
			}
		}
	}
	return ""
}

// TestLoginRefusalChargesTheRateLimiter checks that a refused request
// costs the caller an attempt just as a wrong password does, so that
// disabling local login does not hand an attacker an unmetered endpoint
// to hammer.
func TestLoginRefusalChargesTheRateLimiter(t *testing.T) {
	store := newLocalLoginTestStore(t)

	rateLimiter := auth.NewRateLimiter(15, 2)
	defer rateLimiter.Stop()

	extractor := auth.NewIPExtractor(nil)

	handler := NewAuthHandler(store, rateLimiter, extractor, false, false)
	defer handler.Close()

	for attempt := 1; attempt <= 2; attempt++ {
		if w := postLogin(t, handler, LoginRequest{Username: "testuser", Password: "Testpass1234"}); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", attempt, w.Code)
		}
	}

	w := postLogin(t, handler, LoginRequest{Username: "testuser", Password: "Testpass1234"})
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected the refusal path to exhaust the rate limiter, got status %d", w.Code)
	}
}

// TestLoginRefusalRespectsTheTotalRequestLimit checks the other limiter
// on the refusal path: with no failed-attempt limiter configured, the
// per-IP total-request budget must still be charged and must still stop
// the caller once it is spent.
func TestLoginRefusalRespectsTheTotalRequestLimit(t *testing.T) {
	store := newLocalLoginTestStore(t)

	handler := NewAuthHandler(store, nil, auth.NewIPExtractor(nil), false, false)
	defer handler.Close()

	// The internal total-request limiter allows 20 requests per minute
	// per IP, so the twenty-first must be turned away.
	for attempt := 1; attempt <= 20; attempt++ {
		if w := postLogin(t, handler, LoginRequest{Username: "testuser", Password: "Testpass1234"}); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", attempt, w.Code)
		}
	}

	w := postLogin(t, handler, LoginRequest{Username: "testuser", Password: "Testpass1234"})
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected the total-request limit to apply, got status %d", w.Code)
	}
}

// TestLoginRefusedBeforeTheBodyIsRead checks that the refusal does not
// depend on the request being well formed, which is what keeps the
// response the same whatever a probe sends.
func TestLoginRefusedBeforeTheBodyIsRead(t *testing.T) {
	store := newLocalLoginTestStore(t)

	handler := NewAuthHandler(store, nil, nil, false, false)
	defer handler.Close()

	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		bytes.NewReader([]byte("this is not json")))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.handleLogin(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected a malformed body to be refused with 401, got %d: %s", w.Code, w.Body.String())
	}
}
