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
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// topQueriesTestSchema mirrors the columns handleTopQueries reads from the
// metrics schema. Only the fields referenced by the query are modeled.
const topQueriesTestSchema = `
CREATE SCHEMA IF NOT EXISTS metrics;
DROP TABLE IF EXISTS metrics.pg_stat_statements CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_activity CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_database CASCADE;

CREATE TABLE metrics.pg_stat_statements (
    connection_id     integer     NOT NULL,
    collected_at      timestamptz NOT NULL,
    queryid           bigint      NOT NULL,
    userid            bigint,
    dbid              bigint,
    toplevel          boolean     NOT NULL DEFAULT TRUE,
    database_name     text,
    query             text        NOT NULL,
    calls             bigint      NOT NULL DEFAULT 0,
    total_exec_time   double precision NOT NULL DEFAULT 0,
    mean_exec_time    double precision NOT NULL DEFAULT 0,
    min_exec_time     double precision NOT NULL DEFAULT 0,
    max_exec_time     double precision NOT NULL DEFAULT 0,
    rows              bigint      NOT NULL DEFAULT 0,
    shared_blks_hit   bigint      NOT NULL DEFAULT 0,
    shared_blks_read  bigint      NOT NULL DEFAULT 0
);

CREATE TABLE metrics.pg_stat_activity (
    connection_id    integer     NOT NULL,
    collected_at     timestamptz NOT NULL,
    datid            bigint,
    datname          text,
    usesysid         bigint,
    usename          text,
    client_addr      inet,
    client_hostname  text,
    query_id         bigint
);

CREATE TABLE metrics.pg_stat_database (
    connection_id    integer     NOT NULL,
    collected_at     timestamptz NOT NULL,
    database_name    text        NOT NULL,
    datid            bigint,
    datname          text
);
`

const topQueriesTestSchemaTeardown = `
DROP TABLE IF EXISTS metrics.pg_stat_statements CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_activity CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_database CASCADE;
`

// topQueriesConnID is the connection the fixture is seeded against.
const topQueriesConnID = 4242

// newTopQueriesTestHandler wires a PerfSummaryHandler to the local test
// Postgres and installs the trimmed metrics schema above. The handler is
// built with a nil auth store, so the RBAC check grants access and the test
// can concentrate on parameter handling.
func newTopQueriesTestHandler(
	t *testing.T,
) (*PerfSummaryHandler, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping top-queries test")
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
		t.Fatalf("Failed to create top queries test schema: %v", err)
	}

	ds := database.NewTestDatastore(pool)
	handler := NewPerfSummaryHandler(ds, nil)
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), topQueriesTestSchemaTeardown)
		pool.Close()
	}
	return handler, pool, cleanup
}

// seedTopQueriesFixture inserts six statements: four against database
// "alpha" and two against "beta". One "beta" row carries a stale
// pss.database_name that must be overridden by the pg_stat_activity lookup,
// which proves the database_name filter runs against the resolved name. A
// collector probe query is seeded too, so the exclude_collector behavior
// stays covered.
//
// Because the endpoint reports summed counter deltas rather than a single
// snapshot, every statement is seeded twice: once at baseline with all
// counters at zero and once at latest with the values the assertions use.
// The delta is therefore exactly the latest reading, which keeps the
// expectations readable. A statement seeded only in the older snapshot has
// no predecessor inside the default one-hour window and so contributes no
// delta at all.
func seedTopQueriesFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	latest := time.Now().UTC().Add(-1 * time.Minute)
	baseline := latest.Add(-5 * time.Minute)
	older := time.Now().UTC().Add(-60 * time.Minute)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed exec failed: %v\nSQL: %s", err, sql)
		}
	}

	// datid -> datname and usesysid -> usename mappings used by the
	// db_names and user_names CTEs. The older sample carries the previous
	// names for the same OIDs, standing in for a database and a role
	// renamed within the retention window: both CTEs take the most recent
	// name per OID, so the newer names must win.
	exec(`INSERT INTO metrics.pg_stat_activity
        (connection_id, collected_at, datid, datname, usesysid, usename)
        VALUES ($1, $3, 100, 'alpha_old', 10, 'alice_old'),
               ($1, $3, 200, 'beta_old', 20, 'bob_old'),
               ($1, $2, 100, 'alpha', 10, 'alice'),
               ($1, $2, 200, 'beta', 20, 'bob')`,
		topQueriesConnID, latest, older)

	// Latest snapshot. total_exec_time is unique per row so the default
	// ordering is deterministic. Query 1004 is attributed to a role OID
	// that pg_stat_activity never observed, which is the unresolvable
	// username case.
	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, userid, dbid, database_name,
         query, calls, total_exec_time, mean_exec_time,
         min_exec_time, max_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES
        ($1, $2, 1001, 10, 100, 'alpha', 'SELECT 1', 10, 600, 60,
         1.5, 120.25, 10, 100, 1),
        ($1, $2, 1002, 10, 100, 'alpha', 'SELECT 2', 20, 500, 25,
         2, 90, 20, 200, 2),
        ($1, $2, 1003, 10, 100, 'alpha', 'SELECT 3', 30, 400, 13,
         3, 80, 30, 300, 3),
        ($1, $2, 1004, 999, 100, 'alpha', 'SELECT 4', 40, 300, 7,
         4, 70, 40, 400, 4),
        ($1, $2, 1005, 20, 200, 'stale-name', 'SELECT 5', 50, 200, 4,
         5, 60, 50, 500, 5),
        ($1, $2, 1006, 20, 200, 'beta', 'SELECT 6 ai_dba_wb_probe', 60, 100, 2,
         6, 50, 60, 600, 6)`,
		topQueriesConnID, latest)

	// Baseline snapshot: the same statements with every counter at zero,
	// five minutes before the latest one. The window aggregation needs a
	// predecessor sample per statement, and starting from zero makes each
	// delta equal to the latest reading above. min_exec_time and
	// max_exec_time are lifetime columns taken from the latest sample, so
	// the zeros here never reach the response.
	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, userid, dbid, database_name,
         query, calls, total_exec_time, mean_exec_time,
         min_exec_time, max_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES
        ($1, $2, 1001, 10, 100, 'alpha', 'SELECT 1', 0, 0, 0, 0, 0, 0, 0, 0),
        ($1, $2, 1002, 10, 100, 'alpha', 'SELECT 2', 0, 0, 0, 0, 0, 0, 0, 0),
        ($1, $2, 1003, 10, 100, 'alpha', 'SELECT 3', 0, 0, 0, 0, 0, 0, 0, 0),
        ($1, $2, 1004, 999, 100, 'alpha', 'SELECT 4', 0, 0, 0, 0, 0, 0, 0, 0),
        ($1, $2, 1005, 20, 200, 'stale-name', 'SELECT 5',
         0, 0, 0, 0, 0, 0, 0, 0),
        ($1, $2, 1006, 20, 200, 'beta', 'SELECT 6 ai_dba_wb_probe',
         0, 0, 0, 0, 0, 0, 0, 0)`,
		topQueriesConnID, baseline)

	// Older snapshot for the same connection: a single sample outside the
	// default window, so it has no predecessor to difference against and
	// contributes nothing.
	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, userid, dbid, database_name,
         query, calls, total_exec_time, mean_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES ($1, $2, 9001, 10, 100, 'alpha', 'SELECT old', 1, 9999, 9999, 1,
                1, 1)`,
		topQueriesConnID, older)

	// A different connection that must never leak into the results. It too
	// needs a baseline sample, or it would have no delta of its own.
	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, userid, dbid, database_name,
         query, calls, total_exec_time, mean_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES ($1, $2, 7001, 10, 100, 'alpha', 'SELECT other', 1, 8888, 8888,
                1, 1, 1),
               ($1, $3, 7001, 10, 100, 'alpha', 'SELECT other', 0, 0, 0,
                0, 0, 0)`,
		topQueriesConnID+1, latest, baseline)
}

// callTopQueries invokes the handler with the supplied raw query string and
// returns the recorder for inspection.
func callTopQueries(
	t *testing.T,
	h *PerfSummaryHandler,
	rawQuery string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/top-queries?"+rawQuery, nil)
	rec := httptest.NewRecorder()
	h.handleTopQueries(rec, req)
	return rec
}

// decodeTopQueries asserts a 200 response and decodes the bare JSON array
// body, returning the rows and the X-Total-Count header value.
func decodeTopQueries(
	t *testing.T,
	rec *httptest.ResponseRecorder,
) ([]TopQueryRow, string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var rows []TopQueryRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("response body is not a JSON array of TopQueryRow: %v; "+
			"body: %s", err, rec.Body.String())
	}
	return rows, rec.Header().Get("X-Total-Count")
}

