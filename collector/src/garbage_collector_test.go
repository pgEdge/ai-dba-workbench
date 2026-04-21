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
// Start -> Stop path where the shutdownChan fires before the 5-minute
// startup delay elapses. This verifies the early-exit branch of run().
func TestGarbageCollector_StartShutdownBeforeFirstCollection(t *testing.T) {
	gc := NewGarbageCollector(nil)

	// Don't call Start via its public method because that would call
	// logger.Info (safe) but also spawn a goroutine that needs a
	// datastore. Instead spawn run() directly with a canceled context
	// and then close shutdownChan to exit via the first select.
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
