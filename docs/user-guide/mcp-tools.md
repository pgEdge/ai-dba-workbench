# MCP Tools and Resources

The MCP server implements the Model Context Protocol,
providing AI assistants with standardized access to
PostgreSQL systems. Compatible MCP clients can use these
tools and resources to interact with monitored databases.

## Connecting an MCP Client

The MCP server exposes a JSON-RPC 2.0 endpoint at
`/mcp/v1` over HTTP or HTTPS. Configure your MCP client
with the server address and a valid API token to begin
using the available tools.

## Monitored Database Tools

These tools operate on monitored database connections.
Each tool accepts optional `connection_id` and
`database_name` parameters to target a specific
database; the tool uses the currently selected connection
when these parameters are omitted.

| Tool | Description |
|------|-------------|
| `query_database` | Executes SQL queries against a monitored database. |
| `get_schema_info` | Retrieves table and column information from a database. |
| `execute_explain` | Runs EXPLAIN or EXPLAIN ANALYZE on a query. |
| `similarity_search` | Performs vector similarity search using pgvector. |
| `count_rows` | Counts rows in a specified table. |
| `test_query` | Validates SQL query correctness without executing the query. |

## Datastore Tools

These tools query the metrics datastore for historical
data collected by the collector service.

| Tool | Description |
|------|-------------|
| `list_probes` | Lists available metrics probes in the datastore. |
| `describe_probe` | Retrieves column details for a specific metrics probe. |
| `query_metrics` | Queries historical metrics with time-based aggregation. |
| `list_connections` | Lists available monitored database connections. |
| `describe_datastore` | Lists the sources `query_datastore` can read, or describes one source's columns. |
| `query_datastore` | Runs a structured query against one datastore source, limited to the connections the caller can access. |

## Querying the Datastore

The `query_datastore` tool reads the datastore, which is the workbench's own
PostgreSQL database of collected metrics, alerts and configuration rather than
a monitored database. The tool does not accept SQL; the server builds the
query from structured parameters, so a caller can only read data about the
connections that caller can access.

### Datastore Sources

Each query names one source; the `describe_datastore` tool lists every source
and describes the columns of each. The datastore offers two kinds of source:

- A metric source exists for each collector probe, such as
  `pg_stat_database` or `pg_stat_activity`, and every metric source carries
  `connection_id` and `collected_at` columns.
- A configuration source exposes a configuration table, limited to the
  columns that are safe to read.

The following table describes the configuration sources and how the server
limits each one to the caller's connections:

| Source | Contents | Rows returned |
|--------|----------|---------------|
| `connections` | Registered monitored connections, without stored passwords or certificate paths. | Connections the caller can access. |
| `alerts` | Threshold, anomaly, connection and system alerts. | Alerts for accessible connections, plus system alerts for callers who may see them when no `connection_ids` are given. |
| `alert_acknowledgments` | Acknowledgements recorded against alerts. | Rows for alerts the caller can see. |
| `alert_rules` | Alert rule definitions and their default thresholds. | All rows. |
| `alert_thresholds` | Per-server, per-cluster and per-group threshold overrides. | Estate-wide rows, and rows for accessible connections or the clusters and groups that contain one. |
| `blackouts` | One-off blackout windows. | Estate-wide rows, and rows for accessible connections or the clusters and groups that contain one. |
| `blackout_schedules` | Recurring blackout schedules. | Estate-wide rows, and rows for accessible connections or the clusters and groups that contain one. |
| `probe_configs` | Probe collection settings. | Global rows, and rows for accessible connections or the clusters and groups that contain one. |
| `alerter_settings` | Estate-wide alerter settings. | All rows. |
| `metric_baselines` | Baselines used by anomaly detection. | Rows for accessible connections. |
| `anomaly_candidates` | Metric values the anomaly detector flagged. | Rows for accessible connections. |
| `correlation_groups` | Anomalies the alerter correlated on one connection. | Rows for accessible connections. |
| `clusters` | Database clusters. | Clusters that contain an accessible connection. |
| `cluster_groups` | Groups of clusters. | Groups that contain an accessible connection. |

