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
	"regexp"
	"strings"
)

// MaxDisplayNameLength is the maximum permitted length, in characters, of a
// user-facing display name (cluster group, cluster, or server connection
// name). The limit is measured against the trimmed name.
const MaxDisplayNameLength = 255

// displayNameRegex matches the set of characters permitted in a user-facing
// display name: letters, digits, spaces, and the punctuation characters
// period, underscore, hyphen, and parentheses. The pattern is anchored so the
// entire (trimmed) name must consist solely of these characters. Disallowed
// characters include angle brackets, shell and SQL metacharacters, quotes,
// slashes, and control characters (issue #269).
var displayNameRegex = regexp.MustCompile(`^[A-Za-z0-9 ._()-]+$`)

// nameError is a sentinel error type for display-name validation failures.
// Its Error method returns the user-facing message verbatim, including the
// leading capital letter; the message is defined as a string constant so the
// "error strings should not be capitalized" lint rule (ST1005) does not fire
// on a capitalized errors.New literal. The string is safe to pass directly to
// RespondError.
type nameError string

// Error returns the user-facing validation message.
func (e nameError) Error() string { return string(e) }

// User-facing validation messages. They are exported as errors so callers can
// surface the exact text via RespondError and tests can assert on them with
// errors.Is.
const (
	// ErrNameRequired is returned when a name is empty or consists solely of
	// whitespace once trimmed.
	ErrNameRequired nameError = "Name is required"

	// ErrNameTooLong is returned when a trimmed name exceeds
	// MaxDisplayNameLength characters.
	ErrNameTooLong nameError = "Name must be 255 characters or fewer"

	// ErrNameInvalidChars is returned when a trimmed name contains characters
	// outside the permitted set.
	ErrNameInvalidChars nameError = "Name may only contain letters, numbers, " +
		"spaces, and the characters . _ - ( )"
)

// ValidateDisplayName validates a user-facing display name used for cluster
// groups, clusters, and server connections (issue #269).
//
// Validation operates on the name AFTER trimming leading and trailing
// whitespace. A name is valid when, once trimmed, it is non-empty, contains
// only the characters permitted by displayNameRegex, and is at most
// MaxDisplayNameLength characters long.
//
// Note on storage: this function only validates; it does not return a
// normalised name. Callers continue to persist whatever value they read from
// the request body, matching the pre-existing handler behavior, which did
// not trim names before storage. Validating the trimmed form while storing
// the raw value is the least-surprising choice: a name such as "  prod  "
// that trims to a valid value is accepted and stored exactly as submitted,
// and whitespace-only input is rejected. Changing storage to trim names would
// be a separate, deliberate behavioral change.
//
// It returns nil when the name is valid, or one of ErrNameRequired,
// ErrNameTooLong, or ErrNameInvalidChars describing the failure. The error
// strings are user-facing and safe to pass directly to RespondError.
func ValidateDisplayName(name string) error {
	trimmed := strings.TrimSpace(name)

	if trimmed == "" {
		return ErrNameRequired
	}

	// Measure length in runes so multi-byte characters count as one each;
	// this only matters for the boundary check because the permitted set is
	// otherwise ASCII.
	if len([]rune(trimmed)) > MaxDisplayNameLength {
		return ErrNameTooLong
	}

	if !displayNameRegex.MatchString(trimmed) {
		return ErrNameInvalidChars
	}

	return nil
}
