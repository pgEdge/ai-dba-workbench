# Probe Reference

Complete reference for all built-in probes in the Collector.

## Probe Categories

- Server-Scoped: Collect server-wide statistics (26 probes)
- Database-Scoped: Collect per-database statistics (8 probes)

## Server-Scoped Probes

These probes execute once per monitored connection.

### pg_stat_activity

Monitors current database activity and backend processes.

- Source View: `pg_stat_activity`
- Default Interval: 60 seconds
- Default Retention: 7 days
- Key Metrics: Active connections, query states, wait events
- Use Cases: Connection monitoring, identifying long-running queries,
  detecting locks

**Columns Collected**: datid, datname, pid, leader_pid, usesysid, usename,
application_name, client_addr, client_hostname, client_port, backend_start,
xact_start, query_start, state_change, wait_event_type, wait_event, state,
backend_xid, backend_xmin, query, backend_type

### pg_stat_checkpointer

Monitors checkpointer process statistics. This probe consolidates data from
the `pg_stat_checkpointer` view and includes background writer statistics
that were previously collected separately from `pg_stat_bgwriter`.

- Source View: `pg_stat_checkpointer` (with bgwriter stats)
- Default Interval: 600 seconds (10 minutes)
- Default Retention: 7 days
- Key Metrics: Checkpoints, buffers written, sync times, background writer
  activity
- Use Cases: Checkpoint performance analysis, I/O tuning, buffer management
- Version Notes: PostgreSQL 15+ uses `pg_stat_checkpointer`; earlier
  versions collect from `pg_stat_bgwriter`

**Columns Collected**: num_timed, num_requested, restartpoints_timed,
restartpoints_req, restartpoints_done, write_time, sync_time, buffers_written,
buffers_clean, maxwritten_clean, buffers_backend, buffers_backend_fsync,
buffers_alloc, stats_reset

### pg_stat_connection_security

Monitors SSL and GSSAPI connection security information. This probe
consolidates data from both `pg_stat_ssl` and `pg_stat_gssapi` views to
provide a unified view of connection security.

- Source Views: `pg_stat_ssl`, `pg_stat_gssapi`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: SSL version, cipher, GSSAPI authentication status,
  principals, encryption
- Use Cases: SSL/TLS security monitoring, Kerberos authentication
  monitoring, connection security auditing

**Columns Collected**: pid, ssl, ssl_version, ssl_cipher, ssl_bits,
ssl_client_dn, ssl_client_serial, ssl_issuer_dn, gss_authenticated,
gss_principal, gss_encrypted, gss_credentials_delegated

### pg_stat_io

Monitors I/O statistics by backend type and context. This probe consolidates
data from `pg_stat_io` and includes SLRU cache statistics that were previously
collected separately from `pg_stat_slru`.

- Source Views: `pg_stat_io`, `pg_stat_slru`
- Default Interval: 900 seconds (15 minutes)
- Default Retention: 7 days
- Key Metrics: Reads, writes, extends, hits by context, SLRU cache
  performance
- Use Cases: Detailed I/O analysis, cache efficiency, SLRU performance

**Columns Collected**: backend_type, context, reads, read_time, writes,
write_time, writebacks, writeback_time, extends, extend_time, op_bytes, hits,
evictions, reuses, fsyncs, fsync_time, slru_name, blks_zeroed, blks_hit,
blks_read, blks_written, blks_exists, flushes, truncates, stats_reset

### pg_stat_recovery_prefetch

Monitors recovery prefetch statistics.

- Source View: `pg_stat_recovery_prefetch`
- Default Interval: 600 seconds (10 minutes)
- Default Retention: 7 days
- Key Metrics: Prefetch operations, hit rate, distance
- Use Cases: Recovery performance tuning

**Columns Collected**: stats_reset, prefetch, hit, skip_init, skip_new,
skip_fpw, skip_rep, wal_distance, block_distance, io_depth

### pg_stat_replication

Monitors replication status and lag. This probe consolidates data from
`pg_stat_replication` and includes WAL receiver statistics that were
previously collected separately from `pg_stat_wal_receiver`.

