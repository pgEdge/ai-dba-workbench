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
  need one. `uint32` (oid, xid), `string` (name) and `pgtype.Numeric`
  marshal to JSON that the insert helper reads back exactly (see
  "Writing Rows Under ColdFront"); `exerciseInsertHelper` proves this
  against a real server.

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
Iceberg write in the same transaction, and the setup cannot move ahead
of PART 2's metrics tables because it needs `anomaly_candidates` and
`chat_memories`), v4's metrics `DROP INDEX`, v6 (embedding widening,
SAVEPOINT), v15 (index), v24 entirely, and the row rewrites of v25 and
v26 (their comments still run). The rewrites are safe to skip because
a datastore cannot be switched to ColdFront, so its metrics tables were
created empty in the same startup. The skipped pgvector objects are
created after the migrations by `EnsureColdFrontEmbeddings` (see
Startup and Runtime), so alerter Tier 2 and chat memory embeddings work
under ColdFront as in the default mode.

v7, v10, v12 and v13 run their SQL through `sm.execMetricsAlter`. In
the default mode that is the block verbatim. Under ColdFront each
statement runs on its own, and around each `ALTER TABLE metrics.<v>`
the view's comments are read and written back after the ALTER.
`metricsCommentsQuery` builds the `COMMENT ON VIEW`/`COMMENT ON COLUMN`
statements on the server with `format('%I', '%L')`, so their quoting is
PostgreSQL's own. ColdFront applies `ALTER TABLE ... ADD COLUMN` to one
of its views by dropping and recreating the view
(`coldfront._rebuild_iceberg_view`), which keeps owner and grants but
loses every comment; live, the four commented views these migrations
alter were left bare. (`pg_sys_memory_info`, `pg_sys_cpu_info` and
`pg_sys_io_analysis_info` have no view comment in either mode.) A plain
view rejects that ALTER with 42809, so the form itself relies on
ColdFront's interception. Any new `ALTER TABLE` on a metrics relation
must go through `execMetricsAlter`; never add `COMMENT ON TABLE`,
`GRANT`, `TRUNCATE` or DML on a metrics relation without handling the
ColdFront case.

## Writing Rows Under ColdFront

ColdFront rewrites a cold-tier INSERT with bound parameters into
`format(<template>, $1, ...)` passed to `duckdb.raw_query`, so one
statement can carry at most 99 parameters (never more than 1,024); a
100-row batch of a wide probe failed every cycle live. Reading rows
inside DuckDB (`jsonb_populate_recordset`, multi-array `unnest`) does
not work either. So `StoreMetrics` under ColdFront
(`probes.storeColdFrontMetrics`) encodes each row as a JSON object
keyed by column (`encodeColdFrontRows`), joins a batch into an array,
and calls `SELECT workbench_coldfront.insert_rows($1, $2::jsonb)`
(`coldfront.InsertRowsFunction`), each statement on its own rather
than in one transaction.

### The helper functions

- They live in the schema `workbench_coldfront`
  (`coldfront.HelperSchema`), created by
  `database.EnsureColdFrontInsertHelper` at every ColdFront startup,
  after the namespace step, in one transaction, with
  `CREATE OR REPLACE`; not in a migration, so they always match the
  collector calling them and the default-mode schema stays untouched.
