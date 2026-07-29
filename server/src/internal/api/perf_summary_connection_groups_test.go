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
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"golang.org/x/crypto/bcrypt"
)

// connectionGroupsTestSchema mirrors the columns of metrics.pg_stat_activity
// that the connection-groups aggregation reads. The types match the
// collector's schema, notably client_addr as INET, so that the host()
// extraction behaves exactly as it does in production.
const connectionGroupsTestSchema = `
CREATE SCHEMA IF NOT EXISTS metrics;
DROP TABLE IF EXISTS metrics.pg_stat_activity CASCADE;

CREATE TABLE metrics.pg_stat_activity (
    connection_id    integer     NOT NULL,
    collected_at     timestamptz NOT NULL,
    pid              integer     NOT NULL,
    usename          text,
    datname          text,
    client_addr      inet,
    client_hostname  text,
    state            text,
    backend_type     text
);
`

const connectionGroupsTestSchemaTeardown = `
DROP TABLE IF EXISTS metrics.pg_stat_activity CASCADE;
`

// newConnectionGroupsTestHandler wires a PerfSummaryHandler to the
// TEST_AI_WORKBENCH_SERVER Postgres instance and installs the trimmed
// pg_stat_activity schema above. The supplied auth store may be nil, in which
// case the RBAC checker grants access to every connection.
func newConnectionGroupsTestHandler(
	t *testing.T,
	store *auth.AuthStore,
) (*PerfSummaryHandler, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping connection " +
			"groups test")
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

	if _, err := pool.Exec(ctx, connectionGroupsTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create connection groups test schema: %v", err)
	}

	ds := database.NewTestDatastore(pool)
	handler := NewPerfSummaryHandler(ds, store)
	cleanup := func() {
		_, _ = pool.Exec(context.Background(),
			connectionGroupsTestSchemaTeardown)
		pool.Close()
	}
	return handler, pool, cleanup
}

// newConnectionGroupsAuthStore creates a real, file-backed auth store for the
// tests that need RBAC to actually deny access.
func newConnectionGroupsAuthStore(t *testing.T) (*auth.AuthStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "connection-groups-auth-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	store, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create auth store: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	return store, func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}
}

// seedConnectionGroupsFixture inserts two snapshots for a connection. The
// newest snapshot mixes users, databases, client addresses and backend states,
// and includes a background worker that must be filtered out; the older
// snapshot holds rows that must never be counted, because only the newest
// snapshot contributes.
func seedConnectionGroupsFixture(
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

	// Newest snapshot.
	exec(`INSERT INTO metrics.pg_stat_activity
        (connection_id, collected_at, pid, usename, datname, client_addr,
         client_hostname, state, backend_type)
        VALUES
        ($1, $2, 101, 'app_rw', 'sales', '192.0.2.10', 'app1.example.com',
         'active', 'client backend'),
        ($1, $2, 102, 'app_rw', 'sales', '192.0.2.10', NULL,
         'idle', 'client backend'),
        ($1, $2, 103, 'app_rw', 'sales', '192.0.2.10', NULL,
         'idle in transaction', 'client backend'),
        ($1, $2, 104, 'app_rw', 'reporting', '192.0.2.11', NULL,
         'idle in transaction (aborted)', 'client backend'),
        ($1, $2, 105, 'reporter', 'reporting', NULL, NULL,
         'fastpath function call', 'client backend'),
        ($1, $2, 106, NULL, NULL, NULL, NULL,
         NULL, 'client backend'),
        ($1, $2, 107, '', '', NULL, NULL,
         'active', 'client backend'),
        ($1, $2, 108, NULL, NULL, NULL, NULL,
         NULL, 'walwriter'),
        ($1, $2, 109, 'app_rw', 'sales', '192.0.2.10', NULL,
         'active', 'autovacuum worker')`, connID, latest)

	// Older snapshot: never counted.
	exec(`INSERT INTO metrics.pg_stat_activity
        (connection_id, collected_at, pid, usename, datname, client_addr,
         client_hostname, state, backend_type)
        VALUES
        ($1, $2, 201, 'stale', 'stale_db', '198.51.100.5', 'stale.example.com',
         'active', 'client backend'),
        ($1, $2, 202, 'stale', 'stale_db', '198.51.100.5', NULL,
         'idle', 'client backend')`, connID, prev)

	// A different connection's rows, which must never leak in.
	exec(`INSERT INTO metrics.pg_stat_activity
        (connection_id, collected_at, pid, usename, datname, client_addr,
         client_hostname, state, backend_type)
        VALUES
        ($1, $2, 301, 'other_conn', 'other_db', '203.0.113.7', NULL,
         'active', 'client backend')`, connID+1, latest)
}

