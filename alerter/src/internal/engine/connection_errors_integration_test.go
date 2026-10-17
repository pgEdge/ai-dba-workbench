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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/redact"
)

// connectionAlert is the active connection alert for one connection, as
// the tests below read it back.
type connectionAlert struct {
	id          int64
	description string
}

// activeConnectionAlerts returns the active connection alerts for connID.
func activeConnectionAlerts(t *testing.T, pool *pgxpool.Pool, connID int) []connectionAlert {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT id, description FROM alerts
		WHERE alert_type = 'connection' AND connection_id = $1
		  AND status = 'active'
		ORDER BY id`, connID)
	if err != nil {
		t.Fatalf("failed to read connection alerts: %v", err)
	}
	defer rows.Close()
	var alerts []connectionAlert
	for rows.Next() {
		var a connectionAlert
		if err := rows.Scan(&a.id, &a.description); err != nil {
			t.Fatalf("failed to scan connection alert: %v", err)
		}
		alerts = append(alerts, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to read connection alerts: %v", err)
	}
	return alerts
}

// insertMonitoredConnection adds a monitored connection with the given
// error text (nil for a healthy connection) and returns its id.
func insertMonitoredConnection(t *testing.T, pool *pgxpool.Pool, name string, connErr *string) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO connections (name, enabled, is_monitored, connection_error)
		VALUES ($1, TRUE, TRUE, $2) RETURNING id`, name, connErr).Scan(&id); err != nil {
		t.Fatalf("failed to insert connection %q: %v", name, err)
	}
	return id
}

// setConnectionError sets or (with nil) removes a connection's error.
func setConnectionError(t *testing.T, pool *pgxpool.Pool, connID int, connErr *string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE connections SET connection_error = $2 WHERE id = $1`,
		connID, connErr); err != nil {
		t.Fatalf("failed to set connection error: %v", err)
	}
}

// TestEvaluateConnectionErrors_Lifecycle covers a connection error alert
// from raise to clear: an error raises one alert, re-evaluating the same
// error leaves it alone, a changed error rewrites its description, and a
// healthy connection clears it. Connection errors are not subject to
// threshold.trigger_count, so the engine is given counts above 1 to show
// that the alert is still raised on the first pass.
func TestEvaluateConnectionErrors_Lifecycle(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()
	setThresholdCounts(engine, 3, 3)
	ctx := context.Background()

	refused := "connection refused"
	connID := insertMonitoredConnection(t, pool, "conn-error-lifecycle", &refused)
	healthyID := insertMonitoredConnection(t, pool, "conn-error-healthy", nil)

	engine.evaluateConnectionErrors(ctx)
	alerts := activeConnectionAlerts(t, pool, connID)
	if len(alerts) != 1 || alerts[0].description != refused {
		t.Fatalf("after first pass: alerts = %+v, want one reading %q", alerts, refused)
	}
	if got := activeConnectionAlerts(t, pool, healthyID); len(got) != 0 {
		t.Errorf("healthy connection has alerts %+v", got)
	}
	alertID := alerts[0].id

	engine.evaluateConnectionErrors(ctx)
	if again := activeConnectionAlerts(t, pool, connID); len(again) != 1 || again[0].id != alertID {
		t.Fatalf("after unchanged pass: alerts = %+v, want only alert %d", again, alertID)
	}

	timeout := "timeout expired"
	setConnectionError(t, pool, connID, &timeout)
	engine.evaluateConnectionErrors(ctx)
	updated := activeConnectionAlerts(t, pool, connID)
	if len(updated) != 1 || updated[0].id != alertID || updated[0].description != timeout {
		t.Fatalf("after changed error: alerts = %+v, want alert %d reading %q",
			updated, alertID, timeout)
	}

	setConnectionError(t, pool, connID, nil)
	engine.evaluateConnectionErrors(ctx)
	if left := activeConnectionAlerts(t, pool, connID); len(left) != 0 {
		t.Fatalf("after recovery: alerts = %+v, want none", left)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM alerts WHERE id = $1`,
		alertID).Scan(&status); err != nil {
		t.Fatalf("failed to read alert %d: %v", alertID, err)
	}
	if status != "cleared" {
		t.Errorf("alert %d status = %q, want cleared", alertID, status)
	}
}

