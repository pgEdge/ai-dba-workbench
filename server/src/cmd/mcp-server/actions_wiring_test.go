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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/actions"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/conversations"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/tools"
)

// lazyPool returns a pool that has not connected: pgxpool connects on
// first use, and the address is a reserved one that is never dialed by
// these tests.
func lazyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(),
		"postgresql://nobody@192.0.2.1:5432/none?connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestInitActionsBuildsSharedServices(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := &config.Config{}
	s := &Server{
		ctx:          ctx,
		cfg:          cfg,
		datastore:    database.NewTestDatastore(lazyPool(t)),
		toolProvider: tools.NewContextAwareProvider(nil, nil, nil, cfg, nil, nil, nil),
	}
	s.initActions()

	if s.actionStore == nil || s.actionService == nil || s.actionExecutor == nil {
		t.Fatal("initActions left a part unbuilt")
	}
	deps := s.handlerDependencies(nil, nil)
	if deps.ActionStore != s.actionStore || deps.ActionService != s.actionService ||
		deps.ActionExecutor != s.actionExecutor {
		t.Error("handlers are not given the shared instances")
	}

	// Running the closers shuts the executor down, which refuses new
	// work afterwards.
	s.runHandlerClosers()
	if err := s.actionExecutor.Start(ctx, 1); err == nil {
		t.Error("executor accepted work after shutdown")
	}
}

func TestInitActionsWithoutPool(t *testing.T) {
	s := &Server{cfg: &config.Config{}, datastore: database.NewTestDatastore(nil)}
	s.initActions()
	if s.actionStore != nil || s.actionService != nil || s.actionExecutor != nil {
		t.Error("services built without a datastore pool")
	}
}

func TestMarkInterruptedActions(t *testing.T) {
	// No store: nothing to do, and no panic.
	(&Server{ctx: context.Background()}).markInterruptedActions()

	// A store whose database cannot be reached logs a warning and
	// carries on.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &Server{ctx: ctx, actionStore: actions.NewStore(lazyPool(t))}
	captureStderr(t, s.markInterruptedActions)
}

func TestReportInterruptedActions(t *testing.T) {
	out := captureStderr(t, func() {
		reportInterruptedActions(0, nil)
		reportInterruptedActions(0, errors.New("boom"))
		reportInterruptedActions(2, nil)
	})
	if strings.Contains(out, "Marked 0") {
		t.Errorf("reported zero actions: %q", out)
	}
	if !strings.Contains(out, "could not mark interrupted actions: boom") {
		t.Errorf("no warning for the failure: %q", out)
	}
	if !strings.Contains(out, "Marked 2 interrupted action(s)") {
		t.Errorf("no count for the marked actions: %q", out)
	}
}

func TestNewActionHandlerNeedsEverything(t *testing.T) {
	pool := lazyPool(t)
	store := actions.NewStore(pool)
	svc := actions.NewService(store, actions.StubReviewer{}, 3)
	exec := actions.NewExecutor(store, nil, 0, 0, 0)
	rbac := auth.NewRBACCheckerForDatastore(nil, nil)

	full := func() *HandlerDependencies {
		return &HandlerDependencies{
			ActionStore: store, ActionService: svc, ActionExecutor: exec,
			Datastore: database.NewTestDatastore(pool), Config: &config.Config{},
		}
	}
	if newActionHandler(full(), rbac) == nil {
		t.Error("no handler with every dependency")
	}
	if newActionHandler(full(), nil) != nil {
		t.Error("handler built without an RBAC checker")
	}
	for name, mutate := range map[string]func(*HandlerDependencies){
		"store":     func(d *HandlerDependencies) { d.ActionStore = nil },
		"service":   func(d *HandlerDependencies) { d.ActionService = nil },
		"executor":  func(d *HandlerDependencies) { d.ActionExecutor = nil },
		"datastore": func(d *HandlerDependencies) { d.Datastore = nil },
	} {
		d := full()
		mutate(d)
		if newActionHandler(d, rbac) != nil {
			t.Errorf("handler built without the %s", name)
		}
	}
}

func TestConversationOwnership(t *testing.T) {
	if conversationOwnership(nil) != nil {
		t.Error("ownership check offered without a store")
	}
	if conversationOwnership(conversations.NewStore(lazyPool(t))) == nil {
		t.Error("no ownership check offered with a store")
	}

	boom := errors.New("boom")
	tests := []struct {
		name    string
		err     error
		want    bool
		wantErr error
	}{
		{"owned", nil, true, nil},
		{"not found", conversations.ErrNotFound, false, nil},
		{"lookup fails", boom, false, boom},
	}
	for _, tt := range tests {
		owns := ownershipFrom(func(context.Context, string, string) (*conversations.Conversation, error) {
			return nil, tt.err
		})
		got, err := owns(context.Background(), "c", "u")
		if got != tt.want || !errors.Is(err, tt.wantErr) {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", tt.name, got, err, tt.want, tt.wantErr)
		}
	}
}
