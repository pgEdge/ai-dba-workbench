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
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// BlackoutHandler handles REST API requests for blackout management
type BlackoutHandler struct {
	datastore       *database.Datastore
	authStore       *auth.AuthStore
	rbacChecker     *auth.RBACChecker
	checkPermission func(http.ResponseWriter, *http.Request) bool
}

// NewBlackoutHandler creates a new blackout handler
func NewBlackoutHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *BlackoutHandler {
	h := &BlackoutHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
	if rbacChecker != nil {
		h.checkPermission = RequireAdminPermission(rbacChecker, auth.PermManageBlackouts, "manage blackouts")
	}
	return h
}

// RegisterRoutes registers blackout management routes on the mux
func (h *BlackoutHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	if h.datastore == nil {
		notConfigured := HandleNotConfigured("Blackout management")
		mux.HandleFunc("/api/v1/blackouts", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/blackouts/", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/blackout-schedules", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/blackout-schedules/", authWrapper(notConfigured))
		return
	}

	mux.HandleFunc("/api/v1/blackouts", authWrapper(h.handleBlackouts))
	mux.HandleFunc("/api/v1/blackouts/", authWrapper(h.handleBlackoutSubpath))
	mux.HandleFunc("/api/v1/blackout-schedules", authWrapper(h.handleBlackoutSchedules))
	mux.HandleFunc("/api/v1/blackout-schedules/", authWrapper(h.handleBlackoutScheduleSubpath))
}

// BlackoutCreateRequest is the request body for creating a blackout
type BlackoutCreateRequest struct {
	Scope        string  `json:"scope"`
	GroupID      *int    `json:"group_id,omitempty"`
	ClusterID    *int    `json:"cluster_id,omitempty"`
	ConnectionID *int    `json:"connection_id,omitempty"`
	DatabaseName *string `json:"database_name,omitempty"`
	Reason       string  `json:"reason"`
	StartTime    string  `json:"start_time"`
	EndTime      string  `json:"end_time"`
}

// BlackoutUpdateRequest is the request body for updating a blackout
type BlackoutUpdateRequest struct {
	Reason  *string `json:"reason,omitempty"`
	EndTime *string `json:"end_time,omitempty"`
}

// BlackoutScheduleRequest is the request body for creating or updating a schedule
type BlackoutScheduleRequest struct {
	Scope           string  `json:"scope"`
	GroupID         *int    `json:"group_id,omitempty"`
	ClusterID       *int    `json:"cluster_id,omitempty"`
	ConnectionID    *int    `json:"connection_id,omitempty"`
	DatabaseName    *string `json:"database_name,omitempty"`
	Name            string  `json:"name"`
	CronExpression  string  `json:"cron_expression"`
	DurationMinutes int     `json:"duration_minutes"`
	Timezone        string  `json:"timezone"`
	Reason          string  `json:"reason"`
	Enabled         *bool   `json:"enabled,omitempty"`
}

// handleBlackouts handles GET/POST /api/v1/blackouts
func (h *BlackoutHandler) handleBlackouts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listBlackouts(w, r)
	case http.MethodPost:
		h.createBlackout(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBlackoutSubpath handles /api/v1/blackouts/{id} and /api/v1/blackouts/{id}/stop
func (h *BlackoutHandler) handleBlackoutSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/blackouts/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid blackout ID")
		return
	}

	// Handle /api/v1/blackouts/{id}/stop
	if len(parts) == 2 && parts[1] == "stop" {
		if !RequirePOST(w, r) {
			return
		}
		h.stopBlackout(w, r, id)
		return
	}

	// Handle /api/v1/blackouts/{id}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.getBlackout(w, r, id)
		case http.MethodPut:
			h.updateBlackout(w, r, id)
		case http.MethodDelete:
			h.deleteBlackout(w, r, id)
		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.NotFound(w, r)
}

