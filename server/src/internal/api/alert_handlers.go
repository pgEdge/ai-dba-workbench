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
	"log"
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// AlertHandler handles REST API requests for alerts
type AlertHandler struct {
	datastore   *database.Datastore
	authStore   *auth.AuthStore
	rbacChecker *auth.RBACChecker
}

// NewAlertHandler creates a new alert handler
func NewAlertHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *AlertHandler {
	return &AlertHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
}

// RegisterRoutes registers alert management routes on the mux
func (h *AlertHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	// Check if datastore is configured
	if h.datastore == nil {
		notConfigured := HandleNotConfigured("Alert management")
		mux.HandleFunc("/api/v1/alerts", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/alerts/counts", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/alerts/acknowledge", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/alerts/analysis", authWrapper(notConfigured))
		return
	}

	mux.HandleFunc("/api/v1/alerts", authWrapper(h.handleAlerts))
	mux.HandleFunc("/api/v1/alerts/counts", authWrapper(h.handleAlertCounts))
	mux.HandleFunc("/api/v1/alerts/acknowledge", authWrapper(h.handleAcknowledge))
	mux.HandleFunc("/api/v1/alerts/analysis", authWrapper(h.handleSaveAnalysis))
}

// handleAlerts handles GET /api/v1/alerts
func (h *AlertHandler) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if !RequireGET(w, r) {
		return
	}

	filter := database.AlertListFilter{}

	// Parse connection_id (single) - silently ignore invalid values
	if id, ok := ParseQueryIntSilent(r, "connection_id"); ok {
		filter.ConnectionID = &id
	}

	// Parse connection_ids (multiple, comma-separated) - silently skip invalid
	filter.ConnectionIDs = ParseQueryIntListSilent(r, "connection_ids")

	// Parse status
	if status := ParseQueryString(r, "status"); status != "" {
		filter.Status = &status
	}

	// Parse exclude_cleared
	filter.ExcludeCleared = ParseQueryBool(r, "exclude_cleared")

	// Parse severity
	if severity := ParseQueryString(r, "severity"); severity != "" {
		filter.Severity = &severity
	}

	// Parse alert_type
	if alertType := ParseQueryString(r, "alert_type"); alertType != "" {
		filter.AlertType = &alertType
	}

	// Parse start_time - silently ignore invalid
	if startTime, ok := ParseQueryTimeSilent(r, "start_time"); ok {
		filter.StartTime = &startTime
	}

	// Parse end_time - silently ignore invalid
	if endTime, ok := ParseQueryTimeSilent(r, "end_time"); ok {
		filter.EndTime = &endTime
	}

	// Parse limit and offset with defaults
	filter.Limit = ParseLimitWithDefaults(r, 100, 1000)
	filter.Offset = ParseOffsetWithDefault(r, 0)

	// RBAC: restrict to accessible connections
	accessibleIDs := h.rbacChecker.GetAccessibleConnections(r.Context())
	if accessibleIDs != nil {
		if filter.ConnectionID != nil {
			// Check single connection_id is accessible
			found := false
			for _, aid := range accessibleIDs {
				if aid == *filter.ConnectionID {
					found = true
					break
				}
			}
			if !found {
				RespondJSON(w, http.StatusOK, &database.AlertListResult{Alerts: []database.Alert{}, Total: 0})
				return
			}
		}
		if len(filter.ConnectionIDs) > 0 {
			// Intersect user-specified IDs with accessible IDs
			accessibleSet := make(map[int]bool, len(accessibleIDs))
			for _, id := range accessibleIDs {
				accessibleSet[id] = true
			}
			var intersected []int
			for _, id := range filter.ConnectionIDs {
				if accessibleSet[id] {
					intersected = append(intersected, id)
				}
			}
			if len(intersected) == 0 {
				RespondJSON(w, http.StatusOK, &database.AlertListResult{Alerts: []database.Alert{}, Total: 0})
				return
			}
			filter.ConnectionIDs = intersected
		} else if filter.ConnectionID == nil {
			// No user filter -- restrict to accessible connections only
			filter.ConnectionIDs = accessibleIDs
		}
	}

	// Fetch alerts
	result, err := h.datastore.GetAlerts(r.Context(), filter)
	if err != nil {
		log.Printf("[ERROR] Failed to fetch alerts: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch alerts")
		return
	}

	RespondJSON(w, http.StatusOK, result)
}

// handleAlertCounts handles GET /api/v1/alerts/counts
// Returns counts of active alerts grouped by server
func (h *AlertHandler) handleAlertCounts(w http.ResponseWriter, r *http.Request) {
	if !RequireGET(w, r) {
		return
	}

	// Fetch alert counts
	counts, err := h.datastore.GetAlertCounts(r.Context())
	if err != nil {
		log.Printf("[ERROR] Failed to fetch alert counts: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch alert counts")
		return
	}

	// RBAC: filter counts to accessible connections only
	accessibleIDs := h.rbacChecker.GetAccessibleConnections(r.Context())
	if accessibleIDs != nil {
		accessibleSet := make(map[int]bool, len(accessibleIDs))
		for _, id := range accessibleIDs {
			accessibleSet[id] = true
		}
		var filteredTotal int64
		filteredByServer := make(map[int]int64)
		for connID, count := range counts.ByServer {
			if accessibleSet[connID] {
				filteredByServer[connID] = count
				filteredTotal += count
			}
		}
		counts.Total = filteredTotal
		counts.ByServer = filteredByServer
	}

	RespondJSON(w, http.StatusOK, counts)
}

