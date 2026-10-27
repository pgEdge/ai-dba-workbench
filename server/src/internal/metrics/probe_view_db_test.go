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
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// On a pgEdge ColdFront datastore every metrics.* relation is a view over
// Apache Iceberg storage rather than a partitioned table (issue #755).
// These tests stand a plain view in for a ColdFront one, which is enough
// to prove that probe discovery accepts views: the catalog reports both
// the same way (relkind 'v', table_type VIEW).

// createProbeView creates metrics.<view> as SELECT * FROM metrics.<table>.
// The view is dropped with the table, whose fixture cleanup cascades.
func createProbeView(t *testing.T, pool *pgxpool.Pool, view, table string) {
	t.Helper()
	// Both names are compile-time test constants, QuoteIdentifier-wrapped;
	// CREATE VIEW cannot bind an identifier as a placeholder.
	// nosemgrep: go_sql_rule-concat-sqli
	ddl := "CREATE VIEW metrics." + QuoteIdentifier(view) +
		" AS SELECT * FROM metrics." + QuoteIdentifier(table)
	if _, err := pool.Exec(context.Background(), ddl); err != nil {
		t.Fatalf("failed to create probe view %s: %v", view, err)
	}
}

func TestProbeExistsQuery_AcceptsViews(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	cleanup := setupLatestRowsFixture(t, pool)
	defer cleanup()

	const view = latestRowsTestProbe + "_cf"
	createProbeView(t, pool, view, latestRowsTestProbe)

	ctx := context.Background()
	tests := []struct {
		name  string
		probe string
		want  int
	}{
		{"partitioned or plain table", latestRowsTestProbe, 1},
		{"ColdFront view", view, 1},
		{"missing probe", "does_not_exist_probe", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got int
			if err := pool.QueryRow(ctx, ProbeExistsQuery, tt.probe).Scan(&got); err != nil {
				t.Fatalf("ProbeExistsQuery: %v", err)
			}
			if got != tt.want {
				t.Errorf("ProbeExistsQuery(%s) = %d, want %d", tt.probe, got, tt.want)
			}
		})
	}
}

func TestQueryLatestRows_View(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	cleanup := setupLatestRowsFixture(t, pool)
	defer cleanup()

	const view = latestRowsTestProbe + "_cf"
	createProbeView(t, pool, view, latestRowsTestProbe)

	ctx := context.Background()
	fromTable, err := QueryLatestRows(ctx, pool, latestRowsTestProbe,
		[]int{1}, MetricFilters{}, "", "desc", 10)
	if err != nil {
		t.Fatalf("QueryLatestRows on the table: %v", err)
	}
	fromView, err := QueryLatestRows(ctx, pool, view,
		[]int{1}, MetricFilters{}, "", "desc", 10)
	if err != nil {
		t.Fatalf("QueryLatestRows on the view: %v", err)
	}
	if len(fromView) == 0 || len(fromView) != len(fromTable) {
		t.Errorf("view returned %d rows, table %d; want the same non-zero count",
			len(fromView), len(fromTable))
	}
}

func TestQueryTimeSeries_View(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	_, cleanup := setupTimeSeriesFixture(t, pool)
	defer cleanup()

	const view = timeSeriesTestProbe + "_cf"
	createProbeView(t, pool, view, timeSeriesTestProbe)

	series, err := queryTimeSeriesData(context.Background(), pool, view,
		[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg", []string{"n_live_tup"})
	if err != nil {
		t.Fatalf("QueryTimeSeries on the view: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(series))
	}
	points := nonNull(series[0])
	if len(points) == 0 {
		t.Fatal("expected at least one non-null point from the view")
	}
	for _, p := range points {
		if *p.Value != 90 {
			t.Errorf("n_live_tup point = %v, want 90", *p.Value)
		}
	}
}
