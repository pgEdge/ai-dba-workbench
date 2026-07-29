# Monitored Database Privileges

Every monitored connection stores the credentials of a PostgreSQL role
that the Workbench uses to log in to the monitored instance. This page
describes the privileges that role needs, recommends a least-privilege
configuration, and explains the gaps that remain when the role is not a
superuser.

The collector is strictly read-only on monitored instances. The probes
read system catalogues, statistics views, and a small number of
information functions; the collector never resets statistics, never
terminates or cancels backends, and never reloads the server
configuration. All collected metrics are written to the datastore
instead, which uses its own separate credentials described in
[Collector Configuration](collector.md).

## Recommended Configuration

The recommended monitoring role is a dedicated login role that holds the
`pg_monitor` predefined role plus `CONNECT` on each database you want to
monitor. Perform the following steps on every monitored instance,
connected as a superuser.

1. Create the login role with a strong password:

    ```sql
    CREATE ROLE workbench_monitor LOGIN PASSWORD 'use-a-strong-password';
    ```

2. Grant the `pg_monitor` predefined role, which confers
   `pg_read_all_settings`, `pg_read_all_stats`, and
   `pg_stat_scan_tables`:

    ```sql
    GRANT pg_monitor TO workbench_monitor;
    ```

3. Grant `CONNECT` on each database that the collector should probe,
   repeating the statement for every database:

    ```sql
    GRANT CONNECT ON DATABASE myapp TO workbench_monitor;
    ```

