# pg_settings Probe Usage Guide

This guide provides examples and best practices for
using the `pg_settings` probe to track PostgreSQL
configuration changes over time.

## Overview

The `pg_settings` probe collects PostgreSQL
configuration settings with intelligent change
detection. Unlike other probes that store data on
every collection interval, this probe only stores
configuration snapshots when changes are detected.

### Key Features

The `pg_settings` probe includes the following
features:

- Change detection uses SHA256 hash comparison to
  identify configuration changes.
- Selective storage only stores data when the
  configuration differs from the most recent
  snapshot.
- Long retention provides a default 365-day retention
  for year-over-year analysis.
- The garbage collector preserves the most recent
  snapshot for each server regardless of age.
- Hourly checks run every hour by default to detect
  changes promptly.

## Common Queries

This section provides SQL queries for common
`pg_settings` analysis tasks.

### View Current Configuration for a Server

In the following example, the query retrieves the
most recent configuration snapshot for a specific
monitored connection:

```sql
SELECT name, setting, unit, source,
       sourcefile, pending_restart
FROM metrics.pg_settings
WHERE connection_id = 1
  AND collected_at = (
    SELECT MAX(collected_at)
    FROM metrics.pg_settings
    WHERE connection_id = 1
  )
ORDER BY name;
```

### Compare Configuration Between Two Points in Time

In the following example, the query identifies
settings that changed between two specific
timestamps:

```sql
WITH recent AS (
  SELECT name, setting, source
  FROM metrics.pg_settings
  WHERE connection_id = 1
    AND collected_at = '2025-01-15 10:00:00'
),
previous AS (
  SELECT name, setting, source
  FROM metrics.pg_settings
  WHERE connection_id = 1
    AND collected_at = '2025-01-10 10:00:00'
)
SELECT
  COALESCE(r.name, p.name) AS setting_name,
  p.setting AS old_value,
  r.setting AS new_value,
  p.source AS old_source,
  r.source AS new_source
FROM recent r
FULL OUTER JOIN previous p ON r.name = p.name
WHERE r.setting IS DISTINCT FROM p.setting
   OR r.source IS DISTINCT FROM p.source
ORDER BY setting_name;
```

### Track All Configuration Changes for a Server

In the following example, the query shows the
complete history of configuration changes:

```sql
SELECT
  collected_at,
  COUNT(*) AS settings_count,
  COUNT(*) FILTER (
    WHERE source = 'configuration file'
  ) AS from_config_file,
  COUNT(*) FILTER (
    WHERE source = 'default'
  ) AS from_defaults,
  COUNT(*) FILTER (
    WHERE pending_restart = true
  ) AS pending_restart
FROM metrics.pg_settings
WHERE connection_id = 1
GROUP BY collected_at
ORDER BY collected_at DESC;
```

### Find Settings Changed in the Last 30 Days

In the following example, the query identifies which
specific settings changed recently:

```sql
WITH changes AS (
  SELECT DISTINCT name
  FROM metrics.pg_settings
  WHERE connection_id = 1
    AND collected_at >= CURRENT_TIMESTAMP
        - INTERVAL '30 days'
)
SELECT
  ps.name,
  ps.collected_at,
  ps.setting,
  ps.source,
  ps.pending_restart
FROM metrics.pg_settings ps
JOIN changes c ON ps.name = c.name
WHERE ps.connection_id = 1
  AND ps.collected_at >= CURRENT_TIMESTAMP
      - INTERVAL '30 days'
ORDER BY ps.name, ps.collected_at DESC;
```

### Identify Pending Restart Requirements

In the following example, the query finds
configuration changes that require a server restart:

```sql
SELECT
  name,
  setting,
  unit,
  source,
  boot_val,
  reset_val,
  collected_at
FROM metrics.pg_settings
WHERE connection_id = 1
  AND collected_at = (
    SELECT MAX(collected_at)
    FROM metrics.pg_settings
    WHERE connection_id = 1
  )
  AND pending_restart = true
ORDER BY name;
```

### Compare Configuration Across Multiple Servers

In the following example, the query identifies
configuration differences between servers:

```sql
WITH latest_settings AS (
  SELECT DISTINCT ON (connection_id, name)
    connection_id,
    name,
    setting,
    source
  FROM metrics.pg_settings
  ORDER BY connection_id, name,
           collected_at DESC
)
SELECT
  c.name AS server_name,
  ls.name AS setting_name,
  ls.setting,
  ls.source
FROM latest_settings ls
JOIN connections c ON c.id = ls.connection_id
WHERE ls.name = 'max_connections'
ORDER BY c.name;
```

### Audit Configuration Source Changes

