/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package connstring builds libpq-compatible PostgreSQL connection
// strings with proper value escaping.
package connstring

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pgedge/ai-workbench/pkg/datastoreconfig"
)

// EscapeValue escapes a value for use inside a single-quoted value in a
// libpq keyword/value connection string. libpq, and pgx's
// pgconn.ParseConfig, treat a backslash as escaping the character after
// it, so each backslash becomes \\ and each single quote becomes \'.
// Doubling a quote is SQL literal syntax, not connection string syntax:
// there the first quote ends the value, and the text after it is parsed
// as further keywords such as host or sslcert.
// See: https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING
func EscapeValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return value
}

// Build builds a PostgreSQL connection string from a map of parameters.
// The parameters are formatted as key='value' pairs separated by spaces,
// in sorted key order so that the output is deterministic. Every value is
// quoted and escaped with EscapeValue. Keys are written as given, so they
// must be libpq keywords chosen by the caller, never untrusted input.
func Build(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s='%s'", key, EscapeValue(params[key])))
	}
	return strings.Join(parts, " ")
}

// BuildFromConfig builds a PostgreSQL connection string from a
// DatastoreConfig. The applicationName parameter identifies the
// connection in pg_stat_activity.
func BuildFromConfig(cfg datastoreconfig.DatastoreConfig, applicationName string) string {
	params := make(map[string]string)
	params["dbname"] = cfg.Database
	params["user"] = cfg.Username

	if cfg.HostAddr != "" {
		params["hostaddr"] = cfg.HostAddr
	}
	if cfg.Host != "" {
		params["host"] = cfg.Host
	}

	if cfg.Port != 0 {
		params["port"] = fmt.Sprintf("%d", cfg.Port)
	}

	if cfg.Password != "" {
		params["password"] = cfg.Password
	}

	if cfg.SSLMode != "" {
		params["sslmode"] = cfg.SSLMode
	}
	if cfg.SSLCert != "" {
		params["sslcert"] = cfg.SSLCert
	}
	if cfg.SSLKey != "" {
		params["sslkey"] = cfg.SSLKey
	}
	if cfg.SSLRootCert != "" {
		params["sslrootcert"] = cfg.SSLRootCert
	}

	if applicationName != "" {
		params["application_name"] = applicationName
	}

	return Build(params)
}
