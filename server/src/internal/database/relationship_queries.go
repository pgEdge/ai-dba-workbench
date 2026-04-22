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
)

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
