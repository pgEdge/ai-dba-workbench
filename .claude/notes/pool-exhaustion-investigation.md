# Connection Pool Exhaustion Investigation

The collector experienced repeated connection pool timeouts
across multiple monitored servers. This document records the
root causes and the fixes applied.

## Problem

The collector logged "timed out after 60 seconds while waiting
for a connection from the monitored connection pool" across
many servers and probes. Restarting the collector temporarily
resolved the issue; the timeouts recurred within 30 to 60
minutes.

## Architecture

The collector uses a layered connection management design for
monitored PostgreSQL servers.

Each monitored server receives a per-connection semaphore that
limits concurrent database connections. Each database on a
server gets its own `pgxpool` with `MaxConns=1`. The semaphore
acts as the real concurrency throttle; it gates access across
all pools for a given server.

Approximately 33 probes run independently per connection. Each
probe executes in its own goroutine. All probes for a server
share the same semaphore.

## Root Causes Found

The investigation identified two independent root causes.

### Semaphore Too Small for Probe Count

The `monitored_max_connections` configuration defaulted to 5.
This created a 5-slot semaphore for approximately 33 concurrent
probes. When multiple probes with the same interval fired
simultaneously, they overwhelmed the semaphore. Queued probes
would time out at 60 seconds. Once timeouts began cascading,
the collector appeared hung.

### Broken Pool Key Reverse-Mapping

Commit `fc56b30` changed database-scoped pool keys from
`-(conn.ID * 10000)` to FNV32a hashes. However, `SyncPools`,
`InvalidateChangedPools`, and `CheckConnectionUpdated` still
used `-(poolKey / 10000)` to reverse-map keys to connection
IDs. This mismatch prevented the collector from cleaning up or
invalidating database-specific pools. The affected code resides
in `collector/src/database/monitored_pool.go`.

## Fixes Applied

The team applied three categories of fixes to resolve the pool
exhaustion issue.

### Auto-Sized Semaphore

The semaphore size now derives from the actual probe count per
connection. The `MonitoredConnectionPoolManager` gained a
`SetMaxConnections(n)` method. The scheduler calculates the
maximum probes per connection after `loadConfigs` and calls
`SetMaxConnections` with that value.

The `monitored_max_connections` configuration field is
deprecated. The collector retains the field for YAML
compatibility and logs a warning when the field is set. The
semaphore now sizes to 33 slots; this matches the 33 active
probes.

The fix spans two files:

- `collector/src/database/monitored_pool.go`
- `collector/src/scheduler/scheduler.go`

### Pool Key Mapping

The pool manager now maintains a `poolKeyToConnID` map that
explicitly tracks pool key to connection ID relationships. The
manager populates the map when `GetConnectionForDatabase`
creates pools and cleans the map when pools are removed. This
explicit mapping replaced the broken arithmetic in `SyncPools`,
`InvalidateChangedPools`, and `CheckConnectionUpdated`.

The fix resides in
`collector/src/database/monitored_pool.go`.

### Debug Tracing

The collector now emits `[POOL DEBUG]` log entries for
semaphore acquire, release, and timeout events with fill
levels. The collector also emits `[PROBE DEBUG]` log entries
for probe start and completion events with durations.

In the following example, the `grep` command filters for debug
trace entries:

```bash
grep '[POOL DEBUG]\|[PROBE DEBUG]' collector.log
```

These traces should be removed or gated behind a debug flag
once the team is confident the issue is resolved.

## Key Files

The following files are central to the pool management system:

- `collector/src/database/monitored_pool.go` contains the pool
  manager, semaphores, and pool key mapping.
- `collector/src/scheduler/scheduler.go` contains probe
  scheduling and semaphore auto-sizing logic.
- `collector/src/config.go` contains pool configuration and the
  deprecated `monitored_max_connections` field.
- `collector/src/main.go` contains pool manager creation.

## Remaining Concerns

The debug tracing is verbose in production logs; the team
should gate the tracing behind a flag or remove the tracing
entirely.

Connection 17 (`ai-workbench.conx.page`) has a separate
authentication failure for the `postgres` user. This issue is
unrelated to pool exhaustion.

All 33 probes can now open simultaneous connections to each
monitored server. This is acceptable for the default PostgreSQL
`max_connections` value of 100; the team should monitor
connection counts on heavily probed servers.

## Verification

After deploying the fix, the team confirmed the following
results:

- The semaphores auto-sized to 33 slots as logged on startup.
- Zero timeout entries appeared in logs over sustained
  operation.
- All servers displayed as online in the web interface.