// TestEvaluateConnectionErrors_MasksCredentials checks that a password in
// a connection error never reaches the alert: a new alert is raised with
// the masked text, and an active alert that still holds the raw text is
// rewritten with it. See GitHub issue #679.
func TestEvaluateConnectionErrors_MasksCredentials(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()
	ctx := context.Background()

	raw := `could not connect to postgresql://app:example-secret@192.0.2.1/db: ` +
		`invalid connection option in "host=192.0.2.1 password=example-secret dbname=db"`
	masked := `could not connect to postgresql://app:********@192.0.2.1/db: ` +
		`invalid connection option in "host=192.0.2.1 password=******** dbname=db"`
	connID := insertMonitoredConnection(t, pool, "conn-error-masked", &raw)

	logged := captureStderr(t, func() { engine.evaluateConnectionErrors(ctx) })
	alerts := activeConnectionAlerts(t, pool, connID)
	if len(alerts) != 1 || alerts[0].description != masked {
		t.Fatalf("after first pass: alerts = %+v, want one reading %q", alerts, masked)
	}
	if strings.Contains(logged, "example-secret") || !strings.Contains(logged, masked) {
		t.Errorf("the alert log line should hold only the masked error: %q", logged)
	}

	// An alert raised before the masking holds the raw text.
	if _, err := pool.Exec(ctx, `UPDATE alerts SET description = $2 WHERE id = $1`,
		alerts[0].id, raw); err != nil {
		t.Fatalf("failed to store the raw description: %v", err)
	}
	engine.evaluateConnectionErrors(ctx)
	rewritten := activeConnectionAlerts(t, pool, connID)
	if len(rewritten) != 1 || rewritten[0].id != alerts[0].id || rewritten[0].description != masked {
		t.Fatalf("after rewrite: alerts = %+v, want alert %d reading %q",
			rewritten, alerts[0].id, masked)
	}
}

// TestEvaluateConnectionErrors_MasksOpenAlerts checks that an
// acknowledged alert, and an alert on a connection in a blackout, that
// still hold the raw text are masked too, whilst a cleared alert is left
// alone, and that a description longer than redact.MaxTextLength keeps
// its end. See GitHub issue #679.
func TestEvaluateConnectionErrors_MasksOpenAlerts(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()
	ctx := context.Background()

	raw := `could not connect: password=example-secret dbname=db`
	masked := `could not connect: password=******** dbname=db`
	ackID := insertMonitoredConnection(t, pool, "conn-error-acknowledged", &raw)
	blackoutID := insertMonitoredConnection(t, pool, "conn-error-blacked-out", &raw)
	now := time.Now()
	if _, err := pool.Exec(ctx, `
		INSERT INTO blackouts (scope, connection_id, start_time, end_time, reason)
		VALUES ('server', $1, $2, $3, 'maintenance')`,
		blackoutID, now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatalf("failed to insert blackout: %v", err)
	}

	tail := strings.Repeat("é", redact.MaxTextLength)
	alertIDs := map[string]int64{}
	for name, a := range map[string]struct {
		connID      int
		status      string
		description string
	}{
		"acknowledged": {ackID, "acknowledged", raw},
		"blackout":     {blackoutID, "active", raw},
		"cleared":      {ackID, "cleared", raw},
		"long":         {blackoutID, "acknowledged", raw + tail + raw},
		"oversized":    {blackoutID, "acknowledged", raw + strings.Repeat("x", redact.MaxRewriteLength)},
	} {
		var id int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO alerts (alert_type, connection_id, severity, title, description, status, triggered_at)
			VALUES ('connection', $1, 'critical', 'Connection error', $2, $3, NOW()) RETURNING id`,
			a.connID, a.description, a.status).Scan(&id); err != nil {
			t.Fatalf("failed to insert %s alert: %v", name, err)
		}
		alertIDs[name] = id
	}

	engine.evaluateConnectionErrors(ctx)
	for name, want := range map[string]string{
		"acknowledged": masked, "blackout": masked, "cleared": raw, "long": masked + tail + masked,
		"oversized": redact.RedactedValue,
	} {
		var got string
		if err := pool.QueryRow(ctx, `SELECT description FROM alerts WHERE id = $1`,
			alertIDs[name]).Scan(&got); err != nil {
			t.Fatalf("failed to read %s alert: %v", name, err)
		}
		if got != want {
			if name == "long" {
				t.Errorf("long alert: description has %d characters, want %d with both passwords masked",
					len([]rune(got)), len([]rune(want)))
				continue
			}
			t.Errorf("%s alert: description = %q, want %q", name, got, want)
		}
	}

	// An alert that cannot be rewritten is logged and left for the next
	// pass.
	if _, err := pool.Exec(ctx, `UPDATE alerts SET description = $2 WHERE id = $1`,
		alertIDs["acknowledged"], raw); err != nil {
		t.Fatalf("failed to store the raw description: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION refuse_alert_update() RETURNS trigger LANGUAGE plpgsql AS
		$$BEGIN RAISE EXCEPTION 'refused'; END$$;
		CREATE TRIGGER refuse_alert_update BEFORE UPDATE ON alerts
		FOR EACH ROW EXECUTE FUNCTION refuse_alert_update()`); err != nil {
		t.Fatalf("failed to add the refusing trigger: %v", err)
	}
	logged := captureStderr(t, func() { engine.maskOpenConnectionAlerts(ctx, nil) })
	if _, err := pool.Exec(ctx, `
		DROP TRIGGER refuse_alert_update ON alerts;
		DROP FUNCTION refuse_alert_update()`); err != nil {
		t.Fatalf("failed to drop the refusing trigger: %v", err)
	}
	if !strings.Contains(logged, "Failed to mask connection alert") {
		t.Errorf("a refused rewrite should be logged: %q", logged)
	}
}

