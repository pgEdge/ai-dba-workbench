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

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// These tests cover threshold hysteresis (GitHub issue #614) against a
// real datastore: an alert is raised only after threshold.trigger_count
// consecutive breaching samples and cleared only after
// threshold.clear_count consecutive samples that do not breach, where a
// sample is a distinct collected_at and re-reading one counts for
// nothing.

// insertUnsupportedMetricRuleSQL seeds an enabled rule whose metric has no
// registry entry, so reading its values fails with an error other than
// ErrNoMetricData.
const insertUnsupportedMetricRuleSQL = `
        INSERT INTO alert_rules
            (name, description, category, metric_name, default_operator,
             default_threshold, default_severity, default_enabled, is_built_in)
        VALUES ('unsupported_metric_rule', 'A rule the registry cannot read',
                'test', 'no_such.metric', '>', 0, 'warning', TRUE, FALSE)
        RETURNING id
    `

// setThresholdCounts gives the engine a configuration with the given
// trigger and clear counts. The integration environments build the
// engine with no configuration, which behaves as if both were 1.
func setThresholdCounts(e *Engine, trigger, clear int) {
	cfg := config.NewConfig()
	cfg.Threshold.TriggerCount = trigger
	cfg.Threshold.ClearCount = clear
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = cfg
}

// newSlotInactiveRuleEnv builds the Spock engine environment with the
// replication_slot_inactive rule, whose value is 1 for any connection
// with an inactive slot in its newest sample.
func newSlotInactiveRuleEnv(t *testing.T) (*Engine, *database.Datastore, *pgxpool.Pool, int64, func()) {
	t.Helper()
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	var ruleID int64
	if err := pool.QueryRow(context.Background(), insertSlotInactiveRuleSQL).Scan(&ruleID); err != nil {
		cleanup()
		t.Fatalf("failed to insert replication_slot_inactive rule: %v", err)
	}
	return engine, ds, pool, ruleID, cleanup
}

// slotSample writes one pg_replication_slots sample the given distance in
// the past.
func slotSample(t *testing.T, pool *pgxpool.Pool, connID int, ago time.Duration, active bool) {
	t.Helper()
	insertReplicationSlotRow(t, pool, connID, time.Now().UTC().Add(-ago), "slot_a", active, 100)
}

// TestHysteresis_RaiseNeedsTriggerCountSamples covers the raise side: with
// trigger_count 2 one breaching sample raises nothing however often it is
// read, and the second distinct breaching sample raises the alert.
func TestHysteresis_RaiseNeedsTriggerCountSamples(t *testing.T) {
	engine, ds, pool, ruleID, cleanup := newSlotInactiveRuleEnv(t)
	defer cleanup()
	setThresholdCounts(engine, 2, 3)

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "hysteresis-raise")

	slotSample(t, pool, connID, 3*time.Minute, false)
	engine.evaluateThresholds(ctx)
	engine.evaluateThresholds(ctx)
	if alert := activeAlertForRule(t, ds, ruleID, connID); alert != nil {
		t.Fatalf("one breaching sample read twice raised alert %d", alert.ID)
	}

	slotSample(t, pool, connID, 2*time.Minute, false)
	engine.evaluateThresholds(ctx)
	assertAlertFired(t, ds, ruleID, connID, "critical")
}

// TestHysteresis_HealthySampleResetsTriggerCount proves the run must be
// consecutive: a sample that does not breach, which for this metric means
// no row for the connection, discards the count, so the breaching sample
// after it starts again from one.
func TestHysteresis_HealthySampleResetsTriggerCount(t *testing.T) {
	engine, ds, pool, ruleID, cleanup := newSlotInactiveRuleEnv(t)
	defer cleanup()
	setThresholdCounts(engine, 2, 3)

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "hysteresis-raise-reset")

	steps := []struct {
		ago       time.Duration
		active    bool
		wantAlert bool
	}{
		{5 * time.Minute, false, false},
		{4 * time.Minute, true, false},
		{3 * time.Minute, false, false},
		{2 * time.Minute, false, true},
	}
	for i, step := range steps {
		slotSample(t, pool, connID, step.ago, step.active)
		engine.evaluateThresholds(ctx)
		alert := activeAlertForRule(t, ds, ruleID, connID)
		if (alert != nil) != step.wantAlert {
			t.Fatalf("step %d: alert raised = %v, want %v", i, alert != nil, step.wantAlert)
		}
	}
}

