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
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

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
        RETURNING id, name, host, hostaddr, port, database_name, username,
                  password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
                  owner_username, owner_token, is_monitored, is_shared
    `

	var conn MonitoredConnection
	err := d.pool.QueryRow(ctx, query, id, name).Scan(
		&conn.ID, &conn.Name, &conn.Host, &conn.HostAddr, &conn.Port,
		&conn.DatabaseName, &conn.Username, &conn.PasswordEncrypted,
		&conn.SSLMode, &conn.SSLCert, &conn.SSLKey, &conn.SSLRootCert,
		&conn.OwnerUsername, &conn.OwnerToken, &conn.IsMonitored, &conn.IsShared,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update connection: %w", err)
	}

	return &conn, nil
}

// ConnectionCreateParams contains parameters for creating a new connection
type ConnectionCreateParams struct {
	Name          string
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
            name, host, hostaddr, port, database_name, username,
            password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
            owner_username, is_shared, is_monitored
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
        RETURNING id, name, host, hostaddr, port, database_name, username,
                  password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
                  owner_username, owner_token, is_monitored, is_shared
    `

	var conn MonitoredConnection
	err := d.pool.QueryRow(ctx, query,
		params.Name, params.Host, params.HostAddr, params.Port, params.DatabaseName,
		params.Username, encryptedPassword, params.SSLMode, params.SSLCert,
		params.SSLKey, params.SSLRootCert, params.OwnerUsername, params.IsShared,
		params.IsMonitored,
	).Scan(
		&conn.ID, &conn.Name, &conn.Host, &conn.HostAddr, &conn.Port,
		&conn.DatabaseName, &conn.Username, &conn.PasswordEncrypted,
		&conn.SSLMode, &conn.SSLCert, &conn.SSLKey, &conn.SSLRootCert,
		&conn.OwnerUsername, &conn.OwnerToken, &conn.IsMonitored, &conn.IsShared,
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
	args := []interface{}{}
	argNum := 1

	if params.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, *params.Name)
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
        RETURNING id, name, host, hostaddr, port, database_name, username,
                  password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
                  owner_username, owner_token, is_monitored, is_shared
    `, strings.Join(setClauses, ", "), argNum)

	var conn MonitoredConnection
	err := d.pool.QueryRow(ctx, query, args...).Scan(
		&conn.ID, &conn.Name, &conn.Host, &conn.HostAddr, &conn.Port,
		&conn.DatabaseName, &conn.Username, &conn.PasswordEncrypted,
		&conn.SSLMode, &conn.SSLCert, &conn.SSLKey, &conn.SSLRootCert,
		&conn.OwnerUsername, &conn.OwnerToken, &conn.IsMonitored, &conn.IsShared,
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
	ID             int            `json:"id"`
	GroupID        sql.NullInt32  `json:"group_id,omitempty"`
	Name           string         `json:"name"`
	Description    *string        `json:"description,omitempty"`
	AutoClusterKey sql.NullString `json:"auto_cluster_key,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// ServerInfo represents a server in the cluster hierarchy with status
type ServerInfo struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	Host     string  `json:"host"`
	Port     int     `json:"port"`
	Status   string  `json:"status"`
	Role     *string `json:"role,omitempty"`
	Database string  `json:"database_name,omitempty"`
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
        SELECT id, group_id, name, description, auto_cluster_key, created_at, updated_at
        FROM clusters
        WHERE group_id = $1
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
		if err := rows.Scan(&c.ID, &c.GroupID, &c.Name, &c.Description, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan cluster: %w", err)
		}
		clusters = append(clusters, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating clusters: %w", err)
	}

	return clusters, nil
}

