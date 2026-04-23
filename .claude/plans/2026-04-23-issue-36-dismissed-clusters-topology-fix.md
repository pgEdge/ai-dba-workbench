# Issue #36: Dismissed Clusters Topology Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent dismissed (soft-deleted) auto-detected clusters from
reappearing in the topology view, and allow users to DELETE
auto-detected clusters via the API.

**Architecture:** Add a `dismissedKeys` filter (mirroring the existing
`claimedKeys` pattern) to `buildTopologyHierarchy` and
`buildAutoDetectedClusters`. Extend the API handler at
`/api/v1/clusters/{id}` to accept DELETE for `server-*` and
`cluster-spock-*` paths by resolving the auto_cluster_key and
soft-deleting via a new `DeleteAutoDetectedCluster` datastore method.
Update the OpenAPI specification to reflect the new DELETE operation.

**Tech Stack:** Go 1.24, pgx v5, net/http, PostgreSQL

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `server/src/internal/database/topology_autodetect.go` | Modify | Add `getDismissedAutoClusterKeys()`; update `buildAutoDetectedClusters()` signature to accept and filter dismissed keys |
| `server/src/internal/database/topology_queries.go` | Modify | Wire dismissed keys into `GetClusterTopology()`; update `buildTopologyHierarchy()` signature and filtering |
| `server/src/internal/database/cluster_queries.go` | Modify | Add `DeleteAutoDetectedCluster()` and `deriveClusterNameFromKey()` |
| `server/src/internal/api/cluster_handlers.go` | Modify | Add DELETE case for auto-detected cluster paths; add `deleteAutoDetectedCluster()` handler |
| `server/src/internal/api/openapi.go` | Modify | Add DELETE operation to `/api/v1/clusters/{id}` for auto-detected cluster IDs |
| `server/src/internal/database/cluster_dismiss_integration_test.go` | Modify | Add topology-filtering and delete-by-key integration tests |
| `server/src/internal/api/cluster_handlers_test.go` | Modify | Add tests for DELETE on auto-detected cluster paths |
| `docs/admin-guide/api/reference.md` | Modify | Update endpoint summary table |

---

### Task 1: Add `getDismissedAutoClusterKeys` and unit test

**Files:**

- Modify: `server/src/internal/database/topology_autodetect.go:176`
  (insert after `getClaimedAutoClusterKeys`)
- Modify:
  `server/src/internal/database/cluster_dismiss_integration_test.go`

- [ ] **Step 1: Write the failing integration test**

Add to `cluster_dismiss_integration_test.go` after the existing
`containsClusterID` helper:

```go
// TestGetDismissedAutoClusterKeys verifies that getDismissedAutoClusterKeys
// returns only the auto_cluster_keys of dismissed clusters.
func TestGetDismissedAutoClusterKeys(t *testing.T) {
    ds, pool, cleanup := newClusterDismissTestDatastore(t)
    defer cleanup()

    ctx := context.Background()
    groupID := insertClusterDismissTestGroup(t, pool)

    // Insert two auto-detected clusters: one active, one dismissed.
    _, err := pool.Exec(ctx, `
        INSERT INTO clusters (name, auto_cluster_key, group_id, dismissed)
        VALUES ('active-cluster', 'spock:active', $1, FALSE),
               ('dismissed-cluster', 'spock:dismissed', $1, TRUE)
    `, groupID)
    if err != nil {
        t.Fatalf("failed to seed clusters: %v", err)
    }

    // Also insert a manual cluster (no auto_cluster_key) that is
    // irrelevant to the dismissed key set.
    _, err = pool.Exec(ctx, `
        INSERT INTO clusters (name, group_id, dismissed)
        VALUES ('manual-cluster', $1, FALSE)
    `, groupID)
    if err != nil {
        t.Fatalf("failed to seed manual cluster: %v", err)
    }

    dismissed, err := ds.getDismissedAutoClusterKeys(ctx)
    if err != nil {
        t.Fatalf("getDismissedAutoClusterKeys failed: %v", err)
    }

    if len(dismissed) != 1 {
        t.Fatalf("expected 1 dismissed key, got %d: %v", len(dismissed), dismissed)
    }
    if !dismissed["spock:dismissed"] {
        t.Fatalf("expected spock:dismissed in set, got %v", dismissed)
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src && \
  go test ./internal/database/ \
    -run TestGetDismissedAutoClusterKeys -v -count=1
```

