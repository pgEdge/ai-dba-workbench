/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package scheduler

// The tests in this file drive the shutdown, cancellation and failure
// branches of the scheduler deterministically. Several of these branches
// were previously reached only when a timer or a goroutine happened to
// fire at the right moment, so the package's coverage varied between
// runs of identical code. Nothing here waits on a sleep: each
// branch is reached by arranging state so that exactly one select case
// can be ready, or by a context that changes state at a known call.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/collector/src/database"
)

// scriptedDoneCtx is a context whose Done channel changes at a chosen
// call. acquireProbeSlot reads ps.ctx.Done() exactly once in each of its
// five shutdown checks, in a fixed order, so the call number identifies
// the check that sees the change. Go evaluates every channel operand of
// a select in source order before choosing a case, so a change made
// during a call is visible to the select that made it.
type scriptedDoneCtx struct {
	context.Context

	mu    sync.Mutex
	calls int

	// at is the Done call at which the change happens.
	at int
	// shutdown, when non-nil, is closed at call at and the context
	// itself never finishes; when nil, the context reads as finished
	// from call at onwards.
	shutdown chan struct{}

	open     chan struct{}
	finished chan struct{}
}

func newScriptedDoneCtx(at int, shutdown chan struct{}) *scriptedDoneCtx {
	finished := make(chan struct{})
	close(finished)
	return &scriptedDoneCtx{
		Context:  context.Background(),
		at:       at,
		shutdown: shutdown,
		open:     make(chan struct{}),
		finished: finished,
	}
}

func (c *scriptedDoneCtx) Done() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.shutdown != nil {
		if c.calls == c.at {
			close(c.shutdown)
		}
		return c.open
	}
	if c.calls >= c.at {
		return c.finished
	}
	return c.open
}

func (c *scriptedDoneCtx) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.shutdown == nil && c.calls >= c.at {
		return context.Canceled
	}
	return nil
}

func (c *scriptedDoneCtx) doneCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestAcquireProbeSlot_EveryShutdownCheck reaches each of the shutdown
// checks in acquireProbeSlot in turn, both through shutdownChan and
// through the context, and confirms that nothing is left held whichever
// check refuses the acquisition. The semaphore a stage waits on is
// filled beforehand, so the only select case that can be ready is the
// shutdown signal raised during that stage's Done call.
func TestAcquireProbeSlot_EveryShutdownCheck(t *testing.T) {
	tests := []struct {
		name string
		// at is the Done call at which shutdown arrives: 1 is the check
		// on entry, 2 the wait for a per-connection permit, 3 the
		// recheck after it, 4 the wait for a global slot and 5 the
		// recheck once both are held.
		at             int
		viaShutdown    bool
		fillConnSlots  bool
		fillProbeSlots bool
	}{
		{name: "context finished on entry", at: 1},
		{name: "shutdown on entry", at: 1, viaShutdown: true},
		{name: "shutdown whilst waiting for a per-connection permit", at: 2, viaShutdown: true, fillConnSlots: true},
		{name: "context finished whilst waiting for a per-connection permit", at: 2, fillConnSlots: true},
		{name: "context finished once the per-connection permit is held", at: 3},
		{name: "shutdown once the per-connection permit is held", at: 3, viaShutdown: true},
		{name: "shutdown whilst waiting for a global slot", at: 4, viaShutdown: true, fillProbeSlots: true},
		{name: "context finished whilst waiting for a global slot", at: 4, fillProbeSlots: true},
		{name: "context finished once both are held", at: 5},
		{name: "shutdown once both are held", at: 5, viaShutdown: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := NewProbeScheduler(nil, nil, testConfig{maxConcurrentProbes: 1, maxProbesPerConn: 1}, "")
			t.Cleanup(ps.cancel)

			var shutdown chan struct{}
			if tc.viaShutdown {
				shutdown = ps.shutdownChan
			}
			ctx := newScriptedDoneCtx(tc.at, shutdown)
			ps.ctx = ctx

			const connectionID = 1
			connSlots := ps.connSlotsFor(connectionID)
			wantConn, wantGlobal := 0, 0
			if tc.fillConnSlots {
				connSlots <- struct{}{}
				wantConn = 1
			}
			if tc.fillProbeSlots {
				ps.probeSlots <- struct{}{}
				wantGlobal = 1
			}

			release, ok := ps.acquireProbeSlot(connectionID)
			if ok {
				release()
				t.Fatal("acquired a slot although the scheduler was stopping")
			}
			if release != nil {
				t.Error("a refused acquisition returned a release function")
			}
			if got := ctx.doneCalls(); got != tc.at {
				t.Errorf("refused after %d shutdown checks, want %d", got, tc.at)
			}
			if got := len(connSlots); got != wantConn {
				t.Errorf("per-connection permits held: got %d, want %d", got, wantConn)
			}
			if got := len(ps.probeSlots); got != wantGlobal {
				t.Errorf("global slots held: got %d, want %d", got, wantGlobal)
			}
		})
	}
}

