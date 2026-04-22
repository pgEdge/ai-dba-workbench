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
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Regression Test Coverage for GitHub Issue #35
//
// Issue #35 was an access-control leak in which non-owner users could see
// or act on unshared connections. The production fix added RBAC gates to
// several HTTP handlers: single-resource handlers call
// auth.RBACChecker.CanAccessConnection (returning 403 on denial) and list
// handlers call auth.RBACChecker.VisibleConnectionIDs to filter results.
//
// The tests in this file exercise those gates at the HTTP handler
// boundary. They use a nil datastore so that:
//
//   - For handlers whose RBAC gate runs BEFORE any datastore call, a
//     denied request never reaches the datastore and can be asserted
//     purely from the response code.
//   - For the same handlers, a positive request triggers a nil-pointer
//     panic when the handler reaches the datastore. The tests recover
//     from the panic and verify that the RBAC gate did NOT write a 403
//     (and did NOT write a shortcut empty body, for list handlers).
//
// Handlers whose RBAC filter runs AFTER the datastore call are noted
// below and are covered elsewhere (see the handler-level report
// accompanying these tests).
// =============================================================================

// rbacUnsharedConnID is the connection ID used throughout these tests as
// the resource owned by "alice" and intentionally NOT shared. A
// distinctive value makes it easy to spot in failing diagnostics.
const rbacUnsharedConnID = 42

// stubVisibilityLister is a test-only ConnectionVisibilityLister that
// returns a fixed list of connection visibility records. It mirrors the
// in-package stub used by auth/access_test.go so handler tests can
// exercise VisibleConnectionIDs without a real datastore.
type stubVisibilityLister struct {
	connections []auth.ConnectionVisibilityInfo
}

func (s *stubVisibilityLister) GetAllConnections(_ context.Context) ([]auth.ConnectionVisibilityInfo, error) {
	return s.connections, nil
}

// newTestUser creates a user and returns its ID.
func newTestUser(t *testing.T, store *auth.AuthStore, username string) int64 {
	t.Helper()
	if err := store.CreateUser(username, "Password1", "", "", ""); err != nil {
		t.Fatalf("CreateUser %s: %v", username, err)
	}
	userID, err := store.GetUserID(username)
	if err != nil {
		t.Fatalf("GetUserID %s: %v", username, err)
	}
	return userID
}

// newGroupGrantedUser creates a user, places them in a fresh group, and
// grants the given connection ID at the given access level to that
// group. Returns the user ID.
func newGroupGrantedUser(t *testing.T, store *auth.AuthStore, username string, connID int, level string) int64 {
	t.Helper()
	userID := newTestUser(t, store, username)
	groupID, err := store.CreateGroup(username+"_group", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.AddUserToGroup(groupID, userID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if err := store.GrantConnectionPrivilege(groupID, connID, level); err != nil {
		t.Fatalf("GrantConnectionPrivilege: %v", err)
	}
	return userID
}

// noRespPanic invokes fn and recovers any panic so the test can keep
// running. With a nil datastore, production handlers panic AFTER the
// RBAC gate; recovering lets callers assert on rec.Code to confirm the
// gate did not deny.
func noRespPanic(fn func()) {
	defer func() { _ = recover() }() //nolint:errcheck // nil-datastore panic is expected
	fn()
}

// requireNotForbidden fails the test if rec.Code is 403. Used in
// positive-path assertions where the handler would panic after the gate
// passes; the status code is either the default 200 (nothing written
// before panic) or whatever the handler wrote before panicking.
func requireNotForbidden(t *testing.T, rec *httptest.ResponseRecorder, label string) {
	t.Helper()
	if rec.Code == http.StatusForbidden {
		t.Errorf("%s: RBAC gate denied with 403 unexpectedly. Body: %s",
			label, rec.Body.String())
	}
}

// =============================================================================
// ConnectionHandler per-connection handlers
//
// listDatabases, getConnectionContext, handleGetConnectionCluster, and
// handleUpdateConnectionCluster all call rc.CanAccessConnection BEFORE
// any datastore call. A nil datastore is therefore safe for both the 403
// and positive paths: denial returns 403; a pass leaves the response
// untouched and the test recovers from the nil-pointer panic that
// follows.
// =============================================================================

// connResourceInvoker is the minimal shape of a per-connection handler
// method on ConnectionHandler. Each case in the table below supplies one
// of these to parameterize the common setup.
type connResourceInvoker func(h *ConnectionHandler, w http.ResponseWriter, r *http.Request, id int)

type connResourceCase struct {
	name        string
	method      string
	urlTemplate string // %d substituted with the connection ID
	// body is sent for PUT requests; GET leaves it empty.
	body    []byte
	invoker connResourceInvoker
}

var connResourceHandlers = []connResourceCase{
	{
		name:        "listDatabases",
		method:      http.MethodGet,
		urlTemplate: "/api/v1/connections/%d/databases",
		invoker: func(h *ConnectionHandler, w http.ResponseWriter, r *http.Request, id int) {
			h.listDatabases(w, r, id)
		},
	},
	{
		name:        "getConnectionContext",
		method:      http.MethodGet,
		urlTemplate: "/api/v1/connections/%d/context",
		invoker: func(h *ConnectionHandler, w http.ResponseWriter, r *http.Request, id int) {
			h.getConnectionContext(w, r, id)
		},
	},
	{
		name:        "handleGetConnectionCluster",
		method:      http.MethodGet,
		urlTemplate: "/api/v1/connections/%d/cluster",
		invoker: func(h *ConnectionHandler, w http.ResponseWriter, r *http.Request, id int) {
			h.handleGetConnectionCluster(w, r, id)
		},
	},
	{
		name:        "handleUpdateConnectionCluster",
		method:      http.MethodPut,
		urlTemplate: "/api/v1/connections/%d/cluster",
		body:        []byte(`{"cluster_id": 7, "membership_source": "manual"}`),
		invoker: func(h *ConnectionHandler, w http.ResponseWriter, r *http.Request, id int) {
			h.handleUpdateConnectionCluster(w, r, id)
		},
	},
}

// TestConnectionHandler_PerConnection_NonOwnerUnshared_403 verifies every
// per-connection handler returns 403 when a non-owner user with no group
// grants attempts to access an unshared connection owned by someone else.
func TestConnectionHandler_PerConnection_NonOwnerUnshared_403(t *testing.T) {
	for _, tc := range connResourceHandlers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			bobID := newTestUser(t, store, "bob")

			checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
			handler := NewConnectionHandler(nil, store, checker)

			url := fmtURL(tc.urlTemplate, rbacUnsharedConnID)
			req := httptest.NewRequest(tc.method, url, bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, bobID)
			req = withUsername(req, "bob")
			rec := httptest.NewRecorder()

			tc.invoker(handler, rec, req, rbacUnsharedConnID)

			requireStatus(t, rec, http.StatusForbidden)
		})
	}
}

