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

// guardSuperuserOwnedTokenTx refuses with ErrSuperuserTargetForbidden
// when superuserOwnerAllowed is false and the token's owner, as read
// inside the change's own transaction, is a superuser. A superuser's
// token carries the role up to its scope, so widening that scope hands
// the role out, and narrowing or deleting the token can lock an
// administrator out. Reading the owner here rather than before the call
// means an owner promoted between a handler's read and the write is
// still refused (issue #607), as guardSuperuserTargetTx does for the
// account itself.
//
// A token that does not exist, or whose owner does not, is let through
// for the change to report as it always has; a failed read refuses the
// change.
func guardSuperuserOwnedTokenTx(tx *sql.Tx, tokenID int64,
	superuserOwnerAllowed bool) error {

	if superuserOwnerAllowed {
		return nil
	}
	var ownerIsSuperuser bool
	err := tx.QueryRow(
		`SELECT u.is_superuser FROM tokens t
         JOIN users u ON u.id = t.owner_id
         WHERE t.id = ?`, tokenID).Scan(&ownerIsSuperuser)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read the owner of token %d: %w",
			tokenID, err)
	}
	if ownerIsSuperuser {
		return ErrSuperuserTargetForbidden
	}
	return nil
}

// tokenScopeStep is one scope kind written by setTokenScope: the audit
// action it is recorded under and the write itself.
type tokenScopeStep struct {
	action string
	write  func(tx *sql.Tx) (map[string]any, error)
}

// setTokenScope writes every scope kind the change supplies, in one
// transaction, recording one event per kind as the single-kind setters
// do. A kind left nil is unchanged; a kind given as an empty slice is
// cleared. The MCP scope is written first, then the connection scope,
// then the admin scope, and a failure in any of them, an unknown MCP
// privilege included, leaves every kind as it was. With no kind
// supplied it does nothing.
//
// When superuserOwnerAllowed is false it refuses, with
// ErrSuperuserTargetForbidden, a token whose owner is a superuser when
// the transaction reads the owner (see guardSuperuserOwnedTokenTx).
func (s *AuthStore) setTokenScope(actor Actor, tokenID int64,
	change TokenScopeChange, superuserOwnerAllowed bool) (err error) {

	if change.Connections != nil {
		if err := ValidateScopedConnections(change.Connections); err != nil {
			return err
		}
	}
	if change.AdminPermissions != nil {
		if err := ValidateAdminPermissions(change.AdminPermissions); err != nil {
			return err
		}
	}

	var steps []tokenScopeStep
	if change.MCPPrivileges != nil {
		steps = append(steps, tokenScopeStep{actionSetMCPScope,
			func(tx *sql.Tx) (map[string]any, error) {
				return writeTokenMCPScopeByNamesTx(tx, tokenID,
					change.MCPPrivileges)
			}})
	}
	if change.Connections != nil {
		steps = append(steps, tokenScopeStep{actionSetConnectionScope,
			func(tx *sql.Tx) (map[string]any, error) {
				return writeTokenConnectionScopeTx(tx, tokenID,
					change.Connections)
			}})
	}
	if change.AdminPermissions != nil {
		steps = append(steps, tokenScopeStep{actionSetAdminScope,
			func(tx *sql.Tx) (map[string]any, error) {
				return writeTokenAdminScopeTx(tx, tokenID,
					change.AdminPermissions)
			}})
	}
	if len(steps) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, target, err := s.beginTokenScopeChange(actor, tokenID,
		steps[0].action, superuserOwnerAllowed)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	for _, step := range steps {
		// A failure is recorded under the kind being written when it
		// failed.
		target.action = step.action
		details, writeErr := step.write(tx)
		if writeErr != nil {
			err = writeErr
			return err
		}
		auditErr := s.recordAudit(tx, newEvent(actor, target.action,
			target.targetType, target.targetID, target.targetName,
			details))
		if auditErr != nil {
			err = auditErr
			return err
		}
	}

	err = tx.Commit()
	return err
}
