# Server Information

The Server Information API provides comprehensive details
about a monitored PostgreSQL server and its host system.
The API aggregates data from collector metrics into a
single response for each connection.

## Overview

The Server Information feature provides the following
capabilities:

- The API returns system hardware details including CPU,
  memory, and disk information.
- The API includes PostgreSQL configuration such as
  version, cluster name, and connection limits.
- The response lists all databases with sizes, encodings,
  and installed extensions.
- The API returns 16 curated PostgreSQL configuration
  settings for quick review.
- An optional AI analysis endpoint describes the likely
  purpose of each database.

## Authentication

Both endpoints require a valid Bearer token in the
`Authorization` header. The server enforces RBAC access
checks on the specified connection. The server returns
a `403 Forbidden` response when the authenticated user
lacks access to the connection.

## Server Information Endpoint

The server information endpoint returns system and
PostgreSQL details for a single connection.

### Endpoint

The following endpoint returns the server information:

```
GET /api/v1/server-info/{connection_id}
```

### Path Parameters

The following table describes the path parameters:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `connection_id` | integer | Yes | The numeric ID of the connection to query. |

### Response

The server returns a JSON object containing system
information, PostgreSQL configuration, databases, and
key settings. The following table describes the
top-level response fields:

| Field | Type | Description |
|-------|------|-------------|
| `connection_id` | integer | The connection ID that was queried. |
| `collected_at` | string | The ISO 8601 timestamp of the most recent data collection. |
| `system` | object | The host operating system and hardware details. |
| `postgresql` | object | The PostgreSQL server configuration. |
| `databases` | array | The list of databases on the server. |
| `extensions` | array | The list of installed extensions across all databases. |
| `key_settings` | array | The curated list of PostgreSQL configuration settings. |

### System Object

The following table describes the fields in the
`system` object:

| Field | Type | Description |
|-------|------|-------------|
| `os_name` | string | The operating system name. |
| `os_version` | string | The operating system version. |
| `architecture` | string | The CPU architecture. |
| `hostname` | string | The server hostname. |
| `cpu_model` | string | The CPU model name. |
| `cpu_cores` | integer | The number of physical CPU cores. |
| `cpu_logical` | integer | The number of logical processors. |
| `cpu_clock_speed` | integer | The CPU clock speed in hertz. |
| `memory_total_bytes` | integer | The total system memory in bytes. |
| `memory_used_bytes` | integer | The used system memory in bytes. |
| `memory_free_bytes` | integer | The free system memory in bytes. |
| `swap_total_bytes` | integer | The total swap space in bytes. |
| `swap_used_bytes` | integer | The used swap space in bytes. |
| `disks` | array | The list of mounted disk volumes. |

### PostgreSQL Object

The following table describes the fields in the
`postgresql` object:

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | The PostgreSQL server version string. |
| `cluster_name` | string | The cluster name from the PostgreSQL configuration. |
| `data_directory` | string | The path to the PostgreSQL data directory. |
| `max_connections` | integer | The maximum number of allowed connections. |
| `max_wal_senders` | integer | The maximum number of WAL sender processes. |
| `max_replication_slots` | integer | The maximum number of replication slots. |

### Database Object

The following table describes the fields in each
`databases` array entry:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | The database name. |
| `size_bytes` | integer | The database size in bytes. |
| `encoding` | string | The character encoding for the database. |
| `connection_limit` | integer | The per-database connection limit. |
| `extensions` | array | The list of extension names installed in the database. |

The response excludes the `template0` and `template1`
system databases.

### Extension Object

The following table describes the fields in each
`extensions` array entry:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | The extension name. |
| `version` | string | The installed extension version. |
| `schema` | string | The schema where the extension is installed. |
| `database` | string | The database containing the extension. |

### Setting Object

The following table describes the fields in each
`key_settings` array entry:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | The PostgreSQL setting name. |
| `setting` | string | The current setting value. |
| `unit` | string | The unit for the setting value; omitted when not applicable. |
| `category` | string | The PostgreSQL configuration category. |

The endpoint returns the following 16 curated settings:

- `shared_buffers`
- `work_mem`
- `effective_cache_size`
- `maintenance_work_mem`
- `max_worker_processes`
- `wal_level`
- `archive_mode`
- `max_wal_size`
- `min_wal_size`
- `checkpoint_completion_target`
- `random_page_cost`
- `effective_io_concurrency`
- `max_parallel_workers`
- `max_parallel_workers_per_gather`
- `autovacuum`
- `log_min_duration_statement`

### Example

In the following example, a `curl` command requests the
server information for connection 1:

```bash
curl -H "Authorization: Bearer <token>" \
  https://localhost:8080/api/v1/server-info/1
```

## AI Analysis Endpoint

The AI analysis endpoint returns LLM-generated
descriptions of each database on the server. The
endpoint analyzes database names, sizes, and installed
extensions to infer the purpose of each database.

### Endpoint

The following endpoint returns the AI analysis:

```
GET /api/v1/server-info/{connection_id}/ai-analysis
```

### Path Parameters

The following table describes the path parameters:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `connection_id` | integer | Yes | The numeric ID of the connection to analyze. |

### Response

The server returns a JSON object with per-database
descriptions. The following table describes the
response fields:

| Field | Type | Description |
|-------|------|-------------|
| `databases` | object | A map of database names to AI-generated descriptions. |
| `generated_at` | string | The ISO 8601 timestamp when the analysis was generated. |

The server returns `null` when no LLM provider is
configured. The server also returns `null` when the
connection has no databases to analyze.

### Caching

The server caches AI analysis results for five minutes
per connection. Subsequent requests within the cache
window return the cached analysis without calling the
LLM. The cache refreshes automatically when a request
arrives after the five-minute window expires.

### Example

In the following example, a `curl` command requests the
AI analysis for connection 1:

```bash
curl -H "Authorization: Bearer <token>" \
  https://localhost:8080/api/v1/server-info/1/ai-analysis
```

The server returns a response similar to the following:

```json
{
  "databases": {
    "myapp": "A web application database with PostGIS.",
    "analytics": "A data warehouse for reporting."
  },
  "generated_at": "2026-02-10T12:00:00Z"
}
```

## Error Responses

Both endpoints return standard error responses. The
following table describes the possible error statuses:

| Status | Meaning |
|--------|---------|
| 400 | The connection ID is missing or invalid. |
| 401 | The request lacks a valid authentication token. |
| 403 | The user does not have access to the connection. |
| 500 | An internal server error occurred. |

## Configuration

The server information endpoint requires the collector
to be gathering metrics for the specified connection.
The endpoint returns empty fields when the collector
has not yet gathered data for a metric category.

The AI analysis endpoint requires an LLM provider to
be configured in the server settings. For LLM provider
setup instructions, see [Configuration](configuration.md).

## Related Documentation

- [Connections](connections.md) describes how to manage
  database connections.
- [Metrics](metrics.md) documents the metrics collection
  endpoints.
- [AI Overview](ai-overview.md) describes the AI-powered
  summary feature.
