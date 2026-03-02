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

func TestDynamicPeriodicTask_Basic(t *testing.T) {
	var count atomic.Int32

	task := NewDynamicPeriodicTask(
		func() time.Duration { return 50 * time.Millisecond },
		func(ctx context.Context) { count.Add(1) },
	)

	ctx := context.Background()
	task.Start(ctx)

	time.Sleep(175 * time.Millisecond)
	task.Stop()

	if got := count.Load(); got < 3 {
		t.Errorf("Expected at least 3 executions, got %d", got)
	}
}

func TestDynamicPeriodicTask_RunOnStart(t *testing.T) {
	var count atomic.Int32

	task := NewDynamicPeriodicTask(
		func() time.Duration { return 1 * time.Hour },
		func(ctx context.Context) { count.Add(1) },
		WithDynamicRunOnStart(),
	)

	ctx := context.Background()
	task.Start(ctx)

	time.Sleep(10 * time.Millisecond)
	task.Stop()

	if got := count.Load(); got != 1 {
		t.Errorf("Expected 1 execution with RunOnStart, got %d", got)
	}
}

func TestDynamicPeriodicTask_IntervalChange(t *testing.T) {
	var count atomic.Int32
	var interval atomic.Int64
	interval.Store(int64(50 * time.Millisecond))

	task := NewDynamicPeriodicTask(
		func() time.Duration { return time.Duration(interval.Load()) },
		func(ctx context.Context) { count.Add(1) },
	)

	ctx := context.Background()
	task.Start(ctx)

	// Let it run a few ticks at 50ms
	time.Sleep(125 * time.Millisecond)

	// Change interval to 1 hour (effectively stopping regular ticks)
	interval.Store(int64(1 * time.Hour))

	// Wait for the next tick to pick up the change
	time.Sleep(100 * time.Millisecond)

	countAfterChange := count.Load()

	// Wait and verify no more ticks
	time.Sleep(100 * time.Millisecond)

	task.Stop()

	countFinal := count.Load()

	// After interval change, at most one more tick should have occurred
	if countFinal > countAfterChange+1 {
		t.Errorf("Expected at most %d executions after interval change, got %d",
			countAfterChange+1, countFinal)
	}
}

func TestDynamicPeriodicTask_ContextCancellation(t *testing.T) {
	var count atomic.Int32

	task := NewDynamicPeriodicTask(
		func() time.Duration { return 50 * time.Millisecond },
		func(ctx context.Context) { count.Add(1) },
	)

	ctx, cancel := context.WithCancel(context.Background())
	task.Start(ctx)

	time.Sleep(125 * time.Millisecond)
	cancel()

	countBefore := count.Load()
	time.Sleep(100 * time.Millisecond)
	countAfter := count.Load()

	if countAfter > countBefore+1 {
		t.Errorf("Task should have stopped after context cancellation, count went from %d to %d",
			countBefore, countAfter)
	}

	task.Wait()
}

func TestDynamicPeriodicTask_WithName(t *testing.T) {
	var logged []string

	task := NewDynamicPeriodicTask(
		func() time.Duration { return 1 * time.Hour },
		func(ctx context.Context) {},
		WithName("test-worker"),
		WithLogFunc(func(format string, args ...any) {
			logged = append(logged, format)
		}),
		WithDynamicRunOnStart(),
	)

	ctx := context.Background()
	task.Start(ctx)

	time.Sleep(10 * time.Millisecond)
	task.Stop()

	if len(logged) == 0 {
		t.Error("Expected log messages with name")
	}
}

func TestDynamicPeriodicTask_MultipleStartCalls(t *testing.T) {
	var count atomic.Int32

	task := NewDynamicPeriodicTask(
		func() time.Duration { return 10 * time.Millisecond },
		func(ctx context.Context) { count.Add(1) },
	)

	ctx := context.Background()
	task.Start(ctx)
	task.Start(ctx)
	task.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	task.Stop()

	if got := count.Load(); got > 7 {
		t.Errorf("Multiple starts may have created multiple goroutines, count is %d", got)
	}
}

func TestDynamicPeriodicTask_MultipleStopCalls(t *testing.T) {
	task := NewDynamicPeriodicTask(
		func() time.Duration { return 50 * time.Millisecond },
		func(ctx context.Context) {},
	)

	ctx := context.Background()
	task.Start(ctx)

	task.Stop()
	task.Stop()
	task.Stop()
}

func TestDynamicPeriodicTask_ZeroInterval(t *testing.T) {
	task := NewDynamicPeriodicTask(
		func() time.Duration { return 0 },
		func(ctx context.Context) {},
		WithDynamicRunOnStart(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	task.Start(ctx)

	time.Sleep(10 * time.Millisecond)
	cancel()
	task.Wait()
}
