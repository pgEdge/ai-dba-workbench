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

import (
	"fmt"
	"strings"
)

// buildConnectionFilter constructs a SQL WHERE clause fragment and positional
// parameters that restrict a query to the caller's visible connection IDs.
// The column argument is the fully qualified column name to filter on
// (e.g. "a.connection_id").
//
// When allConnections is true the caller has visibility to every connection
// (superuser or wildcard token scope); the returned clause is "TRUE" with no
// extra parameters.
//
// When allConnections is false and accessibleIDs is empty the caller has
// access to no connections; the returned clause is "FALSE" so the query
// returns an empty result set.
//
// When allConnections is false and accessibleIDs is non-empty the returned
// clause uses positional parameters ($1, $2, ...) for each ID so the filter
// is injection-safe.
func buildConnectionFilter(column string, allConnections bool, accessibleIDs []int) (string, []any) {
	if allConnections {
		// Unrestricted visibility (superuser or wildcard scope).
		return "TRUE", nil
	}

	if len(accessibleIDs) == 0 {
		// No accessible connections: nothing should match
		return "FALSE", nil
	}

	placeholders := make([]string, len(accessibleIDs))
	args := make([]any, len(accessibleIDs))
	for i, id := range accessibleIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	clause := fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", "))
	return clause, args
}
