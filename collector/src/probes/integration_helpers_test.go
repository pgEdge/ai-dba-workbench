/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package probes

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// integrationState holds state shared across integration tests.
type integrationState struct {
	pool   *pgxpool.Pool
	dbName string
	err    error
}

var (
	integration         integrationState
	integrationOnce     sync.Once
	integrationDisabled bool
)

// TestMain wires up integration teardown for the probes package. The
// individual integration tests lazily set up the shared pool via
// requireIntegrationPool; TestMain only runs the existing tests and
// then drops the temporary database (if one was created).
func TestMain(m *testing.M) {
	code := m.Run()
	teardownIntegrationPool()
	os.Exit(code)
}

// teardownIntegrationPool closes the shared pool and drops the test
// database created by requireIntegrationPool. The two cleanup steps are
// independent: the pool close runs only if the pool was assigned, and
// the DROP DATABASE runs whenever a database name was reserved. This
// guards against leaking a temporary database when CREATE DATABASE
// succeeded but a later setup step (pgxpool.New, applyMetricsSchema)
// failed before integration.pool was set.
func teardownIntegrationPool() {
	if integration.pool != nil {
		integration.pool.Close()
		integration.pool = nil
	}

	if integration.dbName == "" {
		return
	}

	if os.Getenv("TEST_AI_WORKBENCH_KEEP_DB") == "1" ||
		os.Getenv("TEST_AI_WORKBENCH_KEEP_DB") == "true" {
		fmt.Printf(
			"Keeping probe test database: %s\n", integration.dbName)
		return
	}
	base, ok := integrationConnString()
	if !ok {
		return
	}
	adminConnStr := replaceProbeDatabase(base, "postgres")
	ctx, cancel := context.WithTimeout(
		context.Background(), 30*time.Second)
	defer cancel()
	adminPool, err := pgxpool.New(ctx, adminConnStr)
	if err != nil {
		return
	}
	defer adminPool.Close()
	if _, dropErr := adminPool.Exec(ctx, fmt.Sprintf(
		"DROP DATABASE IF EXISTS %s WITH (FORCE)",
		integration.dbName)); dropErr != nil {
		// Best-effort teardown; surface the failure to stderr so a
		// stale test database is visible without aborting the run.
		fmt.Fprintf(os.Stderr,
			"warning: drop probe test database %q: %v\n",
			integration.dbName, dropErr)
	}
}

// integrationConnString returns the base connection string for tests.
// It honors TEST_AI_WORKBENCH_SERVER (preferred) and TEST_DB_CONN
// (key=value style). When neither is set, the helper signals the caller
// to skip the test.
func integrationConnString() (string, bool) {
	if u := os.Getenv("TEST_AI_WORKBENCH_SERVER"); u != "" {
		return u, true
	}
	if u := os.Getenv("TEST_DB_CONN"); u != "" {
		return u, true
	}
	return "", false
}

// replaceProbeDatabase swaps the dbname in either a postgres URL or a
// libpq key=value string. It is independent of the database package's
// helper so the probe tests stay free of inter-package dependencies.
//
// The libpq branch uses splitLibpqFields rather than strings.Fields so
// quoted values such as `options='-c search_path=public'` survive the
// round-trip without being shredded on internal whitespace.
func replaceProbeDatabase(connStr, dbName string) string {
	if strings.HasPrefix(connStr, "postgres://") || strings.HasPrefix(connStr, "postgresql://") {
		parts := strings.SplitN(connStr, "?", 2)
		baseURL := parts[0]
		query := ""
		if len(parts) > 1 {
			query = "?" + parts[1]
		}
		lastSlash := strings.LastIndex(baseURL, "/")
		if lastSlash != -1 && lastSlash > len("postgres://") {
			baseURL = baseURL[:lastSlash+1] + dbName
		} else {
			baseURL = baseURL + "/" + dbName
		}
		return baseURL + query
	}
	parts := splitLibpqFields(connStr)
	out := make([]string, 0, len(parts)+1)
	found := false
	for _, p := range parts {
		if strings.HasPrefix(p, "dbname=") {
			out = append(out, "dbname="+dbName)
			found = true
			continue
		}
		out = append(out, p)
	}
	if !found {
		out = append(out, "dbname="+dbName)
	}
	return strings.Join(out, " ")
}

