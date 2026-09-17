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

	// probeConfigSeedSQL registers a server-wide probe config at the
	// collector's 300 second default interval, once per probe name.
	probeConfigSeedSQL = `
        INSERT INTO probe_configs
            (name, connection_id, is_enabled, collection_interval_seconds)
        SELECT $1, NULL, TRUE, 300
         WHERE NOT EXISTS (
               SELECT 1 FROM probe_configs
                WHERE name = $1 AND connection_id IS NULL)
    `

	probeAvailabilitySeedSQL = `
        INSERT INTO probe_availability
            (connection_id, probe_name, is_available, last_collected)
        VALUES ($1, $2, TRUE, NOW() - $3::interval)
        ON CONFLICT (connection_id, probe_name) DO UPDATE
           SET is_available = TRUE, last_collected = EXCLUDED.last_collected
    `
)

// seedProbeReporting makes the named probe visible in the probe staleness
// view for one connection, with its last collection the given interval
// ago. The cleaner reads that view before it accepts an absent metric row
// as a recovery, so the tests that expect a clearWhenAbsent metric to
// clear have to model a collector that is still running (GitHub issue
// #407).
func seedProbeReporting(t *testing.T, pool *pgxpool.Pool, connID int, probe, age string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, probeConfigSeedSQL, probe); err != nil {
		t.Fatalf("failed to seed probe_configs for %s: %v", probe, err)
	}
	if _, err := pool.Exec(ctx, probeAvailabilitySeedSQL, connID, probe, age); err != nil {
		t.Fatalf("failed to seed probe_availability for %s: %v", probe, err)
	}
}

// seedFreshProbe is seedProbeReporting for a probe that collected moments
// ago, which is the normal state of a monitored connection.
func seedFreshProbe(t *testing.T, pool *pgxpool.Pool, connID int, probe string) {
	t.Helper()
	seedProbeReporting(t, pool, connID, probe, "30 seconds")
}

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
	seedFreshProbe(t, pool, recoveredConn, "pg_replication_slots")
	seedFreshProbe(t, pool, stillDownConn, "pg_replication_slots")

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
	seedFreshProbe(t, pool, connID, "pg_replication_slots")
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

// slotInactiveEnv fires the critical replication_slot_inactive alert for
// one connection and then removes every slot row, so
// pg_replication_slots.inactive reports no data at all. What the cleaner
// does next depends only on whether the pg_replication_slots probe is
// still reporting, which each caller seeds differently.
func slotInactiveEnv(t *testing.T, connName string) (*Engine, *pgxpool.Pool, int, int64, func()) {
	t.Helper()
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)

	ctx := context.Background()
	var ruleID int64
	if err := pool.QueryRow(ctx, insertSlotInactiveRuleSQL).Scan(&ruleID); err != nil {
		cleanup()
		t.Fatalf("failed to insert replication_slot_inactive rule: %v", err)
	}
	connID := insertTestConnection(t, pool, connName)
	insertReplicationSlotRow(t, pool, connID, time.Now().UTC().Add(-2*time.Minute),
		"slot_a", false, 100)

	engine.evaluateThresholds(ctx)
	alert := assertAlertFired(t, ds, ruleID, connID, "critical")

	if _, err := pool.Exec(ctx,
		`DELETE FROM metrics.pg_replication_slots WHERE connection_id = $1`,
		connID); err != nil {
		cleanup()
		t.Fatalf("failed to delete slot rows: %v", err)
	}
	if _, err := ds.GetLatestMetricValues(ctx, "pg_replication_slots.inactive"); err == nil {
		cleanup()
		t.Fatal("test setup expects the metric to report no data")
	}
	return engine, pool, connID, alert.ID, cleanup
}

// TestCleaner_AbsentMetricWithFreshProbeClears is the positive half of the
// probe-freshness gate: the slots probe collected 30 seconds ago and
// reports no inactive slot, so the absence really is the recovery signal
// and the critical alert clears.
func TestCleaner_AbsentMetricWithFreshProbeClears(t *testing.T) {
	engine, pool, connID, alertID, cleanup := slotInactiveEnv(t, "slot-absent-fresh-probe")
	defer cleanup()

	capture := installNotificationCapture(t, engine)
	seedFreshProbe(t, pool, connID, "pg_replication_slots")

	engine.cleanResolvedAlerts(context.Background())

	if status := alertStatus(t, pool, alertID); status != "cleared" {
		t.Errorf("alert status with a fresh probe = %q, want \"cleared\"", status)
	}
	counts := countTypes(capture.await(t, 1))
	if counts[database.NotificationTypeAlertClear] != 1 {
		t.Errorf("notifications = %v, want one clear", counts)
	}
}

