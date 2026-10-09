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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// SQL used by the anomaly recovery integration tests (issue #611).
const (
	insertRecoveryEngineAlertSQL = `
        INSERT INTO alerts
            (alert_type, connection_id, database_name, metric_name, severity,
             title, description, status)
        VALUES ('anomaly', $1, $2, $3, $4, $3, 'seeded', $5)
        RETURNING id
    `

	selectRecoveryEngineAlertSQL = `
        SELECT status, cleared_at IS NOT NULL, severity,
               metric_value, anomaly_score, last_updated IS NOT NULL
        FROM alerts WHERE id = $1
    `

	deleteRecoveryMetricRowsSQL = `DELETE FROM metrics.pg_sys_load_avg_info`

	deleteRecoveryRulesSQL = `DELETE FROM alert_rules`

	countRecoveryCandidatesSQL = `SELECT COUNT(*) FROM anomaly_candidates`

	insertRecoveryDBBlackoutSQL = `
        INSERT INTO blackouts
            (scope, connection_id, database_name, start_time, end_time,
             reason, created_by)
        VALUES ('server', $1, $2, $3, $4, 'test', 'tester')
    `

	ageRecoveryClearSQL = `
        UPDATE alerts SET cleared_at = NOW() - INTERVAL '1 hour' WHERE id = $1
    `

	acknowledgeRecoveryAlertSQL = `UPDATE alerts SET status = 'acknowledged' WHERE id = $1`

	// failRecoveryUpdatesSQL makes every UPDATE of alerts fail, so the
	// refresh and clear error paths can be reached after a successful
	// blackout check.
	failRecoveryUpdatesSQL = `
        CREATE OR REPLACE FUNCTION recovery_test_fail_update() RETURNS trigger
        LANGUAGE plpgsql AS $$
        BEGIN
            RAISE EXCEPTION 'update refused by test';
        END;
        $$;
        CREATE TRIGGER recovery_test_fail_update BEFORE UPDATE ON alerts
            FOR EACH ROW EXECUTE FUNCTION recovery_test_fail_update();
    `

	dropFailRecoveryUpdatesSQL = `
        DROP TRIGGER IF EXISTS recovery_test_fail_update ON alerts;
        DROP FUNCTION IF EXISTS recovery_test_fail_update();
    `
)

// Baseline mean and standard deviation for the recovery tests. With the
// default sensitivity of 3, a value of recoveryMean + n gives z = n.
const (
	recoveryMean   = 10.0
	recoveryStdDev = 1.0
	inBandValue    = recoveryMean + 0.5 // z = 0.5
	infoValue      = recoveryMean + 4   // z = 4, info
	warningValue   = recoveryMean + 7   // z = 7, warning
	criticalValue  = recoveryMean + 13  // z = 13, critical
)

// recoveryAlertState is the alert state the recovery tests assert on.
type recoveryAlertState struct {
	status         string
	cleared        bool
	severity       string
	metricValue    *float64
	anomalyScore   *float64
	hasLastUpdated bool
}

// recoveryEnv is the integration environment for the recovery tests.
type recoveryEnv struct {
	engine *Engine
	ds     *database.Datastore
	pool   *pgxpool.Pool
	connID int
}

// newRecoveryEnv builds the detection environment with the alert tables, a
// rule on the test metric, one connection and a warm baseline.
func newRecoveryEnv(t *testing.T) *recoveryEnv {
	t.Helper()

	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, tierSkipAlertsSchema); err != nil {
		t.Fatalf("failed to create alert tables: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), dropFailRecoveryUpdatesSQL); err != nil {
			t.Logf("trigger teardown failed: %v", err)
		}
		if _, err := pool.Exec(context.Background(), tierSkipAlertsTeardown); err != nil {
			t.Logf("alert table teardown failed: %v", err)
		}
	})

	if _, err := pool.Exec(ctx, insertAnomalyAlertRuleSQL, "recovery_rule", tierSkipMetric); err != nil {
		t.Fatalf("failed to insert alert rule: %v", err)
	}

	var connID int
	if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL, "recovery-conn").Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}

	env := &recoveryEnv{engine: engine, ds: ds, pool: pool, connID: connID}
	env.setBaseline(t, 200)
	return env
}

