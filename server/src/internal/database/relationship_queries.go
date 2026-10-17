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

	"github.com/jackc/pgx/v5"
	"github.com/pgedge/ai-workbench/pkg/rollback"
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
        WHERE c.id = $1 AND c.deleted_at IS NULL
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
        WHERE id = $1 AND deleted_at IS NULL
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
        JOIN connections sc ON sc.id = r.source_connection_id AND sc.deleted_at IS NULL
        JOIN connections tc ON tc.id = r.target_connection_id AND tc.deleted_at IS NULL
        WHERE r.cluster_id = $1
        ORDER BY r.source_connection_id, r.target_connection_id
    `

	rows, err := d.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster relationships: %w", err)
	}
	relationships, err := scanAll(rows, func(r pgx.Rows, rel *NodeRelationship) error {
		return r.Scan(
			&rel.ID, &rel.ClusterID,
			&rel.SourceConnectionID, &rel.TargetConnectionID,
			&rel.SourceName, &rel.TargetName,
			&rel.RelationshipType, &rel.IsAutoDetected,
		)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read cluster relationships: %w", err)
	}
	return relationships, nil
}

// ErrHiddenRelationship is returned by SetNodeRelationships and
// ClearNodeRelationships when the source has a manual relationship whose
// target is outside the permitted set; nothing is changed.
var ErrHiddenRelationship = errors.New("relationship to a connection outside the permitted set")

// deleteManualRelationships deletes the source's manual relationships in
// the cluster within tx. When permitted is non-nil and the target of any
// deleted row is not in it, it returns ErrHiddenRelationship and the
// caller must roll back. The targets come from the DELETE's own
// RETURNING list, so the check covers exactly the rows deleted,
// including one written concurrently after the caller's own checks.
func deleteManualRelationships(ctx context.Context, tx pgx.Tx, clusterID, sourceConnectionID int, permitted map[int]bool) error {
	rows, err := tx.Query(ctx,
		`DELETE FROM cluster_node_relationships
         WHERE cluster_id = $1 AND source_connection_id = $2 AND is_auto_detected = FALSE
         RETURNING target_connection_id`,
		clusterID, sourceConnectionID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete existing manual relationships: %w", err)
	}
	targets, err := scanAll(rows, func(r pgx.Rows, target *int) error {
		return r.Scan(target)
	})
	if err != nil {
		return fmt.Errorf("failed to delete existing manual relationships: %w", err)
	}
	if permitted == nil {
		return nil
	}
	for _, target := range targets {
		if !permitted[target] {
			return ErrHiddenRelationship
		}
	}
	return nil
}

// ErrSourceNotInCluster and ErrTargetNotInCluster are returned by
// SetNodeRelationships and AddNodeRelationship when the source, or a
// target, is not a member of the cluster once the write transaction has
// locked it; nothing is changed.
var (
	ErrSourceNotInCluster = errors.New("source connection does not belong to the cluster")
	ErrTargetNotInCluster = errors.New("target connection does not belong to the cluster")
)

// lockClusterMembers takes a FOR SHARE lock, within tx, on the
// connection rows of the source and every target that are members of
// the cluster, in id order, and returns ErrSourceNotInCluster or
// ErrTargetNotInCluster when one of them is not a member. The lock
// conflicts with the UPDATE that moves a connection to another cluster,
// so a move either commits first and is seen here, or waits for tx and
// then deletes the rows tx wrote (see dropRelationshipsOutsideCluster);
// a relationship row naming a connection outside its cluster cannot
// survive either order (issue #707).
func lockClusterMembers(ctx context.Context, tx pgx.Tx, clusterID, sourceID int, targetIDs []int) error {
	ids := append([]int{sourceID}, targetIDs...)
	var locked []int
	err := tx.QueryRow(ctx, `
        SELECT array_agg(id) FROM (
            SELECT id FROM connections
            WHERE id = ANY($1) AND cluster_id = $2
            ORDER BY id
            FOR SHARE
        ) AS members`,
		ids, clusterID,
	).Scan(&locked)
	if err != nil {
		return fmt.Errorf("failed to lock cluster members: %w", err)
	}
	members := make(map[int]bool, len(locked))
	for _, id := range locked {
		members[id] = true
	}
	if !members[sourceID] {
		return ErrSourceNotInCluster
	}
	for _, id := range targetIDs {
		if !members[id] {
			return ErrTargetNotInCluster
		}
	}
	return nil
}

// SetNodeRelationships replaces manual (non-auto-detected) relationships
// for a given source node in a cluster. This runs in a transaction:
// first locking the source and targets as cluster members (see
// lockClusterMembers), then deleting existing manual rows for the
// source, then inserting the new ones with is_auto_detected = FALSE. A
// nil permitted set allows any target; otherwise, when a deleted row
// targets a connection outside permitted, the transaction is rolled
// back and ErrHiddenRelationship is returned.
func (d *Datastore) SetNodeRelationships(ctx context.Context, clusterID int, sourceConnectionID int, relationships []RelationshipInput, permitted map[int]bool) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit

	targets := make([]int, 0, len(relationships))
	for _, rel := range relationships {
		targets = append(targets, rel.TargetConnectionID)
	}
	if err := lockClusterMembers(ctx, tx, clusterID, sourceConnectionID, targets); err != nil {
		return err
	}

	if err := deleteManualRelationships(ctx, tx, clusterID, sourceConnectionID, permitted); err != nil {
		return err
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

// AddNodeRelationship inserts one manual relationship without touching
// the source's other rows. A row that already exists is left as it is.
// Unlike SetNodeRelationships it never deletes, so writing the reverse
// half of a replicates_with pair cannot discard the target's other
// manual relationships. The source and target are locked as cluster
// members first, as in SetNodeRelationships.
func (d *Datastore) AddNodeRelationship(ctx context.Context, clusterID int, sourceConnectionID int, rel RelationshipInput) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit

	if err := lockClusterMembers(ctx, tx, clusterID, sourceConnectionID, []int{rel.TargetConnectionID}); err != nil {
		return err
	}

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

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// SyncAutoDetectedRelationships replaces the auto-detected relationship
// set for a cluster in a single transaction. All existing
// is_auto_detected = TRUE rows for the cluster are deleted, then the
// supplied detected slice is inserted with is_auto_detected = TRUE.
// Manual rows (is_auto_detected = FALSE) and auto-detected rows for
// other clusters are left untouched. The transaction rolls back on any
// failure so the prior auto-detected state survives. ON CONFLICT DO
// NOTHING guards against duplicate entries within the input slice.
//
// To prevent the DELETE-then-INSERT race that READ COMMITTED isolation
// would otherwise allow (two concurrent syncs for the same cluster
// each observing an empty initial state and committing the union of
// their disjoint inserts), the transaction first takes a row-level
// lock on the cluster row via SELECT ... FOR UPDATE. Concurrent syncs
// for the SAME cluster serialize on this lock; syncs for DIFFERENT
// clusters proceed in parallel because the lock is row-scoped.
//
// If the supplied clusterID does not exist in the clusters table, the
// function returns ErrClusterNotFound and makes no changes.
func (d *Datastore) SyncAutoDetectedRelationships(ctx context.Context, clusterID int, detected []AutoRelationshipInput) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit

	// Take a row-level lock on the cluster's row to serialize concurrent
	// syncs targeting the same cluster. Other transactions that take this
	// lock (or otherwise lock the row) block until this transaction
	// commits or rolls back. A missing cluster yields pgx.ErrNoRows.
	var lockedID int
	err = tx.QueryRow(ctx,
		`SELECT id FROM clusters WHERE id = $1 FOR UPDATE`,
		clusterID,
	).Scan(&lockedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrClusterNotFound
		}
		return fmt.Errorf("failed to lock cluster row: %w", err)
	}

	_, err = tx.Exec(ctx,
		`DELETE FROM cluster_node_relationships
         WHERE cluster_id = $1 AND is_auto_detected = TRUE`,
		clusterID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete existing auto-detected relationships: %w", err)
	}

	for _, rel := range detected {
		_, err = tx.Exec(ctx,
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

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
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
// relationships for a source node in a cluster. A nil permitted set
// allows any target; otherwise, when a manual row targets a connection
// outside permitted, nothing is deleted and ErrHiddenRelationship is
// returned.
func (d *Datastore) ClearNodeRelationships(ctx context.Context, clusterID int, sourceConnectionID int, permitted map[int]bool) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to clear manual relationships: %w", err)
	}
	defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit

	if err := deleteManualRelationships(ctx, tx, clusterID, sourceConnectionID, permitted); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
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
		`SELECT EXISTS(SELECT 1 FROM connections WHERE id = $1 AND deleted_at IS NULL)`,
		connectionID,
	).Scan(&connExists)
	if err != nil {
		return fmt.Errorf("failed to check connection existence: %w", err)
	}
	if !connExists {
		return ErrConnectionNotFound
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to add server to cluster: %w", err)
	}
	defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit

	query := `
        UPDATE connections
        SET cluster_id = $2, role = $3, membership_source = 'manual',
            updated_at = CURRENT_TIMESTAMP
        WHERE id = $1 AND deleted_at IS NULL
    `

	_, err = tx.Exec(ctx, query, connectionID, clusterID, role)
	if err != nil {
		return fmt.Errorf("failed to add server to cluster: %w", err)
	}

	// Adding a member of another cluster moves it, so its rows in the
	// old cluster go in the same transaction (issue #707).
	if err := dropRelationshipsOutsideCluster(ctx, tx, connectionID, &clusterID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
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
	defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit

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
