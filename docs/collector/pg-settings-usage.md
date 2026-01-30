# pg_settings Probe Usage Guide

This guide provides examples and best practices for using the `pg_settings`
probe to track PostgreSQL configuration changes over time.

## Overview

The `pg_settings` probe collects PostgreSQL configuration settings with
intelligent change detection. Unlike other probes that store data on every
collection interval, this probe only stores configuration snapshots when
changes are detected, making it ideal for long-term configuration tracking
without excessive storage costs.

### Key Features

- **Change Detection**: Uses SHA256 hash comparison to detect configuration
    changes
- **Selective Storage**: Only stores data when configuration differs from the
    most recent snapshot
- **Long Retention**: Default 365-day retention for year-over-year analysis
- **Protected Data**: Garbage collector preserves the most recent snapshot for
    each server regardless of age
- **Hourly Checks**: Runs every hour by default to detect changes promptly

## Common Queries

### View Current Configuration for a Server

Get the most recent configuration snapshot for a specific monitored connection:

```sql
SELECT name, setting, unit, source, sourcefile, pending_restart
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

Identify settings that changed between two specific timestamps:

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

Show the complete history of configuration changes:

```sql
SELECT
  collected_at,
  COUNT(*) AS settings_count,
  COUNT(*) FILTER (WHERE source = 'configuration file') AS from_config_file,
  COUNT(*) FILTER (WHERE source = 'default') AS from_defaults,
  COUNT(*) FILTER (WHERE pending_restart = true) AS pending_restart
