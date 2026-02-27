# Database Schema

This document describes the database schema that the
Collector uses to store configuration and metrics data.

## Schema Overview

The Collector uses two PostgreSQL schemas:

- The public schema contains core configuration tables.
- The metrics schema contains time-series metrics
  tables that use partitioning.

For detailed information about schema migrations, see
[Schema Management](schema-management.md).

## Core Tables (public schema)

The public schema contains the tables that store
system configuration and connection information.

### schema_version

The `schema_version` table tracks applied database
migrations.

```sql
CREATE TABLE schema_version (
    version INTEGER PRIMARY KEY,
    description TEXT NOT NULL,
    applied_at TIMESTAMP NOT NULL
        DEFAULT CURRENT_TIMESTAMP
);
```

The table contains the following columns:

- `version` stores the migration version number in
  sequential order.
- `description` stores a human-readable description
  of the migration.
- `applied_at` stores the timestamp when the migration
  was applied.

### connections

The `connections` table stores information about
PostgreSQL servers to monitor.

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
    created_at TIMESTAMP NOT NULL
        DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL
        DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_port CHECK (
        port >= 1 AND port <= 65535
    ),
    CONSTRAINT chk_owner_token CHECK (
        (is_shared = TRUE) OR
        (is_shared = FALSE
         AND owner_token IS NOT NULL)
    )
);
```

The table contains the following key columns:

- `id` is the unique connection identifier.
- `name` is the display name for the connection.
- `host` and `hostaddr` specify the server location.
- `port` is the PostgreSQL port, which defaults to
  5432.
- `database_name` is the database to connect to.
- `username` is the username for the connection.
- `password_encrypted` stores the AES-256-GCM
  encrypted password.
- `is_shared` indicates whether the connection is
  shared across all users.
- `is_monitored` indicates whether the connection is
  actively monitored.
- `owner_username` and `owner_token` store ownership
  information.

The table has the following indexes:

- A primary key exists on `id`.
- An index on `owner_token` supports fast ownership
  lookups.
- A partial index on `is_monitored = TRUE` supports
  active connection queries.
- An index on `name` supports lookups by name.

### probe_configs

The `probe_configs` table defines monitoring probes
and their configuration. The table supports both global
defaults and per-connection overrides.

```sql
CREATE TABLE probe_configs (
    id INTEGER GENERATED ALWAYS AS IDENTITY
        PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    collection_interval_seconds INTEGER NOT NULL
        DEFAULT 60,
    retention_days INTEGER NOT NULL DEFAULT 28,
    is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    connection_id INTEGER
        REFERENCES connections(id)
        ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL
        DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL
        DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_name_not_empty
        CHECK (name <> ''),
    CONSTRAINT chk_collection_interval_positive
        CHECK (collection_interval_seconds > 0),
    CONSTRAINT chk_retention_days_positive
        CHECK (retention_days > 0)
);
```

The table contains the following key columns:

- `id` is the unique probe configuration identifier.
- `name` is the probe name, which matches the
  `pg_stat_*` view name.
- `description` is a human-readable description.
- `collection_interval_seconds` defines how often
  the probe runs in seconds.
- `retention_days` defines how long the system
  retains data in days.
- `is_enabled` controls whether the probe runs.
- `connection_id` references the connection for
  per-server configuration; a NULL value indicates
  a global default.

#### Configuration Hierarchy

The Collector uses a fallback hierarchy for probe
settings:

1. Per-server configuration applies if a
   `probe_configs` row exists with the specific
   `connection_id`.
2. Global default applies if no per-server
   configuration exists; the system uses the
   `probe_configs` row where `connection_id IS NULL`.
3. Hardcoded default applies if no database
   configuration exists.

The table has the following indexes:

- A primary key exists on `id`.
- A composite unique index on
  `(name, COALESCE(connection_id, 0))` allows one
  configuration per probe per connection.
- A partial unique index on
  `name WHERE connection_id IS NULL` ensures only
  one global default per probe.
- A partial index on `is_enabled = TRUE` supports
  fast enabled probe lookups.

## Metrics Tables (metrics schema)

Each probe has its own metrics table, partitioned
by week. This section describes the common structure
and partitioning strategy.

### Common Structure

All metrics tables share common columns for
identifying the source connection and collection
time.

```sql
CREATE TABLE metrics.{probe_name} (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMP NOT NULL,
    -- probe-specific columns --
) PARTITION BY RANGE (collected_at);
```

The common columns are:

- `connection_id` references `connections.id`.
- `collected_at` stores the timestamp of metric
  collection.

### Partitioning

The system uses range partitioning by `collected_at`
with a weekly interval from Monday to Sunday.

The following example shows partition creation:

```sql
-- Week of Nov 4, 2025
CREATE TABLE metrics.pg_stat_activity_20251104
    PARTITION OF metrics.pg_stat_activity
    FOR VALUES FROM ('2025-11-04 00:00:00')
    TO ('2025-11-11 00:00:00');

-- Week of Nov 11, 2025
CREATE TABLE metrics.pg_stat_activity_20251111
    PARTITION OF metrics.pg_stat_activity
    FOR VALUES FROM ('2025-11-11 00:00:00')
    TO ('2025-11-18 00:00:00');
