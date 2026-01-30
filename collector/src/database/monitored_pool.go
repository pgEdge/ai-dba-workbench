/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/crypto"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// MonitoredConnectionPoolManager manages connection pools for monitored databases
type MonitoredConnectionPoolManager struct {
	pools          map[int]*pgxpool.Pool
	semaphores     map[int]chan struct{} // Per-connection semaphores for limiting concurrent connections
	versions       map[int]int           // Per-connection PostgreSQL major version cache
	poolHashes     map[int]string        // Pool key -> hash of connection params used to create pool
	poolUpdatedAt  map[int]time.Time     // Connection ID -> updated_at when pool was created
	maxConnections int                   // Maximum concurrent connections per monitored server
	maxIdleSeconds int                   // Maximum idle time (seconds) before closing idle connections
	mu             sync.RWMutex
}

// NewMonitoredConnectionPoolManager creates a new pool manager
func NewMonitoredConnectionPoolManager(maxConnectionsPerServer int, maxIdleSeconds int) *MonitoredConnectionPoolManager {
	return &MonitoredConnectionPoolManager{
		pools:          make(map[int]*pgxpool.Pool),
		semaphores:     make(map[int]chan struct{}),
		versions:       make(map[int]int),
		poolHashes:     make(map[int]string),
		poolUpdatedAt:  make(map[int]time.Time),
		maxConnections: maxConnectionsPerServer,
		maxIdleSeconds: maxIdleSeconds,
	}
}

// GetVersion returns the cached PostgreSQL major version for a connection
// Returns 0 if version is not cached
func (m *MonitoredConnectionPoolManager) GetVersion(connectionID int) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.versions[connectionID]
}

