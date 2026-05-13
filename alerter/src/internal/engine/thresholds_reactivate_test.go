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

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// TestFormatMetricValue exercises the nil-safe formatter used to log
// existing alerts' metric_value. The bare *float64 dereference that this
// helper replaces would panic for any alert row with NULL metric_value, so
// the helper is small but load-bearing for the auto-reactivation path.
func TestFormatMetricValue(t *testing.T) {
	if got := formatMetricValue(nil); got != "<nil>" {
		t.Errorf("formatMetricValue(nil) = %q, want %q", got, "<nil>")
	}
	v := 42.5
	if got := formatMetricValue(&v); got != "42.50" {
		t.Errorf("formatMetricValue(42.5) = %q, want %q", got, "42.50")
	}
	zero := 0.0
	if got := formatMetricValue(&zero); got != "0.00" {
		t.Errorf("formatMetricValue(0) = %q, want %q", got, "0.00")
	}
}

// TestEngine_TriggerThresholdAlert_ReactivatesOnSeverityChange covers the
// happy path for the bug-2 auto-reactivation: an acknowledged alert whose
// severity column no longer matches the rule's current severity must
// transition back to status='active' with the new severity written and any
// acknowledgment rows cleared.
func TestEngine_TriggerThresholdAlert_ReactivatesOnSeverityChange(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	ruleID := insertTestAlertRule(t, pool, "reactivate_on_change",
		"spock_exception_log.recent_count", ">=", 1.0, "warning")
	connID := insertTestConnection(t, pool, "reactivate-on-change-conn")

	value, threshold := 1.0, 1.0
	operator := ">="
	alert := &database.Alert{
		AlertType:      "threshold",
		RuleID:         &ruleID,
		ConnectionID:   connID,
		MetricName:     strPtr("spock_exception_log.recent_count"),
		MetricValue:    &value,
		ThresholdValue: &threshold,
		Operator:       &operator,
		Severity:       "warning",
		Title:          "reactivate_on_change",
		Description:    "test",
		Status:         "acknowledged",
		TriggeredAt:    time.Now(),
	}
	if err := ds.CreateAlert(ctx, alert); err != nil {
		t.Fatalf("create alert: %v", err)
	}
	insertTestAcknowledgment(t, pool, alert.ID, "acknowledge", false)

	rule := &database.AlertRule{
		ID:         ruleID,
		Name:       "reactivate_on_change",
		MetricName: "spock_exception_log.recent_count",
	}

	engine.triggerThresholdAlert(ctx, rule, value, threshold, operator, "critical", connID, nil, nil)

	if status := getAlertStatus(t, pool, alert.ID); status != "active" {
		t.Errorf("alert status = %q, want active", status)
	}
	var sev string
	if err := pool.QueryRow(ctx, `SELECT severity FROM alerts WHERE id=$1`, alert.ID).Scan(&sev); err != nil {
		t.Fatalf("read severity: %v", err)
	}
	if sev != "critical" {
		t.Errorf("alert severity = %q, want critical", sev)
	}
	if n := countAcknowledgments(t, pool, alert.ID); n != 0 {
		t.Errorf("acknowledgments after reactivation = %d, want 0", n)
	}
}

// TestEngine_TriggerThresholdAlert_DoesNotReactivateWhenSeverityUnchanged
// proves the reverse: a violated metric that re-fires at the same severity
// against an already-acknowledged alert must leave the alert acknowledged.
// This was the regression I worried about when moving the comparison ahead
// of UpdateAlertValues: the captured previousSeverity must equal the new
// severity for the no-op case.
func TestEngine_TriggerThresholdAlert_DoesNotReactivateWhenSeverityUnchanged(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	ruleID := insertTestAlertRule(t, pool, "no_reactivate_same_severity",
		"spock_exception_log.recent_count", ">=", 1.0, "warning")
	connID := insertTestConnection(t, pool, "no-reactivate-conn")

	value, threshold := 1.0, 1.0
	operator := ">="
	alert := &database.Alert{
		AlertType:      "threshold",
		RuleID:         &ruleID,
		ConnectionID:   connID,
		MetricName:     strPtr("spock_exception_log.recent_count"),
		MetricValue:    &value,
		ThresholdValue: &threshold,
		Operator:       &operator,
		Severity:       "warning",
		Title:          "no_reactivate_same_severity",
		Description:    "test",
		Status:         "acknowledged",
		TriggeredAt:    time.Now(),
	}
	if err := ds.CreateAlert(ctx, alert); err != nil {
		t.Fatalf("create alert: %v", err)
	}
	insertTestAcknowledgment(t, pool, alert.ID, "acknowledge", false)

	rule := &database.AlertRule{
		ID:         ruleID,
		Name:       "no_reactivate_same_severity",
		MetricName: "spock_exception_log.recent_count",
	}

	// Fire the same severity that the alert already has; the alert must
	// stay acknowledged and the acknowledgment row must remain.
	engine.triggerThresholdAlert(ctx, rule, value, threshold, operator, "warning", connID, nil, nil)

	if status := getAlertStatus(t, pool, alert.ID); status != "acknowledged" {
		t.Errorf("alert status = %q, want acknowledged", status)
	}
	if n := countAcknowledgments(t, pool, alert.ID); n != 1 {
		t.Errorf("acknowledgments count = %d, want 1", n)
	}
}

