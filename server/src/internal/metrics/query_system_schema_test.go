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
	"strings"
	"testing"
	"time"
)

// TestMetricQueryClausesExcludeSystemSchemas covers the exclude_system_schemas
// filter of the bucketed query builders: the predicate is a constant that
// binds nothing, so it must not disturb the $N layout of the other filters.
func TestMetricQueryClausesExcludeSystemSchemas(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	t.Run("adds the predicate without binding an argument", func(t *testing.T) {
		parts := metricQueryClauses("pg_stat_all_tables", 1, start, end,
			time.Minute, MetricFilters{
				DatabaseName:         "northwind",
				DatabaseColumn:       "database_name",
				ExcludeSystemSchemas: true,
			})
		where := parts.where()
		if !strings.Contains(where, SystemSchemaPredicate) {
			t.Errorf("where() missing the system-schema predicate: %q", where)
		}
		if !strings.Contains(where, `"database_name" = $5`) {
			t.Errorf("database filter should keep $5: %q", where)
		}
		if len(parts.args) != 5 {
			t.Errorf("expected 5 args, got %d: %v", len(parts.args), parts.args)
		}
	})

	t.Run("omitted adds nothing", func(t *testing.T) {
		parts := metricQueryClauses("pg_stat_all_tables", 1, start, end,
			time.Minute, MetricFilters{})
		if strings.Contains(parts.where(), "pg_toast") {
			t.Errorf("unexpected system-schema predicate: %q", parts.where())
		}
	})

	t.Run("raw column query is filtered", func(t *testing.T) {
		query, _, err := BuildMetricsQuery("pg_stat_all_tables",
			[]string{"n_live_tup"}, map[string]string{"n_live_tup": "bigint"},
			1, start, end, 60, "last",
			MetricFilters{ExcludeSystemSchemas: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(query, SystemSchemaPredicate) {
			t.Errorf("query missing the system-schema predicate:\n%s", query)
		}
	})

	t.Run("latest rows query is filtered", func(t *testing.T) {
		query, args := buildLatestRowsQuery("pg_stat_all_tables",
			[]string{"schemaname", "relname", "n_live_tup"},
			map[string]string{
				"schemaname": "name", "relname": "name", "n_live_tup": "bigint",
			},
			[]int{1}, MetricFilters{ExcludeSystemSchemas: true},
			"n_live_tup", "desc", 10)
		if !strings.Contains(query, SystemSchemaPredicate) {
			t.Errorf("query missing the system-schema predicate:\n%s", query)
		}
		// The connection list and the limit only.
		if len(args) != 2 {
			t.Errorf("expected 2 args, got %d: %v", len(args), args)
		}
	})
}

// TestDeadTupleRatioTotals_Integration checks the dead-tuple ratio a
// database-wide request returns against a probe holding several relations
// per sample (issue #869). Before the fix, "last" picked one arbitrary
// relation's counts, so a database with millions of dead tuples could read
// 0%; it must instead total the relations of the latest sample, and with
// ExcludeSystemSchemas count only user relations, as pg_stat_user_tables
// does.
func TestDeadTupleRatioTotals_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	ctx := context.Background()

	const probe = "pg_stat_all_tables_ratio_test"
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		t.Fatalf("failed to create metrics schema: %v", err)
	}
	dropTable(ctx, pool, probe)
	defer dropTable(context.Background(), pool, probe)
	registerFixtureProbeForTest(t, probe)
	setProbeIntervalForTest(t, pool, probe, 0, 60)

	if _, err := pool.Exec(ctx, `CREATE TABLE metrics."`+probe+`" (
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        database_name text,
        schemaname    name,
        relname       name,
        n_live_tup    bigint,
        n_dead_tup    bigint
    )`); err != nil {
		t.Fatalf("failed to create fixture table: %v", err)
	}

	// Two samples a minute apart. In the latest one the user tables hold
	// 900 live and 100 dead tuples (10%), a catalog and a TOAST relation
	// add 1000 live and 900 dead, and a tiny user table with no dead
	// tuples is the kind of row the old "last" could land on alone. The
	// earlier sample has a different ratio, so a bucket width of one
	// sample shows the latest sample's totals are what is reported.
	base := time.Now().UTC().Truncate(time.Minute)
	latest := base.Add(-time.Minute)
	earlier := base.Add(-2 * time.Minute)
	type row struct {
		at         time.Time
		db, schema string
		rel        string
		live, dead int64
	}
	rows := []row{
		{earlier, "northwind", "public", "orders", 500, 500},
		{latest, "northwind", "public", "orders", 700, 100},
		{latest, "northwind", "sales", "regions", 200, 0},
		{latest, "northwind", "pg_catalog", "pg_class", 600, 500},
		{latest, "northwind", "pg_toast", "pg_toast_1234", 400, 400},
		{latest, "other", "public", "orders", 0, 999},
	}
	for i, r := range rows {
		if _, err := pool.Exec(ctx, `INSERT INTO metrics."`+probe+`"
            (connection_id, collected_at, database_name, schemaname, relname,
             n_live_tup, n_dead_tup) VALUES (1, $1, $2, $3, $4, $5, $6)`,
			r.at, r.db, r.schema, r.rel, r.live, r.dead); err != nil {
			t.Fatalf("failed to insert fixture row %d: %v", i, err)
		}
	}

	window := TimeWindow{Start: base.Add(-time.Hour), End: base}
	latestRatio := func(t *testing.T, filters MetricFilters) float64 {
		t.Helper()
		series, err := queryTimeSeriesData(ctx, pool, probe, []int{1},
			window, filters, 30, "last", []string{"dead_tuple_ratio"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(series) != 1 {
			t.Fatalf("expected 1 series, got %d", len(series))
		}
		points := nonNull(series[0])
		if len(points) == 0 {
			t.Fatal("expected at least one ratio point")
		}
		return *points[len(points)-1].Value
	}
	near := func(got, want float64) bool { return got > want-0.01 && got < want+0.01 }

	t.Run("user relations only", func(t *testing.T) {
		got := latestRatio(t, MetricFilters{
			DatabaseName: "northwind", ExcludeSystemSchemas: true,
		})
		// (100 + 0) / (700 + 100 + 200 + 0) = 10%.
		if !near(got, 10) {
			t.Errorf("dead_tuple_ratio = %v, want 10", got)
		}
	})

	t.Run("all relations without the exclusion", func(t *testing.T) {
		got := latestRatio(t, MetricFilters{DatabaseName: "northwind"})
		// (100 + 0 + 500 + 400) / (800 + 200 + 1100 + 800) = 34.48%.
		if !near(got, 1000.0/2900.0*100) {
			t.Errorf("dead_tuple_ratio = %v, want %v", got, 1000.0/2900.0*100)
		}
	})

	t.Run("refused for a probe without schemaname", func(t *testing.T) {
		const other = "pg_stat_database_ratio_test"
		dropTable(ctx, pool, other)
		defer dropTable(context.Background(), pool, other)
		registerFixtureProbeForTest(t, other)
		if _, err := pool.Exec(ctx, `CREATE TABLE metrics."`+other+`" (
            connection_id integer NOT NULL,
            collected_at  timestamp with time zone NOT NULL,
            database_name text,
            numbackends   bigint
        )`); err != nil {
			t.Fatalf("failed to create fixture table: %v", err)
		}
		_, err := QueryTimeSeries(ctx, pool, other, []int{1}, window,
			MetricFilters{ExcludeSystemSchemas: true}, 30, "last", nil)
		if err == nil || !strings.Contains(err.Error(), "exclude_system_schemas") {
			t.Errorf("expected an exclude_system_schemas error, got %v", err)
		}
		_, err = QueryLatestRows(ctx, pool, other, []int{1},
			MetricFilters{ExcludeSystemSchemas: true}, "", "desc", 1)
		if err == nil || !strings.Contains(err.Error(), "exclude_system_schemas") {
			t.Errorf("expected an exclude_system_schemas error, got %v", err)
		}
	})
}
