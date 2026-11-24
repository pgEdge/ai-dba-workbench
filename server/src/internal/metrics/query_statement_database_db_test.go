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
	"database/sql"
	"testing"
	"time"
)

// TestStatementDatabaseFallbackAfterCopies_Integration checks that the
// database_name fallback for a statement whose dbid no name resolves is
// applied to the copy of each reading the builders keep, and for a rate
// to each interval's ending reading after the LAG, so that neither a
// discarded copy nor a predecessor read through another database decides
// which database the statement's readings count under.
func TestStatementDatabaseFallbackAfterCopies_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	ctx := context.Background()

	dropTable(ctx, pool, "pg_stat_statements")
	t.Cleanup(func() { dropTable(context.Background(), pool, "pg_stat_statements") })

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	t0 := start.Add(10 * time.Minute)
	t1 := t0.Add(time.Minute)
	// dbid 300 is unresolved. Statement 1 has a copy through alpha and
	// one through beta at t0, then a reading through beta at t1.
	// Statement 2 was read through alpha at t0 and through beta at t1.
	if _, err := pool.Exec(ctx, `
        CREATE SCHEMA IF NOT EXISTS metrics;
        CREATE TABLE metrics.pg_stat_statements (connection_id integer,
            collected_at timestamptz, queryid bigint, userid bigint, dbid bigint,
            toplevel boolean, database_name text, calls bigint)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pool.Exec(ctx, `
        INSERT INTO metrics.pg_stat_statements VALUES
            (1, $1, 1, 10, 300, true, 'alpha', 100),
            (1, $1, 1, 10, 300, true, 'beta', 100),
            (1, $2, 1, 10, 300, true, 'beta', 160),
            (1, $1, 2, 10, 300, true, 'alpha', 100),
            (1, $2, 2, 10, 300, true, 'beta', 160)`, t0, t1); err != nil {
		t.Fatalf("seed: %v", err)
	}

	filter := func(name string, queryID int64) MetricFilters {
		return MetricFilters{
			QueryID: &queryID,
			StatementDatabase: &StatementDatabaseFilter{
				Name: name, DBIDs: []int64{}, ResolvedDBIDs: []int64{100, 200},
			},
		}
	}
	scalar := func(outer, query string, args []any) sql.NullFloat64 {
		t.Helper()
		var v sql.NullFloat64
		if err := pool.QueryRow(ctx, outer+" FROM ("+query+") q", args...).Scan(&v); err != nil {
			t.Fatalf("query: %v\n%s", err, query)
		}
		return v
	}

	// The t0 reading's kept copy is the alpha one, so only alpha counts
	// it, and beta counts only its own reading at t1.
	for _, tt := range []struct {
		name string
		want float64
	}{{"alpha", 100}, {"beta", 160}} {
		query, args, err := BuildMetricsQuery("pg_stat_statements", []string{"calls"},
			map[string]string{"calls": "bigint"}, 1, start, end, 1, "sum", filter(tt.name, 1))
		if err != nil {
			t.Fatalf("BuildMetricsQuery: %v", err)
		}
		if got := scalar("SELECT SUM(calls)::float8", query, args); !got.Valid || got.Float64 != tt.want {
			t.Errorf("calls through %s = %v, want %v", tt.name, got, tt.want)
		}
	}

	// The interval ending at t1 belongs to beta, which read it, and keeps
	// its predecessor although alpha read that one.
	for _, tt := range []struct {
		name  string
		valid bool
	}{{"beta", true}, {"alpha", false}} {
		query, args, err := BuildDerivedMetricsQuery("pg_stat_statements",
			[]DerivedMetric{{OutputName: "calls_per_sec", BaseColumn: "calls", Kind: DerivedPerSec}},
			[]string{"database_name", "queryid", "userid", "dbid", "toplevel"},
			nil, 1, start, end, 1, "avg", filter(tt.name, 2), testMaxElapsed)
		if err != nil {
			t.Fatalf("BuildDerivedMetricsQuery: %v", err)
		}
		got := scalar("SELECT MAX(calls_per_sec)::float8", query, args)
		if got.Valid != tt.valid || (tt.valid && got.Float64 != 1) {
			t.Errorf("calls per second through %s = %v, want valid %v and 1", tt.name, got, tt.valid)
		}
	}
}
