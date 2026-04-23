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

import "fmt"

// serverStatusCaseSQL returns a SQL CASE expression that computes server
// connectivity status based on monitoring state and collection timestamps.
//
// The collectedAtExpr parameter specifies the column or expression containing
// the timestamp of the last successful collection (e.g., "m.collected_at" or
// "lc.collected_at").
//
// The nullCheckExpr parameter specifies the condition that indicates no data
// has been collected yet (e.g., "m.collected_at IS NULL" or
// "lc.connection_id IS NULL").
//
// Status values returned:
//   - 'offline': connection has an error while being monitored
//   - 'initialising': monitoring is enabled but no data collected yet
//   - 'online': data collected within the last 60 seconds
//   - 'warning': data collected between 60 and 150 seconds ago
//   - 'offline': data exists but is older than 150 seconds
//   - 'unknown': not being monitored or no data available
func serverStatusCaseSQL(collectedAtExpr, nullCheckExpr string) string {
	return fmt.Sprintf(`CASE
                WHEN c.is_monitored AND c.connection_error IS NOT NULL
                THEN 'offline'
                WHEN c.is_monitored AND %s
                THEN 'initialising'
                WHEN %s > NOW() - INTERVAL '60 seconds' THEN 'online'
                WHEN %s > NOW() - INTERVAL '150 seconds' THEN 'warning'
                WHEN %s IS NOT NULL THEN 'offline'
                ELSE 'unknown'
            END`, nullCheckExpr, collectedAtExpr, collectedAtExpr, collectedAtExpr)
}
