# pgEdge AI DBA Workbench Alerter

The pgEdge AI DBA Workbench Alerter is a background monitoring service that
evaluates collected metrics against thresholds and uses AI-powered anomaly
detection to generate alerts.

## Overview

The alerter is a standalone Go application that:

- Evaluates metrics against configurable threshold-based alert rules
- Implements tiered AI-powered anomaly detection
- Manages alert lifecycle including automatic clearing
- Supports blackout periods for maintenance windows
- Calculates metric baselines for anomaly detection

## Getting Started

### Prerequisites

- Go 1.24 or later
- PostgreSQL 12 or later (for the datastore with pgvector for Tier 2)
- Network access to the AI DBA Workbench datastore
- (Optional) Ollama, OpenAI, Anthropic, or Voyage API access for Tier 3

### Building

```bash
cd src
go mod tidy
go build -o ai-dba-alerter ./cmd/ai-dba-alerter
```

### Configuration

The alerter can be configured using a YAML configuration file, command line
flags, or environment variables. Command line flags take precedence over
configuration file settings.

#### Configuration File

By default, the alerter looks for configuration in:

1. `/etc/pgedge/ai-dba-alerter.yaml`
2. `<binary-directory>/ai-dba-alerter.yaml`

You can specify a different path using the `-config` flag.

A sample configuration file is provided at
[../examples/ai-dba-alerter.yaml](../examples/ai-dba-alerter.yaml).

Key configuration options:

```yaml
datastore:
  host: localhost
  database: ai_workbench
  username: alerter
  password_file: /path/to/password.txt
  port: 5432
  sslmode: prefer

threshold:
  check_interval_seconds: 60
  default_severity: warning

anomaly:
  enabled: true
  tier1:
    enabled: true
    z_score_threshold: 3.0
```

See the example configuration file for all available options.

#### Command Line Flags

```
-config string
    Path to configuration file
-debug
    Enable debug logging
-db-host string
    Database host (overrides config)
-db-port int
    Database port (overrides config)
-db-name string
    Database name (overrides config)
-db-user string
    Database user (overrides config)
-db-password string
    Database password (overrides config)
-db-sslmode string
    Database SSL mode (overrides config)
```

#### Environment Variables

The following environment variables can override configuration:

- `AI_DBA_PG_HOST` - Database host
- `AI_DBA_PG_HOSTADDR` - Database host address
- `AI_DBA_PG_DATABASE` - Database name
- `AI_DBA_PG_USERNAME` - Database username
- `AI_DBA_PG_PASSWORD` - Database password
- `AI_DBA_PG_SSLMODE` - SSL mode
- `AI_DBA_PG_SSLCERT` - SSL certificate path
- `AI_DBA_PG_SSLKEY` - SSL key path
- `AI_DBA_PG_SSLROOTCERT` - SSL root certificate path

### Running

```bash
./ai-dba-alerter -config /path/to/config.yaml
```

To enable debug logging for troubleshooting:

```bash
./ai-dba-alerter -debug -config /path/to/config.yaml
```

### Signal Handling

The alerter responds to the following signals:

- `SIGINT`, `SIGTERM` - Graceful shutdown
- `SIGHUP` - Reload configuration

## Anomaly Detection Tiers

The alerter implements a tiered approach to anomaly detection:

### Tier 1: Statistical Analysis

Uses z-score calculations to detect deviations from baseline metrics. This
tier is fast and runs on all metrics with baseline data available.

### Tier 2: Embedding Similarity

Uses pgvector to find similar patterns in historical anomaly data. Requires
the pgvector extension and pre-computed embeddings.

### Tier 3: LLM Classification

Uses large language models to classify complex anomalies. Supports Ollama
(local), OpenAI, Anthropic, and Voyage providers.

## Documentation

For detailed documentation, see [../docs/alerter/index.md](../docs/alerter/index.md).

## Testing and Linting

The project uses a Makefile to manage testing, linting, and building.

### Run tests

```bash
make test
```

### Run linting

```bash
make lint
```

### Run all checks

```bash
make test-all
```

## License

This software is released under The PostgreSQL License. See
[LICENSE.md](../LICENSE.md) for details.