// TestConnectionHandler_PerConnection_Owner_NotDenied verifies every
// per-connection handler clears the RBAC gate for the connection's owner.
// With a nil datastore the handler panics after the gate; the test
// recovers and asserts the response code never became 403.
func TestConnectionHandler_PerConnection_Owner_NotDenied(t *testing.T) {
	for _, tc := range connResourceHandlers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			aliceID := newTestUser(t, store, "alice")

			checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
			handler := NewConnectionHandler(nil, store, checker)

			url := fmtURL(tc.urlTemplate, rbacUnsharedConnID)
			req := httptest.NewRequest(tc.method, url, bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, aliceID)
			req = withUsername(req, "alice")
			rec := httptest.NewRecorder()

			noRespPanic(func() {
				tc.invoker(handler, rec, req, rbacUnsharedConnID)
			})

			requireNotForbidden(t, rec, tc.name+"/owner")
		})
	}
}

// TestConnectionHandler_PerConnection_SharedNonOwner_NotDenied verifies a
// non-owner user reaching a shared connection is not denied.
func TestConnectionHandler_PerConnection_SharedNonOwner_NotDenied(t *testing.T) {
	for _, tc := range connResourceHandlers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			bobID := newTestUser(t, store, "bob")

			checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", true)
			handler := NewConnectionHandler(nil, store, checker)

			url := fmtURL(tc.urlTemplate, rbacUnsharedConnID)
			req := httptest.NewRequest(tc.method, url, bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, bobID)
			req = withUsername(req, "bob")
			rec := httptest.NewRecorder()

			noRespPanic(func() {
				tc.invoker(handler, rec, req, rbacUnsharedConnID)
			})

			requireNotForbidden(t, rec, tc.name+"/shared")
		})
	}
}

// TestConnectionHandler_PerConnection_GroupGranted_NotDenied verifies a
// user with an explicit group grant clears the RBAC gate even when the
// connection is unshared and owned by someone else.
func TestConnectionHandler_PerConnection_GroupGranted_NotDenied(t *testing.T) {
	for _, tc := range connResourceHandlers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			bobID := newGroupGrantedUser(t, store, "bob",
				rbacUnsharedConnID, auth.AccessLevelReadWrite)

			checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
			handler := NewConnectionHandler(nil, store, checker)

			url := fmtURL(tc.urlTemplate, rbacUnsharedConnID)
			req := httptest.NewRequest(tc.method, url, bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, bobID)
			req = withUsername(req, "bob")
			rec := httptest.NewRecorder()

			noRespPanic(func() {
				tc.invoker(handler, rec, req, rbacUnsharedConnID)
			})

			requireNotForbidden(t, rec, tc.name+"/group-granted")
		})
	}
}

// TestConnectionHandler_PerConnection_Superuser_NotDenied verifies a
// superuser clears the RBAC gate for unshared non-owned connections.
func TestConnectionHandler_PerConnection_Superuser_NotDenied(t *testing.T) {
	for _, tc := range connResourceHandlers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
			handler := NewConnectionHandler(nil, store, checker)

			url := fmtURL(tc.urlTemplate, rbacUnsharedConnID)
			req := httptest.NewRequest(tc.method, url, bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = withSuperuser(req)
			rec := httptest.NewRecorder()

			noRespPanic(func() {
				tc.invoker(handler, rec, req, rbacUnsharedConnID)
			})

			requireNotForbidden(t, rec, tc.name+"/superuser")
		})
	}
}

// TestConnectionHandler_UpdateConnectionCluster_NoDatastoreSideEffect is
// the explicit "no mutation on 403" check required by the task. The
// handler calls CanAccessConnection before any datastore mutation; with
// a nil datastore we confirm the gate denies before any state change
// could land (the test would panic if the gate were bypassed and the
// datastore call attempted).
func TestConnectionHandler_UpdateConnectionCluster_NoDatastoreSideEffect(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newTestUser(t, store, "bob")

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewConnectionHandler(nil, store, checker)

	body := []byte(`{"cluster_id": 7, "membership_source": "manual"}`)
	req := httptest.NewRequest(http.MethodPut,
		fmtURL("/api/v1/connections/%d/cluster", rbacUnsharedConnID),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, bobID)
	req = withUsername(req, "bob")
	rec := httptest.NewRecorder()

	// If the RBAC gate passed, the nil datastore call would panic. We
	// do NOT recover here: a panic would mean the gate was bypassed and
	// AssignConnectionToCluster (a mutation) was reached.
	handler.handleUpdateConnectionCluster(rec, req, rbacUnsharedConnID)

	requireStatus(t, rec, http.StatusForbidden)
}

