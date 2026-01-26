# System Architecture

The alerter is a multi-worker background service that processes metrics
and generates alerts. This document describes the internal architecture,
data flow, and component interactions.

## High-Level Architecture

The alerter consists of a main engine that coordinates multiple background
workers. Each worker handles a specific responsibility:

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

The alert engine serves as the central coordinator. The engine initializes
all workers, manages configuration reloading, and handles graceful shutdown.
The engine creates a cancellable context that workers use to detect shutdown
requests.

### Threshold Evaluator

The threshold evaluator runs at a configurable interval, defaulting to 60
seconds. During each evaluation cycle, the evaluator performs these steps:

1. Retrieves all enabled alert rules from the datastore.
2. For each rule, fetches the latest metric values.
3. Checks for active blackouts that would suppress alerts.
4. Retrieves effective thresholds including per-connection overrides.
5. Compares metric values against thresholds.
6. Creates or updates alerts for threshold violations.

### Baseline Calculator

The baseline calculator refreshes metric baselines at a configurable
interval, defaulting to one hour. The calculator generates three types
of baselines:

- Global baselines aggregate all historical data.
- Hourly baselines capture patterns for each hour of the day.
- Daily baselines capture patterns for each day of the week.

The calculator uses a configurable lookback period, defaulting to 7 days,
to gather historical data for baseline calculations.

### Anomaly Detector

The anomaly detector implements a tiered detection system:

- Tier 1 uses z-score calculations to identify statistical anomalies.
- Tier 2 uses vector embeddings to find similar past anomalies.
- Tier 3 uses LLM classification to determine alert or suppress decisions.

The detector creates anomaly candidates that progress through each tier.
The final decision determines whether to create an alert or suppress the
anomaly as a false positive.

### Blackout Scheduler

The blackout scheduler runs every minute to check for scheduled blackouts.
The scheduler evaluates cron expressions against the current time in the
configured timezone. When a schedule matches, the scheduler creates a
manual blackout entry with the configured duration.

### Alert Cleaner

The alert cleaner runs every 30 seconds to check for resolved conditions.
The cleaner retrieves active threshold alerts and re-evaluates the
triggering conditions. When a condition no longer violates the threshold,
the cleaner marks the alert as cleared.

### Retention Manager

The retention manager runs daily to clean up old data. The manager deletes
cleared and acknowledged alerts older than the configured retention period.
The manager also removes processed anomaly candidates past retention.

### Notification Workers

The notification system includes two workers:

- The notification worker processes pending and retry notifications.
- The reminder worker sends periodic reminders for active alerts.

## Data Flow

### Metric Evaluation Flow

```
Collector --writes--> Datastore <--reads-- Alerter --writes--> Alerts
```

The collector writes metrics to the datastore. The alerter reads metrics
and evaluates them against rules. When thresholds are violated, the alerter
writes alerts to the alerts table.

### Anomaly Detection Flow

```
Tier 1 --> Candidate --> Tier 2 --> Tier 3 --> Alert or Suppress
(z-score)   (store)    (embedding)  (LLM)
```

Tier 1 creates anomaly candidates for values exceeding the z-score
threshold. These candidates are stored and processed by Tier 2, which
generates embeddings and searches for similar past anomalies. Tier 3
uses LLM classification to make the final decision.

## Database Schema

The alerter uses several tables in the datastore:

### Alert Tables

- `alerts` stores all triggered alerts with their current status.
- `alert_rules` defines threshold-based alert rules.
- `alert_thresholds` stores per-connection threshold overrides.
- `alert_acknowledgments` records user acknowledgments.

### Anomaly Tables

- `anomaly_candidates` stores candidates progressing through tiers.
- `anomaly_embeddings` stores vector embeddings for similarity search.
- `metric_baselines` stores calculated baseline statistics.

### Blackout Tables

- `blackouts` stores active and historical manual blackouts.
- `blackout_schedules` stores recurring blackout schedules.

### Notification Tables

- `notification_channels` defines notification destinations.
- `notification_channel_connections` links channels to connections.
- `notification_history` tracks notification delivery status.
- `notification_reminder_state` tracks reminder progress.

## LLM Integration

The alerter integrates with LLM providers for Tier 2 and Tier 3 processing:

### Embedding Providers

Embedding providers generate vector representations of anomaly context.
Supported providers include:

- Ollama with models like `nomic-embed-text`.
- OpenAI with `text-embedding-3-small`.
- Voyage with `voyage-3-lite`.

### Reasoning Providers

Reasoning providers classify anomalies as real issues or false positives.
Supported providers include:

- Ollama with models like `qwen2.5:7b-instruct`.
- OpenAI with `gpt-4o-mini`.
- Anthropic with `claude-3-5-haiku`.

## Configuration Reloading

The alerter supports configuration reloading without restart. Sending a
`SIGHUP` signal triggers the engine to reload the configuration file and
apply reloadable settings to all workers.

## Graceful Shutdown

The alerter handles `SIGINT` and `SIGTERM` signals for graceful shutdown.
When a shutdown signal is received, the engine cancels the shared context
and waits for all workers to complete their current operations before
exiting.
