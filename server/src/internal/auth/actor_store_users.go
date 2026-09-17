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
	"log"

	"golang.org/x/crypto/bcrypt"
)

// userSnapshot is the audited view of a user row. It deliberately omits
// password_hash, so that a before or after snapshot can never carry a
// credential into the audit log.
type userSnapshot struct {
	ID               int64  `json:"id"`
	Username         string `json:"username"`
	DisplayName      string `json:"display_name"`
	Email            string `json:"email"`
	Annotation       string `json:"annotation"`
	Enabled          bool   `json:"enabled"`
	IsSuperuser      bool   `json:"is_superuser"`
	IsServiceAccount bool   `json:"is_service_account"`
}

// userSnapshotColumns lists the snapshot columns in the order
// userSnapshotTx scans them. password_hash is not among them and must
// never be added.
const userSnapshotColumns = `id, username, display_name, email, annotation,
    enabled, is_superuser, is_service_account`

// userSnapshotTx reads a user's snapshot inside the caller's
// transaction, returning sql.ErrNoRows unwrapped when the user does not
// exist so that callers can render their own not-found message.
func userSnapshotTx(tx *sql.Tx, username string) (userSnapshot, error) {
	var (
		snap        userSnapshot
		displayName sql.NullString
		email       sql.NullString
		annotation  sql.NullString
	)

	err := tx.QueryRow(
		"SELECT "+userSnapshotColumns+" FROM users WHERE username = ?",
		username,
	).Scan(&snap.ID, &snap.Username, &displayName, &email, &annotation,
		&snap.Enabled, &snap.IsSuperuser, &snap.IsServiceAccount)
	if err != nil {
		return snap, err
	}

	snap.DisplayName = displayName.String
	snap.Email = email.String
	snap.Annotation = annotation.String

	return snap, nil
}

// userNotFound renders the canonical not-found error for a user, or
// wraps any other lookup failure.
func userNotFound(username string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("user '%s' not found", username)
	}
	return fmt.Errorf("failed to look up user: %w", err)
}

// =============================================================================
// Creation
// =============================================================================

// CreateUser creates a new user, attributing the change to the system
// actor.
func (s *AuthStore) CreateUser(username, password, annotation, displayName,
	email string) error {

	return s.createUser(systemActor, username, password, annotation,
		displayName, email)
}

// CreateUser creates a new user, attributing the change to this store's
// actor.
func (a *ActorStore) CreateUser(username, password, annotation, displayName,
	email string) error {

	return a.s.createUser(a.actor, username, password, annotation,
		displayName, email)
}

func (s *AuthStore) createUser(actor Actor, username, password, annotation,
	displayName, email string) error {

	if err := ValidateUsername(username); err != nil {
		return err
	}
	if err := ValidatePassword(password); err != nil {
		return err
	}

	return s.insertUserAudited(actor, "user.create", username,
		func(tx *sql.Tx) error {
			hash, err := bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost)
			if err != nil {
				return fmt.Errorf("failed to hash password: %w", err)
			}
			if _, err := tx.Exec(
				"INSERT INTO users (username, password_hash, annotation, display_name, email) VALUES (?, ?, ?, ?, ?)",
				username, string(hash), annotation, displayName, email,
			); err != nil {
				return fmt.Errorf("failed to create user: %w", err)
			}
			return nil
		})
}

// CreateServiceAccount creates a new service account user, attributing
// the change to the system actor. Service accounts have no password and
// cannot authenticate via login; they can only be used via API tokens.
func (s *AuthStore) CreateServiceAccount(username, annotation, displayName,
	email string) error {

	return s.createServiceAccount(systemActor, username, annotation,
		displayName, email)
}

// CreateServiceAccount creates a new service account user, attributing
// the change to this store's actor.
func (a *ActorStore) CreateServiceAccount(username, annotation, displayName,
	email string) error {

	return a.s.createServiceAccount(a.actor, username, annotation,
		displayName, email)
}

