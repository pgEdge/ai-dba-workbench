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
		mux.HandleFunc("/api/timeline/events", authWrapper(h.handleNotConfigured))
		return
	}

	mux.HandleFunc("/api/timeline/events", authWrapper(h.handleTimelineEvents))
}

// handleNotConfigured returns a 503 when the datastore is not configured
func (h *TimelineHandler) handleNotConfigured(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(ErrorResponse{Error: "Datastore not configured"})
}

// handleTimelineEvents handles GET /api/timeline/events
func (h *TimelineHandler) handleTimelineEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed"})
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	filter := database.TimelineFilter{}

	// Parse connection_id (single)
	if connID := query.Get("connection_id"); connID != "" {
		id, err := strconv.Atoi(connID)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid connection_id: " + err.Error()})
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
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid connection_ids: " + err.Error()})
				return
			}
			filter.ConnectionIDs = append(filter.ConnectionIDs, id)
		}
	}

	// Parse start_time (required)
	startTimeStr := query.Get("start_time")
	if startTimeStr == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "start_time is required"})
		return
	}
	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid start_time format, expected RFC3339: " + err.Error()})
		return
	}
	filter.StartTime = startTime

	// Parse end_time (required)
	endTimeStr := query.Get("end_time")
	if endTimeStr == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "end_time is required"})
		return
	}
	endTime, err := time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid end_time format, expected RFC3339: " + err.Error()})
		return
	}
	filter.EndTime = endTime

	// Validate time range
	if filter.EndTime.Before(filter.StartTime) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "end_time must be after start_time"})
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
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid event_type: " + trimmedType})
				return
			}
			filter.EventTypes = append(filter.EventTypes, trimmedType)
		}
	}

	// Parse limit (optional, default 500, max 1000)
	if limitStr := query.Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid limit: " + err.Error()})
			return
		}
		if limit <= 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "limit must be greater than 0"})
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to fetch timeline events: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
