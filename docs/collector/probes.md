# Probes System

This document provides a detailed explanation of how the Collector's probe
system works.

## What is a Probe?

A probe is a data collection unit that:

1. Queries a specific PostgreSQL system view or function
2. Collects metrics at a configured interval
3. Stores the results in a partitioned metrics table
4. Manages data retention through automated cleanup

The Collector includes 34 built-in probes covering the most important
PostgreSQL statistics views.

## Probe Types

Probes are categorized by their scope:

### Server-Scoped Probes

These probes collect server-wide statistics and execute once per monitored
connection:

- `pg_stat_activity` - Current database activity
- `pg_stat_replication` - Replication status, lag, and WAL receiver stats
- `pg_replication_slots` - Replication slot usage and statistics
- `pg_stat_recovery_prefetch` - Recovery prefetch statistics
- `pg_stat_subscription` - Logical replication subscriptions and statistics
- `pg_stat_connection_security` - SSL and GSSAPI connection security
- `pg_stat_io` - I/O and SLRU cache statistics
- `pg_stat_checkpointer` - Checkpointer and background writer statistics
- `pg_stat_wal` - WAL generation and archiver statistics
- `pg_settings` - PostgreSQL configuration settings (change-tracked)
- `pg_hba_file_rules` - pg_hba.conf authentication rules (change-tracked)
- `pg_ident_file_mappings` - pg_ident.conf user mappings (change-tracked)
- `pg_server_info` - Server identification and configuration (change-tracked)
- `pg_node_role` - Node role detection for cluster topologies
- `pg_database` - Database catalog with XID wraparound indicators

### Database-Scoped Probes

These probes collect per-database statistics and execute once for each
database on a monitored server:

- `pg_stat_database` - Database-wide statistics
- `pg_stat_database_conflicts` - Recovery conflict statistics
- `pg_stat_all_tables` - Table access and I/O statistics
- `pg_stat_all_indexes` - Index usage and I/O statistics
- `pg_statio_all_sequences` - Sequence I/O statistics
- `pg_stat_user_functions` - User function statistics
- `pg_stat_statements` - Query performance statistics (requires extension)
- `pg_extension` - Installed extensions (change-tracked)

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
3. Special handling for change-tracked probes (`pg_settings`,
   `pg_hba_file_rules`, `pg_ident_file_mappings`):
   - Identifies the most recent snapshot for each monitored connection
   - Protects those partitions from deletion regardless of age
   - Ensures at least one snapshot is always retained per server

## Probe Configuration

Probes are configured in the `probe_configs` table, which supports both global
defaults and per-server overrides:

```sql
-- View all probe configurations
SELECT name, connection_id, collection_interval_seconds, retention_days,
       is_enabled
FROM probe_configs
ORDER BY name, COALESCE(connection_id, 0);

-- View only global defaults
SELECT name, collection_interval_seconds, retention_days, is_enabled
FROM probe_configs
WHERE connection_id IS NULL
ORDER BY name;

-- View configurations for a specific connection
SELECT name, collection_interval_seconds, retention_days, is_enabled
FROM probe_configs
WHERE connection_id = 1
ORDER BY name;
```

### Default Intervals

Different probe types have different default collection intervals based on
how frequently their data changes:

- **Fast**: 30-60 seconds (replication, activity)
- **Normal**: 300 seconds / 5 minutes (most probes)
- **Slow**: 600 seconds / 10 minutes (checkpointer, WAL)
- **Very Slow**: 900 seconds / 15 minutes (I/O statistics)
- **Change-Tracked**: 3600 seconds / 1 hour (pg_settings - only stored when
    changes detected)

### Per-Server Configuration

The Collector supports customizing probe settings for individual monitored
connections. The configuration hierarchy is:

1. **Connection-Specific**: Override for a specific connection
2. **Global Default**: Default settings when `connection_id IS NULL`
3. **Hardcoded Default**: Built-in values if no database config exists

When a new monitored connection is added, the Collector automatically creates
per-server probe configurations based on the global defaults.

### Automatic Configuration Reload

**Important**: The Collector automatically reloads probe configurations from
the database every 5 minutes. Changes to `collection_interval_seconds`,
`retention_days`, or `is_enabled` take effect within 5 minutes without
requiring a restart.

### Adjusting Collection Interval

```sql
-- Update global default for all connections
UPDATE probe_configs
SET collection_interval_seconds = 60
WHERE name = 'pg_stat_activity'
  AND connection_id IS NULL;

-- Override for a specific connection
UPDATE probe_configs
SET collection_interval_seconds = 30
WHERE name = 'pg_stat_activity'
  AND connection_id = 1;
```

Changes take effect within 5 minutes (automatic config reload).

### Adjusting Retention

```sql
-- Update global default retention
UPDATE probe_configs
SET retention_days = 30
WHERE name = 'pg_stat_activity'
  AND connection_id IS NULL;

-- Override retention for a specific connection
UPDATE probe_configs
SET retention_days = 60
WHERE name = 'pg_stat_activity'
  AND connection_id = 3;
```

Retention changes take effect on the next garbage collection run (within 24
hours).

### Disabling a Probe

```sql
-- Disable globally
UPDATE probe_configs
SET is_enabled = FALSE
WHERE name = 'pg_stat_statements'
  AND connection_id IS NULL;

-- Disable for a specific connection only
UPDATE probe_configs
SET is_enabled = FALSE
WHERE name = 'pg_stat_statements'
  AND connection_id = 2;
```

Changes take effect within 5 minutes (automatic config reload).

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
-- View all enabled probe configurations
SELECT name, connection_id, collection_interval_seconds, retention_days,
       is_enabled
FROM probe_configs
WHERE is_enabled = TRUE
ORDER BY name, COALESCE(connection_id, 0);

-- View enabled global defaults only
SELECT name, collection_interval_seconds, retention_days
FROM probe_configs
WHERE is_enabled = TRUE
  AND connection_id IS NULL
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
   -- Check global default
   SELECT is_enabled FROM probe_configs
   WHERE name = 'probe_name' AND connection_id IS NULL;

   -- Check for specific connection
   SELECT is_enabled FROM probe_configs
   WHERE name = 'probe_name' AND connection_id = 1;
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
