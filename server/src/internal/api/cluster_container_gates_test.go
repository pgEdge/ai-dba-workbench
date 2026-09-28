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
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Tests for the container-write gates added for issue #471: moving a
// connection into a cluster the caller cannot see, an unknown role on
// that move, and cluster group updates and deletes by a token whose
// connection scope does not cover every connection. They reuse the
// estate seeded by newClusterCovFixture.
// =============================================================================

// connectionHandlerOn returns a ConnectionHandler over pool that shares
// the fixture's auth store.
func (f *clusterCovFixture) connectionHandlerOn(pool *pgxpool.Pool) *ConnectionHandler {
	return NewConnectionHandlerWithSecurity(database.NewTestDatastore(pool),
		f.store, auth.NewRBACChecker(f.store), false, nil, nil)
}

// failingConnectionHandler returns a ConnectionHandler whose datastore
// lets the first allow connection acquisitions through and fails every
// later one, in the same way as clusterCovFixture.failing.
func (f *clusterCovFixture) failingConnectionHandler(t *testing.T, allow int64) *ConnectionHandler {
	t.Helper()

	cfg, err := pgxpool.ParseConfig(f.connStr)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.MaxConns = 2
	var acquired atomic.Int64
	cfg.PrepareConn = func(context.Context, *pgx.Conn) (bool, error) {
		if acquired.Add(1) > allow {
			return true, errClusterCovInjected
		}
		return true, nil
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	t.Cleanup(pool.Close)
	return f.connectionHandlerOn(pool)
}

// TestUpdateConnectionClusterTargetAndRole covers the target-cluster
// visibility check and the role validation on
// PUT /api/v1/connections/{id}/cluster.
func TestUpdateConnectionClusterTargetAndRole(t *testing.T) {
	f := newClusterCovFixture(t)
	handler := f.connectionHandlerOn(f.pool)

	put := func(h *ConnectionHandler, caller scopeCaller, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.handleUpdateConnectionCluster(rec,
			newScopeRequest(caller, http.MethodPut, body), 5)
		return rec
	}
	reset := func(t *testing.T) {
		t.Helper()
		if _, err := f.pool.Exec(context.Background(),
			"UPDATE connections SET cluster_id = 1, role = 'primary' WHERE id = 5"); err != nil {
			t.Fatalf("Reset connection: %v", err)
		}
	}

	t.Run("unknown role refused", func(t *testing.T) {
		reset(t)
		rec := put(handler, f.super,
			`{"cluster_id":2,"role":"overlord","membership_source":"manual"}`)
		assertError(t, rec, http.StatusBadRequest, "Invalid role")
		f.expectValue(t, "conn cluster", 5, "1")
		f.expectValue(t, "conn role", 5, "primary")
	})

	t.Run("empty role refused", func(t *testing.T) {
		reset(t)
		rec := put(handler, f.super,
			`{"cluster_id":2,"role":"","membership_source":"manual"}`)
		assertError(t, rec, http.StatusBadRequest, "Invalid role")
		f.expectValue(t, "conn cluster", 5, "1")
	})

	t.Run("known role accepted", func(t *testing.T) {
		reset(t)
		rec := put(handler, f.admin,
			`{"cluster_id":2,"role":"spock_node","membership_source":"manual"}`)
		assertStatus(t, rec, http.StatusOK)
		f.expectValue(t, "conn cluster", 5, "2")
		f.expectValue(t, "conn role", 5, "spock_node")
	})

	t.Run("null role accepted", func(t *testing.T) {
		reset(t)
		rec := put(handler, f.admin,
			`{"cluster_id":2,"role":null,"membership_source":"manual"}`)
		assertStatus(t, rec, http.StatusOK)
		f.expectValue(t, "conn role", 5, "none")
	})

	// Cluster 3's only member is private connection 8, so it is hidden
	// from a non-superuser; the narrowed token covers connections 5 and
	// 7 only, so cluster 2 (member: connection 9) is hidden from it.
	for _, tc := range []struct {
		name   string
		caller scopeCaller
		body   string
	}{
		{"invisible cluster", f.admin, `{"cluster_id":3,"membership_source":"manual"}`},
		{"cluster outside the token's scope", f.narrowed, `{"cluster_id":2,"membership_source":"manual"}`},
	} {
		t.Run(tc.name+" answers 404", func(t *testing.T) {
			reset(t)
			rec := put(handler, tc.caller, tc.body)
			assertError(t, rec, http.StatusNotFound, "Cluster not found")
			f.expectValue(t, "conn cluster", 5, "1")
		})
	}

	// Cluster 4 has no members; the server dialog creates a cluster and
	// then assigns a connection to it, so an empty cluster is allowed.
	t.Run("empty cluster allowed", func(t *testing.T) {
		reset(t)
		rec := put(handler, f.admin, `{"cluster_id":4,"membership_source":"manual"}`)
		assertStatus(t, rec, http.StatusOK)
		f.expectValue(t, "conn cluster", 5, "4")
	})

	t.Run("superuser may move into any cluster", func(t *testing.T) {
		reset(t)
		rec := put(handler, f.super, `{"cluster_id":3,"membership_source":"manual"}`)
		assertStatus(t, rec, http.StatusOK)
		f.expectValue(t, "conn cluster", 5, "3")
	})

	t.Run("reset to auto-detection skips the cluster check", func(t *testing.T) {
		reset(t)
		rec := put(handler, f.admin, `{"cluster_id":null}`)
		assertStatus(t, rec, http.StatusOK)
	})

	// Acquisitions on the datastore: the connection access check, then
	// the visible-set lookup, then the cluster membership lookup.
	for _, tc := range []struct {
		name  string
		allow int64
	}{
		{"visible set lookup fails", 1},
		{"cluster membership lookup fails", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reset(t)
			rec := put(f.failingConnectionHandler(t, tc.allow), f.admin,
				`{"cluster_id":2,"membership_source":"manual"}`)
			assertError(t, rec, http.StatusInternalServerError,
				"Failed to assign connection to cluster")
			f.expectValue(t, "conn cluster", 5, "1")
		})
	}
}

