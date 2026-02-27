# Collector Architecture

The pgEdge AI DBA Workbench Collector is a standalone
monitoring service written in Go. The Collector continuously
gathers PostgreSQL metrics and stores the data in a
centralized datastore. The service monitors multiple
PostgreSQL servers simultaneously through a flexible
probe system.

## Purpose

The Collector serves as the data collection engine for the
pgEdge AI Workbench system. The Collector provides the
following capabilities:

- The service continuously monitors PostgreSQL servers.
- The service collects metrics from standard PostgreSQL
  system views.
- The service stores time-series metrics data for analysis.
- The service manages data retention through automated
  garbage collection.
- The service provides isolation between different users
  and their connections.

## Key Concepts

This section introduces the core concepts that underpin
the Collector architecture.

### Datastore

The datastore is a PostgreSQL database that serves as
the central repository for collected metrics, connection
information, probe configurations, user accounts, and
schema version tracking. The datastore is separate from
the PostgreSQL servers being monitored.

### Monitored Connections

A monitored connection represents a PostgreSQL server
that the Collector should monitor. Each connection
includes connection parameters, SSL/TLS configuration,
ownership information, and monitoring status. Connections
are stored in the datastore `connections` table and can
be managed through the MCP server API.

### Probes

Probes are the data collection units in the Collector.
Each probe targets a specific PostgreSQL system view,
has a configurable collection interval and retention
period, and can be enabled or disabled individually.
The Collector stores probe data in partitioned tables.
The Collector includes 34 built-in probes covering
the most important PostgreSQL statistics views.

### Probe Types

Probes fall into two categories based on their scope.

#### Server-Scoped Probes

Server-scoped probes collect server-wide statistics and
run once per monitored connection. Examples include
`pg_stat_activity`, `pg_stat_replication`, and
`pg_stat_checkpointer`.

#### Database-Scoped Probes

Database-scoped probes collect per-database statistics
and run once for each database on a monitored server.
Examples include `pg_stat_database`, `pg_stat_all_tables`,
and `pg_stat_all_indexes`.

### Partitioning

Metrics tables use PostgreSQL declarative partitioning.
The system partitions tables by week, from Monday to
Sunday. The Collector creates partitions automatically
as needed and drops old partitions during garbage
collection. This approach provides efficient storage
and query performance.

### Garbage Collection

The garbage collector runs daily to drop expired metric
partitions based on retention settings. The first
collection runs 5 minutes after startup; subsequent
collections run every 24 hours.

### Connection Pooling

The Collector uses two types of connection pools.

#### Datastore Connection Pool

The datastore pool manages connections to the central
datastore. The `datastore_pool_max_connections` setting
controls the pool size, which defaults to 25 connections.

#### Monitored Connection Pools

Each monitored PostgreSQL server has its own connection
pool. The `monitored_pool_max_connections` setting
limits each pool to 5 connections by default. This
approach prevents overwhelming monitored servers and
allows connection reuse across probe executions.

### Password Encryption

The system encrypts all passwords for monitored
connections using AES-256-GCM encryption. The shared
`pkg/crypto` package handles encryption and decryption
operations. Key derivation uses PBKDF2 with SHA256
and 100,000 iterations. Each password uses a
cryptographically random 16-byte salt. Users do not
need to manually encrypt passwords; the MCP server
API handles encryption automatically.

## High-Level Architecture

The following diagram shows the major components of
the Collector process and their relationships.

