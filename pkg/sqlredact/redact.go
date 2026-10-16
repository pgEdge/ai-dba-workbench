/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package sqlredact removes secrets from SQL statement text before it is
// stored or returned. PostgreSQL keeps the text of utility statements
// such as ALTER ROLE ... PASSWORD verbatim in pg_stat_activity and
// pg_stat_statements, so the collector redacts the text it collects and
// the server redacts it again as it leaves.
//
// The text is split into SQL tokens by a lexer that follows PostgreSQL's
// rules for string constants, dollar quoting, quoted identifiers and
// comments, so a secret is found by its place in the statement rather
// than by a pattern over raw text. Two rules apply:
//
//   - A string constant that follows a name suggesting a secret, such as
//     PASSWORD in CREATE ROLE, password or sslpassword in the OPTIONS of
//     a user mapping or server, a column or setting named like a secret,
//     or a setting such as restore_command whose value pkg/pgsettings
//     redacts in full, is replaced in full. An optional =, =>, := or TO
//     may sit between them. The same applies to a string constant that
//     follows such a name given as a string constant and a comma, as in
//     set_config('myext.password', '...', false). After a separator, a
//     double-quoted identifier is replaced too, and in a SET command an
//     unquoted value is.
//   - Any other string constant that mentions a password or is a
//     postgres:// URI is treated as a libpq connection string, as in
//     CREATE SUBSCRIPTION ... CONNECTION or dblink_connect, and has its
//     password and sslpassword values replaced, keeping the rest. A
//     truncated URI that ends before its host is replaced to the end of
//     the text, since the cut may fall inside the password.
//
// Where the meaning is uncertain the redaction errs towards removing
// more. Parameter placeholders such as $1, which pg_stat_statements
// leaves in place of constants, are never touched.
package sqlredact

import (
	"slices"
	"strings"

	"github.com/pgedge/ai-workbench/pkg/connstring"
	"github.com/pgedge/ai-workbench/pkg/pgsettings"
)

// Redacted replaces a secret. It matches the placeholder used for
// passwords elsewhere in the Workbench.
const Redacted = connstring.RedactedPassword

// redactedLiteral is the string constant that replaces a secret in full.
const redactedLiteral = "'" + Redacted + "'"

// secretNameFragments mark an identifier as naming a secret when they
// appear anywhere in it, ignoring case. password covers sslpassword and
// passwords stored in user columns; passwd and passphrase cover
// ssl_passphrase_command and FDW options such as MySQL's.
var secretNameFragments = []string{
	"password", "passwd", "passphrase", "secret", "token",
	"credential", "private_key",
}

// nonSecretNames contain a secretNameFragments entry but hold no secret,
// so their values stay readable: the core settings pkg/pgsettings lists
// as holding no credential, and postgres_fdw's password_required user
// mapping option, which is a boolean.
var nonSecretNames = append(pgsettings.NonCredentialSettings(), "password_required")

// wholeValueSettings are the core settings whose value pkg/pgsettings
// replaces in full, such as restore_command, which can carry an inline
// secret such as PGPASSWORD=... and has no structure to parse. They name
// a secret although no secretNameFragments entry matches them all.
var wholeValueSettings = func() []string {
	var names []string
	for _, name := range pgsettings.CredentialSettings() {
		if pgsettings.RedactsWholeValue(name) {
			names = append(names, name)
		}
	}
	return names
}()

// triggerFragments are lower-case substrings at least one of which every
// statement text that RedactQuery changes contains: each secret name
// fragment contains "pass" or appears here itself, each of the
// wholeValueSettings appears here, and a connection string is only
// redacted when it mentions "password" or starts with a URI prefix.
var triggerFragments = append([]string{
	"pass", "secret", "token", "credential", "private_key",
	"postgres://", "postgresql://",
}, wholeValueSettings...)

// MightContainSecret reports whether RedactQuery could change s. It is a
// cheap check that lets callers skip the lexer for most statements.
func MightContainSecret(s string) bool {
	lower := strings.ToLower(s)
	for _, f := range triggerFragments {
		if strings.Contains(lower, f) {
			return true
		}
	}
	return false
}