// doConnectionGroupsRequest issues a GET against the handler with the given
// raw query string and returns the recorder.
func doConnectionGroupsRequest(
	h *PerfSummaryHandler,
	query string,
	decorate func(*http.Request) *http.Request,
) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/connection-groups?"+query, nil)
	if decorate != nil {
		req = decorate(req)
	}
	rec := httptest.NewRecorder()
	h.handleConnectionGroups(rec, req)
	return rec
}

// decodeConnectionGroups asserts a 200 response and decodes the body.
func decodeConnectionGroups(
	t *testing.T,
	rec *httptest.ResponseRecorder,
) ConnectionGroupsResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var resp ConnectionGroupsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v; body: %s", err,
			rec.Body.String())
	}
	return resp
}

// assertGroups compares the decoded groups against the expected sequence,
// preserving order so that the total-descending, label-ascending ordering is
// asserted too.
func assertGroups(
	t *testing.T,
	got []ConnectionGroupRow,
	want []ConnectionGroupRow,
) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d groups, want %d; got: %s", len(got), len(want),
			mustJSON(t, got))
	}
	for i := range want {
		if !groupsEqual(got[i], want[i]) {
			t.Errorf("group[%d] = %s, want %s", i, mustJSON(t, got[i]),
				mustJSON(t, want[i]))
		}
	}
}

// groupsEqual compares two rows, treating the nullable hostname pointer by
// value rather than by address.
func groupsEqual(a, b ConnectionGroupRow) bool {
	if (a.ClientHostname == nil) != (b.ClientHostname == nil) {
		return false
	}
	if a.ClientHostname != nil && *a.ClientHostname != *b.ClientHostname {
		return false
	}
	return a.GroupLabel == b.GroupLabel &&
		a.Total == b.Total &&
		a.Active == b.Active &&
		a.Idle == b.Idle &&
		a.IdleInTransaction == b.IdleInTransaction &&
		a.Other == b.Other
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	return string(data)
}

func strPtr(s string) *string { return &s }

// TestConnectionGroups_GroupByUser verifies the default grouping: counts per
// database user, with a placeholder label for a NULL or empty role name, the
// state buckets, and the total-descending ordering.
func TestConnectionGroups_GroupByUser(t *testing.T) {
	h, pool, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	const connID = 501
	now := time.Now().UTC()
	latest := now.Add(-1 * time.Minute)
	prev := now.Add(-6 * time.Minute)
	seedConnectionGroupsFixture(t, pool, connID, latest, prev)

	for _, query := range []string{
		fmt.Sprintf("connection_id=%d&group_by=user", connID),
		// An omitted group_by must behave identically.
		fmt.Sprintf("connection_id=%d", connID),
	} {
		resp := decodeConnectionGroups(t,
			doConnectionGroupsRequest(h, query, nil))

		if resp.CollectedAt == nil {
			t.Fatalf("collected_at must be populated for %q", query)
		}
		if diff := resp.CollectedAt.Sub(latest); diff > time.Second ||
			diff < -time.Second {
			t.Errorf("collected_at = %v, want ~%v (query %q)",
				resp.CollectedAt, latest, query)
		}

		assertGroups(t, resp.Groups, []ConnectionGroupRow{
			{GroupLabel: "app_rw", Total: 4, Active: 1, Idle: 1,
				IdleInTransaction: 2},
			{GroupLabel: "(unknown)", Total: 2, Active: 1, Other: 1},
			{GroupLabel: "reporter", Total: 1, Other: 1},
		})
	}
}

