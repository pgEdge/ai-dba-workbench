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


// AlertRuleHandler handles REST API requests for alert rule management
type AlertRuleHandler struct {
	datastore   *database.Datastore
	authStore   *auth.AuthStore
	rbacChecker *auth.RBACChecker
}

// NewAlertRuleHandler creates a new alert rule handler
func NewAlertRuleHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *AlertRuleHandler {
	return &AlertRuleHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
}

// RegisterRoutes registers alert rule management routes on the mux
func (h *AlertRuleHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	if h.datastore == nil {
		mux.HandleFunc("/api/v1/alert-rules", authWrapper(h.handleNotConfigured))
		mux.HandleFunc("/api/v1/alert-rules/", authWrapper(h.handleNotConfigured))
		return
	}

	mux.HandleFunc("/api/v1/alert-rules", authWrapper(h.handleAlertRules))
	mux.HandleFunc("/api/v1/alert-rules/", authWrapper(h.handleAlertRuleSubpath))
}

// handleNotConfigured returns a 503 when the datastore is not configured
func (h *AlertRuleHandler) handleNotConfigured(w http.ResponseWriter, r *http.Request) {
	RespondError(w, http.StatusServiceUnavailable,
		"Alert rule management is not available. The datastore is not configured.")
}

// requireAlertRulePermission checks that the user has manage_alert_rules permission
func (h *AlertRuleHandler) requireAlertRulePermission(w http.ResponseWriter, r *http.Request) bool {
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageAlertRules) {
		RespondError(w, http.StatusForbidden,
			"Permission denied: you do not have permission to manage alert rules")
		return false
	}
	return true
}

// handleAlertRules handles GET /api/v1/alert-rules
func (h *AlertRuleHandler) handleAlertRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listAlertRules(w, r)
	default:
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAlertRuleSubpath handles /api/v1/alert-rules/{id}
func (h *AlertRuleHandler) handleAlertRuleSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/alert-rules/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid alert rule ID")
		return
	}

	// Handle /api/v1/alert-rules/{id}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.getAlertRule(w, r, id)
		case http.MethodPut:
			h.updateAlertRule(w, r, id)
		default:
			w.Header().Set("Allow", "GET, PUT")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.NotFound(w, r)
}

// listAlertRules handles GET /api/v1/alert-rules
func (h *AlertRuleHandler) listAlertRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.datastore.GetAlertRules(r.Context())
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to fetch alert rules: "+err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, rules)
}

// getAlertRule handles GET /api/v1/alert-rules/{id}
func (h *AlertRuleHandler) getAlertRule(w http.ResponseWriter, r *http.Request, id int64) {
	rule, err := h.datastore.GetAlertRule(r.Context(), id)
	if err != nil {
		if errors.Is(err, database.ErrAlertRuleNotFound) {
			RespondError(w, http.StatusNotFound, "Alert rule not found")
			return
		}
		RespondError(w, http.StatusInternalServerError, "Failed to fetch alert rule: "+err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, rule)
}

// updateAlertRule handles PUT /api/v1/alert-rules/{id}
func (h *AlertRuleHandler) updateAlertRule(w http.ResponseWriter, r *http.Request, id int64) {
	if !h.requireAlertRulePermission(w, r) {
		return
	}

	var req database.AlertRuleUpdate
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	updated, err := h.datastore.UpdateAlertRule(r.Context(), id, req)
	if err != nil {
		if errors.Is(err, database.ErrAlertRuleNotFound) {
			RespondError(w, http.StatusNotFound, "Alert rule not found")
			return
		}
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, updated)
}

