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

// ErrUnknownMCPPrivilege is returned when an MCP scope names a privilege
// identifier that is not registered. Writing the rest of the list would
// silently drop the unknown name, and a list made up only of unknown
// names would then store no scope at all, which reads as unrestricted,
// so the whole change is refused instead.
var ErrUnknownMCPPrivilege = errors.New("unknown MCP privilege identifier")

// =============================================================================
// Token Scope Management
// =============================================================================

// ErrInvalidAccessLevel is returned when a connection scope entry names
// an access level other than read or read_write.
var ErrInvalidAccessLevel = errors.New("invalid access level")

// ErrInvalidConnectionScope is returned when a connection scope is not a
// plain list of distinct connections: it names a connection twice,
// names a negative connection ID, or mixes the "all connections" entry
// with entries for particular connections.
//
// A mixed scope is refused because its two kinds of entry answer the
// same question differently: IsConnectionInTokenScope lets an entry for
// a connection override the "all connections" entry, so {all:
// read_write, 5: read} holds connection 5 at read only, yet a check that
// reads the "all connections" entry alone would take the token to hold
// every connection at read_write, and let it hand out read_write on
// connection 5. One kind of entry or the other says everything a scope
// needs to.
var ErrInvalidConnectionScope = errors.New("invalid connection scope")

// ErrTokenNotFound is returned by the token scope reads for a token
// that does not exist. A token with no rows in a scope kind is
// unrestricted in that kind, so without this a token deleted part-way
// through a request, which takes its scope rows with it by the cascade,
// would read as unrestricted to every check made after the delete.
// Every access check denies on a scope read error, so a missing token
// reads as having no access at all.
var ErrTokenNotFound = errors.New("token not found")

// requireTokenLocked reports ErrTokenNotFound when tokenID names no
// token. The caller holds s.mu, read or write, so the answer holds for
// the rest of the caller's read: deleting a token takes the write lock.
func (s *AuthStore) requireTokenLocked(tokenID int64) error {
	var exists bool
	if err := s.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM tokens WHERE id = ?)", tokenID,
	).Scan(&exists); err != nil {
		return fmt.Errorf("failed to check token exists: %w", err)
	}
	if !exists {
		return ErrTokenNotFound
	}
	return nil
}

// ValidateScopedConnections checks that a connection scope is well
// formed, so that a caller writing several scope kinds can refuse a bad
// connection scope before writing any of them: every entry's access
// level is read or read_write, no connection is named twice or with a
// negative ID, and the "all connections" entry, if present, is the only
// entry (see ErrInvalidConnectionScope).
func ValidateScopedConnections(connections []ScopedConnection) error {
	seen := make(map[int]bool, len(connections))
	for _, conn := range connections {
		if conn.AccessLevel != AccessLevelRead &&
			conn.AccessLevel != AccessLevelReadWrite {
			return fmt.Errorf("%w %q for connection %d: must be %q or %q",
				ErrInvalidAccessLevel, conn.AccessLevel, conn.ConnectionID,
				AccessLevelRead, AccessLevelReadWrite)
		}
		if conn.ConnectionID < 0 {
			return fmt.Errorf("%w: connection ID %d is negative",
				ErrInvalidConnectionScope, conn.ConnectionID)
		}
		if seen[conn.ConnectionID] {
			return fmt.Errorf("%w: connection %d is listed more than once",
				ErrInvalidConnectionScope, conn.ConnectionID)
		}
		seen[conn.ConnectionID] = true
	}
	if seen[ConnectionIDAll] && len(connections) > 1 {
		return fmt.Errorf("%w: the all-connections entry (connection 0) "+
			"cannot be combined with entries for particular connections",
			ErrInvalidConnectionScope)
	}
	return nil
}

