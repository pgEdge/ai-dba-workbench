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
	"crypto/sha256"
	"fmt"
	"hash/fnv"
	"maps"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/connstring"
	"github.com/pgedge/ai-workbench/pkg/crypto"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// MonitoredConnectionPoolManager manages connection pools for monitored databases
type MonitoredConnectionPoolManager struct {
	pools           map[int]*pgxpool.Pool
	semaphores      map[int]chan struct{} // Per-connection semaphores for limiting concurrent connections
	versions        map[int]int           // Per-connection PostgreSQL major version cache
	poolHashes      map[int]string        // Pool key -> hash of connection params used to create pool
	poolUpdatedAt   map[int]time.Time     // Connection ID -> updated_at when pool was created
	poolKeyToConnID map[int]int           // Pool key -> connection ID (for reverse lookup)
	connLocks       map[int]*sync.Mutex   // Connection ID -> lock guarding pool creation, acquisition and eviction
	maxConnections  int                   // Maximum concurrent connections per monitored server
	maxIdleSeconds  int                   // Maximum idle time (seconds) before closing idle connections
	mu              sync.RWMutex
}

// NewMonitoredConnectionPoolManager creates a new pool manager
func NewMonitoredConnectionPoolManager(maxConnectionsPerServer int, maxIdleSeconds int) *MonitoredConnectionPoolManager {
	return &MonitoredConnectionPoolManager{
		pools:           make(map[int]*pgxpool.Pool),
		semaphores:      make(map[int]chan struct{}),
		versions:        make(map[int]int),
		poolHashes:      make(map[int]string),
		poolUpdatedAt:   make(map[int]time.Time),
		poolKeyToConnID: make(map[int]int),
		connLocks:       make(map[int]*sync.Mutex),
		maxConnections:  maxConnectionsPerServer,
		maxIdleSeconds:  maxIdleSeconds,
	}
}

// DetectAndCacheVersion detects the PostgreSQL major version and caches it
// Returns the major version number (e.g., 14, 15, 16, 17, 18)
func (m *MonitoredConnectionPoolManager) DetectAndCacheVersion(ctx context.Context, connectionID int, conn *pgxpool.Conn) (int, error) {
	// Check if we already have the version cached
	m.mu.RLock()
	version, exists := m.versions[connectionID]
	m.mu.RUnlock()

	if exists && version > 0 {
		return version, nil
	}

	// Query the server version
	var serverVersion int
	err := conn.QueryRow(ctx, "SELECT current_setting('server_version_num')::int / 10000").Scan(&serverVersion)
	if err != nil {
		return 0, fmt.Errorf("failed to detect PostgreSQL version: %w", err)
	}

	// Cache the version
	m.mu.Lock()
	m.versions[connectionID] = serverVersion
	m.mu.Unlock()

	logger.Debugf("Detected PostgreSQL version %d for connection %d", serverVersion, connectionID)
	return serverVersion, nil
}

// SetMaxConnections updates the maximum concurrent connections per server.
// Only the stored maxConnections value is updated; existing semaphore
// channels are left intact to avoid orphaning goroutines blocked on them.
// New semaphores created by getSemaphore will use the updated size.
func (m *MonitoredConnectionPoolManager) SetMaxConnections(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if n == m.maxConnections {
		return
	}

	logger.Infof("Updating max connections per server from %d to %d (existing semaphores unchanged)", m.maxConnections, n)
	m.maxConnections = n
}

// getSemaphore gets or creates a semaphore for a connection ID
func (m *MonitoredConnectionPoolManager) getSemaphore(connectionID int) chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()

	sem, exists := m.semaphores[connectionID]
	if !exists {
		// Create a buffered channel as a semaphore with maxConnections slots
		sem = make(chan struct{}, m.maxConnections)
		m.semaphores[connectionID] = sem
		logger.Infof("Created semaphore for connection %d with %d slots", connectionID, m.maxConnections)
	}
	return sem
}