// TestMaskOpenConnectionAlerts_LogsUnsettled checks a description whose
// masking has not settled is logged, and still written. The masker is
// replaced with one that never settles, so the test does not depend on
// which inputs redact.SettledUncut leaves unsettled.
func TestMaskOpenConnectionAlerts_LogsUnsettled(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()
	ctx := context.Background()

	raw := `could not connect: password=example-secret dbname=db`
	masked := `could not connect: password=******** dbname=db`
	connID := insertMonitoredConnection(t, pool, "conn-error-unsettled", &raw)
	var id int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status, triggered_at)
		VALUES ('connection', $1, 'critical', 'Connection error', $2, 'acknowledged', NOW()) RETURNING id`,
		connID, raw).Scan(&id); err != nil {
		t.Fatalf("failed to insert alert: %v", err)
	}

	saved := maskStoredText
	t.Cleanup(func() { maskStoredText = saved })
	maskStoredText = func(string) (string, bool) { return masked, false }
	logged := captureStderr(t, func() { engine.maskOpenConnectionAlerts(ctx, nil) })
	if want := fmt.Sprintf("Masking connection alert %d did not settle", id); !strings.Contains(logged, want) {
		t.Errorf("an unsettled description should be logged: %q", logged)
	}
	var got string
	if err := pool.QueryRow(ctx, `SELECT description FROM alerts WHERE id = $1`, id).Scan(&got); err != nil ||
		got != masked {
		t.Errorf("an unsettled description should still be written: %q (%v)", got, err)
	}
}

// TestEvaluateConnectionErrors_MasksOnce checks that a connection error
// in a form redact.Text masks further on a second pass is written once
// and then left alone, whether the alert is the loop's (active) or the
// masking pass's (acknowledged, or in a blackout), so that neither stage
// rewrites, and bumps last_updated on, the same alert on every pass.
func TestEvaluateConnectionErrors_MasksOnce(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()
	ctx := context.Background()

	raw := `could not connect: CONNECTION E'host=192.0.2.1 password=\'example-secret a b\'' PUBLICATION p`
	if once := redact.Text(raw); redact.Text(once) == once {
		t.Fatalf("redact.Text is idempotent on %q, so this test proves nothing", raw)
	}
	activeID := insertMonitoredConnection(t, pool, "conn-error-once-active", &raw)
	// A connection still in error would also get a new active alert, so
	// the acknowledged alert's connection has recovered.
	ackID := insertMonitoredConnection(t, pool, "conn-error-once-acknowledged", nil)
	blackoutID := insertMonitoredConnection(t, pool, "conn-error-once-blackout", &raw)
	now := time.Now()
	if _, err := pool.Exec(ctx, `
		INSERT INTO blackouts (scope, connection_id, start_time, end_time, reason)
		VALUES ('server', $1, $2, $3, 'maintenance')`,
		blackoutID, now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatalf("failed to insert blackout: %v", err)
	}
	for connID, status := range map[int]string{ackID: "acknowledged", blackoutID: "active"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO alerts (alert_type, connection_id, severity, title, description, status, triggered_at)
			VALUES ('connection', $1, 'critical', 'Connection error', $2, $3, NOW())`,
			connID, raw, status); err != nil {
			t.Fatalf("failed to insert %s alert: %v", status, err)
		}
	}

	// versions returns each open connection alert's row version, which
	// any UPDATE changes, and fails if a description still holds the
	// secret.
	versions := func() map[int64]string {
		t.Helper()
		rows, err := pool.Query(ctx, `
			SELECT id, xmin::text, description FROM alerts
			WHERE alert_type = 'connection' AND status IN ('active', 'acknowledged')`)
		if err != nil {
			t.Fatalf("failed to read alerts: %v", err)
		}
		defer rows.Close()
		got := map[int64]string{}
		for rows.Next() {
			var id int64
			var xmin, desc string
			if err := rows.Scan(&id, &xmin, &desc); err != nil {
				t.Fatalf("failed to scan alert: %v", err)
			}
			if strings.Contains(desc, "example-secret") {
				t.Errorf("alert %d still holds the secret: %q", id, desc)
			}
			got[id] = xmin
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("failed to read alerts: %v", err)
		}
		return got
	}

	engine.evaluateConnectionErrors(ctx)
	first := versions()
	if len(first) != 3 || len(activeConnectionAlerts(t, pool, activeID)) != 1 {
		t.Fatalf("after the first pass: %d open alerts, want 3", len(first))
	}
	for pass := 2; pass <= 3; pass++ {
		engine.evaluateConnectionErrors(ctx)
		for id, xmin := range versions() {
			if xmin != first[id] {
				t.Errorf("pass %d rewrote alert %d", pass, id)
			}
		}
	}
}

