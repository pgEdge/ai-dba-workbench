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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/database"
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

// TestRefuseDepartedMember covers the mapping of the membership
// sentinels to 400s, including the reverse write, whose source is the
// request's target.
func TestRefuseDepartedMember(t *testing.T) {
	const (
		source = "Source connection does not belong to this cluster"
		target = "Target connection does not belong to this cluster"
	)
	tests := []struct {
		name     string
		err      error
		reversed bool
		want     string
	}{
		{"source", database.ErrSourceNotInCluster, false, source},
		{"target", database.ErrTargetNotInCluster, false, target},
		{"reversed source", database.ErrSourceNotInCluster, true, target},
		{"reversed target", database.ErrTargetNotInCluster, true, source},
		{"wrapped", fmt.Errorf("x: %w", database.ErrTargetNotInCluster), false, target},
		{"other error", fmt.Errorf("boom"), false, ""},
		{"nil", nil, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			got := refuseDepartedMember(rec, tt.err, tt.reversed)
			if got != (tt.want != "") {
				t.Fatalf("refuseDepartedMember = %v", got)
			}
			if tt.want == "" {
				if rec.Body.Len() != 0 {
					t.Errorf("unexpected response %q", rec.Body.String())
				}
				return
			}
			assertStatus(t, rec, http.StatusBadRequest)
			if !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.want)
			}
		})
	}
}

// TestIssue707_RelationshipWriteRacingMove lands a move of connection 6
// from cluster 1 to cluster 4 before each connection acquisition of a
// relationship PUT in cluster 1 that names 6, as the source and as the
// target of a replicates_with pair. Whichever statement the move lands
// before, no row naming 6 may remain outside cluster 4. Before the
// write transactions rechecked membership under a lock, a move landing
// at acquisition 3 (the forward write's transaction) left the row that
// write inserted, and one landing before the reverse insert left the
// reverse row, with the PUT answering 200 each time.
func TestIssue707_RelationshipWriteRacingMove(t *testing.T) {
	f := newClusterCovFixture(t)
	ctx := context.Background()
	mover := database.NewTestDatastore(f.pool)
	cluster4 := 4

	move := func() {
		if err := mover.AssignConnectionToCluster(ctx, 6, &cluster4, nil,
			"manual"); err != nil {
			t.Errorf("Failed to move connection 6: %v", err)
		}
	}
	reset := func() {
		if _, err := f.pool.Exec(ctx, `
            DELETE FROM cluster_node_relationships;
            UPDATE connections SET cluster_id = 1, membership_source = 'auto'
            WHERE id = 6`); err != nil {
			t.Fatalf("Failed to reset the fixture: %v", err)
		}
	}
	staleRows := func() int {
		var n int
		if err := f.pool.QueryRow(ctx, `
            SELECT count(*) FROM cluster_node_relationships
            WHERE (source_connection_id = 6 OR target_connection_id = 6)
              AND cluster_id <> 4`).Scan(&n); err != nil {
			t.Fatalf("Count stale rows: %v", err)
		}
		return n
	}

	for _, req := range []struct{ name, path, body string }{
		{"source 6", "/api/v1/clusters/1/connections/6/relationships",
			`{"relationships":[{"target_connection_id":5,` +
				`"relationship_type":"streams_from"}]}`},
		{"replicates_with target 6",
			"/api/v1/clusters/1/connections/5/relationships",
			`{"relationships":[{"target_connection_id":6,` +
				`"relationship_type":"replicates_with"}]}`},
	} {
		t.Run(req.name, func(t *testing.T) {
			for at := int64(1); ; at++ {
				reset()
				h, fired := f.racingClusterHandler(t, at, move)
				rec := clusterCovServe(h, f.super, http.MethodPut, req.path,
					req.body)
				if !fired.Load() {
					if at <= 3 {
						t.Fatalf("only %d acquisitions", at-1)
					}
					break
				}
				label := fmt.Sprintf("move at %d (status %d: %s)", at,
					rec.Code, rec.Body.String())
				if rec.Code != http.StatusOK && rec.Code != http.StatusBadRequest {
					t.Errorf("%s: unexpected status", label)
				}
				if n := staleRows(); n != 0 {
					t.Errorf("%s: %d rows naming 6 remain outside cluster 4",
						label, n)
				}
				if got := f.readBack(t, "conn cluster", 6); got != "4" {
					t.Errorf("%s: connection 6 is in cluster %s", label, got)
				}
			}
		})
	}
}
