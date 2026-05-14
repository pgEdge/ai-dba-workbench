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
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// insertConnectionDeleteTestConnection inserts a connection row with
// the given name and (optional) cluster_id and returns its id. A nil
// clusterID inserts NULL for cluster_id, matching the production
// behavior where unattached connections have no cluster.
func insertConnectionDeleteTestConnection(
	t *testing.T,
	pool *pgxpool.Pool,
	name string,
	clusterID *int,
) int {
	t.Helper()
	var id int
	err := pool.QueryRow(context.Background(), `
        INSERT INTO connections (name, cluster_id, membership_source)
        VALUES ($1, $2, 'auto')
        RETURNING id
    `, name, clusterID).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert connection %q: %v", name, err)
	}
	return id
}

// insertConnectionDeleteTestCluster inserts a clusters row with the
// supplied auto_cluster_key (which may be nil for user-created
// clusters) and returns its id.
func insertConnectionDeleteTestCluster(
	t *testing.T,
	pool *pgxpool.Pool,
	name string,
	autoKey *string,
	groupID int,
) int {
	t.Helper()
	var id int
	err := pool.QueryRow(context.Background(), `
        INSERT INTO clusters (name, auto_cluster_key, group_id, dismissed)
        VALUES ($1, $2, $3, FALSE)
        RETURNING id
    `, name, autoKey, groupID).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert cluster %q: %v", name, err)
	}
	return id
}

// readClusterDismissed returns the dismissed flag for the cluster
// with the given id, failing the test if the row is missing.
func readClusterDismissed(t *testing.T, pool *pgxpool.Pool, id int) bool {
	t.Helper()
	var dismissed bool
	err := pool.QueryRow(context.Background(),
		`SELECT dismissed FROM clusters WHERE id = $1`, id,
	).Scan(&dismissed)
	if err != nil {
		t.Fatalf("Failed to read dismissed flag for cluster %d: %v", id, err)
	}
	return dismissed
}

