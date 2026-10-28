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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/anomaly"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

const (
	comparisonURL = "/api/v1/metrics/anomaly-comparison"
	cpuMetricName = "pg_sys_cpu_usage_info.processor_time_percent"
	tempMetric    = "pg_stat_database.temp_files_delta"
)

// comparisonHandler returns a handler whose RBAC lookup reports every
// connection live and whose loader records the request it is handed.
func comparisonHandler(t *testing.T, got **metrics.ComparisonRequest,
	result *metrics.AnomalyComparison, err error) *MetricsHandler {
	t.Helper()
	return &MetricsHandler{
		authStore:       newTestAuthStore(t),
		datastore:       &database.Datastore{},
		sharingLookupFn: liveConnectionLookup,
		loadComparisonFn: func(_ context.Context, _ *pgxpool.Pool,
			req metrics.ComparisonRequest, _ time.Time,
		) (*metrics.AnomalyComparison, error) {
			*got = &req
			return result, err
		},
	}
}

func TestHandleAnomalyComparison_Validation(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"missing connection", "?metric=" + cpuMetricName, "connection_id is required"},
		{"bad connection", "?connection_id=x&metric=" + cpuMetricName, "Invalid connection_id"},
		{"missing metric", "?connection_id=1", "metric is required"},
		{"unknown metric", "?connection_id=1&metric=pg_stat_activity.nope",
			"unknown metric: not a baselined metric"},
		{"per-database metric without a database", "?connection_id=1&metric=" + tempMetric,
			"database_name is required for the per-database metric " + tempMetric},
		{"server-level metric with a database",
			"?connection_id=1&database_name=postgres&metric=" + cpuMetricName,
			"database_name must not be given for the server-level metric " + cpuMetricName},
		{"bad model", "?connection_id=1&model=overall&metric=" + cpuMetricName,
			"Invalid model: must be one of as_scored, daily, hourly, all"},
		{"zero sensitivity", "?connection_id=1&sensitivity=0&metric=" + cpuMetricName,
			"Invalid sensitivity: must be greater than 0 and at most 10"},
		{"large sensitivity", "?connection_id=1&sensitivity=10.5&metric=" + cpuMetricName,
			"Invalid sensitivity: must be greater than 0 and at most 10"},
		{"NaN sensitivity", "?connection_id=1&sensitivity=NaN&metric=" + cpuMetricName,
			"Invalid sensitivity: must be greater than 0 and at most 10"},
		{"text sensitivity", "?connection_id=1&sensitivity=high&metric=" + cpuMetricName,
			"Invalid sensitivity: must be greater than 0 and at most 10"},
		{"zero buckets", "?connection_id=1&buckets=0&metric=" + cpuMetricName,
			"Invalid buckets: must be between 1 and 500"},
		{"too many buckets", "?connection_id=1&buckets=501&metric=" + cpuMetricName,
			"Invalid buckets: must be between 1 and 500"},
		{"bad time range", "?connection_id=1&time_range=2y&metric=" + cpuMetricName,
			`invalid time range "2y": must be one of 1h, 6h, 24h, 7d, 30d, custom`},
		{"custom without bounds", "?connection_id=1&time_range=custom&metric=" + cpuMetricName,
			`invalid time range "custom": time_start and time_end are both required`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got *metrics.ComparisonRequest
			h := comparisonHandler(t, &got, &metrics.AnomalyComparison{}, nil)
			rec := httptest.NewRecorder()
			h.handleAnomalyComparison(rec, withSuperuser(
				httptest.NewRequest(http.MethodGet, comparisonURL+tt.query, nil)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
			}
			if resp := decodeError(t, rec); resp.Error != tt.wantErr {
				t.Errorf("error = %q, want %q", resp.Error, tt.wantErr)
			}
			if got != nil {
				t.Error("the comparison loaded for an invalid request")
			}
		})
	}
}

