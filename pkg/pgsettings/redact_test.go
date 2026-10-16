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

import (
	"slices"
	"testing"
)

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
		{"restore_command", true},
		{"archive_command", true},
		{"archive_cleanup_command", true},
		{"recovery_end_command", true},
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
		{"restore command", "restore_command",
			"AWS_SECRET_ACCESS_KEY=s3cret aws s3 cp s3://bucket/%f %p", RedactedValue},
		{"archive command", "archive_command",
			"PGPASSWORD=s3cret pg_receivewal", RedactedValue},
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

func TestCredentialSettings(t *testing.T) {
	want := []string{
		"archive_cleanup_command", "archive_command", "primary_conninfo",
		"recovery_end_command", "restore_command", "ssl_passphrase_command",
	}
	got := CredentialSettings()
	if !slices.Equal(got, want) {
		t.Errorf("CredentialSettings() = %v, want %v", got, want)
	}
	for _, name := range got {
		if !IsCredentialSetting(name) {
			t.Errorf("IsCredentialSetting(%q) = false for a listed setting", name)
		}
	}
}

func TestNonCredentialSettings(t *testing.T) {
	got := NonCredentialSettings()
	if len(got) == 0 || !slices.IsSorted(got) {
		t.Fatalf("NonCredentialSettings() = %v, want a sorted, non-empty list", got)
	}
	for _, name := range got {
		if IsCredentialSetting(name) {
			t.Errorf("IsCredentialSetting(%q) = true for a non-credential setting", name)
		}
	}
	got[0] = "changed"
	if NonCredentialSettings()[0] == "changed" {
		t.Error("NonCredentialSettings() returned the package's own slice")
	}
}

func TestRedactsWholeValue(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"primary_conninfo", false},
		{"restore_command", true},
		{"ssl_passphrase_command", true},
		{"myext.password", true},
		{"work_mem", false},
	}
	for _, tt := range tests {
		if got := RedactsWholeValue(tt.name); got != tt.want {
			t.Errorf("RedactsWholeValue(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