// handleBlackoutSchedules handles GET/POST /api/v1/blackout-schedules
func (h *BlackoutHandler) handleBlackoutSchedules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listBlackoutSchedules(w, r)
	case http.MethodPost:
		h.createBlackoutSchedule(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBlackoutScheduleSubpath handles /api/v1/blackout-schedules/{id}
func (h *BlackoutHandler) handleBlackoutScheduleSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/blackout-schedules/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid blackout schedule ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getBlackoutSchedule(w, r, id)
	case http.MethodPut:
		h.updateBlackoutSchedule(w, r, id)
	case http.MethodDelete:
		h.deleteBlackoutSchedule(w, r, id)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// validateBlackoutScope validates that the scope and associated IDs are consistent
func validateBlackoutScope(scope string, groupID, clusterID, connectionID *int) error {
	if !database.ValidBlackoutScopes[scope] {
		return fmt.Errorf("invalid scope: %s (must be estate, group, cluster, or server)", scope)
	}

	switch database.BlackoutScope(scope) {
	case database.BlackoutScopeEstate:
		if groupID != nil || clusterID != nil || connectionID != nil {
			return fmt.Errorf("estate scope must not specify group_id, cluster_id, or connection_id")
		}
	case database.BlackoutScopeGroup:
		if groupID == nil {
			return fmt.Errorf("group scope requires group_id")
		}
		if clusterID != nil || connectionID != nil {
			return fmt.Errorf("group scope must not specify cluster_id or connection_id")
		}
	case database.BlackoutScopeCluster:
		if clusterID == nil {
			return fmt.Errorf("cluster scope requires cluster_id")
		}
		if groupID != nil || connectionID != nil {
			return fmt.Errorf("cluster scope must not specify group_id or connection_id")
		}
	case database.BlackoutScopeServer:
		if connectionID == nil {
			return fmt.Errorf("server scope requires connection_id")
		}
		if groupID != nil || clusterID != nil {
			return fmt.Errorf("server scope must not specify group_id or cluster_id")
		}
	}

	return nil
}

// listBlackouts handles GET /api/v1/blackouts
func (h *BlackoutHandler) listBlackouts(w http.ResponseWriter, r *http.Request) {
	filter := database.BlackoutFilter{}

	if scope := ParseQueryString(r, "scope"); scope != "" {
		filter.Scope = &scope
	}
	if id, ok := ParseQueryIntSilent(r, "group_id"); ok {
		filter.GroupID = &id
	}
	if id, ok := ParseQueryIntSilent(r, "cluster_id"); ok {
		filter.ClusterID = &id
	}
	if id, ok := ParseQueryIntSilent(r, "connection_id"); ok {
		filter.ConnectionID = &id
	}
	if activeStr := ParseQueryString(r, "active"); activeStr != "" {
		active := activeStr == "true" || activeStr == "1"
		filter.Active = &active
	}

	filter.Limit = ParseLimitWithDefaults(r, 100, 1000)
	filter.Offset = ParseOffsetWithDefault(r, 0)

	result, err := h.datastore.ListBlackouts(r.Context(), filter)
	if err != nil {
		log.Printf("[ERROR] Failed to fetch blackouts: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch blackouts")
		return
	}

	RespondJSON(w, http.StatusOK, result)
}

// getBlackout handles GET /api/v1/blackouts/{id}
func (h *BlackoutHandler) getBlackout(w http.ResponseWriter, r *http.Request, id int64) {
	blackout, err := h.datastore.GetBlackout(r.Context(), id)
	if err != nil {
		if errors.Is(err, database.ErrBlackoutNotFound) {
			RespondError(w, http.StatusNotFound, "Blackout not found")
			return
		}
		log.Printf("[ERROR] Failed to fetch blackout: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch blackout")
		return
	}

	RespondJSON(w, http.StatusOK, blackout)
}

// createBlackout handles POST /api/v1/blackouts
func (h *BlackoutHandler) createBlackout(w http.ResponseWriter, r *http.Request) {
	if !h.checkPermission(w, r) {
		return
	}

	var req BlackoutCreateRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Validate scope
	if err := validateBlackoutScope(req.Scope, req.GroupID, req.ClusterID, req.ConnectionID); err != nil {
		log.Printf("[ERROR] Request error: %v", err)
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Parse times
	startTime, err := time.Parse(time.RFC3339, req.StartTime)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid start_time format, expected RFC3339")
		return
	}
	endTime, err := time.Parse(time.RFC3339, req.EndTime)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid end_time format, expected RFC3339")
		return
	}
	if !endTime.After(startTime) {
		RespondError(w, http.StatusBadRequest, "end_time must be after start_time")
		return
	}

	// Get created_by from auth context
	username := auth.GetUsernameFromContext(r.Context())
	if username == "" {
		username = "unknown"
	}

	blackout := &database.Blackout{
		Scope:        req.Scope,
		GroupID:      req.GroupID,
		ClusterID:    req.ClusterID,
		ConnectionID: req.ConnectionID,
		DatabaseName: req.DatabaseName,
		Reason:       req.Reason,
		StartTime:    startTime,
		EndTime:      endTime,
		CreatedBy:    username,
	}

	if err := h.datastore.CreateBlackout(r.Context(), blackout); err != nil {
		log.Printf("[ERROR] Failed to create blackout: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to create blackout")
		return
	}

	// Compute is_active for the response
	now := time.Now()
	blackout.IsActive = !blackout.StartTime.After(now) && !blackout.EndTime.Before(now)

	RespondJSON(w, http.StatusCreated, blackout)
}

