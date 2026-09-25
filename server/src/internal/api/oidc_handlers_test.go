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
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/oidc"
	"github.com/pgedge/ai-workbench/server/internal/oidc/oidctest"
)

// boolPointer is the shorthand for the *bool configuration fields that
// distinguish "unset" from "explicitly false".
func boolPointer(value bool) *bool {
	return &value
}

// testStateKey is the 32-byte key the tests seal login state with. Its
// exact value is irrelevant; only its length is, since oidc.SealState
// enforces AES-256.
var testStateKey = []byte("0123456789abcdef0123456789abcdef")

// testUsername is the username claim value every test identity carries,
// and therefore the Workbench username a provisioned account gets.
const testUsername = "jane.doe@example.com"

// oidcTestEnv bundles a handler with the fake identity provider it talks
// to and the auth store it resolves users in, so that a test can drive a
// whole login without rebuilding the wiring each time.
type oidcTestEnv struct {
	handler *OIDCHandler
	idp     *oidctest.FakeIDP
	store   *auth.AuthStore
	dataDir string

	// lastState is the login state the most recent call to login sealed,
	// so a test can compare what the handler sent to the token endpoint
	// against what it actually held.
	lastState *oidc.LoginState
}

// newTestOIDCEnv stands up a handler against a fake identity provider.
// The optional mutators run over the OIDC configuration after the issuer
// and client credentials have been filled in, so a test states only the
// policy it cares about.
func newTestOIDCEnv(t *testing.T, mutators ...func(*config.OIDCConfig)) *oidcTestEnv {
	t.Helper()

	idp := oidctest.NewFakeIDP(t)

	cfg := config.OIDCConfig{
		Enabled:        config.BoolPtr(true),
		Issuer:         idp.Issuer(),
		ClientID:       idp.ClientID(),
		ClientSecret:   "test-client-secret",
		RedirectURL:    "https://workbench.example.com" + OIDCCallbackPath,
		UsernameClaim:  "email",
		GroupsClaim:    "groups",
		ProvisionUsers: boolPointer(true),
	}
	for _, mutate := range mutators {
		mutate(&cfg)
	}

	var provider *oidc.Provider
	if cfg.Issuer != "" {
		built, err := oidc.NewProvider(t.Context(), cfg)
		if err != nil {
			t.Fatalf("oidc.NewProvider: %v", err)
		}
		provider = built
	}

	dataDir := t.TempDir()
	store, err := auth.NewAuthStore(dataDir, 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("auth.NewAuthStore: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("closing the auth store: %v", closeErr)
		}
	})

	handler := NewOIDCHandler(store, provider, cfg, testStateKey, false, nil)
	t.Cleanup(handler.Close)

	return &oidcTestEnv{handler: handler, idp: idp, store: store, dataDir: dataDir}
}

// newTestOIDCHandler is the shorthand for a test that needs nothing but
// the handler itself.
func newTestOIDCHandler(t *testing.T) (*OIDCHandler, *oidcTestEnv) {
	t.Helper()

	env := newTestOIDCEnv(t)
	return env.handler, env
}

// startLogin drives handleStart and returns the state cookie it set.
func (e *oidcTestEnv) startLogin(t *testing.T, returnPath string) *http.Cookie {
	t.Helper()

	target := OIDCStartPath
	if returnPath != "" {
		target += "?return=" + url.QueryEscape(returnPath)
	}
	rec := httptest.NewRecorder()
	e.handler.handleStart(rec, httptest.NewRequest(http.MethodGet, target, nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("handleStart status = %d, want %d", rec.Code, http.StatusFound)
	}
	cookie := findCookie(rec, oidc.StateCookieName)
	if cookie == nil {
		t.Fatal("handleStart set no state cookie")
	}
	return cookie
}

// openState unseals a state cookie with the same key the handler used,
// so a test can read the state, nonce and return path out of it.
func (e *oidcTestEnv) openState(t *testing.T, cookie *http.Cookie) *oidc.LoginState {
	t.Helper()

	state, err := oidc.OpenState(testStateKey, cookie.Value)
	if err != nil {
		t.Fatalf("oidc.OpenState: %v", err)
	}
	return state
}

// callback drives handleCallback with the supplied query parameters and
// state cookie. A nil cookie sends no cookie at all.
func (e *oidcTestEnv) callback(t *testing.T, cookie *http.Cookie,
	query url.Values) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, OIDCCallbackPath+"?"+query.Encode(), nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.handler.handleCallback(rec, req)
	return rec
}

// login runs a complete round trip: start, mint an ID token carrying the
// state's nonce plus the supplied claims, and call back.
func (e *oidcTestEnv) login(t *testing.T, returnPath string,
	claims map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	cookie := e.startLogin(t, returnPath)
	state := e.openState(t, cookie)
	e.lastState = state

	payload := map[string]any{"sub": "subject-1", "email": testUsername}
	for name, value := range claims {
		payload[name] = value
	}
	payload["nonce"] = state.Nonce
	e.idp.SetNextIDToken(e.idp.MintIDToken(t, payload))

	return e.callback(t, cookie, url.Values{
		"code":  {"authorization-code"},
		"state": {state.State},
	})
}

// openAuthDB opens a second connection to the handler's auth database so
// that a test can put it into a state the public API cannot produce, such
// as a row that carries an external subject but is not federated.
func (e *oidcTestEnv) openAuthDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(e.dataDir, "auth.db"))
	if err != nil {
		t.Fatalf("opening the auth database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("closing the auth database: %v", closeErr)
		}
	})
	return db
}

// mintStateCookie seals a fresh login state exactly as handleStart
// would, without going through handleStart and so without spending its
// rate-limit allowance.
func mintStateCookie(t *testing.T) (*http.Cookie, *oidc.LoginState) {
	t.Helper()

	state, err := oidc.NewLoginState("/dashboard")
	if err != nil {
		t.Fatalf("oidc.NewLoginState: %v", err)
	}
	sealed, err := oidc.SealState(testStateKey, state)
	if err != nil {
		t.Fatalf("oidc.SealState: %v", err)
	}
	// A request cookie carries only its name and value; the attributes
	// are set to match what the handler issues.
	return &http.Cookie{
		Name:     oidc.StateCookieName,
		Value:    sealed,
		HttpOnly: true,
		Secure:   true,
	}, state
}

// findCookie returns the named cookie from a response, or nil.
func findCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// requireNoSessionCookie fails the test if a usable session cookie was
// set, which is the invariant every refused login shares.
func requireNoSessionCookie(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == SessionCookieName && cookie.Value != "" {
			t.Fatal("a session cookie was set despite the failure")
		}
	}
}

// captureLogDuring runs fn with the standard logger redirected and
// returns what was written, so that a test can assert on what reached
// the server log rather than on what reached the browser. It builds on
// the captureLog helper in handle_test.go.
func captureLogDuring(t *testing.T, fn func()) string {
	t.Helper()

	buf := captureLog(t)
	fn()
	return buf.String()
}

// =============================================================================
// handleStart
// =============================================================================