// CandidatePatterns returns ILIKE patterns that together match every
// text MightContainSecret accepts, and some more, since _ in a pattern
// matches any character. A migration uses them to read only the stored
// rows that might need redacting.
func CandidatePatterns() []string {
	patterns := make([]string, len(triggerFragments))
	for i, f := range triggerFragments {
		patterns[i] = "%" + f + "%"
	}
	return patterns
}

// RedactQuery returns query with the secrets described in the package
// comment replaced. Text with nothing to redact, including the empty
// string, is returned unchanged, and the function is idempotent.
func RedactQuery(query string) string {
	if !MightContainSecret(query) {
		return query
	}
	toks := lex(query)

	// sig holds the indexes of the tokens that are not whitespace or
	// comments, which is how PostgreSQL's grammar sees the statement.
	var sig []int
	for i, t := range toks {
		if t.kind != tokSpace && t.kind != tokComment {
			sig = append(sig, i)
		}
	}

	var out strings.Builder
	last := 0
	emit := func(start, end int, repl string) {
		out.WriteString(query[last:start])
		out.WriteString(repl)
		last = end
	}

	// redactValueAt replaces the string constant at sig[m], with any
	// constants continuing it, and returns the index of the last one and
	// whether the replacement ran to the end of the text.
	redactValueAt := func(m int) (int, bool) {
		first := toks[sig[m]]
		// A string constant continued over whitespace is one value to
		// PostgreSQL, so every part goes.
		for m+1 < len(sig) && toks[sig[m+1]].isLiteral() {
			m++
		}
		end := toks[sig[m]].end
		if first.ambiguousEscapes(query) {
			end = len(query)
		}
		emit(first.start, end, redactedLiteral)
		return m, end == len(query)
	}

	for k := 0; k < len(sig); k++ {
		t := toks[sig[k]]
		if t.isName() && isSecretName(t.name(query)) {
			m := k + 1
			separated := m < len(sig) && toks[sig[m]].isSeparator(query)
			if separated {
				m++
			}
			if m >= len(sig) {
				continue
			}
			v := toks[sig[m]]
			switch {
			case v.isLiteral():
				last, toEnd := redactValueAt(m)
				if toEnd {
					k = len(sig)
					continue
				}
				k = last
			case separated && v.kind == tokQuotedIdent:
				emit(v.start, v.end, `"`+Redacted+`"`)
				k = m
			case separated && isSetTarget(query, toks, sig, k):
				if stop := setValueEnd(query, toks, sig, m); stop > m {
					emit(v.start, toks[sig[stop-1]].end, redactedLiteral)
					k = stop - 1
				}
			}
			continue
		}
		if !t.isLiteral() {
			continue
		}
		if name, ok := t.constantName(query); ok {
			// A secret's name given as a constant, as set_config's
			// first argument, is kept and the value after it goes. A
			// name that holds no secret is kept as well; any other
			// constant that mentions a password, even one shaped like
			// a name, is left to the connection-string rule below.
			if m := k + 2; m < len(sig) && isSecretName(name) &&
				toks[sig[k+1]].is(query, tokOther, ",") && toks[sig[m]].isLiteral() {
				last, toEnd := redactValueAt(m)
				if toEnd {
					break
				}
				k = last
				continue
			}
			if slices.Contains(nonSecretNames, strings.ToLower(name)) {
				continue
			}
		}
		repl, toEnd, changed := redactConnString(query, t)
		if !changed {
			continue
		}
		if toEnd {
			emit(t.start, len(query), repl)
			break
		}
		emit(t.start, t.end, repl)
	}

	if last == 0 {
		return query
	}
	out.WriteString(query[last:])
	return out.String()
}

// isSecretName reports whether an identifier names a secret.
func isSecretName(name string) bool {
	name = strings.ToLower(name)
	if slices.Contains(nonSecretNames, name) {
		return false
	}
	if slices.Contains(wholeValueSettings, name) {
		return true
	}
	for _, f := range secretNameFragments {
		if strings.Contains(name, f) {
			return true
		}
	}
	return false
}

