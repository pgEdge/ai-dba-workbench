# Database Dashboard

The database dashboard presents detailed metrics for a
single database. The dashboard appears when users select
a database in the cluster navigator or click a database
entry in the server dashboard.

## Performance Section

The performance section displays KPI tiles for the
following metrics:

- Database size in a human-readable format.
- Cache hit ratio as a percentage.
- Transactions per second with trend data.
- Dead tuple count across all tables.

Time-series charts below the KPI tiles show historical
trends for each metric over the selected time range.

## Table Leaderboard

The table leaderboard ranks tables by various metrics
such as size, row count, dead tuples, and sequential
scan frequency. The leaderboard helps administrators
identify tables that may need maintenance or
optimization. Users can click a table entry to navigate
to the [object dashboard](object.md) for that table.

## Index Leaderboard

The index leaderboard ranks indexes by metrics such as
size, scan count, and tuple reads. The leaderboard
helps administrators identify unused or inefficient
indexes. Users can click an index entry to navigate to
the [object dashboard](object.md) for that index.

## Vacuum Status

The vacuum status section shows autovacuum and manual
vacuum activity for tables in the database. The section
displays the last vacuum time, dead tuple ratio, and
autovacuum run count for each table.
