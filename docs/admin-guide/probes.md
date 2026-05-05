# Probe Management

Probes are data collection units that gather metrics
from monitored PostgreSQL servers. Administrators
configure probe intervals, retention policies, and
enabled states through the admin panel or the REST API.

## Overview

The Collector includes 36 built-in probes that cover
the most important PostgreSQL statistics views. Each
probe runs on an independent schedule and stores
results in partitioned metrics tables.

## Probe Types

Probes are categorized by their scope:

### Server-Scoped Probes

Server-scoped probes collect server-wide statistics and
execute once per monitored connection:

- `pg_stat_activity` collects current database
  activity.
- `pg_stat_replication` collects replication status,
  lag, and WAL receiver statistics.
- `pg_replication_slots` collects replication slot
  usage and statistics.
- `pg_stat_recovery_prefetch` collects recovery
  prefetch statistics.
- `pg_stat_subscription` collects logical replication
  subscription statistics.
- `pg_stat_connection_security` collects SSL and
  GSSAPI connection security data.
- `pg_stat_io` collects I/O and SLRU cache statistics.
- `pg_stat_checkpointer` collects checkpointer and
  background writer statistics.
- `pg_stat_wal` collects WAL generation and archiver
  statistics.
- `pg_settings` collects PostgreSQL configuration
  settings (change-tracked).
- `pg_hba_file_rules` collects `pg_hba.conf`
  authentication rules (change-tracked).
- `pg_ident_file_mappings` collects `pg_ident.conf`
  user mappings (change-tracked).
- `pg_server_info` collects server identification and
  configuration (change-tracked).
- `pg_node_role` detects the node role for cluster
  topologies.
- `pg_database` collects the database catalog with
  XID wraparound indicators.

### Database-Scoped Probes

Database-scoped probes collect per-database statistics
and execute once for each database on a monitored
server:

- `pg_stat_database` collects database-wide
  statistics.
- `pg_stat_database_conflicts` collects recovery
  conflict statistics.
- `pg_stat_all_tables` collects table access and I/O
  statistics.
- `pg_stat_all_indexes` collects index usage and I/O
  statistics.
- `pg_statio_all_sequences` collects sequence I/O
  statistics.
- `pg_stat_user_functions` collects user function
  statistics.
- `pg_stat_statements` collects query performance
  statistics (requires extension).
- `pg_extension` collects installed extensions
  (change-tracked).
- `spock_exception_log` collects rows added to
  `spock.exception_log` in a rolling 15-minute window
  and targets Spock v5 or later.
- `spock_resolutions` collects rows added to
  `spock.resolutions` in a rolling 15-minute window
  and targets Spock v5 or later.

## Default Collection Intervals

Different probe types have different default collection
intervals based on how frequently their data changes:

- Fast probes run every 30 to 60 seconds and cover
  replication and activity data.
- Normal probes run every 300 seconds (5 minutes) and
  cover most statistics.
- Slow probes run every 600 seconds (10 minutes) and
  cover checkpointer and WAL data.
- Very slow probes run every 900 seconds (15 minutes)
  and cover I/O statistics.
- Change-tracked probes run every 3600 seconds
  (1 hour) and only store data when changes are
  detected.

## Probe Configuration

Probes are configured in the `probe_configs` table,
which supports both global defaults and per-server
overrides.

In the following example, the query displays all probe
configurations:

```sql
SELECT name, connection_id,
       collection_interval_seconds,
       retention_days, is_enabled
FROM probe_configs
ORDER BY name, COALESCE(connection_id, 0);
```

In the following example, the query displays only
global defaults:

```sql
SELECT name, collection_interval_seconds,
       retention_days, is_enabled
FROM probe_configs
WHERE connection_id IS NULL
ORDER BY name;
```

## Hierarchical Probe Overrides

Probe settings can be customized at multiple levels of
the server hierarchy. The override system uses the
following precedence order:

1. Server overrides apply to a specific connection.
2. Cluster overrides apply to all servers in a cluster.
3. Group overrides apply to all clusters in a group.
4. Global default settings apply when no override
   exists.
5. Hardcoded default values apply if no database
   configuration exists.

Overrides are managed through the Probe Configuration
tab in the server, cluster, or group edit dialogs. The
override panel shows all probes with their current
settings. Probes without an override at the current
level appear dimmed to indicate the setting is
inherited.

When a new monitored connection is added, the Collector
automatically creates probe configurations based on
the global defaults.

## Automatic Configuration Reload

The Collector automatically reloads probe
configurations from the database every 5 minutes.
Changes to `collection_interval_seconds`,
`retention_days`, or `is_enabled` take effect within
5 minutes without requiring a restart.

