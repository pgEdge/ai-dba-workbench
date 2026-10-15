/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package sqllex holds the pieces of PostgreSQL's lexer that the
// server's SQL classifiers share: finding where a quoted literal,
// quoted identifier, dollar-quoted string or comment ends, so that text
// inside one is never read as code, and finding calls to functions that
// change server, session or database state (see
// CallsSideEffectingFunction). The query endpoint in package api and
// the MCP tools in package tools both classify statements with it, so a
// statement reads the same to both.
package sqllex

import "strings"

// ScanDollarTag checks whether sql[i] starts a dollar-quote tag. If a
// valid tag is found (either $$ or $tag$), it returns the full tag
// string. Otherwise it returns an empty string. The tag follows
// PostgreSQL's lexer: it starts with a letter, an underscore or any byte
// from 0x80 up, and continues with those or a digit, so $é$ is a tag
// just as $q$ is. Missing one would leave a quote or comment marker
// inside it to hide the code that follows from the classifier.
//
// A dollar sign that continues an identifier, as in foo$bar$ or é$bar$
// (PostgreSQL allows $ after the first character of an unquoted
// identifier), is part of that identifier rather than the start of a
// dollar quote, so it yields no tag. Reading one there would let a later
// $bar$ hide the code in between from the classifier.
func ScanDollarTag(sql string, i int) string {
	if i >= len(sql) || sql[i] != '$' {
		return ""
	}
	if ContinuesIdentifier(sql, i) {
		return ""
	}
	// Check for $$ (empty tag)
	if i+1 < len(sql) && sql[i+1] == '$' {
		return "$$"
	}
	// Check for $tag$, where the tag is an identifier that does not
	// start with a digit.
	j := i + 1
	if j >= len(sql) || !IsIdentChar(sql[j]) ||
		(sql[j] >= '0' && sql[j] <= '9') {
		return ""
	}
	for j++; j < len(sql); j++ {
		if sql[j] == '$' {
			return sql[i : j+1]
		}
		if !IsIdentChar(sql[j]) {
			return ""
		}
	}
	return ""
}

// StringReading is how PostgreSQL's lexer reads a plain '...' literal,
// which depends on standard_conforming_strings when the statement is
// lexed (xqstart in scan.l).
type StringReading int

const (
	// StandardStrings is standard_conforming_strings on, the default: a
	// backslash in '...' is an ordinary character.
	StandardStrings StringReading = iota
	// EscapeStrings is standard_conforming_strings off: '...' is read as
	// an E'...' escape string, so \' is an escaped quote.
	EscapeStrings
)

// StringReadings lists every reading the classifier must agree with.
var StringReadings = []StringReading{StandardStrings, EscapeStrings}

// literalKind is the kind of quoted text a quote or literal prefix
// opens, which decides how the lexer finds its end.
type literalKind int

const (
	// plainLiteral is '...' and N'...': PostgreSQL returns the N of the
	// latter as a keyword and lexes the rest as a plain literal, so its
	// escapes depend on the StringReading.
	plainLiteral literalKind = iota
	// escapeLiteral is E'...', where a backslash always escapes the
	// next character.
	escapeLiteral
	// unicodeLiteral is U&'...': a doubled quote is a quote and a
	// backslash starts a Unicode escape that cannot contain one.
	// PostgreSQL rejects it outright when standard_conforming_strings is
	// off, so its reading there does not matter.
	unicodeLiteral
	// bitLiteral is B'...' or X'...', which has no escapes at all:
	// the next quote ends it, even when another quote follows.
	bitLiteral
	// quotedIdentifier is "..." or U&"...": a doubled double quote is
	// part of the name, and it never continues onto another line.
	quotedIdentifier
)

