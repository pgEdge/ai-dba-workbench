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
	"net/http"
	"strconv"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

func TestAlertOverrideHandler_RegisterRoutes(t *testing.T) {
	mux := http.NewServeMux()
	NewAlertOverrideHandler(nil, nil, nil).RegisterRoutes(mux, covIdentityWrapper)
	covExpect(t, covDo(mux.ServeHTTP, http.MethodGet, "/api/v1/alert-overrides/server/1", ""),
		http.StatusServiceUnavailable)

	f := newCovFixture(t)
	mux = http.NewServeMux()
	NewAlertOverrideHandler(f.ds, nil, newTestRBACChecker(t)).RegisterRoutes(mux, covIdentityWrapper)
	covExpect(t, covDo(mux.ServeHTTP, http.MethodGet,
		"/api/v1/alert-overrides/server/"+strconv.Itoa(f.connID), "", withSuperuser), http.StatusOK)
}

func TestAlertOverrideHandler_Coverage(t *testing.T) {
	f := newCovFixture(t)
	store := newTestAuthStore(t)
	checker := auth.NewRBACChecker(store)
	restricted := covRestrictedUser(t, store, auth.PermManageAlertRules)
	h := NewAlertOverrideHandler(f.ds, store, checker)
	broken := NewAlertOverrideHandler(newBrokenDatastore(t), store, checker)
	rule := strconv.FormatInt(f.ruleID, 10)

	runOverrideCoverage(t, f, h.handleAlertOverrides, broken.handleAlertOverrides, restricted, covOverrideSpec{
		prefix:      "/api/v1/alert-overrides/",
		item:        rule,
		badItem:     "x",
		okBody:      `{"operator": ">=", "threshold": 50, "severity": "critical", "enabled": true}`,
		invalidBody: `{"operator": "~", "threshold": 50, "severity": "critical", "enabled": true}`,
	})

	t.Run("context", func(t *testing.T) {
		base := "/api/v1/alert-overrides/context/"
		visible := base + strconv.Itoa(f.connID) + "/" + rule
		serve := h.handleAlertOverrides

		covExpect(t, covDo(serve, http.MethodPost, visible, "", withSuperuser), http.StatusMethodNotAllowed)
		covExpect(t, covDo(serve, http.MethodGet, base+"1", "", withSuperuser), http.StatusNotFound)
		covExpect(t, covDo(serve, http.MethodGet, base+"1/", "", withSuperuser), http.StatusNotFound)
		covExpect(t, covDo(serve, http.MethodGet, base+"x/1", "", withSuperuser), http.StatusBadRequest)
		covExpect(t, covDo(serve, http.MethodGet, base+"1/x", "", withSuperuser), http.StatusBadRequest)

		// Seed overrides at every scope so the context carries all three.
		for _, scoped := range []string{
			"server/" + strconv.Itoa(f.connID),
			"cluster/" + strconv.Itoa(f.clusterID),
			"group/" + strconv.Itoa(f.groupID),
		} {
			covExpect(t, covDo(serve, http.MethodPut, "/api/v1/alert-overrides/"+scoped+"/"+rule,
				`{"operator": ">", "threshold": 10, "severity": "info", "enabled": true}`, withSuperuser),
				http.StatusOK)
		}
		rec := covDo(serve, http.MethodGet, visible, "", withSuperuser)
		covExpect(t, rec, http.StatusOK)
		m := decodeRaw(t, rec.Body.Bytes())
		overrides, ok := m["overrides"].(map[string]any)
		if !ok || overrides["server"] == nil || overrides["cluster"] == nil || overrides["group"] == nil {
			t.Fatalf("expected overrides at all scopes, got %v", m["overrides"])
		}

		covExpect(t, covDo(serve, http.MethodGet, visible, "", restricted), http.StatusOK)
		covExpect(t, covDo(serve, http.MethodGet, base+strconv.Itoa(f.hiddenConnID)+"/"+rule, "", restricted),
			http.StatusNotFound)
		covExpect(t, covDo(serve, http.MethodGet, visible, "", func(r *http.Request) *http.Request {
			return withUser(r, 424242)
		}), http.StatusForbidden)
		covExpect(t, covDo(broken.handleAlertOverrides, http.MethodGet, visible, "", withSuperuser),
			http.StatusInternalServerError)
	})
}
