# Database Schema

This document describes the database schema used by the Collector to store
configuration and metrics data.

## Schema Overview

The Collector uses two PostgreSQL schemas:

- The public schema contains core configuration tables.
- The metrics schema contains time-series metrics tables (partitioned).

For detailed information about schema migrations, see [Schema
Management](schema-management.md).

## Core Tables (public schema)

### schema_version

Tracks applied database migrations.

```sql
CREATE TABLE schema_version (
    version INTEGER PRIMARY KEY,
    description TEXT NOT NULL,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Columns:**

- `version` - Migration version number (sequential)
- `description` - Human-readable description
- `applied_at` - When the migration was applied

### connections

Stores information about PostgreSQL servers to monitor.

```sql
CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
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
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_port CHECK (port >= 1 AND port <= 65535),
    CONSTRAINT chk_owner_token CHECK (
        (is_shared = TRUE) OR 
        (is_shared = FALSE AND owner_token IS NOT NULL)
    )
);
```

**Key Columns:**

- `id` - Unique connection identifier
- `name` - Display name for the connection
- `host` / `hostaddr` - Server location
- `port` - PostgreSQL port (default 5432)
- `database_name` - Database to connect to
- `username` - Username for connection
- `password_encrypted` - AES-256-GCM encrypted password
- `is_shared` - If true, shared across all users
- `is_monitored` - If true, actively monitored
- `owner_username` / `owner_token` - Ownership information

**Indexes:**

- Primary key on `id`
- Index on `owner_token` for fast ownership lookups
- Partial index on `is_monitored = TRUE` for active connections
- Index on `name` for lookups by name

### probe_configs

Defines monitoring probes and their configuration. Supports both global
defaults and per-connection overrides.

```sql
CREATE TABLE probe_configs (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    collection_interval_seconds INTEGER NOT NULL DEFAULT 60,
    retention_days INTEGER NOT NULL DEFAULT 28,
    is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_name_not_empty CHECK (name <> ''),
    CONSTRAINT chk_collection_interval_positive CHECK (collection_interval_seconds > 0),
    CONSTRAINT chk_retention_days_positive CHECK (retention_days > 0)
);
```

**Key Columns:**

- `id` - Unique probe configuration identifier
- `name` - Probe name (matches pg_stat_* view name)
- `description` - Human-readable description
- `collection_interval_seconds` - How often to run (in seconds)
- `retention_days` - How long to keep data (in days)
- `is_enabled` - If false, probe won't run
- `connection_id` - Connection ID for per-server config. NULL means global
  default

**Configuration Hierarchy:**

The Collector uses a fallback hierarchy for probe settings:

1. Per-server config applies if a probe_configs row exists with the specific
   `connection_id`; use those settings.
2. Global default applies if no per-server config exists; use the probe_configs
   row where `connection_id IS NULL`.
3. Hardcoded default applies if no database config exists; use built-in
   defaults.

**Indexes:**

- Primary key on `id`
- Unique composite index on `(name, COALESCE(connection_id, 0))` - Allows one
  config per probe per connection
- Partial unique index on `name WHERE connection_id IS NULL` - Ensures only
  one global default per probe
- Partial index on `is_enabled = TRUE` for fast enabled probe lookups

## Metrics Tables (metrics schema)

Each probe has its own metrics table, partitioned by week.

### Common Structure

All metrics tables share common columns:

```sql
CREATE TABLE metrics.{probe_name} (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMP NOT NULL,
    -- probe-specific columns --
) PARTITION BY RANGE (collected_at);
```

**Common Columns:**

- `connection_id` - References `connections.id`
- `collected_at` - Timestamp of metric collection

### Partitioning

**Strategy**: Range partitioning by `collected_at`

**Interval**: Weekly (Monday to Sunday)

**Example Partitions:**

```sql
-- Week of Nov 4, 2025
CREATE TABLE metrics.pg_stat_activity_20251104
    PARTITION OF metrics.pg_stat_activity
    FOR VALUES FROM ('2025-11-04 00:00:00') TO ('2025-11-11 00:00:00');

-- Week of Nov 11, 2025  
CREATE TABLE metrics.pg_stat_activity_20251111
    PARTITION OF metrics.pg_stat_activity
    FOR VALUES FROM ('2025-11-11 00:00:00') TO ('2025-11-18 00:00:00');
