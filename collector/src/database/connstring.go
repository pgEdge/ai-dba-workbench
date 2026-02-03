/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
// Package database provides database connection and schema management functionality
package database

import (
	"fmt"
	"strings"
)

// escapeConnStringValue escapes a value for use in a libpq connection string.
// Per the libpq spec, single quotes within a value must be escaped by doubling
// them (i.e., ' becomes ”) and backslashes must also be doubled.
// See: https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING
func escapeConnStringValue(value string) string {
	// First escape backslashes (must be done first to avoid double-escaping)
	value = strings.ReplaceAll(value, `\`, `\\`)
	// Then escape single quotes by doubling them
	value = strings.ReplaceAll(value, `'`, `''`)
	return value
}

// buildPostgresConnectionString builds a PostgreSQL connection string from a map of parameters
// The parameters are formatted as key='value' pairs separated by spaces.
// Values are properly escaped per the libpq connection string specification.
func buildPostgresConnectionString(params map[string]string) string {
	var parts []string
	for key, value := range params {
		parts = append(parts, fmt.Sprintf("%s='%s'", key, escapeConnStringValue(value)))
	}
	return strings.Join(parts, " ")
}
