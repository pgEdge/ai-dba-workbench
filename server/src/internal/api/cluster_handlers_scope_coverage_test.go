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
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Tests for the ClusterHandler endpoints in cluster_handlers.go, run
// against a real datastore so that each handler is exercised past its
// permission and token-scope gates: validation, visibility filtering,
// the datastore call and its error mapping.
//
// Datastore failures are injected through a second pool whose
// PrepareConn hook lets a fixed number of connection acquisitions
// through and fails the rest, so a test can pick which datastore call
// in a handler fails.
// =============================================================================

// clusterCovSchema holds every table and column the cluster handlers
// read or write, seeded with a small estate:
//
//   - group 1 (default) holds cluster 1, whose members are the shared
//     connections 5 and 6 and the private connection 7;
//   - group 2 holds the auto-detected Spock cluster 2 (member: shared
//     connection 9) and the empty cluster 4;
//   - group 3 holds cluster 3, whose only member is the private
//     connection 8;
//   - connection 10 is private and belongs to no cluster.
//
// A non-superuser therefore sees connections 5, 6 and 9, clusters 1 and
// 2, and groups 1 and 2.
const clusterCovSchema = `
DROP TABLE IF EXISTS cluster_node_relationships CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
CREATE SCHEMA IF NOT EXISTS metrics;
DROP TABLE IF EXISTS metrics.pg_stat_activity CASCADE;

CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    auto_group_key VARCHAR(255) UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    auto_cluster_key VARCHAR(255) UNIQUE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    replication_type VARCHAR(50),
    dismissed BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT DEFAULT '',
    host VARCHAR(255) NOT NULL,
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL,
    username VARCHAR(255),
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    membership_source VARCHAR(16) NOT NULL DEFAULT 'auto',
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    role VARCHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE cluster_node_relationships (
    id SERIAL PRIMARY KEY,
    cluster_id INTEGER NOT NULL,
    source_connection_id INTEGER NOT NULL,
    target_connection_id INTEGER NOT NULL,
    relationship_type VARCHAR(50) NOT NULL,
    is_auto_detected BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (cluster_id, source_connection_id, target_connection_id,
            relationship_type)
);

CREATE TABLE metrics.pg_stat_activity (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL
);

INSERT INTO cluster_groups (id, name, is_shared, is_default) VALUES
    (1, 'Default', TRUE, TRUE),
    (2, 'Estate', FALSE, FALSE),
    (3, 'Private', FALSE, FALSE);
SELECT setval(pg_get_serial_sequence('cluster_groups', 'id'), 100);

INSERT INTO clusters (id, group_id, auto_cluster_key, name, replication_type)
VALUES
    (1, 1, NULL, 'Alpha', 'spock'),
    (2, 2, 'spock:auto1', 'Spock one', 'spock'),
    (3, 3, NULL, 'Hidden', NULL),
    (4, 2, NULL, 'Empty', NULL);
SELECT setval(pg_get_serial_sequence('clusters', 'id'), 100);

INSERT INTO connections (id, name, host, database_name, is_shared,
                         owner_username, cluster_id, role) VALUES
    (5, 'five', 'db5.example.com', 'postgres', TRUE, 'someone-else', 1, 'primary'),
    (6, 'six', 'db6.example.com', 'postgres', TRUE, 'someone-else', 1, NULL),
    (7, 'seven', 'db7.example.com', 'postgres', FALSE, 'someone-else', 1, NULL),
    (8, 'eight', 'db8.example.com', 'postgres', FALSE, 'someone-else', 3, NULL),
    (9, 'nine', 'db9.example.com', 'postgres', TRUE, 'someone-else', 2, NULL),
    (10, 'ten', 'db10.example.com', 'postgres', FALSE, 'someone-else', NULL, NULL);
SELECT setval(pg_get_serial_sequence('connections', 'id'), 100);

INSERT INTO cluster_node_relationships (id, cluster_id, source_connection_id,
                                        target_connection_id,
                                        relationship_type) VALUES
    (1, 1, 5, 6, 'streams_from');
SELECT setval(pg_get_serial_sequence('cluster_node_relationships', 'id'), 100);

INSERT INTO metrics.pg_stat_activity (connection_id, collected_at)
    VALUES (5, NOW());
`

const clusterCovTeardown = `
DROP TABLE IF EXISTS cluster_node_relationships CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_activity CASCADE;
`

// errClusterCovInjected is the failure the injecting pool returns.
var errClusterCovInjected = errors.New("injected datastore failure")

// clusterCovFixture bundles the handler, its pool and the callers one
// test uses.
type clusterCovFixture struct {
	connStr string
	pool    *pgxpool.Pool
	store   *auth.AuthStore
	handler *ClusterHandler

	// admin is a non-superuser session holding manage_connections;
	// super is the same session with the superuser flag; plain is a
	// session user with no admin permission; incomplete is an API
	// token whose ID did not reach the context.
	admin, super, plain, incomplete scopeCaller

	// The superuser callers from scopedCallers.
	session, unscoped, wildcard, narrowed, readOnly scopeCaller
}

// newClusterCovFixture installs clusterCovSchema in the database named by
// TEST_AI_WORKBENCH_SERVER and builds a ClusterHandler over it.
func newClusterCovFixture(t *testing.T) *clusterCovFixture {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping cluster handler test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}
	if _, err := pool.Exec(ctx, clusterCovSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create test schema: %v", err)
	}

	_, store, cleanup := createTestRBACHandler(t)
	t.Cleanup(func() {
		cleanup()
		_, _ = pool.Exec(context.Background(), clusterCovTeardown)
		pool.Close()
	})

	f := &clusterCovFixture{
		connStr: connStr,
		pool:    pool,
		store:   store,
		handler: NewClusterHandler(database.NewTestDatastore(pool), store,
			auth.NewRBACChecker(store)),
	}

	adminID := setupUserWithPermission(t, store, "cov_cl_admin",
		auth.PermManageConnections)
	adminToken := clusterCovSessionToken(t, store, "cov_cl_admin")
	f.admin = scopeCaller{name: "admin", wrap: func(r *http.Request) *http.Request {
		return withUsername(withUser(withBearer(r, adminToken), adminID),
			"cov_cl_admin")
	}}
	f.super = scopeCaller{name: "super", wrap: func(r *http.Request) *http.Request {
		return withSuperuser(f.admin.wrap(r))
	}}

	plainID := newTestUser(t, store, "cov_cl_plain")
	plainToken := clusterCovSessionToken(t, store, "cov_cl_plain")
	f.plain = scopeCaller{name: "plain", wrap: func(r *http.Request) *http.Request {
		return withUsername(withUser(withBearer(r, plainToken), plainID),
			"cov_cl_plain")
	}}
	f.incomplete = scopeCaller{name: "incomplete", wrap: withIncompleteToken}

	f.session, f.unscoped, f.wildcard, f.narrowed, f.readOnly =
		scopedCallers(t, store)
	return f
}

