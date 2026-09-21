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

KPI tiles display key metrics with embedded sparklines
that show recent trends. Each tile presents a single
metric value alongside a miniature time-series chart
for context.

The tiles follow the dashboard time range selector, so
each value and sparkline covers the selected window
across every server in the estate.

## Cluster Cards

The cluster cards section shows a summary card for each
cluster in the estate. Each card displays the cluster
name, server count, and high-level health indicators.
Users can click a cluster card to navigate to the
[cluster dashboard](cluster.md) for that cluster.
