# PostgreSQL Database Quick Reference Card

Fast lookup guide for common database operations and queries.

## Connection Information

```bash
# Default connection (adjust for your environment)
psql -h localhost -U ai_workbench -d ai_workbench

# Check version
SELECT version();
```

## Schema Version

```sql
-- Check current migration version
SELECT version, description, applied_at
FROM schema_version
ORDER BY version DESC;

-- Current version should be: 6
```

## Core Tables Summary

| Table | Purpose | Key Columns |
|-------|---------|-------------|
| user_accounts | User authentication | id, username, email, is_superuser |
| service_tokens | Service authentication | id, name, token_hash, is_superuser |
| user_tokens | Personal access tokens | id, user_id, token_hash, expires_at |
| user_sessions | Session management | session_token, username, expires_at |
| connections | Database connections | id, owner_username/owner_token, is_monitored |
| probe_configs | Monitoring configuration | id, connection_id, name, is_enabled |
| user_groups | RBAC groups | id, name, description |
| group_memberships | Group hierarchy | parent_group_id, member_user_id/member_group_id |

## Common Queries

### Authentication

```sql
-- Find user by username
SELECT id, username, email, is_superuser
FROM user_accounts
WHERE username = 'alice';

-- Check if token exists and is valid
SELECT user_id, expires_at
FROM user_tokens
WHERE token_hash = 'hash_value'
  AND expires_at > NOW();

-- List active sessions
SELECT username, created_at, expires_at, last_used_at
FROM user_sessions
WHERE expires_at > NOW()
ORDER BY last_used_at DESC;
```

### Connections

```sql
-- List all connections with owners
SELECT id, name, host, port,
       COALESCE(owner_username, owner_token) as owner,
       is_shared, is_monitored
FROM connections
ORDER BY name;

-- Find connections by owner
SELECT * FROM connections
WHERE owner_username = 'alice' OR owner_token = 'service-token';

-- List monitored connections
SELECT name, host, port, database_name
FROM connections
WHERE is_monitored = TRUE;
```

### Groups and Privileges

```sql
-- List all groups
SELECT id, name, description, created_at
FROM user_groups
ORDER BY name;

-- Find user's direct group memberships
SELECT ug.name, ug.description
FROM group_memberships gm
JOIN user_groups ug ON gm.parent_group_id = ug.id
WHERE gm.member_user_id = 5;  -- Replace with user_id

-- Find all groups user belongs to (including hierarchy)
WITH RECURSIVE user_groups AS (
    -- Direct memberships
    SELECT gm.parent_group_id, ug.name
    FROM group_memberships gm
    JOIN user_groups ug ON gm.parent_group_id = ug.id
    WHERE gm.member_user_id = 5
    UNION
    -- Recursive: parent groups
    SELECT gm.parent_group_id, ug.name
    FROM group_memberships gm
    JOIN user_groups_recursive ugr ON gm.member_group_id = ugr.parent_group_id
    JOIN user_groups ug ON gm.parent_group_id = ug.id
)
SELECT DISTINCT name FROM user_groups ORDER BY name;

-- List group's MCP privileges
SELECT p.identifier, p.item_type, p.description
FROM group_mcp_privileges gmp
JOIN mcp_privilege_identifiers p ON gmp.privilege_id = p.id
WHERE gmp.group_id = 1
ORDER BY p.identifier;

-- List group's connection privileges
SELECT c.name, gcp.access_level
FROM group_connection_privileges gcp
JOIN connections c ON gcp.connection_id = c.id
WHERE gcp.group_id = 1
ORDER BY c.name;
```

### Metrics

```sql
-- Check recent metrics collection
SELECT connection_id, COUNT(*), MAX(collected_at) as last_collected
FROM metrics.pg_stat_activity
GROUP BY connection_id
ORDER BY last_collected DESC;

-- Query metrics for specific connection and time range
SELECT * FROM metrics.pg_stat_database
WHERE connection_id = 1
  AND collected_at >= NOW() - INTERVAL '1 hour'
ORDER BY collected_at DESC;

-- List all metrics tables
SELECT tablename
FROM pg_tables
WHERE schemaname = 'metrics'
ORDER BY tablename;
```

