# Scheduler Implementation

This document explains the implementation details
of the probe scheduler.

## Overview

The `ProbeScheduler` is responsible for the following
tasks:

- The scheduler loads probe configurations from the
  database.
- The scheduler manages probe execution timers.
- The scheduler coordinates parallel probe execution.
- The scheduler handles timeouts and errors.
- The scheduler performs graceful shutdown.

## Scheduler Structure

The following code shows the `ProbeScheduler` struct:

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

## Initialization

The scheduler starts by loading configurations
and launching goroutines.

### Start Sequence

The following code shows the start sequence:

```go
func (ps *ProbeScheduler) Start(
    ctx context.Context) error {
    // 1. Load probe configurations from database
    conn, err := ps.datastore.GetConnection()
    if err != nil {
        return err
    }

    configs, err := probes.LoadProbeConfigs(
        ctx, conn)
    ps.datastore.ReturnConnection(conn)
    if err != nil {
        return err
    }

    // 2. Initialize probe instances
    for _, config := range configs {
        probe := ps.createProbe(&config)
        if probe != nil {
            ps.probes[config.Name] = probe
        }
    }

    // 3. Start scheduling goroutines
    for _, probe := range ps.probes {
        ps.wg.Add(1)
        go ps.scheduleProbe(probe)
    }

    return nil
}
```

## Probe Scheduling

Each probe runs in its own goroutine with an
independent timer. The following code shows the
scheduling loop:

```go
func (ps *ProbeScheduler) scheduleProbe(
    probe probes.MetricsProbe) {
    defer ps.wg.Done()

    config := probe.GetConfig()
    interval := time.Duration(
        config.CollectionIntervalSeconds,
    ) * time.Second
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    // Execute immediately on startup
    ps.executeProbe(ps.ctx, probe)

    // Then execute on timer
    for {
        select {
        case <-ps.shutdownChan:
            return
        case <-ps.ctx.Done():
            return
        case <-ticker.C:
            ps.executeProbe(ps.ctx, probe)
        }
    }
}
```

## Probe Execution

The scheduler executes each probe against all
monitored connections in parallel.

### Execution Overview

The following code shows how the scheduler executes
a probe:

```go
func (ps *ProbeScheduler) executeProbe(
    ctx context.Context,
    probe probes.MetricsProbe) {
    // 1. Get all monitored connections
    connections, err :=
        ps.datastore.GetMonitoredConnections()
    if err != nil {
        log.Printf(
            "Error getting connections: %v", err)
        return
    }

    // 2. Execute probe for each connection
    var wg sync.WaitGroup
    for _, conn := range connections {
        wg.Add(1)
        go func(c database.MonitoredConnection) {
            defer wg.Done()
            ps.executeProbeForConnection(
                ctx, probe, c)
        }(conn)
    }

    // 3. Wait for all to complete
    wg.Wait()
}
```

### Per-Connection Execution

The following code shows how the scheduler executes
a probe for a single connection:

```go
func (ps *ProbeScheduler) executeProbeForConnection(
    ctx context.Context,
    probe probes.MetricsProbe,
    conn database.MonitoredConnection) {

    timeout := time.Duration(
        ps.config.GetMonitoredPoolMaxWaitSeconds(),
    ) * time.Second
    execCtx, cancel := context.WithTimeout(
        ctx, timeout)
    defer cancel()

    timestamp := time.Now()
    var allMetrics []map[string]interface{}

    if probe.IsDatabaseScoped() {
        allMetrics =
            ps.executeProbeForAllDatabases(
                execCtx, probe, conn)
    } else {
        allMetrics =
            ps.executeProbeForServerWide(
                execCtx, probe, conn)
    }

    if execCtx.Err() == context.DeadlineExceeded {
        log.Printf("Probe %s timed out for %s",
            probe.GetName(), conn.Name)
        return
    }

    if len(allMetrics) > 0 {
        ps.storeMetrics(ctx, probe, conn.ID,
            timestamp, allMetrics)
    }
}
```

## Server-Scoped Execution

Server-scoped probes execute once per monitored
connection. The following code shows the execution
flow for server-wide probes:

