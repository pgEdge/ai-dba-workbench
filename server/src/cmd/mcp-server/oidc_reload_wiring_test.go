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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/oidc"
	"github.com/pgedge/ai-workbench/server/internal/oidc/oidctest"
)

// These tests cover issue #484 at the wiring level: SetupHandlers hands
// the federated login handler and the capabilities endpoint the live
// configuration, so a SIGHUP reload reaches both without a restart.

// liveOIDCServer wires SetupHandlers against a fake identity provider
// with a swappable live configuration, and returns the mux and the
// pointer a test stores a "reloaded" configuration into.
func liveOIDCServer(t *testing.T) (*http.ServeMux, *atomic.Pointer[config.Config]) {
	t.Helper()

	idp := oidctest.NewFakeIDP(t)
	startup := newOIDCEnabledConfig(idp)
	provider, err := oidc.NewProvider(t.Context(), startup.HTTP.Auth.OIDC)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}

	store, err := auth.NewAuthStore(t.TempDir(), 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("auth.NewAuthStore: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("closing the auth store: %v", closeErr)
		}
	})

	var live atomic.Pointer[config.Config]
	live.Store(startup)

	var closers []func()
	deps := &HandlerDependencies{
		Config:         startup,
		LiveConfig:     live.Load,
		AuthStore:      store,
		OIDCProvider:   provider,
		OIDCStateKey:   []byte("0123456789abcdef0123456789abcdef"),
		RegisterCloser: func(closer func()) { closers = append(closers, closer) },
	}

	mux := http.NewServeMux()
	if err := SetupHandlers(deps)(mux); err != nil {
		t.Fatalf("SetupHandlers: %v", err)
	}
	t.Cleanup(func() {
		for _, closer := range closers {
			closer()
		}
	})
	return mux, &live
}

// reloadedFrom returns a copy of the current live configuration with
// mutate applied, standing in for the file a SIGHUP re-reads.
func reloadedFrom(live *atomic.Pointer[config.Config], mutate func(*config.Config)) *config.Config {
	reloaded := *live.Load()
	mutate(&reloaded)
	return &reloaded
}

