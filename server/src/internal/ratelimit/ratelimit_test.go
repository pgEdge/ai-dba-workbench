/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package ratelimit

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is a manually advanced clock for driving a Limiter.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
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

func newTestLimiter(t *testing.T, interval time.Duration, burst int) (*Limiter, *fakeClock) {
	t.Helper()
	l := New(interval, burst)
	if l == nil {
		t.Fatal("New returned nil for a positive interval and burst")
	}
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	l.now = clock.Now
	return l, clock
}

func TestNewDisabled(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		burst    int
	}{
		{"zero interval", 0, 5},
		{"negative interval", -time.Second, 5},
		{"zero burst", time.Second, 0},
		{"negative burst", time.Second, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if l := New(tt.interval, tt.burst); l != nil {
				t.Fatalf("New(%v, %d) = %v, want nil", tt.interval, tt.burst, l)
			}
		})
	}
}

func TestPerHour(t *testing.T) {
	if l := PerHour(0, 5); l != nil {
		t.Fatal("PerHour(0, 5) should disable the limit")
	}
	if l := PerHour(-3, 5); l != nil {
		t.Fatal("PerHour(-3, 5) should disable the limit")
	}
	l := PerHour(60, 5)
	if l == nil {
		t.Fatal("PerHour(60, 5) returned nil")
	}
	if l.interval != time.Minute {
		t.Fatalf("interval = %v, want 1m", l.interval)
	}
	if l.burst != 5 {
		t.Fatalf("burst = %v, want 5", l.burst)
	}
}

func TestNilLimiterAllowsEverything(t *testing.T) {
	var l *Limiter
	for i := 0; i < 100; i++ {
		ok, wait := l.Allow("alice")
		if !ok || wait != 0 {
			t.Fatalf("nil limiter denied call %d (wait %v)", i, wait)
		}
	}
}

func TestAllowBurstThenDeny(t *testing.T) {
	l, _ := newTestLimiter(t, 10*time.Second, 3)

	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("alice"); !ok {
			t.Fatalf("call %d within burst was denied", i)
		}
	}
	ok, wait := l.Allow("alice")
	if ok {
		t.Fatal("call beyond burst was allowed")
	}
	if wait != 10*time.Second {
		t.Fatalf("wait = %v, want 10s", wait)
	}
}

func TestAllowRefillsOverTime(t *testing.T) {
	l, clock := newTestLimiter(t, 10*time.Second, 2)

	l.Allow("alice")
	l.Allow("alice")
	if ok, _ := l.Allow("alice"); ok {
		t.Fatal("empty bucket allowed a call")
	}

	// Part of an interval earns part of a token; the wait shrinks and
	// is rounded up to a whole second.
	clock.Advance(4500 * time.Millisecond)
	ok, wait := l.Allow("alice")
	if ok {
		t.Fatal("bucket with less than one token allowed a call")
	}
	if wait != 6*time.Second {
		t.Fatalf("wait = %v, want 6s (5.5s rounded up)", wait)
	}

	clock.Advance(6 * time.Second)
	if ok, _ := l.Allow("alice"); !ok {
		t.Fatal("refilled token was not spent")
	}
	if ok, _ := l.Allow("alice"); ok {
		t.Fatal("second call after a single refill was allowed")
	}
}

func TestAllowCapsAtBurst(t *testing.T) {
	l, clock := newTestLimiter(t, time.Second, 2)

	l.Allow("alice")
	clock.Advance(time.Hour)

	for i := 0; i < 2; i++ {
		if ok, _ := l.Allow("alice"); !ok {
			t.Fatalf("call %d after a long idle period was denied", i)
		}
	}
	if ok, _ := l.Allow("alice"); ok {
		t.Fatal("idle time earned more than burst tokens")
	}
}

func TestAllowKeysAreIndependent(t *testing.T) {
	l, _ := newTestLimiter(t, time.Minute, 1)

	if ok, _ := l.Allow("alice"); !ok {
		t.Fatal("alice's first call was denied")
	}
	if ok, _ := l.Allow("alice"); ok {
		t.Fatal("alice's second call was allowed")
	}
	if ok, _ := l.Allow("bob"); !ok {
		t.Fatal("bob was limited by alice's usage")
	}
}

func TestAllowClockGoingBackwards(t *testing.T) {
	l, clock := newTestLimiter(t, time.Minute, 1)

	l.Allow("alice")
	clock.Advance(-time.Hour)
	if ok, _ := l.Allow("alice"); ok {
		t.Fatal("a backwards clock step earned tokens")
	}
}

func TestSweepDiscardsFullBuckets(t *testing.T) {
	l, clock := newTestLimiter(t, time.Second, 2)

	l.Allow("alice")
	l.Allow("bob")
	l.Allow("bob")
	if got := len(l.buckets); got != 2 {
		t.Fatalf("buckets = %d, want 2", got)
	}

	// Long enough for alice to refill but not bob, and past the sweep
	// interval so the next call sweeps.
	clock.Advance(sweepInterval)
	l.buckets["bob"].last = clock.Now()
	l.Allow("carol")

	if _, ok := l.buckets["alice"]; ok {
		t.Fatal("alice's full bucket was not discarded")
	}
	if _, ok := l.buckets["bob"]; !ok {
		t.Fatal("bob's partly spent bucket was discarded")
	}
	if _, ok := l.buckets["carol"]; !ok {
		t.Fatal("carol's new bucket is missing")
	}

	// Within the sweep interval nothing is discarded.
	clock.Advance(time.Second)
	l.buckets["alice"] = &bucket{tokens: 2, last: clock.Now()}
	l.Allow("carol")
	if _, ok := l.buckets["alice"]; !ok {
		t.Fatal("swept before the sweep interval elapsed")
	}
}

func TestAllowConcurrent(t *testing.T) {
	l := New(time.Hour, 50)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := l.Allow("alice"); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 50 {
		t.Fatalf("allowed = %d, want exactly the burst of 50", allowed)
	}
}
