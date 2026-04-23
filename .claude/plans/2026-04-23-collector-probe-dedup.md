# Collector Probe Deduplication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate duplicated code across collector probe files
(issue #76) by lifting shared methods into `BaseMetricsProbe`,
extracting common helpers, and cleaning up stale configuration.

**Architecture:** Move `EnsurePartition` into `BaseMetricsProbe` so
all 34 probe files inherit it. Extract change-detection helpers
(`hasDataChanged`, `computeMetricsHash`) into a
`ChangeTrackingMixin` struct that change-tracked probes embed.
Add `CheckViewExists` and `InvalidateFeatureCache` to the shared
base. Rename `StoreMetricsWithCopy` to `StoreMetrics`. Clean up
the `getDefaultInterval` map.

**Tech Stack:** Go 1.24, pgx/v5, collector sub-project

**Worktree:** `.worktrees/issue-76-collector-probe-dedup`

---

## File Map

### Files to Modify

- `collector/src/probes/base.go` — Add `EnsurePartition` to
  `BaseMetricsProbe`; add `CheckViewExists` helper; add
  `InvalidateFeatureCache` function for cache eviction.
- `collector/src/probes/storage.go` — Rename
  `StoreMetricsWithCopy` to `StoreMetrics`.
- `collector/src/probes/config_loader.go` — Add missing entries to
  `getDefaultInterval`; remove stale entries.
- `collector/src/probes/change_tracking.go` *(new)* — Shared
  `HasDataChanged` helper for the 4 generic change-tracked probes.
- `collector/src/probes/pg_extension_probe.go` — Remove
  `EnsurePartition`, `computeMetricsHash`; refactor
  `hasDataChanged` to use shared helper.
- `collector/src/probes/pg_settings_probe.go` — Same removals.
- `collector/src/probes/pg_hba_file_rules_probe.go` — Same
  removals.
- `collector/src/probes/pg_ident_file_mappings_probe.go` — Same
  removals; convert `checkViewAvailable` to use `cachedCheck` +
  `CheckViewExists`.
- `collector/src/probes/pg_server_info_probe.go` — Remove
  `EnsurePartition`, `computeMetricsHash`; keep custom
  `hasDataChanged` (different query pattern).
- `collector/src/probes/pg_stat_statements_probe.go` — Remove
  `EnsurePartition`; replace `checkExtensionAvailable` with
  `CheckExtensionExists` + `cachedCheck`.
- `collector/src/probes/pg_stat_checkpointer_probe.go` — Remove
  `EnsurePartition`; wrap `checkCheckpointerViewExists` with
  `cachedCheck`.
- `collector/src/probes/pg_stat_recovery_prefetch_probe.go` —
  Remove `EnsurePartition`; convert `checkViewAvailable` to use
  `cachedCheck` + `CheckViewExists`.
- All remaining 23 probe files — Remove `EnsurePartition` method.
- `collector/src/probes/base_test.go` — Add tests for new shared
  functions.
- `collector/src/probes/change_tracking_test.go` *(new)* — Tests
  for `HasDataChanged`.

### Files Unchanged (reference only)

- `collector/src/probes/partition.go` — Package-level
  `EnsurePartition` stays; `BaseMetricsProbe` calls it.
- `collector/src/probes/hash.go` — `ComputeMetricsHash` stays.
- `collector/src/probes/constants.go` — Probe name constants.

---

## Task 1: Move `EnsurePartition` to `BaseMetricsProbe`

**Files:**
- Modify: `collector/src/probes/base.go`
- Modify: all 34 `*_probe.go` files (remove method)
- Test: `collector/src/probes/base_test.go`

**Why:** Every probe has an identical 3-line `EnsurePartition`
wrapper. Moving it to `BaseMetricsProbe` eliminates 34 copies.

- [ ] **Step 1: Add `EnsurePartition` to `BaseMetricsProbe` in
  `base.go`**

Add this method after the existing `GetConfig` method:

```go
// EnsurePartition creates the partition for the given timestamp
// if it does not already exist.
func (bp *BaseMetricsProbe) EnsurePartition(
    ctx context.Context,
    datastoreConn *pgxpool.Conn,
    timestamp time.Time,
) error {
    return EnsurePartition(ctx, datastoreConn, bp.GetTableName(), timestamp)
}
```

Add `"context"` and `"time"` to the import block. Add the
`pgxpool` import if not already present.

- [ ] **Step 2: Add test for `BaseMetricsProbe.EnsurePartition` in
  `base_test.go`**

Add a test that constructs a `BaseMetricsProbe` and verifies that
`EnsurePartition` delegates to the package-level function with the
correct table name. Since the package-level function requires a
real database connection, test at the unit level by verifying the
method exists and accepts the correct signature. Also verify that
the method satisfies the `MetricsProbe` interface requirement.

- [ ] **Step 3: Remove `EnsurePartition` from all 34 probe files**

Remove the `EnsurePartition` method from every `*_probe.go` file.
Each probe embeds `BaseMetricsProbe` and will inherit the new
shared method. The files are:

`pg_connectivity_probe.go`, `pg_database_probe.go`,
`pg_extension_probe.go`, `pg_hba_file_rules_probe.go`,
`pg_ident_file_mappings_probe.go`, `pg_node_role_probe.go`,
`pg_replication_slots_probe.go`, `pg_server_info_probe.go`,
`pg_settings_probe.go`, `pg_stat_activity_probe.go`,
`pg_stat_all_indexes_probe.go`, `pg_stat_all_tables_probe.go`,
`pg_stat_checkpointer_probe.go`, `pg_stat_connection_security_probe.go`,
`pg_stat_database_conflicts_probe.go`, `pg_stat_database_probe.go`,
`pg_stat_io_probe.go`, `pg_stat_recovery_prefetch_probe.go`,
`pg_stat_replication_probe.go`, `pg_stat_statements_probe.go`,
`pg_stat_subscription_probe.go`, `pg_stat_wal_probe.go`,
`pg_statio_all_sequences_probe.go`,
`pg_stat_user_functions_probe.go`,
`pg_sys_cpu_info_probe.go`, `pg_sys_cpu_memory_by_process_probe.go`,
`pg_sys_cpu_usage_info_probe.go`, `pg_sys_disk_info_probe.go`,
`pg_sys_io_analysis_info_probe.go`,
`pg_sys_load_avg_info_probe.go`, `pg_sys_memory_info_probe.go`,
`pg_sys_network_info_probe.go`, `pg_sys_os_info_probe.go`,
`pg_sys_process_info_probe.go`

Also remove unused imports (`"context"`, `"time"`,
`pgxpool`) from each file if they are only used by the removed
method.

- [ ] **Step 4: Run tests and verify**

```bash
cd collector && make test
```

Expected: all tests pass. The `MetricsProbe` interface is still
satisfied because `BaseMetricsProbe` now provides
`EnsurePartition`.

- [ ] **Step 5: Run linter and format**

```bash
cd collector/src && gofmt -w . && golangci-lint run
```

Expected: no errors or warnings.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: move EnsurePartition to BaseMetricsProbe

Remove 34 identical EnsurePartition wrapper methods from
individual probe files. BaseMetricsProbe now provides the
shared implementation that delegates to the package-level
EnsurePartition function.

Closes part of #76

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 2: Extract shared change-detection helpers

**Files:**
- Create: `collector/src/probes/change_tracking.go`
- Create: `collector/src/probes/change_tracking_test.go`
- Modify: `collector/src/probes/pg_extension_probe.go`
- Modify: `collector/src/probes/pg_settings_probe.go`
- Modify: `collector/src/probes/pg_hba_file_rules_probe.go`
- Modify: `collector/src/probes/pg_ident_file_mappings_probe.go`
- Modify: `collector/src/probes/pg_server_info_probe.go`

**Why:** Five probes have `computeMetricsHash` one-liner wrappers
and four of them share a nearly identical `hasDataChanged`
pattern. The only probe-specific part is the SQL query used to
fetch stored data. `pg_server_info` has a structurally different
approach (single-row QueryRow) so it keeps its own
`hasDataChanged` but drops `computeMetricsHash`.

- [ ] **Step 1: Create `change_tracking.go` with shared helpers**

Create `collector/src/probes/change_tracking.go` containing:

```go
// HasDataChanged checks whether currentMetrics differ from the
// most recently stored data for the given connection. The
// fetchStoredQuery must be a SQL query that accepts a single $1
// parameter (connection_id) and returns the columns to compare.
// The optional normalizeMetrics function transforms collected
// metrics before hashing (e.g., renaming _database_name to
// database_name); pass nil to skip normalization.
func HasDataChanged(
    ctx context.Context,
    datastoreConn *pgxpool.Conn,
    connectionID int,
    probeName string,
    currentMetrics []map[string]any,
    fetchStoredQuery string,
    normalizeMetrics func([]map[string]any) []map[string]any,
) (bool, error) {
    // Apply normalization if provided
    metricsToHash := currentMetrics
    if normalizeMetrics != nil {
        metricsToHash = normalizeMetrics(currentMetrics)
    }

    currentHash, err := ComputeMetricsHash(metricsToHash)
    if err != nil {
        return false, fmt.Errorf(
            "failed to compute current metrics hash: %w", err)
    }

    rows, err := datastoreConn.Query(
        ctx, fetchStoredQuery, connectionID)
    if err != nil {
        return false, fmt.Errorf(
            "failed to query most recent data: %w", err)
    }
    defer rows.Close()

    storedMetrics, err := utils.ScanRowsToMaps(rows)
    if err != nil {
        return false, fmt.Errorf(
            "failed to scan stored data: %w", err)
    }

    if len(storedMetrics) == 0 {
        logger.Infof(
            "No previous %s data found for connection %d",
            probeName, connectionID)
        return true, nil
    }

    storedHash, err := ComputeMetricsHash(storedMetrics)
    if err != nil {
        return false, fmt.Errorf(
            "failed to compute stored metrics hash: %w", err)
    }

    return currentHash != storedHash, nil
}
```

Include the copyright header.

- [ ] **Step 2: Write tests for `HasDataChanged` in
  `change_tracking_test.go`**

Test the following scenarios using `pgxmock` or similar:

1. No stored data returns `(true, nil)`.
2. Identical metrics returns `(false, nil)`.
3. Different metrics returns `(true, nil)`.
4. Query error returns wrapped error.
5. Normalization function is applied before hashing.

- [ ] **Step 3: Refactor `pg_settings_probe.go`**

Remove `computeMetricsHash` and `hasDataChanged`. In `Store`,
replace the call with:

```go
hasChanged, err := HasDataChanged(
    ctx, datastoreConn, connectionID, "pg_settings",
    metrics,
    `SELECT name, setting, unit, category, short_desc,
            extra_desc, context, vartype, source, min_val,
            max_val, enumvals, boot_val, reset_val, sourcefile,
            sourceline, pending_restart
     FROM metrics.pg_settings
     WHERE connection_id = $1
       AND collected_at = (
           SELECT MAX(collected_at)
           FROM metrics.pg_settings
           WHERE connection_id = $1
       )
     ORDER BY name`,
    nil,
)
```

- [ ] **Step 4: Refactor `pg_hba_file_rules_probe.go`**

Same pattern — remove `computeMetricsHash` and `hasDataChanged`;
call `HasDataChanged` with the HBA-specific query and `nil`
normalizer.

- [ ] **Step 5: Refactor `pg_ident_file_mappings_probe.go`**

Same pattern — remove `computeMetricsHash` and `hasDataChanged`;
call `HasDataChanged` with the ident-specific query and `nil`
normalizer.

- [ ] **Step 6: Refactor `pg_extension_probe.go`**

Remove `computeMetricsHash` and `hasDataChanged`. Call
`HasDataChanged` with the extension-specific query and a
normalizer function that renames `_database_name` to
`database_name`:

```go
hasChanged, err := HasDataChanged(
    ctx, datastoreConn, connectionID, "pg_extension",
    metrics,
    `SELECT database_name, extname, extversion,
            extrelocatable, schema_name
     FROM metrics.pg_extension
     WHERE connection_id = $1
       AND collected_at = (
           SELECT MAX(collected_at)
           FROM metrics.pg_extension
           WHERE connection_id = $1
       )
     ORDER BY database_name, extname`,
    normalizeDatabaseName,
)
```

Define `normalizeDatabaseName` as a package-level function in
`change_tracking.go`:

```go
// normalizeDatabaseName renames _database_name keys to
// database_name to match the stored column name.
func normalizeDatabaseName(
    metrics []map[string]any,
) []map[string]any {
    result := make([]map[string]any, len(metrics))
    for i, m := range metrics {
        normalized := make(map[string]any, len(m))
        for k, v := range m {
            if k == "_database_name" {
                normalized["database_name"] = v
            } else {
                normalized[k] = v
            }
        }
        result[i] = normalized
    }
    return result
}
```

- [ ] **Step 7: Refactor `pg_server_info_probe.go`**

Remove only `computeMetricsHash`. Keep the custom `hasDataChanged`
because it uses `QueryRow` and explicit typed scanning (not
`ScanRowsToMaps`). Replace the internal call
`p.computeMetricsHash(...)` with `ComputeMetricsHash(...)`.

- [ ] **Step 8: Run tests**

```bash
cd collector && make test
```

- [ ] **Step 9: Run linter and format**

```bash
cd collector/src && gofmt -w . && golangci-lint run
```

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "refactor: extract shared change-detection helpers

Add HasDataChanged() to change_tracking.go for the common
pattern: hash current metrics, query stored data, hash stored
data, compare. Four change-tracked probes now call the shared
helper. pg_server_info keeps its custom hasDataChanged but
drops the computeMetricsHash wrapper.

Removes 5 computeMetricsHash wrappers and 4 hasDataChanged
copies.

Closes part of #76

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 3: Replace `checkExtensionAvailable` and consolidate view checks

**Files:**
- Modify: `collector/src/probes/base.go`
- Modify: `collector/src/probes/pg_stat_statements_probe.go`
- Modify: `collector/src/probes/pg_stat_checkpointer_probe.go`
- Modify: `collector/src/probes/pg_stat_recovery_prefetch_probe.go`
- Modify: `collector/src/probes/pg_ident_file_mappings_probe.go`
- Test: `collector/src/probes/base_test.go`

**Why:** `pg_stat_statements` duplicates `CheckExtensionExists`
without caching. Three probes have view-existence methods that
bypass `cachedCheck`. Adding a shared `CheckViewExists` helper
and wiring all probes through `cachedCheck` fixes both issues.

- [ ] **Step 1: Add `CheckViewExists` to `base.go`**

```go
// CheckViewExists checks whether a view exists in pg_catalog.
func CheckViewExists(
    ctx context.Context,
    conn *pgxpool.Conn,
    viewName string,
) (bool, error) {
    var exists bool
    err := conn.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1 FROM pg_catalog.pg_views
            WHERE schemaname = 'pg_catalog'
              AND viewname = $1
        )
    `, viewName).Scan(&exists)
    if err != nil {
        return false, fmt.Errorf(
            "failed to check if view %s exists: %w",
            viewName, err)
    }
    return exists, nil
}
```

- [ ] **Step 2: Add test for `CheckViewExists` in `base_test.go`**

Unit test verifying the function signature and SQL correctness
(use pgxmock if available, or verify via interface contract).

- [ ] **Step 3: Replace `checkExtensionAvailable` in
  `pg_stat_statements_probe.go`**

Remove the `checkExtensionAvailable` method. In `Execute`,
replace:

```go
available, err := p.checkExtensionAvailable(ctx, monitoredConn)
```

with:

```go
available, err := cachedCheck(
    connectionName, "pg_stat_statements_ext", func() (bool, error) {
        return CheckExtensionExists(
            ctx, connectionName, monitoredConn,
            "pg_stat_statements")
    })