// TestCleaner_AbsentMetricWithStaleProbeStaysActive covers the bug this
// gate exists for: fifteen minutes after collection stopped the slot
// query goes empty whether or not the slot recovered, and clearing on
// that reported a genuinely inactive slot as resolved. The probe's last
// collection is 40 minutes old, eight times its 300 second interval, so
// the alert must stay active and no clear notification may be queued.
func TestCleaner_AbsentMetricWithStaleProbeStaysActive(t *testing.T) {
	engine, pool, connID, alertID, cleanup := slotInactiveEnv(t, "slot-absent-stale-probe")
	defer cleanup()

	capture := installNotificationCapture(t, engine)
	seedProbeReporting(t, pool, connID, "pg_replication_slots", "40 minutes")

	engine.cleanResolvedAlerts(context.Background())

	if status := alertStatus(t, pool, alertID); status != "active" {
		t.Errorf("alert status with a stale probe = %q, want \"active\"", status)
	}
	assertNoClearQueued(t, capture)
}

// TestCleaner_AbsentMetricWithUnreportedProbeStaysActive covers the probe
// that is missing from the staleness view altogether, which is what a
// probe that was disabled, that has never collected, or whose connection
// stopped being monitored looks like: the view filters all three out. An
// unknown probe is not evidence of recovery, so the alert stays active.
func TestCleaner_AbsentMetricWithUnreportedProbeStaysActive(t *testing.T) {
	engine, pool, _, alertID, cleanup := slotInactiveEnv(t, "slot-absent-no-probe-row")
	defer cleanup()

	capture := installNotificationCapture(t, engine)

	engine.cleanResolvedAlerts(context.Background())

	if status := alertStatus(t, pool, alertID); status != "active" {
		t.Errorf("alert status with no probe_availability row = %q, want \"active\"", status)
	}
	assertNoClearQueued(t, capture)
}

// assertNoClearQueued fails if a clear notification was queued within a
// short grace period.
func assertNoClearQueued(t *testing.T, capture *notificationCapture) {
	t.Helper()
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case job := <-capture.jobs:
			if job.notifTyp == database.NotificationTypeAlertClear {
				t.Fatal("a clear notification was queued for an alert that must stay active")
			}
		case <-deadline:
			return
		}
	}
}

// TestProbeStalenessSnapshotReadsOncePerPass proves the snapshot is a
// snapshot: the second read is served from the first, so the whole
// cleanup pass costs one GetProbeStalenessByConnection however many
// absent alerts it examines. Dropping probe_availability between the two
// reads is the demonstration, because a second read against the
// datastore could only fail. See GitHub issue #407.
func TestProbeStalenessSnapshotReadsOncePerPass(t *testing.T) {
	engine, pool, connID, _, cleanup := slotInactiveEnv(t, "slot-staleness-snapshot")
	defer cleanup()

	ctx := context.Background()
	seedFreshProbe(t, pool, connID, "pg_replication_slots")

	snapshot := &probeStalenessSnapshot{}
	first, err := snapshot.get(ctx, engine)
	if err != nil {
		t.Fatalf("first probe staleness read failed: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("test setup expects the seeded probe to be reporting")
	}

	if _, err := pool.Exec(ctx, dropProbeAvailabilityTableSQL); err != nil {
		t.Fatalf("failed to drop probe_availability: %v", err)
	}

	second, err := snapshot.get(ctx, engine)
	if err != nil {
		t.Fatalf("second probe staleness read hit the datastore again: %v", err)
	}
	if len(second) != len(first) || &second[0] != &first[0] {
		t.Error("the second read returned different entries; the snapshot was not reused")
	}
}

// setProbeInterval rewrites the server-wide collection interval for one
// probe, which is what an operator does through the probe configuration
// page documented in docs/admin-guide/probes.md.
func setProbeInterval(t *testing.T, pool *pgxpool.Pool, probe string, seconds int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE probe_configs SET collection_interval_seconds = $2
		  WHERE name = $1 AND connection_id IS NULL`, probe, seconds); err != nil {
		t.Fatalf("failed to set the %s interval: %v", probe, err)
	}
}

// overrideProbeInterval adds a per-connection collection interval for one
// probe, the other override documented on the same page.
func overrideProbeInterval(t *testing.T, pool *pgxpool.Pool, connID int, probe string, seconds int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO probe_configs
		     (name, connection_id, is_enabled, collection_interval_seconds)
		 VALUES ($1, $2, TRUE, $3)`, probe, connID, seconds); err != nil {
		t.Fatalf("failed to override the %s interval for connection %d: %v",
			probe, connID, err)
	}
}

