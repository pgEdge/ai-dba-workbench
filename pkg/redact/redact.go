/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package redact holds the patterns that mask passwords in free text,
// such as SQL statements, connection strings and error messages, so the
// server and the alerter mask them in the same way. The server applies
// them in SQL with regexp_replace (internal/metrics.RedactedText), and
// Text applies them in Go.
package redact

import (
	"regexp"
	"slices"
	"strings"
)

// RedactedValue replaces a masked value, as PostgreSQL masks a password
// in pg_stat_wal_receiver.conninfo.
const RedactedValue = "********"

// secretColumnFragments are name fragments that mark a column as
// holding a credential. The server never returns a metric column whose
// name contains one, so a future probe cannot expose a secret by
// accident. The connection string fragments cover pg_stat_wal_receiver.conninfo and
// the like: PostgreSQL obscures the password in some of them, but the
// rest of a connection string is still credential material.
var secretColumnFragments = []string{
	"password", "passwd", "passphrase", "secret", "token", "credential", "private_key",
	"conninfo", "connstr", "connection_string", "dsn",
	"sslcert", "sslkey", "sslrootcert",
}

// textSecretNamePatterns are the names whose value is masked in free
// text: the secretColumnFragments, which are specific enough not to
// catch ordinary identifiers such as sort_key or bypass, plus the API
// and access key names.
var textSecretNamePatterns = append(slices.Clone(secretColumnFragments), "api_?key", "access_key")

// credentialSettings are setting names whose value can carry a
// credential: primary_conninfo is a libpq connection string that may
// hold password=, and the rest are shell commands that may carry a
// passphrase or an inline secret such as PGPASSWORD=... or
// AWS_SECRET_ACCESS_KEY=.... The server also masks any setting whose
// name matches one of its secret name patterns, which covers an
// extension setting such as myext.password.
var credentialSettings = []string{
	"primary_conninfo",
	"ssl_passphrase_command",
	"archive_command",
	"archive_cleanup_command",
	"restore_command",
	"recovery_end_command",
}

// TextRedaction is one password form that Text, and the server's
// RedactedText, mask: Pattern is a case-insensitive regular expression
// in the subset PostgreSQL's advanced regular expressions share with
// Go's RE2, so Text applies it in Go, and Replacement keeps capture
// group 1 (\1), the only capturing group, and puts RedactedValue in
// place of the secret. Every quantifier is greedy, so PostgreSQL takes
// the longest match, as Text does with Longest.
type TextRedaction struct {
	Name        string
	Pattern     string
	Replacement string
}

// secretKeyPattern returns a regular expression with no capturing
// group that matches a name holding a textSecretNamePatterns entry,
// such as password, AWS_SECRET_ACCESS_KEY or app.api_key: lead and
// trail are what may come before and after the entry.
func secretKeyPattern(lead, trail string) string {
	return lead + "(?:" + strings.Join(textSecretNamePatterns, "|") + ")" + trail
}