// TestAcquireProbeSlot_ScriptedContextNeverFinishing confirms the
// scripted context leaves acquisition alone when its change comes after
// the last check, so the table above really is selecting each check.
func TestAcquireProbeSlot_ScriptedContextNeverFinishing(t *testing.T) {
	ps := NewProbeScheduler(nil, nil, testConfig{maxConcurrentProbes: 1, maxProbesPerConn: 1}, "")
	t.Cleanup(ps.cancel)
	ctx := newScriptedDoneCtx(6, nil)
	ps.ctx = ctx

	release, ok := ps.acquireProbeSlot(1)
	if !ok {
		t.Fatal("acquisition refused although the scheduler was not stopping")
	}
	if got := ctx.doneCalls(); got != 5 {
		t.Errorf("acquisition made %d shutdown checks, want 5", got)
	}
	release()
	if len(ps.probeSlots) != 0 || len(ps.connSlotsFor(1)) != 0 {
		t.Error("release left a permit held")
	}
}

// finishesAfterFirstCheckCtx passes the first Err check a caller makes
// and reads as ended from then on. The functions under test check
// ctx.Err() once on entry before asking the pool manager for a
// connection, so this drives the branch where the context ends whilst
// the caller is waiting for that connection, without relying on a
// timeout expiring during the wait.
type finishesAfterFirstCheckCtx struct {
	context.Context

	mu      sync.Mutex
	checked bool
	done    chan struct{}
}

func newFinishesAfterFirstCheckCtx() *finishesAfterFirstCheckCtx {
	return &finishesAfterFirstCheckCtx{
		Context: context.Background(),
		done:    make(chan struct{}),
	}
}

func (c *finishesAfterFirstCheckCtx) Done() <-chan struct{} { return c.done }

func (c *finishesAfterFirstCheckCtx) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.checked {
		c.checked = true
		close(c.done)
		return nil
	}
	return context.Canceled
}

// unbuildableConn is a monitored connection whose stored host is a
// multi-host list, which the pool manager refuses before connecting, so
// asking for a connection to it fails at once without any network I/O
// and without the context ending.
func unbuildableConn() database.MonitoredConnection {
	return database.MonitoredConnection{
		ID:           99_101,
		Name:         "unbuildable",
		Host:         "db1.example.com,db2.example.com",
		Port:         5432,
		DatabaseName: "postgres",
		Username:     "postgres",
		SSLMode:      sql.NullString{String: "disable", Valid: true},
	}
}

// waitingConn is a monitored connection used with a pool manager whose
// per-server semaphore has no capacity, so every request for a
// connection waits for a slot until the context ends.
func waitingConn() database.MonitoredConnection {
	return database.MonitoredConnection{
		ID:           99_102,
		Name:         "waiting",
		Host:         "127.0.0.1",
		Port:         5432,
		DatabaseName: "postgres",
		Username:     "postgres",
		SSLMode:      sql.NullString{String: "disable", Valid: true},
	}
}

// TestExecuteProbeOnDatabase_ConnectionFailures covers both ways the
// per-database connection request can fail that are not a closed pool:
// the context ending whilst waiting for a slot, and a connection that
// cannot be built. Each is logged and yields no metrics and no extension
// observation, so the caller moves on to the next database.
func TestExecuteProbeOnDatabase_ConnectionFailures(t *testing.T) {
	endedCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name     string
		maxConns int
		ctx      context.Context
		conn     database.MonitoredConnection
	}{
		{"context ends whilst waiting for a slot", 0, endedCtx, waitingConn()},
		{"connection cannot be built", 1, context.Background(), unbuildableConn()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pm := database.NewMonitoredConnectionPoolManager(tc.maxConns, 5)
			ps := NewProbeScheduler(nil, pm, testConfig{monitoredMaxWait: 1}, "")
			t.Cleanup(ps.cancel)

			metrics, status := ps.executeProbeOnDatabase(tc.ctx, &failingProbe{dbScoped: true}, tc.conn, "postgres", 0)
			if metrics != nil {
				t.Errorf("expected no metrics, got %v", metrics)
			}
			if status != extensionUnknown {
				t.Errorf("extension status: got %v, want extensionUnknown", status)
			}
		})
	}
}