// isSetTarget reports whether the name at sig[k] is the last part of the
// setting a SET command assigns, as in SET myext.api_token = ..., SET
// LOCAL ... or ALTER ROLE r SET ....
func isSetTarget(query string, toks []token, sig []int, k int) bool {
	j := k
	for j >= 2 && toks[sig[j-1]].is(query, tokOther, ".") && toks[sig[j-2]].isName() {
		j -= 2
	}
	if j >= 1 && (toks[sig[j-1]].is(query, tokIdent, "local") ||
		toks[sig[j-1]].is(query, tokIdent, "session")) {
		j--
	}
	return j >= 1 && toks[sig[j-1]].is(query, tokIdent, "set")
}

// setValueEnd returns the index in sig just past the value of a SET
// command that starts at sig[m], which runs to the next semicolon or the
// end of the text. It returns m when the value is DEFAULT or a parameter
// placeholder, which hold no secret.
func setValueEnd(query string, toks []token, sig []int, m int) int {
	stop := m
	for stop < len(sig) && !toks[sig[stop]].is(query, tokOther, ";") {
		stop++
	}
	if stop == m+1 {
		v := toks[sig[m]]
		text := query[v.start:v.end]
		if v.is(query, tokIdent, "default") || len(text) > 1 && text[0] == '$' && isDigit(text[1]) {
			return m
		}
	}
	return stop
}

// mentionsConnSecret reports whether a string constant's raw text might
// be a connection string holding a password.
func mentionsConnSecret(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "password") ||
		strings.Contains(lower, "postgres://") ||
		strings.Contains(lower, "postgresql://")
}

// redactConnString redacts a string constant that may hold a libpq
// connection string. It returns the replacement, whether the replacement
// runs to the end of the text, and whether anything changed.
func redactConnString(query string, t token) (repl string, toEnd, changed bool) {
	body := query[t.bodyStart:t.bodyEnd]
	if !mentionsConnSecret(body) {
		return "", false, false
	}
	if !t.terminated && truncatedInURIHost(body) {
		return redactedLiteral, true, true
	}
	switch {
	case t.kind == tokDollar:
		red := connstring.RedactPasswords(body)
		if red == body {
			return "", false, false
		}
		// The body cannot contain the tag, and redaction only replaces
		// values with asterisks and requotes them, which adds no dollar
		// sign, so the redacted body can go back inside the same tag.
		closing := ""
		if t.terminated {
			closing = t.tag
		}
		return t.tag + red + closing, false, true
	case t.escapes || t.unicode:
		// Decoding escape sequences is not worth the risk of getting one
		// wrong; a constant that mentions a password goes in full.
		return redactedLiteral, false, true
	case t.ambiguousEscapes(query):
		return redactedLiteral, true, true
	}
	value := strings.ReplaceAll(body, "''", "'")
	red := connstring.RedactPasswords(value)
	if red == value {
		return "", false, false
	}
	closing := ""
	if t.terminated {
		closing = "'"
	}
	return query[t.start:t.bodyStart] + strings.ReplaceAll(red, "'", "''") + closing, false, true
}

// truncatedInURIHost reports whether body, the text of an unterminated
// constant, holds a URI cut off before the end of its user information
// or host. libpq takes everything before an @ to be user information,
// so the cut may have removed the @ that would mark user:password; the
// text after the scheme is safe only once a / shows the host has ended.
func truncatedInURIHost(body string) bool {
	// Only ASCII letters are folded, so that byte offsets in lower match
	// those in body; strings.ToLower can change the length of invalid
	// UTF-8.
	lower := []byte(body)
	for i, c := range lower {
		if c >= 'A' && c <= 'Z' {
			lower[i] = c + 'a' - 'A'
		}
	}
	for _, prefix := range []string{"postgresql://", "postgres://"} {
		if i := strings.Index(string(lower), prefix); i >= 0 {
			return !strings.ContainsAny(body[i+len(prefix):], "@/")
		}
	}
	return false
}

type tokenKind int

