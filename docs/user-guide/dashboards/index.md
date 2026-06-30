# Dashboards

The monitoring dashboards provide a hierarchical view of PostgreSQL database
health and performance. You can navigate through five levels of detail, from a
fleet-wide estate overview down to individual database objects.

## Dashboard Hierarchy

The dashboard system organizes metrics into five levels that progress from
broad to specific. Select items in the cluster navigator or click drillable
elements within each dashboard to move between levels. The cluster navigator
tree in the Workbench's left pane reflects the estate, cluster, server, and
database hierarchy.

- The [estate dashboard](estate.md) shows fleet-wide health across all
  monitored servers.
- The [cluster dashboard](cluster.md) focuses on replication topology and
  comparative metrics across cluster members.
- The [server dashboard](server.md) displays system resources and PostgreSQL
  performance for a single server.
- The [database dashboard](database.md) presents table and index leaderboards
  with vacuum status for one database.
- The [object dashboard](object.md) provides detailed metrics for a specific
  table, index, or query.

### Selecting Time Ranges

The time range selector in the `Event Timeline` pane specifies the length of
time for which events are displayed. A similar time range selector also
controls the time window for the charts displayed in the `Monitoring` section.

![The time range selector in the Event Timeline](../images/time_range_selector.png)

The selector is displayed as a toggle button group with the following options:

- 1h displays the last one hour of data.
- 6h displays the last six hours of data.
- 24h displays the last twenty-four hours of data.
- 7d displays the last seven days of data.
- 30d displays the last thirty days of data.

The selected time range persists across dashboard navigation. All time-series
charts and KPI sparklines update when you change the time range.

### Using the Event Timeline

The event timeline displays monitored events across the selected servers. The
timeline appears above the performance summary tiles in the monitoring section.

![The time range selector in the Event Timeline](../images/event_timeline.png)

You can use the event timeline to track the following event types by selecting
from the colored buttons between the `Event Timeline` label and the time range
selector:

| Button | Description |
|------------|------------------------------------------------------|
| Config | Shows configuration change events for PostgreSQL settings. |
| HBA | Shows changes to the host-based authentication file (`pg_hba.conf`). |
| Ident | Shows changes to the ident authentication configuration (`pg_ident.conf`). |
| Restart | Shows server restart and recovery events. |
| Alert | Shows active alert events that have been triggered. |
| Cleared | Shows alert events that have been resolved and cleared. |
| Acked | Shows alert events that have been acknowledged by an operator. |
| Extension | Shows extension installation and upgrade events. |
| Blackouts | Shows scheduled maintenance blackout periods. |

The event timeline refreshes in sync with the cluster navigator refresh cycle.
You can filter events by server and event type. When multiple events occur at
the same point in time, the timeline stacks them into a single icon with a
badge showing the event count; hovering over the icon displays a tooltip that
lists individual event names and a "+N more" indicator when the group contains
more than three events.

![Reviewing Event Timeline details](../images/event_timeline_details.png)

The performance summary tiles below the event timeline provide a quick glance
into the performance of your estate, selected server, or cluster.  Hover over
a chart or graph to review detailed information about a specific point in time
for the selected metric. 

![Reviewing graph details](../images/graph_details.png)


## Using the AI Overview

The AI Overview presents a concise, AI-generated summary of database
health at the top of the status panel. The summary describes server
health, active alerts that need attention, and any ongoing or upcoming
blackouts. When everything is healthy, the overview states so briefly.

The Workbench's AI features are available only when the server is
configured with a valid LLM provider. See
[Enabling AI Mode](../../getting-started/configuration/enable_ai_mode.md)
for configuration details.

The overview adapts to your current selection in the cluster navigator.
The summary reflects one of the following scopes:

- The estate scope summarizes health across all monitored servers.
- The cluster scope summarizes the servers that belong to one cluster.
- The server scope summarizes a single selected server.

A sparkle icon and the "AI Overview" label mark the panel. The body
displays the summary text and a relative timestamp, such as "Updated 5
min ago", that shows when the Workbench last generated the summary.

### Keeping the Overview Current

The server regenerates the overview when the estate state changes
significantly. The Workbench receives these updates in real time and
refreshes the panel without any action from you.

A "(stale)" badge appears next to the label when the current summary
has aged past its freshness window. Click the refresh icon beside the
timestamp to request a new summary immediately.

While the server prepares the first summary, the panel displays a
loading placeholder followed by a "Generating overview..." message.

### Collapsing and Expanding

Click the expand or collapse icon on the right of the header to hide or
show the summary body. The header remains visible when the panel is
collapsed. The Workbench remembers your choice across sessions.

### Running a Full Analysis

When you select a server or cluster, a brain icon appears in the
header. Click the brain icon to run a detailed analysis of that server
or cluster. An amber brain icon indicates that a cached analysis is
already available for the current selection.


## Using the AI Chart Analysis Feature

The AI chart analysis feature provides LLM-powered insights for any chart or
KPI tile in the monitoring dashboards. The analysis examines data trends,
identifies anomalies, and generates actionable recommendations.

The Workbench's AI features are available only when the server is
configured with a valid LLM provider. See
[Enabling AI Mode](../../getting-started/configuration/enable_ai_mode.md)
for configuration details.

Charts, KPI tiles, leaderboards, and the vacuum status section each display a
brain icon in the upper-right icon. Click the icon to open an analysis dialog
and start the LLM analysis.

The analysis follows these steps:

1. The Workbench checks for a cached analysis result.
2. The Workbench fetches server context from the connection.
3. The Workbench fetches timeline events for the time range.
4. The Workbench serializes the chart data and sends it to the LLM.
5. The LLM returns a structured analysis report.

The dialog displays a loading skeleton while the analysis runs. The final
report renders as formatted markdown.

### Analysis Reports

Each chart analysis report contains a structured assessment of the metric
data:

- The summary section describes the current state of the metric and its
  significance.
- The trends and patterns section identifies notable changes, spikes, or
  anomalies in the data.
- The recommendations section suggests specific actions to address any issues
  found.

### Timeline Event Correlation

The analysis includes timeline events from the chart's time range to identify
correlations between metric changes and system events. The LLM considers the
following event types:

- Configuration changes to PostgreSQL settings.
- Alert activations and resolutions.
- Server restarts and recovery events.
- Extension installations and upgrades.
- Blackout periods and maintenance windows.

### Running SQL Queries

SQL code blocks in analysis reports include a play button in the upper right
corner. Click the play button to execute the query against the chart's
associated database server. Results appear inline below the code block.

Write statements such as `ALTER SYSTEM` prompt a confirmation dialog before
executing. Read-only queries execute immediately.

### Caching

The Workbench caches chart analysis results on the client side to avoid
redundant LLM calls.

- An amber brain icon indicates that a cached analysis exists for the chart.
- The cache uses stable identifiers as the cache key; these include the metric
  description, connection, database, and time range.
- The cache expires after 30 minutes.
- Click an amber brain icon to open the cached report instantly.

### Downloading Reports

The dialog footer includes a Download button that saves the analysis as a
markdown file. The downloaded file includes the chart details, the full
analysis report, and a generation timestamp.
