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
`

const topQueriesTestSchemaTeardown = `
DROP TABLE IF EXISTS metrics.pg_stat_statements CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_activity CASCADE;
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

// seedTopQueriesFixture inserts six statements in the latest snapshot: four
// against database "alpha" and two against "beta". One "beta" row carries a
// stale pss.database_name that must be overridden by the pg_stat_activity
// lookup, which proves the database_name filter runs against the resolved
// name. A stale older snapshot and a collector probe query are seeded too,
// so the latest-snapshot and exclude_collector behavior stay covered.
func seedTopQueriesFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	latest := time.Now().UTC().Add(-1 * time.Minute)
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

	// Older snapshot for the same connection: excluded by the
	// collected_at = MAX(collected_at) filter.
	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, userid, dbid, database_name,
         query, calls, total_exec_time, mean_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES ($1, $2, 9001, 10, 100, 'alpha', 'SELECT old', 1, 9999, 9999, 1,
                1, 1)`,
		topQueriesConnID, older)

	// A different connection that must never leak into the results.
	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, userid, dbid, database_name,
         query, calls, total_exec_time, mean_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES ($1, $2, 7001, 10, 100, 'alpha', 'SELECT other', 1, 8888, 8888,
                1, 1, 1)`,
		topQueriesConnID+1, latest)
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

	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, userid, dbid, database_name,
         query, calls, total_exec_time, mean_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES ($1, $2, 1001, 10, 100, 'alpha-probe', 'SELECT 1', 10, 600,
                60, 10, 100, 1),
               ($1, $2, 1002, 20, 200, 'beta-probe', 'SELECT 2', 20, 500,
                25, 20, 200, 2)`,
		topQueriesConnID, latest)

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
        ($1, $2, 2007, 100, 'alpha', 'SELECT g', 5, 100, 20, 7, 1, 1)`,
		topQueriesConnID, latest); err != nil {
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

	authStore, err := auth.NewAuthStore(t.TempDir(), 0, 0)
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

	const seedMarkedRow = `
        INSERT INTO metrics.pg_stat_statements
            (connection_id, collected_at, queryid, dbid, database_name,
             query, calls, total_exec_time, mean_exec_time, rows,
             shared_blks_hit, shared_blks_read)
         SELECT $1, MAX(collected_at), 1007, 100, 'alpha',
             $2, 70, 50, 1, 70, 700, 7
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
	// beta client against an alpha statement.
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
         1, 30, 30, 300, 3)`,
		topQueriesConnID, latest)

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
