# Storing Metrics in pgEdge ColdFront

The collector can store the Workbench's metrics history in pgEdge
ColdFront instead of in partitioned PostgreSQL tables. ColdFront keeps
table data in [Apache Iceberg](https://iceberg.apache.org/) format on
object storage and exposes each Iceberg table to PostgreSQL as an
ordinary view. Metrics history is append-mostly analytic data that
grows steadily; moving that data to object storage lowers storage cost
on the datastore and allows longer retention.

ColdFront storage is a collector option; the server and alerter need no
setting, because both components read the metrics tables through the
same SQL in either mode. This page describes the prerequisites, the
configuration option, and the ways in which a ColdFront datastore
behaves differently from a standard datastore.

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

## Meeting the Prerequisites

ColdFront storage requires a datastore that has been prepared for
ColdFront before the collector first connects to it. The datastore
must meet the following requirements:

- The datastore PostgreSQL server has the
  [pg_duckdb](https://github.com/duckdb/pg_duckdb) and `coldfront`
  extensions installed.
- ColdFront is configured with an Iceberg catalog, such as
  [Lakekeeper](https://github.com/lakekeeper/lakekeeper), and with
  object storage, as the ColdFront documentation describes.
- The installed ColdFront build includes the
  `coldfront._partition_clause(jsonb,text[])` function.
- The datastore roles that the Workbench components use have been
  granted access to ColdFront.

The `coldfront._partition_clause` function arrived in pgEdge ColdFront
pull request #92, which also contains the read support from pull
request #85 that the server relies on. Use ColdFront `1.0.0-rc1` or
later; the `v1.0.0-beta2` release is too old. ColdFront reports the
same extension version in every build, so the collector checks for the
function instead of a version number. The collector refuses to start
when the installed ColdFront build lacks that function.

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

## Understanding Behavioural Differences

A ColdFront datastore stores the same metrics as a standard datastore,
but the Workbench manages the data differently. The following sections
describe each difference.

### Managing Metrics Retention

The Workbench performs no retention on metrics data in ColdFront mode.
The collector creates and drops no partitions, deletes no old rows, and
does not start its garbage collector. The collector ignores the
`retention_days` value in the `probe_configs` table for every probe.

The data lifecycle belongs to ColdFront and to the operator.
ColdFront's compactor, run with the `--expire-snapshots` and
`--orphans` options, removes expired Iceberg snapshots and orphaned
data files. Deleting old metrics rows remains the operator's
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

Custom queries against the metrics tables, such as those run through
the `query_metrics` tool or directly with SQL, see the ColdFront types.

### Indexing Metrics Tables

The collector creates no indexes on metrics tables in ColdFront mode,
including the index that a standard datastore keeps on
`metrics.pg_stat_statements`. Iceberg relies on partition pruning and
on per-file column statistics to limit the data that each query reads.

### Reporting Probe Row Counts

The `list_probes` tool reports an approximate row count for each probe.
The tool never counts the rows of a view, so the `row_count` column
shows `unknown` for every probe that ColdFront stores as a view.

## Understanding the Limitations

ColdFront mode has the following limitations:

- Switching an existing datastore between standard and ColdFront
  storage is not supported.
- The collector refuses to start when the `use_coldfront_for_metrics`
  value does not match the kind of metrics tables already in the
  datastore.
- The optional pgvector chat-memory embedding feature is disabled.
- The collector refuses to start on a ColdFront build that lacks the
  `coldfront._partition_clause` function.
- The `pg_node_role.role_details` column reads back as `json`, so a
  `query_datastore` filter, `order_by`, `group_by`, `min` or `max` on
  that column fails; a plain select of the column works.

To move from one storage mode to the other, create a new datastore with
the required mode and point the Workbench components at that datastore.
