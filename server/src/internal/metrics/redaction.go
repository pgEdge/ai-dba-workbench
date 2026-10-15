/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package metrics

import (
	"slices"
	"strings"
)

// This file holds the rules that keep credentials out of collected
// metrics on every route that returns them: query_datastore and
// GET /api/v1/metrics/latest. A column whose name marks it as a secret
// is never returned, and the values in ProbeRedactions are masked in
// the SQL that reads them, so a filter or sort sees only the masked
// value.

// secretColumnFragments are name fragments that mark a column as
// holding a credential. A metric column whose name contains one is never
// offered, so a future probe cannot expose a secret by accident. The
// connection string fragments cover pg_stat_wal_receiver.conninfo and
// the like: PostgreSQL obscures the password in some of them, but the
// rest of a connection string is still credential material.
var secretColumnFragments = []string{
	"password", "passwd", "passphrase", "secret", "token", "credential", "private_key",
	"conninfo", "connstr", "connection_string", "dsn",
	"sslcert", "sslkey", "sslrootcert",
}

// secretNamePatterns are the regular expressions, matched against a
// lower-cased setting or option name, that mark its value as possibly
// holding a credential: every secretColumnFragments entry, plus
// patterns too broad for column names. Masking a harmless value is
// acceptable; exposing a credential is not.
var secretNamePatterns = append(slices.Clone(secretColumnFragments),
	"pass$", "key$", "api_?key", "access_key", "salt")

// textSecretNamePatterns are the names whose value is masked in free
// text: the secretColumnFragments, which are specific enough not to
// catch ordinary identifiers such as sort_key or bypass, plus the API
// and access key names.
var textSecretNamePatterns = append(slices.Clone(secretColumnFragments), "api_?key", "access_key")

// nonCredentialSettings match secretNamePatterns but hold no secret, so
// pg_settings shows their values.
var nonCredentialSettings = []string{
	"password_encryption",
	"md5_password_warnings",
	"ssl_passphrase_command_supports_reload",
}

