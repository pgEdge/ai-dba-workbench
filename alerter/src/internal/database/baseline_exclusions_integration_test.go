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
	"slices"
	"strings"
	"testing"
	"time"
)

// thresholdOnlyMetrics are registry metrics that are available to
// threshold rules only and are deliberately excluded from baselines and
// anomaly detection (GitHub issue #576): max_connections is a
// configuration value read by the seeded high_max_connections rule, and
// inactive_count is a presence count that is almost always zero, whose
// condition the seeded replication_slot_inactive rule on
// pg_replication_slots.inactive already covers.
var thresholdOnlyMetrics = []string{
	"pg_replication_slots.inactive_count",
	"pg_settings.max_connections",
}

// TestThresholdOnlyMetrics_NotBaselined pins the exclusion: each metric
// keeps the latest query threshold rules read, has no historical query,
// is rejected by SupportsBaselines and is absent from the list the
// baseline sweep keeps.
func TestThresholdOnlyMetrics_NotBaselined(t *testing.T) {
	ds := &Datastore{}
	supported := BaselineSupportedMetrics()
	for _, name := range thresholdOnlyMetrics {
		cfg, ok := metricRegistry[name]
		if !ok {
			t.Fatalf("%s is missing from the registry", name)
		}
		if strings.TrimSpace(cfg.latestSQL) == "" {
			t.Errorf("%s lost its latest query; threshold rules need it", name)
		}
		if cfg.historicalSQL != "" {
			t.Errorf("%s has a historical query; it must not be baselined", name)
		}
		if SupportsBaselines(name) {
			t.Errorf("SupportsBaselines(%s) = true, want false", name)
		}
		if slices.Contains(supported, name) {
			t.Errorf("BaselineSupportedMetrics lists %s", name)
		}
		if _, err := ds.GetHistoricalMetricValues(context.Background(), name, 1); err == nil {
			t.Errorf("GetHistoricalMetricValues(%s) returned no error", name)
		}
	}
}

// TestThresholdOnlyMetrics_OldBaselinesSwept verifies that baseline rows
// written for these metrics before they were excluded are removed by the
// sweep calculateBaselines runs every cycle, whilst a supported metric's
// row survives.
func TestThresholdOnlyMetrics_OldBaselinesSwept(t *testing.T) {
	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertAuditConnection(t, pool, "threshold-only-sweep")

	for _, metric := range append([]string{"pg_stat_activity.count"}, thresholdOnlyMetrics...) {
		if _, err := pool.Exec(ctx, insertAuditBaselineSQL,
			connID, nil, metric, "all", nil, nil,
			1.0, 0.0, 1.0, 1.0, int64(1), nil); err != nil {
			t.Fatalf("failed to insert %s baseline: %v", metric, err)
		}
	}

	deleted, err := ds.DeleteBaselinesForUnsupportedMetrics(ctx)
	if err != nil {
		t.Fatalf("DeleteBaselinesForUnsupportedMetrics failed: %v", err)
	}
	if deleted != int64(len(thresholdOnlyMetrics)) {
		t.Errorf("deleted = %d, want %d", deleted, len(thresholdOnlyMetrics))
	}

	var remaining []string
	rows, err := pool.Query(ctx,
		`SELECT metric_name FROM metric_baselines WHERE connection_id = $1`, connID)
	if err != nil {
		t.Fatalf("failed to read remaining baselines: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		remaining = append(remaining, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("row iteration error: %v", err)
	}
	if len(remaining) != 1 || remaining[0] != "pg_stat_activity.count" {
		t.Errorf("remaining baselines = %v, want [pg_stat_activity.count]", remaining)
	}
}

// TestMaxConnectionsLatest_NewestSnapshot verifies that the latest query
// the high_max_connections threshold rule reads still reports the newest
// max_connections snapshot, however old it is, and ignores other
// settings stored in the same snapshot.
func TestMaxConnectionsLatest_NewestSnapshot(t *testing.T) {
	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertAuditConnection(t, pool, "max-connections-latest")

	now := time.Now().UTC()
	for _, s := range []struct {
		setting string
		at      time.Time
	}{
		{"100", now.Add(-72 * time.Hour)},
		{"600", now.Add(-48 * time.Hour)},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO metrics.pg_settings
			    (connection_id, name, setting, collected_at)
			VALUES ($1, 'max_connections', $2, $3),
			       ($1, 'shared_buffers', '16384', $3)
		`, connID, s.setting, s.at); err != nil {
			t.Fatalf("failed to insert pg_settings snapshot: %v", err)
		}
	}

	values, err := ds.GetLatestMetricValues(ctx, "pg_settings.max_connections")
	if err != nil {
		t.Fatalf("GetLatestMetricValues failed: %v", err)
	}
	var found []MetricValue
	for _, v := range values {
		if v.ConnectionID == connID {
			found = append(found, v)
		}
	}
	if len(found) != 1 || found[0].Value != 600 {
		t.Errorf("latest max_connections = %+v, want one row with value 600", found)
	}
}
