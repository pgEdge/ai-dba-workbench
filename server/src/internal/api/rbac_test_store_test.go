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
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

// newTestAuthStore returns a real, empty, file-backed auth store that is
// closed when the test ends.
//
// An RBAC checker built without an auth store denies every check (issue
// #477), so a handler test that wants to get past the permission gate
// needs a real store and a caller the store admits. Pair this with
// withSuperuser on the request: the superuser bypass never consults the
// store, and the permissive case is then visible at each call site that
// relies on it.
func newTestAuthStore(t *testing.T) *auth.AuthStore {
	t.Helper()

	store, err := auth.NewAuthStore(t.TempDir(), 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)
	t.Cleanup(func() { store.Close() })
	return store
}

// newTestRBACChecker returns an RBAC checker backed by newTestAuthStore.
// It admits a request only once withSuperuser (or a real grant in the
// store) says so.
func newTestRBACChecker(t *testing.T) *auth.RBACChecker {
	t.Helper()
	return auth.NewRBACChecker(newTestAuthStore(t))
}
