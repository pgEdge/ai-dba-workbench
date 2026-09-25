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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// dbidProbeDatabases are the probing databases in the issue #508
// fixtures. The pg_stat_statements probe runs in each of them and stores
// every counter once per database.
var dbidProbeDatabases = []string{"alpha", "postgres"}

// seedProbedCounter stores one pg_stat_statements counter for the given
// queryid and dbid under every probing database, at two collections five
// minutes apart, growing by 120 calls and 240 ms between them. The latest
// collection is returned so that callers can place name observations
// relative to it.
func seedProbedCounter(
	t *testing.T,
	pool *pgxpool.Pool,
	queryID, dbID int64,
) time.Time {
	t.Helper()
	latest := time.Now().UTC().Add(-1 * time.Minute)
	for i, at := range []time.Time{latest.Add(-5 * time.Minute), latest} {
		for _, probeDB := range dbidProbeDatabases {
			if _, err := pool.Exec(context.Background(),
				`INSERT INTO metrics.pg_stat_statements
                (connection_id, collected_at, queryid, userid, dbid,
                 database_name, query, calls, total_exec_time,
                 mean_exec_time, min_exec_time, max_exec_time, rows,
                 shared_blks_hit, shared_blks_read)
                VALUES ($1, $2, $3, 10, $4, $5, 'SELECT unobserved', $6, $7,
                        2, 1, 3, 0, 0, 0)`,
				topQueriesConnID, at, queryID, dbID, probeDB,
				int64(120*i), float64(240*i)); err != nil {
				t.Fatalf("pg_stat_statements seed failed: %v", err)
			}
		}
	}
	return latest
}

// seedStatDatabase records a pg_stat_database row for datid under its own
// name, as the database-scoped probe does, at the given collection.
func seedStatDatabase(
	t *testing.T,
	pool *pgxpool.Pool,
	collectedAt time.Time,
	datID int64,
	datName string,
) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, database_name, datid, datname)
        VALUES ($1, $2, $3, $4, $3)`,
		topQueriesConnID, collectedAt, datName, datID); err != nil {
		t.Fatalf("pg_stat_database seed failed: %v", err)
	}
}

// seedActivityDatabase records a pg_stat_activity sample naming datid.
func seedActivityDatabase(
	t *testing.T,
	pool *pgxpool.Pool,
	collectedAt time.Time,
	datID int64,
	datName string,
) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO metrics.pg_stat_activity
        (connection_id, collected_at, datid, datname, usesysid, usename)
        VALUES ($1, $2, $3, $4, 10, 'alice')`,
		topQueriesConnID, collectedAt, datID, datName); err != nil {
		t.Fatalf("pg_stat_activity seed failed: %v", err)
	}
}

// dbidExpectation is what the top-queries list and the query-stats
// drill-down should both report for one database_name filter.
type dbidExpectation struct {
	filter    string
	wantCalls int64
}

// assertDbidAttribution checks that the unfiltered list reports queryID
// once, under wantDatabase and with its 120 calls counted once, that the
// unfiltered drill-down agrees, and that each filter in cases returns the
// expected calls from both endpoints.
func assertDbidAttribution(
	t *testing.T,
	h *PerfSummaryHandler,
	queryID, wantDatabase string,
	cases []dbidExpectation,
) {
	t.Helper()

	rows, total := decodeTopQueries(t, callTopQueries(t, h,
		"connection_id=4242&time_range=1h&queryid="+queryID))
	if total != "1" || len(rows) != 1 {
		t.Fatalf("unfiltered: total = %s, rows = %#v; want one row",
			total, rows)
	}
	if rows[0].DatabaseName != wantDatabase || rows[0].Calls != 120 {
		t.Errorf("unfiltered: database_name = %q, calls = %d; want %q, 120",
			rows[0].DatabaseName, rows[0].Calls, wantDatabase)
	}
	if rows[0].Query != "SELECT unobserved" {
		t.Errorf("unfiltered: query = %q, want the sampled text",
			rows[0].Query)
	}

	stats := decodeQueryStats(t, callQueryStats(t, h,
		"connection_id=4242&queryid="+queryID))
	if stats.Calls != 120 {
		t.Errorf("query-stats unfiltered: calls = %d, want 120 (one copy "+
			"per collection)", stats.Calls)
	}

	for _, tc := range cases {
		t.Run(tc.filter, func(t *testing.T) {
			rows, total := decodeTopQueries(t, callTopQueries(t, h,
				"connection_id=4242&time_range=1h&database_name="+tc.filter))
			var gotCalls int64
			for _, row := range rows {
				if row.QueryID == queryID {
					gotCalls = row.Calls
				}
			}
			if gotCalls != tc.wantCalls {
				t.Errorf("top-queries: calls = %d, want %d (total %s, rows "+
					"%#v)", gotCalls, tc.wantCalls, total, rows)
			}

			stats := decodeQueryStats(t, callQueryStats(t, h,
				"connection_id=4242&queryid="+queryID+
					"&database_name="+tc.filter))
			if stats.Calls != tc.wantCalls {
				t.Errorf("query-stats: calls = %d, want %d", stats.Calls,
					tc.wantCalls)
			}
		})
	}
}

