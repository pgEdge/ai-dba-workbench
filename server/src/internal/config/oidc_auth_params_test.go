/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Issue #742: http.auth.oidc.auth_params.

func TestValidateOIDCAuthParams(t *testing.T) {
	tooMany := make(map[string]string, maxOIDCAuthParams+1)
	for i := range maxOIDCAuthParams + 1 {
		tooMany[fmt.Sprintf("param%d", i)] = "x"
	}

	tests := []struct {
		name    string
		params  map[string]string
		wantErr string
	}{
		{"none", nil, ""},
		{"google offline access", map[string]string{"access_type": "offline", "prompt": "consent"}, ""},
		{"dotted and hyphenated names", map[string]string{"x-tenant.id": "acme"}, ""},
		{"empty value", map[string]string{"hd": ""}, ""},
		{"reserved", map[string]string{"state": "fixed"}, `"state" is set by the Workbench`},
		{"reserved in another case", map[string]string{"Redirect_URI": "https://evil.example.com"}, `"Redirect_URI" is set by the Workbench`},
		{"request object", map[string]string{"request": "eyJ"}, `"request" is set by the Workbench`},
		{"response mode", map[string]string{"response_mode": "form_post"}, `"response_mode" is set by the Workbench`},
		{"empty name", map[string]string{"": "x"}, "is not a valid parameter name"},
		{"name with a space", map[string]string{"access type": "offline"}, "is not a valid parameter name"},
		{"name with an ampersand", map[string]string{"a&state": "x"}, "is not a valid parameter name"},
		{"name too long", map[string]string{strings.Repeat("a", maxOIDCAuthParamLength+1): "x"}, "is not a valid parameter name"},
		{"value too long", map[string]string{"prompt": strings.Repeat("a", maxOIDCAuthParamLength+1)}, "longer than"},
		{"control character", map[string]string{"prompt": "consent\r\nX: y"}, "control character"},
		{"delete character", map[string]string{"prompt": "consent\x7f"}, "control character"},
		{"too many", tooMany, "at most"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOIDCAuthParams(tt.params)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestIsReservedOIDCAuthParam(t *testing.T) {
	for _, name := range []string{"state", "NONCE", "Code_Challenge", "code_challenge_method",
		"redirect_uri", "client_id", "response_type", "scope", "response_mode",
		"request", "request_uri"} {
		if !IsReservedOIDCAuthParam(name) {
			t.Errorf("%q is not reported as reserved", name)
		}
	}
	for _, name := range []string{"access_type", "prompt", "hd", "login_hint"} {
		if IsReservedOIDCAuthParam(name) {
			t.Errorf("%q is reported as reserved", name)
		}
	}
}

// A reserved parameter is refused at load whether or not federated login
// is on, so that a reload switching it on cannot be the first check.
func TestLoadConfigRefusesAReservedAuthParam(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%v", enabled), func(t *testing.T) {
			cfg := oidcEnabledConfig(enabled)
			cfg.HTTP.Auth.OIDC.AuthParams = map[string]string{"nonce": "fixed"}
			err := validateConfig(cfg)
			if err == nil || !strings.Contains(err.Error(), "http.auth.oidc.auth_params") {
				t.Fatalf("error = %v, want the auth_params refusal", err)
			}
		})
	}
}

func TestLoadConfigReadsAuthParams(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "http:\n  auth:\n    oidc:\n      auth_params:\n" +
		"        access_type: offline\n        prompt: consent\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := loadConfigFile(path)
	if err != nil {
		t.Fatalf("loadConfigFile: %v", err)
	}
	want := map[string]string{"access_type": "offline", "prompt": "consent"}
	if !reflect.DeepEqual(cfg.HTTP.Auth.OIDC.AuthParams, want) {
		t.Errorf("auth_params = %v, want %v", cfg.HTTP.Auth.OIDC.AuthParams, want)
	}
}

func TestMergeConfigAuthParams(t *testing.T) {
	dest := defaultConfig()
	mergeConfig(dest, &Config{})
	if dest.HTTP.Auth.OIDC.AuthParams != nil {
		t.Fatalf("an unset source set auth_params to %v", dest.HTTP.Auth.OIDC.AuthParams)
	}

	src := &Config{}
	src.HTTP.Auth.OIDC.AuthParams = map[string]string{"prompt": "consent"}
	mergeConfig(dest, src)
	if dest.HTTP.Auth.OIDC.AuthParams["prompt"] != "consent" {
		t.Fatalf("auth_params = %v, want the source's", dest.HTTP.Auth.OIDC.AuthParams)
	}
}

// The login handler reads auth_params on every request, so a reload
// applies a change and must say so rather than warn about a restart.
func TestReloadReportsAuthParams(t *testing.T) {
	old := oidcEnabledConfig(true)
	reload := oidcEnabledConfig(true)
	reload.HTTP.Auth.OIDC.AuthParams = map[string]string{"access_type": "offline"}

	rc := &ReloadableConfig{config: old, startup: oidcEnabledConfig(true)}
	out := captureStderr(t, func() {
		rc.logRestartRequiredSettings(reload)
	})
	if !strings.Contains(out, "NOTE: http.auth.oidc.auth_params changed - applied") {
		t.Errorf("no applied note for auth_params:\n%s", out)
	}
	if strings.Contains(out, "auth_params changed - requires restart") {
		t.Errorf("auth_params reported as requiring a restart:\n%s", out)
	}
}
