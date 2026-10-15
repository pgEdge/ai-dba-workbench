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
	"reflect"
	"strings"
	"testing"
)

func TestFirstKeyword(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"simple keyword", "SELECT 1", "SELECT"},
		{"lowercase", "select 1", "SELECT"},
		{"leading whitespace", "  INSERT INTO t", "INSERT"},
		{"form feed and vertical tab", "\f\vVALUES (1)", "VALUES"},
		{"line comment", "-- comment\nUPDATE t SET x=1", "UPDATE"},
		{"carriage return ends a line comment", "-- x\rANALYZE\nSELECT 1", "ANALYZE"},
		{"carriage return and newline", "-- x\r\nDELETE FROM t", "DELETE"},
		{"block comment", "/* comment */ DELETE FROM t", "DELETE"},
		{"nested block comment", "/* a /* b */ c */ WITH x AS (SELECT 1) TABLE x", "WITH"},
		{"unterminated block comment", "/* SELECT 1", ""},
		{"unterminated line comment", "-- SELECT 1", ""},
		{"option list", "(ANALYZE) SELECT 1", ""},
		{"quoted word", `"select" 1`, ""},
		{"empty", "", ""},
		{"only whitespace", "   ", ""},
		{"digit-leading token", "123abc", "123ABC"},
		{"high-bit bytes are identifier characters", "sélect", "SÉLECT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FirstKeyword(tt.sql); got != tt.want {
				t.Errorf("FirstKeyword(%q) = %q, want %q", tt.sql, got, tt.want)
			}
		})
	}
}

func TestStripLeadingComments(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"SELECT 1", "SELECT 1"},
		{"  -- a\n/* b /* c */ */ SELECT 1 -- d", "SELECT 1 -- d"},
		{"-- x\rANALYZE SELECT 1", "ANALYZE SELECT 1"},
		{"-- only a comment", ""},
		{"/* unterminated", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := StripLeadingComments(tt.input); got != tt.want {
			t.Errorf("StripLeadingComments(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestHasOnlyComments(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", true},
		{" \t\r\n\f\v", true},
		{"-- just a comment", true},
		{"/* a /* nested */ comment */", true},
		{"-- a\n/* b */ -- c", true},
		{"-- hello\rSELECT 1", false},
		{"/* c */ SELECT 1", false},
		{"SELECT 1", false},
	}
	for _, tt := range tests {
		if got := HasOnlyComments(tt.input); got != tt.want {
			t.Errorf("HasOnlyComments(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestSplitStatements(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		reading StringReading
		want    []string
	}{
		{"single statement", "SELECT 1", StandardStrings, []string{"SELECT 1"}},
		{"trailing semicolon", "SELECT 1;", StandardStrings, []string{"SELECT 1"}},
		{"several statements", "SELECT 1; SELECT 2;SELECT 3", StandardStrings,
			[]string{"SELECT 1", "SELECT 2", "SELECT 3"}},
		{"empty statements dropped", ";;; SELECT 1 ;;; SELECT 2 ;;;", StandardStrings,
			[]string{"SELECT 1", "SELECT 2"}},
		{"comment-only statement dropped", "SELECT 1; -- done\n/* x */", StandardStrings,
			[]string{"SELECT 1"}},
		{"semicolon in a literal", "SELECT 'a;b'", StandardStrings, []string{"SELECT 'a;b'"}},
		{"semicolon in a quoted identifier", `SELECT "a;b" FROM t`, StandardStrings,
			[]string{`SELECT "a;b" FROM t`}},
		{"semicolon in a dollar quote", "SELECT $t$a;b$t$; SELECT 2", StandardStrings,
			[]string{"SELECT $t$a;b$t$", "SELECT 2"}},
		{"semicolon in a block comment", "SELECT /* a;b */ 1", StandardStrings,
			[]string{"SELECT /* a;b */ 1"}},
		{"semicolon in a line comment", "SELECT 1 -- a;b\n; SELECT 2", StandardStrings,
			[]string{"SELECT 1 -- a;b", "SELECT 2"}},
		{"carriage return ends a line comment", "SELECT 1 -- a\r; ANALYZE", StandardStrings,
			[]string{"SELECT 1 -- a", "ANALYZE"}},
		{"placeholder is not a dollar quote", "SELECT $1; SELECT 2", StandardStrings,
			[]string{"SELECT $1", "SELECT 2"}},
		{"backslash is ordinary in a standard string", `SELECT 'a\'; SELECT 2`, StandardStrings,
			[]string{`SELECT 'a\'`, "SELECT 2"}},
		{"backslash escapes under escape strings", `SELECT 'a\'; SELECT 2`, EscapeStrings,
			[]string{`SELECT 'a\'; SELECT 2`}},
		{"empty input", "", StandardStrings, nil},
		{"whitespace only", " \t\n", StandardStrings, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitStatements(tt.sql, tt.reading)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitStatements(%q) = %q, want %q", tt.sql, got, tt.want)
			}
		})
	}
}

// TestPlainExplainPrefix pins the closed option list that makes a
// statement starting with ANALYZE or VERBOSE a syntax error rather than
// an EXPLAIN ANALYZE.
func TestPlainExplainPrefix(t *testing.T) {
	if !strings.HasPrefix(PlainExplainPrefix, "EXPLAIN (") ||
		!strings.HasSuffix(PlainExplainPrefix, ") ") {
		t.Fatalf("PlainExplainPrefix = %q, want a closed option list", PlainExplainPrefix)
	}
	if strings.Contains(strings.ToUpper(PlainExplainPrefix), "ANALYZE") {
		t.Fatalf("PlainExplainPrefix = %q must not analyze", PlainExplainPrefix)
	}
}
