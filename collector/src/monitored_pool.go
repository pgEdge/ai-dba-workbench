/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"

	_ "github.com/lib/pq"
)

// MonitoredConnectionPoolManager manages connection pools for monitored databases
type MonitoredConnectionPoolManager struct {
	pools          map[int]*ConnectionPool
	semaphores     map[int]chan struct{} // Per-connection semaphores for limiting concurrent connections
	maxConnections int                   // Maximum concurrent connections per monitored server
	mu             sync.RWMutex
}

// NewMonitoredConnectionPoolManager creates a new pool manager
func NewMonitoredConnectionPoolManager(maxConnectionsPerServer int) *MonitoredConnectionPoolManager {
	return &MonitoredConnectionPoolManager{
		pools:          make(map[int]*ConnectionPool),
		semaphores:     make(map[int]chan struct{}),
		maxConnections: maxConnectionsPerServer,
	}
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
		log.Printf("Created semaphore for connection %d with %d slots", connectionID, m.maxConnections)
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
func (m *MonitoredConnectionPoolManager) GetConnection(ctx context.Context, conn MonitoredConnection, serverSecret string) (*sql.DB, error) {
	return m.GetConnectionForDatabase(ctx, conn, "", serverSecret)
}

// GetConnectionForDatabase retrieves a connection for a specific database on a monitored server
// If databaseName is empty, uses the database name from the monitored connection
func (m *MonitoredConnectionPoolManager) GetConnectionForDatabase(ctx context.Context, conn MonitoredConnection, databaseName string, serverSecret string) (*sql.DB, error) {
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
		db, err := pool.GetConnection(ctx)
		if err != nil {
			m.releaseSlot(conn.ID)
			return nil, err
		}
		return db, nil
	}

	// Pool doesn't exist, create it
	// Build connection string with specified database
	connStr, err := buildMonitoredConnectionStringForDatabase(conn, databaseName, serverSecret)
	if err != nil {
		m.releaseSlot(conn.ID)
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// Create new pool (without holding lock)
	newPool, err := NewConnectionPool(connStr, 5, 300)
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
		if cerr := newPool.Close(); cerr != nil {
			log.Printf("Error closing unused pool: %v", cerr)
		}
		// Use the existing pool instead
		db, err := existingPool.GetConnection(ctx)
		if err != nil {
			m.releaseSlot(conn.ID)
			return nil, err
		}
		return db, nil
	}

	// Add our new pool to the map
	m.pools[poolKey] = newPool
	m.mu.Unlock()

	dbInfo := conn.Name
	if databaseName != "" {
		dbInfo = fmt.Sprintf("%s/%s", conn.Name, databaseName)
	}
	log.Printf("Created connection pool for monitored connection %d (%s)", conn.ID, dbInfo)

	// Get connection from pool (without holding lock)
	db, err := newPool.GetConnection(ctx)
	if err != nil {
		m.releaseSlot(conn.ID)
		return nil, err
	}
	return db, nil
}

// ReturnConnection returns a connection to the pool and releases the semaphore slot
func (m *MonitoredConnectionPoolManager) ReturnConnection(connectionID int, db *sql.DB) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// First try the default pool for this connection
	if pool, exists := m.pools[connectionID]; exists {
		if err := pool.ReturnConnection(db); err == nil {
			m.releaseSlot(connectionID)
			return nil
		}
	}

	// If not found, search all pools (needed for database-specific pools)
	for _, pool := range m.pools {
		if err := pool.ReturnConnection(db); err == nil {
			m.releaseSlot(connectionID)
			return nil
		}
	}

	// If we couldn't return the connection, still release the slot to avoid leaks
	m.releaseSlot(connectionID)
	return fmt.Errorf("connection not found in any pool for connection ID %d", connectionID)
}

// RemovePool removes a pool for a monitored connection
func (m *MonitoredConnectionPoolManager) RemovePool(connectionID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pool, exists := m.pools[connectionID]
	if !exists {
		return nil // Already removed
	}

	if err := pool.Close(); err != nil {
		log.Printf("Error closing pool for connection %d: %v", connectionID, err)
	}

	delete(m.pools, connectionID)
	log.Printf("Removed connection pool for monitored connection %d", connectionID)

	return nil
}

// Close closes all pools
func (m *MonitoredConnectionPoolManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	poolCount := len(m.pools)
	if poolCount == 0 {
		log.Println("No monitored connection pools to close")
		return nil
	}

	log.Printf("Closing %d monitored connection pool(s)...", poolCount)

	var lastErr error
	closedCount := 0
	for id, pool := range m.pools {
		if err := pool.Close(); err != nil {
			log.Printf("Error closing pool for connection %d: %v", id, err)
			lastErr = err
		} else {
			closedCount++
		}
	}

	log.Printf("Closed %d of %d monitored connection pool(s)", closedCount, poolCount)

	m.pools = make(map[int]*ConnectionPool)
	return lastErr
}

// buildMonitoredConnectionStringForDatabase builds a connection string for a monitored connection
// with an optional database name override
func buildMonitoredConnectionStringForDatabase(conn MonitoredConnection, databaseName string, _ string) (string, error) {
	// TODO: Use serverSecret to decrypt password
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

	// TODO: Implement actual password decryption using server_secret
	if conn.PasswordEncrypted.Valid && conn.PasswordEncrypted.String != "" {
		params["password"] = conn.PasswordEncrypted.String
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
	params["application_name"] = "pgEdge AI Workbench - Monitoring"

	// Set connection timeout (10 seconds)
	params["connect_timeout"] = "10"

	// Build connection string from params
	var connStr string
	for key, value := range params {
		if connStr != "" {
			connStr += " "
		}
		connStr += fmt.Sprintf("%s='%s'", key, value)
	}

	return connStr, nil
}
