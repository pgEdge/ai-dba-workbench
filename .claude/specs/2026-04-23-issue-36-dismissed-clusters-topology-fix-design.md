# Design Spec: Fix Dismissed Clusters Reappearing in Topology

This specification describes the fix for GitHub issue #36: "Deleted
auto-detected clusters reappear in the cluster selection dropdown."

## Summary

Auto-detected clusters that users dismiss reappear in the ClusterNavigator
topology view because `buildTopologyHierarchy()` reconstructs clusters
from live metrics without checking the `dismissed` flag. This fix adds
a dismissed-key filter to the topology building process and extends the
API to support DELETE on auto-detected cluster paths.

## Problem

The AI DBA Workbench has two paths for displaying clusters:

1. **Server creation dialog dropdown** (`GET /api/v1/clusters/list`) uses
   `ListClustersForAutocomplete()` with `WHERE dismissed = FALSE`. This
   correctly filters dismissed clusters.

2. **Cluster Navigator / topology view** (`GET /api/v1/clusters`) uses
   `GetClusterTopology()` which calls `buildTopologyHierarchy()`. This
   function builds clusters from raw connection metrics data (system
   identifiers, Spock node info, replication topology), not from the
   clusters table. It does NOT check the `dismissed` flag.

When a user deletes an auto-detected cluster:

- `DeleteCluster()` soft-deletes it by setting `dismissed = TRUE`.
- Connections are detached (`cluster_id = NULL`).
- But `buildTopologyHierarchy()` re-detects the cluster from live metrics
  on the next topology refresh.
- The cluster reappears in the ClusterNavigator because the topology
  builder does not know about dismissed clusters.

Additionally, the API handler at `cluster_handlers.go:347-355` returns
"Method not allowed" for DELETE on auto-detected cluster IDs
(`server-*`, `cluster-spock-*`). Only PUT is supported. This means
users cannot delete auto-detected clusters through the ClusterNavigator
UI unless the cluster has been synced to the database.

## Solution Overview

The fix combines two approaches:

### Part A: Filter dismissed clusters from topology building

This prevents dismissed clusters from appearing in the topology view by
checking the `dismissed` flag during the auto-detection process.

### Part C: Support DELETE for auto-detected clusters

This allows users to dismiss auto-detected clusters directly from the
ClusterNavigator UI without requiring the cluster to exist in the
database first.

## Detailed Design

### Part A: Filter Dismissed Clusters from Topology Building

#### New Function: getDismissedAutoClusterKeys

Add a new function in `topology_autodetect.go` that returns the set of
auto_cluster_keys that have been dismissed:

```go
// getDismissedAutoClusterKeys returns auto_cluster_keys for clusters
// that have been dismissed (soft-deleted). These keys are used to filter
// auto-detected clusters from the topology view.
func (d *Datastore) getDismissedAutoClusterKeys(ctx context.Context) (map[string]bool, error) {
    query := `
        SELECT auto_cluster_key
        FROM clusters
        WHERE auto_cluster_key IS NOT NULL
          AND dismissed = TRUE
    `

    rows, err := d.pool.Query(ctx, query)
    if err != nil {
        return nil, fmt.Errorf("failed to query dismissed cluster keys: %w", err)
    }
    defer rows.Close()

    dismissed := make(map[string]bool)
    for rows.Next() {
        var key string
        if err := rows.Scan(&key); err != nil {
            return nil, fmt.Errorf("failed to scan dismissed key: %w", err)
        }
        dismissed[key] = true
    }

    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("error iterating dismissed keys: %w", err)
    }

    return dismissed, nil
}
```

#### Modify GetClusterTopology

Update `GetClusterTopology()` in `topology_queries.go` to query dismissed
keys early and pass them to `buildTopologyHierarchy()`:

```go
// After Step 4 (getClaimedAutoClusterKeys), add:

// Step 4b: Get dismissed auto-detected cluster keys
dismissedKeys, err := d.getDismissedAutoClusterKeys(ctx)
if err != nil {
    return nil, fmt.Errorf("failed to get dismissed cluster keys: %w", err)
}
```

#### Modify buildTopologyHierarchy Signature

Update `buildTopologyHierarchy()` to accept the dismissed keys set and
filter clusters whose `AutoClusterKey` matches a dismissed key:

```go
func (d *Datastore) buildTopologyHierarchy(
    connections []connectionWithRole,
    clusterOverrides map[string]clusterOverride,
    claimedKeys map[string]bool,
    dismissedKeys map[string]bool,  // NEW PARAMETER
    defaultGroup *defaultGroupInfo,
) []TopologyGroup
```

The filtering logic mirrors the existing `claimedKeys` pattern. In each
place where the code checks `claimedKeys[autoKey]`, also check
`dismissedKeys[autoKey]`:

- Spock clusters (around line 545)
- Binary replication clusters (around line 569)
- Logical replication clusters (around line 601)
- Standalone servers (around line 625)

#### Modify buildAutoDetectedClusters

