# Probes System

This document provides a detailed explanation of how the Collector's probe
system works.

## What is a Probe?

A probe is a data collection unit that:

1. Queries a specific PostgreSQL system view or function
2. Collects metrics at a configured interval
3. Stores the results in a partitioned metrics table
4. Manages data retention through automated cleanup

The Collector includes 24 built-in probes covering the most important
PostgreSQL statistics views.

## Probe Types

Probes are categorized by their scope:

### Server-Scoped Probes

These probes collect server-wide statistics and execute once per monitored
connection:

- `pg_stat_activity` - Current database activity
- `pg_stat_replication` - Replication status and lag
- `pg_stat_replication_slots` - Replication slot usage
- `pg_stat_wal_receiver` - WAL receiver status
- `pg_stat_recovery_prefetch` - Recovery prefetch statistics
- `pg_stat_subscription` - Logical replication subscriptions
- `pg_stat_subscription_stats` - Subscription statistics
- `pg_stat_ssl` - SSL connection information
- `pg_stat_gssapi` - GSSAPI connection information
- `pg_stat_archiver` - WAL archiver statistics
- `pg_stat_io` - I/O statistics
- `pg_stat_bgwriter` - Background writer statistics
- `pg_stat_checkpointer` - Checkpointer statistics
- `pg_stat_wal` - WAL generation statistics
- `pg_stat_slru` - SLRU (Simple LRU) cache statistics

### Database-Scoped Probes

These probes collect per-database statistics and execute once for each
database on a monitored server:

- `pg_stat_database` - Database-wide statistics
- `pg_stat_database_conflicts` - Recovery conflict statistics
- `pg_stat_all_tables` - Table access statistics
- `pg_stat_all_indexes` - Index usage statistics
- `pg_statio_all_tables` - Table I/O statistics
- `pg_statio_all_indexes` - Index I/O statistics
- `pg_statio_all_sequences` - Sequence I/O statistics
- `pg_stat_user_functions` - User function statistics
- `pg_stat_statements` - Query performance statistics (requires extension)

## Probe Lifecycle

### 1. Initialization

At startup, the Collector:

1. Loads probe configurations from the `probes` table
2. Creates a probe instance for each enabled probe
3. Logs initialized probes with their intervals and retention settings

### 2. Scheduling

Each probe runs on an independent schedule:

1. Probe executes immediately on startup
2. Timer triggers based on `collection_interval_seconds`
3. Probe executes against all monitored connections
4. Process repeats until shutdown

### 3. Execution

For each probe execution:

**Server-Scoped Probes:**

1. Get all monitored connections from datastore
2. For each connection in parallel:
   - Acquire connection from monitored pool
   - Execute SQL query
   - Release monitored connection
   - Acquire datastore connection
   - Ensure partition exists for current week
   - Store metrics using COPY protocol
   - Release datastore connection

**Database-Scoped Probes:**

1. Get all monitored connections from datastore
2. For each connection in parallel:
   - Acquire connection from monitored pool
   - Query `pg_database` for database list
   - For each database:
     - Acquire connection to that database
     - Execute SQL query
     - Collect metrics
   - Release monitored connection
   - Acquire datastore connection
   - Ensure partition exists
   - Store all collected metrics using COPY protocol
   - Release datastore connection

### 4. Storage

Metrics are stored using PostgreSQL's COPY protocol:

1. Build column list
2. Build values array from collected metrics
3. Create a CopyFrom source
4. Execute COPY command
5. Commit transaction

This is much faster than individual INSERT statements.

### 5. Partition Management

Before storing metrics:

1. Calculate current week start (Monday)
2. Check if partition exists for that week
3. If not, create partition with range:
   - Start: Monday 00:00:00
   - End: Next Monday 00:00:00
4. Store metrics in the partition

### 6. Cleanup

The garbage collector:

1. Runs daily (first run 5 minutes after startup)
2. For each probe:
   - Calculate cutoff: NOW() - retention_days
   - Find partitions entirely before cutoff
   - DROP those partitions
   - Free disk space

## Probe Configuration

Probes are configured in the `probes` table:

```sql
SELECT name, collection_interval_seconds, retention_days, is_enabled
FROM probes
ORDER BY name;
```

### Default Intervals

Different probe types have different default collection intervals based on
how frequently their data changes:

- **Fast**: 30-60 seconds (replication, activity)
- **Normal**: 300 seconds / 5 minutes (most probes)
- **Slow**: 600 seconds / 10 minutes (archiver, bgwriter, checkpointer)
- **Very Slow**: 900 seconds / 15 minutes (I/O statistics)

### Adjusting Collection Interval

```sql
UPDATE probes
SET collection_interval_seconds = 60
WHERE name = 'pg_stat_activity';
```

Changes take effect on the next collector restart.

### Adjusting Retention

```sql
UPDATE probes
SET retention_days = 30
WHERE name = 'pg_stat_activity';
```

Changes take effect on the next garbage collection run (within 24 hours).

### Disabling a Probe

```sql
UPDATE probes
SET is_enabled = FALSE
WHERE name = 'pg_stat_statements';
```

The probe will stop executing on the next collector restart.

## Probe Interface

All probes implement the `MetricsProbe` interface:

```go
type MetricsProbe interface {
    // GetName returns the probe name
    GetName() string
    
    // GetTableName returns the metrics table name
    GetTableName() string
    
    // GetQuery returns the SQL query to execute
    GetQuery() string
    
    // Execute runs the probe and returns metrics
    Execute(ctx context.Context, conn *pgxpool.Conn) ([]map[string]interface{}, error)
    
    // Store saves metrics to the datastore
    Store(ctx context.Context, conn *pgxpool.Conn, connectionID int, 
          timestamp time.Time, metrics []map[string]interface{}) error
    
    // EnsurePartition creates partition if needed
    EnsurePartition(ctx context.Context, conn *pgxpool.Conn, 
                    timestamp time.Time) error
    
    // GetConfig returns probe configuration
    GetConfig() *ProbeConfig
    
    // IsDatabaseScoped returns true for per-database probes
    IsDatabaseScoped() bool
}
```

