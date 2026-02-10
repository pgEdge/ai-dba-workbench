# Alert Override Edit Button on Alert Instances

## Overview

This plan adds a button alongside the AI analysis button on
each alert instance. The button opens a dialog for editing
alert overrides. The dialog includes a scope dropdown at the
top that respects the existing override hierarchy.

## Current State

The current implementation includes the following components:

- Alert instances render in `AlertItem.tsx` for single alerts
  and `GroupedAlertItem.tsx` for grouped instances.
- The AI analysis button uses a `Psychology` icon at
  `AlertItem.tsx:207-219` and `GroupedAlertItem.tsx:151-163`.
- The override edit dialog exists in
  `AlertOverridesPanel.tsx:402-475` with fields for an Enabled
  toggle, Operator select, Threshold input, and Severity
  select.
- Override scopes resolve in the following order: server,
  cluster, group, and then estate defaults.
- `TransformedAlert` does not include `rule_id`; the backend
  `Alert` struct does have the field.
- The `selection` object does not carry `cluster_id` or
  `group_id` for server-level selections.
- No API endpoint returns all applicable overrides across
  scopes for a given connection and rule combination.

## Scope Dropdown Behavior

The scope dropdown determines where the override saves.

The following table describes the dropdown rules:

| Highest Existing Override | Available Scope Options         | Default |
|---------------------------|---------------------------------|---------|
| None (estate defaults)    | Group, Cluster, Server          | Server  |
| Group override            | Group, Cluster, Server          | Group   |
| Cluster override          | Group (disabled), Cluster,      | Cluster |
|                           | Server                          |         |
| Server override           | Group (disabled), Cluster       | Server  |
|                           | (disabled), Server              |         |

Estate is always absent from the dropdown. Estate defaults
come from `alert_rules` and the admin panel manages those
defaults.

### Disabled Scope Logic

Any scope above the highest existing override is disabled.
Creating a more general override has no effect while a more
specific override exists. The user can select the existing
scope or any lower (more specific) scope.

### Pre-population

When the user selects a scope, the form fields populate
according to the following rules:

- If an override exists at the selected scope, the form
  displays those override values.
- If no override exists at the selected scope, the form
  displays the effective inherited values from the next
  override up or the estate defaults.

### Saving

When the user saves to a scope with an existing override,
the system updates the override. When the user saves to a
scope without an override, the system creates a new override
at that scope.

## Required Backend Changes

The backend requires a new API endpoint and changes to the
alert response structure.

### Expose rule_id on Alert API Responses

The `Alert` struct already has `RuleID *int64`. The
`GET /api/v1/alerts` response should include this field. The
transform step should add the field if the response omits it.

### New API Endpoint: Override Context

The following endpoint returns override context for a given
connection and rule combination:

```
GET /api/v1/alert-overrides/context/{connectionId}/{ruleId}
```

The following JSON shows the response structure:

```json
{
    "hierarchy": {
        "connection_id": 5,
        "cluster_id": 2,
        "group_id": 1
    },
    "rule": {
        "rule_id": 12,
        "name": "High CPU Usage",
        "default_operator": ">",
        "default_threshold": 80,
        "default_severity": "warning",
        "default_enabled": true
    },
    "overrides": {
        "server": null,
        "cluster": null,
        "group": {
            "operator": ">",
            "threshold": 75,
            "severity": "critical",
            "enabled": true
        }
    }
}
```

The endpoint performs the following operations:

- The handler joins `connections` to `clusters` to
  `cluster_groups` for hierarchy IDs.
- The handler queries `alert_thresholds` for overrides at all
  three scopes.
- The handler queries `alert_rules` for defaults.
- The response returns `null` for `cluster_id` or `group_id`
  when the server is standalone or ungrouped.

### Implementation

A new handler in `alert_override_handlers.go` performs the
following steps:

- The handler joins connections, clusters, and cluster_groups
  to get the hierarchy.
- The handler queries `alert_thresholds` for the rule at each
  scope.
- The handler queries `alert_rules` for defaults.
- The handler returns the combined response.

A new query in `config_queries.go` supports the handler:

- `GetOverrideContext(ctx, connectionID, ruleID)` performs the
  joins described above.

## Required Frontend Changes

The frontend requires changes to types, components, and prop
threading.

### Add ruleId to TransformedAlert

In `types.ts`, add `ruleId?: number` to `TransformedAlert`.

In `StatusPanel/index.tsx`, add `ruleId: alert.rule_id` to
the `transformAlerts` mapping.

### New Component: AlertOverrideEditDialog

The new component lives at the following location:

```
client/src/components/AlertOverrideEditDialog.tsx
```

The following TypeScript shows the component props:

```typescript
interface AlertOverrideEditDialogProps {
    open: boolean;
    alert: TransformedAlert | null;
    onClose: () => void;
    isDark: boolean;
}
```

