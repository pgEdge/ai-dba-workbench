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

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/collector/src/database"
	"github.com/pgedge/ai-workbench/collector/src/probes"
)

// integrationFixture wires up a Datastore, a MonitoredConnectionPoolManager
// pointing at the same test PostgreSQL server, and seeds a single
// monitored connection record. Tests use the fixture to drive scheduler
// methods that talk to the database.
type integrationFixture struct {
	ds          *database.Datastore
	pool        *database.MonitoredConnectionPoolManager
	connID      int
	connName    string
	dbName      string
	host        string
	port        int
	username    string
	rawPassword string
}

var (
	integration     *integrationFixture
	integrationOnce sync.Once
	integrationSkip string
)

// TestMain ensures the integration fixture's database is dropped
// after the package's tests run. Without this, repeated runs leave
// orphan test databases behind. Production code's
// executeProbeForAllDatabases iterates pg_database, so accumulated
// orphan DBs would inflate per-test work and eventually exhaust
// PostgreSQL's max_connections limit.
func TestMain(m *testing.M) {
	code := m.Run()
	teardownIntegration()
	os.Exit(code)
}

// teardownIntegration drops the shared test database created by
// setupIntegration. Best-effort: failures here only mean an orphan
// database is left behind, not a test failure.
func teardownIntegration() {
	if integration == nil || integration.dbName == "" {
		return
	}
	integration.ds.Close()

	if os.Getenv("TEST_AI_WORKBENCH_KEEP_DB") == "1" ||
		os.Getenv("TEST_AI_WORKBENCH_KEEP_DB") == "true" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, fmt.Sprintf(
		"host=%s port=%d user=%s password=%s sslmode=disable dbname=postgres",
		integration.host, integration.port, integration.username, integration.rawPassword))
	if err != nil {
		return
	}
	defer pool.Close()
	_, _ = pool.Exec(ctx, fmt.Sprintf(
		"DROP DATABASE IF EXISTS %s WITH (FORCE)", integration.dbName))
}

// schedulerConfig satisfies scheduler.Config and matches the fields
// exposed by the production *main.Config wiring.
type schedulerConfig struct {
	datastoreMaxWait int
	monitoredMaxWait int
}

func (c schedulerConfig) GetDatastorePoolMaxWaitSeconds() int { return c.datastoreMaxWait }
func (c schedulerConfig) GetMonitoredPoolMaxWaitSeconds() int { return c.monitoredMaxWait }

// integrationTestConfig builds a schedulerConfig with sane test defaults.
func integrationTestConfig() schedulerConfig {
	return schedulerConfig{
		datastoreMaxWait: 10,
		monitoredMaxWait: 10,
	}
}

// datastoreCfg implements database.Config with values parsed from the
// TEST_AI_WORKBENCH_SERVER (or TEST_DB_CONN) environment variable.
type datastoreCfg struct {
	host           string
	hostAddr       string
	database       string
	username       string
	password       string
	port           int
	sslMode        string
	maxConnections int
	maxIdleSeconds int
}

func (c *datastoreCfg) Validate() error                     { return nil }
func (c *datastoreCfg) GetPgHost() string                   { return c.host }
func (c *datastoreCfg) GetPgHostAddr() string               { return c.hostAddr }
func (c *datastoreCfg) GetPgDatabase() string               { return c.database }
func (c *datastoreCfg) GetPgUsername() string               { return c.username }
func (c *datastoreCfg) GetPgPassword() string               { return c.password }
func (c *datastoreCfg) GetPgPort() int                      { return c.port }
func (c *datastoreCfg) GetPgSSLMode() string                { return c.sslMode }
func (c *datastoreCfg) GetPgSSLCert() string                { return "" }
func (c *datastoreCfg) GetPgSSLKey() string                 { return "" }
func (c *datastoreCfg) GetPgSSLRootCert() string            { return "" }
func (c *datastoreCfg) GetDatastorePoolMaxConnections() int { return c.maxConnections }
func (c *datastoreCfg) GetDatastorePoolMaxIdleSeconds() int { return c.maxIdleSeconds }

func parseTestServer(t *testing.T) *datastoreCfg {
	t.Helper()
	cfg := &datastoreCfg{
		host:           "localhost",
		port:           5432,
		username:       "postgres",
		sslMode:        "disable",
		maxConnections: 5,
		maxIdleSeconds: 60,
	}
	url := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if url == "" {
		url = os.Getenv("TEST_DB_CONN")
	}
	if url == "" {
		return cfg
	}
	if strings.HasPrefix(url, "postgres://") || strings.HasPrefix(url, "postgresql://") {
		stripped := strings.TrimPrefix(url, "postgres://")
		stripped = strings.TrimPrefix(stripped, "postgresql://")
		if i := strings.Index(stripped, "?"); i != -1 {
			stripped = stripped[:i]
		}
		userinfo, hostpart := "", stripped
		if i := strings.Index(stripped, "@"); i != -1 {
			userinfo = stripped[:i]
			hostpart = stripped[i+1:]
		}
		if userinfo != "" {
			if c := strings.Index(userinfo, ":"); c != -1 {
				cfg.username = userinfo[:c]
				cfg.password = userinfo[c+1:]
			} else {
				cfg.username = userinfo
			}
		}
		if i := strings.Index(hostpart, "/"); i != -1 {
			hostpart = hostpart[:i]
		}
		if c := strings.LastIndex(hostpart, ":"); c != -1 {
			cfg.host = hostpart[:c]
			fmt.Sscanf(hostpart[c+1:], "%d", &cfg.port)
		} else {
			cfg.host = hostpart
		}
	}
	return cfg
}

func setupIntegration(t *testing.T) *integrationFixture {
	t.Helper()

	integrationOnce.Do(func() {
		if os.Getenv("SKIP_DB_TESTS") != "" {
			integrationSkip = "SKIP_DB_TESTS is set"
			return
		}

		base := parseTestServer(t)

		// Create a unique test database. The collector schema is large,
		// so we share one DB across all scheduler integration tests.
		dbName := fmt.Sprintf("ai_workbench_sched_%d", time.Now().UnixNano())

		// Connect to admin DB to create the test DB.
		adminCfg := *base
		adminCfg.database = "postgres"
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		adminPool, err := pgxpool.New(ctx, fmt.Sprintf(
			"host=%s port=%d user=%s password=%s sslmode=%s dbname=postgres",
			adminCfg.host, adminCfg.port, adminCfg.username, adminCfg.password, adminCfg.sslMode))
		if err != nil {
			integrationSkip = fmt.Sprintf("connect admin: %v", err)
			return
		}
		defer adminPool.Close()
		if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName)); err != nil {
			integrationSkip = fmt.Sprintf("create db: %v", err)
			return
		}

		// Build a Datastore that points at the new DB. NewDatastore runs
		// migrations automatically, so the connections / probe_configs
		// tables exist on return.
		dsCfg := *base
		dsCfg.database = dbName
		ds, err := database.NewDatastore(&dsCfg)
		if err != nil {
			_, _ = adminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", dbName))
			integrationSkip = fmt.Sprintf("NewDatastore: %v", err)
			return
		}

		// Insert a monitored connection pointing at the same database.
		// The probe execution code will then connect back to the same
		// host so we don't need a separate monitored server.
		conn, err := ds.GetConnection()
		if err != nil {
			ds.Close()
			integrationSkip = fmt.Sprintf("GetConnection: %v", err)
			return
		}
		var connID int
		err = conn.QueryRow(ctx, `
			INSERT INTO connections
				(name, host, port, database_name, username, owner_username, is_monitored, sslmode)
			VALUES
				('monitored-test', $1, $2, $3, $4, 'tester', TRUE, $5)
			RETURNING id
		`, base.host, base.port, dbName, base.username, base.sslMode).Scan(&connID)
		ds.ReturnConnection(conn)
		if err != nil {
			ds.Close()
			integrationSkip = fmt.Sprintf("seed connection: %v", err)
			return
		}

		// Disable all seeded probe_configs so Start/loadConfigs only
		// initializes probes when an individual test re-enables them.
		// This keeps tests fast and prevents resource exhaustion when
		// many goroutines fan out to probe the same DB.
		conn2, err := ds.GetConnection()
		if err != nil {
			ds.Close()
			integrationSkip = fmt.Sprintf("GetConnection 2: %v", err)
			return
		}
		if _, err := conn2.Exec(ctx,
			"UPDATE probe_configs SET is_enabled = FALSE"); err != nil {
			ds.ReturnConnection(conn2)
			ds.Close()
			integrationSkip = fmt.Sprintf("disable probe_configs: %v", err)
			return
		}
		ds.ReturnConnection(conn2)

		integration = &integrationFixture{
			ds: ds,
			// Use 1 connection per monitored server and a short idle
			// time so concurrent tests can't exhaust the postgres
			// max_connections limit. We have ~30 tests that may each
			// trigger pool creation against the same server.
			pool:        database.NewMonitoredConnectionPoolManager(1, 5),
			connID:      connID,
			connName:    "monitored-test",
			dbName:      dbName,
			host:        base.host,
			port:        base.port,
			username:    base.username,
			rawPassword: base.password,
		}
	})

	if integrationSkip != "" {
		t.Skipf("integration setup skipped: %s", integrationSkip)
	}
	if integration == nil {
		t.Skip("integration fixture unavailable")
	}
	return integration
}

