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
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/crypto"
	"github.com/pgedge/ai-workbench/pkg/logger"
	"github.com/pgedge/ai-workbench/server/internal/config"
)

// Sentinel errors for datastore operations
var (
	// ErrConnectionNotFound is returned when a connection is not found
	ErrConnectionNotFound = errors.New("connection not found")

	// ErrClusterGroupNotFound is returned when a cluster group is not found
	ErrClusterGroupNotFound = errors.New("cluster group not found")

	// ErrClusterNotFound is returned when a cluster is not found
	ErrClusterNotFound = errors.New("cluster not found")

	// ErrAlertNotFound is returned when an alert is not found
	ErrAlertNotFound = errors.New("alert not found")
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

// EstateServerSummary holds summary information for a single server
// in the estate snapshot
type EstateServerSummary struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	Role             string `json:"role"`
	ActiveAlertCount int    `json:"active_alert_count"`
}

// EstateAlertSummary holds a summary of a single active alert
type EstateAlertSummary struct {
	Title      string `json:"title"`
	ServerName string `json:"server_name"`
	Severity   string `json:"severity"`
}

// EstateBlackoutSummary holds a summary of a blackout period
type EstateBlackoutSummary struct {
	Scope     string    `json:"scope"`
	Reason    string    `json:"reason"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

// EstateEventSummary holds a summary of a recent timeline event
type EstateEventSummary struct {
	EventType  string    `json:"event_type"`
	ServerName string    `json:"server_name"`
	OccurredAt time.Time `json:"occurred_at"`
	Severity   string    `json:"severity"`
	Title      string    `json:"title"`
	Summary    string    `json:"summary"`
}

// EstateSnapshot contains a point-in-time summary of the entire
// monitored estate for the AI overview
type EstateSnapshot struct {
	Timestamp         time.Time               `json:"timestamp"`
	ServerTotal       int                     `json:"server_total"`
	ServerOnline      int                     `json:"server_online"`
	ServerOffline     int                     `json:"server_offline"`
	ServerWarning     int                     `json:"server_warning"`
	Servers           []EstateServerSummary   `json:"servers"`
	AlertTotal        int                     `json:"alert_total"`
	AlertCritical     int                     `json:"alert_critical"`
	AlertWarning      int                     `json:"alert_warning"`
	AlertInfo         int                     `json:"alert_info"`
	TopAlerts         []EstateAlertSummary    `json:"top_alerts"`
	ActiveBlackouts   []EstateBlackoutSummary `json:"active_blackouts"`
	UpcomingBlackouts []EstateBlackoutSummary `json:"upcoming_blackouts"`
	RecentEvents      []EstateEventSummary    `json:"recent_events"`
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
		var conn ConnectionListItem
		if err := rows.Scan(&conn.ID, &conn.Name, &conn.Description, &conn.Host, &conn.Port, &conn.DatabaseName, &conn.IsMonitored, &conn.IsShared, &conn.OwnerUsername, &conn.ClusterID, &conn.MembershipSource); err != nil {
			return nil, fmt.Errorf("failed to scan connection: %w", err)
		}
		connections = append(connections, conn)
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

	var conn MonitoredConnection
	err := d.pool.QueryRow(ctx, query, id).Scan(
		&conn.ID, &conn.Name, &conn.Description, &conn.Host, &conn.HostAddr, &conn.Port,
		&conn.DatabaseName, &conn.Username, &conn.PasswordEncrypted,
		&conn.SSLMode, &conn.SSLCert, &conn.SSLKey, &conn.SSLRootCert,
		&conn.OwnerUsername, &conn.OwnerToken, &conn.IsMonitored, &conn.IsShared,
		&conn.MembershipSource,
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

	var conn MonitoredConnection
	err := d.pool.QueryRow(ctx, query, id, name).Scan(
		&conn.ID, &conn.Name, &conn.Description, &conn.Host, &conn.HostAddr, &conn.Port,
		&conn.DatabaseName, &conn.Username, &conn.PasswordEncrypted,
		&conn.SSLMode, &conn.SSLCert, &conn.SSLKey, &conn.SSLRootCert,
		&conn.OwnerUsername, &conn.OwnerToken, &conn.IsMonitored, &conn.IsShared,
		&conn.MembershipSource,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update connection: %w", err)
	}

	return &conn, nil
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

	var conn MonitoredConnection
	err := d.pool.QueryRow(ctx, query,
		params.Name, params.Description, params.Host, params.HostAddr, params.Port,
		params.DatabaseName, params.Username, encryptedPassword, params.SSLMode,
		params.SSLCert, params.SSLKey, params.SSLRootCert, params.OwnerUsername,
		params.IsShared, params.IsMonitored,
	).Scan(
		&conn.ID, &conn.Name, &conn.Description, &conn.Host, &conn.HostAddr, &conn.Port,
		&conn.DatabaseName, &conn.Username, &conn.PasswordEncrypted,
		&conn.SSLMode, &conn.SSLCert, &conn.SSLKey, &conn.SSLRootCert,
		&conn.OwnerUsername, &conn.OwnerToken, &conn.IsMonitored, &conn.IsShared,
		&conn.MembershipSource,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	return &conn, nil
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

	var conn MonitoredConnection
	err := d.pool.QueryRow(ctx, query, args...).Scan(
		&conn.ID, &conn.Name, &conn.Description, &conn.Host, &conn.HostAddr, &conn.Port,
		&conn.DatabaseName, &conn.Username, &conn.PasswordEncrypted,
		&conn.SSLMode, &conn.SSLCert, &conn.SSLKey, &conn.SSLRootCert,
		&conn.OwnerUsername, &conn.OwnerToken, &conn.IsMonitored, &conn.IsShared,
		&conn.MembershipSource,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update connection: %w", err)
	}

	// Clear connection error so it shows as initializing until the collector re-probes
	_, _ = d.pool.Exec(ctx, "UPDATE connections SET connection_error = NULL WHERE id = $1", id) //nolint:errcheck // best-effort cleanup

	return &conn, nil
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

// ClusterGroup represents a group of clusters
type ClusterGroup struct {
	ID            int            `json:"id"`
	Name          string         `json:"name"`
	Description   *string        `json:"description,omitempty"`
	OwnerUsername sql.NullString `json:"owner_username,omitempty"`
	OwnerToken    sql.NullString `json:"-"` // Never expose token in JSON
	IsShared      bool           `json:"is_shared"`
	IsDefault     bool           `json:"is_default"`
	AutoGroupKey  sql.NullString `json:"auto_group_key,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// Cluster represents a database cluster containing servers
type Cluster struct {
	ID              int            `json:"id"`
	GroupID         sql.NullInt32  `json:"group_id,omitempty"`
	Name            string         `json:"name"`
	Description     *string        `json:"description,omitempty"`
	ReplicationType *string        `json:"replication_type,omitempty"`
	AutoClusterKey  sql.NullString `json:"auto_cluster_key,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// ServerInfo represents a server in the cluster hierarchy with status
type ServerInfo struct {
	ID               int     `json:"id"`
	Name             string  `json:"name"`
	Host             string  `json:"host"`
	Port             int     `json:"port"`
	Status           string  `json:"status"`
	ConnectionError  *string `json:"connection_error,omitempty"`
	Role             *string `json:"role,omitempty"`
	MembershipSource string  `json:"membership_source,omitempty"`
	Database         string  `json:"database_name,omitempty"`
}

// ClusterWithServers represents a cluster with its servers
type ClusterWithServers struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Servers []ServerInfo `json:"servers"`
}

// ClusterGroupWithClusters represents a cluster group with its clusters
type ClusterGroupWithClusters struct {
	ID       string               `json:"id"`
	Name     string               `json:"name"`
	Clusters []ClusterWithServers `json:"clusters"`
}

// GetClusterGroups returns all cluster groups
func (d *Datastore) GetClusterGroups(ctx context.Context) ([]ClusterGroup, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT id, name, description, owner_username, owner_token, is_shared,
               created_at, updated_at
        FROM cluster_groups
        ORDER BY name
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster groups: %w", err)
	}
	defer rows.Close()

	var groups []ClusterGroup
	for rows.Next() {
		var g ClusterGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.OwnerUsername,
			&g.OwnerToken, &g.IsShared, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan cluster group: %w", err)
		}
		groups = append(groups, g)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating cluster groups: %w", err)
	}

	return groups, nil
}

// GetClusterGroup returns a single cluster group by ID
func (d *Datastore) GetClusterGroup(ctx context.Context, id int) (*ClusterGroup, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT id, name, description, owner_username, owner_token, is_shared,
               created_at, updated_at
        FROM cluster_groups
        WHERE id = $1
    `

	var g ClusterGroup
	err := d.pool.QueryRow(ctx, query, id).Scan(
		&g.ID, &g.Name, &g.Description, &g.OwnerUsername, &g.OwnerToken,
		&g.IsShared, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster group: %w", err)
	}

	return &g, nil
}

// CreateClusterGroup creates a new cluster group
func (d *Datastore) CreateClusterGroup(ctx context.Context, name string, description *string) (*ClusterGroup, error) {
	return d.CreateClusterGroupWithOwner(ctx, name, description, nil, true)
}

// CreateClusterGroupWithOwner creates a new cluster group with optional owner
func (d *Datastore) CreateClusterGroupWithOwner(ctx context.Context, name string, description *string, ownerUsername *string, isShared bool) (*ClusterGroup, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        INSERT INTO cluster_groups (name, description, owner_username, is_shared)
        VALUES ($1, $2, $3, $4)
        RETURNING id, name, description, owner_username, owner_token, is_shared,
                  created_at, updated_at
    `

	var g ClusterGroup
	err := d.pool.QueryRow(ctx, query, name, description, ownerUsername, isShared).Scan(
		&g.ID, &g.Name, &g.Description, &g.OwnerUsername, &g.OwnerToken,
		&g.IsShared, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster group: %w", err)
	}

	return &g, nil
}

// UpdateClusterGroup updates an existing cluster group
func (d *Datastore) UpdateClusterGroup(ctx context.Context, id int, name string, description *string) (*ClusterGroup, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        UPDATE cluster_groups
        SET name = $2, description = $3, updated_at = CURRENT_TIMESTAMP
        WHERE id = $1
        RETURNING id, name, description, owner_username, owner_token, is_shared,
                  created_at, updated_at
    `

	var g ClusterGroup
	err := d.pool.QueryRow(ctx, query, id, name, description).Scan(
		&g.ID, &g.Name, &g.Description, &g.OwnerUsername, &g.OwnerToken,
		&g.IsShared, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update cluster group: %w", err)
	}

	return &g, nil
}

// DeleteClusterGroup deletes a cluster group by ID
func (d *Datastore) DeleteClusterGroup(ctx context.Context, id int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `DELETE FROM cluster_groups WHERE id = $1`

	result, err := d.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete cluster group: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrClusterGroupNotFound
	}

	return nil
}

// GetClustersInGroup returns all clusters in a group
func (d *Datastore) GetClustersInGroup(ctx context.Context, groupID int) ([]Cluster, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT id, group_id, name, description, replication_type, auto_cluster_key, created_at, updated_at
        FROM clusters
        WHERE group_id = $1
          AND dismissed = FALSE
        ORDER BY name
    `

	rows, err := d.pool.Query(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to query clusters: %w", err)
	}
	defer rows.Close()

	var clusters []Cluster
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.ID, &c.GroupID, &c.Name, &c.Description, &c.ReplicationType, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan cluster: %w", err)
		}
		clusters = append(clusters, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating clusters: %w", err)
	}

	return clusters, nil
}

// GetCluster returns a single cluster by ID. Dismissed (soft-deleted)
// clusters are excluded so callers never surface them to end users. Code
// paths that need to see dismissed rows (for example, the dismiss-aware
// auto-detection upsert) must query the clusters table directly rather
// than going through this helper.
func (d *Datastore) GetCluster(ctx context.Context, id int) (*Cluster, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT id, group_id, name, description, replication_type, auto_cluster_key, created_at, updated_at
        FROM clusters
        WHERE id = $1
          AND dismissed = FALSE
    `

	var c Cluster
	err := d.pool.QueryRow(ctx, query, id).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.ReplicationType, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster: %w", err)
	}

	return &c, nil
}

// CreateCluster creates a new cluster
func (d *Datastore) CreateCluster(ctx context.Context, groupID int, name string, description *string) (*Cluster, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        INSERT INTO clusters (group_id, name, description)
        VALUES ($1, $2, $3)
        RETURNING id, group_id, name, description, replication_type, auto_cluster_key, created_at, updated_at
    `

	var c Cluster
	err := d.pool.QueryRow(ctx, query, groupID, name, description).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.ReplicationType, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster: %w", err)
	}

	return &c, nil
}

// UpdateCluster updates an existing cluster
func (d *Datastore) UpdateCluster(ctx context.Context, id int, groupID int, name string, description *string) (*Cluster, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        UPDATE clusters
        SET group_id = $2, name = $3, description = $4, updated_at = CURRENT_TIMESTAMP
        WHERE id = $1
        RETURNING id, group_id, name, description, replication_type, auto_cluster_key, created_at, updated_at
    `

	var c Cluster
	err := d.pool.QueryRow(ctx, query, id, groupID, name, description).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.ReplicationType, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update cluster: %w", err)
	}

	return &c, nil
}

// UpdateClusterPartial updates only the provided fields of a cluster.
// Supports partial updates: name, group_id, description - any can be omitted.
func (d *Datastore) UpdateClusterPartial(ctx context.Context, id int, groupID *int, name string, description *string, replicationType *string) (*Cluster, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Build dynamic update based on what's provided
	setClauses := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []any{}
	argNum := 1

	if name != "" {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, name)
		argNum++
	}

	if groupID != nil {
		setClauses = append(setClauses, fmt.Sprintf("group_id = $%d", argNum))
		args = append(args, *groupID)
		argNum++
	}

	if description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argNum))
		args = append(args, *description)
		argNum++
	}

	if replicationType != nil {
		setClauses = append(setClauses, fmt.Sprintf("replication_type = $%d", argNum))
		args = append(args, *replicationType)
		argNum++
	}

	// Add the cluster ID for the WHERE clause
	args = append(args, id)

	query := fmt.Sprintf(`
        UPDATE clusters
        SET %s
        WHERE id = $%d
        RETURNING id, group_id, name, description, replication_type, auto_cluster_key, created_at, updated_at
    `, strings.Join(setClauses, ", "), argNum)

	var c Cluster
	err := d.pool.QueryRow(ctx, query, args...).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.ReplicationType, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update cluster: %w", err)
	}

	return &c, nil
}

// DeleteCluster deletes a cluster by ID. Auto-detected clusters (those
// with auto_cluster_key set) are soft-deleted by setting dismissed=TRUE
// so that auto-detection does not recreate them. User-created clusters
// (no auto_cluster_key) are hard-deleted.
func (d *Datastore) DeleteCluster(ctx context.Context, id int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check whether the cluster is auto-detected
	var hasAutoKey bool
	err := d.pool.QueryRow(ctx,
		`SELECT auto_cluster_key IS NOT NULL FROM clusters WHERE id = $1`,
		id,
	).Scan(&hasAutoKey)
	if err != nil {
		return ErrClusterNotFound
	}

	if hasAutoKey {
		// Soft-delete: mark as dismissed so auto-detection skips it
		_, err = d.pool.Exec(ctx,
			`UPDATE clusters SET dismissed = TRUE, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
			id,
		)
		if err != nil {
			return fmt.Errorf("failed to dismiss cluster: %w", err)
		}

		// Detach connections since the FK ON DELETE SET NULL won't
		// fire for a soft delete
		_, err = d.pool.Exec(ctx,
			`UPDATE connections SET cluster_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE cluster_id = $1`,
			id,
		)
		if err != nil {
			return fmt.Errorf("failed to detach connections from dismissed cluster: %w", err)
		}
	} else {
		// Hard-delete user-created clusters
		result, err := d.pool.Exec(ctx, `DELETE FROM clusters WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("failed to delete cluster: %w", err)
		}
		if result.RowsAffected() == 0 {
			return ErrClusterNotFound
		}
	}

	return nil
}

// GetClusterOverrides returns a map of auto_cluster_key -> clusterOverride
// for all clusters that have an auto_cluster_key set. This is used to
// apply custom names and descriptions to auto-detected clusters in the
// topology view.
func (d *Datastore) GetClusterOverrides(ctx context.Context) (map[string]clusterOverride, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.getClusterOverridesInternal(ctx)
}

// getClusterOverridesInternal is the lock-free internal implementation
func (d *Datastore) getClusterOverridesInternal(ctx context.Context) (map[string]clusterOverride, error) {
	query := `
        SELECT auto_cluster_key, name, COALESCE(description, '') as description
        FROM clusters
        WHERE auto_cluster_key IS NOT NULL
          AND dismissed = FALSE
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster overrides: %w", err)
	}
	defer rows.Close()

	overrides := make(map[string]clusterOverride)
	for rows.Next() {
		var key, name, description string
		if err := rows.Scan(&key, &name, &description); err != nil {
			return nil, fmt.Errorf("failed to scan cluster override: %w", err)
		}
		overrides[key] = clusterOverride{Name: name, Description: description}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating cluster overrides: %w", err)
	}

	return overrides, nil
}

// UpsertClusterByAutoKey creates or updates a cluster by its auto_cluster_key.
// This is used when users rename auto-detected clusters.
func (d *Datastore) UpsertClusterByAutoKey(ctx context.Context, autoKey, name string) (*Cluster, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Use INSERT ... ON CONFLICT to upsert; clear dismissed on explicit rename
	query := `
        INSERT INTO clusters (name, auto_cluster_key)
        VALUES ($1, $2)
        ON CONFLICT (auto_cluster_key)
        DO UPDATE SET name = EXCLUDED.name, dismissed = FALSE, updated_at = CURRENT_TIMESTAMP
        RETURNING id, group_id, name, description, replication_type, auto_cluster_key, created_at, updated_at
    `

	var c Cluster
	err := d.pool.QueryRow(ctx, query, name, autoKey).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.ReplicationType, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert cluster by auto key: %w", err)
	}

	return &c, nil
}

// UpsertAutoDetectedCluster creates or updates an auto-detected cluster.
// Supports renaming (name), moving to a different group (groupID), or both.
// At least one of name or groupID must be provided.
func (d *Datastore) UpsertAutoDetectedCluster(ctx context.Context, autoKey string, name string, description *string, groupID *int) (*Cluster, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if cluster already exists
	var existingCluster Cluster
	checkQuery := `
        SELECT id, group_id, name, description, replication_type, auto_cluster_key, created_at, updated_at
        FROM clusters
        WHERE auto_cluster_key = $1
    `
	err := d.pool.QueryRow(ctx, checkQuery, autoKey).Scan(
		&existingCluster.ID, &existingCluster.GroupID, &existingCluster.Name,
		&existingCluster.Description, &existingCluster.ReplicationType, &existingCluster.AutoClusterKey,
		&existingCluster.CreatedAt, &existingCluster.UpdatedAt,
	)

	if err != nil {
		// Cluster doesn't exist, create new one
		// For new clusters, name is required
		if name == "" {
			return nil, fmt.Errorf("name is required when creating a new cluster")
		}

		insertQuery := `
            INSERT INTO clusters (name, description, auto_cluster_key, group_id)
            VALUES ($1, $2, $3, $4)
            RETURNING id, group_id, name, description, replication_type, auto_cluster_key, created_at, updated_at
        `
		var c Cluster
		err := d.pool.QueryRow(ctx, insertQuery, name, description, autoKey, groupID).Scan(
			&c.ID, &c.GroupID, &c.Name, &c.Description, &c.ReplicationType, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create cluster: %w", err)
		}
		return &c, nil
	}

	// Cluster exists, update it.
	// Build dynamic update based on what's provided. Preserve the
	// dismissed flag here: if auto-detection (or any repeat call with the
	// same auto_cluster_key) rediscovers a cluster the user previously
	// dismissed, we must not silently un-dismiss it. Issue #36. Users
	// that want to restore a dismissed auto-detected cluster go through
	// UpsertClusterByAutoKey (rename) or UpdateCluster / UpdateClusterPartial,
	// which clear dismissed explicitly.
	setClauses := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []any{}
	argNum := 1

	if name != "" {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, name)
		argNum++
	}

	if description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argNum))
		args = append(args, *description)
		argNum++
	}

	if groupID != nil {
		setClauses = append(setClauses, fmt.Sprintf("group_id = $%d", argNum))
		args = append(args, *groupID)
		argNum++
	}

	// Add the auto_cluster_key for the WHERE clause
	args = append(args, autoKey)

	updateQuery := fmt.Sprintf(`
        UPDATE clusters
        SET %s
        WHERE auto_cluster_key = $%d
        RETURNING id, group_id, name, description, replication_type, auto_cluster_key, created_at, updated_at
    `, strings.Join(setClauses, ", "), argNum)

	var c Cluster
	err = d.pool.QueryRow(ctx, updateQuery, args...).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.ReplicationType, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update cluster: %w", err)
	}

	return &c, nil
}

// GetGroupOverrides returns a map of auto_group_key -> custom name
// for all cluster groups that have an auto_group_key set. This is used to
// apply custom names to auto-detected groups in the topology view.
func (d *Datastore) GetGroupOverrides(ctx context.Context) (map[string]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.getGroupOverridesInternal(ctx)
}

// getGroupOverridesInternal is the lock-free internal implementation
func (d *Datastore) getGroupOverridesInternal(ctx context.Context) (map[string]string, error) {
	query := `
        SELECT auto_group_key, name
        FROM cluster_groups
        WHERE auto_group_key IS NOT NULL
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query group overrides: %w", err)
	}
	defer rows.Close()

	overrides := make(map[string]string)
	for rows.Next() {
		var key, name string
		if err := rows.Scan(&key, &name); err != nil {
			return nil, fmt.Errorf("failed to scan group override: %w", err)
		}
		overrides[key] = name
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating group overrides: %w", err)
	}

	return overrides, nil
}