func (s *AuthStore) createServiceAccount(actor Actor, username, annotation,
	displayName, email string) error {

	if err := ValidateUsername(username); err != nil {
		return err
	}

	return s.insertUserAudited(actor, "service_account.create", username,
		func(tx *sql.Tx) error {
			if _, err := tx.Exec(
				"INSERT INTO users (username, password_hash, annotation, display_name, email, is_service_account, enabled) VALUES (?, '', ?, ?, ?, TRUE, TRUE)",
				username, annotation, displayName, email,
			); err != nil {
				return fmt.Errorf("failed to create service account: %w", err)
			}
			return nil
		})
}

// insertUserAudited runs an insert that creates the named user, records
// the resulting row as the event's after snapshot and commits both
// together. CreateUser and CreateServiceAccount differ only in the
// insert they pass and the action they record.
func (s *AuthStore) insertUserAudited(actor Actor, action, username string,
	insert func(*sql.Tx) error) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	target := &auditTarget{action: action, targetType: "user", targetName: username}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	if insertErr := insert(tx); insertErr != nil {
		err = insertErr
		return err
	}

	after, err := userSnapshotTx(tx, username)
	if err != nil {
		err = fmt.Errorf("failed to read created user: %w", err)
		return err
	}
	target.targetID = &after.ID

	if auditErr := s.recordAudit(tx, newEvent(actor, action, "user", &after.ID,
		username, map[string]any{"after": after})); auditErr != nil {
		err = auditErr
		return err
	}

	if err = tx.Commit(); err != nil {
		err = fmt.Errorf("failed to commit transaction: %w", err)
		return err
	}

	return nil
}

// =============================================================================
// Update
// =============================================================================

// UpdateUser updates a user's password, annotation, display name and
// email, attributing the change to the system actor.
func (s *AuthStore) UpdateUser(username, newPassword, newAnnotation,
	newDisplayName, newEmail string) error {

	return s.updateUser(systemActor, username, newPassword, newAnnotation,
		newDisplayName, newEmail)
}

// UpdateUser updates a user's password, annotation, display name and
// email, attributing the change to this store's actor.
func (a *ActorStore) UpdateUser(username, newPassword, newAnnotation,
	newDisplayName, newEmail string) error {

	return a.s.updateUser(a.actor, username, newPassword, newAnnotation,
		newDisplayName, newEmail)
}

