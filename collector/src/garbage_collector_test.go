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
	"time"
)

func TestGetProbeTableName(t *testing.T) {
	tests := []struct {
		name      string
		probeName string
		want      string
	}{
		{"pg_stat_activity", "pg_stat_activity", "pg_stat_activity"},
		{"pg_stat_all_tables", "pg_stat_all_tables", "pg_stat_all_tables"},
		{"pg_stat_statements", "pg_stat_statements", "pg_stat_statements"},
		{"default probe falls through", "pg_stat_wal", "pg_stat_wal"},
		{"unknown probe returns own name", "something_new", "something_new"},
		{"empty probe name", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getProbeTableName(tc.probeName)
			if got != tc.want {
				t.Errorf("getProbeTableName(%q) = %q, want %q", tc.probeName, got, tc.want)
			}
		})
	}
}

func TestNewGarbageCollector(t *testing.T) {
	gc := NewGarbageCollector(nil)
	if gc == nil {
		t.Fatal("NewGarbageCollector returned nil")
	}
	if gc.shutdownChan == nil {
		t.Error("shutdownChan should be initialized")
	}
	if gc.datastore != nil {
		t.Error("expected nil datastore (as passed)")
	}
}

// TestGarbageCollector_StopIdempotent verifies that Stop works when Start
// has not been called; the shutdownChan is already allocated in
// NewGarbageCollector, so closing it exits the (non-existent) goroutine
// waiter immediately.
func TestGarbageCollector_Stop_NoStart(t *testing.T) {
	gc := NewGarbageCollector(nil)

	done := make(chan struct{})
	go func() {
		gc.Stop()
		close(done)
	}()

	select {
	case <-done:
		// expected: Stop() returns promptly because no goroutines were added
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return in time when Start() had not been called")
	}
}

// TestGarbageCollector_StartShutdownBeforeFirstCollection exercises the
// run() method's early-exit path where the shutdownChan is closed before
// the 5-minute startup delay elapses. We call run() directly (rather than
// the public Start method) to avoid spawning background goroutines that
// require a real datastore connection.
func TestGarbageCollector_StartShutdownBeforeFirstCollection(t *testing.T) {
	gc := NewGarbageCollector(nil)

	// Spawn run() directly with a context we can cancel, and immediately
	// close shutdownChan to trigger the early-exit branch.
	gc.wg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go gc.run(ctx)

	// Closing shutdownChan should cause run() to return from its first
	// select without ever calling collectGarbage.
	close(gc.shutdownChan)

	done := make(chan struct{})
	go func() {
		gc.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run() did not exit when shutdownChan was closed")
	}
}

// TestComputeNextRunDelay covers the scheduling decision that issue #437
// turned on: retention must be timed from the last recorded completion,
// not from process start, so that a collector which restarts frequently
// still enforces it.
func TestComputeNextRunDelay(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	readErr := context.DeadlineExceeded
	gc := NewGarbageCollector(nil)

	tests := []struct {
		name              string
		lastRun           time.Time
		found             bool
		readErr           error
		lastAttemptFailed bool
		want              time.Duration
	}{
		{
			name:  "never run before is due now",
			found: false,
			want:  gcStartupGrace,
		},
		{
			name:    "unreadable timestamp is treated as due",
			readErr: readErr,
			want:    gcStartupGrace,
		},
		{
			name:              "a failed attempt backs off",
			lastRun:           now.Add(-30 * time.Hour),
			found:             true,
			lastAttemptFailed: true,
			want:              gcRetryDelay,
		},
		{
			name:    "recorded run older than the interval is due now",
			lastRun: now.Add(-30 * time.Hour),
			found:   true,
			want:    gcStartupGrace,
		},
		{
			name:    "recorded run exactly at the interval is due now",
			lastRun: now.Add(-gcInterval),
			found:   true,
			want:    gcStartupGrace,
		},
		{
			name:    "mid-cycle carries the remainder, not a fresh interval",
			lastRun: now.Add(-6 * time.Hour),
			found:   true,
			want:    gcInterval - 6*time.Hour,
		},
		{
			name:    "remainder inside the grace period collapses to the grace",
			lastRun: now.Add(-gcInterval).Add(gcStartupGrace / 2),
			found:   true,
			want:    gcStartupGrace,
		},
		{
			name:    "a run recorded in the future waits a full interval",
			lastRun: now.Add(time.Hour),
			found:   true,
			want:    gcInterval + time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gc.computeNextRunDelay(tt.lastRun, tt.found, tt.readErr,
				tt.lastAttemptFailed, now)
			if got != tt.want {
				t.Errorf("computeNextRunDelay() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestComputeNextRunDelay_RestartDoesNotResetTheCycle is the regression
// test for #437 proper: however many times the process restarts, the
// delay depends only on the recorded completion, so a collector that
// never survives the old five-minute delay still collects.
func TestComputeNextRunDelay_RestartDoesNotResetTheCycle(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	overdue := now.Add(-9 * 24 * time.Hour)
	gc := NewGarbageCollector(nil)

	for restart := 0; restart < 112; restart++ {
		got := gc.computeNextRunDelay(overdue, true, nil, false, now)
		if got != gcStartupGrace {
			t.Fatalf("restart %d: delay = %v, want %v",
				restart, got, gcStartupGrace)
		}
	}

	if gcStartupGrace >= 5*time.Minute {
		t.Errorf("startup grace %v is no better than the five-minute delay "+
			"that caused #437", gcStartupGrace)
	}
}

// TestGarbageCollector_NextRunDelayWithoutDatastore verifies that an
// unreadable last-run time schedules a prompt attempt rather than
// panicking or waiting out a full interval. Not dropping partitions is
// the failure that filled a disk in #437, so "due now" is the safe
// direction to fail in.
func TestGarbageCollector_NextRunDelayWithoutDatastore(t *testing.T) {
	gc := NewGarbageCollector(nil)

	if got := gc.nextRunDelay(context.Background(), false); got != gcStartupGrace {
		t.Errorf("nextRunDelay() = %v, want %v", got, gcStartupGrace)
	}
}

// TestGarbageCollector_LastRunWithoutDatastore verifies the nil-datastore
// guard reports an error instead of dereferencing it, since the read
// happens on the collection goroutine where a panic would be fatal.
func TestGarbageCollector_LastRunWithoutDatastore(t *testing.T) {
	gc := NewGarbageCollector(nil)

	at, found, err := gc.lastRun(context.Background())
	if err == nil {
		t.Fatal("lastRun() returned no error for a nil datastore")
	}
	if found {
		t.Error("found = true for a nil datastore")
	}
	if !at.IsZero() {
		t.Errorf("time = %v, want zero value", at)
	}
}

// TestGarbageCollector_CollectGarbageWithoutDatastore verifies that a
// failed pass reports an error, which is what makes run() back off
// rather than retry in a tight loop.
func TestGarbageCollector_CollectGarbageWithoutDatastore(t *testing.T) {
	gc := NewGarbageCollector(nil)

	if err := gc.collectGarbage(context.Background()); err == nil {
		t.Error("collectGarbage() returned no error for a nil datastore")
	}
}

// TestGarbageCollector_RunExitsOnContextCancel verifies the context
// branch of the shutdown check, complementing the shutdownChan case.
func TestGarbageCollector_RunExitsOnContextCancel(t *testing.T) {
	gc := NewGarbageCollector(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	gc.wg.Add(1)
	go gc.run(ctx)

	done := make(chan struct{})
	go func() {
		gc.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run() did not exit when the context was canceled")
	}
}
