/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Metrics Query Conventions
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Metrics Query Conventions

The collector writes probe output into `metrics.*` partitioned tables.
The consolidated migration in `collector/src/database/schema.go` now adds
a `FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE
CASCADE` to each of them via `addConstraintIfMissing`, so deleting a
connection removes its metric rows with it. That was not always true,
and the queries written before it still assume orphaned metric rows can
outlive their owning connection; keep that assumption rather than
stripping the joins out, because it costs nothing and the cascade is the
only thing standing between a stale metric row and a foreign key
violation downstream.

## Filter Orphans at Query Time

Any query that reads from `metrics.*` and then feeds the result into a
table with a foreign key on `connection_id` (for example
`metric_baselines`) must INNER JOIN against `connections` in the query
itself. Do not rely on application-level filtering. Do not prune the
orphan rows - they age out with the normal partition lifecycle and a
pruning job is more expensive than the join.

```sql
-- Correct pattern
SELECT m.connection_id, ...
FROM metrics.pg_stat_activity m
JOIN connections c ON c.id = m.connection_id
WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
```

For queries that use CTEs with window functions (LAG, partitioned by
`connection_id`), place the JOIN inside the innermost CTE so orphaned
rows never enter the window computation:

```sql
WITH db_blocks AS (
    SELECT m.connection_id, m.database_name, m.blks_hit, m.collected_at,
           LAG(m.blks_hit) OVER (
               PARTITION BY m.connection_id, m.database_name
               ORDER BY m.collected_at
           ) AS prev_blks_hit
    FROM metrics.pg_stat_database m
    JOIN connections c ON c.id = m.connection_id
    WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
)
SELECT ...
FROM db_blocks
WHERE prev_blks_hit IS NOT NULL
```

The canonical example is
`alerter/src/internal/database/metric_queries.go` -
`GetHistoricalMetricValues`. Every one of its metric branches
performs the JOIN. The regression test at
`alerter/src/internal/database/queries_integration_test.go` -
`TestGetHistoricalMetricValues_FiltersOrphanedConnections` - asserts
that every branch filters orphans. When adding a new metric branch,
extend both.

## Per-Entity Counter Deltas (server)

