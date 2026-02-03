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
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// AlertHandler handles REST API requests for alerts
type AlertHandler struct {
	datastore *database.Datastore
	authStore *auth.AuthStore
}

// NewAlertHandler creates a new alert handler
func NewAlertHandler(datastore *database.Datastore, authStore *auth.AuthStore) *AlertHandler {
	return &AlertHandler{
		datastore: datastore,
		authStore: authStore,
	}
}

// RegisterRoutes registers alert management routes on the mux
func (h *AlertHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	// Check if datastore is configured
	if h.datastore == nil {
		mux.HandleFunc("/api/v1/alerts", authWrapper(h.handleNotConfigured))
		mux.HandleFunc("/api/v1/alerts/counts", authWrapper(h.handleNotConfigured))
		mux.HandleFunc("/api/v1/alerts/acknowledge", authWrapper(h.handleNotConfigured))
		return
	}

	mux.HandleFunc("/api/v1/alerts", authWrapper(h.handleAlerts))
	mux.HandleFunc("/api/v1/alerts/counts", authWrapper(h.handleAlertCounts))
	mux.HandleFunc("/api/v1/alerts/acknowledge", authWrapper(h.handleAcknowledge))
}

// handleNotConfigured returns a 503 when the datastore is not configured
func (h *AlertHandler) handleNotConfigured(w http.ResponseWriter, r *http.Request) {
	RespondError(w, http.StatusServiceUnavailable, "Datastore not configured")
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

	// Fetch alerts
	result, err := h.datastore.GetAlerts(r.Context(), filter)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to fetch alerts: "+err.Error())
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
		RespondError(w, http.StatusInternalServerError, "Failed to fetch alert counts: "+err.Error())
		return
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
		RespondError(w, http.StatusInternalServerError, "Failed to acknowledge alert: "+err.Error())
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

	// Unacknowledge the alert
	if err := h.datastore.UnacknowledgeAlert(r.Context(), alertID); err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to unacknowledge alert: "+err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "active"})
}
