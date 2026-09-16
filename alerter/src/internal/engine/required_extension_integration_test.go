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

// The tests in this file cover the required_extension gate added for
// GitHub issue #409. They build on the Spock engine environment, which
// already carries metrics.pg_replication_slots, and add
// metrics.pg_extension so a rule on pg_replication_slots.max_retained_bytes
// can be gated on an extension the connection may or may not report.

const (
	extGateRuleName  = "ext_gate_slot_retention"
	extGateMetric    = "pg_replication_slots.max_retained_bytes"
	extGateExtension = "spock"
	extGateOneGiB    = int64(1024) * 1024 * 1024
)

// createPgExtensionTable adds metrics.pg_extension to the Spock engine
// schema. The metrics schema is dropped by the environment's teardown.
func createPgExtensionTable(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), `
		CREATE TABLE metrics.pg_extension (
		    connection_id INTEGER NOT NULL,
		    database_name TEXT NOT NULL,
		    extname TEXT NOT NULL,
		    extversion TEXT,
		    collected_at TIMESTAMPTZ NOT NULL,
		    PRIMARY KEY (connection_id, database_name, extname, collected_at)
		)
	`); err != nil {
		t.Fatalf("failed to create metrics.pg_extension: %v", err)
	}
}

// insertExtensionSnapshot writes one pg_extension snapshot for connID at
// collectedAt listing the given extension names in the postgres database.
func insertExtensionSnapshot(t *testing.T, pool *pgxpool.Pool, connID int,
	collectedAt time.Time, extnames ...string) {
	t.Helper()

	for _, name := range extnames {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO metrics.pg_extension
			    (connection_id, database_name, extname, extversion, collected_at)
			VALUES ($1, 'postgres', $2, '1.0', $3)
		`, connID, name, collectedAt); err != nil {
			t.Fatalf("failed to insert pg_extension row: %v", err)
		}
	}
}

// insertGatedRule seeds an enabled rule on the slot retention metric that
// requires extGateExtension, and returns its id.
func insertGatedRule(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var id int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO alert_rules (name, description, category, metric_name,
		    default_operator, default_threshold, default_severity,
		    default_enabled, required_extension, is_built_in)
		VALUES ($1, 'Gated on an extension', 'replication', $2,
		    '>=', $3, 'warning', TRUE, $4, FALSE)
		RETURNING id
	`, extGateRuleName, extGateMetric, extGateOneGiB, extGateExtension).Scan(&id); err != nil {
		t.Fatalf("failed to insert gated rule: %v", err)
	}
	return id
}

// newExtensionGateEnv returns a Spock engine environment with
// metrics.pg_extension created and the gated rule seeded as the only
// enabled rule.
func newExtensionGateEnv(t *testing.T) (*Engine, *database.Datastore, *pgxpool.Pool, int64, func()) {
	t.Helper()

	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	createPgExtensionTable(t, pool)
	ruleID := insertGatedRule(t, pool)
	return engine, ds, pool, ruleID, cleanup
}

// TestEngine_RequiredExtension_EvaluationGate verifies that a rule with a
// required_extension fires only on connections whose newest pg_extension
// snapshot lists the extension. Three connections carry identical
// violating data; only the one that reports the extension gets an alert.
func TestEngine_RequiredExtension_EvaluationGate(t *testing.T) {
	engine, ds, pool, ruleID, cleanup := newExtensionGateEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()

	withExt := insertTestConnection(t, pool, "ext-gate-with-extension")
	noRows := insertTestConnection(t, pool, "ext-gate-no-snapshot")
	dropped := insertTestConnection(t, pool, "ext-gate-dropped")

	insertExtensionSnapshot(t, pool, withExt, now.Add(-24*time.Hour), "plpgsql", extGateExtension)
	// dropped had the extension two days ago; its newest snapshot lacks it.
	insertExtensionSnapshot(t, pool, dropped, now.Add(-48*time.Hour), "plpgsql", extGateExtension)
	insertExtensionSnapshot(t, pool, dropped, now.Add(-1*time.Hour), "plpgsql")

	sample := now.Add(-2 * time.Minute)
	for _, connID := range []int{withExt, noRows, dropped} {
		insertReplicationSlotRow(t, pool, connID, sample, "slot_a", true, 2*extGateOneGiB)
	}

	engine.evaluateThresholds(ctx)

	assertAlertFired(t, ds, ruleID, withExt, "warning")
	for _, connID := range []int{noRows, dropped} {
		if alert := activeAlertForRule(t, ds, ruleID, connID); alert != nil {
			t.Errorf("connection %d without %s unexpectedly has alert %d",
				connID, extGateExtension, alert.ID)
		}
	}
}

// TestEngine_RequiredExtension_LookupErrorEvaluatesWithoutGate verifies
// that when the pg_extension lookup itself fails the rule is still
// evaluated, so a datastore problem cannot silence every gated rule.
func TestEngine_RequiredExtension_LookupErrorEvaluatesWithoutGate(t *testing.T) {
	engine, ds, pool, ruleID, cleanup := newExtensionGateEnv(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "ext-gate-lookup-error")
	insertReplicationSlotRow(t, pool, connID, time.Now().UTC().Add(-2*time.Minute),
		"slot_a", true, 2*extGateOneGiB)

	// Make the lookup fail by removing the table it reads.
	if _, err := pool.Exec(ctx, `DROP TABLE metrics.pg_extension`); err != nil {
		t.Fatalf("failed to drop metrics.pg_extension: %v", err)
	}

	engine.evaluateThresholds(ctx)

	assertAlertFired(t, ds, ruleID, connID, "warning")
}

