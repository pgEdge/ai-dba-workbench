/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package oidc

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a settable clock for UsedStates. It is guarded by a mutex
// because the concurrency test reads it from many goroutines.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestUsedStatesAcceptsAStateOnce(t *testing.T) {
	used := NewUsedStates()

	if !used.MarkUsed("state-a") {
		t.Fatal("the first presentation of a state was refused")
	}
	if used.MarkUsed("state-a") {
		t.Fatal("the second presentation of a state was accepted")
	}
	if !used.MarkUsed("state-b") {
		t.Fatal("a different state was refused because of the first")
	}
	if got := used.Len(); got != 2 {
		t.Errorf("Len = %d, want 2", got)
	}
}

// TestUsedStatesRemembersAStateForAsLongAsItCanBeOpened pins the
// retention to the longest time OpenState can go on accepting a state:
// StateTTL after an IssuedAt that may itself be clockSkew ahead.
func TestUsedStatesRemembersAStateForAsLongAsItCanBeOpened(t *testing.T) {
	clock := newFakeClock()
	used := newUsedStates(10, clock.Now)

	used.MarkUsed("state-a")

	clock.Advance(StateTTL + clockSkew)
	if used.MarkUsed("state-a") {
		t.Fatal("a state was forgotten while OpenState could still accept it")
	}

	clock.Advance(time.Nanosecond)
	if !used.MarkUsed("state-b") {
		t.Fatal("an unrelated state was refused")
	}
	// state-a has now expired and been dropped; state-b is the only
	// entry left.
	if got := used.Len(); got != 1 {
		t.Errorf("Len = %d after expiry, want 1", got)
	}
}

func TestUsedStatesEvictsTheOldestEntryWhenFull(t *testing.T) {
	clock := newFakeClock()
	used := newUsedStates(3, clock.Now)

	for _, state := range []string{"a", "b", "c", "d"} {
		if !used.MarkUsed(state) {
			t.Fatalf("state %q was refused", state)
		}
		clock.Advance(time.Second)
	}

	if got := used.Len(); got != 3 {
		t.Fatalf("Len = %d, want the capacity of 3", got)
	}
	// "a" was evicted to make room for "d"; the others are still held.
	for _, state := range []string{"b", "c", "d"} {
		if used.MarkUsed(state) {
			t.Errorf("state %q was forgotten, but only the oldest should have been evicted", state)
		}
	}
	if !used.MarkUsed("a") {
		t.Error("the evicted state was still remembered")
	}
}

func TestNewUsedStatesRaisesACapacityBelowOne(t *testing.T) {
	used := newUsedStates(0, time.Now)

	if !used.MarkUsed("a") || !used.MarkUsed("b") {
		t.Fatal("a set with a raised capacity refused a fresh state")
	}
	if got := used.Len(); got != 1 {
		t.Errorf("Len = %d, want 1", got)
	}
}

// TestUsedStatesAcceptsEachStateExactlyOnceUnderConcurrency is the race
// the lock exists for: many goroutines presenting the same states at the
// same moment must between them accept each state exactly once. Run it
// with -race.
func TestUsedStatesAcceptsEachStateExactlyOnceUnderConcurrency(t *testing.T) {
	clock := newFakeClock()
	used := newUsedStates(1000, clock.Now)

	const states = 50
	const presenters = 16

	var accepted [states]atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range presenters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := range states {
				if used.MarkUsed(fmt.Sprintf("state-%d", i)) {
					accepted[i].Add(1)
				}
			}
		}()
	}
	close(start)
	wg.Wait()

	for i := range states {
		if got := accepted[i].Load(); got != 1 {
			t.Errorf("state-%d was accepted %d times, want exactly once", i, got)
		}
	}
}
