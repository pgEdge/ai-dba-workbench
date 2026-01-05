# pgEdge AI DBA Workbench Collector

[![Build Collector](https://github.com/pgEdge/ai-workbench/actions/workflows/build-collector.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/build-collector.yml)
[![Test Collector](https://github.com/pgEdge/ai-workbench/actions/workflows/test-collector.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/test-collector.yml)
[![Lint Collector](https://github.com/pgEdge/ai-workbench/actions/workflows/lint-collector.yml/badge.svg)](https://github.com/pgEdge/ai-workbench/actions/workflows/lint-collector.yml)

The pgEdge AI DBA Workbench Collector is a monitoring service that collects
metrics from PostgreSQL servers and stores them in a centralized datastore
for analysis by the AI Workbench system.

## Overview

The collector is a standalone Go application that:

- Connects to multiple PostgreSQL servers for monitoring
- Executes configurable probes to collect metrics
- Stores collected metrics in a PostgreSQL datastore
- Manages data retention through automated garbage collection

## Getting Started

### Prerequisites

- Go 1.23 or later
- PostgreSQL 12 or later (for the datastore)
- Network access to monitored PostgreSQL servers

### Building

```bash
cd src
go mod tidy
go build -o collector
```

### Configuration

The collector can be configured using a configuration file, command line
flags, or a combination of both. Command line flags take precedence over
configuration file settings.

The configuration file is shared with the MCP server component and uses a
common format.

#### Configuration File

By default, the collector looks for `ai-workbench.conf` in the same
directory as the executable. You can specify a different path using the
`-config` flag.

A sample configuration file is provided at
[../configs/ai-workbench.conf.sample](../configs/ai-workbench.conf.sample).
Copy this file to `ai-workbench.conf` and customize it for your
environment.

Key configuration options:

```
# Datastore connection settings
pg_host = localhost
pg_database = ai_workbench
pg_username = collector
pg_password_file = /path/to/password.txt
pg_port = 5432
pg_sslmode = prefer

# Server secret for encryption (REQUIRED)
server_secret = your-secret-here
```

See the sample configuration file for all available options.

#### Command Line Flags

```
-config string
    Path to configuration file
-v
    Enable verbose logging (shows detailed operational information)
-pg-host string
    PostgreSQL server hostname or IP address
-pg-database string
    PostgreSQL database name
-pg-username string
    PostgreSQL username
-pg-password-file string
    Path to file containing PostgreSQL password
-pg-port int
    PostgreSQL server port (default 5432)
-pg-sslmode string
    PostgreSQL SSL mode (default "prefer")
```

### Running

```bash
./collector -config /path/to/ai-workbench.conf
```

Or, if you place the configuration file in the same directory as the
collector binary:

```bash
./collector
```

To enable verbose logging for troubleshooting or development:

```bash
./collector -v -config /path/to/ai-workbench.conf
```

Verbose mode displays detailed operational information including probe
initialization, connection management, and data collection activities. Without
the `-v` flag, only startup messages, shutdown messages, and errors are
displayed.

## Documentation

For detailed documentation, see [docs/index.md](docs/index.md).

## Testing and Linting

The project uses a Makefile to manage testing, linting, and building.

### Run tests and linting together (recommended)

```bash
make check
```

This will run formatting, go vet, tests, and linting in sequence.

### Run tests only

```bash
make test
```

Or use Go directly:

```bash
cd src
go test -v ./...
```

Tests automatically create a temporary test database with a timestamp in the
name, run all tests against it, and then drop the database when complete. Use
environment variables to customize test behavior:

- `TEST_AI_WORKBENCH_SERVER`: Specify a custom PostgreSQL server (e.g.,
  `postgres://user:pass@host:5432/postgres`)
- `TEST_AI_WORKBENCH_KEEP_DB=1`: Keep the test database after tests complete
  for inspection
- `SKIP_DB_TESTS=1`: Skip all database tests

See [docs/index.md](docs/index.md) for more details on testing.

### Run linting only

```bash
make lint
```

This runs `golangci-lint` which performs comprehensive static analysis
including error checking, security analysis, and code quality checks.

### Other useful commands

```bash
make build      # Build the collector binary
make fmt        # Format code with gofmt
make vet        # Run go vet
make coverage   # Generate test coverage report
make clean      # Remove build artifacts
make help       # Show all available targets
```

## License

This software is released under The PostgreSQL License. See
[LICENSE.md](../LICENSE.md) for details.