In the following example, the query tracks when
settings moved from one source to another:

```sql
WITH numbered_changes AS (
  SELECT
    connection_id,
    name,
    setting,
    source,
    collected_at,
    LAG(source) OVER (
        PARTITION BY connection_id, name
        ORDER BY collected_at
    ) AS prev_source
  FROM metrics.pg_settings
  WHERE connection_id = 1
)
SELECT
  name,
  prev_source AS from_source,
  source AS to_source,
  setting,
  collected_at AS changed_at
FROM numbered_changes
WHERE prev_source IS DISTINCT FROM source
ORDER BY collected_at DESC;
```

### Find Non-Default Settings

In the following example, the query identifies all
settings that differ from PostgreSQL defaults:

```sql
SELECT
  name,
  setting,
  boot_val AS default_value,
  source,
  context,
  sourcefile
FROM metrics.pg_settings
WHERE connection_id = 1
  AND collected_at = (
    SELECT MAX(collected_at)
    FROM metrics.pg_settings
    WHERE connection_id = 1
  )
  AND setting IS DISTINCT FROM boot_val
ORDER BY name;
```

## Use Cases

This section describes common use cases for the
`pg_settings` probe data.

### Configuration Change Auditing

Track configuration changes by correlating
`pg_settings` data with other audit logs. In the
following example, the query finds all configuration
snapshots in a specific time window:

```sql
SELECT
  collected_at AS snapshot_time,
  COUNT(*) AS total_settings,
  COUNT(*) FILTER (
    WHERE sourcefile IS NOT NULL
  ) AS from_files
FROM metrics.pg_settings
WHERE connection_id = 1
  AND collected_at BETWEEN
      '2025-01-01' AND '2025-01-31'
GROUP BY collected_at
ORDER BY collected_at;
```

### Configuration Drift Detection

Identify when servers drift from a standard
configuration. In the following example, the query
compares each server against a baseline:

```sql
WITH baseline AS (
  SELECT name, setting
  FROM metrics.pg_settings
  WHERE connection_id = 1
    AND collected_at = (
      SELECT MAX(collected_at)
      FROM metrics.pg_settings
      WHERE connection_id = 1
    )
),
current_servers AS (
  SELECT DISTINCT ON (connection_id, name)
    connection_id,
    name,
    setting
  FROM metrics.pg_settings
  ORDER BY connection_id, name,
           collected_at DESC
)
SELECT
  c.name AS server_name,
  cs.name AS setting_name,
  b.setting AS baseline_value,
  cs.setting AS current_value
FROM current_servers cs
JOIN connections c ON c.id = cs.connection_id
LEFT JOIN baseline b ON cs.name = b.name
WHERE cs.setting IS DISTINCT FROM b.setting
  AND cs.connection_id != 1
ORDER BY c.name, cs.name;
```

### Capacity Planning Analysis

Track configuration changes related to resource
allocation over time. In the following example, the
query retrieves resource-related settings across
snapshots:

```sql
SELECT
  collected_at,
  (SELECT setting
   FROM metrics.pg_settings p
   WHERE p.connection_id = ps.connection_id
     AND p.collected_at = ps.collected_at
     AND p.name = 'max_connections'
  ) AS max_connections,
  (SELECT setting
   FROM metrics.pg_settings p
   WHERE p.connection_id = ps.connection_id
     AND p.collected_at = ps.collected_at
     AND p.name = 'shared_buffers'
  ) AS shared_buffers,
  (SELECT setting
   FROM metrics.pg_settings p
   WHERE p.connection_id = ps.connection_id
     AND p.collected_at = ps.collected_at
     AND p.name = 'work_mem'
  ) AS work_mem
FROM (
  SELECT DISTINCT connection_id, collected_at
  FROM metrics.pg_settings
  WHERE connection_id = 1
) ps
ORDER BY collected_at DESC;
```

### Compliance Verification

Verify that specific security-related settings meet
compliance requirements. In the following example,
the query checks critical security settings:

```sql
SELECT
  name,
  setting,
  source,
  CASE
    WHEN name = 'ssl'
     AND setting = 'on' THEN 'PASS'
    WHEN name = 'password_encryption'
     AND setting = 'scram-sha-256' THEN 'PASS'
    WHEN name = 'log_connections'
     AND setting = 'on' THEN 'PASS'
    WHEN name = 'log_disconnections'
     AND setting = 'on' THEN 'PASS'
    ELSE 'FAIL'
  END AS compliance_status
FROM metrics.pg_settings
WHERE connection_id = 1
  AND collected_at = (
    SELECT MAX(collected_at)
    FROM metrics.pg_settings
    WHERE connection_id = 1
  )
  AND name IN (
    'ssl', 'password_encryption',
    'log_connections', 'log_disconnections'
  )
ORDER BY name;
```

