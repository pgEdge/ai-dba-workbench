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

// runConnectionCount executes queryConnectionCount in the same read-only
// transaction shape handlePerfSummary uses, so the helper is exercised
// exactly as it is in production.
func runConnectionCount(
	t *testing.T,
	h *PerfSummaryHandler,
	pool *pgxpool.Pool,
	connID int,
) int {
	t.Helper()

	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after read is a no-op

	return h.queryConnectionCount(ctx, tx, connID)
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

	if got := runConnectionCount(t, h, pool, connID); got != 8 {
		t.Errorf("queryConnectionCount = %d, want 8", got)
	}
}

// TestQueryConnectionCount_NoData verifies that a server with no collected
// samples reports zero connections rather than failing the whole summary.
func TestQueryConnectionCount_NoData(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	if got := runConnectionCount(t, h, pool, 9999); got != 0 {
		t.Errorf("queryConnectionCount = %d, want 0", got)
	}
}

// TestQueryConnectionCount_MissingTable verifies the helper degrades to
// zero when the metrics table is absent, matching the other summary
// helpers' behaviour on an uncollected estate.
func TestQueryConnectionCount_MissingTable(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	if _, err := pool.Exec(context.Background(),
		"DROP TABLE IF EXISTS metrics.pg_stat_database CASCADE"); err != nil {
		t.Fatalf("failed to drop table: %v", err)
	}

	if got := runConnectionCount(t, h, pool, 71); got != 0 {
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
    xact_rollback  bigint      NOT NULL DEFAULT 0
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

	// The pre-existing metrics must still be populated alongside it:
	// 1000 hits against 100 reads across both databases is 90.91%.
	if byID[connA].CacheHitRatio.Current != 90.91 {
		t.Errorf("cache hit ratio = %v, want 90.91",
			byID[connA].CacheHitRatio.Current)
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
			want: "Invalid time_range: must be one of 1h, 6h, 24h, 7d, 30d",
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
