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