// clusterCovSessionToken logs the user in and returns the session token.
func clusterCovSessionToken(t *testing.T, store *auth.AuthStore,
	username string) string {

	t.Helper()
	token, _, err := store.AuthenticateUser(username, "Password1234")
	if err != nil {
		t.Fatalf("AuthenticateUser %s: %v", username, err)
	}
	return token
}

// failing returns a ClusterHandler sharing the fixture's auth store whose
// datastore lets the first allow connection acquisitions through and
// fails every later one. Each pool Query, QueryRow, Exec or Begin is one
// acquisition; statements inside a transaction reuse the connection.
func (f *clusterCovFixture) failing(t *testing.T, allow int64) *ClusterHandler {
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

	return NewClusterHandler(database.NewTestDatastore(pool), f.store,
		auth.NewRBACChecker(f.store))
}

// clusterCovServe routes one request through the handler's registered
// routes, as the server does, with an identity auth wrapper.
func clusterCovServe(h *ClusterHandler, caller scopeCaller, method, path,
	body string) *httptest.ResponseRecorder {

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, func(next http.HandlerFunc) http.HandlerFunc {
		return next
	})

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, caller.wrap(req))
	return rec
}

// clusterCovCase is one request and the response it must receive.
type clusterCovCase struct {
	name   string
	h      *ClusterHandler // nil means the fixture's handler
	caller scopeCaller
	method string
	path   string
	body   string
	status int
	msg    string // a substring the body must contain; empty skips it
}

// run issues each case in order; later cases may depend on the writes
// of earlier ones.
func (f *clusterCovFixture) run(t *testing.T, cases []clusterCovCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.h
			if h == nil {
				h = f.handler
			}
			rec := clusterCovServe(h, tc.caller, tc.method, tc.path, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("Expected status %d, got %d: %s", tc.status,
					rec.Code, rec.Body.String())
			}
			if tc.msg != "" && !strings.Contains(rec.Body.String(), tc.msg) {
				t.Errorf("Expected body to contain %q, got %s", tc.msg,
					rec.Body.String())
			}
		})
	}
}

// clusterCovQueries holds the fixed read-back queries the tests use, so
// that no SQL is built at run time.
var clusterCovQueries = map[string]string{
	"cluster name":      "SELECT name FROM clusters WHERE id = $1",
	"cluster dismissed": "SELECT dismissed::text FROM clusters WHERE id = $1",
	"cluster exists":    "SELECT EXISTS (SELECT 1 FROM clusters WHERE id = $1)::text",
	"cluster by key":    "SELECT name FROM clusters WHERE auto_cluster_key = $1",
	"conn cluster":      "SELECT COALESCE(cluster_id::text, 'none') FROM connections WHERE id = $1",
	"conn role":         "SELECT COALESCE(role, 'none') FROM connections WHERE id = $1",
	"relationship":      "SELECT EXISTS (SELECT 1 FROM cluster_node_relationships WHERE id = $1)::text",
	"group by key":      "SELECT name FROM cluster_groups WHERE auto_group_key = $1",
	"relationship pair": "SELECT count(*)::text FROM cluster_node_relationships WHERE source_connection_id = $1",
}

// readBack runs one of clusterCovQueries and returns its single text value.
func (f *clusterCovFixture) readBack(t *testing.T, query string, arg any) string {
	t.Helper()
	sql, ok := clusterCovQueries[query]
	if !ok {
		t.Fatalf("readBack has no query %q", query)
	}
	var value string
	if err := f.pool.QueryRow(context.Background(), sql, arg).Scan(&value); err != nil {
		t.Fatalf("Read back %q: %v", query, err)
	}
	return value
}

// expectValue fails unless the read-back value matches want.
func (f *clusterCovFixture) expectValue(t *testing.T, query string, arg any,
	want string) {

	t.Helper()
	if got := f.readBack(t, query, arg); got != want {
		t.Errorf("%s(%v) = %q, want %q", query, arg, got, want)
	}
}

// TestClusterCov_UpdateCluster covers PUT /api/v1/clusters/{id}.
func TestClusterCov_UpdateCluster(t *testing.T) {
	f := newClusterCovFixture(t)
	const denied = "Permission denied: requires manage_connections permission"

	f.run(t, []clusterCovCase{
		{name: "no manage_connections", caller: f.plain, method: http.MethodPut,
			path: "/api/v1/clusters/1", body: `{"name":"Renamed"}`,
			status: http.StatusForbidden, msg: denied},
		{name: "narrowed token", caller: f.narrowed, method: http.MethodPut,
			path: "/api/v1/clusters/1", body: `{"name":"Renamed"}`,
			status: http.StatusForbidden, msg: targetOutOfTokenScope},
		{name: "malformed body", caller: f.super, method: http.MethodPut,
			path: "/api/v1/clusters/1", body: `{bad`,
			status: http.StatusBadRequest, msg: "Invalid request body"},
		{name: "no fields", caller: f.super, method: http.MethodPut,
			path: "/api/v1/clusters/1", body: `{}`,
			status: http.StatusBadRequest, msg: "At least name"},
		{name: "invalid name", caller: f.super, method: http.MethodPut,
			path: "/api/v1/clusters/1", body: `{"name":"bad<name>"}`,
			status: http.StatusBadRequest},
		{name: "visibility lookup fails", h: f.failing(t, 0), caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/1",
			body: `{"name":"Renamed"}`, status: http.StatusInternalServerError,
			msg: "Failed to update cluster"},
		{name: "membership lookup fails", h: f.failing(t, 1), caller: f.admin,
			method: http.MethodPut, path: "/api/v1/clusters/1",
			body: `{"name":"Renamed"}`, status: http.StatusInternalServerError,
			msg: "Failed to update cluster"},
		{name: "cluster not visible", caller: f.admin, method: http.MethodPut,
			path: "/api/v1/clusters/3", body: `{"name":"Renamed"}`,
			status: http.StatusNotFound, msg: "Cluster not found"},
		{name: "missing cluster", caller: f.super, method: http.MethodPut,
			path: "/api/v1/clusters/999", body: `{"name":"Renamed"}`,
			status: http.StatusInternalServerError, msg: "Failed to update cluster"},
		{name: "visible cluster renamed", caller: f.admin, method: http.MethodPut,
			path: "/api/v1/clusters/1", body: `{"name":"Renamed"}`,
			status: http.StatusOK, msg: "Renamed"},
		{name: "prefixed id", caller: f.wildcard, method: http.MethodPut,
			path:   "/api/v1/clusters/cluster-3",
			body:   `{"description":"d","replication_type":"binary","group_id":2}`,
			status: http.StatusOK, msg: "binary"},
	})

	f.expectValue(t, "cluster name", 1, "Renamed")
	f.expectValue(t, "cluster name", 3, "Hidden")
}

