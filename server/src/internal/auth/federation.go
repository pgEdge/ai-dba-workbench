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
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pgedge/ai-workbench/server/internal/logging"
)

// CreateSessionForUser mints a session for an already-authenticated user. It is
// the single place a session comes into existence, so the per-user cap, the
// expiry and the eviction of the oldest session behave identically however the
// user proved their identity. Deliberately not gated on auth_source: both
// local and federated logins mint sessions through this function.
//
// Callers must map every error this returns to a single opaque response
// before it reaches a client; the distinct error text below is for the
// server log only, so that an operator can see the real reason, and must
// never be relayed verbatim to an API caller.
func (s *AuthStore) CreateSessionForUser(username string) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user StoredUser
	err := s.db.QueryRow(
		"SELECT id, username, enabled, is_service_account FROM users WHERE username = ?",
		username).Scan(&user.ID, &user.Username, &user.Enabled, &user.IsServiceAccount)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("looking up user: %w", err)
	}
	if !user.Enabled {
		return "", time.Time{}, fmt.Errorf("account is disabled: %s", username)
	}
	if user.IsServiceAccount {
		return "", time.Time{}, fmt.Errorf("service account cannot hold a session: %s", username)
	}

	return s.createSessionForUserLocked(user.Username, user.ID)
}

// createSessionForUserLocked does the work with s.mu already held. Callers are
// responsible for having established that the user exists and is enabled.
func (s *AuthStore) createSessionForUserLocked(username string, userID int64) (string, time.Time, error) {
	// Generate session token
	tokenBytes := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to generate session token: %w", err)
	}
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	// Set expiration
	expiration := time.Now().Add(DefaultSessionExpiry)

	// Enforce per-user session limit by evicting the oldest session when
	// the user has reached maxSessionsPerUser active sessions.
	type sessionEntry struct {
		key       string
		expiresAt time.Time
	}
	var userSessions []sessionEntry
	s.sessions.Range(func(key, value any) bool {
		session, ok := value.(*SessionInfo)
		if !ok || session.Username != username {
			return true
		}
		keyStr, ok := key.(string)
		if !ok {
			return true
		}
		userSessions = append(userSessions, sessionEntry{
			key:       keyStr,
			expiresAt: session.ExpiresAt,
		})
		return true
	})
	if len(userSessions) >= maxSessionsPerUser {
		// Find and evict the oldest session
		oldest := userSessions[0]
		for _, entry := range userSessions[1:] {
			if entry.expiresAt.Before(oldest.expiresAt) {
				oldest = entry
			}
		}
		s.sessions.Delete(oldest.key)
	}

	// Store session in memory using hashed token to prevent timing attacks
	// The hash operation is constant-time with respect to the token content,
	// preventing attackers from inferring valid tokens via response timing
	tokenHash := GetTokenHashByRawToken(token)
	s.sessions.Store(tokenHash, &SessionInfo{
		Username:  username,
		ExpiresAt: expiration,
	})

	// Update last login and reset failed attempts (best effort, non-critical)
	now := time.Now()
	//nolint:errcheck // Best effort update, login already succeeded
	s.db.Exec("UPDATE users SET last_login = ?, failed_attempts = 0 WHERE id = ?", now, userID)

	return token, expiration, nil
}

// =============================================================================
// Federated Identity Resolution
// =============================================================================

// FederatedIdentity is what an identity provider asserted about a user. It
// deliberately mirrors oidc.Identity without importing it, so the auth store
// does not depend on the protocol package.
type FederatedIdentity struct {
	Issuer      string
	Subject     string
	Username    string
	DisplayName string
	Email       string
	Groups      []string
}

// FederationOptions carries the operator's federation policy: whether an
// unknown subject may be provisioned an account, which provider groups map
// onto which Workbench groups, and which provider group (if any) confers
// superuser. Nothing outside GroupMap is ever consulted, so a provider can
// only influence the Workbench groups an operator has explicitly listed.
type FederationOptions struct {
	ProvisionUsers bool
	GroupMap       map[string]string
	SuperuserGroup string
}

