# Probe Reference

This document provides a complete reference for all
built-in probes in the Collector.

## Probe Categories

The Collector organizes probes into two categories:

- Server-scoped probes collect server-wide statistics
  and total 26 probes.
- Database-scoped probes collect per-database
  statistics and total 8 probes.

## Server-Scoped Probes

Server-scoped probes execute once per monitored
connection.

### pg_stat_activity

This probe monitors current database activity and
backend processes.

- Source View: `pg_stat_activity`
- Default Interval: 60 seconds
- Default Retention: 7 days
- Key Metrics: Active connections, query states,
  wait events
- Use Cases: Connection monitoring, identifying
  long-running queries, detecting locks

**Columns Collected**: datid, datname, pid,
leader_pid, usesysid, usename, application_name,
client_addr, client_hostname, client_port,
backend_start, xact_start, query_start,
state_change, wait_event_type, wait_event, state,
backend_xid, backend_xmin, query, backend_type

### pg_stat_checkpointer

This probe monitors checkpointer process statistics.
The probe consolidates data from the
`pg_stat_checkpointer` view and includes background
writer statistics that were previously collected
separately from `pg_stat_bgwriter`.

- Source View: `pg_stat_checkpointer` (with bgwriter
  stats)
- Default Interval: 600 seconds (10 minutes)
- Default Retention: 7 days
- Key Metrics: Checkpoints, buffers written, sync
  times, background writer activity
- Use Cases: Checkpoint performance analysis, I/O
  tuning, buffer management
- Version Notes: PostgreSQL 15+ uses
  `pg_stat_checkpointer`; earlier versions collect
  from `pg_stat_bgwriter`

**Columns Collected**: num_timed, num_requested,
restartpoints_timed, restartpoints_req,
restartpoints_done, write_time, sync_time,
buffers_written, buffers_clean, maxwritten_clean,
buffers_backend, buffers_backend_fsync,
buffers_alloc, stats_reset

### pg_stat_connection_security

This probe monitors SSL and GSSAPI connection
security information. The probe consolidates data
from both `pg_stat_ssl` and `pg_stat_gssapi` views.

- Source Views: `pg_stat_ssl`, `pg_stat_gssapi`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: SSL version, cipher, GSSAPI
  authentication status, principals, encryption
- Use Cases: SSL/TLS security monitoring, Kerberos
  authentication monitoring, connection security
  auditing

**Columns Collected**: pid, ssl, ssl_version,
ssl_cipher, ssl_bits, ssl_client_dn,
ssl_client_serial, ssl_issuer_dn, gss_authenticated,
gss_principal, gss_encrypted,
gss_credentials_delegated

### pg_stat_io

This probe monitors I/O statistics by backend type
and context. The probe consolidates data from
`pg_stat_io` and includes SLRU cache statistics from
`pg_stat_slru`.

- Source Views: `pg_stat_io`, `pg_stat_slru`
- Default Interval: 900 seconds (15 minutes)
- Default Retention: 7 days
- Key Metrics: Reads, writes, extends, hits by
  context, SLRU cache performance
- Use Cases: Detailed I/O analysis, cache efficiency,
  SLRU performance

**Columns Collected**: backend_type, context, reads,
read_time, writes, write_time, writebacks,
writeback_time, extends, extend_time, op_bytes, hits,
evictions, reuses, fsyncs, fsync_time, slru_name,
blks_zeroed, blks_hit, blks_read, blks_written,
blks_exists, flushes, truncates, stats_reset

### pg_stat_recovery_prefetch

This probe monitors recovery prefetch statistics.

- Source View: `pg_stat_recovery_prefetch`
- Default Interval: 600 seconds (10 minutes)
- Default Retention: 7 days
- Key Metrics: Prefetch operations, hit rate,
  distance
- Use Cases: Recovery performance tuning

**Columns Collected**: stats_reset, prefetch, hit,
skip_init, skip_new, skip_fpw, skip_rep,
wal_distance, block_distance, io_depth

### pg_stat_replication

This probe monitors replication status and lag. The
probe consolidates data from `pg_stat_replication`
and includes WAL receiver statistics from
`pg_stat_wal_receiver`.

- Source Views: `pg_stat_replication`,
  `pg_stat_wal_receiver`
- Default Interval: 30 seconds
- Default Retention: 7 days
- Key Metrics: Replication lag, sync state,
  sent/received LSN, WAL receiver status
