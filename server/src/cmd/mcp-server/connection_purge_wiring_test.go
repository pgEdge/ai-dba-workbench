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
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/database"
)

// TestConnectionPurgerWiring pins how Run starts the background purge of
// deleted connections (issue #669) and hands its nudge to the handlers.
func TestConnectionPurgerWiring(t *testing.T) {
	t.Run("no datastore starts no purger", func(t *testing.T) {
		s := &Server{ctx: context.Background()}
		s.startConnectionPurger()
		if s.connPurger != nil {
			t.Fatal("purger started without a datastore")
		}
		if s.connectionPurgeNudge() != nil {
			t.Fatal("nudge offered without a purger")
		}
		if s.handlerDependencies(nil, nil).ConnectionPurgeNudge != nil {
			t.Fatal("handlers given a nudge without a purger")
		}
	})

	t.Run("a datastore starts one purger and offers its nudge", func(t *testing.T) {
		// The context is canceled up front, so the purger's goroutine
		// returns at once without touching the datastore's nil pool.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		s := &Server{ctx: ctx, datastore: database.NewTestDatastore(nil)}

		s.startConnectionPurger()
		first := s.connPurger
		if first == nil {
			t.Fatal("no purger started")
		}
		s.startConnectionPurger()
		if s.connPurger != first {
			t.Fatal("a second start replaced the running purger")
		}

		nudge := s.handlerDependencies(nil, nil).ConnectionPurgeNudge
		if nudge == nil {
			t.Fatal("handlers given no nudge")
		}
		nudge()
		nudge()
	})
}