## Monitoring Queries

### Database Performance

```sql
-- Cache hit ratio (should be > 95%)
SELECT
    SUM(blks_hit) / NULLIF(SUM(blks_hit + blks_read), 0) * 100 as cache_hit_ratio
FROM pg_stat_database
WHERE datname = current_database();

-- Active connections
SELECT COUNT(*) as active_connections,
       COUNT(*) FILTER (WHERE state = 'active') as running_queries,
       COUNT(*) FILTER (WHERE state = 'idle') as idle_connections
FROM pg_stat_activity;

-- Long-running queries
SELECT pid, usename, state, now() - query_start as duration, query
FROM pg_stat_activity
WHERE state != 'idle'
  AND now() - query_start > INTERVAL '1 minute'
ORDER BY duration DESC;
```

### Table Statistics

```sql
-- Table sizes
SELECT schemaname, tablename,
       pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as
total_size,
       pg_size_pretty(pg_relation_size(schemaname||'.'||tablename)) as
table_size,
       pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename) -
                      pg_relation_size(schemaname||'.'||tablename)) as
index_size
FROM pg_tables
WHERE schemaname IN ('public', 'metrics')
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC
LIMIT 20;

-- Table bloat (dead tuples)
SELECT schemaname, tablename,
       n_live_tup, n_dead_tup,
       ROUND(100.0 * n_dead_tup / NULLIF(n_live_tup + n_dead_tup, 0), 2) as
pct_dead,
       last_autovacuum, last_vacuum
FROM pg_stat_user_tables
WHERE n_dead_tup > 0
ORDER BY n_dead_tup DESC
LIMIT 20;

-- Index usage
SELECT schemaname, tablename, indexname,
       idx_scan as scans,
       idx_tup_read as tuples_read,
       idx_tup_fetch as tuples_fetched,
       pg_size_pretty(pg_relation_size(indexrelid)) as index_size
FROM pg_stat_user_indexes
ORDER BY idx_scan ASC, pg_relation_size(indexrelid) DESC
LIMIT 20;
```

### Vacuum and Analyze

```sql
-- Recent vacuum/analyze operations
SELECT schemaname, tablename,
       last_vacuum, last_autovacuum,
       last_analyze, last_autoanalyze,
       vacuum_count, autovacuum_count,
       analyze_count, autoanalyze_count
FROM pg_stat_user_tables
ORDER BY GREATEST(last_vacuum, last_autovacuum,
                  last_analyze, last_autoanalyze) DESC NULLS LAST;
```

## Maintenance Operations

### Manual Vacuum

```sql
-- Vacuum specific table
VACUUM ANALYZE user_accounts;

-- Vacuum metrics table
VACUUM ANALYZE metrics.pg_stat_activity;

-- Full vacuum (locks table, use during maintenance window)
VACUUM FULL user_accounts;
```

### Index Maintenance

```sql
-- Rebuild bloated index (no lock with CONCURRENTLY)
REINDEX INDEX CONCURRENTLY idx_user_accounts_username;

-- Rebuild all indexes on table
REINDEX TABLE CONCURRENTLY user_accounts;
```

### Partition Management

