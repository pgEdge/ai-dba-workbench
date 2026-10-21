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
	"slices"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/sqlredact"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// testStatementSource mirrors a discovered pg_stat_statements source.
func testStatementSource() datastoreSource {
	return datastoreSource{
		Name:             "pg_stat_statements",
		Table:            "pg_stat_statements",
		TimeColumn:       "collected_at",
		ConnectionColumn: "connection_id",
		Kind:             datastoreKindMetric,
		Scope:            scopeConnection,
		Columns: redactMetricColumns("pg_stat_statements", []datastoreColumn{
			col("connection_id", typeInteger),
			col("collected_at", typeTimestamptz),
			col("queryid", typeBigint),
			col("query", typeText),
			metricCol("calls", typeBigint),
		}),
	}
}

func TestRedactMetricColumnsMarksStatements(t *testing.T) {
	src := testStatementSource()
	for _, c := range src.Columns {
		if c.RedactStatement != (c.Name == "query") {
			t.Errorf("%s: RedactStatement = %v", c.Name, c.RedactStatement)
		}
	}
	// A row image is masked as text, but is not a statement to lex.
	got := redactMetricColumns("spock_exception_log", []datastoreColumn{
		col("local_tup", typeJSONB), col("ddl_statement", typeText), col("error_message", typeText),
	})
	for _, c := range got {
		if c.RedactStatement != (c.Name != "local_tup") {
			t.Errorf("spock_exception_log %s: RedactStatement = %v", c.Name, c.RedactStatement)
		}
	}
}

func TestDatastoreStatementOutputs(t *testing.T) {
	src := testStatementSource()
	for name, tc := range map[string]struct {
		args map[string]any
		want []string
	}{
		"rows, every column":   {map[string]any{}, []string{"query"}},
		"rows, chosen columns": {map[string]any{"columns": []any{"queryid", "calls"}}, nil},
		"latest":               {map[string]any{"shape": "latest", "columns": []any{"query"}}, []string{"query"}},
		"aggregate group_by": {map[string]any{"shape": "aggregate", "aggregation": "count",
			"group_by": []any{"queryid", "query"}}, []string{"query"}},
		"aggregate max": {map[string]any{"shape": "aggregate", "aggregation": "max",
			"columns": []any{"query", "calls"}}, []string{"max_query"}},
		"aggregate count": {map[string]any{"shape": "aggregate", "aggregation": "count",
			"columns": []any{"query"}}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			tc.args["source"] = src.Name
			got := datastoreStatementOutputs(src, mustParse(t, tc.args))
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBuildDatastoreQuerySelectsStoredStatements checks that every shape
// selects the stored text beside each output holding statement text,
// and that the stored text never reaches a filter or the grouping.
func TestBuildDatastoreQuerySelectsStoredStatements(t *testing.T) {
	src := testStatementSource()
	raw := metrics.QuoteIdentifier(metrics.RawStatementAlias("query"))
	inner := qualified(datastoreTableAlias, "query") + ` AS ` + raw
	for name, tc := range map[string]struct {
		args map[string]any
		want []string
	}{
		"rows":   {map[string]any{"columns": []any{"query"}}, []string{`SELECT t."query", t.` + raw + ` FROM`}},
		"latest": {map[string]any{"shape": "latest", "columns": []any{"query"}}, []string{`SELECT "query", ` + raw + ` FROM`}},
		"aggregate group_by": {map[string]any{"shape": "aggregate", "aggregation": "count", "group_by": []any{"query"}},
			[]string{`count(*) AS "count", min(t.` + raw + `) AS ` + raw + ` FROM`, "GROUP BY 1 "}},
		"aggregate min": {map[string]any{"shape": "aggregate", "aggregation": "min", "columns": []any{"query"}},
			[]string{`min(t."query") AS "min_query", min(t.` + raw + `) AS "_raw_statement_min_query" FROM`}},
		"aggregate max": {map[string]any{"shape": "aggregate", "aggregation": "max", "columns": []any{"query"}},
			[]string{`max(t."query") AS "max_query", max(t.` + raw + `) AS "_raw_statement_max_query" FROM`}},
	} {
		t.Run(name, func(t *testing.T) {
			tc.args["source"] = src.Name
			tc.args["filters"] = []any{map[string]any{"column": "query", "op": "like", "value": "%x%"}}
			sql, _ := mustBuild(t, src, tc.args, everyone)
			for _, want := range append(tc.want, inner) {
				if !strings.Contains(sql, want) {
					t.Errorf("SQL lacks %q:\n%s", want, sql)
				}
			}
			if strings.Contains(sql, raw+" LIKE") || strings.Contains(sql, raw+"::text LIKE") {
				t.Errorf("a filter reads the stored text:\n%s", sql)
			}
		})
	}

	// A caller cannot name the stored text.
	_, _, err := buildDatastoreQuery(src, mustParse(t, map[string]any{
		"source": src.Name, "columns": []any{metrics.RawStatementAlias("query")},
	}), everyone, testNow)
	if err == nil {
		t.Error("a column named as the stored text should be refused")
	}
}

func TestRedactDatastoreStatements(t *testing.T) {
	const leak = `ALTER ROLE app PASSWORD '\''WFLEAKsecret19'`
	raw := metrics.RawStatementAlias("query")
	qr := &queryResult{
		ColumnNames: []string{"queryid", "query", "max_query", raw},
		Rows: [][]any{
			{int64(1), "masked in SQL", "masked max", leak},
			{int64(2), nil, nil, nil},
			{int64(3), "masked in SQL", nil, int64(7)},
		},
		ResultsTSV: "stale",
	}
	redactDatastoreStatements(qr, []string{"query", "max_query", "absent"})

	if !slices.Equal(qr.ColumnNames, []string{"queryid", "query", "max_query"}) {
		t.Fatalf("columns = %v", qr.ColumnNames)
	}
	want := [][]any{
		{int64(1), metrics.RedactStatementText(leak), sqlredact.Redacted},
		{int64(2), nil, nil},
		{int64(3), sqlredact.Redacted, nil},
	}
	for i, row := range qr.Rows {
		if !slices.Equal(row, want[i]) {
			t.Errorf("row %d = %v, want %v", i, row, want[i])
		}
	}
	if strings.Contains(qr.ResultsTSV, "WFLEAKsecret19") || strings.Contains(qr.ResultsTSV, "masked in SQL") ||
		strings.Contains(qr.ResultsTSV, raw) || !strings.Contains(qr.ResultsTSV, "queryid\tquery\tmax_query") {
		t.Errorf("TSV not reformatted from the masked rows:\n%s", qr.ResultsTSV)
	}

	// A result with no stored text is left as it is, bar formatting.
	plain := &queryResult{ColumnNames: []string{"calls"}, Rows: [][]any{{int64(5)}}}
	redactDatastoreStatements(plain, nil)
	if !slices.Equal(plain.ColumnNames, []string{"calls"}) || plain.Rows[0][0] != int64(5) ||
		!strings.Contains(plain.ResultsTSV, "calls") {
		t.Errorf("unexpected result: %+v", plain)
	}
}
