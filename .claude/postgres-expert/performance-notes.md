# Performance Notes and Optimization Guide

This document covers indexing strategies, performance tuning, and operational
best practices for the pgEdge AI Workbench metadata database.

## Database Workload Characteristics

### Workload Type

The Workbench metadata database is primarily **OLTP** with some **time-series**
characteristics:

**OLTP Aspects:**
- Frequent small transactions (authentication, authorization checks)
- High read-to-write ratio (10:1 to 100:1 typical)
- Primary key and index-based lookups
- Low latency requirements (< 10ms for auth checks)

**Time-Series Aspects:**
- Continuous metrics ingestion from monitored servers
- Write-heavy for metrics tables
- Time-range queries common
- Partitioned storage
- Retention-based cleanup

### Transaction Patterns

1. **Authentication** (very frequent, sub-10ms target)
   - Hash lookup in user_accounts, service_tokens, or user_sessions
   - Single row read

2. **Authorization** (frequent, 10-50ms target)
   - Recursive CTE for group hierarchy
   - Join with privilege tables
   - Multiple table reads

3. **Metrics Collection** (continuous, bulk inserts)
   - Large batch INSERTs into partitioned tables
   - Minimal indexes to keep insert performance high
   - Asynchronous from user requests

4. **Metrics Queries** (moderate, 100ms-1s acceptable)
   - Time-range scans on partitioned tables
   - Aggregations over time windows
   - Connection-specific filtering

## Indexing Strategy

### Core Tables

#### user_accounts

```sql
-- Primary key (implicit index)
PRIMARY KEY (id)

-- Unique constraint (implicit unique index)
UNIQUE (username)

-- Explicit indexes for lookups
CREATE INDEX idx_user_accounts_username ON user_accounts(username);
CREATE INDEX idx_user_accounts_email ON user_accounts(email);
```

**Purpose:**
- `username` index: Fast authentication lookup (used in every login)
- `email` index: User search and forgot-password flows

**Performance:**
- Expected: < 1ms for username lookup
- Index size: Small (< 1MB for thousands of users)

**Monitoring:**
```sql
SELECT schemaname, tablename, indexname,
       pg_size_pretty(pg_relation_size(indexrelid)) as size,
       idx_scan, idx_tup_read
FROM pg_stat_user_indexes
WHERE tablename = 'user_accounts'
ORDER BY idx_scan DESC;
```

#### service_tokens

```sql
PRIMARY KEY (id)
UNIQUE (name)
UNIQUE (token_hash)

CREATE INDEX idx_service_tokens_name ON service_tokens(name);
CREATE INDEX idx_service_tokens_token_hash ON service_tokens(token_hash);
CREATE INDEX idx_service_tokens_expires_at ON service_tokens(expires_at);
```

**Purpose:**
- `token_hash` index: Critical for authentication (every API request with
  service token)
- `name` index: Administrative lookups
- `expires_at` index: Cleanup queries for expired tokens

**Performance:**
- Expected: < 1ms for token_hash lookup
- Ensure token_hash uses BTREE (default) for exact match performance

#### user_tokens

```sql
PRIMARY KEY (id)
UNIQUE (token_hash)

CREATE INDEX idx_user_tokens_user_id ON user_tokens(user_id);
CREATE INDEX idx_user_tokens_token_hash ON user_tokens(token_hash);
CREATE INDEX idx_user_tokens_expires_at ON user_tokens(expires_at);
```

**Purpose:**
- `token_hash` index: Authentication lookups
- `user_id` index: Foreign key index for joins and cascade deletes
- `expires_at` index: Token expiration cleanup

**Performance Note:**
- Foreign key index on `user_id` is CRITICAL for DELETE performance on
  user_accounts (CASCADE)
- Without it, deleting a user with many tokens causes seq scan

#### connections

```sql
PRIMARY KEY (id)

CREATE INDEX idx_connections_name ON connections(name);
CREATE INDEX idx_connections_owner_username ON connections(owner_username);
CREATE INDEX idx_connections_owner_token ON connections(owner_token);
CREATE INDEX idx_connections_is_monitored ON connections(is_monitored)
    WHERE is_monitored = TRUE;  -- Partial index
```

**Purpose:**
- `name` index: User-facing connection selection
- `owner_username`, `owner_token`: Fast ownership checks
- `is_monitored` partial index: Collector queries only active connections

