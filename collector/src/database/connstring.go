/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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

// buildPostgresConnectionString builds a PostgreSQL connection string from a map of parameters
// The parameters are formatted as key='value' pairs separated by spaces
func buildPostgresConnectionString(params map[string]string) string {
	var parts []string
	for key, value := range params {
		parts = append(parts, fmt.Sprintf("%s='%s'", key, value))
	}
	return strings.Join(parts, " ")
}