func TestHandleAnomalyComparison_MethodNotAllowed(t *testing.T) {
	var got *metrics.ComparisonRequest
	h := comparisonHandler(t, &got, nil, nil)
	for _, fn := range []http.HandlerFunc{h.handleAnomalyComparison, h.handleAnomalyComparisonMetrics} {
		rec := httptest.NewRecorder()
		fn(rec, httptest.NewRequest(http.MethodPost, comparisonURL, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", rec.Code)
		}
	}
}

func TestHandleAnomalyComparison_RefusesMarkedConnection(t *testing.T) {
	var got *metrics.ComparisonRequest
	h := comparisonHandler(t, &got, &metrics.AnomalyComparison{}, nil)
	h.sharingLookupFn = func(context.Context, int) (bool, string, error) {
		return false, "", errors.New("connection not found")
	}
	rec := httptest.NewRecorder()
	h.handleAnomalyComparison(rec, withSuperuser(httptest.NewRequest(http.MethodGet,
		comparisonURL+"?connection_id=3&metric="+cpuMetricName, nil)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	if resp := decodeError(t, rec); resp.Error !=
		"Permission denied: you do not have access to connection 3" {
		t.Errorf("error = %q", resp.Error)
	}
	if got != nil {
		t.Error("the comparison loaded for a refused connection")
	}
}

func TestHandleAnomalyComparison_Success(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		var got *metrics.ComparisonRequest
		asOf := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
		result := &metrics.AnomalyComparison{
			Metric: cpuMetricName, ConnectionID: 1, Model: "as_scored",
			Sensitivity: 3, DefaultSensitivity: 3, ScoringSource: "alerter",
			BaselinesAsOf: &asOf, Points: []metrics.ComparisonPoint{},
		}
		h := comparisonHandler(t, &got, result, nil)
		rec := httptest.NewRecorder()
		before := time.Now()
		h.handleAnomalyComparison(rec, withSuperuser(httptest.NewRequest(http.MethodGet,
			comparisonURL+"?connection_id=1&metric="+cpuMetricName, nil)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		if got == nil || got.ConnectionID != 1 || got.Metric != cpuMetricName ||
			got.Model != "as_scored" || got.Sensitivity != nil || got.Buckets != 300 ||
			got.DatabaseName != nil {
			t.Fatalf("request = %+v, want the defaults", got)
		}
		if span := got.Window.End.Sub(got.Window.Start); span != 24*time.Hour ||
			got.Window.End.Before(before.Add(-time.Second)) {
			t.Errorf("window = %+v, want the last 24 hours", got.Window)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, key := range []string{"metric", "connection_id", "database_name", "model",
			"sensitivity", "default_sensitivity", "scoring_source", "baselines_as_of", "points"} {
			if _, ok := body[key]; !ok {
				t.Errorf("response lacks %q: %s", key, rec.Body.String())
			}
		}
		if !strings.Contains(rec.Body.String(), `"baselines_as_of":"2026-10-28T12:00:00Z"`) ||
			!strings.Contains(rec.Body.String(), `"points":[]`) {
			t.Errorf("body = %s", rec.Body.String())
		}
	})

	t.Run("explicit parameters", func(t *testing.T) {
		var got *metrics.ComparisonRequest
		h := comparisonHandler(t, &got, &metrics.AnomalyComparison{}, nil)
		rec := httptest.NewRecorder()
		h.handleAnomalyComparison(rec, withSuperuser(httptest.NewRequest(http.MethodGet,
			comparisonURL+"?connection_id=2&metric="+tempMetric+
				"&database_name=sales&model=daily&sensitivity=2.5&buckets=50&time_range=7d", nil)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		if got == nil || got.DatabaseName == nil || *got.DatabaseName != "sales" ||
			got.Model != "daily" || got.Sensitivity == nil || *got.Sensitivity != 2.5 ||
			got.Buckets != 50 || got.Window.End.Sub(got.Window.Start) != 7*24*time.Hour {
			t.Errorf("request = %+v", got)
		}
	})
}

func TestHandleAnomalyComparison_LoadFailure(t *testing.T) {
	var got *metrics.ComparisonRequest
	h := comparisonHandler(t, &got, nil, errors.New("relation secret_table does not exist"))
	rec := httptest.NewRecorder()
	h.handleAnomalyComparison(rec, withSuperuser(httptest.NewRequest(http.MethodGet,
		comparisonURL+"?connection_id=1&metric="+cpuMetricName, nil)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error != "Failed to load the anomaly comparison" {
		t.Errorf("error = %q, want a generic message", resp.Error)
	}
}

func TestHandleAnomalyComparisonMetrics(t *testing.T) {
	h := &MetricsHandler{}
	rec := httptest.NewRecorder()
	h.handleAnomalyComparisonMetrics(rec, httptest.NewRequest(http.MethodGet,
		comparisonURL+"/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []ComparisonMetric
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	defs := anomaly.BaselineMetrics()
	if len(got) != len(defs) {
		t.Fatalf("got %d metrics, want %d", len(got), len(defs))
	}
	for i, def := range defs {
		if got[i] != (ComparisonMetric{def.Name, def.ProbeName, def.PerDatabase}) {
			t.Errorf("metric %d = %+v, want %+v", i, got[i], def)
		}
	}
	if !strings.Contains(rec.Body.String(),
		`{"name":"pg_sys_cpu_usage_info.processor_time_percent","probe_name":"pg_sys_cpu_usage_info","per_database":false}`) {
		t.Errorf("body lacks the CPU metric in the contract's shape: %s", rec.Body.String())
	}
}

func TestMetricsHandler_RegisterRoutes_Comparison(t *testing.T) {
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }

	unconfigured := http.NewServeMux()
	NewMetricsHandler(nil, nil).RegisterRoutes(unconfigured, noopWrapper)
	configured := http.NewServeMux()
	NewMetricsHandler(&database.Datastore{}, nil).RegisterRoutes(configured, noopWrapper)

	for _, path := range []string{comparisonURL, comparisonURL + "/metrics"} {
		rec := httptest.NewRecorder()
		unconfigured.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s without a datastore: status = %d, want 503", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	configured.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, comparisonURL+"/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("metrics list: status = %d, want 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	configured.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, comparisonURL, nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("comparison without parameters: status = %d, want 400", rec.Code)
	}
}