- Source Views: `pg_stat_replication`, `pg_stat_wal_receiver`
- Default Interval: 30 seconds
- Default Retention: 7 days
- Key Metrics: Replication lag, sync state, sent/received LSN, WAL receiver
  status
- Use Cases: Replication monitoring, lag alerting, replica health

**Columns Collected**: pid, usesysid, usename, application_name, client_addr,
client_hostname, client_port, backend_start, backend_xmin, state, sent_lsn,
write_lsn, flush_lsn, replay_lsn, write_lag, flush_lag, replay_lag,
sync_state, sync_priority, reply_time, receiver_pid, receiver_status,
receive_start_lsn, receive_start_tli, written_lsn, flushed_lsn, received_tli,
slot_name, sender_host, sender_port

### pg_replication_slots

Monitors replication slot WAL retention by computing the difference between
the current WAL position and each slot's restart LSN. This probe consolidates
data from `pg_replication_slots` and includes slot statistics that were
previously collected separately from `pg_stat_replication_slots`.

- Source Views: `pg_replication_slots`, `pg_stat_replication_slots`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Retained WAL bytes per slot, slot activity, WAL status,
  spill statistics
- Use Cases: WAL accumulation monitoring, disk usage prevention, inactive
  slot detection, slot performance analysis
- Version Notes: The `wal_status` and `safe_wal_size` columns require
  PostgreSQL 13 or later; older versions return NULL.

**Columns Collected**: slot_name, slot_type, active, wal_status,
safe_wal_size, retained_bytes, spill_txns, spill_count, spill_bytes,
stream_txns, stream_count, stream_bytes, total_txns, total_bytes, stats_reset

### pg_stat_subscription

Monitors logical replication subscriptions. This probe consolidates data from
`pg_stat_subscription` and includes subscription statistics that were
previously collected separately from `pg_stat_subscription_stats`.

- Source Views: `pg_stat_subscription`, `pg_stat_subscription_stats`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Subscription state, worker info, LSN positions, apply
  errors, sync errors
- Use Cases: Logical replication monitoring, subscription health tracking

**Columns Collected**: subid, subname, pid, leader_pid, relid, received_lsn,
last_msg_send_time, last_msg_receipt_time, latest_end_lsn, latest_end_time,
apply_error_count, sync_error_count, stats_reset

### pg_stat_wal

Monitors WAL generation statistics. This probe consolidates data from
`pg_stat_wal` and includes archiver statistics that were previously collected
separately from `pg_stat_archiver`.

- Source Views: `pg_stat_wal`, `pg_stat_archiver`
- Default Interval: 600 seconds (10 minutes)
- Default Retention: 7 days
- Key Metrics: WAL records, bytes, sync operations, archived WAL count,
  archive failures
- Use Cases: WAL generation analysis, write load monitoring, archive health

**Columns Collected**: wal_records, wal_fpi, wal_bytes, wal_buffers_full,
wal_write, wal_sync, wal_write_time, wal_sync_time, archived_count,
last_archived_wal, last_archived_time, failed_count, last_failed_wal,
last_failed_time, stats_reset

### pg_settings

Monitors PostgreSQL configuration settings with change detection. This probe
only stores data when configuration changes are detected, making it ideal for
tracking configuration drift and historical changes over long periods.

- Source View: `pg_settings`
- Default Interval: 3600 seconds (1 hour)
- Default Retention: 365 days (1 year)
- Key Metrics: PostgreSQL configuration parameters and their sources
- Use Cases: Configuration change tracking, configuration drift detection,
    historical configuration analysis, compliance auditing
- Special Behavior: Uses SHA256 hash comparison to detect changes. Data is
    only stored when configuration differs from the most recent snapshot. The
    garbage collector ensures the most recent snapshot for each server is never
    deleted, regardless of age.

**Columns Collected**: name, setting, unit, category, short_desc, extra_desc,
context, vartype, source, min_val, max_val, enumvals, boot_val, reset_val,
sourcefile, sourceline, pending_restart

**See Also**: [pg_settings Usage Guide](pg-settings-usage.md) for detailed
examples and query patterns.

### pg_hba_file_rules

Monitors PostgreSQL pg_hba.conf authentication configuration with change
detection. This probe only stores data when HBA rules change, making it ideal
for tracking authentication policy changes over time.

