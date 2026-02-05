# Blackout Management

Blackouts suppress alert notifications during planned
maintenance windows. Administrators create blackouts to
prevent false alerts while performing database upgrades,
schema changes, or infrastructure work. The AI DBA
Workbench supports both one-time blackouts and recurring
schedules.

## Scopes

Blackouts operate at four hierarchical levels. A blackout
at any level suppresses alerts for everything beneath the
blackout in the hierarchy.

The following table describes the available scopes:

| Scope | Target | Description |
|-------|--------|-------------|
| Estate | Entire installation | Suppresses alerts for all servers across all groups and clusters. |
| Group | Cluster group | Suppresses alerts for all clusters and servers within the group. |
| Cluster | Single cluster | Suppresses alerts for all servers within the cluster. |
| Server | Individual server | Suppresses alerts for only the specified server. |

The system enforces scope validation when creating or
updating a blackout. Estate blackouts must not reference
any group, cluster, or server. Group blackouts require a
`group_id` value. Cluster blackouts require a
`cluster_id` value. Server blackouts require a
`connection_id` value.

## One-Time Blackouts

Administrators create one-time blackouts through the
admin panel or the REST API. The admin panel offers two
timing modes for scheduling a blackout.

The Start Now mode begins the blackout immediately. The
administrator selects a duration preset or enters a custom
duration. The following duration presets are available:

- A 30-minute window covers brief maintenance tasks.
- A 1-hour window covers standard maintenance tasks.
- A 2-hour window covers extended maintenance tasks.
- A 4-hour window covers major upgrade procedures.
- An 8-hour window covers full migration operations.

The Schedule Future mode activates the blackout at a
specified start time. The administrator picks a start time
and an end time for the maintenance window.

The following table describes the fields for a one-time
blackout:

| Field | Required | Description |
|-------|----------|-------------|
| Scope | Yes | The level at which to suppress alerts. |
| Group / Cluster / Server | Conditional | The target entity; depends on the selected scope. |
| Reason | Yes | A description of why the blackout is needed. |
| Start Time | Yes | When the blackout begins (RFC 3339 format in the API). |
| End Time | Yes | When the blackout ends; must be after the start time. |

Administrators can stop active blackouts early using the
Stop button in the admin panel. The
`POST /api/v1/blackouts/{id}/stop` endpoint provides the
same capability through the REST API. Stopping a blackout
sets the end time to the current time.

## Recurring Schedules

Recurring schedules automatically create blackouts at
regular intervals. The alerter evaluates enabled schedules
every minute and creates blackout entries when cron
expressions match the current time.

The following table describes the fields for a recurring
schedule:

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| Name | Yes | -- | A human-readable name for the schedule. |
| Scope | Yes | -- | The level at which to suppress alerts. |
| Cron Expression | Yes | -- | A standard five-field cron expression. |
| Duration | Yes | -- | How long each blackout lasts, in minutes. |
| Timezone | No | UTC | The IANA timezone for cron evaluation. |
| Reason | Yes | -- | The reason recorded on each created blackout. |
| Enabled | No | true | Whether the schedule is active. |

### Cron Format

A cron expression consists of five fields that represent
minute, hour, day of month, month, and day of week. The
following examples demonstrate common scheduling patterns:

- `0 2 * * *` triggers daily at 2:00 AM.
- `0 3 * * 1-5` triggers on weekdays at 3:00 AM.
- `0 4 * * 0,6` triggers on weekends at 4:00 AM.
- `0 1 * * 0` triggers weekly on Sunday at 1:00 AM.
- `0 0 1 * *` triggers monthly on the first day at
  midnight.

For detailed syntax documentation, see
[Cron Expressions](../alerter/cron-expressions.md).

## Navigator Indicators

The Cluster Navigator displays blackout status on affected
nodes in the navigation tree. An amber pause icon appears
on servers, clusters, and groups that have an active
blackout.

The icon appears at full opacity for direct blackouts. The
icon appears at reduced opacity for inherited blackouts.
Hovering over the icon shows whether the blackout applies
directly or through inheritance from a parent scope.

## Alert Suppression

The alerter checks for active blackouts before firing any
alert. The following steps describe the suppression
process:

1. The alerter identifies the target server for the alert.
2. The alerter checks for an active blackout at the server
   scope.
3. The alerter walks up the hierarchy through cluster,
   group, and estate scopes.
4. If any active blackout matches at any level, the
   alerter suppresses the alert.
5. Suppressed alerts do not fire and do not generate
   notifications.
6. When the blackout ends, normal alert evaluation
   resumes.

## Permissions

The `manage_blackouts` permission controls access to
blackout management operations. Administrators with this
permission can create, update, delete, and stop blackouts.
All authenticated users can view active blackouts
regardless of their permissions.

## REST API

The blackout REST API provides endpoints for managing
one-time blackouts and recurring schedules. The following
table lists the available endpoints:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/blackouts` | List blackouts with optional filtering. |
| `POST` | `/api/v1/blackouts` | Create a blackout. |
| `GET` | `/api/v1/blackouts/{id}` | Get a specific blackout. |
| `PUT` | `/api/v1/blackouts/{id}` | Update a blackout. |
| `DELETE` | `/api/v1/blackouts/{id}` | Delete a blackout. |
| `POST` | `/api/v1/blackouts/{id}/stop` | Stop an active blackout immediately. |
| `GET` | `/api/v1/blackout-schedules` | List recurring schedules. |
| `POST` | `/api/v1/blackout-schedules` | Create a recurring schedule. |
| `GET` | `/api/v1/blackout-schedules/{id}` | Get a specific schedule. |
| `PUT` | `/api/v1/blackout-schedules/{id}` | Update a schedule. |
| `DELETE` | `/api/v1/blackout-schedules/{id}` | Delete a schedule. |

The list endpoints support query parameters for filtering
results. The following parameters are available for the
`GET /api/v1/blackouts` endpoint:

- The `scope` parameter filters by blackout scope level.
- The `group_id` parameter filters by cluster group.
- The `cluster_id` parameter filters by cluster.
- The `connection_id` parameter filters by server.
- The `active` parameter filters by active status.
