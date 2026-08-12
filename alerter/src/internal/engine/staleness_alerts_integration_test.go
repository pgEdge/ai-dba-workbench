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
	"github.com/pgedge/ai-workbench/pkg/worker"
)

// Seed statements used by the staleness tests. They are named constants
// so the Codacy/Semgrep go_sql_rule-concat-sqli rule does not flag inline
// multi-line SQL passed to Exec or QueryRow; every value is still bound
// via $N.
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

	refreshProbeAvailabilitySQL = `
        UPDATE probe_availability
           SET last_collected = NOW()
         WHERE connection_id = $1 AND probe_name = $2
    `

	deleteProbeAvailabilitySQL = `
        DELETE FROM probe_availability
         WHERE connection_id = $1 AND probe_name = $2
    `

	selectAlertsForRuleSQL = `
        SELECT id, status, probe_name
        FROM alerts
        WHERE rule_id = $1
        ORDER BY id
    `

	insertUnsupportedMetricAlertSQL = `
        INSERT INTO alerts
            (alert_type, rule_id, connection_id, metric_name, metric_value,
             threshold_value, operator, severity, title, description, status,
             triggered_at)
        VALUES ('threshold', $1, $2, $3, 42, 10, '>', 'warning',
                'unsupported metric alert', 'desc', 'active', NOW())
        RETURNING id
    `
)

// stalenessNotificationCapture records the notification jobs the engine
// submits to its worker pool. The pool handler forwards each job onto a
// buffered channel so tests can await an exact number of jobs without
// polling.
type stalenessNotificationCapture struct {
	jobs chan notificationJob
}

// installStalenessNotificationCapture replaces the engine's notification
// worker pool with one whose handler records every submitted job. The pool
// is stopped via t.Cleanup so no goroutine outlives the test.
//
// The engine's own pool is only created when a notification manager is
// configured; the integration environments in this package build the
// engine without one, so this helper is the only way to observe
// queueNotification.
func installStalenessNotificationCapture(t *testing.T, e *Engine) *stalenessNotificationCapture {
	t.Helper()

	capture := &stalenessNotificationCapture{jobs: make(chan notificationJob, 256)}
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

// drain reads every job that has arrived, waiting a short grace period for
// stragglers, and summarizes the result by notification type.
func (c *stalenessNotificationCapture) drain(t *testing.T) map[database.NotificationType]int {
	t.Helper()

	counts := make(map[database.NotificationType]int)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case job := <-c.jobs:
			counts[job.notifTyp]++
		case <-time.After(300 * time.Millisecond):
			return counts
		case <-deadline:
			return counts
		}
	}
}

// seedStalenessRule inserts the metric_staleness rule and returns its id.
func seedStalenessRule(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var ruleID int64
	if err := pool.QueryRow(context.Background(), insertStalenessRuleSQL).Scan(&ruleID); err != nil {
		t.Fatalf("failed to insert metric_staleness rule: %v", err)
	}
	return ruleID
}

// seedStaleProbe registers a probe with the given collection interval and
// records a last collection far enough in the past to breach the default
// staleness ratio of 3.
func seedStaleProbe(t *testing.T, pool *pgxpool.Pool, connID int, probeName, staleFor string) {
	t.Helper()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, insertProbeConfigSQL, probeName, 60); err != nil {
		t.Fatalf("failed to insert probe config for %s: %v", probeName, err)
	}
	if _, err := pool.Exec(ctx, insertProbeAvailabilitySQL, connID, probeName, staleFor); err != nil {
		t.Fatalf("failed to insert probe availability for %s: %v", probeName, err)
	}
}

// stalenessAlertRow is a minimal projection of the alerts table used by the
// assertions below.
type stalenessAlertRow struct {
	id     int64
	status string
	probe  *string
}

