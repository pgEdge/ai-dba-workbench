/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// HTTP-level tests for the Top Queries endpoint, including the
// behavioral check for GitHub issue #364: with exclude_collector=true,
// neither the collector's probe queries nor the Workbench's own
// datastore statements may appear in the response.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// topQueriesTestSchema creates the minimal connections table and the two
// metrics tables read by the Top Queries endpoint. Only the columns the
// endpoint selects are declared.
const topQueriesTestSchema = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    is_shared BOOLEAN NOT NULL DEFAULT TRUE,
    owner_username VARCHAR(255)
);

CREATE SCHEMA metrics;

CREATE TABLE metrics.pg_stat_activity (
    connection_id INTEGER NOT NULL,
    datid OID,
    datname TEXT,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE metrics.pg_stat_statements (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    queryid BIGINT NOT NULL,
    dbid OID,
    database_name TEXT,
    query TEXT NOT NULL,
    calls BIGINT NOT NULL,
    total_exec_time DOUBLE PRECISION NOT NULL,
    mean_exec_time DOUBLE PRECISION NOT NULL,
    rows BIGINT NOT NULL,
    shared_blks_hit BIGINT NOT NULL,
    shared_blks_read BIGINT NOT NULL
);
`

// newTopQueriesHandler builds a PerfSummaryHandler over the integration
// test database, seeding one user workload statement plus one statement
// of each Workbench-internal flavor.
func newTopQueriesHandler(t *testing.T) (*PerfSummaryHandler, int, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set; skipping")
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
		t.Fatalf("create test schema: %v", err)
	}

	var connID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO connections (name) VALUES ('top-queries')
		RETURNING id
	`).Scan(&connID); err != nil {
		pool.Close()
		t.Fatalf("seed connection: %v", err)
	}

	collectedAt := time.Now().UTC()
	statements := []struct {
		queryid int64
		query   string
	}{
		{1, "SELECT * FROM orders WHERE id = $1"},
		{2, "SELECT 'pg_stat_activity' AS ai_dba_wb_probe, subq.* " +
			"FROM (SELECT * FROM pg_stat_activity) AS subq"},
		{3, `INSERT ` + sqlmarker.Comment +
			` INTO "metrics"."pg_stat_statements" ("a") VALUES ($1)`},
		{4, "WITH " + sqlmarker.Comment +
			" recent_statements AS (SELECT 1) SELECT * FROM recent_statements"},
	}
	for _, st := range statements {
		if _, err := pool.Exec(ctx, `
			INSERT INTO metrics.pg_stat_statements (
				connection_id, collected_at, queryid, dbid, database_name,
				query, calls, total_exec_time, mean_exec_time, rows,
				shared_blks_hit, shared_blks_read)
			VALUES ($1, $2, $3, NULL, 'appdb', $4, 10, 100.0, 10.0, 5, 1, 0)
		`, connID, collectedAt, st.queryid, st.query); err != nil {
			pool.Close()
			t.Fatalf("seed statement %d: %v", st.queryid, err)
		}
	}

	tmpDir, err := os.MkdirTemp("", "top-queries-auth-*")
	if err != nil {
		pool.Close()
		t.Fatalf("temp dir: %v", err)
	}
	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		pool.Close()
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("auth store: %v", err)
	}

	handler := NewPerfSummaryHandler(database.NewTestDatastore(pool), authStore)

	cleanup := func() {
		authStore.Close()
		for _, stmt := range []string{
			"DROP SCHEMA IF EXISTS metrics CASCADE",
			"DROP TABLE IF EXISTS connections CASCADE",
		} {
			if _, err := pool.Exec(context.Background(), stmt); err != nil {
				t.Logf("cleanup: %s: %v", stmt, err)
			}
		}
		pool.Close()
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("cleanup: remove %s: %v", tmpDir, err)
		}
	}

	return handler, connID, cleanup
}

