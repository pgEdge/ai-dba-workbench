# Understanding Alert Behavior

This page describes how the alerter resolves the threshold, operator, and
severity for each server from the override hierarchy, and how an alert moves
from active to cleared, including during a blackout.
[Managing Alert Rules](index.md) describes how to edit rule defaults and
overrides and how to stop monitoring a server in the Workbench.

## Hierarchical Overrides

Alert thresholds can be customized at multiple levels of the server hierarchy.
The alerter resolves the effective threshold using the following precedence
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
| `connection_id` | The connection that a server-level override applies to. |
| `cluster_id` | The cluster that a cluster-level override applies to. |
| `group_id` | The group that a group-level override applies to. |
| `database_name` | An optional database within the connection. |
| `operator` | The comparison operator for this override. |
| `threshold` | The threshold value for this override. |
| `severity` | The severity level for this override. |
| `enabled` | Whether the rule is enabled at this scope. |

The `alert_thresholds` table stores an override; each row sets only the
identifier column that matches its scope and leaves the other two `NULL`. The
REST API takes the scope and its identifier from the request URL instead, as
in `/api/v1/alert-overrides/{scope}/{scopeId}/{ruleId}`.

When evaluating a rule for a server, the alerter checks for a server-level
override first. If none exists, the alerter checks the cluster that contains
the server, then the group that contains the cluster. If no override exists at
any level, the alerter uses the global default values.

### Override Evaluation Order

The alerter evaluates thresholds using a strict precedence order for each
server and database combination:

1. The alerter checks for a server-level override first.
2. The alerter checks the cluster override if no server override exists.
3. The alerter checks the group override if no cluster override exists.
4. The alerter applies global defaults when no overrides exist at any level.

Database matching applies only to server-level overrides. A server-level
override with a `NULL` database name acts as a wildcard that matches any
database on the server, and a server-level override for a specific database
takes precedence over the wildcard override for that database. The alerter
ignores the database name when it looks up cluster and group overrides, so a
cluster or group override applies to every database on each member server.

### Auto-Detected Clusters

The system automatically detects cluster membership by analyzing replication
topology. The Workbench server identifies clusters through Spock replication,
binary replication, and logical replication connections between servers. The
Workbench groups Spock nodes by the prefix of the server name; for details, see
[Using the Workbench](../../user-guide/index.md).

When the Workbench server detects that servers participate in the same
replication topology, the system groups the servers into an auto-detected
cluster. The auto-detected cluster and the parent group appear in the scope
dropdown when a user edits overrides for any member server.

## Alert Lifecycle

When a threshold has been violated on `threshold.trigger_count` consecutive
metric samples (two by default), the alerter creates an alert with status
`active`. A sample is one collection by the collector, so the evaluator reading
the same sample again does not count toward the total. The `metric_staleness`
and `probe_unavailable` rules and connection error alerts do not use
`threshold.trigger_count`. The alert remains active until one of the following
occurs:

- The condition resolves and the alerter clears the alert automatically.
- An operator acknowledges the alert manually.
- An operator marks the alert as a false positive.

The alerter updates the `metric_value` field of an active threshold alert on
each evaluation cycle while the threshold remains violated, so the field
reflects the latest violating value. An active anomaly alert updates
`metric_value` only for a sample outside the expected band; while the alert
counts in-band samples toward `clear_count`, the field keeps the last
out-of-band value rather than the current reading.

## Automatic Alert Clearing

The alerter automatically clears threshold alerts when the triggering condition
returns to normal. The alert cleaner worker runs every 30 seconds and
re-evaluates active alerts. When a metric value no longer violates the
threshold on `threshold.clear_count` consecutive metric samples (three by
default), the alerter marks the alert as `cleared` and records the `cleared_at`
timestamp. A sample that violates the threshold again restarts the count. The
[alerter configuration](../../configuration/alerter/index.md) describes both options.

The `high_max_connections` rule reads `pg_settings.max_connections` from the
change-tracked `pg_settings` probe, which stores a new sample only when a
server's settings change; a server whose settings stay the same supplies no
further samples. With `threshold.trigger_count` above 1, the rule raises an
alert only after another settings change, and with `threshold.clear_count`
above 1, the alert clears only after further settings changes. Setting both
options to 1 lets the rule raise and clear its alert on a single sample.
However, both options apply to every threshold rule, so a value of 1 also lets
a single breaching sample raise an alert for each of the other rules.

The alerter does not treat missing data as a resolved condition. When a metric
reports a value for every monitored connection, such as a CPU percentage or a
cache hit ratio, a connection that stops returning a value has stopped
reporting rather than recovered; the alerter leaves the alert active, and the
`metric_staleness` rule reports the probe that stopped. When a metric reports
only while the condition holds, such as an inactive replication slot or a
blocked session, the absence of a value is the recovery signal. Each probe
collection that returns no value counts as one sample toward
`threshold.clear_count`, and the alerter clears the alert when enough samples
have passed. With the default of three and the default interval of the
`pg_replication_slots` probe, an inactive replication slot alert clears ten to
fifteen minutes after the slot recovers.

