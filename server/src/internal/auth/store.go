/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

const (
	// DefaultSessionExpiry is the default duration for session tokens
	DefaultSessionExpiry = 24 * time.Hour

	// Token types
	TokenTypeService = "service" // Admin-created service tokens
	TokenTypeUser    = "user"    // User-created tokens

	// Schema version for migrations
	schemaVersion = 9
)

// AuthStore manages users and tokens in SQLite
type AuthStore struct {
	db                *sql.DB
	mu                sync.RWMutex
	path              string
	maxUserTokenDays  int      // Maximum lifetime for user-created tokens (0 = unlimited)
	maxFailedAttempts int      // Max failed login attempts before lockout (0 = disabled)
	sessions          sync.Map // In-memory session store: token -> SessionInfo
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
	ID             int64
	Username       string
	PasswordHash   string
	CreatedAt      time.Time
	LastLogin      *time.Time
	Enabled        bool
	Annotation     string
	DisplayName    string
	Email          string
	FailedAttempts int
	IsSuperuser    bool
}

// StoredToken represents a token in the database
type StoredToken struct {
	ID          int64
	TokenHash   string     // SHA256 hash of the actual token
	TokenType   string     // "service" or "user"
	OwnerID     *int64     // NULL for service tokens, user ID for user tokens
	ExpiresAt   *time.Time // NULL for never expires
	Annotation  string
	CreatedAt   time.Time
	Database    string // Bound database name (empty = first configured database)
	IsSuperuser bool   // Superuser status (bypasses all privilege checks)
}

