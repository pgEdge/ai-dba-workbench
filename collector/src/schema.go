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
                )
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
                )
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
			indexes := []string{
				`CREATE INDEX IF NOT EXISTS idx_monitored_connections_owner_token
                 ON monitored_connections(owner_token)`,
				`CREATE INDEX IF NOT EXISTS idx_monitored_connections_is_monitored
                 ON monitored_connections(is_monitored) WHERE is_monitored = TRUE`,
				`CREATE INDEX IF NOT EXISTS idx_monitored_connections_name
                 ON monitored_connections(name)`,
			}

			for _, idx := range indexes {
				if _, err := db.Exec(idx); err != nil {
					return fmt.Errorf("failed to create index: %w", err)
				}
			}
			return nil
		},
	})

	// Migration 4: Create probes table
	sm.migrations = append(sm.migrations, Migration{
		Version:     4,
		Description: "Create probes configuration table",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`
                CREATE TABLE IF NOT EXISTS probes (
                    id SERIAL PRIMARY KEY,
                    name VARCHAR(255) NOT NULL UNIQUE,
                    description TEXT,
                    sql_query TEXT NOT NULL,
                    collection_interval INTEGER NOT NULL DEFAULT 60,
                    retention_days INTEGER NOT NULL DEFAULT 7,
                    enabled BOOLEAN NOT NULL DEFAULT TRUE,
                    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    CONSTRAINT chk_collection_interval CHECK (collection_interval > 0),
                    CONSTRAINT chk_retention_days CHECK (retention_days > 0)
                )
            `)
			if err != nil {
				return fmt.Errorf("failed to create probes table: %w", err)
			}
			return nil
		},
	})

	// Migration 5: Create indexes on probes
	sm.migrations = append(sm.migrations, Migration{
		Version:     5,
		Description: "Create indexes on probes table",
		Up: func(db *sql.DB) error {
			indexes := []string{
				`CREATE INDEX IF NOT EXISTS idx_probes_enabled
                 ON probes(enabled) WHERE enabled = TRUE`,
				`CREATE INDEX IF NOT EXISTS idx_probes_name
                 ON probes(name)`,
			}

			for _, idx := range indexes {
				if _, err := db.Exec(idx); err != nil {
					return fmt.Errorf("failed to create index: %w", err)
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