// TestConnectionGroups_GroupByClient verifies the client grouping: a NULL
// client_addr is labeled "local", the group's first non-NULL client_hostname
// is reported, and groups with equal totals fall back to label ordering.
func TestConnectionGroups_GroupByClient(t *testing.T) {
	h, pool, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	const connID = 502
	now := time.Now().UTC()
	seedConnectionGroupsFixture(t, pool, connID, now.Add(-1*time.Minute),
		now.Add(-6*time.Minute))

	resp := decodeConnectionGroups(t, doConnectionGroupsRequest(h,
		fmt.Sprintf("connection_id=%d&group_by=client", connID), nil))

	assertGroups(t, resp.Groups, []ConnectionGroupRow{
		{GroupLabel: "192.0.2.10",
			ClientHostname: strPtr("app1.example.com"),
			Total:          3, Active: 1, Idle: 1,
			IdleInTransaction: 1},
		{GroupLabel: "local", Total: 3, Active: 1, Other: 2},
		{GroupLabel: "192.0.2.11", Total: 1, IdleInTransaction: 1},
	})
}

// TestConnectionGroups_GroupByDatabase verifies the database grouping,
// including the placeholder label used when datname is NULL or empty.
func TestConnectionGroups_GroupByDatabase(t *testing.T) {
	h, pool, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	const connID = 503
	now := time.Now().UTC()
	seedConnectionGroupsFixture(t, pool, connID, now.Add(-1*time.Minute),
		now.Add(-6*time.Minute))

	resp := decodeConnectionGroups(t, doConnectionGroupsRequest(h,
		fmt.Sprintf("connection_id=%d&group_by=database", connID), nil))

	assertGroups(t, resp.Groups, []ConnectionGroupRow{
		{GroupLabel: "sales", Total: 3, Active: 1, Idle: 1,
			IdleInTransaction: 1},
		{GroupLabel: "(none)", Total: 2, Active: 1, Other: 1},
		{GroupLabel: "reporting", Total: 2, IdleInTransaction: 1, Other: 1},
	})
}

// TestConnectionGroups_LatestSnapshotOnly proves that only the newest
// collected_at inside the window contributes: the older snapshot's "stale"
// user, and the neighboring connection's rows, are both absent.
func TestConnectionGroups_LatestSnapshotOnly(t *testing.T) {
	h, pool, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	const connID = 504
	now := time.Now().UTC()
	seedConnectionGroupsFixture(t, pool, connID, now.Add(-1*time.Minute),
		now.Add(-6*time.Minute))

	resp := decodeConnectionGroups(t, doConnectionGroupsRequest(h,
		fmt.Sprintf("connection_id=%d&group_by=user", connID), nil))

	var total int64
	for _, g := range resp.Groups {
		if g.GroupLabel == "stale" {
			t.Errorf("older snapshot must not be counted; got %s",
				mustJSON(t, resp.Groups))
		}
		if g.GroupLabel == "other_conn" {
			t.Errorf("another connection's rows must not be counted; got %s",
				mustJSON(t, resp.Groups))
		}
		total += g.Total
	}
	// Seven client backends in the newest snapshot; the walwriter and the
	// autovacuum worker are excluded.
	if total != 7 {
		t.Errorf("summed total = %d, want 7; got %s", total,
			mustJSON(t, resp.Groups))
	}
}

// TestConnectionGroups_TimeRangeExcludesOlderSnapshot verifies the window
// bound: a snapshot older than the requested time range yields no data, while
// a wider range picks the same snapshot up.
func TestConnectionGroups_TimeRangeExcludesOlderSnapshot(t *testing.T) {
	h, pool, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	const connID = 505
	now := time.Now().UTC()
	// The only snapshot sits 90 minutes in the past.
	seedConnectionGroupsFixture(t, pool, connID, now.Add(-90*time.Minute),
		now.Add(-120*time.Minute))

	narrow := decodeConnectionGroups(t, doConnectionGroupsRequest(h,
		fmt.Sprintf("connection_id=%d&time_range=1h", connID), nil))
	if narrow.CollectedAt != nil || len(narrow.Groups) != 0 {
		t.Errorf("rows outside the time range must be excluded; got %s",
			rec2string(t, narrow))
	}

	wide := decodeConnectionGroups(t, doConnectionGroupsRequest(h,
		fmt.Sprintf("connection_id=%d&time_range=6h", connID), nil))
	if wide.CollectedAt == nil || len(wide.Groups) == 0 {
		t.Errorf("a wider time range must include the snapshot; got %s",
			rec2string(t, wide))
	}
}

