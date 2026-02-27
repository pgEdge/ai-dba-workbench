# Cluster Dashboard

The cluster dashboard focuses on replication health and
comparative performance across cluster members. The
dashboard appears when users select a cluster node in
the cluster navigator.

## Topology Diagram

The topology section renders an interactive diagram
showing servers as nodes with color-coded replication
edges. Each edge represents a replication relationship
between two servers.

The diagram uses the following color scheme for edges:

- Physical and streaming replication edges appear in
  the primary theme color (blue).
- Spock replication edges appear in the warning theme
  color (orange).
- Logical replication edges appear in the success theme
  color (green).

Edge labels display the replication type so users can
distinguish between different replication methods at a
glance.

## Replication Lag

The replication lag section displays KPI tiles for
current lag values alongside a time-series chart. The
chart tracks replication lag over the selected time
range for all replication relationships in the cluster.

## Comparative Metrics

The comparative charts section presents side-by-side
metrics for all servers in the cluster. The section
allows administrators to identify performance
disparities between cluster members. Users can click a
server entry to navigate to the
[server dashboard](server.md) for that server.
