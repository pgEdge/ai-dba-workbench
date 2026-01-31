# Overview

The pgEdge AI DBA Workbench Alerter is a standalone background service that
monitors collected metrics and generates alerts. The alerter evaluates
threshold-based rules and uses AI-powered anomaly detection to identify
potential issues in your PostgreSQL databases.

## Purpose

The alerter serves as the monitoring brain of the AI DBA Workbench. The
collector gathers metrics from monitored PostgreSQL instances and stores
them in the datastore. The alerter periodically evaluates these metrics
against configured rules and baselines to detect problems.

The alerter provides the following capabilities:

- The threshold engine evaluates metrics against configurable limits.
- The anomaly detection system identifies unusual patterns in metric data.
- The baseline calculator maintains statistical profiles for normal behavior.
- The blackout scheduler suppresses alerts during maintenance windows.
- The alert lifecycle manager tracks alert states and automatic resolution.
- The notification system sends alerts through multiple channels.

## Key Concepts

### Threshold Alerts

Threshold alerts trigger when a metric value crosses a configured boundary.
Each alert rule specifies a metric name, comparison operator, and threshold
value. The alerter includes 24 built-in rules covering common PostgreSQL
monitoring scenarios such as connection utilization, replication lag, and
disk usage.

### Anomaly Detection

The anomaly detection system uses a tiered approach to identify unusual
metric values. Tier 1 performs statistical analysis using z-score
calculations. Tier 2 searches for similar past anomalies using vector
embeddings. Tier 3 uses LLM classification to determine if an anomaly is
a real issue or a false positive.

### Baselines

The alerter calculates metric baselines from historical data. Baselines
include statistical measures such as mean, standard deviation, minimum,
and maximum values. The alerter generates three types of baselines:

- Global baselines aggregate all historical data for a metric.
- Hourly baselines capture patterns by hour of day.
- Daily baselines capture patterns by day of week.

### Blackout Periods

Blackout periods suppress alert generation during scheduled
maintenance windows. The blackout system supports both manual and
scheduled blackouts across four hierarchical scope levels.

#### Scope Levels

Blackouts apply at four levels; each level cascades downward:

- An estate blackout suppresses alerts for all infrastructure.
- A group blackout suppresses alerts for every cluster in the group.
- A cluster blackout suppresses alerts for all servers in the cluster.
- A server blackout suppresses alerts for a single server only.

A blackout at a higher scope automatically applies to all children.
For example, a group-level blackout silences alerts for every
cluster and server within the group.

#### Manual Blackouts

A manual blackout defines a fixed time range with explicit start
and end timestamps. Administrators create manual blackouts for
one-time maintenance events such as upgrades or migrations.

#### Scheduled Blackouts

A scheduled blackout uses a cron expression to define recurring
maintenance windows. The blackout scheduler activates these
windows automatically at the specified times. See the
[Cron Expressions](cron-expressions.md) documentation for
expression syntax details.

#### REST API Endpoints

The server exposes the following endpoints for blackout management:

- `GET /api/v1/blackouts` retrieves all active blackouts.
- `POST /api/v1/blackouts` creates a new manual blackout.
- `DELETE /api/v1/blackouts/:id` removes an existing blackout.
- `GET /api/v1/blackout-schedules` retrieves all schedules.
- `POST /api/v1/blackout-schedules` creates a recurring schedule.
- `DELETE /api/v1/blackout-schedules/:id` removes a schedule.

#### RBAC Requirements

The `manage_blackouts` permission controls access to blackout
operations. Users without this permission can view blackout
status but cannot create or delete blackouts.

### Alert Lifecycle

Alerts progress through several states during their lifecycle:

- Active alerts indicate an ongoing condition requiring attention.
- Acknowledged alerts have been reviewed by an operator.
- Cleared alerts indicate the condition has resolved.

The alerter automatically clears threshold alerts when the triggering
condition returns to normal.

## Architecture

The alerter runs as a single process with multiple background workers:

- The threshold evaluator checks rules at a configurable interval.
- The baseline calculator refreshes baselines periodically.
- The anomaly detector processes tiered anomaly detection.
- The blackout scheduler activates scheduled maintenance windows.
- The alert cleaner checks for resolved conditions.
- The retention manager removes old alert data.
- The notification worker sends alerts through configured channels.
- The reminder worker sends periodic reminders for active alerts.

The alerter connects to the same PostgreSQL datastore used by the collector.
This shared datastore contains collected metrics, alert rules, baselines,
and alert history.

## Integration Points

The alerter integrates with other AI DBA Workbench components:

- The collector provides the metric data that the alerter evaluates.
- The server exposes APIs for managing alert rules and viewing alerts.
- The client displays alerts and provides acknowledgment interfaces.
- LLM providers power the Tier 3 anomaly classification.

## Next Steps

- Read the [Quick Start Guide](quickstart.md) to set up the alerter.
- Review the [Configuration Reference](configuration.md) for all options.
- Explore the [Alert Rules](alert-rules.md) documentation.
- Learn about [Anomaly Detection](anomaly-detection.md) capabilities.
