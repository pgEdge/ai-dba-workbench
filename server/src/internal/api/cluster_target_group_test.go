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
// Creating a cluster in, or moving one into, a cluster group checks the
// group's visibility first (issue #673).
//
// In clusterCovSchema the admin caller (a non-superuser holding
// manage_connections) sees groups 1 and 2; group 3 holds only the
// private connection 8. clusterTargetGroupSetup adds two groups with no
// member connections, 50, owned by the admin caller, and 51, owned by
// someone else, and group 52, owned by the admin caller but holding only
// the private connection 11.
// =============================================================================

const clusterTargetGroupSetup = `
INSERT INTO cluster_groups (id, name, owner_username) VALUES
    (50, 'Empty owned', 'cov_cl_admin'),
    (51, 'Empty unowned', 'someone-else'),
    (52, 'Owned hidden', 'cov_cl_admin');
INSERT INTO clusters (id, group_id, name) VALUES (52, 52, 'Owned hidden');
INSERT INTO connections (id, name, host, database_name, is_shared,
                         owner_username, cluster_id) VALUES
    (11, 'eleven', 'db11.example.com', 'postgres', FALSE, 'someone-else', 52);`

// newClusterTargetGroupFixture returns the cluster fixture with
// clusterTargetGroupSetup applied.
func newClusterTargetGroupFixture(t *testing.T) *clusterCovFixture {
	t.Helper()
	f := newClusterCovFixture(t)
	if _, err := f.pool.Exec(context.Background(),
		clusterTargetGroupSetup); err != nil {
		t.Fatalf("Failed to apply the target group setup: %v", err)
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

// clusterTargetGroupWrite is one of the three routes that place a
// cluster in a group.
type clusterTargetGroupWrite struct {
	name   string
	method string
	path   func(group string) string
	body   func(group string) string
}

var clusterTargetGroupWrites = []clusterTargetGroupWrite{
	{"create in group", http.MethodPost,
		func(g string) string { return "/api/v1/cluster-groups/" + g + "/clusters" },
		func(string) string { return `{"name":"Placed"}` }},
	{"create with group_id", http.MethodPost,
		func(string) string { return "/api/v1/clusters" },
		func(g string) string { return `{"name":"Placed","group_id":` + g + `}` }},
	{"move", http.MethodPut,
		func(string) string { return "/api/v1/clusters/1" },
		func(g string) string { return `{"group_id":` + g + `}` }},
	{"move auto-detected", http.MethodPut,
		func(string) string { return "/api/v1/clusters/server-10" },
		func(g string) string { return `{"name":"Placed","group_id":` + g + `}` }},
}

// TestClusterWritesHideInvisibleGroups checks that a hidden group is
// answered exactly as a missing one, and that a missing group is a 404
// rather than a 500 for every caller.
func TestClusterWritesHideInvisibleGroups(t *testing.T) {
	f := newClusterTargetGroupFixture(t)

	for _, wr := range clusterTargetGroupWrites {
		t.Run(wr.name+" hidden group looks missing", func(t *testing.T) {
			missing := clusterCovServe(f.handler, f.admin, wr.method,
				wr.path("999"), wr.body("999"))
			assertError(t, missing, http.StatusNotFound, "Cluster group not found")
			// 52 is owned by the caller, but its members are hidden.
			for _, g := range []string{"3", "51", "52"} {
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
			f.expectValue(t, "clusters named", "Placed", "0")
		})
	}
}

// TestClusterWritesIntoVisibleGroups checks that visible groups, the
// caller's own empty group and, for a superuser, any group still accept
// a cluster, and covers the datastore failures in the group check.
func TestClusterWritesIntoVisibleGroups(t *testing.T) {
	f := newClusterTargetGroupFixture(t)

	f.run(t, []clusterCovCase{
		// The group check's datastore calls fail in turn: the caller's
		// visibility, the group's members, then the group itself.
		{name: "create in group visibility fails", h: f.failing(t, 0),
			caller: f.admin, method: http.MethodPost,
			path: "/api/v1/cluster-groups/2/clusters", body: `{"name":"Placed"}`,
			status: http.StatusInternalServerError, msg: "Failed to create cluster"},
		{name: "create in group members fail", h: f.failing(t, 1),
			caller: f.admin, method: http.MethodPost,
			path: "/api/v1/cluster-groups/2/clusters", body: `{"name":"Placed"}`,
			status: http.StatusInternalServerError, msg: "Failed to create cluster"},
		{name: "create in group lookup fails", h: f.failing(t, 2),
			caller: f.admin, method: http.MethodPost,
			path: "/api/v1/cluster-groups/2/clusters", body: `{"name":"Placed"}`,
			status: http.StatusInternalServerError, msg: "Failed to create cluster"},
		{name: "create in group insert fails", h: f.failing(t, 1),
			caller: f.super, method: http.MethodPost,
			path: "/api/v1/cluster-groups/2/clusters", body: `{"name":"Placed"}`,
			status: http.StatusInternalServerError, msg: "Failed to create cluster"},
		{name: "create with group_id lookup fails", h: f.failing(t, 0),
			caller: f.super, method: http.MethodPost,
			path: "/api/v1/clusters", body: `{"name":"Placed","group_id":2}`,
			status: http.StatusInternalServerError, msg: "Failed to create cluster"},
		{name: "move target check fails", h: f.failing(t, 2),
			caller: f.admin, method: http.MethodPut,
			path: "/api/v1/clusters/1", body: `{"group_id":2}`,
			status: http.StatusInternalServerError, msg: "Failed to update cluster"},

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
		{name: "create with a visible group_id", caller: f.admin,
			method: http.MethodPost, path: "/api/v1/clusters",
			body: `{"name":"Top visible","group_id":2}`, status: http.StatusCreated,
			msg: "Top visible"},
		{name: "superuser creates in a hidden group", caller: f.super,
			method: http.MethodPost, path: "/api/v1/cluster-groups/3/clusters",
			body: `{"name":"In hidden"}`, status: http.StatusCreated,
			msg: "In hidden"},
		{name: "move into an owned empty group", caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/1",
			body: `{"group_id":50}`, status: http.StatusOK, msg: `"Int32":50`},
		{name: "move into a visible group", caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/1",
			body: `{"group_id":2}`, status: http.StatusOK, msg: "Alpha"},
		{name: "move auto-detected into a visible group", caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/server-10",
			body: `{"name":"Auto placed","group_id":2}`, status: http.StatusOK,
			msg: "Auto placed"},
	})

	f.expectValue(t, "cluster group", 1, "2")
	f.expectValue(t, "clusters named", "Auto placed", "1")
	f.expectValue(t, "clusters named", "In owned", "1")
	f.expectValue(t, "clusters named", "Top owned", "1")
	f.expectValue(t, "clusters named", "Top visible", "1")
	f.expectValue(t, "clusters named", "In hidden", "1")
}
