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
// while still leaving slack for the coverage flush below.
const shutdownGracePeriod = 5 * time.Second

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
	server      shutdowner     // HTTP server to drain; may be nil for unit tests
	closer      func()         // Final cleanup (typically server.Close)
	gocoverdir  string         // Value of $GOCOVERDIR; empty disables the flush
	writeCounts coverageWriter // Coverage flush; ErrNotCovered if not instrumented
	logger      io.Writer      // Where to log progress; defaults to os.Stderr
	exit        func(int)      // Process exit; defaults to os.Exit
	grace       time.Duration  // Drain timeout; defaults to shutdownGracePeriod
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
	return d
}

// runShutdown executes the graceful-shutdown sequence: drain the HTTP
// server, run the caller's Close hook, flush coverage counters when
// the binary was built with -cover, then exit with status 0.
//
// It is split out from installShutdownHandler so tests can drive the
// sequence directly with fakes for every dependency.
func runShutdown(d shutdownDeps) {
	d = applyShutdownDefaults(d)
	logger := d.logger
	exit := d.exit
	grace := d.grace

	fmt.Fprintf(logger, "Received shutdown signal, draining HTTP connections...\n")

	if d.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()
		if err := d.server.Shutdown(ctx); err != nil {
			// Shutdown errors are informational: the process is about
			// to exit either way. Log so an operator can diagnose
			// stuck connections if SIGKILL ever follows.
			fmt.Fprintf(logger, "WARNING: HTTP shutdown returned: %v\n", err)
		}
	}

	if d.closer != nil {
		d.closer()
	}

	flushCoverage(d.gocoverdir, d.writeCounts, logger)

	exit(0)
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
// performs a graceful shutdown and, if the binary was built with
// -cover, flushes coverage counters before exit. It returns the
// notification channel so callers (typically tests) can synthesize
// a signal; production code can ignore the return value.
//
// The signal handler must only run in serve mode; CLI subcommands
// reach os.Exit through their own paths and the runtime's atexit
// hook already flushes counters for them.
func installShutdownHandler(d shutdownDeps) chan os.Signal {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		runShutdown(d)
	}()
	return sigCh
}

// realCoverageWriter is the production binding of coverageWriter.
// It exists as a package-level var so tests can swap it; production
// code calls installShutdownHandler with this value.
var realCoverageWriter coverageWriter = coverage.WriteCountersDir