// setBaseline writes the connection-wide "all" baseline with the given
// sample count; a small count leaves it cold.
func (env *recoveryEnv) setBaseline(t *testing.T, samples int64) {
	t.Helper()
	now := time.Now().UTC()
	b := &database.MetricBaseline{
		ConnectionID:     env.connID,
		MetricName:       tierSkipMetric,
		PeriodType:       "all",
		Mean:             recoveryMean,
		StdDev:           recoveryStdDev,
		Min:              recoveryMean - recoveryStdDev,
		Max:              recoveryMean + recoveryStdDev,
		SampleCount:      samples,
		LastCalculated:   now,
		EarliestSampleAt: now.Add(-48 * time.Hour),
	}
	if err := env.ds.UpsertMetricBaseline(context.Background(), b); err != nil {
		t.Fatalf("UpsertMetricBaseline failed: %v", err)
	}
}

// setValue replaces the latest value of the test metric.
func (env *recoveryEnv) setValue(t *testing.T, v float64) {
	t.Helper()
	env.clearValue(t)
	if _, err := env.pool.Exec(context.Background(), insertAnomalyLoadAvgSQL,
		env.connID, strconv.FormatFloat(v, 'g', -1, 64)); err != nil {
		t.Fatalf("failed to insert metric value: %v", err)
	}
}

// clearValue removes every value of the test metric, as when the probe
// stops reporting.
func (env *recoveryEnv) clearValue(t *testing.T) {
	t.Helper()
	if _, err := env.pool.Exec(context.Background(), deleteRecoveryMetricRowsSQL); err != nil {
		t.Fatalf("failed to delete metric values: %v", err)
	}
}

// seedAlert inserts an anomaly alert on the test metric.
func (env *recoveryEnv) seedAlert(t *testing.T, db *string, severity, status string) int64 {
	t.Helper()
	var id int64
	if err := env.pool.QueryRow(context.Background(), insertRecoveryEngineAlertSQL,
		env.connID, db, tierSkipMetric, severity, status).Scan(&id); err != nil {
		t.Fatalf("failed to seed alert: %v", err)
	}
	return id
}

func (env *recoveryEnv) alert(t *testing.T, id int64) recoveryAlertState {
	t.Helper()
	var s recoveryAlertState
	if err := env.pool.QueryRow(context.Background(), selectRecoveryEngineAlertSQL, id).Scan(
		&s.status, &s.cleared, &s.severity, &s.metricValue, &s.anomalyScore, &s.hasLastUpdated); err != nil {
		t.Fatalf("failed to read alert %d: %v", id, err)
	}
	return s
}

func (env *recoveryEnv) candidates(t *testing.T) int {
	t.Helper()
	var n int
	if err := env.pool.QueryRow(context.Background(), countRecoveryCandidatesSQL).Scan(&n); err != nil {
		t.Fatalf("failed to count candidates: %v", err)
	}
	return n
}

// passes runs detectAnomalies once per value; a nil value removes the
// metric's rows for that pass, as when the probe stops reporting.
func (env *recoveryEnv) passes(t *testing.T, values ...*float64) {
	t.Helper()
	for _, v := range values {
		if v == nil {
			env.clearValue(t)
		} else {
			env.setValue(t, *v)
		}
		env.engine.detectAnomalies(context.Background())
	}
}

func val(v float64) *float64 { return &v }

func wantStatus(t *testing.T, got recoveryAlertState, want string) {
	t.Helper()
	if got.status != want {
		t.Fatalf("alert status = %s, want %s", got.status, want)
	}
}

// TestAnomalyRecoveryClearsAfterClearCount covers the core of issue #611:
// an active anomaly alert clears after clear_count consecutive in-band
// evaluations, with cleared_at set and an AlertClear notification queued.
func TestAnomalyRecoveryClearsAfterClearCount(t *testing.T) {
	env := newRecoveryEnv(t)
	capture := installStalenessNotificationCapture(t, env.engine)
	id := env.seedAlert(t, nil, "warning", "active")

	env.passes(t, val(inBandValue), val(inBandValue))
	wantStatus(t, env.alert(t, id), "active")
	if got := env.engine.anomalyStreak(id); got != 2 {
		t.Fatalf("streak after two in-band passes = %d, want 2", got)
	}

	env.passes(t, val(inBandValue))
	got := env.alert(t, id)
	wantStatus(t, got, "cleared")
	if !got.cleared {
		t.Error("expected cleared_at to be set")
	}
	counts := capture.drain(t)
	if counts[database.NotificationTypeAlertClear] != 1 || len(counts) != 1 {
		t.Errorf("notifications = %v, want one alert_clear", counts)
	}
	if got := env.engine.anomalyStreak(id); got != 0 {
		t.Errorf("streak after clearing = %d, want 0", got)
	}
	if n := env.candidates(t); n != 0 {
		t.Errorf("in-band passes recorded %d candidates", n)
	}
}

