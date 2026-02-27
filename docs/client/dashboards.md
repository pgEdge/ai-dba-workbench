# Monitoring Dashboards

The monitoring dashboards provide a hierarchical view of
PostgreSQL database health and performance. Users navigate
through five levels of detail, from a fleet-wide estate
overview down to individual database objects.

## Dashboard Hierarchy

The dashboard system organizes metrics into five levels:

- The estate dashboard shows fleet-wide health across all
  monitored servers.
- The cluster dashboard focuses on replication topology and
  comparative metrics across cluster members.
- The server dashboard displays system resources and
  PostgreSQL performance for a single server.
- The database dashboard presents table and index
  leaderboards with vacuum status for one database.
- The object dashboard provides detailed metrics for a
  specific table, index, or query.

Users navigate between levels by selecting items in the
cluster navigator or by clicking drillable elements within
each dashboard.

## Estate Dashboard

The estate dashboard presents a fleet-wide health assessment
at a glance. The dashboard appears when users select the
top-level estate node in the cluster navigator.

### Health Overview

The health overview section displays donut charts that
summarize server status counts across the estate. Each chart
groups servers by a health category so administrators can
quickly identify servers that need attention.

### KPI Tiles

KPI tiles display key metrics with embedded sparklines that
show recent trends. Each tile presents a single metric value
alongside a miniature time-series chart for context.

### Cluster Cards

The cluster cards section shows a summary card for each
cluster in the estate. Each card displays the cluster name,
server count, and high-level health indicators.

## Cluster Dashboard

The cluster dashboard focuses on replication health and
comparative performance across cluster members. The
dashboard appears when users select a cluster node in the
cluster navigator.

### Topology Diagram

The topology section renders an interactive diagram showing
servers as nodes with color-coded replication edges. Each
edge represents a replication relationship between two
servers.

The diagram uses the following color scheme for edges:

- Physical and streaming replication edges appear in the
  primary theme color (blue).
- Spock replication edges appear in the warning theme
  color (orange).
- Logical replication edges appear in the success theme
  color (green).

Edge labels display the replication type so users can
distinguish between different replication methods at a
glance.

### Replication Lag

The replication lag section displays KPI tiles for current
lag values alongside a time-series chart. The chart tracks
replication lag over the selected time range for all
replication relationships in the cluster.

### Comparative Metrics

The comparative charts section presents side-by-side metrics
for all servers in the cluster. The section allows
administrators to identify performance disparities between
cluster members.

## Server Dashboard

The server dashboard provides detailed metrics for a single
PostgreSQL server. The dashboard appears when users select a
server node in the cluster navigator.

### System Resources

The system resources section displays the following metrics:

- CPU usage percentage with a time-series chart.
- Memory usage percentage with a time-series chart.
- Disk usage percentage with a time-series chart.
- Load average values with a time-series chart.
- Network I/O throughput with a time-series chart.

### PostgreSQL Overview

The PostgreSQL overview section displays server-level
database metrics:

- Active connections relative to the maximum allowed.
- Transactions per second with a time-series chart.
- Cache hit ratio as a percentage with trend data.
- Temporary files created with a time-series chart.

### WAL and Replication

The WAL and replication section shows write-ahead log
activity and replication status for the server. The section
includes WAL generation rates and replication slot details.

### Database Summaries

The database summaries section lists all databases on the
server with high-level metrics for each database. Users can
click a database entry to navigate to the database
dashboard.

### Top Queries

The top queries section ranks queries by resource
consumption. The section displays execution time, call
count, and rows returned for the most active queries.

The "Hide monitoring queries" toggle filters out the
workbench's own monitoring queries from the list. The
toggle is on by default to focus on application queries.

## Database Dashboard

The database dashboard presents detailed metrics for a
single database. The dashboard appears when users select a
database in the cluster navigator or click a database entry
in the server dashboard.

### Performance Section

The performance section displays KPI tiles for the
following metrics:

- Database size in a human-readable format.
- Cache hit ratio as a percentage.
- Transactions per second with trend data.
- Dead tuple count across all tables.

Time-series charts below the KPI tiles show historical
trends for each metric over the selected time range.

### Table Leaderboard

The table leaderboard ranks tables by various metrics such
as size, row count, dead tuples, and sequential scan
frequency. The leaderboard helps administrators identify
tables that may need maintenance or optimization.

### Index Leaderboard

The index leaderboard ranks indexes by metrics such as
size, scan count, and tuple reads. The leaderboard helps
administrators identify unused or inefficient indexes.

### Vacuum Status

The vacuum status section shows autovacuum and manual vacuum
activity for tables in the database. The section displays
the last vacuum time, dead tuple ratio, and autovacuum
run count for each table.

## Object Dashboard

The object dashboard provides the most detailed view of a
single database object. The dashboard appears when users
click a table, index, or query in the database dashboard
leaderboards.

### Table Detail

The table detail view displays the following metrics:

- Table size and total size including indexes and TOAST.
- Table bloat estimate as a percentage.
- Sequential scan count versus index scan count.
- Insert, update, and delete modification counts.
- Live tuple and dead tuple counts with trend data.

