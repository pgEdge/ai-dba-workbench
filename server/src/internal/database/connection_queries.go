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
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/crypto"
	"github.com/pgedge/ai-workbench/server/internal/config"
)

// MonitoredConnection represents a connection stored in the datastore.
// The MarshalJSON method controls API serialization; sql.NullString
// fields that should appear in JSON responses are converted to plain
// strings (or omitted when NULL).
type MonitoredConnection struct {
	ID                int            `json:"-"`
	Name              string         `json:"-"`
	Description       string         `json:"-"`
	Host              string         `json:"-"`
	HostAddr          sql.NullString `json:"-"`
	Port              int            `json:"-"`
	DatabaseName      string         `json:"-"`
	Username          string         `json:"-"`
	PasswordEncrypted sql.NullString `json:"-"`
	SSLMode           sql.NullString `json:"-"`
	SSLCert           sql.NullString `json:"-"`
	SSLKey            sql.NullString `json:"-"`
	SSLRootCert       sql.NullString `json:"-"`
	OwnerUsername     sql.NullString `json:"-"`
	OwnerToken        sql.NullString `json:"-"`
	IsMonitored       bool           `json:"-"`
	IsShared          bool           `json:"-"`
	MembershipSource  string         `json:"-"`
}

// MarshalJSON serializes MonitoredConnection for API responses.
// Nullable string fields are emitted as plain JSON strings when valid
// and omitted when NULL, so the client receives flat values instead of
// the nested {"String":"...","Valid":true} representation that
// sql.NullString produces by default.
func (mc MonitoredConnection) MarshalJSON() ([]byte, error) {
	type jsonConn struct {
		ID               int     `json:"id"`
		Name             string  `json:"name"`
		Description      string  `json:"description"`
		Host             string  `json:"host"`
		Port             int     `json:"port"`
		DatabaseName     string  `json:"database_name"`
		Username         string  `json:"username"`
		SSLMode          *string `json:"ssl_mode,omitempty"`
		SSLCert          *string `json:"ssl_cert_path,omitempty"`
		SSLKey           *string `json:"ssl_key_path,omitempty"`
		SSLRootCert      *string `json:"ssl_root_cert_path,omitempty"`
		IsMonitored      bool    `json:"is_monitored"`
		IsShared         bool    `json:"is_shared"`
		MembershipSource string  `json:"membership_source,omitempty"`
	}

	out := jsonConn{
		ID:               mc.ID,
		Name:             mc.Name,
		Description:      mc.Description,
		Host:             mc.Host,
		Port:             mc.Port,
		DatabaseName:     mc.DatabaseName,
		Username:         mc.Username,
		IsMonitored:      mc.IsMonitored,
		IsShared:         mc.IsShared,
		MembershipSource: mc.MembershipSource,
	}

	if mc.SSLMode.Valid && mc.SSLMode.String != "" {
		s := mc.SSLMode.String
		out.SSLMode = &s
	}
	if mc.SSLCert.Valid && mc.SSLCert.String != "" {
		s := mc.SSLCert.String
		out.SSLCert = &s
	}
	if mc.SSLKey.Valid && mc.SSLKey.String != "" {
		s := mc.SSLKey.String
		out.SSLKey = &s
	}
	if mc.SSLRootCert.Valid && mc.SSLRootCert.String != "" {
		s := mc.SSLRootCert.String
		out.SSLRootCert = &s
	}

	return json.Marshal(out)
}

// ConnectionListItem is a simplified connection for API responses
type ConnectionListItem struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	DatabaseName     string `json:"database_name"`
	IsMonitored      bool   `json:"is_monitored"`
	IsShared         bool   `json:"is_shared"`
	OwnerUsername    string `json:"owner_username"`
	ClusterID        *int   `json:"cluster_id"`
	MembershipSource string `json:"membership_source"`
}

