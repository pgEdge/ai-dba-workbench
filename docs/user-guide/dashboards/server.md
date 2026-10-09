# Server Dashboard

The server dashboard provides detailed metrics for a
single PostgreSQL server. The dashboard appears when
users select a server node in the cluster navigator.

## System Resources

The system resources section displays the following
metrics:

- CPU usage percentage with a time-series chart.
- Memory usage percentage with a time-series chart,
  and an estimated available memory figure beneath the
  percentage.
- Disk usage percentage with a time-series chart.
- Load average values with a time-series chart.
- Network I/O throughput as bytes transmitted and
  received per second, with a time-series chart.

The memory chart plots four series: used, free, cached,
and Available (est.). The available series is an
estimate of how much memory a new workload could claim,
calculated as free memory plus cached memory, because
the kernel's own `MemAvailable` figure is not available
to the collector. The estimate runs high on a host with
a large non-reclaimable slab or a largely dirty page
cache, so read the series as a guide when sizing
`shared_buffers` or `work_mem` rather than as an exact
figure.

## PostgreSQL Overview

The PostgreSQL overview section displays server-level
database metrics. The charts built on cumulative
PostgreSQL counters plot rates rather than the raw
counter totals, so a rising line shows a busier server
rather than the simple passage of time.

The section displays the following KPI tiles:

- The Backends tile shows the active connections
  relative to the maximum allowed.
- The Commits tile shows the current commits per
  second.
- The Cache Hit Ratio tile shows the ratio as a
  percentage with trend data.
- The Temp Bytes tile shows the bytes spilled to
  temporary files across the selected time range, with
  a sparkline of the bytes spilled in each interval.

The section displays the following time-series charts:

- The Connections (Monitored Database) chart plots the
  backends connected to each monitored database.
- The Sessions Established (Monitored Database) chart
  plots the sessions opened against each monitored
  database.
- The Transactions chart plots commits and rollbacks
  per second.
- The Block I/O chart plots blocks hit and blocks read
  per second.
- The Tuple Operations chart plots the rows fetched,
  inserted, updated, and deleted per second.

The cache hit ratio is computed from the blocks read and
hit during each sample interval, so a current problem is
visible immediately rather than being diluted by history
since the last statistics reset. An interval with no block
access shows as a gap in the sparkline and as '--' for the
headline value. The ratio counts only `shared_buffers`
hits; a block read may still be served from the operating
system page cache, so a lower ratio does not by itself
indicate slow I/O.

## WAL and Replication

The WAL and replication section shows write-ahead log
activity, checkpoint behaviour, and replication status
for the server.

The section displays the following KPI tiles:

- The WAL Bytes tile shows the current WAL bytes
  written per second.
- The WAL Records tile shows the current WAL records
  written per second.
- The Replication Lag tile shows the current lag for
  the server's replicas.
- The Requested Checkpoints tile shows the percentage
  of the checkpoints in the selected time range that
  PostgreSQL requested rather than scheduled. A high
  percentage suggests that `max_wal_size` is too low.

The section displays the following charts:

- The WAL Activity Over Time chart plots the WAL bytes
  and WAL records written per second.
- The Replication Lag Over Time chart plots the write,
  flush, and replay lag.
- The Checkpoints Over Time chart is a stacked bar
  chart of the timed and requested checkpoints
  completed in each interval.
- The Checkpoint Buffers Written chart plots the
  buffers that checkpoints wrote per second.

## Replication Slots

The replication slots section lists the replication
slots on the server, from the most recent snapshot the
collector took in the last hour. The section does not
follow the dashboard time range selector; it always
shows the latest snapshot of each slot.

When the cluster topology records replication between
this server and others, whether the collector detected
it or an administrator set it, the section first names
them:

- The Upstream line lists the servers this server
  streams from, subscribes to, or replicates with.
- The Downstream line lists the standbys that stream
  from this server and the subscribers that subscribe
  to it; these are usually the consumers of its slots.

A summary line counts the slots, the active and
inactive slots, and the slots whose WAL is at risk. The
table shows the following columns for each slot:

- The Slot column shows the slot name.
- The Type column shows whether the slot is physical
  or logical.