- Source View: `pg_hba_file_rules`
- Default Interval: 3600 seconds (1 hour)
- Default Retention: 365 days (1 year)
- Key Metrics: Authentication rules, methods, databases, users, addresses
- Use Cases: Authentication policy tracking, security audit compliance,
    HBA configuration drift detection, forensic analysis
- Special Behavior: Uses SHA256 hash comparison to detect changes. Data is
    only stored when configuration differs from the most recent snapshot. The
    garbage collector ensures the most recent snapshot for each server is never
    deleted, regardless of age.

**Columns Collected**: rule_number, file_name, line_number, type, database,
user_name, address, netmask, auth_method, options, error

### pg_ident_file_mappings

Monitors PostgreSQL pg_ident.conf user mapping configuration with change
detection. This probe only stores data when ident mappings change, enabling
tracking of user mapping changes for audit and compliance purposes.

- Source View: `pg_ident_file_mappings`
- Default Interval: 3600 seconds (1 hour)
- Default Retention: 365 days (1 year)
- Key Metrics: Ident map names, system usernames, PostgreSQL usernames
- Use Cases: User mapping audit tracking, compliance verification, mapping
    drift detection, security policy enforcement
- Special Behavior: Uses SHA256 hash comparison to detect changes. Data is
    only stored when configuration differs from the most recent snapshot. The
    garbage collector ensures the most recent snapshot for each server is never
    deleted, regardless of age.

**Columns Collected**: map_number, file_name, line_number, map_name, sys_name,
pg_username, error

### pg_server_info

Monitors PostgreSQL server identification and configuration with change
detection. This probe only stores data when server configuration changes,
making it ideal for tracking server upgrades and configuration changes.

- Source: Various system functions and pg_extension
- Default Interval: 3600 seconds (1 hour)
- Default Retention: 365 days (1 year)
- Key Metrics: Server version, system identifier, replication settings,
    installed extensions
- Use Cases: Server inventory tracking, upgrade verification, extension
    monitoring, capacity planning
- Special Behavior: Uses SHA256 hash comparison to detect changes. Data is
    only stored when configuration differs from the most recent snapshot. The
    garbage collector ensures the most recent snapshot for each server is never
    deleted, regardless of age.

**Columns Collected**: server_version, server_version_num, system_identifier,
cluster_name, data_directory, max_connections, max_wal_senders,
max_replication_slots, installed_extensions

### pg_node_role

Detects and tracks PostgreSQL node roles within various cluster topologies.
This probe identifies how each node participates in replication configurations
including binary replication, logical replication, and Spock
multi-master.

- Source: Multiple system views and extension catalogs
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 30 days
- Key Metrics: Primary role, role flags, replication status, standby info
- Use Cases: Cluster topology monitoring, failover detection, replication
    health tracking, Spock node status

**Primary Roles Detected**:

- `standalone` - No replication configured
- `binary_primary` - Source for physical replication
- `binary_standby` - Physical replication target
- `binary_cascading` - Standby that is also a primary
- `logical_publisher` - Native logical replication source
- `logical_subscriber` - Native logical replication target
- `logical_bidirectional` - Both publisher and subscriber
- `spock_node` - Active Spock multi-master node
- `spock_standby` - Binary standby of Spock node

**Role Flags**: Non-exclusive capability flags that indicate all replication
capabilities (e.g., a node can be both `binary_primary` and `logical_publisher`
simultaneously).

**Columns Collected**: is_in_recovery, timeline_id, has_binary_standbys,
binary_standby_count, is_streaming_standby, upstream_host, upstream_port,
received_lsn, replayed_lsn, publication_count, subscription_count,
active_subscription_count, has_spock, spock_node_id, spock_node_name,
spock_subscription_count, primary_role, role_flags, role_details

**See Also**: [Node Role Probe Design](node-role-probe-design.md) for detailed
architecture and detection algorithms.

### pg_database

Monitors the `pg_database` catalog including transaction ID
wraparound indicators and database size.

- Source: `pg_database` catalog
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Transaction ID age, multixact age, database
  size
- Use Cases: XID wraparound monitoring, database size
  tracking, capacity planning