// NewAuthStore creates a new SQLite-based auth store
func NewAuthStore(dataDir string, maxUserTokenDays, maxFailedAttempts int) (*AuthStore, error) {
	// Create data directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "auth.db")
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open auth database: %w", err)
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

// initSchema creates the database tables if they don't exist
func (s *AuthStore) initSchema() error {
	// Check current schema version
	var currentVersion int
	err := s.db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&currentVersion)
	if err != nil && err != sql.ErrNoRows {
		// Table might not exist yet, that's fine
		currentVersion = 0
	}

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
        failed_attempts INTEGER DEFAULT 0
    );
    CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

    -- Tokens table (both service and user tokens)
    CREATE TABLE IF NOT EXISTS tokens (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        token_hash TEXT UNIQUE NOT NULL,
        token_type TEXT NOT NULL CHECK(token_type IN ('service', 'user')),
        owner_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
        expires_at TIMESTAMP,
        annotation TEXT DEFAULT '',
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        database TEXT DEFAULT ''
    );
    CREATE INDEX IF NOT EXISTS idx_tokens_hash ON tokens(token_hash);
    CREATE INDEX IF NOT EXISTS idx_tokens_owner ON tokens(owner_id);
    CREATE INDEX IF NOT EXISTS idx_tokens_type ON tokens(token_type);
    `

	_, err = s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Apply migrations for schema version 2
	if currentVersion < 2 {
		migrationV2 := `
        -- Connection sessions table (tracks selected database connection per token)
        CREATE TABLE IF NOT EXISTS connection_sessions (
            token_hash TEXT PRIMARY KEY NOT NULL,
            connection_id INTEGER NOT NULL,
            database_name TEXT,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        );
        CREATE INDEX IF NOT EXISTS idx_conn_sessions_hash ON connection_sessions(token_hash);
        `
		_, err = s.db.Exec(migrationV2)
		if err != nil {
			return fmt.Errorf("failed to apply schema migration v2: %w", err)
		}
	}

	// Apply migrations for schema version 3 (RBAC)
	if currentVersion < 3 {
		migrationV3 := `
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
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        );
        CREATE INDEX IF NOT EXISTS idx_mcp_privileges_identifier ON mcp_privilege_identifiers(identifier);
        CREATE INDEX IF NOT EXISTS idx_mcp_privileges_type ON mcp_privilege_identifiers(item_type);

        -- Group-to-MCP privilege mappings
        CREATE TABLE IF NOT EXISTS group_mcp_privileges (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            group_id INTEGER NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
            privilege_identifier_id INTEGER NOT NULL
                REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
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
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(token_id, connection_id)
        );
        CREATE INDEX IF NOT EXISTS idx_token_conn_scope_token ON token_connection_scope(token_id);

        -- Token-to-MCP privilege scope restrictions
        CREATE TABLE IF NOT EXISTS token_mcp_scope (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            token_id INTEGER NOT NULL REFERENCES tokens(id) ON DELETE CASCADE,
            privilege_identifier_id INTEGER NOT NULL
                REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(token_id, privilege_identifier_id)
        );
        CREATE INDEX IF NOT EXISTS idx_token_mcp_scope_token ON token_mcp_scope(token_id);
        `
		// Execute each statement separately for SQLite (ALTER TABLE doesn't combine well)
		statements := []string{
			"ALTER TABLE users ADD COLUMN is_superuser BOOLEAN DEFAULT FALSE",
			"ALTER TABLE tokens ADD COLUMN is_superuser BOOLEAN DEFAULT FALSE",
		}
		for _, stmt := range statements {
			// Ignore errors for ALTER TABLE as column may already exist
			//nolint:errcheck // Intentionally ignoring error - column may already exist
			s.db.Exec(stmt)
		}

		// Execute the rest of the migration
		_, err = s.db.Exec(migrationV3)
		if err != nil {
			return fmt.Errorf("failed to apply schema migration v3: %w", err)
		}
	}

	// Apply migrations for schema version 4 (admin permissions)
	if currentVersion < 4 {
		migrationV4 := `
        -- Group admin permissions
        CREATE TABLE IF NOT EXISTS group_admin_permissions (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            group_id INTEGER NOT NULL
                REFERENCES user_groups(id) ON DELETE CASCADE,
            permission TEXT NOT NULL CHECK (permission IN (
                'manage_connections', 'manage_groups',
                'manage_permissions', 'manage_users',
                'manage_token_scopes', 'manage_blackouts'
            )),
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(group_id, permission)
        );
        CREATE INDEX IF NOT EXISTS idx_admin_perms_group ON group_admin_permissions(group_id);
        CREATE INDEX IF NOT EXISTS idx_admin_perms_perm ON group_admin_permissions(permission);
        `
		_, err = s.db.Exec(migrationV4)
		if err != nil {
			return fmt.Errorf("failed to apply schema migration v4: %w", err)
		}
	}

	// Apply migrations for schema version 5 (user profile fields)
	if currentVersion < 5 {
		statements := []string{
			"ALTER TABLE users ADD COLUMN display_name TEXT DEFAULT ''",
			"ALTER TABLE users ADD COLUMN email TEXT DEFAULT ''",
		}
		for _, stmt := range statements {
			// Ignore errors for ALTER TABLE as column may already exist
			//nolint:errcheck // Intentionally ignoring error - column may already exist
			s.db.Exec(stmt)
		}
	}

	// Apply migrations for schema version 6 (rename manage_privileges to manage_permissions)
	if currentVersion < 6 {
		migrationV6 := `
        UPDATE group_admin_permissions SET permission = 'manage_permissions'
            WHERE permission = 'manage_privileges';

        -- Recreate table with updated CHECK constraint
        CREATE TABLE IF NOT EXISTS group_admin_permissions_new (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            group_id INTEGER NOT NULL
                REFERENCES user_groups(id) ON DELETE CASCADE,
            permission TEXT NOT NULL CHECK (permission IN (
                'manage_connections', 'manage_groups',
                'manage_permissions', 'manage_users',
                'manage_token_scopes', 'manage_blackouts'
            )),
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(group_id, permission)
        );
        INSERT INTO group_admin_permissions_new (id, group_id, permission, created_at)
            SELECT id, group_id, permission, created_at
            FROM group_admin_permissions;
        DROP TABLE group_admin_permissions;
        ALTER TABLE group_admin_permissions_new RENAME TO group_admin_permissions;
        CREATE INDEX IF NOT EXISTS idx_admin_perms_group ON group_admin_permissions(group_id);
        CREATE INDEX IF NOT EXISTS idx_admin_perms_perm ON group_admin_permissions(permission);
        `
		_, err = s.db.Exec(migrationV6)
		if err != nil {
			return fmt.Errorf("failed to apply schema migration v6: %w", err)
		}
	}

	// Apply migrations for schema version 7 (remove stale admin MCP privilege identifiers)
	if currentVersion < 7 {
		migrationV7 := `
        DELETE FROM mcp_privilege_identifiers WHERE identifier IN (
            'create_group', 'update_group', 'delete_group', 'list_groups',
            'add_group_member', 'remove_group_member',
            'grant_mcp_privilege', 'revoke_mcp_privilege',
            'grant_connection_privilege', 'revoke_connection_privilege',
            'list_privileges',
            'set_token_scope', 'get_token_scope', 'clear_token_scope',
            'set_superuser', 'list_users', 'get_user_privileges'
        );
        `
		_, err = s.db.Exec(migrationV7)
		if err != nil {
			return fmt.Errorf("failed to apply schema migration v7: %w", err)
		}
	}

	// Apply migrations for schema version 8 (remove obsolete privilege identifiers)
	if currentVersion < 8 {
		migrationV8 := `
        DELETE FROM mcp_privilege_identifiers WHERE identifier IN (
            'authenticate_user'
        );
        `
		_, err = s.db.Exec(migrationV8)
		if err != nil {
			return fmt.Errorf("failed to apply schema migration v8: %w", err)
		}
	}

	// Apply migrations for schema version 9 (add manage_blackouts permission)
	if currentVersion < 9 {
		migrationV9 := `
        -- Recreate table with updated CHECK constraint including manage_blackouts
        CREATE TABLE IF NOT EXISTS group_admin_permissions_new (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            group_id INTEGER NOT NULL
                REFERENCES user_groups(id) ON DELETE CASCADE,
            permission TEXT NOT NULL CHECK (permission IN (
                'manage_connections', 'manage_groups',
                'manage_permissions', 'manage_users',
                'manage_token_scopes', 'manage_blackouts'
            )),
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(group_id, permission)
        );
        INSERT INTO group_admin_permissions_new (id, group_id, permission, created_at)
            SELECT id, group_id, permission, created_at
            FROM group_admin_permissions;
        DROP TABLE group_admin_permissions;
        ALTER TABLE group_admin_permissions_new RENAME TO group_admin_permissions;
        CREATE INDEX IF NOT EXISTS idx_admin_perms_group ON group_admin_permissions(group_id);
        CREATE INDEX IF NOT EXISTS idx_admin_perms_perm ON group_admin_permissions(permission);
        `
		_, err = s.db.Exec(migrationV9)
		if err != nil {
			return fmt.Errorf("failed to apply schema migration v9: %w", err)
		}
	}

	// Set schema version
	_, err = s.db.Exec("INSERT OR REPLACE INTO schema_version (version) VALUES (?)", schemaVersion)
	return err
}

// Close closes the database connection
func (s *AuthStore) Close() error {
	return s.db.Close()
}

// =============================================================================
// User Management
// =============================================================================

// CreateUser creates a new user
func (s *AuthStore) CreateUser(username, password, annotation, displayName, email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
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

	var user StoredUser
	var displayName sql.NullString
	var email sql.NullString
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, created_at, last_login, enabled, annotation, display_name, email, failed_attempts, is_superuser
         FROM users WHERE username = ?`,
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt,
		&user.LastLogin, &user.Enabled, &user.Annotation, &displayName, &email, &user.FailedAttempts, &user.IsSuperuser)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if displayName.Valid {
		user.DisplayName = displayName.String
	}
	if email.Valid {
		user.Email = email.String
	}

	return &user, nil
}

