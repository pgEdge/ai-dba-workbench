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
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Regression tests for the second pass of GitHub issue #35 (RBAC leaks).
//
// These tests cover two narrow slices of the fix:
//
//   - The override list handlers (alert/probe/channel) at scope=server
//     return 404 when a non-owner zero-grant caller asks about an
//     unshared server. The gate runs before any datastore call, so the
//     nil-datastore harness from rbac_issue35_test.go suffices.
//
//   - The blackoutVisible helper implements the blackout getter's RBAC
//     decision. A pure unit test pins down the decision matrix for the
//     no-datastore branches (global and server-scoped blackouts). The
//     cluster/group branches call the datastore and are covered by the
//     integration tests accompanying issue #35.
// =============================================================================

// newBlackoutForTest constructs a Blackout populated only with the
// resource pointer fields. The rest are zero values because
// blackoutVisible only inspects the resource pointers.
func newBlackoutForTest(connID, clusterID, groupID *int) *database.Blackout {
	return &database.Blackout{
		ConnectionID: connID,
		ClusterID:    clusterID,
		GroupID:      groupID,
	}
}

// overrideListInvoker describes one override list handler under test.
type overrideListInvoker struct {
	name   string
	invoke func(store *auth.AuthStore, checker *auth.RBACChecker,
		w http.ResponseWriter, r *http.Request, scope string, scopeID int)
}

var overrideListInvokers = []overrideListInvoker{
	{
		name: "alert_override",
		invoke: func(store *auth.AuthStore, checker *auth.RBACChecker,
			w http.ResponseWriter, r *http.Request, scope string, scopeID int) {
			NewAlertOverrideHandler(nil, store, checker).listOverrides(w, r, scope, scopeID)
		},
	},
	{
		name: "probe_override",
		invoke: func(store *auth.AuthStore, checker *auth.RBACChecker,
			w http.ResponseWriter, r *http.Request, scope string, scopeID int) {
			NewProbeOverrideHandler(nil, store, checker).listOverrides(w, r, scope, scopeID)
		},
	},
	{
		name: "channel_override",
		invoke: func(store *auth.AuthStore, checker *auth.RBACChecker,
			w http.ResponseWriter, r *http.Request, scope string, scopeID int) {
			NewChannelOverrideHandler(nil, store, checker).listOverrides(w, r, scope, scopeID)
		},
	},
}

// TestOverrideListHandlers_ServerScope_NonOwnerUnshared_404 verifies that
// every override list handler returns 404 when a non-owner user with no
// grants asks for overrides attached to an unshared server owned by
// someone else. The gate runs before any datastore call so the nil
// datastore is never reached.
func TestOverrideListHandlers_ServerScope_NonOwnerUnshared_404(t *testing.T) {
	for _, tc := range overrideListInvokers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			bobID := newTestUser(t, store, "bob")
			checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)

			req := httptest.NewRequest(http.MethodGet,
				fmtURL("/api/v1/alert-overrides/server/%d", rbacUnsharedConnID), nil)
			req = withUser(req, bobID)
			req = withUsername(req, "bob")
			rec := httptest.NewRecorder()

			tc.invoke(store, checker, rec, req, "server", rbacUnsharedConnID)

			requireStatus(t, rec, http.StatusNotFound)
		})
	}
}

