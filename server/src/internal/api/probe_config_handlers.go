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
	// denialAuditor audits this handler's token-scope refusals.
	denialAuditor
}

// NewProbeConfigHandler creates a new probe config handler
func NewProbeConfigHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *ProbeConfigHandler {
	h := &ProbeConfigHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
	h.checkPermission = RequireAdminPermission(rbacChecker, auth.PermManageProbes, "manage probes")
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

// listProbeConfigs handles GET /api/v1/probe-configs. Without
// connection_id it lists the global defaults, which every caller may
// see; with it, the connection must be visible to the caller, and a
// hidden connection answers 404 exactly as a missing one does (issue
// #647).
func (h *ProbeConfigHandler) listProbeConfigs(w http.ResponseWriter, r *http.Request) {
	var connectionID *int
	if idStr := r.URL.Query().Get("connection_id"); idStr != "" {
		parsed, err := strconv.Atoi(idStr)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid connection_id parameter")
			return
		}
		if !scopeVisibleToCaller(r.Context(), w, h.rbacChecker, h.datastore,
			"server", parsed, "Connection not found") {
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

// getProbeConfig handles GET /api/v1/probe-configs/{id}. A config
// scoped to a server, cluster or group the caller cannot see answers
// 404 with the same message as a missing one (issue #647); a global
// config is visible to every caller.
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
	if scope, scopeID, scoped := probeConfigVisibilityTarget(config); scoped &&
		!scopeVisibleToCaller(r.Context(), w, h.rbacChecker, h.datastore,
			scope, scopeID, "Probe config not found") {
		return
	}

	RespondJSON(w, http.StatusOK, config)
}

// probeConfigVisibilityTarget returns the scope and scope ID that decide
// who may see a probe config, in the form scopeVisibleToCaller takes.
// scoped is false for a global config, which names no target.
func probeConfigVisibilityTarget(c *database.ProbeConfig) (scope string, scopeID int, scoped bool) {
	switch {
	case c.ConnectionID != nil:
		return "server", *c.ConnectionID, true
	case c.ClusterID != nil:
		return "cluster", *c.ClusterID, true
	case c.GroupID != nil:
		return "group", *c.GroupID, true
	}
	return "", 0, false
}

// updateProbeConfig handles PUT /api/v1/probe-configs/{id}
func (h *ProbeConfigHandler) updateProbeConfig(w http.ResponseWriter, r *http.Request, id int64) {
	if !h.checkPermission(w, r) {
		return
	}
	if !h.requireProbeConfigInTokenScope(w, r, id) {
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

// requireProbeConfigInTokenScope looks up a probe config and checks that
// the acting API token's connection scope covers it, answering 404 when
// it does not exist (issue #471). A config naming a connection needs
// that connection in scope at read_write; a global config applies to
// every connection without its own, so it needs a scope covering every
// connection. A caller whose token covers every connection needs no
// lookup, so sessions and unscoped tokens take the same path as before.
func (h *ProbeConfigHandler) requireProbeConfigInTokenScope(
	w http.ResponseWriter, r *http.Request, id int64) bool {

	if h.rbacChecker.AllConnectionsInTokenScope(r.Context()) {
		return true
	}
	existing, err := h.datastore.GetProbeConfig(r.Context(), id)
	if err != nil {
		if errors.Is(err, database.ErrProbeConfigNotFound) {
			RespondError(w, http.StatusNotFound, "Probe config not found")
			return false
		}
		log.Printf("[ERROR] Failed to fetch probe config: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch probe config")
		return false
	}
	scope := string(database.BlackoutScopeEstate)
	if existing.ConnectionID != nil {
		scope = string(database.BlackoutScopeServer)
	}
	return requireTargetInTokenScope(w, r, h.rbacChecker, h.denialRecorder,
		scope, existing.ConnectionID)
}