- The Status column shows Active when a consumer is
  connected to the slot and Inactive when none is, or
  Unknown when the snapshot does not say. An inactive
  slot can stop PostgreSQL from removing WAL until its
  consumer returns, so it is shown as a warning; the WAL
  Status column shows whether the WAL is still kept.
- The WAL Status column shows how safe the slot's WAL
  is. Reserved (green) means the WAL is within
  `max_wal_size`; Extended (amber) means it exceeds
  `max_wal_size` but is still kept; Unreserved (red)
  means the WAL will be removed at the next checkpoint
  unless the consumer catches up; and Lost (red) means
  the WAL has gone and the slot can no longer be used.
  Servers older than PostgreSQL 13 do not report a WAL
  status, so the column shows Not reported.
- The Retained WAL column shows how much WAL the slot
  is holding back.
- The Safe WAL Size column shows how much more WAL can
  be written before the slot is at risk of losing WAL.
  It shows Unlimited when `max_slot_wal_keep_size` is
  `-1`, which means the slot can retain WAL until the
  disk fills. It shows Exceeded when the slot already
  retains more WAL than `max_slot_wal_keep_size`
  allows, which is usually the case for an Unreserved
  slot. A Lost slot has no safe WAL size, and servers
  older than PostgreSQL 13 do not report one, so the
  column shows `--` for both.

The section lists up to 100 slots by name, and notes the
total when a server has more. The collector records a
slot only while PostgreSQL reports a `restart_lsn` for
it, so a slot that has never reserved WAL does not
appear. A slot that loses its WAL usually stops being
recorded as well; for example, a physical slot
invalidated by `max_slot_wal_keep_size` loses its
`restart_lsn` at once. Some lost slots keep their
`restart_lsn` and stay in the list with the Lost status;
a logical slot on a standby that was invalidated because
the rows it needed were removed is one such case.

Each row is the slot's latest snapshot from the last
hour, so a row can outlive its slot by up to an hour. A
slot that has been dropped, or that has stopped being
recorded because it lost its WAL, keeps showing its last
recorded state during that time, with nothing to mark
the row as out of date. A slot that became lost after
its last snapshot can therefore still read Extended or
Unreserved for a while.

## Database Summaries

The database summaries section lists all databases on
the server with high-level metrics for each database.
The cache hit ratio on each card is a per-interval value
and shows '--' when the database had no block access in
the latest interval. Users can click a database entry to
navigate to the [database dashboard](database.md).

The section follows the dashboard time range selector.
Each card reports the databases as they stood at the end
of the selected window, so a window that ends in the past
shows the sizes, connection counts and dead tuple ratios
of that moment rather than the present ones. The
performance tiles in the status panel keep their own fixed
twenty-four hour window, as the
[Dashboards](index.md#views-that-honour-the-selector)
page explains.

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

The section follows the dashboard time range selector, so
the figures describe the activity inside the selected
window rather than the lifetime totals of each statement.
A query that ran no calls inside the window does not
appear, and changing the range returns the section to its
first page. The [object dashboard](object.md) page explains
how the Workbench derives a windowed figure from the
cumulative `pg_stat_statements` counters.

The database filter above the list is the one exception.
The filter lists the databases seen over a fixed
twenty-four hours, so the available choices do not vanish
as the user narrows the selected window.

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

On an existing installation, the toggle only hides
statements that PostgreSQL first recorded after the
upgrade. `pg_stat_statements` identifies a statement by
its parse tree, which ignores comments, so a statement
already recorded before the upgrade keeps the untagged
text it was first seen with, and the filter never
matches it. Those entries also run on every collection
cycle, so they are never evicted. Run `SELECT
pg_stat_statements_reset();` once on each monitored
instance after upgrading if you want the toggle to hide
the monitoring queries already in the view.

The toggle is a display convenience rather than a
security or audit control. The filter matches a marker
comment in the statement text, so a database user who
can run arbitrary SQL can hide a query by including
that marker. Switching the toggle off restores the
full list of statements.

The panel header includes a database filter when the
connection monitors more than one database. The filter
defaults to "All databases"; selecting a single
database restricts the list to queries that ran
against that database.

The panel footer includes a pager that lets users move
through the full result set. The "Rows per page"
selector offers 10, 20, 50, or 100 rows and defaults
to 20 rows. The previous and next controls move
between pages, and the "Showing X-Y of Z" indicator
reports the range of rows on the current page and the
total number of matching queries.
