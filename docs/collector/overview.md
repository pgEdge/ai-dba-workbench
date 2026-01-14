# Collector Overview

The pgEdge AI DBA Workbench Collector is a standalone monitoring service written
in Go that continuously collects PostgreSQL metrics and stores them in a
centralized datastore. It is designed to monitor multiple PostgreSQL servers
simultaneously, collecting comprehensive statistics through a flexible probe
system.

## Purpose

The Collector serves as the data collection engine for the pgEdge AI
Workbench system. It:

- Continuously monitors PostgreSQL servers
- Collects metrics from standard PostgreSQL system views
- Stores time-series metrics data for analysis
- Manages data retention through automated garbage collection
- Provides isolation between different users and their connections

## Key Concepts

### Datastore

The datastore is a PostgreSQL database that serves as the central repository
for:

- Collected metrics from all monitored servers
- Connection information for monitored PostgreSQL servers
- Probe configurations and scheduling information
- User accounts and authentication tokens
- Schema version tracking for migrations

The datastore is separate from the PostgreSQL servers being monitored.

### Monitored Connections

A monitored connection represents a PostgreSQL server that the Collector
should monitor. Each connection includes:

- Connection parameters (host, port, database, credentials)
- SSL/TLS configuration
- Ownership information (for multi-user isolation)
- Monitoring status (enabled/disabled)

Connections are stored in the datastore's `connections` table and can be
managed through the MCP server API.

### Probes

Probes are the data collection units in the Collector. Each probe:

- Targets a specific PostgreSQL system view (e.g., `pg_stat_activity`)
- Has a configurable collection interval
- Has a configurable data retention period
- Can be enabled or disabled individually
- Stores data in its own partitioned table

The Collector includes 24 built-in probes covering the most important
PostgreSQL statistics views.

### Probe Types

Probes fall into two categories:

#### Server-Scoped Probes

These probes collect server-wide statistics and run once per monitored
connection:

- `pg_stat_activity` - Current database activity
- `pg_stat_replication` - Replication status
- `pg_stat_bgwriter` - Background writer statistics
- And 11 more server-wide probes

#### Database-Scoped Probes

These probes collect per-database statistics and run once for each database
on a monitored server:

- `pg_stat_database` - Database-wide statistics
- `pg_stat_all_tables` - Table statistics
- `pg_stat_all_indexes` - Index statistics
- And 6 more database-specific probes

### Scheduling

The probe scheduler manages when probes execute. Each probe:

- Runs on its own independent schedule
- Executes against all applicable connections in parallel
- Has configurable collection intervals (default: 5 minutes)
- Starts immediately when the Collector starts

### Partitioning

Metrics tables use PostgreSQL's declarative partitioning:

- Tables are partitioned by week (Monday to Sunday)
- Partitions are created automatically as needed
- Old partitions are dropped during garbage collection
- This provides efficient storage and query performance

### Garbage Collection

The garbage collector runs daily to:

- Drop expired metric partitions based on retention settings
- Free up disk space
- Keep the datastore size manageable

The first collection runs 5 minutes after startup, then every 24 hours.

### Connection Pooling

The Collector uses two types of connection pools:

#### Datastore Connection Pool

- Manages connections to the central datastore
- Configured with `datastore_pool_max_connections` (default: 25)
- Used for storing metrics and reading configurations

#### Monitored Connection Pools

- One pool per monitored PostgreSQL server
- Each pool limited to `monitored_pool_max_connections` (default: 5)
- Prevents overwhelming monitored servers
- Connections reused across probe executions

### Password Encryption

All passwords for monitored connections are encrypted using industry-standard
algorithms.

- The system uses AES-256-GCM encryption for password protection.
- Encryption keys are derived using PBKDF2 with SHA256 and 100,000 iterations.
- The key derivation combines the server secret with the username as salt.
- Encrypted passwords are stored in the datastore's `connections` table.
- The Collector decrypts passwords only when establishing connections.

## Component Architecture

The Collector consists of several key components:

### Main Package

- Application entry point (`main.go`)
- Configuration management (`config.go`)
- Constants definition (`constants.go`)
- Garbage collector (`garbage_collector.go`)

### Database Package

- Datastore connection management (`datastore.go`, `datastore_pool.go`)
- Monitored connection pools (`monitored_pool.go`)
- Schema migrations (`schema.go`)
- Password encryption/decryption (`crypto.go`)
- Connection string building (`connstring.go`)
- Type definitions (`types.go`)

### Probes Package

- Base probe interface and utilities (`base.go`)
- 24 individual probe implementations (one file each)
- Probe constants (`constants.go`)

### Scheduler Package

- Probe scheduling and execution logic (`scheduler.go`)
- Parallel probe execution
- Error handling and logging

### Utils Package

- Shared utility functions
- Row scanning helpers

## Data Flow

The typical data flow through the Collector is:

1. **Startup**
   - Load configuration from file and command-line flags
   - Connect to datastore and verify/migrate schema
   - Initialize connection pool managers
   - Load probe configurations from datastore
   - Load monitored connections from datastore

2. **Normal Operation**
   - Each probe runs on its own schedule
   - When a probe timer fires:
     - Query monitored connections from datastore
     - For each connection, acquire a connection from the pool
     - Execute probe query against monitored database
     - Acquire datastore connection
     - Ensure partition exists for current time
     - Store metrics using COPY protocol
     - Release connections back to pools

3. **Garbage Collection**
   - Runs every 24 hours (first run after 5 minutes)
   - For each probe:
     - Calculate cutoff date based on retention days
     - Find partitions older than cutoff
     - Drop expired partitions

4. **Shutdown**
   - Stop accepting new probe executions
   - Wait for in-progress probes to complete
   - Stop garbage collector
   - Close all monitored connection pools
   - Close datastore connection pool

## Design Principles

The Collector is designed with several key principles:

### Reliability

- Graceful error handling
- Isolated probe execution (one probe failure doesn't affect others)
- Graceful shutdown with proper cleanup

### Efficiency

- Connection pooling to minimize connection overhead
- Parallel probe execution
- COPY protocol for bulk metric storage
- Automatic partition management

### Security

- Password encryption for stored credentials
- SSL/TLS support for all connections
- User isolation for monitored connections
- No credential exposure in logs

### Maintainability

- Clean separation of concerns
- Modular architecture
- Comprehensive test coverage
- Schema migration system for upgrades

### Scalability

- Efficient connection management
- Configurable concurrency limits
- Partitioned storage for large datasets
- Independent probe scheduling

## Performance Characteristics

The Collector is designed to be lightweight and efficient:

- **Memory**: Minimal memory footprint, primarily for connection pools
- **CPU**: Low CPU usage, spikes during probe execution
- **Disk I/O**: Moderate writes to datastore during metric collection
- **Network**: Depends on number of monitored servers and probe frequency
- **Concurrency**: Parallel probe execution with configurable limits

## Next Steps

To learn more about specific aspects of the Collector:

- [Quick Start Guide](quickstart.md) - Get the Collector running
- [Configuration Guide](configuration.md) - Configure the Collector
- [Architecture](architecture.md) - Detailed component architecture
- [Probes](probes.md) - How probes work in detail
- [Development Guide](development.md) - Contribute to the Collector