FROM metrics.pg_settings
WHERE connection_id = 1
GROUP BY collected_at
ORDER BY collected_at DESC;
```

### Find Settings Changed in the Last 30 Days

Identify which specific settings changed recently:

```sql
WITH changes AS (
  SELECT DISTINCT name
  FROM metrics.pg_settings
  WHERE connection_id = 1
    AND collected_at >= CURRENT_TIMESTAMP - INTERVAL '30 days'
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
  AND ps.collected_at >= CURRENT_TIMESTAMP - INTERVAL '30 days'
ORDER BY ps.name, ps.collected_at DESC;
```

### Identify Pending Restart Requirements

Find configuration changes that require a server restart to take effect:

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

Identify configuration differences between servers:

```sql
WITH latest_settings AS (
  SELECT DISTINCT ON (connection_id, name)
    connection_id,
    name,
    setting,
    source
  FROM metrics.pg_settings
  ORDER BY connection_id, name, collected_at DESC
)
SELECT
  c.name AS server_name,
  ls.name AS setting_name,
  ls.setting,
  ls.source
FROM latest_settings ls
JOIN connections c ON c.id = ls.connection_id
WHERE ls.name = 'max_connections'  -- Example: compare max_connections
ORDER BY c.name;
```

### Audit Configuration Source Changes

Track when settings moved from one source to another (e.g., default to config
file):

```sql
WITH numbered_changes AS (
  SELECT
    connection_id,
    name,
    setting,
    source,
    collected_at,
    LAG(source) OVER (PARTITION BY connection_id, name ORDER BY collected_at) AS prev_source
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

Identify all settings that differ from PostgreSQL defaults:

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

### Configuration Change Auditing

Track who changed what and when by correlating `pg_settings` data with other
audit logs:

```sql
-- Find all configuration snapshots in a specific time window
SELECT
  collected_at AS snapshot_time,
  COUNT(*) AS total_settings,
  COUNT(*) FILTER (WHERE sourcefile IS NOT NULL) AS from_files
FROM metrics.pg_settings
WHERE connection_id = 1
  AND collected_at BETWEEN '2025-01-01' AND '2025-01-31'
GROUP BY collected_at
ORDER BY collected_at;
```

### Configuration Drift Detection

Identify when servers drift from a standard configuration:

```sql
-- Compare each server's latest config against a baseline
WITH baseline AS (
  SELECT name, setting
  FROM metrics.pg_settings
  WHERE connection_id = 1  -- Reference server
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
  ORDER BY connection_id, name, collected_at DESC
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
  AND cs.connection_id != 1  -- Exclude baseline server
ORDER BY c.name, cs.name;
```

### Capacity Planning Analysis

Track configuration changes related to resource allocation over time:

```sql
SELECT
  collected_at,
  (SELECT setting FROM metrics.pg_settings p
   WHERE p.connection_id = ps.connection_id
     AND p.collected_at = ps.collected_at
     AND p.name = 'max_connections') AS max_connections,
  (SELECT setting FROM metrics.pg_settings p
   WHERE p.connection_id = ps.connection_id
     AND p.collected_at = ps.collected_at
     AND p.name = 'shared_buffers') AS shared_buffers,
  (SELECT setting FROM metrics.pg_settings p
   WHERE p.connection_id = ps.connection_id
     AND p.collected_at = ps.collected_at
     AND p.name = 'work_mem') AS work_mem
FROM (
  SELECT DISTINCT connection_id, collected_at
  FROM metrics.pg_settings
  WHERE connection_id = 1
) ps
ORDER BY collected_at DESC;
```

### Compliance Verification

Verify that specific security-related settings meet compliance requirements:

```sql
-- Check critical security settings in latest snapshot
SELECT
  name,
  setting,
  source,
  CASE
    WHEN name = 'ssl' AND setting = 'on' THEN 'PASS'
    WHEN name = 'password_encryption' AND setting = 'scram-sha-256' THEN 'PASS'
    WHEN name = 'log_connections' AND setting = 'on' THEN 'PASS'
    WHEN name = 'log_disconnections' AND setting = 'on' THEN 'PASS'
    ELSE 'FAIL'
  END AS compliance_status
FROM metrics.pg_settings
WHERE connection_id = 1
  AND collected_at = (
    SELECT MAX(collected_at)
    FROM metrics.pg_settings
    WHERE connection_id = 1
  )
  AND name IN ('ssl', 'password_encryption', 'log_connections', 'log_disconnections')
ORDER BY name;
```

## Best Practices

### 1. Understand the Collection Interval

The probe checks configuration every hour by default. If you need more
frequent detection of configuration changes, adjust the collection interval:

```sql
UPDATE probe_configs
SET collection_interval_seconds = 1800  -- 30 minutes
WHERE name = 'pg_settings'
  AND connection_id IS NULL;  -- Global default
```

Changes take effect within 5 minutes (automatic config reload).

### 2. Leverage the Long Retention Period

With a default 365-day retention, you can perform year-over-year analysis:

```sql
-- Compare configuration from exactly one year ago
SELECT
  name,
  setting AS current_setting,
  LAG(setting) OVER (PARTITION BY name ORDER BY collected_at) AS year_ago_setting
FROM metrics.pg_settings
WHERE connection_id = 1
  AND collected_at IN (
    (SELECT MAX(collected_at) FROM metrics.pg_settings WHERE connection_id = 1),
    (SELECT MAX(collected_at) FROM metrics.pg_settings
     WHERE connection_id = 1
       AND collected_at < CURRENT_TIMESTAMP - INTERVAL '1 year')
  )
ORDER BY name;
```

### 3. Monitor for Unexpected Changes

Set up alerts for configuration changes you don't expect:

```sql
-- Find configuration changes in the last 24 hours
SELECT
  collected_at,
  COUNT(*) AS changed_settings
FROM (
  SELECT DISTINCT collected_at
  FROM metrics.pg_settings
  WHERE connection_id = 1
    AND collected_at >= CURRENT_TIMESTAMP - INTERVAL '24 hours'
) recent_snapshots
GROUP BY collected_at
ORDER BY collected_at DESC;
```

If this returns multiple snapshots, configuration is changing frequently and
may warrant investigation.

### 4. Document Configuration Changes

Maintain a separate changelog table that references `pg_settings` snapshots:

```sql
CREATE TABLE config_changes (
  id SERIAL PRIMARY KEY,
  connection_id INTEGER NOT NULL,
  change_date TIMESTAMP NOT NULL,
  changed_by TEXT,
  reason TEXT,
  ticket_number TEXT,
  FOREIGN KEY (connection_id) REFERENCES connections(id)
);

-- Link changes to pg_settings snapshots
SELECT
  cc.change_date,
  cc.changed_by,
  cc.reason,
  ps.collected_at AS snapshot_time,
  COUNT(*) AS settings_in_snapshot
FROM config_changes cc
LEFT JOIN metrics.pg_settings ps
  ON ps.connection_id = cc.connection_id
  AND ps.collected_at >= cc.change_date
  AND ps.collected_at < cc.change_date + INTERVAL '2 hours'
GROUP BY cc.change_date, cc.changed_by, cc.reason, ps.collected_at
ORDER BY cc.change_date DESC;
```

## Integration with Other Probes

### Correlate Configuration with Performance

Join `pg_settings` data with performance metrics to understand how
configuration changes impact system behavior:

```sql
-- Compare query performance before and after a configuration change
WITH config_change AS (
  SELECT collected_at AS change_time
  FROM metrics.pg_settings
  WHERE connection_id = 1
    AND collected_at >= '2025-01-10'
  LIMIT 1
)
SELECT
  CASE
    WHEN ss.collected_at < cc.change_time THEN 'Before'
    ELSE 'After'
  END AS period,
  AVG(ss.mean_exec_time) AS avg_query_time,
  SUM(ss.calls) AS total_calls
FROM metrics.pg_stat_statements ss
CROSS JOIN config_change cc
WHERE ss.connection_id = 1
  AND ss.collected_at BETWEEN cc.change_time - INTERVAL '1 day'
                           AND cc.change_time + INTERVAL '1 day'
GROUP BY period;
```

### Track Configuration Alongside System Changes

Correlate configuration changes with OS-level changes:

```sql
-- Find configuration changes near system restarts
SELECT
  ps.collected_at AS config_snapshot,
  ps.name AS setting_name,
  ps.setting,
  pw.archiver_stats_reset AS postgres_restart
FROM metrics.pg_settings ps
JOIN metrics.pg_stat_wal pw
  ON pw.connection_id = ps.connection_id
  AND ABS(EXTRACT(EPOCH FROM (pw.collected_at - ps.collected_at))) < 3600
WHERE ps.connection_id = 1
  AND pw.archiver_stats_reset IS NOT NULL
ORDER BY ps.collected_at DESC;
```

## Troubleshooting

### No Data Appearing

If you don't see any `pg_settings` data:

1. Verify the probe is enabled:
   ```sql
   SELECT is_enabled
   FROM probe_configs
   WHERE name = 'pg_settings'
     AND connection_id IS NULL;
   ```

2. Check collector logs for errors related to pg_settings

3. Verify partitions exist:
   ```sql
   SELECT tablename
   FROM pg_tables
   WHERE schemaname = 'metrics'
     AND tablename LIKE 'pg_settings_%'
   ORDER BY tablename DESC;
   ```

### Data Not Updating

If configuration data seems stale:

1. Remember: Data only stores when changes are detected
2. Check last collection time:
   ```sql
   SELECT MAX(collected_at) AS last_snapshot
   FROM metrics.pg_settings
   WHERE connection_id = 1;
   ```

3. Verify probe is running:
   ```sql
   -- Check collector logs for recent pg_settings execution
   ```

### Understanding Why Data Wasn't Stored

The probe uses hash comparison. If no new snapshot appears after configuration
changes:

1. Verify the change actually took effect in PostgreSQL:
   ```sql
   SHOW max_connections;  -- On the monitored server
   ```

2. Wait up to 1 hour (default collection interval) for detection

3. Check if change requires restart:
   ```sql
   SELECT name, setting, pending_restart
   FROM pg_settings
   WHERE name = 'your_setting_name';
   ```

## See Also

- [Probe Reference](probe-reference.md) - Complete probe documentation
- [Probes System](probes.md) - How probes work
- [Configuration](configuration.md) - Adjusting probe settings