// =============================================================================
// Current-connection handlers
//
// setCurrentConnection calls CanAccessConnection BEFORE writing to the
// auth store, so we can exercise it with a nil datastore and verify via
// the auth store that no session was persisted on denial. After that the
// handler calls h.datastore.GetConnection, which would panic; the 403
// path never reaches that call.
//
// getCurrentConnection reads the session from the auth store first, so
// it also reaches the RBAC gate with a nil datastore.
// =============================================================================

// withBearerRaw installs a raw bearer token on the request so
// GetTokenHashFromRequest can derive a token hash identical to the one
// SetConnectionSession was called with. The caller supplies both
// rawToken and tokenHash derived from auth.GetTokenHashByRawToken.
func withBearerRaw(req *http.Request, rawToken string) *http.Request {
	req.Header.Set("Authorization", "Bearer "+rawToken)
	return req
}

func TestConnectionHandler_SetCurrentConnection_NonOwnerUnshared_403(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newTestUser(t, store, "bob")
	rawToken, _, err := store.AuthenticateUser("bob", "Password1")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	tokenHash := auth.GetTokenHashByRawToken(rawToken)

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewConnectionHandler(nil, store, checker)

	body, _ := json.Marshal(CurrentConnectionRequest{ConnectionID: rbacUnsharedConnID})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/current",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, bobID)
	req = withUsername(req, "bob")
	req = withBearerRaw(req, rawToken)
	rec := httptest.NewRecorder()

	handler.setCurrentConnection(rec, req, tokenHash)

	requireStatus(t, rec, http.StatusForbidden)

	// Verify no session was written: SetConnectionSession should NOT
	// have been called on the 403 path.
	session, err := store.GetConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session != nil {
		t.Errorf("Expected no session after 403, got %+v", session)
	}
}

func TestConnectionHandler_SetCurrentConnection_Owner_NotDenied(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	aliceID := newTestUser(t, store, "alice")
	rawToken, _, err := store.AuthenticateUser("alice", "Password1")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	tokenHash := auth.GetTokenHashByRawToken(rawToken)

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewConnectionHandler(nil, store, checker)

	body, _ := json.Marshal(CurrentConnectionRequest{ConnectionID: rbacUnsharedConnID})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/current",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, aliceID)
	req = withUsername(req, "alice")
	req = withBearerRaw(req, rawToken)
	rec := httptest.NewRecorder()

	noRespPanic(func() {
		handler.setCurrentConnection(rec, req, tokenHash)
	})

	requireNotForbidden(t, rec, "setCurrentConnection/owner")
}

func TestConnectionHandler_SetCurrentConnection_SharedNonOwner_NotDenied(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newTestUser(t, store, "bob")
	rawToken, _, err := store.AuthenticateUser("bob", "Password1")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	tokenHash := auth.GetTokenHashByRawToken(rawToken)

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", true)
	handler := NewConnectionHandler(nil, store, checker)

	body, _ := json.Marshal(CurrentConnectionRequest{ConnectionID: rbacUnsharedConnID})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/current",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, bobID)
	req = withUsername(req, "bob")
	req = withBearerRaw(req, rawToken)
	rec := httptest.NewRecorder()

	noRespPanic(func() {
		handler.setCurrentConnection(rec, req, tokenHash)
	})

	requireNotForbidden(t, rec, "setCurrentConnection/shared")
}