```

### Example: pg_stat_activity

The following example shows the `pg_stat_activity`
metrics table definition:

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

The columns map directly to `pg_stat_activity` view
columns, plus `connection_id` and `collected_at`.

### Example: pg_stat_database

The following example shows the `pg_stat_database`
metrics table definition:

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

The schema follows several design principles for
reliability and performance.

### Normalization

The `connections` table stores server information
once; metrics reference the connection by
`connection_id`. The `probe_configs` table stores
probe configuration once; the system uses the
configuration to control collection and retention.

### Partitioning Benefits

Partitioning provides the following advantages:

- Query performance improves because queries filtering
  by time scan only relevant partitions.
- Data management is simpler because dropping old
  partitions replaces DELETE operations.
- Index maintenance is easier because smaller indexes
  per partition reduce overhead.
- Parallel operations are possible because PostgreSQL
  can operate on partitions in parallel.

### Storage Optimization

The system optimizes storage in several ways:

- The system partitions by week to balance partition
  count and partition size.
- Metrics tables have no indexes by default; the
  system relies on partition pruning.
- The system uses efficient data types such as
  INTEGER versus BIGINT and TEXT versus VARCHAR.

## Data Retention

The garbage collector manages data retention
automatically.

### Automatic Cleanup

The garbage collector performs the following steps:

1. The collector runs daily, with the first run
   after 5 minutes.
2. For each probe, the collector calculates the cutoff
   as `NOW() - retention_days`.
3. The collector finds partitions entirely before the
   cutoff date.
4. The collector executes `DROP TABLE` on expired
   partitions.

### Manual Cleanup

To manually drop a partition, first check existing
partitions and then drop the target partition.

In the following example, the query lists all
partitions for `pg_stat_activity`:

```sql
SELECT schemaname, tablename
FROM pg_tables
WHERE schemaname = 'metrics'
  AND tablename LIKE 'pg_stat_activity_%'
ORDER BY tablename;
```

In the following example, the command drops a
specific partition:

```sql
DROP TABLE metrics.pg_stat_activity_20251104;
```

### Adjusting Retention

Update the `probe_configs` table to change retention
settings. In the following example, the command
updates the global default retention:

```sql
UPDATE probe_configs
SET retention_days = 30
WHERE name = 'pg_stat_activity'
  AND connection_id IS NULL;
```

In the following example, the command sets retention
for a specific connection:

```sql
UPDATE probe_configs
SET retention_days = 30
WHERE name = 'pg_stat_activity'
  AND connection_id = 1;
```

Changes take effect on the next garbage collection
run.

## Querying Metrics

This section provides examples for querying the
metrics tables.

### Simple Query

In the following example, the query retrieves recent
activity data:

```sql
SELECT connection_id, collected_at,
       datname, state, COUNT(*)
FROM metrics.pg_stat_activity
WHERE collected_at > NOW() - INTERVAL '1 hour'
GROUP BY connection_id, collected_at,
         datname, state
ORDER BY collected_at DESC;
```

### Join with Connection

In the following example, the query retrieves activity
data with connection names:

```sql
SELECT
    c.name AS server_name,
    a.collected_at,
    a.datname,
    a.state,
    COUNT(*) AS count
FROM metrics.pg_stat_activity a
JOIN connections c ON a.connection_id = c.id
WHERE a.collected_at > NOW()
    - INTERVAL '1 hour'
GROUP BY c.name, a.collected_at,
         a.datname, a.state
ORDER BY a.collected_at DESC;
```

### Time-Series Analysis

In the following example, the query retrieves active
connections over time:

```sql
SELECT
    c.name AS server_name,
    DATE_TRUNC('minute', a.collected_at)
        AS minute,
    COUNT(*) AS active_connections
FROM metrics.pg_stat_activity a
JOIN connections c ON a.connection_id = c.id
WHERE a.collected_at > NOW()
    - INTERVAL '24 hours'
  AND a.state = 'active'
GROUP BY c.name,
    DATE_TRUNC('minute', a.collected_at)
ORDER BY minute DESC, server_name;
```

### Partition Information

In the following example, the query checks partition
sizes:

```sql
SELECT
    schemaname || '.' || tablename
        AS partition,
    pg_size_pretty(
        pg_total_relation_size(
            schemaname || '.' || tablename
        )
    ) AS size
FROM pg_tables
WHERE schemaname = 'metrics'
  AND tablename LIKE 'pg_stat_activity_%'
ORDER BY
    pg_total_relation_size(
        schemaname || '.' || tablename
    ) DESC;
```

## Schema Maintenance

This section covers routine schema maintenance
operations.

### Vacuum

PostgreSQL autovacuum handles most maintenance
automatically. In the following example, the command
manually vacuums a specific partition:

```sql
VACUUM ANALYZE metrics.pg_stat_activity_20251104;
```

### Analyze

In the following example, the command updates
statistics for query planning:

```sql
ANALYZE metrics.pg_stat_activity;
```

### Reindex

If indexes become bloated, run the following command
to rebuild them:

```sql
REINDEX TABLE metrics.pg_stat_activity;
```

## See Also

The following resources provide additional details.

- [Schema Management](schema-management.md) covers
  the migration system details.
- [Probes](probes.md) explains how probes collect
  and store data.
- [Architecture](architecture.md) describes the
  overall system design.