func TestStartRedirectsToProviderAndSetsStateCookie(t *testing.T) {
	handler, _ := newTestOIDCHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, OIDCStartPath+"?return=/dashboard", nil)
	handler.handleStart(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	location := rec.Header().Get("Location")
	if location == "" {
		t.Fatal("no Location header")
	}
	authURL, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parsing the Location header: %v", err)
	}
	if got := authURL.Query().Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", got)
	}
	if authURL.Query().Get("code_challenge") == "" {
		t.Error("the authorization URL carried no PKCE code challenge")
	}

	stateCookie := findCookie(rec, oidc.StateCookieName)
	if stateCookie == nil {
		t.Fatal("state cookie was not set")
	}
	if !stateCookie.HttpOnly {
		t.Error("state cookie must be HttpOnly")
	}
	if stateCookie.SameSite != http.SameSiteLaxMode {
		t.Error("state cookie must be SameSite=Lax to survive the provider redirect")
	}
	if stateCookie.Path != "/" {
		t.Errorf("state cookie Path = %q, want /", stateCookie.Path)
	}
	if stateCookie.Domain != "" {
		t.Errorf("state cookie Domain = %q, want it unset", stateCookie.Domain)
	}
	if stateCookie.Secure {
		t.Error("state cookie must not be Secure on a plain-HTTP request")
	}
	if want := int(oidc.StateTTL.Seconds()); stateCookie.MaxAge != want {
		t.Errorf("state cookie MaxAge = %d, want %d", stateCookie.MaxAge, want)
	}

	// The state parameter in the authorization URL must be the one
	// sealed in the cookie, or the callback could never match them.
	state, err := oidc.OpenState(testStateKey, stateCookie.Value)
	if err != nil {
		t.Fatalf("oidc.OpenState: %v", err)
	}
	if got := authURL.Query().Get("state"); got != state.State {
		t.Errorf("authorization URL state = %q, want the sealed %q", got, state.State)
	}
	if state.ReturnPath != "/dashboard" {
		t.Errorf("sealed return path = %q, want /dashboard", state.ReturnPath)
	}
}

func TestStartSanitisesAHostileReturnPath(t *testing.T) {
	_, env := newTestOIDCHandler(t)

	cookie := env.startLogin(t, "//evil.example.com/steal")
	if got := env.openState(t, cookie).ReturnPath; got != "/" {
		t.Errorf("sealed return path = %q, want / for an off-origin value", got)
	}
}

func TestStartRedirectsToTheLoginScreenWhenOIDCIsDisabled(t *testing.T) {
	env := newTestOIDCEnv(t, func(cfg *config.OIDCConfig) {
		cfg.Enabled = config.BoolPtr(false)
	})

	rec := httptest.NewRecorder()
	env.handler.handleStart(rec, httptest.NewRequest(http.MethodGet, OIDCStartPath, nil))

	// A full-page navigation, so the browser must land somewhere it can
	// be used: the login screen with a marker, not an error page.
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d so the browser lands on the login screen",
			rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != providerFailedTarget {
		t.Fatalf("Location = %q, want %q", got, providerFailedTarget)
	}

	rec = httptest.NewRecorder()
	env.handler.handleCallback(rec, httptest.NewRequest(http.MethodGet, OIDCCallbackPath, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("callback status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStartRedirectsWhenNoProviderWasDiscovered(t *testing.T) {
	env := newTestOIDCEnv(t)
	handler := NewOIDCHandler(env.store, nil, config.OIDCConfig{Enabled: config.BoolPtr(true)},
		testStateKey, false, nil)
	defer handler.Close()

	rec := httptest.NewRecorder()
	handler.handleStart(rec, httptest.NewRequest(http.MethodGet, OIDCStartPath, nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != providerFailedTarget {
		t.Fatalf("Location = %q, want %q", got, providerFailedTarget)
	}
}

func TestStartFailsClosedWhenTheStateKeyIsUnusable(t *testing.T) {
	env := newTestOIDCEnv(t)
	handler := NewOIDCHandler(env.store, env.handler.provider, env.handler.cfg,
		[]byte("too-short"), false, nil)
	defer handler.Close()

	rec := httptest.NewRecorder()
	handler.handleStart(rec, httptest.NewRequest(http.MethodGet, OIDCStartPath, nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if findCookie(rec, oidc.StateCookieName) != nil {
		t.Error("a state cookie was set despite the sealing failure")
	}
}

func TestStartAndCallbackRejectNonGETMethods(t *testing.T) {
	handler, _ := newTestOIDCHandler(t)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		for name, serve := range map[string]http.HandlerFunc{
			"start":    handler.handleStart,
			"callback": handler.handleCallback,
		} {
			rec := httptest.NewRecorder()
			serve(rec, httptest.NewRequest(method, OIDCStartPath, nil))

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s status = %d, want %d",
					method, name, rec.Code, http.StatusMethodNotAllowed)
			}
			if got := rec.Header().Get("Allow"); got != http.MethodGet {
				t.Errorf("%s %s Allow = %q, want GET", method, name, got)
			}
		}
	}
}

func TestStartUsesTheHostPrefixedCookieOverHTTPS(t *testing.T) {
	env := newTestOIDCEnv(t)
	handler := NewOIDCHandler(env.store, env.handler.provider, env.handler.cfg,
		testStateKey, true, nil)
	defer handler.Close()

	rec := httptest.NewRecorder()
	handler.handleStart(rec, httptest.NewRequest(http.MethodGet, OIDCStartPath, nil))

	cookie := findCookie(rec, oidc.SecureStateCookieName)
	if cookie == nil {
		t.Fatalf("no %s cookie was set over TLS", oidc.SecureStateCookieName)
	}
	if !cookie.Secure {
		t.Error("the __Host- prefixed cookie must be Secure or a browser refuses it")
	}
	if cookie.Domain != "" {
		t.Errorf("Domain = %q, want it unset for a __Host- cookie", cookie.Domain)
	}
}

func TestSecureRequestDerivationMatchesTheAuthHandler(t *testing.T) {
	// A request that really did arrive through the trusted proxy, and
	// one carrying the same header from somewhere else. The difference
	// between them is the whole point: an extractor exists on every
	// deployment, so "is there an extractor" would answer yes for both.
	const trustedCIDR = "192.0.2.0/24"
	trusted := auth.NewIPExtractor([]string{trustedCIDR})
	untrusted := auth.NewIPExtractor(nil)

	fromProxy := func(header, value string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, OIDCStartPath, nil)
		req.RemoteAddr = "192.0.2.40:5000"
		req.Header.Set(header, value)
		return req
	}
	fromElsewhere := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, OIDCStartPath, nil)
		req.RemoteAddr = "203.0.113.9:5000"
		req.Header.Set("X-Forwarded-Proto", "https")
		return req
	}

	cases := map[string]struct {
		tlsEnabled bool
		extractor  *auth.IPExtractor
		request    *http.Request
		wantSecure bool
	}{
		"plain HTTP": {
			request: httptest.NewRequest(http.MethodGet, OIDCStartPath, nil),
		},
		"server terminates TLS": {
			tlsEnabled: true,
			request:    httptest.NewRequest(http.MethodGet, OIDCStartPath, nil),
			wantSecure: true,
		},
		"connection Go itself terminated": {
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, OIDCStartPath, nil)
				req.TLS = &tls.ConnectionState{}
				return req
			}(),
			wantSecure: true,
		},
		"forwarded header from a trusted proxy": {
			extractor:  trusted,
			request:    fromProxy("X-Forwarded-Proto", "https"),
			wantSecure: true,
		},
		"forwarded header in upper case from a trusted proxy": {
			// A proxy writing "HTTPS" is describing the same deployment;
			// reading it as plain HTTP would silently drop the Secure
			// attribute and the "__Host-" cookie prefix.
			extractor:  trusted,
			request:    fromProxy("X-Forwarded-Proto", "HTTPS"),
			wantSecure: true,
		},
		"forwarded header from a client that is not the proxy": {
			// The Secure attribute follows the header whenever an
			// extractor exists: a forged header can only cost the forger
			// their own cookie. The "__Host-" prefix does not follow it,
			// because the request has not been shown to come from a
			// proxy worth believing.
			extractor:  trusted,
			request:    fromElsewhere(),
			wantSecure: true,
		},
		"forwarded header with an extractor that trusts nothing": {
			// The shipped default: a TLS-terminating proxy in front and
			// http.trusted_proxies empty. Secure must survive, or the
			// session cookie would travel in clear on any plain-HTTP
			// request to the host; the prefix is withheld, since any
			// client could have set the header.
			extractor:  untrusted,
			request:    fromProxy("X-Forwarded-Proto", "https"),
			wantSecure: true,
		},
		"forwarded header with no extractor at all": {
			request: fromProxy("X-Forwarded-Proto", "https"),
		},
		"forwarded header naming plain HTTP": {
			extractor: trusted,
			request:   fromProxy("X-Forwarded-Proto", "http"),
		},
	}

	// The prefix rule agrees with the attribute rule except where the
	// forwarded header is the only evidence and the request did not come
	// from a listed proxy.
	wantPrefixed := map[string]bool{
		"server terminates TLS":                               true,
		"connection Go itself terminated":                     true,
		"forwarded header from a trusted proxy":               true,
		"forwarded header in upper case from a trusted proxy": true,
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := requestIsSecure(tc.request, tc.tlsEnabled, tc.extractor); got != tc.wantSecure {
				t.Errorf("requestIsSecure = %v, want %v", got, tc.wantSecure)
			}
			if got := requestIsProvablySecure(tc.request, tc.tlsEnabled, tc.extractor); got != wantPrefixed[name] {
				t.Errorf("requestIsProvablySecure = %v, want %v", got, wantPrefixed[name])
			}
			if wantPrefixed[name] && !tc.wantSecure {
				t.Error("the test table is inconsistent: a prefixed cookie must also be Secure")
			}
			// The AuthHandler must agree, since the two handlers set
			// cookies the browser is expected to treat alike.
			authHandler := &AuthHandler{tlsEnabled: tc.tlsEnabled, ipExtractor: tc.extractor}
			if got := authHandler.isSecureRequest(tc.request); got != tc.wantSecure {
				t.Errorf("AuthHandler.isSecureRequest = %v, want %v", got, tc.wantSecure)
			}
		})
	}
}

