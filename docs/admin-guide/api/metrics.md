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

## Derived Metrics

Most PostgreSQL statistics columns are cumulative
counters that only ever rise, which makes the raw
values hard to read on a chart. The metrics query
endpoint of the REST API,
`GET /api/v1/metrics/query`, therefore accepts three
derived names in its `metrics` parameter in addition
to the columns the probe collects. The `query_metrics`
MCP tool accepts the probe's own columns only, and
rejects the derived names described here.

The following table describes the derived metrics:

| Name | Description |
|------|-------------|
| `<column>_per_sec` | The per-second rate of change of a counter column, computed from consecutive samples. |
| `<column>_delta` | The increase in a counter column within each time bucket, summed across the samples in the bucket. |
| `dead_tuple_ratio` | The percentage of tuples that are dead, from `n_live_tup` and `n_dead_tup`. |

A per-second rate discards a negative change between
two samples, because a counter falls only when the
statistics are reset rather than because work was
undone. The bucket value follows the `aggregation`
parameter, and `avg` gives the most representative
result.

A delta answers "how many events happened during this
bucket", which suits a bar chart of rare events such
as checkpoints. A bucket that contains no sample
reports zero when the window contains at least one
sample, and the `aggregation` parameter does not
apply. A connection with no samples in the window
returns no data points for either derived form,
rather than a series of zeros.

For a probe that stores one row per monitored entity
in each sample, such as one row per network interface
or per database, both forms compute the change of each
entity's counter separately and then add the changes
together. An entity that disappears between samples
does not produce a negative change, and an entity that
appears does not contribute its existing total.

The first sample in the window is compared with the
most recent sample before the window, provided that
sample is no older than the larger of three bucket
widths and 30 minutes before the window starts. Beyond
that, the first sample contributes nothing, so a long
gap in collection does not appear as a spike in the
first bucket.

The `dead_tuple_ratio` metric requires a probe that
collects both `n_live_tup` and `n_dead_tup`, such as
`pg_stat_all_tables`.

In the following example, the request returns the
commit and rollback rates for the last six hours:

```bash
curl -H "Authorization: Bearer $TOKEN" \
    "https://workbench.example.com/api/v1/metrics/query?probe_name=pg_stat_database&connection_id=1&time_range=6h&buckets=72&aggregation=avg&metrics=xact_commit_per_sec,xact_rollback_per_sec"
```

A request for a derived metric whose base column the
probe does not collect fails rather than returning
empty data. The endpoint answers with HTTP status 400
and an error message that names the metric and the
probe, and the dashboards display that message in the
affected chart panel rather than an empty chart.

## Top Query Client Attribution

The top queries endpoint of the REST API,
`GET /api/v1/metrics/top-queries`, names the client
last seen running each statement alongside the
`pg_stat_statements` counters. The collector records
`pg_stat_activity.query_id` with every activity
sample, and the server resolves the client by joining
that identifier to the samples taken in the hour
before the most recent `pg_stat_statements` snapshot.

The following table describes the client fields on
each row of the response:

| Field | Description |
|-------|-------------|
| `client_addr` | The address of the client most recently observed running the query, reported as `local` when that client connected over a Unix-domain socket, or null when no sample ever caught the query in flight. |
| `client_hostname` | The hostname of that client, or null when the monitored server resolved no hostname, which includes every server that runs with `log_hostname` off. |
| `client_observed_at` | The RFC 3339 time of the activity sample that observed the client, or null when no sample ever caught the query in flight. |

The `client_observed_at` field is the one to test for
a query that was never observed. The field is null
exactly when no activity sample caught the query in
flight, and whenever the field carries a time,
`client_addr` carries a value as well. A backend that
connected over a Unix-domain socket has no address in
`pg_stat_activity`, so the server reports the string
`local` for such a client, matching the convention the
connection groups endpoint already uses.

The server keys the attribution on the database and
the role as well as on the query identifier, because
`pg_stat_statements` records one row per combination
of the three. The client named for a query in one
database is therefore never a client that only ever
connected to another.

The attribution is best effort. The collector samples
`pg_stat_activity` at intervals rather than watching
every statement, so the endpoint credits a client only
to statements that were in flight when a sample was
taken, and the fields describe the client observed
most recently rather than the only client that ever
ran the query.

All three fields are null on PostgreSQL releases
before 14, where `pg_stat_activity` has no `query_id`
column, and on any server that runs with
`compute_query_id` off. Operators who want this
attribution should enable `compute_query_id` on each
monitored server.

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

7. Request a derived metric rather than a raw counter
   column when charting activity over time through the
   REST API; `_per_sec` suits a line chart and `_delta`
   suits a bar chart of rare events.

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
