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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// topQueriesTestSchema mirrors the minimum columns handleTopQueries reads
// from the metrics schema. The tables live in the metrics schema because the
// query fully qualifies its table names with metrics.*.
const topQueriesTestSchema = `
CREATE SCHEMA IF NOT EXISTS metrics;
DROP TABLE IF EXISTS metrics.pg_stat_statements CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_activity CASCADE;

CREATE TABLE metrics.pg_stat_statements (
    connection_id     integer     NOT NULL,
    database_name     text        NOT NULL,
    dbid              oid         NOT NULL DEFAULT 0,
    queryid           bigint      NOT NULL,
    query             text,
    calls             bigint,
    rows              bigint,
    total_exec_time   double precision,
    mean_exec_time    double precision,
    shared_blks_hit   bigint,
    shared_blks_read  bigint,
    collected_at      timestamptz NOT NULL
);

CREATE TABLE metrics.pg_stat_activity (
    connection_id  integer     NOT NULL,
    collected_at   timestamptz NOT NULL,
    datid          oid,
    datname        text
);
`

const topQueriesTestSchemaTeardown = `
DROP TABLE IF EXISTS metrics.pg_stat_statements CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_activity CASCADE;
`

// newTopQueriesTestPool connects to the TEST_AI_WORKBENCH_SERVER Postgres
// instance and installs the trimmed metrics schema above. The test is
// skipped when the environment is not configured for database-backed tests.
func newTopQueriesTestPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping issue #364 test")
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

	if _, err := pool.Exec(ctx, topQueriesTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create top-queries test schema: %v", err)
	}

	cleanup := func() {
		_, _ = pool.Exec(context.Background(), topQueriesTestSchemaTeardown)
		pool.Close()
	}
	return pool, cleanup
}

// topQueryFixture describes one seeded pg_stat_statements row and whether the
// exclude_collector filter is expected to hide it.
type topQueryFixture struct {
	queryid          int64
	query            string
	excludedByToggle bool
}

// topQueriesFixtures is the seed set covering every branch of the exclusion
// clause plus genuine user traffic that must never be filtered.
var topQueriesFixtures = []topQueryFixture{
	{
		// Collector probe SELECT: wrapped in the ai_dba_wb_probe marker.
		queryid: 1,
		query: "SELECT 'ai_dba_wb_probe' AS ai_dba_wb_probe, subq.* " +
			"FROM (SELECT * FROM pg_stat_activity) AS subq",
		excludedByToggle: true,
	},
	{
		// Collector metrics-store write: pgx.Identifier quotes both the
		// schema and table (metrics.pg_stat_statements).
		queryid: 2,
		query: `INSERT INTO "metrics"."pg_stat_statements" ` +
			`("connection_id", "query") VALUES ($1, $2)`,
		excludedByToggle: true,
	},
	{
		// Collector metrics-store write into a spock_* table.
		queryid: 3,
		query: `INSERT INTO "metrics"."spock_exception_log" ` +
			`("connection_id") VALUES ($1)`,
		excludedByToggle: true,
	},
	{
		// Alerter slow_query_count read: unquoted metrics.pg_stat_statements.
		queryid: 4,
		query: "WITH recent_statements AS (SELECT connection_id, queryid " +
			"FROM metrics.pg_stat_statements WHERE collected_at > NOW()) " +
			"SELECT count(*) FROM recent_statements",
		excludedByToggle: true,
	},
	{
		// Alerter age_percent read: unquoted metrics.pg_settings.
		queryid: 5,
		query: "WITH freeze_settings AS (SELECT connection_id " +
			"FROM metrics.pg_settings WHERE name = $1) " +
			"SELECT * FROM freeze_settings",
		excludedByToggle: true,
	},
	{
		// Genuine user query on a monitored database: no relation to the
		// metrics schema and no probe marker. Must never be filtered.
		queryid:          6,
		query:            "SELECT * FROM orders WHERE customer_id = $1",
		excludedByToggle: false,
	},
	{
		// Genuine user query against an external application schema that
		// happens to be named "metrics" but whose table is not a pg_/spock_
		// internal table. Must never be filtered (false-positive guard).
		queryid:          7,
		query:            "SELECT count(*) FROM metrics.events WHERE ts > $1",
		excludedByToggle: false,
	},
	{
		// External table whose name starts with "pgx"; the escaped
		// underscore in the LIKE pattern means metrics.pg_ does not match
		// metrics.pgx_data, so this genuine query is not filtered.
		queryid:          8,
		query:            "SELECT * FROM metrics.pgx_data WHERE id = $1",
		excludedByToggle: false,
	},
}