### Index Detail

The index detail view displays the following metrics:

- Index size in a human-readable format.
- Index scan count with a time-series chart.
- Tuples read and tuples fetched counts.

### Query Detail

The query detail view displays the following metrics:

- Total and mean execution time.
- Total rows returned and rows per call.
- Call count with a time-series chart.

### Query Plan

The query plan section appears in the query detail view
below the AI Overview panel. The section defaults to
expanded, and the expand/collapse state persists across
browser sessions.

The panel fetches PostgreSQL `EXPLAIN` output when the
section first renders. A refresh button in the section
header regenerates the plan on demand.

Two tabs display the plan data:

- The Text tab shows the standard `EXPLAIN` output in
  monospace format for a concise view.
- The Visual tab shows a graphical flow diagram built
  from the JSON `EXPLAIN` plan.

#### Visual Diagram

The visual diagram uses a left-to-right layout. Leaf
scan nodes appear on the left and the root node appears
on the right. SVG bezier arrows connect each child node
to its parent.

Each tile in the diagram displays the node type and the
relation or index name. A colored left border indicates
the cost ratio relative to the root node:

- A red border marks nodes that consume over 80 percent
  of the total cost.
- An orange border marks nodes that consume over 50
  percent of the total cost.
- The default border color applies to all other nodes.

Clicking a tile opens a popover with comprehensive node
details. The popover displays the following information:

- The cost range from startup cost to total cost.
- The estimated row count and row width.
- The output columns produced by the node.
- The execution strategy and scan direction.
- The planned and launched worker counts.
- Any filter, join, or index conditions.

#### Plan Options

The system uses `EXPLAIN VERBOSE` for JSON plans to
provide comprehensive detail in the visual mode. The
text plan uses standard `EXPLAIN` without `VERBOSE` for
a concise view.

For parameterized queries that use `$1`, `$2`
placeholders, the system uses the `GENERIC_PLAN` option
available in PostgreSQL 16 and later. Older PostgreSQL
versions display a friendly informational message
instead of the plan.

The system caches plans for five minutes to avoid
redundant queries against the database server.

### AI Query Overview

The query detail view displays an AI Overview panel below
the query text when an LLM provider is configured. The
panel provides a brief plain-text summary of the query's
performance characteristics in two to three sentences.

The AI Overview panel includes the following behaviors:

- The summary assesses whether the query appears healthy
  or has potential performance issues.
- A refresh button regenerates the summary on demand.
- A relative timestamp shows when the summary was last
  generated.
- A brain icon button opens the full AI Query Analysis
  dialog.
- The system caches summaries for 30 minutes.
- The panel is hidden when no LLM provider is configured.

### AI Query Analysis

Clicking the brain icon in the AI Overview panel opens a
full-screen analysis dialog. The analysis uses an agentic
LLM loop with tools to gather additional context before
producing a structured report.

The following tools are available to the LLM:

- The query metrics tool retrieves historical metric
  values with time-based aggregation.
- The metric baselines tool provides statistical baselines
  including mean, standard deviation, and extremes.
- The database query tool executes diagnostic queries
  against the relevant database.
- The schema inspection tool retrieves table and index
  definitions for referenced objects.
- The query validation tool checks SQL syntax and
  execution plans.
- The knowledgebase search tool finds relevant entries
  using similarity matching.

The dialog displays real-time progress indicators showing
which tools the LLM is currently using. The final report
renders as formatted markdown.

Each analysis report contains four sections:

- The Summary section describes the query and its current
  performance characteristics.
- The Performance Analysis section examines execution
  metrics, trends, and resource consumption.
- The Optimization Opportunities section identifies
  potential improvements to the query or schema.
- The Recommendations section suggests specific actions
  with supporting SQL examples.

SQL code blocks in the report include a Run button that
executes the query against the correct database server.
Write statements display a confirmation dialog before
the system executes the query.

The system caches analysis results for 30 minutes. A
Download button in the dialog footer saves the report as
a markdown file.

## Time Range Selector

The time range selector controls the time window for all
charts in the monitoring section. The selector appears as a
toggle button group with the following options:

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
dashboards. The analysis examines data trends, identifies
anomalies, and generates actionable recommendations.

### Triggering an Analysis

Every chart and KPI tile displays a brain icon in the
upper right corner. Clicking the icon opens an analysis
dialog and starts the LLM analysis.

The analysis follows these steps:

1. The system checks for a cached analysis result.
2. The system fetches server context from the connection.
3. The system fetches timeline events for the time range.
4. The system serializes the chart data and sends the
   data to the LLM.
5. The LLM produces a structured analysis report.

The dialog displays a loading skeleton while the analysis
runs. The final report renders as formatted markdown.

### Analysis Reports

Each chart analysis report contains a structured
assessment of the metric data:

- The summary section describes the current state of the
  metric and its significance.
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

SQL code blocks in analysis reports include a play button
in the upper right corner. The run button executes the
query against the chart's associated database server.
Results appear inline below the code block.

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
