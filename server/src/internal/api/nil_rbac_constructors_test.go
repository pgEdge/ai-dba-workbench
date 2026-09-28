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
	"net/http/httptest"
	"testing"
)

// TestAdminHandlers_NilRBAC_Forbidden checks that each admin handler
// built with no RBAC checker still installs its permission gate, and that
// the gate refuses a superuser with 403. Before issue #561 a nil checker
// left the gate unset, so the handler panicked on its first request.
func TestAdminHandlers_NilRBAC_Forbidden(t *testing.T) {
	gates := map[string]func(http.ResponseWriter, *http.Request) bool{
		"alert overrides":       NewAlertOverrideHandler(nil, nil, nil).checkPermission,
		"alert rules":           NewAlertRuleHandler(nil, nil, nil).checkPermission,
		"blackouts":             NewBlackoutHandler(nil, nil, nil).checkPermission,
		"channel overrides":     NewChannelOverrideHandler(nil, nil, nil).checkPermission,
		"notification channels": NewNotificationChannelHandlerWithSecurity(nil, nil, nil, false, nil, nil).checkPermission,
		"probe configs":         NewProbeConfigHandler(nil, nil, nil).checkPermission,
		"probe overrides":       NewProbeOverrideHandler(nil, nil, nil).checkPermission,
	}
	for name, gate := range gates {
		t.Run(name, func(t *testing.T) {
			if gate == nil {
				t.Fatal("permission gate not installed")
			}
			rec := httptest.NewRecorder()
			req := withSuperuser(httptest.NewRequest(http.MethodPut, "/", nil))
			if gate(rec, req) {
				t.Fatal("gate admitted a caller despite a nil checker")
			}
			if rec.Code != http.StatusForbidden {
				t.Errorf("expected status 403, got %d", rec.Code)
			}
		})
	}
}