// seedTopQueriesFixtures inserts every fixture row at the same latest
// collected_at so the handler's MAX(collected_at) filter keeps all of them.
func seedTopQueriesFixtures(
	t *testing.T, pool *pgxpool.Pool, connID int, collectedAt time.Time,
) {
	t.Helper()
	ctx := context.Background()
	for _, f := range topQueriesFixtures {
		if _, err := pool.Exec(ctx, `
            INSERT INTO metrics.pg_stat_statements
                (connection_id, database_name, dbid, queryid, query, calls,
                 rows, total_exec_time, mean_exec_time, shared_blks_hit,
                 shared_blks_read, collected_at)
            VALUES ($1, 'ai_workbench', 0, $2, $3, 10, 5, 100.0, 10.0, 1, 1,
                    $4)`,
			connID, f.queryid, f.query, collectedAt); err != nil {
			t.Fatalf("seed statement %d failed: %v", f.queryid, err)
		}
	}
}

// runTopQueries executes the exact SQL that handleTopQueries builds for the
// given exclude_collector setting and returns the set of queries returned.
func runTopQueries(
	t *testing.T, pool *pgxpool.Pool, connID int, excludeCollector bool,
) map[string]bool {
	t.Helper()
	ctx := context.Background()

	query, args := buildTopQueriesQuery(
		connID, 100, "", "total_exec_time", "desc", excludeCollector)

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after read is a no-op

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		t.Fatalf("top queries query failed: %v", err)
	}
	defer rows.Close()

	got := make(map[string]bool)
	for rows.Next() {
		var r TopQueryRow
		if err := rows.Scan(
			&r.QueryID, &r.DatabaseName, &r.Query, &r.Calls,
			&r.TotalExecTime, &r.MeanExecTime, &r.Rows,
			&r.SharedBlksHit, &r.SharedBlksRead,
		); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		got[r.Query] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("row iteration failed: %v", err)
	}
	return got
}

// TestTopQueries_Issue364_ExcludeCollector verifies that with the
// exclude_collector toggle on, all three categories of Workbench-internal
// traffic (collector probe SELECTs, collector metrics-store writes, and
// alerter/server metrics-store reads) are filtered, while genuine user
// queries -- including those against an unrelated external schema named
// "metrics" -- are preserved.
func TestTopQueries_Issue364_ExcludeCollector(t *testing.T) {
	pool, cleanup := newTopQueriesTestPool(t)
	defer cleanup()

	const connID = 91
	collectedAt := time.Now().UTC().Add(-time.Minute)
	seedTopQueriesFixtures(t, pool, connID, collectedAt)

	got := runTopQueries(t, pool, connID, true)

	for _, f := range topQueriesFixtures {
		present := got[f.query]
		if f.excludedByToggle && present {
			t.Errorf("query %d should be excluded but was returned: %q",
				f.queryid, f.query)
		}
		if !f.excludedByToggle && !present {
			t.Errorf("query %d should be returned but was excluded: %q",
				f.queryid, f.query)
		}
	}
}

// TestTopQueries_Issue364_NoExcludeReturnsAll verifies that without the
// toggle every seeded row is returned, so the filter never fires unless
// explicitly requested.
func TestTopQueries_Issue364_NoExcludeReturnsAll(t *testing.T) {
	pool, cleanup := newTopQueriesTestPool(t)
	defer cleanup()

	const connID = 92
	collectedAt := time.Now().UTC().Add(-time.Minute)
	seedTopQueriesFixtures(t, pool, connID, collectedAt)

	got := runTopQueries(t, pool, connID, false)

	if len(got) != len(topQueriesFixtures) {
		t.Fatalf("expected all %d rows without toggle, got %d: %#v",
			len(topQueriesFixtures), len(got), got)
	}
	for _, f := range topQueriesFixtures {
		if !got[f.query] {
			t.Errorf("query %d missing when filter disabled: %q",
				f.queryid, f.query)
		}
	}
}

