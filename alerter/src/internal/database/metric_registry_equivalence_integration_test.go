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
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The tests in this file prove that each historical query added for
// GitHub issue #490 computes the same statistic as its latest query, so
// a live value is scored against a baseline of like values (the mismatch
// GitHub issue #567 found in connection_utilization_percent).
//
// The latest queries read the newest sample, so most tests compare them
// sample by sample: they read the historical rows once, then repeatedly
// run the latest query, check it against the historical row for the
// newest remaining sample and delete that sample, which turns the next
// one into the newest.

// deleteSample removes every row of the given metrics table stamped with
// the given connection and collected_at. The table name is one of the
// fixed names the tests pass, never input.
func deleteSample(t *testing.T, pool *pgxpool.Pool, table string, connID int, at time.Time) {
	t.Helper()

	sql := map[string]string{
		"pg_stat_activity":    `DELETE FROM metrics.pg_stat_activity WHERE connection_id = $1 AND collected_at = $2`,
		"pg_stat_replication": `DELETE FROM metrics.pg_stat_replication WHERE connection_id = $1 AND collected_at = $2`,
		"pg_stat_statements":  `DELETE FROM metrics.pg_stat_statements WHERE connection_id = $1 AND collected_at = $2`,
	}[table]
	if sql == "" {
		t.Fatalf("deleteSample: unknown table %q", table)
	}
	if _, err := pool.Exec(context.Background(), sql, connID, at); err != nil {
		t.Fatalf("failed to delete the %s sample at %v: %v", table, at, err)
	}
}

// latestRowsFor returns the latest values the registry reports for the
// metric and connection, keyed by database name ("" when the metric has
// no database dimension). A metric with no rows yields an empty map
// rather than ErrNoMetricData.
func latestRowsFor(t *testing.T, ds *Datastore, metric string, connID int) map[string]MetricValue {
	t.Helper()

	values, err := ds.GetLatestMetricValues(context.Background(), metric)
	if errors.Is(err, ErrNoMetricData) {
		return map[string]MetricValue{}
	}
	if err != nil {
		t.Fatalf("GetLatestMetricValues(%s) failed: %v", metric, err)
	}
	found := make(map[string]MetricValue)
	for _, v := range values {
		if v.ConnectionID != connID {
			continue
		}
		key := ""
		if v.DatabaseName != nil {
			key = *v.DatabaseName
		}
		if _, dup := found[key]; dup {
			t.Fatalf("%s: more than one latest row for connection %d, database %q",
				metric, connID, key)
		}
		found[key] = v
	}
	return found
}

// historicalKey identifies one historical row by database and sample.
type historicalKey struct {
	db string
	at time.Time
}

// historicalRowsByKey returns the metric's historical rows for the
// connection over a one day lookback, keyed by database and collected_at.
func historicalRowsByKey(t *testing.T, ds *Datastore, metric string, connID int) map[historicalKey]HistoricalMetricValue {
	t.Helper()

	found := make(map[historicalKey]HistoricalMetricValue)
	for _, r := range historicalRowsFor(t, ds, metric, connID) {
		key := historicalKey{at: r.CollectedAt.UTC()}
		if r.DatabaseName != nil {
			key.db = *r.DatabaseName
		}
		if _, dup := found[key]; dup {
			t.Fatalf("%s: more than one historical row for %+v", metric, key)
		}
		found[key] = r
	}
	return found
}

