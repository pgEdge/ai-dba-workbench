# API Reference

The AI DBA Workbench server provides a RESTful API for
client applications. This page provides interactive
documentation for exploring and testing the API.

## API Discovery

The API implements RFC 8631 for API discovery. All JSON
responses include a Link header pointing to the OpenAPI
specification:

```
Link: </api/v1/openapi.json>; rel="service-desc"
```

This enables API discovery tools like `restish` to
automatically understand the API structure.

## OpenAPI Specification

The OpenAPI 3.0.3 specification is available at the
following locations:

- At runtime via `GET /api/v1/openapi.json`.
- As a static file at `docs/admin-guide/api/openapi.json`.

You can use this specification with tools like Postman,
Insomnia, or any OpenAPI-compatible client.

## API Versioning

All REST API endpoints use version prefixes:

- The current version is `/api/v1/`.
- The MCP protocol uses `/mcp/v1` with separate
  versioning.

Version changes follow semantic versioning principles.
Breaking changes result in a new major version.

## Authentication

Most API endpoints require authentication. Include a
Bearer token in the Authorization header.

In the following example, the `curl` command uses a
Bearer token to authenticate:

```bash
curl -H "Authorization: Bearer YOUR_TOKEN" \
     https://localhost:8080/api/v1/connections
```

Tokens can be one of two types:

- Session tokens are obtained via
  `/api/v1/auth/login`.
- Service tokens are created via the server command
  line for programmatic access.

For detailed authentication information, see
[Managing Users and Permissions](../managing-users-and-permissions/permission_model.md).

## Interactive API Browser

The interactive API browser renders the OpenAPI
specification in a searchable, navigable format.

[Open the API Browser](browser.md)

## Endpoint Summary

The API provides endpoints in the following categories.

### Authentication

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/auth/login` | Authenticate and obtain a session token. |
| POST | `/api/v1/auth/logout` | Log out and clear the session cookie. |
| GET | `/api/v1/auth/oidc/start` | Start a federated (OIDC) login. |
| GET | `/api/v1/auth/oidc/callback` | Complete a federated (OIDC) login. |
| GET | `/api/v1/user/info` | Get the current user information. |
| GET | `/api/v1/capabilities` | Get server capability flags. |

### Connections

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/connections` | List all connections. |
| POST | `/api/v1/connections` | Create a new connection. |
| GET | `/api/v1/connections/{id}` | Get a connection by ID. |
| PUT | `/api/v1/connections/{id}` | Update a connection. |
| DELETE | `/api/v1/connections/{id}` | Delete a connection. |
| GET | `/api/v1/connections/{id}/databases` | List databases for a connection. |
| GET | `/api/v1/connections/current` | Get the current connection. |
| POST | `/api/v1/connections/current` | Set the current connection. |
| DELETE | `/api/v1/connections/current` | Clear the current connection. |

### Clusters and Groups

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/clusters` | Get the cluster topology hierarchy. |
| POST | `/api/v1/clusters` | Create a new cluster. |
| GET | `/api/v1/clusters/list` | List all clusters as a flat list. |
| GET | `/api/v1/clusters/{id}` | Get a cluster by ID. |
| PUT | `/api/v1/clusters/{id}` | Update a cluster. |
| DELETE | `/api/v1/clusters/{id}` | Delete a cluster; accepts numeric, server-{id}, or cluster-spock-{prefix} IDs. |
| GET | `/api/v1/clusters/{id}/servers` | List servers in a cluster. |
| POST | `/api/v1/clusters/{id}/servers` | Add a server to a cluster. |
| DELETE | `/api/v1/clusters/{id}/servers/{connectionId}` | Remove a server from a cluster. |
| GET | `/api/v1/clusters/{id}/relationships` | List cluster relationships. |
| DELETE | `/api/v1/clusters/{id}/relationships/{relationshipId}` | Delete a cluster relationship. |
| GET | `/api/v1/cluster-groups` | List all cluster groups. |
| POST | `/api/v1/cluster-groups` | Create a cluster group. |
| GET | `/api/v1/cluster-groups/{id}` | Get a cluster group by ID. |
| PUT | `/api/v1/cluster-groups/{id}` | Update a cluster group. |
| DELETE | `/api/v1/cluster-groups/{id}` | Delete a cluster group. |
| GET | `/api/v1/cluster-groups/{id}/clusters` | List clusters in a group. |
| POST | `/api/v1/cluster-groups/{id}/clusters` | Create a cluster in a group. |

### Alerts

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/alerts` | List alerts with filters. |
| GET | `/api/v1/alerts/counts` | Get alert counts by server. |
| POST | `/api/v1/alerts/acknowledge` | Acknowledge an alert. |
| DELETE | `/api/v1/alerts/acknowledge` | Remove an alert acknowledgement. |
| PUT | `/api/v1/alerts/analysis` | Save an AI analysis for an alert. |