## Probe Implementation Example

Here's a simplified example of a probe implementation:

```go
type PgStatActivityProbe struct {
    BaseMetricsProbe
}

func (p *PgStatActivityProbe) GetName() string {
    return "pg_stat_activity"
}

func (p *PgStatActivityProbe) GetTableName() string {
    return "pg_stat_activity"
}

func (p *PgStatActivityProbe) IsDatabaseScoped() bool {
    return false  // Server-scoped
}

func (p *PgStatActivityProbe) GetQuery() string {
    return `
        SELECT
            datid, datname, pid, usename, application_name,
            client_addr, backend_start, state, query
        FROM pg_stat_activity
        WHERE pid <> pg_backend_pid()
    `
}

func (p *PgStatActivityProbe) Execute(ctx context.Context, 
                                      conn *pgxpool.Conn) ([]map[string]interface{}, error) {
    rows, err := conn.Query(ctx, p.GetQuery())
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    return utils.ScanRowsToMaps(rows)
}

func (p *PgStatActivityProbe) Store(ctx context.Context, 
                                    datastoreConn *pgxpool.Conn,
                                    connectionID int, timestamp time.Time,
                                    metrics []map[string]interface{}) error {
    // Ensure partition exists
    if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
        return err
    }
    
    // Build columns and values
    columns := []string{"connection_id", "collected_at", "datid", "datname", ...}
    var values [][]interface{}
    for _, metric := range metrics {
        row := []interface{}{connectionID, timestamp, metric["datid"], ...}
        values = append(values, row)
    }
    
    // Store using COPY
    return StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), 
                                columns, values)
}
```

## Error Handling

### Probe Execution Errors

If a probe fails to execute:

1. Error is logged with context (probe name, connection name, error message)
2. Metrics are not stored for this interval
3. Other probes continue unaffected
4. Probe will retry on the next scheduled interval

### Connection Errors

If a connection cannot be acquired:

1. Error is logged with timeout information
2. Probe execution is skipped
3. Will retry on next interval

### Storage Errors

If metrics cannot be stored:

1. Error is logged
2. Metrics are lost for this interval
3. Probe continues on next interval

## Performance Considerations

### Query Optimization

Probe queries should:

- Use appropriate WHERE clauses to filter data
- Avoid expensive operations (sorts, aggregates) when possible
- Return only needed columns
- Use indexes when available

### Collection Frequency

Balance between:

- **Data freshness**: More frequent = more current data
- **System load**: More frequent = more queries
- **Storage size**: More frequent = more data points

Guidelines:

- Fast-changing data (replication lag): 30-60 seconds
- Moderate data (table stats): 300 seconds (5 minutes)
- Slow-changing data (archiver): 600+ seconds (10+ minutes)

### Concurrent Execution

Probes execute in parallel:

- Each probe has its own goroutine
- Connection pools limit concurrent connections per server
- Prevents overwhelming monitored servers

### Storage Efficiency

The COPY protocol is much faster than INSERT:

- Bulk loading of many rows at once
- Minimal protocol overhead
- Efficient server-side processing

## Monitoring Probes

### Check Probe Status

View configured probes:

```sql
SELECT name, collection_interval_seconds, retention_days, is_enabled
FROM probes
WHERE is_enabled = TRUE
ORDER BY name;
```

### Check Last Collection

See most recent data for a probe:

```sql
SELECT connection_id, MAX(collected_at) AS last_collected
FROM metrics.pg_stat_activity
GROUP BY connection_id
ORDER BY last_collected DESC;
```

### Check Partition Count

Count partitions for a probe:

```sql
SELECT COUNT(*) AS partition_count
FROM pg_tables
WHERE schemaname = 'metrics'
  AND tablename LIKE 'pg_stat_activity_%';
```

### Check Data Volume

See storage used by probe:

```sql
SELECT
    pg_size_pretty(SUM(pg_total_relation_size(schemaname || '.' || tablename))) AS total_size
FROM pg_tables
WHERE schemaname = 'metrics'
  AND tablename LIKE 'pg_stat_activity_%';
```

## Troubleshooting

### Probe Not Collecting Data

1. Check if probe is enabled:
   ```sql
   SELECT is_enabled FROM probes WHERE name = 'probe_name';
   ```

2. Check collector logs for errors

3. Verify monitored connection is accessible

4. Check connection has `is_monitored = TRUE`

### Data Not Appearing

1. Check if partition exists:
   ```sql
   SELECT tablename FROM pg_tables
   WHERE schemaname = 'metrics'
     AND tablename LIKE 'probe_name_%'
   ORDER BY tablename DESC LIMIT 5;
   ```

2. Check for recent data:
   ```sql
   SELECT MAX(collected_at) FROM metrics.probe_name;
   ```

3. Review collector logs for storage errors

### High Memory Usage

1. Check probe result set sizes
2. Reduce collection frequency for high-volume probes
3. Consider filtering probe queries
4. Increase monitored pool size to reduce queueing

### High Storage Usage

1. Reduce retention days for high-volume probes
2. Manually drop old partitions
3. Consider sampling strategies for high-frequency data

## See Also

- [Adding Probes](adding-probes.md) - Create custom probes
- [Probe Reference](probe-reference.md) - Complete probe list
- [Scheduler](scheduler.md) - How probes are scheduled
- [Architecture](architecture.md) - Overall system design
