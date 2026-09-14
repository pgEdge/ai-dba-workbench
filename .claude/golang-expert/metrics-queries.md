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
  column wins even when it ends in `_per_sec` or `_delta`.
- A name ending in `_per_sec` whose prefix is a real numeric column becomes
  a `DerivedPerSec` rate (delta of the counter over elapsed seconds).
- A name ending in `_delta` whose prefix is a real numeric column becomes a
  `DerivedDelta`: the per-bucket increase of a cumulative counter, which is
  what a dashboard bar chart wants in place of the ever-growing raw total.
- The literal `dead_tuple_ratio` is accepted only when the probe exposes
  both `n_live_tup` and `n_dead_tup`; it is a 0-100 percentage.
- A repeated name is silently de-duplicated; anything else is a client
  error.

`BuildDerivedMetricsQuery` builds the derived SQL. It shares the bucketing,
gap-filling (`generate_series`), filtering, and `$N` argument layout of
`BuildMetricsQuery`, so the caller scans and applies LOCF identically for
raw and derived series; it additionally takes the probe's entity-key
columns (see below). Per-second base columns must stay validated against
the discovered column set and `QuoteIdentifier`-wrapped; never interpolate a
caller-supplied metric name that has not passed `classifyMetrics`. Negative
counter deltas (resets/restarts) and non-positive elapsed times are dropped
to NULL so they never yield a bogus rate.

`DerivedPerSec` and `DerivedDelta` share one `rate_samples`/`rate_buckets`
CTE pair, since both derive from the same `LAG` over consecutive samples, so
a request may mix them freely for the same or different base columns: each
counter gets a `total_i`/`prev_i` pair, then either a `rate_i` or a
`delta_i` sample column. Two rules are specific to deltas and must not be
"tidied away":

- The bucket aggregate is always `SUM(delta_i)`; the `aggregation` request
  parameter is deliberately ignored, because averaging or taking the last of
  a set of increments under-reports the events in the bucket. A reset or the
  first sample of the window yields 0 rather than NULL, so the bucket SUM
  stays defined whenever the bucket holds any sample.
- The final SELECT wraps each delta in `COALESCE(..., 0)` after the LEFT
  JOIN, so a bucket with no sample reads 0. Leaving it NULL would hand it to
  `scanSeriesRows`'s LOCF fill, which would repeat the previous bucket's
  increase even though the events of the sample-less bucket are already
  counted by the next sample's delta: a double count. The COALESCE is
  gated on `EXISTS (SELECT 1 FROM rate_samples)`, i.e. on the window
  holding at least one in-window sample (the borrowed pre-window sample is
  already excluded from `rate_samples`). Without the gate a connection
  whose probe never ran, is disabled or is failing produced 151 zero
  points and a confident flat-zero chart or a "0 B" KPI where the raw
  column and the `_per_sec` form produced none; with it every bucket is
  NULL, `scanSeriesRows` has no last value to carry, and the series is
  empty so the dashboard shows its no-data state. A single in-window
  sample is enough to re-enable the zero fill.

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
reset guard nulled the sample and LOCF republished the previous
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
`metricQueryClauses` only, and both clauses pick it up.

Both the raw and derived branches feed the shared `scanSeriesRows` helper,
which scans a bucket-time-plus-N-values result set, applies LOCF per
connection and metric name via `lastKnown`, and treats a NULL bucket or a
non-finite sample as a gap.

### NaN/Inf handling: finiteFloat, not resolveMetricValue

