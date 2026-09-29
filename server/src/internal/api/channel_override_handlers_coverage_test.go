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

func TestChannelOverrideHandler_RegisterRoutes(t *testing.T) {
	mux := http.NewServeMux()
	NewChannelOverrideHandler(nil, nil, nil).RegisterRoutes(mux, covIdentityWrapper)
	covExpect(t, covDo(mux.ServeHTTP, http.MethodGet, "/api/v1/channel-overrides/server/1", ""),
		http.StatusServiceUnavailable)

	f := newCovFixture(t)
	mux = http.NewServeMux()
	NewChannelOverrideHandler(f.ds, nil, newTestRBACChecker(t)).RegisterRoutes(mux, covIdentityWrapper)
	covExpect(t, covDo(mux.ServeHTTP, http.MethodGet,
		"/api/v1/channel-overrides/server/"+strconv.Itoa(f.connID), "", withSuperuser), http.StatusOK)
}

func TestChannelOverrideHandler_Coverage(t *testing.T) {
	f := newCovFixture(t)
	store := newTestAuthStore(t)
	checker := auth.NewRBACChecker(store)
	restricted := covRestrictedUser(t, store, auth.PermManageNotificationChannels)
	h := NewChannelOverrideHandler(f.ds, store, checker)
	broken := NewChannelOverrideHandler(newBrokenDatastore(t), store, checker)

	runOverrideCoverage(t, f, h.handleChannelOverrides, broken.handleChannelOverrides, restricted, covOverrideSpec{
		prefix:  "/api/v1/channel-overrides/",
		item:    strconv.FormatInt(f.channelID, 10),
		badItem: "x",
		okBody:  `{"enabled": false}`,
	})

	// An override for a channel that does not exist violates the
	// foreign key, which the handler reports as a 400.
	covExpect(t, covDo(h.handleChannelOverrides, http.MethodPut,
		"/api/v1/channel-overrides/server/"+strconv.Itoa(f.connID)+"/999999", `{"enabled": true}`, withSuperuser),
		http.StatusBadRequest)
}
