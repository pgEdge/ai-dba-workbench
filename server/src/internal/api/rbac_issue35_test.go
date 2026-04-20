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
// AlertHandler.handleAlertCounts — production-code constraint
//
// handleAlertCounts invokes h.datastore.GetAlertCounts BEFORE the RBAC
// filter runs (see alert_handlers.go:172). With a nil datastore the
// handler panics before reaching the VisibleConnectionIDs call, so a
// handler-level RBAC test is not meaningful without a real or mocked
// datastore.
//
// The filtering logic itself is covered indirectly via
// TestVisibleConnectionIDs_* in auth/access_test.go. A full handler-
// level test of handleAlertCounts would require a mocked Datastore
// (pgxmock or an interface extraction), which the task forbids.
//
// Leaving this commentary in place so a future refactor can move the
// RBAC check to run BEFORE GetAlertCounts (safe because the counts are
// projected through VisibleConnectionIDs anyway) and pick up a real
// regression test here.
// =============================================================================

// =============================================================================
// AlertHandler mutation handlers — production-code constraint
//
// acknowledgeAlert, unacknowledgeAlert, and handleSaveAnalysis call
// h.datastore.GetAlertConnectionID BEFORE the RBAC check. The lookup is
// intrinsic to the flow (the handler must know which connection owns
// the alert before authorizing) and cannot be elided. With a nil
// datastore the lookup panics before RBAC, so the handler-level 403
// path cannot be asserted without a real or mocked datastore.
//
// Coverage for the RBAC call itself is via the per-connection tests
// above (same rbacChecker.CanAccessConnection code path). A full
// handler-level test would require injecting a stub that returns a
// controllable (connectionID, error) pair for GetAlertConnectionID.
// The task forbids adding such infrastructure.
// =============================================================================

// =============================================================================
// ClusterHandler.getClusterTopology — production-code constraint
//
// getClusterTopology calls h.datastore.RefreshClusterAssignments and
// h.datastore.GetClusterTopology BEFORE any RBAC filtering. With a nil
// datastore the handler panics before VisibleConnectionIDs is reached,
// so the handler-level pruning behavior cannot be asserted here.
//
// The filtering logic that IS exercised once the datastore returns is
// covered by the filterTopologyByVisibility unit tests already in
// cluster_handlers_test.go. That is the meaningful regression surface
// because filterTopologyByVisibility is the pure function that decides
// which groups, clusters, and servers to drop.
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