// TestDeleteConnection_LastInAutoClusterDismisses verifies the fix
// for issue #238. When the final connection attached to an
// auto-detected cluster is deleted, the cluster is soft-dismissed in
// the same transaction so it no longer appears in autocomplete
// results.
func TestDeleteConnection_LastInAutoClusterDismisses(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	groupID := insertClusterDismissTestGroup(t, pool)
	autoKey := "spock:issue238-last"
	clusterID := insertConnectionDeleteTestCluster(
		t, pool, "auto-cluster-1", &autoKey, groupID,
	)
	connID := insertConnectionDeleteTestConnection(
		t, pool, "only-conn", &clusterID,
	)

	// Sanity: cluster appears in the autocomplete dropdown.
	summaries, err := ds.ListClustersForAutocomplete(ctx)
	if err != nil {
		t.Fatalf("ListClustersForAutocomplete failed: %v", err)
	}
	if !containsClusterID(summaries, clusterID) {
		t.Fatalf("dropdown missing freshly-inserted cluster %d", clusterID)
	}

	// Delete the only connection in the cluster.
	if err := ds.DeleteConnection(ctx, connID); err != nil {
		t.Fatalf("DeleteConnection failed: %v", err)
	}

	// The connection row must be gone.
	var remaining int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM connections WHERE id = $1`, connID,
	).Scan(&remaining)
	if err != nil {
		t.Fatalf("counting connection rows failed: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected connection %d to be deleted, found %d rows", connID, remaining)
	}

	// The auto-detected cluster must now be soft-dismissed.
	if !readClusterDismissed(t, pool, clusterID) {
		t.Fatalf("expected cluster %d to be dismissed after losing its last connection", clusterID)
	}

	// Dropdown must no longer return the now-empty cluster.
	summaries, err = ds.ListClustersForAutocomplete(ctx)
	if err != nil {
		t.Fatalf("ListClustersForAutocomplete (after delete) failed: %v", err)
	}
	if containsClusterID(summaries, clusterID) {
		t.Fatalf("dropdown still returns empty auto-detected cluster %d (issue #238)", clusterID)
	}
}

// TestDeleteConnection_AutoClusterStillHasMembers verifies that
// deleting one of several connections in an auto-detected cluster
// leaves the cluster active. Only the last departure should trigger
// the soft-dismiss.
func TestDeleteConnection_AutoClusterStillHasMembers(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	groupID := insertClusterDismissTestGroup(t, pool)
	autoKey := "binary:issue238-multi"
	clusterID := insertConnectionDeleteTestCluster(
		t, pool, "auto-cluster-2", &autoKey, groupID,
	)
	connA := insertConnectionDeleteTestConnection(t, pool, "conn-a", &clusterID)
	_ = insertConnectionDeleteTestConnection(t, pool, "conn-b", &clusterID)

	if err := ds.DeleteConnection(ctx, connA); err != nil {
		t.Fatalf("DeleteConnection failed: %v", err)
	}

	// Cluster must remain visible because another connection still
	// references it.
	if readClusterDismissed(t, pool, clusterID) {
		t.Fatalf("cluster %d was dismissed despite still having one connection", clusterID)
	}
	summaries, err := ds.ListClustersForAutocomplete(ctx)
	if err != nil {
		t.Fatalf("ListClustersForAutocomplete failed: %v", err)
	}
	if !containsClusterID(summaries, clusterID) {
		t.Fatalf("dropdown unexpectedly hid still-occupied cluster %d", clusterID)
	}
}

// TestDeleteConnection_LastInUserClusterPreserved verifies that the
// dismiss path is reserved for auto-detected clusters. Deleting the
// last connection in a user-created cluster (auto_cluster_key IS
// NULL) must NOT change the cluster's dismissed flag; the user
// owns those rows and removes them explicitly via DeleteCluster.
func TestDeleteConnection_LastInUserClusterPreserved(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	groupID := insertClusterDismissTestGroup(t, pool)
	clusterID := insertConnectionDeleteTestCluster(
		t, pool, "user-cluster", nil, groupID,
	)
	connID := insertConnectionDeleteTestConnection(
		t, pool, "user-conn", &clusterID,
	)

	if err := ds.DeleteConnection(ctx, connID); err != nil {
		t.Fatalf("DeleteConnection failed: %v", err)
	}

	if readClusterDismissed(t, pool, clusterID) {
		t.Fatalf("user-created cluster %d should not be auto-dismissed", clusterID)
	}
	summaries, err := ds.ListClustersForAutocomplete(ctx)
	if err != nil {
		t.Fatalf("ListClustersForAutocomplete failed: %v", err)
	}
	if !containsClusterID(summaries, clusterID) {
		t.Fatalf("dropdown unexpectedly hid empty user-created cluster %d", clusterID)
	}
}

// TestDeleteConnection_NullClusterID verifies that deleting a
// connection with no parent cluster (cluster_id IS NULL) succeeds
// and leaves every clusters row untouched.
func TestDeleteConnection_NullClusterID(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	groupID := insertClusterDismissTestGroup(t, pool)
	autoKey := "binary:bystander"
	bystander := insertConnectionDeleteTestCluster(
		t, pool, "bystander", &autoKey, groupID,
	)

	// Connection has no parent cluster.
	connID := insertConnectionDeleteTestConnection(t, pool, "orphan", nil)

	if err := ds.DeleteConnection(ctx, connID); err != nil {
		t.Fatalf("DeleteConnection failed: %v", err)
	}

	// The unrelated cluster must remain untouched.
	if readClusterDismissed(t, pool, bystander) {
		t.Fatalf("unrelated cluster %d was dismissed by NULL-clusterID delete", bystander)
	}
}

// TestDeleteConnection_NotFoundReturnsError verifies that deleting
// an id that does not exist returns ErrConnectionNotFound and does
// not perturb any cluster rows.
func TestDeleteConnection_NotFoundReturnsError(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	groupID := insertClusterDismissTestGroup(t, pool)
	autoKey := "spock:not-found"
	clusterID := insertConnectionDeleteTestCluster(
		t, pool, "bystander", &autoKey, groupID,
	)

	err := ds.DeleteConnection(ctx, 999999)
	if !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("DeleteConnection(999999) error = %v, want ErrConnectionNotFound", err)
	}

	// Cluster row must remain visible.
	if readClusterDismissed(t, pool, clusterID) {
		t.Fatalf("cluster %d was dismissed by a no-op delete", clusterID)
	}
}

// TestDeleteConnection_CanceledContextDuringLookup exercises the
// non-ErrNoRows error branch of the initial SELECT. A context that
// is canceled before the call enters DeleteConnection causes the
// QueryRow scan to fail with a wrapped context.Canceled, not with
// pgx.ErrNoRows, so the function must surface a wrapped lookup
// error rather than ErrConnectionNotFound.
func TestDeleteConnection_CanceledContextDuringLookup(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	groupID := insertClusterDismissTestGroup(t, pool)
	autoKey := "binary:cancel-lookup"
	clusterID := insertConnectionDeleteTestCluster(
		t, pool, "cancel-target", &autoKey, groupID,
	)
	connID := insertConnectionDeleteTestConnection(
		t, pool, "cancel-conn", &clusterID,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ds.DeleteConnection(ctx, connID)
	if err == nil {
		t.Fatal("DeleteConnection with canceled context returned nil error")
	}
	if errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("DeleteConnection with canceled context returned ErrConnectionNotFound; "+
			"expected wrapped context error, got %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DeleteConnection error = %v, expected wrapped context.Canceled", err)
	}

	// Side effects must be confined to the transaction; the
	// connection row and cluster should be untouched.
	var remaining int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM connections WHERE id = $1`, connID,
	).Scan(&remaining); err != nil {
		t.Fatalf("post-cancel connection count failed: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("expected connection %d to survive canceled delete, found %d rows", connID, remaining)
	}
	if readClusterDismissed(t, pool, clusterID) {
		t.Fatalf("cluster %d was dismissed despite canceled delete", clusterID)
	}
}