// TestCleaner_AbsentMetricWithRaisedIntervalStaysActive covers an
// operator raising the probe's collection interval past the window its
// metric reads. The slots probe now runs hourly and last collected 30
// minutes ago, so it is only half an interval late whilst its fifteen
// minute window has been empty for a quarter of an hour. Gating on the
// interval cleared a genuinely inactive slot here; gating on the window
// leaves the alert active. See GitHub issue #407.
func TestCleaner_AbsentMetricWithRaisedIntervalStaysActive(t *testing.T) {
	engine, pool, connID, alertID, cleanup := slotInactiveEnv(t, "slot-absent-long-interval")
	defer cleanup()

	capture := installNotificationCapture(t, engine)
	seedProbeReporting(t, pool, connID, "pg_replication_slots", "30 minutes")
	setProbeInterval(t, pool, "pg_replication_slots", 3600)

	engine.cleanResolvedAlerts(context.Background())

	if status := alertStatus(t, pool, alertID); status != "active" {
		t.Errorf("alert status with an hourly probe 30 minutes late = %q, want \"active\"",
			status)
	}
	assertNoClearQueued(t, capture)
}

// TestCleaner_AbsentMetricWithPerConnectionIntervalStaysActive is the
// same case expressed as a per-connection override, which the staleness
// ratio never sees at all because GetProbeStalenessByConnection joins the
// server-wide probe_configs row. The window gate does not read either
// interval, so the verdict follows the data: the probe last collected
// outside the metric's window, so the alert stays active.
func TestCleaner_AbsentMetricWithPerConnectionIntervalStaysActive(t *testing.T) {
	engine, pool, connID, alertID, cleanup := slotInactiveEnv(t, "slot-absent-conn-interval")
	defer cleanup()

	capture := installNotificationCapture(t, engine)
	seedProbeReporting(t, pool, connID, "pg_replication_slots", "30 minutes")
	overrideProbeInterval(t, pool, connID, "pg_replication_slots", 3600)

	engine.cleanResolvedAlerts(context.Background())

	if status := alertStatus(t, pool, alertID); status != "active" {
		t.Errorf("alert status with a per-connection hourly interval = %q, want \"active\"",
			status)
	}
	assertNoClearQueued(t, capture)
}

// TestCleaner_AbsentMetricWithPerConnectionIntervalClears is its
// positive half: the same per-connection override, but the probe
// collected four minutes ago, inside the fifteen minute window. The
// absence is then genuine and the alert clears, whatever either
// configured interval says.
func TestCleaner_AbsentMetricWithPerConnectionIntervalClears(t *testing.T) {
	engine, pool, connID, alertID, cleanup := slotInactiveEnv(t, "slot-absent-conn-interval-fresh")
	defer cleanup()

	capture := installNotificationCapture(t, engine)
	seedProbeReporting(t, pool, connID, "pg_replication_slots", "4 minutes")
	overrideProbeInterval(t, pool, connID, "pg_replication_slots", 10)

	engine.cleanResolvedAlerts(context.Background())

	if status := alertStatus(t, pool, alertID); status != "cleared" {
		t.Errorf("alert status with a collection inside the window = %q, want \"cleared\"",
			status)
	}
	counts := countTypes(capture.await(t, 1))
	if counts[database.NotificationTypeAlertClear] != 1 {
		t.Errorf("notifications = %v, want one clear", counts)
	}
}

