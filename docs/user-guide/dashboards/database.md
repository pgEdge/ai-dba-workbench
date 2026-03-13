# Database Dashboard

The database dashboard presents detailed metrics for a
single database. The dashboard appears when users select
a database in the cluster navigator or click a database
entry in the server dashboard summary cards.

## Performance Section

The performance section displays four KPI tiles with
the following metrics:

- Database size appears in a human-readable format.
- Cache hit ratio appears as a percentage.
- Total transactions show the combined commit and
  rollback count.
- Dead tuple ratio appears as a percentage across all
  tables.

Each KPI tile includes a sparkline that shows recent
trends for the metric. Tiles with sufficient data
display an AI Analysis button (brain icon) that
provides LLM-powered insights.

Two time-series charts appear below the KPI tiles.
The Transactions Over Time chart plots commits against
rollbacks. The Cache Hit Ratio Over Time chart tracks
the ratio across the selected time range. Each chart
includes an AI Analysis button.

## Table Leaderboard

The table leaderboard ranks tables using selectable
sort criteria via tab buttons:

- Rows displays the live row count for each table.
- Seq Scans displays the sequential scan count.
- Dead Tuples displays the dead tuple count.
- Modifications displays the total inserts, updates,
  and deletes.

Each entry shows the table name, a primary metric
value, a secondary metric, and a relative bar
indicator. Clicking a table entry navigates to the
[object dashboard](object.md) for that table.

An AI Analysis button appears beside the tab bar.
The analysis includes data across all sort categories
and indicates the tab the user is currently viewing.

## Index Leaderboard

The index leaderboard ranks indexes using selectable
sort criteria via tab buttons:

- Reads displays the tuples read count.
- Scans displays the index scan count.
- Unused sorts indexes by ascending scan count to
  surface unused indexes.

Each entry shows the index name, a primary metric,
a secondary metric, and a relative bar indicator.
Clicking an index entry navigates to the
[object dashboard](object.md) for that index.

An AI Analysis button provides analysis across all
index metrics.

## Vacuum Status

The vacuum status section displays a table of all
tables sorted by dead tuple ratio in descending order.
The table includes the following columns:

- The table name identifies each entry.
- The last vacuum column shows the most recent manual
  vacuum timestamp.
- The last autovacuum column shows the most recent
  autovacuum timestamp.
- The dead tuple count column shows the raw count.
- The dead tuple ratio column shows the percentage.

Color-coded timestamps indicate vacuum freshness:
green for recent, yellow for aging, and red for stale.
An AI Analysis button provides LLM-powered vacuum
recommendations.

## Accessing the Database Dashboard

Users access the database dashboard by selecting a
database in the cluster navigator. Users can also
click a database entry in the server dashboard
summary cards.
