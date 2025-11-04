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
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

// Datastore represents a connection to the PostgreSQL datastore
type Datastore struct {
	pool   *ConnectionPool
	config *Config
}

// initDatastore initializes the datastore connection
func initDatastore(config *Config) (*Datastore, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	ds := &Datastore{
		config: config,
	}

	if err := ds.connect(); err != nil {
		return nil, err
	}

	if err := ds.initializeSchema(); err != nil {
		if cerr := ds.Close(); cerr != nil {
			log.Printf("Error closing datastore after schema initialization failure: %v", cerr)
		}
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return ds, nil
}

// connect establishes a connection pool to the PostgreSQL datastore
func (ds *Datastore) connect() error {
	connStr := ds.buildConnectionString()

	pool, err := NewConnectionPool(
		connStr,
		ds.config.PoolMaxConnections,
		ds.config.PoolMaxIdleSeconds,
	)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test the connection pool by getting a connection
	conn, err := pool.GetConnection()
	if err != nil {
		if cerr := pool.Close(); cerr != nil {
			log.Printf("Error closing pool after connection test failure: %v", cerr)
		}
		return fmt.Errorf("failed to get test connection: %w", err)
	}

	// Return the test connection
	if err := pool.ReturnConnection(conn); err != nil {
		if cerr := pool.Close(); cerr != nil {
			log.Printf("Error closing pool after return connection failure: %v", cerr)
		}
		return fmt.Errorf("failed to return test connection: %w", err)
	}

	ds.pool = pool
	return nil
}

// buildConnectionString builds a PostgreSQL connection string from config
func (ds *Datastore) buildConnectionString() string {
	cfg := ds.config

	// Start with basic connection parameters
	params := make(map[string]string)
	params["dbname"] = cfg.PgDatabase
	params["user"] = cfg.PgUsername

	if cfg.PgHostAddr != "" {
		params["hostaddr"] = cfg.PgHostAddr
	} else if cfg.PgHost != "" {
		params["host"] = cfg.PgHost
	}

	if cfg.PgPort != 0 {
		params["port"] = fmt.Sprintf("%d", cfg.PgPort)
	}

	if cfg.PgPassword != "" {
		params["password"] = cfg.PgPassword
	}

	// SSL parameters
	if cfg.PgSSLMode != "" {
		params["sslmode"] = cfg.PgSSLMode
	}
	if cfg.PgSSLCert != "" {
		params["sslcert"] = cfg.PgSSLCert
	}
	if cfg.PgSSLKey != "" {
		params["sslkey"] = cfg.PgSSLKey
	}
	if cfg.PgSSLRootCert != "" {
		params["sslrootcert"] = cfg.PgSSLRootCert
	}

	// Build the connection string
	var connStr string
	for key, value := range params {
		if connStr != "" {
			connStr += " "
		}
		connStr += fmt.Sprintf("%s='%s'", key, value)
	}

	return connStr
}

// initializeSchema creates the necessary database schema if it doesn't exist
func (ds *Datastore) initializeSchema() error {
	log.Println("Initializing database schema...")

	conn, err := ds.GetConnection()
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer func() {
		if cerr := ds.ReturnConnection(conn); cerr != nil {
			log.Printf("Error returning connection: %v", cerr)
		}
	}()

	// Create schema version table
	_, err = conn.Exec(`
        CREATE TABLE IF NOT EXISTS schema_version (
            version INTEGER PRIMARY KEY,
            applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
        )
    `)
	if err != nil {
		return fmt.Errorf("failed to create schema_version table: %w", err)
	}

	// Create monitored connections table
	_, err = conn.Exec(`
        CREATE TABLE IF NOT EXISTS monitored_connections (
            id SERIAL PRIMARY KEY,
            name VARCHAR(255) NOT NULL,
            host VARCHAR(255) NOT NULL,
            hostaddr VARCHAR(255),
            port INTEGER NOT NULL DEFAULT 5432,
            database_name VARCHAR(255) NOT NULL,
            username VARCHAR(255) NOT NULL,
            password_encrypted TEXT,
            sslmode VARCHAR(50),
            sslcert TEXT,
            sslkey TEXT,
            sslrootcert TEXT,
            is_shared BOOLEAN NOT NULL DEFAULT FALSE,
            owner_token VARCHAR(255),
            is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
        )
    `)
	if err != nil {
		return fmt.Errorf("failed to create monitored_connections table: %w", err)
	}

	// Create probes configuration table
	_, err = conn.Exec(`
        CREATE TABLE IF NOT EXISTS probes (
            id SERIAL PRIMARY KEY,
            name VARCHAR(255) NOT NULL UNIQUE,
            description TEXT,
            sql_query TEXT NOT NULL,
            collection_interval INTEGER NOT NULL DEFAULT 60,
            retention_days INTEGER NOT NULL DEFAULT 7,
            enabled BOOLEAN NOT NULL DEFAULT TRUE,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
        )
    `)
	if err != nil {
		return fmt.Errorf("failed to create probes table: %w", err)
	}

	log.Println("Database schema initialized")
	return nil
}

// GetConnection retrieves a connection from the pool
func (ds *Datastore) GetConnection() (*sql.DB, error) {
	return ds.pool.GetConnection()
}

// ReturnConnection returns a connection to the pool
func (ds *Datastore) ReturnConnection(conn *sql.DB) error {
	return ds.pool.ReturnConnection(conn)
}

// Close closes the datastore connection pool
func (ds *Datastore) Close() error {
	if ds.pool != nil {
		return ds.pool.Close()
	}
	return nil
}

// CollectGarbage drops old metric partitions based on retention policies
func (ds *Datastore) CollectGarbage() error {
	conn, err := ds.GetConnection()
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer func() {
		if cerr := ds.ReturnConnection(conn); cerr != nil {
			log.Printf("Error returning connection: %v", cerr)
		}
	}()

	// Get all probes with their retention policies
	rows, err := conn.Query(`
        SELECT id, name, retention_days
        FROM probes
        WHERE enabled = TRUE
    `)
	if err != nil {
		return fmt.Errorf("failed to query probes: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Printf("Error closing rows: %v", cerr)
		}
	}()

	for rows.Next() {
		var probeID int
		var probeName string
		var retentionDays int

		if err := rows.Scan(&probeID, &probeName, &retentionDays); err != nil {
			log.Printf("Error scanning probe row: %v", err)
			continue
		}

		// Calculate cutoff date
		cutoffDate := time.Now().AddDate(0, 0, -retentionDays)

		log.Printf("Cleaning up probe %s (id=%d) data older than %v",
			probeName, probeID, cutoffDate)

		// This is a placeholder - actual partition dropping logic would go here
		// For now, we just log the intent
	}

	return rows.Err()
}

// GetMonitoredConnections returns all connections that should be monitored
func (ds *Datastore) GetMonitoredConnections() ([]MonitoredConnection, error) {
	conn, err := ds.GetConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer func() {
		if cerr := ds.ReturnConnection(conn); cerr != nil {
			log.Printf("Error returning connection: %v", cerr)
		}
	}()

	rows, err := conn.Query(`
        SELECT id, name, host, hostaddr, port, database_name, username,
               password_encrypted, sslmode, sslcert, sslkey, sslrootcert
        FROM monitored_connections
        WHERE is_monitored = TRUE
    `)
	if err != nil {
		return nil, fmt.Errorf("failed to query monitored connections: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Printf("Error closing rows: %v", cerr)
		}
	}()

	var connections []MonitoredConnection
	for rows.Next() {
		var c MonitoredConnection
		if err := rows.Scan(
			&c.ID, &c.Name, &c.Host, &c.HostAddr, &c.Port,
			&c.DatabaseName, &c.Username, &c.PasswordEncrypted,
			&c.SSLMode, &c.SSLCert, &c.SSLKey, &c.SSLRootCert,
		); err != nil {
			return nil, fmt.Errorf("failed to scan connection row: %w", err)
		}
		connections = append(connections, c)
	}

	return connections, rows.Err()
}

// MonitoredConnection represents a PostgreSQL connection to monitor
type MonitoredConnection struct {
	ID                int
	Name              string
	Host              string
	HostAddr          sql.NullString
	Port              int
	DatabaseName      string
	Username          string
	PasswordEncrypted sql.NullString
	SSLMode           sql.NullString
	SSLCert           sql.NullString
	SSLKey            sql.NullString
	SSLRootCert       sql.NullString
}
