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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Regression Test Coverage for GitHub Issue #233
//
// Issue #233 is a follow-up to issue #207 / PR #217. The original fix
// gated cluster mutations on manage_connections but missed a sibling
// gap in the connection-create handler: createConnection only gated
// the IsShared branch, so any authenticated caller could create a
// non-shared server connection and silently expand the topology they
// could see.
//
// The fix in connection_handlers.go applies the plain admin gate
// (Variant 1 per .claude/golang-expert/rbac-patterns.md) to ALL
// connection creation, regardless of req.IsShared. The gate is placed
// after the bearer-token authentication step (because the handler
// needs the username for owner_username) but BEFORE request-body
// decoding, so denied callers cannot probe payload shape via
// validation error messages.
//
// The tests below exercise:
//
//   - Denial path: authenticated, unprivileged caller attempting a
//     non-shared create gets 403 with the canonical wording.
//   - Decode-skip property: denial happens before JSON decoding, so
//     an invalid body still returns 403 (not 400).
//   - Shared-create denial: the previously-gated shared branch still
//     denies, locking in no regression for the original behavior.
//   - Admin allowed: a caller with manage_connections passes the gate;
//     the post-gate datastore call panics on a nil datastore, which
//     we recover from. The test asserts only that the gate did not
//     return 403/401.
//
// The denial paths run with a nil datastore so a panic from a stray
// datastore call would itself surface as a test failure (the gate
// must reject before any datastore work).
// =============================================================================

// setupIssue233CreateConnection builds a ConnectionHandler with a real
// RBACChecker and a real auth store, then registers a user with the
// caller-supplied permission set. The user is authenticated so the
// returned bearer token will pass through getUserInfoCompat. The
// datastore is intentionally nil: the denial path must return 403
// before the datastore is touched, and the admin-allowed path uses
// the nil datastore to verify the gate fired (a post-gate datastore
// access panics, which the test recovers from).
//
// Returns the handler, the user's ID, and the user's session token.
// The cleanup is taken care of by the caller via createTestRBACHandler.
func setupIssue233CreateConnection(
	t *testing.T,
	username string,
	permissions []string,
) (*ConnectionHandler, int64, string, func()) {
	t.Helper()

	_, store, cleanup := createTestRBACHandler(t)
	var userID int64
	if len(permissions) == 0 {
		userID = newIssue207UnprivilegedUser(t, store, username)
	} else {
		userID = setupUserWithPermissions(t, store, username, permissions)
	}

	token, _, err := store.AuthenticateUser(username, "Password1234")
	if err != nil {
		cleanup()
		t.Fatalf("Failed to authenticate test user %s: %v", username, err)
	}

	checker := auth.NewRBACChecker(store)
	handler := NewConnectionHandler(nil, store, checker)

	return handler, userID, token, cleanup
}

