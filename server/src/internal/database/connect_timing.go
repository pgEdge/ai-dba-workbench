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
	"sync/atomic"
	"time"
)

// MonitoredConnectTimeout bounds the connect phase of a request that
// connects to a monitored server whilst the caller waits, and is also
// the least time such a request takes to report a failed connect. It
// matches the connect_timeout the collector sets on its own connections.
//
// Without the floor, the time taken to answer separates the outcomes of
// a failed connect: a refused port fails in about a millisecond, whilst
// a filtered port or a silent host takes until the deadline. A user who
// can save a connection could then time the reply to scan ports from
// the server's network position (issue #850). Holding every failure
// until the deadline makes the two look alike; whether a connection
// succeeded remains visible, as it must.
const MonitoredConnectTimeout = 10 * time.Second

// ConnectTiming is how ConnectMonitored bounds and pads a connect
// phase. Timeout is the deadline the connect runs under, and Floor the
// least time a failed connect takes to return. Now and Sleep are the
// clock, replaceable so that tests need not wait in real time; Sleep
// returns early when ctx ends.
type ConnectTiming struct {
	Timeout time.Duration
	Floor   time.Duration
	Now     func() time.Time
	Sleep   func(ctx context.Context, d time.Duration)
}

// defaultConnectTiming is the timing used outside tests.
func defaultConnectTiming() *ConnectTiming {
	return &ConnectTiming{
		Timeout: MonitoredConnectTimeout,
		Floor:   MonitoredConnectTimeout,
		Now:     time.Now,
		Sleep:   sleepContext,
	}
}

// connectTiming holds the timing ConnectMonitored uses.
var connectTiming atomic.Pointer[ConnectTiming]

func init() {
	connectTiming.Store(defaultConnectTiming())
}

// SetConnectTiming replaces the timing ConnectMonitored uses and returns
// a function that restores the previous one. It exists for tests: a
// package's TestMain can lower the floor so that tests exercising a
// failed connect do not each wait MonitoredConnectTimeout, and a test
// can install a fake clock to check the floor without waiting at all.
// A nil Now or Sleep, or a non-positive Timeout, keeps the default; a
// zero Floor holds nothing, so SetConnectTiming(ConnectTiming{}) keeps
// the production deadline and turns the hold off.
func SetConnectTiming(t ConnectTiming) (restore func()) {
	def := defaultConnectTiming()
	if t.Timeout <= 0 {
		t.Timeout = def.Timeout
	}
	if t.Now == nil {
		t.Now = def.Now
	}
	if t.Sleep == nil {
		t.Sleep = def.Sleep
	}
	prev := connectTiming.Swap(&t)
	return func() { connectTiming.Store(prev) }
}

// ConnectMonitored runs connect, the connect phase of a request that
// connects to a monitored server whilst the caller waits, under a
// deadline of MonitoredConnectTimeout. When connect fails, it holds the
// return until MonitoredConnectTimeout has passed since it started, so
// that a refused port, a filtered one and a host that is not PostgreSQL
// all take the same time to report; see MonitoredConnectTimeout. The
// hold ends early if ctx ends, since the caller is then no longer
// waiting for the answer. connect's error is returned unchanged.
func ConnectMonitored(ctx context.Context, connect func(context.Context) error) error {
	elapsed, err := boundedConnect(ctx, connect)
	if err != nil {
		holdFailedConnect(ctx, elapsed)
	}
	return err
}

// boundedConnect runs connect under the connect deadline and returns
// how long it took. It is the first half of ConnectMonitored, for a
// caller whose hold runs elsewhere (ClientManager, through
// failedConnectError).
func boundedConnect(ctx context.Context, connect func(context.Context) error) (time.Duration, error) {
	timing := connectTiming.Load()
	start := timing.Now()

	connectCtx, cancel := context.WithTimeout(ctx, timing.Timeout)
	err := connect(connectCtx)
	cancel()
	return timing.Now().Sub(start), err
}

// holdFailedConnect waits out the rest of the floor after a failed
// connect that took elapsed, ending early if ctx ends.
func holdFailedConnect(ctx context.Context, elapsed time.Duration) {
	timing := connectTiming.Load()
	if remaining := timing.Floor - elapsed; remaining > 0 {
		timing.Sleep(ctx, remaining)
	}
}

// failedConnectError carries a failed connect's duration from where it
// ran, in ClientManager, to GetClientForSession, which holds the
// failure. Its text is the cause's.
type failedConnectError struct {
	err     error
	elapsed time.Duration
}

func (e *failedConnectError) Error() string { return e.err.Error() }
func (e *failedConnectError) Unwrap() error { return e.err }

// sleepContext waits for d, or until ctx ends if that is sooner.
func sleepContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}