// TestClusterCov_DeleteCluster covers DELETE /api/v1/clusters/{id}.
func TestClusterCov_DeleteCluster(t *testing.T) {
	f := newClusterCovFixture(t)

	f.run(t, []clusterCovCase{
		{name: "no manage_connections", caller: f.plain, method: http.MethodDelete,
			path: "/api/v1/clusters/1", status: http.StatusForbidden},
		{name: "narrowed token", caller: f.narrowed, method: http.MethodDelete,
			path: "/api/v1/clusters/1", status: http.StatusForbidden,
			msg: targetOutOfTokenScope},
		{name: "visibility lookup fails", h: f.failing(t, 0), caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/clusters/1",
			status: http.StatusInternalServerError, msg: "Failed to delete cluster"},
		{name: "membership lookup fails", h: f.failing(t, 1), caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/clusters/1",
			status: http.StatusInternalServerError, msg: "Failed to delete cluster"},
		{name: "cluster not visible", caller: f.admin, method: http.MethodDelete,
			path: "/api/v1/clusters/3", status: http.StatusNotFound,
			msg: "Cluster not found"},
		{name: "missing cluster", caller: f.super, method: http.MethodDelete,
			path: "/api/v1/clusters/999", status: http.StatusNotFound,
			msg: "Cluster not found"},
		{name: "dismiss fails", h: f.failing(t, 1), caller: f.super,
			method: http.MethodDelete, path: "/api/v1/clusters/2",
			status: http.StatusInternalServerError, msg: "Failed to delete cluster"},
		{name: "auto-detected cluster dismissed", caller: f.unscoped,
			method: http.MethodDelete, path: "/api/v1/clusters/2",
			status: http.StatusNoContent},
		{name: "manual cluster deleted", caller: f.session,
			method: http.MethodDelete, path: "/api/v1/clusters/4",
			status: http.StatusNoContent},
		{name: "visible cluster deleted", caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/clusters/1",
			status: http.StatusNoContent},
	})

	f.expectValue(t, "cluster dismissed", 2, "true")
	f.expectValue(t, "conn cluster", 9, "none")
	f.expectValue(t, "cluster exists", 4, "false")
	f.expectValue(t, "cluster exists", 1, "false")
	f.expectValue(t, "cluster exists", 3, "true")
}

// TestClusterCov_UpdateAutoDetectedCluster covers PUT on the
// server-{id} and cluster-spock-{prefix} topology IDs.
func TestClusterCov_UpdateAutoDetectedCluster(t *testing.T) {
	f := newClusterCovFixture(t)
	const path = "/api/v1/clusters/cluster-spock-auto1"

	f.run(t, []clusterCovCase{
		{name: "no manage_connections", caller: f.plain, method: http.MethodPut,
			path: path, body: `{"name":"Renamed"}`, status: http.StatusForbidden,
			msg: "modify auto-detected clusters"},
		{name: "narrowed token", caller: f.narrowed, method: http.MethodPut,
			path: path, body: `{"name":"Renamed"}`, status: http.StatusForbidden,
			msg: targetOutOfTokenScope},
		{name: "malformed body", caller: f.super, method: http.MethodPut,
			path: path, body: `{bad`, status: http.StatusBadRequest},
		{name: "no fields", caller: f.super, method: http.MethodPut,
			path: path, body: `{}`, status: http.StatusBadRequest,
			msg: "At least name, description, or group_id is required"},
		{name: "invalid name", caller: f.super, method: http.MethodPut,
			path: path, body: `{"name":"bad<name>"}`,
			status: http.StatusBadRequest},
		{name: "new key without a name", caller: f.super, method: http.MethodPut,
			path: "/api/v1/clusters/server-10", body: `{"description":"d"}`,
			status: http.StatusInternalServerError,
			msg:    "Failed to update auto-detected cluster"},
		{name: "existing key renamed", caller: f.super, method: http.MethodPut,
			path: path, body: `{"name":"Renamed","description":"d","group_id":1}`,
			status: http.StatusOK, msg: "Renamed"},
		{name: "new key created", caller: f.wildcard, method: http.MethodPut,
			path: "/api/v1/clusters/server-10", body: `{"name":"Standalone ten"}`,
			status: http.StatusOK, msg: "standalone:10"},
		{name: "explicit key", caller: f.session, method: http.MethodPut,
			path:   "/api/v1/clusters/server-11",
			body:   `{"name":"Binary eleven","auto_cluster_key":"binary:11"}`,
			status: http.StatusOK, msg: "binary:11"},
		{name: "method not allowed", caller: f.super, method: http.MethodGet,
			path: path, status: http.StatusMethodNotAllowed},
	})

	f.expectValue(t, "cluster by key", "spock:auto1", "Renamed")
	f.expectValue(t, "cluster by key", "standalone:10", "Standalone ten")

	// The router only forwards IDs with a known prefix, so the empty-key
	// branch is reached by calling the handler directly.
	t.Run("unknown cluster type", func(t *testing.T) {
		rec := httptest.NewRecorder()
		f.handler.updateAutoDetectedCluster(rec,
			newScopeRequest(f.session, http.MethodPut, `{"name":"Renamed"}`),
			"binary-1")
		assertError(t, rec, http.StatusBadRequest,
			"auto_cluster_key is required for this cluster type")
	})
}

// TestClusterCov_DeleteAutoDetectedCluster covers DELETE on the
// server-{id} and cluster-spock-{prefix} topology IDs.
func TestClusterCov_DeleteAutoDetectedCluster(t *testing.T) {
	f := newClusterCovFixture(t)

	f.run(t, []clusterCovCase{
		{name: "no manage_connections", caller: f.plain,
			method: http.MethodDelete, path: "/api/v1/clusters/cluster-spock-auto1",
			status: http.StatusForbidden, msg: "delete auto-detected clusters"},
		{name: "narrowed token", caller: f.narrowed, method: http.MethodDelete,
			path:   "/api/v1/clusters/cluster-spock-auto1",
			status: http.StatusForbidden, msg: targetOutOfTokenScope},
		{name: "spock dismiss fails", h: f.failing(t, 0), caller: f.super,
			method: http.MethodDelete, path: "/api/v1/clusters/cluster-spock-auto1",
			status: http.StatusInternalServerError,
			msg:    "Failed to delete auto-detected cluster"},
		{name: "server dismiss fails", h: f.failing(t, 0), caller: f.super,
			method: http.MethodDelete, path: "/api/v1/clusters/server-5",
			status: http.StatusInternalServerError,
			msg:    "Failed to delete auto-detected cluster"},
		{name: "existing spock cluster", caller: f.super,
			method: http.MethodDelete, path: "/api/v1/clusters/cluster-spock-auto1",
			status: http.StatusNoContent},
		{name: "unseen spock cluster", caller: f.unscoped,
			method: http.MethodDelete, path: "/api/v1/clusters/cluster-spock-other",
			status: http.StatusNoContent},
		{name: "server cluster", caller: f.wildcard,
			method: http.MethodDelete, path: "/api/v1/clusters/server-10",
			status: http.StatusNoContent},
	})

	f.expectValue(t, "cluster dismissed", 2, "true")
	f.expectValue(t, "conn cluster", 9, "none")
	f.expectValue(t, "cluster by key", "spock:other", "other Spock")

	t.Run("unknown cluster type", func(t *testing.T) {
		rec := httptest.NewRecorder()
		f.handler.deleteAutoDetectedCluster(rec,
			newScopeRequest(f.session, http.MethodDelete, ""), "binary-1")
		assertError(t, rec, http.StatusBadRequest,
			"Invalid auto-detected cluster ID")
	})
}