```
┌──────────────────────────────────────────────────┐
│                Collector Process                  │
│                                                   │
│  ┌──────────┐ ┌───────────┐ ┌─────────────────┐ │
│  │   Main   │ │ Scheduler │ │    Garbage       │ │
│  │  Thread  │ │           │ │    Collector     │ │
│  └──────────┘ └───────────┘ └─────────────────┘ │
│       │             │                │            │
│       │       ┌─────┴─────┐         │            │
│       │       │           │         │            │
│       │  ┌────▼───┐ ┌────▼───┐     │            │
│       │  │Probe 1 │ │Probe 2 │ ... │            │
│       │  └────┬───┘ └────┬───┘     │            │
│       │       │           │         │            │
│  ┌────▼───────▼───────────▼─────────▼──────────┐│
│  │      Datastore Connection Pool (25)         ││
│  └──────────────────────┬──────────────────────┘│
│                         │                        │
│  ┌──────────────────────▼──────────────────────┐│
│  │   Monitored Connection Pool Manager         ││
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐    ││
│  │  │Server 1  │ │Server 2  │ │Server N  │    ││
│  │  │Pool (5)  │ │Pool (5)  │ │Pool (5)  │    ││
│  │  └──────────┘ └──────────┘ └──────────┘    ││
│  └─────────────────────────────────────────────┘│
└──────────────────────────────────────────────────┘
                    │                │
                    ▼                ▼
         ┌────────────────┐ ┌────────────────┐
         │  Datastore DB  │ │ Monitored DBs  │
         │ (ai_workbench) │ │ (1 to N hosts) │
         └────────────────┘ └────────────────┘
```

## Core Components

This section describes each core component in detail.

### Main Package

The main package resides in `/collector/src/` and
contains the following files:

- `main.go` provides the application entry point.
- `config.go` handles configuration management.
- `constants.go` defines system constants.
- `garbage_collector.go` manages data retention.

The main package parses command-line arguments, loads
and validates configuration, initializes the datastore
connection, starts all subsystems, handles shutdown
signals, and coordinates graceful shutdown.

The following code shows the key functions in the
main package:

```go
func main()
    - Entry point, orchestrates startup
    - Loads configuration
    - Initializes datastore
    - Starts probe scheduler
    - Starts garbage collector
    - Waits for shutdown signal
    - Performs graceful shutdown

func loadConfiguration() (*Config, error)
    - Determines config file path
    - Loads config from file
    - Applies command-line overrides
    - Returns validated configuration

func waitForShutdown()
    - Blocks until SIGINT or SIGTERM received
```

### Database Package

The database package resides in
`/collector/src/database/` and contains the following
files:

- `datastore.go` manages the main datastore connection.
- `datastore_pool.go` provides datastore connection
  pooling.
- `monitored_pool.go` handles monitored server
  connection pooling.
- `schema.go` applies schema migrations.
- `connstring.go` builds connection strings.
- `types.go` defines type definitions.

The shared `pkg/crypto` package handles password
encryption. The database package manages the datastore
connection pool, manages monitored connection pools,
applies schema migrations, and provides a connection
acquisition and release interface.

The following code shows the key types in the database
package:

```go
type Datastore struct {
    pool   *pgxpool.Pool
    config Config
}

type MonitoredConnectionPoolManager struct {
    pools          map[int]*pgxpool.Pool
    semaphores     map[int]chan struct{}
    maxConnections int
    maxIdleSeconds int
    mu             sync.RWMutex
}

type MonitoredConnection struct {
    ID                int
    Name              string
    Host              string
    Port              int
    DatabaseName      string
    Username          string
    PasswordEncrypted string
    // ... SSL/TLS fields ...
    OwnerUsername     string
    OwnerToken        string
}
```

#### Connection Flows

The datastore connection flow follows this sequence:

```
Request -> Datastore.GetConnection()
        -> pgxpool.Acquire() (may block if pool full)
        -> Connection acquired
        -> Use connection
        -> Datastore.ReturnConnection()
        -> Connection returned to pool
```

The monitored connection flow follows this sequence:

```
Request -> MonitoredConnectionPoolManager.GetConnection()
        -> acquireSlot() (blocks at concurrency limit)
        -> Get or create pool for server
        -> pgxpool.Acquire()
        -> Connection acquired
        -> Use connection
        -> ReturnConnection()
        -> releaseSlot()
        -> Connection returned to pool
```

