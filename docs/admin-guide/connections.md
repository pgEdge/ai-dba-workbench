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
allow-listed address permits only that address. Before applying those
rules, the server refuses unspecified, link-local and cloud metadata
addresses, whatever `allowed_hosts` or `allow_internal_networks` say,
both at connect time and when a connection is saved. A request to create
or update a connection whose supplied `host` or `hostaddr` is such an
address, or whose supplied `host` is a name that resolves to one, fails
with a 400 response stating that these addresses are always blocked. A
host name that `allowed_hosts` lists is not resolved when the connection
is saved, so the server applies the check to that name's addresses only
at connect time. The collector has none of the `connection_security`
host settings; when the collector connects, the collector refuses only
the same unspecified, link-local and cloud metadata addresses, and
allows loopback and private addresses. See
[Connection Address Checks](../configuration/collector/configuration_file.md#checking-addresses-at-connect-time)
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
the server and collector hosts, and make each directory, its contents
and its parent directories writable only by root or the service
account. See
[Connection Security](../configuration/server/configuration_file.md#connection-security-properties)
for the server option, and
[Connection Security Options](../configuration/collector/configuration_file.md#specifying-connection-security-options)
for the collector option.

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

In the following example, an error response uses the
standard JSON format:

```json
{
    "error": "Description of the error"
}
```