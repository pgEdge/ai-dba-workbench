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

// bindParam matches a bound parameter reference; castAfter matches a
// :: cast directly after one, and castOpen and castAs the CAST( before
// and the AS after one written CAST($n AS type).
var (
	bindParam = regexp.MustCompile(`\$\d+`)
	castAfter = regexp.MustCompile(`^\s*::`)
	castOpen  = regexp.MustCompile(`(?i)\bCAST\s*\(\s*$`)
	castAs    = regexp.MustCompile(`(?i)^\s+AS\b`)
)

// typeName matches a type name in a cast, including the multi-word
// forms PostgreSQL spells with spaces, but never a following keyword.
const typeName = `[a-z_][a-z0-9_]*(\s+(with(out)?\s+time\s+zone|precision|varying))?`

// paramNullTest matches a NULL test on a parameter (IS [NOT] NULL,
// ISNULL or NOTNULL) whether the parameter is bare, cast with :: or
// CAST(), or wrapped in any number of parentheses.
var paramNullTest = regexp.MustCompile(`(?i)(\$\d+(\s*::\s*` + typeName + `)?|\bCAST\s*\(\s*\$\d+\s+AS\s+` + typeName + `\s*\))` +
	`[\s)]*\b(IS\s+(NOT\s+)?NULL|ISNULL|NOTNULL)\b`)

// forbidden lists SQL shapes the Workbench must not send to a ColdFront
// datastore, each with the reason reported when one appears. Each was
// tried live against ColdFront with pgx bound parameters, except ALL(),
// SOME() and ARRAY[...], which are barred with ANY() and the array cast
// as the same array typing DuckDB rejects, and COALESCE over a
// parameter, which DuckDB runs but which defeats PostgreSQL's index and
// partition pruning under a generic plan.
var forbidden = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`(?i)\bANY\s*\(`),
		"ANY(...) over an array fails with an UNKNOWN type in DuckDB, even with a literal or cast array"},
	{regexp.MustCompile(`(?i)(?:[=<>!]|\bLIKE)\s*(?:ALL|SOME)\s*\(`),
		"ALL(...) and SOME(...) over an array fail in DuckDB as ANY(...) does; use OR'd equalities, IN over literals, or a join"},
	{regexp.MustCompile(`(?i)\bARRAY\s*\[`),
		"array literals fail with an UNKNOWN type in DuckDB; bind scalars or one text value instead"},
	{regexp.MustCompile(`(?i)\bunnest\s*\(`),
		"unnest of an array parameter fails with an UNKNOWN type in DuckDB"},
	{regexp.MustCompile(`(?i)::\s*[a-z0-9_ ]+\[\]`),
		"array casts fail in DuckDB; bind scalars or one text value instead"},
	{regexp.MustCompile(`(?i)\bpg_lsn\b`),
		"DuckDB has no pg_lsn type; use anomaly.LSNDiffBytesSQL on the text LSN"},
	{regexp.MustCompile(`(?i)\bmake_interval\s*\(`),
		"DuckDB has no make_interval(); use INTERVAL '1 day' * $1::int"},
	{paramNullTest,
		"DuckDB cannot plan a NULL test on a parameter, cast or not; bind a finite far-future timestamp for an open bound instead"},
	{regexp.MustCompile(`(?i)\bCOALESCE\s*\(\s*\$\d+`),
		"COALESCE over a parameter stays a per-row filter under a generic plan, with no index condition or partition pruning; compare with the parameter directly and bind a finite far-future timestamp for an open bound"},
	{regexp.MustCompile(`(?i)\binfinity\b`),
		"DuckDB may subtract infinite timestamps in a range join and fail; leave open column bounds NULL and bind a finite far-future timestamp for an open parameter"},
}

// hostCall matches the start of a host() call, inetCast a cast written
// x::inet, castCall the start of a CAST() call, and inetTarget the inet
// type after a CAST's AS.
var (
	hostCall   = regexp.MustCompile(`(?i)\bhost\s*\(`)
	inetCast   = regexp.MustCompile(`(?i)::\s*inet\b`)
	castCall   = regexp.MustCompile(`(?i)\bCAST\s*\(`)
	inetTarget = regexp.MustCompile(`(?i)^\s+inet\b`)
)

// floatCast matches a cast to double precision directly after a closing
// parenthesis.
var floatCast = regexp.MustCompile(`(?i)^\s*::\s*(float8|float|double precision)\b`)

// avgCall matches the start of an avg() aggregate call.
var avgCall = regexp.MustCompile(`(?i)\bavg\s*\(`)

// inList matches the start of an IN (or NOT IN) list, subquery matches
// the start of a subquery inside one, and textCast a cast to text
// directly after a parameter.
var (
	inList   = regexp.MustCompile(`(?i)\bIN\s*\(`)
	subquery = regexp.MustCompile(`(?i)^\s*(SELECT|WITH|VALUES)\b`)
	textCast = regexp.MustCompile(`(?i)^\s*::\s*text\b`)
)