func fetchAuthCapabilities(t *testing.T, mux *http.ServeMux) authCapabilitiesInfo {
	t.Helper()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("capabilities status = %d", rec.Code)
	}
	var body struct {
		Auth authCapabilitiesInfo `json:"auth"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding capabilities: %v", err)
	}
	return body.Auth
}

func TestCapabilitiesFollowAReloadedOIDCConfiguration(t *testing.T) {
	mux, live := liveOIDCServer(t)

	if got := fetchAuthCapabilities(t, mux); !got.OIDCEnabled || got.OIDCLabel != "Sign in with Acme" {
		t.Fatalf("before the reload, auth = %+v", got)
	}

	live.Store(reloadedFrom(live, func(cfg *config.Config) {
		cfg.HTTP.Auth.OIDC.ButtonLabel = "Sign in with Example"
	}))
	if got := fetchAuthCapabilities(t, mux); got.OIDCLabel != "Sign in with Example" {
		t.Errorf("after relabelling, oidc_label = %q, want %q", got.OIDCLabel, "Sign in with Example")
	}

	live.Store(reloadedFrom(live, func(cfg *config.Config) {
		cfg.HTTP.Auth.OIDC.Enabled = config.BoolPtr(false)
	}))
	if got := fetchAuthCapabilities(t, mux); got.OIDCEnabled || got.OIDCLabel != "" {
		t.Errorf("after switching OIDC off, auth = %+v, want it reported off", got)
	}
}

func TestCapabilitiesKeepLocalLoginAsTheLoginHandlerEnforcesIt(t *testing.T) {
	mux, live := liveOIDCServer(t)

	// The login handler is built once with local login on, so a reload
	// switching it off must not make the login page hide a form whose
	// endpoint still accepts passwords.
	live.Store(reloadedFrom(live, func(cfg *config.Config) {
		cfg.HTTP.Auth.Local.Enabled = config.BoolPtr(false)
	}))
	if got := fetchAuthCapabilities(t, mux); !got.LocalEnabled {
		t.Errorf("local_enabled = false after a reload, but the login handler still enforces true")
	}
}

func TestOIDCStartFollowsAReloadedEnabledSwitch(t *testing.T) {
	mux, live := liveOIDCServer(t)

	start := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/start", nil))
		return rec
	}

	if rec := start(); rec.Code != http.StatusFound || rec.Header().Get("Location") == "/?login_error=provider" {
		t.Fatalf("before the reload, start = %d to %q, want a redirect to the provider",
			rec.Code, rec.Header().Get("Location"))
	}

	live.Store(reloadedFrom(live, func(cfg *config.Config) {
		cfg.HTTP.Auth.OIDC.Enabled = config.BoolPtr(false)
	}))
	if got := start().Header().Get("Location"); got != "/?login_error=provider" {
		t.Errorf("after switching OIDC off, start Location = %q, want the login screen", got)
	}
}

func TestLiveConfigFallsBackToTheStartupConfiguration(t *testing.T) {
	startup := &config.Config{}
	cases := map[string]*HandlerDependencies{
		"no live source":       {Config: startup},
		"live source is empty": {Config: startup, LiveConfig: func() *config.Config { return nil }},
	}
	for name, deps := range cases {
		t.Run(name, func(t *testing.T) {
			if got := deps.liveConfig(); got != startup {
				t.Errorf("liveConfig = %p, want the start-up configuration %p", got, startup)
			}
		})
	}
}

// TestLiveOIDCGroupMapFollowsTheLiveConfiguration pins the source the RBAC
// handler's federated group protections read (issue #595): the group map
// from the latest reload, and nothing when there is no configuration.
func TestLiveOIDCGroupMapFollowsTheLiveConfiguration(t *testing.T) {
	startup := &config.Config{}
	startup.HTTP.Auth.OIDC.GroupMap = map[string]string{"idp-admins": "wb-admins"}
	reloaded := &config.Config{}
	reloaded.HTTP.Auth.OIDC.GroupMap = map[string]string{"idp-ops": "wb-ops"}

	deps := &HandlerDependencies{Config: startup,
		LiveConfig: func() *config.Config { return reloaded }}
	if got := deps.liveOIDCGroupMap(); got["idp-ops"] != "wb-ops" || len(got) != 1 {
		t.Errorf("liveOIDCGroupMap = %v, want the reloaded map", got)
	}
	if got := (&HandlerDependencies{}).liveOIDCGroupMap(); got != nil {
		t.Errorf("liveOIDCGroupMap with no configuration = %v, want nil", got)
	}
}

// TestRBACRoutesFollowAReloadedGroupMap pins the wiring in SetupHandlers
// (issue #595): the RBAC handler's federated group protections must read
// the group map from the live configuration, so a bounded token cannot
// create a group that only a reload has mapped. Wiring the start-up
// snapshot instead lets the create through.
func TestRBACRoutesFollowAReloadedGroupMap(t *testing.T) {
	store, err := auth.NewAuthStore(t.TempDir(), 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("auth.NewAuthStore: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("closing the auth store: %v", closeErr)
		}
	})
	if err := store.CreateUser("root", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.SetUserSuperuser("root", true); err != nil {
		t.Fatalf("SetUserSuperuser: %v", err)
	}
	system := store.AsActor(auth.SystemActor())
	rawToken, token, err := system.CreateToken("root", "bounded token", nil, true)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if err := system.SetTokenConnectionScope(token.ID, []auth.ScopedConnection{
		{ConnectionID: 5, AccessLevel: auth.AccessLevelReadWrite},
	}, true); err != nil {
		t.Fatalf("SetTokenConnectionScope: %v", err)
	}

	startup := &config.Config{}
	startup.HTTP.Auth.OIDC.GroupMap = map[string]string{"idp-admins": "wb-admins"}
	reloaded := &config.Config{}
	reloaded.HTTP.Auth.OIDC.GroupMap = map[string]string{"idp-new": "wb-new"}
	// The reload lands after registration, so a snapshot taken while
	// SetupHandlers runs would hold only the start-up map and let the
	// create through.
	var live atomic.Pointer[config.Config]
	live.Store(startup)
	deps := &HandlerDependencies{
		Config:     startup,
		LiveConfig: live.Load,
		AuthStore:  store,
	}
	mux := http.NewServeMux()
	if err := SetupHandlers(deps)(mux); err != nil {
		t.Fatalf("SetupHandlers: %v", err)
	}
	live.Store(reloaded)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/groups",
		strings.NewReader(`{"name":"wb-new"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "OIDC group map") {
		t.Fatalf("creating a group only the reloaded map names = %d %s, want the federated refusal",
			rec.Code, rec.Body.String())
	}
	if group, err := store.GetGroupByName("wb-new"); err != nil || group != nil {
		t.Errorf("wb-new = %v (%v), want it not to be created", group, err)
	}
}

// buttonLabelYAML is a federated login configuration whose button label
// is label, standing in for the file a SIGHUP re-reads.
func buttonLabelYAML(label string) string {
	return `
http:
  tls:
    enabled: true
    cert_file: /nonexistent/cert.pem
    key_file: /nonexistent/key.pem
  auth:
    oidc:
      enabled: true
      issuer: https://idp.example.com
      client_id: workbench
      client_secret: s3cret
      redirect_url: https://workbench.example.com/api/v1/auth/oidc/callback
      button_label: ` + label + `
`
}

// TestHandlerDependenciesFollowAReload pins the production wiring in
// Run: the dependencies it hands SetupHandlers must read the live
// configuration through the reloadable configuration, so a reload from a
// rewritten file reaches the handlers. Without it every SIGHUP would log
// the policy changes as applied and apply none of them.
func TestHandlerDependenciesFollowAReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-dba-server.yaml")
	if err := os.WriteFile(path, []byte(buttonLabelYAML("Before")), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	flags := config.CLIFlags{ConfigFileSet: true, ConfigFile: path}
	startup, err := config.LoadConfig(path, flags)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	reloadable := config.NewReloadableConfig(startup, path, flags)

	s := &Server{cfg: startup}
	deps := s.handlerDependencies(nil, reloadable)
	if got := deps.liveConfig().HTTP.Auth.OIDC.ButtonLabel; got != "Before" {
		t.Fatalf("live button label before the reload = %q, want %q", got, "Before")
	}

	if err := os.WriteFile(path, []byte(buttonLabelYAML("After")), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	if err := reloadable.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got := deps.liveConfig().HTTP.Auth.OIDC.ButtonLabel; got != "After" {
		t.Errorf("live button label after the reload = %q, want %q", got, "After")
	}
	if deps.Config != startup {
		t.Error("deps.Config is not the start-up configuration")
	}
}