- Use Cases: Replication monitoring, lag alerting,
  replica health

**Columns Collected**: pid, usesysid, usename,
application_name, client_addr, client_hostname,
client_port, backend_start, backend_xmin, state,
sent_lsn, write_lsn, flush_lsn, replay_lsn,
write_lag, flush_lag, replay_lag, sync_state,
sync_priority, reply_time, receiver_pid,
receiver_status, receive_start_lsn,
receive_start_tli, written_lsn, flushed_lsn,
received_tli, slot_name, sender_host, sender_port

### pg_replication_slots

This probe monitors replication slot WAL retention
by computing the difference between the current WAL
position and each slot's restart LSN. The probe
consolidates data from `pg_replication_slots` and
includes slot statistics from
`pg_stat_replication_slots`.

- Source Views: `pg_replication_slots`,
  `pg_stat_replication_slots`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Retained WAL bytes per slot, slot
  activity, WAL status, spill statistics
- Use Cases: WAL accumulation monitoring, disk usage
  prevention, inactive slot detection, slot
  performance analysis
- Version Notes: The `wal_status` and `safe_wal_size`
  columns require PostgreSQL 13 or later.

**Columns Collected**: slot_name, slot_type, active,
wal_status, safe_wal_size, retained_bytes,
spill_txns, spill_count, spill_bytes, stream_txns,
stream_count, stream_bytes, total_txns, total_bytes,
stats_reset

### pg_stat_subscription

This probe monitors logical replication
subscriptions. The probe consolidates data from
`pg_stat_subscription` and includes subscription
statistics from `pg_stat_subscription_stats`.

- Source Views: `pg_stat_subscription`,
  `pg_stat_subscription_stats`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Subscription state, worker info,
  LSN positions, apply errors, sync errors
- Use Cases: Logical replication monitoring,
  subscription health tracking

**Columns Collected**: subid, subname, pid,
leader_pid, relid, received_lsn,
last_msg_send_time, last_msg_receipt_time,
latest_end_lsn, latest_end_time,
apply_error_count, sync_error_count, stats_reset

### pg_stat_wal

This probe monitors WAL generation statistics. The
probe consolidates data from `pg_stat_wal` and
includes archiver statistics from
`pg_stat_archiver`.

- Source Views: `pg_stat_wal`, `pg_stat_archiver`
- Default Interval: 600 seconds (10 minutes)
- Default Retention: 7 days
- Key Metrics: WAL records, bytes, sync operations,
  archived WAL count, archive failures
- Use Cases: WAL generation analysis, write load
  monitoring, archive health

**Columns Collected**: wal_records, wal_fpi,
wal_bytes, wal_buffers_full, wal_write, wal_sync,
wal_write_time, wal_sync_time, archived_count,
last_archived_wal, last_archived_time, failed_count,
last_failed_wal, last_failed_time, stats_reset

### pg_settings

This probe monitors PostgreSQL configuration settings
with change detection. The probe only stores data when
configuration changes are detected.

- Source View: `pg_settings`
- Default Interval: 3600 seconds (1 hour)
- Default Retention: 365 days (1 year)
- Key Metrics: PostgreSQL configuration parameters
  and their sources
- Use Cases: Configuration change tracking,
  configuration drift detection, historical
  configuration analysis, compliance auditing
- Special Behavior: Uses SHA256 hash comparison to
  detect changes; the garbage collector ensures the
  most recent snapshot for each server is never
  deleted.

**Columns Collected**: name, setting, unit,
category, short_desc, extra_desc, context, vartype,
source, min_val, max_val, enumvals, boot_val,
reset_val, sourcefile, sourceline, pending_restart

See [pg_settings Usage Guide](pg-settings-usage.md)
for detailed examples and query patterns.

### pg_hba_file_rules

This probe monitors PostgreSQL `pg_hba.conf`
authentication configuration with change detection.
The probe only stores data when HBA rules change.

- Source View: `pg_hba_file_rules`
- Default Interval: 3600 seconds (1 hour)
- Default Retention: 365 days (1 year)
- Key Metrics: Authentication rules, methods,
  databases, users, addresses
- Use Cases: Authentication policy tracking,
  security audit compliance, HBA configuration drift
  detection, forensic analysis
- Special Behavior: Uses SHA256 hash comparison to
  detect changes; the garbage collector ensures the
  most recent snapshot for each server is never
  deleted.

**Columns Collected**: rule_number, file_name,
line_number, type, database, user_name, address,
netmask, auth_method, options, error

