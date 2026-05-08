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
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/crypto"
)

// testMonitoredServerSecret is the server secret used by integration
// tests to encrypt the test PostgreSQL password into the
// MonitoredConnection.PasswordEncrypted field. The pool manager and
// connection-string builder receive the same secret so decryption
// succeeds end-to-end. The literal value is irrelevant; it just must
// be non-empty (crypto.EncryptPassword rejects empty secrets) and
// stable across the test process.
const testMonitoredServerSecret = "monitored-pool-test-secret"

// makeMonitoredConn builds a MonitoredConnection pointed at the test
// PostgreSQL server. It uses the database name from the schema_test
// test fixture. When the parsed test-server URL contains a password
// (typical in CI), the helper encrypts it into PasswordEncrypted using
// the supplied serverSecret so the pool manager can decrypt and
// connect. Callers that need to actually open a connection must pass
// testMonitoredServerSecret (the same secret threaded through to
// downstream pool-manager calls); empty serverSecret produces an
// unencrypted connection which is only useful when the test never
// dials the server.
func makeMonitoredConn(t *testing.T, id int, serverSecret string) MonitoredConnection {
	t.Helper()
	cfg := parseTestServerURL(t)
	mc := MonitoredConnection{
		ID:           id,
		Name:         "test-monitored",
		Host:         cfg.host,
		Port:         cfg.port,
		DatabaseName: testDBName,
		Username:     cfg.username,
		SSLMode:      sql.NullString{String: cfg.sslMode, Valid: cfg.sslMode != ""},
		UpdatedAt:    time.Now(),
	}
	if cfg.password != "" && serverSecret != "" {
		// Encrypt the configured password using the supplied secret so
		// the pool manager can decrypt and connect. crypto.EncryptPassword
		// rejects an empty serverSecret, so callers that want password
		// auth must pass a non-empty serverSecret (use
		// testMonitoredServerSecret for the typical case).
		enc, err := crypto.EncryptPassword(cfg.password, serverSecret)
		if err != nil {
			t.Fatalf("EncryptPassword: %v", err)
		}
		mc.PasswordEncrypted = sql.NullString{String: enc, Valid: true}
	}
	return mc
}

func TestNewMonitoredConnectionPoolManager(t *testing.T) {
	m := NewMonitoredConnectionPoolManager(7, 30)
	if m == nil {
		t.Fatal("got nil")
	}
	if m.GetMaxConnections() != 7 {
		t.Errorf("GetMaxConnections = %d, want 7", m.GetMaxConnections())
	}
	if m.maxIdleSeconds != 30 {
		t.Errorf("maxIdleSeconds = %d, want 30", m.maxIdleSeconds)
	}
	if m.pools == nil || m.semaphores == nil || m.versions == nil ||
		m.poolHashes == nil || m.poolUpdatedAt == nil || m.poolKeyToConnID == nil {
		t.Error("internal maps not initialized")
	}
}

func TestSetGetMaxConnections(t *testing.T) {
	m := NewMonitoredConnectionPoolManager(3, 1)

	// Setting same value: no-op path.
	m.SetMaxConnections(3)
	if got := m.GetMaxConnections(); got != 3 {
		t.Errorf("after no-op set, got %d", got)
	}

	// Update.
	m.SetMaxConnections(8)
	if got := m.GetMaxConnections(); got != 8 {
		t.Errorf("after set, got %d", got)
	}
}

func TestVersionGetSet(t *testing.T) {
	m := NewMonitoredConnectionPoolManager(1, 1)

	if v := m.GetVersion(42); v != 0 {
		t.Errorf("uninitialized GetVersion = %d, want 0", v)
	}
	m.SetVersion(42, 16)
	if v := m.GetVersion(42); v != 16 {
		t.Errorf("after SetVersion, got %d, want 16", v)
	}
}

func TestGetSemaphore_Reuse(t *testing.T) {
	m := NewMonitoredConnectionPoolManager(2, 1)

	first := m.getSemaphore(1)
	second := m.getSemaphore(1)
	if first == nil || second == nil {
		t.Fatal("getSemaphore returned nil")
	}
	if first != second {
		t.Error("getSemaphore should return same channel for same connection ID")
	}
	if cap(first) != 2 {
		t.Errorf("semaphore capacity = %d, want 2", cap(first))
	}
}