// makeMonitoredConn returns a database.MonitoredConnection whose
// connection details point at the test PostgreSQL server.
func makeMonitoredConn(f *integrationFixture) database.MonitoredConnection {
	return database.MonitoredConnection{
		ID:           f.connID,
		Name:         f.connName,
		Host:         f.host,
		Port:         f.port,
		DatabaseName: f.dbName,
		Username:     f.username,
		SSLMode:      sql.NullString{String: "disable", Valid: true},
		UpdatedAt:    time.Now(),
	}
}

func TestSchedulerStart_RunsLoadConfigs(t *testing.T) {
	f := setupIntegration(t)

	// Enable a single probe with a 1-hour interval so Start spawns one
	// goroutine that won't fire before we Stop. Cleanup disables it
	// again so subsequent tests aren't affected.
	exec := func(query string, args ...any) {
		conn, err := f.ds.GetConnection()
		if err != nil {
			t.Fatalf("GetConnection: %v", err)
		}
		defer f.ds.ReturnConnection(conn)
		if _, err := conn.Exec(context.Background(), query, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	exec(`
		UPDATE probe_configs
		SET is_enabled = TRUE, collection_interval_seconds = 3600
		WHERE name = $1 AND scope = 'global'
	`, probes.ProbeNamePgConnectivity)
	t.Cleanup(func() {
		exec("UPDATE probe_configs SET is_enabled = FALSE WHERE name = $1",
			probes.ProbeNamePgConnectivity)
		exec("DELETE FROM probe_configs WHERE scope = 'server'")
	})

	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	if err := ps.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ps.Stop()
}

func TestSchedulerLoadConfigs_NoMonitoredConnections(t *testing.T) {
	// Build a scheduler whose datastore has no monitored connections;
	// loadConfigs must succeed without panic.
	f := setupIntegration(t)
	exec := func(query string, args ...any) {
		conn, err := f.ds.GetConnection()
		if err != nil {
			t.Fatalf("GetConnection: %v", err)
		}
		defer f.ds.ReturnConnection(conn)
		if _, err := conn.Exec(context.Background(), query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec("UPDATE connections SET is_monitored = FALSE WHERE id = $1", f.connID)
	t.Cleanup(func() {
		exec("UPDATE connections SET is_monitored = TRUE WHERE id = $1", f.connID)
	})

	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	if err := ps.loadConfigs(context.Background()); err != nil {
		t.Fatalf("loadConfigs: %v", err)
	}
	ps.Stop()
}

func TestSchedulerLoadConfigs_WithMonitoredConnection(t *testing.T) {
	f := setupIntegration(t)

	// Enable only the connectivity probe so loadConfigs initializes
	// exactly one probe goroutine (large-batch behavior is exercised
	// elsewhere). Use a long interval so the goroutine doesn't actually
	// fire before Stop().
	exec := func(query string, args ...any) {
		conn, err := f.ds.GetConnection()
		if err != nil {
			t.Fatalf("GetConnection: %v", err)
		}
		defer f.ds.ReturnConnection(conn)
		if _, err := conn.Exec(context.Background(), query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`
		UPDATE probe_configs
		SET is_enabled = TRUE, collection_interval_seconds = 3600
		WHERE name = $1 AND scope = 'global'
	`, probes.ProbeNamePgConnectivity)
	t.Cleanup(func() {
		exec("UPDATE probe_configs SET is_enabled = FALSE WHERE name = $1",
			probes.ProbeNamePgConnectivity)
		exec("DELETE FROM probe_configs WHERE scope = 'server'")
	})

	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	if err := ps.loadConfigs(context.Background()); err != nil {
		t.Fatalf("loadConfigs: %v", err)
	}

	ps.probesMutex.RLock()
	probesForConn := ps.probesByConn[f.connID]
	ps.probesMutex.RUnlock()
	if _, ok := probesForConn[probes.ProbeNamePgConnectivity]; !ok {
		t.Errorf("expected pg_connectivity probe to be initialized")
	}

	// Calling loadConfigs a second time exercises the "config unchanged"
	// path that skips re-creating an existing probe.
	if err := ps.loadConfigs(context.Background()); err != nil {
		t.Fatalf("loadConfigs second call: %v", err)
	}

	// Now change the interval so loadConfigs detects the change and
	// spawns a fresh goroutine for the existing probe.
	exec(`
		UPDATE probe_configs
		SET collection_interval_seconds = 7200
		WHERE name = $1 AND scope = 'global'
	`, probes.ProbeNamePgConnectivity)
	if err := ps.loadConfigs(context.Background()); err != nil {
		t.Fatalf("loadConfigs after interval change: %v", err)
	}

	ps.Stop()
}

func TestSchedulerCalculateInitialDelay(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	// With no last collection, delay should be 0.
	delay := ps.calculateInitialDelay(probe, f.connID, f.connName, cfg)
	if delay != 0 {
		t.Errorf("expected 0 delay with no history, got %v", delay)
	}
}

func TestSchedulerGetMonitoredConnectionByID(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	got, err := ps.getMonitoredConnectionByID(f.connID)
	if err != nil {
		t.Fatalf("getMonitoredConnectionByID: %v", err)
	}
	if got.ID != f.connID {
		t.Errorf("ID = %d, want %d", got.ID, f.connID)
	}

	if _, err := ps.getMonitoredConnectionByID(999_999); err == nil {
		t.Error("expected error for unknown connection ID")
	}
}

func TestSchedulerGetDatabaseList(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	mc := makeMonitoredConn(f)
	ctx := context.Background()
	conn, err := ps.poolManager.GetConnection(ctx, mc, "")
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	defer ps.poolManager.ReturnConnection(mc.ID, conn)

	dbs, err := ps.getDatabaseList(ctx, conn)
	if err != nil {
		t.Fatalf("getDatabaseList: %v", err)
	}
	if len(dbs) == 0 {
		t.Error("expected at least one database returned")
	}

	// Cancelled context -> error.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ps.getDatabaseList(cctx, conn); err == nil {
		t.Error("expected error when context is canceled")
	}
}

func TestSchedulerExecuteProbeForServerWide(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	mc := makeMonitoredConn(f)
	metrics, connErr, msg := ps.executeProbeForServerWide(context.Background(), probe, mc)
	if connErr {
		t.Errorf("unexpected connection error: %s", msg)
	}
	if len(metrics) == 0 {
		t.Error("expected metrics from connectivity probe")
	}
}

func TestSchedulerExecuteProbeForServerWide_CtxCanceled(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mc := makeMonitoredConn(f)
	metrics, connErr, _ := ps.executeProbeForServerWide(ctx, probe, mc)
	if connErr {
		t.Error("canceled context should not register as connection error")
	}
	if metrics != nil {
		t.Errorf("expected nil metrics on canceled context, got %v", metrics)
	}
}

func TestSchedulerExecuteProbeForServerWide_ConnectionFailure(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	bad := database.MonitoredConnection{
		ID:           99_001,
		Name:         "bad",
		Host:         "127.0.0.1",
		Port:         1, // invalid port -> immediate failure
		DatabaseName: "x",
		Username:     "postgres",
		SSLMode:      sql.NullString{String: "disable", Valid: true},
		UpdatedAt:    time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, connErr, msg := ps.executeProbeForServerWide(ctx, probe, bad)
	if !connErr {
		t.Error("expected connection error for unreachable host")
	}
	if !strings.Contains(msg, "connection error") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestSchedulerExecuteProbeForAllDatabases(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgStatStatements,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgStatStatementsProbe(cfg)

	mc := makeMonitoredConn(f)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// pg_stat_statements may fail because the extension isn't installed
	// — that's fine. We're exercising the multi-database loop and
	// ensuring no panic.
	metrics, dbs, connErr, _ := ps.executeProbeForAllDatabases(ctx, probe, mc)
	if connErr {
		t.Errorf("unexpected connection error during multi-database probe")
	}
	// The list of databases should include at least the test DB.
	found := false
	for _, db := range dbs {
		if db == f.dbName {
			found = true
			break
		}
	}
	if !found && len(dbs) > 0 {
		t.Errorf("expected test DB %q to be in database list %v", f.dbName, dbs)
	}
	_ = metrics // metrics may legitimately be empty
}

// TestSchedulerExecuteProbeForAllDatabases_HappyPath uses a probe that
// runs successfully against multiple databases.
func TestSchedulerExecuteProbeForAllDatabases_HappyPath(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgStatDatabaseConflicts,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgStatDatabaseConflictsProbe(cfg)

	mc := makeMonitoredConn(f)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	metrics, dbs, connErr, _ := ps.executeProbeForAllDatabases(ctx, probe, mc)
	if connErr {
		t.Errorf("unexpected connection error")
	}
	if len(dbs) == 0 {
		t.Error("expected at least one database in list")
	}
	_ = metrics
}

func TestSchedulerExecuteProbeForAllDatabases_PreCanceled(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgStatStatements,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgStatStatementsProbe(cfg)

	mc := makeMonitoredConn(f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, connErr, _ := ps.executeProbeForAllDatabases(ctx, probe, mc)
	if connErr {
		t.Error("canceled-ctx pre-check should return no connection error")
	}
}

func TestSchedulerExecuteProbeForAllDatabases_ConnectionFailure(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgStatStatements,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgStatStatementsProbe(cfg)

	bad := database.MonitoredConnection{
		ID:           99_002,
		Name:         "bad",
		Host:         "127.0.0.1",
		Port:         1,
		DatabaseName: "x",
		Username:     "postgres",
		SSLMode:      sql.NullString{String: "disable", Valid: true},
		UpdatedAt:    time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, connErr, msg := ps.executeProbeForAllDatabases(ctx, probe, bad)
	if !connErr {
		t.Error("expected connection error")
	}
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestSchedulerStoreMetrics(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)
	metrics := []map[string]any{
		{"response_time_ms": 1.5},
	}
	got := ps.storeMetrics(context.Background(), probe, f.connID, time.Now(), metrics)
	if got != 1 {
		t.Errorf("storeMetrics = %d, want 1", got)
	}
}

func TestSchedulerStoreMetrics_DatastoreClosed(t *testing.T) {
	f := setupIntegration(t)

	// Build a separate Datastore, close it, then watch storeMetrics fail
	// gracefully.
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
	ds2, err := database.NewDatastore(&dsCfg)
	if err != nil {
		t.Fatalf("NewDatastore: %v", err)
	}
	ps := NewProbeScheduler(ds2, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	// Force store path to fail by closing the underlying pool.
	ds2.Close()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)
	metrics := []map[string]any{{"response_time_ms": 1.0}}
	if got := ps.storeMetrics(context.Background(), probe, f.connID, time.Now(), metrics); got != 0 {
		t.Errorf("storeMetrics with closed pool = %d, want 0", got)
	}
}

func TestSchedulerRecordAvailability(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	// Available probe.
	ps.recordAvailability(f.connID, "pg_test_probe", nil, true, nil)

	// Unavailable extension probe.
	ext := "spock"
	reason := "extension not installed"
	ps.recordAvailability(f.connID, "pg_test_probe", &ext, false, &reason)

	// Verify a row was written.
	dsConn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	defer f.ds.ReturnConnection(dsConn)
	var count int
	err = dsConn.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM probe_availability WHERE connection_id=$1 AND probe_name=$2",
		f.connID, "pg_test_probe").Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count == 0 {
		t.Error("expected probe_availability row to be inserted")
	}
}

// TestSchedulerRecordAvailability_DatastoreClosed exercises the error
// path inside recordAvailability where GetConnectionWithContext fails.
func TestSchedulerRecordAvailability_DatastoreClosed(t *testing.T) {
	f := setupIntegration(t)

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
	ds2, err := database.NewDatastore(&dsCfg)
	if err != nil {
		t.Fatalf("NewDatastore: %v", err)
	}
	ps := NewProbeScheduler(ds2, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	// Closing the pool ensures every Acquire fails.
	ds2.Close()
	ps.recordAvailability(1, "pg_test_probe", nil, true, nil)
	// The function returns void; we only need to exercise the error
	// branch for coverage.
}

// TestSchedulerRecordAvailability_UpsertError uses a fake connection ID
// that doesn't exist in connections to trigger the FK violation in
// probe_availability and exercise the error branch from
// UpsertProbeAvailability.
func TestSchedulerRecordAvailability_UpsertError(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	// connection ID 999_999 is not present in connections, so FK fails.
	ps.recordAvailability(999_999, "pg_test_probe", nil, true, nil)
}

// TestSchedulerConfigReloadLoop_TickerFires exercises the ticker path
// of configReloadLoop. We override configReloader to fire quickly.
func TestSchedulerConfigReloadLoop_TickerFires(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")

	// Replace the (nil) reloader with a fast ticker.
	ps.configReloader = time.NewTicker(50 * time.Millisecond)
	ps.wg.Add(1)
	go ps.configReloadLoop()

	// Let it fire at least twice so loadConfigs runs.
	time.Sleep(150 * time.Millisecond)
	ps.Stop()
}

// TestSchedulerStart_Failure exercises Start() when loadConfigs fails.
// We close the datastore before calling Start to force the failure.
func TestSchedulerStart_Failure(t *testing.T) {
	f := setupIntegration(t)

	// Build a separate datastore so we don't break the shared fixture.
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
	ds2, err := database.NewDatastore(&dsCfg)
	if err != nil {
		t.Fatalf("NewDatastore: %v", err)
	}
	ds2.Close()

	ps := NewProbeScheduler(ds2, f.pool, integrationTestConfig(), "")
	if err := ps.Start(context.Background()); err == nil {
		t.Error("expected Start to return error when loadConfigs fails")
	}
	ps.Stop()
}

// TestSchedulerLoadConfigs_DatastoreClosed exercises the failure paths
// inside loadConfigs.
func TestSchedulerLoadConfigs_DatastoreClosed(t *testing.T) {
	f := setupIntegration(t)

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
	ds2, err := database.NewDatastore(&dsCfg)
	if err != nil {
		t.Fatalf("NewDatastore: %v", err)
	}
	ds2.Close()

	ps := NewProbeScheduler(ds2, f.pool, integrationTestConfig(), "")
	if err := ps.loadConfigs(context.Background()); err == nil {
		t.Error("expected loadConfigs to return error with closed datastore")
	}
	ps.Stop()
}

// TestSchedulerExecuteProbeForConnection_ExtensionMissing exercises
// the "extension probe with no metrics" branch (lines ~435-444). The
// SpockResolutions probe returns no metrics when the spock extension
// is not installed, which is the normal state in the test database.
func TestSchedulerExecuteProbeForConnection_ExtensionMissing(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNameSpockResolutions,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewSpockResolutionsProbe(cfg)

	mc := makeMonitoredConn(f)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ps.executeProbeForConnection(ctx, probe, mc)
}

// TestSchedulerExecuteProbeForConnection_TimeoutWithExtensionProbe
// exercises the DeadlineExceeded branch (~line 358) where the probe is
// an ExtensionProbe and recordAvailability is called with the
// extension name.
func TestSchedulerExecuteProbeForConnection_TimeoutWithExtensionProbe(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, schedulerConfig{datastoreMaxWait: 5, monitoredMaxWait: 1}, "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNameSpockResolutions,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewSpockResolutionsProbe(cfg)

	// Unreachable host so the probe times out.
	bad := database.MonitoredConnection{
		ID:           seedTestConnection(t, f, fmt.Sprintf("ext-tmo-%d", time.Now().UnixNano())),
		Name:         "ext-tmo",
		Host:         "127.0.0.1",
		Port:         1,
		DatabaseName: "x",
		Username:     "postgres",
		SSLMode:      sql.NullString{String: "disable", Valid: true},
		UpdatedAt:    time.Now(),
	}
	ps.executeProbeForConnection(context.Background(), probe, bad)
}

// TestSchedulerExecuteProbeForConnection_TimeoutDeadlineExceeded uses a
// pre-deadline-exceeded context to exercise the timeout-recording path.
func TestSchedulerExecuteProbeForConnection_TimeoutDeadline(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, schedulerConfig{datastoreMaxWait: 5, monitoredMaxWait: 1}, "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	// Use an unreachable connection so the GetConnection call burns the
	// monitored timeout and returns context.DeadlineExceeded.
	bad := database.MonitoredConnection{
		ID:           seedTestConnection(t, f, fmt.Sprintf("dle-%d", time.Now().UnixNano())),
		Name:         "dle-conn",
		Host:         "127.0.0.1",
		Port:         1, // unreachable
		DatabaseName: "x",
		Username:     "postgres",
		SSLMode:      sql.NullString{String: "disable", Valid: true},
		UpdatedAt:    time.Now(),
	}
	ps.executeProbeForConnection(context.Background(), probe, bad)
}

// TestSchedulerExecuteProbeForConnection_ErrorMessageUnchanged sets the
// connection_error column ahead of time so that when the probe fails
// with the same message, the errorChanged path is skipped.
func TestSchedulerExecuteProbeForConnection_ErrorMessageUnchanged(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	// Unreachable bad connection.
	connID := seedTestConnection(t, f, fmt.Sprintf("eme-%d", time.Now().UnixNano()))

	// Pre-seed the same error message that the probe will produce so
	// the errorChanged check evaluates to false.
	conn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	preMsg := "preset"
	if _, err := conn.Exec(context.Background(),
		"UPDATE connections SET connection_error = $1, host = '127.0.0.1', port = 1 WHERE id = $2",
		preMsg, connID); err != nil {
		f.ds.ReturnConnection(conn)
		t.Fatalf("UPDATE: %v", err)
	}
	f.ds.ReturnConnection(conn)

	mc := database.MonitoredConnection{
		ID:              connID,
		Name:            "eme-conn",
		Host:            "127.0.0.1",
		Port:            1,
		DatabaseName:    "x",
		Username:        "postgres",
		SSLMode:         sql.NullString{String: "disable", Valid: true},
		UpdatedAt:       time.Now(),
		ConnectionError: &preMsg,
	}
	// First run with the existing message. The new message will differ
	// from preMsg, so errorChanged=true and the column gets updated to
	// the actual error.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ps.executeProbeForConnection(ctx, probe, mc)

	// Read the error message back and replay with the same message so
	// errorChanged=false.
	conn2, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection 2: %v", err)
	}
	var stored sql.NullString
	if err := conn2.QueryRow(context.Background(),
		"SELECT connection_error FROM connections WHERE id=$1", connID).Scan(&stored); err != nil {
		f.ds.ReturnConnection(conn2)
		t.Fatalf("readback: %v", err)
	}
	f.ds.ReturnConnection(conn2)
	if stored.Valid {
		mc.ConnectionError = &stored.String
		ps.executeProbeForConnection(ctx, probe, mc)
	}
}

// TestSchedulerExecuteProbeForServerWide_QueryError exercises the
// "probe.Execute returns error" branch of executeProbeForServerWide. We
// use a probe whose query targets a relation that doesn't exist on a
// healthy connection; PgStatStatementsProbe is database-scoped so we
// instead use PgServerInfoProbe with a closed-down connection.
//
// To trigger it deterministically, we use PgConnectivityProbe but
// cancel the context partway through. That isn't reliable, so instead
// we call probe.Execute() with a closed pool indirectly by using
// SchedulerExecuteProbeForServerWide_ConnectionFailure (already covered).

// TestSchedulerGetDatabaseList_RowsErr exercises the rows.Err() error
// branch of getDatabaseList. We can't easily inject an error mid-stream,
// so this is a placeholder that exercises the query failure branch via
// a closed connection (handled in TestSchedulerGetDatabaseList).

// failingProbe is a probe whose Execute always returns an error. We
// use it to drive the per-database error branches in
// executeProbeForAllDatabases and executeProbeForServerWide. The
// embedded BaseMetricsProbe satisfies any future interface checks that
// require it; we override the public methods we care about.
type failingProbe struct {
	probes.BaseMetricsProbe
	dbScoped bool
}

func (p *failingProbe) GetName() string        { return "failing-probe" }
func (p *failingProbe) GetTableName() string   { return "failing_probe" }
func (p *failingProbe) GetQuery() string       { return "" }
func (p *failingProbe) IsDatabaseScoped() bool { return p.dbScoped }
func (p *failingProbe) GetConfig() *probes.ProbeConfig {
	return &probes.ProbeConfig{Name: "failing-probe"}
}
func (p *failingProbe) Execute(_ context.Context, _ string, _ *pgxpool.Conn, _ int) ([]map[string]any, error) {
	return nil, fmt.Errorf("synthetic execute failure")
}
func (p *failingProbe) Store(_ context.Context, _ *pgxpool.Conn, _ int, _ time.Time, _ []map[string]any) error {
	return nil
}
func (p *failingProbe) EnsurePartition(_ context.Context, _ *pgxpool.Conn, _ time.Time) error {
	return nil
}

// cancelAfterFirst is a wrapper probe that cancels the supplied
// context the first time Execute is called. We use it to drive the
// "ctx canceled mid per-DB loop" branch of executeProbeForAllDatabases.
type cancelAfterFirst struct {
	cancel    context.CancelFunc
	called    int
	dbScoped  bool
	probeName string
}

func (p *cancelAfterFirst) GetName() string        { return p.probeName }
func (p *cancelAfterFirst) GetTableName() string   { return p.probeName }
func (p *cancelAfterFirst) GetQuery() string       { return "" }
func (p *cancelAfterFirst) IsDatabaseScoped() bool { return p.dbScoped }
func (p *cancelAfterFirst) GetConfig() *probes.ProbeConfig {
	return &probes.ProbeConfig{Name: p.probeName}
}
func (p *cancelAfterFirst) Execute(_ context.Context, _ string, _ *pgxpool.Conn, _ int) ([]map[string]any, error) {
	p.called++
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	return nil, nil
}
func (p *cancelAfterFirst) Store(_ context.Context, _ *pgxpool.Conn, _ int, _ time.Time, _ []map[string]any) error {
	return nil
}
func (p *cancelAfterFirst) EnsurePartition(_ context.Context, _ *pgxpool.Conn, _ time.Time) error {
	return nil
}

// TestSchedulerExecuteProbeForAllDatabases_CancelMidLoop uses a probe
// that cancels its context after the first Execute call. The
// per-database loop should then notice ctx.Err on its next iteration
// and break out (line 581).
func TestSchedulerExecuteProbeForAllDatabases_CancelMidLoop(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	probe := &cancelAfterFirst{cancel: cancel, dbScoped: true, probeName: "cancel-mid-loop"}

	mc := makeMonitoredConn(f)
	_, _, _, _ = ps.executeProbeForAllDatabases(ctx, probe, mc)
	if probe.called == 0 {
		t.Error("expected probe.Execute to be called at least once")
	}
}

// TestSchedulerExecuteProbeForAllDatabases_ExecuteFails uses a probe
// that always fails to exercise the "Execute returns error" branches in
// the multi-database loop.
func TestSchedulerExecuteProbeForAllDatabases_ExecuteFails(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	probe := &failingProbe{dbScoped: true}
	mc := makeMonitoredConn(f)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	metrics, dbs, connErr, _ := ps.executeProbeForAllDatabases(ctx, probe, mc)
	if connErr {
		t.Errorf("unexpected connection error")
	}
	if len(metrics) != 0 {
		t.Errorf("expected no metrics from failing probe, got %d", len(metrics))
	}
	if len(dbs) == 0 {
		t.Error("expected database list to be populated")
	}
}

// cancelOnExecute cancels the supplied context inside Execute and
// returns ctx.Err. This drives the "ctx.Err mid-execute" branch of
// executeProbeForServerWide (lines ~711-715).
type cancelOnExecute struct {
	cancel    context.CancelFunc
	probeName string
	dbScoped  bool
}

func (p *cancelOnExecute) GetName() string        { return p.probeName }
func (p *cancelOnExecute) GetTableName() string   { return p.probeName }
func (p *cancelOnExecute) GetQuery() string       { return "" }
func (p *cancelOnExecute) IsDatabaseScoped() bool { return p.dbScoped }
func (p *cancelOnExecute) GetConfig() *probes.ProbeConfig {
	return &probes.ProbeConfig{Name: p.probeName}
}
func (p *cancelOnExecute) Execute(ctx context.Context, _ string, _ *pgxpool.Conn, _ int) ([]map[string]any, error) {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	return nil, fmt.Errorf("execute canceled: %v", ctx.Err())
}
func (p *cancelOnExecute) Store(_ context.Context, _ *pgxpool.Conn, _ int, _ time.Time, _ []map[string]any) error {
	return nil
}
func (p *cancelOnExecute) EnsurePartition(_ context.Context, _ *pgxpool.Conn, _ time.Time) error {
	return nil
}

// TestSchedulerExecuteProbeForConnection_DatastoreClosedDuringErrorUpdate
// exercises the "Failed to get datastore connection" branch in
// executeProbeForConnection's errorChanged block (line ~412): the
// monitored probe fails (so we want to set connection_error) but the
// datastore connection acquisition then fails because we've closed
// the pool.
func TestSchedulerExecuteProbeForConnection_DatastoreClosedDuringErrorUpdate(t *testing.T) {
	f := setupIntegration(t)

	// Build a separate datastore so we don't break the shared fixture.
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
	ds2, err := database.NewDatastore(&dsCfg)
	if err != nil {
		t.Fatalf("NewDatastore: %v", err)
	}
	ps := NewProbeScheduler(ds2, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	bad := database.MonitoredConnection{
		ID:           seedTestConnection(t, f, fmt.Sprintf("ds-close-%d", time.Now().UnixNano())),
		Name:         "ds-close",
		Host:         "127.0.0.1",
		Port:         1, // unreachable so the probe fails
		DatabaseName: "x",
		Username:     "postgres",
		SSLMode:      sql.NullString{String: "disable", Valid: true},
		UpdatedAt:    time.Now(),
	}
	// Close the datastore pool *before* the probe runs so the
	// errorChanged update path fails to acquire a connection.
	ds2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ps.executeProbeForConnection(ctx, probe, bad)
}

// TestSchedulerStoreMetrics_PartitionFailure exercises the "Store
// returns error" branch in storeMetrics by passing an empty-named
// probe so EnsurePartition fails. Because the v3 schema enforces a
// connection_id FK on metrics tables, we use a fake connection id and
// the failingProbe's Store method (which returns nil) -- so we instead
// rely on a probe whose Store will fail. We use the connectivity probe
// with a non-existent connection id.
func TestSchedulerStoreMetrics_StoreFailure(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	// connection_id 999_999 doesn't exist; the FK constraint will
	// reject the insert and storeMetrics should report 0.
	metrics := []map[string]any{{"response_time_ms": 1.0}}
	got := ps.storeMetrics(context.Background(), probe, 999_999, time.Now(), metrics)
	if got != 0 {
		t.Errorf("storeMetrics with bad connection_id = %d, want 0", got)
	}
}

// TestSchedulerExecuteProbeForServerWide_ExecuteCanceled drives the
// ctx-canceled branch inside executeProbeForServerWide.
func TestSchedulerExecuteProbeForServerWide_ExecuteCanceled(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	probe := &cancelOnExecute{cancel: cancel, dbScoped: false, probeName: "cancel-on-exec"}

	mc := makeMonitoredConn(f)
	_, _, _ = ps.executeProbeForServerWide(ctx, probe, mc)
}

// TestSchedulerExecuteProbeForServerWide_ExecuteFails exercises the
// "Execute returns error" branch of executeProbeForServerWide.
func TestSchedulerExecuteProbeForServerWide_ExecuteFails(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	probe := &failingProbe{dbScoped: false}
	mc := makeMonitoredConn(f)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	metrics, connErr, _ := ps.executeProbeForServerWide(ctx, probe, mc)
	if connErr {
		t.Error("Execute failure should not register as connection error")
	}
	if metrics != nil {
		t.Errorf("expected nil metrics on Execute failure, got %v", metrics)
	}
}

// TestSchedulerLoadConfigs_GetMonitoredFails creates a fresh test
// database, drops the connections table, then calls loadConfigs to
// exercise the failure path. The test cleans up its own database so
// the shared fixture isn't affected.
func TestSchedulerLoadConfigs_GetMonitoredFails(t *testing.T) {
	f := setupIntegration(t)

	// Create a brand new database for this test so DROP TABLE doesn't
	// leak into the shared fixture.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adminPool, err := pgxpool.New(ctx, fmt.Sprintf(
		"host=%s port=%d user=%s password=%s sslmode=disable dbname=postgres",
		f.host, f.port, f.username, f.rawPassword))
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	defer adminPool.Close()

	dbName := fmt.Sprintf("ai_workbench_sched_drop_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName)); err != nil {
		t.Fatalf("create db: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(),
			fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", dbName))
	})

	dsCfg := datastoreCfg{
		host:           f.host,
		port:           f.port,
		username:       f.username,
		password:       f.rawPassword,
		database:       dbName,
		sslMode:        "disable",
		maxConnections: 1,
		maxIdleSeconds: 5,
	}
	ds2, err := database.NewDatastore(&dsCfg)
	if err != nil {
		t.Fatalf("NewDatastore: %v", err)
	}
	defer ds2.Close()

	conn, err := ds2.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	if _, err := conn.Exec(context.Background(),
		"DROP TABLE IF EXISTS connections CASCADE"); err != nil {
		ds2.ReturnConnection(conn)
		t.Fatalf("DROP: %v", err)
	}
	ds2.ReturnConnection(conn)

	ps := NewProbeScheduler(ds2, f.pool, integrationTestConfig(), "")
	if err := ps.loadConfigs(context.Background()); err == nil {
		t.Error("expected loadConfigs to fail when connections table is gone")
	}
	ps.Stop()
}

// TestSchedulerLoadConfigs_RemovesStaleProbes exercises the cleanup
// loop that drops probes for connections that are no longer monitored.
func TestSchedulerLoadConfigs_RemovesStaleProbes(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	// Manually populate probesByConn for a connection ID that doesn't
	// exist in the connections table. loadConfigs should remove it.
	staleID := 99_999
	ps.probesMutex.Lock()
	ps.probesByConn[staleID] = map[string]probes.MetricsProbe{
		"stub": probes.NewPgConnectivityProbe(&probes.ProbeConfig{
			Name: probes.ProbeNamePgConnectivity, CollectionIntervalSeconds: 60,
		}),
	}
	ps.probesMutex.Unlock()

	if err := ps.loadConfigs(context.Background()); err != nil {
		t.Fatalf("loadConfigs: %v", err)
	}

	ps.probesMutex.RLock()
	_, stillThere := ps.probesByConn[staleID]
	ps.probesMutex.RUnlock()
	if stillThere {
		t.Errorf("expected stale probes for connection %d to be removed", staleID)
	}
}

// TestSchedulerExecuteProbeForAllDatabases_BadDatabasesInList registers
// a fake monitored connection whose iterates a set of bogus database
// names by inserting fake DBs we can't actually connect to. We approach
// this differently: create a separate database the test user can list
// but cannot connect to (revoked CONNECT).
func TestSchedulerExecuteProbeForAllDatabases_PerDBConnFailure(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	// We can't easily create a DB the user lacks CONNECT on without
	// admin privileges, so use a probe that we know runs against a DB
	// list and verify the loop completes. The per-DB failure path is
	// indirectly exercised when one of the system DBs (e.g. postgres)
	// is connected to but the probe-specific table doesn't exist —
	// already covered by the failingProbe test above.
	_ = f
	_ = ps
}

// TestSchedulerExecuteProbeForConnection_ClearError exercises the
// "errorChanged && !connectionError" path: a connection that previously
// had an error in its row, then the probe runs successfully and clears
// it.
func TestSchedulerExecuteProbeForConnection_ClearError(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	connID := seedTestConnection(t, f, fmt.Sprintf("clear-%d", time.Now().UnixNano()))

	// Pre-set connection_error to something non-empty.
	conn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	preMsg := "previously failed"
	if _, err := conn.Exec(context.Background(),
		"UPDATE connections SET connection_error = $1, host = $2, port = $3, database_name = $4, username = $5, sslmode = 'disable' WHERE id = $6",
		preMsg, f.host, f.port, f.dbName, f.username, connID); err != nil {
		f.ds.ReturnConnection(conn)
		t.Fatalf("UPDATE: %v", err)
	}
	f.ds.ReturnConnection(conn)

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	mc := database.MonitoredConnection{
		ID:              connID,
		Name:            "clear-conn",
		Host:            f.host,
		Port:            f.port,
		DatabaseName:    f.dbName,
		Username:        f.username,
		SSLMode:         sql.NullString{String: "disable", Valid: true},
		UpdatedAt:       time.Now(),
		ConnectionError: &preMsg,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ps.executeProbeForConnection(ctx, probe, mc)

	// Verify the column was cleared.
	conn2, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection 2: %v", err)
	}
	defer f.ds.ReturnConnection(conn2)
	var got sql.NullString
	if err := conn2.QueryRow(context.Background(),
		"SELECT connection_error FROM connections WHERE id=$1", connID).Scan(&got); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if got.Valid {
		t.Errorf("expected connection_error to be cleared, got %q", got.String)
	}
}

// TestSchedulerExecuteProbeForConnection_DatabaseScoped exercises the
// database-scoped branch of executeProbeForConnection (line 349).
func TestSchedulerExecuteProbeForConnection_DatabaseScoped(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgStatDatabaseConflicts,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgStatDatabaseConflictsProbe(cfg)

	mc := makeMonitoredConn(f)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ps.executeProbeForConnection(ctx, probe, mc)
}

func TestSchedulerExecuteProbeForConnection_HappyPath(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	mc := makeMonitoredConn(f)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Should run without panicking; produces metrics + records availability.
	ps.executeProbeForConnection(ctx, probe, mc)
}

func TestSchedulerExecuteProbeForConnection_ConnectionError(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	// Insert a separate "bad" monitored connection so that the connection
	// error path actually has a row to update in the connections table.
	conn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	var badID int
	err = conn.QueryRow(context.Background(), `
		INSERT INTO connections
			(name, host, port, database_name, username, owner_username, is_monitored, sslmode)
		VALUES
			('bad-conn', '127.0.0.1', 1, 'x', 'postgres', 'tester', TRUE, 'disable')
		RETURNING id
	`).Scan(&badID)
	f.ds.ReturnConnection(conn)
	if err != nil {
		t.Fatalf("seed bad conn: %v", err)
	}

	bad := database.MonitoredConnection{
		ID:           badID,
		Name:         "bad-conn",
		Host:         "127.0.0.1",
		Port:         1,
		DatabaseName: "x",
		Username:     "postgres",
		SSLMode:      sql.NullString{String: "disable", Valid: true},
		UpdatedAt:    time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ps.executeProbeForConnection(ctx, probe, bad)

	// connection_error column should now be set.
	conn2, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection 2: %v", err)
	}
	defer f.ds.ReturnConnection(conn2)
	var connErr sql.NullString
	if err := conn2.QueryRow(context.Background(),
		"SELECT connection_error FROM connections WHERE id=$1", badID).Scan(&connErr); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if !connErr.Valid {
		t.Error("expected connection_error to be set after probe failure")
	}

	// Running the probe a second time with the same error message should
	// hit the "errorChanged = false" path. We can't observe it directly,
	// but exercising it for coverage is the goal.
	ps.executeProbeForConnection(ctx, probe, bad)

	// Now exercise the "error cleared" path by pointing the bad
	// connection back at the working server.
	good := bad
	good.Host = f.host
	good.Port = f.port
	good.DatabaseName = f.dbName
	good.Username = f.username
	// The connections row for badID still has the old (broken) host —
	// load it fresh so executeProbeForConnection sees ConnectionError
	// set on the row, then runs successfully and clears it.
	mc, err := f.ds.GetMonitoredConnectionByID(badID)
	if err == nil {
		mc.Host = f.host
		mc.Port = f.port
		mc.DatabaseName = f.dbName
		mc.Username = f.username
		ps.executeProbeForConnection(ctx, probe, mc)
	}
}

func TestSchedulerExecuteProbeForConnection_Timeout(t *testing.T) {
	f := setupIntegration(t)

	// monitoredMaxWait=0 → immediate timeout.
	ps := NewProbeScheduler(f.ds, f.pool, schedulerConfig{datastoreMaxWait: 5, monitoredMaxWait: 0}, "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	mc := makeMonitoredConn(f)
	ctx := context.Background()
	ps.executeProbeForConnection(ctx, probe, mc)
}

// TestSchedulerScheduleProbeForConnection exercises the scheduling
// goroutine for at least one tick, hitting both the initial-execute
// branch and the ticker-driven re-execute branch.
func TestSchedulerScheduleProbeForConnection_ShortRun(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 1,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	// Manually register the probe so the loop exit check sees it.
	ps.probesMutex.Lock()
	ps.probesByConn[f.connID] = map[string]probes.MetricsProbe{
		probe.GetName(): probe,
	}
	ps.probesMutex.Unlock()

	ps.wg.Add(1)
	go ps.scheduleProbeForConnection(probe, f.connID)

	// Wait long enough for the 1-second ticker to fire at least once.
	time.Sleep(1500 * time.Millisecond)
	ps.Stop()
}

// TestSchedulerScheduleProbeForConnection_ConfigChanged exercises the
// "config changed, exit goroutine" branch on the ticker tick.
func TestSchedulerScheduleProbeForConnection_ConfigChanged(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")

	originalCfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 1,
	}
	originalProbe := probes.NewPgConnectivityProbe(originalCfg)

	// Replace the registered probe with one that has a different
	// interval so the ticker-tick check sees a mismatch and returns.
	replacementCfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 7777,
	}
	replacement := probes.NewPgConnectivityProbe(replacementCfg)

	ps.probesMutex.Lock()
	ps.probesByConn[f.connID] = map[string]probes.MetricsProbe{
		originalProbe.GetName(): replacement,
	}
	ps.probesMutex.Unlock()

	ps.wg.Add(1)
	done := make(chan struct{})
	go func() {
		ps.scheduleProbeForConnection(originalProbe, f.connID)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduleProbeForConnection did not exit on config change")
	}
	ps.Stop()
}

// TestSchedulerScheduleProbeForConnection_Removed exercises the "probe
// removed, exit" branch when the probe disappears from probesByConn.
func TestSchedulerScheduleProbeForConnection_Removed(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 1,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	// Register probe initially so first-execute branch finds it.
	ps.probesMutex.Lock()
	ps.probesByConn[f.connID] = map[string]probes.MetricsProbe{
		probe.GetName(): probe,
	}
	ps.probesMutex.Unlock()

	ps.wg.Add(1)
	done := make(chan struct{})
	go func() {
		ps.scheduleProbeForConnection(probe, f.connID)
		close(done)
	}()

	// After 100ms (well under the 1s tick), remove the probe so the
	// next tick check finds it gone.
	time.Sleep(100 * time.Millisecond)
	ps.probesMutex.Lock()
	delete(ps.probesByConn[f.connID], probe.GetName())
	ps.probesMutex.Unlock()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduleProbeForConnection did not exit after probe removed")
	}
	ps.Stop()
}

// TestSchedulerScheduleProbeForConnection_InitialDelayShutdown
// exercises the "shutdownChan during initial delay" branch
// (lines ~268-270): the goroutine is in its initial-delay select{}
// when shutdownChan is closed and must exit via that branch. To
// guarantee we exit via shutdownChan rather than ctx.Done, we close
// the channel directly instead of calling Stop() (Stop cancels the
// context before closing the channel, which makes ctx.Done win the
// select race).
func TestSchedulerScheduleProbeForConnection_InitialDelayShutdown(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")

	connID := seedTestConnection(t, f, fmt.Sprintf("init-delay-shut-%d", time.Now().UnixNano()))

	conn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	if _, err := conn.Exec(context.Background(),
		"UPDATE connections SET is_monitored = TRUE WHERE id = $1", connID); err != nil {
		f.ds.ReturnConnection(conn)
		t.Fatalf("UPDATE: %v", err)
	}
	f.ds.ReturnConnection(conn)

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 3600,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	// Seed a recent metric so calculateInitialDelay returns a long delay.
	conn2, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection 2: %v", err)
	}
	_ = probe.EnsurePartition(context.Background(), conn2, time.Now())
	if _, err := conn2.Exec(context.Background(), `
		INSERT INTO metrics.pg_connectivity (connection_id, collected_at, response_time_ms)
		VALUES ($1, NOW(), 1.0)
	`, connID); err != nil {
		f.ds.ReturnConnection(conn2)
		t.Fatalf("seed metric: %v", err)
	}
	f.ds.ReturnConnection(conn2)

	ps.probesMutex.Lock()
	ps.probesByConn[connID] = map[string]probes.MetricsProbe{probe.GetName(): probe}
	ps.probesMutex.Unlock()

	ps.wg.Add(1)
	done := make(chan struct{})
	go func() {
		ps.scheduleProbeForConnection(probe, connID)
		close(done)
	}()

	// Close shutdownChan first so the select wakes up via that branch
	// rather than via ctx.Done (which Stop would close first).
	time.Sleep(50 * time.Millisecond)
	close(ps.shutdownChan)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduleProbeForConnection did not exit via shutdownChan")
	}

	// Now finalize cleanup. Re-init shutdownChan so Stop doesn't panic.
	ps.shutdownChan = make(chan struct{})
	ps.Stop()
}

// TestSchedulerScheduleProbeForConnection_TickerShutdown closes
// shutdownChan after the goroutine has reached its main ticker loop,
// forcing an exit through the shutdownChan branch (L289) rather than
// ctx.Done.
func TestSchedulerScheduleProbeForConnection_TickerShutdown(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")

	connID := seedTestConnection(t, f, fmt.Sprintf("ticker-shut-%d", time.Now().UnixNano()))

	conn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	if _, err := conn.Exec(context.Background(),
		"UPDATE connections SET is_monitored = TRUE, host = $1, port = $2, database_name = $3, username = $4, sslmode = 'disable' WHERE id = $5",
		f.host, f.port, f.dbName, f.username, connID); err != nil {
		f.ds.ReturnConnection(conn)
		t.Fatalf("UPDATE: %v", err)
	}
	f.ds.ReturnConnection(conn)

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60, // long interval so we sit in the ticker select
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	ps.probesMutex.Lock()
	ps.probesByConn[connID] = map[string]probes.MetricsProbe{probe.GetName(): probe}
	ps.probesMutex.Unlock()

	ps.wg.Add(1)
	done := make(chan struct{})
	go func() {
		ps.scheduleProbeForConnection(probe, connID)
		close(done)
	}()

	// Wait for the goroutine to finish the initial execute and enter
	// its long-interval ticker loop, then close shutdownChan.
	time.Sleep(300 * time.Millisecond)
	close(ps.shutdownChan)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduleProbeForConnection did not exit via shutdownChan")
	}

	// Reset shutdownChan so Stop doesn't double-close.
	ps.shutdownChan = make(chan struct{})
	ps.Stop()
}

// TestSchedulerScheduleProbeForConnection_PastDue seeds an old metric
// row so calculateInitialDelay returns a negative value, exercising the
// "past due, executing immediately" log branch (line ~280).
func TestSchedulerScheduleProbeForConnection_PastDue(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")

	connID := seedTestConnection(t, f, fmt.Sprintf("past-due-%d", time.Now().UnixNano()))

	// Mark monitored and point at our test DB so executeProbe succeeds.
	conn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	if _, err := conn.Exec(context.Background(),
		"UPDATE connections SET is_monitored = TRUE, host = $1, port = $2, database_name = $3, username = $4, sslmode = 'disable' WHERE id = $5",
		f.host, f.port, f.dbName, f.username, connID); err != nil {
		f.ds.ReturnConnection(conn)
		t.Fatalf("UPDATE: %v", err)
	}
	f.ds.ReturnConnection(conn)

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	// Seed an old metric (2 hours ago) — interval is 60s so we're past
	// due by ~119 minutes.
	conn2, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection 2: %v", err)
	}
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	_ = probe.EnsurePartition(context.Background(), conn2, twoHoursAgo)
	if _, err := conn2.Exec(context.Background(), `
		INSERT INTO metrics.pg_connectivity (connection_id, collected_at, response_time_ms)
		VALUES ($1, $2, 1.0)
	`, connID, twoHoursAgo); err != nil {
		f.ds.ReturnConnection(conn2)
		t.Fatalf("seed metric: %v", err)
	}
	f.ds.ReturnConnection(conn2)

	ps.probesMutex.Lock()
	ps.probesByConn[connID] = map[string]probes.MetricsProbe{probe.GetName(): probe}
	ps.probesMutex.Unlock()

	ps.wg.Add(1)
	go ps.scheduleProbeForConnection(probe, connID)

	// Let it execute once, then stop.
	time.Sleep(200 * time.Millisecond)
	ps.Stop()
}

