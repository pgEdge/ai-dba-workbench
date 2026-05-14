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
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// fakeShutdowner is a minimal shutdowner used by the shutdown tests.
// It records the call so a test can assert Shutdown actually ran, and
// optionally returns a configurable error so the warning branch is
// exercisable.
type fakeShutdowner struct {
	mu        sync.Mutex
	called    bool
	gotCtx    context.Context
	returnErr error
}

func (f *fakeShutdowner) Shutdown(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.gotCtx = ctx
	return f.returnErr
}

// TestRunShutdown_FullSequence verifies the happy path: HTTP server
// drains, closer runs, coverage flush succeeds, exit(0) is called.
func TestRunShutdown_FullSequence(t *testing.T) {
	srv := &fakeShutdowner{}
	closerCalls := int32(0)
	var writeArg string
	var writeErr error // nil = success
	var logger bytes.Buffer
	exitCode := -1

	runShutdown(shutdownDeps{
		server: srv,
		closer: func() {
			atomic.AddInt32(&closerCalls, 1)
		},
		gocoverdir: "/tmp/example-cov",
		writeCounts: func(dir string) error {
			writeArg = dir
			return writeErr
		},
		logger: &logger,
		exit: func(code int) {
			exitCode = code
		},
	})

	if !srv.called {
		t.Errorf("expected Shutdown to be called on the HTTP server")
	}
	if got := atomic.LoadInt32(&closerCalls); got != 1 {
		t.Errorf("closer call count = %d, want 1", got)
	}
	if writeArg != "/tmp/example-cov" {
		t.Errorf("coverage writer received %q, want %q", writeArg, "/tmp/example-cov")
	}
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	out := logger.String()
	if !strings.Contains(out, "draining HTTP connections") {
		t.Errorf("expected drain message; log was:\n%s", out)
	}
	if !strings.Contains(out, "wrote counters to /tmp/example-cov") {
		t.Errorf("expected success message; log was:\n%s", out)
	}
}

// TestRunShutdown_ShutdownErrorLogged verifies that a non-nil error
// from server.Shutdown is logged but does not block the rest of the
// sequence: closer still runs, coverage still flushes, exit(0) still
// fires.
func TestRunShutdown_ShutdownErrorLogged(t *testing.T) {
	srv := &fakeShutdowner{returnErr: errors.New("drain timeout")}
	closerRan := false
	var logger bytes.Buffer
	exitCode := -1

	runShutdown(shutdownDeps{
		server:      srv,
		closer:      func() { closerRan = true },
		gocoverdir:  "",
		writeCounts: nil,
		logger:      &logger,
		exit:        func(code int) { exitCode = code },
	})

	if !closerRan {
		t.Error("closer should still run after shutdown error")
	}
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	if !strings.Contains(logger.String(), "drain timeout") {
		t.Errorf("expected drain error in log; got:\n%s", logger.String())
	}
}

// TestRunShutdown_NilServer verifies that a nil server (e.g. in a
// CLI-only test rig) is handled without panicking and still triggers
// coverage flush and exit.
func TestRunShutdown_NilServer(t *testing.T) {
	exitCode := -1
	runShutdown(shutdownDeps{
		server: nil,
		exit:   func(code int) { exitCode = code },
	})
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
}

// TestRunShutdown_DefaultsApplied verifies that omitted dependencies
// fall back to their documented defaults. We swap exit for a fake
// (otherwise the test would terminate) but leave logger nil to
// confirm the os.Stderr default doesn't crash. Grace defaults are
// asserted via the context deadline propagated to Shutdown.
func TestRunShutdown_DefaultsApplied(t *testing.T) {
	srv := &fakeShutdowner{}
	exitCalled := false

	runShutdown(shutdownDeps{
		server: srv,
		// logger, grace deliberately left zero/nil so the helper
		// must apply its defaults.
		exit: func(int) { exitCalled = true },
	})

	if !exitCalled {
		t.Fatal("exit was never invoked")
	}
	srv.mu.Lock()
	gotCtx := srv.gotCtx
	srv.mu.Unlock()
	if gotCtx == nil {
		t.Fatal("Shutdown was not called with a context")
	}
	if deadline, ok := gotCtx.Deadline(); !ok {
		t.Error("expected default grace to install a context deadline")
	} else if time.Until(deadline) > shutdownGracePeriod+time.Second {
		t.Errorf("deadline %v exceeds shutdownGracePeriod=%v", time.Until(deadline), shutdownGracePeriod)
	}
}

