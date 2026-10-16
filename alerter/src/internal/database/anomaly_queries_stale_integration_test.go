/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"testing"
	"time"
)

// insertStaleTestBaselineSQL seeds a baseline row with an explicit
// last_calculated; every value is bound through a placeholder.
const insertStaleTestBaselineSQL = `
    INSERT INTO metric_baselines
        (connection_id, metric_name, period_type, hour_of_day,
         mean, stddev, min, max, sample_count, last_calculated)
    VALUES ($1, $2, $3, $4, 1, 0, 1, 1, 3, $5)
`

// countSurvivingStaleTestBaselinesSQL counts the rows the delete must
// keep: the row rewritten this cycle and the other metric's row.
const countSurvivingStaleTestBaselinesSQL = `
    SELECT COUNT(*) FROM metric_baselines
    WHERE connection_id = $1
      AND ((metric_name = 'pg_stat_activity.count' AND hour_of_day = 5)
           OR metric_name = 'pg_stat_database.deadlocks_delta')
`

// TestDeleteStaleMetricBaselines checks that only the named metric's
// rows calculated before the cutoff are deleted, that the count is
// reported, and that a canceled context returns an error.
func TestDeleteStaleMetricBaselines(t *testing.T) {
	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertAuditConnection(t, pool, "stale-baseline-delete")

	cutoff := time.Now().UTC().Truncate(time.Microsecond)
	old := cutoff.Add(-time.Hour)
	seed := []struct {
		metric, periodType string
		hour               any
		lastCalculated     time.Time
	}{
		{"pg_stat_activity.count", "hourly", 4, old},          // stale: deleted
		{"pg_stat_activity.count", "hourly", 5, cutoff},       // rewritten this cycle
		{"pg_stat_activity.count", "all", nil, old},           // stale: deleted
		{"pg_stat_database.deadlocks_delta", "all", nil, old}, // other metric
	}
	for _, r := range seed {
		if _, err := pool.Exec(ctx, insertStaleTestBaselineSQL, connID,
			r.metric, r.periodType, r.hour, r.lastCalculated); err != nil {
			t.Fatalf("failed to seed %s %s: %v", r.metric, r.periodType, err)
		}
	}

	deleted, err := ds.DeleteStaleMetricBaselines(ctx, "pg_stat_activity.count", cutoff)
	if err != nil {
		t.Fatalf("DeleteStaleMetricBaselines failed: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}

	var remaining int
	if err := pool.QueryRow(ctx,
		countSurvivingStaleTestBaselinesSQL, connID).Scan(&remaining); err != nil {
		t.Fatalf("failed to count remaining rows: %v", err)
	}
	if remaining != 2 {
		t.Errorf("remaining expected rows = %d, want 2", remaining)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.DeleteStaleMetricBaselines(canceled, "pg_stat_activity.count", cutoff); err == nil {
		t.Error("expected an error from a canceled context")
	}
}
