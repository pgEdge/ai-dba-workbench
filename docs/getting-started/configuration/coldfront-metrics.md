# Storing Metrics in pgEdge ColdFront

The collector can store the Workbench's metrics history in pgEdge
ColdFront instead of in partitioned PostgreSQL tables. ColdFront keeps
table data in [Apache Iceberg](https://iceberg.apache.org/) format on
object storage and exposes each Iceberg table to PostgreSQL as an
ordinary view. Metrics history is append-mostly analytic data that
grows steadily; moving that data to object storage lowers storage cost
on the datastore and allows longer retention.

ColdFront storage is a collector option. The server and alerter need no
setting, because both components run metrics queries that work against
either kind of datastore. This page describes the prerequisites, the
configuration option, the operational tasks that ColdFront storage
adds, and the ways in which a ColdFront datastore behaves differently
from a standard datastore.

## Understanding ColdFront Metrics Storage

When ColdFront storage is enabled, the collector creates each table in
the `metrics` schema through the `coldfront.create_iceberg_table`
function instead of with `CREATE TABLE ... PARTITION BY RANGE`. Each
metrics table becomes a PostgreSQL view over an Iceberg table, and
ColdFront partitions the Iceberg table by the day of the `collected_at`
column.

The collector still creates every operational table, such as
`connections`, `probe_configs`, and the alert tables, as an ordinary
PostgreSQL table. Only the tables in the `metrics` schema move to
object storage.

The collector writes metrics rows through helper functions in a
`workbench_coldfront` schema that the collector creates and maintains
itself. Each write appends one Parquet data file and one snapshot to
the Iceberg table; the
[Scheduling the ColdFront Compactor](#scheduling-the-coldfront-compactor)
section explains why that matters for operations.

## Meeting the Prerequisites

ColdFront storage requires a datastore that has been prepared for
ColdFront before the collector first connects to it. The datastore
must meet the following requirements:

- The datastore PostgreSQL server has the
  [pg_duckdb](https://github.com/duckdb/pg_duckdb) and `coldfront`
  extensions installed.
- ColdFront is configured with a
  [Lakekeeper](https://github.com/lakekeeper/lakekeeper) Iceberg
  catalog and with object storage, as the ColdFront documentation
  describes.
- The Lakekeeper warehouse named in the `coldfront.warehouse` setting
  is reachable from the datastore and writable.
- The installed ColdFront build includes the
  `coldfront._partition_clause(jsonb,text[])` and
  `coldfront.ensure_attached()` functions.
- The datastore roles that the Workbench components use have been
  granted access to ColdFront.
- The collector's datastore role can create a schema in the datastore
  database.

ColdFront reports the same extension version in every build, so the
collector checks for two functions instead of a version number. The
`coldfront._partition_clause(jsonb,text[])` function arrived in pgEdge
ColdFront pull request #92, which also contains the read support from
pull request #85 that the server relies on. The collector also calls
the `coldfront.ensure_attached()` function to attach the Lakekeeper
catalog. The ColdFront tags up to and including `v1.0.0-beta2` predate
pull request #92, so install a ColdFront build that includes that
change. The collector refuses to start when the installed ColdFront
build lacks either function.

In the following example, the query reports whether the installed
ColdFront build provides each function that the collector requires:

```sql
SELECT
    to_regprocedure('coldfront._partition_clause(jsonb,text[])')
        IS NOT NULL AS has_partition_clause,
    to_regprocedure('coldfront.ensure_attached()')
        IS NOT NULL AS has_ensure_attached;
```

Both columns must return `t` before you enable ColdFront storage.

### Granting ColdFront Access to Workbench Roles

ColdFront requires an explicit grant before a PostgreSQL role can
create and use Iceberg tables. In the following example, the
`grant_app_access` function grants ColdFront access to the role that
the collector uses to connect to the datastore:

```sql
SELECT coldfront.grant_app_access('ai_workbench');
```

Replace `ai_workbench` with the collector's `datastore.username` value.
If the server or alerter connects to the datastore as a different role,
run the same function for each of those roles as well.

### Preparing the Collector's Datastore Role

The collector creates the `workbench_coldfront` schema at startup, so
the collector's datastore role needs the `CREATE` privilege on the
datastore database. In the following example, the `GRANT` statement
gives the `ai_workbench` role that privilege on the `ai_workbench`
database:

```sql
GRANT CREATE ON DATABASE ai_workbench TO ai_workbench;
```

The collector's role must own the `workbench_coldfront` schema and
every function in it. The collector revokes all `PUBLIC` access to the
schema and its functions, and refuses to start when another role owns
the schema or any function in it. This rule prevents another role from
replacing the functions that the collector writes metrics through.

Run the collector as a dedicated role that is not a superuser, so that
the collector holds only the privileges it needs.

## Enabling ColdFront Storage

The `use_coldfront_for_metrics` option enables ColdFront storage. The
option is a top-level key in the collector configuration file, and the
`-use-coldfront-for-metrics` command-line flag sets the same value. The
flag overrides the configuration file only when you pass the flag on
the command line.

In the following example, the configuration file enables ColdFront
storage for a new datastore:

```yaml
datastore:
  host: datastore.example.com
  database: ai_workbench
  username: ai_workbench
  password_file: /etc/pgedge/password.txt

use_coldfront_for_metrics: true
```

In the following example, the command enables ColdFront storage with
the command-line flag instead:

```bash
./ai-dba-collector \
    -config /etc/pgedge/ai-dba-collector.yaml \
    -use-coldfront-for-metrics
```

Choose the storage mode when you create the datastore. The collector
creates the metrics tables during its first schema migration, and the
mode cannot change afterwards. The collector checks the datastore
before running any migration and refuses to start when the setting
does not match the existing metrics tables; the
[Collector Configuration](collector.md#troubleshooting) document lists
each startup error and its fix.

### Understanding the Startup Steps

In ColdFront mode, the collector prepares the datastore at every
startup, before running any schema migration. The collector performs
the following steps in order:

1. The collector checks that the required ColdFront functions exist
   and that any existing metrics relations are ColdFront views.
2. The collector calls `coldfront.ensure_attached()` and creates the
   `metrics` Iceberg namespace in the Lakekeeper warehouse, outside any
   transaction, if the namespace does not already exist.
3. The collector creates or replaces the `workbench_coldfront` schema
   and its helper functions, and checks their ownership.

The collector creates the Iceberg namespace itself because
`coldfront.create_iceberg_table` cannot create a new namespace on a
Lakekeeper catalog in the same call that creates a table. Every step
is safe to repeat on each startup.

## Scheduling the ColdFront Compactor

ColdFront storage adds one operational task that the operator must
perform: scheduling the ColdFront compactor for every metrics table.
Each metrics write by the collector is one Iceberg append, which adds
one small Parquet file and one snapshot to the table. The Workbench
does not compact or expire this data.

### Estimating the Snapshot Rate

The collector schedules every probe separately for each monitored
connection. Each execution of a probe against one connection stores
that connection's rows in one write, so a metrics table gains one
snapshot for each probe execution on each monitored connection. A
database-scoped probe collects rows from every database on the
connection before the single write. The number of snapshots that a
table gains per hour is therefore the number of probe executions per
hour multiplied by the number of monitored connections. A probe run
that returns more than 5,000 rows, or more than 16 MiB of JSON, writes
more than one batch and adds one snapshot for each batch.

Without compaction, snapshots and small files accumulate quickly. In
testing against a datastore that monitored a single connection, each
metrics table reached about 900 snapshots after 16 hours of one-minute
probe intervals. Reads then slowed down, and object-store errors
followed that caused pg_duckdb to abort backends and PostgreSQL to
restart after a crash. A datastore that monitors ten connections at
the same interval reaches the same snapshot count in about 90 minutes.

### Running the Compactor

Schedule ColdFront's `cmd/compactor` tool to run regularly against each
metrics table, with the `--expire-snapshots` option so that the
compactor also expires old snapshots. The compactor names a table as
`<schema>.<name>`, and a bare table name refers to the `public` schema;
always pass the `metrics.` prefix for the Workbench's metrics tables.

The `--expire-older-than` option sets the age at which the compactor
expires a snapshot, and defaults to `168h`. At the default, a table
written by one-minute probes keeps about 10,080 snapshots for each
monitored connection, which is more than ten times the count at which
the test datastore failed. Lower `--expire-older-than` so that the
retained snapshots stay well below that count.

The retained snapshots for a table approach the expiry age plus the
interval between compactor runs, divided by the probe interval and
multiplied by the number of monitored connections. An expiry age of
`1h` with an hourly compactor run keeps at most about 120 snapshots for
each connection on a one-minute probe. Shorten both values when the
datastore monitors many connections. Keep the expiry age longer than
the longest transaction that reads a metrics view, because ColdFront
holds a transaction's snapshot until the transaction ends; the
Workbench's own metrics queries are short, but a query that an
operator holds open in `psql` can last longer.

The following example lists the metrics views that ColdFront has
registered, and runs the compactor against each one with a one-hour
snapshot expiry and orphan-file removal:

```bash
for table in $(psql -At -c "SELECT relname FROM coldfront.tiered_views
                            WHERE schema_name = 'metrics'
                            ORDER BY relname"); do
    compactor --table "metrics.${table}" \
        --expire-snapshots --expire-older-than 1h --orphans
done
```

The `psql` client and the compactor both read the connection settings
from the libpq environment variables, such as `PGHOST` and
`PGDATABASE`. The
[ColdFront compaction guide](https://github.com/pgEdge/ColdFront/blob/main/docs/compaction.md)
describes how to build and configure the compactor, and every option
that the compactor accepts.

## Protecting the Datastore Log

When ColdFront rejects a metrics write, the PostgreSQL error context
can carry the values of that batch into the datastore's server log.
Those values can include credential-bearing `pg_settings` values and
`pg_hba_file_rules` options collected from monitored servers. The
collector's own log records only the error message and SQLSTATE.

Protect the datastore log as carefully as the datastore itself. The
following practices reduce the exposure:

- Restrict access to the datastore's log files to the roles and
  operators who may read the datastore's data.
- Keep `log_parameter_max_length_on_error` at its default of `0`, so
  that PostgreSQL does not log the bound batch parameter.
- Set `log_error_verbosity = terse`, or raise
  `log_min_error_statement`, to log less of the failing statement.

The last two settings apply to the whole datastore server, so they
also remove diagnostic detail from every other error in the log.

## Understanding Behavioural Differences

A ColdFront datastore stores the same metrics as a standard datastore,
but the Workbench manages the data differently. The following sections
describe each difference.

### Managing Metrics Retention

The Workbench performs no retention on metrics data in ColdFront mode.
The collector creates and drops no partitions, deletes no old rows, and
does not start its garbage collector. The collector ignores the
`retention_days` value in the `probe_configs` table for every probe,
and logs one line at startup to say so.

The data lifecycle belongs to ColdFront and to the operator. The
ColdFront compactor expires old snapshots and data files, as the
[Scheduling the ColdFront Compactor](#scheduling-the-coldfront-compactor)
section describes. Deleting old metrics rows remains the operator's
responsibility.

### Purging Deleted Connections

When a user deletes a connection, the server's background purge still
removes the connection itself, but leaves the connection's metrics
rows in place in every ColdFront view. The server logs one line for
each purge that names the metrics views in which it left rows. The
operator can remove those rows with the methods that ColdFront
provides.

In ColdFront mode, the collector also creates no foreign keys from the
metrics tables to the `connections` table. Deleting a connection
therefore never cascades to the connection's metrics rows.

### Storing Column Types

ColdFront does not accept every PostgreSQL type that the metrics tables
use, so the collector declares a supported type for some columns and
converts each value before inserting it. The following table describes
how the collector stores each affected type:

| Standard Type | ColdFront Type | Notes |
|---------------|----------------|-------|
| `oid` | `bigint` | The value is stored as a number. |
| `xid` | `bigint` | The value is stored as a number. |
| `inet` | `text` | The address is stored in its text form. |
| `"char"` | `text` | The single character is stored as text. |
| `name` | `text` | The identifier is stored as text. |
| unbounded `numeric` | `numeric(38,0)` | The value is stored as a whole number; the two affected columns hold byte counts. |
| `jsonb` | `jsonb` | Queries return the column as `json`. |

When read through a ColdFront view, `text` columns report the type
`character varying` and `oid` columns report `bigint`. Custom queries
against the metrics tables see these types.

### Writing Metrics in Batches

The collector writes each probe's rows through the helper functions in
batches of at most 5,000 rows and 16 MiB of JSON. Each batch is one
statement and one Iceberg commit, so a normal probe run produces a
single write.

The collector handles values and rejected rows as follows:

- A `NaN` or infinite floating-point value is stored as `NULL`, and the
  collector logs one line for each batch and column affected.
- A row that ColdFront refuses because of its data is dropped, and the
  collector logs only the table, the connection ID, and the SQLSTATE.
- More than ten refused rows in one write, or any error that does not
  come from a single row's data, fails the whole write.

### Indexing Metrics Tables

The collector creates no indexes on metrics tables in ColdFront mode,
including the index that a standard datastore keeps on
`metrics.pg_stat_statements`. Iceberg relies on partition pruning and
on per-file column statistics to limit the data that each query reads.

### Understanding the Missing pgvector Objects

In ColdFront mode, the collector skips the optional pgvector setup
during its schema migration, even when the `vector` extension is
available. The setup runs inside savepoints, and pg_duckdb does not
allow a savepoint in a transaction that has already written to
Iceberg. The collector logs one line to say that the setup was
skipped.

The skipped setup leaves out the following pgvector objects:

- the `embedding` column of the `chat_memories` table, so chat memories
  are stored without vector embeddings.
- the `anomaly_embeddings` table, which holds the embeddings that the
  alerter's Tier 2 similarity matching compares.

When Tier 2 is enabled and an embedding provider is configured, the
alerter still generates an embedding for each anomaly candidate. The
similarity search and the embedding insert then fail because the
`anomaly_embeddings` table does not exist, and the alerter logs an
error for each failure. The alerter passes every such candidate on as
if Tier 2 had found no match, so Tier 2 never suppresses a candidate,
and Tier 3 classifies each candidate without any similar past
anomalies. When Tier 3 is not available, every candidate that passes
Tier 1 becomes an alert, subject to the usual checks for blackouts and
for an open alert on the same metric.

On a ColdFront datastore with Tier 3 configured, set
`anomaly.tier2.enabled` to `false` in the alerter configuration. The
setting avoids an embedding request and two logged errors for every
anomaly candidate, and every candidate still reaches Tier 3. When no
Tier 3 provider is configured either, the alerter disables anomaly
detection altogether at startup, because no AI tier remains to filter
the Tier 1 candidates.

### Reporting Probe Row Counts

The `list_probes` tool reports an approximate row count for each probe.
The tool never counts the rows of a view, so the `row_count` column
shows `unknown` for every probe that ColdFront stores as a view.

## Querying the Metrics Views by Hand

DuckDB, through pg_duckdb, plans and runs every statement that reads a
ColdFront view, including the parts of the statement that read
ordinary tables. Queries written by hand, such as those run in `psql`
or built with the `query_datastore` tool, must follow DuckDB's rules:

- A statement that reads a metrics view cannot also join PostgreSQL
  catalog tables or views such as `pg_settings` or `pg_class`.
- Ordinary tables must be named without a schema; write `connections`
  rather than `public.connections`.
- A statement cannot bind an array parameter, so compare a column with
  a list of values through an `IN` list or a chain of `OR` comparisons.
- `avg()` and `EXTRACT` over integer columns need a `::float8` cast for
  clients that use PostgreSQL's binary protocol, such as pgx.
- Casting an array to `text` produces DuckDB's `[a, b]` format rather
  than PostgreSQL's `{a,b}` format.

The `psql` client uses the text protocol, so a query that works in
`psql` can still fail in a binary-protocol client.

## Understanding the Limitations

ColdFront mode has the following limitations:

- Switching an existing datastore between standard and ColdFront
  storage is not supported.
- The collector refuses to start when the `use_coldfront_for_metrics`
  value does not match the kind of metrics tables already in the
  datastore.
- The collector skips the optional pgvector setup, so the datastore has
  no `chat_memories.embedding` column and no `anomaly_embeddings`
  table; the
  [Understanding the Missing pgvector Objects](#understanding-the-missing-pgvector-objects)
  section describes the effect on the alerter.
- The collector refuses to start on a ColdFront build that lacks the
  `coldfront._partition_clause` or `coldfront.ensure_attached`
  function.
- The `pg_node_role.role_details` column reads back as `json`, so a
  `query_datastore` filter, `order_by`, `group_by`, `min` or `max` on
  that column fails; a plain select of the column works.
- The operator must schedule the ColdFront compactor; the Workbench
  performs no compaction or snapshot expiry.

To move from one storage mode to the other, create a new datastore with
the required mode and point the Workbench components at that datastore.
