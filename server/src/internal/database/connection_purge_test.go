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
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// fakePurgeStore records the purges it is asked for and fails on
// request, so the purger's scheduling can be tested without a database.
type fakePurgeStore struct {
	mu       sync.Mutex
	ids      []int
	listErr  error
	failIDs  map[int]bool
	purged   []int
	batches  []int
	lists    int
	listedCh chan struct{}
	// failFirst makes the first failFirst purges fail, whatever the id.
	failFirst int
}

func (f *fakePurgeStore) ListDeletedConnectionIDs(context.Context) ([]int, error) {
	f.mu.Lock()
	f.lists++
	ids, err := slices.Clone(f.ids), f.listErr
	f.mu.Unlock()
	if f.listedCh != nil {
		select {
		case f.listedCh <- struct{}{}:
		default:
		}
	}
	return ids, err
}

func (f *fakePurgeStore) PurgeConnection(_ context.Context, id int, batchSize int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purged = append(f.purged, id)
	f.batches = append(f.batches, batchSize)
	if f.failIDs[id] || len(f.purged) <= f.failFirst {
		return fmt.Errorf("purge of %d failed", id)
	}
	return nil
}

func (f *fakePurgeStore) listCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists
}

// captureLog returns a logf that records formatted lines.
func captureLog() (func(string, ...any), func() []string) {
	var mu sync.Mutex
	var lines []string
	return func(format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			lines = append(lines, fmt.Sprintf(format, args...))
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(lines)
		}
}

func TestConnectionPurger_RunOnce(t *testing.T) {
	tests := []struct {
		name       string
		store      *fakePurgeStore
		wantPurged []int
		wantLogs   int
	}{
		{
			name:       "purges every marked connection",
			store:      &fakePurgeStore{ids: []int{3, 1}},
			wantPurged: []int{3, 1},
		},
		{
			name:       "a failure moves on to the next connection",
			store:      &fakePurgeStore{ids: []int{3, 1}, failIDs: map[int]bool{3: true}},
			wantPurged: []int{3, 1},
			wantLogs:   1,
		},
		{
			name:     "a list failure is logged",
			store:    &fakePurgeStore{listErr: errors.New("boom")},
			wantLogs: 1,
		},
		{
			name:  "nothing to purge",
			store: &fakePurgeStore{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logf, logs := captureLog()
			p := newConnectionPurger(tt.store, time.Hour, 7, logf)
			p.RunOnce(context.Background())

			if !slices.Equal(tt.store.purged, tt.wantPurged) {
				t.Errorf("purged = %v, want %v", tt.store.purged, tt.wantPurged)
			}
			for _, b := range tt.store.batches {
				if b != 7 {
					t.Errorf("batch size = %d, want 7", b)
				}
			}
			if got := len(logs()); got != tt.wantLogs {
				t.Errorf("logged %d lines (%v), want %d", got, logs(), tt.wantLogs)
			}
		})
	}
}

func TestConnectionPurger_RunOnceCanceled(t *testing.T) {
	store := &fakePurgeStore{ids: []int{1}}
	p := newConnectionPurger(store, time.Hour, 1, func(string, ...any) {})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.RunOnce(ctx)
	if store.lists != 0 || len(store.purged) != 0 {
		t.Fatalf("canceled RunOnce listed %d times and purged %v, want nothing",
			store.lists, store.purged)
	}
}

// cancelingStore cancels the purger's context during the list, so the
// per-connection loop sees the cancellation before purging anything.
type cancelingStore struct {
	fakePurgeStore
	cancel context.CancelFunc
}

func (c *cancelingStore) ListDeletedConnectionIDs(context.Context) ([]int, error) {
	c.cancel()
	return []int{1, 2}, nil
}

func TestConnectionPurger_RunOnceStopsWhenCanceledMidPass(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &cancelingStore{cancel: cancel}
	p := newConnectionPurger(store, time.Hour, 1, func(string, ...any) {})
	p.RunOnce(ctx)
	if len(store.purged) != 0 {
		t.Fatalf("purged %v after cancellation, want nothing", store.purged)
	}
}