// setTokenConnectionScope sets the connection scope for a token. If
// connections is empty, it clears all connection scoping (the token has
// no connection restrictions).
func (s *AuthStore) setTokenConnectionScope(actor Actor, tokenID int64,
	connections []ScopedConnection, superuserOwnerAllowed bool) (err error) {

	// The stored access level is checked here rather than left to the
	// SQLite CHECK constraint, so that a bad level is reported as what
	// it is instead of an opaque insert failure, and so that the rule
	// is visible to a reader of this code.
	if err := ValidateScopedConnections(connections); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, target, err := s.beginTokenScopeChange(actor, tokenID,
		actionSetConnectionScope, superuserOwnerAllowed)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	details, err := writeTokenConnectionScopeTx(tx, tokenID, connections)
	if err != nil {
		return err
	}

	return s.commitTokenScopeChange(tx, actor, target, details)
}

// writeTokenConnectionScopeTx replaces a token's connection scope inside
// tx and returns the before and after scope for the audit event.
func writeTokenConnectionScopeTx(tx *sql.Tx, tokenID int64,
	connections []ScopedConnection) (map[string]any, error) {

	before, err := tokenConnectionScopeTx(tx, tokenID)
	if err != nil {
		return nil, err
	}

	// Clear existing scope
	if _, execErr := tx.Exec(
		"DELETE FROM token_connection_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		return nil, fmt.Errorf("failed to clear token connection scope: %w", execErr)
	}

	// Add new scope entries
	for _, conn := range connections {
		if _, execErr := tx.Exec(
			"INSERT INTO token_connection_scope (token_id, connection_id, access_level) VALUES (?, ?, ?)",
			tokenID, conn.ConnectionID, conn.AccessLevel,
		); execErr != nil {
			return nil, fmt.Errorf("failed to add connection to token scope: %w", execErr)
		}
	}

	after, err := tokenConnectionScopeTx(tx, tokenID)
	if err != nil {
		return nil, err
	}

	return map[string]any{"before": before, "after": after}, nil
}

// The audit actions of the token scope mutations.
const (
	actionSetConnectionScope = "token.scope.set_connections"
	actionSetMCPScope        = "token.scope.set_tools"
	actionSetAdminScope      = "token.scope.set_admin"
	actionClearScope         = "token.scope.clear"
)

// beginTokenScopeChange opens the transaction shared by every token
// scope mutation and builds its audit target. The token's annotation
// labels the event when the row can be read; a token that does not
// exist is not an error, because these methods have never required one.
// When superuserOwnerAllowed is false it then refuses a token owned by a
// superuser (see guardSuperuserOwnedTokenTx), rolling the transaction
// back and recording the failure itself. The caller must hold s.mu.
func (s *AuthStore) beginTokenScopeChange(actor Actor, tokenID int64,
	action string, superuserOwnerAllowed bool) (*sql.Tx, *auditTarget, error) {

	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	id := tokenID
	target := &auditTarget{
		action:     action,
		targetType: "token",
		targetID:   &id,
	}
	if annotation, ok := tokenAnnotationTx(tx, tokenID); ok {
		target.targetName = annotation
	}

	if err := guardSuperuserOwnedTokenTx(tx, tokenID,
		superuserOwnerAllowed); err != nil {
		s.failAudit(tx, actor, target, err)
		return nil, nil, err
	}

	return tx, target, nil
}

// commitTokenScopeChange records the scope event and commits, so that
// the change and the event that describes it land together. A commit
// error is returned as the driver reports it, which is what
// SetTokenAdminScope has always done.
func (s *AuthStore) commitTokenScopeChange(tx *sql.Tx, actor Actor,
	target *auditTarget, details map[string]any) error {

	if err := s.recordAudit(tx, newEvent(actor, target.action, target.targetType,
		target.targetID, target.targetName, details)); err != nil {
		return err
	}

	return tx.Commit()
}

