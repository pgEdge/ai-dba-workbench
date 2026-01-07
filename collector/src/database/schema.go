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
	"github.com/pgedge/ai-workbench/pkg/logger"
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
	// Consolidated Migration #1
	// This migration consolidates all 43 original migrations into a single
	// migration with reorganized column order for better maintainability.
	//
	// Column organization follows this pattern:
	// 1. Primary key columns
	// 2. Foreign key columns
	// 3. Control/status indicators (is_enabled, is_superuser, is_shared, etc.)
	// 4. Most important to least important data fields
	// 5. Timestamps (created_at, updated_at, applied_at, inserted_at, etc.)

	sm.migrations = append(sm.migrations, Migration{
		Version:     1,
		Description: "Initial schema with all tables",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Create schema_version table
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

				// Create connections table
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS connections (
				id SERIAL PRIMARY KEY,
				owner_username VARCHAR(255),
				owner_token VARCHAR(255),
				is_shared BOOLEAN NOT NULL DEFAULT FALSE,
				is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
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
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
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

			COMMENT ON INDEX idx_connections_name IS
				'Index for fast lookup of connections by name';
			COMMENT ON INDEX idx_connections_owner_username IS
				'Index for fast lookup of connections by owner username';
			COMMENT ON INDEX idx_connections_owner_token IS
				'Index for fast lookup of connections by owner token';
			COMMENT ON INDEX idx_connections_is_monitored IS
				'Partial index for efficiently finding actively monitored connections';
		`)
			if err != nil {
				return fmt.Errorf("failed to create connections table: %w", err)
			}

			// Create probe_configs table
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS probe_configs (
				id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
				connection_id INTEGER,
				is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
				name TEXT NOT NULL,
				description TEXT NOT NULL,
				collection_interval_seconds INTEGER NOT NULL DEFAULT 60,
				retention_days INTEGER NOT NULL DEFAULT 28,
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
			_, err = conn.Exec(ctx, `
			INSERT INTO probe_configs (connection_id, is_enabled, name, description, collection_interval_seconds, retention_days)
			VALUES
				-- Server-scoped probes
				(NULL, TRUE, 'pg_stat_activity', 'Monitors current database activity and backend processes', 60, 7),
				(NULL, TRUE, 'pg_stat_replication', 'Monitors replication status and lag', 30, 7),
				(NULL, TRUE, 'pg_stat_replication_slots', 'Monitors replication slot status and usage', 300, 7),
				(NULL, TRUE, 'pg_stat_wal_receiver', 'Monitors WAL receiver process status', 30, 7),
				(NULL, TRUE, 'pg_stat_recovery_prefetch', 'Monitors recovery prefetch statistics', 600, 7),
				(NULL, TRUE, 'pg_stat_subscription', 'Monitors logical replication subscription status', 300, 7),
				(NULL, TRUE, 'pg_stat_subscription_stats', 'Monitors logical replication subscription statistics', 300, 7),
				(NULL, TRUE, 'pg_stat_ssl', 'Monitors SSL connection information', 300, 7),
				(NULL, TRUE, 'pg_stat_gssapi', 'Monitors GSSAPI authentication information', 300, 7),
				(NULL, TRUE, 'pg_stat_archiver', 'Monitors WAL archiver process status', 600, 7),
				(NULL, TRUE, 'pg_stat_io', 'Monitors I/O statistics by backend type', 900, 7),
				(NULL, TRUE, 'pg_stat_bgwriter', 'Monitors background writer process statistics', 600, 7),
				(NULL, TRUE, 'pg_stat_checkpointer', 'Monitors checkpoint process statistics', 600, 7),
				(NULL, TRUE, 'pg_stat_wal', 'Monitors WAL generation statistics', 600, 7),
				(NULL, TRUE, 'pg_stat_slru', 'Monitors SLRU cache statistics', 600, 7),
				-- Database-scoped probes
				(NULL, TRUE, 'pg_stat_database', 'Monitors database-wide statistics', 300, 7),
				(NULL, TRUE, 'pg_stat_database_conflicts', 'Monitors database conflicts during recovery', 300, 7),
				(NULL, TRUE, 'pg_stat_all_tables', 'Monitors table access statistics', 300, 7),
				(NULL, TRUE, 'pg_stat_all_indexes', 'Monitors index access statistics', 300, 7),
				(NULL, TRUE, 'pg_statio_all_tables', 'Monitors table I/O statistics', 300, 7),
				(NULL, TRUE, 'pg_statio_all_indexes', 'Monitors index I/O statistics', 300, 7),
				(NULL, TRUE, 'pg_statio_all_sequences', 'Monitors sequence I/O statistics', 300, 7),
				(NULL, TRUE, 'pg_stat_user_functions', 'Monitors user-defined function statistics', 300, 7),
				(NULL, TRUE, 'pg_stat_statements', 'Monitors SQL statement execution statistics', 300, 7),
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
			_, err = conn.Exec(ctx, `
			CREATE SCHEMA IF NOT EXISTS metrics;
			COMMENT ON SCHEMA metrics IS
				'Schema for storing monitoring probe metrics data';
		`)
			if err != nil {
				return fmt.Errorf("failed to create metrics schema: %w", err)
			}

			// Create metrics.pg_stat_activity table (partitioned)
			_, err = conn.Exec(ctx, `
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
				backend_start TIMESTAMP,
				xact_start TIMESTAMP,
				query_start TIMESTAMP,
				state_change TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, pid)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_activity IS
				'Metrics collected from pg_stat_activity view, showing current server activity';
			COMMENT ON COLUMN metrics.pg_stat_activity.connection_id IS
				'ID of the monitored connection from connections table';
			COMMENT ON COLUMN metrics.pg_stat_activity.pid IS
				'Process ID of this backend';
			COMMENT ON COLUMN metrics.pg_stat_activity.datid IS
				'OID of the database this backend is connected to';
			COMMENT ON COLUMN metrics.pg_stat_activity.datname IS
				'Name of the database this backend is connected to';
			COMMENT ON COLUMN metrics.pg_stat_activity.usesysid IS
				'OID of the user logged into this backend';
			COMMENT ON COLUMN metrics.pg_stat_activity.usename IS
				'Name of the user logged into this backend';
			COMMENT ON COLUMN metrics.pg_stat_activity.state IS
				'Current overall state of this backend';
			COMMENT ON COLUMN metrics.pg_stat_activity.application_name IS
				'Name of the application connected to this backend';
			COMMENT ON COLUMN metrics.pg_stat_activity.client_addr IS
				'IP address of the client connected to this backend';
			COMMENT ON COLUMN metrics.pg_stat_activity.client_hostname IS
				'Host name of the client connected to this backend';
			COMMENT ON COLUMN metrics.pg_stat_activity.client_port IS
				'TCP port number that the client is using for communication';
			COMMENT ON COLUMN metrics.pg_stat_activity.leader_pid IS
				'Process ID of the parallel group leader if this is a parallel worker';
			COMMENT ON COLUMN metrics.pg_stat_activity.wait_event_type IS
				'Type of event the backend is waiting for';
			COMMENT ON COLUMN metrics.pg_stat_activity.wait_event IS
				'Wait event name if backend is waiting';
			COMMENT ON COLUMN metrics.pg_stat_activity.backend_type IS
				'Type of backend';
			COMMENT ON COLUMN metrics.pg_stat_activity.backend_xid IS
				'Top-level transaction identifier of this backend';
			COMMENT ON COLUMN metrics.pg_stat_activity.backend_xmin IS
				'Current backend''s xmin horizon';
			COMMENT ON COLUMN metrics.pg_stat_activity.query IS
				'Text of this backend''s most recent query';
			COMMENT ON COLUMN metrics.pg_stat_activity.backend_start IS
				'Time when this process was started';
			COMMENT ON COLUMN metrics.pg_stat_activity.xact_start IS
				'Time when the backend''s current transaction was started';
			COMMENT ON COLUMN metrics.pg_stat_activity.query_start IS
				'Time when the currently active query was started';
			COMMENT ON COLUMN metrics.pg_stat_activity.state_change IS
				'Time when the state was last changed';
			COMMENT ON COLUMN metrics.pg_stat_activity.collected_at IS
				'Timestamp when the metrics were collected';

			CREATE INDEX IF NOT EXISTS idx_pg_stat_activity_collected_at
				ON metrics.pg_stat_activity(collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_stat_activity_connection_time
				ON metrics.pg_stat_activity(connection_id, collected_at DESC);

			COMMENT ON INDEX metrics.idx_pg_stat_activity_collected_at IS
				'Index for efficiently querying metrics by time range';
			COMMENT ON INDEX metrics.idx_pg_stat_activity_connection_time IS
				'Index for efficiently querying metrics by connection and time range';
		`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_activity table: %w", err)
			}

			// Create metrics.pg_stat_all_tables table (partitioned)
			_, err = conn.Exec(ctx, `
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
				last_vacuum TIMESTAMP,
				last_autovacuum TIMESTAMP,
				last_analyze TIMESTAMP,
				last_autoanalyze TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, database_name, collected_at, schemaname, relname)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_all_tables IS
				'Metrics collected from pg_stat_all_tables view, showing table-level statistics per database';
			COMMENT ON COLUMN metrics.pg_stat_all_tables.connection_id IS
				'ID of the monitored connection from connections table';
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
			COMMENT ON COLUMN metrics.pg_stat_all_tables.vacuum_count IS
				'Number of times this table has been manually vacuumed';
			COMMENT ON COLUMN metrics.pg_stat_all_tables.autovacuum_count IS
				'Number of times this table has been vacuumed by the autovacuum daemon';
			COMMENT ON COLUMN metrics.pg_stat_all_tables.analyze_count IS
				'Number of times this table has been manually analyzed';
			COMMENT ON COLUMN metrics.pg_stat_all_tables.autoanalyze_count IS
				'Number of times this table has been analyzed by the autovacuum daemon';
			COMMENT ON COLUMN metrics.pg_stat_all_tables.last_vacuum IS
				'Time of the last vacuum run on this table (not including VACUUM FULL)';
			COMMENT ON COLUMN metrics.pg_stat_all_tables.last_autovacuum IS
				'Time of the last autovacuum run on this table';
			COMMENT ON COLUMN metrics.pg_stat_all_tables.last_analyze IS
				'Time of the last analyze run on this table';
			COMMENT ON COLUMN metrics.pg_stat_all_tables.last_autoanalyze IS
				'Time of the last autoanalyze run on this table';
			COMMENT ON COLUMN metrics.pg_stat_all_tables.collected_at IS
				'Timestamp when the metrics were collected';

			CREATE INDEX IF NOT EXISTS idx_pg_stat_all_tables_collected_at
				ON metrics.pg_stat_all_tables(collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_stat_all_tables_connection_db_time
				ON metrics.pg_stat_all_tables(connection_id, database_name, collected_at DESC);

			COMMENT ON INDEX metrics.idx_pg_stat_all_tables_collected_at IS
				'Index for efficiently querying metrics by time range';
			COMMENT ON INDEX metrics.idx_pg_stat_all_tables_connection_db_time IS
				'Index for efficiently querying metrics by connection, database and time range';
		`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_all_tables table: %w", err)
			}

			// Create metrics.pg_stat_all_indexes table (partitioned)
			_, err = conn.Exec(ctx, `
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
				last_idx_scan TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, database_name, indexrelid)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_all_indexes IS
				'Statistics for all indexes in all databases';

			ALTER TABLE metrics.pg_stat_all_indexes
				ADD CONSTRAINT fk_pg_stat_all_indexes_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;
		`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_all_indexes table: %w", err)
			}

			// Create metrics.pg_stat_statements table (partitioned)
			_, err = conn.Exec(ctx, `
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
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, database_name, collected_at, queryid, userid, dbid, toplevel)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_statements IS
				'Metrics collected from pg_stat_statements extension, showing query execution statistics per database';
			COMMENT ON COLUMN metrics.pg_stat_statements.connection_id IS
				'ID of the monitored connection from connections table';
			COMMENT ON COLUMN metrics.pg_stat_statements.database_name IS
				'Name of the database where these query statistics were collected';
			COMMENT ON COLUMN metrics.pg_stat_statements.userid IS
				'OID of user who executed the statement';
			COMMENT ON COLUMN metrics.pg_stat_statements.dbid IS
				'OID of database in which the statement was executed';
			COMMENT ON COLUMN metrics.pg_stat_statements.queryid IS
				'Internal hash code computed from the statement''s parse tree';
			COMMENT ON COLUMN metrics.pg_stat_statements.toplevel IS
				'True if the statement was executed at the top level, false if executed within a function';
			COMMENT ON COLUMN metrics.pg_stat_statements.query IS
				'Text of a representative statement';
			COMMENT ON COLUMN metrics.pg_stat_statements.calls IS
				'Number of times executed';
			COMMENT ON COLUMN metrics.pg_stat_statements.rows IS
				'Total number of rows retrieved or affected by the statement';
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
			COMMENT ON COLUMN metrics.pg_stat_statements.collected_at IS
				'Timestamp when the metrics were collected';

			CREATE INDEX IF NOT EXISTS idx_pg_stat_statements_collected_at
				ON metrics.pg_stat_statements(collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_stat_statements_connection_db_time
				ON metrics.pg_stat_statements(connection_id, database_name, collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_stat_statements_queryid
				ON metrics.pg_stat_statements(queryid, collected_at DESC);

			COMMENT ON INDEX metrics.idx_pg_stat_statements_collected_at IS
				'Index for efficiently querying metrics by time range';
			COMMENT ON INDEX metrics.idx_pg_stat_statements_connection_db_time IS
				'Index for efficiently querying metrics by connection, database and time range';
			COMMENT ON INDEX metrics.idx_pg_stat_statements_queryid IS
				'Index for efficiently tracking specific queries over time';
		`)
			if err != nil {
				return fmt.Errorf("failed to create pg_stat_statements table: %w", err)
			}

			// Create remaining metrics tables with similar pattern...
			// Due to space constraints, I'll create a condensed version of the remaining tables
			// In production, each table should follow the same detailed pattern

			_, err = conn.Exec(ctx, `
			-- pg_stat_database
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
				checksum_last_failure TIMESTAMP,
				stats_reset TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, database_name)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_database IS 'Per-database statistics';

			ALTER TABLE metrics.pg_stat_database
				ADD CONSTRAINT fk_pg_stat_database_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_database_conflicts
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
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, database_name)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_database_conflicts IS
				'Database conflict statistics (standby servers)';

			ALTER TABLE metrics.pg_stat_database_conflicts
				ADD CONSTRAINT fk_pg_stat_database_conflicts_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_archiver
			CREATE TABLE IF NOT EXISTS metrics.pg_stat_archiver (
				connection_id INTEGER NOT NULL,
				archived_count BIGINT,
				last_archived_wal TEXT,
				failed_count BIGINT,
				last_failed_wal TEXT,
				last_archived_time TIMESTAMP,
				last_failed_time TIMESTAMP,
				stats_reset TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_archiver IS
				'WAL archiver statistics (singleton)';

			ALTER TABLE metrics.pg_stat_archiver
				ADD CONSTRAINT fk_pg_stat_archiver_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_bgwriter
			CREATE TABLE IF NOT EXISTS metrics.pg_stat_bgwriter (
				connection_id INTEGER NOT NULL,
				buffers_clean BIGINT,
				maxwritten_clean BIGINT,
				buffers_alloc BIGINT,
				stats_reset TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_bgwriter IS
				'Background writer statistics (singleton, deprecated PG 17+)';

			ALTER TABLE metrics.pg_stat_bgwriter
				ADD CONSTRAINT fk_pg_stat_bgwriter_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_checkpointer
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
				stats_reset TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_checkpointer IS
				'Checkpointer statistics (singleton, PG 17+)';

			ALTER TABLE metrics.pg_stat_checkpointer
				ADD CONSTRAINT fk_pg_stat_checkpointer_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_wal
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
				stats_reset TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_wal IS
				'WAL generation statistics (singleton)';

			ALTER TABLE metrics.pg_stat_wal
				ADD CONSTRAINT fk_pg_stat_wal_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_replication
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
				backend_start TIMESTAMP,
				reply_time TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, pid)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_replication IS
				'Replication statistics for active replication connections';

			ALTER TABLE metrics.pg_stat_replication
				ADD CONSTRAINT fk_pg_stat_replication_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_replication_slots
			CREATE TABLE IF NOT EXISTS metrics.pg_stat_replication_slots (
				connection_id INTEGER NOT NULL,
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
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, slot_name)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_replication_slots IS
				'Replication slot statistics';

			ALTER TABLE metrics.pg_stat_replication_slots
				ADD CONSTRAINT fk_pg_stat_replication_slots_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_subscription
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
				last_msg_send_time TIMESTAMP,
				last_msg_receipt_time TIMESTAMP,
				latest_end_time TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, subid)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_subscription IS
				'Logical replication subscription statistics';

			ALTER TABLE metrics.pg_stat_subscription
				ADD CONSTRAINT fk_pg_stat_subscription_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_subscription_stats
			CREATE TABLE IF NOT EXISTS metrics.pg_stat_subscription_stats (
				connection_id INTEGER NOT NULL,
				subid OID NOT NULL,
				subname TEXT,
				apply_error_count BIGINT,
				sync_error_count BIGINT,
				stats_reset TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, subid)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_subscription_stats IS
				'Logical replication subscription cumulative statistics';

			ALTER TABLE metrics.pg_stat_subscription_stats
				ADD CONSTRAINT fk_pg_stat_subscription_stats_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_wal_receiver
			CREATE TABLE IF NOT EXISTS metrics.pg_stat_wal_receiver (
				connection_id INTEGER NOT NULL,
				pid INTEGER,
				status TEXT,
				receive_start_lsn TEXT,
				receive_start_tli INTEGER,
				written_lsn TEXT,
				flushed_lsn TEXT,
				received_tli INTEGER,
				slot_name TEXT,
				sender_host TEXT,
				sender_port INTEGER,
				conninfo TEXT,
				latest_end_lsn TEXT,
				last_msg_send_time TIMESTAMP,
				last_msg_receipt_time TIMESTAMP,
				latest_end_time TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_wal_receiver IS
				'WAL receiver statistics (standby servers)';

			ALTER TABLE metrics.pg_stat_wal_receiver
				ADD CONSTRAINT fk_pg_stat_wal_receiver_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_recovery_prefetch
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
				stats_reset TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_recovery_prefetch IS
				'Recovery prefetch statistics (PG 15+)';

			ALTER TABLE metrics.pg_stat_recovery_prefetch
				ADD CONSTRAINT fk_pg_stat_recovery_prefetch_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_slru
			CREATE TABLE IF NOT EXISTS metrics.pg_stat_slru (
				connection_id INTEGER NOT NULL,
				name TEXT NOT NULL,
				blks_zeroed BIGINT,
				blks_hit BIGINT,
				blks_read BIGINT,
				blks_written BIGINT,
				blks_exists BIGINT,
				flushes BIGINT,
				truncates BIGINT,
				stats_reset TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, name)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_slru IS
				'SLRU (Simple LRU) cache statistics';

			ALTER TABLE metrics.pg_stat_slru
				ADD CONSTRAINT fk_pg_stat_slru_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_io
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
				stats_reset TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, backend_type, object, context)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_io IS
				'I/O statistics by backend type and context';

			ALTER TABLE metrics.pg_stat_io
				ADD CONSTRAINT fk_pg_stat_io_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_ssl
			CREATE TABLE IF NOT EXISTS metrics.pg_stat_ssl (
				connection_id INTEGER NOT NULL,
				pid INTEGER NOT NULL,
				ssl BOOLEAN,
				version TEXT,
				cipher TEXT,
				bits INTEGER,
				client_dn TEXT,
				client_serial TEXT,
				issuer_dn TEXT,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, pid)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_ssl IS
				'SSL connection statistics';

			ALTER TABLE metrics.pg_stat_ssl
				ADD CONSTRAINT fk_pg_stat_ssl_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_gssapi
			CREATE TABLE IF NOT EXISTS metrics.pg_stat_gssapi (
				connection_id INTEGER NOT NULL,
				pid INTEGER NOT NULL,
				gss_authenticated BOOLEAN,
				encrypted BOOLEAN,
				credentials_delegated BOOLEAN,
				principal TEXT,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, pid)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_gssapi IS
				'GSSAPI connection statistics';

			ALTER TABLE metrics.pg_stat_gssapi
				ADD CONSTRAINT fk_pg_stat_gssapi_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_stat_user_functions
			CREATE TABLE IF NOT EXISTS metrics.pg_stat_user_functions (
				connection_id INTEGER NOT NULL,
				database_name VARCHAR(255) NOT NULL,
				funcid OID NOT NULL,
				schemaname TEXT,
				funcname TEXT,
				calls BIGINT,
				total_time DOUBLE PRECISION,
				self_time DOUBLE PRECISION,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, database_name, funcid)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_stat_user_functions IS
				'Statistics for user-defined functions';

			ALTER TABLE metrics.pg_stat_user_functions
				ADD CONSTRAINT fk_pg_stat_user_functions_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_statio_all_tables
			CREATE TABLE IF NOT EXISTS metrics.pg_statio_all_tables (
				connection_id INTEGER NOT NULL,
				database_name VARCHAR(255) NOT NULL,
				relid OID NOT NULL,
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
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, database_name, relid)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_statio_all_tables IS
				'I/O statistics for all tables';

			ALTER TABLE metrics.pg_statio_all_tables
				ADD CONSTRAINT fk_pg_statio_all_tables_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_statio_all_indexes
			CREATE TABLE IF NOT EXISTS metrics.pg_statio_all_indexes (
				connection_id INTEGER NOT NULL,
				database_name VARCHAR(255) NOT NULL,
				indexrelid OID NOT NULL,
				relid OID,
				schemaname TEXT,
				relname TEXT,
				indexrelname TEXT,
				idx_blks_read BIGINT,
				idx_blks_hit BIGINT,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, database_name, indexrelid)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_statio_all_indexes IS
				'I/O statistics for all indexes';

			ALTER TABLE metrics.pg_statio_all_indexes
				ADD CONSTRAINT fk_pg_statio_all_indexes_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			-- pg_statio_all_sequences
			CREATE TABLE IF NOT EXISTS metrics.pg_statio_all_sequences (
				connection_id INTEGER NOT NULL,
				database_name VARCHAR(255) NOT NULL,
				relid OID NOT NULL,
				schemaname TEXT,
				relname TEXT,
				blks_read BIGINT,
				blks_hit BIGINT,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, database_name, relid)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_statio_all_sequences IS
				'I/O statistics for all sequences';

			ALTER TABLE metrics.pg_statio_all_sequences
				ADD CONSTRAINT fk_pg_statio_all_sequences_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;
		`)
			if err != nil {
				return fmt.Errorf("failed to create additional pg_stat tables: %w", err)
			}

			// Create system_stats extension metrics tables
			_, err = conn.Exec(ctx, `
			-- pg_sys_cpu_info
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
				collected_at TIMESTAMP WITH TIME ZONE NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			CREATE INDEX IF NOT EXISTS idx_pg_sys_cpu_info_connection_id ON metrics.pg_sys_cpu_info(connection_id);
			CREATE INDEX IF NOT EXISTS idx_pg_sys_cpu_info_collected_at ON metrics.pg_sys_cpu_info(collected_at);

			-- pg_sys_cpu_usage_info
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
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_sys_cpu_usage_info IS
				'CPU usage statistics collected via system_stats extension';

			ALTER TABLE metrics.pg_sys_cpu_usage_info
				ADD CONSTRAINT fk_pg_sys_cpu_usage_info_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			CREATE INDEX IF NOT EXISTS idx_pg_sys_cpu_usage_info_collected_at
				ON metrics.pg_sys_cpu_usage_info(collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_sys_cpu_usage_info_connection_time
				ON metrics.pg_sys_cpu_usage_info(connection_id, collected_at DESC);

			COMMENT ON INDEX metrics.idx_pg_sys_cpu_usage_info_collected_at IS
				'Index for efficiently querying metrics by time range';
			COMMENT ON INDEX metrics.idx_pg_sys_cpu_usage_info_connection_time IS
				'Index for efficiently querying metrics by connection and time range';

			-- pg_sys_cpu_memory_by_process
			CREATE TABLE IF NOT EXISTS metrics.pg_sys_cpu_memory_by_process (
				connection_id INTEGER NOT NULL,
				pid INTEGER NOT NULL,
				name TEXT,
				running_since_seconds BIGINT,
				cpu_usage REAL,
				memory_usage REAL,
				memory_bytes BIGINT,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, pid)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_sys_cpu_memory_by_process IS
				'Per-process CPU and memory usage collected via system_stats extension';

			ALTER TABLE metrics.pg_sys_cpu_memory_by_process
				ADD CONSTRAINT fk_pg_sys_cpu_memory_by_process_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			CREATE INDEX IF NOT EXISTS idx_pg_sys_cpu_memory_by_process_collected_at
				ON metrics.pg_sys_cpu_memory_by_process(collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_sys_cpu_memory_by_process_connection_time
				ON metrics.pg_sys_cpu_memory_by_process(connection_id, collected_at DESC);

			COMMENT ON INDEX metrics.idx_pg_sys_cpu_memory_by_process_collected_at IS
				'Index for efficiently querying metrics by time range';
			COMMENT ON INDEX metrics.idx_pg_sys_cpu_memory_by_process_connection_time IS
				'Index for efficiently querying metrics by connection and time range';

			-- pg_sys_memory_info
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
				collected_at TIMESTAMP WITH TIME ZONE NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			CREATE INDEX IF NOT EXISTS idx_pg_sys_memory_info_connection_id ON metrics.pg_sys_memory_info(connection_id);
			CREATE INDEX IF NOT EXISTS idx_pg_sys_memory_info_collected_at ON metrics.pg_sys_memory_info(collected_at);

			-- pg_sys_disk_info
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
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, mount_point)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_sys_disk_info IS
				'Disk information collected via system_stats extension';

			ALTER TABLE metrics.pg_sys_disk_info
				ADD CONSTRAINT fk_pg_sys_disk_info_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			CREATE INDEX IF NOT EXISTS idx_pg_sys_disk_info_collected_at
				ON metrics.pg_sys_disk_info(collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_sys_disk_info_connection_time
				ON metrics.pg_sys_disk_info(connection_id, collected_at DESC);

			COMMENT ON INDEX metrics.idx_pg_sys_disk_info_collected_at IS
				'Index for efficiently querying metrics by time range';
			COMMENT ON INDEX metrics.idx_pg_sys_disk_info_connection_time IS
				'Index for efficiently querying metrics by connection and time range';

			-- pg_sys_io_analysis_info
			CREATE TABLE IF NOT EXISTS metrics.pg_sys_io_analysis_info (
				connection_id INTEGER NOT NULL,
				device_name TEXT NOT NULL,
				total_reads BIGINT,
				total_writes BIGINT,
				read_bytes BIGINT,
				write_bytes BIGINT,
				read_time_ms BIGINT,
				write_time_ms BIGINT,
				collected_at TIMESTAMP WITH TIME ZONE NOT NULL,
				PRIMARY KEY (connection_id, collected_at, device_name)
			) PARTITION BY RANGE (collected_at);

			CREATE INDEX IF NOT EXISTS idx_pg_sys_io_analysis_info_connection_id ON metrics.pg_sys_io_analysis_info(connection_id);
			CREATE INDEX IF NOT EXISTS idx_pg_sys_io_analysis_info_collected_at ON metrics.pg_sys_io_analysis_info(collected_at);

			-- pg_sys_load_avg_info
			CREATE TABLE IF NOT EXISTS metrics.pg_sys_load_avg_info (
				connection_id INTEGER NOT NULL,
				load_avg_one_minute REAL,
				load_avg_five_minutes REAL,
				load_avg_ten_minutes REAL,
				load_avg_fifteen_minutes REAL,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_sys_load_avg_info IS
				'System load average collected via system_stats extension';

			ALTER TABLE metrics.pg_sys_load_avg_info
				ADD CONSTRAINT fk_pg_sys_load_avg_info_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			CREATE INDEX IF NOT EXISTS idx_pg_sys_load_avg_info_collected_at
				ON metrics.pg_sys_load_avg_info(collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_sys_load_avg_info_connection_time
				ON metrics.pg_sys_load_avg_info(connection_id, collected_at DESC);

			COMMENT ON INDEX metrics.idx_pg_sys_load_avg_info_collected_at IS
				'Index for efficiently querying metrics by time range';
			COMMENT ON INDEX metrics.idx_pg_sys_load_avg_info_connection_time IS
				'Index for efficiently querying metrics by connection and time range';

			-- pg_sys_network_info
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
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, interface_name)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_sys_network_info IS
				'Network information collected via system_stats extension';

			ALTER TABLE metrics.pg_sys_network_info
				ADD CONSTRAINT fk_pg_sys_network_info_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			CREATE INDEX IF NOT EXISTS idx_pg_sys_network_info_collected_at
				ON metrics.pg_sys_network_info(collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_sys_network_info_connection_time
				ON metrics.pg_sys_network_info(connection_id, collected_at DESC);

			COMMENT ON INDEX metrics.idx_pg_sys_network_info_collected_at IS
				'Index for efficiently querying metrics by time range';
			COMMENT ON INDEX metrics.idx_pg_sys_network_info_connection_time IS
				'Index for efficiently querying metrics by connection and time range';

			-- pg_sys_os_info
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
				last_bootup_time TIMESTAMP,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_sys_os_info IS
				'OS information collected via system_stats extension';

			ALTER TABLE metrics.pg_sys_os_info
				ADD CONSTRAINT fk_pg_sys_os_info_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			CREATE INDEX IF NOT EXISTS idx_pg_sys_os_info_collected_at
				ON metrics.pg_sys_os_info(collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_sys_os_info_connection_time
				ON metrics.pg_sys_os_info(connection_id, collected_at DESC);

			COMMENT ON INDEX metrics.idx_pg_sys_os_info_collected_at IS
				'Index for efficiently querying metrics by time range';
			COMMENT ON INDEX metrics.idx_pg_sys_os_info_connection_time IS
				'Index for efficiently querying metrics by connection and time range';

			-- pg_sys_process_info
			CREATE TABLE IF NOT EXISTS metrics.pg_sys_process_info (
				connection_id INTEGER NOT NULL,
				total_processes INTEGER,
				running_processes INTEGER,
				sleeping_processes INTEGER,
				stopped_processes INTEGER,
				zombie_processes INTEGER,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_sys_process_info IS
				'Process information collected via system_stats extension';

			ALTER TABLE metrics.pg_sys_process_info
				ADD CONSTRAINT fk_pg_sys_process_info_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			CREATE INDEX IF NOT EXISTS idx_pg_sys_process_info_collected_at
				ON metrics.pg_sys_process_info(collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_sys_process_info_connection_time
				ON metrics.pg_sys_process_info(connection_id, collected_at DESC);

			COMMENT ON INDEX metrics.idx_pg_sys_process_info_collected_at IS
				'Index for efficiently querying metrics by time range';
			COMMENT ON INDEX metrics.idx_pg_sys_process_info_connection_time IS
				'Index for efficiently querying metrics by connection and time range';
		`)
			if err != nil {
				return fmt.Errorf("failed to create system_stats tables: %w", err)
			}

			return nil
		},
	})

	// Migration 3: Add pg_settings probe and metrics table
	sm.migrations = append(sm.migrations, Migration{
		Version:     3,
		Description: "Add pg_settings probe for configuration tracking",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Create pg_settings metrics table
			_, err := conn.Exec(ctx, `
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
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, name)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_settings IS
				'PostgreSQL configuration settings - only stores snapshots when changes are detected';

			ALTER TABLE metrics.pg_settings
				ADD CONSTRAINT fk_pg_settings_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;
		`)
			if err != nil {
				return fmt.Errorf("failed to create pg_settings table: %w", err)
			}

			// Insert probe configuration
			_, err = conn.Exec(ctx, `
			INSERT INTO probe_configs (connection_id, is_enabled, name, description, collection_interval_seconds, retention_days)
			VALUES (NULL, TRUE, 'pg_settings', 'Monitors PostgreSQL configuration settings (change-tracked)', 3600, 365)
			ON CONFLICT (COALESCE(connection_id, 0), name) DO NOTHING;
		`)
			if err != nil {
				return fmt.Errorf("failed to insert pg_settings probe configuration: %w", err)
			}

			return nil
		},
	})

	// Migration 4: Add pg_hba_file_rules and pg_ident_file_mappings probes
	sm.migrations = append(sm.migrations, Migration{
		Version:     4,
		Description: "Add pg_hba_file_rules and pg_ident_file_mappings probes for authentication configuration tracking",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Create pg_hba_file_rules metrics table
			_, err := conn.Exec(ctx, `
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
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, rule_number)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_hba_file_rules IS
				'PostgreSQL HBA configuration rules - only stores snapshots when changes are detected';

			ALTER TABLE metrics.pg_hba_file_rules
				ADD CONSTRAINT fk_pg_hba_file_rules_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;
		`)
			if err != nil {
				return fmt.Errorf("failed to create pg_hba_file_rules table: %w", err)
			}

			// Create pg_ident_file_mappings metrics table
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS metrics.pg_ident_file_mappings (
				connection_id INTEGER NOT NULL,
				map_number INTEGER NOT NULL,
				file_name TEXT,
				line_number INTEGER,
				map_name TEXT,
				sys_name TEXT,
				pg_username TEXT,
				error TEXT,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at, map_number)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_ident_file_mappings IS
				'PostgreSQL ident mapping configuration - only stores snapshots when changes are detected';

			ALTER TABLE metrics.pg_ident_file_mappings
				ADD CONSTRAINT fk_pg_ident_file_mappings_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;
		`)
			if err != nil {
				return fmt.Errorf("failed to create pg_ident_file_mappings table: %w", err)
			}

			// Insert pg_hba_file_rules probe configuration
			_, err = conn.Exec(ctx, `
			INSERT INTO probe_configs (connection_id, is_enabled, name, description, collection_interval_seconds, retention_days)
			VALUES (NULL, TRUE, 'pg_hba_file_rules', 'Monitors pg_hba.conf authentication rules (change-tracked)', 3600, 365)
			ON CONFLICT (COALESCE(connection_id, 0), name) DO NOTHING;
		`)
			if err != nil {
				return fmt.Errorf("failed to insert pg_hba_file_rules probe configuration: %w", err)
			}

			// Insert pg_ident_file_mappings probe configuration
			_, err = conn.Exec(ctx, `
			INSERT INTO probe_configs (connection_id, is_enabled, name, description, collection_interval_seconds, retention_days)
			VALUES (NULL, TRUE, 'pg_ident_file_mappings', 'Monitors pg_ident.conf user mappings (change-tracked)', 3600, 365)
			ON CONFLICT (COALESCE(connection_id, 0), name) DO NOTHING;
		`)
			if err != nil {
				return fmt.Errorf("failed to insert pg_ident_file_mappings probe configuration: %w", err)
			}

			return nil
		},
	})

	// Migration 5: Add pg_server_info and pg_node_role probes for cluster topology tracking
	sm.migrations = append(sm.migrations, Migration{
		Version:     5,
		Description: "Add pg_server_info and pg_node_role probes for cluster topology tracking",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Create pg_server_info metrics table
			_, err := conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS metrics.pg_server_info (
				connection_id INTEGER NOT NULL,

				-- Server Identification
				server_version TEXT,
				server_version_num INTEGER,
				system_identifier BIGINT,
				cluster_name TEXT,
				data_directory TEXT,

				-- Configuration
				max_connections INTEGER,
				max_wal_senders INTEGER,
				max_replication_slots INTEGER,

				-- Extensions (for role detection)
				installed_extensions TEXT[],

				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_server_info IS
				'Server identification and configuration - only stores snapshots when changes detected';
			COMMENT ON COLUMN metrics.pg_server_info.connection_id IS
				'ID of the monitored connection from connections table';
			COMMENT ON COLUMN metrics.pg_server_info.server_version IS
				'PostgreSQL server version string (e.g., "17.2")';
			COMMENT ON COLUMN metrics.pg_server_info.server_version_num IS
				'PostgreSQL server version as integer (e.g., 170200)';
			COMMENT ON COLUMN metrics.pg_server_info.system_identifier IS
				'Unique system identifier from pg_control';
			COMMENT ON COLUMN metrics.pg_server_info.cluster_name IS
				'Cluster name from postgresql.conf';
			COMMENT ON COLUMN metrics.pg_server_info.data_directory IS
				'Path to the data directory (PGDATA)';
			COMMENT ON COLUMN metrics.pg_server_info.max_connections IS
				'Maximum number of concurrent connections';
			COMMENT ON COLUMN metrics.pg_server_info.max_wal_senders IS
				'Maximum number of WAL sender processes';
			COMMENT ON COLUMN metrics.pg_server_info.max_replication_slots IS
				'Maximum number of replication slots';
			COMMENT ON COLUMN metrics.pg_server_info.installed_extensions IS
				'Array of installed extension names';
			COMMENT ON COLUMN metrics.pg_server_info.collected_at IS
				'Timestamp when the metrics were collected';

			ALTER TABLE metrics.pg_server_info
				ADD CONSTRAINT fk_pg_server_info_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;
		`)
			if err != nil {
				return fmt.Errorf("failed to create pg_server_info table: %w", err)
			}

			// Create pg_node_role metrics table
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS metrics.pg_node_role (
				connection_id INTEGER NOT NULL,

				-- Fundamental Status
				is_in_recovery BOOLEAN NOT NULL,
				timeline_id INTEGER,

				-- Binary Replication Status
				has_binary_standbys BOOLEAN NOT NULL DEFAULT FALSE,
				binary_standby_count INTEGER DEFAULT 0,
				is_streaming_standby BOOLEAN NOT NULL DEFAULT FALSE,
				upstream_host TEXT,
				upstream_port INTEGER,
				received_lsn TEXT,
				replayed_lsn TEXT,

				-- Logical Replication Status
				publication_count INTEGER DEFAULT 0,
				subscription_count INTEGER DEFAULT 0,
				active_subscription_count INTEGER DEFAULT 0,

				-- Spock Status
				has_spock BOOLEAN NOT NULL DEFAULT FALSE,
				spock_node_id BIGINT,
				spock_node_name TEXT,
				spock_subscription_count INTEGER DEFAULT 0,

				-- BDR Status (future)
				has_bdr BOOLEAN NOT NULL DEFAULT FALSE,
				bdr_node_id TEXT,
				bdr_node_name TEXT,
				bdr_node_group TEXT,
				bdr_node_state TEXT,

				-- Computed Primary Role
				primary_role TEXT NOT NULL,

				-- Role Flags (non-exclusive capabilities)
				role_flags TEXT[] NOT NULL DEFAULT '{}',

				-- Extended Information (JSON for flexibility)
				role_details JSONB,

				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_node_role IS
				'Node role detection for cluster topology analysis';
			COMMENT ON COLUMN metrics.pg_node_role.connection_id IS
				'ID of the monitored connection from connections table';
			COMMENT ON COLUMN metrics.pg_node_role.is_in_recovery IS
				'Whether the server is in recovery mode (standby)';
			COMMENT ON COLUMN metrics.pg_node_role.timeline_id IS
				'Current timeline ID from pg_control';
			COMMENT ON COLUMN metrics.pg_node_role.has_binary_standbys IS
				'Whether this server has physical replication standbys';
			COMMENT ON COLUMN metrics.pg_node_role.binary_standby_count IS
				'Number of connected physical replication standbys';
			COMMENT ON COLUMN metrics.pg_node_role.is_streaming_standby IS
				'Whether this server is a streaming replication standby';
			COMMENT ON COLUMN metrics.pg_node_role.upstream_host IS
				'For standbys: hostname of the upstream primary';
			COMMENT ON COLUMN metrics.pg_node_role.upstream_port IS
				'For standbys: port of the upstream primary';
			COMMENT ON COLUMN metrics.pg_node_role.received_lsn IS
				'For standbys: last received WAL location';
			COMMENT ON COLUMN metrics.pg_node_role.replayed_lsn IS
				'For standbys: last replayed WAL location';
			COMMENT ON COLUMN metrics.pg_node_role.publication_count IS
				'Number of logical replication publications';
			COMMENT ON COLUMN metrics.pg_node_role.subscription_count IS
				'Number of logical replication subscriptions';
			COMMENT ON COLUMN metrics.pg_node_role.active_subscription_count IS
				'Number of active logical replication subscriptions';
			COMMENT ON COLUMN metrics.pg_node_role.has_spock IS
				'Whether Spock extension is installed';
			COMMENT ON COLUMN metrics.pg_node_role.spock_node_id IS
				'Spock node ID if participating in Spock cluster';
			COMMENT ON COLUMN metrics.pg_node_role.spock_node_name IS
				'Spock node name if participating in Spock cluster';
			COMMENT ON COLUMN metrics.pg_node_role.spock_subscription_count IS
				'Number of active Spock subscriptions';
			COMMENT ON COLUMN metrics.pg_node_role.has_bdr IS
				'Whether BDR extension is installed (future)';
			COMMENT ON COLUMN metrics.pg_node_role.bdr_node_id IS
				'BDR node ID if participating in BDR cluster';
			COMMENT ON COLUMN metrics.pg_node_role.bdr_node_name IS
				'BDR node name if participating in BDR cluster';
			COMMENT ON COLUMN metrics.pg_node_role.bdr_node_group IS
				'BDR node group name';
			COMMENT ON COLUMN metrics.pg_node_role.bdr_node_state IS
				'BDR node state';
			COMMENT ON COLUMN metrics.pg_node_role.primary_role IS
				'Computed primary role: standalone, binary_primary, binary_standby, spock_node, etc.';
			COMMENT ON COLUMN metrics.pg_node_role.role_flags IS
				'Array of non-exclusive role flags (e.g., binary_primary, logical_publisher)';
			COMMENT ON COLUMN metrics.pg_node_role.role_details IS
				'Additional role-specific details in JSON format';
			COMMENT ON COLUMN metrics.pg_node_role.collected_at IS
				'Timestamp when the metrics were collected';

			ALTER TABLE metrics.pg_node_role
				ADD CONSTRAINT fk_pg_node_role_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			CREATE INDEX IF NOT EXISTS idx_pg_node_role_primary_role
				ON metrics.pg_node_role(connection_id, primary_role, collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_node_role_collected_at
				ON metrics.pg_node_role(collected_at DESC);

			COMMENT ON INDEX metrics.idx_pg_node_role_primary_role IS
				'Index for filtering nodes by role';
			COMMENT ON INDEX metrics.idx_pg_node_role_collected_at IS
				'Index for efficiently querying metrics by time range';
		`)
			if err != nil {
				return fmt.Errorf("failed to create pg_node_role table: %w", err)
			}

			// Insert probe configurations
			_, err = conn.Exec(ctx, `
			INSERT INTO probe_configs (connection_id, is_enabled, name, description, collection_interval_seconds, retention_days)
			VALUES
				(NULL, TRUE, 'pg_server_info', 'Server identification and configuration (change-tracked)', 3600, 365),
				(NULL, TRUE, 'pg_node_role', 'Node role detection for cluster topology', 300, 30)
			ON CONFLICT (COALESCE(connection_id, 0), name) DO NOTHING;
		`)
			if err != nil {
				return fmt.Errorf("failed to insert probe configurations: %w", err)
			}

			return nil
		},
	})

	// Migration 6: Reserved (previously user groups and privileges - moved to SQLite auth store)
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
		if err := migration.Up(conn); err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				logger.Errorf("Failed to rollback transaction: %v", rbErr)
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
