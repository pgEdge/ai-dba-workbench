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
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// perfEndpointTestSchema installs the metrics tables read by the three
// performance-summary endpoints whose deferred rollbacks the issue #381
// sweep converted: the performance summary, the per-database summaries,
// and top queries. All of a handler's queries share one transaction, so
// every table it touches must exist; a missing relation would abort the
// transaction for the queries that follow.
const perfEndpointTestSchema = `
CREATE SCHEMA IF NOT EXISTS metrics;
DROP TABLE IF EXISTS metrics.pg_database CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_database CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_all_tables CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_checkpointer CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_statements CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_activity CASCADE;

CREATE TABLE metrics.pg_database (
    connection_id        integer     NOT NULL,
    collected_at         timestamptz NOT NULL,
    datname              text        NOT NULL,
    datistemplate        boolean     NOT NULL DEFAULT false,
    database_size_bytes  bigint,
    age_datfrozenxid     bigint
);

CREATE TABLE metrics.pg_stat_database (
    connection_id  integer     NOT NULL,
    collected_at   timestamptz NOT NULL,
    datname        text,
    numbackends    integer     NOT NULL DEFAULT 0,
    blks_hit       bigint      NOT NULL DEFAULT 0,
    blks_read      bigint      NOT NULL DEFAULT 0,
    xact_commit    bigint      NOT NULL DEFAULT 0,
    xact_rollback  bigint      NOT NULL DEFAULT 0
);

CREATE TABLE metrics.pg_stat_all_tables (
    connection_id  integer     NOT NULL,
    collected_at   timestamptz NOT NULL,
    database_name  text,
    n_live_tup     bigint      NOT NULL DEFAULT 0,
    n_dead_tup     bigint      NOT NULL DEFAULT 0
);

CREATE TABLE metrics.pg_stat_checkpointer (
    connection_id  integer          NOT NULL,
    collected_at   timestamptz      NOT NULL,
    write_time     double precision NOT NULL DEFAULT 0,
    sync_time      double precision NOT NULL DEFAULT 0
);

CREATE TABLE metrics.pg_stat_statements (
    connection_id     integer          NOT NULL,
    collected_at      timestamptz      NOT NULL,
    queryid           bigint           NOT NULL,
    dbid              oid,
    database_name     text,
    query             text             NOT NULL,
    calls             bigint           NOT NULL DEFAULT 0,
    total_exec_time   double precision NOT NULL DEFAULT 0,
    mean_exec_time    double precision NOT NULL DEFAULT 0,
    rows              bigint           NOT NULL DEFAULT 0,
    shared_blks_hit   bigint           NOT NULL DEFAULT 0,
    shared_blks_read  bigint           NOT NULL DEFAULT 0
);

CREATE TABLE metrics.pg_stat_activity (
    connection_id  integer     NOT NULL,
    collected_at   timestamptz NOT NULL,
    datid          oid,
    datname        text
);
`

const perfEndpointTestSchemaTeardown = `
DROP TABLE IF EXISTS metrics.pg_database CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_database CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_all_tables CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_checkpointer CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_statements CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_activity CASCADE;
`

// newPerfEndpointTestHandler wires a PerfSummaryHandler to the
// TEST_AI_WORKBENCH_SERVER Postgres instance with the schema above
// installed. A nil auth store grants access, so the tests exercise the
// handler body rather than the RBAC gate.
func newPerfEndpointTestHandler(
	t *testing.T,
) (*PerfSummaryHandler, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping database test")
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
	if _, err := pool.Exec(ctx, perfEndpointTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create performance endpoint test schema: %v", err)
	}

	handler := NewPerfSummaryHandler(database.NewTestDatastore(pool), nil)
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), perfEndpointTestSchemaTeardown)
		pool.Close()
	}
	return handler, pool, cleanup
}

