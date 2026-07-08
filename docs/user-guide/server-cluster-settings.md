# Server and Cluster Settings

Server nodes and clusters in the cluster navigator each expose a settings
dialog. Both dialogs share a common structure: a gear icon opens the dialog, a
horizontal tab bar organizes the settings, and `Cancel` and `Save` buttons at
the bottom discard or retain your changes.

## Opening the Dialogs

Each server node in the cluster navigator displays a gear icon when you hover
over the node. Click the gear icon to open the `Server Settings` dialog for
that server. The dialog header reads `Server Settings: <server name>`, and a
close (`X`) icon in the header dismisses the dialog without saving changes.

Each cluster in the cluster navigator displays a gear icon when you hover over
the cluster name. Click the gear icon to open the `Cluster Settings` dialog for
that cluster. The dialog header reads `Cluster Settings: <cluster name>`, and a
close (`X`) icon dismisses the dialog without saving changes.

Both dialogs organize their settings into a horizontal tab bar; the Workbench
underlines and highlights the active tab.

## Server Settings: DETAILS Tab

The `DETAILS` tab presents a form that defines how the Workbench identifies and
connects to the server. The Name field is required and sets the display name
for the server. The Description field is a multi-line text area that holds
optional notes about the server.

The `CONNECTION DETAILS` subsection specifies how the collector reaches the
database. The subsection includes the following fields:

- The `Host`, `Port`, `Maintenance Database`, and `Username` fields specify the
  connection values for the selected server; all values are required.
- The `Password` is optional; leave the field blank to keep the stored password
  unchanged, or enter a new password to replace it.

A collapsible `SSL SETTINGS` section appears below the connection fields and
remains collapsed by default. Expand this section to configure encrypted
connection options.

The `OPTIONS` subsection includes two checkboxes:

- The `Monitor this server` checkbox controls whether the collector gathers
  metrics from the server.
- The `Share with all users` checkbox makes the server visible to every user of
  the Workbench.

![Reviewing the Details tab of Server Settings](../images/server_settings_details.png)

## Server Settings: CLUSTER Tab

The `CLUSTER` tab shows the server's current cluster assignment and role. The
tab displays the following fields:

- The `Cluster` field shows the name of the cluster the server belongs to, such
  as "traffic".
- The `Replication Type` field shows the replication technology used by the
  cluster, such as "Spock".
- The `Role` field shows the server's role within the cluster, or "Not
  assigned" when the server has no assigned role.
- The `Membership` field shows how the server joined the cluster, such as
  "Manual", alongside a badge that repeats the membership type.

Click the `Configure Cluster` button to open the cluster configuration dialog
and manage the server's cluster membership.

![Reviewing the Cluster tab of Server Settings](../images/server_settings_cluster.png)

## Cluster Settings: DETAILS Tab

The `DETAILS` tab presents a form that identifies the cluster and defines its
replication behavior. The tab includes the following fields:

- The `Name` field displays or modifies the display name for the cluster.
- The `Description` field is a multi-line text area that holds optional notes
  about the cluster.
- The `Replication Type` dropdown specifies the replication technology used for
  the cluster.

![Reviewing the Details tab of Cluster Settings](../images/cluster_settings_details.png)

## Cluster Settings: TOPOLOGY Tab

The `TOPOLOGY` tab presents a visual diagram of the cluster members and lets
you assign servers and define replication relationships. The diagram displays
each member node as a tile with a status dot and a role badge that identifies
the node's role in the cluster.

Use the `ADD SERVER` section to add a new node to the cluster:

- Use the `Server` dropdown to search for and select an unassigned server.
- Use the `Role` dropdown to set the role the server will hold in the cluster.
- Use the `+ Add` button to add the selected server to the cluster.

A list of currently assigned servers appears below the `ADD SERVER` section.
Server details display the server name, a role badge, its host and port, and a
`Delete` (trash) icon. Select the `Delete` icon to remove the server from the
cluster.