```go
func (ps *ProbeScheduler) executeProbeForServerWide(
    ctx context.Context,
    probe probes.MetricsProbe,
    conn database.MonitoredConnection,
) []map[string]interface{} {

    monitoredConn, err :=
        ps.poolManager.GetConnection(
            ctx, conn, ps.serverSecret)
    if err != nil {
        log.Printf(
            "Error getting connection: %v", err)
        return nil
    }
    defer ps.poolManager.ReturnConnection(
        conn.ID, monitoredConn)

    metrics, err := probe.Execute(
        ctx, monitoredConn)
    if err != nil {
        log.Printf(
            "Error executing probe: %v", err)
        return nil
    }

    return metrics
}
```

## Database-Scoped Execution

Database-scoped probes execute once for each database
on a monitored server. The scheduler queries
`pg_database` for the database list and then executes
the probe against each database.

The following code shows the execution flow for
database-scoped probes:

```go
func (ps *ProbeScheduler)
    executeProbeForAllDatabases(
    ctx context.Context,
    probe probes.MetricsProbe,
    conn database.MonitoredConnection,
) []map[string]interface{} {

    var allMetrics []map[string]interface{}

    defaultConn, err :=
        ps.poolManager.GetConnection(
            ctx, conn, ps.serverSecret)
    if err != nil {
        return allMetrics
    }

    databases, err := ps.getDatabaseList(
        ctx, defaultConn)
    if err != nil {
        ps.poolManager.ReturnConnection(
            conn.ID, defaultConn)
        return allMetrics
    }

    // Execute on first database
    if len(databases) > 0 {
        metrics, err := probe.Execute(
            ctx, defaultConn)
        if err == nil {
            for i := range metrics {
                metrics[i]["_database_name"] =
                    databases[0]
            }
            allMetrics = append(
                allMetrics, metrics...)
        }
    }
    ps.poolManager.ReturnConnection(
        conn.ID, defaultConn)

    // Execute on remaining databases
    for i := 1; i < len(databases); i++ {
        dbName := databases[i]
        dbConn, err :=
            ps.poolManager.GetConnectionForDatabase(
                ctx, conn, dbName,
                ps.serverSecret)
        if err != nil {
            continue
        }

        metrics, err := probe.Execute(
            ctx, dbConn)
        ps.poolManager.ReturnConnection(
            conn.ID, dbConn)

        if err == nil {
            for j := range metrics {
                metrics[j]["_database_name"] =
                    dbName
            }
            allMetrics = append(
                allMetrics, metrics...)
        }
    }

    return allMetrics
}
```

## Metrics Storage

The following code shows how the scheduler stores
collected metrics:

```go
func (ps *ProbeScheduler) storeMetrics(
    ctx context.Context,
    probe probes.MetricsProbe,
    connectionID int,
    timestamp time.Time,
    metrics []map[string]interface{}) {

    timeout := time.Duration(
        ps.config.GetDatastorePoolMaxWaitSeconds(),
    ) * time.Second
    storeCtx, cancel := context.WithTimeout(
        ctx, timeout)
    defer cancel()

    datastoreConn, err :=
        ps.datastore.GetConnectionWithContext(
            storeCtx)
    if err != nil {
        log.Printf(
            "Error getting datastore conn: %v",
            err)
        return
    }
    defer ps.datastore.ReturnConnection(
        datastoreConn)

    err = probe.Store(storeCtx, datastoreConn,
        connectionID, timestamp, metrics)
    if err != nil {
        log.Printf(
            "Error storing metrics: %v", err)
    }
}
```

## Shutdown Sequence

The following code shows the shutdown sequence:

```go
func (ps *ProbeScheduler) Stop() {
    // 1. Cancel context
    ps.cancel()

    // 2. Signal all goroutines to stop
    close(ps.shutdownChan)

    // 3. Wait for all probe goroutines
    ps.wg.Wait()

    log.Println("Probe scheduler stopped")
}
```

## Concurrency Control

The scheduler manages concurrency at several levels.

### Per-Probe Goroutines

Each probe has one goroutine managing its schedule,
giving a total of 34 goroutines for the standard
probe set.

### Per-Connection Goroutines

When a probe executes, the scheduler spawns one
goroutine per connection. These goroutines are
short-lived and last only for the duration of probe
execution. With 10 monitored servers, the system
can produce up to 340 concurrent goroutines during
probe execution.

### Connection Pool Limits

The datastore uses a global limit of 25 connections
by default. Monitored servers use a per-server limit
of 5 connections each by default.

### Semaphores