// DatabaseInfo represents a database on a PostgreSQL server
type DatabaseInfo struct {
	Name     string `json:"name"`
	Owner    string `json:"owner"`
	Encoding string `json:"encoding"`
	Size     string `json:"size"`
}

// ConnectionCreateParams contains parameters for creating a new connection
type ConnectionCreateParams struct {
	Name          string
	Description   *string
	Host          string
	HostAddr      *string
	Port          int
	DatabaseName  string
	Username      string
	Password      string // Will be encrypted
	SSLMode       *string
	SSLCert       *string
	SSLKey        *string
	SSLRootCert   *string
	IsShared      bool
	IsMonitored   bool
	OwnerUsername string // Set from authenticated user
}

// ConnectionUpdateParams contains parameters for updating a connection
// Only non-nil fields will be updated
type ConnectionUpdateParams struct {
	Name         *string
	Description  *string
	Host         *string
	HostAddr     *string
	Port         *int
	DatabaseName *string
	Username     *string
	Password     *string // Will be encrypted if provided
	SSLMode      *string
	SSLCert      *string
	SSLKey       *string
	SSLRootCert  *string
	IsShared     *bool
	IsMonitored  *bool
}

// scanFullConnection scans a row into a MonitoredConnection struct.
// The row must contain columns in this order:
// id, name, description, host, hostaddr, port, database_name, username,
// password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
// owner_username, owner_token, is_monitored, is_shared, membership_source
func scanFullConnection(scanner interface{ Scan(...any) error }) (*MonitoredConnection, error) {
	var conn MonitoredConnection
	err := scanner.Scan(
		&conn.ID, &conn.Name, &conn.Description, &conn.Host, &conn.HostAddr, &conn.Port,
		&conn.DatabaseName, &conn.Username, &conn.PasswordEncrypted,
		&conn.SSLMode, &conn.SSLCert, &conn.SSLKey, &conn.SSLRootCert,
		&conn.OwnerUsername, &conn.OwnerToken, &conn.IsMonitored, &conn.IsShared,
		&conn.MembershipSource,
	)
	if err != nil {
		return nil, err
	}
	return &conn, nil
}

// scanConnectionListItem scans a row into a ConnectionListItem struct.
// The row must contain columns in this order:
// id, name, description, host, port, database_name, is_monitored, is_shared,
// owner_username, cluster_id, membership_source
func scanConnectionListItem(scanner interface{ Scan(...any) error }) (*ConnectionListItem, error) {
	var conn ConnectionListItem
	err := scanner.Scan(
		&conn.ID, &conn.Name, &conn.Description, &conn.Host, &conn.Port,
		&conn.DatabaseName, &conn.IsMonitored, &conn.IsShared, &conn.OwnerUsername,
		&conn.ClusterID, &conn.MembershipSource,
	)
	if err != nil {
		return nil, err
	}
	return &conn, nil
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

	// Apply pool settings with bounds checking to prevent overflow
	if cfg.PoolMaxConns > 0 && cfg.PoolMaxConns <= math.MaxInt32 {
		poolConfig.MaxConns = int32(cfg.PoolMaxConns) //nolint:gosec // G115: bounds checked above
	}
	if cfg.PoolMinConns > 0 && cfg.PoolMinConns <= math.MaxInt32 {
		poolConfig.MinConns = int32(cfg.PoolMinConns) //nolint:gosec // G115: bounds checked above
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
        SELECT id, name, description, host, port, database_name,
               is_monitored, is_shared, COALESCE(owner_username, ''),
               cluster_id, membership_source
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
		conn, err := scanConnectionListItem(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan connection: %w", err)
		}
		connections = append(connections, *conn)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating connections: %w", err)
	}

	return connections, nil
}

// GetConnectionSharingInfo returns the sharing status and owner of a connection
func (d *Datastore) GetConnectionSharingInfo(ctx context.Context, id int) (isShared bool, ownerUsername string, err error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `SELECT is_shared, COALESCE(owner_username, '') FROM connections WHERE id = $1`
	err = d.pool.QueryRow(ctx, query, id).Scan(&isShared, &ownerUsername)
	if err != nil {
		return false, "", fmt.Errorf("failed to get connection sharing info: %w", err)
	}
	return isShared, ownerUsername, nil
}

