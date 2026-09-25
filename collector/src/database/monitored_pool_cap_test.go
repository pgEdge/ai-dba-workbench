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
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// createExtraDatabases creates count empty databases on the test server
// alongside the one TestMain provisioned, so that the pool manager has
// several per-database pools to open for one monitored connection, as a
// database-scoped probe does on a real server. They are dropped when the
// test finishes.
func createExtraDatabases(t *testing.T, count int) []string {
	t.Helper()
	requireSchema(t)
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, getAdminConnectionString())
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	t.Cleanup(admin.Close)

	names := make([]string, 0, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("%s_cap%d", testDBName, i)
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
			t.Fatalf("create database %s: %v", name, err)
		}
		names = append(names, name)
	}
	t.Cleanup(func() {
		for _, name := range names {
			// WITH (FORCE) terminates any connection a failed test
			// left behind, so the drop cannot be blocked by it.
			if _, err := admin.Exec(context.Background(),
				"DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
				t.Logf("drop database %s: %v", name, err)
			}
		}
	})
	return names
}

// serverConnectionCount counts the monitoring connections the server
// itself reports as open to the given databases, which is what the
// operator of a monitored server sees and what its max_connections
// limit is spent on.
func serverConnectionCount(t *testing.T, databases []string) int {
	t.Helper()
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, getAdminConnectionString())
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	defer admin.Close()

	var n int
	if err := admin.QueryRow(ctx, `
        SELECT count(*)
        FROM pg_stat_activity
        WHERE application_name = $1
          AND datname = ANY($2)`,
		ApplicationName, databases).Scan(&n); err != nil {
		t.Fatalf("count monitoring connections: %v", err)
	}
	return n
}

// waitForServerConnectionCount polls pg_stat_activity until the count
// of monitoring connections drops to at most want, since a connection
// the pool has closed can take a moment to leave the server's view.
// It returns the last count observed.
func waitForServerConnectionCount(t *testing.T, databases []string, want int) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		n := serverConnectionCount(t, databases)
		if n <= want || time.Now().After(deadline) {
			return n
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestPoolManager_CapCountsOpenConnectionsAcrossDatabases is the
// regression test for issue #539. A database-scoped probe visits every
// database on a monitored server through its own per-database pool, and
// each of those pools used to keep its connections open for
// monitored_max_idle_seconds, so the server saw one idle connection per
// database however low max_connections_per_server was set. The cap must
// bound the connections open to the server, not only those in use.
func TestPoolManager_CapCountsOpenConnectionsAcrossDatabases(t *testing.T) {
	extra := createExtraDatabases(t, 4)
	all := append([]string{testDBName}, extra...)

	const maxConns = 2
	m := NewMonitoredConnectionPoolManager(maxConns, 300)
	t.Cleanup(func() { _ = m.Close() })

	mc := makeMonitoredConn(t, 539, testMonitoredServerSecret)
	ctx := context.Background()

	// Visit every database twice, one at a time, returning each
	// connection before taking the next, exactly as the scheduler does.
	for round := 0; round < 2; round++ {
		for _, db := range all {
			c, err := m.GetConnectionForDatabase(ctx, mc, db, testMonitoredServerSecret)
			if err != nil {
				t.Fatalf("GetConnectionForDatabase(%s): %v", db, err)
			}
			if err := c.Ping(ctx); err != nil {
				t.Fatalf("ping %s: %v", db, err)
			}
			m.ReturnConnection(mc.ID, c)

			if got := m.openConnections(mc.ID); got > maxConns {
				t.Fatalf("pool manager holds %d connections for connection %d after visiting %s; cap is %d",
					got, mc.ID, db, maxConns)
			}
		}
	}

	if got := waitForServerConnectionCount(t, all, maxConns); got > maxConns {
		t.Errorf("server reports %d monitoring connections across %d databases; max_connections_per_server is %d",
			got, len(all), maxConns)
	}
}

// TestPoolManager_CapHoldsUnderConcurrency drives several goroutines at
// random databases at once and checks, whilst each holds a connection,
// that the manager never has more connections open for the monitored
// server than the cap allows.
func TestPoolManager_CapHoldsUnderConcurrency(t *testing.T) {
	extra := createExtraDatabases(t, 3)
	all := append([]string{testDBName}, extra...)

	const maxConns = 3
	m := NewMonitoredConnectionPoolManager(maxConns, 300)
	t.Cleanup(func() { _ = m.Close() })

	mc := makeMonitoredConn(t, 5390, testMonitoredServerSecret)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	peak := 0
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 12; i++ {
				db := all[(g+i)%len(all)]
				c, err := m.GetConnectionForDatabase(ctx, mc, db, testMonitoredServerSecret)
				if err != nil {
					t.Errorf("GetConnectionForDatabase(%s): %v", db, err)
					return
				}
				open := m.openConnections(mc.ID)
				mu.Lock()
				if open > peak {
					peak = open
				}
				mu.Unlock()
				if err := c.Ping(ctx); err != nil {
					t.Errorf("ping %s: %v", db, err)
				}
				m.ReturnConnection(mc.ID, c)
			}
		}(g)
	}
	wg.Wait()

	if peak > maxConns {
		t.Errorf("pool manager held up to %d connections for one monitored server; cap is %d", peak, maxConns)
	}
	if got := waitForServerConnectionCount(t, all, maxConns); got > maxConns {
		t.Errorf("server reports %d monitoring connections; cap is %d", got, maxConns)
	}
}

