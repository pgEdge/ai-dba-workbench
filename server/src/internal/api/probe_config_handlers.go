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
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// ProbeConfigHandler handles REST API requests for probe configuration management
type ProbeConfigHandler struct {
	datastore       *database.Datastore
	authStore       *auth.AuthStore
	rbacChecker     *auth.RBACChecker
	checkPermission func(http.ResponseWriter, *http.Request) bool
}

// NewProbeConfigHandler creates a new probe config handler
func NewProbeConfigHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *ProbeConfigHandler {
	h := &ProbeConfigHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
	if rbacChecker != nil {
		h.checkPermission = RequireAdminPermission(rbacChecker, auth.PermManageProbes, "manage probes")
	}
	return h
}

// RegisterRoutes registers probe config management routes on the mux
func (h *ProbeConfigHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	if h.datastore == nil {
		notConfigured := HandleNotConfigured("Probe configuration")
		mux.HandleFunc("/api/v1/probe-configs", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/probe-configs/", authWrapper(notConfigured))
		return
	}

	mux.HandleFunc("/api/v1/probe-configs", authWrapper(h.handleProbeConfigs))
	mux.HandleFunc("/api/v1/probe-configs/", authWrapper(h.handleProbeConfigSubpath))
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

	id, err := strconv.ParseInt(path, 10, 64)
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
		log.Printf("[ERROR] Failed to fetch probe configs: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch probe configs")
		return
	}

	RespondJSON(w, http.StatusOK, configs)
}

// getProbeConfig handles GET /api/v1/probe-configs/{id}
func (h *ProbeConfigHandler) getProbeConfig(w http.ResponseWriter, r *http.Request, id int64) {
	config, err := h.datastore.GetProbeConfig(r.Context(), id)
	if err != nil {
		if errors.Is(err, database.ErrProbeConfigNotFound) {
			RespondError(w, http.StatusNotFound, "Probe config not found")
			return
		}
		log.Printf("[ERROR] Failed to fetch probe config: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch probe config")
		return
	}

	RespondJSON(w, http.StatusOK, config)
}

// updateProbeConfig handles PUT /api/v1/probe-configs/{id}
func (h *ProbeConfigHandler) updateProbeConfig(w http.ResponseWriter, r *http.Request, id int64) {
	if !h.checkPermission(w, r) {
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
		log.Printf("[ERROR] Request error: %v", err)
		RespondError(w, http.StatusBadRequest, "Request failed")
		return
	}

	RespondJSON(w, http.StatusOK, updated)
}
