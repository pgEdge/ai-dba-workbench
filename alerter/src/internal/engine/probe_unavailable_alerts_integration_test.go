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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// Seed and inspection statements for the probe_unavailable tests. They
// are named constants for the same reason as those in
// staleness_alerts_integration_test.go: the Codacy/Semgrep
// go_sql_rule-concat-sqli rule flags inline multi-line SQL, and every
// value here is still bound via $N.
const (
	probeUnavailableRuleInsertSQL = `
        INSERT INTO alert_rules
            (name, description, category, metric_name, default_operator,
             default_threshold, default_severity, default_enabled,
             required_extension, is_built_in)
        VALUES ('probe_unavailable',
                'A probe that was collecting has stopped being available; metric collection for it has stopped',
                'availability', 'probe_available', '<', 1, 'warning',
                TRUE, NULL, TRUE)
        RETURNING id
    `

	probeUnavailableNoReasonUpdateSQL = `
        UPDATE probe_availability
           SET is_available = FALSE, unavailable_reason = NULL
         WHERE connection_id = $1 AND probe_name = $2
    `

	probeUnavailableAlertSelectSQL = `
        SELECT id, status, metric_value, title, description
        FROM alerts
        WHERE rule_id = $1
        ORDER BY id
    `

	probeUnavailableRuleDisableSQL = `
        UPDATE alert_rules SET default_enabled = FALSE WHERE id = $1
    `

	probeUnavailableThresholdOverrideInsertSQL = `
        INSERT INTO alert_thresholds
            (rule_id, scope, connection_id, operator, threshold, severity, enabled)
        VALUES ($1, 'server', $2, '<', 1, 'warning', FALSE)
    `

	probeUnavailableBlackoutInsertSQL = `
        INSERT INTO blackouts
            (scope, connection_id, reason, start_time, end_time, created_by)
        VALUES ('server', $1, 'test blackout',
                NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 hour', 'test')
    `

	probeUnavailableClearAlertSQL = `
        UPDATE alerts
           SET status = 'cleared', cleared_at = NOW()
         WHERE id = $1
    `

	probeUnavailableBreakClearedAtSQL = `
        ALTER TABLE alerts
            ALTER COLUMN cleared_at TYPE TEXT USING cleared_at::text
    `
)

// probeUnavailableAlertRow is the projection of an alert the tests in
// this file assert on.
type probeUnavailableAlertRow struct {
	id          int64
	status      string
	metricValue *float64
	title       string
	description string
}

// seedProbeUnavailableRule inserts the probe_unavailable rule and returns
// its id. The values mirror the collector's migration #16 seed, because
// the alerter's integration schema is built independently of the
// collector's.
func seedProbeUnavailableRule(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var ruleID int64
	if err := pool.QueryRow(context.Background(),
		probeUnavailableRuleInsertSQL).Scan(&ruleID); err != nil {
		t.Fatalf("failed to insert probe_unavailable rule: %v", err)
	}
	return ruleID
}