// queryIDs extracts the query IDs from a result page for order assertions.
func queryIDs(rows []TopQueryRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.QueryID)
	}
	return ids
}

// TestTopQueries_DefaultsAndTotalCount checks that the unpaged defaults are
// unchanged and that X-Total-Count reports every matching row.
func TestTopQueries_DefaultsAndTotalCount(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	rec := callTopQueries(t, h, "connection_id=4242")
	rows, total := decodeTopQueries(t, rec)

	if len(rows) != 6 {
		t.Fatalf("got %d rows, want 6: %#v", len(rows), rows)
	}
	if total != "6" {
		t.Errorf("X-Total-Count = %q, want \"6\"", total)
	}
	// Default ordering is total_exec_time descending.
	want := []string{"1001", "1002", "1003", "1004", "1005", "1006"}
	got := queryIDs(rows)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("default ordering = %v, want %v", got, want)
		}
	}
	// The stale pss.database_name is overridden by the pg_stat_activity
	// lookup on dbid.
	if rows[4].DatabaseName != "beta" {
		t.Errorf("resolved database_name = %q, want \"beta\"",
			rows[4].DatabaseName)
	}
}

// TestTopQueries_UsernameResolution covers the username column added for
// issue #350: the role OID is resolved through pg_stat_activity, the most
// recently observed name wins when a role or database was renamed inside the
// retention window, and a role that was never observed resolves to an empty
// string rather than dropping the row.
func TestTopQueries_UsernameResolution(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	rows, _ := decodeTopQueries(t, callTopQueries(t, h, "connection_id=4242"))
	if len(rows) != 6 {
		t.Fatalf("got %d rows, want 6: %#v", len(rows), rows)
	}

	byQueryID := make(map[string]TopQueryRow, len(rows))
	for _, row := range rows {
		byQueryID[row.QueryID] = row
	}

	tests := []struct {
		queryID      string
		wantUser     string
		wantDatabase string
	}{
		{"1001", "alice", "alpha"},
		{"1002", "alice", "alpha"},
		{"1005", "bob", "beta"},
		// userid 999 was never seen with an active backend.
		{"1004", "", "alpha"},
	}

	for _, tc := range tests {
		t.Run("queryid_"+tc.queryID, func(t *testing.T) {
			row, ok := byQueryID[tc.queryID]
			if !ok {
				t.Fatalf("queryid %s missing from results", tc.queryID)
			}
			if row.Username != tc.wantUser {
				t.Errorf("username = %q, want %q", row.Username, tc.wantUser)
			}
			if row.DatabaseName != tc.wantDatabase {
				t.Errorf("database_name = %q, want %q", row.DatabaseName,
					tc.wantDatabase)
			}
		})
	}
}

// TestTopQueries_NameLookupWindow confirms that the db_names and user_names
// CTEs only consult pg_stat_activity samples taken in the hour before the
// latest pg_stat_statements snapshot. A name observed only outside that
// window is not used: the database falls back to the name recorded by the
// probe and the role resolves to an empty string. The bound is what keeps
// the DISTINCT ON sort from covering every activity row in retention.
func TestTopQueries_NameLookupWindow(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	ctx := context.Background()

	latest := time.Now().UTC().Add(-1 * time.Minute)
	insideWindow := latest.Add(-30 * time.Minute)
	outsideWindow := latest.Add(-90 * time.Minute)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed exec failed: %v\nSQL: %s", err, sql)
		}
	}

	// OID 100 / role 10 were observed inside the window; OID 200 / role 20
	// only before it.
	exec(`INSERT INTO metrics.pg_stat_activity
        (connection_id, collected_at, datid, datname, usesysid, usename)
        VALUES ($1, $2, 100, 'alpha', 10, 'alice'),
               ($1, $3, 200, 'beta', 20, 'bob')`,
		topQueriesConnID, insideWindow, outsideWindow)

	// Each statement is seeded twice so that the window aggregation has a
	// pair to difference; the baseline counters are zero, so the reported
	// figures are the latest readings.
	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, userid, dbid, database_name,
         query, calls, total_exec_time, mean_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES ($1, $2, 1001, 10, 100, 'alpha-probe', 'SELECT 1', 10, 600,
                60, 10, 100, 1),
               ($1, $2, 1002, 20, 200, 'beta-probe', 'SELECT 2', 20, 500,
                25, 20, 200, 2),
               ($1, $3, 1001, 10, 100, 'alpha-probe', 'SELECT 1', 0, 0,
                0, 0, 0, 0),
               ($1, $3, 1002, 20, 200, 'beta-probe', 'SELECT 2', 0, 0,
                0, 0, 0, 0)`,
		topQueriesConnID, latest, latest.Add(-2*time.Minute))

	rows, _ := decodeTopQueries(t, callTopQueries(t, h, "connection_id=4242"))
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %#v", len(rows), rows)
	}
	byQueryID := make(map[string]TopQueryRow, len(rows))
	for _, row := range rows {
		byQueryID[row.QueryID] = row
	}

	if got := byQueryID["1001"]; got.Username != "alice" ||
		got.DatabaseName != "alpha" {
		t.Errorf("inside window: username = %q, database = %q, "+
			"want alice / alpha", got.Username, got.DatabaseName)
	}
	if got := byQueryID["1002"]; got.Username != "" ||
		got.DatabaseName != "beta-probe" {
		t.Errorf("outside window: username = %q, database = %q, "+
			"want \"\" / beta-probe", got.Username, got.DatabaseName)
	}
}

// TestTopQueries_LastClientAttribution covers the client columns added for
// issue #384: a queryid is attributed to the client most recently seen
// running it in pg_stat_activity, samples outside the lookup window are
// ignored, a queryid never observed yields JSON nulls, and several activity
// samples for one queryid still produce a single result row so the total
// count is unaffected by the join.
func TestTopQueries_LastClientAttribution(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)
	ctx := context.Background()

	var latest time.Time
	if err := pool.QueryRow(ctx, `SELECT MAX(collected_at)
        FROM metrics.pg_stat_statements WHERE connection_id = $1`,
		topQueriesConnID).Scan(&latest); err != nil {
		t.Fatalf("read latest snapshot: %v", err)
	}
	newest := latest.Add(-5 * time.Minute)
	older := latest.Add(-30 * time.Minute)
	outsideWindow := latest.Add(-90 * time.Minute)

	// Query 1001 was seen twice inside the window, from two different
	// clients, so the newer sample must win. Query 1002 was only seen
	// before the window opened. Query 1005 was seen once, with no
	// resolved hostname, as happens when log_hostname is off. Query 1003
	// never appears with a query_id at all. The other connection's
	// sample for 1003 must not leak in.
	if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_stat_activity
        (connection_id, collected_at, datid, datname, usesysid, usename,
         client_addr, client_hostname, query_id)
        VALUES ($1, $3, 100, 'alpha', 10, 'alice', '192.0.2.10', 'app-old.example.com', 1001),
               ($1, $2, 100, 'alpha', 10, 'alice', '192.0.2.20', 'app-new.example.com', 1001),
               ($1, $4, 100, 'alpha', 10, 'alice', '192.0.2.30', 'stale.example.com', 1002),
               ($1, $2, 200, 'beta', 20, 'bob', '198.51.100.7', NULL, 1005),
               ($5, $2, 100, 'alpha', 10, 'alice', '203.0.113.9', 'other.example.com', 1003)`,
		topQueriesConnID, newest, older, outsideWindow,
		topQueriesConnID+1); err != nil {
		t.Fatalf("seed activity: %v", err)
	}

	rec := callTopQueries(t, h, "connection_id=4242")
	rows, total := decodeTopQueries(t, rec)
	if total != "6" || len(rows) != 6 {
		t.Fatalf("total = %s, rows = %d, want 6 and 6: %#v",
			total, len(rows), rows)
	}
	byQueryID := make(map[string]TopQueryRow, len(rows))
	for _, row := range rows {
		if _, dup := byQueryID[row.QueryID]; dup {
			t.Errorf("queryid %s returned more than once", row.QueryID)
		}
		byQueryID[row.QueryID] = row
	}

	strPtr := func(v string) *string { return &v }
	tests := []struct {
		queryID        string
		wantAddr       *string
		wantHostname   *string
		wantObservedAt *time.Time
	}{
		{"1001", strPtr("192.0.2.20"), strPtr("app-new.example.com"), &newest},
		{"1002", nil, nil, nil},
		{"1003", nil, nil, nil},
		{"1005", strPtr("198.51.100.7"), nil, &newest},
	}
	for _, tc := range tests {
		t.Run("queryid_"+tc.queryID, func(t *testing.T) {
			row, ok := byQueryID[tc.queryID]
			if !ok {
				t.Fatalf("queryid %s missing from results", tc.queryID)
			}
			assertOptionalString(t, "client_addr", row.ClientAddr, tc.wantAddr)
			assertOptionalString(t, "client_hostname", row.ClientHostname,
				tc.wantHostname)
			switch {
			case tc.wantObservedAt == nil && row.ClientObservedAt != nil:
				t.Errorf("client_observed_at = %v, want null", *row.ClientObservedAt)
			case tc.wantObservedAt != nil && row.ClientObservedAt == nil:
				t.Errorf("client_observed_at = null, want %v", *tc.wantObservedAt)
			case tc.wantObservedAt != nil &&
				!row.ClientObservedAt.Equal(*tc.wantObservedAt):
				t.Errorf("client_observed_at = %v, want %v",
					*row.ClientObservedAt, *tc.wantObservedAt)
			}
		})
	}

	// The wire format must carry explicit nulls, not omit the keys, so the
	// client can distinguish "never observed" without a schema lookup.
	body := rec.Body.String()
	for _, key := range []string{
		`"client_addr":null`, `"client_hostname":null`,
		`"client_observed_at":null`,
	} {
		if !strings.Contains(body, key) {
			t.Errorf("response body lacks %s", key)
		}
	}
}

