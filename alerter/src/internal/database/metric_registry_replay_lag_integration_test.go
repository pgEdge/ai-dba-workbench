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
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// replayLagMetric is the registry entry under test. See GitHub issue
// #625: its query once read a replay_lsn_timestamp column that the
// collector never stores, so it failed on every install.
const replayLagMetric = "pg_stat_replication.replay_lag_seconds"

// pgStatReplicationDDL matches the collector's CREATE TABLE statement for
// metrics.pg_stat_replication. The tests below create the table from the
// collector's own source rather than from a copy, because a hand-written
// copy is what let the missing column go unnoticed.
var pgStatReplicationDDL = regexp.MustCompile(
	`(?s)CREATE TABLE IF NOT EXISTS metrics\.pg_stat_replication \(.*?\) PARTITION BY RANGE \(collected_at\);`)

// collectorPgStatReplicationDDL returns the collector's DDL for
// metrics.pg_stat_replication, failing the test when the collector
// source cannot be read or no longer contains the statement.
func collectorPgStatReplicationDDL(t *testing.T) string {
	t.Helper()

	source, err := os.ReadFile(collectorProbeSeedPath)
	if err != nil {
		t.Fatalf("collector schema not readable beside the alerter: %v", err)
	}
	ddl := pgStatReplicationDDL.Find(source)
	if ddl == nil {
		t.Fatalf("%s no longer creates metrics.pg_stat_replication in the "+
			"expected form; update pgStatReplicationDDL", collectorProbeSeedPath)
	}
	return string(ddl)
}

// newReplayLagTestDatastore creates metrics.pg_stat_replication from the
// collector's DDL, with a default partition to hold the fixture rows, and
// returns a Datastore over it. The cleanup function drops the schema.
func newReplayLagTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	ddl := collectorPgStatReplicationDDL(t)
	connStr := requireLocalTestDSN(t, "the replay lag metric integration test")

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	setup := `
		DROP SCHEMA IF EXISTS metrics CASCADE;
		CREATE SCHEMA metrics;
	` + ddl + `
		CREATE TABLE metrics.pg_stat_replication_default
		    PARTITION OF metrics.pg_stat_replication DEFAULT;
	`
	if _, err := pool.Exec(ctx, setup); err != nil {
		pool.Close()
		t.Fatalf("failed to create metrics.pg_stat_replication: %v", err)
	}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(),
			`DROP SCHEMA IF EXISTS metrics CASCADE`); err != nil {
			t.Logf("replay lag teardown failed: %v", err)
		}
		pool.Close()
	}
	return &Datastore{pool: pool}, pool, cleanup
}

// replicationRow is one stored pg_stat_replication row. A nil lag leaves
// replay_lag NULL, as PostgreSQL reports it for an idle standby that has
// caught up, and as the probe stores it for a WAL receiver row.
type replicationRow struct {
	role string
	pid  int
	lag  *float64
}

// lagSecs returns a pointer to a replay lag in seconds.
func lagSecs(s float64) *float64 { return &s }