### Probes Package

The probes package resides in
`/collector/src/probes/` and contains the following
files:

- `base.go` defines the base probe interface and
  shared functions.
- `constants.go` provides probe name constants.
- `pg_stat_*.go` files contain 34 individual probe
  implementations.

The probes package defines the probe interface,
implements data collection for each PostgreSQL system
view, handles both server-scoped and database-scoped
probes, stores collected metrics using the COPY
protocol, and manages weekly partitions.

For details on the `MetricsProbe` interface and
probe implementation patterns, see
[Probes](probes.md).

### Scheduler Package

The scheduler package resides in
`/collector/src/scheduler/` and contains the
`scheduler.go` file. The scheduler schedules probe
execution at configured intervals, executes probes in
parallel across all monitored connections, and manages
timeouts and error handling.

For details on the scheduling flow and concurrency
model, see [Scheduler](scheduler.md).

### Garbage Collector

The garbage collector resides in
`/collector/src/garbage_collector.go` and runs daily
cleanup operations. The garbage collector follows
this sequence:

```
Start()
  -> Wait 5 minutes (startup delay)
  -> collectGarbage()
  -> Create 24-hour ticker
  -> Loop:
      -> Wait for tick or shutdown
      -> collectGarbage()

collectGarbage()
  -> Load probe configs from datastore
  -> For each probe:
      -> Calculate cutoff date (NOW - retention_days)
      -> Query partitions for probe table
      -> For each partition:
          -> Parse partition date range
          -> If end date < cutoff date:
              -> DROP TABLE partition
              -> Log dropped partition
```

## Data Storage Architecture

This section describes how the Collector organizes
and stores metrics data.

### Schema Organization

The Collector uses two PostgreSQL schemas for data
storage.

```
ai_workbench database
|
+-- public schema
|   +-- schema_version (migration tracking)
|   +-- connections (monitored servers)
|   +-- probes (probe configurations)
|
+-- metrics schema
    +-- pg_stat_activity (partitioned)
    |   +-- pg_stat_activity_20251104 (week)
    |   +-- pg_stat_activity_20251111 (week)
    |   +-- ...
    +-- pg_stat_database (partitioned)
    |   +-- ...
    +-- ... (additional probe tables)
```

### Partitioning Strategy

The system uses range partitioning by timestamp with
a weekly interval from Monday to Sunday. Partition
names follow the format `{table_name}_{YYYYMMDD}`,
where the date represents Monday of the week.

Partitioning provides the following benefits:

- Queries filtering by time scan only relevant
  partitions.
- Dropping old data uses DROP rather than DELETE
  operations.
- Smaller per-partition indexes reduce maintenance
  overhead.
- PostgreSQL can operate on partitions in parallel.

### Metrics Table Structure

All metrics tables share a common structure with the
`connection_id` and `collected_at` columns. The
following example shows the general pattern:

```sql
CREATE TABLE metrics.{probe_name} (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMP NOT NULL,
    -- probe-specific columns --
) PARTITION BY RANGE (collected_at);
```

For detailed table definitions and querying examples,
see [Database Schema](schema.md).

## Concurrency Model

The Collector uses Go goroutines for concurrent
execution.

### Goroutine Structure

The following diagram shows the goroutine hierarchy:

```
main goroutine
+-- scheduler goroutine (per probe: 34 total)
|   +-- probe execution goroutines (per connection)
|       +-- database execution goroutines (if scoped)
+-- garbage collector goroutine
```

### Synchronization

The system uses several synchronization mechanisms.

Channels include `shutdownChan` for signaling
shutdown to all goroutines and per-server semaphores
for limiting concurrent connections. Wait groups
include `scheduler.wg` for tracking all probe
goroutines and local wait groups in `executeProbe()`
for parallel executions. The
`MonitoredConnectionPoolManager.mu` mutex protects
the pool map.