// defaultGroupInfo holds basic info about the default group
type defaultGroupInfo struct {
	ID   int
	Name string
}

// getDefaultGroupInternal returns the default group info (the one with is_default=true)
func (d *Datastore) getDefaultGroupInternal(ctx context.Context) (*defaultGroupInfo, error) {
	query := `
        SELECT id, name
        FROM cluster_groups
        WHERE is_default = TRUE
        LIMIT 1
    `

	var info defaultGroupInfo
	err := d.pool.QueryRow(ctx, query).Scan(&info.ID, &info.Name)
	if err != nil {
		if err == sql.ErrNoRows {
			// No default group exists yet - return a fallback
			// This shouldn't happen after migration 16, but handle gracefully
			return &defaultGroupInfo{ID: 0, Name: "Servers/Clusters"}, nil
		}
		return nil, fmt.Errorf("failed to query default group: %w", err)
	}

	return &info, nil
}

// GetDefaultGroupID returns the ID of the default group
func (d *Datastore) GetDefaultGroupID(ctx context.Context) (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	info, err := d.getDefaultGroupInternal(ctx)
	if err != nil {
		return 0, err
	}
	return info.ID, nil
}

// UpsertGroupByAutoKey creates or updates a cluster group by its auto_group_key.
// This is used when users rename auto-detected groups like "Servers/Clusters".
func (d *Datastore) UpsertGroupByAutoKey(ctx context.Context, autoKey, name string) (*ClusterGroup, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Use INSERT ... ON CONFLICT to upsert
	query := `
        INSERT INTO cluster_groups (name, auto_group_key, is_shared)
        VALUES ($1, $2, true)
        ON CONFLICT (auto_group_key)
        DO UPDATE SET name = EXCLUDED.name, updated_at = CURRENT_TIMESTAMP
        RETURNING id, name, description, owner_username, owner_token, is_shared, auto_group_key, created_at, updated_at
    `

	var g ClusterGroup
	err := d.pool.QueryRow(ctx, query, name, autoKey).Scan(
		&g.ID, &g.Name, &g.Description, &g.OwnerUsername, &g.OwnerToken, &g.IsShared, &g.AutoGroupKey, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert group by auto key: %w", err)
	}

	return &g, nil
}

// GetServersInCluster returns all servers (connections) in a cluster with status
func (d *Datastore) GetServersInCluster(ctx context.Context, clusterID int) ([]ServerInfo, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Query connections with the most recent metrics timestamp to determine status
	query := `
        SELECT
            c.id,
            c.name,
            c.host,
            c.port,
            c.role,
            c.database_name,
            c.membership_source,
            COALESCE(m.last_collected, '1970-01-01'::timestamp) as last_collected
        FROM connections c
        LEFT JOIN LATERAL (
            SELECT MAX(collected_at) as last_collected
            FROM metrics.pg_stat_activity
            WHERE connection_id = c.id
        ) m ON true
        WHERE c.cluster_id = $1
        ORDER BY
            CASE WHEN c.role = 'primary' THEN 0 ELSE 1 END,
            c.name
    `

	rows, err := d.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	var servers []ServerInfo
	for rows.Next() {
		var s ServerInfo
		var lastCollected time.Time
		var role sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.Host, &s.Port, &role, &s.Database, &s.MembershipSource, &lastCollected); err != nil {
			return nil, fmt.Errorf("failed to scan server: %w", err)
		}

		if role.Valid {
			s.Role = &role.String
		}

		// Determine status based on last collected metrics
		// Online: metrics within last 2 minutes
		// Warning: metrics 2-5 minutes old
		// Offline: no recent metrics or never collected
		elapsed := now.Sub(lastCollected)
		switch {
		case elapsed < 2*time.Minute:
			s.Status = "online"
		case elapsed < 5*time.Minute:
			s.Status = "warning"
		default:
			s.Status = "offline"
		}

		servers = append(servers, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating servers: %w", err)
	}

	return servers, nil
}

// GetClusterHierarchy returns the full cluster hierarchy for the web client
func (d *Datastore) GetClusterHierarchy(ctx context.Context) ([]ClusterGroupWithClusters, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Get all cluster groups
	groups, err := d.getClusterGroupsInternal(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]ClusterGroupWithClusters, 0, len(groups)+1)
	for i := range groups {
		groupWithClusters := ClusterGroupWithClusters{
			ID:       fmt.Sprintf("group-%d", groups[i].ID),
			Name:     groups[i].Name,
			Clusters: []ClusterWithServers{},
		}

		// Get clusters for this group
		clusters, err := d.getClustersInGroupInternal(ctx, groups[i].ID)
		if err != nil {
			return nil, err
		}

		for j := range clusters {
			clusterWithServers := ClusterWithServers{
				ID:      fmt.Sprintf("cluster-%d", clusters[j].ID),
				Name:    clusters[j].Name,
				Servers: []ServerInfo{},
			}

			// Get servers for this cluster
			servers, err := d.getServersInClusterInternal(ctx, clusters[j].ID)
			if err != nil {
				return nil, err
			}
			clusterWithServers.Servers = servers

			groupWithClusters.Clusters = append(groupWithClusters.Clusters, clusterWithServers)
		}

		result = append(result, groupWithClusters)
	}

	// Also include ungrouped connections (not assigned to any cluster)
	ungroupedServers, err := d.getUngroupedServersInternal(ctx)
	if err != nil {
		return nil, err
	}

	if len(ungroupedServers) > 0 {
		// Create an "Ungrouped" group for standalone servers
		ungroupedGroup := ClusterGroupWithClusters{
			ID:       "group-ungrouped",
			Name:     "Ungrouped",
			Clusters: []ClusterWithServers{},
		}

		// Each ungrouped server becomes its own "cluster" (standalone server)
		for _, server := range ungroupedServers {
			standaloneCluster := ClusterWithServers{
				ID:      fmt.Sprintf("standalone-%d", server.ID),
				Name:    server.Name,
				Servers: []ServerInfo{server},
			}
			ungroupedGroup.Clusters = append(ungroupedGroup.Clusters, standaloneCluster)
		}

		result = append(result, ungroupedGroup)
	}

	return result, nil
}

// getUngroupedServersInternal returns connections not assigned to any cluster
func (d *Datastore) getUngroupedServersInternal(ctx context.Context) ([]ServerInfo, error) {
	query := `
        SELECT
            c.id,
            c.name,
            c.host,
            c.port,
            COALESCE(c.role, 'primary') as role,
            CASE
                WHEN c.is_monitored AND c.connection_error IS NOT NULL
                THEN 'offline'
                WHEN c.is_monitored AND m.collected_at IS NULL
                THEN 'initialising'
                WHEN m.collected_at > NOW() - INTERVAL '60 seconds' THEN 'online'
                WHEN m.collected_at > NOW() - INTERVAL '150 seconds' THEN 'warning'
                WHEN m.collected_at IS NOT NULL THEN 'offline'
                ELSE 'unknown'
            END as status,
            c.connection_error
        FROM connections c
        LEFT JOIN LATERAL (
            SELECT collected_at
            FROM metrics.pg_connectivity
            WHERE connection_id = c.id
            ORDER BY collected_at DESC
            LIMIT 1
        ) m ON true
        WHERE c.cluster_id IS NULL
        ORDER BY c.name
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query ungrouped servers: %w", err)
	}
	defer rows.Close()

	var servers []ServerInfo
	for rows.Next() {
		var s ServerInfo
		var connErr sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.Host, &s.Port, &s.Role, &s.Status, &connErr); err != nil {
			return nil, fmt.Errorf("failed to scan server: %w", err)
		}
		if connErr.Valid {
			s.ConnectionError = &connErr.String
		}
		servers = append(servers, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating ungrouped servers: %w", err)
	}

	return servers, nil
}

// Internal methods that don't acquire locks (for use within locked methods)

func (d *Datastore) getClusterGroupsInternal(ctx context.Context) ([]ClusterGroup, error) {
	query := `
        SELECT id, name, description, created_at, updated_at
        FROM cluster_groups
        ORDER BY name
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster groups: %w", err)
	}
	defer rows.Close()

	var groups []ClusterGroup
	for rows.Next() {
		var g ClusterGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan cluster group: %w", err)
		}
		groups = append(groups, g)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating cluster groups: %w", err)
	}

	return groups, nil
}

func (d *Datastore) getClustersInGroupInternal(ctx context.Context, groupID int) ([]Cluster, error) {
	query := `
        SELECT id, group_id, name, description, replication_type, auto_cluster_key, created_at, updated_at
        FROM clusters
        WHERE group_id = $1
          AND dismissed = FALSE
        ORDER BY name
    `

	rows, err := d.pool.Query(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to query clusters: %w", err)
	}
	defer rows.Close()

	var clusters []Cluster
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.ID, &c.GroupID, &c.Name, &c.Description, &c.ReplicationType, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan cluster: %w", err)
		}
		clusters = append(clusters, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating clusters: %w", err)
	}

	return clusters, nil
}

func (d *Datastore) getServersInClusterInternal(ctx context.Context, clusterID int) ([]ServerInfo, error) {
	query := `
        SELECT
            c.id,
            c.name,
            c.host,
            c.port,
            c.role,
            c.database_name,
            CASE
                WHEN c.is_monitored AND c.connection_error IS NOT NULL
                THEN 'offline'
                WHEN c.is_monitored AND m.collected_at IS NULL
                THEN 'initialising'
                WHEN m.collected_at > NOW() - INTERVAL '60 seconds' THEN 'online'
                WHEN m.collected_at > NOW() - INTERVAL '150 seconds' THEN 'warning'
                WHEN m.collected_at IS NOT NULL THEN 'offline'
                ELSE 'unknown'
            END as status,
            c.connection_error
        FROM connections c
        LEFT JOIN LATERAL (
            SELECT collected_at
            FROM metrics.pg_connectivity
            WHERE connection_id = c.id
            ORDER BY collected_at DESC
            LIMIT 1
        ) m ON true
        WHERE c.cluster_id = $1
        ORDER BY
            CASE WHEN c.role = 'primary' THEN 0 ELSE 1 END,
            c.name
    `

	rows, err := d.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	var servers []ServerInfo
	for rows.Next() {
		var s ServerInfo
		var role sql.NullString
		var connErr sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.Host, &s.Port, &role, &s.Database, &s.Status, &connErr); err != nil {
			return nil, fmt.Errorf("failed to scan server: %w", err)
		}

		if role.Valid {
			s.Role = &role.String
		}
		if connErr.Valid {
			s.ConnectionError = &connErr.String
		}

		servers = append(servers, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating servers: %w", err)
	}

	return servers, nil
}