Expected: compilation error — `getDismissedAutoClusterKeys` is undefined.

- [ ] **Step 3: Implement `getDismissedAutoClusterKeys`**

Insert the following function in `topology_autodetect.go` immediately
after `getClaimedAutoClusterKeys` (after line 176):

```go
// getDismissedAutoClusterKeys returns auto_cluster_keys for clusters
// that have been dismissed (soft-deleted). The topology builder uses
// this set to suppress clusters the user has hidden.
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

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src && \
  go test ./internal/database/ \
    -run TestGetDismissedAutoClusterKeys -v -count=1
```

Expected: PASS.

- [ ] **Step 5: Run gofmt**

```bash
gofmt -w /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src/internal/database/topology_autodetect.go
gofmt -w /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src/internal/database/cluster_dismiss_integration_test.go
```

- [ ] **Step 6: Commit**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36 && \
git add server/src/internal/database/topology_autodetect.go \
        server/src/internal/database/cluster_dismiss_integration_test.go && \
git commit -m "Add getDismissedAutoClusterKeys query and test

Query the clusters table for auto_cluster_key values that have been
soft-deleted (dismissed = TRUE). This set will be used by the topology
builder to suppress dismissed clusters from the UI. Issue #36.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: Wire dismissed keys into topology building

**Files:**

- Modify: `server/src/internal/database/topology_queries.go:157-273`
  (`GetClusterTopology`, `buildTopologyHierarchy`)
- Modify: `server/src/internal/database/topology_autodetect.go:181-337`
  (`buildAutoDetectedClusters`)
- Modify:
  `server/src/internal/database/cluster_dismiss_integration_test.go`

- [ ] **Step 1: Write the failing integration test**

Add to `cluster_dismiss_integration_test.go`:

```go
// TestDismissedClusterExcludedFromBuildTopologyHierarchy verifies that
// buildTopologyHierarchy omits clusters whose auto_cluster_key appears
// in the dismissedKeys set.
func TestDismissedClusterExcludedFromBuildTopologyHierarchy(t *testing.T) {
    ds, pool, cleanup := newClusterDismissTestDatastore(t)
    defer cleanup()

    ctx := context.Background()
    groupID := insertClusterDismissTestGroup(t, pool)

    // Create an auto-detected cluster and then dismiss it.
    created, err := ds.UpsertAutoDetectedCluster(
        ctx, "binary:999", "doomed-cluster", nil, &groupID,
    )
    if err != nil {
        t.Fatalf("UpsertAutoDetectedCluster failed: %v", err)
    }
    if err := ds.DeleteCluster(ctx, created.ID); err != nil {
        t.Fatalf("DeleteCluster failed: %v", err)
    }

    // The dismissed key must be returned by getDismissedAutoClusterKeys.
    dismissedKeys, err := ds.getDismissedAutoClusterKeys(ctx)
    if err != nil {
        t.Fatalf("getDismissedAutoClusterKeys failed: %v", err)
    }
    if !dismissedKeys["binary:999"] {
        t.Fatalf("expected binary:999 in dismissed set")
    }

    // Build a minimal topology; the dismissed key must be absent from
    // the resulting clusters list.
    defaultGroup := &defaultGroupInfo{ID: groupID, Name: "Servers/Clusters"}
    groups := ds.buildTopologyHierarchy(
        nil,                              // no connections
        make(map[string]clusterOverride), // no overrides
        make(map[string]bool),            // no claimed keys
        dismissedKeys,                    // dismissed keys
        defaultGroup,
    )
    for _, g := range groups {
        for _, c := range g.Clusters {
            if c.AutoClusterKey == "binary:999" {
                t.Fatalf("dismissed cluster binary:999 appeared in topology")
            }
        }
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src && \
  go test ./internal/database/ \
    -run TestDismissedClusterExcludedFromBuildTopologyHierarchy -v -count=1
```

Expected: compilation error — `buildTopologyHierarchy` does not accept
a `dismissedKeys` parameter.

