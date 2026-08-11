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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
	"github.com/pgedge/ai-workbench/pkg/worker"
)

// This file holds verification tests for an audit of the alerter
// subsystem. Each test documents an audit claim and pins the behavior
// the engine actually exhibits today. Where the claim describes a
// defect, the test asserts the CURRENT (defective) behavior and the
// comment states what the behavior SHOULD be, so the suite stays green
// until the defect is fixed.
//
// Tests whose name ends in "Demo" assert the CORRECT behavior instead
// and therefore fail against the current code. They are skipped unless
// ALERTER_DEFECT_DEMO=1 is set, so CI stays green while the defect can
// still be demonstrated on demand.

// engineDefectDemoEnabled skips the calling test unless
// ALERTER_DEFECT_DEMO is set.
func engineDefectDemoEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("ALERTER_DEFECT_DEMO") == "" {
		t.Skip("set ALERTER_DEFECT_DEMO=1 to run the defect demonstration")
	}
}

// Seed and read-back statements used by the audit tests. They are
// named constants so the Codacy/Semgrep go_sql_rule-concat-sqli rule
// does not flag inline multi-line SQL passed to Exec/QueryRow; every
// value is still bound via $N.
const (
	insertStalenessRuleSQL = `
        INSERT INTO alert_rules
            (name, description, category, metric_name, default_operator,
             default_threshold, default_severity, default_enabled, is_built_in)
        VALUES ('metric_staleness',
                'Metrics collection is stale; dashboards may show outdated data',
                'availability', 'probe_staleness_ratio', '>', 3, 'warning',
                TRUE, TRUE)
        RETURNING id
    `

	insertArchiverRuleSQL = `
        INSERT INTO alert_rules
            (name, description, category, metric_name, default_operator,
             default_threshold, default_severity, default_enabled, is_built_in)
        VALUES ('wal_archive_failed', 'WAL archiving failures detected',
                'wal', 'pg_stat_archiver.failed_count_delta', '>', 0,
                'critical', TRUE, TRUE)
        RETURNING id
    `

	insertCacheHitRuleSQL = `
        INSERT INTO alert_rules
            (name, description, category, metric_name, default_operator,
             default_threshold, default_severity, default_enabled, is_built_in)
        VALUES ('cache_hit_ratio_low', 'Cache hit ratio below threshold',
                'performance', 'pg_stat_database.cache_hit_ratio', '<', 80,
                'warning', TRUE, TRUE)
        RETURNING id
    `

	insertProbeConfigSQL = `
        INSERT INTO probe_configs
            (name, connection_id, is_enabled, collection_interval_seconds)
        VALUES ($1, NULL, TRUE, $2)
    `

	insertProbeAvailabilitySQL = `
        INSERT INTO probe_availability
            (connection_id, probe_name, is_available, last_collected)
        VALUES ($1, $2, TRUE, NOW() - $3::interval)
    `

	selectAlertsForRuleSQL = `
        SELECT id, status
        FROM alerts
        WHERE rule_id = $1
        ORDER BY id
    `

	selectAlertStatusSQL = `
        SELECT status FROM alerts WHERE id = $1
    `

	insertSlotSampleSQL = `
        INSERT INTO metrics.pg_replication_slots
            (connection_id, slot_name, active, retained_bytes, collected_at)
        VALUES ($1, $2, $3, $4, NOW())
    `
)

// notificationCapture records the notification jobs the engine submits
// to its worker pool. The pool handler forwards each job onto a
// buffered channel so tests can await an exact number of jobs without
// polling.
type notificationCapture struct {
	jobs chan notificationJob
}

// installNotificationCapture replaces the engine's notification worker
// pool with one whose handler records every submitted job. The pool is
// stopped via t.Cleanup so no goroutine outlives the test.
//
// The engine's own pool is only created when a notification manager is
// configured; the integration environments in this package build the
// engine without one, so this helper is the only way to observe
// queueNotification.
func installNotificationCapture(t *testing.T, e *Engine) *notificationCapture {
	t.Helper()

	capture := &notificationCapture{jobs: make(chan notificationJob, 256)}
	pool := worker.NewWorkerPool(1, 256, func(job notificationJob) {
		capture.jobs <- job
	})
	pool.Start()

	previous := e.notificationPool
	e.notificationPool = pool
	t.Cleanup(func() {
		e.notificationPool = previous
		pool.Stop()
	})
	return capture
}

// await reads exactly n jobs from the capture channel, failing the
// test if they do not arrive within the timeout. It then waits a short
// grace period and fails if any extra job arrives, so the caller's
// count assertion is exact rather than a lower bound.
func (c *notificationCapture) await(t *testing.T, n int) []notificationJob {
	t.Helper()

	jobs := make([]notificationJob, 0, n)
	deadline := time.After(10 * time.Second)
	for len(jobs) < n {
		select {
		case job := <-c.jobs:
			jobs = append(jobs, job)
		case <-deadline:
			t.Fatalf("timed out waiting for %d notifications; got %d",
				n, len(jobs))
		}
	}

	select {
	case extra := <-c.jobs:
		t.Fatalf("received an unexpected extra notification: type=%s alert=%q",
			extra.notifTyp, extra.alert.Title)
	case <-time.After(200 * time.Millisecond):
	}
	return jobs
}

