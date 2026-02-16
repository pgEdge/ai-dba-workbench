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

The `max_connections_per_server` configuration defaults to 3.
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

The `max_connections_per_server` configuration controls both
the per-server semaphore size and the pgxpool MaxConns.
The old `monitored_max_connections` field has been removed.

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
  `max_connections_per_server` field.
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

## Second Hang: Pool Manager Mutex Deadlock (2026-02-01)

### Symptoms

The collector hung again approximately 10 minutes after
restart. The SIGABRT goroutine dump revealed the following
critical state:

- 18+ goroutines blocked on `probesMutex.RLock()` at
  scheduler.go:327 for 27+ minutes
- 10 goroutines blocked on `m.mu.Lock()` (pool manager
  mutex) at monitored_pool.go:278 for 31 minutes
- 1 goroutine blocked on `puddle.Pool.Close()` →
  `WaitGroup.Wait` for 31 minutes
- The configReloadLoop goroutine was not visible; it was
  blocked inside `loadConfigs` which called `SyncPools` or
  `InvalidateChangedPools`

### Root Cause

The issue was a classic lock-ordering deadlock between the
pool manager mutex (`m.mu`) and pgx pool close operations:

1. `configReloadLoop` → `loadConfigs` takes
   `probesMutex.Lock()` then calls `SyncPools` or
   `InvalidateChangedPools`

2. `SyncPools` or `InvalidateChangedPools` takes
   `m.mu.Lock()` then calls `pool.Close()`

3. `pool.Close()` blocks on `WaitGroup.Wait` until all
   borrowed connections are returned

4. Probe goroutines holding borrowed connections call
   `CheckConnectionUpdated` → tries `m.mu.Lock()` →
   deadlocked

5. Those probes cannot return connections → `pool.Close()`
   never completes → `loadConfigs` never releases
   `probesMutex` → all other probes blocked

This circular wait creates an unbreakable deadlock where
the configuration reload and all probe operations stall
indefinitely.

### Fix

The fix collects pools to close while holding `m.mu`,
releases the lock, then closes pools outside the lock. This
breaks the deadlock cycle since `pool.Close()` no longer
runs under `m.mu`. The collector can now return borrowed
connections even while configuration reload operations
progress.

Apply this pattern to three methods in `monitored_pool.go`:

- `SyncPools`
- `InvalidateChangedPools`
- `CheckConnectionUpdated`

Specifically, each method should:

1. Build a list of pools to close while holding `m.mu`
2. Release `m.mu`
3. Close the collected pools outside the lock

### Files Modified

- `collector/src/database/monitored_pool.go` — `SyncPools`,
  `InvalidateChangedPools`, `CheckConnectionUpdated`

## Third Hang: RemovePool and Close Deadlock (2026-02-02)

### Symptoms

The collector hung again approximately 40 minutes after
restart on 2026-02-02. The goroutine dump revealed the
following critical state:

- 175 goroutines blocked on `sync.Mutex.Lock` at
  `CheckConnectionUpdated` (monitored_pool.go:278) for
  422 minutes
- 386 goroutines blocked on `sync.RWMutex.RLock` at
  `scheduleProbeForConnection` (scheduler.go:327) for
  407 minutes
- 1 goroutine (477) blocked on `sync.WaitGroup.Wait`
  inside `puddle.Pool.Close` for 426 minutes, called from
  `RemovePool` at monitored_pool.go:263

### Root Cause

Two methods were missed in the previous deadlock fix:
`RemovePool` and `Close`. Both called `pool.Close()`
while holding `m.mu.Lock()` via `defer m.mu.Unlock()`.
The deadlock cycle was identical to the second hang:

1. A probe goroutine calls `RemovePool` which takes
   `m.mu.Lock()` then calls `pool.Close()`

2. `pool.Close()` blocks on `WaitGroup.Wait` until all
   borrowed connections return

3. Other probe goroutines holding borrowed connections
   call `CheckConnectionUpdated` which tries
   `m.mu.Lock()` — deadlocked

4. Those probes cannot return connections, so
   `pool.Close()` never completes

### Fix

The fix applied the same pattern as the previous
deadlock resolution to both `RemovePool` and `Close`:

1. Collect pool references while holding `m.mu`
2. Delete map entries while holding `m.mu`
3. Release `m.mu`
4. Close collected pools outside the lock

### Files Modified

- `collector/src/database/monitored_pool.go` —
  `RemovePool`, `Close`

## Verification

After deploying the fix, the team confirmed the following
results:

- The semaphores auto-sized to 33 slots as logged on startup.
- Zero timeout entries appeared in logs over sustained
  operation.
- All servers displayed as online in the web interface.
