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
	"strings"
	"testing"
)

// TestValidateOIDCRedirectURL covers the check that redirect_url names
// this server's own callback endpoint. The value is handed to the
// identity provider as the place to deliver the authorization code, so
// anything looser leaves a typo or a hostile edit free to send every
// user's code elsewhere.
func TestValidateOIDCRedirectURL(t *testing.T) {
	cases := map[string]struct {
		url      string
		wantErr  bool
		mentions string
	}{
		"the callback over https": {
			url: "https://workbench.example.com" + OIDCCallbackPath,
		},
		"a non-default port": {
			url: "https://workbench.example.com:8443" + OIDCCallbackPath,
		},
		"plain http on localhost, for development": {
			url: "http://localhost:8080" + OIDCCallbackPath,
		},
		"plain http on the loopback address": {
			url: "http://127.0.0.1:8080" + OIDCCallbackPath,
		},
		"plain http on the IPv6 loopback address": {
			url: "http://[::1]:8080" + OIDCCallbackPath,
		},
		"an upper-case scheme": {
			url: "HTTPS://workbench.example.com" + OIDCCallbackPath,
		},
		"behind a path-rewriting reverse proxy": {
			// The Workbench mounted under a prefix is an ordinary
			// arrangement: the browser sees the prefixed path whilst
			// this server routes the bare one.
			url: "https://example.com/workbench" + OIDCCallbackPath,
		},
		"behind several path segments of prefix": {
			url: "https://example.com/tools/db/workbench" + OIDCCallbackPath,
		},
		"plain http on a real host": {
			url: "http://workbench.example.com" + OIDCCallbackPath, wantErr: true,
			mentions: "must use https",
		},
		"a scheme that is neither": {
			url: "ftp://workbench.example.com" + OIDCCallbackPath, wantErr: true,
			mentions: "must use https",
		},
		"no host": {
			url: "https://" + OIDCCallbackPath, wantErr: true, mentions: "absolute URL",
		},
		"not absolute": {
			url: OIDCCallbackPath, wantErr: true, mentions: "absolute URL",
		},
		"unparseable": {
			url: "https://work bench.example.com" + OIDCCallbackPath, wantErr: true,
			mentions: "absolute URL",
		},
		"some other path": {
			url: "https://workbench.example.com/oauth2/callback", wantErr: true,
			mentions: "must end with",
		},
		"the start endpoint": {
			url: "https://workbench.example.com" + OIDCStartPath, wantErr: true,
			mentions: "must end with",
		},
		"no path at all": {
			url: "https://workbench.example.com", wantErr: true, mentions: "must end with",
		},
		"the callback path with something appended": {
			// A suffix match, not a prefix one: anything after the
			// callback path is a different endpoint.
			url:      "https://workbench.example.com" + OIDCCallbackPath + "/elsewhere",
			wantErr:  true,
			mentions: "must end with",
		},
		"a query string": {
			url:     "https://workbench.example.com" + OIDCCallbackPath + "?next=/admin",
			wantErr: true, mentions: "query string or fragment",
		},
		"a fragment": {
			url:     "https://workbench.example.com" + OIDCCallbackPath + "#top",
			wantErr: true, mentions: "query string or fragment",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateOIDCRedirectURL(testCase.url)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("validateOIDCRedirectURL(%q) = nil, want an error", testCase.url)
				}
				if !strings.Contains(err.Error(), testCase.mentions) {
					t.Errorf("error = %q, want it to mention %q", err, testCase.mentions)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateOIDCRedirectURL(%q) = %v, want nil", testCase.url, err)
			}
		})
	}
}

// TestValidateConfigRejectsAMisdirectedRedirectURL proves the check is
// actually wired into configuration loading, rather than sitting in a
// function nothing calls.
func TestValidateConfigRejectsAMisdirectedRedirectURL(t *testing.T) {
	path := writeTempConfig(t, `
http:
  auth:
    oidc:
      enabled: true
      issuer: https://idp.example.com
      client_id: workbench
      client_secret: s3cret
      redirect_url: https://attacker.example.net/collect
`)

	if _, err := LoadConfig(path, CLIFlags{}); err == nil {
		t.Fatal("a redirect_url pointing somewhere else was accepted")
	}
}

