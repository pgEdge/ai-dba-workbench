# System Architecture

This document provides a detailed technical overview of the Collector's
architecture, components, and implementation.

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Collector Process                        │
│                                                              │
│  ┌────────────┐  ┌─────────────┐  ┌──────────────────────┐ │
│  │   Main     │  │  Scheduler  │  │  Garbage Collector   │ │
│  │  Thread    │  │             │  │                      │ │
│  └────────────┘  └─────────────┘  └──────────────────────┘ │
│        │               │                      │             │
│        │         ┌─────┴─────┐                │             │
│        │         │           │                │             │
│        │    ┌────▼────┐ ┌───▼────┐           │             │
│        │    │ Probe 1 │ │ Probe 2│  ...      │             │
│        │    └────┬────┘ └───┬────┘           │             │
│        │         │           │                │             │
│  ┌─────▼─────────▼───────────▼────────────────▼──────────┐ │
│  │         Datastore Connection Pool (25 conns)          │ │
│  └───────────────────────────────┬────────────────────────┘ │
│                                  │                           │
│  ┌───────────────────────────────▼────────────────────────┐ │
│  │    Monitored Connection Pool Manager                   │ │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐      │ │
│  │  │ Server 1   │  │ Server 2   │  │ Server N   │      │ │
│  │  │ Pool (5)   │  │ Pool (5)   │  │ Pool (5)   │      │ │
│  │  └────────────┘  └────────────┘  └────────────┘      │ │
│  └───────────────────────────────────────────────────────┘  │
│                                                              │
└─────────────────────────────────────────────────────────────┘
                         │                    │
                         ▼                    ▼
              ┌──────────────────┐  ┌──────────────────┐
              │   Datastore DB   │  │  Monitored DBs   │
              │  (ai_workbench)  │  │  (1 to N hosts)  │
              └──────────────────┘  └──────────────────┘
```

## Core Components

### 1. Main Package

**Location**: `/collector/src/`

**Files**:

- `main.go` - Application entry point
- `config.go` - Configuration management
- `constants.go` - System constants
- `garbage_collector.go` - Data retention management

**Responsibilities**:

- Parse command-line arguments
- Load and validate configuration
- Initialize datastore connection
- Start all subsystems (scheduler, garbage collector)
- Handle shutdown signals
- Coordinate graceful shutdown

**Key Functions**:

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

### 2. Database Package

**Location**: `/collector/src/database/`

**Files**:

- `datastore.go` - Main datastore connection
- `datastore_pool.go` - Datastore connection pooling
- `monitored_pool.go` - Monitored server connection pooling
- `schema.go` - Schema migrations
- `crypto.go` - Password encryption/decryption
- `connstring.go` - Connection string building
- `types.go` - Type definitions

**Responsibilities**:

- Manage datastore connection pool
- Manage monitored connection pools (one per server)
- Apply schema migrations
- Encrypt/decrypt passwords
- Provide connection acquisition/release interface

**Key Types**:

```go
type Datastore struct {
    pool   *pgxpool.Pool  // Connection pool to datastore
    config Config         // Configuration
}

type MonitoredConnectionPoolManager struct {
    pools          map[int]*pgxpool.Pool  // Pool per connection ID
    semaphores     map[int]chan struct{}  // Concurrency limiter per server
    maxConnections int                    // Max per server
    maxIdleSeconds int                    // Idle timeout
    mu             sync.RWMutex           // Thread safety
}

type MonitoredConnection struct {
    ID                int     // Unique ID
    Name              string  // Display name
    Host              string  // Hostname
    Port              int     // Port number
    DatabaseName      string  // Database name
    Username          string  // Username
    PasswordEncrypted string  // Encrypted password
    // ... SSL/TLS fields ...
    OwnerUsername     string  // Owner user
    OwnerToken        string  // Owner token
}
```

**Connection Flow**:

1. **Datastore Connection**:
   ```
   Request → Datastore.GetConnection()
          → pgxpool.Acquire() (may block if pool full)
          → Connection acquired
          → Use connection
          → Datastore.ReturnConnection()
          → Connection returned to pool
   ```

2. **Monitored Connection**:
   ```
   Request → MonitoredConnectionPoolManager.GetConnection()
          → acquireSlot() (blocks if concurrency limit hit)
          → Get or create pool for server
          → pgxpool.Acquire()
          → Connection acquired
          → Use connection
          → ReturnConnection()
          → releaseSlot()
          → Connection returned to pool
   ```

### 3. Probes Package

**Location**: `/collector/src/probes/`

**Files**:

- `base.go` - Base probe interface and shared functions
- `constants.go` - Probe name constants
- `pg_stat_*.go` - 24 probe implementations (one per file)

**Responsibilities**:

- Define probe interface
- Implement data collection for each PostgreSQL system view
- Handle both server-scoped and database-scoped probes
- Store collected metrics using COPY protocol
- Manage weekly partitions

**Key Interface**:

```go
type MetricsProbe interface {
    GetName() string
    GetTableName() string
    GetQuery() string
    Execute(ctx context.Context, monitoredConn *pgxpool.Conn) ([]map[string]interface{}, error)
    Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error
    EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error
    GetConfig() *ProbeConfig
    IsDatabaseScoped() bool
}
```

**Probe Implementation Pattern**:

```go
type PgStatActivityProbe struct {
    BaseMetricsProbe
}

