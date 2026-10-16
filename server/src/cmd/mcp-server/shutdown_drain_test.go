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
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/overview"
)

// freeLoopbackAddr returns a loopback address with a port that was
// free a moment ago, so that a test can reach a server RunHTTP binds.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find a free port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("failed to release the free port: %v", err)
	}
	return addr
}

// waitForHealth polls /health on addr until the server answers.
func waitForHealth(t *testing.T, addr string) {
	t.Helper()

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + addr + "/health")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server on %s did not answer within 5s", addr)
}

// recordDenials records the same audit.read denial n times through the
// store's denial coalescer. Unlike repeatAuditDenial it returns the
// error, so that an HTTP handler goroutine can call it.
func recordDenials(store *auth.AuthStore, n int) error {
	actor := auth.Actor{Type: auth.ActorUser, Name: "mallory",
		IP: "192.0.2.1"}
	for i := 0; i < n; i++ {
		if err := store.RecordCoalescedDenial(actor, "audit.read", "audit",
			"Permission denied: requires superuser privileges"); err != nil {
			return err
		}
	}
	return nil
}

// holdingServer starts serveHTTP for s on a free loopback port with a
// /hold handler that signals entered, waits for release, then runs
// after. The returned channel yields serveHTTP's result.
func holdingServer(t *testing.T, s *Server, h *shutdownHandle,
	entered chan<- struct{}, release <-chan struct{},
	after func()) (string, <-chan error) {

	t.Helper()

	addr := freeLoopbackAddr(t)
	cfg := &mcp.HTTPConfig{
		Addr: addr,
		SetupHandlers: func(mux *http.ServeMux) error {
			mux.HandleFunc("/hold", func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-release
				after()
				w.WriteHeader(http.StatusForbidden)
			})
			return nil
		},
	}

	served := make(chan error, 1)
	go func() {
		served <- s.serveHTTP(cfg, h)
	}()
	waitForHealth(t, addr)
	return addr, served
}

// testShutdownDeps returns handler dependencies that drain srv but
// never reach the fallback exit during a test: the fallback waits an
// hour, and its exit does nothing if it ever fires.
func testShutdownDeps(srv shutdowner, grace time.Duration) shutdownDeps {
	return shutdownDeps{
		server:       srv,
		logger:       io.Discard,
		exit:         func(int) {},
		grace:        grace,
		cleanupGrace: time.Hour,
	}
}

// TestServeHTTPWaitsForTheDrainBeforeTheAuditFlush reproduces issue
// #650: with a request held open mid-handler, a shutdown signal must
// not let the run path return, and so main run its deferred Close and
// the coalesced-audit flush, until the handler has finished. Five
// denials before the signal and three recorded by the handler once it
// resumes must all be counted by the flush that follows.
func TestServeHTTPWaitsForTheDrainBeforeTheAuditFlush(t *testing.T) {
	store, _ := newWrapperTestStore(t)
	mcpServer := mcp.NewServer(&shutdownTestToolStub{})
	s := &Server{authStore: store, mcpServer: mcpServer}
	h := installShutdownHandler(testShutdownDeps(mcpServer, 30*time.Second))
	t.Cleanup(func() { signalStop(h) })

	entered := make(chan struct{})
	release := make(chan struct{})
	recorded := make(chan error, 1)
	addr, served := holdingServer(t, s, h, entered, release, func() {
		recorded <- recordDenials(store, 3)
	})

	if err := recordDenials(store, 5); err != nil {
		t.Fatalf("failed to record a denial: %v", err)
	}

	responded := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get("http://" + addr + "/hold")
		if err == nil {
			err = resp.Body.Close()
		}
		responded <- err
	}()
	<-entered

	h.sigCh <- syscall.SIGTERM
	<-h.requested

	select {
	case err := <-served:
		t.Fatalf("serveHTTP returned %v whilst a request was still in flight", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	if err := <-recorded; err != nil {
		t.Fatalf("the held handler failed to record a denial: %v", err)
	}

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serveHTTP returned %v after a clean drain", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serveHTTP did not return within 10s of the drain")
	}
	select {
	case <-h.drained:
	default:
		t.Fatal("serveHTTP returned before the drain had finished")
	}
	if err := <-responded; err != nil {
		t.Errorf("the held request failed: %v", err)
	}

	// main's deferred Server.Close does this when Run returns.
	s.flushCoalescedAudit()

	if got := auditDenialRepeatCounts(t, store); len(got) != 2 ||
		got[0] != 0 || got[1] != 7 {
		t.Errorf("audit.read denial repeat counts = %v, want [0 7], "+
			"accounting for all 8 denials", got)
	}
}