// TestPoolManager_InvalidateChangedPools_KeepsUnchangedPools covers the
// second half of issue #539: every configuration reload used to close
// and re-dial every monitored pool even though nothing had changed,
// because the stored hash came from a connection string whose parameter
// order is random, and because a per-database pool was compared against
// the hash of the default database's string.
func TestPoolManager_InvalidateChangedPools_KeepsUnchangedPools(t *testing.T) {
	extra := createExtraDatabases(t, 1)

	m := NewMonitoredConnectionPoolManager(2, 300)
	t.Cleanup(func() { _ = m.Close() })

	mc := makeMonitoredConn(t, 5391, testMonitoredServerSecret)
	ctx := context.Background()

	for _, db := range []string{"", extra[0]} {
		c, err := m.GetConnectionForDatabase(ctx, mc, db, testMonitoredServerSecret)
		if err != nil {
			t.Fatalf("GetConnectionForDatabase(%q): %v", db, err)
		}
		m.ReturnConnection(mc.ID, c)
	}

	m.mu.RLock()
	before := len(m.pools)
	m.mu.RUnlock()
	if before != 2 {
		t.Fatalf("expected 2 pools before invalidation, got %d", before)
	}

	// Repeat the check: a random parameter order would only need a
	// few attempts to produce a mismatch.
	for i := 0; i < 20; i++ {
		m.InvalidateChangedPools([]MonitoredConnection{mc}, testMonitoredServerSecret)
	}

	m.mu.RLock()
	after := len(m.pools)
	m.mu.RUnlock()
	if after != before {
		t.Errorf("InvalidateChangedPools closed pools for an unchanged connection: %d before, %d after", before, after)
	}

	// A genuine change must still invalidate every pool derived from
	// the connection.
	changed := mc
	changed.Username = mc.Username + "_renamed"
	m.InvalidateChangedPools([]MonitoredConnection{changed}, testMonitoredServerSecret)
	m.mu.RLock()
	remaining := len(m.pools)
	m.mu.RUnlock()
	if remaining != 0 {
		t.Errorf("expected every pool invalidated after a parameter change, %d remain", remaining)
	}
}

// TestPoolManager_EvictionKeepsIdleConnectionsWithinRoom checks that
// eviction closes only as many idle connections as it must: with three
// idle connections in one pool and two free slots, opening a pool for a
// second database closes exactly one and hands the other two back.
func TestPoolManager_EvictionKeepsIdleConnectionsWithinRoom(t *testing.T) {
	extra := createExtraDatabases(t, 1)

	const maxConns = 3
	m := NewMonitoredConnectionPoolManager(maxConns, 300)
	t.Cleanup(func() { _ = m.Close() })

	mc := makeMonitoredConn(t, 5392, testMonitoredServerSecret)
	ctx := context.Background()

	// Fill the default database's pool with three connections at once,
	// then return them all so that they sit idle.
	held := make([]*pgxpool.Conn, 0, maxConns)
	for i := 0; i < maxConns; i++ {
		c, err := m.GetConnectionForDatabase(ctx, mc, "", testMonitoredServerSecret)
		if err != nil {
			t.Fatalf("GetConnectionForDatabase: %v", err)
		}
		held = append(held, c)
	}
	for _, c := range held {
		m.ReturnConnection(mc.ID, c)
	}
	if got := m.openConnections(mc.ID); got != maxConns {
		t.Fatalf("expected %d idle connections, got %d", maxConns, got)
	}

	c, err := m.GetConnectionForDatabase(ctx, mc, extra[0], testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("GetConnectionForDatabase(%s): %v", extra[0], err)
	}
	defer m.ReturnConnection(mc.ID, c)

	m.mu.RLock()
	defaultIdle := m.pools[mc.ID].Stat().IdleConns()
	m.mu.RUnlock()
	if defaultIdle != maxConns-1 {
		t.Errorf("expected %d idle connections kept in the default pool, got %d", maxConns-1, defaultIdle)
	}
	if got := m.openConnections(mc.ID); got != maxConns {
		t.Errorf("expected %d connections open, got %d", maxConns, got)
	}
}

