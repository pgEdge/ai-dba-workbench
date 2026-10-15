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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
// alone. See GitHub issue #679.
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

	alertIDs := map[string]int64{}
	for name, a := range map[string]struct {
		connID int
		status string
	}{
		"acknowledged": {ackID, "acknowledged"},
		"blackout":     {blackoutID, "active"},
		"cleared":      {ackID, "cleared"},
	} {
		var id int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO alerts (alert_type, connection_id, severity, title, description, status, triggered_at)
			VALUES ('connection', $1, 'critical', 'Connection error', $2, $3, NOW()) RETURNING id`,
			a.connID, raw, a.status).Scan(&id); err != nil {
			t.Fatalf("failed to insert %s alert: %v", name, err)
		}
		alertIDs[name] = id
	}

	engine.evaluateConnectionErrors(ctx)
	for name, want := range map[string]string{"acknowledged": masked, "blackout": masked, "cleared": raw} {
		var got string
		if err := pool.QueryRow(ctx, `SELECT description FROM alerts WHERE id = $1`,
			alertIDs[name]).Scan(&got); err != nil {
			t.Fatalf("failed to read %s alert: %v", name, err)
		}
		if got != want {
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
	logged := captureStderr(t, func() { engine.maskOpenConnectionAlerts(ctx) })
	if _, err := pool.Exec(ctx, `
		DROP TRIGGER refuse_alert_update ON alerts;
		DROP FUNCTION refuse_alert_update()`); err != nil {
		t.Fatalf("failed to drop the refusing trigger: %v", err)
	}
	if !strings.Contains(logged, "Failed to mask connection alert") {
		t.Errorf("a refused rewrite should be logged: %q", logged)
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
