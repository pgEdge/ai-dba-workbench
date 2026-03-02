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

// DynamicPeriodicTask runs a function at regular intervals in a background
// goroutine, dynamically adjusting the interval by calling an IntervalFunc
// after each tick. This supports configuration hot-reload without restarting
// the worker.
//
// Example usage:
//
//	task := worker.NewDynamicPeriodicTask(
//	    func() time.Duration { return cfg.GetInterval() },
//	    func(ctx context.Context) { /* work */ },
//	    worker.WithDynamicRunOnStart(),
//	    worker.WithName("threshold-evaluator"),
//	)
//	task.Start(ctx)
//	defer task.Stop()
type DynamicPeriodicTask struct {
	intervalFunc func() time.Duration
	task         func(context.Context)
	name         string
	logFunc      func(string, ...any)
	runOnStart   bool
	stopChan     chan struct{}
	wg           sync.WaitGroup
	startOnce    sync.Once
	stopOnce     sync.Once
}

// DynamicPeriodicTaskOption configures a DynamicPeriodicTask.
type DynamicPeriodicTaskOption func(*DynamicPeriodicTask)

// WithDynamicRunOnStart configures the task to run immediately when Start
// is called, before waiting for the first interval.
func WithDynamicRunOnStart() DynamicPeriodicTaskOption {
	return func(dt *DynamicPeriodicTask) {
		dt.runOnStart = true
	}
}

// WithName sets a descriptive name for the task, used in log messages.
func WithName(name string) DynamicPeriodicTaskOption {
	return func(dt *DynamicPeriodicTask) {
		dt.name = name
	}
}

// WithLogFunc sets a logging function for the task. The function follows
// the fmt.Sprintf signature for formatted output.
func WithLogFunc(fn func(string, ...any)) DynamicPeriodicTaskOption {
	return func(dt *DynamicPeriodicTask) {
		dt.logFunc = fn
	}
}

// NewDynamicPeriodicTask creates a new dynamic periodic task runner.
//
// Parameters:
//   - intervalFunc: Function that returns the current desired interval;
//     called after each tick to detect configuration changes
//   - task: Function to execute periodically (receives context for cancellation)
//   - opts: Optional configuration (WithDynamicRunOnStart, WithName, WithLogFunc)
//
// The task must be started with Start() to begin execution.
func NewDynamicPeriodicTask(
	intervalFunc func() time.Duration,
	task func(context.Context),
	opts ...DynamicPeriodicTaskOption,
) *DynamicPeriodicTask {
	dt := &DynamicPeriodicTask{
		intervalFunc: intervalFunc,
		task:         task,
		stopChan:     make(chan struct{}),
	}

	for _, opt := range opts {
		opt(dt)
	}

	return dt
}

// Start begins the dynamic periodic task execution. The provided context
// is used for cancellation; when the context is cancelled, the task loop
// exits. It is safe to call multiple times; only the first call has any
// effect.
func (dt *DynamicPeriodicTask) Start(ctx context.Context) {
	dt.startOnce.Do(func() {
		dt.wg.Add(1)
		go dt.run(ctx)
	})
}

// run is the main loop for the dynamic periodic task.
func (dt *DynamicPeriodicTask) run(ctx context.Context) {
	defer dt.wg.Done()

	interval := dt.intervalFunc()
	if interval <= 0 {
		interval = time.Minute
	}

	dt.logf("%s started (interval: %v)", dt.label(), interval)

	// Run immediately on start if configured
	if dt.runOnStart {
		dt.task(ctx)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-dt.stopChan:
			dt.logf("%s stopping", dt.label())
			return
		case <-ctx.Done():
			dt.logf("%s stopping", dt.label())
			return
		case <-ticker.C:
			dt.task(ctx)

			newInterval := dt.intervalFunc()
			if newInterval <= 0 {
				newInterval = time.Minute
			}
			if newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
				dt.logf("%s interval updated to %v", dt.label(), interval)
			}
		}
	}
}

// Stop gracefully stops the dynamic periodic task. It signals the task
// loop to exit and waits for any in-flight execution to complete. It is
// safe to call multiple times; only the first call has any effect.
func (dt *DynamicPeriodicTask) Stop() {
	dt.stopOnce.Do(func() {
		close(dt.stopChan)
		dt.wg.Wait()
	})
}

// Wait blocks until the task has stopped. This is useful when the task
// is controlled by context cancellation rather than explicit Stop() calls.
func (dt *DynamicPeriodicTask) Wait() {
	dt.wg.Wait()
}

// label returns a descriptive name for log messages.
func (dt *DynamicPeriodicTask) label() string {
	if dt.name != "" {
		return dt.name
	}
	return "DynamicPeriodicTask"
}

// logf logs a formatted message if a log function is configured.
func (dt *DynamicPeriodicTask) logf(format string, args ...any) {
	if dt.logFunc != nil {
		dt.logFunc(format, args...)
	}
}
