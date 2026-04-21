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
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const (
	// bcryptCost is the cost factor for bcrypt password hashing.
	bcryptCost = 12

	// sessionTokenBytes is the size of generated session tokens in bytes.
	sessionTokenBytes = 32

	// maxSessionsPerUser is the maximum number of concurrent sessions
	// allowed per user. When this limit is reached, the oldest session
	// is evicted to make room for the new one.
	maxSessionsPerUser = 10
)

// dummyHash is a pre-computed bcrypt hash used during authentication
// to prevent timing side-channel attacks that could reveal whether a
// username exists. When a lookup returns no rows, we compare against
// this hash so the response time is consistent with a real comparison.
//
//nolint:errcheck // bcrypt.GenerateFromPassword with a valid cost never fails
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-timing-equalizer"), bcryptCost)

const (
	// DefaultSessionExpiry is the default duration for session tokens
	DefaultSessionExpiry = 24 * time.Hour

	// Schema version for migrations
	schemaVersion = 3
)

// AuthStore manages users and tokens in SQLite
type AuthStore struct {
	db                 *sql.DB
	mu                 sync.RWMutex
	path               string
	maxUserTokenDays   int           // Maximum lifetime for user-created tokens (0 = unlimited)
	maxFailedAttempts  int           // Max failed login attempts before lockout (0 = disabled)
	sessions           sync.Map      // In-memory session store: token -> SessionInfo
	sessionCleanupStop chan struct{} // Signals the session cleanup goroutine to stop
}

// SessionInfo holds session information (in-memory only)
type SessionInfo struct {
	Username  string
	ExpiresAt time.Time
}

// ConnectionSession represents a selected database connection for a token
type ConnectionSession struct {
	TokenHash    string  // The token hash this session belongs to
	ConnectionID int     // The selected connection ID from the datastore
	DatabaseName *string // The selected database name (nil = use connection default)
}

// StoredUser represents a user in the database
type StoredUser struct {
	ID               int64
	Username         string
	PasswordHash     string
	CreatedAt        time.Time
	LastLogin        *time.Time
	Enabled          bool
	Annotation       string
	DisplayName      string
	Email            string
	FailedAttempts   int
	IsSuperuser      bool
	IsServiceAccount bool
}

// StoredToken represents a token in the database
type StoredToken struct {
	ID         int64
	TokenHash  string     // SHA256 hash of the actual token
	OwnerID    int64      // User ID of the token owner
	ExpiresAt  *time.Time // NULL for never expires
	Annotation string
	CreatedAt  time.Time
	Database   string // Bound database name (empty = first configured database)
}

// NewAuthStore creates a new SQLite-based auth store
func NewAuthStore(dataDir string, maxUserTokenDays, maxFailedAttempts int) (*AuthStore, error) {
	// Create data directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "auth.db")
	// Enable foreign key enforcement via DSN pragma. SQLite ships with
	// foreign keys disabled by default for historical compatibility
	// reasons, so every ON DELETE CASCADE declared in the schema is a
	// silent no-op until this pragma is set. The modernc.org/sqlite
	// driver applies each "_pragma=NAME(VALUE)" parameter as a PRAGMA
	// statement on every new connection in the pool, so the setting is
	// applied consistently regardless of connection churn.
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open auth database: %w", err)
	}

	// Restrict database file permissions to owner-only (0600) so that
	// other users on the system cannot read password hashes or tokens.
	if chmodErr := os.Chmod(dbPath, 0600); chmodErr != nil {
		log.Printf("[AUTH] WARNING: Failed to set permissions on %s: %v", dbPath, chmodErr)
	}

	// Verify that the foreign_keys pragma is actually active. SQLite
	// silently ignores the pragma if it was compiled without foreign key
	// support (SQLITE_OMIT_FOREIGN_KEY) or if the DSN parameter was not
	// parsed. We want to fail loudly in that case because several
	// delete paths in this package rely on ON DELETE CASCADE for
	// defense in depth and a future regression would silently leave
	// orphan rows behind.
	var fkEnabled int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fkEnabled); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to verify foreign_keys pragma: %w", err)
	}
	if fkEnabled != 1 {
		db.Close()
		return nil, fmt.Errorf("foreign_keys pragma is not active (value=%d); SQLite build must support foreign keys", fkEnabled)
	}

	store := &AuthStore{
		db:                db,
		path:              dbPath,
		maxUserTokenDays:  maxUserTokenDays,
		maxFailedAttempts: maxFailedAttempts,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// migrateV1ToV2 adds the is_public column to mcp_privilege_identifiers table
func (s *AuthStore) migrateV1ToV2() error {
	// Add is_public column to mcp_privilege_identifiers
	_, err := s.db.Exec(`
		ALTER TABLE mcp_privilege_identifiers ADD COLUMN is_public BOOLEAN NOT NULL DEFAULT FALSE
	`)
	if err != nil {
		// Column might already exist if migration was partially completed
		if !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("failed to add is_public column: %w", err)
		}
	}

	// Update schema version
	_, err = s.db.Exec("DELETE FROM schema_version")
	if err != nil {
		return fmt.Errorf("failed to clear schema version: %w", err)
	}
	_, err = s.db.Exec("INSERT INTO schema_version (version) VALUES (?)", 2)
	if err != nil {
		return fmt.Errorf("failed to set schema version: %w", err)
	}

	return nil
}

