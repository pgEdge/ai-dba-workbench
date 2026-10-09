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
	"slices"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// These tests cover issue #484: the login policy settings under
// http.auth.oidc are read through the handler's configuration source on
// every request, so a SIGHUP reload changes them without a restart.

// reloadTo points the handler at a configuration derived from the one
// it was constructed with, standing in for a SIGHUP reload.
func (e *oidcTestEnv) reloadTo(mutate func(*config.OIDCConfig)) {
	reloaded := e.handler.cfg
	mutate(&reloaded)
	e.handler.SetConfigSource(func() config.OIDCConfig { return reloaded })
}

func TestSetConfigSourceReplacesAndRestoresTheConstructionCopy(t *testing.T) {
	handler := &OIDCHandler{cfg: config.OIDCConfig{SuperuserGroup: "constructed"}}

	handler.SetConfigSource(func() config.OIDCConfig {
		return config.OIDCConfig{SuperuserGroup: "reloaded"}
	})
	if got := handler.currentConfig().SuperuserGroup; got != "reloaded" {
		t.Errorf("with a source, SuperuserGroup = %q, want %q", got, "reloaded")
	}

	handler.SetConfigSource(nil)
	if got := handler.currentConfig().SuperuserGroup; got != "constructed" {
		t.Errorf("after clearing the source, SuperuserGroup = %q, want %q", got, "constructed")
	}
}

func TestCallbackAppliesAReloadedEmailDomainPolicy(t *testing.T) {
	env := newTestOIDCEnv(t)
	claims := map[string]any{"email": "jane@other.example.net", "email_verified": true}

	if rec := env.login(t, "/", claims); rec.Header().Get("Location") != "/" {
		t.Fatalf("before the reload, Location = %q, want %q",
			rec.Header().Get("Location"), "/")
	}

	env.reloadTo(func(cfg *config.OIDCConfig) {
		cfg.AllowedEmailDomains = []string{"example.com"}
	})

	rec := env.login(t, "/", claims)
	if got := rec.Header().Get("Location"); got != loginFailedTarget {
		t.Fatalf("after narrowing allowed_email_domains, Location = %q, want %q",
			got, loginFailedTarget)
	}
	requireNoSessionCookie(t, rec)
}

func TestCallbackAppliesAReloadedProvisioningSwitch(t *testing.T) {
	env := newTestOIDCEnv(t)
	env.reloadTo(func(cfg *config.OIDCConfig) {
		cfg.ProvisionUsers = boolPointer(false)
	})

	rec := env.login(t, "/", nil)
	if got := rec.Header().Get("Location"); got != loginFailedTarget {
		t.Fatalf("with provision_users switched off, Location = %q, want %q",
			got, loginFailedTarget)
	}
	user, err := env.store.GetUser(testUsername)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user != nil {
		t.Fatal("an account was provisioned after provision_users was switched off")
	}
}

func TestCallbackAppliesAReloadedGroupMap(t *testing.T) {
	env := newTestOIDCEnv(t, func(cfg *config.OIDCConfig) {
		cfg.GroupMap = map[string]string{"idp-eng": "engineers"}
	})
	engineers, err := env.store.CreateGroup("engineers", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	claims := map[string]any{"groups": []any{"idp-eng"}}

	if rec := env.login(t, "/", claims); rec.Code != http.StatusFound {
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
	if !slices.Contains(groups, engineers) {
		t.Fatalf("before the reload groups = %v, want engineers (%d)", groups, engineers)
	}

	// Mapping the Workbench group to a different provider group, as an
	// operator would to stop a compromised provider group conferring it,
	// must revoke the membership at the next login rather than at the
	// next restart. (Removing the entry outright would leave the
	// Workbench group unmanaged, and ReconcileFederatedGroups leaves an
	// unmanaged group's membership alone by design.)
	env.reloadTo(func(cfg *config.OIDCConfig) {
		cfg.GroupMap = map[string]string{"idp-eng-vetted": "engineers"}
	})
	if rec := env.login(t, "/", claims); rec.Code != http.StatusFound {
		t.Fatalf("second login status = %d", rec.Code)
	}
	groups, err = env.store.GetUserGroups(userID)
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if slices.Contains(groups, engineers) {
		t.Fatalf("after the reload groups = %v, want engineers (%d) revoked", groups, engineers)
	}
}

func TestReloadSwitchingOIDCOffClosesBothEndpoints(t *testing.T) {
	env := newTestOIDCEnv(t)
	env.reloadTo(func(cfg *config.OIDCConfig) {
		cfg.Enabled = boolPointer(false)
	})

	rec := httptest.NewRecorder()
	env.handler.handleStart(rec, httptest.NewRequest(http.MethodGet, OIDCStartPath, nil))
	if got := rec.Header().Get("Location"); got != providerFailedTarget {
		t.Errorf("start Location = %q, want %q", got, providerFailedTarget)
	}

	rec = env.callback(t, nil, url.Values{"code": {"c"}, "state": {"s"}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("callback status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCallbackRefusesALoginWhenOIDCIsSwitchedOffDuringTheExchange(t *testing.T) {
	env := newTestOIDCEnv(t)

	cookie := env.startLogin(t, "/")
	state := env.openState(t, cookie)
	env.idp.SetNextIDToken(env.idp.MintIDToken(t, map[string]any{
		"sub": "subject-1", "email": testUsername, "nonce": state.Nonce,
	}))

	// The first read is the callback's own enabled check; every read
	// after it sees the reload that switched federated login off. This
	// relies on handleCallback reading the configuration exactly once
	// before completeLogin, which reads it once more, so the whole
	// callback makes wantReads reads. If a change adds a read on the way
	// (in openPresentedState or exchange, say), the reload would land
	// before the exchange rather than during it, and this test would no
	// longer exercise completeLogin's own check: move the switch to the
	// read just before completeLogin's and update wantReads to match.
	const wantReads = 2
	enabled := env.handler.cfg
	disabled := env.handler.cfg
	disabled.Enabled = boolPointer(false)
	reads := 0
	env.handler.SetConfigSource(func() config.OIDCConfig {
		reads++
		if reads == 1 {
			return enabled
		}
		return disabled
	})

	var rec *httptest.ResponseRecorder
	logged := captureLogDuring(t, func() {
		rec = env.callback(t, cookie, url.Values{
			"code":  {"authorization-code"},
			"state": {state.State},
		})
	})

	if reads != wantReads {
		t.Fatalf("the callback read the configuration %d times, want %d; "+
			"see the comment above wantReads", reads, wantReads)
	}
	if got := rec.Header().Get("Location"); got != loginFailedTarget {
		t.Fatalf("Location = %q, want %q", got, loginFailedTarget)
	}
	requireNoSessionCookie(t, rec)
	if !strings.Contains(logged, "switched off during the login") {
		t.Errorf("log = %q, want the reason for the refusal", logged)
	}
	user, err := env.store.GetUser(testUsername)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user != nil {
		t.Fatal("an account was provisioned after federated login was switched off")
	}
}