// countTypes summarizes the captured jobs by notification type.
func countTypes(jobs []notificationJob) map[database.NotificationType]int {
	counts := make(map[database.NotificationType]int)
	for _, job := range jobs {
		counts[job.notifTyp]++
	}
	return counts
}

// seedStalenessFixture inserts the metric_staleness rule plus a probe
// whose last collection is far enough in the past to breach the
// default staleness ratio of 3. It returns the rule id and the
// connection id.
func seedStalenessFixture(t *testing.T, pool *pgxpool.Pool) (int64, int) {
	t.Helper()
	ctx := context.Background()

	var ruleID int64
	if err := pool.QueryRow(ctx, insertStalenessRuleSQL).Scan(&ruleID); err != nil {
		t.Fatalf("failed to insert metric_staleness rule: %v", err)
	}

	connID := insertTestConnection(t, pool, "audit-staleness")

	if _, err := pool.Exec(ctx, insertProbeConfigSQL,
		"pg_stat_activity", 60); err != nil {
		t.Fatalf("failed to insert probe config: %v", err)
	}
	// 30 minutes stale against a 60 second interval is a ratio of 30,
	// well above the seeded threshold of 3.
	if _, err := pool.Exec(ctx, insertProbeAvailabilitySQL,
		connID, "pg_stat_activity", "30 minutes"); err != nil {
		t.Fatalf("failed to insert probe availability: %v", err)
	}

	return ruleID, connID
}

// TestAuditC1StalenessAlertFireClearLoop verifies audit claim C1. The
// metric_staleness rule stores its alerts with alert_type='threshold'
// and a non-NULL rule_id, so the alert cleaner picks them up. The
// cleaner resolves the alert's metric name, "probe_staleness_ratio",
// through GetLatestMetricValues, which has no registry entry for it
// and therefore returns an error. checkAlertResolved treats that error
// as "the condition no longer exists" and clears the alert, queueing
// an ALERT_CLEAR notification. evaluateMetricStaleness then recreates
// the alert on its next pass, because unlike triggerThresholdAlert it
// performs no recently-cleared cooldown check.
//
// The result is an unbounded fire/clear notification loop on stable
// input data, running at the cleaner's 30 second cadence and the
// evaluator's 60 second cadence.
//
// The staleness path SHOULD either be excluded from the cleaner (it
// has no registry metric) or SHOULD apply the same cooldown guard as
// triggerThresholdAlert.
func TestAuditC1StalenessAlertFireClearLoop(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installNotificationCapture(t, engine)
	ruleID, _ := seedStalenessFixture(t, pool)

	// Precondition: the metric the cleaner will look up is genuinely
	// unresolvable, which is what drives the error branch.
	if _, err := engine.datastore.GetLatestMetricValues(ctx,
		"probe_staleness_ratio"); err == nil {
		t.Fatal("expected probe_staleness_ratio to be unimplemented")
	} else if !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("unexpected error for probe_staleness_ratio: %v", err)
	}

	const cycles = 3
	for i := 0; i < cycles; i++ {
		engine.evaluateThresholds(ctx)
		engine.cleanResolvedAlerts(ctx)
	}

	rows, err := pool.Query(ctx, selectAlertsForRuleSQL, ruleID)
	if err != nil {
		t.Fatalf("failed to read alerts: %v", err)
	}
	defer rows.Close()

	var statuses []string
	for rows.Next() {
		var id int64
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("failed to scan alert: %v", err)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("row iteration error: %v", err)
	}

	// Current (defective) behavior: one brand new alert row per cycle,
	// every one of them already cleared. There SHOULD be a single
	// alert that stays active while the probe remains stale.
	if len(statuses) != cycles {
		t.Errorf("alert rows = %d, want %d (one created and cleared per cycle)",
			len(statuses), cycles)
	}
	for i, status := range statuses {
		if status != "cleared" {
			t.Errorf("alert %d status = %q, want \"cleared\"", i, status)
		}
	}

	jobs := capture.await(t, 2*cycles)
	counts := countTypes(jobs)
	if counts[database.NotificationTypeAlertFire] != cycles {
		t.Errorf("fire notifications = %d, want %d",
			counts[database.NotificationTypeAlertFire], cycles)
	}
	if counts[database.NotificationTypeAlertClear] != cycles {
		t.Errorf("clear notifications = %d, want %d",
			counts[database.NotificationTypeAlertClear], cycles)
	}
	t.Logf("after %d evaluate/clean cycles on stable data: %d alert rows, "+
		"%d fire notifications, %d clear notifications",
		cycles, len(statuses),
		counts[database.NotificationTypeAlertFire],
		counts[database.NotificationTypeAlertClear])
}

