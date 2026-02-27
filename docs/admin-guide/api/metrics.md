# Metrics Tools

The MCP server provides tools for querying historical
metrics collected by the pgEdge AI DBA Workbench
collector. These tools access the datastore database,
which contains time-series metrics from all monitored
PostgreSQL servers.

## Database Architecture

The AI DBA Workbench uses a two-tier database
architecture:

- The datastore database contains metrics collected by
  the collector over time; the `list_probes`,
  `describe_probe`, and `query_metrics` tools query
  this database.
- The monitored databases are live PostgreSQL servers
  being monitored; the `query_database`,
  `get_schema_info`, and `execute_explain` tools
  access these databases.

## Available Tools

### list_probes

The `list_probes` tool lists all available metrics
probes in the datastore.

**Parameters**: None

**Returns**: A TSV table with the following columns:

- `name` contains the probe name for use with
  `describe_probe` and `query_metrics`.
- `description` contains a human-readable description.
- `row_count` contains the approximate number of
  metric rows collected.
- `scope` indicates "server" for server-wide metrics
  or "database" for per-database metrics.

In the following example, the tool lists all probes:

```json
{
    "tool": "list_probes",
    "arguments": {}
}
```

### describe_probe

The `describe_probe` tool returns detailed information
about a specific metrics probe including all available
columns and their data types.

**Parameters**:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `probe_name` | string | Yes | The name of the probe (from `list_probes`). |

**Returns**: A TSV table with the following columns:

- `column_name` contains the name of the column.
- `data_type` contains the PostgreSQL data type.
- `description` contains a human-readable description.
- `column_type` indicates "metric" for numeric values
  or "dimension" for identifiers.

In the following example, the tool describes the
`pg_stat_database` probe:

```json
{
    "tool": "describe_probe",
    "arguments": {
        "probe_name": "pg_stat_database"
    }
}
```

### query_metrics

The `query_metrics` tool queries collected metrics with
time-based aggregation into buckets.

**Parameters**:

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `probe_name` | string | Yes | - | The name of the probe. |
| `connection_id` | integer | Yes | - | The ID of the monitored connection. |
| `time_start` | string | No | "1h" | The start time (ISO 8601 or relative: "1h", "24h", "7d"). |
| `time_end` | string | No | "now" | The end time (ISO 8601 or "now"). |
| `buckets` | integer | No | 150 | The number of time buckets (1-500). |
| `metrics` | string | No | all | A comma-separated list of metric columns. |
| `database_name` | string | No | - | A filter by database name. |
| `schema_name` | string | No | - | A filter by schema name. |
| `table_name` | string | No | - | A filter by table name. |
| `aggregation` | string | No | "avg" | The aggregation method: avg, sum, min, max, last. |

**Returns**: A TSV table with the following columns:

- `bucket_time` contains the start time of each bucket.
- One column per requested metric contains the
  aggregated values.

In the following example, the tool queries database
statistics for the last 24 hours:

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
| `pg_stat_activity` | Current database connections and queries. |
| `pg_stat_replication` | Streaming replication and WAL receiver status. |
| `pg_stat_wal` | WAL activity and archiver statistics. |
| `pg_settings` | PostgreSQL configuration settings. |
| `pg_stat_checkpointer` | Checkpoint and background writer statistics. |
| `pg_stat_io` | I/O and SLRU cache statistics. |
| `pg_stat_connection_security` | SSL and GSSAPI connection security. |

### Database-Scoped Probes

| Probe | Description |
|-------|-------------|
| `pg_stat_database` | Per-database statistics. |
| `pg_stat_database_conflicts` | Replication conflicts. |
| `pg_stat_user_tables` | Per-table statistics. |
| `pg_stat_user_indexes` | Per-index statistics. |
| `pg_stat_statements` | Query execution statistics. |

### System Probes

| Probe | Description |
|-------|-------------|
| `pg_sys_cpu_info` | System CPU usage. |
| `pg_sys_memory_info` | System memory usage. |
| `pg_sys_disk_info` | Disk usage statistics. |
| `pg_sys_network_info` | Network I/O statistics. |
| `pg_sys_load_avg_info` | System load averages. |

## Use Cases

### Performance Analysis

In the following example, the tool identifies
performance trends over seven days:

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

In the following example, the tool analyzes slow
queries from `pg_stat_statements`:

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

In the following example, the tool tracks system
resource usage over six hours:

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

Follow these guidelines when querying metrics:

1. Start with `list_probes` to discover what metrics
   are collected before querying.
2. Use `describe_probe` to understand available columns
   before constructing queries.
3. Limit metrics by specifying only the columns you
   need to reduce response size.
4. Choose an appropriate bucket count; use 50 to 150
   buckets for an overview and fewer for quick checks.
5. Select time ranges carefully; start with shorter
   ranges (1h, 6h) and expand as needed.
6. Choose the right aggregation method:

   - `avg` is best for rates and averages over time.
   - `sum` is best for cumulative metrics like
     transaction counts.
   - `max` is best for peak values like connection
     counts.
   - `min` is best for minimum thresholds.
   - `last` is best for point-in-time values.

## Configuration

The datastore tools are enabled by default. The tools
can be disabled in the server configuration.

In the following example, the configuration disables
the datastore tools:

```yaml
builtins:
  tools:
    list_probes: false
    describe_probe: false
    query_metrics: false
```

The tools require the server to be configured with a
datastore connection. For configuration details, see
[Server Configuration](../../getting-started/configuration/server.md).

## Related Documentation

- [Server Information](server-info.md) describes the
  server details endpoint.
- [Probes](../probes.md) covers probe management and
  configuration.