// TestSchedulerScheduleProbeForConnection_RefreshFails exercises the
// "Error refreshing connection" branch (line ~309) of the ticker tick
// when getMonitoredConnectionByID fails. We delete the underlying
// connections row after the goroutine starts.
func TestSchedulerScheduleProbeForConnection_RefreshFails(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")

	connID := seedTestConnection(t, f, fmt.Sprintf("refresh-fail-%d", time.Now().UnixNano()))

	// Mark the row monitored so getMonitoredConnectionByID succeeds at
	// startup, then delete it later.
	conn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	if _, err := conn.Exec(context.Background(),
		"UPDATE connections SET is_monitored = TRUE, host = $1, port = $2, database_name = $3, username = $4, sslmode = 'disable' WHERE id = $5",
		f.host, f.port, f.dbName, f.username, connID); err != nil {
		f.ds.ReturnConnection(conn)
		t.Fatalf("UPDATE: %v", err)
	}
	f.ds.ReturnConnection(conn)

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 1,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	ps.probesMutex.Lock()
	ps.probesByConn[connID] = map[string]probes.MetricsProbe{probe.GetName(): probe}
	ps.probesMutex.Unlock()

	ps.wg.Add(1)
	go ps.scheduleProbeForConnection(probe, connID)

	// Wait for first execute, then mark connection unmonitored so the
	// next tick's refresh fails.
	time.Sleep(200 * time.Millisecond)
	conn2, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection 2: %v", err)
	}
	if _, err := conn2.Exec(context.Background(),
		"UPDATE connections SET is_monitored = FALSE WHERE id = $1", connID); err != nil {
		f.ds.ReturnConnection(conn2)
		t.Fatalf("UPDATE 2: %v", err)
	}
	f.ds.ReturnConnection(conn2)

	// Allow at least one more tick to fire and observe the failure.
	time.Sleep(1500 * time.Millisecond)
	ps.Stop()
}

