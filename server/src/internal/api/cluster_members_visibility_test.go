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
	"reflect"
	"testing"
)

// =============================================================================
// Cluster and group writes that change the overrides reaching every
// member require the caller to see every member (issue #716), and a
// hidden connection cannot be detached from a cluster.
//
// clusterMembersSetup adds to clusterCovSchema, in which the admin caller
// sees the shared connections 5, 6 and 9 and nothing else:
//
//   - group 70 holds cluster 71 (key spock:mixed), whose members are the
//     shared connection 72 and the private connection 73;
//   - group 80 holds cluster 81, whose only member is the shared
//     connection 74;
//   - group 80 also holds cluster 82 (key binary:76), whose members are
//     the shared connection 76 and the private connection 77;
//   - the dismissed cluster 90 still lists the shared connection 78;
//   - relationship 2 links connection 5 to the private connection 7 in
//     cluster 1.
// =============================================================================

const clusterMembersSetup = `
INSERT INTO cluster_groups (id, name, owner_username) VALUES
    (70, 'Mixed', 'someone-else'),
    (80, 'Full', 'someone-else');
INSERT INTO clusters (id, group_id, auto_cluster_key, name, replication_type,
                      dismissed) VALUES
    (71, 70, 'spock:mixed', 'Mixed cluster', 'spock', FALSE),
    (81, 80, NULL, 'Full cluster', NULL, FALSE),
    (82, 80, 'binary:76', 'Binary mixed', 'binary', FALSE),
    (90, 80, NULL, 'Dismissed', NULL, TRUE);
INSERT INTO connections (id, name, host, database_name, is_shared,
                         owner_username, cluster_id) VALUES
    (72, 'seventy-two', 'db72.example.com', 'postgres', TRUE, 'someone-else', 71),
    (73, 'seventy-three', 'db73.example.com', 'postgres', FALSE, 'someone-else', 71),
    (74, 'seventy-four', 'db74.example.com', 'postgres', TRUE, 'someone-else', 81),
    (76, 'seventy-six', 'db76.example.com', 'postgres', TRUE, 'someone-else', 82),
    (77, 'seventy-seven', 'db77.example.com', 'postgres', FALSE, 'someone-else', 82),
    (78, 'seventy-eight', 'db78.example.com', 'postgres', TRUE, 'someone-else', 90);
INSERT INTO cluster_node_relationships (id, cluster_id, source_connection_id,
                                        target_connection_id,
                                        relationship_type) VALUES
    (2, 1, 5, 7, 'streams_from');`

// memberRefusal is the answer the numeric cluster routes give when a
// member is hidden: their own manage_connections refusal.
var memberRefusal = adminPermissionRefusal("manage_connections")

// newClusterMembersFixture returns the cluster fixture with
// clusterMembersSetup applied and its handler's refusals recorded in the
// returned slice.
func newClusterMembersFixture(t *testing.T) (*clusterCovFixture, *[]string) {
	t.Helper()
	f := newClusterCovFixture(t)
	if _, err := f.pool.Exec(context.Background(), clusterMembersSetup); err != nil {
		t.Fatalf("Failed to apply the member setup: %v", err)
	}
	audited := &[]string{}
	f.handler.SetDenialRecorder(func(_ *http.Request, reason string) {
		*audited = append(*audited, reason)
	})
	return f, audited
}

// expectAudited fails unless the recorded refusals are exactly want, then
// clears them.
func expectAudited(t *testing.T, audited *[]string, want ...string) {
	t.Helper()
	got := *audited
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("audited %q, want %q", got, want)
	}
	*audited = nil
}