// =============================================================================
// handleCallback: state handling
// =============================================================================

func TestCallbackWithoutStateCookieIsRefused(t *testing.T) {
	handler, _ := newTestOIDCHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, OIDCCallbackPath+"?code=c&state=s", nil)
	handler.handleCallback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	requireNoSessionCookie(t, rec)
}

func TestCallbackRejectsStateParameterMismatch(t *testing.T) {
	_, env := newTestOIDCHandler(t)

	cookie := env.startLogin(t, "/dashboard")
	rec := env.callback(t, cookie, url.Values{
		"code":  {"authorization-code"},
		"state": {"a-different-state-value"},
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	requireNoSessionCookie(t, rec)

	// The mismatch must be caught before the code is redeemed, or a
	// forged callback would still make the server call the provider.
	if env.idp.LastTokenRequestForm() != nil {
		t.Error("the authorization code was exchanged despite the state mismatch")
	}
}

func TestCallbackRejectsAnUnsealableStateCookie(t *testing.T) {
	_, env := newTestOIDCHandler(t)

	rec := env.callback(t, &http.Cookie{Name: oidc.StateCookieName, Value: "not-sealed-with-our-key"},
		url.Values{"code": {"c"}, "state": {"s"}})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	requireNoSessionCookie(t, rec)
}

func TestCallbackWithoutAnAuthorizationCodeIsRefused(t *testing.T) {
	_, env := newTestOIDCHandler(t)

	cookie := env.startLogin(t, "/")
	state := env.openState(t, cookie)
	rec := env.callback(t, cookie, url.Values{"state": {state.State}})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	requireNoSessionCookie(t, rec)
}

func TestCallbackClearsTheStateCookieOnEveryPath(t *testing.T) {
	assertCleared := func(t *testing.T, rec *httptest.ResponseRecorder, when string) {
		t.Helper()

		cleared := findCookie(rec, oidc.StateCookieName)
		if cleared == nil {
			t.Fatalf("%s: the state cookie was not cleared", when)
		}
		if cleared.Value != "" || cleared.MaxAge != -1 {
			t.Fatalf("%s: state cookie = %q with MaxAge %d, want an empty value and MaxAge -1",
				when, cleared.Value, cleared.MaxAge)
		}
		if cleared.Path != "/" || !cleared.HttpOnly || cleared.SameSite != http.SameSiteLaxMode {
			t.Errorf("%s: the clearing cookie's attributes do not mirror the ones it must replace",
				when)
		}
	}

	t.Run("failure", func(t *testing.T) {
		_, env := newTestOIDCHandler(t)
		cookie := env.startLogin(t, "/")
		assertCleared(t, env.callback(t, cookie, url.Values{"state": {"wrong"}}), "on failure")
	})

	t.Run("success", func(t *testing.T) {
		_, env := newTestOIDCHandler(t)
		assertCleared(t, env.login(t, "/dashboard", nil), "on success")
	})
}

// =============================================================================
// handleCallback: the happy path
// =============================================================================

func TestCallbackSetsSessionCookieAndRedirectsToReturnPath(t *testing.T) {
	_, env := newTestOIDCHandler(t)

	rec := env.login(t, "/dashboard", map[string]any{"name": "Jane Doe"})

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusFound, rec.Body.String())
	}

	// PKCE is only protection if the verifier actually reaches the token
	// request: without it, whoever intercepted the redirect could redeem
	// the code themselves.
	form := env.idp.LastTokenRequestForm()
	if form == nil {
		t.Fatal("the token endpoint was never called")
	}
	// Establish that there is a verifier before comparing against it.
	// url.Values.Get answers "" for a key that is absent, so without
	// this the comparison would hold just as well against a client that
	// sent no code_verifier at all, which is to say against PKCE having
	// been removed outright.
	if env.lastState.CodeVerifier == "" {
		t.Fatal("the sealed login state carries no PKCE code verifier")
	}
	if got := form.Get("code_verifier"); got != env.lastState.CodeVerifier {
		t.Errorf("code_verifier sent = %q, want the sealed %q", got, env.lastState.CodeVerifier)
	}
	if got := rec.Header().Get("Location"); got != "/dashboard" {
		t.Fatalf("Location = %q, want /dashboard", got)
	}

	session := findCookie(rec, SessionCookieName)
	if session == nil || session.Value == "" {
		t.Fatal("no session cookie was set")
	}
	if !session.HttpOnly || session.SameSite != http.SameSiteLaxMode || session.Path != "/" {
		t.Error("the session cookie attributes must match the ones handleLogin sets")
	}
	if session.Expires.IsZero() {
		t.Error("the session cookie must carry the session's expiry")
	}

	username, err := env.store.ValidateSessionToken(session.Value)
	if err != nil {
		t.Fatalf("ValidateSessionToken: %v", err)
	}
	if username != testUsername {
		t.Errorf("session username = %q, want %q", username, testUsername)
	}

	user, err := env.store.GetUser(testUsername)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.AuthSource != auth.AuthSourceOIDC {
		t.Errorf("auth_source = %q, want %q", user.AuthSource, auth.AuthSourceOIDC)
	}
	if user.DisplayName != "Jane Doe" && env.handler.cfg.DisplayNameClaim != "" {
		t.Errorf("display name = %q", user.DisplayName)
	}
}

