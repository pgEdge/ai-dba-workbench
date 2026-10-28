<!--
/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
-->

# Metrics Stored in pgEdge ColdFront

The collector option `use_coldfront_for_metrics` (YAML, default `false`)
and its flag `-use-coldfront-for-metrics` store every `metrics.*` table
in pgEdge ColdFront (Iceberg on object storage, exposed as a PostgreSQL
view) instead of as a weekly-partitioned table (issue #755). Only the
collector has the option. With it off, the schema and behaviour are
exactly the default ones; keep it that way.

## Single Source of Truth

`collector/src/coldfront/` holds:

- `spec.go`: the column specification of all 36 metrics tables, in
  final attribute order, with each column's plain-PostgreSQL type as
  `format_type()` reports it and `AddedIn` for a column a later
  migration adds by `ALTER TABLE` (v7, v10, v12, v13).
- `types.go`: `StorageType`, the mapping to a type ColdFront accepts.
  `oid` and `xid` become `bigint`; `inet`, `"char"` and `name` become
  `text`; unbounded `numeric` becomes `numeric(38,0)`. Anything
  unknown is an error, never a silent fallback.
- `convert.go`: per-type value conversions for `StoreMetrics`. Only
  `inet` (pgx gives `netip.Prefix`; rendered as PostgreSQL's inet
  output, so a host has no `/32`) and `"char"` (pgx gives a `rune`)
  need one. pgx encodes `uint32` (oid, xid) into `bigint`, `string`
  (name) into `text` and `pgtype.Numeric` into `numeric(38,0)`
  directly; `TestColdFrontMigrationsAgainstStub` proves this against
  a real server.

Two guards keep the specification and `schema.go` from drifting:
`checkCreateTableMatchesSpec` parses each metrics `CREATE TABLE` under
ColdFront and refuses a mismatch (exercised for every table by
`TestColdFrontMigrationsIssueNoMetricsIndexOrPartitionDDL`), and
`TestColdFrontSpecMatchesMigratedSchema` compares the specification
with a fully migrated default-mode datastore.

## Migrations

`NewSchemaManager(opts ...SchemaManagerOption)` takes
`WithColdFrontMetrics(bool)`. It is variadic because the server's
integration tests call `NewSchemaManager()` with no arguments.

Each metrics `CREATE TABLE` block in migrations 1 and 3 runs through
`sm.execMetricsDDL(ctx, tx, "<table>", ddl)`, and each metrics FK
through `sm.addMetricsConstraint`. In the default mode they execute
the block verbatim. Under ColdFront `execMetricsDDL` splits the block
(`splitSQLStatements` handles quotes and `--` comments only, no
dollar quoting) and: replaces `CREATE TABLE` with
`coldfront.create_iceberg_table(p_schema => 'metrics', ...)` using
`ColumnsJSON` and `PartitionColumns` (`{day(collected_at)}`); rewrites
`COMMENT ON TABLE` as `COMMENT ON VIEW` (`commentOnTableAsView`),
because the relation is a view and PostgreSQL refuses `COMMENT ON
TABLE` on one (SQLSTATE 42809, which failed migration 1 live); keeps
`COMMENT ON COLUMN`, which works on views; drops `CREATE INDEX` and `COMMENT ON INDEX`;
and errors on any other statement. Keep the DDL text in `schema.go`
as `CREATE TABLE IF NOT EXISTS metrics.<name> (... ) PARTITION BY
RANGE`: the server's `column_kinds_test.go` parses it.

Other migrations that skip work under ColdFront: v1 pgvector and
chat-memory embedding setup (SAVEPOINTs are not allowed after an
Iceberg write in the same transaction), v4's metrics `DROP INDEX`, v6
(embedding widening, SAVEPOINT), v15 (index), v24 entirely, and the
row rewrites of v25 and v26 (their comments still run). The rewrites
are safe to skip because a datastore cannot be switched to ColdFront,
so its metrics tables were created empty in the same startup. v7, v10,
v12 and v13 run unchanged: their `ALTER TABLE ... ADD COLUMN` relies on
ColdFront intercepting that form on its views (a plain view rejects it
with 42809), and their `COMMENT ON COLUMN` works on views. Later
migrations only `COMMENT ON COLUMN` metrics relations; never add a
`COMMENT ON TABLE`, `ALTER TABLE` of another form, `GRANT`, `TRUNCATE`
or DML on a metrics relation without handling the ColdFront case.

## Startup and Runtime

- `database.PrepareMetricsStorage` runs in `initializeSchema` before
  `Migrate`. It calls `CheckMetricsStorage`, which under ColdFront
  requires `coldfront.create_iceberg_table(text,text,jsonb,text[])`
  (its presence is how "installed" is judged, rather than the
  `pg_extension` row, so the stub below passes) plus both
  `coldfront._partition_clause(jsonb,text[])` (pgEdge/coldfront#92;
  `extversion` is 1.0 in every build, so never compare versions) and
  `coldfront.ensure_attached()`. In both modes it refuses existing
  metrics relations of the other kind (tables vs views).
- Under ColdFront it then calls `EnsureColdFrontNamespace`, which runs
  `SELECT coldfront.ensure_attached()` and
  `SELECT duckdb.raw_query('CREATE SCHEMA IF NOT EXISTS ice.metrics')`
  as two separate autocommit statements on the pool connection, outside
  any transaction. This is required: `create_iceberg_table` issues the
  namespace `CREATE SCHEMA` and the `CREATE TABLE` inside one function
  call, and a Lakekeeper REST catalog does not see the namespace before
  the table request, so on an empty warehouse migration 1 failed with
  "Namespace with name 'metrics' does not exist" (404). ColdFront's own
  install steps pre-create the namespace for the same reason. Both
  statements are idempotent; a failure names the namespace and says the
  warehouse in `coldfront.warehouse` must be reachable and writable.
  Never move them into the migration transaction.
- `probes.SetColdFrontMetrics` is process-wide state set once in
  `main` by `applyMetricsStorageMode`, which also logs once that
  retention settings are ignored. `EnsurePartition` and
  `DropExpiredPartitions` return immediately under it, and
  `startGarbageCollector` returns a nil collector
  (`stopGarbageCollector` accepts nil).

## Testing Without ColdFront

No live ColdFront datastore is available to the tests. Two substitutes:
`recordingTx` in `database/coldfront_schema_test.go` runs the whole
migration chain without a database and asserts on the SQL issued, and
`TestColdFrontMigrationsAgainstStub` in
`database/coldfront_integration_test.go` installs `coldFrontStub`:
stub `coldfront` and `duckdb` schemas whose `create_iceberg_table`
refuses a table until `duckdb.raw_query` has created its namespace in
an earlier committed statement, and then creates `metrics.<table>` as a
view (over a backing table in `coldfront_stub`, so `StoreMetrics` can
insert through it). It runs `PrepareMetricsStorage` and the full chain
through `sm.migrate(conn, wrap)`, checks every metrics relation is a
view with the specification's columns and comments, and shows a second
run is a no-op. Removing either the namespace step or the `COMMENT ON
VIEW` rewrite makes it fail with the live error. `coldFrontAlterTx`
replaces only the five v7/v10/v12/v13 `ADD COLUMN` statements with the
stub's emulation, because no stub can intercept DDL on a view the way
ColdFront does; the test also asserts a plain view rejects that
statement. None of this verifies ColdFront's own behaviour (its
`ALTER TABLE` interception, insert rewriting, Iceberg partitioning).
