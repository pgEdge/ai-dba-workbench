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
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// These tests cover how cleanResolvedAlerts treats an active alert whose
// metric no longer returns a row for it (GitHub issue #407). The registry
// classifies each metric with clearWhenAbsent:
//
//   - false (the default): the query emits a row for every healthy
//     connection, so a missing row means the data has stopped arriving.
//     The alert stays active on both the "no rows at all"
//     (ErrNoMetricData) path and the "no row for this connection and
//     database" path.
//   - true: the query emits a row only while the condition holds, or its
//     subject can legitimately disappear, so a missing row is the
//     recovery signal and the alert clears on both paths.
//
// pg_stat_database.cache_hit_ratio stands in for the first class and
// pg_replication_slots.inactive for the second.

const (
	insertSlotInactiveRuleSQL = `
        INSERT INTO alert_rules
            (name, description, category, metric_name, default_operator,
             default_threshold, default_severity, default_enabled, is_built_in)
        VALUES ('replication_slot_inactive', 'A replication slot is inactive',
                'replication', 'pg_replication_slots.inactive', '==', 1,
                'critical', TRUE, TRUE)
        RETURNING id
    `

	deleteStatDatabaseRowsSQL = `
        DELETE FROM metrics.pg_stat_database
        WHERE connection_id = $1 AND database_name = $2
    `
)

// seedColdCache writes four samples for one database whose newest
// interval is a 0% cache hit ratio (30000 reads, no hits), which the
// seeded "< 80" rule fires on.
func seedColdCache(t *testing.T, pool *pgxpool.Pool, connID int, dbName string) {
	t.Helper()
	samples := []struct {
		offset string
		hits   int64
		reads  int64
	}{
		{"12 minutes", 0, 0},
		{"8 minutes", 30_000, 0},
		{"4 minutes", 60_000, 0},
		{"1 minute", 60_000, 30_000},
	}
	for _, s := range samples {
		if _, err := pool.Exec(context.Background(), insertStatDatabaseSampleSQL,
			connID, dbName, s.hits, s.reads, s.offset); err != nil {
			t.Fatalf("failed to seed pg_stat_database for %s: %v", dbName, err)
		}
	}
}

// alertStatus reads the status column of one alert.
func alertStatus(t *testing.T, pool *pgxpool.Pool, alertID int64) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(), selectAlertStatusSQL,
		alertID).Scan(&status); err != nil {
		t.Fatalf("failed to read alert %d status: %v", alertID, err)
	}
	return status
}

// newCacheHitEnv builds the Spock engine environment plus the
// pg_stat_database fixture and the cache_hit_ratio_low rule.
func newCacheHitEnv(t *testing.T) (*Engine, *database.Datastore, *pgxpool.Pool, int64, func()) {
	t.Helper()
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, createStatDatabaseTableSQL); err != nil {
		cleanup()
		t.Fatalf("failed to create metrics.pg_stat_database: %v", err)
	}
	var ruleID int64
	if err := pool.QueryRow(ctx, insertCacheHitRuleSQL).Scan(&ruleID); err != nil {
		cleanup()
		t.Fatalf("failed to insert cache_hit_ratio_low rule: %v", err)
	}
	return engine, ds, pool, ruleID, cleanup
}

// TestCleaner_NoDataLeavesUnflaggedAlertActive drives the ErrNoMetricData
// path for a metric that is not clearWhenAbsent: after the alert fires,
// every pg_stat_database row is deleted (as if the collector had stopped),
// the cleaner runs, and the alert must remain active with no clear
// notification. Before #407 the cleaner treated the empty result as a
// resolution and the evaluator re-raised the alert on the next sample.
func TestCleaner_NoDataLeavesUnflaggedAlertActive(t *testing.T) {
	engine, ds, pool, ruleID, cleanup := newCacheHitEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installNotificationCapture(t, engine)
	connID := insertTestConnection(t, pool, "missing-data-nodata")
	seedColdCache(t, pool, connID, "appdb")

	engine.evaluateThresholds(ctx)
	alert, err := ds.GetActiveThresholdAlert(ctx, ruleID, connID, strPtr("appdb"))
	if err != nil {
		t.Fatalf("GetActiveThresholdAlert failed: %v", err)
	}
	if alert == nil {
		t.Fatal("expected the cold cache to raise an alert")
	}

	if _, err := pool.Exec(ctx, `DELETE FROM metrics.pg_stat_database`); err != nil {
		t.Fatalf("failed to delete pg_stat_database rows: %v", err)
	}
	if _, err := ds.GetLatestMetricValues(ctx, "pg_stat_database.cache_hit_ratio"); err == nil {
		t.Fatal("test setup expects the metric to report no data")
	}

	engine.cleanResolvedAlerts(ctx)

	if status := alertStatus(t, pool, alert.ID); status != "active" {
		t.Errorf("alert status after data vanished = %q, want \"active\"", status)
	}
	counts := countTypes(capture.await(t, 1))
	if counts[database.NotificationTypeAlertFire] != 1 {
		t.Errorf("notifications = %v, want exactly one fire and no clear", counts)
	}
}

