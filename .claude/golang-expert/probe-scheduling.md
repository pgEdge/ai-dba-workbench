# Probe Scheduling: Startup Jitter and the Concurrency Caps

`collector/src/scheduler/scheduler.go` runs one goroutine per
(connection, probe) pair in `scheduleProbeForConnection`. Two
admission-control mechanisms govern when those goroutines actually run
a probe, and both exist because of issue #441, where a restart fired
every probe on every connection at once and OOMKilled the process,
which restarted it into the same state: a self-sustaining loop.

## Never execute immediately on a non-positive initial delay

`calculateInitialDelay` returns zero when there is no recorded last
collection time and on both of its error paths, and a negative value
for any probe already past due. After any restart longer than the
shortest interval, that is every probe.

The past-due path therefore waits out a per-probe jitter drawn
uniformly from `[0, min(probe interval, scheduler.startup_jitter_seconds))`
by `initialStartupJitter`, so a 30-second probe still starts within
30 seconds whilst an hourly probe starts within the configured
window. `startup_jitter_seconds` defaults to 60; zero disables the
jitter. The positive-delay path is deliberately left alone: the
recorded last collection time already spreads those out.

The jitter is drawn with `crypto/rand`, not `math/rand`: Codacy's
Opengrep `math-random-used` rule fires on the latter in production
code, and the draw happens once per probe at startup, so the stronger
source costs nothing measurable. A failed draw falls back to half the
window rather than to zero, since zero would put every probe back on
the same starting line.

## Realign the ticker after the first execution

The ticker is created before the initial wait, so a tick can come due
whilst the goroutine waits out its jitter and would then fire
immediately after the first execution, re-clustering everything that
the jitter just spread out. `restartTicker` stops and resets the
ticker after the first execution so the phase offset survives into
steady state. `TestRestartTicker_RealignsPhase` locks this in.

There is nothing to drain from the channel: since Go 1.23 a pending
tick is held inside the timer rather than buffered in `ticker.C`, so
`cap(ticker.C)` is 0 and a non-blocking receive always takes its
`default` branch. `Stop` followed by `Reset` is what discards the
pending tick, and the next tick then arrives a full interval later.

## Cap total probe concurrency

`executeProbeForConnection` takes a slot from the `probeSlots`
semaphore for its whole duration, so peak concurrency is bounded by
`scheduler.max_concurrent_probes` (default 8) rather than scaling with
connections multiplied by probes. Acquisition selects on
`ps.shutdownChan` and `ps.ctx.Done()` as well as the semaphore, and
prefers those when the scheduler is already stopping, so a backlog of
queued probes never delays `Stop`. A non-positive configured cap falls
back to `defaultMaxConcurrentProbes`, since a zero-capacity channel
would deadlock every probe.

Apply the cap to every execution, not just the startup burst: the
steady-state worst case is the same thundering herd once intervals
align.

## Cap what one connection may hold of that budget

A slot is held for the whole probe execution, which for a server that
accepts connections but answers slowly means the full
`pool.monitored_max_wait_seconds` timeout, 120 seconds by default.
Summed over the seeded probe set, one such server demands roughly
`sum(120 / interval)` = 17.7 slots against a default global cap of 8,
so it can own the entire semaphore and queue healthy connections
behind it; before the cap existed, a slow server could not delay a
healthy one at all. (A wholly unreachable server is far cheaper,
around 1.5 to 2 slots, because `connect_timeout=10` fails its connect
in ten seconds.)

`scheduler.max_concurrent_probes_per_connection` (default 2) is the
answer: each connection gets its own counting semaphore in
`ps.connSlots`, created lazily by `connSlotsFor` on first acquisition.
A ceiling above the global cap can never bind, so `NewProbeScheduler`
clamps it to the global cap; a non-positive value falls back to
`defaultMaxConcurrentProbesPerConnection`.

Three rules keep `acquireProbeSlot` honest, and the tests in
`scheduler_test.go` cover each:

- **Order.** The per-connection permit is always taken first and the
  global slot second, so no goroutine ever waits for a per-connection
  permit whilst holding a global slot, and the pair cannot deadlock.

- **Shutdown.** Every waiting point selects on `ps.shutdownChan` and
  `ps.ctx.Done()`, and both a ready permit and a ready shutdown being
  selectable at random means `stopping()` is rechecked after each
  acquisition; anything already held is released before returning
  false, so acquisition either holds both permits or neither.

- **Release.** Acquisition returns a release function closing over the
  channel it acquired from, and `executeProbeForConnection` defers it,
  so both permits come back on every exit path including the early
  `DeadlineExceeded` return. Closing over the channel rather than
  looking it up again matters because `loadConfigs` may prune the map
  entry whilst the probe is still in flight.

Per-connection state is pruned in `loadConfigs` by `removeConnSlots`,
in the same loop that drops `probesByConn` entries for connections
that are no longer monitored, so a long-running collector does not
leak an entry per connection ever monitored.

## Config plumbing

All three settings live in the `scheduler` section of
`collector/src/config.go` (`SchedulerConfig`), are validated alongside
the pool settings (both caps must be greater than zero, the jitter
non-negative), and reach the scheduler through the getters
`GetMaxConcurrentProbes`, `GetMaxConcurrentProbesPerConnection` and
`GetStartupJitterSeconds` on the `scheduler.Config` interface. Adding
a getter to that interface means updating `testConfig` in
`scheduler_test.go` and `schedulerConfig` in
`scheduler_integration_test.go`; the integration fake deliberately
leaves the jitter at zero so the existing timing-sensitive tests still
see a prompt first execution.

## Monitored pool cap counts open connections

`pool.max_connections_per_server` is enforced in
`collector/src/database/monitored_pool.go`, not in the scheduler. The
per-connection-ID semaphore in `MonitoredConnectionPoolManager` bounds
connections in use, but every (connection, database) pair has its own
pgxpool that keeps idle connections, so on its own it let a server
with N databases hold up to `max * (N+1)` connections (issue #539).
`acquireWithinCap` therefore runs under a per-connection mutex
(`connLocks`) and, whenever the target pool has no idle connection to
hand out, calls `evictIdleConnections`, which closes (via `Hijack`)
idle connections in the connection's other pools until idle plus held
slots stays within the semaphore capacity. The tests in
`monitored_pool_cap_test.go` check the total with `openConnections`
and against `pg_stat_activity`.

`InvalidateChangedPools` compares `hashConnectionParams` over the
sorted parameter map with `dbname` set to the connection's own
database, so every pool derived from one connection shares a hash.
Never hash `connstring.Build` output: it iterates a map, so its order
is random and each reload would re-dial every pool.
