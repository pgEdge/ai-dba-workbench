/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import "testing"

func TestSanitizeTSVField(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"plain text unchanged", "hello world", "hello world"},
		{"tab replaced with space", "a\tb", "a b"},
		{"newline replaced with space", "a\nb", "a b"},
		{"carriage return replaced with space", "a\rb", "a b"},
		{"crlf replaced with two spaces", "a\r\nb", "a  b"},
		{"mixed separators", "a\tb\nc\rd", "a b c d"},
		{"multiple tabs", "x\t\ty", "x  y"},
		{"unicode preserved", "café ✓", "café ✓"},
		{"no backslash escaping", "foo\tbar", "foo bar"},
		{"leading and trailing separators", "\tmiddle\n", " middle "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeTSVField(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeTSVField(%q) = %q, want %q",
					tt.input, got, tt.expected)
			}
		})
	}
}
