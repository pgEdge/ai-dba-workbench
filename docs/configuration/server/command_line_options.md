# Command-Line Options

The server accepts two kinds of command-line options. Startup options
configure the server process that you start: they set the configuration file,
logging, the data directory, the HTTP listener, and the datastore connection,
and they take priority over environment variables and the matching settings in
the configuration file. Administrative options run a single task against the
server's authentication store, such as creating a user or a token, and exit
without starting the server; pass the same `-config` flag that the running
server uses, so that the command opens the same data directory. The server
records each administrative change in the
[audit log](../../admin-guide/audit-log.md) under the `cli` actor.

The following sections group the options by function. The first three
sections list startup options, apart from the `-check-config` and
`-health-check` checks in General Options, which also exit without starting
the server; the remaining sections list administrative options:

- [General Options](#general-options)
- [HTTP Server Options](#http-server-options)
- [Database Connection Options](#database-connection-options)
- [Token Management Options](#token-management-options)
- [User Management Options](#user-management-options)
- [Group Management Options](#group-management-options)
- [Privilege Management Options](#privilege-management-options)
- [Token Scope Options](#token-scope-options)
- [Audit Log Options](#audit-log-options)

The token, user, group, privilege, and token scope flags are useful for
scripted setup and for creating the first administrator account. After you sign
in, the `Users`, `Groups`, `Permissions`, and `Tokens` tabs of the
Administration console provide the same management on screen; see
[Managing Accounts](../../admin-guide/managing-users-and-permissions/accounts.md).

## General Options

The general options set the configuration file path, debug logging, the data
directory, and the trace file.

| Flag | Description |
|------|-------------|
| `-config string` | Path to configuration file |
| `-check-config` | Validate the configuration file, then exit without starting the server; see [Upgrading the Workbench](../../admin-guide/upgrading.md#checking-a-configuration-by-hand) |
| `-debug` | Enable debug logging |
| `-data-dir string` | Data directory for auth and conversations |
| `-health-check host:port` | Check that a TCP listener accepts connections at the given address, then exit with status 0, or 1 if it does not; the example Docker Compose files use it as the server health check |
| `-trace-file string` | Path to MCP trace file |

## HTTP Server Options

The HTTP server options configure the listen address and TLS certificate files
for encrypted connections.

| Flag | Description |
|------|-------------|
| `-addr string` | HTTP address (default: `:8080`) |
| `-tls` | Enable TLS/HTTPS |
| `-cert string` | Path to TLS certificate file |
| `-key string` | Path to TLS key file |
| `-chain string` | Path to TLS certificate chain |

## Database Connection Options

The database connection options set the host, port, credentials, and SSL mode
for the PostgreSQL connection.

| Flag | Description |
|------|-------------|
| `-db-host string` | Database host |
| `-db-port int` | Database port |
| `-db-name string` | Database name |
| `-db-user string` | Database user |
| `-db-password string` | Database password |
| `-db-password-file string` | Path to DB password file, read at startup only; overrides the configured password |
| `-db-sslmode string` | SSL mode |

## Token Management Options

Use the token management options to create, remove, list, and annotate service
tokens.

| Flag | Description |
|------|-------------|
| `-add-token` | Add a new service token |
| `-remove-token string` | Remove token by ID or hash |
| `-list-tokens` | List all service tokens |
| `-token-note string` | Annotation for new token |
| `-token-expiry string` | Token expiry duration |
| `-user string` | Owner username for the token |

## User Management Options

The user management options add, update, delete, enable, and disable user and
service accounts.

| Flag | Description |
|------|-------------|
| `-add-user` | Add a new user |
| `-update-user` | Update an existing user |
| `-delete-user` | Delete a user |
| `-list-users` | List all users |
| `-enable-user` | Enable a user account |
| `-disable-user` | Disable a user account |
| `-add-service-account` | Add a service account |
| `-username string` | Username for user commands |
| `-password string` | Password for user commands |
| `-full-name string` | Full name for the user |
| `-email string` | Email address for the user |
| `-user-note string` | Annotation for new user |

## Group Management Options

The group management options create, delete, and list RBAC groups, and manage
group membership and superuser status.

| Flag | Description |
|------|-------------|
| `-add-group` | Add a new RBAC group |
| `-delete-group` | Delete an RBAC group |
| `-list-groups` | List all RBAC groups |
| `-list-members` | List the members of a group |
| `-add-member` | Add a user or group to a group |
| `-remove-member` | Remove a member from a group |
| `-group string` | Group name for group commands |
| `-member-group string` | Member group for nesting |
| `-set-superuser` | Set superuser status |
| `-unset-superuser` | Remove superuser status |

## Privilege Management Options

The privilege management options grant, revoke, register, and list MCP
privileges and connection access levels.

| Flag | Description |
|------|-------------|
| `-grant-privilege` | Grant an MCP privilege |
| `-revoke-privilege` | Revoke an MCP privilege |
| `-grant-connection` | Grant connection access |
| `-revoke-connection` | Revoke connection access |
| `-list-privileges` | List MCP privileges |
| `-show-group-privileges` | Show group privileges |
| `-register-privilege` | Register a new privilege |
| `-privilege string` | MCP privilege identifier |
| `-privilege-type string` | Privilege type |
| `-privilege-description string` | Description |
| `-connection int` | Connection ID |
| `-access-level string` | Access level (default: read) |

## Token Scope Options

The token scope options set, clear, and display the connections and tools a
service token can access.

| Flag | Description |
|------|-------------|
| `-scope-token-connections` | Set connection scope |
| `-scope-token-tools` | Set MCP tool scope |
| `-clear-token-scope` | Clear all scope restrictions |
| `-show-token-scope` | Show current token scope |
| `-token-id int` | Token ID for scope commands |
| `-scope-connections string` | Connection ID list |
| `-scope-tools string` | Tool name list |

## Audit Log Options

The audit log records every change to users, groups, tokens, and privileges;
the [Managing the Audit Log](../../admin-guide/audit-log.md)
page describes the events and how to read them. The audit log options read and
verify the log. The `-rechain-audit-log` option either re-hashes an inherited
log or re-anchors a log that no longer verifies; a re-anchor leaves the
existing events as they are and appends an anchor event.

| Flag | Description |
|------|-------------|
| `-list-audit` | List RBAC audit log events |
| `-verify-audit-log` | Verify the audit log hash chain |
| `-rechain-audit-log` | Re-hash an inherited audit log, or re-anchor one that no longer verifies |
| `-confirm-rechain` | Confirm `-rechain-audit-log` without an interactive prompt |
| `-previous-secret-file string` | Secret file the older audit events were written under, needed by `-confirm-rechain` to re-anchor them |
| `-audit-actor string` | Filter events by actor name |
| `-audit-action string` | Filter events by action |
| `-audit-target-type string` | Filter events by target type |
| `-audit-target-id int` | Filter events by target ID |
| `-audit-outcome string` | Filter events by outcome |
| `-audit-since string` | Earliest event time to show |
| `-audit-until string` | Latest event time to show |
| `-audit-limit int` | Maximum events to show (default: 50) |
| `-json` | Emit output as one JSON object per line |
