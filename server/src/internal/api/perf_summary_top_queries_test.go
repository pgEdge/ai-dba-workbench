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

// testMaintenanceDBName is the configured maintenance (datastore) database
// name the tests thread into the handler. Rows attributed to this database
// represent the Workbench's own traffic and must be excluded when
// exclude_collector is set, regardless of query-text shape (issue #366).
const testMaintenanceDBName = "ai_workbench"

// testMonitoredDBName is a genuinely-monitored database whose rows must
// survive the exclude_collector filter unless individually marked as a
// collector probe.
const testMonitoredDBName = "northwind"

// topQueryFixture describes one seeded pg_stat_statements row, the database
// it is attributed to, and whether the exclude_collector filter is expected
// to hide it.
type topQueryFixture struct {
	queryid          int64
	databaseName     string
	query            string
	excludedByToggle bool
}

// topQueriesFixtures is the seed set covering the database-identity
// exclusion (all maintenance-database traffic, regardless of query text),
// the surviving probe-marker exclusion (marker-tagged probes against a
// different monitored database), and genuine user traffic that must never
// be filtered -- including monitored-database queries whose text
// superficially resembles internal metrics access.
var topQueriesFixtures = []topQueryFixture{
	{
		// New unfilterable case #1 from issue #366: a schema-introspection
		// query whose schema/table names arrive as bound parameters, so no
		// query-text pattern could ever match it. Caught only by the
		// database-identity check because it runs on the maintenance DB.
		queryid:      1,
		databaseName: testMaintenanceDBName,
		query: "SELECT column_name, data_type FROM " +
			"information_schema.columns WHERE table_schema = $2 " +
			"AND table_name = $1 ORDER BY ordinal_position",
		excludedByToggle: true,
	},
	{
		// New unfilterable case #2 from issue #366: a third quoting style
		// (unquoted schema, quoted table) that #365's patterns did not
		// anticipate. Caught by the database-identity check.
		queryid:      2,
		databaseName: testMaintenanceDBName,
		query: `WITH data_buckets AS (SELECT date_bin('1 hour', ` +
			`collected_at, now()) AS bucket FROM ` +
			`metrics."pg_stat_statements") SELECT * FROM data_buckets`,
		excludedByToggle: true,
	},
	{
		// Collector metrics-store write: pgx.Identifier quotes both the
		// schema and table. Now caught by the database-identity check.
		queryid:      3,
		databaseName: testMaintenanceDBName,
		query: `INSERT INTO "metrics"."pg_stat_statements" ` +
			`("connection_id", "query") VALUES ($1, $2)`,
		excludedByToggle: true,
	},
	{
		// Alerter slow_query_count read: unquoted metrics.pg_stat_statements.
		// Now caught by the database-identity check.
		queryid:      4,
		databaseName: testMaintenanceDBName,
		query: "WITH recent_statements AS (SELECT connection_id, queryid " +
			"FROM metrics.pg_stat_statements WHERE collected_at > NOW()) " +
			"SELECT count(*) FROM recent_statements",
		excludedByToggle: true,
	},
	{
		// Collector probe SELECT wrapped in the ai_dba_wb_probe marker,
		// running against a DIFFERENT monitored database. The
		// database-identity check cannot catch it (its database_name is not
		// the maintenance DB), so the surviving probe-marker clause must.
		queryid:      5,
		databaseName: testMonitoredDBName,
		query: "SELECT 'ai_dba_wb_probe' AS ai_dba_wb_probe, subq.* " +
			"FROM (SELECT * FROM pg_stat_activity) AS subq",
		excludedByToggle: true,
	},
	{
		// Genuine user query on a monitored database: no relation to the
		// metrics schema and no probe marker. Must never be filtered.
		queryid:          6,
		databaseName:     testMonitoredDBName,
		query:            "SELECT * FROM orders WHERE customer_id = $1",
		excludedByToggle: false,
	},
	{
		// Genuine user query on a monitored database against an application
		// schema that happens to be named "metrics". Must never be filtered.
		queryid:          7,
		databaseName:     testMonitoredDBName,
		query:            "SELECT count(*) FROM metrics.events WHERE ts > $1",
		excludedByToggle: false,
	},
	{
		// Genuine user query on a monitored database whose text is
		// indistinguishable from internal metrics access
		// (metrics."pg_stat_statements"). Under the old text-pattern
		// approach this would have been wrongly filtered; the
		// database-identity approach correctly preserves it because it is
		// attributed to a monitored database, not the maintenance DB.
		queryid:      8,
		databaseName: testMonitoredDBName,
		query: `SELECT * FROM metrics."pg_stat_statements" ` +
			`WHERE queryid = $1`,
		excludedByToggle: false,
	},
}