// readAlertsForRule returns every alert row belonging to a rule, oldest first.
func readAlertsForRule(t *testing.T, pool *pgxpool.Pool, ruleID int64) []stalenessAlertRow {
	t.Helper()

	rows, err := pool.Query(context.Background(), selectAlertsForRuleSQL, ruleID)
	if err != nil {
		t.Fatalf("failed to read alerts: %v", err)
	}
	defer rows.Close()

	var out []stalenessAlertRow
	for rows.Next() {
		var row stalenessAlertRow
		if err := rows.Scan(&row.id, &row.status, &row.probe); err != nil {
			t.Fatalf("failed to scan alert: %v", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("row iteration error: %v", err)
	}
	return out
}

// TestStalenessAlertDoesNotFireClearLoop is the regression test for issue
// #405. A continuously stale probe must produce exactly one alert that
// stays active, rather than one created-and-cleared alert per evaluate and
// clean cycle.
func TestStalenessAlertDoesNotFireClearLoop(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-loop")
	// 30 minutes stale against a 60 second interval is a ratio of 30,
	// well above the seeded threshold of 3.
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	const cycles = 3
	for i := 0; i < cycles; i++ {
		engine.evaluateThresholds(ctx)
		engine.cleanResolvedAlerts(ctx)
	}

	alerts := readAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1 for a continuously stale probe", len(alerts))
	}
	if alerts[0].status != "active" {
		t.Errorf("alert status = %q, want \"active\" while the probe is stale",
			alerts[0].status)
	}

	counts := capture.drain(t)
	if counts[database.NotificationTypeAlertFire] != 1 {
		t.Errorf("fire notifications = %d, want 1",
			counts[database.NotificationTypeAlertFire])
	}
	if counts[database.NotificationTypeAlertClear] != 0 {
		t.Errorf("clear notifications = %d, want 0 while the probe is stale",
			counts[database.NotificationTypeAlertClear])
	}
}

// TestStalenessAlertClearsWhenProbeRecovers verifies that the bespoke
// resolution path still clears the alert once the probe collects again, so
// suppressing the spurious clears does not leave the alert latched.
func TestStalenessAlertClearsWhenProbeRecovers(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-recovery")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	engine.evaluateThresholds(ctx)
	engine.cleanResolvedAlerts(ctx)

	alerts := readAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 || alerts[0].status != "active" {
		t.Fatalf("expected one active alert, got %+v", alerts)
	}

	// The probe collects again, so the staleness ratio drops below the
	// threshold.
	if _, err := pool.Exec(ctx, refreshProbeAvailabilitySQL, connID,
		"pg_stat_activity"); err != nil {
		t.Fatalf("failed to refresh probe availability: %v", err)
	}

	engine.cleanResolvedAlerts(ctx)

	alerts = readAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(alerts))
	}
	if alerts[0].status != "cleared" {
		t.Errorf("alert status = %q, want \"cleared\" after the probe recovered",
			alerts[0].status)
	}

	counts := capture.drain(t)
	if counts[database.NotificationTypeAlertClear] != 1 {
		t.Errorf("clear notifications = %d, want 1",
			counts[database.NotificationTypeAlertClear])
	}
}

// TestStalenessAlertClearsWhenProbeStopsReporting covers the case where the
// probe disappears from the staleness view entirely, for example because it
// was disabled or its connection is no longer monitored.
func TestStalenessAlertClearsWhenProbeStopsReporting(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-vanished")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	engine.evaluateThresholds(ctx)

	if _, err := pool.Exec(ctx, deleteProbeAvailabilitySQL, connID,
		"pg_stat_activity"); err != nil {
		t.Fatalf("failed to delete probe availability: %v", err)
	}

	engine.cleanResolvedAlerts(ctx)

	alerts := readAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(alerts))
	}
	if alerts[0].status != "cleared" {
		t.Errorf("alert status = %q, want \"cleared\" once the probe stopped reporting",
			alerts[0].status)
	}
}