- [ ] **Step 3: Update `buildTopologyHierarchy` signature and add filtering**

In `topology_queries.go`, change the function signature at line 455
from:

```go
func (d *Datastore) buildTopologyHierarchy(connections []connectionWithRole, clusterOverrides map[string]clusterOverride, claimedKeys map[string]bool, defaultGroup *defaultGroupInfo) []TopologyGroup {
```

to:

```go
func (d *Datastore) buildTopologyHierarchy(connections []connectionWithRole, clusterOverrides map[string]clusterOverride, claimedKeys map[string]bool, dismissedKeys map[string]bool, defaultGroup *defaultGroupInfo) []TopologyGroup {
```

Then add `dismissedKeys` checks next to every existing `claimedKeys`
check. There are four locations:

**Spock clusters (line 545):** Change:
```go
if cluster.AutoClusterKey != "" && claimedKeys[cluster.AutoClusterKey] {
```
to:
```go
if cluster.AutoClusterKey != "" && (claimedKeys[cluster.AutoClusterKey] || dismissedKeys[cluster.AutoClusterKey]) {
```

**Binary clusters (line 569):** Change:
```go
if claimedKeys[autoKey] {
```
to:
```go
if claimedKeys[autoKey] || dismissedKeys[autoKey] {
```

**Logical clusters (line 601):** Change:
```go
if cluster.AutoClusterKey != "" && claimedKeys[cluster.AutoClusterKey] {
```
to:
```go
if cluster.AutoClusterKey != "" && (claimedKeys[cluster.AutoClusterKey] || dismissedKeys[cluster.AutoClusterKey]) {
```

**Standalone servers (line 625):** Change:
```go
if claimedKeys[autoKey] {
```
to:
```go
if claimedKeys[autoKey] || dismissedKeys[autoKey] {
```

- [ ] **Step 4: Update the call site in `GetClusterTopology`**

In `topology_queries.go`, after Step 4 (line 197, after
`getClaimedAutoClusterKeys`), add:

```go
// Step 4b: Get dismissed auto-detected cluster keys so they are
// excluded from the topology. Issue #36.
dismissedKeys, err := d.getDismissedAutoClusterKeys(ctx)
if err != nil {
    return nil, fmt.Errorf("failed to get dismissed cluster keys: %w", err)
}
```

Then update the call to `buildTopologyHierarchy` at line 226 from:

```go
defaultGroups := d.buildTopologyHierarchy(unclaimedConnections, clusterOverrides, claimedKeys, defaultGroup)
```

to:

```go
defaultGroups := d.buildTopologyHierarchy(unclaimedConnections, clusterOverrides, claimedKeys, dismissedKeys, defaultGroup)
```

- [ ] **Step 5: Update `buildAutoDetectedClusters` to filter dismissed keys**

In `topology_autodetect.go`, change the signature at line 181 from:

```go
func (d *Datastore) buildAutoDetectedClusters(connections []connectionWithRole, clusterOverrides map[string]clusterOverride) map[string]TopologyCluster {
```

to:

```go
func (d *Datastore) buildAutoDetectedClusters(connections []connectionWithRole, clusterOverrides map[string]clusterOverride, dismissedKeys map[string]bool) map[string]TopologyCluster {
```

Add dismissed-key checks for each cluster type:

**Spock clusters (after line 262):** Change:
```go
for _, cluster := range spockClusters {
    if cluster.AutoClusterKey != "" {
        result[cluster.AutoClusterKey] = cluster
    }
}
```
to:
```go
for _, cluster := range spockClusters {
    if cluster.AutoClusterKey != "" && !dismissedKeys[cluster.AutoClusterKey] {
        result[cluster.AutoClusterKey] = cluster
    }
}
```

**Binary clusters (after line 282):** Add before building the
TopologyCluster (immediately after the `autoKey :=` line):
```go
if dismissedKeys[autoKey] {
    continue
}
```

**Logical clusters (after line 303):** Change:
```go
for _, cluster := range logicalClusters {
    if cluster.AutoClusterKey != "" {
        result[cluster.AutoClusterKey] = cluster
    }
}
```
to:
```go
for _, cluster := range logicalClusters {
    if cluster.AutoClusterKey != "" && !dismissedKeys[cluster.AutoClusterKey] {
        result[cluster.AutoClusterKey] = cluster
    }
}
```