Metric sources return rows only for the connections the caller can access.
For an API token, the accessible connections are further limited to the
connections in the token's connection scope. Stored connection passwords and
certificates are never available from any source, and metric columns whose
names suggest a password, token or other secret are not offered.

### Describing the Sources

The `describe_datastore` tool accepts one optional parameter. When called
without `source`, the tool lists every source with the source's kind, time
column, scoping rule and description. When called with `source`, the tool
lists that source's columns with each column's data type and whether the
column holds a metric or a dimension; for a metric source, the output also
names the key columns that the `latest` shape uses.

Only the columns that `describe_datastore` lists can be selected, filtered,
grouped or ordered on in `query_datastore`. A caller who holds the
`query_datastore` privilege can also call `describe_datastore`.

### Query Parameters

The `query_datastore` tool requires the `source` parameter and rejects any
parameter the tool does not recognise; a `sql` or `query` argument returns an
error stating that the tool no longer accepts SQL.

The following table describes the `query_datastore` parameters:

| Parameter | Description |
|-----------|-------------|
| `source` | The source to query, as listed by `describe_datastore`; required. |
| `columns` | The columns to return; defaults to every column of the source, or names the columns to aggregate with the `aggregate` shape. |
| `connection_ids` | Limits the results to these connection IDs; defaults to every accessible connection, and naming an inaccessible connection returns an error. Applies only to metric sources and to sources scoped by connection or alert. |
| `time_start` | The start of the time range, as a relative duration such as `1h` or `7d` counted back from `time_end`, `now`, or an RFC 3339 timestamp; metric sources default to one hour before `time_end`. |
| `time_end` | The end of the time range, in the same forms as `time_start`; metric sources default to `now`. |
| `filters` | An array of at most 20 conditions, each with a `column`, an `op` and either a `value` or `values`. |
| `shape` | One of `rows` (the default), `latest` or `aggregate`. |
| `group_by` | At most 20 columns to group by; `aggregate` shape only. |
| `aggregation` | One of `avg`, `sum`, `min`, `max` or `count`; required with the `aggregate` shape. |
| `buckets` | Divides the time range into between 1 and 500 time buckets; `aggregate` shape on a source with a time column only. |
| `order_by` | An array of at most 20 ordering terms, each with a `column` and an optional `direction` of `asc` or `desc`. |
| `page` | The page of results to return; defaults to 1. The product of `page` and `page_size` may be at most 100000. |
| `page_size` | The rows per page; defaults to 100, with a maximum of 1000. |
| `explain` | When `true`, returns the query plan from `EXPLAIN` (never `EXPLAIN ANALYZE`) instead of rows. |

The time range applies only to sources with a time column, and the
one-hour default window for metric sources applies to every shape, including
`latest`. The `latest` shape returns the most recent row per connection and
entity within the time window, and applies only to metric sources. The `avg` and `sum` aggregations accept only numeric metric
columns.

The filter operators `=`, `!=`, `<`, `<=`, `>`, `>=` and `like` take a
`value`, written as a string such as `"app"` or `"42"`; the `like` operator
needs a text column. The `in` and `not in` operators take `values`, a list of
up to 1000 strings. The `is null` and `is not null` operators take neither.

A filter on a time column takes an RFC 3339 timestamp or `now` as the
`value`. The server breaks ties in the ordering using the row's key columns,
so pages are stable when many rows share the same ordering value.

The tool returns results as tab-separated values; when more rows exist, the
response names the page to request next. Each query runs in a read-only
transaction with a statement timeout. When a query fails at run time, the tool
returns a general message rather than the database error, and the server logs
the detail.

### Querying Change-Tracked Probes

