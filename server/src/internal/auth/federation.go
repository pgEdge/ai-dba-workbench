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
	"net/url"
	"sort"
	"strconv"
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
		// The issuer and the subject are spelled out separately, and
		// quoted, because this line is the only place an operator can
		// read them: with provisioning disabled the account has to be
		// linked by hand, and -link-oidc-user takes the two halves as
		// separate arguments. Printing only the packed key would leave
		// the operator to split it themselves.
		return nil, fmt.Errorf(
			"no account for federated identity issuer %q, subject %q (username %q) and provisioning is "+
				"disabled; link an existing account with: ai-dba-server -link-oidc-user -username <name> "+
				"-issuer %q -subject %q",
			identity.Issuer, identity.Subject, identity.Username, identity.Issuer, identity.Subject)
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

	email := usableFederatedEmail(identity.Email, key)

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

	return s.insertFederatedUserLocked(identity, key, hash, email)
}

// insertFederatedUserLocked inserts a provisioned account and records its
// user.create event in the same transaction, so an account whose event cannot
// be written is never created and the login that asked for it fails. That is
// the fail-closed rule every federated grant follows, and creating the account
// is the largest grant a federated login makes. The event is attributed to the
// system actor with the identity provider reason and issuer, as
// reconciliation's events are, and also carries the subject: the account is
// matched on the pair, and the audit log is the record of which identity it
// was created for.
//
// A failed INSERT is rolled back before resolveProvisionInsertFailureLocked
// runs, since adopting a concurrently created row reads it outside this
// transaction; the server that created that row recorded its event. s.mu must
// be held.
func (s *AuthStore) insertFederatedUserLocked(identity FederatedIdentity,
	key, hash, email string) (*StoredUser, error) {

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin provisioning transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			//nolint:errcheck // Rollback error is not critical; the
			// outer error is already being returned.
			tx.Rollback()
		}
	}()

	if _, err := tx.Exec(
		`INSERT INTO users (username, password_hash, display_name, email, enabled, auth_source, external_subject)
         VALUES (?, ?, ?, ?, TRUE, ?, ?)`,
		identity.Username, hash, identity.DisplayName, email, AuthSourceOIDC, key,
	); err != nil {
		//nolint:errcheck // Rollback error is not critical; the insert
		// error decides what happens next.
		tx.Rollback()
		return s.resolveProvisionInsertFailureLocked(err, identity, key)
	}

	// The row is read back by username, which is unique, inside the
	// transaction that inserted it, so it is the row just written.
	user, err := scanUser(tx.QueryRow(
		"SELECT "+federatedUserColumns+" FROM users WHERE username = ?", identity.Username))
	if err != nil {
		return nil, fmt.Errorf("failed to read back provisioned user: %w", err)
	}
	after, err := userSnapshotTx(tx, user.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to read back provisioned user: %w", err)
	}
	if err := s.recordAudit(tx, newEvent(systemActor, "user.create", "user", &after.ID,
		after.Username, federatedAuditDetails(identity.Issuer, map[string]any{
			"subject": identity.Subject,
			"after":   after,
		}))); err != nil {
		return nil, fmt.Errorf("failed to record the provisioned user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit the provisioned user: %w", err)
	}
	committed = true

	// Both values originate with the identity provider, and the subject key
	// embeds the raw "sub" claim, so a hostile provider could otherwise put
	// CR/LF in it and forge whole log lines.
	//nolint:gosec // G706: both values passed through logging.SanitizeForLog
	log.Printf("[AUTH] Provisioned federated user %s for subject %s",
		logging.SanitizeForLog(user.Username), logging.SanitizeForLog(key))
	return user, nil
}

// usableFederatedEmail returns email when the identity provider sent one this
// application can store, and "" otherwise. A malformed email claim is dropped
// rather than failing the login: the address is profile decoration here,
// nothing authenticates or grants access on it, and a sloppy identity provider
// should not lock people out. key names the subject in the log line only.
func usableFederatedEmail(email, key string) string {
	if email == "" {
		return ""
	}
	if err := ValidateEmail(email); err != nil {
		//nolint:gosec // G706: key passed through logging.SanitizeForLog
		log.Printf("[AUTH] Ignoring malformed email claim for federated subject %s: %v",
			logging.SanitizeForLog(key), err)
		return ""
	}
	return email
}

