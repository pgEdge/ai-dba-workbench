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
	"strings"

	"github.com/pgedge/ai-workbench/pkg/datastoreconfig"
)

// EscapeValue escapes a value for use in a libpq connection string.
// Per the libpq spec, backslashes must be doubled and single quotes
// within a value must be escaped by doubling them.
// See: https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING
func EscapeValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `''`)
	return value
}

// Build builds a PostgreSQL connection string from a map of parameters.
// The parameters are formatted as key='value' pairs separated by spaces.
// Values are properly escaped per the libpq connection string
// specification.
func Build(params map[string]string) string {
	var parts []string
	for key, value := range params {
		parts = append(parts, fmt.Sprintf("%s='%s'", key, EscapeValue(value)))
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
