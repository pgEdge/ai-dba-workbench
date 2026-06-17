# Role-Based Access Control (RBAC)

The Workbench RBAC system controls:

* which database connections a user can reach.
* which MCP tools a user can invoke.
* which administrative operations a user can perform. 

The system builds this control from three layered pieces that work together.

Groups are named collections of users and service accounts; the system
assigns permissions to groups rather than to individuals. Groups can be
nested, and a nested group inherits the parent group's privileges. When added
to a group, a user account inherits the privileges assigned to the group.

Privileges come in two kinds: connection privileges grant access to a
monitored database connection at a specified level, while MCP
privileges grant access to specific tools such as `query_database`.

Token scopes add an optional further restriction on an individual
token; a scope narrows what that token can do within the privileges
that its group already allows.

Superusers bypass all permission checks. Administrators manage RBAC
through the Administration console or the command line.

## Groups

Groups organize users and assign shared permissions. Each group can
contain users, service accounts, or other groups. Nested groups
inherit the parent group's privileges.

Administrators assign two types of privileges to groups:

- Connection privileges grant access to monitored database connections
  with a specified access level.
- MCP privileges grant access to specific MCP tools such as
  `query_database` or `get_schema_info`.

Privileges define what a group's members may reach within the
Workbench. The system recognizes two kinds of privileges; connection
privileges control database access, while MCP privileges control tool
access. Administrators assign privileges to groups rather than to
individual users; a user gains a privilege by joining a group that
holds it.

Connection privileges grant a group access to a specific monitored
database connection at a chosen access level. The system supports two
access levels:

- The `read` access level allows read-only operations against the
  connection; the group may inspect data and metadata without
  changing them.
- The `read_write` access level allows both read and write operations
  against the connection.

MCP privileges grant a group access to named MCP tools. Administrators
grant individual tool names such as `query_database` or
`get_schema_info`, rather than broad categories; this keeps each grant
explicit and auditable.

Administrators manage privileges through the Administration console or
the command line. The `-grant-connection` and `-revoke-connection`
flags assign or remove connection access, while `-access-level` sets
the level for a connection grant. The `-grant-privilege` and
`-revoke-privilege` flags assign or remove MCP tool access for a
group.

## Token Scopes

Token scopes apply an optional restriction to an individual token;
a scope narrows what the token can do within the privileges its
owner's groups already allow. A scope never expands access beyond
those group privileges; it only restricts them further. When no scope
is set, the token inherits the full set of privileges from the owner's
groups.

A scope can restrict a token to specific connections, specific MCP
tools, or both. A connection scope limits the token to a chosen set of
monitored connections, while a tool scope limits the token to a chosen
set of MCP tools. The system applies each part of the scope
independently; an unrestricted part inherits the owner's full group
privileges for that category.

Administrators manage token scopes through the command line. The
following flags control token scopes:

- The `-scope-token-connections` flag sets the connection scope for a
  token; pass connection IDs with `-scope-connections` as a
  comma-separated list.
- The `-scope-token-tools` flag sets the MCP tool scope for a token;
  pass tool names with `-scope-tools` as a comma-separated list.
- The `-show-token-scope` flag displays the current scope for a token,
  including its connection and MCP restrictions.
- The `-clear-token-scope` flag removes all scope restrictions from a
  token, restoring the owner's full group privileges.

Each scope command identifies the target token with the `-token-id`
flag.

## Administrative Permissions

Administrative permissions control access to management operations in the
Administration console and REST API. The Workbench defines the following
ten administrative permissions:

| Permission                   | Description                                                             |
|------------------------------|-------------------------------------------------------------------------|
| `manage_connections`         | Allows creating, editing, and deleting monitored database connections.  |
| `manage_groups`              | Allows creating, editing, and deleting RBAC groups and their memberships. |
| `manage_permissions`         | Allows granting and revoking privileges on groups.                      |
| `manage_users`               | Allows creating, editing, and deleting user accounts and service accounts. |
| `manage_token_scopes`        | Allows viewing and modifying token scope restrictions.                  |
| `manage_blackouts`           | Allows creating, editing, and deleting maintenance blackout windows.    |
| `manage_probes`              | Allows configuring probe frequency, retention, and enabled state.       |
| `manage_alert_rules`         | Allows configuring alert rule defaults and per-connection overrides.    |
| `manage_notification_channels` | Allows creating, editing, and deleting alert notification channels.   |
| `store_system_memory`        | Allows storing and deleting system-scoped chat memories visible to all users. |

Superusers bypass all permission checks and have full access to every
operation.