func TestConnectionHandler_GetCurrentConnection_NonOwnerUnshared_403(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newTestUser(t, store, "bob")
	rawToken, _, err := store.AuthenticateUser("bob", "Password1")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	tokenHash := auth.GetTokenHashByRawToken(rawToken)

	// Seed a session pointing at the unshared connection Bob does not
	// own. In production a token could hold a stale selection if
	// sharing was revoked between calls; the RBAC check on every GET is
	// the safety net.
	if err := store.SetConnectionSession(tokenHash, rbacUnsharedConnID, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewConnectionHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/current", nil)
	req = withUser(req, bobID)
	req = withUsername(req, "bob")
	req = withBearerRaw(req, rawToken)
	rec := httptest.NewRecorder()

	handler.getCurrentConnection(rec, req, tokenHash)

	requireStatus(t, rec, http.StatusForbidden)
}

func TestConnectionHandler_GetCurrentConnection_Owner_NotDenied(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	aliceID := newTestUser(t, store, "alice")
	rawToken, _, err := store.AuthenticateUser("alice", "Password1")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	tokenHash := auth.GetTokenHashByRawToken(rawToken)

	if err := store.SetConnectionSession(tokenHash, rbacUnsharedConnID, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewConnectionHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/current", nil)
	req = withUser(req, aliceID)
	req = withUsername(req, "alice")
	req = withBearerRaw(req, rawToken)
	rec := httptest.NewRecorder()

	noRespPanic(func() {
		handler.getCurrentConnection(rec, req, tokenHash)
	})

	requireNotForbidden(t, rec, "getCurrentConnection/owner")
}

func TestConnectionHandler_GetCurrentConnection_SharedNonOwner_NotDenied(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newTestUser(t, store, "bob")
	rawToken, _, err := store.AuthenticateUser("bob", "Password1")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	tokenHash := auth.GetTokenHashByRawToken(rawToken)

	if err := store.SetConnectionSession(tokenHash, rbacUnsharedConnID, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", true)
	handler := NewConnectionHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/current", nil)
	req = withUser(req, bobID)
	req = withUsername(req, "bob")
	req = withBearerRaw(req, rawToken)
	rec := httptest.NewRecorder()

	noRespPanic(func() {
		handler.getCurrentConnection(rec, req, tokenHash)
	})

	requireNotForbidden(t, rec, "getCurrentConnection/shared")
}

// =============================================================================
// AlertHandler.handleAlerts — list-handler filtering
//
// handleAlerts calls VisibleConnectionIDs BEFORE the datastore; with a
// nil datastore, newConnectionVisibilityLister returns nil and the
// auth-store-backed path still resolves correctly.
//
// - Zero-grant non-owner user: VisibleConnectionIDs returns an empty
//   set and the handler short-circuits to {"alerts":[], "total":0}
//   without touching the datastore.
// - Group-granted user: VisibleConnectionIDs returns the granted IDs
//   and the handler proceeds to h.datastore.GetAlerts, which panics
//   against a nil datastore. The test recovers and verifies the empty
//   shortcut did NOT fire.
// =============================================================================

func TestAlertHandler_HandleAlerts_NonOwnerUnshared_EmptyResult(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newTestUser(t, store, "bob")

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewAlertHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	req = withUser(req, bobID)
	req = withUsername(req, "bob")
	rec := httptest.NewRecorder()

	handler.handleAlerts(rec, req)

	requireStatus(t, rec, http.StatusOK)

	var body database.AlertListResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode response: %v", err)
	}
	if len(body.Alerts) != 0 {
		t.Errorf("Expected zero alerts, got %d", len(body.Alerts))
	}
	if body.Total != 0 {
		t.Errorf("Expected Total=0, got %d", body.Total)
	}
}

func TestAlertHandler_HandleAlerts_FilterToUnshared_EmptyResult(t *testing.T) {
	// Even when the caller requests a specific unshared connection via
	// ?connection_id=..., the handler must strip the filter because that
	// connection is not visible to bob.
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newTestUser(t, store, "bob")

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewAlertHandler(nil, store, checker)

	url := "/api/v1/alerts?connection_id=" + strconv.Itoa(rbacUnsharedConnID)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req = withUser(req, bobID)
	req = withUsername(req, "bob")
	rec := httptest.NewRecorder()

	handler.handleAlerts(rec, req)

	requireStatus(t, rec, http.StatusOK)

	var body database.AlertListResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode response: %v", err)
	}
	if len(body.Alerts) != 0 || body.Total != 0 {
		t.Errorf("Expected empty result, got %+v", body)
	}
}

func TestAlertHandler_HandleAlerts_GroupGranted_ProceedsPastGate(t *testing.T) {
	// Positive path: a group grant puts the unshared connection in
	// bob's visible set. The handler should NOT short-circuit to the
	// empty JSON body; it should reach h.datastore.GetAlerts (which
	// panics against the nil datastore; we recover and assert the
	// shortcut did not fire).
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newGroupGrantedUser(t, store, "bob",
		rbacUnsharedConnID, auth.AccessLevelRead)

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewAlertHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	req = withUser(req, bobID)
	req = withUsername(req, "bob")
	rec := httptest.NewRecorder()

	noRespPanic(func() {
		handler.handleAlerts(rec, req)
	})

	// The empty-set shortcut would write "{"alerts":[],"total":0}" with
	// a 200 status. A positive path must have an empty body because the
	// panic happened BEFORE any write reached the response.
	if rec.Body.Len() != 0 {
		t.Errorf("Expected empty body (panic-before-write), got %q",
			rec.Body.String())
	}
	requireNotForbidden(t, rec, "handleAlerts/group-granted")
}

// =============================================================================
// AlertHandler.handleAlertCounts — issue #67 regression coverage
//
// The issue #67 refactor moved VisibleConnectionIDs AHEAD of
// GetAlertCounts so a zero-grant caller short-circuits to the empty
// counts response without touching the datastore. With a nil datastore
// we can now assert that behavior purely from the HTTP boundary: denial
// returns 200 + {total:0, by_server:{}} without panicking on the
// datastore call.
//
// The group-granted positive path panics against the nil datastore at
// GetAlertCounts; the test recovers and verifies that the empty
// short-circuit did NOT fire.
// =============================================================================

// TestAlertHandler_HandleAlertCounts_NonOwnerUnshared_EmptyResult verifies
// that a zero-grant caller receives {total:0, by_server:{}} without
// invoking GetAlertCounts against the datastore.
func TestAlertHandler_HandleAlertCounts_NonOwnerUnshared_EmptyResult(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newTestUser(t, store, "bob")

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewAlertHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/counts", nil)
	req = withUser(req, bobID)
	req = withUsername(req, "bob")
	rec := httptest.NewRecorder()

	handler.handleAlertCounts(rec, req)

	requireStatus(t, rec, http.StatusOK)

	var body database.AlertCountsResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode response: %v", err)
	}
	if body.Total != 0 {
		t.Errorf("Expected Total=0, got %d", body.Total)
	}
	if len(body.ByServer) != 0 {
		t.Errorf("Expected empty ByServer, got %+v", body.ByServer)
	}
}

