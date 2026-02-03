/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package worker

import (
	"context"
	"sync"
	"time"
)

// PeriodicTask runs a function at regular intervals in a background
// goroutine. It supports context-aware cancellation, optional initial
// delay, and optional immediate execution on start.
//
// Example usage:
//
//	task := worker.NewPeriodicTask(5*time.Minute, func(ctx context.Context) {
//	    // Perform periodic work
//	})
//	task.Start(ctx)
//	defer task.Stop()
type PeriodicTask struct {
	interval     time.Duration
	initialDelay time.Duration
	runOnStart   bool
	task         func(context.Context)
	stopChan     chan struct{}
	wg           sync.WaitGroup
	startOnce    sync.Once
	stopOnce     sync.Once
}

// PeriodicTaskOption configures a PeriodicTask.
type PeriodicTaskOption func(*PeriodicTask)

// WithInitialDelay sets an initial delay before the first task execution.
// If RunOnStart is also set, the immediate execution happens first, then
// the initial delay applies before starting the regular interval.
func WithInitialDelay(delay time.Duration) PeriodicTaskOption {
	return func(pt *PeriodicTask) {
		pt.initialDelay = delay
	}
}

// WithRunOnStart configures the task to run immediately when Start is
// called, before waiting for the first interval.
func WithRunOnStart() PeriodicTaskOption {
	return func(pt *PeriodicTask) {
		pt.runOnStart = true
	}
}

// NewPeriodicTask creates a new periodic task runner.
//
// Parameters:
//   - interval: Duration between task executions
//   - task: Function to execute periodically (receives context for cancellation)
//   - opts: Optional configuration (WithInitialDelay, WithRunOnStart)
//
// The task must be started with Start() to begin execution.
func NewPeriodicTask(interval time.Duration, task func(context.Context), opts ...PeriodicTaskOption) *PeriodicTask {
	if interval <= 0 {
		interval = time.Minute
	}
	pt := &PeriodicTask{
		interval: interval,
		task:     task,
		stopChan: make(chan struct{}),
	}

	for _, opt := range opts {
		opt(pt)
	}

	return pt
}

// Start begins the periodic task execution. The provided context is used
// for cancellation; when the context is cancelled, the task loop exits.
// It is safe to call multiple times; only the first call has any effect.
func (pt *PeriodicTask) Start(ctx context.Context) {
	pt.startOnce.Do(func() {
		pt.wg.Add(1)
		go pt.run(ctx)
	})
}

// run is the main loop for the periodic task.
func (pt *PeriodicTask) run(ctx context.Context) {
	defer pt.wg.Done()

	// Run immediately on start if configured
	if pt.runOnStart {
		pt.task(ctx)
	}

	// Handle initial delay if configured
	if pt.initialDelay > 0 {
		select {
		case <-pt.stopChan:
			return
		case <-ctx.Done():
			return
		case <-time.After(pt.initialDelay):
			// Initial delay complete
		}
	}

	ticker := time.NewTicker(pt.interval)
	defer ticker.Stop()

	for {
		select {
		case <-pt.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			pt.task(ctx)
		}
	}
}

// Stop gracefully stops the periodic task. It signals the task loop to
// exit and waits for any in-flight execution to complete. It is safe
// to call multiple times; only the first call has any effect.
func (pt *PeriodicTask) Stop() {
	pt.stopOnce.Do(func() {
		close(pt.stopChan)
		pt.wg.Wait()
	})
}

// Wait blocks until the task has stopped. This is useful when the task
// is controlled by context cancellation rather than explicit Stop() calls.
func (pt *PeriodicTask) Wait() {
	pt.wg.Wait()
}