// TestEngine_RequiredExtension_ResolutionGate verifies that an active
// alert is left in place while the connection's newest snapshot lacks the
// rule's extension, even though the metric would otherwise report the
// condition resolved, and that it clears normally once the extension is
// reported again.
func TestEngine_RequiredExtension_ResolutionGate(t *testing.T) {
	engine, ds, pool, ruleID, cleanup := newExtensionGateEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	connID := insertTestConnection(t, pool, "ext-gate-resolution")

	insertExtensionSnapshot(t, pool, connID, now.Add(-24*time.Hour), "plpgsql", extGateExtension)
	insertReplicationSlotRow(t, pool, connID, now.Add(-3*time.Minute), "slot_a", true, 2*extGateOneGiB)

	// The metric clears when absent, and the cleaner only believes an
	// absent row while the probe behind it is still reporting, so this
	// connection has to look like one a collector is running against.
	// It is seeded from the start deliberately: the first assertion
	// below then proves the extension gate is what holds the alert open,
	// not a stalled probe. See GitHub issue #407.
	seedFreshProbe(t, pool, connID, "pg_replication_slots")

	engine.evaluateThresholds(ctx)
	alert := assertAlertFired(t, ds, ruleID, connID, "warning")

	// The extension disappears and the slot data would clear the alert.
	insertExtensionSnapshot(t, pool, connID, now.Add(-2*time.Minute), "plpgsql")
	truncateReplicationSlots(t, pool, connID)

	engine.cleanResolvedAlerts(ctx)

	still := activeAlertForRule(t, ds, ruleID, connID)
	if still == nil || still.ID != alert.ID {
		t.Fatalf("expected alert %d to stay active while %s is absent, got %+v",
			alert.ID, extGateExtension, still)
	}
	if status := getAlertStatus(t, pool, alert.ID); status != "active" {
		t.Errorf("alert status = %q, want active", status)
	}

	// The extension comes back; the alert now resolves on the metric.
	insertExtensionSnapshot(t, pool, connID, now.Add(-1*time.Minute), "plpgsql", extGateExtension)
	engine.cleanResolvedAlerts(ctx)

	assertAlertCleared(t, ds, pool, ruleID, connID, alert.ID)
}

// TestEngine_RequiredExtension_ResolutionRuleLookupError verifies that
// an alert whose rule cannot be loaded is checked without the gate. The
// in-memory alert points at a rule id that does not exist, while the
// stored alert row has no rule so that clearing it succeeds.
func TestEngine_RequiredExtension_ResolutionRuleLookupError(t *testing.T) {
	engine, _, pool, _, cleanup := newExtensionGateEnv(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "ext-gate-rule-missing")
	alertID := insertTestAlert(t, pool, "threshold", nil, connID, "warning", "active", "orphaned rule")

	bogusRule := int64(999999)
	alert := &database.Alert{
		ID:             alertID,
		AlertType:      "threshold",
		RuleID:         &bogusRule,
		ConnectionID:   connID,
		MetricName:     strPtr(extGateMetric),
		ThresholdValue: float64Ptr(float64(extGateOneGiB)),
		Operator:       strPtr(">="),
		Title:          "orphaned rule",
	}

	// No slot rows exist, so the metric reports no data; with the probe
	// behind it still reporting (GitHub issue #407) that absence is a
	// recovery, and the alert clears once the gate has been skipped.
	seedFreshProbe(t, pool, connID, "pg_replication_slots")

	gates := engine.resolveExtensionGates(ctx, []*database.Alert{alert})
	if gate, ok := gates[bogusRule]; !ok || gate != nil {
		t.Errorf("gate for an unloadable rule = %v (present %v), want a nil gate",
			gate, ok)
	}
	engine.checkAlertResolved(ctx, alert, gates[bogusRule], nil)

	if status := getAlertStatus(t, pool, alertID); status != "cleared" {
		t.Errorf("alert status = %q, want cleared", status)
	}
}

// TestEngine_RequiredExtension_GateResolutionSkipsUngatedAlerts verifies
// that resolveExtensionGates ignores alerts it cannot gate, so neither an
// alert without a rule id nor a non-threshold alert reaches the datastore.
// A nil Engine datastore makes any lookup panic, which is the assertion.
func TestEngine_RequiredExtension_GateResolutionSkipsUngatedAlerts(t *testing.T) {
	engine := &Engine{}
	ruleID := int64(7)
	gates := engine.resolveExtensionGates(context.Background(), []*database.Alert{
		{ID: 1, AlertType: "threshold", RuleID: nil},
		{ID: 2, AlertType: "anomaly", RuleID: &ruleID},
	})
	if len(gates) != 0 {
		t.Errorf("gates = %v, want none", gates)
	}
}
