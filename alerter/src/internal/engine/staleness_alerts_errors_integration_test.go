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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// The statements below break the schema in a controlled way so the error
// branches of the staleness evaluator and the alert cleaner can be driven
// from an integration test. Each one disables exactly one database
// operation, leaving the rest of the path working.
const (
	dropProbeAvailabilityTableSQL = `DROP TABLE probe_availability CASCADE`

	dropAlertsTableSQL = `DROP TABLE alerts CASCADE`

	rejectProbeAlertInsertsSQL = `
        ALTER TABLE alerts
            ADD CONSTRAINT reject_probe_alerts CHECK (probe_name IS NULL)
    `

	createRejectAlertUpdatesFuncSQL = `
        CREATE OR REPLACE FUNCTION reject_alert_updates() RETURNS trigger AS $$
        BEGIN
            RAISE EXCEPTION 'alert updates are rejected by this test';
        END;
        $$ LANGUAGE plpgsql
    `

	createRejectAlertUpdatesTriggerSQL = `
        CREATE TRIGGER reject_alert_updates
            BEFORE UPDATE ON alerts
            FOR EACH ROW EXECUTE FUNCTION reject_alert_updates()
    `

	dropRejectAlertUpdatesSQL = `
        DROP FUNCTION IF EXISTS reject_alert_updates() CASCADE
    `

	disableStalenessRuleSQL = `
        UPDATE alert_rules SET default_enabled = FALSE WHERE id = $1
    `

	stalenessThresholdOverrideInsertSQL = `
        INSERT INTO alert_thresholds
            (rule_id, scope, connection_id, operator, threshold, severity, enabled)
        VALUES ($1, 'server', $2, '>', 3, 'warning', FALSE)
    `

	stalenessActiveBlackoutInsertSQL = `
        INSERT INTO blackouts
            (scope, connection_id, reason, start_time, end_time, created_by)
        VALUES ('server', $1, 'test blackout',
                NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 hour', 'test')
    `

	dropBlackoutsTableSQL = `DROP TABLE blackouts CASCADE`

	stalenessSlotSampleInsertSQL = `
        INSERT INTO metrics.pg_replication_slots
            (connection_id, slot_name, active, retained_bytes, collected_at)
        VALUES ($1, $2, FALSE, 5000000000, NOW())
    `

	stalenessRegistryMetricAlertInsertSQL = `
        INSERT INTO alerts
            (alert_type, rule_id, connection_id, database_name, metric_name,
             metric_value, threshold_value, operator, severity, title,
             description, status, triggered_at)
        VALUES ('threshold', $1, $2, $3, 'pg_replication_slots.max_retained_bytes',
                5000000000, 1000000000, '>', 'warning', 'slot retention',
                'desc', 'active', NOW())
        RETURNING id
    `
)

// TestStalenessEvaluatorSurvivesStalenessQueryFailure covers the two paths
// that read probe_availability when the table cannot be queried at all: the
// evaluator gives up for this pass, and the cleaner leaves the alert alone
// rather than guessing that the condition resolved.
func TestStalenessEvaluatorSurvivesStalenessQueryFailure(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-query-failure")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	engine.evaluateThresholds(ctx)
	if alerts := readStalenessAlertsForRule(t, pool, ruleID); len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(alerts))
	}

	if _, err := pool.Exec(ctx, dropProbeAvailabilityTableSQL); err != nil {
		t.Fatalf("failed to drop probe_availability: %v", err)
	}

	// Neither pass may change anything: the evaluator cannot see the
	// probes, and the cleaner cannot tell whether the alert resolved.
	engine.evaluateMetricStaleness(ctx)
	engine.cleanResolvedAlerts(ctx)

	alerts := readStalenessAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(alerts))
	}
	if alerts[0].status != "active" {
		t.Errorf("alert status = %q, want \"active\"; the staleness query failed "+
			"and says nothing about the condition", alerts[0].status)
	}
}

