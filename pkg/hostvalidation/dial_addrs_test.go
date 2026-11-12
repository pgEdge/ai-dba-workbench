/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package hostvalidation

import (
	"context"
	"errors"
	"io"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// setDialTimings shortens fallbackDelay and minAttemptTimeout for one
// test, restoring them afterwards.
func setDialTimings(t *testing.T, fallback, minAttempt time.Duration) {
	t.Helper()
	oldFallback, oldMin := fallbackDelay, minAttemptTimeout
	fallbackDelay, minAttemptTimeout = fallback, minAttempt
	t.Cleanup(func() { fallbackDelay, minAttemptTimeout = oldFallback, oldMin })
}

// silentUntilDone is a dial step for an address that never answers: it
// waits for ctx to end and returns its error, as a dial to a route that
// drops packets does when its timeout expires.
func silentUntilDone(ctx context.Context) (net.Conn, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestGuardedDialContext_SilentFirstAddress is the case the reviewer
// raised: a provider whose IPv6 address silently drops traffic and whose
// IPv4 address works. The connection must be made through the IPv4
// address shortly after fallbackDelay, long before the dial timeout,
// with a real net.Dialer and a real listener.
func TestGuardedDialContext_SilentFirstAddress(t *testing.T) {
	setDialTimings(t, 50*time.Millisecond, minAttemptTimeout)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	const dialTimeout = 5 * time.Second
	dialer := &net.Dialer{Timeout: dialTimeout}
	var silentStopped atomic.Bool
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == net.JoinHostPort("2001:db8::1", port) {
			_, err := silentUntilDone(ctx)
			silentStopped.Store(true)
			return nil, err
		}
		return dialer.DialContext(ctx, network, addr)
	}
	guarded := GuardedDialContext(CheckAlwaysBlocked,
		fixedLookup("2001:db8::1", "127.0.0.1"), dial)

	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	began := time.Now()
	conn, err := guarded(ctx, "tcp", net.JoinHostPort("hooks.example.com", port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if elapsed := time.Since(began); elapsed > dialTimeout/2 {
		t.Errorf("connected after %v, want well under the %v dial timeout", elapsed, dialTimeout)
	}
	if got := conn.RemoteAddr().String(); got != ln.Addr().String() {
		t.Errorf("connected to %s, want %s", got, ln.Addr())
	}
	deadline := time.Now().Add(2 * time.Second)
	for !silentStopped.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !silentStopped.Load() {
		t.Error("the silent attempt was not stopped once the other connected")
	}
}

// pipeConn returns one end of a net.Pipe, closing both ends at cleanup.
func pipeConn(t *testing.T) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return client
}

func TestDialAddrs(t *testing.T) {
	ctx := context.Background()

	t.Run("a silent first address of the same family gets only its share of the deadline", func(t *testing.T) {
		setDialTimings(t, time.Hour, 10*time.Millisecond)
		want := pipeConn(t)
		dial := func(ctx context.Context, _, addr string) (net.Conn, error) {
			if addr == "192.0.2.1:25" {
				return silentUntilDone(ctx)
			}
			return want, nil
		}
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		began := time.Now()
		conn, err := dialAddrs(cctx, dial, "tcp", "25", []string{"192.0.2.1", "198.51.100.1"})
		if err != nil || conn != want {
			t.Fatalf("conn, err = %v, %v", conn, err)
		}
		if elapsed := time.Since(began); elapsed > 1500*time.Millisecond {
			t.Errorf("connected after %v, want about half the 2s deadline", elapsed)
		}
	})

	t.Run("a failed primary family starts the fallback at once", func(t *testing.T) {
		setDialTimings(t, time.Hour, minAttemptTimeout)
		want := pipeConn(t)
		var mu sync.Mutex
		var seen []string
		dial := func(_ context.Context, _, addr string) (net.Conn, error) {
			mu.Lock()
			seen = append(seen, addr)
			mu.Unlock()
			if addr == "8.8.8.8:443" {
				return want, nil
			}
			return nil, errDialRecorded
		}
		conn, err := dialAddrs(ctx, dial, "tcp", "443", []string{"2001:db8::1", "2001:db8::2", "8.8.8.8"})
		if err != nil || conn != want {
			t.Fatalf("conn, err = %v, %v", conn, err)
		}
		if w := []string{"[2001:db8::1]:443", "[2001:db8::2]:443", "8.8.8.8:443"}; !reflect.DeepEqual(seen, w) {
			t.Errorf("tried %v, want %v", seen, w)
		}
	})

	t.Run("when both families fail the primary family's error is returned", func(t *testing.T) {
		setDialTimings(t, time.Millisecond, minAttemptTimeout)
		errV4 := errors.New("v4 failed")
		errV6 := errors.New("v6 failed")
		dial := func(_ context.Context, _, addr string) (net.Conn, error) {
			if addr == "8.8.8.8:443" {
				return nil, errV4
			}
			return nil, errV6
		}
		if _, err := dialAddrs(ctx, dial, "tcp", "443", []string{"8.8.8.8", "2001:db8::1"}); !errors.Is(err, errV4) {
			t.Errorf("err = %v, want the IPv4 (primary) error", err)
		}
		if _, err := dialAddrs(ctx, dial, "tcp", "443", []string{"2001:db8::1", "8.8.8.8"}); !errors.Is(err, errV6) {
			t.Errorf("err = %v, want the IPv6 (primary) error", err)
		}
	})

	t.Run("a connection the losing attempt makes anyway is closed", func(t *testing.T) {
		setDialTimings(t, time.Millisecond, minAttemptTimeout)
		winner := pipeConn(t)
		late, lateServer := net.Pipe()
		defer func() { _ = lateServer.Close() }()
		release := make(chan struct{})
		dial := func(ctx context.Context, _, addr string) (net.Conn, error) {
			if addr == "8.8.8.8:443" {
				// Connects, but only after the fallback has won.
				<-release
				return late, nil
			}
			return winner, nil
		}
		conn, err := dialAddrs(ctx, dial, "tcp", "443", []string{"8.8.8.8", "2001:db8::1"})
		if err != nil || conn != winner {
			t.Fatalf("conn, err = %v, %v", conn, err)
		}
		close(release)
		// A closed pipe end makes a read on the other end fail.
		_ = lateServer.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := lateServer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
			t.Errorf("late connection not closed: read err = %v", err)
		}
	})

	t.Run("a canceled context does not start the fallback", func(t *testing.T) {
		setDialTimings(t, time.Hour, minAttemptTimeout)
		cctx, cancel := context.WithCancel(ctx)
		var mu sync.Mutex
		var seen []string
		dial := func(_ context.Context, _, addr string) (net.Conn, error) {
			mu.Lock()
			seen = append(seen, addr)
			mu.Unlock()
			cancel()
			return nil, errDialRecorded
		}
		if _, err := dialAddrs(cctx, dial, "tcp", "443", []string{"8.8.8.8", "2001:db8::1"}); !errors.Is(err, errDialRecorded) {
			t.Errorf("err = %v", err)
		}
		if w := []string{"8.8.8.8:443"}; !reflect.DeepEqual(seen, w) {
			t.Errorf("tried %v, want %v", seen, w)
		}
	})
}

func TestSplitByFamily(t *testing.T) {
	p, f := splitByFamily(nil)
	if p != nil || f != nil {
		t.Errorf("empty: %v, %v", p, f)
	}
	p, f = splitByFamily([]string{"2001:db8::1", "192.0.2.1", "::ffff:192.0.2.2", "2001:db8::2"})
	if w := []string{"2001:db8::1", "2001:db8::2"}; !reflect.DeepEqual(p, w) {
		t.Errorf("primaries = %v, want %v", p, w)
	}
	if w := []string{"192.0.2.1", "::ffff:192.0.2.2"}; !reflect.DeepEqual(f, w) {
		t.Errorf("fallbacks = %v, want %v", f, w)
	}
}

func TestAttemptContext(t *testing.T) {
	setDialTimings(t, fallbackDelay, time.Second)
	ctx := context.Background()

	// No deadline, or the last attempt: ctx itself.
	if got, cancel := attemptContext(ctx, 3); got != ctx {
		t.Error("no deadline: a derived context")
	} else {
		cancel()
	}
	dctx, cancelD := context.WithTimeout(ctx, 10*time.Second)
	defer cancelD()
	if got, cancel := attemptContext(dctx, 1); got != dctx {
		t.Error("last attempt: a derived context")
	} else {
		cancel()
	}

	share := func(parent context.Context, remaining int) time.Duration {
		got, cancel := attemptContext(parent, remaining)
		defer cancel()
		d, ok := got.Deadline()
		if !ok {
			t.Fatal("no deadline")
		}
		return time.Until(d)
	}
	// An even share of the time left.
	if d := share(dctx, 4); d > 2600*time.Millisecond || d < 2*time.Second {
		t.Errorf("share of 10s among 4 = %v, want about 2.5s", d)
	}
	// Never less than minAttemptTimeout.
	if d := share(dctx, 100); d < 900*time.Millisecond || d > time.Second {
		t.Errorf("share of 10s among 100 = %v, want the 1s minimum", d)
	}
	// But no more than is left.
	sctx, cancelS := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancelS()
	if d := share(sctx, 5); d > 300*time.Millisecond {
		t.Errorf("share of 300ms = %v, want at most 300ms", d)
	}
}