```

Also wrap the two column-existence checks
(`checkHasSharedBlkTime`, `checkHasBlkReadTime`) with
`cachedCheck` to avoid repeated catalog queries.

- [ ] **Step 4: Convert `pg_stat_checkpointer_probe.go` to use
  `cachedCheck` + `CheckViewExists`**

Remove `checkCheckpointerViewExists`. Replace the call in
`Execute`:

```go
checkpointerExists, err := cachedCheck(
    connectionName, "pg_stat_checkpointer_exists",
    func() (bool, error) {
        return CheckViewExists(
            ctx, monitoredConn, "pg_stat_checkpointer")
    })
```

- [ ] **Step 5: Convert `pg_stat_recovery_prefetch_probe.go`**

Remove `checkViewAvailable`. Replace the call:

```go
available, err := cachedCheck(
    connectionName, "pg_stat_recovery_prefetch_exists",
    func() (bool, error) {
        return CheckViewExists(
            ctx, monitoredConn, "pg_stat_recovery_prefetch")
    })
```

- [ ] **Step 6: Convert `pg_ident_file_mappings_probe.go`**

Remove `checkViewAvailable`. Replace:

```go
available, err := cachedCheck(
    connectionName, "pg_ident_file_mappings_exists",
    func() (bool, error) {
        return CheckViewExists(
            ctx, monitoredConn, "pg_ident_file_mappings")
    })
