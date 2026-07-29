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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

func TestNewMetricsHandler(t *testing.T) {
	handler := NewMetricsHandler(nil, nil)
	if handler == nil {
		t.Fatal("NewMetricsHandler returned nil")
	}
	if handler.datastore != nil {
		t.Error("expected nil datastore")
	}
	if handler.authStore != nil {
		t.Error("expected nil authStore")
	}
}

func TestMetricsHandler_RegisterRoutes_NotConfigured(t *testing.T) {
	handler := NewMetricsHandler(nil, nil)
	mux := http.NewServeMux()
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }
	handler.RegisterRoutes(mux, noopWrapper)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/query", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d",
			http.StatusServiceUnavailable, rec.Code)
	}
}

// decodeError reads an ErrorResponse from the recorder.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return response
}

func TestHandleMetricsQuery_MissingConnectionID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?probe_name=pg_stat_all_tables", nil)
	rec := httptest.NewRecorder()

	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error !=
		"Either connection_id or connection_ids is required" {
		t.Errorf("unexpected error: %q", resp.Error)
	}
}

func TestHandleMetricsQuery_MissingProbeName(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1", nil)
	rec := httptest.NewRecorder()

	// With nil authStore the RBAC checker treats the caller as a
	// superuser, so we reach probe_name validation.
	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error != "probe_name is required" {
		t.Errorf("unexpected error: %q", resp.Error)
	}
}

func TestHandleMetricsQuery_InvalidProbeName(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1&probe_name=bad;name", nil)
	rec := httptest.NewRecorder()

	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
}

func TestHandleMetricsQuery_LatestMode_InvalidLimit(t *testing.T) {
	cases := []string{"0", "-1", "101", "abc"}
	for _, limit := range cases {
		t.Run("limit="+limit, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet,
				"/api/v1/metrics/query?connection_id=1"+
					"&probe_name=pg_stat_all_tables&limit="+limit, nil)
			rec := httptest.NewRecorder()

			handler := &MetricsHandler{}
			handler.handleMetricsQuery(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d",
					http.StatusBadRequest, rec.Code)
			}
			if resp := decodeError(t, rec); resp.Error !=
				"Invalid limit: must be between 1 and 100" {
				t.Errorf("unexpected error: %q", resp.Error)
			}
		})
	}
}

func TestHandleMetricsQuery_LatestMode_InvalidOrderBy(t *testing.T) {
	// A syntactically invalid order_by is rejected at the handler before
	// any column discovery or SQL execution occurs.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_all_tables&order_by=bad-column", nil)
	rec := httptest.NewRecorder()

	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error !=
		"Invalid order_by: must contain only letters, numbers, and underscores" {
		t.Errorf("unexpected error: %q", resp.Error)
	}
}

func TestHandleMetricsQuery_LatestMode_InvalidOrder(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_all_tables&limit=1"+
			"&order_by=n_live_tup&order=sideways", nil)
	rec := httptest.NewRecorder()

	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error !=
		"Invalid order: must be asc or desc" {
		t.Errorf("unexpected error: %q", resp.Error)
	}
}

func TestHandleMetricsQuery_TimeSeriesMode_InvalidTimeRange(t *testing.T) {
	// Without limit/order_by the handler stays on the time-series path;
	// an invalid time_range is rejected there.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_all_tables&time_range=99z", nil)
	rec := httptest.NewRecorder()

	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error !=
		`invalid time range "99z": must be one of 1h, 6h, 24h, 7d, 30d, custom` {
		t.Errorf("unexpected error: %q", resp.Error)
	}
}

func TestHandleMetricsQuery_TimeSeriesMode_ParsesIndexNameFilter(t *testing.T) {
	// The Index detail dashboard's Scan Activity chart relies on the
	// time-series path forwarding index_name into MetricFilters.IndexName.
	// Inject a fake query function to capture the filters the handler
	// actually passes downstream; the request uses only valid parameters
	// so the handler reaches the query layer instead of short-circuiting
	// on a validation error. This test fails if index_name parsing on the
	// time-series path is removed or broken.
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
		) ([]metrics.MetricSeries, error) {
			called = true
			gotFilters = filters
			return []metrics.MetricSeries{}, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_all_indexes&time_range=1h"+
			"&index_name=pk_orders", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("expected time-series query function to be called")
	}
	if gotFilters.IndexName != "pk_orders" {
		t.Errorf("expected IndexName %q reaching the query layer, got %q",
			"pk_orders", gotFilters.IndexName)
	}
}