// TestAnomalyRecoveryHonoursClearCount checks a reloaded clear_count of 1
// clears on the first in-band pass.
func TestAnomalyRecoveryHonoursClearCount(t *testing.T) {
	env := newRecoveryEnv(t)
	env.engine.getConfig().Anomaly.Tier1.ClearCount = 1
	id := env.seedAlert(t, nil, "warning", "active")

	env.passes(t, val(inBandValue))
	wantStatus(t, env.alert(t, id), "cleared")
}

// TestAnomalyRecoveryOutOfBandResetsAndRefreshes checks an out-of-band
// pass resets the count, refreshes value, score and last_updated, and only
// ever raises the severity.
func TestAnomalyRecoveryOutOfBandResetsAndRefreshes(t *testing.T) {
	env := newRecoveryEnv(t)
	capture := installStalenessNotificationCapture(t, env.engine)
	id := env.seedAlert(t, nil, "warning", "active")

	env.passes(t, val(inBandValue), val(inBandValue), val(criticalValue))
	got := env.alert(t, id)
	wantStatus(t, got, "active")
	if got.severity != "critical" {
		t.Errorf("severity = %s, want escalation to critical", got.severity)
	}
	if got.metricValue == nil || *got.metricValue != criticalValue {
		t.Errorf("metric_value = %v, want %v", got.metricValue, criticalValue)
	}
	if got.anomalyScore == nil || *got.anomalyScore != 13 {
		t.Errorf("anomaly_score = %v, want 13", got.anomalyScore)
	}
	if !got.hasLastUpdated {
		t.Error("expected last_updated to be set")
	}
	if streak := env.engine.anomalyStreak(id); streak != 0 {
		t.Errorf("streak after out-of-band pass = %d, want 0", streak)
	}

	// A smaller deviation updates the value but keeps the severity.
	env.passes(t, val(infoValue))
	got = env.alert(t, id)
	if got.severity != "critical" {
		t.Errorf("severity = %s, want critical kept", got.severity)
	}
	if got.metricValue == nil || *got.metricValue != infoValue {
		t.Errorf("metric_value = %v, want %v", got.metricValue, infoValue)
	}

	// Two in-band passes are not enough after the reset; a third is.
	env.passes(t, val(inBandValue), val(inBandValue))
	wantStatus(t, env.alert(t, id), "active")
	env.passes(t, val(inBandValue))
	wantStatus(t, env.alert(t, id), "cleared")

	counts := capture.drain(t)
	if counts[database.NotificationTypeAlertClear] != 1 || len(counts) != 1 {
		t.Errorf("notifications = %v, want only one alert_clear", counts)
	}
	if n := env.candidates(t); n != 0 {
		t.Errorf("out-of-band passes under an open alert recorded %d candidates", n)
	}
}

// TestAnomalyRecoveryEscalatesFromInfo checks a warning-level deviation
// raises an info alert.
func TestAnomalyRecoveryEscalatesFromInfo(t *testing.T) {
	env := newRecoveryEnv(t)
	id := env.seedAlert(t, nil, "info", "active")

	env.passes(t, val(warningValue))
	if got := env.alert(t, id); got.severity != "warning" {
		t.Errorf("severity = %s, want warning", got.severity)
	}
}

// TestAnomalyRecoveryLeavesAcknowledgedAlone checks an acknowledged alert
// is neither refreshed nor cleared.
func TestAnomalyRecoveryLeavesAcknowledgedAlone(t *testing.T) {
	env := newRecoveryEnv(t)
	id := env.seedAlert(t, nil, "warning", "acknowledged")

	env.passes(t, val(inBandValue), val(inBandValue), val(inBandValue), val(criticalValue))
	got := env.alert(t, id)
	wantStatus(t, got, "acknowledged")
	if got.cleared || got.metricValue != nil || got.hasLastUpdated || got.severity != "warning" {
		t.Errorf("acknowledged alert changed: %+v", got)
	}
}

