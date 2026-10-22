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
- The `duckdb.postgres_role` setting names a role whose members may run
  DuckDB, as the
  [Configuring the DuckDB Role](#configuring-the-duckdb-role) section
  describes.
- The installed ColdFront build includes the
  `coldfront._partition_clause(jsonb,text[])` and
  `coldfront.ensure_attached()` functions.
- The datastore roles that the Workbench components use have been
  granted access to ColdFront.
- The collector's datastore role can write to the ColdFront view
  registry while the collector creates the metrics tables.
- The collector's datastore role can create a schema in the datastore
  database and a table in the schema on the role's `search_path`.
- A superuser has created the `vector` extension in the schema on the
  `search_path` of the Workbench roles, when the collector's role is not
  a superuser and the deployment uses Tier 2 anomaly matching or chat
  memory embeddings.

ColdFront reports the same extension version in every build, so the
collector checks for two functions instead of a version number. The
`coldfront._partition_clause(jsonb,text[])` function arrived in pgEdge
ColdFront pull request #92, which also contains the read support from
pull request #85 that the server relies on. The collector also calls
the `coldfront.ensure_attached()` function to attach the Lakekeeper
catalog. ColdFront 1.0.0-beta3 is the first release that includes pull
request #92; the releases up to and including 1.0.0-beta2 predate the
change, so install ColdFront 1.0.0-beta3 or later. The collector
refuses to start when the installed ColdFront build lacks either
function.

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

### Configuring the DuckDB Role

The `duckdb.postgres_role` setting names the role whose members may run
DuckDB. Without the setting, only superusers can run DuckDB, and the
`coldfront.grant_app_access` function fails. The collector should run as
a role that is not a superuser, so the datastore needs this setting.

In the following example, the `CREATE ROLE` statement creates the role
that the setting names; create the role before you grant ColdFront
access to any Workbench role:

```sql
CREATE ROLE coldfront_duckdb NOLOGIN;
```

In the following example, the `postgresql.conf` line names that role;
restart PostgreSQL after you add the line:

```ini
duckdb.postgres_role = 'coldfront_duckdb'
```

The `grant_app_access` function makes each role it covers a member of
the DuckDB role. The
[ColdFront configuration guide](https://github.com/pgEdge/ColdFront/blob/main/docs/configuration.md)
lists this setting with the other PostgreSQL settings that ColdFront
requires.

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

The `grant_app_access` function reads ColdFront's view registry when
you call the function, so the call covers only the views that exist at
that moment. Before the collector's first start, no metrics views
exist yet. The collector's role owns the metrics views that the
collector creates and needs no further grant on those views. The
function is idempotent, so a repeated call is safe. ColdFront revokes
the function from `PUBLIC`, so a superuser or another operator role
must run each call.

The `grant_app_access` function does not give the collector everything
that creating the metrics tables requires. The collector creates each
metrics table by calling `coldfront.create_iceberg_table`, which runs
with the privileges of the calling role and inserts or updates the
table's row in the `coldfront.tiered_views` registry. The
`grant_app_access` function grants only `SELECT` on that registry, so a
collector role that is not a superuser also needs `INSERT` and `UPDATE`
on the registry. In the following example, the `GRANT` statement gives
the `ai_workbench` role those privileges:

```sql
GRANT INSERT, UPDATE ON coldfront.tiered_views TO ai_workbench;
```

The collector needs the registry privileges during every schema
migration that creates a metrics table; the first start and any
upgrade that adds a metrics table run such a migration. The privileges
let the role add or change the registry row of any ColdFront view in
the database, including a view that another application registered. On
a shared datastore, revoke the privileges after the migration completes
and grant them again before the next upgrade. In the following
example, the `REVOKE` statement removes the registry privileges:

```sql
REVOKE INSERT, UPDATE ON coldfront.tiered_views FROM ai_workbench;
```

Run the first start under the collector's own role rather than under a
superuser. The collector's role must own the `workbench_coldfront`
schema, so after a first start under another role the collector
refuses to start under its own role.

The setup that this page describes has been verified end to end with a
collector and server role that is not a superuser and does not own the
datastore database. When the first migration fails with a permission
error, the error message names the object that the role cannot access.

Start the Workbench components on a new ColdFront datastore in the
following order:

1. Run `grant_app_access` for the collector's datastore role, and grant
   the role `INSERT` and `UPDATE` on the `coldfront.tiered_views`
   registry.
2. Create the `vector` extension as a superuser when the collector's
   role is not a superuser, as the
   [Installing the pgvector Extension](#installing-the-pgvector-extension)
   section describes.
3. Start the collector, which creates the metrics views during its
   first schema migration.
4. Run `grant_app_access` again for the server's role and the
   alerter's role when either role differs from the collector's role.
5. Start the server and the alerter.

The order matters because the server's startup check reads the
`metrics.pg_settings` view. A server role that differs from the
collector's role cannot read that view until the grant runs after the
view exists, so the server fails to start until you re-run the grant.
Repeat the fourth step after each Workbench upgrade, because an upgrade
can add metrics tables.

The function grants each role `SELECT`, `INSERT`, `UPDATE`, and
`DELETE` on every view registered in ColdFront at the time of the call.
The grant therefore includes the metrics views and any ColdFront view
that another application registered in the same database.

### Preparing the Collector's Datastore Role

The collector creates the `workbench_coldfront` and `metrics` schemas,
so the collector's datastore role needs the `CREATE` privilege on the
datastore database. In the following example, the `GRANT` statement
gives the `ai_workbench` role that privilege on the `ai_workbench`
database:

```sql
GRANT CREATE ON DATABASE ai_workbench TO ai_workbench;
```

The `CREATE` privilege on the database allows the role to create
schemas, but not to create tables in an existing schema. The
migrations create the tables outside the `metrics` schema without a
schema prefix, so the role also needs the `CREATE` privilege on the
schema that the role's `search_path` resolves to. From PostgreSQL 15
onwards, only the database owner can create objects in the `public`
schema by default. When the collector's role does not own the datastore
database, the `GRANT` statement in the following example gives the
role that privilege on the `public` schema:

```sql
GRANT CREATE ON SCHEMA public TO ai_workbench;
```

On a deployment that uses neither alerter Tier 2 anomaly matching nor
chat memory embeddings, you can instead create a schema that the
collector's role owns, and set the `search_path` of each Workbench
role to that schema; the
[Keeping Workbench Tables out of the ColdFront Schema](#keeping-workbench-tables-out-of-the-coldfront-schema)
section shows the `ALTER ROLE` statement that sets a role's
`search_path`. The alternative does not remove the need for the grant
on `public` when either feature is in use, because the collector
always creates the `anomaly_embeddings` table in the `public` schema,
whatever the `search_path`. The collector adds the `chat_memories`
embedding column only after the `anomaly_embeddings` table exists, so
chat memory embeddings depend on the same grant.

The collector's role must own the `workbench_coldfront` schema and
every function in it. The collector revokes all `PUBLIC` access to the
schema and its functions, and refuses to start when another role owns
the schema or any function in it. This rule prevents another role from
replacing the functions that the collector writes metrics through.

Run the collector as a dedicated role that is not a superuser, so that
the collector holds only the privileges it needs.

### Keeping Workbench Tables out of the ColdFront Schema

The Workbench migrations create the tables outside the `metrics` schema,
such as `connections` and `alerts`, without a schema prefix. PostgreSQL
creates each of those tables in the first existing schema on the
`search_path` of the collector's role. The default `search_path` of
`"$user", public` resolves `$user` to the role name, so a role named
`coldfront` places the tables in the schema that the ColdFront extension
creates. Workbench tables in the extension's schema can clash with
ColdFront object names, can be affected when the extension is upgraded
or dropped, and make the schema's grants harder to audit.

Connect the collector, the server, and the alerter as a dedicated
application role that is not named `coldfront`; the ColdFront
installation examples connect as a role with that name. The
[Connecting to ColdFront](https://github.com/pgEdge/ColdFront/blob/main/docs/connecting.md)
guide likewise onboards a separate least-privilege application role
with the `grant_app_access` function. Alternatively, set the role's
`search_path` before the collector's first start. In the following
example, the `ALTER ROLE` statement makes PostgreSQL create the
unqualified Workbench tables in the `public` schema:

```sql
ALTER ROLE ai_workbench SET search_path = public;
```

Changing the `search_path` after the collector's first start does not
move the tables that the migrations have already created.

### Installing the pgvector Extension

A collector role that is not a superuser cannot create the `vector`
extension; the attempt fails with a `permission denied to create
extension "vector"` error. The collector logs the failure and continues,
but alerter Tier 2 anomaly matching and chat memory embeddings stay
disabled until a superuser creates the extension. The
[Creating the pgvector Objects](#creating-the-pgvector-objects) section
describes the objects that the collector creates once the extension
exists.

A superuser must create the extension in the datastore database before
the collector's first start. In the following example, the
`CREATE EXTENSION` statement creates the extension in the `public`
schema:

```sql
CREATE EXTENSION vector SCHEMA public;
```

Name the schema on the `search_path` of the Workbench roles when that
schema is not `public`. When the extension is created after the first
start, restart the collector; the collector creates the
`anomaly_embeddings` table and the `chat_memories.embedding` column at
the next start.

The `SCHEMA` clause matters because, without the clause, PostgreSQL
creates the extension in the first existing schema on the `search_path`
of the superuser who runs the statement. For a superuser named
`coldfront`, the default `search_path` resolves to the `coldfront`
schema that the ColdFront extension creates, as the
[Keeping Workbench Tables out of the ColdFront Schema](#keeping-workbench-tables-out-of-the-coldfront-schema)
section explains. The Workbench roles then cannot resolve the
extension's types, and the collector fails with a `type "halfvec" does
not exist` error. In the following example, the `ALTER EXTENSION`
statement moves an extension that was created in the wrong schema to
the `public` schema:

```sql
ALTER EXTENSION vector SET SCHEMA public;
```

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
[ColdFront Metrics Storage Errors](../../troubleshooting/coldfront-errors.md)
page lists each startup error and its fix.

### Restarting the Server After a Storage Change

The server detects a ColdFront datastore once, when the server starts
and opens its datastore connection pool. The server treats the
datastore as a ColdFront datastore when the `coldfront` extension is
installed or when the `metrics` schema holds a view. On a ColdFront
datastore, the server runs its queries without named prepared
statements, so that a query cannot fail with the
`Catalog "ice" does not exist!` error after an earlier query rolled
back. A configuration reload through `SIGHUP` does not repeat the
detection.

Restart the server whenever the datastore that the server connects to
changes between standard and ColdFront storage, for example when you
rebuild the datastore with the other storage mode under the same
connection settings. Until the restart, a server that started against
a standard datastore can fail ColdFront queries with that error. A
server that started against a ColdFront datastore keeps working on a
standard datastore, but makes an extra round trip for every query.

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

After the schema migrations, the collector also creates any missing
pgvector objects; the
[Creating the pgvector Objects](#creating-the-pgvector-objects)
section describes that step.

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
datastore monitors many connections.

A transaction that reads a metrics view keeps the snapshot of its
first read until the transaction ends, and snapshot expiry does not
know about that transaction. When expiry deletes the files of that
snapshot, the transaction's next scan fails with the object store's
not-found error. The ColdFront compaction guide advises keeping
`--expire-older-than` longer than the longest transaction that reads
the table. Expiry measures the age of the snapshot rather than the age
of the transaction, so a transaction can begin on a snapshot that is
already old. Keep the expiry age longer than the longest reader's
lifetime plus the age of the snapshot when the reader first reads the
view. While the collector writes a table, that snapshot age is at most
about one probe interval; after the collector stops writing a table,
the current snapshot can grow older. The Workbench's own metrics
queries are short, but a query that an operator holds open in `psql`
can last much longer.

The following script lists the metrics views that ColdFront has
registered, and runs the compactor against each one with a one-hour
snapshot expiry and orphan-file removal. The script exits with a
non-zero status when the listing fails, when the listing returns no
views, or when any compactor run fails:

```bash
#!/usr/bin/env bash
set -euo pipefail

tables=$(psql -X -At -v ON_ERROR_STOP=1 \
    -c "SELECT relname FROM coldfront.tiered_views
        WHERE schema_name = 'metrics' ORDER BY relname")

if [ -z "${tables}" ]; then
    echo "No ColdFront metrics views found." >&2
    exit 1
fi

status=0
for table in ${tables}; do
    if ! compactor --table "metrics.${table}" \
            --expire-snapshots --expire-older-than 1h --orphans; then
        echo "The compactor failed for metrics.${table}." >&2
        status=1
    fi
done
exit "${status}"
```

The script continues past a failed table so that one failure does not
stop maintenance of the other tables, and still reports the failure
through its exit status.

The `psql` client reads the connection settings from the libpq
environment variables, such as `PGHOST` and `PGDATABASE`. The
compactor in ColdFront 1.0.0-beta3 reads the same variables, or accepts
a connection string through the `--dsn` option, and reads the catalog
and storage settings from the datastore. The script requires such a
compactor; a compactor built from any revision that includes pgEdge
ColdFront pull request #98 behaves the same way.

A compactor built from a development revision that includes pull
request #92 but predates pull request #98 does not read the libpq
environment. That compactor requires the `--config` option, which names
a deployment YAML file that holds the connection string and the storage
credentials, and exits without compacting when the option is missing.
No ColdFront release contains such a compactor, because ColdFront
1.0.0-beta3 includes both pull requests. In the following example, the
compactor command in the script passes a deployment file to an earlier
compactor:

```bash
compactor --config /etc/coldfront/deploy.yaml --table "metrics.${table}" \
    --expire-snapshots --expire-older-than 1h --orphans
```

The
[ColdFront compaction guide](https://github.com/pgEdge/ColdFront/blob/main/docs/compaction.md)
describes how to build and configure the current compactor, and every
option that the compactor accepts. The
[compaction guide at the pull request #92 merge](https://github.com/pgEdge/ColdFront/blob/968f3594b238d36ff453687b40c24311406297bd/docs/compaction.md)
describes the deployment file that an earlier compactor reads.

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
- Set `log_error_verbosity = terse`, which omits the `DETAIL`, `HINT`,
  `QUERY`, and `CONTEXT` fields from each logged error; the `CONTEXT`
  field is where the batch values can appear.
- Keep `log_parameter_max_length_on_error` at its default of `0`, so
  that PostgreSQL does not log the bound batch parameter with an error.

Statement logging is a separate exposure that `log_error_verbosity`
does not control. The `log_min_error_statement` setting decides
whether PostgreSQL logs the text of a failing statement; setting the
value to `panic` stops that logging, but does not remove the `CONTEXT`
field, so the setting is not a substitute for terse verbosity. When
`log_statement` or `log_min_duration_statement` logs the collector's
writes, set `log_parameter_max_length` to `0` so that those entries
omit the bound batch parameter; the default of `-1` logs the parameter
in full.

These settings apply to the whole datastore server, so they also
remove diagnostic detail from every other entry in the log.

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

ColdFront rewrites a `DELETE` statement on a metrics view into a
delete on the Iceberg table. In the following example, the statement
removes the rows older than 30 days from the `metrics.pg_settings`
view:

```sql
DELETE FROM metrics.pg_settings
WHERE collected_at < now() - interval '30 days';
```

Run such a statement as the collector's datastore role, a superuser,
or a role that `coldfront.grant_app_access` covers. The `DELETE`
privilege alone is not enough, because the statement also needs the
DuckDB role membership and the `coldfront` schema access that the
grant provides. Run each statement on its own, outside any transaction
block that also writes an ordinary PostgreSQL table, because pg_duckdb
refuses a transaction that writes both kinds of table. A deleted row's
values can remain in object storage after the delete, as the
[Purging Deleted Connections](#purging-deleted-connections) section
describes.

### Purging Deleted Connections

When a user deletes a connection, the server's background purge also
removes the connection's metrics rows from each ColdFront view. The
purge issues one `DELETE` statement with a `connection_id` condition
for each metrics view, and runs each statement in its own transaction,
because pg_duckdb refuses a transaction that writes both Iceberg and
ordinary PostgreSQL tables. The purge removes the connection itself
only after the metrics deletes succeed.

When a delete on a metrics view fails, the purge leaves the
connection row in place and reports the failure, so that a later purge
run can retry the connection. The server's datastore role needs the
access that `coldfront.grant_app_access` provides, as the
[Granting ColdFront Access to Workbench Roles](#granting-coldfront-access-to-workbench-roles)
section describes. Check the server log for the purge failure when a
deleted connection's row remains in the datastore.

In ColdFront mode, the collector creates no foreign keys from the
metrics tables to the `connections` table. Deleting a connection
therefore never cascades to the connection's metrics rows, and the
purge's own deletes are the only step that removes them.

Iceberg records a delete in delete files and does not rewrite the
existing data files, so the deleted values remain in object storage.
The values leave object storage only after the compactor rewrites the
affected data files and snapshot expiry deletes the files that only
expired snapshots reference. The
[Scheduling the ColdFront Compactor](#scheduling-the-coldfront-compactor)
section describes both steps.

Scheduled compaction does not guarantee that the compactor ever
rewrites a given data file. The compactor rewrites a data file only
when the file is smaller than 75% of the target size or carries five
or more delete files, and leaves a partition alone when the partition
holds fewer than five such files. A day partition that an earlier run
has already compacted can therefore keep a deleted row in a current
data file indefinitely, and snapshot expiry never removes a file that
the current snapshot references. ColdFront documents no option that
forces the compactor to rewrite a particular partition. Do not rely
on a delete, compaction, and snapshot expiry to erase metrics values
from object storage. The
[ColdFront compaction guide](https://github.com/pgEdge/ColdFront/blob/main/docs/compaction.md)
lists the conditions under which the compactor rewrites a data file.

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
  collector logs at most one line an hour for each metrics column
  affected, with the number of values stored as `NULL` since the
  column's previous line.
- A row that ColdFront refuses because of its data is dropped, and the
  collector logs only the table, the connection ID, and the SQLSTATE.
- More than ten refused rows in one write, or any error that does not
  come from a single row's data, fails the write; the rows that the
  collector has already committed remain in the table.

To find a refused row, the collector splits the failed batch in half
and retries each half, repeating the split until the refused row stands
alone. Each half that succeeds is a separate statement and Iceberg
commit, so one refused row splits a batch into several commits and
adds a snapshot for each commit.

### Indexing Metrics Tables

The collector creates no indexes on metrics tables in ColdFront mode,
including the index that a standard datastore keeps on
`metrics.pg_stat_statements`. Iceberg relies on partition pruning and
on per-file column statistics to limit the data that each query reads.

### Creating the pgvector Objects

Alerter Tier 2 anomaly matching and chat memory embeddings work in
ColdFront mode when the `vector` extension is available on the
datastore. Migration 1 defers the optional pgvector setup in ColdFront
mode, because the setup runs inside savepoints and pg_duckdb does not
allow a savepoint in a transaction that has already written to
Iceberg. The collector logs a `pgvector setup deferred` line when
migration 1 runs.

At every startup, after the schema migrations, the collector creates
any of the following pgvector objects that are missing:

- the `vector` extension, when the extension is available but not yet
  installed and the collector's role may create the extension.
- the `anomaly_embeddings` table, with its HNSW index and its foreign
  key to `anomaly_candidates`, which holds the embeddings that the
  alerter's Tier 2 similarity matching compares.
- the `embedding` column of the `chat_memories` table, at
  `halfvec(4000)`, which stores the vector embedding of each chat
  memory.

The collector creates the table and the column in separate
transactions that touch no Iceberg table and take no savepoint. The
collector skips any object that already exists, so a normal startup
takes no lock on either table. A ColdFront datastore that the
collector set up before this step existed receives the objects at the
next collector start, as does a datastore on which an administrator
installs pgvector later. The collector allows the step 30 seconds to
complete.

A failure in this step never stops the collector; the collector logs
the failure and continues without the affected objects. When the
`anomaly_embeddings` step fails, the collector also skips the
`chat_memories` column step. Look for the following log lines when the
objects are missing after a startup:

- `pgvector extension not available` means that the datastore has no
  `vector` extension to install, so the collector created neither
  object.
- `pgvector setup skipped` means that the availability check or the
  `anomaly_embeddings` step failed, so the collector created neither
  object; the line includes the underlying error.
- `chat_memories embedding setup skipped` means that the
  `anomaly_embeddings` table exists but the `chat_memories` column step
  failed; the line includes the underlying error.

While the collector cannot create the `anomaly_embeddings` table, Tier
2 cannot match candidates. When Tier 2 is enabled and an embedding
provider is configured, the alerter still generates an embedding for
each anomaly candidate. The similarity search and the embedding insert
then fail because the `anomaly_embeddings` table does not exist, and
the alerter logs an error for each failure. The alerter passes every
such candidate on as if Tier 2 had found no match, so Tier 2 never
suppresses a candidate, and Tier 3 classifies each candidate without
any similar past anomalies. When Tier 3 is not available, every
candidate that passes Tier 1 becomes an alert, subject to the usual
suppression checks, such as an active blackout or an open alert on the
same metric.

When the table cannot be created on a datastore with Tier 3
configured, set `anomaly.tier2.enabled` to `false` in the alerter
configuration. The setting avoids an embedding request and two logged
errors for every anomaly candidate, and every candidate still reaches
Tier 3. When no Tier 3 provider is configured either, the alerter
disables anomaly detection altogether at startup, because no AI tier
remains to filter the Tier 1 candidates.

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
- The collector refuses to start on a ColdFront build that lacks the
  `coldfront._partition_clause` or `coldfront.ensure_attached`
  function.
- The `pg_node_role.role_details` column reads back as `json`, so a
  `query_datastore` filter, `order_by`, `group_by`, `min` or `max` on
  that column fails; a plain select of the column works.
- The operator must schedule the ColdFront compactor; the Workbench
  performs no compaction or snapshot expiry.
- A probe run larger than 5,000 rows or 16 MiB of JSON is written as
  several separate Iceberg commits, and ColdFront has no savepoints to
  roll back such a run; when a later batch fails, the earlier batches
  remain and leave a partial set of rows under one `collected_at`
  value.

To move from one storage mode to the other, create a new datastore with
the required mode and point the Workbench components at that datastore.

A rebuilt ColdFront datastore can silently reuse old Iceberg tables,
because `coldfront.create_iceberg_table` creates the namespace and each
table only if they do not exist, and the collector's startup check reads
only the PostgreSQL catalog. A new datastore that uses a warehouse still
holding `metrics` tables from an earlier datastore inherits their old
rows and column set; the connection IDs in those rows may not match the
new datastore's connections. Unless you intend to keep that history,
point the rebuilt datastore at a fresh warehouse, or run
`coldfront.drop_iceberg_table()` on each `metrics` table from the old
datastore before you retire the old datastore, as the
[ColdFront usage guide](https://github.com/pgEdge/ColdFront/blob/main/docs/using_coldfront/index.md#dropping-an-iceberg-table)
describes.
