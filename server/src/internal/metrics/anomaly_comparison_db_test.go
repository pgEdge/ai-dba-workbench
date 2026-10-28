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
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/pkg/anomaly"
)

const cpuMetric = "pg_sys_cpu_usage_info.processor_time_percent"

// TestLoadComparison_Integration seeds CPU samples, baselines and the
// alerter's published settings, and checks the comparison end to end:
// only the requested connection's samples inside the window come back,
// scored against the stored row with the published settings.
func TestLoadComparison_Integration(t *testing.T) {
	pool := newCollectorSchemaPool(t)
	ctx := context.Background()

	conn := insertTestConnection(t, pool, "comparison-a")
	other := insertTestConnection(t, pool, "comparison-b")
	now := time.Now().UTC().Truncate(time.Second)
	window := TimeWindow{Start: now.Add(-2 * time.Hour), End: now}

	insertCPU := func(connID int, at time.Time, pct float64) {
		t.Helper()
		execSchema(t, pool, `
            INSERT INTO metrics.pg_sys_cpu_usage_info
                (connection_id, processor_time_percent, collected_at)
            VALUES ($1, $2, $3)`, connID, pct, at)
	}
	insertCPU(conn, now.Add(-3*time.Hour), 99) // before the window
	insertCPU(conn, now.Add(-90*time.Minute), 40)
	insertCPU(conn, now.Add(-30*time.Minute), 90)
	// The other connection's sample sits in a bucket of its own, so a
	// series query that leaked it would return a third point.
	insertCPU(other, now.Add(-60*time.Minute), 77)

	req := ComparisonRequest{
		ConnectionID: conn,
		Metric:       cpuMetric,
		Model:        ComparisonModelAsScored,
		Window:       window,
		Buckets:      DefaultComparisonBuckets,
	}

	t.Run("defaults without baselines or settings", func(t *testing.T) {
		got, err := LoadComparison(ctx, pool, req, now)
		if err != nil {
			t.Fatalf("LoadComparison: %v", err)
		}
		if got.ScoringSource != ScoringSourceDefaults || got.Sensitivity != 3 ||
			got.BaselinesAsOf != nil || len(got.Points) != 2 {
			t.Fatalf("comparison = %+v, want defaults, no baselines, two points", got)
		}
		for _, p := range got.Points {
			if p.Cold || p.Outside || p.Mean != nil {
				t.Errorf("point %+v, want no band and not cold", p)
			}
		}
	})

	calculated := now.Add(-10 * time.Minute)
	execSchema(t, pool, `
        INSERT INTO metric_baselines
            (connection_id, database_name, metric_name, period_type,
             mean, stddev, min, max, sample_count, earliest_sample_at,
             last_calculated)
        VALUES ($1, NULL, $2, 'all', 40, 5, 0, 100, 1000, $3, $4),
               ($5, NULL, $2, 'all', 77, 1, 0, 100, 1000, $3, $3)`,
		conn, cpuMetric, now.AddDate(0, -1, 0), calculated, other)
	published := anomaly.DefaultScoringSettings()
	published.DefaultSensitivity = 4
	raw, err := json.Marshal(published)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	execSchema(t, pool, `UPDATE alerter_settings SET tier1_scoring = $1 WHERE id = 1`, raw)

	t.Run("scored against the published settings", func(t *testing.T) {
		got, err := LoadComparison(ctx, pool, req, now)
		if err != nil {
			t.Fatalf("LoadComparison: %v", err)
		}
		if got.ScoringSource != ScoringSourceAlerter || got.Sensitivity != 4 ||
			got.DefaultSensitivity != 4 || got.DatabaseName != nil {
			t.Errorf("comparison = %+v, want the alerter's sensitivity 4", got)
		}
		if got.BaselinesAsOf == nil || !got.BaselinesAsOf.Equal(calculated) {
			t.Errorf("baselines_as_of = %v, want %v", got.BaselinesAsOf, calculated)
		}
		if len(got.Points) != 2 {
			t.Fatalf("points = %+v, want two", got.Points)
		}
		low, high := got.Points[0], got.Points[1]
		if low.Value != 40 || low.Outside || low.Cold || low.ZScore == nil || *low.ZScore != 0 {
			t.Errorf("first point = %+v, want 40 in band", low)
		}
		if high.Value != 90 || !high.Outside || high.ZScore == nil || *high.ZScore != 10 {
			t.Errorf("second point = %+v, want 90 outside with z 10", high)
		}
		if !high.Time.Equal(now.Add(-30*time.Minute)) || high.Time.Location() != time.UTC {
			t.Errorf("second point time = %v", high.Time)
		}
	})

	t.Run("an explicit sensitivity overrides the default", func(t *testing.T) {
		wide := 10.0
		r := req
		r.Sensitivity = &wide
		got, err := LoadComparison(ctx, pool, r, now)
		if err != nil {
			t.Fatalf("LoadComparison: %v", err)
		}
		if got.Sensitivity != 10 || got.DefaultSensitivity != 4 || got.Points[1].Outside {
			t.Errorf("comparison = %+v, want sensitivity 10 with nothing outside", got)
		}
	})

	t.Run("malformed settings fall back to the defaults", func(t *testing.T) {
		execSchema(t, pool, `UPDATE alerter_settings SET tier1_scoring = '{"default_sensitivity":"x"}' WHERE id = 1`)
		got, err := LoadComparison(ctx, pool, req, now)
		if err != nil {
			t.Fatalf("LoadComparison: %v", err)
		}
		if got.ScoringSource != ScoringSourceDefaults || got.Sensitivity != 3 {
			t.Errorf("comparison = %+v, want the defaults", got)
		}
	})

	t.Run("invalid requests run no query", func(t *testing.T) {
		db := "postgres"
		for _, r := range []ComparisonRequest{
			{Metric: "pg_sys_cpu_usage_info.nope", Model: ComparisonModelAll},
			{Metric: cpuMetric, DatabaseName: &db, Model: ComparisonModelAll},
			{Metric: cpuMetric, Model: "overall"},
		} {
			cctx, cancel := context.WithCancel(ctx)
			cancel() // any query would fail with context.Canceled
			_, err := LoadComparison(cctx, pool, r, now)
			if err == nil || errors.Is(err, context.Canceled) {
				t.Errorf("request %+v: err = %v, want a validation error", r, err)
			}
		}
	})

	t.Run("query failures are returned", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := LoadComparison(cctx, pool, req, now); err == nil {
			t.Error("LoadComparison with an already-done context succeeded")
		}
		if _, _, err := QueryComparisonBaselines(cctx, pool, conn, cpuMetric, nil); err == nil {
			t.Error("QueryComparisonBaselines with an already-done context succeeded")
		}
		if _, err := QueryComparisonSeries(cctx, pool, conn, cpuMetric, nil, window, now); err == nil {
			t.Error("QueryComparisonSeries with an already-done context succeeded")
		}
		if _, err := QueryComparisonSeries(ctx, pool, conn, "nope", nil, window, now); !errors.Is(err, ErrUnknownComparisonMetric) {
			t.Errorf("unknown metric: err = %v", err)
		}
	})
}

