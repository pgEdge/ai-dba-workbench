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
)

// TestResolveVisibleConnectionSet_NilRBAC_ReturnsAllConnections verifies
// the defense-in-depth nil guard added after CodeRabbit's follow-up
// review on issue #35. NewBlackoutHandler and NewClusterHandler both
// accept a nil rbacChecker (the blackout and cluster handler tests
// construct handlers that way), so resolveVisibleConnectionSet must
// treat a nil checker as unrestricted visibility rather than panicking
// on the method call. The ds argument is also nil here to prove the
// guard returns before touching the datastore.
func TestResolveVisibleConnectionSet_NilRBAC_ReturnsAllConnections(t *testing.T) {
	visible, all, err := resolveVisibleConnectionSet(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("resolveVisibleConnectionSet with nil rbac: %v", err)
	}
	if !all {
		t.Error("Expected allConnections=true when rbacChecker is nil")
	}
	if visible != nil {
		t.Errorf("Expected nil visible map when allConnections=true, got %v", visible)
	}
}

// TestScopeVisibleToCaller_InvalidScope_RejectsUnrestrictedCaller verifies
// that scope validation runs before the allConnections shortcut. An
// unrestricted caller (nil rbac, thanks to the guard added for #35) that
// passes a garbage scope must receive 400, matching the behavior
// restricted callers already saw via the switch default. This closes the
// inconsistency CodeRabbit flagged on the recurring #35 follow-up.
func TestScopeVisibleToCaller_InvalidScope_RejectsUnrestrictedCaller(t *testing.T) {
	rec := httptest.NewRecorder()
	ok := scopeVisibleToCaller(context.Background(), rec, nil, nil, "garbage", 1, "not found")
	if ok {
		t.Fatal("Expected scopeVisibleToCaller to return false for invalid scope")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid scope, got %d", rec.Code)
	}
}