// GetCluster returns a single cluster by ID
func (d *Datastore) GetCluster(ctx context.Context, id int) (*Cluster, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT id, group_id, name, description, auto_cluster_key, created_at, updated_at
        FROM clusters
        WHERE id = $1
    `

	var c Cluster
	err := d.pool.QueryRow(ctx, query, id).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
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
        RETURNING id, group_id, name, description, auto_cluster_key, created_at, updated_at
    `

	var c Cluster
	err := d.pool.QueryRow(ctx, query, groupID, name, description).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
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
        RETURNING id, group_id, name, description, auto_cluster_key, created_at, updated_at
    `

	var c Cluster
	err := d.pool.QueryRow(ctx, query, id, groupID, name, description).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update cluster: %w", err)
	}

	return &c, nil
}

// UpdateClusterPartial updates only the provided fields of a cluster.
// Supports partial updates: name, group_id, description - any can be omitted.
func (d *Datastore) UpdateClusterPartial(ctx context.Context, id int, groupID *int, name string, description *string) (*Cluster, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Build dynamic update based on what's provided
	setClauses := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []interface{}{}
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

	// Add the cluster ID for the WHERE clause
	args = append(args, id)

	query := fmt.Sprintf(`
        UPDATE clusters
        SET %s
        WHERE id = $%d
        RETURNING id, group_id, name, description, auto_cluster_key, created_at, updated_at
    `, strings.Join(setClauses, ", "), argNum)

	var c Cluster
	err := d.pool.QueryRow(ctx, query, args...).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update cluster: %w", err)
	}

	return &c, nil
}

// DeleteCluster deletes a cluster by ID
func (d *Datastore) DeleteCluster(ctx context.Context, id int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `DELETE FROM clusters WHERE id = $1`

	result, err := d.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete cluster: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrClusterNotFound
	}

	return nil
}

// GetClusterOverrides returns a map of auto_cluster_key -> custom name
// for all clusters that have an auto_cluster_key set. This is used to
// apply custom names to auto-detected clusters in the topology view.
func (d *Datastore) GetClusterOverrides(ctx context.Context) (map[string]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.getClusterOverridesInternal(ctx)
}

// getClusterOverridesInternal is the lock-free internal implementation
func (d *Datastore) getClusterOverridesInternal(ctx context.Context) (map[string]string, error) {
	query := `
        SELECT auto_cluster_key, name
        FROM clusters
        WHERE auto_cluster_key IS NOT NULL
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster overrides: %w", err)
	}
	defer rows.Close()

	overrides := make(map[string]string)
	for rows.Next() {
		var key, name string
		if err := rows.Scan(&key, &name); err != nil {
			return nil, fmt.Errorf("failed to scan cluster override: %w", err)
		}
		overrides[key] = name
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

	// Use INSERT ... ON CONFLICT to upsert
	query := `
        INSERT INTO clusters (name, auto_cluster_key)
        VALUES ($1, $2)
        ON CONFLICT (auto_cluster_key)
        DO UPDATE SET name = EXCLUDED.name, updated_at = CURRENT_TIMESTAMP
        RETURNING id, group_id, name, description, auto_cluster_key, created_at, updated_at
    `

	var c Cluster
	err := d.pool.QueryRow(ctx, query, name, autoKey).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert cluster by auto key: %w", err)
	}

	return &c, nil
}

