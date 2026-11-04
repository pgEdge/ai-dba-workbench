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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/testutil/coldfrontcheck"
)

// coldFrontShapeSource is a metric source with a column of every type
// query_datastore treats differently, so each filter and aggregation is
// built for each type.
func coldFrontShapeSource() datastoreSource {
	src := testMetricSource()
	src.Columns = append(slices.Clone(src.Columns),
		col("backend_type", typeText),
		col("state", typeVarchar),
		metricCol("numbackends", "smallint"),
		metricCol("conflicts", typeInteger),
		metricCol("blk_read_time", typeReal),
		metricCol("temp_bytes", "numeric"),
		metricCol("write_lag", "interval"),
		col("is_template", typeBoolean),
	)
	return src
}

// coldFrontFilterValues gives two filter values a column of each type
// accepts.
var coldFrontFilterValues = map[string][]any{
	typeInteger:        {float64(3), "7"},
	"smallint":         {float64(1), float64(2)},
	typeBigint:         {float64(10), "11"},
	typeReal:           {0.5, "1.5"},
	"double precision": {0.25, 0.75},
	"numeric":          {float64(100), "200"},
	"interval":         {"1 minute", "2 minutes"},
	typeBoolean:        {true, "false"},
	typeTimestamptz:    {"2026-10-01T00:00:00Z", "2026-10-02T00:00:00Z"},
	typeText:           {"client backend", "walsender"},
	typeVarchar:        {"active", "idle"},
	"name":             {"app", "postgres"},
}

// coldFrontShapeRequests returns a request for every shape, every
// aggregation over every column it accepts, and every in and not in
// filter on every column, each named for its failure message.
func coldFrontShapeRequests(t *testing.T, src datastoreSource) map[string]map[string]any {
	t.Helper()
	reqs := map[string]map[string]any{
		"rows":   {"source": src.Name},
		"latest": {"source": src.Name, "shape": "latest", "columns": []any{"datname", "xact_commit"}},
		"paged rows": {"source": src.Name, "time_start": "4h", "page": float64(3), "page_size": float64(10),
			"order_by": []any{map[string]any{"column": "xact_commit", "direction": "DESC"}}},
		"named connections": {"source": src.Name, "connection_ids": []any{float64(3), float64(7)}},
	}
	for agg := range datastoreAggregations {
		for _, c := range src.Columns {
			if (agg == "avg" || agg == "sum") && !c.IsMetric {
				continue
			}
			if (agg == "min" || agg == "max") && (c.DataType == typeBoolean || c.DataType == typeJSONB) {
				continue
			}
			base := map[string]any{"source": src.Name, "shape": "aggregate", "aggregation": agg,
				"columns": []any{c.Name}}
			reqs[fmt.Sprintf("aggregate %s(%s %s)", agg, c.Name, c.DataType)] = base
			group := "datname"
			if c.Name == group {
				group = "backend_type"
			}
			reqs[fmt.Sprintf("bucketed %s(%s %s) by %s", agg, c.Name, c.DataType, group)] = map[string]any{
				"source": src.Name, "shape": "aggregate", "aggregation": agg, "columns": []any{c.Name},
				"buckets": float64(12), "group_by": []any{group}, "time_start": "6h"}
		}
	}
	reqs["count by group"] = map[string]any{"source": src.Name, "shape": "aggregate", "aggregation": "count",
		"group_by": []any{"datname", "connection_id"}}
	for _, c := range src.Columns {
		values, ok := coldFrontFilterValues[c.DataType]
		if !ok {
			t.Fatalf("no filter values for %s column %s", c.DataType, c.Name)
		}
		for _, op := range []string{"in", "not in"} {
			for n := 1; n <= len(values); n++ {
				reqs[fmt.Sprintf("%s filter on %s %s with %d values", op, c.Name, c.DataType, n)] = map[string]any{
					"source": src.Name, "shape": "rows",
					"filters": []any{map[string]any{"column": c.Name, "op": op, "values": values[:n]}}}
			}
		}
	}
	return reqs
}