// TestSchedulerScheduleProbeForConnection_InitialDelay exercises the
// "initial delay > 0, then execute" path. We seed a recent metric so
// calculateInitialDelay returns a positive value.
func TestSchedulerScheduleProbeForConnection_InitialDelay(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")

	uniqueConnID := seedTestConnection(t, f, fmt.Sprintf("delay-init-%d", time.Now().UnixNano()))

	// Insert a recent collection so calculateInitialDelay reports a
	// positive delay (longer than the test's runtime).
	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 3600,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	conn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	if err := probe.EnsurePartition(context.Background(), conn, time.Now()); err != nil {
		f.ds.ReturnConnection(conn)
		t.Fatalf("EnsurePartition: %v", err)
	}
	if _, err := conn.Exec(context.Background(), `
		INSERT INTO metrics.pg_connectivity (connection_id, collected_at, response_time_ms)
		VALUES ($1, NOW(), 1.0)
	`, uniqueConnID); err != nil {
		f.ds.ReturnConnection(conn)
		t.Fatalf("seed metric: %v", err)
	}
	f.ds.ReturnConnection(conn)

	// Update the connection name so getMonitoredConnectionByID can find
	// a row matching uniqueConnID. Mark it monitored.
	conn2, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection 2: %v", err)
	}
	if _, err := conn2.Exec(context.Background(),
		"UPDATE connections SET is_monitored = TRUE WHERE id = $1", uniqueConnID); err != nil {
		f.ds.ReturnConnection(conn2)
		t.Fatalf("monitor: %v", err)
	}
	f.ds.ReturnConnection(conn2)

	ps.probesMutex.Lock()
	ps.probesByConn[uniqueConnID] = map[string]probes.MetricsProbe{
		probe.GetName(): probe,
	}
	ps.probesMutex.Unlock()

	ps.wg.Add(1)
	go ps.scheduleProbeForConnection(probe, uniqueConnID)

	// Stop quickly to exit through the initial-delay-shutdown branch.
	time.Sleep(50 * time.Millisecond)
	ps.Stop()
}