// TestFlushCoverage_NoDir asserts that an empty GOCOVERDIR is a no-op
// and never calls the writer.
func TestFlushCoverage_NoDir(t *testing.T) {
	called := false
	flushCoverage("", func(string) error {
		called = true
		return nil
	}, &bytes.Buffer{})
	if called {
		t.Error("writer should not be called when gocoverdir is empty")
	}
}

// TestFlushCoverage_NilWriter asserts that a nil writer is a no-op
// even when GOCOVERDIR is set. This guards against test rigs that
// forget to wire writeCounts and would otherwise NPE.
func TestFlushCoverage_NilWriter(t *testing.T) {
	// Should not panic.
	flushCoverage("/tmp/whatever", nil, &bytes.Buffer{})
}

// TestFlushCoverage_NotCovered verifies that the runtime/coverage
// "not built with -cover" sentinel is downgraded from a warning to
// an info-level log so production binaries don't spam stderr.
func TestFlushCoverage_NotCovered(t *testing.T) {
	var logger bytes.Buffer
	flushCoverage("/tmp/cov", func(string) error {
		return errors.New("coverage: not built with -cover")
	}, &logger)
	if !strings.Contains(logger.String(), "binary not built with -cover") {
		t.Errorf("expected info-level skip message; got:\n%s", logger.String())
	}
	if strings.Contains(logger.String(), "WARNING") {
		t.Errorf("not-covered case should not log WARNING; got:\n%s", logger.String())
	}
}

// TestFlushCoverage_NotCovered_AltMessage covers the alternative
// error string variant of the not-covered sentinel. Different Go
// versions have shipped slightly different wording.
func TestFlushCoverage_NotCovered_AltMessage(t *testing.T) {
	var logger bytes.Buffer
	flushCoverage("/tmp/cov", func(string) error {
		return errors.New("not built with -cover")
	}, &logger)
	if !strings.Contains(logger.String(), "binary not built with -cover") {
		t.Errorf("expected info-level skip message; got:\n%s", logger.String())
	}
}

// TestFlushCoverage_GenericError verifies that any other error from
// the writer is surfaced as a WARNING so operators can investigate.
func TestFlushCoverage_GenericError(t *testing.T) {
	var logger bytes.Buffer
	flushCoverage("/tmp/cov", func(string) error {
		return errors.New("disk full")
	}, &logger)
	out := logger.String()
	if !strings.Contains(out, "WARNING") || !strings.Contains(out, "disk full") {
		t.Errorf("expected WARNING with error text; got:\n%s", out)
	}
}

// TestFlushCoverage_Success verifies the success branch logs the
// destination directory so operators can confirm the flush landed.
func TestFlushCoverage_Success(t *testing.T) {
	var logger bytes.Buffer
	dir := t.TempDir()
	flushCoverage(dir, func(d string) error {
		if d != dir {
			t.Errorf("writer got %q, want %q", d, dir)
		}
		return nil
	}, &logger)
	if !strings.Contains(logger.String(), "wrote counters to "+dir) {
		t.Errorf("expected success message; got:\n%s", logger.String())
	}
}