The `pg_settings`, `pg_hba_file_rules`, `pg_ident_file_mappings`,
`pg_server_info` and `pg_extension` probes store a row only when something
changes. The default one-hour window therefore often finds no rows, even with
the `latest` shape. When a metric source returns no rows on the first page and
the query gave no `time_start`, the response includes a hint to set a wider
range. Give these probes a wide `time_start`, such as `30d`.

### Choosing a Datastore Tool

The `query_datastore` tool returns stored values; the tool does not compute
rates or deltas from cumulative counters. Use `query_metrics` for counter
rates and deltas over time, and `query_database` to query a monitored
database. The `list_connections`, `get_alert_history`, `get_blackouts` and
`get_metric_baselines` tools return formatted views of the same data.

### Example Queries

In the following example, the arguments request the latest database
statistics for connection 3:

```json
{
  "source": "pg_stat_database",
  "connection_ids": [3],
  "shape": "latest",
  "columns": ["datname", "numbackends", "xact_commit"]
}
```

In the following example, the arguments request the hourly average number of
backends for each connection over the last day:

```json
{
  "source": "pg_stat_database",
  "time_start": "24h",
  "shape": "aggregate",
  "aggregation": "avg",
  "columns": ["numbackends"],
  "group_by": ["connection_id"],
  "buckets": 24
}
```

In the following example, the arguments request the current settings, using
a wide window because the `pg_settings` probe stores rows only on change:

```json
{
  "source": "pg_settings",
  "connection_ids": [3],
  "time_start": "30d",
  "shape": "latest"
}
```

In the following example, the arguments request the active alerts:

```json
{
  "source": "alerts",
  "filters": [{"column": "status", "op": "=", "value": "active"}]
}
```

In the following example, the arguments request the current and future
one-time blackouts, filtering the time column `end_time` against `now`:

```json
{
  "source": "blackouts",
  "filters": [{"column": "end_time", "op": ">", "value": "now"}]
}
```

## Alert Tools

These tools query alert data from the monitoring system.

| Tool | Description |
|------|-------------|
| `get_alert_history` | Queries alerts for monitored connections. |
| `get_alert_rules` | Queries alert rules and their effective thresholds. |
| `get_metric_baselines` | Queries statistical baselines for anomaly detection. |
| `get_blackouts` | Queries blackout periods and recurring schedules for monitored connections. |
| `get_timeline_events` | Queries the unified incident-investigation timeline of configuration changes, restarts, extension changes, alerts, and blackouts. |

## Memory Tools

These tools manage persistent memories that the AI
assistant can store and recall across conversations.

| Tool | Description |
|------|-------------|
| `store_memory` | Stores a persistent memory with a category, scope, and optional pinned flag. |
| `recall_memories` | Searches stored memories using semantic similarity; pinned memories are always included. |
| `delete_memory` | Deletes a stored memory by its ID; only the owning user can delete a memory. |

## Utility Tools

These tools provide general-purpose capabilities.

| Tool | Description |
|------|-------------|
| `generate_embedding` | Generates text embeddings from input text. |
| `search_knowledgebase` | Searches the pgEdge documentation knowledge base. |
| `read_resource` | Reads MCP resources via the tool interface for backward compatibility with older clients. |

## Available Resources

Resources expose read-only data that MCP clients can
retrieve without calling a tool. The following resources
are available:

| Resource URI | Description |
|--------------|-------------|
| `pg://system_info` | PostgreSQL server information including version and platform. |
| `pg://connection_info` | Current database connection details. |

Administrators can disable resources in the server
configuration.

## Authentication

All MCP endpoints require a valid API token for
authentication. Include the token in the `Authorization`
header of each request.

In the following example, a `curl` command lists the
available tools:

```bash
curl -X POST http://localhost:8080/mcp/v1 \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

## Related Documentation

- [Ask Ellie](ai/ask-ellie.md) describes the built-in
  AI assistant that uses these tools internally.
- [AI Overview](ai/overview.md) covers AI-powered
  summaries of database health.
