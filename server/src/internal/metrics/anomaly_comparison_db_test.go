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
	insertCPU(other, now.Add(-30*time.Minute), 77)

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
			if !p.Cold || p.Outside || p.Mean != nil {
				t.Errorf("point %+v, want cold with no band", p)
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