// TestEvaluateConnectionErrors_Blackout checks that a connection in a
// blackout raises no connection error alert.
func TestEvaluateConnectionErrors_Blackout(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()
	ctx := context.Background()

	refused := "connection refused"
	connID := insertMonitoredConnection(t, pool, "conn-error-blackout", &refused)
	now := time.Now()
	if _, err := pool.Exec(ctx, `
		INSERT INTO blackouts (scope, connection_id, start_time, end_time, reason)
		VALUES ('server', $1, $2, $3, 'maintenance')`,
		connID, now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatalf("failed to insert blackout: %v", err)
	}

	engine.evaluateConnectionErrors(ctx)
	if got := activeConnectionAlerts(t, pool, connID); len(got) != 0 {
		t.Errorf("blacked-out connection has alerts %+v", got)
	}
}

// TestEvaluateConnectionErrors_DatastoreFailures checks that the
// evaluator gives up quietly, raising and clearing nothing, when the
// connections or the alerts cannot be read, and that it stops on a
// context that is already done.
func TestEvaluateConnectionErrors_DatastoreFailures(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()
	ctx := context.Background()

	refused := "connection refused"
	connID := insertMonitoredConnection(t, pool, "conn-error-failures", &refused)
	insertMonitoredConnection(t, pool, "conn-error-failures-healthy", nil)

	done, cancel := context.WithCancel(ctx)
	cancel()
	engine.evaluateConnectionErrors(done)
	if got := activeConnectionAlerts(t, pool, connID); len(got) != 0 {
		t.Fatalf("pass on a done context raised alerts %+v", got)
	}

	// With the alerts table out of the way, neither the erroring nor the
	// healthy connection can look up its alert, and both are skipped.
	if _, err := pool.Exec(ctx, `ALTER TABLE alerts RENAME TO alerts_hidden`); err != nil {
		t.Fatalf("failed to hide alerts: %v", err)
	}
	engine.evaluateConnectionErrors(ctx)
	if _, err := pool.Exec(ctx, `ALTER TABLE alerts_hidden RENAME TO alerts`); err != nil {
		t.Fatalf("failed to restore alerts: %v", err)
	}
	if got := activeConnectionAlerts(t, pool, connID); len(got) != 0 {
		t.Fatalf("pass without an alerts table raised alerts %+v", got)
	}

	// Without the connection_error column the connections cannot be read
	// at all.
	if _, err := pool.Exec(ctx,
		`ALTER TABLE connections RENAME COLUMN connection_error TO connection_error_hidden`); err != nil {
		t.Fatalf("failed to hide connection_error: %v", err)
	}
	engine.evaluateConnectionErrors(ctx)
	if got := activeConnectionAlerts(t, pool, connID); len(got) != 0 {
		t.Errorf("pass without connection errors raised alerts %+v", got)
	}
}

