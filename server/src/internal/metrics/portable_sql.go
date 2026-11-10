/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import (
	"strconv"
	"strings"
)

// SQL that reads the metrics schema must run on two kinds of datastore
// (issue #755). On plain PostgreSQL every metrics.* relation is a
// partitioned table; on a pgEdge ColdFront datastore it is a view over
// Apache Iceberg storage, and pg_duckdb hands every query that reads one
// to DuckDB. DuckDB plans the statement but PostgreSQL has already
// described its result columns, so the two must agree, and DuckDB cannot
// type some parameter shapes at all. The server has no setting for the
// datastore kind, so metrics SQL is written in shapes that are correct
// on both:
//
//   - An expression whose PostgreSQL type differs from what DuckDB
//     computes is cast to a type both agree on, by a cast that changes
//     its PostgreSQL type (a cast to the type PostgreSQL already infers
//     is a no-op and does not help). avg() and EXTRACT(EPOCH ...) are
//     numeric to PostgreSQL but DOUBLE in DuckDB, so they are cast to
//     float8; sum() of an integer column is bigint to PostgreSQL but
//     HUGEINT in DuckDB, so the column is cast to bigint first, which
//     makes the sum numeric in both. An integer or numeric division is
//     a DOUBLE in DuckDB, so one operand is cast to float8 first.
//   - A list of values is never bound as one array parameter, which
//     DuckDB cannot type: integers are bound as one comma-separated text
//     parameter expanded by string_to_array (IntArrayExpr), and text
//     values as individually typed parameters.
//   - A parameter is never an operand of arithmetic, such as a bound
//     timestamp minus an interval; the bound is computed in Go and bound
//     as a plain value. Parameters compared directly with a column, and
//     the arguments of date_bin and generate_series, are fine.
//
// TestMetricsSQLAvoidsColdFrontShapes guards against the shapes known to
// fail, and .claude/golang-expert/metrics-queries.md records the rules.

// IntArrayExpr returns an integer[] expression built from placeholder, a
// bound text parameter holding comma-separated integers as IntListArg
// renders them. Compare a column with it as "col = ANY(IntArrayExpr(...))".
// DuckDB cannot type an array parameter, or an IN list of more than one
// integer parameter, but it can type one text parameter, so this shape
// works on a ColdFront datastore as well as on plain PostgreSQL.
func IntArrayExpr(placeholder string) string {
	return "string_to_array(" + placeholder + ", ',')::integer[]"
}

// IntListArg renders ids as the comma-separated text IntArrayExpr expects.
// An empty list renders as the empty string, which string_to_array turns
// into an empty array that matches nothing. The values are formatted from
// Go integers, so the parameter can hold nothing but digits, minus signs
// and commas.
func IntListArg(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

// BigIntArrayExpr is IntArrayExpr for values that may exceed the integer
// range, such as OIDs, which are unsigned 32-bit on plain PostgreSQL.
// Compare a column cast to bigint with it, as
// "col::bigint = ANY(BigIntArrayExpr(...))", since the column is oid on
// plain PostgreSQL and bigint on a ColdFront datastore.
func BigIntArrayExpr(placeholder string) string {
	return "string_to_array(" + placeholder + ", ',')::bigint[]"
}

// Int64ListArg renders ids as the comma-separated text BigIntArrayExpr
// expects, with the same guarantees as IntListArg.
func Int64ListArg(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

// isIntegerType reports whether dataType, as information_schema names it,
// is an integer type narrower than bigint, whose sum PostgreSQL types as
// bigint but DuckDB computes as HUGEINT.
func isIntegerType(dataType string) bool {
	return dataType == "integer" || dataType == "smallint"
}

// AggregateExpr returns aggregation (avg, sum, min, max or count) applied
// to expr, a column of type dataType, in a form whose result PostgreSQL
// and DuckDB type alike. avg is cast to float8, except over an interval,
// whose average is an interval in both; sum of an integer column sums it
// as bigint, so the result is numeric in both. DuckDB has no sum over an
// interval, so an interval is summed as seconds and turned back into an
// interval; the total is the same, though PostgreSQL then carries it all
// in the time field ("26:00:00") where the bare sum would keep a day
// field the inputs had ("1 day 02:00:00"). min, max and count keep the
// type of their input and need nothing. aggregation must already be a
// validated name.
func AggregateExpr(aggregation, expr, dataType string) string {
	switch aggregation {
	case "avg":
		if dataType == "interval" {
			return "avg(" + expr + ")"
		}
		return "avg(" + expr + ")::float8"
	case "sum":
		switch {
		case isIntegerType(dataType):
			return "sum(" + expr + "::bigint)"
		case dataType == "interval":
			return "(sum(" + EpochSeconds(expr) + ") * INTERVAL '1 second')"
		}
		return "sum(" + expr + ")"
	default:
		return aggregation + "(" + expr + ")"
	}
}

// EpochSeconds returns the number of seconds in the interval expression
// expr as float8. EXTRACT(EPOCH ...) is numeric to PostgreSQL but DOUBLE
// in DuckDB, so it is cast to float8 for both to agree.
func EpochSeconds(expr string) string {
	return "EXTRACT(EPOCH FROM " + expr + ")::float8"
}