// AssignConnectionToCluster assigns a connection to a cluster with a role.
// When membershipSource is "manual" the connection stays pinned to the
// cluster even when auto-detection would move it elsewhere.
func (d *Datastore) AssignConnectionToCluster(ctx context.Context, connectionID int, clusterID *int, role *string, membershipSource string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        UPDATE connections
        SET cluster_id = $2, role = $3, membership_source = $4, updated_at = CURRENT_TIMESTAMP
        WHERE id = $1
    `

	result, err := d.pool.Exec(ctx, query, connectionID, clusterID, role, membershipSource)
	if err != nil {
		return fmt.Errorf("failed to assign connection to cluster: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrConnectionNotFound
	}

	return nil
}

// TopologyServerInfo extends ServerInfo with topology and child servers
type TopologyServerInfo struct {
	ID               int                    `json:"id"`
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	Host             string                 `json:"host"`
	Port             int                    `json:"port"`
	Status           string                 `json:"status"`
	ConnectionError  string                 `json:"connection_error,omitempty"`
	ActiveAlertCount int                    `json:"active_alert_count"`
	Role             string                 `json:"role,omitempty"`
	PrimaryRole      string                 `json:"primary_role"`
	IsExpandable     bool                   `json:"is_expandable"`
	MembershipSource string                 `json:"membership_source,omitempty"`
	OwnerUsername    string                 `json:"owner_username,omitempty"`
	Version          string                 `json:"version,omitempty"`
	OS               string                 `json:"os,omitempty"`
	SpockNodeName    string                 `json:"spock_node_name,omitempty"`
	SpockVersion     string                 `json:"spock_version,omitempty"`
	DatabaseName     string                 `json:"database_name,omitempty"`
	Username         string                 `json:"username,omitempty"`
	Children         []TopologyServerInfo   `json:"children,omitempty"`
	Relationships    []TopologyRelationship `json:"relationships,omitempty"`
}

// TopologyRelationship represents a directed edge from a source server to
// a target server within a cluster
type TopologyRelationship struct {
	TargetServerID   int    `json:"target_server_id"`
	TargetServerName string `json:"target_server_name"`
	RelationshipType string `json:"relationship_type"`
	IsAutoDetected   bool   `json:"is_auto_detected"`
}

// NodeRelationship represents a stored relationship between two nodes
type NodeRelationship struct {
	ID                 int    `json:"id"`
	ClusterID          int    `json:"cluster_id"`
	SourceConnectionID int    `json:"source_connection_id"`
	TargetConnectionID int    `json:"target_connection_id"`
	SourceName         string `json:"source_name"`
	TargetName         string `json:"target_name"`
	RelationshipType   string `json:"relationship_type"`
	IsAutoDetected     bool   `json:"is_auto_detected"`
}

// RelationshipInput is the request body for setting manual relationships
// from a source node
type RelationshipInput struct {
	TargetConnectionID int    `json:"target_connection_id"`
	RelationshipType   string `json:"relationship_type"`
}

// AutoRelationshipInput describes a single auto-detected relationship
// for use during topology refresh
type AutoRelationshipInput struct {
	SourceConnectionID int
	TargetConnectionID int
	RelationshipType   string
}

// TopologyCluster represents a replication-aware cluster
type TopologyCluster struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Description     string               `json:"description,omitempty"`
	ClusterType     string               `json:"cluster_type"`               // spock, spock_ha, binary, logical, server
	ReplicationType string               `json:"replication_type,omitempty"` // user-set replication type from the database
	AutoClusterKey  string               `json:"auto_cluster_key,omitempty"`
	Servers         []TopologyServerInfo `json:"servers"`
}

// clusterOverride holds custom name and description for auto-detected clusters
type clusterOverride struct {
	Name        string
	Description string
}

// TopologyGroup represents a group with topology-aware clusters
type TopologyGroup struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	IsDefault    bool              `json:"is_default,omitempty"`
	AutoGroupKey string            `json:"auto_group_key,omitempty"`
	Clusters     []TopologyCluster `json:"clusters"`
}

// connectionWithRole holds connection data with role information from metrics
type connectionWithRole struct {
	ID                 int
	Name               string
	Description        sql.NullString
	Host               string
	Port               int
	OwnerUsername      sql.NullString
	DatabaseName       string
	Username           string
	Version            sql.NullString // PostgreSQL version from metrics
	OS                 sql.NullString // OS name from pg_sys_os_info
	PrimaryRole        string
	UpstreamHost       sql.NullString
	UpstreamPort       sql.NullInt32
	PublisherHost      sql.NullString // For logical subscribers: publisher's host
	PublisherPort      sql.NullInt32  // For logical subscribers: publisher's port
	HasSpock           bool
	SpockNodeName      sql.NullString
	SpockVersion       sql.NullString // Spock extension version from metrics.pg_extension
	BinaryStandbyCount int
	IsStreamingStandby bool
	MembershipSource   string
	Status             string
	ConnectionError    sql.NullString
	ActiveAlertCount   int
	SystemIdentifier   sql.NullInt64 // from metrics.pg_server_info
	ClusterID          sql.NullInt64 // persisted cluster_id from connections table
}

// ConnectionClusterInfo holds cluster-related information for a connection
type ConnectionClusterInfo struct {
	ClusterID        *int    `json:"cluster_id"`
	Role             *string `json:"role"`
	MembershipSource string  `json:"membership_source"`
	ClusterName      *string `json:"cluster_name"`
	ReplicationType  *string `json:"replication_type"`
	AutoClusterKey   *string `json:"auto_cluster_key"`
}

// ClusterSummary holds minimal cluster information for autocomplete
type ClusterSummary struct {
	ID              int     `json:"id"`
	Name            string  `json:"name"`
	ReplicationType *string `json:"replication_type"`
	AutoClusterKey  *string `json:"auto_cluster_key"`
}

// GetClusterTopology returns the combined topology including manually-created
// cluster groups and auto-detected replication topology. When
// visibleConnectionIDs is non-nil the topology is pruned to servers whose
// connection ID is in the slice; clusters with no visible servers and
// groups with no visible clusters are dropped. A nil slice means the
// caller has unrestricted visibility (superuser or wildcard scope) and
// the full topology is returned.
func (d *Datastore) GetClusterTopology(ctx context.Context, visibleConnectionIDs []int) ([]TopologyGroup, error) {
	// An explicit empty allow-list means no connections are visible;
	// short-circuit before any datastore work.
	if visibleConnectionIDs != nil && len(visibleConnectionIDs) == 0 {
		return []TopologyGroup{}, nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	var result []TopologyGroup

	// Step 1: Get the default group info (database-backed since migration 16)
	defaultGroup, err := d.getDefaultGroupInternal(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get default group: %w", err)
	}

	// Step 2: Query ALL connections for auto-detection (not filtered by cluster_id)
	// This builds the complete picture of auto-detected clusters
	allConnections, err := d.getAllConnectionsWithRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections with roles: %w", err)
	}

	// Get cluster name overrides for auto-detected clusters
	clusterOverrides, err := d.getClusterOverridesInternal(ctx)
	if err != nil {
		clusterOverrides = make(map[string]clusterOverride)
	}

	// Step 3: Build auto-detected topology from ALL connections
	// This creates a map of auto_cluster_key -> TopologyCluster with servers
	autoDetectedClusters := d.buildAutoDetectedClusters(allConnections, clusterOverrides)

	// Step 4: Get clusters that have been moved to non-default groups
	// These have auto_cluster_key set AND group_id pointing to a non-default group
	claimedKeys, err := d.getClaimedAutoClusterKeys(ctx, defaultGroup.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get claimed cluster keys: %w", err)
	}

	// Step 5: Build manual groups topology (excluding the default group)
	manualGroups, err := d.buildManualGroupsTopology(ctx, autoDetectedClusters, defaultGroup.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to build manual groups topology: %w", err)
	}

	// Track connection IDs assigned to manual clusters (including children recursively)
	manuallyAssignedConnections := make(map[int]bool)
	for i := range manualGroups {
		for j := range manualGroups[i].Clusters {
			collectServerIDsRecursive(manualGroups[i].Clusters[j].Servers, manuallyAssignedConnections)
		}
	}

	// Add manual groups to result
	result = append(result, manualGroups...)

	// Step 6: Build default group with remaining (unclaimed) clusters
	// Filter out connections that are in manual groups
	var unclaimedConnections []connectionWithRole
	for i := range allConnections {
		if !manuallyAssignedConnections[allConnections[i].ID] {
			unclaimedConnections = append(unclaimedConnections, allConnections[i])
		}
	}

	// Build topology hierarchy for the default group, excluding claimed auto_cluster_keys
	defaultGroups := d.buildTopologyHierarchy(unclaimedConnections, clusterOverrides, claimedKeys, defaultGroup)

	// Step 6b: Add manual clusters (no auto_cluster_key) from the
	// default group. These clusters were created by users and are not
	// discovered by auto-detection, so buildTopologyHierarchy does
	// not include them.
	manualDefaultClusters, err := d.getManualClustersInDefaultGroup(ctx, defaultGroup.ID)
	if err != nil {
		logger.Errorf("GetClusterTopology: failed to get manual default-group clusters: %v", err)
	} else if len(manualDefaultClusters) > 0 && len(defaultGroups) > 0 {
		defaultGroups[0].Clusters = append(defaultGroups[0].Clusters, manualDefaultClusters...)
	}

	// Append the default group (contains auto-detected clusters)
	result = append(result, defaultGroups...)

	// Step 7: Merge persisted-but-undetected cluster members into the
	// topology. These are connections that have cluster_id set in the
	// database but were not found by auto-detection in this cycle.
	autoDetectedIDs := collectAutoDetectedIDs(autoDetectedClusters)
	persistedMembers, err := d.getPersistedClusterMembers(ctx, autoDetectedIDs)
	if err != nil {
		logger.Errorf("GetClusterTopology: failed to get persisted members: %v", err)
	} else {
		parentMap := d.getPersistedMemberParents(ctx, persistedMembers)
		mergedKeys := mergePersistedMembers(result, persistedMembers, parentMap)

		// Step 7b: Create shell clusters for any persisted members
		// whose auto_cluster_key did not match an existing cluster in
		// the topology. This happens when auto-detection temporarily
		// fails to recognize a cluster (e.g. stale metrics) but the
		// connections still have cluster_id set in the database.
		d.createShellClustersForUnmerged(ctx, result, persistedMembers, mergedKeys, parentMap)
	}

	// Step 8: Populate relationships on each server in the topology
	d.populateTopologyRelationships(ctx, result)

	// Step 9: Apply the caller's visibility filter, if any. Pruning
	// happens after the full topology is assembled so that relationships
	// are populated before servers are dropped; downstream code relies on
	// relationship metadata only for servers the caller can see.
	if visibleConnectionIDs != nil {
		result = pruneTopologyByVisibility(result, visibleConnectionIDs)
	}

	return result, nil
}

// pruneTopologyByVisibility removes servers, clusters, and groups that
// the caller is not allowed to see. A cluster with no visible servers is
// dropped; a group with no visible clusters is dropped. The visibleIDs
// slice is converted to a set once so the filter walks the topology in
// linear time. Child servers are filtered recursively so a hidden parent
// with visible children is dropped along with those children; this
// matches the "cluster visibility" contract where children are only
// meaningful under an accessible parent.
func pruneTopologyByVisibility(groups []TopologyGroup, visibleIDs []int) []TopologyGroup {
	visible := make(map[int]bool, len(visibleIDs))
	for _, id := range visibleIDs {
		visible[id] = true
	}

	filtered := make([]TopologyGroup, 0, len(groups))
	for i := range groups {
		group := groups[i]
		clusters := make([]TopologyCluster, 0, len(group.Clusters))
		for j := range group.Clusters {
			cluster := group.Clusters[j]
			servers := pruneTopologyServers(cluster.Servers, visible)
			if len(servers) == 0 {
				continue
			}
			cluster.Servers = servers
			clusters = append(clusters, cluster)
		}
		if len(clusters) == 0 {
			continue
		}
		group.Clusters = clusters
		filtered = append(filtered, group)
	}
	return filtered
}

// pruneTopologyServers returns the subset of servers whose connection IDs
// appear in the visible set, recursing into Children. Relationships
// pointing at hidden peers are also dropped so TargetServerID and
// TargetServerName never leak across the visibility boundary.
func pruneTopologyServers(servers []TopologyServerInfo, visible map[int]bool) []TopologyServerInfo {
	out := make([]TopologyServerInfo, 0, len(servers))
	for i := range servers {
		s := servers[i]
		if !visible[s.ID] {
			continue
		}
		if len(s.Children) > 0 {
			s.Children = pruneTopologyServers(s.Children, visible)
		}
		if len(s.Relationships) > 0 {
			rels := make([]TopologyRelationship, 0, len(s.Relationships))
			for _, rel := range s.Relationships {
				if visible[rel.TargetServerID] {
					rels = append(rels, rel)
				}
			}
			s.Relationships = rels
		}
		s.IsExpandable = len(s.Children) > 0
		out = append(out, s)
	}
	return out
}

// populateTopologyRelationships loads relationships from the database
// and attaches them to each server in the topology groups.
func (d *Datastore) populateTopologyRelationships(ctx context.Context, groups []TopologyGroup) {
	// Query all relationships at once, keyed by cluster ID
	rows, err := d.pool.Query(ctx, `
        SELECT r.cluster_id, r.source_connection_id,
               r.target_connection_id, tc.name AS target_name,
               r.relationship_type, r.is_auto_detected
        FROM cluster_node_relationships r
        JOIN connections tc ON tc.id = r.target_connection_id
        ORDER BY r.cluster_id, r.source_connection_id
    `)
	if err != nil {
		logger.Errorf("populateTopologyRelationships: failed to query relationships: %v", err)
		return
	}
	defer rows.Close()

	// Build map: source_connection_id -> []TopologyRelationship
	relsBySource := make(map[int][]TopologyRelationship)
	for rows.Next() {
		var clusterID, sourceID, targetID int
		var targetName, relType string
		var isAuto bool
		if err := rows.Scan(&clusterID, &sourceID, &targetID, &targetName, &relType, &isAuto); err != nil {
			logger.Errorf("populateTopologyRelationships: failed to scan relationship: %v", err)
			continue
		}
		relsBySource[sourceID] = append(relsBySource[sourceID], TopologyRelationship{
			TargetServerID:   targetID,
			TargetServerName: targetName,
			RelationshipType: relType,
			IsAutoDetected:   isAuto,
		})
	}
	if err := rows.Err(); err != nil {
		logger.Errorf("populateTopologyRelationships: error iterating relationships: %v", err)
	}

	if len(relsBySource) == 0 {
		return
	}

	// Walk the topology and attach relationships to matching servers
	for i := range groups {
		for j := range groups[i].Clusters {
			attachRelationshipsToServers(groups[i].Clusters[j].Servers, relsBySource)
		}
	}
}

// attachRelationshipsToServers recursively attaches relationships to
// servers and their children.
func attachRelationshipsToServers(servers []TopologyServerInfo, relsBySource map[int][]TopologyRelationship) {
	for i := range servers {
		if rels, ok := relsBySource[servers[i].ID]; ok {
			servers[i].Relationships = rels
		}
		if len(servers[i].Children) > 0 {
			attachRelationshipsToServers(servers[i].Children, relsBySource)
		}
	}
}

// collectServerIDsRecursive recursively collects all server IDs including children
// This is used to track which connections are assigned to clusters (including hot standbys)
func collectServerIDsRecursive(servers []TopologyServerInfo, ids map[int]bool) {
	for i := range servers {
		ids[servers[i].ID] = true
		if len(servers[i].Children) > 0 {
			collectServerIDsRecursive(servers[i].Children, ids)
		}
	}
}

// getAllConnectionsWithRoles queries all connections with their role data
func (d *Datastore) getAllConnectionsWithRoles(ctx context.Context) ([]connectionWithRole, error) {
	query := `
        WITH latest_connectivity AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, collected_at
            FROM metrics.pg_connectivity
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_roles AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, primary_role, upstream_host, upstream_port,
                has_spock, spock_node_name, binary_standby_count, is_streaming_standby,
                publisher_host, publisher_port
            FROM metrics.pg_node_role
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_server_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, server_version, system_identifier
            FROM metrics.pg_server_info
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_os_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, name as os_name
            FROM metrics.pg_sys_os_info
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_spock_version AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, extversion as spock_version
            FROM metrics.pg_extension
            WHERE extname = 'spock'
              AND collected_at > NOW() - INTERVAL '1 hour'
            ORDER BY connection_id, collected_at DESC
        ),
        active_alerts AS (
            SELECT connection_id, COUNT(*) as alert_count
            FROM alerts
            WHERE status = 'active'
            GROUP BY connection_id
        )
        SELECT c.id, c.name, c.description, c.host, c.port, c.owner_username,
               c.database_name, c.username,
               lsi.server_version,
               lsi.system_identifier,
               loi.os_name,
               COALESCE(NULLIF(lr.primary_role, 'standalone'), c.role, lr.primary_role, 'unknown') as primary_role,
               lr.upstream_host, lr.upstream_port,
               COALESCE(lr.has_spock, false) as has_spock,
               lr.spock_node_name,
               lsv.spock_version,
               COALESCE(lr.binary_standby_count, 0) as binary_standby_count,
               COALESCE(lr.is_streaming_standby, false) as is_streaming_standby,
               lr.publisher_host, lr.publisher_port,
               c.membership_source,
               CASE
                   WHEN c.is_monitored AND c.connection_error IS NOT NULL
                   THEN 'offline'
                   WHEN c.is_monitored AND lc.connection_id IS NULL
                   THEN 'initialising'
                   WHEN lc.collected_at > NOW() - INTERVAL '60 seconds' THEN 'online'
                   WHEN lc.collected_at > NOW() - INTERVAL '150 seconds' THEN 'warning'
                   WHEN lc.collected_at IS NOT NULL THEN 'offline'
                   ELSE 'unknown'
               END as status,
               c.connection_error,
               COALESCE(aa.alert_count, 0) as active_alert_count,
               c.cluster_id
        FROM connections c
        LEFT JOIN latest_connectivity lc ON c.id = lc.connection_id
        LEFT JOIN latest_roles lr ON c.id = lr.connection_id
        LEFT JOIN latest_server_info lsi ON c.id = lsi.connection_id
        LEFT JOIN latest_os_info loi ON c.id = loi.connection_id
        LEFT JOIN latest_spock_version lsv ON c.id = lsv.connection_id
        LEFT JOIN active_alerts aa ON c.id = aa.connection_id
        ORDER BY c.name
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections: %w", err)
	}
	defer rows.Close()

	var connections []connectionWithRole
	for rows.Next() {
		var conn connectionWithRole
		if err := rows.Scan(
			&conn.ID, &conn.Name, &conn.Description, &conn.Host, &conn.Port, &conn.OwnerUsername,
			&conn.DatabaseName, &conn.Username,
			&conn.Version,
			&conn.SystemIdentifier,
			&conn.OS,
			&conn.PrimaryRole, &conn.UpstreamHost, &conn.UpstreamPort,
			&conn.HasSpock, &conn.SpockNodeName,
			&conn.SpockVersion,
			&conn.BinaryStandbyCount, &conn.IsStreamingStandby,
			&conn.PublisherHost, &conn.PublisherPort,
			&conn.MembershipSource,
			&conn.Status,
			&conn.ConnectionError,
			&conn.ActiveAlertCount,
			&conn.ClusterID,
		); err != nil {
			return nil, fmt.Errorf("failed to scan connection: %w", err)
		}
		connections = append(connections, conn)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating connections: %w", err)
	}

	return connections, nil
}

// getClaimedAutoClusterKeys returns auto_cluster_keys that have been moved to non-default groups
// Clusters in the default group are NOT considered claimed (they should show in the default group)
func (d *Datastore) getClaimedAutoClusterKeys(ctx context.Context, defaultGroupID int) (map[string]bool, error) {
	query := `
        SELECT auto_cluster_key
        FROM clusters
        WHERE auto_cluster_key IS NOT NULL
          AND group_id IS NOT NULL
          AND group_id != $1
          AND dismissed = FALSE
    `

	rows, err := d.pool.Query(ctx, query, defaultGroupID)
	if err != nil {
		return nil, fmt.Errorf("failed to query claimed cluster keys: %w", err)
	}
	defer rows.Close()

	claimed := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("failed to scan cluster key: %w", err)
		}
		claimed[key] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating cluster keys: %w", err)
	}

	return claimed, nil
}

// buildAutoDetectedClusters builds a map of auto_cluster_key -> TopologyCluster
// This is used to get server information for auto-detected clusters that have been
// moved to manual groups
func (d *Datastore) buildAutoDetectedClusters(connections []connectionWithRole, clusterOverrides map[string]clusterOverride) map[string]TopologyCluster {
	result := make(map[string]TopologyCluster)

	// Create maps for lookups
	connByID := make(map[int]*connectionWithRole)
	connByHostPort := make(map[string]*connectionWithRole)
	connByNamePort := make(map[string]*connectionWithRole)

	for i := range connections {
		conn := &connections[i]
		connByID[conn.ID] = conn
		ipKey := fmt.Sprintf("%s:%d", conn.Host, conn.Port)
		connByHostPort[ipKey] = conn
		nameKey := fmt.Sprintf("%s:%d", conn.Name, conn.Port)
		connByNamePort[nameKey] = conn
	}

	// Track which connections are assigned to clusters
	assignedConnections := make(map[int]bool)

	// Build parent->children map for binary replication
	childrenMap := make(map[int][]int)
	for i := range connections {
		conn := &connections[i]
		if conn.IsStreamingStandby && conn.UpstreamHost.Valid && conn.UpstreamPort.Valid {
			upstreamKey := fmt.Sprintf("%s:%d", conn.UpstreamHost.String, conn.UpstreamPort.Int32)
			if parent, exists := connByHostPort[upstreamKey]; exists {
				childrenMap[parent.ID] = append(childrenMap[parent.ID], conn.ID)
			} else if parent, exists := connByNamePort[upstreamKey]; exists {
				childrenMap[parent.ID] = append(childrenMap[parent.ID], conn.ID)
			}
		}
	}

	// Second pass: use system_identifier to associate binary standbys
	// that weren't linked by upstream info. All physical replicas of a
	// PostgreSQL instance share the same system_identifier, making this
	// a reliable grouping mechanism regardless of network topology.
	alreadyChildren := make(map[int]bool)
	for _, children := range childrenMap {
		for _, childID := range children {
			alreadyChildren[childID] = true
		}
	}

	// Build a map of system_identifier -> primary connection ID
	sysIDToPrimary := make(map[int64]int)
	for i := range connections {
		conn := &connections[i]
		if conn.PrimaryRole == "binary_primary" && conn.SystemIdentifier.Valid {
			sysIDToPrimary[conn.SystemIdentifier.Int64] = conn.ID
		}
	}

	// Match unlinked binary standbys to their primary by system_identifier
	for i := range connections {
		conn := &connections[i]
		if conn.PrimaryRole != "binary_standby" || alreadyChildren[conn.ID] {
			continue
		}
		if !conn.SystemIdentifier.Valid {
			continue
		}
		if primaryID, ok := sysIDToPrimary[conn.SystemIdentifier.Int64]; ok {
			childrenMap[primaryID] = append(childrenMap[primaryID], conn.ID)
		}
	}

	// Process Spock clusters
	// Exclude connections with membership_source = 'manual'; those are
	// pinned to a manually created cluster and must not be re-grouped
	// into an auto-detected cluster on the next topology refresh.
	spockNodes := make([]*connectionWithRole, 0)
	for i := range connections {
		conn := &connections[i]
		if conn.HasSpock && conn.PrimaryRole != "binary_standby" && conn.MembershipSource != "manual" {
			spockNodes = append(spockNodes, conn)
		}
	}

	spockClusters := d.groupSpockNodesByClusters(spockNodes, childrenMap, connByID, assignedConnections, clusterOverrides)
	for _, cluster := range spockClusters {
		if cluster.AutoClusterKey != "" {
			result[cluster.AutoClusterKey] = cluster
		}
	}

	// Process binary replication clusters
	// Skip connections pinned to a manual cluster (membership_source =
	// 'manual'); they must not feed auto-detected cluster creation.
	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
			continue
		}
		if conn.MembershipSource == "manual" {
			continue
		}
		if !conn.HasSpock && (conn.PrimaryRole == "binary_primary" && len(childrenMap[conn.ID]) > 0) {
			// Auto binary cluster: exclude manually pinned children.
			server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections, false)
			autoKey := fmt.Sprintf("binary:%d", conn.ID)
			clusterName := conn.Name
			clusterDescription := ""
			if override, ok := clusterOverrides[autoKey]; ok {
				clusterName = override.Name
				clusterDescription = override.Description
			}
			cluster := TopologyCluster{
				ID:             fmt.Sprintf("server-%d", conn.ID),
				Name:           clusterName,
				Description:    clusterDescription,
				ClusterType:    "binary",
				AutoClusterKey: autoKey,
				Servers:        []TopologyServerInfo{server},
			}
			result[autoKey] = cluster
		}
	}

	// Process logical replication clusters
	logicalClusters := d.groupLogicalReplicationByPublisher(connections, connByID, connByHostPort, connByNamePort, assignedConnections, clusterOverrides)
	for _, cluster := range logicalClusters {
		if cluster.AutoClusterKey != "" {
			result[cluster.AutoClusterKey] = cluster
		}
	}

	// Process standalone servers (not part of any cluster)
	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
			continue
		}
		// Build server info. Standalone is an auto-detected entry, so
		// manually pinned children must not be pulled into it.
		server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections, false)
		autoKey := fmt.Sprintf("standalone:%d", conn.ID)
		clusterName := conn.Name
		clusterDescription := ""
		if override, ok := clusterOverrides[autoKey]; ok {
			clusterName = override.Name
			clusterDescription = override.Description
		}
		cluster := TopologyCluster{
			ID:             fmt.Sprintf("server-%d", conn.ID),
			Name:           clusterName,
			Description:    clusterDescription,
			ClusterType:    "server",
			AutoClusterKey: autoKey,
			Servers:        []TopologyServerInfo{server},
		}
		result[autoKey] = cluster
	}

	return result
}

// serverIDAndRole pairs a connection ID with its detected role.
type serverIDAndRole struct {
	ID   int
	Role string
}

// collectServerIDsAndRoles walks servers and their children recursively,
// collecting each server's ID and Role.
func collectServerIDsAndRoles(servers []TopologyServerInfo) []serverIDAndRole {
	var result []serverIDAndRole
	for i := range servers {
		result = append(result, serverIDAndRole{
			ID:   servers[i].ID,
			Role: servers[i].PrimaryRole,
		})
		if len(servers[i].Children) > 0 {
			result = append(result, collectServerIDsAndRoles(servers[i].Children)...)
		}
	}
	return result
}

