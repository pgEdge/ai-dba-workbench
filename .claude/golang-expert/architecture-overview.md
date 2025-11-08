/*-----------------------------------------------------------
 *
 * pgEdge AI Workbench - Go Backend Architecture Overview
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Go Backend Architecture Overview

This document provides a comprehensive overview of the Go backend architecture
for the pgEdge AI Workbench project, covering both the MCP server and collector
components.

## Project Structure

The Go codebase is divided into two main components:

### 1. MCP Server (`/server/src`)

The MCP (Model Context Protocol) server provides a JSON-RPC 2.0 API for AI
models to interact with PostgreSQL databases through tools, resources, and
prompts.

**Directory Structure:**
```
server/src/
├── main.go              # Application entry point
├── config/              # Configuration management
├── database/            # Database connection handling
├── mcp/                 # MCP protocol implementation
├── privileges/          # RBAC authorization logic
├── usermgmt/           # User and token management
├── groupmgmt/          # Group management
├── integration/        # Integration utilities
├── logger/             # Logging infrastructure
└── server/             # HTTP/HTTPS server
```

**Key Responsibilities:**
- Serve MCP protocol over HTTP/HTTPS
- Authenticate users via bearer tokens (user passwords, service tokens, user
  tokens)
- Authorize operations via RBAC (Role-Based Access Control)
- Manage database connections for authenticated users
- Execute SQL queries and return results
- Provide MCP tools for database operations and user management

### 2. Collector (`/collector/src`)

The collector is a background daemon that continuously monitors PostgreSQL
databases by running scheduled probes and storing metrics in a central
datastore.

**Directory Structure:**
```
collector/src/
├── main.go              # Application entry point
├── config.go            # Configuration management
├── garbage_collector.go # Partition cleanup
├── database/            # Database connection and schema management
├── probes/              # Metrics collection probes
├── scheduler/           # Probe scheduling and execution
├── logger/             # Logging infrastructure
└── utils/              # Utility functions
```

**Key Responsibilities:**
- Monitor multiple PostgreSQL servers concurrently
- Execute metrics collection probes on configurable schedules
- Store time-series metrics data in partitioned tables
- Manage connection pools for monitored databases
- Clean up expired partitions based on retention policies
- Handle both server-wide and database-scoped probes

## Core Architectural Patterns

### 1. Connection Pool Management

Both components use `pgx/v5/pgxpool` for PostgreSQL connection pooling:

**Server Pattern:**
- Single connection pool to the datastore database
- Each MCP request gets a connection from the pool via `Acquire()`
- Connections are released immediately after use via `Release()`
- Pool configured with max connections, idle timeout, and health checks

**Collector Pattern:**
- **Datastore Pool:** Single pool for storing metrics (pgx default pooling)
- **Monitored Pools:** Separate pools per monitored database connection
- **Semaphore-based Limiting:** Per-connection semaphores limit concurrent
  connections to monitored servers (prevents overwhelming them)
- **Dynamic Pool Creation:** Pools created on-demand and cached
- **Database-Scoped Probes:** Additional pools created per database when needed

Example from collector:
```go
// Acquire semaphore slot before getting connection
if err := m.acquireSlot(ctx, conn.ID); err != nil {
    return nil, fmt.Errorf("failed to acquire connection slot: %w", err)
}

// Get connection from pool
pgxConn, err := pool.Acquire(ctx)
if err != nil {
    m.releaseSlot(conn.ID)
    return nil, err
}

// Connection is used, then released
defer m.releaseSlot(conn.ID)
defer pgxConn.Release()
```

### 2. Configuration Management

Both components follow the same configuration pattern:

1. **Default Values:** Hardcoded defaults in `NewConfig()`
2. **Config File:** Key-value format loaded from `ai-workbench.conf` or
   `server.conf`
3. **Command-Line Flags:** Override config file values
4. **Validation:** `Validate()` method checks required fields

Configuration is loaded once at startup and passed to components via interfaces,
avoiding import cycles.

### 3. Error Handling

Standard Go error handling with explicit checks and context wrapping:

```go
conn, err := ds.GetConnection()
if err != nil {
    return fmt.Errorf("failed to get connection: %w", err)
}
defer ds.ReturnConnection(conn)

result, err := performOperation(conn)
if err != nil {
    return fmt.Errorf("failed to perform operation: %w", err)
}
```

**Key Patterns:**
- Always use `fmt.Errorf` with `%w` verb to wrap errors
- Provide context in error messages
- Use `defer` for cleanup (connection release, transaction rollback)
- Check context cancellation in long-running operations
- Return early on errors to minimize nesting

### 4. Context Management

Contexts are used throughout for cancellation and timeouts:

**Server:**
- HTTP requests create request-scoped contexts
- Database operations use request context for automatic cancellation
- Graceful shutdown uses context with timeout

**Collector:**
- Global context for application lifecycle
- Per-probe contexts with timeouts for query execution
- Context cancellation triggers graceful shutdown

Example:
```go
// Create timeout context for database operation
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

conn, err := pool.Acquire(ctx)
if err != nil {
    if ctx.Err() == context.DeadlineExceeded {
        return fmt.Errorf("timeout acquiring connection")
    }
    return fmt.Errorf("failed to acquire connection: %w", err)
}
defer conn.Release()
```

### 5. Goroutine Management

Both components use goroutines with proper lifecycle management:

**Pattern:**
```go
type Component struct {
    wg           sync.WaitGroup
    shutdownChan chan struct{}
    ctx          context.Context
    cancel       context.CancelFunc
}

func (c *Component) Start() {
    c.wg.Add(1)
    go c.worker()
}

func (c *Component) worker() {
    defer c.wg.Done()

    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-c.shutdownChan:
            return
        case <-c.ctx.Done():
            return
        case <-ticker.C:
            c.doWork()
        }
    }
}

func (c *Component) Stop() {
    c.cancel()
    close(c.shutdownChan)
    c.wg.Wait()
}
```

**Key Points:**
- Use `sync.WaitGroup` to track active goroutines
- Provide shutdown channel and context for cancellation
- Always call `defer wg.Done()` at start of goroutine
- Wait for all goroutines to finish in `Stop()`

## Database Schema Management

### Server

The server relies on the collector to create and maintain the schema. It only
validates connectivity and seeds MCP privilege identifiers.

### Collector

The collector uses a migration-based schema manager:

```go
type SchemaManager struct {
    migrations []Migration
}

type Migration struct {
    Version     int
    Description string
    Up          string  // SQL to apply migration
    Down        string  // SQL to rollback (not implemented)
}

func (sm *SchemaManager) Migrate(conn *pgxpool.Conn) error {
    // Check current version
    currentVersion := sm.getCurrentVersion(conn)

    // Apply migrations in order
    for _, migration := range sm.migrations {
        if migration.Version > currentVersion {
            if err := sm.applyMigration(conn, migration); err != nil {
                return err
            }
        }
    }

    return nil
}
```

**Key Features:**
- Sequential migration versioning
- Idempotent migration application
- Version tracking in `schema_version` table
- Automatic schema initialization on first run
- Weekly partitioned tables for metrics with automatic partition creation

## Dependency Injection

Components receive dependencies through constructors, enabling testability:

```go
// Server MCP Handler
func NewHandler(
    serverName string,
    serverVersion string,
    dbPool *pgxpool.Pool,
    cfg *config.Config,
) *Handler {
    return &Handler{
        serverName:    serverName,
        serverVersion: serverVersion,
        dbPool:        dbPool,
        config:        cfg,
    }
}

// Collector Scheduler
func NewProbeScheduler(
    datastore *database.Datastore,
    poolManager *database.MonitoredConnectionPoolManager,
    config Config,
    serverSecret string,
) *ProbeScheduler {
    return &ProbeScheduler{
        datastore:    datastore,
        poolManager:  poolManager,
        config:       config,
        serverSecret: serverSecret,
        probesByConn: make(map[int]map[string]probes.MetricsProbe),
    }
}
```

**Benefits:**
- Easy to mock dependencies in tests
- Clear component boundaries
- Explicit dependency graph
- Supports unit testing without database

## Graceful Shutdown

Both components implement graceful shutdown:

```go
// Setup signal handling
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

// Start components
go startServer()

// Wait for signal
<-sigChan

// Shutdown with timeout
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

// Stop components in reverse order
probeScheduler.Stop()
garbageCollector.Stop()
poolManager.Close()
datastore.Close()
```

**Shutdown Order (Collector):**
1. Stop probe scheduler (no new queries)
2. Stop garbage collector (no new cleanup)
3. Close monitored connection pools (all probe connections)
4. Close datastore connection pool (last to close)

**Shutdown Order (Server):**
1. Stop accepting new HTTP requests
2. Wait for in-flight requests to complete
3. Close database connection pool

## Security Considerations

### Authentication

**Server:**
- Bearer token authentication via HTTP Authorization header
- Three token types: user passwords (SHA256), service tokens, user tokens
- Tokens stored as SHA256 hashes in database
- Token expiry checked on each request

### Authorization

**RBAC Model:**
- Users belong to groups (recursive hierarchy supported)
- Groups have privileges on connections (read/read_write)
- Groups have privileges on MCP tools/resources/prompts
- Superusers bypass all checks
- Privilege checks use recursive CTEs to resolve group hierarchies

**Connection Isolation:**
- Each connection is owned by a user or token
- Shared connections restricted by group privileges
- Connection credentials encrypted with server secret
- MCP handlers validate user access before returning connection data

### SQL Injection Prevention

**Server:**
- All user input passed as parameterized queries
- No string concatenation for SQL generation
- Exception: MCP tool for arbitrary SQL execution (documented risk)

**Collector:**
- Probe queries are hardcoded, not user-supplied
- Table names validated against probe definitions
- Connection strings built with pgx connection string builder

## Performance Optimizations

### 1. Connection Pooling

- Reuse connections instead of creating new ones
- Configurable pool sizes based on workload
- Health checks to detect failed connections

### 2. Batch Operations

- Probe metrics stored in batches (up to 100 rows per INSERT)
- COPY protocol initially attempted but not supported for partitioned tables
- Transactions used to ensure atomicity

### 3. Partitioning

- Metrics tables partitioned by week
- Partitions created on-demand
- Old partitions dropped automatically based on retention
- Significant performance improvement for queries

### 4. Concurrent Execution

- Collector runs probes concurrently across connections
- Semaphores limit concurrent connections per server
- Goroutines used for parallel probe execution
- Database-scoped probes parallelized across databases

### 5. Change Detection

- Some probes (pg_settings, pg_hba_file_rules) only store data when changed
- SHA256 hash comparison to detect changes
- Reduces storage and query overhead

## Testing Strategy

See `testing-strategy.md` for detailed testing approach.

## Logging

Both components use a custom logger package:

```go
logger.Init()
logger.SetVerbose(verbose)

logger.Startup("Application started")  // Startup messages
logger.Info("Informational message")   // General info
logger.Infof("Formatted: %s", value)   // Formatted info
logger.Error("Error occurred")         // Error without exit
logger.Errorf("Error: %v", err)        // Formatted error
logger.Fatal("Fatal error")            // Error and exit
logger.Fatalf("Fatal: %v", err)        // Formatted fatal
```

**Log Levels:**
- **Startup:** Application lifecycle events
- **Info:** Normal operations, probe execution, metrics stored
- **Error:** Recoverable errors (logged but execution continues)
- **Fatal:** Unrecoverable errors (application exits)

**Verbose Mode:**
- Enabled via `-v` flag
- Additional debug information
- SQL query logging (if implemented)

## Future Enhancements

1. **Distributed Tracing:** Add OpenTelemetry for distributed tracing
2. **Metrics Exposition:** Prometheus metrics for server and collector
3. **Circuit Breakers:** Prevent cascade failures when databases are down
4. **Rate Limiting:** Protect against excessive API usage
5. **Connection Rebalancing:** Dynamically adjust pool sizes based on load
6. **Query Result Caching:** Cache frequent queries in server
7. **Probe Result Compression:** Compress stored metrics to reduce storage
