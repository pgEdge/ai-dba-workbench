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
// replaced. ssl_passphrase_command is a shell command whose purpose is
// to produce the key passphrase, and it commonly embeds it, for example
// as an echo, so unlike the archive and recovery commands in
// shellCommandSettings it keeps nothing, not even its program name.
var wholeValueSettings = map[string]bool{
	"ssl_passphrase_command": true,
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
	names := make([]string, 0,
		len(conninfoSettings)+len(wholeValueSettings)+len(shellCommandSettings))
	for _, set := range []map[string]bool{conninfoSettings, wholeValueSettings, shellCommandSettings} {
		for name := range set {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// NonCredentialSettings returns the names of the core settings that
// look like a secret by name but hold none, sorted.
func NonCredentialSettings() []string {
	return slices.Clone(nonCredentialSettings)
}

// shellCommandSettings are the core settings whose value is a shell
// command that PostgreSQL runs for WAL archiving and recovery. Such a
// command commonly embeds a credential, for example a PGPASSWORD=...
// prefix, a cloud storage key or a password argument to a backup tool,
// so its arguments are redacted. The program name, and the names of any
// environment variables assigned before it, are kept so that the stored
// value still shows which tool the server runs.
var shellCommandSettings = map[string]bool{
	"archive_command":         true,
	"archive_cleanup_command": true,
	"restore_command":         true,
	"recovery_end_command":    true,
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
	if conninfoSettings[name] || wholeValueSettings[name] || shellCommandSettings[name] {
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

// IsConnectionStringSetting reports whether the named setting holds a
// libpq connection string, such as primary_conninfo, from which
// RedactValue removes only the passwords. Every other credential setting
// holds a shell command or an opaque secret.
func IsConnectionStringSetting(name string) bool {
	return conninfoSettings[strings.ToLower(name)]
}

// RedactValue returns value with any credential removed, given the name
// of the setting it belongs to. A connection string keeps everything but
// its passwords; a shell command keeps its program name and the names of
// any leading environment variable assignments, as described for
// RedactShellCommand; any other credential-bearing setting is replaced
// by RedactedValue in full. The empty string, which means the setting is
// unset, and values of other settings are returned unchanged. The
// function is idempotent.
func RedactValue(name, value string) string {
	if value == "" || !IsCredentialSetting(name) {
		return value
	}
	lower := strings.ToLower(name)
	if conninfoSettings[lower] {
		return connstring.RedactPasswords(value)
	}
	if shellCommandSettings[lower] {
		return RedactShellCommand(value)
	}
	return RedactedValue
}

// RedactShellCommand returns a shell command with everything that can
// hold a credential replaced by RedactedValue. The value of each leading
// NAME=value environment variable assignment is replaced, the program
// name that follows is kept, and all of its arguments are replaced by a
// single RedactedValue, so
//
//	PGPASSWORD=s3cret pg_receivewal -d "host=192.0.2.1" %p
//
// becomes
//
//	PGPASSWORD=******** pg_receivewal ********
//
// A program name is kept only if it is a plain word of letters, digits
// and the characters _ . / + - ~; a command that starts any other way, for
// example with a quoted path, a subshell or a redirection, or that uses
// $ expansion or an empty value in a leading assignment, is replaced in
// full, since its program name cannot be picked out with confidence.
// A command of whitespace alone holds nothing and is returned unchanged.
// The function is idempotent.
//
// Words are split as the shell splits them, so in
//
//	PGPASSWORD=foo bar pg_receivewal %p
//
// the shell sets PGPASSWORD to foo and runs bar, and bar is kept as the
// program name. A credential containing a space works only when it is
// quoted or escaped, and its whole value is then redacted; an unquoted
// one leaves a command that does not run as intended, and the words
// after its first are shown as the shell would read them.
func RedactShellCommand(command string) string {
	rest := strings.TrimLeft(command, " \t")
	if rest == "" {
		return command
	}

	var out []string
	for rest != "" {
		word, after, ok := nextShellWord(rest)
		if !ok {
			return RedactedValue
		}
		rest = strings.TrimLeft(after, " \t")

		if envName, envValue, isAssignment := strings.Cut(word, "="); isAssignment {
			// An empty value followed by a space, as in "PGPASSWORD= s3cret
			// cmd" or "PGPASSWORD='' s3cret cmd", is most likely a typo
			// that would otherwise expose the next word as the program
			// name. The value is checked after quote and escape removal,
			// so '', "" and an escaped blank count as empty too.
			if !isShellVariableName(envName) || isBlankShellWord(envValue) {
				return RedactedValue
			}
			out = append(out, envName+"="+RedactedValue)
			continue
		}

		if !isPlainProgramName(word) {
			return RedactedValue
		}
		out = append(out, word)
		if rest != "" {
			out = append(out, RedactedValue)
		}
		break
	}
	return strings.Join(out, " ")
}

// nextShellWord splits the first shell word from s, which must not start
// with a space or tab, honoring single quotes, double quotes and
// backslash escapes. It reports false if a quote is not closed, if the
// word ends in an unpaired backslash, if the word contains an unquoted
// character that would end or redirect the command, since what follows
// could then be a second command, or if the word contains a $ or a
// backtick outside single quotes. An expansion such as ${...}, $(...) or
// $'...' can hold spaces and quotes that the shell keeps within one
// word, so rather than model expansion the caller redacts such a command
// in full.
func nextShellWord(s string) (word, rest string, ok bool) {
	for i := 0; i < len(s); {
		if s[i] == ' ' || s[i] == '\t' {
			return s[:i], s[i:], true
		}
		next, ok := skipShellWordPart(s, i)
		if !ok {
			return "", "", false
		}
		i = next
	}
	return s, "", true
}

// unsafeShellChars are the unquoted characters that make nextShellWord
// give up: those that end or redirect a command, start a subshell or
// begin an expansion, and line breaks.
const unsafeShellChars = ";&|<>()`$\n\r"

// skipShellWordPart returns the index just past the part of a shell word
// that starts at s[i]: a single-quoted string, a double-quoted string, a
// backslash escape or one other character. It reports false where
// nextShellWord gives up.
func skipShellWordPart(s string, i int) (int, bool) {
	switch s[i] {
	case '\'':
		end := strings.IndexByte(s[i+1:], '\'')
		if end < 0 {
			return 0, false
		}
		return i + end + 2, true
	case '"':
		return skipDoubleQuoted(s, i+1)
	case '\\':
		if i+1 >= len(s) {
			return 0, false
		}
		return i + 2, true
	}
	if strings.IndexByte(unsafeShellChars, s[i]) >= 0 {
		return 0, false
	}
	return i + 1, true
}

// skipDoubleQuoted returns the index just past the closing quote of a
// double-quoted string whose contents start at s[i]. It reports false if
// the quote is not closed, or if the contents hold a $ or a backtick,
// which the shell expands inside double quotes.
func skipDoubleQuoted(s string, i int) (int, bool) {
	for ; i < len(s); i++ {
		switch s[i] {
		case '"':
			return i + 1, true
		case '$', '`':
			return 0, false
		case '\\':
			i++
		}
	}
	return 0, false
}

// isBlankShellWord reports whether word, a shell word or part of one
// that nextShellWord accepted, holds nothing but blanks once its quotes
// and backslash escapes are removed, as an empty quoted string or an
// escaped space or tab is. It does not track which quotes are open, so a quote or
// backslash that is itself quoted, as in '"' or '\ ', is read as syntax
// too; that errs towards blank, which only ever redacts a command in
// full.
func isBlankShellWord(word string) bool {
	for i := 0; i < len(word); i++ {
		switch c := word[i]; c {
		case '\'', '"':
			// Quote characters are syntax, not content; what they
			// enclose is checked like any other character.
		case '\\':
			// The escaped character, if any, is content. nextShellWord
			// rejects a word that ends in an unpaired backslash.
			i++
			if i < len(word) && !isShellBlank(word[i]) {
				return false
			}
		default:
			if !isShellBlank(c) {
				return false
			}
		}
	}
	return true
}

// isShellBlank reports whether c is a space or a tab.
func isShellBlank(c byte) bool {
	return c == ' ' || c == '\t'
}

// isShellVariableName reports whether name is a valid shell variable
// name: a letter or underscore followed by letters, digits and
// underscores.
func isShellVariableName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		switch {
		case c == '_', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// isPlainProgramName reports whether word is a program name or path that
// can be shown as it is: letters, digits and _ . / + - ~ only, so it
// holds no quoting, variable expansion or glob that could hide an
// argument.
func isPlainProgramName(word string) bool {
	for _, c := range word {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case strings.ContainsRune("_./+-~", c):
		default:
			return false
		}
	}
	return true
}
