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
Those tables intentionally carry no foreign key to `connections` so that
probes never fail because a connection was deleted mid-write. The cost
of that design is that orphaned metric rows can briefly outlive their
owning connection.

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
cut. Quote the constant in the OpenAPI description so clients know the
response is capped.

Two further conventions matter when reading `metrics.pg_stat_activity`:

- Always restrict to `backend_type = 'client backend'`. The probe stores
  every backend, including background workers such as the walwriter and
  autovacuum workers, and counting those as "connections" inflates
  every total.

- Never render `client_addr` with a `::text` cast. `client_addr` is
  `inet`, and on PostgreSQL 18 the text output carries the netmask, so a
  cast yields `192.0.2.10/32` rather than `192.0.2.10`. Use
  `host(client_addr)` instead. A NULL `client_addr` means the backend
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
- #342: Derived metrics (`_per_sec` rates and `dead_tuple_ratio`) added to
  `QueryTimeSeries` to fix blank Activity Charts; retired #339's
  `resolveMetricValue` in favour of the `finiteFloat` guard in `toFloat64`.
- #346: `GET /api/v1/metrics/connection-groups`, the reference
  latest-snapshot aggregation over `metrics.pg_stat_activity`.
