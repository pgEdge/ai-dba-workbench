/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"errors"
	"strings"
	"unicode"
)

// maxDatabaseNameLen is the longest database name PostgreSQL will
// accept, NAMEDATALEN - 1 bytes.
const maxDatabaseNameLen = 63

// ValidateDatabaseName checks a caller-supplied database name before it
// is used to override the database of a stored connection.
//
// Correct escaping is the primary control: BuildConnectionString builds
// its URL with net/url, so the name is escaped by construction and
// cannot smuggle libpq parameters such as host, options or password
// into the connection string. This check is the second line, and it is
// deliberately narrow so that legitimate names, which PostgreSQL allows
// to contain very nearly anything when quoted, are not rejected: it
// turns away only a name that is empty, longer than PostgreSQL itself
// permits, or carrying a NUL or another control character, none of
// which can name a real database and any of which would be awkward in a
// log line or an error message.
func ValidateDatabaseName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("database name must not be empty")
	}
	if len(name) > maxDatabaseNameLen {
		return errors.New("database name must be 63 bytes or fewer")
	}
	for _, r := range name {
		if r == 0 || unicode.IsControl(r) {
			return errors.New("database name must not contain control characters")
		}
	}
	return nil
}
