/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"database/sql"
	"fmt"
	"log"
	"sort"
)

// Migration represents a database schema migration
type Migration struct {
	Version     int
	Description string
	Up          func(*sql.DB) error
}

// SchemaManager handles database schema migrations
type SchemaManager struct {
	migrations []Migration
}

// NewSchemaManager creates a new schema manager
func NewSchemaManager() *SchemaManager {
	sm := &SchemaManager{
		migrations: []Migration{},
	}
	sm.registerMigrations()
	return sm
}

// registerMigrations registers all available migrations
func (sm *SchemaManager) registerMigrations() {
	// Migration 1: Initial schema with core tables
	sm.migrations = append(sm.migrations, Migration{
		Version:     1,
		Description: "Create schema_version table",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`
                CREATE TABLE IF NOT EXISTS schema_version (
                    version INTEGER PRIMARY KEY,
                    description TEXT NOT NULL,
                    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
                );

                COMMENT ON TABLE schema_version IS
                    'Tracks applied schema migrations for version control';
                COMMENT ON COLUMN schema_version.version IS
                    'Migration version number, used as primary key';
                COMMENT ON COLUMN schema_version.description IS
                    'Human-readable description of what the migration does';
                COMMENT ON COLUMN schema_version.applied_at IS
                    'Timestamp when the migration was applied';
            `)
			if err != nil {
				return fmt.Errorf("failed to create schema_version table: %w", err)
			}
			return nil
		},
	})

	// Migration 2: Create monitored_connections table
	sm.migrations = append(sm.migrations, Migration{
		Version:     2,
		Description: "Create monitored_connections table",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`
                CREATE TABLE IF NOT EXISTS monitored_connections (
                    id SERIAL PRIMARY KEY,
                    name VARCHAR(255) NOT NULL,
                    host VARCHAR(255) NOT NULL,
                    hostaddr VARCHAR(255),
                    port INTEGER NOT NULL DEFAULT 5432,
                    database_name VARCHAR(255) NOT NULL,
                    username VARCHAR(255) NOT NULL,
                    password_encrypted TEXT,
                    sslmode VARCHAR(50),
                    sslcert TEXT,
                    sslkey TEXT,
                    sslrootcert TEXT,
                    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
                    owner_token VARCHAR(255),
                    is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
                    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    CONSTRAINT chk_port CHECK (port > 0 AND port <= 65535),
                    CONSTRAINT chk_owner_token CHECK (
                        (is_shared = TRUE) OR
                        (is_shared = FALSE AND owner_token IS NOT NULL)
                    )
                );

                COMMENT ON TABLE monitored_connections IS
                    'PostgreSQL server connections that can be monitored by the collector';
                COMMENT ON COLUMN monitored_connections.id IS
                    'Unique identifier for the connection';
                COMMENT ON COLUMN monitored_connections.name IS
                    'User-friendly name for the connection';
                COMMENT ON COLUMN monitored_connections.host IS
                    'Hostname or IP address of the PostgreSQL server';
                COMMENT ON COLUMN monitored_connections.hostaddr IS
                    'IP address to bypass DNS lookup (optional)';
                COMMENT ON COLUMN monitored_connections.port IS
                    'Port number for PostgreSQL connection (default 5432)';
                COMMENT ON COLUMN monitored_connections.database_name IS
                    'Maintenance database name for initial connection';
                COMMENT ON COLUMN monitored_connections.username IS
                    'Username for PostgreSQL authentication';
                COMMENT ON COLUMN monitored_connections.password_encrypted IS
                    'Encrypted password for authentication';
                COMMENT ON COLUMN monitored_connections.sslmode IS
                    'SSL mode (disable, allow, prefer, require, verify-ca, verify-full)';
                COMMENT ON COLUMN monitored_connections.sslcert IS
                    'Path to client SSL certificate';
                COMMENT ON COLUMN monitored_connections.sslkey IS
                    'Path to client SSL key';
                COMMENT ON COLUMN monitored_connections.sslrootcert IS
                    'Path to root SSL certificate';
                COMMENT ON COLUMN monitored_connections.is_shared IS
                    'Whether the connection is shared among users or private';
                COMMENT ON COLUMN monitored_connections.owner_token IS
                    'Token or username that owns this connection (required for non-shared)';
                COMMENT ON COLUMN monitored_connections.is_monitored IS
                    'Whether this connection is actively being monitored';
                COMMENT ON COLUMN monitored_connections.created_at IS
                    'Timestamp when the connection was created';
                COMMENT ON COLUMN monitored_connections.updated_at IS
                    'Timestamp when the connection was last updated';
                COMMENT ON CONSTRAINT chk_port ON monitored_connections IS
                    'Ensures port is in valid range (1-65535)';
                COMMENT ON CONSTRAINT chk_owner_token ON monitored_connections IS
                    'Ensures non-shared connections have an owner_token';
            `)
			if err != nil {
				return fmt.Errorf("failed to create monitored_connections table: %w", err)
			}
			return nil
		},
	})

	// Migration 3: Create indexes on monitored_connections
	sm.migrations = append(sm.migrations, Migration{
		Version:     3,
		Description: "Create indexes on monitored_connections table",
		Up: func(db *sql.DB) error {
			indexes := []struct {
				name    string
				sql     string
				comment string
			}{
				{
					"idx_monitored_connections_owner_token",
					`CREATE INDEX IF NOT EXISTS idx_monitored_connections_owner_token
                     ON monitored_connections(owner_token)`,
					"Index for fast lookup of connections by owner token",
				},
				{
					"idx_monitored_connections_is_monitored",
					`CREATE INDEX IF NOT EXISTS idx_monitored_connections_is_monitored
                     ON monitored_connections(is_monitored) WHERE is_monitored = TRUE`,
					"Partial index for efficiently finding actively monitored connections",
				},
				{
					"idx_monitored_connections_name",
					`CREATE INDEX IF NOT EXISTS idx_monitored_connections_name
                     ON monitored_connections(name)`,
					"Index for fast lookup of connections by name",
				},
			}

			for _, idx := range indexes {
				if _, err := db.Exec(idx.sql); err != nil {
					return fmt.Errorf("failed to create index %s: %w", idx.name, err)
				}
				if _, err := db.Exec(fmt.Sprintf("COMMENT ON INDEX %s IS '%s'", idx.name, idx.comment)); err != nil {
					return fmt.Errorf("failed to add comment on index %s: %w", idx.name, err)
				}
			}
			return nil
		},
	})

	// Migration 4: Create user_accounts table
	sm.migrations = append(sm.migrations, Migration{
		Version:     4,
		Description: "Create user_accounts table",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`
                CREATE TABLE IF NOT EXISTS user_accounts (
                    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                    username TEXT NOT NULL UNIQUE,
                    email TEXT NOT NULL,
                    full_name TEXT NOT NULL,
                    password_hash TEXT NOT NULL,
                    password_expiry TIMESTAMP,
                    is_superuser BOOLEAN NOT NULL DEFAULT FALSE,
                    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    CONSTRAINT chk_username_not_empty CHECK (username <> ''),
                    CONSTRAINT chk_email_not_empty CHECK (email <> ''),
                    CONSTRAINT chk_password_hash_not_empty CHECK (password_hash <> '')
                );

                COMMENT ON TABLE user_accounts IS
                    'User accounts for authentication and authorization';
                COMMENT ON COLUMN user_accounts.id IS
                    'Unique identifier for the user account';
                COMMENT ON COLUMN user_accounts.username IS
                    'Unique username for login';
                COMMENT ON COLUMN user_accounts.email IS
                    'Email address for the user';
                COMMENT ON COLUMN user_accounts.full_name IS
                    'Full name of the user';
                COMMENT ON COLUMN user_accounts.password_hash IS
                    'SHA256 hash of the user password';
                COMMENT ON COLUMN user_accounts.password_expiry IS
                    'Timestamp when the password expires (optional)';
                COMMENT ON COLUMN user_accounts.is_superuser IS
                    'Whether the user has superuser privileges';
                COMMENT ON COLUMN user_accounts.created_at IS
                    'Timestamp when the account was created';
                COMMENT ON COLUMN user_accounts.updated_at IS
                    'Timestamp when the account was last updated';
                COMMENT ON CONSTRAINT chk_username_not_empty ON user_accounts IS
                    'Ensures username is not empty';
                COMMENT ON CONSTRAINT chk_email_not_empty ON user_accounts IS
                    'Ensures email is not empty';
                COMMENT ON CONSTRAINT chk_password_hash_not_empty ON user_accounts IS
                    'Ensures password_hash is not empty';
            `)
			if err != nil {
				return fmt.Errorf("failed to create user_accounts table: %w", err)
			}
			return nil
		},
	})

	// Migration 5: Create user_tokens table
	sm.migrations = append(sm.migrations, Migration{
		Version:     5,
		Description: "Create user_tokens table",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`
                CREATE TABLE IF NOT EXISTS user_tokens (
                    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                    user_id INTEGER NOT NULL,
                    token_hash TEXT NOT NULL UNIQUE,
                    expires_at TIMESTAMP NOT NULL,
                    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    CONSTRAINT fk_user_tokens_user_id
                        FOREIGN KEY (user_id)
                        REFERENCES user_accounts(id)
                        ON DELETE CASCADE,
                    CONSTRAINT chk_token_hash_not_empty CHECK (token_hash <> ''),
                    CONSTRAINT chk_expires_at_future CHECK (expires_at > created_at)
                );

                COMMENT ON TABLE user_tokens IS
                    'Authentication tokens issued to users for API access';
                COMMENT ON COLUMN user_tokens.id IS
                    'Unique identifier for the token';
                COMMENT ON COLUMN user_tokens.user_id IS
                    'Reference to the user account that owns this token';
                COMMENT ON COLUMN user_tokens.token_hash IS
                    'Hash of the authentication token';
                COMMENT ON COLUMN user_tokens.expires_at IS
                    'Timestamp when the token expires';
                COMMENT ON COLUMN user_tokens.created_at IS
                    'Timestamp when the token was created';
                COMMENT ON CONSTRAINT fk_user_tokens_user_id ON user_tokens IS
                    'Foreign key to user_accounts, cascade delete when user is deleted';
                COMMENT ON CONSTRAINT chk_token_hash_not_empty ON user_tokens IS
                    'Ensures token_hash is not empty';
                COMMENT ON CONSTRAINT chk_expires_at_future ON user_tokens IS
                    'Ensures expiration time is in the future when token is created';
            `)
			if err != nil {
				return fmt.Errorf("failed to create user_tokens table: %w", err)
			}
			return nil
		},
	})

	// Migration 6: Create service_tokens table
	sm.migrations = append(sm.migrations, Migration{
		Version:     6,
		Description: "Create service_tokens table",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`
                CREATE TABLE IF NOT EXISTS service_tokens (
                    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                    name TEXT NOT NULL UNIQUE,
                    token_hash TEXT NOT NULL UNIQUE,
                    expires_at TIMESTAMP,
                    is_superuser BOOLEAN NOT NULL DEFAULT FALSE,
                    note TEXT,
                    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    CONSTRAINT chk_name_not_empty CHECK (name <> ''),
                    CONSTRAINT chk_token_hash_not_empty CHECK (token_hash <> '')
                );

                COMMENT ON TABLE service_tokens IS
                    'Authentication tokens for service accounts and automated systems';
                COMMENT ON COLUMN service_tokens.id IS
                    'Unique identifier for the service token';
                COMMENT ON COLUMN service_tokens.name IS
                    'Unique name identifying the service or purpose';
                COMMENT ON COLUMN service_tokens.token_hash IS
                    'Hash of the authentication token';
                COMMENT ON COLUMN service_tokens.expires_at IS
                    'Timestamp when the token expires (NULL for permanent tokens)';
                COMMENT ON COLUMN service_tokens.is_superuser IS
                    'Whether the service token has superuser privileges';
                COMMENT ON COLUMN service_tokens.note IS
                    'Optional note describing the purpose of the service token';
                COMMENT ON COLUMN service_tokens.created_at IS
                    'Timestamp when the token was created';
                COMMENT ON COLUMN service_tokens.updated_at IS
                    'Timestamp when the token was last updated';
                COMMENT ON CONSTRAINT chk_name_not_empty ON service_tokens IS
                    'Ensures name is not empty';
                COMMENT ON CONSTRAINT chk_token_hash_not_empty ON service_tokens IS
                    'Ensures token_hash is not empty';
            `)
			if err != nil {
				return fmt.Errorf("failed to create service_tokens table: %w", err)
			}
			return nil
		},
	})

	// Migration 7: Create indexes on user_accounts
	sm.migrations = append(sm.migrations, Migration{
		Version:     7,
		Description: "Create indexes on user_accounts table",
		Up: func(db *sql.DB) error {
			indexes := []struct {
				name    string
				sql     string
				comment string
			}{
				{
					"idx_user_accounts_username",
					`CREATE INDEX IF NOT EXISTS idx_user_accounts_username
                     ON user_accounts(username)`,
					"Index for fast lookup of users by username",
				},
				{
					"idx_user_accounts_email",
					`CREATE INDEX IF NOT EXISTS idx_user_accounts_email
                     ON user_accounts(email)`,
					"Index for fast lookup of users by email address",
				},
			}

			for _, idx := range indexes {
				if _, err := db.Exec(idx.sql); err != nil {
					return fmt.Errorf("failed to create index %s: %w", idx.name, err)
				}
				if _, err := db.Exec(fmt.Sprintf("COMMENT ON INDEX %s IS '%s'", idx.name, idx.comment)); err != nil {
					return fmt.Errorf("failed to add comment on index %s: %w", idx.name, err)
				}
			}
			return nil
		},
	})

	// Migration 8: Create indexes on user_tokens
	sm.migrations = append(sm.migrations, Migration{
		Version:     8,
		Description: "Create indexes on user_tokens table",
		Up: func(db *sql.DB) error {
			indexes := []struct {
				name    string
				sql     string
				comment string
			}{
				{
					"idx_user_tokens_user_id",
					`CREATE INDEX IF NOT EXISTS idx_user_tokens_user_id
                     ON user_tokens(user_id)`,
					"Index for fast lookup of tokens by user (foreign key index)",
				},
				{
					"idx_user_tokens_token_hash",
					`CREATE INDEX IF NOT EXISTS idx_user_tokens_token_hash
                     ON user_tokens(token_hash)`,
					"Index for fast authentication by token hash",
				},
				{
					"idx_user_tokens_expires_at",
					`CREATE INDEX IF NOT EXISTS idx_user_tokens_expires_at
                     ON user_tokens(expires_at)`,
					"Index for efficiently finding and cleaning up expired tokens",
				},
			}

			for _, idx := range indexes {
				if _, err := db.Exec(idx.sql); err != nil {
					return fmt.Errorf("failed to create index %s: %w", idx.name, err)
				}
				if _, err := db.Exec(fmt.Sprintf("COMMENT ON INDEX %s IS '%s'", idx.name, idx.comment)); err != nil {
					return fmt.Errorf("failed to add comment on index %s: %w", idx.name, err)
				}
			}
			return nil
		},
	})

	// Migration 9: Create indexes on service_tokens
	sm.migrations = append(sm.migrations, Migration{
		Version:     9,
		Description: "Create indexes on service_tokens table",
		Up: func(db *sql.DB) error {
			indexes := []struct {
				name    string
				sql     string
				comment string
			}{
				{
					"idx_service_tokens_name",
					`CREATE INDEX IF NOT EXISTS idx_service_tokens_name
                     ON service_tokens(name)`,
					"Index for fast lookup of service tokens by name",
				},
				{
					"idx_service_tokens_token_hash",
					`CREATE INDEX IF NOT EXISTS idx_service_tokens_token_hash
                     ON service_tokens(token_hash)`,
					"Index for fast authentication by token hash",
				},
				{
					"idx_service_tokens_expires_at",
					`CREATE INDEX IF NOT EXISTS idx_service_tokens_expires_at
                     ON service_tokens(expires_at)`,
					"Index for efficiently finding and cleaning up expired tokens",
				},
			}

			for _, idx := range indexes {
				if _, err := db.Exec(idx.sql); err != nil {
					return fmt.Errorf("failed to create index %s: %w", idx.name, err)
				}
				if _, err := db.Exec(fmt.Sprintf("COMMENT ON INDEX %s IS '%s'", idx.name, idx.comment)); err != nil {
					return fmt.Errorf("failed to add comment on index %s: %w", idx.name, err)
				}
			}
			return nil
		},
	})
}