## Adjusting Collection Intervals

In the following example, the query updates the global
default interval for the `pg_stat_activity` probe:

```sql
UPDATE probe_configs
SET collection_interval_seconds = 60
WHERE name = 'pg_stat_activity'
  AND connection_id IS NULL;
```

In the following example, the query overrides the
interval for a specific connection:

```sql
UPDATE probe_configs
SET collection_interval_seconds = 30
WHERE name = 'pg_stat_activity'
  AND connection_id = 1;
```

Changes take effect within 5 minutes through the
automatic configuration reload.

## Adjusting Retention

In the following example, the query updates the global
default retention for the `pg_stat_activity` probe:

```sql
UPDATE probe_configs
SET retention_days = 30
WHERE name = 'pg_stat_activity'
  AND connection_id IS NULL;
```

In the following example, the query overrides the
retention for a specific connection:

```sql
UPDATE probe_configs
SET retention_days = 60
WHERE name = 'pg_stat_activity'
  AND connection_id = 3;
```

Retention changes take effect on the next garbage
collection run (within 24 hours).

## Enabling and Disabling Probes

In the following example, the query disables a probe
globally:

```sql
UPDATE probe_configs
SET is_enabled = FALSE
WHERE name = 'pg_stat_statements'
  AND connection_id IS NULL;
```

In the following example, the query disables a probe
for a specific connection only:

```sql
UPDATE probe_configs
SET is_enabled = FALSE
WHERE name = 'pg_stat_statements'
  AND connection_id = 2;
```

Changes take effect within 5 minutes through the
automatic configuration reload.

## Collection Frequency Guidelines

Balance collection frequency against the following
trade-offs:

- More frequent collection provides more current data.
- More frequent collection generates more queries on
  monitored servers.
- More frequent collection produces more data points
  and increases storage usage.

The following guidelines apply to common probe types:

- Fast-changing data such as replication lag should use
  30 to 60 second intervals.
- Moderate data such as table statistics should use
  300 second (5 minute) intervals.
- Slow-changing data such as archiver statistics should
  use 600 second (10 minute) or longer intervals.

## Monitoring Probe Status

### Checking Probe Configuration

In the following example, the query displays all
enabled probe configurations:

```sql
SELECT name, connection_id,
       collection_interval_seconds,
       retention_days, is_enabled
FROM probe_configs
WHERE is_enabled = TRUE
ORDER BY name, COALESCE(connection_id, 0);
```

### Checking Last Collection

In the following example, the query displays the most
recent data collection for a probe:

```sql
SELECT connection_id,
       MAX(collected_at) AS last_collected
FROM metrics.pg_stat_activity
GROUP BY connection_id
ORDER BY last_collected DESC;
```

### Checking Storage Usage

In the following example, the query displays the
storage used by a probe:

```sql
SELECT pg_size_pretty(
    SUM(pg_total_relation_size(
        schemaname || '.' || tablename
    ))
) AS total_size
FROM pg_tables
WHERE schemaname = 'metrics'
  AND tablename LIKE 'pg_stat_activity_%';
```

## Troubleshooting

### Probe Not Collecting Data

Verify that the probe is enabled by checking the
probe configuration:

```sql
SELECT is_enabled FROM probe_configs
WHERE name = 'probe_name'
  AND connection_id IS NULL;
```

If the probe is enabled, check the collector logs for
errors. Verify that the monitored connection is
accessible and has `is_monitored` set to `TRUE`.

### High Storage Usage

Reduce retention days for high-volume probes to manage
storage consumption. Manually drop old partitions if
immediate space reclamation is needed. Consider
sampling strategies for high-frequency data.

## REST API

The probe configuration REST API provides endpoints
for managing probe settings. All write operations
require the `manage_probes` permission.

The following table lists the available endpoints:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/probe-configs` | List probe configurations. |
| `GET` | `/api/v1/probe-configs/{id}` | Get a probe configuration by ID. |
| `PUT` | `/api/v1/probe-configs/{id}` | Update a probe configuration. |

### Probe Override Endpoints

The probe override REST API manages per-scope probe
settings. Write operations require the `manage_probes`
permission.

The following table lists the available endpoints:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/probe-overrides/{scope}/{scopeId}` | List probe overrides for a scope. |
| `PUT` | `/api/v1/probe-overrides/{scope}/{scopeId}/{probeName}` | Create or update a probe override. |
| `DELETE` | `/api/v1/probe-overrides/{scope}/{scopeId}/{probeName}` | Delete a probe override. |

## Related Documentation

- [Alert Rules](alert-rules.md) describes the rules
  that evaluate probe metrics.
- [Metrics](api/metrics.md) documents the tools for
  querying collected probe data.