### Alert Rules

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/alert-rules` | List all alert rules. |
| GET | `/api/v1/alert-rules/{id}` | Get an alert rule by ID. |
| PUT | `/api/v1/alert-rules/{id}` | Update an alert rule. |

### Alert Overrides

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/alert-overrides/{scope}/{scopeId}` | List alert overrides for a scope. |
| PUT | `/api/v1/alert-overrides/{scope}/{scopeId}/{ruleId}` | Create or update an override. |
| DELETE | `/api/v1/alert-overrides/{scope}/{scopeId}/{ruleId}` | Delete an override. |
| GET | `/api/v1/alert-overrides/context/{connectionId}/{ruleId}` | Get the override editing context. |

### Blackouts

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/blackouts` | List blackout windows. |
| POST | `/api/v1/blackouts` | Create a blackout window. |
| GET | `/api/v1/blackouts/{id}` | Get a blackout by ID. |
| PUT | `/api/v1/blackouts/{id}` | Update a blackout. |
| DELETE | `/api/v1/blackouts/{id}` | Delete a blackout. |
| POST | `/api/v1/blackouts/{id}/stop` | Stop an active blackout early. |
| GET | `/api/v1/blackout-schedules` | List blackout schedules. |
| POST | `/api/v1/blackout-schedules` | Create a blackout schedule. |
| GET | `/api/v1/blackout-schedules/{id}` | Get a blackout schedule by ID. |
| PUT | `/api/v1/blackout-schedules/{id}` | Update a blackout schedule. |
| DELETE | `/api/v1/blackout-schedules/{id}` | Delete a blackout schedule. |

### Notification Channels

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/notification-channels` | List all notification channels. |
| POST | `/api/v1/notification-channels` | Create a notification channel. |
| GET | `/api/v1/notification-channels/{id}` | Get a channel by ID. |
| PUT | `/api/v1/notification-channels/{id}` | Update a channel. |
| DELETE | `/api/v1/notification-channels/{id}` | Delete a channel. |
| POST | `/api/v1/notification-channels/{id}/test` | Send a test notification. |
| GET | `/api/v1/notification-channels/{id}/recipients` | List email recipients. |
| POST | `/api/v1/notification-channels/{id}/recipients` | Add an email recipient. |
| PUT | `/api/v1/notification-channels/{id}/recipients/{recipientId}` | Update a recipient. |
| DELETE | `/api/v1/notification-channels/{id}/recipients/{recipientId}` | Delete a recipient. |

### Channel Overrides

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/channel-overrides/{scope}/{scopeId}` | List channel overrides for a scope. |
| PUT | `/api/v1/channel-overrides/{scope}/{scopeId}/{channelId}` | Create or update an override. |
| DELETE | `/api/v1/channel-overrides/{scope}/{scopeId}/{channelId}` | Delete an override. |

### Probe Configuration

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/probe-configs` | List probe configurations. |
| GET | `/api/v1/probe-configs/{id}` | Get a probe configuration by ID. |
| PUT | `/api/v1/probe-configs/{id}` | Update a probe configuration. |

### Probe Overrides

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/probe-overrides/{scope}/{scopeId}` | List probe overrides for a scope. |
| PUT | `/api/v1/probe-overrides/{scope}/{scopeId}/{probeName}` | Create or update an override. |
| DELETE | `/api/v1/probe-overrides/{scope}/{scopeId}/{probeName}` | Delete an override. |

### Server Information

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/server-info/{connection_id}` | Get server information. |
| GET | `/api/v1/server-info/{id}/ai-analysis` | Get an AI database analysis. |

