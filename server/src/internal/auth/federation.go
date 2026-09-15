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

// =============================================================================
// Operator-Driven Account Linking
// =============================================================================

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
// The returned string is the external subject key that was stored.
func (s *AuthStore) LinkFederatedIdentity(username, issuer, subject string, relink bool) (string, error) {
	if username == "" {
		return "", fmt.Errorf("a username is required")
	}
	if issuer == "" || subject == "" {
		return "", fmt.Errorf("an issuer and a subject are both required")
	}
	key := ExternalSubjectKey(issuer, subject)

	s.mu.Lock()
	defer s.mu.Unlock()

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
	result, err := s.db.Exec(query, args...)
	if err != nil {
		if isUniqueViolation(err, "users.external_subject") {
			holder, lookupErr := s.subjectHolderLocked(key)
			if lookupErr == nil && holder != "" {
				return "", fmt.Errorf("issuer %q, subject %q is already linked to account %s",
					issuer, subject, holder)
			}
		}
		return "", fmt.Errorf("failed to link %s to the identity provider: %w", username, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("failed to confirm the link for %s: %w", username, err)
	}
	if affected == 0 {
		// Either the row already carries this link, in which case there
		// is nothing to do and nobody to log out, or one of the
		// conditions refused it.
		if err := s.explainLinkRefusalLocked(username, key); err != nil {
			return "", err
		}
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
// The returned values are the external subject key that was cleared and the
// number of tokens revoked, which is always zero when restorePassword is set.
func (s *AuthStore) UnlinkFederatedIdentity(username string, restorePassword bool) (string, int, error) {
	if username == "" {
		return "", 0, fmt.Errorf("a username is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var id int64
	var authSource string
	var existing sql.NullString
	err := s.db.QueryRow(
		"SELECT id, auth_source, external_subject FROM users WHERE username = ?",
		username).Scan(&id, &authSource, &existing)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return "", 0, fmt.Errorf("looking up user: %w", err)
	}

	key := ""
	if existing.Valid {
		key = existing.String
	}
	if key == "" && authSource != AuthSourceOIDC {
		return "", 0, fmt.Errorf("account %s is not linked to an identity provider", username)
	}

	// The UPDATE is pinned to the subject that was just read, with IS
	// rather than = so a NULL matches, which is what makes the key reported
	// to the operator and written to the log the key that was actually
	// cleared: if a link commits in between, this matches no row and says
	// so rather than naming a subject it did not clear.
	query := "UPDATE users SET auth_source = ?, external_subject = NULL WHERE id = ? AND external_subject IS ?"
	args := []any{AuthSourceLocal, id, existing}
	if !restorePassword {
		hash, hashErr := s.unusablePasswordHashLocked()
		if hashErr != nil {
			return "", 0, hashErr
		}
		query = "UPDATE users SET auth_source = ?, external_subject = NULL, password_hash = ? " +
			"WHERE id = ? AND external_subject IS ?"
		args = []any{AuthSourceLocal, hash, id, existing}
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return "", 0, fmt.Errorf("failed to unlink %s from the identity provider: %w", username, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", 0, fmt.Errorf("failed to confirm the unlink for %s: %w", username, err)
	}
	if affected == 0 {
		// Which of the two conditions failed decides the message only,
		// never the write, exactly as on the link side.
		return "", 0, s.explainUnlinkRefusalLocked(username)
	}

	// Before the token revocation, because it cannot fail and the failure
	// path below must not leave the worst of the three states behind:
	// password dead, tokens live, sessions live.
	s.InvalidateUserSessions(username)

	revoked := 0
	if !restorePassword {
		revoked, err = s.revokeAccountTokensLocked(username)
		if err != nil {
			// The account is already local and its password already
			// unusable, so this is reported rather than rolled back:
			// the operator has to know the tokens are still live.
			return "", 0, fmt.Errorf(
				"account %s was unlinked and its in-process sessions cleared, but its tokens "+
					"could not be revoked, so they are still valid: %w",
				username, err)
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
// through the same transactional path DeleteUserToken uses, so the per-token
// scope rows and the connection_sessions row keyed on the token hash go with
// them. It returns how many tokens were deleted. s.mu must be held, which is
// why it calls deleteTokensByFilter rather than an exported method that would
// take the lock again.
func (s *AuthStore) revokeAccountTokensLocked(username string) (int, error) {
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
	if err := s.deleteTokensByFilter(ownedByUser, []any{username}, ""); err != nil {
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
