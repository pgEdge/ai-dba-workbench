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
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// unprefixedCookieConfig is the example issue #784 gives of a
// configuration start-up refuses: OIDC behind a TLS-terminating proxy
// with no trusted_proxies.
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

// checkConfigMainEnv names the environment variable that makes the test
// binary, re-executed by TestMainCheckConfigExitStatus, run main() with
// -check-config on the file it names instead of running the tests.
const checkConfigMainEnv = "AI_DBA_TEST_CHECK_CONFIG_MAIN"

// TestMainCheckConfigExitStatus runs main() with -check-config in a
// child process and checks its exit status, which the Debian postinst
// and RPM %pre rely on to refuse an upgrade: zero for a configuration
// start-up accepts, one for one it refuses.
func TestMainCheckConfigExitStatus(t *testing.T) {
	if path := os.Getenv(checkConfigMainEnv); path != "" {
		flag.CommandLine = flag.NewFlagSet("ai-dba-server", flag.ExitOnError)
		os.Args = []string{"ai-dba-server", "-check-config", "-config", path}
		main()
		// main returns after a successful check; anything after that
		// would start the server, so exit here as the real binary does.
		os.Exit(0)
	}

	tests := []struct {
		name     string
		body     string
		wantCode int
		wantOut  string
	}{
		{name: "valid", body: "http:\n  address: \":8443\"\n", wantCode: 0, wantOut: "Configuration OK"},
		{name: "refused", body: unprefixedCookieConfig, wantCode: 1, wantOut: "allow_unprefixed_state_cookie"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeCheckConfig(t, tt.body)
			code, out := runMainChild(t, "TestMainCheckConfigExitStatus", checkConfigMainEnv, path)
			if code != tt.wantCode {
				t.Fatalf("exit status %d, want %d; output:\n%s", code, tt.wantCode, out)
			}
			if !strings.Contains(string(out), tt.wantOut) {
				t.Fatalf("output lacks %q:\n%s", tt.wantOut, out)
			}
		})
	}
}

// runMainChild re-executes this test binary, running only testName,
// with envName set to value, and returns the child's exit status and
// combined output.
func runMainChild(t *testing.T, testName, envName, value string) (int, string) {
	t.Helper()
	//nolint:gosec // G204: re-executes this test binary with a fixed test filter.
	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), envName+"="+value)
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), string(out)
	}
	if err != nil {
		t.Fatalf("running main: %v", err)
	}
	return 0, string(out)
}

// startupMainEnv names the environment variable that makes the test
// binary, re-executed by TestMainStartupRefusesWhatCheckRefuses, run
// main() without -check-config, as the server starts, on the
// configuration file and data directory it names, separated by a
// newline.
const startupMainEnv = "AI_DBA_TEST_STARTUP_MAIN"

// errorLine returns the line of out that main() writes for a fatal
// error, or "" if there is none.
func errorLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "ERROR: ") {
			return line
		}
	}
	return ""
}

// TestMainStartupRefusesWhatCheckRefuses runs main() as the server
// starts, without -check-config, on a configuration whose password_file
// cannot be read, and checks that start-up exits with status 1 and the
// error -check-config gives for the same file. It pins start-up to
// loadStartupConfig: were start-up to call config.LoadConfig directly,
// it would no longer read password_file whilst the check still did, and
// the two would drift apart.
//
// Start-up fails at loadStartupConfig, before NewServer, so it reads no
// secret and opens no auth store, database or socket. The file names a
// secret_file that does not exist, and the data directory is a fresh
// temporary one, so that a start-up which did get past the configuration
// would stop at the server secret rather than reach a database.
func TestMainStartupRefusesWhatCheckRefuses(t *testing.T) {
	if spec := os.Getenv(startupMainEnv); spec != "" {
		path, dataDir, _ := strings.Cut(spec, "\n")
		flag.CommandLine = flag.NewFlagSet("ai-dba-server", flag.ExitOnError)
		os.Args = []string{"ai-dba-server", "-config", path, "-data-dir", dataDir}
		main()
		// main only returns here if start-up went wrong; say so rather
		// than let the child pass as a test.
		os.Exit(2)
	}

	dir := t.TempDir()
	missingPasswordFile := filepath.Join(dir, "missing-password")
	path := writeCheckConfig(t, "secret_file: "+filepath.Join(dir, "missing-secret")+
		"\ndatabase:\n  user: workbench\n  password_file: "+missingPasswordFile+"\n")

	checkCode, checkOut := runMainChild(t, "TestMainCheckConfigExitStatus", checkConfigMainEnv, path)
	if checkCode != 1 {
		t.Fatalf("-check-config exit status %d, want 1; output:\n%s", checkCode, checkOut)
	}
	checkErr := errorLine(checkOut)
	if !strings.Contains(checkErr, missingPasswordFile) {
		t.Fatalf("-check-config error does not name the password file:\n%s", checkOut)
	}

	startCode, startOut := runMainChild(t, "TestMainStartupRefusesWhatCheckRefuses",
		startupMainEnv, path+"\n"+t.TempDir())
	if startCode != 1 {
		t.Fatalf("start-up exit status %d, want 1; output:\n%s", startCode, startOut)
	}
	if startErr := errorLine(startOut); startErr != checkErr {
		t.Fatalf("errors differ:\nstart-up: %s\ncheck:    %s", startErr, checkErr)
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
