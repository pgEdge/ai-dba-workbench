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
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRedactConfigChangeDetails(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string // "" means nil
	}{
		{
			name: "conninfo password redacted, rest kept",
			in: `{"change_count":2,"changes":[` +
				`{"name":"primary_conninfo","old_value":"host=192.0.2.1 password=old","new_value":"host=192.0.2.2 password=new","change_type":"modified"},` +
				`{"name":"work_mem","old_value":"4096","new_value":"8192","change_type":"modified"}]}`,
			want: `{"change_count":2,"changes":[` +
				`{"change_type":"modified","name":"primary_conninfo","new_value":"host=192.0.2.2 password=********","old_value":"host=192.0.2.1 password=********"},` +
				`{"change_type":"modified","name":"work_mem","new_value":"8192","old_value":"4096"}]}`,
		},
		{
			name: "null, non-string and unparseable values",
			in:   `{"changes":[{"name":"ssl_passphrase_command","old_value":null,"new_value":42}]}`,
			want: `{"changes":[{"name":"ssl_passphrase_command","new_value":"********","old_value":null}]}`,
		},
		{
			name: "shell command keeps its program name",
			in:   `{"changes":[{"name":"archive_command","old_value":"","new_value":"PGPASSWORD=s3cret pgbackrest archive-push %p"}]}`,
			want: `{"changes":[{"name":"archive_command","new_value":"PGPASSWORD=******** pgbackrest ********","old_value":""}]}`,
		},
		{
			name: "missing value key is not added",
			in:   `{"changes":[{"name":"myext.password","new_value":"s3cret"}]}`,
			want: `{"changes":[{"name":"myext.password","new_value":"********"}]}`,
		},
		{
			name: "nothing to redact returns input unchanged",
			in:   `{"change_count": 1, "changes": [{"name": "work_mem", "old_value": "1", "new_value": "2"}, {"old_value": "x"}]}`,
			want: `{"change_count": 1, "changes": [{"name": "work_mem", "old_value": "1", "new_value": "2"}, {"old_value": "x"}]}`,
		},
		{
			name: "no changes key",
			in:   `{"change_count":0}`,
			want: `{"change_count":0}`,
		},
		{name: "not an object", in: `[1,2]`, want: ""},
		{name: "changes not an array", in: `{"changes":"primary_conninfo"}`, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactConfigChangeDetails(json.RawMessage(tt.in))
			if tt.want == "" {
				if got != nil {
					t.Errorf("got %s, want nil", got)
				}
				return
			}
			if string(got) != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestRedactSettingJSONValue_InvalidJSON(t *testing.T) {
	got := redactSettingJSONValue("primary_conninfo", json.RawMessage(`{`))
	if string(got) != `"********"` {
		t.Errorf("got %s, want the redacted placeholder", got)
	}
}

func TestRedactTimelineEvents(t *testing.T) {
	secret := json.RawMessage(`{"changes":[{"name":"primary_conninfo","new_value":"password=x"}]}`)
	events := []TimelineEvent{
		{EventType: EventTypeConfigChange, Details: secret},
		{EventType: EventTypeConfigChange},
		{EventType: EventTypeHBAChange, Details: secret},
	}

	redactTimelineEvents(events)

	if strings.Contains(string(events[0].Details), "password=x") {
		t.Errorf("config change details not redacted: %s", events[0].Details)
	}
	if events[1].Details != nil {
		t.Errorf("nil details became %s", events[1].Details)
	}
	if string(events[2].Details) != string(secret) {
		t.Errorf("non-config event details changed: %s", events[2].Details)
	}
}

const configChangeRedactionSchema = `
DROP TABLE IF EXISTS metrics.pg_settings CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
CREATE SCHEMA IF NOT EXISTS metrics;
CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);
CREATE TABLE metrics.pg_settings (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    name TEXT NOT NULL,
    setting TEXT
);
`

const configChangeRedactionTeardown = `
DROP TABLE IF EXISTS metrics.pg_settings CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
`

// TestGetTimelineEvents_RedactsConfigChangeCredentials stores two
// pg_settings snapshots as a collector that did not redact them would
// have, and checks that the configuration change event returned for them
// carries no password (GitHub issue #643).
func TestGetTimelineEvents_RedactsConfigChangeCredentials(t *testing.T) {
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping config change redaction test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("Test database ping failed: %v", err)
	}
	if _, err := pool.Exec(ctx, configChangeRedactionSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(), configChangeRedactionTeardown); err != nil {
			t.Logf("teardown: %v", err)
		}
	}()

	var connID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (name) VALUES ('standby') RETURNING id`,
	).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	first := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	second := first.Add(time.Hour)
	snapshots := []struct {
		at                time.Time
		conninfo, workMem string
	}{
		{first, "host=192.0.2.1 user=replicator password=0ldS3cret", "4096"},
		{second, "host=192.0.2.2 user=replicator password=********", "8192"},
	}
	for _, s := range snapshots {
		if _, err := pool.Exec(ctx, `
			INSERT INTO metrics.pg_settings (connection_id, collected_at, name, setting)
			VALUES ($1, $2, 'primary_conninfo', $3), ($1, $2, 'work_mem', $4)
		`, connID, s.at, s.conninfo, s.workMem); err != nil {
			t.Fatalf("insert settings: %v", err)
		}
	}

	ds := NewTestDatastore(pool)
	result, err := ds.GetTimelineEvents(ctx, TimelineFilter{
		ConnectionID: &connID,
		StartTime:    first.Add(-time.Hour),
		EndTime:      second.Add(time.Hour),
		EventTypes:   []string{EventTypeConfigChange},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("GetTimelineEvents: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("got %d events, want 1", len(result.Events))
	}

	details := string(result.Events[0].Details)
	for _, secret := range []string{"0ldS3cret"} {
		if strings.Contains(details, secret) {
			t.Errorf("event details leak %q: %s", secret, details)
		}
	}
	for _, want := range []string{
		// A stored password is masked with the whole value; a value
		// the collector already redacted is kept.
		`"old_value":"********"`,
		`"new_value":"host=192.0.2.2 user=replicator password=********"`,
		`"old_value":"4096"`,
		`"new_value":"8192"`,
	} {
		if !strings.Contains(details, want) {
			t.Errorf("event details lack %q: %s", want, details)
		}
	}
}