// acquireSlot acquires a slot from the semaphore, blocking if all slots are in use
func (m *MonitoredConnectionPoolManager) acquireSlot(ctx context.Context, connectionID int) error {
	sem := m.getSemaphore(connectionID)
	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// releaseSlot releases a slot back to the semaphore
func (m *MonitoredConnectionPoolManager) releaseSlot(connectionID int) {
	m.mu.RLock()
	sem, exists := m.semaphores[connectionID]
	m.mu.RUnlock()

	if exists {
		<-sem
	}
}

// openConnections returns how many connections the manager's pools
// currently hold open, idle or in use, for a monitored connection.
func (m *MonitoredConnectionPoolManager) openConnections(connectionID int) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0
	for poolKey, pool := range m.pools {
		if m.poolKeyToConnID[poolKey] == connectionID {
			total += int(pool.Stat().TotalConns())
		}
	}
	return total
}

// GetConnection retrieves a connection for a monitored database
func (m *MonitoredConnectionPoolManager) GetConnection(ctx context.Context, conn MonitoredConnection, serverSecret string) (*pgxpool.Conn, error) {
	return m.GetConnectionForDatabase(ctx, conn, "", serverSecret)
}

// GetConnectionForDatabase retrieves a connection for a specific database on a monitored server
// If databaseName is empty, uses the database name from the monitored connection
func (m *MonitoredConnectionPoolManager) GetConnectionForDatabase(ctx context.Context, conn MonitoredConnection, databaseName string, serverSecret string) (*pgxpool.Conn, error) {
	// Acquire a slot from the semaphore (blocks if all slots are in use)
	if err := m.acquireSlot(ctx, conn.ID); err != nil {
		return nil, fmt.Errorf("failed to acquire connection slot: %w", err)
	}

	pgxConn, err := m.acquireWithinCap(ctx, conn, databaseName, serverSecret)
	if err != nil {
		m.releaseSlot(conn.ID)
		return nil, err
	}
	return pgxConn, nil
}

// monitoredPoolKey returns the key of the pool for one database on a
// monitored connection. Server-level pools use the positive conn.ID.
// Database-specific pools use a deterministic negative key derived from
// conn.ID and databaseName to avoid collisions with server-level keys.
func monitoredPoolKey(connectionID int, databaseName string) int {
	if databaseName == "" {
		return connectionID
	}
	h := fnv.New32a()
	fmt.Fprintf(h, "%d:%s", connectionID, databaseName)
	return -int(h.Sum32())
}

// connLock gets or creates the mutex that guards pool creation,
// connection acquisition and idle eviction for one monitored connection.
func (m *MonitoredConnectionPoolManager) connLock(connectionID int) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()

	lock, exists := m.connLocks[connectionID]
	if !exists {
		lock = &sync.Mutex{}
		m.connLocks[connectionID] = lock
	}
	return lock
}

// acquireWithinCap returns a connection to one database on a monitored
// server, opening the pool for that database if necessary, without
// letting the connections open to the server exceed its semaphore's
// capacity (pool.max_connections_per_server). The caller must already
// hold a semaphore slot for the connection.
//
// A database-scoped probe visits every database through its own pool,
// and each pool keeps its connections open once they are returned, so
// counting only the connections in use would let a server with many
// databases accumulate an idle connection per database however low the
// cap is set (issue #539). When the target pool has no idle connection
// to hand out, and so would have to open one, idle connections held by
// the connection's other pools are closed first to make room.
//
// Everything here runs under the connection's own lock: without it, a
// concurrent acquisition could take the idle connection this one had
// counted on, or two acquisitions could each decide there was room for
// one more connection.
func (m *MonitoredConnectionPoolManager) acquireWithinCap(ctx context.Context, conn MonitoredConnection, databaseName string, serverSecret string) (*pgxpool.Conn, error) {
	lock := m.connLock(conn.ID)
	lock.Lock()
	defer lock.Unlock()

	poolKey := monitoredPoolKey(conn.ID, databaseName)

	m.mu.RLock()
	pool, exists := m.pools[poolKey]
	m.mu.RUnlock()

	if !exists || pool.Stat().IdleConns() == 0 {
		m.evictIdleConnections(ctx, conn.ID, poolKey)
	}

	if !exists {
		var err error
		pool, err = m.createPool(conn, databaseName, serverSecret, poolKey)
		if err != nil {
			return nil, err
		}
	}

	return pool.Acquire(ctx)
}