**Columns Collected**: datname, datdba, encoding,
datlocprovider, datistemplate, datallowconn, datconnlimit,
datfrozenxid, datminmxid, dattablespace, age_datfrozenxid,
age_datminmxid, database_size_bytes

## Database-Scoped Probes

These probes execute once for each database on a monitored server.

### pg_stat_database

Monitors database-wide statistics.

- Source View: `pg_stat_database`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Transaction counts, buffer hits, tuples, deadlocks
- Use Cases: Database activity monitoring, cache hit ratio analysis

**Columns Collected**: datid, datname, numbackends, xact_commit,
xact_rollback, blks_read, blks_hit, tup_returned, tup_fetched, tup_inserted,
tup_updated, tup_deleted, conflicts, temp_files, temp_bytes, deadlocks,
checksum_failures, checksum_last_failure, blk_read_time, blk_write_time,
session_time, active_time, idle_in_transaction_time, sessions,
sessions_abandoned, sessions_fatal, sessions_killed, stats_reset

### pg_stat_database_conflicts

Monitors recovery conflicts on replicas.

- Source View: `pg_stat_database_conflicts`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Conflict counts by type
- Use Cases: Replica conflict monitoring

**Columns Collected**: datid, datname, confl_tablespace, confl_lock,
confl_snapshot, confl_bufferpin, confl_deadlock

### pg_stat_all_tables

Monitors table access and I/O statistics. This probe consolidates data from
`pg_stat_all_tables` and includes I/O statistics that were previously
collected separately from `pg_statio_all_tables`.

- Source Views: `pg_stat_all_tables`, `pg_statio_all_tables`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Sequential/index scans, tuples read/written, vacuum/analyze,
  heap/index/toast blocks read/hit
- Use Cases: Table usage analysis, vacuum monitoring, I/O analysis, cache
  effectiveness

**Columns Collected**: relid, schemaname, relname, seq_scan, seq_tup_read,
idx_scan, idx_tup_fetch, n_tup_ins, n_tup_upd, n_tup_del, n_tup_hot_upd,
n_live_tup, n_dead_tup, n_mod_since_analyze, n_ins_since_vacuum,
last_vacuum, last_autovacuum, last_analyze, last_autoanalyze, vacuum_count,
autovacuum_count, analyze_count, autoanalyze_count, heap_blks_read,
heap_blks_hit, idx_blks_read, idx_blks_hit, toast_blks_read, toast_blks_hit,
tidx_blks_read, tidx_blks_hit

### pg_stat_all_indexes

Monitors index usage and I/O statistics. This probe consolidates data from
`pg_stat_all_indexes` and includes I/O statistics that were previously
collected separately from `pg_statio_all_indexes`.

- Source Views: `pg_stat_all_indexes`, `pg_statio_all_indexes`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Index scans, tuples read/fetched, index blocks read/hit
- Use Cases: Index usage analysis, unused index detection, I/O analysis

**Columns Collected**: relid, indexrelid, schemaname, relname, indexrelname,
idx_scan, idx_tup_read, idx_tup_fetch, idx_blks_read, idx_blks_hit

### pg_statio_all_sequences

Monitors sequence I/O statistics.

- Source View: `pg_statio_all_sequences`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Sequence blocks read/hit
- Use Cases: Sequence performance analysis

**Columns Collected**: relid, schemaname, relname, blks_read, blks_hit

### pg_stat_user_functions

Monitors user-defined function statistics.

- Source View: `pg_stat_user_functions`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Function calls, total/self time
- Use Cases: Function performance analysis

**Columns Collected**: funcid, schemaname, funcname, calls, total_time,
self_time

### pg_stat_statements

Monitors query performance statistics (requires pg_stat_statements extension).

- Source View: `pg_stat_statements`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Query execution times, calls, rows, I/O, planning
- Use Cases: Query performance analysis, slow query identification
- Query Limit: Top 1000 queries by total execution time

**Important**: This probe collects only the **top 1000 queries** ordered by
total execution time to prevent excessive data collection on busy systems.
Queries with NULL queryid (utility statements like VACUUM, ANALYZE) are
automatically filtered
out. If your database tracks more than 1000 queries, lower-impact queries
will not be collected. This limit is hard-coded in the probe implementation.
Focus on optimizing the highest-impact queries first.

