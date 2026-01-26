# Adding Alert Rules

This guide explains how to create custom alert rules for the alerter.
Custom rules extend the built-in rule set with monitoring requirements
specific to your organization.

## Prerequisites

Before creating a custom rule, ensure:

- The metric you want to monitor is collected by the collector.
- You understand the metric's normal value range.
- You have determined an appropriate threshold and severity.

## Creating a Rule

Alert rules are stored in the `alert_rules` table in the datastore. You
can create rules using SQL or through the API.

### Using SQL

In the following example, a custom rule monitors temporary file usage:

```sql
INSERT INTO alert_rules (
    name,
    description,
    category,
    metric_name,
    default_operator,
    default_threshold,
    default_severity,
    default_enabled,
    is_built_in
) VALUES (
    'High Temporary File Usage',
    'Alerts when temporary files exceed 100 per interval',
    'performance',
    'pg_stat_database.temp_files_delta',
    '>',
    100.0,
    'warning',
    true,
    false
);
```

### Rule Fields

Each rule requires the following fields:

| Field | Type | Description |
|-------|------|-------------|
| `name` | text | A unique, descriptive name |
| `description` | text | An explanation of what the rule detects |
| `category` | text | The category for grouping rules |
| `metric_name` | text | The metric identifier to evaluate |
| `default_operator` | text | The comparison operator |
| `default_threshold` | numeric | The threshold value |
| `default_severity` | text | The alert severity level |
| `default_enabled` | boolean | Whether the rule is enabled by default |
| `is_built_in` | boolean | Set to `false` for custom rules |

## Metric Names

The metric name must match a metric that the alerter can query. The
alerter supports the following metric name patterns:

### Connection Metrics

- `pg_stat_activity.count` - Number of active connections.
- `connection_utilization_percent` - Connection usage percentage.
- `pg_stat_activity.blocked_count` - Number of blocked sessions.
- `pg_stat_activity.idle_in_transaction_seconds` - Idle transaction time.
- `pg_stat_activity.max_query_duration_seconds` - Longest running query.
- `pg_stat_activity.max_xact_duration_seconds` - Longest transaction.
- `pg_stat_activity.max_lock_wait_seconds` - Longest lock wait time.

### Replication Metrics

- `pg_stat_replication.replay_lag_seconds` - Replication lag in seconds.
- `pg_stat_replication.lag_bytes` - Replication lag in bytes.
- `pg_replication_slots.inactive` - Inactive replication slots.

### Database Metrics

- `pg_stat_database.cache_hit_ratio` - Buffer cache hit ratio.
- `pg_stat_database.deadlocks_delta` - New deadlocks per interval.
- `pg_stat_database.temp_files_delta` - New temporary files per interval.

### Table Metrics

- `pg_stat_all_tables.dead_tuple_percent` - Dead tuple percentage.
- `table_bloat_ratio` - Estimated table bloat.
- `table_last_autovacuum_hours` - Hours since last autovacuum.
- `age_percent` - Transaction ID age percentage.

### System Metrics

- `pg_sys_cpu_usage_info.processor_time_percent` - CPU usage percentage.
- `pg_sys_memory_info.used_percent` - Memory usage percentage.
- `pg_sys_disk_info.used_percent` - Disk usage percentage.
- `pg_sys_load_avg_info.load_avg_fifteen_minutes` - 15-minute load average.

### Other Metrics

- `pg_stat_archiver.failed_count_delta` - Failed archive attempts.
- `pg_stat_checkpointer.checkpoints_req_delta` - Requested checkpoints.
- `pg_stat_statements.slow_query_count` - Slow queries per interval.

## Adding Support for New Metrics

If you need a metric that is not currently supported, you must add
support in the alerter code before creating a rule.

### Step 1: Add the Metric Query

Edit `internal/database/queries.go` and add a case to the
`GetLatestMetricValues` function:

