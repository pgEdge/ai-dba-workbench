# Alerter Architecture

The pgEdge AI DBA Workbench Alerter is a standalone background
service that monitors collected metrics and generates alerts. The
alerter evaluates threshold-based rules and uses AI-powered anomaly
detection to identify potential issues in PostgreSQL databases.

## Purpose

The alerter serves as the monitoring brain of the AI DBA Workbench.
The collector gathers metrics from monitored PostgreSQL instances
and stores the metrics in the datastore. The alerter periodically
evaluates these metrics against configured rules and baselines to
detect problems.

The alerter provides the following capabilities:

- The threshold engine evaluates metrics against configurable limits.
- The anomaly detection system identifies unusual metric patterns.
- The baseline calculator maintains statistical profiles for normal
  behavior.
- The blackout scheduler suppresses alerts during maintenance
  windows.
- The alert lifecycle manager tracks alert states and automatic
  resolution.
- The notification system sends alerts through multiple channels.

## Key Concepts

### Threshold Alerts

Threshold alerts trigger when a metric value crosses a configured
boundary. Each alert rule specifies a metric name, comparison
operator, and threshold value. The alerter includes 24 built-in
rules covering common PostgreSQL monitoring scenarios such as
connection utilization, replication lag, and disk usage.

### Anomaly Detection

The anomaly detection system uses a tiered approach to identify
unusual metric values. Tier 1 performs statistical analysis using
z-score calculations. Tier 2 searches for similar past anomalies
using vector embeddings. Tier 3 uses LLM classification to
determine if an anomaly is a real issue or a false positive. See
[Anomaly Detection](anomaly-detection.md) for full details.

### Baselines

The alerter calculates metric baselines from historical data.
Baselines include statistical measures such as mean, standard
deviation, minimum, and maximum values. The alerter generates
three types of baselines:

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
- A group blackout suppresses alerts for every cluster in the
  group.
- A cluster blackout suppresses alerts for all servers in the
  cluster.
- A server blackout suppresses alerts for a single server only.

A blackout at a higher scope automatically applies to all
children. For example, a group-level blackout silences alerts
for every cluster and server within the group.

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

The server exposes the following endpoints for blackout
management:

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

The alerter automatically clears threshold alerts when the
triggering condition returns to normal.

## High-Level Architecture

The alerter consists of a main engine that coordinates multiple
background workers. Each worker handles a specific responsibility:

```
                Alert Engine (Coordinator)
                          |
    +-----------+---------+---------+-----------+
    |           |         |         |           |
    v           v         v         v           v
Threshold   Baseline   Anomaly  Blackout     Alert
Evaluator  Calculator Detector Scheduler   Cleaner
    |           |         |         |           |
    +-----------+---------+---------+-----------+
                          |
                      Datastore
                    (PostgreSQL)
```

## Engine Components

### Alert Engine

The alert engine serves as the central coordinator. The engine
initializes all workers, manages configuration reloading, and
handles graceful shutdown. The engine creates a cancellable
context that workers use to detect shutdown requests.

### Threshold Evaluator

The threshold evaluator runs at a configurable interval,
defaulting to 60 seconds. During each evaluation cycle, the
evaluator performs these steps:

1. Retrieve all enabled alert rules from the datastore.
2. For each rule, fetch the latest metric values.
3. Check for active blackouts that would suppress alerts.
4. Retrieve effective thresholds including per-connection
   overrides.
5. Compare metric values against thresholds.
6. Create or update alerts for threshold violations.

### Baseline Calculator

The baseline calculator refreshes metric baselines at a
configurable interval, defaulting to one hour. The calculator
generates three types of baselines:

- Global baselines aggregate all historical data.
- Hourly baselines capture patterns for each hour of the day.
- Daily baselines capture patterns for each day of the week.

The calculator uses a configurable lookback period, defaulting
to 7 days, to gather historical data for baseline calculations.

### Anomaly Detector

The anomaly detector implements a tiered detection system:

- Tier 1 uses z-score calculations to identify statistical
  anomalies.
