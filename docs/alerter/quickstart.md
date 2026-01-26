# Quick Start Guide

This guide helps you get the alerter running quickly. The alerter requires
a working datastore with collected metrics before it can generate alerts.

## Prerequisites

Before starting the alerter, ensure you have:

- A running PostgreSQL datastore with the AI DBA Workbench schema.
- The collector running and collecting metrics from monitored instances.
- Network access from the alerter to the datastore.
- Optionally, Ollama or API keys for LLM-based anomaly detection.

## Installation

Build the alerter binary from source:

```bash
cd alerter
go build -o bin/ai-dba-alerter ./src
```

Alternatively, download a pre-built binary from the releases page.

## Basic Configuration

The alerter can run with minimal configuration by using environment
variables for the datastore connection:

```bash
export AI_DBA_PG_HOST=localhost
export AI_DBA_PG_DATABASE=ai_workbench
export AI_DBA_PG_USERNAME=postgres
export AI_DBA_PG_PASSWORD=your_password
```

## Starting the Alerter

Start the alerter with debug logging to verify operation:

```bash
./ai-dba-alerter -debug
```

The alerter outputs status messages to `stderr` during startup:

```
Configuration loaded from /etc/pgedge/ai-dba-alerter.yaml
Datastore: connected to postgres@localhost:5432/ai_workbench
Starting alerter engine...
Threshold evaluator started (interval: 1m0s)
Baseline calculator started (interval: 1h0m0s)
Anomaly detector started (interval: 1m0s)
Blackout scheduler started
Alert cleaner started
Retention manager started
All workers started
```

## Verifying Operation

After the alerter starts, verify that it is evaluating rules by checking
the debug output. The alerter logs rule evaluation progress:

```
Evaluating threshold rules...
Found 24 enabled rules
Calculating baselines...
Baseline calculation complete
```

## Creating a Configuration File

For production use, create a YAML configuration file. In the following
example, the configuration file specifies datastore settings and enables
Ollama for anomaly detection:

```yaml
datastore:
  host: db.example.com
  database: ai_workbench
  username: alerter
  password_file: /etc/ai-workbench/db-password.txt
  sslmode: verify-full

threshold:
  evaluation_interval_seconds: 60

anomaly:
  enabled: true
  tier1:
    enabled: true
    default_sensitivity: 3.0
  tier2:
    enabled: true
  tier3:
    enabled: true

llm:
  embedding_provider: ollama
  reasoning_provider: ollama
  ollama:
    base_url: http://localhost:11434
    embedding_model: nomic-embed-text
    reasoning_model: qwen2.5:7b-instruct
```

Save this file as `/etc/pgedge/ai-dba-alerter.yaml` or specify a custom
path with the `-config` flag.

## Running as a Service

For production deployments, run the alerter as a systemd service. Create
a service file at `/etc/systemd/system/ai-dba-alerter.service`:

```ini
[Unit]
Description=pgEdge AI DBA Workbench Alerter
After=network.target postgresql.service

[Service]
Type=simple
User=ai-workbench
ExecStart=/opt/ai-workbench/ai-dba-alerter
Restart=always
RestartSec=10
EnvironmentFile=/etc/ai-workbench/alerter.env

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable ai-dba-alerter
sudo systemctl start ai-dba-alerter
```

## Viewing Alerts

The alerter writes alerts to the `alerts` table in the datastore. You can
query this table directly or use the web client to view active alerts.

In the following example, the query retrieves recent active alerts:

```sql
SELECT id, title, severity, status, triggered_at
FROM alerts
WHERE status = 'active'
ORDER BY triggered_at DESC
LIMIT 10;
```

## Next Steps

- Review the [Configuration Reference](configuration.md) for all options.
- Explore the [Alert Rules](alert-rules.md) to customize thresholds.
- Set up [Anomaly Detection](anomaly-detection.md) for AI-powered alerting.
- Configure notifications to receive alerts through Slack or email.