func TestAcquireReleaseSlot(t *testing.T) {
	m := NewMonitoredConnectionPoolManager(1, 1)

	ctx := context.Background()
	if err := m.acquireSlot(ctx, 1); err != nil {
		t.Fatalf("first acquireSlot error = %v", err)
	}

	// Slot is full; with canceled context we should get an error fast.
	ctx2, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := m.acquireSlot(ctx2, 1); err == nil {
		t.Error("expected acquireSlot to fail when no slots free")
	}

	m.releaseSlot(1)

	// Should now be acquirable again.
	if err := m.acquireSlot(ctx, 1); err != nil {
		t.Errorf("acquireSlot after release error = %v", err)
	}
	m.releaseSlot(1)

	// Releasing for an unknown connection ID is a no-op.
	m.releaseSlot(999)
}

func TestHashConnString_Stable(t *testing.T) {
	a := hashConnString("foo=bar")
	b := hashConnString("foo=bar")
	c := hashConnString("foo=baz")
	if a != b {
		t.Errorf("hash not stable: %q vs %q", a, b)
	}
	if a == c {
		t.Errorf("hash collision: %q", a)
	}
	if len(a) != 64 { // sha256 hex
		t.Errorf("unexpected hash length %d", len(a))
	}
}

func TestBuildMonitoredConnectionStringForDatabase_Defaults(t *testing.T) {
	conn := MonitoredConnection{
		ID:           1,
		Name:         "n",
		Host:         "host.example",
		Port:         5432,
		DatabaseName: "default-db",
		Username:     "alice",
	}
	s, err := buildMonitoredConnectionStringForDatabase(conn, "", "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	// connstring.Build emits values in single quotes (key='value').
	for _, want := range []string{
		"host='host.example'",
		"port='5432'",
		"dbname='default-db'",
		"user='alice'",
		"sslmode='prefer'",
		"connect_timeout='10'",
		"application_name='" + ApplicationName + "'",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("conn string missing %q; got %q", want, s)
		}
	}
}

func TestBuildMonitoredConnectionStringForDatabase_AllOptions(t *testing.T) {
	plain := "secret"
	enc, err := crypto.EncryptPassword(plain, "server-secret")
	if err != nil {
		t.Fatalf("EncryptPassword: %v", err)
	}
	conn := MonitoredConnection{
		ID:                1,
		Name:              "n",
		Host:              "host.example",
		HostAddr:          sql.NullString{String: "10.1.2.3", Valid: true},
		Port:              5433,
		DatabaseName:      "default-db",
		Username:          "alice",
		PasswordEncrypted: sql.NullString{String: enc, Valid: true},
		SSLMode:           sql.NullString{String: "verify-full", Valid: true},
		SSLCert:           sql.NullString{String: "/c", Valid: true},
		SSLKey:            sql.NullString{String: "/k", Valid: true},
		SSLRootCert:       sql.NullString{String: "/r", Valid: true},
	}
	s, err := buildMonitoredConnectionStringForDatabase(conn, "override-db", "server-secret")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	for _, want := range []string{
		"hostaddr='10.1.2.3'",
		"port='5433'",
		"dbname='override-db'",
		"user='alice'",
		"password='secret'",
		"sslmode='verify-full'",
		"sslcert='/c'",
		"sslkey='/k'",
		"sslrootcert='/r'",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("conn string missing %q; got %q", want, s)
		}
	}
	// host param should NOT be present when hostaddr is supplied; the
	// implementation chooses one or the other.
	if strings.Contains(s, "host='host.example'") {
		t.Errorf("did not expect host= when hostaddr is set, got %q", s)
	}
}

func TestBuildMonitoredConnectionStringForDatabase_BadPassword(t *testing.T) {
	conn := MonitoredConnection{
		ID:                1,
		Host:              "h",
		Port:              5432,
		DatabaseName:      "d",
		Username:          "u",
		PasswordEncrypted: sql.NullString{String: "not-base64-or-encrypted", Valid: true},
	}
	if _, err := buildMonitoredConnectionStringForDatabase(conn, "", "secret"); err == nil {
		t.Fatal("expected decryption failure")
	}
}

