/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Seed statements for the stale-baseline tests, kept as constants so
// the multi-line SQL is not flagged as concatenated; every value is
// bound through a placeholder.
const (
	insertStaleBaselineSQL = `
        INSERT INTO metric_baselines
            (connection_id, metric_name, period_type, hour_of_day,
             mean, stddev, min, max, sample_count, last_calculated)
        VALUES ($1, $2, $3, $4, $5, 0, $5, $5, 3, $6)
    `

	insertStaleActivityRowSQL = `
        INSERT INTO metrics.pg_stat_activity
            (connection_id, backend_type, state, xact_start, collected_at)
        VALUES ($1, $2, $3, $4, $5)
    `

	selectStaleBaselineRowsSQL = `
        SELECT metric_name, period_type, COALESCE(hour_of_day, -1), mean
        FROM metric_baselines
        WHERE connection_id = $1
        ORDER BY metric_name, period_type, hour_of_day
    `
)

// staleBaselineRow is one metric_baselines row as the stale-baseline
// tests read it back.
type staleBaselineRow struct {
	metric     string
	periodType string
	hour       int
	mean       float64
}

// readStaleBaselineRows returns every baseline row for the connection.
func readStaleBaselineRows(t *testing.T, pool *pgxpool.Pool, connID int) []staleBaselineRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), selectStaleBaselineRowsSQL, connID)
	if err != nil {
		t.Fatalf("failed to read baselines: %v", err)
	}
	defer rows.Close()
	var out []staleBaselineRow
	for rows.Next() {
		var r staleBaselineRow
		if err := rows.Scan(&r.metric, &r.periodType, &r.hour, &r.mean); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("row iteration error: %v", err)
	}
	return out
}

// insertStaleBaseline writes a baseline row last calculated a week ago.
func insertStaleBaseline(t *testing.T, pool *pgxpool.Pool, connID int, metric, periodType string, hour any, mean float64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), insertStaleBaselineSQL,
		connID, metric, periodType, hour, mean,
		time.Now().Add(-7*24*time.Hour)); err != nil {
		t.Fatalf("failed to insert stale %s %s baseline: %v", metric, periodType, err)
	}
}

// TestCalculateBaselinesPrunesBucketsItNoLongerComputes reproduces the
// upgrade case from the #569 review. An hourly bucket whose samples
// held only an autovacuum worker with a 900 second transaction and an
// idle client had a 900 second baseline built by the old, unfiltered
// historical query. The corrected query returns no rows for that hour,
// so the bucket is never upserted again; the cycle must delete it
// rather than leave a live 45 second value scored against 900. The
// hour that still has client transactions is rewritten, and a row for
// a metric no rule asks for is left alone.
func TestCalculateBaselinesPrunesBucketsItNoLongerComputes(t *testing.T) {
	engine, _, pool, cleanup := newBaselinesIntegrationEnv(t)
	defer cleanup()

	ctx := context.Background()
	const metric = "pg_stat_activity.max_xact_duration_seconds"
	connID := insertBaselinesTestConnection(t, pool, "stale-bucket-conn")
	insertBaselinesTestAlertRule(t, pool, "stale-bucket-rule", metric)

	now := time.Now().UTC().Truncate(time.Hour)
	staleHour := now.Add(-2 * time.Hour).Hour()
	liveHour := now.Add(-5 * time.Hour).Hour()

	for d := 1; d <= 3; d++ {
		// The stale hour: background work only, plus an idle client
		// with no open transaction.
		at := now.Add(-time.Duration(24*d+2) * time.Hour)
		for _, r := range []struct {
			backendType, state string
			xactStart          any
		}{
			{"autovacuum worker", "active", at.Add(-900 * time.Second)},
			{"client backend", "idle", nil},
		} {
			if _, err := pool.Exec(ctx, insertStaleActivityRowSQL,
				connID, r.backendType, r.state, r.xactStart, at); err != nil {
				t.Fatalf("failed to seed stale-hour activity: %v", err)
			}
		}

		// The live hour: a client transaction 45 seconds old.
		at = now.Add(-time.Duration(24*d+5) * time.Hour)
		if _, err := pool.Exec(ctx, insertStaleActivityRowSQL,
			connID, "client backend", "active", at.Add(-45*time.Second), at); err != nil {
			t.Fatalf("failed to seed live-hour activity: %v", err)
		}
	}

	insertStaleBaseline(t, pool, connID, metric, "hourly", staleHour, 900)
	insertStaleBaseline(t, pool, connID, metric, "hourly", liveHour, 900)
	insertStaleBaseline(t, pool, connID, "pg_stat_activity.count", "all", nil, 7)

	engine.calculateBaselines(ctx)

	got := readStaleBaselineRows(t, pool, connID)
	want := []staleBaselineRow{
		{"pg_stat_activity.count", "all", -1, 7},
		{metric, "all", -1, 45},
		{metric, "hourly", liveHour, 45},
	}
	if len(got) != len(want) {
		t.Fatalf("baselines = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("baseline[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestCalculateBaselinesPrunesMetricWithNoSamples covers a metric whose
// historical query succeeds but returns nothing in the lookback window:
// every one of its rows was built from data that has aged out, so all
// of them go.
func TestCalculateBaselinesPrunesMetricWithNoSamples(t *testing.T) {
	engine, _, pool, cleanup := newBaselinesIntegrationEnv(t)
	defer cleanup()

	ctx := context.Background()
	const metric = "pg_stat_activity.count"
	connID := insertBaselinesTestConnection(t, pool, "stale-empty-conn")
	insertBaselinesTestAlertRule(t, pool, "stale-empty-rule", metric)

	insertStaleBaseline(t, pool, connID, metric, "all", nil, 12)
	insertStaleBaseline(t, pool, connID, metric, "hourly", 3, 12)

	engine.calculateBaselines(ctx)

	if got := readStaleBaselineRows(t, pool, connID); len(got) != 0 {
		t.Errorf("expected every stale row to be pruned, got %+v", got)
	}
}

// TestPruneStaleBaselinesLogsFailure checks that a failed prune is
// logged and does not panic or abort the caller.
func TestPruneStaleBaselinesLogsFailure(t *testing.T) {
	engine, _, _, cleanup := newBaselinesIntegrationEnv(t)
	defer cleanup()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	output := captureStderr(t, func() {
		engine.pruneStaleBaselines(canceled, "pg_stat_activity.count", time.Now())
	})
	if !strings.Contains(output, "ERROR: Failed to delete stale baselines for metric pg_stat_activity.count") {
		t.Errorf("expected prune failure to be logged, got:\n%s", output)
	}
}
