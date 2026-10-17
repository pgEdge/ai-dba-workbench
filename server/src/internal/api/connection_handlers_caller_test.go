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

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// TestConnectionLifecycleForEachCaller is the regression test for issue
// #668: the connection create, update and delete handlers identify the
// caller from the request context, so an API token is accepted exactly
// as a session is. Each request is authenticated the way the middleware
// does it, from the raw bearer alone, so nothing in the context is set
// by hand.
func TestConnectionLifecycleForEachCaller(t *testing.T) {
	for _, tc := range []struct {
		name, username string
		token          func(t *testing.T, f *connCovFixture, username string) string
	}{
		{"session", "issue668_session", func(t *testing.T, f *connCovFixture, username string) string {
			t.Helper()
			return f.newCaller(t, username, auth.PermManageConnections).token
		}},
		{"api token", "issue668_token", func(t *testing.T, f *connCovFixture, username string) string {
			t.Helper()
			f.newCaller(t, username, auth.PermManageConnections)
			raw, _, err := f.store.AsActor(auth.SystemActor()).CreateToken(
				username, "issue 668 test", nil, false)
			if err != nil {
				t.Fatalf("CreateToken failed: %v", err)
			}
			return raw
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newConnCovFixture(t, connCovSecret)
			username := tc.username
			bearer := tc.token(t, f, username)
			send := func(method, path string, body any) int {
				t.Helper()
				req := withAuth(t, f.store, connCovRequest(t, method, path, body), bearer)
				return f.serve(req).Code
			}

			// serve routes the per-connection subpaths only, so the
			// create is sent to its handler directly.
			createReq := withAuth(t, f.store, connCovRequest(t, http.MethodPost,
				"/api/v1/connections", ConnectionCreateRequest{
					Name:         "issue-668",
					Host:         connCovAllowedHost,
					Port:         5432,
					DatabaseName: "postgres",
					Username:     "app",
					Password:     "example-password",
				}), bearer)
			rec := httptest.NewRecorder()
			f.handler.createConnection(rec, createReq)
			assertStatus(t, rec, http.StatusCreated)
			var created connCovJSONConn
			decodeInto(t, rec, &created)

			var owner string
			if err := f.pool.QueryRow(context.Background(),
				"SELECT owner_username FROM connections WHERE id = $1",
				created.ID).Scan(&owner); err != nil {
				t.Fatalf("Read back owner: %v", err)
			}
			if owner != username {
				t.Errorf("owner_username = %q, want %q", owner, username)
			}

			name := "issue-668-renamed"
			if code := send(http.MethodPut, connCovPath(created.ID, ""),
				ConnectionFullUpdateRequest{Name: &name}); code != http.StatusOK {
				t.Fatalf("update status = %d, want 200", code)
			}
			if code := send(http.MethodDelete, connCovPath(created.ID, ""), nil); code != http.StatusNoContent {
				t.Fatalf("delete status = %d, want 204", code)
			}
			if f.rowExists(t, created.ID) {
				t.Error("deleted connection is still live")
			}
		})
	}
}

// TestCreateConnectionNeedsWholeTokenScope is the regression test for
// the connection-create gap the #688 review found: a token for an owner
// holding manage_connections, but scoped to one connection read-only,
// could create a shared connection. Connection create now needs a
// token whose scope covers every connection at read_write, as cluster
// and cluster group creation do, and the refusal is audited as
// connection.create. A token covering every connection, and the owner's
// session, may still create one.
func TestCreateConnectionNeedsWholeTokenScope(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	f.handler.SetDenialRecorder(NewRBACHandler(f.store,
		auth.NewRBACChecker(f.store)).RecordDenial)
	owner := f.newCaller(t, "create_scope_owner", auth.PermManageConnections)
	existing := f.seed(t, "create-scope-existing", owner.username, true)

	tokenWith := func(note string, conns []auth.ScopedConnection) string {
		t.Helper()
		raw, stored, err := f.store.AsActor(auth.SystemActor()).CreateToken(
			owner.username, note, nil, false)
		if err != nil {
			t.Fatalf("CreateToken failed: %v", err)
		}
		if err := f.store.AsActor(auth.SystemActor()).SetTokenConnectionScope(
			stored.ID, conns, true); err != nil {
			t.Fatalf("SetTokenConnectionScope failed: %v", err)
		}
		return raw
	}
	create := func(bearer, name string) *httptest.ResponseRecorder {
		t.Helper()
		req := withAuth(t, f.store, connCovRequest(t, http.MethodPost,
			"/api/v1/connections", ConnectionCreateRequest{
				Name:         name,
				Host:         connCovAllowedHost,
				Port:         5432,
				DatabaseName: "postgres",
				Username:     "app",
				Password:     "example-password",
				IsShared:     true,
			}), bearer)
		rec := httptest.NewRecorder()
		f.handler.createConnection(rec, req)
		return rec
	}
	countNamed := func(name string) int {
		t.Helper()
		var n int
		if err := f.pool.QueryRow(context.Background(),
			"SELECT count(*) FROM connections WHERE name = $1", name).Scan(&n); err != nil {
			t.Fatalf("count connections: %v", err)
		}
		return n
	}

	narrow := tokenWith("narrow", []auth.ScopedConnection{
		{ConnectionID: existing, AccessLevel: auth.AccessLevelRead},
	})
	rec := create(narrow, "create-scope-refused")
	assertOutOfTokenScope(t, rec)
	assertDenialRecorded(t, f.store, "connection.create", targetOutOfTokenScope)
	if n := countNamed("create-scope-refused"); n != 0 {
		t.Errorf("refused create left %d rows", n)
	}

	wildcard := tokenWith("wildcard", []auth.ScopedConnection{
		{ConnectionID: auth.ConnectionIDAll, AccessLevel: auth.AccessLevelReadWrite},
	})
	assertStatus(t, create(wildcard, "create-scope-wildcard"), http.StatusCreated)
	assertStatus(t, create(owner.token, "create-scope-session"), http.StatusCreated)
}