// TestServeHTTPReturnsAfterTheGracePeriodWithAHungHandler checks that a
// handler which never finishes holds the run path for the grace period
// and no longer.
func TestServeHTTPReturnsAfterTheGracePeriodWithAHungHandler(t *testing.T) {
	mcpServer := mcp.NewServer(&shutdownTestToolStub{})
	s := &Server{mcpServer: mcpServer}
	const grace = 300 * time.Millisecond
	h := installShutdownHandler(testShutdownDeps(mcpServer, grace))
	t.Cleanup(func() { signalStop(h) })

	entered := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	addr, served := holdingServer(t, s, h, entered, release, func() {})

	go func() {
		client := &http.Client{Timeout: 30 * time.Second}
		if resp, err := client.Get("http://" + addr + "/hold"); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-entered

	start := time.Now()
	h.sigCh <- syscall.SIGTERM

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serveHTTP returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serveHTTP was still waiting 10s after the signal")
	}
	if elapsed := time.Since(start); elapsed < grace {
		t.Errorf("serveHTTP returned after %v, before the %v grace period", elapsed, grace)
	}
}

// signalStop unregisters h's channel from the process's signals, so
// that a real SIGTERM after the test does not reach its handler.
func signalStop(h *shutdownHandle) {
	signal.Stop(h.sigCh)
}

// TestWaitForDrainReturnsAtOnceWithoutASignal covers a server that
// stopped for some other reason, such as a listen error: there is no
// drain to wait for.
func TestWaitForDrainReturnsAtOnceWithoutASignal(t *testing.T) {
	h := newShutdownHandle()
	done := make(chan struct{})
	go func() {
		h.waitForDrain()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("waitForDrain blocked with no shutdown requested")
	}
}

// TestWaitForDrainBlocksUntilTheDrainFinishes checks that once a
// shutdown is requested, the wait lasts until drained is closed.
func TestWaitForDrainBlocksUntilTheDrainFinishes(t *testing.T) {
	h := newShutdownHandle()
	close(h.requested)
	done := make(chan struct{})
	go func() {
		h.waitForDrain()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("waitForDrain returned before the drain finished")
	case <-time.After(100 * time.Millisecond):
	}
	close(h.drained)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("waitForDrain did not return once the drain finished")
	}
}

// blockingShutdowner is a shutdowner whose Shutdown waits for its
// context to end, as http.Server.Shutdown does with a hung handler.
type blockingShutdowner struct{}

