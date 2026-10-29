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
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// unprefixedCookieConfig is the deployment issue #784 is about: OIDC
// behind a TLS-terminating proxy with no trusted_proxies, which 1.0.0
// started with a warning and this release refuses.
const unprefixedCookieConfig = `
http:
  auth:
    oidc:
      enabled: true
      issuer: https://idp.example.com
      client_id: workbench
      client_secret: s3cret
      redirect_url: https://workbench.example.com/api/v1/auth/oidc/callback
`

// writeCheckConfig writes body to a configuration file in a fresh
// temporary directory and returns its path.
func writeCheckConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ai-dba-server.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

// TestRunCheckConfig checks that -check-config accepts what start-up
// accepts and refuses what start-up refuses, with start-up's own error.
func TestRunCheckConfig(t *testing.T) {
	missingPasswordFile := filepath.Join(t.TempDir(), "missing-password")

	tests := []struct {
		name      string
		body      string
		cliFlags  func(path string) config.CLIFlags
		wantErr   string
		wantNotes []string
	}{
		{
			name:      "valid file",
			body:      "http:\n  address: \":8443\"\n",
			wantNotes: []string{"Config: ", "Configuration OK"},
		},
		{
			name:    "unprefixed state cookie refused",
			body:    unprefixedCookieConfig,
			wantErr: "allow_unprefixed_state_cookie",
		},
		{
			name:      "opt-out accepted",
			body:      unprefixedCookieConfig + "      allow_unprefixed_state_cookie: true\n",
			wantNotes: []string{"Configuration OK"},
		},
		{
			name:      "trusted proxies accepted",
			body:      unprefixedCookieConfig + "  trusted_proxies: [\"192.0.2.10/32\"]\n",
			wantNotes: []string{"Configuration OK"},
		},
		{
			// The check applies the command line as start-up does, so a
			// server run with -tls is judged as terminating TLS itself.
			name: "command-line tls applied",
			body: unprefixedCookieConfig,
			cliFlags: func(string) config.CLIFlags {
				return config.CLIFlags{
					TLSEnabledSet: true, TLSEnabled: true,
					TLSCertSet: true, TLSCertFile: "/nonexistent/cert.pem",
					TLSKeySet: true, TLSKeyFile: "/nonexistent/key.pem",
				}
			},
			wantNotes: []string{"Configuration OK"},
		},
		{
			name:    "unparseable file refused",
			body:    "http: [\n",
			wantErr: "failed to parse YAML",
			cliFlags: func(path string) config.CLIFlags {
				return config.CLIFlags{ConfigFileSet: true, ConfigFile: path}
			},
		},
		{
			name: "unreadable password_file refused",
			body: "database:\n  user: workbench\n  password_file: " +
				missingPasswordFile + "\n",
			wantErr: "missing-password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeCheckConfig(t, tt.body)
			var cliFlags config.CLIFlags
			if tt.cliFlags != nil {
				cliFlags = tt.cliFlags(path)
			}

			var out bytes.Buffer
			err := runCheckConfig(path, cliFlags, &out)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got none", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected an error containing %q, got %v", tt.wantErr, err)
				}
				if strings.Contains(out.String(), "Configuration OK") {
					t.Fatalf("reported OK despite the error: %q", out.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, note := range tt.wantNotes {
				if !strings.Contains(out.String(), note) {
					t.Errorf("output %q lacks %q", out.String(), note)
				}
			}
		})
	}
}

// TestRunCheckConfigMatchesStartup checks that the check and start-up
// return the same error for the same file, since start-up goes through
// loadStartupConfig and the packages rely on the two agreeing.
func TestRunCheckConfigMatchesStartup(t *testing.T) {
	path := writeCheckConfig(t, unprefixedCookieConfig)

	_, startupErr := loadStartupConfig(path, config.CLIFlags{})
	checkErr := runCheckConfig(path, config.CLIFlags{}, &bytes.Buffer{})
	if startupErr == nil || checkErr == nil {
		t.Fatalf("expected both to fail, got start-up %v and check %v", startupErr, checkErr)
	}
	if startupErr.Error() != checkErr.Error() {
		t.Fatalf("errors differ:\nstart-up: %v\ncheck:    %v", startupErr, checkErr)
	}
}

// TestRunCheckConfigMissingFile covers the two ways a file can be
// absent: named with -config, which is an error as it is at start-up,
// and a missing default, which falls back to the compiled-in defaults.
func TestRunCheckConfigMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.yaml")

	err := runCheckConfig(missing, config.CLIFlags{ConfigFileSet: true, ConfigFile: missing}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "configuration file not found") {
		t.Fatalf("expected a not-found error, got %v", err)
	}

	var out bytes.Buffer
	if err := runCheckConfig("", config.CLIFlags{}, &out); err != nil {
		t.Fatalf("defaults should validate, got %v", err)
	}
	if !strings.Contains(out.String(), "using defaults") || !strings.Contains(out.String(), "Configuration OK") {
		t.Fatalf("unexpected output %q", out.String())
	}
}

// TestParseFlagsCheckConfig checks the flag's name.
func TestParseFlagsCheckConfig(t *testing.T) {
	if parseTestFlags(t).CheckConfig {
		t.Fatal("-check-config must default to false")
	}
	if !parseTestFlags(t, "-check-config").CheckConfig {
		t.Fatal("-check-config was not parsed")
	}
}