// resolveProvisionInsertFailureLocked decides what a failed provisioning
// INSERT means. s.mu makes provisioning sequential within one process, but two
// servers can share an auth.db, and the unique index on external_subject is
// what actually decides the race: the loser adopts the winner's row rather
// than failing a legitimate login. A username collision that slipped past the
// check the caller made first means the same race created a different account
// under this name, which is refused for the reason that check exists. s.mu
// must be held.
func (s *AuthStore) resolveProvisionInsertFailureLocked(
	insertErr error, identity FederatedIdentity, key string) (*StoredUser, error) {

	if isUniqueViolation(insertErr, "users.external_subject") {
		user, lookupErr := s.lookupFederatedUserLocked(key)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if user != nil {
			return user, nil
		}
	}
	if isUniqueViolation(insertErr, "users.username") {
		return nil, fmt.Errorf("refusing to provision federated identity %s: username %q was taken concurrently",
			key, identity.Username)
	}
	return nil, fmt.Errorf("failed to provision federated user: %w", insertErr)
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
//
// Every change is audited, attributed to the system actor with a
// details.reason of identity_provider and the issuer in details.issuer: a
// superuser change as user.update, and a membership change as
// group.member.add or group.member.remove. A login that changes nothing
// records nothing. The two directions treat a failed audit write
// differently, for the reason the lockout does (see disableForLockout). A
// grant records its events inside its own transaction, so a grant whose event
// cannot be written is rolled back and the login fails. A revocation records
// its events after it has committed and logs a failed write, because rolling
// a revocation back over an audit failure would hand the user the privilege
// the provider has just taken away.
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
		if err := s.revokeFederatedSuperuserLocked(userID, username, identity.Issuer); err != nil {
			return err
		}
	}

	if err := s.revokeFederatedLocked(userID, username, identity.Issuer, targets); err != nil {
		return err
	}
	return s.grantFederatedLocked(userID, username, identity.Issuer, targets, opts, superuser)
}

// federatedAuditReason is the details.reason of every event
// ReconcileFederatedGroups records, as "lockout" is for the lockout's
// user.disable.
const federatedAuditReason = "identity_provider"

// federatedAuditDetails returns the details shared by every event
// ReconcileFederatedGroups records, with extra merged in.
func federatedAuditDetails(issuer string, extra map[string]any) map[string]any {
	details := map[string]any{
		"reason": federatedAuditReason,
		"issuer": issuer,
	}
	for key, value := range extra {
		details[key] = value
	}
	return details
}

// federatedSuperuserEvent builds the user.update event for a superuser change
// made by reconciliation, from the account's snapshot before the change.
func federatedSuperuserEvent(before userSnapshot, issuer string) *AuditEvent {
	after := before
	after.IsSuperuser = !before.IsSuperuser
	return newEvent(systemActor, "user.update", "user", &before.ID, before.Username,
		federatedAuditDetails(issuer, map[string]any{
			"before":           before,
			"after":            after,
			"password_changed": false,
		}))
}

// federatedMembershipEvent builds the group.member.add or group.member.remove
// event for a membership change made by reconciliation.
func federatedMembershipEvent(action string, target mappedGroupTarget,
	userID int64, username, issuer string) *AuditEvent {

	return newEvent(systemActor, action, "group", &target.id, target.name,
		federatedAuditDetails(issuer, membershipDetails(memberTypeUser, userID, username)))
}

// revokeFederatedSuperuserLocked clears is_superuser for a federated user
// the provider no longer places in the superuser group.
//
// This deliberately does not go through guardLastSuperuserTx: the provider
// is authoritative for the flag, and an operator who loses the last
// superuser this way can have the provider grant it to another account, or
// add a local one with -add-user and -set-superuser (ruled on issue #595).
// What it does do is log a warning when the revocation leaves no enabled
// superuser, so that the operator learns of it from the log rather than
// from a refused administrative request. The warning is best effort: a
// failure to read the outcome is logged and does not fail the login,
// because the revocation it describes has already been committed. s.mu
// must be held.
func (s *AuthStore) revokeFederatedSuperuserLocked(userID int64, username, issuer string) error {
	// Matching only a row that holds the flag makes the affected count say
	// whether this login demoted anyone, rather than whether the row
	// exists.
	result, err := s.db.Exec(
		"UPDATE users SET is_superuser = FALSE WHERE id = ? AND is_superuser", userID)
	if err != nil {
		return fmt.Errorf("revoking superuser for %s: %w", username, err)
	}
	// The SQLite driver's RowsAffected cannot fail, and were it to, the
	// only cost would be a missed warning about a committed change.
	if demoted, _ := result.RowsAffected(); demoted == 0 { //nolint:errcheck // see above
		return nil
	}

	s.recordFederatedSuperuserRevocationLocked(username, issuer)

	s.warnIfNoEnabledSuperuserLocked(
		"the identity provider revoked superuser from "+logging.SanitizeForLog(username),
		"grant the superuser group to another account at the provider, "+
			"or add a local superuser with -add-user and -set-superuser")
	return nil
}

// recordFederatedSuperuserRevocationLocked records the user.update event for a
// superuser revocation that has already been committed. The snapshot is read
// afterwards, in the event's own transaction, so that nothing can fail between
// the decision to revoke and its commit; the before state is that snapshot
// with the flag still set. A failure is logged rather than returned, because
// the revocation stands either way. s.mu must be held.
func (s *AuthStore) recordFederatedSuperuserRevocationLocked(username, issuer string) {
	tx, err := s.db.Begin()
	if err == nil {
		var after userSnapshot
		after, err = userSnapshotTx(tx, username)
		if err == nil {
			before := after
			before.IsSuperuser = true
			err = s.recordAudit(tx, federatedSuperuserEvent(before, issuer))
		}
		if err == nil {
			err = tx.Commit()
		} else {
			//nolint:errcheck // Rollback error is not critical; the
			// failure is logged below.
			tx.Rollback()
		}
	}
	if err != nil {
		//nolint:gosec // G706: username passed through logging.SanitizeForLog
		log.Printf("[AUTH] Failed to record the federated superuser revocation for user %s: %v",
			logging.SanitizeForLog(username), err)
	}
}