// TestCleaner_MissingRowLeavesUnflaggedAlertActive drives the "no row for
// this connection and database" path for a metric that is not
// clearWhenAbsent. Two databases on one connection both fire; the rows
// for one are then deleted while the other receives a healthy newer
// interval. The cleaner must clear the database that measurably
// recovered and leave the one that merely stopped reporting active.
func TestCleaner_MissingRowLeavesUnflaggedAlertActive(t *testing.T) {
	engine, ds, pool, ruleID, cleanup := newCacheHitEnv(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "missing-data-row")
	seedColdCache(t, pool, connID, "appdb")
	seedColdCache(t, pool, connID, "otherdb")

	engine.evaluateThresholds(ctx)

	alerts := map[string]*database.Alert{}
	for _, dbName := range []string{"appdb", "otherdb"} {
		alert, err := ds.GetActiveThresholdAlert(ctx, ruleID, connID, strPtr(dbName))
		if err != nil {
			t.Fatalf("GetActiveThresholdAlert(%s) failed: %v", dbName, err)
		}
		if alert == nil {
			t.Fatalf("expected an alert for %s", dbName)
		}
		alerts[dbName] = alert
	}

	// appdb stops reporting; otherdb recovers with a healthy interval.
	if _, err := pool.Exec(ctx, deleteStatDatabaseRowsSQL, connID, "appdb"); err != nil {
		t.Fatalf("failed to delete appdb rows: %v", err)
	}
	if _, err := pool.Exec(ctx, insertStatDatabaseSampleSQL,
		connID, "otherdb", int64(90_000), int64(30_000), "10 seconds"); err != nil {
		t.Fatalf("failed to seed the otherdb recovery sample: %v", err)
	}

	engine.cleanResolvedAlerts(ctx)

	if status := alertStatus(t, pool, alerts["appdb"].ID); status != "active" {
		t.Errorf("appdb alert status = %q, want \"active\" (no row is not a recovery)", status)
	}
	if status := alertStatus(t, pool, alerts["otherdb"].ID); status != "cleared" {
		t.Errorf("otherdb alert status = %q, want \"cleared\" (healthy newest interval)", status)
	}
}

// TestCleaner_MissingRowClearsFlaggedAlert drives the "no row for this
// connection" path for pg_replication_slots.inactive, which is
// clearWhenAbsent because the query emits a row only while some slot is
// inactive. Two connections fire; one slot then reports active in a newer
// sample, so the metric still returns rows (for the other connection) but
// none for the recovered one, and that alert must clear.
func TestCleaner_MissingRowClearsFlaggedAlert(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	var ruleID int64
	if err := pool.QueryRow(ctx, insertSlotInactiveRuleSQL).Scan(&ruleID); err != nil {
		t.Fatalf("failed to insert replication_slot_inactive rule: %v", err)
	}
	recoveredConn := insertTestConnection(t, pool, "slot-inactive-recovers")
	stillDownConn := insertTestConnection(t, pool, "slot-inactive-stays")

	sample := time.Now().UTC().Add(-2 * time.Minute)
	insertReplicationSlotRow(t, pool, recoveredConn, sample, "slot_a", false, 100)
	insertReplicationSlotRow(t, pool, stillDownConn, sample, "slot_a", false, 100)

	engine.evaluateThresholds(ctx)
	recovered := assertAlertFired(t, ds, ruleID, recoveredConn, "critical")
	stillDown := assertAlertFired(t, ds, ruleID, stillDownConn, "critical")

	insertReplicationSlotRow(t, pool, recoveredConn, time.Now().UTC(), "slot_a", true, 100)
	engine.cleanResolvedAlerts(ctx)

	assertAlertCleared(t, ds, pool, ruleID, recoveredConn, recovered.ID)
	if status := alertStatus(t, pool, stillDown.ID); status != "active" {
		t.Errorf("still-inactive connection's alert status = %q, want \"active\"", status)
	}
}

// TestCleaner_NoDataClearsFlaggedAlert drives the ErrNoMetricData path for
// a clearWhenAbsent metric by way of the fifteen minute freshness cutoff
// added to pg_replication_slots.max_retained_bytes in #407. The retention
// alert fires on a fresh 15 GiB sample; the sample is then aged to 16
// minutes, so the query returns nothing at all, and the alert clears.
// Before the cutoff the alert would have fired on that sample for as long
// as the partition existed.
func TestCleaner_NoDataClearsFlaggedAlert(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	rules := seedSpockBuiltInRules(t, pool)
	disableAllRulesExcept(t, pool, "replication_slot_retention_high")

	connID := insertTestConnection(t, pool, "slot-retention-stale")
	const fifteenGiB = int64(15) * 1024 * 1024 * 1024
	insertReplicationSlotRow(t, pool, connID, time.Now().UTC().Add(-1*time.Minute),
		"slot_a", true, fifteenGiB)

	engine.evaluateThresholds(ctx)
	alert := assertAlertFired(t, ds, rules["replication_slot_retention_high"], connID, "critical")

	if _, err := pool.Exec(ctx, `
        UPDATE metrics.pg_replication_slots
           SET collected_at = $1
         WHERE connection_id = $2
    `, time.Now().UTC().Add(-16*time.Minute), connID); err != nil {
		t.Fatalf("failed to age the slot sample: %v", err)
	}

	engine.cleanResolvedAlerts(ctx)
	assertAlertCleared(t, ds, pool, rules["replication_slot_retention_high"], connID, alert.ID)
}
