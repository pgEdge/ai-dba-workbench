/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/oidc"
	"github.com/pgedge/ai-workbench/server/internal/oidc/oidctest"
)

// newOIDCEnabledConfig builds a configuration with federated login
// switched on against the supplied fake identity provider.
func newOIDCEnabledConfig(idp *oidctest.FakeIDP) *config.Config {
	cfg := &config.Config{}
	cfg.HTTP.Auth.OIDC = config.OIDCConfig{
		Enabled:      true,
		Issuer:       idp.Issuer(),
		ClientID:     idp.ClientID(),
		ClientSecret: "test-client-secret",
		ButtonLabel:  "Sign in with Acme",
	}
	return cfg
}

func TestInitOIDCDoesNothingWhenDisabled(t *testing.T) {
	server := &Server{cfg: &config.Config{}, ctx: context.Background()}

	if err := server.initOIDC("a-server-secret"); err != nil {
		t.Fatalf("initOIDC: %v", err)
	}
	if server.oidcProvider != nil || server.oidcStateKey != nil {
		t.Error("a disabled OIDC configuration must leave the provider and key unset")
	}
}

func TestInitOIDCDiscoversTheProviderAndDerivesAStateKey(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	server := &Server{cfg: newOIDCEnabledConfig(idp), ctx: context.Background()}

	if err := server.initOIDC("a-server-secret"); err != nil {
		t.Fatalf("initOIDC: %v", err)
	}
	if server.oidcProvider == nil {
		t.Fatal("no provider was discovered")
	}
	if len(server.oidcStateKey) != 32 {
		t.Fatalf("state key is %d bytes, want the 32 AES-256 requires", len(server.oidcStateKey))
	}

	// The state key must be the login-state key and not the one that
	// encrypts stored database passwords, or one compromise would be
	// two. Different salts are the whole of that separation, so check
	// the key actually differs from a key derived under another salt.
	other := &Server{cfg: newOIDCEnabledConfig(idp), ctx: context.Background()}
	if err := other.initOIDC("a-different-server-secret"); err != nil {
		t.Fatalf("initOIDC: %v", err)
	}
	if string(other.oidcStateKey) == string(server.oidcStateKey) {
		t.Error("two different server secrets produced the same state key")
	}
}

func TestInitOIDCRefusesAnEmptyServerSecret(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	server := &Server{cfg: newOIDCEnabledConfig(idp), ctx: context.Background()}

	err := server.initOIDC("")
	if err == nil {
		t.Fatal("an empty server secret must not yield a usable state key")
	}
	if !strings.Contains(err.Error(), "server secret is empty") {
		t.Errorf("error = %q, want it to name the empty secret", err)
	}
}

func TestInitOIDCFailsWhenDiscoveryFails(t *testing.T) {
	// A server that answers 404 to the discovery request stands in for
	// an unreachable or misconfigured identity provider; the failure
	// must reach the caller, which treats it as fatal.
	unreachable := httptest.NewServer(http.HandlerFunc(http.NotFound))
	defer unreachable.Close()

	cfg := &config.Config{}
	cfg.HTTP.Auth.OIDC = config.OIDCConfig{
		Enabled: true, Issuer: unreachable.URL, ClientID: "workbench",
	}
	server := &Server{cfg: cfg, ctx: context.Background()}

	if err := server.initOIDC("a-server-secret"); err == nil {
		t.Fatal("a discovery failure must be reported, not swallowed")
	}
	if server.oidcProvider != nil {
		t.Error("a failed discovery must leave the provider unset")
	}
}