func TestHandleMetricsQuery_TimeSeriesMode_DefaultQueryFn(t *testing.T) {
	// Exercises the nil-queryTimeSeriesFn fallback: a handler built without
	// an injected query function must resolve to metrics.QueryTimeSeries. A
	// real pool lets that default run; an unknown probe makes the query
	// return an error the handler maps to 400, proving the default wiring
	// reaches the query layer instead of relying on the injected seam.
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping datastore integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("Test database ping failed: %v", err)
	}

	handler := &MetricsHandler{datastore: database.NewTestDatastore(pool)}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=zzz_no_such_probe&time_range=1h", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

// newWindowCapturingHandler returns a handler whose injected query function
// records the resolved metrics.TimeWindow reaching the query layer.
func newWindowCapturingHandler(
	got *metrics.TimeWindow,
	called *bool,
) *MetricsHandler {
	return &MetricsHandler{
		datastore: &database.Datastore{},
		queryTimeSeriesFn: func(
			_ context.Context,
			_ *pgxpool.Pool,
			_ string,
			_ []int,
			window metrics.TimeWindow,
			_ metrics.MetricFilters,
			_ int,
			_ string,
			_ []string,
		) ([]metrics.MetricSeries, error) {
			*called = true
			*got = window
			return []metrics.MetricSeries{}, nil
		},
	}
}

func TestHandleMetricsQuery_CustomWindowReachesQueryLayer(t *testing.T) {
	// A custom range must arrive at the query layer as the exact absolute
	// window the caller asked for, since the bucketing derives from it.
	var got metrics.TimeWindow
	called := false
	handler := newWindowCapturingHandler(&got, &called)

	start := "2026-07-01T00:00:00Z"
	end := "2026-07-02T06:30:00Z"
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_all_tables&time_range=custom"+
			"&time_start="+start+"&time_end="+end, nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("expected the time-series query function to be called")
	}
	wantStart, _ := time.Parse(time.RFC3339, start)
	wantEnd, _ := time.Parse(time.RFC3339, end)
	if !got.Start.Equal(wantStart) || !got.End.Equal(wantEnd) {
		t.Errorf("window = %v..%v, want %v..%v",
			got.Start, got.End, wantStart, wantEnd)
	}
}

func TestHandleMetricsQuery_DefaultTimeRangeReachesQueryLayer(t *testing.T) {
	// Omitting time_range keeps the historical one-hour default.
	var got metrics.TimeWindow
	called := false
	handler := newWindowCapturingHandler(&got, &called)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_all_tables", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("expected the time-series query function to be called")
	}
	span := got.End.Sub(got.Start)
	if span < 59*time.Minute || span > 61*time.Minute {
		t.Errorf("default window span = %v, want ~1h", span)
	}
}

func TestHandleMetricsQuery_CustomWindowFutureEndClamped(t *testing.T) {
	// A picker set to today routinely overshoots the present moment, so the
	// handler must clamp rather than reject.
	var got metrics.TimeWindow
	called := false
	handler := newWindowCapturingHandler(&got, &called)

	start := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	end := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_all_tables&time_range=custom"+
			"&time_start="+start+"&time_end="+end, nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("expected the time-series query function to be called")
	}
	if got.End.After(time.Now().UTC()) {
		t.Errorf("window end %v was not clamped to now", got.End)
	}
}

func TestHandleMetricsQuery_CustomWindowRejections(t *testing.T) {
	// Every rejection ResolveTimeWindow can raise must surface as a 400
	// carrying the resolver's own message.
	now := time.Now().UTC()
	iso := func(t time.Time) string { return t.Format(time.RFC3339) }

	tests := []struct {
		name    string
		query   string
		wantErr string
	}{
		{
			name:    "missing both timestamps",
			query:   "&time_range=custom",
			wantErr: "time_start and time_end are both required",
		},
		{
			name:    "missing end",
			query:   "&time_range=custom&time_start=" + iso(now.Add(-time.Hour)),
			wantErr: "time_start and time_end are both required",
		},
		{
			name: "unparsable start",
			query: "&time_range=custom&time_start=yesterday&time_end=" +
				iso(now),
			wantErr: "must be an RFC 3339 timestamp",
		},
		{
			name: "end before start",
			query: "&time_range=custom&time_start=" + iso(now.Add(-time.Hour)) +
				"&time_end=" + iso(now.Add(-2*time.Hour)),
			wantErr: "time_end must be after time_start",
		},
		{
			name: "start in the future",
			query: "&time_range=custom&time_start=" + iso(now.Add(time.Hour)) +
				"&time_end=" + iso(now.Add(2*time.Hour)),
			wantErr: "invalid time_start: must not be in the future",
		},
		{
			name: "span beyond the cap",
			query: "&time_range=custom&time_start=" +
				iso(now.Add(-400*24*time.Hour)) + "&time_end=" + iso(now),
			wantErr: "span must not exceed 366 days",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet,
				"/api/v1/metrics/query?connection_id=1"+
					"&probe_name=pg_stat_all_tables"+tt.query, nil)
			rec := httptest.NewRecorder()

			handler := &MetricsHandler{}
			handler.handleMetricsQuery(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d (body %q)",
					http.StatusBadRequest, rec.Code, rec.Body.String())
			}
			if resp := decodeError(t, rec); !strings.Contains(
				resp.Error, tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q",
					resp.Error, tt.wantErr)
			}
		})
	}
}

