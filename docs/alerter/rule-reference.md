# Alert Rule Reference

This document lists all built-in alert rules included with the alerter.
Each rule monitors a specific PostgreSQL metric and triggers an alert
when the threshold is exceeded.

## Connection Rules

### High Connection Utilization

Alerts when database connection usage approaches the maximum limit.

| Property | Value |
|----------|-------|
| Metric | `connection_utilization_percent` |
| Operator | `>` |
| Default Threshold | 80 |
| Default Severity | warning |

A high connection utilization indicates the database may run out of
available connections. Consider increasing `max_connections` or
implementing connection pooling.

### High Max Connections

Alerts when the `max_connections` setting exceeds a threshold.

| Property | Value |
|----------|-------|
| Metric | `pg_settings.max_connections` |
| Operator | `>` |
| Default Threshold | 500 |
| Default Severity | warning |

A very high `max_connections` setting can degrade performance. Consider
using a connection pooler such as PgBouncer instead of increasing the
connection limit.

### Blocked Sessions

Alerts when sessions are waiting for locks held by other sessions.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_activity.blocked_count` |
| Operator | `>` |
| Default Threshold | 5 |
| Default Severity | warning |

Blocked sessions indicate lock contention. Investigate the blocking
queries and consider optimizing the workload.

### Long-Running Idle Transactions

Alerts when a transaction has been idle in transaction state too long.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_activity.idle_in_transaction_seconds` |
| Operator | `>` |
| Default Threshold | 300 |
| Default Severity | warning |

Idle in transaction connections hold locks and prevent vacuum from
reclaiming space. Configure `idle_in_transaction_session_timeout` to
automatically terminate these connections.

### Long Lock Wait Time

Alerts when a session has been waiting for a lock too long.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_activity.max_lock_wait_seconds` |
| Operator | `>` |
| Default Threshold | 60 |
| Default Severity | warning |

Long lock waits can indicate deadlock-prone workloads or inefficient
query patterns.

## Query Performance Rules

### Long-Running Query

Alerts when a query has been executing for longer than the threshold.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_activity.max_query_duration_seconds` |
| Operator | `>` |
| Default Threshold | 300 |
| Default Severity | warning |

Long-running queries may indicate missing indexes, inefficient query
plans, or inappropriate workloads.

### Long-Running Transaction

Alerts when a transaction has been active for longer than the threshold.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_activity.max_xact_duration_seconds` |
| Operator | `>` |
| Default Threshold | 600 |
| Default Severity | warning |

Long transactions can cause bloat and prevent vacuum from running
effectively.

### Slow Query Count

Alerts when the number of slow queries exceeds a threshold. This rule
requires the pg_stat_statements extension.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_statements.slow_query_count` |
| Operator | `>` |
| Default Threshold | 10 |
| Default Severity | warning |
| Required Extension | `pg_stat_statements` |

A high slow query count indicates performance problems that should be
investigated.

## Replication Rules

### High Replication Lag (Time)

Alerts when replication replay is behind the primary.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_replication.replay_lag_seconds` |
| Operator | `>` |
| Default Threshold | 30 |
| Default Severity | warning |

Replication lag can indicate network issues, replica resource
constraints, or write-heavy workloads.

### High Replication Lag (Bytes)

Alerts when replication is behind by more than the specified byte count.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_replication.lag_bytes` |
| Operator | `>` |
| Default Threshold | 104857600 (100 MB) |
| Default Severity | warning |

This metric provides a more accurate view of replication lag when
write activity is bursty.

### Inactive Replication Slot

Alerts when a replication slot becomes inactive.

| Property | Value |
|----------|-------|
| Metric | `pg_replication_slots.inactive` |
| Operator | `>=` |
| Default Threshold | 1 |
| Default Severity | critical |

Inactive replication slots prevent WAL cleanup and can cause disk
exhaustion. Drop unused slots or reconnect the subscriber.

### High Replication Slot WAL Retention

Alerts when a replication slot retains more WAL data than the
threshold.

| Property | Value |
|----------|-------|
| Metric | `pg_replication_slots.retained_bytes` |
| Operator | `>` |
| Default Threshold | 1073741824 (1 GB) |
| Default Severity | warning |

Large WAL retention by a replication slot can lead to disk
exhaustion. Investigate the subscriber connection or consider
dropping unused slots.

## Storage Rules

### High Disk Usage

Alerts when disk usage exceeds the threshold.

| Property | Value |
|----------|-------|
| Metric | `pg_sys_disk_info.used_percent` |
| Operator | `>` |
| Default Threshold | 85 |
| Default Severity | warning |

High disk usage can lead to database failures. Add storage capacity or
clean up unnecessary data.

### Critical Disk Usage

