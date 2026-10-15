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
	"regexp"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/sqllex"
)

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

// isSelectQuery reports whether sql is a read-only, SELECT-like
// statement that can safely receive LIMIT/OFFSET injection and
// should run inside a read-only transaction. It recognizes SELECT,
// WITH ... SELECT (CTEs), EXPLAIN, SHOW, VALUES, and TABLE as
// SELECT-like. Leading whitespace, line comments (--), and block
// comments (/* ... */) are stripped before the check.
//
// A SELECT-like statement that calls a known side-effecting function,
// such as SELECT pg_terminate_backend(...), is not read-only: a
// read-only transaction does not stop such functions, so the statement
// is treated as a write (issue #646). The call is looked for under both
// readings of a plain '...' literal, because the tool does not know the
// server's standard_conforming_strings setting, and the check fails
// closed on SQL it cannot tokenize. Such a statement also gets no
// injected LIMIT, which would silently cap how many rows the function
// acts on.
func isSelectQuery(sql string) bool {
	keyword := firstSQLKeyword(sql)
	switch keyword {
	case "SELECT", "SHOW", "VALUES", "TABLE", "EXPLAIN", "WITH":
		return !sqllex.CallsSideEffectingFunctionUnderAnyReading(sql)
	default:
		return false
	}
}

// explainOnlyPlans reports whether "EXPLAIN " followed by stmt only
// plans stmt rather than running it. That holds when stmt starts, after
// any leading comments, with a keyword that begins a statement EXPLAIN
// can plan. Anything else, such as ANALYZE, VERBOSE or an option list in
// parentheses, could make the composed command an EXPLAIN ANALYZE that
// runs the statement, so it is reported as running.
func explainOnlyPlans(stmt string) bool {
	switch firstSQLKeyword(stmt) {
	case "SELECT", "WITH", "INSERT", "UPDATE", "DELETE", "MERGE", "VALUES", "TABLE":
		return true
	default:
		return false
	}
}

// firstSQLKeyword returns the first SQL keyword in the query after
// stripping leading whitespace, line comments (--), and block
// comments (/* ... */). The result is returned in uppercase.
func firstSQLKeyword(sql string) string {
	i := 0
	n := len(sql)

	for i < n {
		// Skip whitespace
		if sql[i] == ' ' || sql[i] == '\t' || sql[i] == '\n' || sql[i] == '\r' {
			i++
			continue
		}

		// Skip line comment: -- until end of line
		if i+1 < n && sql[i] == '-' && sql[i+1] == '-' {
			i += 2
			for i < n && sql[i] != '\n' {
				i++
			}
			continue
		}

		// Skip block comment: /* ... */ (with nesting)
		if i+1 < n && sql[i] == '/' && sql[i+1] == '*' {
			i += 2
			depth := 1
			for i < n && depth > 0 {
				if i+1 < n && sql[i] == '/' && sql[i+1] == '*' {
					depth++
					i += 2
				} else if i+1 < n && sql[i] == '*' && sql[i+1] == '/' {
					depth--
					i += 2
				} else {
					i++
				}
			}
			continue
		}

		// Found start of a keyword; collect it.
		start := i
		for i < n && isIdentChar(sql[i]) {
			i++
		}
		if i > start {
			return strings.ToUpper(sql[start:i])
		}

		// Non-identifier, non-whitespace, non-comment character.
		return ""
	}

	return ""
}
