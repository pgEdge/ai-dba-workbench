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

// TestClusterWritesAdmitTheDefaultGroup checks that the default group is
// writable whether or not the caller can see a connection in it, both
// when group_id is omitted and when it names the default group.
func TestClusterWritesAdmitTheDefaultGroup(t *testing.T) {
	f := newClusterWriteVisibilityFixture(t)
	// Make group 3, which holds only the private connection 8, the
	// default.
	if _, err := f.pool.Exec(context.Background(),
		"UPDATE cluster_groups SET is_default = (id = 3)"); err != nil {
		t.Fatalf("Failed to move the default group: %v", err)
	}

	f.run(t, []clusterCovCase{
		{name: "create without a group", caller: f.admin,
			method: http.MethodPost, path: "/api/v1/clusters",
			body: `{"name":"No group"}`, status: http.StatusCreated},
		{name: "create in the default group", caller: f.admin,
			method: http.MethodPost, path: "/api/v1/cluster-groups/3/clusters",
			body: `{"name":"In default"}`, status: http.StatusCreated},
		{name: "create with the default group_id", caller: f.admin,
			method: http.MethodPost, path: "/api/v1/clusters",
			body: `{"name":"Top default","group_id":3}`, status: http.StatusCreated},
		{name: "move into the default group", caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/1",
			body: `{"group_id":3}`, status: http.StatusOK},
	})

	f.expectValue(t, "clusters named", "No group", "1")
	f.expectValue(t, "clusters named", "In default", "1")
	f.expectValue(t, "clusters named", "Top default", "1")
	f.expectValue(t, "cluster group", 1, "3")
}

// TestAutoDetectedClusterWritesCheckVisibility checks that a restricted
// caller cannot rename, move or dismiss an auto-detected cluster they
// cannot see, nor move one into a group they cannot see, and that each
// refusal matches the answer for a missing cluster or group.
func TestAutoDetectedClusterWritesCheckVisibility(t *testing.T) {
	f := newClusterWriteVisibilityFixture(t)
	const clusters = "/api/v1/clusters/"

	type request struct{ path, body string }
	pairs := []struct {
		name            string
		method          string
		missing, hidden request
	}{
		{"rename a server cluster", http.MethodPut,
			request{"server-999", `{"name":"Taken"}`},
			request{"server-8", `{"name":"Taken"}`}},
		{"rename a spock cluster", http.MethodPut,
			request{"cluster-spock-none", `{"name":"Taken"}`},
			request{"cluster-spock-priv", `{"name":"Taken"}`}},
		{"server key of a hidden cluster", http.MethodPut,
			request{"server-999", `{"name":"Taken"}`},
			request{"server-5", `{"name":"Taken","auto_cluster_key":"binary:8"}`}},
		{"spock key of a hidden cluster", http.MethodPut,
			request{"cluster-spock-none", `{"name":"Taken"}`},
			request{"cluster-spock-auto1", `{"name":"Taken","auto_cluster_key":"spock:priv"}`}},
		{"unparsable server id", http.MethodPut,
			request{"server-999", `{"name":"Taken"}`},
			request{"server-x", `{"name":"Taken"}`}},
		{"move into a hidden group", http.MethodPut,
			request{"cluster-spock-auto1", `{"group_id":999}`},
			request{"cluster-spock-auto1", `{"group_id":3}`}},
		{"dismiss a server cluster", http.MethodDelete,
			request{"server-999", ""}, request{"server-8", ""}},
		{"dismiss a spock cluster", http.MethodDelete,
			request{"cluster-spock-none", ""}, request{"cluster-spock-priv", ""}},
	}
	for _, tc := range pairs {
		t.Run(tc.name, func(t *testing.T) {
			missing := clusterCovServe(f.handler, f.admin, tc.method,
				clusters+tc.missing.path, tc.missing.body)
			assertStatus(t, missing, http.StatusNotFound)
			hidden := clusterCovServe(f.handler, f.admin, tc.method,
				clusters+tc.hidden.path, tc.hidden.body)
			assertSameResponse(t, hidden, missing)
		})
	}
	f.expectValue(t, "clusters named", "Taken", "0")
	f.expectValue(t, "cluster group", 2, "2")
	f.expectValue(t, "cluster dismissed", 60, "false")

	f.run(t, []clusterCovCase{
		// The checks fail in turn: the caller's visibility, then the
		// Spock cluster's members.
		{name: "update visibility fails", h: f.failing(t, 0), caller: f.admin,
			method: http.MethodPut, path: clusters + "cluster-spock-auto1",
			body: `{"name":"Renamed"}`, status: http.StatusInternalServerError,
			msg: "Failed to update auto-detected cluster"},
		{name: "update members fail", h: f.failing(t, 1), caller: f.admin,
			method: http.MethodPut, path: clusters + "cluster-spock-auto1",
			body: `{"name":"Renamed"}`, status: http.StatusInternalServerError,
			msg: "Failed to update auto-detected cluster"},
		{name: "delete visibility fails", h: f.failing(t, 0), caller: f.admin,
			method: http.MethodDelete, path: clusters + "server-5",
			status: http.StatusInternalServerError,
			msg:    "Failed to delete auto-detected cluster"},
		{name: "superuser moves into a missing group", caller: f.super,
			method: http.MethodPut, path: clusters + "cluster-spock-auto1",
			body: `{"group_id":999}`, status: http.StatusNotFound,
			msg: "Cluster group not found"},

		// Visible clusters can still be renamed, moved and dismissed.
		{name: "rename a visible spock cluster", caller: f.admin,
			method: http.MethodPut, path: clusters + "cluster-spock-auto1",
			body:   `{"name":"Renamed","auto_cluster_key":"spock:auto1"}`,
			status: http.StatusOK, msg: "Renamed"},
		{name: "move a visible spock cluster", caller: f.admin,
			method: http.MethodPut, path: clusters + "cluster-spock-auto1",
			body: `{"group_id":1}`, status: http.StatusOK},
		{name: "rename a visible server cluster", caller: f.admin,
			method: http.MethodPut, path: clusters + "server-5",
			body:   `{"name":"Five","auto_cluster_key":"logical:5"}`,
			status: http.StatusOK, msg: "Five"},
		{name: "dismiss a visible server cluster", caller: f.admin,
			method: http.MethodDelete, path: clusters + "server-5",
			status: http.StatusNoContent},
		{name: "dismiss a visible spock cluster", caller: f.admin,
			method: http.MethodDelete, path: clusters + "cluster-spock-auto1",
			status: http.StatusNoContent},
	})
	f.expectValue(t, "cluster by key", "spock:auto1", "Renamed")
	f.expectValue(t, "cluster group", 2, "1")
	f.expectValue(t, "cluster dismissed", 2, "true")

	// The router forwards only the two known prefixes, so a cluster ID
	// of any other shape is reached by calling the handler directly.
	t.Run("unknown cluster type", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := f.admin.wrap(httptest.NewRequest(http.MethodDelete,
			clusters+"binary-1", nil))
		f.handler.deleteAutoDetectedCluster(rec, req, "binary-1")
		assertError(t, rec, http.StatusNotFound, "Cluster not found")
	})
}