// splitLibpqFields splits a libpq key=value connection string into its
// constituent fields, preserving single-quoted values verbatim so the
// round-trip output remains parseable by libpq. Backslash escapes
// inside a quoted value are passed through as-is (the goal here is a
// faithful round-trip, not interpretation). Whitespace outside quoted
// values is treated as a delimiter.
func splitLibpqFields(s string) []string {
	var (
		out     []string
		current []byte
		inQuote bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote:
			// Pass backslash escapes through verbatim so the output
			// preserves the original encoding exactly.
			if c == '\\' && i+1 < len(s) {
				current = append(current, c, s[i+1])
				i++
				continue
			}
			current = append(current, c)
			if c == '\'' {
				inQuote = false
			}
		case c == '\'':
			inQuote = true
			current = append(current, c)
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if len(current) > 0 {
				out = append(out, string(current))
				current = current[:0]
			}
		default:
			current = append(current, c)
		}
	}
	if len(current) > 0 {
		out = append(out, string(current))
	}
	return out
}

// requireIntegrationPool returns a connection pool to a freshly created
// test database that already has the minimal `metrics` schema applied.
// The pool is shared across tests in the package to amortize the cost
// of database creation. Tests that use the pool must not drop or
// recreate the metrics tables.
func requireIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}

	integrationOnce.Do(func() {
		base, ok := integrationConnString()
		if !ok {
			integrationDisabled = true
			return
		}
		integration.dbName = fmt.Sprintf(
			"ai_workbench_probes_%d", time.Now().UnixNano())
		adminConnStr := replaceProbeDatabase(base, "postgres")

		ctx, cancel := context.WithTimeout(
			context.Background(), 30*time.Second)
		defer cancel()

		adminPool, err := pgxpool.New(ctx, adminConnStr)
		if err != nil {
			integration.err = fmt.Errorf(
				"connect to admin db: %w", err)
			return
		}
		defer adminPool.Close()

		if err := adminPool.Ping(ctx); err != nil {
			integration.err = fmt.Errorf("ping admin db: %w", err)
			return
		}

		_, err = adminPool.Exec(ctx,
			fmt.Sprintf("CREATE DATABASE %s", integration.dbName))
		if err != nil {
			integration.err = fmt.Errorf(
				"create test db: %w", err)
			return
		}

		testConnStr := replaceProbeDatabase(base, integration.dbName)
		pool, err := pgxpool.New(ctx, testConnStr)
		if err != nil {
			integration.err = fmt.Errorf(
				"connect to test db: %w", err)
			return
		}

		if err := applyMetricsSchema(ctx, pool); err != nil {
			pool.Close()
			integration.err = fmt.Errorf(
				"apply metrics schema: %w", err)
			return
		}

		integration.pool = pool
	})

	if integrationDisabled {
		t.Skip("TEST_AI_WORKBENCH_SERVER (or TEST_DB_CONN) not set; " +
			"skipping integration test")
	}
	if integration.err != nil {
		t.Skipf("Integration setup failed: %v", integration.err)
	}
	return integration.pool
}

// acquireConn acquires a connection from the integration pool and
// schedules its release at test end.
func acquireConn(t *testing.T, pool *pgxpool.Pool) *pgxpool.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	t.Cleanup(func() { conn.Release() })
	return conn
}

