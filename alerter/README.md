# pgEdge AI DBA Workbench Alerter

The pgEdge AI DBA Workbench Alerter is a background monitoring service that
evaluates collected metrics against thresholds and uses AI-powered anomaly
detection to generate alerts.

## Table of Contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Building](#building)
- [Configuration](#configuration)
- [Running](#running)
- [Anomaly Detection Tiers](#anomaly-detection-tiers)
- [Documentation](#documentation)

For complete documentation, visit [docs.pgedge.com](https://docs.pgedge.com).

## Features

The alerter provides the following capabilities:

- The threshold engine evaluates metrics against configurable alert rules.
- The tiered anomaly detection system uses statistical analysis, embedding
  similarity, and LLM classification.
- The alert lifecycle manager tracks states and handles automatic clearing.
- The blackout scheduler suppresses alerts during maintenance windows.
- The baseline calculator computes metric baselines for anomaly detection.

## Prerequisites

Before installing, ensure you have the following:

- [Go 1.24](https://go.dev/doc/install) or later.
- [PostgreSQL 12](https://www.postgresql.org/download/) or later with pgvector
  extension for Tier 2 anomaly detection.
- Network access to the AI DBA Workbench datastore.
- (Optional) [Ollama](https://ollama.ai/), OpenAI, Anthropic, or Voyage API
  access for Tier 3 LLM classification.

## Building

```bash
cd src
go mod tidy
go build -o ai-dba-alerter ./cmd/ai-dba-alerter
```

## Configuration

The alerter supports configuration through YAML files, environment variables,
and command-line flags. Command-line flags take precedence over environment
variables, which take precedence over configuration file settings.

### Configuration File

By default, the alerter searches for configuration in these locations:

1. `/etc/pgedge/ai-dba-alerter.yaml`
2. `<binary-directory>/ai-dba-alerter.yaml`

You can specify a different path using the `-config` flag.

A sample configuration file is provided at
[../examples/ai-dba-alerter.yaml](../examples/ai-dba-alerter.yaml).

In the following example, the configuration sets up a basic alerter instance:

```yaml
datastore:
  host: localhost
  database: ai_workbench
  username: alerter
  password_file: /etc/ai-workbench/password.txt
  port: 5432
  sslmode: prefer

threshold:
  evaluation_interval_seconds: 60

anomaly:
  enabled: true
  tier1:
    enabled: true
    default_sensitivity: 3.0
```

### Command-Line Flags

The following table lists all command-line flags:

| Flag | Description | Default |
|------|-------------|---------|
| `-config` | Path to configuration file | Auto-detected |
| `-debug` | Enable debug logging | `false` |
| `-db-host` | Database host (overrides config) | None |
| `-db-port` | Database port (overrides config) | None |
| `-db-name` | Database name (overrides config) | None |
| `-db-user` | Database user (overrides config) | None |
| `-db-password` | Database password (overrides config) | None |
| `-db-sslmode` | Database SSL mode (overrides config) | None |

### Environment Variables

The following environment variables override configuration file settings:

| Variable | Description |
|----------|-------------|
| `AI_DBA_PG_HOST` | Database hostname |
| `AI_DBA_PG_HOSTADDR` | Database IP address |
| `AI_DBA_PG_DATABASE` | Database name |
| `AI_DBA_PG_USERNAME` | Database username |
| `AI_DBA_PG_PASSWORD` | Database password |
| `AI_DBA_PG_SSLMODE` | SSL connection mode |
| `AI_DBA_PG_SSLCERT` | Path to SSL certificate |
| `AI_DBA_PG_SSLKEY` | Path to SSL key |
| `AI_DBA_PG_SSLROOTCERT` | Path to CA certificate |

For complete configuration documentation, see
[docs/alerter/configuration.md](../docs/alerter/configuration.md).

## Running

In the following example, the alerter starts with a custom configuration file:

```bash
./ai-dba-alerter -config /path/to/config.yaml
```

In the following example, the alerter starts with debug logging enabled:

```bash
./ai-dba-alerter -debug -config /path/to/config.yaml
```

### Signal Handling

The alerter responds to Unix signals for operational control:

- `SIGINT` and `SIGTERM` trigger a graceful shutdown.
- `SIGHUP` reloads the configuration file without restarting.

## Anomaly Detection Tiers

The alerter implements a tiered approach to anomaly detection. Each tier
provides increasingly sophisticated analysis at the cost of additional
processing time.

### Tier 1: Statistical Analysis

Tier 1 uses z-score calculations to detect deviations from baseline metrics.
The tier runs on all metrics that have baseline data available. This approach
provides fast detection with minimal resource usage.

### Tier 2: Embedding Similarity

Tier 2 uses pgvector to find similar patterns in historical anomaly data.
The tier requires the pgvector extension and pre-computed embeddings. This
approach identifies anomalies that match known problematic patterns.

### Tier 3: LLM Classification

Tier 3 uses large language models to classify complex anomalies. The tier
supports Ollama (local), OpenAI, Anthropic, and Voyage providers. This
approach provides the most sophisticated analysis for difficult cases.

## Documentation

For detailed documentation, see [docs/alerter/index.md](../docs/alerter/index.md).

The documentation includes the following topics:

- [Configuration Reference](../docs/alerter/configuration.md) covers all
  configuration options, environment variables, and command-line flags.
- [Cron Expressions](../docs/alerter/cron-expressions.md) explains the cron
  syntax for scheduling blackout periods.

## Testing and Linting

The project uses a Makefile to manage testing, linting, and building.

Run all tests with the following command:

```bash
make test
```

Run the linter with the following command:

```bash
make lint
```

Run both tests and linting with the following command:

```bash
make test-all
```

---

To report an issue with the software, visit:
[GitHub Issues](https://github.com/pgEdge/ai-dba-workbench/issues)

We welcome your project contributions; for more information, see
[docs/developers.md](../docs/developers.md).

For more information, visit [docs.pgedge.com](https://docs.pgedge.com)

This project is licensed under the [PostgreSQL License](../LICENSE.md).