func TestCallbackDoesNotDecodeTheReturnPath(t *testing.T) {
	_, env := newTestOIDCHandler(t)

	// A percent-encoded value SanitiseReturnPath accepts must reach the
	// Location header exactly as it was sealed: decoding it here would
	// reintroduce the bypasses that function exists to reject.
	const encoded = "/reports/%2e%2e%2ffoo"
	rec := env.login(t, encoded, nil)

	if got := rec.Header().Get("Location"); got != encoded {
		t.Fatalf("Location = %q, want the sealed %q verbatim", got, encoded)
	}
}

func TestCallbackReconcilesGroupsOnEveryLogin(t *testing.T) {
	env := newTestOIDCEnv(t, func(cfg *config.OIDCConfig) {
		cfg.GroupMap = map[string]string{"idp-eng": "engineers", "idp-ops": "operators"}
	})

	engineers, err := env.store.CreateGroup("engineers", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	operators, err := env.store.CreateGroup("operators", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	if rec := env.login(t, "/", map[string]any{"groups": []any{"idp-eng"}}); rec.Code != http.StatusFound {
		t.Fatalf("first login status = %d", rec.Code)
	}
	userID, err := env.store.GetUserID(testUsername)
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	groups, err := env.store.GetUserGroups(userID)
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if !slices.Contains(groups, engineers) || slices.Contains(groups, operators) {
		t.Fatalf("after the first login groups = %v, want only engineers (%d)", groups, engineers)
	}

	if rec := env.login(t, "/", map[string]any{"groups": []any{"idp-ops"}}); rec.Code != http.StatusFound {
		t.Fatalf("second login status = %d", rec.Code)
	}
	groups, err = env.store.GetUserGroups(userID)
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if slices.Contains(groups, engineers) || !slices.Contains(groups, operators) {
		t.Fatalf("after the second login groups = %v, want only operators (%d)", groups, operators)
	}
}

// =============================================================================
// handleCallback: refusals
// =============================================================================

func TestCallbackPropagatesProviderError(t *testing.T) {
	_, env := newTestOIDCHandler(t)

	cookie := env.startLogin(t, "/dashboard")
	var rec *httptest.ResponseRecorder
	logged := captureLogDuring(t, func() {
		rec = env.callback(t, cookie, url.Values{
			"error":             {"access_denied"},
			"error_description": {"the user said no"},
		})
	})

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != "/?login_error=provider" {
		t.Fatalf("Location = %q, want /?login_error=provider", got)
	}
	if body := rec.Body.String(); body != "" {
		t.Fatalf("body = %q, want it empty so nothing is reflected back", body)
	}
	if strings.Contains(rec.Header().Get("Location"), "access_denied") {
		t.Error("the provider's error text leaked into the redirect")
	}
	if !strings.Contains(logged, "access_denied") {
		t.Errorf("the provider's error was not logged; log was %q", logged)
	}
}

func TestCallbackRefusesUnknownSubjectWithoutProvisioning(t *testing.T) {
	env := newTestOIDCEnv(t, func(cfg *config.OIDCConfig) {
		cfg.ProvisionUsers = boolPointer(false)
	})

	rec := env.login(t, "/dashboard", nil)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != loginFailedTarget {
		t.Fatalf("Location = %q, want %q", got, loginFailedTarget)
	}
	requireNoSessionCookie(t, rec)

	user, err := env.store.GetUser(testUsername)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user != nil {
		t.Fatal("a user was provisioned even though provisioning is disabled")
	}
}

func TestCallbackRefusesDisallowedEmailDomain(t *testing.T) {
	env := newTestOIDCEnv(t, func(cfg *config.OIDCConfig) {
		cfg.AllowedEmailDomains = []string{"example.com"}
	})

	rec := env.login(t, "/dashboard", map[string]any{
		"email":          "jane@other.example.net",
		"email_verified": true,
	})

	if got := rec.Header().Get("Location"); got != loginFailedTarget {
		t.Fatalf("Location = %q, want %q", got, loginFailedTarget)
	}
	requireNoSessionCookie(t, rec)
	user, err := env.store.GetUser("jane@other.example.net")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user != nil {
		t.Fatal("the refused identity was provisioned anyway")
	}
}

func TestCallbackRefusesAnUnverifiedEmailAddress(t *testing.T) {
	env := newTestOIDCEnv(t, func(cfg *config.OIDCConfig) {
		cfg.AllowedEmailDomains = []string{"example.com"}
	})

	// The address is in the permitted domain, but the provider has not
	// verified it, so it is the user's own assertion and must not
	// satisfy the domain list.
	rec := env.login(t, "/dashboard", map[string]any{"email_verified": false})

	if got := rec.Header().Get("Location"); got != loginFailedTarget {
		t.Fatalf("Location = %q, want %q", got, loginFailedTarget)
	}
	requireNoSessionCookie(t, rec)
}

func TestCallbackAcceptsAVerifiedAddressInAnAllowedDomain(t *testing.T) {
	env := newTestOIDCEnv(t, func(cfg *config.OIDCConfig) {
		cfg.AllowedEmailDomains = []string{"@EXAMPLE.COM"}
	})

	rec := env.login(t, "/dashboard", map[string]any{"email_verified": "true"})

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/dashboard" {
		t.Fatalf("status = %d, Location = %q; want a successful login",
			rec.Code, rec.Header().Get("Location"))
	}
	if findCookie(rec, SessionCookieName) == nil {
		t.Fatal("no session cookie was set")
	}
}

func TestCallbackRefusesWhenTheExchangeFails(t *testing.T) {
	_, env := newTestOIDCHandler(t)

	cookie := env.startLogin(t, "/dashboard")
	state := env.openState(t, cookie)

	// No ID token is staged, so the fake provider answers the token
	// request with an OAuth 2.0 error carrying a response body.
	var rec *httptest.ResponseRecorder
	logged := captureLogDuring(t, func() {
		rec = env.callback(t, cookie, url.Values{
			"code":  {"authorization-code"},
			"state": {state.State},
		})
	})

	if got := rec.Header().Get("Location"); got != loginFailedTarget {
		t.Fatalf("Location = %q, want %q", got, loginFailedTarget)
	}
	if body := rec.Body.String(); body != "" {
		t.Fatalf("body = %q, want it empty", body)
	}
	if strings.Contains(rec.Header().Get("Location"), "invalid_grant") {
		t.Error("the provider's response body leaked into the redirect")
	}
	if !strings.Contains(logged, "exchange failed") {
		t.Errorf("the exchange failure was not logged; log was %q", logged)
	}
	requireNoSessionCookie(t, rec)
}

func TestCallbackRefusesAReplayedNonce(t *testing.T) {
	_, env := newTestOIDCHandler(t)

	cookie := env.startLogin(t, "/dashboard")
	state := env.openState(t, cookie)
	env.idp.SetNextIDToken(env.idp.MintIDToken(t, map[string]any{
		"sub": "subject-1", "email": testUsername, "nonce": "some-other-logins-nonce",
	}))

	rec := env.callback(t, cookie, url.Values{
		"code":  {"authorization-code"},
		"state": {state.State},
	})

	if got := rec.Header().Get("Location"); got != loginFailedTarget {
		t.Fatalf("Location = %q, want %q", got, loginFailedTarget)
	}
	requireNoSessionCookie(t, rec)
}

// TestCallbackFailuresAreIndistinguishable is the enumeration guard. The
// errors from ResolveFederatedUser deliberately name the real reason for
// the log, and the username-collision case quotes the stored spelling of
// the colliding local account; if any of that reached the browser, an
// attacker holding a self-registered identity at a permissive provider
// could enumerate local usernames and their exact casing.
func TestCallbackFailuresAreIndistinguishable(t *testing.T) {
	type scenario struct {
		name    string
		mutate  func(*config.OIDCConfig)
		prepare func(t *testing.T, env *oidcTestEnv)
	}

	scenarios := []scenario{
		{
			name:    "unknown subject",
			mutate:  func(cfg *config.OIDCConfig) { cfg.ProvisionUsers = boolPointer(false) },
			prepare: func(*testing.T, *oidcTestEnv) {},
		},
		{
			name:   "disabled account",
			mutate: func(*config.OIDCConfig) {},
			prepare: func(t *testing.T, env *oidcTestEnv) {
				if rec := env.login(t, "/", nil); rec.Code != http.StatusFound {
					t.Fatalf("seeding login failed with status %d", rec.Code)
				}
				if err := env.store.DisableUser(testUsername); err != nil {
					t.Fatalf("DisableUser: %v", err)
				}
			},
		},
		{
			name:   "username collision",
			mutate: func(*config.OIDCConfig) {},
			prepare: func(t *testing.T, env *oidcTestEnv) {
				if err := env.store.CreateUser(testUsername, "a-strong-local-password",
					"", "Local Jane", ""); err != nil {
					t.Fatalf("CreateUser: %v", err)
				}
			},
		},
		{
			name:   "not federated",
			mutate: func(*config.OIDCConfig) {},
			prepare: func(t *testing.T, env *oidcTestEnv) {
				if rec := env.login(t, "/", nil); rec.Code != http.StatusFound {
					t.Fatalf("seeding login failed with status %d", rec.Code)
				}
				// A row carrying an external subject but marked local is
				// exactly what a federated login must never adopt.
				db := env.openAuthDB(t)
				if _, err := db.Exec(
					"UPDATE users SET auth_source = 'local' WHERE username = ?",
					testUsername); err != nil {
					t.Fatalf("marking the account local: %v", err)
				}
			},
		},
	}

	responses := make(map[string]string, len(scenarios))
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			env := newTestOIDCEnv(t, sc.mutate)
			sc.prepare(t, env)

			rec := env.login(t, "/dashboard", nil)
			requireNoSessionCookie(t, rec)
			responses[sc.name] = describeResponse(rec)
		})
	}

	if len(responses) != len(scenarios) {
		t.Fatalf("only %d of %d scenarios produced a response", len(responses), len(scenarios))
	}

	reference := responses[scenarios[0].name]
	for name, got := range responses {
		if got != reference {
			t.Errorf("the %q response differs from %q:\n got: %s\nwant: %s",
				name, scenarios[0].name, got, reference)
		}
	}
	if !strings.Contains(reference, loginFailedTarget) {
		t.Errorf("the shared response does not redirect to %q: %s", loginFailedTarget, reference)
	}
}

