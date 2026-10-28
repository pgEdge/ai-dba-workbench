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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// TestHandleMetricsBaselines_RequestValidation covers the request checks
// of GET /api/v1/metrics/baselines that answer before any query runs.
func TestHandleMetricsBaselines_RequestValidation(t *testing.T) {
	handler := &MetricsHandler{authStore: newTestAuthStore(t), sharingLookupFn: liveConnectionLookup}

	tests := []struct {
		name   string
		method string
		query  string
		want   int
		msg    string
	}{
		{"wrong method", http.MethodPost, "connection_id=1&probe_name=pg_stat_activity", http.StatusMethodNotAllowed, ""},
		{"missing connection_id", http.MethodGet, "probe_name=pg_stat_activity", http.StatusBadRequest, "connection_id is required"},
		{"invalid connection_id", http.MethodGet, "connection_id=abc&probe_name=pg_stat_activity", http.StatusBadRequest, "Invalid connection_id"},
		{"missing probe_name", http.MethodGet, "connection_id=1", http.StatusBadRequest, "probe_name is required"},
		{"invalid probe_name", http.MethodGet, "connection_id=1&probe_name=bad-name", http.StatusBadRequest, "Invalid probe_name"},
		{"invalid period_type", http.MethodGet, "connection_id=1&probe_name=pg_stat_activity&period_type=overall", http.StatusBadRequest, "Invalid period_type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := withSuperuser(httptest.NewRequest(tt.method, "/api/v1/metrics/baselines?"+tt.query, nil))
			rec := httptest.NewRecorder()
			handler.handleMetricsBaselines(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body.String())
			}
			if tt.msg != "" && !strings.Contains(rec.Body.String(), tt.msg) {
				t.Errorf("body = %s, want it to contain %q", rec.Body.String(), tt.msg)
			}
		})
	}
}

// baselinesTestSchema keeps this test's metric_baselines table out of
// the shared public schema; the pool's search_path points at it.
const baselinesTestSchema = "api_metrics_baselines_test"

// TestHandleMetricsBaselines_Query drives the baselines query through
// the handler: a datastore without the table answers 400 with the query
// error, and one with it returns the requested probe's baselines only.
func TestHandleMetricsBaselines_Query(t *testing.T) {
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping baselines handler test")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("parse connection string: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = baselinesTestSchema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}
	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("exec %q: %v", sql, err)
		}
	}
	exec(`DROP SCHEMA IF EXISTS ` + baselinesTestSchema + ` CASCADE`)
	exec(`CREATE SCHEMA ` + baselinesTestSchema)
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`DROP SCHEMA IF EXISTS `+baselinesTestSchema+` CASCADE`); err != nil {
			t.Logf("drop test schema: %v", err)
		}
		pool.Close()
	})

	handler := &MetricsHandler{datastore: database.NewTestDatastore(pool), authStore: newTestAuthStore(t),
		sharingLookupFn: liveConnectionLookup}
	serve := func() *httptest.ResponseRecorder {
		req := withSuperuser(httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/baselines?connection_id=1&probe_name=pg_stat_activity&metrics=pg_stat_activity.count,%20,pg_stat_activity.idle", nil))
		rec := httptest.NewRecorder()
		handler.handleMetricsBaselines(rec, req)
		return rec
	}

	t.Run("query failure", func(t *testing.T) {
		rec := serve()
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "failed to query baselines") {
			t.Fatalf("status = %d, body = %s; want 400 with the query error", rec.Code, rec.Body.String())
		}
	})

	exec(`CREATE TABLE metric_baselines (
        connection_id INTEGER NOT NULL,
        database_name TEXT,
        metric_name TEXT NOT NULL,
        period_type TEXT NOT NULL,
        day_of_week INTEGER,
        hour_of_day INTEGER,
        mean REAL NOT NULL,
        stddev REAL NOT NULL,
        min REAL NOT NULL,
        max REAL NOT NULL,
        sample_count BIGINT NOT NULL DEFAULT 0,
        earliest_sample_at TIMESTAMPTZ)`)
	exec(`INSERT INTO metric_baselines
        (connection_id, database_name, metric_name, period_type, hour_of_day,
         mean, stddev, min, max, sample_count)
        VALUES
        (1, NULL, 'pg_stat_activity.count', 'all', NULL, 4, 1, 2, 7, 10),
        (1, NULL, 'pg_stat_activity.count', 'hourly', 5, 6, 1, 2, 7, 3),
        (1, 'db1', 'pg_stat_database.xact_commit', 'all', NULL, 9, 1, 8, 10, 10),
        (2, NULL, 'pg_stat_activity.count', 'all', NULL, 40, 1, 20, 70, 10)`)

	t.Run("baselines returned", func(t *testing.T) {
		rec := serve()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var got []metrics.MetricBaseline
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(got) != 1 || got[0].Metric != "pg_stat_activity.count" || got[0].Mean != 4 ||
			got[0].PeriodType != "all" || got[0].SampleCount != 10 {
			t.Errorf("baselines = %+v, want connection 1's 'all' pg_stat_activity.count only", got)
		}
	})

	t.Run("period and database parameters", func(t *testing.T) {
		for _, tc := range []struct {
			query string
			want  string
		}{
			{"connection_id=1&probe_name=pg_stat_activity&period_type=hourly", `"hour_of_day":5`},
			{"connection_id=1&probe_name=pg_stat_database&database_name=db1", `"database_name":"db1"`},
		} {
			req := withSuperuser(httptest.NewRequest(http.MethodGet,
				"/api/v1/metrics/baselines?"+tc.query, nil))
			rec := httptest.NewRecorder()
			handler.handleMetricsBaselines(rec, req)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("%s: status = %d, body = %s; want 200 containing %s",
					tc.query, rec.Code, rec.Body.String(), tc.want)
			}
		}
	})
}
