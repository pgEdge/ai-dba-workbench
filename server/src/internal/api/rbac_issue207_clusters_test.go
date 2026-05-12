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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Regression Test Coverage for GitHub Issue #207
//
// Issue #207 was an authorization gap: eleven cluster-handler entry
// points performed cluster-group and cluster mutations without checking
// the manage_connections admin permission, so any authenticated caller
// could create groups, create clusters, attach/detach servers, or
// rewrite topology relationships.
//
// The production fix added one of two gates at the very top of each
// handler (before request decoding) so denied callers cannot probe
// payload shape via validation error messages:
//
//   - Plain admin gate: RBACChecker.HasAdminPermission with
//     auth.PermManageConnections. Used by all create handlers,
//     the server-attach/detach handlers, and the topology-relationship
//     handlers, where no per-object owner concept exists.
//   - Owner-fallback gate: same admin check, but on failure an
//     ownership check via getUserInfoCompat against the object's
//     owner_username column lets the row's owner through. Used by
//     deleteClusterGroup. NOTE: updateCluster and deleteCluster were
//     originally specified to use the owner-fallback variant too, but
//     the clusters table has no owner_username column, so the plain
//     admin gate is applied to them instead. See the source comments in
//     updateCluster/deleteCluster for the rationale.
//
// The tests below exercise the "denied" path for every newly gated
// handler without requiring Postgres. The handler returns 403 before
// any datastore access, so a nil-datastore handler with a real
// RBACChecker is sufficient for the negative path; the assertion that
// the rejection happens BEFORE the datastore would have been touched is
// implicit in the nil datastore not panicking.
//
// The integration-style "owner-fallback success" cases for
// deleteClusterGroup require Postgres and are gated on
// TEST_AI_WORKBENCH_SERVER through the same path as the existing
// TestClusterHandler_UpdateClusterGroup_Integration_* tests in
// cluster_handlers_test.go.
// =============================================================================

// newIssue207Handler builds a ClusterHandler with a real RBACChecker
// backed by a temporary auth store. The datastore is intentionally nil;
// the gate must return 403 before any datastore call, so a panic from a
// nil datastore would itself be a regression.
func newIssue207Handler(t *testing.T) (*ClusterHandler, *auth.AuthStore, func()) {
	t.Helper()
	_, store, cleanup := createTestRBACHandler(t)
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)
	return handler, store, cleanup
}

// newIssue207UnprivilegedUser creates an authenticated user with no
// admin permissions. Returns the user ID for use with withUser.
func newIssue207UnprivilegedUser(t *testing.T, store *auth.AuthStore, name string) int64 {
	t.Helper()
	if err := store.CreateUser(name, "Password1234", "", "", ""); err != nil {
		t.Fatalf("Failed to create user %q: %v", name, err)
	}
	userID, err := store.GetUserID(name)
	if err != nil {
		t.Fatalf("Failed to get user ID for %q: %v", name, err)
	}
	return userID
}

// assertForbiddenWithMessage fails the test if the recorded response is
// not a 403 carrying the canonical permission-denied error message.
// The substring "Permission denied" is the stable prefix of the plain
// admin gate response (see cluster_handlers.go); locking on it ensures
// a future change to an empty or differently-worded message surfaces as
// a test failure rather than a silent regression.
func assertForbiddenWithMessage(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}
	if !strings.Contains(resp.Error, "Permission denied") {
		t.Errorf("Expected error message to contain %q, got %q",
			"Permission denied", resp.Error)
	}
}

// =============================================================================
// Plain admin gate: critical create + topology handlers
// =============================================================================