// recordFederatedRevocationLocked records the events for membership removals
// that have already been committed, in a transaction of its own. A failure is
// logged rather than returned, because the removals stand whether or not
// their events can be written. s.mu must be held.
func (s *AuthStore) recordFederatedRevocationLocked(username string, evs ...*AuditEvent) {
	if err := s.recordAuditInOwnTx(evs...); err != nil {
		//nolint:gosec // G706: username passed through logging.SanitizeForLog
		log.Printf("[AUTH] Failed to record %d federated membership removal audit event(s) for user %s: %v",
			len(evs), logging.SanitizeForLog(username), err)
	}
}

// warnIfNoEnabledSuperuserLocked logs a warning, naming remedy, when no
// enabled superuser remains after change, a committed change that skipped
// guardLastSuperuserTx by ruling on issue #595 (the federated superuser
// revocation and the lockout disable). change describes it in a form safe
// to log, so any username in it must already have been through
// logging.SanitizeForLog. The check is best effort: a failure to count is
// logged rather than returned, because the change it reports on already
// stands. s.mu must be held.
func (s *AuthStore) warnIfNoEnabledSuperuserLocked(change, remedy string) {
	query := "SELECT COUNT(*) FROM users WHERE is_superuser AND enabled"
	if s.superuserCountSQL != "" {
		query = s.superuserCountSQL
	}
	var remaining int
	if err := s.db.QueryRow(query).Scan(&remaining); err != nil {
		//nolint:gosec // G706: callers build change with logging.SanitizeForLog
		log.Printf("[AUTH] %s, but could not count the remaining superusers: %v", change, err)
		return
	}
	if remaining == 0 {
		//nolint:gosec // G706: callers build change with logging.SanitizeForLog
		log.Printf("[AUTH] WARNING: %s, leaving no enabled superuser; %s", change, remedy)
	}
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
// cannot roll it back and hand a demoted user their flag again. A
// group.member.remove event is recorded for each membership actually removed,
// once the removals have committed. s.mu must be held.
func (s *AuthStore) revokeFederatedLocked(userID int64, username, issuer string,
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

	var events []*AuditEvent
	for _, target := range targets {
		if target.desired {
			continue
		}
		result, execErr := tx.Exec(
			"DELETE FROM group_memberships WHERE parent_group_id = ? AND member_user_id = ?",
			target.id, userID)
		if execErr != nil {
			err = fmt.Errorf("removing %s from mapped group %q: %w", username, target.name, execErr)
			return err
		}
		// The SQLite driver's RowsAffected cannot fail; were it to, the
		// only cost would be a missing event for a committed change.
		if removed, _ := result.RowsAffected(); removed > 0 { //nolint:errcheck // see above
			events = append(events, federatedMembershipEvent("group.member.remove",
				target, userID, username, issuer))
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		err = fmt.Errorf("failed to commit federated revocations for %s: %w", username, commitErr)
		return err
	}
	if len(events) > 0 {
		s.recordFederatedRevocationLocked(username, events...)
	}
	return nil
}

// grantFederatedLocked applies everything that adds privilege, as one
// transaction, once every revocation has been committed. Each grant actually
// made records its group.member.add or user.update event in the same
// transaction, so a grant whose event cannot be written is rolled back rather
// than applied unaudited. s.mu must be held.
func (s *AuthStore) grantFederatedLocked(userID int64, username, issuer string,
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
		// UNIQUE(parent_group_id, member_user_id) makes this idempotent,
		// and an ignored row reports no change, so a membership the user
		// already holds records no event.
		result, execErr := tx.Exec(
			"INSERT OR IGNORE INTO group_memberships (parent_group_id, member_user_id) VALUES (?, ?)",
			target.id, userID)
		if execErr != nil {
			err = fmt.Errorf("adding %s to mapped group %q: %w", username, target.name, execErr)
			return err
		}
		// The SQLite driver's RowsAffected cannot fail; were it to, the
		// grant would stand without its event.
		if added, _ := result.RowsAffected(); added == 0 { //nolint:errcheck // see above
			continue
		}
		if auditErr := s.recordAudit(tx, federatedMembershipEvent("group.member.add",
			target, userID, username, issuer)); auditErr != nil {
			err = fmt.Errorf("recording %s joining mapped group %q: %w", username, target.name, auditErr)
			return err
		}
	}

	if opts.SuperuserGroup != "" && superuser {
		if grantErr := s.grantFederatedSuperuserTx(tx, userID, username, issuer); grantErr != nil {
			err = grantErr
			return err
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		err = fmt.Errorf("failed to commit federated grants for %s: %w", username, commitErr)
		return err
	}
	return nil
}

// grantFederatedSuperuserTx sets is_superuser inside the grant transaction and
// records the user.update event alongside it, doing neither when the account
// already holds the flag. s.mu must be held.
func (s *AuthStore) grantFederatedSuperuserTx(tx *sql.Tx, userID int64, username, issuer string) error {
	before, err := userSnapshotTx(tx, username)
	if err != nil {
		return fmt.Errorf("reading %s before granting superuser: %w", username, err)
	}
	if before.IsSuperuser {
		return nil
	}
	if _, err := tx.Exec("UPDATE users SET is_superuser = TRUE WHERE id = ?", userID); err != nil {
		return fmt.Errorf("granting superuser to %s: %w", username, err)
	}
	if err := s.recordAudit(tx, federatedSuperuserEvent(before, issuer)); err != nil {
		return fmt.Errorf("recording the superuser grant to %s: %w", username, err)
	}
	return nil
}

// =============================================================================
// Operator-Driven Account Linking
// =============================================================================

// ErrPartialUnlink marks the one outcome of UnlinkFederatedIdentity in which
// the account has been changed but not everything the command promises has
// been applied: the account is local again with an unusable password, and its
// tokens are still valid. A caller matches on it to tell the operator what is
// still live, rather than matching on the error's text.
var ErrPartialUnlink = errors.New("account was unlinked but not fully secured")

// parseExternalSubjectKey reads an ExternalSubjectKey back into the issuer and
// subject it was built from. The encoding is injective precisely so that this
// is possible, and the round trip matters here because an operator reading a
// message about an existing link needs the two halves, not the packed form.
// A key this cannot parse is reported as such rather than guessed at.
func parseExternalSubjectKey(key string) (issuer, subject string, ok bool) {
	first := strings.IndexByte(key, '|')
	if first <= 0 {
		return "", "", false
	}
	length, err := strconv.Atoi(key[:first])
	if err != nil || length < 0 {
		return "", "", false
	}
	rest := key[first+1:]
	// The issuer is followed by the separator, so the remainder must be at
	// least one byte longer than the issuer itself.
	if len(rest) <= length || rest[length] != '|' {
		return "", "", false
	}
	return rest[:length], rest[length+1:], true
}

// IssuerFromExternalSubject returns the issuer half of a stored
// users.external_subject key, so that an administrator reading a user list
// can tell which identity provider owns a federated account.
//
// The subject half is deliberately not returned. It is a long opaque string
// that means nothing to a human reading a list, and it is an identifier there
// is no reason to spread through API responses and CLI tables when nothing
// reading them needs it; the few callers that genuinely need both halves, such
// as the messages -link-oidc-user prints, build them from the parts they
// already hold.
//
// An empty key, or one that does not parse, yields "" rather than a guess, so
// that a caller can fall back to a generic label.
func IssuerFromExternalSubject(key string) string {
	issuer, _, ok := parseExternalSubjectKey(key)
	if !ok {
		return ""
	}
	return issuer
}

// describeSubjectKey renders a stored external subject for an operator,
// falling back to the raw key when it does not parse.
func describeSubjectKey(key string) string {
	issuer, subject, ok := parseExternalSubjectKey(key)
	if !ok {
		return fmt.Sprintf("external subject %q", key)
	}
	return fmt.Sprintf("issuer %q, subject %q", issuer, subject)
}

// LinkFederatedIdentity attaches a provider identity to an account that
// already exists, which is the only way a federated login can succeed while
// provisioning is disabled: nothing else in the server writes
// users.external_subject.
//
// The account is stamped exactly as a provisioned one is, with
// external_subject set to ExternalSubjectKey(issuer, subject) and auth_source
// set to AuthSourceOIDC, so resolution, group reconciliation and every later
// check treat a linked account and a provisioned account identically. One
// consequence is worth stating plainly: from the next federated login onward
// the provider owns this account's group membership and, when a superuser
// group is configured, its is_superuser flag, so linking a local administrator
// hands that privilege to the provider and revokes it if the provider does not
// assert the configured group.
//
// Linking deliberately leaves password_hash alone, since the account stops
// being reachable by password anyway: AuthenticateUser refuses any auth_source
// other than local. UnlinkFederatedIdentity is where that decision is
// revisited, because putting auth_source back is what would make the old hash
// live again.
//
// This process's live sessions for the account are invalidated, as they are
// for every other credential-lifecycle change in this store: ValidateSessionToken
// re-reads only enabled, never auth_source, so a session minted before the link
// would otherwise outlive it by up to the session expiry. Note the scope: the
// session map is per-process and in memory, so a caller in another process, the
// CLI in particular, invalidates nothing the running server holds. Only the
// enabled flag, which ValidateSessionToken does re-read from the database, ends
// another process's sessions.
//
// An account already linked to a different subject is refused unless relink is
// true. Moving an identity silently from one account to another is how a
// person ends up logged in as somebody else, so it has to be deliberate. That
// guard, the service-account refusal and the auth_source check are all
// conditions on the UPDATE rather than a SELECT that ran beforehand, so two
// concurrent invocations cannot slip an unflagged move between the check and
// the write.
//
// A link that changes the row records a user.link_identity event in the same
// transaction, so a link whose event cannot be written is not applied; a
// refused link records a failure event, and the idempotent re-link records
// nothing. This variant attributes the event to the system actor; the CLI goes
// through ActorStore.LinkFederatedIdentity so it names the operator.
//
// The returned string is the external subject key that was stored.
func (s *AuthStore) LinkFederatedIdentity(username, issuer, subject string, relink bool) (string, error) {
	return s.linkFederatedIdentity(systemActor, username, issuer, subject, relink)
}

// LinkFederatedIdentity attaches a provider identity to an existing account,
// attributing the change to this store's actor. See
// AuthStore.LinkFederatedIdentity.
func (a *ActorStore) LinkFederatedIdentity(username, issuer, subject string, relink bool) (string, error) {
	return a.s.linkFederatedIdentity(a.actor, username, issuer, subject, relink)
}

// Action names for the account changes LinkFederatedIdentity and
// UnlinkFederatedIdentity make. They are verbs of their own rather than
// user.update because the user.update snapshot carries neither auth_source
// nor external_subject, and handing an account to the provider or taking it
// back is a change of who controls the account that an auditor needs to find
// by name, as user.set_superuser is.
const (
	actionLinkIdentity   = "user.link_identity"
	actionUnlinkIdentity = "user.unlink_identity"
)

func (s *AuthStore) linkFederatedIdentity(actor Actor, username, issuer, subject string,
	relink bool) (string, error) {

	if username == "" {
		return "", fmt.Errorf("a username is required")
	}
	if issuer == "" || subject == "" {
		return "", fmt.Errorf("an issuer and a subject are both required")
	}
	// The configured issuer must be an https:// URL with a host (see the
	// OIDC checks in config.validateConfig), and a federated login only ever
	// looks up the issuer it was configured with, so a link to anything
	// else would create an account that no login can reach. Refusing it
	// here turns a mistyped -issuer into an error instead.
	//
	// An issuer never carries credentials either, and one that did would
	// be stored in external_subject and later reported as auth_issuer, so
	// userinfo is refused first, whatever the scheme. Neither error echoes
	// the value: a string that does not even parse can still hold a
	// password, and quoting it would put that on the terminal or in a log.
	issuerURL, err := url.Parse(issuer)
	if err == nil && issuerURL.User != nil {
		return "", fmt.Errorf("issuer must not contain user credentials")
	}
	if err != nil || issuerURL.Scheme != "https" || issuerURL.Host == "" {
		return "", fmt.Errorf("issuer is not a valid https:// URL")
	}
	key := ExternalSubjectKey(issuer, subject)

	s.mu.Lock()
	defer s.mu.Unlock()

	changed, err := s.applyLinkLocked(actor, username, issuer, subject, relink)
	if err != nil {
		return "", err
	}
	if !changed {
		//nolint:gosec // G706: both values passed through logging.SanitizeForLog
		log.Printf("[AUTH] Account %s is already linked to federated subject %s; nothing to do",
			logging.SanitizeForLog(username), logging.SanitizeForLog(key))
		return key, nil
	}

	s.InvalidateUserSessions(username)

	// The subject originates with the identity provider by way of the
	// operator's command line, so it is sanitized for the same reason
	// provisioning sanitizes it.
	//nolint:gosec // G706: both values passed through logging.SanitizeForLog
	log.Printf("[AUTH] Linked account %s to federated subject %s",
		logging.SanitizeForLog(username), logging.SanitizeForLog(key))
	return key, nil
}

// federatedLinkState is the part of a user row that linking and unlinking
// change, read inside the transaction that changes it.
type federatedLinkState struct {
	id              int64
	authSource      string
	externalSubject sql.NullString
}

// readFederatedLinkState reads username's federatedLinkState, returning
// sql.ErrNoRows unwrapped when there is no such account.
func readFederatedLinkState(q rowQuerier, username string) (federatedLinkState, error) {
	var state federatedLinkState
	err := q.QueryRow(
		"SELECT id, auth_source, external_subject FROM users WHERE username = ?",
		username).Scan(&state.id, &state.authSource, &state.externalSubject)
	return state, err
}

// auditView renders the state for an audit event. external_subject is the
// stored key, or null for an account no provider holds.
func (st federatedLinkState) auditView() map[string]any {
	view := map[string]any{"auth_source": st.authSource, "external_subject": nil}
	if st.externalSubject.Valid {
		view["external_subject"] = st.externalSubject.String
	}
	return view
}

// applyLinkLocked writes the link and its user.link_identity event in one
// transaction, reporting whether the row changed. An UPDATE that matched no
// row is either the idempotent re-link, which returns false and no error, or
// a refusal, which explainLinkRefusalLocked names and which is recorded as a
// failure event. s.mu must be held.
func (s *AuthStore) applyLinkLocked(actor Actor, username, issuer, subject string,
	relink bool) (changed bool, err error) {

	key := ExternalSubjectKey(issuer, subject)

	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	target := &auditTarget{action: actionLinkIdentity, targetType: "user", targetName: username}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	// The transaction begins IMMEDIATE (see NewAuthStore), so this read
	// and the UPDATE below see the same row; it supplies the event's before
	// state, while the UPDATE's own conditions still decide the write.
	// Each error return below assigns the named err, which is what the
	// deferred failAudit reads.
	before, err := readFederatedLinkState(tx, username)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return false, fmt.Errorf("looking up user: %w", err)
	}
	target.targetID = &before.id

	// Sessions are not for service accounts: CreateSessionForUser refuses
	// one outright, so a linked service account could never complete a
	// login and the link would be a trap rather than a configuration. The
	// auth_source condition is the converse of the belt-and-braces check
	// lookupFederatedUserLocked makes, and keeps this from quietly
	// converting an account whose identity some future source owns.
	//
	// The last condition excludes a row that already says exactly what this
	// UPDATE would say. SQLite's changes() counts matched rows rather than
	// modified ones, so without it the documented idempotent re-link would
	// report a row affected and log the user out; a configuration-management
	// run that reasserts links every pass would then log every federated
	// user out every pass. IS NOT is used rather than <> because
	// external_subject is nullable.
	query := `UPDATE users SET auth_source = ?, external_subject = ?
        WHERE username = ? AND is_service_account = FALSE AND auth_source IN (?, ?)
          AND (external_subject IS NOT ? OR auth_source <> ?)`
	args := []any{AuthSourceOIDC, key, username, AuthSourceLocal, AuthSourceOIDC, key, AuthSourceOIDC}
	if !relink {
		query += " AND (external_subject IS NULL OR external_subject = ?)"
		args = append(args, key)
	}

	// No pre-flight check that some other account already holds this
	// subject: the partial unique index on external_subject is what
	// actually decides that, including against a second server sharing the
	// auth.db, so the collision is read off the constraint rather than off
	// a racy SELECT that ran beforehand. The account holding the subject is
	// then looked up so the operator is told who it is, instead of being
	// handed a raw SQLite constraint error.
	//
	// Both lookups below read outside the transaction, so it is rolled
	// back first; failAudit's own rollback is then a no-op.
	result, err := tx.Exec(query, args...)
	if err != nil {
		//nolint:errcheck // Rollback error is not critical; see above.
		tx.Rollback()
		if isUniqueViolation(err, "users.external_subject") {
			holder, lookupErr := s.subjectHolderLocked(key)
			if lookupErr == nil && holder != "" {
				return false, fmt.Errorf("issuer %q, subject %q is already linked to account %s",
					issuer, subject, holder)
			}
		}
		return false, fmt.Errorf("failed to link %s to the identity provider: %w", username, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to confirm the link for %s: %w", username, err)
	}
	if affected == 0 {
		// Either the row already carries this link, in which case there
		// is nothing to do and nobody to log out, or one of the
		// conditions refused it.
		//nolint:errcheck // Rollback error is not critical; see above.
		tx.Rollback()
		return false, s.explainLinkRefusalLocked(username, key)
	}

	after := federatedLinkState{
		id:              before.id,
		authSource:      AuthSourceOIDC,
		externalSubject: sql.NullString{String: key, Valid: true},
	}
	err = s.recordAudit(tx, newEvent(actor, actionLinkIdentity, "user", &before.id,
		username, map[string]any{
			"issuer":  issuer,
			"subject": subject,
			"relink":  relink,
			"before":  before.auditView(),
			"after":   after.auditView(),
		}))
	if err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit the link for %s: %w", username, err)
	}
	return true, nil
}

// explainLinkRefusalLocked turns an UPDATE that matched no row into the reason
// it matched none, by reading back the row the conditions were about. It
// returns nil when the row already carries the link the UPDATE would have
// written, which is not a refusal but the idempotent case. Every answer it
// gives is a snapshot taken after the fact, so it decides the message and the
// no-op, never whether the write should have happened. s.mu must be held.
func (s *AuthStore) explainLinkRefusalLocked(username, key string) error {
	var authSource string
	var isServiceAccount bool
	var existing sql.NullString
	err := s.db.QueryRow(
		"SELECT auth_source, is_service_account, external_subject FROM users WHERE username = ?",
		username).Scan(&authSource, &isServiceAccount, &existing)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return fmt.Errorf("looking up user: %w", err)
	}
	// Both refusals sit above the idempotent short-circuit deliberately.
	// Neither state is reachable through the shipped commands today, since
	// nothing stamps a subject onto a service account or onto an account
	// another source owns, but a refusal that sits downstream of a success
	// short-circuit is one command away from being skipped: anything that
	// later converted an account type would turn that combination into a
	// silent success.
	if isServiceAccount {
		return fmt.Errorf("service account cannot be linked to an identity provider: %s", username)
	}
	if authSource != AuthSourceLocal && authSource != AuthSourceOIDC {
		return fmt.Errorf("account %s has an identity this command does not manage (auth_source=%s)",
			username, authSource)
	}
	// The row already says exactly what the UPDATE would have said, so
	// there is nothing to do and nobody to log out.
	if authSource == AuthSourceOIDC && existing.Valid && existing.String == key {
		return nil
	}
	if existing.Valid && existing.String != "" && existing.String != key {
		return fmt.Errorf(
			"account %s is already linked to a different identity (%s); pass -relink to move it",
			username, describeSubjectKey(existing.String))
	}
	// The row changed under us between the UPDATE and this read, which is
	// as much as can honestly be said about it.
	return fmt.Errorf("account %s was not linked; it changed while the link was being applied", username)
}