// readProbeUnavailableAlerts returns every alert belonging to a rule,
// oldest first.
func readProbeUnavailableAlerts(t *testing.T, pool *pgxpool.Pool,
	ruleID int64) []probeUnavailableAlertRow {
	t.Helper()

	rows, err := pool.Query(context.Background(), probeUnavailableAlertSelectSQL, ruleID)
	if err != nil {
		t.Fatalf("failed to read alerts: %v", err)
	}
	defer rows.Close()

	var out []probeUnavailableAlertRow
	for rows.Next() {
		var row probeUnavailableAlertRow
		if err := rows.Scan(&row.id, &row.status, &row.metricValue, &row.title,
			&row.description); err != nil {
			t.Fatalf("failed to scan alert: %v", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("row iteration error: %v", err)
	}
	return out
}

// newProbeUnavailableEnv builds the common fixture for these tests: an
// engine with a captured notification pool, the probe_unavailable rule, a
// monitored connection and one probe that collected ten seconds ago, so
// that nothing in the fixture is stale enough to raise a staleness alert
// and only availability is under test.
func newProbeUnavailableEnv(t *testing.T, name string) (*Engine, *pgxpool.Pool,
	*stalenessNotificationCapture, int64, int, func()) {
	t.Helper()

	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	capture := installStalenessNotificationCapture(t, engine)
	ruleID := seedProbeUnavailableRule(t, pool)
	connID := insertTestConnection(t, pool, name)
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "10 seconds")

	return engine, pool, capture, ruleID, connID, cleanup
}

// TestProbeUnavailableAlertRaisedWhenProbeStopsCollecting is the
// regression test for issue #512. A probe that was collecting and has
// gone unavailable must raise an alert of its own, even though it is
// nowhere near stale enough for metric_staleness to have fired and the
// staleness evaluator skips it.
func TestProbeUnavailableAlertRaisedWhenProbeStopsCollecting(t *testing.T) {
	engine, pool, capture, ruleID, connID, cleanup := newProbeUnavailableEnv(t,
		"probe-unavailable-raised")
	defer cleanup()

	ctx := context.Background()
	stalenessRuleID := seedStalenessRule(t, pool)

	const reason = "extension pg_stat_statements is not installed"
	markProbeUnavailable(t, pool, connID, "pg_stat_activity", reason)

	engine.evaluateThresholds(ctx)

	alerts := readProbeUnavailableAlerts(t, pool, ruleID)
	if len(alerts) != 1 || alerts[0].status != "active" {
		t.Fatalf("alerts = %+v, want one active alert", alerts)
	}
	if alerts[0].metricValue == nil || *alerts[0].metricValue != 0 {
		t.Errorf("metric_value = %v, want 0", alerts[0].metricValue)
	}
	if !strings.Contains(alerts[0].title, "pg_stat_activity") ||
		!strings.Contains(alerts[0].title, "probe-unavailable-raised") {
		t.Errorf("title = %q, want it to name the probe and the connection",
			alerts[0].title)
	}
	if !strings.Contains(alerts[0].description, reason) {
		t.Errorf("description = %q, want it to carry the reason", alerts[0].description)
	}

	// The probe is only ten seconds late, so metric_staleness has not
	// fired and this alert is the only thing reporting the fault.
	if stale := readStalenessAlertsForRule(t, pool, stalenessRuleID); len(stale) != 0 {
		t.Errorf("staleness alerts = %+v, want none", stale)
	}

	counts := capture.drain(t)
	if counts[database.NotificationTypeAlertFire] != 1 {
		t.Errorf("fire notifications = %d, want 1",
			counts[database.NotificationTypeAlertFire])
	}
}

// TestProbeUnavailableAlertNotRaisedWhileProbeCollects verifies the
// evaluator says nothing about a healthy probe.
func TestProbeUnavailableAlertNotRaisedWhileProbeCollects(t *testing.T) {
	engine, pool, _, ruleID, _, cleanup := newProbeUnavailableEnv(t,
		"probe-unavailable-healthy")
	defer cleanup()

	engine.evaluateThresholds(context.Background())

	if alerts := readProbeUnavailableAlerts(t, pool, ruleID); len(alerts) != 0 {
		t.Errorf("alerts = %+v, want none for an available probe", alerts)
	}
}

// TestProbeUnavailableAlertFallsBackWithoutAReason covers the NULL
// unavailable_reason case, where the description must still read as a
// whole sentence.
func TestProbeUnavailableAlertFallsBackWithoutAReason(t *testing.T) {
	engine, pool, _, ruleID, connID, cleanup := newProbeUnavailableEnv(t,
		"probe-unavailable-no-reason")
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, probeUnavailableNoReasonUpdateSQL, connID,
		"pg_stat_activity"); err != nil {
		t.Fatalf("failed to mark the probe unavailable: %v", err)
	}

	engine.evaluateThresholds(ctx)

	alerts := readProbeUnavailableAlerts(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Fatalf("alerts = %+v, want one", alerts)
	}
	if !strings.Contains(alerts[0].description, "no reason recorded") {
		t.Errorf("description = %q, want the stand-in reason", alerts[0].description)
	}
}