// Pieces of the textRedactions patterns. A quoted value may lack its
// closing quote, since pg_stat_activity truncates long statements, and
// a quoted value accepts both a doubled quote and a backslash escape,
// so an escaped quote never ends it early. Row images are masked as
// JSON text, where every backslash is doubled, so an escape backslash
// (escapeBackslash) may be one backslash or two: in SQL text that can
// mask more than the value, but never less. White space is the ASCII
// white space SQL itself accepts, spelt out so that PostgreSQL, whose
// [[:space:]] follows the database locale, and Go agree.
const (
	// ws is one ASCII white space character, and notWS starts a
	// bracket expression that excludes them.
	ws    = `[ \t\n\r\f\v]`
	notWS = `[^ \t\n\r\f\v`
	// escapeBackslash is the backslash that starts an escape, doubled
	// in JSON text.
	escapeBackslash = `\\\\?`
	// sqlQuoted is a single-quoted SQL string.
	sqlQuoted = `'(?:''|` + escapeBackslash + `.|[^'\\])*'?`
	// sqlDollarQuoted is a dollar-quoted SQL string, $$...$$ or
	// $tag$...$tag$. Without back-references the closing tag cannot be
	// matched to the opening one, so a tagged string ends at the first
	// $name$; a parameter such as $1 is not a dollar quote.
	sqlDollarQuoted = `\$\$(?:[^$]|\$[^$])*(?:\$\$)?` +
		`|\$[a-z_][a-z0-9_]*\$(?:[^$]|\$\$|\$[^a-z_$]|\$[a-z_][a-z0-9_]*(?:[^a-z0-9_$]|$))*(?:\$[a-z_][a-z0-9_]*\$)?`
	// sqlConstant is any SQL string constant: '...', E'...', N'...',
	// U&'...' or dollar-quoted.
	sqlConstant = `(?:(?:e|n|u&)?` + sqlQuoted + `|` + sqlDollarQuoted + `)`
	// sqlNestedQuoted is a quoted value inside a SQL literal, as in
	// CONNECTION 'password=''secret''', where its quotes are doubled
	// and an escaped quote is '''' or \''.
	sqlNestedQuoted = `''(?:''''|` + escapeBackslash + `''|[^'])*(?:'')?`
	// sqlBackslashQuoted is a quoted value inside an escape string, as
	// in CONNECTION E'password=\'secret\'', where its quotes are \', an
	// escaped quote is \\\', an escaped backslash is \\\\ (the
	// conninfo escape \\, escaped again for the escape string) and a
	// bare quote can only be doubled. A backslash followed by a quote
	// always ends the value or escapes the quote, so in JSON text, where
	// the closing \' is \\', the value cannot run on past it.
	sqlBackslashQuoted = escapeBackslash + `'(?:` +
		escapeBackslash + escapeBackslash + escapeBackslash + escapeBackslash + `|` +
		escapeBackslash + escapeBackslash + escapeBackslash + `'|` +
		escapeBackslash + escapeBackslash + `[^'\\]|` + escapeBackslash + `[^'\\]|''|[^'\\])*(?:` +
		escapeBackslash + `')?`
	// nameQuote is the double quote that may close a quoted name, as in
	// SET "app.password", escaped as \" in JSON text and as \\\" in
	// JSON text inside JSON text.
	nameQuote = `(?:(?:\\|\\\\\\)?")?`
	// doubleQuoted is a double-quoted value, as in PGPASSWORD="secret",
	// and jsonDoubleQuoted is one in JSON text, as in
	// PGPASSWORD=\"secret\" inside a row image.
	doubleQuoted     = `"(?:[^"\\]|\\.)*"?`
	jsonDoubleQuoted = `\\"(?:[^\\]|\\[^"])*(?:\\")?`
	// secretValue is a value that follows a secret name: any SQL string
	// constant or one nested in another literal.
	secretValue = `(?:` + sqlConstant + `|` + sqlNestedQuoted + `|` + sqlBackslashQuoted + `)`
	// blank is one white space character. In JSON text a line break
	// is \n, so an escaped \n, \r or \t, with one backslash or two,
	// counts as white space there.
	blank = `(?:` + ws + `|` + escapeBackslash + `[nrt])`
	// blockComment is a closed /* */ comment whose body is at most 32
	// characters, or runs of stars each followed by one character, and
	// lineComment a -- comment, which runs to the end of the line (\n
	// in JSON text). A longer or nested comment is not matched, so a
	// value behind one is not masked.
	blockComment = `/\*(?:[^*]|\*+[^*/]){0,32}\*+/`
	lineComment  = `--(?:[^\n\\]|\\[^n])*`
	// sep is white space with up to two comments, and sep1 is the
	// same but not empty. Requiring a block comment to be closed and
	// short, and bounding the number of comments, keeps a run of
	// comments from making a match scan to the end of the text at every
	// secret name, which took minutes on a megabyte of password=/* .
	sep  = blank + `*(?:(?:` + blockComment + `|` + lineComment + `)` + blank + `*){0,2}`
	sep1 = `(?:` + blank + `|` + blockComment + `|` + lineComment + `)` + sep
	// identChars may surround a secret name, as in app.password or
	// AWS_SECRET_ACCESS_KEY.
	identChars = `[a-z0-9_.]*`
)