// integerSum matches a sum, for the check that an integer column is
// summed as bigint.
var integerSum = regexp.MustCompile(`(?i)\bsum\(\s*t\."([a-z_]+)"(::bigint)?\s*\)`)

// TestDatastoreQueryAvoidsColdFrontShapes runs the statement
// query_datastore builds for each request shape, aggregation, list
// filter and column type through the ColdFront shape check
// (coldfrontcheck.ShapeProblems), for an unrestricted caller and for one
// limited to some connections. TestMetricsSQLAvoidsColdFrontShapes scans
// source literals and cannot see this SQL, which the builder assembles
// at run time with the table name and columns spliced in (issue #755).
// The check cannot see column types, so this test also requires an
// integer or smallint column to be summed as bigint, which this test
// knows the type of.
func TestDatastoreQueryAvoidsColdFrontShapes(t *testing.T) {
	src := coldFrontShapeSource()
	reqs := coldFrontShapeRequests(t, src)
	if len(reqs) < 60 {
		t.Fatalf("only %d requests built; the generator is broken", len(reqs))
	}
	types := map[string]string{}
	for _, c := range src.Columns {
		types[c.Name] = c.DataType
	}
	for name, args := range reqs {
		for visName, vis := range map[string]visibleSet{"everyone": everyone, "restricted": restricted} {
			sql, _, err := buildDatastoreQuery(src, mustParse(t, args), vis, testNow)
			if err != nil {
				t.Errorf("%s (%s): %v", name, visName, err)
				continue
			}
			for _, p := range coldfrontcheck.ShapeProblems(sql) {
				if intervalAverage(p, types) {
					continue
				}
				t.Errorf("%s (%s): %s\n%s", name, visName, p, sql)
			}
			for _, m := range integerSum.FindAllStringSubmatch(sql, -1) {
				if dt := types[m[1]]; (dt == typeInteger || dt == "smallint") && m[2] == "" {
					t.Errorf("%s (%s): %q sums a %s column, which DuckDB sums as HUGEINT; sum it as bigint",
						name, visName, m[0], dt)
				}
			}
		}
	}
}

// intervalAverage reports whether problem is the shape check's finding
// on an uncast avg over an interval column. AggregateExpr leaves that avg
// uncast on purpose, because an interval's average is an interval to
// PostgreSQL and DuckDB alike; the check cannot see the column's type,
// so it flags every uncast avg.
func intervalAverage(problem string, types map[string]string) bool {
	for name, dt := range types {
		if dt == "interval" && strings.HasPrefix(problem, strconv.Quote("avg(t."+strconv.Quote(name)+")")) {
			return true
		}
	}
	return false
}

// TestDatastoreQueryAvoidsColdFrontShapesSeesAggregates checks that the
// test above would catch an aggregate written without AggregateExpr: the
// SQL an unwrapped avg and integer sum would give is flagged.
func TestDatastoreQueryAvoidsColdFrontShapesSeesAggregates(t *testing.T) {
	sql, _ := mustBuild(t, coldFrontShapeSource(), map[string]any{"source": "pg_stat_database",
		"shape": "aggregate", "aggregation": "avg", "columns": []any{"xact_commit"}}, everyone)
	unwrapped := strings.Replace(sql, `avg(t."xact_commit")::float8`, `avg(t."xact_commit")`, 1)
	if unwrapped == sql {
		t.Fatalf("the avg is not built through AggregateExpr:\n%s", sql)
	}
	if len(coldfrontcheck.ShapeProblems(unwrapped)) == 0 {
		t.Errorf("an uncast avg is not flagged:\n%s", unwrapped)
	}
	if m := integerSum.FindStringSubmatch(`SELECT sum(t."conflicts") AS "sum_conflicts"`); m == nil || m[2] != "" {
		t.Errorf("an uncast integer sum is not matched: %q", m)
	}
}
