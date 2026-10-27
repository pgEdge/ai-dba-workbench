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
	"slices"
	"testing"
	"time"

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

// entityViewStore holds the rows behind the pg_stat_database view
// TestQueryTimeSeries_ViewRatesPerEntity reads. The view takes the real
// probe's name, because the entity-key fallback is keyed by it.
const (
	entityViewStore = "pg_stat_database_cf_store"
	entityViewProbe = "pg_stat_database"
)

// setupEntityKeyedView creates metrics.pg_stat_database as a view, with
// no primary key, over a table holding three databases' xact_commit
// counters. appdb commits 60 a minute (1/s) and reportdb 120 a minute
// (2/s) from six minutes back; archivedb arrives two minutes back with a
// lifetime total of 500000 and does not move. Differenced per database
// every rate is 3/s; summed first, archivedb's arrival is a 500000 spike.
// The returned cleanup drops the store, which cascades to the view; the
// caller defers it, so it runs before the deferred pool close (a
// t.Cleanup would run after it, on a closed pool, and leave the view
// behind to break other packages' pg_stat_database fixtures).
func setupEntityKeyedView(t *testing.T, pool *pgxpool.Pool) (time.Time, func()) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		t.Fatalf("failed to create metrics schema: %v", err)
	}
	dropTable(ctx, pool, entityViewStore)
	cleanup := func() { dropTable(context.Background(), pool, entityViewStore) }
	setProbeIntervalForTest(t, pool, entityViewProbe, 0, 60)

	ddl := `CREATE TABLE metrics."` + entityViewStore + `" (
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        database_name text NOT NULL,
        xact_commit   bigint
    )`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("failed to create the view's store: %v", err)
	}
	createProbeView(t, pool, entityViewProbe, entityViewStore)

	now := time.Now().UTC().Truncate(time.Minute)
	insert := `INSERT INTO metrics."` + entityViewStore + `"
        (connection_id, collected_at, database_name, xact_commit)
        VALUES ($1, $2, $3, $4)`
	for i := 0; i <= 6; i++ {
		at := now.Add(-time.Duration(6-i) * time.Minute)
		rows := []struct {
			db    string
			value int64
		}{
			{"appdb", int64(1000 + 60*i)},
			{"reportdb", int64(2000 + 120*i)},
		}
		if i >= 4 {
			rows = append(rows, struct {
				db    string
				value int64
			}{"archivedb", 500000})
		}
		for _, r := range rows {
			if _, err := pool.Exec(ctx, insert, 1, at, r.db, r.value); err != nil {
				cleanup()
				t.Fatalf("failed to seed %s at %s: %v", r.db, at, err)
			}
		}
	}
	return now, cleanup
}

// TestQueryTimeSeries_ViewRatesPerEntity checks that a probe stored as a
// view, which has no primary key (as on a pgEdge ColdFront datastore,
// issue #755), takes its entity keys from probeEntityKeyFallback, so its
// counter rates are differenced per database.
func TestQueryTimeSeries_ViewRatesPerEntity(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	// Registered first so it runs last, after setProbeIntervalForTest's
	// t.Cleanup has removed the pg_stat_database interval row.
	t.Cleanup(closePool)
	base, cleanup := setupEntityKeyedView(t, pool)
	defer cleanup()
	ctx := context.Background()

	t.Run("a view without a primary key gets the fallback keys", func(t *testing.T) {
		keys, hasKey, err := primaryKeyEntityColumns(ctx, pool, entityViewProbe)
		if err != nil || hasKey || len(keys) != 0 {
			t.Fatalf("primaryKeyEntityColumns on the view = %v, %v, %v; want no key", keys, hasKey, err)
		}
		got, err := GetProbeEntityKeyColumns(ctx, pool, entityViewProbe)
		if err != nil {
			t.Fatalf("GetProbeEntityKeyColumns: %v", err)
		}
		if want := probeEntityKeyFallback[entityViewProbe]; !slices.Equal(got, want) {
			t.Errorf("entity keys = %q, want the fallback %q", got, want)
		}
	})

	t.Run("an unknown view without a primary key has no keys", func(t *testing.T) {
		got, err := GetProbeEntityKeyColumns(ctx, pool, entityViewStore)
		if err != nil || len(got) != 0 {
			t.Errorf("GetProbeEntityKeyColumns(%s) = %q, %v; want none", entityViewStore, got, err)
		}
	})

	t.Run("rates are differenced per database", func(t *testing.T) {
		series, err := queryTimeSeriesData(ctx, pool, entityViewProbe,
			[]int{1}, windowSince(base, 60), MetricFilters{}, 60, "avg",
			[]string{"xact_commit_per_sec"})
		if err != nil {
			t.Fatalf("QueryTimeSeries on the view: %v", err)
		}
		s := seriesByMetric(t, series, "xact_commit_per_sec")
		for offset := -5; offset <= 0; offset++ {
			at := base.Add(time.Duration(offset) * time.Minute)
			if got := pointAt(t, s, at); got != 3 {
				t.Errorf("rate at %d min = %v, want 3 (1/s + 2/s, nothing from archivedb's arrival)", offset, got)
			}
		}
	})
}

func TestGetProbeEntityKeyColumns_QueryError(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GetProbeEntityKeyColumns(ctx, pool, entityViewProbe); err == nil {
		t.Error("GetProbeEntityKeyColumns with a canceled context should fail, not fall back")
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