// TestProbeUnavailableAlertHonoursOperatorControls checks that the
// evaluator obeys the same three controls as the staleness evaluator: the
// rule's own default_enabled, a per-connection override that disables it,
// and an active blackout. The fourth case proves that a blackout lookup
// failure does not silence the rule, since a broken blackouts table says
// nothing about whether the probe has stopped collecting.
func TestProbeUnavailableAlertHonoursOperatorControls(t *testing.T) {
	cases := []struct {
		name      string
		setUp     func(t *testing.T, pool *pgxpool.Pool, ruleID int64, connID int)
		wantAlert bool
	}{
		{
			name: "rule disabled",
			setUp: func(t *testing.T, pool *pgxpool.Pool, ruleID int64, _ int) {
				if _, err := pool.Exec(context.Background(),
					probeUnavailableRuleDisableSQL, ruleID); err != nil {
					t.Fatalf("failed to disable the rule: %v", err)
				}
			},
		},
		{
			name: "disabled by a per-connection override",
			setUp: func(t *testing.T, pool *pgxpool.Pool, ruleID int64, connID int) {
				if _, err := pool.Exec(context.Background(),
					probeUnavailableThresholdOverrideInsertSQL, ruleID, connID); err != nil {
					t.Fatalf("failed to insert the override: %v", err)
				}
			},
		},
		{
			name: "blackout active",
			setUp: func(t *testing.T, pool *pgxpool.Pool, _ int64, connID int) {
				if _, err := pool.Exec(context.Background(),
					probeUnavailableBlackoutInsertSQL, connID); err != nil {
					t.Fatalf("failed to insert the blackout: %v", err)
				}
			},
		},
		{
			name: "blackout lookup failure",
			setUp: func(t *testing.T, pool *pgxpool.Pool, _ int64, _ int) {
				if _, err := pool.Exec(context.Background(),
					dropBlackoutsTableSQL); err != nil {
					t.Fatalf("failed to drop blackouts: %v", err)
				}
			},
			wantAlert: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, pool, _, ruleID, connID, cleanup := newProbeUnavailableEnv(t,
				"probe-unavailable-controls")
			defer cleanup()

			ctx := context.Background()
			markProbeUnavailable(t, pool, connID, "pg_stat_activity", "gone")
			tc.setUp(t, pool, ruleID, connID)

			engine.evaluateProbeScopedRules(ctx)

			alerts := readProbeUnavailableAlerts(t, pool, ruleID)
			if tc.wantAlert && len(alerts) != 1 {
				t.Errorf("alerts = %+v, want one", alerts)
			}
			if !tc.wantAlert && len(alerts) != 0 {
				t.Errorf("alerts = %+v, want none", alerts)
			}
		})
	}
}

// TestProbeUnavailableAlertUpdatedRatherThanDuplicated verifies that a
// second evaluation pass over a probe that is still unavailable updates
// the existing alert rather than raising another one.
func TestProbeUnavailableAlertUpdatedRatherThanDuplicated(t *testing.T) {
	engine, pool, _, ruleID, connID, cleanup := newProbeUnavailableEnv(t,
		"probe-unavailable-once")
	defer cleanup()

	ctx := context.Background()
	markProbeUnavailable(t, pool, connID, "pg_stat_activity", "gone")

	engine.evaluateProbeScopedRules(ctx)
	engine.evaluateProbeScopedRules(ctx)

	alerts := readProbeUnavailableAlerts(t, pool, ruleID)
	if len(alerts) != 1 || alerts[0].status != "active" {
		t.Errorf("alerts = %+v, want exactly one active alert", alerts)
	}
}

// TestProbeUnavailableAlertRespectsCooldown verifies the flap guard: an
// alert cleared moments ago is not immediately re-raised whilst the probe
// is still unavailable.
func TestProbeUnavailableAlertRespectsCooldown(t *testing.T) {
	engine, pool, _, ruleID, connID, cleanup := newProbeUnavailableEnv(t,
		"probe-unavailable-cooldown")
	defer cleanup()

	ctx := context.Background()
	markProbeUnavailable(t, pool, connID, "pg_stat_activity", "gone")

	engine.evaluateProbeScopedRules(ctx)
	alerts := readProbeUnavailableAlerts(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Fatalf("alerts = %+v, want one", alerts)
	}

	if _, err := pool.Exec(ctx, probeUnavailableClearAlertSQL, alerts[0].id); err != nil {
		t.Fatalf("failed to clear the alert: %v", err)
	}

	engine.evaluateProbeScopedRules(ctx)

	alerts = readProbeUnavailableAlerts(t, pool, ruleID)
	if len(alerts) != 1 || alerts[0].status != "cleared" {
		t.Errorf("alerts = %+v, want the cleared alert and no replacement", alerts)
	}
}

