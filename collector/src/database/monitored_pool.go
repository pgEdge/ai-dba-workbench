/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MonitoredConnectionPoolManager manages connection pools for monitored databases
type MonitoredConnectionPoolManager struct {
	pools          map[int]*pgxpool.Pool
	semaphores     map[int]chan struct{} // Per-connection semaphores for limiting concurrent connections
	maxConnections int                   // Maximum concurrent connections per monitored server
	maxIdleSeconds int                   // Maximum idle time (seconds) before closing idle connections
	mu             sync.RWMutex
}

// NewMonitoredConnectionPoolManager creates a new pool manager
func NewMonitoredConnectionPoolManager(maxConnectionsPerServer int, maxIdleSeconds int) *MonitoredConnectionPoolManager {
	return &MonitoredConnectionPoolManager{
		pools:          make(map[int]*pgxpool.Pool),
		semaphores:     make(map[int]chan struct{}),
		maxConnections: maxConnectionsPerServer,
		maxIdleSeconds: maxIdleSeconds,
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

	// Add our new pool to the map
	m.pools[poolKey] = newPool
	m.mu.Unlock()

	dbInfo := conn.Name
	if databaseName != "" {
		dbInfo = fmt.Sprintf("%s/%s", conn.Name, databaseName)
	}
	log.Printf("Created connection pool for monitored connection %d (%s)", conn.ID, dbInfo)

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

	for id, pool := range m.pools {
		pool.Close()
		log.Printf("Closed pool for connection %d", id)
	}

	log.Printf("Closed %d monitored connection pool(s)", poolCount)

	m.pools = make(map[int]*pgxpool.Pool)
	return nil
}

// createMonitoredPool creates a pgxpool.Pool for a monitored connection
func createMonitoredPool(connStr string, maxConnections int, maxIdleSeconds int) (*pgxpool.Pool, error) {
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

	// Decrypt password if encrypted and we have an owner username
	if conn.PasswordEncrypted.Valid && conn.PasswordEncrypted.String != "" {
		if conn.OwnerUsername.Valid && conn.OwnerUsername.String != "" {
			// Decrypt the password using the server secret and owner username
			decryptedPassword, err := DecryptPassword(conn.PasswordEncrypted.String, serverSecret, conn.OwnerUsername.String)
			if err != nil {
				return "", fmt.Errorf("failed to decrypt password for connection %d: %w", conn.ID, err)
			}
			params["password"] = decryptedPassword
		} else {
			// No owner username - password might not be encrypted or uses legacy encryption
			// For now, use it as-is (this handles backward compatibility)
			params["password"] = conn.PasswordEncrypted.String
		}
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
