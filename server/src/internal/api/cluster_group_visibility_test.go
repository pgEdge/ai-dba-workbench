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
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Cluster-group writes check visibility before ownership (issue #649)
// =============================================================================

// clusterGroupVisibilitySetup adds two groups with no member
// connections to the fixture: 50 is owned by the plain caller and 51 by
// someone else.
const clusterGroupVisibilitySetup = `
INSERT INTO cluster_groups (id, name, owner_username) VALUES
    (50, 'Empty owned', 'cov_cl_plain'),
    (51, 'Empty unowned', 'someone-else')`

// TestClusterGroupWritesHideInvisibleGroups checks that PUT and DELETE
// on /api/v1/cluster-groups/{id} answer a group the caller cannot see
// exactly as they answer a missing one, record no refusal for it, and
// keep the audited ownership 403 for a group the caller can see.
func TestClusterGroupWritesHideInvisibleGroups(t *testing.T) {
	f := newClusterCovFixture(t)
	if _, err := f.pool.Exec(context.Background(),
		clusterGroupVisibilitySetup); err != nil {
		t.Fatalf("Failed to add the empty groups: %v", err)
	}
	capture := &routedCapture{}
	f.handler.SetDenialRecorder(capture.record)

	serve := func(caller scopeCaller, method, path, body string) *httptest.ResponseRecorder {
		capture.denials = nil
		return clusterCovServe(f.handler, caller, method, path, body)
	}
	const rename = `{"name":"Renamed"}`

	// Group 3's only member is a private connection the plain caller
	// cannot see; group 999 does not exist. Each write must get the
	// same response for both, and neither is an audited refusal.
	for _, tc := range []struct {
		name   string
		method string
		body   string
	}{
		{"update", http.MethodPut, rename},
		{"delete", http.MethodDelete, ""},
	} {
		t.Run(tc.name+" hidden group looks missing", func(t *testing.T) {
			missing := serve(f.plain, tc.method, "/api/v1/cluster-groups/999", tc.body)
			assertError(t, missing, http.StatusNotFound, "Cluster group not found")
			capture.expect(t)

			hidden := serve(f.plain, tc.method, "/api/v1/cluster-groups/3", tc.body)
			assertError(t, hidden, http.StatusNotFound, "Cluster group not found")
			capture.expect(t)

			if hidden.Body.String() != missing.Body.String() {
				t.Errorf("Hidden group body %q differs from missing group body %q",
					hidden.Body.String(), missing.Body.String())
			}
		})
	}

	// A visible group the caller does not own still draws the audited
	// ownership refusal.
	t.Run("update visible group not owned", func(t *testing.T) {
		rec := serve(f.plain, http.MethodPut, "/api/v1/cluster-groups/2", rename)
		const reason = "You do not have permission to update this cluster group"
		assertError(t, rec, http.StatusForbidden, reason)
		capture.expect(t, routedDenial{"cluster_group.update",
			"cluster-groups/2", reason})
	})

	// The token-scope check depends only on the caller, so it refuses a
	// missing group as it refuses an existing one.
	narrowed := scopeCaller{name: f.narrowed.name,
		wrap: func(r *http.Request) *http.Request {
			return f.narrowed.wrap(f.admin.wrap(r))
		}}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method+" out of token scope before lookup", func(t *testing.T) {
			body := ""
			if method == http.MethodPut {
				body = rename
			}
			existing := serve(narrowed, method, "/api/v1/cluster-groups/2", body)
			assertOutOfTokenScope(t, existing)
			missing := serve(narrowed, method, "/api/v1/cluster-groups/999", body)
			assertOutOfTokenScope(t, missing)
			if existing.Body.String() != missing.Body.String() {
				t.Errorf("Existing group body %q differs from missing group body %q",
					existing.Body.String(), missing.Body.String())
			}
		})
	}
}