// TestConnectionHandler_CreateConnection_Issue233_DeniedNonShared verifies
// that an authenticated user without manage_connections cannot create a
// non-shared connection. The previous behavior silently allowed this;
// the fix returns 403 with the canonical wording.
func TestConnectionHandler_CreateConnection_Issue233_DeniedNonShared(t *testing.T) {
	handler, userID, token, cleanup := setupIssue233CreateConnection(
		t, "issue233_create_unshared", nil)
	defer cleanup()

	body, _ := json.Marshal(ConnectionCreateRequest{
		Name:         "attempt",
		Host:         "db.example.com",
		Port:         5432,
		DatabaseName: "postgres",
		Username:     "alice",
		Password:     "secret",
		IsShared:     false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.createConnection(rec, req)

	assertForbiddenWithMessage(t, rec)
}

// TestConnectionHandler_CreateConnection_Issue233_DeniedShared verifies
// that the original (shared-only) denial behavior is preserved: an
// authenticated user without manage_connections cannot create a shared
// connection either. This locks in no regression for the #207 case.
func TestConnectionHandler_CreateConnection_Issue233_DeniedShared(t *testing.T) {
	handler, userID, token, cleanup := setupIssue233CreateConnection(
		t, "issue233_create_shared", nil)
	defer cleanup()

	body, _ := json.Marshal(ConnectionCreateRequest{
		Name:         "attempt",
		Host:         "db.example.com",
		Port:         5432,
		DatabaseName: "postgres",
		Username:     "alice",
		Password:     "secret",
		IsShared:     true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.createConnection(rec, req)

	assertForbiddenWithMessage(t, rec)
}

// TestConnectionHandler_CreateConnection_Issue233_DeniedSkipsDecode
// verifies that the 403 happens BEFORE JSON decoding: a malformed body
// that would otherwise trigger 400 ("Invalid request body") returns 403
// instead. This is the "no payload probing" property the gate ordering
// protects.
func TestConnectionHandler_CreateConnection_Issue233_DeniedSkipsDecode(t *testing.T) {
	handler, userID, token, cleanup := setupIssue233CreateConnection(
		t, "issue233_create_baddecode", nil)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.createConnection(rec, req)

	assertForbiddenWithMessage(t, rec)
}

// TestConnectionHandler_CreateConnection_Issue233_AdminAllowed confirms
// that a caller with manage_connections passes the gate. The handler
// then reaches the nil datastore and panics; the test recovers and
// asserts only that the gate did not reject the caller. Coverage of
// the post-gate write path lives in the integration suite gated on
// TEST_AI_WORKBENCH_SERVER.
func TestConnectionHandler_CreateConnection_Issue233_AdminAllowed(t *testing.T) {
	handler, userID, token, cleanup := setupIssue233CreateConnection(
		t, "issue233_create_admin",
		[]string{auth.PermManageConnections})
	defer cleanup()

	body, _ := json.Marshal(ConnectionCreateRequest{
		Name:         "admin-conn",
		Host:         "db.example.com",
		Port:         5432,
		DatabaseName: "postgres",
		Username:     "alice",
		Password:     "secret",
		IsShared:     false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	assertGatePassed(t, rec, func() {
		handler.createConnection(rec, req)
	})
}

// TestConnectionHandler_CreateConnection_Issue233_SuperuserAllowed
// confirms that the gate is satisfied by the superuser bypass. Like
// the admin-allowed case, the nil datastore panics post-gate and the
// test recovers; the assertion is that the gate did not reject.
func TestConnectionHandler_CreateConnection_Issue233_SuperuserAllowed(t *testing.T) {
	// Superuser bypass requires both a valid bearer token (so
	// getUserInfoCompat succeeds) and the superuser flag on the
	// request context (so HasAdminPermission returns true).
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	if err := store.CreateUser("issue233_super", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.SetUserSuperuser("issue233_super", true); err != nil {
		t.Fatalf("SetUserSuperuser: %v", err)
	}
	token, _, err := store.AuthenticateUser("issue233_super", "Password1234")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}

	checker := auth.NewRBACChecker(store)
	handler := NewConnectionHandler(nil, store, checker)

	body, _ := json.Marshal(ConnectionCreateRequest{
		Name:         "super-conn",
		Host:         "db.example.com",
		Port:         5432,
		DatabaseName: "postgres",
		Username:     "alice",
		Password:     "secret",
		IsShared:     false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	assertGatePassed(t, rec, func() {
		handler.createConnection(rec, req)
	})
}

// =============================================================================
// Regression Test Coverage for handleUpdateConnectionCluster (#233 follow-up)
//
// PUT /api/v1/connections/{id}/cluster previously gated only on
// CanAccessConnection, which is a visibility check. Any caller who
// could see the connection (e.g. via a group share) could re-home it
// between clusters; this is the same bug-class as #207 / #233 (a
// mutating handler missing the admin gate).
//
// The fix places HasAdminPermission(manage_connections) AFTER the
// existing CanAccessConnection check (so non-visible callers still
// learn nothing about the connection's existence) and BEFORE
// DecodeJSONBody (so denied callers cannot probe payload shape via
// validation error messages). The gate uses the same canonical 403
// wording used elsewhere in connection_handlers.go and
// cluster_handlers.go: "Permission denied: requires manage_connections
// permission".
//
// The tests below exercise:
//
//   - Denied: a non-admin caller with visibility on the connection
//     (the nil sharing lookup fn means the visibility check returns
//     true) cannot reassign the connection; the response is 403 with
//     the canonical wording.
//   - Denied-skips-decode: an invalid JSON body still returns 403
//     (not 400) because the gate fires before DecodeJSONBody.
//   - Admin allowed: a caller with manage_connections passes the gate;
//     the nil datastore panics post-gate and the test recovers.
//   - Superuser allowed: same shape as admin allowed but via the
//     superuser bypass.
// =============================================================================

// setupIssue233UpdateConnectionCluster builds a ConnectionHandler with a
// real RBACChecker and a real auth store, then registers a user with
// the caller-supplied permission set. The visibility check
// (CanAccessConnection) is configured to pass: the auth store has no
// group assignments for the test connection ID and the sharing lookup
// function is nil, so the visibility check returns true. The gate is
// the only protection between the caller and the datastore mutation.
func setupIssue233UpdateConnectionCluster(
	t *testing.T,
	username string,
	permissions []string,
) (*ConnectionHandler, int64, string, func()) {
	t.Helper()

	_, store, cleanup := createTestRBACHandler(t)
	var userID int64
	if len(permissions) == 0 {
		userID = newIssue207UnprivilegedUser(t, store, username)
	} else {
		userID = setupUserWithPermissions(t, store, username, permissions)
	}

	token, _, err := store.AuthenticateUser(username, "Password1234")
	if err != nil {
		cleanup()
		t.Fatalf("Failed to authenticate test user %s: %v", username, err)
	}

	checker := auth.NewRBACChecker(store)
	handler := NewConnectionHandler(nil, store, checker)

	return handler, userID, token, cleanup
}

// TestConnectionHandler_UpdateConnectionCluster_Issue233 groups the
// regression cases for the PUT /api/v1/connections/{id}/cluster gate
// added as a follow-up to #233.
func TestConnectionHandler_UpdateConnectionCluster_Issue233(t *testing.T) {
	const testConnectionID = 4242

	t.Run("DeniedVisibleButNotAdmin", func(t *testing.T) {
		handler, userID, token, cleanup := setupIssue233UpdateConnectionCluster(
			t, "issue233_updatecluster_denied", nil)
		defer cleanup()

		clusterID := 1
		body, _ := json.Marshal(ConnectionClusterUpdateRequest{
			ClusterID:        &clusterID,
			MembershipSource: "manual",
		})
		req := httptest.NewRequest(http.MethodPut,
			"/api/v1/connections/4242/cluster", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withBearer(req, token)
		req = withUser(req, userID)
		rec := httptest.NewRecorder()

		handler.handleUpdateConnectionCluster(rec, req, testConnectionID)

		assertForbiddenWithMessage(t, rec)
	})

	t.Run("DeniedSkipsDecode", func(t *testing.T) {
		handler, userID, token, cleanup := setupIssue233UpdateConnectionCluster(
			t, "issue233_updatecluster_baddecode", nil)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPut,
			"/api/v1/connections/4242/cluster",
			bytes.NewBufferString("not json"))
		req.Header.Set("Content-Type", "application/json")
		req = withBearer(req, token)
		req = withUser(req, userID)
		rec := httptest.NewRecorder()

		handler.handleUpdateConnectionCluster(rec, req, testConnectionID)

		assertForbiddenWithMessage(t, rec)
	})

	t.Run("AdminAllowed", func(t *testing.T) {
		handler, userID, token, cleanup := setupIssue233UpdateConnectionCluster(
			t, "issue233_updatecluster_admin",
			[]string{auth.PermManageConnections})
		defer cleanup()

		clusterID := 1
		body, _ := json.Marshal(ConnectionClusterUpdateRequest{
			ClusterID:        &clusterID,
			MembershipSource: "manual",
		})
		req := httptest.NewRequest(http.MethodPut,
			"/api/v1/connections/4242/cluster", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withBearer(req, token)
		req = withUser(req, userID)
		rec := httptest.NewRecorder()

		assertGatePassed(t, rec, func() {
			handler.handleUpdateConnectionCluster(rec, req, testConnectionID)
		})
	})

	t.Run("SuperuserAllowed", func(t *testing.T) {
		// Superuser bypass requires both a valid bearer token (so the
		// auth path through getUserInfoCompat succeeds when invoked)
		// and the superuser flag on the request context (so
		// HasAdminPermission returns true).
		_, store, cleanup := createTestRBACHandler(t)
		defer cleanup()

		if err := store.CreateUser("issue233_updatecluster_super",
			"Password1234", "", "", ""); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if err := store.SetUserSuperuser(
			"issue233_updatecluster_super", true); err != nil {
			t.Fatalf("SetUserSuperuser: %v", err)
		}
		token, _, err := store.AuthenticateUser(
			"issue233_updatecluster_super", "Password1234")
		if err != nil {
			t.Fatalf("AuthenticateUser: %v", err)
		}

		checker := auth.NewRBACChecker(store)
		handler := NewConnectionHandler(nil, store, checker)

		clusterID := 1
		body, _ := json.Marshal(ConnectionClusterUpdateRequest{
			ClusterID:        &clusterID,
			MembershipSource: "manual",
		})
		req := httptest.NewRequest(http.MethodPut,
			"/api/v1/connections/4242/cluster", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withBearer(req, token)
		req = withSuperuser(req)
		rec := httptest.NewRecorder()

		assertGatePassed(t, rec, func() {
			handler.handleUpdateConnectionCluster(rec, req, testConnectionID)
		})
	})
}
