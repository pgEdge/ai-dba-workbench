# Understanding Probe Behavior

This page describes how the collector resolves the settings for each probe from
the override hierarchy, when configuration changes take effect, and how often
each kind of probe collects data.
[Managing Probes](index.md) describes how to change probe settings in the
Workbench, with SQL, and through the REST API.

## Hierarchical Probe Overrides

Probe settings can be customized at multiple levels of the server hierarchy.
The override system uses the following precedence order:

1. Server overrides apply to a specific connection.
2. Cluster overrides apply to all servers in a cluster.
3. Group overrides apply to all clusters in a group.
4. Global default settings apply when no override exists.
5. Hardcoded default values apply if no database configuration exists.

When a monitored connection first needs a probe, the Collector creates a
server-level configuration for the connection by copying the values from the
first level of the hierarchy that has a configuration. A later change to a
cluster, group, or global setting does not change a connection that already
has a server-level configuration for that probe.

## Automatic Configuration Reload

The Collector automatically reloads probe configurations from the database
every 5 minutes. Changes to `collection_interval_seconds`, `retention_days`, or
`is_enabled` in the server-level configuration that a connection uses take
effect within 5 minutes without requiring a restart. A change to a cluster,
group, or global setting reaches only the connections that do not yet have a
server-level configuration for the probe.

## Default Collection Intervals

Different probe types have different default collection intervals based on how
frequently their data changes:

- Fast probes run every 30 to 60 seconds and cover replication and activity
  data.
- Normal probes run every 300 seconds (5 minutes) and cover most statistics.
- Slow probes run every 600 seconds (10 minutes) and cover checkpointer and WAL
  data.
- Very slow probes run every 900 seconds (15 minutes) and cover I/O statistics.
- Change-tracked probes run every 3600 seconds (1 hour) and only store data
  when changes are detected.

## Collection Frequency Guidelines

Balance collection frequency against the following trade-offs:

- More frequent collection provides more current data.
- More frequent collection generates more queries on monitored servers.
- More frequent collection produces more data points and increases storage
  usage.

The following guidelines apply to common probe types:

- Fast-changing data such as replication lag should use 30 to 60 second
  intervals.
- Moderate data such as table statistics should use 300 second (5 minute)
  intervals.
- Slow-changing data such as archiver statistics should use 600 second (10
  minute) or longer intervals.

## Intervals and Alerting

Keep each collection interval well inside the windows that the alert rules
read. Each alerting metric considers only the samples collected in the last
few minutes, commonly 5 or 15 minutes depending on the probe. An interval
raised beyond that window leaves the rule query with nothing to read most of
the time; rules on that probe stop firing, and an alert that is already active
stays active, because the alerter does not report a condition resolved on data
it cannot see. As a rule of thumb, an interval of a third of the window or less
is safe; the shipped defaults follow this rule.