// TestClusterGroupEmptyGroupOneRule checks that every cluster-group
// route shows a group with no member connections to its owner and to a
// caller who sees every connection, and answers anyone else exactly as
// for a missing group, whatever permissions they hold (issue #705).
func TestClusterGroupEmptyGroupOneRule(t *testing.T) {
	f := newClusterCovFixture(t)
	if _, err := f.pool.Exec(context.Background(),
		clusterGroupVisibilitySetup); err != nil {
		t.Fatalf("Failed to add the empty groups: %v", err)
	}
	capture := &routedCapture{}
	f.handler.SetDenialRecorder(capture.record)

	// The plain caller owns group 50, but through a token narrowed to
	// some connections is not treated as its owner.
	narrowedOwner := scopeCaller{name: "narrowed owner",
		wrap: func(r *http.Request) *http.Request {
			return f.narrowed.wrap(f.plain.wrap(r))
		}}
	const (
		owned    = "/api/v1/cluster-groups/50"
		unowned  = "/api/v1/cluster-groups/51"
		notFound = "Cluster group not found"
	)

	f.run(t, []clusterCovCase{
		// The owner, who holds no admin permission.
		{name: "owner gets empty group", caller: f.plain,
			method: http.MethodGet, path: owned, status: http.StatusOK,
			msg: "Empty owned"},
		{name: "owner lists clusters of empty group", caller: f.plain,
			method: http.MethodGet, path: owned + "/clusters",
			status: http.StatusOK, msg: "[]"},
		{name: "owner renames empty group", caller: f.plain,
			method: http.MethodPut, path: owned,
			body: `{"name":"Owned renamed"}`, status: http.StatusOK,
			msg: "Owned renamed"},

		// A restricted manage_connections holder who owns neither group.
		{name: "admin cannot get empty group", caller: f.admin,
			method: http.MethodGet, path: owned,
			status: http.StatusNotFound, msg: notFound},
		{name: "admin cannot list clusters of empty group", caller: f.admin,
			method: http.MethodGet, path: owned + "/clusters",
			status: http.StatusNotFound, msg: notFound},
		{name: "admin cannot rename empty group", caller: f.admin,
			method: http.MethodPut, path: unowned,
			body: `{"name":"Admin renamed"}`, status: http.StatusNotFound,
			msg: notFound},
		{name: "admin cannot delete empty group", caller: f.admin,
			method: http.MethodDelete, path: owned,
			status: http.StatusNotFound, msg: notFound},

		// The owner through a narrowed token.
		{name: "narrowed owner cannot get empty group", caller: narrowedOwner,
			method: http.MethodGet, path: owned,
			status: http.StatusNotFound, msg: notFound},
		{name: "narrowed owner cannot list clusters of empty group",
			caller: narrowedOwner, method: http.MethodGet,
			path: owned + "/clusters", status: http.StatusNotFound,
			msg: notFound},

		// A caller who sees every connection.
		{name: "superuser gets empty group", caller: f.super,
			method: http.MethodGet, path: unowned, status: http.StatusOK,
			msg: "Empty unowned"},
		{name: "superuser lists clusters of empty group", caller: f.super,
			method: http.MethodGet, path: unowned + "/clusters",
			status: http.StatusOK, msg: "[]"},
		{name: "superuser renames empty group", caller: f.super,
			method: http.MethodPut, path: unowned,
			body: `{"name":"Super renamed"}`, status: http.StatusOK,
			msg: "Super renamed"},
		{name: "superuser deletes empty group", caller: f.super,
			method: http.MethodDelete, path: unowned,
			status: http.StatusNoContent},

		// The owner can delete their own empty group.
		{name: "owner deletes empty group", caller: f.plain,
			method: http.MethodDelete, path: owned,
			status: http.StatusNoContent},
		{name: "deleted group is gone", caller: f.super,
			method: http.MethodGet, path: owned,
			status: http.StatusNotFound, msg: notFound},
	})
	if len(capture.denials) != 0 {
		t.Errorf("Expected no recorded denials, got %v", capture.denials)
	}
}

// TestClusterGroupListShowsOwnEmptyGroups checks that the group list
// applies the same rule as the single-group routes (issue #705).
func TestClusterGroupListShowsOwnEmptyGroups(t *testing.T) {
	f := newClusterCovFixture(t)
	if _, err := f.pool.Exec(context.Background(),
		clusterGroupVisibilitySetup); err != nil {
		t.Fatalf("Failed to add the empty groups: %v", err)
	}
	narrowedOwner := scopeCaller{name: "narrowed owner",
		wrap: func(r *http.Request) *http.Request {
			return f.narrowed.wrap(f.plain.wrap(r))
		}}

	for _, tc := range []struct {
		caller        scopeCaller
		owned, others bool
	}{
		{f.plain, true, false},
		{f.admin, false, false},
		{narrowedOwner, false, false},
		{f.super, true, true},
	} {
		t.Run(tc.caller.name, func(t *testing.T) {
			rec := clusterCovServe(f.handler, tc.caller, http.MethodGet,
				"/api/v1/cluster-groups", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("Expected status 200, got %d: %s", rec.Code,
					rec.Body.String())
			}
			body := rec.Body.String()
			if got := strings.Contains(body, "Empty owned"); got != tc.owned {
				t.Errorf("Owned empty group listed = %v, want %v: %s",
					got, tc.owned, body)
			}
			if got := strings.Contains(body, "Empty unowned"); got != tc.others {
				t.Errorf("Unowned empty group listed = %v, want %v: %s",
					got, tc.others, body)
			}
		})
	}
}

// countingClusterHandler returns a handler over its own pool and the
// number of connections that pool has handed out, which counts the
// datastore queries a request runs.
func countingClusterHandler(t *testing.T, f *clusterCovFixture) (*ClusterHandler, *atomic.Int64) {
	t.Helper()

	cfg, err := pgxpool.ParseConfig(f.connStr)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.MaxConns = 2
	var acquired atomic.Int64
	cfg.PrepareConn = func(context.Context, *pgx.Conn) (bool, error) {
		acquired.Add(1)
		return true, nil
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	t.Cleanup(pool.Close)

	return NewClusterHandler(database.NewTestDatastore(pool), f.store,
		auth.NewRBACChecker(f.store)), &acquired
}

// TestClusterGroupLookupsCostTheSameForHiddenAndMissing checks that each
// cluster-group route runs as many datastore queries for a group the
// caller cannot see as for an id that does not exist (issue #649).
func TestClusterGroupLookupsCostTheSameForHiddenAndMissing(t *testing.T) {
	f := newClusterCovFixture(t)
	h, acquired := countingClusterHandler(t, f)

	for _, tc := range []struct {
		name   string
		method string
		suffix string
		body   string
	}{
		{"get", http.MethodGet, "", ""},
		{"list clusters", http.MethodGet, "/clusters", ""},
		{"update", http.MethodPut, "", `{"name":"Renamed"}`},
		{"delete", http.MethodDelete, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queries := func(id string) (int64, string) {
				before := acquired.Load()
				rec := clusterCovServe(h, f.plain, tc.method,
					"/api/v1/cluster-groups/"+id+tc.suffix, tc.body)
				assertError(t, rec, http.StatusNotFound, "Cluster group not found")
				return acquired.Load() - before, rec.Body.String()
			}
			missing, missingBody := queries("999")
			hidden, hiddenBody := queries("3")
			if missing != hidden {
				t.Errorf("A missing group ran %d queries but a hidden one ran %d",
					missing, hidden)
			}
			if missingBody != hiddenBody {
				t.Errorf("Hidden group body %q differs from missing group body %q",
					hiddenBody, missingBody)
			}
		})
	}
}
