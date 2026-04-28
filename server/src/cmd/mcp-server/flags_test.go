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
	"path/filepath"
	"testing"
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