// IsSecretColumnName reports whether a column name looks like it holds
// a credential or other secret.
func IsSecretColumnName(name string) bool {
	lower := strings.ToLower(name)
	for _, fragment := range secretColumnFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

// ProbeRedaction says which columns of a probe are masked, as described
// at ProbeRedactions.
type ProbeRedaction struct {
	KeyColumn     string
	Columns       []string
	OptionColumns []string
	TextColumns   []string
	WholeColumns  []string
	Note          string
}

// ProbeRedactions lists, per probe, the values that can carry a
// credential no column-name check can catch, so they are masked:
// Columns hold a setting's value and KeyColumn names the setting (a
// credential-bearing setting such as primary_conninfo can hold a
// password), OptionColumns are text[] columns of name=value options
// (pg_hba.conf's ldapbindpasswd and radiussecrets hold one in clear
// text), and TextColumns hold free text, such as SQL statements and
// replicated row images, that can quote a password. WholeColumns can
// quote a credential in a form no pattern catches, as
// pg_hba_file_rules.error quotes an option it cannot parse, so a
// non-empty value is masked entirely. Note is added to the probe's
// description.
var ProbeRedactions = map[string]ProbeRedaction{
	"pg_settings": {
		KeyColumn: "name", Columns: []string{"setting", "reset_val", "boot_val"},
		Note: SettingRedactionNote,
	},
	"pg_hba_file_rules": {
		OptionColumns: []string{"options"}, WholeColumns: []string{"error"},
		Note: OptionsRedactionNote,
	},
	"pg_stat_activity":   {TextColumns: []string{"query"}, Note: TextRedactionNote},
	"pg_stat_statements": {TextColumns: []string{"query"}, Note: TextRedactionNote},
	"spock_exception_log": {
		TextColumns: []string{"ddl_statement", "error_message", "local_tup", "remote_old_tup", "remote_new_tup"},
		Note:        TextRedactionNote + " Row images are returned as text.",
	},
	"spock_resolutions": {TextColumns: []string{"local_tuple", "remote_tuple"}, Note: TextRedactionNote},
}

// credentialSettings are setting names whose value can carry a
// credential: primary_conninfo is a libpq connection string that may
// hold password=, and the rest are shell commands that may carry a
// passphrase or an inline secret such as PGPASSWORD=... or
// AWS_SECRET_ACCESS_KEY=.... Any setting whose name contains a
// secretNamePatterns entry is masked as well, which covers an extension
// setting such as myext.password.
var credentialSettings = []string{
	"primary_conninfo",
	"ssl_passphrase_command",
	"archive_command",
	"archive_cleanup_command",
	"restore_command",
	"recovery_end_command",
}

// RedactedValue replaces a masked value, as PostgreSQL masks a password
// in pg_stat_wal_receiver.conninfo.
const RedactedValue = "********"

// SettingRedactionNote, OptionsRedactionNote and TextRedactionNote are
// appended to the description of a probe in ProbeRedactions, so
// describe_datastore says which values are masked.
const (
	SettingRedactionNote = "Values of settings that can hold a credential, such as primary_conninfo, are shown as " +
		RedactedValue + "."
	OptionsRedactionNote = "Values of options that can hold a credential, such as ldapbindpasswd and " +
		"radiussecrets, are shown as " + RedactedValue + ", and so is a non-empty error, which can quote an " +
		"option."
	TextRedactionNote = "Passwords in known forms in SQL text and row data, such as PASSWORD '...', " +
		"password=... in a connection string and the value of a JSON key naming a password or secret, " +
		"are shown as " + RedactedValue + "; other sensitive text, including passwords in other forms, " +
		"is returned as stored."
)

// textRedaction is one password form RedactedText masks: Pattern is a
// case-insensitive regular expression in the subset PostgreSQL's
// advanced regular expressions share with Go's RE2, so the unit tests
// can check it in Go, and Replacement keeps capture group 1 (\1), the
// only capturing group, and puts RedactedValue in place of the secret.
// Every quantifier is greedy, so PostgreSQL takes the longest match, as
// Go does in the tests.
type textRedaction struct {
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
// so an escaped quote never ends it early.
const (
	// sqlQuoted is a single-quoted SQL string.
	sqlQuoted = `'(?:''|\\.|[^'\\])*'?`
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
	sqlNestedQuoted = `''(?:''''|\\''|[^'])*(?:'')?`
	// sqlBackslashQuoted is a quoted value inside an escape string, as
	// in CONNECTION E'password=\'secret\'', where its quotes are \' and
	// an escaped quote is \\\'.
	sqlBackslashQuoted = `\\'(?:\\\\\\'|[^\\]|\\[^'])*(?:\\')?`
	// secretValue is a value that follows a secret name: any SQL string
	// constant or one nested in another literal.
	secretValue = `(?:` + sqlConstant + `|` + sqlNestedQuoted + `|` + sqlBackslashQuoted + `)`
	// sqlSeparator is white space or a comment between SQL tokens.
	sqlSeparator = `(?:[[:space:]]|/\*(?:[^*]|\*+[^*/])*\*+/)`
	// identChars may surround a secret name, as in app.password or
	// AWS_SECRET_ACCESS_KEY.
	identChars = `[a-z0-9_.]*`
)

// textRedactions are the usual places a password appears in SQL text,
// connection strings, shell commands and row data. They are applied in
// order, as regexp_replace calls inside the scoped subquery, so a
// caller's filters and aggregates see only the masked text. A secret
// name is any name holding a secretNamePatterns entry. They cannot
// catch every form: a password in any other form is returned as stored.
var textRedactions = []textRedaction{
	{
		// scheme://user:secret@host, including a truncated one that ends
		// before the @.
		Name:        "URI userinfo",
		Pattern:     `([a-z][a-z0-9+.-]*://[^:/@[:space:]'"]*:)[^@[:space:]]+(?:@|$)`,
		Replacement: `\1` + RedactedValue + `@`,
	},
	{
		// "password": value in a JSON row image, whatever the value's
		// type; a nested value is masked to its first closing bracket.
		Name: "JSON key",
		Pattern: `(` + secretKeyPattern(`"[^"]*`, `[^"]*`) + `"[[:space:]]*:[[:space:]]*)` +
			`(?:"(?:[^"\\]|\\.)*"?|\{[^}]*\}?|\[[^\]]*\]?|[^,}\][:space:]]+)`,
		Replacement: `\1"` + RedactedValue + `"`,
	},
	{
		// ALTER SYSTEM SET archive_command = '...' and the other
		// credentialSettings.
		Name: "SET credential setting",
		Pattern: `(set[[:space:]]+"?(?:` + strings.Join(credentialSettings, "|") + `)"?` +
			`(?:[[:space:]]*=|[[:space:]]+to)[[:space:]]*)` + sqlConstant,
		Replacement: `\1'` + RedactedValue + `'`,
	},
	{
		// set_config('app.password', 'secret', false) and
		// json_build_object('password', 'secret').
		Name:        "quoted name, value",
		Pattern:     `(` + secretKeyPattern(`'[^']*`, `[^']*`) + `'[[:space:]]*,[[:space:]]*)` + sqlConstant,
		Replacement: `\1'` + RedactedValue + `'`,
	},
	{
		// password=secret in a libpq connection string, ldapbindpasswd=
		// and radiussecrets= in pg_hba.conf, PGPASSWORD=... and
		// AWS_SECRET_ACCESS_KEY=... in a shell command, and
		// SET app.password = '...'. An unquoted value is masked to the
		// next white space; a parameter such as $1 is kept when white
		// space, a comma, a bracket or a semicolon ends it.
		Name: "keyword=value",
		Pattern: `(` + secretKeyPattern(identChars, identChars) + `"?[[:space:]]*(?:=>?|:=)[[:space:]]*)` +
			`(?:` + secretValue + `|"(?:[^"\\]|\\.)*"?|` +
			`(?:[^[:space:]$'"]|\$[^0-9[:space:]]|\$[0-9]+[^0-9[:space:],);])[^[:space:]]*)`,
		Replacement: `\1` + RedactedValue,
	},
	{
		// [ENCRYPTED] PASSWORD '...' in CREATE or ALTER ROLE and USER,
		// password '...' in a user mapping's OPTIONS, and
		// SET app.password TO '...'. White space or a comment may
		// separate the name from the value, or nothing at all, as in
		// PASSWORD'...'; a name inside a quoted string, as in
		// set_config('app.password', ...), is left to the quoted name
		// pattern. PASSWORD NULL and a $1 parameter are kept.
		Name: "PASSWORD 'value'",
		Pattern: `((?:^|[^'a-z0-9_.])` + secretKeyPattern(identChars, identChars) + `"?` + sqlSeparator +
			`*(?:to` + sqlSeparator + `+)?)` + secretValue,
		Replacement: `\1'` + RedactedValue + `'`,
	},
}

// sqlStringLiteral quotes a constant as an escape string literal,
// doubling quotes and backslashes, so it means the same whatever
// standard_conforming_strings is set to. It is used only for the
// constants in textRedactions, never caller input.
func sqlStringLiteral(v string) string {
	return "E'" + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), "'", "''") + "'"
}