The `RELATIONSHIPS` section at the bottom of the tab shows the replication
relationships the topology diagram presents. Use the section's controls to
define a new relationship between two cluster members:

- Use the `Source` dropdown to select the source node for the relationship.
- Use the `Target` dropdown to select the target node for the relationship.
- Use the `Type` dropdown to select the replication type, such as "Replicates
  with (Spock)".
- Click `+ Add` to create the relationship between the selected nodes.

![Reviewing the Topology tab of Cluster Settings](../images/cluster_settings_topology.png)

## ALERT OVERRIDES Tab

The `ALERT OVERRIDES` tab appears identically in both the Server Settings and
Cluster Settings dialogs, scoped to the selected server or cluster. It lets you
tailor the alert rules for that scope. A table lists the current rules and
settings; each row describes one alert rule and its threshold. The table
contains the following columns:

- `Name` identifies the alert rule.
- `Metric` names the metric the rule monitors.
- `Condition` specifies the threshold that triggers the alert.
- `Severity` indicates the alert level, such as "warning".
- `Enabled` provides a toggle that activates or deactivates the rule for the
  selected server or cluster.
- `Actions` provides an edit (pencil) icon that opens the rule for adjustment.

The Workbench groups the rows under category headers such as `AVAILABILITY`,
`CONNECTIONS`, and `LOCKS`; additional categories appear as you scroll through
the table. See [Alert Rules](../admin-guide/alert-rules.md) for the full list
of built-in rules and their default thresholds.

![Reviewing the Alert Overrides tab of Server Settings](../images/server_settings_alert_overrides.png)

![Reviewing the Alert Overrides tab of Cluster Settings](../images/cluster_settings_alert_overrides.png)

## PROBE CONFIGURATION Tab

The `PROBE CONFIGURATION` tab appears identically in both the Server Settings
and Cluster Settings dialogs, scoped to the selected server or cluster. It
controls the probes that collect metrics for that scope. A table lists the
current probes and their settings; each row describes one probe and its
configuration. The table contains the following columns:

- `Name` identifies the probe.
- `Description` explains what the probe monitors.
- `Enabled` provides a toggle that activates or deactivates the probe for the
  selected server or cluster.
- `Interval` specifies how often the probe collects data, in seconds.
- `Retention` specifies how long the Workbench retains the probe's collected
  data.
- `Actions` provides an edit (pencil) icon that opens the probe for adjustment.

The table scrolls to reveal additional probes below the visible rows. See
[Probe Management](../admin-guide/probes.md) for the full list of built-in
probes and their scopes.

![Reviewing the Probe Configuration tab of Server Settings](../images/server_settings_probe_configuration.png)

![Reviewing the Probe Configuration tab of Cluster Settings](../images/cluster_settings_probe_configuration.png)

## NOTIFICATION CHANNELS Tab

The `NOTIFICATION CHANNELS` tab appears identically in both the Server Settings
and Cluster Settings dialogs, scoped to the selected server or cluster. It
manages the channels that deliver alert notifications for that scope. A table
lists the available channels and their settings; each row describes one channel
and its current override state. The table contains the following columns:

- `Name` identifies the notification channel.
- `Type` shows the channel type, such as email, Slack, Mattermost, or webhook.
- `Description` shows the channel's optional description.
- `Estate Default` indicates whether the channel applies to all servers or
  clusters by default.
- `Enabled` provides a toggle that activates or deactivates the channel for the
  selected server or cluster, overriding the estate default.
- `Actions` provides controls for managing the channel's override for the
  selected server or cluster.

If you have not configured any notification channels, the tab displays the
empty state "No notification channels found." See
[Notification Channels](../admin-guide/notification-channels.md) for the full
list of supported channel types and how to configure them.

![Reviewing the Notification Channels tab of Server Settings](../images/server_settings_notification_channels.png)

![Reviewing the Notification Channels tab of Cluster Settings](../images/cluster_settings_notification_channels.png)