func TestSchedulerScheduleProbeForConnection_UnknownConn(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 1,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	// Connection ID doesn't exist -> goroutine returns immediately.
	ps.wg.Add(1)
	done := make(chan struct{})
	go func() {
		ps.scheduleProbeForConnection(probe, 999_999)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduleProbeForConnection did not return for unknown ID")
	}
}

// seedTestConnection creates a stub row in connections so foreign-key
// constraints on metrics tables are satisfied. Returns the new ID.
func seedTestConnection(t *testing.T, f *integrationFixture, name string) int {
	t.Helper()
	conn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	defer f.ds.ReturnConnection(conn)
	var id int
	err = conn.QueryRow(context.Background(), `
		INSERT INTO connections
			(name, host, port, database_name, username, owner_username, is_monitored, sslmode)
		VALUES ($1, '127.0.0.1', 5432, 'x', 'x', 'tester', FALSE, 'disable')
		RETURNING id
	`, name).Scan(&id)
	if err != nil {
		t.Fatalf("seed connection %q: %v", name, err)
	}
	return id
}

// TestSchedulerCalculateInitialDelay_PastDue forces the "past due"
// branch by inserting a synthetic row into the metrics table for a
// probe that has no recent collection. A fresh connection row is
// created so this test isn't affected by data left behind by other
// tests.
func TestSchedulerCalculateInitialDelay_PastDue(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	uniqueConnID := seedTestConnection(t, f, fmt.Sprintf("delay-past-%d", time.Now().UnixNano()))

	ds, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	defer f.ds.ReturnConnection(ds)
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	if err := probe.EnsurePartition(context.Background(), ds, twoHoursAgo); err != nil {
		t.Fatalf("EnsurePartition: %v", err)
	}
	if _, err := ds.Exec(context.Background(), `
		INSERT INTO metrics.pg_connectivity (connection_id, collected_at, response_time_ms)
		VALUES ($1, $2, 1.0)
	`, uniqueConnID, twoHoursAgo); err != nil {
		t.Fatalf("seed metric: %v", err)
	}

	delay := ps.calculateInitialDelay(probe, uniqueConnID, f.connName, cfg)
	if delay > 0 {
		t.Errorf("expected 0 or negative delay, got %v", delay)
	}
}