func (p *PgStatActivityProbe) GetQuery() string {
    return "SELECT ... FROM pg_stat_activity WHERE ..."
}

func (p *PgStatActivityProbe) Execute(ctx context.Context, conn *pgxpool.Conn) ([]map[string]interface{}, error) {
    rows, err := conn.Query(ctx, p.GetQuery())
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    return utils.ScanRowsToMaps(rows)
}

func (p *PgStatActivityProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
    // Ensure partition exists
    // Build values array
    // Use COPY protocol to store
}
```

### 4. Scheduler Package

**Location**: `/collector/src/scheduler/`

**Files**:

- `scheduler.go` - Probe scheduling and execution

**Responsibilities**:

- Schedule probe execution at configured intervals
- Execute probes in parallel across all monitored connections
- Handle database-scoped vs server-scoped probes
- Manage timeouts and error handling
- Coordinate connection acquisition from pools

**Key Type**:

```go
type ProbeScheduler struct {
    datastore    *database.Datastore
    poolManager  *database.MonitoredConnectionPoolManager
    serverSecret string
    config       Config
    probes       map[string]probes.MetricsProbe
    shutdownChan chan struct{}
    ctx          context.Context
    cancel       context.CancelFunc
    wg           sync.WaitGroup
}
```

**Scheduling Flow**:

```
Start()
  → Load probe configs from datastore
  → Create probe instances
  → For each probe:
      → Launch goroutine running scheduleProbe()

scheduleProbe(probe)
  → Run immediately on startup
  → Create ticker with probe interval
  → Loop:
      → Wait for tick or shutdown
      → executeProbe(probe)

executeProbe(probe)
  → Get all monitored connections from datastore
  → For each connection:
      → Launch goroutine executing executeProbeForConnection()
  → Wait for all goroutines to complete

executeProbeForConnection(probe, connection)
  → If server-scoped:
      → Acquire monitored connection
      → Execute probe.Execute()
      → Acquire datastore connection
      → Call probe.Store()
  → If database-scoped:
      → Acquire monitored connection
      → Query pg_database for database list
      → For each database:
          → Acquire connection to that database
          → Execute probe.Execute()
      → Acquire datastore connection
      → Call probe.Store() with all collected metrics
```

### 5. Garbage Collector

**Location**: `/collector/src/garbage_collector.go`

**Responsibilities**:

- Run daily cleanup operations
- Drop expired metric partitions
- Free up disk space

**Operation**:

```
Start()
  → Wait 5 minutes (startup delay)
  → collectGarbage()
  → Create 24-hour ticker
  → Loop:
      → Wait for tick or shutdown
      → collectGarbage()

collectGarbage()
  → Load probe configs from datastore
  → For each probe:
      → Calculate cutoff date (NOW - retention_days)
      → Query partitions for probe table
      → For each partition:
          → Parse partition date range
          → If end date < cutoff date:
              → DROP TABLE partition
              → Log dropped partition
```

## Data Storage Architecture

### Schema Organization

```
ai_workbench database
│
├── public schema
│   ├── schema_version (migration tracking)
│   ├── connections (monitored servers)
│   └── probes (probe configurations)
│
└── metrics schema
    ├── pg_stat_activity (partitioned)
    │   ├── pg_stat_activity_20251104 (week of Nov 4)
    │   ├── pg_stat_activity_20251111 (week of Nov 11)
    │   └── ...
    ├── pg_stat_database (partitioned)
    │   └── ...
    └── ... (22 more probe tables)
```

### Partitioning Strategy

**Partition Type**: Range partitioning by timestamp

**Partition Interval**: Weekly (Monday to Sunday)

**Partition Naming**: `{table_name}_{YYYYMMDD}` (date of Monday)

**Example**:

- Table: `metrics.pg_stat_activity`
- Partition: `metrics.pg_stat_activity_20251104`
- Range: `['2025-11-04 00:00:00', '2025-11-11 00:00:00')`

**Benefits**:

- Efficient queries by time range
- Easy deletion of old data (DROP partition vs DELETE rows)
- Better query planning
- Reduced index maintenance

### Metrics Table Structure

All metrics tables follow a similar pattern:

```sql
CREATE TABLE metrics.pg_stat_activity (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMP NOT NULL,
    -- View-specific columns --
    datid OID,
    datname TEXT,
    pid INTEGER,
    ...
) PARTITION BY RANGE (collected_at);
```

Common columns across all metric tables:

- `connection_id` - References `connections.id`
- `collected_at` - Timestamp when metric was collected

## Concurrency Model

### Goroutine Structure

```
main goroutine
├── scheduler goroutine (per probe: 24 total)
│   └── probe execution goroutines (per monitored connection)
│       └── database execution goroutines (if database-scoped)
└── garbage collector goroutine
```

### Synchronization

**Channels**:

- `shutdownChan` - Signals shutdown to all goroutines
- `semaphores` (per server) - Limits concurrent connections per server

**Wait Groups**:

- `scheduler.wg` - Tracks all probe goroutines
- Used in `executeProbe()` to wait for parallel executions

**Mutexes**:

- `MonitoredConnectionPoolManager.mu` - Protects pool map

### Shutdown Sequence

```
1. User presses Ctrl+C
2. waitForShutdown() returns
3. probeScheduler.Stop()
   → Close shutdownChan
   → Wait for all probe goroutines (wg.Wait())
