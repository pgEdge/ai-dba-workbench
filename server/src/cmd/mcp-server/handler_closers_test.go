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
	"net/http"
	"sync"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// TestRegisterHandlerCloser_RecordsClosers verifies that registered
// closers accumulate in order and that a nil closer is ignored rather
// than stored, since running a nil func would panic during shutdown.
func TestRegisterHandlerCloser_RecordsClosers(t *testing.T) {
	s := &Server{}

	s.registerHandlerCloser(nil)
	if len(s.handlerClosers) != 0 {
		t.Fatalf("nil closer should be ignored, got %d entries",
			len(s.handlerClosers))
	}

	s.registerHandlerCloser(func() {})
	s.registerHandlerCloser(func() {})
	if len(s.handlerClosers) != 2 {
		t.Errorf("expected 2 registered closers, got %d",
			len(s.handlerClosers))
	}
}

// TestRunHandlerClosers_RunsEachOnce verifies that every registered
// closer runs, and that the list is cleared afterwards so a second
// shutdown pass does not stop the same resource twice.
func TestRunHandlerClosers_RunsEachOnce(t *testing.T) {
	s := &Server{}

	var calls int
	s.registerHandlerCloser(func() { calls++ })
	s.registerHandlerCloser(func() { calls++ })

	s.runHandlerClosers()
	if calls != 2 {
		t.Errorf("expected both closers to run, got %d calls", calls)
	}
	if len(s.handlerClosers) != 0 {
		t.Errorf("expected closer list to be cleared, got %d entries",
			len(s.handlerClosers))
	}

	s.runHandlerClosers()
	if calls != 2 {
		t.Errorf("second run should be a no-op, got %d calls", calls)
	}
}

// TestRunHandlerClosers_NoClosers verifies that shutting down a server
// that never wired any handlers is safe, which is the case for the CLI
// subcommands that never start the HTTP listener.
func TestRunHandlerClosers_NoClosers(t *testing.T) {
	s := &Server{}
	s.runHandlerClosers()
	if len(s.handlerClosers) != 0 {
		t.Errorf("expected no closers, got %d", len(s.handlerClosers))
	}
}

// TestRegisterHandlerCloser_ConcurrentRegistration exercises the mutex
// guarding the closer list. SetupHandlers runs on the HTTP server's
// goroutine whilst Close may run from the signal handler, so the two
// must not race.
func TestRegisterHandlerCloser_ConcurrentRegistration(t *testing.T) {
	s := &Server{}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.registerHandlerCloser(func() {})
		}()
	}
	wg.Wait()

	if len(s.handlerClosers) != 50 {
		t.Errorf("expected 50 registered closers, got %d",
			len(s.handlerClosers))
	}
}

// TestSetupHandlers_RegistersAuthHandlerCloser verifies the actual
// defect this addresses: NewAuthHandler starts a cleanup goroutine for
// its internal login rate limiter, and SetupHandlers must hand that
// back to the server so shutdown stops it.
func TestSetupHandlers_RegistersAuthHandlerCloser(t *testing.T) {
	var registered []func()
	deps := &HandlerDependencies{
		// A zero-value Config is enough: setupLLMHandlers reads
		// cfg.LLM fields unconditionally and panics on a nil Config.
		Config:         &config.Config{},
		RegisterCloser: func(c func()) { registered = append(registered, c) },
	}

	if err := SetupHandlers(deps)(http.NewServeMux()); err != nil {
		t.Fatalf("SetupHandlers returned an error: %v", err)
	}

	if len(registered) == 0 {
		t.Fatal("SetupHandlers registered no closer for the auth handler")
	}

	// The registered closer must be callable without panicking; it
	// stops the auth handler's internal rate limiter.
	for _, c := range registered {
		c()
	}
}

// TestSetupHandlers_NilRegisterCloser verifies that route registration
// still succeeds when no closer sink is supplied, which keeps existing
// callers and tests that only exercise routing working unchanged.
func TestSetupHandlers_NilRegisterCloser(t *testing.T) {
	deps := &HandlerDependencies{Config: &config.Config{}}

	if err := SetupHandlers(deps)(http.NewServeMux()); err != nil {
		t.Fatalf("SetupHandlers returned an error: %v", err)
	}
}