4. Create the optional extensions described in
   [Optional Extensions](#optional-extensions) if you want the metrics
   they provide.

5. Add a `pg_hba.conf` entry that permits the new role to connect from
   the host running the collector, then reload the server
   configuration.

Store the resulting credentials against the monitored connection as
described in [Connection Management](../../admin-guide/connections.md).
The alerter needs no privileges at all on monitored instances, because
the alerter only ever reads the datastore.

## Per-Database CONNECT Requirement

Ten of the collector's probes are database-scoped and therefore run in
every database on the monitored instance. The probes concerned are
`pg_stat_database`, `pg_stat_database_conflicts`, `pg_stat_all_tables`,
`pg_stat_all_indexes`, `pg_statio_all_sequences`,
`pg_stat_user_functions`, `pg_extension`, `pg_stat_statements`,
`spock_exception_log`, and `spock_resolutions`.

The collector enumerates the databases to probe with a query equivalent
to the following statement:

```sql
SELECT datname
FROM pg_database
WHERE datallowconn = TRUE
  AND NOT datistemplate;
```

The collector then opens a separate connection to each database in that
list, reusing the same stored credentials. This has three practical
consequences:

- The monitoring role needs `CONNECT` on every non-template database
  where `datallowconn` is true.
- Each optional extension must be created separately in every database
  from which you want the extension's metrics.
- The instance needs enough connection headroom for one collector
  connection per database, bounded by the `datconnlimit` of each
  database, the server's `max_connections` setting, and the collector's
  own `pool.max_connections_per_server` option.

A database that the role cannot connect to is skipped with a logged
error rather than a fatal one, so a missing `CONNECT` grant degrades
coverage silently. This is the most common misconfiguration; check the
collector log after adding a monitored connection, and confirm that
metrics arrive for every database you expect.

## Optional Extensions

Two extensions supply metrics that PostgreSQL does not expose on its
own. Create each extension in every database you want to probe, because
extensions are per-database objects.

The following table describes the optional extensions and the probes
that depend on them:

| Extension | Probes | Additional requirement |
|-----------|--------|------------------------|
| pg_stat_statements | pg_stat_statements | In shared_preload_libraries |
| system_stats | The ten pg_sys_* probes | None beyond the extension |

In the following example, the statements create both extensions in the
current database:

```sql
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
CREATE EXTENSION IF NOT EXISTS system_stats;
```

The probes that rely on these extensions check for the relevant objects
before running, and return no data rather than raising an error when an
extension is absent. You can therefore leave both extensions out
without disturbing the rest of the collection.

## Spock Clusters

Monitored instances that participate in a Spock cluster expose
replication metadata in the `spock` schema. The collector reads five
tables in that schema and calls no Spock functions at all.

In the following example, the statements grant the access the collector
needs for Spock metrics:

```sql
GRANT USAGE ON SCHEMA spock TO workbench_monitor;
GRANT SELECT ON spock.exception_log TO workbench_monitor;
GRANT SELECT ON spock.resolutions TO workbench_monitor;
GRANT SELECT ON spock.local_node TO workbench_monitor;
GRANT SELECT ON spock.node TO workbench_monitor;
GRANT SELECT ON spock.subscription TO workbench_monitor;
```

The `spock.exception_log` and `spock.resolutions` tables are
database-scoped, so grant access in every database that hosts Spock
replication. Instances with no Spock installation need none of these
grants.

## Privileges by Probe

The `pg_monitor` role covers the great majority of the collection, but
a handful of probes need more, and a few need nothing at all. The
following table shows the privilege each group of probes requires:

| Probe or feature | Required privilege |
|------------------|--------------------|
| pg_server_info | pg_read_all_settings, for data_directory |
| pg_settings | pg_read_all_settings, or restricted values are NULL |
| pg_stat_activity | pg_read_all_stats, or other sessions are masked |
| pg_stat_replication | pg_read_all_stats, for sender and receiver rows |
| pg_node_role | pg_read_all_stats, plus superuser for subconninfo |
| pg_stat_connection_security | pg_read_all_stats, for backend detail |
| pg_stat_statements | pg_read_all_stats, or query text is hidden |
| pg_database | pg_read_all_stats, or CONNECT on all databases |
| pg_hba_file_rules | Grants on the view and function, or superuser |
| pg_ident_file_mappings | Grants on the view and function, or superuser |
| The ten database-scoped probes | CONNECT on each database probed |
| spock_exception_log, spock_resolutions | USAGE and SELECT in spock |
| The ten pg_sys_* probes | The system_stats extension, no grants |
| Remaining server-scoped probes | No privilege beyond login |

## Why pg_monitor Is Required

Several probes fail or silently lose data without `pg_monitor`, so
treat the role as a requirement rather than a refinement. The most
important dependencies are as follows:

- The `pg_server_info` probe calls
  `current_setting('data_directory')`, which raises a hard error
  without `pg_read_all_settings` and so fails the whole probe.
- Superuser-restricted values in `pg_settings` read as NULL without
  `pg_read_all_settings`, even though the view itself is readable by
  PUBLIC.
- The `query` column and other cross-session columns of
  `pg_stat_activity` are masked without `pg_read_all_stats`.
- The `pg_stat_wal_receiver` view is defined with a `WHERE pid IS NOT
  NULL` clause over a privileged function, so an unprivileged role sees
  zero rows rather than nulls; this affects both the
  `pg_stat_replication` probe and standby detection in the
  `pg_node_role` probe.
- The wal sender columns of `pg_stat_replication` are masked without
  `pg_read_all_stats`.
- Per-backend detail in `pg_stat_ssl` and `pg_stat_gssapi` is masked
  without `pg_read_all_stats`.
- The `query` text of other users' entries in `pg_stat_statements`
  reads as `<insufficient privilege>` without `pg_read_all_stats`.
- The `pg_database_size()` function requires either `CONNECT` on the
  target database or `pg_read_all_stats`, and the `pg_database` probe
  sizes every row including templates, so `pg_read_all_stats` is the
  reliable answer.

The third role that `pg_monitor` confers, `pg_stat_scan_tables`, is not
currently exercised by any probe, because the collector uses neither
`pgstattuple` nor `pg_buffercache`. The role simply comes along with
`pg_monitor`, so do not go looking for the feature that needs it.

## Privileges Not Required

Two functions that look privileged need no grant at all. The
`pg_server_info` probe calls `pg_control_system()` and the
`pg_node_role` probe calls `pg_control_checkpoint()`; PostgreSQL leaves
execution of both available to PUBLIC. Adding explicit grants for
either function achieves nothing.

The collector also needs no access to the data in your tables. Table
and index statistics come from catalogue views combined with
`pg_table_size()` and `pg_relation_size()`, which report on relation
storage rather than reading rows.

## Known Gaps Under a Non-Superuser Role

A `pg_monitor` role leaves a small number of gaps, each of which
degrades quietly rather than raising an error. Review the following
list and decide whether the affected data matters in your environment.

- The `pg_hba_file_rules` and `pg_ident_file_mappings` views are
  revoked from PUBLIC and granted to no predefined role, so both
  probes return no data. The `pg_monitor` role does not cover them.
- The `subconninfo` column of `pg_subscription` is superuser-only,
  although SELECT on every other column is granted to PUBLIC. The
  `pg_node_role` probe reads that column to derive the publisher host
  and port, and degrades silently without access.
- A database that the role cannot connect to is skipped, as described
  in [Per-Database CONNECT Requirement](#per-database-connect-requirement).
- The `system_stats` and Spock probes return empty results when the
  corresponding extension or schema is absent from the monitored
  server.

To close the authentication file gaps without granting superuser, grant
both the view and the underlying function of the same name. The
function grant is easy to overlook, and the view alone is not enough.

In the following example, the statements grant access to both
authentication file views and their functions:

```sql
GRANT SELECT ON pg_hba_file_rules TO workbench_monitor;
GRANT EXECUTE ON FUNCTION pg_hba_file_rules() TO workbench_monitor;
GRANT SELECT ON pg_ident_file_mappings TO workbench_monitor;
GRANT EXECUTE ON FUNCTION pg_ident_file_mappings()
    TO workbench_monitor;
```

A superuser monitoring role covers every probe with no gaps at all, and
that is the trade-off to weigh: complete coverage against least
privilege. The shipped demonstration configuration at
[ai-dba-server.yaml](https://github.com/pgEdge/ai-dba-workbench/blob/main/examples/walkthrough/config/ai-dba-server.yaml)
monitors as the `postgres` superuser for the sake of simplicity, which
suits a walkthrough rather than a production estate.

## Privileges for AI and MCP Features

The MCP server connects to monitored instances with the same stored
credentials that the collector uses, and several of its tools do read
user data. The following table describes the tools that touch table
data:

| Tool | Access to user data |
|------|---------------------|
| query_database | Executes arbitrary SQL supplied by the user |
| execute_explain | Runs EXPLAIN ANALYZE, which executes the query |
| test_query | Runs EXPLAIN only, without executing the query |
| count_rows | Runs SELECT COUNT(*) against a table |
| get_schema_info | Reads schema metadata for a database |
| similarity_search | Reads vector data from a table |

If you want these features to work, the monitoring role additionally
needs `USAGE` on the relevant user schemas and `SELECT` on the tables
you are willing to expose. The collector alone needs neither.

Grant this access deliberately, because the read_write setting on a
Workbench connection is an application-level gate rather than a
database one. The privileges of the PostgreSQL role are the real
backstop: a read_write grant in the Workbench achieves nothing unless
the underlying role also holds write privileges, and granting the role
write privileges widens what the AI features can do regardless of the
Workbench setting. Treat the two as one security decision.

## Next Steps

The following documents cover related configuration.

- The [Collector Configuration](collector.md) document describes the
  datastore credentials, connection pooling, and probe settings.
- The [Connection Management](../../admin-guide/connections.md)
  document explains how to create and select monitored connections.
- The
  [Probe Reference](../../developer-guide/collector/probe-reference.md)
  document lists the source views and columns that each probe reads.