**Columns Collected**: userid, dbid, toplevel, queryid, query, plans,
total_plan_time, min_plan_time, max_plan_time, mean_plan_time,
stddev_plan_time, calls, total_exec_time, min_exec_time, max_exec_time,
mean_exec_time, stddev_exec_time, rows, shared_blks_hit, shared_blks_read,
shared_blks_dirtied, shared_blks_written, local_blks_hit, local_blks_read,
local_blks_dirtied, local_blks_written, temp_blks_read, temp_blks_written,
blk_read_time, blk_write_time, temp_blk_read_time, temp_blk_write_time,
wal_records, wal_fpi, wal_bytes, jit_functions, jit_generation_time,
jit_inlining_count, jit_inlining_time, jit_optimization_count,
jit_optimization_time, jit_emission_count, jit_emission_time

### pg_extension

Monitors installed PostgreSQL extensions with change detection.
This probe only stores data when installed extensions change,
making it ideal for tracking extension installations and
upgrades.

- Source: `pg_extension` catalog joined with `pg_namespace`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Extension names, versions, schemas
- Use Cases: Extension inventory tracking, upgrade
  verification, compliance auditing
- Special Behavior: Uses SHA256 hash comparison to detect
  changes. Data is only stored when extensions differ from the
  most recent snapshot. The garbage collector ensures the most
  recent snapshot for each server is never deleted, regardless
  of age.

**Columns Collected**: extname, extversion, extrelocatable,
schema_name

## System Statistics Probes

These probes collect operating system metrics through the
`system_stats` PostgreSQL extension. They require the extension
to be installed on the monitored server.

### pg_sys_os_info

Monitors operating system identification information.

- Source: `pg_sys_os_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: OS name, version, architecture
- Use Cases: Server inventory, OS upgrade tracking

### pg_sys_cpu_info

Monitors CPU hardware information.

- Source: `pg_sys_cpu_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: CPU model, cores, speed
- Use Cases: Hardware inventory, capacity planning

### pg_sys_cpu_usage_info

Monitors real-time CPU utilization.

- Source: `pg_sys_cpu_usage_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: CPU utilization percentages (user, system,
  idle)
- Use Cases: CPU load monitoring, alerting on high usage

### pg_sys_memory_info

Monitors system memory usage.

- Source: `pg_sys_memory_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Total, used, free, cached memory
- Use Cases: Memory utilization monitoring, capacity planning

### pg_sys_io_analysis_info

Monitors disk I/O statistics.

- Source: `pg_sys_io_analysis_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Read/write operations, throughput, latency
- Use Cases: I/O performance analysis, bottleneck detection

### pg_sys_disk_info

Monitors disk space usage.

- Source: `pg_sys_disk_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Disk capacity, used space, usage percentage
- Use Cases: Disk space monitoring, capacity planning,
  alerting

### pg_sys_load_avg_info

Monitors system load averages.

- Source: `pg_sys_load_avg_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: 1-minute, 5-minute, 15-minute load averages
- Use Cases: System load monitoring, trend analysis

### pg_sys_process_info

Monitors system process statistics.

- Source: `pg_sys_process_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Process counts, states
- Use Cases: Process monitoring, resource usage tracking

### pg_sys_network_info

Monitors network interface statistics.

- Source: `pg_sys_network_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Network throughput, packets, errors
- Use Cases: Network performance monitoring, error detection

### pg_sys_cpu_memory_by_process

Monitors per-process CPU and memory usage.

- Source: `pg_sys_cpu_memory_by_process()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Per-process CPU percentage, memory usage
- Use Cases: Process-level resource analysis, identifying
  resource-intensive processes

## Configuring Probes

All probes are configured in the `probes` table:

```sql
SELECT name, collection_interval_seconds, retention_days, is_enabled
FROM probes
ORDER BY name;
```

### Adjusting Collection Interval

```sql
UPDATE probes
SET collection_interval_seconds = 60
WHERE name = 'pg_stat_activity';
```

### Adjusting Retention

```sql
UPDATE probes
SET retention_days = 30
WHERE name = 'pg_stat_statements';
```

### Disabling a Probe

