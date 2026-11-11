/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// perDatabaseCounter is one pg_stat_statements identity in the issue #802
// fixtures: the dbid it ran in and the calls it made between the two
// collections that seedPerDatabaseCounters stores.
type perDatabaseCounter struct {
	dbID  int64
	calls int64
}

// seedPerDatabaseCounters stores one counter per entry of counters for the
// same queryid, under every probing database in dbidProbeDatabases, at two
// collections five minutes apart. Each counter grows by its calls, and by
// two milliseconds per call, between them. The latest collection is
// returned so that callers can record database names relative to it.
func seedPerDatabaseCounters(
	t *testing.T,
	pool *pgxpool.Pool,
	queryID int64,
	counters []perDatabaseCounter,
) time.Time {
	t.Helper()
	latest := time.Now().UTC().Add(-1 * time.Minute)
	for i, at := range []time.Time{latest.Add(-5 * time.Minute), latest} {
		for _, probeDB := range dbidProbeDatabases {
			for _, c := range counters {
				calls := c.calls * int64(i)
				if _, err := pool.Exec(context.Background(),
					`INSERT INTO metrics.pg_stat_statements
                    (connection_id, collected_at, queryid, userid, dbid,
                     database_name, query, calls, total_exec_time,
                     mean_exec_time, min_exec_time, max_exec_time, rows,
                     shared_blks_hit, shared_blks_read)
                    VALUES ($1, $2, $3, 10, $4, $5, 'BEGIN', $6, $7,
                            2, 1, 3, 0, 0, 0)`,
					topQueriesConnID, at, queryID, c.dbID, probeDB,
					calls, float64(2*calls)); err != nil {
					t.Fatalf("pg_stat_statements seed failed: %v", err)
				}
			}
		}
	}
	return latest
}

// assertPerDatabaseRows checks that the unfiltered list reports queryID
// once per database in want, with only that database's calls, and that
// both the list filtered to each database and the query-stats drill-down
// opened from each row agree with it.
func assertPerDatabaseRows(
	t *testing.T,
	h *PerfSummaryHandler,
	queryID string,
	want map[string]int64,
) {
	t.Helper()

	rows, total := decodeTopQueries(t, callTopQueries(t, h,
		"connection_id=4242&time_range=1h&queryid="+queryID))
	if len(rows) != len(want) || total != strconv.Itoa(len(want)) {
		t.Fatalf("unfiltered: total = %s, rows = %#v; want %d rows", total,
			rows, len(want))
	}
	seen := map[string]bool{}
	for _, row := range rows {
		wantCalls, ok := want[row.DatabaseName]
		if !ok || seen[row.DatabaseName] {
			t.Errorf("unfiltered: unexpected row %#v", row)
			continue
		}
		seen[row.DatabaseName] = true
		if row.Calls != wantCalls {
			t.Errorf("unfiltered %s: calls = %d, want %d", row.DatabaseName,
				row.Calls, wantCalls)
		}
	}

	for database, wantCalls := range want {
		t.Run(database, func(t *testing.T) {
			rows, total := decodeTopQueries(t, callTopQueries(t, h,
				"connection_id=4242&time_range=1h&queryid="+queryID+
					"&database_name="+database))
			if total != "1" || len(rows) != 1 {
				t.Fatalf("filtered: total = %s, rows = %#v; want one row",
					total, rows)
			}
			if rows[0].DatabaseName != database || rows[0].Calls != wantCalls {
				t.Errorf("filtered: %s:%d, want %s:%d", rows[0].DatabaseName,
					rows[0].Calls, database, wantCalls)
			}

			stats := decodeQueryStats(t, callQueryStats(t, h,
				"connection_id=4242&queryid="+queryID+
					"&database_name="+database))
			if stats.Calls != wantCalls {
				t.Errorf("query-stats: calls = %d, want %d", stats.Calls,
					wantCalls)
			}
		})
	}
}

// TestTopQueries_StatementInSeveralDatabases reproduces issue #802: BEGIN
// has the same queryid in every database, here the Workbench datastore
// (dbid 50, which sorts first) and a pgbench database (dbid 300). The list
// used to sum both counters under the queryid alone and label the row with
// the datastore, so pgbench's calls were reported under ai_workbench. Each
// database now has a row of its own holding only its own calls.
func TestTopQueries_StatementInSeveralDatabases(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	latest := seedPerDatabaseCounters(t, pool, 7001, []perDatabaseCounter{
		{dbID: 50, calls: 300},
		{dbID: 300, calls: 120},
	})
	seedStatDatabase(t, pool, latest, 50, "ai_workbench")
	seedStatDatabase(t, pool, latest, 300, "pgbench_db")

	assertPerDatabaseRows(t, h, "7001", map[string]int64{
		"ai_workbench": 300,
		"pgbench_db":   120,
	})
}

// TestTopQueries_UnresolvedDbidSharesItsFallbackRow covers a dbid that no
// name source resolves, whose fallback is the probing database alpha, run
// alongside the real alpha (dbid 100). Rows are keyed by the name they are
// reported under, so both counters land on the one alpha row, and the
// drill-down, which filters on the same resolved name, sums the same two
// counters rather than disagreeing with the row it was opened from.
func TestTopQueries_UnresolvedDbidSharesItsFallbackRow(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	latest := seedPerDatabaseCounters(t, pool, 7002, []perDatabaseCounter{
		{dbID: 100, calls: 30},
		{dbID: 400, calls: 12},
		{dbID: 300, calls: 5},
	})
	seedStatDatabase(t, pool, latest, 100, "alpha")
	seedStatDatabase(t, pool, latest, 300, "gamma")

	assertPerDatabaseRows(t, h, "7002", map[string]int64{
		"alpha": 42,
		"gamma": 5,
	})
}

// TestTopQueries_PerDatabaseRowsPage checks that paging over a statement
// reported in several databases is a total order: the database name breaks
// the tie between rows sharing a queryid, so successive pages of one row
// each return every row exactly once.
func TestTopQueries_PerDatabaseRowsPage(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	latest := seedPerDatabaseCounters(t, pool, 7003, []perDatabaseCounter{
		{dbID: 100, calls: 10},
		{dbID: 200, calls: 10},
		{dbID: 300, calls: 10},
	})
	seedStatDatabase(t, pool, latest, 100, "alpha")
	seedStatDatabase(t, pool, latest, 200, "beta")
	seedStatDatabase(t, pool, latest, 300, "gamma")

	var got []string
	for offset := 0; offset < 3; offset++ {
		rows, total := decodeTopQueries(t, callTopQueries(t, h,
			"connection_id=4242&time_range=1h&limit=1&offset="+strconv.Itoa(offset)))
		if total != "3" || len(rows) != 1 {
			t.Fatalf("offset %d: total = %s, rows = %#v; want one of three",
				offset, total, rows)
		}
		got = append(got, rows[0].DatabaseName)
	}
	if got[0] != "alpha" || got[1] != "beta" || got[2] != "gamma" {
		t.Errorf("pages = %v, want [alpha beta gamma]", got)
	}
}
