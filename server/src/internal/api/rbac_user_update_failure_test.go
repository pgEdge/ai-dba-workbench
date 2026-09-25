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
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// updateUserTestEnv is an RBAC handler whose auth database a test can also
// reach over a second connection, so that it can put the database into a
// state the public API cannot produce.
type updateUserTestEnv struct {
	handler  *RBACHandler
	store    *auth.AuthStore
	adminID  int64
	targetID int64
	db       *sql.DB
}

// newUpdateUserTestEnv builds the handler, an administrator holding
// PermManageUsers and a local target account.
func newUpdateUserTestEnv(t *testing.T) *updateUserTestEnv {
	t.Helper()

	dataDir := t.TempDir()
	store, err := auth.NewAuthStore(dataDir, 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	handler := NewRBACHandler(store, auth.NewRBACChecker(store))

	if err := store.CreateUser("admin", "Password1234", "Admin", "", ""); err != nil {
		t.Fatalf("CreateUser admin: %v", err)
	}
	adminID, err := store.GetUserID("admin")
	if err != nil {
		t.Fatalf("GetUserID admin: %v", err)
	}
	groupID, err := store.CreateGroup("admins", "Admins")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.AddUserToGroup(groupID, adminID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if err := store.GrantAdminPermission(groupID, auth.PermManageUsers); err != nil {
		t.Fatalf("GrantAdminPermission: %v", err)
	}

	if err := store.CreateUser("target", "Password1234", "before", "", ""); err != nil {
		t.Fatalf("CreateUser target: %v", err)
	}
	targetID, err := store.GetUserID("target")
	if err != nil {
		t.Fatalf("GetUserID target: %v", err)
	}

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "auth.db"))
	if err != nil {
		t.Fatalf("opening the auth database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("closing the auth database: %v", closeErr)
		}
	})

	return &updateUserTestEnv{
		handler:  handler,
		store:    store,
		adminID:  adminID,
		targetID: targetID,
		db:       db,
	}
}

// put sends body as a PUT to the target account, as the administrator.
func (e *updateUserTestEnv) put(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding the request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/rbac/users/"+strconv.FormatInt(e.targetID, 10),
		bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, e.adminID)
	rec := httptest.NewRecorder()

	e.handler.updateUser(rec, req, e.targetID)
	return rec
}

// errorMessage decodes the error from a refused request.
func errorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decoding the error response: %v", err)
	}
	return response.Error
}

// TestRBACHandler_UpdateUser_StoreFailure covers the one path a well-formed
// request can still fail on: the store refusing the write. A trigger that
// aborts every update to users stands in for whatever the database might
// refuse, and the handler must answer with a generic 500 rather than the
// store's own error text, and leave the account untouched.
func TestRBACHandler_UpdateUser_StoreFailure(t *testing.T) {
	env := newUpdateUserTestEnv(t)

	if _, err := env.db.Exec(`CREATE TRIGGER refuse_user_updates BEFORE UPDATE ON users
        BEGIN SELECT RAISE(ABORT, 'forced failure'); END`); err != nil {
		t.Fatalf("creating the trigger: %v", err)
	}

	rec := env.put(t, map[string]any{"annotation": "after"})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusInternalServerError, rec.Code, rec.Body.String())
	}
	if got := errorMessage(t, rec); got != "Failed to update user" {
		t.Errorf("Expected the generic failure message, got %q", got)
	}

	user, err := env.store.GetUserByID(env.targetID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if user.Annotation != "before" {
		t.Errorf("annotation = %q after a failed update, want it unchanged", user.Annotation)
	}
}

