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
// into the connection string. This check is otherwise the second line,
// and it is deliberately narrow so that legitimate names, which
// PostgreSQL allows to contain very nearly anything when quoted, are
// not rejected: it turns away only a name that is empty, longer than
// PostgreSQL itself permits, or carrying a NUL or another control
// character, none of which can name a real database and any of which
// would be awkward in a log line or an error message.
//
// For a NUL byte, though, this check is the only defense. pgx writes
// each startup parameter as a NUL-terminated string without checking
// for an embedded NUL (StartupMessage.Encode in pgproto3, fed from
// pgconn's connect path, as of v5.9.2), and URL escaping does not help
// because pgx decodes %00 in the URL back to a NUL byte. A name with an
// embedded NUL could therefore end the database parameter early and
// inject startup parameters of the caller's choosing, such as options,
// replication or user. Every path that accepts a database override
// must call this function before the name reaches
// BuildConnectionString.
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
