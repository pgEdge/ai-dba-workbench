# Metrics Tools

The MCP server provides tools for querying historical metrics collected by the
pgEdge AI DBA Workbench collector. These tools access the **datastore**
database, which contains time-series metrics from all monitored PostgreSQL
servers.

## Database Architecture

The AI DBA Workbench uses a two-tier database architecture:

1. The datastore database contains metrics collected by the collector over
   time. The `list_probes`, `describe_probe`, and `query_metrics` tools query
   this database.

2. The monitored databases are live PostgreSQL servers being monitored. The
   `query_database`, `get_schema_info`, and `execute_explain` tools access
   these databases.

## Available Tools

### list_probes

Lists all available metrics probes in the datastore.

**Parameters**: None

**Returns**: TSV table with:

- `name`: Probe name (use with `describe_probe` and `query_metrics`)
- `description`: Human-readable description
- `row_count`: Approximate number of metric rows collected
- `scope`: "server" for server-wide metrics, "database" for per-database
  metrics

**Example**:

```json
{
    "tool": "list_probes",
    "arguments": {}
}
```

### describe_probe

Gets detailed information about a specific metrics probe including all
available columns and their data types.

**Parameters**:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `probe_name` | string | Yes | Name of the probe (from `list_probes`) |

**Returns**: TSV table with:

- `column_name`: Name of the column
- `data_type`: PostgreSQL data type
- `description`: Human-readable description
- `column_type`: "metric" for numeric values, "dimension" for identifiers

**Example**:

```json
{
    "tool": "describe_probe",
    "arguments": {
        "probe_name": "pg_stat_database"
    }
}
```

### query_metrics

Queries collected metrics with time-based aggregation into buckets.

**Parameters**:

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `probe_name` | string | Yes | - | Name of the probe |
| `connection_id` | integer | Yes | - | ID of the monitored connection |
| `time_start` | string | No | "1h" | Start time (ISO 8601 or relative: "1h", "24h", "7d") |
| `time_end` | string | No | "now" | End time (ISO 8601 or "now") |
| `buckets` | integer | No | 150 | Number of time buckets (1-500) |
| `metrics` | string | No | all | Comma-separated list of metric columns |
| `database_name` | string | No | - | Filter by database name |
| `schema_name` | string | No | - | Filter by schema name |
| `table_name` | string | No | - | Filter by table name |
| `aggregation` | string | No | "avg" | Aggregation: avg, sum, min, max, last |

**Returns**: TSV table with:

- `bucket_time`: Start time of each bucket
- One column per requested metric with aggregated values

**Example**:

```json
{
    "tool": "query_metrics",
    "arguments": {
        "probe_name": "pg_stat_database",
        "connection_id": 1,
        "time_start": "24h",
        "metrics": "numbackends,xact_commit,xact_rollback",
        "buckets": 100
    }
}
```

## Common Probes

### Server-Wide Probes

| Probe | Description |
|-------|-------------|
| `pg_stat_activity` | Current database connections and queries |
| `pg_stat_replication` | Streaming replication and WAL receiver status |
| `pg_stat_wal` | WAL activity and archiver statistics |
| `pg_settings` | PostgreSQL configuration settings |
| `pg_stat_checkpointer` | Checkpoint and background writer statistics |
| `pg_stat_io` | I/O and SLRU cache statistics |
| `pg_stat_connection_security` | SSL and GSSAPI connection security |

### Database-Scoped Probes

| Probe | Description |
|-------|-------------|
| `pg_stat_database` | Per-database statistics |
| `pg_stat_database_conflicts` | Replication conflicts |
| `pg_stat_user_tables` | Per-table statistics |
| `pg_stat_user_indexes` | Per-index statistics |
| `pg_stat_statements` | Query execution statistics |

### System Probes

| Probe | Description |
|-------|-------------|
| `pg_sys_cpu_info` | System CPU usage |
| `pg_sys_memory_info` | System memory usage |
| `pg_sys_disk_info` | Disk usage statistics |
| `pg_sys_network_info` | Network I/O statistics |
| `pg_sys_load_avg_info` | System load averages |

## Use Cases

### Performance Analysis

Identify performance trends over time:

```json
{
    "tool": "query_metrics",
    "arguments": {
        "probe_name": "pg_stat_database",
        "connection_id": 1,
        "time_start": "7d",
        "metrics": "xact_commit,tup_returned,tup_fetched",
        "aggregation": "sum",
        "buckets": 168
    }
}
```

### Query Statistics

Analyze slow queries from pg_stat_statements:

```json
{
    "tool": "query_metrics",
    "arguments": {
        "probe_name": "pg_stat_statements",
        "connection_id": 1,
        "time_start": "24h",
        "metrics": "total_exec_time,calls,mean_exec_time",
        "buckets": 48
    }
}
```

### System Resource Monitoring

Track system resource usage:

```json
{
    "tool": "query_metrics",
    "arguments": {
        "probe_name": "pg_sys_cpu_info",
        "connection_id": 1,
        "time_start": "6h",
        "buckets": 72
    }
}
```

## Best Practices

1. Start with `list_probes` to discover what metrics are being collected
   before querying.

2. Use `describe_probe` to understand available columns before constructing
   queries.

3. Limit metrics by specifying only the metrics you need to reduce response
   size.

4. Choose an appropriate bucket count; use 50-150 buckets for overview, fewer
   for quick checks.

5. Select time ranges carefully; start with shorter ranges (1h, 6h) and expand
   as needed.

6. Choose the right aggregation:

   - `avg`: Best for rates and averages over time
   - `sum`: Best for cumulative metrics like transaction counts
   - `max`: Best for peak values like connection counts
   - `min`: Best for minimum thresholds
   - `last`: Best for point-in-time values

## Configuration

The datastore tools are enabled by default. They can be disabled in the server
configuration:

```yaml
builtins:
  tools:
    list_probes: false
    describe_probe: false
    query_metrics: false
```

The tools require the server to be configured with a datastore connection. See
[Configuration](configuration.md) for details.