// syncAutoDetectedClusterAssignments ensures that auto-detected clusters
// have corresponding records in the clusters table and that each
// connection's cluster_id reflects the auto-detected topology.
// This must be called from within a write-locked context (d.mu held).
// It returns a map of auto_cluster_key -> database cluster ID for
// clusters that were successfully upserted.
func (d *Datastore) syncAutoDetectedClusterAssignments(ctx context.Context, autoDetectedClusters map[string]TopologyCluster, defaultGroupID int) map[string]int {
	clusterIDMap := make(map[string]int)

	for autoKey, cluster := range autoDetectedClusters {
		if cluster.ClusterType == "server" {
			// Standalone servers: never clear cluster_id. If a
			// connection already belongs to a cluster, the assignment
			// persists even when auto-detection no longer confirms the
			// relationship.
			continue
		}

		serversAndRoles := collectServerIDsAndRoles(cluster.Servers)

		// For real clusters (spock, binary, logical, spock_ha):
		// Upsert the cluster record. Only auto-update the name when the
		// cluster is still in the default group (the user has not moved it).
		// The UPSERT also returns the dismissed flag so we can skip
		// assigning connections to dismissed clusters.
		var clusterID int
		var dismissed bool
		err := d.pool.QueryRow(ctx, `
            INSERT INTO clusters (name, auto_cluster_key, group_id)
            VALUES ($1, $2, $3)
            ON CONFLICT (auto_cluster_key) DO UPDATE SET
                name = CASE WHEN clusters.group_id = $3 THEN EXCLUDED.name ELSE clusters.name END,
                updated_at = CURRENT_TIMESTAMP
            RETURNING id, dismissed
        `, cluster.Name, autoKey, defaultGroupID).Scan(&clusterID, &dismissed)
		if err != nil {
			logger.Errorf("syncAutoDetectedClusterAssignments: failed to upsert cluster %q: %v", autoKey, err)
			continue
		}

		// Skip connection assignment for dismissed clusters; the
		// dismissed row stays in the DB to prevent re-insertion.
		if dismissed {
			continue
		}

		clusterIDMap[autoKey] = clusterID

		// Update each connection's cluster_id and role based on
		// membership_source:
		//
		// - cluster_id IS NULL: new discovery; assign cluster_id and
		//   role regardless of membership_source.
		// - cluster_id = this cluster: re-confirm; update role to
		//   reflect current metrics (handles failover).
		// - cluster_id != this cluster AND membership_source = 'auto':
		//   reassign to the newly detected cluster.
		// - cluster_id != this cluster AND membership_source = 'manual':
		//   leave cluster_id unchanged but still update role when the
		//   connection is detected in this cluster.
		for _, sr := range serversAndRoles {
			// Assign when NULL (new discovery) or reassign when auto
			_, err := d.pool.Exec(ctx,
				`UPDATE connections
                 SET cluster_id = $1, role = $2, updated_at = CURRENT_TIMESTAMP
                 WHERE id = $3
                   AND (cluster_id IS NULL OR cluster_id = $1 OR membership_source = 'auto')`,
				clusterID, sr.Role, sr.ID,
			)
			if err != nil {
				logger.Errorf("syncAutoDetectedClusterAssignments: failed to update connection %d: %v", sr.ID, err)
			}

			// For manual connections pointing to a different cluster,
			// update only the role so failover is tracked.
			_, err = d.pool.Exec(ctx,
				`UPDATE connections
                 SET role = $1, updated_at = CURRENT_TIMESTAMP
                 WHERE id = $2
                   AND membership_source = 'manual'
                   AND cluster_id IS NOT NULL
                   AND cluster_id != $3`,
				sr.Role, sr.ID, clusterID,
			)
			if err != nil {
				logger.Errorf("syncAutoDetectedClusterAssignments: failed to update role for manual connection %d: %v", sr.ID, err)
			}
		}
	}

	return clusterIDMap
}

// getPersistedClusterMembers queries connections that have cluster_id set
// in the database but were not found by auto-detection in the current
// cycle. These persisted-but-undetected members are returned grouped by
// their cluster's auto_cluster_key so they can be merged into the
// topology response.
func (d *Datastore) getPersistedClusterMembers(ctx context.Context, autoDetectedIDs map[int]bool) (map[string][]TopologyServerInfo, error) {
	query := `
        WITH latest_connectivity AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, collected_at
            FROM metrics.pg_connectivity
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_roles AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, primary_role, spock_node_name
            FROM metrics.pg_node_role
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_server_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, server_version
            FROM metrics.pg_server_info
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_os_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, name as os_name
            FROM metrics.pg_sys_os_info
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        active_alerts AS (
            SELECT connection_id, COUNT(*) as alert_count
            FROM alerts
            WHERE status = 'active'
            GROUP BY connection_id
        )
        SELECT c.id, c.name, c.description, c.host, c.port,
               c.owner_username, c.role,
               c.database_name, c.username,
               lsi.server_version,
               loi.os_name,
               lr.spock_node_name,
               COALESCE(NULLIF(lr.primary_role, 'standalone'), c.role, lr.primary_role, 'unknown') as primary_role,
               CASE
                   WHEN c.is_monitored AND c.connection_error IS NOT NULL
                   THEN 'offline'
                   WHEN c.is_monitored AND lc.connection_id IS NULL
                   THEN 'initialising'
                   WHEN lc.collected_at > NOW() - INTERVAL '60 seconds' THEN 'online'
                   WHEN lc.collected_at > NOW() - INTERVAL '150 seconds' THEN 'warning'
                   WHEN lc.collected_at IS NOT NULL THEN 'offline'
                   ELSE 'unknown'
               END as status,
               c.connection_error,
               COALESCE(aa.alert_count, 0) as active_alert_count,
               c.membership_source,
               cl.auto_cluster_key
        FROM connections c
        JOIN clusters cl ON cl.id = c.cluster_id
        LEFT JOIN latest_connectivity lc ON c.id = lc.connection_id
        LEFT JOIN latest_roles lr ON c.id = lr.connection_id
        LEFT JOIN latest_server_info lsi ON c.id = lsi.connection_id
        LEFT JOIN latest_os_info loi ON c.id = loi.connection_id
        LEFT JOIN active_alerts aa ON c.id = aa.connection_id
        WHERE c.cluster_id IS NOT NULL
          AND cl.auto_cluster_key IS NOT NULL
          AND cl.dismissed = FALSE
        ORDER BY c.name
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query persisted cluster members: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]TopologyServerInfo)
	for rows.Next() {
		var s TopologyServerInfo
		var ownerUsername, description, role, version, osName, spockNodeName, connError sql.NullString
		var autoClusterKey string
		if err := rows.Scan(
			&s.ID, &s.Name, &description, &s.Host, &s.Port,
			&ownerUsername, &role,
			&s.DatabaseName, &s.Username,
			&version, &osName, &spockNodeName,
			&s.PrimaryRole, &s.Status,
			&connError,
			&s.ActiveAlertCount,
			&s.MembershipSource,
			&autoClusterKey,
		); err != nil {
			return nil, fmt.Errorf("failed to scan persisted member: %w", err)
		}

		// Skip connections that auto-detection already discovered
		if autoDetectedIDs[s.ID] {
			continue
		}

		if ownerUsername.Valid {
			s.OwnerUsername = ownerUsername.String
		}
		if description.Valid {
			s.Description = description.String
		}
		if role.Valid {
			s.Role = role.String
		} else {
			s.Role = d.mapPrimaryRoleToDisplayRole(s.PrimaryRole)
		}
		if version.Valid {
			s.Version = version.String
		}
		if osName.Valid {
			s.OS = osName.String
		}
		if spockNodeName.Valid {
			s.SpockNodeName = spockNodeName.String
		}
		if connError.Valid {
			s.ConnectionError = connError.String
		}

		result[autoClusterKey] = append(result[autoClusterKey], s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating persisted members: %w", err)
	}

	return result, nil
}

// mergePersistedMembers adds persisted-but-undetected cluster members
// into the auto-detected topology. When a relationship exists that
// identifies a parent server (via streams_from or subscribes_to), the
// persisted member is nested as a child of that parent in the hierarchy.
// Members without a known parent are appended at the top level of the
// cluster.
// mergePersistedMembers adds persisted-but-undetected cluster members
// into the topology. It returns the set of auto_cluster_keys that were
// successfully merged so that the caller can identify any keys from the
// persisted map that still need shell clusters created for them.
func mergePersistedMembers(groups []TopologyGroup, persisted map[string][]TopologyServerInfo, parentMap map[int]int) map[string]bool {
	merged := make(map[string]bool)
	if len(persisted) == 0 {
		return merged
	}

	for i := range groups {
		for j := range groups[i].Clusters {
			cluster := &groups[i].Clusters[j]
			if cluster.AutoClusterKey == "" {
				continue
			}
			members, ok := persisted[cluster.AutoClusterKey]
			if !ok {
				continue
			}

			merged[cluster.AutoClusterKey] = true

			// Build a set of IDs already in this cluster
			existing := make(map[int]bool)
			collectServerIDsRecursive(cluster.Servers, existing)

			for k := range members {
				if existing[members[k].ID] {
					continue
				}

				parentID, hasParent := parentMap[members[k].ID]
				if hasParent {
					if addChildToServer(cluster.Servers, parentID, members[k]) {
						continue
					}
				}

				// No relationship or parent not found in tree;
				// fall back to top-level placement.
				cluster.Servers = append(cluster.Servers, members[k])
			}
		}
	}

	return merged
}

// createShellClustersForUnmerged creates placeholder cluster entries for
// persisted members that could not be merged because auto-detection did
// not produce a matching cluster in the topology. The function queries
// the clusters table for the unmerged auto_cluster_keys and adds shell
// clusters (with the persisted servers) to the appropriate group.
func (d *Datastore) createShellClustersForUnmerged(ctx context.Context, groups []TopologyGroup, persisted map[string][]TopologyServerInfo, mergedKeys map[string]bool, parentMap map[int]int) {
	// Collect unmerged keys
	var unmergedKeys []string
	for key := range persisted {
		if !mergedKeys[key] {
			unmergedKeys = append(unmergedKeys, key)
		}
	}
	if len(unmergedKeys) == 0 {
		return
	}

	// Query cluster metadata for unmerged keys (exclude dismissed)
	query := `
        SELECT id, name, COALESCE(description, ''), COALESCE(replication_type, ''), auto_cluster_key, group_id
        FROM clusters
        WHERE auto_cluster_key = ANY($1)
          AND dismissed = FALSE
    `
	rows, err := d.pool.Query(ctx, query, unmergedKeys)
	if err != nil {
		logger.Errorf("createShellClustersForUnmerged: failed to query clusters: %v", err)
		return
	}
	defer rows.Close()

	type shellInfo struct {
		dbID            int
		name            string
		description     string
		replicationType string
		autoClusterKey  string
		groupID         int
	}
	var shells []shellInfo
	for rows.Next() {
		var s shellInfo
		if err := rows.Scan(&s.dbID, &s.name, &s.description, &s.replicationType, &s.autoClusterKey, &s.groupID); err != nil {
			logger.Errorf("createShellClustersForUnmerged: failed to scan cluster: %v", err)
			continue
		}
		shells = append(shells, s)
	}
	if err := rows.Err(); err != nil {
		logger.Errorf("createShellClustersForUnmerged: error iterating clusters: %v", err)
	}

	// Derive cluster type from the auto_cluster_key prefix
	clusterTypeFromKey := func(key string) string {
		if idx := strings.Index(key, ":"); idx >= 0 {
			return key[:idx]
		}
		return "server"
	}

	// Build shell clusters and add to the matching group
	for _, s := range shells {
		members := persisted[s.autoClusterKey]
		if len(members) == 0 {
			continue
		}

		// Nest children using the parent map
		servers := nestPersistedMembers(members, parentMap)

		tc := TopologyCluster{
			ID:              fmt.Sprintf("cluster-%d", s.dbID),
			Name:            s.name,
			Description:     s.description,
			ReplicationType: s.replicationType,
			ClusterType:     clusterTypeFromKey(s.autoClusterKey),
			AutoClusterKey:  s.autoClusterKey,
			Servers:         servers,
		}

		// Find the matching group and append the shell cluster
		groupKey := fmt.Sprintf("group-%d", s.groupID)
		added := false
		for i := range groups {
			if groups[i].ID == groupKey {
				groups[i].Clusters = append(groups[i].Clusters, tc)
				added = true
				break
			}
		}
		if !added {
			logger.Errorf("createShellClustersForUnmerged: group %q not found for cluster %q", groupKey, s.autoClusterKey)
		}
	}
}

// nestPersistedMembers arranges persisted members into a parent-child
// hierarchy using the parentMap (child ID -> parent ID). Members without
// a parent or whose parent is not in the list stay at the top level.
func nestPersistedMembers(members []TopologyServerInfo, parentMap map[int]int) []TopologyServerInfo {
	if len(parentMap) == 0 {
		return members
	}

	// Build index by ID
	byID := make(map[int]*TopologyServerInfo)
	for i := range members {
		members[i].Children = make([]TopologyServerInfo, 0)
		byID[members[i].ID] = &members[i]
	}

	childIDs := make(map[int]bool)
	for i := range members {
		parentID, ok := parentMap[members[i].ID]
		if !ok {
			continue
		}
		parent, parentInSet := byID[parentID]
		if !parentInSet {
			continue
		}
		parent.IsExpandable = true
		parent.Children = append(parent.Children, *byID[members[i].ID])
		childIDs[members[i].ID] = true
	}

	var result []TopologyServerInfo
	for i := range members {
		if !childIDs[members[i].ID] {
			result = append(result, *byID[members[i].ID])
		}
	}
	return result
}

// addChildToServer searches the server tree for a server with the given
// parentID and appends the child to its Children slice. Returns true if
// the parent was found and the child was added.
func addChildToServer(servers []TopologyServerInfo, parentID int, child TopologyServerInfo) bool {
	for i := range servers {
		if servers[i].ID == parentID {
			servers[i].Children = append(servers[i].Children, child)
			servers[i].IsExpandable = true
			return true
		}
		if len(servers[i].Children) > 0 {
			if addChildToServer(servers[i].Children, parentID, child) {
				return true
			}
		}
	}
	return false
}

// getPersistedMemberParents queries cluster_node_relationships to build
// a map from persisted member connection ID to its parent connection ID.
// A member is a child when it has a streams_from or subscribes_to
// relationship (source = child, target = parent).
func (d *Datastore) getPersistedMemberParents(ctx context.Context, persisted map[string][]TopologyServerInfo) map[int]int {
	// Collect all persisted member IDs
	var ids []int
	for _, members := range persisted {
		for i := range members {
			ids = append(ids, members[i].ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	query := `
        SELECT source_connection_id, target_connection_id
        FROM cluster_node_relationships
        WHERE source_connection_id = ANY($1)
          AND relationship_type IN ('streams_from', 'subscribes_to')
    `

	rows, err := d.pool.Query(ctx, query, ids)
	if err != nil {
		logger.Errorf("getPersistedMemberParents: failed to query relationships: %v", err)
		return nil
	}
	defer rows.Close()

	parentMap := make(map[int]int)
	for rows.Next() {
		var sourceID, targetID int
		if err := rows.Scan(&sourceID, &targetID); err != nil {
			logger.Errorf("getPersistedMemberParents: failed to scan row: %v", err)
			continue
		}
		parentMap[sourceID] = targetID
	}

	if err := rows.Err(); err != nil {
		logger.Errorf("getPersistedMemberParents: error iterating rows: %v", err)
	}

	return parentMap
}

// collectAutoDetectedIDs gathers connection IDs that were placed into
// real clusters (spock, binary, logical, etc.) by auto-detection.
// Standalone entries (ClusterType "server") are excluded because they
// represent unassigned connections, not confirmed cluster membership.
// Without this exclusion a server that is down (and therefore detected
// as standalone) would be suppressed from the persisted-member merge
// step, causing it to vanish from its manually-assigned cluster.
func collectAutoDetectedIDs(autoDetectedClusters map[string]TopologyCluster) map[int]bool {
	ids := make(map[int]bool)
	for _, cluster := range autoDetectedClusters {
		if cluster.ClusterType == "server" {
			continue
		}
		collectServerIDsRecursive(cluster.Servers, ids)
	}
	return ids
}

// getManualClustersInDefaultGroup returns topology clusters for
// manually-created clusters (no auto_cluster_key) that belong to the
// default group. These clusters are not discovered by auto-detection
// so they must be added to the topology explicitly.
func (d *Datastore) getManualClustersInDefaultGroup(ctx context.Context, defaultGroupID int) ([]TopologyCluster, error) {
	clusters, err := d.getClustersInGroupInternal(ctx, defaultGroupID)
	if err != nil {
		return nil, fmt.Errorf("failed to get clusters in default group: %w", err)
	}

	var result []TopologyCluster
	for _, c := range clusters {
		// Skip auto-detected clusters; they are already handled by
		// buildTopologyHierarchy.
		if c.AutoClusterKey.Valid && c.AutoClusterKey.String != "" {
			continue
		}

		clusterDescription := ""
		if c.Description != nil {
			clusterDescription = *c.Description
		}
		replicationType := ""
		if c.ReplicationType != nil {
			replicationType = *c.ReplicationType
		}
		tc := TopologyCluster{
			ID:              fmt.Sprintf("cluster-%d", c.ID),
			Name:            c.Name,
			Description:     clusterDescription,
			ReplicationType: replicationType,
			ClusterType:     "manual",
			Servers:         []TopologyServerInfo{},
		}

		servers, err := d.getServersInClusterWithRolesInternal(ctx, c.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get servers for cluster %d: %w", c.ID, err)
		}
		tc.Servers = d.buildManualClusterHierarchy(ctx, c.ID, servers)

		result = append(result, tc)
	}

	return result, nil
}

// RefreshClusterAssignments rebuilds auto-detected cluster topology and
// syncs the cluster_id assignments on the connections table.
func (d *Datastore) RefreshClusterAssignments(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Get default group
	defaultGroup, err := d.getDefaultGroupInternal(ctx)
	if err != nil {
		return fmt.Errorf("failed to get default group: %w", err)
	}

	// Query all connections with roles
	allConnections, err := d.getAllConnectionsWithRoles(ctx)
	if err != nil {
		return fmt.Errorf("failed to query connections: %w", err)
	}

	// Get cluster overrides
	clusterOverrides, err := d.getClusterOverridesInternal(ctx)
	if err != nil {
		clusterOverrides = make(map[string]clusterOverride)
	}

	// Build auto-detected clusters
	autoDetectedClusters := d.buildAutoDetectedClusters(allConnections, clusterOverrides)

	// Sync assignments and get mapping of auto_cluster_key -> db cluster ID
	clusterIDMap := d.syncAutoDetectedClusterAssignments(ctx, autoDetectedClusters, defaultGroup.ID)

	// Sync auto-detected relationships for each cluster
	d.syncRelationshipsFromTopology(ctx, autoDetectedClusters, clusterIDMap)

	return nil
}

// syncRelationshipsFromTopology extracts relationship edges from the
// auto-detected cluster topology and syncs them to the database.
func (d *Datastore) syncRelationshipsFromTopology(ctx context.Context, autoDetectedClusters map[string]TopologyCluster, clusterIDMap map[string]int) {
	for autoKey, cluster := range autoDetectedClusters {
		dbClusterID, ok := clusterIDMap[autoKey]
		if !ok {
			// Standalone or failed upsert; skip
			continue
		}

		var detected []AutoRelationshipInput

		switch cluster.ClusterType {
		case "binary":
			// Binary replication: children stream from their parent
			// Servers[0] is the primary; its Children are standbys
			detected = extractBinaryRelationships(cluster.Servers)

		case "spock", "spock_ha":
			// Spock bidirectional: every pair gets two replicates_with rows
			// Spock nodes with binary children also get streams_from edges
			detected = extractSpockRelationships(cluster.Servers)

		case "logical":
			// Logical replication: Servers[0] is the publisher with
			// subscribers as Children
			detected = extractLogicalRelationships(cluster.Servers)
		}

		if len(detected) > 0 {
			if err := d.SyncAutoDetectedRelationships(ctx, dbClusterID, detected); err != nil {
				logger.Errorf("syncRelationshipsFromTopology: failed to sync relationships for cluster %q (id=%d): %v", autoKey, dbClusterID, err)
			}
		}
	}
}

// extractBinaryRelationships extracts streams_from relationships from
// binary replication clusters. Each child (standby) streams_from its
// parent (primary).
func extractBinaryRelationships(servers []TopologyServerInfo) []AutoRelationshipInput {
	var result []AutoRelationshipInput
	for i := range servers {
		result = append(result, extractBinaryChildRelationships(servers[i])...)
	}
	return result
}

// extractBinaryChildRelationships recursively extracts streams_from
// relationships for a server and its children.
func extractBinaryChildRelationships(server TopologyServerInfo) []AutoRelationshipInput {
	var result []AutoRelationshipInput
	for i := range server.Children {
		child := &server.Children[i]
		// Child (standby) streams_from parent (primary)
		result = append(result, AutoRelationshipInput{
			SourceConnectionID: child.ID,
			TargetConnectionID: server.ID,
			RelationshipType:   "streams_from",
		})
		// Recurse for cascading standbys
		result = append(result, extractBinaryChildRelationships(*child)...)
	}
	return result
}

// extractSpockRelationships extracts replicates_with relationships for
// all Spock node pairs and streams_from relationships for any binary
// standbys attached to Spock nodes.
func extractSpockRelationships(servers []TopologyServerInfo) []AutoRelationshipInput {
	var result []AutoRelationshipInput

	// Collect all top-level Spock node IDs (not their children)
	for i := 0; i < len(servers); i++ {
		for j := i + 1; j < len(servers); j++ {
			// Bidirectional: two rows per pair
			result = append(result, AutoRelationshipInput{
				SourceConnectionID: servers[i].ID,
				TargetConnectionID: servers[j].ID,
				RelationshipType:   "replicates_with",
			})
			result = append(result, AutoRelationshipInput{
				SourceConnectionID: servers[j].ID,
				TargetConnectionID: servers[i].ID,
				RelationshipType:   "replicates_with",
			})
		}
		// Binary standbys of Spock nodes get streams_from edges
		result = append(result, extractBinaryChildRelationships(servers[i])...)
	}

	return result
}

// extractLogicalRelationships extracts subscribes_to relationships from
// logical replication clusters. Each subscriber (child) subscribes_to the
// publisher (parent).
func extractLogicalRelationships(servers []TopologyServerInfo) []AutoRelationshipInput {
	var result []AutoRelationshipInput
	for i := range servers {
		publisher := &servers[i]
		for j := range publisher.Children {
			subscriber := &publisher.Children[j]
			// Subscriber subscribes_to publisher
			result = append(result, AutoRelationshipInput{
				SourceConnectionID: subscriber.ID,
				TargetConnectionID: publisher.ID,
				RelationshipType:   "subscribes_to",
			})
		}
	}
	return result
}

// buildManualGroupsTopology builds TopologyGroup entries for manually-created
// cluster groups, including empty groups. For clusters that have auto_cluster_key
// (moved auto-detected clusters), it uses the autoDetectedClusters map to get
// their servers instead of querying by cluster_id.
// The default group is excluded (handled separately by buildTopologyHierarchy).
func (d *Datastore) buildManualGroupsTopology(ctx context.Context, autoDetectedClusters map[string]TopologyCluster, defaultGroupID int) ([]TopologyGroup, error) {
	// Get all cluster groups except the default group
	// (manual groups that users have created)
	query := `
        SELECT id, name
        FROM cluster_groups
        WHERE NOT is_default
        ORDER BY name
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query manual cluster groups: %w", err)
	}
	defer rows.Close()

	type groupInfo struct {
		id   int
		name string
	}
	var groups []groupInfo
	for rows.Next() {
		var g groupInfo
		if err := rows.Scan(&g.id, &g.name); err != nil {
			return nil, fmt.Errorf("failed to scan cluster group: %w", err)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating cluster groups: %w", err)
	}

	result := make([]TopologyGroup, 0, len(groups))
	for _, g := range groups {
		topologyGroup := TopologyGroup{
			ID:       fmt.Sprintf("group-%d", g.id),
			Name:     g.name,
			Clusters: []TopologyCluster{},
		}

		// Get clusters in this group
		clusters, err := d.getClustersInGroupInternal(ctx, g.id)
		if err != nil {
			return nil, fmt.Errorf("failed to get clusters for group %d: %w", g.id, err)
		}

		for _, c := range clusters {
			// Check if this is a moved auto-detected cluster
			if c.AutoClusterKey.Valid && c.AutoClusterKey.String != "" {
				// This cluster was auto-detected and moved to this manual group
				// Use the auto-detected cluster data (with servers) from our map
				if autoCluster, ok := autoDetectedClusters[c.AutoClusterKey.String]; ok {
					clusterDescription := ""
					if c.Description != nil {
						clusterDescription = *c.Description
					}
					replicationType := ""
					if c.ReplicationType != nil {
						replicationType = *c.ReplicationType
					}
					topologyCluster := TopologyCluster{
						ID:              fmt.Sprintf("cluster-%d", c.ID),
						Name:            c.Name, // Use the custom name from the cluster record
						Description:     clusterDescription,
						ReplicationType: replicationType,
						ClusterType:     autoCluster.ClusterType,
						AutoClusterKey:  c.AutoClusterKey.String,
						Servers:         autoCluster.Servers,
					}
					topologyGroup.Clusters = append(topologyGroup.Clusters, topologyCluster)
				}
				// If not found in autoDetectedClusters, the cluster may have been removed
				// or topology changed - skip it
				continue
			}

			// Regular manual cluster - get servers via cluster_id
			manualDescription := ""
			if c.Description != nil {
				manualDescription = *c.Description
			}
			manualReplicationType := ""
			if c.ReplicationType != nil {
				manualReplicationType = *c.ReplicationType
			}
			topologyCluster := TopologyCluster{
				ID:              fmt.Sprintf("cluster-%d", c.ID),
				Name:            c.Name,
				Description:     manualDescription,
				ReplicationType: manualReplicationType,
				ClusterType:     "manual",
				Servers:         []TopologyServerInfo{},
			}

			// Get servers in this cluster with their role data
			servers, err := d.getServersInClusterWithRolesInternal(ctx, c.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to get servers for cluster %d: %w", c.ID, err)
			}
			topologyCluster.Servers = d.buildManualClusterHierarchy(ctx, c.ID, servers)

			topologyGroup.Clusters = append(topologyGroup.Clusters, topologyCluster)
		}

		result = append(result, topologyGroup)
	}

	return result, nil
}