// Violations returns a description of every ColdFront rule sql breaks,
// or nil when it breaks none: an uncast bound parameter, ANY(), ALL()
// or SOME() over an array, an ARRAY[...] literal, unnest(), an array
// cast, pg_lsn, make_interval(), any host() call, any cast to inet, a
// NULL test on a parameter, a COALESCE over a parameter, an infinite
// timestamp, an IN list holding two or more parameters that are not
// text, or an avg() not cast to float where it stands.
//
// It is a textual check and does not parse SQL, so it also reports a
// forbidden word inside a string literal or comment; keep such words out
// of the SQL text altogether.
func Violations(sql string) []string {
	var problems []string

	for _, loc := range bindParam.FindAllStringIndex(sql, -1) {
		if !paramCast(sql, loc) {
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
	problems = append(problems, inListViolations(sql)...)

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

// inListViolations reports each IN (or NOT IN) list holding two or more
// bound parameters that are not cast to text, as in IN ($1::int,
// $2::int). pg_duckdb fails to plan such a list under pgx's extended
// protocol, in every exec mode, with "Could not convert DuckDB type:
// UNKNOWN to Postgres type", whatever the parameters' casts; a single
// parameter, a parameter beside literals, and parameters cast to text
// all run. A subquery's own lists are checked where they stand.
func inListViolations(sql string) []string {
	var problems []string
	for _, loc := range inList.FindAllStringIndex(sql, -1) {
		end := matchingParen(sql, loc[1]-1)
		if end < 0 || subquery.MatchString(sql[loc[1]:end]) {
			continue
		}
		typed := 0
		for _, p := range bindParam.FindAllStringIndex(sql[loc[1]:end], -1) {
			if !textCast.MatchString(sql[loc[1]+p[1]:]) {
				typed++
			}
		}
		if typed >= 2 {
			problems = append(problems, strings.TrimSpace(sql[loc[0]:end+1])+
				": DuckDB cannot type an IN list of two or more non-text "+
				"parameters, even cast; write (col = $1::int OR col = $2::int)")
		}
	}
	return problems
}

// paramCast reports whether the parameter at loc in sql is cast where it
// stands, as $n::type (spaces allowed around the ::) or CAST($n AS type).
func paramCast(sql string, loc []int) bool {
	if castAfter.MatchString(sql[loc[1]:]) {
		return true
	}
	return castOpen.MatchString(sql[:loc[0]]) && castAs.MatchString(sql[loc[1]:])
}

// inetViolations reports each host() call and each cast to inet, whether
// written ::inet or CAST(... AS inet). inet columns are text on
// ColdFront, so host() over one fails, and a cast to inet makes DuckDB
// load its inet extension, which a ColdFront bundle with extension
// autoinstall disabled cannot do (issue #889). The portable way to render
// an address is split_part(col::text, '/', 1): on PostgreSQL it drops the
// prefix length inet's text form carries, as host() does, and on
// ColdFront the text column already holds the bare address.
func inetViolations(sql string) []string {
	var problems []string
	for range hostCall.FindAllStringIndex(sql, -1) {
		problems = append(problems, "host(...): host() needs an inet, which "+
			"inet columns are not on ColdFront; write "+inetHint)
	}
	for range InetCasts(sql) {
		problems = append(problems, "cast to inet: ::inet and CAST(... AS "+
			"inet) need DuckDB's inet extension, which a ColdFront bundle "+
			"may be unable to load; write "+inetHint)
	}
	return problems
}

// InetCasts returns the text of each cast to inet in sql: the ::inet of
// each x::inet, and the whole call of each CAST(x AS inet). A CAST's
// target is the text after its AS at the call's own parenthesis depth,
// so CAST(CAST(x AS text) AS inet) is a cast to inet but neither
// CAST(x AS text) AS inet nor the output alias in x::text AS inet is. An
// unbalanced CAST( is read to the end of sql. Like Violations, it does
// not parse SQL, so a cast inside a string literal or comment counts.
func InetCasts(sql string) []string {
	casts := inetCast.FindAllString(sql, -1)
	for _, loc := range castCall.FindAllStringIndex(sql, -1) {
		end := matchingParen(sql, loc[1]-1)
		if end < 0 {
			end = len(sql)
		}
		as := topLevelAS(sql, loc[1], end)
		if as >= 0 && inetTarget.MatchString(sql[as+2:end]) {
			casts = append(casts, sql[loc[0]:min(end+1, len(sql))])
		}
	}
	return casts
}

// topLevelAS returns the index of the first AS keyword in s[from:to] at
// depth zero, outside nested parentheses and quoted literals or
// identifiers, or -1 when there is none.
func topLevelAS(s string, from, to int) int {
	depth := 0
	for i := from; i < to; i++ {
		switch c := s[i]; {
		case c == '\'' || c == '"':
			end := strings.IndexByte(s[i+1:to], c)
			if end < 0 {
				return -1
			}
			i += end + 1
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && i+2 < to && strings.EqualFold(s[i:i+2], "AS") &&
			(i == from || !identByte(s[i-1])) && !identByte(s[i+2]):
			return i
		}
	}
	return -1
}

// identByte reports whether c can be part of an unquoted SQL identifier.
func identByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' ||
		c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// inetHint is the portable rendering of an inet column, named in each
// inetViolations report.
const inetHint = "split_part(col::text, '/', 1)"

// matchingParen returns the index of the parenthesis closing the one at
// open, or -1 when it is unbalanced. Parentheses inside a single-quoted
// string literal or a double-quoted identifier do not count; a doubled
// quote inside either reads as two adjacent quoted runs, which skips the
// same text.
func matchingParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '\'', '"':
			end := strings.IndexByte(s[i+1:], s[i])
			if end < 0 {
				return -1
			}
			i += end + 1
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
