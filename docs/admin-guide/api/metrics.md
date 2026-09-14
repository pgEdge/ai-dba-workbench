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
`GET /api/v1/metrics/query`, therefore accepts five
derived names in its `metrics` parameter in addition
to the columns the probe collects. The `query_metrics`
MCP tool accepts the probe's own columns only, and
rejects the derived names described here.

The following table describes the derived metrics:

| Name | Unit | Description |
|------|------|-------------|
| `<column>_per_sec` | `/s` | The per-second rate of change of a cumulative counter column, computed from consecutive samples. |
| `<column>_delta` | `ms` or empty | The increase in a counter or time counter column within each time bucket, summed across the samples in the bucket. |
| `<column>_pct` | `%` | The share of wall-clock time that a cumulative millisecond column advanced by, such as `blk_read_time_pct` on `pg_stat_database`. |
| `<column>_sessions` | `sessions` | The average number of sessions in a state over the interval, from `session_time`, `active_time` or `idle_in_transaction_time` on `pg_stat_database`. |
| `dead_tuple_ratio` | `%` | The percentage of tuples that are dead, from `n_live_tup` and `n_dead_tup`. |

The `dead_tuple_ratio` metric requires a probe that
collects both `n_live_tup` and `n_dead_tup`, such as
`pg_stat_all_tables`.

### Column Kinds

Each derived form applies only to columns of a
particular kind, and the server keeps a registry of
the kind of every counter-like column in the probes it
collects. The rules are:

- `_per_sec` applies only to a cumulative event or
  byte counter, such as `xact_commit` or `wal_bytes`.
- `_delta` applies to a cumulative counter, to a
  cumulative time counter, which stores elapsed
  milliseconds as `blk_read_time` does, and to the
  session time columns of `pg_stat_database`; the unit
  is `ms` for either time kind and empty for a counter.
- `_pct` applies only to a cumulative time counter.
- `_sessions` applies only to the `session_time`,
  `active_time` and `idle_in_transaction_time` columns
  of `pg_stat_database`.

A column that the registry does not list is a gauge.
A gauge such as `n_dead_tup`, a lifetime watermark
such as `mean_exec_time` and a ratio such as a CPU
percentage cannot be differenced meaningfully, so a
request for `_per_sec` on one of them fails with HTTP
status 400 rather than returning a misleading series.
The error message names the metric, the probe, the
base column and the kind the registry records for it:

```text
metric "n_dead_tup_per_sec" not supported for probe "pg_stat_all_tables": "n_dead_tup" is a gauge, not a cumulative counter
```

A `_per_sec` request on a time counter is refused in
the same way, because a millisecond total divided by
seconds is a dimensionless number that only looks like
a rate; the message adds a hint naming the `_pct` or
`_sessions` form to request instead.

A `_pct` value can exceed 100 on a probe that stores
one row per monitored entity in each sample, because
the shares of all entities are added together: five
databases that each spent half the interval reading
blocks give a `blk_read_time_pct` of 250.

### Response Shape

The endpoint returns a JSON array with one object per
requested metric. Each object carries the requested
`name`, the base `metric` column, a `unit` and a
`data` array with one point per time bucket; a point
holds the bucket `time` and a `value`, which is either
a number or `null`. The `unit` is `/s`, `ms`, `%` or
`sessions` for a derived metric, and empty for a raw
column or a counter delta.

In the following example, the response reports one
rate series with a gap in the second bucket:

```json
[
    {
        "name": "xact_commit_per_sec",
        "metric": "xact_commit",
        "unit": "/s",
        "data": [
            {"time": "2026-09-14T10:00:00Z", "value": 42.5},
            {"time": "2026-09-14T10:05:00Z", "value": null},
            {"time": "2026-09-14T10:10:00Z", "value": 40.1}
        ]
    }
]
```

Every series in a response contains every bucket, so
all series share the same length and bucket times. A
connection with no samples in the window returns a
series of `null` values rather than an empty array.

### Per-Entity Differencing

For a probe that stores one row per monitored entity
in each sample, such as one row per network interface
or per database, every derived form computes the
change of each entity's counter separately and then
adds the changes together. An entity that disappears
between samples does not produce a negative change,
and an entity that appears does not contribute its
existing total.

