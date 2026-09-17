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
)

// Seed and inspection statements for the unavailable probe tests. They
// are named constants for the same reason as those in
// staleness_alerts_integration_test.go: the Codacy/Semgrep
// go_sql_rule-concat-sqli rule flags inline multi-line SQL, and every
// value here is still bound via $N.
const (
	probeUnavailableUpdateSQL = `
        UPDATE probe_availability
           SET is_available = FALSE, unavailable_reason = $3
         WHERE connection_id = $1 AND probe_name = $2
    `

	alertDescriptionSelectSQL = `
        SELECT title, description, last_updated
        FROM alerts
        WHERE id = $1
    `
)

// markProbeUnavailable flips a seeded probe's availability and records a
// reason, imitating a collector that has lost the extension, the
// privileges or the connection the probe needs.
func markProbeUnavailable(t *testing.T, pool *pgxpool.Pool, connID int, probe, reason string) {
	t.Helper()

	tag, err := pool.Exec(context.Background(), probeUnavailableUpdateSQL, connID, probe, reason)
	if err != nil {
		t.Fatalf("failed to mark probe %s unavailable: %v", probe, err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("marking probe %s unavailable affected %d rows, want 1",
			probe, tag.RowsAffected())
	}
}

// alertNarrative is the part of an alert a held staleness alert is judged
// on: the title must not move, the description must say what happened,
// and last_updated must only advance when the description actually
// changes.
type alertNarrative struct {
	title       string
	description string
	lastUpdated *time.Time
}

// readAlertNarrative reads one alert's title, description and last_updated.
func readAlertNarrative(t *testing.T, pool *pgxpool.Pool, alertID int64) alertNarrative {
	t.Helper()

	var got alertNarrative
	if err := pool.QueryRow(context.Background(), alertDescriptionSelectSQL, alertID).
		Scan(&got.title, &got.description, &got.lastUpdated); err != nil {
		t.Fatalf("failed to read alert %d: %v", alertID, err)
	}
	return got
}

// TestStalenessAlertHeldWhenProbeGoesUnavailable is the regression test
// for issue #465. A probe whose availability goes false has stopped
// collecting through no decision of the operator's, so the staleness
// alert must stay active, its description must say that collection has
// stopped and why, and its title must be left alone.
func TestStalenessAlertHeldWhenProbeGoesUnavailable(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-unavailable")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	engine.evaluateThresholds(ctx)

	alerts := readStalenessAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 || alerts[0].status != "active" {
		t.Fatalf("expected one active alert, got %+v", alerts)
	}
	raised := readAlertNarrative(t, pool, alerts[0].id)

	const reason = "extension pg_stat_statements is not installed"
	markProbeUnavailable(t, pool, connID, "pg_stat_activity", reason)

	engine.cleanResolvedAlerts(ctx)

	alerts = readStalenessAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(alerts))
	}
	if alerts[0].status != "active" {
		t.Errorf("alert status = %q, want \"active\"; an unavailable probe is a "+
			"fault, not a resolution", alerts[0].status)
	}

	held := readAlertNarrative(t, pool, alerts[0].id)
	if held.title != raised.title {
		t.Errorf("alert title = %q, want it unchanged at %q", held.title, raised.title)
	}
	if !strings.Contains(held.description, "Collection has stopped") {
		t.Errorf("alert description = %q, want it to report that collection has stopped",
			held.description)
	}
	if !strings.Contains(held.description, reason) {
		t.Errorf("alert description = %q, want it to carry the unavailable reason %q",
			held.description, reason)
	}
	if counts := capture.drain(t); counts[database.NotificationTypeAlertClear] != 0 {
		t.Errorf("clear notifications = %d, want 0 whilst the probe is unavailable",
			counts[database.NotificationTypeAlertClear])
	}

	// A second pass must not rewrite identical text, because the cleaner
	// runs on every cycle and would otherwise bump last_updated for as
	// long as the probe stayed unavailable.
	engine.cleanResolvedAlerts(ctx)

	again := readAlertNarrative(t, pool, alerts[0].id)
	if again.description != held.description {
		t.Errorf("description on the second pass = %q, want it unchanged", again.description)
	}
	if held.lastUpdated == nil || again.lastUpdated == nil {
		t.Fatalf("last_updated = %v then %v, want both set", held.lastUpdated, again.lastUpdated)
	}
	if !again.lastUpdated.Equal(*held.lastUpdated) {
		t.Errorf("last_updated moved from %s to %s on a pass that changed nothing",
			held.lastUpdated, again.lastUpdated)
	}
}

// TestStalenessAlertHeldWhenDescriptionUpdateFails covers the branch
// where the description rewrite fails. The alert must still be held open,
// because keeping an alert whose wording is out of date is far better
// than clearing one whose probe has stopped collecting.
func TestStalenessAlertHeldWhenDescriptionUpdateFails(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-description-failure")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	engine.evaluateThresholds(ctx)
	alerts := readStalenessAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(alerts))
	}
	markProbeUnavailable(t, pool, connID, "pg_stat_activity", "connection refused")

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

	engine.cleanResolvedAlerts(ctx)

	alerts = readStalenessAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 || alerts[0].status != "active" {
		t.Errorf("alerts = %+v, want one active alert after a failed description update",
			alerts)
	}
}