### pg_ident_file_mappings

This probe monitors PostgreSQL `pg_ident.conf` user
mapping configuration with change detection. The
probe only stores data when ident mappings change.

- Source View: `pg_ident_file_mappings`
- Default Interval: 3600 seconds (1 hour)
- Default Retention: 365 days (1 year)
- Key Metrics: Ident map names, system usernames,
  PostgreSQL usernames
- Use Cases: User mapping audit tracking, compliance
  verification, mapping drift detection, security
  policy enforcement
- Special Behavior: Uses SHA256 hash comparison to
  detect changes; the garbage collector ensures the
  most recent snapshot for each server is never
  deleted.

**Columns Collected**: map_number, file_name,
line_number, map_name, sys_name, pg_username, error

### pg_server_info

This probe monitors PostgreSQL server identification
and configuration with change detection. The probe
only stores data when server configuration changes.

- Source: Various system functions and `pg_extension`
- Default Interval: 3600 seconds (1 hour)
- Default Retention: 365 days (1 year)
- Key Metrics: Server version, system identifier,
  replication settings, installed extensions
- Use Cases: Server inventory tracking, upgrade
  verification, extension monitoring, capacity
  planning
- Special Behavior: Uses SHA256 hash comparison to
  detect changes; the garbage collector ensures the
  most recent snapshot for each server is never
  deleted.

**Columns Collected**: server_version,
server_version_num, system_identifier, cluster_name,
data_directory, max_connections, max_wal_senders,
max_replication_slots, installed_extensions

### pg_node_role

This probe detects and tracks PostgreSQL node roles
within various cluster topologies. The probe
identifies how each node participates in replication
configurations including binary replication, logical
replication, and Spock multi-master replication.

- Source: Multiple system views and extension
  catalogs
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 30 days
- Key Metrics: Primary role, role flags, replication
  status, standby info
- Use Cases: Cluster topology monitoring, failover
  detection, replication health tracking, Spock node
  status monitoring

The probe detects the following primary roles:

- `standalone` indicates no replication configured.
- `binary_primary` indicates a source for physical
  replication.
- `binary_standby` indicates a physical replication
  target.
- `binary_cascading` indicates a standby that is
  also a primary.
- `logical_publisher` indicates a native logical
  replication source.
- `logical_subscriber` indicates a native logical
  replication target.
- `logical_bidirectional` indicates both publisher
  and subscriber.
- `spock_node` indicates an active Spock multi-master
  node.
- `spock_standby` indicates a binary standby of a
  Spock node.

Role flags are non-exclusive capability flags that
indicate all replication capabilities simultaneously.

**Columns Collected**: is_in_recovery, timeline_id,
has_binary_standbys, binary_standby_count,
is_streaming_standby, upstream_host, upstream_port,
received_lsn, replayed_lsn, publication_count,
subscription_count, active_subscription_count,
has_spock, spock_node_id, spock_node_name,
spock_subscription_count, primary_role, role_flags,
role_details

### pg_database

This probe monitors the `pg_database` catalog
including transaction ID wraparound indicators and
database size.

- Source: `pg_database` catalog
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Transaction ID age, multixact age,
  database size
- Use Cases: XID wraparound monitoring, database
  size tracking, capacity planning

**Columns Collected**: datname, datdba, encoding,
datlocprovider, datistemplate, datallowconn,
datconnlimit, datfrozenxid, datminmxid,
dattablespace, age_datfrozenxid, age_datminmxid,
database_size_bytes

## Database-Scoped Probes

Database-scoped probes execute once for each database
on a monitored server.

### pg_stat_database

This probe monitors database-wide statistics.

- Source View: `pg_stat_database`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Transaction counts, buffer hits,
  tuples, deadlocks
- Use Cases: Database activity monitoring, cache
  hit ratio analysis

**Columns Collected**: datid, datname, numbackends,
xact_commit, xact_rollback, blks_read, blks_hit,
tup_returned, tup_fetched, tup_inserted,
tup_updated, tup_deleted, conflicts, temp_files,
temp_bytes, deadlocks, checksum_failures,
checksum_last_failure, blk_read_time,
blk_write_time, session_time, active_time,
idle_in_transaction_time, sessions,
sessions_abandoned, sessions_fatal,
sessions_killed, stats_reset

### pg_stat_database_conflicts

This probe monitors recovery conflicts on replicas.