// migrateV2ToV3 rebuilds group_mcp_privileges and token_mcp_scope without
// the FOREIGN KEY constraint on privilege_identifier_id. The FK was
// incompatible with the wildcard sentinel (privilege_identifier_id = 0)
// that represents "all MCP privileges"; once PRAGMA foreign_keys = ON is
// enabled every wildcard INSERT would fail. We rebuild the tables in a
// transaction so the upgrade is atomic.
//
// The FK target table (mcp_privilege_identifiers) is never deleted from
// in this codebase, so removing the CASCADE behavior changes nothing in
// practice.
func (s *AuthStore) migrateV2ToV3() error {
	// Temporarily disable foreign_keys for the duration of the migration
	// so the ALTER-via-rebuild pattern below does not trip on any
	// transient inconsistencies. The DSN sets foreign_keys(1) on every
	// new connection, so the effect is scoped to this migration.
	if _, err := s.db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("failed to disable foreign_keys during migration: %w", err)
	}
	defer func() {
		//nolint:errcheck // Best effort; the DSN pragma re-enables FKs
		// on every new connection anyway, so a failure here only
		// affects the current connection's residual state.
		s.db.Exec("PRAGMA foreign_keys = ON")
	}()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin migration transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			//nolint:errcheck // Rollback error is not critical; the
			// outer error is already being returned.
			tx.Rollback()
		}
	}()

	// Rebuild group_mcp_privileges without the FK on privilege_identifier_id.
	rebuildGroupMCP := []string{
		`CREATE TABLE group_mcp_privileges_new (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            group_id INTEGER NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
            privilege_identifier_id INTEGER NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(group_id, privilege_identifier_id)
        )`,
		`INSERT INTO group_mcp_privileges_new (id, group_id, privilege_identifier_id, created_at)
         SELECT id, group_id, privilege_identifier_id, created_at
         FROM group_mcp_privileges`,
		`DROP TABLE group_mcp_privileges`,
		`ALTER TABLE group_mcp_privileges_new RENAME TO group_mcp_privileges`,
		`CREATE INDEX IF NOT EXISTS idx_group_mcp_privs_group ON group_mcp_privileges(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_group_mcp_privs_priv ON group_mcp_privileges(privilege_identifier_id)`,
	}
	for _, stmt := range rebuildGroupMCP {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("failed to rebuild group_mcp_privileges: %w", err)
		}
	}

	// Rebuild token_mcp_scope without the FK on privilege_identifier_id.
	rebuildTokenMCP := []string{
		`CREATE TABLE token_mcp_scope_new (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            token_id INTEGER NOT NULL REFERENCES tokens(id) ON DELETE CASCADE,
            privilege_identifier_id INTEGER NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(token_id, privilege_identifier_id)
        )`,
		`INSERT INTO token_mcp_scope_new (id, token_id, privilege_identifier_id, created_at)
         SELECT id, token_id, privilege_identifier_id, created_at
         FROM token_mcp_scope`,
		`DROP TABLE token_mcp_scope`,
		`ALTER TABLE token_mcp_scope_new RENAME TO token_mcp_scope`,
		`CREATE INDEX IF NOT EXISTS idx_token_mcp_scope_token ON token_mcp_scope(token_id)`,
	}
	for _, stmt := range rebuildTokenMCP {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("failed to rebuild token_mcp_scope: %w", err)
		}
	}

	// Bump schema version inside the same transaction.
	if _, err := tx.Exec("DELETE FROM schema_version"); err != nil {
		return fmt.Errorf("failed to clear schema version: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO schema_version (version) VALUES (?)", 3); err != nil {
		return fmt.Errorf("failed to set schema version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration: %w", err)
	}
	committed = true

	return nil
}