// refuseAlertWrites makes every insert into or update of alerts fail
// with "refused" until the returned function is called.
func refuseAlertWrites(t *testing.T, pool *pgxpool.Pool) func() {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION refuse_alert_write() RETURNS trigger LANGUAGE plpgsql AS
		$$BEGIN RAISE EXCEPTION 'refused'; END$$;
		DROP TRIGGER IF EXISTS refuse_alert_write ON alerts;
		CREATE TRIGGER refuse_alert_write BEFORE INSERT OR UPDATE ON alerts
		FOR EACH ROW EXECUTE FUNCTION refuse_alert_write()`); err != nil {
		t.Fatalf("failed to add the refusing trigger: %v", err)
	}
	return func() {
		if _, err := pool.Exec(ctx, `
			DROP TRIGGER refuse_alert_write ON alerts;
			DROP FUNCTION refuse_alert_write()`); err != nil {
			t.Fatalf("failed to drop the refusing trigger: %v", err)
		}
	}
}

// TestEvaluateConnectionErrors_WriteFailures checks that the evaluator
// logs and moves on when the blackouts cannot be read or an alert
// cannot be raised, rewritten or cleared.
func TestEvaluateConnectionErrors_WriteFailures(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()
	ctx := context.Background()

	refused := "connection refused"
	connID := insertMonitoredConnection(t, pool, "conn-error-writes", &refused)

	// captureStderr holds its lock until the test ends, so each pass
	// that is captured runs as its own subtest.
	refusedPass := func(name string) string {
		var logged string
		t.Run(name, func(t *testing.T) {
			restore := refuseAlertWrites(t, pool)
			defer restore()
			logged = captureStderr(t, func() { engine.evaluateConnectionErrors(ctx) })
		})
		return logged
	}

	// Without a blackouts table the check fails and is treated as no
	// blackout, so the alert is still raised.
	if _, err := pool.Exec(ctx, `ALTER TABLE blackouts RENAME TO blackouts_hidden`); err != nil {
		t.Fatalf("failed to hide blackouts: %v", err)
	}
	engine.evaluateConnectionErrors(ctx)
	if _, err := pool.Exec(ctx, `ALTER TABLE blackouts_hidden RENAME TO blackouts`); err != nil {
		t.Fatalf("failed to restore blackouts: %v", err)
	}
	alerts := activeConnectionAlerts(t, pool, connID)
	if len(alerts) != 1 {
		t.Fatalf("pass without a blackouts table: alerts %+v, want one", alerts)
	}

	// A changed error that cannot be written leaves the old description.
	timedOut := "connection timed out"
	setConnectionError(t, pool, connID, &timedOut)
	logged := refusedPass("rewrite")
	if !strings.Contains(logged, "Failed to update connection alert description") {
		t.Errorf("a refused rewrite should be logged: %q", logged)
	}
	if got := activeConnectionAlerts(t, pool, connID); len(got) != 1 || got[0].description != refused {
		t.Errorf("after a refused rewrite: alerts %+v, want %q", got, refused)
	}

	// A healthy connection whose alert cannot be cleared keeps it.
	setConnectionError(t, pool, connID, nil)
	logged = refusedPass("clear")
	if !strings.Contains(logged, "Failed to clear connection alert") {
		t.Errorf("a refused clear should be logged: %q", logged)
	}
	if got := activeConnectionAlerts(t, pool, connID); len(got) != 1 {
		t.Errorf("after a refused clear: alerts %+v, want one", got)
	}

	// A new error whose alert cannot be raised raises nothing.
	otherID := insertMonitoredConnection(t, pool, "conn-error-writes-new", &refused)
	logged = refusedPass("raise")
	if !strings.Contains(logged, "Failed to create connection alert") {
		t.Errorf("a refused raise should be logged: %q", logged)
	}
	if got := activeConnectionAlerts(t, pool, otherID); len(got) != 0 {
		t.Errorf("after a refused raise: alerts %+v, want none", got)
	}
}