**Partial Index Benefit:**
```sql
-- This query uses the partial index (very fast)
SELECT * FROM connections WHERE is_monitored = TRUE;

-- This query does NOT use it (but that's fine, it's rare)
SELECT * FROM connections WHERE is_monitored = FALSE;
```

Partial index size is much smaller (only indexes monitored=true rows).

#### probe_configs

```sql
PRIMARY KEY (id)

CREATE INDEX idx_probe_configs_enabled ON probe_configs(is_enabled);
CREATE UNIQUE INDEX probe_configs_name_global_key
    ON probe_configs(name) WHERE connection_id IS NULL;
CREATE UNIQUE INDEX probe_configs_name_connection_key
    ON probe_configs(name, COALESCE(connection_id, 0));
```

**Purpose:**
- `is_enabled` index: Find active probes for collection
- Unique indexes: Enforce business rules and support lookups

**Performance:**
- This table is tiny (< 100 rows typical), so indexes are overkill
- Kept for constraint enforcement and future-proofing

### RBAC Tables

#### group_memberships

```sql
PRIMARY KEY (id)
UNIQUE (parent_group_id, member_user_id)
UNIQUE (parent_group_id, member_group_id)

-- Foreign key indexes (crucial for recursive queries)
CREATE INDEX idx_group_memberships_parent ON group_memberships(parent_group_id);
CREATE INDEX idx_group_memberships_user ON group_memberships(member_user_id);
CREATE INDEX idx_group_memberships_group ON group_memberships(member_group_id);
```

**Purpose:**
- Foreign key indexes enable fast joins in recursive CTEs
- Unique constraints prevent duplicate memberships

**Critical Performance Path:**
The recursive group resolution query depends on these indexes:

```sql
WITH RECURSIVE user_groups AS (
    -- This uses idx_group_memberships_user
    SELECT parent_group_id FROM group_memberships
    WHERE member_user_id = :user_id
    UNION
    -- This uses idx_group_memberships_group
    SELECT gm.parent_group_id FROM group_memberships gm
    JOIN user_groups ug ON gm.member_group_id = ug.parent_group_id
)
SELECT * FROM user_groups;
```

**Performance:**
- Without indexes: O(n^2) nested loop, 100ms+ for deep hierarchies
- With indexes: O(n log n), < 5ms for typical hierarchies

**Monitoring:**
```sql
-- Check if recursive CTE is using indexes
EXPLAIN (ANALYZE, BUFFERS)
WITH RECURSIVE ...;
```

Look for "Index Scan" not "Seq Scan" in the plan.

#### group_mcp_privileges

```sql
PRIMARY KEY (id)
UNIQUE (group_id, privilege_id)

CREATE INDEX idx_group_mcp_privileges_group ON group_mcp_privileges(group_id);
CREATE INDEX idx_group_mcp_privileges_priv ON group_mcp_privileges(privilege_id);
```

**Purpose:**
- Fast privilege lookups during authorization
- Support for listing privileges by group or by privilege

**Query Pattern:**
```sql
-- Check if user has privilege (uses idx_group_mcp_privileges_group)
SELECT 1
FROM group_mcp_privileges
WHERE group_id IN (...user's groups...)
  AND privilege_id = :required_privilege
LIMIT 1;
```

#### group_connection_privileges

```sql
PRIMARY KEY (id)
UNIQUE (group_id, connection_id)

CREATE INDEX idx_group_connection_privileges_group
    ON group_connection_privileges(group_id);
CREATE INDEX idx_group_connection_privileges_conn
    ON group_connection_privileges(connection_id);
```

**Purpose:**
- Authorization checks for connection access
- Listing connections by group
- Listing groups by connection

### Metrics Tables

All metrics tables follow this pattern:

```sql
CREATE TABLE metrics.pg_stat_activity (
    connection_id INTEGER NOT NULL,
    -- ... metric columns ...
    collected_at TIMESTAMP NOT NULL,
    PRIMARY KEY (connection_id, collected_at, ...)
) PARTITION BY RANGE (collected_at);

CREATE INDEX idx_pg_stat_activity_collected_at
    ON metrics.pg_stat_activity(collected_at DESC);
CREATE INDEX idx_pg_stat_activity_connection_time
    ON metrics.pg_stat_activity(connection_id, collected_at DESC);
```