// TestPoolManager_EvictIdleConnections_NoSemaphore checks that eviction
// does nothing for a connection that has no semaphore, rather than
// computing room from a nil channel.
func TestPoolManager_EvictIdleConnections_NoSemaphore(t *testing.T) {
	m := NewMonitoredConnectionPoolManager(2, 300)
	t.Cleanup(func() { _ = m.Close() })

	m.evictIdleConnections(context.Background(), 987654, 987654)

	if got := m.openConnections(987654); got != 0 {
		t.Errorf("expected no connections for an unknown connection, got %d", got)
	}
}

// startDelayingProxy starts a TCP proxy to the test server that waits
// for the current delay before forwarding each new connection, so that
// a test can make pgxpool take as long as it likes to open one. It
// returns the proxy's port and the delay to set.
func startDelayingProxy(t *testing.T) (int, *atomic.Int64) {
	t.Helper()
	cfg := parseTestServerURL(t)
	upstream := net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	delay := &atomic.Int64{}
	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer client.Close()
				time.Sleep(time.Duration(delay.Load()))
				server, err := net.Dial("tcp", upstream)
				if err != nil {
					return
				}
				defer server.Close()
				go pipeUntilClosed(server, client)
				pipeUntilClosed(client, server)
			}()
		}
	}()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("proxy address %v is not TCP", ln.Addr())
	}
	return addr.Port, delay
}

// pipeUntilClosed copies src to dst until either side is closed. The
// error that ends the copy is expected, since it is how the proxy
// learns that the pool closed the connection, so it is not reported.
func pipeUntilClosed(dst io.Writer, src io.Reader) {
	if _, err := io.Copy(dst, src); err != nil {
		return
	}
}

// TestPoolManager_SlotHeldWhilstAbandonedAcquireConstructs covers an
// acquisition whose context ends first: pgxpool returns the context's error at once
// but goes on opening the connection in the background and then keeps
// it as idle. Releasing the semaphore slot straight away would let
// another acquisition open a connection alongside the one still being
// opened, so the slot must stay held until the construction finishes.
func TestPoolManager_SlotHeldWhilstAbandonedAcquireConstructs(t *testing.T) {
	requireSchema(t)
	port, delay := startDelayingProxy(t)

	m := NewMonitoredConnectionPoolManager(2, 300)
	t.Cleanup(func() { _ = m.Close() })

	mc := makeMonitoredConn(t, 5394, testMonitoredServerSecret)
	mc.Host = "127.0.0.1"
	mc.Port = port

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	held, err := m.GetConnection(ctx, mc, testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	defer m.ReturnConnection(mc.ID, held)

	// The pool now has no idle connection, so the next acquisition has
	// to open one, and the proxy makes that outlast the caller.
	delay.Store(int64(time.Second))
	short, cancelShort := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelShort()
	if c, err := m.GetConnection(short, mc, testMonitoredServerSecret); err == nil {
		m.ReturnConnection(mc.ID, c)
		t.Fatal("expected the acquisition to time out")
	}

	sem := m.getSemaphore(mc.ID)
	if got := len(sem); got != 2 {
		t.Errorf("slots held straight after the abandoned acquisition = %d, want 2 whilst its connection is still being opened", got)
	}

	deadline := time.Now().Add(10 * time.Second)
	for len(sem) != 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := len(sem); got != 1 {
		t.Fatalf("slots held once the construction finished = %d, want 1", got)
	}
	pool := getPoolFor(t, m, mc.ID)
	if idle := pool.Stat().IdleConns(); idle != 1 {
		t.Errorf("idle connections after the construction finished = %d, want 1", idle)
	}
}