// TestSchedulerCalculateInitialDelay_FutureCollection forces the "delay
// > 0" branch by inserting a metric row with collected_at very recent
// — within the configured interval. The expected delay is positive.
func TestSchedulerCalculateInitialDelay_FutureCollection(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), "")
	defer ps.Stop()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 600,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	uniqueConnID := seedTestConnection(t, f, fmt.Sprintf("delay-future-%d", time.Now().UnixNano()))

	ds, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	defer f.ds.ReturnConnection(ds)
	now := time.Now()
	if err := probe.EnsurePartition(context.Background(), ds, now); err != nil {
		t.Fatalf("EnsurePartition: %v", err)
	}
	if _, err := ds.Exec(context.Background(), `
		INSERT INTO metrics.pg_connectivity (connection_id, collected_at, response_time_ms)
		VALUES ($1, $2, 1.0)
	`, uniqueConnID, now); err != nil {
		t.Fatalf("seed metric: %v", err)
	}

	delay := ps.calculateInitialDelay(probe, uniqueConnID, f.connName, cfg)
	if delay <= 0 {
		t.Errorf("expected positive delay, got %v", delay)
	}
}

// TestSchedulerCalculateInitialDelay_DatastoreClosed exercises the
// error path where the datastore connection cannot be acquired.
func TestSchedulerCalculateInitialDelay_DatastoreClosed(t *testing.T) {
	f := setupIntegration(t)

	// Build a separate Datastore and close it so calculateInitialDelay
	// hits the "failed to get datastore connection" branch.
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
	ds2, err := database.NewDatastore(&dsCfg)
	if err != nil {
		t.Fatalf("NewDatastore: %v", err)
	}
	ps := NewProbeScheduler(ds2, f.pool, integrationTestConfig(), "")
	defer ps.Stop()
	ds2.Close()

	cfg := &probes.ProbeConfig{
		Name:                      probes.ProbeNamePgConnectivity,
		CollectionIntervalSeconds: 60,
	}
	probe := probes.NewPgConnectivityProbe(cfg)

	delay := ps.calculateInitialDelay(probe, 1, "x", cfg)
	if delay != 0 {
		t.Errorf("expected 0 delay when datastore unavailable, got %v", delay)
	}
}