func (s *AuthStore) updateUser(actor Actor, username, newPassword,
	newAnnotation, newDisplayName, newEmail string) (err error) {

	if newPassword != "" {
		if valErr := ValidatePassword(newPassword); valErr != nil {
			return valErr
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	target := &auditTarget{
		action:     "user.update",
		targetType: "user",
		targetName: username,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	before, err := userSnapshotTx(tx, username)
	if err != nil {
		err = userNotFound(username, err)
		return err
	}
	target.targetID = &before.ID

	if newPassword != "" {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(newPassword), s.bcryptCost)
		if hashErr != nil {
			err = fmt.Errorf("failed to hash password: %w", hashErr)
			return err
		}
		// writePasswordHashLocked refuses the write to a federated
		// account, whose password is not managed by this store.
		if writeErr := s.writePasswordHashLocked(tx, username, hash); writeErr != nil {
			err = writeErr
			return err
		}
	}

	if _, execErr := tx.Exec(
		"UPDATE users SET annotation = ?, display_name = ?, email = ? WHERE username = ?",
		newAnnotation, newDisplayName, newEmail, username,
	); execErr != nil {
		err = fmt.Errorf("failed to update user: %w", execErr)
		return err
	}

	after := before
	after.Annotation = newAnnotation
	after.DisplayName = newDisplayName
	after.Email = newEmail

	if auditErr := s.recordUserUpdate(tx, actor, before, after,
		newPassword != ""); auditErr != nil {
		err = auditErr
		return err
	}

	if err = tx.Commit(); err != nil {
		err = fmt.Errorf("failed to commit transaction: %w", err)
		return err
	}

	// Invalidate all active sessions when the password changes.
	if newPassword != "" {
		s.InvalidateUserSessions(username)
	}

	return nil
}

// recordUserUpdate appends the user.update event shared by every
// update entry point.
func (s *AuthStore) recordUserUpdate(tx *sql.Tx, actor Actor,
	before, after userSnapshot, passwordChanged bool) error {

	return s.recordAudit(tx, newEvent(actor, "user.update", "user",
		&before.ID, before.Username, map[string]any{
			"before":           before,
			"after":            after,
			"password_changed": passwordChanged,
		}))
}

// UpdateUserAtomic updates multiple user fields in a single atomic
// transaction, attributing the change to the system actor. Either all
// changes are applied or none are, so a partial update can never leave
// the user in an inconsistent state.
func (s *AuthStore) UpdateUserAtomic(username string, update UserUpdate) error {
	return s.updateUserAtomic(systemActor, username, update)
}

// UpdateUserAtomic updates multiple user fields in a single atomic
// transaction, attributing the change to this store's actor.
func (a *ActorStore) UpdateUserAtomic(username string, update UserUpdate) error {
	return a.s.updateUserAtomic(a.actor, username, update)
}

func (s *AuthStore) updateUserAtomic(actor Actor, username string,
	update UserUpdate) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	target := &auditTarget{
		action:     "user.update",
		targetType: "user",
		targetName: username,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	before, err := userSnapshotTx(tx, username)
	if err != nil {
		err = userNotFound(username, err)
		return err
	}
	target.targetID = &before.ID
	after := before

	passwordChanged := update.Password != nil && *update.Password != ""
	if applyErr := s.applyUserUpdatesTx(tx, username, update,
		&after); applyErr != nil {
		err = applyErr
		return err
	}

	if auditErr := s.recordUserUpdate(tx, actor, before, after,
		passwordChanged); auditErr != nil {
		err = auditErr
		return err
	}

	if err = tx.Commit(); err != nil {
		err = fmt.Errorf("failed to commit transaction: %w", err)
		return err
	}

	// Invalidate all active sessions when the password changes.
	if passwordChanged {
		s.InvalidateUserSessions(username)
	}

	return nil
}

// applyUserUpdatesTx applies each supplied field of the update to the
// user row, in password, profile, then flags order, and mutates after
// to match the state the row will hold once the transaction commits.
func (s *AuthStore) applyUserUpdatesTx(tx *sql.Tx, username string,
	update UserUpdate, after *userSnapshot) error {

	if err := s.applyUserPasswordTx(tx, username, update); err != nil {
		return err
	}
	if err := applyUserProfileTx(tx, username, update, after); err != nil {
		return err
	}

	return applyUserFlagsTx(tx, username, update, after)
}

// applyUserPasswordTx validates and stores a new password hash when the
// update carries a non-empty password, and does nothing otherwise. The
// after snapshot is untouched, because it never carries the hash.
func (s *AuthStore) applyUserPasswordTx(tx *sql.Tx, username string,
	update UserUpdate) error {

	if update.Password == nil || *update.Password == "" {
		return nil
	}

	if valErr := ValidatePassword(*update.Password); valErr != nil {
		return valErr
	}
	hash, hashErr := bcrypt.GenerateFromPassword([]byte(*update.Password), s.bcryptCost)
	if hashErr != nil {
		return fmt.Errorf("failed to hash password: %w", hashErr)
	}
	// writePasswordHashLocked refuses the write to a federated account,
	// whose password is not managed by this store.
	return s.writePasswordHashLocked(tx, username, hash)
}

// applyUserProfileTx updates annotation, display name and email if any
// are provided, carrying the unchanged fields over from the before
// snapshot held in after.
func applyUserProfileTx(tx *sql.Tx, username string, update UserUpdate,
	after *userSnapshot) error {

	if update.Annotation == nil && update.DisplayName == nil &&
		update.Email == nil {
		return nil
	}

	if update.Annotation != nil {
		after.Annotation = *update.Annotation
	}
	if update.DisplayName != nil {
		after.DisplayName = *update.DisplayName
	}
	if update.Email != nil {
		after.Email = *update.Email
	}

	if _, execErr := tx.Exec(
		"UPDATE users SET annotation = ?, display_name = ?, email = ? WHERE username = ?",
		after.Annotation, after.DisplayName, after.Email, username,
	); execErr != nil {
		return fmt.Errorf("failed to update user fields: %w", execErr)
	}

	return nil
}

// applyUserFlagsTx updates the enabled and superuser flags when the
// update supplies them.
func applyUserFlagsTx(tx *sql.Tx, username string, update UserUpdate,
	after *userSnapshot) error {

	if update.Enabled != nil {
		if _, execErr := tx.Exec(
			"UPDATE users SET enabled = ? WHERE username = ?",
			*update.Enabled, username,
		); execErr != nil {
			return fmt.Errorf("failed to update enabled status: %w", execErr)
		}
		after.Enabled = *update.Enabled
	}

	if update.IsSuperuser != nil {
		if _, execErr := tx.Exec(
			"UPDATE users SET is_superuser = ? WHERE username = ?",
			*update.IsSuperuser, username,
		); execErr != nil {
			return fmt.Errorf("failed to update superuser status: %w", execErr)
		}
		after.IsSuperuser = *update.IsSuperuser
	}

	return nil
}

// UpdateUserDisplayName updates a user's display name, attributing the
// change to the system actor.
func (s *AuthStore) UpdateUserDisplayName(username, displayName string) error {
	return s.updateUserField(systemActor, username, "display_name", displayName)
}

// UpdateUserDisplayName updates a user's display name, attributing the
// change to this store's actor.
func (a *ActorStore) UpdateUserDisplayName(username, displayName string) error {
	return a.s.updateUserField(a.actor, username, "display_name", displayName)
}

// UpdateUserEmail updates a user's email address, attributing the
// change to the system actor.
func (s *AuthStore) UpdateUserEmail(username, email string) error {
	return s.updateUserField(systemActor, username, "email", email)
}

// UpdateUserEmail updates a user's email address, attributing the
// change to this store's actor.
func (a *ActorStore) UpdateUserEmail(username, email string) error {
	return a.s.updateUserField(a.actor, username, "email", email)
}

// updateUserField updates one text column of a user row and records the
// resulting user.update event. The column is chosen from a fixed
// allow-list, never from caller input.
func (s *AuthStore) updateUserField(actor Actor, username, column,
	value string) (err error) {

	var (
		stmt    string
		failure string
		apply   func(snap *userSnapshot)
	)
	switch column {
	case "display_name":
		stmt = "UPDATE users SET display_name = ? WHERE username = ?"
		failure = "failed to update display name: %w"
		apply = func(snap *userSnapshot) { snap.DisplayName = value }
	case "email":
		stmt = "UPDATE users SET email = ? WHERE username = ?"
		failure = "failed to update email: %w"
		apply = func(snap *userSnapshot) { snap.Email = value }
	default:
		return fmt.Errorf("unsupported user column %q", column)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	target := &auditTarget{
		action:     "user.update",
		targetType: "user",
		targetName: username,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	before, err := userSnapshotTx(tx, username)
	if err != nil {
		err = userNotFound(username, err)
		return err
	}
	target.targetID = &before.ID

	if _, execErr := tx.Exec(stmt, value, username); execErr != nil {
		err = fmt.Errorf(failure, execErr)
		return err
	}

	after := before
	apply(&after)

	if auditErr := s.recordUserUpdate(tx, actor, before, after, false); auditErr != nil {
		err = auditErr
		return err
	}

	if err = tx.Commit(); err != nil {
		err = fmt.Errorf("failed to commit transaction: %w", err)
		return err
	}

	return nil
}

// =============================================================================
// Enable, disable and superuser
// =============================================================================

// EnableUser enables a user account, attributing the change to the
// system actor.
func (s *AuthStore) EnableUser(username string) error {
	return s.setUserEnabled(systemActor, username, true)
}

// EnableUser enables a user account, attributing the change to this
// store's actor.
func (a *ActorStore) EnableUser(username string) error {
	return a.s.setUserEnabled(a.actor, username, true)
}

// DisableUser disables a user account, attributing the change to the
// system actor.
func (s *AuthStore) DisableUser(username string) error {
	return s.setUserEnabled(systemActor, username, false)
}

// DisableUser disables a user account, attributing the change to this
// store's actor.
func (a *ActorStore) DisableUser(username string) error {
	return a.s.setUserEnabled(a.actor, username, false)
}

func (s *AuthStore) setUserEnabled(actor Actor, username string,
	enabled bool) (err error) {

	action := "user.disable"
	failure := "failed to disable user: %w"
	if enabled {
		action = "user.enable"
		failure = "failed to enable user: %w"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	target := &auditTarget{action: action, targetType: "user", targetName: username}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	before, err := userSnapshotTx(tx, username)
	if err != nil {
		err = userNotFound(username, err)
		return err
	}
	target.targetID = &before.ID

	// Enabling an account also clears its failed-attempt counter, so
	// that an account locked out by repeated failures is usable again
	// as soon as it is enabled, and so that the reset commits with the
	// enable and its audit event rather than in a statement of its own.
	stmt := "UPDATE users SET enabled = ? WHERE id = ?"
	if enabled {
		stmt = "UPDATE users SET enabled = ?, failed_attempts = 0 WHERE id = ?"
	}
	if _, execErr := tx.Exec(stmt, enabled, before.ID); execErr != nil {
		err = fmt.Errorf(failure, execErr)
		return err
	}

	after := before
	after.Enabled = enabled

	details := map[string]any{"before": before, "after": after}
	if enabled {
		details["failed_attempts_reset"] = true
	}

	if auditErr := s.recordAudit(tx, newEvent(actor, action, "user", &before.ID,
		username, details)); auditErr != nil {
		err = auditErr
		return err
	}

	if err = tx.Commit(); err != nil {
		err = fmt.Errorf("failed to commit transaction: %w", err)
		return err
	}

	if !enabled {
		s.InvalidateUserSessions(username)
	}

	return nil
}

// disableForLockout disables a user account after too many failed
// authentication attempts and records the resulting user.disable event,
// attributed to the system actor because no principal asked for the
// change. It reports whether the account was actually disabled, so the
// caller does not log a lockout that did not happen. The caller must
// hold s.mu, which AuthenticateUser does.
//
// The lockout is committed first and the event written afterwards, in a
// transaction of its own. This is the one audited change that is not
// fail-closed, and deliberately so: everywhere else an audit failure
// rolls the change back, because those are changes an operator asked
// for and refusing them is the safe answer. Here the server is
// defending itself against a password-guessing attack, so an audit
// failure, a full disk, a corrupt audit_events or another writer
// holding the lock past the busy timeout, must not hand the attacker
// an account that stays enabled. A lockout with a missing event is
// strictly better than no lockout at all, and the failure is logged.
func (s *AuthStore) disableForLockout(userID int64, username string) bool {
	before, ok := s.commitLockout(userID, username)
	if !ok {
		return false
	}

	after := before
	after.Enabled = false

	if err := s.recordAuditInOwnTx(newEvent(systemActor, "user.disable", "user",
		&before.ID, username, map[string]any{
			"reason": "lockout",
			"before": before,
			"after":  after,
		})); err != nil {
		log.Printf("[AUTH] Failed to record lockout audit event for user %s: %v",
			username, err)
	}

	return true
}

// commitLockout reads the user's before snapshot and disables the
// account in a single committed transaction, returning the snapshot and
// whether the lockout was applied. Every error is logged and swallowed:
// authentication has already failed by this point, so there is no
// caller to report to.
func (s *AuthStore) commitLockout(userID int64,
	username string) (userSnapshot, bool) {

	var before userSnapshot

	tx, err := s.db.Begin()
	if err != nil {
		log.Printf("[AUTH] Failed to begin lockout transaction for user %s: %v",
			username, err)
		return before, false
	}
	committed := false
	defer func() {
		if !committed {
			//nolint:errcheck // Rollback error is not critical; the
			// lockout is best effort and already logged.
			tx.Rollback()
		}
	}()

	before, err = userSnapshotTx(tx, username)
	if err != nil {
		log.Printf("[AUTH] Failed to read user %s for lockout: %v", username, err)
		return before, false
	}

	if _, err := tx.Exec(
		"UPDATE users SET enabled = FALSE WHERE id = ?", userID,
	); err != nil {
		log.Printf("[AUTH] Failed to lock account for user %s: %v", username, err)
		return before, false
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[AUTH] Failed to commit lockout for user %s: %v", username, err)
		return before, false
	}
	committed = true

	return before, true
}

// SetUserSuperuser sets or clears the superuser flag for a user,
// attributing the change to the system actor.
func (s *AuthStore) SetUserSuperuser(username string, isSuperuser bool) error {
	return s.setUserSuperuser(systemActor, username, isSuperuser)
}

// SetUserSuperuser sets or clears the superuser flag for a user,
// attributing the change to this store's actor.
func (a *ActorStore) SetUserSuperuser(username string, isSuperuser bool) error {
	return a.s.setUserSuperuser(a.actor, username, isSuperuser)
}

func (s *AuthStore) setUserSuperuser(actor Actor, username string,
	isSuperuser bool) (err error) {

	action := "user.unset_superuser"
	if isSuperuser {
		action = "user.set_superuser"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	target := &auditTarget{action: action, targetType: "user", targetName: username}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	before, err := userSnapshotTx(tx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = fmt.Errorf("user not found: %s", username)
		} else {
			err = fmt.Errorf("failed to look up user: %w", err)
		}
		return err
	}
	target.targetID = &before.ID

	if _, execErr := tx.Exec(
		"UPDATE users SET is_superuser = ? WHERE id = ?", isSuperuser, before.ID,
	); execErr != nil {
		err = fmt.Errorf("failed to update superuser status: %w", execErr)
		return err
	}

	after := before
	after.IsSuperuser = isSuperuser

	if auditErr := s.recordAudit(tx, newEvent(actor, action, "user", &before.ID,
		username, map[string]any{"before": before, "after": after})); auditErr != nil {
		err = auditErr
		return err
	}

	if err = tx.Commit(); err != nil {
		err = fmt.Errorf("failed to commit transaction: %w", err)
		return err
	}

	return nil
}

// =============================================================================
// Deletion
// =============================================================================

// DeleteUser removes a user and all of its dependent rows in a single
// atomic transaction, attributing the change to the system actor.
//
// This removes every row that references the user: tokens (and their
// scope rows via the token delete cascade), group memberships, and
// connection_sessions rows for any of the user's tokens.
//
// With PRAGMA foreign_keys = ON enabled in NewAuthStore, the ON DELETE
// CASCADE foreign keys declared in the schema would remove most of
// these rows automatically. These explicit deletes are intentionally
// kept as defense in depth: they document exactly which tables are
// touched, survive accidental pragma regression, and clean up
// connection_sessions rows which reference token_hash without an FK.
func (s *AuthStore) DeleteUser(username string) error {
	return s.deleteUser(systemActor, username)
}

// DeleteUser removes a user and all of its dependent rows, attributing
// the change to this store's actor.
func (a *ActorStore) DeleteUser(username string) error {
	return a.s.deleteUser(a.actor, username)
}

func (s *AuthStore) deleteUser(actor Actor, username string) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	target := &auditTarget{
		action:     "user.delete",
		targetType: "user",
		targetName: username,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	// Look up the user first so we can fail fast on "not found", drive
	// the dependent deletes by id rather than by username, and capture
	// the before state for the audit event.
	before, groups, beforeErr := deleteUserBeforeTx(tx, username)
	if beforeErr != nil {
		err = beforeErr
		return err
	}
	userID := before.ID
	target.targetID = &userID

	tokensDeleted, depErr := deleteUserDependentsTx(tx, userID)
	if depErr != nil {
		err = depErr
		return err
	}

	if rowErr := deleteUserRowTx(tx, userID, username); rowErr != nil {
		err = rowErr
		return err
	}

	if auditErr := s.recordAudit(tx, newEvent(actor, "user.delete", "user",
		&userID, username, map[string]any{
			"before":         before,
			"groups":         groups,
			"tokens_deleted": tokensDeleted,
		})); auditErr != nil {
		err = auditErr
		return err
	}

	if commitErr := tx.Commit(); commitErr != nil {
		err = fmt.Errorf("failed to commit user deletion: %w", commitErr)
		return err
	}

	s.InvalidateUserSessions(username)

	return nil
}

// deleteUserBeforeTx captures the state a user delete is about to
// destroy: the user's own snapshot, and the names of the groups the
// user is a direct member of. Both are read before any dependent row
// is removed, so the event says what access was lost.
func deleteUserBeforeTx(tx *sql.Tx, username string) (userSnapshot, []string,
	error) {

	before, snapErr := userSnapshotTx(tx, username)
	if snapErr != nil {
		return before, nil, userNotFound(username, snapErr)
	}

	groups, groupsErr := userGroupNamesTx(tx, before.ID)
	if groupsErr != nil {
		return before, nil, groupsErr
	}

	return before, groups, nil
}

// deleteUserDependentsTx removes every row that references the user:
// connection sessions for the user's tokens, the per-token scope rows,
// the tokens themselves and the user's group memberships. It returns
// the number of tokens deleted, for the audit event.
func deleteUserDependentsTx(tx *sql.Tx, userID int64) (int64, error) {
	// Remove connection_sessions rows for every token owned by this
	// user. connection_sessions references tokens.token_hash but has
	// no declared FK, so SQLite will never cascade-delete these even
	// with foreign_keys pragma on.
	if _, err := tx.Exec(
		`DELETE FROM connection_sessions
         WHERE token_hash IN (SELECT token_hash FROM tokens WHERE owner_id = ?)`,
		userID,
	); err != nil {
		return 0, fmt.Errorf("failed to delete user's connection sessions: %w", err)
	}

	// Remove per-token scope rows for every token owned by this user.
	// These would cascade via tokens' ON DELETE CASCADE, but we delete
	// them explicitly so a pragma regression cannot leave them behind.
	scopeTables := []string{
		"token_connection_scope",
		"token_mcp_scope",
		"token_admin_scope",
	}
	for _, table := range scopeTables {
		//nolint:gosec // table name is from a static allow-list above
		stmt := "DELETE FROM " + table + " WHERE token_id IN (SELECT id FROM tokens WHERE owner_id = ?)"
		if _, err := tx.Exec(stmt, userID); err != nil {
			return 0, fmt.Errorf("failed to delete %s rows: %w", table, err)
		}
	}

	// Remove the user's tokens. tokens.owner_id has ON DELETE CASCADE
	// so the user-row delete would take these out anyway; we delete
	// them first so the scope-row cleanup above has a stable set to
	// operate on and so the behavior is obvious to readers.
	tokenResult, err := tx.Exec("DELETE FROM tokens WHERE owner_id = ?", userID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user's tokens: %w", err)
	}
	tokensDeleted, countErr := tokenResult.RowsAffected()
	if countErr != nil {
		return 0, fmt.Errorf("failed to count deleted tokens: %w", countErr)
	}

	// Remove group memberships that reference this user directly.
	if _, err := tx.Exec(
		"DELETE FROM group_memberships WHERE member_user_id = ?", userID,
	); err != nil {
		return 0, fmt.Errorf("failed to delete user's group memberships: %w", err)
	}

	return tokensDeleted, nil
}

// deleteUserRowTx deletes the user row itself, once its dependent rows
// have gone.
func deleteUserRowTx(tx *sql.Tx, userID int64, username string) error {
	result, execErr := tx.Exec("DELETE FROM users WHERE id = ?", userID)
	if execErr != nil {
		return fmt.Errorf("failed to delete user: %w", execErr)
	}
	// The SQLite driver never fails this call, and a driver that did
	// would leave us unable to tell whether the row went, so the zero
	// value it returns alongside an error lands on the not-found path,
	// which is the answer we would have to give anyway.
	rows, _ := result.RowsAffected() //nolint:errcheck // See above.
	if rows == 0 {
		// This should be unreachable because the caller already looked
		// the user up, but guard against races anyway.
		return fmt.Errorf("user '%s' not found", username)
	}

	return nil
}

// userGroupNamesTx returns the names of the groups the user is a direct
// member of, in name order. An empty result is returned as an empty
// slice so that the audit details carry [] rather than null.
func userGroupNamesTx(tx *sql.Tx, userID int64) ([]string, error) {
	return scanStringsTx(tx, `
        SELECT g.name
        FROM group_memberships m
        JOIN user_groups g ON g.id = m.parent_group_id
        WHERE m.member_user_id = ?
        ORDER BY g.name`, "failed to list user's groups", userID)
}
