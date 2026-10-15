/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package sqllex

import "strings"

// PlainExplainPrefix is what a caller puts in front of a statement it
// only wants planned. The explicit option list matters: after
// "EXPLAIN " a statement starting with ANALYZE, VERBOSE or its own
// option list would turn the command into an EXPLAIN ANALYZE that runs
// it, whereas after a closed option list those are a syntax error.
// That holds however a classifier reads the statement's first word, so
// a lexer mismatch cannot turn planning into execution.
const PlainExplainPrefix = "EXPLAIN (COSTS TRUE) "

// skipSpaceAndComments returns the index of the first byte at or after
// s[i] that is neither whitespace nor part of a line or block comment.
// A line comment ends at a newline or a carriage return, as in
// PostgreSQL's lexer, so code after a lone \r is code.
func skipSpaceAndComments(s string, i int) int {
	for i < len(s) {
		switch {
		case IsSQLSpace(s[i]):
			i++
		case s[i] == '-' && i+1 < len(s) && s[i+1] == '-':
			for i < len(s) && !IsSQLNewline(s[i]) {
				i++
			}
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '*':
			i = skipBlockComment(s, i)
		default:
			return i
		}
	}
	return i
}

// FirstKeyword returns the first word of sql in upper case, after any
// leading whitespace and comments, or an empty string when sql starts
// with something other than an identifier character (a parenthesis, a
// quote) or holds no code at all.
func FirstKeyword(sql string) string {
	start := skipSpaceAndComments(sql, 0)
	end := start
	for end < len(sql) && IsIdentChar(sql[end]) {
		end++
	}
	return strings.ToUpper(sql[start:end])
}

// StripLeadingComments returns s from its first byte of code, after any
// leading whitespace and comments, or an empty string when s holds no
// code.
func StripLeadingComments(s string) string {
	return s[skipSpaceAndComments(s, 0):]
}

// HasOnlyComments reports whether s holds nothing but whitespace and
// comments.
func HasOnlyComments(s string) bool {
	return skipSpaceAndComments(s, 0) == len(s)
}

// SplitStatements splits sql into its statements at each semicolon that
// is outside a quoted string, quoted identifier, dollar-quoted string or
// comment. Each statement is trimmed, and empty or comment-only
// statements are dropped. reading says how a plain '...' literal is
// read (see StringReading), which can move a statement boundary.
func SplitStatements(sql string, reading StringReading) []string {
	var statements []string
	add := func(part string) {
		part = strings.TrimSpace(part)
		if part != "" && !HasOnlyComments(part) {
			statements = append(statements, part)
		}
	}

	start := 0
	for i := 0; i < len(sql); {
		if j := SkipNonCode(sql, i, reading); j != i {
			i = j
			continue
		}
		if sql[i] == ';' {
			add(sql[start:i])
			start = i + 1
		}
		i++
	}
	add(sql[start:])
	return statements
}