// Migrate applies all pending migrations
func (sm *SchemaManager) Migrate(db *sql.DB) error {
	log.Println("Starting schema migration...")

	// Sort migrations by version
	sort.Slice(sm.migrations, func(i, j int) bool {
		return sm.migrations[i].Version < sm.migrations[j].Version
	})

	// Get current schema version
	currentVersion, err := sm.getCurrentVersion(db)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	log.Printf("Current schema version: %d", currentVersion)

	// Apply each pending migration
	appliedCount := 0
	for _, migration := range sm.migrations {
		if migration.Version <= currentVersion {
			continue
		}

		log.Printf("Applying migration %d: %s", migration.Version, migration.Description)

		// Start a transaction for the migration
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration %d: %w",
				migration.Version, err)
		}

		// Apply the migration
		if err := migration.Up(db); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("Failed to rollback transaction: %v", rbErr)
			}
			return fmt.Errorf("failed to apply migration %d: %w", migration.Version, err)
		}

		// Record the migration in schema_version
		_, err = db.Exec(`
            INSERT INTO schema_version (version, description)
            VALUES ($1, $2)
            ON CONFLICT (version) DO NOTHING
        `, migration.Version, migration.Description)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("Failed to rollback transaction: %v", rbErr)
			}
			return fmt.Errorf("failed to record migration %d: %w", migration.Version, err)
		}

		// Commit the transaction
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %d: %w", migration.Version, err)
		}

		log.Printf("Successfully applied migration %d", migration.Version)
		appliedCount++
	}

	if appliedCount == 0 {
		log.Println("Schema is up to date")
	} else {
		log.Printf("Applied %d migration(s)", appliedCount)
	}

	return nil
}