### Metrics

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/metrics/query` | Query metrics for preset or custom windows. |
| GET | `/api/v1/metrics/baselines` | Get metric baseline values. |
| GET | `/api/v1/metrics/performance-summary` | Get a performance summary. |
| GET | `/api/v1/metrics/database-summaries` | Get database-level summaries over a time window. |
| GET | `/api/v1/metrics/top-queries` | Get the top queries by resource usage over a time window. |
| GET | `/api/v1/metrics/connection-groups` | Get connection counts grouped by user, client, or database. |
| GET | `/api/v1/metrics/query-stats` | Get period-scoped statistics for a query. |
| GET | `/api/v1/metrics/latest` | Get the latest probe snapshot per entity. |

### Timeline

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/timeline/events` | List timeline events. |

The endpoint takes absolute `start_time` and `end_time`
values as RFC 3339 timestamps, and validates them with the
same rules as a custom window on `/api/v1/metrics/query`,
described under Metric Time Windows below: the end must fall
strictly after the start, the start must fall before the
present moment, the span must not exceed 366 days, and an
end in the future is clamped to the present moment.

### Overview

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/overview` | Get an AI-generated estate overview. |
| GET | `/api/v1/overview/stream` | Stream overview generation via SSE. |

### Conversations

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/conversations` | List conversations. |
| POST | `/api/v1/conversations` | Create a conversation. |
| DELETE | `/api/v1/conversations` | Delete all conversations. |
| GET | `/api/v1/conversations/{id}` | Get a conversation by ID. |
| PUT | `/api/v1/conversations/{id}` | Update a conversation. |
| PATCH | `/api/v1/conversations/{id}` | Rename a conversation. |
| DELETE | `/api/v1/conversations/{id}` | Delete a conversation. |

### LLM Proxy

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/llm/providers` | List LLM providers (public). |
| GET | `/api/v1/llm/models` | List available models (public). |
| GET | `/api/v1/llm/health` | Check LLM proxy health (public). |
| POST | `/api/v1/llm/chat` | Send a chat message to the LLM. |
| POST | `/api/v1/llm/chat/stream` | Stream a chat response as SSE. |
| POST | `/api/v1/llm/embed` | Generate embeddings for input text. |
| POST | `/api/v1/llm/embed/multimodal` | Generate embeddings for multimodal (text and image) inputs. |
| POST | `/api/v1/llm/rerank` | Rerank documents against a query. |

### Memory

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/memories` | List pinned memories. |
| DELETE | `/api/v1/memories/{id}` | Delete a memory. |
| PATCH | `/api/v1/memories/{id}` | Update a memory pin status. |

### MCP Tools

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/mcp/tools` | List available MCP tools. |
| POST | `/api/v1/mcp/tools/call` | Execute an MCP tool. |

### RBAC Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/rbac/users` | List all users. |
| POST | `/api/v1/rbac/users` | Create a user. |
| PUT | `/api/v1/rbac/users/{id}` | Update a user. |
| DELETE | `/api/v1/rbac/users/{id}` | Delete a user. |
| GET | `/api/v1/rbac/users/{id}/privileges` | Get a user's effective privileges. |
| GET | `/api/v1/rbac/groups` | List all groups. |
| POST | `/api/v1/rbac/groups` | Create a group. |
| GET | `/api/v1/rbac/groups/{id}` | Get a group by ID. |
| PUT | `/api/v1/rbac/groups/{id}` | Update a group. |
| DELETE | `/api/v1/rbac/groups/{id}` | Delete a group. |
| POST | `/api/v1/rbac/groups/{id}/members` | Add a group member. |
| DELETE | `/api/v1/rbac/groups/{id}/members/{type}/{memberId}` | Remove a group member. |
| GET | `/api/v1/rbac/groups/{id}/effective-privileges` | Get group effective privileges. |
| GET | `/api/v1/rbac/groups/{id}/privileges/mcp` | Get group MCP tool privileges. |
| PUT | `/api/v1/rbac/groups/{id}/privileges/mcp` | Set group MCP tool privileges. |
| GET | `/api/v1/rbac/groups/{id}/privileges/connections` | Get group connection privileges. |
| PUT | `/api/v1/rbac/groups/{id}/privileges/connections` | Set group connection privileges. |
| GET | `/api/v1/rbac/groups/{id}/permissions` | Get group admin permissions. |
| PUT | `/api/v1/rbac/groups/{id}/permissions` | Set group admin permissions. |
| GET | `/api/v1/rbac/tokens` | List all API tokens. |
| POST | `/api/v1/rbac/tokens` | Create an API token. |
| DELETE | `/api/v1/rbac/tokens/{id}` | Delete an API token. |
| GET | `/api/v1/rbac/tokens/{id}/scope` | Get a token scope. |
| PUT | `/api/v1/rbac/tokens/{id}/scope` | Set a token scope. |
| DELETE | `/api/v1/rbac/tokens/{id}/scope` | Clear a token scope. |
| GET | `/api/v1/rbac/privileges/mcp` | List all MCP privilege identifiers. |
| GET | `/api/v1/rbac/audit` | List RBAC audit events (superuser only). |

