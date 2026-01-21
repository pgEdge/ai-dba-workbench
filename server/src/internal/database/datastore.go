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

// ClusterGroup represents a group of clusters
type ClusterGroup struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Cluster represents a database cluster containing servers
type Cluster struct {
	ID          int       `json:"id"`
	GroupID     int       `json:"group_id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
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

// GetClusterGroup returns a single cluster group by ID
func (d *Datastore) GetClusterGroup(ctx context.Context, id int) (*ClusterGroup, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT id, name, description, created_at, updated_at
        FROM cluster_groups
        WHERE id = $1
    `

	var g ClusterGroup
	err := d.pool.QueryRow(ctx, query, id).Scan(
		&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster group: %w", err)
	}

	return &g, nil
}

// CreateClusterGroup creates a new cluster group
func (d *Datastore) CreateClusterGroup(ctx context.Context, name string, description *string) (*ClusterGroup, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        INSERT INTO cluster_groups (name, description)
        VALUES ($1, $2)
        RETURNING id, name, description, created_at, updated_at
    `

	var g ClusterGroup
	err := d.pool.QueryRow(ctx, query, name, description).Scan(
		&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.UpdatedAt,
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
        RETURNING id, name, description, created_at, updated_at
    `

	var g ClusterGroup
	err := d.pool.QueryRow(ctx, query, id, name, description).Scan(
		&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.UpdatedAt,
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
		return fmt.Errorf("cluster group not found")
	}

	return nil
}