// TestDeleteConnection_ClosedPoolReturnsError exercises the Begin
// failure branch. A pool whose underlying connections have been
// closed cannot start a new transaction, so DeleteConnection must
// return a wrapped "failed to begin" error rather than silently
// succeeding.
func TestDeleteConnection_ClosedPoolReturnsError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping closed-pool test in -short mode")
	}
	_, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	// Build a second pool we can close without disturbing the
	// shared teardown pool above. The cluster_dismiss schema is
	// already in place from newClusterDismissTestDatastore.
	connStr := pool.Config().ConnString()
	scratchPool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		t.Fatalf("scratch pool creation failed: %v", err)
	}
	scratch := NewTestDatastore(scratchPool)
	scratchPool.Close()

	err = scratch.DeleteConnection(context.Background(), 1)
	if err == nil {
		t.Fatal("DeleteConnection against closed pool returned nil error")
	}
	if errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("DeleteConnection against closed pool returned ErrConnectionNotFound; "+
			"expected a Begin-failure wrap, got %v", err)
	}
}

// TestDeleteConnection_LookupTypeMismatch exercises the
// non-ErrNoRows error branch of the initial SELECT. By temporarily
// dropping the cluster_id column we force the SELECT to fail with
// an undefined-column error, which is not pgx.ErrNoRows, so
// DeleteConnection must surface a wrapped lookup error.
func TestDeleteConnection_LookupTypeMismatch(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	_ = insertClusterDismissTestGroup(t, pool)

	// Insert a real connection so that ErrNoRows is NOT the
	// failure mode; the SELECT instead fails on the missing column.
	_ = insertConnectionDeleteTestConnection(t, pool, "doomed", nil)

	if _, err := pool.Exec(ctx, `ALTER TABLE connections DROP COLUMN cluster_id`); err != nil {
		t.Fatalf("failed to drop cluster_id: %v", err)
	}

	err := ds.DeleteConnection(ctx, 1)
	if err == nil {
		t.Fatal("DeleteConnection with broken schema returned nil error")
	}
	if errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("DeleteConnection with broken schema returned ErrConnectionNotFound; "+
			"expected a lookup-failure wrap, got %v", err)
	}
}

