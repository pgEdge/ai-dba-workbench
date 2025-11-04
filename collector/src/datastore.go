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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pool.GetConnection(ctx)
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

	// Set application name to identify datastore connections
	params["application_name"] = "pgEdge AI Workbench - Metric Storage"

	return buildPostgresConnectionString(params)
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

	// Use the schema manager to apply migrations
	schemaManager := NewSchemaManager()
	if err := schemaManager.Migrate(conn); err != nil {
		return fmt.Errorf("failed to migrate schema: %w", err)
	}

	log.Println("Database schema initialized")
	return nil
}

// GetConnection retrieves a connection from the pool
func (ds *Datastore) GetConnection() (*sql.DB, error) {
	// Create a context with timeout for datastore connections
	// Datastore should be local and fast, so use a shorter timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ds.pool.GetConnection(ctx)
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
               password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
               owner_username, owner_token
        FROM connections
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
			&c.OwnerUsername, &c.OwnerToken,
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
	OwnerUsername     sql.NullString
	OwnerToken        sql.NullString
}
