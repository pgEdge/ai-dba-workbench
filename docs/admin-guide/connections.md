# Connection Management

The MCP server provides REST APIs for managing
connections to monitored PostgreSQL databases. This
feature allows users to select which database connection
they want to work with for their session.

## Overview

When the MCP server is configured with a datastore
connection, the server can access connection information
stored by the Collector. This enables users to:

- List available database connections.
- Select a connection for their session.
- Switch between databases on a connection.
- Persist their selection across requests.

## Configuration

To enable connection management, configure the server's
database settings to point to the same datastore used
by the Collector:

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

The `secret_file` must contain the same secret used by
the Collector for encrypting connection passwords. For
full configuration details, see the
[Server Configuration](../getting-started/configuration/server.md).

## Monitored Server Privileges

Each connection stores the credentials of a PostgreSQL
role on the monitored server. Before creating a
connection, grant that role the privileges the
Workbench needs, which are the `pg_monitor` predefined
role and `CONNECT` on every database you want to
monitor. The
[Monitored Database Privileges](../getting-started/configuration/monitored-database-privileges.md)
document covers the least-privilege grants, the
optional extensions, and the gaps that remain without
superuser access.

## Connection Targets and SSL Files

The server checks the target and the SSL file settings of a connection
when a user creates or updates the connection; a refused value returns
status 400. Before each connection attempt, the server checks the SSL
file paths and the syntax of the stored `host` and `hostaddr` again,
and each time the server or the collector connects, each component
checks every address that the target resolves to, as described below.

The `host` and `hostaddr` fields name the monitored server:

- `host` is a host name or IP address, and must not contain `,`, `[`,
  `]`, `/`, `?`, `@`, `#`, `%` or whitespace.
- `hostaddr` is optional and must be a literal IPv4 or IPv6 address,
  optionally in one pair of square brackets; the server connects to the
  address in place of resolving `host`.

The server checks both `host` and `hostaddr` against the
`connection_security` blocklist, allowlist and internal network rules,
so a permitted host name cannot stand in front of a blocked address.
The server and the collector also refuse a stored connection whose
`host` contains one of the characters above or whose `hostaddr` is not
a literal IP address.

When the server connects, the server applies the same rules to each
address that the target resolves to at that moment and connects only to
the addresses that pass, which defeats DNS rebinding. At save time an
allow-listed address can permit the whole host name; at connect time an
allow-listed address permits only that address. The collector has none
of the `connection_security` host settings; when the collector connects,
the collector refuses only unspecified, link-local and cloud metadata
addresses, and allows loopback and private addresses. The collector
therefore refuses a link-local target even when the server's
`allow_internal_networks` option permits it. See
[Connection Address Checks](../getting-started/configuration/collector.md#connection-address-checks)
for the collector's list.

The server and the collector read the files that the `ssl_cert_path`,
`ssl_key_path` and `ssl_root_cert_path` fields name themselves, so each
component only accepts paths inside the directory that its own
`connection_security.ssl_cert_dir` option names. Each path must be
absolute, must contain no `..` element or control character, and must
resolve, following symbolic links, to a location inside that directory.
When the option is unset, which is the default, each component refuses
every SSL file path; a `ssl_root_cert_path` of `system`, which selects
the platform trust store, is always allowed. An error from the server
names the refused field and the broken rule, or the option to set when
the option is unset, but never quotes the path. The collector does not
monitor a connection that its own check refuses, and records an error
ending in `invalid connection parameters` against the connection.

Set the option on both components, keep the files at the same paths on
the server and collector hosts, and make each directory and its
contents writable only by root or the service account. See
[Connection Security](../getting-started/configuration/server.md#connection-security-connection_security)
for the server option, and
[Connection Security Options](../getting-started/configuration/collector.md#connection-security-options)
for the collector option.

## REST API Endpoints

All endpoints require authentication via Bearer token.

### List Connections

The following endpoint lists all available database
connections from the datastore:

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

The following endpoint lists all databases on a
specific connection:

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

The following endpoint returns the currently selected
connection for the session:

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

The following endpoint selects a connection and
optionally a specific database:

```
POST /api/v1/connections/current
Authorization: Bearer <token>
Content-Type: application/json

{
    "connection_id": 1,
    "database_name": "analytics"
}
```

The `database_name` is optional. If not specified, the
connection's default database is used.

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

The following endpoint clears the current connection
selection:

```
DELETE /api/v1/connections/current
Authorization: Bearer <token>
```

Response: `204 No Content`

### Delete a Connection

The following endpoint deletes a connection; the caller
must own the connection or hold the `manage_connections`
permission, and must be able to see the connection. A
connection the caller cannot see answers `404 Not Found`,
exactly as a missing one does, even to a holder of
`manage_connections`; updating a connection follows the
same rule:

```
DELETE /api/v1/connections/{id}
Authorization: Bearer <token>
```

Response: `204 No Content`

The server answers as soon as the connection is marked
deleted. From that moment the connection disappears from
every listing, the API and the MCP tools, and the
collector and alerter stop monitoring it. The server then
removes the connection's collected metrics in the
background, in small batches, and removes the connection
itself once the metrics are gone. A connection with a long
metrics history may take some time to clear from the
datastore, but the delete request does not wait for it.

The background purge runs when the server starts, every
five minutes, and immediately after each delete; a server
restart resumes any purge that was interrupted.

On a datastore that stores metrics in pgEdge ColdFront,
the purge deletes the connection's rows from each metrics
view in a separate transaction before removing the
connection. When a view delete fails, the purge keeps the
connection and retries it on a later run. The
[Storing Metrics in pgEdge ColdFront](../getting-started/configuration/coldfront-metrics.md)
document describes that mode.

## Session Persistence

Connection selections are stored in the authentication
database (`auth.db`) and persist across requests and
server restarts. Each token has an independent
connection selection.

## Tool Behavior

When a connection is selected, all database tools
(`query_database`, `get_schema_info`,
`similarity_search`, `execute_explain`, `count_rows`)
operate on the selected database.

If no connection is selected, the tools return an error
message instructing the user to select a connection:

```
No database connection selected. Please select a
database connection using the web client.
```

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

In the following example, an error response uses the
standard JSON format:

```json
{
    "error": "Description of the error"
}
```

## Related Documentation

- [Managing Users and Permissions](managing-users-and-permissions/permission_model.md) covers tokens and
  access control.
- [API Reference](api/reference.md) provides
  interactive API documentation.
- [Monitored Database Privileges](../getting-started/configuration/monitored-database-privileges.md)
  describes the PostgreSQL grants a monitored role
  needs.