**Standalone servers (after line 318):** Add after the `autoKey :=`
line:
```go
if dismissedKeys[autoKey] {
    continue
}
```

- [ ] **Step 6: Update the call site for `buildAutoDetectedClusters`**

In `topology_queries.go` at line 190, change:

```go
autoDetectedClusters := d.buildAutoDetectedClusters(allConnections, clusterOverrides)
```

to:

```go
autoDetectedClusters := d.buildAutoDetectedClusters(allConnections, clusterOverrides, dismissedKeys)
```

Note: `dismissedKeys` must be queried before this line. Move the new
Step 4b code (from Step 4 above) to before Step 3, i.e., between the
`clusterOverrides` block (line 186) and the
`buildAutoDetectedClusters` call (line 190).

- [ ] **Step 7: Run the test to verify it passes**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src && \
  go test ./internal/database/ \
    -run TestDismissedClusterExcluded -v -count=1
```

Expected: PASS.

- [ ] **Step 8: Run full database package tests**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src && \
  go test ./internal/database/ -v -count=1
```

Expected: all tests pass (existing tests must not break).

- [ ] **Step 9: Run gofmt**

```bash
gofmt -w /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src/internal/database/topology_queries.go
gofmt -w /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src/internal/database/topology_autodetect.go
gofmt -w /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src/internal/database/cluster_dismiss_integration_test.go
```

- [ ] **Step 10: Commit**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36 && \
git add server/src/internal/database/topology_queries.go \
        server/src/internal/database/topology_autodetect.go \
        server/src/internal/database/cluster_dismiss_integration_test.go && \
git commit -m "Filter dismissed clusters from topology building

Wire getDismissedAutoClusterKeys into GetClusterTopology,
buildTopologyHierarchy, and buildAutoDetectedClusters so that clusters
a user has dismissed no longer reappear in the ClusterNavigator. The
dismissedKeys set mirrors the existing claimedKeys pattern. Issue #36.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: Add `DeleteAutoDetectedCluster` datastore method and tests

**Files:**

- Modify: `server/src/internal/database/cluster_queries.go`
- Modify:
  `server/src/internal/database/cluster_dismiss_integration_test.go`

- [ ] **Step 1: Write the failing integration tests**

Add to `cluster_dismiss_integration_test.go`:

