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
	"log"
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// alertConnectionResolver is the narrow contract the alert mutation
// handlers use to look up which connection owns an alert before running
// the RBAC gate. The concrete *database.Datastore satisfies this
// interface via GetAlertConnectionID; tests inject a lightweight fake so
// the RBAC branch can be exercised without a real database.
type alertConnectionResolver interface {
	GetAlertConnectionID(ctx context.Context, alertID int64) (int, error)
}

// AlertHandler handles REST API requests for alerts
type AlertHandler struct {
	datastore     *database.Datastore
	authStore     *auth.AuthStore
	rbacChecker   *auth.RBACChecker
	alertResolver alertConnectionResolver
}

// NewAlertHandler creates a new alert handler
func NewAlertHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *AlertHandler {
	h := &AlertHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
	if datastore != nil {
		h.alertResolver = datastore
	}
	return h
}

// setAlertResolver overrides the alert-connection resolver for tests. The
// production constructor defaults to the datastore; tests replace it with
// a fake that returns a known connection ID without touching the
// database.
func (h *AlertHandler) setAlertResolver(r alertConnectionResolver) {
	h.alertResolver = r
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

	// RBAC: restrict to visible connections. VisibleConnectionIDs loads
	// sharing info once and returns the full allow-list including owned
	// connections and shared ones the caller may see.
	lister := newConnectionVisibilityLister(h.datastore)
	accessibleIDs, allConnections, err := h.rbacChecker.VisibleConnectionIDs(r.Context(), lister)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for alerts: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to list alerts")
		return
	}
	if !allConnections {
		if filter.ConnectionID != nil {
			// Check single connection_id is visible.
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
			// Intersect user-specified IDs with visible IDs.
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
			// No user filter -- restrict to visible connections only.
			if len(accessibleIDs) == 0 {
				RespondJSON(w, http.StatusOK, &database.AlertListResult{Alerts: []database.Alert{}, Total: 0})
				return
			}
			filter.ConnectionIDs = accessibleIDs
		}
	}

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

	// RBAC first: resolve the caller's visible connection set before any
	// datastore work so a zero-grant caller never hits the alerts table.
	// Visibility includes owned and shared connections, not just
	// group/token grants.
	lister := newConnectionVisibilityLister(h.datastore)
	accessibleIDs, allConnections, err := h.rbacChecker.VisibleConnectionIDs(r.Context(), lister)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for alert counts: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch alert counts")
		return
	}

	// Zero-grant caller: return an empty counts response without
	// invoking GetAlertCounts. A nil datastore test can rely on this
	// short-circuit to prove the RBAC gate runs before any database
	// call.
	if !allConnections && len(accessibleIDs) == 0 {
		RespondJSON(w, http.StatusOK, &database.AlertCountsResult{
			Total:    0,
			ByServer: map[int]int64{},
		})
		return
	}

	// Push the caller's allow-list into the query. A nil slice (superuser
	// or wildcard scope) means "no filter"; a non-nil slice restricts the
	// SQL to the caller's visible connection IDs.
	var filterIDs []int
	if !allConnections {
		filterIDs = accessibleIDs
	}
	counts, err := h.datastore.GetAlertCounts(r.Context(), filterIDs)
	if err != nil {
		log.Printf("[ERROR] Failed to fetch alert counts: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch alert counts")
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

	// Resolve the alert's connection via the narrow resolver interface so
	// tests can inject a fake without stubbing the full datastore. This
	// is the minimum datastore work required before the RBAC gate; the
	// gate runs immediately after so no mutation datastore call executes
	// for a denied caller.
	if h.alertResolver == nil {
		log.Printf("[ERROR] acknowledgeAlert: alert resolver is not configured")
		RespondError(w, http.StatusInternalServerError, "Failed to acknowledge alert")
		return
	}
	connID, err := h.alertResolver.GetAlertConnectionID(r.Context(), req.AlertID)
	if err != nil {
		log.Printf("[ERROR] Failed to look up alert connection: %v", err)
		RespondError(w, http.StatusNotFound, "Alert not found")
		return
	}
	if canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), connID); !canAccess {
		RespondError(w, http.StatusForbidden, "Access denied")
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

	// Resolve the alert's connection via the narrow resolver interface so
	// tests can inject a fake without stubbing the full datastore. The
	// RBAC gate runs immediately after so no mutation datastore call
	// executes for a denied caller.
	if h.alertResolver == nil {
		log.Printf("[ERROR] unacknowledgeAlert: alert resolver is not configured")
		RespondError(w, http.StatusInternalServerError, "Failed to unacknowledge alert")
		return
	}
	connID, err := h.alertResolver.GetAlertConnectionID(r.Context(), alertID)
	if err != nil {
		log.Printf("[ERROR] Failed to look up alert connection: %v", err)
		RespondError(w, http.StatusNotFound, "Alert not found")
		return
	}
	if canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), connID); !canAccess {
		RespondError(w, http.StatusForbidden, "Access denied")
		return
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

	// Resolve the alert's connection via the narrow resolver interface so
	// tests can inject a fake without stubbing the full datastore. The
	// RBAC gate runs immediately after so no write call executes for a
	// denied caller.
	if h.alertResolver == nil {
		log.Printf("[ERROR] handleSaveAnalysis: alert resolver is not configured")
		RespondError(w, http.StatusInternalServerError, "Failed to save analysis")
		return
	}
	connID, err := h.alertResolver.GetAlertConnectionID(r.Context(), req.AlertID)
	if err != nil {
		log.Printf("[ERROR] Failed to look up alert connection: %v", err)
		RespondError(w, http.StatusNotFound, "Alert not found")
		return
	}
	if canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), connID); !canAccess {
		RespondError(w, http.StatusForbidden, "Access denied")
		return
	}

	if err := h.datastore.SaveAlertAnalysis(r.Context(), req.AlertID, req.Analysis, req.MetricValue); err != nil {
		log.Printf("[ERROR] Failed to save analysis: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to save analysis")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