// TestClusterCov_UpdateAutoDetectedGroup covers PUT
// /api/v1/cluster-groups/group-auto.
func TestClusterCov_UpdateAutoDetectedGroup(t *testing.T) {
	f := newClusterCovFixture(t)
	const path = "/api/v1/cluster-groups/group-auto"

	f.run(t, []clusterCovCase{
		{name: "no manage_connections", caller: f.plain, method: http.MethodPut,
			path: path, body: `{"name":"Renamed"}`, status: http.StatusForbidden,
			msg: "rename auto-detected groups"},
		{name: "narrowed token", caller: f.narrowed, method: http.MethodPut,
			path: path, body: `{"name":"Renamed"}`, status: http.StatusForbidden,
			msg: targetOutOfTokenScope},
		{name: "malformed body", caller: f.super, method: http.MethodPut,
			path: path, body: `{bad`, status: http.StatusBadRequest},
		{name: "invalid name", caller: f.super, method: http.MethodPut,
			path: path, body: `{"name":"bad<name>"}`,
			status: http.StatusBadRequest},
		{name: "upsert fails", h: f.failing(t, 0), caller: f.super,
			method: http.MethodPut, path: path, body: `{"name":"Renamed"}`,
			status: http.StatusInternalServerError,
			msg:    "Failed to update auto-detected group"},
		{name: "group created", caller: f.super, method: http.MethodPut,
			path: path, body: `{"name":"Auto group"}`, status: http.StatusOK,
			msg: "Auto group"},
		{name: "group renamed", caller: f.unscoped, method: http.MethodPut,
			path: path, body: `{"name":"Renamed"}`, status: http.StatusOK,
			msg: "Renamed"},
		{name: "method not allowed", caller: f.super, method: http.MethodGet,
			path: path, status: http.StatusMethodNotAllowed},
	})

	f.expectValue(t, "group by key", "auto", "Renamed")

	t.Run("empty key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		f.handler.updateAutoDetectedGroup(rec,
			newScopeRequest(f.session, http.MethodPut, `{"name":"Renamed"}`),
			"group-")
		assertError(t, rec, http.StatusBadRequest,
			"Invalid auto-detected group ID")
	})
}

// TestClusterCov_AddServerToCluster covers POST
// /api/v1/clusters/{id}/servers.
func TestClusterCov_AddServerToCluster(t *testing.T) {
	f := newClusterCovFixture(t)

	f.run(t, []clusterCovCase{
		{name: "no manage_connections", caller: f.plain, method: http.MethodPost,
			path: "/api/v1/clusters/1/servers", body: `{"connection_id":5}`,
			status: http.StatusForbidden},
		{name: "malformed body", caller: f.super, method: http.MethodPost,
			path: "/api/v1/clusters/1/servers", body: `{bad`,
			status: http.StatusBadRequest},
		{name: "missing connection", caller: f.super, method: http.MethodPost,
			path: "/api/v1/clusters/1/servers", body: `{"connection_id":0}`,
			status: http.StatusBadRequest, msg: "A valid connection_id is required"},
		{name: "read-only token", caller: f.readOnly, method: http.MethodPost,
			path: "/api/v1/clusters/1/servers", body: `{"connection_id":5}`,
			status: http.StatusForbidden, msg: targetOutOfTokenScope},
		{name: "narrowed token, other connection", caller: f.narrowed,
			method: http.MethodPost, path: "/api/v1/clusters/1/servers",
			body: `{"connection_id":6}`, status: http.StatusForbidden,
			msg: targetOutOfTokenScope},
		{name: "visibility lookup fails", h: f.failing(t, 0), caller: f.admin,
			method: http.MethodPost, path: "/api/v1/clusters/1/servers",
			body: `{"connection_id":5}`, status: http.StatusInternalServerError,
			msg: "Failed to add server to cluster"},
		{name: "membership lookup fails", h: f.failing(t, 0), caller: f.narrowed,
			method: http.MethodPost, path: "/api/v1/clusters/1/servers",
			body: `{"connection_id":5}`, status: http.StatusInternalServerError,
			msg: "Failed to add server to cluster"},
		{name: "cluster not visible", caller: f.narrowed, method: http.MethodPost,
			path: "/api/v1/clusters/3/servers", body: `{"connection_id":5}`,
			status: http.StatusNotFound, msg: "Cluster not found"},
		{name: "connection not visible", caller: f.admin, method: http.MethodPost,
			path: "/api/v1/clusters/1/servers", body: `{"connection_id":10}`,
			status: http.StatusNotFound, msg: "Connection not found"},
		{name: "missing cluster", caller: f.super, method: http.MethodPost,
			path: "/api/v1/clusters/999/servers", body: `{"connection_id":5}`,
			status: http.StatusNotFound, msg: "Cluster not found"},
		{name: "missing connection row", caller: f.super, method: http.MethodPost,
			path: "/api/v1/clusters/1/servers", body: `{"connection_id":999}`,
			status: http.StatusNotFound, msg: "Connection not found"},
		{name: "datastore fails", h: f.failing(t, 0), caller: f.super,
			method: http.MethodPost, path: "/api/v1/clusters/1/servers",
			body: `{"connection_id":5}`, status: http.StatusInternalServerError,
			msg: "Failed to add server to cluster"},
		{name: "narrowed token adds its connection", caller: f.narrowed,
			method: http.MethodPost, path: "/api/v1/clusters/1/servers",
			body: `{"connection_id":7,"role":"replica"}`, status: http.StatusOK,
			msg: "replica"},
		{name: "superuser adds any connection", caller: f.super,
			method: http.MethodPost, path: "/api/v1/clusters/2/servers",
			body: `{"connection_id":10}`, status: http.StatusOK},
	})

	f.expectValue(t, "conn role", 7, "replica")
	f.expectValue(t, "conn cluster", 10, "2")
}