### Utilities

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/chat/compact` | Compact chat history. |
| GET | `/health` | Check server health (no auth required). |

## Metric Time Windows

The `/api/v1/metrics/query` endpoint accepts either a
rolling preset or an explicit window. The `time_range`
parameter takes one of the `1h`, `6h`, `24h`, `7d`, and
`30d` presets, or the value `custom`; the following
parameters select the window:

| Parameter | Required | Description |
|-----------|----------|-------------|
| `time_range` | No | The window selector; defaults to `1h`. |
| `time_start` | For `custom` | The window start, as RFC 3339. |
| `time_end` | For `custom` | The window end, as RFC 3339. |

A preset selects a rolling window that ends at the
present moment; the server ignores `time_start` and
`time_end` for every preset. The `custom` value selects
the window between the two supplied timestamps instead.

The server validates a custom window against the
following rules, and returns status 400 with an
explanatory message when a request breaks one:

- The request must supply both `time_start` and
  `time_end`.
- Both timestamps must parse as RFC 3339 values.
- The end must fall strictly after the start.
- The start must fall before the present moment.
- The span must not exceed 366 days.

Three endpoints apply a tighter limit of 30 days on top of
that, because their cost grows with the window rather than
being absorbed by a wider bucket:
`/api/v1/metrics/top-queries`,
`/api/v1/metrics/performance-summary` and
`/api/v1/metrics/database-summaries`. A span longer than 30
days but no longer than 366 days is rejected with
`invalid time range: span must not exceed 30 days`, and a
span longer than 366 days with the 366 day message above.
Thirty days is the longest preset, so no preset is
affected.

The server clamps an end time in the future to the
present moment rather than rejecting the request,
because a picker set to the current day often overshoots
by a few minutes. The span limit protects the server
because the bucket width derives from the span; an
unbounded window would let a single request scan an
arbitrary amount of history.

In the following example, the `curl` command queries CPU
metrics for an eight-hour window in the past:

```bash
QUERY="probe_name=pg_sys_cpu_info&connection_id=1"
WINDOW="time_range=custom&time_start=2026-07-01T09:00:00Z"
WINDOW="$WINDOW&time_end=2026-07-01T17:00:00Z"

curl -H "Authorization: Bearer YOUR_TOKEN" \
     "https://localhost:8080/api/v1/metrics/query?$QUERY&$WINDOW"
