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
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/database"
	"github.com/pgedge/ai-workbench/collector/src/probes"
)

// gcTestConfig satisfies database.Config for the integration tests.
type gcTestConfig struct {
	host     string
	port     int
	database string
	username string
	password string
}

func (c *gcTestConfig) Validate() error                     { return nil }
func (c *gcTestConfig) GetPgHost() string                   { return c.host }
func (c *gcTestConfig) GetPgHostAddr() string               { return "" }
func (c *gcTestConfig) GetPgDatabase() string               { return c.database }
func (c *gcTestConfig) GetPgUsername() string               { return c.username }
func (c *gcTestConfig) GetPgPassword() string               { return c.password }
func (c *gcTestConfig) GetPgPort() int                      { return c.port }
func (c *gcTestConfig) GetPgSSLMode() string                { return "disable" }
func (c *gcTestConfig) GetPgSSLCert() string                { return "" }
func (c *gcTestConfig) GetPgSSLKey() string                 { return "" }
func (c *gcTestConfig) GetPgSSLRootCert() string            { return "" }
func (c *gcTestConfig) GetDatastorePoolMaxConnections() int { return 4 }
func (c *gcTestConfig) GetDatastorePoolMaxIdleSeconds() int { return 60 }

// gcTestEnv parses TEST_AI_WORKBENCH_SERVER into connection parts. The
// tests skip when it is unset, matching the rest of the suite.
func gcTestEnv(t *testing.T) *gcTestConfig {
	t.Helper()

	url := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if url == "" {
		url = os.Getenv("TEST_DB_CONN")
	}
	if url == "" {
		t.Skip("test database not provisioned")
	}
	if !strings.HasPrefix(url, "postgres://") && !strings.HasPrefix(url, "postgresql://") {
		t.Skipf("unsupported test database URL form: %q", url)
	}

	stripped := strings.TrimPrefix(strings.TrimPrefix(url, "postgresql://"), "postgres://")
	if i := strings.Index(stripped, "?"); i != -1 {
		stripped = stripped[:i]
	}

	cfg := &gcTestConfig{host: "127.0.0.1", port: 5432, username: "postgres"}

	userinfo, hostpart := "", stripped
	if at := strings.Index(stripped, "@"); at != -1 {
		userinfo, hostpart = stripped[:at], stripped[at+1:]
	}
	if userinfo != "" {
		if c := strings.Index(userinfo, ":"); c != -1 {
			cfg.username, cfg.password = userinfo[:c], userinfo[c+1:]
		} else {
			cfg.username = userinfo
		}
	}
	if slash := strings.Index(hostpart, "/"); slash != -1 {
		cfg.database = hostpart[slash+1:]
		hostpart = hostpart[:slash]
	}
	if colon := strings.Index(hostpart, ":"); colon != -1 {
		cfg.host = hostpart[:colon]
		if p, err := strconv.Atoi(hostpart[colon+1:]); err == nil {
			cfg.port = p
		}
	} else if hostpart != "" {
		cfg.host = hostpart
	}

	return cfg
}

// gcTestDatastore builds a throwaway database, migrates it, and returns a
// Datastore pointed at it along with a cleanup function.
func gcTestDatastore(t *testing.T) (*database.Datastore, func()) {
	t.Helper()

	cfg := gcTestEnv(t)
	ctx := context.Background()

	admin := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		cfg.username, cfg.password, cfg.host, cfg.port, cfg.database)
	adminPool, err := pgxpool.New(ctx, admin)
	if err != nil {
		t.Skipf("cannot reach the test server: %v", err)
	}

	name := fmt.Sprintf("ai_wb_gc437_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		adminPool.Close()
		t.Skipf("cannot create a test database: %v", err)
	}

	dropDatabase := func() {
		if _, err := adminPool.Exec(ctx, "DROP DATABASE IF EXISTS "+name); err != nil {
			t.Logf("dropping test database %s: %v", name, err)
		}
	}

	testCfg := *cfg
	testCfg.database = name

	ds, err := database.NewDatastore(&testCfg)
	if err != nil {
		dropDatabase()
		adminPool.Close()
		t.Fatalf("NewDatastore: %v", err)
	}

	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	if err := database.NewSchemaManager().Migrate(conn); err != nil {
		ds.ReturnConnection(conn)
		t.Fatalf("Migrate: %v", err)
	}
	ds.ReturnConnection(conn)

	return ds, func() {
		ds.Close()
		dropDatabase()
		adminPool.Close()
	}
}

