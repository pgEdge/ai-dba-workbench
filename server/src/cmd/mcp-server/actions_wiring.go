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
	"os"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/actions"
	"github.com/pgedge/ai-workbench/server/internal/api"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/conversations"
)

// actionShutdownGrace bounds how long shutdown waits for running
// actions to record their outcome. It sits inside shutdownCleanupGrace's
// budget, so a stuck action cannot hold the process past its exit
// window.
const actionShutdownGrace = 1500 * time.Millisecond

// initActions builds the proposed action store, review service and
// executor once, hands the service to the propose_action tool and
// registers the executor's shutdown. It runs after the tool provider
// exists and before it serves a request, as SetActionProposer requires.
func (s *Server) initActions() {
	pool := s.datastore.GetPool()
	if pool == nil {
		return
	}
	cfg := s.cfg.Actions
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = config.DefaultActionsMaxAttempts
	}

	s.actionStore = actions.NewStore(pool)
	s.actionService = actions.NewService(s.actionStore, actions.StubReviewer{}, maxAttempts)
	s.actionExecutor = actions.NewExecutor(s.actionStore,
		api.NewActionPoolOpener(s.datastore),
		cfg.LockTimeoutDuration(), cfg.StatementTimeoutDuration(), 0)

	s.toolProvider.SetActionProposer(s.actionService)

	s.registerHandlerCloser(func() {
		ctx, cancel := context.WithTimeout(context.Background(), actionShutdownGrace)
		defer cancel()
		if err := s.actionExecutor.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: actions still running at shutdown: %v\n", err)
		}
	})
}

// markInterruptedActions marks every action left executing by a
// previous process as unknown. It runs from Run, after main has
// verified the schema, because the table only exists from then on.
func (s *Server) markInterruptedActions() {
	if s.actionStore == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	n, err := s.actionStore.MarkInterruptedUnknown(ctx)
	reportInterruptedActions(n, err)
}

// reportInterruptedActions logs the outcome of marking interrupted
// actions: a warning on failure, and a line only when some were marked.
func reportInterruptedActions(n int64, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: could not mark interrupted actions: %v\n", err)
		return
	}
	if n > 0 {
		fmt.Fprintf(os.Stderr, "Marked %d interrupted action(s) as unknown\n", n)
	}
}

// conversationOwnership returns the check the link endpoint uses to
// confirm a conversation belongs to the caller, or nil when
// conversations are not stored.
func conversationOwnership(store *conversations.Store) api.ConversationOwnership {
	if store == nil {
		return nil
	}
	return ownershipFrom(store.Get)
}

// ownershipFrom builds the ownership check from a conversation lookup
// that is scoped to the user, so a conversation owned by someone else
// reads as not found.
func ownershipFrom(get func(ctx context.Context, id, username string) (*conversations.Conversation, error)) api.ConversationOwnership {
	return func(ctx context.Context, id, username string) (bool, error) {
		_, err := get(ctx, id, username)
		if errors.Is(err, conversations.ErrNotFound) {
			return false, nil
		}
		return err == nil, err
	}
}

// newActionHandler builds the action endpoints from the shared
// store, service and executor, or returns nil when they are not
// available.
func newActionHandler(deps *HandlerDependencies, rbac *auth.RBACChecker) *api.ActionHandler {
	if deps.ActionStore == nil || deps.ActionService == nil ||
		deps.ActionExecutor == nil || deps.Datastore == nil || rbac == nil {
		return nil
	}
	limit := deps.Config.Actions.ContextSnapshotLimit
	if limit <= 0 {
		limit = config.DefaultActionsContextSnapshotLimit
	}
	return api.NewActionHandler(deps.ActionStore, deps.ActionService,
		deps.ActionExecutor, deps.Datastore, rbac,
		conversationOwnership(deps.ConvStore), limit)
}