- Tier 2 uses vector embeddings to find similar past anomalies.
- Tier 3 uses LLM classification to determine alert or suppress
  decisions.

The detector creates anomaly candidates that progress through
each tier. The final decision determines whether to create an
alert or suppress the anomaly as a false positive.

### Blackout Scheduler

The blackout scheduler runs every minute to check for scheduled
blackouts. The scheduler evaluates cron expressions against the
current time in the configured timezone. When a schedule matches,
the scheduler creates a manual blackout entry with the configured
duration.

### Alert Cleaner

The alert cleaner runs every 30 seconds to check for resolved
conditions. The cleaner retrieves active threshold alerts and
re-evaluates the triggering conditions. When a condition no longer
violates the threshold, the cleaner marks the alert as cleared.

### Retention Manager

The retention manager runs daily to clean up old data. The manager
deletes cleared and acknowledged alerts older than the configured
retention period. The manager also removes processed anomaly
candidates past retention.

### Notification Workers

The notification system includes two workers:

- The notification worker processes pending and retry
  notifications.
- The reminder worker sends periodic reminders for active alerts.

## Data Flow

### Metric Evaluation Flow

```
Collector --writes--> Datastore <--reads-- Alerter --writes--> Alerts
```

The collector writes metrics to the datastore. The alerter reads
metrics and evaluates the metrics against rules. When thresholds
are violated, the alerter writes alerts to the alerts table.

### Anomaly Detection Flow

```
Tier 1 --> Candidate --> Tier 2 --> Tier 3 --> Alert or Suppress
(z-score)   (store)    (embedding)  (LLM)
```

Tier 1 creates anomaly candidates for values exceeding the
z-score threshold. These candidates are stored and processed by
Tier 2, which generates embeddings and searches for similar past
anomalies. Tier 3 uses LLM classification to make the final
decision.

## Database Schema

The alerter uses several tables in the datastore.

### Alert Tables

- `alerts` stores all triggered alerts with their current status.
- `alert_rules` defines threshold-based alert rules.
- `alert_thresholds` stores per-connection threshold overrides.
- `alert_acknowledgments` records user acknowledgments.

### Anomaly Tables

- `anomaly_candidates` stores candidates progressing through
  tiers.
- `anomaly_embeddings` stores vector embeddings for similarity
  search.
- `metric_baselines` stores calculated baseline statistics.

### Blackout Tables

- `blackouts` stores active and historical manual blackouts.
- `blackout_schedules` stores recurring blackout schedules.

### Notification Tables

- `notification_channels` defines notification destinations.
- `notification_channel_connections` links channels to
  connections.
- `notification_history` tracks notification delivery status.
- `notification_reminder_state` tracks reminder progress.

## LLM Integration

The alerter integrates with LLM providers for Tier 2 and Tier 3
processing.

### Embedding Providers

Embedding providers generate vector representations of anomaly
context. The alerter supports the following providers:

- Ollama with models like `nomic-embed-text`.
- OpenAI with `text-embedding-3-small`.
- Voyage with `voyage-3-lite`.

### Reasoning Providers

Reasoning providers classify anomalies as real issues or false
positives. The alerter supports the following providers:

- Ollama with models like `qwen2.5:7b-instruct`.
- OpenAI with `gpt-4o-mini`.
- Anthropic with `claude-3-5-haiku`.

## Configuration Reloading

The alerter supports configuration reloading without restart.
Sending a `SIGHUP` signal triggers the engine to reload the
configuration file and apply reloadable settings to all workers.

## Graceful Shutdown

The alerter handles `SIGINT` and `SIGTERM` signals for graceful
shutdown. When the alerter receives a shutdown signal, the engine
cancels the shared context and waits for all workers to complete
their current operations before exiting.

## Integration Points

The alerter integrates with other AI DBA Workbench components:

- The collector provides the metric data that the alerter
  evaluates.
- The server exposes APIs for managing alert rules and viewing
  alerts.
- The client displays alerts and provides acknowledgment
  interfaces.
- LLM providers power the Tier 3 anomaly classification.
