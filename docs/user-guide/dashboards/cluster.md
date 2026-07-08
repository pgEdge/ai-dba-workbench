# Cluster Dashboard

The CLUSTER dashboard focuses on replication health and comparative performance
across cluster members. To view the cluster dashboard, select a cluster name in
the [cluster navigator](index.md#using-the-cluster-navigator).

## Event Timeline

The `Event Timeline` pane displays monitored events for the servers in the
cluster. See [Event Timeline](../event-timeline.md) for details on the time
range selector, event type filters, and reviewing event details.

## Key Performance Indicators

The `Key Performance Indicator` tiles sit below the event timeline and
summarize cluster-wide metrics. Each tile presents a single value and displays
`No data` when no data is available.

The Workbench displays the following tiles:

- The `XID AGE` tile shows the transaction ID age for the cluster.
- The `CACHE HIT RATIO` tile shows the buffer cache hit ratio across the
  cluster.
- The `TRANSACTIONS` tile shows the transaction rate for the cluster.
- The `CHECKPOINTS` tile shows the checkpoint activity for the cluster.

## Active Alerts

The `Active Alerts` pane shows the alerts that are currently active across the
cluster.

![Reviewing Active Alerts](../../images/cluster_active_alerts.png)

See [Using Alerts](../alerts/index.md) for details on reviewing, acknowledging,
and analyzing alerts, and for how to find an alert's acknowledgment reason or a
past alert from the Event Timeline.

## Topology

The `Topology` pane renders an interactive diagram showing servers as nodes
with color-coded replication edges. Each edge represents a replication
relationship between two servers.

![Reviewing timeline tooltips](../../images/cluster_topology.png)

A colored dot on each node indicates server status; a green dot marks an online
server. Each node tile displays a label that identifies the server.

The diagram uses the following color scheme for edges:

- Physical and streaming replication edges display in blue.
- Spock replication edges display in orange.
- Logical replication edges display in green.

Edge labels display the replication type so you can distinguish between
different replication methods at a glance.


## Monitoring

The `Monitoring` pane presents replication health and comparative performance
data for the cluster. See [Dashboard Conventions](../dashboard-conventions.md)
for details on the time range selector and expanding or collapsing panes.

### Replication Lag

The `Replication Lag` tile tracks replication lag over the selected time range
for the replication relationships in the cluster. The tile presents the current
lag values alongside a time-series chart.

When the Workbench detects no primary server, the tile displays the message
`No primary server detected in this cluster.`

### Comparative Metrics

The `Comparative Metrics` tile presents side-by-side metrics for all servers in
the cluster. Use the tile to identify performance disparities between cluster
members. Click a server entry to navigate to the [server dashboard](server.md)
for that server.
