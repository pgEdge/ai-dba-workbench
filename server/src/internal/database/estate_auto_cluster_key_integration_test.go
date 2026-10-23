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

// TestGetClusterGroupIDForAutoClusterKey covers the lookup of an
// auto-detected cluster's group by its key.
func TestGetClusterGroupIDForAutoClusterKey(t *testing.T) {
	ds, pool, cleanup := newClusterQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	mustExecClusterQueries(t, pool, `
        INSERT INTO cluster_groups (id, name) VALUES (7, 'Seven');
        INSERT INTO clusters (id, auto_cluster_key, name, group_id)
        VALUES (1, 'spock:grouped', 'Grouped', 7),
               (2, 'spock:loose', 'Loose', NULL)`)

	tests := []struct {
		key       string
		wantID    int
		wantFound bool
	}{
		{"spock:grouped", 7, true},
		{"spock:loose", 0, false},
		{"spock:missing", 0, false},
	}
	for _, tt := range tests {
		id, found, err := ds.GetClusterGroupIDForAutoClusterKey(ctx, tt.key)
		if err != nil {
			t.Fatalf("GetClusterGroupIDForAutoClusterKey(%q): %v", tt.key, err)
		}
		if id != tt.wantID || found != tt.wantFound {
			t.Errorf("GetClusterGroupIDForAutoClusterKey(%q) = %d, %v, want %d, %v",
				tt.key, id, found, tt.wantID, tt.wantFound)
		}
	}

	mustExecClusterQueries(t, pool, "DROP TABLE clusters CASCADE")
	if _, _, err := ds.GetClusterGroupIDForAutoClusterKey(ctx, "spock:grouped"); err == nil {
		t.Error("Expected an error once the clusters table is gone")
	}
}