```sql
-- List all partitions for a table
SELECT schemaname, tablename
FROM pg_tables
WHERE schemaname = 'metrics'
  AND tablename LIKE 'pg_stat_activity_%'
ORDER BY tablename;

-- Create new partition for tomorrow
DO $$
DECLARE
    part_date DATE := CURRENT_DATE + INTERVAL '1 day';
    part_name TEXT := 'pg_stat_activity_' || to_char(part_date, 'YYYYMMDD');
BEGIN
    EXECUTE format(
        'CREATE TABLE IF NOT EXISTS metrics.%I
         PARTITION OF metrics.pg_stat_activity
         FOR VALUES FROM (%L) TO (%L)',
        part_name,
        part_date,
        part_date + INTERVAL '1 day'
    );
END $$;

-- Drop old partition (7 days ago)
DO $$
DECLARE
    part_date DATE := CURRENT_DATE - INTERVAL '7 days';
    part_name TEXT := 'pg_stat_activity_' || to_char(part_date, 'YYYYMMDD');
BEGIN
    EXECUTE 'DROP TABLE IF EXISTS metrics.' || part_name;
    RAISE NOTICE 'Dropped partition: %', part_name;
END $$;
```

## User and Token Management

### Create User

```sql
-- Insert new user
INSERT INTO user_accounts (username, email, full_name, password_hash,
is_superuser)
VALUES ('newuser', 'new@example.com', 'New User', 'sha256_hash', FALSE)
RETURNING id, username;
```

### Create Service Token

```sql
-- Insert new service token
INSERT INTO service_tokens (name, token_hash, is_superuser, note)
VALUES ('monitoring-agent', 'sha256_hash', FALSE, 'Monitoring agent token')
RETURNING id, name;
```

### Create User Token

```sql
-- Create personal access token
INSERT INTO user_tokens (user_id, token_hash, expires_at)
VALUES (5, 'sha256_hash', NOW() + INTERVAL '30 days')
RETURNING id, token_hash, expires_at;
```

### Grant Privileges

```sql
-- Add user to group
INSERT INTO group_memberships (parent_group_id, member_user_id)
VALUES (1, 5);

-- Grant MCP privilege to group
INSERT INTO group_mcp_privileges (group_id, privilege_id)
SELECT 1, id FROM mcp_privilege_identifiers
WHERE identifier = 'create_user';

-- Grant connection access to group
INSERT INTO group_connection_privileges (group_id, connection_id, access_level)
VALUES (1, 5, 'read_write');
```

## Backup and Restore

### Backup

```bash
# Logical backup (entire database)
pg_dump -Fc -f ai_workbench_backup.dump ai_workbench

# Logical backup (specific tables)
pg_dump -Fc -t user_accounts -t connections -f core_tables.dump ai_workbench

# Plain SQL backup
pg_dump -f ai_workbench_backup.sql ai_workbench
```

### Restore

```bash
# Restore entire database
pg_restore -d ai_workbench ai_workbench_backup.dump

# Restore specific table
pg_restore -d ai_workbench -t user_accounts ai_workbench_backup.dump

# Restore from SQL file
psql -d ai_workbench -f ai_workbench_backup.sql
```

## Performance Analysis

### Query Performance

```sql
-- Enable pg_stat_statements if not already enabled
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- Top 10 slowest queries by average time
SELECT
    LEFT(query, 60) as query_snippet,
    calls,
    ROUND(mean_exec_time::numeric, 2) as avg_ms,
    ROUND(total_exec_time::numeric, 2) as total_ms
FROM pg_stat_statements
WHERE query NOT LIKE '%pg_stat_statements%'
ORDER BY mean_exec_time DESC
LIMIT 10;

-- Most frequently called queries
SELECT
    LEFT(query, 60) as query_snippet,
    calls,
    ROUND(mean_exec_time::numeric, 2) as avg_ms
FROM pg_stat_statements
WHERE query NOT LIKE '%pg_stat_statements%'
ORDER BY calls DESC
LIMIT 10;

-- Queries consuming most total time
SELECT
    LEFT(query, 60) as query_snippet,
    calls,
    ROUND(total_exec_time::numeric, 2) as total_ms,
    ROUND(mean_exec_time::numeric, 2) as avg_ms
FROM pg_stat_statements
WHERE query NOT LIKE '%pg_stat_statements%'
ORDER BY total_exec_time DESC
LIMIT 10;
```