// TestStalenessEvaluatorSurvivesAlertLookupFailure covers the branch where
// the existing-alert lookup fails. The evaluator must not create a fresh
// alert on a failed lookup, because that would duplicate an alert that may
// already exist.
func TestStalenessEvaluatorSurvivesAlertLookupFailure(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installStalenessNotificationCapture(t, engine)
	seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-lookup-failure")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	if _, err := pool.Exec(ctx, dropAlertsTableSQL); err != nil {
		t.Fatalf("failed to drop alerts: %v", err)
	}

	engine.evaluateMetricStaleness(ctx)

	if counts := capture.drain(t); counts[database.NotificationTypeAlertFire] != 0 {
		t.Errorf("fire notifications = %d, want 0 when the lookup failed",
			counts[database.NotificationTypeAlertFire])
	}
}

// TestStalenessEvaluatorSurvivesAlertCreateFailure covers the branch where
// the INSERT itself fails; the evaluator logs and moves on to the next
// probe rather than notifying about an alert that was never stored.
func TestStalenessEvaluatorSurvivesAlertCreateFailure(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-create-failure")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	if _, err := pool.Exec(ctx, rejectProbeAlertInsertsSQL); err != nil {
		t.Fatalf("failed to add the rejecting constraint: %v", err)
	}

	engine.evaluateMetricStaleness(ctx)

	if alerts := readStalenessAlertsForRule(t, pool, ruleID); len(alerts) != 0 {
		t.Errorf("alert rows = %d, want 0 when the insert is rejected", len(alerts))
	}
	if counts := capture.drain(t); counts[database.NotificationTypeAlertFire] != 0 {
		t.Errorf("fire notifications = %d, want 0 when the insert failed",
			counts[database.NotificationTypeAlertFire])
	}
}

// TestStalenessEvaluatorSurvivesAlertUpdateFailure covers the branch where
// refreshing an existing alert's values fails. The alert must survive
// unchanged rather than being duplicated.
func TestStalenessEvaluatorSurvivesAlertUpdateFailure(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-update-failure")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	engine.evaluateMetricStaleness(ctx)
	if alerts := readStalenessAlertsForRule(t, pool, ruleID); len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(alerts))
	}

	if _, err := pool.Exec(ctx, createRejectAlertUpdatesFuncSQL); err != nil {
		t.Fatalf("failed to create the rejecting trigger function: %v", err)
	}
	if _, err := pool.Exec(ctx, createRejectAlertUpdatesTriggerSQL); err != nil {
		t.Fatalf("failed to install the rejecting trigger: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), dropRejectAlertUpdatesSQL); err != nil {
			t.Logf("failed to drop the rejecting trigger: %v", err)
		}
	})

	engine.evaluateMetricStaleness(ctx)

	alerts := readStalenessAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Errorf("alert rows = %d, want 1 after a failed update", len(alerts))
	}
}

// TestStalenessEvaluatorSkipConditions covers the guards that stop the
// staleness evaluator raising an alert: a disabled rule, an active
// blackout, and a per-connection threshold override that disables the rule.
// Each case runs against a probe that is comfortably stale, so an alert
// would appear were the guard not honored.
func TestStalenessEvaluatorSkipConditions(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, pool *pgxpool.Pool, ruleID int64, connID int)
	}{
		{
			name: "rule disabled",
			prepare: func(t *testing.T, pool *pgxpool.Pool, ruleID int64, connID int) {
				if _, err := pool.Exec(context.Background(),
					disableStalenessRuleSQL, ruleID); err != nil {
					t.Fatalf("failed to disable the rule: %v", err)
				}
			},
		},
		{
			name: "blackout active",
			prepare: func(t *testing.T, pool *pgxpool.Pool, ruleID int64, connID int) {
				if _, err := pool.Exec(context.Background(),
					stalenessActiveBlackoutInsertSQL, connID); err != nil {
					t.Fatalf("failed to insert blackout: %v", err)
				}
			},
		},
		{
			name: "rule disabled for connection",
			prepare: func(t *testing.T, pool *pgxpool.Pool, ruleID int64, connID int) {
				if _, err := pool.Exec(context.Background(),
					stalenessThresholdOverrideInsertSQL, ruleID, connID); err != nil {
					t.Fatalf("failed to insert threshold override: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, _, pool, cleanup := newEngineSpockTestEnv(t)
			defer cleanup()

			ctx := context.Background()
			installStalenessNotificationCapture(t, engine)
			ruleID := seedStalenessRule(t, pool)
			connID := insertTestConnection(t, pool, "staleness-skip")
			seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

			tt.prepare(t, pool, ruleID, connID)

			engine.evaluateMetricStaleness(ctx)

			if alerts := readStalenessAlertsForRule(t, pool, ruleID); len(alerts) != 0 {
				t.Errorf("alert rows = %d, want 0", len(alerts))
			}
		})
	}
}

// TestStalenessEvaluatorSurvivesBlackoutLookupFailure covers the branch
// where the blackout check itself fails. A failed check must not stop the
// evaluator raising the alert, because suppressing alerts on a database
// error would hide real problems.
func TestStalenessEvaluatorSurvivesBlackoutLookupFailure(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-blackout-failure")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	if _, err := pool.Exec(ctx, dropBlackoutsTableSQL); err != nil {
		t.Fatalf("failed to drop blackouts: %v", err)
	}

	engine.evaluateMetricStaleness(ctx)

	alerts := readStalenessAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(alerts))
	}
	if alerts[0].status != "active" {
		t.Errorf("alert status = %q, want \"active\"", alerts[0].status)
	}
}

