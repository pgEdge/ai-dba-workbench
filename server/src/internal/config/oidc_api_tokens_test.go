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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #742: http.auth.oidc.allow_api_tokens.

func TestAPITokensAllowedDefaultsOff(t *testing.T) {
	if (OIDCConfig{}).APITokensAllowed() {
		t.Error("allow_api_tokens is on by default")
	}
	if (OIDCConfig{AllowAPITokens: boolPtr(false)}).APITokensAllowed() {
		t.Error("allow_api_tokens: false read as on")
	}
	if !(OIDCConfig{AllowAPITokens: boolPtr(true)}).APITokensAllowed() {
		t.Error("allow_api_tokens: true read as off")
	}
}

func TestMergeConfigAllowAPITokens(t *testing.T) {
	dest := defaultConfig()
	src := &Config{}
	mergeConfig(dest, src)
	if dest.HTTP.Auth.OIDC.APITokensAllowed() {
		t.Fatal("an unset source switched allow_api_tokens on")
	}

	src.HTTP.Auth.OIDC.AllowAPITokens = boolPtr(true)
	mergeConfig(dest, src)
	if !dest.HTTP.Auth.OIDC.APITokensAllowed() {
		t.Fatal("allow_api_tokens: true was not merged")
	}

	// An explicit false must be able to switch it back off.
	src.HTTP.Auth.OIDC.AllowAPITokens = boolPtr(false)
	mergeConfig(dest, src)
	if dest.HTTP.Auth.OIDC.APITokensAllowed() {
		t.Fatal("allow_api_tokens: false was not merged")
	}
}

func TestLoadConfigOIDCAllowAPITokens(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
		return path
	}

	tests := []struct {
		name    string
		path    string
		want    bool
		wantErr bool
	}{
		{"empty path", "", false, false},
		{"missing file", filepath.Join(dir, "absent.yaml"), false, false},
		{"empty file", write("empty.yaml", ""), false, false},
		{"not set", write("unset.yaml", "data_dir: /var/lib/x\n"), false, false},
		{"on", write("on.yaml", "http:\n  auth:\n    oidc:\n      allow_api_tokens: true\n"), true, false},
		{"off", write("off.yaml", "http:\n  auth:\n    oidc:\n      allow_api_tokens: false\n"), false, false},
		{"malformed", write("bad.yaml", "http: [\n"), false, true},
		{"a directory", dir, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadConfigOIDCAllowAPITokens(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// Switching allow_api_tokens is applied by a reload, and switching it on
// is warned about, since it lets tokens outlive a user's removal at the
// identity provider.
func TestReloadReportsAllowAPITokens(t *testing.T) {
	const (
		applied = "NOTE: http.auth.oidc.allow_api_tokens changed - applied"
		warning = "WARNING: http.auth.oidc.allow_api_tokens switched on"
	)
	withSetting := func(value *bool) *Config {
		cfg := oidcEnabledConfig(true)
		cfg.HTTP.Auth.OIDC.AllowAPITokens = value
		return cfg
	}

	tests := []struct {
		name        string
		old, reload *Config
		wantApplied bool
		wantWarning bool
	}{
		{"switched on", withSetting(nil), withSetting(boolPtr(true)), true, true},
		{"switched off", withSetting(boolPtr(true)), withSetting(boolPtr(false)), true, false},
		{"unset to explicit false", withSetting(nil), withSetting(boolPtr(false)), false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := &ReloadableConfig{config: tt.old, startup: oidcEnabledConfig(true)}
			out := captureStderr(t, func() {
				rc.logRestartRequiredSettings(tt.reload)
			})
			if strings.Contains(out, applied) != tt.wantApplied {
				t.Errorf("applied note present = %v, want %v:\n%s",
					!tt.wantApplied, tt.wantApplied, out)
			}
			if strings.Contains(out, warning) != tt.wantWarning {
				t.Errorf("warning present = %v, want %v:\n%s",
					!tt.wantWarning, tt.wantWarning, out)
			}
		})
	}
}