func rec2string(t *testing.T, resp ConnectionGroupsResponse) string {
	t.Helper()
	return mustJSON(t, resp)
}

// TestConnectionGroups_GroupCapTruncatesSmallestGroups verifies the defensive
// bound on group cardinality end to end: with more distinct client addresses
// in the snapshot than the cap allows, exactly maxConnectionGroups groups come
// back, and the ones retained are the largest.
func TestConnectionGroups_GroupCapTruncatesSmallestGroups(t *testing.T) {
	h, pool, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	const connID = 514
	collectedAt := time.Now().UTC().Add(-time.Minute)

	// maxConnectionGroups + 50 distinct client addresses. The first 20 get
	// two backends each, so they must survive truncation; the rest get one.
	if _, err := pool.Exec(context.Background(), `
        INSERT INTO metrics.pg_stat_activity
            (connection_id, collected_at, pid, usename, datname, client_addr,
             client_hostname, state, backend_type)
        SELECT $1::int, $2::timestamptz, g.n, 'app', 'sales',
               ('10.' || (g.n / 256) || '.' || (g.n % 256) || '.1')::inet,
               NULL, 'active', 'client backend'
        FROM generate_series(1, $3::int) AS g(n)
        UNION ALL
        -- The same first 20 addresses again, so those groups hold two
        -- backends and must outrank the single-backend groups.
        SELECT $1::int, $2::timestamptz, 100000 + g.n, 'app', 'sales',
               ('10.' || (g.n / 256) || '.' || (g.n % 256) || '.1')::inet,
               NULL, 'idle', 'client backend'
        FROM generate_series(1, 20) AS g(n)`,
		connID, collectedAt, maxConnectionGroups+50); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	resp := decodeConnectionGroups(t, doConnectionGroupsRequest(h,
		fmt.Sprintf("connection_id=%d&group_by=client", connID), nil))

	if len(resp.Groups) != maxConnectionGroups {
		t.Fatalf("got %d groups, want the cap of %d", len(resp.Groups),
			maxConnectionGroups)
	}
	// The 20 two-backend groups must be the head of the result, and the
	// tail must be single-backend groups: truncation drops the smallest.
	for i := 0; i < 20; i++ {
		if resp.Groups[i].Total != 2 {
			t.Errorf("group[%d].Total = %d, want 2; the largest groups must "+
				"survive truncation", i, resp.Groups[i].Total)
		}
	}
	if last := resp.Groups[len(resp.Groups)-1]; last.Total != 1 {
		t.Errorf("last group Total = %d, want 1", last.Total)
	}
}

// TestConnectionGroups_EmptyResult verifies the documented empty shape when a
// connection has no pg_stat_activity rows at all.
func TestConnectionGroups_EmptyResult(t *testing.T) {
	h, _, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	rec := doConnectionGroupsRequest(h, "connection_id=59999", nil)
	resp := decodeConnectionGroups(t, rec)

	if resp.CollectedAt != nil {
		t.Errorf("collected_at = %v, want null", resp.CollectedAt)
	}
	if resp.Groups == nil || len(resp.Groups) != 0 {
		t.Errorf("groups = %#v, want an empty array", resp.Groups)
	}
	if body := rec.Body.String(); !jsonHasEmptyGroups(body) {
		t.Errorf("body = %s, want groups serialized as []", body)
	}
}

// jsonHasEmptyGroups reports whether the serialized body carries an empty JSON
// array (rather than null) for groups.
func jsonHasEmptyGroups(body string) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return false
	}
	groups, ok := raw["groups"]
	if !ok {
		return false
	}
	return string(groups) == "[]"
}

