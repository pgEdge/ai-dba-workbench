/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"reflect"
	"testing"
)

// TestGetConnectionIDsForAutoClusterKey covers the lookup of the
// connections assigned to an auto-detected cluster by its key.
func TestGetConnectionIDsForAutoClusterKey(t *testing.T) {
	ds, pool, cleanup := newClusterQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	mustExecClusterQueries(t, pool, `
        INSERT INTO clusters (id, auto_cluster_key, name)
        VALUES (1, 'spock:alpha', 'Alpha'), (2, 'spock:beta', 'Beta')`)
	one, two := 1, 2
	second := insertClusterQueriesConnection(t, pool, "a2", &one, nil)
	first := insertClusterQueriesConnection(t, pool, "a1", &one, nil)
	insertClusterQueriesConnection(t, pool, "b1", &two, nil)
	insertClusterQueriesConnection(t, pool, "none", nil, nil)

	got, err := ds.GetConnectionIDsForAutoClusterKey(ctx, "spock:alpha")
	if err != nil {
		t.Fatalf("GetConnectionIDsForAutoClusterKey: %v", err)
	}
	if want := []int{second, first}; !reflect.DeepEqual(got, want) {
		t.Errorf("members = %v, want %v", got, want)
	}

	got, err = ds.GetConnectionIDsForAutoClusterKey(ctx, "spock:missing")
	if err != nil {
		t.Fatalf("GetConnectionIDsForAutoClusterKey missing: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("members of a missing key = %v, want none", got)
	}

	mustExecClusterQueries(t, pool, "DROP TABLE connections CASCADE")
	if _, err := ds.GetConnectionIDsForAutoClusterKey(ctx, "spock:alpha"); err == nil {
		t.Error("Expected an error once the connections table is gone")
	}
}