// TestExecuteProbe_ConnectionRequestFailures covers the server-wide
// probe path and the database-scoped preparation step when the first
// connection request fails, either because the context ended whilst
// waiting for a slot (after the entry check had passed) or because the
// connection cannot be built. Both are reported as connection errors.
func TestExecuteProbe_ConnectionRequestFailures(t *testing.T) {
	tests := []struct {
		name     string
		maxConns int
		ctx      func() context.Context
		conn     database.MonitoredConnection
	}{
		{
			name:     "context ends whilst waiting for a slot",
			maxConns: 0,
			ctx:      func() context.Context { return newFinishesAfterFirstCheckCtx() },
			conn:     waitingConn(),
		},
		{
			name:     "connection cannot be built",
			maxConns: 1,
			ctx:      context.Background,
			conn:     unbuildableConn(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name+"/server-wide", func(t *testing.T) {
			pm := database.NewMonitoredConnectionPoolManager(tc.maxConns, 5)
			ps := NewProbeScheduler(nil, pm, testConfig{monitoredMaxWait: 1}, "")
			t.Cleanup(ps.cancel)

			metrics, connErr, msg, status := ps.executeProbeForServerWide(tc.ctx(), &failingProbe{}, tc.conn)
			if !connErr {
				t.Error("expected a connection error")
			}
			if !strings.HasPrefix(msg, "connection error: ") {
				t.Errorf("unexpected error message: %q", msg)
			}
			if metrics != nil {
				t.Errorf("expected no metrics, got %v", metrics)
			}
			if status != extensionUnknown {
				t.Errorf("extension status: got %v, want extensionUnknown", status)
			}
		})

		t.Run(tc.name+"/database-scoped", func(t *testing.T) {
			pm := database.NewMonitoredConnectionPoolManager(tc.maxConns, 5)
			ps := NewProbeScheduler(nil, pm, testConfig{monitoredMaxWait: 1}, "")
			t.Cleanup(ps.cancel)

			metrics, databases, connErr, msg, status := ps.executeProbeForAllDatabases(tc.ctx(), &failingProbe{dbScoped: true}, tc.conn)
			if !connErr {
				t.Error("expected a connection error")
			}
			if !strings.HasPrefix(msg, "connection error: ") {
				t.Errorf("unexpected error message: %q", msg)
			}
			if metrics != nil || databases != nil {
				t.Errorf("expected no metrics or databases, got %v and %v", metrics, databases)
			}
			if status != extensionUnknown {
				t.Errorf("extension status: got %v, want extensionUnknown", status)
			}
		})
	}
}

// handFedTicker returns a ticker whose channel the test sends on, so the
// reload loop wakes exactly when the test says. A Ticker not made by
// time.NewTicker is safe to Stop.
func handFedTicker() (*time.Ticker, chan time.Time) {
	ch := make(chan time.Time)
	return &time.Ticker{C: ch}, ch
}

// feedTick hands one tick to the reload loop. The channel is unbuffered,
// so the send completes only once the loop is back in its select, which
// for every tick after the first proves the previous reload finished and
// the loop survived it. The timeout is a guard against a hung loop, not
// a means of ordering the steps.
func feedTick(t *testing.T, tick chan<- time.Time) {
	t.Helper()
	select {
	case tick <- time.Now():
	case <-time.After(30 * time.Second):
		t.Fatal("configReloadLoop did not take the tick")
	}
}

// closedDatastore returns a datastore for the fixture's database whose
// pool has already been closed, so every request for a connection fails
// at once.
func closedDatastore(t *testing.T, f *integrationFixture) *database.Datastore {
	t.Helper()
	dsCfg := datastoreCfg{
		host:           f.host,
		port:           f.port,
		username:       f.username,
		password:       f.rawPassword,
		database:       f.dbName,
		sslMode:        "disable",
		maxConnections: 1,
		maxIdleSeconds: 5,
	}
	ds, err := database.NewDatastore(&dsCfg)
	if err != nil {
		t.Fatalf("NewDatastore: %v", err)
	}
	ds.Close()
	return ds
}

// TestConfigReloadLoop_ReloadFailureKeepsLooping drives the reload error
// branch: with the datastore closed every reload fails, and the loop
// logs the failure and keeps waiting for the next tick.
func TestConfigReloadLoop_ReloadFailureKeepsLooping(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(closedDatastore(t, f), f.pool, integrationTestConfig(), testServerSecret)
	t.Cleanup(ps.cancel)

	ticker, tick := handFedTicker()
	ps.configReloader = ticker
	ps.wg.Add(1)
	go ps.configReloadLoop()

	feedTick(t, tick)
	feedTick(t, tick)

	// Closing shutdownChan rather than ending the context leaves the
	// reload in progress to finish undisturbed.
	close(ps.shutdownChan)
	ps.wg.Wait()
}

// scratchDatastore creates a database of its own for a test that needs
// to alter the schema, with one monitored connection seeded and no
// server-level probe configuration, and drops it when the test ends.
func scratchDatastore(t *testing.T, f *integrationFixture) (*database.Datastore, int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adminPool, err := pgxpool.New(ctx, fmt.Sprintf(
		"host=%s port=%d user=%s password=%s sslmode=disable dbname=postgres pool_max_conns=1",
		f.host, f.port, f.username, f.rawPassword))
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	// Registered first so that it runs last, after the drop below.
	t.Cleanup(adminPool.Close)

	dbName := fmt.Sprintf("ai_workbench_sched_branch_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName)); err != nil {
		t.Fatalf("create db: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(context.Background(),
			fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", dbName)); err != nil {
			t.Logf("warning: drop %s on cleanup: %v", dbName, err)
		}
	})

	// loadConfigs holds one connection throughout and takes a second
	// for each monitored-connection lookup, so one is not enough.
	ds, err := database.NewDatastore(&datastoreCfg{
		host:           f.host,
		port:           f.port,
		username:       f.username,
		password:       f.rawPassword,
		database:       dbName,
		sslMode:        "disable",
		maxConnections: 4,
		maxIdleSeconds: 5,
	})
	if err != nil {
		t.Fatalf("NewDatastore: %v", err)
	}
	t.Cleanup(ds.Close)

	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	defer ds.ReturnConnection(conn)

	var connID int
	if err := conn.QueryRow(ctx, `
		INSERT INTO connections
			(name, host, port, database_name, username, owner_username, is_monitored, sslmode)
		VALUES ('branch-test', '127.0.0.1', 5432, 'postgres', 'postgres', 'tester', TRUE, 'disable')
		RETURNING id
	`).Scan(&connID); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	if _, err := conn.Exec(ctx,
		"DELETE FROM probe_configs WHERE connection_id = $1", connID); err != nil {
		t.Fatalf("clear server-level probe configs: %v", err)
	}
	return ds, connID
}

