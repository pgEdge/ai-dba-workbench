# Alert Rules

Alert rules define the conditions that trigger
threshold-based alerts. Each rule specifies a metric to
monitor, a comparison operator, and a threshold value.
The alerter includes 30 built-in rules and supports
custom rules.

## Rule Structure

Each alert rule contains the following fields:

| Field | Description |
|-------|-------------|
| `name` | A human-readable name for the rule. |
| `description` | A detailed explanation of what the rule detects. |
| `category` | The category grouping for the rule. |
| `metric_name` | The metric identifier to evaluate. |
| `default_operator` | The comparison operator. |
| `default_threshold` | The threshold value for comparison. |
| `default_severity` | The alert severity (`critical`, `warning`, `info`). |
| `default_enabled` | Whether the rule is enabled by default. |
| `required_extension` | An optional PostgreSQL extension required. |
| `is_built_in` | Indicates whether the rule is built-in. |

## Comparison Operators

The alerter supports six comparison operators:

- `>` triggers when the metric value is greater than
  the threshold.
- `>=` triggers when the metric value is at least the
  threshold.
- `<` triggers when the metric value is less than the
  threshold.
- `<=` triggers when the metric value is at most the
  threshold.
- `==` triggers when the metric value equals the
  threshold.
- `!=` triggers when the metric value does not equal
  the threshold.

## Severity Levels

Alert rules use three severity levels:

- `critical` indicates a severe issue requiring
  immediate attention.
- `warning` indicates a potential problem that should
  be investigated.
- `info` indicates an informational condition for
  awareness.

## Rule Categories

Built-in rules are organized into the following
categories:

- Connection rules monitor database connections and
  session state.
- Replication rules monitor replication lag, slot
  status, Spock exceptions, and Spock conflict
  resolutions.
- Performance rules monitor query performance and
  locking.
- Storage rules monitor disk usage and table
  maintenance.
- System rules monitor CPU, memory, and system
  resources.

## Replication Rules

The replication category includes built-in rules that
monitor Spock exception activity, Spock conflict
auto-resolutions, and replication-slot WAL retention.
The Spock rules require the `spock` extension on the
monitored database; the slot retention rules apply to
every PostgreSQL deployment.

The Spock recent-count rules read from the
`spock_exception_log` and `spock_resolutions` probe
tables, both of which capture a rolling 15-minute
window. The alerter clears each Spock alert
automatically as the corresponding rows age out of the
window and the recent count returns below the
threshold.

The following bullets describe the built-in
replication rules added for Spock and slot retention
monitoring:

- `spock_recent_exceptions_present` fires at warning
  severity when `spock_exception_log.recent_count` is
  greater than or equal to 1; the rule requires the
  `spock` extension.
- `spock_recent_exceptions_high` fires at critical
  severity when `spock_exception_log.recent_count` is
  greater than or equal to 10; the rule requires the
  `spock` extension.
- `spock_recent_resolutions_present` fires at warning
  severity when `spock_resolutions.recent_count` is
  greater than or equal to 1; the rule requires the
  `spock` extension.
- `spock_recent_resolutions_high` fires at critical
  severity when `spock_resolutions.recent_count` is
  greater than or equal to 25; the rule requires the
  `spock` extension.
- `replication_slot_retention_warn` fires at warning
  severity when `pg_replication_slots.max_retained_bytes`
  is greater than or equal to 1073741824 (1 GiB); the
  rule has no extension requirement.
- `replication_slot_retention_high` fires at critical
  severity when `pg_replication_slots.max_retained_bytes`
  is greater than or equal to 10737418240 (10 GiB); the
  rule has no extension requirement.

The slot retention rules evaluate the maximum retained
WAL across all replication slots on a server; the
alerter fires a rule when any single slot retains more
WAL than the threshold permits.

## Hierarchical Overrides

Alert thresholds can be customized at multiple levels
of the server hierarchy. The alerter resolves the
effective threshold using the following precedence
order:

1. Server overrides apply to a specific connection.
2. Cluster overrides apply to all servers in a cluster.
3. Group overrides apply to all clusters in a group.
4. Global defaults apply when no override exists.

An override specifies the following fields:

| Field | Description |
|-------|-------------|
| `rule_id` | The alert rule to override. |
| `scope` | The override level: server, cluster, or group. |
| `scope_id` | The identifier for the connection, cluster, or group. |
| `database_name` | An optional database within the connection. |
| `operator` | The comparison operator for this override. |
| `threshold` | The threshold value for this override. |
| `severity` | The severity level for this override. |
| `enabled` | Whether the rule is enabled at this scope. |

When evaluating a rule for a server, the alerter checks
for a server-level override first. If none exists, the
alerter checks the cluster that contains the server,
then the group that contains the cluster. If no
override exists at any level, the alerter uses the
global default values.