// seedPerfEndpointMetrics inserts one collection cycle of metrics for the
// given connection, plus an older cycle so the "latest snapshot only"
// behavior of the queries is exercised.
func seedPerfEndpointMetrics(
	t *testing.T,
	pool *pgxpool.Pool,
	connID int,
	latest, prev time.Time,
) {
	t.Helper()

	ctx := context.Background()
	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed exec failed: %v\nSQL: %s", err, sql)
		}
	}

	exec(`INSERT INTO metrics.pg_database
        (connection_id, collected_at, datname, datistemplate,
         database_size_bytes, age_datfrozenxid)
        VALUES ($1, $2, 'appdb', false, 1048576, 1000000)`, connID, latest)

	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends, blks_hit,
         blks_read, xact_commit, xact_rollback)
        VALUES ($1, $2, 'appdb', 6, 900, 100, 5000, 50)`, connID, latest)
	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends, blks_hit,
         blks_read, xact_commit, xact_rollback)
        VALUES ($1, $2, 'appdb', 4, 400, 60, 4000, 40)`, connID, prev)

	exec(`INSERT INTO metrics.pg_stat_all_tables
        (connection_id, collected_at, database_name, n_live_tup, n_dead_tup)
        VALUES ($1, $2, 'appdb', 900, 100)`, connID, latest)

	exec(`INSERT INTO metrics.pg_stat_checkpointer
        (connection_id, collected_at, write_time, sync_time)
        VALUES ($1, $2, 100, 10), ($1, $3, 250, 30)`, connID, prev, latest)
}

// TestHandlePerfSummary_ReturnsMetricsAndReleasesTransaction drives the
// performance-summary endpoint end to end. Beyond the response contents,
// it asserts that the handler leaves no transaction behind: the deferred
// rollback converted for issue #381 sits on this path, and a leaked or
// destroyed pooled connection would show up as a non-zero acquired count.
func TestHandlePerfSummary_ReturnsMetricsAndReleasesTransaction(t *testing.T) {
	h, pool, cleanup := newPerfEndpointTestHandler(t)
	defer cleanup()

	const connA = 4101
	const connB = 4102
	now := time.Now().UTC()
	latest := now.Add(-1 * time.Minute)
	prev := now.Add(-2 * time.Minute)

	seedPerfEndpointMetrics(t, pool, connA, latest, prev)
	seedPerfEndpointMetrics(t, pool, connB, latest, prev)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/performance-summary?connection_ids=4101,4102"+
			"&time_range=24h", nil)
	rec := httptest.NewRecorder()

	h.handlePerfSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp PerfSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Connections) != 2 {
		t.Fatalf("connections = %d, want 2", len(resp.Connections))
	}
	if resp.TimeRange != "24h" {
		t.Errorf("time_range = %q, want \"24h\"", resp.TimeRange)
	}
	if resp.Aggregate == nil {
		t.Error("expected an aggregate for a multi-connection request")
	}

	first := resp.Connections[0]
	// 900 hits against 100 reads is 90%.
	if first.CacheHitRatio.Current != 90 {
		t.Errorf("cache hit ratio = %v, want 90", first.CacheHitRatio.Current)
	}
	if len(first.XIDAgeEntries) != 1 {
		t.Errorf("XID age entries = %d, want 1", len(first.XIDAgeEntries))
	}
	if len(first.Checkpoints.TimeSeries) == 0 {
		t.Error("expected checkpoint time series data")
	}

	// The read-only transaction must be finished and its connection back
	// in the pool.
	if acquired := pool.Stat().AcquiredConns(); acquired != 0 {
		t.Errorf("acquired connections after the request = %d, want 0", acquired)
	}
}