// RedactedText returns value, cast to text, with every textRedactions
// pattern masked. A NULL value stays NULL.
func RedactedText(value string) string {
	out := value + "::text"
	for _, r := range textRedactions {
		out = "regexp_replace(" + out + ", " + sqlStringLiteral(r.Pattern) + ", " +
			sqlStringLiteral(r.Replacement) + ", 'gi')"
	}
	return out
}

// credentialSettingCondition is true for a row whose setting name, the
// column reference name, is a credentialSettings entry or contains a
// secretNamePatterns entry. Every literal is a constant above, checked
// by TestCredentialSettingLiteralsAreSafe, so nothing from the caller
// reaches it.
func credentialSettingCondition(name string) string {
	lhs := "lower(" + name + ")"
	quoted := make([]string, len(credentialSettings))
	for i, v := range credentialSettings {
		quoted[i] = "'" + v + "'"
	}
	exempt := make([]string, len(nonCredentialSettings))
	for i, v := range nonCredentialSettings {
		exempt[i] = "'" + v + "'"
	}
	return "(" + lhs + " IN (" + strings.Join(quoted, ", ") + ") OR (" +
		secretNameMatch(lhs) + " AND " + lhs + " NOT IN (" + strings.Join(exempt, ", ") + ")))"
}

// secretNameMatch is true when the lower-cased name lhs matches a
// secretNamePatterns entry.
func secretNameMatch(lhs string) string {
	return lhs + " ~ '(" + strings.Join(secretNamePatterns, "|") + ")'"
}

