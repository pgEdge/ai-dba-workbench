/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Feature-Detection Cache
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Probe Feature Detection and Probe Availability

Probes decide which query shape to run by asking the monitored server
what it supports: does a view exist, does a column exist, is an
extension installed. Those answers are cached in `featureCache` in
`collector/src/probes/base.go`.

## The Cache Key Includes the Database

`featureCacheKey` carries three fields: the monitored connection's
name, the database the answer was obtained in, and the check name.
The database is part of the key because feature detection is not
server-wide. An extension can be installed in one database and absent
from another on the same server, which is exactly what happens on RDS,
and reusing one database's verdict elsewhere silently suppressed Top
Queries collection (issues #435 and #438).

`cachedCheck` takes the `*pgxpool.Conn` it is checking against and
derives the database from `conn.Conn().Config().Database`, so no extra
round trip is needed; each database gets its own pool, from
`MonitoredConnectionPoolManager.GetConnectionForDatabase`, and the
pool's connection string always names the database. Pass the same
connection the check itself queries; passing nil yields an empty
database name, which only test code does.

Nothing invalidates the cache: entries live for the lifetime of the
process. Anything that does start invalidating on pool recycling has
to clear every entry for the connection across all databases, not just
the connection's own.

## Absent Extension versus Present but Empty

`probes.ErrExtensionNotInstalled` is the explicit signal that a
required extension is missing in the database being probed. It is not
a failure: the database-scoped helpers
`executeProbeOnDefaultDatabase` and `executeProbeOnDatabase`, which
`executeProbeForAllDatabases` drives, along with
`executeProbeForServerWide`, all in
`collector/src/scheduler/scheduler.go`, log it at `Debugf` and record
the observation, whereas every other `Execute` error is logged at
`Errorf` so a probe that collects nothing is visible at default
verbosity.

The scheduler classifies each `Execute` outcome with
`classifyProbeResult` and merges the observations across databases
with `extensionStatus.merge`, where presence beats absence and absence
beats no observation at all. An extension is taken as present when a
call returns no error and a non-nil slice, empty or not.
`utils.ScanRowsToMaps` returns an empty, non-nil slice for zero rows
(nil only alongside an error), so a probe can return its result
directly; do not change that back, or every installed-but-quiet
extension reads as unknown (issue #612).

Every `ExtensionProbe` (`pg_stat_statements`, the ten `system_stats`
`pg_sys_*` probes and the two Spock probes) must return
`ErrExtensionNotInstalled` when its extension is absent and must never
return `(nil, nil)`. A new extension probe has to follow the same
contract.

`availabilityVerdict` in `scheduler.go` turns the merged status into
the `probe_availability` row: stored rows, a non-extension probe, or
`extensionPresent` record available; `extensionAbsent` records
`extension '<name>' not installed`; and `extensionUnknown`, which now
only means every execution failed (each logged at `Errorf`), records
`probe execution failed for extension '<name>'`.
Unit tests for the verdict and the mixed-database cases live in
`scheduler/probe_availability_verdict_test.go`.

## Database Enumeration

`databaseListQuery` in `collector/src/scheduler/scheduler.go` filters
on `has_database_privilege(current_user, datname, 'CONNECT')` as well
as `datallowconn` and `datistemplate`, so databases the monitoring
user cannot connect to are never probed. On RDS that keeps `rdsadmin`,
which `pg_hba.conf` refuses, out of the list instead of producing an
error per probe per cycle (issue #440).