### Override Evaluation Order

The alerter evaluates thresholds using a strict
precedence order for each server and database
combination:

1. The alerter checks for a server-level override
   first.
2. The alerter checks the cluster override if no
   server override exists.
3. The alerter checks the group override if no cluster
   override exists.
4. The alerter applies global defaults when no
   overrides exist at any level.

A `NULL` database name in an override acts as a
wildcard. The wildcard override matches any database on
the server. A database-specific override takes
precedence over a wildcard override at the same scope
level.

### Auto-Detected Clusters

The system automatically detects cluster membership by
analyzing replication topology. The alerter identifies
clusters through Spock replication, binary replication,
and logical replication connections between servers.

When the alerter detects that servers participate in
the same replication topology, the system groups the
servers into an auto-detected cluster. The auto-detected
cluster and the parent group appear in the scope
dropdown when a user edits overrides for any member
server.

### Editing Overrides from Alerts

Users can edit an override directly from an alert
instance. The edit button on an alert instance opens
the Edit Override dialog for the associated rule and
scope.

The Edit Override dialog allows the user to adjust the
threshold, operator, severity, and enabled state. The
scope dropdown displays the available override levels:
server, cluster, and group. The dialog pre-selects the
scope that matches the alert's originating context.

### Scope Disabling Logic

The Edit Override dialog disables scope levels that
would have no practical effect. Scopes above the
highest existing override are disabled in the dropdown.
For example, if a server-level override exists for a
rule, the cluster and group scope options are disabled.
Editing the cluster or group override would have no
effect because the more specific server-level override
takes precedence.

This behavior prevents users from creating overrides
that the alerter would never apply. The dialog displays
a tooltip on disabled options to explain why the scope
is unavailable.

### Managing Overrides

Overrides are managed through the Alert Overrides tab
in the server, cluster, or group edit dialogs. The
override panel shows all alert rules with their current
effective settings. Rules without an override at the
current level appear dimmed to indicate that the
setting is inherited from a higher level or the global
default.

## Enabling and Disabling Rules

Rules can be enabled or disabled globally or at any
level of the hierarchy. A disabled rule is not evaluated
during threshold checks. You can disable built-in rules
that do not apply to your environment or enable rules
that require specific PostgreSQL extensions.

To disable a rule globally, set `default_enabled` to
`false` in the rule definition. To disable a rule for
a specific scope, create an override with `enabled` set
to `false`.

## Creating Custom Rules

Custom rules extend the built-in rule set with
organization-specific monitoring requirements. Custom
rules follow the same structure as built-in rules but
have `is_built_in` set to `false`.

When creating custom rules, consider the following
guidelines:

- The metric must be collected by the collector.
- The metric name must match the collector's metric
  naming convention.
- The threshold should reflect your organization's
  operational requirements.
- The severity should match the impact of the
  condition.

## Alert Lifecycle

When a threshold is violated, the alerter creates an
alert with status `active`. The alert remains active
until one of the following occurs:

- The condition resolves and the alerter clears the
  alert automatically.
- An operator acknowledges the alert manually.
- An operator marks the alert as a false positive.

The alerter updates the `metric_value` field of active
alerts on each evaluation cycle. This update reflects
the current value even if the threshold remains
violated.

## Automatic Alert Clearing

The alerter automatically clears threshold alerts when
the triggering condition returns to normal. The alert
cleaner worker runs every 30 seconds and re-evaluates
active alerts. When a metric value no longer violates
the threshold, the alerter marks the alert as `cleared`
and records the `cleared_at` timestamp.

## Blackout Interaction

During an active blackout period, the alerter
suppresses new alerts for the affected connection or
database. Existing active alerts are not cleared during
a blackout; the blackout only prevents new alerts from
being created.

## Example Rule Configuration

In the following example, a rule monitors connection
utilization:

```yaml
name: High Connection Utilization
description: >
  Alerts when database connections exceed 80%
  of max_connections
category: connection
metric_name: connection_utilization_percent
default_operator: ">"
default_threshold: 80.0
default_severity: warning
default_enabled: true
```

In the following example, a group-level override
adjusts the threshold for all servers in a development
group:

```yaml
rule_id: 1
scope: group
scope_id: 3
operator: ">"
threshold: 95.0
severity: info
enabled: true
```

In the following example, a server-level override
further customizes the threshold for one production
server with higher connection requirements:

```yaml
rule_id: 1
scope: server
scope_id: 5
operator: ">"
threshold: 90.0
severity: warning
enabled: true
```

## Related Documentation

- [Notification Channels](notification-channels.md)
  covers alert delivery configuration.
- [Probes](probes.md) describes the data collection
  that feeds alert rules.
