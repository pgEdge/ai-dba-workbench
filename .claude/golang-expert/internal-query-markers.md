/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Internal Query Markers
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Marking Workbench-Internal SQL

The Top Queries panel reads `metrics.pg_stat_statements`, which holds
instance-wide activity for a monitored server. The Workbench's own
datastore usually lives on that same PostgreSQL instance, so its
overhead lands in the same view as the user's workload. Two markers
distinguish Workbench traffic, and the panel's "Hide monitoring
queries" toggle filters on both.

## The Two Markers

The collector wraps every read-only probe query that runs against a
monitored database with a synthetic column alias:

- `ai_dba_wb_probe`, injected by `WrapQuery` in
  `collector/src/probes/base.go`.

Everything the Workbench runs against its own datastore carries an
in-statement comment instead, because those statements are inserts,
deletes, DDL, and multi-column reads whose shape must not change:

- `ai_dba_wb_internal`, injected by `Tag` in `pkg/sqlmarker`.

The server filters on both in `excludeWorkbenchQueriesClause` in
`server/src/internal/api/perf_summary_handlers.go`. That clause is a
compile-time constant built from `sqlmarker.Marker` and a local copy of
the probe alias; the collector is a separate Go module, so the alias is
duplicated rather than imported, and the two must be kept in step.

The filter is deliberately per-statement. Excluding the whole datastore
database was rejected: users legitimately run other tools against that
database and expect to see them in the panel.

## Marker Placement Is Not Negotiable

PostgreSQL does not preserve a comment in every position when it
normalises a statement for `pg_stat_statements`. Measured on
PostgreSQL 18:

| Placement                          | Survives? |
|------------------------------------|-----------|
| `/* m */ INSERT INTO t ...`        | No        |
| `INSERT /* m */ INTO t ...`        | Yes       |
| `INSERT INTO t VALUES (1) /* m */` | Yes       |
| `INSERT INTO t VALUES (1); -- m`   | No        |

`sqlmarker.Tag` therefore inserts the comment immediately after the
leading keyword token. Never "tidy" the marker to the front of the
string; it will be stripped and the filter will silently stop working.
`Tag` is idempotent, tolerates leading whitespace and newlines, and
returns its input unchanged when there is no leading alphabetic
keyword (a parenthesised sub-select, or SQL already prefixed with a
comment).

## Where the Tagging Lives

Tag at chokepoints, never by hand-editing individual SQL literals. The
current chokepoints are:

- `buildMetricsInsert` in `collector/src/probes/storage.go`, through
  which every probe's bulk metric writes pass.
- `HasDataChanged` in `collector/src/probes/change_tracking.go`, which
  tags the caller-supplied stored-snapshot query.
- The SQL builders in `collector/src/probes/partition.go`
  (`partitionExistsQuery`, `createPartitionSQL`, `dropPartitionSQL`,
  `protectedPartitionsQuery`, `partitionCandidatesQuery`).
- `lastCollectionTimeQuery` in `collector/src/probes/config_loader.go`.
- `probeAvailabilityUpsert` in
  `collector/src/database/probe_availability.go`.
- The connections statements in `collector/src/database/datastore.go`
  (`monitoredConnectionsQuery`, `monitoredConnectionByIDQuery`,
  `setConnectionErrorStatement`).
- `databaseListQuery` in `collector/src/scheduler/scheduler.go`.
- `queryInternal` in `alerter/src/internal/database/metric_queries.go`,
  through which every `metricRegistry` statement is executed; the
  registry holds over a hundred SQL literals and tagging them
  individually would guarantee the next addition went untagged.

Probe queries that run against monitored databases must not be tagged
with `ai_dba_wb_internal`; they already carry `ai_dba_wb_probe`.

## Still Untagged

The server's own traffic against the datastore (sessions, RBAC,
conversations, timeline, and the API handlers generally) is the same
class of noise and is not yet tagged. It needs a sweep across many
handler-level chokepoints and is tracked separately from issue #364.
The collector's `probe_configs` resolution path
(`LoadProbeConfigs` and `EnsureProbeConfig`) is also untagged.

## Testing Notes for pg_stat_statements

Writing an integration test that reads a statement back out of
`pg_stat_statements` is the only real proof that a marker survives. Two
traps make naive tests useless:

- `queryid` is computed from the parse tree, which ignores both
  comments and column aliases, and the view keeps the text of whichever
  form of a statement it saw first. A statement that differs from an
  existing entry only by a comment or an alias therefore collapses onto
  that entry and the new text is never stored.
- Literals are normalised into `$n` placeholders, so a unique string
  constant cannot be used to identify a statement either.

Use a uniquely named relation to give the statement its own `queryid`,
and match on that relation name. Do not call
`pg_stat_statements_reset()`; it discards the whole instance's history,
including whatever the developer was looking at.
