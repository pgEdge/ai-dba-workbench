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
// applied to BOTH the trimmed and the raw value: exactly 255 characters is
// accepted, 256 is rejected with ErrNameTooLong, and whitespace padding that
// pushes the raw length over 255 is rejected even when the trimmed length is
// within the limit. The raw cap exists because handlers persist the raw
// value to the VARCHAR(255) column; a padded name must not slip past the
// limit by trimming to a shorter length than it is stored at.
func TestValidateDisplayName_LengthBoundary(t *testing.T) {
	at255 := strings.Repeat("a", MaxDisplayNameLength)
	if err := ValidateDisplayName(at255); err != nil {
		t.Errorf("255-character name should be valid, got %v", err)
	}

	at256 := strings.Repeat("a", MaxDisplayNameLength+1)
	if err := ValidateDisplayName(at256); !errors.Is(err, ErrNameTooLong) {
		t.Errorf("256-character name: got %v, want ErrNameTooLong", err)
	}

	// 255 content characters wrapped in whitespace has a raw length of 259,
	// which exceeds the VARCHAR(255) column even though the trimmed length is
	// 255; it must be rejected because the raw value is what is stored.
	padded := "  " + at255 + "  "
	if err := ValidateDisplayName(padded); !errors.Is(err, ErrNameTooLong) {
		t.Errorf("padded 255-character name (raw 259): got %v, want ErrNameTooLong", err)
	}

	// A 256-character name that trims to 256 invalid runes is still too long.
	if err := ValidateDisplayName("  " + at256 + "  "); !errors.Is(err, ErrNameTooLong) {
		t.Errorf("padded 256-character name: got %v, want ErrNameTooLong", err)
	}
}

// TestValidateDisplayName_RawLengthExceedsTrimmed pins the precise
// whitespace-padding edge case behind the raw-length cap: a name whose
// trimmed length is at or below the limit but whose raw length exceeds it is
// rejected with ErrNameTooLong. This is the case that the removed
// validateMaxLen("Name", ...) calls used to guard; ValidateDisplayName now
// owns it so the VARCHAR(255) column is never reachable with an over-length
// raw value.
func TestValidateDisplayName_RawLengthExceedsTrimmed(t *testing.T) {
	// 250 content characters plus 10 trailing spaces: trimmed length 250
	// (within the limit) but raw length 260 (over the limit).
	content := strings.Repeat("a", 250)
	padded := content + strings.Repeat(" ", 10)

	if got := len([]rune(strings.TrimSpace(padded))); got > MaxDisplayNameLength {
		t.Fatalf("test setup: trimmed length %d should be within the limit", got)
	}
	if got := len([]rune(padded)); got <= MaxDisplayNameLength {
		t.Fatalf("test setup: raw length %d should exceed the limit", got)
	}

	if err := ValidateDisplayName(padded); !errors.Is(err, ErrNameTooLong) {
		t.Errorf("raw-over-limit padded name: got %v, want ErrNameTooLong", err)
	}

	// The exact boundary: a name whose raw length is exactly 255 (254 content
	// characters plus one trailing space) is accepted, confirming the cap is
	// inclusive of the limit and not off by one.
	atRawLimit := strings.Repeat("a", MaxDisplayNameLength-1) + " "
	if got := len([]rune(atRawLimit)); got != MaxDisplayNameLength {
		t.Fatalf("test setup: raw length %d should equal the limit", got)
	}
	if err := ValidateDisplayName(atRawLimit); err != nil {
		t.Errorf("name at raw limit (255) should be valid, got %v", err)
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