// insertStandbyLSNSample writes one pg_stat_replication row per standby
// for a single probe run.
func insertStandbyLSNSample(t *testing.T, pool *pgxpool.Pool, connID int,
	at time.Time, standbys map[int][2]*string) {
	t.Helper()

	for pid, lsns := range standbys {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO metrics.pg_stat_replication
			    (connection_id, pid, sent_lsn, replay_lsn, collected_at)
			VALUES ($1, $2, $3, $4, $5)
		`, connID, pid, lsns[0], lsns[1], at); err != nil {
			t.Fatalf("failed to insert pg_stat_replication row: %v", err)
		}
	}
}

// lsns pairs a sent and a replay LSN for insertStandbyLSNSample.
func lsns(sent, replay string) [2]*string {
	return [2]*string{&sent, &replay}
}

// TestLagBytes_HistoricalMatchesLatest checks that both queries report
// the largest lag across the standbys of each sample, ignore a standby
// whose LSNs are NULL, and agree on every sample. The newest sample has
// lost the standby that lagged furthest in the sample before it, so a
// latest query that took each pid's newest row would still report that
// standby's lag. A newer sample still holds only a row with NULL LSNs,
// as a primary demoted to a standby stores for its WAL receiver, so
// neither query may report a value for it; a latest query that filtered
// NULLs before choosing the newest sample would report the older lag.
func TestLagBytes_HistoricalMatchesLatest(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	const metric = "pg_stat_replication.lag_bytes"
	connID := insertConnection(t, pool, "lag-bytes-490")

	older := sampleInstant(4 * time.Minute)
	middle := sampleInstant(3 * time.Minute)
	newest := sampleInstant(2 * time.Minute)
	nullOnly := sampleInstant(1 * time.Minute)
	// The differences are 0x1000 = 4096 and 0x100000 = 1048576 bytes.
	insertStandbyLSNSample(t, pool, connID, older, map[int][2]*string{
		100: lsns("0/2001000", "0/2000000"),
		200: lsns("0/2000000", "0/2000000"),
	})
	insertStandbyLSNSample(t, pool, connID, middle, map[int][2]*string{
		100: lsns("0/3001000", "0/3000000"),
		200: lsns("0/3100000", "0/3000000"),
		300: {nil, nil},
	})
	insertStandbyLSNSample(t, pool, connID, newest, map[int][2]*string{
		100: lsns("0/4001000", "0/4000000"),
	})
	insertStandbyLSNSample(t, pool, connID, nullOnly, map[int][2]*string{
		400: {nil, nil},
	})

	want := map[time.Time]float64{older: 4096, middle: 1048576, newest: 4096}
	historical := historicalRowsByKey(t, ds, metric, connID)
	if len(historical) != len(want) {
		t.Fatalf("historical rows = %+v, want one per sample with a standby LSN",
			historical)
	}
	if latest, ok := latestRowsFor(t, ds, metric, connID)[""]; ok {
		t.Fatalf("latest row = %+v with only NULL LSNs newest, want none", latest)
	}
	deleteSample(t, pool, "pg_stat_replication", connID, nullOnly)
	for _, at := range []time.Time{newest, middle, older} {
		row, ok := historical[historicalKey{at: at}]
		if !ok {
			t.Fatalf("no historical row for the sample at %v", at)
		}
		assertClose(t, "historical", row.Value, want[at])
		if row.DatabaseName != nil {
			t.Errorf("historical database = %v, want NULL", *row.DatabaseName)
		}

		latest := latestRowsFor(t, ds, metric, connID)
		got, ok := latest[""]
		if !ok {
			t.Fatalf("no latest row with the sample at %v newest", at)
		}
		assertClose(t, "latest", got.Value, row.Value)
		deleteSample(t, pool, "pg_stat_replication", connID, at)
	}
}

// TestMaxLockWaitSeconds_HistoricalMatchesLatest checks that the
// historical query reports what the latest query reports for a sample
// with lock waiters, counting client backends only, and, like the latest
// query, reports no row for a sample with none, so the baseline holds
// only values the latest query can produce.
func TestMaxLockWaitSeconds_HistoricalMatchesLatest(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	const metric = "pg_stat_activity.max_lock_wait_seconds"
	lock := strPtr("Lock")
	active := strPtr("active")
	connID := insertConnection(t, pool, "max-lock-wait-490")

	samples := []struct {
		at   time.Time
		rows []activityRow
		want float64
	}{
		{
			// The oldest sample: two waiters, plus an autovacuum worker
			// waiting longer, which neither query may count.
			at: sampleInstant(4 * time.Minute),
			rows: []activityRow{
				{backendType: clientBackend, waitEventType: lock, queryAge: 12 * time.Second},
				{backendType: clientBackend, waitEventType: lock, queryAge: 30 * time.Second},
				{backendType: autovacuumWorker, waitEventType: lock, queryAge: 900 * time.Second},
			},
			want: 30,
		},
		{
			// No waiter at all, only a running query and a background
			// process waiting on a lock.
			at: sampleInstant(3 * time.Minute),
			rows: []activityRow{
				{backendType: clientBackend, state: active, queryAge: 50 * time.Second},
				{backendType: backgroundWorkerT, waitEventType: lock, queryAge: 70 * time.Second},
			},
			want: 0,
		},
		{
			// The newest sample: an idle server.
			at:   sampleInstant(2 * time.Minute),
			rows: []activityRow{{backendType: clientBackend}, {backendType: checkpointer}},
			want: 0,
		},
	}
	for _, s := range samples {
		insertActivitySnapshot(t, pool, connID, s.at, s.rows)
	}

	// Only the first sample has a waiter, so it is the only one that may
	// reach the baseline.
	historical := historicalRowsByKey(t, ds, metric, connID)
	if len(historical) != 1 {
		t.Fatalf("historical rows = %+v, want one for the sample with a waiter",
			historical)
	}
	for i := len(samples) - 1; i >= 0; i-- {
		s := samples[i]
		row, inHistory := historical[historicalKey{at: s.at}]
		latest := latestRowsFor(t, ds, metric, connID)
		got, inLatest := latest[""]
		switch {
		case s.want == 0 && (inHistory || inLatest):
			t.Errorf("sample at %v has no waiter: historical %+v (%v), latest %+v (%v), want neither",
				s.at, row, inHistory, got, inLatest)
		case s.want != 0 && (!inHistory || !inLatest):
			t.Errorf("sample at %v has a waiter: historical present %v, latest present %v, want both",
				s.at, inHistory, inLatest)
		case s.want != 0:
			assertClose(t, "historical", row.Value, s.want)
			assertClose(t, "latest", got.Value, row.Value)
		}
		deleteSample(t, pool, "pg_stat_activity", connID, s.at)
	}
}

// insertCheckpointerSample writes one pg_stat_checkpointer row and,
// when role is not nil, a pg_node_role row at the same instant.
func insertCheckpointerSample(t *testing.T, pool *pgxpool.Pool, connID int,
	at time.Time, requested int64, role *bool) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), `
		WITH checkpointer AS (
			INSERT INTO metrics.pg_stat_checkpointer
			    (connection_id, num_requested, collected_at)
			VALUES ($1, $2, $3)
		)
		INSERT INTO metrics.pg_node_role (connection_id, is_in_recovery, collected_at)
		SELECT $1, $4::boolean, $3
		WHERE $4::boolean IS NOT NULL
	`, connID, requested, at, role); err != nil {
		t.Fatalf("failed to insert the checkpointer sample: %v", err)
	}
}

// testClockSchema defines test_clock.now(), which returns the instant
// stored in test_clock.clock. A session whose search_path lists
// test_clock before pg_catalog resolves an unqualified NOW() to it, so a
// registry query runs unchanged as though it were that instant.
const testClockSchema = `
DROP SCHEMA IF EXISTS test_clock CASCADE;
CREATE SCHEMA test_clock;
CREATE TABLE test_clock.clock (instant TIMESTAMPTZ NOT NULL);
INSERT INTO test_clock.clock VALUES (pg_catalog.now());
CREATE FUNCTION test_clock.now() RETURNS TIMESTAMPTZ
    LANGUAGE sql STABLE
    AS 'SELECT instant FROM test_clock.clock';
`

// newClockedDatastore returns a Datastore over the same test database
// whose queries read NOW() from a clock the test sets, so a latest query
// can be run as it would have run at the end of a past hour. The tables
// are the ones pool's datastore created; the returned function sets the
// clock.
func newClockedDatastore(t *testing.T, pool *pgxpool.Pool) (*Datastore, func(time.Time)) {
	t.Helper()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, testClockSchema); err != nil {
		t.Fatalf("failed to create the test clock: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(requireLocalTestDSN(t, "the test clock"))
	if err != nil {
		t.Fatalf("could not parse the test database DSN: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "test_clock, pg_catalog, public"
	clocked, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("could not connect the clocked pool: %v", err)
	}
	// Cleanups run last in, first out, and after the caller's deferred
	// teardown has closed pool, so the clock is dropped through its own
	// pool before that pool closes.
	t.Cleanup(clocked.Close)
	t.Cleanup(func() {
		if _, err := clocked.Exec(context.Background(),
			"DROP SCHEMA IF EXISTS test_clock CASCADE"); err != nil {
			t.Logf("test clock teardown failed: %v", err)
		}
	})

	set := func(at time.Time) {
		t.Helper()
		if _, err := clocked.Exec(context.Background(),
			"UPDATE test_clock.clock SET instant = $1", at); err != nil {
			t.Fatalf("failed to set the test clock: %v", err)
		}
	}
	return &Datastore{pool: clocked}, set
}

// TestCheckpointsReqDelta_HistoricalMatchesLatest checks that every
// hourly historical bucket equals what the latest query reported at the
// end of that hour, for each of several connections whose samples
// interleave. The samples span three hours at a 600 second interval, so
// a bucket that took its first delta from the previous hour's last
// sample would hold six deltas against the live value's five; a LAG that
// ignored the connection would difference one server's counter against
// another's; and a standby must be absent from both queries.
func TestCheckpointsReqDelta_HistoricalMatchesLatest(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()
	clocked, setClock := newClockedDatastore(t, pool)

	const metric = "pg_stat_checkpointer.checkpoints_req_delta"
	const hours = 3
	primaryRole, standbyRole := false, true
	h0 := time.Now().UTC().Truncate(time.Hour).Add(-5 * time.Hour)

	servers := []struct {
		name   string
		minute int
		role   *bool
		// counter returns the cumulative num_requested at sample n.
		counter func(n int) int64
		// want is the per-bucket value, or nil for no row.
		want []float64
	}{
		{
			// A steady primary requesting one checkpoint per sample.
			name: "steady", minute: 5, role: &primaryRole,
			counter: func(n int) int64 { return 1000 + int64(n) },
			want:    []float64{5, 5, 5},
		},
		{
			// No role samples, three per sample, and a statistics reset
			// at the fourth sample of the second hour, counted as 0.
			name: "reset", minute: 7,
			counter: func(n int) int64 {
				if n >= 9 {
					return 2 + 3*int64(n-9)
				}
				return 50000 + 3*int64(n)
			},
			want: []float64{15, 12, 15},
		},
		{
			// A standby: excluded from both queries.
			name: "standby", minute: 9, role: &standbyRole,
			counter: func(n int) int64 { return 7000 + 2*int64(n) },
		},
	}

	connIDs := make([]int, len(servers))
	for i, s := range servers {
		connIDs[i] = insertConnection(t, pool, "checkpoints-490-"+s.name)
	}

	// The latest query assumes no sample is newer than NOW(), so each
	// hour's samples are written just before the clock reaches the end
	// of that hour.
	for h := 0; h < hours; h++ {
		for i, s := range servers {
			for n := 6 * h; n < 6*(h+1); n++ {
				at := h0.Add(time.Duration(s.minute+10*n) * time.Minute)
				insertCheckpointerSample(t, pool, connIDs[i], at, s.counter(n), s.role)
			}
		}

		setClock(h0.Add(time.Duration(h+1) * time.Hour))
		for i, s := range servers {
			latest := latestRowsFor(t, clocked, metric, connIDs[i])
			got, ok := latest[""]
			switch {
			case s.want == nil && ok:
				t.Errorf("%s: latest at the end of hour %d = %v, want no row", s.name, h, got.Value)
			case s.want != nil && !ok:
				t.Errorf("%s: no latest row at the end of hour %d", s.name, h)
			case ok:
				assertClose(t, s.name+" latest", got.Value, s.want[h])
			}
		}
	}

	for i, s := range servers {
		historical := historicalRowsFor(t, ds, metric, connIDs[i])
		if s.want == nil {
			if len(historical) != 0 {
				t.Errorf("%s: historical = %+v, want none", s.name, historical)
			}
			continue
		}
		if len(historical) != hours {
			t.Fatalf("%s: historical = %+v, want %d buckets", s.name, historical, hours)
		}
		for h, row := range historical {
			bucket := h0.Add(time.Duration(h) * time.Hour)
			if !row.CollectedAt.Equal(bucket) {
				t.Errorf("%s: bucket[%d] = %v, want %v", s.name, h, row.CollectedAt.UTC(), bucket)
			}
			if row.DatabaseName != nil {
				t.Errorf("%s: historical database = %v, want NULL", s.name, *row.DatabaseName)
			}
			if row.SampleCount != 6 {
				t.Errorf("%s: bucket[%d] sample count = %d, want 6", s.name, h, row.SampleCount)
			}
			assertClose(t, s.name+" historical", row.Value, s.want[h])
		}
	}
}

// TestCheckpointsReqDelta_HistoricalRecoveryPerBucket checks that the
// recovery exclusion is judged per hour, by the newest role sample in
// that hour, as the latest query would have judged it at the end of the
// hour: hours spent in recovery are left out, the hour of a promotion is
// kept, and an hour with no role sample is kept. The session runs in a
// zone with a half-hour offset so a bucket cut in session time rather
// than UTC would split the hours.
func TestCheckpointsReqDelta_HistoricalRecoveryPerBucket(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastoreInTimeZone(t, "Asia/Kolkata")
	defer cleanup()

	const metric = "pg_stat_checkpointer.checkpoints_req_delta"
	connID := insertConnection(t, pool, "checkpoints-490-buckets")
	h0 := time.Now().UTC().Truncate(time.Hour).Add(-4 * time.Hour)
	inRecovery, primary := true, false

	at := func(hour int, minute int) time.Time {
		return h0.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
	}
	// Hour 0, a standby: excluded however many checkpoints it requests.
	insertCheckpointerSample(t, pool, connID, at(0, 10), 100, &inRecovery)
	insertCheckpointerSample(t, pool, connID, at(0, 40), 150, &inRecovery)
	// Hour 1, promoted at minute 35: the newest role sample is primary,
	// so the hour is kept. Its first sample has no predecessor inside the
	// hour, so only the +3 counts.
	insertCheckpointerSample(t, pool, connID, at(1, 5), 155, &inRecovery)
	insertCheckpointerSample(t, pool, connID, at(1, 35), 158, &primary)
	// Hour 2, no role sample: kept, with +1.
	insertCheckpointerSample(t, pool, connID, at(2, 20), 162, nil)
	insertCheckpointerSample(t, pool, connID, at(2, 50), 163, nil)

	got := historicalRowsFor(t, ds, metric, connID)
	want := []struct {
		bucket  time.Time
		value   float64
		samples int64
	}{{at(1, 0), 3, 2}, {at(2, 0), 1, 2}}
	if len(got) != len(want) {
		t.Fatalf("historical buckets = %+v, want %d", got, len(want))
	}
	for i, w := range want {
		if !got[i].CollectedAt.Equal(w.bucket) {
			t.Errorf("bucket[%d] = %v, want %v", i, got[i].CollectedAt.UTC(), w.bucket)
		}
		assertClose(t, "bucket value", got[i].Value, w.value)
		if got[i].SampleCount != w.samples {
			t.Errorf("bucket[%d] sample count = %d, want %d", i, got[i].SampleCount, w.samples)
		}
	}
}

// statementRow is one synthetic metrics.pg_stat_statements row.
type statementRow struct {
	db      string
	queryID int64
	userID  int64
	calls   int64
	totalMs float64
}

// insertStatementSample writes the rows of one pg_stat_statements probe
// run.
func insertStatementSample(t *testing.T, pool *pgxpool.Pool, connID int,
	at time.Time, rows []statementRow) {
	t.Helper()

	for _, r := range rows {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO metrics.pg_stat_statements
			    (connection_id, database_name, queryid, userid, calls,
			     total_exec_time, collected_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, connID, r.db, r.queryID, r.userID, r.calls, r.totalMs, at); err != nil {
			t.Fatalf("failed to insert pg_stat_statements row: %v", err)
		}
	}
}

// TestSlowQueryCount_HistoricalMatchesLatest checks that, for every
// sample and database, the historical query counts the same slow
// queryids the latest query counts when that sample is the newest:
// interval means rebuilt from consecutive samples of each statement
// identity, idle and reset statements skipped, and databases kept apart.
func TestSlowQueryCount_HistoricalMatchesLatest(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	const metric = "pg_stat_statements.slow_query_count"
	connID := insertConnection(t, pool, "slow-query-490")

	s1 := sampleInstant(12 * time.Minute)
	s2 := sampleInstant(7 * time.Minute)
	s3 := sampleInstant(2 * time.Minute)
	insertStatementSample(t, pool, connID, s1, []statementRow{
		{"alpha", 1, 10, 5, 21000},
		{"alpha", 2, 10, 1000, 50000},
		{"alpha", 3, 10, 10, 1000},
		{"beta", 1, 10, 100, 1000},
		{"beta", 1, 20, 1000, 50000},
	})
	insertStatementSample(t, pool, connID, s2, []statementRow{
		// queryid 1 idle; queryid 2 fast; queryid 3: 2 calls in 6 s.
		{"alpha", 1, 10, 5, 21000},
		{"alpha", 2, 10, 1100, 51000},
		{"alpha", 3, 10, 12, 7000},
		// beta: userid 10 runs 1 call in 3 s; userid 20 is reset.
		{"beta", 1, 10, 101, 4000},
		{"beta", 1, 20, 2, 6000},
	})
	insertStatementSample(t, pool, connID, s3, []statementRow{
		// queryid 1: 10 calls in 1 s; queryid 2: 2 calls in 5 s;
		// queryid 3 idle.
		{"alpha", 1, 10, 15, 22000},
		{"alpha", 2, 10, 1102, 56000},
		{"alpha", 3, 10, 12, 7000},
		// beta: userid 10 idle, userid 20 runs 2 calls in 4 s. Both
		// queries pool the identities of a queryid before taking the
		// mean, so a busy fast identity would hide the slow one.
		{"beta", 1, 10, 101, 4000},
		{"beta", 1, 20, 4, 10000},
	})

	// A second server runs the same statement identity, fast and with a
	// larger counter, a minute after each of the first server's samples.
	// An identity window without the connection would pair the first
	// server's queryid 3 with these rows, and its slow interval at s2
	// would vanish.
	otherID := insertConnection(t, pool, "slow-query-490-other")
	for i, at := range []time.Time{s1, s2, s3} {
		calls := int64(100 * (i + 1))
		insertStatementSample(t, pool, otherID, at.Add(time.Minute),
			[]statementRow{{"alpha", 3, 10, calls, float64(calls)}})
	}
	other := historicalRowsByKey(t, ds, metric, otherID)
	if len(other) != 3 {
		t.Fatalf("other server's historical rows = %+v, want one per sample", other)
	}
	for key, row := range other {
		assertClose(t, "other server historical at "+key.at.String(), row.Value, 0)
	}
	if got, ok := latestRowsFor(t, ds, metric, otherID)["alpha"]; !ok || got.Value != 0 {
		t.Errorf("other server's latest = %+v (present %v), want 0", got, ok)
	}

	want := map[historicalKey]float64{
		{"alpha", s1}: 0, {"alpha", s2}: 1, {"alpha", s3}: 1,
		{"beta", s1}: 0, {"beta", s2}: 1, {"beta", s3}: 1,
	}
	historical := historicalRowsByKey(t, ds, metric, connID)
	if len(historical) != len(want) {
		t.Fatalf("historical rows = %+v, want one per database and sample", historical)
	}
	for _, at := range []time.Time{s3, s2, s1} {
		latest := latestRowsFor(t, ds, metric, connID)
		for _, db := range []string{"alpha", "beta"} {
			row, ok := historical[historicalKey{db, at}]
			if !ok {
				t.Fatalf("no historical row for %s at %v", db, at)
			}
			assertClose(t, db+" historical", row.Value, want[historicalKey{db, at}])
			got, ok := latest[db]
			if !ok {
				t.Fatalf("no latest row for %s with the sample at %v newest", db, at)
			}
			assertClose(t, db+" latest", got.Value, row.Value)
		}
		deleteSample(t, pool, "pg_stat_statements", connID, at)
	}
}

// TestSlowQueryCount_HistoricalSkipsCollectionGap checks that an
// interval longer than the latest query's fifteen minute window is not
// counted, because the latest query can never see both of its samples:
// a slow interval across a gap would otherwise enter the baseline as a
// value no live sample can produce.
func TestSlowQueryCount_HistoricalSkipsCollectionGap(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	const metric = "pg_stat_statements.slow_query_count"
	connID := insertConnection(t, pool, "slow-query-490-gap")

	before := sampleInstant(40 * time.Minute)
	after := sampleInstant(2 * time.Minute)
	insertStatementSample(t, pool, connID, before, []statementRow{{"alpha", 1, 10, 10, 1000}})
	insertStatementSample(t, pool, connID, after, []statementRow{{"alpha", 1, 10, 12, 9000}})

	latest := latestRowsFor(t, ds, metric, connID)
	if got, ok := latest["alpha"]; !ok || got.Value != 0 {
		t.Errorf("latest = %+v, want a single row of 0", latest)
	}
	row, ok := historicalRowsByKey(t, ds, metric, connID)[historicalKey{"alpha", after}]
	if !ok {
		t.Fatalf("no historical row for the sample after the gap")
	}
	assertClose(t, "historical after the gap", row.Value, 0)
}