// TestAlertHandler_HandleAlertCounts_GroupGranted_ProceedsPastGate
// verifies that a group-granted caller is not short-circuited by the
// zero-grant gate and reaches GetAlertCounts (which panics against the
// nil datastore; the test recovers and asserts the empty JSON body was
// NOT written).
func TestAlertHandler_HandleAlertCounts_GroupGranted_ProceedsPastGate(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newGroupGrantedUser(t, store, "bob",
		rbacUnsharedConnID, auth.AccessLevelRead)

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewAlertHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/counts", nil)
	req = withUser(req, bobID)
	req = withUsername(req, "bob")
	rec := httptest.NewRecorder()

	noRespPanic(func() {
		handler.handleAlertCounts(rec, req)
	})

	// The zero-grant shortcut writes a 200 with {total:0, by_server:{}}.
	// A group-granted caller must not trip that shortcut: the panic
	// inside GetAlertCounts happens BEFORE any write reaches the
	// response, so the body must be empty.
	if rec.Body.Len() != 0 {
		t.Errorf("Expected empty body (panic-before-write), got %q",
			rec.Body.String())
	}
	requireNotForbidden(t, rec, "handleAlertCounts/group-granted")
}

// TestAlertHandler_HandleAlertCounts_Superuser_ProceedsPastGate verifies
// that a superuser request is NOT short-circuited by the zero-grant
// gate. VisibleConnectionIDs returns allConnections=true for a
// superuser; the handler must proceed to GetAlertCounts (panic against
// nil datastore) without writing the empty shortcut.
func TestAlertHandler_HandleAlertCounts_Superuser_ProceedsPastGate(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewAlertHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/counts", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	noRespPanic(func() {
		handler.handleAlertCounts(rec, req)
	})

	if rec.Body.Len() != 0 {
		t.Errorf("Expected empty body (panic-before-write), got %q",
			rec.Body.String())
	}
	requireNotForbidden(t, rec, "handleAlertCounts/superuser")
}

// =============================================================================
// AlertHandler mutation handlers — issue #67 regression coverage
//
// The issue #67 refactor introduced a narrow alertConnectionResolver
// interface so tests can inject a fake that returns a known connection
// ID without stubbing the full datastore. That unlocks HTTP-level tests
// for the three mutation handlers: fakeAlertResolver returns
// rbacUnsharedConnID; the handler then runs CanAccessConnection; a
// denied caller hits 403 before any datastore mutation call executes
// (the mutation call would panic against the nil datastore; we do NOT
// recover from that because a panic would mean the gate was bypassed).
// =============================================================================

// fakeAlertResolver is a deterministic alertConnectionResolver for
// tests. It returns a fixed connection ID for every alert ID and tracks
// how many times GetAlertConnectionID was called so tests can assert
// that the resolver DID run (the RBAC check depends on it).
type fakeAlertResolver struct {
	connID int
	calls  int
	err    error
}

func (f *fakeAlertResolver) GetAlertConnectionID(_ context.Context, _ int64) (int, error) {
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	return f.connID, nil
}

// alertMutationInvoker is the minimal shape of a mutation handler method
// on AlertHandler. Each case in the table below supplies one of these
// to parameterize the common setup.
type alertMutationInvoker func(h *AlertHandler, w http.ResponseWriter, r *http.Request)

type alertMutationCase struct {
	name    string
	method  string
	url     string
	body    []byte
	invoker alertMutationInvoker
}

var alertMutationHandlers = []alertMutationCase{
	{
		name:   "acknowledgeAlert",
		method: http.MethodPost,
		url:    "/api/v1/alerts/acknowledge",
		body:   []byte(`{"alert_id": 7, "message": "test"}`),
		invoker: func(h *AlertHandler, w http.ResponseWriter, r *http.Request) {
			h.acknowledgeAlert(w, r)
		},
	},
	{
		name:   "unacknowledgeAlert",
		method: http.MethodDelete,
		url:    "/api/v1/alerts/acknowledge?alert_id=7",
		invoker: func(h *AlertHandler, w http.ResponseWriter, r *http.Request) {
			h.unacknowledgeAlert(w, r)
		},
	},
	{
		name:   "handleSaveAnalysis",
		method: http.MethodPut,
		url:    "/api/v1/alerts/analysis",
		body:   []byte(`{"alert_id": 7, "analysis": "test analysis"}`),
		invoker: func(h *AlertHandler, w http.ResponseWriter, r *http.Request) {
			h.handleSaveAnalysis(w, r)
		},
	},
}

// TestAlertHandler_Mutation_NonOwnerUnshared_403 verifies every alert
// mutation handler returns 403 when a non-owner user with no group
// grants targets an unshared connection. The nil datastore would panic
// if the handler reached any mutation call after the RBAC gate; we do
// NOT recover here because a panic would indicate a gate bypass.
func TestAlertHandler_Mutation_NonOwnerUnshared_403(t *testing.T) {
	for _, tc := range alertMutationHandlers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			bobID := newTestUser(t, store, "bob")

			checker := mockSharingChecker(t, store,
				rbacUnsharedConnID, "alice", false)
			handler := NewAlertHandler(nil, store, checker)
			resolver := &fakeAlertResolver{connID: rbacUnsharedConnID}
			handler.setAlertResolver(resolver)

			req := httptest.NewRequest(tc.method, tc.url,
				bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, bobID)
			req = withUsername(req, "bob")
			rec := httptest.NewRecorder()

			tc.invoker(handler, rec, req)

			requireStatus(t, rec, http.StatusForbidden)

			if resolver.calls != 1 {
				t.Errorf("Expected resolver to run once, got %d calls",
					resolver.calls)
			}
		})
	}
}