**Index Strategy:**

1. **Primary Key**: Includes collected_at for uniqueness and locality
2. **collected_at DESC**: Time-range queries (recent data first)
3. **connection_id, collected_at DESC**: Per-connection time-series queries

**Performance Considerations:**

1. **Index Size vs. Query Performance**: Metrics tables grow large, so indexes
   add significant overhead
2. **Insert Performance**: Every index slows down INSERTs
3. **Partition-Local Indexes**: Indexes are created per partition

**Recommended Approach:**
- Keep only essential indexes on metrics tables
- Consider dropping indexes during bulk loads, recreating after
- Monitor index bloat on high-churn tables

### Index Bloat Management

Metrics tables can experience index bloat over time due to high insert volume.

**Check Index Bloat:**
```sql
SELECT schemaname, tablename, indexname,
       pg_size_pretty(pg_relation_size(indexrelid)) as index_size,
       idx_scan,
       idx_tup_read,
       idx_tup_fetch
FROM pg_stat_user_indexes
WHERE schemaname = 'metrics'
ORDER BY pg_relation_size(indexrelid) DESC;
```

**Bloat Indicators:**
- Very large index size relative to table size
- Low idx_scan (unused index)
- High write rate but poor SELECT performance

**Remediation:**
```sql
-- Rebuild a bloated index
REINDEX INDEX CONCURRENTLY metrics.idx_pg_stat_activity_collected_at;
```

Use CONCURRENTLY to avoid locking the table.

## Partitioning Strategy

### Current Implementation

All metrics tables use **range partitioning** on `collected_at`:

```sql
CREATE TABLE metrics.pg_stat_activity (...)
PARTITION BY RANGE (collected_at);
```

**Partitions are created dynamically** when data is inserted (via trigger or
application logic).

### Partition Management

#### Creating Partitions

Partitions should be created ahead of time to avoid runtime overhead:

```sql
-- Create daily partitions for next 7 days
DO $$
DECLARE
    start_date DATE := CURRENT_DATE;
    end_date DATE;
BEGIN
    FOR i IN 0..7 LOOP
        start_date := CURRENT_DATE + (i || ' days')::INTERVAL;
        end_date := start_date + INTERVAL '1 day';

        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS metrics.pg_stat_activity_%s
             PARTITION OF metrics.pg_stat_activity
             FOR VALUES FROM (%L) TO (%L)',
            to_char(start_date, 'YYYYMMDD'),
            start_date,
            end_date
        );
    END LOOP;
END $$;
```

#### Partition Sizing

**Recommendations:**
- **Daily partitions**: For high-volume metrics (pg_stat_activity,
  pg_stat_statements)
- **Weekly partitions**: For moderate-volume metrics (pg_stat_database)
- **Monthly partitions**: For low-volume metrics (pg_settings)

**Trade-offs:**
- Smaller partitions: Faster DROP (for retention), easier to manage
- Larger partitions: Fewer partitions to track, less overhead

#### Retention and Cleanup

Drop old partitions to implement retention:

```sql
-- Drop partitions older than 7 days for pg_stat_activity
DO $$
DECLARE
    cutoff_date DATE := CURRENT_DATE - INTERVAL '7 days';
    partition_name TEXT;
BEGIN
    FOR partition_name IN
        SELECT tablename
        FROM pg_tables
        WHERE schemaname = 'metrics'
          AND tablename LIKE 'pg_stat_activity_%'
          AND to_date(substring(tablename from 18), 'YYYYMMDD') < cutoff_date
    LOOP
        EXECUTE 'DROP TABLE IF EXISTS metrics.' || partition_name;
        RAISE NOTICE 'Dropped partition: %', partition_name;
    END LOOP;
END $$;
```

**Automated Cleanup:**
Use pg_cron or external scheduler:

```sql
-- Install pg_cron extension
CREATE EXTENSION pg_cron;

-- Schedule daily cleanup at 2 AM
SELECT cron.schedule(
    'cleanup-old-metrics',
    '0 2 * * *',
    $$ DO ... (cleanup logic) ... $$
);
```

### Partition-Wise Operations

**Partition Pruning** (automatic query optimization):

