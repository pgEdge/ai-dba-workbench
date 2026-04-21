/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"testing"
)

// ---------------------------------------------------------------------------
// newDatastoreVisibilityLister tests
// ---------------------------------------------------------------------------

func TestNewDatastoreVisibilityListerNilDatastore(t *testing.T) {
	// A nil datastore should return a nil lister
	lister := newDatastoreVisibilityLister(nil)
	if lister != nil {
		t.Errorf("expected nil lister for nil datastore, got %v", lister)
	}
}

// Note: Testing with a non-nil datastore requires a real database connection.
// The integration tests cover that scenario.
// The important behavior here is that nil datastore returns nil lister,
// which allows VisibleConnectionIDs to fall back gracefully.
