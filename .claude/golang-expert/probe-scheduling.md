# Probe Scheduling: Startup Jitter and the Concurrency Cap

`collector/src/scheduler/scheduler.go` runs one goroutine per
(connection, probe) pair in `scheduleProbeForConnection`. Two
admission-control rules govern when those goroutines actually run a
probe, and both exist because of issue #441, where a restart fired
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

The jitter uses `math/rand/v2` with a `//nolint:gosec` on the draw
itself, because the CI linter requires the directive on the reported
line. This is scheduling de-clustering, not a security decision, and
a cryptographic source would buy nothing.

## Realign the ticker after the first execution

The ticker is created before the initial wait, so a tick can come due
whilst the goroutine waits out its jitter and would then fire
immediately after the first execution, re-clustering everything that
the jitter just spread out. `restartTicker` stops, drains and resets
the ticker after the first execution so the phase offset survives into
steady state. `TestRestartTicker_RealignsPhase` locks this in.

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

## Config plumbing

Both settings live in the `scheduler` section of
`collector/src/config.go` (`SchedulerConfig`), are validated alongside
the pool settings, and reach the scheduler through the two getters
`GetMaxConcurrentProbes` and `GetStartupJitterSeconds` on the
`scheduler.Config` interface. Adding a getter to that interface means
updating `testConfig` in `scheduler_test.go` and `schedulerConfig` in
`scheduler_integration_test.go`; the integration fake deliberately
leaves the jitter at zero so the existing timing-sensitive tests still
see a prompt first execution.