// TestHandlePerfSummary_SingleConnectionOmitsAggregate covers the
// single-connection branch, which reports no aggregate, and the default
// time range.
func TestHandlePerfSummary_SingleConnectionOmitsAggregate(t *testing.T) {
	h, pool, cleanup := newPerfEndpointTestHandler(t)
	defer cleanup()

	const connID = 4103
	now := time.Now().UTC()
	seedPerfEndpointMetrics(t, pool, connID, now.Add(-1*time.Minute),
		now.Add(-2*time.Minute))

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/performance-summary?connection_id=4103", nil)
	rec := httptest.NewRecorder()

	h.handlePerfSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp PerfSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.TimeRange != "1h" {
		t.Errorf("time_range = %q, want the \"1h\" default", resp.TimeRange)
	}
	if resp.Aggregate != nil {
		t.Error("single-connection request must not report an aggregate")
	}
}

// TestHandlePerfSummary_RejectsInvalidRequests covers the method guard
// and the two parameter-validation early returns.
func TestHandlePerfSummary_RejectsInvalidRequests(t *testing.T) {
	h, _, cleanup := newPerfEndpointTestHandler(t)
	defer cleanup()

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost,
			"/api/v1/metrics/performance-summary?connection_id=1", nil)
		rec := httptest.NewRecorder()

		h.handlePerfSummary(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
	})

	t.Run("missing connection", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/performance-summary", nil)
		rec := httptest.NewRecorder()

		h.handlePerfSummary(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("invalid time range", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/performance-summary?connection_id=1&time_range=99z",
			nil)
		rec := httptest.NewRecorder()

		h.handlePerfSummary(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
		want := "Invalid time_range: must be one of 1h, 6h, 24h, 7d, 30d"
		if got := decodeError(t, rec).Error; got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
	})
}

// TestHandleDatabaseSummaries_ReturnsSummaries drives the per-database
// summaries endpoint end to end, which is the second converted rollback
// site in this file.
func TestHandleDatabaseSummaries_ReturnsSummaries(t *testing.T) {
	h, pool, cleanup := newPerfEndpointTestHandler(t)
	defer cleanup()

	const connID = 4104
	now := time.Now().UTC()
	seedPerfEndpointMetrics(t, pool, connID, now.Add(-1*time.Minute),
		now.Add(-2*time.Minute))

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/database-summaries?connection_id=4104&time_range=6h",
		nil)
	rec := httptest.NewRecorder()

	h.handleDatabaseSummaries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp DatabaseSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Databases) != 1 {
		t.Fatalf("databases = %d, want 1", len(resp.Databases))
	}
	db := resp.Databases[0]
	if db.DatabaseName != "appdb" {
		t.Errorf("database name = %q, want \"appdb\"", db.DatabaseName)
	}
	if db.SizeBytes != 1048576 {
		t.Errorf("size bytes = %d, want 1048576", db.SizeBytes)
	}
	if db.CacheHitRatio.TimeSeries == nil {
		t.Error("cache hit ratio time series must never be nil")
	}
	if acquired := pool.Stat().AcquiredConns(); acquired != 0 {
		t.Errorf("acquired connections after the request = %d, want 0", acquired)
	}
}

// TestHandleDatabaseSummaries_RejectsInvalidRequests covers the method
// guard, the single-connection requirement, and the time-range check.
func TestHandleDatabaseSummaries_RejectsInvalidRequests(t *testing.T) {
	h, _, cleanup := newPerfEndpointTestHandler(t)
	defer cleanup()

	cases := []struct {
		name   string
		method string
		url    string
		status int
		want   string
	}{
		{
			name:   "method not allowed",
			method: http.MethodPost,
			url:    "/api/v1/metrics/database-summaries?connection_id=1",
			status: http.StatusMethodNotAllowed,
		},
		{
			name:   "missing connection",
			method: http.MethodGet,
			url:    "/api/v1/metrics/database-summaries",
			status: http.StatusBadRequest,
			want:   "Either connection_id or connection_ids is required",
		},
		{
			name:   "too many connections",
			method: http.MethodGet,
			url:    "/api/v1/metrics/database-summaries?connection_ids=1,2",
			status: http.StatusBadRequest,
			want:   "Exactly one connection_id is required",
		},
		{
			name:   "invalid time range",
			method: http.MethodGet,
			url:    "/api/v1/metrics/database-summaries?connection_id=1&time_range=99z",
			status: http.StatusBadRequest,
			want:   "Invalid time_range: must be one of 1h, 6h, 24h, 7d, 30d",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.url, nil)
			rec := httptest.NewRecorder()

			h.handleDatabaseSummaries(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if tc.want != "" {
				if got := decodeError(t, rec).Error; got != tc.want {
					t.Errorf("error = %q, want %q", got, tc.want)
				}
			}
		})
	}
}

