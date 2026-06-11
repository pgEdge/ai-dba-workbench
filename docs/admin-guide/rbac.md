# Role-Based Access Control (RBAC)

The server enforces access control through groups, privileges, and
token scopes. Administrators manage these settings through the admin
panel or the REST API.


## Groups

Groups organize users and assign shared permissions. Each group can
contain users, service accounts, or other groups. Nested groups
inherit the parent group's privileges.

Administrators assign two types of privileges to groups:

- Connection privileges grant access to monitored database connections
  with a specified access level.
- MCP privileges grant access to specific MCP tools such as
  `query_database` or `get_schema_info`.

## Administrative Permissions

Admin permissions control access to management operations in the
Administration console and REST API. The Workbench defines the following ten administrative
permissions:

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