```sql
-- PostgreSQL automatically limits scan to relevant partitions
EXPLAIN SELECT * FROM metrics.pg_stat_activity
WHERE collected_at >= NOW() - INTERVAL '1 hour';

-- Output will show:
-- Seq Scan on pg_stat_activity_20250108
-- (Only the partition for 2025-01-08 is scanned)
```

**Ensure partition pruning works:**
1. WHERE clause must filter on partition key (collected_at)
2. Use simple comparisons (=, <, >, BETWEEN)
3. Avoid functions that prevent pruning: `WHERE DATE(collected_at) = '2025-01-08'`
   Use instead: `WHERE collected_at >= '2025-01-08' AND collected_at < '2025-01-09'`

## PostgreSQL Configuration

### Memory Settings

For a dedicated Workbench metadata database:

```ini
# Assuming 8GB RAM server, dedicated to Workbench

# Shared buffers: 25% of RAM
shared_buffers = 2GB

# Effective cache size: 50-75% of RAM (for query planner)
effective_cache_size = 6GB

# Work mem: For sorts and hash joins
# Formula: (Total RAM - shared_buffers) / max_connections / 4
# Example: (8GB - 2GB) / 100 / 4 = 15MB
work_mem = 16MB

# Maintenance work mem: For VACUUM, CREATE INDEX, etc.
# 5-10% of RAM
maintenance_work_mem = 512MB
```

**Rationale:**
- Workbench has moderate connection count (< 100 typical)
- Queries are mostly simple (point lookups, small joins)
- Some complex queries (recursive CTEs for authorization)
- VACUUM and INDEX operations need memory for metrics tables

### Connection Pooling

```ini
# Maximum connections
max_connections = 100

# Connection pooling (use pgBouncer or application-level pooling)
# For Workbench, application-level pooling is recommended
```

**Application Connection Pool** (pgx pool in Go):
```go
config.MaxConns = 25          // Maximum active connections
config.MinConns = 5           // Minimum idle connections
config.MaxConnIdleTime = 5 * time.Minute
config.MaxConnLifetime = 1 * time.Hour
```

### Autovacuum Tuning

```ini
# Autovacuum is CRITICAL for metrics tables
autovacuum = on

# More aggressive autovacuum for high-churn tables
autovacuum_vacuum_scale_factor = 0.1    # Vacuum when 10% of rows change
autovacuum_analyze_scale_factor = 0.05  # Analyze when 5% of rows change

# Increase worker count for parallel vacuuming
autovacuum_max_workers = 4

# Reduce vacuum cost delay for faster cleanup
autovacuum_vacuum_cost_delay = 10ms     # Default is 20ms
autovacuum_vacuum_cost_limit = 400      # Default is 200
```

**Per-Table Settings** (for very high-churn tables):
```sql
ALTER TABLE metrics.pg_stat_activity
SET (autovacuum_vacuum_scale_factor = 0.05);
```

### Checkpoint Tuning

```ini
# Balance between recovery time and I/O smoothing
checkpoint_timeout = 15min
max_wal_size = 2GB
min_wal_size = 512MB

# Spread checkpoints over time to reduce I/O spikes
checkpoint_completion_target = 0.9
```

**Monitoring Checkpoints:**
```sql
SELECT * FROM pg_stat_bgwriter;
```

If `checkpoints_req > checkpoints_timed`, increase `max_wal_size`.

### Write-Ahead Log (WAL)

```ini
# For metadata database, synchronous commit is recommended
synchronous_commit = on

# WAL level (minimal for standalone, replica for replication)
wal_level = replica

# Archive mode (if backing up with WAL archiving)
archive_mode = on
archive_command = 'cp %p /path/to/wal_archive/%f'
```

### Query Planner

```ini
# Cost settings (usually leave at defaults)
random_page_cost = 1.1      # For SSD storage (default 4.0 is for HDD)
effective_io_concurrency = 200  # For SSD (default 1)

# Join settings
from_collapse_limit = 12    # Default 8, increase for complex joins
join_collapse_limit = 12    # Default 8
```

## Monitoring and Observability

### Critical Metrics to Monitor

#### 1. Authentication Performance

**Query:**
```sql
SELECT query, mean_exec_time, calls, total_exec_time
FROM pg_stat_statements
WHERE query LIKE '%user_accounts%' OR query LIKE '%service_tokens%'
ORDER BY mean_exec_time DESC
LIMIT 10;
```