```

- [ ] **Step 7: Run tests**

```bash
cd collector && make test
```

- [ ] **Step 8: Run linter and format**

```bash
cd collector/src && gofmt -w . && golangci-lint run
```

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "refactor: consolidate view and extension checks

Add CheckViewExists() helper in base.go. Replace the
pg_stat_statements checkExtensionAvailable with the shared
CheckExtensionExists + cachedCheck. Convert three probes
(checkpointer, recovery_prefetch, ident_file_mappings) to use
CheckViewExists + cachedCheck instead of bespoke view-check
methods.

Closes part of #76

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 4: Add `InvalidateFeatureCache` for cache eviction

**Files:**
- Modify: `collector/src/probes/base.go`
- Modify: `collector/src/probes/base_test.go`

**Why:** `featureCache` is never invalidated. If a PostgreSQL
server is upgraded or an extension is installed, cached results
persist until the collector restarts. Adding an invalidation
function keyed by connection name lets the scheduler clear
entries when a connection is recycled.

- [ ] **Step 1: Add `InvalidateFeatureCache` to `base.go`**

```go
// InvalidateFeatureCache removes all cached feature-detection
// results for the given connection name. Call this when a
// monitored connection is recycled or its pool is refreshed
// so that stale view/extension checks do not persist.
func InvalidateFeatureCache(connectionName string) {
    prefix := connectionName + ":"
    featureCache.Range(func(key, _ any) bool {
        if k, ok := key.(string); ok && strings.HasPrefix(k, prefix) {
            featureCache.Delete(key)
        }
        return true
    })
}
```

Add `"strings"` to the import block.

- [ ] **Step 2: Add tests for `InvalidateFeatureCache`**

In `base_test.go`:

```go
func TestInvalidateFeatureCache(t *testing.T) {
    // Seed the cache with entries for two connections.
    featureCache.Store("conn1:view_x", true)
    featureCache.Store("conn1:view_y", false)
    featureCache.Store("conn2:view_x", true)

    InvalidateFeatureCache("conn1")

    // conn1 entries are gone.
    if _, ok := featureCache.Load("conn1:view_x"); ok {
        t.Error("expected conn1:view_x to be invalidated")
    }
    if _, ok := featureCache.Load("conn1:view_y"); ok {
        t.Error("expected conn1:view_y to be invalidated")
    }
    // conn2 entry is untouched.
    if _, ok := featureCache.Load("conn2:view_x"); !ok {
        t.Error("expected conn2:view_x to survive")
    }

    // Clean up.
    featureCache.Delete("conn2:view_x")
}
```

- [ ] **Step 3: Run tests**

```bash
cd collector && make test
```

- [ ] **Step 4: Run linter and format**

```bash
cd collector/src && gofmt -w . && golangci-lint run
```

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add InvalidateFeatureCache for connection-scoped eviction

featureCache entries now have an explicit eviction path keyed
by connection name. Callers should invoke
InvalidateFeatureCache when a monitored connection pool is
recycled or refreshed.

Closes part of #76

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 5: Rename `StoreMetricsWithCopy` to `StoreMetrics`

**Files:**
- Modify: `collector/src/probes/storage.go`
- Modify: all probe files that call `StoreMetricsWithCopy`
- Test: existing tests

**Why:** The function uses batched INSERT statements, not the
COPY protocol. The name misleads maintainers about performance
characteristics and implementation.

- [ ] **Step 1: Rename the function in `storage.go`**

Rename `StoreMetricsWithCopy` to `StoreMetrics`. Update the
comment:

```go
// StoreMetrics stores metrics using batched INSERT statements.
func StoreMetrics(ctx context.Context, conn *pgxpool.Conn,
    tableName string, columns []string, values [][]any) error {
```

- [ ] **Step 2: Update all call sites**

Find-and-replace `StoreMetricsWithCopy` with `StoreMetrics`
across all probe files. Also update any comments referencing
"COPY protocol" in the Store methods to say "batched INSERT"
instead.

- [ ] **Step 3: Run tests**

```bash
cd collector && make test
```

- [ ] **Step 4: Run linter and format**

```bash
cd collector/src && gofmt -w . && golangci-lint run
```

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor: rename StoreMetricsWithCopy to StoreMetrics

The function uses batched INSERT statements, not the COPY
protocol. Rename to avoid misleading maintainers. Update all
call sites and comments.

Closes part of #76

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 6: Clean up `getDefaultInterval` map

**Files:**
- Modify: `collector/src/probes/config_loader.go`
- Test: `collector/src/probes/base_test.go` (or a new
  `config_loader_test.go`)

**Why:** The map is missing entries for 15+ existing probes and
the stale-entry concern from the issue.

- [ ] **Step 1: Update `getDefaultInterval` in `config_loader.go`**

Cross-reference the map with the probe name constants in
`constants.go`. Add all missing probes. The complete map should
include every constant from `constants.go`:

```go
func getDefaultInterval(probeName string) int {
    defaultIntervals := map[string]int{
        // Server-wide probes
        ProbeNamePgStatActivity:           60,
        ProbeNamePgStatReplication:        30,
        ProbeNamePgReplicationSlots:       300,
        ProbeNamePgStatRecoveryPrefetch:   600,
        ProbeNamePgStatSubscription:       300,
        ProbeNamePgStatConnectionSecurity: 300,
        ProbeNamePgStatIO:                 900,
        ProbeNamePgStatCheckpointer:       600,
        ProbeNamePgStatWAL:                600,
        ProbeNamePgSettings:               3600,
        ProbeNamePgHbaFileRules:           3600,
        ProbeNamePgIdentFileMappings:      3600,
        ProbeNamePgServerInfo:             3600,
        ProbeNamePgNodeRole:               300,
        ProbeNamePgConnectivity:           30,
        ProbeNamePgDatabase:               300,

        // Database-scoped probes
        ProbeNamePgStatDatabase:          300,
        ProbeNamePgStatDatabaseConflicts: 300,
        ProbeNamePgStatAllTables:         300,
        ProbeNamePgStatAllIndexes:        300,
        ProbeNamePgStatioAllSequences:    300,
        ProbeNamePgStatUserFunctions:     300,
        ProbeNamePgStatStatements:        300,
        ProbeNamePgExtension:             3600,

        // System stats probes
        ProbeNamePgSysOsInfo:             3600,
        ProbeNamePgSysCPUInfo:            3600,
        ProbeNamePgSysCPUUsageInfo:       60,
        ProbeNamePgSysMemoryInfo:         300,
        ProbeNamePgSysIoAnalysisInfo:     300,
        ProbeNamePgSysDiskInfo:           300,
        ProbeNamePgSysLoadAvgInfo:        60,
        ProbeNamePgSysProcessInfo:        300,
        ProbeNamePgSysNetworkInfo:        300,
        ProbeNamePgSysCPUMemoryByProcess: 300,
    }

    if interval, ok := defaultIntervals[probeName]; ok {
        return interval
    }
    return 300
}
```

Remove the old string-keyed entries and the inline comments with
old constant names.

Note: Previously the map used strings like
`"pg_stat_replication_slots"` for what is now the constant
`ProbeNamePgReplicationSlots` (value `"pg_replication_slots"`).
The old entry `"pg_stat_replication_slots"` no longer matches
any probe and should be removed. The entry
`"pg_stat_subscription_stats"` should also be removed; there is
no probe by that name. The entries `"pg_stat_bgwriter"`,
`"pg_stat_slru"`, `"pg_stat_ssl"`, and `"pg_stat_gssapi"` should
also be removed if they do not correspond to registered probe
names.

- [ ] **Step 2: Add test for `getDefaultInterval` completeness**

Write a test that iterates over all constants in `constants.go`
and verifies that `getDefaultInterval` returns a non-default
value for each one. This catches future drift.

- [ ] **Step 3: Run tests**

```bash
cd collector && make test
```

- [ ] **Step 4: Run linter and format**

```bash
cd collector/src && gofmt -w . && golangci-lint run
```

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "fix: clean up getDefaultInterval map

Use ProbeNameXxx constants instead of string literals. Add
missing entries for all registered probes. Remove stale entries
for non-existent probes. Add completeness test to prevent
future drift.

Closes part of #76

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 7: Coverage verification and final check

**Files:** All modified files from Tasks 1–6.

- [ ] **Step 1: Run full test suite with coverage**

```bash
cd collector && make coverage
```

Verify that `base.go`, `change_tracking.go`, `storage.go`, and
`config_loader.go` all meet the 90% coverage floor.

- [ ] **Step 2: Run `make test-all` from root**

```bash
make test-all
```

Expected: all sub-projects pass.

- [ ] **Step 3: Final format and lint**

```bash
cd collector/src && gofmt -w . && golangci-lint run
```

- [ ] **Step 4: Final commit if any adjustments needed**

If coverage gaps required additional tests, commit them:

```bash
git add -A
git commit -m "test: improve coverage for refactored probe code

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Notes

- `pg_server_info_probe.go` keeps its own `hasDataChanged`
  because it uses `QueryRow` with explicit typed scanning rather
  than `ScanRowsToMaps`. Forcing it into the generic helper would
  require changing its query strategy for no benefit.

- The `pg_stat_io_probe.go` already uses `cachedCheck` for both
  `checkIOViewExists` and `checkSLRUViewExists`. Those methods
  contain inline SQL rather than calling `CheckViewExists`. They
  could optionally be updated to use `CheckViewExists`, but since
  they already use `cachedCheck`, the caching concern is already
  addressed. The sub-agent should update them if straightforward,
  but this is lower priority.

- `pg_stat_connection_security_probe.go` already uses
  `cachedCheck`. No changes needed there.

- Change-tracked probes (`pg_settings`, `pg_hba_file_rules`,
  `pg_ident_file_mappings`, `pg_extension`) get a 3600-second
  (hourly) default interval since they only store on change
  anyway.

- System stats probes that report rapidly-changing data
  (`cpu_usage`, `load_avg`) get a 60-second default interval;
  the rest default to 300 seconds.