// GetUserByID retrieves a user by their ID
func (s *AuthStore) GetUserByID(id int64) (*StoredUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var user StoredUser
	var displayName sql.NullString
	var email sql.NullString
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, created_at, last_login, enabled, annotation, display_name, email, failed_attempts, is_superuser
         FROM users WHERE id = ?`,
		id,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt,
		&user.LastLogin, &user.Enabled, &user.Annotation, &displayName, &email, &user.FailedAttempts, &user.IsSuperuser)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by ID: %w", err)
	}

	if displayName.Valid {
		user.DisplayName = displayName.String
	}
	if email.Valid {
		user.Email = email.String
	}

	return &user, nil
}

// UpdateUser updates a user's password, annotation, display name, and/or email
func (s *AuthStore) UpdateUser(username, newPassword, newAnnotation, newDisplayName, newEmail string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if newPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
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

// DeleteUser removes a user
func (s *AuthStore) DeleteUser(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM users WHERE username = ?", username)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
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

	return nil
}

// ListUsers returns all users
func (s *AuthStore) ListUsers() ([]*StoredUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, username, password_hash, created_at, last_login, enabled, annotation, display_name, email, failed_attempts, is_superuser
         FROM users ORDER BY username`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*StoredUser
	for rows.Next() {
		var user StoredUser
		var displayName sql.NullString
		var email sql.NullString
		if err := rows.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt,
			&user.LastLogin, &user.Enabled, &user.Annotation, &displayName, &email, &user.FailedAttempts, &user.IsSuperuser); err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		if displayName.Valid {
			user.DisplayName = displayName.String
		}
		if email.Valid {
			user.Email = email.String
		}
		users = append(users, &user)
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
		`SELECT id, username, password_hash, enabled, failed_attempts FROM users WHERE username = ?`,
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Enabled, &user.FailedAttempts)

	if err == sql.ErrNoRows {
		return "", time.Time{}, fmt.Errorf("invalid username or password")
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("authentication error: %w", err)
	}

	if !user.Enabled {
		return "", time.Time{}, fmt.Errorf("user account is disabled")
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
		}

		return "", time.Time{}, fmt.Errorf("invalid username or password")
	}

	// Generate session token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to generate session token: %w", err)
	}
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	// Set expiration
	expiration := time.Now().Add(DefaultSessionExpiry)

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
		return "", fmt.Errorf("session has expired")
	}

	// Verify user is still enabled
	user, err := s.GetUser(session.Username)
	if err != nil || user == nil {
		s.sessions.Delete(tokenHash)
		return "", fmt.Errorf("user not found")
	}
	if !user.Enabled {
		s.sessions.Delete(tokenHash)
		return "", fmt.Errorf("user account is disabled")
	}

	return session.Username, nil
}

