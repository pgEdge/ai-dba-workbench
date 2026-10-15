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
	"maps"
)

// ErrTokenScopeChanged reports a scope write refused because the
// token's stored scope is no longer the one a ceiling check judged the
// write against. The check reads the scope before the write's
// transaction opens, so without this a token could have a change
// approved against the scope as it stood, wait for an administrator to
// narrow it, and then write back what the narrowing removed.
var ErrTokenScopeChanged = errors.New(
	"the token's scope changed while the request was being checked")

// TokenScopeSnapshot is a token's stored scope rows exactly as a
// ceiling check read them: every connection entry and its level, every
// MCP scope row by identifier id, and every admin permission. A
// scope write given one refuses, with ErrTokenScopeChanged, to proceed
// unless the scope it reads inside its own transaction is identical.
// The zero value is not a valid snapshot; only the ceiling checks make
// one.
type TokenScopeSnapshot struct {
	conns  map[int]string
	mcpIDs map[int64]bool
	admin  map[string]bool
}

// newTokenScopeSnapshot builds a snapshot from a token's stored rows.
// mcpIDs are the raw identifier ids, rows naming deleted identifiers
// included, so that the comparison sees every row the scope holds rather
// than the names a join yields.
func newTokenScopeSnapshot(conns []ScopedConnection, mcpIDs []int64,
	admin []string) *TokenScopeSnapshot {

	snap := &TokenScopeSnapshot{
		conns:  make(map[int]string, len(conns)),
		mcpIDs: make(map[int64]bool, len(mcpIDs)),
		admin:  make(map[string]bool, len(admin)),
	}
	for _, sc := range conns {
		snap.conns[sc.ConnectionID] = sc.AccessLevel
	}
	for _, id := range mcpIDs {
		snap.mcpIDs[id] = true
	}
	for _, perm := range admin {
		snap.admin[perm] = true
	}
	return snap
}

// equal reports whether two snapshots record the same scope.
func (s *TokenScopeSnapshot) equal(o *TokenScopeSnapshot) bool {
	return maps.Equal(s.conns, o.conns) && maps.Equal(s.mcpIDs, o.mcpIDs) &&
		maps.Equal(s.admin, o.admin)
}

// tokenScopeSnapshotTx reads a token's scope rows inside the caller's
// transaction.
func tokenScopeSnapshotTx(tx *sql.Tx, tokenID int64) (*TokenScopeSnapshot,
	error) {

	conns, err := tokenConnectionScopeTx(tx, tokenID)
	if err != nil {
		return nil, err
	}
	ids, err := tokenMCPScopeIDsTx(tx, tokenID)
	if err != nil {
		return nil, err
	}
	admin, err := tokenAdminScopeTx(tx, tokenID)
	if err != nil {
		return nil, err
	}
	return newTokenScopeSnapshot(conns, ids, admin), nil
}

// guardJudgedTokenScopeTx refuses, with ErrTokenScopeChanged, a scope
// write whose token's scope, read inside the write's transaction, is not
// the one judged recorded. judged nil means no ceiling check judged the
// write (a session, or a token that holds everything), so there is
// nothing to compare.
func guardJudgedTokenScopeTx(tx *sql.Tx, tokenID int64,
	judged *TokenScopeSnapshot) error {

	if judged == nil {
		return nil
	}
	current, err := tokenScopeSnapshotTx(tx, tokenID)
	if err != nil {
		return err
	}
	if !judged.equal(current) {
		return ErrTokenScopeChanged
	}
	return nil
}