// TestClusterCov_RemoveServerFromCluster covers DELETE
// /api/v1/clusters/{id}/servers/{connectionId}.
func TestClusterCov_RemoveServerFromCluster(t *testing.T) {
	f := newClusterCovFixture(t)

	f.run(t, []clusterCovCase{
		{name: "method not allowed", caller: f.super, method: http.MethodGet,
			path:   "/api/v1/clusters/1/servers/6",
			status: http.StatusMethodNotAllowed},
		{name: "no manage_connections", caller: f.plain,
			method: http.MethodDelete, path: "/api/v1/clusters/1/servers/6",
			status: http.StatusForbidden},
		{name: "read-only token", caller: f.readOnly, method: http.MethodDelete,
			path: "/api/v1/clusters/1/servers/5", status: http.StatusForbidden,
			msg: targetOutOfTokenScope},
		{name: "visibility lookup fails", h: f.failing(t, 0), caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/clusters/1/servers/6",
			status: http.StatusInternalServerError,
			msg:    "Failed to remove server from cluster"},
		{name: "membership lookup fails", h: f.failing(t, 0), caller: f.narrowed,
			method: http.MethodDelete, path: "/api/v1/clusters/1/servers/5",
			status: http.StatusInternalServerError,
			msg:    "Failed to remove server from cluster"},
		{name: "cluster not visible", caller: f.narrowed,
			method: http.MethodDelete, path: "/api/v1/clusters/3/servers/5",
			status: http.StatusNotFound, msg: "Cluster not found"},
		{name: "connection not in cluster", caller: f.super,
			method: http.MethodDelete, path: "/api/v1/clusters/1/servers/9",
			status: http.StatusNotFound,
			msg:    "Connection not found in this cluster"},
		{name: "transaction fails", h: f.failing(t, 1), caller: f.super,
			method: http.MethodDelete, path: "/api/v1/clusters/1/servers/6",
			status: http.StatusInternalServerError,
			msg:    "Failed to remove server from cluster"},
		{name: "visible connection removed", caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/clusters/1/servers/6",
			status: http.StatusNoContent},
	})

	f.expectValue(t, "conn cluster", 6, "none")
	f.expectValue(t, "relationship", 1, "false")
}

// TestClusterCov_SetConnectionRelationships covers PUT
// /api/v1/clusters/{id}/connections/{connId}/relationships.
func TestClusterCov_SetConnectionRelationships(t *testing.T) {
	f := newClusterCovFixture(t)
	const path = "/api/v1/clusters/1/connections/5/relationships"
	const streams = `{"relationships":[{"target_connection_id":6,` +
		`"relationship_type":"streams_from"}]}`
	const replicates = `{"relationships":[{"target_connection_id":7,` +
		`"relationship_type":"replicates_with"}]}`

	f.run(t, []clusterCovCase{
		{name: "no manage_connections", caller: f.plain, method: http.MethodPut,
			path: path, body: streams, status: http.StatusForbidden},
		{name: "malformed body", caller: f.super, method: http.MethodPut,
			path: path, body: `{bad`, status: http.StatusBadRequest},
		{name: "narrowed token", caller: f.narrowed, method: http.MethodPut,
			path: path, body: streams, status: http.StatusForbidden,
			msg: targetOutOfTokenScope},
		{name: "source lookup fails", h: f.failing(t, 0), caller: f.super,
			method: http.MethodPut, path: path, body: streams,
			status: http.StatusInternalServerError,
			msg:    "Failed to validate cluster membership"},
		{name: "source outside cluster", caller: f.super, method: http.MethodPut,
			path: "/api/v1/clusters/1/connections/8/relationships", body: streams,
			status: http.StatusBadRequest,
			msg:    "Source connection does not belong to this cluster"},
		{name: "invalid type", caller: f.super, method: http.MethodPut,
			path: path,
			body: `{"relationships":[{"target_connection_id":6,` +
				`"relationship_type":"bogus"}]}`,
			status: http.StatusBadRequest, msg: "Invalid relationship type"},
		{name: "self relationship", caller: f.super, method: http.MethodPut,
			path: path,
			body: `{"relationships":[{"target_connection_id":5,` +
				`"relationship_type":"streams_from"}]}`,
			status: http.StatusBadRequest,
			msg:    "Self-relationships are not allowed"},
		{name: "target lookup fails", h: f.failing(t, 1), caller: f.super,
			method: http.MethodPut, path: path, body: streams,
			status: http.StatusInternalServerError,
			msg:    "Failed to validate cluster membership"},
		{name: "target outside cluster", caller: f.super, method: http.MethodPut,
			path: path,
			body: `{"relationships":[{"target_connection_id":8,` +
				`"relationship_type":"streams_from"}]}`,
			status: http.StatusBadRequest,
			msg:    "Target connection does not belong to this cluster"},
		{name: "save fails", h: f.failing(t, 2), caller: f.super,
			method: http.MethodPut, path: path, body: streams,
			status: http.StatusInternalServerError,
			msg:    "Failed to set relationships"},
		// Saving succeeds, the reverse-row lookup fails and is skipped,
		// then the final read fails.
		{name: "reverse lookup and read back fail", h: f.failing(t, 3),
			caller: f.super, method: http.MethodPut, path: path,
			body: replicates, status: http.StatusInternalServerError,
			msg: "Relationships saved but failed to retrieve updated list"},
		// Saving and the reverse-row lookup succeed, then writing the
		// reverse row fails and is skipped.
		{name: "reverse write fails", h: f.failing(t, 4), caller: f.super,
			method: http.MethodPut,
			path:   "/api/v1/clusters/1/connections/6/relationships",
			body: `{"relationships":[{"target_connection_id":7,` +
				`"relationship_type":"replicates_with"}]}`,
			status: http.StatusInternalServerError,
			msg:    "Relationships saved but failed to retrieve updated list"},
		{name: "replicates_with creates the reverse row", caller: f.super,
			method: http.MethodPut, path: path, body: replicates,
			status: http.StatusOK, msg: "replicates_with"},
		{name: "reverse row already present", caller: f.unscoped,
			method: http.MethodPut, path: path, body: replicates,
			status: http.StatusOK, msg: "replicates_with"},
		{name: "empty set on an empty cluster", caller: f.wildcard,
			method: http.MethodPut,
			path:   "/api/v1/clusters/2/connections/9/relationships",
			body:   `{"relationships":[]}`, status: http.StatusOK, msg: "[]"},
	})

	f.expectValue(t, "relationship pair", 7, "1")
	f.expectValue(t, "relationship pair", 5, "1")
}