// setTokenMCPScope sets the MCP privilege scope for a token. If
// privilegeIDs is empty, it clears all MCP scoping (the token has no MCP
// restrictions).
func (s *AuthStore) setTokenMCPScope(actor Actor, tokenID int64,
	privilegeIDs []int64, superuserOwnerAllowed bool) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, target, err := s.beginTokenScopeChange(actor, tokenID,
		actionSetMCPScope, superuserOwnerAllowed)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	before, err := tokenMCPScopeIDsTx(tx, tokenID)
	if err != nil {
		return err
	}

	// Clear existing scope
	if _, execErr := tx.Exec(
		"DELETE FROM token_mcp_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		err = fmt.Errorf("failed to clear token MCP scope: %w", execErr)
		return err
	}

	// Add new scope entries
	for _, privID := range privilegeIDs {
		if _, execErr := tx.Exec(
			"INSERT INTO token_mcp_scope (token_id, privilege_identifier_id) VALUES (?, ?)",
			tokenID, privID,
		); execErr != nil {
			err = fmt.Errorf("failed to add privilege to token scope: %w", execErr)
			return err
		}
	}

	after, err := tokenMCPScopeIDsTx(tx, tokenID)
	if err != nil {
		return err
	}

	return s.commitTokenScopeChange(tx, actor, target,
		map[string]any{"before": before, "after": after})
}

// MCPPrivilegeIDWildcard is the sentinel privilege_identifier_id stored in
// token_mcp_scope to represent a wildcard ("all MCP privileges") grant.
const MCPPrivilegeIDWildcard int64 = 0

// setTokenMCPScopeByNames sets the MCP privilege scope for a token using
// privilege identifiers. If identifiers contains "*", a single wildcard
// entry is stored instead of looking up individual privilege IDs.
func (s *AuthStore) setTokenMCPScopeByNames(actor Actor, tokenID int64,
	identifiers []string, superuserOwnerAllowed bool) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, target, err := s.beginTokenScopeChange(actor, tokenID,
		actionSetMCPScope, superuserOwnerAllowed)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	details, err := writeTokenMCPScopeByNamesTx(tx, tokenID, identifiers)
	if err != nil {
		return err
	}

	return s.commitTokenScopeChange(tx, actor, target, details)
}

// writeTokenMCPScopeByNamesTx replaces a token's MCP scope inside tx
// with the named privileges and returns the before and after scope for
// the audit event. A name that is not registered fails the write with
// ErrUnknownMCPPrivilege.
func writeTokenMCPScopeByNamesTx(tx *sql.Tx, tokenID int64,
	identifiers []string) (map[string]any, error) {

	before, err := tokenMCPScopeNamesTx(tx, tokenID)
	if err != nil {
		return nil, err
	}

	// Clear existing scope
	if _, execErr := tx.Exec(
		"DELETE FROM token_mcp_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		return nil, fmt.Errorf("failed to clear token MCP scope: %w", execErr)
	}

	// Add new scope entries
	for _, identifier := range identifiers {
		if identifier == mcpWildcardIdentifier {
			// Store wildcard sentinel (privilege_identifier_id = 0)
			if _, execErr := tx.Exec(
				"INSERT INTO token_mcp_scope (token_id, privilege_identifier_id) VALUES (?, ?)",
				tokenID, MCPPrivilegeIDWildcard,
			); execErr != nil {
				return nil, fmt.Errorf("failed to add wildcard privilege to token scope: %w", execErr)
			}
			// Wildcard covers everything; skip remaining identifiers
			break
		}

		res, execErr := tx.Exec(
			`INSERT INTO token_mcp_scope (token_id, privilege_identifier_id)
             SELECT ?, id FROM mcp_privilege_identifiers WHERE identifier = ?`,
			tokenID, identifier,
		)
		if execErr != nil {
			return nil, fmt.Errorf("failed to add privilege to token scope: %w", execErr)
		}
		inserted, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			return nil, fmt.Errorf("failed to add privilege to token scope: %w", rowsErr)
		}
		if inserted == 0 {
			return nil, fmt.Errorf("%w: %q", ErrUnknownMCPPrivilege, identifier)
		}
	}

	after, err := tokenMCPScopeNamesTx(tx, tokenID)
	if err != nil {
		return nil, err
	}

	return map[string]any{"before": before, "after": after}, nil
}