```sql
UPDATE probes
SET is_enabled = FALSE
WHERE name = 'pg_stat_io';
```

Changes require collector restart.

## System Limits and Constants

The Collector enforces several limits to ensure stable operation and prevent
resource exhaustion. These limits are hard-coded in the implementation.

### Query Collection Limits

**pg_stat_statements Query Limit: 1000**

- Defined in: `src/probes/constants.go`
- Constant: `PgStatStatementsQueryLimit = 1000`
- Applies to: pg_stat_statements probe only
- Behavior: Collects top 1000 queries ordered by total execution time
- Rationale: Prevents excessive data collection on busy systems with thousands
  of tracked queries
- Override: Not configurable; requires code modification

### Connection Pool Limits

**Datastore Pool Max Connections: 25**

- Defined in: `src/constants.go`
- Constant: `DefaultPoolMaxConnections = 25`
- Applies to: Connection pool for the datastore database
- Behavior: Maximum concurrent connections to the datastore
- Configurable via: `pool_max_connections` in configuration file

**Monitored Pool Max Connections: 5 per server**

- Defined in: `src/constants.go`
- Constant: `DefaultMonitoredPoolMaxConnections = 5`
- Applies to: Connection pool for each monitored database server
- Behavior: Maximum concurrent connections per monitored server
- Rationale: Prevents overwhelming monitored servers with connections
- Configurable via: `monitored_pool_max_connections` in configuration file

**Idle Connection Timeout: 300 seconds (5 minutes)**

- Defined in: `src/constants.go`
- Constant: `DefaultPoolIdleSeconds = 300`
- Applies to: Both datastore and monitored connection pools
- Behavior: Idle connections are closed after 5 minutes
- Configurable via: `pool_max_idle_seconds` in configuration file

### Operation Timeouts

**Connection Timeout: 10 seconds**

- Defined in: `src/constants.go`
- Constant: `ConnectionTimeout = 10 * time.Second`
- Applies to: Initial database connection establishment
- Behavior: Connection attempt fails if not established within 10 seconds

**Context Timeout: 30 seconds**

- Defined in: `src/constants.go`
- Constant: `ContextTimeout = 30 * time.Second`
- Applies to: General context operations
- Behavior: Operations time out after 30 seconds

**Probe Execution Timeout: 60 seconds**

- Defined in: `src/constants.go`
- Constant: `ProbeExecutionTimeout = 60 * time.Second`
- Applies to: Individual probe query execution
- Behavior: Probe execution fails if query doesn't complete within 60 seconds
- Note: Includes time to acquire connection + query execution + result
  processing

**Datastore Wait Timeout: 5 seconds**

- Defined in: `src/constants.go`
- Constant: `DatastoreWaitTimeout = 5 * time.Second`
- Applies to: Waiting for datastore connection from pool
- Behavior: Returns error if datastore connection not available within 5
  seconds
- Configurable via: `datastore_pool_max_wait_seconds` in configuration file

**Monitored Pool Wait Timeout: 120 seconds (2 minutes)**

- Defined in: Default configuration
- Configurable via: `monitored_pool_max_wait_seconds` in configuration file
- Applies to: Waiting for monitored connection from pool
- Behavior: Returns error if monitored connection not available within timeout
- Rationale: Allows more time for connections to busy monitored servers

### Configuration Reload

**Probe Configuration Reload Interval: 5 minutes**

- Behavior: Probe configurations automatically reload from database every 5
  minutes
- Effect: Changes to `collection_interval_seconds`, `retention_days`, or
  `is_enabled` take effect within 5 minutes without restart

### Modifying Limits

Most limits are configured through the configuration file. To modify hard-coded
limits:

1. Edit the constant in the source file
2. Rebuild the collector: `make build`
3. Restart the collector with the new binary

**Important**: Hard-coded limits exist for stability and performance reasons.
Increasing them may impact system resources or data quality. Test thoroughly in
a non-production environment before modifying.

## See Also

- [Probes System](probes.md) - How probes work
- [Adding Probes](adding-probes.md) - Create custom probes
- [Configuration](configuration.md) - Configure collection intervals
- [pg_settings Usage Guide](pg-settings-usage.md) - Examples and best
    practices for using pg_settings probe data
