/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package coldfrontcheck finds SQL shapes a pgEdge ColdFront datastore
// cannot plan, or describes with a type PostgreSQL does not expect
// (issue #755). Every metrics.* relation there is a view that DuckDB
// plans, and each shape it reports failed against a live ColdFront
// datastore; .claude/golang-expert/metrics-queries.md records them.
//
// It is for tests only: the metrics package's source scan
// (TestMetricsSQLAvoidsColdFrontShapes) and the tests that check SQL
// generated at run time, such as query_datastore's. The server binary
// must never import it.
package coldfrontcheck

import (
	"regexp"
	"strconv"
	"strings"
)

// BareHost matches host() over client_addr without the inet cast.
var BareHost = regexp.MustCompile(`(?i)\bhost\s*\(\s*client_addr\s*\)`)

// coldFrontBadShapes are the SQL shapes a ColdFront datastore cannot plan
// or describes with a type PostgreSQL does not expect, each with the
// portable shape to use instead.
var coldFrontBadShapes = []struct {
	re   *regexp.Regexp
	hint string
}{
	{regexp.MustCompile(`(?i)\bANY\s*\(\s*\$(\d+|%d)`),
		"an array parameter cannot be typed; bind the list with IntListArg and expand it with IntArrayExpr"},
	{regexp.MustCompile(`(?i)\bIN\s*\(\s*\$(\d+|%d)(::(int|int4|int8|integer|bigint|smallint))?\s*,`),
		"an IN list of untyped or integer parameters cannot be typed; use IntArrayExpr for integers, or $n::text for each text value"},
	{BareHost,
		"client_addr is text on ColdFront and host() takes an inet; write host(client_addr::inet)"},
	{regexp.MustCompile(`\$(\d+|%d)(::[a-z ]+)?\s*[-+*/]\s`), paramArithmeticHint},
	{regexp.MustCompile(`(?i)[-*/]\s*\$(\d+|%d)\b`), paramArithmeticHint},
	{regexp.MustCompile(`(?i)\bpg_lsn\b`),
		"DuckDB has no pg_lsn type; work in bigint byte positions or compute the difference in Go"},
	{regexp.MustCompile(`(?i)'[-+]?infinity'`),
		"DuckDB may subtract an infinite bound and fail; for an open upper bound, " +
			"bind a finite far-future timestamp (9999-12-31) and compare with the parameter directly"},
	{regexp.MustCompile(`(?i)\$(\d+|%d)(::[a-z_ ]+?)?\s+IS\s+(NOT\s+)?NULL\b`),
		"a NULL test on a parameter cannot be planned, cast or not; bind a finite " +
			"far-future timestamp (9999-12-31) for an open bound, or test a column instead"},
}

// paramArithmeticHint is the advice for arithmetic on a parameter.
const paramArithmeticHint = "arithmetic on a parameter cannot be planned; compute the value " +
	"in Go and bind it, or multiply an interval by an integer-typed parameter " +
	"(INTERVAL '1 day' * $n::int)"

// intervalTimesIntParam matches the one parameter arithmetic a ColdFront
// datastore plans: an interval literal multiplied by a parameter cast to
// an integer type, in either order. Every other parameter arithmetic
// fails, a float8-cast multiplier and a cast parameter plus or minus
// anything included.
var intervalTimesIntParam = regexp.MustCompile(
	`(?i)INTERVAL\s+'[^']*'\s*\*\s*\$(\d+|%d)::(int|integer|int4|int8|bigint)\b|` +
		`\$(\d+|%d)::(int|integer|int4|int8|bigint)\s*\*\s*INTERVAL\s+'[^']*'`)

// arrayLiteral matches a '{...}' literal and the cast that follows it,
// if any. DuckDB cannot cast such a literal's text to an array, though a
// JSON object literal cast to json or jsonb is fine.
var arrayLiteral = regexp.MustCompile(`(?i)'\{[^']*\}'(\s*::\s*[a-z_]+)?`)

// qualifiedTable matches a schema-qualified name after FROM or JOIN,
// capturing the word before the keyword and the schema.
var qualifiedTable = regexp.MustCompile(`(?i)(?:(\w+)\s+)?\b(?:from|join)\s+"?([a-z_][a-z0-9_]*)"?\s*\.\s*["a-z_%]`)

// notARelation are the words before a FROM that is not a relation's:
// IS DISTINCT FROM and EXTRACT(field FROM ...) take a column.
var notARelation = map[string]bool{"distinct": true, "epoch": true, "year": true, "month": true,
	"day": true, "hour": true, "minute": true, "second": true}

// untypedJSONValue matches a string literal passed as a JSON builder's
// value: a key literal, a comma and a second literal with no cast.
var untypedJSONValue = regexp.MustCompile(`'[a-z_]+'\s*,\s*'[^']*'(\s*[,)\n]|\s*$)`)

// uncastCalls are the calls whose PostgreSQL type (numeric) differs from
// DuckDB's (DOUBLE), so each must be followed by a cast that changes it.
var uncastCalls = regexp.MustCompile(`(?i)\b(avg|extract)\s*\(`)

// castAfter matches a type-changing cast at the start of the text that
// follows a call.
var castAfter = regexp.MustCompile(`(?i)^\s*::\s*(float8|double precision|bigint)\b`)

