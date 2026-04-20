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

// AlertOverrideHandler handles REST API requests for alert threshold overrides
type AlertOverrideHandler struct {
	datastore       *database.Datastore
	authStore       *auth.AuthStore
	rbacChecker     *auth.RBACChecker
	checkPermission func(http.ResponseWriter, *http.Request) bool
}

// NewAlertOverrideHandler creates a new alert override handler
func NewAlertOverrideHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *AlertOverrideHandler {
	h := &AlertOverrideHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
	if rbacChecker != nil {
		h.checkPermission = RequireAdminPermission(rbacChecker, auth.PermManageAlertRules, "manage alert rules")
	}
	return h
}

// RegisterRoutes registers alert override management routes on the mux
func (h *AlertOverrideHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	if h.datastore == nil {
		mux.HandleFunc("/api/v1/alert-overrides/", authWrapper(HandleNotConfigured("Alert override management")))
		return
	}

	mux.HandleFunc("/api/v1/alert-overrides/", authWrapper(h.handleAlertOverrides))
}

// handleAlertOverrides routes requests to the appropriate handler based on the URL path.
// URL patterns:
//
//	GET    /api/v1/alert-overrides/{scope}/{scopeId}          - list overrides
//	PUT    /api/v1/alert-overrides/{scope}/{scopeId}/{ruleId}  - upsert override
//	DELETE /api/v1/alert-overrides/{scope}/{scopeId}/{ruleId}  - delete override
func (h *AlertOverrideHandler) handleAlertOverrides(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/alert-overrides/")
	parts := strings.Split(path, "/")

	// Check for context endpoint first:
	// GET /api/v1/alert-overrides/context/{connectionId}/{ruleId}
	if len(parts) >= 1 && parts[0] == "context" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
			http.NotFound(w, r)
			return
		}
		connectionID, err := strconv.Atoi(parts[1])
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid connection ID")
			return
		}
		ruleID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid rule ID")
			return
		}
		h.getOverrideContext(w, r, connectionID, ruleID)
		return
	}

	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}

	scope := parts[0] // "server", "cluster", or "group"
	if scope != "server" && scope != "cluster" && scope != "group" {
		RespondError(w, http.StatusBadRequest, "Invalid scope: must be server, cluster, or group")
		return
	}

	scopeID, err := strconv.Atoi(parts[1])
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid scope ID")
		return
	}

	// GET /api/v1/alert-overrides/{scope}/{scopeId}
	if len(parts) == 2 {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.listOverrides(w, r, scope, scopeID)
		return
	}

	// PUT/DELETE /api/v1/alert-overrides/{scope}/{scopeId}/{ruleId}
	if len(parts) == 3 {
		ruleID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid rule ID")
			return
		}

		switch r.Method {
		case http.MethodPut:
			h.upsertOverride(w, r, scope, scopeID, ruleID)
		case http.MethodDelete:
			h.deleteOverride(w, r, scope, scopeID, ruleID)
		default:
			w.Header().Set("Allow", "PUT, DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.NotFound(w, r)
}

// listOverrides handles GET /api/v1/alert-overrides/{scope}/{scopeId}
func (h *AlertOverrideHandler) listOverrides(w http.ResponseWriter, r *http.Request, scope string, scopeID int) {
	if !scopeVisibleToCaller(r.Context(), w, h.rbacChecker, h.datastore, scope, scopeID, "Alert override scope not found") {
		return
	}

	var overrides []database.AlertOverride
	var err error

	switch scope {
	case "server":
		overrides, err = h.datastore.GetAlertOverridesForServer(r.Context(), scopeID)
	case "cluster":
		overrides, err = h.datastore.GetAlertOverridesForCluster(r.Context(), scopeID)
	case "group":
		overrides, err = h.datastore.GetAlertOverridesForGroup(r.Context(), scopeID)
	}

	if err != nil {
		log.Printf("[ERROR] Failed to fetch alert overrides: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch alert overrides")
		return
	}

	RespondJSON(w, http.StatusOK, overrides)
}

// upsertOverride handles PUT /api/v1/alert-overrides/{scope}/{scopeId}/{ruleId}
func (h *AlertOverrideHandler) upsertOverride(w http.ResponseWriter, r *http.Request, scope string, scopeID int, ruleID int64) {
	if !h.checkPermission(w, r) {
		return
	}

	var req database.AlertThresholdUpdate
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	err := h.datastore.UpsertAlertThreshold(r.Context(), scope, scopeID, ruleID, req)
	if err != nil {
		log.Printf("[ERROR] Request error: %v", err)
		RespondError(w, http.StatusBadRequest, "Request failed")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// deleteOverride handles DELETE /api/v1/alert-overrides/{scope}/{scopeId}/{ruleId}
func (h *AlertOverrideHandler) deleteOverride(w http.ResponseWriter, r *http.Request, scope string, scopeID int, ruleID int64) {
	if !h.checkPermission(w, r) {
		return
	}

	err := h.datastore.DeleteAlertThreshold(r.Context(), scope, scopeID, ruleID)
	if err != nil {
		log.Printf("[ERROR] Failed to delete alert override: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to delete alert override")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// getOverrideContext handles GET /api/v1/alert-overrides/context/{connectionId}/{ruleId}
func (h *AlertOverrideHandler) getOverrideContext(w http.ResponseWriter, r *http.Request, connectionID int, ruleID int64) {
	if !h.checkPermission(w, r) {
		return
	}

	// Gate by scope visibility so a caller cannot enumerate the
	// override-context hierarchy for a connection they cannot see.
	// scopeVisibleToCaller writes a 404 and logs when the caller is denied.
	if !scopeVisibleToCaller(r.Context(), w, h.rbacChecker, h.datastore, "server", connectionID, "Connection not found") {
		return
	}

	ctx, err := h.datastore.GetOverrideContext(r.Context(), connectionID, ruleID)
	if err != nil {
		log.Printf("[ERROR] Failed to get override context: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to get override context")
		return
	}
	RespondJSON(w, http.StatusOK, ctx)
}