// TestAlertHandler_Mutation_Owner_NotDenied verifies every alert
// mutation handler clears the RBAC gate for the connection's owner.
// With a nil datastore the handler panics after the gate when attempting
// the actual mutation; the test recovers and asserts the response code
// never became 403.
func TestAlertHandler_Mutation_Owner_NotDenied(t *testing.T) {
	for _, tc := range alertMutationHandlers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			aliceID := newTestUser(t, store, "alice")

			checker := mockSharingChecker(t, store,
				rbacUnsharedConnID, "alice", false)
			handler := NewAlertHandler(nil, store, checker)
			handler.setAlertResolver(&fakeAlertResolver{
				connID: rbacUnsharedConnID,
			})

			req := httptest.NewRequest(tc.method, tc.url,
				bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, aliceID)
			req = withUsername(req, "alice")
			rec := httptest.NewRecorder()

			noRespPanic(func() {
				tc.invoker(handler, rec, req)
			})

			requireNotForbidden(t, rec, tc.name+"/owner")
		})
	}
}

// TestAlertHandler_Mutation_SharedNonOwner_NotDenied verifies that a
// non-owner caller accessing a shared connection is not denied.
func TestAlertHandler_Mutation_SharedNonOwner_NotDenied(t *testing.T) {
	for _, tc := range alertMutationHandlers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			bobID := newTestUser(t, store, "bob")

			checker := mockSharingChecker(t, store,
				rbacUnsharedConnID, "alice", true)
			handler := NewAlertHandler(nil, store, checker)
			handler.setAlertResolver(&fakeAlertResolver{
				connID: rbacUnsharedConnID,
			})

			req := httptest.NewRequest(tc.method, tc.url,
				bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, bobID)
			req = withUsername(req, "bob")
			rec := httptest.NewRecorder()

			noRespPanic(func() {
				tc.invoker(handler, rec, req)
			})

			requireNotForbidden(t, rec, tc.name+"/shared")
		})
	}
}

// TestAlertHandler_Mutation_GroupGranted_NotDenied verifies that a user
// with an explicit group grant is not denied for an unshared connection.
func TestAlertHandler_Mutation_GroupGranted_NotDenied(t *testing.T) {
	for _, tc := range alertMutationHandlers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			bobID := newGroupGrantedUser(t, store, "bob",
				rbacUnsharedConnID, auth.AccessLevelReadWrite)

			checker := mockSharingChecker(t, store,
				rbacUnsharedConnID, "alice", false)
			handler := NewAlertHandler(nil, store, checker)
			handler.setAlertResolver(&fakeAlertResolver{
				connID: rbacUnsharedConnID,
			})

			req := httptest.NewRequest(tc.method, tc.url,
				bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, bobID)
			req = withUsername(req, "bob")
			rec := httptest.NewRecorder()

			noRespPanic(func() {
				tc.invoker(handler, rec, req)
			})

			requireNotForbidden(t, rec, tc.name+"/group-granted")
		})
	}
}

// TestAlertHandler_Mutation_Superuser_NotDenied verifies that a
// superuser is never denied, even against unshared non-owned resources.
func TestAlertHandler_Mutation_Superuser_NotDenied(t *testing.T) {
	for _, tc := range alertMutationHandlers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			checker := mockSharingChecker(t, store,
				rbacUnsharedConnID, "alice", false)
			handler := NewAlertHandler(nil, store, checker)
			handler.setAlertResolver(&fakeAlertResolver{
				connID: rbacUnsharedConnID,
			})

			req := httptest.NewRequest(tc.method, tc.url,
				bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = withSuperuser(req)
			rec := httptest.NewRecorder()

			noRespPanic(func() {
				tc.invoker(handler, rec, req)
			})

			requireNotForbidden(t, rec, tc.name+"/superuser")
		})
	}
}

// TestAlertHandler_Mutation_ResolverMissing_500 exercises the defensive
// branch that rejects a request when the resolver has not been
// configured. Production never hits this (the constructor wires the
// datastore), but the nil-interface case must fail safely rather than
// panic.
func TestAlertHandler_Mutation_ResolverMissing_500(t *testing.T) {
	for _, tc := range alertMutationHandlers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			bobID := newTestUser(t, store, "bob")

			checker := mockSharingChecker(t, store,
				rbacUnsharedConnID, "alice", false)
			handler := NewAlertHandler(nil, store, checker)
			// Deliberately leave alertResolver nil.

			req := httptest.NewRequest(tc.method, tc.url,
				bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, bobID)
			req = withUsername(req, "bob")
			rec := httptest.NewRecorder()

			tc.invoker(handler, rec, req)

			requireStatus(t, rec, http.StatusInternalServerError)
		})
	}
}

// TestAlertHandler_Mutation_ResolverError_404 exercises the branch that
// returns 404 when the resolver reports an error (e.g. the alert does
// not exist). This keeps parity with the old code path and ensures the
// RBAC gate does not fire for an unknown alert.
func TestAlertHandler_Mutation_ResolverError_404(t *testing.T) {
	for _, tc := range alertMutationHandlers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			bobID := newTestUser(t, store, "bob")

			checker := mockSharingChecker(t, store,
				rbacUnsharedConnID, "alice", false)
			handler := NewAlertHandler(nil, store, checker)
			handler.setAlertResolver(&fakeAlertResolver{
				err: errors.New("alert not found"),
			})

			req := httptest.NewRequest(tc.method, tc.url,
				bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, bobID)
			req = withUsername(req, "bob")
			rec := httptest.NewRecorder()

			tc.invoker(handler, rec, req)

			requireStatus(t, rec, http.StatusNotFound)
		})
	}
}

