# pgEdge AI Workbench Collector

The pgEdge AI Workbench Collector is a monitoring service that collects metrics from PostgreSQL servers and stores them in a centralized datastore for analysis by the AI Workbench system.

## Overview

The collector is a standalone Go application that:

- Connects to multiple PostgreSQL servers for monitoring
- Executes configurable probes to collect metrics
- Stores collected metrics in a PostgreSQL datastore
- Manages data retention through automated garbage collection

## Getting Started

### Prerequisites

- Go 1.21 or later
- PostgreSQL 12 or later (for the datastore)
- Network access to monitored PostgreSQL servers

### Building

```bash
cd src
go mod tidy
go build -o collector
```

### Configuration

The collector can be configured using a configuration file, command line flags, or a combination of both. Command line flags take precedence over configuration file settings.

The configuration file is shared with the MCP server component and uses a common format.

#### Configuration File

By default, the collector looks for `ai-workbench.conf` in the same directory as the executable. You can specify a different path using the `-config` flag.

A sample configuration file is provided at [../configs/ai-workbench.conf.sample](../configs/ai-workbench.conf.sample). Copy this file to `ai-workbench.conf` and customize it for your environment.

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

Or, if you place the configuration file in the same directory as the collector binary:

```bash
./collector
```

## Documentation

For detailed documentation, see [docs/index.md](docs/index.md).

## Testing

To run the test suite:

```bash
go test ./...
```

## License

This software is released under The PostgreSQL License. See [LICENSE.md](../LICENSE.md) for details.
