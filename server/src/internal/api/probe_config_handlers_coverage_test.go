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
	"context"
	"net/http"
	"strconv"
	"testing"
)

func TestProbeConfigHandler_RegisterRoutesNotConfigured(t *testing.T) {
	h := NewProbeConfigHandler(nil, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, covIdentityWrapper)
	for _, p := range []string{"/api/v1/probe-configs", "/api/v1/probe-configs/1"} {
		covExpect(t, covDo(mux.ServeHTTP, http.MethodGet, p, ""), http.StatusServiceUnavailable)
	}
}

func TestProbeConfigHandler_Coverage(t *testing.T) {
	f := newCovFixture(t)
	var probeID int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT id FROM probe_configs WHERE name = $1`, f.probeName).Scan(&probeID); err != nil {
		t.Fatalf("lookup probe id: %v", err)
	}
	h := NewProbeConfigHandler(f.ds, nil, newTestRBACChecker(t))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, covIdentityWrapper)
	serve := mux.ServeHTTP
	probeURL := "/api/v1/probe-configs/" + strconv.FormatInt(probeID, 10)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"list global", http.MethodGet, "/api/v1/probe-configs", "", http.StatusOK},
		{"list for connection", http.MethodGet, "/api/v1/probe-configs?connection_id=" + strconv.Itoa(f.connID), "", http.StatusOK},
		{"list bad connection id", http.MethodGet, "/api/v1/probe-configs?connection_id=x", "", http.StatusBadRequest},
		{"list wrong method", http.MethodPost, "/api/v1/probe-configs", "", http.StatusMethodNotAllowed},
		{"subpath empty", http.MethodGet, "/api/v1/probe-configs/", "", http.StatusNotFound},
		{"subpath bad id", http.MethodGet, "/api/v1/probe-configs/abc", "", http.StatusBadRequest},
		{"subpath wrong method", http.MethodDelete, probeURL, "", http.StatusMethodNotAllowed},
		{"get", http.MethodGet, probeURL, "", http.StatusOK},
		{"get missing", http.MethodGet, "/api/v1/probe-configs/999999", "", http.StatusNotFound},
		{"update", http.MethodPut, probeURL, `{"is_enabled": false, "collection_interval_seconds": 120, "retention_days": 7}`, http.StatusOK},
		{"update bad json", http.MethodPut, probeURL, `{`, http.StatusBadRequest},
		{"update invalid interval", http.MethodPut, probeURL, `{"collection_interval_seconds": 0}`, http.StatusBadRequest},
		{"update missing", http.MethodPut, "/api/v1/probe-configs/999999", `{"retention_days": 3}`, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			covExpect(t, covDo(serve, tc.method, tc.path, tc.body, withSuperuser), tc.want)
		})
	}

	t.Run("update denied without permission", func(t *testing.T) {
		covExpect(t, covDo(serve, http.MethodPut, probeURL, `{}`, func(r *http.Request) *http.Request {
			return withUser(r, 42)
		}), http.StatusForbidden)
	})
}

func TestProbeConfigHandler_DatastoreErrors(t *testing.T) {
	h := NewProbeConfigHandler(newBrokenDatastore(t), nil, newTestRBACChecker(t))
	covExpect(t, covDo(h.handleProbeConfigs, http.MethodGet, "/api/v1/probe-configs", "", withSuperuser),
		http.StatusInternalServerError)
	covExpect(t, covDo(h.handleProbeConfigSubpath, http.MethodGet, "/api/v1/probe-configs/1", "", withSuperuser),
		http.StatusInternalServerError)
}