// TestTopQueries_DbidResolvedFromPgStatDatabase reproduces issue #508: a
// counter for dbid 300, never observed in pg_stat_activity and stored under
// both probing databases, used to be returned for database_name=postgres
// and for database_name=alpha alike. pg_stat_database now names dbid 300,
// so the counter belongs to gamma alone, and the drill-down finds it there
// even though no copy was probed through gamma.
func TestTopQueries_DbidResolvedFromPgStatDatabase(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	latest := seedProbedCounter(t, pool, 6001, 300)
	seedStatDatabase(t, pool, latest, 300, "gamma")
	seedStatDatabase(t, pool, latest, 100, "alpha")

	assertDbidAttribution(t, h, "6001", "gamma", []dbidExpectation{
		{"gamma", 120},
		{"alpha", 0},
		{"postgres", 0},
	})
}

// TestTopQueries_UnresolvedDbidMatchesOneDatabase covers the residual case
// of issue #508: a dbid that neither pg_stat_database nor pg_stat_activity
// names, such as a database the monitoring role cannot connect to. It still
// falls back to a probing database, but to one only, the lowest name, so a
// filtered view agrees with the unfiltered one rather than reporting the
// counter under every probing database.
func TestTopQueries_UnresolvedDbidMatchesOneDatabase(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	latest := seedProbedCounter(t, pool, 6002, 400)
	seedStatDatabase(t, pool, latest, 100, "alpha")

	assertDbidAttribution(t, h, "6002", "alpha", []dbidExpectation{
		{"alpha", 120},
		{"postgres", 0},
	})
}

// TestTopQueries_DbidLookupSources pins how db_names combines its two
// sources: pg_stat_activity still names a database pg_stat_database never
// saw, the most recent observation wins across the two tables, and rows
// older than the lookup window are ignored.
func TestTopQueries_DbidLookupSources(t *testing.T) {
	tests := []struct {
		name         string
		seed         func(t *testing.T, pool *pgxpool.Pool, latest time.Time)
		wantDatabase string
	}{
		{
			name: "pg_stat_activity names a database the probe never ran in",
			seed: func(t *testing.T, pool *pgxpool.Pool, latest time.Time) {
				seedActivityDatabase(t, pool, latest, 300, "restricted")
			},
			wantDatabase: "restricted",
		},
		{
			name: "newer pg_stat_database name beats older activity name",
			seed: func(t *testing.T, pool *pgxpool.Pool, latest time.Time) {
				seedActivityDatabase(t, pool, latest.Add(-10*time.Minute),
					300, "gamma_old")
				seedStatDatabase(t, pool, latest, 300, "gamma")
			},
			wantDatabase: "gamma",
		},
		{
			name: "newer activity name beats older pg_stat_database name",
			seed: func(t *testing.T, pool *pgxpool.Pool, latest time.Time) {
				seedStatDatabase(t, pool, latest.Add(-10*time.Minute),
					300, "gamma_old")
				seedActivityDatabase(t, pool, latest, 300, "gamma")
			},
			wantDatabase: "gamma",
		},
		{
			name: "pg_stat_database row outside the lookup window is ignored",
			seed: func(t *testing.T, pool *pgxpool.Pool, latest time.Time) {
				seedStatDatabase(t, pool, latest.Add(-2*time.Hour),
					300, "gamma")
			},
			wantDatabase: "alpha",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, pool, cleanup := newTopQueriesTestHandler(t)
			defer cleanup()
			latest := seedProbedCounter(t, pool, 6003, 300)
			tc.seed(t, pool, latest)

			rows, total := decodeTopQueries(t, callTopQueries(t, h,
				"connection_id=4242&time_range=1h&queryid=6003"))
			if total != "1" || len(rows) != 1 {
				t.Fatalf("total = %s, rows = %#v; want one row", total, rows)
			}
			if rows[0].DatabaseName != tc.wantDatabase {
				t.Errorf("database_name = %q, want %q", rows[0].DatabaseName,
					tc.wantDatabase)
			}
		})
	}
}

// TestStatementDatabaseLookupSQL checks the shared lookup text: it reads
// both name sources, bounds each to the lookup window, and carries no
// format verbs, since it is spliced into format strings as an argument.
func TestStatementDatabaseLookupSQL(t *testing.T) {
	for _, want := range []string{
		"FROM metrics.pg_stat_database",
		"FROM metrics.pg_stat_activity",
	} {
		if !strings.Contains(statementDatabaseLookupSQL, want) {
			t.Errorf("lookup SQL lacks %q:\n%s", want,
				statementDatabaseLookupSQL)
		}
	}
	if got := strings.Count(statementDatabaseLookupSQL,
		"INTERVAL '"+nameLookupWindowSQL+"'"); got != 2 {
		t.Errorf("lookup window appears %d times, want 2", got)
	}
	if strings.Contains(statementDatabaseLookupSQL, "%") {
		t.Errorf("lookup SQL contains a format verb:\n%s",
			statementDatabaseLookupSQL)
	}
}