// describeResponse renders a response as the browser would see it, so
// two of them can be compared byte for byte. The session cookie is
// included deliberately: "no session was set" is part of what must match.
func describeResponse(rec *httptest.ResponseRecorder) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "status=%d\n", rec.Code)

	names := make([]string, 0, len(rec.Header()))
	for name := range rec.Header() {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		values := slices.Clone(rec.Header().Values(name))
		slices.Sort(values)
		fmt.Fprintf(&sb, "%s: %s\n", name, strings.Join(values, "; "))
	}

	fmt.Fprintf(&sb, "body=%q", rec.Body.String())
	return sb.String()
}

// TestCallbackRefusesWhenGroupReconciliationFails covers the rule that a
// partially reconciled user must not receive a session.
func TestCallbackRefusesWhenGroupReconciliationFails(t *testing.T) {
	env := newTestOIDCEnv(t, func(cfg *config.OIDCConfig) {
		cfg.GroupMap = map[string]string{"idp-eng": "engineers"}
	})
	if _, err := env.store.CreateGroup("engineers", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	// Seed the account, then take the membership table out from under
	// reconciliation so that resolving the user still succeeds and only
	// the group work fails.
	if rec := env.login(t, "/", map[string]any{"groups": []any{"idp-eng"}}); rec.Code != http.StatusFound {
		t.Fatalf("seeding login status = %d", rec.Code)
	}
	db := env.openAuthDB(t)
	if _, err := db.Exec("ALTER TABLE group_memberships RENAME TO group_memberships_moved"); err != nil {
		t.Fatalf("moving group_memberships aside: %v", err)
	}

	var rec *httptest.ResponseRecorder
	logged := captureLogDuring(t, func() {
		rec = env.login(t, "/dashboard", map[string]any{"groups": []any{"idp-eng"}})
	})

	if got := rec.Header().Get("Location"); got != loginFailedTarget {
		t.Fatalf("Location = %q, want %q", got, loginFailedTarget)
	}
	requireNoSessionCookie(t, rec)
	if !strings.Contains(logged, "Refusing federated login") {
		t.Errorf("the reconciliation failure was not logged; log was %q", logged)
	}
}

// =============================================================================
// Rate limiting and logging
// =============================================================================

// sendFailingExchange drives a callback that passes every check up to
// the code exchange and then fails at the fake provider, which has no
// ID token staged and so answers invalid_grant. It is the cheapest
// request that spends rate-limit allowance, because the allowance is
// charged immediately before the call to the provider and nowhere
// earlier. The state is minted directly rather than through handleStart,
// so that these tests of the callback's allowance do not also spend the
// start endpoint's.
func (e *oidcTestEnv) sendFailingExchange(t *testing.T, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()

	cookie, state := mintStateCookie(t)
	req := httptest.NewRequest(http.MethodGet, OIDCCallbackPath+"?"+url.Values{
		"code": {"unredeemable-code"}, "state": {state.State},
	}.Encode(), nil)
	req.RemoteAddr = remoteAddr
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.handler.handleCallback(rec, req)
	return rec
}

func TestCallbackIsRateLimitedPerClientIP(t *testing.T) {
	_, env := newTestOIDCHandler(t)

	for attempt := range callbackRateMaxAttempts {
		if code := env.sendFailingExchange(t, "192.0.2.10:4000").Code; code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was rate limited before the allowance ran out", attempt+1)
		}
	}
	if code := env.sendFailingExchange(t, "192.0.2.10:4000").Code; code != http.StatusTooManyRequests {
		t.Fatalf("status = %d after the allowance ran out, want %d",
			code, http.StatusTooManyRequests)
	}

	// The limit is per IP, so another client is unaffected.
	if code := env.sendFailingExchange(t, "192.0.2.11:4000").Code; code == http.StatusTooManyRequests {
		t.Error("a different client IP was rate limited by the first one's attempts")
	}
}