// getCurrentVersion returns the current schema version
func (sm *SchemaManager) getCurrentVersion(db *sql.DB) (int, error) {
	var version int
	err := db.QueryRow(`
        SELECT COALESCE(MAX(version), 0)
        FROM schema_version
    `).Scan(&version)

	if err != nil {
		// If the table doesn't exist, return version 0
		if err == sql.ErrNoRows {
			return 0, nil
		}
		// Check if the error is because the table doesn't exist
		if isTableNotExistError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to query schema version: %w", err)
	}

	return version, nil
}

// isTableNotExistError checks if an error is due to a non-existent table
func isTableNotExistError(err error) bool {
	if err == nil {
		return false
	}
	// PostgreSQL error code 42P01 is "undefined_table"
	return err.Error() == `pq: relation "schema_version" does not exist`
}

// GetMigrationStatus returns information about migration status
func (sm *SchemaManager) GetMigrationStatus(db *sql.DB) ([]MigrationStatus, error) {
	currentVersion, err := sm.getCurrentVersion(db)
	if err != nil {
		return nil, fmt.Errorf("failed to get current version: %w", err)
	}

	// Get applied migrations from database
	appliedMigrations := make(map[int]MigrationRecord)
	rows, err := db.Query(`
        SELECT version, description, applied_at
        FROM schema_version
        ORDER BY version
    `)
	if err != nil && !isTableNotExistError(err) {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}

	if rows != nil {
		defer func() {
			if cerr := rows.Close(); cerr != nil {
				log.Printf("Error closing rows: %v", cerr)
			}
		}()

		for rows.Next() {
			var record MigrationRecord
			if err := rows.Scan(&record.Version, &record.Description, &record.AppliedAt); err != nil {
				return nil, fmt.Errorf("failed to scan migration record: %w", err)
			}
			appliedMigrations[record.Version] = record
		}

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("error iterating migrations: %w", err)
		}
	}

	// Build status for each migration
	var statuses []MigrationStatus
	for _, migration := range sm.migrations {
		status := MigrationStatus{
			Version:     migration.Version,
			Description: migration.Description,
			Applied:     migration.Version <= currentVersion,
		}

		if record, ok := appliedMigrations[migration.Version]; ok {
			status.AppliedAt = &record.AppliedAt
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}

// MigrationRecord represents a migration record in the database
type MigrationRecord struct {
	Version     int
	Description string
	AppliedAt   string
}

// MigrationStatus represents the status of a migration
type MigrationStatus struct {
	Version     int
	Description string
	Applied     bool
	AppliedAt   *string
}
