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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// runConnectionCount executes queryConnectionCount over the given window
// in the same read-only transaction shape handlePerfSummary uses, so the
// helper is exercised exactly as it is in production.
func runConnectionCount(
	t *testing.T,
	h *PerfSummaryHandler,
	pool *pgxpool.Pool,
	connID int,
	startTime, endTime time.Time,
) int {
	t.Helper()

	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after read is a no-op

	return h.queryConnectionCount(ctx, tx, connID, startTime, endTime)
}

// TestQueryConnectionCount_SumsLatestSnapshot verifies that the comparative
// Connection Count chart is fed a real per-server backend count: the sum of
// numbackends across every database in the server's most recent
// pg_stat_database snapshot, ignoring older samples and other servers.
func TestQueryConnectionCount_SumsLatestSnapshot(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 71
	const otherConnID = 72
	ctx := context.Background()
	now := time.Now().UTC()
	latest := now.Add(-1 * time.Minute)
	prev := now.Add(-2 * time.Minute)

	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed exec failed: %v\nSQL: %s", err, sql)
		}
	}

	// Latest snapshot for the server under test: two databases with 5 and
	// 3 backends respectively.
	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends)
        VALUES ($1, $2, 'appdb', 5), ($1, $2, 'postgres', 3)`,
		connID, latest)

	// An older snapshot that must be ignored entirely.
	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends)
        VALUES ($1, $2, 'appdb', 99)`, connID, prev)

	// Another server's latest snapshot must not leak into the count.
	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends)
        VALUES ($1, $2, 'appdb', 40)`, otherConnID, latest)

	if got := runConnectionCount(
		t, h, pool, connID, now.Add(-1*time.Hour), now); got != 8 {
		t.Errorf("queryConnectionCount = %d, want 8", got)
	}
}

// TestQueryConnectionCount_HonoursWindow verifies that the count follows
// the selected time window rather than always reporting the live backend
// count: a window that ends before the newest snapshot reports the newest
// snapshot inside it, and a window that predates every sample reports
// zero, matching the empty Transaction Rate, Cache Hit Ratio and Rollback
// Rate series the Cluster dashboard draws beside it.
func TestQueryConnectionCount_HonoursWindow(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 73
	ctx := context.Background()
	now := time.Now().UTC()
	latest := now.Add(-1 * time.Minute)
	older := now.Add(-90 * time.Minute)

	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed exec failed: %v\nSQL: %s", err, sql)
		}
	}

	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends)
        VALUES ($1, $2, 'appdb', 15)`, connID, latest)
	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends)
        VALUES ($1, $2, 'appdb', 4), ($1, $2, 'postgres', 3)`, connID, older)

	tests := []struct {
		name  string
		start time.Time
		end   time.Time
		want  int
	}{
		{
			name:  "window covering every sample takes the newest",
			start: now.Add(-3 * time.Hour),
			end:   now,
			want:  15,
		},
		{
			name:  "window ending before the newest sample excludes it",
			start: now.Add(-3 * time.Hour),
			end:   now.Add(-30 * time.Minute),
			want:  7,
		},
		{
			name:  "window predating every sample reports zero",
			start: now.Add(-48 * time.Hour),
			end:   now.Add(-24 * time.Hour),
			want:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := runConnectionCount(t, h, pool, connID, tc.start, tc.end)
			if got != tc.want {
				t.Errorf("queryConnectionCount = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestQueryConnectionCount_NoData verifies that a server with no collected
// samples reports zero connections rather than failing the whole summary.
func TestQueryConnectionCount_NoData(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	now := time.Now().UTC()
	if got := runConnectionCount(
		t, h, pool, 9999, now.Add(-1*time.Hour), now); got != 0 {
		t.Errorf("queryConnectionCount = %d, want 0", got)
	}
}

// TestQueryConnectionCount_MissingTable verifies the helper degrades to
// zero when the metrics table is absent, matching the other summary
// helpers' behavior on an uncollected estate.
func TestQueryConnectionCount_MissingTable(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	if _, err := pool.Exec(context.Background(),
		"DROP TABLE IF EXISTS metrics.pg_stat_database CASCADE"); err != nil {
		t.Fatalf("failed to drop table: %v", err)
	}

	now := time.Now().UTC()
	if got := runConnectionCount(
		t, h, pool, 71, now.Add(-1*time.Hour), now); got != 0 {
		t.Errorf("queryConnectionCount = %d, want 0", got)
	}
}

// perfSummaryTestSchema mirrors the minimum columns the performance
// summary queries read. Every table the handler touches must exist,
// because all of its queries share one transaction and a missing
// relation would abort that transaction for the queries that follow.
const perfSummaryTestSchema = `
CREATE SCHEMA IF NOT EXISTS metrics;
DROP TABLE IF EXISTS metrics.pg_database CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_database CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_checkpointer CASCADE;

