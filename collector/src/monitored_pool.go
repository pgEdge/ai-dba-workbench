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
	pools map[int]*ConnectionPool
	mu    sync.RWMutex
}

// NewMonitoredConnectionPoolManager creates a new pool manager
func NewMonitoredConnectionPoolManager() *MonitoredConnectionPoolManager {
	return &MonitoredConnectionPoolManager{
		pools: make(map[int]*ConnectionPool),
	}
}

// GetConnection retrieves a connection for a monitored database
func (m *MonitoredConnectionPoolManager) GetConnection(_ context.Context, conn MonitoredConnection, serverSecret string) (*sql.DB, error) {
	return m.GetConnectionForDatabase(context.Background(), conn, "", serverSecret)
}

// GetConnectionForDatabase retrieves a connection for a specific database on a monitored server
// If databaseName is empty, uses the database name from the monitored connection
func (m *MonitoredConnectionPoolManager) GetConnectionForDatabase(_ context.Context, conn MonitoredConnection, databaseName string, serverSecret string) (*sql.DB, error) {
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
		return pool.GetConnection()
	}

	// Pool doesn't exist, create it
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check again in case another goroutine created it
	pool, exists = m.pools[poolKey]
	if exists {
		return pool.GetConnection()
	}

	// Build connection string with specified database
	connStr, err := buildMonitoredConnectionStringForDatabase(conn, databaseName, serverSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// Create new pool
	newPool, err := NewConnectionPool(connStr, 5, 300)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool for monitored connection %d: %w", conn.ID, err)
	}

	m.pools[poolKey] = newPool
	dbInfo := conn.Name
	if databaseName != "" {
		dbInfo = fmt.Sprintf("%s/%s", conn.Name, databaseName)
	}
	log.Printf("Created connection pool for monitored connection %d (%s)", conn.ID, dbInfo)

	return newPool.GetConnection()
}

// ReturnConnection returns a connection to the pool
func (m *MonitoredConnectionPoolManager) ReturnConnection(connectionID int, db *sql.DB) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// First try the default pool for this connection
	if pool, exists := m.pools[connectionID]; exists {
		if err := pool.ReturnConnection(db); err == nil {
			return nil
		}
	}

	// If not found, search all pools (needed for database-specific pools)
	for _, pool := range m.pools {
		if err := pool.ReturnConnection(db); err == nil {
			return nil
		}
	}

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

	var lastErr error
	for id, pool := range m.pools {
		if err := pool.Close(); err != nil {
			log.Printf("Error closing pool for connection %d: %v", id, err)
			lastErr = err
		}
	}

	m.pools = make(map[int]*ConnectionPool)
	return lastErr
}

// buildMonitoredConnectionString builds a connection string for a monitored connection
func buildMonitoredConnectionString(conn MonitoredConnection, serverSecret string) (string, error) {
	return buildMonitoredConnectionStringForDatabase(conn, "", serverSecret)
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