// TestClusterGroupWritesRespectTokenScope covers the connection-scope
// gate and the visibility check on PUT and DELETE
// /api/v1/cluster-groups/{id}.
func TestClusterGroupWritesRespectTokenScope(t *testing.T) {
	f := newClusterCovFixture(t)
	const rename = `{"name":"Renamed"}`

	groupName := func(t *testing.T, id int) string {
		t.Helper()
		var name string
		if err := f.pool.QueryRow(context.Background(),
			"SELECT name FROM cluster_groups WHERE id = $1", id).Scan(&name); err != nil {
			t.Fatalf("Read group %d: %v", id, err)
		}
		return name
	}

	// GetUserInfoFromRequest resolves only session tokens, so an API
	// token is refused with 401 before these gates today. The token
	// callers here carry a session bearer as well as the token context,
	// so that the gate is exercised should that lookup ever accept API
	// tokens.
	asToken := func(token scopeCaller) scopeCaller {
		return scopeCaller{name: token.name, wrap: func(r *http.Request) *http.Request {
			return token.wrap(f.admin.wrap(r))
		}}
	}

	for _, caller := range []scopeCaller{asToken(f.narrowed), asToken(f.readOnly)} {
		t.Run(caller.name+" update refused", func(t *testing.T) {
			rec := clusterCovServe(f.handler, caller, http.MethodPut,
				"/api/v1/cluster-groups/2", rename)
			assertOutOfTokenScope(t, rec)
			if got := groupName(t, 2); got != "Estate" {
				t.Errorf("Group renamed to %q despite the refusal", got)
			}
		})
		t.Run(caller.name+" delete refused", func(t *testing.T) {
			rec := clusterCovServe(f.handler, caller, http.MethodDelete,
				"/api/v1/cluster-groups/2", "")
			assertOutOfTokenScope(t, rec)
			if got := groupName(t, 2); got != "Estate" {
				t.Errorf("Group changed to %q despite the refusal", got)
			}
		})
	}

	f.run(t, []clusterCovCase{
		{name: "update hidden group", caller: f.admin, method: http.MethodPut,
			path: "/api/v1/cluster-groups/3", body: rename,
			status: http.StatusNotFound, msg: "Cluster group not found"},
		{name: "update visibility fails", h: f.failing(t, 1), caller: f.admin,
			method: http.MethodPut, path: "/api/v1/cluster-groups/2", body: rename,
			status: http.StatusInternalServerError,
			msg:    "Failed to update cluster group"},
		{name: "update membership fails", h: f.failing(t, 2), caller: f.admin,
			method: http.MethodPut, path: "/api/v1/cluster-groups/2", body: rename,
			status: http.StatusInternalServerError,
			msg:    "Failed to update cluster group"},
		{name: "superuser updates hidden group", caller: f.super,
			method: http.MethodPut, path: "/api/v1/cluster-groups/3",
			body: `{"name":"Private renamed"}`, status: http.StatusOK,
			msg: "Private renamed"},
	})

	for _, caller := range []scopeCaller{f.admin, asToken(f.session),
		asToken(f.unscoped), asToken(f.wildcard)} {
		t.Run(caller.name+" update allowed", func(t *testing.T) {
			rec := clusterCovServe(f.handler, caller, http.MethodPut,
				"/api/v1/cluster-groups/2", `{"name":"By `+caller.name+`"}`)
			assertStatus(t, rec, http.StatusOK)
			if got := groupName(t, 2); got != "By "+caller.name {
				t.Errorf("Group name = %q, want %q", got, "By "+caller.name)
			}
		})
	}

	t.Run("wildcard delete allowed", func(t *testing.T) {
		rec := clusterCovServe(f.handler, asToken(f.wildcard), http.MethodDelete,
			"/api/v1/cluster-groups/3", "")
		assertStatus(t, rec, http.StatusNoContent)
	})
}