CREATE TABLE metrics.pg_database (
    connection_id     integer     NOT NULL,
    collected_at      timestamptz NOT NULL,
    datname           text        NOT NULL,
    datistemplate     boolean     NOT NULL DEFAULT false,
    age_datfrozenxid  bigint
);

CREATE TABLE metrics.pg_stat_database (
    connection_id  integer     NOT NULL,
    collected_at   timestamptz NOT NULL,
    datname        text,
    numbackends    integer     NOT NULL DEFAULT 0,
    blks_hit       bigint      NOT NULL DEFAULT 0,
    blks_read      bigint      NOT NULL DEFAULT 0,
    xact_commit    bigint      NOT NULL DEFAULT 0,
    xact_rollback  bigint      NOT NULL DEFAULT 0,
    stats_reset    timestamptz
);

CREATE TABLE metrics.pg_stat_checkpointer (
    connection_id  integer          NOT NULL,
    collected_at   timestamptz      NOT NULL,
    write_time     double precision NOT NULL DEFAULT 0,
    sync_time      double precision NOT NULL DEFAULT 0
);
`

const perfSummaryTestSchemaTeardown = `
DROP TABLE IF EXISTS metrics.pg_database CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_database CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_checkpointer CASCADE;
`

// newPerfSummaryTestHandler wires a PerfSummaryHandler to the
// TEST_AI_WORKBENCH_SERVER Postgres instance with the trimmed schema
// above installed.
func newPerfSummaryTestHandler(
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

	if _, err := pool.Exec(ctx, perfSummaryTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create performance summary test schema: %v", err)
	}

	handler := NewPerfSummaryHandler(database.NewTestDatastore(pool), nil)
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), perfSummaryTestSchemaTeardown)
		pool.Close()
	}
	return handler, pool, cleanup
}

// TestHandlePerfSummary_ReportsActiveConnections drives the endpoint the
// comparative charts call and asserts that each connection carries a real
// active_connections count alongside the metrics it already reported.
func TestHandlePerfSummary_ReportsActiveConnections(t *testing.T) {
	h, pool, cleanup := newPerfSummaryTestHandler(t)
	defer cleanup()

	const connA = 81
	const connB = 82
	ctx := context.Background()
	now := time.Now().UTC()
	latest := now.Add(-1 * time.Minute)
	prev := now.Add(-2 * time.Minute)

	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed exec failed: %v\nSQL: %s", err, sql)
		}
	}

	exec(`INSERT INTO metrics.pg_database
        (connection_id, collected_at, datname, datistemplate, age_datfrozenxid)
        VALUES ($1, $2, 'appdb', false, 1000000)`, connA, latest)

	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends, blks_hit,
         blks_read, xact_commit, xact_rollback)
        VALUES
        ($1, $2, 'appdb', 6, 900, 100, 5000, 50),
        ($1, $2, 'postgres', 2, 100, 0, 100, 0)`, connA, latest)
	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends, blks_hit,
         blks_read, xact_commit, xact_rollback)
        VALUES ($1, $2, 'appdb', 99, 400, 60, 4000, 40)`, connA, prev)

	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends, blks_hit,
         blks_read, xact_commit, xact_rollback)
        VALUES ($1, $2, 'appdb', 11, 500, 500, 900, 100)`, connB, latest)

	exec(`INSERT INTO metrics.pg_stat_checkpointer
        (connection_id, collected_at, write_time, sync_time)
        VALUES ($1, $2, 100, 10), ($1, $3, 250, 30)`, connA, prev, latest)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/performance-summary?connection_ids=81,82"+
			"&time_range=24h", nil)
	rec := httptest.NewRecorder()

	h.handlePerfSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp PerfSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(resp.Connections))
	}

	byID := make(map[int]PerfConnectionResponse, len(resp.Connections))
	for _, c := range resp.Connections {
		byID[c.ConnectionID] = c
	}

	// 6 + 2 backends in the latest snapshot; the 99 in the older sample
	// must not be counted.
	if got := byID[connA].ActiveConnections; got != 8 {
		t.Errorf("connection %d active_connections = %d, want 8", connA, got)
	}
	if got := byID[connB].ActiveConnections; got != 11 {
		t.Errorf("connection %d active_connections = %d, want 11", connB, got)
	}

	// The pre-existing metrics must still be populated alongside it. The
	// ratio is per interval and per database: appdb grew by 500 hits and
	// 40 reads between the two samples, which is 92.59%, whilst postgres
	// only appears in the latest sample and so has no delta; its lifetime
	// counters must not be counted as activity in the interval.
	if cur := byID[connA].CacheHitRatio.Current; cur == nil || *cur != 92.59 {
		t.Errorf("cache hit ratio = %v, want 92.59", cur)
	}
	if len(byID[connA].XIDAgeEntries) != 1 {
		t.Errorf("expected 1 XID age entry, got %d",
			len(byID[connA].XIDAgeEntries))
	}
	if len(byID[connA].Checkpoints.TimeSeries) == 0 {
		t.Error("expected checkpoint time series data")
	}
	if resp.Aggregate == nil {
		t.Error("expected an aggregate for a multi-connection request")
	}
}