// TestAnomalyRecoveryHoldsOpen checks the conditions under which an alert
// must stay open, and that each resets the consecutive count.
func TestAnomalyRecoveryHoldsOpen(t *testing.T) {
	tests := []struct {
		name string
		// hold puts the environment into the holding condition and
		// returns a function that lifts it.
		hold func(t *testing.T, env *recoveryEnv) func()
	}{
		{
			name: "metric stops reporting",
			hold: func(t *testing.T, env *recoveryEnv) func() {
				env.clearValue(t)
				return func() {}
			},
		},
		{
			name: "baseline goes cold",
			hold: func(t *testing.T, env *recoveryEnv) func() {
				env.setBaseline(t, 5)
				return func() { env.setBaseline(t, 200) }
			},
		},
		{
			name: "connection blackout",
			hold: func(t *testing.T, env *recoveryEnv) func() {
				now := time.Now()
				if _, err := env.pool.Exec(context.Background(), insertAnomalyBlackoutSQL,
					env.connID, now.Add(-time.Minute), now.Add(time.Hour)); err != nil {
					t.Fatalf("failed to insert blackout: %v", err)
				}
				return func() {
					if _, err := env.pool.Exec(context.Background(), `DELETE FROM blackouts`); err != nil {
						t.Fatalf("failed to delete blackout: %v", err)
					}
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newRecoveryEnv(t)
			id := env.seedAlert(t, nil, "warning", "active")

			env.passes(t, val(inBandValue), val(inBandValue))

			lift := tc.hold(t, env)
			for i := 0; i < 4; i++ {
				env.engine.detectAnomalies(context.Background())
			}
			wantStatus(t, env.alert(t, id), "active")
			if streak := env.engine.anomalyStreak(id); streak != 0 {
				t.Errorf("streak while held = %d, want 0", streak)
			}
			lift()

			env.passes(t, val(inBandValue), val(inBandValue))
			wantStatus(t, env.alert(t, id), "active")
			env.passes(t, val(inBandValue))
			wantStatus(t, env.alert(t, id), "cleared")
		})
	}
}

// TestAnomalyRecoveryWithoutDetection checks recovery runs for a metric no
// rule covers any more, and when no later tier could process a new
// candidate, but writes no candidate in either case.
func TestAnomalyRecoveryWithoutDetection(t *testing.T) {
	t.Run("metric without a rule", func(t *testing.T) {
		env := newRecoveryEnv(t)
		if _, err := env.pool.Exec(context.Background(), deleteRecoveryRulesSQL); err != nil {
			t.Fatalf("failed to delete rules: %v", err)
		}
		id := env.seedAlert(t, nil, "warning", "active")

		env.passes(t, val(criticalValue))
		if got := env.alert(t, id); got.severity != "critical" {
			t.Errorf("severity = %s, want critical", got.severity)
		}
		env.passes(t, val(inBandValue), val(inBandValue), val(inBandValue))
		wantStatus(t, env.alert(t, id), "cleared")

		// With the alert gone nothing covers the metric, so an
		// out-of-band value is not even scored.
		env.passes(t, val(criticalValue))
		if n := env.candidates(t); n != 0 {
			t.Errorf("recorded %d candidates for a metric without a rule", n)
		}
	})

	t.Run("no tier can process candidates", func(t *testing.T) {
		env := newRecoveryEnv(t)
		env.engine.reasoningProvider = nil
		id := env.seedAlert(t, nil, "warning", "active")

		env.passes(t, val(inBandValue), val(inBandValue), val(inBandValue))
		wantStatus(t, env.alert(t, id), "cleared")

		// No open alert remains, so the pass returns before scoring.
		env.passes(t, val(criticalValue))
		if n := env.candidates(t); n != 0 {
			t.Errorf("recorded %d candidates with no usable tier", n)
		}
	})
}

// TestAnomalyRecoveryCooldown checks a cleared alert's series records no
// candidate within AlertCooldownPeriod, and does again afterwards.
func TestAnomalyRecoveryCooldown(t *testing.T) {
	env := newRecoveryEnv(t)
	env.engine.getConfig().Anomaly.Tier1.ClearCount = 1
	id := env.seedAlert(t, nil, "warning", "active")

	env.passes(t, val(inBandValue))
	wantStatus(t, env.alert(t, id), "cleared")

	env.passes(t, val(criticalValue))
	if n := env.candidates(t); n != 0 {
		t.Fatalf("recorded %d candidates inside the cooldown", n)
	}

	if _, err := env.pool.Exec(context.Background(), ageRecoveryClearSQL, id); err != nil {
		t.Fatalf("failed to age the clear: %v", err)
	}
	env.passes(t, val(criticalValue))
	if n := env.candidates(t); n != 1 {
		t.Errorf("recorded %d candidates after the cooldown, want 1", n)
	}
}

// TestAnomalyRecoveryDisabledResetsStreaks checks disabling Tier 1 drops
// every count, so recovery starts afresh when it is re-enabled.
func TestAnomalyRecoveryDisabledResetsStreaks(t *testing.T) {
	env := newRecoveryEnv(t)
	id := env.seedAlert(t, nil, "warning", "active")

	env.passes(t, val(inBandValue), val(inBandValue))
	env.engine.getConfig().Anomaly.Tier1.Enabled = false
	env.engine.detectAnomalies(context.Background())
	if streak := env.engine.anomalyStreak(id); streak != 0 {
		t.Errorf("streak after disabling = %d, want 0", streak)
	}
	env.engine.getConfig().Anomaly.Tier1.Enabled = true
	env.passes(t, val(inBandValue))
	wantStatus(t, env.alert(t, id), "active")
}

// TestRecoverAnomalyAlertsDirect drives recoverAnomalyAlerts directly for
// the cases detectAnomalies cannot reach with a connection-wide metric:
// database-scoped blackouts, failing writes and an alert that a user
// acknowledged after the pass read it.
func TestRecoverAnomalyAlertsDirect(t *testing.T) {
	ctx := context.Background()
	db := "app"

	setup := func(t *testing.T, severity string) (*recoveryEnv, *anomalyRecoveryPass, int64, *database.MetricValue) {
		t.Helper()
		env := newRecoveryEnv(t)
		id := env.seedAlert(t, &db, severity, "active")
		pass := env.engine.loadAnomalyRecovery(ctx)
		if pass.empty() {
			t.Fatal("expected the seeded alert in the pass")
		}
		value := &database.MetricValue{ConnectionID: env.connID, DatabaseName: &db, Value: inBandValue}
		return env, pass, id, value
	}
	apply := func(env *recoveryEnv, pass *anomalyRecoveryPass, value *database.MetricValue, z float64) {
		cfg := env.engine.getConfig()
		env.engine.recoverAnomalyAlerts(ctx, pass, tierSkipMetric, value, z, true, cfg,
			cfg.Anomaly.Tier1.DefaultSensitivity)
	}

	t.Run("database blackout holds the alert", func(t *testing.T) {
		env, pass, id, value := setup(t, "warning")
		now := time.Now()
		if _, err := env.pool.Exec(ctx, insertRecoveryDBBlackoutSQL, env.connID, db,
			now.Add(-time.Minute), now.Add(time.Hour)); err != nil {
			t.Fatalf("failed to insert blackout: %v", err)
		}
		env.engine.getConfig().Anomaly.Tier1.ClearCount = 1
		apply(env, pass, value, 0.5)
		if _, ok := pass.next[id]; ok || pass.visited[id] {
			t.Error("expected a blacked-out alert not to be counted")
		}
		wantStatus(t, env.alert(t, id), "active")
	})

	t.Run("failed blackout check holds the alert", func(t *testing.T) {
		env, pass, id, value := setup(t, "warning")
		env.engine.getConfig().Anomaly.Tier1.ClearCount = 1
		if _, err := env.pool.Exec(ctx, `ALTER TABLE blackouts RENAME TO blackouts_hidden`); err != nil {
			t.Fatalf("failed to hide blackouts: %v", err)
		}
		defer func() {
			if _, err := env.pool.Exec(ctx, `ALTER TABLE blackouts_hidden RENAME TO blackouts`); err != nil {
				t.Fatalf("failed to restore blackouts: %v", err)
			}
		}()
		apply(env, pass, value, 0.5)
		wantStatus(t, env.alert(t, id), "active")
	})

	t.Run("unscored value holds the alert", func(t *testing.T) {
		env, pass, id, value := setup(t, "warning")
		env.engine.getConfig().Anomaly.Tier1.ClearCount = 1
		cfg := env.engine.getConfig()
		env.engine.recoverAnomalyAlerts(ctx, pass, tierSkipMetric, value, 0, false, cfg, 3)
		wantStatus(t, env.alert(t, id), "active")
	})

	t.Run("each alert is evaluated once per pass", func(t *testing.T) {
		env, pass, id, value := setup(t, "warning")
		apply(env, pass, value, 0.5)
		apply(env, pass, value, 0.5)
		if pass.next[id] != 1 {
			t.Errorf("streak = %d, want 1", pass.next[id])
		}
	})

	t.Run("alert acknowledged after the pass read it", func(t *testing.T) {
		env, pass, id, value := setup(t, "warning")
		env.engine.getConfig().Anomaly.Tier1.ClearCount = 1
		capture := installStalenessNotificationCapture(t, env.engine)
		if _, err := env.pool.Exec(ctx, acknowledgeRecoveryAlertSQL, id); err != nil {
			t.Fatalf("failed to acknowledge: %v", err)
		}
		apply(env, pass, value, 0.5)
		wantStatus(t, env.alert(t, id), "acknowledged")
		if counts := capture.drain(t); len(counts) != 0 {
			t.Errorf("notifications = %v, want none", counts)
		}

		// A refresh is likewise refused, and the in-memory alert is
		// left as it was read.
		pass2 := &anomalyRecoveryPass{alerts: pass.alerts, next: map[int64]int{}, visited: map[int64]bool{}}
		value.Value = criticalValue
		apply(env, pass2, value, 13)
		got := env.alert(t, id)
		if got.metricValue != nil || got.severity != "warning" {
			t.Errorf("acknowledged alert refreshed: %+v", got)
		}
		if a := pass2.alerts[newAnomalyAlertKey(tierSkipMetric, env.connID, &db)][0]; a.Severity != "warning" {
			t.Errorf("in-memory severity = %s, want warning", a.Severity)
		}
	})

	t.Run("failed writes keep the alert open", func(t *testing.T) {
		env, pass, id, value := setup(t, "warning")
		env.engine.getConfig().Anomaly.Tier1.ClearCount = 1
		if _, err := env.pool.Exec(ctx, failRecoveryUpdatesSQL); err != nil {
			t.Fatalf("failed to install trigger: %v", err)
		}

		apply(env, pass, value, 0.5)
		wantStatus(t, env.alert(t, id), "active")
		if pass.next[id] != 1 {
			t.Errorf("streak after a failed clear = %d, want it kept at 1", pass.next[id])
		}

		pass2 := &anomalyRecoveryPass{alerts: pass.alerts, next: map[int64]int{}, visited: map[int64]bool{}}
		value.Value = criticalValue
		apply(env, pass2, value, 13)
		if got := env.alert(t, id); got.metricValue != nil || got.severity != "warning" {
			t.Errorf("alert changed despite the failed refresh: %+v", got)
		}
	})
}

// TestDetectAnomalyForValueUnscored checks a value with no baseline
// records no candidate.
func TestDetectAnomalyForValueUnscored(t *testing.T) {
	env := newRecoveryEnv(t)
	cfg := env.engine.getConfig()
	value := &database.MetricValue{ConnectionID: env.connID + 1000, Value: criticalValue}
	env.engine.detectAnomalyForValue(context.Background(), tierSkipMetric, value, cfg,
		cfg.Anomaly.Tier1.DefaultSensitivity, time.Now())
	if n := env.candidates(t); n != 0 {
		t.Errorf("recorded %d candidates without a baseline", n)
	}
}

// TestAnomalyRecoveryListFailureKeepsStreaks checks a pass that cannot
// read the active alerts keeps the previous counts and still detects.
func TestAnomalyRecoveryListFailureKeepsStreaks(t *testing.T) {
	env := newRecoveryEnv(t)
	id := env.seedAlert(t, nil, "warning", "active")
	env.passes(t, val(inBandValue))

	if _, err := env.pool.Exec(context.Background(), tierSkipAlertsTeardown); err != nil {
		t.Fatalf("failed to drop alerts: %v", err)
	}
	out := captureStderr(t, func() {
		env.passes(t, val(criticalValue))
	})
	if streak := env.engine.anomalyStreak(id); streak != 1 {
		t.Errorf("streak after a failed list = %d, want 1", streak)
	}
	if !strings.Contains(out, "Failed to list active anomaly alerts") {
		t.Errorf("expected the list failure to be logged, got %q", out)
	}
	if n := env.candidates(t); n != 1 {
		t.Errorf("recorded %d candidates, want detection to carry on", n)
	}
}
