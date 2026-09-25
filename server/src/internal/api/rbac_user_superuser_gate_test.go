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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// putUser issues a PUT /api/v1/rbac/users/{id} request carrying body as the
// request context req was given by wrap, and returns the recorder.
func putUser(t *testing.T, handler *RBACHandler, targetID int64, body map[string]any,
	wrap func(*http.Request) *http.Request) *httptest.ResponseRecorder {

	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding the request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/"+strconv.FormatInt(targetID, 10),
		bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	req = wrap(req)
	rec := httptest.NewRecorder()
	handler.updateUser(rec, req, targetID)
	return rec
}

// assertDeniedAudited checks that the refusal was written to the audit log
// under the action the change would have recorded.
func assertDeniedAudited(t *testing.T, store *auth.AuthStore, action string) {
	t.Helper()
	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Action:  action,
		Outcome: string(auth.OutcomeDenied),
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) == 0 {
		t.Errorf("expected a denied %s audit event, got none", action)
	}
}

// TestRBACHandler_CreateUser_SuperuserFlagRequiresSuperuser covers issue
// #497: manage_users is grantable to a group, so a holder who is not a
// superuser must not be able to create a superuser. The field is refused
// whenever it is present, and the account is not created.
func TestRBACHandler_CreateUser_SuperuserFlagRequiresSuperuser(t *testing.T) {
	for _, flag := range []bool{true, false} {
		t.Run(fmt.Sprintf("is_superuser=%v", flag), func(t *testing.T) {
			handler, store, adminID, cleanup := adminRBACHandler(t)
			defer cleanup()

			body, _ := json.Marshal(map[string]any{
				"username":     "escalated",
				"password":     "Password1234",
				"is_superuser": flag,
			})
			rec := postUser(handler, adminID, body)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("Expected status %d, got %d. Body: %s",
					http.StatusForbidden, rec.Code, rec.Body.String())
			}
			if user, err := store.GetUser("escalated"); err == nil && user != nil {
				t.Error("the refused request created the account anyway")
			}
			assertDeniedAudited(t, store, "user.create")
		})
	}
}

// TestRBACHandler_CreateUser_SuperuserCanCreateSuperuser checks the gate is
// narrow: a superuser may still create a superuser.
func TestRBACHandler_CreateUser_SuperuserCanCreateSuperuser(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]any{
		"username":     "newsuper",
		"password":     "Password1234",
		"is_superuser": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()
	handler.handleUsers(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusCreated, rec.Code, rec.Body.String())
	}
	user, err := store.GetUser("newsuper")
	if err != nil || user == nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !user.IsSuperuser {
		t.Error("expected the new account to be a superuser")
	}
}

// TestRBACHandler_UpdateUser_SuperuserFlagRequiresSuperuser covers the
// update half of issue #497, including the self-escalation the issue
// describes and clearing the flag on an existing superuser. Nothing else
// in a refused request may be applied either.
func TestRBACHandler_UpdateUser_SuperuserFlagRequiresSuperuser(t *testing.T) {
	tests := []struct {
		name        string
		self        bool
		targetSuper bool
		flag        bool
	}{
		{name: "grant to self", self: true, targetSuper: false, flag: true},
		{name: "grant to another user", targetSuper: false, flag: true},
		{name: "clear on a superuser", targetSuper: true, flag: false},
		{name: "restate the current value", targetSuper: false, flag: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, store, adminID, cleanup := adminRBACHandler(t)
			defer cleanup()

			targetID := adminID
			if !tt.self {
				if err := store.CreateUser("target", "Password1234", "before", "", ""); err != nil {
					t.Fatalf("CreateUser: %v", err)
				}
				targetID, _ = store.GetUserID("target")
			}
			target, err := store.GetUserByID(targetID)
			if err != nil {
				t.Fatalf("GetUserByID: %v", err)
			}
			if tt.targetSuper {
				if err := store.SetUserSuperuser(target.Username, true); err != nil {
					t.Fatalf("SetUserSuperuser: %v", err)
				}
			}

			rec := putUser(t, handler, targetID, map[string]any{
				"is_superuser": tt.flag,
				"annotation":   "after",
			}, func(r *http.Request) *http.Request { return withUser(r, adminID) })

			if rec.Code != http.StatusForbidden {
				t.Fatalf("Expected status %d, got %d. Body: %s",
					http.StatusForbidden, rec.Code, rec.Body.String())
			}
			after, err := store.GetUserByID(targetID)
			if err != nil {
				t.Fatalf("GetUserByID: %v", err)
			}
			if after.IsSuperuser != tt.targetSuper {
				t.Errorf("is_superuser = %v after a refused request, want %v",
					after.IsSuperuser, tt.targetSuper)
			}
			if after.Annotation == "after" {
				t.Error("the refused request applied its other changes anyway")
			}
			assertDeniedAudited(t, store, "user.update")
		})
	}
}