// TestHandleDatabaseSummaries_DefaultsAndEmptySeries covers the default
// time range and the guard that replaces a nil cache-hit series with an
// empty one, which a database with a size but no collected statistics
// exercises.
func TestHandleDatabaseSummaries_DefaultsAndEmptySeries(t *testing.T) {
	h, pool, cleanup := newPerfEndpointTestHandler(t)
	defer cleanup()

	const connID = 4106
	now := time.Now().UTC()
	if _, err := pool.Exec(context.Background(), `INSERT INTO metrics.pg_database
        (connection_id, collected_at, datname, datistemplate, database_size_bytes)
        VALUES ($1, $2, 'sizeonly', false, 2048)`,
		connID, now.Add(-1*time.Minute)); err != nil {
		t.Fatalf("seed exec failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/database-summaries?connection_id=4106", nil)
	rec := httptest.NewRecorder()

	h.handleDatabaseSummaries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp DatabaseSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Databases) != 1 {
		t.Fatalf("databases = %d, want 1", len(resp.Databases))
	}
	if resp.Databases[0].CacheHitRatio.TimeSeries == nil {
		t.Error("cache hit ratio time series must be an empty slice, not nil")
	}
	if len(resp.Databases[0].CacheHitRatio.TimeSeries) != 0 {
		t.Errorf("time series = %v, want empty",
			resp.Databases[0].CacheHitRatio.TimeSeries)
	}
}

// TestPerfSummaryEndpoints_ClosedPoolReturnsError covers the
// begin-transaction failure branch of all three endpoints, which is the
// path taken when the datastore pool is unavailable. No transaction
// exists at that point, so the deferred rollback never runs.
func TestPerfSummaryEndpoints_ClosedPoolReturnsError(t *testing.T) {
	h, pool, cleanup := newPerfEndpointTestHandler(t)
	cleanup() // drops the schema and closes the pool
	if pool.Stat().TotalConns() != 0 {
		t.Log("pool reports lingering connections after Close")
	}

	cases := []struct {
		name    string
		url     string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{
			name:    "performance summary",
			url:     "/api/v1/metrics/performance-summary?connection_id=1",
			handler: h.handlePerfSummary,
		},
		{
			name:    "database summaries",
			url:     "/api/v1/metrics/database-summaries?connection_id=1",
			handler: h.handleDatabaseSummaries,
		},
		{
			name:    "top queries",
			url:     "/api/v1/metrics/top-queries?connection_id=1",
			handler: h.handleTopQueries,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()

			tc.handler(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d (body %q)",
					rec.Code, http.StatusInternalServerError, rec.Body.String())
			}
		})
	}
}

// seedTopQueries inserts two statements in the latest collection for the
// connection, one of which looks like a collector probe, plus an older
// collection that must be ignored.
func seedTopQueries(t *testing.T, pool *pgxpool.Pool, connID int) {
	t.Helper()

	ctx := context.Background()
	now := time.Now().UTC()
	latest := now.Add(-1 * time.Minute)
	prev := now.Add(-2 * time.Minute)

	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed exec failed: %v\nSQL: %s", err, sql)
		}
	}

	exec(`INSERT INTO metrics.pg_stat_activity
        (connection_id, collected_at, datid, datname)
        VALUES ($1, $2, 16384, 'appdb')`, connID, latest)

	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, dbid, database_name, query,
         calls, total_exec_time, mean_exec_time, rows, shared_blks_hit,
         shared_blks_read)
        VALUES
        ($1, $2, 111, 16384, NULL, 'SELECT * FROM orders', 10, 500, 50, 100, 900, 100),
        ($1, $2, 222, 16384, NULL, 'SELECT ai_dba_wb_probe()', 5, 900, 180, 5, 10, 1)`,
		connID, latest)

	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, dbid, database_name, query,
         calls, total_exec_time, mean_exec_time, rows, shared_blks_hit,
         shared_blks_read)
        VALUES ($1, $2, 333, 16384, NULL, 'SELECT 1', 1, 1, 1, 1, 1, 1)`,
		connID, prev)
}

