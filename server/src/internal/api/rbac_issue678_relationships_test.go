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
// Regression tests for GitHub issue #678: the cluster relationship routes
// checked only the manage_connections admin permission (writes) or
// nothing at all (the read), so a caller could list, rewrite or delete
// relationships between connections they cannot see.
//
// The tests run against clusterCovSchema, in which the restricted admin
// caller (f.admin) sees connections 5, 6 and 9 but not 7 or 8. Each test
// adds a manual relationship 6 -> 7 (id 2), whose target is hidden, to
// the seeded visible relationship 5 -> 6 (id 1).
// =============================================================================

// seedIssue678Relationships adds the hidden-target relationship 6 -> 7
// (id 2), and a stale relationship 6 -> 8 (id 3) left in the hidden
// cluster 3, as happens when a connection moves to another cluster.
func seedIssue678Relationships(t *testing.T, f *clusterCovFixture) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
        INSERT INTO cluster_node_relationships
            (id, cluster_id, source_connection_id, target_connection_id,
             relationship_type)
        VALUES (2, 1, 6, 7, 'streams_from'),
               (3, 3, 6, 8, 'streams_from')`); err != nil {
		t.Fatalf("Failed to seed relationship: %v", err)
	}
}

// TestIssue678_GetRelationships covers GET
// /api/v1/clusters/{id}/relationships for a restricted caller.
func TestIssue678_GetRelationships(t *testing.T) {
	f := newClusterCovFixture(t)
	seedIssue678Relationships(t, f)

	f.run(t, []clusterCovCase{
		{name: "visibility fails", h: f.failing(t, 0), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters/1/relationships",
			status: http.StatusInternalServerError,
			msg:    "Failed to get cluster relationships"},
		{name: "membership fails", h: f.failing(t, 1), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters/1/relationships",
			status: http.StatusInternalServerError,
			msg:    "Failed to get cluster relationships"},
		{name: "read fails", h: f.failing(t, 2), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters/1/relationships",
			status: http.StatusInternalServerError,
			msg:    "Failed to get cluster relationships"},
		{name: "hidden cluster", caller: f.admin, method: http.MethodGet,
			path: "/api/v1/clusters/3/relationships", status: http.StatusNotFound,
			msg: "Cluster not found"},
		{name: "caller who sees nothing", caller: f.incomplete,
			method: http.MethodGet, path: "/api/v1/clusters/1/relationships",
			status: http.StatusNotFound},
		{name: "superuser sees every relationship", caller: f.super,
			method: http.MethodGet, path: "/api/v1/clusters/1/relationships",
			status: http.StatusOK, msg: "seven"},
	})

	rec := clusterCovServe(f.handler, f.admin, http.MethodGet,
		"/api/v1/clusters/1/relationships", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"id":1`) {
		t.Errorf("Expected the visible relationship 1, got %s", body)
	}
	if strings.Contains(body, "seven") || strings.Contains(body, `"id":2`) {
		t.Errorf("Response discloses the hidden relationship: %s", body)
	}
}

