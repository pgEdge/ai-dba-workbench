# API Reference

The AI DBA Workbench server provides a RESTful API for client applications.
This page provides interactive documentation for exploring and testing the API.

## API Discovery

The API implements RFC 8631 for API discovery. All JSON responses include a
Link header pointing to the OpenAPI specification:

```
Link: </api/v1/openapi.json>; rel="service-desc"
```

This enables API discovery tools like `restish` to automatically understand
the API structure.

## OpenAPI Specification

The OpenAPI 3.0.3 specification is available at runtime:

- JSON format is available at `GET /api/v1/openapi.json`.

You can use this specification with tools like Postman, Insomnia, or any
OpenAPI-compatible client.

## API Versioning

All REST API endpoints use version prefixes:

- The current version is `/api/v1/`.
- The MCP protocol uses `/mcp/v1` with separate versioning.

Version changes follow semantic versioning principles. Breaking changes
result in a new major version.

## Authentication

Most API endpoints require authentication. Include a Bearer token in the
Authorization header:

```bash
curl -H "Authorization: Bearer YOUR_TOKEN" \
     https://localhost:8080/api/v1/connections
```

Tokens can be:

- Session tokens are obtained via `/api/v1/auth/login`.
- Service tokens are created via CLI for programmatic access.

## Interactive API Browser

The interactive API browser below allows you to explore all available
endpoints, view request and response schemas, and understand the API
structure.

<div id="redoc-container"></div>

<script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
<script>
  Redoc.init('/api/v1/openapi.json', {
    scrollYOffset: 60,
    hideDownloadButton: false,
    theme: {
      colors: {
        primary: {
          main: '#1976d2'
        }
      },
      typography: {
        fontSize: '15px',
        fontFamily: '"Roboto", "Helvetica", "Arial", sans-serif',
        headings: {
          fontFamily: '"Roboto", "Helvetica", "Arial", sans-serif'
        },
        code: {
          fontSize: '13px',
          fontFamily: '"Roboto Mono", monospace'
        }
      },
      sidebar: {
        width: '260px'
      }
    }
  }, document.getElementById('redoc-container'));
</script>

<style>
  #redoc-container {
    margin-top: 20px;
    min-height: 800px;
  }

  /* Hide MKDocs TOC when viewing API browser */
  .md-sidebar--secondary {
    display: none;
  }

  /* Expand content area for API browser */
  .md-content {
    max-width: none;
  }
</style>

## Endpoint Summary

The API provides endpoints in the following categories:

### Authentication

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/auth/login` | Authenticate and obtain session token |
| GET | `/api/v1/user/info` | Get current user information |

### Connections

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/connections` | List all connections |
| POST | `/api/v1/connections` | Create a new connection |
| GET | `/api/v1/connections/{id}` | Get connection by ID |
| PUT | `/api/v1/connections/{id}` | Update connection |
| DELETE | `/api/v1/connections/{id}` | Delete connection |
| GET | `/api/v1/connections/{id}/databases` | List databases |
| GET | `/api/v1/connections/current` | Get current connection |
| POST | `/api/v1/connections/current` | Set current connection |
| DELETE | `/api/v1/connections/current` | Clear current connection |

### Clusters and Groups

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/clusters` | List all clusters |
| GET | `/api/v1/clusters/{id}` | Get cluster by ID |
| PUT | `/api/v1/clusters/{id}` | Update cluster |
| DELETE | `/api/v1/clusters/{id}` | Delete cluster |
| GET | `/api/v1/clusters/{id}/servers` | List servers in cluster |
| GET | `/api/v1/cluster-groups` | List all cluster groups |
| POST | `/api/v1/cluster-groups` | Create cluster group |
| GET | `/api/v1/cluster-groups/{id}` | Get cluster group |
| PUT | `/api/v1/cluster-groups/{id}` | Update cluster group |
| DELETE | `/api/v1/cluster-groups/{id}` | Delete cluster group |
| GET | `/api/v1/cluster-groups/{id}/clusters` | List clusters in group |
| POST | `/api/v1/cluster-groups/{id}/clusters` | Add cluster to group |

### Alerts

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/alerts` | List alerts with filters |
| GET | `/api/v1/alerts/counts` | Get alert counts by server |
| POST | `/api/v1/alerts/acknowledge` | Acknowledge an alert |
| DELETE | `/api/v1/alerts/acknowledge` | Unacknowledge an alert |

### Timeline

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/timeline/events` | List timeline events |

### Conversations

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/conversations` | List conversations |
| POST | `/api/v1/conversations` | Create conversation |
| DELETE | `/api/v1/conversations` | Delete all conversations |
| GET | `/api/v1/conversations/{id}` | Get conversation |
| PUT | `/api/v1/conversations/{id}` | Replace conversation |
| PATCH | `/api/v1/conversations/{id}` | Update conversation |
| DELETE | `/api/v1/conversations/{id}` | Delete conversation |

### LLM Proxy

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/llm/providers` | List LLM providers |
| GET | `/api/v1/llm/models` | List available models |
| POST | `/api/v1/llm/chat` | Send chat message |

### RBAC Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/rbac/users` | List all users |
| POST | `/api/v1/rbac/users` | Create a user |
| GET | `/api/v1/rbac/users/{id}` | Get a user by ID |
| PUT | `/api/v1/rbac/users/{id}` | Update a user |
| DELETE | `/api/v1/rbac/users/{id}` | Delete a user |
| GET | `/api/v1/rbac/users/{id}/privileges` | Get effective privileges |
| GET | `/api/v1/rbac/groups` | List all groups |
| POST | `/api/v1/rbac/groups` | Create a group |
| GET | `/api/v1/rbac/groups/{id}` | Get a group by ID |
| PUT | `/api/v1/rbac/groups/{id}` | Update a group |
| DELETE | `/api/v1/rbac/groups/{id}` | Delete a group |
| POST | `/api/v1/rbac/groups/{id}/members` | Add a group member |
| DELETE | `/api/v1/rbac/groups/{id}/members/{type}/{member_id}` | Remove a group member |
| GET | `/api/v1/rbac/groups/{id}/effective-privileges` | Get group privileges |
| GET | `/api/v1/rbac/tokens` | List all tokens |
| POST | `/api/v1/rbac/tokens` | Create a token |
| DELETE | `/api/v1/rbac/tokens/{id}` | Delete a token |
| GET | `/api/v1/rbac/tokens/{id}/scope` | Get token scope |
| PUT | `/api/v1/rbac/tokens/{id}/scope` | Set token scope |
| DELETE | `/api/v1/rbac/tokens/{id}/scope` | Clear token scope |
| GET | `/api/v1/rbac/privileges/mcp` | List MCP privileges |

### Utilities

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/chat/compact` | Compact chat history |
| GET | `/health` | Health check (no auth required) |

## Error Responses

All API errors return a consistent JSON format:

```json
{
  "error": "Description of the error"
}
```

Common HTTP status codes:

| Status | Meaning |
|--------|---------|
| 200 | Success |
| 400 | Bad request - invalid parameters |
| 401 | Unauthorized - missing or invalid token |
| 403 | Forbidden - insufficient permissions |
| 404 | Not found - resource does not exist |
| 405 | Method not allowed |
| 500 | Internal server error |
| 503 | Service unavailable |