// literalPrefix reports the length and kind of the string-literal
// prefix at s[i]. PostgreSQL accepts E'...' (backslash escapes), U&'...'
// and U&"..." (Unicode escapes), and the B'...', X'...' and N'...'
// forms, each of which introduces a literal that a bare quote scan would
// start one or two bytes late. The length is zero when no prefix starts
// here.
func literalPrefix(s string, i int) (length int, kind literalKind) {
	if i+1 >= len(s) {
		return 0, plainLiteral
	}
	switch s[i] {
	case 'E', 'e':
		if s[i+1] == '\'' {
			return 1, escapeLiteral
		}
	case 'B', 'b', 'X', 'x':
		if s[i+1] == '\'' {
			return 1, bitLiteral
		}
	case 'N', 'n':
		if s[i+1] == '\'' {
			return 1, plainLiteral
		}
	case 'U', 'u':
		if s[i+1] == '&' && i+2 < len(s) {
			switch s[i+2] {
			case '\'':
				return 2, unicodeLiteral
			case '"':
				return 2, quotedIdentifier
			}
		}
	}
	return 0, plainLiteral
}

// IsQuoteStart reports whether a quoted string or quoted identifier
// starts at s[i], either with the quote itself or with one of the
// literal prefixes literalPrefix recognizes. A prefix letter only counts
// when it does not continue an identifier, so the trailing e of a column
// name is not mistaken for an E'...' literal.
func IsQuoteStart(s string, i int) bool {
	if s[i] == '\'' || s[i] == '"' {
		return true
	}
	if ContinuesIdentifier(s, i) {
		return false
	}
	length, _ := literalPrefix(s, i)
	return length > 0
}

// SkipQuoted returns the index just past the quoted string or quoted
// identifier starting at s[i], which must be a single or double quote or
// the first character of a literal prefix (see literalPrefix), following
// the string states of PostgreSQL's lexer. A doubled quote is an escaped
// quote rather than a terminator, except in a bit string. A backslash
// escapes the character that follows it in an E'...' literal, and in a
// plain '...' or N'...' literal under EscapeStrings. A string literal
// that is followed by whitespace containing a newline and another quote
// continues after that quote in the same state (quotecontinue in
// scan.l), so this is a single escape string whose second part ends at
// its last quote, not at the one after the backslash:
//
//	E'a'
//	'\''
//
// An unterminated literal runs to the end of the string.
func SkipQuoted(s string, i int, reading StringReading) int {
	kind := plainLiteral
	switch s[i] {
	case '\'':
	case '"':
		kind = quotedIdentifier
	default:
		length, prefixKind := literalPrefix(s, i)
		if length == 0 {
			return i + 1
		}
		kind = prefixKind
		i += length
	}

	escapes := kind == escapeLiteral ||
		(kind == plainLiteral && reading == EscapeStrings)
	doubles := kind != bitLiteral
	continues := kind != quotedIdentifier

	quote := s[i]
	i++
	for i < len(s) {
		switch {
		case escapes && s[i] == '\\':
			i += 2
		case s[i] == quote:
			if doubles && i+1 < len(s) && s[i+1] == quote {
				i += 2
				continue
			}
			if continues {
				if j := quoteContinuation(s, i+1); j > 0 {
					i = j
					continue
				}
			}
			return i + 1
		default:
			i++
		}
	}
	return len(s)
}

// quoteContinuation reports where a string literal resumes when the
// text from s[i], just after its closing quote, continues it: whitespace
// and line comments containing at least one newline, then a quote
// (quotecontinue in scan.l). It returns the index just past that quote,
// or -1 when the literal really ended. A line comment must itself end in
// a newline, and a block comment ends the whitespace, as in scan.l.
func quoteContinuation(s string, i int) int {
	sawNewline := false
	for i < len(s) {
		switch {
		case IsSQLNewline(s[i]):
			sawNewline = true
			i++
		case IsSQLSpace(s[i]):
			i++
		case s[i] == '-' && i+1 < len(s) && s[i+1] == '-':
			for i < len(s) && !IsSQLNewline(s[i]) {
				i++
			}
		case s[i] == '\'' && sawNewline:
			return i + 1
		default:
			return -1
		}
	}
	return -1
}

// skipBlockComment returns the index just past the (possibly nested)
// block comment starting at s[i], which must be the opening slash.
func skipBlockComment(s string, i int) int {
	depth := 1
	i += 2
	for i < len(s) && depth > 0 {
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
			depth++
			i += 2
		} else if s[i] == '*' && i+1 < len(s) && s[i+1] == '/' {
			depth--
			i += 2
		} else {
			i++
		}
	}
	return i
}