// assertNoPermissionRefusal checks that hidden, a 403 for a hidden
// member, is byte for byte the answer that f.plain, who holds no admin
// permission, gets from the same request, and that the two are audited
// with the same single reason. It clears audited.
func assertNoPermissionRefusal(t *testing.T, f *clusterCovFixture,
	audited *[]string, hidden *httptest.ResponseRecorder, method, path,
	body string) {

	t.Helper()
	if hidden.Code != http.StatusForbidden {
		t.Fatalf("%s %s: status %d, want 403: %s", method, path,
			hidden.Code, hidden.Body.String())
	}
	got := *audited
	*audited = nil
	if len(got) != 1 {
		t.Fatalf("%s %s: audited %q, want one reason", method, path, got)
	}
	plain := clusterCovServe(f.handler, f.plain, method, path, body)
	assertSameResponse(t, hidden, plain)
	expectAudited(t, audited, got...)
}

// TestRemoveServerHidesHiddenMembers checks that a restricted caller
// cannot detach a connection they cannot see, and that the answer is the
// one for a connection that is not a member.
func TestRemoveServerHidesHiddenMembers(t *testing.T) {
	f, audited := newClusterMembersFixture(t)

	member := clusterCovServe(f.handler, f.admin, http.MethodDelete,
		"/api/v1/clusters/1/servers/7", "")
	assertError(t, member, http.StatusNotFound, "Connection not found in this cluster")
	for _, path := range []string{
		"/api/v1/clusters/1/servers/9",  // visible, not a member
		"/api/v1/clusters/1/servers/10", // hidden, not a member
	} {
		other := clusterCovServe(f.handler, f.admin, http.MethodDelete, path, "")
		assertSameResponse(t, member, other)
	}
	expectAudited(t, audited)
	f.expectValue(t, "conn cluster", 7, "1")
	f.expectValue(t, "relationship", 2, "true")

	// A superuser sees every connection and may detach it.
	f.run(t, []clusterCovCase{
		{name: "superuser detaches a private member", caller: f.super,
			method: http.MethodDelete, path: "/api/v1/clusters/1/servers/7",
			status: http.StatusNoContent},
	})
	f.expectValue(t, "conn cluster", 7, "none")
	f.expectValue(t, "relationship", 2, "false")
}

// TestDeleteClusterGroupNeedsEveryMember covers DELETE
// /api/v1/cluster-groups/{id} for a group with a hidden member.
func TestDeleteClusterGroupNeedsEveryMember(t *testing.T) {
	f, audited := newClusterMembersFixture(t)

	// The refusal is the route's own one for a caller who neither holds
	// manage_connections nor owns the group.
	const path = "/api/v1/cluster-groups/70"
	rec := clusterCovServe(f.handler, f.admin, http.MethodDelete, path, "")
	assertError(t, rec, http.StatusForbidden, clusterGroupDeleteRefusal)
	assertNoPermissionRefusal(t, f, audited, rec, http.MethodDelete, path, "")
	f.expectValue(t, "cluster exists", 71, "true")

	// Group 80 holds the private connection 77 too, so detach it first
	// to leave a group whose members are all visible.
	if _, err := f.pool.Exec(context.Background(),
		"UPDATE connections SET cluster_id = NULL WHERE id = 77"); err != nil {
		t.Fatalf("Failed to detach connection 77: %v", err)
	}
	f.run(t, []clusterCovCase{
		{name: "group with every member visible", caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/cluster-groups/80",
			status: http.StatusNoContent},
		{name: "superuser deletes a mixed group", caller: f.super,
			method: http.MethodDelete, path: "/api/v1/cluster-groups/70",
			status: http.StatusNoContent},
	})
	expectAudited(t, audited)
	f.expectValue(t, "cluster exists", 81, "false")
	f.expectValue(t, "cluster exists", 71, "false")
}