// assertOptionalString compares two nullable strings, reporting a mismatch
// in either nullness or value.
func assertOptionalString(t *testing.T, field string, got, want *string) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s = %q, want null", field, *got)
	case want != nil && got == nil:
		t.Errorf("%s = null, want %q", field, *want)
	case want != nil && got != nil && *got != *want:
		t.Errorf("%s = %q, want %q", field, *got, *want)
	}
}

// TestTopQueries_MinAndMaxExecTime confirms the two new timing columns are
// returned intact and can be sorted on.
func TestTopQueries_MinAndMaxExecTime(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	rows, _ := decodeTopQueries(t, callTopQueries(t, h,
		"connection_id=4242&queryid=1001"))
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %#v", len(rows), rows)
	}
	if rows[0].MinExecTime != 1.5 {
		t.Errorf("min_exec_time = %v, want 1.5", rows[0].MinExecTime)
	}
	if rows[0].MaxExecTime != 120.25 {
		t.Errorf("max_exec_time = %v, want 120.25", rows[0].MaxExecTime)
	}

	orderings := []struct {
		name  string
		query string
		want  string
	}{
		{"min ascending",
			"connection_id=4242&order_by=min_exec_time&order=asc&limit=1",
			"1001"},
		{"min descending",
			"connection_id=4242&order_by=min_exec_time&order=desc&limit=1",
			"1006"},
		{"max ascending",
			"connection_id=4242&order_by=max_exec_time&order=asc&limit=1",
			"1006"},
		{"max descending",
			"connection_id=4242&order_by=max_exec_time&order=desc&limit=1",
			"1001"},
	}

	for _, tc := range orderings {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := decodeTopQueries(t, callTopQueries(t, h, tc.query))
			if len(got) != 1 || got[0].QueryID != tc.want {
				t.Fatalf("rows = %#v, want queryid %s first", got, tc.want)
			}
		})
	}
}

// TestTopQueriesOrderByErrorMessage confirms the 400 message enumerates every
// accepted order_by value, including the two added for issue #350.
func TestTopQueriesOrderByErrorMessage(t *testing.T) {
	for token := range validTopQueryOrderColumns {
		if !strings.Contains(topQueryOrderByError, token) {
			t.Errorf("order_by error message %q omits %q",
				topQueryOrderByError, token)
		}
	}
	if !strings.HasPrefix(topQueryOrderByError, "Invalid order_by: ") {
		t.Errorf("order_by error message = %q, want the Invalid order_by "+
			"prefix", topQueryOrderByError)
	}
}