// UnlinkFederatedIdentity detaches an account from its provider identity,
// clearing external_subject and returning auth_source to local. It is the
// inverse of LinkFederatedIdentity, for when a person leaves or the provider
// reissues their subject.
//
// Returning auth_source to local would make the account's stored password_hash
// live again, because AuthenticateUser only refuses a non-local auth_source and
// never looks at external_subject: the password the account had before it was
// linked, unrotated for the whole federated period, would start working again
// at the moment of unlinking. So by default this writes an unusable hash at the
// same time, exactly as provisionFederatedUserLocked does, and deletes every
// API token the account owns, because ValidateToken checks only expiry and the
// owner's enabled flag, so a token minted whilst the account was federated
// would otherwise keep authenticating at that account's full privileges, for
// ever in the case of a token minted without an expiry. The account leaves
// federation reachable by nothing until an operator sets a password with
// -update-user.
//
// Passing restorePassword makes this the other command: the account is
// converted back to local login with the password and the tokens it already
// had, which is only correct when the operator means to hand it back to the
// person who holds it.
//
// This process's live sessions for the account are invalidated either way,
// with the same per-process caveat as in LinkFederatedIdentity: a call from the
// CLI cannot end a session the running server is holding.
//
// The account change records a user.unlink_identity event in the same
// transaction, so an unlink whose event cannot be written is not applied, and
// a refused unlink records a failure event. The token deletions that follow
// record their own token.delete events, attributed to the same actor. This
// variant attributes everything to the system actor; the CLI goes through
// ActorStore.UnlinkFederatedIdentity so it names the operator.
//
// The returned values are the external subject key that was cleared and the
// number of tokens revoked, which is always zero when restorePassword is set.
func (s *AuthStore) UnlinkFederatedIdentity(username string, restorePassword bool) (string, int, error) {
	return s.unlinkFederatedIdentity(systemActor, username, restorePassword)
}

