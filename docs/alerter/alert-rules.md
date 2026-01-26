# Alert Rules

Alert rules define the conditions that trigger threshold-based alerts.
Each rule specifies a metric to monitor, a comparison operator, and a
threshold value. The alerter includes 24 built-in rules and supports
custom rules.

## Rule Structure

Each alert rule contains the following fields:

| Field | Description |
|-------|-------------|
| `name` | A human-readable name for the rule |
| `description` | A detailed explanation of what the rule detects |
| `category` | The category grouping for the rule |
| `metric_name` | The metric identifier to evaluate |
| `default_operator` | The comparison operator |
| `default_threshold` | The threshold value for comparison |
| `default_severity` | The alert severity (`critical`, `warning`, `info`) |
| `default_enabled` | Whether the rule is enabled by default |
| `required_extension` | Optional PostgreSQL extension required |
| `is_built_in` | Indicates if the rule is a built-in rule |

## Comparison Operators

The alerter supports six comparison operators:

- `>` triggers when the metric value is greater than the threshold.
- `>=` triggers when the metric value is at least the threshold.
- `<` triggers when the metric value is less than the threshold.
- `<=` triggers when the metric value is at most the threshold.
- `==` triggers when the metric value equals the threshold.
- `!=` triggers when the metric value does not equal the threshold.

## Severity Levels

Alert rules use three severity levels:

- `critical` indicates a severe issue requiring immediate attention.
- `warning` indicates a potential problem that should be investigated.
- `info` indicates an informational condition for awareness.

## Rule Categories

Built-in rules are organized into categories:

- Connection rules monitor database connections and session state.
- Replication rules monitor replication lag and slot status.
- Performance rules monitor query performance and locking.
- Storage rules monitor disk usage and table maintenance.
- System rules monitor CPU, memory, and system resources.

## Per-Connection Overrides

You can customize threshold values for specific connections or databases.
Per-connection overrides allow you to set different thresholds based on
the workload characteristics of each monitored instance.

An override specifies:

| Field | Description |
|-------|-------------|
| `rule_id` | The alert rule to override |
| `connection_id` | The connection to apply the override to |
| `database_name` | Optional database within the connection |
| `operator` | The comparison operator for this override |
| `threshold` | The threshold value for this override |
| `severity` | The severity level for this override |
| `enabled` | Whether the rule is enabled for this connection |

When evaluating a rule, the alerter checks for a per-connection override
first. If no override exists, the alerter uses the rule's default values.

## Enabling and Disabling Rules

Rules can be enabled or disabled globally or per-connection. A disabled
rule is not evaluated during threshold checks. You can disable built-in
rules that do not apply to your environment or enable rules that require
specific PostgreSQL extensions.

To disable a rule globally, set `default_enabled` to `false` in the rule
definition. To disable a rule for a specific connection, create an override
with `enabled` set to `false`.

## Creating Custom Rules

Custom rules extend the built-in rule set with organization-specific
monitoring requirements. Custom rules follow the same structure as built-in
rules but have `is_built_in` set to `false`.

When creating custom rules, consider:

- The metric must be collected by the collector.
- The metric name must match the collector's metric naming convention.
- The threshold should reflect your organization's operational requirements.
- The severity should match the impact of the condition.

## Alert Lifecycle

When a threshold is violated, the alerter creates an alert with status
`active`. The alert remains active until one of the following occurs:

- The condition resolves and the alerter clears the alert automatically.
- An operator acknowledges the alert manually.
- An operator marks the alert as a false positive.

The alerter updates the `metric_value` field of active alerts on each
evaluation cycle. This update reflects the current value even if the
threshold remains violated.

## Automatic Alert Clearing

The alerter automatically clears threshold alerts when the triggering
condition returns to normal. The alert cleaner worker runs every 30
seconds and re-evaluates active alerts. When a metric value no longer
violates the threshold, the alerter marks the alert as `cleared` and
records the `cleared_at` timestamp.

## Blackout Interaction

During an active blackout period, the alerter suppresses new alerts for
the affected connection or database. Existing active alerts are not
cleared during a blackout; the blackout only prevents new alerts from
being created.

## Example Rule Configuration

In the following example, a rule monitors connection utilization:

```yaml
name: High Connection Utilization
description: Alerts when database connections exceed 80% of max_connections
category: connection
metric_name: connection_utilization_percent
default_operator: ">"
default_threshold: 80.0
default_severity: warning
default_enabled: true
```

In the following example, a per-connection override increases the
threshold for a production database with higher connection requirements:

```yaml
rule_id: 1
connection_id: 5
operator: ">"
threshold: 90.0
severity: warning
enabled: true
```

## Related Documentation

- [Rule Reference](rule-reference.md) lists all built-in rules.
- [Adding Rules](adding-rules.md) explains how to create custom rules.
- [Configuration Reference](configuration.md) covers threshold settings.