// seedTopQueriesFixtures inserts every fixture row at the same latest
// collected_at so the handler's MAX(collected_at) filter keeps all of them.
// Each row is attributed to its fixture's database so the database-identity
// exclusion can be exercised.
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
            VALUES ($1, $2, 0, $3, $4, 10, 5, 100.0, 10.0, 1, 1,
                    $5)`,
			connID, f.databaseName, f.queryid, f.query,
			collectedAt); err != nil {
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
		connID, 100, "", "total_exec_time", "desc",
		testMaintenanceDBName, excludeCollector)

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
// exclude_collector toggle on, every category of Workbench-internal
// traffic is filtered: all traffic attributed to the maintenance database
// (collector metrics-store writes, alerter/server metrics-store reads, and
// -- per issue #366 -- parameterized schema-introspection queries and
// mixed-quoting metrics access that no text pattern could catch), plus
// marker-tagged collector probes running against a different monitored
// database. Genuine user queries on a monitored database are preserved,
// including one whose text is indistinguishable from internal metrics
// access, proving the filter keys on database identity rather than text.
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

	h := NewPerfSummaryHandler(
		database.NewTestDatastore(pool), nil, testMaintenanceDBName)

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

	h := NewPerfSummaryHandler(
		database.NewTestDatastore(pool), nil, testMaintenanceDBName)
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

	h := NewPerfSummaryHandler(
		database.NewTestDatastore(pool), nil, testMaintenanceDBName)
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

	h := NewPerfSummaryHandler(
		database.NewTestDatastore(pool), nil, testMaintenanceDBName)
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

	h := NewPerfSummaryHandler(
		database.NewTestDatastore(pool), nil, testMaintenanceDBName)
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
// database, that the probe-marker and database-identity exclusions are
// present only when requested, that the maintenance database name is bound
// as a positional parameter (never interpolated), and that the optional
// queryid filter is likewise bound positionally.
func TestBuildTopQueriesQuery_ClauseSelection(t *testing.T) {
	const identityFrag = "COALESCE(dn.datname, pss.database_name) <>"

	// Without exclusion neither clause must be present.
	q, args := buildTopQueriesQuery(
		1, 10, "", "calls", "asc", testMaintenanceDBName, false)
	if strings.Contains(q, "ai_dba_wb_probe") {
		t.Errorf("probe-marker clause present when not requested")
	}
	if strings.Contains(q, identityFrag) {
		t.Errorf("database-identity clause present when not requested")
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}

	// With exclusion and a maintenance DB name, both the probe-marker
	// clause and the database-identity clause must be spliced in, and the
	// maintenance name must be bound as $3 rather than interpolated.
	q, args = buildTopQueriesQuery(
		1, 10, "", "calls", "asc", testMaintenanceDBName, true)
	if !strings.Contains(q, "ai_dba_wb_probe") {
		t.Errorf("probe-marker clause missing when requested")
	}
	if !strings.Contains(q, identityFrag+" $3") {
		t.Errorf("database-identity clause not bound at $3: %q", q)
	}
	if strings.Contains(q, testMaintenanceDBName) {
		t.Errorf("maintenance DB name interpolated into query text: %q", q)
	}
	if len(args) != 3 || args[2] != testMaintenanceDBName {
		t.Errorf("maintenance DB name not bound positionally: %#v", args)
	}

	// The removed text-pattern fragments must no longer appear.
	for _, frag := range []string{
		`metrics.pg\_`,
		`metrics.spock\_`,
		`"metrics"."pg\_`,
		`"metrics"."spock\_`,
	} {
		if strings.Contains(q, frag) {
			t.Errorf("obsolete text-pattern fragment still present: %q",
				frag)
		}
	}

	// With exclusion but an empty maintenance DB name, only the
	// probe-marker clause applies and no extra argument is bound.
	q, args = buildTopQueriesQuery(1, 10, "", "calls", "asc", "", true)
	if !strings.Contains(q, "ai_dba_wb_probe") {
		t.Errorf("probe-marker clause missing with empty maintenance name")
	}
	if strings.Contains(q, identityFrag) {
		t.Errorf("database-identity clause present with empty name")
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args with empty maintenance name, got %d",
			len(args))
	}

	// A queryid filter must be added as a positional parameter, and with
	// exclusion enabled the maintenance name must follow it at $4.
	q, args = buildTopQueriesQuery(
		1, 10, "42", "calls", "asc", testMaintenanceDBName, true)
	if !strings.Contains(q, "pss.queryid::text = $3") {
		t.Errorf("queryid filter not bound positionally: %q", q)
	}
	if !strings.Contains(q, identityFrag+" $4") {
		t.Errorf("maintenance name not bound at $4 after queryid: %q", q)
	}
	if len(args) != 4 || args[2] != "42" ||
		args[3] != testMaintenanceDBName {
		t.Errorf("args not appended in order: %#v", args)
	}
}