// TestStalenessAlertIgnoresOtherProbesInTheView covers the loop skip in the
// staleness resolution check: entries for other connections and other
// probes must not be mistaken for the alert's own probe.
func TestStalenessAlertIgnoresOtherProbesInTheView(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-other-probes")
	otherConnID := insertTestConnection(t, pool, "staleness-other-conn")

	// A fresh probe on another connection, plus a fresh probe on this
	// connection, both of which the resolution check must skip over
	// before it finds the stale probe the alert belongs to.
	seedStaleProbe(t, pool, otherConnID, "pg_stat_bgwriter", "1 second")
	seedStaleProbe(t, pool, connID, "pg_stat_database", "1 second")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	engine.evaluateMetricStaleness(ctx)
	engine.cleanResolvedAlerts(ctx)

	alerts := readStalenessAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(alerts))
	}
	if alerts[0].status != "active" {
		t.Errorf("alert status = %q, want \"active\"; the probe is still stale",
			alerts[0].status)
	}
}

// TestCheckAlertResolvedClearsWhenConnectionOrDatabaseHasNoValue covers the
// registry-backed matching loop: values reported for a different connection,
// or for a row with no database name when the alert is database-scoped, do
// not count as the alert's own value, so the alert resolves.
func TestCheckAlertResolvedClearsWhenConnectionOrDatabaseHasNoValue(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	alertConnID := insertTestConnection(t, pool, "resolve-alert-conn")
	dataConnID := insertTestConnection(t, pool, "resolve-data-conn")

	// The only slot sample belongs to a different connection.
	if _, err := pool.Exec(ctx, stalenessSlotSampleInsertSQL, dataConnID, "slot_a"); err != nil {
		t.Fatalf("failed to seed replication slot sample: %v", err)
	}

	var alertID int64
	if err := pool.QueryRow(ctx, stalenessRegistryMetricAlertInsertSQL, ruleID, alertConnID,
		nil).Scan(&alertID); err != nil {
		t.Fatalf("failed to insert alert: %v", err)
	}

	// A second alert on the connection that does have data, but scoped to
	// a database the basic metric never reports.
	dbName := "app_db"
	var dbAlertID int64
	if err := pool.QueryRow(ctx, stalenessRegistryMetricAlertInsertSQL, ruleID, dataConnID,
		&dbName).Scan(&dbAlertID); err != nil {
		t.Fatalf("failed to insert database-scoped alert: %v", err)
	}

	engine.cleanResolvedAlerts(ctx)

	alerts := readStalenessAlertsForRule(t, pool, ruleID)
	if len(alerts) != 2 {
		t.Fatalf("alert rows = %d, want 2", len(alerts))
	}
	for _, alert := range alerts {
		if alert.status != "cleared" {
			t.Errorf("alert %d status = %q, want \"cleared\"; the metric reports "+
				"no value for it", alert.id, alert.status)
		}
	}
}