// MaxTextLength is the length, in characters, to which Text and the
// server's RedactedText cut a value before masking it, so that the
// time a crafted value can take to mask is bounded (about a second at
// most in PostgreSQL). Text longer than this is returned cut short.
const MaxTextLength = 65536

// textRedactions are the usual places a password appears in SQL text,
// connection strings, shell commands and row data. They are applied in
// order, by Text and, as regexp_replace calls inside the server's scoped
// subquery, by RedactedText, so a caller's filters and aggregates see
// only the masked text. A secret name is any name holding a
// textSecretNamePatterns entry. Masking is best effort: a password in
// any other form is returned as stored.
var textRedactions = []TextRedaction{
	{
		// scheme://user:secret@host, including a truncated one that ends
		// before the @. A user name may hold a doubled quote, as in
		// 'postgresql://o''brien:secret@host', or a double quote.
		Name:        "URI userinfo",
		Pattern:     `([a-z][a-z0-9+.-]*://(?:` + notWS + `:/@']|'')*:)` + notWS + `@]+(?:@|$)`,
		Replacement: `\1` + RedactedValue + `@`,
	},
	{
		// "password": value in a JSON row image, whatever the value's
		// type, and the bare keys "pass" and "pwd"; a nested value is
		// masked to its first closing bracket.
		Name: "JSON key",
		Pattern: `((?:` + secretKeyPattern(`"[^"]*`, `[^"]*`) + `|"(?:pass|pwd)\\?)"` + ws + `*:` + ws + `*)` +
			`(?:"(?:[^"\\]|\\.)*"?|\{[^}]*\}?|\[[^\]]*\]?|` + notWS + `,}\]]+)`,
		Replacement: `\1"` + RedactedValue + `"`,
	},
	{
		// ALTER SYSTEM SET archive_command = '...' and the other
		// credentialSettings.
		Name: "SET credential setting",
		Pattern: `(set` + sep1 + nameQuote + `(?:` + strings.Join(credentialSettings, "|") + `)` + nameQuote +
			`(?:` + sep + `=|` + sep1 + `to)` + sep + `)` + sqlConstant,
		Replacement: `\1'` + RedactedValue + `'`,
	},
	{
		// set_config('app.password', 'secret', false), with or without a
		// cast on the name or the new_value => argument name, and
		// json_build_object('password', 'secret').
		Name: "quoted name, value",
		Pattern: `(` + secretKeyPattern(`'[^']*`, `[^']*`) + `'(?:::[a-z_ ]+)?` + sep + `,` + sep +
			`(?:new_value` + sep + `=>` + sep + `)?)` + sqlConstant,
		Replacement: `\1'` + RedactedValue + `'`,
	},
	{
		// password=secret in a libpq connection string, ldapbindpasswd=
		// and radiussecrets= in pg_hba.conf, PGPASSWORD=... and
		// AWS_SECRET_ACCESS_KEY=... in a shell command, and
		// SET app.password = '...'. A quoted value may follow white
		// space or up to two /* */ comments, which are masked with it.
		// An unquoted value is masked to the next white space that no
		// backslash escapes, as in password=correct\ horse, whatever it
		// starts with, so a value such as --secret is not taken for a
		// comment; a parameter such as $1 is kept when white space, a
		// comma, a bracket, a semicolon, a double quote or a backslash
		// ends it.
		Name: "keyword=value",
		Pattern: `(` + secretKeyPattern(identChars, identChars) + nameQuote + sep + `(?:=>?|:=)` + ws + `*)(?:` +
			`(?:(?:` + blockComment + `|` + escapeBackslash + `[nrt])` + blank + `*){0,2}` +
			`(?:` + secretValue + `|` + doubleQuoted + `|` + jsonDoubleQuoted + `)|` +
			`(?:` + blockComment + `)*(?:` + notWS + `$'"/]|/` + notWS + `*]|\$` + notWS + `0-9]|\$[0-9]+` + notWS + `0-9,);"\\])` +
			`(?:` + escapeBackslash + `[^\n\r]|` + notWS + `])*)`,
		Replacement: `\1` + RedactedValue,
	},
	{
		// EXECUTE 'ALTER ROLE app PASSWORD ' || '''secret''', where the
		// value is a quoted value in the next literal.
		Name: "concatenated PASSWORD",
		Pattern: `(` + secretKeyPattern(identChars, identChars) + nameQuote + sep + `'` + sep + `\|\|` + sep + `'` +
			ws + `*)` + sqlNestedQuoted,
		Replacement: `\1''` + RedactedValue + `''`,
	},
	{
		// [ENCRYPTED] PASSWORD '...' in CREATE or ALTER ROLE and USER,
		// password '...' in a user mapping's OPTIONS, and
		// SET app.password TO '...'. White space or a comment may
		// separate the name from the value, or nothing at all, as in
		// PASSWORD'...'. A name that starts a quoted string, as in
		// EXECUTE 'ALTER ROLE app ' || 'PASSWORD ''...''', needs a
		// separator after it, so a quoted name followed by a comma, as in
		// set_config('app.password', ...), is left to the quoted name
		// pattern. PASSWORD NULL and a $1 parameter are kept.
		Name: "PASSWORD 'value'",
		Pattern: `((?:(?:^|[^'a-z0-9_.])` + secretKeyPattern(identChars, identChars) + nameQuote + sep + `|` +
			secretKeyPattern(`'`+identChars, identChars) + nameQuote + sep1 + `)` +
			`(?:to` + sep + `)?)` + secretValue,
		Replacement: `\1'` + RedactedValue + `'`,
	},
}