Update `buildAutoDetectedClusters()` in `topology_autodetect.go` to also
skip dismissed keys. This ensures dismissed clusters do not appear in
manual group topology either.

### Part C: Support DELETE for Auto-Detected Clusters

#### Modify handleClusterSubpath

Extend the handling for `server-*` and `cluster-spock-*` paths in
`cluster_handlers.go` to support DELETE in addition to PUT:

```go
// Check if it's an auto-detected cluster ID (server-{id} or cluster-spock-{prefix})
if strings.HasPrefix(parts[0], "server-") || strings.HasPrefix(parts[0], "cluster-spock-") {
    switch r.Method {
    case http.MethodPut:
        h.updateAutoDetectedCluster(w, r, parts[0])
    case http.MethodDelete:
        h.deleteAutoDetectedCluster(w, r, parts[0])
    default:
        w.Header().Set("Allow", "PUT, DELETE")
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
    }
    return
}
```

#### New Handler: deleteAutoDetectedCluster

Add a new handler function that resolves the auto_cluster_key from the
topology ID and performs the soft-delete:

```go
// deleteAutoDetectedCluster handles DELETE requests for auto-detected clusters.
// It resolves the auto_cluster_key from the topology ID, ensures a database
// record exists, then soft-deletes it.
func (h *ClusterHandler) deleteAutoDetectedCluster(w http.ResponseWriter, r *http.Request, clusterID string) {
    // Check user permissions - requires manage_connections permission
    if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
        RespondError(w, http.StatusForbidden,
            "Permission denied: you do not have permission to delete auto-detected clusters")
        return
    }

    // Compute auto_cluster_key from cluster ID
    autoKey := computeAutoClusterKey(clusterID)
    if autoKey == "" {
        RespondError(w, http.StatusBadRequest, "Invalid auto-detected cluster ID")
        return
    }

    ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
    defer cancel()

    // Delete (soft-delete) the cluster by auto_cluster_key
    err := h.datastore.DeleteAutoDetectedCluster(ctx, autoKey)
    if err != nil {
        log.Printf("[ERROR] Failed to delete auto-detected cluster %s: %v",
            logging.SanitizeForLog(clusterID), err)
        RespondError(w, http.StatusInternalServerError,
            "Failed to delete auto-detected cluster")
        return
    }

    w.WriteHeader(http.StatusNoContent)
}
```

#### New Datastore Method: DeleteAutoDetectedCluster

Add a new method in `cluster_queries.go` that handles soft-delete by
auto_cluster_key, creating the record first if necessary:

```go
// DeleteAutoDetectedCluster soft-deletes an auto-detected cluster by its
// auto_cluster_key. If no database record exists for the key, one is created
// first (with dismissed = TRUE) so the dismissed state persists across
// topology rebuilds.
func (d *Datastore) DeleteAutoDetectedCluster(ctx context.Context, autoKey string) error {
    d.mu.Lock()
    defer d.mu.Unlock()

    // First, try to find an existing cluster with this auto_cluster_key
    var clusterID int
    err := d.pool.QueryRow(ctx,
        `SELECT id FROM clusters WHERE auto_cluster_key = $1`,
        autoKey,
    ).Scan(&clusterID)

    if err != nil {
        // No existing record - create one in dismissed state
        // The name is derived from the auto_cluster_key prefix
        name := deriveClusterNameFromKey(autoKey)
        err = d.pool.QueryRow(ctx, `
            INSERT INTO clusters (name, auto_cluster_key, dismissed)
            VALUES ($1, $2, TRUE)
            RETURNING id
        `, name, autoKey).Scan(&clusterID)
        if err != nil {
            return fmt.Errorf("failed to create dismissed cluster record: %w", err)
        }
        return nil // Already dismissed upon creation
    }

    // Existing record found - soft-delete it
    _, err = d.pool.Exec(ctx,
        `UPDATE clusters SET dismissed = TRUE, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
        clusterID,
    )
    if err != nil {
        return fmt.Errorf("failed to dismiss cluster: %w", err)
    }

    // Detach connections
    _, err = d.pool.Exec(ctx,
        `UPDATE connections SET cluster_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE cluster_id = $1`,
        clusterID,
    )
    if err != nil {
        return fmt.Errorf("failed to detach connections from dismissed cluster: %w", err)
    }

    return nil
}

