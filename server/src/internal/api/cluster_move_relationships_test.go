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

// =============================================================================
// Regression tests for GitHub issue #707: moving a connection to another
// cluster left its relationship rows in the old cluster, so the old
// topology kept edges to a node that was no longer a member.
//
// The tests run against clusterCovSchema plus seedIssue678Relationships:
// connection 6 is in cluster 1 and named by rows 1 (5 -> 6), 2 (6 -> 7,
// whose target the restricted admin cannot see) and 3 (6 -> 8, in the
// hidden cluster 3). Row 4 (5 -> 7) does not name 6 and must survive.
// =============================================================================

// seedIssue707Relationships adds row 4 to the #678 seed.
func seedIssue707Relationships(t *testing.T, f *clusterCovFixture) {
	t.Helper()
	seedIssue678Relationships(t, f)
	if _, err := f.pool.Exec(context.Background(), `
        INSERT INTO cluster_node_relationships
            (id, cluster_id, source_connection_id, target_connection_id,
             relationship_type)
        VALUES (4, 1, 5, 7, 'streams_from')`); err != nil {
		t.Fatalf("Failed to seed relationship: %v", err)
	}
}

// expectRelationshipIDs checks which relationship rows remain.
func expectRelationshipIDs(t *testing.T, f *clusterCovFixture, want ...int) {
	t.Helper()
	rows, err := f.pool.Query(context.Background(),
		"SELECT id FROM cluster_node_relationships ORDER BY id")
	if err != nil {
		t.Fatalf("Query relationships: %v", err)
	}
	defer rows.Close()
	var got []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("Scan relationship: %v", err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Iterate relationships: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("relationship ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("relationship ids = %v, want %v", got, want)
		}
	}
}

// TestIssue707_UpdateConnectionClusterDropsOldRows covers PUT
// /api/v1/connections/{id}/cluster: the move deletes every row naming
// the connection outside its new cluster, including the row to a
// connection the restricted caller cannot see, and nothing else.
func TestIssue707_UpdateConnectionClusterDropsOldRows(t *testing.T) {
	f := newClusterCovFixture(t)
	seedIssue707Relationships(t, f)

	rec := httptest.NewRecorder()
	f.connectionHandlerOn(f.pool).handleUpdateConnectionCluster(rec,
		newScopeRequest(f.admin, http.MethodPut,
			`{"cluster_id":2,"membership_source":"manual"}`), 6)
	assertStatus(t, rec, http.StatusOK)
	f.expectValue(t, "conn cluster", 6, "2")
	expectRelationshipIDs(t, f, 4)
}

// TestIssue707_UnassignConnectionDropsRows covers pinning the
// connection to no cluster through the same route: every row naming it
// goes. (A null cluster with automatic membership only resets the
// membership source and leaves cluster_id alone.)
func TestIssue707_UnassignConnectionDropsRows(t *testing.T) {
	f := newClusterCovFixture(t)
	seedIssue707Relationships(t, f)

	rec := httptest.NewRecorder()
	f.connectionHandlerOn(f.pool).handleUpdateConnectionCluster(rec,
		newScopeRequest(f.super, http.MethodPut,
			`{"cluster_id":null,"membership_source":"manual"}`), 6)
	assertStatus(t, rec, http.StatusOK)
	f.expectValue(t, "conn cluster", 6, "none")
	expectRelationshipIDs(t, f, 4)
}

// TestIssue707_AddServerToClusterDropsOldRows covers POST
// /api/v1/clusters/{id}/servers, which moves a member of another
// cluster in the same way.
func TestIssue707_AddServerToClusterDropsOldRows(t *testing.T) {
	f := newClusterCovFixture(t)
	seedIssue707Relationships(t, f)

	rec := clusterCovServe(f.handler, f.admin, http.MethodPost,
		"/api/v1/clusters/2/servers", `{"connection_id":6}`)
	assertStatus(t, rec, http.StatusOK)
	f.expectValue(t, "conn cluster", 6, "2")
	expectRelationshipIDs(t, f, 4)
}

// TestIssue707_SameClusterKeepsRows covers a reassignment to the
// connection's current cluster, which must not touch its rows.
func TestIssue707_SameClusterKeepsRows(t *testing.T) {
	f := newClusterCovFixture(t)
	seedIssue707Relationships(t, f)

	rec := httptest.NewRecorder()
	f.connectionHandlerOn(f.pool).handleUpdateConnectionCluster(rec,
		newScopeRequest(f.super, http.MethodPut,
			`{"cluster_id":1,"membership_source":"manual"}`), 6)
	assertStatus(t, rec, http.StatusOK)
	// Row 3 is in cluster 3, not the connection's cluster 1, so it is
	// stale and goes; rows 1 and 2 in cluster 1 stay.
	expectRelationshipIDs(t, f, 1, 2, 4)
}