// TestAuditC1StalenessAlertFireClearLoopDemo asserts the behavior the
// engine SHOULD have: a persistently stale probe produces exactly one
// alert, which stays active and notifies once. It fails against the
// current code, so it is skipped unless ALERTER_DEFECT_DEMO=1.
func TestAuditC1StalenessAlertFireClearLoopDemo(t *testing.T) {
	engineDefectDemoEnabled(t)

	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installNotificationCapture(t, engine)
	ruleID, _ := seedStalenessFixture(t, pool)

	const cycles = 3
	for i := 0; i < cycles; i++ {
		engine.evaluateThresholds(ctx)
		engine.cleanResolvedAlerts(ctx)
	}

	var total, active int
	if err := pool.QueryRow(ctx, `
        SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'active')
        FROM alerts WHERE rule_id = $1
    `, ruleID).Scan(&total, &active); err != nil {
		t.Fatalf("failed to count alerts: %v", err)
	}

	if total != 1 {
		t.Errorf("alert rows = %d, want 1 for a continuously stale probe", total)
	}
	if active != 1 {
		t.Errorf("active alerts = %d, want 1; the probe is still stale", active)
	}

	jobs := capture.await(t, 1)
	if counts := countTypes(jobs); counts[database.NotificationTypeAlertClear] != 0 {
		t.Errorf("clear notifications = %d, want 0 while the probe is stale",
			counts[database.NotificationTypeAlertClear])
	}
}

// TestAuditC1StalenessPathSkipsCooldownGuard isolates the second half
// of claim C1: evaluateMetricStaleness performs no
// GetRecentlyClearedAlert check, so it re-fires immediately after a
// clear, whereas triggerThresholdAlert suppresses a re-fire for
// AlertCooldownPeriod. The two paths are driven back to back on the
// same engine to make the asymmetry explicit.
func TestAuditC1StalenessPathSkipsCooldownGuard(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installNotificationCapture(t, engine)

	stalenessRuleID, stalenessConnID := seedStalenessFixture(t, pool)

	// Control: a standard registry-backed rule that flows through
	// triggerThresholdAlert.
	ruleIDs := seedSpockBuiltInRules(t, pool)
	slotRuleID := ruleIDs["replication_slot_retention_warn"]
	slotConnID := insertTestConnection(t, pool, "audit-cooldown-control")
	if _, err := pool.Exec(ctx, insertSlotSampleSQL,
		slotConnID, "slot_a", false, int64(5_000_000_000)); err != nil {
		t.Fatalf("failed to seed replication slot sample: %v", err)
	}

	// First pass creates both alerts.
	engine.evaluateThresholds(ctx)

	stalenessAlert, err := ds.GetActiveThresholdAlert(ctx, stalenessRuleID,
		stalenessConnID, nil)
	if err != nil || stalenessAlert == nil {
		t.Fatalf("expected a staleness alert after the first pass "+
			"(alert=%v err=%v)", stalenessAlert, err)
	}
	slotAlert, err := ds.GetActiveThresholdAlert(ctx, slotRuleID,
		slotConnID, nil)
	if err != nil || slotAlert == nil {
		t.Fatalf("expected a slot retention alert after the first pass "+
			"(alert=%v err=%v)", slotAlert, err)
	}

	// Clear both alerts right now, so both sit well inside
	// AlertCooldownPeriod.
	if err := ds.ClearAlert(ctx, stalenessAlert.ID); err != nil {
		t.Fatalf("failed to clear staleness alert: %v", err)
	}
	if err := ds.ClearAlert(ctx, slotAlert.ID); err != nil {
		t.Fatalf("failed to clear slot alert: %v", err)
	}

	// Sanity check that the cooldown lookup sees both clears.
	for name, spec := range map[string]struct {
		ruleID int64
		connID int
	}{
		"staleness": {stalenessRuleID, stalenessConnID},
		"slot":      {slotRuleID, slotConnID},
	} {
		recent, err := ds.GetRecentlyClearedAlert(ctx, spec.ruleID,
			spec.connID, nil, AlertCooldownPeriod)
		if err != nil {
			t.Fatalf("GetRecentlyClearedAlert(%s) failed: %v", name, err)
		}
		if !recent {
			t.Fatalf("expected %s alert to be inside the cooldown window", name)
		}
	}

	// Second pass on unchanged data.
	engine.evaluateThresholds(ctx)

	newStaleness, err := ds.GetActiveThresholdAlert(ctx, stalenessRuleID,
		stalenessConnID, nil)
	if err != nil {
		t.Fatalf("GetActiveThresholdAlert(staleness) failed: %v", err)
	}
	newSlot, err := ds.GetActiveThresholdAlert(ctx, slotRuleID, slotConnID, nil)
	if err != nil {
		t.Fatalf("GetActiveThresholdAlert(slot) failed: %v", err)
	}

	// Current (defective) behavior: the staleness path re-fires
	// immediately. It SHOULD respect AlertCooldownPeriod like the
	// control rule does.
	if newStaleness == nil {
		t.Error("expected the staleness path to re-fire inside the cooldown")
	} else if newStaleness.ID == stalenessAlert.ID {
		t.Errorf("expected a new staleness alert row, got the cleared one (%d)",
			newStaleness.ID)
	}
	if newSlot != nil {
		t.Errorf("control rule unexpectedly re-fired inside the cooldown "+
			"(alert %d)", newSlot.ID)
	}
}

