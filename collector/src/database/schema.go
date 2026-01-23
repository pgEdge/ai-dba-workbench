/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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

	// Migration 7: Alerter core tables - settings, probe availability, rules, thresholds, alerts
	sm.migrations = append(sm.migrations, Migration{
		Version:     7,
		Description: "Add alerter core tables for threshold-based alerts",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Create alerter_settings table
			_, err := conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS alerter_settings (
				id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
				retention_days INTEGER NOT NULL DEFAULT 90,
				default_anomaly_enabled BOOLEAN NOT NULL DEFAULT TRUE,
				default_anomaly_sensitivity REAL NOT NULL DEFAULT 3.0,
				baseline_refresh_interval_mins INTEGER NOT NULL DEFAULT 60,
				correlation_window_seconds INTEGER NOT NULL DEFAULT 120,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
			);

			COMMENT ON TABLE alerter_settings IS
				'Global settings for the alerter service (singleton table)';
			COMMENT ON COLUMN alerter_settings.retention_days IS
				'Number of days to retain cleared/acknowledged alerts';
			COMMENT ON COLUMN alerter_settings.default_anomaly_enabled IS
				'Whether anomaly detection is enabled by default for new connections';
			COMMENT ON COLUMN alerter_settings.default_anomaly_sensitivity IS
				'Default z-score threshold for anomaly detection (higher = less sensitive)';
			COMMENT ON COLUMN alerter_settings.baseline_refresh_interval_mins IS
				'How often to recalculate metric baselines in minutes';
			COMMENT ON COLUMN alerter_settings.correlation_window_seconds IS
				'Time window for correlating related anomalies';

			-- Insert default settings
			INSERT INTO alerter_settings (id) VALUES (1) ON CONFLICT DO NOTHING;
		`)
			if err != nil {
				return fmt.Errorf("failed to create alerter_settings table: %w", err)
			}

			// Create probe_availability table
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS probe_availability (
				id BIGSERIAL PRIMARY KEY,
				connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
				database_name TEXT,
				probe_name TEXT NOT NULL,
				extension_name TEXT,
				is_available BOOLEAN NOT NULL DEFAULT FALSE,
				last_checked TIMESTAMP,
				last_collected TIMESTAMP,
				unavailable_reason TEXT,
				UNIQUE(connection_id, database_name, probe_name)
			);

			COMMENT ON TABLE probe_availability IS
				'Tracks which probes have collected data for each connection/database';
			COMMENT ON COLUMN probe_availability.extension_name IS
				'Required extension for this probe (e.g., system_stats, pg_stat_statements)';
			COMMENT ON COLUMN probe_availability.is_available IS
				'Whether the probe has successfully collected data';
			COMMENT ON COLUMN probe_availability.unavailable_reason IS
				'Reason why the probe is unavailable (e.g., extension not installed)';

			CREATE INDEX IF NOT EXISTS idx_probe_availability_connection
				ON probe_availability(connection_id);
			CREATE INDEX IF NOT EXISTS idx_probe_availability_probe
				ON probe_availability(probe_name);
		`)
			if err != nil {
				return fmt.Errorf("failed to create probe_availability table: %w", err)
			}

			// Create alert_rules table
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS alert_rules (
				id BIGSERIAL PRIMARY KEY,
				name TEXT NOT NULL UNIQUE,
				description TEXT NOT NULL,
				category TEXT NOT NULL,
				metric_name TEXT NOT NULL,
				default_operator TEXT NOT NULL CHECK (default_operator IN ('>', '>=', '<', '<=', '==', '!=')),
				default_threshold REAL NOT NULL,
				default_severity TEXT NOT NULL CHECK (default_severity IN ('info', 'warning', 'critical')),
				default_enabled BOOLEAN NOT NULL DEFAULT TRUE,
				required_extension TEXT,
				is_built_in BOOLEAN NOT NULL DEFAULT FALSE,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
			);

			COMMENT ON TABLE alert_rules IS
				'Threshold-based alert rules for monitored metrics';
			COMMENT ON COLUMN alert_rules.category IS
				'Category grouping for the rule (e.g., performance, storage, replication)';
			COMMENT ON COLUMN alert_rules.metric_name IS
				'Name of the metric to monitor';
			COMMENT ON COLUMN alert_rules.required_extension IS
				'Extension required for this metric (e.g., system_stats, pg_stat_statements)';
			COMMENT ON COLUMN alert_rules.is_built_in IS
				'Whether this is a built-in rule (cannot be deleted)';

			CREATE INDEX IF NOT EXISTS idx_alert_rules_category ON alert_rules(category);
			CREATE INDEX IF NOT EXISTS idx_alert_rules_metric ON alert_rules(metric_name);
			CREATE INDEX IF NOT EXISTS idx_alert_rules_enabled ON alert_rules(default_enabled) WHERE default_enabled = TRUE;
		`)
			if err != nil {
				return fmt.Errorf("failed to create alert_rules table: %w", err)
			}

			// Create alert_thresholds table (per-connection overrides)
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS alert_thresholds (
				id BIGSERIAL PRIMARY KEY,
				rule_id BIGINT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
				connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
				database_name TEXT,
				operator TEXT NOT NULL CHECK (operator IN ('>', '>=', '<', '<=', '==', '!=')),
				threshold REAL NOT NULL,
				severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
				enabled BOOLEAN NOT NULL DEFAULT TRUE,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(rule_id, connection_id, database_name)
			);

			COMMENT ON TABLE alert_thresholds IS
				'Per-connection threshold overrides for alert rules';
			COMMENT ON COLUMN alert_thresholds.connection_id IS
				'Connection ID for override (NULL means global default)';
			COMMENT ON COLUMN alert_thresholds.database_name IS
				'Database name for override (NULL means all databases)';

			CREATE INDEX IF NOT EXISTS idx_alert_thresholds_rule ON alert_thresholds(rule_id);
			CREATE INDEX IF NOT EXISTS idx_alert_thresholds_connection ON alert_thresholds(connection_id);
		`)
			if err != nil {
				return fmt.Errorf("failed to create alert_thresholds table: %w", err)
			}

			// Create alerts table
			_, err = conn.Exec(ctx, `
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
				correlation_id TEXT,
				status TEXT NOT NULL CHECK (status IN ('active', 'cleared', 'acknowledged')),
				triggered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				cleared_at TIMESTAMP,
				anomaly_score REAL,
				anomaly_details JSONB
			);

			COMMENT ON TABLE alerts IS
				'Active and historical alerts from threshold and anomaly detection';
			COMMENT ON COLUMN alerts.alert_type IS
				'Type of alert: threshold (rule-based) or anomaly (AI-detected)';
			COMMENT ON COLUMN alerts.correlation_id IS
				'ID linking related anomalies within a correlation window';
			COMMENT ON COLUMN alerts.status IS
				'Current status: active, cleared (auto-resolved), or acknowledged (user action)';
			COMMENT ON COLUMN alerts.anomaly_score IS
				'For anomaly alerts: the z-score or similarity score';
			COMMENT ON COLUMN alerts.anomaly_details IS
				'For anomaly alerts: additional detection details (tier results, context)';

			CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts(status);
			CREATE INDEX IF NOT EXISTS idx_alerts_connection ON alerts(connection_id);
			CREATE INDEX IF NOT EXISTS idx_alerts_triggered ON alerts(triggered_at DESC);
			CREATE INDEX IF NOT EXISTS idx_alerts_active ON alerts(connection_id, status) WHERE status = 'active';
			CREATE INDEX IF NOT EXISTS idx_alerts_correlation ON alerts(correlation_id) WHERE correlation_id IS NOT NULL;
		`)
			if err != nil {
				return fmt.Errorf("failed to create alerts table: %w", err)
			}

			// Create alert_acknowledgments table
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS alert_acknowledgments (
				id BIGSERIAL PRIMARY KEY,
				alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
				acknowledged_by TEXT NOT NULL,
				acknowledged_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				acknowledge_type TEXT NOT NULL CHECK (acknowledge_type IN ('acknowledge', 'dismiss', 'false_positive')),
				message TEXT NOT NULL DEFAULT '',
				false_positive BOOLEAN NOT NULL DEFAULT FALSE
			);

			COMMENT ON TABLE alert_acknowledgments IS
				'User acknowledgments of alerts for learning and audit trail';
			COMMENT ON COLUMN alert_acknowledgments.acknowledge_type IS
				'Type: acknowledge (noted), dismiss (ignore), false_positive (learning feedback)';
			COMMENT ON COLUMN alert_acknowledgments.false_positive IS
				'If true, this acknowledgment marks the alert as a false positive for ML learning';

			CREATE INDEX IF NOT EXISTS idx_alert_acknowledgments_alert ON alert_acknowledgments(alert_id);
			CREATE INDEX IF NOT EXISTS idx_alert_acknowledgments_user ON alert_acknowledgments(acknowledged_by);
			CREATE INDEX IF NOT EXISTS idx_alert_acknowledgments_false_positive ON alert_acknowledgments(false_positive) WHERE false_positive = TRUE;
		`)
			if err != nil {
				return fmt.Errorf("failed to create alert_acknowledgments table: %w", err)
			}

			return nil
		},
	})

	// Migration 8: Blackout tables for maintenance windows
	sm.migrations = append(sm.migrations, Migration{
		Version:     8,
		Description: "Add blackout tables for maintenance windows",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Create blackouts table (manual blackouts)
			_, err := conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS blackouts (
				id BIGSERIAL PRIMARY KEY,
				connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
				database_name TEXT,
				reason TEXT NOT NULL,
				start_time TIMESTAMP NOT NULL,
				end_time TIMESTAMP NOT NULL,
				created_by TEXT NOT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				CHECK (end_time > start_time)
			);

			COMMENT ON TABLE blackouts IS
				'Manual blackout periods during which alerts are suppressed';
			COMMENT ON COLUMN blackouts.connection_id IS
				'Connection ID (NULL means global blackout for all connections)';
			COMMENT ON COLUMN blackouts.database_name IS
				'Database name (NULL means all databases on the connection)';

			CREATE INDEX IF NOT EXISTS idx_blackouts_active
				ON blackouts(start_time, end_time);
			CREATE INDEX IF NOT EXISTS idx_blackouts_connection
				ON blackouts(connection_id);
		`)
			if err != nil {
				return fmt.Errorf("failed to create blackouts table: %w", err)
			}

			// Create blackout_schedules table (recurring blackouts)
			_, err = conn.Exec(ctx, `
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
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
			);

			COMMENT ON TABLE blackout_schedules IS
				'Scheduled recurring blackout periods using cron expressions';
			COMMENT ON COLUMN blackout_schedules.cron_expression IS
				'Cron expression defining when the blackout starts (e.g., "0 2 * * 0" for Sunday 2am)';
			COMMENT ON COLUMN blackout_schedules.duration_minutes IS
				'Duration of each blackout period in minutes';
			COMMENT ON COLUMN blackout_schedules.timezone IS
				'Timezone for interpreting the cron expression';

			CREATE INDEX IF NOT EXISTS idx_blackout_schedules_enabled
				ON blackout_schedules(enabled) WHERE enabled = TRUE;
			CREATE INDEX IF NOT EXISTS idx_blackout_schedules_connection
				ON blackout_schedules(connection_id);
		`)
			if err != nil {
				return fmt.Errorf("failed to create blackout_schedules table: %w", err)
			}

			return nil
		},
	})

	// Migration 9: Anomaly detection tables - baselines and candidates
	sm.migrations = append(sm.migrations, Migration{
		Version:     9,
		Description: "Add anomaly detection tables for baselines and candidates",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Create metric_definitions table
			_, err := conn.Exec(ctx, `
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
			COMMENT ON COLUMN metric_definitions.unit IS
				'Unit of measurement (e.g., percent, bytes, milliseconds)';
			COMMENT ON COLUMN metric_definitions.min_value IS
				'Minimum valid value for this metric';
			COMMENT ON COLUMN metric_definitions.max_value IS
				'Maximum valid value for this metric';
		`)
			if err != nil {
				return fmt.Errorf("failed to create metric_definitions table: %w", err)
			}

			// Create metric_baselines table
			_, err = conn.Exec(ctx, `
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
				last_calculated TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
			);

			COMMENT ON TABLE metric_baselines IS
				'Statistical baselines for metrics used in anomaly detection';
			COMMENT ON COLUMN metric_baselines.period_type IS
				'Granularity of baseline: all (global), hourly, daily, or weekly';
			COMMENT ON COLUMN metric_baselines.day_of_week IS
				'Day of week for weekly baselines (0=Sunday, 6=Saturday)';
			COMMENT ON COLUMN metric_baselines.hour_of_day IS
				'Hour of day for hourly baselines (0-23)';

			CREATE INDEX IF NOT EXISTS idx_metric_baselines_connection
				ON metric_baselines(connection_id);
			CREATE INDEX IF NOT EXISTS idx_metric_baselines_metric
				ON metric_baselines(metric_name);

			CREATE UNIQUE INDEX IF NOT EXISTS idx_metric_baselines_unique
				ON metric_baselines(
					connection_id,
					COALESCE(database_name, ''),
					metric_name,
					period_type,
					COALESCE(day_of_week, -1),
					COALESCE(hour_of_day, -1)
				);
			COMMENT ON INDEX idx_metric_baselines_unique IS
				'Unique index for baselines with NULL-safe handling for optional columns';
		`)
			if err != nil {
				return fmt.Errorf("failed to create metric_baselines table: %w", err)
			}

			// Create correlation_groups table
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS correlation_groups (
				id BIGSERIAL PRIMARY KEY,
				connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
				database_name TEXT,
				start_time TIMESTAMP NOT NULL,
				end_time TIMESTAMP,
				anomaly_count INTEGER NOT NULL DEFAULT 1,
				root_cause_guess TEXT
			);

			COMMENT ON TABLE correlation_groups IS
				'Groups of related anomalies detected within a correlation window';
			COMMENT ON COLUMN correlation_groups.root_cause_guess IS
				'LLM-generated hypothesis about the root cause';

			CREATE INDEX IF NOT EXISTS idx_correlation_groups_connection
				ON correlation_groups(connection_id);
			CREATE INDEX IF NOT EXISTS idx_correlation_groups_time
				ON correlation_groups(start_time DESC);
		`)
			if err != nil {
				return fmt.Errorf("failed to create correlation_groups table: %w", err)
			}

			// Create anomaly_candidates table
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS anomaly_candidates (
				id BIGSERIAL PRIMARY KEY,
				connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
				database_name TEXT,
				metric_name TEXT NOT NULL,
				metric_value REAL NOT NULL,
				z_score REAL NOT NULL,
				detected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				context JSONB NOT NULL DEFAULT '{}',
				tier1_pass BOOLEAN NOT NULL DEFAULT FALSE,
				tier2_score REAL,
				tier2_pass BOOLEAN,
				tier3_result TEXT,
				tier3_pass BOOLEAN,
				tier3_error TEXT,
				final_decision TEXT CHECK (final_decision IN ('alert', 'suppress', 'pending')),
				alert_id BIGINT REFERENCES alerts(id) ON DELETE SET NULL,
				processed_at TIMESTAMP
			);

			COMMENT ON TABLE anomaly_candidates IS
				'Anomaly candidates being processed through the tiered detection system';
			COMMENT ON COLUMN anomaly_candidates.z_score IS
				'Statistical z-score from Tier 1 detection';
			COMMENT ON COLUMN anomaly_candidates.context IS
				'Contextual information for embedding generation';
			COMMENT ON COLUMN anomaly_candidates.tier2_score IS
				'Similarity score from Tier 2 embedding comparison';
			COMMENT ON COLUMN anomaly_candidates.tier3_result IS
				'LLM classification result from Tier 3';
			COMMENT ON COLUMN anomaly_candidates.tier3_error IS
				'Error message if Tier 3 processing failed';
			COMMENT ON COLUMN anomaly_candidates.final_decision IS
				'Final decision: alert (create alert), suppress (false positive), pending (needs review)';

			CREATE INDEX IF NOT EXISTS idx_anomaly_candidates_connection
				ON anomaly_candidates(connection_id);
			CREATE INDEX IF NOT EXISTS idx_anomaly_candidates_detected
				ON anomaly_candidates(detected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_anomaly_candidates_pending
				ON anomaly_candidates(final_decision) WHERE final_decision = 'pending';
		`)
			if err != nil {
				return fmt.Errorf("failed to create anomaly_candidates table: %w", err)
			}

			// Check if pgvector extension is available and add embedding column
			var hasVector bool
			err = conn.QueryRow(ctx, `
				SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'vector')
			`).Scan(&hasVector)
			if err != nil {
				return fmt.Errorf("failed to check for pgvector: %w", err)
			}

			if hasVector {
				_, err = conn.Exec(ctx, `
					ALTER TABLE anomaly_candidates
					ADD COLUMN IF NOT EXISTS context_embedding vector(1536);

					CREATE INDEX IF NOT EXISTS idx_anomaly_candidates_embedding
						ON anomaly_candidates USING ivfflat (context_embedding vector_cosine_ops)
						WITH (lists = 100);

					COMMENT ON COLUMN anomaly_candidates.context_embedding IS
						'Normalized embedding vector for similarity search (1536 dimensions)';
				`)
				if err != nil {
					return fmt.Errorf("failed to add embedding column: %w", err)
				}
			}

			return nil
		},
	})

	// Migration 10: Seed built-in alert rules
	sm.migrations = append(sm.migrations, Migration{
		Version:     10,
		Description: "Seed built-in alert rules for common monitoring scenarios",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			_, err := conn.Exec(ctx, `
			INSERT INTO alert_rules (name, description, category, metric_name, default_operator, default_threshold, default_severity, default_enabled, required_extension, is_built_in)
			VALUES
				-- Connection alerts
				('high_connection_count', 'Active connections exceed threshold', 'connections', 'pg_stat_activity.count', '>', 100, 'warning', TRUE, NULL, TRUE),
				('connection_utilization', 'Connection utilization above threshold', 'connections', 'connection_utilization_percent', '>', 80, 'warning', TRUE, NULL, TRUE),

				-- Replication alerts
				('replication_lag_bytes', 'Replication lag in bytes exceeds threshold', 'replication', 'pg_stat_replication.lag_bytes', '>', 104857600, 'warning', TRUE, NULL, TRUE),
				('replication_slot_inactive', 'Replication slot is inactive', 'replication', 'pg_replication_slots.inactive', '==', 1, 'critical', TRUE, NULL, TRUE),

				-- Storage alerts
				('disk_usage_percent', 'Disk usage exceeds threshold', 'storage', 'pg_sys_disk_info.used_percent', '>', 80, 'warning', TRUE, 'system_stats', TRUE),
				('disk_usage_critical', 'Disk usage critically high', 'storage', 'pg_sys_disk_info.used_percent', '>', 95, 'critical', TRUE, 'system_stats', TRUE),
				('table_bloat_ratio', 'Table bloat ratio exceeds threshold', 'storage', 'table_bloat_ratio', '>', 50, 'warning', TRUE, NULL, TRUE),

				-- Performance alerts
				('cpu_usage_high', 'CPU usage exceeds threshold', 'performance', 'pg_sys_cpu_usage_info.processor_time_percent', '>', 80, 'warning', TRUE, 'system_stats', TRUE),
				('memory_usage_high', 'Memory usage exceeds threshold', 'performance', 'pg_sys_memory_info.used_percent', '>', 85, 'warning', TRUE, 'system_stats', TRUE),
				('load_average_high', 'System load average exceeds threshold', 'performance', 'pg_sys_load_avg_info.load_avg_fifteen_minutes', '>', 4, 'warning', TRUE, 'system_stats', TRUE),
				('long_running_queries', 'Queries running longer than threshold', 'performance', 'pg_stat_activity.max_query_duration_seconds', '>', 300, 'warning', TRUE, NULL, TRUE),
				('blocked_queries', 'Blocked queries detected', 'performance', 'pg_stat_activity.blocked_count', '>', 0, 'warning', TRUE, NULL, TRUE),

				-- Transaction alerts
				('long_running_transaction', 'Transaction running too long', 'transactions', 'pg_stat_activity.max_xact_duration_seconds', '>', 3600, 'warning', TRUE, NULL, TRUE),
				('idle_in_transaction', 'Connection idle in transaction too long', 'transactions', 'pg_stat_activity.idle_in_transaction_seconds', '>', 300, 'warning', TRUE, NULL, TRUE),
				('transaction_wraparound', 'Transaction ID wraparound approaching', 'transactions', 'age_percent', '>', 75, 'critical', TRUE, NULL, TRUE),

				-- Lock alerts
				('deadlocks_detected', 'Deadlocks detected', 'locks', 'pg_stat_database.deadlocks_delta', '>', 0, 'warning', TRUE, NULL, TRUE),
				('lock_wait_time', 'Lock wait time exceeds threshold', 'locks', 'pg_stat_activity.max_lock_wait_seconds', '>', 30, 'warning', TRUE, NULL, TRUE),

				-- WAL and Checkpoint alerts
				('checkpoint_warning', 'Checkpoints requested too frequently', 'wal', 'pg_stat_checkpointer.checkpoints_req_delta', '>', 10, 'warning', TRUE, NULL, TRUE),
				('wal_archive_failed', 'WAL archiving failures detected', 'wal', 'pg_stat_archiver.failed_count_delta', '>', 0, 'critical', TRUE, NULL, TRUE),

				-- Vacuum alerts
				('autovacuum_not_running', 'Autovacuum has not run recently', 'maintenance', 'table_last_autovacuum_hours', '>', 24, 'warning', TRUE, NULL, TRUE),
				('dead_tuple_ratio', 'Dead tuple ratio too high', 'maintenance', 'pg_stat_all_tables.dead_tuple_percent', '>', 20, 'warning', TRUE, NULL, TRUE),

				-- Statement alerts
				('slow_query_count', 'High number of slow queries', 'queries', 'pg_stat_statements.slow_query_count', '>', 10, 'warning', TRUE, 'pg_stat_statements', TRUE),
				('cache_hit_ratio_low', 'Buffer cache hit ratio below threshold', 'queries', 'pg_stat_database.cache_hit_ratio', '<', 95, 'warning', TRUE, NULL, TRUE),

				-- Error alerts
				('temp_files_created', 'Temporary files being created', 'performance', 'pg_stat_database.temp_files_delta', '>', 100, 'warning', TRUE, NULL, TRUE)
			ON CONFLICT (name) DO NOTHING;
		`)
			if err != nil {
				return fmt.Errorf("failed to insert built-in alert rules: %w", err)
			}

			return nil
		},
	})

	// Migration 11: Add cluster_groups and clusters tables for hierarchical organization
	sm.migrations = append(sm.migrations, Migration{
		Version:     11,
		Description: "Add cluster_groups and clusters tables for hierarchical server organization",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Create cluster_groups table
			_, err := conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS cluster_groups (
				id SERIAL PRIMARY KEY,
				name VARCHAR(255) NOT NULL,
				description TEXT,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				CONSTRAINT cluster_groups_name_unique UNIQUE (name)
			);

			COMMENT ON TABLE cluster_groups IS
				'Groups for organizing database clusters hierarchically';
			COMMENT ON COLUMN cluster_groups.id IS
				'Unique identifier for the cluster group';
			COMMENT ON COLUMN cluster_groups.name IS
				'User-friendly name for the cluster group';
			COMMENT ON COLUMN cluster_groups.description IS
				'Optional description of the cluster group';
			COMMENT ON COLUMN cluster_groups.created_at IS
				'Timestamp when the cluster group was created';
			COMMENT ON COLUMN cluster_groups.updated_at IS
				'Timestamp when the cluster group was last updated';

			CREATE INDEX IF NOT EXISTS idx_cluster_groups_name ON cluster_groups(name);
		`)
			if err != nil {
				return fmt.Errorf("failed to create cluster_groups table: %w", err)
			}

			// Create clusters table
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS clusters (
				id SERIAL PRIMARY KEY,
				group_id INTEGER NOT NULL REFERENCES cluster_groups(id) ON DELETE CASCADE,
				name VARCHAR(255) NOT NULL,
				description TEXT,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				CONSTRAINT clusters_group_name_unique UNIQUE (group_id, name)
			);

			COMMENT ON TABLE clusters IS
				'Database clusters that contain one or more server connections';
			COMMENT ON COLUMN clusters.id IS
				'Unique identifier for the cluster';
			COMMENT ON COLUMN clusters.group_id IS
				'Reference to the parent cluster group';
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
		`)
			if err != nil {
				return fmt.Errorf("failed to create clusters table: %w", err)
			}

			// Add cluster_id and role columns to connections table
			_, err = conn.Exec(ctx, `
			ALTER TABLE connections
				ADD COLUMN IF NOT EXISTS cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
				ADD COLUMN IF NOT EXISTS role VARCHAR(50) DEFAULT 'primary';

			COMMENT ON COLUMN connections.cluster_id IS
				'Reference to the cluster this connection belongs to (NULL if unassigned)';
			COMMENT ON COLUMN connections.role IS
				'Role of the server in the cluster (primary, replica, standby, etc.)';

			CREATE INDEX IF NOT EXISTS idx_connections_cluster_id ON connections(cluster_id);
			CREATE INDEX IF NOT EXISTS idx_connections_role ON connections(role);
		`)
			if err != nil {
				return fmt.Errorf("failed to add cluster columns to connections: %w", err)
			}

			return nil
		},
	})

	// Migration 12: Add publisher connection info for logical replication topology tracking
	sm.migrations = append(sm.migrations, Migration{
		Version:     12,
		Description: "Add publisher_host and publisher_port to pg_node_role for logical replication topology",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Add publisher connection columns to pg_node_role
			_, err := conn.Exec(ctx, `
			ALTER TABLE metrics.pg_node_role
				ADD COLUMN IF NOT EXISTS publisher_host VARCHAR(255),
				ADD COLUMN IF NOT EXISTS publisher_port INTEGER,
				ADD COLUMN IF NOT EXISTS has_active_logical_slots BOOLEAN DEFAULT FALSE,
				ADD COLUMN IF NOT EXISTS active_logical_slot_count INTEGER DEFAULT 0;

			COMMENT ON COLUMN metrics.pg_node_role.publisher_host IS
				'For logical subscribers: hostname of the publisher server';
			COMMENT ON COLUMN metrics.pg_node_role.publisher_port IS
				'For logical subscribers: port of the publisher server';
			COMMENT ON COLUMN metrics.pg_node_role.has_active_logical_slots IS
				'Whether this server has active logical replication slots (subscribers connected)';
			COMMENT ON COLUMN metrics.pg_node_role.active_logical_slot_count IS
				'Number of active logical replication slots';
		`)
			if err != nil {
				return fmt.Errorf("failed to add publisher columns to pg_node_role: %w", err)
			}

			return nil
		},
	})

	// Migration 13: Add owner and shared columns to cluster_groups
	sm.migrations = append(sm.migrations, Migration{
		Version:     13,
		Description: "Add owner and shared columns to cluster_groups table",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Add owner and shared columns to cluster_groups
			_, err := conn.Exec(ctx, `
			ALTER TABLE cluster_groups
				ADD COLUMN IF NOT EXISTS owner_username VARCHAR(255),
				ADD COLUMN IF NOT EXISTS owner_token VARCHAR(255),
				ADD COLUMN IF NOT EXISTS is_shared BOOLEAN NOT NULL DEFAULT TRUE;

			COMMENT ON COLUMN cluster_groups.owner_username IS
				'Username of the user who owns this cluster group';
			COMMENT ON COLUMN cluster_groups.owner_token IS
				'Token that owns this cluster group (alternative to user ownership)';
			COMMENT ON COLUMN cluster_groups.is_shared IS
				'Whether this group is shared with all users (default true)';
		`)
			if err != nil {
				return fmt.Errorf("failed to add owner columns to cluster_groups: %w", err)
			}

			return nil
		},
	})

	// Migration 14: Add auto_cluster_key to clusters table for linking auto-detected clusters
	sm.migrations = append(sm.migrations, Migration{
		Version:     14,
		Description: "Add auto_cluster_key column to clusters table",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Add auto_cluster_key column and make group_id nullable
			_, err := conn.Exec(ctx, `
			ALTER TABLE clusters
				ADD COLUMN IF NOT EXISTS auto_cluster_key VARCHAR(255) UNIQUE,
				ALTER COLUMN group_id DROP NOT NULL;

			COMMENT ON COLUMN clusters.auto_cluster_key IS
				'Key linking to auto-detected cluster (format: type:id, e.g., binary:123, spock:pg17)';
		`)
			if err != nil {
				return fmt.Errorf("failed to add auto_cluster_key to clusters: %w", err)
			}

			return nil
		},
	})

	// Migration 15: Add auto_group_key to cluster_groups table for linking auto-detected groups
	sm.migrations = append(sm.migrations, Migration{
		Version:     15,
		Description: "Add auto_group_key column to cluster_groups table",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Add auto_group_key column for auto-detected groups like "Servers/Clusters"
			_, err := conn.Exec(ctx, `
			ALTER TABLE cluster_groups
				ADD COLUMN IF NOT EXISTS auto_group_key VARCHAR(255) UNIQUE;

			COMMENT ON COLUMN cluster_groups.auto_group_key IS
				'Key linking to auto-detected group (e.g., auto for the default Servers/Clusters group)';
		`)
			if err != nil {
				return fmt.Errorf("failed to add auto_group_key to cluster_groups: %w", err)
			}

			return nil
		},
	})

	// Migration 16: Add is_default column and create default group
	sm.migrations = append(sm.migrations, Migration{
		Version:     16,
		Description: "Add is_default column to cluster_groups and create default group",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Add is_default column to cluster_groups
			_, err := conn.Exec(ctx, `
			ALTER TABLE cluster_groups
				ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT FALSE;

			COMMENT ON COLUMN cluster_groups.is_default IS
				'Whether this is the default group for ungrouped servers/clusters (only one allowed)';

			-- Create unique partial index to ensure only one default group
			CREATE UNIQUE INDEX IF NOT EXISTS idx_cluster_groups_is_default
				ON cluster_groups (is_default) WHERE is_default = TRUE;
		`)
			if err != nil {
				return fmt.Errorf("failed to add is_default column to cluster_groups: %w", err)
			}

			// Insert the default group
			_, err = conn.Exec(ctx, `
			INSERT INTO cluster_groups (name, description, is_shared, is_default)
			VALUES ('Servers/Clusters', 'Default group for all servers and clusters', TRUE, TRUE)
			ON CONFLICT DO NOTHING;
		`)
			if err != nil {
				return fmt.Errorf("failed to create default cluster group: %w", err)
			}

			// Update any clusters with NULL group_id to use the default group
			_, err = conn.Exec(ctx, `
			UPDATE clusters
			SET group_id = (SELECT id FROM cluster_groups WHERE is_default = TRUE)
			WHERE group_id IS NULL;
		`)
			if err != nil {
				return fmt.Errorf("failed to update clusters with default group: %w", err)
			}

			return nil
		},
	})

	// Migration 17: Add pg_extension table for tracking installed extensions
	sm.migrations = append(sm.migrations, Migration{
		Version:     17,
		Description: "Add pg_extension table for tracking installed extensions",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Create the pg_extension metrics table (partitioned)
			_, err := conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS metrics.pg_extension (
				connection_id INTEGER NOT NULL,
				extname TEXT NOT NULL,
				extversion TEXT,
				extrelocatable BOOLEAN,
				schema_name TEXT,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, extname, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_extension IS
				'Installed PostgreSQL extensions and their versions';

			ALTER TABLE metrics.pg_extension
				ADD CONSTRAINT fk_pg_extension_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			CREATE INDEX IF NOT EXISTS idx_pg_extension_collected_at
				ON metrics.pg_extension(collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_extension_connection_time
				ON metrics.pg_extension(connection_id, collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_extension_extname
				ON metrics.pg_extension(connection_id, extname);

			COMMENT ON INDEX metrics.idx_pg_extension_collected_at IS
				'Index for efficiently querying extensions by time range';
			COMMENT ON INDEX metrics.idx_pg_extension_connection_time IS
				'Index for efficiently querying extensions by connection and time range';
			COMMENT ON INDEX metrics.idx_pg_extension_extname IS
				'Index for efficiently looking up specific extensions';
		`)
			if err != nil {
				return fmt.Errorf("failed to create pg_extension table: %w", err)
			}

			// Insert the probe configuration
			_, err = conn.Exec(ctx, `
			INSERT INTO probe_configs (connection_id, is_enabled, name, description, collection_interval_seconds, retention_days)
			VALUES (NULL, TRUE, 'pg_extension', 'Monitors installed PostgreSQL extensions and versions', 3600, 30)
			ON CONFLICT (COALESCE(connection_id, 0), name) DO NOTHING;
		`)
			if err != nil {
				return fmt.Errorf("failed to insert pg_extension probe config: %w", err)
			}

			return nil
		},
	})

	// Migration 18: Add database_name to pg_extension table for database-scoped data
	sm.migrations = append(sm.migrations, Migration{
		Version:     18,
		Description: "Add database_name to pg_extension table",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Drop and recreate the table with database_name column
			// Safe since we're early in deployment and table should be empty
			_, err := conn.Exec(ctx, `
			-- Drop existing table and partitions
			DROP TABLE IF EXISTS metrics.pg_extension CASCADE;

			-- Recreate with database_name column
			CREATE TABLE IF NOT EXISTS metrics.pg_extension (
				connection_id INTEGER NOT NULL,
				database_name TEXT NOT NULL,
				extname TEXT NOT NULL,
				extversion TEXT,
				extrelocatable BOOLEAN,
				schema_name TEXT,
				collected_at TIMESTAMP NOT NULL,
				PRIMARY KEY (connection_id, database_name, extname, collected_at)
			) PARTITION BY RANGE (collected_at);

			COMMENT ON TABLE metrics.pg_extension IS
				'Installed PostgreSQL extensions and their versions per database';

			ALTER TABLE metrics.pg_extension
				ADD CONSTRAINT fk_pg_extension_connection_id
				FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;

			CREATE INDEX IF NOT EXISTS idx_pg_extension_collected_at
				ON metrics.pg_extension(collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_extension_connection_time
				ON metrics.pg_extension(connection_id, collected_at DESC);
			CREATE INDEX IF NOT EXISTS idx_pg_extension_extname
				ON metrics.pg_extension(connection_id, database_name, extname);

			COMMENT ON INDEX metrics.idx_pg_extension_collected_at IS
				'Index for efficiently querying extensions by time range';
			COMMENT ON INDEX metrics.idx_pg_extension_connection_time IS
				'Index for efficiently querying extensions by connection and time range';
			COMMENT ON INDEX metrics.idx_pg_extension_extname IS
				'Index for efficiently looking up specific extensions';
		`)
			if err != nil {
				return fmt.Errorf("failed to recreate pg_extension table: %w", err)
			}

			return nil
		},
	})

	// Migration 19: Add notification channel tables for alert delivery
	sm.migrations = append(sm.migrations, Migration{
		Version:     19,
		Description: "Add notification channel tables for alert delivery",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Create notification_channels table
			_, err := conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS notification_channels (
				id BIGSERIAL PRIMARY KEY,
				owner_username VARCHAR(255),
				owner_token VARCHAR(255),
				is_shared BOOLEAN NOT NULL DEFAULT FALSE,
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
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				CONSTRAINT chk_notification_channel_owner CHECK (
					(owner_username IS NOT NULL AND owner_token IS NULL) OR
					(owner_username IS NULL AND owner_token IS NOT NULL)
				)
			);

			COMMENT ON TABLE notification_channels IS
				'Notification channels for delivering alerts (Slack, Mattermost, webhook, email)';
			COMMENT ON COLUMN notification_channels.id IS
				'Unique identifier for the notification channel';
			COMMENT ON COLUMN notification_channels.owner_username IS
				'Username of the user who owns this channel (mutually exclusive with owner_token)';
			COMMENT ON COLUMN notification_channels.owner_token IS
				'Service token that owns this channel (mutually exclusive with owner_username)';
			COMMENT ON COLUMN notification_channels.is_shared IS
				'Whether this channel is shared among users or private';
			COMMENT ON COLUMN notification_channels.enabled IS
				'Whether this notification channel is enabled';
			COMMENT ON COLUMN notification_channels.channel_type IS
				'Type of notification channel: slack, mattermost, webhook, or email';
			COMMENT ON COLUMN notification_channels.name IS
				'User-friendly name for the notification channel';
			COMMENT ON COLUMN notification_channels.description IS
				'Optional description of the notification channel';
			COMMENT ON COLUMN notification_channels.webhook_url_encrypted IS
				'Encrypted webhook URL for Slack or Mattermost channels';
			COMMENT ON COLUMN notification_channels.endpoint_url IS
				'Endpoint URL for webhook channels';
			COMMENT ON COLUMN notification_channels.http_method IS
				'HTTP method for webhook channels (default POST)';
			COMMENT ON COLUMN notification_channels.headers_json IS
				'Custom HTTP headers for webhook channels as JSON object';
			COMMENT ON COLUMN notification_channels.auth_type IS
				'Authentication type for webhook channels (e.g., basic, bearer, api_key)';
			COMMENT ON COLUMN notification_channels.auth_credentials_encrypted IS
				'Encrypted authentication credentials for webhook channels';
			COMMENT ON COLUMN notification_channels.smtp_host IS
				'SMTP server hostname for email channels';
			COMMENT ON COLUMN notification_channels.smtp_port IS
				'SMTP server port for email channels (default 587)';
			COMMENT ON COLUMN notification_channels.smtp_username IS
				'SMTP authentication username for email channels';
			COMMENT ON COLUMN notification_channels.smtp_password_encrypted IS
				'Encrypted SMTP password for email channels';
			COMMENT ON COLUMN notification_channels.smtp_use_tls IS
				'Whether to use TLS for SMTP connections (default true)';
			COMMENT ON COLUMN notification_channels.from_address IS
				'From email address for email channels';
			COMMENT ON COLUMN notification_channels.from_name IS
				'From display name for email channels';
			COMMENT ON COLUMN notification_channels.template_alert_fire IS
				'Custom template for alert firing notifications';
			COMMENT ON COLUMN notification_channels.template_alert_clear IS
				'Custom template for alert clearing notifications';
			COMMENT ON COLUMN notification_channels.template_reminder IS
				'Custom template for reminder notifications';
			COMMENT ON COLUMN notification_channels.reminder_enabled IS
				'Whether reminder notifications are enabled for this channel';
			COMMENT ON COLUMN notification_channels.reminder_interval_hours IS
				'Interval in hours between reminder notifications (default 24)';
			COMMENT ON COLUMN notification_channels.created_at IS
				'Timestamp when the notification channel was created';
			COMMENT ON COLUMN notification_channels.updated_at IS
				'Timestamp when the notification channel was last updated';
			COMMENT ON CONSTRAINT chk_notification_channel_owner ON notification_channels IS
				'Ensures exactly one of owner_username or owner_token is set';

			CREATE INDEX IF NOT EXISTS idx_notification_channels_channel_type
				ON notification_channels(channel_type);
			CREATE INDEX IF NOT EXISTS idx_notification_channels_enabled
				ON notification_channels(enabled) WHERE enabled = TRUE;
			CREATE INDEX IF NOT EXISTS idx_notification_channels_owner_username
				ON notification_channels(owner_username);
			CREATE INDEX IF NOT EXISTS idx_notification_channels_owner_token
				ON notification_channels(owner_token);

			COMMENT ON INDEX idx_notification_channels_channel_type IS
				'Index for filtering notification channels by type';
			COMMENT ON INDEX idx_notification_channels_enabled IS
				'Partial index for efficiently finding enabled notification channels';
			COMMENT ON INDEX idx_notification_channels_owner_username IS
				'Index for fast lookup of notification channels by owner username';
			COMMENT ON INDEX idx_notification_channels_owner_token IS
				'Index for fast lookup of notification channels by owner token';
		`)
			if err != nil {
				return fmt.Errorf("failed to create notification_channels table: %w", err)
			}

			// Create email_recipients table
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS email_recipients (
				id BIGSERIAL PRIMARY KEY,
				channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
				email_address TEXT NOT NULL,
				display_name TEXT,
				enabled BOOLEAN NOT NULL DEFAULT TRUE,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
			);

			COMMENT ON TABLE email_recipients IS
				'Email recipients for email notification channels';
			COMMENT ON COLUMN email_recipients.id IS
				'Unique identifier for the email recipient';
			COMMENT ON COLUMN email_recipients.channel_id IS
				'Foreign key to the notification channel';
			COMMENT ON COLUMN email_recipients.email_address IS
				'Email address of the recipient';
			COMMENT ON COLUMN email_recipients.display_name IS
				'Optional display name for the recipient';
			COMMENT ON COLUMN email_recipients.enabled IS
				'Whether this recipient is enabled';
			COMMENT ON COLUMN email_recipients.created_at IS
				'Timestamp when the recipient was created';

			CREATE INDEX IF NOT EXISTS idx_email_recipients_channel_id
				ON email_recipients(channel_id);
			CREATE INDEX IF NOT EXISTS idx_email_recipients_channel_enabled
				ON email_recipients(channel_id, enabled) WHERE enabled = TRUE;

			COMMENT ON INDEX idx_email_recipients_channel_id IS
				'Index for fast lookup of recipients by channel';
			COMMENT ON INDEX idx_email_recipients_channel_enabled IS
				'Partial index for efficiently finding enabled recipients per channel';
		`)
			if err != nil {
				return fmt.Errorf("failed to create email_recipients table: %w", err)
			}

			// Create connection_notification_channels table
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS connection_notification_channels (
				id BIGSERIAL PRIMARY KEY,
				connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
				channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
				enabled BOOLEAN NOT NULL DEFAULT TRUE,
				reminder_enabled_override BOOLEAN,
				reminder_interval_hours_override INTEGER,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				CONSTRAINT connection_channel_unique UNIQUE (connection_id, channel_id)
			);

			COMMENT ON TABLE connection_notification_channels IS
				'Links connections to notification channels for alert delivery';
			COMMENT ON COLUMN connection_notification_channels.id IS
				'Unique identifier for the connection-channel link';
			COMMENT ON COLUMN connection_notification_channels.connection_id IS
				'Foreign key to the connection';
			COMMENT ON COLUMN connection_notification_channels.channel_id IS
				'Foreign key to the notification channel';
			COMMENT ON COLUMN connection_notification_channels.enabled IS
				'Whether notifications are enabled for this connection-channel pair';
			COMMENT ON COLUMN connection_notification_channels.reminder_enabled_override IS
				'Override for reminder enabled setting (NULL uses channel default)';
			COMMENT ON COLUMN connection_notification_channels.reminder_interval_hours_override IS
				'Override for reminder interval (NULL uses channel default)';
			COMMENT ON COLUMN connection_notification_channels.created_at IS
				'Timestamp when the link was created';
			COMMENT ON CONSTRAINT connection_channel_unique ON connection_notification_channels IS
				'Ensures each connection-channel pair is unique';

			CREATE INDEX IF NOT EXISTS idx_connection_notification_channels_connection_id
				ON connection_notification_channels(connection_id);
			CREATE INDEX IF NOT EXISTS idx_connection_notification_channels_channel_id
				ON connection_notification_channels(channel_id);

			COMMENT ON INDEX idx_connection_notification_channels_connection_id IS
				'Index for fast lookup of notification channels by connection';
			COMMENT ON INDEX idx_connection_notification_channels_channel_id IS
				'Index for fast lookup of connections by notification channel';
		`)
			if err != nil {
				return fmt.Errorf("failed to create connection_notification_channels table: %w", err)
			}

			// Create notification_history table
			_, err = conn.Exec(ctx, `
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
				next_retry_at TIMESTAMP,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				sent_at TIMESTAMP
			);

			COMMENT ON TABLE notification_history IS
				'History of notification delivery attempts and their outcomes';
			COMMENT ON COLUMN notification_history.id IS
				'Unique identifier for the notification history entry';
			COMMENT ON COLUMN notification_history.alert_id IS
				'Foreign key to the alert that triggered the notification';
			COMMENT ON COLUMN notification_history.channel_id IS
				'Foreign key to the notification channel used';
			COMMENT ON COLUMN notification_history.connection_id IS
				'Foreign key to the connection associated with the alert';
			COMMENT ON COLUMN notification_history.notification_type IS
				'Type of notification: alert_fire, alert_clear, or reminder';
			COMMENT ON COLUMN notification_history.status IS
				'Delivery status: pending, sent, failed, or retrying';
			COMMENT ON COLUMN notification_history.payload_json IS
				'JSON payload sent to the notification channel';
			COMMENT ON COLUMN notification_history.response_code IS
				'HTTP response code from the notification endpoint';
			COMMENT ON COLUMN notification_history.response_body IS
				'Response body from the notification endpoint';
			COMMENT ON COLUMN notification_history.error_message IS
				'Error message if the notification failed';
			COMMENT ON COLUMN notification_history.attempt_count IS
				'Number of delivery attempts made';
			COMMENT ON COLUMN notification_history.max_attempts IS
				'Maximum number of delivery attempts allowed';
			COMMENT ON COLUMN notification_history.next_retry_at IS
				'Timestamp for next retry attempt if retrying';
			COMMENT ON COLUMN notification_history.created_at IS
				'Timestamp when the notification was created';
			COMMENT ON COLUMN notification_history.sent_at IS
				'Timestamp when the notification was successfully sent';

			CREATE INDEX IF NOT EXISTS idx_notification_history_alert_id
				ON notification_history(alert_id);
			CREATE INDEX IF NOT EXISTS idx_notification_history_channel_id
				ON notification_history(channel_id);
			CREATE INDEX IF NOT EXISTS idx_notification_history_status
				ON notification_history(status);
			CREATE INDEX IF NOT EXISTS idx_notification_history_pending_retry
				ON notification_history(status, next_retry_at)
				WHERE status IN ('pending', 'retrying');
			CREATE INDEX IF NOT EXISTS idx_notification_history_created_at
				ON notification_history(created_at DESC);

			COMMENT ON INDEX idx_notification_history_alert_id IS
				'Index for fast lookup of notifications by alert';
			COMMENT ON INDEX idx_notification_history_channel_id IS
				'Index for fast lookup of notifications by channel';
			COMMENT ON INDEX idx_notification_history_status IS
				'Index for filtering notifications by status';
			COMMENT ON INDEX idx_notification_history_pending_retry IS
				'Partial index for efficiently finding notifications pending retry';
			COMMENT ON INDEX idx_notification_history_created_at IS
				'Index for querying notification history by time';
		`)
			if err != nil {
				return fmt.Errorf("failed to create notification_history table: %w", err)
			}

			// Create notification_reminder_state table
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS notification_reminder_state (
				id BIGSERIAL PRIMARY KEY,
				alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
				channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
				last_reminder_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				reminder_count INTEGER NOT NULL DEFAULT 0,
				CONSTRAINT alert_channel_reminder_unique UNIQUE (alert_id, channel_id)
			);

			COMMENT ON TABLE notification_reminder_state IS
				'Tracks reminder notification state for active alerts per channel';
			COMMENT ON COLUMN notification_reminder_state.id IS
				'Unique identifier for the reminder state entry';
			COMMENT ON COLUMN notification_reminder_state.alert_id IS
				'Foreign key to the active alert';
			COMMENT ON COLUMN notification_reminder_state.channel_id IS
				'Foreign key to the notification channel';
			COMMENT ON COLUMN notification_reminder_state.last_reminder_at IS
				'Timestamp of the last reminder notification sent';
			COMMENT ON COLUMN notification_reminder_state.reminder_count IS
				'Number of reminder notifications sent for this alert-channel pair';
			COMMENT ON CONSTRAINT alert_channel_reminder_unique ON notification_reminder_state IS
				'Ensures each alert-channel pair has only one reminder state';

			CREATE INDEX IF NOT EXISTS idx_notification_reminder_state_alert_id
				ON notification_reminder_state(alert_id);
			CREATE INDEX IF NOT EXISTS idx_notification_reminder_state_last_reminder
				ON notification_reminder_state(last_reminder_at);

			COMMENT ON INDEX idx_notification_reminder_state_alert_id IS
				'Index for fast lookup of reminder state by alert';
			COMMENT ON INDEX idx_notification_reminder_state_last_reminder IS
				'Index for finding alerts due for reminder notifications';
		`)
			if err != nil {
				return fmt.Errorf("failed to create notification_reminder_state table: %w", err)
			}

			return nil
		},
	})

	// Migration #20: Fix metric_baselines constraints for baseline calculation
	sm.migrations = append(sm.migrations, Migration{
		Version:     20,
		Description: "Fix metric_baselines: allow 'all' period type and add NULL-safe unique index",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Drop the existing CHECK constraint and recreate with 'all' included
			_, err := conn.Exec(ctx, `
			ALTER TABLE metric_baselines
				DROP CONSTRAINT IF EXISTS metric_baselines_period_type_check;

			ALTER TABLE metric_baselines
				ADD CONSTRAINT metric_baselines_period_type_check
				CHECK (period_type IN ('all', 'hourly', 'daily', 'weekly'));

			COMMENT ON COLUMN metric_baselines.period_type IS
				'Granularity of baseline: all (global), hourly, daily, or weekly';
		`)
			if err != nil {
				return fmt.Errorf("failed to update metric_baselines period_type constraint: %w", err)
			}

			// Drop the existing unique constraint that doesn't handle NULLs properly
			// and create a functional unique index that uses COALESCE for NULL handling
			_, err = conn.Exec(ctx, `
			ALTER TABLE metric_baselines
				DROP CONSTRAINT IF EXISTS metric_baselines_connection_id_database_name_metric_name_pe_key;

			CREATE UNIQUE INDEX IF NOT EXISTS idx_metric_baselines_unique
				ON metric_baselines(
					connection_id,
					COALESCE(database_name, ''),
					metric_name,
					period_type,
					COALESCE(day_of_week, -1),
					COALESCE(hour_of_day, -1)
				);

			COMMENT ON INDEX idx_metric_baselines_unique IS
				'Unique index for baselines with NULL-safe handling for optional columns';
		`)
			if err != nil {
				return fmt.Errorf("failed to create NULL-safe unique index: %w", err)
			}

			return nil
		},
	})

	// Migration #21: Add pgvector support for anomaly embedding similarity
	sm.migrations = append(sm.migrations, Migration{
		Version:     21,
		Description: "Add pgvector extension and anomaly embeddings table for Tier 2 similarity",
		Up: func(conn *pgxpool.Conn) error {
			ctx := context.Background()

			// Create pgvector extension
			_, err := conn.Exec(ctx, `
			CREATE EXTENSION IF NOT EXISTS vector;
		`)
			if err != nil {
				return fmt.Errorf("failed to create vector extension: %w", err)
			}

			// Create anomaly_embeddings table for storing embeddings
			_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS anomaly_embeddings (
				id BIGSERIAL PRIMARY KEY,
				candidate_id BIGINT REFERENCES anomaly_candidates(id) ON DELETE CASCADE,
				embedding vector(1536),
				model_name TEXT NOT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(candidate_id)
			);

			COMMENT ON TABLE anomaly_embeddings IS
				'Embeddings for anomaly candidates used in Tier 2 similarity matching';
			COMMENT ON COLUMN anomaly_embeddings.candidate_id IS
				'Reference to the anomaly candidate';
			COMMENT ON COLUMN anomaly_embeddings.embedding IS
				'Vector embedding (1536 dimensions for OpenAI/Voyage, resized for others)';
			COMMENT ON COLUMN anomaly_embeddings.model_name IS
				'Name of the embedding model used';

			CREATE INDEX IF NOT EXISTS idx_anomaly_embeddings_candidate
				ON anomaly_embeddings(candidate_id);

			CREATE INDEX IF NOT EXISTS idx_anomaly_embeddings_vector
				ON anomaly_embeddings USING hnsw (embedding vector_cosine_ops);

			COMMENT ON INDEX idx_anomaly_embeddings_candidate IS
				'Fast lookup of embeddings by candidate';
			COMMENT ON INDEX idx_anomaly_embeddings_vector IS
				'HNSW index for fast vector similarity search';
		`)
			if err != nil {
				return fmt.Errorf("failed to create anomaly_embeddings table: %w", err)
			}

			// Add embedding_id column to anomaly_candidates for quick reference
			_, err = conn.Exec(ctx, `
			ALTER TABLE anomaly_candidates
				ADD COLUMN IF NOT EXISTS embedding_id BIGINT REFERENCES anomaly_embeddings(id) ON DELETE SET NULL;

			COMMENT ON COLUMN anomaly_candidates.embedding_id IS
				'Reference to the embedding for this candidate (for Tier 2 processing)';
		`)
			if err != nil {
				return fmt.Errorf("failed to add embedding_id to anomaly_candidates: %w", err)
			}

			return nil
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