// TestConnectionGroups_QueryErrorReturnsEmpty verifies the "treat a query
// error as no data" policy: with the metrics table missing, the endpoint still
// answers 200 with an empty payload.
func TestConnectionGroups_QueryErrorReturnsEmpty(t *testing.T) {
	h, pool, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	if _, err := pool.Exec(context.Background(),
		connectionGroupsTestSchemaTeardown); err != nil {
		t.Fatalf("failed to drop metrics table: %v", err)
	}

	resp := decodeConnectionGroups(t,
		doConnectionGroupsRequest(h, "connection_id=506", nil))
	if resp.CollectedAt != nil || len(resp.Groups) != 0 {
		t.Errorf("query error must yield an empty payload; got %s",
			rec2string(t, resp))
	}
}

// TestConnectionGroups_ScanErrorSkipsRow verifies the defensive handling in
// the row loop: a row whose columns cannot be scanned into the response struct
// is logged and dropped, and the endpoint still answers 200 with an empty
// payload rather than panicking or emitting a half-populated group. The
// condition is forced by redefining client_hostname as a bytea, so the client
// grouping's MIN() aggregate yields a value that will not scan into a *string.
func TestConnectionGroups_ScanErrorSkipsRow(t *testing.T) {
	h, pool, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
        DROP TABLE IF EXISTS metrics.pg_stat_activity CASCADE;
        CREATE TABLE metrics.pg_stat_activity (
            connection_id    integer     NOT NULL,
            collected_at     timestamptz NOT NULL,
            pid              integer     NOT NULL,
            usename          text,
            datname          text,
            client_addr      inet,
            client_hostname  bytea,
            state            text,
            backend_type     text
        );`); err != nil {
		t.Fatalf("failed to install mistyped schema: %v", err)
	}

	const connID = 513
	if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_stat_activity
        (connection_id, collected_at, pid, usename, datname, client_addr,
         client_hostname, state, backend_type)
        VALUES ($1, $2, 401, 'app_rw', 'sales', '192.0.2.10',
                '\x0102'::bytea, 'active', 'client backend')`, connID,
		time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	resp := decodeConnectionGroups(t, doConnectionGroupsRequest(h,
		fmt.Sprintf("connection_id=%d&group_by=client", connID), nil))

	if len(resp.Groups) != 0 {
		t.Errorf("rows that fail to scan must be skipped; got %s",
			mustJSON(t, resp.Groups))
	}
	if resp.CollectedAt != nil {
		t.Errorf("collected_at = %v, want null", resp.CollectedAt)
	}
}

// TestConnectionGroups_TransactionBeginFailure verifies the 500 path when a
// read-only transaction cannot be started, here by closing the pool first.
func TestConnectionGroups_TransactionBeginFailure(t *testing.T) {
	h, pool, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	_, _ = pool.Exec(context.Background(), connectionGroupsTestSchemaTeardown)
	pool.Close()

	rec := doConnectionGroupsRequest(h, "connection_id=507", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", rec.Code,
			rec.Body.String())
	}
}

// TestConnectionGroups_InvalidGroupBy verifies the 400 response and its
// wording, which must name every accepted value.
func TestConnectionGroups_InvalidGroupBy(t *testing.T) {
	h, _, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	rec := doConnectionGroupsRequest(h,
		"connection_id=508&group_by=application_name", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code,
			rec.Body.String())
	}
	want := "Invalid group_by: must be one of client, database, user"
	if got := errorMessageFromBody(t, rec); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

// TestConnectionGroups_InvalidTimeRange verifies the 400 response for an
// unsupported time range, reusing the wording of the sibling endpoints.
func TestConnectionGroups_InvalidTimeRange(t *testing.T) {
	h, _, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	rec := doConnectionGroupsRequest(h,
		"connection_id=509&time_range=90m", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code,
			rec.Body.String())
	}
	want := "Invalid time_range: must be one of 1h, 6h, 24h, 7d, 30d"
	if got := errorMessageFromBody(t, rec); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