- Source View: `pg_stat_database_conflicts`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Conflict counts by type
- Use Cases: Replica conflict monitoring

**Columns Collected**: datid, datname,
confl_tablespace, confl_lock, confl_snapshot,
confl_bufferpin, confl_deadlock

### pg_stat_all_tables

This probe monitors table access and I/O statistics.
The probe consolidates data from
`pg_stat_all_tables` and includes I/O statistics from
`pg_statio_all_tables`.

- Source Views: `pg_stat_all_tables`,
  `pg_statio_all_tables`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Sequential/index scans, tuples
  read/written, vacuum/analyze, heap/index/toast
  blocks read/hit
- Use Cases: Table usage analysis, vacuum monitoring,
  I/O analysis, cache effectiveness

**Columns Collected**: relid, schemaname, relname,
seq_scan, seq_tup_read, idx_scan, idx_tup_fetch,
n_tup_ins, n_tup_upd, n_tup_del, n_tup_hot_upd,
n_live_tup, n_dead_tup, n_mod_since_analyze,
n_ins_since_vacuum, last_vacuum, last_autovacuum,
last_analyze, last_autoanalyze, vacuum_count,
autovacuum_count, analyze_count,
autoanalyze_count, heap_blks_read, heap_blks_hit,
idx_blks_read, idx_blks_hit, toast_blks_read,
toast_blks_hit, tidx_blks_read, tidx_blks_hit

### pg_stat_all_indexes

This probe monitors index usage and I/O statistics.
The probe consolidates data from
`pg_stat_all_indexes` and includes I/O statistics
from `pg_statio_all_indexes`.

- Source Views: `pg_stat_all_indexes`,
  `pg_statio_all_indexes`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Index scans, tuples read/fetched,
  index blocks read/hit
- Use Cases: Index usage analysis, unused index
  detection, I/O analysis

**Columns Collected**: relid, indexrelid,
schemaname, relname, indexrelname, idx_scan,
idx_tup_read, idx_tup_fetch, idx_blks_read,
idx_blks_hit

### pg_statio_all_sequences

This probe monitors sequence I/O statistics.

- Source View: `pg_statio_all_sequences`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Sequence blocks read/hit
- Use Cases: Sequence performance analysis

**Columns Collected**: relid, schemaname, relname,
blks_read, blks_hit

### pg_stat_user_functions

This probe monitors user-defined function statistics.

- Source View: `pg_stat_user_functions`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Function calls, total/self time
- Use Cases: Function performance analysis

**Columns Collected**: funcid, schemaname, funcname,
calls, total_time, self_time

### pg_stat_statements

This probe monitors query performance statistics.
The probe requires the `pg_stat_statements`
extension.

- Source View: `pg_stat_statements`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Query execution times, calls, rows,
  I/O, planning
- Use Cases: Query performance analysis, slow query
  identification
- Query Limit: Top 1000 queries by total execution
  time

The probe collects only the top 1000 queries ordered
by total execution time to prevent excessive data
collection on busy systems. Queries with NULL
`queryid` are automatically filtered out. The limit
is hard-coded in the probe implementation.

**Columns Collected**: userid, dbid, toplevel,
queryid, query, plans, total_plan_time,
min_plan_time, max_plan_time, mean_plan_time,
stddev_plan_time, calls, total_exec_time,
min_exec_time, max_exec_time, mean_exec_time,
stddev_exec_time, rows, shared_blks_hit,
shared_blks_read, shared_blks_dirtied,
shared_blks_written, local_blks_hit,
local_blks_read, local_blks_dirtied,
local_blks_written, temp_blks_read,
temp_blks_written, blk_read_time, blk_write_time,
temp_blk_read_time, temp_blk_write_time,
wal_records, wal_fpi, wal_bytes, jit_functions,
jit_generation_time, jit_inlining_count,
jit_inlining_time, jit_optimization_count,
jit_optimization_time, jit_emission_count,
jit_emission_time

### pg_extension

This probe monitors installed PostgreSQL extensions
with change detection. The probe only stores data
when installed extensions change.

- Source: `pg_extension` catalog joined with
  `pg_namespace`
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Extension names, versions, schemas
- Use Cases: Extension inventory tracking, upgrade
  verification, compliance auditing
- Special Behavior: Uses SHA256 hash comparison to
  detect changes; the garbage collector ensures the
  most recent snapshot for each server is never
  deleted.

**Columns Collected**: extname, extversion,
extrelocatable, schema_name

## System Statistics Probes