## Best Practices

Follow these best practices when working with
`pg_settings` data.

### Understand the Collection Interval

The probe checks configuration every hour by default.
To detect changes more frequently, adjust the
collection interval. In the following example, the
command changes the interval to 30 minutes:

```sql
UPDATE probe_configs
SET collection_interval_seconds = 1800
WHERE name = 'pg_settings'
  AND connection_id IS NULL;
```

Changes take effect within 5 minutes through
automatic configuration reload.

### Leverage the Long Retention Period

With a default 365-day retention, you can perform
year-over-year analysis. In the following example,
the query compares configuration from exactly one
year ago:

```sql
SELECT
  name,
  setting AS current_setting,
  LAG(setting) OVER (
    PARTITION BY name
    ORDER BY collected_at
  ) AS year_ago_setting
FROM metrics.pg_settings
WHERE connection_id = 1
  AND collected_at IN (
    (SELECT MAX(collected_at)
     FROM metrics.pg_settings
     WHERE connection_id = 1),
    (SELECT MAX(collected_at)
     FROM metrics.pg_settings
     WHERE connection_id = 1
       AND collected_at < CURRENT_TIMESTAMP
           - INTERVAL '1 year')
  )
ORDER BY name;
```

### Monitor for Unexpected Changes

Set up alerts for configuration changes you do not
expect. In the following example, the query finds
configuration changes in the last 24 hours:

```sql
SELECT
  collected_at,
  COUNT(*) AS changed_settings
FROM (
  SELECT DISTINCT collected_at
  FROM metrics.pg_settings
  WHERE connection_id = 1
    AND collected_at >= CURRENT_TIMESTAMP
        - INTERVAL '24 hours'
) recent_snapshots
GROUP BY collected_at
ORDER BY collected_at DESC;
```

If this query returns multiple snapshots, the
configuration is changing frequently and may warrant
investigation.

## Integration with Other Probes

This section shows how to correlate `pg_settings`
data with other metrics.

### Correlate Configuration with Performance

Join `pg_settings` data with performance metrics to
understand how configuration changes impact system
behavior. In the following example, the query
compares query performance before and after a
configuration change:

```sql
WITH config_change AS (
  SELECT collected_at AS change_time
  FROM metrics.pg_settings
  WHERE connection_id = 1
    AND collected_at >= '2025-01-10'
  LIMIT 1
)
SELECT
  CASE
    WHEN ss.collected_at < cc.change_time
        THEN 'Before'
    ELSE 'After'
  END AS period,
  AVG(ss.mean_exec_time) AS avg_query_time,
  SUM(ss.calls) AS total_calls
FROM metrics.pg_stat_statements ss
CROSS JOIN config_change cc
WHERE ss.connection_id = 1
  AND ss.collected_at BETWEEN
      cc.change_time - INTERVAL '1 day'
      AND cc.change_time + INTERVAL '1 day'
GROUP BY period;
```

## Troubleshooting

This section covers common issues with the
`pg_settings` probe.

### No Data Appearing

If no `pg_settings` data appears, check the
following:

1. Verify the probe is enabled:
   ```sql
   SELECT is_enabled
   FROM probe_configs
   WHERE name = 'pg_settings'
     AND connection_id IS NULL;
   ```

2. Check the Collector logs for errors related to
   `pg_settings`.

3. Verify that partitions exist:
   ```sql
   SELECT tablename
   FROM pg_tables
   WHERE schemaname = 'metrics'
     AND tablename LIKE 'pg_settings_%'
   ORDER BY tablename DESC;
   ```

### Data Not Updating

If configuration data seems stale, consider the
following points:

1. The probe only stores data when changes are
   detected.
2. Check the last collection time:
   ```sql
   SELECT MAX(collected_at) AS last_snapshot
   FROM metrics.pg_settings
   WHERE connection_id = 1;
   ```
3. Verify the probe is running by checking the
   Collector logs.

### Understanding Why Data Was Not Stored

The probe uses hash comparison for change detection.
If no new snapshot appears after configuration
changes, follow these steps:

1. Verify the change took effect in PostgreSQL:
   ```sql
   SHOW max_connections;
   ```

2. Wait up to 1 hour (default collection interval)
   for detection.

3. Check whether the change requires a restart:
   ```sql
   SELECT name, setting, pending_restart
   FROM pg_settings
   WHERE name = 'your_setting_name';
   ```

## See Also

The following resources provide additional details.

- [Probe Reference](probe-reference.md) provides
  complete probe documentation.
- [Probes](probes.md) explains how probes work
  internally.
