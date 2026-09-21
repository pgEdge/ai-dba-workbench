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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

// newWrapperTestStore builds a throwaway auth store with one user and a
// session token for the createAuthWrapper tests.
func newWrapperTestStore(t *testing.T) (*auth.AuthStore, string) {
	t.Helper()

	store, err := auth.NewAuthStore(t.TempDir(), 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("failed to create auth store: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)
	t.Cleanup(func() { store.Close() })

	if err := store.CreateUser("wrapper", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	sessionToken, _, err := store.AuthenticateUser("wrapper", "Testpass1234")
	if err != nil {
		t.Fatalf("failed to authenticate: %v", err)
	}

	return store, sessionToken
}

func TestCreateAuthWrapperSetsIPFromExtractor(t *testing.T) {
	store, sessionToken := newWrapperTestStore(t)

	extractor := auth.NewIPExtractor([]string{"192.0.2.0/24"})
	wrapper := createAuthWrapper(store, extractor)

	var gotIP string
	handler := wrapper(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = r.Context().Value(auth.IPAddressContextKey).(string)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	req.Header.Set("X-Forwarded-For", "198.51.100.4")
	req.RemoteAddr = "192.0.2.10:4321"

	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotIP != "198.51.100.4" {
		t.Errorf("ip: got %q, want %q", gotIP, "198.51.100.4")
	}
}

func TestCreateAuthWrapperFallsBackToRemoteAddr(t *testing.T) {
	store, sessionToken := newWrapperTestStore(t)

	wrapper := createAuthWrapper(store, nil)

	var gotIP string
	handler := wrapper(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = r.Context().Value(auth.IPAddressContextKey).(string)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	req.RemoteAddr = "203.0.113.9:5555"

	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotIP != "203.0.113.9" {
		t.Errorf("ip: got %q, want %q", gotIP, "203.0.113.9")
	}
}

func TestCreateAuthWrapperUnparseableRemoteAddr(t *testing.T) {
	store, sessionToken := newWrapperTestStore(t)

	wrapper := createAuthWrapper(store, nil)

	var gotIP string
	handler := wrapper(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = r.Context().Value(auth.IPAddressContextKey).(string)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	req.RemoteAddr = "not-a-host-port"

	rec := httptest.NewRecorder()
	handler(rec, req)

	if gotIP != "not-a-host-port" {
		t.Errorf("ip: got %q, want the raw RemoteAddr", gotIP)
	}
}

func TestCreateAuthWrapperRejectsMissingCredentials(t *testing.T) {
	store, _ := newWrapperTestStore(t)

	wrapper := createAuthWrapper(store, nil)
	handler := wrapper(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not run for an unauthenticated request")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestCreateAuthWrapperRejectsInvalidToken(t *testing.T) {
	store, _ := newWrapperTestStore(t)

	wrapper := createAuthWrapper(store, nil)
	handler := wrapper(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not run for an invalid token")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil)
	req.Header.Set("Authorization", "Bearer nope")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if body := rec.Body.String(); body == "" {
		t.Error("expected an error body")
	}
}