**Alert Threshold:**
- Mean execution time > 10ms: Investigate

**Common Causes:**
- Missing index on token_hash
- Table bloat
- Slow disk I/O

#### 2. Authorization Performance

**Query:**
```sql
SELECT query, mean_exec_time, calls
FROM pg_stat_statements
WHERE query LIKE '%WITH RECURSIVE%'
ORDER BY mean_exec_time DESC;
```

**Alert Threshold:**
- Mean execution time > 50ms: Review group hierarchy depth

**Optimization:**
- Consider caching group memberships in application
- Limit group nesting depth

#### 3. Connection Pool Utilization

**Query:**
```sql
SELECT count(*), state
FROM pg_stat_activity
WHERE application_name = 'ai-workbench-collector'
GROUP BY state;
```

**Alert Threshold:**
- Active connections > 80% of pool max: Scale up pool or investigate slow
  queries

#### 4. Metrics Insert Rate

**Query:**
```sql
SELECT schemaname, tablename,
       n_tup_ins - n_tup_del as net_inserts,
       n_tup_ins,
       n_tup_upd,
       n_tup_del
FROM pg_stat_user_tables
WHERE schemaname = 'metrics'
ORDER BY n_tup_ins DESC;
```

**Alert Threshold:**
- Insert rate drops to zero: Collector may be down

#### 5. Table and Index Bloat

**Query (simplified bloat estimation):**
```sql
SELECT schemaname, tablename,
       pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as
total_size,
       n_live_tup, n_dead_tup,
       ROUND(100.0 * n_dead_tup / NULLIF(n_live_tup + n_dead_tup, 0), 2) as
pct_dead
FROM pg_stat_user_tables
WHERE n_dead_tup > 1000
ORDER BY n_dead_tup DESC;
```

**Alert Threshold:**
- pct_dead > 20%: Autovacuum may be falling behind

**Remediation:**
```sql
-- Manual vacuum if needed
VACUUM ANALYZE metrics.pg_stat_activity;
```

#### 6. Partition Count

**Query:**
```sql
SELECT schemaname, tablename
FROM pg_tables
WHERE schemaname = 'metrics'
  AND tablename LIKE '%_2%'
ORDER BY tablename;
```

**Alert Threshold:**
- Missing future partitions: Risk of insert failures
- Too many old partitions: Cleanup not running

#### 7. Replication Lag (if using standby)

**Query (on primary):**
```sql
SELECT application_name,
       client_addr,
       state,
       sync_state,
       pg_wal_lsn_diff(pg_current_wal_lsn(), sent_lsn) as send_lag_bytes,
       pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn) as replay_lag_bytes,
       write_lag, flush_lag, replay_lag
FROM pg_stat_replication;
```

**Alert Threshold:**
- replay_lag > 10 seconds: Investigate network or standby performance

### Useful System Views

**pg_stat_activity** - Current session activity:
```sql
SELECT pid, usename, state, query_start, state_change, query
FROM pg_stat_activity
WHERE state != 'idle'
ORDER BY query_start;
```

**pg_stat_database** - Database-wide statistics:
```sql
SELECT datname,
       numbackends,
       xact_commit,
       xact_rollback,
       blks_read,
       blks_hit,
       ROUND(100.0 * blks_hit / NULLIF(blks_hit + blks_read, 0), 2) as
cache_hit_ratio
FROM pg_stat_database
WHERE datname = current_database();
```

**pg_locks** - Lock monitoring:
```sql
SELECT locktype, relation::regclass, mode, granted
FROM pg_locks
WHERE NOT granted;
```

## Backup and Recovery

### Backup Strategy

**Recommended Approach: Continuous Archiving + Point-in-Time Recovery (PITR)**

1. **Base Backup** (weekly):
```bash
pg_basebackup -h localhost -U postgres -D /backup/base -Ft -z -P
```

2. **WAL Archiving** (continuous):
```ini
# In postgresql.conf
archive_mode = on
archive_command = 'cp %p /backup/wal_archive/%f'
```

3. **Logical Backup** (daily, for easy table recovery):
```bash
pg_dump -Fc -f /backup/logical/ai_workbench_$(date +%Y%m%d).dump
ai_workbench
```

### Recovery Scenarios