// SkipNonCode returns the index just past the quoted literal or comment
// starting at s[i], or i itself when s[i] does not begin one. It is the
// step every scan over statement text shares: a quoted string, a quoted
// identifier (including the prefixed E'...', U&'...' and U&"..." forms),
// a dollar-quoted string, a line comment or a block comment holds text
// rather than SQL, so a comma, parenthesis, keyword or $N inside one
// must not be read as code, and a quote or comment marker inside a
// dollar-quoted string must not open a region that swallows the code
// after it. An unterminated literal or comment runs to the end of the
// string, which ends the caller's scan rather than resuming inside the
// quoted text. A $N placeholder is not a dollar quote (see
// ScanDollarTag), so it is left for the caller to see. reading says how
// a plain '...' literal is read (see StringReading).
func SkipNonCode(s string, i int, reading StringReading) int {
	if IsQuoteStart(s, i) {
		return SkipQuoted(s, i, reading)
	}
	if tag := ScanDollarTag(s, i); tag != "" {
		end := strings.Index(s[i+len(tag):], tag)
		if end < 0 {
			return len(s)
		}
		return i + len(tag) + end + len(tag)
	}
	if s[i] == '-' && i+1 < len(s) && s[i+1] == '-' {
		j := i
		for j < len(s) && !IsSQLNewline(s[j]) {
			j++
		}
		return j
	}
	if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
		return skipBlockComment(s, i)
	}
	return i
}

// MaskNonCode replaces every quoted literal, quoted identifier and
// comment in s with spaces, so a keyword scan reads code only. Each
// masked byte becomes one space, leaving the result the same length as
// s, which keeps any offset measured on the mask valid against the
// original. Callers must mask before uppercasing, because
// strings.ToUpper is not length-preserving for every input.
func MaskNonCode(s string, reading StringReading) string {
	masked := []byte(s)
	for i := 0; i < len(masked); {
		j := SkipNonCode(s, i, reading)
		if j == i {
			i++
			continue
		}
		for k := i; k < j; k++ {
			masked[k] = ' '
		}
		i = j
	}
	return string(masked)
}

// IsIdentChar returns true if the byte is a valid SQL identifier
// character: an ASCII letter, digit or underscore, or any byte from 0x80
// up, which PostgreSQL's lexer treats as a letter (ident_start and
// ident_cont in scan.l). Outside a quoted literal or comment such a byte
// can only be part of an identifier, so a keyword, literal prefix or
// dollar sign that touches one continues that identifier. ident_cont
// also includes $, which cannot start an identifier and so is left out
// here; ContinuesIdentifier covers it where a character that follows
// one matters.
func IsIdentChar(b byte) bool {
	return (b >= 'A' && b <= 'Z') ||
		(b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') ||
		b == '_' ||
		b >= 0x80
}

// ContinuesIdentifier reports whether s[i] would continue an unquoted
// identifier, because the byte before it is an identifier character or
// a $ (PostgreSQL's ident_cont includes $, as in a$b). A dollar sign
// or literal prefix there is part of the identifier rather than the
// start of a dollar quote or a prefixed literal, so in a$E'\' the
// literal is a standard string rather than an escape string. A $ that
// is not itself part of an identifier makes the statement a syntax
// error, so treating every preceding $ this way cannot hide code that
// runs.
func ContinuesIdentifier(s string, i int) bool {
	if i == 0 {
		return false
	}
	prev := s[i-1]
	return IsIdentChar(prev) || prev == '$'
}

// IsSQLSpace reports whether b is whitespace to PostgreSQL's lexer
// (space in scan.l): space, tab, newline, carriage return, form feed or
// vertical tab.
func IsSQLSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

// IsSQLNewline reports whether b ends a -- line comment. PostgreSQL's
// lexer ends one at a carriage return as well as a newline
// (non_newline in scan.l), so code after a lone \r is code.
func IsSQLNewline(b byte) bool {
	return b == '\n' || b == '\r'
}
