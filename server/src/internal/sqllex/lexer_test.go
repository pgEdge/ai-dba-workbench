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

import (
	"strings"
	"testing"
)

func TestScanDollarTag(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		i        int
		expected string
	}{
		{"empty_tag", "$$body$$", 0, "$$"},
		{"named_tag", "$tag$body$tag$", 0, "$tag$"},
		{"underscore_tag", "$_t1$body$_t1$", 0, "$_t1$"},
		{"not_a_tag_digit_start", "$1abc$", 0, ""},
		{"not_a_tag_no_close", "$abc", 0, ""},
		{"mid_string", "SELECT $fn$code$fn$", 7, "$fn$"},
		{"single_dollar", "$", 0, ""},
		{"out_of_bounds", "abc", 5, ""},
		{"identifier_continuation", "a$q$b$q$", 1, ""},
		{"after_dollar", "a$$b$$", 2, ""},
		{"after_non_ascii_byte", "\u00e9$q$x$q$", 2, ""},
		{"after_paren", "($q$x$q$", 1, "$q$"},
		{"non_ascii_tag", "$\u00e9$body$\u00e9$", 0, "$\u00e9$"},
		{"non_ascii_continuation", "$q\u00e9$x$q\u00e9$", 0, "$q\u00e9$"},
		{"non_ascii_after_digit", "$q1\u00e9$x$q1\u00e9$", 0, "$q1\u00e9$"},
		{"high_byte_tag", "$\xff$x$\xff$", 0, "$\xff$"},
		{"non_ascii_tag_no_close", "$\u00e9x", 0, ""},
		{"after_non_ascii_letter_in_tag_position", "\u00e9$\u00e9$", 2, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScanDollarTag(tt.sql, tt.i)
			if got != tt.expected {
				t.Errorf("ScanDollarTag(%q, %d) = %q, want %q",
					tt.sql, tt.i, got, tt.expected)
			}
		})
	}
}

// TestIsIdentChar pins the identifier-character set to PostgreSQL's
// lexer, which treats every byte from 0x80 up as a letter, so a keyword
// or literal prefix that continues a non-ASCII identifier is read as
// part of that identifier.
func TestIsIdentChar(t *testing.T) {
	tests := []struct {
		b    byte
		want bool
	}{
		{'a', true}, {'Z', true}, {'0', true}, {'_', true},
		{0x80, true}, {0xc3, true}, {0xff, true},
		{' ', false}, {'$', false}, {'\'', false}, {'(', false}, {0x7f, false},
	}
	for _, tt := range tests {
		if got := IsIdentChar(tt.b); got != tt.want {
			t.Errorf("IsIdentChar(%#x) = %v, want %v", tt.b, got, tt.want)
		}
	}
}

// TestIssue530_SkipQuoted covers the literal scanner directly, including
// the prefixed forms and the unterminated cases that run to the end.
func TestIssue530_SkipQuoted(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"plain literal", "'abc' rest", 5},
		{"doubled quote", "'a''b' rest", 6},
		{"quoted identifier", `"a b" rest`, 5},
		{"doubled double quote", `"a""b" rest`, 6},
		{"escape literal", `E'\'' rest`, 5},
		{"escape literal lowercase", `e'\'' rest`, 5},
		{"unicode literal", `U&'ab' rest`, 6},
		{"unicode identifier", `U&"ab" rest`, 6},
		{"bit string", "B'10' rest", 5},
		{"hex string", "x'ff' rest", 5},
		{"a quote ends a bit string even when another follows",
			"B'1''0' rest", 4},
		{"national string", "N'ab' rest", 5},
		{"unterminated literal", "'abc", 4},
		{"not a literal prefix", "abc", 1},
		{"prefix letter at the end", "E", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SkipQuoted(tt.input, 0, StandardStrings); got != tt.want {
				t.Errorf("SkipQuoted(%q, 0) = %d, want %d",
					tt.input, got, tt.want)
			}
		})
	}
}