// TestTopQueries_Issue364_HandlerHTTP drives handleTopQueries end-to-end
// over HTTP with a nil authStore (which makes the RBAC checker treat the
// caller as fully authorized) to prove the exclude_collector query parameter
// filters Workbench-internal traffic through the real handler, not just the
// extracted query builder.
func TestTopQueries_Issue364_HandlerHTTP(t *testing.T) {
	pool, cleanup := newTopQueriesTestPool(t)
	defer cleanup()

	const connID = 93
	collectedAt := time.Now().UTC().Add(-time.Minute)
	seedTopQueriesFixtures(t, pool, connID, collectedAt)

	h := NewPerfSummaryHandler(database.NewTestDatastore(pool), nil)

	do := func(exclude string) []TopQueryRow {
		url := "/api/v1/metrics/top-queries?connection_id=" +
			strconv.Itoa(connID) + "&limit=100"
		if exclude != "" {
			url += "&exclude_collector=" + exclude
		}
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		h.handleTopQueries(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var out []TopQueryRow
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v (body %s)", err, rec.Body.String())
		}
		return out
	}

	// With the toggle on, internal traffic must be gone but user queries
	// must survive.
	filtered := do("true")
	seen := make(map[string]bool)
	for _, r := range filtered {
		seen[r.Query] = true
	}
	for _, f := range topQueriesFixtures {
		if f.excludedByToggle && seen[f.query] {
			t.Errorf("HTTP: query %d should be excluded: %q",
				f.queryid, f.query)
		}
		if !f.excludedByToggle && !seen[f.query] {
			t.Errorf("HTTP: query %d should be present: %q",
				f.queryid, f.query)
		}
	}

	// With the toggle absent, every seeded row must be returned.
	all := do("")
	if len(all) != len(topQueriesFixtures) {
		t.Fatalf("HTTP without toggle: expected %d rows, got %d",
			len(topQueriesFixtures), len(all))
	}

	// Exercise the limit-clamp branches: 0 clamps up to 1, and an
	// over-large value clamps down to 100.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/top-queries?connection_id="+strconv.Itoa(connID)+
			"&limit=0", nil)
	rec := httptest.NewRecorder()
	h.handleTopQueries(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("limit=0 status = %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/top-queries?connection_id="+strconv.Itoa(connID)+
			"&limit=500", nil)
	rec = httptest.NewRecorder()
	h.handleTopQueries(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("limit=500 status = %d", rec.Code)
	}
}

// TestTopQueries_HandlerQueryError verifies that when the underlying query
// fails (metrics tables missing) the handler responds 200 with an empty list
// rather than an error, matching its defensive "no data" contract.
func TestTopQueries_HandlerQueryError(t *testing.T) {
	pool, cleanup := newTopQueriesTestPool(t)
	defer cleanup()

	// Drop the metrics tables so the top-queries query fails.
	if _, err := pool.Exec(context.Background(),
		topQueriesTestSchemaTeardown); err != nil {
		t.Fatalf("teardown failed: %v", err)
	}

	h := NewPerfSummaryHandler(database.NewTestDatastore(pool), nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/top-queries?connection_id=1", nil)
	rec := httptest.NewRecorder()
	h.handleTopQueries(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var out []TopQueryRow
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty result on query error, got %d", len(out))
	}
}

// TestTopQueries_HandlerScanError verifies that a row which fails to scan (a
// NULL in a NOT NULL destination column) is skipped rather than aborting the
// response.
func TestTopQueries_HandlerScanError(t *testing.T) {
	pool, cleanup := newTopQueriesTestPool(t)
	defer cleanup()

	const connID = 94
	collectedAt := time.Now().UTC().Add(-time.Minute)
	// calls is NULL, so scanning into the int64 Calls field fails.
	if _, err := pool.Exec(context.Background(), `
        INSERT INTO metrics.pg_stat_statements
            (connection_id, database_name, dbid, queryid, query, calls,
             rows, total_exec_time, mean_exec_time, shared_blks_hit,
             shared_blks_read, collected_at)
        VALUES ($1, 'ai_workbench', 0, 1, 'SELECT 1', NULL, 5, 1.0, 1.0, 1,
                1, $2)`, connID, collectedAt); err != nil {
		t.Fatalf("seed bad row: %v", err)
	}

	h := NewPerfSummaryHandler(database.NewTestDatastore(pool), nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/top-queries?connection_id="+strconv.Itoa(connID), nil)
	rec := httptest.NewRecorder()
	h.handleTopQueries(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out []TopQueryRow
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("row that fails to scan must be skipped, got %d", len(out))
	}
}

// TestTopQueries_HandlerBeginTxError verifies the handler returns 500 when a
// read-only transaction cannot be started (here, a closed pool).
func TestTopQueries_HandlerBeginTxError(t *testing.T) {
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping issue #364 test")
	}
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		t.Skipf("connect: %v", err)
	}
	pool.Close() // BeginTx on a closed pool fails.

	h := NewPerfSummaryHandler(database.NewTestDatastore(pool), nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/top-queries?connection_id=1", nil)
	rec := httptest.NewRecorder()
	h.handleTopQueries(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)",
			rec.Code, rec.Body.String())
	}
}

