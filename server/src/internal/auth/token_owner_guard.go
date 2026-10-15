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
	"database/sql"
	"errors"
	"fmt"
)

// ErrTokenNotOwned reports a token write that only the token's owner,
// or a superuser, may make (issue #522): creating a token for another
// user, or widening or clearing the scope of a token the caller does not
// own. Anyone else holding manage_token_scopes may only narrow a token.
var ErrTokenNotOwned = errors.New(
	"only a token's owner or a superuser may create, widen or clear it")

// TokenOwnerGuard says whom a token scope write acts for, so that the
// store can refuse, inside the write's own transaction, a write that
// widens or clears a token owned by someone else. The zero value owns
// no token, so every write it guards must narrow.
type TokenOwnerGuard struct {
	callerUserID int64
	anyOwner     bool
}

// AnyTokenOwner is the guard for a principal that may widen any token:
// a superuser, and the CLI, which acts for the server itself.
func AnyTokenOwner() TokenOwnerGuard {
	return TokenOwnerGuard{anyOwner: true}
}

// TokensOwnedBy is the guard for a principal acting as the user
// callerUserID: a session's user, or an API token's owner. It may widen
// only the tokens that user owns.
func TokensOwnedBy(callerUserID int64) TokenOwnerGuard {
	return TokenOwnerGuard{callerUserID: callerUserID}
}

// guardTokenOwnerTx refuses with ErrTokenNotOwned a scope write to a
// token the guard does not own, unless it narrows the token. change is
// the write; nil means the scope is cleared, which always widens. The
// owner and the stored scope are read inside the write's transaction,
// so that a concurrent narrowing cannot be undone by a write judged
// against the scope as it stood before. A token that does not exist is
// let through for the write to report as it always has.
func guardTokenOwnerTx(tx *sql.Tx, tokenID int64, guard TokenOwnerGuard,
	change *TokenScopeChange) error {

	if guard.anyOwner {
		return nil
	}
	var ownerID int64
	err := tx.QueryRow(`SELECT owner_id FROM tokens WHERE id = ?`,
		tokenID).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read the owner of token %d: %w",
			tokenID, err)
	}
	if ownerID == guard.callerUserID {
		return nil
	}
	if change == nil {
		return ErrTokenNotOwned
	}
	stored, err := storedTokenScopeTx(tx, tokenID)
	if err != nil {
		return err
	}
	if stored.widens(*change) {
		return ErrTokenNotOwned
	}
	return nil
}

// storedTokenScopeTx reads a token's scope inside the caller's
// transaction, kind by kind, as loadStoredTokenScope reads it outside
// one. Only the scope is filled in, not the owner.
func storedTokenScopeTx(tx *sql.Tx, tokenID int64) (*storedTokenScope,
	error) {

	conns, err := tokenConnectionScopeTx(tx, tokenID)
	if err != nil {
		return nil, err
	}
	names, err := tokenMCPScopeNamesTx(tx, tokenID)
	if err != nil {
		return nil, err
	}
	ids, err := tokenMCPScopeIDsTx(tx, tokenID)
	if err != nil {
		return nil, err
	}
	perms, err := tokenAdminScopeTx(tx, tokenID)
	if err != nil {
		return nil, err
	}
	return newStoredTokenScope(conns, names, len(ids) > 0, perms), nil
}

// newStoredTokenScope builds a storedTokenScope from a token's stored
// connection entries, MCP identifiers and admin permissions. hasMCPRows
// reports whether any MCP scope row exists: rows naming only deleted
// identifiers yield no names but still restrict the token, to nothing
// (see actorMCPScope).
func newStoredTokenScope(conns []ScopedConnection, mcpNames []string,
	hasMCPRows bool, adminPerms []string) *storedTokenScope {

	scope := &storedTokenScope{conns: make(map[int]string, len(conns))}
	for _, sc := range conns {
		scope.conns[sc.ConnectionID] = sc.AccessLevel
	}
	scope.connsRestricted = len(scope.conns) > 0
	scope.mcp, scope.mcpRestricted = namedScope(mcpNames, mcpScopeWildcard)
	if !scope.mcpRestricted && len(mcpNames) == 0 && hasMCPRows {
		scope.mcp, scope.mcpRestricted = map[string]bool{}, true
	}
	scope.admin, scope.adminRestricted = namedScope(adminPerms,
		AdminPermissionWildcard)
	return scope
}