// =============================================================================
// ClusterHandler.getClusterTopology — issue #67 regression coverage
//
// The issue #67 refactor moved VisibleConnectionIDs AHEAD of
// RefreshClusterAssignments and GetClusterTopology so a zero-grant
// caller returns an empty topology without triggering any datastore
// work. With a nil datastore we can now assert that behavior purely
// from the HTTP boundary: denial returns 200 + []; no panic, no
// refresh, no topology read.
// =============================================================================

// TestClusterHandler_GetClusterTopology_NonOwnerUnshared_EmptyTopology
// verifies a zero-grant caller gets an empty topology array without
// invoking any datastore method on the handler.
func TestClusterHandler_GetClusterTopology_NonOwnerUnshared_EmptyTopology(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newTestUser(t, store, "bob")

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewClusterHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
	req = withUser(req, bobID)
	req = withUsername(req, "bob")
	rec := httptest.NewRecorder()

	handler.getClusterTopology(rec, req)

	requireStatus(t, rec, http.StatusOK)

	var body []database.TopologyGroup
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode response: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("Expected empty topology, got %d groups", len(body))
	}
}

// TestClusterHandler_GetClusterTopology_GroupGranted_ProceedsPastGate
// verifies a group-granted caller does NOT receive the zero-grant
// shortcut. The handler must proceed past the gate into
// RefreshClusterAssignments, which panics against the nil datastore;
// the test recovers and asserts the empty JSON body was NOT written.
func TestClusterHandler_GetClusterTopology_GroupGranted_ProceedsPastGate(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newGroupGrantedUser(t, store, "bob",
		rbacUnsharedConnID, auth.AccessLevelRead)

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewClusterHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
	req = withUser(req, bobID)
	req = withUsername(req, "bob")
	rec := httptest.NewRecorder()

	noRespPanic(func() {
		handler.getClusterTopology(rec, req)
	})

	// The zero-grant shortcut writes an empty array with a 200 status.
	// A group-granted caller must not trip that shortcut: the panic
	// inside RefreshClusterAssignments happens BEFORE any write reaches
	// the response.
	if rec.Body.Len() != 0 {
		t.Errorf("Expected empty body (panic-before-write), got %q",
			rec.Body.String())
	}
	requireNotForbidden(t, rec, "getClusterTopology/group-granted")
}

// TestClusterHandler_GetClusterTopology_Superuser_ProceedsPastGate
// verifies that a superuser is NOT short-circuited by the zero-grant
// gate; the handler proceeds into the datastore (which panics against
// the nil datastore).
func TestClusterHandler_GetClusterTopology_Superuser_ProceedsPastGate(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewClusterHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	noRespPanic(func() {
		handler.getClusterTopology(rec, req)
	})

	if rec.Body.Len() != 0 {
		t.Errorf("Expected empty body (panic-before-write), got %q",
			rec.Body.String())
	}
	requireNotForbidden(t, rec, "getClusterTopology/superuser")
}