// initSchema creates the database tables if they don't exist
func (s *AuthStore) initSchema() error {
	// Check current schema version
	var currentVersion int
	err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&currentVersion)
	if err != nil {
		// Table might not exist yet, that's fine
		currentVersion = 0
	}

	// Run migrations for existing databases
	if currentVersion == 1 {
		if err := s.migrateV1ToV2(); err != nil {
			return fmt.Errorf("failed to migrate from v1 to v2: %w", err)
		}
		currentVersion = 2
	}
	if currentVersion == 2 {
		if err := s.migrateV2ToV3(); err != nil {
			return fmt.Errorf("failed to migrate from v2 to v3: %w", err)
		}
		currentVersion = 3
	}

	if currentVersion < schemaVersion {
		schema := `
    -- Schema version tracking
    CREATE TABLE IF NOT EXISTS schema_version (
        version INTEGER PRIMARY KEY
    );

    -- Users table
    CREATE TABLE IF NOT EXISTS users (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        username TEXT UNIQUE NOT NULL,
        password_hash TEXT NOT NULL,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        last_login TIMESTAMP,
        enabled BOOLEAN DEFAULT TRUE,
        annotation TEXT DEFAULT '',
        failed_attempts INTEGER DEFAULT 0,
        is_superuser BOOLEAN DEFAULT FALSE,
        display_name TEXT DEFAULT '',
        email TEXT DEFAULT '',
        is_service_account BOOLEAN DEFAULT FALSE
    );
    CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

    -- Tokens table
    CREATE TABLE IF NOT EXISTS tokens (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        token_hash TEXT UNIQUE NOT NULL,
        owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
        expires_at TIMESTAMP,
        annotation TEXT DEFAULT '',
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        database TEXT DEFAULT ''
    );
    CREATE INDEX IF NOT EXISTS idx_tokens_hash ON tokens(token_hash);
    CREATE INDEX IF NOT EXISTS idx_tokens_owner ON tokens(owner_id);

    -- Connection sessions table (tracks selected database connection per token)
    CREATE TABLE IF NOT EXISTS connection_sessions (
        token_hash TEXT PRIMARY KEY NOT NULL,
        connection_id INTEGER NOT NULL,
        database_name TEXT,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );
    CREATE INDEX IF NOT EXISTS idx_conn_sessions_hash ON connection_sessions(token_hash);

    -- Hierarchical user groups
    CREATE TABLE IF NOT EXISTS user_groups (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT UNIQUE NOT NULL,
        description TEXT DEFAULT '',
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );
    CREATE INDEX IF NOT EXISTS idx_user_groups_name ON user_groups(name);

    -- Group memberships (users and nested groups)
    CREATE TABLE IF NOT EXISTS group_memberships (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        parent_group_id INTEGER NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
        member_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
        member_group_id INTEGER REFERENCES user_groups(id) ON DELETE CASCADE,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        UNIQUE(parent_group_id, member_user_id),
        UNIQUE(parent_group_id, member_group_id),
        CHECK (
            (member_user_id IS NOT NULL AND member_group_id IS NULL) OR
            (member_user_id IS NULL AND member_group_id IS NOT NULL)
        ),
        CHECK (parent_group_id != member_group_id)
    );
    CREATE INDEX IF NOT EXISTS idx_group_memberships_parent ON group_memberships(parent_group_id);
    CREATE INDEX IF NOT EXISTS idx_group_memberships_user ON group_memberships(member_user_id);
    CREATE INDEX IF NOT EXISTS idx_group_memberships_group ON group_memberships(member_group_id);

    -- MCP privilege identifiers (tools, resources, prompts)
    CREATE TABLE IF NOT EXISTS mcp_privilege_identifiers (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        identifier TEXT UNIQUE NOT NULL,
        item_type TEXT NOT NULL CHECK (item_type IN ('tool', 'resource', 'prompt')),
        description TEXT DEFAULT '',
        is_public BOOLEAN NOT NULL DEFAULT FALSE,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );
    CREATE INDEX IF NOT EXISTS idx_mcp_privileges_identifier ON mcp_privilege_identifiers(identifier);
    CREATE INDEX IF NOT EXISTS idx_mcp_privileges_type ON mcp_privilege_identifiers(item_type);

    -- Group-to-MCP privilege mappings.
    -- privilege_identifier_id intentionally has no FOREIGN KEY declaration
    -- because the value 0 is used as a "wildcard / all MCP privileges"
    -- sentinel and will never match a real row in
    -- mcp_privilege_identifiers. With PRAGMA foreign_keys = ON an FK on
    -- this column would reject every wildcard grant, and nothing ever
    -- deletes from mcp_privilege_identifiers so the CASCADE behavior is
    -- not needed in practice.
    CREATE TABLE IF NOT EXISTS group_mcp_privileges (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        group_id INTEGER NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
        privilege_identifier_id INTEGER NOT NULL,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        UNIQUE(group_id, privilege_identifier_id)
    );
    CREATE INDEX IF NOT EXISTS idx_group_mcp_privs_group ON group_mcp_privileges(group_id);
    CREATE INDEX IF NOT EXISTS idx_group_mcp_privs_priv ON group_mcp_privileges(privilege_identifier_id);

    -- Group-to-connection privilege mappings
    CREATE TABLE IF NOT EXISTS connection_privileges (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        group_id INTEGER NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
        connection_id INTEGER NOT NULL,
        access_level TEXT NOT NULL DEFAULT 'read'
            CHECK (access_level IN ('read', 'read_write')),
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        UNIQUE(group_id, connection_id)
    );
    CREATE INDEX IF NOT EXISTS idx_conn_privs_group ON connection_privileges(group_id);
    CREATE INDEX IF NOT EXISTS idx_conn_privs_conn ON connection_privileges(connection_id);

    -- Token-to-connection scope restrictions
    CREATE TABLE IF NOT EXISTS token_connection_scope (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        token_id INTEGER NOT NULL REFERENCES tokens(id) ON DELETE CASCADE,
        connection_id INTEGER NOT NULL,
        access_level TEXT NOT NULL DEFAULT 'read_write'
            CHECK (access_level IN ('read', 'read_write')),
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        UNIQUE(token_id, connection_id)
    );
    CREATE INDEX IF NOT EXISTS idx_token_conn_scope_token ON token_connection_scope(token_id);

    -- Token-to-MCP privilege scope restrictions.
    -- privilege_identifier_id intentionally has no FOREIGN KEY declaration
    -- for the same reason as group_mcp_privileges above: 0 is used as a
    -- wildcard sentinel that will never match a real privilege row.
    CREATE TABLE IF NOT EXISTS token_mcp_scope (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        token_id INTEGER NOT NULL REFERENCES tokens(id) ON DELETE CASCADE,
        privilege_identifier_id INTEGER NOT NULL,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        UNIQUE(token_id, privilege_identifier_id)
    );
    CREATE INDEX IF NOT EXISTS idx_token_mcp_scope_token ON token_mcp_scope(token_id);

    -- Token admin permission scope
    CREATE TABLE IF NOT EXISTS token_admin_scope (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        token_id INTEGER NOT NULL REFERENCES tokens(id) ON DELETE CASCADE,
        permission TEXT NOT NULL,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        UNIQUE(token_id, permission)
    );
    CREATE INDEX IF NOT EXISTS idx_token_admin_scope_token ON token_admin_scope(token_id);

    -- Group admin permissions
    CREATE TABLE IF NOT EXISTS group_admin_permissions (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        group_id INTEGER NOT NULL
            REFERENCES user_groups(id) ON DELETE CASCADE,
        permission TEXT NOT NULL CHECK (permission IN (
            'manage_connections', 'manage_groups',
            'manage_permissions', 'manage_users',
            'manage_token_scopes', 'manage_blackouts',
            'manage_probes', 'manage_alert_rules',
            'manage_notification_channels', 'store_system_memory', '*'
        )),
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        UNIQUE(group_id, permission)
    );
    CREATE INDEX IF NOT EXISTS idx_admin_perms_group ON group_admin_permissions(group_id);
    CREATE INDEX IF NOT EXISTS idx_admin_perms_perm ON group_admin_permissions(permission);
    `

		_, err = s.db.Exec(schema)
		if err != nil {
			return fmt.Errorf("failed to create schema: %w", err)
		}

		// Set schema version
		_, err = s.db.Exec("DELETE FROM schema_version")
		if err != nil {
			return fmt.Errorf("failed to clean schema version: %w", err)
		}
		_, err = s.db.Exec("INSERT INTO schema_version (version) VALUES (?)", schemaVersion)
		if err != nil {
			return fmt.Errorf("failed to set schema version: %w", err)
		}
	}

	return nil
}

// Close stops the session cleanup goroutine (if running) and closes
// the database connection.
func (s *AuthStore) Close() error {
	s.StopSessionCleanup()
	return s.db.Close()
}

// =============================================================================
// Password Validation
// =============================================================================

// MinPasswordLength is the minimum number of characters required for a password.
const MinPasswordLength = 8

// MaxPasswordLength is the maximum number of characters allowed for a password.
// This is set to 72 because bcrypt silently truncates inputs beyond 72 bytes.
const MaxPasswordLength = 72

// ValidatePassword checks that a password meets complexity requirements.
// Passwords must be between MinPasswordLength and MaxPasswordLength characters
// and contain at least one uppercase letter, one lowercase letter, and one digit.
func ValidatePassword(password string) error {
	var failures []string
	if utf8.RuneCountInString(password) < MinPasswordLength {
		failures = append(failures, fmt.Sprintf("must be at least %d characters", MinPasswordLength))
	}
	if len(password) > MaxPasswordLength {
		failures = append(failures, fmt.Sprintf("must be at most %d characters", MaxPasswordLength))
	}
	var hasUpper, hasLower, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasUpper {
		failures = append(failures, "must contain at least one uppercase letter")
	}
	if !hasLower {
		failures = append(failures, "must contain at least one lowercase letter")
	}
	if !hasDigit {
		failures = append(failures, "must contain at least one digit")
	}
	if len(failures) > 0 {
		return fmt.Errorf("password does not meet complexity requirements: %s", strings.Join(failures, "; "))
	}
	return nil
}

