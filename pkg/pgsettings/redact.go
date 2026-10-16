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
	"slices"
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
// replaced. They are shell commands with no structure to parse:
// ssl_passphrase_command commonly embeds the key passphrase, for example
// as an echo, and the archive and recovery commands commonly carry an
// inline secret such as PGPASSWORD=... or AWS_SECRET_ACCESS_KEY=... for
// the tool they run.
var wholeValueSettings = map[string]bool{
	"ssl_passphrase_command":  true,
	"archive_command":         true,
	"archive_cleanup_command": true,
	"restore_command":         true,
	"recovery_end_command":    true,
}

// nonCredentialSettings are the core settings whose names contain a
// customSecretFragments entry yet hold no secret. Core settings are
// matched by name alone, so they are never redacted; the list is
// exported for callers that match names by fragment, such as the SQL
// statement redaction, so they can leave these values readable.
var nonCredentialSettings = []string{
	"md5_password_warnings",
	"password_encryption",
	"ssl_passphrase_command_supports_reload",
}

// CredentialSettings returns the names of the core settings that can
// hold a credential, sorted. Extension settings named like a secret are
// credential settings too, but cannot be listed; IsCredentialSetting
// recognises them.
func CredentialSettings() []string {
	names := make([]string, 0, len(conninfoSettings)+len(wholeValueSettings))
	for name := range conninfoSettings {
		names = append(names, name)
	}
	for name := range wholeValueSettings {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// NonCredentialSettings returns the names of the core settings that
// look like a secret by name but hold none, sorted.
func NonCredentialSettings() []string {
	return slices.Clone(nonCredentialSettings)
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

// RedactsWholeValue reports whether RedactValue replaces the whole of a
// non-empty value of the named setting, rather than only the passwords
// inside a connection string.
func RedactsWholeValue(name string) bool {
	return IsCredentialSetting(name) && !conninfoSettings[strings.ToLower(name)]
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