// TestProbeUnavailableAlertClearsWhenProbeCollectsAgain covers the
// resolution path that matters most: the probe becomes available again,
// so the alert clears with a value of 1 and the operator sees a line
// saying so.
func TestProbeUnavailableAlertClearsWhenProbeCollectsAgain(t *testing.T) {
	engine, pool, capture, ruleID, connID, cleanup := newProbeUnavailableEnv(t,
		"probe-unavailable-recovers")
	defer cleanup()

	ctx := context.Background()
	markProbeUnavailable(t, pool, connID, "pg_stat_activity", "gone")
	engine.evaluateProbeScopedRules(ctx)
	if alerts := readProbeUnavailableAlerts(t, pool, ruleID); len(alerts) != 1 {
		t.Fatalf("alerts = %+v, want one", alerts)
	}

	markProbeAvailable(t, pool, connID, "pg_stat_activity")

	if engine.debug {
		t.Fatal("the test engine has debug logging on, which would make the assertion meaningless")
	}
	logged := captureEngineStderr(t, func() { engine.cleanResolvedAlerts(ctx) })
	if !strings.Contains(logged, "is available again") {
		t.Errorf("engine log = %q, want the recovery reported", logged)
	}

	alerts := readProbeUnavailableAlerts(t, pool, ruleID)
	if len(alerts) != 1 || alerts[0].status != "cleared" {
		t.Errorf("alerts = %+v, want one cleared alert", alerts)
	}

	counts := capture.drain(t)
	if counts[database.NotificationTypeAlertClear] != 1 {
		t.Errorf("clear notifications = %d, want 1",
			counts[database.NotificationTypeAlertClear])
	}
}

// TestProbeUnavailableAlertStaysActiveWhilstProbeIsUnavailable verifies
// the second resolution case, and with it that the cleaner does not route
// a probe_unavailable alert through the staleness path: the alert stays
// active and its description keeps the evaluator's wording rather than
// gaining the hold wording issue #465 added for staleness alerts.
func TestProbeUnavailableAlertStaysActiveWhilstProbeIsUnavailable(t *testing.T) {
	engine, pool, _, ruleID, connID, cleanup := newProbeUnavailableEnv(t,
		"probe-unavailable-held")
	defer cleanup()

	ctx := context.Background()
	markProbeUnavailable(t, pool, connID, "pg_stat_activity", "gone")
	engine.evaluateProbeScopedRules(ctx)

	raised := readProbeUnavailableAlerts(t, pool, ruleID)
	if len(raised) != 1 {
		t.Fatalf("alerts = %+v, want one", raised)
	}

	engine.cleanResolvedAlerts(ctx)

	after := readProbeUnavailableAlerts(t, pool, ruleID)
	if len(after) != 1 || after[0].status != "active" {
		t.Fatalf("alerts = %+v, want one that is still active", after)
	}
	if strings.HasPrefix(after[0].description, unavailableProbeDescriptionPrefix) {
		t.Errorf("description = %q, want the evaluator's wording rather than the staleness hold",
			after[0].description)
	}
	if after[0].description != raised[0].description {
		t.Errorf("description changed from %q to %q", raised[0].description,
			after[0].description)
	}
}

// TestProbeUnavailableAlertClearsWhenProbeLeavesTheView covers the third
// resolution case. A probe that has left the staleness view altogether
// has been disabled, unmonitored or deleted, all of which are deliberate
// operator action, so the alert clears and says so at normal log level.
func TestProbeUnavailableAlertClearsWhenProbeLeavesTheView(t *testing.T) {
	engine, pool, _, ruleID, connID, cleanup := newProbeUnavailableEnv(t,
		"probe-unavailable-gone")
	defer cleanup()

	ctx := context.Background()
	markProbeUnavailable(t, pool, connID, "pg_stat_activity", "gone")
	engine.evaluateProbeScopedRules(ctx)
	if alerts := readProbeUnavailableAlerts(t, pool, ruleID); len(alerts) != 1 {
		t.Fatalf("alerts = %+v, want one", alerts)
	}

	if _, err := pool.Exec(ctx, stalenessProbeAvailabilityDeleteSQL, connID,
		"pg_stat_activity"); err != nil {
		t.Fatalf("failed to delete the availability row: %v", err)
	}

	if engine.debug {
		t.Fatal("the test engine has debug logging on, which would make the assertion meaningless")
	}
	logged := captureEngineStderr(t, func() { engine.cleanResolvedAlerts(ctx) })
	if !strings.Contains(logged, "is no longer reported") {
		t.Errorf("engine log = %q, want the clear reported at normal level", logged)
	}

	alerts := readProbeUnavailableAlerts(t, pool, ruleID)
	if len(alerts) != 1 || alerts[0].status != "cleared" {
		t.Errorf("alerts = %+v, want one cleared alert", alerts)
	}
}