// TestCallbackDoesNotChargeTheRateLimitBeforeTheStateIsValidated is the
// regression test for the callback charging its rate limiter before it
// had validated anything. A request with no cookie, no state and no code
// costs the sender nothing and used to cost the deployment one unit of an
// allowance that, without a trusted proxy list, is shared by everyone
// behind the reverse proxy, so 240 such requests in a minute locked
// federated login for the whole deployment. Nothing that fails before
// the call to the provider may spend the allowance.
func TestCallbackDoesNotChargeTheRateLimitBeforeTheStateIsValidated(t *testing.T) {
	_, env := newTestOIDCHandler(t)
	const client = "192.0.2.14:4000"

	bare := func(target string, cookie *http.Cookie) int {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.RemoteAddr = client
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		env.handler.handleCallback(rec, req)
		return rec.Code
	}

	// Every kind of request that fails before the exchange, each sent
	// more times than the whole allowance.
	cookie := env.startLogin(t, "/dashboard")
	for range callbackRateMaxAttempts + 1 {
		if code := bare(OIDCCallbackPath, nil); code != http.StatusBadRequest {
			t.Fatalf("no cookie, no state, no code: status = %d, want 400", code)
		}
		if code := bare(OIDCCallbackPath+"?code=c&state=s", nil); code != http.StatusBadRequest {
			t.Fatalf("no cookie: status = %d, want 400", code)
		}
		if code := bare(OIDCCallbackPath+"?code=c&state=wrong", cookie); code != http.StatusBadRequest {
			t.Fatalf("mismatched state: status = %d, want 400", code)
		}
		if code := bare(OIDCCallbackPath+"?error=access_denied", nil); code != http.StatusFound {
			t.Fatalf("provider error: status = %d, want 302", code)
		}
	}
	state := env.openState(t, cookie)
	for range callbackRateMaxAttempts + 1 {
		if code := bare(OIDCCallbackPath+"?state="+state.State, cookie); code != http.StatusBadRequest {
			t.Fatalf("valid state but no code: status = %d, want 400", code)
		}
	}

	// A login from the same client must still reach the provider.
	if code := env.sendFailingExchange(t, client).Code; code == http.StatusTooManyRequests {
		t.Fatal("requests that never reached the provider spent the rate-limit allowance")
	}
}

// TestCallbackRateLimitIsReturnedByASuccessfulLogin covers the reason
// the allowance is given back: without a trusted proxy list the key is
// the reverse proxy's address, so every working login would otherwise
// spend from one budget shared by the whole deployment.
func TestCallbackRateLimitIsReturnedByASuccessfulLogin(t *testing.T) {
	_, env := newTestOIDCHandler(t)

	// Spend all but one of the allowance, then log in, which must hand
	// the whole allowance back rather than exhaust it.
	for range callbackRateMaxAttempts - 1 {
		env.sendFailingExchange(t, "192.0.2.12:4000")
	}

	cookie := env.startLogin(t, "/dashboard")
	state := env.openState(t, cookie)
	env.idp.SetNextIDToken(env.idp.MintIDToken(t, map[string]any{
		"sub": "subject-1", "email": testUsername, "nonce": state.Nonce,
	}))
	req := httptest.NewRequest(http.MethodGet, OIDCCallbackPath+"?"+url.Values{
		"code": {"authorization-code"}, "state": {state.State},
	}.Encode(), nil)
	req.RemoteAddr = "192.0.2.12:4000"
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	env.handler.handleCallback(rec, req)

	if rec.Code != http.StatusFound || findCookie(rec, SessionCookieName) == nil {
		t.Fatalf("the login did not succeed: status %d", rec.Code)
	}

	// The next request must still be allowed, which it would not be if
	// the successful login had spent the last of the allowance.
	if code := env.sendFailingExchange(t, "192.0.2.12:4000").Code; code == http.StatusTooManyRequests {
		t.Error("a successful login did not return its rate-limit allowance")
	}
}

// TestCallbackClearsTheStateCookieWhenRateLimited pins the ordering the
// comment in handleCallback claims: the cookie is gone on every path out
// of the function, the 429 included.
func TestCallbackClearsTheStateCookieWhenRateLimited(t *testing.T) {
	_, env := newTestOIDCHandler(t)

	var rec *httptest.ResponseRecorder
	for range callbackRateMaxAttempts + 1 {
		rec = env.sendFailingExchange(t, "192.0.2.13:4000")
	}

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	cleared := findCookie(rec, oidc.StateCookieName)
	if cleared == nil || cleared.Value != "" || cleared.MaxAge != -1 {
		t.Error("the state cookie was not cleared on the rate-limited path")
	}
}

// TestCallbackSanitisesTheSubjectClaimInTheLog is the log-injection
// guard. The subject comes from the verified token rather than from the
// claim map, so it never goes through safeClaimValue: nothing but
// logging.SanitizeForLog stands between a hostile provider and a forged
// log line. A "sub" carrying a newline and a plausible success line must
// not produce one.
func TestCallbackSanitisesTheSubjectClaimInTheLog(t *testing.T) {
	const forged = "[OIDC] Federated login succeeded for admin"

	env := newTestOIDCEnv(t, func(cfg *config.OIDCConfig) {
		// Provisioning off, so the login is refused and the refusal logs
		// the subject by way of the resolve error.
		cfg.ProvisionUsers = boolPointer(false)
	})

	logged := captureLogDuring(t, func() {
		rec := env.login(t, "/dashboard", map[string]any{
			"sub": "subject-1\n" + forged,
		})
		if got := rec.Header().Get("Location"); got != loginFailedTarget {
			t.Fatalf("Location = %q, want the refusal", got)
		}
		requireNoSessionCookie(t, rec)
	})

	for _, line := range strings.Split(logged, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[OIDC] Federated login succeeded") {
			t.Fatalf("a forged log line was written:\n%s", logged)
		}
	}
	if !strings.Contains(logged, "Refusing federated login") {
		t.Errorf("the refusal was not logged at all; log was %q", logged)
	}
}

func TestCallbackLogsTheClaimExtractionDiagnostics(t *testing.T) {
	env := newTestOIDCEnv(t, func(cfg *config.OIDCConfig) {
		cfg.DisplayNameClaim = "name"
	})

	logged := captureLogDuring(t, func() {
		rec := env.login(t, "/", map[string]any{
			// A display name of control characters is dropped by name,
			// a non-string group element is skipped, and the count of
			// both must reach the log exactly once.
			"name":   "bad\nname",
			"groups": []any{"idp-eng", 42},
		})
		if rec.Code != http.StatusFound {
			t.Fatalf("login status = %d", rec.Code)
		}
	})

	for _, want := range []string{"Skipped 1 unusable value", "Dropped unusable claim(s) name"} {
		if !strings.Contains(logged, want) {
			t.Errorf("log does not mention %q; log was %q", want, logged)
		}
	}
	if strings.Contains(logged, "bad") {
		t.Error("the dropped claim's value reached the log; only names may")
	}
}

