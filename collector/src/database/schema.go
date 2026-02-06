/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// Migration represents a database schema migration
type Migration struct {
	Version     int
	Description string
	Up          func(pgx.Tx) error
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
	// Consolidated Migration #1
	// This migration consolidates all previous migrations into a single migration.
	// It creates the complete schema from scratch with:
	// - All tables with proper TIMESTAMPTZ columns
	// - Standardized indexes on all metrics tables
	// - All seed data for probe configs and alert rules

	sm.migrations = append(sm.migrations, Migration{
		Version:     1,
		Description: "Complete schema with all tables, indexes, and seed data",
		Up: func(tx pgx.Tx) error {
			ctx := context.Background()

			// =====================================================================
			// PART 1: Core Infrastructure Tables
			// =====================================================================

			// Create schema_version table
			_, err := tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS schema_version (
					version INTEGER PRIMARY KEY,
					description TEXT NOT NULL,
					applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
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

			// Create connections table
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS connections (
					id SERIAL PRIMARY KEY,
					owner_username VARCHAR(255),
					owner_token VARCHAR(255),
					is_shared BOOLEAN NOT NULL DEFAULT FALSE,
					is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
					enabled BOOLEAN NOT NULL DEFAULT TRUE,
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
					cluster_id INTEGER,
					role VARCHAR(50) DEFAULT 'primary',
					created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					CONSTRAINT chk_owner CHECK (
						(owner_username IS NOT NULL AND owner_token IS NULL) OR
						(owner_username IS NULL AND owner_token IS NOT NULL)
					),
					CONSTRAINT chk_port CHECK (port > 0 AND port <= 65535)
				);

				COMMENT ON TABLE connections IS
					'PostgreSQL server connections that can be monitored by the collector';
				COMMENT ON COLUMN connections.id IS
					'Unique identifier for the connection';
				COMMENT ON COLUMN connections.owner_username IS
					'Username of the user who owns this connection (mutually exclusive with owner_token)';
				COMMENT ON COLUMN connections.owner_token IS
					'Service token that owns this connection (mutually exclusive with owner_username)';
				COMMENT ON COLUMN connections.is_shared IS
					'Whether the connection is shared among users or private';
				COMMENT ON COLUMN connections.is_monitored IS
					'Whether this connection is actively being monitored';
				COMMENT ON COLUMN connections.enabled IS
					'Whether this connection is enabled for alerting';
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
				COMMENT ON COLUMN connections.cluster_id IS
					'Reference to the cluster this connection belongs to (NULL if unassigned)';
				COMMENT ON COLUMN connections.role IS
					'Role of the server in the cluster (primary, replica, standby, etc.)';
				COMMENT ON COLUMN connections.created_at IS
					'Timestamp when the connection was created';
				COMMENT ON COLUMN connections.updated_at IS
					'Timestamp when the connection was last updated';
				COMMENT ON CONSTRAINT chk_owner ON connections IS
					'Ensures exactly one of owner_username or owner_token is set';
				COMMENT ON CONSTRAINT chk_port ON connections IS
					'Ensures port is in valid range (1-65535)';

				CREATE INDEX IF NOT EXISTS idx_connections_name ON connections(name);
				CREATE INDEX IF NOT EXISTS idx_connections_owner_username ON connections(owner_username);
				CREATE INDEX IF NOT EXISTS idx_connections_owner_token ON connections(owner_token);
				CREATE INDEX IF NOT EXISTS idx_connections_is_monitored ON connections(is_monitored) WHERE is_monitored = TRUE;
				CREATE INDEX IF NOT EXISTS idx_connections_enabled ON connections(enabled) WHERE enabled = TRUE;
				CREATE INDEX IF NOT EXISTS idx_connections_cluster_id ON connections(cluster_id);
				CREATE INDEX IF NOT EXISTS idx_connections_role ON connections(role);

				COMMENT ON INDEX idx_connections_name IS
					'Index for fast lookup of connections by name';
				COMMENT ON INDEX idx_connections_owner_username IS
					'Index for fast lookup of connections by owner username';
				COMMENT ON INDEX idx_connections_owner_token IS
					'Index for fast lookup of connections by owner token';
				COMMENT ON INDEX idx_connections_is_monitored IS
					'Partial index for efficiently finding actively monitored connections';
				COMMENT ON INDEX idx_connections_enabled IS
					'Partial index for efficiently finding enabled connections for alerting';
			`)
			if err != nil {
				return fmt.Errorf("failed to create connections table: %w", err)
			}

			// Create cluster_groups table
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS cluster_groups (
					id SERIAL PRIMARY KEY,
					owner_username VARCHAR(255),
					owner_token VARCHAR(255),
					is_shared BOOLEAN NOT NULL DEFAULT TRUE,
					is_default BOOLEAN NOT NULL DEFAULT FALSE,
					auto_group_key VARCHAR(255) UNIQUE,
					name VARCHAR(255) NOT NULL,
					description TEXT,
					created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					CONSTRAINT cluster_groups_name_unique UNIQUE (name)
				);

				COMMENT ON TABLE cluster_groups IS
					'Groups for organizing database clusters hierarchically';
				COMMENT ON COLUMN cluster_groups.id IS
					'Unique identifier for the cluster group';
				COMMENT ON COLUMN cluster_groups.owner_username IS
					'Username of the user who owns this cluster group';
				COMMENT ON COLUMN cluster_groups.owner_token IS
					'Token that owns this cluster group (alternative to user ownership)';
				COMMENT ON COLUMN cluster_groups.is_shared IS
					'Whether this group is shared with all users (default true)';
				COMMENT ON COLUMN cluster_groups.is_default IS
					'Whether this is the default group for ungrouped servers/clusters (only one allowed)';
				COMMENT ON COLUMN cluster_groups.auto_group_key IS
					'Key linking to auto-detected group (e.g., auto for the default Servers/Clusters group)';
				COMMENT ON COLUMN cluster_groups.name IS
					'User-friendly name for the cluster group';
				COMMENT ON COLUMN cluster_groups.description IS
					'Optional description of the cluster group';
				COMMENT ON COLUMN cluster_groups.created_at IS
					'Timestamp when the cluster group was created';
				COMMENT ON COLUMN cluster_groups.updated_at IS
					'Timestamp when the cluster group was last updated';

				CREATE INDEX IF NOT EXISTS idx_cluster_groups_name ON cluster_groups(name);
				CREATE UNIQUE INDEX IF NOT EXISTS idx_cluster_groups_is_default
					ON cluster_groups (is_default) WHERE is_default = TRUE;

				-- Insert the default group
				INSERT INTO cluster_groups (name, description, is_shared, is_default)
				VALUES ('Servers/Clusters', 'Default group for all servers and clusters', TRUE, TRUE)
				ON CONFLICT DO NOTHING;
			`)
			if err != nil {
				return fmt.Errorf("failed to create cluster_groups table: %w", err)
			}

			// Create clusters table
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS clusters (
					id SERIAL PRIMARY KEY,
					group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
					auto_cluster_key VARCHAR(255) UNIQUE,
					name VARCHAR(255) NOT NULL,
					description TEXT,
					created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					CONSTRAINT clusters_group_name_unique UNIQUE (group_id, name)
				);

				COMMENT ON TABLE clusters IS
					'Database clusters that contain one or more server connections';
				COMMENT ON COLUMN clusters.id IS
					'Unique identifier for the cluster';
				COMMENT ON COLUMN clusters.group_id IS
					'Reference to the parent cluster group';
				COMMENT ON COLUMN clusters.auto_cluster_key IS
					'Key linking to auto-detected cluster (format: type:id, e.g., binary:123, spock:pg17)';
				COMMENT ON COLUMN clusters.name IS
					'User-friendly name for the cluster';
				COMMENT ON COLUMN clusters.description IS
					'Optional description of the cluster';
				COMMENT ON COLUMN clusters.created_at IS
					'Timestamp when the cluster was created';
				COMMENT ON COLUMN clusters.updated_at IS
					'Timestamp when the cluster was last updated';

				CREATE INDEX IF NOT EXISTS idx_clusters_group_id ON clusters(group_id);
				CREATE INDEX IF NOT EXISTS idx_clusters_name ON clusters(name);

				-- Add foreign key from connections to clusters
				ALTER TABLE connections
					ADD CONSTRAINT fk_connections_cluster_id
					FOREIGN KEY (cluster_id) REFERENCES clusters(id) ON DELETE SET NULL;
			`)
			if err != nil {
				return fmt.Errorf("failed to create clusters table: %w", err)
			}

			// Create probe_configs table
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS probe_configs (
					id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
					connection_id INTEGER,
					is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
					name TEXT NOT NULL,
					description TEXT NOT NULL,
					collection_interval_seconds INTEGER NOT NULL DEFAULT 60,
					retention_days INTEGER NOT NULL DEFAULT 28,
					created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					CONSTRAINT chk_name_not_empty CHECK (name <> ''),
					CONSTRAINT chk_collection_interval_positive CHECK (collection_interval_seconds > 0),
					CONSTRAINT chk_retention_days_positive CHECK (retention_days > 0)
				);

				COMMENT ON TABLE probe_configs IS
					'Configuration for monitoring probes';
				COMMENT ON COLUMN probe_configs.id IS
					'Unique identifier for the probe configuration';
				COMMENT ON COLUMN probe_configs.connection_id IS
					'Connection ID for per-server configuration. NULL means global default.';
				COMMENT ON COLUMN probe_configs.is_enabled IS
					'Whether the probe is currently enabled';
				COMMENT ON COLUMN probe_configs.name IS
					'Unique name of the probe';
				COMMENT ON COLUMN probe_configs.description IS
					'Description of what the probe monitors';
				COMMENT ON COLUMN probe_configs.collection_interval_seconds IS
					'How often to run the probe (in seconds)';
				COMMENT ON COLUMN probe_configs.retention_days IS
					'How long to keep collected data (in days)';
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

				CREATE INDEX IF NOT EXISTS idx_probe_configs_enabled ON probe_configs(is_enabled);
				CREATE UNIQUE INDEX IF NOT EXISTS probe_configs_name_global_key ON probe_configs(name) WHERE connection_id IS NULL;
				CREATE UNIQUE INDEX IF NOT EXISTS probe_configs_name_connection_key ON probe_configs(name, COALESCE(connection_id, 0));

				COMMENT ON INDEX idx_probe_configs_enabled IS
					'Index for efficiently finding enabled probes';

				ALTER TABLE probe_configs
					ADD CONSTRAINT probe_configs_connection_id_fkey
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;
			`)
			if err != nil {
				return fmt.Errorf("failed to create probe_configs table: %w", err)
			}

			// Insert default global probe configurations
			_, err = tx.Exec(ctx, `
				INSERT INTO probe_configs (connection_id, is_enabled, name, description, collection_interval_seconds, retention_days)
				VALUES
					-- Server-scoped probes
					(NULL, TRUE, 'pg_stat_activity', 'Monitors current database activity and backend processes', 60, 7),
					(NULL, TRUE, 'pg_stat_replication', 'Monitors replication status and lag (includes WAL receiver)', 30, 7),
					(NULL, TRUE, 'pg_replication_slots', 'Monitors replication slot WAL retention and statistics', 300, 7),
					(NULL, TRUE, 'pg_stat_recovery_prefetch', 'Monitors recovery prefetch statistics', 600, 7),
					(NULL, TRUE, 'pg_stat_subscription', 'Monitors logical replication subscription status and statistics', 300, 7),
					(NULL, TRUE, 'pg_stat_connection_security', 'Monitors connection security (SSL and GSSAPI)', 300, 7),
					(NULL, TRUE, 'pg_stat_io', 'Monitors I/O statistics by backend type', 900, 7),
					(NULL, TRUE, 'pg_stat_checkpointer', 'Monitors checkpoint and background writer statistics', 600, 7),
					(NULL, TRUE, 'pg_stat_wal', 'Monitors WAL generation and archiver statistics', 600, 7),
					(NULL, TRUE, 'pg_database', 'Monitors database catalog including transaction ID wraparound metrics', 300, 7),
					-- Database-scoped probes
					(NULL, TRUE, 'pg_stat_database', 'Monitors database-wide statistics', 300, 7),
					(NULL, TRUE, 'pg_stat_database_conflicts', 'Monitors database conflicts during recovery', 300, 7),
					(NULL, TRUE, 'pg_stat_all_tables', 'Monitors table access and I/O statistics', 300, 7),
					(NULL, TRUE, 'pg_stat_all_indexes', 'Monitors index access and I/O statistics', 300, 7),
					(NULL, TRUE, 'pg_statio_all_sequences', 'Monitors sequence I/O statistics', 300, 7),
					(NULL, TRUE, 'pg_stat_user_functions', 'Monitors user-defined function statistics', 300, 7),
					(NULL, TRUE, 'pg_stat_statements', 'Monitors SQL statement execution statistics', 300, 7),
					-- Configuration probes (change-tracked)
					(NULL, TRUE, 'pg_settings', 'Monitors PostgreSQL configuration settings (change-tracked)', 3600, 365),
					(NULL, TRUE, 'pg_hba_file_rules', 'Monitors pg_hba.conf authentication rules (change-tracked)', 3600, 365),
					(NULL, TRUE, 'pg_ident_file_mappings', 'Monitors pg_ident.conf user mappings (change-tracked)', 3600, 365),
					(NULL, TRUE, 'pg_server_info', 'Server identification and configuration (change-tracked)', 3600, 365),
					(NULL, TRUE, 'pg_node_role', 'Node role detection for cluster topology', 300, 30),
					(NULL, TRUE, 'pg_extension', 'Monitors installed PostgreSQL extensions and versions', 3600, 30),
					-- System Stats Extension probes
					(NULL, TRUE, 'pg_sys_os_info', 'Monitors operating system information', 600, 7),
					(NULL, TRUE, 'pg_sys_cpu_info', 'Monitors CPU information and configuration', 600, 7),
					(NULL, TRUE, 'pg_sys_cpu_usage_info', 'Monitors CPU usage statistics', 600, 7),
					(NULL, TRUE, 'pg_sys_memory_info', 'Monitors memory usage statistics', 600, 7),
					(NULL, TRUE, 'pg_sys_io_analysis_info', 'Monitors I/O analysis statistics', 600, 7),
					(NULL, TRUE, 'pg_sys_disk_info', 'Monitors disk information and usage', 600, 7),
					(NULL, TRUE, 'pg_sys_load_avg_info', 'Monitors system load averages', 600, 7),
					(NULL, TRUE, 'pg_sys_process_info', 'Monitors process information', 600, 7),
					(NULL, TRUE, 'pg_sys_network_info', 'Monitors network statistics', 600, 7),
					(NULL, TRUE, 'pg_sys_cpu_memory_by_process', 'Monitors CPU and memory usage by process', 600, 7)
				ON CONFLICT DO NOTHING;
			`)
			if err != nil {
				return fmt.Errorf("failed to insert default probe configurations: %w", err)
			}

			// Create metrics schema
			_, err = tx.Exec(ctx, `
				CREATE SCHEMA IF NOT EXISTS metrics;
				COMMENT ON SCHEMA metrics IS
					'Schema for storing monitoring probe metrics data';
			`)
			if err != nil {
				return fmt.Errorf("failed to create metrics schema: %w", err)
			}

			// =====================================================================
			// PART 2: Metrics Tables - PostgreSQL Statistics
			// =====================================================================

			// metrics.pg_stat_activity
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_activity (
					connection_id INTEGER NOT NULL,
					pid INTEGER NOT NULL,
					datid OID,
					datname TEXT,
					usesysid OID,
					usename TEXT,
					state TEXT,
					application_name TEXT,
					client_addr INET,
					client_hostname TEXT,
					client_port INTEGER,
					leader_pid INTEGER,
					wait_event_type TEXT,
					wait_event TEXT,
					backend_type TEXT,
					backend_xid TEXT,
					backend_xmin TEXT,
					query TEXT,
					backend_start TIMESTAMPTZ,
					xact_start TIMESTAMPTZ,
					query_start TIMESTAMPTZ,
					state_change TIMESTAMPTZ,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, pid)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_activity IS
					'Metrics collected from pg_stat_activity view, showing current server activity';

				CREATE INDEX IF NOT EXISTS idx_pg_stat_activity_conn_time
					ON metrics.pg_stat_activity(connection_id, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_activity table: %w", err)
			}

			// metrics.pg_stat_all_tables (consolidated with pg_statio_all_tables)
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_all_tables (
					connection_id INTEGER NOT NULL,
					database_name TEXT NOT NULL,
					schemaname TEXT NOT NULL,
					relname TEXT NOT NULL,
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
					vacuum_count BIGINT,
					autovacuum_count BIGINT,
					analyze_count BIGINT,
					autoanalyze_count BIGINT,
					heap_blks_read BIGINT,
					heap_blks_hit BIGINT,
					idx_blks_read BIGINT,
					idx_blks_hit BIGINT,
					toast_blks_read BIGINT,
					toast_blks_hit BIGINT,
					tidx_blks_read BIGINT,
					tidx_blks_hit BIGINT,
					last_vacuum TIMESTAMPTZ,
					last_autovacuum TIMESTAMPTZ,
					last_analyze TIMESTAMPTZ,
					last_autoanalyze TIMESTAMPTZ,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, database_name, collected_at, schemaname, relname)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_all_tables IS
					'Table-level access and I/O statistics per database';

				CREATE INDEX IF NOT EXISTS idx_pg_stat_all_tables_conn_time
					ON metrics.pg_stat_all_tables(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_stat_all_tables_conn_db_time
					ON metrics.pg_stat_all_tables(connection_id, database_name, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_stat_all_tables_object
					ON metrics.pg_stat_all_tables(connection_id, database_name, schemaname, relname, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_all_tables table: %w", err)
			}

			// metrics.pg_stat_all_indexes (consolidated with pg_statio_all_indexes)
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_all_indexes (
					connection_id INTEGER NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					indexrelid OID NOT NULL,
					relid OID,
					schemaname TEXT,
					relname TEXT,
					indexrelname TEXT,
					idx_scan BIGINT,
					idx_tup_read BIGINT,
					idx_tup_fetch BIGINT,
					idx_blks_read BIGINT,
					idx_blks_hit BIGINT,
					last_idx_scan TIMESTAMPTZ,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, database_name, indexrelid)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_all_indexes IS
					'Index access and I/O statistics for all databases';

				ALTER TABLE metrics.pg_stat_all_indexes
					ADD CONSTRAINT fk_pg_stat_all_indexes_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_stat_all_indexes_conn_time
					ON metrics.pg_stat_all_indexes(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_stat_all_indexes_conn_db_time
					ON metrics.pg_stat_all_indexes(connection_id, database_name, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_stat_all_indexes_object
					ON metrics.pg_stat_all_indexes(connection_id, database_name, schemaname, indexrelname, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_all_indexes table: %w", err)
			}

			// metrics.pg_stat_statements
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_statements (
					connection_id INTEGER NOT NULL,
					database_name TEXT NOT NULL,
					userid OID NOT NULL,
					dbid OID NOT NULL,
					queryid BIGINT NOT NULL,
					toplevel BOOLEAN NOT NULL DEFAULT TRUE,
					query TEXT,
					calls BIGINT,
					rows BIGINT,
					total_exec_time DOUBLE PRECISION,
					mean_exec_time DOUBLE PRECISION,
					min_exec_time DOUBLE PRECISION,
					max_exec_time DOUBLE PRECISION,
					stddev_exec_time DOUBLE PRECISION,
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
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, database_name, collected_at, queryid, userid, dbid, toplevel)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_statements IS
					'Metrics collected from pg_stat_statements extension, showing query execution statistics per database';

				CREATE INDEX IF NOT EXISTS idx_pg_stat_statements_conn_time
					ON metrics.pg_stat_statements(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_stat_statements_conn_db_time
					ON metrics.pg_stat_statements(connection_id, database_name, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_stat_statements_object
					ON metrics.pg_stat_statements(connection_id, database_name, queryid, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_statements table: %w", err)
			}

			// metrics.pg_stat_database
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_database (
					connection_id INTEGER NOT NULL,
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
					blk_read_time DOUBLE PRECISION,
					blk_write_time DOUBLE PRECISION,
					session_time DOUBLE PRECISION,
					active_time DOUBLE PRECISION,
					idle_in_transaction_time DOUBLE PRECISION,
					sessions BIGINT,
					sessions_abandoned BIGINT,
					sessions_fatal BIGINT,
					sessions_killed BIGINT,
					checksum_last_failure TIMESTAMPTZ,
					stats_reset TIMESTAMPTZ,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, database_name)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_database IS 'Per-database statistics';

				ALTER TABLE metrics.pg_stat_database
					ADD CONSTRAINT fk_pg_stat_database_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_stat_database_conn_time
					ON metrics.pg_stat_database(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_stat_database_conn_db_time
					ON metrics.pg_stat_database(connection_id, database_name, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_database table: %w", err)
			}

			// metrics.pg_stat_database_conflicts
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_database_conflicts (
					connection_id INTEGER NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					datid OID,
					datname TEXT,
					confl_tablespace BIGINT,
					confl_lock BIGINT,
					confl_snapshot BIGINT,
					confl_bufferpin BIGINT,
					confl_deadlock BIGINT,
					confl_active_logicalslot BIGINT,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, database_name)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_database_conflicts IS
					'Database conflict statistics (standby servers)';

				ALTER TABLE metrics.pg_stat_database_conflicts
					ADD CONSTRAINT fk_pg_stat_database_conflicts_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_stat_database_conflicts_conn_time
					ON metrics.pg_stat_database_conflicts(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_stat_database_conflicts_conn_db_time
					ON metrics.pg_stat_database_conflicts(connection_id, database_name, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_database_conflicts table: %w", err)
			}

			// metrics.pg_stat_checkpointer (consolidated with pg_stat_bgwriter)
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_checkpointer (
					connection_id INTEGER NOT NULL,
					num_timed BIGINT,
					num_requested BIGINT,
					restartpoints_timed BIGINT,
					restartpoints_req BIGINT,
					restartpoints_done BIGINT,
					write_time DOUBLE PRECISION,
					sync_time DOUBLE PRECISION,
					buffers_written BIGINT,
					buffers_clean BIGINT,
					maxwritten_clean BIGINT,
					buffers_alloc BIGINT,
					stats_reset TIMESTAMPTZ,
					bgwriter_stats_reset TIMESTAMPTZ,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_checkpointer IS
					'Checkpointer and background writer statistics';

				ALTER TABLE metrics.pg_stat_checkpointer
					ADD CONSTRAINT fk_pg_stat_checkpointer_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_stat_checkpointer_conn_time
					ON metrics.pg_stat_checkpointer(connection_id, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_checkpointer table: %w", err)
			}

			// metrics.pg_stat_wal (consolidated with pg_stat_archiver)
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_wal (
					connection_id INTEGER NOT NULL,
					wal_records BIGINT,
					wal_fpi BIGINT,
					wal_bytes NUMERIC,
					wal_buffers_full BIGINT,
					wal_write BIGINT,
					wal_sync BIGINT,
					wal_write_time DOUBLE PRECISION,
					wal_sync_time DOUBLE PRECISION,
					archived_count BIGINT,
					last_archived_wal TEXT,
					last_archived_time TIMESTAMPTZ,
					failed_count BIGINT,
					last_failed_wal TEXT,
					last_failed_time TIMESTAMPTZ,
					stats_reset TIMESTAMPTZ,
					archiver_stats_reset TIMESTAMPTZ,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_wal IS
					'WAL generation and archiver statistics';

				ALTER TABLE metrics.pg_stat_wal
					ADD CONSTRAINT fk_pg_stat_wal_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_stat_wal_conn_time
					ON metrics.pg_stat_wal(connection_id, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_wal table: %w", err)
			}

			// metrics.pg_stat_replication (consolidated with pg_stat_wal_receiver)
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_replication (
					connection_id INTEGER NOT NULL,
					pid INTEGER NOT NULL,
					usesysid OID,
					usename TEXT,
					state TEXT,
					application_name TEXT,
					client_addr INET,
					client_hostname TEXT,
					client_port INTEGER,
					backend_xmin TEXT,
					sent_lsn TEXT,
					write_lsn TEXT,
					flush_lsn TEXT,
					replay_lsn TEXT,
					write_lag INTERVAL,
					flush_lag INTERVAL,
					replay_lag INTERVAL,
					sync_priority INTEGER,
					sync_state TEXT,
					backend_start TIMESTAMPTZ,
					reply_time TIMESTAMPTZ,
					role TEXT,
					receiver_pid INTEGER,
					receiver_status TEXT,
					receive_start_lsn TEXT,
					receive_start_tli INTEGER,
					written_lsn TEXT,
					receiver_flushed_lsn TEXT,
					received_tli INTEGER,
					last_msg_send_time TIMESTAMPTZ,
					last_msg_receipt_time TIMESTAMPTZ,
					latest_end_lsn TEXT,
					latest_end_time TIMESTAMPTZ,
					receiver_slot_name TEXT,
					sender_host TEXT,
					sender_port INTEGER,
					conninfo TEXT,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, pid)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_replication IS
					'Replication statistics for senders and receivers';

				ALTER TABLE metrics.pg_stat_replication
					ADD CONSTRAINT fk_pg_stat_replication_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_stat_replication_conn_time
					ON metrics.pg_stat_replication(connection_id, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_replication table: %w", err)
			}

			// metrics.pg_replication_slots (consolidated with pg_stat_replication_slots)
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_replication_slots (
					connection_id INTEGER NOT NULL,
					slot_name TEXT NOT NULL,
					slot_type TEXT,
					active BOOLEAN,
					wal_status TEXT,
					safe_wal_size BIGINT,
					retained_bytes NUMERIC,
					spill_txns BIGINT,
					spill_count BIGINT,
					spill_bytes BIGINT,
					stream_txns BIGINT,
					stream_count BIGINT,
					stream_bytes BIGINT,
					total_txns BIGINT,
					total_count BIGINT,
					total_bytes BIGINT,
					stats_reset TIMESTAMPTZ,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, slot_name)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_replication_slots IS
					'Replication slot WAL retention and statistics metrics';

				ALTER TABLE metrics.pg_replication_slots
					ADD CONSTRAINT fk_pg_replication_slots_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_replication_slots_conn_time
					ON metrics.pg_replication_slots(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_replication_slots_object
					ON metrics.pg_replication_slots(connection_id, slot_name, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_replication_slots table: %w", err)
			}

			// metrics.pg_stat_subscription (consolidated with pg_stat_subscription_stats)
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_subscription (
					connection_id INTEGER NOT NULL,
					subid OID NOT NULL,
					subname TEXT,
					worker_type TEXT,
					pid INTEGER,
					leader_pid INTEGER,
					relid OID,
					received_lsn TEXT,
					latest_end_lsn TEXT,
					last_msg_send_time TIMESTAMPTZ,
					last_msg_receipt_time TIMESTAMPTZ,
					latest_end_time TIMESTAMPTZ,
					apply_error_count BIGINT,
					sync_error_count BIGINT,
					stats_reset TIMESTAMPTZ,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, subid)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_subscription IS
					'Logical replication subscription statistics';

				ALTER TABLE metrics.pg_stat_subscription
					ADD CONSTRAINT fk_pg_stat_subscription_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_stat_subscription_conn_time
					ON metrics.pg_stat_subscription(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_stat_subscription_object
					ON metrics.pg_stat_subscription(connection_id, subid, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_subscription table: %w", err)
			}

			// metrics.pg_stat_recovery_prefetch
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_recovery_prefetch (
					connection_id INTEGER NOT NULL,
					prefetch BIGINT,
					hit BIGINT,
					skip_init BIGINT,
					skip_new BIGINT,
					skip_fpw BIGINT,
					skip_rep BIGINT,
					wal_distance BIGINT,
					block_distance BIGINT,
					io_depth BIGINT,
					stats_reset TIMESTAMPTZ,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_recovery_prefetch IS
					'Recovery prefetch statistics (PG 15+)';

				ALTER TABLE metrics.pg_stat_recovery_prefetch
					ADD CONSTRAINT fk_pg_stat_recovery_prefetch_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_stat_recovery_prefetch_conn_time
					ON metrics.pg_stat_recovery_prefetch(connection_id, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_recovery_prefetch table: %w", err)
			}

			// metrics.pg_stat_io
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_io (
					connection_id INTEGER NOT NULL,
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
					stats_reset TIMESTAMPTZ,
					blks_zeroed BIGINT,
					blks_exists BIGINT,
					flushes BIGINT,
					truncates BIGINT,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, backend_type, object, context)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_io IS
					'I/O statistics by backend type and context. When backend_type is slru, the object column contains the SLRU cache name rather than an I/O object type';

				ALTER TABLE metrics.pg_stat_io
					ADD CONSTRAINT fk_pg_stat_io_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_stat_io_conn_time
					ON metrics.pg_stat_io(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_stat_io_object
					ON metrics.pg_stat_io(connection_id, backend_type, object, context, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_io table: %w", err)
			}

			// metrics.pg_stat_connection_security (merged from pg_stat_ssl and pg_stat_gssapi)
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_connection_security (
					connection_id INTEGER NOT NULL,
					pid INTEGER NOT NULL,
					ssl BOOLEAN,
					ssl_version TEXT,
					cipher TEXT,
					bits INTEGER,
					client_dn TEXT,
					client_serial TEXT,
					issuer_dn TEXT,
					gss_authenticated BOOLEAN,
					principal TEXT,
					gss_encrypted BOOLEAN,
					credentials_delegated BOOLEAN,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, pid)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_connection_security IS
					'Combined SSL and GSSAPI connection security statistics';

				ALTER TABLE metrics.pg_stat_connection_security
					ADD CONSTRAINT fk_pg_stat_connection_security_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_stat_connection_security_conn_time
					ON metrics.pg_stat_connection_security(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_stat_connection_security_object
					ON metrics.pg_stat_connection_security(connection_id, pid, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_connection_security table: %w", err)
			}

			// metrics.pg_stat_user_functions
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_stat_user_functions (
					connection_id INTEGER NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					funcid OID NOT NULL,
					schemaname TEXT,
					funcname TEXT,
					calls BIGINT,
					total_time DOUBLE PRECISION,
					self_time DOUBLE PRECISION,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, database_name, funcid)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_stat_user_functions IS
					'Statistics for user-defined functions';

				ALTER TABLE metrics.pg_stat_user_functions
					ADD CONSTRAINT fk_pg_stat_user_functions_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_stat_user_functions_conn_time
					ON metrics.pg_stat_user_functions(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_stat_user_functions_conn_db_time
					ON metrics.pg_stat_user_functions(connection_id, database_name, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_stat_user_functions_object
					ON metrics.pg_stat_user_functions(connection_id, database_name, schemaname, funcname, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_user_functions table: %w", err)
			}

			// metrics.pg_statio_all_sequences
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_statio_all_sequences (
					connection_id INTEGER NOT NULL,
					database_name VARCHAR(255) NOT NULL,
					relid OID NOT NULL,
					schemaname TEXT,
					relname TEXT,
					blks_read BIGINT,
					blks_hit BIGINT,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, database_name, relid)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_statio_all_sequences IS
					'I/O statistics for all sequences';

				ALTER TABLE metrics.pg_statio_all_sequences
					ADD CONSTRAINT fk_pg_statio_all_sequences_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_statio_all_sequences_conn_time
					ON metrics.pg_statio_all_sequences(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_statio_all_sequences_conn_db_time
					ON metrics.pg_statio_all_sequences(connection_id, database_name, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_statio_all_sequences table: %w", err)
			}

			// metrics.pg_database
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_database (
					connection_id INTEGER NOT NULL,
					datname TEXT NOT NULL,
					datdba OID,
					encoding INTEGER,
					datlocprovider "char",
					datistemplate BOOLEAN,
					datallowconn BOOLEAN,
					datconnlimit INTEGER,
					datfrozenxid XID,
					datminmxid XID,
					dattablespace OID,
					age_datfrozenxid BIGINT,
					age_datminmxid BIGINT,
					database_size_bytes BIGINT,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, datname)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_database IS
					'Stores pg_database catalog metrics including transaction ID wraparound indicators';

				ALTER TABLE metrics.pg_database
					ADD CONSTRAINT fk_pg_database_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_database_conn_time
					ON metrics.pg_database(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_database_conn_db_time
					ON metrics.pg_database(connection_id, datname, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_database table: %w", err)
			}

			// metrics.pg_settings
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_settings (
					connection_id INTEGER NOT NULL,
					name TEXT NOT NULL,
					setting TEXT,
					unit TEXT,
					category TEXT,
					short_desc TEXT,
					extra_desc TEXT,
					context TEXT,
					vartype TEXT,
					source TEXT,
					min_val TEXT,
					max_val TEXT,
					enumvals TEXT[],
					boot_val TEXT,
					reset_val TEXT,
					sourcefile TEXT,
					sourceline INTEGER,
					pending_restart BOOLEAN,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, name)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_settings IS
					'PostgreSQL configuration settings - only stores snapshots when changes are detected';

				ALTER TABLE metrics.pg_settings
					ADD CONSTRAINT fk_pg_settings_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_settings_conn_time
					ON metrics.pg_settings(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_settings_object
					ON metrics.pg_settings(connection_id, name, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_settings table: %w", err)
			}

			// metrics.pg_hba_file_rules
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_hba_file_rules (
					connection_id INTEGER NOT NULL,
					rule_number INTEGER NOT NULL,
					file_name TEXT,
					line_number INTEGER,
					type TEXT,
					database TEXT[],
					user_name TEXT[],
					address TEXT,
					netmask TEXT,
					auth_method TEXT,
					options TEXT[],
					error TEXT,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, rule_number)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_hba_file_rules IS
					'PostgreSQL HBA configuration rules - only stores snapshots when changes are detected';

				ALTER TABLE metrics.pg_hba_file_rules
					ADD CONSTRAINT fk_pg_hba_file_rules_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_hba_file_rules_conn_time
					ON metrics.pg_hba_file_rules(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_hba_file_rules_object
					ON metrics.pg_hba_file_rules(connection_id, rule_number, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_hba_file_rules table: %w", err)
			}

			// metrics.pg_ident_file_mappings
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_ident_file_mappings (
					connection_id INTEGER NOT NULL,
					map_number INTEGER NOT NULL,
					file_name TEXT,
					line_number INTEGER,
					map_name TEXT,
					sys_name TEXT,
					pg_username TEXT,
					error TEXT,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, map_number)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_ident_file_mappings IS
					'PostgreSQL ident mapping configuration - only stores snapshots when changes are detected';

				ALTER TABLE metrics.pg_ident_file_mappings
					ADD CONSTRAINT fk_pg_ident_file_mappings_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_ident_file_mappings_conn_time
					ON metrics.pg_ident_file_mappings(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_ident_file_mappings_object
					ON metrics.pg_ident_file_mappings(connection_id, map_number, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_ident_file_mappings table: %w", err)
			}

			// metrics.pg_server_info
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_server_info (
					connection_id INTEGER NOT NULL,
					server_version TEXT,
					server_version_num INTEGER,
					system_identifier BIGINT,
					cluster_name TEXT,
					data_directory TEXT,
					max_connections INTEGER,
					max_wal_senders INTEGER,
					max_replication_slots INTEGER,
					installed_extensions TEXT[],
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_server_info IS
					'Server identification and configuration - only stores snapshots when changes detected';

				ALTER TABLE metrics.pg_server_info
					ADD CONSTRAINT fk_pg_server_info_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_server_info_conn_time
					ON metrics.pg_server_info(connection_id, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_server_info table: %w", err)
			}

			// metrics.pg_node_role
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_node_role (
					connection_id INTEGER NOT NULL,
					is_in_recovery BOOLEAN NOT NULL,
					timeline_id INTEGER,
					has_binary_standbys BOOLEAN NOT NULL DEFAULT FALSE,
					binary_standby_count INTEGER DEFAULT 0,
					is_streaming_standby BOOLEAN NOT NULL DEFAULT FALSE,
					upstream_host TEXT,
					upstream_port INTEGER,
					received_lsn TEXT,
					replayed_lsn TEXT,
					publication_count INTEGER DEFAULT 0,
					subscription_count INTEGER DEFAULT 0,
					active_subscription_count INTEGER DEFAULT 0,
					has_spock BOOLEAN NOT NULL DEFAULT FALSE,
					spock_node_id BIGINT,
					spock_node_name TEXT,
					spock_subscription_count INTEGER DEFAULT 0,
					has_bdr BOOLEAN NOT NULL DEFAULT FALSE,
					bdr_node_id TEXT,
					bdr_node_name TEXT,
					bdr_node_group TEXT,
					bdr_node_state TEXT,
					primary_role TEXT NOT NULL,
					role_flags TEXT[] NOT NULL DEFAULT '{}',
					role_details JSONB,
					publisher_host VARCHAR(255),
					publisher_port INTEGER,
					has_active_logical_slots BOOLEAN DEFAULT FALSE,
					active_logical_slot_count INTEGER DEFAULT 0,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_node_role IS
					'Node role detection for cluster topology analysis';

				ALTER TABLE metrics.pg_node_role
					ADD CONSTRAINT fk_pg_node_role_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_node_role_conn_time
					ON metrics.pg_node_role(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_node_role_primary_role
					ON metrics.pg_node_role(connection_id, primary_role, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_node_role table: %w", err)
			}

			// metrics.pg_extension
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_extension (
					connection_id INTEGER NOT NULL,
					database_name TEXT NOT NULL,
					extname TEXT NOT NULL,
					extversion TEXT,
					extrelocatable BOOLEAN,
					schema_name TEXT,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, database_name, extname, collected_at)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_extension IS
					'Installed PostgreSQL extensions and their versions per database';

				ALTER TABLE metrics.pg_extension
					ADD CONSTRAINT fk_pg_extension_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_extension_conn_time
					ON metrics.pg_extension(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_extension_extname
					ON metrics.pg_extension(connection_id, database_name, extname);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_extension table: %w", err)
			}

			// =====================================================================
			// PART 3: System Stats Extension Tables
			// =====================================================================

			// metrics.pg_sys_cpu_info
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_sys_cpu_info (
					connection_id INTEGER NOT NULL,
					vendor TEXT,
					description TEXT,
					model_name TEXT,
					processor_type TEXT,
					logical_processor INTEGER,
					physical_processor INTEGER,
					no_of_cores INTEGER,
					architecture TEXT,
					clock_speed_hz BIGINT,
					cpu_type TEXT,
					cpu_family TEXT,
					byte_order TEXT,
					l1dcache_size BIGINT,
					l1icache_size BIGINT,
					l2cache_size BIGINT,
					l3cache_size BIGINT,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at)
				) PARTITION BY RANGE (collected_at);

				CREATE INDEX IF NOT EXISTS idx_pg_sys_cpu_info_conn_time
					ON metrics.pg_sys_cpu_info(connection_id, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_sys_cpu_info table: %w", err)
			}

			// metrics.pg_sys_cpu_usage_info
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_sys_cpu_usage_info (
					connection_id INTEGER NOT NULL,
					usermode_normal_process_percent REAL,
					usermode_niced_process_percent REAL,
					kernelmode_process_percent REAL,
					io_completion_percent REAL,
					servicing_irq_percent REAL,
					servicing_softirq_percent REAL,
					idle_mode_percent REAL,
					user_time_percent REAL,
					processor_time_percent REAL,
					privileged_time_percent REAL,
					interrupt_time_percent REAL,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_sys_cpu_usage_info IS
					'CPU usage statistics collected via system_stats extension';

				ALTER TABLE metrics.pg_sys_cpu_usage_info
					ADD CONSTRAINT fk_pg_sys_cpu_usage_info_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_sys_cpu_usage_info_conn_time
					ON metrics.pg_sys_cpu_usage_info(connection_id, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_sys_cpu_usage_info table: %w", err)
			}

			// metrics.pg_sys_cpu_memory_by_process
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_sys_cpu_memory_by_process (
					connection_id INTEGER NOT NULL,
					pid INTEGER NOT NULL,
					name TEXT,
					running_since_seconds BIGINT,
					cpu_usage REAL,
					memory_usage REAL,
					memory_bytes BIGINT,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, pid)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_sys_cpu_memory_by_process IS
					'Per-process CPU and memory usage collected via system_stats extension';

				ALTER TABLE metrics.pg_sys_cpu_memory_by_process
					ADD CONSTRAINT fk_pg_sys_cpu_memory_by_process_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_sys_cpu_memory_by_process_conn_time
					ON metrics.pg_sys_cpu_memory_by_process(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_sys_cpu_memory_by_process_object
					ON metrics.pg_sys_cpu_memory_by_process(connection_id, pid, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_sys_cpu_memory_by_process table: %w", err)
			}

			// metrics.pg_sys_memory_info
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_sys_memory_info (
					connection_id INTEGER NOT NULL,
					total_memory BIGINT,
					used_memory BIGINT,
					free_memory BIGINT,
					swap_total BIGINT,
					swap_used BIGINT,
					swap_free BIGINT,
					cache_total BIGINT,
					kernel_total BIGINT,
					kernel_paged BIGINT,
					kernel_non_paged BIGINT,
					total_page_file BIGINT,
					avail_page_file BIGINT,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at)
				) PARTITION BY RANGE (collected_at);

				CREATE INDEX IF NOT EXISTS idx_pg_sys_memory_info_conn_time
					ON metrics.pg_sys_memory_info(connection_id, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_sys_memory_info table: %w", err)
			}

			// metrics.pg_sys_disk_info
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_sys_disk_info (
					connection_id INTEGER NOT NULL,
					mount_point TEXT NOT NULL,
					drive_letter TEXT,
					file_system TEXT,
					drive_type TEXT,
					total_space BIGINT,
					used_space BIGINT,
					free_space BIGINT,
					total_inodes BIGINT,
					used_inodes BIGINT,
					free_inodes BIGINT,
					file_system_type TEXT,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, mount_point)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_sys_disk_info IS
					'Disk information collected via system_stats extension';

				ALTER TABLE metrics.pg_sys_disk_info
					ADD CONSTRAINT fk_pg_sys_disk_info_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_sys_disk_info_conn_time
					ON metrics.pg_sys_disk_info(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_sys_disk_info_object
					ON metrics.pg_sys_disk_info(connection_id, mount_point, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_sys_disk_info table: %w", err)
			}

			// metrics.pg_sys_io_analysis_info
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_sys_io_analysis_info (
					connection_id INTEGER NOT NULL,
					device_name TEXT NOT NULL,
					total_reads BIGINT,
					total_writes BIGINT,
					read_bytes BIGINT,
					write_bytes BIGINT,
					read_time_ms BIGINT,
					write_time_ms BIGINT,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, device_name)
				) PARTITION BY RANGE (collected_at);

				CREATE INDEX IF NOT EXISTS idx_pg_sys_io_analysis_info_conn_time
					ON metrics.pg_sys_io_analysis_info(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_sys_io_analysis_info_object
					ON metrics.pg_sys_io_analysis_info(connection_id, device_name, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_sys_io_analysis_info table: %w", err)
			}

			// metrics.pg_sys_load_avg_info
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_sys_load_avg_info (
					connection_id INTEGER NOT NULL,
					load_avg_one_minute REAL,
					load_avg_five_minutes REAL,
					load_avg_ten_minutes REAL,
					load_avg_fifteen_minutes REAL,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_sys_load_avg_info IS
					'System load average collected via system_stats extension';

				ALTER TABLE metrics.pg_sys_load_avg_info
					ADD CONSTRAINT fk_pg_sys_load_avg_info_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_sys_load_avg_info_conn_time
					ON metrics.pg_sys_load_avg_info(connection_id, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_sys_load_avg_info table: %w", err)
			}

			// metrics.pg_sys_network_info
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_sys_network_info (
					connection_id INTEGER NOT NULL,
					interface_name TEXT NOT NULL,
					ip_address TEXT,
					tx_bytes BIGINT,
					tx_packets BIGINT,
					tx_errors BIGINT,
					tx_dropped BIGINT,
					rx_bytes BIGINT,
					rx_packets BIGINT,
					rx_errors BIGINT,
					rx_dropped BIGINT,
					link_speed_mbps INTEGER,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at, interface_name)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_sys_network_info IS
					'Network information collected via system_stats extension';

				ALTER TABLE metrics.pg_sys_network_info
					ADD CONSTRAINT fk_pg_sys_network_info_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_sys_network_info_conn_time
					ON metrics.pg_sys_network_info(connection_id, collected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_pg_sys_network_info_object
					ON metrics.pg_sys_network_info(connection_id, interface_name, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_sys_network_info table: %w", err)
			}

			// metrics.pg_sys_os_info
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_sys_os_info (
					connection_id INTEGER NOT NULL,
					name TEXT,
					version TEXT,
					host_name TEXT,
					domain_name TEXT,
					handle_count BIGINT,
					process_count BIGINT,
					thread_count BIGINT,
					architecture TEXT,
					os_up_since_seconds BIGINT,
					last_bootup_time TIMESTAMPTZ,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_sys_os_info IS
					'OS information collected via system_stats extension';

				ALTER TABLE metrics.pg_sys_os_info
					ADD CONSTRAINT fk_pg_sys_os_info_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_sys_os_info_conn_time
					ON metrics.pg_sys_os_info(connection_id, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_sys_os_info table: %w", err)
			}

			// metrics.pg_sys_process_info
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metrics.pg_sys_process_info (
					connection_id INTEGER NOT NULL,
					total_processes INTEGER,
					running_processes INTEGER,
					sleeping_processes INTEGER,
					stopped_processes INTEGER,
					zombie_processes INTEGER,
					collected_at TIMESTAMPTZ NOT NULL,
					PRIMARY KEY (connection_id, collected_at)
				) PARTITION BY RANGE (collected_at);

				COMMENT ON TABLE metrics.pg_sys_process_info IS
					'Process information collected via system_stats extension';

				ALTER TABLE metrics.pg_sys_process_info
					ADD CONSTRAINT fk_pg_sys_process_info_connection_id
					FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

				CREATE INDEX IF NOT EXISTS idx_pg_sys_process_info_conn_time
					ON metrics.pg_sys_process_info(connection_id, collected_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create pg_sys_process_info table: %w", err)
			}

			// =====================================================================
			// PART 4: Alerting Tables
			// =====================================================================

			// alerter_settings
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS alerter_settings (
					id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
					retention_days INTEGER NOT NULL DEFAULT 90,
					default_anomaly_enabled BOOLEAN NOT NULL DEFAULT TRUE,
					default_anomaly_sensitivity REAL NOT NULL DEFAULT 3.0,
					baseline_refresh_interval_mins INTEGER NOT NULL DEFAULT 60,
					correlation_window_seconds INTEGER NOT NULL DEFAULT 120,
					updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
				);

				COMMENT ON TABLE alerter_settings IS
					'Global settings for the alerter service (singleton table)';

				INSERT INTO alerter_settings (id) VALUES (1) ON CONFLICT DO NOTHING;
			`)
			if err != nil {
				return fmt.Errorf("failed to create alerter_settings table: %w", err)
			}

			// probe_availability
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS probe_availability (
					id BIGSERIAL PRIMARY KEY,
					connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
					database_name TEXT,
					probe_name TEXT NOT NULL,
					extension_name TEXT,
					is_available BOOLEAN NOT NULL DEFAULT FALSE,
					last_checked TIMESTAMPTZ,
					last_collected TIMESTAMPTZ,
					unavailable_reason TEXT,
					UNIQUE(connection_id, database_name, probe_name)
				);

				COMMENT ON TABLE probe_availability IS
					'Tracks which probes have collected data for each connection/database';

				CREATE INDEX IF NOT EXISTS idx_probe_availability_connection
					ON probe_availability(connection_id);
				CREATE INDEX IF NOT EXISTS idx_probe_availability_probe
					ON probe_availability(probe_name);
			`)
			if err != nil {
				return fmt.Errorf("failed to create probe_availability table: %w", err)
			}

			// alert_rules
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS alert_rules (
					id BIGSERIAL PRIMARY KEY,
					name TEXT NOT NULL UNIQUE,
					description TEXT NOT NULL,
					category TEXT NOT NULL,
					metric_name TEXT NOT NULL,
					metric_unit TEXT,
					default_operator TEXT NOT NULL CHECK (default_operator IN ('>', '>=', '<', '<=', '==', '!=')),
					default_threshold REAL NOT NULL,
					default_severity TEXT NOT NULL CHECK (default_severity IN ('info', 'warning', 'critical')),
					default_enabled BOOLEAN NOT NULL DEFAULT TRUE,
					required_extension TEXT,
					is_built_in BOOLEAN NOT NULL DEFAULT FALSE,
					created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
				);

				COMMENT ON TABLE alert_rules IS
					'Threshold-based alert rules for monitored metrics';

				CREATE INDEX IF NOT EXISTS idx_alert_rules_category ON alert_rules(category);
				CREATE INDEX IF NOT EXISTS idx_alert_rules_metric ON alert_rules(metric_name);
				CREATE INDEX IF NOT EXISTS idx_alert_rules_enabled ON alert_rules(default_enabled) WHERE default_enabled = TRUE;
			`)
			if err != nil {
				return fmt.Errorf("failed to create alert_rules table: %w", err)
			}

			// alert_thresholds
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS alert_thresholds (
					id BIGSERIAL PRIMARY KEY,
					rule_id BIGINT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
					connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
					database_name TEXT,
					operator TEXT NOT NULL CHECK (operator IN ('>', '>=', '<', '<=', '==', '!=')),
					threshold REAL NOT NULL,
					severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
					enabled BOOLEAN NOT NULL DEFAULT TRUE,
					created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					UNIQUE(rule_id, connection_id, database_name)
				);

				COMMENT ON TABLE alert_thresholds IS
					'Per-connection threshold overrides for alert rules';

				CREATE INDEX IF NOT EXISTS idx_alert_thresholds_rule ON alert_thresholds(rule_id);
				CREATE INDEX IF NOT EXISTS idx_alert_thresholds_connection ON alert_thresholds(connection_id);
			`)
			if err != nil {
				return fmt.Errorf("failed to create alert_thresholds table: %w", err)
			}

			// alerts
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS alerts (
					id BIGSERIAL PRIMARY KEY,
					alert_type TEXT NOT NULL CHECK (alert_type IN ('threshold', 'anomaly')),
					rule_id BIGINT REFERENCES alert_rules(id) ON DELETE SET NULL,
					connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
					database_name TEXT,
					probe_name TEXT,
					metric_name TEXT,
					metric_value REAL,
					threshold_value REAL,
					operator TEXT,
					severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
					title TEXT NOT NULL,
					description TEXT NOT NULL,
					object_name TEXT,
					correlation_id TEXT,
					status TEXT NOT NULL CHECK (status IN ('active', 'cleared', 'acknowledged')),
					triggered_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					cleared_at TIMESTAMPTZ,
					last_updated TIMESTAMPTZ,
					anomaly_score REAL,
					anomaly_details JSONB
				);

				COMMENT ON TABLE alerts IS
					'Active and historical alerts from threshold and anomaly detection';

				CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts(status);
				CREATE INDEX IF NOT EXISTS idx_alerts_connection ON alerts(connection_id);
				CREATE INDEX IF NOT EXISTS idx_alerts_triggered ON alerts(triggered_at DESC);
				CREATE INDEX IF NOT EXISTS idx_alerts_active ON alerts(connection_id, status) WHERE status = 'active';
				CREATE INDEX IF NOT EXISTS idx_alerts_correlation ON alerts(correlation_id) WHERE correlation_id IS NOT NULL;
				CREATE INDEX IF NOT EXISTS idx_alerts_object_name ON alerts(object_name) WHERE object_name IS NOT NULL;
			`)
			if err != nil {
				return fmt.Errorf("failed to create alerts table: %w", err)
			}

			// alert_acknowledgments
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS alert_acknowledgments (
					id BIGSERIAL PRIMARY KEY,
					alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
					acknowledged_by TEXT NOT NULL,
					acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					acknowledge_type TEXT NOT NULL CHECK (acknowledge_type IN ('acknowledge', 'dismiss', 'false_positive')),
					message TEXT NOT NULL DEFAULT '',
					false_positive BOOLEAN NOT NULL DEFAULT FALSE
				);

				COMMENT ON TABLE alert_acknowledgments IS
					'User acknowledgments of alerts for learning and audit trail';

				CREATE INDEX IF NOT EXISTS idx_alert_acknowledgments_alert ON alert_acknowledgments(alert_id);
				CREATE INDEX IF NOT EXISTS idx_alert_acknowledgments_user ON alert_acknowledgments(acknowledged_by);
				CREATE INDEX IF NOT EXISTS idx_alert_acknowledgments_false_positive ON alert_acknowledgments(false_positive) WHERE false_positive = TRUE;
			`)
			if err != nil {
				return fmt.Errorf("failed to create alert_acknowledgments table: %w", err)
			}

			// blackouts
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS blackouts (
					id BIGSERIAL PRIMARY KEY,
					connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
					database_name TEXT,
					reason TEXT NOT NULL,
					start_time TIMESTAMPTZ NOT NULL,
					end_time TIMESTAMPTZ NOT NULL,
					created_by TEXT NOT NULL,
					created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					CHECK (end_time > start_time)
				);

				COMMENT ON TABLE blackouts IS
					'Manual blackout periods during which alerts are suppressed';

				CREATE INDEX IF NOT EXISTS idx_blackouts_active ON blackouts(start_time, end_time);
				CREATE INDEX IF NOT EXISTS idx_blackouts_connection ON blackouts(connection_id);
			`)
			if err != nil {
				return fmt.Errorf("failed to create blackouts table: %w", err)
			}

			// blackout_schedules
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS blackout_schedules (
					id BIGSERIAL PRIMARY KEY,
					connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
					database_name TEXT,
					name TEXT NOT NULL,
					cron_expression TEXT NOT NULL,
					duration_minutes INTEGER NOT NULL CHECK (duration_minutes > 0),
					timezone TEXT NOT NULL DEFAULT 'UTC',
					reason TEXT NOT NULL,
					enabled BOOLEAN NOT NULL DEFAULT TRUE,
					created_by TEXT NOT NULL,
					created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
				);

				COMMENT ON TABLE blackout_schedules IS
					'Scheduled recurring blackout periods using cron expressions';

				CREATE INDEX IF NOT EXISTS idx_blackout_schedules_enabled ON blackout_schedules(enabled) WHERE enabled = TRUE;
				CREATE INDEX IF NOT EXISTS idx_blackout_schedules_connection ON blackout_schedules(connection_id);
			`)
			if err != nil {
				return fmt.Errorf("failed to create blackout_schedules table: %w", err)
			}

			// =====================================================================
			// PART 5: Anomaly Detection Tables
			// =====================================================================

			// metric_definitions
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metric_definitions (
					id BIGSERIAL PRIMARY KEY,
					name TEXT NOT NULL UNIQUE,
					category TEXT NOT NULL,
					description TEXT NOT NULL,
					unit TEXT,
					anomaly_enabled BOOLEAN NOT NULL DEFAULT TRUE,
					min_value REAL,
					max_value REAL
				);

				COMMENT ON TABLE metric_definitions IS
					'Definitions of metrics that can be monitored for anomalies';
			`)
			if err != nil {
				return fmt.Errorf("failed to create metric_definitions table: %w", err)
			}

			// metric_baselines
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS metric_baselines (
					id BIGSERIAL PRIMARY KEY,
					connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
					database_name TEXT,
					metric_name TEXT NOT NULL,
					period_type TEXT NOT NULL CHECK (period_type IN ('all', 'hourly', 'daily', 'weekly')),
					day_of_week INTEGER CHECK (day_of_week >= 0 AND day_of_week <= 6),
					hour_of_day INTEGER CHECK (hour_of_day >= 0 AND hour_of_day <= 23),
					mean REAL NOT NULL,
					stddev REAL NOT NULL,
					min REAL NOT NULL,
					max REAL NOT NULL,
					sample_count BIGINT NOT NULL DEFAULT 0,
					last_calculated TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
				);

				COMMENT ON TABLE metric_baselines IS
					'Statistical baselines for metrics used in anomaly detection';

				CREATE INDEX IF NOT EXISTS idx_metric_baselines_connection ON metric_baselines(connection_id);
				CREATE INDEX IF NOT EXISTS idx_metric_baselines_metric ON metric_baselines(metric_name);
				CREATE UNIQUE INDEX IF NOT EXISTS idx_metric_baselines_unique
					ON metric_baselines(
						connection_id,
						COALESCE(database_name, ''),
						metric_name,
						period_type,
						COALESCE(day_of_week, -1),
						COALESCE(hour_of_day, -1)
					);
			`)
			if err != nil {
				return fmt.Errorf("failed to create metric_baselines table: %w", err)
			}

			// correlation_groups
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS correlation_groups (
					id BIGSERIAL PRIMARY KEY,
					connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
					database_name TEXT,
					start_time TIMESTAMPTZ NOT NULL,
					end_time TIMESTAMPTZ,
					anomaly_count INTEGER NOT NULL DEFAULT 1,
					root_cause_guess TEXT
				);

				COMMENT ON TABLE correlation_groups IS
					'Groups of related anomalies detected within a correlation window';

				CREATE INDEX IF NOT EXISTS idx_correlation_groups_connection ON correlation_groups(connection_id);
				CREATE INDEX IF NOT EXISTS idx_correlation_groups_time ON correlation_groups(start_time DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create correlation_groups table: %w", err)
			}

			// anomaly_candidates
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS anomaly_candidates (
					id BIGSERIAL PRIMARY KEY,
					connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
					database_name TEXT,
					metric_name TEXT NOT NULL,
					metric_value REAL NOT NULL,
					z_score REAL NOT NULL,
					detected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					context JSONB NOT NULL DEFAULT '{}',
					tier1_pass BOOLEAN NOT NULL DEFAULT FALSE,
					tier2_score REAL,
					tier2_pass BOOLEAN,
					tier3_result TEXT,
					tier3_pass BOOLEAN,
					tier3_error TEXT,
					final_decision TEXT CHECK (final_decision IN ('alert', 'suppress', 'pending')),
					alert_id BIGINT REFERENCES alerts(id) ON DELETE SET NULL,
					embedding_id BIGINT,
					processed_at TIMESTAMPTZ
				);

				COMMENT ON TABLE anomaly_candidates IS
					'Anomaly candidates being processed through the tiered detection system';

				CREATE INDEX IF NOT EXISTS idx_anomaly_candidates_connection ON anomaly_candidates(connection_id);
				CREATE INDEX IF NOT EXISTS idx_anomaly_candidates_detected ON anomaly_candidates(detected_at DESC);
				CREATE INDEX IF NOT EXISTS idx_anomaly_candidates_pending ON anomaly_candidates(final_decision) WHERE final_decision = 'pending';
			`)
			if err != nil {
				return fmt.Errorf("failed to create anomaly_candidates table: %w", err)
			}

			// =====================================================================
			// PART 6: Notification Tables
			// =====================================================================

			// notification_channels
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS notification_channels (
					id BIGSERIAL PRIMARY KEY,
					owner_username VARCHAR(255),
					owner_token VARCHAR(255),
					enabled BOOLEAN NOT NULL DEFAULT TRUE,
					channel_type TEXT NOT NULL CHECK (channel_type IN ('slack', 'mattermost', 'webhook', 'email')),
					name TEXT NOT NULL,
					description TEXT,
					webhook_url_encrypted TEXT,
					endpoint_url TEXT,
					http_method TEXT DEFAULT 'POST',
					headers_json JSONB DEFAULT '{}',
					auth_type TEXT,
					auth_credentials_encrypted TEXT,
					smtp_host TEXT,
					smtp_port INTEGER DEFAULT 587,
					smtp_username TEXT,
					smtp_password_encrypted TEXT,
					smtp_use_tls BOOLEAN DEFAULT TRUE,
					from_address TEXT,
					from_name TEXT,
					template_alert_fire TEXT,
					template_alert_clear TEXT,
					template_reminder TEXT,
					reminder_enabled BOOLEAN NOT NULL DEFAULT FALSE,
					reminder_interval_hours INTEGER DEFAULT 24,
					created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					CONSTRAINT chk_notification_channel_owner CHECK (
						(owner_username IS NOT NULL AND owner_token IS NULL) OR
						(owner_username IS NULL AND owner_token IS NOT NULL)
					)
				);

				COMMENT ON TABLE notification_channels IS
					'Notification channels for delivering alerts (Slack, Mattermost, webhook, email)';

				CREATE INDEX IF NOT EXISTS idx_notification_channels_channel_type ON notification_channels(channel_type);
				CREATE INDEX IF NOT EXISTS idx_notification_channels_enabled ON notification_channels(enabled) WHERE enabled = TRUE;
				CREATE INDEX IF NOT EXISTS idx_notification_channels_owner_username ON notification_channels(owner_username);
				CREATE INDEX IF NOT EXISTS idx_notification_channels_owner_token ON notification_channels(owner_token);
			`)
			if err != nil {
				return fmt.Errorf("failed to create notification_channels table: %w", err)
			}

			// email_recipients
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS email_recipients (
					id BIGSERIAL PRIMARY KEY,
					channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
					email_address TEXT NOT NULL,
					display_name TEXT,
					enabled BOOLEAN NOT NULL DEFAULT TRUE,
					created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
				);

				COMMENT ON TABLE email_recipients IS
					'Email recipients for email notification channels';

				CREATE INDEX IF NOT EXISTS idx_email_recipients_channel_id ON email_recipients(channel_id);
				CREATE INDEX IF NOT EXISTS idx_email_recipients_channel_enabled ON email_recipients(channel_id, enabled) WHERE enabled = TRUE;
			`)
			if err != nil {
				return fmt.Errorf("failed to create email_recipients table: %w", err)
			}

			// connection_notification_channels
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS connection_notification_channels (
					id BIGSERIAL PRIMARY KEY,
					connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
					channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
					enabled BOOLEAN NOT NULL DEFAULT TRUE,
					reminder_enabled_override BOOLEAN,
					reminder_interval_hours_override INTEGER,
					created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					CONSTRAINT connection_channel_unique UNIQUE (connection_id, channel_id)
				);

				COMMENT ON TABLE connection_notification_channels IS
					'Links connections to notification channels for alert delivery';

				CREATE INDEX IF NOT EXISTS idx_connection_notification_channels_connection_id ON connection_notification_channels(connection_id);
				CREATE INDEX IF NOT EXISTS idx_connection_notification_channels_channel_id ON connection_notification_channels(channel_id);
			`)
			if err != nil {
				return fmt.Errorf("failed to create connection_notification_channels table: %w", err)
			}

			// notification_history
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS notification_history (
					id BIGSERIAL PRIMARY KEY,
					alert_id BIGINT REFERENCES alerts(id) ON DELETE SET NULL,
					channel_id BIGINT REFERENCES notification_channels(id) ON DELETE SET NULL,
					connection_id INTEGER REFERENCES connections(id) ON DELETE SET NULL,
					notification_type TEXT NOT NULL CHECK (notification_type IN ('alert_fire', 'alert_clear', 'reminder')),
					status TEXT NOT NULL CHECK (status IN ('pending', 'sent', 'failed', 'retrying')),
					payload_json JSONB,
					response_code INTEGER,
					response_body TEXT,
					error_message TEXT,
					attempt_count INTEGER NOT NULL DEFAULT 1,
					max_attempts INTEGER NOT NULL DEFAULT 3,
					next_retry_at TIMESTAMPTZ,
					created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					sent_at TIMESTAMPTZ
				);

				COMMENT ON TABLE notification_history IS
					'History of notification delivery attempts and their outcomes';

				CREATE INDEX IF NOT EXISTS idx_notification_history_alert_id ON notification_history(alert_id);
				CREATE INDEX IF NOT EXISTS idx_notification_history_channel_id ON notification_history(channel_id);
				CREATE INDEX IF NOT EXISTS idx_notification_history_status ON notification_history(status);
				CREATE INDEX IF NOT EXISTS idx_notification_history_pending_retry ON notification_history(status, next_retry_at) WHERE status IN ('pending', 'retrying');
				CREATE INDEX IF NOT EXISTS idx_notification_history_created_at ON notification_history(created_at DESC);
			`)
			if err != nil {
				return fmt.Errorf("failed to create notification_history table: %w", err)
			}

			// notification_reminder_state
			_, err = tx.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS notification_reminder_state (
					id BIGSERIAL PRIMARY KEY,
					alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
					channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
					last_reminder_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
					reminder_count INTEGER NOT NULL DEFAULT 0,
					CONSTRAINT alert_channel_reminder_unique UNIQUE (alert_id, channel_id)
				);

				COMMENT ON TABLE notification_reminder_state IS
					'Tracks reminder notification state for active alerts per channel';

				CREATE INDEX IF NOT EXISTS idx_notification_reminder_state_alert_id ON notification_reminder_state(alert_id);
				CREATE INDEX IF NOT EXISTS idx_notification_reminder_state_last_reminder ON notification_reminder_state(last_reminder_at);
			`)
			if err != nil {
				return fmt.Errorf("failed to create notification_reminder_state table: %w", err)
			}

			// =====================================================================
			// PART 7: pgvector Support (Optional)
			// =====================================================================

			// Check if pgvector extension is available
			var vectorAvailable bool
			err = tx.QueryRow(ctx, `
				SELECT EXISTS(SELECT 1 FROM pg_available_extensions WHERE name = 'vector')
			`).Scan(&vectorAvailable)
			if err != nil {
				return fmt.Errorf("failed to check vector extension availability: %w", err)
			}

			if vectorAvailable {
				_, err = tx.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector;`)
				if err != nil {
					logger.Infof("Failed to create vector extension: %v", err)
				} else {
					// anomaly_embeddings table
					_, err = tx.Exec(ctx, `
						CREATE TABLE IF NOT EXISTS anomaly_embeddings (
							id BIGSERIAL PRIMARY KEY,
							candidate_id BIGINT REFERENCES anomaly_candidates(id) ON DELETE CASCADE,
							embedding vector(1536),
							model_name TEXT NOT NULL,
							created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
							UNIQUE(candidate_id)
						);

						COMMENT ON TABLE anomaly_embeddings IS
							'Embeddings for anomaly candidates used in Tier 2 similarity matching';

						CREATE INDEX IF NOT EXISTS idx_anomaly_embeddings_candidate ON anomaly_embeddings(candidate_id);
						CREATE INDEX IF NOT EXISTS idx_anomaly_embeddings_vector ON anomaly_embeddings USING hnsw (embedding vector_cosine_ops);

						ALTER TABLE anomaly_candidates
							ADD CONSTRAINT fk_anomaly_candidates_embedding
							FOREIGN KEY (embedding_id) REFERENCES anomaly_embeddings(id) ON DELETE SET NULL;
					`)
					if err != nil {
						logger.Infof("Failed to create anomaly_embeddings table: %v", err)
					}
				}
			} else {
				logger.Info("pgvector extension not available, skipping anomaly embeddings setup")
			}

			// =====================================================================
			// PART 8: Seed Data - Built-in Alert Rules
			// =====================================================================

			_, err = tx.Exec(ctx, `
				INSERT INTO alert_rules (name, description, category, metric_name, metric_unit, default_operator, default_threshold, default_severity, default_enabled, required_extension, is_built_in)
				VALUES
					-- Connection alerts
					('high_connection_count', 'Active connections exceed threshold', 'connections', 'pg_stat_activity.count', 'connections', '>', 100, 'warning', TRUE, NULL, TRUE),
					('connection_utilization', 'Connection utilization above threshold', 'connections', 'pg_stat_activity.connection_utilization_percent', 'percent', '>', 80, 'warning', TRUE, NULL, TRUE),

					-- Replication alerts
					('replication_lag_bytes', 'Replication lag in bytes exceeds threshold', 'replication', 'pg_stat_replication.lag_bytes', 'bytes', '>', 104857600, 'warning', TRUE, NULL, TRUE),
					('replication_slot_inactive', 'Replication slot is inactive', 'replication', 'pg_replication_slots.inactive', NULL, '==', 1, 'critical', TRUE, NULL, TRUE),

					-- Storage alerts
					('disk_usage_percent', 'Disk usage exceeds threshold', 'storage', 'pg_sys_disk_info.used_percent', 'percent', '>', 80, 'warning', TRUE, 'system_stats', TRUE),
					('disk_usage_critical', 'Disk usage critically high', 'storage', 'pg_sys_disk_info.used_percent', 'percent', '>', 95, 'critical', TRUE, 'system_stats', TRUE),
					('table_bloat_ratio', 'Table bloat ratio exceeds threshold', 'storage', 'pg_stat_all_tables.bloat_ratio', 'percent', '>', 50, 'warning', TRUE, NULL, TRUE),

					-- Performance alerts
					('cpu_usage_high', 'CPU usage exceeds threshold', 'performance', 'pg_sys_cpu_usage_info.processor_time_percent', 'percent', '>', 80, 'warning', TRUE, 'system_stats', TRUE),
					('memory_usage_high', 'Memory usage exceeds threshold', 'performance', 'pg_sys_memory_info.used_percent', 'percent', '>', 85, 'warning', TRUE, 'system_stats', TRUE),
					('load_average_high', 'System load average exceeds threshold', 'performance', 'pg_sys_load_avg_info.load_avg_fifteen_minutes', 'load average', '>', 4, 'warning', TRUE, 'system_stats', TRUE),
					('long_running_queries', 'Queries running longer than threshold', 'performance', 'pg_stat_activity.max_query_duration_seconds', 'seconds', '>', 300, 'warning', TRUE, NULL, TRUE),
					('blocked_queries', 'Blocked queries detected', 'performance', 'pg_stat_activity.blocked_count', 'queries', '>', 0, 'warning', TRUE, NULL, TRUE),

					-- Transaction alerts
					('long_running_transaction', 'Transaction running too long', 'transactions', 'pg_stat_activity.max_xact_duration_seconds', 'seconds', '>', 3600, 'warning', TRUE, NULL, TRUE),
					('idle_in_transaction', 'Connection idle in transaction too long', 'transactions', 'pg_stat_activity.idle_in_transaction_seconds', 'seconds', '>', 300, 'warning', TRUE, NULL, TRUE),
					('transaction_wraparound', 'Transaction ID wraparound approaching', 'transactions', 'pg_class.age_percent', 'percent', '>', 75, 'critical', TRUE, NULL, TRUE),

					-- Lock alerts
					('deadlocks_detected', 'Deadlocks detected', 'locks', 'pg_stat_database.deadlocks_delta', 'deadlocks', '>', 0, 'warning', TRUE, NULL, TRUE),
					('lock_wait_time', 'Lock wait time exceeds threshold', 'locks', 'pg_stat_activity.max_lock_wait_seconds', 'seconds', '>', 30, 'warning', TRUE, NULL, TRUE),

					-- WAL and Checkpoint alerts
					('checkpoint_warning', 'Checkpoints requested too frequently', 'wal', 'pg_stat_checkpointer.checkpoints_req_delta', 'checkpoints', '>', 10, 'warning', TRUE, NULL, TRUE),
					('wal_archive_failed', 'WAL archiving failures detected', 'wal', 'pg_stat_wal.failed_count_delta', 'failures', '>', 0, 'critical', TRUE, NULL, TRUE),

					-- Vacuum alerts
					('autovacuum_not_running', 'Autovacuum has not run recently', 'maintenance', 'pg_stat_all_tables.last_autovacuum_hours', 'hours', '>', 24, 'warning', TRUE, NULL, TRUE),
					('dead_tuple_ratio', 'Dead tuple ratio too high', 'maintenance', 'pg_stat_all_tables.dead_tuple_percent', 'percent', '>', 20, 'warning', TRUE, NULL, TRUE),

					-- Statement alerts
					('slow_query_count', 'High number of slow queries', 'queries', 'pg_stat_statements.slow_query_count', 'queries', '>', 10, 'warning', TRUE, 'pg_stat_statements', TRUE),
					('cache_hit_ratio_low', 'Buffer cache hit ratio below threshold', 'queries', 'pg_stat_database.cache_hit_ratio', 'percent', '<', 95, 'warning', TRUE, NULL, TRUE),

					-- Error alerts
					('temp_files_created', 'Temporary files being created', 'performance', 'pg_stat_database.temp_files_delta', 'files', '>', 100, 'warning', TRUE, NULL, TRUE)
				ON CONFLICT (name) DO NOTHING;
			`)
			if err != nil {
				return fmt.Errorf("failed to insert built-in alert rules: %w", err)
			}

			return nil
		},
	})

	// Migration #2: Add connection_error column to connections table
	sm.migrations = append(sm.migrations, Migration{
		Version:     2,
		Description: "Add connection_error column to connections table",
		Up: func(tx pgx.Tx) error {
			ctx := context.Background()

			_, err := tx.Exec(ctx, `
				ALTER TABLE connections ADD COLUMN IF NOT EXISTS connection_error TEXT;

				COMMENT ON COLUMN connections.connection_error IS
					'Last connection error message, NULL when connection is healthy';
			`)
			if err != nil {
				return fmt.Errorf("failed to add connection_error column: %w", err)
			}

			return nil
		},
	})

	// Migration #3: Add connection alert type
	sm.migrations = append(sm.migrations, Migration{
		Version:     3,
		Description: "Add connection alert type",
		Up: func(tx pgx.Tx) error {
			ctx := context.Background()

			_, err := tx.Exec(ctx, `
				ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_alert_type_check;
				ALTER TABLE alerts ADD CONSTRAINT alerts_alert_type_check CHECK (alert_type IN ('threshold', 'anomaly', 'connection'));
			`)
			if err != nil {
				return fmt.Errorf("failed to add connection alert type: %w", err)
			}

			return nil
		},
	})

	// Migration #4: Add scope columns to blackouts and blackout_schedules
	sm.migrations = append(sm.migrations, Migration{
		Version:     4,
		Description: "Add hierarchical scope to blackouts and blackout_schedules",
		Up: func(tx pgx.Tx) error {
			ctx := context.Background()

			// Add scope columns and foreign keys to blackouts
			_, err := tx.Exec(ctx, `
				ALTER TABLE blackouts
					ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'server',
					ADD COLUMN IF NOT EXISTS group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
					ADD COLUMN IF NOT EXISTS cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE;

				ALTER TABLE blackouts DROP CONSTRAINT IF EXISTS blackouts_scope_check;
				ALTER TABLE blackouts ADD CONSTRAINT blackouts_scope_check
					CHECK (scope IN ('estate', 'group', 'cluster', 'server'));

				ALTER TABLE blackouts DROP CONSTRAINT IF EXISTS blackouts_scope_ids_check;
				ALTER TABLE blackouts ADD CONSTRAINT blackouts_scope_ids_check CHECK (
					(scope = 'estate' AND connection_id IS NULL AND group_id IS NULL AND cluster_id IS NULL)
					OR (scope = 'group' AND group_id IS NOT NULL AND connection_id IS NULL AND cluster_id IS NULL)
					OR (scope = 'cluster' AND cluster_id IS NOT NULL AND connection_id IS NULL AND group_id IS NULL)
					OR (scope = 'server')
				);

				CREATE INDEX IF NOT EXISTS idx_blackouts_scope ON blackouts(scope);
				CREATE INDEX IF NOT EXISTS idx_blackouts_group_id ON blackouts(group_id);
				CREATE INDEX IF NOT EXISTS idx_blackouts_cluster_id ON blackouts(cluster_id);

				COMMENT ON COLUMN blackouts.scope IS
					'Blackout scope level: estate, group, cluster, or server';
				COMMENT ON COLUMN blackouts.group_id IS
					'Cluster group targeted when scope is group';
				COMMENT ON COLUMN blackouts.cluster_id IS
					'Cluster targeted when scope is cluster';
			`)
			if err != nil {
				return fmt.Errorf("failed to add scope columns to blackouts: %w", err)
			}

			// Add scope columns and foreign keys to blackout_schedules
			_, err = tx.Exec(ctx, `
				ALTER TABLE blackout_schedules
					ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'server',
					ADD COLUMN IF NOT EXISTS group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
					ADD COLUMN IF NOT EXISTS cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE;

				ALTER TABLE blackout_schedules DROP CONSTRAINT IF EXISTS blackout_schedules_scope_check;
				ALTER TABLE blackout_schedules ADD CONSTRAINT blackout_schedules_scope_check
					CHECK (scope IN ('estate', 'group', 'cluster', 'server'));

				ALTER TABLE blackout_schedules DROP CONSTRAINT IF EXISTS blackout_schedules_scope_ids_check;
				ALTER TABLE blackout_schedules ADD CONSTRAINT blackout_schedules_scope_ids_check CHECK (
					(scope = 'estate' AND connection_id IS NULL AND group_id IS NULL AND cluster_id IS NULL)
					OR (scope = 'group' AND group_id IS NOT NULL AND connection_id IS NULL AND cluster_id IS NULL)
					OR (scope = 'cluster' AND cluster_id IS NOT NULL AND connection_id IS NULL AND group_id IS NULL)
					OR (scope = 'server')
				);

				CREATE INDEX IF NOT EXISTS idx_blackout_schedules_scope ON blackout_schedules(scope);
				CREATE INDEX IF NOT EXISTS idx_blackout_schedules_group_id ON blackout_schedules(group_id);
				CREATE INDEX IF NOT EXISTS idx_blackout_schedules_cluster_id ON blackout_schedules(cluster_id);

				COMMENT ON COLUMN blackout_schedules.scope IS
					'Blackout scope level: estate, group, cluster, or server';
				COMMENT ON COLUMN blackout_schedules.group_id IS
					'Cluster group targeted when scope is group';
				COMMENT ON COLUMN blackout_schedules.cluster_id IS
					'Cluster targeted when scope is cluster';
			`)
			if err != nil {
				return fmt.Errorf("failed to add scope columns to blackout_schedules: %w", err)
			}

			return nil
		},
	})

	// Migration #5: Remove is_shared from notification_channels
	sm.migrations = append(sm.migrations, Migration{
		Version:     5,
		Description: "Remove is_shared from notification_channels (all channels are shared)",
		Up: func(tx pgx.Tx) error {
			ctx := context.Background()

			_, err := tx.Exec(ctx, `
				ALTER TABLE notification_channels DROP COLUMN IF EXISTS is_shared;
			`)
			if err != nil {
				return fmt.Errorf("failed to drop is_shared from notification_channels: %w", err)
			}

			return nil
		},
	})

	// Migration #6: Add postmaster_start_time to pg_node_role
	sm.migrations = append(sm.migrations, Migration{
		Version:     6,
		Description: "Add postmaster_start_time to pg_node_role for restart detection",
		Up: func(tx pgx.Tx) error {
			ctx := context.Background()

			_, err := tx.Exec(ctx, `
				ALTER TABLE metrics.pg_node_role
					ADD COLUMN IF NOT EXISTS postmaster_start_time TIMESTAMPTZ;

				COMMENT ON COLUMN metrics.pg_node_role.postmaster_start_time IS
					'PostgreSQL postmaster start time for restart detection';
			`)
			return err
		},
	})
}

// Migrate applies all pending migrations
func (sm *SchemaManager) Migrate(conn *pgxpool.Conn) error {
	ctx := context.Background()
	logger.Info("Starting schema migration...")

	// Sort migrations by version
	sort.Slice(sm.migrations, func(i, j int) bool {
		return sm.migrations[i].Version < sm.migrations[j].Version
	})

	// Get current schema version
	currentVersion, err := sm.getCurrentVersion(conn)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	logger.Infof("Current schema version: %d", currentVersion)

	// Apply each pending migration
	appliedCount := 0
	for _, migration := range sm.migrations {
		if migration.Version <= currentVersion {
			continue
		}

		logger.Infof("Applying migration %d: %s", migration.Version, migration.Description)

		// Start a transaction for the migration
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration %d: %w",
				migration.Version, err)
		}

		// Apply the migration
		if err := migration.Up(tx); err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				logger.Errorf("Failed to rollback transaction: %v", rbErr)
			}
			return fmt.Errorf("failed to apply migration %d: %w", migration.Version, err)
		}

		// Record the migration in schema_version
		_, err = tx.Exec(ctx, `
            INSERT INTO schema_version (version, description)
            VALUES ($1, $2)
            ON CONFLICT (version) DO NOTHING
        `, migration.Version, migration.Description)
		if err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				logger.Errorf("Failed to rollback transaction: %v", rbErr)
			}
			return fmt.Errorf("failed to record migration %d: %w", migration.Version, err)
		}

		// Commit the transaction
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit migration %d: %w", migration.Version, err)
		}

		logger.Infof("Successfully applied migration %d", migration.Version)
		appliedCount++
	}

	if appliedCount == 0 {
		logger.Startup("Schema is up to date")
	} else {
		logger.Infof("Applied %d migration(s)", appliedCount)
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

	if !tableNotExist {
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
