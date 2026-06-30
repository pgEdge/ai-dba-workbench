# Server Dashboard

The server dashboard provides detailed metrics for a single PostgreSQL server.
Select a server node in the cluster navigator to open the dashboard.

## AI Overview

The `AI Overview` pane presents a concise, AI-generated summary of the
server's health and activity. A sparkle icon and an `AI Overview` label
identify the pane; a settings icon lets you pin the summary. The pane displays
a loading skeleton while the Workbench generates the summary.

For details about the AI Overview, see
[Using the AI Overview](index.md#using-the-ai-overview).

## Connection Details

The connection details bar displays the server's connection parameters in a
non-collapsible bar below the AI Overview. An info icon on the right side of
the bar provides additional context.

The bar displays the following details:

- The `HOST` field shows the network address of the server.
- The `PORT` field shows the port on which the server listens.
- The `DATABASE` field shows the database used for the connection.
- The `USER` field shows the user account used for the connection.
- The `ROLE` field shows the server's replication role, such as `Primary`.

## Event Timeline

The `Event Timeline` pane displays monitored events for the server across the
selected time range. Filter buttons and a time range selector control which
events the pane displays.

For details about the filter buttons and the time range selector, see
[Using the Event Timeline](index.md#using-the-event-timeline).

When no events fall within the selected range, the pane displays the message
`No events in this time range`. The pane suggests that you try expanding the
time range or adjusting the filters.

## Key Performance Indicators

The `Key Performance Indicator` tiles sit below the event timeline and
summarize server-wide metrics. Each tile presents a single value and displays
`No data` when no data is available.

The Workbench displays the following tiles:

- The `XID AGE` tile shows the transaction ID age for the server.
- The `CACHE HIT RATIO` tile shows the buffer cache hit ratio for the server.
- The `TRANSACTIONS` tile shows the transaction rate for the server.
- The `CHECKPOINTS` tile shows the checkpoint activity for the server.

## Active Alerts

The `Active Alerts` pane summarizes the alerts that are currently active for
the server. A bell icon and an `Active Alerts` label identify the pane; a
count badge displays the number of active alerts. Use the chevron on the right
side of the pane heading to expand or collapse the pane.

When no alerts are active, the pane displays a light green banner with a green
checkmark and the message `No active alerts`.

## Monitoring

The `Monitoring` pane presents detailed performance data for the server. The
[time range selector](index.md#selecting-time-ranges) lets you choose the
period for the displayed metrics; the available ranges are `1h`, `6h`, `24h`,
`7d`, and `30d`. Use the chevron on the right side of the pane heading to
expand or collapse the pane.

The Monitoring pane contains three collapsible sub-panes that group related
metrics.

### System Resources

The `System Resources` sub-pane displays operating-system metrics for the host
that runs the server. Four tiles summarize current resource usage, and each
tile displays `--` when no data is available.

The sub-pane includes the following tiles:

- The `CPU USAGE` tile shows the current processor utilization as a
  percentage.
- The `MEMORY USAGE` tile shows the current memory utilization as a
  percentage.
- The `DISK USAGE` tile shows the current disk utilization as a percentage.
- The `LOAD AVERAGE` tile shows the current system load average.

The sub-pane also displays the following time-series charts:

- The `CPU Usage Over Time` chart tracks processor utilization over the
  selected range.
- The `Memory Usage Over Time` chart tracks memory utilization over the
  selected range.
- The `Disk Space` chart tracks disk consumption over the selected range.
- The `Load Average Over Time` chart tracks the system load average over the
  selected range.
- The `Network I/O` chart tracks network throughput over the selected range.

When no data is available, each chart displays a message such as `No CPU data
available. Is the system_stats extension installed?` The charts require the
`system_stats` extension to collect operating-system metrics.

### PostgreSQL Overview

The `PostgreSQL Overview` sub-pane displays server-level database metrics.
Four tiles summarize current database activity, and each tile displays `--`
when no data is available.

The sub-pane includes the following tiles:

- The `BACKENDS` tile shows the number of active backend connections.
- The `COMMITS` tile shows the transaction commit rate for the server.
- The `CACHE HIT RATIO` tile shows the buffer cache hit ratio for the server.
- The `TEMP BYTES` tile shows the volume of temporary file data written.

The sub-pane also displays the following time-series charts:

- The `Connections Over Time` chart tracks active connections over the
  selected range.
- The `Transactions` chart tracks the transaction rate over the selected
  range.
- The `Block I/O` chart tracks block read and write activity over the selected
  range.
- The `Tuple Operations` chart tracks tuple-level activity over the selected
  range.

When no data is available, each chart displays a message such as `No
connection data available`, `No transaction data available`, `No block I/O
data available`, or `No tuple operation data available`.

### WAL and Replication

The `WAL and Replication` sub-pane displays write-ahead log activity and
replication status for the server. Four tiles summarize current WAL and
replication activity, and each tile displays `--` when no data is available.

The sub-pane includes the following tiles:

- The `WAL BYTES` tile shows the volume of write-ahead log data generated.
- The `WAL RECORDS` tile shows the number of write-ahead log records
  generated.
- The `REPLICATION LAG` tile shows the current replication lag for the server.
- The `CHECKPOINTS` tile shows the checkpoint activity for the server.

The sub-pane also displays the following time-series charts:

- The `WAL Activity Over Time` chart tracks write-ahead log generation over
  the selected range.
- The `Replication Lag Over Time` chart tracks replication lag over the
  selected range.
- The `Checkpoints Over Time` chart tracks checkpoint activity over the
  selected range.

When no data is available, each chart displays a message such as `No WAL data
available`, `No replication data available. Is this server a primary with
standbys?`, or `No checkpoint data available`.

## Database Summaries

The `Database Summaries` pane lists all databases on the server with
high-level metrics for each database. Use the chevron on the right side of the
pane heading to expand or collapse the pane. Click a database entry to navigate
to the [database dashboard](database.md) for that database.

## Top Queries

The `Top Queries` pane ranks queries by resource consumption. The pane
displays execution time, call count, rows returned, and source database for
the most active queries. Use the chevron on the right side of the pane heading
to expand or collapse the pane.

The Database column resolves each query's source database from the `dbid`
field in `pg_stat_statements` using `pg_stat_activity`. Because
`pg_stat_statements` collects data cluster-wide, the pane deduplicates queries
so each entry reflects a single database context.

The `Hide monitoring queries` toggle filters out the Workbench's own
monitoring queries from the list. The toggle is on by default to focus on
application queries.