// TestTopQueries_OffsetPaging walks the result set a page at a time and
// verifies the pages are disjoint, correctly ordered, and that the total
// stays constant regardless of the page requested.
func TestTopQueries_OffsetPaging(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	tests := []struct {
		name   string
		query  string
		want   []string
		total  string
		length int
	}{
		{"first page", "connection_id=4242&limit=2&offset=0",
			[]string{"1001", "1002"}, "6", 2},
		{"second page", "connection_id=4242&limit=2&offset=2",
			[]string{"1003", "1004"}, "6", 2},
		{"third page", "connection_id=4242&limit=2&offset=4",
			[]string{"1005", "1006"}, "6", 2},
		{"partial last page", "connection_id=4242&limit=2&offset=5",
			[]string{"1006"}, "6", 1},
		{"offset past end", "connection_id=4242&limit=2&offset=6",
			[]string{}, "6", 0},
		{"offset far past end", "connection_id=4242&limit=2&offset=1000",
			[]string{}, "6", 0},
		{"offset with ascending order",
			"connection_id=4242&limit=2&offset=1&order=asc",
			[]string{"1005", "1004"}, "6", 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, total := decodeTopQueries(t, callTopQueries(t, h, tc.query))
			if len(rows) != tc.length {
				t.Fatalf("got %d rows, want %d: %#v", len(rows), tc.length,
					rows)
			}
			if total != tc.total {
				t.Errorf("X-Total-Count = %q, want %q", total, tc.total)
			}
			got := queryIDs(rows)
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("page = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// seedTiedTopQueriesFixture inserts seven statements that all share the same
// total_exec_time, calls, and rows values. The main fixture deliberately uses
// a unique value per column so its ordering assertions are readable, which
// means it cannot exercise tie handling; this fixture exists purely so that
// paging over ties can be tested.
func seedTiedTopQueriesFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	latest := time.Now().UTC().Add(-1 * time.Minute)
	baseline := latest.Add(-5 * time.Minute)

	if _, err := pool.Exec(context.Background(),
		`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, dbid, database_name, query,
         calls, total_exec_time, mean_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES
        ($1, $2, 2001, 100, 'alpha', 'SELECT a', 5, 100, 20, 7, 1, 1),
        ($1, $2, 2002, 100, 'alpha', 'SELECT b', 5, 100, 20, 7, 1, 1),
        ($1, $2, 2003, 100, 'alpha', 'SELECT c', 5, 100, 20, 7, 1, 1),
        ($1, $2, 2004, 100, 'alpha', 'SELECT d', 5, 100, 20, 7, 1, 1),
        ($1, $2, 2005, 100, 'alpha', 'SELECT e', 5, 100, 20, 7, 1, 1),
        ($1, $2, 2006, 100, 'alpha', 'SELECT f', 5, 100, 20, 7, 1, 1),
        ($1, $2, 2007, 100, 'alpha', 'SELECT g', 5, 100, 20, 7, 1, 1),
        ($1, $3, 2001, 100, 'alpha', 'SELECT a', 0, 0, 0, 0, 0, 0),
        ($1, $3, 2002, 100, 'alpha', 'SELECT b', 0, 0, 0, 0, 0, 0),
        ($1, $3, 2003, 100, 'alpha', 'SELECT c', 0, 0, 0, 0, 0, 0),
        ($1, $3, 2004, 100, 'alpha', 'SELECT d', 0, 0, 0, 0, 0, 0),
        ($1, $3, 2005, 100, 'alpha', 'SELECT e', 0, 0, 0, 0, 0, 0),
        ($1, $3, 2006, 100, 'alpha', 'SELECT f', 0, 0, 0, 0, 0, 0),
        ($1, $3, 2007, 100, 'alpha', 'SELECT g', 0, 0, 0, 0, 0, 0)`,
		topQueriesConnID, latest, baseline); err != nil {
		t.Fatalf("tied fixture seed failed: %v", err)
	}
}

// TestTopQueries_PagingOverTiedValues walks a result set in which every row
// ties on the ordering column. The ORDER BY carries a queryid tiebreaker, so
// the sort is a total order and successive pages must visit every row exactly
// once; without the tiebreaker Postgres is free to return tied rows in any
// order per statement, which would let a page repeat or drop a row.
func TestTopQueries_PagingOverTiedValues(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTiedTopQueriesFixture(t, pool)

	all := []string{"2001", "2002", "2003", "2004", "2005", "2006", "2007"}

	orders := []struct {
		name  string
		query string
	}{
		{"descending", "connection_id=4242&limit=2&offset="},
		{"ascending", "connection_id=4242&order=asc&limit=2&offset="},
		{"ordered by calls", "connection_id=4242&order_by=calls&limit=2" +
			"&offset="},
	}

	for _, ord := range orders {
		t.Run(ord.name, func(t *testing.T) {
			seen := make([]string, 0, len(all))
			for offset := 0; offset < len(all); offset += 2 {
				rows, total := decodeTopQueries(t, callTopQueries(t, h,
					ord.query+strconv.Itoa(offset)))
				if total != "7" {
					t.Fatalf("X-Total-Count = %q at offset %d, want \"7\"",
						total, offset)
				}
				seen = append(seen, queryIDs(rows)...)
			}

			// Every row is visited exactly once across the pages: the
			// tiebreaker makes the paged sort deterministic.
			if len(seen) != len(all) {
				t.Fatalf("paging visited %d rows, want %d: %v", len(seen),
					len(all), seen)
			}
			sorted := append([]string(nil), seen...)
			sort.Strings(sorted)
			if !reflect.DeepEqual(sorted, all) {
				t.Fatalf("paging visited %v, want each of %v exactly once",
					seen, all)
			}

			// The tiebreaker sorts ascending by queryid, so with every row
			// tied on the primary key the pages come back in queryid order.
			if !reflect.DeepEqual(seen, all) {
				t.Errorf("page order = %v, want %v", seen, all)
			}
		})
	}
}

// TestTopQueries_DatabaseNameFilter verifies the filter matches the resolved
// database name exactly, that it feeds into X-Total-Count, and that it
// composes with offset and the other filters.
func TestTopQueries_DatabaseNameFilter(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	tests := []struct {
		name  string
		query string
		want  []string
		total string
	}{
		{"alpha only", "connection_id=4242&database_name=alpha",
			[]string{"1001", "1002", "1003", "1004"}, "4"},
		{"beta resolves stale name",
			"connection_id=4242&database_name=beta",
			[]string{"1005", "1006"}, "2"},
		{"raw stale name does not match",
			"connection_id=4242&database_name=stale-name",
			[]string{}, "0"},
		{"unknown database", "connection_id=4242&database_name=nope",
			[]string{}, "0"},
		{"case sensitive exact match",
			"connection_id=4242&database_name=ALPHA",
			[]string{}, "0"},
		{"with offset", "connection_id=4242&database_name=alpha&limit=2&offset=2",
			[]string{"1003", "1004"}, "4"},
		{"offset past end keeps total",
			"connection_id=4242&database_name=alpha&offset=10",
			[]string{}, "4"},
		{"with exclude_collector",
			"connection_id=4242&database_name=beta&exclude_collector=true",
			[]string{"1005"}, "1"},
		{"with queryid",
			"connection_id=4242&database_name=alpha&queryid=1002",
			[]string{"1002"}, "1"},
		{"queryid in a different database",
			"connection_id=4242&database_name=beta&queryid=1002",
			[]string{}, "0"},
		{"SQL metacharacters are bound, not interpolated",
			"connection_id=4242&database_name=alpha%27%20OR%20%271%27%3D%271",
			[]string{}, "0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, total := decodeTopQueries(t, callTopQueries(t, h, tc.query))
			if len(rows) != len(tc.want) {
				t.Fatalf("got %d rows, want %d: %#v", len(rows), len(tc.want),
					rows)
			}
			if total != tc.total {
				t.Errorf("X-Total-Count = %q, want %q", total, tc.total)
			}
			got := queryIDs(rows)
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("rows = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestTopQueries_QueryIDWithDatabaseName exercises the combination that
// pushes the database filter onto the $3 placeholder, because the queryid
// filter has already consumed $2. It walks that path with the collector
// exclusion and with paging as well, so a placeholder-numbering mistake
// would surface as a wrong row set rather than passing unnoticed.
func TestTopQueries_QueryIDWithDatabaseName(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	tests := []struct {
		name  string
		query string
		want  []string
		total string
	}{
		{"matching pair", "connection_id=4242&queryid=1003&database_name=alpha",
			[]string{"1003"}, "1"},
		{"resolved name on the stale row",
			"connection_id=4242&queryid=1005&database_name=beta",
			[]string{"1005"}, "1"},
		{"stale raw name does not match",
			"connection_id=4242&queryid=1005&database_name=stale-name",
			[]string{}, "0"},
		{"mismatched pair",
			"connection_id=4242&queryid=1003&database_name=beta",
			[]string{}, "0"},
		{"with exclude_collector",
			"connection_id=4242&queryid=1006&database_name=beta" +
				"&exclude_collector=true",
			[]string{}, "0"},
		{"with paging",
			"connection_id=4242&queryid=1003&database_name=alpha" +
				"&limit=1&offset=0",
			[]string{"1003"}, "1"},
		{"offset past the single match",
			"connection_id=4242&queryid=1003&database_name=alpha&offset=1",
			[]string{}, "1"},
		{"ascending order",
			"connection_id=4242&queryid=1002&database_name=alpha" +
				"&order_by=calls&order=asc",
			[]string{"1002"}, "1"},
		{"database metacharacters stay bound",
			"connection_id=4242&queryid=1003" +
				"&database_name=alpha%27%20OR%20%271%27%3D%271",
			[]string{}, "0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, total := decodeTopQueries(t, callTopQueries(t, h, tc.query))
			if len(rows) != len(tc.want) {
				t.Fatalf("got %d rows, want %d: %#v", len(rows), len(tc.want),
					rows)
			}
			if total != tc.total {
				t.Errorf("X-Total-Count = %q, want %q", total, tc.total)
			}
			got := queryIDs(rows)
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("rows = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestTopQueries_ExistingBehaviorPreserved covers the pre-existing
// parameters alongside the new ones, so the pagination work cannot quietly
// regress limit clamping, the order whitelist, or the filters.
func TestTopQueries_ExistingBehaviorPreserved(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	t.Run("limit clamps to at least one", func(t *testing.T) {
		rows, total := decodeTopQueries(t, callTopQueries(t, h,
			"connection_id=4242&limit=0"))
		if len(rows) != 1 || total != "6" {
			t.Fatalf("rows = %d, total = %q; want 1 row and total 6",
				len(rows), total)
		}
	})

	t.Run("limit clamps to one hundred", func(t *testing.T) {
		rows, _ := decodeTopQueries(t, callTopQueries(t, h,
			"connection_id=4242&limit=5000"))
		if len(rows) != 6 {
			t.Fatalf("got %d rows, want 6", len(rows))
		}
	})

	t.Run("order_by calls ascending", func(t *testing.T) {
		rows, _ := decodeTopQueries(t, callTopQueries(t, h,
			"connection_id=4242&order_by=calls&order=asc&limit=1"))
		if len(rows) != 1 || rows[0].QueryID != "1001" {
			t.Fatalf("rows = %#v, want queryid 1001 first", rows)
		}
	})

	t.Run("exclude_collector", func(t *testing.T) {
		rows, total := decodeTopQueries(t, callTopQueries(t, h,
			"connection_id=4242&exclude_collector=true"))
		if len(rows) != 5 || total != "5" {
			t.Fatalf("rows = %d, total = %q; want 5 and \"5\"", len(rows),
				total)
		}
	})

	t.Run("queryid filter", func(t *testing.T) {
		rows, total := decodeTopQueries(t, callTopQueries(t, h,
			"connection_id=4242&queryid=1003"))
		if len(rows) != 1 || total != "1" || rows[0].QueryID != "1003" {
			t.Fatalf("rows = %#v, total = %q", rows, total)
		}
	})

	t.Run("other connections are not visible", func(t *testing.T) {
		rows, total := decodeTopQueries(t, callTopQueries(t, h,
			"connection_id=4243"))
		if len(rows) != 1 || total != "1" || rows[0].QueryID != "7001" {
			t.Fatalf("rows = %#v, total = %q", rows, total)
		}
	})
}

// TestTopQueries_InvalidParameters checks that bad input is rejected with a
// 400 and that no X-Total-Count header is emitted on the error path.
func TestTopQueries_InvalidParameters(t *testing.T) {
	h, _, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	tests := []struct {
		name  string
		query string
	}{
		{"negative offset", "connection_id=4242&offset=-1"},
		{"large negative offset", "connection_id=4242&offset=-1000"},
		{"non-numeric offset", "connection_id=4242&offset=abc"},
		{"empty-ish offset", "connection_id=4242&offset=+"},
		{"float offset", "connection_id=4242&offset=1.5"},
		{"non-numeric limit", "connection_id=4242&limit=abc"},
		{"invalid order_by", "connection_id=4242&order_by=DROP+TABLE"},
		{"invalid order", "connection_id=4242&order=sideways"},
		{"missing connection", "limit=5"},
		{"multiple connections", "connection_ids=1,2"},
		{"non-integer queryid", "connection_id=4242&queryid=abc"},
		{"injection in queryid",
			"connection_id=4242&queryid=1001%27%20OR%20%271%27%3D%271"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := callTopQueries(t, h, tc.query)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", rec.Code,
					rec.Body.String())
			}
			if got := rec.Header().Get("X-Total-Count"); got != "" {
				t.Errorf("X-Total-Count = %q on error path, want unset", got)
			}
		})
	}
}

// TestTopQueries_EmptyOffsetParameterUsesDefault confirms an empty offset
// value falls back to zero rather than being rejected.
func TestTopQueries_EmptyOffsetParameterUsesDefault(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	rows, total := decodeTopQueries(t, callTopQueries(t, h,
		"connection_id=4242&offset=&limit="))
	if len(rows) != 6 || total != "6" {
		t.Fatalf("rows = %d, total = %q; want 6 and \"6\"", len(rows), total)
	}
}

// TestTopQueries_PermissionDenied confirms the RBAC check still guards the
// endpoint once a real auth store is wired in and the caller is neither a
// superuser nor the owner of the connection.
// TestTopQueries_TransactionBeginFailure confirms that a pool that cannot
// hand out a connection yields a 500 rather than the empty result used for
// missing metrics tables: the former is an outage, the latter is expected on
// a workbench whose collector has never run.
func TestTopQueries_TransactionBeginFailure(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	_, _ = pool.Exec(context.Background(), topQueriesTestSchemaTeardown)
	pool.Close()

	rec := callTopQueries(t, h, "connection_id=4242")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", rec.Code,
			rec.Body.String())
	}
}

func TestTopQueries_PermissionDenied(t *testing.T) {
	_, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	authStore, err := auth.NewAuthStore(t.TempDir(), 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("NewAuthStore failed: %v", err)
	}
	h := NewPerfSummaryHandler(database.NewTestDatastore(pool), authStore)

	rec := callTopQueries(t, h, "connection_id=4242&offset=1")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code,
			rec.Body.String())
	}
	if got := rec.Header().Get("X-Total-Count"); got != "" {
		t.Errorf("X-Total-Count = %q on the denied path, want unset", got)
	}
}

// TestTopQueries_MethodNotAllowed confirms non-GET requests are rejected.
func TestTopQueries_MethodNotAllowed(t *testing.T) {
	h, _, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/metrics/top-queries?connection_id=4242", nil)
	rec := httptest.NewRecorder()
	h.handleTopQueries(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// TestTopQueries_QueryErrorReturnsEmptyWithZeroTotal verifies that missing
// metrics tables still yield a 200 with an empty array and a zero total,
// which is the long-standing behavior of this endpoint.
func TestTopQueries_QueryErrorReturnsEmptyWithZeroTotal(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	if _, err := pool.Exec(context.Background(),
		topQueriesTestSchemaTeardown); err != nil {
		t.Fatalf("teardown for query-error test failed: %v", err)
	}

	rec := callTopQueries(t, h, "connection_id=4242&offset=3&database_name=alpha")
	rows, total := decodeTopQueries(t, rec)
	if len(rows) != 0 {
		t.Fatalf("got %d rows, want 0", len(rows))
	}
	if total != "0" {
		t.Errorf("X-Total-Count = %q, want \"0\"", total)
	}
	// The body must remain a bare JSON array, not an object wrapper.
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("body = %s, want []", body)
	}
}

// TestTopQueries_ScanErrorSkipsRow verifies a row that fails to scan is
// skipped without failing the request, whilst the total still counts it.
// TestTopQueries_ExcludesInternalMarkerToo covers the second marker the
// filter matches. The shared fixture seeds a statement carrying the
// collector's probe column alias; this adds one carrying the
// in-statement comment the Workbench puts on its own datastore traffic,
// so both classes are proven to be hidden rather than just the first.
func TestTopQueries_ExcludesInternalMarkerToo(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	// The marked statement is the captured query *text* being seeded, so
	// it is bound as $2 rather than being part of the statement run
	// here. Built before the call so that no concatenation appears in
	// the Exec argument list, which static analysis reads as a SQL
	// injection shape even when the value is parameterised.
	markedQueryText := sqlmarker.Tag("INSERT INTO metrics.pg_stat_activity")

	// Two samples are seeded, matching the shared fixture: a zeroed
	// baseline five minutes earlier and the reading itself, so the
	// statement has a delta inside the window and appears at all.
	const seedMarkedRow = `
        INSERT INTO metrics.pg_stat_statements
            (connection_id, collected_at, queryid, dbid, database_name,
             query, calls, total_exec_time, mean_exec_time, rows,
             shared_blks_hit, shared_blks_read)
         SELECT $1, MAX(collected_at), 1007, 100, 'alpha',
             $2, 70, 50, 1, 70, 700, 7
         FROM metrics.pg_stat_statements WHERE connection_id = $1
         UNION ALL
         SELECT $1, MAX(collected_at) - INTERVAL '5 minutes', 1007, 100,
             'alpha', $2, 0, 0, 0, 0, 0, 0
         FROM metrics.pg_stat_statements WHERE connection_id = $1`

	if _, err := pool.Exec(context.Background(), seedMarkedRow,
		topQueriesConnID, markedQueryText); err != nil {
		t.Fatalf("seeding an internally marked statement: %v", err)
	}

	// Unfiltered, it is present alongside the six fixture rows.
	rows, total := decodeTopQueries(t, callTopQueries(t, h,
		"connection_id=4242&limit=100"))
	if len(rows) != 7 || total != "7" {
		t.Fatalf("unfiltered rows = %d, total = %q; want 7 and \"7\"",
			len(rows), total)
	}

	// Filtered, both marked statements are gone.
	rows, total = decodeTopQueries(t, callTopQueries(t, h,
		"connection_id=4242&limit=100&exclude_collector=true"))
	if len(rows) != 5 || total != "5" {
		t.Fatalf("filtered rows = %d, total = %q; want 5 and \"5\"",
			len(rows), total)
	}
	for _, r := range rows {
		if strings.Contains(r.Query, sqlmarker.Marker) {
			t.Errorf("internally marked statement survived the filter: %s",
				r.Query)
		}
	}
}

func TestTopQueries_NullQueryRowIsRetained(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	// A NULL query column used to fail the scan and drop the row. Issue
	// #364 changed that deliberately: a row that cannot be positively
	// identified as Workbench traffic must be shown rather than hidden,
	// so the column is scanned through a nullable destination and the
	// row comes back with empty query text.
	if _, err := pool.Exec(context.Background(),
		`ALTER TABLE metrics.pg_stat_statements ALTER COLUMN query DROP NOT NULL`,
	); err != nil {
		t.Fatalf("alter table failed: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`UPDATE metrics.pg_stat_statements SET query = NULL
         WHERE connection_id = $1 AND queryid = 1006`,
		topQueriesConnID); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	rows, total := decodeTopQueries(t, callTopQueries(t, h,
		"connection_id=4242"))
	if len(rows) != 6 {
		t.Fatalf("got %d rows, want 6 (the NULL-query row is kept): %#v",
			len(rows), rows)
	}
	if rows[len(rows)-1].Query != "" {
		t.Errorf("NULL query rendered as %q, want an empty string",
			rows[len(rows)-1].Query)
	}
	if total != "6" {
		t.Errorf("X-Total-Count = %q, want \"6\"", total)
	}
}

// seedLastClientKeyFixture seeds a snapshot in which one queryid was
// observed running under two different (database, role) pairs, alongside a
// backend that connected over a Unix-domain socket and a statement that was
// never caught in flight. It returns the snapshot time and the two activity
// sample times.
func seedLastClientKeyFixture(
	t *testing.T,
	pool *pgxpool.Pool,
) (latest, alphaSeen, localSeen time.Time) {
	t.Helper()
	ctx := context.Background()

	latest = time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Microsecond)
	alphaSeen = latest.Add(-20 * time.Minute)
	localSeen = latest.Add(-3 * time.Minute)
	betaSeen := latest.Add(-2 * time.Minute)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed exec failed: %v\nSQL: %s", err, sql)
		}
	}

	// Every statement in the snapshot belongs to database alpha (dbid 100)
	// and role alice (usesysid 10). Query 2001 is nonetheless observed in
	// flight under both alpha/alice and beta/bob, the latter more
	// recently: keying last_client on the queryid alone would report the
	// beta client against an alpha statement. Each statement is seeded
	// twice, with a zeroed baseline five minutes earlier, so that the
	// windowed aggregation has a pair to difference and the statement
	// appears at all.
	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, userid, dbid, database_name,
         query, calls, total_exec_time, mean_exec_time,
         min_exec_time, max_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES
        ($1, $2, 2001, 10, 100, 'alpha', 'SELECT shared', 10, 300, 30,
         1, 50, 10, 100, 1),
        ($1, $2, 2002, 10, 100, 'alpha', 'SELECT socket', 20, 200, 10,
         1, 40, 20, 200, 2),
        ($1, $2, 2003, 10, 100, 'alpha', 'SELECT unseen', 30, 100, 3,
         1, 30, 30, 300, 3),
        ($1, $3, 2001, 10, 100, 'alpha', 'SELECT shared', 0, 0, 0,
         0, 0, 0, 0, 0),
        ($1, $3, 2002, 10, 100, 'alpha', 'SELECT socket', 0, 0, 0,
         0, 0, 0, 0, 0),
        ($1, $3, 2003, 10, 100, 'alpha', 'SELECT unseen', 0, 0, 0,
         0, 0, 0, 0, 0)`,
		topQueriesConnID, latest, latest.Add(-5*time.Minute))

	// OID-to-name samples, then the activity samples themselves. The
	// duplicate alpha sample for 2001 also proves DISTINCT ON still
	// collapses the CTE to one row per (query_id, datid, usesysid), so the
	// join cannot fan deduped out.
	exec(`INSERT INTO metrics.pg_stat_activity
        (connection_id, collected_at, datid, datname, usesysid, usename,
         client_addr, client_hostname, query_id)
        VALUES
        ($1, $2, 100, 'alpha', 10, 'alice', NULL, NULL, NULL),
        ($1, $2, 200, 'beta', 20, 'bob', NULL, NULL, NULL),
        ($1, $3, 100, 'alpha', 10, 'alice',
         '192.0.2.10', 'alpha-client.example.com', 2001),
        ($1, $6, 100, 'alpha', 10, 'alice',
         '192.0.2.11', 'alpha-older.example.com', 2001),
        ($1, $5, 200, 'beta', 20, 'bob',
         '198.51.100.7', 'beta-client.example.com', 2001),
        ($1, $4, 100, 'alpha', 10, 'alice', NULL, NULL, 2002)`,
		topQueriesConnID, latest, alphaSeen, localSeen, betaSeen,
		alphaSeen.Add(-1*time.Minute))

	return latest, alphaSeen, localSeen
}

// TestTopQueries_LastClientKeyedOnDatabaseAndRole covers the three-way key
// on last_client. pg_stat_statements is keyed on (userid, dbid, queryid,
// toplevel), so one queryid can be in flight under several databases and
// roles at once; the client reported for a statement must be the one seen
// under that statement's own database and role, not simply the most recent
// backend to run the queryid anywhere. It also pins the two null cases
// apart: a Unix-domain-socket backend reports "local" with a non-null
// observation time, whereas a statement never caught in flight reports
// nulls throughout.
func TestTopQueries_LastClientKeyedOnDatabaseAndRole(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	_, alphaSeen, localSeen := seedLastClientKeyFixture(t, pool)

	strPtr := func(v string) *string { return &v }
	tests := []struct {
		queryID        string
		wantAddr       *string
		wantHostname   *string
		wantObservedAt *time.Time
	}{
		{
			queryID:        "2001",
			wantAddr:       strPtr("192.0.2.10"),
			wantHostname:   strPtr("alpha-client.example.com"),
			wantObservedAt: &alphaSeen,
		},
		{
			queryID:        "2002",
			wantAddr:       strPtr("local"),
			wantObservedAt: &localSeen,
		},
		{queryID: "2003"},
	}

	assertRows := func(t *testing.T, rows []TopQueryRow, want []string) {
		t.Helper()
		byQueryID := make(map[string]TopQueryRow, len(rows))
		for _, row := range rows {
			if _, dup := byQueryID[row.QueryID]; dup {
				t.Fatalf("queryid %s returned more than once", row.QueryID)
			}
			byQueryID[row.QueryID] = row
		}
		for _, tc := range tests {
			wanted := false
			for _, id := range want {
				if id == tc.queryID {
					wanted = true
				}
			}
			if !wanted {
				continue
			}
			row, ok := byQueryID[tc.queryID]
			if !ok {
				t.Fatalf("queryid %s missing from results", tc.queryID)
			}
			if row.DatabaseName != "alpha" {
				t.Errorf("queryid %s database_name = %q, want alpha",
					tc.queryID, row.DatabaseName)
			}
			assertOptionalString(t, "client_addr", row.ClientAddr,
				tc.wantAddr)
			assertOptionalString(t, "client_hostname", row.ClientHostname,
				tc.wantHostname)
			switch {
			case tc.wantObservedAt == nil && row.ClientObservedAt != nil:
				t.Errorf("queryid %s client_observed_at = %v, want null",
					tc.queryID, *row.ClientObservedAt)
			case tc.wantObservedAt != nil && row.ClientObservedAt == nil:
				t.Errorf("queryid %s client_observed_at = null, want %v",
					tc.queryID, *tc.wantObservedAt)
			case tc.wantObservedAt != nil &&
				!row.ClientObservedAt.Equal(*tc.wantObservedAt):
				t.Errorf("queryid %s client_observed_at = %v, want %v",
					tc.queryID, *row.ClientObservedAt, *tc.wantObservedAt)
			}
		}
	}

	t.Run("unfiltered", func(t *testing.T) {
		rows, total := decodeTopQueries(t,
			callTopQueries(t, h, "connection_id=4242"))
		if total != "3" || len(rows) != 3 {
			t.Fatalf("total = %s, rows = %d, want 3 and 3: %#v",
				total, len(rows), rows)
		}
		assertRows(t, rows, []string{"2001", "2002", "2003"})
	})

	// The drill-down path, which pushes the queryid predicate into
	// last_client as well as into deduped. The attribution must be
	// identical to the unfiltered read.
	t.Run("queryid_filter", func(t *testing.T) {
		rows, total := decodeTopQueries(t,
			callTopQueries(t, h, "connection_id=4242&queryid=2001"))
		if total != "1" || len(rows) != 1 {
			t.Fatalf("total = %s, rows = %d, want 1 and 1: %#v",
				total, len(rows), rows)
		}
		assertRows(t, rows, []string{"2001"})
	})

	// A statement never caught in flight is signaled by a null
	// client_observed_at, since client_addr is now non-null for every
	// observed row.
	t.Run("unobserved_queryid_filter", func(t *testing.T) {
		rows, _ := decodeTopQueries(t,
			callTopQueries(t, h, "connection_id=4242&queryid=2003"))
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want 1: %#v", len(rows), rows)
		}
		if rows[0].ClientObservedAt != nil {
			t.Errorf("client_observed_at = %v, want null",
				*rows[0].ClientObservedAt)
		}
		if rows[0].ClientAddr != nil {
			t.Errorf("client_addr = %q, want null", *rows[0].ClientAddr)
		}
	})
}

// seedWindowedTopQueries inserts a controlled series of samples for a set of
// statements whose behavior inside the window differs: one that runs
// steadily, one whose counters are reset partway through, one that is
// sampled only once, one that first appears midway through the window, and
// one that stops being reported midway through it. Every sample carries the
// same identity (database, role, dbid, toplevel), so the deltas are taken
// across the series in collection order.
//
// The returned time is the anchor the offsets are measured back from, so a
// test can build a custom window around any part of the series.
func seedWindowedTopQueries(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()

	// Truncated to the second so that a custom window built from
	// RFC 3339 text, which has no sub-second component, lines up exactly
	// with the seeded collection times.
	anchor := time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Second)
	at := func(minutesBefore int) time.Time {
		return anchor.Add(-time.Duration(minutesBefore) * time.Minute)
	}

	// queryid, minutes before the anchor, calls, total_exec_time, rows.
	samples := []struct {
		queryID  int64
		at       int
		calls    int64
		execTime float64
		rows     int64
	}{
		// 3001 runs steadily: three pairs of ten calls each.
		{3001, 30, 100, 1000, 100},
		{3001, 20, 110, 1100, 110},
		{3001, 10, 120, 1200, 120},
		{3001, 0, 130, 1300, 130},
		// 3002 is reset between the second and third samples: the pair
		// spanning the reset is discarded, the rest survive.
		{3002, 30, 500, 5000, 500},
		{3002, 20, 520, 5200, 520},
		{3002, 10, 3, 30, 3},
		{3002, 0, 11, 110, 11},
		// 3003 is sampled once and so has no predecessor to difference
		// against.
		{3003, 20, 900, 9000, 900},
		// 3004 first appears midway through the window.
		{3004, 10, 40, 400, 40},
		{3004, 0, 47, 470, 47},
		// 3005 stops being reported midway through: its earlier deltas
		// still count.
		{3005, 30, 60, 600, 60},
		{3005, 20, 75, 750, 75},
	}

	ctx := context.Background()
	for _, smp := range samples {
		if _, err := pool.Exec(ctx,
			`INSERT INTO metrics.pg_stat_statements
            (connection_id, collected_at, queryid, userid, dbid,
             database_name, query, calls, total_exec_time, mean_exec_time,
             min_exec_time, max_exec_time, rows,
             shared_blks_hit, shared_blks_read)
            VALUES ($1, $2, $3, 10, 100, 'alpha', 'SELECT ' || $3::bigint::text,
                    $4, $5, 1, 1.25, 99.5, $6, $4, $4)`,
			topQueriesConnID, at(smp.at), smp.queryID, smp.calls,
			smp.execTime, smp.rows); err != nil {
			t.Fatalf("windowed fixture seed failed: %v", err)
		}
	}
	return anchor
}

// TestTopQueries_WindowedDeltas covers the aggregation introduced for issue
// #387: the reported figures are summed counter deltas over the requested
// window rather than a single snapshot reading. A statement with no usable
// pair, or with no calls at all in the window, does not appear.
func TestTopQueries_WindowedDeltas(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedWindowedTopQueries(t, pool)

	rows, total := decodeTopQueries(t, callTopQueries(t, h,
		"connection_id=4242&time_range=1h&limit=100"))
	if total != "4" {
		t.Errorf("X-Total-Count = %q, want \"4\"", total)
	}

	byQueryID := make(map[string]TopQueryRow, len(rows))
	for _, row := range rows {
		byQueryID[row.QueryID] = row
	}
	if _, ok := byQueryID["3003"]; ok {
		t.Errorf("the single-sample statement has no delta pair and must "+
			"not appear: %#v", byQueryID["3003"])
	}

	tests := []struct {
		name     string
		queryID  string
		calls    int64
		execTime float64
		rows     int64
	}{
		{"steady statement sums every pair", "3001", 30, 300, 30},
		// 520 - 500 = 20 survives, the reset pair is dropped, and
		// 11 - 3 = 8 follows it.
		{"counter reset discards only the offending pair", "3002", 28, 280,
			28},
		{"statement appearing mid-window", "3004", 7, 70, 7},
		{"statement disappearing mid-window", "3005", 15, 150, 15},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row, ok := byQueryID[tc.queryID]
			if !ok {
				t.Fatalf("queryid %s missing from %#v", tc.queryID, rows)
			}
			if row.Calls != tc.calls {
				t.Errorf("calls = %d, want %d", row.Calls, tc.calls)
			}
			if row.TotalExecTime != tc.execTime {
				t.Errorf("total_exec_time = %v, want %v", row.TotalExecTime,
					tc.execTime)
			}
			if row.Rows != tc.rows {
				t.Errorf("rows = %d, want %d", row.Rows, tc.rows)
			}
			// mean_exec_time is derived from the window, not read from
			// the sample, which carries 1 in every row.
			if want := tc.execTime / float64(tc.calls); row.MeanExecTime !=
				want {
				t.Errorf("mean_exec_time = %v, want %v", row.MeanExecTime,
					want)
			}
			// min and max stay lifetime values taken from the latest
			// sample in the window.
			if row.MinExecTime != 1.25 || row.MaxExecTime != 99.5 {
				t.Errorf("min/max exec time = %v/%v, want 1.25/99.5",
					row.MinExecTime, row.MaxExecTime)
			}
		})
	}
}

// TestTopQueries_CustomWindowNarrowsTheResult confirms that explicit bounds
// select the sample pairs they cover and nothing else, including the case
// of a window so narrow that it contains a single sample and therefore no
// pairs at all.
func TestTopQueries_CustomWindowNarrowsTheResult(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	anchor := seedWindowedTopQueries(t, pool)

	custom := func(startMinutes, endMinutes int) string {
		start := anchor.Add(-time.Duration(startMinutes) * time.Minute)
		end := anchor.Add(-time.Duration(endMinutes) * time.Minute)
		return "connection_id=4242&limit=100&time_range=custom" +
			"&time_start=" + start.Format(time.RFC3339) +
			"&time_end=" + end.Format(time.RFC3339)
	}

	t.Run("last ten minutes only", func(t *testing.T) {
		rows, total := decodeTopQueries(t, callTopQueries(t, h,
			custom(10, 0)))
		if total != "3" {
			t.Fatalf("X-Total-Count = %q, want \"3\"; rows %#v", total, rows)
		}
		got := queryIDs(rows)
		sort.Strings(got)
		want := []string{"3001", "3002", "3004"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("rows = %v, want %v (3005 stopped reporting earlier)",
				got, want)
		}
		for _, row := range rows {
			if row.QueryID == "3001" && row.Calls != 10 {
				t.Errorf("3001 calls = %d, want 10 for a single pair",
					row.Calls)
			}
		}
	})

	t.Run("a window holding one sample has no pairs", func(t *testing.T) {
		rows, total := decodeTopQueries(t, callTopQueries(t, h,
			custom(21, 19)))
		if len(rows) != 0 || total != "0" {
			t.Fatalf("rows = %#v, total = %q; want none, because a single "+
				"sample cannot be differenced", rows, total)
		}
		if body := strings.TrimSpace(
			callTopQueries(t, h, custom(21, 19)).Body.String()); body !=
			"[]" {
			t.Errorf("body = %s, want []", body)
		}
	})

	t.Run("a window before the series is empty", func(t *testing.T) {
		rows, total := decodeTopQueries(t, callTopQueries(t, h,
			custom(120, 60)))
		if len(rows) != 0 || total != "0" {
			t.Fatalf("rows = %#v, total = %q; want none", rows, total)
		}
	})
}

// TestTopQueries_InvalidTimeWindow confirms every rejection from
// metrics.ResolveTimeWindow reaches the client as a 400 carrying the
// resolver's own wording, so the endpoint cannot drift from the rest of the
// metrics API.
func TestTopQueries_InvalidTimeWindow(t *testing.T) {
	h, _, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	now := time.Now().UTC()
	rfc := func(d time.Duration) string {
		return now.Add(d).Format(time.RFC3339)
	}

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "unknown preset",
			query: "connection_id=4242&time_range=5y",
			want: "invalid time range \"5y\": must be one of 1h, 6h, 24h, " +
				"7d, 30d, custom",
		},
		{
			name:  "custom without bounds",
			query: "connection_id=4242&time_range=custom",
			want: "invalid time range \"custom\": time_start and time_end " +
				"are both required",
		},
		{
			name: "custom with only a start",
			query: "connection_id=4242&time_range=custom&time_start=" +
				rfc(-time.Hour),
			want: "invalid time range \"custom\": time_start and time_end " +
				"are both required",
		},
		{
			name: "unparseable start",
			query: "connection_id=4242&time_range=custom&time_start=nonsense" +
				"&time_end=" + rfc(0),
			want: "invalid time_start \"nonsense\": must be an RFC 3339 " +
				"timestamp",
		},
		{
			name: "unparseable end",
			query: "connection_id=4242&time_range=custom&time_start=" +
				rfc(-time.Hour) + "&time_end=nonsense",
			want: "invalid time_end \"nonsense\": must be an RFC 3339 " +
				"timestamp",
		},
		{
			name: "end before start",
			query: "connection_id=4242&time_range=custom&time_start=" +
				rfc(-time.Hour) + "&time_end=" + rfc(-2*time.Hour),
			want: "invalid time range: time_end must be after time_start",
		},
		{
			name: "start in the future",
			query: "connection_id=4242&time_range=custom&time_start=" +
				rfc(time.Hour) + "&time_end=" + rfc(2*time.Hour),
			want: "invalid time_start: must not be in the future",
		},
		{
			name: "span beyond the maximum",
			query: "connection_id=4242&time_range=custom&time_start=" +
				rfc(-400*24*time.Hour) + "&time_end=" + rfc(0),
			want: "invalid time range: span must not exceed 366 days",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := callTopQueries(t, h, tc.query)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", rec.Code,
					rec.Body.String())
			}
			var body struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("error body is not JSON: %v; body: %s", err,
					rec.Body.String())
			}
			// The message must be the resolver's own, verbatim.
			if body.Error != tc.want {
				t.Errorf("error = %q, want %q", body.Error, tc.want)
			}
			if got := rec.Header().Get("X-Total-Count"); got != "" {
				t.Errorf("X-Total-Count = %q on the error path, want unset",
					got)
			}
		})
	}
}

