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
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// ProbeConfigHandler handles REST API requests for probe configuration management
type ProbeConfigHandler struct {
	datastore   *database.Datastore
	authStore   *auth.AuthStore
	rbacChecker *auth.RBACChecker
}

// NewProbeConfigHandler creates a new probe config handler
func NewProbeConfigHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *ProbeConfigHandler {
	return &ProbeConfigHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
}

// RegisterRoutes registers probe config management routes on the mux
func (h *ProbeConfigHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	if h.datastore == nil {
		mux.HandleFunc("/api/v1/probe-configs", authWrapper(h.handleNotConfigured))
		mux.HandleFunc("/api/v1/probe-configs/", authWrapper(h.handleNotConfigured))
		return
	}

	mux.HandleFunc("/api/v1/probe-configs", authWrapper(h.handleProbeConfigs))
	mux.HandleFunc("/api/v1/probe-configs/", authWrapper(h.handleProbeConfigSubpath))
}

// handleNotConfigured returns a 503 when the datastore is not configured
func (h *ProbeConfigHandler) handleNotConfigured(w http.ResponseWriter, r *http.Request) {
	RespondError(w, http.StatusServiceUnavailable,
		"Probe configuration is not available. The datastore is not configured.")
}

// requireProbePermission checks that the user has manage_probes permission
func (h *ProbeConfigHandler) requireProbePermission(w http.ResponseWriter, r *http.Request) bool {
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageProbes) {
		RespondError(w, http.StatusForbidden,
			"Permission denied: you do not have permission to manage probes")
		return false
	}
	return true
}

// handleProbeConfigs handles GET /api/v1/probe-configs
func (h *ProbeConfigHandler) handleProbeConfigs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listProbeConfigs(w, r)
	default:
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleProbeConfigSubpath handles /api/v1/probe-configs/{id}
func (h *ProbeConfigHandler) handleProbeConfigSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/probe-configs/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.Atoi(path)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid probe config ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getProbeConfig(w, r, id)
	case http.MethodPut:
		h.updateProbeConfig(w, r, id)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listProbeConfigs handles GET /api/v1/probe-configs
func (h *ProbeConfigHandler) listProbeConfigs(w http.ResponseWriter, r *http.Request) {
	var connectionID *int
	if idStr := r.URL.Query().Get("connection_id"); idStr != "" {
		parsed, err := strconv.Atoi(idStr)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid connection_id parameter")
			return
		}
		connectionID = &parsed
	}

	configs, err := h.datastore.GetProbeConfigs(r.Context(), connectionID)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to fetch probe configs: "+err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, configs)
}

// getProbeConfig handles GET /api/v1/probe-configs/{id}
func (h *ProbeConfigHandler) getProbeConfig(w http.ResponseWriter, r *http.Request, id int) {
	config, err := h.datastore.GetProbeConfig(r.Context(), id)
	if err != nil {
		if errors.Is(err, database.ErrProbeConfigNotFound) {
			RespondError(w, http.StatusNotFound, "Probe config not found")
			return
		}
		RespondError(w, http.StatusInternalServerError, "Failed to fetch probe config: "+err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, config)
}

// updateProbeConfig handles PUT /api/v1/probe-configs/{id}
func (h *ProbeConfigHandler) updateProbeConfig(w http.ResponseWriter, r *http.Request, id int) {
	if !h.requireProbePermission(w, r) {
		return
	}

	var req database.ProbeConfigUpdate
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	updated, err := h.datastore.UpdateProbeConfig(r.Context(), id, req)
	if err != nil {
		if errors.Is(err, database.ErrProbeConfigNotFound) {
			RespondError(w, http.StatusNotFound, "Probe config not found")
			return
		}
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, updated)
}