### Shutdown Sequence

The shutdown sequence proceeds as follows:

1. The user presses Ctrl+C.
2. The `waitForShutdown()` function returns.
3. The `probeScheduler.Stop()` method closes the
   shutdown channel and waits for all probe goroutines.
4. The `garbageCollector.Stop()` method closes the
   shutdown channel and waits for the GC goroutine.
5. The `poolManager.Close()` method closes all
   monitored connection pools.
6. The `datastore.Close()` method closes the datastore
   connection pool.
7. The process exits.

## Error Handling

The Collector uses an isolation strategy where errors
in one probe do not affect other probes. All errors
are logged with context. The system does not retry
automatically; probes wait for the next scheduled
interval.

### Error Types

The system handles four types of errors:

- Connection errors cause the probe to log the error,
  return from execution, and retry on the next
  interval.
- Query errors cause the probe to log the error with
  query context and skip storing metrics.
- Storage errors cause the probe to log the error;
  metrics are lost for that interval.
- Configuration errors cause the probe to log the
  error, skip the probe, and reload configuration
  on the next interval.

## Security Architecture

This section describes the security mechanisms used
by the Collector.

### Password Encryption

The system uses AES-256-GCM encryption with PBKDF2
key derivation. The server secret is loaded from a
file specified by the `secret_file` configuration
option. The shared `pkg/crypto` package handles all
encryption operations.

The encryption process follows these steps:

1. Generate a random 16-byte salt.
2. Derive a 256-bit key using PBKDF2 with the server
   secret and salt.
3. Generate a random 12-byte nonce.
4. Encrypt the password with AES-256-GCM.
5. Concatenate salt, nonce, and ciphertext.
6. Base64-encode the result.
7. Store the result in `connections.password_encrypted`.

Users should not manually encrypt passwords; the MCP
server API handles encryption automatically.

### Isolation

The system provides three levels of isolation:

- User isolation tags connections with `owner_token`.
- Connection isolation gives each monitored server
  its own connection pool.
- Data isolation tags metrics with `connection_id`.

## Performance Characteristics

This section describes the resource usage profile
of the Collector.

### Memory Usage

The baseline memory footprint is approximately
10-50 MB. Each monitored connection adds 1-5 MB
for the connection pool. Each probe execution uses
100 KB to 10 MB depending on the result set size.
A rough estimate is 50 MB plus 5 MB per monitored
server.

### CPU Usage

The baseline CPU usage is below 1 percent when idle.
During probe execution, CPU usage rises to 5-20
percent depending on the number of concurrent probes.

### Network Traffic

Network usage depends on monitored servers and probe
frequency. For 10 monitored servers with 34 probes
averaging 5-minute intervals and 100 KB average
result size, the estimated bandwidth is approximately
113 KB/s.

### Disk I/O

Write rate depends on metrics volume and collection
frequency. Read rate is minimal, limited to
configuration queries and garbage collection queries.

## Design Principles

The Collector is designed around five key principles.

### Reliability

The system provides graceful error handling, isolated
probe execution, and graceful shutdown with proper
cleanup.

### Efficiency

The system uses connection pooling, parallel probe
execution, the COPY protocol for bulk metric storage,
and automatic partition management.

### Security

The system provides password encryption, SSL/TLS
support, user isolation, and no credential exposure
in logs.

### Maintainability

The system has clean separation of concerns, modular
architecture, comprehensive test coverage, and a
schema migration system.

### Scalability

The system provides efficient connection management,
configurable concurrency limits, partitioned storage,
and independent probe scheduling.

## See Also

The following resources provide additional details
on specific aspects of the Collector.

- [Probes](probes.md) describes the probe system
  internals.
- [Scheduler](scheduler.md) covers scheduling
  implementation details.
- [Database Schema](schema.md) documents the schema
  structure and design.
- [Testing and Development](testing.md) covers the
  development environment and test practices.