// GetConnection returns a single connection by ID with decrypted password
func (d *Datastore) GetConnection(ctx context.Context, id int) (*MonitoredConnection, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT id, name, description, host, hostaddr, port, database_name, username,
               password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
               owner_username, owner_token, is_monitored, is_shared,
               membership_source
        FROM connections
        WHERE id = $1
    `

	conn, err := scanFullConnection(d.pool.QueryRow(ctx, query, id))
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	return conn, nil
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
		password, err = crypto.DecryptPassword(conn.PasswordEncrypted.String, d.serverSecret)
		if err != nil {
			return nil, "", fmt.Errorf("failed to decrypt password: %w", err)
		}
	}

	return conn, password, nil
}

// UpdateConnectionName updates a connection's name
func (d *Datastore) UpdateConnectionName(ctx context.Context, id int, name string) (*MonitoredConnection, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        UPDATE connections
        SET name = $2
        WHERE id = $1
        RETURNING id, name, description, host, hostaddr, port, database_name, username,
                  password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
                  owner_username, owner_token, is_monitored, is_shared,
                  membership_source
    `

	conn, err := scanFullConnection(d.pool.QueryRow(ctx, query, id, name))
	if err != nil {
		return nil, fmt.Errorf("failed to update connection: %w", err)
	}

	return conn, nil
}

// CreateConnection creates a new connection in the datastore
func (d *Datastore) CreateConnection(ctx context.Context, params ConnectionCreateParams) (*MonitoredConnection, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Encrypt password using shared crypto package
	var encryptedPassword *string
	if params.Password != "" {
		if d.serverSecret == "" {
			return nil, fmt.Errorf("server secret is required to encrypt password")
		}
		encrypted, err := crypto.EncryptPassword(params.Password, d.serverSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt password: %w", err)
		}
		encryptedPassword = &encrypted
	}

	query := `
        INSERT INTO connections (
            name, description, host, hostaddr, port, database_name, username,
            password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
            owner_username, is_shared, is_monitored
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
        RETURNING id, name, description, host, hostaddr, port, database_name, username,
                  password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
                  owner_username, owner_token, is_monitored, is_shared,
                  membership_source
    `

	conn, err := scanFullConnection(d.pool.QueryRow(ctx, query,
		params.Name, params.Description, params.Host, params.HostAddr, params.Port,
		params.DatabaseName, params.Username, encryptedPassword, params.SSLMode,
		params.SSLCert, params.SSLKey, params.SSLRootCert, params.OwnerUsername,
		params.IsShared, params.IsMonitored,
	))
	if err != nil {
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	return conn, nil
}

// DeleteConnection deletes a connection by ID
func (d *Datastore) DeleteConnection(ctx context.Context, id int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `DELETE FROM connections WHERE id = $1`

	result, err := d.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete connection: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrConnectionNotFound
	}

	return nil
}