// TestQueryComparisonSeries_EveryMetric runs the wrapped historical query
// of every baselined metric, so a registry query whose columns or shape
// cannot be wrapped fails here rather than in production.
func TestQueryComparisonSeries_EveryMetric(t *testing.T) {
	pool := newCollectorSchemaPool(t)
	ctx := context.Background()
	conn := insertTestConnection(t, pool, "comparison-every")
	now := time.Now().UTC()
	window := TimeWindow{Start: now.Add(-24 * time.Hour), End: now}
	db := "postgres"

	for _, def := range anomaly.BaselineMetrics() {
		t.Run(def.Name, func(t *testing.T) {
			var dbName *string
			if def.PerDatabase {
				dbName = &db
			}
			got, err := QueryComparisonSeries(ctx, pool, conn, def.Name, dbName, window, now)
			if err != nil {
				t.Fatalf("QueryComparisonSeries: %v", err)
			}
			if got == nil {
				t.Error("QueryComparisonSeries returned nil, want an empty slice")
			}
		})
	}
}

const deadlocksMetric = "pg_stat_database.deadlocks_delta"

// TestLoadComparison_PerDatabase seeds deadlock samples and baselines for
// two databases on one connection and checks that a comparison for one
// database sees only that database's samples and band.
func TestLoadComparison_PerDatabase(t *testing.T) {
	pool := newCollectorSchemaPool(t)
	ctx := context.Background()

	conn := insertTestConnection(t, pool, "comparison-per-db")
	now := time.Now().UTC().Truncate(time.Second)
	window := TimeWindow{Start: now.Add(-3 * time.Hour), End: now}
	// Both samples fall in one UTC hour inside the window, so the
	// metric's hourly delta is the difference between them.
	hour := now.Truncate(time.Hour).Add(-time.Hour)

	insertDeadlocks := func(db string, at time.Time, n int) {
		t.Helper()
		execSchema(t, pool, `
            INSERT INTO metrics.pg_stat_database
                (connection_id, database_name, datname, deadlocks, collected_at)
            VALUES ($1, $2::text, $2::text, $3, $4)`, conn, db, n, at)
	}
	insertDeadlocks("db_a", hour.Add(10*time.Minute), 0)
	insertDeadlocks("db_a", hour.Add(20*time.Minute), 5)
	insertDeadlocks("db_b", hour.Add(10*time.Minute), 0)
	insertDeadlocks("db_b", hour.Add(20*time.Minute), 50)

	execSchema(t, pool, `
        INSERT INTO metric_baselines
            (connection_id, database_name, metric_name, period_type,
             mean, stddev, min, max, sample_count, earliest_sample_at,
             last_calculated)
        VALUES ($1, 'db_a', $2, 'all', 5, 2, 0, 20, 1000, $3, $4),
               ($1, 'db_b', $2, 'all', 100, 2, 0, 200, 1000, $3, $4)`,
		conn, deadlocksMetric, now.AddDate(0, -1, 0), now)

	for _, tc := range []struct {
		db      string
		value   float64
		mean    float64
		outside bool
	}{
		{"db_a", 5, 5, false},
		{"db_b", 50, 100, true},
	} {
		t.Run(tc.db, func(t *testing.T) {
			db := tc.db
			got, err := LoadComparison(ctx, pool, ComparisonRequest{
				ConnectionID: conn,
				Metric:       deadlocksMetric,
				DatabaseName: &db,
				Model:        ComparisonModelAll,
				Window:       window,
				Buckets:      DefaultComparisonBuckets,
			}, now)
			if err != nil {
				t.Fatalf("LoadComparison: %v", err)
			}
			if got.DatabaseName == nil || *got.DatabaseName != tc.db {
				t.Errorf("database_name = %v, want %s", got.DatabaseName, tc.db)
			}
			if len(got.Points) != 1 {
				t.Fatalf("points = %+v, want only %s's one hourly point", got.Points, tc.db)
			}
			p := got.Points[0]
			if p.Value != tc.value {
				t.Errorf("value = %v, want %v", p.Value, tc.value)
			}
			if p.Cold || p.Mean == nil || *p.Mean != tc.mean || p.Outside != tc.outside {
				t.Errorf("point = %+v, want %s's band (mean %v, outside %v)",
					p, tc.db, tc.mean, tc.outside)
			}
		})
	}
}

