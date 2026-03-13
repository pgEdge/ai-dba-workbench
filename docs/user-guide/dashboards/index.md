# Dashboards

The monitoring dashboards provide a hierarchical view of
PostgreSQL database health and performance. Users navigate
through five levels of detail, from a fleet-wide estate
overview down to individual database objects.

## Dashboard Hierarchy

The dashboard system organizes metrics into five levels
that progress from broad to specific.

- The [estate dashboard](estate.md) shows fleet-wide
  health across all monitored servers.
- The [cluster dashboard](cluster.md) focuses on
  replication topology and comparative metrics across
  cluster members.
- The [server dashboard](server.md) displays system
  resources and PostgreSQL performance for a single
  server.
- The [database dashboard](database.md) presents table
  and index leaderboards with vacuum status for one
  database.
- The [object dashboard](object.md) provides detailed
  metrics for a specific table, index, or query.

## Navigation

Users navigate between dashboard levels by selecting
items in the cluster navigator or by clicking drillable
elements within each dashboard. The cluster navigator
tree reflects the estate, cluster, server, and database
hierarchy.

## Time Range Selector

The time range selector controls the time window for
all charts in the monitoring section. The selector
appears as a toggle button group with the following
options:

- 1h displays the last one hour of data.
- 6h displays the last six hours of data.
- 24h displays the last twenty-four hours of data.
- 7d displays the last seven days of data.
- 30d displays the last thirty days of data.

The selected time range persists across dashboard
navigation. All time-series charts and KPI sparklines
update when users change the time range.

## Event Timeline

The event timeline displays notable events across the
selected servers. The timeline appears above the
performance summary tiles in the monitoring section.

The event timeline tracks the following event types:

- Configuration changes to PostgreSQL settings.
- Alert activations and resolutions.
- Server restarts and recovery events.
- Extension installations and upgrades.
- Other system-level events.

The event timeline refreshes in sync with the cluster
navigator refresh cycle. Users can filter events by
server and event type.

## AI Chart Analysis

The AI chart analysis feature provides LLM-powered
insights for any chart or KPI tile in the monitoring
dashboards. The analysis examines data trends,
identifies anomalies, and generates actionable
recommendations.

### Triggering an Analysis

Charts, KPI tiles, leaderboards, and the vacuum status
section each display a brain icon. Clicking the icon
opens an analysis dialog and starts the LLM analysis.

The analysis follows these steps:

1. The system checks for a cached analysis result.
2. The system fetches server context from the
   connection.
3. The system fetches timeline events for the time
   range.
4. The system serializes the chart data and sends the
   data to the LLM.
5. The LLM produces a structured analysis report.

The dialog displays a loading skeleton while the
analysis runs. The final report renders as formatted
markdown.

### Analysis Reports

Each chart analysis report contains a structured
assessment of the metric data:

- The summary section describes the current state of
  the metric and its significance.
- The trends and patterns section identifies notable
  changes, spikes, or anomalies in the data.
- The recommendations section suggests specific actions
  to address any issues found.

### Timeline Event Correlation

The analysis includes timeline events from the chart's
time range to identify correlations between metric
changes and system events. The LLM considers the
following event types:

- Configuration changes to PostgreSQL settings.
- Alert activations and resolutions.
- Server restarts and recovery events.
- Extension installations and upgrades.
- Blackout periods and maintenance windows.

### Running SQL Queries

SQL code blocks in analysis reports include a play
button in the upper right corner. The run button
executes the query against the chart's associated
database server. Results appear inline below the code
block.

Write statements such as `ALTER SYSTEM` display a
confirmation dialog before executing. Read-only queries
execute immediately.

### Caching

The system caches chart analysis results on the client
side to avoid redundant LLM calls.

- An amber brain icon indicates that a cached analysis
  exists for the chart.
- The cache uses stable identifiers as the cache key;
  these include the metric description, connection,
  database, and time range.
- The cache expires after 30 minutes.
- Clicking an amber brain icon opens the cached report
  instantly.

### Downloading Reports

The dialog footer includes a Download button that saves
the analysis as a markdown file. The downloaded file
includes the chart details, the full analysis report,
and a generation timestamp.