// TestEngine_TriggerThresholdAlert_NilMetricValueDoesNotPanic exercises the
// nil-deref guard added to the formatter. Before the fix the eager evaluation
// of `*existing.MetricValue` in the debugLog call panicked for any alert
// with NULL metric_value, aborting triggerThresholdAlert before the
// reactivation branch could run. The database schema permits NULL on this
// column, so even though the alerter never writes NULL today, a stray row
// from an older migration or hand-inserted record must not crash the
// evaluator. The test inserts such a row and verifies both that the call
// returns without panicking and that the reactivation succeeds.
func TestEngine_TriggerThresholdAlert_NilMetricValueDoesNotPanic(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	ruleID := insertTestAlertRule(t, pool, "nil_metric_value_rule",
		"spock_exception_log.recent_count", ">=", 1.0, "warning")
	connID := insertTestConnection(t, pool, "nil-mv-conn")

	// Insert an alert with NULL metric_value directly; CreateAlert always
	// sets a non-nil pointer so the only way to construct this row is via
	// raw SQL. The alert is acknowledged so the reactivation branch should
	// run.
	var alertID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO alerts (alert_type, rule_id, connection_id, severity,
		    title, description, status, triggered_at)
		VALUES ('threshold', $1, $2, 'warning', 'nil mv', 'desc',
		    'acknowledged', NOW())
		RETURNING id`, ruleID, connID).Scan(&alertID); err != nil {
		t.Fatalf("insert alert: %v", err)
	}
	insertTestAcknowledgment(t, pool, alertID, "acknowledge", false)

	rule := &database.AlertRule{
		ID:         ruleID,
		Name:       "nil_metric_value_rule",
		MetricName: "spock_exception_log.recent_count",
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("triggerThresholdAlert panicked on nil MetricValue: %v", r)
		}
	}()
	engine.triggerThresholdAlert(ctx, rule, 1.0, 1.0, ">=", "critical", connID, nil, nil)

	// The reactivation logic must still run despite the missing metric value.
	if status := getAlertStatus(t, pool, alertID); status != "active" {
		t.Errorf("alert status = %q, want active", status)
	}
	if n := countAcknowledgments(t, pool, alertID); n != 0 {
		t.Errorf("acknowledgments count = %d, want 0", n)
	}
}

// TestEngine_TriggerThresholdAlert_ActiveAlertNotReactivated guards against
// accidentally calling ReactivateAlert when the alert is already active.
// ReactivateAlert is gated server-side on status='acknowledged', but the
// caller still issues the SQL round-trip, so suppressing the call when the
// alert is active keeps the path cheaper and the logs cleaner.
func TestEngine_TriggerThresholdAlert_ActiveAlertNotReactivated(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	ruleID := insertTestAlertRule(t, pool, "active_not_reactivated",
		"spock_exception_log.recent_count", ">=", 1.0, "warning")
	connID := insertTestConnection(t, pool, "active-not-reactivated-conn")

	value, threshold := 1.0, 1.0
	operator := ">="
	alert := &database.Alert{
		AlertType:      "threshold",
		RuleID:         &ruleID,
		ConnectionID:   connID,
		MetricName:     strPtr("spock_exception_log.recent_count"),
		MetricValue:    &value,
		ThresholdValue: &threshold,
		Operator:       &operator,
		Severity:       "warning",
		Title:          "active_not_reactivated",
		Description:    "test",
		Status:         "active",
		TriggeredAt:    time.Now(),
	}
	if err := ds.CreateAlert(ctx, alert); err != nil {
		t.Fatalf("create alert: %v", err)
	}

	rule := &database.AlertRule{
		ID:         ruleID,
		Name:       "active_not_reactivated",
		MetricName: "spock_exception_log.recent_count",
	}

	// Severity changes from warning to critical, but the alert is active —
	// no reactivation is needed; UpdateAlertValues alone writes the new
	// severity.
	engine.triggerThresholdAlert(ctx, rule, value, threshold, operator, "critical", connID, nil, nil)

	if status := getAlertStatus(t, pool, alert.ID); status != "active" {
		t.Errorf("alert status = %q, want active", status)
	}
	var sev string
	if err := pool.QueryRow(ctx, `SELECT severity FROM alerts WHERE id=$1`, alert.ID).Scan(&sev); err != nil {
		t.Fatalf("read severity: %v", err)
	}
	if sev != "critical" {
		t.Errorf("alert severity = %q, want critical", sev)
	}
}

// TestEngine_TriggerThresholdAlert_ClosedPool exercises the error logging
// paths inside triggerThresholdAlert when the alerter's database pool has
// been torn down mid-evaluation. Both UpdateAlertValues and (if
// reactivation is needed) ReactivateAlert must fail safely, surface
// error logs, and the function must return without panicking. This
// covers the error branches that the happy-path tests cannot reach.
func TestEngine_TriggerThresholdAlert_ClosedPool(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	// We close the pool ourselves; let the deferred cleanup tolerate the
	// double-close because it only runs Exec on a closed pool which is a
	// no-op error.
	defer cleanup()

	ctx := context.Background()
	ruleID := insertTestAlertRule(t, pool, "closed_pool_rule",
		"spock_exception_log.recent_count", ">=", 1.0, "warning")
	connID := insertTestConnection(t, pool, "closed-pool-conn")

	value, threshold := 1.0, 1.0
	operator := ">="
	alert := &database.Alert{
		AlertType:      "threshold",
		RuleID:         &ruleID,
		ConnectionID:   connID,
		MetricName:     strPtr("spock_exception_log.recent_count"),
		MetricValue:    &value,
		ThresholdValue: &threshold,
		Operator:       &operator,
		Severity:       "warning",
		Title:          "closed_pool_rule",
		Description:    "test",
		Status:         "acknowledged",
		TriggeredAt:    time.Now(),
	}
	if err := ds.CreateAlert(ctx, alert); err != nil {
		t.Fatalf("create alert: %v", err)
	}
	insertTestAcknowledgment(t, pool, alert.ID, "acknowledge", false)

	rule := &database.AlertRule{
		ID:         ruleID,
		Name:       "closed_pool_rule",
		MetricName: "spock_exception_log.recent_count",
	}

	// Close the pool so all subsequent database calls return errors.
	pool.Close()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("triggerThresholdAlert panicked on closed pool: %v", r)
		}
	}()
	// All three potential database calls (GetActiveThresholdAlert,
	// UpdateAlertValues, ReactivateAlert) will error against the closed
	// pool. The function must still return; we are testing that it
	// reaches and logs every error branch without crashing.
	engine.triggerThresholdAlert(ctx, rule, value, threshold, operator, "critical", connID, nil, nil)
}

// TestEngine_AcknowledgedAlert_AutoReactivatesAfterOverrideAddsSeverity is
// the closest engine-level analog of the user-visible scenario: an alert
// fires at warning, the user acknowledges, and then adds a server-level
// override that bumps the rule's severity to critical. The next
// evaluateThresholds tick must observe the override, write the new
// severity, and transition the alert back to active.
func TestEngine_AcknowledgedAlert_AutoReactivatesAfterOverrideAddsSeverity(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	rules := seedSpockBuiltInRules(t, pool)
	disableAllRulesExcept(t, pool, "spock_recent_exceptions_present")

	connID := insertTestConnection(t, pool, "override-auto-reactivate-conn")

	// Fire the alert at the rule's default warning severity.
	sample := time.Now().UTC().Add(-1 * time.Minute)
	insertSpockExceptionRow(t, pool, connID, sample, 1)
	engine.evaluateThresholds(ctx)
	alert := assertAlertFired(t, ds, rules["spock_recent_exceptions_present"], connID, "warning")

	// Acknowledge it.
	if _, err := pool.Exec(ctx, `UPDATE alerts SET status='acknowledged' WHERE id=$1`, alert.ID); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	insertTestAcknowledgment(t, pool, alert.ID, "acknowledge", false)

	// Add a server-level override that bumps severity to critical.
	if _, err := pool.Exec(ctx, `
		INSERT INTO alert_thresholds (rule_id, scope, connection_id,
		    operator, threshold, severity, enabled)
		VALUES ($1, 'server', $2, '>=', 1, 'critical', TRUE)
	`, rules["spock_recent_exceptions_present"], connID); err != nil {
		t.Fatalf("insert override: %v", err)
	}

	// Re-evaluate; the metric still violates so triggerThresholdAlert runs
	// and observes the bumped severity.
	engine.evaluateThresholds(ctx)

	if status := getAlertStatus(t, pool, alert.ID); status != "active" {
		t.Errorf("alert status = %q, want active", status)
	}
	var sev string
	if err := pool.QueryRow(ctx, `SELECT severity FROM alerts WHERE id=$1`, alert.ID).Scan(&sev); err != nil {
		t.Fatalf("read severity: %v", err)
	}
	if sev != "critical" {
		t.Errorf("alert severity = %q, want critical", sev)
	}
	if n := countAcknowledgments(t, pool, alert.ID); n != 0 {
		t.Errorf("acknowledgments after reactivation = %d, want 0", n)
	}
}

// TestEngine_TriggerThresholdAlert_SkipsReactivationWhenUpdateFails proves
// the bug-2 follow-up fix: when UpdateAlertValues fails, ReactivateAlert
// must not run. Otherwise the database would still hold the previous
// severity while the queued notification advertised the new one, drifting
// the user-visible state away from the persisted row.
//
// The test forces UpdateAlertValues to fail by passing a severity that
// violates the alerts.severity CHECK constraint. ReactivateAlert itself
// would succeed against this row (it only writes status and last_updated,
// neither of which are constrained by the bad input), so any reactivation
// observed after the run came from the buggy unconditional path. The
// assertions therefore prove ReactivateAlert was skipped: status stays
// acknowledged, severity stays warning, and the acknowledgment row stays.
func TestEngine_TriggerThresholdAlert_SkipsReactivationWhenUpdateFails(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	ruleID := insertTestAlertRule(t, pool, "skip_reactivate_on_update_fail",
		"spock_exception_log.recent_count", ">=", 1.0, "warning")
	connID := insertTestConnection(t, pool, "skip-reactivate-update-fail-conn")

	value, threshold := 1.0, 1.0
	operator := ">="
	alert := &database.Alert{
		AlertType:      "threshold",
		RuleID:         &ruleID,
		ConnectionID:   connID,
		MetricName:     strPtr("spock_exception_log.recent_count"),
		MetricValue:    &value,
		ThresholdValue: &threshold,
		Operator:       &operator,
		Severity:       "warning",
		Title:          "skip_reactivate_on_update_fail",
		Description:    "test",
		Status:         "acknowledged",
		TriggeredAt:    time.Now(),
	}
	if err := ds.CreateAlert(ctx, alert); err != nil {
		t.Fatalf("create alert: %v", err)
	}
	insertTestAcknowledgment(t, pool, alert.ID, "acknowledge", false)

	rule := &database.AlertRule{
		ID:         ruleID,
		Name:       "skip_reactivate_on_update_fail",
		MetricName: "spock_exception_log.recent_count",
	}

	// "bogus_severity" differs from "warning" so needsReactivation is true,
	// but it violates the severity CHECK constraint so UpdateAlertValues
	// returns an error. The fix must therefore skip ReactivateAlert.
	engine.triggerThresholdAlert(ctx, rule, value, threshold, operator,
		"bogus_severity", connID, nil, nil)

	if status := getAlertStatus(t, pool, alert.ID); status != "acknowledged" {
		t.Errorf("alert status = %q, want acknowledged (ReactivateAlert ran despite failed UpdateAlertValues)", status)
	}
	var sev string
	if err := pool.QueryRow(ctx, `SELECT severity FROM alerts WHERE id=$1`, alert.ID).Scan(&sev); err != nil {
		t.Fatalf("read severity: %v", err)
	}
	if sev != "warning" {
		t.Errorf("alert severity = %q, want warning (UpdateAlertValues should have failed)", sev)
	}
	if n := countAcknowledgments(t, pool, alert.ID); n != 1 {
		t.Errorf("acknowledgments count = %d, want 1 (ReactivateAlert ran despite failed UpdateAlertValues)", n)
	}
}