// TestIsNotCoveredErr covers the matcher's true and false branches.
func TestIsNotCoveredErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"canonical", errors.New("coverage: not built with -cover"), true},
		{"short form", errors.New("not built with -cover"), true},
		{"unrelated", errors.New("disk full"), false},
		{"sentinel via errors.New copy", errNotCovered, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNotCoveredErr(tc.err); got != tc.want {
				t.Errorf("isNotCoveredErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestInstallShutdownHandler_RunsOnSignal verifies that sending
// SIGTERM through the returned channel triggers the same shutdown
// sequence as a real OS signal would. We use a fake exit to capture
// the result instead of terminating the test process.
func TestInstallShutdownHandler_RunsOnSignal(t *testing.T) {
	srv := &fakeShutdowner{}
	exitCh := make(chan int, 1)

	sigCh := installShutdownHandler(shutdownDeps{
		server: srv,
		exit:   func(code int) { exitCh <- code },
		logger: &bytes.Buffer{},
	})

	sigCh <- syscall.SIGTERM

	select {
	case code := <-exitCh:
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown handler did not run within 2s of signal")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !srv.called {
		t.Error("server.Shutdown was not invoked")
	}
}

// TestApplyShutdownDefaults_FillsZeroFields verifies that the helper
// substitutes the production defaults for every unset field. This
// covers the os.Exit-binding branch that runShutdown itself cannot
// exercise without terminating the test process.
func TestApplyShutdownDefaults_FillsZeroFields(t *testing.T) {
	out := applyShutdownDefaults(shutdownDeps{})

	if out.logger == nil {
		t.Error("logger should default to non-nil")
	}
	if out.logger != os.Stderr {
		t.Errorf("logger default = %v, want os.Stderr", out.logger)
	}
	if out.exit == nil {
		t.Error("exit should default to non-nil")
	}
	if out.grace != shutdownGracePeriod {
		t.Errorf("grace default = %v, want %v", out.grace, shutdownGracePeriod)
	}
}

// TestApplyShutdownDefaults_PreservesNonZeroFields verifies that
// callers' explicit values are not overwritten. Tests that inject
// fakes rely on this contract.
func TestApplyShutdownDefaults_PreservesNonZeroFields(t *testing.T) {
	var buf bytes.Buffer
	called := false
	in := shutdownDeps{
		logger: &buf,
		exit:   func(int) { called = true },
		grace:  42 * time.Millisecond,
	}
	out := applyShutdownDefaults(in)

	if out.logger != &buf {
		t.Error("logger was overwritten despite being set")
	}
	out.exit(0)
	if !called {
		t.Error("exit was overwritten despite being set")
	}
	if out.grace != 42*time.Millisecond {
		t.Errorf("grace = %v, want 42ms", out.grace)
	}
}

// TestServer_SetupShutdownHandler_Registers exercises the Server
// helper that wires production dependencies into installShutdownHandler.
// We can't intercept the os.Exit triggered when the production handler
// receives a signal, so this test only asserts that the helper runs
// to completion without panicking. The deeper signal-round-trip
// behavior is covered by TestInstallShutdownHandler_RunsOnSignal.
func TestServer_SetupShutdownHandler_Registers(t *testing.T) {
	// Use a real mcp.Server fixture. NewServer requires a tool
	// provider; the empty stub used elsewhere in this package is
	// fine because the handler never actually calls into it for
	// this test.
	s := &Server{mcpServer: mcp.NewServer(&shutdownTestToolStub{})}

	// Save and restore the production coverage writer so any later
	// test sees the pristine binding.
	origWriter := realCoverageWriter
	defer func() { realCoverageWriter = origWriter }()
	realCoverageWriter = func(string) error { return nil }

	// The handler runs in a goroutine waiting on its sigCh. We never
	// send a signal, so the goroutine sits idle for the rest of the
	// test process — harmless because Go's test runner exits after
	// all tests pass.
	s.setupShutdownHandler()
}

// shutdownTestToolStub is a do-nothing tool provider used purely so
// mcp.NewServer can be constructed without bringing in heavyweight
// dependencies.
type shutdownTestToolStub struct{}

func (shutdownTestToolStub) List() []mcp.Tool { return nil }
func (shutdownTestToolStub) Execute(ctx context.Context, name string, args map[string]any) (mcp.ToolResponse, error) {
	return mcp.ToolResponse{}, nil
}

// TestRealCoverageWriter_PointsAtRuntime checks that the package
// variable is initialized. The runtime function itself is impossible
// to invoke meaningfully from a test (it depends on -cover at build
// time), but a nil writer would silently disable production coverage
// flushing, so a presence test is still worthwhile.
func TestRealCoverageWriter_PointsAtRuntime(t *testing.T) {
	if realCoverageWriter == nil {
		t.Fatal("realCoverageWriter is nil; production coverage flush would be skipped")
	}
	// Invoke it against a temp dir. On a non-cover test binary this
	// returns the sentinel error; on a -cover binary it succeeds and
	// writes counters. Either outcome is fine; what we're verifying
	// is that the function pointer is callable and doesn't panic.
	dir := t.TempDir()
	if err := realCoverageWriter(dir); err != nil {
		if !isNotCoveredErr(err) {
			t.Logf("realCoverageWriter returned %v (acceptable on -cover or non-cover builds)", err)
		}
	}
	// Best-effort cleanup of any counter files the writer may have
	// produced — keeps the temp dir tidy on -cover runs.
	matches, _ := filepath.Glob(filepath.Join(dir, "covcounters.*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}