const (
	tokSpace tokenKind = iota
	tokComment
	tokIdent       // unquoted identifier or keyword
	tokQuotedIdent // "identifier"
	tokString      // '...', with an optional E, U&, N, B or X prefix
	tokDollar      // $tag$...$tag$
	tokOp          // a run of = < > : ! characters
	tokOther       // anything else, one byte or a $n parameter
)

// token is one lexical element of a statement, spanning
// query[start:end]. For string constants, dollar-quoted strings and
// quoted identifiers, query[bodyStart:bodyEnd] is the raw text between
// the delimiters.
type token struct {
	kind               tokenKind
	start, end         int
	bodyStart, bodyEnd int
	escapes            bool   // E'...': backslash escapes apply
	unicode            bool   // U&'...'
	terminated         bool   // the closing delimiter is present
	tag                string // the dollar-quote delimiter, such as $x$
}

func (t token) isLiteral() bool {
	return t.kind == tokString || t.kind == tokDollar
}

// is reports whether the token has the given kind and text, ignoring
// case.
func (t token) is(query string, kind tokenKind, text string) bool {
	return t.kind == kind && strings.EqualFold(query[t.start:t.end], text)
}

// constantName returns the text of a plain string constant that holds
// nothing but a name, possibly dotted, such as 'myext.password' as the
// first argument of set_config, and whether the constant is one.
func (t token) constantName(query string) (string, bool) {
	if t.kind != tokString || t.start != t.bodyStart-1 || !t.terminated {
		return "", false
	}
	body := query[t.bodyStart:t.bodyEnd]
	for i := 0; i < len(body); i++ {
		if !isIdentCont(body[i]) && body[i] != '.' {
			return "", false
		}
	}
	return body, body != ""
}

func (t token) isName() bool {
	return t.kind == tokIdent || t.kind == tokQuotedIdent
}

// name returns the identifier a name token denotes.
func (t token) name(query string) string {
	if t.kind == tokQuotedIdent {
		return strings.ReplaceAll(query[t.bodyStart:t.bodyEnd], `""`, `"`)
	}
	return query[t.start:t.end]
}

// isSeparator reports whether the token may sit between a secret's name
// and its value: = in a SET or a comparison, => or := in a named
// function argument, or TO in a SET.
func (t token) isSeparator(query string) bool {
	text := query[t.start:t.end]
	switch t.kind {
	case tokOp:
		return text == "=" || text == "=>" || text == ":="
	case tokIdent:
		return strings.EqualFold(text, "to")
	default:
		return false
	}
}

