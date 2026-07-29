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
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// timeSeriesQueryFunc matches the signature of metrics.QueryTimeSeries.
// Tests inject a fake to assert the parsed MetricFilters (notably the
// IndexName the Index detail dashboard's Scan Activity chart depends on)
// actually reach the query layer, without requiring a live pool.
type timeSeriesQueryFunc func(
	ctx context.Context,
	pool *pgxpool.Pool,
	probeName string,
	connectionIDs []int,
	timeRange string,
	filters metrics.MetricFilters,
	buckets int,
	aggregation string,
	requestedMetrics []string,
) ([]metrics.MetricSeries, error)

// MetricsHandler handles REST API endpoints for monitoring dashboard
// metric queries and baselines.
type MetricsHandler struct {
	datastore *database.Datastore
	authStore *auth.AuthStore

	// queryTimeSeriesFn performs the time-series query. When nil the
	// handler falls back to metrics.QueryTimeSeries; tests substitute a
	// fake to capture the MetricFilters reaching the query layer.
	queryTimeSeriesFn timeSeriesQueryFunc
}

// NewMetricsHandler creates a new MetricsHandler.
func NewMetricsHandler(
	datastore *database.Datastore,
	authStore *auth.AuthStore,
) *MetricsHandler {
	return &MetricsHandler{
		datastore: datastore,
		authStore: authStore,
	}
}

// RegisterRoutes registers the metrics query endpoints on the mux.
func (h *MetricsHandler) RegisterRoutes(
	mux *http.ServeMux,
	authWrapper func(http.HandlerFunc) http.HandlerFunc,
) {
	if h.datastore == nil {
		notConfigured := HandleNotConfigured("Metrics")
		mux.HandleFunc("/api/v1/metrics/query",
			authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/metrics/baselines",
			authWrapper(notConfigured))
		return
	}

	mux.HandleFunc("/api/v1/metrics/query",
		authWrapper(h.handleMetricsQuery))
	mux.HandleFunc("/api/v1/metrics/baselines",
		authWrapper(h.handleMetricsBaselines))
}

// handleMetricsQuery handles GET /api/v1/metrics/query.
func (h *MetricsHandler) handleMetricsQuery(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !RequireGET(w, r) {
		return
	}

	// Parse connection IDs
	connectionIDs := h.parseConnectionIDs(w, r)
	if connectionIDs == nil {
		return
	}

	// RBAC check for each connection
	rbacChecker := auth.NewRBACCheckerWithSharing(h.authStore, h.datastore.GetConnectionSharingInfo)
	for _, connID := range connectionIDs {
		canAccess, _ := rbacChecker.CanAccessConnection(r.Context(), connID)
		if !canAccess {
			RespondError(w, http.StatusForbidden,
				fmt.Sprintf("Permission denied: you do not have access to connection %d", connID))
			return
		}
	}

	// Parse required probe_name
	probeName := ParseQueryString(r, "probe_name")
	if probeName == "" {
		RespondError(w, http.StatusBadRequest,
			"probe_name is required")
		return
	}
	if !metrics.IsValidIdentifier(probeName) {
		RespondError(w, http.StatusBadRequest,
			"Invalid probe_name: must contain only letters, numbers, and underscores")
		return
	}

	// Latest-row mode: when the caller supplies limit and/or order_by,
	// return the most recent raw rows instead of a bucketed time series.
	// The two modes produce genuinely different response shapes.
	limitStr := ParseQueryString(r, "limit")
	orderByParam := ParseQueryString(r, "order_by")
	if limitStr != "" || orderByParam != "" {
		h.handleLatestRows(w, r, connectionIDs, probeName, limitStr, orderByParam)
		return
	}

	// Parse time_range (default "1h")
	timeRange := ParseQueryString(r, "time_range")
	if timeRange == "" {
		timeRange = "1h"
	}
	if _, ok := metrics.ValidTimeRanges[timeRange]; !ok {
		RespondError(w, http.StatusBadRequest,
			"Invalid time_range: must be one of 1h, 6h, 24h, 7d, 30d")
		return
	}

	// Parse optional filters. QueryID pins the series to a single
	// pg_stat_statements statement; without it the Query detail dashboard's
	// charts would aggregate every statement on the connection.
	filters := metrics.MetricFilters{
		DatabaseName: ParseQueryString(r, "database_name"),
		SchemaName:   ParseQueryString(r, "schema_name"),
		TableName:    ParseQueryString(r, "table_name"),
		IndexName:    ParseQueryString(r, "index_name"),
		QueryID:      ParseQueryString(r, "queryid"),
	}

	// Parse buckets (default 150)
	buckets := 150
	if bucketsStr := ParseQueryString(r, "buckets"); bucketsStr != "" {
		b, err := strconv.Atoi(bucketsStr)
		if err != nil || b < 1 || b > 500 {
			RespondError(w, http.StatusBadRequest,
				"Invalid buckets: must be between 1 and 500")
			return
		}
		buckets = b
	}

	// Parse aggregation (default "avg")
	aggregation := ParseQueryString(r, "aggregation")
	if aggregation == "" {
		aggregation = "avg"
	}
	validAggs := map[string]bool{
		"avg": true, "sum": true, "min": true, "max": true, "last": true,
	}
	if !validAggs[strings.ToLower(aggregation)] {
		RespondError(w, http.StatusBadRequest,
			"Invalid aggregation: must be one of avg, sum, min, max, last")
		return
	}
	aggregation = strings.ToLower(aggregation)

	// Parse optional metrics filter
	var requestedMetrics []string
	if metricsStr := ParseQueryString(r, "metrics"); metricsStr != "" {
		parts := strings.Split(metricsStr, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				requestedMetrics = append(requestedMetrics, trimmed)
			}
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	pool := h.datastore.GetPool()
	queryFn := h.queryTimeSeriesFn
	if queryFn == nil {
		queryFn = metrics.QueryTimeSeries
	}
	result, err := queryFn(
		ctx, pool, probeName, connectionIDs, timeRange,
		filters, buckets, aggregation, requestedMetrics)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, result)
}