```go
// TestDeleteAutoDetectedCluster_ExistingRow verifies that
// DeleteAutoDetectedCluster soft-deletes an existing cluster by its
// auto_cluster_key and detaches connections.
func TestDeleteAutoDetectedCluster_ExistingRow(t *testing.T) {
    ds, pool, cleanup := newClusterDismissTestDatastore(t)
    defer cleanup()

    ctx := context.Background()
    groupID := insertClusterDismissTestGroup(t, pool)

    // Create cluster and attach a connection.
    cluster, err := ds.UpsertAutoDetectedCluster(
        ctx, "binary:100", "test-cluster", nil, &groupID,
    )
    if err != nil {
        t.Fatalf("UpsertAutoDetectedCluster failed: %v", err)
    }
    _, err = pool.Exec(ctx, `
        INSERT INTO connections (name, cluster_id, membership_source)
        VALUES ('conn-1', $1, 'auto')
    `, cluster.ID)
    if err != nil {
        t.Fatalf("failed to insert connection: %v", err)
    }

    // Soft-delete by auto_cluster_key.
    if err := ds.DeleteAutoDetectedCluster(ctx, "binary:100"); err != nil {
        t.Fatalf("DeleteAutoDetectedCluster failed: %v", err)
    }

    // Cluster must be dismissed.
    var dismissed bool
    err = pool.QueryRow(ctx,
        `SELECT dismissed FROM clusters WHERE id = $1`, cluster.ID,
    ).Scan(&dismissed)
    if err != nil {
        t.Fatalf("failed to read dismissed flag: %v", err)
    }
    if !dismissed {
        t.Fatal("cluster was not dismissed")
    }

    // Connection must be detached.
    var clusterID *int
    err = pool.QueryRow(ctx,
        `SELECT cluster_id FROM connections WHERE name = 'conn-1'`,
    ).Scan(&clusterID)
    if err != nil {
        t.Fatalf("failed to read connection cluster_id: %v", err)
    }
    if clusterID != nil {
        t.Fatalf("connection still attached to cluster %d", *clusterID)
    }
}

// TestDeleteAutoDetectedCluster_NoRow verifies that
// DeleteAutoDetectedCluster creates a dismissed record when no cluster
// row exists for the given auto_cluster_key.
func TestDeleteAutoDetectedCluster_NoRow(t *testing.T) {
    ds, pool, cleanup := newClusterDismissTestDatastore(t)
    defer cleanup()

    ctx := context.Background()
    _ = insertClusterDismissTestGroup(t, pool)

    // Delete a cluster that has no database row yet.
    if err := ds.DeleteAutoDetectedCluster(ctx, "spock:phantom"); err != nil {
        t.Fatalf("DeleteAutoDetectedCluster (no row) failed: %v", err)
    }

    // A dismissed row must have been created.
    var dismissed bool
    var name string
    err := pool.QueryRow(ctx, `
        SELECT name, dismissed FROM clusters
        WHERE auto_cluster_key = 'spock:phantom'
    `).Scan(&name, &dismissed)
    if err != nil {
        t.Fatalf("failed to read newly created cluster: %v", err)
    }
    if !dismissed {
        t.Fatal("newly created cluster is not dismissed")
    }
    if name != "phantom Spock" {
        t.Fatalf("derived name = %q, want %q", name, "phantom Spock")
    }
}

// TestDeriveClusterNameFromKey verifies the key-to-name mapping.
func TestDeriveClusterNameFromKey(t *testing.T) {
    tests := []struct {
        key  string
        want string
    }{
        {"spock:pg17", "pg17 Spock"},
        {"binary:42", "binary-42"},
        {"standalone:7", "standalone-7"},
        {"logical:99", "logical-99"},
        {"unknown", "unknown"},
        {"custom:xyz", "custom:xyz"},
    }
    for _, tt := range tests {
        got := deriveClusterNameFromKey(tt.key)
        if got != tt.want {
            t.Errorf("deriveClusterNameFromKey(%q) = %q, want %q",
                tt.key, got, tt.want)
        }
    }
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src && \
  go test ./internal/database/ \
    -run "TestDeleteAutoDetectedCluster|TestDeriveClusterNameFromKey" \
    -v -count=1
```

Expected: compilation error — `DeleteAutoDetectedCluster` and
`deriveClusterNameFromKey` are undefined.

- [ ] **Step 3: Implement `deriveClusterNameFromKey`**

Add at the end of `cluster_queries.go`:

```go
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
```

- [ ] **Step 4: Implement `DeleteAutoDetectedCluster`**

Add in `cluster_queries.go` after `DeleteCluster`:

```go
// DeleteAutoDetectedCluster soft-deletes an auto-detected cluster by
// its auto_cluster_key. If no database record exists for the key, a
// dismissed placeholder is created so the topology builder skips the
// cluster on subsequent refreshes.
func (d *Datastore) DeleteAutoDetectedCluster(ctx context.Context, autoKey string) error {
    d.mu.Lock()
    defer d.mu.Unlock()

    // Try to find an existing cluster with this auto_cluster_key.
    var clusterID int
    err := d.pool.QueryRow(ctx,
        `SELECT id FROM clusters WHERE auto_cluster_key = $1`,
        autoKey,
    ).Scan(&clusterID)

    if err != nil {
        // No existing record — create one in dismissed state.
        name := deriveClusterNameFromKey(autoKey)
        _, insertErr := d.pool.Exec(ctx, `
            INSERT INTO clusters (name, auto_cluster_key, dismissed)
            VALUES ($1, $2, TRUE)
        `, name, autoKey)
        if insertErr != nil {
            return fmt.Errorf("failed to create dismissed cluster record: %w", insertErr)
        }
        return nil
    }

    // Existing record found — soft-delete it.
    _, err = d.pool.Exec(ctx,
        `UPDATE clusters SET dismissed = TRUE, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
        clusterID,
    )
    if err != nil {
        return fmt.Errorf("failed to dismiss cluster: %w", err)
    }

    // Detach connections.
    _, err = d.pool.Exec(ctx,
        `UPDATE connections SET cluster_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE cluster_id = $1`,
        clusterID,
    )
    if err != nil {
        return fmt.Errorf("failed to detach connections from dismissed cluster: %w", err)
    }

    return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src && \
  go test ./internal/database/ \
    -run "TestDeleteAutoDetectedCluster|TestDeriveClusterNameFromKey" \
    -v -count=1
