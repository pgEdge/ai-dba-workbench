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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// TestParseMetricFilters covers the shared parser both variants of GET
// /api/v1/metrics/query use, which is where mount_point reaches the filter
// struct for the time-series and the latest-row paths alike.
func TestParseMetricFilters(t *testing.T) {
	t.Run("parses every dimension", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/query?database_name=northwind"+
				"&schema_name=public&table_name=orders"+
				"&index_name=orders_pkey&mount_point=%2Fsrv%2Fdata"+
				"&queryid=-42", nil)
		rec := httptest.NewRecorder()

		filters, ok := parseMetricFilters(rec, req)
		if !ok {
			t.Fatalf("expected the parse to succeed, got %d (body %q)",
				rec.Code, rec.Body.String())
		}
		if filters.DatabaseName != "northwind" {
			t.Errorf("DatabaseName = %q", filters.DatabaseName)
		}
		if filters.SchemaName != "public" {
			t.Errorf("SchemaName = %q", filters.SchemaName)
		}
		if filters.TableName != "orders" {
			t.Errorf("TableName = %q", filters.TableName)
		}
		if filters.IndexName != "orders_pkey" {
			t.Errorf("IndexName = %q", filters.IndexName)
		}
		if filters.MountPoint != "/srv/data" {
			t.Errorf("MountPoint = %q, want %q", filters.MountPoint, "/srv/data")
		}
		if filters.QueryID == nil || *filters.QueryID != -42 {
			t.Errorf("QueryID = %v, want -42", filters.QueryID)
		}
	})

	t.Run("absent mount_point leaves the field empty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/query?probe_name=pg_sys_cpu_info", nil)
		rec := httptest.NewRecorder()

		filters, ok := parseMetricFilters(rec, req)
		if !ok {
			t.Fatalf("expected the parse to succeed, got %d", rec.Code)
		}
		if filters.MountPoint != "" {
			t.Errorf("MountPoint = %q, want empty", filters.MountPoint)
		}
	})

	t.Run("rejects a malformed queryid", func(t *testing.T) {
		// The queryid failure must still short-circuit now that the string
		// dimensions are parsed in the same helper.
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/query?mount_point=%2F&queryid=not-a-number", nil)
		rec := httptest.NewRecorder()

		filters, ok := parseMetricFilters(rec, req)
		if ok {
			t.Fatal("expected the parse to fail on a malformed queryid")
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
		}
		if filters != (metrics.MetricFilters{}) {
			t.Errorf("expected a zero filter set on failure, got %+v", filters)
		}
	})
}

// TestHandleMetricsQuery_TimeSeriesMode_ParsesMountPointFilter proves the
// Disk Space chart's mount selection reaches the query layer; without it the
// chart would silently average every mounted filesystem again.
func TestHandleMetricsQuery_TimeSeriesMode_ParsesMountPointFilter(t *testing.T) {
	var gotFilters metrics.MetricFilters
	called := false
	handler := &MetricsHandler{
		datastore: &database.Datastore{},
		queryTimeSeriesFn: func(
			_ context.Context,
			_ *pgxpool.Pool,
			_ string,
			_ []int,
			_ metrics.TimeWindow,
			filters metrics.MetricFilters,
			_ int,
			_ string,
			_ []string,
		) (*metrics.MetricsQueryResult, error) {
			called = true
			gotFilters = filters
			return &metrics.MetricsQueryResult{Series: []metrics.MetricSeries{}}, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_sys_disk_info&time_range=1h"+
			"&mount_point=%2Fvar%2Flib%2Fpostgresql", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("expected time-series query function to be called")
	}
	if gotFilters.MountPoint != "/var/lib/postgresql" {
		t.Errorf("expected MountPoint %q reaching the query layer, got %q",
			"/var/lib/postgresql", gotFilters.MountPoint)
	}
}

// TestBuildOpenAPISpec_MetricsQueryMountPointParam locks the documented
// parameter to the code, so a client reading the published specification
// discovers the mount dimension.
func TestBuildOpenAPISpec_MetricsQueryMountPointParam(t *testing.T) {
	spec := BuildOpenAPISpec()

	path, ok := spec.Paths["/metrics/query"]
	if !ok {
		t.Fatal("spec is missing the /metrics/query path")
	}
	if path.Get == nil {
		t.Fatal("the metrics query path has no GET operation")
	}

	var found *OpenAPIParameter
	var indexIdx, mountIdx = -1, -1
	for i := range path.Get.Parameters {
		switch path.Get.Parameters[i].Name {
		case "mount_point":
			found = &path.Get.Parameters[i]
			mountIdx = i
		case "index_name":
			indexIdx = i
		}
	}
	if found == nil {
		t.Fatal("the metrics query path is missing the mount_point parameter")
	}
	if found.In != "query" {
		t.Errorf("mount_point parameter In = %q, want %q", found.In, "query")
	}
	if found.Required {
		t.Error("mount_point must stay optional")
	}
	if found.Schema == nil || found.Schema.Type != "string" {
		t.Errorf("mount_point schema = %+v, want a string", found.Schema)
	}
	if found.Description == "" {
		t.Error("mount_point needs a description")
	}
	// It mirrors index_name, so it belongs beside the other dimension
	// filters rather than appended to the end of the parameter list.
	if indexIdx == -1 || mountIdx != indexIdx+1 {
		t.Errorf("mount_point at %d should follow index_name at %d",
			mountIdx, indexIdx)
	}
}
