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

func TestProbeOverrideHandler_RegisterRoutes(t *testing.T) {
	mux := http.NewServeMux()
	NewProbeOverrideHandler(nil, nil, nil).RegisterRoutes(mux, covIdentityWrapper)
	covExpect(t, covDo(mux.ServeHTTP, http.MethodGet, "/api/v1/probe-overrides/server/1", ""),
		http.StatusServiceUnavailable)

	f := newCovFixture(t)
	mux = http.NewServeMux()
	NewProbeOverrideHandler(f.ds, nil, newTestRBACChecker(t)).RegisterRoutes(mux, covIdentityWrapper)
	covExpect(t, covDo(mux.ServeHTTP, http.MethodGet,
		"/api/v1/probe-overrides/server/"+strconv.Itoa(f.connID), "", withSuperuser), http.StatusOK)
}

func TestProbeOverrideHandler_Coverage(t *testing.T) {
	f := newCovFixture(t)
	store := newTestAuthStore(t)
	checker := auth.NewRBACChecker(store)
	restricted := covRestrictedUser(t, store, auth.PermManageProbes)
	h := NewProbeOverrideHandler(f.ds, store, checker)
	broken := NewProbeOverrideHandler(newBrokenDatastore(t), store, checker)

	runOverrideCoverage(t, f, h.handleProbeOverrides, broken.handleProbeOverrides, restricted, covOverrideSpec{
		prefix:      "/api/v1/probe-overrides/",
		item:        f.probeName,
		badItem:     "",
		okBody:      `{"is_enabled": true, "collection_interval_seconds": 30, "retention_days": 14}`,
		invalidBody: `{"is_enabled": true, "collection_interval_seconds": 0, "retention_days": 14}`,
	})
}
