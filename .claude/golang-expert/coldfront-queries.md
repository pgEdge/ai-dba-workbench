/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ColdFront-Compatible Metrics Queries
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# ColdFront-Compatible Metrics Queries

GitHub issue #755 lets the collector store metrics on a pgEdge ColdFront
datastore (`use_coldfront_for_metrics`). There each `metrics.<probe>`
relation is a view that `coldfront.create_iceberg_table` creates over an
Apache Iceberg table, and pg_duckdb translates any query that reads one
and runs the whole query in DuckDB, including its joins against ordinary
tables such as `connections` (DuckDB reads those through its postgres
scanner). The views have no indexes, primary keys or foreign keys.

Neither the alerter nor the server has a ColdFront setting. Every query
they run against `metrics.*` is one SQL text that must work on plain
PostgreSQL and on ColdFront, with identical plain-PostgreSQL results.

## Where the Queries Live

- `pkg/anomaly/metrics.go`: `HistoricalSQL` for every baselined metric
  (`metricDefs`), plus `LSNDiffBytesSQL` and `ReplicationLagBytesExpr`.
  The alerter's baselines (`GetHistoricalMetricValues`) run these, and
  the server's anomaly comparison
  (`server/src/internal/metrics/anomaly_comparison.go`) wraps them in
  `comparisonSeriesQuery`. Each takes `$1::int` (lookback days) and
  `$2::timestamptz`, an inclusive upper bound on `collected_at` written
  `<col> <= COALESCE($2::timestamptz, <col>)` beside every lookback. A
  NULL `$2` means no upper bound: the alerter binds a NULL
  `pgtype.Timestamptz{}` (`noHistoricalUpperBound` in
  `alerter/src/internal/database/metric_queries.go`); the server binds
  a real timestamp, which PostgreSQL folds into a plain
  `<col> <= const` index condition.
- `alerter/src/internal/database/metric_registry.go`: `latestSQL` for
  every registry entry, which feeds threshold evaluation, anomaly
  scoring and recovery (`GetLatestMetricValues`).
  `withBaselineQueries` copies each entry's `historicalSQL` in from
  `pkg/anomaly`, so the alerter's tests below cover those too.
- `alerter/src/internal/database/queries.go`:
  `connectionsWithExtensionSQL` (rule extension gates) and
  `clusterPeersSQL` (cluster context for alerts).

Probe staleness, connection errors and everything else read ordinary
tables (`probe_availability`, `connections`, ...) and run in PostgreSQL.

The server's own `metrics.*` readers, including the outer predicates
`comparisonSeriesQuery` adds, are not held to these rules by a test in
this module. Rule 1 applies to them all the same: the outer `$3`, `$5`
and `$6` must read `$3::int`, `$5::timestamptz` and `$6::timestamptz`.

## Rules

Each was reproduced live (ColdFront main, pg_duckdb 1.1.0, PostgreSQL
18.6) with pgx bound parameters. psql's text mode hides the scan
failures in rule 3, so reproduce with pgx, never psql.

1. **Cast every bound parameter.** DuckDB types a bare `$1` as UNKNOWN
   and fails at plan time with "Could not convert DuckDB type: UNKNOWN
   to Postgres type". Every `historicalSQL` failed this way on
   `NOW() - INTERVAL '1 day' * $1`. Write `INTERVAL '1 day' * $1::int`
   and `collected_at <= $2::timestamptz`.
   `make_interval(days => $1::int)` does not work: DuckDB has no
   `make_interval`. Cast comparison parameters too (`$1::text`,
   `$1::int`); some happen to plan uncast, but the guard requires a
   cast on every one.
2. **Bind no arrays.** `col = ANY($1)`, `= ANY($1::int[])`,
   `IN (SELECT unnest($1::int[]))`, `= ANY($1::text::int[])`, a literal
   `= ANY('{2,3}'::int[])` and even `IN ($1::int, $2::int)` all fail
   with the UNKNOWN error. What works: `col = $1::int`, `col = $1 OR
   col = $2`, `col IN ($1::text, $2::text)`, integer literal lists
   `IN (2, 3)`, and `= ANY(string_to_array($1, ',')::int[])` with one
   text parameter (the guard rejects `ANY(` wholesale, so prefer the
   others).
