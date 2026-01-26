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
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// TimelineHandler handles REST API requests for timeline events
type TimelineHandler struct {
	datastore *database.Datastore
	authStore *auth.AuthStore
}

// NewTimelineHandler creates a new timeline handler
func NewTimelineHandler(datastore *database.Datastore, authStore *auth.AuthStore) *TimelineHandler {
	return &TimelineHandler{
		datastore: datastore,
		authStore: authStore,
	}
}

// RegisterRoutes registers timeline routes on the mux
func (h *TimelineHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	// Check if datastore is configured
	if h.datastore == nil {
		mux.HandleFunc("/api/v1/timeline/events", authWrapper(h.handleNotConfigured))
		return
	}

	mux.HandleFunc("/api/v1/timeline/events", authWrapper(h.handleTimelineEvents))
}

// handleNotConfigured returns a 503 when the datastore is not configured
func (h *TimelineHandler) handleNotConfigured(w http.ResponseWriter, r *http.Request) {
	RespondError(w, http.StatusServiceUnavailable, "Datastore not configured")
}

// handleTimelineEvents handles GET /api/v1/timeline/events
func (h *TimelineHandler) handleTimelineEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	filter := database.TimelineFilter{}

	// Parse connection_id (single)
	if connID := query.Get("connection_id"); connID != "" {
		id, err := strconv.Atoi(connID)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid connection_id: "+err.Error())
			return
		}
		filter.ConnectionID = &id
	}

	// Parse connection_ids (multiple, comma-separated)
	if connIDs := query.Get("connection_ids"); connIDs != "" {
		ids := strings.Split(connIDs, ",")
		for _, idStr := range ids {
			id, err := strconv.Atoi(strings.TrimSpace(idStr))
			if err != nil {
				RespondError(w, http.StatusBadRequest, "Invalid connection_ids: "+err.Error())
				return
			}
			filter.ConnectionIDs = append(filter.ConnectionIDs, id)
		}
	}

	// Parse start_time (required)
	startTimeStr := query.Get("start_time")
	if startTimeStr == "" {
		RespondError(w, http.StatusBadRequest, "start_time is required")
		return
	}
	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid start_time format, expected RFC3339: "+err.Error())
		return
	}
	filter.StartTime = startTime

	// Parse end_time (required)
	endTimeStr := query.Get("end_time")
	if endTimeStr == "" {
		RespondError(w, http.StatusBadRequest, "end_time is required")
		return
	}
	endTime, err := time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid end_time format, expected RFC3339: "+err.Error())
		return
	}
	filter.EndTime = endTime

	// Validate time range
	if filter.EndTime.Before(filter.StartTime) {
		RespondError(w, http.StatusBadRequest, "end_time must be after start_time")
		return
	}

	// Parse event_types (optional, comma-separated)
	if eventTypes := query.Get("event_types"); eventTypes != "" {
		types := strings.Split(eventTypes, ",")
		validTypes := map[string]bool{
			database.EventTypeConfigChange:      true,
			database.EventTypeHBAChange:         true,
			database.EventTypeIdentChange:       true,
			database.EventTypeRestart:           true,
			database.EventTypeAlertFired:        true,
			database.EventTypeAlertCleared:      true,
			database.EventTypeAlertAcknowledged: true,
			database.EventTypeExtensionChange:   true,
		}
		for _, t := range types {
			trimmedType := strings.TrimSpace(t)
			if !validTypes[trimmedType] {
				RespondError(w, http.StatusBadRequest, "Invalid event_type: "+trimmedType)
				return
			}
			filter.EventTypes = append(filter.EventTypes, trimmedType)
		}
	}

	// Parse limit (optional, default 500, max 1000)
	if limitStr := query.Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid limit: "+err.Error())
			return
		}
		if limit <= 0 {
			RespondError(w, http.StatusBadRequest, "limit must be greater than 0")
			return
		}
		if limit > 1000 {
			limit = 1000
		}
		filter.Limit = limit
	}
	if filter.Limit == 0 {
		filter.Limit = 500
	}

	// Fetch timeline events
	result, err := h.datastore.GetTimelineEvents(r.Context(), filter)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to fetch timeline events: "+err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, result)
}