// updateBlackout handles PUT /api/v1/blackouts/{id}
func (h *BlackoutHandler) updateBlackout(w http.ResponseWriter, r *http.Request, id int64) {
	if !h.checkPermission(w, r) {
		return
	}

	var req BlackoutUpdateRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Fetch existing blackout
	existing, err := h.datastore.GetBlackout(r.Context(), id)
	if err != nil {
		if errors.Is(err, database.ErrBlackoutNotFound) {
			RespondError(w, http.StatusNotFound, "Blackout not found")
			return
		}
		log.Printf("[ERROR] Failed to fetch blackout: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch blackout")
		return
	}

	reason := existing.Reason
	if req.Reason != nil {
		reason = *req.Reason
	}

	endTime := existing.EndTime
	if req.EndTime != nil {
		parsed, err := time.Parse(time.RFC3339, *req.EndTime)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid end_time format, expected RFC3339")
			return
		}
		if !parsed.After(existing.StartTime) {
			RespondError(w, http.StatusBadRequest, "end_time must be after start_time")
			return
		}
		endTime = parsed
	}

	if err := h.datastore.UpdateBlackout(r.Context(), id, reason, endTime); err != nil {
		if errors.Is(err, database.ErrBlackoutNotFound) {
			RespondError(w, http.StatusNotFound, "Blackout not found")
			return
		}
		log.Printf("[ERROR] Failed to update blackout: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to update blackout")
		return
	}

	// Return updated blackout
	updated, err := h.datastore.GetBlackout(r.Context(), id)
	if err != nil {
		log.Printf("[ERROR] Failed to fetch updated blackout: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch updated blackout")
		return
	}

	RespondJSON(w, http.StatusOK, updated)
}