// createPool opens a new pool for one database on a monitored
// connection and records it in the manager's maps. The caller must hold
// the connection's lock, which is what guarantees that no other pool can
// be created for the same key in the meantime.
func (m *MonitoredConnectionPoolManager) createPool(conn MonitoredConnection, databaseName string, serverSecret string, poolKey int) (*pgxpool.Pool, error) {
	params, err := buildMonitoredConnectionParams(conn, databaseName, serverSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// Record the hash of the connection's own parameters, as
	// InvalidateChangedPools computes it, rather than of this pool's
	// database-specific ones.
	identity := maps.Clone(params)
	identity["dbname"] = conn.DatabaseName
	paramsHash := hashConnectionParams(identity)

	newPool, err := createMonitoredPool(connstring.Build(params), m.maxConnections, m.maxIdleSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool for monitored connection %d: %w", conn.ID, err)
	}

	m.mu.Lock()
	m.pools[poolKey] = newPool
	m.poolHashes[poolKey] = paramsHash
	m.poolKeyToConnID[poolKey] = conn.ID
	m.poolUpdatedAt[conn.ID] = conn.UpdatedAt
	m.mu.Unlock()

	dbInfo := conn.Name
	if databaseName != "" {
		dbInfo = fmt.Sprintf("%s/%s", conn.Name, databaseName)
	}
	logger.Infof("Created connection pool for monitored connection %d (%s)", conn.ID, dbInfo)

	return newPool, nil
}

// evictIdleConnections closes idle connections held by a monitored
// connection's pools, other than the pool identified by exceptKey, until
// opening one more connection would keep the total within the
// connection's semaphore capacity. The caller must hold the connection's
// lock and a semaphore slot.
//
// Every connection in use belongs to a goroutine holding a semaphore
// slot, including one borrowed from a pool that has since been closed
// and removed from the map, so the connections open to the server number
// at most the idle ones plus the slots held by other goroutines. Opening
// one more therefore stays within the cap when the idle count is no
// greater than the free slots.
func (m *MonitoredConnectionPoolManager) evictIdleConnections(ctx context.Context, connectionID int, exceptKey int) {
	m.mu.RLock()
	sem := m.semaphores[connectionID]
	var others []*pgxpool.Pool
	idle := 0
	for poolKey, pool := range m.pools {
		if m.poolKeyToConnID[poolKey] != connectionID {
			continue
		}
		idle += int(pool.Stat().IdleConns())
		if poolKey != exceptKey {
			others = append(others, pool)
		}
	}
	m.mu.RUnlock()

	if sem == nil {
		return
	}
	excess := idle - (cap(sem) - len(sem))

	for _, pool := range others {
		if excess <= 0 {
			return
		}
		for _, c := range pool.AcquireAllIdle(ctx) {
			if excess <= 0 {
				c.Release()
				continue
			}
			closeHijacked(c.Hijack())
			excess--
		}
	}
}

// closeHijacked closes a connection taken out of its pool with Hijack.
// The bounded context stops an unresponsive server from holding the
// connection's lock, and with it every probe on that server, whilst the
// termination message is sent.
func closeHijacked(c *pgx.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Close(ctx); err != nil {
		logger.Debugf("Error closing idle monitored connection: %v", err)
	}
}

// ReturnConnection returns a connection to the pool and releases the semaphore slot
func (m *MonitoredConnectionPoolManager) ReturnConnection(connectionID int, conn *pgxpool.Conn) {
	// Simply release the connection back to its pool
	conn.Release()
	// Release the semaphore slot
	m.releaseSlot(connectionID)
}

// RemovePool removes a pool for a monitored connection
func (m *MonitoredConnectionPoolManager) RemovePool(connectionID int) error {
	m.mu.Lock()

	pool, exists := m.pools[connectionID]
	if !exists {
		m.mu.Unlock()
		return nil // Already removed
	}

	delete(m.pools, connectionID)
	delete(m.poolHashes, connectionID)
	delete(m.poolKeyToConnID, connectionID)
	delete(m.poolUpdatedAt, connectionID)

	m.mu.Unlock()

	// Close pool outside the lock to avoid deadlock with borrowed connections
	pool.Close()
	logger.Infof("Removed connection pool for monitored connection %d", connectionID)

	return nil
}

// CheckConnectionUpdated checks if the connection's updated_at timestamp has
// changed since the pool was created. If so, it closes and removes all pools
// for that connection and returns true. This allows credential or parameter
// changes to take effect within one probe cycle.
func (m *MonitoredConnectionPoolManager) CheckConnectionUpdated(connectionID int, updatedAt time.Time) bool {
	m.mu.Lock()

	stored, exists := m.poolUpdatedAt[connectionID]
	if !exists || stored.Equal(updatedAt) {
		m.mu.Unlock()
		return false
	}

	// Connection was updated, collect pools to close and remove map entries
	var poolsToClose []*pgxpool.Pool
	for poolKey := range m.pools {
		connID := m.poolKeyToConnID[poolKey]
		if connID == connectionID {
			poolsToClose = append(poolsToClose, m.pools[poolKey])
			delete(m.pools, poolKey)
			delete(m.poolHashes, poolKey)
			delete(m.poolKeyToConnID, poolKey)
		}
	}
	delete(m.poolUpdatedAt, connectionID)
	delete(m.versions, connectionID)

	m.mu.Unlock()

	// Close pools outside the lock to avoid deadlock with borrowed connections
	for _, pool := range poolsToClose {
		pool.Close()
	}

	return true
}

// SyncPools synchronizes the pools with the current list of monitored connections
// Closes and removes pools for connections that are no longer monitored
func (m *MonitoredConnectionPoolManager) SyncPools(activeConnectionIDs []int) {
	m.mu.Lock()

	// Build a set of active connection IDs for fast lookup
	activeSet := make(map[int]bool)
	for _, id := range activeConnectionIDs {
		activeSet[id] = true
	}

	// Find pools for connections that are no longer monitored
	var toRemove []int
	for poolKey := range m.pools {
		connID := m.poolKeyToConnID[poolKey]
		if !activeSet[connID] {
			toRemove = append(toRemove, poolKey)
		}
	}

	// Collect pools to close and remove map entries while holding the lock
	type removedPool struct {
		pool   *pgxpool.Pool
		connID int
	}
	var poolsToClose []removedPool
	for _, poolKey := range toRemove {
		pool := m.pools[poolKey]
		connID := m.poolKeyToConnID[poolKey]
		poolsToClose = append(poolsToClose, removedPool{pool: pool, connID: connID})
		delete(m.pools, poolKey)
		delete(m.poolHashes, poolKey)
		delete(m.poolKeyToConnID, poolKey)

		// Only remove semaphore if there are no more pools for this connection
		hasOtherPools := false
		for pk := range m.pools {
			if m.poolKeyToConnID[pk] == connID {
				hasOtherPools = true
				break
			}
		}
		if !hasOtherPools {
			delete(m.semaphores, connID)
			delete(m.connLocks, connID)
			delete(m.poolUpdatedAt, connID)
		}
	}

	m.mu.Unlock()

	// Close pools outside the lock to avoid deadlock with borrowed connections
	for _, rp := range poolsToClose {
		rp.pool.Close()
		logger.Infof("Removed connection pool for connection %d (no longer monitored)", rp.connID)
	}
}

// monitoredConnectionHash returns a hash of the parameters a monitored
// connection's pools are built from. Every pool derived from the
// connection, whichever database it serves, records this same hash, so a
// per-database pool is not mistaken for a changed one merely because its
// dbname differs from the connection's default. The parameters are
// hashed in sorted order because connstring.Build emits them in map
// iteration order, which differs from one call to the next; hashing its
// output made every pool look changed on every configuration reload.
func monitoredConnectionHash(conn MonitoredConnection, serverSecret string) (string, error) {
	params, err := buildMonitoredConnectionParams(conn, "", serverSecret)
	if err != nil {
		return "", err
	}
	return hashConnectionParams(params), nil
}

// hashConnectionParams returns a hex-encoded SHA-256 hash of a set of
// libpq parameters that does not depend on map iteration order.
func hashConnectionParams(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, key := range keys {
		// NUL cannot appear in a libpq parameter, so it separates
		// fields unambiguously.
		fmt.Fprintf(h, "%s\x00%s\x00", key, params[key])
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// InvalidateChangedPools compares current connection parameters against the
// hashes stored when each pool was created. If a connection's parameters have
// changed (e.g. password rotation), every pool derived from that connection is
// closed so the next GetConnection call creates a fresh pool.
func (m *MonitoredConnectionPoolManager) InvalidateChangedPools(connections []MonitoredConnection, serverSecret string) {
	// Build a map of connection ID -> current connection string hash
	currentHashes := make(map[int]string)
	for _, conn := range connections {
		paramsHash, err := monitoredConnectionHash(conn, serverSecret)
		if err != nil {
			logger.Errorf("Failed to build connection string for pool invalidation check on connection %d: %v", conn.ID, err)
			continue
		}
		currentHashes[conn.ID] = paramsHash
	}

	m.mu.Lock()

	// Identify pool keys whose connection parameters have changed
	var toRemove []int
	for poolKey, storedHash := range m.poolHashes {
		connID := m.poolKeyToConnID[poolKey]

		currentHash, exists := currentHashes[connID]
		if !exists {
			// Connection no longer active; SyncPools handles removal
			continue
		}

		if currentHash != storedHash {
			toRemove = append(toRemove, poolKey)
		}
	}

	// Collect pools to close and remove map entries while holding the lock
	type removedPool struct {
		pool    *pgxpool.Pool
		poolKey int
		connID  int
	}
	var poolsToClose []removedPool
	for _, poolKey := range toRemove {
		connID := m.poolKeyToConnID[poolKey]

		pool := m.pools[poolKey]
		if pool != nil {
			poolsToClose = append(poolsToClose, removedPool{pool: pool, poolKey: poolKey, connID: connID})
		}
		delete(m.pools, poolKey)
		delete(m.poolHashes, poolKey)
		delete(m.poolKeyToConnID, poolKey)

		// Also clear the cached version for this connection
		delete(m.versions, connID)
	}

	m.mu.Unlock()

	// Close pools outside the lock to avoid deadlock with borrowed connections
	for _, rp := range poolsToClose {
		rp.pool.Close()
		logger.Infof("Invalidated connection pool (key %d) for connection %d due to changed connection parameters", rp.poolKey, rp.connID)
	}
}

// Close closes all pools
func (m *MonitoredConnectionPoolManager) Close() error {
	m.mu.Lock()

	poolCount := len(m.pools)
	if poolCount == 0 {
		m.mu.Unlock()
		logger.Info("No monitored connection pools to close")
		return nil
	}

	logger.Infof("Closing %d monitored connection pool(s)...", poolCount)

	// Collect pools to close
	poolsToClose := make(map[int]*pgxpool.Pool, poolCount)
	for id, pool := range m.pools {
		poolsToClose[id] = pool
	}
	m.pools = make(map[int]*pgxpool.Pool)

	m.mu.Unlock()

	// Close pools outside the lock to avoid deadlock with borrowed connections
	for id, pool := range poolsToClose {
		pool.Close()
		logger.Infof("Closed pool for connection %d", id)
	}

	logger.Infof("Closed %d monitored connection pool(s)", poolCount)

	return nil
}

// createMonitoredPool creates a pgxpool.Pool for a monitored connection
func createMonitoredPool(connStr string, maxConns int, maxIdleSeconds int) (*pgxpool.Pool, error) {
	ctx := context.Background()

	// Parse connection string
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Set MaxConns to match the per-server connection limit
	if maxConns > math.MaxInt32 {
		maxConns = math.MaxInt32
	}
	config.MaxConns = int32(maxConns) // #nosec G115 - bounds checked above
	config.MinConns = 0               // Start with no connections

	// Set max connection lifetime (use maxIdleSeconds as max conn lifetime)
	// pgxpool will close connections that have been idle for this duration
	if maxIdleSeconds > 0 {
		config.MaxConnIdleTime = time.Duration(maxIdleSeconds) * time.Second
	}

	// Create the pool
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	// Test the pool with a ping
	conn, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to acquire test connection: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		conn.Release()
		pool.Close()
		return nil, fmt.Errorf("failed to ping: %w", err)
	}
	conn.Release()

	return pool, nil
}

// buildMonitoredConnectionStringForDatabase builds a connection string for a monitored connection
// with an optional database name override
func buildMonitoredConnectionStringForDatabase(conn MonitoredConnection, databaseName string, serverSecret string) (string, error) {
	params, err := buildMonitoredConnectionParams(conn, databaseName, serverSecret)
	if err != nil {
		return "", err
	}
	return connstring.Build(params), nil
}

// buildMonitoredConnectionParams returns the libpq parameters for a
// monitored connection, with an optional database name override
func buildMonitoredConnectionParams(conn MonitoredConnection, databaseName string, serverSecret string) (map[string]string, error) {
	params := make(map[string]string)

	if conn.HostAddr.Valid && conn.HostAddr.String != "" {
		params["hostaddr"] = conn.HostAddr.String
	} else {
		params["host"] = conn.Host
	}

	params["port"] = fmt.Sprintf("%d", conn.Port)

	// Use provided database name or fall back to connection's default
	if databaseName != "" {
		params["dbname"] = databaseName
	} else {
		params["dbname"] = conn.DatabaseName
	}

	params["user"] = conn.Username

	// Decrypt password if encrypted
	if conn.PasswordEncrypted.Valid && conn.PasswordEncrypted.String != "" {
		decryptedPassword, err := crypto.DecryptPassword(conn.PasswordEncrypted.String, serverSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt password for connection %d: %w", conn.ID, err)
		}
		params["password"] = decryptedPassword
	}

	addMonitoredSSLParams(params, conn)

	// Set application name to identify monitoring connections
	params["application_name"] = ApplicationName

	// Set connection timeout (10 seconds)
	params["connect_timeout"] = "10"

	return params, nil
}

// addMonitoredSSLParams adds a monitored connection's TLS settings to
// params, defaulting sslmode to prefer when none is configured.
func addMonitoredSSLParams(params map[string]string, conn MonitoredConnection) {
	if conn.SSLMode.Valid && conn.SSLMode.String != "" {
		params["sslmode"] = conn.SSLMode.String
	} else {
		params["sslmode"] = "prefer"
	}

	if conn.SSLCert.Valid && conn.SSLCert.String != "" {
		params["sslcert"] = conn.SSLCert.String
	}

	if conn.SSLKey.Valid && conn.SSLKey.String != "" {
		params["sslkey"] = conn.SSLKey.String
	}

	if conn.SSLRootCert.Valid && conn.SSLRootCert.String != "" {
		params["sslrootcert"] = conn.SSLRootCert.String
	}
}