func TestLogIdentityDiagnosticsReportsAnUnexpectedGroupsShape(t *testing.T) {
	logged := captureLogDuring(t, func() {
		logIdentityDiagnostics(&oidc.Identity{
			Subject:                    "subject-1",
			UnexpectedGroupsClaimShape: "map[string]interface {}",
		})
	})

	if !strings.Contains(logged, "unexpected shape") {
		t.Errorf("log does not report the unexpected shape; log was %q", logged)
	}
}

// =============================================================================
// Pure helpers
// =============================================================================

func TestEmailDomainAllowed(t *testing.T) {
	cases := map[string]struct {
		domains  []string
		identity oidc.Identity
		want     bool
	}{
		"no restriction configured": {
			identity: oidc.Identity{Email: ""},
			want:     true,
		},
		"verified address in an allowed domain": {
			domains:  []string{"example.com"},
			identity: oidc.Identity{Email: "jane@example.com", EmailVerified: true},
			want:     true,
		},
		"allowed domain written with a leading at sign": {
			domains:  []string{" @Example.COM "},
			identity: oidc.Identity{Email: "jane@example.com", EmailVerified: true},
			want:     true,
		},
		"address in another domain": {
			domains:  []string{"example.com"},
			identity: oidc.Identity{Email: "jane@evil.example.net", EmailVerified: true},
		},
		"unverified address": {
			domains:  []string{"example.com"},
			identity: oidc.Identity{Email: "jane@example.com"},
		},
		"absent address is not an absence of restriction": {
			domains:  []string{"example.com"},
			identity: oidc.Identity{EmailVerified: true},
		},
		"rejected address": {
			domains:  []string{"example.com"},
			identity: oidc.Identity{Email: "jane@example.com", EmailRejected: true, EmailVerified: true},
		},
		"address with no domain part": {
			domains:  []string{"example.com"},
			identity: oidc.Identity{Email: "jane@", EmailVerified: true},
		},
		"address with no at sign": {
			domains:  []string{"example.com"},
			identity: oidc.Identity{Email: "jane", EmailVerified: true},
		},
		"empty entry in the allow list matches nothing": {
			domains:  []string{"", "@"},
			identity: oidc.Identity{Email: "jane@example.com", EmailVerified: true},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			handler := &OIDCHandler{cfg: config.OIDCConfig{AllowedEmailDomains: tc.domains}}
			if got := handler.emailDomainAllowed(&tc.identity); got != tc.want {
				t.Errorf("emailDomainAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFederatedIdentityCarriesOnlyTheDecisionInputs(t *testing.T) {
	converted := federatedIdentity(&oidc.Identity{
		Issuer:                     "https://idp.example.com",
		Subject:                    "subject-1",
		Username:                   testUsername,
		DisplayName:                "Jane Doe",
		Email:                      "jane@example.com",
		EmailRejected:              true,
		EmailVerified:              true,
		Groups:                     []string{"idp-eng"},
		SkippedGroups:              2,
		UnexpectedGroupsClaimShape: "float64",
		DroppedClaims:              []string{"name"},
	})

	want := auth.FederatedIdentity{
		Issuer:      "https://idp.example.com",
		Subject:     "subject-1",
		Username:    testUsername,
		DisplayName: "Jane Doe",
		Email:       "jane@example.com",
		Groups:      []string{"idp-eng"},
	}
	if converted.Issuer != want.Issuer || converted.Subject != want.Subject ||
		converted.Username != want.Username || converted.DisplayName != want.DisplayName ||
		converted.Email != want.Email || !slices.Equal(converted.Groups, want.Groups) {
		t.Errorf("federatedIdentity = %+v, want %+v", converted, want)
	}
}

func TestFederationOptionsMirrorTheConfiguration(t *testing.T) {
	handler := &OIDCHandler{cfg: config.OIDCConfig{
		ProvisionUsers: boolPointer(true),
		GroupMap:       map[string]string{"idp-eng": "engineers"},
		SuperuserGroup: "idp-admins",
	}}

	opts := handler.federationOptions()
	if !opts.ProvisionUsers || opts.SuperuserGroup != "idp-admins" ||
		opts.GroupMap["idp-eng"] != "engineers" {
		t.Errorf("federationOptions = %+v", opts)
	}
}

func TestRegisterRoutesServesBothEndpoints(t *testing.T) {
	handler, _ := newTestOIDCHandler(t)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	for _, path := range []string{OIDCStartPath, OIDCCallbackPath} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s is not registered", path)
		}
	}
}

func TestRegisterDisabledStartRouteRedirectsAndLeavesTheCallbackAlone(t *testing.T) {
	mux := http.NewServeMux()
	RegisterDisabledStartRoute(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, OIDCStartPath, nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("start status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != providerFailedTarget {
		t.Fatalf("start Location = %q, want %q", got, providerFailedTarget)
	}

	// Every method answers the same way, since the point is that a
	// browser following the button lands somewhere usable.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, OIDCStartPath, nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("start POST status = %d, want %d", rec.Code, http.StatusFound)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, OIDCCallbackPath, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("callback status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCloseIsSafeWithoutARateLimiter(t *testing.T) {
	// Close must not panic on a hand-built handler, since that is how
	// the pure-helper tests above construct one.
	(&OIDCHandler{}).Close()
}

func TestExtractIPFallsBackToRemoteAddr(t *testing.T) {
	handler := &OIDCHandler{}
	req := httptest.NewRequest(http.MethodGet, OIDCCallbackPath, nil)
	req.RemoteAddr = "192.0.2.20:5000"
	req.Header.Set("X-Forwarded-For", "203.0.113.5")

	if got := handler.extractIPFromRequest(req); got != "192.0.2.20:5000" {
		t.Errorf("extractIPFromRequest = %q, want the untrusted-header-free RemoteAddr", got)
	}
}

func TestExtractIPUsesTheConfiguredExtractor(t *testing.T) {
	env := newTestOIDCEnv(t)
	// A trusted proxy list is what makes the forwarded headers mean
	// anything, both for the client IP and for the Secure attribute.
	handler := NewOIDCHandler(env.store, env.handler.provider, env.handler.cfg,
		testStateKey, false, auth.NewIPExtractor([]string{"192.0.2.0/24"}))
	defer handler.Close()

	req := httptest.NewRequest(http.MethodGet, OIDCCallbackPath, nil)
	req.RemoteAddr = "192.0.2.30:5000"
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	if got := handler.extractIPFromRequest(req); got != "203.0.113.5" {
		t.Errorf("extractIPFromRequest = %q, want the forwarded address from a trusted proxy", got)
	}

	req.Header.Set("X-Forwarded-Proto", "https")
	attrs := handler.stateCookieAttributes(req)
	if !attrs.secure || !attrs.prefixed {
		t.Errorf("stateCookieAttributes = %+v from a trusted proxy, want both true", attrs)
	}

	// And the cookie name must follow, since the "__Host-" prefix is
	// only usable on a Secure cookie.
	rec := httptest.NewRecorder()
	handler.handleStart(rec, req)
	if findCookie(rec, oidc.SecureStateCookieName) == nil {
		t.Errorf("no %s cookie behind a TLS-terminating proxy", oidc.SecureStateCookieName)
	}
}

// TestStateCookieIsSecureButUnprefixedFromAnUnlistedProxy pins the split
// between the two rules on the shipped default configuration, where a
// TLS-terminating proxy sits in front of the server but
// http.trusted_proxies is empty. The Secure attribute follows the
// forwarded header, because losing it would send the cookie in clear on
// any plain-HTTP request to the host, but the "__Host-" prefix does not,
// because the header has not been shown to come from a proxy worth
// believing. The callback must then read the cookie back under the same
// name, or every login on such a deployment would fail.
func TestStateCookieIsSecureButUnprefixedFromAnUnlistedProxy(t *testing.T) {
	env := newTestOIDCEnv(t)
	handler := NewOIDCHandler(env.store, env.handler.provider, env.handler.cfg,
		testStateKey, false, auth.NewIPExtractor(nil))
	defer handler.Close()

	req := httptest.NewRequest(http.MethodGet, OIDCStartPath, nil)
	req.RemoteAddr = "192.0.2.30:5000"
	req.Header.Set("X-Forwarded-Proto", "https")

	attrs := handler.stateCookieAttributes(req)
	if !attrs.secure || attrs.prefixed {
		t.Fatalf("stateCookieAttributes = %+v, want secure and not prefixed", attrs)
	}

	rec := httptest.NewRecorder()
	handler.handleStart(rec, req)
	if findCookie(rec, oidc.SecureStateCookieName) != nil {
		t.Errorf("a %s cookie was set on a request from an unlisted proxy", oidc.SecureStateCookieName)
	}
	cookie := findCookie(rec, oidc.StateCookieName)
	if cookie == nil {
		t.Fatalf("no %s cookie was set", oidc.StateCookieName)
	}
	if !cookie.Secure {
		t.Error("the state cookie lost its Secure attribute behind a TLS-terminating proxy")
	}

	// The callback reads the same name, and clears it with the same
	// flags.
	callback := httptest.NewRequest(http.MethodGet, OIDCCallbackPath+"?error=access_denied", nil)
	callback.RemoteAddr = req.RemoteAddr
	callback.Header.Set("X-Forwarded-Proto", "https")
	callback.AddCookie(cookie)
	callbackRec := httptest.NewRecorder()
	handler.handleCallback(callbackRec, callback)
	cleared := findCookie(callbackRec, oidc.StateCookieName)
	if cleared == nil || cleared.MaxAge != -1 || !cleared.Secure {
		t.Errorf("the state cookie was not cleared with matching flags: %+v", cleared)
	}
}

func TestAllowRequestPassesWhenNoIPCanBeDetermined(t *testing.T) {
	handler := NewOIDCHandler(nil, nil, config.OIDCConfig{}, testStateKey, false, nil)
	defer handler.Close()

	if !handler.allowRequest(httptest.NewRecorder(), "") {
		t.Error("a request with no determinable IP must not be rate limited into a 429")
	}

	// And a handler with no limiter at all, which is how the pure-helper
	// tests construct one, must not refuse either.
	if !(&OIDCHandler{}).allowRequest(httptest.NewRecorder(), "192.0.2.1:5000") {
		t.Error("a handler with no rate limiter must let the request through")
	}
}

// stubFederationStore stands in for auth.AuthStore so that a test can
// fail any one of the three calls a login makes. The real store cannot
// be made to fail the session call from a test, because doing so needs
// the account to change between the resolve and the session within one
// request.
type stubFederationStore struct {
	user           *auth.StoredUser
	resolveErr     error
	reconcileErr   error
	sessionErr     error
	reconcileCalls int
	sessionCalls   int
}

func (s *stubFederationStore) ResolveFederatedUser(auth.FederatedIdentity,
	auth.FederationOptions) (*auth.StoredUser, error) {
	if s.resolveErr != nil {
		return nil, s.resolveErr
	}
	return s.user, nil
}

func (s *stubFederationStore) ReconcileFederatedGroups(int64, auth.FederatedIdentity,
	auth.FederationOptions) error {
	s.reconcileCalls++
	return s.reconcileErr
}

func (s *stubFederationStore) CreateSessionForUser(string) (string, time.Time, error) {
	s.sessionCalls++
	if s.sessionErr != nil {
		return "", time.Time{}, s.sessionErr
	}
	return "a-session-token", time.Now().Add(time.Hour), nil
}

// newStubbedOIDCEnv builds an env whose handler talks to store instead
// of the real auth store; the fake identity provider is still real, so
// the whole protocol round trip runs.
func newStubbedOIDCEnv(t *testing.T, store federationStore) *oidcTestEnv {
	t.Helper()

	env := newTestOIDCEnv(t)
	handler := newOIDCHandler(store, env.handler.provider, env.handler.cfg,
		testStateKey, false, nil)
	t.Cleanup(handler.Close)
	env.handler = handler
	return env
}

// TestCallbackRefusesWhenTheSessionCannotBeCreated covers the last
// failure on the way to a session. It must look exactly like every other
// refusal, and must not leave a cookie behind.
func TestCallbackRefusesWhenTheSessionCannotBeCreated(t *testing.T) {
	store := &stubFederationStore{
		user:       &auth.StoredUser{ID: 7, Username: testUsername},
		sessionErr: errors.New("service account cannot hold a session: " + testUsername),
	}
	env := newStubbedOIDCEnv(t, store)

	var rec *httptest.ResponseRecorder
	logged := captureLogDuring(t, func() {
		rec = env.login(t, "/dashboard", nil)
	})

	if got := rec.Header().Get("Location"); got != loginFailedTarget {
		t.Fatalf("Location = %q, want %q", got, loginFailedTarget)
	}
	if body := rec.Body.String(); body != "" {
		t.Errorf("body = %q, want it empty", body)
	}
	requireNoSessionCookie(t, rec)
	if strings.Contains(rec.Header().Get("Location"), "service account") {
		t.Error("the store's error text leaked into the redirect")
	}
	if !strings.Contains(logged, "Refusing federated login") {
		t.Errorf("the session failure was not logged; log was %q", logged)
	}
}

// TestCallbackDoesNotMintASessionAfterAFailedReconciliation pins the
// ordering directly rather than through a database side effect: the
// session call must not happen at all.
func TestCallbackDoesNotMintASessionAfterAFailedReconciliation(t *testing.T) {
	store := &stubFederationStore{
		user:         &auth.StoredUser{ID: 7, Username: testUsername},
		reconcileErr: errors.New("removing jane from mapped group \"engineers\": disk full"),
	}
	env := newStubbedOIDCEnv(t, store)

	rec := env.login(t, "/dashboard", nil)

	if got := rec.Header().Get("Location"); got != loginFailedTarget {
		t.Fatalf("Location = %q, want %q", got, loginFailedTarget)
	}
	requireNoSessionCookie(t, rec)
	if store.reconcileCalls != 1 {
		t.Errorf("reconciliation ran %d times, want once", store.reconcileCalls)
	}
	if store.sessionCalls != 0 {
		t.Error("a session was minted despite the failed reconciliation")
	}
}