// TestDeleteClusterNeedsEveryMember covers DELETE /api/v1/clusters/{id}
// for manual and auto-detected clusters with a hidden member.
func TestDeleteClusterNeedsEveryMember(t *testing.T) {
	f, audited := newClusterMembersFixture(t)

	f.run(t, []clusterCovCase{
		{name: "member lookup fails", h: f.failing(t, 2), caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/clusters/71",
			status: http.StatusInternalServerError,
			msg:    "Failed to check scope visibility"},
		{name: "auto-detected cluster with a hidden member", caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/clusters/71",
			status: http.StatusForbidden, msg: memberRefusal},
		{name: "manual cluster with a hidden member", caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/clusters/1",
			status: http.StatusForbidden, msg: memberRefusal},
	})
	expectAudited(t, audited, memberRefusal, memberRefusal)
	f.expectValue(t, "cluster dismissed", 71, "false")
	f.expectValue(t, "conn cluster", 73, "71")
	f.expectValue(t, "cluster exists", 1, "true")

	f.run(t, []clusterCovCase{
		{name: "cluster with every member visible", caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/clusters/81",
			status: http.StatusNoContent},
		{name: "superuser dismisses a mixed cluster", caller: f.super,
			method: http.MethodDelete, path: "/api/v1/clusters/71",
			status: http.StatusNoContent},
	})
	expectAudited(t, audited)
	f.expectValue(t, "cluster exists", 81, "false")
	f.expectValue(t, "cluster dismissed", 71, "true")
	f.expectValue(t, "conn cluster", 73, "none")
}

// TestUpdateClusterGroupMoveNeedsEveryMember covers PUT
// /api/v1/clusters/{id} with a group_id: only a change of group is
// checked, and only against the moved cluster's members.
func TestUpdateClusterGroupMoveNeedsEveryMember(t *testing.T) {
	f, audited := newClusterMembersFixture(t)

	f.run(t, []clusterCovCase{
		{name: "cluster lookup fails", h: f.failing(t, 6), caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/71",
			body: `{"group_id":80}`, status: http.StatusInternalServerError,
			msg: "Failed to update cluster"},
		{name: "member lookup fails", h: f.failing(t, 7), caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/71",
			body: `{"group_id":80}`, status: http.StatusInternalServerError,
			msg: "Failed to check scope visibility"},
		{name: "dismissed cluster", caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/90",
			body: `{"group_id":70}`, status: http.StatusNotFound,
			msg: "Cluster not found"},
		{name: "move a cluster with a hidden member", caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/71",
			body: `{"group_id":80}`, status: http.StatusForbidden,
			msg: memberRefusal},
	})
	expectAudited(t, audited, memberRefusal)
	f.expectValue(t, "cluster group", 71, "70")

	f.run(t, []clusterCovCase{
		// The client sends the current group_id when it renames.
		{name: "rename keeping the group", caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/71",
			body:   `{"name":"Renamed mixed","group_id":70}`,
			status: http.StatusOK, msg: "Renamed mixed"},
		{name: "rename without a group", caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/1",
			body: `{"name":"Renamed one"}`, status: http.StatusOK},
		// The target group 70 holds a hidden member, which is not
		// checked: the move changes nothing for that group's members.
		{name: "move a fully visible cluster", caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/81",
			body: `{"group_id":70}`, status: http.StatusOK},
		{name: "superuser moves a mixed cluster", caller: f.super,
			method: http.MethodPut, path: "/api/v1/clusters/71",
			body: `{"group_id":80}`, status: http.StatusOK},
	})
	expectAudited(t, audited)
	f.expectValue(t, "cluster name", 71, "Renamed mixed")
	f.expectValue(t, "cluster group", 81, "70")
	f.expectValue(t, "cluster group", 71, "80")
}

