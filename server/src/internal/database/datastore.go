/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/config"
)

// MonitoredConnection represents a connection stored in the datastore
type MonitoredConnection struct {
	ID                int            `json:"id"`
	Name              string         `json:"name"`
	Host              string         `json:"host"`
	HostAddr          sql.NullString `json:"-"`
	Port              int            `json:"port"`
	DatabaseName      string         `json:"database_name"`
	Username          string         `json:"username"`
	PasswordEncrypted sql.NullString `json:"-"`
	SSLMode           sql.NullString `json:"ssl_mode,omitempty"`
	SSLCert           sql.NullString `json:"-"`
	SSLKey            sql.NullString `json:"-"`
	SSLRootCert       sql.NullString `json:"-"`
	OwnerUsername     sql.NullString `json:"-"`
	OwnerToken        sql.NullString `json:"-"`
	IsMonitored       bool           `json:"is_monitored"`
	IsShared          bool           `json:"is_shared"`
}

// ConnectionListItem is a simplified connection for API responses
type ConnectionListItem struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	DatabaseName string `json:"database_name"`
	IsMonitored  bool   `json:"is_monitored"`
}

// DatabaseInfo represents a database on a PostgreSQL server
type DatabaseInfo struct {
	Name     string `json:"name"`
	Owner    string `json:"owner"`
	Encoding string `json:"encoding"`
	Size     string `json:"size"`
}

// Datastore manages the connection to the collector's datastore database
type Datastore struct {
	pool         *pgxpool.Pool
	serverSecret string
	mu           sync.RWMutex
}

// NewDatastore creates a new datastore connection
func NewDatastore(cfg *config.DatabaseConfig, serverSecret string) (*Datastore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("database configuration is required")
	}

	connStr := cfg.BuildConnectionString()

	// Parse and configure pool
	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Set application name
	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "pgEdge AI DBA Workbench - Server"

	// Apply pool settings
	if cfg.PoolMaxConns > 0 {
		poolConfig.MaxConns = int32(cfg.PoolMaxConns)
	}
	if cfg.PoolMinConns > 0 {
		poolConfig.MinConns = int32(cfg.PoolMinConns)
	}
	if cfg.PoolMaxConnIdleTime != "" {
		idleTime, err := time.ParseDuration(cfg.PoolMaxConnIdleTime)
		if err != nil {
			return nil, fmt.Errorf("invalid pool_max_conn_idle_time: %w", err)
		}
		poolConfig.MaxConnIdleTime = idleTime
	}

	// Create pool
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to connect to datastore: %w", err)
	}

	return &Datastore{
		pool:         pool,
		serverSecret: serverSecret,
	}, nil
}

// Close closes the datastore connection pool
func (d *Datastore) Close() {
	if d.pool != nil {
		d.pool.Close()
	}
}

// GetAllConnections returns all connections from the datastore
func (d *Datastore) GetAllConnections(ctx context.Context) ([]ConnectionListItem, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT id, name, host, port, database_name, is_monitored
        FROM connections
        ORDER BY name
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections: %w", err)
	}
	defer rows.Close()

	var connections []ConnectionListItem
	for rows.Next() {
		var conn ConnectionListItem
		if err := rows.Scan(&conn.ID, &conn.Name, &conn.Host, &conn.Port, &conn.DatabaseName, &conn.IsMonitored); err != nil {
			return nil, fmt.Errorf("failed to scan connection: %w", err)
		}
		connections = append(connections, conn)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating connections: %w", err)
	}

	return connections, nil
}