// TestClusterHandler_GetClusterTopology_WildcardToken_ProceedsPastGate
// covers the wildcard-scoped token path. A token scoped to
// ConnectionIDAll with read access resolves allConnections=true; the
// handler must not short-circuit and must proceed into the datastore.
func TestClusterHandler_GetClusterTopology_WildcardToken_ProceedsPastGate(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	userID := newTestUser(t, store, "tok")
	// Grant the user the ConnectionIDAll wildcard through a group so the
	// effective privileges include the wildcard.
	groupID, err := store.CreateGroup("tok_group", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.AddUserToGroup(groupID, userID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if err := store.GrantConnectionPrivilege(groupID,
		auth.ConnectionIDAll, auth.AccessLevelRead); err != nil {
		t.Fatalf("GrantConnectionPrivilege: %v", err)
	}

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewClusterHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
	req = withUser(req, userID)
	req = withUsername(req, "tok")
	rec := httptest.NewRecorder()

	noRespPanic(func() {
		handler.getClusterTopology(rec, req)
	})

	if rec.Body.Len() != 0 {
		t.Errorf("Expected empty body (panic-before-write), got %q",
			rec.Body.String())
	}
	requireNotForbidden(t, rec, "getClusterTopology/wildcard")
}

// =============================================================================

// TestFilterTopologyByVisibility_Issue35_PrunesUnsharedNonOwned adds
// explicit coverage tying filterTopologyByVisibility back to issue #35:
// when the visibility set contains only the shared server's ID, every
// unshared non-owned server must be removed along with its empty
// cluster and empty group.
func TestFilterTopologyByVisibility_Issue35_PrunesUnsharedNonOwned(t *testing.T) {
	// Seed a topology with two clusters in one group and one cluster
	// in another group. Server 1 is "shared"; servers 2 and 3 are
	// "unshared non-owned".
	topology := []database.TopologyGroup{
		{
			ID:   "g1",
			Name: "Visible Group",
			Clusters: []database.TopologyCluster{
				{
					ID:   "c1",
					Name: "Mixed Cluster",
					Servers: []database.TopologyServerInfo{
						{ID: 1, Name: "shared-server"},
						{ID: 2, Name: "unshared-server"},
					},
				},
			},
		},
		{
			ID:   "g2",
			Name: "All-Hidden Group",
			Clusters: []database.TopologyCluster{
				{
					ID:   "c2",
					Name: "Hidden Cluster",
					Servers: []database.TopologyServerInfo{
						{ID: 3, Name: "also-unshared"},
					},
				},
			},
		},
	}

	// Only server 1 is visible; VisibleConnectionIDs produces this set
	// for a non-owner zero-grant user against a shared server 1.
	filtered := filterTopologyByVisibility(topology, []int{1})

	if len(filtered) != 1 {
		t.Fatalf("Expected 1 group after filter, got %d", len(filtered))
	}
	if filtered[0].ID != "g1" {
		t.Errorf("Expected g1, got %q", filtered[0].ID)
	}
	if len(filtered[0].Clusters) != 1 {
		t.Fatalf("Expected 1 cluster, got %d", len(filtered[0].Clusters))
	}
	if len(filtered[0].Clusters[0].Servers) != 1 {
		t.Fatalf("Expected 1 server, got %d",
			len(filtered[0].Clusters[0].Servers))
	}
	if filtered[0].Clusters[0].Servers[0].ID != 1 {
		t.Errorf("Expected only server 1, got %d",
			filtered[0].Clusters[0].Servers[0].ID)
	}
}

// TestFilterTopologyByVisibility_Issue35_AllHidden_EmptyTopology verifies
// that when no servers are visible, the topology is entirely pruned.
// This matches the non-owner-zero-grant case where VisibleConnectionIDs
// returns an empty set for a topology populated entirely with unshared
// connections owned by another user.
func TestFilterTopologyByVisibility_Issue35_AllHidden_EmptyTopology(t *testing.T) {
	topology := []database.TopologyGroup{
		{
			ID:   "g1",
			Name: "All Unshared",
			Clusters: []database.TopologyCluster{
				{
					ID:   "c1",
					Name: "Unshared Cluster",
					Servers: []database.TopologyServerInfo{
						{ID: 10, Name: "hidden-10"},
						{ID: 11, Name: "hidden-11"},
					},
				},
			},
		},
	}

	filtered := filterTopologyByVisibility(topology, []int{})

	if len(filtered) != 0 {
		t.Errorf("Expected empty topology, got %d groups", len(filtered))
	}
}

// =============================================================================
// VisibleConnectionIDs integration with a realistic lister (issue #35)
//
// These tests pair the handler-level filter logic with a stub lister so
// we can confirm end-to-end that a non-owner zero-grant user resolves
// to the empty visibility set when the only connection in the datastore
// is unshared and owned by someone else. This is the piece that
// handleAlertCounts and getClusterTopology rely on; the stub closes the
// coverage gap those handlers leave because of their datastore-first
// flow.
// =============================================================================

func TestVisibleConnectionIDs_Issue35_NonOwnerZeroGrant_EmptySet(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newTestUser(t, store, "bob")

	checker := auth.NewRBACChecker(store)

	lister := &stubVisibilityLister{
		connections: []auth.ConnectionVisibilityInfo{
			{ID: rbacUnsharedConnID, IsShared: false, OwnerUsername: "alice"},
		},
	}

	ctx := context.WithValue(context.Background(), auth.UserIDContextKey, bobID)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "bob")

	ids, all, err := checker.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		t.Fatalf("VisibleConnectionIDs: %v", err)
	}
	if all {
		t.Fatal("Expected allConnections=false for non-superuser zero-grant user")
	}
	if len(ids) != 0 {
		t.Errorf("Expected empty visibility set, got %v", ids)
	}
}

func TestVisibleConnectionIDs_Issue35_OwnerSeesOwnUnshared(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	aliceID := newTestUser(t, store, "alice")

	checker := auth.NewRBACChecker(store)

	lister := &stubVisibilityLister{
		connections: []auth.ConnectionVisibilityInfo{
			{ID: rbacUnsharedConnID, IsShared: false, OwnerUsername: "alice"},
		},
	}

	ctx := context.WithValue(context.Background(), auth.UserIDContextKey, aliceID)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "alice")

	ids, all, err := checker.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		t.Fatalf("VisibleConnectionIDs: %v", err)
	}
	if all {
		t.Fatal("Owner should not get allConnections")
	}
	if len(ids) != 1 || ids[0] != rbacUnsharedConnID {
		t.Errorf("Expected owner to see only %d, got %v",
			rbacUnsharedConnID, ids)
	}
}

func TestVisibleConnectionIDs_Issue35_SharedVisibleToNonOwner(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newTestUser(t, store, "bob")

	checker := auth.NewRBACChecker(store)

	// Seed two connections: one shared by alice (visible to bob) and
	// one unshared by alice (NOT visible to bob). The filter must keep
	// exactly the shared one.
	lister := &stubVisibilityLister{
		connections: []auth.ConnectionVisibilityInfo{
			{ID: 100, IsShared: true, OwnerUsername: "alice"},
			{ID: 101, IsShared: false, OwnerUsername: "alice"},
		},
	}

	ctx := context.WithValue(context.Background(), auth.UserIDContextKey, bobID)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "bob")

	ids, all, err := checker.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		t.Fatalf("VisibleConnectionIDs: %v", err)
	}
	if all {
		t.Fatal("Expected allConnections=false")
	}
	if len(ids) != 1 || ids[0] != 100 {
		t.Errorf("Expected only shared connection 100 visible, got %v", ids)
	}
}

// =============================================================================
// Helpers
// =============================================================================

// fmtURL builds an HTTP path by substituting a single connection ID
// into a "%d" placeholder. It is a thin wrapper that keeps the table
// entries readable.
func fmtURL(template string, id int) string {
	// strconv avoids pulling fmt into this hot path for a trivial sub.
	return replaceFirstPercentD(template, id)
}

func replaceFirstPercentD(template string, id int) string {
	idx := indexOfPercentD(template)
	if idx < 0 {
		return template
	}
	return template[:idx] + strconv.Itoa(id) + template[idx+2:]
}

func indexOfPercentD(s string) int {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '%' && s[i+1] == 'd' {
			return i
		}
	}
	return -1
}