// InvalidateSession removes a session token
func (s *AuthStore) InvalidateSession(token string) {
	s.sessions.Delete(GetTokenHashByRawToken(token))
}

// =============================================================================
// Service Token Management (Admin-created)
// =============================================================================

// CreateServiceToken creates a new service token
// Returns the raw token (only shown once) and the stored token info
func (s *AuthStore) CreateServiceToken(annotation string, expiry *time.Time, database string, isSuperuser bool) (string, *StoredToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", nil, fmt.Errorf("failed to generate token: %w", err)
	}
	rawToken := base64.URLEncoding.EncodeToString(tokenBytes)

	// Hash the token for storage
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	// Insert into database
	result, err := s.db.Exec(
		`INSERT INTO tokens (token_hash, token_type, expires_at, annotation, database, is_superuser)
         VALUES (?, ?, ?, ?, ?, ?)`,
		tokenHash, TokenTypeService, expiry, annotation, database, isSuperuser,
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create token: %w", err)
	}

	//nolint:errcheck // SQLite always supports LastInsertId
	id, _ := result.LastInsertId()
	token := &StoredToken{
		ID:          id,
		TokenHash:   tokenHash,
		TokenType:   TokenTypeService,
		ExpiresAt:   expiry,
		Annotation:  annotation,
		CreatedAt:   time.Now(),
		Database:    database,
		IsSuperuser: isSuperuser,
	}

	return rawToken, token, nil
}