// TestAutoDetectedClusterWritesNeedEveryMember covers PUT and DELETE on
// the cluster-spock-{prefix} and server-{id} topology IDs.
func TestAutoDetectedClusterWritesNeedEveryMember(t *testing.T) {
	f, audited := newClusterMembersFixture(t)
	const spock = "/api/v1/clusters/cluster-spock-mixed"
	const server = "/api/v1/clusters/server-76"

	f.run(t, []clusterCovCase{
		// The cluster's own visibility check takes two acquisitions and
		// the target group's four; a move then looks up the cluster's
		// current group, and the member check's visibility lookup and
		// member list come next.
		{name: "delete visibility lookup fails", h: f.failing(t, 2),
			caller: f.admin, method: http.MethodDelete, path: spock,
			status: http.StatusInternalServerError,
			msg:    "Failed to delete auto-detected cluster"},
		{name: "move group lookup fails", h: f.failing(t, 6),
			caller: f.admin, method: http.MethodPut, path: spock,
			body: `{"group_id":80}`, status: http.StatusInternalServerError,
			msg: "Failed to update auto-detected cluster"},
		{name: "move member lookup fails", h: f.failing(t, 7),
			caller: f.admin, method: http.MethodPut, path: spock,
			body: `{"group_id":70}`, status: http.StatusInternalServerError,
			msg: "Failed to update auto-detected cluster"},
		{name: "delete member lookup fails", h: f.failing(t, 3),
			caller: f.admin, method: http.MethodDelete, path: spock,
			status: http.StatusInternalServerError,
			msg:    "Failed to delete auto-detected cluster"},
	})
	expectAudited(t, audited)

	// Each refusal is the route's own one for a caller without
	// manage_connections.
	for _, tc := range []struct {
		method, path, body, refusal string
	}{
		{http.MethodPut, spock, `{"group_id":80}`, autoClusterModifyRefusal},
		{http.MethodPut, server, `{"group_id":70,"auto_cluster_key":"binary:76"}`, autoClusterModifyRefusal},
		{http.MethodDelete, spock, "", autoClusterDeleteRefusal},
		{http.MethodDelete, server, "", autoClusterDeleteRefusal},
	} {
		rec := clusterCovServe(f.handler, f.admin, tc.method, tc.path, tc.body)
		assertError(t, rec, http.StatusForbidden, tc.refusal)
		assertNoPermissionRefusal(t, f, audited, rec, tc.method, tc.path, tc.body)
	}
	f.expectValue(t, "cluster group", 71, "70")
	f.expectValue(t, "cluster group", 82, "80")
	f.expectValue(t, "cluster dismissed", 71, "false")
	f.expectValue(t, "cluster dismissed", 82, "false")

	f.run(t, []clusterCovCase{
		{name: "rename a spock cluster with a hidden member", caller: f.admin,
			method: http.MethodPut, path: spock, body: `{"name":"Renamed"}`,
			status: http.StatusOK, msg: "Renamed"},
		// Naming the group the cluster is already in moves nothing, as
		// on the numeric route.
		{name: "keep a spock cluster with a hidden member in its group",
			caller: f.admin, method: http.MethodPut, path: spock,
			body:   `{"name":"Kept","group_id":70}`,
			status: http.StatusOK, msg: "Kept"},
		{name: "keep a server cluster with a hidden member in its group",
			caller: f.admin, method: http.MethodPut, path: server,
			body:   `{"group_id":80,"auto_cluster_key":"binary:76"}`,
			status: http.StatusOK},
		{name: "move a fully visible spock cluster", caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/cluster-spock-auto1",
			body: `{"group_id":1}`, status: http.StatusOK},
		{name: "delete a fully visible spock cluster", caller: f.admin,
			method: http.MethodDelete,
			path:   "/api/v1/clusters/cluster-spock-auto1",
			status: http.StatusNoContent},
		{name: "superuser deletes a mixed server cluster", caller: f.super,
			method: http.MethodDelete, path: server,
			status: http.StatusNoContent},
	})
	expectAudited(t, audited)
	f.expectValue(t, "cluster by key", "spock:mixed", "Kept")
	f.expectValue(t, "cluster group", 71, "70")
	f.expectValue(t, "cluster dismissed", 2, "true")
	f.expectValue(t, "cluster dismissed", 82, "true")
}

// TestAutoDetectedClusterKeys covers the topology ID to key mapping.
func TestAutoDetectedClusterKeys(t *testing.T) {
	tests := []struct {
		id   string
		want []string
	}{
		{"cluster-spock-abc", []string{"spock:abc"}},
		{"server-12", []string{"binary:12", "standalone:12", "logical:12"}},
		{"binary-1", nil},
	}
	for _, tt := range tests {
		if got := autoDetectedClusterKeys(tt.id); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("autoDetectedClusterKeys(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}
