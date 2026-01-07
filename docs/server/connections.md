# Connection Management

The MCP server provides REST APIs for managing connections to monitored
PostgreSQL databases. This feature allows users to select which database
connection they want to work with for their session.

## Overview

When the MCP server is configured with a datastore connection, it can access
connection information stored by the Collector. This enables users to:

- List available database connections
- Select a connection for their session
- Switch between databases on a connection
- Persist their selection across requests

## Configuration

To enable connection management, configure the server's database settings to
point to the same datastore used by the Collector:

```yaml
database:
  host: localhost
  port: 5432
  database: ai_workbench
  user: ai_workbench
  sslmode: prefer

# Secret file must match the collector's for password decryption
secret_file: /etc/ai-workbench/secret
```

The `secret_file` must contain the same secret used by the Collector for
encrypting connection passwords.

## REST API Endpoints

All endpoints require authentication via Bearer token.

### List Connections

List all available database connections from the datastore.

```
GET /api/connections
Authorization: Bearer <token>
```

**Response:**

```json
[
    {
        "id": 1,
        "name": "Production DB",
        "host": "db.example.com",
        "port": 5432,
        "database_name": "myapp",
        "is_monitored": true
    },
    {
        "id": 2,
        "name": "Staging DB",
        "host": "staging.example.com",
        "port": 5432,
        "database_name": "myapp_staging",
        "is_monitored": true
    }
]
```

### List Databases

List all databases on a specific connection.

```
GET /api/connections/{id}/databases
Authorization: Bearer <token>
```

**Response:**

```json
[
    {
        "name": "myapp",
        "owner": "postgres",
        "encoding": "UTF8",
        "size": "125 MB"
    },
    {
        "name": "analytics",
        "owner": "postgres",
        "encoding": "UTF8",
        "size": "2 GB"
    }
]
```

### Get Current Connection

Get the currently selected connection for the session.

```
GET /api/connections/current
Authorization: Bearer <token>
```

**Response (200 OK):**

```json
{
    "connection_id": 1,
    "database_name": "myapp",
    "host": "db.example.com",
    "port": 5432,
    "name": "Production DB"
}
```

**Response (404 Not Found):**

```json
{
    "error": "No database connection selected"
}
```

### Set Current Connection

Select a connection and optionally a specific database.

```
POST /api/connections/current
Authorization: Bearer <token>
Content-Type: application/json

{
    "connection_id": 1,
    "database_name": "analytics"
}
```

The `database_name` is optional. If not specified, the connection's default
database is used.

**Response:**

```json
{
    "connection_id": 1,
    "database_name": "analytics",
    "host": "db.example.com",
    "port": 5432,
    "name": "Production DB"
}
```

### Clear Current Connection

Clear the current connection selection.

```
DELETE /api/connections/current
Authorization: Bearer <token>
```

**Response:** `204 No Content`

## Session Persistence

Connection selections are stored in the authentication database (auth.db) and
persist across requests and server restarts. Each token has its own independent
connection selection.

## Tool Behavior

When a connection is selected, all database tools (`query_database`,
`get_schema_info`, `similarity_search`, `execute_explain`, `count_rows`)
operate on the selected database.

If no connection is selected, the tools return an error message instructing
the user to select a connection:

```
No database connection selected. Please select a database connection
using your client interface (CLI or web client).
```

## CLI Usage

The CLI provides slash commands for connection management:

```
/list connections              List available database connections
/list databases                List databases on current connection
/connect                       Show current database connection
/connect <id> [database]       Connect to database (by connection ID)
/disconnect                    Disconnect from current database
```

### Examples

```
> /list connections
Available connections (2):

  1: Production DB [monitored]
     Host: db.example.com:5432, Database: myapp

  2: Staging DB [monitored]
     Host: staging.example.com:5432, Database: myapp_staging

Use '/connect <id>' to select a connection

> /connect 1
Connected to: Production DB (db.example.com:5432)

> /list databases
Databases on Production DB (3):

* myapp (owner: postgres, size: 125 MB, encoding: UTF8)
  analytics (owner: postgres, size: 2 GB, encoding: UTF8)
  archive (owner: postgres, size: 500 MB, encoding: UTF8)

Use '/connect <connection-id> <database-name>' to select a specific database

> /connect 1 analytics
Connected to: Production DB (db.example.com:5432, database: analytics)
```

## Error Handling

The connection APIs return standard error responses:

| Status | Description |
|--------|-------------|
| 400 | Invalid request (bad connection ID, missing required fields) |
| 401 | Invalid or missing authentication token |
| 404 | Connection not found, or no current connection selected |
| 500 | Internal server error |
| 503 | Datastore not configured |

Error response format:

```json
{
    "error": "Description of the error"
}
```