// TestIssue530_MaskNonCode pins the masking helper itself: every masked
// region becomes spaces and the result keeps the original byte length.
func TestIssue530_MaskNonCode(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain code is untouched", "SELECT a FROM t", "SELECT a FROM t"},
		{"string literal", "SELECT 'into' FROM t", "SELECT        FROM t"},
		{"quoted identifier", `SELECT "into" FROM t`, "SELECT        FROM t"},
		{"line comment", "SELECT a -- into\nFROM t", "SELECT a        \nFROM t"},
		{"block comment", "SELECT /* into */ a", "SELECT            a"},
		{"unterminated literal", "SELECT 'into", "SELECT      "},
		{"dollar-quoted literal", "SELECT $q$'$q$ AS c", "SELECT " + strings.Repeat(" ", 7) + " AS c"},
		{"anonymous dollar quote", "SELECT $$x$$", "SELECT      "},
		{"unterminated dollar quote", "SELECT $q$ into", "SELECT " + strings.Repeat(" ", 8)},
		{"dollar continuing an identifier", "SELECT a$q$ FROM t", "SELECT a$q$ FROM t"},
		{"non-ASCII dollar tag", "SELECT $é$'$é$ AS c", "SELECT " + strings.Repeat(" ", 9) + " AS c"},
		{"E continuing a non-ASCII identifier", `SELECT éE'\' AS c`, `SELECT éE    AS c`},
		{"E after an identifier dollar", `SELECT a$E'\' AS c`, `SELECT a$E    AS c`},
		{"carriage return ends a line comment", "SELECT a -- x\rFROM t", "SELECT a     \rFROM t"},
		{"placeholder is code", "SELECT $1", "SELECT $1"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskNonCode(tt.input, StandardStrings)
			if got != tt.want {
				t.Errorf("MaskNonCode(%q) = %q, want %q",
					tt.input, got, tt.want)
			}
			if len(got) != len(tt.input) {
				t.Errorf("MaskNonCode(%q) changed length: %d, want %d",
					tt.input, len(got), len(tt.input))
			}
		})
	}
}

// TestIssue530_SkipQuotedReadings covers the literal scanner under both
// readings of a plain '...' literal, and the continuation of a string
// literal onto a later line, which keeps the state it started in.
func TestIssue530_SkipQuotedReadings(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		reading StringReading
		want    int
	}{
		{"backslash is ordinary in a standard string",
			`'a\' rest`, StandardStrings, 4},
		{"backslash escapes in an escape reading",
			`'a\'' rest`, EscapeStrings, 5},
		{"doubled quote after a backslash in a standard string",
			`'\'' rest`, StandardStrings, len(`'\'' rest`)},
		{"national literal follows the reading",
			`N'\'' rest`, EscapeStrings, 5},
		{"E literal escapes under either reading",
			`E'\'' rest`, StandardStrings, 5},
		{"unicode literal never takes a backslash escape",
			`U&'\'' rest`, EscapeStrings, len(`U&'\'' rest`)},
		{"bit string never takes a backslash escape",
			`B'\' rest`, EscapeStrings, 4},
		{"quoted identifier never takes a backslash escape",
			`"\" rest`, EscapeStrings, 3},
		{"literal continued onto the next line", "'a'\n'b' rest", StandardStrings, 7},
		{"continuation after a carriage return", "'a'\r'b' rest", StandardStrings, 7},
		{"continuation after spaces and a line comment",
			"'a' -- c\n  'b' rest", StandardStrings, 14},
		{"continuation after a comment on its own line",
			"'a'\n-- c\n'b' rest", StandardStrings, 12},
		{"continuation keeps the escape state",
			"E'a'\n'\\'' rest", StandardStrings, 9},
		{"continuation keeps the bit string state",
			"B'1'\n'' rest", StandardStrings, 7},
		{"no continuation without a newline", "'a' 'b'", StandardStrings, 3},
		{"a block comment ends the continuation",
			"'a' /* c */\n'b'", StandardStrings, 3},
		{"a line comment running to the end ends it",
			"'a'\n-- c", StandardStrings, 3},
		{"a newline alone is no continuation", "'a'\n", StandardStrings, 3},
		{"a quoted identifier does not continue",
			"\"a\"\n\"b\"", StandardStrings, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SkipQuoted(tt.input, 0, tt.reading); got != tt.want {
				t.Errorf("SkipQuoted(%q, 0, %d) = %d, want %d",
					tt.input, tt.reading, got, tt.want)
			}
		})
	}
}