// UnlinkFederatedIdentity detaches an account from its provider identity,
// attributing the change and its token deletions to this store's actor. See
// AuthStore.UnlinkFederatedIdentity.
func (a *ActorStore) UnlinkFederatedIdentity(username string, restorePassword bool) (string, int, error) {
	return a.s.unlinkFederatedIdentity(a.actor, username, restorePassword)
}

func (s *AuthStore) unlinkFederatedIdentity(actor Actor, username string,
	restorePassword bool) (string, int, error) {

	if username == "" {
		return "", 0, fmt.Errorf("a username is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	before, err := s.federatedAccountForUnlinkLocked(username)
	if err != nil {
		var targetID *int64
		if before.id != 0 {
			targetID = &before.id
		}
		s.recordFailure(actor, actionUnlinkIdentity, "user", targetID, username, err)
		return "", 0, err
	}
	key := before.externalSubject.String

	if err := s.applyUnlinkLocked(actor, username, before, restorePassword); err != nil {
		return "", 0, err
	}

	// Before the token revocation, because it cannot fail and the failure
	// path below must not leave the worst of the three states behind:
	// password dead, tokens live, sessions live.
	s.InvalidateUserSessions(username)

	revoked := 0
	if !restorePassword {
		revoked, err = s.revokeAccountTokensLocked(actor, username)
		if err != nil {
			// The account is already local and its password already
			// unusable, so this is reported rather than rolled back:
			// the operator has to know the tokens are still live.
			// No claim about sessions here: from the CLI this
			// process has none, and the caller's caveat is what
			// tells the operator the truth about them. The error
			// wraps ErrPartialUnlink so a caller can detect the
			// half-applied case without reading its text.
			return "", 0, fmt.Errorf(
				"%w: account %s was unlinked but its tokens could not be revoked, so they are "+
					"still valid: %w",
				ErrPartialUnlink, username, err)
		}
	}

	reachable := fmt.Sprintf("no password login is possible until one is set, %d token(s) revoked", revoked)
	if restorePassword {
		reachable = "its previous password and tokens still work"
	}
	//nolint:gosec // G706: both values passed through logging.SanitizeForLog
	log.Printf("[AUTH] Unlinked account %s from federated subject %s; %s",
		logging.SanitizeForLog(username), logging.SanitizeForLog(key), reachable)
	return key, revoked, nil
}

// federatedAccountForUnlinkLocked reads the account an unlink is about and
// refuses the ones that have no federated identity to clear. It returns the
// row as it was read, which the UPDATE pins itself to and the event reports as
// the before state; on a refusal of an account that exists, the row is
// returned too, so the failure event can name it. s.mu must be held.
func (s *AuthStore) federatedAccountForUnlinkLocked(username string) (federatedLinkState, error) {
	state, err := readFederatedLinkState(s.db, username)
	if errors.Is(err, sql.ErrNoRows) {
		return federatedLinkState{}, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return federatedLinkState{}, fmt.Errorf("looking up user: %w", err)
	}

	if state.externalSubject.String == "" && state.authSource != AuthSourceOIDC {
		return state, fmt.Errorf(
			"account %s is not linked to an identity provider", username)
	}
	return state, nil
}

// applyUnlinkLocked writes the unlink itself: auth_source back to local,
// external_subject cleared, and unless restorePassword is set, an unusable
// password hash written at the same time so the account's pre-federation
// password cannot come back to life.
//
// The UPDATE is pinned to the subject that was just read, with IS rather than
// = so a NULL matches, which is what makes the key reported to the operator
// and written to the log the key that was actually cleared: if a link commits
// in between, this matches no row and says so rather than naming a subject it
// did not clear.
//
// The UPDATE and its user.unlink_identity event share one transaction, so an
// unlink whose event cannot be written is rolled back and reported, and a
// refused one records a failure event. The unusable hash is computed before
// the transaction begins, because the transaction holds SQLite's write lock
// and bcrypt is deliberately slow. s.mu must be held.
func (s *AuthStore) applyUnlinkLocked(actor Actor, username string,
	before federatedLinkState, restorePassword bool) (err error) {

	existing := before.externalSubject
	query := "UPDATE users SET auth_source = ?, external_subject = NULL WHERE id = ? AND external_subject IS ?"
	args := []any{AuthSourceLocal, before.id, existing}
	if !restorePassword {
		hash, hashErr := s.unusablePasswordHashLocked()
		if hashErr != nil {
			return hashErr
		}
		query = "UPDATE users SET auth_source = ?, external_subject = NULL, password_hash = ? " +
			"WHERE id = ? AND external_subject IS ?"
		args = []any{AuthSourceLocal, hash, before.id, existing}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	target := &auditTarget{action: actionUnlinkIdentity, targetType: "user",
		targetID: &before.id, targetName: username}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	// Each error return below assigns the named err, which is what the
	// deferred failAudit reads.
	result, err := tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to unlink %s from the identity provider: %w", username, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to confirm the unlink for %s: %w", username, err)
	}
	if affected == 0 {
		// Which of the two conditions failed decides the message only,
		// never the write, exactly as on the link side. The explanation
		// reads outside the transaction, so it is rolled back first.
		//nolint:errcheck // Rollback error is not critical; failAudit
		// rolls back again regardless.
		tx.Rollback()
		return s.explainUnlinkRefusalLocked(username)
	}

	err = s.recordAudit(tx, newEvent(actor, actionUnlinkIdentity, "user", &before.id,
		username, unlinkAuditDetails(before, restorePassword)))
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit the unlink for %s: %w", username, err)
	}
	return nil
}