// TestAuditC2ArchiverRuleErrorIsSwallowed verifies the engine half of
// audit claim C2: when the metric query fails because
// metrics.pg_stat_archiver does not exist,
// evaluateRuleForAllConnections logs at debug level and returns. With
// debug disabled - the production default - nothing reaches the log
// and the wal_archive_failed rule is silently inert.
//
// The evaluator SHOULD distinguish "no data" from a query error and
// surface the latter.
func TestAuditC2ArchiverRuleErrorIsSwallowed(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installNotificationCapture(t, engine)

	var ruleID int64
	if err := pool.QueryRow(ctx, insertArchiverRuleSQL).Scan(&ruleID); err != nil {
		t.Fatalf("failed to insert wal_archive_failed rule: %v", err)
	}
	insertTestConnection(t, pool, "audit-c2-engine")

	output := captureStderr(t, func() {
		engine.evaluateThresholds(ctx)
	})

	if strings.Contains(output, "pg_stat_archiver") {
		t.Errorf("expected the archiver query failure to be swallowed, "+
			"but stderr mentioned it: %s", output)
	}
	if strings.Contains(output, "ERROR") {
		t.Errorf("expected no ERROR log for the failing rule, got: %s", output)
	}

	var alertCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alerts WHERE rule_id = $1`,
		ruleID).Scan(&alertCount); err != nil {
		t.Fatalf("failed to count alerts: %v", err)
	}
	if alertCount != 0 {
		t.Errorf("alerts for wal_archive_failed = %d, want 0", alertCount)
	}
}

// TestAuditC8CacheHitRatioFiresAndClearsOnSameData verifies the engine
// half of audit claim C8. The cache_hit_ratio metric returns one row
// per delta interval in the 15 minute window with no ordering
// guarantee. evaluateRuleForAllConnections fires when ANY row breaches
// the threshold, while checkAlertResolved inspects only the first
// matching row. When the breaching interval is not the first row, the
// evaluator and the cleaner disagree on identical data and the alert
// flaps once per cleaner tick.
//
// The metric SHOULD reduce to the latest interval so both sides read
// the same value.
func TestAuditC8CacheHitRatioFiresAndClearsOnSameData(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installNotificationCapture(t, engine)

	if _, err := pool.Exec(ctx, `
        CREATE TABLE metrics.pg_stat_database (
            connection_id INTEGER NOT NULL,
            database_name VARCHAR(255) NOT NULL,
            datname TEXT,
            blks_hit BIGINT,
            blks_read BIGINT,
            collected_at TIMESTAMPTZ NOT NULL
        )
    `); err != nil {
		t.Fatalf("failed to create metrics.pg_stat_database: %v", err)
	}

	var ruleID int64
	if err := pool.QueryRow(ctx, insertCacheHitRuleSQL).Scan(&ruleID); err != nil {
		t.Fatalf("failed to insert cache_hit_ratio_low rule: %v", err)
	}
	connID := insertTestConnection(t, pool, "audit-c8-engine")

	// Two healthy intervals followed by a cold one. The healthy rows
	// sort first, so the cleaner sees 100% while the evaluator sees
	// the 0% row.
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
		if _, err := pool.Exec(ctx, `
            INSERT INTO metrics.pg_stat_database
                (connection_id, database_name, datname, blks_hit, blks_read,
                 collected_at)
            VALUES ($1, 'appdb', 'appdb', $2, $3, NOW() - $4::interval)
        `, connID, s.hits, s.reads, s.offset); err != nil {
			t.Fatalf("failed to seed pg_stat_database: %v", err)
		}
	}

	values, err := ds.GetLatestMetricValues(ctx,
		"pg_stat_database.cache_hit_ratio")
	if err != nil {
		t.Fatalf("GetLatestMetricValues failed: %v", err)
	}
	if len(values) < 2 {
		t.Fatalf("expected several unreduced rows, got %d", len(values))
	}
	if values[0].Value < 80 {
		t.Fatalf("test setup expects the first row to be healthy, got %v",
			values[0].Value)
	}

	// Cycle the evaluator and the cleaner over unchanged data.
	const cycles = 2
	for i := 0; i < cycles; i++ {
		engine.evaluateThresholds(ctx)

		alert, err := ds.GetActiveThresholdAlert(ctx, ruleID, connID, strPtr("appdb"))
		if err != nil {
			t.Fatalf("GetActiveThresholdAlert failed: %v", err)
		}
		if alert == nil {
			t.Fatalf("cycle %d: expected the evaluator to fire on the "+
				"violating row", i)
		}

		engine.cleanResolvedAlerts(ctx)

		var status string
		if err := pool.QueryRow(ctx, selectAlertStatusSQL,
			alert.ID).Scan(&status); err != nil {
			t.Fatalf("failed to read alert status: %v", err)
		}
		// Current (defective) behavior: the cleaner clears the alert
		// the evaluator just raised, on byte-identical data. The
		// alert SHOULD stay active while the latest interval is cold.
		if status != "cleared" {
			t.Fatalf("cycle %d: alert status = %q, want \"cleared\" "+
				"(evaluator and cleaner disagree)", i, status)
		}

		// The cooldown guard on triggerThresholdAlert bounds the flap
		// rate but not the flap itself, so advance the cleared_at
		// timestamp to model the next cycle outside the cooldown.
		if _, err := pool.Exec(ctx, `
            UPDATE alerts SET cleared_at = NOW() - $1::interval WHERE id = $2
        `, "1 hour", alert.ID); err != nil {
			t.Fatalf("failed to age cleared_at: %v", err)
		}
	}

	jobs := capture.await(t, 2*cycles)
	counts := countTypes(jobs)
	if counts[database.NotificationTypeAlertFire] != cycles ||
		counts[database.NotificationTypeAlertClear] != cycles {
		t.Errorf("notifications = %v, want %d fire and %d clear",
			counts, cycles, cycles)
	}
}

// Additional seed statements for the anomaly-side audit tests.
const (
	insertAnomalyRuleForMetricSQL = `
        INSERT INTO alert_rules
            (name, description, category, metric_name, default_operator,
             default_threshold, default_severity, default_enabled, is_built_in)
        VALUES ($1, 'Audit rule', 'audit', $2, '>', 0, 'warning', TRUE, FALSE)
    `

	createSlotsTableSQL = `
        CREATE TABLE metrics.pg_replication_slots (
            connection_id INTEGER NOT NULL,
            slot_name TEXT NOT NULL,
            active BOOLEAN,
            retained_bytes NUMERIC,
            collected_at TIMESTAMPTZ NOT NULL
        )
    `

	createStatDatabaseTableSQL = `
        CREATE TABLE metrics.pg_stat_database (
            connection_id INTEGER NOT NULL,
            database_name VARCHAR(255) NOT NULL,
            datname TEXT,
            deadlocks BIGINT,
            collected_at TIMESTAMPTZ NOT NULL
        )
    `

	selectBaselineRowSQL = `
        SELECT sample_count, stddev, earliest_sample_at
        FROM metric_baselines
        WHERE connection_id = $1 AND metric_name = $2 AND period_type = 'all'
    `

	selectCandidateDatabaseSQL = `
        SELECT database_name, metric_value
        FROM anomaly_candidates
        WHERE connection_id = $1 AND metric_name = $2
        ORDER BY id
    `
)

// seedAuditBaseline upserts a baseline row through the datastore.
func seedAuditBaseline(t *testing.T, ds *database.Datastore, b *database.MetricBaseline) {
	t.Helper()
	if err := ds.UpsertMetricBaseline(context.Background(), b); err != nil {
		t.Fatalf("UpsertMetricBaseline(%s) failed: %v", b.PeriodType, err)
	}
}

// TestAuditC6DetectAnomaliesIgnoresTimeAwareBaselines verifies the
// engine half of audit claim C6. detectAnomalies reads baselines[0]
// from GetMetricBaselines, which orders by the TEXT column
// period_type. 'all' sorts before 'daily' and 'hourly', so the global
// baseline always wins and the hourly and daily baselines the
// baseline calculator writes are never consulted.
//
// detectAnomalies SHOULD select the baseline matching the current hour
// or weekday and fall back to 'all'.
func TestAuditC6DetectAnomaliesIgnoresTimeAwareBaselines(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := pool.Exec(ctx, insertAnomalyAlertRuleSQL,
		"audit_c6_rule", "pg_settings.max_connections"); err != nil {
		t.Fatalf("failed to insert alert rule: %v", err)
	}

	cases := []struct {
		name string
		// allSampleCount and allEarliest control whether the 'all'
		// baseline passes the warmup gate.
		allSampleCount int64
		allEarliest    time.Time
		allStdDev      float64
	}{
		{
			// Both baselines are warm, but the 'all' baseline is so
			// wide that the current value is unremarkable against it,
			// while the hourly baseline would flag it instantly.
			name:           "warm wide all baseline masks tight hourly baseline",
			allSampleCount: 500,
			allEarliest:    now.Add(-10 * 24 * time.Hour),
			allStdDev:      1000,
		},
		{
			// The 'all' baseline has not warmed up, so detection is
			// suppressed entirely even though the hourly baseline is
			// mature.
			name:           "cold all baseline suppresses warm hourly baseline",
			allSampleCount: 5,
			allEarliest:    now.Add(-1 * time.Hour),
			allStdDev:      1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx,
				`TRUNCATE anomaly_candidates RESTART IDENTITY`); err != nil {
				t.Fatalf("truncate anomaly_candidates failed: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM metric_baselines`); err != nil {
				t.Fatalf("delete metric_baselines failed: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM metrics.pg_settings`); err != nil {
				t.Fatalf("delete metrics.pg_settings failed: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM connections`); err != nil {
				t.Fatalf("delete connections failed: %v", err)
			}

			var connID int
			if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
				"audit-c6").Scan(&connID); err != nil {
				t.Fatalf("failed to insert connection: %v", err)
			}
			if _, err := pool.Exec(ctx, insertAnomalyPgSettingsSQL,
				connID, "500"); err != nil {
				t.Fatalf("failed to insert pg_settings sample: %v", err)
			}

			seedAuditBaseline(t, ds, &database.MetricBaseline{
				ConnectionID:     connID,
				MetricName:       "pg_settings.max_connections",
				PeriodType:       "all",
				Mean:             100,
				StdDev:           tc.allStdDev,
				Min:              0,
				Max:              200,
				SampleCount:      tc.allSampleCount,
				LastCalculated:   now,
				EarliestSampleAt: tc.allEarliest,
			})

			hour := time.Now().Hour()
			seedAuditBaseline(t, ds, &database.MetricBaseline{
				ConnectionID:     connID,
				MetricName:       "pg_settings.max_connections",
				PeriodType:       "hourly",
				HourOfDay:        &hour,
				Mean:             100,
				StdDev:           1,
				Min:              99,
				Max:              101,
				SampleCount:      500,
				LastCalculated:   now,
				EarliestSampleAt: now.Add(-10 * 24 * time.Hour),
			})

			// Confirm the ordering assumption before relying on it.
			baselines, err := ds.GetMetricBaselines(ctx, connID,
				"pg_settings.max_connections")
			if err != nil {
				t.Fatalf("GetMetricBaselines failed: %v", err)
			}
			if len(baselines) != 2 {
				t.Fatalf("expected 2 baselines, got %d", len(baselines))
			}
			if baselines[0].PeriodType != "all" {
				t.Fatalf("baselines[0].PeriodType = %q, want \"all\"",
					baselines[0].PeriodType)
			}

			engine.detectAnomalies(ctx)

			var count int
			if err := pool.QueryRow(ctx, selectAnomalyCountByConnSQL,
				connID).Scan(&count); err != nil {
				t.Fatalf("failed to count candidates: %v", err)
			}

			// Current (defective) behavior: no candidate, because only
			// the 'all' baseline is consulted. A time-aware detector
			// SHOULD emit one, since 500 is 400 standard deviations
			// away from the hourly baseline.
			if count != 0 {
				t.Errorf("anomaly candidates = %d, want 0 "+
					"(only the 'all' baseline is consulted)", count)
			}
		})
	}
}

