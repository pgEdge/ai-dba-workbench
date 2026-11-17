/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// fakeConnectClock is a ConnectTiming clock that never waits: Sleep
// records the duration asked for and moves the clock on by it.
type fakeConnectClock struct {
	mu    sync.Mutex
	now   time.Time
	slept []time.Duration
}

func (f *fakeConnectClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeConnectClock) Sleep(_ context.Context, d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.slept = append(f.slept, d)
	f.now = f.now.Add(d)
}

func (f *fakeConnectClock) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func (f *fakeConnectClock) sleeps() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.slept...)
}

// installFakeConnectClock makes ConnectMonitored use the production
// timeout and floor with a fake clock until the test ends. Tests that
// call it must not run in parallel, since the timing is package state.
func installFakeConnectClock(t *testing.T) *fakeConnectClock {
	t.Helper()
	clock := &fakeConnectClock{now: time.Unix(1_700_000_000, 0)}
	restore := SetConnectTiming(ConnectTiming{
		Timeout: MonitoredConnectTimeout,
		Floor:   MonitoredConnectTimeout,
		Now:     clock.Now,
		Sleep:   clock.Sleep,
	})
	t.Cleanup(restore)
	return clock
}

func TestConnectMonitored(t *testing.T) {
	errRefused := errors.New("connection refused")

	tests := []struct {
		name      string
		elapsed   time.Duration // how long the connect takes on the fake clock
		err       error
		wantSleep []time.Duration
	}{
		{"success returns at once", time.Millisecond, nil, nil},
		{"fast failure is held to the floor", time.Millisecond, errRefused,
			[]time.Duration{MonitoredConnectTimeout - time.Millisecond}},
		{"failure at the deadline is not held further", MonitoredConnectTimeout,
			errRefused, nil},
		{"failure after the deadline is not held further", 2 * MonitoredConnectTimeout,
			errRefused, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clock := installFakeConnectClock(t)
			var deadline time.Time
			var hasDeadline bool
			err := ConnectMonitored(context.Background(), func(ctx context.Context) error {
				deadline, hasDeadline = ctx.Deadline()
				clock.advance(tc.elapsed)
				return tc.err
			})
			if !errors.Is(err, tc.err) || (tc.err == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if !hasDeadline {
				t.Fatal("connect ran without a deadline")
			}
			if left := time.Until(deadline); left <= 0 || left > MonitoredConnectTimeout {
				t.Errorf("connect deadline is %v away, want within %v",
					left, MonitoredConnectTimeout)
			}
			got := clock.sleeps()
			if len(got) != len(tc.wantSleep) {
				t.Fatalf("slept %v, want %v", got, tc.wantSleep)
			}
			for i := range got {
				if got[i] != tc.wantSleep[i] {
					t.Errorf("slept %v, want %v", got, tc.wantSleep)
				}
			}
		})
	}
}

// TestConnectMonitoredHoldUsesCallerContext checks that the hold is
// given the caller's context, not the connect deadline that may already
// have passed, so it ends when the caller stops waiting.
func TestConnectMonitoredHoldUsesCallerContext(t *testing.T) {
	type ctxKey struct{}
	parent := context.WithValue(context.Background(), ctxKey{}, "caller")
	var holdCtx context.Context
	restore := SetConnectTiming(ConnectTiming{
		Floor: MonitoredConnectTimeout,
		Now:   func() time.Time { return time.Unix(0, 0) },
		Sleep: func(ctx context.Context, _ time.Duration) { holdCtx = ctx },
	})
	defer restore()

	_ = ConnectMonitored(parent, func(context.Context) error { //nolint:errcheck // only the hold is under test
		return errors.New("refused")
	})
	if holdCtx != parent {
		t.Fatalf("hold ran under %v, want the caller's context", holdCtx)
	}
}

// TestGetClientForSessionHoldsFailedConnect checks that the MCP path
// to a monitored server, through GetClientForSession, holds a refused
// connect to the floor.
func TestGetClientForSessionHoldsFailedConnect(t *testing.T) {
	clock := installFakeConnectClock(t)
	cm := NewClientManager(nil)
	// Let the loopback address through, so the failure comes from the
	// dial itself rather than the always-blocked floor.
	cm.SetMonitoredDialGuard(func(*pgconn.Config) {})

	// Port 1 is tcpmux, which nothing on a test host listens on, so the
	// connect is refused at once.
	_, err := cm.GetClientForSession(context.Background(),
		&SessionInfo{TokenHash: "t", ConnectionID: 1},
		"postgres://u@127.0.0.1:1/x?sslmode=disable")
	if err == nil {
		t.Fatal("expected the connect to fail")
	}
	if got := clock.sleeps(); len(got) != 1 || got[0] != MonitoredConnectTimeout {
		t.Errorf("slept %v, want one hold of %v", got, MonitoredConnectTimeout)
	}
}

func TestSetConnectTimingDefaultsAndRestore(t *testing.T) {
	before := connectTiming.Load()
	restore := SetConnectTiming(ConnectTiming{})
	got := connectTiming.Load()
	if got.Timeout != MonitoredConnectTimeout || got.Floor != 0 ||
		got.Now == nil || got.Sleep == nil {
		t.Errorf("SetConnectTiming(zero) = %+v, want the default timeout and clock with no floor", got)
	}
	restore()
	if connectTiming.Load() != before {
		t.Error("restore did not reinstate the previous timing")
	}

	def := defaultConnectTiming()
	if def.Timeout != MonitoredConnectTimeout || def.Floor != MonitoredConnectTimeout {
		t.Errorf("default timing = %+v, want timeout and floor of %v", def, MonitoredConnectTimeout)
	}
}

func TestSleepContext(t *testing.T) {
	t.Parallel()

	t.Run("returns once the duration has passed", func(t *testing.T) {
		t.Parallel()
		sleepContext(context.Background(), time.Nanosecond)
	})

	t.Run("returns when the context ends", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan struct{})
		go func() {
			sleepContext(ctx, time.Hour)
			close(done)
		}()
		<-done
	})
}
