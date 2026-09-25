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
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/oidc"
)

// These tests cover issue #505: a sealed state cookie is free to obtain
// from the start endpoint and used to stay valid for its whole lifetime,
// so one cookie could be replayed against the callback with any number of
// bogus codes, spending the shared rate-limit allowance and sending the
// real client credentials to the provider's token endpoint every time.

// startFrom drives handleStart from the given client address.
func (e *oidcTestEnv) startFrom(remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, OIDCStartPath+"?return=%2Fdashboard", nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	e.handler.handleStart(rec, req)
	return rec
}

// callbackFrom drives handleCallback from the given client address with
// the given state cookie, state and code.
func (e *oidcTestEnv) callbackFrom(remoteAddr string, cookie *http.Cookie,
	state, code string) *httptest.ResponseRecorder {

	req := httptest.NewRequest(http.MethodGet, OIDCCallbackPath+"?"+url.Values{
		"code": {code}, "state": {state},
	}.Encode(), nil)
	req.RemoteAddr = remoteAddr
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.handler.handleCallback(rec, req)
	return rec
}

func TestCallbackRefusesAReplayedStateAfterASuccessfulLogin(t *testing.T) {
	_, env := newTestOIDCHandler(t)

	cookie := env.startLogin(t, "/dashboard")
	state := env.openState(t, cookie)
	env.idp.SetNextIDToken(env.idp.MintIDToken(t, map[string]any{
		"sub": "subject-1", "email": testUsername, "nonce": state.Nonce,
	}))
	first := env.callback(t, cookie, url.Values{
		"code": {"authorization-code"}, "state": {state.State},
	})
	if first.Code != http.StatusFound || findCookie(first, SessionCookieName) == nil {
		t.Fatalf("the genuine login did not succeed: status %d", first.Code)
	}

	// The same cookie and state again, with a fresh ID token staged so
	// that the only thing that can refuse it is the single-use rule.
	env.idp.SetNextIDToken(env.idp.MintIDToken(t, map[string]any{
		"sub": "subject-1", "email": testUsername, "nonce": state.Nonce,
	}))
	var replay *httptest.ResponseRecorder
	logged := captureLogDuring(t, func() {
		replay = env.callback(t, cookie, url.Values{
			"code": {"replayed-code"}, "state": {state.State},
		})
	})

	if replay.Code != http.StatusBadRequest {
		t.Fatalf("replay status = %d, want %d", replay.Code, http.StatusBadRequest)
	}
	if !strings.Contains(replay.Body.String(), genericCallbackError) {
		t.Errorf("replay body = %q, want the generic callback error", replay.Body.String())
	}
	requireNoSessionCookie(t, replay)
	if !strings.Contains(logged, "already been used") {
		t.Errorf("the replay was not logged; log was %q", logged)
	}
	if got := env.idp.LastTokenRequestForm().Get("code"); got != "authorization-code" {
		t.Errorf("the replay reached the token endpoint with code %q", got)
	}
}

// TestCallbackReplayNeverReachesTheProviderOrTheRateLimit is the attack
// in the issue: one state, many codes. Only the first presentation may
// reach the provider, and the rest must spend none of the allowance.
func TestCallbackReplayNeverReachesTheProviderOrTheRateLimit(t *testing.T) {
	_, env := newTestOIDCHandler(t)
	const client = "192.0.2.30:4000"

	cookie, state := mintStateCookie(t)
	if rec := env.callbackFrom(client, cookie, state.State, "code-0"); rec.Code != http.StatusFound {
		t.Fatalf("first presentation status = %d, want %d", rec.Code, http.StatusFound)
	}

	for attempt := range callbackRateMaxAttempts + 1 {
		code := "code-" + strings.Repeat("x", attempt%7+1)
		if rec := env.callbackFrom(client, cookie, state.State, code); rec.Code != http.StatusBadRequest {
			t.Fatalf("replay %d status = %d, want %d", attempt+1, rec.Code, http.StatusBadRequest)
		}
	}
	if got := env.idp.LastTokenRequestForm().Get("code"); got != "code-0" {
		t.Errorf("a replay reached the token endpoint with code %q", got)
	}

	// The client still has all but one unit of its allowance.
	if code := env.sendFailingExchange(t, client).Code; code == http.StatusTooManyRequests {
		t.Error("replayed states spent the callback's rate-limit allowance")
	}
}

// TestCallbackAcceptsAConcurrentlyReplayedStateOnce presents one state
// from many goroutines at once: exactly one may get past the single-use
// check. Run with -race.
func TestCallbackAcceptsAConcurrentlyReplayedStateOnce(t *testing.T) {
	_, env := newTestOIDCHandler(t)
	cookie, state := mintStateCookie(t)

	const presenters = 20
	codes := make(chan int, presenters)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range presenters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes <- env.callbackFrom("192.0.2.31:4000", cookie, state.State, "a-code").Code
		}()
	}
	close(start)
	wg.Wait()
	close(codes)

	// The one accepted presentation reaches the fake provider, which has
	// no ID token staged and so fails the exchange with a redirect.
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[http.StatusFound] != 1 || counts[http.StatusBadRequest] != presenters-1 {
		t.Errorf("status counts = %v, want one %d and %d of %d",
			counts, http.StatusFound, presenters-1, http.StatusBadRequest)
	}
}

func TestStartIsRateLimitedPerClientIP(t *testing.T) {
	_, env := newTestOIDCHandler(t)
	const client = "192.0.2.32:4000"

	for attempt := range startRateMaxAttempts {
		if code := env.startFrom(client).Code; code != http.StatusFound {
			t.Fatalf("start %d status = %d before the allowance ran out, want %d",
				attempt+1, code, http.StatusFound)
		}
	}

	rec := env.startFrom(client)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d after the allowance ran out, want %d",
			rec.Code, http.StatusTooManyRequests)
	}
	if findCookie(rec, oidc.StateCookieName) != nil {
		t.Error("a rate-limited start still minted a state cookie")
	}
	if rec.Header().Get("Location") != "" {
		t.Error("a rate-limited start still redirected to the provider")
	}

	// The limit is per IP, so another client is unaffected.
	if code := env.startFrom("192.0.2.33:4000").Code; code != http.StatusFound {
		t.Errorf("a different client IP got status %d, want %d", code, http.StatusFound)
	}
}

// TestStartRateLimitIsReturnedByASuccessfulLogin mirrors the callback's
// rule: working logins must not spend a budget that, without a trusted
// proxy list, is shared by everyone behind the reverse proxy.
func TestStartRateLimitIsReturnedByASuccessfulLogin(t *testing.T) {
	_, env := newTestOIDCHandler(t)
	const client = "192.0.2.34:4000"

	for range startRateMaxAttempts - 1 {
		env.startFrom(client)
	}

	rec := env.startFrom(client)
	if rec.Code != http.StatusFound {
		t.Fatalf("the last start within the allowance got status %d", rec.Code)
	}
	cookie := findCookie(rec, oidc.StateCookieName)
	state := env.openState(t, cookie)
	env.idp.SetNextIDToken(env.idp.MintIDToken(t, map[string]any{
		"sub": "subject-1", "email": testUsername, "nonce": state.Nonce,
	}))
	login := env.callbackFrom(client, cookie, state.State, "authorization-code")
	if login.Code != http.StatusFound || findCookie(login, SessionCookieName) == nil {
		t.Fatalf("the login did not succeed: status %d", login.Code)
	}

	if code := env.startFrom(client).Code; code != http.StatusFound {
		t.Errorf("a successful login did not return the start allowance: status %d", code)
	}
}
