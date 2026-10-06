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
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

func TestResolveDataDir(t *testing.T) {
	execPath := "/usr/local/bin/mcp-server"
	defaultDir := filepath.Join(filepath.Dir(execPath), "data")

	tests := []struct {
		name          string
		cliDataDir    string
		configDataDir string
		expected      string
	}{
		{
			name:          "CLI flag takes highest priority",
			cliDataDir:    "/cli/data/dir",
			configDataDir: "/config/data/dir",
			expected:      "/cli/data/dir",
		},
		{
			name:          "config takes priority over default",
			cliDataDir:    "",
			configDataDir: "/config/data/dir",
			expected:      "/config/data/dir",
		},
		{
			name:          "default when nothing set",
			cliDataDir:    "",
			configDataDir: "",
			expected:      defaultDir,
		},
		{
			name:          "CLI flag with empty config",
			cliDataDir:    "/cli/data/dir",
			configDataDir: "",
			expected:      "/cli/data/dir",
		},
		{
			name:          "relative path from CLI",
			cliDataDir:    "./relative/data",
			configDataDir: "/config/data/dir",
			expected:      "./relative/data",
		},
		{
			name:          "relative path from config",
			cliDataDir:    "",
			configDataDir: "./relative/data",
			expected:      "./relative/data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Flags{DataDir: tt.cliDataDir}
			result := f.ResolveDataDir(execPath, tt.configDataDir)

			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestResolveDataDirDefaultPath(t *testing.T) {
	// Test that the default path is correctly derived from the executable path
	tests := []struct {
		name     string
		execPath string
		expected string
	}{
		{
			name:     "standard unix path",
			execPath: "/usr/local/bin/mcp-server",
			expected: "/usr/local/bin/data",
		},
		{
			name:     "root path",
			execPath: "/mcp-server",
			expected: "/data",
		},
		{
			name:     "nested path",
			execPath: "/opt/pgedge/bin/ai-dba-server",
			expected: "/opt/pgedge/bin/data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Flags{DataDir: ""}
			result := f.ResolveDataDir(tt.execPath, "")

			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestHasTokenCommand(t *testing.T) {
	tests := []struct {
		name     string
		flags    Flags
		expected bool
	}{
		{"no commands", Flags{}, false},
		{"add token", Flags{AddTokenCmd: true}, true},
		{"remove token", Flags{RemoveTokenCmd: "abc"}, true},
		{"list tokens", Flags{ListTokensCmd: true}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.flags.HasTokenCommand()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHasUserCommand(t *testing.T) {
	tests := []struct {
		name     string
		flags    Flags
		expected bool
	}{
		{"no commands", Flags{}, false},
		{"add user", Flags{AddUserCmd: true}, true},
		{"update user", Flags{UpdateUserCmd: true}, true},
		{"delete user", Flags{DeleteUserCmd: true}, true},
		{"list users", Flags{ListUsersCmd: true}, true},
		{"enable user", Flags{EnableUserCmd: true}, true},
		{"disable user", Flags{DisableUserCmd: true}, true},
		{"add service account", Flags{AddServiceAccountCmd: true}, true},
		{"link oidc user", Flags{LinkOIDCUserCmd: true}, true},
		{"restore password alone is not a command", Flags{RestorePassword: true}, false},
		{"unlink oidc user", Flags{UnlinkOIDCUserCmd: true}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.flags.HasUserCommand()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHasGroupCommand(t *testing.T) {
	tests := []struct {
		name     string
		flags    Flags
		expected bool
	}{
		{"no commands", Flags{}, false},
		{"add group", Flags{AddGroupCmd: true}, true},
		{"delete group", Flags{DeleteGroupCmd: true}, true},
		{"list groups", Flags{ListGroupsCmd: true}, true},
		{"add member", Flags{AddMemberCmd: true}, true},
		{"remove member", Flags{RemoveMemberCmd: true}, true},
		{"list members", Flags{ListMembersCmd: true}, true},
		{"set superuser", Flags{SetSuperuserCmd: true}, true},
		{"unset superuser", Flags{UnsetSuperuserCmd: true}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.flags.HasGroupCommand()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHasPrivilegeCommand(t *testing.T) {
	tests := []struct {
		name     string
		flags    Flags
		expected bool
	}{
		{"no commands", Flags{}, false},
		{"grant privilege", Flags{GrantPrivilegeCmd: true}, true},
		{"revoke privilege", Flags{RevokePrivilegeCmd: true}, true},
		{"grant connection", Flags{GrantConnectionCmd: true}, true},
		{"revoke connection", Flags{RevokeConnectionCmd: true}, true},
		{"list privileges", Flags{ListPrivilegesCmd: true}, true},
		{"show group privileges", Flags{ShowGroupPrivilegesCmd: true}, true},
		{"register privilege", Flags{RegisterPrivilegeCmd: true}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.flags.HasPrivilegeCommand()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHasTokenScopeCommand(t *testing.T) {
	tests := []struct {
		name     string
		flags    Flags
		expected bool
	}{
		{"no commands", Flags{}, false},
		{"scope token connections", Flags{ScopeTokenConnCmd: true}, true},
		{"scope token tools", Flags{ScopeTokenToolsCmd: true}, true},
		{"clear token scope", Flags{ClearTokenScopeCmd: true}, true},
		{"show token scope", Flags{ShowTokenScopeCmd: true}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.flags.HasTokenScopeCommand()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHasAuditCommand(t *testing.T) {
	tests := []struct {
		name     string
		flags    Flags
		expected bool
	}{
		{"no commands", Flags{}, false},
		{"list audit", Flags{ListAuditCmd: true}, true},
		{"verify audit log", Flags{VerifyAuditCmd: true}, true},
		{"audit filter without a command", Flags{AuditActor: "dave"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.flags.HasAuditCommand()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHasCLICommand(t *testing.T) {
	tests := []struct {
		name     string
		flags    Flags
		expected bool
	}{
		{"no commands", Flags{}, false},
		{"token command", Flags{AddTokenCmd: true}, true},
		{"user command", Flags{AddUserCmd: true}, true},
		{"group command", Flags{AddGroupCmd: true}, true},
		{"privilege command", Flags{GrantPrivilegeCmd: true}, true},
		{"token scope command", Flags{ScopeTokenConnCmd: true}, true},
		{"audit command", Flags{ListAuditCmd: true}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.flags.HasCLICommand()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// parseTestFlags runs ParseFlags over args with a fresh flag set, so a
// test can exercise the real parsing and "explicitly set" tracking that
// main uses without colliding with the test binary's own flags.
func parseTestFlags(t *testing.T, args ...string) *Flags {
	t.Helper()
	oldCommandLine, oldArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = oldCommandLine, oldArgs })
	flag.CommandLine = flag.NewFlagSet("ai-dba-server", flag.ContinueOnError)
	os.Args = append([]string{"ai-dba-server"}, args...)
	return ParseFlags("/nonexistent/ai-dba-server.yaml")
}

// TestToReloadCLIFlags checks that a reload carries every flag given on
// the command line, with its "set" marker, and nothing that was not
// given, so a reload rebuilds the configuration the way start-up did.
func TestToReloadCLIFlags(t *testing.T) {
	f := parseTestFlags(t,
		"-config", "/etc/test.yaml", "-addr", ":9090", "-trace-file", "trace.log",
		"-tls", "-cert", "server.crt", "-key", "server.key", "-chain", "chain.pem",
		"-db-host", "testhost", "-db-port", "5433", "-db-name", "testdb",
		"-db-user", "testuser", "-db-password", "testpass", "-db-sslmode", "require")

	got := f.ToReloadCLIFlags()
	want := config.CLIFlags{
		ConfigFileSet: true, ConfigFile: "/etc/test.yaml",
		HTTPAddrSet: true, HTTPAddr: ":9090",
		TraceFileSet: true, TraceFile: "trace.log",
		TLSEnabledSet: true, TLSEnabled: true,
		TLSCertSet: true, TLSCertFile: "server.crt",
		TLSKeySet: true, TLSKeyFile: "server.key",
		TLSChainSet: true, TLSChainFile: "chain.pem",
		DBHostSet: true, DBHost: "testhost",
		DBPortSet: true, DBPort: 5433,
		DBNameSet: true, DBName: "testdb",
		DBUserSet: true, DBUser: "testuser",
		DBPassSet: true, DBPassword: "testpass",
		DBSSLSet: true, DBSSLMode: "require",
	}
	if got != want {
		t.Errorf("ToReloadCLIFlags() = %+v, want %+v", got, want)
	}
	if got != f.ToCLIFlags() {
		t.Error("ToReloadCLIFlags() differs from the start-up ToCLIFlags()")
	}

	if none := parseTestFlags(t).ToReloadCLIFlags(); none != (config.CLIFlags{}) {
		t.Errorf("ToReloadCLIFlags() with no flags = %+v, want nothing set", none)
	}
}

// TestReloadKeepsCommandLineTLS covers the SIGHUP regression review found
// in issue #506: with TLS enabled by -tls rather than in the file, OIDC
// enabled, an https redirect_url and no trusted_proxies, start-up passes
// because the server terminates TLS itself, and reloading the unchanged
// file must pass too rather than being refused as though TLS were off.
func TestReloadKeepsCommandLineTLS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-dba-server.yaml")
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
		t.Fatalf("writing the config file: %v", err)
	}

	f := parseTestFlags(t, "-config", path, "-tls", "-cert", "server.crt", "-key", "server.key")
	initial, err := config.LoadConfig(path, f.ToCLIFlags())
	if err != nil {
		t.Fatalf("LoadConfig at start-up: %v", err)
	}
	if !initial.HTTP.TLS.Enabled {
		t.Fatal("-tls did not enable TLS at start-up")
	}

	rc := config.NewReloadableConfig(initial, path, f.ToReloadCLIFlags())
	if err := rc.Reload(); err != nil {
		t.Fatalf("Reload() of the unchanged file = %v, want success", err)
	}
	reloaded := rc.Get()
	if !reloaded.HTTP.TLS.Enabled || reloaded.HTTP.TLS.CertFile != "server.crt" ||
		reloaded.HTTP.TLS.KeyFile != "server.key" {
		t.Errorf("reloaded TLS = %+v, want the command-line settings kept", reloaded.HTTP.TLS)
	}
}

// TestToCLIFlagsCarriesServerFlags covers the flags ToCLIFlags maps
// besides TLS and the database: the configuration file, the listen
// address and the trace file.
func TestToCLIFlagsCarriesServerFlags(t *testing.T) {
	f := parseTestFlags(t, "-config", "/etc/test.yaml", "-addr", ":9090", "-trace-file", "trace.log")

	got := f.ToCLIFlags()
	want := config.CLIFlags{
		ConfigFileSet: true, ConfigFile: "/etc/test.yaml",
		HTTPAddrSet: true, HTTPAddr: ":9090",
		TraceFileSet: true, TraceFile: "trace.log",
	}
	if got != want {
		t.Errorf("ToCLIFlags() = %+v, want %+v", got, want)
	}
}

// writeSecret writes a password file readable only by its owner.
func writeSecret(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing the password file: %v", err)
	}
	return path
}

// TestResolvePasswords covers each source ResolvePasswords reads the
// database and user passwords from, and the error for an unreadable
// password file.
func TestResolvePasswords(t *testing.T) {
	dbFile := writeSecret(t, "db-secret\n")
	userFile := writeSecret(t, "user-secret\n")
	missing := filepath.Join(t.TempDir(), "missing")

	cases := map[string]struct {
		args     []string
		wantDB   string
		wantUser string
		wantErr  string
	}{
		"nothing given": {},
		"password files": {
			args:     []string{"-db-password-file", dbFile, "-password-file", userFile},
			wantDB:   "db-secret",
			wantUser: "user-secret",
		},
		"command-line passwords": {
			args:     []string{"-db-password", "db-flag", "-password", "user-flag"},
			wantDB:   "db-flag",
			wantUser: "user-flag",
		},
		"unreadable database password file": {
			args:    []string{"-db-password-file", missing},
			wantErr: "resolving database password",
		},
		"unreadable user password file": {
			args:    []string{"-password-file", missing},
			wantErr: "resolving user password",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := parseTestFlags(t, tc.args...)
			var err error
			captureStderr(t, func() { err = f.ResolvePasswords() })
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ResolvePasswords() = %v, want an error containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolvePasswords() = %v", err)
			}
			if f.DBPassword != tc.wantDB || f.UserPassword != tc.wantUser {
				t.Errorf("passwords = (%q, %q), want (%q, %q)",
					f.DBPassword, f.UserPassword, tc.wantDB, tc.wantUser)
			}
		})
	}
}

// TestGetDefaultPaths checks the default paths are derived from the
// running executable.
func TestGetDefaultPaths(t *testing.T) {
	execPath, configPath, secretPath, err := GetDefaultPaths()
	if err != nil {
		t.Fatalf("GetDefaultPaths() = %v", err)
	}
	wantExec, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() = %v", err)
	}
	if execPath != wantExec {
		t.Errorf("execPath = %q, want %q", execPath, wantExec)
	}
	if configPath != config.GetDefaultConfigPath(wantExec) {
		t.Errorf("configPath = %q, want %q", configPath, config.GetDefaultConfigPath(wantExec))
	}
	if secretPath != config.GetDefaultSecretPath() {
		t.Errorf("secretPath = %q, want %q", secretPath, config.GetDefaultSecretPath())
	}
}

// parseArgs runs ParseFlags on args with a fresh flag set standing in
// for the process's own, restored when the test ends.
func parseArgs(t *testing.T, args ...string) *Flags {
	t.Helper()

	savedSet, savedArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = savedSet, savedArgs })
	flag.CommandLine = flag.NewFlagSet("ai-dba-server", flag.ContinueOnError)
	os.Args = append([]string{"ai-dba-server"}, args...)

	return ParseFlags("/etc/example/ai-dba-server.yaml")
}

// TestParseFlagsDefaults checks the values a bare invocation gets.
func TestParseFlagsDefaults(t *testing.T) {
	f := parseArgs(t)

	if f.ConfigFile != "/etc/example/ai-dba-server.yaml" ||
		f.AccessLevel != "read" || f.AuditLimit != 50 ||
		f.PreviousSecretFile != "" || f.ConfirmRechain {
		t.Errorf("unexpected defaults: %+v", f)
	}
	if got := f.ToCLIFlags(); got != (config.CLIFlags{}) {
		t.Errorf("expected no flag to be marked set, got %+v", got)
	}
}

// TestParseFlagsAuditRechain checks the flags the re-anchor reads.
func TestParseFlagsAuditRechain(t *testing.T) {
	f := parseArgs(t, "-rechain-audit-log", "-confirm-rechain",
		"-previous-secret-file", "/etc/example/old.secret")

	if !f.RechainAuditCmd || !f.ConfirmRechain ||
		f.PreviousSecretFile != "/etc/example/old.secret" ||
		!f.HasAuditCommand() {
		t.Errorf("unexpected flags: %+v", f)
	}
}

// TestToCLIFlags checks that each flag the configuration can override
// is marked set, with its value, only when given.
func TestToCLIFlags(t *testing.T) {
	f := parseArgs(t, "-config", "/etc/example/other.yaml",
		"-addr", "192.0.2.1:8080", "-tls", "-cert", "c.pem", "-key", "k.pem",
		"-chain", "ch.pem", "-db-host", "db.example.com", "-db-port", "5433",
		"-db-name", "workbench", "-db-user", "tester",
		"-db-password", "not-a-real-password", "-db-sslmode", "require",
		"-trace-file", "trace.log", "-debug")

	want := config.CLIFlags{
		ConfigFileSet: true, ConfigFile: "/etc/example/other.yaml",
		HTTPAddrSet: true, HTTPAddr: "192.0.2.1:8080",
		TLSEnabledSet: true, TLSEnabled: true,
		TLSCertSet: true, TLSCertFile: "c.pem",
		TLSKeySet: true, TLSKeyFile: "k.pem",
		TLSChainSet: true, TLSChainFile: "ch.pem",
		DBHostSet: true, DBHost: "db.example.com",
		DBPortSet: true, DBPort: 5433,
		DBNameSet: true, DBName: "workbench",
		DBUserSet: true, DBUser: "tester",
		DBPassSet: true, DBPassword: "not-a-real-password",
		DBSSLSet: true, DBSSLMode: "require",
		TraceFileSet: true, TraceFile: "trace.log",
	}
	if got := f.ToCLIFlags(); got != want {
		t.Errorf("ToCLIFlags() =\n%+v\nwant\n%+v", got, want)
	}
}