// MaskedOptions returns value, a text[] of name=value options, with the
// value of every option whose name matches secretNamePatterns replaced
// by RedactedValue, keeping the options' order. A NULL array stays NULL.
func MaskedOptions(value string) string {
	const opt = "o.opt"
	name := "split_part(" + opt + ", '=', 1)"
	return "CASE WHEN " + value + " IS NULL THEN NULL ELSE ARRAY(SELECT CASE WHEN strpos(" + opt +
		", '=') > 0 AND " + secretNameMatch("lower("+name+")") + " THEN " + name + " || '=" +
		RedactedValue + "' ELSE " + opt + " END FROM unnest(" + value +
		") WITH ORDINALITY AS o(opt, n) ORDER BY o.n) END"
}

// MaskedSetting returns value with RedactedValue in place of a non-empty
// value on a row whose setting name, the column reference name, can
// carry a credential. An empty or NULL value is kept, since it shows
// only that the setting is unset.
func MaskedSetting(value, name string) string {
	return "CASE WHEN " + credentialSettingCondition(name) + " AND " + value + " <> '' THEN '" +
		RedactedValue + "' ELSE " + value + " END"
}

// ColumnMask is how a probe column is masked before it is returned.
type ColumnMask int

const (
	// MaskNone returns the column as stored.
	MaskNone ColumnMask = iota
	// MaskOmit never returns the column.
	MaskOmit
	// MaskSetting masks a setting's value (MaskedSetting).
	MaskSetting
	// MaskOptions masks the secret options in a name=value array
	// (MaskedOptions).
	MaskOptions
	// MaskText masks known password forms in free text, returned as
	// text (RedactedText).
	MaskText
	// MaskWhole masks a non-empty value entirely (MaskedWhole).
	MaskWhole
)

// MaskedWhole returns value with RedactedValue in place of a non-empty
// value. An empty or NULL value is kept.
func MaskedWhole(value string) string {
	return "CASE WHEN " + value + " <> '' THEN '" + RedactedValue + "' ELSE " + value + " END"
}

// ProbeColumnMask returns how column of probe is masked, given all the
// probe's columns. A column whose name marks it as a secret is omitted,
// and so is a setting's value when the probe has no column naming the
// setting, since it cannot then be masked row by row.
func ProbeColumnMask(probe, column string, columns []string) ColumnMask {
	if IsSecretColumnName(column) {
		return MaskOmit
	}
	rule, ok := ProbeRedactions[probe]
	switch {
	case !ok:
		return MaskNone
	case slices.Contains(rule.OptionColumns, column):
		return MaskOptions
	case slices.Contains(rule.TextColumns, column):
		return MaskText
	case slices.Contains(rule.WholeColumns, column):
		return MaskWhole
	case slices.Contains(rule.Columns, column):
		if !slices.Contains(columns, rule.KeyColumn) {
			return MaskOmit
		}
		return MaskSetting
	}
	return MaskNone
}

// ReturnableColumns returns the columns of probe that may be returned,
// leaving out those ProbeColumnMask omits. It does not change columns,
// which may be a cached slice.
func ReturnableColumns(probe string, columns []string) []string {
	out := make([]string, 0, len(columns))
	for _, col := range columns {
		if ProbeColumnMask(probe, col, columns) != MaskOmit {
			out = append(out, col)
		}
	}
	return out
}

// MaskedProbeColumn returns the expression that selects column of probe
// with any credential masked, given all the probe's columns and ref,
// which returns the SQL reference to a column. It returns false when
// the column must not be returned at all.
func MaskedProbeColumn(probe, column string, columns []string, ref func(string) string) (string, bool) {
	value := ref(column)
	switch ProbeColumnMask(probe, column, columns) {
	case MaskOmit:
		return "", false
	case MaskSetting:
		return MaskedSetting(value, ref(ProbeRedactions[probe].KeyColumn)), true
	case MaskOptions:
		return MaskedOptions(value), true
	case MaskText:
		return RedactedText(value), true
	case MaskWhole:
		return MaskedWhole(value), true
	}
	return value, true
}
