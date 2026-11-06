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
	"context"
	"testing"
)

// TestQueryDatastoreAggregateFormatIntegration pins how query_datastore
// prints aggregates on plain PostgreSQL now that they take forms a pgEdge
// ColdFront datastore can also type (metrics.AggregateExpr, issue #755).
// avg() is cast to float8, and a float8 must still print as the numeric
// avg() did, in plain decimal rather than as 1.431655765e+09. A sum of
// intervals is computed in seconds, since DuckDB has no sum(interval),
// so the total carries no day field: PostgreSQL's own sum printed
// '1 day 26:00:00.100003' for these rows, and this prints the same
// duration as '50:00:00.100003' (see .claude/golang-expert/metrics-queries.md).
func TestQueryDatastoreAggregateFormatIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `
        CREATE TABLE metrics.pg_sys_memory_info (
            connection_id INTEGER NOT NULL,
            collected_at TIMESTAMPTZ NOT NULL,
            n INTEGER,
            lag INTERVAL
        )`); err != nil {
		t.Fatalf("create probe: %v", err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DROP TABLE IF EXISTS metrics.pg_sys_memory_info`); err != nil {
			t.Logf("drop probe: %v", err)
		}
	})
	if _, err := f.pool.Exec(ctx, `
        INSERT INTO metrics.pg_sys_memory_info VALUES
            ($1, now() - interval '20 minutes', 1431655764, '1 day 02:00:00'),
            ($1, now() - interval '10 minutes', 1431655766, '24:00:00.100003')`, f.visible); err != nil {
		t.Fatalf("seed probe: %v", err)
	}

	tests := []struct {
		aggregation, column, output, want string
	}{
		{"avg", "n", "avg_n", "1431655765"},
		{"sum", "n", "sum_n", "2863311530"},
		{"sum", "lag", "sum_lag", "50:00:00.100003"},
	}
	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			rows := mustQuery(t, f.tool(), f.userCtx(), map[string]any{
				"source": "pg_sys_memory_info", "shape": "aggregate",
				"aggregation": tt.aggregation, "columns": []any{tt.column}})
			if len(rows) != 1 || rows[0][tt.output] != tt.want {
				t.Errorf("%s(%s) = %v, want %s %q", tt.aggregation, tt.column, rows, tt.output, tt.want)
			}
		})
	}
}
