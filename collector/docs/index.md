# pgEdge AI Workbench Collector Documentation

## Table of Contents

- [Introduction](#introduction)
- [Architecture](#architecture)
- [Configuration](#configuration)
- [Database Schema](#database-schema)
- [Monitoring Probes](#monitoring-probes)
- [Development](#development)
- [Schema Management](schema-management.md)

## Introduction

The pgEdge AI Workbench Collector is a critical component of the AI
Workbench system, responsible for collecting and storing metrics from
monitored PostgreSQL servers. It operates as a standalone service that
continuously monitors configured PostgreSQL instances and stores their
metrics in a centralized datastore.

### Key Features

- **Multi-Server Monitoring**: Monitor multiple PostgreSQL servers
  simultaneously
- **Configurable Probes**: Define custom SQL queries to collect specific
  metrics
- **Automated Data Management**: Built-in garbage collection for metric
  retention
- **Secure Connections**: Support for SSL/TLS connections to both datastore
  and monitored servers
- **Flexible Configuration**: Support for file-based and command-line
  configuration

## Architecture

The collector is built in Go and consists of several key components:

### Components

#### Main Thread

The main thread is responsible for:

- Loading configuration
- Initializing the datastore connection
- Starting the monitoring system
- Managing shutdown

#### Monitoring Threads

Each enabled probe runs in its own goroutine, executing at configured
intervals. Probes are executed against all monitored connections that have
monitoring enabled.

#### Garbage Collector Thread

Runs daily to drop old metric partitions based on each probe's retention
policy.

### Data Flow

1. Collector loads configuration and connects to the datastore
2. Monitored connections are loaded from the datastore
3. Probes are loaded from the datastore
4. Each probe runs on a timer, executing against all monitored connections
5. Metrics are stored in probe-specific tables in the datastore
6. The garbage collector periodically removes old data

## Configuration

The collector uses a shared configuration file format that is also used by
the MCP server component. By default, the collector looks for
`ai-workbench.conf` in the same directory as the executable, but you can
specify a different path using the `-config` command line flag.

A sample configuration file is provided in the project root:
[ai-workbench.conf.sample][sample-config]

Copy this file to your desired location and customize it for your
environment.

[sample-config]: ../../configs/ai-workbench.conf.sample

### Configuration File Format

The configuration file uses a simple key-value format with `#` for comments:

```
# Comment
key = value
key = "quoted value"
```

### Available Configuration Options

#### Datastore Options

- `pg_host`: PostgreSQL server hostname or IP address
- `pg_hostaddr`: PostgreSQL server IP address (bypasses DNS)
- `pg_database`: Database name for the datastore
- `pg_username`: Username for datastore connection
- `pg_password_file`: Path to file containing the password
- `pg_port`: PostgreSQL server port (default: 5432)
- `pg_sslmode`: SSL mode (disable, allow, prefer, require, verify-ca,
  verify-full)
- `pg_sslcert`: Path to client SSL certificate
- `pg_sslkey`: Path to client SSL key
- `pg_sslrootcert`: Path to root SSL certificate

#### Security Options

- `server_secret`: Per-installation secret for encryption (config file only)

### Command Line Flags

All datastore configuration options can be specified as command-line flags
using the `--` prefix and `-` instead of `_`. For example:

```bash
./collector --pg-host localhost --pg-database ai_workbench
```

Command line flags take precedence over configuration file values.

## Database Schema

The collector uses a migration-based schema management system to automatically
create and update database schemas at startup. For detailed information about
the schema management system, including how to add new migrations, see the
[Schema Management](schema-management.md) documentation.

### Core Tables

#### `schema_version`

Tracks the current schema version for migration purposes.

#### `monitored_connections`

Stores connection information for PostgreSQL servers to monitor:

- Connection parameters (host, port, database, credentials)
- SSL/TLS configuration
- Ownership (shared vs. private connections)
- Monitoring status

#### `probes`

Defines monitoring probes:

- Name and description
- SQL query to execute
- Collection interval (seconds)
- Data retention period (days)
- Enabled status

### Metric Tables

Each probe stores its data in a dedicated table, partitioned by week for
efficient data management and garbage collection.

## Monitoring Probes

### Probe Configuration

Probes are configured in the `probes` table with the following attributes:

- **Name**: Unique identifier for the probe
- **SQL Query**: The query to execute against monitored servers
- **Collection Interval**: How often to run the probe (in seconds)
- **Retention Days**: How long to keep collected data

### Probe Execution

Probes execute as follows:

1. Timer triggers based on collection interval
2. Probe query is executed against each monitored connection
3. Results are stored in the probe's metric table
4. Any errors are logged but don't stop other probes

### Creating Custom Probes

Custom probes can be added by inserting records into the `probes` table.
The SQL query should return consistent columns across executions.

## Development

### Project Structure

```
collector/
├── src/              # Source code
│   ├── main.go      # Application entry point
│   ├── config.go    # Configuration management
│   ├── datastore.go # Datastore connection and schema
│   └── monitor.go   # Monitoring and probe execution
├── tests/           # Unit and integration tests
├── docs/            # Documentation
└── README.md        # Quick start guide
```

### Building from Source

```bash
cd src
go mod tidy
go build -o collector
```

### Running Tests

The test suite automatically creates a temporary test database, runs all
tests against it, and then drops the database when complete.

```bash
go test ./...
```

#### Test Environment Variables

The following environment variables can be used to configure test behavior:

- **`TEST_DB_URL`**: PostgreSQL URL for the test database server (e.g.,
  `postgres://user:pass@localhost:5432/postgres`). The tests will connect to
  this server, create a temporary database, run tests, and drop it.

- **`TEST_DB_CONN`**: Alternative connection string format (e.g.,
  `host=localhost port=5432 user=postgres sslmode=disable`). Use this for
  backward compatibility or if you prefer the key=value format.

- **`TEST_DB_KEEP`**: Set to `1` or `true` to prevent automatic cleanup of
  the test database. Useful for inspecting the database state after tests
  run. The test database name includes a timestamp for easy identification.

- **`SKIP_DB_TESTS`**: Set to any value to skip all database tests. Useful
  when PostgreSQL is not available.

#### Examples

Run tests against a remote PostgreSQL server:

```bash
TEST_DB_URL="postgres://testuser:testpass@testserver:5432/postgres" go test ./...
```

Keep the test database for inspection:

```bash
TEST_DB_KEEP=1 go test ./...
# Test database will be named like: ai_workbench_test_20251104_133248_374204
# Clean up manually when done: DROP DATABASE ai_workbench_test_20251104_133248_374204
```

Skip database tests when PostgreSQL is unavailable:

```bash
SKIP_DB_TESTS=1 go test ./...
```

### Code Style

- Use 4 spaces for indentation
- Follow standard Go formatting (use `go fmt`)
- Include the copyright header in all source files
- Write tests for all new functionality

### Adding New Features

1. Update the schema if needed (add migration logic)
2. Implement the feature
3. Add comprehensive tests
4. Update documentation
5. Test with a real PostgreSQL server