// TestStalenessEvaluatorSkipsUnavailableProbes pins the deliberate
// asymmetry introduced with issue #465: unavailable probes are in the
// staleness snapshot for the cleaner's benefit, but the evaluator must
// still ignore them, or every probe whose extension is absent would carry
// a permanent staleness alert.
func TestStalenessEvaluatorSkipsUnavailableProbes(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-never-available")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")
	markProbeUnavailable(t, pool, connID, "pg_stat_activity",
		"probe is not supported on this server")

	engine.evaluateThresholds(ctx)

	if alerts := readStalenessAlertsForRule(t, pool, ruleID); len(alerts) != 0 {
		t.Errorf("alert rows = %d, want 0; an unavailable probe must not raise "+
			"a staleness alert", len(alerts))
	}
}

// TestStalenessAlertHeldWithoutAnUnavailableReason covers the collector
// recording no reason alongside is_available = FALSE, where the
// description still has to read sensibly.
func TestStalenessAlertHeldWithoutAnUnavailableReason(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-no-reason")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	engine.evaluateThresholds(ctx)
	alerts := readStalenessAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(alerts))
	}

	if _, err := pool.Exec(ctx, `
        UPDATE probe_availability
           SET is_available = FALSE, unavailable_reason = NULL
         WHERE connection_id = $1 AND probe_name = $2`, connID,
		"pg_stat_activity"); err != nil {
		t.Fatalf("failed to mark the probe unavailable: %v", err)
	}

	engine.cleanResolvedAlerts(ctx)

	alerts = readStalenessAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 || alerts[0].status != "active" {
		t.Fatalf("alerts = %+v, want one active alert", alerts)
	}
	held := readAlertNarrative(t, pool, alerts[0].id)
	if !strings.Contains(held.description, "no reason recorded") {
		t.Errorf("alert description = %q, want the stand-in reason", held.description)
	}
}

// TestUnavailableProbeNarrative exercises the two pure helpers behind the
// held alert's wording without a database, including the empty string the
// collector can leave in unavailable_reason.
func TestUnavailableProbeNarrative(t *testing.T) {
	reason := "permission denied for relation pg_stat_activity"
	empty := ""

	tests := []struct {
		name  string
		entry database.ProbeStaleness
		want  string
	}{
		{
			name: "reason recorded",
			entry: database.ProbeStaleness{
				ProbeName:         "pg_stat_activity",
				ConnectionName:    "node1",
				UnavailableReason: &reason,
			},
			want: reason,
		},
		{
			name: "no reason recorded",
			entry: database.ProbeStaleness{
				ProbeName:      "pg_stat_activity",
				ConnectionName: "node1",
			},
			want: "no reason recorded",
		},
		{
			name: "empty reason recorded",
			entry: database.ProbeStaleness{
				ProbeName:         "pg_stat_activity",
				ConnectionName:    "node1",
				UnavailableReason: &empty,
			},
			want: "no reason recorded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unavailableProbeReason(tt.entry)
			if got != tt.want {
				t.Errorf("unavailableProbeReason = %q, want %q", got, tt.want)
			}

			description := unavailableProbeDescription(tt.entry, got)
			for _, want := range []string{"Collection has stopped", tt.entry.ProbeName,
				tt.entry.ConnectionName, tt.want} {
				if !strings.Contains(description, want) {
					t.Errorf("description %q does not mention %q", description, want)
				}
			}
			if description != unavailableProbeDescription(tt.entry, got) {
				t.Error("unavailableProbeDescription is not deterministic")
			}
		})
	}
}

// captureEngineStderr redirects os.Stderr to a temporary file for the
// duration of fn and returns whatever the engine logged. The engine logs
// through fmt.Fprintf to os.Stderr rather than through an injectable
// writer, so this is the only way to assert on the level a line was
// emitted at. A file is used rather than a pipe so that a long message
// cannot block on an unread buffer.
func captureEngineStderr(t *testing.T, fn func()) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "alerter-stderr-*")
	if err != nil {
		t.Fatalf("failed to create the capture file: %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Logf("failed to close the capture file: %v", err)
		}
	}()

	previous := os.Stderr
	os.Stderr = file
	defer func() { os.Stderr = previous }()

	fn()

	captured, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("failed to read the capture file: %v", err)
	}
	return string(captured)
}

// TestStalenessAlertClearLoggedAtNormalLevel pins the other half of issue
// #465: when the probe leaves the staleness view altogether the alert
// still clears, but the line saying so is emitted at normal level rather
// than debug, so an operator running without debug logging sees which of
// their changes retired the alert.
func TestStalenessAlertClearLoggedAtNormalLevel(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	ruleID := seedStalenessRule(t, pool)
	connID := insertTestConnection(t, pool, "staleness-clear-logged")
	seedStaleProbe(t, pool, connID, "pg_stat_activity", "30 minutes")

	engine.evaluateThresholds(ctx)
	if alerts := readStalenessAlertsForRule(t, pool, ruleID); len(alerts) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(alerts))
	}

	if _, err := pool.Exec(ctx, stalenessProbeAvailabilityDeleteSQL, connID,
		"pg_stat_activity"); err != nil {
		t.Fatalf("failed to delete probe availability: %v", err)
	}

	if engine.debug {
		t.Fatal("the test engine has debug logging on, which would make the assertion meaningless")
	}
	logged := captureEngineStderr(t, func() { engine.cleanResolvedAlerts(ctx) })

	if !strings.Contains(logged, "is no longer reported") {
		t.Errorf("engine log = %q, want the clear reported at normal level", logged)
	}

	alerts := readStalenessAlertsForRule(t, pool, ruleID)
	if len(alerts) != 1 || alerts[0].status != "cleared" {
		t.Errorf("alerts = %+v, want one cleared alert", alerts)
	}
}