// getServersInClusterWithRolesInternal returns servers with their topology role info
func (d *Datastore) getServersInClusterWithRolesInternal(ctx context.Context, clusterID int) ([]TopologyServerInfo, error) {
	query := `
        WITH latest_roles AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, primary_role, spock_node_name,
                COALESCE(
                    CASE
                        WHEN collected_at > NOW() - INTERVAL '6 minutes' THEN 'online'
                        WHEN collected_at > NOW() - INTERVAL '12 minutes' THEN 'warning'
                        ELSE 'offline'
                    END, 'unknown'
                ) as status
            FROM metrics.pg_node_role
            ORDER BY connection_id, collected_at DESC
        ),
        latest_server_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, server_version
            FROM metrics.pg_server_info
            ORDER BY connection_id, collected_at DESC
        ),
        latest_os_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, name as os_name
            FROM metrics.pg_sys_os_info
            ORDER BY connection_id, collected_at DESC
        ),
        active_alerts AS (
            SELECT connection_id, COUNT(*) as alert_count
            FROM alerts
            WHERE status = 'active'
            GROUP BY connection_id
        )
        SELECT c.id, c.name, c.description, c.host, c.port, c.owner_username, c.role,
               c.database_name, c.username,
               lsi.server_version,
               loi.os_name,
               lr.spock_node_name,
               COALESCE(NULLIF(lr.primary_role, 'standalone'), c.role, lr.primary_role, 'unknown') as primary_role,
               COALESCE(lr.status, 'unknown') as status,
               COALESCE(aa.alert_count, 0) as active_alert_count,
               c.membership_source
        FROM connections c
        LEFT JOIN latest_roles lr ON c.id = lr.connection_id
        LEFT JOIN latest_server_info lsi ON c.id = lsi.connection_id
        LEFT JOIN latest_os_info loi ON c.id = loi.connection_id
        LEFT JOIN active_alerts aa ON c.id = aa.connection_id
        WHERE c.cluster_id = $1
        ORDER BY
            CASE WHEN c.role = 'primary' THEN 0 ELSE 1 END,
            c.name
    `

	rows, err := d.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	var servers []TopologyServerInfo
	for rows.Next() {
		var s TopologyServerInfo
		var ownerUsername, description, role, version, osName, spockNodeName sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &description, &s.Host, &s.Port, &ownerUsername, &role,
			&s.DatabaseName, &s.Username, &version, &osName, &spockNodeName,
			&s.PrimaryRole, &s.Status, &s.ActiveAlertCount, &s.MembershipSource); err != nil {
			return nil, fmt.Errorf("failed to scan server: %w", err)
		}
		if ownerUsername.Valid {
			s.OwnerUsername = ownerUsername.String
		}
		if description.Valid {
			s.Description = description.String
		}
		if role.Valid {
			s.Role = role.String
		} else {
			s.Role = d.mapPrimaryRoleToDisplayRole(s.PrimaryRole)
		}
		if version.Valid {
			s.Version = version.String
		}
		if osName.Valid {
			s.OS = osName.String
		}
		if spockNodeName.Valid {
			s.SpockNodeName = spockNodeName.String
		}
		s.IsExpandable = false
		servers = append(servers, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating servers: %w", err)
	}

	return servers, nil
}

// buildManualClusterHierarchy takes a flat list of servers for a manual
// cluster and nests children under their parents based on relationships
// stored in cluster_node_relationships. A server with a streams_from or
// subscribes_to relationship is a child of the target server.
func (d *Datastore) buildManualClusterHierarchy(ctx context.Context, clusterID int, servers []TopologyServerInfo) []TopologyServerInfo {
	if len(servers) == 0 {
		return servers
	}

	// Query parent relationships for servers in this cluster
	query := `
        SELECT source_connection_id, target_connection_id
        FROM cluster_node_relationships
        WHERE cluster_id = $1
          AND relationship_type IN ('streams_from', 'subscribes_to')
    `
	rows, err := d.pool.Query(ctx, query, clusterID)
	if err != nil {
		logger.Errorf("buildManualClusterHierarchy: failed to query relationships: %v", err)
		return servers
	}
	defer rows.Close()

	// Build parent map: child ID -> parent ID
	parentMap := make(map[int]int)
	for rows.Next() {
		var sourceID, targetID int
		if err := rows.Scan(&sourceID, &targetID); err != nil {
			logger.Errorf("buildManualClusterHierarchy: failed to scan row: %v", err)
			continue
		}
		parentMap[sourceID] = targetID
	}
	if err := rows.Err(); err != nil {
		logger.Errorf("buildManualClusterHierarchy: error iterating rows: %v", err)
	}

	if len(parentMap) == 0 {
		return servers
	}

	// Build server index by ID
	serverByID := make(map[int]*TopologyServerInfo)
	for i := range servers {
		servers[i].Children = make([]TopologyServerInfo, 0)
		serverByID[servers[i].ID] = &servers[i]
	}

	// Track which servers are children
	childIDs := make(map[int]bool)
	for childID, parentID := range parentMap {
		parent, parentExists := serverByID[parentID]
		child, childExists := serverByID[childID]
		if parentExists && childExists {
			parent.IsExpandable = true
			parent.Children = append(parent.Children, *child)
			childIDs[childID] = true
		}
	}

	// Return only top-level servers (those that are not children)
	var result []TopologyServerInfo
	for i := range servers {
		if !childIDs[servers[i].ID] {
			result = append(result, *serverByID[servers[i].ID])
		}
	}

	return result
}

// buildTopologyHierarchy builds the topology hierarchy from connections.
// The claimedKeys parameter contains auto_cluster_keys that have been moved to
// manual groups - these clusters will be excluded from the default group.
// The defaultGroup parameter provides the database-backed default group info.
func (d *Datastore) buildTopologyHierarchy(connections []connectionWithRole, clusterOverrides map[string]clusterOverride, claimedKeys map[string]bool, defaultGroup *defaultGroupInfo) []TopologyGroup {
	// Create maps for lookups
	connByID := make(map[int]*connectionWithRole)
	connByHostPort := make(map[string]*connectionWithRole)
	connByNamePort := make(map[string]*connectionWithRole) // Name-based lookup for hostname matching

	for i := range connections {
		conn := &connections[i]
		connByID[conn.ID] = conn
		// IP-based key
		ipKey := fmt.Sprintf("%s:%d", conn.Host, conn.Port)
		connByHostPort[ipKey] = conn
		// Name-based key (connection names often match hostnames)
		nameKey := fmt.Sprintf("%s:%d", conn.Name, conn.Port)
		connByNamePort[nameKey] = conn
	}

	// Track which connections are assigned to clusters
	assignedConnections := make(map[int]bool)

	// Build parent->children map for binary replication (streaming standbys)
	childrenMap := make(map[int][]int) // parent connection ID -> child connection IDs
	for i := range connections {
		conn := &connections[i]
		if conn.IsStreamingStandby && conn.UpstreamHost.Valid && conn.UpstreamPort.Valid {
			upstreamKey := fmt.Sprintf("%s:%d", conn.UpstreamHost.String, conn.UpstreamPort.Int32)
			// Try IP-based matching first
			if parent, exists := connByHostPort[upstreamKey]; exists {
				childrenMap[parent.ID] = append(childrenMap[parent.ID], conn.ID)
			} else if parent, exists := connByNamePort[upstreamKey]; exists {
				// Fall back to name-based matching (upstream_host often contains hostname)
				childrenMap[parent.ID] = append(childrenMap[parent.ID], conn.ID)
			}
		}
	}

	// Second pass: use system_identifier to associate binary standbys
	// that weren't linked by upstream info.
	alreadyChildren := make(map[int]bool)
	for _, children := range childrenMap {
		for _, childID := range children {
			alreadyChildren[childID] = true
		}
	}

	sysIDToPrimary := make(map[int64]int)
	for i := range connections {
		conn := &connections[i]
		if conn.PrimaryRole == "binary_primary" && conn.SystemIdentifier.Valid {
			sysIDToPrimary[conn.SystemIdentifier.Int64] = conn.ID
		}
	}

	for i := range connections {
		conn := &connections[i]
		if conn.PrimaryRole != "binary_standby" || alreadyChildren[conn.ID] {
			continue
		}
		if !conn.SystemIdentifier.Valid {
			continue
		}
		if primaryID, ok := sysIDToPrimary[conn.SystemIdentifier.Int64]; ok {
			childrenMap[primaryID] = append(childrenMap[primaryID], conn.ID)
		}
	}

	// Identify Spock nodes (has_spock=true AND not a standby)
	// These are the actual Spock multi-master nodes.
	// Connections with membership_source = 'manual' are pinned to a
	// manually created cluster and must be excluded from auto-detected
	// Spock grouping; otherwise they would appear in both clusters.
	spockNodes := make([]*connectionWithRole, 0)
	for i := range connections {
		conn := &connections[i]
		// A Spock node has Spock installed and is not a binary standby
		// (binary standbys with Spock are hot standbys of Spock nodes)
		if conn.HasSpock && conn.PrimaryRole != "binary_standby" && conn.MembershipSource != "manual" {
			spockNodes = append(spockNodes, conn)
		}
	}

	// Group Spock nodes into clusters based on naming patterns
	// e.g., pg17-node1, pg17-node2, pg17-node3 -> one cluster
	// pg18-spock1, pg18-spock2 -> another cluster
	spockClusters := d.groupSpockNodesByClusters(spockNodes, childrenMap, connByID, assignedConnections, clusterOverrides)

	// Build clusters list, filtering out claimed clusters
	var clusters []TopologyCluster
	for _, cluster := range spockClusters {
		// Skip clusters that have been moved to manual groups
		if cluster.AutoClusterKey != "" && claimedKeys[cluster.AutoClusterKey] {
			continue
		}
		clusters = append(clusters, cluster)
	}

	// 2. Create entries for non-Spock primaries with standbys (binary replication)
	// These now get a cluster wrapper with type "binary" so UI shows cluster header.
	// Skip connections pinned to a manual cluster (membership_source =
	// 'manual'); they must not feed auto-detected cluster creation.
	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
			continue
		}
		if conn.MembershipSource == "manual" {
			continue
		}
		// Check if this is a primary with standbys (and not a Spock node)
		if !conn.HasSpock && (conn.PrimaryRole == "binary_primary" && len(childrenMap[conn.ID]) > 0) {
			// Compute auto_cluster_key first to check if claimed
			autoKey := fmt.Sprintf("binary:%d", conn.ID)

			// Skip if this cluster has been moved to a manual group
			if claimedKeys[autoKey] {
				continue
			}

			// Build server with children. This is an auto binary
			// cluster, so manually pinned standbys must be excluded.
			server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections, false)

			clusterName := conn.Name
			clusterDescription := ""
			if override, ok := clusterOverrides[autoKey]; ok {
				clusterName = override.Name
				clusterDescription = override.Description
			}

			cluster := TopologyCluster{
				ID:             fmt.Sprintf("server-%d", conn.ID),
				Name:           clusterName,
				Description:    clusterDescription,
				ClusterType:    "binary", // UI will show cluster header for binary replication
				AutoClusterKey: autoKey,
				Servers:        []TopologyServerInfo{server},
			}
			clusters = append(clusters, cluster)
		}
	}

	// 2.5. Group logical replication publishers with their subscribers
	// Match subscribers to publishers based on publisher_host:publisher_port
	logicalClusters := d.groupLogicalReplicationByPublisher(connections, connByID, connByHostPort, connByNamePort, assignedConnections, clusterOverrides)
	// Filter out claimed logical clusters
	for _, cluster := range logicalClusters {
		if cluster.AutoClusterKey != "" && claimedKeys[cluster.AutoClusterKey] {
			continue
		}
		clusters = append(clusters, cluster)
	}

	// 3. Handle standalone servers and servers without children
	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
			continue
		}

		// Skip connections that have a persisted cluster_id. These
		// belong to a cluster (manual or auto-detected) and will be
		// rendered inside that cluster, not as standalone.
		if conn.ClusterID.Valid {
			continue
		}

		// Compute auto_cluster_key for standalone servers
		autoKey := fmt.Sprintf("standalone:%d", conn.ID)

		// Skip if this standalone has been moved to a manual group
		if claimedKeys[autoKey] {
			continue
		}

		// Build server (with any children if applicable). Standalone is
		// an auto-detected entry, so manually pinned children must not
		// leak into its tree.
		server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections, false)
		standaloneDescription := ""
		if override, ok := clusterOverrides[autoKey]; ok {
			standaloneDescription = override.Description
		}
		standaloneCluster := TopologyCluster{
			ID:             fmt.Sprintf("server-%d", conn.ID),
			Name:           conn.Name,
			Description:    standaloneDescription,
			ClusterType:    "server", // UI will not show cluster header for this type
			AutoClusterKey: autoKey,
			Servers:        []TopologyServerInfo{server},
		}
		clusters = append(clusters, standaloneCluster)
	}

	// Create the topology group using the database-backed default group
	group := TopologyGroup{
		ID:        fmt.Sprintf("group-%d", defaultGroup.ID),
		Name:      defaultGroup.Name,
		IsDefault: true,
		Clusters:  clusters,
	}

	return []TopologyGroup{group}
}

// groupSpockNodesByClusters groups Spock nodes into clusters based on naming patterns
func (d *Datastore) groupSpockNodesByClusters(
	spockNodes []*connectionWithRole,
	childrenMap map[int][]int,
	connByID map[int]*connectionWithRole,
	assignedConnections map[int]bool,
	overrides map[string]clusterOverride,
) []TopologyCluster {
	if len(spockNodes) == 0 {
		return nil
	}

	// Group nodes by naming prefix (e.g., "pg17-" or "pg18-")
	// This is a heuristic - ideally we'd have explicit cluster metadata
	nodesByPrefix := make(map[string][]*connectionWithRole)
	for _, node := range spockNodes {
		prefix := d.extractClusterPrefix(node.Name)
		nodesByPrefix[prefix] = append(nodesByPrefix[prefix], node)
	}

	var clusters []TopologyCluster
	for prefix, nodes := range nodesByPrefix {
		// Determine cluster type based on whether nodes have hot standbys
		hasHotStandbys := false
		for _, node := range nodes {
			if len(childrenMap[node.ID]) > 0 {
				hasHotStandbys = true
				break
			}
		}

		clusterType := "spock"
		clusterName := fmt.Sprintf("%s Spock", prefix)
		if hasHotStandbys {
			clusterType = "spock_ha"
			clusterName = fmt.Sprintf("%s Spock HA", prefix)
		}

		// Compute auto_cluster_key and check for custom name/description
		autoKey := fmt.Sprintf("spock:%s", prefix)
		clusterDescription := ""
		if override, ok := overrides[autoKey]; ok {
			clusterName = override.Name
			clusterDescription = override.Description
		}

		cluster := TopologyCluster{
			ID:             fmt.Sprintf("cluster-spock-%s", prefix),
			Name:           clusterName,
			Description:    clusterDescription,
			ClusterType:    clusterType,
			AutoClusterKey: autoKey,
			Servers:        make([]TopologyServerInfo, 0, len(nodes)),
		}

		for _, node := range nodes {
			// Use buildServerWithChildren to include hot standbys as
			// children. This is an auto Spock (or Spock HA) cluster, so
			// manually pinned hot standbys must be excluded.
			server := d.buildServerWithChildren(node, childrenMap, connByID, assignedConnections, false)
			cluster.Servers = append(cluster.Servers, server)
		}

		clusters = append(clusters, cluster)
	}

	return clusters
}

