/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package pgsettings decides which PostgreSQL configuration settings can
// hold a credential and redacts their values. The collector applies it
// before storing pg_settings rows, and the server applies it again to
// rows read back, which covers rows stored by an older collector.
package pgsettings

import (
	"strings"

	"github.com/pgedge/ai-workbench/pkg/connstring"
)

// RedactedValue replaces a secret, either a whole setting value or the
// password inside a connection string.
const RedactedValue = connstring.RedactedPassword

// conninfoSettings are the core settings whose value is a libpq
// connection string. primary_conninfo is the only one: PostgreSQL shows
// it unredacted to members of pg_read_all_settings, whilst
// pg_stat_wal_receiver.conninfo obscures the same password. Only the
// password is removed, so the host, user and other options stay
// readable.
var conninfoSettings = map[string]bool{
	"primary_conninfo": true,
}

// wholeValueSettings are the core settings whose entire value is
// replaced. ssl_passphrase_command is a shell command that commonly
// embeds the key passphrase, for example as an echo, and has no
// structure to parse.
var wholeValueSettings = map[string]bool{
	"ssl_passphrase_command": true,
}

// customSecretFragments mark an extension (dotted) setting name as
// holding a secret. They are only applied to dotted names because core
// settings such as password_encryption and md5_password_warnings contain
// "password" yet hold no secret, and every core setting that does is
// listed above.
var customSecretFragments = []string{
	"password", "passwd", "passphrase", "secret", "token",
	"credential", "private_key",
}

// IsCredentialSetting reports whether the named setting can hold a
// credential and so has its values redacted.
func IsCredentialSetting(name string) bool {
	name = strings.ToLower(name)
	if conninfoSettings[name] || wholeValueSettings[name] {
		return true
	}
	if !strings.Contains(name, ".") {
		return false
	}
	for _, fragment := range customSecretFragments {
		if strings.Contains(name, fragment) {
			return true
		}
	}
	return false
}

// RedactValue returns value with any credential removed, given the name
// of the setting it belongs to. A connection string keeps everything but
// its passwords; any other credential-bearing setting is replaced by
// RedactedValue in full. The empty string, which means the setting is
// unset, and values of other settings are returned unchanged. The
// function is idempotent.
func RedactValue(name, value string) string {
	if value == "" || !IsCredentialSetting(name) {
		return value
	}
	if conninfoSettings[strings.ToLower(name)] {
		return connstring.RedactPasswords(value)
	}
	return RedactedValue
}