// decodeTopQueries decodes a top-queries response body.
func decodeTopQueries(t *testing.T, rec *httptest.ResponseRecorder) []TopQueryRow {
	t.Helper()
	var rows []TopQueryRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("failed to decode top queries response: %v", err)
	}
	return rows
}

// TestHandleTopQueries_ReturnsLatestSnapshot drives the top-queries
// endpoint, the third converted rollback site in this file, and covers
// the ordering, database-name join, limit clamping, and the optional
// queryid and exclude_collector filters.
func TestHandleTopQueries_ReturnsLatestSnapshot(t *testing.T) {
	h, pool, cleanup := newPerfEndpointTestHandler(t)
	defer cleanup()

	const connID = 4105
	seedTopQueries(t, pool, connID)

	t.Run("orders by total exec time", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/top-queries?connection_id=4105&limit=500", nil)
		rec := httptest.NewRecorder()

		h.handleTopQueries(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body %q)",
				rec.Code, http.StatusOK, rec.Body.String())
		}
		rows := decodeTopQueries(t, rec)
		if len(rows) != 2 {
			t.Fatalf("rows = %d, want 2 (the older collection must be ignored)",
				len(rows))
		}
		if rows[0].QueryID != "222" {
			t.Errorf("first row queryid = %q, want \"222\" (highest total_exec_time)",
				rows[0].QueryID)
		}
		if rows[0].DatabaseName != "appdb" {
			t.Errorf("database name = %q, want \"appdb\" from the activity join",
				rows[0].DatabaseName)
		}
	})

	t.Run("ascending order and row limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/top-queries?connection_id=4105"+
				"&order_by=calls&order=ASC&limit=0", nil)
		rec := httptest.NewRecorder()

		h.handleTopQueries(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		rows := decodeTopQueries(t, rec)
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want 1 (limit clamped up to 1)", len(rows))
		}
		if rows[0].QueryID != "222" {
			t.Errorf("queryid = %q, want \"222\" (fewest calls first)", rows[0].QueryID)
		}
	})

	t.Run("filters by queryid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/top-queries?connection_id=4105&queryid=111", nil)
		rec := httptest.NewRecorder()

		h.handleTopQueries(rec, req)

		rows := decodeTopQueries(t, rec)
		if len(rows) != 1 || rows[0].QueryID != "111" {
			t.Fatalf("rows = %+v, want only queryid 111", rows)
		}
	})

	t.Run("excludes collector probes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/top-queries?connection_id=4105&exclude_collector=true",
			nil)
		rec := httptest.NewRecorder()

		h.handleTopQueries(rec, req)

		rows := decodeTopQueries(t, rec)
		if len(rows) != 1 || rows[0].QueryID != "111" {
			t.Fatalf("rows = %+v, want the probe query excluded", rows)
		}
	})

	if acquired := pool.Stat().AcquiredConns(); acquired != 0 {
		t.Errorf("acquired connections after the requests = %d, want 0", acquired)
	}
}