// TestClusterCreationRespectsTokenScope covers the connection-scope gate
// on POST /api/v1/clusters, POST /api/v1/cluster-groups/{id}/clusters and
// POST /api/v1/cluster-groups: a new cluster or group, and any
// connection later moved into it, inherits its group's settings, so a
// token whose connection scope does not cover every connection may not
// create one.
func TestClusterCreationRespectsTokenScope(t *testing.T) {
	f := newClusterCovFixture(t)

	// As in TestClusterGroupWritesRespectTokenScope, a token caller also
	// carries a session bearer so that it reaches the gates after the
	// session lookup on POST /cluster-groups.
	asToken := func(token scopeCaller) scopeCaller {
		return scopeCaller{name: token.name, wrap: func(r *http.Request) *http.Request {
			return token.wrap(f.admin.wrap(r))
		}}
	}

	for _, caller := range []scopeCaller{f.narrowed, f.readOnly} {
		t.Run(caller.name+" may not create a cluster", func(t *testing.T) {
			rec := clusterCovServe(f.handler, caller, http.MethodPost,
				"/api/v1/clusters", `{"name":"Refused","group_id":3}`)
			assertOutOfTokenScope(t, rec)
			f.expectValue(t, "clusters named", "Refused", "0")
		})
		t.Run(caller.name+" may not create a cluster in a group", func(t *testing.T) {
			rec := clusterCovServe(f.handler, caller, http.MethodPost,
				"/api/v1/cluster-groups/3/clusters", `{"name":"Refused"}`)
			assertOutOfTokenScope(t, rec)
			f.expectValue(t, "clusters named", "Refused", "0")
		})
		t.Run(caller.name+" may not create a group", func(t *testing.T) {
			rec := clusterCovServe(f.handler, asToken(caller), http.MethodPost,
				"/api/v1/cluster-groups", `{"name":"Refused"}`)
			assertOutOfTokenScope(t, rec)
			f.expectValue(t, "groups named", "Refused", "0")
		})
	}

	for _, caller := range []scopeCaller{f.session, f.unscoped, f.wildcard} {
		t.Run(caller.name+" creates clusters", func(t *testing.T) {
			name := "Top " + caller.name
			rec := clusterCovServe(f.handler, caller, http.MethodPost,
				"/api/v1/clusters", `{"name":"`+name+`","group_id":2}`)
			assertStatus(t, rec, http.StatusCreated)
			f.expectValue(t, "clusters named", name, "1")

			name = "Grouped " + caller.name
			rec = clusterCovServe(f.handler, caller, http.MethodPost,
				"/api/v1/cluster-groups/2/clusters", `{"name":"`+name+`"}`)
			assertStatus(t, rec, http.StatusCreated)
			f.expectValue(t, "clusters named", name, "1")
		})
		t.Run(caller.name+" creates a group", func(t *testing.T) {
			name := "Group " + caller.name
			rec := clusterCovServe(f.handler, asToken(caller), http.MethodPost,
				"/api/v1/cluster-groups", `{"name":"`+name+`"}`)
			assertStatus(t, rec, http.StatusCreated)
			f.expectValue(t, "groups named", name, "1")
		})
	}
}

