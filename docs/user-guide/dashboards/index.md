# Dashboards

Monitoring dashboards provide a hierarchical view of PostgreSQL database health
and performance. You can navigate through five levels of detail, from a
fleet-wide estate overview down to individual database objects.

## Dashboard Hierarchy

The dashboard system organizes metrics into five levels that progress from
broad to specific. Select items in the cluster navigator or click drillable
elements within each dashboard to move between levels. See
[Using the Cluster Navigator](#using-the-cluster-navigator) for details on the
navigator tree.

- The [ESTATE DASHBOARD](estate.md) shows fleet-wide health across all
  monitored servers.
- The [CLUSTER DASHBOARD](cluster.md) focuses on replication topology and
  comparative metrics across cluster members.
- The [SERVER DASHBOARD](server.md) displays system resources and PostgreSQL
  performance for a single server.
- The [DATABASE DASHBOARD](database.md) presents table and index leaderboards
  with vacuum status for one database.
- The [OBJECT DASHBOARD](object.md) provides detailed metrics for a specific
  table, index, or query.

## Using the Workbench with an AI Provider

If you have enabled an
[AI provider](../ai/index.md#enabling-ai-mode), the Workbench
displays an informational analysis of each dashboard that you visit, and an
`Ask Ellie` chat assistant for interactive help. See
[AI Overview](../ai/index.md) and [Ask Ellie](../ai/ask-ellie.md) for
details.

## Using the Cluster Navigator

The cluster navigator on the left side of the console provides tree-based
navigation across groups, clusters, and individual servers. The navigator tree
reflects the estate, cluster, server, and database hierarchy. Select a node in
the navigator to view dashboards, alerts, and AI insights for the selected
resource.

![Using the Cluster Navigator](../../images/cluster_navigator.png)

## Reviewing Server and Cluster Settings

Server nodes and clusters in the cluster navigator each display a gear icon
when you hover over them. Click the gear icon to open the `Server Settings` or
`Cluster Settings` dialog for that resource. See
[Server and Cluster Settings](../server-cluster-settings.md) for details on
each tab, including connection details, cluster topology, alert overrides,
probe configuration, and notification channels.