// TestHandlePerfSummary_RejectsBadRequests covers the parameter
// validation on the endpoint that now also reports connection counts.
func TestHandlePerfSummary_RejectsBadRequests(t *testing.T) {
	h, _, cleanup := newPerfSummaryTestHandler(t)
	defer cleanup()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "missing connection",
			url:  "/api/v1/metrics/performance-summary",
			want: "Either connection_id or connection_ids is required",
		},
		{
			name: "invalid time range",
			url: "/api/v1/metrics/performance-summary" +
				"?connection_id=81&time_range=99z",
			want: `invalid time range "99z": must be one of 1h, 6h, 24h, 7d, 30d, custom`,
		},
		{
			name: "custom without bounds",
			url: "/api/v1/metrics/performance-summary" +
				"?connection_id=81&time_range=custom",
			want: `invalid time range "custom": time_start and time_end are both required`,
		},
		{
			name: "custom with an unparsable start",
			url: "/api/v1/metrics/performance-summary" +
				"?connection_id=81&time_range=custom" +
				"&time_start=yesterday&time_end=2026-01-01T01:00:00Z",
			want: `invalid time_start "yesterday": must be an RFC 3339 timestamp`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()

			h.handlePerfSummary(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d",
					http.StatusBadRequest, rec.Code)
			}
			if resp := decodeError(t, rec); resp.Error != tc.want {
				t.Errorf("unexpected error: %q", resp.Error)
			}
		})
	}
}