// TestIssue678_SetRelationships covers PUT
// /api/v1/clusters/{id}/connections/{connId}/relationships for a
// restricted caller.
func TestIssue678_SetRelationships(t *testing.T) {
	f := newClusterCovFixture(t)
	seedIssue678Relationships(t, f)

	const toSix = `{"relationships":[{"target_connection_id":6,` +
		`"relationship_type":"replicates_with"}]}`
	const toSeven = `{"relationships":[{"target_connection_id":7,` +
		`"relationship_type":"streams_from"}]}`
	const toFive = `{"relationships":[{"target_connection_id":5,` +
		`"relationship_type":"streams_from"}]}`

	f.run(t, []clusterCovCase{
		{name: "visibility fails", h: f.failing(t, 0), caller: f.admin,
			method: http.MethodPut,
			path:   "/api/v1/clusters/1/connections/5/relationships",
			body:   toSix, status: http.StatusInternalServerError,
			msg: "Failed to validate relationships"},
		{name: "membership check fails", h: f.failing(t, 1),
			caller: f.admin, method: http.MethodPut,
			path: "/api/v1/clusters/1/connections/5/relationships",
			body: toSix, status: http.StatusInternalServerError,
			msg: "Failed to validate relationships"},
		{name: "existing relationships read fails", h: f.failing(t, 2),
			caller: f.admin, method: http.MethodPut,
			path: "/api/v1/clusters/1/connections/5/relationships",
			body: toSix, status: http.StatusInternalServerError,
			msg: "Failed to validate relationships"},
		{name: "stale row in a hidden cluster is not disclosed",
			caller: f.admin, method: http.MethodPut,
			path: "/api/v1/clusters/3/connections/6/relationships",
			body: toFive, status: http.StatusBadRequest,
			msg: "Source connection does not belong to this cluster"},
		{name: "hidden source", caller: f.admin, method: http.MethodPut,
			path: "/api/v1/clusters/1/connections/7/relationships",
			body: toFive, status: http.StatusNotFound,
			msg: "Connection not found"},
		{name: "hidden source skips decode", caller: f.admin,
			method: http.MethodPut,
			path:   "/api/v1/clusters/1/connections/7/relationships",
			body:   `{bad`, status: http.StatusNotFound},
		{name: "rewrite would delete a hidden relationship", caller: f.admin,
			method: http.MethodPut,
			path:   "/api/v1/clusters/1/connections/6/relationships",
			body:   toFive, status: http.StatusForbidden,
			msg: "relationships to connections you cannot access"},
		{name: "hidden target", caller: f.admin, method: http.MethodPut,
			path: "/api/v1/clusters/1/connections/5/relationships",
			body: toSeven, status: http.StatusBadRequest,
			msg: "Target connection does not belong to this cluster"},
	})

	// The hidden relationship survives the refused rewrite.
	f.expectValue(t, "relationship", 2, "true")

	// A permitted rewrite answers with visible relationships only, and
	// the reverse replicates_with row is added to 6 without deleting
	// 6's relationship to the hidden connection 7.
	rec := clusterCovServe(f.handler, f.admin, http.MethodPut,
		"/api/v1/clusters/1/connections/5/relationships", toSix)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "replicates_with") {
		t.Errorf("Expected the new relationship, got %s", body)
	}
	if strings.Contains(body, "seven") {
		t.Errorf("Response discloses the hidden relationship: %s", body)
	}
	f.expectValue(t, "relationship", 2, "true")
	f.expectValue(t, "relationship pair", 6, "3")
}

// TestIssue678_ClearRelationships covers DELETE
// /api/v1/clusters/{id}/connections/{connId}/relationships for a
// restricted caller.
func TestIssue678_ClearRelationships(t *testing.T) {
	f := newClusterCovFixture(t)
	seedIssue678Relationships(t, f)

	f.run(t, []clusterCovCase{
		{name: "visibility fails", h: f.failing(t, 0), caller: f.admin,
			method: http.MethodDelete,
			path:   "/api/v1/clusters/1/connections/5/relationships",
			status: http.StatusInternalServerError,
			msg:    "Failed to clear relationships"},
		{name: "membership check fails", h: f.failing(t, 1),
			caller: f.admin, method: http.MethodDelete,
			path:   "/api/v1/clusters/1/connections/5/relationships",
			status: http.StatusInternalServerError,
			msg:    "Failed to validate relationships"},
		{name: "existing relationships read fails", h: f.failing(t, 2),
			caller: f.admin, method: http.MethodDelete,
			path:   "/api/v1/clusters/1/connections/5/relationships",
			status: http.StatusInternalServerError,
			msg:    "Failed to validate relationships"},
		{name: "stale row in a hidden cluster is not touched",
			caller: f.admin, method: http.MethodDelete,
			path:   "/api/v1/clusters/3/connections/6/relationships",
			status: http.StatusBadRequest,
			msg:    "Source connection does not belong to this cluster"},
		{name: "hidden source", caller: f.admin, method: http.MethodDelete,
			path:   "/api/v1/clusters/1/connections/7/relationships",
			status: http.StatusNotFound, msg: "Connection not found"},
		{name: "clear would delete a hidden relationship", caller: f.admin,
			method: http.MethodDelete,
			path:   "/api/v1/clusters/1/connections/6/relationships",
			status: http.StatusForbidden,
			msg:    "relationships to connections you cannot access"},
		{name: "visible relationships cleared", caller: f.admin,
			method: http.MethodDelete,
			path:   "/api/v1/clusters/1/connections/5/relationships",
			status: http.StatusNoContent},
	})

	f.expectValue(t, "relationship", 1, "false")
	f.expectValue(t, "relationship", 2, "true")
	f.expectValue(t, "relationship", 3, "true")

	// An unrestricted caller keeps the previous behavior and clears the
	// relationship to the hidden connection too.
	f.run(t, []clusterCovCase{
		{name: "superuser clears every relationship", caller: f.super,
			method: http.MethodDelete,
			path:   "/api/v1/clusters/1/connections/6/relationships",
			status: http.StatusNoContent},
	})
	f.expectValue(t, "relationship", 2, "false")
}

