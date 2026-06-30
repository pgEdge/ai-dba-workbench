# ESTATE OVERVIEW Dashboard

The `ESTATE OVERVIEW` presents a fleet-wide health assessment at a glance. To
view the `ESTATE OVERVIEW`, select the top-level estate node in the cluster
navigator.

![Accessing the ESTATE OVERVIEW](../images/open_estate_overview.png)

If you have enabled AI, the Overview displays the
[AI Overview](../ai/overview.md). The Overview provides a concise, AI-powered
summary of your database estate.

Tiles across the top of the ESTATE OVERVIEW provide an at-a-glance overview
of the state of your servers:

![Reviewing the state of your servers](../images/server_state.png)

- The `OK` tile (green) shows the count of servers that conform to all
  configured alert thresholds.
- The `WARNING` tile (orange) shows the count of servers with one or more
  active threshold violations.
- The `OFFLINE` tile (red) shows the count of servers that the collector
  cannot reach.
- The `CLUSTERS` tile shows the total number of clusters across the estate.
- The `GROUPS` tile shows the total number of groups in the estate.

## Active Alerts

The `Active Alerts` pane summarizes the alerts that are currently active
across the estate. A bell icon and an `Active Alerts` label identify the
panel; a count badge displays the number of active alerts. Use the down-arrow
on the right side of the panel heading to expand or collapse the panel.

When no alerts are active, the panel displays a light green banner with a
green checkmark and the message `No active alerts`.

## Monitoring

The `Monitoring` pane presents estate-wide health and performance data. The
[time range selector](index.md#selecting-time-ranges) lets you choose the
period for the displayed metrics; the available ranges are `1h`, `6h`, `24h`,
`7d`, and `30d`.

### Health Overview

The `Health Overview` tile summarizes server status and alert distribution
across the estate. The left side displays a donut chart labeled `Server
Status` that groups servers by health category. The chart legend identifies
the `OK` category in green, the `Warning` category in orange, and the
`Offline` category in red.

The right side displays an `Alert Distribution` panel that breaks down active
alerts. When no alerts are active, the panel displays `No active alerts`. A
brain icon in the top-right corner of the tile opens an AI chart analysis of
the displayed data.

### Key Performance Indicators

The Key Performance Indicators pane displays four tiles that summarize
estate-wide metrics. Each tile presents a single aggregate value across all
monitored servers.

The pane includes the following tiles:

- The `TOTAL SERVERS` tile shows the total number of monitored servers across
  the estate.
- The `TOTAL CONNECTIONS` tile shows the total number of active database
  connections across all servers.
- The `TRANSACTION RATE` tile shows the aggregate transactions per second
  (tx/s) across the estate.
- The `ACTIVE ALERTS` tile shows the total number of currently active alerts
  across all servers.

### Clusters

The Clusters pane displays one tile for each cluster in the estate. Click a
cluster tile to navigate to the [cluster dashboard](cluster.md) for that
cluster.

Each cluster tile displays the following details:

- The cluster name identifies the cluster; example names include
  `development`, `management`, and `traffic`.
- The server count shows the number of servers in the cluster, such as
  `1 server` or `2 servers`.
- Colored dots indicate the online and warning counts; a green dot marks
  online servers, and an orange dot marks servers in a warning state.
- Role tags display as chips that show each role and its count, such as
  `primary: 1`, `binary_standby: 1`, and `spock: 1`.
- An orange badge displays the active alert count when the cluster has active
  alerts.
