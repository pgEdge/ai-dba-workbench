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
	"net/http/httptest"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// TestResolveVisibleConnectionSet_NilRBAC_ReturnsNoConnections verifies
// that a nil checker fails closed: the caller sees an empty set, never
// unrestricted visibility (issue #561). The ds argument is also nil to
// prove no datastore lookup is needed to reach that answer.
func TestResolveVisibleConnectionSet_NilRBAC_ReturnsNoConnections(t *testing.T) {
	ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, true)
	visible, all, err := resolveVisibleConnectionSet(ctx, nil, nil)
	if err != nil {
		t.Fatalf("resolveVisibleConnectionSet with nil rbac: %v", err)
	}
	if all {
		t.Error("Expected allConnections=false when rbacChecker is nil")
	}
	if len(visible) != 0 {
		t.Errorf("Expected an empty visible set when rbacChecker is nil, got %v", visible)
	}
}

// TestScopeVisibleToCaller_NilRBAC_NotFound verifies that a nil checker
// hides a server scope with the same 404 a restricted caller receives.
func TestScopeVisibleToCaller_NilRBAC_NotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, true)
	if scopeVisibleToCaller(ctx, rec, nil, nil, "server", 1, "not found") {
		t.Fatal("Expected scopeVisibleToCaller to deny a nil checker")
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404 for a nil checker, got %d", rec.Code)
	}
}

// TestScopeVisibleToCaller_InvalidScope_RejectsUnrestrictedCaller verifies
// that scope validation runs before the allConnections shortcut. An
// unrestricted caller (a superuser on a real checker) that passes a
// garbage scope must receive 400, matching the behavior restricted
// callers already saw via the switch default. This closes the
// inconsistency CodeRabbit flagged on the recurring #35 follow-up.
func TestScopeVisibleToCaller_InvalidScope_RejectsUnrestrictedCaller(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, true)
	ok := scopeVisibleToCaller(ctx, rec, newTestRBACChecker(t), nil, "garbage", 1, "not found")
	if ok {
		t.Fatal("Expected scopeVisibleToCaller to return false for invalid scope")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid scope, got %d", rec.Code)
	}
}
