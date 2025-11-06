# Probe Reference

Complete reference for all built-in probes in the Collector.

## Probe Categories

- **Server-Scoped**: Collect server-wide statistics (15 probes)
- **Database-Scoped**: Collect per-database statistics (9 probes)

## Server-Scoped Probes

These probes execute once per monitored connection.

### pg_stat_activity

Monitors current database activity and backend processes.

- **Source View**: `pg_stat_activity`
- **Default Interval**: 60 seconds
- **Default Retention**: 7 days
- **Key Metrics**: Active connections, query states, wait events
- **Use Cases**: Connection monitoring, identifying long-running queries,
  detecting locks

**Columns Collected**: datid, datname, pid, leader_pid, usesysid, usename,
application_name, client_addr, client_hostname, client_port, backend_start,
xact_start, query_start, state_change, wait_event_type, wait_event, state,
backend_xid, backend_xmin, query, backend_type

### pg_stat_archiver

Monitors WAL archiver statistics.

- **Source View**: `pg_stat_archiver`
- **Default Interval**: 600 seconds (10 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Archived WAL count, failed archives, last archive time
- **Use Cases**: Archive monitoring, backup health checks

**Columns Collected**: archived_count, last_archived_wal, last_archived_time,
failed_count, last_failed_wal, last_failed_time, stats_reset

### pg_stat_bgwriter

Monitors background writer statistics.

- **Source View**: `pg_stat_bgwriter`
- **Default Interval**: 600 seconds (10 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Checkpoint timing, buffer writes, backend fsync
- **Use Cases**: Checkpoint tuning, I/O performance analysis

**Columns Collected**: checkpoints_timed, checkpoints_req,
checkpoint_write_time, checkpoint_sync_time, buffers_checkpoint,
buffers_clean, maxwritten_clean, buffers_backend, buffers_backend_fsync,
buffers_alloc, stats_reset

### pg_stat_checkpointer

Monitors checkpointer process statistics (PostgreSQL 15+).

- **Source View**: `pg_stat_checkpointer`
- **Default Interval**: 600 seconds (10 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Checkpoints, buffers written, sync times
- **Use Cases**: Checkpoint performance analysis, I/O tuning

**Columns Collected**: num_timed, num_requested, restartpoints_timed,
restartpoints_req, restartpoints_done, write_time, sync_time, buffers_written,
stats_reset

### pg_stat_gssapi

Monitors GSSAPI authentication information.

- **Source View**: `pg_stat_gssapi`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: GSSAPI authentication status, principals, encryption
- **Use Cases**: Kerberos authentication monitoring

**Columns Collected**: pid, gss_authenticated, gss_principal, gss_encrypted,
gss_credentials_delegated

### pg_stat_io

Monitors I/O statistics by backend type and context.

- **Source View**: `pg_stat_io`
- **Default Interval**: 900 seconds (15 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Reads, writes, extends, hits by context
- **Use Cases**: Detailed I/O analysis, cache efficiency

**Columns Collected**: backend_type, context, reads, read_time, writes,
write_time, writebacks, writeback_time, extends, extend_time, op_bytes, hits,
evictions, reuses, fsyncs, fsync_time, stats_reset

### pg_stat_recovery_prefetch

Monitors recovery prefetch statistics.

- **Source View**: `pg_stat_recovery_prefetch`
- **Default Interval**: 600 seconds (10 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Prefetch operations, hit rate, distance
- **Use Cases**: Recovery performance tuning

**Columns Collected**: stats_reset, prefetch, hit, skip_init, skip_new,
skip_fpw, skip_rep, wal_distance, block_distance, io_depth

### pg_stat_replication

Monitors replication status and lag.

- **Source View**: `pg_stat_replication`
- **Default Interval**: 30 seconds
- **Default Retention**: 7 days
- **Key Metrics**: Replication lag, sync state, sent/received LSN
- **Use Cases**: Replication monitoring, lag alerting

**Columns Collected**: pid, usesysid, usename, application_name, client_addr,
client_hostname, client_port, backend_start, backend_xmin, state, sent_lsn,
write_lsn, flush_lsn, replay_lsn, write_lag, flush_lag, replay_lag,
sync_state, sync_priority, reply_time

### pg_stat_replication_slots

Monitors replication slot usage and WAL retention.

- **Source View**: `pg_stat_replication_slots`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Spill files, bytes, transactions
- **Use Cases**: Slot monitoring, WAL buildup detection

**Columns Collected**: slot_name, spill_txns, spill_count, spill_bytes,
stream_txns, stream_count, stream_bytes, total_txns, total_bytes, stats_reset

### pg_stat_slru

Monitors SLRU (Simple LRU) cache statistics.

- **Source View**: `pg_stat_slru`
- **Default Interval**: 600 seconds (10 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Cache hits, reads, writes by SLRU type
- **Use Cases**: Internal cache performance analysis

**Columns Collected**: name, blks_zeroed, blks_hit, blks_read, blks_written,
blks_exists, flushes, truncates, stats_reset

### pg_stat_ssl

Monitors SSL connection information.

- **Source View**: `pg_stat_ssl`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: SSL version, cipher, compression, client DN
- **Use Cases**: SSL/TLS security monitoring

**Columns Collected**: pid, ssl, ssl_version, ssl_cipher, ssl_bits,
ssl_client_dn, ssl_client_serial, ssl_issuer_dn

### pg_stat_subscription

Monitors logical replication subscriptions.

- **Source View**: `pg_stat_subscription`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Subscription state, worker info, LSN positions
- **Use Cases**: Logical replication monitoring

**Columns Collected**: subid, subname, pid, leader_pid, relid, received_lsn,
last_msg_send_time, last_msg_receipt_time, latest_end_lsn, latest_end_time

### pg_stat_subscription_stats

Monitors subscription statistics.

- **Source View**: `pg_stat_subscription_stats`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Apply errors, sync errors
- **Use Cases**: Subscription health monitoring

**Columns Collected**: subid, subname, apply_error_count, sync_error_count,
stats_reset

### pg_stat_wal

Monitors WAL generation statistics.

- **Source View**: `pg_stat_wal`
- **Default Interval**: 600 seconds (10 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: WAL records, bytes, sync operations
- **Use Cases**: WAL generation analysis, write load monitoring

**Columns Collected**: wal_records, wal_fpi, wal_bytes, wal_buffers_full,
wal_write, wal_sync, wal_write_time, wal_sync_time, stats_reset

### pg_stat_wal_receiver

Monitors WAL receiver status on replicas.

- **Source View**: `pg_stat_wal_receiver`
- **Default Interval**: 30 seconds
- **Default Retention**: 7 days
- **Key Metrics**: Receiver status, timeline, LSN positions
- **Use Cases**: Replica health monitoring

**Columns Collected**: pid, status, receive_start_lsn, receive_start_tli,
written_lsn, flushed_lsn, received_tli, last_msg_send_time,
last_msg_receipt_time, latest_end_lsn, latest_end_time, slot_name, sender_host,
sender_port, conninfo

## Database-Scoped Probes

These probes execute once for each database on a monitored server.

### pg_stat_database

Monitors database-wide statistics.

- **Source View**: `pg_stat_database`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Transaction counts, buffer hits, tuples, deadlocks
- **Use Cases**: Database activity monitoring, cache hit ratio analysis

**Columns Collected**: datid, datname, numbackends, xact_commit,
xact_rollback, blks_read, blks_hit, tup_returned, tup_fetched, tup_inserted,
tup_updated, tup_deleted, conflicts, temp_files, temp_bytes, deadlocks,
checksum_failures, checksum_last_failure, blk_read_time, blk_write_time,
session_time, active_time, idle_in_transaction_time, sessions,
sessions_abandoned, sessions_fatal, sessions_killed, stats_reset

### pg_stat_database_conflicts

Monitors recovery conflicts on replicas.

- **Source View**: `pg_stat_database_conflicts`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Conflict counts by type
- **Use Cases**: Replica conflict monitoring

**Columns Collected**: datid, datname, confl_tablespace, confl_lock,
confl_snapshot, confl_bufferpin, confl_deadlock

### pg_stat_all_tables

Monitors table access statistics.

- **Source View**: `pg_stat_all_tables`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Sequential/index scans, tuples read/written, vacuum/analyze
- **Use Cases**: Table usage analysis, vacuum monitoring

**Columns Collected**: relid, schemaname, relname, seq_scan, seq_tup_read,
idx_scan, idx_tup_fetch, n_tup_ins, n_tup_upd, n_tup_del, n_tup_hot_upd,
n_live_tup, n_dead_tup, n_mod_since_analyze, n_ins_since_vacuum,
last_vacuum, last_autovacuum, last_analyze, last_autoanalyze, vacuum_count,
autovacuum_count, analyze_count, autoanalyze_count

### pg_stat_all_indexes

Monitors index usage statistics.

- **Source View**: `pg_stat_all_indexes`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Index scans, tuples read/fetched
- **Use Cases**: Index usage analysis, unused index detection

**Columns Collected**: relid, indexrelid, schemaname, relname, indexrelname,
idx_scan, idx_tup_read, idx_tup_fetch

### pg_statio_all_tables

Monitors table I/O statistics.

- **Source View**: `pg_statio_all_tables`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Heap/index/toast blocks read/hit
- **Use Cases**: Table I/O analysis, cache effectiveness

**Columns Collected**: relid, schemaname, relname, heap_blks_read,
heap_blks_hit, idx_blks_read, idx_blks_hit, toast_blks_read, toast_blks_hit,
tidx_blks_read, tidx_blks_hit

### pg_statio_all_indexes

Monitors index I/O statistics.

- **Source View**: `pg_statio_all_indexes`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Index blocks read/hit
- **Use Cases**: Index I/O analysis

**Columns Collected**: relid, indexrelid, schemaname, relname, indexrelname,
idx_blks_read, idx_blks_hit

### pg_statio_all_sequences

Monitors sequence I/O statistics.

- **Source View**: `pg_statio_all_sequences`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Sequence blocks read/hit
- **Use Cases**: Sequence performance analysis

**Columns Collected**: relid, schemaname, relname, blks_read, blks_hit

### pg_stat_user_functions

Monitors user-defined function statistics.

- **Source View**: `pg_stat_user_functions`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Function calls, total/self time
- **Use Cases**: Function performance analysis

**Columns Collected**: funcid, schemaname, funcname, calls, total_time,
self_time

### pg_stat_statements

Monitors query performance statistics (requires pg_stat_statements extension).

- **Source View**: `pg_stat_statements`
- **Default Interval**: 300 seconds (5 minutes)
- **Default Retention**: 7 days
- **Key Metrics**: Query execution times, calls, rows, I/O, planning
- **Use Cases**: Query performance analysis, slow query identification
- **Query Limit**: Top 1000 queries by total execution time

**Important**: This probe collects only the **top 1000 queries** ordered by total
execution time to prevent excessive data collection on busy systems. Queries with
NULL queryid (utility statements like VACUUM, ANALYZE) are automatically filtered
out. If your database tracks more than 1000 queries, lower-impact queries will
not be collected. This limit is hard-coded in the probe implementation. Focus on
optimizing the highest-impact queries first.

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
- Behavior: Returns error if datastore connection not available within 5 seconds
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