// TestTopQueries_PresetWindowExcludesOlderSamples confirms the preset
// ranges bound the aggregation: a pair that falls entirely before the
// requested range contributes nothing, whilst a wider preset picks it up.
func TestTopQueries_PresetWindowExcludesOlderSamples(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	now := time.Now().UTC()
	exec := func(at time.Time, queryID int64, calls int64) {
		t.Helper()
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO metrics.pg_stat_statements
            (connection_id, collected_at, queryid, userid, dbid,
             database_name, query, calls, total_exec_time, mean_exec_time,
             min_exec_time, max_exec_time, rows,
             shared_blks_hit, shared_blks_read)
            VALUES ($1, $2, $3, 10, 100, 'alpha', 'SELECT 1', $4, $5, 1,
                    1, 1, $4, $4, $4)`,
			topQueriesConnID, at, queryID, calls,
			float64(calls)); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}

	// An old pair three hours back and a recent pair ten minutes back.
	exec(now.Add(-3*time.Hour), 4001, 10)
	exec(now.Add(-170*time.Minute), 4001, 25)
	exec(now.Add(-10*time.Minute), 4002, 5)
	exec(now.Add(-5*time.Minute), 4002, 9)

	t.Run("one hour sees only the recent pair", func(t *testing.T) {
		rows, total := decodeTopQueries(t, callTopQueries(t, h,
			"connection_id=4242&time_range=1h"))
		if len(rows) != 1 || total != "1" || rows[0].QueryID != "4002" {
			t.Fatalf("rows = %#v, total = %q; want only queryid 4002", rows,
				total)
		}
		if rows[0].Calls != 4 {
			t.Errorf("calls = %d, want 4", rows[0].Calls)
		}
	})

	t.Run("six hours sees both", func(t *testing.T) {
		rows, total := decodeTopQueries(t, callTopQueries(t, h,
			"connection_id=4242&time_range=6h&order_by=calls&order=desc"))
		if len(rows) != 2 || total != "2" {
			t.Fatalf("rows = %#v, total = %q; want two", rows, total)
		}
		if rows[0].QueryID != "4001" || rows[0].Calls != 15 {
			t.Errorf("first row = %#v, want queryid 4001 with 15 calls",
				rows[0])
		}
	})
}

// seedMultiDatabaseProbeFixture reproduces what the collector stores on a
// server with pg_stat_statements installed in more than one database. The
// probe runs in every such database and reads the same cluster-wide view
// each time, so one counter, identified by (queryid, userid, dbid,
// toplevel), lands once per probing database at the same collected_at,
// differing only in database_name. Here queryid 5001 grows by ten calls and
// 100 ms per five-minute sample and is stored under both "alpha" and
// "postgres" on each of three samples. It also seeds one statement whose
// counters genuinely differ per database: queryid 5002 runs in dbid 100
// ("alpha", 1,200 calls in the window) and in dbid 200 ("beta", 12 calls),
// each identity again stored under both probing databases.
func seedMultiDatabaseProbeFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	latest := time.Now().UTC().Add(-1 * time.Minute)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed exec failed: %v\nSQL: %s", err, sql)
		}
	}

	exec(`INSERT INTO metrics.pg_stat_activity
        (connection_id, collected_at, datid, datname, usesysid, usename)
        VALUES ($1, $2, 100, 'alpha', 10, 'alice'),
               ($1, $2, 200, 'beta', 10, 'alice')`,
		topQueriesConnID, latest)

	for i, minutesBefore := range []int{10, 5, 0} {
		at := latest.Add(-time.Duration(minutesBefore) * time.Minute)
		calls := int64(100 + 10*i)
		for _, probeDB := range []string{"alpha", "postgres"} {
			exec(`INSERT INTO metrics.pg_stat_statements
                (connection_id, collected_at, queryid, userid, dbid,
                 database_name, query, calls, total_exec_time,
                 mean_exec_time, min_exec_time, max_exec_time, rows,
                 shared_blks_hit, shared_blks_read)
                VALUES ($1, $2, 5001, 10, 100, $3, 'SELECT shared', $4, $5,
                        10, 1, 20, $4, $4, $4),
                       ($1, $2, 5002, 10, 100, $3, 'SELECT per db', $6, $7,
                        1, 1, 2, $6, $6, $6),
                       ($1, $2, 5002, 10, 200, $3, 'SELECT per db', $8, $9,
                        1, 1, 2, $8, $8, $8)`,
				topQueriesConnID, at, probeDB,
				calls, float64(calls)*10,
				int64(600*i), float64(600*i),
				int64(6*i), float64(6*i))
		}
	}
}

// TestTopQueries_OneCounterStoredUnderSeveralDatabases covers the first
// blocking finding on the #387 review: a counter the probe sampled through
// two databases must be counted once, not once per database. Two pairs of
// ten calls and 100 ms give 20 calls and 200 ms; partitioning the LAG on
// database_name as well would have reported 40 and 400.
func TestTopQueries_OneCounterStoredUnderSeveralDatabases(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedMultiDatabaseProbeFixture(t, pool)

	rows, total := decodeTopQueries(t, callTopQueries(t, h,
		"connection_id=4242&time_range=1h&queryid=5001"))
	if total != "1" || len(rows) != 1 {
		t.Fatalf("total = %s, rows = %d, want 1 and 1: %#v", total,
			len(rows), rows)
	}
	row := rows[0]
	if row.Calls != 20 {
		t.Errorf("calls = %d, want 20 (one copy per sample, two pairs)",
			row.Calls)
	}
	if row.TotalExecTime != 200 {
		t.Errorf("total_exec_time = %v, want 200", row.TotalExecTime)
	}
	if row.Rows != 20 || row.SharedBlksHit != 20 || row.SharedBlksRead != 20 {
		t.Errorf("rows/hit/read = %d/%d/%d, want 20 each", row.Rows,
			row.SharedBlksHit, row.SharedBlksRead)
	}
	if row.MeanExecTime != 10 {
		t.Errorf("mean_exec_time = %v, want 10", row.MeanExecTime)
	}
	if row.DatabaseName != "alpha" {
		t.Errorf("database_name = %q, want alpha (resolved from dbid 100)",
			row.DatabaseName)
	}
	if row.Query != "SELECT shared" {
		t.Errorf("query = %q, want the sampled text", row.Query)
	}
}

// TestTopQueries_DatabaseFilterSumsOneDatabase covers the second blocking
// finding on the #387 review: the database filter must select which
// counters are summed rather than filter the summed row. queryid 5002 runs
// 1,200 calls in alpha and 12 in beta; asking for alpha must return 1,200,
// asking for beta must return 12, and asking for neither returns the sum.
func TestTopQueries_DatabaseFilterSumsOneDatabase(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedMultiDatabaseProbeFixture(t, pool)

	tests := []struct {
		name         string
		query        string
		wantCalls    int64
		wantTime     float64
		wantDatabase string
	}{
		{"alpha only", "&database_name=alpha", 1200, 1200, "alpha"},
		{"beta only", "&database_name=beta", 12, 12, "beta"},
		{"unfiltered sums both", "", 1212, 1212, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, total := decodeTopQueries(t, callTopQueries(t, h,
				"connection_id=4242&time_range=1h&queryid=5002"+tc.query))
			if total != "1" || len(rows) != 1 {
				t.Fatalf("total = %s, rows = %d, want 1 and 1: %#v",
					total, len(rows), rows)
			}
			if rows[0].Calls != tc.wantCalls {
				t.Errorf("calls = %d, want %d", rows[0].Calls, tc.wantCalls)
			}
			if rows[0].TotalExecTime != tc.wantTime {
				t.Errorf("total_exec_time = %v, want %v",
					rows[0].TotalExecTime, tc.wantTime)
			}
			if tc.wantDatabase != "" && rows[0].DatabaseName != tc.wantDatabase {
				t.Errorf("database_name = %q, want %q", rows[0].DatabaseName,
					tc.wantDatabase)
			}
		})
	}

	// The probing database is not a statement database: filtering on it
	// must match nothing even though every sample row was stored under it.
	rows, total := decodeTopQueries(t, callTopQueries(t, h,
		"connection_id=4242&time_range=1h&database_name=postgres"))
	if total != "0" || len(rows) != 0 {
		t.Errorf("database_name=postgres: total = %s, rows = %#v; want none",
			total, rows)
	}

	// Without a queryid, the leaderboard for beta holds only the beta
	// identity of 5002, and the count agrees with the page.
	rows, total = decodeTopQueries(t, callTopQueries(t, h,
		"connection_id=4242&time_range=1h&database_name=beta"))
	if total != "1" || len(rows) != 1 || rows[0].QueryID != "5002" {
		t.Fatalf("beta leaderboard: total = %s, rows = %#v; want 5002 only",
			total, rows)
	}
	if rows[0].Calls != 12 {
		t.Errorf("beta leaderboard calls = %d, want 12", rows[0].Calls)
	}
}
