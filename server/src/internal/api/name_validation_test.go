/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"errors"
	"strings"
	"testing"
)

// TestValidateDisplayName is a table-driven exercise of the issue #269
// display-name policy. It covers accepted names (with every permitted
// punctuation class), each rejected special character called out in the
// issue, empty and whitespace-only input, the 255/256 length boundary, and
// surrounding whitespace that trims to a valid value.
func TestValidateDisplayName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		// Valid names exercising each permitted character class.
		{"simple letters", "Production", nil},
		{"letters and digits", "Cluster01", nil},
		{"with spaces", "East Coast Cluster", nil},
		{"with period", "db.primary", nil},
		{"with underscore", "primary_db", nil},
		{"with hyphen", "us-east-1", nil},
		{"with parentheses", "Primary (read-write)", nil},
		{"all permitted classes", "Region 1 - east_coast.db (rw)", nil},
		{"digits only", "12345", nil},
		{"single character", "A", nil},

		// Leading/trailing whitespace that trims to a valid value.
		{"leading whitespace trims valid", "   Production", nil},
		{"trailing whitespace trims valid", "Production   ", nil},
		{"surrounding whitespace trims valid", "  Prod Cluster  ", nil},

		// Empty and whitespace-only input.
		{"empty string", "", ErrNameRequired},
		{"single space", " ", ErrNameRequired},
		{"only spaces", "     ", ErrNameRequired},
		{"only tabs and newlines", "\t\n\r ", ErrNameRequired},

		// Each rejected special character from the issue (<>!@#$%).
		{"less than", "name<", ErrNameInvalidChars},
		{"greater than", "name>", ErrNameInvalidChars},
		{"angle brackets", "<>!@#$%", ErrNameInvalidChars},
		{"exclamation", "name!", ErrNameInvalidChars},
		{"at sign", "name@", ErrNameInvalidChars},
		{"hash", "name#", ErrNameInvalidChars},
		{"dollar", "name$", ErrNameInvalidChars},
		{"percent", "name%", ErrNameInvalidChars},

		// Other dangerous characters that must also be rejected.
		{"caret", "name^", ErrNameInvalidChars},
		{"ampersand", "a&b", ErrNameInvalidChars},
		{"asterisk", "name*", ErrNameInvalidChars},
		{"single quote", "O'Brien", ErrNameInvalidChars},
		{"double quote", `say "hi"`, ErrNameInvalidChars},
		{"forward slash", "a/b", ErrNameInvalidChars},
		{"backslash", `a\b`, ErrNameInvalidChars},
		{"semicolon", "a;b", ErrNameInvalidChars},
		{"control char", "name\x00", ErrNameInvalidChars},
		{"comma", "a,b", ErrNameInvalidChars},
		{"brace", "name{", ErrNameInvalidChars},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDisplayName(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateDisplayName(%q) = %v, want %v",
					tt.input, err, tt.wantErr)
			}
		})
	}
}

// TestValidateDisplayName_LengthBoundary verifies the 255-character limit is
// applied to the trimmed name: exactly 255 characters is accepted, 256 is
// rejected with ErrNameTooLong, and trailing whitespace that trims a 256-rune
// string back to 255 is accepted.
func TestValidateDisplayName_LengthBoundary(t *testing.T) {
	at255 := strings.Repeat("a", MaxDisplayNameLength)
	if err := ValidateDisplayName(at255); err != nil {
		t.Errorf("255-character name should be valid, got %v", err)
	}

	at256 := strings.Repeat("a", MaxDisplayNameLength+1)
	if err := ValidateDisplayName(at256); !errors.Is(err, ErrNameTooLong) {
		t.Errorf("256-character name: got %v, want ErrNameTooLong", err)
	}

	// 255 content characters wrapped in whitespace trims back to 255 and so
	// must be accepted; the limit is measured against the trimmed value.
	padded := "  " + at255 + "  "
	if err := ValidateDisplayName(padded); err != nil {
		t.Errorf("padded 255-character name should be valid, got %v", err)
	}

	// A 256-character name that trims to 256 invalid runes is still too long.
	if err := ValidateDisplayName("  " + at256 + "  "); !errors.Is(err, ErrNameTooLong) {
		t.Errorf("padded 256-character name: got %v, want ErrNameTooLong", err)
	}
}

// TestValidateDisplayName_ErrorMessages locks in the exact user-facing wording
// so callers and the client team can rely on the strings surfaced via
// RespondError.
func TestValidateDisplayName_ErrorMessages(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{ErrNameRequired, "Name is required"},
		{ErrNameTooLong, "Name must be 255 characters or fewer"},
		{ErrNameInvalidChars,
			"Name may only contain letters, numbers, spaces, and the characters . _ - ( )"},
	}
	for _, c := range cases {
		if c.err.Error() != c.want {
			t.Errorf("message = %q, want %q", c.err.Error(), c.want)
		}
	}
}
