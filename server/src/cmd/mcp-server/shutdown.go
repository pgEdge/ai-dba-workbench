/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/coverage"
	"syscall"
	"time"
)

// shutdownGracePeriod bounds how long the SIGTERM handler will wait
// for in-flight HTTP requests to drain before forcing exit. The E2E
// stop-stack script gives the process 10 seconds before SIGKILL, so
// the in-process timeout has to comfortably fit inside that window
// while still leaving slack for the cleanup below.
const shutdownGracePeriod = 5 * time.Second

// shutdownCleanupGrace bounds how long the handler waits, once the
// drain has finished, for main to return through its deferred
// Server.Close before it flushes the coalesced audit counts and exits
// by itself, and then how long that flush may take. With
// shutdownGracePeriod that comes to nine seconds at most, inside the
// E2E script's ten.
const shutdownCleanupGrace = 2 * time.Second

// shutdowner is the minimal subset of *mcp.Server the signal handler
// needs. Defining it as an interface keeps installShutdownHandler
// unit-testable without spinning up a full HTTP server.
type shutdowner interface {
	Shutdown(ctx context.Context) error
}

// coverageWriter abstracts the runtime/coverage entry point so tests
// can exercise the flush plumbing without running under a -cover
// build. In production this is bound to runtime/coverage.WriteCountersDir.
type coverageWriter func(dir string) error

// shutdownDeps groups the small set of dependencies the SIGTERM
// handler relies on. Tests substitute fakes; main wires in the real
// ones.
type shutdownDeps struct {
	server       shutdowner     // HTTP server to drain; may be nil for unit tests
	closer       func()         // Fallback cleanup if main's own cleanup overruns
	gocoverdir   string         // Value of $GOCOVERDIR; empty disables the flush
	writeCounts  coverageWriter // Coverage flush; ErrNotCovered if not instrumented
	logger       io.Writer      // Where to log progress; defaults to os.Stderr
	exit         func(int)      // Process exit; defaults to os.Exit
	grace        time.Duration  // Drain timeout; defaults to shutdownGracePeriod
	cleanupGrace time.Duration  // Fallback delay; defaults to shutdownCleanupGrace
}

// shutdownHandle connects the signal handler to the run path, which
// must not return, and so let main run its deferred cleanup, until the
// HTTP drain has finished: a handler still in flight can record an
// audit event, and main's cleanup writes the coalesced audit counts
// for the last time.
type shutdownHandle struct {
	sigCh     chan os.Signal
	requested chan struct{} // Closed on the first signal, before the drain
	drained   chan struct{} // Closed once the drain has finished or timed out
}

// newShutdownHandle returns a handle whose signal channel has room
// for one pending signal, as signal.Notify requires.
func newShutdownHandle() *shutdownHandle {
	return &shutdownHandle{
		sigCh:     make(chan os.Signal, 1),
		requested: make(chan struct{}),
		drained:   make(chan struct{}),
	}
}

// waitForDrain blocks until the HTTP drain has finished if a shutdown
// signal has arrived, and returns at once otherwise. The run path calls
// it after the HTTP server stops serving, which it does as soon as the
// drain starts; requested is closed before Shutdown is called, so a
// server stopped by the handler always waits here. The wait is bounded
// by the drain's grace period, which a further signal cuts short.
func (h *shutdownHandle) waitForDrain() {
	select {
	case <-h.requested:
		<-h.drained
	default:
	}
}

// applyShutdownDefaults fills in the standard production fallbacks
// for any zero-valued fields in d. It is extracted from runShutdown
// so the default-resolution logic can be unit-tested without invoking
// os.Exit, which would terminate the test process.
func applyShutdownDefaults(d shutdownDeps) shutdownDeps {
	if d.logger == nil {
		d.logger = os.Stderr
	}
	if d.exit == nil {
		d.exit = os.Exit
	}
	if d.grace <= 0 {
		d.grace = shutdownGracePeriod
	}
	if d.cleanupGrace <= 0 {
		d.cleanupGrace = shutdownCleanupGrace
	}
	return d
}