// ShapeProblems returns a description of every non-portable
// shape in sql, which reads the metrics schema.
func ShapeProblems(sql string) []string {
	var problems []string
	for _, check := range []func(string) []string{
		badShapeProblems, arrayLiteralProblems, qualifiedTableProblems,
		jsonValueProblems, uncastCallProblems,
	} {
		problems = append(problems, check(sql)...)
	}
	return problems
}

// badShapeProblems reports each coldFrontBadShapes match, once the one
// planned parameter arithmetic (intervalTimesIntParam) is masked.
func badShapeProblems(sql string) []string {
	var problems []string
	masked := intervalTimesIntParam.ReplaceAllString(sql, "interval_multiple")
	for _, bad := range coldFrontBadShapes {
		if m := bad.re.FindString(masked); m != "" {
			problems = append(problems, strconv.Quote(m)+": "+bad.hint)
		}
	}
	return problems
}

// arrayLiteralProblems reports a '{...}' literal not cast to json or
// jsonb.
func arrayLiteralProblems(sql string) []string {
	var problems []string
	for _, m := range arrayLiteral.FindAllStringSubmatch(sql, -1) {
		cast := strings.ToLower(strings.TrimLeft(m[1], " \t\n:"))
		if cast != "json" && cast != "jsonb" {
			problems = append(problems, strconv.Quote(m[0])+": DuckDB cannot cast a '{...}' "+
				"literal to an array; write ARRAY[...], or bind integers with IntListArg and IntArrayExpr")
		}
	}
	return problems
}

// qualifiedTableProblems reports a schema-qualified relation other than
// one in the metrics schema.
func qualifiedTableProblems(sql string) []string {
	var problems []string
	for _, m := range qualifiedTable.FindAllStringSubmatch(sql, -1) {
		if !strings.EqualFold(m[2], "metrics") && !notARelation[strings.ToLower(m[1])] {
			problems = append(problems, strconv.Quote(strings.TrimSpace(m[0]))+": ColdFront plans the "+
				"whole statement in DuckDB, which cannot resolve a schema-qualified PostgreSQL table; "+
				"name it unqualified, as the other metrics queries do")
		}
	}
	return problems
}

// jsonValueProblems reports an untyped string literal passed as a JSON
// builder's value.
func jsonValueProblems(sql string) []string {
	if !strings.Contains(sql, "_build_object") {
		return nil
	}
	if m := untypedJSONValue.FindString(sql); m != "" {
		return []string{strconv.Quote(strings.TrimSpace(m)) + ": DuckDB cannot type a " +
			"string literal as a JSON builder's value; cast it to text"}
	}
	return nil
}

// uncastCallProblems reports an avg or EXTRACT call whose result is not
// cast to a type both engines agree on.
func uncastCallProblems(sql string) []string {
	var problems []string
	for _, loc := range uncastCalls.FindAllStringIndex(sql, -1) {
		end := matchingParen(sql, loc[1]-1)
		if end < 0 {
			// The call is closed in another fragment; nothing to check.
			continue
		}
		if !castAfter.MatchString(sql[end+1:]) {
			problems = append(problems, strconv.Quote(sql[loc[0]:end+1])+": numeric to PostgreSQL but DOUBLE on "+
				"ColdFront; cast it to float8 (EpochSeconds, AggregateExpr)")
		}
	}
	return problems
}

// runtimeINList matches an IN list whose items are formatted at run time.
var runtimeINList = regexp.MustCompile(`(?i)\bIN\s*\(\s*%s\s*\)`)

// listItemPlaceholder matches a literal that is a placeholder alone, with
// any cast: the fragment a run-time IN list is joined from.
var listItemPlaceholder = regexp.MustCompile(`^\$%d(::[a-z_ ]+)?$`)

// UnitProblems returns the problems of a unit's SQL that need more than
// one literal to judge: an IN list formatted at run time must be built
// from $n::text placeholders, the only list items a ColdFront datastore
// can type. When a unit formats such a list, every placeholder literal
// it holds must carry the text cast, so one typed list cannot excuse an
// untyped one beside it.
func UnitProblems(literals []string) []string {
	inList, typed := "", false
	var untyped []string
	for _, lit := range literals {
		if m := runtimeINList.FindString(lit); m != "" {
			inList = m
		}
		if listItemPlaceholder.MatchString(lit) {
			if lit == "$%d::text" {
				typed = true
			} else {
				untyped = append(untyped, lit)
			}
		}
	}
	if inList == "" {
		return nil
	}
	var problems []string
	for _, lit := range untyped {
		problems = append(problems, strconv.Quote(lit)+": a unit that formats "+strconv.Quote(inList)+
			" must cast every list placeholder to text; bind integers with IntListArg and IntArrayExpr")
	}
	if !typed && len(untyped) == 0 {
		problems = append(problems, strconv.Quote(inList)+": build the list from \"$%d::text\" placeholders, "+
			"or bind integers with IntListArg and IntArrayExpr")
	}
	return problems
}

// matchingParen returns the index of the parenthesis closing the one at
// open, or -1 when sql ends first.
func matchingParen(sql string, open int) int {
	depth := 0
	for i := open; i < len(sql); i++ {
		switch sql[i] {
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