// GetTokenScope retrieves the complete scope configuration for a token
// Returns nil if the token has no scope restrictions
func (s *AuthStore) GetTokenScope(tokenID int64) (*TokenScope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.requireTokenLocked(tokenID); err != nil {
		return nil, err
	}

	scope := &TokenScope{TokenID: tokenID}

	// Get connection scope
	rows, err := s.db.Query(
		"SELECT connection_id, access_level FROM token_connection_scope WHERE token_id = ?",
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get token connection scope: %w", err)
	}

	for rows.Next() {
		var conn ScopedConnection
		if err := rows.Scan(&conn.ConnectionID, &conn.AccessLevel); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan connection scope: %w", err)
		}
		scope.Connections = append(scope.Connections, conn)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("error iterating connection scope: %w", err)
	}
	rows.Close()

	// Get MCP scope
	rows, err = s.db.Query(
		"SELECT privilege_identifier_id FROM token_mcp_scope WHERE token_id = ?",
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get token MCP scope: %w", err)
	}

	for rows.Next() {
		var privID int64
		if err := rows.Scan(&privID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan privilege ID: %w", err)
		}
		scope.MCPPrivileges = append(scope.MCPPrivileges, privID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("error iterating MCP scope: %w", err)
	}
	rows.Close()

	// Get admin scope
	rows, err = s.db.Query(
		"SELECT permission FROM token_admin_scope WHERE token_id = ?",
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get token admin scope: %w", err)
	}

	for rows.Next() {
		var perm string
		if err := rows.Scan(&perm); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan admin permission: %w", err)
		}
		scope.AdminPermissions = append(scope.AdminPermissions, perm)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("error iterating admin scope: %w", err)
	}
	rows.Close()

	// Return nil if no scope is defined
	if len(scope.Connections) == 0 && len(scope.MCPPrivileges) == 0 && len(scope.AdminPermissions) == 0 {
		return nil, nil
	}

	return scope, nil
}

// clearTokenScope removes all scope restrictions from a token.
func (s *AuthStore) clearTokenScope(actor Actor, tokenID int64,
	superuserOwnerAllowed bool) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, target, err := s.beginTokenScopeChange(actor, tokenID,
		actionClearScope, superuserOwnerAllowed)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	before, err := tokenScopesTx(tx, tokenID)
	if err != nil {
		return err
	}

	if _, execErr := tx.Exec(
		"DELETE FROM token_connection_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		err = fmt.Errorf("failed to clear token connection scope: %w", execErr)
		return err
	}

	if _, execErr := tx.Exec(
		"DELETE FROM token_mcp_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		err = fmt.Errorf("failed to clear token MCP scope: %w", execErr)
		return err
	}

	if _, execErr := tx.Exec(
		"DELETE FROM token_admin_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		err = fmt.Errorf("failed to clear token admin scope: %w", execErr)
		return err
	}

	return s.commitTokenScopeChange(tx, actor, target,
		map[string]any{"before": before})
}

// IsConnectionInTokenScope checks if a connection is within a token's scope.
// Returns (inScope, accessLevel, error) where:
// - inScope is true if no connection scope is defined (unrestricted) or the connection is in scope.
// - accessLevel is the scoped access level ("read" or "read_write"), or empty if unrestricted.
func (s *AuthStore) IsConnectionInTokenScope(tokenID int64, connectionID int) (bool, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.requireTokenLocked(tokenID); err != nil {
		return false, "", err
	}

	// Check if token has any connection scope
	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM token_connection_scope WHERE token_id = ?",
		tokenID,
	).Scan(&count)
	if err != nil {
		return false, "", fmt.Errorf("failed to check connection scope: %w", err)
	}

	// No scope means unrestricted
	if count == 0 {
		return true, AccessLevelNone, nil
	}

	// Check for specific connection
	var accessLevel string
	err = s.db.QueryRow(
		"SELECT access_level FROM token_connection_scope WHERE token_id = ? AND connection_id = ?",
		tokenID, connectionID,
	).Scan(&accessLevel)
	if err == nil {
		return true, accessLevel, nil
	}
	if err != sql.ErrNoRows {
		return false, "", fmt.Errorf("failed to check connection in scope: %w", err)
	}

	// Check for wildcard connection (connection_id = 0 means all connections)
	err = s.db.QueryRow(
		"SELECT access_level FROM token_connection_scope WHERE token_id = ? AND connection_id = ?",
		tokenID, ConnectionIDAll,
	).Scan(&accessLevel)
	if err == sql.ErrNoRows {
		return false, AccessLevelNone, nil
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to check wildcard connection in scope: %w", err)
	}

	return true, accessLevel, nil
}

