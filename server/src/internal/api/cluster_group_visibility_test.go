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

	// A group with no members is visible to its owner and to a
	// manage_connections holder for an update, and to nobody restricted
	// for a delete, as for the GET.
	f.run(t, []clusterCovCase{
		{name: "update empty group not owned", caller: f.plain,
			method: http.MethodPut, path: "/api/v1/cluster-groups/51",
			body: rename, status: http.StatusNotFound,
			msg: "Cluster group not found"},
		{name: "delete empty group not owned", caller: f.plain,
			method: http.MethodDelete, path: "/api/v1/cluster-groups/51",
			status: http.StatusNotFound, msg: "Cluster group not found"},
		{name: "owner updates empty group", caller: f.plain,
			method: http.MethodPut, path: "/api/v1/cluster-groups/50",
			body: `{"name":"Owned renamed"}`, status: http.StatusOK,
			msg: "Owned renamed"},
		{name: "owner cannot delete empty group", caller: f.plain,
			method: http.MethodDelete, path: "/api/v1/cluster-groups/50",
			status: http.StatusNotFound, msg: "Cluster group not found"},
		{name: "admin updates empty group", caller: f.admin,
			method: http.MethodPut, path: "/api/v1/cluster-groups/51",
			body: `{"name":"Admin renamed"}`, status: http.StatusOK,
			msg: "Admin renamed"},
		{name: "superuser deletes empty group", caller: f.super,
			method: http.MethodDelete, path: "/api/v1/cluster-groups/51",
			status: http.StatusNoContent},
	})
	if len(capture.denials) != 0 {
		t.Errorf("Expected no recorded denials, got %v", capture.denials)
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
