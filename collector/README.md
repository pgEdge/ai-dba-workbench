# pgEdge AI DBA Workbench Collector

[![Build Collector](https://github.com/pgEdge/ai-workbench/actions/workflows/build-collector.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/build-collector.yml)
[![Test Collector](https://github.com/pgEdge/ai-workbench/actions/workflows/test-collector.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/test-collector.yml)
[![Lint Collector](https://github.com/pgEdge/ai-workbench/actions/workflows/lint-collector.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/lint-collector.yml)

The pgEdge AI DBA Workbench Collector is a monitoring service that collects
metrics from PostgreSQL servers and stores them in a centralized datastore
for analysis by the AI DBA Workbench system.

For complete documentation, visit [docs.pgedge.com](https://docs.pgedge.com).

## Table of Contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Testing](#testing)
- [Documentation](#documentation)

## Features

The Collector provides the following capabilities:

- The service connects to multiple PostgreSQL servers for monitoring.
- Configurable probes collect metrics from PostgreSQL system views.
- Collected metrics are stored in a PostgreSQL datastore.
- Automated garbage collection manages data retention.
- AES-256-GCM encryption protects stored passwords.

## Prerequisites

Before installing the Collector, ensure you have the following:

- [Go 1.23](https://go.dev/doc/install) or later installed.
- [PostgreSQL 12](https://www.postgresql.org/download/) or later for the
  datastore.
- Network access to the PostgreSQL servers you want to monitor.

## Installation

Clone the repository and build the Collector:

```bash
git clone https://github.com/pgEdge/ai-dba-workbench.git
cd ai-dba-workbench/collector/src
go mod tidy
go build -o ai-dba-collector
```

The build process creates the `ai-dba-collector` binary in the `src`
directory.

## Configuration

The Collector uses a YAML configuration file. You can also specify settings
using command-line flags, which take precedence over the configuration file.

### Configuration File

By default, the Collector searches for its configuration file in these
locations (in order):

1. `/etc/pgedge/ai-dba-collector.yaml`
2. `ai-dba-collector.yaml` in the same directory as the binary

Copy the example configuration file to get started:

```bash
cp ../examples/ai-dba-collector.yaml ./ai-dba-collector.yaml
```

### Example Configuration

The following example shows a basic configuration:

```yaml
datastore:
  host: localhost
  database: ai_workbench
  username: collector
  password_file: /path/to/password.txt
  port: 5432
  sslmode: prefer

pool:
  datastore_max_connections: 25
  monitored_max_connections: 5

# secret_file uses default search paths
```

### Server Secret

The Collector requires a secret file for encrypting monitored connection
passwords. Generate a secure secret file using the following command:

```bash
openssl rand -base64 32 > ./ai-dba-collector.secret
chmod 600 ./ai-dba-collector.secret
```

For detailed configuration options, see
[Configuration Guide](../docs/collector/configuration.md).

### Command-Line Flags

The following flags are available:

| Flag | Description | Default |
|------|-------------|---------|
| `-config` | Path to configuration file | See search paths |
| `-v` | Enable verbose logging | `false` |
| `-pg-host` | PostgreSQL server hostname | `localhost` |
| `-pg-hostaddr` | PostgreSQL server IP address | none |
| `-pg-database` | PostgreSQL database name | `ai_workbench` |
| `-pg-username` | PostgreSQL username | `postgres` |
| `-pg-password-file` | Path to password file | none |
| `-pg-port` | PostgreSQL server port | `5432` |
| `-pg-sslmode` | SSL mode | `prefer` |
| `-pg-sslcert` | Path to client SSL certificate | none |
| `-pg-sslkey` | Path to client SSL key | none |
| `-pg-sslrootcert` | Path to root SSL certificate | none |

## Usage

Start the Collector with a configuration file:

```bash
./ai-dba-collector -config /path/to/ai-dba-collector.yaml
```

Enable verbose logging for troubleshooting:

```bash
./ai-dba-collector -v -config /path/to/ai-dba-collector.yaml
```

Verbose mode displays detailed operational information including probe
initialization, connection management, and data collection activities.

For detailed usage instructions, see
[Quick Start Guide](../docs/collector/quickstart.md).

## Testing

The project uses a Makefile to manage testing, linting, and building.

Run tests and linting together:

```bash
make check
```

Run tests only:

```bash
make test
```

Run linting only:

```bash
make lint
```

The test suite automatically creates a temporary test database, runs all
tests, and drops the database when complete. Use the following environment
variables to customize test behavior:

- `TEST_AI_WORKBENCH_SERVER` - Specify a custom PostgreSQL server.
- `TEST_AI_WORKBENCH_KEEP_DB=1` - Keep the test database for inspection.
- `SKIP_DB_TESTS=1` - Skip all database tests.

Additional make targets:

```bash
make build      # Build the collector binary
make fmt        # Format code with gofmt
make vet        # Run go vet
make coverage   # Generate test coverage report
make clean      # Remove build artifacts
make help       # Show all available targets
```

## Documentation

For complete documentation, see [docs/collector/](../docs/collector/).

The documentation covers the following topics:

- [Configuration Guide](../docs/collector/configuration.md) - Configuration
  options and examples.
- [Architecture](../docs/collector/architecture.md) - System design and
  components.
- [Probes](../docs/collector/probes.md) - Available probes and customization.
- [Config Reference](../docs/collector/config-reference.md) - Complete
  configuration reference.

---

To report an issue with the software, visit:
[GitHub Issues](https://github.com/pgEdge/ai-dba-workbench/issues)

We welcome your project contributions; for more information, see
[docs/developers.md](../docs/developers.md).

For more information, visit [docs.pgedge.com](https://docs.pgedge.com)

This project is licensed under the [PostgreSQL License](../LICENSE.md).