// SetVersion caches the PostgreSQL major version for a connection
func (m *MonitoredConnectionPoolManager) SetVersion(connectionID int, majorVersion int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.versions[connectionID] = majorVersion
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
	// Generate a unique pool key based on connection ID and database name
	poolKey := conn.ID
	if databaseName != "" {
		// Use a hash or composite key for database-specific pools
		// For simplicity, we'll use negative IDs for database-specific pools
		// This is a temporary solution - a better approach would use a struct key
		poolKey = -(conn.ID * 10000) // Negative to distinguish from regular connections
	}

	m.mu.RLock()
	pool, exists := m.pools[poolKey]
	m.mu.RUnlock()

	if exists {
		pgxConn, err := pool.Acquire(ctx)
		if err != nil {
			m.releaseSlot(conn.ID)
			return nil, err
		}
		return pgxConn, nil
	}

	// Pool doesn't exist, create it
	// Build connection string with specified database
	connStr, err := buildMonitoredConnectionStringForDatabase(conn, databaseName, serverSecret)
	if err != nil {
		m.releaseSlot(conn.ID)
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// Create new pool (without holding lock)
	newPool, err := createMonitoredPool(connStr, m.maxConnections, m.maxIdleSeconds)
	if err != nil {
		m.releaseSlot(conn.ID)
		return nil, fmt.Errorf("failed to create connection pool for monitored connection %d: %w", conn.ID, err)
	}

	// Now acquire lock just to add pool to map
	m.mu.Lock()
	// Check again in case another goroutine created it while we were creating ours
	if existingPool, exists := m.pools[poolKey]; exists {
		m.mu.Unlock()
		// Close our newly created pool since we don't need it
		newPool.Close()
		// Use the existing pool instead
		pgxConn, err := existingPool.Acquire(ctx)
		if err != nil {
			m.releaseSlot(conn.ID)
			return nil, err
		}
		return pgxConn, nil
	}

	// Add our new pool to the map and store the connection string hash
	m.pools[poolKey] = newPool
	m.poolHashes[poolKey] = hashConnString(connStr)
	m.poolUpdatedAt[conn.ID] = conn.UpdatedAt
	m.mu.Unlock()

	dbInfo := conn.Name
	if databaseName != "" {
		dbInfo = fmt.Sprintf("%s/%s", conn.Name, databaseName)
	}
	logger.Infof("Created connection pool for monitored connection %d (%s)", conn.ID, dbInfo)

	// Get connection from pool (without holding lock)
	pgxConn, err := newPool.Acquire(ctx)
	if err != nil {
		m.releaseSlot(conn.ID)
		return nil, err
	}
	return pgxConn, nil
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
	defer m.mu.Unlock()

	pool, exists := m.pools[connectionID]
	if !exists {
		return nil // Already removed
	}

	pool.Close()
	delete(m.pools, connectionID)
	delete(m.poolHashes, connectionID)
	delete(m.poolUpdatedAt, connectionID)
	logger.Infof("Removed connection pool for monitored connection %d", connectionID)

	return nil
}

// CheckConnectionUpdated checks if the connection's updated_at timestamp has
// changed since the pool was created. If so, it closes and removes all pools
// for that connection and returns true. This allows credential or parameter
// changes to take effect within one probe cycle.
func (m *MonitoredConnectionPoolManager) CheckConnectionUpdated(connectionID int, updatedAt time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	stored, exists := m.poolUpdatedAt[connectionID]
	if !exists || stored.Equal(updatedAt) {
		return false
	}

	// Connection was updated, invalidate all pools for this connection
	for poolKey := range m.pools {
		connID := poolKey
		if poolKey < 0 {
			connID = -(poolKey / 10000)
		}
		if connID == connectionID {
			m.pools[poolKey].Close()
			delete(m.pools, poolKey)
			delete(m.poolHashes, poolKey)
		}
	}
	delete(m.poolUpdatedAt, connectionID)
	delete(m.versions, connectionID)

	return true
}

// SyncPools synchronizes the pools with the current list of monitored connections
// Closes and removes pools for connections that are no longer monitored
func (m *MonitoredConnectionPoolManager) SyncPools(activeConnectionIDs []int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Build a set of active connection IDs for fast lookup
	activeSet := make(map[int]bool)
	for _, id := range activeConnectionIDs {
		activeSet[id] = true
	}

	// Find and close pools for connections that are no longer monitored
	var toRemove []int
	for poolKey := range m.pools {
		// Extract the actual connection ID from the pool key
		// Positive keys are regular connections, negative keys are database-specific pools
		connID := poolKey
		if poolKey < 0 {
			// Database-specific pool - extract connection ID
			connID = -(poolKey / 10000)
		}

		// If this connection is no longer in the active set, mark for removal
		if !activeSet[connID] {
			toRemove = append(toRemove, poolKey)
		}
	}

	// Close and remove pools that are no longer needed
	for _, poolKey := range toRemove {
		pool := m.pools[poolKey]
		pool.Close()
		delete(m.pools, poolKey)
		delete(m.poolHashes, poolKey)

		// Also remove the semaphore for regular connections
		connID := poolKey
		if poolKey < 0 {
			connID = -(poolKey / 10000)
		}
		// Only remove semaphore if there are no more pools for this connection
		hasOtherPools := false
		for pk := range m.pools {
			pkConnID := pk
			if pk < 0 {
				pkConnID = -(pk / 10000)
			}
			if pkConnID == connID {
				hasOtherPools = true
				break
			}
		}
		if !hasOtherPools {
			delete(m.semaphores, connID)
			delete(m.poolUpdatedAt, connID)
			logger.Infof("Removed connection pool for connection %d (no longer monitored)", connID)
		}
	}
}

// hashConnString returns a hex-encoded SHA-256 hash of a connection string
func hashConnString(connStr string) string {
	h := sha256.Sum256([]byte(connStr))
	return fmt.Sprintf("%x", h)
}

// InvalidateChangedPools compares current connection parameters against the
// hashes stored when each pool was created. If a connection's parameters have
// changed (e.g. password rotation), every pool derived from that connection is
// closed so the next GetConnection call creates a fresh pool.
func (m *MonitoredConnectionPoolManager) InvalidateChangedPools(connections []MonitoredConnection, serverSecret string) {
	// Build a map of connection ID -> current connection string hash
	currentHashes := make(map[int]string)
	for _, conn := range connections {
		connStr, err := buildMonitoredConnectionStringForDatabase(conn, "", serverSecret)
		if err != nil {
			logger.Errorf("Failed to build connection string for pool invalidation check on connection %d: %v", conn.ID, err)
			continue
		}
		currentHashes[conn.ID] = hashConnString(connStr)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Identify pool keys whose connection parameters have changed
	var toRemove []int
	for poolKey, storedHash := range m.poolHashes {
		// Derive the connection ID from the pool key
		connID := poolKey
		if poolKey < 0 {
			connID = -(poolKey / 10000)
		}

		currentHash, exists := currentHashes[connID]
		if !exists {
			// Connection no longer active; SyncPools handles removal
			continue
		}

		if currentHash != storedHash {
			toRemove = append(toRemove, poolKey)
		}
	}

	// Close and remove invalidated pools
	for _, poolKey := range toRemove {
		connID := poolKey
		if poolKey < 0 {
			connID = -(poolKey / 10000)
		}

		pool := m.pools[poolKey]
		if pool != nil {
			pool.Close()
		}
		delete(m.pools, poolKey)
		delete(m.poolHashes, poolKey)

		// Also clear the cached version for this connection
		delete(m.versions, connID)

		logger.Infof("Invalidated connection pool (key %d) for connection %d due to changed connection parameters", poolKey, connID)
	}
}

// Close closes all pools
func (m *MonitoredConnectionPoolManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	poolCount := len(m.pools)
	if poolCount == 0 {
		logger.Info("No monitored connection pools to close")
		return nil
	}

	logger.Infof("Closing %d monitored connection pool(s)...", poolCount)

	for id, pool := range m.pools {
		pool.Close()
		logger.Infof("Closed pool for connection %d", id)
	}

	logger.Infof("Closed %d monitored connection pool(s)", poolCount)

	m.pools = make(map[int]*pgxpool.Pool)
	return nil
}

// createMonitoredPool creates a pgxpool.Pool for a monitored connection
func createMonitoredPool(connStr string, _ int, maxIdleSeconds int) (*pgxpool.Pool, error) {
	ctx := context.Background()

	// Parse connection string
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Configure pool settings
	// Set MaxConns to 1 since each database gets its own pool
	// The semaphore in the pool manager controls the actual concurrency limit
	config.MaxConns = 1
	config.MinConns = 0 // Start with no connections

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
	// Build connection string
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
			return "", fmt.Errorf("failed to decrypt password for connection %d: %w", conn.ID, err)
		}
		params["password"] = decryptedPassword
	}

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

	// Set application name to identify monitoring connections
	params["application_name"] = ApplicationName

	// Set connection timeout (10 seconds)
	params["connect_timeout"] = "10"

	return buildPostgresConnectionString(params), nil
}
