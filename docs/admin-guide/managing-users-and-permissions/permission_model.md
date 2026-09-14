# Understanding the Workbench Permission Model

The Workbench permission model controls what each user account, service
account, and token can do. The model layers several pieces that work together
to grant or restrict access.

The permission model controls three kinds of access:

- The model controls which database connections an account can reach.
- The model controls which MCP tools an account can invoke.
- The model controls which administrative operations an account can perform.

The Workbench builds this access from layered pieces using groups, privileges,
permissions, and token scopes to manage the layers. Groups collect users and
assign shared privileges; privileges define connection and tool access; token
scopes narrow what an individual token may do. Administrators manage these
pieces with the Administration console or the command line.

!!! hint

    Superusers bypass every permission check and hold full access to all
    operations.

## How the Pieces Interact

The Workbench evaluates a request by combining the layered pieces in order. An
account first inherits privileges from every group it belongs to, including
the privileges of any nested groups. A token then applies its scope, narrowing
those inherited privileges to the scoped subset.

The following sequence describes how the Workbench resolves access for a
request:

1. The Workbench identifies the account that owns the request token.

2. The Workbench collects the privileges from every group the account belongs
   to, including nested groups.

3. The Workbench applies the token scope, restricting the request to the
   scoped subset of those privileges.

4. The Workbench grants the request only when the resulting permissions allow
   the requested operation.

A superuser short-circuits this sequence; the Workbench grants every request
from a superuser account regardless of group or scope.

## Accounts and Roles

Permissions apply to two kinds of accounts.

- A user account authenticates with a username and password to receive a
  session token.
- A service account provides programmatic access and authenticates only with
  an API token.

Both account types gain privileges in the same way; each joins groups that
hold the privileges the account needs.

A superuser holds a special role that bypasses all permission checks. This
allows a superuser to reach every connection, invoke every MCP tool, and
perform every administrative operation. An administrator may grant the
superuser role when they create or edit an account. For details on creating
and managing accounts, see [Account Management](accounts.md).

## Groups and Privileges

Groups organize accounts and assign shared permissions. The Workbench assigns
privileges to groups rather than to individual accounts; an account gains a
privilege by joining a group that holds the privilege.

Each group can contain users, service accounts, or other groups. Groups can be
nested, and a nested group inherits the parent group's privileges. This
approach keeps permission management consistent and auditable.

The Workbench recognizes three kinds of privileges that administrators assign
to groups:

- CONNECTION privileges grant access to a specified database connection at a
  chosen access level.
- ADMIN privileges grant permission to perform administrative actions.
- MCP privileges grant access to specific MCP tools such as `query_database`
  or `get_schema_info`.

A connection privilege grants a group access to one monitored connection at a
specified access level. The Workbench supports two connection access levels:

- The `read` access level allows read-only operations against the connection;
  the group inspects data and metadata without changing them.
- The `read_write` access level allows both read and write operations against
  the connection.

An ADMIN privilege grants a group permission to perform administrative actions
in the Workbench, such as managing users, groups, and connections. Admin
privileges are broad by nature and should be granted only to trusted groups.

An MCP privilege grants a group access to named MCP tools. Administrators
grant individual tool names rather than broad categories; this keeps each
grant explicit and auditable.

Administrators manage group privileges through the `Administration` console's
[`Permissions`](permission_mgmt.md) page or the command line. The following
flags assign and remove privileges for a group:

- The `-grant-connection` and `-revoke-connection` flags assign or remove
  connection access for a group.
- The `-access-level` flag sets the access level for a connection grant.
- The `-grant-privilege` and `-revoke-privilege` flags assign or remove ADMIN
  privileges or MCP tool access for a group.

For detailed information about creating groups and managing their membership,
see [Group Management](groups.md).

## Token Scopes

A token carries the permissions of the account that owns it. A token's
effective access can never exceed the owner's access; the Workbench always
enforces the more restrictive level. An optional token scope narrows what the
token can do within the owner's permissions.

A token scope restricts the token without expanding its access. When a token
has no scope, the token inherits the full set of the owner's group privileges.
The Workbench applies each part of a scope independently; an unrestricted part
inherits the owner's full group privileges for that category. The Workbench
supports three scope types:

- A connection scope limits the token to specific database connections, each
  at a `read` or `read_write` access level.
- An ADMIN permission scope limits the token to specific administrative
  operations.
- An MCP privilege scope limits the token to specific MCP tools.

Each scope type supports a wildcard that grants access to all items of that
type the owner holds. A connection scope can lower a token to `read` even when
the owner holds `read_write` access through a group.

!!! hint

    The effective access for a scoped token equals the intersection of the
    owner's group access and the token scope.

Administrators [manage token scopes](tokens.md) with the `Administration`
console or at the command line. The following flags control token scopes:

- The `-scope-token-connections` flag sets the connection scope for a token;
  pass connection IDs with `-scope-connections` as a comma-separated list.
- The `-scope-token-tools` flag sets the MCP tool scope for a token; pass
  tool names with `-scope-tools` as a comma-separated list.
- The `-show-token-scope` flag displays the current scope for a token,
  including its connection and MCP restrictions.
- The `-clear-token-scope` flag removes all scope restrictions from a token,
  restoring the owner's full group privileges.

Each scope command identifies the target token with the `-token-id` flag. For
details on creating tokens and setting their scopes, see
[Token Management](tokens.md).

## Administrative Permissions

Administrative (or `ADMIN`) permissions control access to management
operations in the Workbench's `Administration` console and the REST API.
Privileged users assign these permissions through groups, alongside connection
and MCP privileges. A superuser bypasses these checks and holds every
administrative permission automatically.

The Workbench defines the following ten ADMIN permissions:

| Permission | Description |
|------------|-------------|
| `manage_connections` | Allows creating, editing, and deleting monitored database connections. |
| `manage_groups` | Allows creating, editing, and deleting groups and their memberships. |
| `manage_permissions` | Allows granting and revoking privileges on groups. |
| `manage_users` | Allows creating, editing, and deleting user accounts and service accounts. |
| `manage_token_scopes` | Allows viewing and modifying token scope restrictions. |
| `manage_blackouts` | Allows creating, editing, and deleting maintenance blackout windows. |
| `manage_probes` | Allows configuring probe frequency, retention, and enabled state. |
| `manage_alert_rules` | Allows configuring alert rule defaults and per-connection overrides. |
| `manage_notification_channels` | Allows creating, editing, and deleting alert notification channels. |
| `store_system_memory` | Allows storing and deleting system-scoped chat memories visible to all users. |

Each permission allows a specific class of management operation; for example,
`manage_users` allows creating, editing, and deleting accounts.

For detailed instructions about assigning permissions to a group, see
[Permission Management](permission_mgmt.md).

## Related Pages

The following pages describe how to manage each part of the Workbench
permission model:

- The [Account Management](accounts.md) page describes how to
  create and manage user and service accounts.
- The [Group Management](groups.md) page describes how to create groups and
  manage their membership.
- The [Token Management](tokens.md) page describes how to create tokens and
  define their scopes.
- The [Permission Management](permission_mgmt.md) page describes how to
  assign and revoke permissions.
