# Monitoring Dashboards

The monitoring dashboards provide a visual interface for
exploring PostgreSQL metrics collected by the AI DBA Workbench.
Users can assess fleet health at a glance, compare metrics
across cluster members, and diagnose issues at the server,
database, and object level.

## Design Philosophy

The dashboards follow two core principles.

- The user stays in context during drill-down navigation.
  Drilling down opens an overlay that expands the selected
  information within the Status Panel. The background
  dashboard remains partially visible to preserve spatial
  orientation.

- The hierarchy matches the data scoping model. The dashboard
  levels follow the natural data structure: estate, cluster,
  server, database, and object (table, index, or query).

## Navigation Model

Users navigate the dashboards through two complementary
mechanisms: entity selection and overlay drill-down.

### Entity Selection

The Cluster Navigator sidebar controls which dashboard level
appears in the Status Panel. Selecting the estate shows the
Estate Dashboard. Selecting a cluster shows the Cluster
Dashboard. Selecting a server shows the Server Dashboard.
The application already supports this mechanism.

### Overlay Drill-Down

Clicking any metric tile, chart, or summary card within a
dashboard opens a detail overlay. The overlay expands to fill
most of the Status Panel area. The overlay shows detailed
charts, breakdowns, and further drill-down options. A single
close action (X button or Escape key) returns the user to
the underlying dashboard.

The overlay has the following characteristics:

- The overlay fills the Status Panel content area rather
  than the browser viewport.

- The background dashboard appears dimmed but remains
  partially visible.

- A title bar shows the metric name and entity context
  (for example, "CPU Usage -- prod-primary-1").

- The content area scrolls independently from the
  background dashboard.

- Clicking a drill-down item within the overlay replaces
  the overlay content rather than stacking a second overlay.
  A back arrow appears to return to the previous content.

- The overlay inherits the dashboard time range but allows
  a local override.

## Dashboard Levels

The dashboards organize metrics into five hierarchical levels.

### Estate Dashboard

The Estate Dashboard appears when the user selects the estate
in the Cluster Navigator. The dashboard provides fleet-wide
health information at a glance.

The Estate Dashboard includes the following elements:

- A health ring (donut chart) shows server distribution by
  status: online, warning, and offline.

- KPI tiles display total active connections, aggregate
  transactions per second, total disk usage, and the total
  active alert count.

- A cluster health grid contains one card per cluster, with
  each card showing cluster status, node count, and a
  replication lag summary.

- A hot spots panel lists the top five servers ranked by
  alert count, replication lag, or resource utilization.

- A recent events timeline shows estate-wide events in
  chronological order.

#### Estate Dashboard Drill-Downs

The following overlay drill-downs are available from the
Estate Dashboard:

- Clicking a health ring segment opens an overlay that lists
  all servers in that status category.

- Clicking a KPI tile opens an overlay with the estate-wide
  time-series chart for that metric.

- Clicking a cluster card selects the cluster in the Cluster
  Navigator and switches to the Cluster Dashboard.

- Clicking a hot spot entry selects that server in the
  Cluster Navigator and switches to the Server Dashboard.

### Cluster Dashboard

The Cluster Dashboard appears when the user selects a cluster
in the Cluster Navigator. The dashboard focuses on replication
health and cross-node comparison.

The Cluster Dashboard includes the following elements:

- A topology diagram shows cluster nodes, replication links,
  and node roles (primary, standby, subscriber, or Spock
  node). Replication links display direction and current lag.

- Replication tiles show write lag, flush lag, and replay lag
  for each standby or subscriber.

- Comparative charts display selected metrics side by side
  across all cluster members. Default comparisons include
  transactions per second, active connections, and cache
  hit ratio.

- Cluster KPI tiles show total cluster connections, aggregate
  transactions per second, and WAL generation rate.

- An alert summary shows active alerts grouped by server
  within the cluster.

#### Cluster Dashboard Drill-Downs

The following overlay drill-downs are available from the
Cluster Dashboard:

- Clicking a topology node opens an overlay with a summary
  of that server's key metrics.

- Clicking a replication link opens an overlay with a
  time-series chart of replication lag for that link.

- Clicking a comparative chart opens an overlay with the
  expanded chart for that metric across all cluster nodes;
  the expanded chart includes baseline bands.

- Clicking an alert entry opens the existing Alert Analysis
  Dialog for that alert.

### Server Dashboard

The Server Dashboard appears when the user selects a server
in the Cluster Navigator. The dashboard provides comprehensive
server health and performance information organized in
collapsible sections.

#### System Resources Section

The system resources section appears when the monitored server
has the `system_stats` extension installed.

The section includes the following tiles:

- A CPU utilization tile shows the current percentage with a
  sparkline.

- A memory usage tile shows used and total memory with a
  sparkline.

- Disk usage bars show per-mount-point usage with used, free,
  and total space.

- A network throughput tile shows receive and transmit byte
  rates with sparklines.

- A load average tile shows one-minute, five-minute,
  ten-minute, and fifteen-minute averages.

#### PostgreSQL Overview Section

The PostgreSQL overview section displays core database
performance metrics.

The section includes the following tiles:

- A connections tile shows active, idle, and
  idle-in-transaction counts with a sparkline of total
  connections.

- A transactions tile shows commits and rollbacks per second
  with a sparkline.

- A cache hit ratio tile shows the current ratio with a
  sparkline.

- A tuple activity tile shows insert, update, and delete
  rates with a sparkline of total operations.

- A temporary files tile shows the count and total size with
  a sparkline.

#### WAL and Replication Section

The WAL and replication section displays write-ahead log
activity and replication status.

The section includes the following tiles:

- A WAL generation rate tile shows bytes per second with a
  sparkline.

- A checkpoint activity tile shows timed versus requested
  checkpoint counts with write and sync timing.

- A replication status tile shows per-standby lag values if
  the server is a primary; the tile shows WAL receiver
  status if the server is a standby.

#### Database Summary

The database summary shows one card per database on the
server. Each card displays the database size, active
connection count, transactions per second, and database age
(transaction ID wraparound distance).

#### Top Queries

The top queries section appears when the monitored server has
the `pg_stat_statements` extension installed. The section
lists the top five queries by total execution time. Each
entry shows the query text (truncated), call count, mean
execution time, and total execution time.

#### Server Dashboard Drill-Downs

The following overlay drill-downs are available from the
Server Dashboard:

- Clicking any metric tile opens an overlay with a full
  time-series chart for that metric. The overlay includes a
  time range selector, an aggregation selector, baseline
  bands, related metrics, and anomaly markers.

- Clicking a database card opens the Database Detail overlay
  for that database.

- Clicking a query entry opens the Query Detail overlay for
  that query.

### Database Detail Overlay

The Database Detail overlay opens from a database card on the
Server Dashboard. The overlay shows database-specific
performance information.

The Database Detail overlay includes the following elements:

- Database statistics appear as time-series charts for
  transactions per second, tuple operations, block I/O,
  temporary file usage, and deadlock count.

- A table leaderboard lists tables ranked by sequential scan
  count, dead tuple count, total size, or modification
  count. The user selects the ranking metric from a dropdown.

- An index leaderboard lists indexes ranked by scan count.
  The leaderboard highlights unused indexes (zero scans).

- A vacuum status panel lists tables that need vacuuming,
  ranked by dead tuple ratio or time since the last vacuum.

The following further drill-downs are available within the
Database Detail overlay:

- Clicking a table row replaces the overlay content with the
  Table Detail view.

- Clicking an index row replaces the overlay content with
  the Index Detail view.

### Table Detail Overlay

The Table Detail overlay shows access patterns and
maintenance status for a single table.

The Table Detail overlay includes the following time-series
charts:

- A chart shows the sequential scan versus index scan ratio
  over time.

- A chart shows tuple operation rates: inserts, updates, and
  deletes per second.

- A chart shows live tuple versus dead tuple counts over
  time.

- A chart shows heap, index, and TOAST block I/O with reads
  versus cache hits.

- Vacuum and analyze history appears as event markers on a
  timeline.

### Index Detail Overlay

The Index Detail overlay shows usage trends for a single
index.

The Index Detail overlay includes the following time-series
charts:

- A chart shows the index scan count over time.

- A chart shows tuples read versus tuples fetched per scan.

- A chart shows block I/O with reads versus cache hits.

### Query Detail Overlay

The Query Detail overlay shows execution trends for a single
query identified by query ID from `pg_stat_statements`.

The Query Detail overlay includes the following time-series
charts:

- A chart shows execution time trend: total, mean, and
  maximum execution time.

- A chart shows call frequency over time.

- A chart shows buffer usage: shared block hits versus reads
  per execution.

- A chart shows rows returned per execution.

## Time Controls

A global time range selector appears in the Status Panel
header. The selector provides preset buttons for one hour,
six hours, twenty-four hours, and seven days. The selected
range applies to all charts on the current dashboard.

Overlays inherit the dashboard time range by default. Each
overlay includes a local time range override for focused
analysis.

An auto-refresh toggle in the header enables periodic data
refresh. The default refresh interval matches the shortest
probe collection interval for the displayed metrics.

## Baseline Integration

Every time-series chart integrates baseline data from the
`get_metric_baselines` tool.

The baseline appears as a shaded band between the mean minus
one standard deviation and the mean plus one standard
deviation. The current metric value turns red when the value
falls outside the baseline range. Tooltips display the
baseline mean and standard deviation alongside the current
value.

## Alert Integration

Metric tiles that have active alerts display an alert badge
icon. Clicking the alert badge opens the existing Alert
Analysis Dialog.

In overlay time-series charts, alert trigger points appear
as vertical markers on the time axis. Each marker opens the
alert detail for that event when clicked.

## Sparklines

Each metric tile on the Server Dashboard and Cluster
Dashboard includes a sparkline. A sparkline is a small,
inline time-series chart that shows the metric trend for the
current time range. Sparklines use 30 data points from the
`query_metrics` tool with the `avg` aggregation.

On narrow viewport widths, sparklines collapse and the tile
shows only the current value.

## Implementation Notes

The following sections describe key implementation details
for the monitoring dashboards.

### Data Fetching

All metric data flows through the existing `query_metrics`
MCP tool. The dashboards use 150 buckets for full-width
charts and 30 buckets for sparklines. The
`get_metric_baselines` tool provides baseline data for every
chart.

### Charting

The dashboards use the existing ECharts integration in
`client/src/components/Chart/`. New chart configurations
extend the existing line, bar, and pie option builders. The
baseline band renders as an ECharts `markArea` on line
charts.

### Overlay Component

A new `MetricOverlay` component manages the overlay
lifecycle. The component uses a MUI `Backdrop` for the
dimmed background and a `Slide` or `Grow` transition for
the content panel. The component accepts a render function
for the overlay content. Each dashboard level defines its
own drill-down views through the render function.

### State Management

A new `DashboardContext` manages the following state:

- The current global time range for all dashboard charts.

- The auto-refresh interval and the enabled flag.

- The overlay stack (current and previous overlay content)
  for back navigation.

The `DashboardContext` integrates with the existing
`ClusterSelectionContext` to respond to entity selection
changes.