// TestCleaner_AbsentMetricWithUnreadableProbesStaysActive covers the last
// way the freshness check can come back inconclusive: the staleness query
// itself fails. A failed read says nothing about whether the condition
// ended, so the alert stays active rather than clearing on a database
// error.
func TestCleaner_AbsentMetricWithUnreadableProbesStaysActive(t *testing.T) {
	engine, pool, connID, alertID, cleanup := slotInactiveEnv(t, "slot-absent-probe-error")
	defer cleanup()

	ctx := context.Background()
	capture := installNotificationCapture(t, engine)
	seedFreshProbe(t, pool, connID, "pg_replication_slots")
	if _, err := pool.Exec(ctx, dropProbeAvailabilityTableSQL); err != nil {
		t.Fatalf("failed to drop probe_availability: %v", err)
	}

	engine.cleanResolvedAlerts(ctx)

	if status := alertStatus(t, pool, alertID); status != "active" {
		t.Errorf("alert status after a failed probe read = %q, want \"active\"", status)
	}
	assertNoClearQueued(t, capture)
}

// activeAlertByID fetches one active alert through the engine's datastore,
// as cleanResolvedAlerts would.
func activeAlertByID(t *testing.T, engine *Engine, alertID int64) *database.Alert {
	t.Helper()
	alerts, err := engine.datastore.GetActiveAlerts(context.Background())
	if err != nil {
		t.Fatalf("failed to read active alerts: %v", err)
	}
	for _, a := range alerts {
		if a.ID == alertID {
			return a
		}
	}
	t.Fatalf("alert %d is not active", alertID)
	return nil
}

// agedSnapshot returns a probe staleness snapshot whose clock jumps
// forward by skew once it has been read, standing in for a cleanup pass
// that runs on for that long between the read and the judgement.
func agedSnapshot(skew time.Duration) *probeStalenessSnapshot {
	s := &probeStalenessSnapshot{}
	s.now = func() time.Time {
		if s.loaded {
			return time.Now().Add(skew)
		}
		return time.Now()
	}
	return s
}

// TestCleaner_AbsentMetricJudgedAfterSnapshotAgesStaysActive is the
// second-round finding on #407: the slots probe last collected 14m55s
// before the snapshot was read, inside its fifteen minute window, but the
// alert is judged ten seconds later, when the probe is really 15m05s late
// and the window has emptied. The frozen snapshot said "inside"; the aged
// one must say "outside" and leave the critical alert active.
func TestCleaner_AbsentMetricJudgedAfterSnapshotAgesStaysActive(t *testing.T) {
	engine, pool, connID, alertID, cleanup := slotInactiveEnv(t, "slot-absent-aged-snapshot")
	defer cleanup()

	ctx := context.Background()
	capture := installNotificationCapture(t, engine)
	seedProbeReporting(t, pool, connID, "pg_replication_slots", "14 minutes 55 seconds")

	alert := activeAlertByID(t, engine, alertID)
	engine.checkAlertResolved(ctx, alert, nil, agedSnapshot(10*time.Second))

	if status := alertStatus(t, pool, alertID); status != "active" {
		t.Errorf("alert status judged 10s after a 14m55s snapshot = %q, want \"active\"",
			status)
	}
	assertNoClearQueued(t, capture)
}

// TestCleaner_AbsentMetricJudgedAfterSnapshotAgesStillClears is the
// control: the same ten second skew against a probe that collected 30
// seconds ago is nowhere near the window's edge, so bringing the snapshot
// forward must not stop a genuine recovery from clearing.
func TestCleaner_AbsentMetricJudgedAfterSnapshotAgesStillClears(t *testing.T) {
	engine, pool, connID, alertID, cleanup := slotInactiveEnv(t, "slot-absent-aged-fresh")
	defer cleanup()

	ctx := context.Background()
	seedFreshProbe(t, pool, connID, "pg_replication_slots")

	alert := activeAlertByID(t, engine, alertID)
	engine.checkAlertResolved(ctx, alert, nil, agedSnapshot(10*time.Second))

	if status := alertStatus(t, pool, alertID); status != "cleared" {
		t.Errorf("alert status with a fresh probe and an aged snapshot = %q, want \"cleared\"",
			status)
	}
}