```

Expected: PASS.

- [ ] **Step 6: Run gofmt**

```bash
gofmt -w /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src/internal/database/cluster_queries.go
gofmt -w /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src/internal/database/cluster_dismiss_integration_test.go
```

- [ ] **Step 7: Commit**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36 && \
git add server/src/internal/database/cluster_queries.go \
        server/src/internal/database/cluster_dismiss_integration_test.go && \
git commit -m "Add DeleteAutoDetectedCluster datastore method

Soft-delete auto-detected clusters by auto_cluster_key. When no DB
row exists for the key, create a dismissed placeholder so the topology
builder suppresses the cluster on subsequent refreshes. Issue #36.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 4: Add DELETE handler for auto-detected clusters

**Files:**

- Modify: `server/src/internal/api/cluster_handlers.go:346-355`
- Modify: `server/src/internal/api/cluster_handlers_test.go`

- [ ] **Step 1: Write the failing test**

Add to `cluster_handlers_test.go`. Follow the existing test patterns
in that file (use `httptest.NewRecorder`, mock datastore, etc.).
First, find the existing test setup in the file to understand the
mock pattern, then add:

```go
func TestDeleteAutoDetectedCluster_Success(t *testing.T) {
    // This test verifies that DELETE /api/v1/clusters/server-42
    // calls DeleteAutoDetectedCluster with the correct auto_cluster_key.
    // Follow the existing mock pattern in this file.
}
```

The exact test shape depends on the mock infrastructure already in the
file. The test must:

1. Send `DELETE /api/v1/clusters/server-42` with admin auth.
2. Assert the response is 204 No Content.
3. Assert `DeleteAutoDetectedCluster` was called with
   `"standalone:42"`.

Also test the Spock path:

1. Send `DELETE /api/v1/clusters/cluster-spock-pg17` with admin auth.
2. Assert 204 No Content.
3. Assert `DeleteAutoDetectedCluster` was called with `"spock:pg17"`.

And a forbidden test:

1. Send `DELETE /api/v1/clusters/server-42` without admin auth.
2. Assert 403 Forbidden.

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src && \
  go test ./internal/api/ \
    -run TestDeleteAutoDetectedCluster -v -count=1
```

Expected: failure (handler not implemented).

- [ ] **Step 3: Add DELETE case to `handleClusterSubpath`**

In `cluster_handlers.go` at line 347-355, change:

```go
if strings.HasPrefix(parts[0], "server-") || strings.HasPrefix(parts[0], "cluster-spock-") {
    switch r.Method {
    case http.MethodPut:
        h.updateAutoDetectedCluster(w, r, parts[0])
    default:
        w.Header().Set("Allow", "PUT")
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
    }
    return
}
```

to:

```go
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

- [ ] **Step 4: Add `deleteAutoDetectedCluster` handler**

Add after `updateAutoDetectedCluster` in `cluster_handlers.go`:

```go
// deleteAutoDetectedCluster handles DELETE requests for auto-detected
// clusters. It resolves the auto_cluster_key from the topology ID and
// soft-deletes the cluster.
func (h *ClusterHandler) deleteAutoDetectedCluster(w http.ResponseWriter, r *http.Request, clusterID string) {
    if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
        RespondError(w, http.StatusForbidden,
            "Permission denied: you do not have permission to delete auto-detected clusters")
        return
    }

    autoKey := computeAutoClusterKey(clusterID)
    if autoKey == "" {
        RespondError(w, http.StatusBadRequest, "Invalid auto-detected cluster ID")
        return
    }

    ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
    defer cancel()

    if err := h.datastore.DeleteAutoDetectedCluster(ctx, autoKey); err != nil {
        log.Printf("[ERROR] Failed to delete auto-detected cluster %s: %v",
            logging.SanitizeForLog(clusterID), err)
        RespondError(w, http.StatusInternalServerError,
            "Failed to delete auto-detected cluster")
        return
    }

    w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 5: Add `DeleteAutoDetectedCluster` to the datastore interface**

