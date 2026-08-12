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
`alerter/src/internal/database/queries.go` -
`GetHistoricalMetricValues`. Every one of its metric branches
performs the JOIN. The regression test at
`alerter/src/internal/database/queries_integration_test.go` -
`TestGetHistoricalMetricValues_FiltersOrphanedConnections` - asserts
that every branch filters orphans. When adding a new metric branch,
extend both.

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
  column wins even when it ends in `_per_sec`.
- A name ending in `_per_sec` whose prefix is a real numeric column becomes
  a `DerivedPerSec` rate (delta of the counter over elapsed seconds).
- The literal `dead_tuple_ratio` is accepted only when the probe exposes
  both `n_live_tup` and `n_dead_tup`; it is a 0-100 percentage.
- A repeated name is silently de-duplicated; anything else is a client
  error.

`BuildDerivedMetricsQuery` builds the derived SQL. It shares the bucketing,
gap-filling (`generate_series`), filtering, and `$N` argument layout of
`BuildMetricsQuery`, so the caller scans and applies LOCF identically for
raw and derived series. Per-second base columns must stay validated against
the discovered column set and `QuoteIdentifier`-wrapped; never interpolate a
caller-supplied metric name that has not passed `classifyMetrics`. Negative
counter deltas (resets/restarts) and non-positive elapsed times are dropped
to NULL so they never yield a bogus rate.

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
convention as `query_db_test.go`). Its fixture inserts minute-spaced samples
with counters rising 60 per minute (a clean 1.0/sec rate) and constant
live/dead tuple counts (a steady 10% ratio), then exercises raw, `_per_sec`,
`dead_tuple_ratio`, and mixed requests end-to-end. The two `scanSeriesRows`
error returns in `QueryTimeSeries` are driven deterministically by passing
an aggregation that names no SQL function, which makes the built query fail
at execution; `scanSeriesRows`'s own `pool.Query` and `rows.Scan` error
branches are driven by a cancelled context and a destination-count mismatch
respectively.

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
  FROM clause; there are 36 `metrics.*` tables and no views.

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

## Related Issues

- #56: Alerter FK violations when calculating baselines for deleted
  connections.
- #406: Five built-in alert rules that could never fire; fixed in the
  alerter metric registry plus collector migration 8.
- #342: Derived metrics (`_per_sec` rates and `dead_tuple_ratio`) added to
  `QueryTimeSeries` to fix blank Activity Charts; retired #339's
  `resolveMetricValue` in favour of the `finiteFloat` guard in `toFloat64`.