// TestProbeUnavailableAndStalenessAlertsCoexist pins the intended
// interaction between the two probe-scoped rules. A probe that went
// unavailable long enough ago to be stale as well carries both alerts at
// once, and the cleaner resolves each on its own terms: the staleness
// alert is held with the hold wording, whilst the probe_unavailable alert
// stays active with the wording it was raised with.
func TestProbeUnavailableAndStalenessAlertsCoexist(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	stalenessRuleID := seedStalenessRule(t, pool)
	unavailableRuleID := seedProbeUnavailableRule(t, pool)
	connID := insertTestConnection(t, pool, "probe-unavailable-coexist")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	// The staleness alert fires first, whilst the probe is still
	// available, which is the sequence issue #465 preserves.
	engine.evaluateThresholds(ctx)
	if alerts := readStalenessAlertsForRule(t, pool, stalenessRuleID); len(alerts) != 1 {
		t.Fatalf("staleness alerts = %+v, want one", alerts)
	}

	markProbeUnavailable(t, pool, connID, "pg_stat_activity", "gone")
	engine.evaluateThresholds(ctx)
	engine.cleanResolvedAlerts(ctx)

	staleness := readProbeUnavailableAlerts(t, pool, stalenessRuleID)
	if len(staleness) != 1 || staleness[0].status != "active" {
		t.Fatalf("staleness alerts = %+v, want one that is still active", staleness)
	}
	if !strings.HasPrefix(staleness[0].description, unavailableProbeDescriptionPrefix) {
		t.Errorf("staleness description = %q, want the hold wording",
			staleness[0].description)
	}

	unavailable := readProbeUnavailableAlerts(t, pool, unavailableRuleID)
	if len(unavailable) != 1 || unavailable[0].status != "active" {
		t.Fatalf("probe_unavailable alerts = %+v, want one that is still active",
			unavailable)
	}
	if strings.HasPrefix(unavailable[0].description, unavailableProbeDescriptionPrefix) {
		t.Errorf("probe_unavailable description = %q, want the evaluator's wording",
			unavailable[0].description)
	}
}

