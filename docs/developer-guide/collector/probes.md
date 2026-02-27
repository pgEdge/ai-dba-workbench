# Probes System

This document provides a detailed explanation of how
the Collector probe system works internally.

## What is a Probe?

A probe is a data collection unit that performs
the following tasks:

1. The probe queries a specific PostgreSQL system
   view or function.
2. The probe collects metrics at a configured
   interval.
3. The probe stores the results in a partitioned
   metrics table.
4. The probe manages data retention through automated
   cleanup.

The Collector includes 34 built-in probes covering
the most important PostgreSQL statistics views.

## Probe Types

Probes are categorized by their scope.

### Server-Scoped Probes

Server-scoped probes collect server-wide statistics
and execute once per monitored connection. Examples
include the following probes:

- `pg_stat_activity` monitors current database
  activity.
- `pg_stat_replication` monitors replication status,
  lag, and WAL receiver statistics.
- `pg_replication_slots` monitors replication slot
  usage and statistics.
- `pg_stat_recovery_prefetch` monitors recovery
  prefetch statistics.
- `pg_stat_subscription` monitors logical replication
  subscriptions and statistics.
- `pg_stat_connection_security` monitors SSL and
  GSSAPI connection security.
- `pg_stat_io` monitors I/O and SLRU cache
  statistics.
- `pg_stat_checkpointer` monitors checkpointer and
  background writer statistics.
- `pg_stat_wal` monitors WAL generation and archiver
  statistics.
- `pg_settings` monitors PostgreSQL configuration
  settings with change tracking.
- `pg_hba_file_rules` monitors `pg_hba.conf`
  authentication rules with change tracking.
- `pg_ident_file_mappings` monitors `pg_ident.conf`
  user mappings with change tracking.
- `pg_server_info` monitors server identification
  and configuration with change tracking.
- `pg_node_role` detects node roles for cluster
  topologies.
- `pg_database` monitors the database catalog with
  XID wraparound indicators.

### Database-Scoped Probes

Database-scoped probes collect per-database statistics
and execute once for each database on a monitored
server. Examples include the following probes:

- `pg_stat_database` monitors database-wide
  statistics.
- `pg_stat_database_conflicts` monitors recovery
  conflict statistics.
- `pg_stat_all_tables` monitors table access and
  I/O statistics.
- `pg_stat_all_indexes` monitors index usage and
  I/O statistics.
- `pg_statio_all_sequences` monitors sequence I/O
  statistics.
- `pg_stat_user_functions` monitors user function
  statistics.
- `pg_stat_statements` monitors query performance
  statistics.
- `pg_extension` monitors installed extensions with
  change tracking.

## Probe Lifecycle

This section describes the lifecycle of a probe from
initialization through cleanup.

### 1. Initialization

At startup, the Collector performs the following
steps:

1. The Collector loads probe configurations from
   the `probes` table.
2. The Collector creates a probe instance for each
   enabled probe.
3. The Collector logs initialized probes with their
   intervals and retention settings.

### 2. Scheduling

Each probe runs on an independent schedule:

1. The probe executes immediately on startup.
2. A timer triggers based on
   `collection_interval_seconds`.
3. The probe executes against all monitored
   connections.
4. The process repeats until shutdown.

### 3. Execution

The execution flow differs based on probe scope.

For server-scoped probes, the scheduler performs
these steps:

1. Get all monitored connections from the datastore.
2. For each connection in parallel:
   - Acquire a connection from the monitored pool.
   - Execute the SQL query.
   - Release the monitored connection.
   - Acquire a datastore connection.
   - Ensure a partition exists for the current week.
   - Store metrics using the COPY protocol.
   - Release the datastore connection.

For database-scoped probes, the scheduler performs
these steps:

1. Get all monitored connections from the datastore.
2. For each connection in parallel:
   - Acquire a connection from the monitored pool.
   - Query `pg_database` for the database list.
   - For each database, acquire a connection and
     execute the SQL query.
   - Release the monitored connection.
   - Acquire a datastore connection.
   - Ensure a partition exists.
   - Store all collected metrics using the COPY
     protocol.
   - Release the datastore connection.

### 4. Storage

The Collector stores metrics using the PostgreSQL
COPY protocol by following these steps:

1. Build the column list.
2. Build the values array from collected metrics.
3. Create a `CopyFrom` source.
4. Execute the COPY command.
5. Commit the transaction.

The COPY protocol is much faster than individual
INSERT statements.

### 5. Partition Management

Before storing metrics, the system manages partitions
by following these steps:

1. Calculate the current week start (Monday).
2. Check whether a partition exists for that week.
3. If the partition does not exist, create a
   partition with a range from Monday 00:00:00 to
   the next Monday 00:00:00.
4. Store metrics in the partition.

### 6. Cleanup

The garbage collector manages data retention:

1. The garbage collector runs daily, with the first
   run 5 minutes after startup.
2. For each probe, the collector calculates the
   cutoff as `NOW() - retention_days`, finds
   partitions entirely before the cutoff, and drops
   those partitions.
3. For change-tracked probes such as `pg_settings`,
   `pg_hba_file_rules`, and `pg_ident_file_mappings`,
   the collector identifies the most recent snapshot
   for each connection and protects those partitions
   from deletion.

## Probe Interface

All probes implement the `MetricsProbe` interface.
The following code shows the interface definition:

```go
type MetricsProbe interface {
    // GetName returns the probe name
    GetName() string

    // GetTableName returns the metrics table name
    GetTableName() string

    // GetQuery returns the SQL query to execute
    GetQuery() string

    // Execute runs the probe and returns metrics
    Execute(
        ctx context.Context,
        conn *pgxpool.Conn,
    ) ([]map[string]interface{}, error)

    // Store saves metrics to the datastore
    Store(
        ctx context.Context,
        conn *pgxpool.Conn,
        connectionID int,
        timestamp time.Time,
        metrics []map[string]interface{},
    ) error

    // EnsurePartition creates partition if needed
    EnsurePartition(
        ctx context.Context,
        conn *pgxpool.Conn,
        timestamp time.Time,
    ) error

    // GetConfig returns probe configuration
    GetConfig() *ProbeConfig

    // IsDatabaseScoped returns true for
    // per-database probes
    IsDatabaseScoped() bool
}
```

## Probe Implementation Example

The following code shows a simplified example of a
probe implementation:

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
    return false // Server-scoped
}

func (p *PgStatActivityProbe) GetQuery() string {
    return `
        SELECT
            datid, datname, pid, usename,
            application_name, client_addr,
            backend_start, state, query
        FROM pg_stat_activity
        WHERE pid <> pg_backend_pid()
    `
}

func (p *PgStatActivityProbe) Execute(
    ctx context.Context,
    conn *pgxpool.Conn,
) ([]map[string]interface{}, error) {
    rows, err := conn.Query(ctx, p.GetQuery())
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    return utils.ScanRowsToMaps(rows)
}

func (p *PgStatActivityProbe) Store(
    ctx context.Context,
    datastoreConn *pgxpool.Conn,
    connectionID int,
    timestamp time.Time,
    metrics []map[string]interface{},
) error {
    if err := p.EnsurePartition(
        ctx, datastoreConn, timestamp); err != nil {
        return err
    }

    columns := []string{
        "connection_id", "collected_at",
        "datid", "datname",
    }
    var values [][]interface{}
    for _, metric := range metrics {
        row := []interface{}{
            connectionID, timestamp,
            metric["datid"],
        }
        values = append(values, row)
    }

    return StoreMetricsWithCopy(
        ctx, datastoreConn,
        p.GetTableName(), columns, values)
}
```

## Error Handling

The probe system handles errors at several levels.

### Probe Execution Errors

If a probe fails to execute:

1. The system logs the error with context including
   the probe name, connection name, and error
   message.
2. The system does not store metrics for that
   interval.
3. Other probes continue without interruption.
4. The probe retries on the next scheduled interval.

### Connection Errors

If a connection cannot be acquired:

1. The system logs the error with timeout
   information.
2. The system skips probe execution for that
   connection.
3. The probe retries on the next interval.

### Storage Errors

If metrics cannot be stored:

1. The system logs the error.
2. Metrics are lost for that interval.
3. The probe continues on the next interval.

## Performance Considerations

This section covers performance factors to consider
when working with probes.

### Query Optimization

Probe queries should follow these guidelines:

- Use appropriate WHERE clauses to filter data.
- Avoid expensive operations such as sorts and
  aggregates when possible.
- Return only the columns that are needed.
- Use indexes when available.

### Collection Frequency

Balance data freshness against system load using
these guidelines:

- Fast-changing data such as replication lag
  should use 30-60 second intervals.
- Moderate data such as table statistics should
  use 300 second (5 minute) intervals.
- Slow-changing data such as archiver statistics
  should use 600+ second (10+ minute) intervals.

### Concurrent Execution

Probes execute in parallel with the following
constraints:

- Each probe has its own goroutine.
- Connection pools limit concurrent connections per
  server.
- The system prevents overwhelming monitored servers.

### Storage Efficiency

The COPY protocol is much faster than INSERT
because the protocol provides bulk loading, minimal
protocol overhead, and efficient server-side
processing.

## See Also

The following resources provide additional details.

- [Adding Probes](adding-probes.md) covers how to
  create custom probes.
- [Probe Reference](probe-reference.md) provides
  the complete probe list.
- [Scheduler](scheduler.md) explains how probes
  are scheduled.
- [Architecture](architecture.md) describes the
  overall system design.
