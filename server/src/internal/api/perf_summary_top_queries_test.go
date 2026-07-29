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
    dbid              bigint,
    database_name     text,
    query             text        NOT NULL,
    calls             bigint      NOT NULL DEFAULT 0,
    total_exec_time   double precision NOT NULL DEFAULT 0,
    mean_exec_time    double precision NOT NULL DEFAULT 0,
    rows              bigint      NOT NULL DEFAULT 0,
    shared_blks_hit   bigint      NOT NULL DEFAULT 0,
    shared_blks_read  bigint      NOT NULL DEFAULT 0
);

CREATE TABLE metrics.pg_stat_activity (
    connection_id  integer     NOT NULL,
    collected_at   timestamptz NOT NULL,
    datid          bigint,
    datname        text
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

	// datid -> datname mapping used by the db_names CTE.
	exec(`INSERT INTO metrics.pg_stat_activity
        (connection_id, collected_at, datid, datname)
        VALUES ($1, $2, 100, 'alpha'), ($1, $2, 200, 'beta')`,
		topQueriesConnID, latest)

	// Latest snapshot. total_exec_time is unique per row so the default
	// ordering is deterministic.
	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, dbid, database_name, query,
         calls, total_exec_time, mean_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES
        ($1, $2, 1001, 100, 'alpha', 'SELECT 1', 10, 600, 60, 10, 100, 1),
        ($1, $2, 1002, 100, 'alpha', 'SELECT 2', 20, 500, 25, 20, 200, 2),
        ($1, $2, 1003, 100, 'alpha', 'SELECT 3', 30, 400, 13, 30, 300, 3),
        ($1, $2, 1004, 100, 'alpha', 'SELECT 4', 40, 300, 7,  40, 400, 4),
        ($1, $2, 1005, 200, 'stale-name', 'SELECT 5', 50, 200, 4, 50, 500, 5),
        ($1, $2, 1006, 200, 'beta', 'SELECT 6 ai_dba_wb_probe', 60, 100, 2,
         60, 600, 6)`,
		topQueriesConnID, latest)

	// Older snapshot for the same connection: excluded by the
	// collected_at = MAX(collected_at) filter.
	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, dbid, database_name, query,
         calls, total_exec_time, mean_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES ($1, $2, 9001, 100, 'alpha', 'SELECT old', 1, 9999, 9999, 1,
                1, 1)`,
		topQueriesConnID, older)

	// A different connection that must never leak into the results.
	exec(`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, dbid, database_name, query,
         calls, total_exec_time, mean_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES ($1, $2, 7001, 100, 'alpha', 'SELECT other', 1, 8888, 8888, 1,
                1, 1)`,
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
		{"queryid metacharacters stay bound",
			"connection_id=4242&database_name=alpha" +
				"&queryid=1003%27%20OR%20%271%27%3D%271",
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
func TestTopQueries_ScanErrorSkipsRow(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	// A NULL query column fails the scan into a string destination. The
	// last row in the default ordering is used so the rows preceding it
	// are still delivered.
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
	if len(rows) != 5 {
		t.Fatalf("got %d rows, want 5 (the unscannable row is skipped): %#v",
			len(rows), rows)
	}
	if total != "6" {
		t.Errorf("X-Total-Count = %q, want \"6\"", total)
	}
}