// superuserRequest builds a GET request for the Top Queries endpoint
// carrying a superuser context, which is all the RBAC checker needs.
func superuserRequest(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	ctx := context.WithValue(
		req.Context(), auth.IsSuperuserContextKey, true)
	return req.WithContext(ctx)
}

// decodeTopQueries reads the endpoint's JSON array response.
func decodeTopQueries(t *testing.T, rec *httptest.ResponseRecorder) []TopQueryRow {
	t.Helper()
	var rows []TopQueryRow
	if err := json.NewDecoder(rec.Body).Decode(&rows); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return rows
}

// TestHandleTopQueries_ExcludesWorkbenchQueries is the regression test
// for issue #364: with the filter on, the collector's probe query and
// both Workbench-internal statements must disappear, leaving only the
// user's own workload.
func TestHandleTopQueries_ExcludesWorkbenchQueries(t *testing.T) {
	handler, connID, cleanup := newTopQueriesHandler(t)
	defer cleanup()

	// Without the filter, all four statements come back.
	rec := httptest.NewRecorder()
	handler.handleTopQueries(rec, superuserRequest(fmt.Sprintf(
		"/api/v1/metrics/top-queries?connection_id=%d", connID)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := len(decodeTopQueries(t, rec)); got != 4 {
		t.Errorf("unfiltered rows = %d, want 4", got)
	}

	// With the filter, only the user workload survives.
	rec = httptest.NewRecorder()
	handler.handleTopQueries(rec, superuserRequest(fmt.Sprintf(
		"/api/v1/metrics/top-queries?connection_id=%d"+
			"&exclude_collector=true", connID)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rows := decodeTopQueries(t, rec)
	if len(rows) != 1 {
		for _, r := range rows {
			t.Logf("returned: %s", r.Query)
		}
		t.Fatalf("filtered rows = %d, want 1", len(rows))
	}
	if rows[0].QueryID != "1" {
		t.Errorf("surviving queryid = %s, want 1", rows[0].QueryID)
	}
	if rows[0].DatabaseName != "appdb" {
		t.Errorf("database_name = %q, want appdb", rows[0].DatabaseName)
	}
}

// TestHandleTopQueries_QueryIDAndOrdering covers the queryid filter, the
// limit clamps, and the ordering parameters.
func TestHandleTopQueries_QueryIDAndOrdering(t *testing.T) {
	handler, connID, cleanup := newTopQueriesHandler(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	handler.handleTopQueries(rec, superuserRequest(fmt.Sprintf(
		"/api/v1/metrics/top-queries?connection_id=%d&queryid=1"+
			"&order_by=calls&order=ASC&limit=1000", connID)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rows := decodeTopQueries(t, rec)
	if len(rows) != 1 || rows[0].QueryID != "1" {
		t.Fatalf("unexpected rows: %+v", rows)
	}

	// A limit below the minimum is clamped rather than rejected.
	rec = httptest.NewRecorder()
	handler.handleTopQueries(rec, superuserRequest(fmt.Sprintf(
		"/api/v1/metrics/top-queries?connection_id=%d&limit=0", connID)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := len(decodeTopQueries(t, rec)); got != 1 {
		t.Errorf("rows with limit=0 = %d, want 1", got)
	}
}

// TestHandleTopQueries_NoData confirms an empty array is returned when
// the metrics tables are missing, rather than an error.
func TestHandleTopQueries_NoData(t *testing.T) {
	handler, connID, cleanup := newTopQueriesHandler(t)
	defer cleanup()

	pool := handler.datastore.GetPool()
	if _, err := pool.Exec(context.Background(),
		"DROP SCHEMA metrics CASCADE"); err != nil {
		t.Fatalf("drop metrics schema: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.handleTopQueries(rec, superuserRequest(fmt.Sprintf(
		"/api/v1/metrics/top-queries?connection_id=%d", connID)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := len(decodeTopQueries(t, rec)); got != 0 {
		t.Errorf("rows = %d, want 0", got)
	}
}

// TestHandleTopQueries_RowScanError covers the branch that skips a row
// which cannot be scanned, by relaxing a NOT NULL constraint and
// inserting a row with a null count.
func TestHandleTopQueries_RowScanError(t *testing.T) {
	handler, connID, cleanup := newTopQueriesHandler(t)
	defer cleanup()

	ctx := context.Background()
	pool := handler.datastore.GetPool()
	if _, err := pool.Exec(ctx, `
		ALTER TABLE metrics.pg_stat_statements ALTER COLUMN calls
			DROP NOT NULL
	`); err != nil {
		t.Fatalf("relax constraint: %v", err)
	}

	var collectedAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT max(collected_at) FROM metrics.pg_stat_statements
	`).Scan(&collectedAt); err != nil {
		t.Fatalf("read collected_at: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO metrics.pg_stat_statements (
			connection_id, collected_at, queryid, dbid, database_name,
			query, calls, total_exec_time, mean_exec_time, rows,
			shared_blks_hit, shared_blks_read)
		VALUES ($1, $2, 99, NULL, 'appdb', 'SELECT 99', NULL,
		        1.0, 1.0, 1, 0, 0)
	`, connID, collectedAt); err != nil {
		t.Fatalf("seed unscannable row: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.handleTopQueries(rec, superuserRequest(fmt.Sprintf(
		"/api/v1/metrics/top-queries?connection_id=%d", connID)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	// The four seeded statements are returned; the null row is skipped.
	if got := len(decodeTopQueries(t, rec)); got != 4 {
		t.Errorf("rows = %d, want 4 with the unscannable row skipped", got)
	}
}

// TestHandleTopQueries_TransactionError covers the failure to open the
// read-only transaction, by closing the pool first.
func TestHandleTopQueries_TransactionError(t *testing.T) {
	handler, connID, cleanup := newTopQueriesHandler(t)
	defer cleanup()

	handler.datastore.GetPool().Close()

	rec := httptest.NewRecorder()
	handler.handleTopQueries(rec, superuserRequest(fmt.Sprintf(
		"/api/v1/metrics/top-queries?connection_id=%d", connID)))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d",
			rec.Code, http.StatusInternalServerError)
	}
}

// TestHandleTopQueries_RequestValidation covers the guard clauses:
// method, connection identification, parameter validation, and RBAC.
func TestHandleTopQueries_RequestValidation(t *testing.T) {
	handler, connID, cleanup := newTopQueriesHandler(t)
	defer cleanup()

	base := fmt.Sprintf(
		"/api/v1/metrics/top-queries?connection_id=%d", connID)

	t.Run("method not allowed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, base, nil)
		handler.handleTopQueries(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want %d",
				rec.Code, http.StatusMethodNotAllowed)
		}
	})

	t.Run("missing connection id", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.handleTopQueries(rec,
			superuserRequest("/api/v1/metrics/top-queries"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d",
				rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("too many connection ids", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.handleTopQueries(rec, superuserRequest(
			"/api/v1/metrics/top-queries?connection_ids=1,2"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d",
				rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("invalid limit", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.handleTopQueries(rec,
			superuserRequest(base+"&limit=abc"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d",
				rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("invalid order_by", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.handleTopQueries(rec,
			superuserRequest(base+"&order_by=drop_table"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d",
				rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("invalid order", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.handleTopQueries(rec,
			superuserRequest(base+"&order=sideways"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d",
				rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("permission denied", func(t *testing.T) {
		// A connection that is neither shared nor owned by the caller,
		// requested without a superuser flag or user context, must be
		// refused by the RBAC checker.
		var privateID int
		err := handler.datastore.GetPool().QueryRow(context.Background(), `
			INSERT INTO connections (name, is_shared, owner_username)
			VALUES ('private', FALSE, 'someone-else')
			RETURNING id
		`).Scan(&privateID)
		if err != nil {
			t.Fatalf("seed private connection: %v", err)
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf(
			"/api/v1/metrics/top-queries?connection_id=%d", privateID), nil)
		handler.handleTopQueries(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d",
				rec.Code, http.StatusForbidden)
		}
	})
}
