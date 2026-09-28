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
	"strings"
	"testing"
)

// stateCookieTestConfig returns a configuration that passes every OIDC
// check except, depending on the mutation, the one for the "__Host-"
// state cookie prefix: federated login is enabled with an https
// redirect_url, TLS is off and no trusted proxy is listed.
func stateCookieTestConfig() *Config {
	cfg := defaultConfig()
	cfg.HTTP.Auth.OIDC.Enabled = boolPtr(true)
	cfg.HTTP.Auth.OIDC.Issuer = "https://idp.example.com"
	cfg.HTTP.Auth.OIDC.ClientID = "workbench"
	cfg.HTTP.Auth.OIDC.ClientSecret = "s3cret"
	cfg.HTTP.Auth.OIDC.RedirectURL = "https://workbench.example.com/api/v1/auth/oidc/callback"
	return cfg
}

// TestValidateConfigRefusesAnUnprefixedStateCookie covers issue #506.
// Behind a TLS-terminating proxy with no http.trusted_proxies, the login
// state cookie is written without the "__Host-" prefix, which lets a
// sibling subdomain plant its own login state (login CSRF). Validation
// refuses that unless the operator has opted out explicitly, and accepts
// every configuration on which the prefix is available or impossible.
func TestValidateConfigRefusesAnUnprefixedStateCookie(t *testing.T) {
	cases := map[string]struct {
		mutate  func(*Config)
		wantErr bool
	}{
		"https redirect, no TLS, no trusted proxies": {
			mutate:  func(*Config) {},
			wantErr: true,
		},
		"upper-case https scheme is still https": {
			mutate: func(cfg *Config) {
				cfg.HTTP.Auth.OIDC.RedirectURL = "HTTPS://workbench.example.com/api/v1/auth/oidc/callback"
			},
			wantErr: true,
		},
		"explicit opt-out false still refuses": {
			mutate:  func(cfg *Config) { cfg.HTTP.Auth.OIDC.AllowUnprefixedStateCookie = boolPtr(false) },
			wantErr: true,
		},
		"trusted proxies configured": {
			mutate: func(cfg *Config) { cfg.HTTP.TrustedProxies = []string{"10.0.0.0/8"} },
		},
		"TLS terminated by the server": {
			mutate: func(cfg *Config) {
				cfg.HTTP.TLS.Enabled = true
				cfg.HTTP.TLS.CertFile = "server.crt"
				cfg.HTTP.TLS.KeyFile = "server.key"
			},
		},
		"explicit opt-out": {
			mutate: func(cfg *Config) { cfg.HTTP.Auth.OIDC.AllowUnprefixedStateCookie = boolPtr(true) },
		},
		"plain http loopback redirect for local development": {
			mutate: func(cfg *Config) {
				cfg.HTTP.Auth.OIDC.RedirectURL = "http://localhost:8080/api/v1/auth/oidc/callback"
			},
		},
		"OIDC disabled": {
			mutate: func(cfg *Config) { cfg.HTTP.Auth.OIDC.Enabled = boolPtr(false) },
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := stateCookieTestConfig()
			tc.mutate(cfg)
			err := validateConfig(cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected validation to refuse an unprefixed state cookie")
				}
				for _, want := range []string{
					"http.trusted_proxies",
					"__Host-",
					"login CSRF",
					"allow_unprefixed_state_cookie: true",
				} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not mention %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("expected validation to pass, got: %v", err)
			}
		})
	}
}

// TestValidateOIDCStateCookiePrefixIgnoresAnUnparseableRedirect covers
// the defensive branch: validateOIDCRedirectURL refuses such a value
// before this check runs, so this one must simply not add a second,
// misleading, error.
func TestValidateOIDCStateCookiePrefixIgnoresAnUnparseableRedirect(t *testing.T) {
	cfg := stateCookieTestConfig()
	cfg.HTTP.Auth.OIDC.RedirectURL = "://nowhere"
	if err := validateOIDCStateCookiePrefix(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestAllowUnprefixedStateCookieLoadsFromYAML checks the setting's key
// name and its path through the merge, which the validation test above
// does not exercise.
func TestAllowUnprefixedStateCookieLoadsFromYAML(t *testing.T) {
	body := `
http:
  auth:
    oidc:
      enabled: true
      issuer: https://idp.example.com
      client_id: workbench
      client_secret: s3cret
      redirect_url: https://workbench.example.com/api/v1/auth/oidc/callback
`
	if _, err := LoadConfig(writeTempConfig(t, body), CLIFlags{}); err == nil {
		t.Fatal("LoadConfig accepted an unprefixed state cookie without the opt-out")
	}

	cfg, err := LoadConfig(writeTempConfig(t, body+"      allow_unprefixed_state_cookie: true\n"), CLIFlags{})
	if err != nil {
		t.Fatalf("LoadConfig with the opt-out: %v", err)
	}
	if !cfg.HTTP.Auth.OIDC.UnprefixedStateCookieAllowed() {
		t.Fatal("allow_unprefixed_state_cookie: true did not survive the load")
	}
}

// TestUnprefixedStateCookieAllowedDefaultsToFalse checks the accessor:
// an operator who has said nothing has not accepted the risk.
func TestUnprefixedStateCookieAllowedDefaultsToFalse(t *testing.T) {
	if defaultConfig().HTTP.Auth.OIDC.UnprefixedStateCookieAllowed() {
		t.Fatal("allow_unprefixed_state_cookie must default to false")
	}
}

// TestReloadRefusesAnUnprefixedStateCookie checks that a SIGHUP reload
// applies the same rule as start-up. The OIDC settings take effect only
// at a restart, so the running server is unaffected either way, but a
// reload that accepted the file would leave a configuration on disk that
// the next start refuses; rejecting it now tells the operator at once.
func TestReloadRefusesAnUnprefixedStateCookie(t *testing.T) {
	path := writeTempConfig(t, `
http:
  trusted_proxies: ["10.0.0.0/8"]
  auth:
    oidc:
      enabled: true
      issuer: https://idp.example.com
      client_id: workbench
      client_secret: s3cret
      redirect_url: https://workbench.example.com/api/v1/auth/oidc/callback
`)
	flags := CLIFlags{ConfigFileSet: true, ConfigFile: path}
	initial, err := LoadConfig(path, flags)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	rc := NewReloadableConfig(initial, path, flags)

	if err := os.WriteFile(path, []byte(`
http:
  auth:
    oidc:
      enabled: true
      issuer: https://idp.example.com
      client_id: workbench
      client_secret: s3cret
      redirect_url: https://workbench.example.com/api/v1/auth/oidc/callback
`), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	err = rc.Reload()
	if err == nil || !strings.Contains(err.Error(), "allow_unprefixed_state_cookie") {
		t.Fatalf("Reload() = %v, want a refusal naming allow_unprefixed_state_cookie", err)
	}
	if rc.Get() != initial {
		t.Error("Reload() swapped the configuration despite refusing it")
	}
}
