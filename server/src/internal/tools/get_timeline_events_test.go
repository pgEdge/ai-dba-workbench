/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"golang.org/x/crypto/bcrypt"
)

// ---------------------------------------------------------------------------
// get_timeline_events tool definition tests
// ---------------------------------------------------------------------------

func TestGetTimelineEventsToolDefinition(t *testing.T) {
	tool := GetTimelineEventsTool(nil, nil, nil)

	if tool.Definition.Name != "get_timeline_events" {
		t.Errorf("expected tool name 'get_timeline_events', got %q", tool.Definition.Name)
	}
	if tool.Definition.Description == "" {
		t.Error("expected non-empty description")
	}
	if tool.Definition.CompactDescription == "" {
		t.Error("expected non-empty compact description")
	}
	if len(tool.Definition.InputSchema.Required) != 0 {
		t.Errorf("expected 0 required parameters, got %d", len(tool.Definition.InputSchema.Required))
	}
	for _, prop := range []string{"connection_id", "start_time", "end_time", "event_types", "limit"} {
		if _, ok := tool.Definition.InputSchema.Properties[prop]; !ok {
			t.Errorf("expected property %q in input schema", prop)
		}
	}

	limitProp, ok := tool.Definition.InputSchema.Properties["limit"].(map[string]any)
	if !ok {
		t.Fatal("expected 'limit' property to be a map")
	}
	if limitProp["default"] != timelineDefaultLimit {
		t.Errorf("expected limit default %d, got %v", timelineDefaultLimit, limitProp["default"])
	}
	if limitProp["maximum"] != timelineMaxLimit {
		t.Errorf("expected limit maximum %d, got %v", timelineMaxLimit, limitProp["maximum"])
	}
}

// ---------------------------------------------------------------------------
// get_timeline_events nil datastore test
// ---------------------------------------------------------------------------

func TestGetTimelineEventsNilDatastore(t *testing.T) {
	tool := GetTimelineEventsTool(nil, nil, nil)

	response, err := tool.Handler(map[string]any{})
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected error response when datastore is nil")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "Datastore not configured") {
		t.Errorf("expected 'Datastore not configured' error, got: %s", response.Content[0].Text)
	}
}

// ---------------------------------------------------------------------------
// formatTimelineEvents output shape
// ---------------------------------------------------------------------------

func TestFormatTimelineEventsHappyPath(t *testing.T) {
	occurred := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	detailJSON := json.RawMessage(`{"change_count":2}`)
	result := &database.TimelineResult{
		Events: []database.TimelineEvent{
			{
				ID:           "config-7-2026-05-01T12:00:00Z",
				EventType:    database.EventTypeConfigChange,
				ConnectionID: 7,
				ServerName:   "prod-1",
				OccurredAt:   occurred,
				Severity:     "info",
				Title:        "Configuration Changed",
				Summary:      "Changed 2 settings",
				Details:      detailJSON,
			},
			{
				ID:           "restart-7-2026-05-01T13:00:00Z",
				EventType:    database.EventTypeRestart,
				ConnectionID: 7,
				ServerName:   "prod-1",
				OccurredAt:   occurred.Add(time.Hour),
				Severity:     "warning",
				Title:        "Server Restart Detected",
				Summary:      "Server started",
			},
		},
		TotalCount: 2,
	}

	start := occurred.Add(-24 * time.Hour)
	end := occurred.Add(2 * time.Hour)
	out := formatTimelineEvents(result, true, 7, "prod-1", start, end, 100)

	if !strings.Contains(out, "Timeline Events | Connection: 7 (prod-1)") {
		t.Errorf("expected single-connection header, got: %s", out)
	}
	if !strings.Contains(out, "connection_id\tserver_name\tevent_time\tevent_type\tseverity\ttitle\tsummary\tid\n") {
		t.Errorf("expected TSV header row, got: %s", out)
	}
	if !strings.Contains(out, database.EventTypeConfigChange) {
		t.Errorf("expected config_change row, got: %s", out)
	}
	if !strings.Contains(out, database.EventTypeRestart) {
		t.Errorf("expected restart row, got: %s", out)
	}
	if !strings.Contains(out, "(2 rows, 2 total in window)") {
		t.Errorf("expected row count footer, got: %s", out)
	}
}

