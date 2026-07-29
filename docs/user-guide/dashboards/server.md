# Server Dashboard

The server dashboard provides detailed metrics for a
single PostgreSQL server. The dashboard appears when
users select a server node in the cluster navigator.

## System Resources

The system resources section displays the following
metrics:

- CPU usage percentage with a time-series chart.
- Memory usage percentage with a time-series chart.
- Disk usage percentage with a time-series chart.
- Load average values with a time-series chart.
- Network I/O throughput with a time-series chart.

## PostgreSQL Overview

The PostgreSQL overview section displays server-level
database metrics:

- Active connections relative to the maximum allowed.
- Transactions per second with a time-series chart.
- Cache hit ratio as a percentage with trend data.
- Temporary files created with a time-series chart.

## WAL and Replication

The WAL and replication section shows write-ahead log
activity and replication status for the server. The
section includes WAL generation rates and replication
slot details.

## Database Summaries

The database summaries section lists all databases on
the server with high-level metrics for each database.
Users can click a database entry to navigate to the
[database dashboard](database.md).

## Connections

The connections section breaks down the server's
client connections by database user, client address,
or database. Three tabs select the grouping: By User,
By Client, and By Database. Each tab lists one row per
group, with the columns Total, Active, Idle, Idle in
transaction, and Other.

The counts come from the single most recent snapshot
the collector stored within the dashboard's selected
time range. The time range only decides which snapshot
counts as the latest; the section neither averages nor
peaks the figures across the period. This behaviour
differs from the charted metrics elsewhere on the
dashboard. The section labels the table with the time
of the snapshot the counts came from.

The section counts only real client connections. The
collector stores a row for every backend in
`pg_stat_activity`, including background workers such
as the WAL writer and the autovacuum workers, and the
section excludes those rows. The section needs no
additional collection, because the existing
`pg_stat_activity` probe already stores the data.

Some labels stand in for a missing or special value:

- A connection over a Unix-domain socket has no client
  address, so the section groups the connection under
  `local`.
- A backend with no recorded role name appears under
  `(unknown)`.
- A backend with no recorded database appears under
  `(none)`.

The By Client tab shows the reverse-resolved client
hostname on a second line beneath the client address,
where PostgreSQL recorded one. PostgreSQL populates
the `client_hostname` field only on servers that
enable `log_hostname`.

The Idle in transaction column counts the backends in
either the `idle in transaction` state or the
`idle in transaction (aborted)` state. The Other
column counts every remaining state, including the
backends that report no state at all.

The section lists each grouping in descending order of
total connections, and shows at most 200 groups. A
server with very many distinct client addresses
therefore shows only the 200 busiest groups on the By
Client tab. The groups omitted are always the smallest
ones, and the section adds no roll-up row for them.

## Top Queries

The top queries section ranks queries by resource
consumption. The section displays execution time, call
count, rows returned, and source database for the most
active queries.

The Database column resolves each query's source
database from the `dbid` field in
`pg_stat_statements` using `pg_stat_activity`.
Because `pg_stat_statements` collects data
cluster-wide, the section deduplicates queries so
each entry reflects a single database context.

The "Hide monitoring queries" toggle filters out the
workbench's own monitoring queries from the list. The
toggle is on by default to focus on application
queries.
