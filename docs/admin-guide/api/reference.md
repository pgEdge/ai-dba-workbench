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
[Authentication](../authentication.md).

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
| DELETE | `/api/v1/clusters/{id}` | Delete a cluster. |
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
| GET | `/api/v1/metrics/query` | Query metric data points. |
| GET | `/api/v1/metrics/baselines` | Get metric baseline values. |
| GET | `/api/v1/metrics/performance-summary` | Get a performance summary. |
| GET | `/api/v1/metrics/database-summaries` | Get database-level summaries. |
| GET | `/api/v1/metrics/top-queries` | Get the top queries by resource usage. |
| GET | `/api/v1/metrics/latest` | Get the latest probe snapshot per entity. |

### Timeline

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/timeline/events` | List timeline events. |

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
| GET | `/api/v1/llm/providers` | List LLM providers. |
| GET | `/api/v1/llm/models` | List available models. |
| POST | `/api/v1/llm/chat` | Send a chat message to the LLM. |

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

### Utilities

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/chat/compact` | Compact chat history. |
| GET | `/health` | Check server health (no auth required). |

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
