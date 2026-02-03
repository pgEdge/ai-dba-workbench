# Scheduler Implementation

This document explains the implementation details of the probe scheduler.

## Overview

The ProbeScheduler is responsible for:

- Loading probe configurations
- Managing probe execution timers
- Coordinating parallel probe execution
- Handling timeouts and errors
- Gracefully shutting down

## Scheduler Structure

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

### Start Sequence

```go
func (ps *ProbeScheduler) Start(ctx context.Context) error {
    // 1. Load probe configurations from database
    conn, err := ps.datastore.GetConnection()
    if err != nil {
        return err
    }
    
    configs, err := probes.LoadProbeConfigs(ctx, conn)
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

Each probe runs in its own goroutine with an independent timer:

```go
func (ps *ProbeScheduler) scheduleProbe(probe probes.MetricsProbe) {
    defer ps.wg.Done()
    
    config := probe.GetConfig()
    interval := time.Duration(config.CollectionIntervalSeconds) * time.Second
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

### Overview

```go
func (ps *ProbeScheduler) executeProbe(ctx context.Context, 
                                       probe probes.MetricsProbe) {
    // 1. Get all monitored connections
    connections, err := ps.datastore.GetMonitoredConnections()
    if err != nil {
        log.Printf("Error getting monitored connections: %v", err)
        return
    }
    
    // 2. Execute probe for each connection in parallel
    var wg sync.WaitGroup
    for _, conn := range connections {
        wg.Add(1)
        go func(c database.MonitoredConnection) {
            defer wg.Done()
            ps.executeProbeForConnection(ctx, probe, c)
        }(conn)
    }
    
    // 3. Wait for all to complete
    wg.Wait()
}
```

### Per-Connection Execution

```go
func (ps *ProbeScheduler) executeProbeForConnection(
    ctx context.Context,
    probe probes.MetricsProbe,
    conn database.MonitoredConnection) {
    
    // Create timeout context
    timeout := time.Duration(ps.config.GetMonitoredPoolMaxWaitSeconds()) * time.Second
    execCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    
    timestamp := time.Now()
    var allMetrics []map[string]interface{}
    
    // Execute based on scope
    if probe.IsDatabaseScoped() {
        allMetrics = ps.executeProbeForAllDatabases(execCtx, probe, conn)
    } else {
        allMetrics = ps.executeProbeForServerWide(execCtx, probe, conn)
    }
    
    // Check for timeout
    if execCtx.Err() == context.DeadlineExceeded {
        log.Printf("Probe %s timed out for %s", probe.GetName(), conn.Name)
        return
    }
    
    // Store metrics
    if len(allMetrics) > 0 {
        ps.storeMetrics(ctx, probe, conn.ID, timestamp, allMetrics)
    }
}
```

## Server-Scoped Execution

For server-wide probes (e.g., pg_stat_activity):

```go
func (ps *ProbeScheduler) executeProbeForServerWide(
    ctx context.Context,
    probe probes.MetricsProbe,
    conn database.MonitoredConnection) []map[string]interface{} {
    
    // Get monitored connection
    monitoredConn, err := ps.poolManager.GetConnection(ctx, conn, ps.serverSecret)
    if err != nil {
        log.Printf("Error getting connection: %v", err)
        return nil
    }
    defer ps.poolManager.ReturnConnection(conn.ID, monitoredConn)
    
    // Execute probe
    metrics, err := probe.Execute(ctx, monitoredConn)
    if err != nil {
        log.Printf("Error executing probe: %v", err)
        return nil
    }
    
    return metrics
}
```

## Database-Scoped Execution

For per-database probes (e.g., pg_stat_database):

```go
func (ps *ProbeScheduler) executeProbeForAllDatabases(
    ctx context.Context,
    probe probes.MetricsProbe,
    conn database.MonitoredConnection) []map[string]interface{} {
    
    var allMetrics []map[string]interface{}
    
    // Get connection to default database
    defaultConn, err := ps.poolManager.GetConnection(ctx, conn, ps.serverSecret)
    if err != nil {
        return allMetrics
    }
    
    // Query for database list
    databases, err := ps.getDatabaseList(ctx, defaultConn)
    if err != nil {
        ps.poolManager.ReturnConnection(conn.ID, defaultConn)
        return allMetrics
    }
    
    // Execute on first database using existing connection
    if len(databases) > 0 {
        metrics, err := probe.Execute(ctx, defaultConn)
        if err == nil {
            for i := range metrics {
                metrics[i]["_database_name"] = databases[0]
            }
            allMetrics = append(allMetrics, metrics...)
        }
    }
    ps.poolManager.ReturnConnection(conn.ID, defaultConn)
    
    // Execute on remaining databases
    for i := 1; i < len(databases); i++ {
        dbName := databases[i]
        
        // Get connection to specific database
        dbConn, err := ps.poolManager.GetConnectionForDatabase(
            ctx, conn, dbName, ps.serverSecret)
        if err != nil {
            continue
        }
        
        // Execute probe
        metrics, err := probe.Execute(ctx, dbConn)
        ps.poolManager.ReturnConnection(conn.ID, dbConn)
        
        if err == nil {
            for j := range metrics {
                metrics[j]["_database_name"] = dbName
            }
            allMetrics = append(allMetrics, metrics...)
        }
    }
    
    return allMetrics
}
```

## Metrics Storage

```go
func (ps *ProbeScheduler) storeMetrics(
    ctx context.Context,
    probe probes.MetricsProbe,
    connectionID int,
    timestamp time.Time,
    metrics []map[string]interface{}) {
    
    // Create context with timeout for storage
    timeout := time.Duration(ps.config.GetDatastorePoolMaxWaitSeconds()) * time.Second
    storeCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    
    // Get datastore connection
    datastoreConn, err := ps.datastore.GetConnectionWithContext(storeCtx)
    if err != nil {
        log.Printf("Error getting datastore connection: %v", err)
        return
    }
    defer ps.datastore.ReturnConnection(datastoreConn)
    
    // Store metrics
    err = probe.Store(storeCtx, datastoreConn, connectionID, timestamp, metrics)
    if err != nil {
        log.Printf("Error storing metrics: %v", err)
    }
}
```

## Shutdown Sequence

```go
func (ps *ProbeScheduler) Stop() {
    // 1. Cancel context (stops new probe executions)
    ps.cancel()
    
    // 2. Signal all goroutines to stop
    close(ps.shutdownChan)
    
    // 3. Wait for all probe goroutines to finish
    ps.wg.Wait()
    
    log.Println("Probe scheduler stopped")
}
```

## Concurrency Control

### Per-Probe Goroutines

- Each probe has one goroutine managing its schedule
- Total: 24 goroutines (one per probe)

### Per-Connection Goroutines

- When a probe executes, it spawns one goroutine per connection
- These are short-lived (duration of probe execution)
- If 10 monitored servers: up to 240 concurrent goroutines during probe
  execution

### Connection Pool Limits

- Datastore uses a global limit (e.g., 25 connections).
- Monitored servers use a per-server limit (e.g., 5 connections each).

### Semaphores

Each monitored server has a semaphore (buffered channel) that limits
concurrent probe executions:

```go
type MonitoredConnectionPoolManager struct {
    semaphores map[int]chan struct{}  // Per-connection semaphores
    maxConnections int                // Size of each semaphore
}

// Acquire slot (blocks if limit reached)
func (m *MonitoredConnectionPoolManager) acquireSlot(ctx context.Context, 
                                                      connectionID int) error {
    sem := m.getSemaphore(connectionID)
    select {
    case sem <- struct{}{}:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}

// Release slot
func (m *MonitoredConnectionPoolManager) releaseSlot(connectionID int) {
    sem := m.getSemaphore(connectionID)
    <-sem
}
```

## Timeout Handling

### Execution Timeout

Each probe execution has a timeout equal to
`monitored_pool_max_wait_seconds`:

```go
timeout := time.Duration(ps.config.GetMonitoredPoolMaxWaitSeconds()) * time.Second
execCtx, cancel := context.WithTimeout(ctx, timeout)
defer cancel()
```

If exceeded:
- Probe execution is cancelled
- Error is logged
- Probe will retry on next interval

### Storage Timeout

Metric storage has its own timeout equal to
`datastore_pool_max_wait_seconds`:

```go
timeout := time.Duration(ps.config.GetDatastorePoolMaxWaitSeconds()) * time.Second
storeCtx, cancel := context.WithTimeout(ctx, timeout)
defer cancel()
```

## Error Handling

### Connection Errors

- Logged with connection name and error
- Probe skipped for this connection
- Will retry on next interval

### Execution Errors

- Logged with probe name, connection, and error
- Metrics not collected
- Will retry on next interval

### Storage Errors

- Logged with probe name and error
- Metrics lost for this interval
- Will retry on next interval

### Timeout Errors

- Logged with timeout duration
- Execution cancelled
- Will retry on next interval

## Performance Characteristics

### Scheduling Overhead

- Minimal: each probe just sleeps on a timer
- 24 goroutines × minimal stack size ≈ 2-4 MB

### Execution Parallelism

- Probes execute independently
- Connections processed in parallel
- Limited by connection pool sizes

### Memory Usage

- Per probe execution: 100 KB - 10 MB (result set dependent)
- Peak during mass probe execution
- Released when metrics stored

## Monitoring the Scheduler

### Check Probe Timing

Watch logs for probe execution times:

```
2025/11/05 10:00:00 Probe pg_stat_activity on Server1 completed in 45.23ms
2025/11/05 10:00:00 Probe pg_stat_database on Server1 completed in 123.45ms
```

### Check for Timeouts

Look for timeout messages in logs:

```
2025/11/05 10:00:00 Probe pg_stat_activity timed out for Server1 (timeout: 60 seconds)
```

### Check Probe Status

Query most recent collection:

```sql
SELECT 
    c.name,
    MAX(pa.collected_at) AS last_activity_collection,
    MAX(pd.collected_at) AS last_database_collection
FROM connections c
LEFT JOIN metrics.pg_stat_activity pa ON c.id = pa.connection_id
LEFT JOIN metrics.pg_stat_database pd ON c.id = pd.connection_id
WHERE c.is_monitored = TRUE
GROUP BY c.name
ORDER BY c.name;
```

## Tuning the Scheduler

### Adjusting Collection Intervals

Balance between data freshness and system load:

```sql
-- Reduce frequency for low-priority probes
UPDATE probes
SET collection_interval_seconds = 900  -- 15 minutes
WHERE name = 'pg_stat_io';

-- Increase frequency for critical probes
UPDATE probes
SET collection_interval_seconds = 30
WHERE name = 'pg_stat_replication';
```

### Adjusting Timeouts

Increase if seeing timeout errors:

```ini
# In configuration file
monitored_pool_max_wait_seconds = 120
datastore_pool_max_wait_seconds = 90
```

### Adjusting Concurrency

Increase pool sizes for better parallelism:

```ini
monitored_pool_max_connections = 10
datastore_pool_max_connections = 50
```

## See Also

- [Probes](probes.md) - Probe system overview
- [Architecture](architecture.md) - Overall system design
- [Configuration](configuration.md) - Tuning scheduler performance
