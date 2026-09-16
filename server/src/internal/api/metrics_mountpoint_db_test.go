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

// mountProbeTable is a fixture probe rather than the real pg_sys_disk_info
// so that the test owns its own rows and can drop the table afterwards
// without disturbing collected data in the test database.
const mountProbeTable = "pg_sys_disk_info_mount_test"

// setupMountFixture creates a probe table shaped like pg_sys_disk_info and
// inserts one current sample for each of two mounts on connection 1, plus an
// older sample for the root mount so the DISTINCT ON reduction has something
// to discard. It returns the pool and a cleanup that drops the table and
// closes the pool; it skips the test when no local test database is
// configured, following the gating convention of the other DB-backed tests.
func setupMountFixture(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping mount filter test")
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

	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		pool.Close()
		t.Fatalf("failed to create metrics schema: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DROP TABLE IF EXISTS metrics."`+mountProbeTable+`"`); err != nil {
		pool.Close()
		t.Fatalf("failed to drop the fixture table: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE metrics."`+mountProbeTable+`" (
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        inserted_at   timestamp without time zone NOT NULL DEFAULT now(),
        mount_point   text,
        used_space    bigint,
        free_space    bigint
    )`); err != nil {
		pool.Close()
		t.Fatalf("failed to create the fixture table: %v", err)
	}

	now := time.Now().UTC()
	rows := []struct {
		mount string
		when  time.Time
		used  int64
	}{
		{"/", now.Add(-10 * time.Minute), 100},
		{"/", now.Add(-time.Minute), 200},
		{"/srv/data", now.Add(-time.Minute), 900},
	}
	for _, r := range rows {
		if _, err := pool.Exec(ctx,
			`INSERT INTO metrics."`+mountProbeTable+`"
             (connection_id, collected_at, mount_point, used_space, free_space)
             VALUES (1, $1, $2, $3, 50)`,
			r.when, r.mount, r.used); err != nil {
			pool.Close()
			t.Fatalf("failed to insert a fixture row: %v", err)
		}
	}

	return pool, func() {
		if _, err := pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS metrics."`+mountProbeTable+`"`); err != nil {
			t.Logf("mount fixture teardown failed: %v", err)
		}
		pool.Close()
	}
}

// TestHandleMetricsQuery_LatestRowsMode_ScopesToMountPoint drives the
// latest-row path end to end against a real pool, which is how the Disk
// Usage KPI tile reads its current figures. Without the mount_point clause
// in buildLatestRowsQuery the request would return a row per mount and the
// tile would describe whichever filesystem happened to sort first.
func TestHandleMetricsQuery_LatestRowsMode_ScopesToMountPoint(t *testing.T) {
	pool, cleanup := setupMountFixture(t)
	defer cleanup()

	handler := &MetricsHandler{datastore: database.NewTestDatastore(pool)}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1&probe_name="+mountProbeTable+
			"&limit=10&order_by=collected_at&order=desc"+
			"&mount_point=%2Fsrv%2Fdata", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode the response: %v (body %q)",
			err, rec.Body.String())
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly the one selected mount, got %d rows: %v",
			len(got), got)
	}
	if got[0]["mount_point"] != "/srv/data" {
		t.Errorf("expected mount_point %q, got %v", "/srv/data", got[0]["mount_point"])
	}
	if used, ok := got[0]["used_space"].(float64); !ok || used != 900 {
		t.Errorf("expected used_space 900 from the selected mount, got %v",
			got[0]["used_space"])
	}
}

// TestHandleMetricsQuery_LatestRowsMode_UnfilteredReturnsEveryMount is the
// control for the test above: with no mount_point the same request returns
// the current row for each mount, which is the set of rows a caller that
// averages them blends into the single meaningless figure this filter
// exists to replace.
func TestHandleMetricsQuery_LatestRowsMode_UnfilteredReturnsEveryMount(t *testing.T) {
	pool, cleanup := setupMountFixture(t)
	defer cleanup()

	handler := &MetricsHandler{datastore: database.NewTestDatastore(pool)}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1&probe_name="+mountProbeTable+
			"&limit=10&order_by=collected_at&order=desc", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode the response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected one current row per mount, got %d rows: %v", len(got), got)
	}
}

// TestHandleMetricsQuery_LatestRowsMode_UnknownProbe covers the error return
// from the latest-row path: an unknown probe fails column discovery, and the
// handler maps that to a 400 rather than a 500.
func TestHandleMetricsQuery_LatestRowsMode_UnknownProbe(t *testing.T) {
	pool, cleanup := setupMountFixture(t)
	defer cleanup()

	handler := &MetricsHandler{datastore: database.NewTestDatastore(pool)}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=zzz_no_such_probe&limit=1", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}