// UpdateConnectionFull updates a connection with the provided parameters
// Only non-nil fields are updated
func (d *Datastore) UpdateConnectionFull(ctx context.Context, id int, params ConnectionUpdateParams) (*MonitoredConnection, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Build dynamic update query
	setClauses := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []any{}
	argNum := 1

	if params.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, *params.Name)
		argNum++
	}
	if params.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argNum))
		args = append(args, *params.Description)
		argNum++
	}
	if params.Host != nil {
		setClauses = append(setClauses, fmt.Sprintf("host = $%d", argNum))
		args = append(args, *params.Host)
		argNum++
	}
	if params.HostAddr != nil {
		setClauses = append(setClauses, fmt.Sprintf("hostaddr = $%d", argNum))
		args = append(args, *params.HostAddr)
		argNum++
	}
	if params.Port != nil {
		setClauses = append(setClauses, fmt.Sprintf("port = $%d", argNum))
		args = append(args, *params.Port)
		argNum++
	}
	if params.DatabaseName != nil {
		setClauses = append(setClauses, fmt.Sprintf("database_name = $%d", argNum))
		args = append(args, *params.DatabaseName)
		argNum++
	}
	if params.Username != nil {
		setClauses = append(setClauses, fmt.Sprintf("username = $%d", argNum))
		args = append(args, *params.Username)
		argNum++
	}
	if params.Password != nil {
		if d.serverSecret == "" {
			return nil, fmt.Errorf("server secret is required to encrypt password")
		}
		encrypted, err := crypto.EncryptPassword(*params.Password, d.serverSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt password: %w", err)
		}
		setClauses = append(setClauses, fmt.Sprintf("password_encrypted = $%d", argNum))
		args = append(args, encrypted)
		argNum++
	}
	if params.SSLMode != nil {
		setClauses = append(setClauses, fmt.Sprintf("sslmode = $%d", argNum))
		args = append(args, *params.SSLMode)
		argNum++
	}
	if params.SSLCert != nil {
		setClauses = append(setClauses, fmt.Sprintf("sslcert = $%d", argNum))
		args = append(args, *params.SSLCert)
		argNum++
	}
	if params.SSLKey != nil {
		setClauses = append(setClauses, fmt.Sprintf("sslkey = $%d", argNum))
		args = append(args, *params.SSLKey)
		argNum++
	}
	if params.SSLRootCert != nil {
		setClauses = append(setClauses, fmt.Sprintf("sslrootcert = $%d", argNum))
		args = append(args, *params.SSLRootCert)
		argNum++
	}
	if params.IsShared != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_shared = $%d", argNum))
		args = append(args, *params.IsShared)
		argNum++
	}
	if params.IsMonitored != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_monitored = $%d", argNum))
		args = append(args, *params.IsMonitored)
		argNum++
	}

	// Add the ID parameter
	args = append(args, id)

	query := fmt.Sprintf(`
        UPDATE connections
        SET %s
        WHERE id = $%d
        RETURNING id, name, description, host, hostaddr, port, database_name, username,
                  password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
                  owner_username, owner_token, is_monitored, is_shared,
                  membership_source
    `, strings.Join(setClauses, ", "), argNum)

	conn, err := scanFullConnection(d.pool.QueryRow(ctx, query, args...))
	if err != nil {
		return nil, fmt.Errorf("failed to update connection: %w", err)
	}

	// Clear connection error so it shows as initializing until the collector re-probes
	_, _ = d.pool.Exec(ctx, "UPDATE connections SET connection_error = NULL WHERE id = $1", id) //nolint:errcheck // best-effort cleanup

	return conn, nil
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

	// Collect query parameters so per-connection SSL settings flow
	// through to the pgx driver (e.g., verify-full with a custom CA).
	// Use "?" for the first parameter and "&" for subsequent ones.
	sep := "?"
	appendParam := func(key, value string) {
		connStr += sep + key + "=" + url.QueryEscape(value)
		sep = "&"
	}

	// Add SSL mode
	if conn.SSLMode.Valid && conn.SSLMode.String != "" {
		appendParam("sslmode", conn.SSLMode.String)
	}

	// Add SSL root certificate path (CA bundle used to verify the server)
	if conn.SSLRootCert.Valid && conn.SSLRootCert.String != "" {
		appendParam("sslrootcert", conn.SSLRootCert.String)
	}

	// Add SSL client certificate path (mutual TLS)
	if conn.SSLCert.Valid && conn.SSLCert.String != "" {
		appendParam("sslcert", conn.SSLCert.String)
	}

	// Add SSL client key path (mutual TLS)
	if conn.SSLKey.Valid && conn.SSLKey.String != "" {
		appendParam("sslkey", conn.SSLKey.String)
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