Non-finite samples (NaN, +/-Inf) must be treated as gaps, never plotted,
because `encoding/json` cannot marshal them and one bad value would blank
the whole response. This guard lives in `toFloat64` via the `finiteFloat`
helper: `toFloat64` returns `(0, false)` for a non-finite float64/float32/
`pgtype.Numeric`, and `scanSeriesRows` then treats `!ok` as a gap (LOCF
carry-forward, or skip when there is no prior value). An earlier design
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
convention as `query_db_test.go`). Its fixture inserts minute-spaced
samples with counters rising 60 per minute (a clean 1.0/sec rate) and
constant live/dead tuple counts (a steady 10% ratio), then exercises raw,
`_per_sec`, `dead_tuple_ratio`, and mixed requests end-to-end. Both
fixtures return the minute-truncated base time their offsets hang off, so a
test can build a window with `windowSince(base, minutes)` that lines up
exactly with the samples instead of re-reading the clock; the lookback
tests use a window that opens on the second sample, leaving the first just
outside it, and assert the first bucket carries the increase since that
outside sample, plus a companion case whose window opens on the earliest
sample of all and so must behave exactly as it did before the lookback
existed. A second fixture in the same file covers `_delta` with a
deliberately awkward progression: a first sample with no `LAG`, a minute
carrying no sample at all, and a counter reset, asserting the exact
non-zero per-bucket deltas, the 0 fill, and that the reset contributes
nothing. A third, `setupNetworkFixture`, is shaped like
`pg_sys_network_info` with its real primary key and an interface set that
changes under the window (one interface resets and is then torn down,
another appears carrying a large lifetime counter); it asserts the exact
per-minute totals for both `_delta` and `_per_sec`, that a connection
with no rows and a window with no samples each yield zero points, and
that a single in-window sample still zero-fills. A fourth,
`setupLookbackFixture`, holds one scenario per connection for the
lookback bound: a reset straddling the window boundary, a predecessor
beyond and one inside the 30-minute floor, and a predecessor beyond and
one inside three 1-hour buckets. Asserting exact values at exact bucket
times needs the window anchored on the minute (`windowSince`), not on
`time.Now()`, or the bucket boundaries drift off the samples.
Minute-spaced samples always land in distinct 60-second buckets whatever
the window origin is, which is what makes those exact assertions safe. The
two `scanSeriesRows` error returns in `QueryTimeSeries` are driven
deterministically by passing an aggregation that names no SQL function,
which makes the built query fail at execution; `scanSeriesRows`'s own
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
  pattern to copy (see the archiver and checkpointer entries): it survives
  a stats reset, it reports 0 rather than dropping the connection when
  only one sample lands in the window, and it gives the rule an absolute
  per-hour count rather than a per-probe-interval figure. Rules whose
  value is a rate should say so in `metric_unit`, for example
  `checkpoints/hour`.

- `system_stats` columns are platform-specific. `processor_time_percent`,
  `user_time_percent`, `privileged_time_percent` and
  `interrupt_time_percent` are Windows-only and NULL on Linux; the Linux
  build populates the per-mode buckets and `idle_mode_percent`. Use the
  shared `cpuBusyPercentExpr` rather than coalescing a platform-specific
  column to zero, which reads as an idle host.

Changing a seeded rule's threshold or unit needs a collector migration as
well as the registry edit, because migration 1 only seeds a fresh install.
Migration 8 is the worked example: it rewrites descriptions and units
unconditionally, but only rewrites a threshold that still carries the old
shipped default, so operator tuning survives the upgrade.

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
  so test fakes must accept a `metrics.TimeWindow`.

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

## Bounded Name Lookups (server)

`buildTopQueriesSQL` resolves `dbid` and `userid` OIDs to names through
`DISTINCT ON (...) ... ORDER BY oid, collected_at DESC` over
`metrics.pg_stat_activity`, which has no index on `datid` or
`usesysid`. Without a `collected_at` bound that sort covers every
activity row in retention for the connection, and it runs twice per
page (count and page statements). Both CTEs are therefore anchored to
the latest `pg_stat_statements` snapshot and read only the preceding
`nameLookupWindowSQL` (one hour) of activity samples; on a fixture of
500,000 activity rows that took the page from 1.6 s and a 29 MB
external sort to 8 ms in memory. Any new OID-to-name lookup over
`pg_stat_activity` needs the same bound.

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