// TestConnectionPurger_Run checks that Run makes a pass at once, another
// on each nudge, and returns when its context is canceled.
func TestConnectionPurger_Run(t *testing.T) {
	store := &fakePurgeStore{ids: []int{9}, listedCh: make(chan struct{}, 1)}
	p := newConnectionPurger(store, time.Hour, 1, func(string, ...any) {})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	waitList := func(what string) {
		t.Helper()
		select {
		case <-store.listedCh:
		case <-time.After(5 * time.Second):
			t.Fatalf("no purge pass %s", what)
		}
	}
	waitList("at startup")
	p.Nudge()
	waitList("after a nudge")

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

// TestConnectionPurger_RunTicks checks the interval pass.
func TestConnectionPurger_RunTicks(t *testing.T) {
	store := &fakePurgeStore{listedCh: make(chan struct{}, 1)}
	p := newConnectionPurger(store, 10*time.Millisecond, 1, func(string, ...any) {})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	for i := 0; i < 2; i++ {
		select {
		case <-store.listedCh:
		case <-time.After(5 * time.Second):
			t.Fatalf("pass %d never ran", i)
		}
	}
}

// TestConnectionPurger_RetriesAfterFailedPass checks that a pass with a
// failed purge is followed by a retry after the retry delay, without
// waiting for the interval or a nudge, and that once a pass succeeds
// the purger goes back to waiting for the interval.
func TestConnectionPurger_RetriesAfterFailedPass(t *testing.T) {
	store := &fakePurgeStore{ids: []int{9}, failFirst: 1, listedCh: make(chan struct{}, 1)}
	logf, lines := captureLog()
	p := newConnectionPurger(store, time.Hour, 1, logf)
	p.retryDelay = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()

	for _, what := range []string{"at startup", "after the failed pass"} {
		select {
		case <-store.listedCh:
		case <-time.After(5 * time.Second):
			t.Fatalf("no purge pass %s", what)
		}
	}

	// The second pass succeeded, so nothing should run again until the
	// hour-long interval. Allow many retry delays for a stray pass.
	time.Sleep(200 * time.Millisecond)
	if got := store.listCount(); got != 2 {
		t.Errorf("purge passes = %d, want 2", got)
	}
	if got := lines(); len(got) != 1 {
		t.Errorf("logged %q, want one failure", got)
	}
}

// TestConnectionPurger_RetriesAfterListFailure checks that a failure to
// list the marked connections also schedules a retry.
func TestConnectionPurger_RetriesAfterListFailure(t *testing.T) {
	store := &fakePurgeStore{listErr: errors.New("list failed"), listedCh: make(chan struct{}, 1)}
	p := newConnectionPurger(store, time.Hour, 1, func(string, ...any) {})
	p.retryDelay = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	for i := 0; i < 3; i++ {
		select {
		case <-store.listedCh:
		case <-time.After(5 * time.Second):
			t.Fatalf("pass %d never ran", i)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

// TestConnectionPurger_NextRetryDelay checks that the retry delay
// doubles and stops at the interval.
func TestConnectionPurger_NextRetryDelay(t *testing.T) {
	p := newConnectionPurger(&fakePurgeStore{}, time.Minute, 1, func(string, ...any) {})
	tests := []struct {
		cur, want time.Duration
	}{
		{15 * time.Second, 30 * time.Second},
		{30 * time.Second, time.Minute},
		{45 * time.Second, time.Minute},
		{time.Minute, time.Minute},
	}
	for _, tt := range tests {
		if got := p.nextRetryDelay(tt.cur); got != tt.want {
			t.Errorf("nextRetryDelay(%v) = %v, want %v", tt.cur, got, tt.want)
		}
	}
}

// TestConnectionPurger_NudgeNeverBlocks checks that nudges coalesce into
// the single buffered slot rather than blocking the caller.
func TestConnectionPurger_NudgeNeverBlocks(t *testing.T) {
	p := newConnectionPurger(&fakePurgeStore{}, time.Hour, 1, func(string, ...any) {})
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			p.Nudge()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Nudge blocked")
	}
	if got := len(p.nudge); got != 1 {
		t.Fatalf("pending nudges = %d, want 1", got)
	}
}

func TestNewConnectionPurgerDefaults(t *testing.T) {
	p := NewConnectionPurger(NewTestDatastore(nil))
	if p.interval != DefaultConnectionPurgeInterval {
		t.Errorf("interval = %v, want %v", p.interval, DefaultConnectionPurgeInterval)
	}
	if p.retryDelay != DefaultConnectionPurgeRetryDelay {
		t.Errorf("retry delay = %v, want %v", p.retryDelay, DefaultConnectionPurgeRetryDelay)
	}
	if p.batchSize != DefaultConnectionPurgeBatchSize {
		t.Errorf("batch size = %d, want %d", p.batchSize, DefaultConnectionPurgeBatchSize)
	}
	if p.logf == nil {
		t.Error("logf is nil")
	}
}

// TestNewConnectionPurgerRetryDelayCapped checks that the retry delay
// never exceeds a shorter interval.
func TestNewConnectionPurgerRetryDelayCapped(t *testing.T) {
	p := newConnectionPurger(&fakePurgeStore{}, time.Second, 1, func(string, ...any) {})
	if p.retryDelay != time.Second {
		t.Errorf("retry delay = %v, want the 1s interval", p.retryDelay)
	}
}