// SecretColumnFragments returns a copy of secretColumnFragments.
func SecretColumnFragments() []string { return slices.Clone(secretColumnFragments) }

// TextSecretNamePatterns returns a copy of textSecretNamePatterns.
func TextSecretNamePatterns() []string { return slices.Clone(textSecretNamePatterns) }

// CredentialSettings returns a copy of credentialSettings.
func CredentialSettings() []string { return slices.Clone(credentialSettings) }

// TextRedactions returns a copy of textRedactions, in the order they
// are applied.
func TextRedactions() []TextRedaction { return slices.Clone(textRedactions) }

// compiledRedaction is a textRedactions entry compiled for Go.
type compiledRedaction struct {
	re          *regexp.Regexp
	replacement string
}

// compiledRedactions are textRedactions compiled case-insensitively,
// with . matching a newline as it does in PostgreSQL, and leftmost
// longest, so Go takes the same match PostgreSQL's greedy quantifiers
// take. Each Replacement's \1 becomes ${1}.
var compiledRedactions = func() []compiledRedaction {
	out := make([]compiledRedaction, len(textRedactions))
	for i, r := range textRedactions {
		re := regexp.MustCompile("(?is)" + r.Pattern)
		re.Longest()
		out[i] = compiledRedaction{re: re, replacement: strings.ReplaceAll(r.Replacement, `\1`, "${1}")}
	}
	return out
}()

// Text returns s with every textRedactions pattern masked, as the
// server's RedactedText does in SQL. It cannot catch every form: a
// password in any other form is returned as it is. Text longer than
// MaxTextLength characters is cut to that length first, as PostgreSQL's
// left() cuts it.
func Text(s string) string {
	s = truncate(s, MaxTextLength)
	for _, r := range compiledRedactions {
		s = r.re.ReplaceAllString(s, r.replacement)
	}
	return s
}

// maxSettlePasses bounds how many times Settled applies Text. Every
// redacttest case settles within two passes, and of 50,000 fuzzed
// inputs one needed three; TestSettled pins both.
const maxSettlePasses = 3

// Settled returns s masked with Text and then masked again until the
// text stops changing, for at most maxSettlePasses passes. Text is not
// idempotent: masking a masked value can mask further, for example the
// closing quote after a masked conninfo password
// (CONNECTION E'host=h password=********' loses its last quote). A
// caller that stores masked text and masks it again on a later pass
// uses Settled, so that the second pass finds nothing to rewrite.
func Settled(s string) string {
	s = Text(s)
	for i := 1; i < maxSettlePasses; i++ {
		next := Text(s)
		if next == s {
			break
		}
		s = next
	}
	return s
}

// truncate returns s cut to at most n characters (runes).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}