// TestHysteresis_TriggerCountOneRaisesOnFirstSample proves that a count of
// 1 raises on the first breaching sample, as before the setting existed.
func TestHysteresis_TriggerCountOneRaisesOnFirstSample(t *testing.T) {
	engine, ds, pool, ruleID, cleanup := newSlotInactiveRuleEnv(t)
	defer cleanup()
	setThresholdCounts(engine, 1, 1)

	connID := insertTestConnection(t, pool, "hysteresis-raise-one")
	slotSample(t, pool, connID, 2*time.Minute, false)
	engine.evaluateThresholds(context.Background())
	assertAlertFired(t, ds, ruleID, connID, "critical")
}

// TestHysteresis_UnreadRuleKeepsTriggerCounts covers the evaluator's
// pruning: a rule whose values could not be read keeps its counts,
// because a failed query says nothing about the condition, whilst a key
// the pass could read but found no row for is dropped.
func TestHysteresis_UnreadRuleKeepsTriggerCounts(t *testing.T) {
	engine, _, pool, slotRuleID, cleanup := newSlotInactiveRuleEnv(t)
	defer cleanup()
	setThresholdCounts(engine, 2, 3)

	var unreadRuleID int64
	if err := pool.QueryRow(context.Background(),
		insertUnsupportedMetricRuleSQL).Scan(&unreadRuleID); err != nil {
		t.Fatalf("failed to insert the unsupported metric rule: %v", err)
	}

	unreadKey := thresholdSampleKey{ruleID: unreadRuleID, connectionID: 1}
	noRowKey := thresholdSampleKey{ruleID: slotRuleID, connectionID: 1}
	engine.triggerStreaks.observe(unreadKey, time.Now(), true)
	engine.triggerStreaks.observe(noRowKey, time.Now(), true)

	engine.evaluateThresholds(context.Background())

	if c := engine.triggerStreaks.count(unreadKey); c != 1 {
		t.Errorf("unread rule's count = %d, want 1 (kept)", c)
	}
	if c := engine.triggerStreaks.count(noRowKey); c != 0 {
		t.Errorf("count for a key with no row = %d, want 0 (swept)", c)
	}
}

// insertStatDatabaseSampleSecsAgoSQL seeds one metrics.pg_stat_database
// row the given number of seconds before NOW(), taking the age as a
// number rather than as interval text.
const insertStatDatabaseSampleSecsAgoSQL = `
        INSERT INTO metrics.pg_stat_database
            (connection_id, database_name, datname, blks_hit, blks_read,
             collected_at)
        VALUES ($1, $2::text, $2::text, $3, $4,
                NOW() - make_interval(secs => $5))
    `

// statDatabaseSample writes one cumulative pg_stat_database sample for
// appdb the given number of seconds in the past.
func statDatabaseSample(t *testing.T, pool *pgxpool.Pool, connID int, hits, reads int64, ago int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), insertStatDatabaseSampleSecsAgoSQL,
		connID, "appdb", hits, reads, ago); err != nil {
		t.Fatalf("failed to seed pg_stat_database: %v", err)
	}
}