// TestClusterCov_DeleteRelationship covers DELETE
// /api/v1/clusters/{id}/relationships/{relationshipId}.
func TestClusterCov_DeleteRelationship(t *testing.T) {
	f := newClusterCovFixture(t)
	const path = "/api/v1/clusters/1/relationships/1"

	f.run(t, []clusterCovCase{
		{name: "method not allowed", caller: f.super, method: http.MethodPut,
			path: path, status: http.StatusMethodNotAllowed},
		{name: "no manage_connections", caller: f.plain,
			method: http.MethodDelete, path: path, status: http.StatusForbidden},
		{name: "narrowed token", caller: f.narrowed, method: http.MethodDelete,
			path: path, status: http.StatusForbidden, msg: targetOutOfTokenScope},
		{name: "missing relationship", caller: f.super,
			method: http.MethodDelete, path: "/api/v1/clusters/1/relationships/999",
			status: http.StatusNotFound, msg: "Relationship not found"},
		{name: "relationship deleted", caller: f.wildcard,
			method: http.MethodDelete, path: path, status: http.StatusNoContent},
	})

	f.expectValue(t, "relationship", 1, "false")
}

// TestClusterCov_ClearConnectionRelationships covers DELETE
// /api/v1/clusters/{id}/connections/{connId}/relationships.
func TestClusterCov_ClearConnectionRelationships(t *testing.T) {
	f := newClusterCovFixture(t)
	const path = "/api/v1/clusters/1/connections/5/relationships"

	f.run(t, []clusterCovCase{
		{name: "method not allowed", caller: f.super, method: http.MethodGet,
			path: path, status: http.StatusMethodNotAllowed},
		{name: "no manage_connections", caller: f.plain,
			method: http.MethodDelete, path: path, status: http.StatusForbidden},
		{name: "narrowed token", caller: f.narrowed, method: http.MethodDelete,
			path: path, status: http.StatusForbidden, msg: targetOutOfTokenScope},
		{name: "clear fails", h: f.failing(t, 0), caller: f.super,
			method: http.MethodDelete, path: path,
			status: http.StatusInternalServerError,
			msg:    "Failed to clear relationships"},
		{name: "relationships cleared", caller: f.session,
			method: http.MethodDelete, path: path, status: http.StatusNoContent},
	})

	f.expectValue(t, "relationship", 1, "false")
}

// TestClusterCov_ClusterReads covers the cluster read endpoints: the
// topology, the autocomplete list, a single cluster, its servers and its
// relationships, for unrestricted and restricted callers.
func TestClusterCov_ClusterReads(t *testing.T) {
	f := newClusterCovFixture(t)

	f.run(t, []clusterCovCase{
		// Topology.
		{name: "topology visibility fails", h: f.failing(t, 0), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters",
			status: http.StatusInternalServerError,
			msg:    "Failed to filter cluster topology"},
		{name: "topology for a caller who sees nothing", caller: f.incomplete,
			method: http.MethodGet, path: "/api/v1/clusters",
			status: http.StatusOK, msg: "[]"},
		{name: "topology read fails", h: f.failing(t, 1), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters",
			status: http.StatusInternalServerError,
			msg:    "Failed to get cluster topology"},
		{name: "topology method not allowed", caller: f.super,
			method: http.MethodPatch, path: "/api/v1/clusters",
			status: http.StatusMethodNotAllowed},

		// Autocomplete list.
		{name: "list method not allowed", caller: f.super,
			method: http.MethodPost, path: "/api/v1/clusters/list",
			status: http.StatusMethodNotAllowed},
		{name: "list read fails", h: f.failing(t, 0), caller: f.super,
			method: http.MethodGet, path: "/api/v1/clusters/list",
			status: http.StatusInternalServerError, msg: "Failed to list clusters"},
		{name: "list visibility fails", h: f.failing(t, 1), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters/list",
			status: http.StatusInternalServerError, msg: "Failed to list clusters"},
		{name: "list membership fails", h: f.failing(t, 2), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters/list",
			status: http.StatusInternalServerError, msg: "Failed to list clusters"},
		{name: "list for a restricted caller", caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters/list",
			status: http.StatusOK, msg: "Spock one"},
		{name: "list for a superuser", caller: f.super,
			method: http.MethodGet, path: "/api/v1/clusters/list",
			status: http.StatusOK, msg: "Hidden"},

		// A single cluster.
		{name: "missing cluster", caller: f.super, method: http.MethodGet,
			path: "/api/v1/clusters/999", status: http.StatusNotFound},
		{name: "cluster visibility fails", h: f.failing(t, 1), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters/1",
			status: http.StatusInternalServerError, msg: "Failed to load cluster"},
		{name: "cluster membership fails", h: f.failing(t, 2), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters/1",
			status: http.StatusInternalServerError, msg: "Failed to load cluster"},
		{name: "hidden cluster", caller: f.admin, method: http.MethodGet,
			path: "/api/v1/clusters/3", status: http.StatusNotFound},
		{name: "visible cluster", caller: f.admin, method: http.MethodGet,
			path: "/api/v1/clusters/cluster-1", status: http.StatusOK,
			msg: "Alpha"},
		{name: "cluster for a superuser", caller: f.super,
			method: http.MethodGet, path: "/api/v1/clusters/3",
			status: http.StatusOK, msg: "Hidden"},

		// Servers in a cluster.
		{name: "servers method not allowed", caller: f.super,
			method: http.MethodPut, path: "/api/v1/clusters/1/servers",
			status: http.StatusMethodNotAllowed},
		{name: "servers read fails", h: f.failing(t, 0), caller: f.super,
			method: http.MethodGet, path: "/api/v1/clusters/1/servers",
			status: http.StatusInternalServerError, msg: "Failed to list servers"},
		{name: "servers visibility fails", h: f.failing(t, 1), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters/1/servers",
			status: http.StatusInternalServerError, msg: "Failed to list servers"},
		{name: "servers membership fails", h: f.failing(t, 2), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters/1/servers",
			status: http.StatusInternalServerError, msg: "Failed to list servers"},
		{name: "servers of a hidden cluster", caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters/3/servers",
			status: http.StatusNotFound},
		{name: "servers for a restricted caller", caller: f.admin,
			method: http.MethodGet, path: "/api/v1/clusters/1/servers",
			status: http.StatusOK, msg: "online"},
		{name: "servers for a superuser", caller: f.super,
			method: http.MethodGet, path: "/api/v1/clusters/1/servers",
			status: http.StatusOK, msg: "seven"},

		// Relationships in a cluster.
		{name: "relationships method not allowed", caller: f.super,
			method: http.MethodPost, path: "/api/v1/clusters/1/relationships",
			status: http.StatusMethodNotAllowed},
		{name: "relationships read fails", h: f.failing(t, 0), caller: f.super,
			method: http.MethodGet, path: "/api/v1/clusters/1/relationships",
			status: http.StatusInternalServerError,
			msg:    "Failed to get cluster relationships"},
		{name: "relationships listed", caller: f.super,
			method: http.MethodGet, path: "/api/v1/clusters/1/relationships",
			status: http.StatusOK, msg: "streams_from"},
		{name: "no relationships", caller: f.super,
			method: http.MethodGet, path: "/api/v1/clusters/2/relationships",
			status: http.StatusOK, msg: "[]"},
	})
}