// TestRBACHandler_UpdateUser_EmptyAuthSourceRefusesPassword pins the reading
// login applies: only an auth_source of exactly local accepts a password, so
// an empty one, which the NOT NULL DEFAULT 'local' column should make
// impossible, is refused a password too, and the store's refusal reaches the
// caller as a 400 rather than a 500.
func TestRBACHandler_UpdateUser_EmptyAuthSourceRefusesPassword(t *testing.T) {
	env := newUpdateUserTestEnv(t)

	if _, err := env.db.Exec("UPDATE users SET auth_source = '' WHERE id = ?", env.targetID); err != nil {
		t.Fatalf("clearing auth_source: %v", err)
	}

	rec := env.put(t, map[string]any{"password": "An0ther-Str0ng-Pass!"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	if got := errorMessage(t, rec); !strings.Contains(got, "identity provider") {
		t.Errorf("Expected the refusal to explain why, got %q", got)
	}
}

// Triggers that abort every insert into, or update of, the users table,
// standing in for whatever the database might refuse.
const (
	refuseUserInserts = `CREATE TRIGGER refuse_user_inserts BEFORE INSERT ON users
        BEGIN SELECT RAISE(ABORT, 'forced failure'); END`
	refuseUserUpdates = `CREATE TRIGGER refuse_user_updates BEFORE UPDATE ON users
        BEGIN SELECT RAISE(ABORT, 'forced failure'); END`
)

// TestRBACHandler_CreateUser_StoreFailure covers each store call createUser
// makes failing for a reason that is not the caller's: every one must answer
// with its own generic 500 rather than the store's error text.
func TestRBACHandler_CreateUser_StoreFailure(t *testing.T) {
	tests := []struct {
		name    string
		trigger string
		body    map[string]any
		want    string
	}{
		{
			name:    "user insert",
			trigger: refuseUserInserts,
			body:    map[string]any{"username": "newuser", "password": "Password1234"},
			want:    "Failed to create user",
		},
		{
			name:    "service account insert",
			trigger: refuseUserInserts,
			body:    map[string]any{"username": "newsvc", "is_service_account": true},
			want:    "Failed to create service account",
		},
		{
			name:    "disable",
			trigger: refuseUserUpdates,
			body:    map[string]any{"username": "newuser", "password": "Password1234", "enabled": false},
			want:    "Failed to disable user",
		},
		{
			name:    "superuser grant",
			trigger: refuseUserUpdates,
			body:    map[string]any{"username": "newuser", "password": "Password1234", "is_superuser": true},
			want:    "Failed to set superuser status",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newUpdateUserTestEnv(t)

			if _, err := env.db.Exec(tt.trigger); err != nil {
				t.Fatalf("creating the trigger: %v", err)
			}

			encoded, err := json.Marshal(tt.body)
			if err != nil {
				t.Fatalf("encoding the request body: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/users",
				bytes.NewReader(encoded))
			req.Header.Set("Content-Type", "application/json")
			req = withSuperuser(req)
			rec := httptest.NewRecorder()
			env.handler.handleUsers(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("Expected status %d, got %d. Body: %s",
					http.StatusInternalServerError, rec.Code, rec.Body.String())
			}
			if got := errorMessage(t, rec); got != tt.want {
				t.Errorf("message = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRBACHandler_DeleteUser_StoreFailure covers the store refusing the
// deletion: a generic 500, and the account left in place.
func TestRBACHandler_DeleteUser_StoreFailure(t *testing.T) {
	env := newUpdateUserTestEnv(t)

	if _, err := env.db.Exec(`CREATE TRIGGER refuse_user_deletes BEFORE DELETE ON users
        BEGIN SELECT RAISE(ABORT, 'forced failure'); END`); err != nil {
		t.Fatalf("creating the trigger: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/rbac/users/"+strconv.FormatInt(env.targetID, 10), nil)
	req = withUser(req, env.adminID)
	rec := httptest.NewRecorder()
	env.handler.deleteUser(rec, req, env.targetID)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusInternalServerError, rec.Code, rec.Body.String())
	}
	if got := errorMessage(t, rec); got != "Failed to delete user" {
		t.Errorf("message = %q, want %q", got, "Failed to delete user")
	}
	if user, err := env.store.GetUserByID(env.targetID); err != nil || user == nil {
		t.Errorf("the account was removed despite the failure: %v", err)
	}
}
