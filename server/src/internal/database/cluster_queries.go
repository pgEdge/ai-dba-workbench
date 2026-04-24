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
	"errors"
	"fmt"
	"strings"
	"time"
)

// Sentinel errors for cluster operations
var (
	// ErrClusterGroupNotFound is returned when a cluster group is not found
	ErrClusterGroupNotFound = errors.New("cluster group not found")

	// ErrClusterNotFound is returned when a cluster is not found
	ErrClusterNotFound = errors.New("cluster not found")
)

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

// clusterOverride holds custom name and description for auto-detected clusters
type clusterOverride struct {
	Name        string
	Description string
}

// defaultGroupInfo holds basic info about the default group
type defaultGroupInfo struct {
	ID   int
	Name string
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

// deriveClusterNameFromKey produces a human-readable name from an
// auto_cluster_key. The result is used when creating a dismissed
// placeholder row for a cluster that has no existing database record.
func deriveClusterNameFromKey(autoKey string) string {
	parts := strings.SplitN(autoKey, ":", 2)
	if len(parts) != 2 {
		return autoKey
	}
	prefix, suffix := parts[0], parts[1]
	switch prefix {
	case "spock":
		return suffix + " Spock"
	case "binary":
		return "binary-" + suffix
	case "standalone":
		return "standalone-" + suffix
	case "logical":
		return "logical-" + suffix
	default:
		return autoKey
	}
}

// DeleteAutoDetectedCluster soft-deletes an auto-detected cluster by
// its auto_cluster_key. If no database record exists for the key, a
// dismissed placeholder is created so the topology builder skips the
// cluster on subsequent refreshes. All operations run in a transaction
// to prevent partial state if connection detach fails after dismiss.
func (d *Datastore) DeleteAutoDetectedCluster(ctx context.Context, autoKey string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin cluster dismiss transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Rollback is no-op if already committed

	var clusterID int
	err = tx.QueryRow(ctx,
		`SELECT id FROM clusters WHERE auto_cluster_key = $1`,
		autoKey,
	).Scan(&clusterID)

	if err != nil {
		// No existing record — create a dismissed placeholder
		name := deriveClusterNameFromKey(autoKey)
		_, insertErr := tx.Exec(ctx, `
            INSERT INTO clusters (name, auto_cluster_key, dismissed)
            VALUES ($1, $2, TRUE)
        `, name, autoKey)
		if insertErr != nil {
			return fmt.Errorf("failed to create dismissed cluster record: %w", insertErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit cluster dismiss transaction: %w", err)
		}
		return nil
	}

	// Existing record — dismiss it and detach connections
	_, err = tx.Exec(ctx,
		`UPDATE clusters SET dismissed = TRUE, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
		clusterID,
	)
	if err != nil {
		return fmt.Errorf("failed to dismiss cluster: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE connections SET cluster_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE cluster_id = $1`,
		clusterID,
	)
	if err != nil {
		return fmt.Errorf("failed to detach connections from dismissed cluster: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit cluster dismiss transaction: %w", err)
	}
	return nil
}

// DismissAutoDetectedClusterKeys soft-deletes clusters matching any of
// the provided auto_cluster_keys. For keys with existing database records,
// it sets dismissed=TRUE and detaches connections. For keys without records,
// it creates dismissed placeholder rows so the topology builder skips them.
// All operations run in a single transaction for atomicity.
func (d *Datastore) DismissAutoDetectedClusterKeys(ctx context.Context, autoKeys []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin dismiss transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Rollback is no-op if already committed

	for _, autoKey := range autoKeys {
		var clusterID int
		err := tx.QueryRow(ctx,
			`SELECT id FROM clusters WHERE auto_cluster_key = $1`,
			autoKey,
		).Scan(&clusterID)

		if err != nil {
			// No existing record — create a dismissed placeholder so the
			// topology builder skips this key on subsequent refreshes.
			name := deriveClusterNameFromKey(autoKey)
			_, insertErr := tx.Exec(ctx, `
                INSERT INTO clusters (name, auto_cluster_key, dismissed)
                VALUES ($1, $2, TRUE)
                ON CONFLICT (auto_cluster_key) DO UPDATE SET dismissed = TRUE, updated_at = CURRENT_TIMESTAMP
            `, name, autoKey)
			if insertErr != nil {
				return fmt.Errorf("failed to create dismissed placeholder for %s: %w", autoKey, insertErr)
			}
			continue
		}

		// Existing record — dismiss it and detach connections
		_, err = tx.Exec(ctx,
			`UPDATE clusters SET dismissed = TRUE, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
			clusterID,
		)
		if err != nil {
			return fmt.Errorf("failed to dismiss cluster %d: %w", clusterID, err)
		}

		_, err = tx.Exec(ctx,
			`UPDATE connections SET cluster_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE cluster_id = $1`,
			clusterID,
		)
		if err != nil {
			return fmt.Errorf("failed to detach connections from cluster %d: %w", clusterID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit dismiss transaction: %w", err)
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
	query := fmt.Sprintf(`
        SELECT
            c.id,
            c.name,
            c.host,
            c.port,
            COALESCE(c.role, 'primary') as role,
            %s as status,
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
    `, serverStatusCaseSQL("m.collected_at", "m.collected_at IS NULL"))

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
	query := fmt.Sprintf(`
        SELECT
            c.id,
            c.name,
            c.host,
            c.port,
            c.role,
            c.database_name,
            %s as status,
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
    `, serverStatusCaseSQL("m.collected_at", "m.collected_at IS NULL"))

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
