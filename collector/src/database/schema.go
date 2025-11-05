/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migration represents a database schema migration
type Migration struct {
	Version     int
	Description string
	Up          func(*pgxpool.Conn) error
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
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			_, err := conn.Exec(ctx, `
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

	// Migration 2: Create connections table
	sm.migrations = append(sm.migrations, Migration{
		Version:     2,
		Description: "Create connections table",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			_, err := conn.Exec(ctx, `
                CREATE TABLE IF NOT EXISTS connections (
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
                    owner_username VARCHAR(255),
                    owner_token VARCHAR(255),
                    is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
                    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    CONSTRAINT chk_port CHECK (port > 0 AND port <= 65535),
                    CONSTRAINT chk_owner CHECK (
                        (owner_username IS NOT NULL AND owner_token IS NULL) OR
                        (owner_username IS NULL AND owner_token IS NOT NULL)
                    )
                );

                COMMENT ON TABLE connections IS
                    'PostgreSQL server connections that can be monitored by the collector';
                COMMENT ON COLUMN connections.id IS
                    'Unique identifier for the connection';
                COMMENT ON COLUMN connections.name IS
                    'User-friendly name for the connection';
                COMMENT ON COLUMN connections.host IS
                    'Hostname or IP address of the PostgreSQL server';
                COMMENT ON COLUMN connections.hostaddr IS
                    'IP address to bypass DNS lookup (optional)';
                COMMENT ON COLUMN connections.port IS
                    'Port number for PostgreSQL connection (default 5432)';
                COMMENT ON COLUMN connections.database_name IS
                    'Maintenance database name for initial connection';
                COMMENT ON COLUMN connections.username IS
                    'Username for PostgreSQL authentication';
                COMMENT ON COLUMN connections.password_encrypted IS
                    'Encrypted password for authentication';
                COMMENT ON COLUMN connections.sslmode IS
                    'SSL mode (disable, allow, prefer, require, verify-ca, verify-full)';
                COMMENT ON COLUMN connections.sslcert IS
                    'Path to client SSL certificate';
                COMMENT ON COLUMN connections.sslkey IS
                    'Path to client SSL key';
                COMMENT ON COLUMN connections.sslrootcert IS
                    'Path to root SSL certificate';
                COMMENT ON COLUMN connections.is_shared IS
                    'Whether the connection is shared among users or private';
                COMMENT ON COLUMN connections.owner_username IS
                    'Username of the user who owns this connection (mutually exclusive with owner_token)';
                COMMENT ON COLUMN connections.owner_token IS
                    'Service token that owns this connection (mutually exclusive with owner_username)';
                COMMENT ON COLUMN connections.is_monitored IS
                    'Whether this connection is actively being monitored';
                COMMENT ON COLUMN connections.created_at IS
                    'Timestamp when the connection was created';
                COMMENT ON COLUMN connections.updated_at IS
                    'Timestamp when the connection was last updated';
                COMMENT ON CONSTRAINT chk_port ON connections IS
                    'Ensures port is in valid range (1-65535)';
                COMMENT ON CONSTRAINT chk_owner ON connections IS
                    'Ensures exactly one of owner_username or owner_token is set';
            `)
			if err != nil {
				return fmt.Errorf("failed to create connections table: %w", err)
			}
			return nil
		},
	})

	// Migration 3: Create indexes on connections
	sm.migrations = append(sm.migrations, Migration{
		Version:     3,
		Description: "Create indexes on connections table",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			indexes := []struct {
				name    string
				sql     string
				comment string
			}{
				{
					"idx_connections_owner_username",
					`CREATE INDEX IF NOT EXISTS idx_connections_owner_username
                     ON connections(owner_username)`,
					"Index for fast lookup of connections by owner username",
				},
				{
					"idx_connections_owner_token",
					`CREATE INDEX IF NOT EXISTS idx_connections_owner_token
                     ON connections(owner_token)`,
					"Index for fast lookup of connections by owner token",
				},
				{
					"idx_connections_is_monitored",
					`CREATE INDEX IF NOT EXISTS idx_connections_is_monitored
                     ON connections(is_monitored) WHERE is_monitored = TRUE`,
					"Partial index for efficiently finding actively monitored connections",
				},
				{
					"idx_connections_name",
					`CREATE INDEX IF NOT EXISTS idx_connections_name
                     ON connections(name)`,
					"Index for fast lookup of connections by name",
				},
			}

			for _, idx := range indexes {
				if _, err := conn.Exec(ctx, idx.sql); err != nil {
					return fmt.Errorf("failed to create index %s: %w", idx.name, err)
				}
				if _, err := conn.Exec(ctx, fmt.Sprintf("COMMENT ON INDEX %s IS '%s'", idx.name, idx.comment)); err != nil {
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
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			_, err := conn.Exec(ctx, `
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
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			_, err := conn.Exec(ctx, `
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
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			_, err := conn.Exec(ctx, `
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
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
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
				if _, err := conn.Exec(ctx, idx.sql); err != nil {
					return fmt.Errorf("failed to create index %s: %w", idx.name, err)
				}
				if _, err := conn.Exec(ctx, fmt.Sprintf("COMMENT ON INDEX %s IS '%s'", idx.name, idx.comment)); err != nil {
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
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
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
				if _, err := conn.Exec(ctx, idx.sql); err != nil {
					return fmt.Errorf("failed to create index %s: %w", idx.name, err)
				}
				if _, err := conn.Exec(ctx, fmt.Sprintf("COMMENT ON INDEX %s IS '%s'", idx.name, idx.comment)); err != nil {
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
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
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
				if _, err := conn.Exec(ctx, idx.sql); err != nil {
					return fmt.Errorf("failed to create index %s: %w", idx.name, err)
				}
				if _, err := conn.Exec(ctx, fmt.Sprintf("COMMENT ON INDEX %s IS '%s'", idx.name, idx.comment)); err != nil {
					return fmt.Errorf("failed to add comment on index %s: %w", idx.name, err)
				}
			}
			return nil
		},
	})

	// Migration 10: Create metrics schema and probe_configs table
	sm.migrations = append(sm.migrations, Migration{
		Version:     10,
		Description: "Create metrics schema and probe_configs table",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			// Create metrics schema
			if _, err := conn.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS metrics`); err != nil {
				return fmt.Errorf("failed to create metrics schema: %w", err)
			}
			if _, err := conn.Exec(ctx, `COMMENT ON SCHEMA metrics IS 'Schema for storing monitoring probe metrics data'`); err != nil {
				return fmt.Errorf("failed to add comment on metrics schema: %w", err)
			}

			// Create probe_configs table
			_, err := conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS probe_configs (
					id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
					name TEXT NOT NULL UNIQUE,
					description TEXT NOT NULL,
					collection_interval_seconds INTEGER NOT NULL DEFAULT 60,
					retention_days INTEGER NOT NULL DEFAULT 28,
					is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
					created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					CONSTRAINT chk_name_not_empty CHECK (name <> ''),
					CONSTRAINT chk_collection_interval_positive CHECK (collection_interval_seconds > 0),
					CONSTRAINT chk_retention_days_positive CHECK (retention_days > 0)
				);

				COMMENT ON TABLE probe_configs IS
					'Configuration for monitoring probes';
				COMMENT ON COLUMN probe_configs.id IS
					'Unique identifier for the probe configuration';
				COMMENT ON COLUMN probe_configs.name IS
					'Unique name of the probe';
				COMMENT ON COLUMN probe_configs.description IS
					'Description of what the probe monitors';
				COMMENT ON COLUMN probe_configs.collection_interval_seconds IS
					'How often to run the probe (in seconds)';
				COMMENT ON COLUMN probe_configs.retention_days IS
					'How long to keep collected data (in days)';
				COMMENT ON COLUMN probe_configs.is_enabled IS
					'Whether the probe is currently enabled';
				COMMENT ON COLUMN probe_configs.created_at IS
					'When the probe configuration was created';
				COMMENT ON COLUMN probe_configs.updated_at IS
					'When the probe configuration was last updated';
				COMMENT ON CONSTRAINT chk_name_not_empty ON probe_configs IS
					'Probe name must not be empty';
				COMMENT ON CONSTRAINT chk_collection_interval_positive ON probe_configs IS
					'Collection interval must be positive';
				COMMENT ON CONSTRAINT chk_retention_days_positive ON probe_configs IS
					'Retention days must be positive';
			`)
			if err != nil {
				return fmt.Errorf("failed to create probe_configs table: %w", err)
			}

			// Create indexes on probe_configs
			indexes := []struct {
				name    string
				sql     string
				comment string
			}{
				{
					"idx_probe_configs_name",
					`CREATE INDEX IF NOT EXISTS idx_probe_configs_name
					 ON probe_configs(name)`,
					"Index for fast lookup of probe configurations by name",
				},
				{
					"idx_probe_configs_enabled",
					`CREATE INDEX IF NOT EXISTS idx_probe_configs_enabled
					 ON probe_configs(is_enabled)`,
					"Index for efficiently finding enabled probes",
				},
			}

			for _, idx := range indexes {
				if _, err := conn.Exec(ctx, idx.sql); err != nil {
					return fmt.Errorf("failed to create index %s: %w", idx.name, err)
				}
				if _, err := conn.Exec(ctx, fmt.Sprintf("COMMENT ON INDEX %s IS '%s'", idx.name, idx.comment)); err != nil {
					return fmt.Errorf("failed to add comment on index %s: %w", idx.name, err)
				}
			}

			return nil
		},
	})

	// Migration 11: Create pg_stat_activity metrics table
	sm.migrations = append(sm.migrations, Migration{
		Version:     11,
		Description: "Create pg_stat_activity metrics table",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			// Create the partitioned table for pg_stat_activity metrics
			_, err := conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_activity (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					datid OID,
					datname TEXT,
					pid INTEGER,
					leader_pid INTEGER,
					usesysid OID,
					usename TEXT,
					application_name TEXT,
					client_addr INET,
					client_hostname TEXT,
					client_port INTEGER,
					backend_start TIMESTAMP,
					xact_start TIMESTAMP,
					query_start TIMESTAMP,
					state_change TIMESTAMP,
					wait_event_type TEXT,
					wait_event TEXT,
					state TEXT,
					backend_xid TEXT,
					backend_xmin TEXT,
					query TEXT,
					backend_type TEXT,
					PRIMARY KEY (connection_id, collected_at)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_activity IS
					'Metrics collected from pg_stat_activity view, showing current server activity';
				COMMENT ON COLUMN metrics.pg_stat_activity.connection_id IS
					'ID of the monitored connection from connections table';
				COMMENT ON COLUMN metrics.pg_stat_activity.collected_at IS
					'Timestamp when the metrics were collected';
				COMMENT ON COLUMN metrics.pg_stat_activity.datid IS
					'OID of the database this backend is connected to';
				COMMENT ON COLUMN metrics.pg_stat_activity.datname IS
					'Name of the database this backend is connected to';
				COMMENT ON COLUMN metrics.pg_stat_activity.pid IS
					'Process ID of this backend';
				COMMENT ON COLUMN metrics.pg_stat_activity.leader_pid IS
					'Process ID of the parallel group leader if this is a parallel worker';
				COMMENT ON COLUMN metrics.pg_stat_activity.usesysid IS
					'OID of the user logged into this backend';
				COMMENT ON COLUMN metrics.pg_stat_activity.usename IS
					'Name of the user logged into this backend';
				COMMENT ON COLUMN metrics.pg_stat_activity.application_name IS
					'Name of the application connected to this backend';
				COMMENT ON COLUMN metrics.pg_stat_activity.client_addr IS
					'IP address of the client connected to this backend';
				COMMENT ON COLUMN metrics.pg_stat_activity.client_hostname IS
					'Host name of the client connected to this backend';
				COMMENT ON COLUMN metrics.pg_stat_activity.client_port IS
					'TCP port number that the client is using for communication';
				COMMENT ON COLUMN metrics.pg_stat_activity.backend_start IS
					'Time when this process was started';
				COMMENT ON COLUMN metrics.pg_stat_activity.xact_start IS
					'Time when the backend''s current transaction was started';
				COMMENT ON COLUMN metrics.pg_stat_activity.query_start IS
					'Time when the currently active query was started';
				COMMENT ON COLUMN metrics.pg_stat_activity.state_change IS
					'Time when the state was last changed';
				COMMENT ON COLUMN metrics.pg_stat_activity.wait_event_type IS
					'Type of event the backend is waiting for';
				COMMENT ON COLUMN metrics.pg_stat_activity.wait_event IS
					'Wait event name if backend is waiting';
				COMMENT ON COLUMN metrics.pg_stat_activity.state IS
					'Current overall state of this backend';
				COMMENT ON COLUMN metrics.pg_stat_activity.backend_xid IS
					'Top-level transaction identifier of this backend';
				COMMENT ON COLUMN metrics.pg_stat_activity.backend_xmin IS
					'Current backend''s xmin horizon';
				COMMENT ON COLUMN metrics.pg_stat_activity.query IS
					'Text of this backend''s most recent query';
				COMMENT ON COLUMN metrics.pg_stat_activity.backend_type IS
					'Type of backend';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_activity metrics table: %w", err)
			}

			// Create indexes for efficient querying
			// Note: Indexes on partitioned tables are created on each partition automatically
			indexes := []struct {
				name    string
				sql     string
				comment string
			}{
				{
					"idx_pg_stat_activity_connection_time",
					`CREATE INDEX IF NOT EXISTS idx_pg_stat_activity_connection_time
					 ON metrics.pg_stat_activity(connection_id, collected_at DESC)`,
					"Index for efficiently querying metrics by connection and time range",
				},
				{
					"idx_pg_stat_activity_collected_at",
					`CREATE INDEX IF NOT EXISTS idx_pg_stat_activity_collected_at
					 ON metrics.pg_stat_activity(collected_at DESC)`,
					"Index for efficiently querying metrics by time range",
				},
			}

			for _, idx := range indexes {
				if _, err := conn.Exec(ctx, idx.sql); err != nil {
					return fmt.Errorf("failed to create index %s: %w", idx.name, err)
				}
				// Comments on indexes of partitioned tables must be added per partition
				// or we can skip them for the parent table
				if _, err := conn.Exec(ctx, fmt.Sprintf("COMMENT ON INDEX metrics.%s IS '%s'", idx.name, idx.comment)); err != nil {
					// Log warning but don't fail - index comments on partitioned tables can be tricky
					log.Printf("Warning: failed to add comment on index %s: %v (this may be expected for partitioned tables)", idx.name, err)
				}
			}

			// Insert probe configuration
			_, err = conn.Exec(ctx, `
				INSERT INTO probe_configs (name, description, collection_interval_seconds, retention_days, is_enabled)
				VALUES ('pg_stat_activity', 'Collects current server activity from pg_stat_activity view', 60, 28, TRUE)
				ON CONFLICT (name) DO NOTHING
			`)
			if err != nil {
				return fmt.Errorf("failed to insert pg_stat_activity probe config: %w", err)
			}

			return nil
		},
	})

	// Migration 12: Create pg_stat_all_tables metrics table
	sm.migrations = append(sm.migrations, Migration{
		Version:     12,
		Description: "Create pg_stat_all_tables metrics table",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			// Create the partitioned table for pg_stat_all_tables metrics
			_, err := conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_all_tables (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name TEXT NOT NULL,
					schemaname TEXT,
					relname TEXT,
					seq_scan BIGINT,
					seq_tup_read BIGINT,
					idx_scan BIGINT,
					idx_tup_fetch BIGINT,
					n_tup_ins BIGINT,
					n_tup_upd BIGINT,
					n_tup_del BIGINT,
					n_tup_hot_upd BIGINT,
					n_live_tup BIGINT,
					n_dead_tup BIGINT,
					n_mod_since_analyze BIGINT,
					last_vacuum TIMESTAMP,
					last_autovacuum TIMESTAMP,
					last_analyze TIMESTAMP,
					last_autoanalyze TIMESTAMP,
					vacuum_count BIGINT,
					autovacuum_count BIGINT,
					analyze_count BIGINT,
					autoanalyze_count BIGINT,
					PRIMARY KEY (connection_id, database_name, collected_at)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_all_tables IS
					'Metrics collected from pg_stat_all_tables view, showing table-level statistics per database';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.connection_id IS
					'ID of the monitored connection from connections table';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.collected_at IS
					'Timestamp when the metrics were collected';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.database_name IS
					'Name of the database where these table statistics were collected';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.schemaname IS
					'Name of the schema containing the table';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.relname IS
					'Name of the table';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.seq_scan IS
					'Number of sequential scans initiated on this table';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.seq_tup_read IS
					'Number of live rows fetched by sequential scans';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.idx_scan IS
					'Number of index scans initiated on this table';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.idx_tup_fetch IS
					'Number of live rows fetched by index scans';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.n_tup_ins IS
					'Number of rows inserted';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.n_tup_upd IS
					'Number of rows updated';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.n_tup_del IS
					'Number of rows deleted';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.n_tup_hot_upd IS
					'Number of rows HOT updated (i.e., with no separate index update required)';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.n_live_tup IS
					'Estimated number of live rows';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.n_dead_tup IS
					'Estimated number of dead rows';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.n_mod_since_analyze IS
					'Estimated number of rows modified since this table was last analyzed';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.last_vacuum IS
					'Time of the last vacuum run on this table (not including VACUUM FULL)';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.last_autovacuum IS
					'Time of the last autovacuum run on this table';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.last_analyze IS
					'Time of the last analyze run on this table';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.last_autoanalyze IS
					'Time of the last autoanalyze run on this table';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.vacuum_count IS
					'Number of times this table has been manually vacuumed';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.autovacuum_count IS
					'Number of times this table has been vacuumed by the autovacuum daemon';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.analyze_count IS
					'Number of times this table has been manually analyzed';
				COMMENT ON COLUMN metrics.pg_stat_all_tables.autoanalyze_count IS
					'Number of times this table has been analyzed by the autovacuum daemon';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_all_tables metrics table: %w", err)
			}

			// Create indexes for efficient querying
			indexes := []struct {
				name    string
				sql     string
				comment string
			}{
				{
					"idx_pg_stat_all_tables_connection_db_time",
					`CREATE INDEX IF NOT EXISTS idx_pg_stat_all_tables_connection_db_time
					 ON metrics.pg_stat_all_tables(connection_id, database_name, collected_at DESC)`,
					"Index for efficiently querying metrics by connection, database and time range",
				},
				{
					"idx_pg_stat_all_tables_collected_at",
					`CREATE INDEX IF NOT EXISTS idx_pg_stat_all_tables_collected_at
					 ON metrics.pg_stat_all_tables(collected_at DESC)`,
					"Index for efficiently querying metrics by time range",
				},
			}

			for _, idx := range indexes {
				if _, err := conn.Exec(ctx, idx.sql); err != nil {
					return fmt.Errorf("failed to create index %s: %w", idx.name, err)
				}
				// Comments on indexes of partitioned tables must be added per partition
				// or we can skip them for the parent table
				if _, err := conn.Exec(ctx, fmt.Sprintf("COMMENT ON INDEX metrics.%s IS '%s'", idx.name, idx.comment)); err != nil {
					// Log warning but don't fail - index comments on partitioned tables can be tricky
					log.Printf("Warning: failed to add comment on index %s: %v (this may be expected for partitioned tables)", idx.name, err)
				}
			}

			// Insert probe configuration
			_, err = conn.Exec(ctx, `
				INSERT INTO probe_configs (name, description, collection_interval_seconds, retention_days, is_enabled)
				VALUES ('pg_stat_all_tables', 'Collects table-level statistics from pg_stat_all_tables view for each database', 300, 28, TRUE)
				ON CONFLICT (name) DO NOTHING
			`)
			if err != nil {
				return fmt.Errorf("failed to insert pg_stat_all_tables probe config: %w", err)
			}

			return nil
		},
	})

	// Migration 13: Create pg_stat_statements metrics table
	sm.migrations = append(sm.migrations, Migration{
		Version:     13,
		Description: "Create pg_stat_statements metrics table",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			// Create the partitioned table for pg_stat_statements metrics
			_, err := conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_statements (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name TEXT NOT NULL,
					userid OID,
					dbid OID,
					queryid BIGINT,
					query TEXT,
					calls BIGINT,
					total_exec_time DOUBLE PRECISION,
					mean_exec_time DOUBLE PRECISION,
					min_exec_time DOUBLE PRECISION,
					max_exec_time DOUBLE PRECISION,
					stddev_exec_time DOUBLE PRECISION,
					rows BIGINT,
					shared_blks_hit BIGINT,
					shared_blks_read BIGINT,
					shared_blks_dirtied BIGINT,
					shared_blks_written BIGINT,
					local_blks_hit BIGINT,
					local_blks_read BIGINT,
					local_blks_dirtied BIGINT,
					local_blks_written BIGINT,
					temp_blks_read BIGINT,
					temp_blks_written BIGINT,
					shared_blk_read_time DOUBLE PRECISION,
					shared_blk_write_time DOUBLE PRECISION,
					local_blk_read_time DOUBLE PRECISION,
					local_blk_write_time DOUBLE PRECISION,
					PRIMARY KEY (connection_id, database_name, collected_at, queryid)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_statements IS
					'Metrics collected from pg_stat_statements extension, showing query execution statistics per database';
				COMMENT ON COLUMN metrics.pg_stat_statements.connection_id IS
					'ID of the monitored connection from connections table';
				COMMENT ON COLUMN metrics.pg_stat_statements.collected_at IS
					'Timestamp when the metrics were collected';
				COMMENT ON COLUMN metrics.pg_stat_statements.database_name IS
					'Name of the database where these query statistics were collected';
				COMMENT ON COLUMN metrics.pg_stat_statements.userid IS
					'OID of user who executed the statement';
				COMMENT ON COLUMN metrics.pg_stat_statements.dbid IS
					'OID of database in which the statement was executed';
				COMMENT ON COLUMN metrics.pg_stat_statements.queryid IS
					'Internal hash code computed from the statement''s parse tree';
				COMMENT ON COLUMN metrics.pg_stat_statements.query IS
					'Text of a representative statement';
				COMMENT ON COLUMN metrics.pg_stat_statements.calls IS
					'Number of times executed';
				COMMENT ON COLUMN metrics.pg_stat_statements.total_exec_time IS
					'Total time spent executing the statement, in milliseconds';
				COMMENT ON COLUMN metrics.pg_stat_statements.mean_exec_time IS
					'Mean time spent executing the statement, in milliseconds';
				COMMENT ON COLUMN metrics.pg_stat_statements.min_exec_time IS
					'Minimum time spent executing the statement, in milliseconds';
				COMMENT ON COLUMN metrics.pg_stat_statements.max_exec_time IS
					'Maximum time spent executing the statement, in milliseconds';
				COMMENT ON COLUMN metrics.pg_stat_statements.stddev_exec_time IS
					'Population standard deviation of time spent executing the statement, in milliseconds';
				COMMENT ON COLUMN metrics.pg_stat_statements.rows IS
					'Total number of rows retrieved or affected by the statement';
				COMMENT ON COLUMN metrics.pg_stat_statements.shared_blks_hit IS
					'Total number of shared block cache hits by the statement';
				COMMENT ON COLUMN metrics.pg_stat_statements.shared_blks_read IS
					'Total number of shared blocks read by the statement';
				COMMENT ON COLUMN metrics.pg_stat_statements.shared_blks_dirtied IS
					'Total number of shared blocks dirtied by the statement';
				COMMENT ON COLUMN metrics.pg_stat_statements.shared_blks_written IS
					'Total number of shared blocks written by the statement';
				COMMENT ON COLUMN metrics.pg_stat_statements.local_blks_hit IS
					'Total number of local block cache hits by the statement';
				COMMENT ON COLUMN metrics.pg_stat_statements.local_blks_read IS
					'Total number of local blocks read by the statement';
				COMMENT ON COLUMN metrics.pg_stat_statements.local_blks_dirtied IS
					'Total number of local blocks dirtied by the statement';
				COMMENT ON COLUMN metrics.pg_stat_statements.local_blks_written IS
					'Total number of local blocks written by the statement';
				COMMENT ON COLUMN metrics.pg_stat_statements.temp_blks_read IS
					'Total number of temp blocks read by the statement';
				COMMENT ON COLUMN metrics.pg_stat_statements.temp_blks_written IS
					'Total number of temp blocks written by the statement';
				COMMENT ON COLUMN metrics.pg_stat_statements.shared_blk_read_time IS
					'Total time the statement spent reading shared blocks, in milliseconds (if track_io_timing is enabled). PG <17: stores blk_read_time';
				COMMENT ON COLUMN metrics.pg_stat_statements.shared_blk_write_time IS
					'Total time the statement spent writing shared blocks, in milliseconds (if track_io_timing is enabled). PG <17: stores blk_write_time';
				COMMENT ON COLUMN metrics.pg_stat_statements.local_blk_read_time IS
					'Total time the statement spent reading local blocks, in milliseconds (if track_io_timing is enabled). NULL on PG <17';
				COMMENT ON COLUMN metrics.pg_stat_statements.local_blk_write_time IS
					'Total time the statement spent writing local blocks, in milliseconds (if track_io_timing is enabled). NULL on PG <17';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_statements metrics table: %w", err)
			}

			// Create indexes for efficient querying
			indexes := []struct {
				name    string
				sql     string
				comment string
			}{
				{
					"idx_pg_stat_statements_connection_db_time",
					`CREATE INDEX IF NOT EXISTS idx_pg_stat_statements_connection_db_time
					 ON metrics.pg_stat_statements(connection_id, database_name, collected_at DESC)`,
					"Index for efficiently querying metrics by connection, database and time range",
				},
				{
					"idx_pg_stat_statements_collected_at",
					`CREATE INDEX IF NOT EXISTS idx_pg_stat_statements_collected_at
					 ON metrics.pg_stat_statements(collected_at DESC)`,
					"Index for efficiently querying metrics by time range",
				},
				{
					"idx_pg_stat_statements_queryid",
					`CREATE INDEX IF NOT EXISTS idx_pg_stat_statements_queryid
					 ON metrics.pg_stat_statements(queryid, collected_at DESC)`,
					"Index for efficiently tracking specific queries over time",
				},
			}

			for _, idx := range indexes {
				if _, err := conn.Exec(ctx, idx.sql); err != nil {
					return fmt.Errorf("failed to create index %s: %w", idx.name, err)
				}
				// Comments on indexes of partitioned tables must be added per partition
				// or we can skip them for the parent table
				if _, err := conn.Exec(ctx, fmt.Sprintf("COMMENT ON INDEX metrics.%s IS '%s'", idx.name, idx.comment)); err != nil {
					// Log warning but don't fail - index comments on partitioned tables can be tricky
					log.Printf("Warning: failed to add comment on index %s: %v (this may be expected for partitioned tables)", idx.name, err)
				}
			}

			// Insert probe configuration
			_, err = conn.Exec(ctx, `
				INSERT INTO probe_configs (name, description, collection_interval_seconds, retention_days, is_enabled)
				VALUES ('pg_stat_statements', 'Collects query execution statistics from pg_stat_statements extension for each database', 300, 28, TRUE)
				ON CONFLICT (name) DO NOTHING
			`)
			if err != nil {
				return fmt.Errorf("failed to insert pg_stat_statements probe config: %w", err)
			}

			return nil
		},
	})

	// Migration 14: Fix primary keys for pg_stat_activity and pg_stat_all_tables
	sm.migrations = append(sm.migrations, Migration{
		Version:     14,
		Description: "Fix primary keys for pg_stat_activity and pg_stat_all_tables",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			// Fix pg_stat_activity: add pid to primary key
			_, err := conn.Exec(ctx, `
				-- Drop existing primary key constraint on parent table
				ALTER TABLE metrics.pg_stat_activity DROP CONSTRAINT IF EXISTS pg_stat_activity_pkey CASCADE;

				-- Add new primary key including pid
				ALTER TABLE metrics.pg_stat_activity
					ADD PRIMARY KEY (connection_id, collected_at, pid);
			`)
			if err != nil {
				return fmt.Errorf("failed to fix pg_stat_activity primary key: %w", err)
			}

			// Fix pg_stat_all_tables: add schemaname and relname to primary key
			_, err = conn.Exec(ctx, `
				-- Drop existing primary key constraint on parent table
				ALTER TABLE metrics.pg_stat_all_tables DROP CONSTRAINT IF EXISTS pg_stat_all_tables_pkey CASCADE;

				-- Add new primary key including schemaname and relname
				ALTER TABLE metrics.pg_stat_all_tables
					ADD PRIMARY KEY (connection_id, database_name, collected_at, schemaname, relname);
			`)
			if err != nil {
				return fmt.Errorf("failed to fix pg_stat_all_tables primary key: %w", err)
			}

			return nil
		},
	})

	// Migration 15: Rename monitored_connections to connections and add ownership fields
	sm.migrations = append(sm.migrations, Migration{
		Version:     15,
		Description: "Rename monitored_connections to connections and add owner_username field",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			// Check if monitored_connections table exists
			// If it doesn't exist, that means we're on a fresh installation that started
			// with Migration 2's updated schema, so we can skip this migration
			var exists bool
			err := conn.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM pg_class
					WHERE relname = 'monitored_connections'
					AND relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = 'public')
				)
			`).Scan(&exists)
			if err != nil {
				return fmt.Errorf("failed to check if monitored_connections exists: %w", err)
			}

			if !exists {
				// Table already has the new schema, nothing to do
				return nil
			}

			// Step 1: Add owner_username column
			_, err = conn.Exec(ctx, `
				ALTER TABLE monitored_connections
					ADD COLUMN owner_username VARCHAR(255);
			`)
			if err != nil {
				return fmt.Errorf("failed to add owner_username column: %w", err)
			}

			// Step 2: Drop old constraint
			_, err = conn.Exec(ctx, `
				ALTER TABLE monitored_connections
					DROP CONSTRAINT IF EXISTS chk_owner_token;
			`)
			if err != nil {
				return fmt.Errorf("failed to drop old constraint: %w", err)
			}

			// Step 2.5: Update existing rows where service_token is NULL to have a default owner_username
			// This handles shared connections where service_token was NULL
			_, err = conn.Exec(ctx, `
				UPDATE monitored_connections
				SET owner_username = 'admin'
				WHERE service_token IS NULL AND owner_username IS NULL;
			`)
			if err != nil {
				return fmt.Errorf("failed to set default owner_username: %w", err)
			}

			// Step 3: Add new constraint to ensure exactly one of owner_username or service_token is set
			_, err = conn.Exec(ctx, `
				ALTER TABLE monitored_connections
					ADD CONSTRAINT chk_owner CHECK (
						(owner_username IS NOT NULL AND service_token IS NULL) OR
						(owner_username IS NULL AND service_token IS NOT NULL)
					);
			`)
			if err != nil {
				return fmt.Errorf("failed to add ownership constraint: %w", err)
			}

			// Step 4: Rename service_token column to owner_token
			_, err = conn.Exec(ctx, `
				ALTER TABLE monitored_connections RENAME COLUMN service_token TO owner_token;
			`)
			if err != nil {
				return fmt.Errorf("failed to rename service_token column: %w", err)
			}

			// Step 5: Rename table
			_, err = conn.Exec(ctx, `
				ALTER TABLE monitored_connections RENAME TO connections;
			`)
			if err != nil {
				return fmt.Errorf("failed to rename table: %w", err)
			}

			// Step 6: Update comments
			_, err = conn.Exec(ctx, `
				COMMENT ON TABLE connections IS
					'PostgreSQL server connections that can be monitored by the collector';
				COMMENT ON COLUMN connections.id IS
					'Unique identifier for the connection';
				COMMENT ON COLUMN connections.owner_username IS
					'Username of the user who owns this connection (mutually exclusive with owner_token)';
				COMMENT ON COLUMN connections.owner_token IS
					'Service token that owns this connection (mutually exclusive with owner_username)';
			`)
			if err != nil {
				return fmt.Errorf("failed to update comments: %w", err)
			}

			return nil
		},
	})

	// Migration 16: Add foreign key constraints for owner_username and owner_token
	sm.migrations = append(sm.migrations, Migration{
		Version:     16,
		Description: "Add foreign key constraints to connections table",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			// Add foreign key from connections.owner_username to user_accounts.username
			_, err := conn.Exec(ctx, `
				ALTER TABLE connections
					ADD CONSTRAINT fk_connections_owner_username
					FOREIGN KEY (owner_username)
					REFERENCES user_accounts(username)
					ON DELETE RESTRICT
					ON UPDATE CASCADE;
			`)
			if err != nil {
				return fmt.Errorf("failed to add foreign key for owner_username: %w", err)
			}

			// Add foreign key from connections.owner_token to service_tokens.name
			_, err = conn.Exec(ctx, `
				ALTER TABLE connections
					ADD CONSTRAINT fk_connections_owner_token
					FOREIGN KEY (owner_token)
					REFERENCES service_tokens(name)
					ON DELETE RESTRICT
					ON UPDATE CASCADE;
			`)
			if err != nil {
				return fmt.Errorf("failed to add foreign key for owner_token: %w", err)
			}

			// Add comments for the foreign keys
			_, err = conn.Exec(ctx, `
				COMMENT ON CONSTRAINT fk_connections_owner_username ON connections IS
					'Foreign key to user_accounts ensuring valid owner username';
				COMMENT ON CONSTRAINT fk_connections_owner_token ON connections IS
					'Foreign key to service_tokens ensuring valid owner token';
			`)
			if err != nil {
				return fmt.Errorf("failed to add foreign key comments: %w", err)
			}

			return nil
		},
	})

	// Migration 17: Create server-wide monitoring probe tables
	sm.migrations = append(sm.migrations, Migration{
		Version:     17,
		Description: "Create metrics tables for server-wide monitoring probes",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			// Create pg_stat_replication table
			_, err := conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_replication (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					pid INTEGER,
					usesysid OID,
					usename TEXT,
					application_name TEXT,
					client_addr INET,
					client_hostname TEXT,
					client_port INTEGER,
					backend_start TIMESTAMP,
					backend_xmin TEXT,
					state TEXT,
					sent_lsn TEXT,
					write_lsn TEXT,
					flush_lsn TEXT,
					replay_lsn TEXT,
					write_lag INTERVAL,
					flush_lag INTERVAL,
					replay_lag INTERVAL,
					sync_priority INTEGER,
					sync_state TEXT,
					reply_time TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, pid),
					CONSTRAINT fk_pg_stat_replication_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_replication IS
					'Replication statistics for active replication connections';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_replication table: %w", err)
			}

			// Create pg_stat_replication_slots table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_replication_slots (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					slot_name TEXT NOT NULL,
					spill_txns BIGINT,
					spill_count BIGINT,
					spill_bytes BIGINT,
					stream_txns BIGINT,
					stream_count BIGINT,
					stream_bytes BIGINT,
					total_txns BIGINT,
					total_bytes BIGINT,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, slot_name),
					CONSTRAINT fk_pg_stat_replication_slots_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_replication_slots IS
					'Replication slot statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_replication_slots table: %w", err)
			}

			// Create pg_stat_wal_receiver table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_wal_receiver (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					pid INTEGER,
					status TEXT,
					receive_start_lsn TEXT,
					receive_start_tli INTEGER,
					written_lsn TEXT,
					flushed_lsn TEXT,
					received_tli INTEGER,
					last_msg_send_time TIMESTAMP,
					last_msg_receipt_time TIMESTAMP,
					latest_end_lsn TEXT,
					latest_end_time TIMESTAMP,
					slot_name TEXT,
					sender_host TEXT,
					sender_port INTEGER,
					conninfo TEXT,
					PRIMARY KEY (connection_id, collected_at),
					CONSTRAINT fk_pg_stat_wal_receiver_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_wal_receiver IS
					'WAL receiver statistics (standby servers)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_wal_receiver table: %w", err)
			}

			// Create pg_stat_recovery_prefetch table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_recovery_prefetch (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					stats_reset TIMESTAMP,
					prefetch BIGINT,
					hit BIGINT,
					skip_init BIGINT,
					skip_new BIGINT,
					skip_fpw BIGINT,
					skip_rep BIGINT,
					wal_distance BIGINT,
					block_distance BIGINT,
					io_depth BIGINT,
					PRIMARY KEY (connection_id, collected_at),
					CONSTRAINT fk_pg_stat_recovery_prefetch_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_recovery_prefetch IS
					'Recovery prefetch statistics (PG 15+)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_recovery_prefetch table: %w", err)
			}

			// Create pg_stat_subscription table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_subscription (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					subid OID NOT NULL,
					subname TEXT,
					worker_type TEXT,
					pid INTEGER,
					leader_pid INTEGER,
					relid OID,
					received_lsn TEXT,
					last_msg_send_time TIMESTAMP,
					last_msg_receipt_time TIMESTAMP,
					latest_end_lsn TEXT,
					latest_end_time TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, subid),
					CONSTRAINT fk_pg_stat_subscription_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_subscription IS
					'Logical replication subscription statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_subscription table: %w", err)
			}

			// Create pg_stat_subscription_stats table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_subscription_stats (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					subid OID NOT NULL,
					subname TEXT,
					apply_error_count BIGINT,
					sync_error_count BIGINT,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, subid),
					CONSTRAINT fk_pg_stat_subscription_stats_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_subscription_stats IS
					'Logical replication subscription cumulative statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_subscription_stats table: %w", err)
			}

			// Create pg_stat_ssl table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_ssl (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					pid INTEGER NOT NULL,
					ssl BOOLEAN,
					version TEXT,
					cipher TEXT,
					bits INTEGER,
					client_dn TEXT,
					client_serial TEXT,
					issuer_dn TEXT,
					PRIMARY KEY (connection_id, collected_at, pid),
					CONSTRAINT fk_pg_stat_ssl_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_ssl IS
					'SSL connection statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_ssl table: %w", err)
			}

			// Create pg_stat_gssapi table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_gssapi (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					pid INTEGER NOT NULL,
					gss_authenticated BOOLEAN,
					principal TEXT,
					encrypted BOOLEAN,
					credentials_delegated BOOLEAN,
					PRIMARY KEY (connection_id, collected_at, pid),
					CONSTRAINT fk_pg_stat_gssapi_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_gssapi IS
					'GSSAPI connection statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_gssapi table: %w", err)
			}

			// Create pg_stat_archiver table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_archiver (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					archived_count BIGINT,
					last_archived_wal TEXT,
					last_archived_time TIMESTAMP,
					failed_count BIGINT,
					last_failed_wal TEXT,
					last_failed_time TIMESTAMP,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at),
					CONSTRAINT fk_pg_stat_archiver_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_archiver IS
					'WAL archiver statistics (singleton)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_archiver table: %w", err)
			}

			// Create pg_stat_bgwriter table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_bgwriter (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					buffers_clean BIGINT,
					maxwritten_clean BIGINT,
					buffers_alloc BIGINT,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at),
					CONSTRAINT fk_pg_stat_bgwriter_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_bgwriter IS
					'Background writer statistics (singleton, deprecated PG 17+)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_bgwriter table: %w", err)
			}

			// Create pg_stat_checkpointer table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_checkpointer (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					num_timed BIGINT,
					num_requested BIGINT,
					restartpoints_timed BIGINT,
					restartpoints_req BIGINT,
					restartpoints_done BIGINT,
					write_time DOUBLE PRECISION,
					sync_time DOUBLE PRECISION,
					buffers_written BIGINT,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at),
					CONSTRAINT fk_pg_stat_checkpointer_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_checkpointer IS
					'Checkpointer statistics (singleton, PG 17+)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_checkpointer table: %w", err)
			}

			// Create pg_stat_wal table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_wal (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					wal_records BIGINT,
					wal_fpi BIGINT,
					wal_bytes NUMERIC,
					wal_buffers_full BIGINT,
					wal_write BIGINT,
					wal_sync BIGINT,
					wal_write_time DOUBLE PRECISION,
					wal_sync_time DOUBLE PRECISION,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at),
					CONSTRAINT fk_pg_stat_wal_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_wal IS
					'WAL generation statistics (singleton)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_wal table: %w", err)
			}

			// Create pg_stat_io table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_io (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					backend_type TEXT NOT NULL,
					object TEXT NOT NULL,
					context TEXT NOT NULL,
					reads BIGINT,
					read_time DOUBLE PRECISION,
					writes BIGINT,
					write_time DOUBLE PRECISION,
					writebacks BIGINT,
					writeback_time DOUBLE PRECISION,
					extends BIGINT,
					extend_time DOUBLE PRECISION,
					op_bytes BIGINT,
					hits BIGINT,
					evictions BIGINT,
					reuses BIGINT,
					fsyncs BIGINT,
					fsync_time DOUBLE PRECISION,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, backend_type, object, context),
					CONSTRAINT fk_pg_stat_io_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_io IS
					'I/O statistics by backend type and context';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_io table: %w", err)
			}

			// Create pg_stat_slru table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_slru (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					name TEXT NOT NULL,
					blks_zeroed BIGINT,
					blks_hit BIGINT,
					blks_read BIGINT,
					blks_written BIGINT,
					blks_exists BIGINT,
					flushes BIGINT,
					truncates BIGINT,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, name),
					CONSTRAINT fk_pg_stat_slru_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_slru IS
					'SLRU (Simple LRU) cache statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_slru table: %w", err)
			}

			return nil
		},
	})

	// Migration 18: Create database-scoped monitoring probe tables
	sm.migrations = append(sm.migrations, Migration{
		Version:     18,
		Description: "Create metrics tables for database-scoped monitoring probes",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			// Create pg_stat_database table
			_, err := conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_database (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					datid OID,
					datname TEXT,
					numbackends INTEGER,
					xact_commit BIGINT,
					xact_rollback BIGINT,
					blks_read BIGINT,
					blks_hit BIGINT,
					tup_returned BIGINT,
					tup_fetched BIGINT,
					tup_inserted BIGINT,
					tup_updated BIGINT,
					tup_deleted BIGINT,
					conflicts BIGINT,
					temp_files BIGINT,
					temp_bytes BIGINT,
					deadlocks BIGINT,
					checksum_failures BIGINT,
					checksum_last_failure TIMESTAMP,
					blk_read_time DOUBLE PRECISION,
					blk_write_time DOUBLE PRECISION,
					session_time DOUBLE PRECISION,
					active_time DOUBLE PRECISION,
					idle_in_transaction_time DOUBLE PRECISION,
					sessions BIGINT,
					sessions_abandoned BIGINT,
					sessions_fatal BIGINT,
					sessions_killed BIGINT,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, database_name),
					CONSTRAINT fk_pg_stat_database_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_database IS
					'Per-database statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_database table: %w", err)
			}

			// Create pg_stat_database_conflicts table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_database_conflicts (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					datid OID,
					datname TEXT,
					confl_tablespace BIGINT,
					confl_lock BIGINT,
					confl_snapshot BIGINT,
					confl_bufferpin BIGINT,
					confl_deadlock BIGINT,
					confl_active_logicalslot BIGINT,
					PRIMARY KEY (connection_id, collected_at, database_name),
					CONSTRAINT fk_pg_stat_database_conflicts_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_database_conflicts IS
					'Database conflict statistics (standby servers)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_database_conflicts table: %w", err)
			}

			// Create pg_stat_all_indexes table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_all_indexes (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					relid OID,
					indexrelid OID,
					schemaname TEXT,
					relname TEXT,
					indexrelname TEXT,
					idx_scan BIGINT,
					last_idx_scan TIMESTAMP,
					idx_tup_read BIGINT,
					idx_tup_fetch BIGINT,
					PRIMARY KEY (connection_id, collected_at, database_name, indexrelid),
					CONSTRAINT fk_pg_stat_all_indexes_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_all_indexes IS
					'Statistics for all indexes in all databases';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_all_indexes table: %w", err)
			}

			// Create pg_statio_all_tables table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_statio_all_tables (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					relid OID,
					schemaname TEXT,
					relname TEXT,
					heap_blks_read BIGINT,
					heap_blks_hit BIGINT,
					idx_blks_read BIGINT,
					idx_blks_hit BIGINT,
					toast_blks_read BIGINT,
					toast_blks_hit BIGINT,
					tidx_blks_read BIGINT,
					tidx_blks_hit BIGINT,
					PRIMARY KEY (connection_id, collected_at, database_name, relid),
					CONSTRAINT fk_pg_statio_all_tables_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_statio_all_tables IS
					'I/O statistics for all tables';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_statio_all_tables table: %w", err)
			}

			// Create pg_statio_all_indexes table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_statio_all_indexes (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					relid OID,
					indexrelid OID,
					schemaname TEXT,
					relname TEXT,
					indexrelname TEXT,
					idx_blks_read BIGINT,
					idx_blks_hit BIGINT,
					PRIMARY KEY (connection_id, collected_at, database_name, indexrelid),
					CONSTRAINT fk_pg_statio_all_indexes_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_statio_all_indexes IS
					'I/O statistics for all indexes';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_statio_all_indexes table: %w", err)
			}

			// Create pg_statio_all_sequences table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_statio_all_sequences (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					relid OID,
					schemaname TEXT,
					relname TEXT,
					blks_read BIGINT,
					blks_hit BIGINT,
					PRIMARY KEY (connection_id, collected_at, database_name, relid),
					CONSTRAINT fk_pg_statio_all_sequences_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_statio_all_sequences IS
					'I/O statistics for all sequences';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_statio_all_sequences table: %w", err)
			}

			// Create pg_stat_user_functions table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_user_functions (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					funcid OID,
					schemaname TEXT,
					funcname TEXT,
					calls BIGINT,
					total_time DOUBLE PRECISION,
					self_time DOUBLE PRECISION,
					PRIMARY KEY (connection_id, collected_at, database_name, funcid),
					CONSTRAINT fk_pg_stat_user_functions_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_user_functions IS
					'Statistics for user-defined functions';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_user_functions table: %w", err)
			}

			return nil
		},
	})

	// Migration 19: Recreate probe metrics tables with explicit columns
	sm.migrations = append(sm.migrations, Migration{
		Version:     19,
		Description: "Recreate probe metrics tables with explicit columns (replacing JSONB storage)",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			// Drop all old tables
			tables := []string{
				"pg_stat_replication", "pg_stat_replication_slots", "pg_stat_wal_receiver",
				"pg_stat_recovery_prefetch", "pg_stat_subscription", "pg_stat_subscription_stats",
				"pg_stat_ssl", "pg_stat_gssapi", "pg_stat_archiver", "pg_stat_io",
				"pg_stat_bgwriter", "pg_stat_checkpointer", "pg_stat_wal", "pg_stat_slru",
				"pg_stat_database", "pg_stat_database_conflicts", "pg_stat_all_indexes",
				"pg_statio_all_tables", "pg_statio_all_indexes", "pg_statio_all_sequences",
				"pg_stat_user_functions",
			}

			for _, table := range tables {
				_, err := conn.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS metrics.%s CASCADE", table))
				if err != nil {
					return fmt.Errorf("failed to drop table %s: %w", table, err)
				}
			}

			// Recreate all tables from migrations 17 and 18

			// Create pg_stat_replication table
			_, err := conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_replication (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					pid INTEGER,
					usesysid OID,
					usename TEXT,
					application_name TEXT,
					client_addr INET,
					client_hostname TEXT,
					client_port INTEGER,
					backend_start TIMESTAMP,
					backend_xmin TEXT,
					state TEXT,
					sent_lsn TEXT,
					write_lsn TEXT,
					flush_lsn TEXT,
					replay_lsn TEXT,
					write_lag INTERVAL,
					flush_lag INTERVAL,
					replay_lag INTERVAL,
					sync_priority INTEGER,
					sync_state TEXT,
					reply_time TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, pid),
					CONSTRAINT fk_pg_stat_replication_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_replication IS
					'Replication statistics for active replication connections';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_replication table: %w", err)
			}

			// Create pg_stat_replication_slots table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_replication_slots (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					slot_name TEXT NOT NULL,
					spill_txns BIGINT,
					spill_count BIGINT,
					spill_bytes BIGINT,
					stream_txns BIGINT,
					stream_count BIGINT,
					stream_bytes BIGINT,
					total_txns BIGINT,
					total_bytes BIGINT,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, slot_name),
					CONSTRAINT fk_pg_stat_replication_slots_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_replication_slots IS
					'Replication slot statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_replication_slots table: %w", err)
			}

			// Create pg_stat_wal_receiver table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_wal_receiver (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					pid INTEGER,
					status TEXT,
					receive_start_lsn TEXT,
					receive_start_tli INTEGER,
					written_lsn TEXT,
					flushed_lsn TEXT,
					received_tli INTEGER,
					last_msg_send_time TIMESTAMP,
					last_msg_receipt_time TIMESTAMP,
					latest_end_lsn TEXT,
					latest_end_time TIMESTAMP,
					slot_name TEXT,
					sender_host TEXT,
					sender_port INTEGER,
					conninfo TEXT,
					PRIMARY KEY (connection_id, collected_at),
					CONSTRAINT fk_pg_stat_wal_receiver_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_wal_receiver IS
					'WAL receiver statistics (standby servers)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_wal_receiver table: %w", err)
			}

			// Create pg_stat_recovery_prefetch table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_recovery_prefetch (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					stats_reset TIMESTAMP,
					prefetch BIGINT,
					hit BIGINT,
					skip_init BIGINT,
					skip_new BIGINT,
					skip_fpw BIGINT,
					skip_rep BIGINT,
					wal_distance BIGINT,
					block_distance BIGINT,
					io_depth BIGINT,
					PRIMARY KEY (connection_id, collected_at),
					CONSTRAINT fk_pg_stat_recovery_prefetch_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_recovery_prefetch IS
					'Recovery prefetch statistics (PG 15+)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_recovery_prefetch table: %w", err)
			}

			// Create pg_stat_subscription table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_subscription (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					subid OID NOT NULL,
					subname TEXT,
					worker_type TEXT,
					pid INTEGER,
					leader_pid INTEGER,
					relid OID,
					received_lsn TEXT,
					last_msg_send_time TIMESTAMP,
					last_msg_receipt_time TIMESTAMP,
					latest_end_lsn TEXT,
					latest_end_time TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, subid),
					CONSTRAINT fk_pg_stat_subscription_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_subscription IS
					'Logical replication subscription statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_subscription table: %w", err)
			}

			// Create pg_stat_subscription_stats table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_subscription_stats (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					subid OID NOT NULL,
					subname TEXT,
					apply_error_count BIGINT,
					sync_error_count BIGINT,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, subid),
					CONSTRAINT fk_pg_stat_subscription_stats_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_subscription_stats IS
					'Logical replication subscription cumulative statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_subscription_stats table: %w", err)
			}

			// Create pg_stat_ssl table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_ssl (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					pid INTEGER NOT NULL,
					ssl BOOLEAN,
					version TEXT,
					cipher TEXT,
					bits INTEGER,
					client_dn TEXT,
					client_serial TEXT,
					issuer_dn TEXT,
					PRIMARY KEY (connection_id, collected_at, pid),
					CONSTRAINT fk_pg_stat_ssl_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_ssl IS
					'SSL connection statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_ssl table: %w", err)
			}

			// Create pg_stat_gssapi table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_gssapi (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					pid INTEGER NOT NULL,
					gss_authenticated BOOLEAN,
					principal TEXT,
					encrypted BOOLEAN,
					credentials_delegated BOOLEAN,
					PRIMARY KEY (connection_id, collected_at, pid),
					CONSTRAINT fk_pg_stat_gssapi_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_gssapi IS
					'GSSAPI connection statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_gssapi table: %w", err)
			}

			// Create pg_stat_archiver table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_archiver (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					archived_count BIGINT,
					last_archived_wal TEXT,
					last_archived_time TIMESTAMP,
					failed_count BIGINT,
					last_failed_wal TEXT,
					last_failed_time TIMESTAMP,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at),
					CONSTRAINT fk_pg_stat_archiver_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_archiver IS
					'WAL archiver statistics (singleton)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_archiver table: %w", err)
			}

			// Create pg_stat_bgwriter table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_bgwriter (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					buffers_clean BIGINT,
					maxwritten_clean BIGINT,
					buffers_alloc BIGINT,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at),
					CONSTRAINT fk_pg_stat_bgwriter_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_bgwriter IS
					'Background writer statistics (singleton, deprecated PG 17+)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_bgwriter table: %w", err)
			}

			// Create pg_stat_checkpointer table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_checkpointer (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					num_timed BIGINT,
					num_requested BIGINT,
					restartpoints_timed BIGINT,
					restartpoints_req BIGINT,
					restartpoints_done BIGINT,
					write_time DOUBLE PRECISION,
					sync_time DOUBLE PRECISION,
					buffers_written BIGINT,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at),
					CONSTRAINT fk_pg_stat_checkpointer_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_checkpointer IS
					'Checkpointer statistics (singleton, PG 17+)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_checkpointer table: %w", err)
			}

			// Create pg_stat_wal table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_wal (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					wal_records BIGINT,
					wal_fpi BIGINT,
					wal_bytes NUMERIC,
					wal_buffers_full BIGINT,
					wal_write BIGINT,
					wal_sync BIGINT,
					wal_write_time DOUBLE PRECISION,
					wal_sync_time DOUBLE PRECISION,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at),
					CONSTRAINT fk_pg_stat_wal_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_wal IS
					'WAL generation statistics (singleton)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_wal table: %w", err)
			}

			// Create pg_stat_io table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_io (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					backend_type TEXT NOT NULL,
					object TEXT NOT NULL,
					context TEXT NOT NULL,
					reads BIGINT,
					read_time DOUBLE PRECISION,
					writes BIGINT,
					write_time DOUBLE PRECISION,
					writebacks BIGINT,
					writeback_time DOUBLE PRECISION,
					extends BIGINT,
					extend_time DOUBLE PRECISION,
					op_bytes BIGINT,
					hits BIGINT,
					evictions BIGINT,
					reuses BIGINT,
					fsyncs BIGINT,
					fsync_time DOUBLE PRECISION,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, backend_type, object, context),
					CONSTRAINT fk_pg_stat_io_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_io IS
					'I/O statistics by backend type and context';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_io table: %w", err)
			}

			// Create pg_stat_slru table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_slru (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					name TEXT NOT NULL,
					blks_zeroed BIGINT,
					blks_hit BIGINT,
					blks_read BIGINT,
					blks_written BIGINT,
					blks_exists BIGINT,
					flushes BIGINT,
					truncates BIGINT,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, name),
					CONSTRAINT fk_pg_stat_slru_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_slru IS
					'SLRU (Simple LRU) cache statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_slru table: %w", err)
			}

			// Create pg_stat_database table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_database (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					datid OID,
					datname TEXT,
					numbackends INTEGER,
					xact_commit BIGINT,
					xact_rollback BIGINT,
					blks_read BIGINT,
					blks_hit BIGINT,
					tup_returned BIGINT,
					tup_fetched BIGINT,
					tup_inserted BIGINT,
					tup_updated BIGINT,
					tup_deleted BIGINT,
					conflicts BIGINT,
					temp_files BIGINT,
					temp_bytes BIGINT,
					deadlocks BIGINT,
					checksum_failures BIGINT,
					checksum_last_failure TIMESTAMP,
					blk_read_time DOUBLE PRECISION,
					blk_write_time DOUBLE PRECISION,
					session_time DOUBLE PRECISION,
					active_time DOUBLE PRECISION,
					idle_in_transaction_time DOUBLE PRECISION,
					sessions BIGINT,
					sessions_abandoned BIGINT,
					sessions_fatal BIGINT,
					sessions_killed BIGINT,
					stats_reset TIMESTAMP,
					PRIMARY KEY (connection_id, collected_at, database_name),
					CONSTRAINT fk_pg_stat_database_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_database IS
					'Per-database statistics';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_database table: %w", err)
			}

			// Create pg_stat_database_conflicts table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_database_conflicts (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					datid OID,
					datname TEXT,
					confl_tablespace BIGINT,
					confl_lock BIGINT,
					confl_snapshot BIGINT,
					confl_bufferpin BIGINT,
					confl_deadlock BIGINT,
					confl_active_logicalslot BIGINT,
					PRIMARY KEY (connection_id, collected_at, database_name),
					CONSTRAINT fk_pg_stat_database_conflicts_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_database_conflicts IS
					'Database conflict statistics (standby servers)';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_database_conflicts table: %w", err)
			}

			// Create pg_stat_all_indexes table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_all_indexes (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					relid OID,
					indexrelid OID,
					schemaname TEXT,
					relname TEXT,
					indexrelname TEXT,
					idx_scan BIGINT,
					last_idx_scan TIMESTAMP,
					idx_tup_read BIGINT,
					idx_tup_fetch BIGINT,
					PRIMARY KEY (connection_id, collected_at, database_name, indexrelid),
					CONSTRAINT fk_pg_stat_all_indexes_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_all_indexes IS
					'Statistics for all indexes in all databases';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_all_indexes table: %w", err)
			}

			// Create pg_statio_all_tables table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_statio_all_tables (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					relid OID,
					schemaname TEXT,
					relname TEXT,
					heap_blks_read BIGINT,
					heap_blks_hit BIGINT,
					idx_blks_read BIGINT,
					idx_blks_hit BIGINT,
					toast_blks_read BIGINT,
					toast_blks_hit BIGINT,
					tidx_blks_read BIGINT,
					tidx_blks_hit BIGINT,
					PRIMARY KEY (connection_id, collected_at, database_name, relid),
					CONSTRAINT fk_pg_statio_all_tables_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_statio_all_tables IS
					'I/O statistics for all tables';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_statio_all_tables table: %w", err)
			}

			// Create pg_statio_all_indexes table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_statio_all_indexes (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					relid OID,
					indexrelid OID,
					schemaname TEXT,
					relname TEXT,
					indexrelname TEXT,
					idx_blks_read BIGINT,
					idx_blks_hit BIGINT,
					PRIMARY KEY (connection_id, collected_at, database_name, indexrelid),
					CONSTRAINT fk_pg_statio_all_indexes_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_statio_all_indexes IS
					'I/O statistics for all indexes';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_statio_all_indexes table: %w", err)
			}

			// Create pg_statio_all_sequences table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_statio_all_sequences (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					relid OID,
					schemaname TEXT,
					relname TEXT,
					blks_read BIGINT,
					blks_hit BIGINT,
					PRIMARY KEY (connection_id, collected_at, database_name, relid),
					CONSTRAINT fk_pg_statio_all_sequences_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_statio_all_sequences IS
					'I/O statistics for all sequences';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_statio_all_sequences table: %w", err)
			}

			// Create pg_stat_user_functions table
			_, err = conn.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_user_functions (
					connection_id INTEGER NOT NULL,
					collected_at TIMESTAMP NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					funcid OID,
					schemaname TEXT,
					funcname TEXT,
					calls BIGINT,
					total_time DOUBLE PRECISION,
					self_time DOUBLE PRECISION,
					PRIMARY KEY (connection_id, collected_at, database_name, funcid),
					CONSTRAINT fk_pg_stat_user_functions_connection_id
						FOREIGN KEY (connection_id)
						REFERENCES connections(id)
						ON DELETE CASCADE
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_user_functions IS
					'Statistics for user-defined functions';
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_user_functions table: %w", err)
			}

			return nil
		},
	})

	// Migration 20: Update pg_stat_statements table to support PostgreSQL 17+ column changes
	sm.migrations = append(sm.migrations, Migration{
		Version:     20,
		Description: "Update pg_stat_statements table to support PostgreSQL 17+ timing columns",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()
			// In PostgreSQL 17, blk_read_time and blk_write_time were renamed to
			// shared_blk_read_time and shared_blk_write_time, and new columns
			// local_blk_read_time and local_blk_write_time were added.
			//
			// This migration only applies to databases that were created before migration 13
			// was updated to use the new column names. We check if the old columns exist
			// before attempting to rename them.

			// Check if old column names exist
			var hasOldColumns bool
			err := conn.QueryRow(ctx, `
                SELECT EXISTS (
                    SELECT 1
                    FROM information_schema.columns
                    WHERE table_schema = 'metrics'
                      AND table_name = 'pg_stat_statements'
                      AND column_name = 'blk_read_time'
                )
            `).Scan(&hasOldColumns)

			if err != nil {
				return fmt.Errorf("failed to check for old columns: %w", err)
			}

			// If old columns don't exist, migration 13 was already updated
			// and nothing needs to be done
			if !hasOldColumns {
				return nil
			}

			// Old columns exist, perform the migration
			statements := []string{
				// Rename old columns to new names
				`ALTER TABLE metrics.pg_stat_statements
				 RENAME COLUMN blk_read_time TO shared_blk_read_time`,

				`ALTER TABLE metrics.pg_stat_statements
				 RENAME COLUMN blk_write_time TO shared_blk_write_time`,

				// Add new local timing columns for PG 17+
				`ALTER TABLE metrics.pg_stat_statements
				 ADD COLUMN local_blk_read_time DOUBLE PRECISION`,

				`ALTER TABLE metrics.pg_stat_statements
				 ADD COLUMN local_blk_write_time DOUBLE PRECISION`,

				// Update comments
				`COMMENT ON COLUMN metrics.pg_stat_statements.shared_blk_read_time
				 IS 'Total time spent reading shared data file blocks (in milliseconds). PG <17: stores blk_read_time'`,

				`COMMENT ON COLUMN metrics.pg_stat_statements.shared_blk_write_time
				 IS 'Total time spent writing shared data file blocks (in milliseconds). PG <17: stores blk_write_time'`,

				`COMMENT ON COLUMN metrics.pg_stat_statements.local_blk_read_time
				 IS 'Total time spent reading local data file blocks (in milliseconds). NULL on PG <17'`,

				`COMMENT ON COLUMN metrics.pg_stat_statements.local_blk_write_time
				 IS 'Total time spent writing local data file blocks (in milliseconds). NULL on PG <17'`,
			}

			for _, stmt := range statements {
				if _, err := conn.Exec(ctx, stmt); err != nil {
					return fmt.Errorf("migration failed: %w", err)
				}
			}

			return nil
		},
	})

	// Migration 21: Add toplevel column and update pg_stat_statements primary key
	sm.migrations = append(sm.migrations, Migration{
		Version:     21,
		Description: "Add toplevel column to pg_stat_statements and update primary key to match PostgreSQL uniqueness constraint",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Check if toplevel column already exists
			var hasColumn bool
			err := conn.QueryRow(ctx, `
                SELECT EXISTS (
                    SELECT 1
                    FROM information_schema.columns
                    WHERE table_schema = 'metrics'
                      AND table_name = 'pg_stat_statements'
                      AND column_name = 'toplevel'
                )
            `).Scan(&hasColumn)

			if err != nil {
				return fmt.Errorf("failed to check for toplevel column: %w", err)
			}

			// Add toplevel column if it doesn't exist
			if !hasColumn {
				_, err = conn.Exec(ctx, `
                    ALTER TABLE metrics.pg_stat_statements
                    ADD COLUMN toplevel BOOLEAN DEFAULT TRUE
                `)
				if err != nil {
					return fmt.Errorf("failed to add toplevel column: %w", err)
				}

				// Update any existing NULL values to TRUE (default for PG <13 where column doesn't exist)
				_, err = conn.Exec(ctx, `
                    UPDATE metrics.pg_stat_statements
                    SET toplevel = TRUE
                    WHERE toplevel IS NULL
                `)
				if err != nil {
					return fmt.Errorf("failed to update NULL toplevel values: %w", err)
				}

				// Add comment
				_, err = conn.Exec(ctx, `
                    COMMENT ON COLUMN metrics.pg_stat_statements.toplevel IS
                        'True if the statement was executed at the top level, false if executed within a function'
                `)
				if err != nil {
					return fmt.Errorf("failed to add comment on toplevel column: %w", err)
				}
			}

			// Update primary key to include userid, dbid, and toplevel
			// This matches PostgreSQL's pg_stat_statements uniqueness constraint
			_, err = conn.Exec(ctx, `
                -- Drop existing primary key constraint on parent table
                ALTER TABLE metrics.pg_stat_statements DROP CONSTRAINT IF EXISTS pg_stat_statements_pkey CASCADE;

                -- Add new primary key matching PostgreSQL's uniqueness constraint
                ALTER TABLE metrics.pg_stat_statements
                    ADD PRIMARY KEY (connection_id, database_name, collected_at, queryid, userid, dbid, toplevel);
            `)
			if err != nil {
				return fmt.Errorf("failed to update primary key: %w", err)
			}

			return nil
		},
	})
}

// Migrate applies all pending migrations
func (sm *SchemaManager) Migrate(conn *pgxpool.Conn) error {
	ctx := context.Background()
	log.Println("Starting schema migration...")

	// Sort migrations by version
	sort.Slice(sm.migrations, func(i, j int) bool {
		return sm.migrations[i].Version < sm.migrations[j].Version
	})

	// Get current schema version
	currentVersion, err := sm.getCurrentVersion(conn)
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
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration %d: %w",
				migration.Version, err)
		}

		// Apply the migration
		if err := migration.Up(conn); err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				log.Printf("Failed to rollback transaction: %v", rbErr)
			}
			return fmt.Errorf("failed to apply migration %d: %w", migration.Version, err)
		}

		// Record the migration in schema_version
		_, err = conn.Exec(ctx, `
            INSERT INTO schema_version (version, description)
            VALUES ($1, $2)
            ON CONFLICT (version) DO NOTHING
        `, migration.Version, migration.Description)
		if err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				log.Printf("Failed to rollback transaction: %v", rbErr)
			}
			return fmt.Errorf("failed to record migration %d: %w", migration.Version, err)
		}

		// Commit the transaction
		if err := tx.Commit(ctx); err != nil {
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
func (sm *SchemaManager) getCurrentVersion(conn *pgxpool.Conn) (int, error) {
	ctx := context.Background()
	var version int
	err := conn.QueryRow(ctx, `
        SELECT COALESCE(MAX(version), 0)
        FROM schema_version
    `).Scan(&version)

	if err != nil {
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
	// Check for pgx error with PostgreSQL error code 42P01 (undefined_table)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42P01"
	}
	// Also handle the case where QueryRow returns pgx.ErrNoRows for non-existent tables
	return errors.Is(err, pgx.ErrNoRows)
}

// GetMigrationStatus returns information about migration status
func (sm *SchemaManager) GetMigrationStatus(conn *pgxpool.Conn) ([]MigrationStatus, error) {
	ctx := context.Background()
	currentVersion, err := sm.getCurrentVersion(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to get current version: %w", err)
	}

	// Get applied migrations from database
	appliedMigrations := make(map[int]MigrationRecord)
	rows, err := conn.Query(ctx, `
        SELECT version, description, applied_at
        FROM schema_version
        ORDER BY version
    `)
	tableNotExist := isTableNotExistError(err)
	if err != nil && !tableNotExist {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}

	if rows != nil && !tableNotExist {
		defer rows.Close()

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
	AppliedAt   time.Time
}

// MigrationStatus represents the status of a migration
type MigrationStatus struct {
	Version     int
	Description string
	Applied     bool
	AppliedAt   *time.Time
}
