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

// TestGetConnectionIDsForScope_MissingID_Integration pins the contract
// the API scope-visibility check relies on: a cluster or group ID that
// does not exist yields an empty list and no error. An error here would
// answer a missing scope with 500 whilst a hidden one answers 404,
// revealing which IDs exist (issue #672).
func TestGetConnectionIDsForScope_MissingID_Integration(t *testing.T) {
	ds, pool, cleanup := newClusterQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	var groupID, clusterID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO cluster_groups (name) VALUES ('Scope Group') RETURNING id`,
	).Scan(&groupID); err != nil {
		t.Fatalf("Insert group: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO clusters (group_id, name) VALUES ($1, 'Scope Cluster') RETURNING id`,
		groupID,
	).Scan(&clusterID); err != nil {
		t.Fatalf("Insert cluster: %v", err)
	}
	connID := insertClusterQueriesConnection(t, pool, "scope-member", &clusterID, nil)

	const missingID = 987654
	tests := []struct {
		name   string
		lookup func(context.Context, int) ([]int, error)
		id     int
		want   []int
	}{
		{"cluster present", ds.GetConnectionIDsForCluster, clusterID, []int{connID}},
		{"cluster missing", ds.GetConnectionIDsForCluster, missingID, nil},
		{"group present", ds.GetConnectionIDsForGroup, groupID, []int{connID}},
		{"group missing", ds.GetConnectionIDsForGroup, missingID, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.lookup(ctx, tt.id)
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}
			if len(got) != len(tt.want) || (len(got) > 0 && !reflect.DeepEqual(got, tt.want)) {
				t.Errorf("Got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGetAllConnectionIDs_Integration verifies that every connection is
// listed except a deleted one, which an estate-wide write must not wait
// on, and that a failed query is reported (issue #732).
func TestGetAllConnectionIDs_Integration(t *testing.T) {
	ds, pool, cleanup := newClusterQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	first := insertClusterQueriesConnection(t, pool, "all-first", nil, nil)
	second := insertClusterQueriesConnection(t, pool, "all-second", nil, nil)
	deleted := insertClusterQueriesConnection(t, pool, "all-deleted", nil, nil)
	if _, err := pool.Exec(ctx, `UPDATE connections SET deleted_at = NOW() WHERE id = $1`, deleted); err != nil {
		t.Fatalf("Delete connection: %v", err)
	}

	got, err := ds.GetAllConnectionIDs(ctx)
	if err != nil {
		t.Fatalf("GetAllConnectionIDs: %v", err)
	}
	if want := []int{first, second}; !reflect.DeepEqual(got, want) {
		t.Errorf("Got %v, want %v", got, want)
	}

	stopped, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetAllConnectionIDs(stopped); err == nil {
		t.Error("Expected an error from a stopped query")
	}
}
