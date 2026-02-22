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

import "regexp"

// Precompiled regexes for SQL clause detection. These match LIMIT and
// OFFSET as standalone SQL keywords followed by a numeric value, avoiding
// false positives from column names such as "credit_limit".
var (
	limitClauseRe  = regexp.MustCompile(`(?i)\bLIMIT\s+\d+`)
	offsetClauseRe = regexp.MustCompile(`(?i)\bOFFSET\s+\d+`)
)

// hasLimitClause reports whether sql already contains a LIMIT clause
// as a standalone keyword (not as part of a column or table name).
func hasLimitClause(sql string) bool {
	return limitClauseRe.MatchString(sql)
}

// hasOffsetClause reports whether sql already contains an OFFSET clause
// as a standalone keyword (not as part of a column or table name).
func hasOffsetClause(sql string) bool {
	return offsetClauseRe.MatchString(sql)
}