Alerts when disk usage is critically high.

| Property | Value |
|----------|-------|
| Metric | `pg_sys_disk_info.used_percent` |
| Operator | `>` |
| Default Threshold | 95 |
| Default Severity | critical |

Critical disk usage requires immediate action to prevent database
outages.

### High Dead Tuple Percentage

Alerts when tables have accumulated too many dead tuples.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_all_tables.dead_tuple_percent` |
| Operator | `>` |
| Default Threshold | 10 |
| Default Severity | warning |

Dead tuples indicate vacuum is not keeping up with updates.
Check vacuum settings and consider running manual vacuum.
The alerter excludes tables with fewer than 1,000 total tuples
from evaluation. This filter reduces noise from small catalog
and system tables.

### High Table Bloat

Alerts when table bloat exceeds the threshold.

| Property | Value |
|----------|-------|
| Metric | `table_bloat_ratio` |
| Operator | `>` |
| Default Threshold | 50 |
| Default Severity | warning |

Table bloat reduces query performance and wastes storage. Consider
running `VACUUM FULL` during a maintenance window.

### Stale Autovacuum

Alerts when a table has not been autovacuumed recently.

| Property | Value |
|----------|-------|
| Metric | `table_last_autovacuum_hours` |
| Operator | `>` |
| Default Threshold | 168 (7 days) |
| Default Severity | warning |

Tables that have not been vacuumed may have accumulated dead tuples
or outdated statistics.

### High Transaction ID Age

Alerts when transaction IDs are approaching wraparound.

| Property | Value |
|----------|-------|
| Metric | `age_percent` |
| Operator | `>` |
| Default Threshold | 50 |
| Default Severity | warning |

Transaction ID wraparound prevention requires aggressive vacuuming.
Monitor this metric carefully on busy databases.

## Database Performance Rules

### Low Cache Hit Ratio

Alerts when the buffer cache hit ratio falls below the threshold.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_database.cache_hit_ratio` |
| Operator | `<` |
| Default Threshold | 90 |
| Default Severity | warning |

A low cache hit ratio indicates the database needs more memory for
shared_buffers or the working set is too large.

### Deadlocks Detected

Alerts when deadlocks occur.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_database.deadlocks_delta` |
| Operator | `>` |
| Default Threshold | 0 |
| Default Severity | warning |

Deadlocks indicate lock ordering problems in the application. Review
the application logic to prevent deadlocks.

### High Temporary File Usage

Alerts when temporary file creation exceeds the threshold.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_database.temp_files_delta` |
| Operator | `>` |
| Default Threshold | 10 |
| Default Severity | warning |

Temporary files are created when work_mem is insufficient for sort
and hash operations. Consider increasing work_mem.

## System Resource Rules

### High CPU Usage

Alerts when CPU usage exceeds the threshold.

| Property | Value |
|----------|-------|
| Metric | `pg_sys_cpu_usage_info.processor_time_percent` |
| Operator | `>` |
| Default Threshold | 80 |
| Default Severity | warning |

High CPU usage may indicate inefficient queries, missing indexes, or
insufficient hardware capacity.

### High Memory Usage

Alerts when memory usage exceeds the threshold.

| Property | Value |
|----------|-------|
| Metric | `pg_sys_memory_info.used_percent` |
| Operator | `>` |
| Default Threshold | 85 |
| Default Severity | warning |

High memory usage can lead to swap usage and performance degradation.
Review memory allocation settings.

### High System Load

Alerts when the 15-minute load average exceeds the threshold.

| Property | Value |
|----------|-------|
| Metric | `pg_sys_load_avg_info.load_avg_fifteen_minutes` |
| Operator | `>` |
| Default Threshold | 4 |
| Default Severity | warning |

High system load indicates the server is overloaded. Investigate the
source of the load and consider scaling resources.

## Archive Rules

### Archive Failures

Alerts when WAL archiving fails.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_wal.failed_count_delta` |
| Operator | `>` |
| Default Threshold | 0 |
| Default Severity | critical |

Archive failures can prevent point-in-time recovery. Check the archive
command and destination storage.

## Checkpoint Rules

### Frequent Requested Checkpoints

Alerts when checkpoints are requested too frequently.

| Property | Value |
|----------|-------|
| Metric | `pg_stat_checkpointer.checkpoints_req_delta` |
| Operator | `>` |
| Default Threshold | 5 |
| Default Severity | warning |

Frequent requested checkpoints indicate checkpoint_segments or
max_wal_size may be too low for the workload.

## Customizing Rules

All built-in rules can be customized through per-connection overrides.
See the [Alert Rules](alert-rules.md) documentation for details on
creating overrides.

To disable a built-in rule, create an override with `enabled = false`.