// TestHysteresis_ClearNeedsClearCountSamples covers the clear side with
// clear_count 3, on a metric that reports a row for a healthy database:
// healthy samples are counted once however often the cleaner reads them,
// a breaching sample in the middle of the run starts it again, and the
// third consecutive healthy sample clears the alert. The count is then
// dropped along with the alert.
func TestHysteresis_ClearNeedsClearCountSamples(t *testing.T) {
	engine, ds, pool, ruleID, cleanup := newCacheHitEnv(t)
	defer cleanup()
	setThresholdCounts(engine, 1, 3)

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "hysteresis-clear")
	seedColdCache(t, pool, connID, "appdb")

	engine.evaluateThresholds(ctx)
	alert, err := ds.GetActiveThresholdAlert(ctx, ruleID, connID, strPtr("appdb"))
	if err != nil {
		t.Fatalf("GetActiveThresholdAlert failed: %v", err)
	}
	if alert == nil {
		t.Fatal("expected the cold cache to raise an alert")
	}

	// seedColdCache leaves the counters at 60000 hits and 30000 reads,
	// a minute ago. Each step is five seconds after the last. A healthy
	// step adds 30000 hits and 1000 reads (200 reads per second at a
	// 96.8% ratio); a cold one adds 30000 reads and no hits (a 0%
	// ratio). Every interval reads at least 100 blocks per second and
	// touches at least 10000 blocks, so cache_hit_ratio reports a row
	// for each of them rather than leaving the metric absent.
	steps := []struct {
		label      string
		hits       int64
		reads      int64
		ago        int
		wantStatus string
		wantCount  int
	}{
		{"first healthy sample", 90_000, 31_000, 55, "active", 1},
		{"second healthy sample", 120_000, 32_000, 50, "active", 2},
		{"cold sample resets the run", 120_000, 62_000, 45, "active", 0},
		{"healthy sample after the reset", 150_000, 63_000, 40, "active", 1},
		{"second healthy sample after the reset", 180_000, 64_000, 35, "active", 2},
		{"third consecutive healthy sample", 210_000, 65_000, 30, "cleared", 3},
	}
	for _, step := range steps {
		statDatabaseSample(t, pool, connID, step.hits, step.reads, step.ago)
		// Two cleaner passes read the same sample; only one may count.
		engine.cleanResolvedAlerts(ctx)
		if step.wantStatus == "active" {
			engine.cleanResolvedAlerts(ctx)
		}
		if status := alertStatus(t, pool, alert.ID); status != step.wantStatus {
			t.Fatalf("%s: alert status = %q, want %q", step.label, status, step.wantStatus)
		}
		if c := engine.clearStreaks.count(alert.ID); c != step.wantCount {
			t.Errorf("%s: clear count = %d, want %d", step.label, c, step.wantCount)
		}
	}

	engine.cleanResolvedAlerts(ctx)
	if n := engine.clearStreaks.size(); n != 0 {
		t.Errorf("clear counts after the alert cleared = %d, want 0", n)
	}
}

// TestHysteresis_AcknowledgedAlertDropsClearCount proves the cleaner
// drops the count of an alert that is no longer active, so an
// acknowledged alert that is later reactivated counts afresh.
func TestHysteresis_AcknowledgedAlertDropsClearCount(t *testing.T) {
	engine, ds, pool, ruleID, cleanup := newCacheHitEnv(t)
	defer cleanup()
	setThresholdCounts(engine, 1, 3)

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "hysteresis-ack")
	seedColdCache(t, pool, connID, "appdb")

	engine.evaluateThresholds(ctx)
	alert, err := ds.GetActiveThresholdAlert(ctx, ruleID, connID, strPtr("appdb"))
	if err != nil || alert == nil {
		t.Fatalf("expected the cold cache to raise an alert (err %v)", err)
	}

	// A healthy interval: 30000 hits and 1000 reads in five seconds.
	statDatabaseSample(t, pool, connID, 90_000, 31_000, 55)
	engine.cleanResolvedAlerts(ctx)
	if c := engine.clearStreaks.count(alert.ID); c != 1 {
		t.Fatalf("clear count = %d, want 1", c)
	}

	if _, err := pool.Exec(ctx,
		`UPDATE alerts SET status = 'acknowledged' WHERE id = $1`, alert.ID); err != nil {
		t.Fatalf("failed to acknowledge the alert: %v", err)
	}
	engine.cleanResolvedAlerts(ctx)
	if c := engine.clearStreaks.count(alert.ID); c != 0 {
		t.Errorf("clear count after acknowledgement = %d, want 0", c)
	}
}