// IsMCPItemInTokenScope checks if an MCP item is within a token's scope
// Returns true if:
// - The token has no MCP scope defined (no restrictions)
// - The scope contains the wildcard sentinel (privilege_identifier_id = 0)
// - The item's privilege is explicitly in the token's scope
func (s *AuthStore) IsMCPItemInTokenScope(tokenID int64, identifier string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.requireTokenLocked(tokenID); err != nil {
		return false, err
	}

	// Check if token has any MCP scope
	var scopeCount int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ?",
		tokenID,
	).Scan(&scopeCount)
	if err != nil {
		return false, fmt.Errorf("failed to check token scope: %w", err)
	}

	// No scope defined - no restrictions
	if scopeCount == 0 {
		return true, nil
	}

	// Check for wildcard sentinel (privilege_identifier_id = 0 means all privileges)
	var wildcardCount int
	err = s.db.QueryRow(
		"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ? AND privilege_identifier_id = ?",
		tokenID, MCPPrivilegeIDWildcard,
	).Scan(&wildcardCount)
	if err != nil {
		return false, fmt.Errorf("failed to check wildcard scope: %w", err)
	}
	if wildcardCount > 0 {
		return true, nil
	}

	// Check if this item is in scope
	var inScope int
	err = s.db.QueryRow(
		`SELECT COUNT(*) FROM token_mcp_scope tms
         JOIN mcp_privilege_identifiers mpi ON tms.privilege_identifier_id = mpi.id
         WHERE tms.token_id = ? AND mpi.identifier = ?`,
		tokenID, identifier,
	).Scan(&inScope)
	if err != nil {
		return false, fmt.Errorf("failed to check item in scope: %w", err)
	}

	return inScope > 0, nil
}

// GetTokenConnectionScope returns just the connection IDs in scope for a token
func (s *AuthStore) GetTokenConnectionScope(tokenID int64) ([]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.requireTokenLocked(tokenID); err != nil {
		return nil, err
	}

	rows, err := s.db.Query(
		"SELECT connection_id FROM token_connection_scope WHERE token_id = ? ORDER BY connection_id",
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get token connection scope: %w", err)
	}
	defer rows.Close()

	var connections []int
	for rows.Next() {
		var connID int
		if err := rows.Scan(&connID); err != nil {
			return nil, fmt.Errorf("failed to scan connection ID: %w", err)
		}
		connections = append(connections, connID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating token connection scope: %w", err)
	}

	return connections, nil
}

// HasTokenMCPScope reports whether a token has any MCP scope row at
// all. It counts the raw rows, as IsMCPItemInTokenScope does, so a row
// whose privilege identifier has since been deleted still marks the
// token as MCP-restricted even though GetTokenMCPScope, which joins the
// identifiers, no longer returns it.
func (s *AuthStore) HasTokenMCPScope(tokenID int64) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.requireTokenLocked(tokenID); err != nil {
		return false, err
	}

	var count int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ?",
		tokenID,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to check token MCP scope: %w", err)
	}
	return count > 0, nil
}