// TestClusterHandler_CreateClusterGroup_Issue207_Denied verifies that an
// authenticated user without manage_connections cannot create a cluster
// group. The handler must return 403 before reading the request body so
// a denied caller cannot enumerate validation errors.
func TestClusterHandler_CreateClusterGroup_Issue207_Denied(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_create_group")

	body, _ := json.Marshal(ClusterGroupRequest{Name: "Attempt"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-groups",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.createClusterGroup(rec, req)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_CreateClusterGroup_Issue207_DeniedSkipsDecode
// verifies that the 403 happens before JSON decoding: a body that would
// otherwise trigger a 400 ("Invalid request body") returns 403 instead.
// This is the "no payload probing" property the gate ordering protects.
func TestClusterHandler_CreateClusterGroup_Issue207_DeniedSkipsDecode(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_create_group_bad")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-groups",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.createClusterGroup(rec, req)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_CreateClusterGroup_Issue207_SuperuserAllowed
// confirms that the gate is satisfied by the superuser bypass. A nil
// datastore deliberately panics if the handler reaches the datastore
// call, so this test additionally proves the gate path returns to the
// caller before any datastore work.
func TestClusterHandler_CreateClusterGroup_Issue207_SuperuserAllowed(t *testing.T) {
	handler, _, cleanup := newIssue207Handler(t)
	defer cleanup()

	body, _ := json.Marshal(ClusterGroupRequest{Name: "AdminGroup"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-groups",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	defer func() {
		// We intentionally allow a panic here: the nil datastore will
		// blow up when the handler reaches CreateClusterGroup. The
		// security contract we are testing is that the handler PASSES
		// the gate (i.e., does NOT return 403). The integration tests
		// against a real Postgres cover the success-write path.
		_ = recover()
	}()
	handler.createClusterGroup(rec, req)

	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("Superuser failed auth (status %d): %s",
			rec.Code, rec.Body.String())
	}
}

// TestClusterHandler_CreateClusterInGroup_Issue207_Denied verifies that
// a non-admin caller cannot POST to /api/v1/cluster-groups/{id}/clusters.
func TestClusterHandler_CreateClusterInGroup_Issue207_Denied(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_create_in_group")

	body, _ := json.Marshal(ClusterRequest{Name: "Attempt"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-groups/1/clusters",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.createClusterInGroup(rec, req, 1)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_CreateClusterInGroup_Issue207_DeniedSkipsDecode
// confirms that the 403 happens before JSON decoding.
func TestClusterHandler_CreateClusterInGroup_Issue207_DeniedSkipsDecode(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_create_in_group_bad")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-groups/1/clusters",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.createClusterInGroup(rec, req, 1)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_HandleCreateCluster_Issue207_Denied verifies that
// a non-admin caller cannot POST to /api/v1/clusters.
func TestClusterHandler_HandleCreateCluster_Issue207_Denied(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_create_cluster")

	body, _ := json.Marshal(ManualClusterRequest{Name: "Attempt"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleCreateCluster(rec, req)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_HandleCreateCluster_Issue207_DeniedSkipsDecode
// confirms that the 403 happens before JSON decoding.
func TestClusterHandler_HandleCreateCluster_Issue207_DeniedSkipsDecode(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_create_cluster_bad")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleCreateCluster(rec, req)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_AddServerToCluster_Issue207_Denied verifies that a
// non-admin caller cannot attach a server to a cluster.
func TestClusterHandler_AddServerToCluster_Issue207_Denied(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_attach")

	body, _ := json.Marshal(AddServerToClusterRequest{ConnectionID: 42})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters/1/servers",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.addServerToCluster(rec, req, 1)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_HandleRemoveServerFromCluster_Issue207_Denied
// verifies that a non-admin caller cannot detach a server from a
// cluster.
func TestClusterHandler_HandleRemoveServerFromCluster_Issue207_Denied(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_detach")

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/clusters/1/servers/42", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleRemoveServerFromCluster(rec, req, 1, 42)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_SetConnectionRelationships_Issue207_Denied
// verifies that a non-admin caller cannot rewrite topology
// relationships.
func TestClusterHandler_SetConnectionRelationships_Issue207_Denied(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_setrel")

	body := `{"relationships":[{"target_connection_id":3,"relationship_type":"streams_from"}]}`
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/clusters/1/connections/2/relationships",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.setConnectionRelationships(rec, req, 1, 2)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_SetConnectionRelationships_Issue207_DeniedSkipsDecode
// confirms the 403 happens before JSON decoding.
func TestClusterHandler_SetConnectionRelationships_Issue207_DeniedSkipsDecode(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_setrel_bad")

	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/clusters/1/connections/2/relationships",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.setConnectionRelationships(rec, req, 1, 2)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_ClearConnectionRelationships_Issue207_Denied
// verifies that a non-admin caller cannot clear topology relationships.
func TestClusterHandler_ClearConnectionRelationships_Issue207_Denied(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_clearrel")

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/clusters/1/connections/2/relationships", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.clearConnectionRelationships(rec, req, 1, 2)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_HandleDeleteRelationship_Issue207_Denied verifies
// that a non-admin caller cannot delete a relationship row.
func TestClusterHandler_HandleDeleteRelationship_Issue207_Denied(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_delrel")

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/clusters/1/relationships/5", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleDeleteRelationship(rec, req, 1, 5)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_UpdateCluster_Issue207_Denied verifies that a
// non-admin caller cannot rename or move a cluster. The body must
// otherwise pass the "at least one field" validation; we test that the
// 403 happens before that validation runs.
func TestClusterHandler_UpdateCluster_Issue207_Denied(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_update_cluster")

	body, _ := json.Marshal(ClusterRequest{Name: "Attempt"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/clusters/1",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.updateCluster(rec, req, 1)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_UpdateCluster_Issue207_DeniedSkipsDecode confirms
// the 403 happens before JSON decoding even when the body would have
// been valid for the legacy code path.
func TestClusterHandler_UpdateCluster_Issue207_DeniedSkipsDecode(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_update_cluster_bad")

	req := httptest.NewRequest(http.MethodPut, "/api/v1/clusters/1",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.updateCluster(rec, req, 1)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_DeleteCluster_Issue207_Denied verifies that a
// non-admin caller cannot delete a cluster.
func TestClusterHandler_DeleteCluster_Issue207_Denied(t *testing.T) {
	handler, store, cleanup := newIssue207Handler(t)
	defer cleanup()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_delete_cluster")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/clusters/1", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.deleteCluster(rec, req, 1)

	assertForbiddenWithMessage(t, rec)
}

// TestClusterHandler_DeleteClusterGroup_Issue207_Denied verifies the
// owner-fallback variant on the negative path: a non-owner caller
// without manage_connections cannot delete a cluster group. The handler
// must look up the group's owner before deciding, so the datastore is a
// real Postgres instance gated on TEST_AI_WORKBENCH_SERVER.
func TestClusterHandler_DeleteClusterGroup_Issue207_Denied(t *testing.T) {
	ds, _, cleanupDS := newTestDatastore(t)
	defer cleanupDS()

	_, store, cleanupStore := createTestRBACHandler(t)
	defer cleanupStore()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_outsider")
	token, _, err := store.AuthenticateUser("issue207_outsider", "Password1234")
	if err != nil {
		t.Fatalf("Failed to authenticate: %v", err)
	}
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(ds, store, checker)

	ctx := context.Background()
	created, err := ds.CreateClusterGroup(ctx, "Issue207 Target", nil)
	if err != nil {
		t.Fatalf("Failed to create cluster group: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/cluster-groups/"+strconv.Itoa(created.ID), nil)
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}

	// The group must still exist after the rejected delete.
	if _, err := ds.GetClusterGroup(ctx, created.ID); err != nil {
		t.Errorf("Group should still exist after rejected delete: %v", err)
	}
}

// TestClusterHandler_DeleteClusterGroup_Issue207_OwnerAllowed verifies
// the positive owner-fallback path: a non-admin caller who owns the
// cluster group can delete it. The group is created with the caller's
// username so the ownership branch fires.
func TestClusterHandler_DeleteClusterGroup_Issue207_OwnerAllowed(t *testing.T) {
	ds, pool, cleanupDS := newTestDatastore(t)
	defer cleanupDS()

	_, store, cleanupStore := createTestRBACHandler(t)
	defer cleanupStore()
	userID := newIssue207UnprivilegedUser(t, store, "issue207_owner")
	token, _, err := store.AuthenticateUser("issue207_owner", "Password1234")
	if err != nil {
		t.Fatalf("Failed to authenticate: %v", err)
	}
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(ds, store, checker)

	ctx := context.Background()
	created, err := ds.CreateClusterGroup(ctx, "Issue207 Owned", nil)
	if err != nil {
		t.Fatalf("Failed to create cluster group: %v", err)
	}
	// Mark the group as owned by the caller. CreateClusterGroup does
	// not set the owner_username column directly, so we update it here.
	_, err = pool.Exec(ctx,
		`UPDATE cluster_groups SET owner_username = $1 WHERE id = $2`,
		"issue207_owner", created.ID)
	if err != nil {
		t.Fatalf("Failed to mark group as owned: %v", err)
	}
	// Seed a cluster + owner-visible connection inside the group so the
	// post-gate visibility check in deleteClusterGroup
	// (groupHasVisibleConnection) succeeds. Without a visible member
	// connection the handler responds 404 even when the gate passes.
	// The connection is unshared and owned by the caller, which mirrors
	// the realistic owner-fallback scenario: an owner of an unshared
	// group + connection can delete their own group.
	var clusterID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO clusters (group_id, name) VALUES ($1, $2) RETURNING id`,
		created.ID, "owner-cluster").Scan(&clusterID); err != nil {
		t.Fatalf("Failed to seed cluster: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO connections (name, owner_username, is_shared, cluster_id)
         VALUES ($1, $2, FALSE, $3)`,
		"owner-conn", "issue207_owner", clusterID); err != nil {
		t.Fatalf("Failed to seed connection: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/cluster-groups/"+strconv.Itoa(created.ID), nil)
	req = withBearer(req, token)
	req = withUser(req, userID)
	// VisibleConnectionIDs reads the username from the request context to
	// apply the owner-visible rule for unshared connections. Production
	// middleware populates UsernameContextKey from the same bearer token
	// withBearer sets above; the test fixture must add it explicitly.
	req = withUsername(req, "issue207_owner")
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected status %d (owner allowed), got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}

	// The group should be gone now.
	if _, err := ds.GetClusterGroup(ctx, created.ID); err == nil {
		t.Errorf("Group should be deleted; GetClusterGroup unexpectedly succeeded")
	}
}

// TestClusterHandler_DeleteClusterGroup_Issue207_AdminAllowed verifies
// the positive admin path: a manage_connections grant lets a non-owner
// delete a cluster group.
func TestClusterHandler_DeleteClusterGroup_Issue207_AdminAllowed(t *testing.T) {
	ds, pool, cleanupDS := newTestDatastore(t)
	defer cleanupDS()

	_, store, cleanupStore := createTestRBACHandler(t)
	defer cleanupStore()
	userID := setupUserWithPermission(t, store, "issue207_admin",
		auth.PermManageConnections)
	token, _, err := store.AuthenticateUser("issue207_admin", "Password1234")
	if err != nil {
		t.Fatalf("Failed to authenticate: %v", err)
	}
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(ds, store, checker)

	ctx := context.Background()
	created, err := ds.CreateClusterGroup(ctx, "Issue207 Admin Target", nil)
	if err != nil {
		t.Fatalf("Failed to create cluster group: %v", err)
	}
	// Seed a cluster + shared connection inside the group so the
	// post-gate visibility check in deleteClusterGroup succeeds for the
	// admin caller. manage_connections is an admin permission and does
	// NOT grant ConnectionIDAll visibility, so the visibility helper
	// still applies the standard owner-or-shared rule. A shared
	// connection authored by another user is enough to make the group
	// visible to the admin caller without making them the owner.
	var clusterID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO clusters (group_id, name) VALUES ($1, $2) RETURNING id`,
		created.ID, "admin-cluster").Scan(&clusterID); err != nil {
		t.Fatalf("Failed to seed cluster: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO connections (name, owner_username, is_shared, cluster_id)
         VALUES ($1, $2, TRUE, $3)`,
		"admin-conn", "someone_else", clusterID); err != nil {
		t.Fatalf("Failed to seed connection: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/cluster-groups/"+strconv.Itoa(created.ID), nil)
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected status %d (admin allowed), got %d. Body: %s",
			http.StatusNoContent, rec.Code, rec.Body.String())
	}
}

// TestClusterHandler_DeleteClusterGroup_Issue207_MissingAuth verifies
// the unauthenticated path: a delete with no bearer token returns 401
// from getUserInfoCompat before any datastore call.
func TestClusterHandler_DeleteClusterGroup_Issue207_MissingAuth(t *testing.T) {
	handler, _, cleanup := newIssue207Handler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/cluster-groups/1", nil)
	rec := httptest.NewRecorder()

	handler.deleteClusterGroup(rec, req, 1)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

// =============================================================================
// Allowed-path sanity tests for the plain-admin gates
//
// These tests pair an admin-permission grant with a nil datastore. The
// gate must let the caller through; the subsequent datastore call will
// panic. We use a deferred recover() so the test asserts only the
// pre-datastore behavior (no 403). Integration tests against a real
// Postgres cover the write side of the success path.
// =============================================================================

// assertGatePassed runs the supplied call and asserts that the recorded
// response is neither a 403 nor a 401. A panic from the nil datastore
// is recovered silently; the test fails only if the gate rejected the
// caller. Treating 401 as a failure too catches regressions where the
// auth lookup itself starts rejecting a context that previously
// satisfied the gate.
func assertGatePassed(t *testing.T, rec *httptest.ResponseRecorder, call func()) {
	t.Helper()
	defer func() {
		_ = recover()
		if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
			t.Fatalf("Permitted caller failed auth (status %d): %s",
				rec.Code, rec.Body.String())
		}
	}()
	call()
}

// TestClusterHandler_CreateClusterInGroup_Issue207_AdminAllowed
// confirms the gate lets a manage_connections caller through.
func TestClusterHandler_CreateClusterInGroup_Issue207_AdminAllowed(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	userID := setupUserWithPermission(t, store, "issue207_create_in_group_admin",
		auth.PermManageConnections)
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	body, _ := json.Marshal(ClusterRequest{Name: "Attempt"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-groups/1/clusters",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	assertGatePassed(t, rec, func() {
		handler.createClusterInGroup(rec, req, 1)
	})
}

// TestClusterHandler_HandleCreateCluster_Issue207_AdminAllowed
// confirms the gate lets a manage_connections caller through.
func TestClusterHandler_HandleCreateCluster_Issue207_AdminAllowed(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	userID := setupUserWithPermission(t, store, "issue207_create_cluster_admin",
		auth.PermManageConnections)
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	body, _ := json.Marshal(ManualClusterRequest{Name: "Attempt"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	assertGatePassed(t, rec, func() {
		handler.handleCreateCluster(rec, req)
	})
}

// TestClusterHandler_AddServerToCluster_Issue207_AdminAllowed confirms
// the gate lets a manage_connections caller through.
func TestClusterHandler_AddServerToCluster_Issue207_AdminAllowed(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	userID := setupUserWithPermission(t, store, "issue207_attach_admin",
		auth.PermManageConnections)
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	body, _ := json.Marshal(AddServerToClusterRequest{ConnectionID: 42})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters/1/servers",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	assertGatePassed(t, rec, func() {
		handler.addServerToCluster(rec, req, 1)
	})
}

// TestClusterHandler_HandleRemoveServerFromCluster_Issue207_AdminAllowed
// confirms the gate lets a manage_connections caller through.
func TestClusterHandler_HandleRemoveServerFromCluster_Issue207_AdminAllowed(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	userID := setupUserWithPermission(t, store, "issue207_detach_admin",
		auth.PermManageConnections)
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/clusters/1/servers/42", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	assertGatePassed(t, rec, func() {
		handler.handleRemoveServerFromCluster(rec, req, 1, 42)
	})
}

// TestClusterHandler_SetConnectionRelationships_Issue207_AdminAllowed
// confirms the gate lets a manage_connections caller through.
func TestClusterHandler_SetConnectionRelationships_Issue207_AdminAllowed(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	userID := setupUserWithPermission(t, store, "issue207_setrel_admin",
		auth.PermManageConnections)
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	body := `{"relationships":[{"target_connection_id":3,"relationship_type":"streams_from"}]}`
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/clusters/1/connections/2/relationships",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	assertGatePassed(t, rec, func() {
		handler.setConnectionRelationships(rec, req, 1, 2)
	})
}

// TestClusterHandler_ClearConnectionRelationships_Issue207_AdminAllowed
// confirms the gate lets a manage_connections caller through.
func TestClusterHandler_ClearConnectionRelationships_Issue207_AdminAllowed(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	userID := setupUserWithPermission(t, store, "issue207_clearrel_admin",
		auth.PermManageConnections)
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/clusters/1/connections/2/relationships", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	assertGatePassed(t, rec, func() {
		handler.clearConnectionRelationships(rec, req, 1, 2)
	})
}

// TestClusterHandler_HandleDeleteRelationship_Issue207_AdminAllowed
// confirms the gate lets a manage_connections caller through.
func TestClusterHandler_HandleDeleteRelationship_Issue207_AdminAllowed(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	userID := setupUserWithPermission(t, store, "issue207_delrel_admin",
		auth.PermManageConnections)
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/clusters/1/relationships/5", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	assertGatePassed(t, rec, func() {
		handler.handleDeleteRelationship(rec, req, 1, 5)
	})
}

// TestClusterHandler_UpdateCluster_Issue207_AdminAllowed confirms the
// gate lets a manage_connections caller through to the body-validation
// path. The body is intentionally empty so the handler returns a 400
// after the gate; we assert only that the gate did not return 403.
func TestClusterHandler_UpdateCluster_Issue207_AdminAllowed(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	userID := setupUserWithPermission(t, store, "issue207_update_cluster_admin",
		auth.PermManageConnections)
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	body := `{}` // Empty body -> 400 "at least one field required" after the gate.
	req := httptest.NewRequest(http.MethodPut, "/api/v1/clusters/1",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.updateCluster(rec, req, 1)

	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("Admin caller failed auth (status %d): %s",
			rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 after gate passes (empty body), got %d: %s",
			rec.Code, rec.Body.String())
	}
}

// TestClusterHandler_DeleteCluster_Issue207_AdminAllowed confirms the
// gate lets a manage_connections caller through.
func TestClusterHandler_DeleteCluster_Issue207_AdminAllowed(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	userID := setupUserWithPermission(t, store, "issue207_delete_cluster_admin",
		auth.PermManageConnections)
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/clusters/1", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	assertGatePassed(t, rec, func() {
		handler.deleteCluster(rec, req, 1)
	})
}

// =============================================================================
// Issue #207 type smoke tests
//
// These tests anchor the JSON shape of the request bodies used by the
// gated handlers so a future struct rename will surface here rather
// than as a silent regression in the gate tests above.
// =============================================================================

// TestIssue207ClusterRequest_JSONShape verifies the ClusterRequest
// struct encodes the fields the gate tests rely on.
func TestIssue207ClusterRequest_JSONShape(t *testing.T) {
	desc := "test"
	repl := "binary"
	gid := 1
	req := ClusterRequest{
		Name:            "n",
		Description:     &desc,
		ReplicationType: &repl,
		GroupID:         &gid,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ClusterRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Name != "n" || decoded.Description == nil ||
		*decoded.Description != "test" {
		t.Errorf("Round-trip mismatch: %+v", decoded)
	}
}

// TestIssue207AddServerToClusterRequest_JSONShape verifies the body
// shape used by the addServerToCluster denial test.
func TestIssue207AddServerToClusterRequest_JSONShape(t *testing.T) {
	role := "primary"
	req := AddServerToClusterRequest{ConnectionID: 42, Role: &role}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded AddServerToClusterRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ConnectionID != 42 || decoded.Role == nil ||
		*decoded.Role != "primary" {
		t.Errorf("Round-trip mismatch: %+v", decoded)
	}
}

// TestIssue207ManualClusterRequest_JSONShape verifies the body shape
// used by the handleCreateCluster denial test.
func TestIssue207ManualClusterRequest_JSONShape(t *testing.T) {
	gid := 1
	req := ManualClusterRequest{
		Name:            "x",
		Description:     "y",
		ReplicationType: "binary",
		GroupID:         &gid,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ManualClusterRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Name != "x" || decoded.GroupID == nil || *decoded.GroupID != 1 {
		t.Errorf("Round-trip mismatch: %+v", decoded)
	}
}