// UpsertAutoDetectedCluster creates or updates an auto-detected cluster.
// Supports renaming (name), moving to a different group (groupID), or both.
// At least one of name or groupID must be provided.
func (d *Datastore) UpsertAutoDetectedCluster(ctx context.Context, autoKey string, name string, groupID *int) (*Cluster, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if cluster already exists
	var existingCluster Cluster
	checkQuery := `
        SELECT id, group_id, name, description, auto_cluster_key, created_at, updated_at
        FROM clusters
        WHERE auto_cluster_key = $1
    `
	err := d.pool.QueryRow(ctx, checkQuery, autoKey).Scan(
		&existingCluster.ID, &existingCluster.GroupID, &existingCluster.Name,
		&existingCluster.Description, &existingCluster.AutoClusterKey,
		&existingCluster.CreatedAt, &existingCluster.UpdatedAt,
	)

	if err != nil {
		// Cluster doesn't exist, create new one
		// For new clusters, name is required
		if name == "" {
			return nil, fmt.Errorf("name is required when creating a new cluster")
		}

		insertQuery := `
            INSERT INTO clusters (name, auto_cluster_key, group_id)
            VALUES ($1, $2, $3)
            RETURNING id, group_id, name, description, auto_cluster_key, created_at, updated_at
        `
		var c Cluster
		err := d.pool.QueryRow(ctx, insertQuery, name, autoKey, groupID).Scan(
			&c.ID, &c.GroupID, &c.Name, &c.Description, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create cluster: %w", err)
		}
		return &c, nil
	}

	// Cluster exists, update it
	// Build dynamic update based on what's provided
	setClauses := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []interface{}{}
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

	// Add the auto_cluster_key for the WHERE clause
	args = append(args, autoKey)

	updateQuery := fmt.Sprintf(`
        UPDATE clusters
        SET %s
        WHERE auto_cluster_key = $%d
        RETURNING id, group_id, name, description, auto_cluster_key, created_at, updated_at
    `, strings.Join(setClauses, ", "), argNum)

	var c Cluster
	err = d.pool.QueryRow(ctx, updateQuery, args...).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt,
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
		if err := rows.Scan(&s.ID, &s.Name, &s.Host, &s.Port, &role, &s.Database, &lastCollected); err != nil {
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
            COALESCE(
                CASE
                    WHEN m.collected_at > NOW() - INTERVAL '2 minutes' THEN 'online'
                    WHEN m.collected_at > NOW() - INTERVAL '5 minutes' THEN 'warning'
                    ELSE 'offline'
                END,
                'unknown'
            ) as status
        FROM connections c
        LEFT JOIN LATERAL (
            SELECT collected_at
            FROM metrics.pg_stat_database
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
		if err := rows.Scan(&s.ID, &s.Name, &s.Host, &s.Port, &s.Role, &s.Status); err != nil {
			return nil, fmt.Errorf("failed to scan server: %w", err)
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
        SELECT id, group_id, name, description, auto_cluster_key, created_at, updated_at
        FROM clusters
        WHERE group_id = $1
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
		if err := rows.Scan(&c.ID, &c.GroupID, &c.Name, &c.Description, &c.AutoClusterKey, &c.CreatedAt, &c.UpdatedAt); err != nil {
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
		if err := rows.Scan(&s.ID, &s.Name, &s.Host, &s.Port, &role, &s.Database, &lastCollected); err != nil {
			return nil, fmt.Errorf("failed to scan server: %w", err)
		}

		if role.Valid {
			s.Role = &role.String
		}

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

// AssignConnectionToCluster assigns a connection to a cluster with a role
func (d *Datastore) AssignConnectionToCluster(ctx context.Context, connectionID int, clusterID *int, role *string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        UPDATE connections
        SET cluster_id = $2, role = $3, updated_at = CURRENT_TIMESTAMP
        WHERE id = $1
    `

	result, err := d.pool.Exec(ctx, query, connectionID, clusterID, role)
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
	ID              int                  `json:"id"`
	Name            string               `json:"name"`
	Host            string               `json:"host"`
	Port            int                  `json:"port"`
	Status          string               `json:"status"`
	ConnectionError string               `json:"connection_error,omitempty"`
	Role            string               `json:"role,omitempty"`
	PrimaryRole     string               `json:"primary_role"`
	IsExpandable    bool                 `json:"is_expandable"`
	OwnerUsername   string               `json:"owner_username,omitempty"`
	Version         string               `json:"version,omitempty"`
	OS              string               `json:"os,omitempty"`
	SpockNodeName   string               `json:"spock_node_name,omitempty"`
	SpockVersion    string               `json:"spock_version,omitempty"`
	DatabaseName    string               `json:"database_name,omitempty"`
	Username        string               `json:"username,omitempty"`
	Children        []TopologyServerInfo `json:"children,omitempty"`
}

// TopologyCluster represents a replication-aware cluster
type TopologyCluster struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	ClusterType    string               `json:"cluster_type"` // spock, spock_ha, binary, logical, server
	AutoClusterKey string               `json:"auto_cluster_key,omitempty"`
	Servers        []TopologyServerInfo `json:"servers"`
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
	Status             string
	ConnectionError    sql.NullString
}

// GetClusterTopology returns the combined topology including manually-created
// cluster groups and auto-detected replication topology
func (d *Datastore) GetClusterTopology(ctx context.Context) ([]TopologyGroup, error) {
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
		clusterOverrides = make(map[string]string)
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

	// Append the default group (contains auto-detected clusters)
	result = append(result, defaultGroups...)

	return result, nil
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
        WITH latest_roles AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, primary_role, upstream_host, upstream_port,
                has_spock, spock_node_name, binary_standby_count, is_streaming_standby,
                publisher_host, publisher_port,
                COALESCE(
                    CASE
                        WHEN collected_at > NOW() - INTERVAL '6 minutes' THEN 'online'
                        WHEN collected_at > NOW() - INTERVAL '12 minutes' THEN 'warning'
                        ELSE 'offline'
                    END, 'unknown'
                ) as status
            FROM metrics.pg_node_role
            WHERE collected_at > NOW() - INTERVAL '15 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_server_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, server_version
            FROM metrics.pg_server_info
            WHERE collected_at > NOW() - INTERVAL '15 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_os_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, name as os_name
            FROM metrics.pg_sys_os_info
            WHERE collected_at > NOW() - INTERVAL '15 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_spock_version AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, extversion as spock_version
            FROM metrics.pg_extension
            WHERE extname = 'spock'
            ORDER BY connection_id, collected_at DESC
        )
        SELECT c.id, c.name, c.host, c.port, c.owner_username,
               c.database_name, c.username,
               lsi.server_version,
               loi.os_name,
               COALESCE(lr.primary_role, 'unknown') as primary_role,
               lr.upstream_host, lr.upstream_port,
               COALESCE(lr.has_spock, false) as has_spock,
               lr.spock_node_name,
               lsv.spock_version,
               COALESCE(lr.binary_standby_count, 0) as binary_standby_count,
               COALESCE(lr.is_streaming_standby, false) as is_streaming_standby,
               lr.publisher_host, lr.publisher_port,
               CASE
                   WHEN c.is_monitored AND c.connection_error IS NOT NULL
                   THEN 'offline'
                   WHEN c.is_monitored AND lr.connection_id IS NULL
                   THEN 'initialising'
                   ELSE COALESCE(lr.status, 'unknown')
               END as status,
               c.connection_error
        FROM connections c
        LEFT JOIN latest_roles lr ON c.id = lr.connection_id
        LEFT JOIN latest_server_info lsi ON c.id = lsi.connection_id
        LEFT JOIN latest_os_info loi ON c.id = loi.connection_id
        LEFT JOIN latest_spock_version lsv ON c.id = lsv.connection_id
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
			&conn.ID, &conn.Name, &conn.Host, &conn.Port, &conn.OwnerUsername,
			&conn.DatabaseName, &conn.Username,
			&conn.Version,
			&conn.OS,
			&conn.PrimaryRole, &conn.UpstreamHost, &conn.UpstreamPort,
			&conn.HasSpock, &conn.SpockNodeName,
			&conn.SpockVersion,
			&conn.BinaryStandbyCount, &conn.IsStreamingStandby,
			&conn.PublisherHost, &conn.PublisherPort,
			&conn.Status,
			&conn.ConnectionError,
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
func (d *Datastore) buildAutoDetectedClusters(connections []connectionWithRole, clusterOverrides map[string]string) map[string]TopologyCluster {
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

	// Process Spock clusters
	spockNodes := make([]*connectionWithRole, 0)
	for i := range connections {
		conn := &connections[i]
		if conn.HasSpock && conn.PrimaryRole != "binary_standby" {
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
	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
			continue
		}
		if !conn.HasSpock && (conn.PrimaryRole == "binary_primary" && len(childrenMap[conn.ID]) > 0) {
			server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections)
			autoKey := fmt.Sprintf("binary:%d", conn.ID)
			clusterName := conn.Name
			if customName, ok := clusterOverrides[autoKey]; ok {
				clusterName = customName
			}
			cluster := TopologyCluster{
				ID:             fmt.Sprintf("server-%d", conn.ID),
				Name:           clusterName,
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
		// Build server info
		server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections)
		autoKey := fmt.Sprintf("standalone:%d", conn.ID)
		clusterName := conn.Name
		if customName, ok := clusterOverrides[autoKey]; ok {
			clusterName = customName
		}
		cluster := TopologyCluster{
			ID:             fmt.Sprintf("server-%d", conn.ID),
			Name:           clusterName,
			ClusterType:    "server",
			AutoClusterKey: autoKey,
			Servers:        []TopologyServerInfo{server},
		}
		result[autoKey] = cluster
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
					topologyCluster := TopologyCluster{
						ID:             fmt.Sprintf("cluster-%d", c.ID),
						Name:           c.Name, // Use the custom name from the cluster record
						ClusterType:    autoCluster.ClusterType,
						AutoClusterKey: c.AutoClusterKey.String,
						Servers:        autoCluster.Servers,
					}
					topologyGroup.Clusters = append(topologyGroup.Clusters, topologyCluster)
				}
				// If not found in autoDetectedClusters, the cluster may have been removed
				// or topology changed - skip it
				continue
			}

			// Regular manual cluster - get servers via cluster_id
			topologyCluster := TopologyCluster{
				ID:          fmt.Sprintf("cluster-%d", c.ID),
				Name:        c.Name,
				ClusterType: "manual",
				Servers:     []TopologyServerInfo{},
			}

			// Get servers in this cluster with their role data
			servers, err := d.getServersInClusterWithRolesInternal(ctx, c.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to get servers for cluster %d: %w", c.ID, err)
			}
			topologyCluster.Servers = servers

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
            WHERE collected_at > NOW() - INTERVAL '15 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_server_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, server_version
            FROM metrics.pg_server_info
            WHERE collected_at > NOW() - INTERVAL '15 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_os_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, name as os_name
            FROM metrics.pg_sys_os_info
            WHERE collected_at > NOW() - INTERVAL '15 minutes'
            ORDER BY connection_id, collected_at DESC
        )
        SELECT c.id, c.name, c.host, c.port, c.owner_username, c.role,
               c.database_name, c.username,
               lsi.server_version,
               loi.os_name,
               lr.spock_node_name,
               COALESCE(lr.primary_role, 'unknown') as primary_role,
               COALESCE(lr.status, 'unknown') as status
        FROM connections c
        LEFT JOIN latest_roles lr ON c.id = lr.connection_id
        LEFT JOIN latest_server_info lsi ON c.id = lsi.connection_id
        LEFT JOIN latest_os_info loi ON c.id = loi.connection_id
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
		var ownerUsername, role, version, osName, spockNodeName sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.Host, &s.Port, &ownerUsername, &role,
			&s.DatabaseName, &s.Username, &version, &osName, &spockNodeName,
			&s.PrimaryRole, &s.Status); err != nil {
			return nil, fmt.Errorf("failed to scan server: %w", err)
		}
		if ownerUsername.Valid {
			s.OwnerUsername = ownerUsername.String
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

// buildTopologyHierarchy builds the topology hierarchy from connections.
// The claimedKeys parameter contains auto_cluster_keys that have been moved to
// manual groups - these clusters will be excluded from the default group.
// The defaultGroup parameter provides the database-backed default group info.
func (d *Datastore) buildTopologyHierarchy(connections []connectionWithRole, clusterOverrides map[string]string, claimedKeys map[string]bool, defaultGroup *defaultGroupInfo) []TopologyGroup {
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

	// Identify Spock nodes (has_spock=true AND not a standby)
	// These are the actual Spock multi-master nodes
	spockNodes := make([]*connectionWithRole, 0)
	for i := range connections {
		conn := &connections[i]
		// A Spock node has Spock installed and is not a binary standby
		// (binary standbys with Spock are hot standbys of Spock nodes)
		if conn.HasSpock && conn.PrimaryRole != "binary_standby" {
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
	// These now get a cluster wrapper with type "binary" so UI shows cluster header
	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
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

			// Build server with children
			server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections)

			clusterName := conn.Name
			if customName, ok := clusterOverrides[autoKey]; ok {
				clusterName = customName
			}

			cluster := TopologyCluster{
				ID:             fmt.Sprintf("server-%d", conn.ID),
				Name:           clusterName,
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

		// Compute auto_cluster_key for standalone servers
		autoKey := fmt.Sprintf("standalone:%d", conn.ID)

		// Skip if this standalone has been moved to a manual group
		if claimedKeys[autoKey] {
			continue
		}

		// Build server (with any children if applicable)
		server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections)
		standaloneCluster := TopologyCluster{
			ID:             fmt.Sprintf("server-%d", conn.ID),
			Name:           conn.Name,
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
	overrides map[string]string,
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

		// Compute auto_cluster_key and check for custom name
		autoKey := fmt.Sprintf("spock:%s", prefix)
		if customName, ok := overrides[autoKey]; ok {
			clusterName = customName
		}

		cluster := TopologyCluster{
			ID:             fmt.Sprintf("cluster-spock-%s", prefix),
			Name:           clusterName,
			ClusterType:    clusterType,
			AutoClusterKey: autoKey,
			Servers:        make([]TopologyServerInfo, 0, len(nodes)),
		}

		for _, node := range nodes {
			// Use buildServerWithChildren to include hot standbys as children
			server := d.buildServerWithChildren(node, childrenMap, connByID, assignedConnections)
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
	overrides map[string]string,
) []TopologyCluster {
	// Build publisher->subscribers map by matching publisher_host:port to connections
	subscribersByPublisher := make(map[int][]*connectionWithRole) // publisher connection ID -> subscribers

	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
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

		if publisher != nil && !assignedConnections[publisher.ID] {
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
		pubServer := TopologyServerInfo{
			ID:            publisher.ID,
			Name:          publisher.Name,
			Host:          publisher.Host,
			Port:          publisher.Port,
			Status:        publisher.Status,
			Role:          d.mapPrimaryRoleToDisplayRole(publisher.PrimaryRole),
			PrimaryRole:   publisher.PrimaryRole,
			IsExpandable:  true,
			OwnerUsername: pubOwner,
			Version:       pubVersion,
			OS:            pubOS,
			SpockNodeName: pubSpockNodeName,
			SpockVersion:  pubSpockVersion,
			DatabaseName:  publisher.DatabaseName,
			Username:      publisher.Username,
			Children:      make([]TopologyServerInfo, 0, len(subscribers)),
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
			subServer := TopologyServerInfo{
				ID:            sub.ID,
				Name:          sub.Name,
				Host:          sub.Host,
				Port:          sub.Port,
				Status:        sub.Status,
				Role:          d.mapPrimaryRoleToDisplayRole(sub.PrimaryRole),
				PrimaryRole:   sub.PrimaryRole,
				IsExpandable:  false,
				OwnerUsername: subOwner,
				Version:       subVersion,
				OS:            subOS,
				SpockNodeName: subSpockNodeName,
				SpockVersion:  subSpockVersion,
				DatabaseName:  sub.DatabaseName,
				Username:      sub.Username,
				Children:      nil,
			}
			pubServer.Children = append(pubServer.Children, subServer)
		}

		// Compute auto_cluster_key and check for custom name
		autoKey := fmt.Sprintf("logical:%d", publisher.ID)
		clusterName := publisher.Name
		if customName, ok := overrides[autoKey]; ok {
			clusterName = customName
		}

		// Use cluster_type "logical" so UI shows cluster header for logical replication
		cluster := TopologyCluster{
			ID:             fmt.Sprintf("server-%d", publisher.ID),
			Name:           clusterName,
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

// buildServerWithChildren recursively builds server tree with standbys as children
func (d *Datastore) buildServerWithChildren(
	conn *connectionWithRole,
	childrenMap map[int][]int,
	connByID map[int]*connectionWithRole,
	assignedConnections map[int]bool,
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

	server := TopologyServerInfo{
		ID:            conn.ID,
		Name:          conn.Name,
		Host:          conn.Host,
		Port:          conn.Port,
		Status:        conn.Status,
		Role:          d.mapPrimaryRoleToDisplayRole(conn.PrimaryRole),
		PrimaryRole:   conn.PrimaryRole,
		IsExpandable:  len(childrenMap[conn.ID]) > 0,
		OwnerUsername: ownerUsername,
		Version:       version,
		OS:            os,
		SpockNodeName: spockNodeName,
		SpockVersion:  spockVersion,
		DatabaseName:  conn.DatabaseName,
		Username:      conn.Username,
		Children:      make([]TopologyServerInfo, 0),
	}

	if conn.ConnectionError.Valid {
		server.ConnectionError = conn.ConnectionError.String
	}

	// Recursively add children
	for _, childID := range childrenMap[conn.ID] {
		if child, exists := connByID[childID]; exists && !assignedConnections[childID] {
			childServer := d.buildServerWithChildren(child, childrenMap, connByID, assignedConnections)
			server.Children = append(server.Children, childServer)
		}
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
	case "bdr_node":
		return "bdr"
	case "bdr_standby":
		return "bdr_standby"
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
	AnomalyScore   *float64   `json:"anomaly_score,omitempty"`
	AnomalyDetails *string    `json:"anomaly_details,omitempty"`
	ServerName     string     `json:"server_name,omitempty"`
	// Acknowledgment fields (from alert_acknowledgments table)
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy *string    `json:"acknowledged_by,omitempty"`
	AckMessage     *string    `json:"ack_message,omitempty"`
	FalsePositive  *bool      `json:"false_positive,omitempty"`
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
	args := []interface{}{}
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
		       a.anomaly_score, a.anomaly_details,
		       COALESCE(c.name, 'Unknown') as server_name,
		       ack.acknowledged_at, ack.acknowledged_by, ack.message, ack.false_positive
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
			&alert.ClearedAt, &alert.AnomalyScore, &alert.AnomalyDetails,
			&alert.ServerName,
			&alert.AcknowledgedAt, &alert.AcknowledgedBy, &alert.AckMessage,
			&alert.FalsePositive,
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

// GetAlertCounts returns counts of active alerts grouped by connection_id
func (d *Datastore) GetAlertCounts(ctx context.Context) (*AlertCountsResult, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Get total count of active alerts
	var total int64
	err := d.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM alerts
		WHERE status = 'active'
	`).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("failed to count total alerts: %w", err)
	}

	// Get counts grouped by connection_id
	rows, err := d.pool.Query(ctx, `
		SELECT connection_id, COUNT(*) as count
		FROM alerts
		WHERE status = 'active'
		GROUP BY connection_id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query alert counts: %w", err)
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

	return &AlertCountsResult{
		Total:    total,
		ByServer: byServer,
	}, nil
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
// to active status
func (d *Datastore) UnacknowledgeAlert(ctx context.Context, alertID int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Update alert status back to active
	result, err := d.pool.Exec(ctx, `
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

	return nil
}
