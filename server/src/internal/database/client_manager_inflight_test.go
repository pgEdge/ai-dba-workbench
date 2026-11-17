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
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// blockingDialGuard is a dial guard whose dials wait until release is
// closed and then fail. started receives once per dial.
type blockingDialGuard struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	dials   int
}

var errBlockedDial = errors.New("blocked dial released")

func newBlockingDialGuard() *blockingDialGuard {
	return &blockingDialGuard{started: make(chan struct{}, 8), release: make(chan struct{})}
}

func (g *blockingDialGuard) guard(c *pgconn.Config) {
	c.DialFunc = func(ctx context.Context, _, _ string) (net.Conn, error) {
		g.mu.Lock()
		g.dials++
		g.mu.Unlock()
		g.started <- struct{}{}
		select {
		case <-g.release:
		case <-ctx.Done():
		}
		return nil, errBlockedDial
	}
}

func (g *blockingDialGuard) dialCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.dials
}

// TestGetOrCreateClientConnectOutsideLock checks that a connect in
// flight holds no lock that other requests wait on: a cached client is
// returned whilst another key's connect is blocked, so its latency does
// not follow that connect's outcome (issue #850). It also checks that
// callers for the same key and connection string share the one connect,
// and that a sharing caller whose context has ended stops waiting.
func TestGetOrCreateClientConnectOutsideLock(t *testing.T) {
	installFakeConnectClock(t)
	cm := NewClientManager(nil)
	t.Cleanup(func() { _ = cm.CloseAll() })
	g := newBlockingDialGuard()
	cm.SetMonitoredDialGuard(g.guard)

	cachedStr := "postgres://u@127.0.0.1:1/cached?sslmode=disable"
	cached := NewClientWithConnectionString(cachedStr, nil)
	if err := cm.SetClient("cached", cached); err != nil {
		t.Fatalf("SetClient: %v", err)
	}

	slowStr := "postgres://u@127.0.0.1:1/slow?sslmode=disable"
	results := make(chan error, 2)
	slow := func() {
		_, err := cm.GetOrCreateClientWithConnString(context.Background(), "slow", slowStr)
		results <- err
	}
	go slow()
	<-g.started

	got, err := cm.GetOrCreateClientWithConnString(context.Background(), "cached", cachedStr)
	if err != nil || got != cached {
		t.Fatalf("cache hit during another connect = %p, %v; want %p", got, err, cached)
	}

	doneCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cm.GetOrCreateClientWithConnString(doneCtx, "slow", slowStr); !errors.Is(err, context.Canceled) {
		t.Errorf("waiter with an ended context: error = %v, want context.Canceled", err)
	}

	// The caller above whose context had ended was the first waiter; this is the second.
	go slow()
	waitForWaiters(t, cm, "slow\x00"+slowStr, 2)
	close(g.release)

	for range 2 {
		if err := <-results; !errors.Is(err, errBlockedDial) {
			t.Errorf("error = %v, want the blocked dial's", err)
		}
	}
	if n := g.dialCount(); n != 1 {
		t.Errorf("dials = %d, want 1 shared by both callers", n)
	}
	if n := cm.GetClientCount(); n != 1 {
		t.Errorf("client count = %d, want only the cached client", n)
	}
}

// TestGetOrCreateClientInitiatorCancelDoesNotFailWaiters checks that
// the caller who started a shared connect giving up neither cancels the
// connect nor fails the callers still waiting on it.
func TestGetOrCreateClientInitiatorCancelDoesNotFailWaiters(t *testing.T) {
	installFakeConnectClock(t)
	cm := NewClientManager(nil)
	g := newBlockingDialGuard()
	cm.SetMonitoredDialGuard(g.guard)
	connStr := "postgres://u@127.0.0.1:1/x?sslmode=disable"

	initCtx, cancel := context.WithCancel(context.Background())
	initiator := make(chan error, 1)
	go func() {
		_, err := cm.GetOrCreateClientWithConnString(initCtx, "k", connStr)
		initiator <- err
	}()
	<-g.started

	waiter := make(chan error, 1)
	go func() {
		_, err := cm.GetOrCreateClientWithConnString(context.Background(), "k", connStr)
		waiter <- err
	}()
	waitForWaiters(t, cm, "k\x00"+connStr, 1)

	cancel()
	if err := <-initiator; !errors.Is(err, context.Canceled) {
		t.Errorf("initiator error = %v, want context.Canceled", err)
	}
	close(g.release)
	err := <-waiter
	if !errors.Is(err, errBlockedDial) || errors.Is(err, context.Canceled) {
		t.Errorf("waiter error = %v, want the dial's own failure", err)
	}
}

// waitForWaiters polls until the connect in flight under flightKey has
// n callers waiting on it.
func waitForWaiters(t *testing.T, cm *ClientManager, flightKey string, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		cm.mu.RLock()
		call, ok := cm.inflight[flightKey]
		reached := ok && call.waiters >= n
		cm.mu.RUnlock()
		if reached {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %d waiters on the connect in flight", n)
		}
		time.Sleep(time.Millisecond)
	}
}