func TestFormatTimelineEventsAllConnectionsHeader(t *testing.T) {
	result := &database.TimelineResult{Events: []database.TimelineEvent{}, TotalCount: 0}
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	out := formatTimelineEvents(result, false, 0, "", start, end, 100)
	if !strings.Contains(out, "No timeline events found across accessible connections") {
		t.Errorf("expected multi-connection empty message, got: %s", out)
	}
}

func TestFormatTimelineEventsSingleConnectionEmpty(t *testing.T) {
	result := &database.TimelineResult{Events: []database.TimelineEvent{}, TotalCount: 0}
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	out := formatTimelineEvents(result, true, 9, "edge-2", start, end, 100)
	if !strings.Contains(out, "No timeline events for connection 9 (edge-2)") {
		t.Errorf("expected single-connection empty message, got: %s", out)
	}
}

func TestFormatTimelineEventsNilResult(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	end := time.Now()
	out := formatTimelineEvents(nil, false, 0, "", start, end, 100)
	if !strings.Contains(out, "No timeline events found across accessible connections") {
		t.Errorf("expected empty message when result is nil, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Integration tests against a real Postgres instance
// ---------------------------------------------------------------------------

// timelineSchema mirrors the subset of production tables that the
// timeline queries hit. We seed enough rows to exercise the alert_fired,
// alert_cleared, alert_acknowledged, restart, blackout_started, and
// blackout_ended subqueries; the config/HBA/extension/ident subqueries
// also execute, but with no data they simply contribute zero rows.
const timelineSchema = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP SCHEMA IF EXISTS metrics CASCADE;

CREATE SCHEMA metrics;

CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    group_id INTEGER
);

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    host VARCHAR(255) NOT NULL DEFAULT '',
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL DEFAULT 'postgres',
    is_shared BOOLEAN NOT NULL DEFAULT TRUE,
    owner_username VARCHAR(255),
    cluster_id INTEGER
);

CREATE TABLE alert_rules (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    metric_unit VARCHAR(64)
);

CREATE TABLE alerts (
    id SERIAL PRIMARY KEY,
    connection_id INTEGER NOT NULL,
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

CREATE TABLE alert_acknowledgments (
    id SERIAL PRIMARY KEY,
    alert_id INTEGER NOT NULL,
    acknowledged_at TIMESTAMPTZ NOT NULL,
    acknowledged_by VARCHAR(255) NOT NULL,
    acknowledge_type VARCHAR(32) NOT NULL DEFAULT 'manual',
    false_positive BOOLEAN NOT NULL DEFAULT FALSE,
    message TEXT NOT NULL DEFAULT ''
);

CREATE TABLE blackouts (
    id SERIAL PRIMARY KEY,
    scope VARCHAR(16) NOT NULL,
    connection_id INTEGER,
    cluster_id INTEGER,
    group_id INTEGER,
    reason TEXT,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ,
    created_by VARCHAR(255) NOT NULL DEFAULT 'tester'
);

CREATE TABLE metrics.pg_settings (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    name VARCHAR(255) NOT NULL,
    setting TEXT
);

CREATE TABLE metrics.pg_hba_file_rules (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    rule_number INTEGER NOT NULL,
    type VARCHAR(64),
    database VARCHAR(255),
    user_name VARCHAR(255),
    address VARCHAR(255),
    auth_method VARCHAR(64)
);

CREATE TABLE metrics.pg_ident_file_mappings (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    map_number INTEGER,
    map_name VARCHAR(255),
    sys_name VARCHAR(255),
    pg_username VARCHAR(255)
);

CREATE TABLE metrics.pg_node_role (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    postmaster_start_time TIMESTAMPTZ,
    is_in_recovery BOOLEAN,
    primary_role VARCHAR(64)
);

CREATE TABLE metrics.pg_extension (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    database_name VARCHAR(255),
    extname VARCHAR(255),
    extversion VARCHAR(64)
);
`

const timelineTeardown = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP SCHEMA IF EXISTS metrics CASCADE;
`

// newTimelineTestEnv prepares a Postgres instance with the timeline
// schema and seed data. The returned cleanup drops the schema and closes
// the pool. The test is skipped when TEST_AI_WORKBENCH_SERVER is unset.
func newTimelineTestEnv(t *testing.T) (*pgxpool.Pool, *database.Datastore, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping timeline integration test")
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

	if _, err := pool.Exec(ctx, timelineSchema); err != nil {
		pool.Close()
		t.Fatalf("create schema: %v", err)
	}

	ds := database.NewTestDatastore(pool)
	cleanup := func() {
		if _, err := pool.Exec(context.Background(), timelineTeardown); err != nil {
			t.Logf("teardown: %v", err)
		}
		pool.Close()
	}
	return pool, ds, cleanup
}

// seedTimelineFixtures inserts a small but representative set of timeline
// events. It returns the connection ID so tests can target it and the
// occurredAt time anchor used for the events so tests can compute the
// window. Events are timed so the default 24h window includes them.
func seedTimelineFixtures(t *testing.T, pool *pgxpool.Pool) (connID int, base time.Time) {
	t.Helper()
	ctx := context.Background()
	base = time.Now().UTC().Add(-2 * time.Hour)

	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (name, host, port, database_name, is_shared)
         VALUES ('prod-edge', 'localhost', 5432, 'postgres', TRUE)
         RETURNING id`,
	).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	// One fired + cleared + acknowledged alert.
	var alertID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO alerts (connection_id, severity, title, description, triggered_at, cleared_at, status)
         VALUES ($1, 'warning', 'CPU high', 'cpu over threshold', $2, $3, 'cleared')
         RETURNING id`,
		connID, base, base.Add(30*time.Minute),
	).Scan(&alertID); err != nil {
		t.Fatalf("insert alert: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO alert_acknowledgments (alert_id, acknowledged_at, acknowledged_by, message)
         VALUES ($1, $2, 'oncall', 'investigated')`,
		alertID, base.Add(45*time.Minute),
	); err != nil {
		t.Fatalf("insert acknowledgment: %v", err)
	}

	// A blackout that started and ended inside the window.
	if _, err := pool.Exec(ctx,
		`INSERT INTO blackouts (scope, connection_id, reason, start_time, end_time)
         VALUES ('server', $1, 'maintenance', $2, $3)`,
		connID, base.Add(time.Hour), base.Add(90*time.Minute),
	); err != nil {
		t.Fatalf("insert blackout: %v", err)
	}

	// Two pg_node_role rows so the restart subquery sees a transition.
	if _, err := pool.Exec(ctx,
		`INSERT INTO metrics.pg_node_role
         (connection_id, collected_at, postmaster_start_time, is_in_recovery, primary_role)
         VALUES ($1, $2, $3, FALSE, 'primary'),
                ($1, $4, $5, FALSE, 'primary')`,
		connID,
		base.Add(-30*time.Minute), base.Add(-time.Hour),
		base.Add(10*time.Minute), base.Add(5*time.Minute),
	); err != nil {
		t.Fatalf("insert pg_node_role: %v", err)
	}

	// Two pg_settings rows for the same parameter, with a changed value
	// between the first and second snapshot. The LAG() detection in the
	// config_change subquery requires at least two rows.
	if _, err := pool.Exec(ctx,
		`INSERT INTO metrics.pg_settings (connection_id, collected_at, name, setting)
         VALUES ($1, $2, 'work_mem', '4MB'),
                ($1, $3, 'work_mem', '8MB')`,
		connID,
		base.Add(-15*time.Minute), base.Add(20*time.Minute),
	); err != nil {
		t.Fatalf("insert pg_settings: %v", err)
	}
	return connID, base
}

func TestGetTimelineEventsIntegrationHappyPath(t *testing.T) {
	pool, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	connID, _ := seedTimelineFixtures(t, pool)

	tool := GetTimelineEventsTool(ds, nil, nil)
	resp, err := tool.Handler(map[string]any{
		"connection_id": float64(connID),
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content[0].Text)
	}
	body := resp.Content[0].Text
	if !strings.Contains(body, "Timeline Events | Connection:") {
		t.Errorf("expected single-connection header, got: %s", body)
	}
	for _, want := range []string{
		database.EventTypeAlertFired,
		database.EventTypeAlertCleared,
		database.EventTypeAlertAcknowledged,
		database.EventTypeBlackoutStarted,
		database.EventTypeBlackoutEnded,
		database.EventTypeRestart,
		database.EventTypeConfigChange,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected event type %q in output: %s", want, body)
		}
	}
}

func TestGetTimelineEventsIntegrationEventTypeFilter(t *testing.T) {
	pool, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	connID, _ := seedTimelineFixtures(t, pool)

	tool := GetTimelineEventsTool(ds, nil, nil)
	resp, err := tool.Handler(map[string]any{
		"connection_id": float64(connID),
		"event_types":   "restart, config_change",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content[0].Text)
	}
	body := resp.Content[0].Text
	if !strings.Contains(body, database.EventTypeRestart) {
		t.Errorf("expected restart event, got: %s", body)
	}
	if !strings.Contains(body, database.EventTypeConfigChange) {
		t.Errorf("expected config_change event, got: %s", body)
	}
	if strings.Contains(body, database.EventTypeAlertFired) {
		t.Errorf("alert_fired must be excluded by filter, got: %s", body)
	}
	if strings.Contains(body, database.EventTypeBlackoutStarted) {
		t.Errorf("blackout_started must be excluded by filter, got: %s", body)
	}
}

func TestGetTimelineEventsIntegrationExplicitTimeRange(t *testing.T) {
	pool, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	connID, base := seedTimelineFixtures(t, pool)

	// Narrow window that excludes the seeded alert/blackout events. We
	// pick a 1-minute slice ending right before "base" so all fixtures
	// fall outside.
	start := base.Add(-5 * time.Minute)
	end := base.Add(-4 * time.Minute)

	tool := GetTimelineEventsTool(ds, nil, nil)
	resp, err := tool.Handler(map[string]any{
		"connection_id": float64(connID),
		"start_time":    start.Format(time.RFC3339),
		"end_time":      end.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content[0].Text)
	}
	body := resp.Content[0].Text
	if !strings.Contains(body, "No timeline events for connection") {
		t.Errorf("expected empty-window message, got: %s", body)
	}
}

func TestGetTimelineEventsIntegrationDefaultRangeCoversFixtures(t *testing.T) {
	pool, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	connID, _ := seedTimelineFixtures(t, pool)

	// No start_time / end_time means default last 24h, which includes
	// all fixtures (anchored at now-2h).
	tool := GetTimelineEventsTool(ds, nil, nil)
	resp, err := tool.Handler(map[string]any{
		"connection_id": float64(connID),
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content[0].Text)
	}
	body := resp.Content[0].Text
	if !strings.Contains(body, "rows,") {
		t.Errorf("expected non-empty result with default range, got: %s", body)
	}
}

func TestGetTimelineEventsIntegrationLimitClamped(t *testing.T) {
	pool, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	connID, _ := seedTimelineFixtures(t, pool)

	tool := GetTimelineEventsTool(ds, nil, nil)
	resp, err := tool.Handler(map[string]any{
		"connection_id": float64(connID),
		"limit":         float64(timelineMaxLimit + 100),
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content[0].Text)
	}
	body := resp.Content[0].Text
	// The advertised limit in the header should be clamped to the max.
	want := fmt.Sprintf("Limit: %d", timelineMaxLimit)
	if !strings.Contains(body, want) {
		t.Errorf("expected header to advertise clamped limit %q, got: %s", want, body)
	}
}

func TestGetTimelineEventsIntegrationLimitDefault(t *testing.T) {
	pool, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	connID, _ := seedTimelineFixtures(t, pool)

	tool := GetTimelineEventsTool(ds, nil, nil)
	resp, err := tool.Handler(map[string]any{
		"connection_id": float64(connID),
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content[0].Text)
	}
	if !strings.Contains(resp.Content[0].Text, fmt.Sprintf("Limit: %d", timelineDefaultLimit)) {
		t.Errorf("expected default limit advertised in header: %s", resp.Content[0].Text)
	}
}

func TestGetTimelineEventsIntegrationInvalidConnectionID(t *testing.T) {
	_, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	tool := GetTimelineEventsTool(ds, nil, nil)
	resp, err := tool.Handler(map[string]any{
		"connection_id": "abc",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected error response for string connection_id")
	}
	if !strings.Contains(resp.Content[0].Text, "connection_id") ||
		!strings.Contains(resp.Content[0].Text, "integer") {
		t.Errorf("expected connection_id integer error, got: %s", resp.Content[0].Text)
	}
}

func TestGetTimelineEventsIntegrationUnknownConnectionID(t *testing.T) {
	_, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	tool := GetTimelineEventsTool(ds, nil, nil)
	resp, err := tool.Handler(map[string]any{
		"connection_id": float64(99999),
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected error response for unknown connection_id")
	}
	if !strings.Contains(resp.Content[0].Text, "does not exist") {
		t.Errorf("expected 'does not exist' error, got: %s", resp.Content[0].Text)
	}
}

func TestGetTimelineEventsIntegrationInvalidEventType(t *testing.T) {
	_, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	tool := GetTimelineEventsTool(ds, nil, nil)
	resp, err := tool.Handler(map[string]any{
		"event_types": "config_change,bogus_event",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected error response for invalid event_type")
	}
	if !strings.Contains(resp.Content[0].Text, "Invalid event_type") {
		t.Errorf("expected 'Invalid event_type' error, got: %s", resp.Content[0].Text)
	}
}

func TestGetTimelineEventsIntegrationInvalidLimit(t *testing.T) {
	_, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	tool := GetTimelineEventsTool(ds, nil, nil)
	cases := []struct {
		name string
		val  any
	}{
		{"zero", float64(0)},
		{"negative", float64(-1)},
		{"string", "lots"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := tool.Handler(map[string]any{"limit": tc.val})
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			if !resp.IsError {
				t.Fatalf("expected error response for limit=%v", tc.val)
			}
			if !strings.Contains(resp.Content[0].Text, "limit") {
				t.Errorf("expected limit error message, got: %s", resp.Content[0].Text)
			}
		})
	}
}

func TestGetTimelineEventsIntegrationInvalidStartTime(t *testing.T) {
	_, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	tool := GetTimelineEventsTool(ds, nil, nil)
	resp, err := tool.Handler(map[string]any{
		"start_time": "not-a-time",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected error response for invalid start_time")
	}
	if !strings.Contains(resp.Content[0].Text, "Invalid 'start_time'") {
		t.Errorf("expected start_time error, got: %s", resp.Content[0].Text)
	}
}

func TestGetTimelineEventsIntegrationInvalidEndTime(t *testing.T) {
	_, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	tool := GetTimelineEventsTool(ds, nil, nil)
	resp, err := tool.Handler(map[string]any{
		"end_time": "not-a-time",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected error response for invalid end_time")
	}
	if !strings.Contains(resp.Content[0].Text, "Invalid 'end_time'") {
		t.Errorf("expected end_time error, got: %s", resp.Content[0].Text)
	}
}

func TestGetTimelineEventsIntegrationInvertedTimeRange(t *testing.T) {
	_, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	tool := GetTimelineEventsTool(ds, nil, nil)
	resp, err := tool.Handler(map[string]any{
		"start_time": "2026-05-02T00:00:00Z",
		"end_time":   "2026-05-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected error for inverted range")
	}
	if !strings.Contains(resp.Content[0].Text, "start_time must be before end_time") {
		t.Errorf("expected inverted-range error, got: %s", resp.Content[0].Text)
	}
}

func TestGetTimelineEventsIntegrationMultiConnectionAllAccess(t *testing.T) {
	pool, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	// Seed fixtures so the query yields at least one event and the
	// multi-connection header is emitted instead of the empty-result
	// short message. The seed connection is shared by default, so the
	// nil RBACChecker path treats access as unrestricted.
	seedTimelineFixtures(t, pool)

	tool := GetTimelineEventsTool(ds, nil, nil)
	resp, err := tool.Handler(map[string]any{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content[0].Text)
	}
	if !strings.Contains(resp.Content[0].Text, "Timeline Events | All accessible connections") {
		t.Errorf("expected multi-connection header, got: %s", resp.Content[0].Text)
	}
}

// ---------------------------------------------------------------------------
// RBAC-denial test using an in-process auth store and stub lister
// ---------------------------------------------------------------------------

// TestGetTimelineEventsRBACDeniesNoAccess mirrors the regression guards in
// rbac_regression_test.go for the other monitored-data tools: a user with
// zero visible connections must see the explicit "no access" message and
// no leaked event data. The handler should short-circuit before any SQL
// is issued, so a nil datastore would prevent us from reaching this code
// path. We use an unreachable pool wrapped in a Datastore instead.
func TestGetTimelineEventsRBACDeniesNoAccess(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://nobody:nopass@127.0.0.1:1/nope")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()
	ds := database.NewTestDatastore(pool)

	tmpDir, err := os.MkdirTemp("", "tools-timeline-rbac-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer store.Close()
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	if err := store.CreateUser("bob", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	lister := &stubVisibilityLister{
		connections: []auth.ConnectionVisibilityInfo{
			{ID: 42, IsShared: false, OwnerUsername: "alice"},
		},
	}
	rbac := auth.NewRBACChecker(store)
	tool := GetTimelineEventsTool(ds, rbac, lister)

	ctx := nonSuperuserContext(userID, "bob")
	resp, err := tool.Handler(map[string]any{"__context": ctx})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if resp.IsError {
		t.Fatalf("expected non-error response, got: %+v", resp.Content)
	}
	body := resp.Content[0].Text
	if !strings.Contains(body, "No timeline events found") ||
		!strings.Contains(body, "You do not have access to any connections") {
		t.Errorf("expected RBAC denial message, got: %q", body)
	}
	if strings.Contains(body, "42") {
		t.Errorf("response leaked unshared connection id 42: %q", body)
	}
}

// TestGetTimelineEventsRBACDeniesExplicitConnectionID guards the case
// where the caller asks for a specific connection_id they do not own and
// is not shared with them. The tool must return an access-denied error
// without revealing whether the connection exists, mirroring
// get_alert_history's behavior.
func TestGetTimelineEventsRBACDeniesExplicitConnectionID(t *testing.T) {
	pool, ds, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	// Seed a connection owned by alice, NOT shared.
	var connID int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name, host, port, database_name, is_shared, owner_username)
         VALUES ('alice-only', 'localhost', 5432, 'postgres', FALSE, 'alice')
         RETURNING id`,
	).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "tools-timeline-rbac-conn-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer store.Close()
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)
	if err := store.CreateUser("bob", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	rbac := auth.NewRBACCheckerForDatastore(store, ds)
	tool := GetTimelineEventsTool(ds, rbac, database.NewVisibilityLister(ds))

	ctx := nonSuperuserContext(userID, "bob")
	resp, err := tool.Handler(map[string]any{
		"__context":     ctx,
		"connection_id": float64(connID),
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected access denied, got: %+v", resp.Content)
	}
	if !strings.Contains(resp.Content[0].Text, "Access denied") {
		t.Errorf("expected access denied message, got: %s", resp.Content[0].Text)
	}
}