// TestClusterCov_CreateCluster covers POST /api/v1/clusters.
func TestClusterCov_CreateCluster(t *testing.T) {
	f := newClusterCovFixture(t)

	f.run(t, []clusterCovCase{
		{name: "malformed body", caller: f.super, method: http.MethodPost,
			path: "/api/v1/clusters", body: `{bad`,
			status: http.StatusBadRequest},
		{name: "invalid name", caller: f.super, method: http.MethodPost,
			path: "/api/v1/clusters", body: `{"name":"bad<name>"}`,
			status: http.StatusBadRequest},
		{name: "create fails", h: f.failing(t, 0), caller: f.super,
			method: http.MethodPost, path: "/api/v1/clusters",
			body: `{"name":"Manual"}`, status: http.StatusInternalServerError,
			msg: "Failed to create cluster"},
		{name: "created in the default group", caller: f.super,
			method: http.MethodPost, path: "/api/v1/clusters",
			body:   `{"name":"Manual","replication_type":"binary"}`,
			status: http.StatusCreated, msg: "Manual"},
		{name: "created in a named group", caller: f.super,
			method: http.MethodPost, path: "/api/v1/clusters",
			body:   `{"name":"Grouped","group_id":2}`,
			status: http.StatusCreated, msg: "Grouped"},
	})
}

// TestClusterCov_GroupEndpoints covers the cluster-group read and
// delete endpoints and the clusters-in-group sub-resource.
func TestClusterCov_GroupEndpoints(t *testing.T) {
	f := newClusterCovFixture(t)

	f.run(t, []clusterCovCase{
		// Listing groups.
		{name: "groups method not allowed", caller: f.super,
			method: http.MethodPatch, path: "/api/v1/cluster-groups",
			status: http.StatusMethodNotAllowed},
		{name: "groups read fails", h: f.failing(t, 0), caller: f.super,
			method: http.MethodGet, path: "/api/v1/cluster-groups",
			status: http.StatusInternalServerError,
			msg:    "Failed to list cluster groups"},
		{name: "groups visibility fails", h: f.failing(t, 1), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/cluster-groups",
			status: http.StatusInternalServerError,
			msg:    "Failed to filter cluster groups"},
		{name: "groups filter fails", h: f.failing(t, 2), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/cluster-groups",
			status: http.StatusInternalServerError,
			msg:    "Failed to filter cluster groups"},
		{name: "groups for a restricted caller", caller: f.admin,
			method: http.MethodGet, path: "/api/v1/cluster-groups",
			status: http.StatusOK, msg: "Estate"},
		{name: "groups for a superuser", caller: f.super,
			method: http.MethodGet, path: "/api/v1/cluster-groups",
			status: http.StatusOK, msg: "Private"},

		// A single group.
		{name: "missing group", caller: f.super, method: http.MethodGet,
			path: "/api/v1/cluster-groups/999", status: http.StatusNotFound},
		{name: "group visibility fails", h: f.failing(t, 1), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/cluster-groups/1",
			status: http.StatusInternalServerError,
			msg:    "Failed to load cluster group"},
		{name: "group membership fails", h: f.failing(t, 2), caller: f.admin,
			method: http.MethodGet, path: "/api/v1/cluster-groups/1",
			status: http.StatusInternalServerError,
			msg:    "Failed to load cluster group"},
		{name: "hidden group", caller: f.admin, method: http.MethodGet,
			path: "/api/v1/cluster-groups/3", status: http.StatusNotFound},
		{name: "visible group", caller: f.admin, method: http.MethodGet,
			path: "/api/v1/cluster-groups/1", status: http.StatusOK,
			msg: "Default"},
		{name: "group for a superuser", caller: f.super,
			method: http.MethodGet, path: "/api/v1/cluster-groups/3",
			status: http.StatusOK, msg: "Private"},
		{name: "group method not allowed", caller: f.super,
			method: http.MethodPatch, path: "/api/v1/cluster-groups/1",
			status: http.StatusMethodNotAllowed},

		// Clusters in a group.
		{name: "group clusters method not allowed", caller: f.super,
			method: http.MethodPut, path: "/api/v1/cluster-groups/2/clusters",
			status: http.StatusMethodNotAllowed},
		{name: "group clusters read fails", h: f.failing(t, 0), caller: f.super,
			method: http.MethodGet, path: "/api/v1/cluster-groups/2/clusters",
			status: http.StatusInternalServerError, msg: "Failed to list clusters"},
		{name: "group clusters visibility fails", h: f.failing(t, 1),
			caller: f.admin, method: http.MethodGet,
			path:   "/api/v1/cluster-groups/2/clusters",
			status: http.StatusInternalServerError, msg: "Failed to filter clusters"},
		{name: "group clusters group check fails", h: f.failing(t, 2),
			caller: f.admin, method: http.MethodGet,
			path:   "/api/v1/cluster-groups/2/clusters",
			status: http.StatusInternalServerError, msg: "Failed to filter clusters"},
		{name: "group clusters membership fails", h: f.failing(t, 3),
			caller: f.admin, method: http.MethodGet,
			path:   "/api/v1/cluster-groups/2/clusters",
			status: http.StatusInternalServerError, msg: "Failed to filter clusters"},
		{name: "clusters of a hidden group", caller: f.admin,
			method: http.MethodGet, path: "/api/v1/cluster-groups/3/clusters",
			status: http.StatusNotFound, msg: "Cluster group not found"},
		{name: "clusters for a restricted caller", caller: f.admin,
			method: http.MethodGet, path: "/api/v1/cluster-groups/2/clusters",
			status: http.StatusOK, msg: "Spock one"},
		{name: "clusters for a superuser", caller: f.super,
			method: http.MethodGet, path: "/api/v1/cluster-groups/2/clusters",
			status: http.StatusOK, msg: "Empty"},

		// Creating a cluster in a group.
		{name: "create in group malformed body", caller: f.super,
			method: http.MethodPost, path: "/api/v1/cluster-groups/2/clusters",
			body: `{bad`, status: http.StatusBadRequest},
		{name: "create in group invalid name", caller: f.super,
			method: http.MethodPost, path: "/api/v1/cluster-groups/2/clusters",
			body: `{"name":"bad<name>"}`, status: http.StatusBadRequest},
		{name: "create in group fails", h: f.failing(t, 0), caller: f.super,
			method: http.MethodPost, path: "/api/v1/cluster-groups/2/clusters",
			body: `{"name":"Grouped"}`, status: http.StatusInternalServerError,
			msg: "Failed to create cluster"},
		{name: "created in group", caller: f.super, method: http.MethodPost,
			path: "/api/v1/cluster-groups/2/clusters", body: `{"name":"Grouped"}`,
			status: http.StatusCreated, msg: "Grouped"},

		// Deleting a group.
		{name: "delete visibility fails", h: f.failing(t, 1), caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/cluster-groups/2",
			status: http.StatusInternalServerError,
			msg:    "Failed to delete cluster group"},
		{name: "delete membership fails", h: f.failing(t, 2), caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/cluster-groups/2",
			status: http.StatusInternalServerError,
			msg:    "Failed to delete cluster group"},
		{name: "delete hidden group", caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/cluster-groups/3",
			status: http.StatusNotFound, msg: "Cluster group not found"},
		{name: "delete default group", caller: f.super,
			method: http.MethodDelete, path: "/api/v1/cluster-groups/1",
			status: http.StatusForbidden,
			msg:    "The default group cannot be deleted"},
		{name: "delete fails", h: f.failing(t, 2), caller: f.super,
			method: http.MethodDelete, path: "/api/v1/cluster-groups/2",
			status: http.StatusInternalServerError,
			msg:    "Failed to delete cluster group"},
		{name: "visible group deleted", caller: f.admin,
			method: http.MethodDelete, path: "/api/v1/cluster-groups/2",
			status: http.StatusNoContent},
	})
}