// ExternalSubjectKey builds the stable identifier a federated account is
// matched on. Both halves matter: two providers can and do issue the same
// "sub", so the issuer is part of the identity rather than context for it.
//
// The issuer is length-prefixed so the encoding is injective. A plain
// issuer+"|"+subject is not: issuer "https://idp.example.com|evil" with
// subject "x" would produce the same key as issuer "https://idp.example.com"
// with subject "evil|x", so a hostile or mistyped issuer configured alongside
// a legitimate one could be made to collide with an account belonging to the
// legitimate one. With the length prefix, one key can be read back as exactly
// one (issuer, subject) pair.
func ExternalSubjectKey(issuer, subject string) string {
	return fmt.Sprintf("%d|%s|%s", len(issuer), issuer, subject)
}

// federatedUserColumns is the column list every federated lookup scans, in
// the order scanUser expects.
const federatedUserColumns = `id, username, password_hash, created_at, last_login, enabled, annotation,
    display_name, email, failed_attempts, is_superuser, is_service_account, auth_source, external_subject`

// ResolveFederatedUser turns a verified assertion from an identity provider
// into a Workbench user.
//
// The account is matched on ExternalSubjectKey(issuer, subject) held in
// users.external_subject, and never on the username: matching on the username
// would let an identity whose username claim is "admin" take over the local
// "admin" account, which is a complete authentication bypass.
//
// An unknown subject is provisioned only when opts.ProvisionUsers is true, and
// then only when the asserted username is not already taken (compared without
// regard to case, because the Workbench does not case-fold usernames and so
// "Alice" and "alice" are two distinct local accounts). A provisioned account
// is recorded with auth_source = AuthSourceOIDC and a password hash of
// discarded random bytes, so it can never be reached through password login.
//
// Every error returned here names the real reason for the server log only.
// Callers must map them to a single opaque response before anything reaches a
// browser, since the text distinguishes an unknown subject from a disabled
// account from a username collision.
func (s *AuthStore) ResolveFederatedUser(identity FederatedIdentity, opts FederationOptions) (*StoredUser, error) {
	if identity.Issuer == "" || identity.Subject == "" {
		return nil, fmt.Errorf("federated identity is missing an issuer or subject")
	}
	key := ExternalSubjectKey(identity.Issuer, identity.Subject)

	s.mu.Lock()
	defer s.mu.Unlock()

	user, err := s.lookupFederatedUserLocked(key)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return user, nil
	}

	if !opts.ProvisionUsers {
		return nil, fmt.Errorf("no account for federated identity %s (username %q) and provisioning is disabled",
			key, identity.Username)
	}

	return s.provisionFederatedUserLocked(identity, key)
}