// TestUpdateConnectionClusterMissingCluster covers a move into a cluster
// id that does not exist on PUT /api/v1/connections/{id}/cluster: it
// answers 404, as a hidden cluster does, rather than 500 from the
// foreign key.
func TestUpdateConnectionClusterMissingCluster(t *testing.T) {
	f := newClusterCovFixture(t)
	handler := f.connectionHandlerOn(f.pool)
	const body = `{"cluster_id":999,"membership_source":"manual"}`

	for _, caller := range []scopeCaller{f.admin, f.super, f.narrowed} {
		t.Run(caller.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.handleUpdateConnectionCluster(rec,
				newScopeRequest(caller, http.MethodPut, body), 5)
			assertError(t, rec, http.StatusNotFound, "Cluster not found")
			f.expectValue(t, "conn cluster", 5, "1")
		})
	}

	t.Run("missing connection", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.handleUpdateConnectionCluster(rec,
			newScopeRequest(f.super, http.MethodPut,
				`{"cluster_id":2,"membership_source":"manual"}`), 999)
		assertError(t, rec, http.StatusNotFound, "Connection not found")
	})
}

// TestAddServerToClusterRefusesUnknownRole covers the role check on
// POST /api/v1/clusters/{id}/servers, which writes connections.role as
// PUT /api/v1/connections/{id}/cluster does.
func TestAddServerToClusterRefusesUnknownRole(t *testing.T) {
	f := newClusterCovFixture(t)

	for _, role := range []string{"overlord", ""} {
		t.Run("role "+role, func(t *testing.T) {
			rec := clusterCovServe(f.handler, f.super, http.MethodPost,
				"/api/v1/clusters/2/servers",
				`{"connection_id":5,"role":"`+role+`"}`)
			assertError(t, rec, http.StatusBadRequest, "Invalid role")
			f.expectValue(t, "conn cluster", 5, "1")
			f.expectValue(t, "conn role", 5, "primary")
		})
	}

	t.Run("known role accepted", func(t *testing.T) {
		rec := clusterCovServe(f.handler, f.super, http.MethodPost,
			"/api/v1/clusters/2/servers",
			`{"connection_id":5,"role":"spock_node"}`)
		assertStatus(t, rec, http.StatusOK)
		f.expectValue(t, "conn role", 5, "spock_node")
	})
}
