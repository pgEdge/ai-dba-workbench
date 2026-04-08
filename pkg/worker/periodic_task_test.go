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
	"sync/atomic"
	"testing"
	"time"
)

// contextKey is a private type for context keys to avoid staticcheck SA1029.
type contextKey string

func TestPeriodicTask_Basic(t *testing.T) {
	var count atomic.Int32

	task := NewPeriodicTask(50*time.Millisecond, func(ctx context.Context) {
		count.Add(1)
	})

	ctx := context.Background()
	task.Start(ctx)

	// Let it run a few times
	time.Sleep(175 * time.Millisecond)

	task.Stop()

	// Should have run at least 3 times (50ms, 100ms, 150ms)
	if got := count.Load(); got < 3 {
		t.Errorf("Expected at least 3 executions, got %d", got)
	}
}

func TestPeriodicTask_RunOnStart(t *testing.T) {
	var count atomic.Int32

	task := NewPeriodicTask(1*time.Hour, func(ctx context.Context) {
		count.Add(1)
	}, WithRunOnStart())

	ctx := context.Background()
	task.Start(ctx)

	// Give time for immediate execution
	time.Sleep(10 * time.Millisecond)

	task.Stop()

	// Should have run exactly once (immediately on start)
	if got := count.Load(); got != 1 {
		t.Errorf("Expected 1 execution with RunOnStart, got %d", got)
	}
}

func TestPeriodicTask_InitialDelay(t *testing.T) {
	var count atomic.Int32

	task := NewPeriodicTask(50*time.Millisecond, func(ctx context.Context) {
		count.Add(1)
	}, WithInitialDelay(100*time.Millisecond))

	ctx := context.Background()
	task.Start(ctx)

	// Check before initial delay completes
	time.Sleep(50 * time.Millisecond)
	if got := count.Load(); got != 0 {
		t.Errorf("Expected 0 executions during initial delay, got %d", got)
	}

	// Wait for initial delay + one interval
	time.Sleep(150 * time.Millisecond)

	task.Stop()

	// Should have run at least once after delay
	if got := count.Load(); got < 1 {
		t.Errorf("Expected at least 1 execution after initial delay, got %d", got)
	}
}

func TestPeriodicTask_RunOnStartWithInitialDelay(t *testing.T) {
	var execTimes []time.Time

	start := time.Now()
	task := NewPeriodicTask(50*time.Millisecond, func(ctx context.Context) {
		execTimes = append(execTimes, time.Now())
	}, WithRunOnStart(), WithInitialDelay(100*time.Millisecond))

	ctx := context.Background()
	task.Start(ctx)

	// Wait for immediate + delay + one interval
	time.Sleep(200 * time.Millisecond)

	task.Stop()

	if len(execTimes) < 2 {
		t.Errorf("Expected at least 2 executions, got %d", len(execTimes))
		return
	}

	// First execution should be immediate
	firstDelay := execTimes[0].Sub(start)
	if firstDelay > 20*time.Millisecond {
		t.Errorf("First execution should be immediate, but was delayed by %v", firstDelay)
	}

	// Second execution should be after initial delay
	secondDelay := execTimes[1].Sub(start)
	if secondDelay < 100*time.Millisecond {
		t.Errorf("Second execution should be after initial delay, but was at %v", secondDelay)
	}
}

func TestPeriodicTask_ContextCancellation(t *testing.T) {
	var count atomic.Int32

	task := NewPeriodicTask(50*time.Millisecond, func(ctx context.Context) {
		count.Add(1)
	})

	ctx, cancel := context.WithCancel(context.Background())
	task.Start(ctx)

	// Let it run a couple times
	time.Sleep(125 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait and check that it stopped
	countBefore := count.Load()
	time.Sleep(100 * time.Millisecond)
	countAfter := count.Load()

	if countAfter > countBefore+1 {
		t.Errorf("Task should have stopped after context cancellation, but count went from %d to %d", countBefore, countAfter)
	}

	task.Wait()
}

func TestPeriodicTask_TaskReceivesContext(t *testing.T) {
	var receivedCtx context.Context

	task := NewPeriodicTask(50*time.Millisecond, func(ctx context.Context) {
		receivedCtx = ctx
	}, WithRunOnStart())

	ctx := context.WithValue(context.Background(), contextKey("testkey"), "testvalue")
	task.Start(ctx)

	time.Sleep(10 * time.Millisecond)
	task.Stop()

	if receivedCtx == nil {
		t.Error("Task should have received context")
	}
	if receivedCtx.Value(contextKey("testkey")) != "testvalue" {
		t.Error("Task should have received context with values")
	}
}

func TestPeriodicTask_MultipleStartCalls(t *testing.T) {
	var count atomic.Int32

	task := NewPeriodicTask(10*time.Millisecond, func(ctx context.Context) {
		count.Add(1)
	})

	ctx := context.Background()

	// Call Start multiple times
	task.Start(ctx)
	task.Start(ctx)
	task.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	task.Stop()

	// Should not have created multiple goroutines
	// With 10ms interval and 50ms wait, we'd see ~5 runs from one goroutine
	// If multiple goroutines were created, we'd see ~15 runs
	if got := count.Load(); got > 7 {
		t.Errorf("Multiple starts may have created multiple goroutines, count is %d", got)
	}
}

func TestPeriodicTask_MultipleStopCalls(t *testing.T) {
	task := NewPeriodicTask(50*time.Millisecond, func(ctx context.Context) {})

	ctx := context.Background()
	task.Start(ctx)

	// Call Stop multiple times - should not panic
	task.Stop()
	task.Stop()
	task.Stop()
}

func TestPeriodicTask_DefaultInterval(t *testing.T) {
	// Zero interval should default to 1 minute
	task := NewPeriodicTask(0, func(ctx context.Context) {})

	if task.interval != time.Minute {
		t.Errorf("Expected default interval of 1 minute, got %v", task.interval)
	}
}

func TestPeriodicTask_StopDuringInitialDelay(t *testing.T) {
	var count atomic.Int32

	task := NewPeriodicTask(50*time.Millisecond, func(ctx context.Context) {
		count.Add(1)
	}, WithInitialDelay(1*time.Hour))

	ctx := context.Background()
	task.Start(ctx)

	// Stop during initial delay
	time.Sleep(10 * time.Millisecond)
	task.Stop()

	// Task should never have executed
	if got := count.Load(); got != 0 {
		t.Errorf("Expected 0 executions when stopped during initial delay, got %d", got)
	}
}

func TestPeriodicTask_Wait(t *testing.T) {
	var running atomic.Bool

	task := NewPeriodicTask(1*time.Hour, func(ctx context.Context) {
		running.Store(true)
		time.Sleep(50 * time.Millisecond)
		running.Store(false)
	}, WithRunOnStart())

	ctx := context.Background()
	task.Start(ctx)

	// Wait for task to start
	time.Sleep(10 * time.Millisecond)

	// Stop should wait for in-flight task
	task.Stop()
	task.Wait()

	// Task should have completed
	if running.Load() {
		t.Error("Task should have completed after Wait")
	}
}
