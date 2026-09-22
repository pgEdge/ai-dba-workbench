# Estate Dashboard

The estate dashboard presents a fleet-wide health
assessment at a glance. The dashboard appears when users
select the top-level estate node in the cluster
navigator.

## Health Overview

The health overview section displays donut charts that
summarize server status counts across the estate. Each
chart groups servers by a health category so
administrators can quickly identify servers that need
attention.

## KPI Tiles

KPI tiles display key metrics across the estate. Each
tile presents a single metric value with its label and
its unit.

Transaction Rate follows the dashboard time range
selector and reports the commit rate over the selected
window, summed across every server in the estate. Total
Servers, Total Connections, and Active Alerts do not
follow the selector; each of those tiles reports the
estate as it stands now.

## Cluster Cards

The cluster cards section shows a summary card for each
cluster in the estate. Each card displays the cluster
name, server count, and high-level health indicators.
Users can click a cluster card to navigate to the
[cluster dashboard](cluster.md) for that cluster.
