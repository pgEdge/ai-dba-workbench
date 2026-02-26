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
// parameters that restrict a query to the given set of accessible connection
// IDs. The column argument is the fully qualified column name to filter on
// (e.g. "a.connection_id").
//
// When accessibleIDs is nil the caller is a superuser and all connections are
// allowed, so the returned clause is "TRUE" with no extra parameters.
//
// When accessibleIDs is non-nil the returned clause uses positional parameters
// ($1, $2, ...) for each ID so the filter is injection-safe.
func buildConnectionFilter(column string, accessibleIDs []int) (string, []interface{}) {
	if accessibleIDs == nil {
		// Superuser: no restriction
		return "TRUE", nil
	}

	if len(accessibleIDs) == 0 {
		// No accessible connections: nothing should match
		return "FALSE", nil
	}

	placeholders := make([]string, len(accessibleIDs))
	args := make([]interface{}, len(accessibleIDs))
	for i, id := range accessibleIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	clause := fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", "))
	return clause, args
}