// TestOverrideListHandlers_ServerScope_Superuser_NotDenied verifies a
// superuser bypasses the gate. With a nil datastore the handler panics
// after the gate is cleared; noRespPanic recovers the panic before any
// response is written, leaving rec.Code at the httptest default (200).
// Asserting on the default status confirms the gate passed AND that no
// error response (403/404/500) was written before the datastore call.
func TestOverrideListHandlers_ServerScope_Superuser_NotDenied(t *testing.T) {
	for _, tc := range overrideListInvokers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)

			req := httptest.NewRequest(http.MethodGet,
				fmtURL("/api/v1/alert-overrides/server/%d", rbacUnsharedConnID), nil)
			req = withSuperuser(req)
			rec := httptest.NewRecorder()

			noRespPanic(func() {
				tc.invoke(store, checker, rec, req, "server", rbacUnsharedConnID)
			})

			// The gate must not write any error status. The datastore
			// call that follows panics on nil and noRespPanic recovers
			// before any status is written, so rec.Code remains the
			// httptest default of 200. Anything else (403, 404, or a
			// 500 from a premature error response) indicates the gate
			// path is broken.
			if rec.Code != http.StatusOK {
				t.Errorf("%s/superuser: gate wrote %d; expected gate to pass (rec.Code should remain default 200). Body: %s",
					tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestOverrideListHandlers_ServerScope_GroupGranted_NotDenied verifies
// that a user with an explicit group grant clears the gate. The explicit
// grant lets the handler's resolver see the connection via the auth
// store without needing a datastore-backed lister. noRespPanic recovers
// the nil-datastore panic that follows the gate, leaving rec.Code at
// the httptest default (200) when the gate passes cleanly.
func TestOverrideListHandlers_ServerScope_GroupGranted_NotDenied(t *testing.T) {
	for _, tc := range overrideListInvokers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, store, cleanup := createTestRBACHandler(t)
			defer cleanup()

			bobID := newGroupGrantedUser(t, store, "bob",
				rbacUnsharedConnID, auth.AccessLevelReadWrite)
			checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)

			req := httptest.NewRequest(http.MethodGet,
				fmtURL("/api/v1/alert-overrides/server/%d", rbacUnsharedConnID), nil)
			req = withUser(req, bobID)
			req = withUsername(req, "bob")
			rec := httptest.NewRecorder()

			noRespPanic(func() {
				tc.invoke(store, checker, rec, req, "server", rbacUnsharedConnID)
			})

			// See TestOverrideListHandlers_ServerScope_Superuser_NotDenied
			// for why the expected status is the default 200. Anything
			// else (403, 404, 500) indicates the gate wrote an error.
			if rec.Code != http.StatusOK {
				t.Errorf("%s/group-granted: gate wrote %d; expected gate to pass (rec.Code should remain default 200). Body: %s",
					tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestBlackoutVisible_DecisionMatrix exercises the pure visibility
// decision for blackouts with server-scoped or global references. The
// cluster/group branches call the datastore and are tested via the
// integration harness.
func TestBlackoutVisible_DecisionMatrix(t *testing.T) {
	const connID = rbacUnsharedConnID

	visibleYes := map[int]bool{connID: true}
	visibleNo := map[int]bool{}

	connPtr := func(id int) *int { return &id }

	type row struct {
		name    string
		connID  *int
		visible map[int]bool
		want    bool
	}
	rows := []row{
		{name: "global_visible_to_all", connID: nil, visible: visibleNo, want: true},
		{name: "server_visible", connID: connPtr(connID), visible: visibleYes, want: true},
		{name: "server_not_visible", connID: connPtr(connID), visible: visibleNo, want: false},
	}

	h := &BlackoutHandler{}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			b := newBlackoutForTest(r.connID, nil, nil)
			got, err := h.blackoutVisible(context.Background(), b, r.visible)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != r.want {
				t.Errorf("blackoutVisible = %v, want %v", got, r.want)
			}
		})
	}
}

// TestBlackoutScheduleVisible_DecisionMatrix exercises the pure
// visibility decision for blackout schedules with server-scoped or
// global references, matching the guard added to listBlackoutSchedules
// and getBlackoutSchedule in the #35 follow-up. This locks in that a
// non-owner caller cannot see a schedule attached to an unshared
// server that was not filtered by the legacy code path.
func TestBlackoutScheduleVisible_DecisionMatrix(t *testing.T) {
	const connID = rbacUnsharedConnID

	visibleYes := map[int]bool{connID: true}
	visibleNo := map[int]bool{}

	connPtr := func(id int) *int { return &id }

	type row struct {
		name    string
		connID  *int
		visible map[int]bool
		want    bool
	}
	rows := []row{
		{name: "global_visible_to_all", connID: nil, visible: visibleNo, want: true},
		{name: "server_visible", connID: connPtr(connID), visible: visibleYes, want: true},
		{name: "server_not_visible", connID: connPtr(connID), visible: visibleNo, want: false},
	}

	h := &BlackoutHandler{}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			s := &database.BlackoutSchedule{ConnectionID: r.connID}
			got, err := h.blackoutScheduleVisible(context.Background(), s, r.visible)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != r.want {
				t.Errorf("blackoutScheduleVisible = %v, want %v", got, r.want)
			}
		})
	}
}

// TestAlertOverrideGetContext_NonOwnerUnshared_404 verifies that a
// user with the manage_alert_rules admin permission (so the permission
// gate passes) but no visibility into an unshared server receives 404
// from the getOverrideContext endpoint. The scope gate runs before any
// datastore call so the nil datastore is never reached.
func TestAlertOverrideGetContext_NonOwnerUnshared_404(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newTestUser(t, store, "bob")
	// Grant bob the admin permission so checkPermission passes and the
	// scope-visibility gate becomes the test target.
	groupID, err := store.CreateGroup("bob_group", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.AddUserToGroup(groupID, bobID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if err := store.GrantAdminPermission(groupID, auth.PermManageAlertRules); err != nil {
		t.Fatalf("GrantAdminPermission: %v", err)
	}

	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)

	h := NewAlertOverrideHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodGet,
		fmtURL("/api/v1/alert-overrides/context/%d/1", rbacUnsharedConnID), nil)
	req = withUser(req, bobID)
	req = withUsername(req, "bob")
	rec := httptest.NewRecorder()

	h.getOverrideContext(rec, req, rbacUnsharedConnID, 1)

	requireStatus(t, rec, http.StatusNotFound)
}

// TestVisibleConnectionIDs_Issue35_Overview_NonOwnerZeroGrant pins down
// the visible-set contract the overview handler depends on for scoped
// queries. A non-owner zero-grant caller must see the unshared
// connection as invisible, so a scope=server request for it will be
// denied with 403 and a connection_ids list containing only it will be
// filtered to empty.
func TestVisibleConnectionIDs_Issue35_Overview_NonOwnerZeroGrant(t *testing.T) {
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
		t.Fatal("Non-superuser zero-grant caller must not receive allConnections=true")
	}
	if len(ids) != 0 {
		t.Errorf("Expected empty visible set for non-owner zero-grant, got %v", ids)
	}
}