The alerter does not apply `threshold.clear_count` to `metric_staleness`
alerts. When the probe collects again, the alerter clears the staleness alert
on the next cleanup pass in which the probe staleness ratio no longer violates
the rule's threshold condition, as the rule's operator defines it; for example,
under a `>=` rule a ratio equal to the threshold keeps the alert active. A probe can
become unavailable because the monitored server no longer offers the extension
the probe reads or because the probe run timed out; in either case, a staleness
alert that has already fired for the probe stays active. The alerter rewrites
the alert description to report that collection has stopped and to name the
reason the collector recorded, and leaves the alert title alone so that the
alert stays recognizable in notification history. The alerter restores the
evaluator's original wording when the probe collects again, so the clear
notification and the stored history describe the recovery rather than the
outage.

When an operator disables a probe, the alerter clears the staleness alert,
because disabling a probe represents a deliberate change rather than a fault.
The alerter restores the original wording on that path too, rebuilding it from
the connection and probe configuration, so a cleared alert does not report that
collection has stopped. When the connection and its probe configuration have
also gone, nothing remains to rebuild the wording from, and the alert clears as
it stands. The alerter logs whichever decision it takes at the normal log
level, once when the decision changes rather than on every cleanup cycle.

This hold preserves an alert that is already active; it does not raise a new
one. The staleness rule never fires for an unavailable probe, because an
unavailable probe is the normal steady state on a server that lacks the
extension a probe reads, and firing on one would leave a permanent alert
against every such probe. The `probe_unavailable` rule instead reports a probe
that stops collecting before its staleness ratio reaches the rule's threshold.
The seeded staleness rule shows why the second rule is needed; the staleness
rule fires on a ratio above 3, and a probe that runs every 60 seconds is still
at a ratio of about 1.0 at the moment the collector records the probe as
unavailable.

The alert cleaner checks a `probe_unavailable` alert against the same probe
availability data that the evaluator reads, rather than against the staleness
ratio, so the two probe-scoped rules resolve independently of each other.

## Anomaly Alert Clearing

The alerter also clears anomaly alerts automatically. An anomaly alert has no
rule threshold, so the anomaly detector re-scores the metric behind each active
anomaly alert on every tier 1 evaluation, using the same baseline and
sensitivity that raised the alert. The alerter clears the alert and sends the
usual clear notification when `anomaly.tier1.clear_count` consecutive samples
of the metric have scored inside the sensitivity band.

Only a newly collected sample counts toward the total. Tier 1 evaluates every
60 seconds by default, while most probes collect every 300 to 600 seconds; an
evaluation that re-scores a sample already counted neither advances nor
restarts the count. The default of 3 therefore clears an alert on the third
in-band sample, about ten minutes after the first for a probe that collects
every 300 seconds. A value under the metric's `min_value` floor in
`anomaly.tier1.metric_floors` counts as inside the band, because the detector
never treats such a value as anomalous.

While the value stays outside the band, the alerter updates the alert's metric
value, anomaly score, and last updated time, and raises the severity when the
deviation grows. The severity never falls, and the description keeps the values
at which the alert fired. The alerter discards a cached AI analysis when the
metric value changes.

The alerter holds an anomaly alert open, and keeps the count, on any evaluation
in which the metric or connection does not report, since the alerter missed no
sample. Metrics that report only while their condition holds, such as blocked
sessions or long-running transactions, are the exception. For these metrics, a
missing value means the condition has passed, so each new collection by the
metric's probe that reports no value counts as an in-band sample, provided the
probe is still collecting for the connection.

The alerter also holds the alert open and restarts the count in the following
cases:

- A sample falls outside the band.
- The baseline is not usable.
- A blackout covers the connection or database.

The alerter holds the count in memory, so the count also restarts when the
alerter restarts or when an administrator disables tier 1. The alerter does not
clear acknowledged anomaly alerts this way and leaves them to the re-evaluation
worker. Recovery stops whenever anomaly detection is disabled, including when
the alerter disables anomaly detection because no enabled tier has a usable LLM
provider; recovery itself makes no embedding or LLM call.

On upgrade, the alerter closes any active anomaly alert that it can no longer
re-score, without sending a clear notification. The alerter closes the
following anomaly alerts this way:

- An alert on a metric that anomaly detection no longer covers is closed.
- An alert with no database on a metric measured per database is closed;
  earlier releases raised such alerts for the cache hit ratio, deadlock, and
  temporary file metrics.

The description of each closed alert begins `Closed without re-evaluation:` and
gives the reason.

After an anomaly alert clears, the alerter raises no new anomaly alert for the
same metric, connection, and database for five minutes, so a value at the edge
of the band does not raise and clear an alert repeatedly.

## Blackout Interaction

During an active blackout period, the alerter suppresses new alerts for the
affected connection or database. A blackout holds an open anomaly alert open
until the blackout ends. A blackout does not hold a threshold alert open; when
the metric returns within the threshold during a blackout, the alerter clears
the threshold alert and queues a clear notification as usual.

## Example Rule Configuration

In the following example, a rule monitors connection utilization:

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

In the following example, a group-level override adjusts the threshold for all
servers in a development group:

```yaml
rule_id: 1
scope: group
group_id: 3
operator: ">"
threshold: 95.0
severity: info
enabled: true
```

In the following example, a server-level override further customizes the
threshold for one production server with higher connection requirements:

```yaml
rule_id: 1
scope: server
connection_id: 5
operator: ">"
threshold: 90.0
severity: warning
enabled: true
```