// TestComparisonSeriesQuery_Plan logs the plan of the wrapped series
// query for a CTE and LAG metric with a connection and database bound,
// to show whether those predicates reach the inner scan. It asserts only
// that the query plans; the plan itself is evidence for review.
func TestComparisonSeriesQuery_Plan(t *testing.T) {
	pool := newCollectorSchemaPool(t)
	ctx := context.Background()
	db := "db_a"
	def, err := ResolveComparisonMetric("pg_stat_database.cache_hit_ratio", &db)
	if err != nil {
		t.Fatalf("ResolveComparisonMetric: %v", err)
	}
	now := time.Now().UTC()
	rows, err := pool.Query(ctx, "EXPLAIN "+comparisonSeriesQuery(def),
		2, 1, db, now.Add(-24*time.Hour), now)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	t.Logf("plan:\n%s", strings.Join(plan, "\n"))
}

// TestLoadScoringSettings_MissingSchema checks that a server upgraded
// before the collector has added the tier1_scoring column, or created
// alerter_settings at all, compares against the defaults instead of
// failing the request.
func TestLoadScoringSettings_MissingSchema(t *testing.T) {
	pool := newCollectorSchemaPool(t)
	ctx := context.Background()

	for _, step := range []struct {
		name string
		ddl  string
	}{
		{"missing column", `ALTER TABLE alerter_settings DROP COLUMN tier1_scoring`},
		{"missing table", `DROP TABLE alerter_settings CASCADE`},
	} {
		t.Run(step.name, func(t *testing.T) {
			execSchema(t, pool, step.ddl)
			got, source, err := LoadScoringSettings(ctx, pool)
			if err != nil {
				t.Fatalf("LoadScoringSettings: %v", err)
			}
			if source != ScoringSourceDefaults ||
				got.DefaultSensitivity != anomaly.DefaultScoringSettings().DefaultSensitivity {
				t.Errorf("settings = %+v from %q, want the defaults", got, source)
			}
		})
	}
}

