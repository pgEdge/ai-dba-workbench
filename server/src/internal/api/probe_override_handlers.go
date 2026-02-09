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

// ProbeOverrideHandler handles REST API requests for probe config overrides
type ProbeOverrideHandler struct {
	datastore       *database.Datastore
	authStore       *auth.AuthStore
	rbacChecker     *auth.RBACChecker
	checkPermission func(http.ResponseWriter, *http.Request) bool
}

// NewProbeOverrideHandler creates a new probe override handler
func NewProbeOverrideHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *ProbeOverrideHandler {
	h := &ProbeOverrideHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
	if rbacChecker != nil {
		h.checkPermission = RequireAdminPermission(rbacChecker, auth.PermManageProbes, "manage probes")
	}
	return h
}

// RegisterRoutes registers probe override management routes on the mux
func (h *ProbeOverrideHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	if h.datastore == nil {
		mux.HandleFunc("/api/v1/probe-overrides/", authWrapper(HandleNotConfigured("Probe override management")))
		return
	}

	mux.HandleFunc("/api/v1/probe-overrides/", authWrapper(h.handleProbeOverrides))
}

// handleProbeOverrides routes requests to the appropriate handler based on the URL path.
// URL patterns:
//
//	GET    /api/v1/probe-overrides/{scope}/{scopeId}              - list overrides
//	PUT    /api/v1/probe-overrides/{scope}/{scopeId}/{probeName}  - upsert override
//	DELETE /api/v1/probe-overrides/{scope}/{scopeId}/{probeName}  - delete override
func (h *ProbeOverrideHandler) handleProbeOverrides(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/probe-overrides/")
	parts := strings.Split(path, "/")

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

	// GET /api/v1/probe-overrides/{scope}/{scopeId}
	if len(parts) == 2 {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.listOverrides(w, r, scope, scopeID)
		return
	}

	// PUT/DELETE /api/v1/probe-overrides/{scope}/{scopeId}/{probeName}
	if len(parts) == 3 {
		probeName := parts[2]
		if probeName == "" {
			RespondError(w, http.StatusBadRequest, "Invalid probe name")
			return
		}

		switch r.Method {
		case http.MethodPut:
			h.upsertOverride(w, r, scope, scopeID, probeName)
		case http.MethodDelete:
			h.deleteOverride(w, r, scope, scopeID, probeName)
		default:
			w.Header().Set("Allow", "PUT, DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.NotFound(w, r)
}

// listOverrides handles GET /api/v1/probe-overrides/{scope}/{scopeId}
func (h *ProbeOverrideHandler) listOverrides(w http.ResponseWriter, r *http.Request, scope string, scopeID int) {
	var overrides []database.ProbeOverride
	var err error

	switch scope {
	case "server":
		overrides, err = h.datastore.GetProbeOverridesForServer(r.Context(), scopeID)
	case "cluster":
		overrides, err = h.datastore.GetProbeOverridesForCluster(r.Context(), scopeID)
	case "group":
		overrides, err = h.datastore.GetProbeOverridesForGroup(r.Context(), scopeID)
	}

	if err != nil {
		log.Printf("[ERROR] Failed to fetch probe overrides: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch probe overrides")
		return
	}

	RespondJSON(w, http.StatusOK, overrides)
}

// upsertOverride handles PUT /api/v1/probe-overrides/{scope}/{scopeId}/{probeName}
func (h *ProbeOverrideHandler) upsertOverride(w http.ResponseWriter, r *http.Request, scope string, scopeID int, probeName string) {
	if !h.checkPermission(w, r) {
		return
	}

	var req database.ProbeOverrideUpdate
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	err := h.datastore.UpsertProbeOverride(r.Context(), scope, scopeID, probeName, req)
	if err != nil {
		log.Printf("[ERROR] Request error: %v", err)
		RespondError(w, http.StatusBadRequest, "Request failed")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// deleteOverride handles DELETE /api/v1/probe-overrides/{scope}/{scopeId}/{probeName}
func (h *ProbeOverrideHandler) deleteOverride(w http.ResponseWriter, r *http.Request, scope string, scopeID int, probeName string) {
	if !h.checkPermission(w, r) {
		return
	}

	err := h.datastore.DeleteProbeOverride(r.Context(), scope, scopeID, probeName)
	if err != nil {
		log.Printf("[ERROR] Failed to delete probe override: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to delete probe override")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
