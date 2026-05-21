/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// timelineAlertClearedSchema mirrors the minimum subset of tables that
// the buildAlertClearedQuery subquery touches. We intentionally keep
// this independent from the schema in the tools-package integration
// test so a future change to that file cannot silently break us.
const timelineAlertClearedSchema = `
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE alert_rules (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    metric_unit VARCHAR(64)
);

CREATE TABLE alerts (
    id SERIAL PRIMARY KEY,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    rule_id INTEGER,
    alert_type VARCHAR(64) NOT NULL DEFAULT 'threshold',
    severity VARCHAR(32) NOT NULL DEFAULT 'warning',
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    metric_name VARCHAR(255),
    metric_value DOUBLE PRECISION,
    threshold_value DOUBLE PRECISION,
    operator VARCHAR(8),
    database_name VARCHAR(255),
    probe_name VARCHAR(255),
    triggered_at TIMESTAMPTZ NOT NULL,
    cleared_at TIMESTAMPTZ,
    status VARCHAR(32) NOT NULL DEFAULT 'active'
);
`

const timelineAlertClearedTeardown = `
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
`

// newAlertClearedTestEnv brings up the minimum Postgres state needed to
// drive the alert_cleared subquery end-to-end. It skips when no test DB
// is configured so unit-test runs that lack TEST_AI_WORKBENCH_SERVER
// still pass.
func newAlertClearedTestEnv(t *testing.T) (*pgxpool.Pool, *Datastore, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping alert_cleared integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}
	if _, err := pool.Exec(ctx, timelineAlertClearedSchema); err != nil {
		pool.Close()
		t.Fatalf("create schema: %v", err)
	}

	ds := NewTestDatastore(pool)
	cleanup := func() {
		if _, err := pool.Exec(context.Background(), timelineAlertClearedTeardown); err != nil {
			t.Logf("teardown: %v", err)
		}
		pool.Close()
	}
	return pool, ds, cleanup
}

// insertClearedAlert seeds a fired-then-cleared alert with the given
// description and elapsed duration between triggered_at and cleared_at.
// It returns the cleared_at time anchor so callers can build a time
// window that includes the resulting row.
func insertClearedAlert(t *testing.T, pool *pgxpool.Pool, connID int, description string, elapsed time.Duration) time.Time {
	t.Helper()
	cleared := time.Now().UTC().Add(-30 * time.Minute)
	triggered := cleared.Add(-elapsed)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO alerts (
            connection_id, severity, title, description, triggered_at, cleared_at, status
        ) VALUES ($1, 'warning', 'Staleness', $2, $3, $4, 'cleared')`,
		connID, description, triggered, cleared,
	); err != nil {
		t.Fatalf("insert cleared alert: %v", err)
	}
	return cleared
}

// fetchClearedSummary runs GetTimelineEvents with the alert_cleared
// filter and returns the summary string of the single expected row.
// It fails the test if the row count is not exactly one.
func fetchClearedSummary(t *testing.T, ds *Datastore, connID int, cleared time.Time) string {
	t.Helper()
	cid := connID
	result, err := ds.GetTimelineEvents(context.Background(), TimelineFilter{
		ConnectionID: &cid,
		StartTime:    cleared.Add(-2 * time.Hour),
		EndTime:      cleared.Add(time.Hour),
		EventTypes:   []string{EventTypeAlertCleared},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("GetTimelineEvents: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected exactly one cleared row, got %d", len(result.Events))
	}
	if result.Events[0].EventType != EventTypeAlertCleared {
		t.Fatalf("expected alert_cleared event, got %q", result.Events[0].EventType)
	}
	return result.Events[0].Summary
}

// TestAlertClearedSummaryShortDuration verifies the < 60s branch of
// the SQL CASE expression. The Ellie regression involved a 27-second
// gap between triggered_at and cleared_at, so the short branch is the
// most important one to cover.
func TestAlertClearedSummaryShortDuration(t *testing.T) {
	pool, ds, cleanup := newAlertClearedTestEnv(t)
	defer cleanup()

	var connID int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name) VALUES ('short') RETURNING id`,
	).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	desc := "has not collected data for 154 minutes (threshold: 15 minutes)"
	cleared := insertClearedAlert(t, pool, connID, desc, 27*time.Second)
	summary := fetchClearedSummary(t, ds, connID, cleared)

	want := fmt.Sprintf("Resolved after 27s. Fired: %s", desc)
	if summary != want {
		t.Errorf("cleared summary mismatch\n got: %q\nwant: %q", summary, want)
	}
}