// GetTokenMCPScope returns the MCP privilege identifiers in scope for a token.
// If the scope contains the wildcard sentinel (privilege_identifier_id = 0),
// this returns ["*"].
func (s *AuthStore) GetTokenMCPScope(tokenID int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.requireTokenLocked(tokenID); err != nil {
		return nil, err
	}

	// Check for wildcard sentinel first
	var wildcardCount int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ? AND privilege_identifier_id = ?",
		tokenID, MCPPrivilegeIDWildcard,
	).Scan(&wildcardCount)
	if err != nil {
		return nil, fmt.Errorf("failed to check wildcard MCP scope: %w", err)
	}
	if wildcardCount > 0 {
		return []string{"*"}, nil
	}

	rows, err := s.db.Query(
		`SELECT mpi.identifier FROM token_mcp_scope tms
         JOIN mcp_privilege_identifiers mpi ON tms.privilege_identifier_id = mpi.id
         WHERE tms.token_id = ?
         ORDER BY mpi.identifier`,
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get token MCP scope: %w", err)
	}
	defer rows.Close()

	var identifiers []string
	for rows.Next() {
		var identifier string
		if err := rows.Scan(&identifier); err != nil {
			return nil, fmt.Errorf("failed to scan identifier: %w", err)
		}
		identifiers = append(identifiers, identifier)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating token MCP scope: %w", err)
	}

	return identifiers, nil
}

// HasTokenScope checks if a token has any scope restrictions
func (s *AuthStore) HasTokenScope(tokenID int64) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.requireTokenLocked(tokenID); err != nil {
		return false, err
	}

	var connCount, mcpCount, adminCount int

	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM token_connection_scope WHERE token_id = ?",
		tokenID,
	).Scan(&connCount)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to check connection scope: %w", err)
	}

	err = s.db.QueryRow(
		"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ?",
		tokenID,
	).Scan(&mcpCount)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to check MCP scope: %w", err)
	}

	err = s.db.QueryRow(
		"SELECT COUNT(*) FROM token_admin_scope WHERE token_id = ?",
		tokenID,
	).Scan(&adminCount)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to check admin scope: %w", err)
	}

	return connCount > 0 || mcpCount > 0 || adminCount > 0, nil
}

// AdminPermissionWildcard is the sentinel permission string stored in
// token_admin_scope to represent a wildcard ("all admin permissions") grant.
const AdminPermissionWildcard = "*"

// ErrUnknownAdminPermission is returned when an admin scope names a
// permission that does not exist. Such an entry would match nothing,
// so a scope of typos would read as a restriction whilst its holder
// could never tell why every admin call was refused, and a name added
// later would silently start to match.
var ErrUnknownAdminPermission = errors.New("unknown admin permission")

// knownAdminPermissions is every admin permission a scope may name, and
// the wildcard. It matches the CHECK constraint on
// group_admin_permissions.permission.
var knownAdminPermissions = map[string]bool{
	PermManageConnections:          true,
	PermManageGroups:               true,
	PermManagePermissions:          true,
	PermManageUsers:                true,
	PermManageTokenScopes:          true,
	PermManageBlackouts:            true,
	PermManageProbes:               true,
	PermManageAlertRules:           true,
	PermManageNotificationChannels: true,
	PermStoreSystemMemory:          true,
	AdminPermissionWildcard:        true,
}

// ValidateAdminPermissions checks that every entry of an admin scope is
// a known admin permission or the wildcard, so that a caller writing
// several scope kinds can refuse a bad admin scope before writing any
// of them.
func ValidateAdminPermissions(permissions []string) error {
	for _, permission := range permissions {
		if !knownAdminPermissions[permission] {
			return fmt.Errorf("%w: %q", ErrUnknownAdminPermission, permission)
		}
	}
	return nil
}