// ValidateUsername checks that a username meets format requirements.
func ValidateUsername(username string) error {
	n := utf8.RuneCountInString(username)
	if n == 0 {
		return fmt.Errorf("username must not be empty")
	}
	if n > 128 {
		return fmt.Errorf("username must be at most 128 characters")
	}
	first := true
	for _, r := range username {
		if first {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return fmt.Errorf("username must start with a letter or digit")
			}
			first = false
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '.' && r != '@' && r != '-' {
			return fmt.Errorf("username contains invalid character: %c", r)
		}
	}
	return nil
}

// =============================================================================
// User Management
// =============================================================================

// scannable is an interface satisfied by both *sql.Row and *sql.Rows,
// allowing a single helper to scan user rows from either source.
type scannable interface {
	Scan(dest ...any) error
}

// scanUser scans a user row into a StoredUser. It handles the NullString
// conversion for display_name and email columns.
func scanUser(row scannable) (*StoredUser, error) {
	var user StoredUser
	var displayName sql.NullString
	var email sql.NullString
	err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt,
		&user.LastLogin, &user.Enabled, &user.Annotation, &displayName, &email,
		&user.FailedAttempts, &user.IsSuperuser, &user.IsServiceAccount)
	if err != nil {
		return nil, err
	}
	if displayName.Valid {
		user.DisplayName = displayName.String
	}
	if email.Valid {
		user.Email = email.String
	}
	return &user, nil
}

// CreateUser creates a new user
func (s *AuthStore) CreateUser(username, password, annotation, displayName, email string) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}

	if err := ValidatePassword(password); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	_, err = s.db.Exec(
		"INSERT INTO users (username, password_hash, annotation, display_name, email) VALUES (?, ?, ?, ?, ?)",
		username, string(hash), annotation, displayName, email,
	)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

// GetUser retrieves a user by username
func (s *AuthStore) GetUser(username string) (*StoredUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(
		`SELECT id, username, password_hash, created_at, last_login, enabled, annotation, display_name, email, failed_attempts, is_superuser, is_service_account
         FROM users WHERE username = ?`,
		username,
	)
	user, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return user, nil
}

// GetUserByID retrieves a user by their ID
func (s *AuthStore) GetUserByID(id int64) (*StoredUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(
		`SELECT id, username, password_hash, created_at, last_login, enabled, annotation, display_name, email, failed_attempts, is_superuser, is_service_account
         FROM users WHERE id = ?`,
		id,
	)
	user, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by ID: %w", err)
	}

	return user, nil
}

// UpdateUser updates a user's password, annotation, display name, and/or email
func (s *AuthStore) UpdateUser(username, newPassword, newAnnotation, newDisplayName, newEmail string) error {
	if newPassword != "" {
		if err := ValidatePassword(newPassword); err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if newPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
		if err != nil {
			return fmt.Errorf("failed to hash password: %w", err)
		}
		_, err = s.db.Exec("UPDATE users SET password_hash = ? WHERE username = ?", string(hash), username)
		if err != nil {
			return fmt.Errorf("failed to update password: %w", err)
		}
	}

	_, err := s.db.Exec("UPDATE users SET annotation = ?, display_name = ?, email = ? WHERE username = ?",
		newAnnotation, newDisplayName, newEmail, username)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	// Invalidate all active sessions when the password changes
	if newPassword != "" {
		s.InvalidateUserSessions(username)
	}

	return nil
}

// UserUpdate contains all fields that can be updated atomically for a user.
// Pointer fields are used to distinguish between "not provided" (nil) and
// "set to empty/zero value".
type UserUpdate struct {
	Password    *string // New password (will be hashed), nil = no change
	Annotation  *string // New annotation, nil = no change
	DisplayName *string // New display name, nil = no change
	Email       *string // New email, nil = no change
	Enabled     *bool   // New enabled status, nil = no change
	IsSuperuser *bool   // New superuser status, nil = no change
}

// UpdateUserAtomic updates multiple user fields in a single atomic transaction.
// This ensures that either all changes are applied or none are, preventing
// partial updates that could leave the user in an inconsistent state.
func (s *AuthStore) UpdateUserAtomic(username string, update UserUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Start transaction
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			//nolint:errcheck // Rollback error is not critical, transaction already failed
			tx.Rollback()
		}
	}()

	// Validate and update password if provided
	if update.Password != nil && *update.Password != "" {
		if valErr := ValidatePassword(*update.Password); valErr != nil {
			err = valErr
			return err
		}
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(*update.Password), bcryptCost)
		if hashErr != nil {
			err = fmt.Errorf("failed to hash password: %w", hashErr)
			return err
		}
		_, execErr := tx.Exec("UPDATE users SET password_hash = ? WHERE username = ?", string(hash), username)
		if execErr != nil {
			err = fmt.Errorf("failed to update password: %w", execErr)
			return err
		}
	}

	// Update annotation, display name, and email if any are provided
	if update.Annotation != nil || update.DisplayName != nil || update.Email != nil {
		// Get current values to preserve unchanged fields
		var currentAnnotation, currentDisplayName, currentEmail sql.NullString
		queryErr := tx.QueryRow(
			"SELECT annotation, display_name, email FROM users WHERE username = ?",
			username,
		).Scan(&currentAnnotation, &currentDisplayName, &currentEmail)
		if queryErr != nil {
			err = fmt.Errorf("failed to get current user values: %w", queryErr)
			return err
		}

		newAnnotation := currentAnnotation.String
		if update.Annotation != nil {
			newAnnotation = *update.Annotation
		}
		newDisplayName := currentDisplayName.String
		if update.DisplayName != nil {
			newDisplayName = *update.DisplayName
		}
		newEmail := currentEmail.String
		if update.Email != nil {
			newEmail = *update.Email
		}

		_, execErr := tx.Exec(
			"UPDATE users SET annotation = ?, display_name = ?, email = ? WHERE username = ?",
			newAnnotation, newDisplayName, newEmail, username,
		)
		if execErr != nil {
			err = fmt.Errorf("failed to update user fields: %w", execErr)
			return err
		}
	}

	// Update enabled status if provided
	if update.Enabled != nil {
		_, execErr := tx.Exec("UPDATE users SET enabled = ? WHERE username = ?", *update.Enabled, username)
		if execErr != nil {
			err = fmt.Errorf("failed to update enabled status: %w", execErr)
			return err
		}
	}

	// Update superuser status if provided
	if update.IsSuperuser != nil {
		_, execErr := tx.Exec("UPDATE users SET is_superuser = ? WHERE username = ?", *update.IsSuperuser, username)
		if execErr != nil {
			err = fmt.Errorf("failed to update superuser status: %w", execErr)
			return err
		}
	}

	// Commit transaction
	if commitErr := tx.Commit(); commitErr != nil {
		err = fmt.Errorf("failed to commit transaction: %w", commitErr)
		return err
	}

	// Invalidate all active sessions when the password changes
	if update.Password != nil && *update.Password != "" {
		s.InvalidateUserSessions(username)
	}

	return nil
}