// applyMetricsSchema creates the minimal `metrics` schema and the
// partitioned parent tables required by every probe Store path. The
// schema deliberately mirrors the production DDL only for columns
// the probes write so the tests stay self-contained and quick. The
// `collected_at` partition key is required so EnsurePartition can
// attach weekly child partitions.
func applyMetricsSchema(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	stmts := []string{
		`CREATE SCHEMA IF NOT EXISTS metrics`,

		// Server-scoped probe tables.
		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_activity (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			datid OID, datname TEXT, pid INTEGER, leader_pid INTEGER,
			usesysid OID, usename TEXT, application_name TEXT,
			client_addr INET, client_hostname TEXT, client_port INTEGER,
			backend_start TIMESTAMPTZ, xact_start TIMESTAMPTZ,
			query_start TIMESTAMPTZ, state_change TIMESTAMPTZ,
			wait_event_type TEXT, wait_event TEXT, state TEXT,
			backend_xid TEXT, backend_xmin TEXT, query TEXT,
			backend_type TEXT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_database (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			datname TEXT, datdba OID, encoding INTEGER,
			datlocprovider "char", datistemplate BOOLEAN,
			datallowconn BOOLEAN, datconnlimit INTEGER,
			datfrozenxid XID, datminmxid XID, dattablespace OID,
			age_datfrozenxid BIGINT, age_datminmxid BIGINT,
			database_size_bytes BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_replication_slots (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			slot_name TEXT, slot_type TEXT, active BOOLEAN,
			wal_status TEXT, safe_wal_size BIGINT, retained_bytes BIGINT,
			spill_txns BIGINT, spill_count BIGINT, spill_bytes BIGINT,
			stream_txns BIGINT, stream_count BIGINT, stream_bytes BIGINT,
			total_txns BIGINT, total_count BIGINT, total_bytes BIGINT,
			stats_reset TIMESTAMPTZ
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_recovery_prefetch (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			stats_reset TIMESTAMPTZ, prefetch BIGINT, hit BIGINT,
			skip_init BIGINT, skip_new BIGINT, skip_fpw BIGINT,
			skip_rep BIGINT, wal_distance BIGINT, block_distance BIGINT,
			io_depth BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_connection_security (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			pid INTEGER, ssl BOOLEAN, ssl_version TEXT, cipher TEXT,
			bits INTEGER, client_dn TEXT, client_serial TEXT,
			issuer_dn TEXT, gss_authenticated BOOLEAN, principal TEXT,
			gss_encrypted BOOLEAN, credentials_delegated BOOLEAN
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_io (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			backend_type TEXT, object TEXT, context TEXT,
			reads BIGINT, read_time DOUBLE PRECISION, writes BIGINT,
			write_time DOUBLE PRECISION, writebacks BIGINT,
			writeback_time DOUBLE PRECISION, extends BIGINT,
			extend_time DOUBLE PRECISION, op_bytes BIGINT, hits BIGINT,
			evictions BIGINT, reuses BIGINT, fsyncs BIGINT,
			fsync_time DOUBLE PRECISION, stats_reset TIMESTAMPTZ,
			blks_zeroed BIGINT, blks_exists BIGINT, flushes BIGINT,
			truncates BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_checkpointer (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			num_timed BIGINT, num_requested BIGINT,
			restartpoints_timed BIGINT, restartpoints_req BIGINT,
			restartpoints_done BIGINT, write_time DOUBLE PRECISION,
			sync_time DOUBLE PRECISION, buffers_written BIGINT,
			stats_reset TIMESTAMPTZ, buffers_clean BIGINT,
			maxwritten_clean BIGINT, buffers_alloc BIGINT,
			bgwriter_stats_reset TIMESTAMPTZ
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_wal (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			wal_records BIGINT, wal_fpi BIGINT, wal_bytes NUMERIC,
			wal_buffers_full BIGINT, wal_write BIGINT, wal_sync BIGINT,
			wal_write_time DOUBLE PRECISION, wal_sync_time DOUBLE PRECISION,
			stats_reset TIMESTAMPTZ, archived_count BIGINT,
			last_archived_wal TEXT, last_archived_time TIMESTAMPTZ,
			failed_count BIGINT, last_failed_wal TEXT,
			last_failed_time TIMESTAMPTZ,
			archiver_stats_reset TIMESTAMPTZ
		) PARTITION BY RANGE (collected_at)`,

		// Database-scoped probe tables.
		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_database (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			database_name TEXT NOT NULL,
			datid OID, datname TEXT, numbackends INTEGER,
			xact_commit BIGINT, xact_rollback BIGINT,
			blks_read BIGINT, blks_hit BIGINT,
			tup_returned BIGINT, tup_fetched BIGINT,
			tup_inserted BIGINT, tup_updated BIGINT, tup_deleted BIGINT,
			conflicts BIGINT, temp_files BIGINT, temp_bytes BIGINT,
			deadlocks BIGINT, checksum_failures BIGINT,
			checksum_last_failure TIMESTAMPTZ,
			blk_read_time DOUBLE PRECISION,
			blk_write_time DOUBLE PRECISION,
			session_time DOUBLE PRECISION,
			active_time DOUBLE PRECISION,
			idle_in_transaction_time DOUBLE PRECISION,
			sessions BIGINT, sessions_abandoned BIGINT,
			sessions_fatal BIGINT, sessions_killed BIGINT,
			stats_reset TIMESTAMPTZ
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_database_conflicts (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			database_name TEXT NOT NULL,
			datid OID, datname TEXT,
			confl_tablespace BIGINT, confl_lock BIGINT,
			confl_snapshot BIGINT, confl_bufferpin BIGINT,
			confl_deadlock BIGINT, confl_active_logicalslot BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_all_tables (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			database_name TEXT NOT NULL,
			schemaname TEXT, relname TEXT,
			seq_scan BIGINT, seq_tup_read BIGINT,
			idx_scan BIGINT, idx_tup_fetch BIGINT,
			n_tup_ins BIGINT, n_tup_upd BIGINT, n_tup_del BIGINT,
			n_tup_hot_upd BIGINT, n_live_tup BIGINT, n_dead_tup BIGINT,
			n_mod_since_analyze BIGINT,
			last_vacuum TIMESTAMPTZ, last_autovacuum TIMESTAMPTZ,
			last_analyze TIMESTAMPTZ, last_autoanalyze TIMESTAMPTZ,
			vacuum_count BIGINT, autovacuum_count BIGINT,
			analyze_count BIGINT, autoanalyze_count BIGINT,
			heap_blks_read BIGINT, heap_blks_hit BIGINT,
			idx_blks_read BIGINT, idx_blks_hit BIGINT,
			toast_blks_read BIGINT, toast_blks_hit BIGINT,
			tidx_blks_read BIGINT, tidx_blks_hit BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_all_indexes (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			database_name TEXT NOT NULL,
			relid OID, indexrelid OID,
			schemaname TEXT, relname TEXT, indexrelname TEXT,
			idx_scan BIGINT, last_idx_scan TIMESTAMPTZ,
			idx_tup_read BIGINT, idx_tup_fetch BIGINT,
			idx_blks_read BIGINT, idx_blks_hit BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_statio_all_sequences (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			database_name TEXT NOT NULL,
			relid OID, schemaname TEXT, relname TEXT,
			blks_read BIGINT, blks_hit BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_user_functions (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			database_name TEXT NOT NULL,
			funcid OID, schemaname TEXT, funcname TEXT,
			calls BIGINT, total_time DOUBLE PRECISION,
			self_time DOUBLE PRECISION
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_statements (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			database_name TEXT NOT NULL,
			userid OID, dbid OID, queryid BIGINT, toplevel BOOLEAN,
			query TEXT, calls BIGINT,
			total_exec_time DOUBLE PRECISION,
			mean_exec_time DOUBLE PRECISION,
			min_exec_time DOUBLE PRECISION,
			max_exec_time DOUBLE PRECISION,
			stddev_exec_time DOUBLE PRECISION,
			rows BIGINT,
			shared_blks_hit BIGINT, shared_blks_read BIGINT,
			shared_blks_dirtied BIGINT, shared_blks_written BIGINT,
			local_blks_hit BIGINT, local_blks_read BIGINT,
			local_blks_dirtied BIGINT, local_blks_written BIGINT,
			temp_blks_read BIGINT, temp_blks_written BIGINT,
			shared_blk_read_time DOUBLE PRECISION,
			shared_blk_write_time DOUBLE PRECISION,
			local_blk_read_time DOUBLE PRECISION,
			local_blk_write_time DOUBLE PRECISION
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_extension (
			connection_id INTEGER NOT NULL,
			database_name TEXT NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			extname TEXT, extversion TEXT,
			extrelocatable BOOLEAN, schema_name TEXT
		) PARTITION BY RANGE (collected_at)`,

		// system_stats probe tables (parent only; children created by
		// EnsurePartition during the test run).
		`CREATE TABLE IF NOT EXISTS metrics.pg_sys_os_info (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			name TEXT, version TEXT, host_name TEXT, domain_name TEXT,
			handle_count BIGINT, process_count BIGINT, thread_count BIGINT,
			architecture TEXT, last_bootup_time TIMESTAMPTZ,
			os_up_since_seconds BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_sys_cpu_info (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			vendor TEXT, description TEXT, model_name TEXT,
			processor_type TEXT, logical_processor INTEGER,
			physical_processor INTEGER, no_of_cores INTEGER,
			architecture TEXT, clock_speed_hz BIGINT, cpu_type TEXT,
			cpu_family TEXT, byte_order TEXT,
			l1dcache_size BIGINT, l1icache_size BIGINT,
			l2cache_size BIGINT, l3cache_size BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_sys_cpu_usage_info (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			usermode_normal_process_percent DOUBLE PRECISION,
			usermode_niced_process_percent DOUBLE PRECISION,
			kernelmode_process_percent DOUBLE PRECISION,
			idle_mode_percent DOUBLE PRECISION,
			io_completion_percent DOUBLE PRECISION,
			servicing_irq_percent DOUBLE PRECISION,
			servicing_softirq_percent DOUBLE PRECISION,
			user_time_percent DOUBLE PRECISION,
			processor_time_percent DOUBLE PRECISION,
			privileged_time_percent DOUBLE PRECISION,
			interrupt_time_percent DOUBLE PRECISION
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_sys_memory_info (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			total_memory BIGINT, used_memory BIGINT, free_memory BIGINT,
			swap_total BIGINT, swap_used BIGINT, swap_free BIGINT,
			cache_total BIGINT, kernel_total BIGINT, kernel_paged BIGINT,
			kernel_non_paged BIGINT, total_page_file BIGINT,
			avail_page_file BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_sys_disk_info (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			mount_point TEXT, file_system TEXT, drive_letter TEXT,
			drive_type TEXT, file_system_type TEXT,
			total_space BIGINT, used_space BIGINT, free_space BIGINT,
			total_inodes BIGINT, used_inodes BIGINT, free_inodes BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_sys_io_analysis_info (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			device_name TEXT, total_reads BIGINT, total_writes BIGINT,
			read_bytes BIGINT, write_bytes BIGINT,
			read_time_ms BIGINT, write_time_ms BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_sys_load_avg_info (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			load_avg_one_minute DOUBLE PRECISION,
			load_avg_five_minutes DOUBLE PRECISION,
			load_avg_ten_minutes DOUBLE PRECISION,
			load_avg_fifteen_minutes DOUBLE PRECISION
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_sys_process_info (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			total_processes BIGINT, running_processes BIGINT,
			sleeping_processes BIGINT, stopped_processes BIGINT,
			zombie_processes BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_sys_network_info (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			interface_name TEXT, ip_address TEXT,
			tx_bytes BIGINT, tx_packets BIGINT,
			tx_errors BIGINT, tx_dropped BIGINT,
			rx_bytes BIGINT, rx_packets BIGINT,
			rx_errors BIGINT, rx_dropped BIGINT,
			link_speed_mbps BIGINT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_sys_cpu_memory_by_process (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			pid INTEGER, name TEXT, running_since_seconds BIGINT,
			cpu_usage DOUBLE PRECISION, memory_usage DOUBLE PRECISION,
			memory_bytes BIGINT
		) PARTITION BY RANGE (collected_at)`,

		// Tables for probes that already had structural tests but
		// whose Execute/Store paths were not previously exercised.
		`CREATE TABLE IF NOT EXISTS metrics.pg_connectivity (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			response_time_ms DOUBLE PRECISION
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_settings (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			name TEXT, setting TEXT, unit TEXT, category TEXT,
			short_desc TEXT, extra_desc TEXT, context TEXT,
			vartype TEXT, source TEXT, min_val TEXT, max_val TEXT,
			enumvals TEXT[], boot_val TEXT, reset_val TEXT,
			sourcefile TEXT, sourceline INTEGER, pending_restart BOOLEAN
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_hba_file_rules (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			rule_number INTEGER, file_name TEXT, line_number INTEGER,
			type TEXT, database TEXT[], user_name TEXT[], address TEXT,
			netmask TEXT, auth_method TEXT, options TEXT[], error TEXT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_ident_file_mappings (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			map_number INTEGER, file_name TEXT, line_number INTEGER,
			map_name TEXT, sys_name TEXT, pg_username TEXT, error TEXT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_server_info (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			server_version TEXT, server_version_num INTEGER,
			system_identifier BIGINT, cluster_name TEXT,
			data_directory TEXT, max_connections INTEGER,
			max_wal_senders INTEGER, max_replication_slots INTEGER,
			installed_extensions TEXT[]
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_node_role (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			is_in_recovery BOOLEAN, timeline_id INTEGER,
			postmaster_start_time TIMESTAMPTZ,
			has_binary_standbys BOOLEAN, binary_standby_count INTEGER,
			is_streaming_standby BOOLEAN,
			upstream_host TEXT, upstream_port INTEGER,
			received_lsn TEXT, replayed_lsn TEXT,
			publication_count INTEGER, subscription_count INTEGER,
			active_subscription_count INTEGER,
			has_active_logical_slots BOOLEAN,
			active_logical_slot_count INTEGER,
			publisher_host TEXT, publisher_port INTEGER,
			has_spock BOOLEAN, spock_node_id BIGINT,
			spock_node_name TEXT, spock_subscription_count INTEGER,
			primary_role TEXT, role_flags TEXT[], role_details JSONB
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_replication (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			role TEXT, pid INTEGER, usesysid OID, usename TEXT,
			application_name TEXT, client_addr INET,
			client_hostname TEXT, client_port INTEGER,
			backend_start TIMESTAMPTZ, backend_xmin TEXT,
			state TEXT,
			sent_lsn TEXT, write_lsn TEXT, flush_lsn TEXT, replay_lsn TEXT,
			write_lag INTERVAL, flush_lag INTERVAL, replay_lag INTERVAL,
			sync_priority INTEGER, sync_state TEXT,
			reply_time TIMESTAMPTZ,
			receiver_pid INTEGER, receiver_status TEXT,
			receive_start_lsn TEXT, receive_start_tli INTEGER,
			written_lsn TEXT, receiver_flushed_lsn TEXT,
			received_tli INTEGER,
			last_msg_send_time TIMESTAMPTZ,
			last_msg_receipt_time TIMESTAMPTZ,
			latest_end_lsn TEXT, latest_end_time TIMESTAMPTZ,
			receiver_slot_name TEXT, sender_host TEXT,
			sender_port INTEGER, conninfo TEXT
		) PARTITION BY RANGE (collected_at)`,

		`CREATE TABLE IF NOT EXISTS metrics.pg_stat_subscription (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			subid OID, subname TEXT, worker_type TEXT, pid INTEGER,
			leader_pid INTEGER, relid OID, received_lsn TEXT,
			last_msg_send_time TIMESTAMPTZ,
			last_msg_receipt_time TIMESTAMPTZ,
			latest_end_lsn TEXT, latest_end_time TIMESTAMPTZ,
			apply_error_count BIGINT, sync_error_count BIGINT,
			stats_reset TIMESTAMPTZ
		) PARTITION BY RANGE (collected_at)`,

		// Spock probe target tables. Mirrors the v3 migration column
		// shape so the probe Store paths can write rows without
		// schema drift in unit/integration tests. database_name is
		// NOT NULL here just like in the v3 migration: the probe's
		// Store path always supplies a value sourced from the
		// scheduler-injected "_database_name" key.
		`CREATE TABLE IF NOT EXISTS metrics.spock_exception_log (
			connection_id INTEGER NOT NULL,
			database_name TEXT NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			remote_origin OID,
			remote_commit_ts TIMESTAMPTZ,
			command_counter INTEGER,
			retry_errored_at TIMESTAMPTZ,
			remote_xid BIGINT,
			local_origin OID,
			local_commit_ts TIMESTAMPTZ,
			table_schema TEXT,
			table_name TEXT,
			operation TEXT,
			local_tup JSONB,
			remote_old_tup JSONB,
			remote_new_tup JSONB,
			ddl_statement TEXT,
			ddl_user TEXT,
			error_message TEXT
		) PARTITION BY RANGE (collected_at)`,

		// metrics.spock_resolutions mirrors the v3 migration column
		// list, including the TEXT representation of xid and pg_lsn
		// values that the probe casts on the source side. The probe's
		// Store path appends to this parent table; child partitions
		// are created on demand by EnsurePartition during the test.
		`CREATE TABLE IF NOT EXISTS metrics.spock_resolutions (
			connection_id INTEGER NOT NULL,
			database_name TEXT NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			id INTEGER NOT NULL,
			node_name NAME NOT NULL,
			log_time TIMESTAMPTZ NOT NULL,
			relname TEXT,
			idxname TEXT,
			conflict_type TEXT,
			conflict_resolution TEXT,
			local_origin INTEGER,
			local_tuple TEXT,
			local_xid TEXT,
			local_timestamp TIMESTAMPTZ,
			remote_origin INTEGER,
			remote_tuple TEXT,
			remote_xid TEXT,
			remote_timestamp TIMESTAMPTZ,
			remote_lsn TEXT
		) PARTITION BY RANGE (collected_at)`,

		// pg_stat_statements is a real extension and is enabled in
		// the CI environment via shared_preload_libraries. We try
		// to install it; if the server was not started with the
		// preload library, CREATE EXTENSION succeeds but later
		// queries against the view will fail. To stay portable we
		// allow the create to fail silently.
		`DO $$
		BEGIN
			BEGIN
				CREATE EXTENSION IF NOT EXISTS
					pg_stat_statements;
			EXCEPTION WHEN OTHERS THEN
				NULL;
			END;
		END$$`,

		// Mock the system_stats extension by creating SQL function
		// stubs that match the signatures the probes expect, then
		// register them in pg_extension so the existence check
		// passes. This drives the extension-installed branch in
		// every pg_sys_* probe without requiring a real install.
		// The dummy extension is harmless for the rest of the test
		// run and is dropped along with the test database.
		`CREATE OR REPLACE FUNCTION pg_sys_os_info() RETURNS TABLE(
			name TEXT, version TEXT, host_name TEXT,
			domain_name TEXT, handle_count BIGINT,
			process_count BIGINT, thread_count BIGINT,
			architecture TEXT, last_bootup_time TIMESTAMPTZ,
			os_up_since_seconds BIGINT
		) LANGUAGE SQL AS $$
		SELECT 'Linux'::TEXT, '1'::TEXT, 'host'::TEXT,
			NULL::TEXT, 1::BIGINT, 1::BIGINT, 1::BIGINT,
			'x86_64'::TEXT, NOW(), 60::BIGINT
		$$`,
		`CREATE OR REPLACE FUNCTION pg_sys_cpu_info() RETURNS TABLE(
			vendor TEXT, description TEXT, model_name TEXT,
			processor_type TEXT, logical_processor INTEGER,
			physical_processor INTEGER, no_of_cores INTEGER,
			architecture TEXT, clock_speed_hz BIGINT,
			cpu_type TEXT, cpu_family TEXT, byte_order TEXT,
			l1dcache_size BIGINT, l1icache_size BIGINT,
			l2cache_size BIGINT, l3cache_size BIGINT
		) LANGUAGE SQL AS $$
		SELECT 'Intel'::TEXT, 'CPU'::TEXT, 'i7'::TEXT,
			'x86'::TEXT, 8, 4, 4, 'x86_64'::TEXT,
			1000000::BIGINT, 'type'::TEXT, 'fam'::TEXT,
			'le'::TEXT, 1::BIGINT, 1::BIGINT, 1::BIGINT,
			1::BIGINT
		$$`,
		`CREATE OR REPLACE FUNCTION pg_sys_cpu_usage_info()
			RETURNS TABLE(
			usermode_normal_process_percent FLOAT8,
			usermode_niced_process_percent FLOAT8,
			kernelmode_process_percent FLOAT8,
			idle_mode_percent FLOAT8,
			io_completion_percent FLOAT8,
			servicing_irq_percent FLOAT8,
			servicing_softirq_percent FLOAT8,
			user_time_percent FLOAT8,
			processor_time_percent FLOAT8,
			privileged_time_percent FLOAT8,
			interrupt_time_percent FLOAT8
		) LANGUAGE SQL AS $$
		SELECT 1.0::FLOAT8, 0.0::FLOAT8, 1.0::FLOAT8,
			98.0::FLOAT8, 0.0::FLOAT8, 0.0::FLOAT8,
			0.0::FLOAT8, 1.0::FLOAT8, 1.0::FLOAT8,
			1.0::FLOAT8, 0.0::FLOAT8
		$$`,
		`CREATE OR REPLACE FUNCTION pg_sys_memory_info() RETURNS TABLE(
			total_memory BIGINT, used_memory BIGINT,
			free_memory BIGINT, swap_total BIGINT,
			swap_used BIGINT, swap_free BIGINT,
			cache_total BIGINT, kernel_total BIGINT,
			kernel_paged BIGINT, kernel_non_paged BIGINT,
			total_page_file BIGINT, avail_page_file BIGINT
		) LANGUAGE SQL AS $$
		SELECT 1::BIGINT, 1::BIGINT, 0::BIGINT, 0::BIGINT,
			0::BIGINT, 0::BIGINT, 0::BIGINT, 0::BIGINT,
			0::BIGINT, 0::BIGINT, 0::BIGINT, 0::BIGINT
		$$`,
		`CREATE OR REPLACE FUNCTION pg_sys_io_analysis_info()
			RETURNS TABLE(
			device_name TEXT, total_reads BIGINT,
			total_writes BIGINT, read_bytes BIGINT,
			write_bytes BIGINT, read_time_ms BIGINT,
			write_time_ms BIGINT
		) LANGUAGE SQL AS $$
		SELECT 'sda'::TEXT, 1::BIGINT, 1::BIGINT, 1::BIGINT,
			1::BIGINT, 1::BIGINT, 1::BIGINT
		$$`,
		`CREATE OR REPLACE FUNCTION pg_sys_disk_info() RETURNS TABLE(
			mount_point TEXT, file_system TEXT,
			drive_letter TEXT, drive_type TEXT,
			file_system_type TEXT, total_space BIGINT,
			used_space BIGINT, free_space BIGINT,
			total_inodes BIGINT, used_inodes BIGINT,
			free_inodes BIGINT
		) LANGUAGE SQL AS $$
		SELECT '/'::TEXT, 'ext4'::TEXT, ''::TEXT,
			'ssd'::TEXT, 'ext4'::TEXT, 1::BIGINT,
			1::BIGINT, 0::BIGINT, 1::BIGINT, 1::BIGINT,
			0::BIGINT
		$$`,
		`CREATE OR REPLACE FUNCTION pg_sys_load_avg_info()
			RETURNS TABLE(
			load_avg_one_minute FLOAT8,
			load_avg_five_minutes FLOAT8,
			load_avg_ten_minutes FLOAT8,
			load_avg_fifteen_minutes FLOAT8
		) LANGUAGE SQL AS $$
		SELECT 0.1::FLOAT8, 0.1::FLOAT8, 0.1::FLOAT8,
			0.1::FLOAT8
		$$`,
		`CREATE OR REPLACE FUNCTION pg_sys_process_info()
			RETURNS TABLE(
			total_processes BIGINT, running_processes BIGINT,
			sleeping_processes BIGINT,
			stopped_processes BIGINT,
			zombie_processes BIGINT
		) LANGUAGE SQL AS $$
		SELECT 1::BIGINT, 1::BIGINT, 0::BIGINT, 0::BIGINT,
			0::BIGINT
		$$`,
		`CREATE OR REPLACE FUNCTION pg_sys_network_info()
			RETURNS TABLE(
			interface_name TEXT, ip_address TEXT,
			tx_bytes BIGINT, tx_packets BIGINT,
			tx_errors BIGINT, tx_dropped BIGINT,
			rx_bytes BIGINT, rx_packets BIGINT,
			rx_errors BIGINT, rx_dropped BIGINT,
			link_speed_mbps BIGINT
		) LANGUAGE SQL AS $$
		SELECT 'eth0'::TEXT, '127.0.0.1'::TEXT,
			0::BIGINT, 0::BIGINT, 0::BIGINT, 0::BIGINT,
			0::BIGINT, 0::BIGINT, 0::BIGINT, 0::BIGINT,
			1000::BIGINT
		$$`,
		`CREATE OR REPLACE FUNCTION pg_sys_cpu_memory_by_process()
			RETURNS TABLE(
			pid INTEGER, name TEXT,
			running_since_seconds BIGINT,
			cpu_usage FLOAT8, memory_usage FLOAT8,
			memory_bytes BIGINT
		) LANGUAGE SQL AS $$
		SELECT 1, 'init'::TEXT, 60::BIGINT,
			0.0::FLOAT8, 0.0::FLOAT8, 1024::BIGINT
		$$`,
		// Register the dummy extension. We use the next free OID and
		// assign it to the postgres role (BOOTSTRAP_SUPERUSERID = 10)
		// in the public namespace.
		`INSERT INTO pg_extension (oid, extname, extowner,
			extnamespace, extrelocatable, extversion,
			extconfig, extcondition)
		SELECT (SELECT MAX(oid::oid::int) FROM pg_extension) + 1,
			'system_stats', 10,
			(SELECT oid FROM pg_namespace WHERE nspname='public'),
			TRUE, '1.0', NULL, NULL
		WHERE NOT EXISTS (
			SELECT 1 FROM pg_extension
			WHERE extname='system_stats')`,
	}

	for _, stmt := range stmts {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("exec %q: %w", firstLine(stmt), err)
		}
	}
	return nil
}

// firstLine returns the first non-empty line of s for diagnostic
// messages.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t != "" {
			return t
		}
	}
	return s
}

// detectPgVersion returns the PostgreSQL major version reported by the
// connected server (e.g. 16). Tests use the version to gate version-
// specific assertions and to pass the right value to probe Execute.
func detectPgVersion(t *testing.T, conn *pgxpool.Conn) int {
	t.Helper()
	var num int
	err := conn.QueryRow(context.Background(),
		"SELECT current_setting('server_version_num')::int").Scan(&num)
	if err != nil {
		t.Fatalf("server_version_num: %v", err)
	}
	return num / 10000
}