// setTokenAdminScope sets the admin permission scope for a token, which
// restricts which admin permissions the token can use. If permissions
// contains "*", a single wildcard entry is stored instead of individual
// permission strings.
func (s *AuthStore) setTokenAdminScope(actor Actor, tokenID int64,
	permissions []string, superuserOwnerAllowed bool) (err error) {

	if err := ValidateAdminPermissions(permissions); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, target, err := s.beginTokenScopeChange(actor, tokenID,
		actionSetAdminScope, superuserOwnerAllowed)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	details, err := writeTokenAdminScopeTx(tx, tokenID, permissions)
	if err != nil {
		return err
	}

	return s.commitTokenScopeChange(tx, actor, target, details)
}

// writeTokenAdminScopeTx replaces a token's admin scope inside tx and
// returns the before and after scope for the audit event. The caller
// validates the permissions first.
func writeTokenAdminScopeTx(tx *sql.Tx, tokenID int64,
	permissions []string) (map[string]any, error) {

	before, err := tokenAdminScopeTx(tx, tokenID)
	if err != nil {
		return nil, err
	}

	// Clear existing admin scope
	if _, execErr := tx.Exec(
		"DELETE FROM token_admin_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		return nil, fmt.Errorf("failed to clear admin scope: %w", execErr)
	}

	// Insert new admin permissions
	for _, perm := range permissions {
		if perm == AdminPermissionWildcard {
			// Store only the wildcard; skip remaining permissions
			if _, execErr := tx.Exec(
				"INSERT INTO token_admin_scope (token_id, permission) VALUES (?, ?)",
				tokenID, AdminPermissionWildcard,
			); execErr != nil {
				return nil, fmt.Errorf("failed to add wildcard admin permission to token scope: %w", execErr)
			}
			break
		}

		if _, execErr := tx.Exec(
			"INSERT INTO token_admin_scope (token_id, permission) VALUES (?, ?)",
			tokenID, perm,
		); execErr != nil {
			return nil, fmt.Errorf("failed to add admin permission %s to token scope: %w", perm, execErr)
		}
	}

	after, err := tokenAdminScopeTx(tx, tokenID)
	if err != nil {
		return nil, err
	}

	return map[string]any{"before": before, "after": after}, nil
}

// GetTokenAdminScope returns the admin permissions in a token's scope.
func (s *AuthStore) GetTokenAdminScope(tokenID int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.requireTokenLocked(tokenID); err != nil {
		return nil, err
	}

	rows, err := s.db.Query(
		"SELECT permission FROM token_admin_scope WHERE token_id = ?",
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get admin scope: %w", err)
	}
	defer rows.Close()

	var permissions []string
	for rows.Next() {
		var perm string
		if err := rows.Scan(&perm); err != nil {
			return nil, fmt.Errorf("failed to scan admin permission: %w", err)
		}
		permissions = append(permissions, perm)
	}
	return permissions, rows.Err()
}

// IsAdminPermissionInTokenScope checks if a given admin permission is in the token's scope.
// Returns true if the token has no admin scope (unrestricted), if the scope
// contains the wildcard "*", or if the permission is explicitly in scope.
func (s *AuthStore) IsAdminPermissionInTokenScope(tokenID int64, permission string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.requireTokenLocked(tokenID); err != nil {
		return false, err
	}

	// Check if token has any admin scope at all
	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM token_admin_scope WHERE token_id = ?",
		tokenID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check admin scope: %w", err)
	}

	// No admin scope means unrestricted
	if count == 0 {
		return true, nil
	}

	// Check for wildcard ("*" means all admin permissions)
	var wildcardCount int
	err = s.db.QueryRow(
		"SELECT COUNT(*) FROM token_admin_scope WHERE token_id = ? AND permission = ?",
		tokenID, AdminPermissionWildcard,
	).Scan(&wildcardCount)
	if err != nil {
		return false, fmt.Errorf("failed to check wildcard admin scope: %w", err)
	}
	if wildcardCount > 0 {
		return true, nil
	}

	// Check if the specific permission is in scope
	err = s.db.QueryRow(
		"SELECT COUNT(*) FROM token_admin_scope WHERE token_id = ? AND permission = ?",
		tokenID, permission,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check admin permission in scope: %w", err)
	}

	return count > 0, nil
}