// TestHysteresis_AbsentMetricCountsProbeCollections covers a metric that
// clears when absent. Its empty result carries no collected_at, so the
// probe's last collection identifies the sample: with clear_count 2, two
// cleaner passes over one collection leave the alert active, and the
// next collection clears it.
func TestHysteresis_AbsentMetricCountsProbeCollections(t *testing.T) {
	engine, pool, connID, alertID, cleanup := slotInactiveEnv(t, "hysteresis-absent")
	defer cleanup()
	setThresholdCounts(engine, 1, 2)

	ctx := context.Background()
	seedProbeReporting(t, pool, connID, "pg_replication_slots", "30 seconds")
	engine.cleanResolvedAlerts(ctx)
	engine.cleanResolvedAlerts(ctx)
	if status := alertStatus(t, pool, alertID); status != "active" {
		t.Fatalf("alert status after one collection = %q, want \"active\"", status)
	}

	seedProbeReporting(t, pool, connID, "pg_replication_slots", "10 seconds")
	engine.cleanResolvedAlerts(ctx)
	if status := alertStatus(t, pool, alertID); status != "cleared" {
		t.Errorf("alert status after a second collection = %q, want \"cleared\"", status)
	}
}

// TestHysteresis_GateBreaksTriggerRun covers the keys the evaluator skips
// without judging them. A blackout or a disabled override in the middle
// of a run of breaching samples breaks the run, so after the gate lifts
// the evaluator needs trigger_count fresh samples before it raises.
func TestHysteresis_GateBreaksTriggerRun(t *testing.T) {
	gates := []struct {
		name  string
		apply string
		lift  string
	}{
		{
			name: "blackout",
			apply: `INSERT INTO blackouts (scope, connection_id, start_time, end_time)
                    VALUES ('server', $1, NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 hour')`,
			lift: `DELETE FROM blackouts WHERE connection_id = $1`,
		},
		{
			name: "disabled override",
			apply: `INSERT INTO alert_thresholds
                        (rule_id, scope, connection_id, operator, threshold, severity, enabled)
                    SELECT id, 'server', $1, '==', 1, 'critical', FALSE
                      FROM alert_rules WHERE name = 'replication_slot_inactive'`,
			lift: `DELETE FROM alert_thresholds WHERE connection_id = $1`,
		},
	}
	for _, gate := range gates {
		t.Run(gate.name, func(t *testing.T) {
			engine, ds, pool, ruleID, cleanup := newSlotInactiveRuleEnv(t)
			defer cleanup()
			setThresholdCounts(engine, 2, 3)

			ctx := context.Background()
			connID := insertTestConnection(t, pool, "hysteresis-gate")
			key := thresholdSampleKey{ruleID: ruleID, connectionID: connID}

			slotSample(t, pool, connID, 5*time.Minute, false)
			engine.evaluateThresholds(ctx)
			if c := engine.triggerStreaks.count(key); c != 1 {
				t.Fatalf("count before the gate = %d, want 1", c)
			}

			if _, err := pool.Exec(ctx, gate.apply, connID); err != nil {
				t.Fatalf("failed to apply the %s: %v", gate.name, err)
			}
			slotSample(t, pool, connID, 4*time.Minute, false)
			engine.evaluateThresholds(ctx)
			if c := engine.triggerStreaks.count(key); c != 0 {
				t.Errorf("count under the gate = %d, want 0", c)
			}
			if _, err := pool.Exec(ctx, gate.lift, connID); err != nil {
				t.Fatalf("failed to lift the %s: %v", gate.name, err)
			}

			slotSample(t, pool, connID, 3*time.Minute, false)
			engine.evaluateThresholds(ctx)
			if alert := activeAlertForRule(t, ds, ruleID, connID); alert != nil {
				t.Fatalf("first sample after the gate raised alert %d", alert.ID)
			}
			slotSample(t, pool, connID, 2*time.Minute, false)
			engine.evaluateThresholds(ctx)
			assertAlertFired(t, ds, ruleID, connID, "critical")
		})
	}
}

// TestHysteresis_CancelledEvaluationKeepsCounts proves an evaluation
// that cannot read its rules, here because its context has already
// ended, leaves the trigger counts alone rather than sweeping them.
func TestHysteresis_CancelledEvaluationKeepsCounts(t *testing.T) {
	engine, _, _, ruleID, cleanup := newSlotInactiveRuleEnv(t)
	defer cleanup()

	key := thresholdSampleKey{ruleID: ruleID, connectionID: 1}
	engine.triggerStreaks.observe(key, time.Now(), true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine.evaluateThresholds(ctx)

	if c := engine.triggerStreaks.count(key); c != 1 {
		t.Errorf("count after an evaluation with an ended context = %d, want 1", c)
	}
}