```go
case "your_new_metric_name":
    rows, err := d.pool.Query(ctx, `
        SELECT connection_id, your_value::float, collected_at
        FROM metrics.your_table
        WHERE collected_at > NOW() - INTERVAL '5 minutes'
    `)
    if err != nil {
        return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
    }
    defer rows.Close()

    for rows.Next() {
        var mv MetricValue
        if err := rows.Scan(&mv.ConnectionID, &mv.Value,
            &mv.CollectedAt); err != nil {
            return nil, fmt.Errorf("failed to scan metric: %w", err)
        }
        results = append(results, mv)
    }
```

### Step 2: Add Historical Query

If the metric should support anomaly detection, add a case to the
`GetHistoricalMetricValues` function:

```go
case "your_new_metric_name":
    rows, err := d.pool.Query(ctx, `
        SELECT connection_id, NULL::text, your_value::float, collected_at
        FROM metrics.your_table
        WHERE collected_at > NOW() - INTERVAL '1 day' * $1
        ORDER BY connection_id, collected_at
    `, lookbackDays)
    // ... handle rows
```

### Step 3: Test the Metric

Verify the metric query returns expected values:

```sql
-- Check current values
SELECT * FROM metrics.your_table
WHERE collected_at > NOW() - INTERVAL '5 minutes';

-- Verify the alerter can query the metric
SELECT connection_id, your_value
FROM metrics.your_table
WHERE collected_at > NOW() - INTERVAL '5 minutes'
GROUP BY connection_id;
```

## Choosing Thresholds

Select thresholds based on your operational requirements:

### Critical Thresholds

Set critical thresholds for conditions that require immediate action:

- System running out of resources (disk, connections).
- Replication failures or significant lag.
- Security-related events.

### Warning Thresholds

Set warning thresholds for conditions that should be investigated:

- Resources approaching limits.
- Performance degradation.
- Unusual patterns that may indicate problems.

### Information Thresholds

Set informational thresholds for awareness:

- Routine events that should be logged.
- Conditions that may need future attention.

## Per-Connection Overrides

After creating a rule, you can customize thresholds for specific
connections:

```sql
INSERT INTO alert_thresholds (
    rule_id,
    connection_id,
    operator,
    threshold,
    severity,
    enabled
) VALUES (
    (SELECT id FROM alert_rules WHERE name = 'Your Rule Name'),
    5,  -- connection_id
    '>',
    150.0,  -- higher threshold for this connection
    'warning',
    true
);
```

## Disabling Rules

Disable a rule globally by updating the rule:

```sql
UPDATE alert_rules
SET default_enabled = false
WHERE name = 'Rule Name';
```

Disable a rule for a specific connection:

```sql
INSERT INTO alert_thresholds (rule_id, connection_id, enabled)
VALUES (
    (SELECT id FROM alert_rules WHERE name = 'Rule Name'),
    5,
    false
);
```

## Testing Rules

After creating a rule, verify it evaluates correctly:

1. Check the alerter debug logs for rule evaluation.
2. Temporarily lower the threshold to trigger an alert.
3. Verify the alert appears in the `alerts` table.
4. Restore the threshold to the intended value.
5. Verify the alert clears when the condition resolves.

## Rule Best Practices

Follow these guidelines when creating rules:

- Use descriptive names that explain what the rule detects.
- Write clear descriptions that help operators understand alerts.
- Choose appropriate categories for organization.
- Set thresholds based on historical data and operational experience.
- Start with warning severity and escalate to critical after validation.
- Document any dependencies or special considerations.
- Test rules thoroughly before enabling in production.

## Removing Rules

Delete a custom rule by removing it from the database:

```sql
-- First remove any overrides
DELETE FROM alert_thresholds
WHERE rule_id = (SELECT id FROM alert_rules WHERE name = 'Rule Name');

-- Then remove the rule
DELETE FROM alert_rules WHERE name = 'Rule Name';
```

Built-in rules should not be deleted. Disable them instead if you do
not need them.
