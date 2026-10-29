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
  `$2::timestamptz` (inclusive upper bound on `collected_at`).
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
`comparisonSeriesQuery` adds (`$3`, `$5` and `$6` are uncast), are not
yet held to these rules.

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
4. **No PostgreSQL-only types or functions.** DuckDB has no `pg_lsn`,
   `inet`, `host()` or `make_interval()`. LSNs and inet columns are
   stored as text. `LSNDiffBytesSQL` in `pkg/anomaly/metrics.go` computes
   `from - to` in bytes from two text LSNs with `split_part`, `lpad`,
   `upper`, `substr` and `strpos`, all present in both engines; it is
   exactly equal to `(from::pg_lsn - to::pg_lsn)::float`. Do not use
   `('0x' || h)::bigint`: DuckDB accepts it but PostgreSQL 14 and 15,
   which the datastore still supports, do not.
5. **No functions over the views.** A PL/pgSQL or SQL function that
   reads a ColdFront view fails ("DuckDB execution is not supported
   inside functions").
6. **Column types differ.** On the views text columns report as
   `character varying` and oid columns as `bigint`. Scanning
   `database_name` into a Go `string` works either way.

## Tests That Enforce Them

In `alerter/src/internal/database/coldfront_compat_test.go`, unless
noted:

- `TestMetricsSQLColdFrontShapes` (no database) runs
  `coldFrontShapeViolations` over `metricsSQLStatements()`, which lists
  every registry entry's latest and historical query (the latter from
  `pkg/anomaly`) and the readers in `queries.go`: an uncast `$n`,
  `ANY(`, `unnest(`, an array cast, `pg_lsn`, `make_interval(`,
  `host(`, `::inet`, and an `avg(...)` not cast to float in place.
  `TestColdFrontShapeViolations` pins the rules against the shapes that
  failed live and the replacements that worked.
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
  cast `$1::int` lookback and `$2::timestamptz` bound on every
  `HistoricalSQL`.

## Live Verification

`GetLatestMetricValues` and `GetHistoricalMetricValues` (lookback 7) were
run for every registry entry against a ColdFront datastore, plus
`GetConnectionsWithExtension` and `GetClusterPeers`; all passed after
the fix, against 20 historical failures and the `lag_bytes` latest
failure before it. Prepared on ColdFront, every statement's result
columns were int4, int8, float8, text, varchar or timestamptz. Many wide
probes had no rows at the time, so their scans were checked by result
type rather than by data. To repeat, a throwaway test file in the
package (behind a build tag, never committed) that builds a
`Datastore{pool: ...}` over the ColdFront DSN and loops over
`metricRegistry` is the quickest harness. That run predates the move
of the historical queries to `pkg/anomaly` and their `$2` bound.