The `pg_sys_network_info` probe leaves the loopback
interfaces `lo` and `lo0` out of every query, raw or
derived, because traffic on them is not network
traffic; the exclusion also applies to the
`query_metrics` MCP tool.

### Statistics Resets

A counter falls only when the statistics are reset
rather than because work was undone, so a negative
change between two samples is discarded rather than
charted. Where a probe stores the `stats_reset`
timestamp of the view it reads, the server also
compares the marker of consecutive samples, so a reset
is detected even when the counter has already climbed
past its previous value. The following probes carry a
marker:

- `pg_stat_database`, `pg_stat_io`,
  `pg_stat_recovery_prefetch`, `pg_replication_slots`
  and `pg_stat_subscription` store `stats_reset`.
- `pg_stat_wal` stores `stats_reset`, and
  `archiver_stats_reset` for `archived_count` and
  `failed_count`.
- `pg_stat_checkpointer` stores `stats_reset`, and
  `bgwriter_stats_reset` for `buffers_clean`,
  `maxwritten_clean` and `buffers_alloc`.
- `pg_stat_statements` stores `stats_reset` from
  `pg_stat_statements_info`, a view that the
  extension provides from version 1.9, shipped with
  PostgreSQL 14; the collector records `null` where
  the view is absent, and the column is added by
  collector schema migration 10.

An interval whose marker changed reports `null` for
`_per_sec`, `_pct` and `_sessions`, and contributes
zero to `_delta`. A probe without a marker relies on
the negative-change rule alone.

### Collection Gaps

Each probe runs on a collection interval, and the
server resolves the interval for a request from the
collector's `probe_configs` table: the server-scope
row for the connection, then the global row, then the
collector's default of 300 seconds. When a request
spans several connections, the largest interval
applies, because one bucket width serves every series
in the response.

An interval between two consecutive samples longer
than three times the collection interval is a gap in
collection rather than a measurement, and it reports
`null` for every derived form, `_delta` included. A
bucket whose only samples fall in such a gap is `null`
rather than zero, whilst a bucket with no samples at
all still reports zero for `_delta` when the window
contains at least one sample.

The first sample in the window is compared with the
most recent sample before the window, provided that
sample is no older than the larger of three bucket
widths and 30 minutes before the window starts. The
gap rule still applies to the borrowed sample, so a
long outage before the window does not appear as a
spike in the first bucket.

### Buckets and Missing Values

The requested `buckets` count is clamped so that no
bucket is narrower than the collection interval; a
narrower bucket cannot hold a sample of its own and
would only alternate between zero and repeated values.
A one-hour window on a probe collected every 300
seconds therefore yields twelve buckets however many
were requested; the series holds 13 points, because
the last point marks the end of the window.

A bucket with no sample is filled according to the
kind of metric:

- a raw column and `dead_tuple_ratio` repeat the last
  observed value for at most three collection
  intervals, after which the bucket is `null`.
- `_per_sec`, `_delta`, `_pct` and `_sessions` never
  repeat a value; a bucket without a valid interval is
  `null`, or zero for `_delta` as described above.

The bucket value of a `_per_sec`, `_pct` or
`_sessions` metric follows the `aggregation`
parameter, and `avg` gives the most representative
result. A `_delta` answers "how many events happened
during this bucket", which suits a bar chart of rare
events such as checkpoints, and the `aggregation`
parameter does not apply to it.

### Requesting Derived Metrics

In the following example, the request returns the
commit and rollback rates for the last six hours:

```bash
curl -H "Authorization: Bearer $TOKEN" \
    "https://workbench.example.com/api/v1/metrics/query?probe_name=pg_stat_database&connection_id=1&time_range=6h&buckets=72&aggregation=avg&metrics=xact_commit_per_sec,xact_rollback_per_sec"
```

A request for a derived metric whose base column the
probe does not collect, or whose kind does not suit
the derived form, fails rather than returning empty
data. The endpoint answers with HTTP status 400 and an
error message that names the metric and the probe, and
the dashboards display that message in the affected
chart panel rather than an empty chart.

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
