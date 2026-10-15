/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package probes

import "github.com/pgedge/ai-workbench/pkg/sqlredact"

// redactStatementText removes secrets, such as the password in an ALTER
// ROLE statement, from the named statement-text columns of each row so
// they never reach the datastore (GitHub issue #643). PostgreSQL keeps
// utility statements verbatim in pg_stat_activity and pg_stat_statements.
// A NULL or non-text value is left as it is.
func redactStatementText(rows []map[string]any, columns ...string) {
	for _, row := range rows {
		for _, col := range columns {
			if s, ok := row[col].(string); ok {
				row[col] = sqlredact.RedactQuery(s)
			}
		}
	}
}