The same rule applies to the api package, and
`queryCacheHit` and `queryDatabaseCacheHitTimeSeries` in
`server/src/internal/api/perf_summary_handlers.go` are its reference
implementations (issue #401). `metrics.pg_stat_database` stores one row
per database per sample, so a cumulative counter such as `blks_hit` must
be differenced per database before anything is summed: a `deltas` CTE
takes `blks_hit - LAG(blks_hit) OVER (PARTITION BY datname ORDER BY
collected_at)`, a `valid_deltas` CTE drops rows where either delta is
NULL (the database's first sample in the window) or negative (a stats
reset or restart), and only then does the outer query `SUM` the deltas
into `date_bin` buckets. Differencing an already aggregated value, as in
`LAG(SUM(blks_hit)) OVER (ORDER BY collected_at)`, is wrong for two
reasons that both show up in practice: a database created between two
samples adds its whole lifetime counter to that interval, and a database
dropped between two samples drives the delta negative so the interval is
discarded. The per-database tests in `perf_summary_cache_hit_test.go`
(`TestQueryCacheHit_NewDatabaseExcludedFromInterval`,
`TestQueryCacheHit_DroppedDatabaseKeepsInterval` and
`TestQueryCacheHit_ResetInOneDatabaseKeepsOther`) pin each case; the
last one is the only case where filtering per row and filtering per
bucket differ.

Two consequences for callers. A bucket whose deltas are all valid but
sum to zero block accesses is emitted with a NULL ratio rather than 0%,
so the JSON `current`, `time_series[].value` and
`aggregate.cache_hit_ratio` are all nullable (`*float64` in Go and
`nullable: true` in the OpenAPI schemas `CacheHitRatioData`,
`CacheHitRatioPoint` and `PerfAggregate`). And the bucket width is
`duration / 60` with a 10 second floor, so `current` is the last minute
of a 1h range but the last 12 hours of a 30d range; say so in any
comment or documentation that describes it.

`queryTransactions` in the same file, `BuildDerivedMetricsQuery` in
`internal/metrics/query.go` and the alerter's `queryStatsSQL` still
difference an aggregated value; #449 is reworking
`BuildDerivedMetricsQuery` to partition by entity, so do not patch it
piecemeal. Until then the web client must not derive a server-wide
ratio from `blks_hit_per_sec`/`blks_read_per_sec` requested without a
`database_name`; the server dashboard reads `cache_hit_ratio` from
`/api/v1/metrics/performance-summary` instead, passing `time_start` and
`time_end` for a custom range.

### Testing the error branches

pgx prepares statements by default, so a missing table fails inside
`tx.Query` and reaches the early return, whilst an execution-time
failure surfaces through `rows.Err()` after `rows.Next()` returns false.
To drive the latter deterministically, rename the fixture table and put
a view of the same name over it whose `blks_read` column divides by
`(blks_read - blks_read)`, which the planner cannot fold
(`installFailingCacheHitView`). A `rows.Scan` failure is not reachable
in `queryCacheHit` with real rows, because every output column is an
explicit `::float` cast or a `date_bin` over a bounded `timestamptz`;
the per-database sibling scans `datname` into a `string`, so a NULL
`datname` fixture row drives its scan-error branch.

## Schema References

Metrics tables live under the `metrics` schema
(`metrics.pg_stat_activity`, `metrics.pg_stat_database`, etc.). The
`connections` table lives in the default schema (unqualified
`connections`) and is managed by the collector. The alerter and server
both read from `connections` by unqualified name; do not add schema
qualifiers unless the caller's search_path requires it.

## Latest-Row Queries (server)

`server/src/internal/metrics/query.go` exposes a latest-row query path
alongside the bucketed `QueryTimeSeries`. It returns the most recent raw
rows of a probe table as flat `map[string]any` objects keyed by real
column name, so dashboards can read individual dimension and timestamp
values directly. The public entry point is `QueryLatestRows`; it is
decomposed into small, separately testable helpers:

- `validateLatestRowParams` validates the probe name, sort direction, and
  connection list, and clamps the limit to `[1, maxLatestRowLimit]` (100).
- `discoverLatestRowColumns` confirms the probe exists, discovers its
  columns via `GetProbeAllColumns`, strips bookkeeping columns with
  `selectLatestOutputColumns` (`connection_id`, `collected_at`,
  `inserted_at`), resolves `order_by` against the discovered columns with
  `ResolveOrderByColumn`, and resolves the database filter column with
  `ResolveDatabaseColumn` when a `DatabaseName` filter is set.
- `buildLatestRowsQuery` assembles the SQL; `scanLatestRows` reads rows
  into flat maps, normalising each value through `normalizeLatestValue`
  (RFC3339 for `time.Time`, `sanitizeFloat` dropping NaN/Inf to JSON
  null, `pgtype.Numeric`/`pgtype.Interval` collapsed to seconds/float).

### Identifier safety and the Codacy suppression

Latest-row SQL interpolates only identifiers drawn from a live-discovered
allow-list: the probe name is checked against
`information_schema.tables`, the `order_by` column against
`GetProbeAllColumns`, and every one is `QuoteIdentifier`-wrapped. All
runtime values bind through `$N` placeholders. golangci-lint/gosec is
satisfied without a `//nolint`, but Codacy's Opengrep flags the
`pool.Query(ctx, query, args...)` call in `QueryLatestRows` as
`go_sql_rule-concat-sqli`. That is a false positive; it is cleared with a
`// nosemgrep: go_sql_rule-concat-sqli` line immediately above the call,
kept alongside the existing justification comment. Use `nosemgrep`, not
`//nolint:gosec`, for Opengrep-only findings.

### Integration tests

DB-executing metrics functions are covered by integration tests in
`server/src/internal/metrics/query_db_test.go` (package `metrics`, so
they can exercise unexported helpers). They follow the api package's
gating convention: skip when `SKIP_DB_TESTS` is set or
`TEST_AI_WORKBENCH_SERVER` is unset, connect via `pgxpool`, and skip on
ping failure, so they run in the Server CI jobs and skip cleanly with no
database. Each test builds its own fixture probe table in the `metrics`
schema with a representative column mix and drops it on cleanup. Error
paths that need a failing query (for example the exists-check branch in
`discoverLatestRowColumns`) are driven with an already-cancelled context;
the scan-error branch in `scanLatestRows` is driven by passing fewer
output columns than the query returns.

## Derived Metrics in QueryTimeSeries (server)

`QueryTimeSeries` in `server/src/internal/metrics/query.go` serves both raw
stored columns and computed (derived) metrics from a single request.
`classifyMetrics` splits the requested metric names, preserving request
order, into three results: raw column names, a `[]DerivedMetric`, and the
combined output order. The routing rules are deliberate and order-sensitive:

- A name matching a real numeric column is always a raw metric; a real
  column wins even when it ends in a derived suffix.
- A name ending in one of the `derivedSuffixRules` suffixes is a derived
  metric whose prefix must be a real numeric column of a kind the rule
  accepts (see the column kind registry below): `_per_sec` is a
  `DerivedPerSec` rate and needs a `KindCounter`; `_delta` is a
  `DerivedDelta`, the per-bucket increase, and accepts a counter or either
  time kind; `_pct` is a `DerivedTimeShare`, the share of wall-clock time a
  millisecond counter advanced by (`100 * delta_ms / (elapsed_sec *
  1000)`), and needs a `KindTimeCounter`; `_sessions` is a
  `DerivedSessionAverage`, the average number of sessions in a state
  (`delta_ms / (elapsed_sec * 1000)`), and needs a
  `KindSessionTimeCounter`. A kind mismatch is a client error that names
  the column's kind (`"n_dead_tup" is a gauge, not a cumulative counter`),
  and a `_per_sec` request on a time kind points at the `_pct` or
  `_sessions` name that does make sense; a millisecond counter divided by
  seconds is a dimensionless number that only looks like a rate.
- The literal `dead_tuple_ratio` is accepted only when the probe exposes
  both `n_live_tup` and `n_dead_tup`; it is a 0-100 percentage.
- A repeated name is silently de-duplicated; anything else is a client
  error.

Each `DerivedMetric` carries a `Unit` (`"/s"`, `"%"`, `"sessions"`, and
`""` or `"ms"` for a delta depending on whether the base column counts
events or milliseconds) which `QueryTimeSeries` copies onto
`MetricSeries.Unit`; raw columns report `""`, because the client knows its
own columns.

### The column kind registry

`server/src/internal/metrics/column_kinds.go` holds `probeRegistry`, a
static map from probe name to `probeRegistryEntry`: the `kinds` of its
counter-like columns (`ColumnKind`: `KindCounter`, `KindTimeCounter`,
`KindSessionTimeCounter`, `KindWatermark`, `KindRatio`; anything unlisted,
and any column of an unlisted probe, is `KindGauge`), a `resetColumn` func
naming the `stats_reset` marker that guards a given column (`""` when the
probe has none; `pg_stat_wal` and `pg_stat_checkpointer` split between two
markers because each was consolidated from two PostgreSQL views), and an
`excludeEntities` WHERE fragment (`pg_sys_network_info` drops the loopback
interface). The exported readers are `ColumnKindFor`, `ResetColumnFor` and
`ProbeEntityExclusion`. Watermarks and ratios are listed even though they
are never differenced, purely so the error message can say why.

The registry names columns by string, so a typo would silently make a
counter a gauge. `TestProbeRegistryMatchesCollectorDDL` reads the
collector's `schema.go` (relative to the package, skipping if absent) and
checks both directions. Outwards, it fails on any registered column,
reset column or exclusion column that the `CREATE TABLE` or a later `ADD
COLUMN` does not declare, and on any probe without a `CREATE TABLE`.
Inwards (`unregisteredColumnProblems`), every numeric column of a
registered probe must be either classified in the entry or listed in the
test's `knownGaugeColumns`, so a counter column added to the collector
cannot quietly default to a gauge and be found by a user hitting the 400
on its `_per_sec`; `connection_id` is excluded everywhere, and only the
integer, numeric and floating-point types are considered, since a text,
boolean, timestamp or OID column can never be a counter. The same test
pins the metrics tables that have no registry entry at all in
`unregisteredProbeTables`, so a new probe table forces the same decision.
When a probe gains a counter column, add it to the registry in the same
change or `_per_sec` on it is refused.

The bucket aggregation is a closed set (`avg`, `sum`, `min`, `max`,
`last`) held in `query.go` as `validAggregations`, read by
`IsValidAggregation` and `ValidAggregations`. It is interpolated into SQL
as a bare function name, so both `BuildMetricsQuery` and
`BuildDerivedMetricsQuery` refuse anything outside the set themselves
rather than trusting a caller to have validated first; the HTTP handler
and the `query_metrics` MCP tool call the same validator, so there is one
list. A test may no longer force a query to fail at execution by passing a
nonsense aggregation, as it once did: use a pre-resolved
`MetricFilters.DatabaseColumn` naming a column the table does not have.

Integration fixtures create tables under their own names
(`pg_stat_all_tables_ts_test` and so on), which are not registered probes,
so each `setup*Fixture` calls `registerProbeKindsForTest(t, probe, entry)`
from `column_kinds_test.go`. The hook installs the entry under a
`sync.RWMutex` and restores or removes it in `t.Cleanup`;
`countersForTest(cols...)` builds the common all-counters entry. Without
the registration every fixture column is a gauge and the fixture's
`_per_sec` requests fail.

`BuildDerivedMetricsQuery` builds the derived SQL. It shares the bucketing,
gap-filling (`generate_series`), filtering, and `$N` argument layout of
`BuildMetricsQuery`, so the caller scans raw and derived series with the
same `scanSeriesRows`; it additionally takes the probe's entity-key
columns (see below), the probe's full column list from
`GetProbeAllColumns` (so it can tell whether a registered reset marker is
actually present), and a `maxElapsed` gap bound, which it binds as one
extra trailing argument after the filter values (only when a
counter-derived metric is requested; a ratio-only query binds nothing,
because PostgreSQL rejects an argument no placeholder uses). Per-second
base columns must stay validated against the discovered column set and
`QuoteIdentifier`-wrapped; never interpolate a caller-supplied metric name
that has not passed `classifyMetrics`. The per-sample guards are described
under "Reset guard, gap rejection, bucket clamp and fill policy" below.

`DerivedPerSec`, `DerivedTimeShare`, `DerivedSessionAverage` and
`DerivedDelta` share one `rate_samples`/`rate_buckets` CTE pair, since all
derive from the same `LAG` over consecutive samples, so a request may mix
them freely for the same or different base columns: each counter gets a
`total_i`/`prev_i` pair, then either a `rate_i` or a `delta_i` sample
column. The time share and session average are the per-second rate with a
scale factor (`rateSampleExpr` picks the numerator), so they share its
guards, its `rateAggExpr` bucket aggregation and its NULL behaviour. Two
rules are specific to deltas and must not be "tidied away":

- The bucket aggregate is always `SUM(delta_i)`; the `aggregation` request
  parameter is deliberately ignored, because averaging or taking the last of
  a set of increments under-reports the events in the bucket. A reset or the
  first sample of the window yields 0 rather than NULL, so the bucket SUM
  stays defined whenever the bucket holds any accepted sample; only an
  interval rejected by the gap bound is NULL (see below).
- The final SELECT decides a delta bucket in two branches. A bucket the
  LEFT JOIN matched (`rate_buckets.bucket_time IS NOT NULL`) reports
  `COALESCE(rate_buckets.<output>, 0)`, unless `rate_buckets.gap_i`
  (computed as `COUNT(delta_i) = 0`) says every one of its samples was
  rejected by the gap bound, which is a gap (NULL) rather than a zero
  because the events in a rejected interval are unknown. A bucket holding
  no sample reads 0 only when an accepted sample interval spans it,
  tested as `EXISTS (SELECT 1 FROM rate_samples s WHERE s.delta_i IS NOT
  NULL AND s.prev_collected_at IS NOT NULL AND s.prev_collected_at <
  all_buckets.bucket_time + $1::interval AND s.collected_at >=
  all_buckets.bucket_time)`, the standard half-open overlap between the
  interval `(prev_collected_at, collected_at]` and the bucket. That is
  what the double-counting argument actually licenses: a sample-less
  bucket saw no counter reading, and its events are counted by the next
  sample's delta, so reporting them here as well would count them twice.
  Where no accepted interval spans the bucket the argument does not
  apply, because nothing counts those events anywhere, and the bucket is
  NULL: before the probe's first sample, after its last, and through a
  collection outage, where `_delta` now breaks exactly as `_per_sec`
  does instead of drawing a run of confident zero bars. It follows that a
  connection whose probe never ran, is disabled or is failing yields all
  NULLs and the client shows its no-data state.
- `prev_collected_at` exists only to serve that span test, so it is added
  to the three `rate_samples` levels (a partitioned `LAG(collected_at)`
  innermost, passed through the middle query, `MAX(prev_collected_at)` in
  the outer one) only when a `_delta` is requested; a rate-only query's
  SQL is unchanged.

### The LAG is partitioned by the probe's entity keys

`rate_samples` is three queries deep. The innermost groups the probe rows
by `collected_at` plus the probe's entity-key columns and computes
`SUM(col)`, `LAG(SUM(col)) OVER (PARTITION BY <entity keys> ORDER BY
collected_at)` and the elapsed seconds, so each entity's counter is
differenced against its own previous reading; the middle query turns
those into per-entity `rate_i`/`delta_i` values and drops the borrowed
pre-window row; the outer query sums the per-entity values per
`collected_at`, so `rate_buckets` still sees one row per sample whatever
the entity count. Summing the counters first and differencing the sum
(the previous shape) was wrong whenever the entity set changed between
samples: on `pg_sys_network_info`, keyed per `interface_name`, an
interface being torn down dropped the sum by its lifetime total, the
negative-delta guard nulled the sample and the then-uniform
carry-forward republished the previous
throughput, whilst an interface coming up folded its whole lifetime
counter into one interval and spiked the axis. With the partition a
vanished entity simply stops contributing and a new one contributes
nothing until its second sample. A per-entity rate that is NULL (that
entity reset) drops out of the per-sample `SUM`, so the total rate loses
only that entity's share.

The entity keys come from `GetProbeEntityKeyColumns`: the columns of the
probe table's primary key other than `connection_id` and `collected_at`,
in key order (`interface_name`; `datname`; `database_name, queryid,
userid, dbid, toplevel`; nothing for a single-row probe such as
`pg_stat_wal`, which then reduces to the unpartitioned form). Every
`metrics.*` table declares such a key. Do not substitute
`EntityKeyColumns`/`IsEntityKeyColumn` here: those treat every text
column as identity, and `pg_stat_wal.last_archived_wal` or
`pg_sys_network_info.ip_address` would fragment the partition so that
every sample became a first sample. `QueryTimeSeries` discovers the keys
only when `needsEntityKeys` reports a counter-derived metric in the
request; the ratio reads absolute values and ignores them. The keys are
validated with `IsValidIdentifier` and `QuoteIdentifier`-wrapped; they
are identifiers from `information_schema`, never bound values, so the
`$N` layout is unchanged. Fixture tables for the derived path must
therefore declare a primary key when they mean to exercise the
partition; the existing table-probe fixtures declare none and run
unpartitioned.

### The rate/delta sample query reaches one sample before the window

The `rate_samples` CTE differences consecutive samples with `LAG`, so the
first sample inside the window has no predecessor and any counter increase
between the last sample before the window start and it would be lost: a
`NULL` rate and a zero delta, and a first bucket that under-reports. The
inner sample query therefore widens its lower bound with
`metricQueryParts.lookbackWhere`, which replaces `collected_at >= $3` with
`collected_at >= COALESCE((SELECT MAX(collected_at) FROM metrics.<probe>
WHERE connection_id = $2 AND collected_at < $3 AND collected_at >= $3 -
<lookbackBound> AND <same dimension filters>), $3)`, and the CTE's middle
`WHERE collected_at >= $3` then drops the borrowed sample again so it never
becomes a bucket of its own. The `COALESCE` fallback means a window with
no earlier sample within reach behaves exactly as it did before the
lookback existed. The collector stamps every entity of one probe run
with the same `collected_at`, which is why one borrowed timestamp serves
every partition.

The search is bounded below by the `lookbackBound` constant,
`GREATEST($1::interval * 3, INTERVAL '30 minutes')`: the larger of three
bucket widths and a 30-minute floor, expressed in the query's own bucket
argument so it adds nothing to the argument list. The bound exists
because a predecessor left behind by a long collection gap inflates the
first bucket: a collector down for three days followed by a 1h window on
`xact_commit_delta` reported three days of commits in the first bucket
and autoscale flattened every real bucket, and the `_per_sec` form,
though damped by dividing by the true elapsed time, presented a
three-day average as the first bucket's rate. The multiplier is small
because a borrowed sample k widths back can inflate the first bucket to
k times a normal one, but one width is too few (a probe interval only
slightly longer than the bucket would never resolve). The floor covers
the 1h preset, where 150 buckets are 24s wide and the coarsest charted
counter probes (`pg_stat_wal`, `pg_stat_checkpointer`,
`pg_sys_network_info`) run every 600s; on those windows the sample-less
buckets already read zero, so a first bucket carrying up to 30 minutes
of increase is at most a few teeth tall. At the 7d and 30d presets the
bucket term dominates (3.4 and 14.4 hours), so an outage of a day or
more falls outside the bound and the window starts from zero. Beyond the
bound the first bucket under-reports by one probe interval, which is the
lesser harm. Change the constant only together with its justification
comment in `query.go` and `TestLookbackBound`.

Since #402 the gap bound (three probe intervals, next section) decides
whether a borrowed predecessor actually counts: the lookback bound only
limits how far the `MAX(collected_at)` scan reaches, and a predecessor it
finds is still rejected as a gap when it is more than three probe
intervals before the first in-window sample. Because the bucket clamp
keeps every bucket at least one probe interval wide, "three bucket
widths" is never shorter than "three probe intervals", so a predecessor
beyond the bucket term of `lookbackBound` would have been rejected
anyway; the 30-minute floor still matters only in that it lets a
predecessor be found on a short window, after which the gap rule has the
final say. The two cases render differently: a predecessor out of the
lookback's reach leaves the first sample with no `LAG`, so its delta is 0
and its rate NULL, whereas a borrowed-but-rejected predecessor makes the
first bucket a gap (NULL delta and NULL rate).

Three things must stay true. The lookback subquery repeats the same
dimension filters as the outer clause, or the LAG would subtract another
entity's counter. It reuses the existing `$1`/`$2`/`$3` placeholders and
adds no arguments, so the `$N` layout stays shared with
`BuildMetricsQuery`. And neither the raw-column path nor the
`dead_tuple_ratio` CTE looks back at all: both read absolute values rather
than differences, so an out-of-window row would simply be wrong.

`metricQueryBase` now delegates to `metricQueryClauses`, which returns a
`metricQueryParts` holding the dimension filter clauses and the argument
list separately; `where()` reassembles the standard clause and
`lookbackWhere()` the widened one. Add a new dimension filter in
`metricQueryClauses` and both of those clauses pick it up, so the
bucketed, rate and delta builders all scope alike; the latest-row path is
the exception, because `buildLatestRowsQuery` assembles its own WHERE
clause from the same `MetricFilters` value and needs the clause adding
there too, or the filter scopes the chart but not the KPI tile behind it.

Two ordering rules govern `metricQueryClauses`. Every clause increments
`argNum` except the last one, and the `QueryID` clause is deliberately
last and deliberately omits the increment, with a comment saying so; a new
filter therefore goes above it and carries its own `argNum++`, leaving
that comment attached to whichever clause is genuinely last. And the
dimension filters apply no probe-column-existence check: `SchemaName`,
`TableName`, `IndexName` and `MountPoint` (issue #428, `mount_point`, so
the Disk Space chart can scope to one real filesystem rather than
averaging every mounted one) all fail at execution time against a probe
lacking the column, which keeps the validation semantics identical across
dimensions.

### Reset guard, gap rejection, bucket clamp and fill policy

Issue #402 added four rules that `QueryTimeSeries` applies to every
request. They all hang off the probe's collection interval, resolved once
per request by `ResolveProbeInterval` (`probe_interval.go`): the
server-scope `probe_configs` rows for the requested connections, with the
global row standing in for any connection without one, then
`DefaultProbeInterval` (300 s, the collector's own fallback); the largest
interval wins because one bucket width serves every series in the
response.

That row says how the collector runs now, whilst the stored samples were
taken under whatever configuration was in force when they were collected,
so `ResolveEffectiveInterval` pairs it with `ObserveSampleSpacing` and
returns both the configured interval and an effective one, the larger of
the two. `ObserveSampleSpacing` measures the samples in the window
themselves: per connection the smallest positive gap between consecutive
distinct `collected_at` values, and across connections the largest of
those, or zero when no connection has two samples. The smallest gap is
deliberate, because a genuine outage is one wide interval among tighter
ones and leaves the minimum alone, so the effective interval only widens
when every sample in the window is spaced more widely than the
configuration claims. The guards that judge samples (the counter gap
bound and the gauge carry) use the effective interval, so tightening a
probe's interval cannot retroactively blank the history collected at the
old, wider one; the bucket clamp keeps using the configured interval,
since widening it on sparse history would coarsen the whole chart rather
than fix a gap.

The table is the collector's, so the metrics package tests create a
minimal `probe_configs` with `setProbeIntervalForTest`
(`probe_interval_db_test.go`) when the test database lacks the collector
schema, and every fixture registers its probe at 60 s (or per connection
for the lookback fixture) so minute-spaced samples keep their exact
bucket assertions; without a row a fixture would resolve to 300 s and a
1h window would clamp to twelve 300 s buckets. A server-scope row needs
its connection to exist, because another package's fixture creates
`probe_configs` with a foreign key on `connection_id` and leaves the table
behind, so `ensureConnectionForTest` adds the referenced row first. It
never creates the `connections` table: other packages create their own
with a plain `CREATE TABLE`, and with no such table there is no foreign
key to satisfy either.

- Reset guard: for each counter-derived metric whose
  `ResetColumnFor(probe, col)` is non-empty and whose marker column is in
  the `allCols` list, the innermost sample query adds `MAX(reset) AS
  reset_i` and `LAG(MAX(reset)) OVER (<partition> ORDER BY collected_at)
  AS prev_reset_i`, and the per-sample CASE adds `reset_i IS NOT DISTINCT
  FROM prev_reset_i`, so a `stats_reset` change between consecutive
  samples invalidates the interval even when the counter happens to read
  higher; `IS NOT DISTINCT FROM` keeps an interval whose marker is NULL on
  both sides valid. `_per_sec`, `_pct` and `_sessions` become NULL, and
  `_delta` contributes 0, exactly as for a negative delta. An absent
  marker column (a collector schema predating migration 10) is dropped
  silently and the negative-delta guard alone applies;
  `MinCollectorSchemaVersion` was deliberately not bumped.
- Gap rejection: `MaxElapsedIntervals = 3`; `QueryTimeSeries` passes
  `maxElapsed = 3 * effective interval` and the builder binds it as the
  trailing argument. The rate CASE is `CASE WHEN (total_i - prev_i) >= 0
  AND elapsed_sec > 0 AND elapsed_sec <= $N::float8 [AND reset_i IS NOT
  DISTINCT FROM prev_reset_i] THEN <expr> END`; the delta CASE is `CASE
  WHEN prev_i IS NULL THEN 0 WHEN elapsed_sec > $N::float8 THEN NULL WHEN
  (total_i - prev_i) >= 0 [AND reset guard] THEN (total_i - prev_i) ELSE
  0 END`. A first-ever sample (no `LAG`) still contributes 0, so the
  start of a probe's history reads as zero events, not as a gap; only a
  known-too-long interval is NULL. The bucket-level `gap_i` flag turns an
  all-rejected bucket into a NULL bucket (see the delta rules above).
- Loopback exclusion: `metricQueryClauses` takes the probe name and
  appends `ProbeEntityExclusion(probe)` (a fixed fragment with no bound
  value, so the `$N` layout is unchanged) to the filter clauses, which
  `where()` and `lookbackWhere()` both emit, so the raw query, the derived
  sample query and its lookback subquery all leave `lo`/`lo0` out of
  `pg_sys_network_info`.
- Bucket clamp: after resolving the interval, `maxBuckets = max(1,
  window / configured interval)` and `buckets = min(buckets, maxBuckets)`,
  applied to raw and derived queries alike so series stay aligned. A
  bucket narrower than the interval cannot hold a sample of its own and
  only produced the "comb" of zero and carried buckets. `generate_series`
  is inclusive of the window end, so a clamped 1h window on a 300 s probe
  returns 13 points (12 buckets plus the end), as before the clamp for any
  bucket count.
- Fill policy and null points: `MetricDataPoint.Value` is a
  `*float64` (JSON `"value": null`). `scanSeriesRows` takes a
  `[]fillPolicy` parallel to `names` plus the interval: `fillGauge` (raw
  columns and `dead_tuple_ratio`, via `derivedFillPolicy`) repeats the
  last real value whilst `bucketTime - lastSeenTime <= MaxCarryIntervals
  * effective interval` (`MaxCarryIntervals = 3`) and emits nil beyond
  that; `fillNone` (`_per_sec`, `_delta`, `_pct`, `_sessions`) never
  repeats. A carried point is marked `Filled: true` (JSON
  `"filled": true`, `omitempty`, so it is absent on observed and null
  points), which is how a chart tells a carried stretch from an observed
  one; `fillNone` never sets it.
  Every bucket is emitted for every series, nulls included, so all series
  in a response have identical length and bucket times; a series whose
  query matched no rows is all nulls rather than empty, and the client
  treats an all-null series as no data. There is no shared `lastKnown`
  map any more: the carry state is local to one `scanSeriesRows` call,
  which is fine because raw and derived names never overlap.

`scanSeriesRows` scans a bucket-time-plus-N-values result set and treats a
NULL bucket or a non-finite sample identically under the fill policy.

### NaN/Inf handling: finiteFloat, not resolveMetricValue

Non-finite samples (NaN, +/-Inf) must be treated as gaps, never plotted,
because `encoding/json` cannot marshal them and one bad value would blank
the whole response. This guard lives in `toFloat64` via the `finiteFloat`
helper: `toFloat64` returns `(0, false)` for a non-finite float64/float32/
`pgtype.Numeric`, and `scanSeriesRows` then treats `!ok` as a gap (a
gauge carry-forward within the carry bound, otherwise a null point). An
earlier design
(#339) guarded NaN/Inf in a `resolveMetricValue` helper inside the scan
loop; that was retired in favour of guarding inside `toFloat64`, because the
`toFloat64` guard is broader and benefits every caller, including the
latest-row path's `normalizeLatestValue`/`sanitizeFloat`. Do not
reintroduce `resolveMetricValue`. Note that `toFloat64` still converts
`pgtype.Interval` through `intervalToSeconds` (handling Days/Months); the
`finiteFloat` guard and `intervalToSeconds` are independent and both must
remain.

### Derived-path integration tests

The derived path and `scanSeriesRows` are covered by
`server/src/internal/metrics/query_timeseries_db_test.go` (same gating
convention as `query_db_test.go`; every fixture registers its counter kinds
through `registerProbeKindsForTest`). Its fixture inserts minute-spaced
samples with counters rising 60 per minute (a clean 1.0/sec rate) and
constant live/dead tuple counts (a steady 10% ratio), then exercises raw,
`_per_sec`, `dead_tuple_ratio`, and mixed requests end-to-end. Both
fixtures return the minute-truncated base time their offsets hang off, so a
test can build a window with `windowSince(base, minutes)` that lines up
exactly with the samples instead of re-reading the clock; the lookback
tests use a window that opens on the second sample, leaving the first just
outside it, and assert the first bucket carries the increase since that
outside sample, plus a companion case whose window opens on the earliest
sample of all and so has a null first rate bucket. A second fixture in the
same file covers `_delta` with a deliberately awkward progression: a first
sample with no `LAG`, a minute carrying no sample at all (a two-interval
spacing, inside the gap bound), and a counter reset, asserting the exact
non-zero per-bucket deltas, the 0 fill inside the spanned bucket, the
nulls over the hour before the first sample and after the last, and that
the reset contributes nothing; it registers a `stats_reset` marker its
table does not have, to exercise the silent drop. A third,
`setupNetworkFixture`, is shaped like `pg_sys_network_info` with its real
primary key, the real probe's
loopback exclusion and a `lo` row on every sample, and an interface set
that changes under the window (one interface resets and is then torn
down, another appears carrying a large lifetime counter); it asserts the
exact per-minute totals for both `_delta` and `_per_sec`, that the raw
path ignores `lo` too, that a connection with no rows and a window with
no samples each yield all-null series, and that a window opening on the
probe's very first sample fills that sample's bucket with 0 and leaves
the bucket after it null, nothing having spanned it. A fourth,
`setupLookbackFixture`, holds one scenario per connection for the
lookback bound: a reset straddling the window
boundary, a predecessor beyond the 30-minute floor (not borrowed, first
delta 0) and one inside it but beyond three 60 s intervals (borrowed and
rejected, first bucket a gap), and, on hourly connections, a predecessor
beyond and one inside three 1-hour buckets. A fifth, `setupTimeShareFixture`, is
shaped like `pg_stat_database`'s time columns with `blk_read_time`
registered as a time counter and `active_time` as a session time counter,
each rising 30000 ms per minute, so `blk_read_time_pct` is exactly 50 and
`active_time_sessions` exactly 0.5; it also asserts the `Unit` on each
series and the kind-mismatch errors end to end. A sixth,
`setupGuardFixture` in `query_guards_db_test.go`, carries a real
`stats_reset` column and one scenario per connection for #402: a marker
change with a rising counter (null rate, zero delta), a four-minute gap
followed by a two-minute one (rejected, then accepted), a gauge that
stops reporting (carried for three buckets, then null), and a connection
with a 300 s server-scope interval for the bucket clamp (13 points on a
1h window with 150 requested). Its last case is the regression for the
effective interval: it reads the rate and delta of the minute-spaced
connection, tightens the global `probe_configs` row to 10 s with the
samples untouched, and asserts the same points come back, where before
`ResolveEffectiveInterval` the series went blank. The pointer-valued
points have helpers: `pointAt` fails on a null, `assertNullAt` demands
one, `nonNull`, `sumValues` and `assertAllNull` cover the rest. Asserting
exact values at exact bucket times needs the window anchored on the
minute (`windowSince`), not on `time.Now()`, or the bucket boundaries
drift off the samples.
Minute-spaced samples always land in distinct 60-second buckets whatever
the window origin is, which is what makes those exact assertions safe. The
two `scanSeriesRows` error returns in `QueryTimeSeries` are driven
deterministically by a `MetricFilters.DatabaseColumn` naming a column the
probe table does not have, which builds valid SQL that fails at
execution; `scanSeriesRows`'s own
`pool.Query` and `rows.Scan` error branches are driven by a cancelled
context and a destination-count mismatch respectively.

## Alerter Metric Lookup Errors

`GetLatestMetricValues` (`alerter/src/internal/database/metric_queries.go`)
distinguishes three failure modes, and callers must keep them apart:

- `ErrMetricNotSupported` - the metric has no `metricRegistry` entry, or
  its entry carries an unknown scan type. Metrics evaluated by bespoke
  code paths, notably `probe_staleness_ratio`, are deliberately absent
  from the registry and always fail this way.
- `ErrNoMetricData` - the registry query ran and returned no rows.
- Any other error - the query itself failed.

When `GetLatestMetricValues` returns an error, `checkAlertResolved`
(`alerter/src/internal/engine/cleanup.go`) clears an alert only for
`ErrNoMetricData`; it still clears on its other, non-error paths, when
the metric reports no value for the alert's connection or database, or
when the current value no longer violates the threshold. An
unsupported metric or a failed query
says nothing about whether the alerting condition still holds, so the
alert is left active and logged. Treating those errors as a resolution
was the root cause of issue #405, in which `metric_staleness` alerts were
cleared by the cleaner and immediately re-raised by the evaluator, once
per cycle, for as long as a probe stayed stale.

Before the metric is queried, the cleaner applies the same
`required_extension` gate the evaluator does. `cleanResolvedAlerts` calls
`resolveExtensionGates` once per pass: it walks the active threshold
alerts, loads each distinct rule with `GetAlertRuleByID` and, for each
distinct `required_extension`, asks `GetConnectionsWithExtension`
(`alerter/src/internal/database/queries.go`) which connections list that
extension in their newest `metrics.pg_extension` snapshot. The resulting
rule id to connection set map is passed into `checkAlertResolved`, where
a nil set means the rule is ungated; a connection outside the set leaves
the alert active and logs at debug level, because the alerter can no
longer see the condition rather than knowing it has resolved, so an
operator who uninstalls the extension keeps the alert until they
acknowledge or clear it. A failed rule or extension lookup is logged and
leaves the rule ungated. Resolving per rule rather than per alert keeps a
pass at one rule lookup per distinct rule and one extension lookup per
distinct extension however many alerts are open. The evaluator applies
the same gate the other way round: `evaluateRuleForAllConnections`
resolves the set once per rule through `connectionsWithRequiredExtension`
and skips any connection not in it, and a lookup error evaluates without the gate so a datastore
fault cannot silence every gated rule. The `pg_extension` probe is change
tracked and stores one snapshot per connection, all databases sharing a
`collected_at`, so the query keys on `MAX(collected_at)` per connection
with no time window; a connection with no rows at all is treated as
lacking the extension on both paths. See GitHub issue #409.

Probe-scoped alerts (those with a non-NULL `probe_name`) never reach the
registry path at all: `checkAlertResolved` routes them to
`checkStalenessAlertResolved`, which re-reads
`GetProbeStalenessByConnection` and clears the alert when the probe's
staleness ratio no longer violates the stored threshold, or when the
probe stops being reported. Their evaluator,
`evaluateMetricStaleness` (`alerter/src/internal/engine/thresholds.go`),
keys both the active-alert lookup and the cooldown check by probe via
`GetActiveThresholdAlertForProbe` and `GetRecentlyClearedAlertForProbe`,
so several stale probes on one connection raise one alert each.

## Alerter Metric Registry (alerter)

`alerter/src/internal/database/metric_registry.go` maps each alert rule's
metric name to the SQL that produces its value. A query error there is
swallowed by `evaluateRuleForAllConnections` in
`alerter/src/internal/engine/thresholds.go`, which logs at debug level and
moves on, so a broken metric looks exactly like an idle one. Four rules
that follow from that, all learned the hard way in #406:

- The metric name is not the table name. `pg_stat_archiver.*` metrics read
  `metrics.pg_stat_wal`, because the collector consolidates the archiver
  columns (`archived_count`, `failed_count`, `last_failed_wal`) onto that
  table and never creates a `metrics.pg_stat_archiver`. Check the CREATE
  TABLE statements in `collector/src/database/schema.go` before writing a
  FROM clause; there is one `metrics.*` table per probe and no views.

- Never put a max-age predicate on `metrics.pg_settings`. The probe is
  change-tracked (`collector/src/probes/pg_settings_probe.go`) and skips
  the write whenever the settings hash is unchanged, with no heartbeat, so
  a stable server stores one snapshot at onboarding and nothing after.
  Take the newest row per connection with
  `DISTINCT ON (connection_id) ... ORDER BY collected_at DESC` instead,
  and remember that a LEFT JOIN onto settings with COALESCE defaults hides
  the failure by silently substituting the shipped defaults.

- Counter deltas are windowed, and the window must comfortably exceed the
  probe interval. Summing positive per-sample deltas over an hour is the
  pattern to copy (see the archiver, checkpointer, `deadlocks_delta` and
  `temp_files_delta` entries): it survives a stats reset, it reports 0
  rather than dropping the connection when only one sample lands in the
  window, and it gives the rule an absolute per-hour count rather than a
  per-probe-interval figure. Rules whose value is a rate should say so in
  `metric_unit`, for example `checkpoints/hour`. Where such an entry also
  has a `historicalSQL`, compute the per-sample delta over the whole
  lookback first and then group by the hour bucket, so baselines see the
  same per-hour figure the latest query reports and the first sample of
  an hour is paired with the last of the previous one;
  `metric_registry_hourly_deltas_integration_test.go` pins the interval
  independence, reset, single-sample, bucket, session timezone and
  sample count behaviour. Three details of the historical query are easy
  to get wrong. Filter inside the aggregate,
  `SUM(GREATEST(x - prev_x, 0)) FILTER (WHERE prev_x IS NOT NULL)`, as
  the latest query does: a `WHERE prev_x IS NOT NULL` ahead of the
  `GROUP BY` drops the whole bucket, so a newly onboarded connection
  with one sample contributes no baseline row at all instead of a zero.
  Truncate on the UTC value,
  `date_trunc('hour', collected_at AT TIME ZONE 'UTC') AT TIME ZONE
  'UTC'`: nothing pins the alerter's pool to UTC, the collector writes
  UTC, and a server whose `TimeZone` GUC carries a fractional offset
  would otherwise cut buckets at half past. Report how many raw samples
  each bucket covers, through the `historicalScanWithDBAndSamples` scan
  type and `HistoricalMetricValue.SampleCount`, because
  `calculateAllBaseline` persists the sum of that field as the
  baseline's `sample_count` and `anomaly.tier1.warmup.*.min_samples` is
  counted in samples: one row per hour would leave these baselines short
  of the default `all` threshold of 100 at the shipped seven day
  lookback, and unable to warm at all at a lookback of four days or
  fewer. No
  registry entry reports a per-probe-interval delta any more (#409), and
  `table_bloat_ratio` was removed from the registry in the same change:
  its rule row survives, disabled, so historical alerts stay
  attributable, and re-enabling it logs "No data for metric" at debug.

- `system_stats` columns are platform-specific. `processor_time_percent`,
  `user_time_percent`, `privileged_time_percent` and
  `interrupt_time_percent` are Windows-only and NULL on Linux; the Linux
  build populates the per-mode buckets and `idle_mode_percent`. Use the
  shared `cpuBusyPercentExpr` rather than coalescing a platform-specific
  column to zero, which reads as an idle host.

- A MAX across the mounts of `metrics.pg_sys_disk_info` has to exclude
  pseudo filesystems. `pg_sys_disk_info.used_percent` takes the fullest
  mount, which is the right threshold semantics, but a squashfs image is
  by construction 100% used, so on any host with a snap installed the
  metric was pinned at 100% and the disk alert fired permanently (#428).
  The `pseudoFilesystemExclusion` constant in `metric_registry.go` holds
  the predicate and both the `latestSQL` and the `historicalSQL` embed
  it, so the two cannot drift. It is
  `COALESCE(file_system_type, '') NOT IN (...)` and the COALESCE is
  load-bearing: the column is nullable and a bare `NOT IN` against NULL
  yields NULL rather than true, which would drop every mount whose type
  the probe did not record.
  `metric_registry_disk_mounts_integration_test.go` pins the exclusion,
  the NULL-type retention and the fragment's presence in both queries;
  the shared fixture in `queries_integration_test.go` therefore carries
  `mount_point` (defaulted, since the older cases insert one unnamed
  mount) and a nullable `file_system_type`.

Changing a seeded rule's threshold or unit needs a collector migration as
well as the registry edit, because migration 1 only seeds a fresh install.
Migration 8 is the worked example: it rewrites descriptions and units
unconditionally, but only rewrites a threshold that still carries the old
shipped default, so operator tuning survives the upgrade. Migration 11
follows the same shape for `deadlocks_detected` and `temp_files_created`
(hourly units, thresholds untouched) and shows how to retire a built-in
rule: the row is kept with `default_enabled = FALSE` and a description
starting `Retired:`, and its `active` and `acknowledged` alerts are set to
`status = 'cleared', cleared_at = NOW()`, matching the alerter's
`ClearAlert`. Each such migration has a `migration_vN_test.go` covering
registration, the fresh-install seed values and the upgrade path.

## Time-Window Resolution (server)

Every dashboard metrics query runs against an absolute window, and the
window is resolved exactly once, at the HTTP boundary. The types live in
`server/src/internal/metrics/query.go`:

- `ParseTimeRange(timeRange string) (time.Time, time.Time, error)`
  handles the rolling presets in `ValidTimeRanges` (`1h`, `6h`, `24h`,
  `7d`, `30d`) and always ends at now.
- `TimeWindow{Start, End time.Time}` is the resolved window.
- `ResolveTimeWindow(timeRange, startISO, endISO string) (TimeWindow,
  error)` is the entry point for `/metrics/query`. A `timeRange` other
  than `custom` ignores the timestamps and delegates to `ParseTimeRange`;
  `custom` requires both timestamps in RFC 3339 form, parses them, and
  hands them to `ResolveCustomWindow` naming `time_start`/`time_end`.
- `ResolveCustomWindow(start, end time.Time, startParam, endParam
  string) (TimeWindow, error)` is the single source of truth for the
  custom-window rules on already-parsed timestamps. The parameter names
  are only used in error messages, so each endpoint reports the field
  the client actually sent.
- `QueryTimeSeries` takes a `metrics.TimeWindow`, not a time-range
  string, and does no window validation of its own. The
  `timeSeriesQueryFunc` seam in
  `server/src/internal/api/metrics_handlers.go` mirrors that signature,
  so test fakes must accept a `metrics.TimeWindow` and return a
  `*metrics.MetricsQueryResult`.

### The metrics/query response envelope (#430)

`QueryTimeSeries` returns `*MetricsQueryResult`, not a bare
`[]MetricSeries`, and `GET /api/v1/metrics/query` serves it as the JSON
object `{probe_name, connection_ids, time_range, time_start, time_end,
bucket_seconds, buckets, aggregation, series}`. The window fields let a
chart anchor its axis to the range that was requested rather than to
whichever points came back, which on a young instance is what made every
range render identically.

- `time_start`/`time_end` are the resolved absolute window, so a preset
  and a custom range are described identically.
- `time_range` is the range parameter as received, defaulted to `1h`.
  The query layer never sees it, so `handleMetricsQuery` sets it on the
  returned struct after the query; a fake query function may leave it
  empty.
- `bucket_seconds` is `BucketWidth(timeStart, timeEnd, buckets)` in whole
  seconds, computed from the bucket count *after* the interval clamp, and
  `buckets` is that clamped count.
- `BucketWidth(timeStart, timeEnd time.Time, buckets int) time.Duration`
  in `query.go` is the one place the width is decided: span / buckets,
  floored at `MinBucketWidth` (one second), with a bucket count below one
  treated as one. `BuildMetricsQuery`, `BuildDerivedMetricsQuery` and the
  envelope all call it, so the reported width is always the width the SQL
  binned by. Never re-inline the division.

The latest-row mode (`handleLatestRows`, reached when `limit` or
`order_by` is present) is a different response shape and is deliberately
untouched by this envelope.

The custom-path validation order matters, because the error message is
returned to API clients verbatim: missing timestamp, unparsable
timestamp, `End` not after `Start`, `Start` at or after now, then the
span cap. An `End` in the future is clamped to now rather than rejected,
because a picker set to the current day routinely overshoots by minutes.
The span cap (`MaxCustomTimeSpan`, 366 days) is a resource-exhaustion
guard, not a nicety: `BuildMetricsQuery` derives the bucket width from
the span, so an unbounded window turns one request into an arbitrarily
large scan.

The rules are shared, not metrics-only. `GET /api/v1/timeline/events`
takes absolute `start_time` and `end_time` values; `resolveTimelineWindow`
in `server/src/internal/api/timeline_handlers.go` parses them with
`RequireQueryTime` (keeping that helper's `start_time is required` and
`Invalid start_time format, expected RFC3339` messages) and then calls
`metrics.ResolveCustomWindow(start, end, "start_time", "end_time")`, so
a future start is a `400`, a future end is clamped, and the span cap
message is identical on both endpoints. Do not re-implement any of these
checks inline in a handler; the `api` package already depends on
`metrics`, so there is no cycle.

`GET /api/v1/metrics/query`, `GET /api/v1/metrics/connection-groups`,
`GET /api/v1/metrics/performance-summary` and
`GET /api/v1/metrics/query-stats` accept `time_range=custom` alongside
`time_start` and `time_end`, resolve the window through
`ResolveTimeWindow` and map any resolution error to `400`;
`performance-summary` derives its bucket width from the resolved
window (span / 60, 10 second floor). The database-summaries handler
in `perf_summary_handlers.go` still uses the inline `validTimeRanges`
map and accepts presets only; consolidating that is deliberately
deferred.

Every handler that accepts a `queryid` parameter (`/metrics/query`,
`/metrics/latest`, `/metrics/top-queries` and `/metrics/query-stats`)
parses it with `parseQueryIDFilter` in `metrics_handlers.go`, which
returns a `*int64` and answers `400` to anything that is not a 64-bit
integer. The value is bound uncast as `queryid = $N`, never through a
`queryid::text` cast. Responses still carry the identifier as a decimal
string, because JavaScript cannot represent it exactly.

Whether the `(connection_id, database_name, queryid, collected_at)`
object index actually serves a queryid lookup depends on the predicate
also naming `database_name`, the index's second column. Before
PostgreSQL 18, which added B-tree skip scans, a predicate on
`connection_id` and `queryid` alone falls back to a bitmap scan of
`idx_pg_stat_statements_conn_time` over every row of the connection in
the window. `/metrics/query` and `/metrics/latest` always carry the
database name; `/metrics/top-queries` reads one snapshot by
`collected_at` and does not need the object index; `/metrics/query-stats`
takes an optional `database_name` parameter, which the drill-down always
sends, and `buildQueryStatsSQL` binds it as an extra predicate so that
the object index applies.

## Cumulative Counter Deltas per Identity (server)

`pg_stat_statements` keeps one row per `(database_name, userid, dbid,
toplevel)` for a queryid, and each row is an independent cumulative
counter that `pg_stat_reset()` or a restart can zero on its own. Any
query that turns those counters into per-period deltas must `LAG` within
each identity, drop the negative deltas, and only then sum across
identities. Summing first hides a reset in one identity behind growth in
its siblings, so the guard never fires and the pre-reset total is
subtracted from the post-reset one. `queryStatsSQLTemplate` in
`perf_summary_handlers.go` is the reference implementation, and
`TestQueryStats_PerIdentityReset` is the regression test; any new
counter-delta query over `metrics.pg_stat_statements` should follow the
same shape.

## Bounded Activity Lookups (server)

`buildTopQueriesSQL` resolves `dbid` and `userid` OIDs to names through
`DISTINCT ON (...) ... ORDER BY oid, collected_at DESC` over
`metrics.pg_stat_activity`, which has no index on `datid` or
`usesysid`. Without a `collected_at` bound that sort covers every
activity row in retention for the connection, and it runs twice per
page (count and page statements). Both CTEs are therefore anchored to
the latest `pg_stat_statements` snapshot and read only the preceding
`nameLookupWindowSQL` (one hour) of activity samples; on a fixture of
500,000 activity rows that took the page from 1.6 s and a 29 MB
external sort to 8 ms in memory. Any new lookup over
`pg_stat_activity` needs the same bound.

The third CTE in the same statement, `last_client`, follows that rule:
it takes `DISTINCT ON (query_id, datid, usesysid)` over the same window,
ordered by `collected_at DESC`, and is `LEFT JOIN`ed on all three of
`pss.queryid`, `pss.dbid` and `pss.userid` to fill the `client_addr`
(via `host(client_addr)`, the repo's convention for rendering `inet`),
`client_hostname` and `client_observed_at` columns of `TopQueryRow`.

The three-way key is not optional. `pg_stat_statements` is keyed on
`(userid, dbid, queryid, toplevel)`, so one `queryid` can appear once per
database and role that ran the statement; keying `last_client` on the
identifier alone picks whichever backend was seen most recently across
every database and role, and a caller filtering on one database can then
be shown a client that only ever connected to another. `DISTINCT ON`
runs over the same tuple the join matches on, so the CTE holds at most
one row per tuple and the join cannot fan out `deduped` or the count.

`metrics.pg_stat_activity.query_id` is collected from schema version 12
(issue #384) and is NULL before PostgreSQL 14, with `compute_query_id`
off, and on idle backends, so the join resolves nothing on such servers.
Because `pg_stat_activity` is sampled, the attribution is the most
recently observed client, not the only one; keep any UI wording to
"last observed client". `client_addr` is rendered
`COALESCE(host(client_addr), 'local')`, so it is non-null for every
observed row and a statement never caught in flight is signaled solely
by a null `client_observed_at`; both the `TopQueryRow` doc comment and
the OpenAPI descriptions say so, and any UI logic must test
`client_observed_at` rather than the address.

The optional `queryid` filter is applied inside `last_client` as well as
inside `deduped`, bound to the same placeholder rather than to a second
parameter, because the query drill-down always supplies one and would
otherwise sort the whole one-hour window twice per request to retrieve a
single row. `metrics.pg_stat_activity` carries no index on `query_id`;
an index on `(connection_id, query_id, collected_at DESC) WHERE query_id
IS NOT NULL` may be worth profiling, bearing in mind that the table is a
partitioned parent and a partitioned parent cannot take `CREATE INDEX
CONCURRENTLY` in a single statement.

## Latest-Snapshot Aggregations (server)

Some dashboard panels want a point-in-time picture rather than a time
series. `GET /api/v1/metrics/connection-groups`
(`server/src/internal/api/perf_summary_connection_groups.go`) is the
reference implementation: it aggregates the single newest `collected_at`
for a connection inside the requested `time_range`, so the range only
selects which snapshot counts as the latest and nothing is averaged or
peaked across it. The pattern is a `latest` CTE holding
`MAX(collected_at)`, a `snapshot` CTE joined to it, and the aggregation
on top; `collected_at` is returned to the client so the UI can show the
age of what it is displaying, and it is null when the window held no
snapshot.

### Bound the partition key in every CTE, not just the first

`metrics.*` probe tables are `PARTITION BY RANGE (collected_at)`, so any CTE
that reads one must constrain `collected_at` with the window bounds
*directly*. Constraining it only indirectly, by joining to a CTE that
already narrowed it (`JOIN latest l ON psa.collected_at = l.collected_at`),
gives the planner nothing to prune with, and it keeps every retained
partition in the plan. Repeating the bounds is a semantic no-op but a
substantial planning win.

Measured on a 90-daily-partition fixture with `plan_cache_mode =
force_generic_plan` (the mode pgx's statement cache settles into), for
`connection-groups`: without the repeated bounds the snapshot `Append`
carried all 90 partitions as sub-plans and the plan ran to 283 lines with
104 partition scan nodes; with them, both `Append` nodes reported
`Subplans Removed: 88`, leaving 2 sub-plans each and a 54-line plan with no
partition scan nodes at all. Mean per-execution latency over 300 executions
fell from 0.575 ms to 0.445 ms, and the result sets were byte-identical
across all five time ranges and both connections. Note that execution-side
buffer counts were the same either way (`shared hit=13`), because runtime
pruning already stopped the executor from touching the irrelevant
partitions; the win is in plan size and per-execution node setup, and it
grows with the partition count.

### Bound group cardinality

Where a grouping key is influenceable from outside the workbench, cap the
row count. For `connection-groups`, `group_by=client` has cardinality up to
the monitored server's `max_connections`, and anyone who can reach that
server from many source addresses can inflate it, so the final `SELECT`
carries `LIMIT maxConnectionGroups` (200). With `total DESC` ordering,
truncation only ever discards the smallest groups; no `(other)` roll-up row
is synthesised, because a partial roll-up is more misleading than a plain
cut. A bare cap is ambiguous, though, since 200-of-200 and 200-of-more look
identical, so the tail also selects `COUNT(*) OVER () AS total_groups`,
which is evaluated after `GROUP BY` and before `ORDER BY`/`LIMIT` and thus
reports the pre-cap group count on every row; the handler copies it into
`total_groups` in the response, and a client treats the result as
truncated when it exceeds `len(groups)`. Quote the constant in the OpenAPI
description so clients know the response is capped.

When testing the row loop's error branches, note that pgx v5 renders any
scalar column into a `*string` through its text fallback, so a mistyped
scalar (`integer`, `timestamptz`) scans cleanly and a `bytea` fails at
prepare time (`MIN(bytea)` does not exist), never reaching `rows.Scan`. A
`text[]` column is the fixture that works: `MIN(anyarray)` exists, and pgx
refuses to decode a binary array into `*string`. For the `rows.Err()`
branch, replace the table with a view whose column divides by
`(pid - pid)`; the planner cannot fold that, so the error is raised at
execution time rather than at prepare time. `TestConnectionGroups_ScanErrorSkipsRow`
and `TestConnectionGroups_RowsErrorReturnsPartialResult` are the worked
examples, and each first proves its fixture takes the intended path.

Two further conventions matter when reading `metrics.pg_stat_activity`:

- Always restrict to `backend_type = 'client backend'`. The probe stores
  every backend, including background workers such as the walwriter and
  autovacuum workers, and counting those as "connections" inflates
  every total.

- Never render `client_addr` with a `::text` cast. `client_addr` is
  `inet`, and the standard `inet` text output carries the netmask on
  every supported PostgreSQL version (it reproduces identically on 16
  and 18), so a cast yields `192.0.2.10/32` rather than `192.0.2.10`.
  Use `host(client_addr)` instead. A NULL `client_addr` means the backend
  arrived over a Unix-domain socket, which is worth labelling as local
  rather than unknown.

### Compose query variants as compile-time constants, not with Sprintf

Where an endpoint lets the caller choose a grouping or an ordering, do not
build the SQL with a runtime `fmt.Sprintf` over whitelisted fragments. Go
permits constant concatenation, so assemble one complete `const` per
variant instead: a shared head, a shared tail, and the per-variant
expressions spliced between them with `+`. `connection-groups` is the
worked example, with `connectionGroupsQueryHead`,
`connectionGroupsQueryLabelSuffix` and `connectionGroupsQueryTail` shared
across the three finished queries (`connectionGroupsQueryByUser`,
`connectionGroupsQueryByClient` and `connectionGroupsQueryByDatabase`), and
a `map[string]string` (`connectionGroupQueries`) mapping the accepted
parameter values to those queries. `buildConnectionGroupsSQL` then
degenerates to a map lookup returning `(query string, args []any)`, with
the default variant as the fallback for an unrecognised key; the handler
still rejects unknown values with a 400 first, listing the accepted values
from a sorted key helper so the message is stable despite Go's randomised
map iteration.

This is worth doing for three reasons. The whitelist property becomes a
compiler guarantee rather than a convention, since there is no expression
anywhere that could splice a caller-supplied value into SQL. The `%%`
escaping wrinkle in `LIKE 'idle in transaction%'` disappears with the
format string. And Codacy's Opengrep `go_sql_rule-concat-sqli` rule fires
Error-level on `tx.Query(ctx, query, args...)` whenever `query` came from a
`Sprintf`, even with every value bound; removing the pattern clears the
finding on its merits instead of needing a `nosemgrep` suppression. Prefer
this shape over `nosemgrep` for new code where the variants are a small
fixed set; the existing `nosemgrep` in `internal/metrics/query.go` remains
appropriate there, because its identifiers are discovered from
`information_schema` at run time and genuinely cannot be constants.

One caveat: the `LIMIT` cannot be formatted from a Go integer constant
inside a constant expression, so the literal is written into the tail and a
test (`TestBuildConnectionGroupsSQL_GroupLimit`) asserts it stays in step
with `maxConnectionGroups`. `TestConnectionGroupQueriesShareOneBody`
likewise asserts the variants still share one head and tail, so nobody
quietly turns three composed constants into three divergent copies of a
30-line query.

### Sanitise every logged error, consistently

Errors that may carry values echoed from the database go through
`logging.SanitizeForLog(err.Error())` with a `%s` verb, never a bare `%v`.
Apply it to all three error sites in a scan loop (the query error, the
per-row scan error, and the `rows.Err()` check), not just the first; a file
that sanitises one and not the others is worse than one that is uniformly
careful, because the inconsistency reads as intentional. Much of
`perf_summary_handlers.go` still logs raw errors with `%v` and is due a
separate sweep.

### Query errors are "no data", not 500s

The metrics endpoints deliberately answer 200 with an empty payload when
their query fails, because the common cause is a probe that has never
run against the connection, and a dashboard panel showing "no data"
beats one showing an error. Follow `handleTopQueries` and
`handleConnectionGroups`: log the error at DEBUG through
`logging.SanitizeForLog` and return the empty shape.

### Package-level test-database interference

The `internal/api` package's database-backed tests share one Postgres
database and each install and drop their own trimmed `metrics.*`
fixture. Running the whole package under `go test -race -p=1` fails
intermittently, with a varying set of unrelated tests reporting
`relation "connections" does not exist` or `commit failed: connection
reset`. This predates the connection-groups work and reproduces with
those tests skipped entirely; do not treat it as a regression in
whichever change happens to be in flight, and prefer verifying a
specific change with a targeted `-run` before interpreting a full-package
run.

## Related Issues

- #56: Alerter FK violations when calculating baselines for deleted
  connections.
- #405: `metric_staleness` alert fire/clear loop; introduced the
  `ErrMetricNotSupported`/`ErrNoMetricData` sentinels and the
  probe-scoped alert lookups.
- #406: Five built-in alert rules that could never fire; fixed in the
  alerter metric registry plus collector migration 8.
- #402: Column kind registry (`column_kinds.go`) gating `_per_sec` and
  `_delta` by kind; `_pct` (`DerivedTimeShare`) and `_sessions`
  (`DerivedSessionAverage`) added; `DerivedMetric.Unit` reported on each
  series. Also the `stats_reset` guard, the three-interval gap bound
  (`MaxElapsedIntervals`), `ResolveProbeInterval` and the bucket clamp,
  the loopback exclusion in `metricQueryClauses`, and the per-series fill
  policy with nullable points (`MetricDataPoint.Value *float64`,
  `MaxCarryIntervals`) replacing uniform LOCF.
- #430: The `/api/v1/metrics/query` response envelope
  (`MetricsQueryResult`) carrying the resolved window and
  `bucket_seconds`, the exported `BucketWidth` helper, and the `filled`
  flag on carried-forward points.
- #428: Pseudo filesystems excluded from
  `pg_sys_disk_info.used_percent`, so a squashfs mount no longer pins the
  disk metric at 100%.
- #409: `deadlocks_delta` and `temp_files_delta` moved to hourly sums,
  `required_extension` enforced in evaluation and resolution,
  `table_bloat_ratio` retired from the registry; collector migration 11.
- #384: Last observed client attributed to a query on the top-queries
  endpoint; the `last_client` `DISTINCT ON` CTE keyed on `(query_id,
  datid, usesysid)`, fed by `metrics.pg_stat_activity.query_id` which
  collector migration 12 adds.
- #400: `_delta` (`DerivedDelta`) added alongside `_per_sec` so dashboard
  charts plot per-bucket counter increases instead of cumulative totals.
- #342: Derived metrics (`_per_sec` rates and `dead_tuple_ratio`) added to
  `QueryTimeSeries` to fix blank Activity Charts; retired #339's
  `resolveMetricValue` in favour of the `finiteFloat` guard in `toFloat64`.
- #345: Custom time ranges; `ResolveTimeWindow`/`TimeWindow` added and
  `QueryTimeSeries` switched from a time-range string to a resolved
  window.

- #346: `GET /api/v1/metrics/connection-groups`, the reference
  latest-snapshot aggregation over `metrics.pg_stat_activity`.
- #401: Cache hit ratios computed from per-database counter deltas in
  `queryCacheHit` and `queryDatabaseCacheHitTimeSeries`; nullable
  ratio fields.