// insertReplicationSample writes rows as one probe sample: the probe
// stamps every row of a collection with the same collected_at.
func insertReplicationSample(t *testing.T, pool *pgxpool.Pool, connID int,
	at time.Time, rows []replicationRow) {
	t.Helper()

	for _, r := range rows {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO metrics.pg_stat_replication
			    (connection_id, pid, role, application_name, state,
			     replay_lag, collected_at)
			VALUES ($1, $2, $3, $4,
			        CASE WHEN $3 = 'primary' THEN 'streaming' END,
			        make_interval(secs => $5), $6)
		`, connID, r.pid, r.role, fmt.Sprintf("standby_%d", r.pid), r.lag,
			at); err != nil {
			t.Fatalf("failed to insert pg_stat_replication row (pid %d): %v",
				r.pid, err)
		}
	}
}

// TestReplayLagSeconds_LatestValues runs the registry query against rows
// in the collector's own metrics.pg_stat_replication table and checks
// that it reports, for each connection, the largest replay_lag in that
// connection's newest sample, and nothing when that sample has no lag.
func TestReplayLagSeconds_LatestValues(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name string
		// samples maps an age (subtracted from now) to the rows of the
		// sample collected at that age.
		samples map[time.Duration][]replicationRow
		// want is the expected value, or nil when no row is expected.
		want *float64
	}{
		{
			name: "largest lag across standbys in the newest sample",
			samples: map[time.Duration][]replicationRow{
				40 * time.Second: {
					{role: "primary", pid: 101, lag: lagSecs(90)},
					{role: "primary", pid: 102, lag: lagSecs(120)},
				},
				10 * time.Second: {
					{role: "primary", pid: 101, lag: lagSecs(2.5)},
					{role: "primary", pid: 102, lag: lagSecs(7.25)},
				},
			},
			want: lagSecs(7.25),
		},
		{
			name: "standby without a lag value is ignored",
			samples: map[time.Duration][]replicationRow{
				10 * time.Second: {
					{role: "primary", pid: 101, lag: nil},
					{role: "primary", pid: 102, lag: lagSecs(31)},
				},
			},
			want: lagSecs(31),
		},
		{
			name: "idle caught-up standby clears despite an older lag",
			samples: map[time.Duration][]replicationRow{
				40 * time.Second: {
					{role: "primary", pid: 101, lag: lagSecs(45)},
				},
				10 * time.Second: {
					{role: "primary", pid: 101, lag: nil},
				},
			},
			want: nil,
		},
		{
			name: "WAL receiver rows on a standby report nothing",
			samples: map[time.Duration][]replicationRow{
				10 * time.Second: {
					{role: "standby", pid: 201, lag: nil},
				},
			},
			want: nil,
		},
		{
			name: "cascading standby reports its downstream sender",
			samples: map[time.Duration][]replicationRow{
				10 * time.Second: {
					{role: "standby", pid: 201, lag: nil},
					{role: "primary", pid: 202, lag: lagSecs(4)},
				},
			},
			want: lagSecs(4),
		},
		{
			name: "sample older than the five minute window is ignored",
			samples: map[time.Duration][]replicationRow{
				6 * time.Minute: {
					{role: "primary", pid: 101, lag: lagSecs(600)},
				},
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ds, pool, cleanup := newReplayLagTestDatastore(t)
			defer cleanup()

			const connID = 1
			var newest time.Time
			for age, rows := range tc.samples {
				at := now.Add(-age)
				insertReplicationSample(t, pool, connID, at, rows)
				if at.After(newest) {
					newest = at
				}
			}

			values, err := ds.GetLatestMetricValues(context.Background(),
				replayLagMetric)
			if tc.want == nil {
				// An empty result is how the metric reports that no
				// standby is lagging; the alert cleaner reads it as
				// recovery because the entry is clearWhenAbsent.
				if !errors.Is(err, ErrNoMetricData) {
					t.Fatalf("got rows %+v and error %v, want ErrNoMetricData",
						values, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetLatestMetricValues failed: %v", err)
			}
			if len(values) != 1 {
				t.Fatalf("got %d rows (%+v), want exactly one", len(values), values)
			}
			got := values[0]
			if got.ConnectionID != connID {
				t.Errorf("connection_id = %d, want %d", got.ConnectionID, connID)
			}
			if math.Abs(got.Value-*tc.want) > 1e-9 {
				t.Errorf("value = %v, want %v", got.Value, *tc.want)
			}
			if !got.CollectedAt.Equal(newest.Truncate(time.Microsecond)) {
				t.Errorf("collected_at = %v, want the newest sample %v",
					got.CollectedAt, newest)
			}
		})
	}
}

// TestReplayLagSeconds_OneRowPerConnection checks that each connection is
// reported once, whatever its number of standbys and samples, because
// the threshold hysteresis keys its state on the connection (GitHub
// issue #620).
func TestReplayLagSeconds_OneRowPerConnection(t *testing.T) {
	ds, pool, cleanup := newReplayLagTestDatastore(t)
	defer cleanup()

	now := time.Now().UTC()
	for i, age := range []time.Duration{90 * time.Second, 60 * time.Second, 30 * time.Second} {
		lag := float64(10 * (i + 1))
		insertReplicationSample(t, pool, 1, now.Add(-age), []replicationRow{
			{role: "primary", pid: 101, lag: lagSecs(lag)},
			{role: "primary", pid: 102, lag: lagSecs(lag + 1)},
			{role: "primary", pid: 103, lag: lagSecs(lag / 2)},
		})
		insertReplicationSample(t, pool, 2, now.Add(-age), []replicationRow{
			{role: "primary", pid: 301, lag: lagSecs(lag * 3)},
		})
	}

	values, err := ds.GetLatestMetricValues(context.Background(), replayLagMetric)
	if err != nil {
		t.Fatalf("GetLatestMetricValues failed: %v", err)
	}

	want := map[int]float64{1: 31, 2: 90}
	if len(values) != len(want) {
		t.Fatalf("got %d rows (%+v), want %d", len(values), values, len(want))
	}
	for connID, w := range want {
		if got := findValueForConnection(t, values, connID); math.Abs(got-w) > 1e-9 {
			t.Errorf("connection %d value = %v, want %v", connID, got, w)
		}
	}
}
