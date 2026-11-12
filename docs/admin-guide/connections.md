# Connection Management

The MCP server provides REST API endpoints that manage connections to
monitored PostgreSQL databases, so that each user can select the database
connection to work with during a session.

When the MCP server connects to the datastore, the server reads the connection
information that the collector stores. The connection endpoints let a user:

- list the available database connections.
- select a connection for the session.
- switch between the databases on a connection.
- keep the selection across requests.

!!! hint

    The server stores each connection selection in the authentication
    database (`auth.db`), so the selection persists across requests and server
    restarts. Each token holds an independent connection selection.

When a user selects a connection, the database tools (`query_database`,
`get_schema_info`, `similarity_search`, `execute_explain`, `count_rows`, and
`test_query`) run against the selected database unless the request names a
connection with the `connection_id` argument. When a request names no
connection and the token has no selected connection, the tools return the
following error:

```text
No database connection available. Specify connection_id or select a connection first.
```

To enable connection management, configure the server's database settings to
point to the same datastore that the collector uses:

```yaml
database:
  host: localhost
  port: 5432
  database: ai_workbench
  user: ai_workbench
  sslmode: prefer

# Secret file must match the collector's for decryption
secret_file: /etc/ai-workbench/secret
```

The `secret_file` must contain the same secret as the collector's secret file,
because the server encrypts the connection passwords that the collector
decrypts. For full configuration details, see
[Configuring the Server](../configuration/server/index.md).

The role whose credentials a connection stores needs privileges on the
monitored server;
[Preparing Monitored Servers](monitored-database-privileges.md)
describes those privileges.

## REST API Endpoints

All endpoints require a Bearer token in the `Authorization` header; the
[Authentication](api/reference.md#authentication) section of the API Reference
shows an example, and
[Managing Tokens](managing-users-and-permissions/tokens.md) describes how to
create a token. This section describes the endpoints that list connections and
their databases, select and clear a session's connection, and delete a
connection. The [Connections](api/reference.md#connections) table in the API
Reference lists every connection endpoint, including the endpoints that create
and update a connection and validate a query against one, and the
[API Browser](api/browser.md) shows the request and response schema for each
endpoint.

### List Connections

The following endpoint lists all available database connections from the
datastore:

```
GET /api/v1/connections
Authorization: Bearer <token>
```

Response:

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

The following endpoint lists all databases on a specific connection:

```
GET /api/v1/connections/{id}/databases
Authorization: Bearer <token>
```

Response:

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

The following endpoint returns the currently selected connection for the
session:

```
GET /api/v1/connections/current
Authorization: Bearer <token>
```

Response (200 OK):

```json
{
    "connection_id": 1,
    "database_name": "myapp",
    "host": "db.example.com",
    "port": 5432,
    "name": "Production DB"
}
```

Response (404 Not Found):

```json
{
    "error": "No database connection selected"
}
```

### Set Current Connection

The following endpoint selects a connection and optionally a specific database:

```
POST /api/v1/connections/current
Authorization: Bearer <token>
Content-Type: application/json

{
    "connection_id": 1,
    "database_name": "analytics"
}
```

The `database_name` is optional. If not specified, the connection's default
database is used.

Response:

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

The following endpoint clears the current connection selection:

```
DELETE /api/v1/connections/current
Authorization: Bearer <token>
```

Response: `204 No Content`

### Delete a Connection

The caller must own the connection or hold the `manage_connections`
permission, and must be able to see the connection, to delete it. A connection
the caller cannot see returns `404 Not Found`, exactly as a missing connection
does, even to a holder of `manage_connections`; updating a connection follows
the same rule. The following endpoint deletes a connection:

```
DELETE /api/v1/connections/{id}
Authorization: Bearer <token>
```

Response: `204 No Content`

The server responds as soon as it marks the connection deleted. From that
moment, the connection disappears from every listing, the API, and the MCP
tools, and the collector and alerter stop monitoring it. The server then
removes the connection's collected metrics in the background, in small
batches, and removes the connection itself when the metrics are gone. A
connection with a long metrics history may take some time to clear from the
datastore, but the delete request does not wait for the purge.

The background purge runs when the server starts, every five minutes, and
immediately after each delete; a server restart resumes any interrupted purge.

On a datastore that stores metrics in pgEdge ColdFront, the purge deletes the
connection's rows from each metrics view in a separate transaction before
removing the connection. When a view delete fails, the purge keeps the
connection and retries it on a later run. The
[Storing Metrics in pgEdge ColdFront](../configuration/collector/coldfront-metrics.md)
page describes that mode.

## Error Handling

The connection APIs return standard error responses.

| Status | Description |
|--------|-------------|
| 400 | Invalid request (bad connection ID or missing fields). |
| 401 | Invalid or missing authentication token. |
| 403 | The caller can see the connection but may not change it. |
| 404 | Connection not found, not visible to the caller, or no current connection selected. |
| 500 | Internal server error. |
| 503 | Datastore not configured. |

In the following example, an error response uses the standard JSON format:

```json
{
    "error": "Description of the error"
}
```