// TestGarbageCollector_RecordsAndResumesCycle is the end-to-end
// regression test for #437. A collection must stamp its completion, and
// a fresh collector against the same datastore must then see the cycle
// as in progress rather than starting a new one, which is what makes
// retention survive a restart loop.
func TestGarbageCollector_RecordsAndResumesCycle(t *testing.T) {
	ds, cleanup := gcTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	gc := NewGarbageCollector(ds)

	// Nothing recorded yet, so collection is due.
	if got := gc.nextRunDelay(ctx, false); got != gcStartupGrace {
		t.Errorf("first nextRunDelay() = %v, want %v", got, gcStartupGrace)
	}

	if err := gc.collectGarbage(ctx); err != nil {
		t.Fatalf("collectGarbage: %v", err)
	}

	// The completion must be visible to a different collector instance,
	// standing in for the process having restarted.
	restarted := NewGarbageCollector(ds)
	at, found, err := restarted.lastRun(ctx)
	if err != nil || !found {
		t.Fatalf("lastRun after collection: found=%v err=%v", found, err)
	}
	if d := time.Since(at); d > time.Minute || d < -time.Minute {
		t.Errorf("recorded completion is %v away from now", d)
	}

	delay := restarted.nextRunDelay(ctx, false)
	if delay <= gcStartupGrace || delay > gcInterval {
		t.Errorf("restarted nextRunDelay() = %v, want the remainder of the cycle", delay)
	}
}

// TestGarbageCollector_DueAfterRecordedRunAges verifies that an
// overdue recorded completion schedules a prompt collection, which is
// the state Alex's datastore was in for nine days.
func TestGarbageCollector_DueAfterRecordedRunAges(t *testing.T) {
	ds, cleanup := gcTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	stale := time.Now().UTC().Add(-9 * 24 * time.Hour)
	if err := database.RecordMaintenanceRun(ctx, conn,
		database.PartitionRetentionTask, stale); err != nil {
		ds.ReturnConnection(conn)
		t.Fatalf("RecordMaintenanceRun: %v", err)
	}
	ds.ReturnConnection(conn)

	gc := NewGarbageCollector(ds)
	if got := gc.nextRunDelay(ctx, false); got != gcStartupGrace {
		t.Errorf("nextRunDelay() = %v, want %v", got, gcStartupGrace)
	}

	// A failed attempt must back off rather than spin.
	if got := gc.nextRunDelay(ctx, true); got != gcRetryDelay {
		t.Errorf("nextRunDelay(failed) = %v, want %v", got, gcRetryDelay)
	}
}

// TestGarbageCollector_DropsExpiredPartitionForConfiguredProbe drives a
// full pass against a real datastore and checks that a partition older
// than its probe's retention_days actually goes.
func TestGarbageCollector_DropsExpiredPartitionForConfiguredProbe(t *testing.T) {
	ds, cleanup := gcTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}

	var retention int
	if err := conn.QueryRow(ctx, `
        SELECT retention_days FROM probe_configs
        WHERE name = 'pg_stat_statements' AND scope = 'global'
    `).Scan(&retention); err != nil {
		ds.ReturnConnection(conn)
		t.Skipf("pg_stat_statements has no global probe config: %v", err)
	}

	partitions := func(c *pgxpool.Conn) map[string]bool {
		rows, err := c.Query(ctx, `
            SELECT tablename FROM pg_tables
            WHERE schemaname = 'metrics'
              AND tablename LIKE 'pg_stat_statements\_%'
        `)
		if err != nil {
			t.Fatalf("listing partitions: %v", err)
		}
		defer rows.Close()
		out := map[string]bool{}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatalf("scanning partition name: %v", err)
			}
			out[n] = true
		}
		return out
	}

	before := partitions(conn)

	// Create a partition comfortably past the configured window, using
	// the same helper the probes use so the bounds match production.
	old := time.Now().AddDate(0, 0, -(retention + 30))
	if err := probes.EnsurePartition(ctx, conn, "pg_stat_statements", old); err != nil {
		ds.ReturnConnection(conn)
		t.Fatalf("EnsurePartition: %v", err)
	}

	var aged string
	for n := range partitions(conn) {
		if !before[n] {
			aged = n
			break
		}
	}
	ds.ReturnConnection(conn)

	if aged == "" {
		t.Fatal("EnsurePartition created no new partition")
	}
	t.Logf("aged partition under test: metrics.%s (retention_days=%d)", aged, retention)

	gc := NewGarbageCollector(ds)
	if err := gc.collectGarbage(ctx); err != nil {
		t.Fatalf("collectGarbage: %v", err)
	}

	conn, err = ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	defer ds.ReturnConnection(conn)

	if partitions(conn)[aged] {
		t.Errorf("partition %s is older than retention_days=%d but was not dropped",
			aged, retention)
	}
}

