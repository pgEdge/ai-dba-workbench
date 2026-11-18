# ColdFront Metrics Storage Errors

A collector that stores metrics in pgEdge ColdFront can fail at startup when
the datastore does not match its storage mode, and can report object storage
errors while it runs. The
[Storing Metrics in pgEdge ColdFront](../configuration/collector/coldfront-metrics.md)
page describes the prerequisites of ColdFront mode.

## Startup Errors

The collector checks the metrics storage mode before running any migration and
refuses to start when the datastore does not match the
`use_coldfront_for_metrics` setting. The collector logs each startup error
after the prefix `Failed to initialize datastore: failed to initialize schema:
`, so the message text appears within a longer log line. The following table
describes each startup error and the corrective step:

| Error Message Contains | Cause | Fix |
|------------------------|-------|-----|
| `use_coldfront_for_metrics is enabled but the coldfront extension is not installed in the datastore` | The option is enabled, but the datastore has no `coldfront.create_iceberg_table` function, so the `coldfront` extension is not installed. | Install ColdFront 1.0.0-beta3 or later and run `CREATE EXTENSION coldfront`, or disable the option. |
| `use_coldfront_for_metrics is enabled but the installed coldfront extension is too old` | The installed ColdFront build lacks `coldfront._partition_clause(jsonb,text[])` or `coldfront.ensure_attached()`; the message names the missing function. | Install ColdFront 1.0.0-beta3 or later, which provides both the `coldfront._partition_clause(jsonb,text[])` and `coldfront.ensure_attached()` functions; a development build must include pgEdge ColdFront pull request #92. |
| `use_coldfront_for_metrics is enabled but the datastore already stores metrics in PostgreSQL tables` | The datastore was created in standard mode. | Use a new datastore for ColdFront storage, or disable the option. |
| `use_coldfront_for_metrics is disabled but the datastore stores metrics in ColdFront views` | The datastore was created in ColdFront mode. | Enable the option, or use a new datastore for standard storage. |
| `failed to create the Iceberg namespace "metrics" for the ColdFront metrics tables` | The collector could not attach the Lakekeeper catalog or create the `metrics` namespace in the warehouse. | Check that the warehouse named in `coldfront.warehouse` is reachable from the datastore and writable, and that the ColdFront catalog settings are correct. |
| `the workbench_coldfront schema is owned by role` | Another role owns the helper schema that the collector writes metrics through. | Drop the `workbench_coldfront` schema, or make the collector's datastore role its owner. |
| `the workbench_coldfront schema holds functions owned by other roles` | Another role owns a function in the helper schema; the message lists each such function. | Drop the listed functions before starting the collector. |
| `failed to create the workbench_coldfront schema` | The collector's datastore role cannot create a schema in the datastore database. | Grant the role the `CREATE` privilege on the datastore database. |

The two mismatch errors name up to three of the affected metrics relations.

ColdFront reports `Namespace with name 'metrics' does not exist` when a metrics
table is created before its Iceberg namespace exists on a Lakekeeper catalog.
The collector creates the namespace before the first migration, so this error
means that the namespace step did not take effect. Check that the warehouse
named in `coldfront.warehouse` is reachable and writable, and then restart the
collector.

## Object Storage Errors

On a running ColdFront datastore, object-store input and output errors, slow
metrics reads, or PostgreSQL backends that pg_duckdb aborts, followed by a
crash restart of the datastore server, usually mean that snapshots and small
data files have accumulated in the metrics tables. Take the following steps:

- Schedule the ColdFront compactor with the `--expire-snapshots` option and a
  lowered `--expire-older-than` value for every metrics table, as the
  [Scheduling the ColdFront Compactor](../configuration/collector/coldfront-metrics.md#scheduling-the-coldfront-compactor)
  section describes.
- Reduce the number of monitored connections or the probe frequency when the
  snapshot count grows faster than the compactor expires snapshots.
- Check that the object storage is healthy, reachable from the datastore, and
  not rejecting or throttling requests.
- Lengthen the collection interval of high-frequency probes when the compactor
  cannot keep up.
