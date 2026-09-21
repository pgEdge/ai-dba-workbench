/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package auth

import (
	"strings"
	"testing"
)

// newTokenScopeFailureStore returns a store holding a single token, ready
// for a test to break one of the scope tables underneath it.
func newTokenScopeFailureStore(t *testing.T) (*AuthStore, int64) {
	t.Helper()

	store, cleanup := createTestAuthStoreForTokenScope(t)
	t.Cleanup(cleanup)

	_, token, err := store.CreateToken("testuser", "Failure token", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	return store, token.ID
}

// dropScopeTable removes one table from the live auth database so that the
// next query against it fails. The scope tables are children of tokens, and
// mcp_privilege_identifiers is referenced by identifier rather than by a
// foreign key, so no other table's integrity depends on any of them.
func dropScopeTable(t *testing.T, store *AuthStore, table string) {
	t.Helper()

	if _, err := store.db.Exec("DROP TABLE " + table); err != nil {
		t.Fatalf("Failed to drop %s: %v", table, err)
	}
}

// TestTokenScopeReadsReportDatabaseFailures checks that every scope read
// returns the failure to its caller, rather than reporting an empty scope,
// when the table it depends on cannot be queried. Reporting "no scope" on a
// database error would read as an unrestricted token and open the very
// bypass the scope checks exist to prevent.
func TestTokenScopeReadsReportDatabaseFailures(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, store *AuthStore, tokenID int64)
		drop    string
		call    func(store *AuthStore, tokenID int64) error
		wantErr string
	}{
		{
			name: "GetTokenScope admin query",
			drop: "token_admin_scope",
			call: func(store *AuthStore, tokenID int64) error {
				_, err := store.GetTokenScope(tokenID)
				return err
			},
			wantErr: "failed to get token admin scope",
		},
		{
			name: "GetTokenConnectionScope query",
			drop: "token_connection_scope",
			call: func(store *AuthStore, tokenID int64) error {
				_, err := store.GetTokenConnectionScope(tokenID)
				return err
			},
			wantErr: "failed to get token connection scope",
		},
		{
			name: "GetTokenMCPScope wildcard check",
			drop: "token_mcp_scope",
			call: func(store *AuthStore, tokenID int64) error {
				_, err := store.GetTokenMCPScope(tokenID)
				return err
			},
			wantErr: "failed to check wildcard MCP scope",
		},
		{
			name: "GetTokenMCPScope identifier join",
			drop: "mcp_privilege_identifiers",
			call: func(store *AuthStore, tokenID int64) error {
				_, err := store.GetTokenMCPScope(tokenID)
				return err
			},
			wantErr: "failed to get token MCP scope",
		},
		{
			name: "IsConnectionInTokenScope scope check",
			drop: "token_connection_scope",
			call: func(store *AuthStore, tokenID int64) error {
				_, _, err := store.IsConnectionInTokenScope(tokenID, 1)
				return err
			},
			wantErr: "failed to check connection scope",
		},
		{
			name: "IsMCPItemInTokenScope item check",
			setup: func(t *testing.T, store *AuthStore, tokenID int64) {
				t.Helper()
				// A non-wildcard scope row, so that the item lookup is
				// reached: privilege identifier 42 need not exist, since
				// the join is what is being broken.
				if _, err := store.db.Exec(
					"INSERT INTO token_mcp_scope (token_id, privilege_identifier_id) VALUES (?, ?)",
					tokenID, 42,
				); err != nil {
					t.Fatalf("Failed to seed MCP scope: %v", err)
				}
			},
			drop: "mcp_privilege_identifiers",
			call: func(store *AuthStore, tokenID int64) error {
				_, err := store.IsMCPItemInTokenScope(tokenID, "list_tables")
				return err
			},
			wantErr: "failed to check item in scope",
		},
		{
			name: "HasTokenScope connection count",
			drop: "token_connection_scope",
			call: func(store *AuthStore, tokenID int64) error {
				_, err := store.HasTokenScope(tokenID)
				return err
			},
			wantErr: "failed to check connection scope",
		},
		{
			name: "HasTokenScope MCP count",
			drop: "token_mcp_scope",
			call: func(store *AuthStore, tokenID int64) error {
				_, err := store.HasTokenScope(tokenID)
				return err
			},
			wantErr: "failed to check MCP scope",
		},
		{
			name: "HasTokenScope admin count",
			drop: "token_admin_scope",
			call: func(store *AuthStore, tokenID int64) error {
				_, err := store.HasTokenScope(tokenID)
				return err
			},
			wantErr: "failed to check admin scope",
		},
		{
			name: "IsAdminPermissionInTokenScope scope check",
			drop: "token_admin_scope",
			call: func(store *AuthStore, tokenID int64) error {
				_, err := store.IsAdminPermissionInTokenScope(tokenID, "manage_users")
				return err
			},
			wantErr: "failed to check admin scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, tokenID := newTokenScopeFailureStore(t)
			if tt.setup != nil {
				tt.setup(t, store, tokenID)
			}
			dropScopeTable(t, store, tt.drop)

			err := tt.call(store, tokenID)
			if err == nil {
				t.Fatalf("Expected an error after dropping %s, got none", tt.drop)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Expected error containing %q, got %q", tt.wantErr, err)
			}
			if !strings.Contains(err.Error(), "no such table") {
				t.Errorf("Expected the underlying SQLite error to be wrapped, got %q", err)
			}
		})
	}
}

// TestHasTokenScopeReportsRestrictionAfterFailureIsFalse checks the boolean
// returned alongside a scope-read failure, since a caller that ignored the
// error would otherwise be told the token is unrestricted.
func TestHasTokenScopeReportsRestrictionAfterFailureIsFalse(t *testing.T) {
	store, tokenID := newTokenScopeFailureStore(t)
	dropScopeTable(t, store, "token_connection_scope")

	restricted, err := store.HasTokenScope(tokenID)
	if err == nil {
		t.Fatal("Expected an error from HasTokenScope")
	}
	if restricted {
		t.Error("Expected restricted=false alongside the error")
	}

	inScope, _, err := store.IsConnectionInTokenScope(tokenID, 7)
	if err == nil {
		t.Fatal("Expected an error from IsConnectionInTokenScope")
	}
	if inScope {
		t.Error("Expected inScope=false alongside the error")
	}
}
