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
	"fmt"
	"time"
)

// tokenSnapshot is the audited view of a token row. It deliberately
// omits token_hash, so that a snapshot can never carry a credential, or
// any part of one, into the audit log.
type tokenSnapshot struct {
	ID         int64      `json:"id"`
	Owner      string     `json:"owner"`
	Annotation string     `json:"annotation"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

// tokenScopes is the audited view of a token's three scope tables.
// Every field is an empty slice rather than nil when the scope is
// unset, so the details JSON carries [] rather than null.
type tokenScopes struct {
	Connections []ScopedConnection `json:"connections"`
	Tools       []string           `json:"tools"`
	Admin       []string           `json:"admin"`
}

// tokenDeleteSnapshot is the before state recorded by token.delete: the
// token row itself along with every scope it held.
type tokenDeleteSnapshot struct {
	tokenSnapshot
	tokenScopes
}

// tokenSnapshotTx reads a token's snapshot inside the caller's
// transaction, resolving the owner id to a username. sql.ErrNoRows is
// returned unwrapped when the token does not exist.
func tokenSnapshotTx(tx *sql.Tx, tokenID int64) (tokenSnapshot, error) {
	var (
		snap       tokenSnapshot
		annotation sql.NullString
		owner      sql.NullString
	)

	err := tx.QueryRow(
		`SELECT t.id, u.username, t.annotation, t.expires_at
         FROM tokens t
         LEFT JOIN users u ON u.id = t.owner_id
         WHERE t.id = ?`,
		tokenID,
	).Scan(&snap.ID, &owner, &annotation, &snap.ExpiresAt)
	if err != nil {
		return snap, err
	}

	snap.Owner = owner.String
	snap.Annotation = annotation.String

	return snap, nil
}

// tokenAnnotationTx returns a token's annotation for use as an audit
// target name, and false when the token row cannot be read. A missing
// row is not an error here: the scope setters have never required the
// token to exist, so a failure to label the event must not change what
// they do.
func tokenAnnotationTx(tx *sql.Tx, tokenID int64) (string, bool) {
	var annotation sql.NullString

	if err := tx.QueryRow(
		"SELECT annotation FROM tokens WHERE id = ?", tokenID,
	).Scan(&annotation); err != nil {
		return "", false
	}

	return annotation.String, true
}

// tokenConnectionScopeTx reads a token's connection scope inside the
// caller's transaction, in connection id order.
func tokenConnectionScopeTx(tx *sql.Tx, tokenID int64) ([]ScopedConnection, error) {
	rows, err := tx.Query(
		`SELECT connection_id, access_level FROM token_connection_scope
         WHERE token_id = ? ORDER BY connection_id`,
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to read token connection scope: %w", err)
	}
	defer rows.Close()

	connections := []ScopedConnection{}
	for rows.Next() {
		var conn ScopedConnection
		if err := rows.Scan(&conn.ConnectionID, &conn.AccessLevel); err != nil {
			return nil, fmt.Errorf("failed to scan connection scope: %w", err)
		}
		connections = append(connections, conn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read token connection scope: %w", err)
	}

	return connections, nil
}

// tokenMCPScopeIDsTx reads a token's MCP privilege scope as privilege
// identifier ids, in id order.
func tokenMCPScopeIDsTx(tx *sql.Tx, tokenID int64) ([]int64, error) {
	rows, err := tx.Query(
		`SELECT privilege_identifier_id FROM token_mcp_scope
         WHERE token_id = ? ORDER BY privilege_identifier_id`,
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to read token MCP scope: %w", err)
	}
	defer rows.Close()

	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan privilege ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read token MCP scope: %w", err)
	}

	return ids, nil
}

// mcpWildcardIdentifier is how the wildcard sentinel is rendered in the
// audit log, matching what GetTokenMCPScope returns to callers.
const mcpWildcardIdentifier = "*"

// tokenMCPScopeNamesTx reads a token's MCP privilege scope as privilege
// identifiers, in identifier order. The wildcard sentinel is rendered
// as "*", matching what GetTokenMCPScope returns.
//
// The outer query drops NULL identifiers, which the LEFT JOIN produces
// for a scope row whose privilege has since been removed, so that the
// result is a plain list of names.
func tokenMCPScopeNamesTx(tx *sql.Tx, tokenID int64) ([]string, error) {
	return scanStringsTx(tx, `
        SELECT identifier FROM (
            SELECT CASE WHEN tms.privilege_identifier_id = ? THEN ?
                        ELSE mpi.identifier END AS identifier
            FROM token_mcp_scope tms
            LEFT JOIN mcp_privilege_identifiers mpi
                ON mpi.id = tms.privilege_identifier_id
            WHERE tms.token_id = ?
        )
        WHERE identifier IS NOT NULL
        ORDER BY identifier`, "failed to read token MCP scope",
		MCPPrivilegeIDWildcard, mcpWildcardIdentifier, tokenID)
}

// tokenAdminScopeTx reads a token's admin permission scope, in
// permission order.
func tokenAdminScopeTx(tx *sql.Tx, tokenID int64) ([]string, error) {
	rows, err := tx.Query(
		`SELECT permission FROM token_admin_scope
         WHERE token_id = ? ORDER BY permission`,
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to read token admin scope: %w", err)
	}
	defer rows.Close()

	permissions := []string{}
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return nil, fmt.Errorf("failed to scan admin permission: %w", err)
		}
		permissions = append(permissions, permission)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read token admin scope: %w", err)
	}

	return permissions, nil
}

// tokenScopesTx reads all three of a token's scopes, for the events
// that record a token's whole scope rather than one dimension of it.
func tokenScopesTx(tx *sql.Tx, tokenID int64) (tokenScopes, error) {
	var scopes tokenScopes

	connections, err := tokenConnectionScopeTx(tx, tokenID)
	if err != nil {
		return scopes, err
	}
	tools, err := tokenMCPScopeNamesTx(tx, tokenID)
	if err != nil {
		return scopes, err
	}
	admin, err := tokenAdminScopeTx(tx, tokenID)
	if err != nil {
		return scopes, err
	}

	scopes.Connections = connections
	scopes.Tools = tools
	scopes.Admin = admin

	return scopes, nil
}

// =============================================================================
// Actor-attributed token mutations
// =============================================================================

// CreateToken creates a new token owned by the specified user,
// attributing the change to this store's actor.
func (a *ActorStore) CreateToken(ownerUsername, annotation string,
	requestedExpiry *time.Time) (string, *StoredToken, error) {

	return a.s.createToken(a.actor, ownerUsername, annotation, requestedExpiry)
}

// DeleteUserToken deletes a token owned by the named user, attributing
// the change to this store's actor.
func (a *ActorStore) DeleteUserToken(username string, tokenID int64) error {
	return a.s.deleteUserToken(a.actor, username, tokenID)
}

// DeleteToken deletes a token by ID or hash prefix, attributing the
// change to this store's actor.
func (a *ActorStore) DeleteToken(identifier string) error {
	return a.s.deleteToken(a.actor, identifier)
}

// SetTokenConnectionScope sets a token's connection scope, attributing
// the change to this store's actor.
func (a *ActorStore) SetTokenConnectionScope(tokenID int64,
	connections []ScopedConnection) error {

	return a.s.setTokenConnectionScope(a.actor, tokenID, connections)
}

// SetTokenMCPScope sets a token's MCP privilege scope by privilege id,
// attributing the change to this store's actor.
func (a *ActorStore) SetTokenMCPScope(tokenID int64, privilegeIDs []int64) error {
	return a.s.setTokenMCPScope(a.actor, tokenID, privilegeIDs)
}

// SetTokenMCPScopeByNames sets a token's MCP privilege scope by
// privilege identifier, attributing the change to this store's actor.
func (a *ActorStore) SetTokenMCPScopeByNames(tokenID int64,
	identifiers []string) error {

	return a.s.setTokenMCPScopeByNames(a.actor, tokenID, identifiers)
}

// SetTokenAdminScope sets a token's admin permission scope, attributing
// the change to this store's actor.
func (a *ActorStore) SetTokenAdminScope(tokenID int64, permissions []string) error {
	return a.s.setTokenAdminScope(a.actor, tokenID, permissions)
}

// ClearTokenScope removes all of a token's scope restrictions,
// attributing the change to this store's actor.
func (a *ActorStore) ClearTokenScope(tokenID int64) error {
	return a.s.clearTokenScope(a.actor, tokenID)
}