func TestHandleMetricsQuery_RejectsNonGET(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_all_tables", nil)
	rec := httptest.NewRecorder()

	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d",
			http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestHandleMetricsQuery_RejectsNonIdentifierProbeName(t *testing.T) {
	// A percent-encoded separator survives query parsing, so the probe name
	// reaches the identifier check with an illegal character in it.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1&probe_name=bad%3Bname", nil)
	rec := httptest.NewRecorder()

	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error !=
		"Invalid probe_name: must contain only letters, numbers, and underscores" {
		t.Errorf("unexpected error: %q", resp.Error)
	}
}

func TestHandleMetricsQuery_TimeSeriesMode_ParameterValidation(t *testing.T) {
	// The remaining time-series parameters are validated ahead of the query
	// layer; each bad value yields a 400 with its own message.
	tests := []struct {
		name    string
		query   string
		wantErr string
	}{
		{
			name:    "buckets below the range",
			query:   "&buckets=0",
			wantErr: "Invalid buckets: must be between 1 and 500",
		},
		{
			name:    "buckets above the range",
			query:   "&buckets=501",
			wantErr: "Invalid buckets: must be between 1 and 500",
		},
		{
			name:    "buckets not a number",
			query:   "&buckets=lots",
			wantErr: "Invalid buckets: must be between 1 and 500",
		},
		{
			name:    "unknown aggregation",
			query:   "&aggregation=median",
			wantErr: "Invalid aggregation: must be one of avg, sum, min, max, last",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet,
				"/api/v1/metrics/query?connection_id=1"+
					"&probe_name=pg_stat_all_tables"+tt.query, nil)
			rec := httptest.NewRecorder()

			handler := &MetricsHandler{}
			handler.handleMetricsQuery(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d (body %q)",
					http.StatusBadRequest, rec.Code, rec.Body.String())
			}
			if resp := decodeError(t, rec); resp.Error != tt.wantErr {
				t.Errorf("error = %q, want %q", resp.Error, tt.wantErr)
			}
		})
	}
}

func TestHandleMetricsQuery_TimeSeriesMode_ForwardsBucketsAndMetrics(t *testing.T) {
	// Valid buckets, a mixed-case aggregation, and a metrics list with an
	// empty element all reach the query layer normalised.
	var gotBuckets int
	var gotAggregation string
	var gotMetrics []string
	handler := &MetricsHandler{
		datastore: &database.Datastore{},
		queryTimeSeriesFn: func(
			_ context.Context,
			_ *pgxpool.Pool,
			_ string,
			_ []int,
			_ metrics.TimeWindow,
			_ metrics.MetricFilters,
			buckets int,
			aggregation string,
			requestedMetrics []string,
		) ([]metrics.MetricSeries, error) {
			gotBuckets = buckets
			gotAggregation = aggregation
			gotMetrics = requestedMetrics
			return []metrics.MetricSeries{}, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_all_tables&buckets=42&aggregation=MAX"+
			"&metrics=seq_scan,,%20idx_scan%20", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	if gotBuckets != 42 {
		t.Errorf("buckets = %d, want 42", gotBuckets)
	}
	if gotAggregation != "max" {
		t.Errorf("aggregation = %q, want %q", gotAggregation, "max")
	}
	if len(gotMetrics) != 2 ||
		gotMetrics[0] != "seq_scan" || gotMetrics[1] != "idx_scan" {
		t.Errorf("metrics = %v, want [seq_scan idx_scan]", gotMetrics)
	}
}