// TestHandleTopQueries_MissingTableReturnsEmptyList covers the
// query-failure branch, which answers with an empty list rather than an
// error so an uncollected estate still renders.
func TestHandleTopQueries_MissingTableReturnsEmptyList(t *testing.T) {
	h, pool, cleanup := newPerfEndpointTestHandler(t)
	defer cleanup()

	if _, err := pool.Exec(context.Background(),
		`DROP TABLE metrics.pg_stat_statements CASCADE`); err != nil {
		t.Fatalf("Failed to drop pg_stat_statements: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/top-queries?connection_id=4105", nil)
	rec := httptest.NewRecorder()

	h.handleTopQueries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rows := decodeTopQueries(t, rec); len(rows) != 0 {
		t.Errorf("rows = %+v, want an empty list", rows)
	}
}

// TestHandleTopQueries_SkipsUnscannableRows covers the per-row scan
// failure branch, which must skip the offending row and still answer with
// the rows it could read. Widening a numeric column to text and storing a
// non-numeric value reproduces the type mismatch that branch defends
// against.
func TestHandleTopQueries_SkipsUnscannableRows(t *testing.T) {
	h, pool, cleanup := newPerfEndpointTestHandler(t)
	defer cleanup()

	ctx := context.Background()
	const connID = 4107
	if _, err := pool.Exec(ctx,
		`ALTER TABLE metrics.pg_stat_statements ALTER COLUMN calls TYPE text`); err != nil {
		t.Fatalf("Failed to alter calls column: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, dbid, database_name, query,
         calls, total_exec_time, mean_exec_time, rows, shared_blks_hit,
         shared_blks_read)
        VALUES ($1, $2, 444, 16384, 'appdb', 'SELECT 1', 'not-a-number',
                1, 1, 1, 1, 1)`, connID, time.Now().UTC()); err != nil {
		t.Fatalf("seed exec failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/top-queries?connection_id=4107", nil)
	rec := httptest.NewRecorder()

	h.handleTopQueries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	if rows := decodeTopQueries(t, rec); len(rows) != 0 {
		t.Errorf("rows = %+v, want the unscannable row skipped", rows)
	}
}

// TestHandleTopQueries_RejectsInvalidRequests covers the method guard and
// every parameter-validation early return on the top-queries endpoint.
func TestHandleTopQueries_RejectsInvalidRequests(t *testing.T) {
	h, _, cleanup := newPerfEndpointTestHandler(t)
	defer cleanup()

	cases := []struct {
		name   string
		method string
		url    string
		status int
		want   string
	}{
		{
			name:   "method not allowed",
			method: http.MethodPost,
			url:    "/api/v1/metrics/top-queries?connection_id=1",
			status: http.StatusMethodNotAllowed,
		},
		{
			name:   "missing connection",
			method: http.MethodGet,
			url:    "/api/v1/metrics/top-queries",
			status: http.StatusBadRequest,
			want:   "Either connection_id or connection_ids is required",
		},
		{
			name:   "too many connections",
			method: http.MethodGet,
			url:    "/api/v1/metrics/top-queries?connection_ids=1,2",
			status: http.StatusBadRequest,
			want:   "Exactly one connection_id is required",
		},
		{
			name:   "invalid limit",
			method: http.MethodGet,
			url:    "/api/v1/metrics/top-queries?connection_id=1&limit=abc",
			status: http.StatusBadRequest,
		},
		{
			name:   "invalid order_by",
			method: http.MethodGet,
			url:    "/api/v1/metrics/top-queries?connection_id=1&order_by=drop",
			status: http.StatusBadRequest,
			want: "Invalid order_by: must be one of total_exec_time, calls, " +
				"mean_exec_time, rows, shared_blks_hit, shared_blks_read",
		},
		{
			name:   "invalid order",
			method: http.MethodGet,
			url:    "/api/v1/metrics/top-queries?connection_id=1&order=sideways",
			status: http.StatusBadRequest,
			want:   "Invalid order: must be asc or desc",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.url, nil)
			rec := httptest.NewRecorder()

			h.handleTopQueries(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %q)",
					rec.Code, tc.status, rec.Body.String())
			}
			if tc.want != "" {
				if got := decodeError(t, rec).Error; got != tc.want {
					t.Errorf("error = %q, want %q", got, tc.want)
				}
			}
		})
	}
}