4. garbageCollector.Stop()
   → Close shutdownChan
   → Wait for GC goroutine
5. poolManager.Close()
   → Close all monitored connection pools
6. datastore.Close()
   → Close datastore connection pool
7. Exit
```

## Performance Characteristics

### Memory Usage

**Baseline**: ~10-50 MB

**Per Connection**: ~1-5 MB (for connection pool)

**Per Probe Execution**: ~100 KB - 10 MB (depends on result set size)

**Estimation**: `50 MB + (5 MB × num_monitored_servers)`

### CPU Usage

**Baseline**: < 1% (idle)

**During Probe Execution**: 5-20% (depends on number of concurrent probes)

**Peak Usage**: When multiple probes execute simultaneously

### Network Traffic

**Per Probe Execution**: 10 KB - 10 MB (depends on result set size)

**Frequency**: Depends on probe intervals (30s to 15min)

**Bandwidth Estimate**:
- 10 monitored servers
- 24 probes averaging 5-minute intervals
- Average result size: 100 KB
- Bandwidth: `(10 × 24 × 100 KB) / 300s ≈ 80 KB/s`

### Disk I/O

**Write Rate**: Depends on metrics volume and collection frequency

**Read Rate**: Minimal (configuration queries, GC queries)

**Estimate**:
- 10 servers, 24 probes, 5-min intervals, 100 KB per probe
- Write rate: `(10 × 24 × 100 KB) / 300s ≈ 80 KB/s`

## Error Handling

### Strategy

**Isolation**: Errors in one probe don't affect others

**Logging**: All errors logged with context

**Retry**: No automatic retry (wait for next interval)

**Graceful Degradation**: Continue operation even if some probes fail

### Error Types

**Connection Errors**:
- Log error
- Return from probe execution
- Retry on next interval

**Query Errors**:
- Log error with query context
- Skip storing metrics
- Retry on next interval

**Storage Errors**:
- Log error
- Metrics lost for this interval
- Retry on next interval

**Configuration Errors**:
- Log error
- Skip probe
- Re-load config on next interval

## Security Architecture

### Password Encryption

**Algorithm**: AES-256-GCM

The server secret is loaded from a file (see `secret_file` configuration option).

**Key Derivation**:
```
key = SHA256(server_secret + username)
```

**Encryption Process**:
```
1. Derive key from server_secret and username
2. Generate random nonce
3. Encrypt password with AES-GCM
4. Prepend nonce to ciphertext
5. Base64 encode result
6. Store in connections.password_encrypted
```

**Decryption Process**:
```
1. Base64 decode encrypted password
2. Extract nonce (first 12 bytes)
3. Extract ciphertext (remaining bytes)
4. Derive key from server_secret and username
5. Decrypt with AES-GCM
6. Return plaintext password
```

### Isolation

**User Isolation**: Connections tagged with owner_token

**Connection Isolation**: Each monitored server has its own connection pool

**Data Isolation**: Metrics tagged with connection_id

## Extensibility Points

### Adding New Probes

1. Create new file in `probes/` package
2. Implement `MetricsProbe` interface
3. Add table creation to schema migrations
4. Add probe registration to scheduler
5. Insert probe configuration into `probes` table

### Custom Storage

The `MetricsProbe.Store()` method can be overridden for custom storage
logic (e.g., aggregation, transformation, external storage).

### Custom Scheduling

The scheduler can be extended to support:
- Custom intervals per connection
- Priority-based scheduling
- Adaptive intervals based on load

## Future Enhancements

Potential architectural improvements:

1. **Distributed Collection**: Multiple collector instances with work
   distribution
2. **Metrics Aggregation**: Pre-aggregate metrics before storage
3. **Compression**: Compress old metrics data
4. **External Storage**: Support for time-series databases
5. **Push Model**: Support for monitored servers pushing metrics
6. **Caching**: Cache probe results to reduce query frequency
7. **Dynamic Configuration**: Hot reload of configuration without restart

## See Also

- [Probes System](probes.md) - Detailed probe documentation
- [Scheduler](scheduler.md) - Scheduling implementation details
- [Database Schema](schema.md) - Schema structure and design