// TestLoadComparison_NonFinite seeds NaN and infinite samples and
// baseline rows, which float columns can hold, and checks that they are
// left out rather than breaking the JSON encoding of the response.
func TestLoadComparison_NonFinite(t *testing.T) {
	pool := newCollectorSchemaPool(t)
	ctx := context.Background()

	// The load average is read without clamping, unlike the CPU
	// percentage, so a NaN sample reaches the scan loop.
	const metric = "pg_sys_load_avg_info.load_avg_fifteen_minutes"
	conn := insertTestConnection(t, pool, "comparison-non-finite")
	now := time.Now().UTC().Truncate(time.Second)
	for i, v := range []string{"4", "NaN", "Infinity", "-Infinity"} {
		execSchema(t, pool, `
            INSERT INTO metrics.pg_sys_load_avg_info
                (connection_id, load_avg_fifteen_minutes, collected_at)
            VALUES ($1, $2::real, $3)`,
			conn, v, now.Add(-time.Duration(i+1)*10*time.Minute))
	}
	execSchema(t, pool, `
        INSERT INTO metric_baselines
            (connection_id, database_name, metric_name, period_type,
             hour_of_day, mean, stddev, min, max, sample_count,
             earliest_sample_at, last_calculated)
        VALUES ($1, NULL, $2, 'all', NULL, 'NaN', 5, 0, 100, 1000, $3, $4)`,
		conn, metric, now.AddDate(0, -1, 0), now)
	execSchema(t, pool, `
        INSERT INTO metric_baselines
            (connection_id, database_name, metric_name, period_type,
             hour_of_day, mean, stddev, min, max, sample_count,
             earliest_sample_at, last_calculated)
        SELECT $1, NULL, $2, 'hourly', h, 40, 'Infinity', 0, 100, 1000, $3, $4
          FROM generate_series(0, 23) AS h`,
		conn, metric, now.AddDate(0, -1, 0), now)

	got, err := LoadComparison(ctx, pool, ComparisonRequest{
		ConnectionID: conn,
		Metric:       metric,
		Model:        ComparisonModelAsScored,
		Window:       TimeWindow{Start: now.Add(-time.Hour), End: now},
		Buckets:      DefaultComparisonBuckets,
	}, now)
	if err != nil {
		t.Fatalf("LoadComparison: %v", err)
	}
	if len(got.Points) != 1 || got.Points[0].Value != 4 {
		t.Fatalf("points = %+v, want only the finite sample", got.Points)
	}
	if got.BaselinesAsOf != nil || got.Points[0].Mean != nil {
		t.Errorf("comparison = %+v, want the non-finite baselines treated as missing", got)
	}
	if _, err := json.Marshal(got); err != nil {
		t.Errorf("marshal comparison: %v", err)
	}
}