// TestStalenessAlertPerProbe verifies that two stale probes on one
// connection raise one alert each, rather than collapsing into a single row
// whose title is overwritten by whichever probe was evaluated last.
func TestStalenessAlertPerProbe(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-multi-probe")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")
	seedStaleProbe(t, pool, connID, "pg_stat_database", "45 minutes")

	engine.evaluateThresholds(ctx)

	alerts := readAlertsForRule(t, pool, ruleID)
	if len(alerts) != 2 {
		t.Fatalf("alert rows = %d, want 2 (one per stale probe)", len(alerts))
	}

	seen := make(map[string]bool)
	for _, alert := range alerts {
		if alert.probe == nil {
			t.Fatalf("alert %d has no probe name", alert.id)
		}
		seen[*alert.probe] = true
	}
	for _, probe := range []string{"pg_stat_activity", "pg_stat_database"} {
		if !seen[probe] {
			t.Errorf("no alert raised for stale probe %s", probe)
		}
	}

	// A second pass updates the existing alerts instead of adding more.
	engine.evaluateThresholds(ctx)
	if alerts = readAlertsForRule(t, pool, ruleID); len(alerts) != 2 {
		t.Errorf("alert rows after a second pass = %d, want 2", len(alerts))
	}
}

// TestStalenessAlertRespectsCooldown verifies that the staleness path now
// applies the same recently-cleared guard as triggerThresholdAlert, so an
// alert cleared by an operator does not immediately re-fire.
func TestStalenessAlertRespectsCooldown(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-cooldown")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	engine.evaluateThresholds(ctx)

	alert, err := ds.GetActiveThresholdAlertForProbe(ctx, ruleID, connID,
		"pg_stat_activity")
	if err != nil || alert == nil {
		t.Fatalf("expected a staleness alert after the first pass (alert=%v err=%v)",
			alert, err)
	}
	if err := ds.ClearAlert(ctx, alert.ID); err != nil {
		t.Fatalf("failed to clear staleness alert: %v", err)
	}

	// Second pass on unchanged data, well inside AlertCooldownPeriod.
	engine.evaluateThresholds(ctx)

	refired, err := ds.GetActiveThresholdAlertForProbe(ctx, ruleID, connID,
		"pg_stat_activity")
	if err != nil {
		t.Fatalf("GetActiveThresholdAlertForProbe: %v", err)
	}
	if refired != nil {
		t.Errorf("staleness alert re-fired inside the cooldown window (alert %d)",
			refired.ID)
	}
}

// TestCheckAlertResolvedKeepsUnevaluableMetricAlerts verifies that an alert
// whose metric cannot be evaluated at all is left alone rather than being
// cleared. Treating an unusable metric as a resolution is what drove the
// notification loop in issue #405.
func TestCheckAlertResolvedKeepsUnevaluableMetricAlerts(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "unevaluable-metric")

	var alertID int64
	if err := pool.QueryRow(ctx, insertUnsupportedMetricAlertSQL, ruleID, connID,
		"metric_that_does_not_exist").Scan(&alertID); err != nil {
		t.Fatalf("failed to insert alert: %v", err)
	}

	engine.cleanResolvedAlerts(ctx)

	alerts := readAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(alerts))
	}
	if alerts[0].status != "active" {
		t.Errorf("alert status = %q, want \"active\"; an unusable metric says "+
			"nothing about the condition", alerts[0].status)
	}
	if counts := capture.drain(t); counts[database.NotificationTypeAlertClear] != 0 {
		t.Errorf("clear notifications = %d, want 0",
			counts[database.NotificationTypeAlertClear])
	}
}

// TestCheckAlertResolvedClearsWhenMetricReportsNoData pins the behavior
// that is deliberately unchanged: a registry metric that runs successfully
// and returns no rows still resolves the alert.
func TestCheckAlertResolvedClearsWhenMetricReportsNoData(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "empty-metric")

	var alertID int64
	if err := pool.QueryRow(ctx, insertUnsupportedMetricAlertSQL, ruleID, connID,
		"pg_replication_slots.inactive_count").Scan(&alertID); err != nil {
		t.Fatalf("failed to insert alert: %v", err)
	}

	engine.cleanResolvedAlerts(ctx)

	alerts := readAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(alerts))
	}
	if alerts[0].status != "cleared" {
		t.Errorf("alert status = %q, want \"cleared\" when the metric reports nothing",
			alerts[0].status)
	}
}