3. **No numeric result columns.** PostgreSQL labels `avg()` over an
   integer and `EXTRACT(EPOCH ...)` as numeric (OID 1700), but DuckDB
   sends DOUBLE bytes under that label and pgx fails with "numeric
   incomplete". Cast to `::float` before the value leaves the query
   (`MAX(EXTRACT(...))::float` is fine; so is an uncast EXTRACT inside
   a predicate). sum/min/max of bigint are fine.
4. **No PostgreSQL-only types or functions.** DuckDB has no `pg_lsn`
   or `make_interval()`. LSNs and inet columns are stored as text.
   `host()` runs only over an inet cast: `host(client_addr::inet)`
   works on ColdFront, but `host()` over the text (varchar) column
   fails. Cast to `inet` nowhere else; it is unverified there and an
   inet result column is not one the Workbench scans.
   `LSNDiffBytesSQL` in `pkg/anomaly/metrics.go` computes `from - to`
   in bytes from two text LSNs with `split_part`, `lpad`, `upper`,
   `substr` and `strpos`, all present in both engines; it is exactly
   equal to `(from::pg_lsn - to::pg_lsn)::float`. Do not use
   `('0x' || h)::bigint`: DuckDB accepts it but PostgreSQL 14 and 15,
   which the datastore still supports, do not.
5. **No functions over the views.** A PL/pgSQL or SQL function that
   reads a ColdFront view fails ("DuckDB execution is not supported
   inside functions").
6. **No infinite timestamps.** Write no `'infinity'` or `'-infinity'`
   literal, and no other infinite timestamp, as a range bound. DuckDB
   may plan a range join that subtracts the bounds and then fails with
   "Cannot subtract infinite timestamps"; whether it does depends on
   the plan, so a query can pass many times and fail once (seen once
   in about 45 hourly baseline runs on PostgreSQL 17). Leave an
   open end NULL instead and test it in the join:
   `(m.valid_from IS NULL OR a.collected_at >= m.valid_from) AND
   (m.valid_to IS NULL OR a.collected_at < m.valid_to)`. The
   `connection_utilization_percent` historical query bounds its
   `max_connections` ranges this way, with `valid_from` NULL on a
   connection's first `pg_settings` row (`LAG` is NULL) and `valid_to`
   the bare `LEAD`. The same goes for bound parameters: never bind
   `pgtype.Infinity` or `-infinity` as an open end. Bind NULL and open
   the bound with `COALESCE`, as in rule 7.
7. **No NULL test on a parameter.** `$2::timestamptz IS NULL` (cast or
   not, `IS NOT NULL` too, on either side of an `OR`) fails at DuckDB
   plan time with the UNKNOWN error of rule 1, whatever `$2` is bound
   to. A NULL test on a column (`m.valid_from IS NULL`) is fine. To let
   NULL mean "no bound", write `col <= COALESCE($2::timestamptz, col)`;
   `COALESCE(col <= $2::timestamptz, true)`,
   `(col > $2::timestamptz) IS NOT TRUE` and
   `col <= COALESCE($2::timestamptz, NOW())` also ran, but only the
   first folds to an indexable `col <= const` when `$2` is a constant,
   and the `NOW()` form drops rows stamped after the datastore's clock,
   which an unbounded scan keeps.
8. **Column types differ.** On the views text columns report as
   `character varying` and oid columns as `bigint`. Scanning
   `database_name` into a Go `string` works either way.

## Tests That Enforce Them

The textual rules live in one place, `coldfrontsql.Violations` in
`pkg/coldfrontsql/coldfrontsql.go`: an uncast `$n`, `ANY(`, `unnest(`,
an array cast, `pg_lsn`, `make_interval(`, a `host(` whose argument
does not end in `::inet`, an `::inet` that is not such an argument, a
`$n [::type] IS [NOT] NULL` test, `infinity` (any case), and an
`avg(...)` not cast to float in place.
`TestViolations` in `pkg/coldfrontsql/coldfrontsql_test.go` pins them
against the shapes that failed live and the replacements that worked.
A module that adds a `metrics.*` reader runs its SQL through
`Violations` in a test. In `alerter/src/internal/database/coldfront_compat_test.go`
unless noted:

- `TestMetricsSQLColdFrontShapes` (no database) runs
  `coldfrontsql.Violations` over `metricsSQLStatements()`, which lists
  every registry entry's latest and historical query (the latter from
  `pkg/anomaly`) and the readers in `queries.go`.