// TestIssue678_DeleteRelationship covers DELETE
// /api/v1/clusters/{id}/relationships/{relationshipId} for a restricted
// caller.
func TestIssue678_DeleteRelationship(t *testing.T) {
	f := newClusterCovFixture(t)
	seedIssue678Relationships(t, f)

	f.run(t, []clusterCovCase{
		{name: "visibility fails", h: f.failing(t, 0), caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/clusters/1/relationships/1",
			status: http.StatusInternalServerError,
			msg:    "Failed to delete relationship"},
		{name: "relationship read fails", h: f.failing(t, 1), caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/clusters/1/relationships/1",
			status: http.StatusInternalServerError,
			msg:    "Failed to delete relationship"},
		{name: "hidden target", caller: f.admin, method: http.MethodDelete,
			path:   "/api/v1/clusters/1/relationships/2",
			status: http.StatusNotFound, msg: "Relationship not found"},
		{name: "relationship in another cluster", caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/clusters/2/relationships/1",
			status: http.StatusNotFound, msg: "Relationship not found"},
		{name: "nonexistent relationship answers like a hidden one",
			caller: f.admin, method: http.MethodDelete,
			path:   "/api/v1/clusters/1/relationships/999",
			status: http.StatusNotFound, msg: "Relationship not found"},
		{name: "visible relationship deleted", caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/clusters/1/relationships/1",
			status: http.StatusNoContent},
	})

	f.expectValue(t, "relationship", 1, "false")
	f.expectValue(t, "relationship", 2, "true")
}

// TestIssue678_GetConnectionCluster covers GET
// /api/v1/connections/{id}/cluster, which returned the connection's
// relationships, and the cluster selector list, without filtering out
// connections and clusters the caller cannot see.
func TestIssue678_GetConnectionCluster(t *testing.T) {
	f := newClusterCovFixture(t)
	seedIssue678Relationships(t, f)

	get := func(h *ConnectionHandler, caller scopeCaller, connID int) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.handleGetConnectionCluster(rec,
			newScopeRequest(caller, http.MethodGet, ""), connID)
		return rec
	}

	t.Run("restricted caller sees visible peers and clusters only", func(t *testing.T) {
		rec := get(f.connectionHandlerOn(f.pool), f.admin, 6)
		assertStatus(t, rec, http.StatusOK)
		body := rec.Body.String()
		if !strings.Contains(body, `"id":1,`) {
			t.Errorf("Expected the visible relationship 1, got %s", body)
		}
		for _, hidden := range []string{"seven", "Hidden", "Empty"} {
			if strings.Contains(body, hidden) {
				t.Errorf("Response discloses %q: %s", hidden, body)
			}
		}
		if !strings.Contains(body, "Spock one") {
			t.Errorf("Expected the visible cluster Spock one, got %s", body)
		}
	})

	t.Run("superuser sees everything", func(t *testing.T) {
		rec := get(f.connectionHandlerOn(f.pool), f.super, 6)
		assertStatus(t, rec, http.StatusOK)
		body := rec.Body.String()
		for _, want := range []string{"seven", "Hidden"} {
			if !strings.Contains(body, want) {
				t.Errorf("Expected %q in the response, got %s", want, body)
			}
		}
	})

	for allow := int64(1); allow <= 3; allow++ {
		t.Run(fmt.Sprintf("datastore fails after %d", allow), func(t *testing.T) {
			rec := get(f.failingConnectionHandler(t, allow), f.admin, 6)
			assertError(t, rec, http.StatusInternalServerError,
				"Failed to list clusters")
		})
	}
}

// TestFilterRelationshipsByVisibility_Issue678 covers the pure filter:
// a relationship survives only when both ends are visible.
func TestFilterRelationshipsByVisibility_Issue678(t *testing.T) {
	rels := []database.NodeRelationship{
		{ID: 1, SourceConnectionID: 5, TargetConnectionID: 6},
		{ID: 2, SourceConnectionID: 6, TargetConnectionID: 7},
		{ID: 3, SourceConnectionID: 7, TargetConnectionID: 5},
		{ID: 4, SourceConnectionID: 7, TargetConnectionID: 8},
	}
	got := filterRelationshipsByVisibility(rels, map[int]bool{5: true, 6: true})
	if len(got) != 1 || got[0].ID != 1 {
		t.Errorf("filterRelationshipsByVisibility = %+v, want only id 1", got)
	}
	if got := filterRelationshipsByVisibility(nil, map[int]bool{}); got == nil || len(got) != 0 {
		t.Errorf("filterRelationshipsByVisibility(nil) = %#v, want empty non-nil", got)
	}
}
