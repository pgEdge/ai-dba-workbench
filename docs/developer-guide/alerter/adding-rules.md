# Adding Alert Rules

This guide explains how to create custom alert rules for the
alerter. Custom rules extend the built-in rule set with monitoring
requirements specific to your organization.

## Prerequisites

Before creating a custom rule, ensure the following conditions
are met:

- The metric you want to monitor is collected by the collector.
- You understand the metric's normal value range.
- You have determined an appropriate threshold and severity.

## Creating a Rule

Alert rules are stored in the `alert_rules` table in the
datastore. You can create rules using SQL or through the API.

### Using SQL

In the following example, a custom rule monitors temporary file
usage:

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
    'Alerts when temporary files exceed 100 per hour',
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
| `required_extension` | text | An optional PostgreSQL extension the rule needs |
| `is_built_in` | boolean | Set to `false` for custom rules |

### Required Extensions

When a rule sets `required_extension`, the alerter checks the
newest `metrics.pg_extension` snapshot for each connection before
evaluating the rule, and skips any connection whose snapshot does
not list the extension in any database. The `pg_extension` probe
only writes a new snapshot when the installed set changes, so the
newest snapshot may be older than the metric values being
evaluated; no time window is applied to it. A connection with no
snapshot at all is treated as lacking the extension.

The same check applies when the alerter decides whether an existing
alert has resolved: an alert on a connection that lacks the
extension is left active rather than resolved, so uninstalling an
extension never reports a condition as cleared. Such an alert stays
active until an operator acknowledges or clears it.

## Metric Names

The metric name must match a metric that the alerter can query.
The alerter supports the following metric name patterns.

### Connection Metrics

- `pg_settings.max_connections` - The max_connections setting
  value.
- `connection_utilization_percent` - Connection usage percentage.
- `pg_stat_activity.blocked_count` - Number of blocked sessions.
- `pg_stat_activity.idle_in_transaction_seconds` - Idle
  transaction time.
- `pg_stat_activity.max_query_duration_seconds` - Longest running
  query.
- `pg_stat_activity.max_xact_duration_seconds` - Longest
  transaction.
- `pg_stat_activity.max_lock_wait_seconds` - Longest lock wait
  time.

### Replication Metrics

- `pg_stat_replication.replay_lag_seconds` - Replication lag in
  seconds.
- `pg_stat_replication.lag_bytes` - Replication lag in bytes.
- `pg_replication_slots.inactive` - Inactive replication slots.

### Database Metrics

- `pg_stat_database.cache_hit_ratio` - Buffer cache hit ratio.
- `pg_stat_database.deadlocks_delta` - Deadlocks in the last
  hour.
- `pg_stat_database.temp_files_delta` - Temporary files created
  in the last hour.

### Table Metrics

- `pg_stat_all_tables.dead_tuple_percent` - Dead tuple
  percentage.
- `table_last_autovacuum_hours` - Hours since last autovacuum.
- `age_percent` - Transaction ID age percentage.

### System Metrics

- `pg_sys_cpu_usage_info.processor_time_percent` - CPU usage
  percentage.
- `pg_sys_memory_info.used_percent` - Memory usage percentage.
- `pg_sys_disk_info.used_percent` - Disk usage percentage.
- `pg_sys_load_avg_info.load_avg_fifteen_minutes` - 15-minute
  load average.

### Other Metrics

- `pg_stat_archiver.failed_count_delta` - Failed archive attempts.
- `pg_stat_checkpointer.checkpoints_req_delta` - Requested
  checkpoints.
- `pg_stat_statements.slow_query_count` - Slow queries per
  interval.

## Adding Support for New Metrics

If you need a metric that is not currently supported, you must add
support in the alerter code before creating a rule.

### Step 1: Add the Metric Query

Edit `internal/database/metric_registry.go` and add an entry to
the `metricRegistry` map. The `latestSQL` field holds the query
that `GetLatestMetricValues` runs to fetch the current value for
each connection, and the `scan` field names the column layout the
query returns: `scanBasic` for `(connection_id, value,
collected_at)`, `scanWithDB` when a database name column follows
`connection_id`, and `scanWithDBObject` when an object name column
follows the database name.

In the following example, a new registry entry retrieves values
from a custom table:

```go
"your_new_metric_name": {
    latestSQL: `
        SELECT connection_id, your_value::float,
            collected_at
        FROM metrics.your_table
        WHERE collected_at > NOW()
            - INTERVAL '5 minutes'
    `,
    scan: scanBasic,
},
```

