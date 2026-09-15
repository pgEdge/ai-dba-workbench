/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// newUserInfoTestStore builds a throwaway auth store for the user-info
// handler tests.
func newUserInfoTestStore(t *testing.T) *auth.AuthStore {
	t.Helper()
	store, _ := newUserInfoTestStoreDir(t)
	return store
}

// newUserInfoTestStoreDir is newUserInfoTestStore plus the directory the
// store's SQLite file lives in, for the one test that has to reach past
// the store's API to manufacture an orphaned token.
func newUserInfoTestStoreDir(t *testing.T) (*auth.AuthStore, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "userinfo-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create auth store: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)
	t.Cleanup(func() {
		store.Close()
		os.RemoveAll(tmpDir)
	})
	return store, tmpDir
}

// callUserInfo drives the handler and decodes its JSON body, asserting the
// endpoint's invariant that it always answers HTTP 200.
func callUserInfo(t *testing.T, store *auth.AuthStore, decorate func(*http.Request)) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, auth.UserInfoPath, nil)
	if decorate != nil {
		decorate(req)
	}
	rr := httptest.NewRecorder()
	createUserInfoHandler(store)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (the endpoint must never 401)", rr.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body %q: %v", rr.Body.String(), err)
	}
	return body
}

func TestCreateUserInfoHandler_NoCredentials(t *testing.T) {
	store := newUserInfoTestStore(t)

	body := callUserInfo(t, store, nil)
	if body["authenticated"] != false {
		t.Errorf("authenticated = %v, want false", body["authenticated"])
	}
	if _, ok := body["error"]; ok {
		t.Errorf("unexpected error field for missing credentials: %v", body["error"])
	}
}

func TestCreateUserInfoHandler_InvalidToken(t *testing.T) {
	store := newUserInfoTestStore(t)

	body := callUserInfo(t, store, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer not-a-real-token")
	})
	if body["authenticated"] != false {
		t.Errorf("authenticated = %v, want false", body["authenticated"])
	}
	if body["error"] != "Invalid or expired token" {
		t.Errorf("error = %v, want %q", body["error"], "Invalid or expired token")
	}
}

func TestCreateUserInfoHandler_SessionCookie(t *testing.T) {
	store := newUserInfoTestStore(t)

	if err := store.CreateUser("alice", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	sessionToken, _, err := store.AuthenticateUser("alice", "Testpass1234")
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}

	body := callUserInfo(t, store, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "session_token", Value: sessionToken})
	})
	if body["authenticated"] != true {
		t.Fatalf("authenticated = %v, want true", body["authenticated"])
	}
	if body["username"] != "alice" {
		t.Errorf("username = %v, want %q", body["username"], "alice")
	}
	if body["is_superuser"] != false {
		t.Errorf("is_superuser = %v, want false", body["is_superuser"])
	}
	perms, ok := body["admin_permissions"].([]any)
	if !ok {
		t.Fatalf("admin_permissions = %#v, want a JSON array", body["admin_permissions"])
	}
	if len(perms) != 0 {
		t.Errorf("admin_permissions = %v, want empty", perms)
	}
}

func TestCreateUserInfoHandler_SuperuserAPIToken(t *testing.T) {
	store := newUserInfoTestStore(t)

	if err := store.CreateUser("root", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := store.SetUserSuperuser("root", true); err != nil {
		t.Fatalf("failed to set superuser: %v", err)
	}
	rawToken, _, err := store.CreateToken("root", "test token", nil)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	body := callUserInfo(t, store, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+rawToken)
	})
	if body["authenticated"] != true {
		t.Fatalf("authenticated = %v, want true", body["authenticated"])
	}
	if body["username"] != "root" {
		t.Errorf("username = %v, want %q", body["username"], "root")
	}
	if body["is_superuser"] != true {
		t.Errorf("is_superuser = %v, want true", body["is_superuser"])
	}
}

// TestCreateUserInfoHandler_AdminPermissions covers the permission-lookup
// branch: a group grant has to reach the client so that the UI can render
// the corresponding administrative affordances.
func TestCreateUserInfoHandler_AdminPermissions(t *testing.T) {
	store := newUserInfoTestStore(t)

	if err := store.CreateUser("bob", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	user, err := store.GetUser("bob")
	if err != nil {
		t.Fatalf("failed to fetch user: %v", err)
	}
	groupID, err := store.CreateGroup("admins", "test group")
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}
	if err := store.AddUserToGroup(groupID, user.ID); err != nil {
		t.Fatalf("failed to add user to group: %v", err)
	}
	if err := store.GrantAdminPermission(groupID, auth.PermManageUsers); err != nil {
		t.Fatalf("failed to grant admin permission: %v", err)
	}

	sessionToken, _, err := store.AuthenticateUser("bob", "Testpass1234")
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}

	body := callUserInfo(t, store, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+sessionToken)
	})
	perms, ok := body["admin_permissions"].([]any)
	if !ok {
		t.Fatalf("admin_permissions = %#v, want a JSON array", body["admin_permissions"])
	}
	if len(perms) != 1 || perms[0] != auth.PermManageUsers {
		t.Errorf("admin_permissions = %v, want [%s]", perms, auth.PermManageUsers)
	}
}

// TestCreateUserInfoHandler_UnresolvableOwner exercises the defensive
// branch where a credential validates but its owner does not resolve to a
// username. AuthStore.ValidateToken checks that the owning row exists and
// is enabled, so the state cannot be reached through the store's own API
// and is manufactured by blanking the username directly. The endpoint must
// report the caller as unauthenticated rather than as an authenticated
// session with an empty username, which the client would render as a
// logged-in user with no name.
func TestCreateUserInfoHandler_UnresolvableOwner(t *testing.T) {
	store, dir := newUserInfoTestStoreDir(t)

	if err := store.CreateUser("ghost", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	rawToken, _, err := store.CreateToken("ghost", "test token", nil)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	db, err := sql.Open("sqlite", filepath.Join(dir, "auth.db"))
	if err != nil {
		t.Fatalf("failed to open auth database: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE users SET username = '' WHERE username = ?", "ghost"); err != nil {
		t.Fatalf("failed to blank the owner's username: %v", err)
	}

	body := callUserInfo(t, store, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+rawToken)
	})
	if body["authenticated"] != false {
		t.Errorf("authenticated = %v, want false", body["authenticated"])
	}
	if body["error"] != "Invalid or expired token" {
		t.Errorf("error = %v, want %q", body["error"], "Invalid or expired token")
	}
}