// groupLogicalReplicationByPublisher groups logical subscribers with their publishers
// by matching subscriber's publisher_host:publisher_port to connection host:port
func (d *Datastore) groupLogicalReplicationByPublisher(
	connections []connectionWithRole,
	connByID map[int]*connectionWithRole,
	connByHostPort map[string]*connectionWithRole,
	connByNamePort map[string]*connectionWithRole,
	assignedConnections map[int]bool,
	overrides map[string]clusterOverride,
) []TopologyCluster {
	// Build publisher->subscribers map by matching publisher_host:port to connections.
	// Subscribers or publishers with membership_source = 'manual' are
	// pinned to a manually created cluster and must not be used to build
	// an auto-detected logical-replication cluster.
	subscribersByPublisher := make(map[int][]*connectionWithRole) // publisher connection ID -> subscribers

	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
			continue
		}
		if conn.MembershipSource == "manual" {
			continue
		}

		// Only process logical subscribers that have publisher connection info
		if conn.PrimaryRole != "logical_subscriber" {
			continue
		}
		if !conn.PublisherHost.Valid || !conn.PublisherPort.Valid {
			continue
		}

		// Try to match publisher_host:port to a known connection
		pubKey := fmt.Sprintf("%s:%d", conn.PublisherHost.String, conn.PublisherPort.Int32)

		var publisher *connectionWithRole
		// Try IP-based matching first
		if pub, exists := connByHostPort[pubKey]; exists {
			publisher = pub
		} else if pub, exists := connByNamePort[pubKey]; exists {
			// Fall back to name-based matching (publisher_host often contains hostname)
			publisher = pub
		}

		if publisher != nil && !assignedConnections[publisher.ID] && publisher.MembershipSource != "manual" {
			subscribersByPublisher[publisher.ID] = append(subscribersByPublisher[publisher.ID], conn)
		}
	}

	var clusters []TopologyCluster

	// Create clusters for publishers that have subscribers
	for pubID, subscribers := range subscribersByPublisher {
		publisher := connByID[pubID]
		if publisher == nil || len(subscribers) == 0 {
			continue
		}

		// Mark publisher as assigned
		assignedConnections[publisher.ID] = true

		// Build publisher server with subscribers as children
		pubOwner := ""
		if publisher.OwnerUsername.Valid {
			pubOwner = publisher.OwnerUsername.String
		}
		pubVersion := ""
		if publisher.Version.Valid {
			pubVersion = publisher.Version.String
		}
		pubOS := ""
		if publisher.OS.Valid {
			pubOS = publisher.OS.String
		}
		pubSpockNodeName := ""
		if publisher.SpockNodeName.Valid {
			pubSpockNodeName = publisher.SpockNodeName.String
		}
		pubSpockVersion := ""
		if publisher.SpockVersion.Valid {
			pubSpockVersion = publisher.SpockVersion.String
		}
		pubDescription := ""
		if publisher.Description.Valid {
			pubDescription = publisher.Description.String
		}
		pubServer := TopologyServerInfo{
			ID:               publisher.ID,
			Name:             publisher.Name,
			Description:      pubDescription,
			Host:             publisher.Host,
			Port:             publisher.Port,
			Status:           publisher.Status,
			Role:             d.mapPrimaryRoleToDisplayRole(publisher.PrimaryRole),
			PrimaryRole:      publisher.PrimaryRole,
			IsExpandable:     true,
			MembershipSource: publisher.MembershipSource,
			OwnerUsername:    pubOwner,
			Version:          pubVersion,
			OS:               pubOS,
			SpockNodeName:    pubSpockNodeName,
			SpockVersion:     pubSpockVersion,
			DatabaseName:     publisher.DatabaseName,
			Username:         publisher.Username,
			ActiveAlertCount: publisher.ActiveAlertCount,
			Children:         make([]TopologyServerInfo, 0, len(subscribers)),
		}

		// Add subscribers as children
		for _, sub := range subscribers {
			assignedConnections[sub.ID] = true
			subOwner := ""
			if sub.OwnerUsername.Valid {
				subOwner = sub.OwnerUsername.String
			}
			subVersion := ""
			if sub.Version.Valid {
				subVersion = sub.Version.String
			}
			subOS := ""
			if sub.OS.Valid {
				subOS = sub.OS.String
			}
			subSpockNodeName := ""
			if sub.SpockNodeName.Valid {
				subSpockNodeName = sub.SpockNodeName.String
			}
			subSpockVersion := ""
			if sub.SpockVersion.Valid {
				subSpockVersion = sub.SpockVersion.String
			}
			subDescription := ""
			if sub.Description.Valid {
				subDescription = sub.Description.String
			}
			subServer := TopologyServerInfo{
				ID:               sub.ID,
				Name:             sub.Name,
				Description:      subDescription,
				Host:             sub.Host,
				Port:             sub.Port,
				Status:           sub.Status,
				Role:             d.mapPrimaryRoleToDisplayRole(sub.PrimaryRole),
				PrimaryRole:      sub.PrimaryRole,
				IsExpandable:     false,
				MembershipSource: sub.MembershipSource,
				OwnerUsername:    subOwner,
				Version:          subVersion,
				OS:               subOS,
				SpockNodeName:    subSpockNodeName,
				SpockVersion:     subSpockVersion,
				DatabaseName:     sub.DatabaseName,
				Username:         sub.Username,
				ActiveAlertCount: sub.ActiveAlertCount,
				Children:         nil,
			}
			pubServer.Children = append(pubServer.Children, subServer)
		}

		// Compute auto_cluster_key and check for custom name/description
		autoKey := fmt.Sprintf("logical:%d", publisher.ID)
		clusterName := publisher.Name
		clusterDescription := ""
		if override, ok := overrides[autoKey]; ok {
			clusterName = override.Name
			clusterDescription = override.Description
		}

		// Use cluster_type "logical" so UI shows cluster header for logical replication
		cluster := TopologyCluster{
			ID:             fmt.Sprintf("server-%d", publisher.ID),
			Name:           clusterName,
			Description:    clusterDescription,
			ClusterType:    "logical",
			AutoClusterKey: autoKey,
			Servers:        []TopologyServerInfo{pubServer},
		}
		clusters = append(clusters, cluster)
	}

	return clusters
}

// extractClusterPrefix extracts a cluster prefix from a connection name
// e.g., "pg17-node1" -> "pg17", "pg18-spock1" -> "pg18"
func (d *Datastore) extractClusterPrefix(name string) string {
	// Look for common patterns: pgXX-*, name-XX, etc.
	for i, ch := range name {
		if ch == '-' && i > 0 {
			return name[:i]
		}
	}
	// No separator found, use the whole name
	return name
}

// buildServerWithChildren recursively builds server tree with standbys as children.
//
// The allowManual parameter controls whether children whose
// MembershipSource is "manual" are included in the returned tree. Auto
// cluster builders (binary, spock, standalone) must pass false so that a
// child pinned to a manual cluster does not leak into the auto-detected
// tree; manual cluster builders (which legitimately include every server
// they own) must pass true. See issue #74.
func (d *Datastore) buildServerWithChildren(
	conn *connectionWithRole,
	childrenMap map[int][]int,
	connByID map[int]*connectionWithRole,
	assignedConnections map[int]bool,
	allowManual bool,
) TopologyServerInfo {
	assignedConnections[conn.ID] = true

	ownerUsername := ""
	if conn.OwnerUsername.Valid {
		ownerUsername = conn.OwnerUsername.String
	}

	version := ""
	if conn.Version.Valid {
		version = conn.Version.String
	}

	os := ""
	if conn.OS.Valid {
		os = conn.OS.String
	}

	spockNodeName := ""
	if conn.SpockNodeName.Valid {
		spockNodeName = conn.SpockNodeName.String
	}

	spockVersion := ""
	if conn.SpockVersion.Valid {
		spockVersion = conn.SpockVersion.String
	}

	description := ""
	if conn.Description.Valid {
		description = conn.Description.String
	}

	server := TopologyServerInfo{
		ID:               conn.ID,
		Name:             conn.Name,
		Description:      description,
		Host:             conn.Host,
		Port:             conn.Port,
		Status:           conn.Status,
		Role:             d.mapPrimaryRoleToDisplayRole(conn.PrimaryRole),
		PrimaryRole:      conn.PrimaryRole,
		IsExpandable:     len(childrenMap[conn.ID]) > 0,
		MembershipSource: conn.MembershipSource,
		OwnerUsername:    ownerUsername,
		Version:          version,
		OS:               os,
		SpockNodeName:    spockNodeName,
		SpockVersion:     spockVersion,
		DatabaseName:     conn.DatabaseName,
		Username:         conn.Username,
		ActiveAlertCount: conn.ActiveAlertCount,
		Children:         make([]TopologyServerInfo, 0),
	}

	if conn.ConnectionError.Valid {
		server.ConnectionError = conn.ConnectionError.String
	}

	// Recursively add children. When allowManual is false, skip any
	// child whose MembershipSource is "manual": that connection has been
	// pinned to a manually created cluster and must not leak into the
	// auto-detected tree (issue #74).
	for _, childID := range childrenMap[conn.ID] {
		child, exists := connByID[childID]
		if !exists || assignedConnections[childID] {
			continue
		}
		if !allowManual && child.MembershipSource == "manual" {
			continue
		}
		childServer := d.buildServerWithChildren(child, childrenMap, connByID, assignedConnections, allowManual)
		server.Children = append(server.Children, childServer)
	}

	return server
}

// mapPrimaryRoleToDisplayRole maps the detailed primary_role to a simpler display role
func (d *Datastore) mapPrimaryRoleToDisplayRole(primaryRole string) string {
	switch primaryRole {
	case "binary_primary":
		return "primary"
	case "binary_standby", "binary_cascading":
		return "standby"
	case "spock_node":
		return "spock"
	case "spock_standby":
		return "spock_standby"
	case "logical_publisher":
		return "publisher"
	case "logical_subscriber":
		return "subscriber"
	case "logical_bidirectional":
		return "bidirectional"
	case "standalone":
		return "standalone"
	default:
		return primaryRole
	}
}