// TestTopQueries_HandlerValidationErrors covers the request-validation
// branches of handleTopQueries that reject the request before any database
// access, so they need no datastore.
func TestTopQueries_HandlerValidationErrors(t *testing.T) {
	h := &PerfSummaryHandler{}
	cases := []struct {
		name   string
		method string
		url    string
		want   int
	}{
		{
			name:   "non-GET rejected",
			method: http.MethodPost,
			url:    "/api/v1/metrics/top-queries?connection_id=1",
			want:   http.StatusMethodNotAllowed,
		},
		{
			name:   "missing connection id",
			method: http.MethodGet,
			url:    "/api/v1/metrics/top-queries",
			want:   http.StatusBadRequest,
		},
		{
			name:   "multiple connection ids rejected",
			method: http.MethodGet,
			url:    "/api/v1/metrics/top-queries?connection_ids=1,2",
			want:   http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.url, nil)
			rec := httptest.NewRecorder()
			h.handleTopQueries(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)",
					rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestTopQueries_HandlerParamValidationDB covers the parameter-validation
// branches that run after the RBAC check and therefore need a datastore; a
// nil authStore authorizes the caller so validation is reached.
func TestTopQueries_HandlerParamValidationDB(t *testing.T) {
	pool, cleanup := newTopQueriesTestPool(t)
	defer cleanup()

	h := NewPerfSummaryHandler(database.NewTestDatastore(pool), nil)
	cases := []struct {
		name string
		url  string
		want int
	}{
		{
			name: "invalid limit",
			url:  "/api/v1/metrics/top-queries?connection_id=1&limit=abc",
			want: http.StatusBadRequest,
		},
		{
			name: "invalid order_by",
			url:  "/api/v1/metrics/top-queries?connection_id=1&order_by=drop",
			want: http.StatusBadRequest,
		},
		{
			name: "invalid order",
			url:  "/api/v1/metrics/top-queries?connection_id=1&order=sideways",
			want: http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()
			h.handleTopQueries(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)",
					rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestBuildTopQueriesQuery_ClauseSelection asserts, without touching a
// database, that the exclusion fragment is present only when requested and
// that the optional queryid filter is bound as a positional parameter rather
// than interpolated.
func TestBuildTopQueriesQuery_ClauseSelection(t *testing.T) {
	// Without exclusion the internal clause must be absent.
	q, args := buildTopQueriesQuery(1, 10, "", "calls", "asc", false)
	if strings.Contains(q, "ai_dba_wb_probe") {
		t.Errorf("exclusion clause present when not requested")
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}

	// With exclusion the full internal clause must be spliced in.
	q, _ = buildTopQueriesQuery(1, 10, "", "calls", "asc", true)
	for _, frag := range []string{
		"ai_dba_wb_probe",
		`metrics.pg\_`,
		`metrics.spock\_`,
		`"metrics"."pg\_`,
		`"metrics"."spock\_`,
	} {
		if !strings.Contains(q, frag) {
			t.Errorf("exclusion clause missing fragment %q", frag)
		}
	}

	// A queryid filter must be added as a positional parameter.
	q, args = buildTopQueriesQuery(1, 10, "42", "calls", "asc", false)
	if !strings.Contains(q, "pss.queryid::text = $3") {
		t.Errorf("queryid filter not bound positionally: %q", q)
	}
	if len(args) != 3 || args[2] != "42" {
		t.Errorf("queryid not appended to args: %#v", args)
	}
}
