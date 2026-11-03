/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package coldfrontsql checks SQL that reads metrics.* for shapes a
// pgEdge ColdFront datastore cannot run. There each metrics.* relation
// is a view over an Iceberg table, and pg_duckdb hands any query that
// reads one to DuckDB, which rejects several shapes PostgreSQL accepts.
// Every Workbench query against metrics.* is one SQL text that must run
// on both, so tests in each module that owns such queries run them
// through Violations. See GitHub issue #755 and
// .claude/golang-expert/coldfront-queries.md.
package coldfrontsql

import (
	"regexp"
	"strings"
)

// bindParam matches a bound parameter reference.
var bindParam = regexp.MustCompile(`\$\d+`)

// forbidden lists SQL shapes that DuckDB cannot run, each with the
// reason reported when one appears. Every one was tried live against
// ColdFront with pgx bound parameters.
var forbidden = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`(?i)\bANY\s*\(`),
		"ANY(...) over an array fails with an UNKNOWN type in DuckDB, even with a literal or cast array"},
	{regexp.MustCompile(`(?i)\bunnest\s*\(`),
		"unnest of an array parameter fails with an UNKNOWN type in DuckDB"},
	{regexp.MustCompile(`(?i)::\s*[a-z0-9_ ]+\[\]`),
		"array casts fail in DuckDB; bind scalars or one text value instead"},
	{regexp.MustCompile(`(?i)\bpg_lsn\b`),
		"DuckDB has no pg_lsn type; use anomaly.LSNDiffBytesSQL on the text LSN"},
	{regexp.MustCompile(`(?i)\bmake_interval\s*\(`),
		"DuckDB has no make_interval(); use INTERVAL '1 day' * $1::int"},
	{regexp.MustCompile(`(?i)\$\d+(\s*::\s*[a-z0-9_ ]+?)?\s+IS\s+(NOT\s+)?NULL\b`),
		"DuckDB cannot plan a NULL test on a parameter, cast or not; open a bound with COALESCE($2::timestamptz, col) instead"},
	{regexp.MustCompile(`(?i)\binfinity\b`),
		"DuckDB may subtract infinite timestamps in a range join and fail; leave open bounds NULL"},
}

// hostCall matches the start of a host() call, inetCast a cast to inet,
// and inetArg a host() argument that ends in a cast to inet.
var (
	hostCall = regexp.MustCompile(`(?i)\bhost\s*\(`)
	inetCast = regexp.MustCompile(`(?i)::\s*inet\b`)
	inetArg  = regexp.MustCompile(`(?i)::\s*inet\s*$`)
)

// floatCast matches a cast to double precision directly after a closing
// parenthesis.
var floatCast = regexp.MustCompile(`(?i)^\s*::\s*(float8|float|double precision)\b`)

// avgCall matches the start of an avg() aggregate call.
var avgCall = regexp.MustCompile(`(?i)\bavg\s*\(`)

// Violations returns a description of every ColdFront rule sql breaks,
// or nil when it breaks none: an uncast bound parameter, ANY(), unnest(),
// an array cast, pg_lsn, make_interval(), host() over anything but an
// inet cast, an inet cast outside host(), a NULL test on a parameter, an
// infinite timestamp, or an avg() not cast to float where it stands.
//
// It is a textual check and does not parse SQL, so it also reports a
// forbidden word inside a string literal or comment; keep such words out
// of the SQL text altogether.
func Violations(sql string) []string {
	var problems []string

	for _, loc := range bindParam.FindAllStringIndex(sql, -1) {
		if !strings.HasPrefix(sql[loc[1]:], "::") {
			problems = append(problems, sql[loc[0]:loc[1]]+
				" has no cast; DuckDB types a bare parameter as UNKNOWN, so write "+
				sql[loc[0]:loc[1]]+"::int (or ::text, ...)")
		}
	}

	for _, f := range forbidden {
		if m := f.pattern.FindString(sql); m != "" {
			problems = append(problems, strings.TrimSpace(m)+": "+f.reason)
		}
	}

	problems = append(problems, inetViolations(sql)...)

	// avg() over an integer column is numeric to PostgreSQL but DOUBLE
	// to DuckDB, so pgx fails to scan it; the call must be cast where it
	// stands. EXTRACT() has the same problem, but is legitimately used
	// uncast inside predicates and larger expressions, so its output is
	// policed by result type against a database instead (the alerter's
	// TestMetricsSQLResultTypes).
	for _, loc := range avgCall.FindAllStringIndex(sql, -1) {
		end := matchingParen(sql, loc[1]-1)
		if end < 0 || !floatCast.MatchString(sql[end+1:]) {
			problems = append(problems,
				"avg(...) must be cast to float directly: avg(x)::float")
		}
	}
	return problems
}

// inetViolations reports each host() call whose argument is not cast to
// inet, and each inet cast that is not such an argument. inet columns are
// text on ColdFront, and DuckDB rejects host() over text but runs
// host(col::inet); an inet cast anywhere else is unverified there, and
// would return a result type the Workbench does not scan.
func inetViolations(sql string) []string {
	var problems []string
	excused := map[int]bool{}
	for _, loc := range hostCall.FindAllStringIndex(sql, -1) {
		end := matchingParen(sql, loc[1]-1)
		if end >= 0 {
			if m := inetArg.FindStringIndex(sql[loc[1]:end]); m != nil {
				excused[loc[1]+m[0]] = true
				continue
			}
		}
		problems = append(problems, "host(...) without an inet cast: "+
			"DuckDB rejects host() over text, which inet columns are on "+
			"ColdFront; write host(col::inet)")
	}
	for _, loc := range inetCast.FindAllStringIndex(sql, -1) {
		if !excused[loc[0]] {
			problems = append(problems, "::inet outside host(...::inet): "+
				"inet columns are text on ColdFront; cast to inet only as "+
				"the argument of host()")
		}
	}
	return problems
}

// matchingParen returns the index of the parenthesis closing the one at
// open, or -1 when it is unbalanced.
func matchingParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