These probes collect operating system metrics through
the `system_stats` PostgreSQL extension. The probes
require the extension on the monitored server.

### pg_sys_os_info

This probe monitors operating system identification
information.

- Source: `pg_sys_os_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: OS name, version, architecture
- Use Cases: Server inventory, OS upgrade tracking

### pg_sys_cpu_info

This probe monitors CPU hardware information.

- Source: `pg_sys_cpu_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: CPU model, cores, speed
- Use Cases: Hardware inventory, capacity planning

### pg_sys_cpu_usage_info

This probe monitors real-time CPU utilization.

- Source: `pg_sys_cpu_usage_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: CPU utilization percentages (user,
  system, idle)
- Use Cases: CPU load monitoring, alerting on high
  usage

### pg_sys_memory_info

This probe monitors system memory usage.

- Source: `pg_sys_memory_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Total, used, free, cached memory
- Use Cases: Memory utilization monitoring, capacity
  planning

### pg_sys_io_analysis_info

This probe monitors disk I/O statistics.

- Source: `pg_sys_io_analysis_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Read/write operations, throughput,
  latency
- Use Cases: I/O performance analysis, bottleneck
  detection

### pg_sys_disk_info

This probe monitors disk space usage.

- Source: `pg_sys_disk_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Disk capacity, used space, usage
  percentage
- Use Cases: Disk space monitoring, capacity
  planning, alerting

### pg_sys_load_avg_info

This probe monitors system load averages.

- Source: `pg_sys_load_avg_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: 1-minute, 5-minute, 15-minute load
  averages
- Use Cases: System load monitoring, trend analysis

### pg_sys_process_info

This probe monitors system process statistics.

- Source: `pg_sys_process_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Process counts, states
- Use Cases: Process monitoring, resource usage
  tracking

### pg_sys_network_info

This probe monitors network interface statistics.

- Source: `pg_sys_network_info()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Network throughput, packets, errors
- Use Cases: Network performance monitoring, error
  detection

### pg_sys_cpu_memory_by_process

This probe monitors per-process CPU and memory usage.

- Source: `pg_sys_cpu_memory_by_process()` function
- Default Interval: 300 seconds (5 minutes)
- Default Retention: 7 days
- Key Metrics: Per-process CPU percentage, memory
  usage
- Use Cases: Process-level resource analysis,
  identifying resource-intensive processes

## System Limits and Constants

The Collector enforces several limits to ensure
stable operation and prevent resource exhaustion.
These limits are hard-coded in the implementation.

### Query Collection Limits

The `pg_stat_statements` probe has a query limit of
1000 queries, defined as
`PgStatStatementsQueryLimit` in
`src/probes/constants.go`. The probe collects the
top 1000 queries ordered by total execution time.
This limit is not configurable and requires code
modification to change.

### Connection Pool Limits

The datastore pool has a default maximum of 25
connections, defined as `DefaultPoolMaxConnections`
in `src/constants.go`. The monitored pool has a
default maximum of 5 connections per server, defined
as `DefaultMonitoredPoolMaxConnections`. The idle
connection timeout defaults to 300 seconds, defined
as `DefaultPoolIdleSeconds`.

### Operation Timeouts

The system enforces the following timeouts:

- Connection timeout is 10 seconds for initial
  database connection establishment.
- Context timeout is 30 seconds for general context
  operations.
- Probe execution timeout is 60 seconds for
  individual probe query execution.
- Datastore wait timeout is 5 seconds for waiting
  for a datastore connection from the pool.
- Monitored pool wait timeout is 120 seconds for
  waiting for a monitored connection from the pool.

### Configuration Reload

Probe configurations automatically reload from the
database every 5 minutes. Changes to
`collection_interval_seconds`, `retention_days`, or
`is_enabled` take effect within 5 minutes without
a restart.

### Modifying Limits

Most limits are configured through the configuration
file. To modify hard-coded limits, follow these
steps:

1. Edit the constant in the source file.
2. Rebuild the Collector with `make build`.
3. Restart the Collector with the new binary.

Hard-coded limits exist for stability and performance
reasons. Test thoroughly in a non-production
environment before modifying these values.

## See Also

The following resources provide additional details.

- [Probes](probes.md) explains how probes work
  internally.
- [Adding Probes](adding-probes.md) covers how to
  create custom probes.
- [pg_settings Usage Guide](pg-settings-usage.md)
  provides examples for using `pg_settings` data.