// execScratch runs one statement against a scratch datastore.
func execScratch(t *testing.T, ds *database.Datastore, query string) {
	t.Helper()
	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	defer ds.ReturnConnection(conn)
	if _, err := conn.Exec(context.Background(), query); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// TestSchedulerLoadConfigs_FailurePaths drives two failure branches of
// loadConfigs through the schema rather than through a context ending
// part way through: a server-level probe configuration that cannot be
// created is logged and skipped, and probe configurations that cannot
// be read fail the whole load.
func TestSchedulerLoadConfigs_FailurePaths(t *testing.T) {
	f := setupIntegration(t)
	ds, connID := scratchDatastore(t, f)
	ps := NewProbeScheduler(ds, f.pool, integrationTestConfig(), testServerSecret)
	t.Cleanup(ps.Stop)

	t.Run("server-level config cannot be created", func(t *testing.T) {
		execScratch(t, ds, `
			CREATE FUNCTION reject_server_probe_config() RETURNS trigger
			LANGUAGE plpgsql AS $$
			BEGIN
				RAISE EXCEPTION 'server-level probe configuration rejected by test';
			END
			$$`)
		execScratch(t, ds, `
			CREATE TRIGGER reject_server_probe_config
			BEFORE INSERT ON probe_configs
			FOR EACH ROW WHEN (NEW.scope = 'server')
			EXECUTE FUNCTION reject_server_probe_config()`)

		if err := ps.loadConfigs(context.Background()); err != nil {
			t.Fatalf("loadConfigs should skip a config it cannot create, got: %v", err)
		}
		ps.probesMutex.RLock()
		started := len(ps.probesByConn[connID])
		ps.probesMutex.RUnlock()
		if started != 0 {
			t.Errorf("started %d probes without a server-level config, want 0", started)
		}
	})

	t.Run("probe configs cannot be read", func(t *testing.T) {
		execScratch(t, ds, "ALTER TABLE probe_configs RENAME TO probe_configs_hidden")

		err := ps.loadConfigs(context.Background())
		if err == nil {
			t.Fatal("expected loadConfigs to fail when probe_configs cannot be read")
		}
		if !strings.Contains(err.Error(), "probe configs") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