**Scenario 1: Restore Entire Database to Specific Time**
```bash
# Stop PostgreSQL
systemctl stop postgresql

# Restore base backup
tar -xzf /backup/base/base.tar.gz -C /var/lib/postgresql/data

# Create recovery.conf (or recovery.signal in PG 12+)
cat > /var/lib/postgresql/data/recovery.signal <<EOF
restore_command = 'cp /backup/wal_archive/%f %p'
recovery_target_time = '2025-01-08 10:00:00'
EOF

# Start PostgreSQL (recovery will happen automatically)
systemctl start postgresql
```

**Scenario 2: Restore Single Table**
```bash
# Extract single table from logical backup
pg_restore -t user_accounts -d ai_workbench /backup/logical/ai_workbench_20250108.dump
```

### Disaster Recovery Testing

**Test restore procedure quarterly:**
1. Restore to a test server
2. Verify data integrity
3. Measure recovery time objective (RTO)
4. Measure recovery point objective (RPO)

**Typical Targets:**
- RTO: < 1 hour (time to restore from backup)
- RPO: < 15 minutes (acceptable data loss window)

## Performance Tuning Checklist

### Pre-Production

- [ ] Run EXPLAIN ANALYZE on all critical queries
- [ ] Verify all foreign keys have indexes
- [ ] Configure connection pooling
- [ ] Set appropriate memory parameters (shared_buffers, work_mem)
- [ ] Enable pg_stat_statements extension
- [ ] Configure autovacuum appropriately
- [ ] Create partitions for metrics tables
- [ ] Set up monitoring (metrics collection)
- [ ] Test backup and restore procedures

### Post-Deployment

- [ ] Monitor authentication query performance (< 10ms target)
- [ ] Monitor authorization query performance (< 50ms target)
- [ ] Check table bloat weekly
- [ ] Review slow queries in pg_stat_statements
- [ ] Verify autovacuum is running on metrics tables
- [ ] Monitor partition creation and cleanup
- [ ] Check connection pool utilization
- [ ] Review checkpoint and WAL statistics

### Ongoing Maintenance

- [ ] Analyze slow query reports monthly
- [ ] Review and update indexes quarterly
- [ ] Test restore procedures quarterly
- [ ] Capacity planning: Monitor disk growth
- [ ] Vacuum full on heavily bloated tables (during maintenance window)
- [ ] Update PostgreSQL minor version regularly

## Troubleshooting Common Issues

### Slow Authentication

**Symptom:** Login takes > 1 second

**Diagnosis:**
```sql
EXPLAIN ANALYZE
SELECT * FROM user_accounts WHERE username = 'testuser';
```

**Common Causes:**
1. Missing index on username
2. Table bloat (run VACUUM)
3. Slow disk I/O

**Fix:**
```sql
VACUUM ANALYZE user_accounts;
REINDEX TABLE user_accounts;
```

### Slow Authorization Checks

**Symptom:** Authorization queries take > 100ms

**Diagnosis:**
```sql
EXPLAIN ANALYZE
WITH RECURSIVE ... (authorization query);
```

**Common Causes:**
1. Deep group hierarchy (> 10 levels)
2. Missing indexes on group_memberships
3. No index on foreign keys

**Fix:**
- Limit group nesting depth
- Cache group membership results in application

### Metrics Insert Failures

**Symptom:** Collector errors on metric inserts

**Diagnosis:**
```sql
-- Check if partition exists
SELECT * FROM pg_tables
WHERE schemaname = 'metrics'
  AND tablename = 'pg_stat_activity_' || to_char(NOW(), 'YYYYMMDD');
```

**Common Causes:**
1. Missing partition for current date
2. Disk full

**Fix:**
```sql
-- Create missing partition
CREATE TABLE metrics.pg_stat_activity_20250108
PARTITION OF metrics.pg_stat_activity
FOR VALUES FROM ('2025-01-08') TO ('2025-01-09');
```

### High Disk Usage

**Symptom:** Disk usage growing faster than expected

**Diagnosis:**
```sql
SELECT schemaname, tablename,
       pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename))
FROM pg_tables
WHERE schemaname IN ('public', 'metrics')
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;
```

**Common Causes:**
1. Old metrics partitions not being cleaned up
2. Table bloat from autovacuum not keeping up

**Fix:**
1. Drop old partitions
2. Run VACUUM FULL during maintenance window (locks table)