### EXPLAIN Analysis

```sql
-- Analyze query plan
EXPLAIN (ANALYZE, BUFFERS)
SELECT * FROM user_accounts WHERE username = 'alice';

-- Check if index is used
EXPLAIN SELECT * FROM connections WHERE owner_username = 'alice';
-- Look for "Index Scan" not "Seq Scan"
```

## Configuration Check

```sql
-- View important settings
SELECT name, setting, unit, context
FROM pg_settings
WHERE name IN (
    'shared_buffers',
    'effective_cache_size',
    'work_mem',
    'maintenance_work_mem',
    'max_connections',
    'autovacuum',
    'checkpoint_timeout',
    'max_wal_size'
)
ORDER BY name;

-- Check autovacuum settings
SELECT name, setting
FROM pg_settings
WHERE name LIKE 'autovacuum%'
ORDER BY name;
```

## Emergency Operations

### Kill Long-Running Query

```sql
-- Find problematic query
SELECT pid, usename, state, now() - query_start as duration, query
FROM pg_stat_activity
WHERE state = 'active'
ORDER BY duration DESC;

-- Terminate specific query (graceful)
SELECT pg_cancel_backend(12345);  -- Replace with actual PID

-- Force terminate (less graceful)
SELECT pg_terminate_backend(12345);
```

### Check Locks

```sql
-- View current locks
SELECT
    locktype, relation::regclass, mode, granted,
    pid, usename, query
FROM pg_locks
LEFT JOIN pg_stat_activity USING (pid)
WHERE NOT granted
ORDER BY relation;

-- Find blocking queries
SELECT
    blocked_locks.pid AS blocked_pid,
    blocked_activity.query AS blocked_query,
    blocking_locks.pid AS blocking_pid,
    blocking_activity.query AS blocking_query
FROM pg_locks blocked_locks
JOIN pg_stat_activity blocked_activity ON blocked_activity.pid =
blocked_locks.pid
JOIN pg_locks blocking_locks ON blocking_locks.locktype = blocked_locks.locktype
    AND blocking_locks.database IS NOT DISTINCT FROM blocked_locks.database
    AND blocking_locks.relation IS NOT DISTINCT FROM blocked_locks.relation
    AND blocking_locks.page IS NOT DISTINCT FROM blocked_locks.page
    AND blocking_locks.tuple IS NOT DISTINCT FROM blocked_locks.tuple
    AND blocking_locks.virtualxid IS NOT DISTINCT FROM blocked_locks.virtualxid
    AND blocking_locks.transactionid IS NOT DISTINCT FROM
blocked_locks.transactionid
    AND blocking_locks.classid IS NOT DISTINCT FROM blocked_locks.classid
    AND blocking_locks.objid IS NOT DISTINCT FROM blocked_locks.objid
    AND blocking_locks.objsubid IS NOT DISTINCT FROM blocked_locks.objsubid
    AND blocking_locks.pid != blocked_locks.pid
JOIN pg_stat_activity blocking_activity ON blocking_activity.pid =
blocking_locks.pid
WHERE NOT blocked_locks.granted;
```

### Disk Space

```sql
-- Database size
SELECT pg_size_pretty(pg_database_size(current_database())) as database_size;

-- Check WAL directory size (from OS)
-- du -sh $PGDATA/pg_wal

-- Largest tables
SELECT
    schemaname || '.' || tablename as table_name,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as size
FROM pg_tables
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC
LIMIT 10;
```

## See Full Documentation

For detailed information, see:
- `README.md` - Complete guide and index
- `schema-overview.md` - Architecture overview
- `migration-history.md` - Migration details
- `privilege-system.md` - RBAC system
- `performance-notes.md` - Performance tuning
- `relationships.md` - Entity relationships

---
**Location:** `/Users/dpage/git/ai-workbench/.claude/postgres-expert/`
**Last Updated:** 2025-01-08
