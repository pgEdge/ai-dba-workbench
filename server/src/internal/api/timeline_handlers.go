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

// TimelineHandler handles REST API requests for timeline events
type TimelineHandler struct {
	datastore   *database.Datastore
	authStore   *auth.AuthStore
	rbacChecker *auth.RBACChecker
}

// NewTimelineHandler creates a new timeline handler. The rbacChecker is
// used to filter events to the connections the caller is allowed to
// see.
func NewTimelineHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *TimelineHandler {
	return &TimelineHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
}

// RegisterRoutes registers timeline routes on the mux
func (h *TimelineHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	// Check if datastore is configured
	if h.datastore == nil {
		mux.HandleFunc("/api/v1/timeline/events", authWrapper(HandleNotConfigured("Timeline")))
		return
	}

	mux.HandleFunc("/api/v1/timeline/events", authWrapper(h.handleTimelineEvents))
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

	// RBAC: restrict to visible connections. VisibleConnectionIDs
	// returns allConnections=true for superusers and wildcard token
	// scopes; otherwise it returns the explicit set of visible IDs.
	if h.rbacChecker != nil {
		lister := newConnectionVisibilityLister(h.datastore)
		accessibleIDs, allConnections, err := h.rbacChecker.VisibleConnectionIDs(r.Context(), lister)
		if err != nil {
			log.Printf("[ERROR] Failed to resolve visible connections for timeline: %v", err)
			RespondError(w, http.StatusInternalServerError, "Failed to fetch timeline events")
			return
		}
		if !allConnections {
			// Empty visible set -> return an empty result without
			// hitting the datastore.
			if len(accessibleIDs) == 0 {
				RespondJSON(w, http.StatusOK, &database.TimelineResult{Events: []database.TimelineEvent{}, TotalCount: 0})
				return
			}
			accessibleSet := make(map[int]bool, len(accessibleIDs))
			for _, id := range accessibleIDs {
				accessibleSet[id] = true
			}
			if filter.ConnectionID != nil {
				if !accessibleSet[*filter.ConnectionID] {
					RespondJSON(w, http.StatusOK, &database.TimelineResult{Events: []database.TimelineEvent{}, TotalCount: 0})
					return
				}
			}
			if len(filter.ConnectionIDs) > 0 {
				intersected := make([]int, 0, len(filter.ConnectionIDs))
				for _, id := range filter.ConnectionIDs {
					if accessibleSet[id] {
						intersected = append(intersected, id)
					}
				}
				if len(intersected) == 0 {
					RespondJSON(w, http.StatusOK, &database.TimelineResult{Events: []database.TimelineEvent{}, TotalCount: 0})
					return
				}
				filter.ConnectionIDs = intersected
			} else if filter.ConnectionID == nil {
				// No user filter -- restrict to visible connections.
				filter.ConnectionIDs = accessibleIDs
			}
		}
	}

	// Fetch timeline events
	result, err := h.datastore.GetTimelineEvents(r.Context(), filter)
	if err != nil {
		log.Printf("[ERROR] Failed to fetch timeline events: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch timeline events")
		return
	}

	RespondJSON(w, http.StatusOK, result)
}