// Alert represents an alert from the alerter
type Alert struct {
	ID             int64      `json:"id"`
	AlertType      string     `json:"alert_type"`
	RuleID         *int64     `json:"rule_id,omitempty"`
	ConnectionID   int        `json:"connection_id"`
	DatabaseName   *string    `json:"database_name,omitempty"`
	ObjectName     *string    `json:"object_name,omitempty"`
	ProbeName      *string    `json:"probe_name,omitempty"`
	MetricName     *string    `json:"metric_name,omitempty"`
	MetricValue    *float64   `json:"metric_value,omitempty"`
	MetricUnit     *string    `json:"metric_unit,omitempty"`
	ThresholdValue *float64   `json:"threshold_value,omitempty"`
	Operator       *string    `json:"operator,omitempty"`
	Severity       string     `json:"severity"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	CorrelationID  *string    `json:"correlation_id,omitempty"`
	Status         string     `json:"status"`
	TriggeredAt    time.Time  `json:"triggered_at"`
	ClearedAt      *time.Time `json:"cleared_at,omitempty"`
	LastUpdated    *time.Time `json:"last_updated,omitempty"`
	AnomalyScore   *float64   `json:"anomaly_score,omitempty"`
	AnomalyDetails *string    `json:"anomaly_details,omitempty"`
	ServerName     string     `json:"server_name,omitempty"`
	// Acknowledgment fields (from alert_acknowledgments table)
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy *string    `json:"acknowledged_by,omitempty"`
	AckMessage     *string    `json:"ack_message,omitempty"`
	FalsePositive  *bool      `json:"false_positive,omitempty"`
	// AI analysis cache fields
	AIAnalysis            *string  `json:"ai_analysis,omitempty"`
	AIAnalysisMetricValue *float64 `json:"ai_analysis_metric_value,omitempty"`
}

// AlertListFilter holds filter options for listing alerts
type AlertListFilter struct {
	ConnectionID   *int
	ConnectionIDs  []int
	DatabaseName   *string
	Status         *string
	Severity       *string
	AlertType      *string
	StartTime      *time.Time
	EndTime        *time.Time
	ExcludeCleared bool // If true, only return alerts where cleared_at IS NULL
	Limit          int
	Offset         int
}

// AlertListResult holds the result of listing alerts
type AlertListResult struct {
	Alerts []Alert `json:"alerts"`
	Total  int64   `json:"total"`
}

// GetAlerts retrieves alerts with optional filtering
func (d *Datastore) GetAlerts(ctx context.Context, filter AlertListFilter) (*AlertListResult, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Build the WHERE clause
	conditions := []string{}
	args := []any{}
	argNum := 1

	if filter.ConnectionID != nil {
		conditions = append(conditions, fmt.Sprintf("a.connection_id = $%d", argNum))
		args = append(args, *filter.ConnectionID)
		argNum++
	}

	if len(filter.ConnectionIDs) > 0 {
		placeholders := make([]string, len(filter.ConnectionIDs))
		for i, id := range filter.ConnectionIDs {
			placeholders[i] = fmt.Sprintf("$%d", argNum)
			args = append(args, id)
			argNum++
		}
		conditions = append(conditions, fmt.Sprintf("a.connection_id IN (%s)", strings.Join(placeholders, ", ")))
	}

	if filter.Status != nil {
		conditions = append(conditions, fmt.Sprintf("a.status = $%d", argNum))
		args = append(args, *filter.Status)
		argNum++
	}

	if filter.Severity != nil {
		conditions = append(conditions, fmt.Sprintf("a.severity = $%d", argNum))
		args = append(args, *filter.Severity)
		argNum++
	}

	if filter.AlertType != nil {
		conditions = append(conditions, fmt.Sprintf("a.alert_type = $%d", argNum))
		args = append(args, *filter.AlertType)
		argNum++
	}

	if filter.StartTime != nil {
		conditions = append(conditions, fmt.Sprintf("a.triggered_at >= $%d", argNum))
		args = append(args, *filter.StartTime)
		argNum++
	}

	if filter.EndTime != nil {
		conditions = append(conditions, fmt.Sprintf("a.triggered_at <= $%d", argNum))
		args = append(args, *filter.EndTime)
		argNum++
	}

	if filter.ExcludeCleared {
		conditions = append(conditions, "a.cleared_at IS NULL")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Count total matching alerts
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM alerts a
		%s
	`, whereClause)

	var total int64
	err := d.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("failed to count alerts: %w", err)
	}

	// Apply limit and offset
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	// Query alerts with connection name, metric unit, and acknowledgment info
	// Uses DISTINCT ON to get only the most recent acknowledgment per alert
	query := fmt.Sprintf(`
		SELECT a.id, a.alert_type, a.rule_id, a.connection_id, a.database_name,
		       a.object_name, a.probe_name, a.metric_name, a.metric_value, r.metric_unit,
		       a.threshold_value, a.operator, a.severity, a.title, a.description,
		       a.correlation_id, a.status, a.triggered_at, a.cleared_at,
		       a.last_updated, a.anomaly_score, a.anomaly_details,
		       COALESCE(c.name, 'Unknown') as server_name,
		       ack.acknowledged_at, ack.acknowledged_by, ack.message, ack.false_positive,
		       a.ai_analysis, a.ai_analysis_metric_value
		FROM alerts a
		LEFT JOIN connections c ON a.connection_id = c.id
		LEFT JOIN alert_rules r ON a.rule_id = r.id
		LEFT JOIN LATERAL (
			SELECT acknowledged_at, acknowledged_by, message, false_positive
			FROM alert_acknowledgments
			WHERE alert_id = a.id
			ORDER BY acknowledged_at DESC
			LIMIT 1
		) ack ON true
		%s
		ORDER BY a.triggered_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argNum, argNum+1)

	args = append(args, limit, offset)

	rows, err := d.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query alerts: %w", err)
	}
	defer rows.Close()

	var alerts []Alert
	for rows.Next() {
		var alert Alert
		err := rows.Scan(
			&alert.ID, &alert.AlertType, &alert.RuleID, &alert.ConnectionID,
			&alert.DatabaseName, &alert.ObjectName, &alert.ProbeName, &alert.MetricName,
			&alert.MetricValue, &alert.MetricUnit, &alert.ThresholdValue, &alert.Operator,
			&alert.Severity, &alert.Title, &alert.Description,
			&alert.CorrelationID, &alert.Status, &alert.TriggeredAt,
			&alert.ClearedAt, &alert.LastUpdated,
			&alert.AnomalyScore, &alert.AnomalyDetails,
			&alert.ServerName,
			&alert.AcknowledgedAt, &alert.AcknowledgedBy, &alert.AckMessage,
			&alert.FalsePositive,
			&alert.AIAnalysis, &alert.AIAnalysisMetricValue,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}
		alerts = append(alerts, alert)
	}

	if alerts == nil {
		alerts = []Alert{}
	}

	return &AlertListResult{
		Alerts: alerts,
		Total:  total,
	}, nil
}

// AlertCountsResult contains alert counts grouped by server
type AlertCountsResult struct {
	Total    int64         `json:"total"`
	ByServer map[int]int64 `json:"by_server"`
}

// GetAlertCounts returns counts of active alerts grouped by connection_id.
// When connectionIDs is non-nil, the query is restricted to alerts whose
// connection_id appears in the slice. A nil slice means "no filter"
// (superuser or wildcard-scoped caller); an empty non-nil slice returns
// an empty result without touching the database.
func (d *Datastore) GetAlertCounts(ctx context.Context, connectionIDs []int) (*AlertCountsResult, error) {
	// An explicit empty allow-list means the caller can see no
	// connections; avoid the database round-trip entirely.
	if connectionIDs != nil && len(connectionIDs) == 0 {
		return &AlertCountsResult{
			Total:    0,
			ByServer: make(map[int]int64),
		}, nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	var (
		total    int64
		rows     pgx.Rows
		queryErr error
	)

	if connectionIDs == nil {
		queryErr = d.pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM alerts
			WHERE status = 'active'
		`).Scan(&total)
		if queryErr != nil {
			return nil, fmt.Errorf("failed to count total alerts: %w", queryErr)
		}

		rows, queryErr = d.pool.Query(ctx, `
			SELECT connection_id, COUNT(*) as count
			FROM alerts
			WHERE status = 'active'
			GROUP BY connection_id
		`)
	} else {
		queryErr = d.pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM alerts
			WHERE status = 'active'
			  AND connection_id = ANY($1)
		`, connectionIDs).Scan(&total)
		if queryErr != nil {
			return nil, fmt.Errorf("failed to count total alerts: %w", queryErr)
		}

		rows, queryErr = d.pool.Query(ctx, `
			SELECT connection_id, COUNT(*) as count
			FROM alerts
			WHERE status = 'active'
			  AND connection_id = ANY($1)
			GROUP BY connection_id
		`, connectionIDs)
	}
	if queryErr != nil {
		return nil, fmt.Errorf("failed to query alert counts: %w", queryErr)
	}
	defer rows.Close()

	byServer := make(map[int]int64)
	for rows.Next() {
		var connID int
		var count int64
		if err := rows.Scan(&connID, &count); err != nil {
			return nil, fmt.Errorf("failed to scan alert count: %w", err)
		}
		byServer[connID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate alert counts: %w", err)
	}

	return &AlertCountsResult{
		Total:    total,
		ByServer: byServer,
	}, nil
}

// GetAlertConnectionID returns the connection_id for the given alert.
func (d *Datastore) GetAlertConnectionID(ctx context.Context, alertID int64) (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var connectionID int
	err := d.pool.QueryRow(ctx,
		"SELECT connection_id FROM alerts WHERE id = $1", alertID).Scan(&connectionID)
	if err != nil {
		return 0, fmt.Errorf("failed to get alert connection ID: %w", err)
	}
	return connectionID, nil
}

// SaveAlertAnalysis saves an AI analysis result for an alert
func (d *Datastore) SaveAlertAnalysis(ctx context.Context, alertID int64, analysis string, metricValue float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.pool.Exec(ctx, `
		UPDATE alerts
		SET ai_analysis = $2, ai_analysis_metric_value = $3
		WHERE id = $1
	`, alertID, analysis, metricValue)
	return err
}

// AcknowledgeAlertRequest contains the data for acknowledging an alert
type AcknowledgeAlertRequest struct {
	AlertID        int64  `json:"alert_id"`
	AcknowledgedBy string `json:"acknowledged_by"`
	Message        string `json:"message"`
	FalsePositive  bool   `json:"false_positive"`
}

// AcknowledgeAlert acknowledges an alert, updating its status and creating
// an acknowledgment record
func (d *Datastore) AcknowledgeAlert(ctx context.Context, req AcknowledgeAlertRequest) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	logger.Infof("AcknowledgeAlert: starting for alert_id=%d, by=%s, false_positive=%v",
		req.AlertID, req.AcknowledgedBy, req.FalsePositive)

	// Start a transaction
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		logger.Errorf("AcknowledgeAlert: failed to begin transaction: %v", err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	//nolint:errcheck // Rollback is no-op if already committed
	defer tx.Rollback(ctx)

	// Update alert status to acknowledged
	result, err := tx.Exec(ctx, `
		UPDATE alerts
		SET status = 'acknowledged'
		WHERE id = $1 AND status = 'active'
	`, req.AlertID)
	if err != nil {
		logger.Errorf("AcknowledgeAlert: failed to update alert status: %v", err)
		return fmt.Errorf("failed to update alert status: %w", err)
	}

	rowsAffected := result.RowsAffected()
	logger.Infof("AcknowledgeAlert: UPDATE affected %d rows", rowsAffected)

	if rowsAffected == 0 {
		logger.Infof("AcknowledgeAlert: alert %d not found or already acknowledged", req.AlertID)
		return fmt.Errorf("alert not found or already acknowledged")
	}

	// Create acknowledgment record
	_, err = tx.Exec(ctx, `
		INSERT INTO alert_acknowledgments (alert_id, acknowledged_by, message, acknowledge_type, false_positive)
		VALUES ($1, $2, $3, 'acknowledge', $4)
	`, req.AlertID, req.AcknowledgedBy, req.Message, req.FalsePositive)
	if err != nil {
		logger.Errorf("AcknowledgeAlert: failed to create acknowledgment record: %v", err)
		return fmt.Errorf("failed to create acknowledgment record: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Errorf("AcknowledgeAlert: failed to commit transaction: %v", err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	logger.Infof("AcknowledgeAlert: successfully acknowledged alert %d", req.AlertID)
	return nil
}

// UnacknowledgeAlert removes acknowledgment from an alert, returning it
// to active status and deleting any alert_acknowledgments rows so the
// server's alert listing query no longer surfaces the alert as
// acknowledged. Both statements run in a single transaction so the alert
// cannot end up with status = 'active' while a stale acknowledgment row
// still exists (or the reverse).
func (d *Datastore) UnacknowledgeAlert(ctx context.Context, alertID int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	//nolint:errcheck // Rollback is a no-op if the tx was already committed.
	defer tx.Rollback(ctx)

	// Update alert status back to active.
	result, err := tx.Exec(ctx, `
		UPDATE alerts
		SET status = 'active'
		WHERE id = $1 AND status = 'acknowledged'
	`, alertID)
	if err != nil {
		return fmt.Errorf("failed to update alert status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("alert not found or not acknowledged")
	}

	// Clear acknowledgment rows so the LATERAL join in GetAlerts no
	// longer returns a stale acknowledged_at for this alert.
	if _, err := tx.Exec(ctx, `
		DELETE FROM alert_acknowledgments WHERE alert_id = $1
	`, alertID); err != nil {
		return fmt.Errorf("failed to clear alert acknowledgments: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetEstateSnapshot gathers all data needed for an AI overview of the
// estate. It returns a point-in-time snapshot of server status, alerts,
// blackouts, and recent events. If individual sub-queries fail, partial
// data is returned with what succeeded.
func (d *Datastore) GetEstateSnapshot(ctx context.Context) (*EstateSnapshot, error) {
	snapshotCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	snapshot := &EstateSnapshot{
		Timestamp:         time.Now().UTC(),
		Servers:           []EstateServerSummary{},
		TopAlerts:         []EstateAlertSummary{},
		ActiveBlackouts:   []EstateBlackoutSummary{},
		UpcomingBlackouts: []EstateBlackoutSummary{},
		RecentEvents:      []EstateEventSummary{},
	}

	// Gather server topology and compute status counts
	d.gatherEstateServerData(snapshotCtx, snapshot)

	// Gather alert summary and top alerts
	d.gatherEstateAlertData(snapshotCtx, snapshot)

	// Gather active and upcoming blackout periods
	d.gatherEstateBlackoutData(snapshotCtx, snapshot)

	// Gather recent events from the last 24 hours
	d.gatherEstateRecentEvents(snapshotCtx, snapshot)

	return snapshot, nil
}

// gatherEstateServerData populates server status counts and per-server
// details by walking the cluster topology hierarchy. Servers are
// classified as offline (status is offline), warning (has active alerts
// but not offline), or online (no alerts, not offline).
func (d *Datastore) gatherEstateServerData(ctx context.Context, snapshot *EstateSnapshot) {
	// Sync cluster_id assignments before reading topology
	if err := d.RefreshClusterAssignments(ctx); err != nil {
		logger.Infof("gatherEstateServerData: failed to refresh cluster assignments: %v", err)
	}

	groups, err := d.GetClusterTopology(ctx, nil)
	if err != nil {
		logger.Errorf("GetEstateSnapshot: failed to get cluster topology: %v", err)
		return
	}

	var servers []EstateServerSummary
	for _, group := range groups {
		for _, cluster := range group.Clusters {
			flattenTopologyServers(cluster.Servers, &servers)
		}
	}

	// Map roles to display-friendly names and compute status counts
	for i := range servers {
		servers[i].Role = d.mapPrimaryRoleToDisplayRole(servers[i].Role)

		switch {
		case servers[i].Status == "offline":
			snapshot.ServerOffline++
		case servers[i].ActiveAlertCount > 0:
			snapshot.ServerWarning++
		default:
			snapshot.ServerOnline++
		}
	}

	snapshot.Servers = servers
	snapshot.ServerTotal = len(servers)
}

// flattenTopologyServers recursively extracts server summaries from the
// nested topology hierarchy into a flat slice. Children (such as hot
// standbys) are included alongside their parents.
func flattenTopologyServers(servers []TopologyServerInfo, result *[]EstateServerSummary) {
	for i := range servers {
		s := &servers[i]
		*result = append(*result, EstateServerSummary{
			ID:               s.ID,
			Name:             s.Name,
			Status:           s.Status,
			Role:             s.PrimaryRole,
			ActiveAlertCount: s.ActiveAlertCount,
		})
		if len(s.Children) > 0 {
			flattenTopologyServers(s.Children, result)
		}
	}
}

// gatherEstateAlertData populates alert severity counts and the top
// active alerts list. Active alerts are retrieved in order of most
// recent trigger time. Severity counts are computed from the returned
// set; if total active alerts exceed the query limit of 500, the
// breakdown is approximate while AlertTotal remains accurate.
func (d *Datastore) gatherEstateAlertData(ctx context.Context, snapshot *EstateSnapshot) {
	activeStatus := "active"
	result, err := d.GetAlerts(ctx, AlertListFilter{
		Status: &activeStatus,
		Limit:  500,
	})
	if err != nil {
		logger.Errorf("GetEstateSnapshot: failed to get alerts: %v", err)
		return
	}

	snapshot.AlertTotal = int(result.Total)

	for i := range result.Alerts {
		alert := &result.Alerts[i]
		switch alert.Severity {
		case "critical":
			snapshot.AlertCritical++
		case "warning":
			snapshot.AlertWarning++
		case "info":
			snapshot.AlertInfo++
		}

		if i < 10 {
			snapshot.TopAlerts = append(snapshot.TopAlerts, EstateAlertSummary{
				Title:      alert.Title,
				ServerName: alert.ServerName,
				Severity:   alert.Severity,
			})
		}
	}
}

// gatherEstateBlackoutData populates active blackouts and upcoming
// blackouts. Upcoming blackouts are one-time blackouts whose start
// time falls within the next 24 hours. Scheduled blackout occurrences
// are not evaluated because that would require a cron expression
// parser.
func (d *Datastore) gatherEstateBlackoutData(ctx context.Context, snapshot *EstateSnapshot) {
	// Retrieve recent blackouts ordered by start_time DESC; future
	// blackouts appear first, followed by currently active ones
	result, err := d.ListBlackouts(ctx, BlackoutFilter{
		Limit: 100,
	})
	if err != nil {
		logger.Errorf("GetEstateSnapshot: failed to get blackouts: %v", err)
		return
	}

	now := time.Now().UTC()
	cutoff := now.Add(24 * time.Hour)

	for i := range result.Blackouts {
		b := &result.Blackouts[i]
		summary := EstateBlackoutSummary{
			Scope:     b.Scope,
			Reason:    b.Reason,
			StartTime: b.StartTime,
			EndTime:   b.EndTime,
		}

		if b.IsActive {
			snapshot.ActiveBlackouts = append(snapshot.ActiveBlackouts, summary)
		} else if b.StartTime.After(now) && b.StartTime.Before(cutoff) {
			snapshot.UpcomingBlackouts = append(snapshot.UpcomingBlackouts, summary)
		}
	}
}

// gatherEstateRecentEvents populates recent events from the last 24
// hours, focusing on restarts, configuration changes, and blackout
// transitions. Events are returned in reverse chronological order
// with a maximum of 20 entries.
func (d *Datastore) gatherEstateRecentEvents(ctx context.Context, snapshot *EstateSnapshot) {
	now := time.Now().UTC()
	dayAgo := now.Add(-24 * time.Hour)

	result, err := d.GetTimelineEvents(ctx, TimelineFilter{
		StartTime: dayAgo,
		EndTime:   now,
		EventTypes: []string{
			EventTypeRestart,
			EventTypeConfigChange,
			EventTypeBlackoutStarted,
			EventTypeBlackoutEnded,
		},
		Limit: 20,
	})
	if err != nil {
		logger.Errorf("GetEstateSnapshot: failed to get timeline events: %v", err)
		return
	}

	for i := range result.Events {
		event := &result.Events[i]
		snapshot.RecentEvents = append(snapshot.RecentEvents, EstateEventSummary{
			EventType:  event.EventType,
			ServerName: event.ServerName,
			OccurredAt: event.OccurredAt,
			Severity:   event.Severity,
			Title:      event.Title,
			Summary:    event.Summary,
		})
	}
}

// GetServerSnapshot returns an estate snapshot filtered to a single
// server (connection). It returns the same EstateSnapshot structure but
// populated only with data for the given connection ID.
func (d *Datastore) GetServerSnapshot(ctx context.Context, serverID int) (*EstateSnapshot, string, error) {
	// Verify the connection exists and get its name.
	conn, err := d.GetConnection(ctx, serverID)
	if err != nil {
		return nil, "", fmt.Errorf("server not found: %w", err)
	}

	snapshot := d.buildScopedSnapshot(ctx, []int{serverID})
	return snapshot, conn.Name, nil
}

// GetClusterSnapshot returns an estate snapshot filtered to all servers
// in a given cluster. It returns the snapshot, the cluster name, and
// any error encountered.
func (d *Datastore) GetClusterSnapshot(ctx context.Context, clusterID int) (*EstateSnapshot, string, error) {
	cluster, err := d.GetCluster(ctx, clusterID)
	if err != nil {
		return nil, "", fmt.Errorf("cluster not found: %w", err)
	}

	connectionIDs, err := d.getConnectionIDsForCluster(ctx, clusterID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get connections for cluster: %w", err)
	}

	snapshot := d.buildScopedSnapshot(ctx, connectionIDs)
	return snapshot, cluster.Name, nil
}

// GetGroupSnapshot returns an estate snapshot filtered to all servers
// in all clusters belonging to a given group. It returns the snapshot,
// the group name, and any error encountered.
func (d *Datastore) GetGroupSnapshot(ctx context.Context, groupID int) (*EstateSnapshot, string, error) {
	group, err := d.GetClusterGroup(ctx, groupID)
	if err != nil {
		return nil, "", fmt.Errorf("group not found: %w", err)
	}

	connectionIDs, err := d.getConnectionIDsForGroup(ctx, groupID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get connections for group: %w", err)
	}

	snapshot := d.buildScopedSnapshot(ctx, connectionIDs)
	return snapshot, group.Name, nil
}

// GetConnectionsSnapshot returns an estate snapshot filtered to the
// specified connection IDs. It is a public wrapper around
// buildScopedSnapshot for callers that already have a list of
// connection IDs and do not need scope-name resolution.
func (d *Datastore) GetConnectionsSnapshot(ctx context.Context, connectionIDs []int) *EstateSnapshot {
	return d.buildScopedSnapshot(ctx, connectionIDs)
}

// buildScopedSnapshot creates an EstateSnapshot containing only data
// for the specified connection IDs. It reuses the same gathering
// helpers as the estate-wide snapshot but filters by connection.
func (d *Datastore) buildScopedSnapshot(ctx context.Context, connectionIDs []int) *EstateSnapshot {
	snapshotCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	snapshot := &EstateSnapshot{
		Timestamp:         time.Now().UTC(),
		Servers:           []EstateServerSummary{},
		TopAlerts:         []EstateAlertSummary{},
		ActiveBlackouts:   []EstateBlackoutSummary{},
		UpcomingBlackouts: []EstateBlackoutSummary{},
		RecentEvents:      []EstateEventSummary{},
	}

	if len(connectionIDs) == 0 {
		return snapshot
	}

	// Gather server data filtered to the given connections.
	d.gatherScopedServerData(snapshotCtx, snapshot, connectionIDs)

	// Gather alert data filtered to the given connections.
	d.gatherScopedAlertData(snapshotCtx, snapshot, connectionIDs)

	// Gather blackout data (estate-wide; filtered post-query is not
	// possible because blackouts reference scopes, not connections
	// directly). Include all blackouts so the LLM can note relevant
	// maintenance windows.
	d.gatherEstateBlackoutData(snapshotCtx, snapshot)

	// Gather recent events filtered to the given connections.
	d.gatherScopedRecentEvents(snapshotCtx, snapshot, connectionIDs)

	return snapshot
}

// getConnectionIDsForCluster returns all connection IDs that belong
// to the given cluster.
func (d *Datastore) getConnectionIDsForCluster(ctx context.Context, clusterID int) ([]int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `SELECT id FROM connections WHERE cluster_id = $1 ORDER BY id`
	rows, err := d.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections for cluster: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan connection id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// getConnectionIDsForGroup returns all connection IDs that belong to
// any cluster in the given group.
func (d *Datastore) getConnectionIDsForGroup(ctx context.Context, groupID int) ([]int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT c.id
        FROM connections c
        JOIN clusters cl ON c.cluster_id = cl.id
        WHERE cl.group_id = $1
        ORDER BY c.id
    `
	rows, err := d.pool.Query(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections for group: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan connection id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetConnectionIDsForCluster returns all connection IDs that belong to
// the given cluster. It is an exported wrapper around the internal
// helper so other packages (notably the API handlers that apply RBAC
// visibility filtering) can enumerate a cluster's members without
// duplicating the SQL.
func (d *Datastore) GetConnectionIDsForCluster(ctx context.Context, clusterID int) ([]int, error) {
	return d.getConnectionIDsForCluster(ctx, clusterID)
}

// GetConnectionIDsForGroup returns all connection IDs that belong to
// any cluster in the given group. Exported companion to
// GetConnectionIDsForCluster for group-scoped RBAC checks.
func (d *Datastore) GetConnectionIDsForGroup(ctx context.Context, groupID int) ([]int, error) {
	return d.getConnectionIDsForGroup(ctx, groupID)
}

// gatherScopedServerData populates server status counts for a specific
// set of connection IDs by walking the full topology and filtering.
func (d *Datastore) gatherScopedServerData(ctx context.Context, snapshot *EstateSnapshot, connectionIDs []int) {
	idSet := make(map[int]bool, len(connectionIDs))
	for _, id := range connectionIDs {
		idSet[id] = true
	}

	// Sync cluster_id assignments before reading topology
	if err := d.RefreshClusterAssignments(ctx); err != nil {
		logger.Infof("gatherScopedServerData: failed to refresh cluster assignments: %v", err)
	}

	groups, err := d.GetClusterTopology(ctx, nil)
	if err != nil {
		logger.Errorf("GetScopedSnapshot: failed to get cluster topology: %v", err)
		return
	}

	var allServers []EstateServerSummary
	for _, group := range groups {
		for _, cluster := range group.Clusters {
			flattenTopologyServers(cluster.Servers, &allServers)
		}
	}

	// Filter to only the requested connections.
	var servers []EstateServerSummary
	for i := range allServers {
		if idSet[allServers[i].ID] {
			allServers[i].Role = d.mapPrimaryRoleToDisplayRole(allServers[i].Role)
			servers = append(servers, allServers[i])

			switch {
			case allServers[i].Status == "offline":
				snapshot.ServerOffline++
			case allServers[i].ActiveAlertCount > 0:
				snapshot.ServerWarning++
			default:
				snapshot.ServerOnline++
			}
		}
	}

	snapshot.Servers = servers
	snapshot.ServerTotal = len(servers)
}

// gatherScopedAlertData populates alert severity counts and top alerts
// for a specific set of connection IDs.
func (d *Datastore) gatherScopedAlertData(ctx context.Context, snapshot *EstateSnapshot, connectionIDs []int) {
	activeStatus := "active"
	result, err := d.GetAlerts(ctx, AlertListFilter{
		Status:        &activeStatus,
		ConnectionIDs: connectionIDs,
		Limit:         500,
	})
	if err != nil {
		logger.Errorf("GetScopedSnapshot: failed to get alerts: %v", err)
		return
	}

	snapshot.AlertTotal = int(result.Total)

	for i := range result.Alerts {
		switch result.Alerts[i].Severity {
		case "critical":
			snapshot.AlertCritical++
		case "warning":
			snapshot.AlertWarning++
		case "info":
			snapshot.AlertInfo++
		}

		if i < 10 {
			snapshot.TopAlerts = append(snapshot.TopAlerts, EstateAlertSummary{
				Title:      result.Alerts[i].Title,
				ServerName: result.Alerts[i].ServerName,
				Severity:   result.Alerts[i].Severity,
			})
		}
	}
}

// gatherScopedRecentEvents populates recent events from the last 24
// hours for a specific set of connection IDs.
func (d *Datastore) gatherScopedRecentEvents(ctx context.Context, snapshot *EstateSnapshot, connectionIDs []int) {
	now := time.Now().UTC()
	dayAgo := now.Add(-24 * time.Hour)

	result, err := d.GetTimelineEvents(ctx, TimelineFilter{
		StartTime:     dayAgo,
		EndTime:       now,
		ConnectionIDs: connectionIDs,
		EventTypes: []string{
			EventTypeRestart,
			EventTypeConfigChange,
			EventTypeBlackoutStarted,
			EventTypeBlackoutEnded,
		},
		Limit: 20,
	})
	if err != nil {
		logger.Errorf("GetScopedSnapshot: failed to get timeline events: %v", err)
		return
	}

	for i := range result.Events {
		snapshot.RecentEvents = append(snapshot.RecentEvents, EstateEventSummary{
			EventType:  result.Events[i].EventType,
			ServerName: result.Events[i].ServerName,
			OccurredAt: result.Events[i].OccurredAt,
			Severity:   result.Events[i].Severity,
			Title:      result.Events[i].Title,
			Summary:    result.Events[i].Summary,
		})
	}
}

// ConnectionContext holds comprehensive system context for a monitored connection
type ConnectionContext struct {
	ConnectionID int                `json:"connection_id"`
	ServerName   string             `json:"server_name"`
	PostgreSQL   *PostgreSQLContext `json:"postgresql,omitempty"`
	System       *SystemContext     `json:"system,omitempty"`
}

// PostgreSQLContext holds PostgreSQL server information
type PostgreSQLContext struct {
	Version             string            `json:"version,omitempty"`
	VersionNum          int               `json:"version_num,omitempty"`
	MaxConnections      int               `json:"max_connections,omitempty"`
	DataDirectory       string            `json:"data_directory,omitempty"`
	InstalledExtensions []string          `json:"installed_extensions,omitempty"`
	Settings            map[string]string `json:"settings,omitempty"`
}

// SystemContext holds operating system and hardware information
type SystemContext struct {
	OSName       string         `json:"os_name,omitempty"`
	OSVersion    string         `json:"os_version,omitempty"`
	Architecture string         `json:"architecture,omitempty"`
	Hostname     string         `json:"hostname,omitempty"`
	CPU          *CPUContext    `json:"cpu,omitempty"`
	Memory       *MemoryContext `json:"memory,omitempty"`
	Disks        []DiskContext  `json:"disks,omitempty"`
}

// CPUContext holds CPU information
type CPUContext struct {
	Model             string `json:"model,omitempty"`
	Cores             int    `json:"cores,omitempty"`
	LogicalProcessors int    `json:"logical_processors,omitempty"`
}

// MemoryContext holds memory information
type MemoryContext struct {
	TotalBytes int64 `json:"total_bytes,omitempty"`
	FreeBytes  int64 `json:"free_bytes,omitempty"`
}

// DiskContext holds disk information for a single mount point
type DiskContext struct {
	MountPoint     string `json:"mount_point"`
	FilesystemType string `json:"filesystem_type,omitempty"`
	TotalBytes     int64  `json:"total_bytes,omitempty"`
	UsedBytes      int64  `json:"used_bytes,omitempty"`
	FreeBytes      int64  `json:"free_bytes,omitempty"`
}

// GetConnectionContext retrieves comprehensive system context for a connection
func (d *Datastore) GetConnectionContext(ctx context.Context, connectionID int) (*ConnectionContext, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Get connection name
	var serverName string
	err := d.pool.QueryRow(ctx,
		`SELECT name FROM connections WHERE id = $1`, connectionID).Scan(&serverName)
	if err != nil {
		return nil, fmt.Errorf("connection not found: %w", err)
	}

	result := &ConnectionContext{
		ConnectionID: connectionID,
		ServerName:   serverName,
	}

	// Query pg_server_info for PostgreSQL version, max_connections, extensions, etc.
	pgCtx := &PostgreSQLContext{}
	hasPGData := false

	var version sql.NullString
	var versionNum sql.NullInt32
	var maxConns sql.NullInt32
	var dataDir sql.NullString
	var extensions []string

	err = d.pool.QueryRow(ctx, `
		SELECT server_version, server_version_num, max_connections,
		       data_directory, installed_extensions
		FROM metrics.pg_server_info
		WHERE connection_id = $1
		ORDER BY collected_at DESC
		LIMIT 1
	`, connectionID).Scan(&version, &versionNum, &maxConns, &dataDir, &extensions)
	if err == nil {
		hasPGData = true
		if version.Valid {
			pgCtx.Version = version.String
		}
		if versionNum.Valid {
			pgCtx.VersionNum = int(versionNum.Int32)
		}
		if maxConns.Valid {
			pgCtx.MaxConnections = int(maxConns.Int32)
		}
		if dataDir.Valid {
			pgCtx.DataDirectory = dataDir.String
		}
		if len(extensions) > 0 {
			pgCtx.InstalledExtensions = extensions
		}
	}

	// Query pg_settings for key configuration parameters
	settingsRows, err := d.pool.Query(ctx, `
		SELECT name, setting
		FROM metrics.pg_settings
		WHERE connection_id = $1
		  AND name IN (
		      'shared_buffers', 'work_mem', 'effective_cache_size',
		      'maintenance_work_mem', 'max_worker_processes',
		      'max_parallel_workers', 'max_parallel_workers_per_gather',
		      'wal_level', 'wal_buffers', 'random_page_cost',
		      'effective_io_concurrency', 'checkpoint_completion_target',
		      'huge_pages', 'temp_buffers'
		  )
		  AND collected_at = (
		      SELECT MAX(collected_at)
		      FROM metrics.pg_settings
		      WHERE connection_id = $1
		  )
		ORDER BY name
	`, connectionID)
	if err == nil {
		defer settingsRows.Close()
		settings := make(map[string]string)
		for settingsRows.Next() {
			var name string
			var setting sql.NullString
			if err := settingsRows.Scan(&name, &setting); err == nil && setting.Valid {
				settings[name] = setting.String
			}
		}
		if len(settings) > 0 {
			hasPGData = true
			pgCtx.Settings = settings
		}
	}

	if hasPGData {
		result.PostgreSQL = pgCtx
	}

	// Query system information (requires system_stats extension)
	sysCtx := &SystemContext{}
	hasSysData := false

	// OS info
	var osName, osVersion, architecture, hostname sql.NullString
	err = d.pool.QueryRow(ctx, `
		SELECT name, version, architecture, host_name
		FROM metrics.pg_sys_os_info
		WHERE connection_id = $1
		ORDER BY collected_at DESC
		LIMIT 1
	`, connectionID).Scan(&osName, &osVersion, &architecture, &hostname)
	if err == nil {
		hasSysData = true
		if osName.Valid {
			sysCtx.OSName = osName.String
		}
		if osVersion.Valid {
			sysCtx.OSVersion = osVersion.String
		}
		if architecture.Valid {
			sysCtx.Architecture = architecture.String
		}
		if hostname.Valid {
			sysCtx.Hostname = hostname.String
		}
	}

	// CPU info
	var cpuModel sql.NullString
	var cores, logicalProcs sql.NullInt32
	err = d.pool.QueryRow(ctx, `
		SELECT model_name, no_of_cores, logical_processor
		FROM metrics.pg_sys_cpu_info
		WHERE connection_id = $1
		ORDER BY collected_at DESC
		LIMIT 1
	`, connectionID).Scan(&cpuModel, &cores, &logicalProcs)
	if err == nil {
		hasSysData = true
		cpu := &CPUContext{}
		hasCPU := false
		if cpuModel.Valid {
			cpu.Model = cpuModel.String
			hasCPU = true
		}
		if cores.Valid {
			cpu.Cores = int(cores.Int32)
			hasCPU = true
		}
		if logicalProcs.Valid {
			cpu.LogicalProcessors = int(logicalProcs.Int32)
			hasCPU = true
		}
		if hasCPU {
			sysCtx.CPU = cpu
		}
	}

	// Memory info
	var totalMem, freeMem sql.NullInt64
	err = d.pool.QueryRow(ctx, `
		SELECT total_memory, free_memory
		FROM metrics.pg_sys_memory_info
		WHERE connection_id = $1
		ORDER BY collected_at DESC
		LIMIT 1
	`, connectionID).Scan(&totalMem, &freeMem)
	if err == nil {
		hasSysData = true
		if totalMem.Valid || freeMem.Valid {
			mem := &MemoryContext{}
			if totalMem.Valid {
				mem.TotalBytes = totalMem.Int64
			}
			if freeMem.Valid {
				mem.FreeBytes = freeMem.Int64
			}
			sysCtx.Memory = mem
		}
	}

	// Disk info
	diskRows, err := d.pool.Query(ctx, `
		SELECT mount_point, file_system_type, total_space, used_space, free_space
		FROM metrics.pg_sys_disk_info
		WHERE connection_id = $1
		  AND collected_at = (
		      SELECT MAX(collected_at)
		      FROM metrics.pg_sys_disk_info
		      WHERE connection_id = $1
		  )
		ORDER BY mount_point
	`, connectionID)
	if err == nil {
		defer diskRows.Close()
		var disks []DiskContext
		for diskRows.Next() {
			var mountPoint string
			var fsType sql.NullString
			var totalSpace, usedSpace, freeSpace sql.NullInt64
			if err := diskRows.Scan(&mountPoint, &fsType, &totalSpace,
				&usedSpace, &freeSpace); err == nil {
				disk := DiskContext{MountPoint: mountPoint}
				if fsType.Valid {
					disk.FilesystemType = fsType.String
				}
				if totalSpace.Valid {
					disk.TotalBytes = totalSpace.Int64
				}
				if usedSpace.Valid {
					disk.UsedBytes = usedSpace.Int64
				}
				if freeSpace.Valid {
					disk.FreeBytes = freeSpace.Int64
				}
				disks = append(disks, disk)
			}
		}
		if len(disks) > 0 {
			hasSysData = true
			sysCtx.Disks = disks
		}
	}

	if hasSysData {
		result.System = sysCtx
	}

	return result, nil
}

// CreateManualCluster creates a cluster with no auto_cluster_key and an
// explicit replication_type. If groupID is nil the default group is used.
func (d *Datastore) CreateManualCluster(ctx context.Context, name, description, replicationType string, groupID *int) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	resolvedGroupID := 0
	if groupID != nil {
		resolvedGroupID = *groupID
	} else {
		info, err := d.getDefaultGroupInternal(ctx)
		if err != nil {
			return 0, fmt.Errorf("failed to get default group: %w", err)
		}
		resolvedGroupID = info.ID
	}

	var clusterID int
	query := `
        INSERT INTO clusters (name, description, replication_type, group_id)
        VALUES ($1, $2, $3, $4)
        RETURNING id
    `
	err := d.pool.QueryRow(ctx, query, name, description, replicationType, resolvedGroupID).Scan(&clusterID)
	if err != nil {
		return 0, fmt.Errorf("failed to create manual cluster: %w", err)
	}

	return clusterID, nil
}

// GetConnectionClusterInfo returns the cluster-related information for a
// connection including the joined cluster details. Fields are nil when the
// connection has no cluster assignment.
func (d *Datastore) GetConnectionClusterInfo(ctx context.Context, connectionID int) (*ConnectionClusterInfo, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT c.cluster_id, c.role, c.membership_source,
               cl.name, cl.replication_type, cl.auto_cluster_key
        FROM connections c
        LEFT JOIN clusters cl ON c.cluster_id = cl.id
        WHERE c.id = $1
    `

	var info ConnectionClusterInfo
	err := d.pool.QueryRow(ctx, query, connectionID).Scan(
		&info.ClusterID, &info.Role, &info.MembershipSource,
		&info.ClusterName, &info.ReplicationType, &info.AutoClusterKey,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConnectionNotFound
		}
		return nil, fmt.Errorf("failed to get connection cluster info: %w", err)
	}

	return &info, nil
}

// ListClustersForAutocomplete returns all clusters ordered by name for use
// in autocomplete and selection UIs.
func (d *Datastore) ListClustersForAutocomplete(ctx context.Context) ([]ClusterSummary, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT id, name, replication_type, auto_cluster_key
        FROM clusters
        WHERE dismissed = FALSE
        ORDER BY name
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list clusters: %w", err)
	}
	defer rows.Close()

	clusters := make([]ClusterSummary, 0)
	for rows.Next() {
		var cs ClusterSummary
		if err := rows.Scan(&cs.ID, &cs.Name, &cs.ReplicationType, &cs.AutoClusterKey); err != nil {
			return nil, fmt.Errorf("failed to scan cluster: %w", err)
		}
		clusters = append(clusters, cs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating clusters: %w", err)
	}

	return clusters, nil
}

// ResetMembershipSource sets membership_source to 'auto' on a connection
// so that auto-detection resumes managing its cluster assignment.
func (d *Datastore) ResetMembershipSource(ctx context.Context, connectionID int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        UPDATE connections
        SET membership_source = 'auto', updated_at = CURRENT_TIMESTAMP
        WHERE id = $1
    `

	result, err := d.pool.Exec(ctx, query, connectionID)
	if err != nil {
		return fmt.Errorf("failed to reset membership source: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrConnectionNotFound
	}

	return nil
}

// GetClusterRelationships returns all node relationships for a cluster
// with source and target connection names joined from the connections table.
func (d *Datastore) GetClusterRelationships(ctx context.Context, clusterID int) ([]NodeRelationship, error) {
	query := `
        SELECT r.id, r.cluster_id,
               r.source_connection_id, r.target_connection_id,
               sc.name AS source_name, tc.name AS target_name,
               r.relationship_type, r.is_auto_detected
        FROM cluster_node_relationships r
        JOIN connections sc ON sc.id = r.source_connection_id
        JOIN connections tc ON tc.id = r.target_connection_id
        WHERE r.cluster_id = $1
        ORDER BY r.source_connection_id, r.target_connection_id
    `

	rows, err := d.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster relationships: %w", err)
	}
	defer rows.Close()

	var relationships []NodeRelationship
	for rows.Next() {
		var rel NodeRelationship
		if err := rows.Scan(
			&rel.ID, &rel.ClusterID,
			&rel.SourceConnectionID, &rel.TargetConnectionID,
			&rel.SourceName, &rel.TargetName,
			&rel.RelationshipType, &rel.IsAutoDetected,
		); err != nil {
			return nil, fmt.Errorf("failed to scan relationship: %w", err)
		}
		relationships = append(relationships, rel)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating relationships: %w", err)
	}

	return relationships, nil
}

// SetNodeRelationships replaces manual (non-auto-detected) relationships
// for a given source node in a cluster. This runs in a transaction:
// first deleting existing manual rows for the source, then inserting
// the new ones with is_auto_detected = FALSE.
func (d *Datastore) SetNodeRelationships(ctx context.Context, clusterID int, sourceConnectionID int, relationships []RelationshipInput) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Rollback is no-op if already committed

	// Delete existing manual relationships for this source in this cluster
	_, err = tx.Exec(ctx,
		`DELETE FROM cluster_node_relationships
         WHERE cluster_id = $1 AND source_connection_id = $2 AND is_auto_detected = FALSE`,
		clusterID, sourceConnectionID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete existing manual relationships: %w", err)
	}

	// Insert new relationships
	for _, rel := range relationships {
		_, err = tx.Exec(ctx,
			`INSERT INTO cluster_node_relationships
             (cluster_id, source_connection_id, target_connection_id, relationship_type, is_auto_detected)
             VALUES ($1, $2, $3, $4, FALSE)
             ON CONFLICT (cluster_id, source_connection_id, target_connection_id, relationship_type) DO NOTHING`,
			clusterID, sourceConnectionID, rel.TargetConnectionID, rel.RelationshipType,
		)
		if err != nil {
			return fmt.Errorf("failed to insert relationship: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// SyncAutoDetectedRelationships inserts auto-detected relationships for a
// cluster. For each detected relationship it performs an INSERT ... ON
// CONFLICT DO NOTHING so that existing rows are preserved. This method
// NEVER removes existing rows; auto-detection only adds.
func (d *Datastore) SyncAutoDetectedRelationships(ctx context.Context, clusterID int, detected []AutoRelationshipInput) error {
	for _, rel := range detected {
		_, err := d.pool.Exec(ctx,
			`INSERT INTO cluster_node_relationships
             (cluster_id, source_connection_id, target_connection_id, relationship_type, is_auto_detected)
             VALUES ($1, $2, $3, $4, TRUE)
             ON CONFLICT (cluster_id, source_connection_id, target_connection_id, relationship_type)
             DO NOTHING`,
			clusterID, rel.SourceConnectionID, rel.TargetConnectionID, rel.RelationshipType,
		)
		if err != nil {
			return fmt.Errorf("failed to sync auto-detected relationship: %w", err)
		}
	}

	return nil
}

// RemoveNodeRelationship deletes a single relationship by its ID.
func (d *Datastore) RemoveNodeRelationship(ctx context.Context, relationshipID int) error {
	result, err := d.pool.Exec(ctx,
		`DELETE FROM cluster_node_relationships WHERE id = $1`,
		relationshipID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete relationship: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("relationship not found")
	}

	return nil
}

// ClearNodeRelationships deletes all manual (is_auto_detected = FALSE)
// relationships for a source node in a cluster.
func (d *Datastore) ClearNodeRelationships(ctx context.Context, clusterID int, sourceConnectionID int) error {
	_, err := d.pool.Exec(ctx,
		`DELETE FROM cluster_node_relationships
         WHERE cluster_id = $1 AND source_connection_id = $2 AND is_auto_detected = FALSE`,
		clusterID, sourceConnectionID,
	)
	if err != nil {
		return fmt.Errorf("failed to clear manual relationships: %w", err)
	}

	return nil
}

// AddServerToCluster assigns a connection to a cluster with manual
// membership source. The caller provides the cluster ID, connection ID,
// and an optional role for the connection within the cluster.
func (d *Datastore) AddServerToCluster(ctx context.Context, clusterID int, connectionID int, role *string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Verify the cluster exists and is not dismissed
	var clusterExists bool
	err := d.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM clusters WHERE id = $1 AND dismissed = FALSE)`,
		clusterID,
	).Scan(&clusterExists)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}
	if !clusterExists {
		return ErrClusterNotFound
	}

	// Verify the connection exists
	var connExists bool
	err = d.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM connections WHERE id = $1)`,
		connectionID,
	).Scan(&connExists)
	if err != nil {
		return fmt.Errorf("failed to check connection existence: %w", err)
	}
	if !connExists {
		return ErrConnectionNotFound
	}

	query := `
        UPDATE connections
        SET cluster_id = $2, role = $3, membership_source = 'manual',
            updated_at = CURRENT_TIMESTAMP
        WHERE id = $1
    `

	_, err = d.pool.Exec(ctx, query, connectionID, clusterID, role)
	if err != nil {
		return fmt.Errorf("failed to add server to cluster: %w", err)
	}

	return nil
}

// RemoveServerFromCluster clears the cluster assignment for a connection
// and deletes all relationships in cluster_node_relationships where the
// connection is a source or target within the cluster.
func (d *Datastore) RemoveServerFromCluster(ctx context.Context, clusterID int, connectionID int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Verify the connection belongs to this cluster
	var belongs bool
	err := d.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM connections WHERE id = $1 AND cluster_id = $2)`,
		connectionID, clusterID,
	).Scan(&belongs)
	if err != nil {
		return fmt.Errorf("failed to check connection cluster membership: %w", err)
	}
	if !belongs {
		return ErrConnectionNotFound
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Rollback is no-op if already committed

	// Delete all relationships where this connection is source or target
	_, err = tx.Exec(ctx,
		`DELETE FROM cluster_node_relationships
         WHERE cluster_id = $1
           AND (source_connection_id = $2 OR target_connection_id = $2)`,
		clusterID, connectionID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete relationships: %w", err)
	}

	// Clear cluster assignment and reset membership source
	_, err = tx.Exec(ctx,
		`UPDATE connections
         SET cluster_id = NULL, role = NULL, membership_source = 'auto',
             updated_at = CURRENT_TIMESTAMP
         WHERE id = $1`,
		connectionID,
	)
	if err != nil {
		return fmt.Errorf("failed to clear cluster assignment: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// IsConnectionInCluster returns true if the given connection belongs to the
// specified cluster (i.e. connections.cluster_id matches).
func (d *Datastore) IsConnectionInCluster(ctx context.Context, clusterID int, connectionID int) (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var exists bool
	err := d.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM connections WHERE id = $1 AND cluster_id = $2)`,
		connectionID, clusterID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check connection cluster membership: %w", err)
	}

	return exists, nil
}