// ---- Integration tests against the live PostgreSQL test database ----

// requireSchema makes sure the test database is provisioned and migrated.
// Many monitored_pool tests don't strictly need the schema, but reusing
// the running PG server keeps tests fast.
func requireSchema(t *testing.T) {
	t.Helper()
	if testDBName == "" {
		t.Skip("test database not provisioned")
	}
}

func TestCreateMonitoredPool_Success(t *testing.T) {
	requireSchema(t)
	cfg := parseTestServerURL(t)
	cfg.database = testDBName

	conn := MonitoredConnection{
		ID:           1,
		Name:         "x",
		Host:         cfg.host,
		Port:         cfg.port,
		DatabaseName: cfg.database,
		Username:     cfg.username,
		SSLMode:      sql.NullString{String: cfg.sslMode, Valid: cfg.sslMode != ""},
	}
	// Encrypt the test server password into the connection so the
	// builder produces a complete conn string. CI's PostgreSQL service
	// requires password auth; locally a passwordless trust setup works
	// because cfg.password is empty and encryption is skipped.
	if cfg.password != "" {
		enc, err := crypto.EncryptPassword(cfg.password, testMonitoredServerSecret)
		if err != nil {
			t.Fatalf("EncryptPassword: %v", err)
		}
		conn.PasswordEncrypted = sql.NullString{String: enc, Valid: true}
	}
	connStr, err := buildMonitoredConnectionStringForDatabase(conn, "", testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	pool, err := createMonitoredPool(connStr, 2, 1)
	if err != nil {
		t.Fatalf("createMonitoredPool: %v", err)
	}
	defer pool.Close()
}

func TestCreateMonitoredPool_BadConnString(t *testing.T) {
	if _, err := createMonitoredPool("::not::a::valid::connstr", 1, 0); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestCreateMonitoredPool_PingFails(t *testing.T) {
	// Reachable parse, unreachable host -> Acquire/Ping fails.
	connStr := "host=127.0.0.1 port=1 user=postgres dbname=x sslmode=disable connect_timeout=1"
	if _, err := createMonitoredPool(connStr, 1, 0); err == nil {
		t.Fatal("expected acquire/ping failure")
	}
}

func TestCreateMonitoredPool_ClampsMaxConns(t *testing.T) {
	requireSchema(t)
	cfg := parseTestServerURL(t)

	conn := MonitoredConnection{
		ID:           1,
		Host:         cfg.host,
		Port:         cfg.port,
		DatabaseName: testDBName,
		Username:     cfg.username,
		SSLMode:      sql.NullString{String: cfg.sslMode, Valid: cfg.sslMode != ""},
	}
	// Encrypt the test server password into the connection so the
	// builder produces a complete conn string under CI (which requires
	// password auth). See TestCreateMonitoredPool_Success.
	if cfg.password != "" {
		enc, err := crypto.EncryptPassword(cfg.password, testMonitoredServerSecret)
		if err != nil {
			t.Fatalf("EncryptPassword: %v", err)
		}
		conn.PasswordEncrypted = sql.NullString{String: enc, Valid: true}
	}
	connStr, err := buildMonitoredConnectionStringForDatabase(conn, "", testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	huge := int(int64(1<<31) + 100)
	pool, err := createMonitoredPool(connStr, huge, 60)
	if err != nil {
		t.Fatalf("createMonitoredPool with huge maxConns: %v", err)
	}
	defer pool.Close()
}

func TestPoolManager_GetReturnRemoveClose(t *testing.T) {
	requireSchema(t)
	m := NewMonitoredConnectionPoolManager(2, 60)
	t.Cleanup(func() { _ = m.Close() })

	mc := makeMonitoredConn(t, 100, testMonitoredServerSecret)
	ctx := context.Background()
	c1, err := m.GetConnection(ctx, mc, testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("first GetConnection: %v", err)
	}

	// Second call should reuse the existing pool.
	c2, err := m.GetConnection(ctx, mc, testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("second GetConnection: %v", err)
	}
	m.ReturnConnection(mc.ID, c1)
	m.ReturnConnection(mc.ID, c2)

	// Version should be detectable.
	c3, err := m.GetConnection(ctx, mc, testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("third GetConnection: %v", err)
	}
	v, err := m.DetectAndCacheVersion(ctx, mc.ID, c3)
	if err != nil {
		t.Fatalf("DetectAndCacheVersion: %v", err)
	}
	if v <= 0 {
		t.Errorf("DetectAndCacheVersion returned non-positive version %d", v)
	}

	// Calling again should hit the cache (no error even with a
	// nil-equivalent conn... but pgx requires a non-nil conn for the
	// initial fetch, so reuse c3).
	v2, err := m.DetectAndCacheVersion(ctx, mc.ID, c3)
	if err != nil {
		t.Fatalf("cached DetectAndCacheVersion: %v", err)
	}
	if v2 != v {
		t.Errorf("cached version mismatch: got %d want %d", v2, v)
	}
	m.ReturnConnection(mc.ID, c3)

	// RemovePool a known connection.
	if err := m.RemovePool(mc.ID); err != nil {
		t.Fatalf("RemovePool: %v", err)
	}
	// Removing an already-absent pool is a no-op.
	if err := m.RemovePool(mc.ID); err != nil {
		t.Fatalf("RemovePool (idempotent): %v", err)
	}
}

func TestPoolManager_GetConnectionForDatabase(t *testing.T) {
	requireSchema(t)
	m := NewMonitoredConnectionPoolManager(2, 60)
	t.Cleanup(func() { _ = m.Close() })

	mc := makeMonitoredConn(t, 200, testMonitoredServerSecret)
	ctx := context.Background()

	// Same DB name as default -> still uses positive pool key.
	c, err := m.GetConnectionForDatabase(ctx, mc, "", testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("first GetConnectionForDatabase (default): %v", err)
	}
	m.ReturnConnection(mc.ID, c)

	// Different DB name -> negative pool key.
	c2, err := m.GetConnectionForDatabase(ctx, mc, testDBName, testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("GetConnectionForDatabase (named): %v", err)
	}
	m.ReturnConnection(mc.ID, c2)

	// Re-acquire to exercise the "pool exists" branch for both keys.
	c3, err := m.GetConnectionForDatabase(ctx, mc, testDBName, testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("re-GetConnectionForDatabase: %v", err)
	}
	m.ReturnConnection(mc.ID, c3)
}

func TestPoolManager_GetConnection_BuildFailure(t *testing.T) {
	m := NewMonitoredConnectionPoolManager(1, 1)
	mc := MonitoredConnection{
		ID:                1,
		Host:              "h",
		Port:              5432,
		DatabaseName:      "d",
		Username:          "u",
		PasswordEncrypted: sql.NullString{String: "junk", Valid: true},
	}
	if _, err := m.GetConnection(context.Background(), mc, "secret"); err == nil {
		t.Fatal("expected build failure to propagate")
	}
}

func TestPoolManager_GetConnection_SemaphoreExhausted(t *testing.T) {
	m := NewMonitoredConnectionPoolManager(1, 1)
	// Pre-fill the semaphore with a sentinel for ID 5.
	m.mu.Lock()
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	m.semaphores[5] = sem
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	mc := MonitoredConnection{ID: 5, Host: "h", Port: 5432, DatabaseName: "d", Username: "u"}
	if _, err := m.GetConnection(ctx, mc, ""); err == nil {
		t.Fatal("expected acquireSlot timeout")
	}
}

func TestPoolManager_GetConnection_PoolCreateFailure(t *testing.T) {
	m := NewMonitoredConnectionPoolManager(1, 1)
	// host:port that won't accept connections quickly.
	mc := MonitoredConnection{
		ID:           7,
		Host:         "127.0.0.1",
		Port:         1,
		DatabaseName: "x",
		Username:     "postgres",
		SSLMode:      sql.NullString{String: "disable", Valid: true},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := m.GetConnection(ctx, mc, ""); err == nil {
		t.Fatal("expected pool creation failure")
	}
}

func TestPoolManager_CheckConnectionUpdated(t *testing.T) {
	requireSchema(t)
	m := NewMonitoredConnectionPoolManager(2, 60)
	t.Cleanup(func() { _ = m.Close() })

	mc := makeMonitoredConn(t, 300, testMonitoredServerSecret)
	ctx := context.Background()
	c, err := m.GetConnection(ctx, mc, testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	m.ReturnConnection(mc.ID, c)

	// Same updated_at -> false.
	if changed := m.CheckConnectionUpdated(mc.ID, mc.UpdatedAt); changed {
		t.Error("expected CheckConnectionUpdated to be false for same time")
	}

	// Unknown connection -> false.
	if changed := m.CheckConnectionUpdated(999, time.Now()); changed {
		t.Error("expected false for unknown connection")
	}

	// Different timestamp -> true and pool is dropped.
	newer := mc.UpdatedAt.Add(time.Second)
	if changed := m.CheckConnectionUpdated(mc.ID, newer); !changed {
		t.Error("expected true when updated_at differs")
	}

	m.mu.RLock()
	if _, ok := m.pools[mc.ID]; ok {
		t.Error("pool should have been removed after CheckConnectionUpdated")
	}
	m.mu.RUnlock()
}

func TestPoolManager_SyncPools(t *testing.T) {
	requireSchema(t)
	m := NewMonitoredConnectionPoolManager(2, 60)
	t.Cleanup(func() { _ = m.Close() })

	mc1 := makeMonitoredConn(t, 401, testMonitoredServerSecret)
	mc2 := makeMonitoredConn(t, 402, testMonitoredServerSecret)

	// Build pools for two connections.
	for _, mc := range []MonitoredConnection{mc1, mc2} {
		c, err := m.GetConnection(context.Background(), mc, testMonitoredServerSecret)
		if err != nil {
			t.Fatalf("GetConnection(%d): %v", mc.ID, err)
		}
		m.ReturnConnection(mc.ID, c)
	}

	// Also build a database-scoped pool for mc1 (different pool key).
	c, err := m.GetConnectionForDatabase(context.Background(), mc1, testDBName, testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("GetConnectionForDatabase: %v", err)
	}
	m.ReturnConnection(mc1.ID, c)

	// Keep only mc2 active; mc1's pools should be removed.
	m.SyncPools([]int{mc2.ID})

	m.mu.RLock()
	defer m.mu.RUnlock()
	for poolKey := range m.pools {
		if m.poolKeyToConnID[poolKey] == mc1.ID {
			t.Errorf("expected mc1 (id=%d) pools removed; pool key %d remains", mc1.ID, poolKey)
		}
	}
	if _, ok := m.semaphores[mc1.ID]; ok {
		t.Errorf("expected mc1 semaphore cleared")
	}
}

func TestPoolManager_InvalidateChangedPools(t *testing.T) {
	requireSchema(t)
	m := NewMonitoredConnectionPoolManager(2, 60)
	t.Cleanup(func() { _ = m.Close() })

	mc := makeMonitoredConn(t, 500, testMonitoredServerSecret)
	c, err := m.GetConnection(context.Background(), mc, testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	m.ReturnConnection(mc.ID, c)

	// Force the stored hash to a known value so we can deterministically
	// verify both branches of the invalidation logic. The map iteration
	// order in connstring.Build means freshly built strings are not
	// guaranteed to hash the same as an earlier build.
	m.mu.Lock()
	m.poolHashes[mc.ID] = "stable-hash"
	m.mu.Unlock()

	// Calling with a connection whose current hash differs from the
	// stored hash should invalidate the pool.
	m.InvalidateChangedPools([]MonitoredConnection{mc}, testMonitoredServerSecret)
	m.mu.RLock()
	_, stillExists := m.pools[mc.ID]
	m.mu.RUnlock()
	if stillExists {
		t.Error("pool should have been invalidated when hashes differ")
	}

	// Invalidate again with no pools present is a no-op.
	m.InvalidateChangedPools([]MonitoredConnection{mc}, testMonitoredServerSecret)
}

func TestPoolManager_InvalidateChangedPools_BuildError(t *testing.T) {
	m := NewMonitoredConnectionPoolManager(1, 1)

	// Inject a fake pool entry so InvalidateChangedPools sees something.
	fakeStr := "host=fake port=5432 user=u dbname=d"
	m.mu.Lock()
	m.poolHashes[1] = hashConnString(fakeStr)
	m.poolKeyToConnID[1] = 1
	// Note: we deliberately don't insert into pools so that even after
	// invalidation the missing pool entry is gracefully handled.
	m.mu.Unlock()

	// Connection with bad encrypted password -> currentHashes won't include it.
	bad := MonitoredConnection{
		ID:                1,
		Host:              "h",
		Port:              5432,
		DatabaseName:      "d",
		Username:          "u",
		PasswordEncrypted: sql.NullString{String: "bad-cipher", Valid: true},
	}
	m.InvalidateChangedPools([]MonitoredConnection{bad}, "secret")

	// Hash entry should remain because currentHash lookup failed.
	m.mu.RLock()
	if _, ok := m.poolHashes[1]; !ok {
		t.Error("expected poolHashes entry to survive when build failed")
	}
	m.mu.RUnlock()
}

func TestPoolManager_Close_Empty(t *testing.T) {
	m := NewMonitoredConnectionPoolManager(1, 1)
	if err := m.Close(); err != nil {
		t.Fatalf("Close on empty manager: %v", err)
	}
}

func TestPoolManager_Close_WithPool(t *testing.T) {
	requireSchema(t)
	m := NewMonitoredConnectionPoolManager(2, 60)

	mc := makeMonitoredConn(t, 600, testMonitoredServerSecret)
	c, err := m.GetConnection(context.Background(), mc, testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	m.ReturnConnection(mc.ID, c)

	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(m.pools) != 0 {
		t.Errorf("expected pools map to be empty after Close, got %d", len(m.pools))
	}
}

// TestPoolManager_GetConnection_Race exercises the double-check branch
// inside GetConnection: when two goroutines race to create a pool, the
// second must close its newly created pool and use the winner. We can't
// reliably trigger the race in a unit test, but exercising parallel Gets
// gives -race a chance to flag any issue.
func TestPoolManager_GetConnection_Race(t *testing.T) {
	requireSchema(t)
	m := NewMonitoredConnectionPoolManager(4, 60)
	t.Cleanup(func() { _ = m.Close() })

	mc := makeMonitoredConn(t, 700, testMonitoredServerSecret)
	ctx := context.Background()

	var wg sync.WaitGroup
	const goroutines = 8
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := m.GetConnection(ctx, mc, testMonitoredServerSecret)
			if err != nil {
				t.Errorf("GetConnection error: %v", err)
				return
			}
			defer m.ReturnConnection(mc.ID, c)
			// Quick query to make sure the conn is usable.
			if _, err := c.Exec(ctx, "SELECT 1"); err != nil {
				t.Errorf("SELECT 1 on pooled conn: %v", err)
			}
		}()
	}
	wg.Wait()
}

// TestPoolManager_RemovePool_PoolCloseHappensOutsideLock ensures
// RemovePool actually closes the pgx pool.
func TestPoolManager_RemovePool_ClosesPool(t *testing.T) {
	requireSchema(t)
	m := NewMonitoredConnectionPoolManager(2, 60)
	t.Cleanup(func() { _ = m.Close() })

	mc := makeMonitoredConn(t, 800, testMonitoredServerSecret)
	c, err := m.GetConnection(context.Background(), mc, testMonitoredServerSecret)
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	pool := getPoolFor(t, m, mc.ID)
	m.ReturnConnection(mc.ID, c)

	if err := m.RemovePool(mc.ID); err != nil {
		t.Fatalf("RemovePool: %v", err)
	}

	// Acquiring after Close should fail.
	if _, err := pool.Acquire(context.Background()); err == nil {
		t.Error("expected acquire on closed pool to fail")
	}
}

// getPoolFor returns the pool for a given connection ID.
func getPoolFor(t *testing.T, m *MonitoredConnectionPoolManager, id int) *pgxpool.Pool {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.pools[id]
	if !ok {
		t.Fatalf("no pool for connection %d", id)
	}
	return p
}
