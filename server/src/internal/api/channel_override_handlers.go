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
	"strconv"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// ChannelOverrideHandler handles REST API requests for notification channel overrides
type ChannelOverrideHandler struct {
	datastore   *database.Datastore
	authStore   *auth.AuthStore
	rbacChecker *auth.RBACChecker
}

// NewChannelOverrideHandler creates a new channel override handler
func NewChannelOverrideHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *ChannelOverrideHandler {
	return &ChannelOverrideHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
}

// RegisterRoutes registers channel override management routes on the mux
func (h *ChannelOverrideHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	if h.datastore == nil {
		mux.HandleFunc("/api/v1/channel-overrides/", authWrapper(h.handleNotConfigured))
		return
	}

	mux.HandleFunc("/api/v1/channel-overrides/", authWrapper(h.handleChannelOverrides))
}

// handleNotConfigured returns a 503 when the datastore is not configured
func (h *ChannelOverrideHandler) handleNotConfigured(w http.ResponseWriter, r *http.Request) {
	RespondError(w, http.StatusServiceUnavailable,
		"Channel override management is not available. The datastore is not configured.")
}

// requirePermission checks that the user has manage_notification_channels permission
func (h *ChannelOverrideHandler) requirePermission(w http.ResponseWriter, r *http.Request) bool {
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageNotificationChannels) {
		RespondError(w, http.StatusForbidden,
			"Permission denied: you do not have permission to manage notification channels")
		return false
	}
	return true
}

// handleChannelOverrides routes requests based on URL path.
// URL patterns:
//
//	GET    /api/v1/channel-overrides/{scope}/{scopeId}              - list overrides
//	PUT    /api/v1/channel-overrides/{scope}/{scopeId}/{channelId}  - upsert override
//	DELETE /api/v1/channel-overrides/{scope}/{scopeId}/{channelId}  - delete override
func (h *ChannelOverrideHandler) handleChannelOverrides(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/channel-overrides/")
	parts := strings.Split(path, "/")

	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}

	scope := parts[0]
	if scope != "server" && scope != "cluster" && scope != "group" {
		RespondError(w, http.StatusBadRequest, "Invalid scope: must be server, cluster, or group")
		return
	}

	scopeID, err := strconv.Atoi(parts[1])
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid scope ID")
		return
	}

	// GET /api/v1/channel-overrides/{scope}/{scopeId}
	if len(parts) == 2 {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.listOverrides(w, r, scope, scopeID)
		return
	}

	// PUT/DELETE /api/v1/channel-overrides/{scope}/{scopeId}/{channelId}
	if len(parts) == 3 {
		channelID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid channel ID")
			return
		}

		switch r.Method {
		case http.MethodPut:
			h.upsertOverride(w, r, scope, scopeID, channelID)
		case http.MethodDelete:
			h.deleteOverride(w, r, scope, scopeID, channelID)
		default:
			w.Header().Set("Allow", "PUT, DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.NotFound(w, r)
}

// listOverrides handles GET /api/v1/channel-overrides/{scope}/{scopeId}
func (h *ChannelOverrideHandler) listOverrides(w http.ResponseWriter, r *http.Request, scope string, scopeID int) {
	var overrides []database.ChannelOverride
	var err error

	switch scope {
	case "server":
		overrides, err = h.datastore.GetChannelOverridesForServer(r.Context(), scopeID)
	case "cluster":
		overrides, err = h.datastore.GetChannelOverridesForCluster(r.Context(), scopeID)
	case "group":
		overrides, err = h.datastore.GetChannelOverridesForGroup(r.Context(), scopeID)
	}

	if err != nil {
		log.Printf("[ERROR] Failed to fetch channel overrides: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch channel overrides")
		return
	}

	RespondJSON(w, http.StatusOK, overrides)
}

// upsertOverride handles PUT /api/v1/channel-overrides/{scope}/{scopeId}/{channelId}
func (h *ChannelOverrideHandler) upsertOverride(w http.ResponseWriter, r *http.Request, scope string, scopeID int, channelID int64) {
	if !h.requirePermission(w, r) {
		return
	}

	var req database.ChannelOverrideUpdate
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	err := h.datastore.UpsertChannelOverride(r.Context(), scope, scopeID, channelID, req)
	if err != nil {
		log.Printf("[ERROR] Request error: %v", err)
		RespondError(w, http.StatusBadRequest, "Request failed")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// deleteOverride handles DELETE /api/v1/channel-overrides/{scope}/{scopeId}/{channelId}
func (h *ChannelOverrideHandler) deleteOverride(w http.ResponseWriter, r *http.Request, scope string, scopeID int, channelID int64) {
	if !h.requirePermission(w, r) {
		return
	}

	err := h.datastore.DeleteChannelOverride(r.Context(), scope, scopeID, channelID)
	if err != nil {
		log.Printf("[ERROR] Failed to delete channel override: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to delete channel override")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