// TestAuditC6DetectAnomaliesIgnoresTimeAwareBaselinesDemo asserts the
// behavior detection SHOULD have: a value that is wildly anomalous
// against a mature hourly baseline must produce a candidate. It fails
// against the current code, so it is skipped unless
// ALERTER_DEFECT_DEMO=1.
func TestAuditC6DetectAnomaliesIgnoresTimeAwareBaselinesDemo(t *testing.T) {
	engineDefectDemoEnabled(t)

	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := pool.Exec(ctx, insertAnomalyAlertRuleSQL,
		"audit_c6_demo_rule", "pg_settings.max_connections"); err != nil {
		t.Fatalf("failed to insert alert rule: %v", err)
	}

	var connID int
	if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
		"audit-c6-demo").Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}
	if _, err := pool.Exec(ctx, insertAnomalyPgSettingsSQL,
		connID, "500"); err != nil {
		t.Fatalf("failed to insert pg_settings sample: %v", err)
	}

	seedAuditBaseline(t, ds, &database.MetricBaseline{
		ConnectionID:     connID,
		MetricName:       "pg_settings.max_connections",
		PeriodType:       "all",
		Mean:             100,
		StdDev:           1000,
		Min:              0,
		Max:              200,
		SampleCount:      500,
		LastCalculated:   now,
		EarliestSampleAt: now.Add(-10 * 24 * time.Hour),
	})
	hour := time.Now().Hour()
	seedAuditBaseline(t, ds, &database.MetricBaseline{
		ConnectionID:     connID,
		MetricName:       "pg_settings.max_connections",
		PeriodType:       "hourly",
		HourOfDay:        &hour,
		Mean:             100,
		StdDev:           1,
		Min:              99,
		Max:              101,
		SampleCount:      500,
		LastCalculated:   now,
		EarliestSampleAt: now.Add(-10 * 24 * time.Hour),
	})

	engine.detectAnomalies(ctx)

	var count int
	if err := pool.QueryRow(ctx, selectAnomalyCountByConnSQL,
		connID).Scan(&count); err != nil {
		t.Fatalf("failed to count candidates: %v", err)
	}
	if count == 0 {
		t.Fatal("expected an anomaly candidate from the mature hourly " +
			"baseline, got none")
	}
}