// TestHandlePerfSummary_CustomWindow asserts that time_range=custom with
// time_start and time_end restricts the cache hit ratio to exactly that
// window: samples either side of it, carrying a very different ratio,
// must not appear in the series or the headline, and the echoed
// time_range is "custom".
func TestHandlePerfSummary_CustomWindow(t *testing.T) {
	h, pool, cleanup := newPerfSummaryTestHandler(t)
	defer cleanup()

	const connID = 83
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute)
	windowStart := now.Add(-3 * time.Hour)
	windowEnd := now.Add(-2 * time.Hour)

	seed := func(at time.Time, hit, read int64) {
		if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_stat_database
            (connection_id, collected_at, datname, blks_hit, blks_read)
            VALUES ($1, $2, 'appdb', $3, $4)`, connID, at, hit, read); err != nil {
			t.Fatalf("seed pg_stat_database: %v", err)
		}
	}
	// Before the window: an interval at 10%.
	seed(windowStart.Add(-20*time.Minute), 1000, 1000)
	seed(windowStart.Add(-10*time.Minute), 1100, 1900)
	// Inside the window: one interval at 90%.
	seed(windowStart.Add(10*time.Minute), 5000, 5000)
	seed(windowStart.Add(20*time.Minute), 5900, 5100)
	// After the window: an interval at 10%.
	seed(windowEnd.Add(10*time.Minute), 9000, 9000)
	seed(windowEnd.Add(20*time.Minute), 9100, 9900)

	url := "/api/v1/metrics/performance-summary?connection_id=83" +
		"&time_range=custom&time_start=" +
		windowStart.Format(time.RFC3339) +
		"&time_end=" + windowEnd.Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()

	h.handlePerfSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	var resp PerfSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.TimeRange != "custom" {
		t.Errorf("time_range = %q, want custom", resp.TimeRange)
	}
	if len(resp.Connections) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(resp.Connections))
	}
	ch := resp.Connections[0].CacheHitRatio
	// A one hour window has 60 second buckets, so the two in-window
	// samples yield exactly one delta bucket.
	if len(ch.TimeSeries) != 1 {
		t.Fatalf("len(time_series) = %d, want 1: %#v",
			len(ch.TimeSeries), ch.TimeSeries)
	}
	if got := ch.TimeSeries[0].Time; got.Before(windowStart) || !got.Before(windowEnd) {
		t.Errorf("bucket %v lies outside [%v, %v)", got, windowStart, windowEnd)
	}
	if ch.Current == nil || *ch.Current != 90.0 {
		t.Errorf("current = %v, want 90", fmtFloatPtr(ch.Current))
	}
}

// TestHandlePerfSummary_RejectsNonGET covers the method guard.
func TestHandlePerfSummary_RejectsNonGET(t *testing.T) {
	h, _, cleanup := newPerfSummaryTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/metrics/performance-summary?connection_id=81", nil)
	rec := httptest.NewRecorder()

	h.handlePerfSummary(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d",
			http.StatusMethodNotAllowed, rec.Code)
	}
}

// TestHandlePerfSummary_DefaultsToOneHour checks that an omitted
// time_range resolves to the 1h preset and is echoed as such.
func TestHandlePerfSummary_DefaultsToOneHour(t *testing.T) {
	h, _, cleanup := newPerfSummaryTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/performance-summary?connection_id=81", nil)
	rec := httptest.NewRecorder()

	h.handlePerfSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	var resp PerfSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.TimeRange != "1h" {
		t.Errorf("time_range = %q, want 1h", resp.TimeRange)
	}
}

// TestHandlePerfSummary_ShortCustomWindowUsesBucketFloor drives a five
// minute custom window, whose one sixtieth is below the ten second
// floor: two samples 30 seconds apart must then land in separate
// 10 second buckets rather than being merged by a 5 second bucket
// rounding to zero.
func TestHandlePerfSummary_ShortCustomWindowUsesBucketFloor(t *testing.T) {
	h, pool, cleanup := newPerfSummaryTestHandler(t)
	defer cleanup()

	const connID = 84
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute)
	windowStart := now.Add(-10 * time.Minute)
	windowEnd := windowStart.Add(5 * time.Minute)

	for i, hit := range []int64{1000, 1900, 2800} {
		at := windowStart.Add(time.Duration(i) * 30 * time.Second)
		if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_stat_database
            (connection_id, collected_at, datname, blks_hit, blks_read)
            VALUES ($1, $2, 'appdb', $3, $4)`, connID, at, hit, int64(100*(i+1))); err != nil {
			t.Fatalf("seed pg_stat_database: %v", err)
		}
	}

	url := "/api/v1/metrics/performance-summary?connection_id=84" +
		"&time_range=custom&time_start=" +
		windowStart.Format(time.RFC3339) +
		"&time_end=" + windowEnd.Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()

	h.handlePerfSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	var resp PerfSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	series := resp.Connections[0].CacheHitRatio.TimeSeries
	if len(series) != 2 {
		t.Fatalf("len(time_series) = %d, want 2 (one per 30s delta): %#v",
			len(series), series)
	}
	for i, pt := range series {
		if pt.Value == nil || *pt.Value != 90.0 {
			t.Errorf("point %d = %s, want 90", i, fmtFloatPtr(pt.Value))
		}
	}
}

// TestHandlePerfSummary_ConnectionCountFollowsWindow drives the endpoint
// the Cluster dashboard's comparative charts call and asserts that
// active_connections follows the selected window like the series charted
// beside it: a custom window that predates every sample must report zero
// rather than the live backend count.
func TestHandlePerfSummary_ConnectionCountFollowsWindow(t *testing.T) {
	h, pool, cleanup := newPerfSummaryTestHandler(t)
	defer cleanup()

	const connID = 85
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute)

	if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends)
        VALUES ($1, $2, 'appdb', 15)`, connID, now.Add(-5*time.Minute)); err != nil {
		t.Fatalf("seed pg_stat_database: %v", err)
	}

	get := func(url string) PerfConnectionResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		h.handlePerfSummary(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d (body %q)",
				http.StatusOK, rec.Code, rec.Body.String())
		}
		var resp PerfSummaryResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(resp.Connections) != 1 {
			t.Fatalf("expected 1 connection, got %d", len(resp.Connections))
		}
		return resp.Connections[0]
	}

	if got := get("/api/v1/metrics/performance-summary?connection_id=85" +
		"&time_range=24h").ActiveConnections; got != 15 {
		t.Errorf("active_connections over 24h = %d, want 15", got)
	}

	windowStart := now.Add(-48 * time.Hour)
	windowEnd := now.Add(-24 * time.Hour)
	stale := get("/api/v1/metrics/performance-summary?connection_id=85" +
		"&time_range=custom&time_start=" + windowStart.Format(time.RFC3339) +
		"&time_end=" + windowEnd.Format(time.RFC3339))
	if stale.ActiveConnections != 0 {
		t.Errorf("active_connections over a pre-data window = %d, want 0",
			stale.ActiveConnections)
	}
	if len(stale.Transactions.TimeSeries) != 0 {
		t.Errorf("expected an empty transaction series alongside it, got %d points",
			len(stale.Transactions.TimeSeries))
	}
}