func (blockingShutdowner) Shutdown(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// TestDrainHTTPStopsAtTheGracePeriod checks that a drain which never
// completes on its own ends at the grace period with a warning.
func TestDrainHTTPStopsAtTheGracePeriod(t *testing.T) {
	var logger bytes.Buffer
	d := applyShutdownDefaults(shutdownDeps{
		server: blockingShutdowner{},
		logger: &logger,
		grace:  50 * time.Millisecond,
	})

	drainHTTP(d, make(chan os.Signal))

	if !strings.Contains(logger.String(), context.DeadlineExceeded.Error()) {
		t.Errorf("expected a deadline warning; log was:\n%s", logger.String())
	}
}

// TestDrainHTTPAbandonsTheDrainOnAFurtherSignal checks that a second
// signal during the drain ends it at once rather than at the grace
// period.
func TestDrainHTTPAbandonsTheDrainOnAFurtherSignal(t *testing.T) {
	var logger bytes.Buffer
	d := applyShutdownDefaults(shutdownDeps{
		server: blockingShutdowner{},
		logger: &logger,
		grace:  time.Hour,
	})
	more := make(chan os.Signal, 1)
	more <- syscall.SIGINT

	done := make(chan struct{})
	go func() {
		drainHTTP(d, more)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a further signal did not end the drain")
	}

	out := logger.String()
	if !strings.Contains(out, "abandoning the drain") {
		t.Errorf("expected the abandon message; log was:\n%s", out)
	}
	if !strings.Contains(out, context.Canceled.Error()) {
		t.Errorf("expected a cancellation warning; log was:\n%s", out)
	}
}

// TestRunShutdownExitsOnAFurtherSignalAfterTheDrain checks that a
// signal after the drain skips the wait for main's cleanup, and that
// the fallback still writes the coalesced audit counts.
func TestRunShutdownExitsOnAFurtherSignalAfterTheDrain(t *testing.T) {
	var logger bytes.Buffer
	closerRan := false
	exitCode := -1
	h := newShutdownHandle()
	h.sigCh <- syscall.SIGTERM

	done := make(chan struct{})
	go func() {
		runShutdown(shutdownDeps{
			closer:       func() { closerRan = true },
			logger:       &logger,
			exit:         func(code int) { exitCode = code },
			cleanupGrace: time.Hour,
		}, h)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a further signal did not end the wait for cleanup")
	}

	if !closerRan {
		t.Error("the closer did not run")
	}
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	if !strings.Contains(logger.String(), "exiting without waiting for cleanup") {
		t.Errorf("expected the further-signal message; log was:\n%s", logger.String())
	}
}

// TestRunBoundedGivesUpOnAHungCleanup checks that a cleanup step which
// never returns cannot keep the process from exiting.
func TestRunBoundedGivesUpOnAHungCleanup(t *testing.T) {
	var logger bytes.Buffer
	hung := make(chan struct{})
	t.Cleanup(func() { close(hung) })

	runBounded(func() { <-hung }, 20*time.Millisecond, &logger)

	if !strings.Contains(logger.String(), "did not finish within 20ms") {
		t.Errorf("expected a timeout warning; log was:\n%s", logger.String())
	}

	// A nil step is skipped.
	runBounded(nil, time.Hour, &logger)
}

// TestRunBoundedWaitsForACleanupThatFinishes checks that a cleanup step
// which returns in time is waited for and logs nothing.
func TestRunBoundedWaitsForACleanupThatFinishes(t *testing.T) {
	var logger bytes.Buffer
	ran := false
	runBounded(func() { ran = true }, time.Hour, &logger)

	if !ran {
		t.Error("the cleanup step did not run")
	}
	if logger.Len() != 0 {
		t.Errorf("expected no log output; got:\n%s", logger.String())
	}
}

// TestOverviewStreamCloserWithoutAHub covers a server whose overview
// generator is not running: there are no streams to end.
func TestOverviewStreamCloserWithoutAHub(t *testing.T) {
	s := &Server{}
	if s.overviewStreamCloser() != nil {
		t.Error("overviewStreamCloser returned a function without a hub")
	}
}

// TestNewHTTPConfigInstallsTheOverviewStreamCloser checks the wiring
// Run serves: the configuration's shutdown hook ends every open
// overview stream, so that an open AI Overview panel cannot hold the
// drain to the grace period.
func TestNewHTTPConfigInstallsTheOverviewStreamCloser(t *testing.T) {
	cfg := &config.Config{}
	cfg.HTTP.Address = "127.0.0.1:0"
	s := &Server{cfg: cfg, overviewHub: overview.NewHub()}

	httpConfig := s.newHTTPConfig(config.NewReloadableConfig(cfg, "", config.CLIFlags{}))

	if httpConfig.Addr != cfg.HTTP.Address {
		t.Errorf("Addr = %q; want %q", httpConfig.Addr, cfg.HTTP.Address)
	}
	if httpConfig.SetupHandlers == nil {
		t.Error("newHTTPConfig did not set the handlers")
	}
	if httpConfig.OnShutdown == nil {
		t.Fatal("newHTTPConfig did not install a shutdown hook")
	}

	sub := s.overviewHub.Subscribe("")
	httpConfig.OnShutdown()

	select {
	case _, ok := <-sub.C:
		if ok {
			t.Error("the shutdown hook sent an update instead of ending the stream")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the shutdown hook did not end the open overview stream")
	}
}

// TestNewHTTPConfigWithoutAHub covers a server whose overview generator
// is not running: there are no streams to end, so there is no hook.
func TestNewHTTPConfigWithoutAHub(t *testing.T) {
	cfg := &config.Config{}
	s := &Server{cfg: cfg}

	httpConfig := s.newHTTPConfig(config.NewReloadableConfig(cfg, "", config.CLIFlags{}))

	if httpConfig.OnShutdown != nil {
		t.Error("newHTTPConfig installed a shutdown hook without a hub")
	}
}

// TestServeHTTPEndsOpenOverviewStreamsWhenTheDrainStarts checks that an
// open overview SSE stream, which returns only when its client leaves
// or its subscription closes, does not hold the shutdown to the grace
// period: the hook Run installs closes the subscription as soon as the
// drain starts.
func TestServeHTTPEndsOpenOverviewStreamsWhenTheDrainStarts(t *testing.T) {
	mcpServer := mcp.NewServer(&shutdownTestToolStub{})
	s := &Server{mcpServer: mcpServer, overviewHub: overview.NewHub()}
	const grace = 30 * time.Second
	h := installShutdownHandler(testShutdownDeps(mcpServer, grace))
	t.Cleanup(func() { signalStop(h) })

	subscribed := make(chan struct{})
	addr := freeLoopbackAddr(t)
	cfg := &mcp.HTTPConfig{
		Addr:       addr,
		OnShutdown: s.overviewStreamCloser(),
		SetupHandlers: func(mux *http.ServeMux) error {
			// Waits on its subscription as handleSSE does.
			mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
				sub := s.overviewHub.Subscribe("")
				defer s.overviewHub.Unsubscribe(sub)
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				close(subscribed)
				for {
					select {
					case <-r.Context().Done():
						return
					case _, ok := <-sub.C:
						if !ok {
							return
						}
					}
				}
			})
			return nil
		},
	}
	served := make(chan error, 1)
	go func() {
		served <- s.serveHTTP(cfg, h)
	}()
	waitForHealth(t, addr)

	client := &http.Client{Timeout: time.Minute}
	resp, err := client.Get("http://" + addr + "/stream")
	if err != nil {
		t.Fatalf("failed to open the stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	<-subscribed

	start := time.Now()
	h.sigCh <- syscall.SIGTERM

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serveHTTP returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serveHTTP was still waiting 10s after the signal")
	}
	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Errorf("shutdown with an open stream took %v; the stream "+
			"should have ended when the drain started", elapsed)
	}
	if n := s.overviewHub.Count(); n != 0 {
		t.Errorf("%d overview subscribers remain after the drain", n)
	}
}
