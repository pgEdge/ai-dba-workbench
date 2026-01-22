/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

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
		mux.HandleFunc("/api/alerts", authWrapper(h.handleNotConfigured))
		mux.HandleFunc("/api/alerts/counts", authWrapper(h.handleNotConfigured))
		mux.HandleFunc("/api/alerts/acknowledge", authWrapper(h.handleNotConfigured))
		return
	}

	mux.HandleFunc("/api/alerts", authWrapper(h.handleAlerts))
	mux.HandleFunc("/api/alerts/counts", authWrapper(h.handleAlertCounts))
	mux.HandleFunc("/api/alerts/acknowledge", authWrapper(h.handleAcknowledge))
}

// handleNotConfigured returns a 503 when the datastore is not configured
func (h *AlertHandler) handleNotConfigured(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(ErrorResponse{Error: "Datastore not configured"})
}

// handleAlerts handles GET /api/alerts
func (h *AlertHandler) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed"})
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	filter := database.AlertListFilter{}

	// Parse connection_id (single)
	if connID := query.Get("connection_id"); connID != "" {
		id, err := strconv.Atoi(connID)
		if err == nil {
			filter.ConnectionID = &id
		}
	}

	// Parse connection_ids (multiple, comma-separated)
	if connIDs := query.Get("connection_ids"); connIDs != "" {
		ids := strings.Split(connIDs, ",")
		for _, idStr := range ids {
			if id, err := strconv.Atoi(strings.TrimSpace(idStr)); err == nil {
				filter.ConnectionIDs = append(filter.ConnectionIDs, id)
			}
		}
	}

	// Parse status
	if status := query.Get("status"); status != "" {
		filter.Status = &status
	}

	// Parse severity
	if severity := query.Get("severity"); severity != "" {
		filter.Severity = &severity
	}

	// Parse alert_type
	if alertType := query.Get("alert_type"); alertType != "" {
		filter.AlertType = &alertType
	}

	// Parse start_time
	if startTimeStr := query.Get("start_time"); startTimeStr != "" {
		if startTime, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
			filter.StartTime = &startTime
		}
	}

	// Parse end_time
	if endTimeStr := query.Get("end_time"); endTimeStr != "" {
		if endTime, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
			filter.EndTime = &endTime
		}
	}

	// Parse limit
	if limitStr := query.Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			filter.Limit = limit
		}
	}
	if filter.Limit == 0 {
		filter.Limit = 100
	}

	// Parse offset
	if offsetStr := query.Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			filter.Offset = offset
		}
	}

	// Fetch alerts
	result, err := h.datastore.GetAlerts(r.Context(), filter)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to fetch alerts: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleAlertCounts handles GET /api/alerts/counts
// Returns counts of active alerts grouped by server
func (h *AlertHandler) handleAlertCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed"})
		return
	}

	// Fetch alert counts
	counts, err := h.datastore.GetAlertCounts(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to fetch alert counts: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(counts)
}

// AcknowledgeRequest represents the request body for acknowledging an alert
type AcknowledgeRequest struct {
	AlertID       int64  `json:"alert_id"`
	Message       string `json:"message"`
	FalsePositive bool   `json:"false_positive"`
}

// handleAcknowledge handles POST /api/alerts/acknowledge (acknowledge) and
// DELETE /api/alerts/acknowledge (unacknowledge)
func (h *AlertHandler) handleAcknowledge(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.acknowledgeAlert(w, r)
	case http.MethodDelete:
		h.unacknowledgeAlert(w, r)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed"})
	}
}

// acknowledgeAlert handles POST /api/alerts/acknowledge
func (h *AlertHandler) acknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req AcknowledgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid request body: " + err.Error()})
		return
	}

	if req.AlertID == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "alert_id is required"})
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to acknowledge alert: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "acknowledged"})
}

// unacknowledgeAlert handles DELETE /api/alerts/acknowledge
func (h *AlertHandler) unacknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	// Parse alert_id from query params
	alertIDStr := r.URL.Query().Get("alert_id")
	if alertIDStr == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "alert_id query parameter is required"})
		return
	}

	alertID, err := strconv.ParseInt(alertIDStr, 10, 64)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid alert_id"})
		return
	}

	// Unacknowledge the alert
	if err := h.datastore.UnacknowledgeAlert(r.Context(), alertID); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to unacknowledge alert: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "active"})
}