// ambiguousEscapes reports whether a plain string constant contains a
// backslash. With standard_conforming_strings off, a backslash escapes
// the next character, so the constant could end somewhere other than
// where the lexer, which assumes the default of on, ended it.
func (t token) ambiguousEscapes(query string) bool {
	return t.kind == tokString && !t.escapes && !t.unicode &&
		strings.Contains(query[t.bodyStart:t.bodyEnd], `\`)
}

// lex splits query into tokens covering all of it. An unterminated
// string, quoted identifier, dollar quote or comment runs to the end of
// the text, which is how a statement truncated by
// track_activity_query_size ends.
func lex(query string) []token {
	var toks []token
	n := len(query)
	i := 0
	for i < n {
		c := query[i]
		var t token
		switch {
		case isSpace(c):
			j := i + 1
			for j < n && isSpace(query[j]) {
				j++
			}
			t = token{kind: tokSpace, start: i, end: j}
		case c == '-' && i+1 < n && query[i+1] == '-':
			j := strings.IndexByte(query[i:], '\n')
			if j < 0 {
				j = n
			} else {
				j += i
			}
			t = token{kind: tokComment, start: i, end: j}
		case c == '/' && i+1 < n && query[i+1] == '*':
			t = lexBlockComment(query, i)
		case c == '\'':
			t = lexString(query, i, i, false)
		case c == '"':
			t = lexQuotedIdent(query, i)
		case c == '$':
			t = lexDollar(query, i)
		case isIdentStart(c):
			t = lexIdentOrPrefixedString(query, i)
		case isOpChar(c):
			j := i + 1
			for j < n && isOpChar(query[j]) {
				j++
			}
			t = token{kind: tokOp, start: i, end: j}
		default:
			t = token{kind: tokOther, start: i, end: i + 1}
		}
		toks = append(toks, t)
		i = t.end
	}
	return toks
}

// lexBlockComment reads a /* */ comment, which nests in PostgreSQL.
func lexBlockComment(query string, start int) token {
	n := len(query)
	depth := 0
	i := start
	for i < n {
		switch {
		case i+1 < n && query[i] == '/' && query[i+1] == '*':
			depth++
			i += 2
		case i+1 < n && query[i] == '*' && query[i+1] == '/':
			depth--
			i += 2
			if depth == 0 {
				return token{kind: tokComment, start: start, end: i}
			}
		default:
			i++
		}
	}
	return token{kind: tokComment, start: start, end: n}
}

// lexString reads a string constant whose opening quote is at quote and
// whose prefix, if any, starts at start. A doubled quote stands for one
// quote; with escapes, a backslash also escapes the next byte.
func lexString(query string, start, quote int, escapes bool) token {
	n := len(query)
	t := token{kind: tokString, start: start, bodyStart: quote + 1, escapes: escapes}
	i := quote + 1
	for i < n {
		switch {
		case query[i] == '\'' && i+1 < n && query[i+1] == '\'':
			i += 2
		case query[i] == '\'':
			t.bodyEnd, t.end, t.terminated = i, i+1, true
			return t
		case escapes && query[i] == '\\':
			i += 2
		default:
			i++
		}
	}
	t.bodyEnd, t.end = n, n
	return t
}

// lexQuotedIdent reads a double-quoted identifier, in which a doubled
// quote stands for one quote.
func lexQuotedIdent(query string, start int) token {
	n := len(query)
	t := token{kind: tokQuotedIdent, start: start, bodyStart: start + 1}
	i := start + 1
	for i < n {
		if query[i] == '"' {
			if i+1 < n && query[i+1] == '"' {
				i += 2
				continue
			}
			t.bodyEnd, t.end, t.terminated = i, i+1, true
			return t
		}
		i++
	}
	t.bodyEnd, t.end = n, n
	return t
}

// lexDollar reads a $n parameter, a dollar-quoted string, or a lone $.
func lexDollar(query string, start int) token {
	n := len(query)
	j := start + 1
	if j < n && isDigit(query[j]) {
		for j < n && isDigit(query[j]) {
			j++
		}
		return token{kind: tokOther, start: start, end: j}
	}
	if j < n && isIdentStart(query[j]) {
		j++
		for j < n && isIdentStart(query[j]) || j < n && isDigit(query[j]) {
			j++
		}
	}
	if j >= n || query[j] != '$' {
		return token{kind: tokOther, start: start, end: start + 1}
	}
	tag := query[start : j+1]
	t := token{kind: tokDollar, start: start, bodyStart: j + 1, tag: tag}
	if k := strings.Index(query[j+1:], tag); k >= 0 {
		t.bodyEnd = j + 1 + k
		t.end = t.bodyEnd + len(tag)
		t.terminated = true
		return t
	}
	t.bodyEnd, t.end = n, n
	return t
}

// lexIdentOrPrefixedString reads an identifier, or a string constant
// when the identifier is a prefix such as E, B, X or N directly followed
// by a quote, or U followed by &'.
func lexIdentOrPrefixedString(query string, start int) token {
	n := len(query)
	j := start + 1
	for j < n && isIdentCont(query[j]) {
		j++
	}
	word := strings.ToLower(query[start:j])
	if j < n && query[j] == '\'' {
		switch word {
		case "e":
			return lexString(query, start, j, true)
		case "b", "x", "n":
			return lexString(query, start, j, false)
		}
	}
	if word == "u" && j+1 < n && query[j] == '&' && query[j+1] == '\'' {
		t := lexString(query, start, j+1, false)
		t.unicode = true
		return t
	}
	return token{kind: tokIdent, start: start, end: j}
}

func isSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 0x80
}

func isIdentCont(c byte) bool {
	return isIdentStart(c) || isDigit(c) || c == '$'
}

func isOpChar(c byte) bool {
	switch c {
	case '=', '<', '>', ':', '!':
		return true
	default:
		return false
	}
}