// runShutdown executes the graceful-shutdown sequence once the first
// signal has arrived: drain the HTTP server, then release the run path
// so that main returns through its deferred Server.Close, which writes
// the coalesced audit counts after the last in-flight handler has
// finished. If main has not returned within cleanupGrace, or a further
// signal arrives, it runs the caller's closer (itself bounded by
// cleanupGrace), flushes coverage counters when the binary was built
// with -cover, and exits with status 0.
//
// It is split out from installShutdownHandler so tests can drive the
// sequence directly with fakes for every dependency.
func runShutdown(d shutdownDeps, h *shutdownHandle) {
	d = applyShutdownDefaults(d)
	logger := d.logger

	close(h.requested)
	fmt.Fprintf(logger, "Received shutdown signal, draining HTTP connections...\n")
	drainHTTP(d, h.sigCh)
	close(h.drained)

	timer := time.NewTimer(d.cleanupGrace)
	defer timer.Stop()
	select {
	case <-timer.C:
		fmt.Fprintf(logger, "WARNING: server cleanup did not finish within %s of the drain; exiting\n",
			d.cleanupGrace)
	case <-h.sigCh:
		fmt.Fprintf(logger, "Received another shutdown signal; exiting without waiting for cleanup\n")
	}

	runBounded(d.closer, d.cleanupGrace, logger)
	flushCoverage(d.gocoverdir, d.writeCounts, logger)
	d.exit(0)
}

// drainHTTP calls Shutdown on d.server, bounded by d.grace. A further
// signal on more ends the wait early; Shutdown has already closed the
// listeners by then, so the handlers still running are abandoned.
func drainHTTP(d shutdownDeps, more <-chan os.Signal) {
	if d.server == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), d.grace)
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-more:
			fmt.Fprintf(d.logger, "Received another shutdown signal; abandoning the drain\n")
			cancel()
		case <-ctx.Done():
		}
	}()

	err := d.server.Shutdown(ctx)
	cancel()
	<-watcherDone
	if err != nil {
		// Shutdown errors are informational: the process is about
		// to exit either way. Log so an operator can diagnose
		// stuck connections if SIGKILL ever follows.
		fmt.Fprintf(d.logger, "WARNING: HTTP shutdown returned: %v\n", err)
	}
}

// runBounded runs fn, if it is not nil, and waits for it for at most
// limit, so that a cleanup step stuck on a lock or a write cannot keep
// the process from exiting. On a timeout fn keeps running until the
// process exits.
func runBounded(fn func(), limit time.Duration, logger io.Writer) {
	if fn == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		fmt.Fprintf(logger, "WARNING: shutdown cleanup did not finish within %s; exiting\n", limit)
	}
}

// flushCoverage writes accumulated coverage counters to gocoverdir
// when the binary was built with `go build -cover`. A non-instrumented
// binary returns ErrNotCovered, which is logged at info level and
// otherwise ignored. An empty gocoverdir means the caller did not
// opt in to coverage collection; in that case the function is a
// no-op.
func flushCoverage(gocoverdir string, write coverageWriter, logger io.Writer) {
	if gocoverdir == "" {
		return
	}
	if write == nil {
		return
	}
	if err := write(gocoverdir); err != nil {
		// runtime/coverage returns this sentinel when the program was
		// built without -cover; that is the expected state for
		// production binaries, so treat it as informational rather
		// than a warning.
		if errors.Is(err, errNotCovered) || isNotCoveredErr(err) {
			fmt.Fprintf(logger, "Coverage flush: binary not built with -cover, skipping\n")
			return
		}
		fmt.Fprintf(logger, "WARNING: failed to flush coverage to %s: %v\n", gocoverdir, err)
		return
	}
	fmt.Fprintf(logger, "Coverage flush: wrote counters to %s\n", gocoverdir)
}

// errNotCovered is the exact sentinel the runtime/coverage package
// returns from WriteCountersDir on non-instrumented binaries. It is
// not exported by the runtime, so we match by string as a fallback
// in isNotCoveredErr; this typed value lets future Go releases
// surface a real sentinel through errors.Is without breaking us.
var errNotCovered = errors.New("coverage: not built with -cover")

// isNotCoveredErr reports whether err is the runtime/coverage
// "not built with -cover" condition. The runtime constructs its
// error with errors.New, so identity comparison is unreliable across
// Go versions; matching on the message is the documented contract.
func isNotCoveredErr(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "coverage: not built with -cover" ||
		err.Error() == "not built with -cover"
}

// installShutdownHandler registers a SIGTERM/SIGINT handler that
// performs a graceful shutdown, and returns the handle through which
// the run path waits for the drain; its signal channel lets tests
// synthesize a signal.
//
// The signal handler must only run in serve mode; CLI subcommands
// reach os.Exit through their own paths and the runtime's atexit
// hook already flushes counters for them.
func installShutdownHandler(d shutdownDeps) *shutdownHandle {
	h := newShutdownHandle()
	signal.Notify(h.sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-h.sigCh
		runShutdown(d, h)
	}()
	return h
}

// realCoverageWriter is the production binding of coverageWriter.
// It exists as a package-level var so tests can swap it; production
// code calls installShutdownHandler with this value.
var realCoverageWriter coverageWriter = coverage.WriteCountersDir