Each monitored server has a semaphore implemented as
a buffered channel. The semaphore limits concurrent
probe executions per server.

The following code shows the semaphore
implementation:

```go
type MonitoredConnectionPoolManager struct {
    semaphores     map[int]chan struct{}
    maxConnections int
}

func (m *MonitoredConnectionPoolManager) acquireSlot(
    ctx context.Context,
    connectionID int) error {
    sem := m.getSemaphore(connectionID)
    select {
    case sem <- struct{}{}:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}

func (m *MonitoredConnectionPoolManager) releaseSlot(
    connectionID int) {
    sem := m.getSemaphore(connectionID)
    <-sem
}
```

## Timeout Handling

The scheduler applies timeouts at two stages.

### Execution Timeout

Each probe execution has a timeout equal to
`monitored_pool_max_wait_seconds`. If the timeout
is exceeded, the probe execution is cancelled and
the error is logged. The probe retries on the next
interval.

### Storage Timeout

Metric storage has its own timeout equal to
`datastore_pool_max_wait_seconds`. The system
applies this timeout independently from the
execution timeout.

## Error Handling

The scheduler handles errors at several levels.

### Connection Errors

The scheduler logs connection errors with the
connection name and error message. The probe is
skipped for that connection and retries on the next
interval.

### Execution Errors

The scheduler logs execution errors with the probe
name, connection, and error message. Metrics are
not collected for that interval.

### Storage Errors

The scheduler logs storage errors with the probe
name. Metrics are lost for that interval and the
probe continues on the next interval.

### Timeout Errors

The scheduler logs timeout errors with the timeout
duration. The execution is cancelled and retries
on the next interval.

## Performance Characteristics

This section describes the scheduler performance
profile.

### Scheduling Overhead

The scheduling overhead is minimal because each
probe sleeps on a timer. The 34 goroutines with
minimal stack sizes consume approximately 2-4 MB
of memory.

### Execution Parallelism

Probes execute independently of each other. The
scheduler processes connections in parallel, limited
by connection pool sizes.

### Memory Usage

Each probe execution uses 100 KB to 10 MB depending
on the result set size. Peak memory occurs during
mass probe execution. The system releases memory
when metrics are stored.

## Monitoring the Scheduler

This section describes how to monitor the scheduler.

### Check Probe Timing

Watch the logs for probe execution times:

```
2025/11/05 10:00:00 Probe pg_stat_activity
    on Server1 completed in 45.23ms
```

### Check for Timeouts

Look for timeout messages in the logs:

```
2025/11/05 10:00:00 Probe pg_stat_activity
    timed out for Server1 (timeout: 60 seconds)
```

### Check Probe Status

In the following example, the query shows the most
recent collection time for each connection:

```sql
SELECT
    c.name,
    MAX(pa.collected_at)
        AS last_activity_collection,
    MAX(pd.collected_at)
        AS last_database_collection
FROM connections c
LEFT JOIN metrics.pg_stat_activity pa
    ON c.id = pa.connection_id
LEFT JOIN metrics.pg_stat_database pd
    ON c.id = pd.connection_id
WHERE c.is_monitored = TRUE
GROUP BY c.name
ORDER BY c.name;
```

## Tuning the Scheduler

This section describes how to tune the scheduler
for optimal performance.

### Adjusting Collection Intervals

Balance data freshness against system load by
adjusting collection intervals. In the following
example, the commands adjust intervals for specific
probes:

```sql
-- Reduce frequency for low-priority probes
UPDATE probes
SET collection_interval_seconds = 900
WHERE name = 'pg_stat_io';

-- Increase frequency for critical probes
UPDATE probes
SET collection_interval_seconds = 30
WHERE name = 'pg_stat_replication';
```

### Adjusting Timeouts

Increase timeouts if the logs show timeout errors.
In the following example, the configuration file
settings increase both timeouts:

```ini
monitored_pool_max_wait_seconds = 120
datastore_pool_max_wait_seconds = 90
```

### Adjusting Concurrency

Increase pool sizes for better parallelism. In the
following example, the configuration file settings
increase both pool sizes:

```ini
monitored_pool_max_connections = 10
datastore_pool_max_connections = 50
```

## See Also

The following resources provide additional details.

- [Probes](probes.md) covers the probe system
  internals.
- [Architecture](architecture.md) describes the
  overall system design.
- [Testing and Development](testing.md) covers
  the development environment.