### Step 2: Add the Historical Query

Set the `historicalSQL` field on the same registry entry if the
metric should support anomaly detection. The baseline calculator
builds the `all`, `hourly` and `daily` baselines from this query,
and the `historicalScan` field names its column layout:
`historicalScanBasic` when the database name column is always
`NULL`, `historicalScanWithDB` when the column carries a database
name, and `historicalScanWithDBAndSamples` when the query returns
pre-aggregated rows with a trailing `sample_count` column.

A metric without a historical query is skipped by both baseline
calculation and anomaly detection; the alerter does not build a
baseline from the current value in its place. Threshold rules for
the metric still work. See the
[Anomaly Detection](anomaly-detection.md) page for the list of
built-in metrics that are excluded on this basis.

In the following example, a historical query retrieves data for
baseline calculations:

```go
"your_new_metric_name": {
    latestSQL: `...`,
    historicalSQL: `
        SELECT connection_id, NULL::text AS database_name,
            your_value::float, collected_at
        FROM metrics.your_table
        WHERE collected_at > NOW()
            - INTERVAL '1 day' * $1
        ORDER BY connection_id, collected_at
    `,
    scan:           scanBasic,
    historicalScan: historicalScanBasic,
},
```

### Step 3: Test the Metric

Verify the metric query returns expected values.

In the following example, the queries check current metric values
and verify the alerter can access the data:

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

## Registry Requirements

Every metric in the alerter's registry
(`internal/database/metric_registry.go`) must satisfy three further
requirements that govern how the alert cleaner reads its results.

### Freshness Cutoffs

Each `latestSQL` query must bound `collected_at` with a
`NOW() - INTERVAL` cutoff of at least three probe intervals. Three
intervals leave room for two consecutive samples inside the window,
so that a delta metric still has a predecessor, whilst one late
collection cannot empty the window. A metric whose probe runs every
300 seconds therefore uses a 15 minute cutoff.

For a metric that clears on absent data the three intervals are a
hard requirement rather than a guideline, and
`TestMetricRegistryAbsenceWindowCoversProbeInterval` enforces it
against the intervals the collector seeds into `probe_configs`. A
window of one interval holds a single sample, so one collection
arriving later than the probe's own runtime empties the query, and
the cleaner reads that as a recovery.

Without a cutoff, the query returns the newest row the table still
holds, however old that row is. An alert then goes on firing on data
that is days old after the collector stops, or after the connection
stops being monitored, until retention purges the partition.

In the following example, the cutoff restricts a latest query to the
last three samples of a 300 second probe:

```sql
SELECT connection_id, your_value::float, collected_at
FROM metrics.your_table
WHERE collected_at > NOW() - INTERVAL '15 minutes';
```

A metric may omit the cutoff only where freshness is not meaningful.
The `pg_settings.max_connections` metric is the one current
exception, because the settings probe is change-tracked and stores
nothing whilst the configuration is unchanged, so any maximum age
predicate would silence the metric on a stable server. The
`TestMetricRegistryLatestSQLFreshnessCutoff` test in
`internal/database/audit_defects_test.go` enforces the requirement
and holds the allowlist of exceptions, so a metric without a cutoff
must be added to that allowlist with a reason.

### Clearing on Missing Data

The `clearWhenAbsent` field on `metricQueryConfig` tells the alert
cleaner what a missing row means. The cleaner consults the field
through `Datastore.MetricClearsWhenAbsent` whenever the latest query
returns no row for an active alert's connection and database, or no
rows at all.

Choose the value from the shape of the query:

- Leave the field at its default of `false` when the query emits a
  row for every healthy connection, as a CPU percentage or a cache
  hit ratio does. A connection that disappears from the result set
  has stopped reporting rather than recovered, so the alert stays
  active until fresh data shows the condition has ended, and the
  `metric_staleness` rule reports the stalled probe.
- Set the field to `true` when the query emits a row only whilst the
  condition holds, as a blocked backend count does, or when the query
  describes an object that an operator can legitimately drop, such as
  a replication slot or a standby. A missing row is then the recovery
  signal, so the cleaner clears the alert.

Every entry that sets `clearWhenAbsent` to `true` carries a comment
on the registry entry explaining why absence means recovery for that
metric; add one alongside the field. A metric name that the registry
does not know returns `false`, so a metric evaluated outside the
registry never clears on absent data.

### Declaring the Window