Check if there is a `DatastoreInterface` or similar interface that the
mock uses. If so, add the method signature:

```go
DeleteAutoDetectedCluster(ctx context.Context, autoKey string) error
```

- [ ] **Step 6: Run the tests to verify they pass**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src && \
  go test ./internal/api/ \
    -run TestDeleteAutoDetectedCluster -v -count=1
```

Expected: PASS.

- [ ] **Step 7: Run gofmt**

```bash
gofmt -w /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src/internal/api/cluster_handlers.go
```

- [ ] **Step 8: Commit**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36 && \
git add server/src/internal/api/cluster_handlers.go \
        server/src/internal/api/cluster_handlers_test.go && \
git commit -m "Add DELETE handler for auto-detected clusters

Support DELETE on /api/v1/clusters/server-{id} and
/api/v1/clusters/cluster-spock-{prefix} paths. The handler resolves
the auto_cluster_key and delegates to DeleteAutoDetectedCluster.
Requires manage_connections permission. Issue #36.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 5: Update OpenAPI specification

**Files:**

- Modify: `server/src/internal/api/openapi.go`
- Modify: `docs/admin-guide/api/reference.md`

- [ ] **Step 1: Add DELETE operation to the clusters path in `openapi.go`**

Find the `/api/v1/clusters/{id}` path entry in `BuildOpenAPISpec()`
and add a DELETE operation entry. Follow the pattern of the existing
DELETE operation for database-backed clusters, but note that this
DELETE also applies to `server-*` and `cluster-spock-*` ID formats.

- [ ] **Step 2: Regenerate the static OpenAPI file**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server && \
  make openapi
```

- [ ] **Step 3: Run the OpenAPI tests**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src && \
  go test ./internal/api/ -run OpenAPI -v
```

Expected: PASS.

- [ ] **Step 4: Update endpoint summary table in `reference.md`**

Add or update the DELETE entry for `/api/v1/clusters/{id}` to note
that auto-detected cluster IDs (`server-*`, `cluster-spock-*`) are
now accepted.

- [ ] **Step 5: Commit**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36 && \
git add server/src/internal/api/openapi.go \
        docs/admin-guide/api/openapi.json \
        docs/admin-guide/api/reference.md && \
git commit -m "Update OpenAPI spec for auto-detected cluster DELETE

Document that DELETE /api/v1/clusters/{id} now accepts auto-detected
cluster IDs (server-*, cluster-spock-*). Issue #36.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 6: Full test suite and coverage verification

**Files:** All modified files.

- [ ] **Step 1: Run the full test suite**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36 && \
  make test-all
```

Expected: all tests pass.

- [ ] **Step 2: Run coverage for the server package**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server && \
  make coverage && \
  go tool cover -func=coverage.out | grep -E \
    "(getDismissed|DeleteAutoDetected|deriveCluster|buildTopologyHierarchy|buildAutoDetected)"
```

Verify all new functions meet the 90% line coverage floor.

- [ ] **Step 3: Run gofmt on all modified files**

```bash
gofmt -l /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src/internal/database/topology_autodetect.go \
         /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src/internal/database/topology_queries.go \
         /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src/internal/database/cluster_queries.go \
         /workspaces/ai-dba-workbench/.worktrees/fix-issue-36/server/src/internal/api/cluster_handlers.go
```

Expected: no output (all files already formatted).

- [ ] **Step 4: Fix any failures and re-run**

If tests fail or coverage is below 90%, fix the issues and re-run.

- [ ] **Step 5: Final commit (if any fixes were needed)**

```bash
cd /workspaces/ai-dba-workbench/.worktrees/fix-issue-36 && \
git add -A && \
git commit -m "Fix test and coverage issues from review

Co-Authored-By: Claude <noreply@anthropic.com>"
```
