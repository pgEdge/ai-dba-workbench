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
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// insertProbeScopedAlert inserts a probe-scoped threshold alert with the
// supplied status and returns its id. cleared alerts are stamped with a
// cleared_at of NOW() so the cooldown lookup can see them.
func insertProbeScopedAlert(t *testing.T, pool *pgxpool.Pool, connID int, ruleID int64, probeName, status string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO alerts (
			alert_type, rule_id, connection_id, probe_name, severity,
			title, description, status, metric_name, metric_value,
			threshold_value, operator, cleared_at
		) VALUES ('threshold', $1, $2, $3, 'warning', 'stale-alert',
		    'desc', $4, 'probe_staleness_ratio', 30, 3, '>',
		    CASE WHEN $4 = 'cleared' THEN NOW() ELSE NULL END)
		RETURNING id
	`, ruleID, connID, probeName, status).Scan(&id)
	if err != nil {
		t.Fatalf("insertProbeScopedAlert: %v", err)
	}
	return id
}

// TestGetActiveThresholdAlertForProbe verifies that the probe-scoped lookup
// distinguishes alerts raised for different probes on the same connection,
// which the rule/connection-keyed lookup cannot do.
func TestGetActiveThresholdAlertForProbe(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "probe-alert-conn")
	ruleID := insertTestRule(t, pool, "metric_staleness", "probe_staleness_ratio",
		">", 3, "warning", true)

	// No alert yet.
	got, err := ds.GetActiveThresholdAlertForProbe(ctx, ruleID, connID, "pg_stat_activity")
	if err != nil {
		t.Fatalf("GetActiveThresholdAlertForProbe (none): %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}

	activityID := insertProbeScopedAlert(t, pool, connID, ruleID, "pg_stat_activity", "active")
	databaseID := insertProbeScopedAlert(t, pool, connID, ruleID, "pg_stat_database", "active")

	got, err = ds.GetActiveThresholdAlertForProbe(ctx, ruleID, connID, "pg_stat_activity")
	if err != nil {
		t.Fatalf("GetActiveThresholdAlertForProbe: %v", err)
	}
	if got == nil || got.ID != activityID {
		t.Fatalf("got %+v, want id=%d", got, activityID)
	}
	if got.ProbeName == nil || *got.ProbeName != "pg_stat_activity" {
		t.Errorf("probe name = %v, want pg_stat_activity", got.ProbeName)
	}

	got, err = ds.GetActiveThresholdAlertForProbe(ctx, ruleID, connID, "pg_stat_database")
	if err != nil {
		t.Fatalf("GetActiveThresholdAlertForProbe (second probe): %v", err)
	}
	if got == nil || got.ID != databaseID {
		t.Fatalf("got %+v, want id=%d", got, databaseID)
	}

	// A cleared alert must not be returned.
	if _, err := pool.Exec(ctx, `UPDATE alerts SET status = 'cleared' WHERE id = $1`,
		activityID); err != nil {
		t.Fatalf("failed to clear alert: %v", err)
	}
	got, err = ds.GetActiveThresholdAlertForProbe(ctx, ruleID, connID, "pg_stat_activity")
	if err != nil {
		t.Fatalf("GetActiveThresholdAlertForProbe (cleared): %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for a cleared alert, got %+v", got)
	}

	// Canceled context: error.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetActiveThresholdAlertForProbe(canceled, ruleID, connID,
		"pg_stat_activity"); err == nil {
		t.Errorf("expected cancellation error")
	}
}

// TestGetRecentlyClearedAlertForProbe verifies the probe-scoped cooldown
// lookup, including that a clear for one probe does not suppress another.
func TestGetRecentlyClearedAlertForProbe(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "probe-cooldown-conn")
	ruleID := insertTestRule(t, pool, "metric_staleness", "probe_staleness_ratio",
		">", 3, "warning", true)

	insertProbeScopedAlert(t, pool, connID, ruleID, "pg_stat_activity", "cleared")

	exists, err := ds.GetRecentlyClearedAlertForProbe(ctx, ruleID, connID,
		"pg_stat_activity", 60*time.Second)
	if err != nil {
		t.Fatalf("GetRecentlyClearedAlertForProbe: %v", err)
	}
	if !exists {
		t.Error("expected the recent clear to be inside the cooldown window")
	}

	// A different probe on the same connection is unaffected.
	exists, err = ds.GetRecentlyClearedAlertForProbe(ctx, ruleID, connID,
		"pg_stat_database", 60*time.Second)
	if err != nil {
		t.Fatalf("GetRecentlyClearedAlertForProbe (other probe): %v", err)
	}
	if exists {
		t.Error("a clear for one probe must not suppress another probe")
	}

	// The clear falls outside a very short cooldown.
	if _, err := pool.Exec(ctx, `
		UPDATE alerts SET cleared_at = NOW() - INTERVAL '10 seconds'
		WHERE rule_id = $1
	`, ruleID); err != nil {
		t.Fatalf("failed to age the clear: %v", err)
	}
	exists, err = ds.GetRecentlyClearedAlertForProbe(ctx, ruleID, connID,
		"pg_stat_activity", 1*time.Second)
	if err != nil {
		t.Fatalf("GetRecentlyClearedAlertForProbe (short cooldown): %v", err)
	}
	if exists {
		t.Error("expected exists=false with a 1s cooldown")
	}

	// Canceled context: error.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetRecentlyClearedAlertForProbe(canceled, ruleID, connID,
		"pg_stat_activity", 60*time.Second); err == nil {
		t.Errorf("expected cancellation error")
	}
}

// TestGetLatestMetricValuesErrorKinds pins the sentinel errors that
// checkAlertResolved relies on to tell an unusable metric apart from one
// that genuinely reports nothing.
func TestGetLatestMetricValuesErrorKinds(t *testing.T) {
	ds, _, cleanup := newMetricRegistryTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	// A metric with no registry entry, such as the bespoke staleness
	// metric, is not supported rather than empty.
	_, err := ds.GetLatestMetricValues(ctx, "probe_staleness_ratio")
	if !errors.Is(err, ErrMetricNotSupported) {
		t.Errorf("probe_staleness_ratio error = %v, want ErrMetricNotSupported", err)
	}
	if errors.Is(err, ErrNoMetricData) {
		t.Error("probe_staleness_ratio must not report as an empty result")
	}

	// A registered metric whose tables are empty reports no data.
	_, err = ds.GetLatestMetricValues(ctx, "pg_replication_slots.inactive_count")
	if !errors.Is(err, ErrNoMetricData) {
		t.Errorf("empty metric error = %v, want ErrNoMetricData", err)
	}
}

// withTestMetric registers a temporary metric registry entry for the
// duration of the test. Tests in this package run sequentially, so
// mutating the package-level registry is safe as long as the entry is
// removed again.
func withTestMetric(t *testing.T, name string, cfg metricQueryConfig) {
	t.Helper()

	if _, exists := metricRegistry[name]; exists {
		t.Fatalf("metric %s already exists in the registry", name)
	}
	metricRegistry[name] = cfg
	t.Cleanup(func() { delete(metricRegistry, name) })
}

// TestGetLatestMetricValuesScanTypes exercises every scan branch of
// GetLatestMetricValues, including the guard against a registry entry with
// an unrecognized scan type and the wrapping of a failed query.
func TestGetLatestMetricValuesScanTypes(t *testing.T) {
	ds, _, cleanup := newMetricRegistryTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	withTestMetric(t, "test.with_db", metricQueryConfig{
		latestSQL: `SELECT 1 AS connection_id, 'app_db'::text AS database_name,
		                   2.5::float AS value, NOW() AS collected_at`,
		scan: scanWithDB,
	})
	withTestMetric(t, "test.with_db_object", metricQueryConfig{
		latestSQL: `SELECT 1 AS connection_id, 'app_db'::text AS database_name,
		                   'tbl'::text AS object_name, 3.5::float AS value,
		                   NOW() AS collected_at`,
		scan: scanWithDBObject,
	})
	withTestMetric(t, "test.bad_scan", metricQueryConfig{
		latestSQL: `SELECT 1`,
		scan:      scanType(99),
	})
	withTestMetric(t, "test.broken_query", metricQueryConfig{
		latestSQL: `SELECT connection_id, value, collected_at FROM metrics.no_such_table`,
		scan:      scanBasic,
	})

	values, err := ds.GetLatestMetricValues(ctx, "test.with_db")
	if err != nil {
		t.Fatalf("scanWithDB: %v", err)
	}
	if len(values) != 1 || values[0].DatabaseName == nil || *values[0].DatabaseName != "app_db" {
		t.Errorf("scanWithDB values = %+v", values)
	}

	values, err = ds.GetLatestMetricValues(ctx, "test.with_db_object")
	if err != nil {
		t.Fatalf("scanWithDBObject: %v", err)
	}
	if len(values) != 1 || values[0].ObjectName == nil || *values[0].ObjectName != "tbl" {
		t.Errorf("scanWithDBObject values = %+v", values)
	}

	if _, err := ds.GetLatestMetricValues(ctx, "test.bad_scan"); !errors.Is(err, ErrMetricNotSupported) {
		t.Errorf("unknown scan type error = %v, want ErrMetricNotSupported", err)
	}

	_, err = ds.GetLatestMetricValues(ctx, "test.broken_query")
	if err == nil {
		t.Error("expected an error from a query against a missing table")
	}
	if errors.Is(err, ErrNoMetricData) || errors.Is(err, ErrMetricNotSupported) {
		t.Errorf("a failed query must not report as missing data: %v", err)
	}
}