// TestDeleteConnection_DeleteStepFails exercises the error
// branch where the DELETE statement itself fails after the
// initial lookup succeeds. We install a BEFORE DELETE trigger
// that unconditionally raises an exception; the SELECT step
// scans the row normally, but the subsequent DELETE aborts and
// must be surfaced as a wrapped "failed to delete connection"
// error.
func TestDeleteConnection_DeleteStepFails(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	_ = insertClusterDismissTestGroup(t, pool)
	connID := insertConnectionDeleteTestConnection(t, pool, "trigger-target", nil)

	if _, err := pool.Exec(ctx, `
        CREATE OR REPLACE FUNCTION block_connection_delete()
        RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN
            RAISE EXCEPTION 'connection delete blocked by test trigger';
        END;
        $$;
        DROP TRIGGER IF EXISTS block_conn_delete ON connections;
        CREATE TRIGGER block_conn_delete
        BEFORE DELETE ON connections
        FOR EACH ROW EXECUTE FUNCTION block_connection_delete();
    `); err != nil {
		t.Fatalf("failed to install delete-blocking trigger: %v", err)
	}

	err := ds.DeleteConnection(ctx, connID)
	if err == nil {
		t.Fatal("DeleteConnection returned nil error despite blocking trigger")
	}
	if errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("DeleteConnection returned ErrConnectionNotFound; "+
			"expected delete-failure wrap, got %v", err)
	}

	// The transaction must roll back; the row should remain.
	var remaining int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM connections WHERE id = $1`, connID,
	).Scan(&remaining); err != nil {
		t.Fatalf("post-trigger connection count failed: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("expected connection to survive trigger-failed delete, got %d rows", remaining)
	}
}

// TestDeleteConnection_DismissUpdateFails exercises the error
// branch where the UPDATE clusters statement fails. We drop the
// dismissed column on the clusters table after the SELECT and
// DELETE would have succeeded against a freshly inserted row; the
// follow-up UPDATE then fails with an undefined-column error,
// which must be surfaced as a wrapped "failed to dismiss" error.
//
// Implementation note: we cannot drop a column mid-transaction, so
// we exploit a different misuse: we insert a connection whose
// cluster_id points to a clusters row that we then delete out from
// under the foreign key by disabling the constraint. With no
// referenced clusters row, the UPDATE simply matches zero rows
// (no error). To force a real UPDATE error we instead poison the
// dismissed column by altering its type to one the UPDATE cannot
// satisfy.
func TestDeleteConnection_DismissUpdateFails(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	groupID := insertClusterDismissTestGroup(t, pool)
	autoKey := "binary:update-fail"
	clusterID := insertConnectionDeleteTestCluster(
		t, pool, "doomed-cluster", &autoKey, groupID,
	)
	connID := insertConnectionDeleteTestConnection(
		t, pool, "doomed-conn", &clusterID,
	)

	// Drop the dismissed column. The UPDATE statement references
	// it explicitly, so the statement will fail with an
	// undefined-column error after the DELETE has already
	// succeeded inside the transaction.
	if _, err := pool.Exec(ctx, `ALTER TABLE clusters DROP COLUMN dismissed`); err != nil {
		t.Fatalf("failed to drop dismissed column: %v", err)
	}

	err := ds.DeleteConnection(ctx, connID)
	if err == nil {
		t.Fatal("DeleteConnection returned nil error after dismissed column dropped")
	}
	if errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("DeleteConnection returned ErrConnectionNotFound; expected dismiss-failure wrap, got %v", err)
	}

	// The transaction must have rolled back; the connection row
	// should still exist.
	var remaining int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM connections WHERE id = $1`, connID,
	).Scan(&remaining); err != nil {
		t.Fatalf("post-failure connection count failed: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("expected connection to survive rolled-back delete, got %d rows", remaining)
	}
}