// handleLatestRows serves the latest-row variant of GET
// /api/v1/metrics/query, returning the most recent raw rows for the probe
// table as flat JSON objects keyed by column name.
func (h *MetricsHandler) handleLatestRows(
	w http.ResponseWriter,
	r *http.Request,
	connectionIDs []int,
	probeName string,
	limitStr string,
	orderByParam string,
) {
	// Parse limit (default 1) following the bounds-checking style used
	// for buckets: reject anything outside the accepted range.
	limit := 1
	if limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err != nil || l < 1 || l > 100 {
			RespondError(w, http.StatusBadRequest,
				"Invalid limit: must be between 1 and 100")
			return
		}
		limit = l
	}

	// Reject syntactically invalid order_by before column discovery; the
	// semantic column-existence check happens against discovered columns
	// inside QueryLatestRows.
	if orderByParam != "" && !metrics.IsValidIdentifier(orderByParam) {
		RespondError(w, http.StatusBadRequest,
			"Invalid order_by: must contain only letters, numbers, and underscores")
		return
	}

	// Parse order (default "desc") against an asc/desc allow-list.
	order := strings.ToLower(ParseQueryString(r, "order"))
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		RespondError(w, http.StatusBadRequest,
			"Invalid order: must be asc or desc")
		return
	}

	filters := metrics.MetricFilters{
		DatabaseName: ParseQueryString(r, "database_name"),
		SchemaName:   ParseQueryString(r, "schema_name"),
		TableName:    ParseQueryString(r, "table_name"),
		IndexName:    ParseQueryString(r, "index_name"),
		QueryID:      ParseQueryString(r, "queryid"),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	pool := h.datastore.GetPool()
	result, err := metrics.QueryLatestRows(
		ctx, pool, probeName, connectionIDs, filters,
		orderByParam, order, limit)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, result)
}

// handleMetricsBaselines handles GET /api/v1/metrics/baselines.
func (h *MetricsHandler) handleMetricsBaselines(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !RequireGET(w, r) {
		return
	}

	// Parse required connection_id
	connIDStr := ParseQueryString(r, "connection_id")
	if connIDStr == "" {
		RespondError(w, http.StatusBadRequest,
			"connection_id is required")
		return
	}
	connectionID, err := strconv.Atoi(connIDStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest,
			"Invalid connection_id")
		return
	}

	// RBAC check
	rbacChecker := auth.NewRBACCheckerWithSharing(h.authStore, h.datastore.GetConnectionSharingInfo)
	canAccess, _ := rbacChecker.CanAccessConnection(r.Context(), connectionID)
	if !canAccess {
		RespondError(w, http.StatusForbidden,
			fmt.Sprintf("Permission denied: you do not have access to connection %d", connectionID))
		return
	}

	// Parse required probe_name
	probeName := ParseQueryString(r, "probe_name")
	if probeName == "" {
		RespondError(w, http.StatusBadRequest,
			"probe_name is required")
		return
	}
	if !metrics.IsValidIdentifier(probeName) {
		RespondError(w, http.StatusBadRequest,
			"Invalid probe_name: must contain only letters, numbers, and underscores")
		return
	}

	// Parse optional metrics filter
	var requestedMetrics []string
	if metricsStr := ParseQueryString(r, "metrics"); metricsStr != "" {
		parts := strings.Split(metricsStr, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				requestedMetrics = append(requestedMetrics, trimmed)
			}
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	pool := h.datastore.GetPool()
	result, err := metrics.QueryBaselines(
		ctx, pool, connectionID, probeName, requestedMetrics)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, result)
}

// parseConnectionIDs extracts connection IDs from query parameters.
// It supports both connection_id (single) and connection_ids (comma-separated).
func (h *MetricsHandler) parseConnectionIDs(
	w http.ResponseWriter,
	r *http.Request,
) []int {
	// Try connection_ids first (comma-separated list)
	if ids, ok := ParseQueryIntList(w, r, "connection_ids"); ok {
		return ids
	}
	if r.URL.Query().Get("connection_ids") != "" {
		return nil // Error already sent by ParseQueryIntList
	}

	// Try single connection_id
	if id, ok := ParseQueryInt(w, r, "connection_id"); ok {
		return []int{id}
	}
	if r.URL.Query().Get("connection_id") != "" {
		return nil // Error already sent by ParseQueryInt
	}

	RespondError(w, http.StatusBadRequest,
		"Either connection_id or connection_ids is required")
	return nil
}