// deriveClusterNameFromKey extracts a reasonable name from an auto_cluster_key.
// For example, "spock:pg17" becomes "pg17 Spock", "binary:42" becomes "binary-42".
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
```

## Files Changed

### Server Changes

| File | Change |
|------|--------|
| `server/src/internal/database/topology_autodetect.go` | Add `getDismissedAutoClusterKeys()` function; update `buildAutoDetectedClusters()` to filter dismissed keys |
| `server/src/internal/database/topology_queries.go` | Update `GetClusterTopology()` to query dismissed keys; update `buildTopologyHierarchy()` signature and filtering logic |
| `server/src/internal/database/cluster_queries.go` | Add `DeleteAutoDetectedCluster()` and `deriveClusterNameFromKey()` functions |
| `server/src/internal/api/cluster_handlers.go` | Add DELETE support for auto-detected cluster paths; add `deleteAutoDetectedCluster()` handler |

### Test Changes

| File | Change |
|------|--------|
| `server/src/internal/database/cluster_dismiss_integration_test.go` | Add tests for topology filtering of dismissed clusters |
| `server/src/internal/api/cluster_handlers_test.go` | Add tests for DELETE on auto-detected clusters |

## Architecture Notes

### Pattern Consistency

The dismissed filter joins the existing `claimedKeys` filter pattern.
Both are `map[string]bool` sets that cause `buildTopologyHierarchy` to
skip matching auto_cluster_keys. This keeps the change minimal and
consistent with existing patterns.

### RBAC and Error Handling

The DELETE handler for auto-detected clusters follows the same RBAC and
error handling patterns as the existing `deleteCluster` handler for
database-backed clusters. It requires the `manage_connections` permission
and returns appropriate HTTP status codes.

### Soft-Delete Semantics

The fix preserves the existing soft-delete semantics:

- Auto-detected clusters are never hard-deleted.
- The `dismissed` flag prevents re-detection.
- Users can restore dismissed clusters through the rename (PUT) endpoint,
  which clears the dismissed flag via `UpsertClusterByAutoKey`.

### Query Flow

The topology query flow after this change:

1. `GetClusterTopology()` is called.
2. Query all connections with roles.
3. Get cluster name overrides (non-dismissed only).
4. Build auto-detected clusters map.
5. Get claimed keys (clusters moved to non-default groups).
6. **NEW:** Get dismissed keys.
7. Build manual groups topology.
8. Build default group topology, filtering out claimed AND dismissed keys.
9. Merge persisted members.
10. Populate relationships.
11. Apply visibility filter.
12. Return topology.

## Testing Strategy

### Unit Tests

- Verify `getDismissedAutoClusterKeys` returns the correct set of keys.
- Verify `deriveClusterNameFromKey` handles all key prefixes correctly.
- Verify `computeAutoClusterKey` handles all cluster ID formats.

### Integration Tests

New integration tests in `cluster_dismiss_integration_test.go`:

1. **TestDismissedClusterExcludedFromTopology**: Create a cluster, dismiss
   it, verify it does not appear in `GetClusterTopology` results.

2. **TestDismissedClusterStaysHiddenAfterRefresh**: Create a cluster,
   dismiss it, call `RefreshClusterAssignments`, verify the cluster
   remains hidden.

3. **TestDeleteAutoDetectedClusterAPI**: Call DELETE on a `server-*`
   path, verify the cluster is dismissed.

4. **TestDeleteAutoDetectedClusterCreatesRecord**: Call DELETE on a
   cluster that has no database record, verify a dismissed record is
   created.

### Existing Tests

The existing `TestUpsertAutoDetectedCluster_PreservesDismissed` test
continues to pass. This test verifies that `UpsertAutoDetectedCluster`
does not clear the dismissed flag when rediscovering a cluster.

### Coverage Requirements

All new and modified code must reach at least 90% line coverage:

- `getDismissedAutoClusterKeys()` - query execution paths
- `DeleteAutoDetectedCluster()` - both create and update paths
- `deleteAutoDetectedCluster()` - HTTP handler paths
- `buildTopologyHierarchy()` - dismissed key filtering branches

Run coverage verification:

```bash
cd server && make coverage
go tool cover -func=coverage.out | grep -E "(getDismissed|DeleteAuto|buildTopology)"
```

## Out of Scope

The following items are explicitly excluded from this change:

- **UI changes**: The client already calls the correct endpoints. This
  fix enables the DELETE operation server-side.

- **Restore dismissed clusters UI**: Users can restore dismissed clusters
  by renaming them (PUT). A dedicated "restore" UI is out of scope.

- **Hard-delete option**: All auto-detected clusters use soft-delete.
  A hard-delete option would require additional consideration around
  data retention and audit trails.

- **Batch dismiss**: Dismissing multiple clusters at once is out of scope.

## Risks and Mitigations

### Risk: Performance impact of additional query

The new `getDismissedAutoClusterKeys()` query runs on every topology
request. However, the query is simple (single-table scan with two
conditions) and the clusters table is small. The risk is minimal.

**Mitigation**: The query uses indexed columns (`auto_cluster_key`,
`dismissed`) and returns only keys, not full rows.

### Risk: Stale dismissed state

If the dismissed flag is set but the cluster row is later modified
through a bug or direct database access, the cluster could reappear.

**Mitigation**: The `dismissed` flag is only cleared through explicit
user action (PUT on the cluster). The `UpsertAutoDetectedCluster`
function explicitly preserves the dismissed flag.

### Risk: Orphaned dismissed records

Dismissed clusters remain in the database indefinitely. Over time, this
could accumulate unused rows.

**Mitigation**: The number of dismissed clusters is expected to be small
(user-initiated deletes only). A periodic cleanup job could be added in
a future release if needed.