```

### Example: pg_stat_activity

```sql
CREATE TABLE metrics.pg_stat_activity (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMP NOT NULL,
    datid OID,
    datname TEXT,
    pid INTEGER,
    leader_pid INTEGER,
    usesysid OID,
    usename TEXT,
    application_name TEXT,
    client_addr INET,
    client_hostname TEXT,
    client_port INTEGER,
    backend_start TIMESTAMP,
    xact_start TIMESTAMP,
    query_start TIMESTAMP,
    state_change TIMESTAMP,
    wait_event_type TEXT,
    wait_event TEXT,
    state TEXT,
    backend_xid TEXT,
    backend_xmin TEXT,
    query TEXT,
    backend_type TEXT
) PARTITION BY RANGE (collected_at);
```

**Columns** map directly to `pg_stat_activity` view columns, plus:

- `connection_id` - Which server this data came from
- `collected_at` - When it was collected

### Example: pg_stat_database

```sql
CREATE TABLE metrics.pg_stat_database (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMP NOT NULL,
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
    checksum_last_failure TIMESTAMP,
    blk_read_time DOUBLE PRECISION,
    blk_write_time DOUBLE PRECISION,
    session_time DOUBLE PRECISION,
    active_time DOUBLE PRECISION,
    idle_in_transaction_time DOUBLE PRECISION,
    sessions BIGINT,
    sessions_abandoned BIGINT,
    sessions_fatal BIGINT,
    sessions_killed BIGINT,
    stats_reset TIMESTAMP
) PARTITION BY RANGE (collected_at);
```

## Schema Design Principles

### Normalization

**Connections table** stores server information once, referenced by
connection_id in metrics.

**Probes table** stores probe configuration once, used to control collection
and retention.

### Partitioning Benefits

1. Query performance improves because queries filtering by time only scan
   relevant partitions.
2. Data management is simpler because you can drop old partitions instead of
   running DELETE operations.
3. Index maintenance is easier because smaller indexes per partition reduce
   overhead.
4. Parallel operations are possible because PostgreSQL can operate on
   partitions in parallel.

### Storage Optimization

- Partition by week to balance partition count and partition size.
- No indexes on metrics tables; rely on partition pruning (indexes can be
  added if needed).
- Use efficient data types; use appropriate types (INTEGER vs BIGINT, TEXT
  vs VARCHAR).

## Data Retention

### Automatic Cleanup

The garbage collector:

1. Runs daily (first run after 5 minutes)
2. For each probe:
   - Calculate cutoff: `NOW() - retention_days`
   - Find partitions entirely before cutoff
   - `DROP TABLE` those partitions

### Manual Cleanup

To manually drop a partition:

```sql
-- Check existing partitions
SELECT schemaname, tablename
FROM pg_tables
WHERE schemaname = 'metrics'
  AND tablename LIKE 'pg_stat_activity_%'
ORDER BY tablename;

-- Drop specific partition
DROP TABLE metrics.pg_stat_activity_20251104;
```

### Adjusting Retention

Update the `probe_configs` table:

```sql
-- Update global default retention for all connections
UPDATE probe_configs
SET retention_days = 30
WHERE name = 'pg_stat_activity'
  AND connection_id IS NULL;

-- Or set retention for a specific connection
UPDATE probe_configs
SET retention_days = 30
WHERE name = 'pg_stat_activity'
  AND connection_id = 1;
```

Changes take effect on the next garbage collection run.

## Querying Metrics

### Simple Query

Get recent activity data:

```sql
SELECT connection_id, collected_at, datname, state, COUNT(*)
FROM metrics.pg_stat_activity
WHERE collected_at > NOW() - INTERVAL '1 hour'
GROUP BY connection_id, collected_at, datname, state
ORDER BY collected_at DESC;
```

### Join with Connection

Get activity with connection names:

```sql
SELECT 
    c.name AS server_name,
    a.collected_at,
    a.datname,
    a.state,
    COUNT(*) AS count
FROM metrics.pg_stat_activity a
JOIN connections c ON a.connection_id = c.id
WHERE a.collected_at > NOW() - INTERVAL '1 hour'
GROUP BY c.name, a.collected_at, a.datname, a.state
ORDER BY a.collected_at DESC;
```

### Time-Series Analysis

Get active connections over time:

```sql
SELECT 
    c.name AS server_name,
    DATE_TRUNC('minute', a.collected_at) AS minute,
    COUNT(*) AS active_connections
FROM metrics.pg_stat_activity a
JOIN connections c ON a.connection_id = c.id
WHERE a.collected_at > NOW() - INTERVAL '24 hours'
  AND a.state = 'active'
GROUP BY c.name, DATE_TRUNC('minute', a.collected_at)
ORDER BY minute DESC, server_name;
```

### Partition Information

Check partition sizes:

```sql
SELECT
    schemaname || '.' || tablename AS partition,
    pg_size_pretty(pg_total_relation_size(schemaname || '.' || tablename)) AS size,
    pg_total_relation_size(schemaname || '.' || tablename) AS size_bytes
FROM pg_tables
WHERE schemaname = 'metrics'
  AND tablename LIKE 'pg_stat_activity_%'
ORDER BY size_bytes DESC;
```

## Schema Maintenance

### Vacuum

PostgreSQL's autovacuum handles most maintenance, but you can manually
vacuum:

```sql
-- Vacuum specific partition
VACUUM ANALYZE metrics.pg_stat_activity_20251104;

-- Vacuum all partitions of a table
VACUUM ANALYZE metrics.pg_stat_activity;
```

### Analyze

Update statistics for query planning:

```sql
ANALYZE metrics.pg_stat_activity;
```

### Reindex

If indexes become bloated (generally not needed):

```sql
REINDEX TABLE metrics.pg_stat_activity;
```

## See Also

- [Schema Management](schema-management.md) - Migration system details
- [Probes](probes.md) - How probes collect and store data
- [Architecture](architecture.md) - Overall system design
