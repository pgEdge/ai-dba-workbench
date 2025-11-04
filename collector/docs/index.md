# pgEdge AI Workbench Collector Documentation

## Table of Contents

- [Introduction](#introduction)
- [Architecture](#architecture)
- [Configuration](#configuration)
- [Database Schema](#database-schema)
- [Monitoring Probes](#monitoring-probes)
- [Development](#development)

## Introduction

The pgEdge AI Workbench Collector is a critical component of the AI Workbench system, responsible for collecting and storing metrics from monitored PostgreSQL servers. It operates as a standalone service that continuously monitors configured PostgreSQL instances and stores their metrics in a centralized datastore.

### Key Features

- **Multi-Server Monitoring**: Monitor multiple PostgreSQL servers simultaneously
- **Configurable Probes**: Define custom SQL queries to collect specific metrics
- **Automated Data Management**: Built-in garbage collection for metric retention
- **Secure Connections**: Support for SSL/TLS connections to both datastore and monitored servers
- **Flexible Configuration**: Support for file-based and command-line configuration

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

Each enabled probe runs in its own goroutine, executing at configured intervals. Probes are executed against all monitored connections that have monitoring enabled.

#### Garbage Collector Thread

Runs daily to drop old metric partitions based on each probe's retention policy.

### Data Flow

1. Collector loads configuration and connects to the datastore
2. Monitored connections are loaded from the datastore
3. Probes are loaded from the datastore
4. Each probe runs on a timer, executing against all monitored connections
5. Metrics are stored in probe-specific tables in the datastore
6. The garbage collector periodically removes old data

## Configuration

The collector uses a shared configuration file format that is also used by the MCP server component. By default, the collector looks for `ai-workbench.conf` in the same directory as the executable, but you can specify a different path using the `-config` command line flag.

A sample configuration file is provided at [../../configs/ai-workbench.conf.sample](../../configs/ai-workbench.conf.sample) in the project root. Copy this file to your desired location and customize it for your environment.

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
- `pg_sslmode`: SSL mode (disable, allow, prefer, require, verify-ca, verify-full)
- `pg_sslcert`: Path to client SSL certificate
- `pg_sslkey`: Path to client SSL key
- `pg_sslrootcert`: Path to root SSL certificate

#### Security Options

- `server_secret`: Per-installation secret for encryption (config file only)

### Command Line Flags

All datastore configuration options can be specified as command-line flags using the `--` prefix and `-` instead of `_`. For example:

```bash
./collector --pg-host localhost --pg-database ai_workbench
```

Command line flags take precedence over configuration file values.

## Database Schema

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

Each probe stores its data in a dedicated table, partitioned by week for efficient data management and garbage collection.

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

Custom probes can be added by inserting records into the `probes` table. The SQL query should return consistent columns across executions.

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

```bash
go test ./...
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