```

The `/api/v1/metrics/query`,
`/api/v1/metrics/connection-groups`,
`/api/v1/metrics/database-summaries`,
`/api/v1/metrics/performance-summary`,
`/api/v1/metrics/query-stats` and
`/api/v1/metrics/top-queries` endpoints support a custom
window with the same three parameters and the same rules;
`performance-summary` derives its bucket width from the
resolved window, one sixtieth of the span with a ten second
floor.

The `/api/v1/metrics/database-summaries` endpoint defaults
to `time_range=24h` when the request names no window, and
bounds every figure in the response by the resolved window.
The database size, the connection count, the dead tuple
ratio and the transaction rate all describe the state at
the end of the window rather than the present moment, so a
window that ends in the past reports each database as the
database stood then. A window that ends at the present
moment returns the current figures, as before.

The `/api/v1/metrics/query` and
`/api/v1/metrics/performance-summary` endpoints accept at
most 100 entries in `connection_ids`, because each one adds
a further set of queries to the request; a longer list
returns status 400.

The `/api/v1/metrics/top-queries` endpoint defaults to
`time_range=1h` when the request names no window.
PostgreSQL reports the `pg_stat_statements` counters
cumulatively, so the endpoint sums the differences between
consecutive samples in the window rather than reading one
sample. The behaviour that follows from that is:

- The `calls`, `total_exec_time`, `rows`,
  `shared_blks_hit` and `shared_blks_read` fields report
  the activity inside the window.
- The `mean_exec_time` field divides the windowed total
  execution time by the windowed call count.
- The `min_exec_time` and `max_exec_time` fields remain
  lifetime figures for the statement, because a window
  extreme cannot be recovered by differencing two lifetime
  extremes. Ordering on either field therefore sorts a
  windowed list on a lifetime figure.
- A pair of samples whose counters went backwards is
  discarded as a `pg_stat_statements_reset()`, so a reset
  inside the window loses the activity of that one
  interval rather than producing a negative figure.
- A statement that the collector sampled but that ran no
  calls inside the window is left out of the response
  altogether.
- A custom window may span at most 30 days, which is the
  longest preset; the endpoint rejects a longer span with
  `span must not exceed 30 days`, as described under
  Metric Time Windows below.
- On a server with `pg_stat_statements` installed in more
  than one database, the collector stores each counter
  once per such database. The endpoint counts each
  counter once, and a `database_name` filter sums only
  the counters of the statements that ran in that
  database.

### Query Response Envelope

The `/api/v1/metrics/query` endpoint returns an object that
describes the window the server queried alongside the data
the server found. The bucketed mode described above returns
this envelope; the latest-rows mode, which a request selects
by passing `limit` or `order_by`, is unchanged and still
returns the matching rows.

In the following example, the server answers a six-hour
preset with a five-minute bucket width:

```json
{
  "probe_name": "pg_stat_all_tables",
  "connection_ids": [1],
  "time_range": "6h",
  "time_start": "2026-09-16T06:00:00Z",
  "time_end": "2026-09-16T12:00:00Z",
  "bucket_seconds": 300,
  "buckets": 72,
  "aggregation": "avg",
  "series": [
    {
      "name": "n_live_tup",
      "metric": "n_live_tup",
      "unit": "",
      "data": [
        {"time": "2026-09-16T06:00:00Z", "value": null},
        {"time": "2026-09-16T06:05:00Z", "value": 90},
        {"time": "2026-09-16T06:10:00Z", "value": 90, "filled": true}
      ]
    }
  ]
}
```

The following table describes the fields of the envelope:

| Field | Description |
|-------|-------------|
| `probe_name` | The probe the series were read from. |
| `connection_ids` | The connections the query covered. |
| `time_range` | The range selection the caller asked for. |
| `time_start` | The resolved window start, as RFC 3339. |
| `time_end` | The resolved window end, as RFC 3339. |
| `bucket_seconds` | The bucket width the query binned by. |
| `buckets` | The bucket count the query divided the window into. |
| `aggregation` | The aggregate applied within each bucket. |
| `series` | One entry per metric, per connection. |

The server divides the window into the number of buckets the
request asked for, then clamps that count to one bucket per
probe collection interval, because a bucket narrower than the
interval cannot hold a sample of its own; `buckets` and
`bucket_seconds` report the count and the width the query
actually used. The bucket times include both ends of the
window, so a series normally holds one point more than
`buckets` reports.

Every point in a series carries a `time` and a `value`, and a
point may also carry `filled`:

- A `value` of null marks a bucket that collected no data and
  had no earlier value to carry forward, so a chart draws a
  gap rather than a line across the bucket.
- A `filled` value of true marks a value carried forward from
  an earlier sample rather than aggregated from a sample of
  the bucket's own; the field is absent for every other point.

Because the envelope reports the window back to the caller,
a client can anchor a chart axis to the window the client
requested rather than to the extent of the data that came
back. An instance holding a few hours of history therefore
draws a 30-day request as a mostly empty 30-day axis, rather
than as the same chart a one-hour request produces.

## Error Responses

All API errors return a consistent JSON format.

In the following example, the server returns an error
response:

```json
{
  "error": "Description of the error"
}
```

The following table lists common HTTP status codes:

| Status | Meaning |
|--------|---------|
| 200 | Success. |
| 400 | Bad request with invalid parameters. |
| 401 | Unauthorized with missing or invalid token. |
| 403 | Forbidden with insufficient permissions. |
| 404 | Not found because resource does not exist. |
| 405 | Method not allowed. |
| 500 | Internal server error. |
| 503 | Service unavailable. |