- `TestHistoricalSQLColdFrontShapes` in `pkg/anomaly/coldfront_test.go`
  (no database) runs `coldfrontsql.Violations` over every
  `BaselineMetrics()` `HistoricalSQL` and the shared fragments
  (`CPUBusyPercentExpr`, `PseudoFilesystemExclusion`,
  `ReplicationLagBytesExpr`), so `pkg/anomaly` is guarded on its own,
  whichever consumer runs the queries.
- `TestMetricsSQLArgsFinite` (no database) fails when any statement in
  `metricsSQLStatements()` is bound with an infinite
  `pgtype.Timestamptz`, `pgtype.Timestamp` or `pgtype.Date`, and
  `TestNoHistoricalUpperBoundIsNull` pins `noHistoricalUpperBound` to
  NULL. Plain PostgreSQL accepts an infinite bind and `Violations` sees
  only SQL text, so these are the only guards on rule 6's bound
  parameters.
- `TestConnectionUtilizationHistoricalOpenBounds` in
  `pkg/anomaly/metrics_test.go` (no database) pins the NULL-open
  `valid_from`/`valid_to` ranges of the `connection_utilization_percent`
  historical query; the alerter's `TestConnectionUtilizationPercent_*`
  integration tests check that the first setting covers earlier samples
  and the last covers later ones.
- `TestMetricsSQLResultTypes` (database) builds every `metrics.*`
  table from the collector's DDL in `collector/src/database/schema.go`,
  prepares and runs each statement, and fails on a result column
  outside int4, int8, float8, text, varchar and timestamptz. This is
  the guard for rule 3, EXTRACT included.
- `TestMetricsSQLStatementsListEveryReader` fails when a non-test file
  in the package gains a `FROM`/`JOIN metrics.` that
  `metricsSQLStatements` does not list. A new reader outside the
  registry goes in a named constant and in that list.
- `TestLSNDiffBytesSQLMatchesPgLSN` checks `anomaly.LSNDiffBytesSQL`
  against `pg_lsn` subtraction on plain PostgreSQL, including halves
  above 2^31. `TestLSNHalfSQLWeights` and `TestLSNDiffBytesSQL` in
  `pkg/anomaly/lsn_test.go` check its shape without a database.
- `TestHistoricalUpperBound` in `pkg/anomaly/metrics_test.go` pins the
  cast `$1::int` lookback and, directly after it, the
  `AND <col> <= COALESCE($2::timestamptz, <col>)` bound on every
  `HistoricalSQL`. The server's `TestComparisonSeriesQuery_Plan` checks
  that a real `$2` still reaches the inner scan's condition.

## Known Trade-offs

Both were measured on plain PostgreSQL 17 and accepted for one SQL text
that runs on both engines; neither changes results.

- `LSNDiffBytesSQL` decodes each LSN half with one `strpos` per hex
  digit, so the `lag_bytes` historical query ran about 6.3 times slower
  than with `pg_lsn` subtraction (4.4 ms to 27.6 ms over 10,200 rows).
- With `$2` NULL, as the alerter binds it, the planner cannot fold
  `collected_at <= COALESCE($2::timestamptz, collected_at)` and keeps it
  as a filter at the default 1/3 selectivity, cutting row estimates by
  two thirds on 16 of the 20 historical queries. A real `$2`, as the
  server binds, folds to a plain index condition.
