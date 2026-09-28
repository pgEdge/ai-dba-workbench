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

func TestToReloadCLIFlags(t *testing.T) {
	f := &Flags{
		DBHost:     "testhost",
		DBPort:     5433,
		DBName:     "testdb",
		DBUser:     "testuser",
		DBPassword: "testpass",
		DBSSLMode:  "require",
	}

	result := f.ToReloadCLIFlags()

	if result.DBHost != "testhost" {
		t.Errorf("expected DBHost 'testhost', got %q", result.DBHost)
	}
	if result.DBPort != 5433 {
		t.Errorf("expected DBPort 5433, got %d", result.DBPort)
	}
	if result.DBName != "testdb" {
		t.Errorf("expected DBName 'testdb', got %q", result.DBName)
	}
	if result.DBUser != "testuser" {
		t.Errorf("expected DBUser 'testuser', got %q", result.DBUser)
	}
	if result.DBPassword != "testpass" {
		t.Errorf("expected DBPassword 'testpass', got %q", result.DBPassword)
	}
	if result.DBSSLMode != "require" {
		t.Errorf("expected DBSSLMode 'require', got %q", result.DBSSLMode)
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

// TestResolvePasswords checks that each password comes from its flag,
// then its file, and that an unreadable file is reported.
func TestResolvePasswords(t *testing.T) {
	file := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(file, []byte("from-the-file\n"), 0o600); err != nil {
		t.Fatalf("failed to write the password file: %v", err)
	}

	f := parseArgs(t, "-db-password", "from-the-flag",
		"-password-file", file)
	if err := f.ResolvePasswords(); err != nil {
		t.Fatalf("ResolvePasswords failed: %v", err)
	}
	if f.DBPassword != "from-the-flag" || f.UserPassword != "from-the-file" {
		t.Errorf("unexpected passwords: db %q, user %q", f.DBPassword,
			f.UserPassword)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	for _, tt := range []struct{ flag, want string }{
		{"-db-password-file", "resolving database password"},
		{"-password-file", "resolving user password"},
	} {
		f := parseArgs(t, tt.flag, missing)
		if err := f.ResolvePasswords(); err == nil ||
			!strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: expected %q, got %v", tt.flag, tt.want, err)
		}
	}
}

// TestGetDefaultPaths checks that the paths start from the running
// binary. The configuration and secret paths depend on which files the
// host has, so only the executable path is checked.
func TestGetDefaultPaths(t *testing.T) {
	execPath, _, _, err := GetDefaultPaths()
	if err != nil {
		t.Fatalf("GetDefaultPaths failed: %v", err)
	}
	if want, _ := os.Executable(); execPath != want {
		t.Errorf("expected %q, got %q", want, execPath)
	}
}