// TestClusterCov_Routing covers the path parsing in handleClusterSubpath
// and handleClusterGroupSubpath, and the routes registered when no
// datastore is configured.
func TestClusterCov_Routing(t *testing.T) {
	f := newClusterCovFixture(t)

	cases := []clusterCovCase{
		{name: "empty cluster path", path: "/api/v1/clusters/",
			status: http.StatusNotFound},
		{name: "servers bad cluster", path: "/api/v1/clusters/x/servers",
			status: http.StatusBadRequest, msg: "Invalid cluster ID"},
		{name: "server removal bad cluster", path: "/api/v1/clusters/x/servers/5",
			status: http.StatusBadRequest, msg: "Invalid cluster ID"},
		{name: "server removal bad connection",
			path: "/api/v1/clusters/1/servers/x", status: http.StatusBadRequest,
			msg: "Invalid connection ID"},
		{name: "relationships bad cluster",
			path:   "/api/v1/clusters/x/relationships",
			status: http.StatusBadRequest, msg: "Invalid cluster ID"},
		{name: "relationship bad cluster",
			path:   "/api/v1/clusters/x/relationships/1",
			status: http.StatusBadRequest, msg: "Invalid cluster ID"},
		{name: "relationship bad id", path: "/api/v1/clusters/1/relationships/x",
			status: http.StatusBadRequest, msg: "Invalid relationship ID"},
		{name: "connection relationships bad cluster",
			path:   "/api/v1/clusters/x/connections/5/relationships",
			status: http.StatusBadRequest, msg: "Invalid cluster ID"},
		{name: "connection relationships bad connection",
			path:   "/api/v1/clusters/1/connections/x/relationships",
			status: http.StatusBadRequest, msg: "Invalid connection ID"},
		{name: "bad prefixed cluster", path: "/api/v1/clusters/cluster-x",
			status: http.StatusBadRequest, msg: "Invalid cluster ID"},
		{name: "bad plain cluster", path: "/api/v1/clusters/x",
			status: http.StatusBadRequest, msg: "Invalid cluster ID"},
		{name: "cluster method not allowed", method: http.MethodPatch,
			path: "/api/v1/clusters/1", status: http.StatusMethodNotAllowed},
		{name: "empty group path", path: "/api/v1/cluster-groups/",
			status: http.StatusNotFound},
		{name: "group clusters bad group",
			path:   "/api/v1/cluster-groups/x/clusters",
			status: http.StatusBadRequest, msg: "Invalid group ID"},
		{name: "bad group", path: "/api/v1/cluster-groups/x",
			status: http.StatusBadRequest, msg: "Invalid group ID"},
	}
	for i := range cases {
		cases[i].caller = f.super
		if cases[i].method == "" {
			cases[i].method = http.MethodGet
		}
	}
	f.run(t, cases)

	t.Run("no datastore", func(t *testing.T) {
		h := NewClusterHandler(nil, f.store, auth.NewRBACChecker(f.store))
		for _, path := range []string{
			"/api/v1/clusters", "/api/v1/clusters/list", "/api/v1/clusters/1",
			"/api/v1/cluster-groups", "/api/v1/cluster-groups/1",
		} {
			rec := clusterCovServe(h, f.super, http.MethodGet, path, "")
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("%s: expected 503, got %d: %s", path, rec.Code,
					rec.Body.String())
			}
		}
	})
}

// TestClusterCov_UpdateConnectionClusterTokenScope covers the token-scope
// gate on PUT /api/v1/connections/{id}/cluster: re-homing a connection
// needs read_write on it, so a read-only scope entry is refused whilst
// read_write, session, unscoped and wildcard callers pass.
func TestClusterCov_UpdateConnectionClusterTokenScope(t *testing.T) {
	f := newClusterCovFixture(t)
	handler := NewConnectionHandlerWithSecurity(database.NewTestDatastore(f.pool),
		f.store, auth.NewRBACChecker(f.store), false, nil, nil)
	const body = `{"cluster_id":2,"membership_source":"manual"}`

	t.Run("read-only token refused", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.handleUpdateConnectionCluster(rec,
			newScopeRequest(f.readOnly, http.MethodPut, body), 5)
		assertError(t, rec, http.StatusForbidden, connectionOutOfTokenScope)
		f.expectValue(t, "conn cluster", 5, "1")
	})

	// The narrowed token cannot see cluster 2 (member: connection 9), so
	// it is refused with 404; TestUpdateConnectionClusterTargetAndRole
	// covers that case.
	for _, caller := range []scopeCaller{f.session, f.unscoped, f.wildcard} {
		t.Run(caller.name+" allowed", func(t *testing.T) {
			if _, err := f.pool.Exec(context.Background(),
				"UPDATE connections SET cluster_id = 1 WHERE id = 5"); err != nil {
				t.Fatalf("Reset connection: %v", err)
			}
			rec := httptest.NewRecorder()
			handler.handleUpdateConnectionCluster(rec,
				newScopeRequest(caller, http.MethodPut, body), 5)
			assertStatus(t, rec, http.StatusOK)
			f.expectValue(t, "conn cluster", 5, "2")
		})
	}
}
