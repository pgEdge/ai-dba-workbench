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
	"fmt"
	"github.com/pgedge/ai-workbench/pkg/logger"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config interface defines the minimal configuration needed by Datastore
// This avoids importing the main package
type Config interface {
	Validate() error
	GetPgHost() string
	GetPgHostAddr() string
	GetPgDatabase() string
	GetPgUsername() string
	GetPgPassword() string
	GetPgPort() int
	GetPgSSLMode() string
	GetPgSSLCert() string
	GetPgSSLKey() string
	GetPgSSLRootCert() string
	GetDatastorePoolMaxConnections() int
	GetDatastorePoolMaxIdleSeconds() int
}

// Datastore represents a connection to the PostgreSQL datastore
type Datastore struct {
	pool   *pgxpool.Pool
	config Config
}

// NewDatastore initializes the datastore connection
func NewDatastore(config Config) (*Datastore, error) {
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
		ds.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return ds, nil
}

// connect establishes a connection pool to the PostgreSQL datastore
func (ds *Datastore) connect() error {
	connStr := ds.buildConnectionString()

	// Parse the pgx connection config
	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Configure pool settings
	maxConns := ds.config.GetDatastorePoolMaxConnections()
	if maxConns > 2147483647 {
		maxConns = 2147483647 // Limit to int32 max value
	}
	poolConfig.MaxConns = int32(maxConns) // #nosec G115 - safe after bounds check
	poolConfig.MaxConnIdleTime = time.Duration(ds.config.GetDatastorePoolMaxIdleSeconds()) * time.Second
	poolConfig.HealthCheckPeriod = 1 * time.Minute

	// Create the pool
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test the connection pool by pinging
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	ds.pool = pool
	return nil
}

// buildConnectionString builds a PostgreSQL connection string from config
func (ds *Datastore) buildConnectionString() string {
	cfg := ds.config

	// Start with basic connection parameters
	params := make(map[string]string)
	params["dbname"] = cfg.GetPgDatabase()
	params["user"] = cfg.GetPgUsername()

	if cfg.GetPgHostAddr() != "" {
		params["hostaddr"] = cfg.GetPgHostAddr()
	} else if cfg.GetPgHost() != "" {
		params["host"] = cfg.GetPgHost()
	}

	if cfg.GetPgPort() != 0 {
		params["port"] = fmt.Sprintf("%d", cfg.GetPgPort())
	}

	if cfg.GetPgPassword() != "" {
		params["password"] = cfg.GetPgPassword()
	}

	// SSL parameters
	if cfg.GetPgSSLMode() != "" {
		params["sslmode"] = cfg.GetPgSSLMode()
	}
	if cfg.GetPgSSLCert() != "" {
		params["sslcert"] = cfg.GetPgSSLCert()
	}
	if cfg.GetPgSSLKey() != "" {
		params["sslkey"] = cfg.GetPgSSLKey()
	}
	if cfg.GetPgSSLRootCert() != "" {
		params["sslrootcert"] = cfg.GetPgSSLRootCert()
	}

	// Set application name to identify datastore connections
	params["application_name"] = "pgEdge AI DBA Workbench - Metric Storage"

	return buildPostgresConnectionString(params)
}

// initializeSchema creates the necessary database schema if it doesn't exist
func (ds *Datastore) initializeSchema() error {
	logger.Info("Initializing database schema...")

	conn, err := ds.GetConnection()
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer ds.ReturnConnection(conn)

	// Use the schema manager to apply migrations
	schemaManager := NewSchemaManager()
	if err := schemaManager.Migrate(conn); err != nil {
		return fmt.Errorf("failed to migrate schema: %w", err)
	}

	logger.Info("Database schema initialized")
	return nil
}

// GetConnection retrieves a connection from the pool with a default 5-second timeout
func (ds *Datastore) GetConnection() (*pgxpool.Conn, error) {
	// Create a context with timeout for datastore connections
	// Datastore should be local and fast, so use a shorter timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ds.pool.Acquire(ctx)
}

// GetConnectionWithContext retrieves a connection from the pool using the provided context
// This allows callers to specify their own timeout when they need to wait longer
// (e.g., when storing probe metrics during high load)
func (ds *Datastore) GetConnectionWithContext(ctx context.Context) (*pgxpool.Conn, error) {
	return ds.pool.Acquire(ctx)
}

// ReturnConnection returns a connection to the pool
// Note: With pgxpool, connections are released rather than returned
func (ds *Datastore) ReturnConnection(conn *pgxpool.Conn) {
	if conn != nil {
		conn.Release()
	}
}

// Close closes the datastore connection pool
func (ds *Datastore) Close() {
	if ds.pool != nil {
		ds.pool.Close()
	}
}

// GetMonitoredConnections returns all connections that should be monitored
func (ds *Datastore) GetMonitoredConnections() ([]MonitoredConnection, error) {
	conn, err := ds.GetConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer ds.ReturnConnection(conn)

	ctx := context.Background()
	rows, err := conn.Query(ctx, `
        SELECT id, name, host, hostaddr, port, database_name, username,
               password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
               owner_username, owner_token
        FROM connections
        WHERE is_monitored = TRUE
    `)
	if err != nil {
		return nil, fmt.Errorf("failed to query monitored connections: %w", err)
	}
	defer rows.Close()

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