func TestAuthCapabilitiesReportsTheLoginPageState(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider, err := oidc.NewProvider(t.Context(), newOIDCEnabledConfig(idp).HTTP.Auth.OIDC)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}
	localOff := false

	cases := map[string]struct {
		deps *HandlerDependencies
		want authCapabilitiesInfo
	}{
		"no configuration at all": {
			deps: &HandlerDependencies{},
			want: authCapabilitiesInfo{LocalEnabled: true},
		},
		"nil dependencies": {
			want: authCapabilitiesInfo{LocalEnabled: true},
		},
		"OIDC on with a label": {
			deps: &HandlerDependencies{
				Config: newOIDCEnabledConfig(idp), OIDCProvider: provider,
			},
			want: authCapabilitiesInfo{
				LocalEnabled: true, OIDCEnabled: true, OIDCLabel: "Sign in with Acme",
			},
		},
		"OIDC on without a label": {
			deps: func() *HandlerDependencies {
				cfg := newOIDCEnabledConfig(idp)
				cfg.HTTP.Auth.OIDC.ButtonLabel = ""
				return &HandlerDependencies{Config: cfg, OIDCProvider: provider}
			}(),
			want: authCapabilitiesInfo{
				LocalEnabled: true, OIDCEnabled: true, OIDCLabel: defaultOIDCButtonLabel,
			},
		},
		"OIDC configured but never discovered": {
			deps: &HandlerDependencies{Config: newOIDCEnabledConfig(idp)},
			want: authCapabilitiesInfo{LocalEnabled: true},
		},
		"local login switched off": {
			deps: func() *HandlerDependencies {
				cfg := newOIDCEnabledConfig(idp)
				cfg.HTTP.Auth.Local.Enabled = &localOff
				return &HandlerDependencies{Config: cfg, OIDCProvider: provider}
			}(),
			want: authCapabilitiesInfo{
				OIDCEnabled: true, OIDCLabel: "Sign in with Acme",
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			if got := authCapabilities(testCase.deps); got != testCase.want {
				t.Errorf("authCapabilities = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

func TestHandleCapabilitiesReportsTheAuthBlock(t *testing.T) {
	handler := handleCapabilities(true, 50, authCapabilitiesInfo{
		LocalEnabled: true, OIDCEnabled: true, OIDCLabel: "Sign in with Acme",
	})

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		AIEnabled bool `json:"ai_enabled"`
		Auth      struct {
			LocalEnabled bool   `json:"local_enabled"`
			OIDCEnabled  bool   `json:"oidc_enabled"`
			OIDCLabel    string `json:"oidc_label"`
		} `json:"auth"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the response: %v", err)
	}
	if !body.AIEnabled || !body.Auth.LocalEnabled || !body.Auth.OIDCEnabled ||
		body.Auth.OIDCLabel != "Sign in with Acme" {
		t.Errorf("capabilities body = %+v", body)
	}

	// The client secret must never appear here: the endpoint is public.
	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
	if strings.Contains(rec.Body.String(), "test-client-secret") {
		t.Error("the capabilities response leaked the OIDC client secret")
	}
}

func TestSetupHandlersRegistersTheOIDCEndpoints(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	provider, err := oidc.NewProvider(t.Context(), newOIDCEnabledConfig(idp).HTTP.Auth.OIDC)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}

	store, err := auth.NewAuthStore(t.TempDir(), 0, 0)
	if err != nil {
		t.Fatalf("auth.NewAuthStore: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("closing the auth store: %v", closeErr)
		}
	})

	var registered []func()
	deps := &HandlerDependencies{
		Config:         newOIDCEnabledConfig(idp),
		AuthStore:      store,
		OIDCProvider:   provider,
		OIDCStateKey:   []byte("0123456789abcdef0123456789abcdef"),
		RegisterCloser: func(closer func()) { registered = append(registered, closer) },
	}

	mux := http.NewServeMux()
	if err := SetupHandlers(deps)(mux); err != nil {
		t.Fatalf("SetupHandlers: %v", err)
	}
	// The handler owns a rate limiter whose goroutine only Close stops,
	// so the closer must have been handed back along with the auth
	// handler's.
	if len(registered) < 2 {
		t.Fatalf("SetupHandlers registered %d closers, want the auth and OIDC handlers' both",
			len(registered))
	}
	t.Cleanup(func() {
		for _, closer := range registered {
			closer()
		}
	})

	for _, path := range []string{"/api/v1/auth/oidc/start", "/api/v1/auth/oidc/callback"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s was not registered", path)
		}
	}
}

func TestSetupHandlersRedirectsTheOIDCStartEndpointWhenDisabled(t *testing.T) {
	deps := &HandlerDependencies{Config: &config.Config{}}

	mux := http.NewServeMux()
	if err := SetupHandlers(deps)(mux); err != nil {
		t.Fatalf("SetupHandlers: %v", err)
	}

	// The start endpoint exists in every configuration, because the
	// login screen offers the button whenever it cannot reach the
	// capabilities endpoint, and following it is a full-page
	// navigation: a 404 here would cost the user the login screen.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/start", nil))
	if rec.Code != http.StatusFound {
		t.Errorf("start status = %d, want %d when no provider was discovered",
			rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != "/?login_error=provider" {
		t.Errorf("start Location = %q, want %q", got, "/?login_error=provider")
	}

	// The callback stays unregistered: nothing sends a user there by
	// hand, so there is nothing to hand back to the login screen.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("callback status = %d, want %d when no provider was discovered",
			rec.Code, http.StatusNotFound)
	}
}

func TestHandleCapabilitiesRejectsNonGET(t *testing.T) {
	handler := handleCapabilities(false, 50, authCapabilitiesInfo{LocalEnabled: true})

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, "/api/v1/capabilities", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// TestInitOIDCWarnsWhenNoTrustedProxiesAreConfigured covers the start-up
// warning. Without a trusted proxy list every request behind a reverse
// proxy arrives with that proxy's address, so the callback's rate limit
// has one key for the whole deployment: it stops nothing an attacker
// does and can be spent deliberately to deny everybody a login.
func TestInitOIDCWarnsWhenNoTrustedProxiesAreConfigured(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)

	t.Run("no trusted proxies", func(t *testing.T) {
		server := &Server{cfg: newOIDCEnabledConfig(idp), ctx: context.Background()}

		out := captureStderr(t, func() {
			if err := server.initOIDC("a-server-secret"); err != nil {
				t.Fatalf("initOIDC: %v", err)
			}
		})

		if !strings.Contains(out, "http.trusted_proxies is empty") {
			t.Errorf("start-up said nothing about the inoperative rate limiting:\n%s", out)
		}
	})

	t.Run("trusted proxies configured", func(t *testing.T) {
		cfg := newOIDCEnabledConfig(idp)
		cfg.HTTP.TrustedProxies = []string{"10.0.0.0/8"}
		server := &Server{cfg: cfg, ctx: context.Background()}

		out := captureStderr(t, func() {
			if err := server.initOIDC("a-server-secret"); err != nil {
				t.Fatalf("initOIDC: %v", err)
			}
		})

		if strings.Contains(out, "http.trusted_proxies is empty") {
			t.Errorf("warned despite a configured proxy list:\n%s", out)
		}
	})
}