// GetClustersInGroup returns all clusters in a group
func (d *Datastore) GetClustersInGroup(ctx context.Context, groupID int) ([]Cluster, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT id, group_id, name, description, created_at, updated_at
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
		if err := rows.Scan(&c.ID, &c.GroupID, &c.Name, &c.Description, &c.CreatedAt, &c.UpdatedAt); err != nil {
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
        SELECT id, group_id, name, description, created_at, updated_at
        FROM clusters
        WHERE id = $1
    `

	var c Cluster
	err := d.pool.QueryRow(ctx, query, id).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.CreatedAt, &c.UpdatedAt,
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
        RETURNING id, group_id, name, description, created_at, updated_at
    `

	var c Cluster
	err := d.pool.QueryRow(ctx, query, groupID, name, description).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.CreatedAt, &c.UpdatedAt,
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
        RETURNING id, group_id, name, description, created_at, updated_at
    `

	var c Cluster
	err := d.pool.QueryRow(ctx, query, id, groupID, name, description).Scan(
		&c.ID, &c.GroupID, &c.Name, &c.Description, &c.CreatedAt, &c.UpdatedAt,
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
		return fmt.Errorf("cluster not found")
	}

	return nil
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
	for _, group := range groups {
		groupWithClusters := ClusterGroupWithClusters{
			ID:       fmt.Sprintf("group-%d", group.ID),
			Name:     group.Name,
			Clusters: []ClusterWithServers{},
		}

		// Get clusters for this group
		clusters, err := d.getClustersInGroupInternal(ctx, group.ID)
		if err != nil {
			return nil, err
		}

		for _, cluster := range clusters {
			clusterWithServers := ClusterWithServers{
				ID:      fmt.Sprintf("cluster-%d", cluster.ID),
				Name:    cluster.Name,
				Servers: []ServerInfo{},
			}

			// Get servers for this cluster
			servers, err := d.getServersInClusterInternal(ctx, cluster.ID)
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
        SELECT id, group_id, name, description, created_at, updated_at
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
		if err := rows.Scan(&c.ID, &c.GroupID, &c.Name, &c.Description, &c.CreatedAt, &c.UpdatedAt); err != nil {
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
		return fmt.Errorf("connection not found")
	}

	return nil
}

// TopologyServerInfo extends ServerInfo with topology and child servers
type TopologyServerInfo struct {
	ID           int                  `json:"id"`
	Name         string               `json:"name"`
	Host         string               `json:"host"`
	Port         int                  `json:"port"`
	Status       string               `json:"status"`
	Role         string               `json:"role,omitempty"`
	PrimaryRole  string               `json:"primary_role"`
	IsExpandable bool                 `json:"is_expandable"`
	Children     []TopologyServerInfo `json:"children,omitempty"`
}

// TopologyCluster represents a replication-aware cluster
type TopologyCluster struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	ClusterType string               `json:"cluster_type"` // spock, binary_replication, standalone
	Servers     []TopologyServerInfo `json:"servers"`
}

// TopologyGroup represents a group with topology-aware clusters
type TopologyGroup struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Clusters []TopologyCluster `json:"clusters"`
}

// connectionWithRole holds connection data with role information from metrics
type connectionWithRole struct {
	ID                 int
	Name               string
	Host               string
	Port               int
	PrimaryRole        string
	UpstreamHost       sql.NullString
	UpstreamPort       sql.NullInt32
	PublisherHost      sql.NullString // For logical subscribers: publisher's host
	PublisherPort      sql.NullInt32  // For logical subscribers: publisher's port
	HasSpock           bool
	SpockNodeName      sql.NullString
	BinaryStandbyCount int
	IsStreamingStandby bool
	Status             string
}

// GetClusterTopology returns the auto-detected replication topology
func (d *Datastore) GetClusterTopology(ctx context.Context) ([]TopologyGroup, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Query all connections with their latest node role data
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
        )
        SELECT c.id, c.name, c.host, c.port,
               COALESCE(lr.primary_role, 'unknown') as primary_role,
               lr.upstream_host, lr.upstream_port,
               COALESCE(lr.has_spock, false) as has_spock,
               lr.spock_node_name,
               COALESCE(lr.binary_standby_count, 0) as binary_standby_count,
               COALESCE(lr.is_streaming_standby, false) as is_streaming_standby,
               lr.publisher_host, lr.publisher_port,
               COALESCE(lr.status, 'unknown') as status
        FROM connections c
        LEFT JOIN latest_roles lr ON c.id = lr.connection_id
        ORDER BY c.name
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections with roles: %w", err)
	}
	defer rows.Close()

	// Collect all connections
	var connections []connectionWithRole
	for rows.Next() {
		var conn connectionWithRole
		if err := rows.Scan(
			&conn.ID, &conn.Name, &conn.Host, &conn.Port,
			&conn.PrimaryRole, &conn.UpstreamHost, &conn.UpstreamPort,
			&conn.HasSpock, &conn.SpockNodeName,
			&conn.BinaryStandbyCount, &conn.IsStreamingStandby,
			&conn.PublisherHost, &conn.PublisherPort,
			&conn.Status,
		); err != nil {
			return nil, fmt.Errorf("failed to scan connection: %w", err)
		}
		connections = append(connections, conn)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating connections: %w", err)
	}

	return d.buildTopologyHierarchy(connections), nil
}

// buildTopologyHierarchy builds the topology hierarchy from connections
func (d *Datastore) buildTopologyHierarchy(connections []connectionWithRole) []TopologyGroup {
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
	spockClusters := d.groupSpockNodesByClusters(spockNodes, childrenMap, connByID, assignedConnections)

	// Build clusters list
	var clusters []TopologyCluster
	clusters = append(clusters, spockClusters...)

	// 2. Create entries for non-Spock primaries with standbys (binary replication)
	// These don't get a cluster wrapper - just the primary server with children
	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
			continue
		}
		// Check if this is a primary with standbys (and not a Spock node)
		if !conn.HasSpock && (conn.PrimaryRole == "binary_primary" && len(childrenMap[conn.ID]) > 0) {
			// Build server with children, use cluster_type "server" to indicate no cluster wrapper
			server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections)
			cluster := TopologyCluster{
				ID:          fmt.Sprintf("server-%d", conn.ID),
				Name:        conn.Name,
				ClusterType: "server", // UI will not show cluster header for this type
				Servers:     []TopologyServerInfo{server},
			}
			clusters = append(clusters, cluster)
		}
	}

	// 2.5. Group logical replication publishers with their subscribers
	// Match subscribers to publishers based on publisher_host:publisher_port
	logicalClusters := d.groupLogicalReplicationByPublisher(connections, connByID, connByHostPort, connByNamePort, assignedConnections)
	clusters = append(clusters, logicalClusters...)

	// 3. Handle standalone servers and servers without children
	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
			continue
		}

		// Build server (with any children if applicable)
		server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections)
		standaloneCluster := TopologyCluster{
			ID:          fmt.Sprintf("server-%d", conn.ID),
			Name:        conn.Name,
			ClusterType: "server", // UI will not show cluster header for this type
			Servers:     []TopologyServerInfo{server},
		}
		clusters = append(clusters, standaloneCluster)
	}

	// Create the topology group
	group := TopologyGroup{
		ID:       "group-auto",
		Name:     "Servers/Clusters",
		Clusters: clusters,
	}

	return []TopologyGroup{group}
}

// groupSpockNodesByClusters groups Spock nodes into clusters based on naming patterns
func (d *Datastore) groupSpockNodesByClusters(
	spockNodes []*connectionWithRole,
	childrenMap map[int][]int,
	connByID map[int]*connectionWithRole,
	assignedConnections map[int]bool,
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

		cluster := TopologyCluster{
			ID:          fmt.Sprintf("cluster-spock-%s", prefix),
			Name:        clusterName,
			ClusterType: clusterType,
			Servers:     make([]TopologyServerInfo, 0, len(nodes)),
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
		pubServer := TopologyServerInfo{
			ID:           publisher.ID,
			Name:         publisher.Name,
			Host:         publisher.Host,
			Port:         publisher.Port,
			Status:       publisher.Status,
			Role:         d.mapPrimaryRoleToDisplayRole(publisher.PrimaryRole),
			PrimaryRole:  publisher.PrimaryRole,
			IsExpandable: true,
			Children:     make([]TopologyServerInfo, 0, len(subscribers)),
		}

		// Add subscribers as children
		for _, sub := range subscribers {
			assignedConnections[sub.ID] = true
			subServer := TopologyServerInfo{
				ID:           sub.ID,
				Name:         sub.Name,
				Host:         sub.Host,
				Port:         sub.Port,
				Status:       sub.Status,
				Role:         d.mapPrimaryRoleToDisplayRole(sub.PrimaryRole),
				PrimaryRole:  sub.PrimaryRole,
				IsExpandable: false,
				Children:     nil,
			}
			pubServer.Children = append(pubServer.Children, subServer)
		}

		// Use cluster_type "server" so UI doesn't show cluster header
		cluster := TopologyCluster{
			ID:          fmt.Sprintf("server-%d", publisher.ID),
			Name:        publisher.Name,
			ClusterType: "server",
			Servers:     []TopologyServerInfo{pubServer},
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

	server := TopologyServerInfo{
		ID:           conn.ID,
		Name:         conn.Name,
		Host:         conn.Host,
		Port:         conn.Port,
		Status:       conn.Status,
		Role:         d.mapPrimaryRoleToDisplayRole(conn.PrimaryRole),
		PrimaryRole:  conn.PrimaryRole,
		IsExpandable: len(childrenMap[conn.ID]) > 0,
		Children:     make([]TopologyServerInfo, 0),
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