// TestRBACHandler_UpdateUser_SuperuserCanChangeSuperuser checks a superuser
// can still grant and clear the flag.
func TestRBACHandler_UpdateUser_SuperuserCanChangeSuperuser(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	if err := store.CreateUser("target", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	targetID, _ := store.GetUserID("target")

	for _, flag := range []bool{true, false} {
		rec := putUser(t, handler, targetID, map[string]any{"is_superuser": flag}, withSuperuser)
		if rec.Code != http.StatusOK {
			t.Fatalf("is_superuser=%v: expected status %d, got %d. Body: %s",
				flag, http.StatusOK, rec.Code, rec.Body.String())
		}
		user, err := store.GetUserByID(targetID)
		if err != nil {
			t.Fatalf("GetUserByID: %v", err)
		}
		if user.IsSuperuser != flag {
			t.Errorf("is_superuser = %v, want %v", user.IsSuperuser, flag)
		}
	}
}

// TestRBACHandler_UpdateUser_ManageUsersWithoutSuperuserFlag checks the
// gate does not get in the way of the ordinary manage_users work the
// permission exists for.
func TestRBACHandler_UpdateUser_ManageUsersWithoutSuperuserFlag(t *testing.T) {
	handler, store, adminID, cleanup := adminRBACHandler(t)
	defer cleanup()

	if err := store.CreateUser("target", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	targetID, _ := store.GetUserID("target")

	rec := putUser(t, handler, targetID, map[string]any{
		"password": "An0ther-Str0ng-Pass!",
	}, func(r *http.Request) *http.Request { return withUser(r, adminID) })
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}
}

// TestRespondUserStoreError covers issue #485: a store refusal marked as
// invalid input reaches the caller as a 400 carrying the store's own
// message, without any context wrapped around it, whilst every other error
// stays a 500 with the fixed message.
func TestRespondUserStoreError(t *testing.T) {
	reason := &auth.InvalidInputError{Err: errors.New("cannot do that to alice")}

	tests := []struct {
		name     string
		err      error
		wantCode int
		wantMsg  string
	}{
		{"invalid input", reason, http.StatusBadRequest, "Cannot do that to alice"},
		{"wrapped invalid input", fmt.Errorf("updating: %w", reason),
			http.StatusBadRequest, "Cannot do that to alice"},
		{"store failure", errors.New("database is locked"),
			http.StatusInternalServerError, "Failed to update user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			respondUserStoreError(rec, tt.err, "Failed to update user", "alice")

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if got := errorMessage(t, rec); got != tt.wantMsg {
				t.Errorf("message = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

// superuserTarget creates a second account holding the superuser role and
// returns its ID, for tests of what a manage_users holder may do to one.
func superuserTarget(t *testing.T, store *auth.AuthStore) int64 {
	t.Helper()
	if err := store.CreateUser("target", "Password1234", "before", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.SetUserSuperuser("target", true); err != nil {
		t.Fatalf("SetUserSuperuser: %v", err)
	}
	id, err := store.GetUserID("target")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	return id
}

// TestRBACHandler_UpdateUser_SuperuserTargetRequiresSuperuser covers the
// other half of #497: resetting a superuser's password would let the
// caller sign in as them, and disabling one could lock every
// administrator out, so a manage_users holder may not edit a superuser.
func TestRBACHandler_UpdateUser_SuperuserTargetRequiresSuperuser(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
	}{
		{"password reset", map[string]any{"password": "An0ther-Str0ng-Pass!"}},
		{"disable", map[string]any{"enabled": false}},
		{"annotation", map[string]any{"annotation": "after"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, store, adminID, cleanup := adminRBACHandler(t)
			defer cleanup()
			targetID := superuserTarget(t, store)

			rec := putUser(t, handler, targetID, tt.body,
				func(r *http.Request) *http.Request { return withUser(r, adminID) })
			if rec.Code != http.StatusForbidden {
				t.Fatalf("Expected status %d, got %d. Body: %s",
					http.StatusForbidden, rec.Code, rec.Body.String())
			}

			after, err := store.GetUserByID(targetID)
			if err != nil {
				t.Fatalf("GetUserByID: %v", err)
			}
			if !after.Enabled || after.Annotation != "before" {
				t.Errorf("the refused request changed the account: enabled=%v annotation=%q",
					after.Enabled, after.Annotation)
			}
			if _, _, err := store.AuthenticateUser("target", "Password1234"); err != nil {
				t.Errorf("the original password no longer works: %v", err)
			}
			assertDeniedAudited(t, store, "user.update")
		})
	}
}

// TestRBACHandler_UpdateUser_SuperuserCanEditSuperuser checks a superuser
// may still reset another superuser's password.
func TestRBACHandler_UpdateUser_SuperuserCanEditSuperuser(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	targetID := superuserTarget(t, store)

	rec := putUser(t, handler, targetID,
		map[string]any{"password": "An0ther-Str0ng-Pass!"}, withSuperuser)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	if _, _, err := store.AuthenticateUser("target", "An0ther-Str0ng-Pass!"); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
}

// TestRBACHandler_DeleteUser_SuperuserTarget checks that only a superuser
// may delete a superuser account.
func TestRBACHandler_DeleteUser_SuperuserTarget(t *testing.T) {
	tests := []struct {
		name       string
		superuser  bool
		wantStatus int
	}{
		{"manage_users holder is refused", false, http.StatusForbidden},
		{"superuser may delete", true, http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, store, adminID, cleanup := adminRBACHandler(t)
			defer cleanup()
			targetID := superuserTarget(t, store)

			req := httptest.NewRequest(http.MethodDelete,
				"/api/v1/rbac/users/"+strconv.FormatInt(targetID, 10), nil)
			if tt.superuser {
				req = withSuperuser(req)
			} else {
				req = withUser(req, adminID)
			}
			rec := httptest.NewRecorder()
			handler.deleteUser(rec, req, targetID)

			if rec.Code != tt.wantStatus {
				t.Fatalf("Expected status %d, got %d. Body: %s",
					tt.wantStatus, rec.Code, rec.Body.String())
			}
			user, _ := store.GetUserByID(targetID)
			if exists := user != nil; exists == tt.superuser {
				t.Errorf("account exists = %v after the request", exists)
			}
			if !tt.superuser {
				assertDeniedAudited(t, store, "user.delete")
			}
		})
	}
}