// TestConnectionGroups_ConnectionIDValidation verifies that the endpoint
// requires exactly one connection ID.
func TestConnectionGroups_ConnectionIDValidation(t *testing.T) {
	h, _, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	tests := []struct {
		name  string
		query string
	}{
		{name: "missing", query: "group_by=user"},
		{name: "not an integer", query: "connection_id=abc"},
		{name: "multiple", query: "connection_ids=1,2"},
		{name: "invalid list", query: "connection_ids=1,x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doConnectionGroupsRequest(h, tc.query, nil)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body: %s", rec.Code,
					rec.Body.String())
			}
		})
	}
}

// TestConnectionGroups_PermissionDenied verifies the 403 path: the connection
// is assigned to an RBAC group, and the requesting user holds no privilege on
// it.
func TestConnectionGroups_PermissionDenied(t *testing.T) {
	store, storeCleanup := newConnectionGroupsAuthStore(t)
	defer storeCleanup()

	h, _, cleanup := newConnectionGroupsTestHandler(t, store)
	defer cleanup()

	const connID = 510
	groupID, err := store.CreateGroup("connection-groups-denied",
		"restricts the test connection")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if err := store.GrantConnectionPrivilege(groupID, connID,
		auth.AccessLevelRead); err != nil {
		t.Fatalf("GrantConnectionPrivilege failed: %v", err)
	}

	rec := doConnectionGroupsRequest(h,
		fmt.Sprintf("connection_id=%d", connID),
		func(r *http.Request) *http.Request { return withUser(r, 999999) })

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code,
			rec.Body.String())
	}
	want := fmt.Sprintf(
		"Permission denied: you do not have access to connection %d", connID)
	if got := errorMessageFromBody(t, rec); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

// TestConnectionGroups_MethodNotAllowed verifies that only GET is accepted.
func TestConnectionGroups_MethodNotAllowed(t *testing.T) {
	h, _, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	for _, method := range []string{
		http.MethodPost, http.MethodPut, http.MethodDelete,
	} {
		req := httptest.NewRequest(method,
			"/api/v1/metrics/connection-groups?connection_id=511", nil)
		rec := httptest.NewRecorder()
		h.handleConnectionGroups(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s status = %d, want 405; body: %s", method, rec.Code,
				rec.Body.String())
		}
	}
}

// TestConnectionGroups_RegisterRoutes verifies both registration branches:
// a configured handler serves the endpoint, and a handler with no datastore
// answers with the not-configured response instead.
func TestConnectionGroups_RegisterRoutes(t *testing.T) {
	h, pool, cleanup := newConnectionGroupsTestHandler(t, nil)
	defer cleanup()

	const connID = 512
	now := time.Now().UTC()
	seedConnectionGroupsFixture(t, pool, connID, now.Add(-1*time.Minute),
		now.Add(-6*time.Minute))

	passthrough := func(next http.HandlerFunc) http.HandlerFunc { return next }

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, passthrough)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf(
		"/api/v1/metrics/connection-groups?connection_id=%d", connID), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("configured route status = %d, want 200; body: %s", rec.Code,
			rec.Body.String())
	}

	unconfigured := NewPerfSummaryHandler(nil, nil)
	unconfiguredMux := http.NewServeMux()
	unconfigured.RegisterRoutes(unconfiguredMux, passthrough)
	unconfiguredRec := httptest.NewRecorder()
	unconfiguredMux.ServeHTTP(unconfiguredRec, httptest.NewRequest(
		http.MethodGet,
		"/api/v1/metrics/connection-groups?connection_id=1", nil))
	if unconfiguredRec.Code == http.StatusOK {
		t.Errorf("unconfigured route status = %d, want a non-200 "+
			"not-configured response", unconfiguredRec.Code)
	}
}

// errorMessageFromBody extracts the message from an ErrorResponse body,
// tolerating either the "error" or "message" field name.
func errorMessageFromBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode error body %q: %v", rec.Body.String(), err)
	}
	for _, key := range []string{"error", "message"} {
		if v, ok := body[key].(string); ok && v != "" {
			return v
		}
	}
	t.Fatalf("no error message in body %q", rec.Body.String())
	return ""
}
