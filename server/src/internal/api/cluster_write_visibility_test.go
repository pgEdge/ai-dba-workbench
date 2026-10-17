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
// Cluster writes check the target group's visibility (issue #673).
//
// In clusterCovSchema the admin caller (a non-superuser holding
// manage_connections) sees connections 5, 6 and 9, clusters 1 and 2 and
// groups 1 and 2; group 3 and cluster 3 hold only the private
// connection 8, and connection 7 in cluster 1 is private.
// =============================================================================

// clusterWriteVisibilitySetup adds two groups with no member
// connections, 50 owned by the admin caller and 51 by someone else, and
// the auto-detected Spock cluster 60 (key spock:priv) in group 3, whose
// only member is the private connection 61.
const clusterWriteVisibilitySetup = `
INSERT INTO cluster_groups (id, name, owner_username) VALUES
    (50, 'Empty owned', 'cov_cl_admin'),
    (51, 'Empty unowned', 'someone-else');
INSERT INTO clusters (id, group_id, auto_cluster_key, name, replication_type)
    VALUES (60, 3, 'spock:priv', 'Private Spock', 'spock');
INSERT INTO connections (id, name, host, database_name, is_shared,
                         owner_username, cluster_id) VALUES
    (61, 'sixty-one', 'db61.example.com', 'postgres', FALSE,
     'someone-else', 60);`

// newClusterWriteVisibilityFixture returns the cluster fixture with
// clusterWriteVisibilitySetup applied.
func newClusterWriteVisibilityFixture(t *testing.T) *clusterCovFixture {
	t.Helper()
	f := newClusterCovFixture(t)
	if _, err := f.pool.Exec(context.Background(),
		clusterWriteVisibilitySetup); err != nil {
		t.Fatalf("Failed to apply the visibility setup: %v", err)
	}
	return f
}

// assertSameResponse fails unless the hidden and missing responses have
// the same status and body.
func assertSameResponse(t *testing.T, hidden, missing *httptest.ResponseRecorder) {
	t.Helper()
	if hidden.Code != missing.Code || hidden.Body.String() != missing.Body.String() {
		t.Errorf("Hidden response %d %q differs from missing response %d %q",
			hidden.Code, hidden.Body.String(), missing.Code, missing.Body.String())
	}
}

// TestClusterWritesHideInvisibleGroups checks that creating a cluster in,
// or moving a cluster into, a group the caller cannot see is answered
// exactly as for a missing group, and that a missing group gives 404
// rather than 500 to every caller.
func TestClusterWritesHideInvisibleGroups(t *testing.T) {
	f := newClusterWriteVisibilityFixture(t)

	type write struct {
		name   string
		method string
		path   func(group string) string
		body   func(group string) string
	}
	writes := []write{
		{"create in group", http.MethodPost,
			func(g string) string { return "/api/v1/cluster-groups/" + g + "/clusters" },
			func(string) string { return `{"name":"Placed"}` }},
		{"create with group_id", http.MethodPost,
			func(string) string { return "/api/v1/clusters" },
			func(g string) string { return `{"name":"Placed","group_id":` + g + `}` }},
		{"move", http.MethodPut,
			func(string) string { return "/api/v1/clusters/1" },
			func(g string) string { return `{"group_id":` + g + `}` }},
	}

	for _, wr := range writes {
		t.Run(wr.name+" hidden group looks missing", func(t *testing.T) {
			missing := clusterCovServe(f.handler, f.admin, wr.method,
				wr.path("999"), wr.body("999"))
			assertError(t, missing, http.StatusNotFound, "Cluster group not found")
			for _, g := range []string{"3", "51"} {
				hidden := clusterCovServe(f.handler, f.admin, wr.method,
					wr.path(g), wr.body(g))
				assertSameResponse(t, hidden, missing)
			}
			f.expectValue(t, "clusters named", "Placed", "0")
			f.expectValue(t, "cluster group", 1, "1")
		})
		t.Run(wr.name+" missing group for a superuser", func(t *testing.T) {
			rec := clusterCovServe(f.handler, f.super, wr.method,
				wr.path("999"), wr.body("999"))
			assertError(t, rec, http.StatusNotFound, "Cluster group not found")
		})
	}

	f.run(t, []clusterCovCase{
		// The group lookups fail in turn: the caller's visibility, the
		// group's members, then the group itself.
		{name: "create in group visibility fails", h: f.failing(t, 0),
			caller: f.admin, method: http.MethodPost,
			path: "/api/v1/cluster-groups/2/clusters", body: `{"name":"Placed"}`,
			status: http.StatusInternalServerError, msg: "Failed to create cluster"},
		{name: "create in group members fail", h: f.failing(t, 1),
			caller: f.admin, method: http.MethodPost,
			path: "/api/v1/cluster-groups/2/clusters", body: `{"name":"Placed"}`,
			status: http.StatusInternalServerError, msg: "Failed to create cluster"},
		{name: "create in group default lookup fails", h: f.failing(t, 2),
			caller: f.admin, method: http.MethodPost,
			path: "/api/v1/cluster-groups/2/clusters", body: `{"name":"Placed"}`,
			status: http.StatusInternalServerError, msg: "Failed to create cluster"},
		{name: "create in group lookup fails", h: f.failing(t, 3),
			caller: f.admin, method: http.MethodPost,
			path: "/api/v1/cluster-groups/2/clusters", body: `{"name":"Placed"}`,
			status: http.StatusInternalServerError, msg: "Failed to create cluster"},
		{name: "create in group insert fails", h: f.failing(t, 1),
			caller: f.super, method: http.MethodPost,
			path: "/api/v1/cluster-groups/2/clusters", body: `{"name":"Placed"}`,
			status: http.StatusInternalServerError, msg: "Failed to create cluster"},
		{name: "move target lookup fails", h: f.failing(t, 2),
			caller: f.admin, method: http.MethodPut,
			path: "/api/v1/clusters/1", body: `{"group_id":2}`,
			status: http.StatusInternalServerError, msg: "Failed to update cluster"},
		{name: "create with group_id lookup fails", h: f.failing(t, 0),
			caller: f.super, method: http.MethodPost,
			path: "/api/v1/clusters", body: `{"name":"Placed","group_id":2}`,
			status: http.StatusInternalServerError, msg: "Failed to create cluster"},

		// Visible groups, and the caller's own empty group, still work.
		{name: "create in a visible group", caller: f.admin,
			method: http.MethodPost, path: "/api/v1/cluster-groups/2/clusters",
			body: `{"name":"In visible"}`, status: http.StatusCreated,
			msg: "In visible"},
		{name: "create in an owned empty group", caller: f.admin,
			method: http.MethodPost, path: "/api/v1/cluster-groups/50/clusters",
			body: `{"name":"In owned"}`, status: http.StatusCreated,
			msg: "In owned"},
		{name: "create with an owned empty group_id", caller: f.admin,
			method: http.MethodPost, path: "/api/v1/clusters",
			body: `{"name":"Top owned","group_id":50}`, status: http.StatusCreated,
			msg: "Top owned"},
		{name: "superuser creates in a hidden group", caller: f.super,
			method: http.MethodPost, path: "/api/v1/cluster-groups/3/clusters",
			body: `{"name":"In hidden"}`, status: http.StatusCreated,
			msg: "In hidden"},
		{name: "move into a visible group", caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/1",
			body: `{"group_id":2}`, status: http.StatusOK, msg: "Alpha"},
	})

	f.expectValue(t, "cluster group", 1, "2")
	f.expectValue(t, "clusters named", "Top owned", "1")
}