// UpdateUserDisplayName updates a user's display name
func (s *AuthStore) UpdateUserDisplayName(username, displayName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("UPDATE users SET display_name = ? WHERE username = ?", displayName, username)
	if err != nil {
		return fmt.Errorf("failed to update display name: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("user '%s' not found", username)
	}

	return nil
}

// UpdateUserEmail updates a user's email address
func (s *AuthStore) UpdateUserEmail(username, email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("UPDATE users SET email = ? WHERE username = ?", email, username)
	if err != nil {
		return fmt.Errorf("failed to update email: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("user '%s' not found", username)
	}

	return nil
}

// DeleteUser removes a user and all of its dependent rows in a single
// atomic transaction. This removes every row that references the user:
// tokens (and their scope rows via the token delete cascade), group
// memberships, and connection_sessions rows for any of the user's
// tokens.
//
// With PRAGMA foreign_keys = ON enabled in NewAuthStore, the ON DELETE
// CASCADE foreign keys declared in the schema would remove most of
// these rows automatically. These explicit deletes are intentionally
// kept as defense in depth: they document exactly which tables are
// touched, survive accidental pragma regression, and clean up
// connection_sessions rows which reference token_hash without an FK.
func (s *AuthStore) DeleteUser(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			//nolint:errcheck // Rollback error is not critical; the
			// outer error is already being returned to the caller.
			tx.Rollback()
		}
	}()

	// Look up the user ID first so we can fail fast on "not found" and
	// drive the dependent deletes by id rather than by username.
	var userID int64
	if err = tx.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&userID); err != nil {
		if err == sql.ErrNoRows {
			err = fmt.Errorf("user '%s' not found", username)
			return err
		}
		return fmt.Errorf("failed to look up user: %w", err)
	}

	// Remove connection_sessions rows for every token owned by this
	// user. connection_sessions references tokens.token_hash but has
	// no declared FK, so SQLite will never cascade-delete these even
	// with foreign_keys pragma on.
	if _, err = tx.Exec(
		`DELETE FROM connection_sessions
         WHERE token_hash IN (SELECT token_hash FROM tokens WHERE owner_id = ?)`,
		userID,
	); err != nil {
		return fmt.Errorf("failed to delete user's connection sessions: %w", err)
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
		if _, err = tx.Exec(stmt, userID); err != nil { // nosemgrep: go-sql-concat-sqli
			return fmt.Errorf("failed to delete %s rows: %w", table, err)
		}
	}

	// Remove the user's tokens. tokens.owner_id has ON DELETE CASCADE
	// so the user-row delete below would take these out anyway; we
	// delete them first so the scope-row cleanup above has a stable
	// set to operate on and so the behavior is obvious to readers.
	if _, err = tx.Exec("DELETE FROM tokens WHERE owner_id = ?", userID); err != nil {
		return fmt.Errorf("failed to delete user's tokens: %w", err)
	}

	// Remove group memberships that reference this user directly.
	if _, err = tx.Exec(
		"DELETE FROM group_memberships WHERE member_user_id = ?", userID,
	); err != nil {
		return fmt.Errorf("failed to delete user's group memberships: %w", err)
	}

	// Finally, delete the user row itself.
	result, execErr := tx.Exec("DELETE FROM users WHERE id = ?", userID)
	if execErr != nil {
		err = fmt.Errorf("failed to delete user: %w", execErr)
		return err
	}
	rows, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		err = fmt.Errorf("failed to get rows affected: %w", rowsErr)
		return err
	}
	if rows == 0 {
		// This should be unreachable because we already looked the
		// user up above, but guard against races anyway.
		err = fmt.Errorf("user '%s' not found", username)
		return err
	}

	if commitErr := tx.Commit(); commitErr != nil {
		err = fmt.Errorf("failed to commit user deletion: %w", commitErr)
		return err
	}

	s.InvalidateUserSessions(username)

	return nil
}