// lookupFederatedUserLocked returns the account holding key, or (nil, nil)
// when no account holds it. It applies the account rules a federated login
// must satisfy, so every caller that reaches an existing row goes through the
// same checks. s.mu must be held.
func (s *AuthStore) lookupFederatedUserLocked(key string) (*StoredUser, error) {
	user, err := scanUser(s.db.QueryRow(
		"SELECT "+federatedUserColumns+" FROM users WHERE external_subject = ?", key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("looking up federated user: %w", err)
	}

	// A row carrying an external subject is federated by construction, but
	// check anyway: if some future path ever stamped a subject onto a local
	// or service account, a login must not adopt it.
	if user.AuthSource != AuthSourceOIDC {
		return nil, fmt.Errorf("account %s is not federated (auth_source=%s)",
			user.Username, user.AuthSource)
	}
	if user.IsServiceAccount {
		return nil, fmt.Errorf("service account cannot log in through an identity provider: %s",
			user.Username)
	}
	if !user.Enabled {
		return nil, fmt.Errorf("account is disabled: %s", user.Username)
	}
	return user, nil
}

// provisionFederatedUserLocked creates an account for a previously unseen
// subject. s.mu must be held.
func (s *AuthStore) provisionFederatedUserLocked(identity FederatedIdentity, key string) (*StoredUser, error) {
	if err := ValidateUsername(identity.Username); err != nil {
		return nil, fmt.Errorf("federated username is not usable: %w", err)
	}

	// A malformed email claim is dropped rather than failing the login: the
	// address is profile decoration here, nothing authenticates or grants
	// access on it, and a sloppy identity provider should not lock people out.
	email := identity.Email
	if email != "" {
		if err := ValidateEmail(email); err != nil {
			//nolint:gosec // G706: key passed through logging.SanitizeForLog
			log.Printf("[AUTH] Ignoring malformed email claim for federated subject %s: %v",
				logging.SanitizeForLog(key), err)
			email = ""
		}
	}

	existing, err := s.usernameTakenLocked(identity.Username)
	if err != nil {
		return nil, err
	}
	if existing != "" {
		return nil, fmt.Errorf("refusing to provision federated identity %s: username %q already exists as %q",
			key, identity.Username, existing)
	}

	hash, err := s.unusablePasswordHashLocked()
	if err != nil {
		return nil, err
	}

	result, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, display_name, email, enabled, auth_source, external_subject)
         VALUES (?, ?, ?, ?, TRUE, ?, ?)`,
		identity.Username, hash, identity.DisplayName, email, AuthSourceOIDC, key,
	)
	if err != nil {
		// s.mu makes provisioning sequential within one process, but two servers
		// can share an auth.db, and the unique index on external_subject is
		// what actually decides the race. The loser adopts the winner's row
		// rather than failing a legitimate login.
		if isUniqueViolation(err, "users.external_subject") {
			user, lookupErr := s.lookupFederatedUserLocked(key)
			if lookupErr != nil {
				return nil, lookupErr
			}
			if user != nil {
				return user, nil
			}
		}
		// A username collision that slipped past the check above means the
		// same race created a different account under this name; refuse it
		// for the same reason the check exists.
		if isUniqueViolation(err, "users.username") {
			return nil, fmt.Errorf("refusing to provision federated identity %s: username %q was taken concurrently",
				key, identity.Username)
		}
		return nil, fmt.Errorf("failed to provision federated user: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to read provisioned user ID: %w", err)
	}

	user, err := scanUser(s.db.QueryRow(
		"SELECT "+federatedUserColumns+" FROM users WHERE id = ?", id))
	if err != nil {
		return nil, fmt.Errorf("failed to read back provisioned user: %w", err)
	}
	// Both values originate with the identity provider, and the subject key
	// embeds the raw "sub" claim, so a hostile provider could otherwise put
	// CR/LF in it and forge whole log lines.
	//nolint:gosec // G706: both values passed through logging.SanitizeForLog
	log.Printf("[AUTH] Provisioned federated user %s for subject %s",
		logging.SanitizeForLog(user.Username), logging.SanitizeForLog(key))
	return user, nil
}

// usernameTakenLocked reports the stored spelling of any existing account
// whose username matches candidate without regard to case, or "" when the
// name is free. The comparison is deliberately case-insensitive even though
// the users table is not: "Alice" and "alice" are distinct accounts today, so
// a case-sensitive check would let a federated identity claim "Alice" beside
// a local "alice", and an administrator revoking one would not have revoked
// the other. Folding happens in Go rather than SQL because SQLite's NOCASE
// collation and lower() only fold ASCII. s.mu must be held.
func (s *AuthStore) usernameTakenLocked(candidate string) (string, error) {
	rows, err := s.db.Query("SELECT username FROM users")
	if err != nil {
		return "", fmt.Errorf("failed to check for an existing username: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var existing string
		if err := rows.Scan(&existing); err != nil {
			return "", fmt.Errorf("failed to scan an existing username: %w", err)
		}
		if strings.EqualFold(existing, candidate) {
			return existing, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("failed to read existing usernames: %w", err)
	}
	return "", nil
}

// unusablePasswordHashLocked returns a bcrypt hash of discarded random bytes,
// so no password a caller can supply will ever verify against it. An empty
// hash is never stored, because the empty string is itself an input a caller
// can supply. s.mu must be held, since it reads s.bcryptCost.
func (s *AuthStore) unusablePasswordHashLocked() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("failed to generate an unusable password: %w", err)
	}
	// Base64 so the value bcrypt hashes cannot contain a NUL byte, which
	// some bcrypt implementations treat as a terminator.
	hash, err := bcrypt.GenerateFromPassword(
		[]byte(base64.RawStdEncoding.EncodeToString(secret)), s.bcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash an unusable password: %w", err)
	}
	if len(hash) == 0 {
		return "", fmt.Errorf("refusing to store an empty password hash")
	}
	return string(hash), nil
}

// ReconcileFederatedGroups aligns a federated user's Workbench group
// membership with what the provider asserted, working only through
// opts.GroupMap.
//
// Group names are never matched by equality across the two systems, and a
// group is never created because the provider mentioned one: if a provider
// group could reach a Workbench group directly, anyone able to set their own
// group at the provider would grant themselves whatever that group confers.
// A Workbench group the map does not name is left exactly as it is in both
// directions, so a locally managed group is neither emptied nor joined.
//
// Superuser comes from opts.SuperuserGroup alone, granted while the provider
// asserts it and revoked as soon as it stops; it is never inferred from any
// other claim. When opts.SuperuserGroup is empty the flag is left untouched,
// which is how an operator keeps superuser under local control.
//
// A mapped Workbench group that does not exist is logged and skipped rather
// than failing the login, since a typo in the operator's map should not lock
// everybody out. A disabled account, or one that is not federated, is refused
// outright.
//
// The work runs in three separately committed steps, in this order: the
// superuser revocation on its own, then the membership removals as one
// transaction, then every grant as one transaction. So each step's own writes
// are all-or-nothing, and a failure in any step leaves the steps before it
// committed and the steps after it unapplied. Since every revocation precedes
// every grant, and the superuser revocation precedes the membership removals,
// a failure can only leave the user holding fewer privileges than the provider
// asserts, never more.
//
// The steps are deliberately not bound into one transaction. Rolling a failed
// step back over a revocation that already succeeded would restore exactly the
// privilege being taken away: a user the provider has just demoted would keep
// is_superuser, which every authorisation path reads live and which bypasses
// token scope entirely. Nothing needs the flag and the membership rows to move
// atomically, because both are revocations and a partial revocation errs in
// the safe direction.
func (s *AuthStore) ReconcileFederatedGroups(userID int64, identity FederatedIdentity, opts FederationOptions) error {
	asserted := make(map[string]bool, len(identity.Groups))
	for _, group := range identity.Groups {
		asserted[group] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	username, err := s.federatedAccountForReconcileLocked(userID)
	if err != nil {
		return err
	}
	targets, err := s.mappedGroupTargetsLocked(opts, asserted)
	if err != nil {
		return err
	}
	superuser := opts.SuperuserGroup != "" && asserted[opts.SuperuserGroup]

	// Step one, on its own and first: take superuser away. It is the most
	// consequential privilege here, so nothing that can fail is allowed to
	// stand between the decision and its commit.
	if opts.SuperuserGroup != "" && !superuser {
		if _, err := s.db.Exec("UPDATE users SET is_superuser = FALSE WHERE id = ?", userID); err != nil {
			return fmt.Errorf("revoking superuser for %s: %w", username, err)
		}
	}

	if err := s.revokeFederatedLocked(userID, username, targets); err != nil {
		return err
	}
	return s.grantFederatedLocked(userID, username, targets, opts, superuser)
}

// mappedGroupTarget is one Workbench group named by the operator's map,
// resolved to its ID, with the decision the provider's assertion implies.
type mappedGroupTarget struct {
	name    string
	id      int64
	desired bool
}

// federatedAccountForReconcileLocked checks that userID names an account
// reconciliation may act on, returning its username. s.mu must be held.
func (s *AuthStore) federatedAccountForReconcileLocked(userID int64) (string, error) {
	var username, authSource string
	var enabled bool
	err := s.db.QueryRow("SELECT username, auth_source, enabled FROM users WHERE id = ?", userID).
		Scan(&username, &authSource, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("user not found: %d", userID)
	}
	if err != nil {
		return "", fmt.Errorf("looking up user for reconciliation: %w", err)
	}
	// A disabled account is refused outright. Nothing here would re-enable
	// it, but reconciling one would pre-load the privileges, including
	// is_superuser, that an administrator has just taken the account out of
	// service to contain.
	if !enabled {
		return "", fmt.Errorf("refusing to reconcile groups for disabled account %s", username)
	}
	// Reconciliation is only ever correct for an account the provider owns;
	// refusing anything else keeps this from becoming a way to rewrite a
	// local administrator's groups or superuser flag.
	if authSource != AuthSourceOIDC {
		return "", fmt.Errorf("refusing to reconcile groups for non-federated account %s (auth_source=%s)",
			username, authSource)
	}
	return username, nil
}

// mappedGroupTargetsLocked resolves the operator's map to the Workbench groups
// reconciliation will act on, in a stable order. s.mu must be held.
func (s *AuthStore) mappedGroupTargetsLocked(opts FederationOptions, asserted map[string]bool) ([]mappedGroupTarget, error) {
	// Collapse the map to one decision per Workbench group, so that two
	// provider groups mapped onto the same Workbench group union rather than
	// letting map iteration order decide the outcome.
	desired := make(map[string]bool, len(opts.GroupMap))
	for providerGroup, workbenchGroup := range opts.GroupMap {
		if workbenchGroup == "" {
			continue
		}
		desired[workbenchGroup] = desired[workbenchGroup] || asserted[providerGroup]
	}

	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)

	targets := make([]mappedGroupTarget, 0, len(names))
	for _, name := range names {
		var groupID int64
		err := s.db.QueryRow("SELECT id FROM user_groups WHERE name = ?", name).Scan(&groupID)
		if errors.Is(err, sql.ErrNoRows) {
			//nolint:gosec // G706: name passed through logging.SanitizeForLog
			log.Printf("[AUTH] Federated group map names unknown Workbench group %q; skipping",
				logging.SanitizeForLog(name))
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("looking up mapped group %q: %w", name, err)
		}
		targets = append(targets, mappedGroupTarget{name: name, id: groupID, desired: desired[name]})
	}
	return targets, nil
}

// revokeFederatedLocked removes the memberships the provider no longer
// asserts, as one transaction. It deliberately does not carry the superuser
// revocation: that is committed by the caller beforehand, so a failure here
// cannot roll it back and hand a demoted user their flag again. s.mu must be
// held.
func (s *AuthStore) revokeFederatedLocked(userID int64, username string,
	targets []mappedGroupTarget) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin revocation transaction: %w", err)
	}
	defer func() {
		if err != nil {
			//nolint:errcheck // Rollback error is not critical; the outer
			// error is already being returned to the caller.
			tx.Rollback()
		}
	}()

	for _, target := range targets {
		if target.desired {
			continue
		}
		if _, execErr := tx.Exec(
			"DELETE FROM group_memberships WHERE parent_group_id = ? AND member_user_id = ?",
			target.id, userID); execErr != nil {
			err = fmt.Errorf("removing %s from mapped group %q: %w", username, target.name, execErr)
			return err
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		err = fmt.Errorf("failed to commit federated revocations for %s: %w", username, commitErr)
		return err
	}
	return nil
}

// grantFederatedLocked applies everything that adds privilege, as one
// transaction, once every revocation has been committed. s.mu must be held.
func (s *AuthStore) grantFederatedLocked(userID int64, username string,
	targets []mappedGroupTarget, opts FederationOptions, superuser bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin grant transaction: %w", err)
	}
	defer func() {
		if err != nil {
			//nolint:errcheck // Rollback error is not critical; the outer
			// error is already being returned to the caller.
			tx.Rollback()
		}
	}()

	for _, target := range targets {
		if !target.desired {
			continue
		}
		// UNIQUE(parent_group_id, member_user_id) makes this idempotent.
		if _, execErr := tx.Exec(
			"INSERT OR IGNORE INTO group_memberships (parent_group_id, member_user_id) VALUES (?, ?)",
			target.id, userID); execErr != nil {
			err = fmt.Errorf("adding %s to mapped group %q: %w", username, target.name, execErr)
			return err
		}
	}

	if opts.SuperuserGroup != "" && superuser {
		if _, execErr := tx.Exec("UPDATE users SET is_superuser = TRUE WHERE id = ?", userID); execErr != nil {
			err = fmt.Errorf("granting superuser to %s: %w", username, execErr)
			return err
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		err = fmt.Errorf("failed to commit federated grants for %s: %w", username, commitErr)
		return err
	}
	return nil
}