// AcknowledgeRequest represents the request body for acknowledging an alert
type AcknowledgeRequest struct {
	AlertID       int64  `json:"alert_id"`
	Message       string `json:"message"`
	FalsePositive bool   `json:"false_positive"`
}

// handleAcknowledge handles POST /api/v1/alerts/acknowledge (acknowledge) and
// DELETE /api/v1/alerts/acknowledge (unacknowledge)
func (h *AlertHandler) handleAcknowledge(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.acknowledgeAlert(w, r)
	case http.MethodDelete:
		h.unacknowledgeAlert(w, r)
	default:
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// acknowledgeAlert handles POST /api/v1/alerts/acknowledge
func (h *AlertHandler) acknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req AcknowledgeRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.AlertID == 0 {
		RespondError(w, http.StatusBadRequest, "alert_id is required")
		return
	}

	// RBAC: verify the user has access to the alert's connection
	accessibleIDs := h.rbacChecker.GetAccessibleConnections(r.Context())
	if accessibleIDs != nil {
		connID, err := h.datastore.GetAlertConnectionID(r.Context(), req.AlertID)
		if err != nil {
			log.Printf("[ERROR] Failed to look up alert connection: %v", err)
			RespondError(w, http.StatusNotFound, "Alert not found")
			return
		}
		found := false
		for _, aid := range accessibleIDs {
			if aid == connID {
				found = true
				break
			}
		}
		if !found {
			RespondError(w, http.StatusForbidden, "Access denied")
			return
		}
	}

	// Get the username from the auth context
	username := auth.GetUsernameFromContext(r.Context())
	if username == "" {
		username = "unknown"
	}

	// Acknowledge the alert
	ackReq := database.AcknowledgeAlertRequest{
		AlertID:        req.AlertID,
		AcknowledgedBy: username,
		Message:        req.Message,
		FalsePositive:  req.FalsePositive,
	}

	if err := h.datastore.AcknowledgeAlert(r.Context(), ackReq); err != nil {
		log.Printf("[ERROR] Failed to acknowledge alert: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to acknowledge alert")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
}

// unacknowledgeAlert handles DELETE /api/v1/alerts/acknowledge
func (h *AlertHandler) unacknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	// Parse alert_id from query params (required)
	alertIDStr := ParseQueryString(r, "alert_id")
	if alertIDStr == "" {
		RespondError(w, http.StatusBadRequest, "alert_id query parameter is required")
		return
	}

	alertID, ok := ParseQueryInt64(w, r, "alert_id")
	if !ok {
		return // Error already sent
	}

	// RBAC: verify the user has access to the alert's connection
	accessibleIDs := h.rbacChecker.GetAccessibleConnections(r.Context())
	if accessibleIDs != nil {
		connID, err := h.datastore.GetAlertConnectionID(r.Context(), alertID)
		if err != nil {
			log.Printf("[ERROR] Failed to look up alert connection: %v", err)
			RespondError(w, http.StatusNotFound, "Alert not found")
			return
		}
		found := false
		for _, aid := range accessibleIDs {
			if aid == connID {
				found = true
				break
			}
		}
		if !found {
			RespondError(w, http.StatusForbidden, "Access denied")
			return
		}
	}

	// Unacknowledge the alert
	if err := h.datastore.UnacknowledgeAlert(r.Context(), alertID); err != nil {
		log.Printf("[ERROR] Failed to unacknowledge alert: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to unacknowledge alert")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

// SaveAnalysisRequest represents the request body for saving an AI analysis
type SaveAnalysisRequest struct {
	AlertID     int64   `json:"alert_id"`
	Analysis    string  `json:"analysis"`
	MetricValue float64 `json:"metric_value"`
}

// handleSaveAnalysis handles PUT /api/v1/alerts/analysis
func (h *AlertHandler) handleSaveAnalysis(w http.ResponseWriter, r *http.Request) {
	if !RequireMethod(w, r, http.MethodPut) {
		return
	}

	var req SaveAnalysisRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.AlertID == 0 {
		RespondError(w, http.StatusBadRequest, "alert_id is required")
		return
	}

	if req.Analysis == "" {
		RespondError(w, http.StatusBadRequest, "analysis is required")
		return
	}

	// RBAC: verify the user has access to the alert's connection
	accessibleIDs := h.rbacChecker.GetAccessibleConnections(r.Context())
	if accessibleIDs != nil {
		connID, err := h.datastore.GetAlertConnectionID(r.Context(), req.AlertID)
		if err != nil {
			log.Printf("[ERROR] Failed to look up alert connection: %v", err)
			RespondError(w, http.StatusNotFound, "Alert not found")
			return
		}
		found := false
		for _, aid := range accessibleIDs {
			if aid == connID {
				found = true
				break
			}
		}
		if !found {
			RespondError(w, http.StatusForbidden, "Access denied")
			return
		}
	}

	if err := h.datastore.SaveAlertAnalysis(r.Context(), req.AlertID, req.Analysis, req.MetricValue); err != nil {
		log.Printf("[ERROR] Failed to save analysis: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to save analysis")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