// EnableUser enables a user account
func (s *AuthStore) EnableUser(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("UPDATE users SET enabled = TRUE WHERE username = ?", username)
	if err != nil {
		return fmt.Errorf("failed to enable user: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("user '%s' not found", username)
	}

	return nil
}

// DisableUser disables a user account
func (s *AuthStore) DisableUser(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("UPDATE users SET enabled = FALSE WHERE username = ?", username)
	if err != nil {
		return fmt.Errorf("failed to disable user: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("user '%s' not found", username)
	}

	s.InvalidateUserSessions(username)

	return nil
}

// ListUsers returns all users
func (s *AuthStore) ListUsers() ([]*StoredUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, username, password_hash, created_at, last_login, enabled, annotation, display_name, email, failed_attempts, is_superuser, is_service_account
         FROM users ORDER BY username`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*StoredUser
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating users: %w", err)
	}

	return users, nil
}

// ResetFailedAttempts resets the failed login attempts counter
func (s *AuthStore) ResetFailedAttempts(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("UPDATE users SET failed_attempts = 0 WHERE username = ?", username)
	return err
}

// =============================================================================
// User Authentication
// =============================================================================

// AuthenticateUser verifies credentials and returns a session token
func (s *AuthStore) AuthenticateUser(username, password string) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user StoredUser
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, enabled, failed_attempts, is_service_account FROM users WHERE username = ?`,
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Enabled, &user.FailedAttempts, &user.IsServiceAccount)

	if err == sql.ErrNoRows {
		// Perform a dummy bcrypt comparison to ensure consistent response
		// timing regardless of whether the username exists, preventing
		// user enumeration via timing side-channel.
		//nolint:errcheck // Result is intentionally ignored
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", time.Time{}, fmt.Errorf("invalid username or password")
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("authentication error: %w", err)
	}

	// Service accounts cannot authenticate with password
	if user.IsServiceAccount {
		log.Printf("[AUTH] Authentication failed for user %s: service account cannot use password login", username)
		return "", time.Time{}, fmt.Errorf("invalid username or password")
	}

	if !user.Enabled {
		// Log the actual reason for audit purposes, but return generic error
		// to prevent user enumeration attacks
		log.Printf("[AUTH] Authentication failed for user %s: account is disabled", username)
		return "", time.Time{}, fmt.Errorf("invalid username or password")
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		// Increment failed attempts (non-critical, best effort)
		user.FailedAttempts++
		//nolint:errcheck // Best effort update, authentication already failed
		s.db.Exec("UPDATE users SET failed_attempts = ? WHERE id = ?", user.FailedAttempts, user.ID)

		// Lock account if threshold reached
		if s.maxFailedAttempts > 0 && user.FailedAttempts >= s.maxFailedAttempts {
			//nolint:errcheck // Best effort update, authentication already failed
			s.db.Exec("UPDATE users SET enabled = FALSE WHERE id = ?", user.ID)
			log.Printf("[AUTH] Account locked for user %s after %d failed attempts; invalidating sessions", username, user.FailedAttempts)
			s.InvalidateUserSessions(username)
		}

		return "", time.Time{}, fmt.Errorf("invalid username or password")
	}

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
	s.db.Exec("UPDATE users SET last_login = ?, failed_attempts = 0 WHERE id = ?", now, user.ID)

	return token, expiration, nil
}

// ValidateSessionToken checks if a session token is valid.
// Uses token hashing to prevent timing attacks - the hash lookup has
// consistent timing regardless of whether the token exists.
func (s *AuthStore) ValidateSessionToken(token string) (string, error) {
	// Hash the token first for constant-time lookup
	tokenHash := GetTokenHashByRawToken(token)

	value, ok := s.sessions.Load(tokenHash)
	if !ok {
		return "", fmt.Errorf("invalid session token")
	}

	session, ok := value.(*SessionInfo)
	if !ok {
		return "", fmt.Errorf("invalid session data")
	}
	if session.ExpiresAt.Before(time.Now()) {
		s.sessions.Delete(tokenHash)
		return "", fmt.Errorf("invalid session token")
	}

	// Verify user is still enabled
	user, err := s.GetUser(session.Username)
	if err != nil || user == nil {
		s.sessions.Delete(tokenHash)
		return "", fmt.Errorf("invalid session token")
	}
	if !user.Enabled {
		// Log the actual reason for audit purposes, but return generic error
		// to prevent user enumeration attacks
		log.Printf("[AUTH] Session validation failed for user %s: account is disabled", session.Username)
		s.sessions.Delete(tokenHash)
		return "", fmt.Errorf("invalid session token")
	}

	return session.Username, nil
}

// InvalidateSession removes a session token
func (s *AuthStore) InvalidateSession(token string) {
	s.sessions.Delete(GetTokenHashByRawToken(token))
}

// InvalidateUserSessions removes all active sessions for a given username.
// This is called after a password change to ensure that compromised sessions
// cannot persist after credential rotation.
func (s *AuthStore) InvalidateUserSessions(username string) {
	count := 0
	s.sessions.Range(func(key, value any) bool {
		session, ok := value.(*SessionInfo)
		if ok && session.Username == username {
			s.sessions.Delete(key)
			count++
		}
		return true
	})
	if count > 0 {
		log.Printf("[AUTH] Invalidated %d active session(s) for user %s due to password change", count, username)
	}
}

// StartSessionCleanup starts a background goroutine that periodically
// sweeps expired sessions from the in-memory session store. Call
// StopSessionCleanup (or Close) to terminate the goroutine.
func (s *AuthStore) StartSessionCleanup(interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Prevent double-start
	if s.sessionCleanupStop != nil {
		return
	}
	stop := make(chan struct{})
	s.sessionCleanupStop = stop

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				now := time.Now()
				s.sessions.Range(func(key, value any) bool {
					session, ok := value.(*SessionInfo)
					if ok && session.ExpiresAt.Before(now) {
						s.sessions.Delete(key)
					}
					return true
				})
			case <-stop:
				return
			}
		}
	}()
}

// StopSessionCleanup terminates the background session cleanup goroutine
// started by StartSessionCleanup. It is safe to call multiple times.
func (s *AuthStore) StopSessionCleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionCleanupStop != nil {
		close(s.sessionCleanupStop)
		s.sessionCleanupStop = nil
	}
}

// =============================================================================
// Token Management
// =============================================================================

// CreateToken creates a new token owned by the specified user.
// Returns the raw token (only shown once) and the stored token info.
// If requestedExpiry is nil, superusers get no expiry while non-superusers
// are subject to the configured maxUserTokenDays limit.
func (s *AuthStore) CreateToken(ownerUsername, annotation string, requestedExpiry *time.Time) (string, *StoredToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Look up the user to get their ID and superuser status
	var userID int64
	var isSuperuser bool
	err := s.db.QueryRow(
		"SELECT id, is_superuser FROM users WHERE username = ?",
		ownerUsername,
	).Scan(&userID, &isSuperuser)
	if err == sql.ErrNoRows {
		return "", nil, fmt.Errorf("user '%s' not found", ownerUsername)
	}
	if err != nil {
		return "", nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Calculate expiry
	var expiry *time.Time
	if requestedExpiry != nil {
		expiry = requestedExpiry
	} else if !isSuperuser && s.maxUserTokenDays > 0 {
		// Non-superuser with no explicit expiry: enforce max token lifetime
		exp := time.Now().AddDate(0, 0, s.maxUserTokenDays)
		expiry = &exp
	}
	// else: superuser with no explicit expiry gets no expiry (unlimited)

	// Generate random token
	tokenBytes := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", nil, fmt.Errorf("failed to generate token: %w", err)
	}
	rawToken := base64.URLEncoding.EncodeToString(tokenBytes)

	// Hash the token for storage
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	// Insert into database
	result, err := s.db.Exec(
		`INSERT INTO tokens (token_hash, owner_id, expires_at, annotation)
         VALUES (?, ?, ?, ?)`,
		tokenHash, userID, expiry, annotation,
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create token: %w", err)
	}

	//nolint:errcheck // SQLite always supports LastInsertId
	id, _ := result.LastInsertId()
	token := &StoredToken{
		ID:         id,
		TokenHash:  tokenHash,
		OwnerID:    userID,
		ExpiresAt:  expiry,
		Annotation: annotation,
		CreatedAt:  time.Now(),
	}

	return rawToken, token, nil
}

