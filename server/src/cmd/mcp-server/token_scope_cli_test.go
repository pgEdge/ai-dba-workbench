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
	"errors"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// TestScopeTokenConnectionsCommandRefusesMalformedScope checks that
// -scope-token-connections refuses a scope that mixes connection 0, the
// "all connections" entry, with particular connections, or names a
// connection twice, and leaves the stored scope as it was.
func TestScopeTokenConnectionsCommandRefusesMalformedScope(t *testing.T) {
	dataDir := t.TempDir()
	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		t.Fatalf("openAuthStoreCLI failed: %v", err)
	}
	if err := store.CreateUser("cli-owner", "Password1234", "", "", ""); err != nil {
		store.Close()
		t.Fatalf("CreateUser failed: %v", err)
	}
	_, token, err := store.CreateToken("cli-owner", "cli token", nil)
	store.Close()
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	if err := scopeTokenConnectionsCommand(dataDir, token.ID, "5"); err != nil {
		t.Fatalf("Scoping to one connection failed: %v", err)
	}
	for _, ids := range []string{"0,5", "5,0", "5,5", "-1"} {
		err := scopeTokenConnectionsCommand(dataDir, token.ID, ids)
		if !errors.Is(err, auth.ErrInvalidConnectionScope) {
			t.Errorf("Scoping to %q: expected ErrInvalidConnectionScope, got %v",
				ids, err)
		}
	}

	store, err = openAuthStoreCLI(dataDir)
	if err != nil {
		t.Fatalf("openAuthStoreCLI failed: %v", err)
	}
	defer store.Close()
	scope, err := store.GetTokenScope(token.ID)
	if err != nil {
		t.Fatalf("GetTokenScope failed: %v", err)
	}
	if scope == nil || len(scope.Connections) != 1 ||
		scope.Connections[0].ConnectionID != 5 {
		t.Errorf("Expected the scope to stay on connection 5, got %+v", scope)
	}
}
