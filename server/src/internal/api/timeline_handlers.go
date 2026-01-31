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
	if !RequireGET(w, r) {
		return
	}

	filter := database.TimelineFilter{}

	// Parse connection_id (single)
	if id, ok := ParseQueryInt(w, r, "connection_id"); ok {
		filter.ConnectionID = &id
	} else if r.URL.Query().Get("connection_id") != "" {
		return // Error already sent
	}

	// Parse connection_ids (multiple, comma-separated)
	if ids, ok := ParseQueryIntList(w, r, "connection_ids"); ok {
		filter.ConnectionIDs = ids
	} else if r.URL.Query().Get("connection_ids") != "" {
		return // Error already sent
	}

	// Parse start_time (required)
	startTime, ok := RequireQueryTime(w, r, "start_time")
	if !ok {
		return
	}
	filter.StartTime = startTime

	// Parse end_time (required)
	endTime, ok := RequireQueryTime(w, r, "end_time")
	if !ok {
		return
	}
	filter.EndTime = endTime

	// Validate time range
	if !ValidateTimeRange(w, filter.StartTime, filter.EndTime) {
		return
	}

	// Parse event_types (optional, comma-separated)
	if eventTypes, ok := ParseQueryStringList(r, "event_types"); ok {
		validTypes := map[string]bool{
			database.EventTypeConfigChange:      true,
			database.EventTypeHBAChange:         true,
			database.EventTypeIdentChange:       true,
			database.EventTypeRestart:           true,
			database.EventTypeAlertFired:        true,
			database.EventTypeAlertCleared:      true,
			database.EventTypeAlertAcknowledged: true,
			database.EventTypeExtensionChange:   true,
			database.EventTypeBlackoutStarted:   true,
			database.EventTypeBlackoutEnded:     true,
		}
		if !ValidateStringsInSet(w, eventTypes, "event_type", validTypes) {
			return
		}
		filter.EventTypes = eventTypes
	}

	// Parse limit (optional, default 500, max 1000)
	filter.Limit = ParseLimitWithDefaults(r, 500, 1000)

	// Fetch timeline events
	result, err := h.datastore.GetTimelineEvents(r.Context(), filter)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to fetch timeline events: "+err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, result)
}