// TestProbeUnavailableEvaluatorWithoutItsRule covers the case of an
// alerter running against a datastore an older collector built, where the
// rule has not been seeded: the pass gives up quietly rather than
// failing.
func TestProbeUnavailableEvaluatorWithoutItsRule(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	connID := insertTestConnection(t, pool, "probe-unavailable-no-rule")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "10 seconds")
	markProbeUnavailable(t, pool, connID, "pg_stat_activity", "gone")

	engine.evaluateProbeScopedRules(ctx)

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM alerts`).Scan(&count); err != nil {
		t.Fatalf("failed to count alerts: %v", err)
	}
	if count != 0 {
		t.Errorf("alerts = %d, want none without the rule", count)
	}
}

// TestProbeUnavailableEvaluatorSurvivesDatastoreFailures drives the error
// branches of the evaluator and of its resolution check. None of them may
// panic, and none may invent or retire an alert on the strength of a
// failed read.
func TestProbeUnavailableEvaluatorSurvivesDatastoreFailures(t *testing.T) {
	t.Run("staleness read failure", func(t *testing.T) {
		engine, pool, _, ruleID, _, cleanup := newProbeUnavailableEnv(t,
			"probe-unavailable-read-fails")
		defer cleanup()

		ctx := context.Background()
		if _, err := pool.Exec(ctx, dropProbeAvailabilityTableSQL); err != nil {
			t.Fatalf("failed to drop probe_availability: %v", err)
		}

		engine.evaluateProbeScopedRules(ctx)

		if alerts := readProbeUnavailableAlerts(t, pool, ruleID); len(alerts) != 0 {
			t.Errorf("alerts = %+v, want none from a failed read", alerts)
		}
	})

	t.Run("existing alert lookup failure", func(t *testing.T) {
		engine, pool, _, ruleID, connID, cleanup := newProbeUnavailableEnv(t,
			"probe-unavailable-lookup-fails")
		defer cleanup()

		ctx := context.Background()
		markProbeUnavailable(t, pool, connID, "pg_stat_activity", "gone")

		// Dropping the alerts table breaks the lookup that guards
		// against duplicates, so the pass must decline to create
		// anything rather than risk a second alert.
		if _, err := pool.Exec(ctx, dropAlertsTableSQL); err != nil {
			t.Fatalf("failed to drop alerts: %v", err)
		}

		engine.evaluateProbeScopedRules(ctx)

		// Nothing to assert on beyond the absence of a panic: the table
		// the alert would have gone into no longer exists.
		_ = ruleID
	})

	t.Run("alert insert failure", func(t *testing.T) {
		engine, pool, _, ruleID, connID, cleanup := newProbeUnavailableEnv(t,
			"probe-unavailable-insert-fails")
		defer cleanup()

		ctx := context.Background()
		markProbeUnavailable(t, pool, connID, "pg_stat_activity", "gone")
		if _, err := pool.Exec(ctx, rejectProbeAlertInsertsSQL); err != nil {
			t.Fatalf("failed to add the rejecting constraint: %v", err)
		}

		engine.evaluateProbeScopedRules(ctx)

		if alerts := readProbeUnavailableAlerts(t, pool, ruleID); len(alerts) != 0 {
			t.Errorf("alerts = %+v, want none when the insert is rejected", alerts)
		}
	})

	t.Run("alert update failure", func(t *testing.T) {
		engine, pool, _, ruleID, connID, cleanup := newProbeUnavailableEnv(t,
			"probe-unavailable-update-fails")
		defer cleanup()

		ctx := context.Background()
		markProbeUnavailable(t, pool, connID, "pg_stat_activity", "gone")
		engine.evaluateProbeScopedRules(ctx)
		if alerts := readProbeUnavailableAlerts(t, pool, ruleID); len(alerts) != 1 {
			t.Fatalf("alerts = %+v, want one", alerts)
		}

		if _, err := pool.Exec(ctx, createRejectAlertUpdatesFuncSQL); err != nil {
			t.Fatalf("failed to create the rejecting function: %v", err)
		}
		defer func() {
			if _, err := pool.Exec(context.Background(),
				dropRejectAlertUpdatesSQL); err != nil {
				t.Logf("failed to drop the rejecting function: %v", err)
			}
		}()
		if _, err := pool.Exec(ctx, createRejectAlertUpdatesTriggerSQL); err != nil {
			t.Fatalf("failed to create the rejecting trigger: %v", err)
		}

		engine.evaluateProbeScopedRules(ctx)

		alerts := readProbeUnavailableAlerts(t, pool, ruleID)
		if len(alerts) != 1 || alerts[0].status != "active" {
			t.Errorf("alerts = %+v, want the one alert left as it was", alerts)
		}
	})

	t.Run("cooldown lookup failure", func(t *testing.T) {
		engine, pool, _, ruleID, connID, cleanup := newProbeUnavailableEnv(t,
			"probe-unavailable-cooldown-fails")
		defer cleanup()

		ctx := context.Background()
		markProbeUnavailable(t, pool, connID, "pg_stat_activity", "gone")

		// Retyping cleared_at breaks only the cooldown query's
		// comparison against NOW(): the duplicate lookup runs first and
		// returns no rows, so it never scans the column. A cooldown that
		// cannot be read must not stop the alert being raised, since the
		// probe has demonstrably stopped collecting.
		if _, err := pool.Exec(ctx, probeUnavailableBreakClearedAtSQL); err != nil {
			t.Fatalf("failed to retype cleared_at: %v", err)
		}

		engine.evaluateProbeScopedRules(ctx)

		if alerts := readProbeUnavailableAlerts(t, pool, ruleID); len(alerts) != 1 {
			t.Errorf("alerts = %+v, want the alert raised despite the failed cooldown read",
				alerts)
		}
	})

	t.Run("resolution read failure", func(t *testing.T) {
		engine, pool, _, ruleID, connID, cleanup := newProbeUnavailableEnv(t,
			"probe-unavailable-resolve-fails")
		defer cleanup()

		ctx := context.Background()
		markProbeUnavailable(t, pool, connID, "pg_stat_activity", "gone")
		engine.evaluateProbeScopedRules(ctx)
		if alerts := readProbeUnavailableAlerts(t, pool, ruleID); len(alerts) != 1 {
			t.Fatalf("alerts = %+v, want one", alerts)
		}

		if _, err := pool.Exec(ctx, dropProbeAvailabilityTableSQL); err != nil {
			t.Fatalf("failed to drop probe_availability: %v", err)
		}

		engine.cleanResolvedAlerts(ctx)

		alerts := readProbeUnavailableAlerts(t, pool, ruleID)
		if len(alerts) != 1 || alerts[0].status != "active" {
			t.Errorf("alerts = %+v, want the alert left active by a failed read", alerts)
		}
	})
}