// unlinkAuditDetails builds the details of a user.unlink_identity event. The
// issuer and subject are split out of the cleared key when it parses, as the
// link event carries them, and password_changed says whether the unlink wrote
// an unusable hash, with the meaning user.update gives it.
func unlinkAuditDetails(before federatedLinkState, restorePassword bool) map[string]any {
	after := federatedLinkState{id: before.id, authSource: AuthSourceLocal}
	details := map[string]any{
		"password_changed": !restorePassword,
		"before":           before.auditView(),
		"after":            after.auditView(),
	}
	if issuer, subject, ok := parseExternalSubjectKey(before.externalSubject.String); ok {
		details["issuer"] = issuer
		details["subject"] = subject
	}
	return details
}

// explainUnlinkRefusalLocked says why an unlink UPDATE matched no row: either
// the account has gone since it was read, or its federated identity moved
// under us. s.mu must be held.
func (s *AuthStore) explainUnlinkRefusalLocked(username string) error {
	var present int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&present)
	if err != nil {
		return fmt.Errorf("looking up user: %w", err)
	}
	if present == 0 {
		return fmt.Errorf("user not found: %s", username)
	}
	return fmt.Errorf(
		"account %s was not unlinked; its federated identity changed while the unlink was being applied",
		username)
}

// revokeAccountTokensLocked deletes every API token the named account owns,
// through the same transactional path DeleteToken uses, so the per-token
// scope rows and the connection_sessions row keyed on the token hash go with
// them. It returns how many tokens were deleted. s.mu must be held, which is
// why it calls deleteTokensByFilter rather than an exported method that would
// take the lock again.
func (s *AuthStore) revokeAccountTokensLocked(actor Actor, username string) (int, error) {
	const ownedByUser = "owner_id = (SELECT id FROM users WHERE username = ?)"

	var count int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM tokens WHERE "+ownedByUser, username).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting the account's tokens: %w", err)
	}
	// deleteTokensByFilter treats an empty match set as an error, which is
	// right for deleting one named token and wrong here, where an account
	// with no tokens is the ordinary case.
	if count == 0 {
		return 0, nil
	}
	// The revocation is a consequence of the unlink, so its token.delete
	// events are attributed to whoever asked for the unlink: the operator,
	// when it comes from the command line.
	if err := s.deleteTokensByFilter(actor, ownedByUser, []any{username},
		true); err != nil {
		return 0, err
	}
	return count, nil
}

// subjectHolderLocked returns the username of the account holding key, or ""
// when no account holds it. s.mu must be held.
func (s *AuthStore) subjectHolderLocked(key string) (string, error) {
	var holder string
	err := s.db.QueryRow(
		"SELECT username FROM users WHERE external_subject = ?", key).Scan(&holder)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("looking up the account holding a federated subject: %w", err)
	}
	return holder, nil
}