// TestAuditC7FallbackBaselineCanNeverWarm verifies audit claim C7.
// Metrics whose registry entry has an empty historicalSQL make
// GetHistoricalMetricValues fail, so calculateBaselines falls back to
// calculateGlobalBaselinesFallback. That path derives a baseline from
// the single current sample: sample_count is 1, stddev is 0, and
// earliest_sample_at is left NULL. isBaselineWarm rejects such a row
// under every default warmup profile, so anomaly detection is
// permanently disabled for those metrics.
//
// The fallback SHOULD either populate a real sample history or be
// skipped entirely rather than writing an unusable baseline.
func TestAuditC7FallbackBaselineCanNeverWarm(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()

	if _, err := pool.Exec(ctx, createSlotsTableSQL); err != nil {
		t.Fatalf("failed to create metrics.pg_replication_slots: %v", err)
	}
	// pg_replication_slots.inactive is one of the registry entries
	// with an empty historicalSQL.
	if _, err := pool.Exec(ctx, insertAnomalyRuleForMetricSQL,
		"audit_c7_rule", "pg_replication_slots.inactive"); err != nil {
		t.Fatalf("failed to insert alert rule: %v", err)
	}

	var connID int
	if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
		"audit-c7").Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}
	if _, err := pool.Exec(ctx, `
        INSERT INTO metrics.pg_replication_slots
            (connection_id, slot_name, active, retained_bytes, collected_at)
        VALUES ($1, 'slot_a', FALSE, 0, NOW())
    `, connID); err != nil {
		t.Fatalf("failed to seed replication slot: %v", err)
	}

	// The historical query must fail; that is what routes the metric
	// onto the fallback path.
	if _, err := ds.GetHistoricalMetricValues(ctx,
		"pg_replication_slots.inactive", 7); err == nil {
		t.Fatal("expected GetHistoricalMetricValues to fail for a metric " +
			"with no historical SQL")
	}

	engine.calculateBaselines(ctx)

	var sampleCount int64
	var stddev float64
	var earliest *time.Time
	if err := pool.QueryRow(ctx, selectBaselineRowSQL,
		connID, "pg_replication_slots.inactive").Scan(
		&sampleCount, &stddev, &earliest); err != nil {
		t.Fatalf("failed to read fallback baseline: %v", err)
	}

	// Current (defective) behavior.
	if sampleCount != 1 {
		t.Errorf("fallback sample_count = %d, want 1", sampleCount)
	}
	if stddev != 0 {
		t.Errorf("fallback stddev = %v, want 0", stddev)
	}
	if earliest != nil {
		t.Errorf("fallback earliest_sample_at = %v, want NULL", *earliest)
	}

	baselines, err := ds.GetMetricBaselines(ctx, connID,
		"pg_replication_slots.inactive")
	if err != nil {
		t.Fatalf("GetMetricBaselines failed: %v", err)
	}
	if len(baselines) != 1 {
		t.Fatalf("expected exactly one fallback baseline, got %d", len(baselines))
	}
	if !baselines[0].EarliestSampleAt.IsZero() {
		t.Errorf("EarliestSampleAt = %v, want the zero time",
			baselines[0].EarliestSampleAt)
	}

	// The warmup gate rejects the fallback row under every default
	// profile, so no amount of waiting makes it usable.
	warmup := engine.getConfig().Anomaly.Tier1.Warmup
	for _, periodType := range []string{"all", "hourly", "daily"} {
		b := *baselines[0]
		b.PeriodType = periodType
		if isBaselineWarm(b, warmup, time.Now().Add(365*24*time.Hour)) {
			t.Errorf("isBaselineWarm(%s) = true for the fallback baseline, "+
				"want false even a year later", periodType)
		}
	}

	// End to end: detection emits nothing for this metric.
	engine.detectAnomalies(ctx)
	var count int
	if err := pool.QueryRow(ctx, selectAnomalyCountByConnSQL,
		connID).Scan(&count); err != nil {
		t.Fatalf("failed to count candidates: %v", err)
	}
	if count != 0 {
		t.Errorf("anomaly candidates = %d, want 0", count)
	}
}

