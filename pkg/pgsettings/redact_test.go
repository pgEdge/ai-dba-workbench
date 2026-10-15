/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package pgsettings

import "testing"

func TestIsCredentialSetting(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"primary_conninfo", true},
		{"PRIMARY_CONNINFO", true},
		{"ssl_passphrase_command", true},
		{"ssl_passphrase_command_supports_reload", false},
		{"password_encryption", false},
		{"md5_password_warnings", false},
		{"restore_command", false},
		{"shared_buffers", false},
		{"myext.password", true},
		{"myext.api_token", true},
		{"myext.client_secret", true},
		{"myext.private_key", true},
		{"myext.work_mem", false},
	}
	for _, tt := range tests {
		if got := IsCredentialSetting(tt.name); got != tt.want {
			t.Errorf("IsCredentialSetting(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestRedactValue(t *testing.T) {
	tests := []struct {
		name, setting, value, want string
	}{
		{"conninfo keeps the rest", "primary_conninfo",
			"host=192.0.2.1 user=replicator password=s3cret",
			"host=192.0.2.1 user=replicator password=********"},
		{"conninfo URI", "primary_conninfo", "postgresql://u:s3cret@192.0.2.1/db",
			"postgresql://u:********@192.0.2.1/db"},
		{"conninfo without password", "primary_conninfo", "host=192.0.2.1",
			"host=192.0.2.1"},
		{"unset conninfo", "primary_conninfo", "", ""},
		{"passphrase command", "ssl_passphrase_command", "echo s3cret",
			RedactedValue},
		{"unset passphrase command", "ssl_passphrase_command", "", ""},
		{"extension secret", "myext.password", "s3cret", RedactedValue},
		{"ordinary setting", "work_mem", "4096", "4096"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RedactValue(tt.setting, tt.value); got != tt.want {
				t.Errorf("RedactValue(%q, %q) = %q, want %q",
					tt.setting, tt.value, got, tt.want)
			}
		})
	}
}