Every entry that sets `clearWhenAbsent` to `true` also sets
`absenceWindow` to the interval its latest query looks back over,
written as a `time.Duration`: a query carrying a 15 minute cutoff
declares `15 * time.Minute`. Where a query has more than one cutoff,
the window is the shortest of them, since that is the bound which
decides whether the result is empty.

The cleaner reads the field through
`Datastore.MetricAbsenceWindow`, and
`TestMetricRegistryAbsenceWindowMatchesSQL` checks the declared value
against the SQL literal, so the two cannot drift apart. Entries that
do not clear on absent data leave the field unset.

### Naming the Collector Probe

Every registry entry also sets `probeName` to the collector probe
that fills the metrics table its latest query reads, such as
`pg_stat_activity`, `pg_replication_slots` or `pg_stat_database`. The
`TestMetricRegistryProbeName` test requires the name to match a table
the query selects from, so an entry whose query reads a table other
than its metric prefix suggests names that table's probe: the
`pg_stat_archiver.failed_count_delta` metric reads
`metrics.pg_stat_wal`, for example.

The cleaner uses the probe name to tell a condition that ended from
data that stopped arriving. Because every query bounds `collected_at`,
a stopped collector empties the result set exactly as a recovered
condition does, so an alert on a `clearWhenAbsent` metric clears only
when that probe last collected for the alert's connection inside the
metric's `absenceWindow`. A probe that has stalled, that an operator
has disabled, or that belongs to a connection which is no longer
monitored leaves the alert active until somebody clears or
acknowledges it, which is the safe direction: the alternative is
reporting a resolution that nobody observed.

The window is the yardstick here, and not the probe's configured
collection interval, because the two move independently: an operator
may raise `probe_configs.collection_interval_seconds`, for every
connection or for one, whilst the window stays where the query's SQL
puts it. A check counted in intervals would then widen along with the
interval and stay quiet exactly where ordinary collection starts
emptying the query. Comparing the probe's last collection against the
window holds at any interval, and needs to know nothing about how the
probe is configured. The `metric_staleness` rule still works in
ratios, because what counts as a late probe does depend on how often
it is meant to run.

## Choosing Thresholds

Select thresholds based on your operational requirements.

### Critical Thresholds

Set critical thresholds for conditions that require immediate
action:

- System running out of resources (disk, connections).
- Replication failures or significant lag.
- Security-related events.

### Warning Thresholds

Set warning thresholds for conditions that should be
investigated:

- Resources approaching limits.
- Performance degradation.
- Unusual patterns that may indicate problems.

### Information Thresholds

Set informational thresholds for awareness:

- Routine events that should be logged.
- Conditions that may need future attention.

## Per-Connection Overrides

After creating a rule, you can customize thresholds for specific
connections.

In the following example, an override sets a higher threshold for
a specific connection:

```sql
INSERT INTO alert_thresholds (
    rule_id,
    connection_id,
    operator,
    threshold,
    severity,
    enabled
) VALUES (
    (SELECT id FROM alert_rules
     WHERE name = 'Your Rule Name'),
    5,  -- connection_id
    '>',
    150.0,  -- higher threshold for this connection
    'warning',
    true
);
```

## Disabling Rules

Disable a rule globally by updating the rule.

In the following example, the `UPDATE` statement disables a rule
for all connections:

```sql
UPDATE alert_rules
SET default_enabled = false
WHERE name = 'Rule Name';
```

In the following example, an override disables a rule for a
specific connection:

```sql
INSERT INTO alert_thresholds (rule_id, connection_id, enabled)
VALUES (
    (SELECT id FROM alert_rules
     WHERE name = 'Rule Name'),
    5,
    false
);
```

## Testing Rules

After creating a rule, verify the rule evaluates correctly:

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
- Set thresholds based on historical data and operational
  experience.
- Start with warning severity and escalate to critical after
  validation.
- Document any dependencies or special considerations.
- Test rules thoroughly before enabling rules in production.

## Removing Rules

Delete a custom rule by removing the rule from the database.

In the following example, the `DELETE` statements remove a custom
rule and any associated overrides:

```sql
-- First remove any overrides
DELETE FROM alert_thresholds
WHERE rule_id = (
    SELECT id FROM alert_rules
    WHERE name = 'Rule Name'
);

-- Then remove the rule
DELETE FROM alert_rules WHERE name = 'Rule Name';
```

Built-in rules should not be deleted. Disable built-in rules
instead if you do not need the rules.
