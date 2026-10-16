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
		{"ARCHIVE_COMMAND", true},
		{"archive_cleanup_command", true},
		{"recovery_end_command", true},
		{"archive_library", false},
		{"archive_mode", false},
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
		{"restore command with a key", "restore_command",
			"AWS_SECRET_ACCESS_KEY=s3cret aws s3 cp s3://bucket/%f %p",
			"AWS_SECRET_ACCESS_KEY=******** aws ********"},
		{"archive command with no arguments", "archive_command",
			"PGPASSWORD=s3cret pg_receivewal", "PGPASSWORD=******** pg_receivewal"},
		{"extension secret", "myext.password", "s3cret", RedactedValue},
		{"ordinary setting", "work_mem", "4096", "4096"},
		{"archive command", "archive_command",
			"PGPASSWORD=s3cret pgbackrest --stanza=main archive-push %p",
			"PGPASSWORD=******** pgbackrest ********"},
		{"restore command", "RESTORE_COMMAND", "cp /mnt/archive/%f %p",
			"cp ********"},
		{"unset archive command", "archive_command", "", ""},
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

func TestRedactShellCommand(t *testing.T) {
	tests := []struct {
		name, command, want string
	}{
		{"program and arguments", "cp %p /mnt/archive/%f", "cp ********"},
		{"program alone", "/usr/local/bin/archive.sh", "/usr/local/bin/archive.sh"},
		{"home-relative program", "~/bin/archive %p", "~/bin/archive ********"},
		{"trailing blanks only", "true \t ", "true"},
		{"leading blanks", " \t aws s3 cp %p s3://bucket/%f", "aws ********"},
		{"environment assignment",
			"AWS_SECRET_ACCESS_KEY=abc123 aws s3 cp %p s3://bucket/%f",
			"AWS_SECRET_ACCESS_KEY=******** aws ********"},
		{"several assignments", `A=1  _B2='x y' C="p\"q" wal-g wal-push %p`,
			"A=******** _B2=******** C=******** wal-g ********"},
		{"escaped space in assignment", `PGPASSWORD=a\ b pg_receivewal %p`,
			"PGPASSWORD=******** pg_receivewal ********"},
		{"assignments alone", "PGPASSWORD=s3cret", "PGPASSWORD=********"},
		{"empty assignment", "PGPASSWORD= s3cret pg_receivewal %p", RedactedValue},
		{"single-quoted empty assignment",
			"PGPASSWORD='' s3cretword pg_receivewal %p", RedactedValue},
		{"double-quoted empty assignment",
			`PGPASSWORD="" s3cretword pg_receivewal %p`, RedactedValue},
		{"escaped-blank assignment",
			`PGPASSWORD=\  s3cretword pg_receivewal %p`, RedactedValue},
		{"quoted-blank assignment",
			"PGPASSWORD=' \t' s3cretword pg_receivewal %p", RedactedValue},
		{"several empty quotes", `PGPASSWORD=''""'' s3cretword cp %p`,
			RedactedValue},
		{"escaped-blank inside double quotes",
			`PGPASSWORD="\ " s3cretword cp %p`, RedactedValue},
		{"quoted value with content", `PGPASSWORD='' x"y"\z cp %p`,
			RedactedValue},
		{"non-blank quoted value", `PGPASSWORD=''"a" pg_receivewal %p`,
			"PGPASSWORD=******** pg_receivewal ********"},
		{"escaped non-blank value", `PGPASSWORD=\a pg_receivewal %p`,
			"PGPASSWORD=******** pg_receivewal ********"},
		{"quoted quote read as blank", `PGPASSWORD='"' s3cretword cp %p`,
			RedactedValue},
		{"variable in assignment", "X=$HOME/pw cp %p /x", RedactedValue},
		{"substitution in double quotes",
			`PGPASSWORD="$(echo "correct horse battery")" pg_receivewal %p`, RedactedValue},
		{"backtick in double quotes", "X=\"`cat /pw`\" cp %p /x", RedactedValue},
		{"parameter expansion", "PGPASSWORD=${PW:-my secret pass} pg_basebackup", RedactedValue},
		{"ANSI-C quoting", `PGPASSWORD=$'x\' secretpart ' cmd`, RedactedValue},
		{"dollar in single quotes", "X='$HOME' cp %p /x", "X=******** cp ********"},
		{"invalid variable name", "1X=s3cret cp %p /x", RedactedValue},
		{"empty variable name", "=s3cret cp %p /x", RedactedValue},
		{"empty quoted program", "'' %p", RedactedValue},
		{"quoted variable name", "'X'=s3cret cp %p /x", RedactedValue},
		{"option before program", "--password=s3cret", RedactedValue},
		{"quoted program", "'/opt/my tool/archive' %p", RedactedValue},
		{"variable as program", "$ARCHIVER %p", RedactedValue},
		{"subshell", "(cd /x && cp %p .)", RedactedValue},
		{"redirection after program", "echo s3cret > /tmp/x", "echo ********"},
		{"redirection first", ">/tmp/x echo s3cret", RedactedValue},
		{"separator in assignment", "X=1;cp %p /x", RedactedValue},
		{"pipe in assignment", "X=1|cat", RedactedValue},
		{"command substitution", "X=`cat /secret` cp %p /x", RedactedValue},
		{"newline in assignment", "X=1\ncp %p /x", RedactedValue},
		{"unclosed single quote", "X='s3cret cp %p /x", RedactedValue},
		{"unclosed double quote", `X="s3cret cp %p /x`, RedactedValue},
		{"escaped closing quote", `X="s3cret\"`, RedactedValue},
		{"trailing backslash", `X=s3cret\`, RedactedValue},
		{"glob in program", "c* %p", RedactedValue},
		{"already redacted", "PGPASSWORD=******** pgbackrest ********",
			"PGPASSWORD=******** pgbackrest ********"},
		{"redacted in full", RedactedValue, RedactedValue},
		{"blank", " \t", " \t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactShellCommand(tt.command)
			if got != tt.want {
				t.Errorf("RedactShellCommand(%q) = %q, want %q",
					tt.command, got, tt.want)
			}
			if again := RedactShellCommand(got); again != got {
				t.Errorf("RedactShellCommand is not idempotent: %q, then %q",
					got, again)
			}
		})
	}
}