// CreateServiceAccount creates a new service account user.
// Service accounts have no password and cannot authenticate via login.
// They can only be used via API tokens.
func (s *AuthStore) CreateServiceAccount(username, annotation, displayName, email string) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		"INSERT INTO users (username, password_hash, annotation, display_name, email, is_service_account, enabled) VALUES (?, '', ?, ?, ?, TRUE, TRUE)",
		username, annotation, displayName, email,
	)
	if err != nil {
		return fmt.Errorf("failed to create service account: %w", err)
	}

	return nil
}

// ListUserTokens lists all tokens owned by a user
func (s *AuthStore) ListUserTokens(username string) ([]*StoredToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT t.id, t.token_hash, t.owner_id, t.expires_at, t.annotation, t.created_at, t.database
         FROM tokens t
         JOIN users u ON t.owner_id = u.id
         WHERE u.username = ?
         ORDER BY t.created_at DESC`,
		username,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list user tokens: %w", err)
	}
	defer rows.Close()

	return s.scanTokens(rows)
}

// DeleteUserToken deletes a token (only if owned by the specified user)
// and removes every row that references that token in a single atomic
// transaction: its connection scope, MCP scope, admin scope, and the
// connection_sessions row keyed on its token_hash.
//
// With PRAGMA foreign_keys = ON enabled in NewAuthStore, the scope rows
// would cascade via ON DELETE CASCADE. These explicit deletes are
// intentionally kept as defense in depth and also clean up
// connection_sessions, which references token_hash without an FK.
func (s *AuthStore) DeleteUserToken(username string, tokenID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.deleteTokensByFilter(
		// Filter to token IDs owned by the named user.
		"id = ? AND owner_id = (SELECT id FROM users WHERE username = ?)",
		[]any{tokenID, username},
		"token not found or not owned by user",
	)
}

// =============================================================================
// Token Validation (all token types)
// =============================================================================

// ValidateToken checks if a token is valid.
// Returns the token info if valid.
func (s *AuthStore) ValidateToken(rawToken string) (*StoredToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Hash the provided token
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	var token StoredToken
	var annotation sql.NullString
	var database sql.NullString
	err := s.db.QueryRow(
		`SELECT id, token_hash, owner_id, expires_at, annotation, created_at, database
         FROM tokens WHERE token_hash = ?`,
		tokenHash,
	).Scan(&token.ID, &token.TokenHash, &token.OwnerID,
		&token.ExpiresAt, &annotation, &token.CreatedAt, &database)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid token")
	}
	if err != nil {
		return nil, fmt.Errorf("token validation error: %w", err)
	}
	if annotation.Valid {
		token.Annotation = annotation.String
	}
	if database.Valid {
		token.Database = database.String
	}

	// Check expiration
	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("token has expired")
	}

	// Verify the owning user is still enabled
	var enabled bool
	err = s.db.QueryRow("SELECT enabled FROM users WHERE id = ?", token.OwnerID).Scan(&enabled)
	if err != nil || !enabled {
		return nil, fmt.Errorf("token owner is disabled")
	}

	return &token, nil
}

// =============================================================================
// Token Management (for CLI commands)
// =============================================================================

// ListAllTokens lists all tokens
func (s *AuthStore) ListAllTokens() ([]*StoredToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, token_hash, owner_id, expires_at, annotation, created_at, database
         FROM tokens ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list tokens: %w", err)
	}
	defer rows.Close()

	return s.scanTokens(rows)
}

// DeleteToken deletes a token by ID or hash prefix (admin use, no owner
// check) and removes every row that references that token in a single
// atomic transaction: its connection scope, MCP scope, admin scope, and
// the connection_sessions row keyed on its token_hash.
//
// With PRAGMA foreign_keys = ON enabled in NewAuthStore, the scope rows
// would cascade via ON DELETE CASCADE. These explicit deletes are
// intentionally kept as defense in depth and also clean up
// connection_sessions, which references token_hash without an FK.
func (s *AuthStore) DeleteToken(identifier string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Try by exact ID first. The callers pass strings, and the tokens
	// table id column is INTEGER, so SQLite will coerce the string
	// safely. A non-numeric identifier matches nothing here and falls
	// through to the hash-prefix branch below.
	if err := s.deleteTokensByFilter("id = ?", []any{identifier}, ""); err == nil {
		return nil
	}

	// Try by hash prefix. Require at least 8 characters to avoid
	// matching a huge swath of tokens on short inputs.
	if len(identifier) >= 8 {
		if err := s.deleteTokensByFilter(
			"token_hash LIKE ?", []any{identifier + "%"}, "",
		); err == nil {
			return nil
		}
	}

	return fmt.Errorf("token not found")
}

// deleteTokensByFilter deletes every token matched by the supplied
// WHERE clause along with every row that references those tokens,
// atomically. The whereClause is embedded in "SELECT id FROM tokens
// WHERE <clause>", so it must not contain user-supplied text; all
// dynamic values belong in the args slice. notFoundMsg, when non-empty,
// is returned (wrapped in an error) if the filter matches no rows.
// When empty, a zero-rows-affected outcome returns a generic
// "token not found" error so callers can chain filters.
func (s *AuthStore) deleteTokensByFilter(whereClause string, args []any, notFoundMsg string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			//nolint:errcheck // Rollback error is not critical; the
			// outer error is already being returned to the caller.
			tx.Rollback()
		}
	}()

	// Collect the set of (id, token_hash) pairs that match. We need
	// token_hash so we can clean up connection_sessions rows, which
	// are keyed on the raw hash and have no declared FK.
	//nolint:gosec // whereClause is a static SQL fragment from callers in this package
	selectStmt := "SELECT id, token_hash FROM tokens WHERE " + whereClause
	rows, err := tx.Query(selectStmt, args...) // nosemgrep: go-sql-concat-sqli
	if err != nil {
		return fmt.Errorf("failed to query tokens: %w", err)
	}
	type tokenRef struct {
		id   int64
		hash string
	}
	var matches []tokenRef
	for rows.Next() {
		var ref tokenRef
		if scanErr := rows.Scan(&ref.id, &ref.hash); scanErr != nil {
			rows.Close()
			err = fmt.Errorf("failed to scan token row: %w", scanErr)
			return err
		}
		matches = append(matches, ref)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		rows.Close()
		err = fmt.Errorf("error iterating tokens: %w", rowsErr)
		return err
	}
	rows.Close()

	if len(matches) == 0 {
		if notFoundMsg != "" {
			err = fmt.Errorf("%s", notFoundMsg)
		} else {
			err = fmt.Errorf("token not found")
		}
		return err
	}

	// Delete the per-token scope rows explicitly (defense in depth;
	// FK cascades would handle this when the pragma is on).
	scopeTables := []string{
		"token_connection_scope",
		"token_mcp_scope",
		"token_admin_scope",
	}
	for _, ref := range matches {
		for _, table := range scopeTables {
			//nolint:gosec // table name is from a static allow-list above
			stmt := "DELETE FROM " + table + " WHERE token_id = ?"
			if _, err = tx.Exec(stmt, ref.id); err != nil { // nosemgrep: go-sql-concat-sqli
				return fmt.Errorf("failed to delete %s rows: %w", table, err)
			}
		}
		// Clear the connection_sessions row keyed on this hash.
		if _, err = tx.Exec(
			"DELETE FROM connection_sessions WHERE token_hash = ?", ref.hash,
		); err != nil {
			return fmt.Errorf("failed to delete connection session: %w", err)
		}
	}

	// Delete the token rows themselves.
	//nolint:gosec // whereClause is a static SQL fragment from callers in this package
	deleteStmt := "DELETE FROM tokens WHERE " + whereClause
	if _, err = tx.Exec(deleteStmt, args...); err != nil { // nosemgrep: go-sql-concat-sqli
		return fmt.Errorf("failed to delete tokens: %w", err)
	}

	if commitErr := tx.Commit(); commitErr != nil {
		err = fmt.Errorf("failed to commit token deletion: %w", commitErr)
		return err
	}

	return nil
}

// CleanupExpiredTokens removes all expired tokens
// Returns the number of tokens removed and their hashes
func (s *AuthStore) CleanupExpiredTokens() (int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get hashes of expired tokens before deletion (for connection cleanup)
	rows, err := s.db.Query(
		"SELECT token_hash FROM tokens WHERE expires_at IS NOT NULL AND expires_at < ?",
		time.Now(),
	)
	if err != nil {
		return 0, nil
	}

	var hashes []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err == nil {
			hashes = append(hashes, hash)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		log.Printf("[AUTH] Error iterating expired token rows: %v", rowsErr)
	}
	rows.Close()

	// Delete expired tokens
	result, err := s.db.Exec(
		"DELETE FROM tokens WHERE expires_at IS NOT NULL AND expires_at < ?",
		time.Now(),
	)
	if err != nil {
		log.Printf("[AUTH] Failed to delete expired tokens: %v", err)
		return 0, nil
	}

	//nolint:errcheck // RowsAffected error non-critical for cleanup count
	count, _ := result.RowsAffected()
	return int(count), hashes
}

// scanTokens is a helper to scan token rows
func (s *AuthStore) scanTokens(rows *sql.Rows) ([]*StoredToken, error) {
	var tokens []*StoredToken
	for rows.Next() {
		var token StoredToken
		var annotation sql.NullString
		var database sql.NullString
		if err := rows.Scan(&token.ID, &token.TokenHash, &token.OwnerID,
			&token.ExpiresAt, &annotation, &token.CreatedAt, &database); err != nil {
			return nil, fmt.Errorf("failed to scan token: %w", err)
		}
		if annotation.Valid {
			token.Annotation = annotation.String
		}
		if database.Valid {
			token.Database = database.String
		}
		tokens = append(tokens, &token)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tokens: %w", err)
	}

	return tokens, nil
}

// GetTokenHashByRawToken returns the hash for a raw token (for connection lookup)
func GetTokenHashByRawToken(rawToken string) string {
	hash := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(hash[:])
}

// UserCount returns the number of users in the store
func (s *AuthStore) UserCount() int {
	var count int
	//nolint:errcheck // Returns 0 on error, which is acceptable
	s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count
}

// TokenCount returns the number of tokens in the store
func (s *AuthStore) TokenCount() int {
	var count int
	//nolint:errcheck // Returns 0 on error, which is acceptable
	s.db.QueryRow("SELECT COUNT(*) FROM tokens").Scan(&count)
	return count
}

// GetCounts returns the number of users and tokens
func (s *AuthStore) GetCounts() (int, int) {
	return s.UserCount(), s.TokenCount()
}

// =============================================================================
// Connection Session Management
// =============================================================================

// SetConnectionSession sets the selected database connection for a token
func (s *AuthStore) SetConnectionSession(tokenHash string, connectionID int, databaseName *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
        INSERT INTO connection_sessions (token_hash, connection_id, database_name, updated_at)
        VALUES (?, ?, ?, CURRENT_TIMESTAMP)
        ON CONFLICT(token_hash) DO UPDATE SET
            connection_id = excluded.connection_id,
            database_name = excluded.database_name,
            updated_at = CURRENT_TIMESTAMP
    `, tokenHash, connectionID, databaseName)

	if err != nil {
		return fmt.Errorf("failed to set connection session: %w", err)
	}

	return nil
}

// GetConnectionSession returns the selected database connection for a token
func (s *AuthStore) GetConnectionSession(tokenHash string) (*ConnectionSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var session ConnectionSession
	session.TokenHash = tokenHash

	err := s.db.QueryRow(`
        SELECT connection_id, database_name
        FROM connection_sessions
        WHERE token_hash = ?
    `, tokenHash).Scan(&session.ConnectionID, &session.DatabaseName)

	if err == sql.ErrNoRows {
		return nil, nil // No session set
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get connection session: %w", err)
	}

	return &session, nil
}

// ClearConnectionSession clears the selected database connection for a token
func (s *AuthStore) ClearConnectionSession(tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM connection_sessions WHERE token_hash = ?", tokenHash)
	if err != nil {
		return fmt.Errorf("failed to clear connection session: %w", err)
	}

	return nil
}

// ClearAllConnectionSessions clears all connection sessions (for cleanup)
func (s *AuthStore) ClearAllConnectionSessions() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM connection_sessions")
	if err != nil {
		return fmt.Errorf("failed to clear all connection sessions: %w", err)
	}

	return nil
}