// TestGarbageCollector_RunLoopCollectsAndReschedules exercises the
// scheduling loop itself with a compressed cadence: the first pass must
// run promptly, record its completion, and then wait out the remainder
// of the cycle rather than collecting again straight away.
func TestGarbageCollector_RunLoopCollectsAndReschedules(t *testing.T) {
	ds, cleanup := gcTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	gc := NewGarbageCollector(ds)
	gc.interval = 30 * time.Second
	gc.startupGrace = 50 * time.Millisecond
	gc.retryDelay = 50 * time.Millisecond

	if err := gc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for the first pass to record itself.
	var recorded time.Time
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		at, found, err := gc.lastRun(ctx)
		if err == nil && found {
			recorded = at
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if recorded.IsZero() {
		gc.Stop()
		t.Fatal("run loop never recorded a collection")
	}

	// With the cycle now in progress, the loop must be waiting rather
	// than collecting repeatedly.
	time.Sleep(500 * time.Millisecond)
	again, _, err := gc.lastRun(ctx)
	if err != nil {
		gc.Stop()
		t.Fatalf("lastRun: %v", err)
	}
	if !again.Equal(recorded) {
		t.Errorf("collection ran again inside the interval: %v then %v",
			recorded, again)
	}

	gc.Stop()
}

// TestGarbageCollector_ErrorPaths covers the failure branches: an
// unreachable datastore, probe configs that cannot be loaded, a probe
// whose table does not exist, and a completion that cannot be recorded.
// Each must be reported rather than swallowed, because run() relies on
// the error to back off instead of spinning.
func TestGarbageCollector_ErrorPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("probe with an empty name is skipped", func(t *testing.T) {
		gc := NewGarbageCollector(nil)
		// getProbeTableName maps an empty probe name to no table, and
		// the guard must return before the connection is touched, so a
		// nil connection here is deliberate.
		dropped, err := gc.collectGarbageForProbe(ctx, nil,
			&probes.ProbeConfig{Name: ""})
		if err != nil || dropped != 0 {
			t.Errorf("collectGarbageForProbe() = (%d, %v), want (0, nil)",
				dropped, err)
		}
	})

	t.Run("probe whose metrics table is missing is a no-op", func(t *testing.T) {
		ds, cleanup := gcTestDatastore(t)
		defer cleanup()

		conn, err := ds.GetConnection()
		if err != nil {
			t.Fatalf("GetConnection: %v", err)
		}
		defer ds.ReturnConnection(conn)

		// The partition lookups match on the parent table's name, so an
		// unknown table yields no candidates rather than an error. That
		// makes a misconfigured probe name silently do nothing, which is
		// worth knowing but is not this change's problem to fix.
		gc := NewGarbageCollector(ds)
		dropped, err := gc.collectGarbageForProbe(ctx, conn,
			&probes.ProbeConfig{Name: "no_such_metric_437", RetentionDays: 7})
		if err != nil || dropped != 0 {
			t.Errorf("collectGarbageForProbe() = (%d, %v), want (0, nil)",
				dropped, err)
		}
	})

	t.Run("a probe with no table does not abandon the pass", func(t *testing.T) {
		ds, cleanup := gcTestDatastore(t)
		defer cleanup()

		conn, err := ds.GetConnection()
		if err != nil {
			t.Fatalf("GetConnection: %v", err)
		}
		if _, err := conn.Exec(ctx, `
            INSERT INTO probe_configs (connection_id, is_enabled, name,
                description, collection_interval_seconds, retention_days)
            VALUES (NULL, TRUE, 'no_such_metric_437',
                'Probe with no metrics table, for error-path testing', 60, 7)
        `); err != nil {
			ds.ReturnConnection(conn)
			t.Fatalf("seeding a broken probe config: %v", err)
		}
		ds.ReturnConnection(conn)

		// The pass carries on past the unusable probe and still
		// records a completion.
		gc := NewGarbageCollector(ds)
		if err := gc.collectGarbage(ctx); err != nil {
			t.Errorf("collectGarbage() = %v, want nil despite one bad probe", err)
		}
		if _, found, err := gc.lastRun(ctx); err != nil || !found {
			t.Errorf("completion not recorded: found=%v err=%v", found, err)
		}
	})

	t.Run("unloadable probe configs report an error", func(t *testing.T) {
		ds, cleanup := gcTestDatastore(t)
		defer cleanup()

		conn, err := ds.GetConnection()
		if err != nil {
			t.Fatalf("GetConnection: %v", err)
		}
		if _, err := conn.Exec(ctx, "DROP TABLE probe_configs CASCADE"); err != nil {
			ds.ReturnConnection(conn)
			t.Fatalf("dropping probe_configs: %v", err)
		}
		ds.ReturnConnection(conn)

		gc := NewGarbageCollector(ds)
		if err := gc.collectGarbage(ctx); err == nil {
			t.Error("collectGarbage() returned no error with probe_configs missing")
		}
	})

	t.Run("unrecordable completion reports an error", func(t *testing.T) {
		ds, cleanup := gcTestDatastore(t)
		defer cleanup()

		conn, err := ds.GetConnection()
		if err != nil {
			t.Fatalf("GetConnection: %v", err)
		}
		if _, err := conn.Exec(ctx, "DROP TABLE maintenance_runs"); err != nil {
			ds.ReturnConnection(conn)
			t.Fatalf("dropping maintenance_runs: %v", err)
		}
		ds.ReturnConnection(conn)

		gc := NewGarbageCollector(ds)
		if err := gc.collectGarbage(ctx); err == nil {
			t.Error("collectGarbage() returned no error with maintenance_runs missing")
		}

		// And the read side must surface the failure too, so that
		// nextRunDelay treats collection as due rather than skipping it.
		if _, _, err := gc.lastRun(ctx); err == nil {
			t.Error("lastRun() returned no error with maintenance_runs missing")
		}
	})

	t.Run("closed datastore reports an error", func(t *testing.T) {
		ds, cleanup := gcTestDatastore(t)
		defer cleanup()

		ds.Close()

		gc := NewGarbageCollector(ds)
		if _, _, err := gc.lastRun(ctx); err == nil {
			t.Error("lastRun() returned no error against a closed datastore")
		}
		if err := gc.collectGarbage(ctx); err == nil {
			t.Error("collectGarbage() returned no error against a closed datastore")
		}
	})
}

// TestGarbageCollector_RunExitsOnCancelWhileWaiting covers the context
// branch of the delay select, which is the path a collector takes when
// the process is shutting down mid-cycle rather than at startup.
func TestGarbageCollector_RunExitsOnCancelWhileWaiting(t *testing.T) {
	ds, cleanup := gcTestDatastore(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gc := NewGarbageCollector(ds)
	// A long grace period parks the loop in the delay select, so the
	// cancellation lands there rather than on the pre-read check.
	gc.startupGrace = time.Hour

	gc.wg.Add(1)
	go gc.run(ctx)

	// Give the loop time to reach the select before canceling.
	time.Sleep(200 * time.Millisecond)
	cancel()

	done := make(chan struct{})
	go func() {
		gc.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not exit when the context was canceled mid-wait")
	}
}