// GetConnection returns a single connection by ID with decrypted password
func (d *Datastore) GetConnection(ctx context.Context, id int) (*MonitoredConnection, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT id, name, host, hostaddr, port, database_name, username,
               password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
               owner_username, owner_token, is_monitored, is_shared
        FROM connections
        WHERE id = $1
    `

	var conn MonitoredConnection
	err := d.pool.QueryRow(ctx, query, id).Scan(
		&conn.ID, &conn.Name, &conn.Host, &conn.HostAddr, &conn.Port,
		&conn.DatabaseName, &conn.Username, &conn.PasswordEncrypted,
		&conn.SSLMode, &conn.SSLCert, &conn.SSLKey, &conn.SSLRootCert,
		&conn.OwnerUsername, &conn.OwnerToken, &conn.IsMonitored, &conn.IsShared,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	return &conn, nil
}

// GetConnectionWithPassword returns a connection with the password decrypted
func (d *Datastore) GetConnectionWithPassword(ctx context.Context, id int) (*MonitoredConnection, string, error) {
	conn, err := d.GetConnection(ctx, id)
	if err != nil {
		return nil, "", err
	}

	// Decrypt password if present
	var password string
	if conn.PasswordEncrypted.Valid && conn.PasswordEncrypted.String != "" {
		if d.serverSecret == "" {
			return nil, "", fmt.Errorf("server secret is required to decrypt password")
		}
		// Use owner_username as salt if present (matches collector encryption),
		// otherwise fall back to connection username
		salt := conn.Username
		if conn.OwnerUsername.Valid && conn.OwnerUsername.String != "" {
			salt = conn.OwnerUsername.String
		}
		password, err = DecryptPassword(conn.PasswordEncrypted.String, d.serverSecret, salt)
		if err != nil {
			return nil, "", fmt.Errorf("failed to decrypt password: %w", err)
		}
	}

	return conn, password, nil
}

// BuildConnectionString creates a PostgreSQL connection string from a MonitoredConnection
func (d *Datastore) BuildConnectionString(conn *MonitoredConnection, password string, databaseOverride string) string {
	// Use override database if specified, otherwise use connection's default
	database := conn.DatabaseName
	if databaseOverride != "" {
		database = databaseOverride
	}

	// Start with user (URL-encode in case of special characters)
	connStr := fmt.Sprintf("postgres://%s", url.QueryEscape(conn.Username))

	// Add password if present (URL-encode to handle special characters)
	if password != "" {
		connStr += ":" + url.QueryEscape(password)
	}

	// Use hostaddr if available, otherwise host
	host := conn.Host
	if conn.HostAddr.Valid && conn.HostAddr.String != "" {
		host = conn.HostAddr.String
	}

	connStr += fmt.Sprintf("@%s:%d/%s", host, conn.Port, database)

	// Add SSL mode
	if conn.SSLMode.Valid && conn.SSLMode.String != "" {
		connStr += "?sslmode=" + conn.SSLMode.String
	}

	return connStr
}

// ListDatabases returns a list of databases on a monitored server
func (d *Datastore) ListDatabases(ctx context.Context, connectionID int) ([]DatabaseInfo, error) {
	// First get the connection info
	conn, password, err := d.GetConnectionWithPassword(ctx, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Build connection string to template1 (always exists)
	connStr := d.BuildConnectionString(conn, password, "template1")

	// Connect temporarily
	tempPool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	defer tempPool.Close()

	// Query databases
	query := `
        SELECT
            d.datname AS name,
            pg_catalog.pg_get_userbyid(d.datdba) AS owner,
            pg_catalog.pg_encoding_to_char(d.encoding) AS encoding,
            pg_catalog.pg_size_pretty(pg_catalog.pg_database_size(d.datname)) AS size
        FROM pg_catalog.pg_database d
        WHERE d.datallowconn = true
          AND d.datname NOT IN ('template0', 'template1')
        ORDER BY d.datname
    `

	rows, err := tempPool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query databases: %w", err)
	}
	defer rows.Close()

	var databases []DatabaseInfo
	for rows.Next() {
		var db DatabaseInfo
		if err := rows.Scan(&db.Name, &db.Owner, &db.Encoding, &db.Size); err != nil {
			return nil, fmt.Errorf("failed to scan database: %w", err)
		}
		databases = append(databases, db)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating databases: %w", err)
	}

	return databases, nil
}

// GetPool returns the underlying connection pool (for future metrics access)
func (d *Datastore) GetPool() *pgxpool.Pool {
	return d.pool
}
