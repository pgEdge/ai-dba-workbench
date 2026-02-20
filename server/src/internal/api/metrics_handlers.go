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

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// MetricsHandler handles REST API endpoints for monitoring dashboard
// metric queries and baselines.
type MetricsHandler struct {
	datastore *database.Datastore
	authStore *auth.AuthStore
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
	rbacChecker := auth.NewRBACChecker(h.authStore)
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

	// Parse optional filters
	filters := metrics.MetricFilters{
		DatabaseName: ParseQueryString(r, "database_name"),
		SchemaName:   ParseQueryString(r, "schema_name"),
		TableName:    ParseQueryString(r, "table_name"),
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
	result, err := metrics.QueryTimeSeries(
		ctx, pool, probeName, connectionIDs, timeRange,
		filters, buckets, aggregation, requestedMetrics)
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
	rbacChecker := auth.NewRBACChecker(h.authStore)
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