#### Behavior on Open

The dialog performs the following steps when opened:

1. Call the override context endpoint with the connection ID
   and rule ID.
2. Parse the response to determine available scopes, existing
   overrides, and which scopes to enable or disable.
3. Default the scope dropdown to the highest existing override
   scope; default to "server" if none exist.
4. Populate form fields from the selected scope override or
   from effective inherited values.

#### UI Layout

The following diagram shows the dialog layout from top to
bottom:

```
+------------------------------------------+
| Edit Alert Override: {alert.title}        |
+------------------------------------------+
| Scope: [  Server  v ]                    |
|                                           |
| (info banner if override exists at        |
|  higher scope explaining inheritance)     |
|                                           |
| Enabled        [toggle]                   |
| Operator       [ > v  ]                  |
| Threshold      [ 80   ]                  |
| Severity       [ warning v ]             |
+------------------------------------------+
|                     [Cancel]  [Save]      |
+------------------------------------------+
```

#### Scope Dropdown Items

The scope dropdown items follow these rules:

- Each item shows the scope name and target, such as "Server:
  pg-primary-01", "Cluster: us-east", or "Group: production".
- Disabled items show a tooltip explaining the reason.
- The dropdown omits a scope when `cluster_id` or `group_id`
  is null.
- The dropdown always omits estate.

#### Scope Change Behavior

When the user changes the scope, the dialog follows these
rules:

- If the selected scope has an override, the form populates
  with those values.
- If the selected scope lacks an override, the form populates
  with the effective inherited values.

#### Save Behavior

On save, the dialog calls the following endpoint:

```
PUT /api/v1/alert-overrides/{scope}/{scopeId}/{ruleId}
```

The `scopeId` comes from the context response:
`connection_id` for server, `cluster_id` for cluster, and
`group_id` for group.

### Add Button to AlertItem and GroupedAlertInstance

In `AlertItem.tsx`, insert a new `IconButton` between the AI
analysis button and the ack/unack button. The button uses the
`TuneRounded` icon.

In `GroupedAlertItem.tsx`, add the same button in
`GroupedAlertInstance`.

The button follows these rules:

- The button disables when `alert.ruleId` is undefined.
- The button shows the tooltip "Edit alert override".
- The button calls `onEditOverride?.(alert)` on click.

### Thread the Callback

Add `onEditOverride?: (alert: TransformedAlert) => void` to
the following interfaces:

- `AlertItemProps`
- `GroupedAlertInstanceProps`
- `GroupedAlertItemProps`
- `AlertsSectionProps`

Pass the callback from `StatusPanel` through `AlertsSection`
to `AlertItem` and `GroupedAlertItem` to
`GroupedAlertInstance`.

In `StatusPanel/index.tsx`, add the following:

- State variables: `overrideDialogOpen` and `overrideAlert`.
- Handler function: `handleEditOverride`.
- Pass the handler to `AlertsSection` as `onEditOverride`.
- Render `AlertOverrideEditDialog` alongside the other
  dialogs.

## Edge Cases

The implementation must handle the following edge cases:

- Some alerts lack a `rule_id` because the alerts are
  anomaly-based. The button disables for these alerts.
- A standalone server without a cluster shows only "Server"
  in the scope dropdown.
- A server in a cluster but without a group shows "Server"
  and "Cluster" in the scope dropdown.
- When overrides exist at both group and server levels, the
  highest override (group) determines the disable boundary.
- Creating a cluster override when a group override exists
  works correctly. The cluster override takes precedence for
  servers in that cluster.
- The save operation uses PUT as an upsert; the operation
  handles both create and update.

## Files to Change

The following table lists all files that require changes:

| File                                     | Change       |
|------------------------------------------|--------------|
| `server/src/internal/api/`               | New handler  |
| `alert_override_handlers.go`             |              |
| `server/src/internal/database/`          | New query    |
| `config_queries.go`                      |              |
| `server/src/internal/api/routes.go`      | Register     |
|                                          | route        |
| `client/src/components/StatusPanel/`     | Add ruleId;  |
| `types.ts`                               | add callback |
| `client/src/components/StatusPanel/`     | Add ruleId   |
| `index.tsx`                              | transform;   |
|                                          | state;       |
|                                          | dialog       |
| `client/src/components/StatusPanel/`     | Add button   |
| `AlertItem.tsx`                          |              |
| `client/src/components/StatusPanel/`     | Add button   |
| `GroupedAlertItem.tsx`                    |              |
| `client/src/components/StatusPanel/`     | Pass prop    |
| `AlertsSection.tsx`                      |              |
| `client/src/components/`                 | New dialog   |
| `AlertOverrideEditDialog.tsx`            |              |
