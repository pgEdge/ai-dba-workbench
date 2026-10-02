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
)

func TestAlertRuleHandler_RegisterRoutesNotConfigured(t *testing.T) {
	h := NewAlertRuleHandler(nil, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, covIdentityWrapper)
	for _, p := range []string{"/api/v1/alert-rules", "/api/v1/alert-rules/1"} {
		covExpect(t, covDo(mux.ServeHTTP, http.MethodGet, p, ""), http.StatusServiceUnavailable)
	}
}

func TestAlertRuleHandler_Coverage(t *testing.T) {
	f := newCovFixture(t)
	h := NewAlertRuleHandler(f.ds, nil, newTestRBACChecker(t))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, covIdentityWrapper)
	covAssertRouteRegistered(t, mux, "/api/v1/alert-rules")
	serve := mux.ServeHTTP
	ruleURL := "/api/v1/alert-rules/" + strconv.FormatInt(f.ruleID, 10)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"list", http.MethodGet, "/api/v1/alert-rules", "", http.StatusOK},
		{"list wrong method", http.MethodPost, "/api/v1/alert-rules", "", http.StatusMethodNotAllowed},
		{"subpath empty", http.MethodGet, "/api/v1/alert-rules/", "", http.StatusNotFound},
		{"subpath bad id", http.MethodGet, "/api/v1/alert-rules/abc", "", http.StatusBadRequest},
		{"subpath extra part", http.MethodGet, ruleURL + "/extra", "", http.StatusNotFound},
		{"subpath wrong method", http.MethodDelete, ruleURL, "", http.StatusMethodNotAllowed},
		{"get", http.MethodGet, ruleURL, "", http.StatusOK},
		{"get missing", http.MethodGet, "/api/v1/alert-rules/999999", "", http.StatusNotFound},
		{"update", http.MethodPut, ruleURL, `{"default_threshold": 75, "default_severity": "critical"}`, http.StatusOK},
		{"update bad json", http.MethodPut, ruleURL, `{`, http.StatusBadRequest},
		{"update invalid operator", http.MethodPut, ruleURL, `{"default_operator": "~"}`, http.StatusBadRequest},
		{"update missing", http.MethodPut, "/api/v1/alert-rules/999999", `{"default_threshold": 1}`, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			covExpect(t, covDo(serve, tc.method, tc.path, tc.body, withSuperuser), tc.want)
		})
	}

	t.Run("update denied without permission", func(t *testing.T) {
		covExpect(t, covDo(serve, http.MethodPut, ruleURL, `{}`, func(r *http.Request) *http.Request {
			return withUser(r, 42)
		}), http.StatusForbidden)
	})
}

func TestAlertRuleHandler_DatastoreErrors(t *testing.T) {
	h := NewAlertRuleHandler(newBrokenDatastore(t), nil, newTestRBACChecker(t))
	covExpect(t, covDo(h.handleAlertRules, http.MethodGet, "/api/v1/alert-rules", "", withSuperuser),
		http.StatusInternalServerError)
	covExpect(t, covDo(h.handleAlertRuleSubpath, http.MethodGet, "/api/v1/alert-rules/1", "", withSuperuser),
		http.StatusInternalServerError)
}