// deleteBlackout handles DELETE /api/v1/blackouts/{id}
func (h *BlackoutHandler) deleteBlackout(w http.ResponseWriter, r *http.Request, id int64) {
	if !h.checkPermission(w, r) {
		return
	}

	if err := h.datastore.DeleteBlackout(r.Context(), id); err != nil {
		if errors.Is(err, database.ErrBlackoutNotFound) {
			RespondError(w, http.StatusNotFound, "Blackout not found")
			return
		}
		log.Printf("[ERROR] Failed to delete blackout: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to delete blackout")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// stopBlackout handles POST /api/v1/blackouts/{id}/stop
func (h *BlackoutHandler) stopBlackout(w http.ResponseWriter, r *http.Request, id int64) {
	if !h.checkPermission(w, r) {
		return
	}

	if err := h.datastore.StopBlackout(r.Context(), id); err != nil {
		if errors.Is(err, database.ErrBlackoutNotFound) {
			RespondError(w, http.StatusNotFound, "Blackout not found or not currently active")
			return
		}
		log.Printf("[ERROR] Failed to stop blackout: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to stop blackout")
		return
	}

	// Return the stopped blackout
	stopped, err := h.datastore.GetBlackout(r.Context(), id)
	if err != nil {
		log.Printf("[ERROR] Failed to fetch stopped blackout: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch stopped blackout")
		return
	}

	RespondJSON(w, http.StatusOK, stopped)
}

// listBlackoutSchedules handles GET /api/v1/blackout-schedules
func (h *BlackoutHandler) listBlackoutSchedules(w http.ResponseWriter, r *http.Request) {
	filter := database.BlackoutFilter{}

	if scope := ParseQueryString(r, "scope"); scope != "" {
		filter.Scope = &scope
	}
	if id, ok := ParseQueryIntSilent(r, "group_id"); ok {
		filter.GroupID = &id
	}
	if id, ok := ParseQueryIntSilent(r, "cluster_id"); ok {
		filter.ClusterID = &id
	}
	if id, ok := ParseQueryIntSilent(r, "connection_id"); ok {
		filter.ConnectionID = &id
	}
	if enabledStr := ParseQueryString(r, "enabled"); enabledStr != "" {
		enabled := enabledStr == "true" || enabledStr == "1"
		filter.Active = &enabled
	}

	filter.Limit = ParseLimitWithDefaults(r, 100, 1000)
	filter.Offset = ParseOffsetWithDefault(r, 0)

	result, err := h.datastore.ListBlackoutSchedules(r.Context(), filter)
	if err != nil {
		log.Printf("[ERROR] Failed to fetch blackout schedules: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch blackout schedules")
		return
	}

	RespondJSON(w, http.StatusOK, result)
}

// getBlackoutSchedule handles GET /api/v1/blackout-schedules/{id}
func (h *BlackoutHandler) getBlackoutSchedule(w http.ResponseWriter, r *http.Request, id int64) {
	schedule, err := h.datastore.GetBlackoutSchedule(r.Context(), id)
	if err != nil {
		if errors.Is(err, database.ErrBlackoutScheduleNotFound) {
			RespondError(w, http.StatusNotFound, "Blackout schedule not found")
			return
		}
		log.Printf("[ERROR] Failed to fetch blackout schedule: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch blackout schedule")
		return
	}

	RespondJSON(w, http.StatusOK, schedule)
}

// createBlackoutSchedule handles POST /api/v1/blackout-schedules
func (h *BlackoutHandler) createBlackoutSchedule(w http.ResponseWriter, r *http.Request) {
	if !h.checkPermission(w, r) {
		return
	}

	var req BlackoutScheduleRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Validate scope
	if err := validateBlackoutScope(req.Scope, req.GroupID, req.ClusterID, req.ConnectionID); err != nil {
		log.Printf("[ERROR] Request error: %v", err)
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate required fields
	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
		return
	}
	if req.CronExpression == "" {
		RespondError(w, http.StatusBadRequest, "Cron expression is required")
		return
	}
	if req.DurationMinutes <= 0 {
		RespondError(w, http.StatusBadRequest, "Duration minutes must be positive")
		return
	}

	timezone := req.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	// Get created_by from auth context
	username := auth.GetUsernameFromContext(r.Context())
	if username == "" {
		username = "unknown"
	}

	schedule := &database.BlackoutSchedule{
		Scope:           req.Scope,
		GroupID:         req.GroupID,
		ClusterID:       req.ClusterID,
		ConnectionID:    req.ConnectionID,
		DatabaseName:    req.DatabaseName,
		Name:            req.Name,
		CronExpression:  req.CronExpression,
		DurationMinutes: req.DurationMinutes,
		Timezone:        timezone,
		Reason:          req.Reason,
		Enabled:         enabled,
		CreatedBy:       username,
	}

	if err := h.datastore.CreateBlackoutSchedule(r.Context(), schedule); err != nil {
		log.Printf("[ERROR] Failed to create blackout schedule: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to create blackout schedule")
		return
	}

	RespondJSON(w, http.StatusCreated, schedule)
}

// updateBlackoutSchedule handles PUT /api/v1/blackout-schedules/{id}
func (h *BlackoutHandler) updateBlackoutSchedule(w http.ResponseWriter, r *http.Request, id int64) {
	if !h.checkPermission(w, r) {
		return
	}

	var req BlackoutScheduleRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Validate scope
	if err := validateBlackoutScope(req.Scope, req.GroupID, req.ClusterID, req.ConnectionID); err != nil {
		log.Printf("[ERROR] Request error: %v", err)
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate required fields
	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
		return
	}
	if req.CronExpression == "" {
		RespondError(w, http.StatusBadRequest, "Cron expression is required")
		return
	}
	if req.DurationMinutes <= 0 {
		RespondError(w, http.StatusBadRequest, "Duration minutes must be positive")
		return
	}

	timezone := req.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	schedule := &database.BlackoutSchedule{
		ID:              id,
		Scope:           req.Scope,
		GroupID:         req.GroupID,
		ClusterID:       req.ClusterID,
		ConnectionID:    req.ConnectionID,
		DatabaseName:    req.DatabaseName,
		Name:            req.Name,
		CronExpression:  req.CronExpression,
		DurationMinutes: req.DurationMinutes,
		Timezone:        timezone,
		Reason:          req.Reason,
		Enabled:         enabled,
	}

	if err := h.datastore.UpdateBlackoutSchedule(r.Context(), schedule); err != nil {
		if errors.Is(err, database.ErrBlackoutScheduleNotFound) {
			RespondError(w, http.StatusNotFound, "Blackout schedule not found")
			return
		}
		log.Printf("[ERROR] Failed to update blackout schedule: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to update blackout schedule")
		return
	}

	RespondJSON(w, http.StatusOK, schedule)
}

// deleteBlackoutSchedule handles DELETE /api/v1/blackout-schedules/{id}
func (h *BlackoutHandler) deleteBlackoutSchedule(w http.ResponseWriter, r *http.Request, id int64) {
	if !h.checkPermission(w, r) {
		return
	}

	if err := h.datastore.DeleteBlackoutSchedule(r.Context(), id); err != nil {
		if errors.Is(err, database.ErrBlackoutScheduleNotFound) {
			RespondError(w, http.StatusNotFound, "Blackout schedule not found")
			return
		}
		log.Printf("[ERROR] Failed to delete blackout schedule: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to delete blackout schedule")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