// TestAlertClearedSummaryMediumDuration verifies the < 1h branch.
func TestAlertClearedSummaryMediumDuration(t *testing.T) {
	pool, ds, cleanup := newAlertClearedTestEnv(t)
	defer cleanup()

	var connID int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name) VALUES ('medium') RETURNING id`,
	).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	desc := "replication lag exceeded threshold"
	cleared := insertClearedAlert(t, pool, connID, desc, 2*time.Minute+15*time.Second)
	summary := fetchClearedSummary(t, ds, connID, cleared)

	want := fmt.Sprintf("Resolved after 2m 15s. Fired: %s", desc)
	if summary != want {
		t.Errorf("cleared summary mismatch\n got: %q\nwant: %q", summary, want)
	}
}

// TestAlertClearedSummaryLongDuration verifies the >= 1h branch.
func TestAlertClearedSummaryLongDuration(t *testing.T) {
	pool, ds, cleanup := newAlertClearedTestEnv(t)
	defer cleanup()

	var connID int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name) VALUES ('long') RETURNING id`,
	).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	desc := "CPU saturation"
	cleared := insertClearedAlert(t, pool, connID, desc, time.Hour+23*time.Minute+5*time.Second)
	summary := fetchClearedSummary(t, ds, connID, cleared)

	want := fmt.Sprintf("Resolved after 1h 23m. Fired: %s", desc)
	if summary != want {
		t.Errorf("cleared summary mismatch\n got: %q\nwant: %q", summary, want)
	}
}

// TestAlertClearedSummaryNegativeClampedToZero exercises the
// GREATEST(...,0) guard against clock skew or out-of-order timestamps.
// When cleared_at < triggered_at the summary must report "0s" rather
// than a negative or oddly formatted duration.
func TestAlertClearedSummaryNegativeClampedToZero(t *testing.T) {
	pool, ds, cleanup := newAlertClearedTestEnv(t)
	defer cleanup()

	var connID int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name) VALUES ('skew') RETURNING id`,
	).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	// Insert an alert with cleared_at deliberately earlier than
	// triggered_at to simulate clock skew between collector and server.
	cleared := time.Now().UTC().Add(-30 * time.Minute)
	triggered := cleared.Add(5 * time.Second) // 5s after cleared_at
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO alerts (
            connection_id, severity, title, description, triggered_at, cleared_at, status
        ) VALUES ($1, 'warning', 'Staleness', 'clock skew probe', $2, $3, 'cleared')`,
		connID, triggered, cleared,
	); err != nil {
		t.Fatalf("insert cleared alert: %v", err)
	}

	summary := fetchClearedSummary(t, ds, connID, cleared)
	if !strings.HasPrefix(summary, "Resolved after 0s. Fired: ") {
		t.Errorf("expected negative duration to clamp to 0s, got: %q", summary)
	}
}

// TestAlertFiredSummaryPreservesDescription confirms the alert_fired
// summary continues to be a.description verbatim. The Ellie investigation
// established that the fired-row summary is the immutable historical
// record and must not be rewritten alongside the cleared-row enrichment.
func TestAlertFiredSummaryPreservesDescription(t *testing.T) {
	pool, ds, cleanup := newAlertClearedTestEnv(t)
	defer cleanup()

	var connID int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name) VALUES ('fired-only') RETURNING id`,
	).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	desc := "exact text that must survive verbatim"
	triggered := time.Now().UTC().Add(-30 * time.Minute)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO alerts (
            connection_id, severity, title, description, triggered_at, status
        ) VALUES ($1, 'warning', 'Probe', $2, $3, 'active')`,
		connID, desc, triggered,
	); err != nil {
		t.Fatalf("insert fired alert: %v", err)
	}

	cid := connID
	result, err := ds.GetTimelineEvents(context.Background(), TimelineFilter{
		ConnectionID: &cid,
		StartTime:    triggered.Add(-time.Hour),
		EndTime:      triggered.Add(time.Hour),
		EventTypes:   []string{EventTypeAlertFired},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("GetTimelineEvents: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected exactly one fired row, got %d", len(result.Events))
	}
	if result.Events[0].Summary != desc {
		t.Errorf("fired summary mismatch\n got: %q\nwant: %q", result.Events[0].Summary, desc)
	}
}