// =============================================================================
// User Token Management (User-created)
// =============================================================================

// CreateUserToken creates a new user token
// Returns the raw token (only shown once) and the stored token info
func (s *AuthStore) CreateUserToken(username, annotation string, requestedDays int) (string, *StoredToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get user ID
	var userID int64
	err := s.db.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&userID)
	if err == sql.ErrNoRows {
		return "", nil, fmt.Errorf("user '%s' not found", username)
	}
	if err != nil {
		return "", nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Calculate expiry
	var expiry *time.Time
	if s.maxUserTokenDays > 0 {
		// Apply max limit
		days := requestedDays
		if days <= 0 || days > s.maxUserTokenDays {
			days = s.maxUserTokenDays
		}
		exp := time.Now().AddDate(0, 0, days)
		expiry = &exp
	} else if requestedDays > 0 {
		// No max limit, use requested days
		exp := time.Now().AddDate(0, 0, requestedDays)
		expiry = &exp
	}
	// else: no expiry (unlimited)

	// Generate random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", nil, fmt.Errorf("failed to generate token: %w", err)
	}
	rawToken := base64.URLEncoding.EncodeToString(tokenBytes)

	// Hash the token for storage
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	// Insert into database
	result, err := s.db.Exec(
		`INSERT INTO tokens (token_hash, token_type, owner_id, expires_at, annotation)
         VALUES (?, ?, ?, ?, ?)`,
		tokenHash, TokenTypeUser, userID, expiry, annotation,
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create token: %w", err)
	}

	//nolint:errcheck // SQLite always supports LastInsertId
	id, _ := result.LastInsertId()
	token := &StoredToken{
		ID:         id,
		TokenHash:  tokenHash,
		TokenType:  TokenTypeUser,
		OwnerID:    &userID,
		ExpiresAt:  expiry,
		Annotation: annotation,
		CreatedAt:  time.Now(),
	}

	return rawToken, token, nil
}