// TestValidateConfigAcceptsAProxyPrefixedRedirectURL is the other
// direction: a Workbench mounted under a reverse proxy prefix must load,
// since refusing that would be a bug rather than a safety measure.
func TestValidateConfigAcceptsAProxyPrefixedRedirectURL(t *testing.T) {
	path := writeTempConfig(t, `
http:
  auth:
    oidc:
      enabled: true
      issuer: https://idp.example.com
      client_id: workbench
      client_secret: s3cret
      redirect_url: https://example.com/workbench/api/v1/auth/oidc/callback
`)

	if _, err := LoadConfig(path, CLIFlags{}); err != nil {
		t.Fatalf("a proxy-prefixed redirect_url was refused: %v", err)
	}
}

// oidcConfigYAML is a minimal valid OIDC block, with provisionUsers
// appended when it is not empty.
func oidcConfigYAML(provisionUsers string) string {
	yaml := `
http:
  auth:
    oidc:
      enabled: true
      issuer: https://idp.example.com
      client_id: workbench
      client_secret: s3cret
      redirect_url: https://workbench.example.com/api/v1/auth/oidc/callback
`
	if provisionUsers != "" {
		yaml += "      provision_users: " + provisionUsers + "\n"
	}
	return yaml
}

// TestProvisionUsersCanBeDisabledExplicitly is the regression for a
// plain bool: an explicit "false" has to survive the whole load
// pipeline, defaults and merge included. Provisioning decides whether an
// unknown identity is given an account, so a switch that cannot be
// switched off is not a cosmetic problem.
//
// The subtests below load a single file onto the defaults, which proves
// the YAML parses into the right effective value but NOT the override
// the pointer exists for: nothing in that path sets the value true
// before the file is merged, so "false" has nothing to overcome and the
// old plain-bool code would pass it too. The override itself is
// exercised further down, and at the merge level by
// TestMergeConfigRestoresProvisionUsersToFalse.
func TestProvisionUsersCanBeDisabledExplicitly(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "explicitly false", value: "false"},
		{name: "explicitly true", value: "true", want: true},
		// Omitted entirely, provisioning is off: an operator who has
		// said nothing has not asked for accounts to be created.
		{name: "omitted", value: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cfg, err := LoadConfig(writeTempConfig(t, oidcConfigYAML(testCase.value)), CLIFlags{})
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if got := cfg.HTTP.Auth.OIDC.ProvisionUsersEnabled(); got != testCase.want {
				t.Fatalf("ProvisionUsersEnabled = %v, want %v", got, testCase.want)
			}
		})
	}

	// The real regression, run through the same steps LoadConfig runs:
	// an earlier source has already turned provisioning on, and the
	// operator's file turns it off again. Against the old plain bool the
	// "false" was discarded here and provisioning stayed on, which is
	// the bug. The environment and CLI stages are not repeated because
	// neither touches this key.
	t.Run("an explicit false overrides an earlier true", func(t *testing.T) {
		cfg := defaultConfig()
		enabled := true
		cfg.HTTP.Auth.OIDC.ProvisionUsers = &enabled

		fileCfg, err := loadConfigFile(writeTempConfig(t, oidcConfigYAML("false")))
		if err != nil {
			t.Fatalf("loadConfigFile: %v", err)
		}
		mergeConfig(cfg, fileCfg)
		if err := validateConfig(cfg); err != nil {
			t.Fatalf("validateConfig: %v", err)
		}

		if cfg.HTTP.Auth.OIDC.ProvisionUsersEnabled() {
			t.Fatal("an explicit false was discarded, leaving provisioning on")
		}
	})
}

// TestMergeConfigRestoresProvisionUsersToFalse is the merge-level half
// of the same regression: a later source saying false must override an
// earlier source saying true, which a plain bool could not express.
func TestMergeConfigRestoresProvisionUsersToFalse(t *testing.T) {
	enabled := true
	disabled := false

	dest := defaultConfig()
	dest.HTTP.Auth.OIDC.ProvisionUsers = &enabled

	mergeConfig(dest, &Config{HTTP: HTTPConfig{Auth: AuthConfig{
		OIDC: OIDCConfig{ProvisionUsers: &disabled},
	}}})

	if dest.HTTP.Auth.OIDC.ProvisionUsersEnabled() {
		t.Fatal("an explicit false did not override an earlier true")
	}

	// And a source that says nothing leaves the earlier value alone.
	dest.HTTP.Auth.OIDC.ProvisionUsers = &enabled
	mergeConfig(dest, &Config{})
	if !dest.HTTP.Auth.OIDC.ProvisionUsersEnabled() {
		t.Fatal("a silent source cleared the earlier true")
	}
}