- Not in `public`: a role with CREATE there could create the functions
  first, keep ownership through `CREATE OR REPLACE`, and later swap in
  a body the collector runs on its next write (security review
  VULN-001). Not in `metrics` (views only) nor `coldfront` (the
  extension's). The collector refuses to start if the schema, or any
  function in it, is owned by another role (`coldFrontHelperOwnersQuery`),
  and revokes PUBLIC from the schema and from every function.
- `literal(text)`, `insert_rows_sql(p_table, p_rows)`, which renders the
  INSERT (separate so the tests can assert on its SQL), and
  `insert_rows`, which EXECUTEs it. The renderer accepts only a relation
  registered in `coldfront.tiered_views` with `schema_name = 'metrics'`,
  takes the column list and types from `pg_attribute`/`format_type` in
  attnum order (never from the JSON keys), and types the values with
  `jsonb_populate_recordset(NULL::<view>, ...)`. The INSERT carries the
  `sqlmarker` comment, like every other collector write. Both insert
  functions are `SECURITY INVOKER` with `search_path = pg_catalog,
  pg_temp`. Neither may read the view: ColdFront refuses DuckDB reads
  inside a function.
- `literal(text)` must stay inlinable, since it runs once per value: a
  single-SELECT `LANGUAGE sql` `IMMUTABLE` `SECURITY INVOKER` function
  with no `SET` clause (a `SET search_path` blocked inlining and made it
  roughly six times slower). An inlined body is parsed under the
  caller's search_path, so every function and operator in it is
  `pg_catalog`-qualified (`OPERATOR(pg_catalog.||)`,
  `pg_catalog.replace`). `CREATE OR REPLACE` clears the `SET` clause of
  a helper an earlier collector created. `TestColdFrontLiteralIsInlinable`
  checks the upgrade, the `EXPLAIN VERBOSE` plan (it needs a multi-row
  `VALUES`, or constant folding hides the call) and a hostile
  search_path.
- No helper function may have an `EXCEPTION` handler. A PL/pgSQL
  `EXCEPTION` block runs its body in a subtransaction, and pg_duckdb
  refuses every write in one ("SAVEPOINT and subtransactions are not
  supported in DuckDB", XX000); wrapping the EXECUTE that way, to keep
  values out of the log (VULN-003), failed every probe live.
  `TestEnsureColdFrontInsertHelper` fails if one is added.
- Residual risk (VULN-003): when the INSERT fails, the error's CONTEXT
  carries the whole statement, and every value of the batch
  (credentials from `pg_settings` or `pg_hba_file_rules` included),
  into the datastore log; with the default `log_min_error_statement`
  the top-level statement is logged too, though its jsonb parameter is
  not unless `log_parameter_max_length_on_error` is raised. Nothing
  short of a subtransaction or a superuser-only setting
  (`log_error_verbosity = terse`, `log_min_error_statement`) avoids it,
  so the datastore's log must be protected like its data.
  `exerciseRejectedRows` pins the behaviour, so a change that removes
  the exposure shows up. The collector itself logs only pgx's
  `PgError.Error()` (severity, message, SQLSTATE), never `Where` or
  `InternalQuery`.
- DuckDB evaluates the rendered values, so they must be SQL it reads:
  - A scalar is `literal(v::text) || '::<type>'`: NULL or a standard
    literal with quotes doubled and backslashes left alone. (`E'...'`
    from `quote_literal` was later shown to work live as well.)
  - An array column (`pg_type.typcategory = 'A'`) is
    `ARRAY[<literal(elem)>, ...]::<type>` built from
    `unnest(...) WITH ORDINALITY`, `ARRAY[]::<type>` when empty and
    `NULL::<type>` when NULL. DuckDB cannot parse the `'{...}'` array
    literal, which failed every `pg_settings`, `pg_hba_file_rules` and
    `pg_node_role` write live. A multi-dimensional value would be
    flattened, so it is refused.
  - The functions pin `standard_conforming_strings = on`,
    `DateStyle = 'ISO, YMD'`, `IntervalStyle = 'postgres'` and
    `extra_float_digits = 1`, so the text means the same whatever the
    session settings.
  - One expression per column is concatenated, rather than passing
    every column to one `format()`, so a wide table stays under the
    100-argument limit.

### Values, batches and failures

- `jsonValue` makes the JSON faithful: `time.Time` as RFC 3339 with
  nanoseconds, integers and `pgtype.Numeric` as exact JSON numbers
  (jsonb keeps them exactly), `pgtype.Interval` as "N months N days N
  microseconds", and a string or `[]byte` for a json/jsonb column
  embedded as the document (`pg_node_role.role_details` is a string),
  with invalid JSON an error. Every column must be in the spec, since
  the helper would silently ignore an unknown key.
- NaN and infinite floats cannot be JSON: they are stored as NULL, with
  one log line per batch and column. Failing or dropping the row would
  lose the rest of a sample for one undefined gauge.
- `splitColdFrontBatches` bounds a statement by `coldFrontBatchRows`
  (5,000; 100 in the default mode) and `coldFrontBatchBytes` (16 MiB of
  encoded JSON). Each statement is one Iceberg commit, so a normal
  probe run should be one statement; but free text such as
  `pg_stat_statements.query` is unbounded and comes from the monitored
  server, so rows alone could push a batch past jsonb's 256 MB or the
  1 GB literal-INSERT limit and fail that probe every cycle
  (VULN-002). 16 MiB keeps the helper's working copies well inside
  those limits; a single larger row is still sent on its own. There is
  no value truncation, in either mode.
- `writeOrHalve` retries in halves a batch that fails with an error one
  row's data can cause (`rowSpecificColdFrontError`): SQLSTATE class 22,
  23 or 54, or an XX000 whose message is a DuckDB "Conversion Error".
  Every other XX000 is systemic, and so is any message saying
  something is not supported or naming a SAVEPOINT or subtransaction:
  live, the subtransaction error (XX000) was mistaken for a row error
  and rows were dropped one by one. Splitting lets one refused row be
  dropped and logged with only the table, its `connection_id` and the
  SQLSTATE, never the values. Other errors are returned at once, and more than
  `maxColdFrontDroppedRows` (10) dropped rows in one call is treated as
  systemic and returned, bounding statements and log lines. That error
  is a `*probes.ColdFrontRowsRefusedError` (table, SQLSTATE, dropped and
  total rows, `SQLState()` method) and deliberately does not wrap the
  `*pgconn.PgError`, whose message can quote the rejected value
  (`invalid input syntax for type ...: "<value>"`); `errors.As` for a
  `PgError` does not match it, and no caller of `StoreMetrics` needs it
  to. `TestWriteColdFrontBatchesDropLimitErrorHidesValues` pins this.

## Startup and Runtime

- After `Migrate`, under ColdFront only, `initializeSchema` calls
  `database.EnsureColdFrontEmbeddings` (`coldfront_embeddings.go`)
  through `ensureEmbeddingsAfterMigrate`, with a 30-second timeout. It
  creates what v1 skipped, already at v6's `halfvec(4000)` width:
  `anomaly_embeddings` with its HNSW index and the
  `fk_anomaly_candidates_embedding` key (`createAnomalyEmbeddings`),
  then `chat_memories.embedding` and its index
  (`addChatMemoryEmbedding`); migration 1's savepointed
  `runPgVectorSetup`/`runChatMemoryEmbeddingSetup` share those bodies.
  Each runs in its own transaction with no SAVEPOINT and no Iceberg
  statement, and only when its object is missing, so a normal startup
  takes no lock and an existing ColdFront datastore gains the objects
  on its next start. Failures are logged (always shown, naming alerter
  Tier 2 and chat memory embeddings as disabled), never returned. It
  relies on pg_duckdb allowing the subtransaction that
  `addConstraintIfMissing`'s `EXCEPTION` block opens in a transaction
  that has not touched DuckDB; that is unverified against live
  ColdFront.

- `database.PrepareMetricsStorage` runs in `initializeSchema` before
  `Migrate`, bounded by `prepareMetricsStorageTimeout`: 10 seconds in
  the default mode, 60 under ColdFront, whose attach and namespace
  steps go over the network to the catalog and object storage. It calls `CheckMetricsStorage`, which under ColdFront
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
stub's emulation, which drops and recreates the view as ColdFront does
(so lost comments show up), because no stub can intercept DDL on a
view; the test also asserts a plain view rejects that statement. The
stub registers each view in `coldfront.tiered_views`, and
`exerciseInsertHelper` (`database/coldfront_insert_test.go`) writes
every column type of every table through `StoreMetrics` and the helper,
with a NULL row, awkward quoting, a NaN and refused relations; asserts
the rendered SQL for arrays (`exerciseArrayRendering`), since plain
PostgreSQL also accepts `'{...}'`; and, through a trigger that refuses
one `pg_settings` row, that the error text the collector logs
(`err.Error()`) carries no value, that the error's CONTEXT
(`pgErr.Where`) still carries the INSERT (the residual VULN-003
exposure, so its removal shows up), and that `StoreMetrics` drops only
that row (`exerciseRejectedRows`). `TestColdFrontHelperSchemaOwnership` uses a
second role to check the schema is locked down and that a schema or
function another role owns is refused. `TestColdFrontAltersRestoreComments`
asserts the restore statements without a database. None of this verifies ColdFront's own behaviour (its
`ALTER TABLE` interception, insert rewriting, Iceberg partitioning).