// ListUserTokens lists all tokens owned by a user
func (s *AuthStore) ListUserTokens(username string) ([]*StoredToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT t.id, t.token_hash, t.token_type, t.owner_id, t.expires_at, t.annotation, t.created_at, t.database, t.is_superuser
         FROM tokens t
         JOIN users u ON t.owner_id = u.id
         WHERE u.username = ? AND t.token_type = ?
         ORDER BY t.created_at DESC`,
		username, TokenTypeUser,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list user tokens: %w", err)
	}
	defer rows.Close()

	return s.scanTokens(rows)
}

// DeleteUserToken deletes a user token (only if owned by the user)
func (s *AuthStore) DeleteUserToken(username string, tokenID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		`DELETE FROM tokens
         WHERE id = ? AND token_type = ? AND owner_id = (SELECT id FROM users WHERE username = ?)`,
		tokenID, TokenTypeUser, username,
	)
	if err != nil {
		return fmt.Errorf("failed to delete token: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("token not found or not owned by user")
	}

	return nil
}

// =============================================================================
// Token Validation (all token types)
// =============================================================================

// ValidateToken checks if a token (service or user) is valid
// Returns the token info if valid
func (s *AuthStore) ValidateToken(rawToken string) (*StoredToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Hash the provided token
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	var token StoredToken
	var annotation sql.NullString
	var database sql.NullString
	var isSuperuser sql.NullBool
	err := s.db.QueryRow(
		`SELECT id, token_hash, token_type, owner_id, expires_at, annotation, created_at, database, is_superuser
         FROM tokens WHERE token_hash = ?`,
		tokenHash,
	).Scan(&token.ID, &token.TokenHash, &token.TokenType, &token.OwnerID,
		&token.ExpiresAt, &annotation, &token.CreatedAt, &database, &isSuperuser)

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
	if isSuperuser.Valid {
		token.IsSuperuser = isSuperuser.Bool
	}

	// Check expiration
	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("token has expired")
	}

	// For user tokens, verify the owning user is still enabled
	if token.TokenType == TokenTypeUser && token.OwnerID != nil {
		var enabled bool
		err := s.db.QueryRow("SELECT enabled FROM users WHERE id = ?", *token.OwnerID).Scan(&enabled)
		if err != nil || !enabled {
			return nil, fmt.Errorf("token owner is disabled")
		}
	}

	return &token, nil
}

// =============================================================================
// Service Token Management (for CLI commands)
// =============================================================================

// ListAllTokens lists all tokens regardless of type
func (s *AuthStore) ListAllTokens() ([]*StoredToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, token_hash, token_type, owner_id, expires_at, annotation, created_at, database, is_superuser
         FROM tokens ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list tokens: %w", err)
	}
	defer rows.Close()

	return s.scanTokens(rows)
}

// ListServiceTokens lists all service tokens
func (s *AuthStore) ListServiceTokens() ([]*StoredToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, token_hash, token_type, owner_id, expires_at, annotation, created_at, database, is_superuser
         FROM tokens WHERE token_type = ?
         ORDER BY created_at DESC`,
		TokenTypeService,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list service tokens: %w", err)
	}
	defer rows.Close()

	return s.scanTokens(rows)
}

// DeleteServiceToken deletes a service token by ID or hash prefix
func (s *AuthStore) DeleteServiceToken(identifier string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Try by ID first
	result, err := s.db.Exec(
		"DELETE FROM tokens WHERE id = ? AND token_type = ?",
		identifier, TokenTypeService,
	)
	if err == nil {
		//nolint:errcheck // RowsAffected error non-critical, we try hash prefix next
		if rows, _ := result.RowsAffected(); rows > 0 {
			return nil
		}
	}

	// Try by hash prefix
	if len(identifier) >= 8 {
		result, err = s.db.Exec(
			"DELETE FROM tokens WHERE token_hash LIKE ? AND token_type = ?",
			identifier+"%", TokenTypeService,
		)
		if err != nil {
			return fmt.Errorf("failed to delete token: %w", err)
		}
		//nolint:errcheck // RowsAffected error non-critical for conditional check
		if rows, _ := result.RowsAffected(); rows > 0 {
			return nil
		}
	}

	return fmt.Errorf("token not found")
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
	//nolint:errcheck // Err check non-critical for cleanup operation
	_ = rows.Err()
	rows.Close()

	// Delete expired tokens
	result, err := s.db.Exec(
		"DELETE FROM tokens WHERE expires_at IS NOT NULL AND expires_at < ?",
		time.Now(),
	)
	if err != nil {
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
		var isSuperuser sql.NullBool
		if err := rows.Scan(&token.ID, &token.TokenHash, &token.TokenType, &token.OwnerID,
			&token.ExpiresAt, &annotation, &token.CreatedAt, &database, &isSuperuser); err != nil {
			return nil, fmt.Errorf("failed to scan token: %w", err)
		}
		if annotation.Valid {
			token.Annotation = annotation.String
		}
		if database.Valid {
			token.Database = database.String
		}
		if isSuperuser.Valid {
			token.IsSuperuser = isSuperuser.Bool
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

// ServiceTokenCount returns the number of service tokens
func (s *AuthStore) ServiceTokenCount() int {
	var count int
	//nolint:errcheck // Returns 0 on error, which is acceptable
	s.db.QueryRow("SELECT COUNT(*) FROM tokens WHERE token_type = ?", TokenTypeService).Scan(&count)
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