// TestAuditC10AnomalyCandidateDropsDatabaseName verifies audit claim
// C10. For per-database metrics the baseline calculator writes one
// baseline row per (connection, database), but detectAnomalies fetches
// baselines by connection and metric only, picks the first metric
// value whose connection matches regardless of database, and never
// assigns DatabaseName on the candidate it creates.
//
// Detection SHOULD pair each per-database value with the baseline for
// that database and record the database on the candidate.
func TestAuditC10AnomalyCandidateDropsDatabaseName(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := pool.Exec(ctx, createStatDatabaseTableSQL); err != nil {
		t.Fatalf("failed to create metrics.pg_stat_database: %v", err)
	}
	if _, err := pool.Exec(ctx, insertAnomalyRuleForMetricSQL,
		"audit_c10_rule", "pg_stat_database.deadlocks_delta"); err != nil {
		t.Fatalf("failed to insert alert rule: %v", err)
	}

	var connID int
	if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
		"audit-c10").Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}

	// Two databases on one connection with very different deadlock
	// deltas: alpha jumps by 900, beta by 1.
	samples := []struct {
		dbName    string
		deadlocks []int64
	}{
		{"alpha", []int64{100, 1000}},
		{"beta", []int64{5, 6}},
	}
	offsets := []string{"10 minutes", "1 minute"}
	for _, s := range samples {
		for i, offset := range offsets {
			if _, err := pool.Exec(ctx, `
                INSERT INTO metrics.pg_stat_database
                    (connection_id, database_name, datname, deadlocks,
                     collected_at)
                VALUES ($1, $2, $3, $4, NOW() - $5::interval)
            `, connID, s.dbName, s.dbName, s.deadlocks[i], offset); err != nil {
				t.Fatalf("failed to seed pg_stat_database: %v", err)
			}
		}
	}

	values, err := ds.GetLatestMetricValues(ctx,
		"pg_stat_database.deadlocks_delta")
	if err != nil {
		t.Fatalf("GetLatestMetricValues failed: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("expected one row per database, got %d", len(values))
	}
	firstDB := "<nil>"
	if values[0].DatabaseName != nil {
		firstDB = *values[0].DatabaseName
	}
	t.Logf("detection will use the first row: database=%s value=%v",
		firstDB, values[0].Value)

	// The baseline calculator does key its output on the database:
	// running it over this data writes one 'all' baseline per database.
	engine.calculateBaselines(ctx)
	var perDatabaseBaselines int
	if err := pool.QueryRow(ctx, `
        SELECT COUNT(DISTINCT database_name)
        FROM metric_baselines
        WHERE connection_id = $1 AND metric_name = $2
          AND database_name IS NOT NULL
    `, connID, "pg_stat_database.deadlocks_delta").Scan(
		&perDatabaseBaselines); err != nil {
		t.Fatalf("failed to count per-database baselines: %v", err)
	}
	if perDatabaseBaselines != 2 {
		t.Errorf("distinct baseline database names = %d, want 2",
			perDatabaseBaselines)
	}

	// Replace them with a single warm baseline for database "beta"
	// only, so the mismatch between the baseline's database and the
	// value detection actually uses is unambiguous.
	if _, err := pool.Exec(ctx, `DELETE FROM metric_baselines`); err != nil {
		t.Fatalf("failed to reset baselines: %v", err)
	}
	betaName := "beta"
	seedAuditBaseline(t, ds, &database.MetricBaseline{
		ConnectionID:     connID,
		DatabaseName:     &betaName,
		MetricName:       "pg_stat_database.deadlocks_delta",
		PeriodType:       "all",
		Mean:             1,
		StdDev:           0.5,
		Min:              0,
		Max:              2,
		SampleCount:      500,
		LastCalculated:   now,
		EarliestSampleAt: now.Add(-10 * 24 * time.Hour),
	})

	engine.detectAnomalies(ctx)

	rows, err := pool.Query(ctx, selectCandidateDatabaseSQL,
		connID, "pg_stat_database.deadlocks_delta")
	if err != nil {
		t.Fatalf("failed to read candidates: %v", err)
	}
	defer rows.Close()

	type candidateRow struct {
		dbName *string
		value  float64
	}
	var candidates []candidateRow
	for rows.Next() {
		var c candidateRow
		if err := rows.Scan(&c.dbName, &c.value); err != nil {
			t.Fatalf("failed to scan candidate: %v", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("row iteration error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected exactly one candidate, got %d", len(candidates))
	}

	// Current (defective) behavior: the candidate carries no database
	// name even though the metric and the baseline are per-database.
	// It SHOULD record the database the value came from.
	if candidates[0].dbName != nil {
		t.Errorf("candidate database_name = %q, want NULL "+
			"(the field is never assigned)", *candidates[0].dbName)
	}

	// The value is taken from whichever row the query returned first,
	// not from the database the baseline describes.
	if candidates[0].value != float32ish(values[0].Value) {
		t.Errorf("candidate metric_value = %v, want %v (the first row)",
			candidates[0].value, values[0].Value)
	}
}

// float32ish rounds a float64 through float32, matching the REAL
// column type used by anomaly_candidates.metric_value.
func float32ish(v float64) float64 {
	return float64(float32(v))
}

// TestAuditC7FallbackBaselineCanNeverWarmDemo asserts the behavior the
// baseline calculator SHOULD have: any baseline it persists must carry
// the timestamp of its earliest sample, so the warmup gate can
// eventually admit it. It fails against the current code, so it is
// skipped unless ALERTER_DEFECT_DEMO=1.
func TestAuditC7FallbackBaselineCanNeverWarmDemo(t *testing.T) {
	engineDefectDemoEnabled(t)

	engine, _, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()

	if _, err := pool.Exec(ctx, createSlotsTableSQL); err != nil {
		t.Fatalf("failed to create metrics.pg_replication_slots: %v", err)
	}
	if _, err := pool.Exec(ctx, insertAnomalyRuleForMetricSQL,
		"audit_c7_demo_rule", "pg_replication_slots.inactive"); err != nil {
		t.Fatalf("failed to insert alert rule: %v", err)
	}

	var connID int
	if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
		"audit-c7-demo").Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}
	if _, err := pool.Exec(ctx, `
        INSERT INTO metrics.pg_replication_slots
            (connection_id, slot_name, active, retained_bytes, collected_at)
        VALUES ($1, 'slot_a', FALSE, 0, NOW())
    `, connID); err != nil {
		t.Fatalf("failed to seed replication slot: %v", err)
	}

	engine.calculateBaselines(ctx)

	var sampleCount int64
	var stddev float64
	var earliest *time.Time
	if err := pool.QueryRow(ctx, selectBaselineRowSQL,
		connID, "pg_replication_slots.inactive").Scan(
		&sampleCount, &stddev, &earliest); err != nil {
		t.Fatalf("failed to read fallback baseline: %v", err)
	}
	if earliest == nil {
		t.Fatal("persisted baseline has a NULL earliest_sample_at, so the " +
			"warmup gate can never admit it")
	}
}
