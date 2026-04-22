# Decompose Oversized Go Files — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split three oversized Go files into domain-focused files,
extract duplicated scan helpers, and refactor metric queries into a
data-driven registry — all without changing external behavior.

**Architecture:** Each file is decomposed within its existing Go
package so no import paths change. Functions are moved wholesale;
new scan-helper functions and a metric registry replace duplicated
code. Every task ends with `make test-all` green.

**Tech Stack:** Go 1.24, pgx/v5, PostgreSQL

**Spec:** `docs/superpowers/specs/2026-04-22-decompose-oversized-go-files-design.md`

---

## Phase 1: Collector `base.go` (886 lines → 5 files)

### Task 1: Extract `hash.go` from `collector/src/probes/base.go`

The simplest extraction: three pure functions with no database
dependencies.

**Files:**
- Create: `collector/src/probes/hash.go`
- Modify: `collector/src/probes/base.go` (remove lines 745-886)

- [ ] **Step 1: Create `hash.go`**

Create `collector/src/probes/hash.go` containing the copyright
header, `package probes`, the necessary imports (`crypto/sha256`,
`encoding/hex`, `encoding/json`, `fmt`, `math`, `sort`), and
three functions cut from `base.go`:

- `ComputeMetricsHash` (base.go lines 748-788)
- `normalizeValue` (base.go lines 794-876)
- `getSortedKeys` (base.go lines 879-886)

- [ ] **Step 2: Remove the moved code from `base.go`**

Delete lines 745-886 from `base.go` (the `ComputeMetricsHash`,
`normalizeValue`, and `getSortedKeys` functions). Also remove
any imports that are no longer used in `base.go` (`crypto/sha256`,
`encoding/hex`, `encoding/json`, `math`, `sort`).

- [ ] **Step 3: Verify compilation and tests**

```bash
cd collector/src && go build ./... && go test ./probes/ -v -count=1
```

All existing `base_test.go` tests (`TestNormalizeValue*`,
`TestComputeMetricsHash*`) must pass because the functions are
still in the same package.

- [ ] **Step 4: Run gofmt**

```bash
gofmt -w collector/src/probes/hash.go collector/src/probes/base.go
```

- [ ] **Step 5: Commit**

```bash
git add collector/src/probes/hash.go collector/src/probes/base.go
git commit -m "refactor(collector): extract hash.go from base.go

Move ComputeMetricsHash, normalizeValue, and getSortedKeys into
their own file for change-detection hashing.

Part of #75.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: Extract `partition.go` from `collector/src/probes/base.go`

Partition management: weekly bounds, partition creation, GC, and
helpers.

**Files:**
- Create: `collector/src/probes/partition.go`
- Modify: `collector/src/probes/base.go` (remove lines 138-415)

- [ ] **Step 1: Create `partition.go`**

Create `collector/src/probes/partition.go` containing the copyright
header, `package probes`, the necessary imports (`context`, `fmt`,
`strings`, `time`, `pgx/v5`, `pgx/v5/pgconn`, `pgx/v5/pgxpool`,
`logger`), and these items cut from `base.go`:

- `weeklyPartitionBounds` (lines 143-157)
- `partitionBoundLayout` constant (line 162)
- `EnsurePartition` (lines 165-214)
- `DropExpiredPartitions` (lines 223-275)
- `partitionCandidate` struct (lines 280-283)
- `loadProtectedPartitions` (lines 291-336)
- `loadPartitionCandidates` (lines 343-377)
- `parsePartitionEnd` (lines 385-415)

Include the comment block above each function.

- [ ] **Step 2: Remove the moved code from `base.go`**

Delete lines 138-415 from `base.go`. Remove any imports no longer
needed (`errors`, `strings`, `pgx/v5/pgconn`). Keep the imports
still used by remaining code.

- [ ] **Step 3: Verify compilation and tests**

```bash
cd collector/src && go build ./... && go test ./probes/ -v -count=1
```

Existing `base_test.go` partition tests (`TestWeeklyPartitionBounds*`,
`TestPartitionBoundLayout*`, `TestParsePartitionEnd`) must still pass.

- [ ] **Step 4: Run gofmt**

```bash
gofmt -w collector/src/probes/partition.go collector/src/probes/base.go
```

- [ ] **Step 5: Commit**

```bash
git add collector/src/probes/partition.go collector/src/probes/base.go
git commit -m "refactor(collector): extract partition.go from base.go

Move partition management (weekly bounds, creation, GC) into a
dedicated file.

Part of #75.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: Extract `storage.go` and `config_loader.go`

Split the remaining non-interface code into storage and config files.

**Files:**
- Create: `collector/src/probes/storage.go`
- Create: `collector/src/probes/config_loader.go`
- Modify: `collector/src/probes/base.go` (remove lines 417-743)

- [ ] **Step 1: Create `storage.go`**

Create `collector/src/probes/storage.go` with the copyright header,
`package probes`, necessary imports (`context`, `fmt`,
`pgx/v5`, `pgx/v5/pgxpool`, `logger`), and:

- `StoreMetricsWithCopy` (base.go lines 419-491)

- [ ] **Step 2: Create `config_loader.go`**

Create `collector/src/probes/config_loader.go` with the copyright
header, `package probes`, necessary imports (`context`, `errors`,
`fmt`, `strings`, `time`, `pgx/v5`, `pgx/v5/pgxpool`, `logger`),
and:

- `LoadProbeConfigs` (base.go lines 495-530)
- `EnsureProbeConfig` (base.go lines 536-647)
- `getDefaultInterval` (base.go lines 650-689)
- `GetLastCollectionTime` (base.go lines 693-719)
- `CheckExtensionExists` (base.go lines 723-743)

- [ ] **Step 3: Remove the moved code from `base.go`**

Delete lines 417-743 from `base.go` (everything from
`StoreMetricsWithCopy` through `CheckExtensionExists`). The
remaining `base.go` should contain only: the package declaration,
imports, `WrapQuery`, `featureCache`, `cachedCheck`, `ProbeConfig`,
`MetricsProbe`, `ExtensionProbe`, `BaseMetricsProbe`, and its
methods.

- [ ] **Step 4: Verify compilation and tests**

```bash
cd collector/src && go build ./... && go test ./probes/ -v -count=1
```

- [ ] **Step 5: Run gofmt and verify line counts**

```bash
gofmt -w collector/src/probes/storage.go collector/src/probes/config_loader.go collector/src/probes/base.go
wc -l collector/src/probes/base.go collector/src/probes/partition.go collector/src/probes/storage.go collector/src/probes/config_loader.go collector/src/probes/hash.go
```

Every file should be well under 1,000 lines.

- [ ] **Step 6: Commit**

```bash
git add collector/src/probes/storage.go collector/src/probes/config_loader.go collector/src/probes/base.go
git commit -m "refactor(collector): extract storage.go and config_loader.go

Move StoreMetricsWithCopy into storage.go and config resolution
functions (LoadProbeConfigs, EnsureProbeConfig, etc.) into
config_loader.go. base.go now contains only interface definitions.

Part of #75.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 4: Run full test suite for Phase 1

- [ ] **Step 1: Run make test-all**

```bash
cd /workspaces/ai-dba-workbench && make test-all
```

All tests across all sub-projects must pass. Fix any issues before
proceeding to Phase 2.

- [ ] **Step 2: Commit any fixes if needed**

---

## Phase 2: Alerter `queries.go` (2,557 lines → 5 files + registry)

### Task 5: Extract `alert_queries.go` with `scanAlert` helper

Move alert lifecycle functions and extract the duplicated 21-field
scan pattern.

**Files:**
- Create: `alerter/src/internal/database/alert_queries.go`
- Modify: `alerter/src/internal/database/queries.go`

- [ ] **Step 1: Create `alert_queries.go`**

Create `alerter/src/internal/database/alert_queries.go` with the
copyright header, `package database`, necessary imports, and these
functions moved from `queries.go`:

- `GetActiveThresholdAlert` (line 1073)
- `GetActiveAnomalyAlert` (line 1103)
- `GetRecentlyClearedAlert` (line 1131)
- `GetReevaluationSuppressedAlert` (line 1152)
- `GetFalsePositiveSuppressedAlert` (line 1177)
- `UpdateAlertValues` (line 1202)
- `CreateAlert` (line 1215)
- `GetActiveAlerts` (line 1232)
- `GetAlert` (line 1271)
- `ClearAlert` (line 1295)
- `ReactivateAlert` (line 1314)
- `GetAlertsByCluster` (line 2442)
- `GetAlertsByConnection` (line 2488)
- `UpdateAlertReevaluation` (line 2528)

Also add a `scanAlert` helper function at the top of the file.
Identify the exact 21-field scan pattern from the existing code
(in `GetActiveThresholdAlert`, `GetActiveAnomalyAlert`,
`GetActiveAlerts`, `GetAlert`, `GetAlertsByCluster`,
`GetAlertsByConnection`) and replace each inline `Scan(...)` call
with `scanAlert(row, &alert)`.

The `scanAlert` helper should accept a `pgx.Row` interface and
an `*Alert` pointer, scanning all 21 fields.

- [ ] **Step 2: Remove the moved functions from `queries.go`**

Delete the moved functions from `queries.go`. Update imports in
both files.

- [ ] **Step 3: Verify compilation and tests**

```bash
cd alerter/src && go build ./... && go test ./internal/database/ -v -count=1
```

- [ ] **Step 4: Run gofmt**

```bash
gofmt -w alerter/src/internal/database/alert_queries.go alerter/src/internal/database/queries.go
```

- [ ] **Step 5: Commit**

```bash
git add alerter/src/internal/database/alert_queries.go alerter/src/internal/database/queries.go
git commit -m "refactor(alerter): extract alert_queries.go with scanAlert helper

Move 14 alert lifecycle functions into alert_queries.go. Extract
the duplicated 21-field alert scan pattern into scanAlert().

Part of #75.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 6: Extract `anomaly_queries.go` with scan helper

Move anomaly, baseline, and embedding functions.

**Files:**
- Create: `alerter/src/internal/database/anomaly_queries.go`
- Modify: `alerter/src/internal/database/queries.go`

- [ ] **Step 1: Create `anomaly_queries.go`**

Create `alerter/src/internal/database/anomaly_queries.go` with the
copyright header, `package database`, necessary imports, and these
functions moved from `queries.go`:

- `CreateAnomalyCandidate` (line 1557)
- `GetUnprocessedAnomalyCandidates` (line 1569)
- `UpdateAnomalyCandidate` (line 1606)
- `StoreAnomalyEmbedding` (line 2144)
- `FindSimilarAnomalies` (line 2175)
- `GetAnomalyCandidateByID` (line 2222)
- `GetMetricBaselines` (line 1479)
- `UpsertMetricBaseline` (line 1513)
- `GetAcknowledgedAnomalyAlerts` (line 2310)
- `GetAcknowledgmentHistoryForMetric` (line 2359)
- `float32SliceToVectorString` (line 2539)

Also add a `scanAcknowledgedAnomalyAlert` helper for the 15-field
scan pattern shared by `GetAcknowledgedAnomalyAlerts` and
`GetAcknowledgmentHistoryForMetric`.

- [ ] **Step 2: Remove the moved functions from `queries.go`**

- [ ] **Step 3: Verify compilation and tests**

```bash
cd alerter/src && go build ./... && go test ./internal/database/ -v -count=1
```

- [ ] **Step 4: Run gofmt and commit**

```bash
gofmt -w alerter/src/internal/database/anomaly_queries.go alerter/src/internal/database/queries.go
git add alerter/src/internal/database/anomaly_queries.go alerter/src/internal/database/queries.go
git commit -m "refactor(alerter): extract anomaly_queries.go

Move anomaly candidate, embedding, baseline, and acknowledgment
functions. Extract scanAcknowledgedAnomalyAlert helper.

Part of #75.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 7: Build metric registry and extract `metric_queries.go`

This is the most significant behavioral refactor: replacing the two
massive switch statements with a data-driven metric registry.

**Files:**
- Create: `alerter/src/internal/database/metric_registry.go`
- Create: `alerter/src/internal/database/metric_queries.go`
- Modify: `alerter/src/internal/database/queries.go`

- [ ] **Step 1: Create `metric_registry.go`**

Create `alerter/src/internal/database/metric_registry.go` with the
copyright header, `package database`, and the registry types and
data.

Read the exact SQL from each case in `GetLatestMetricValues`
(lines 295-1057) and `GetHistoricalMetricValues` (lines 1626-2132)
to populate the registry. Each entry maps a metric name to its
latest SQL, historical SQL, and scan type.

Define the `scanType` enum, `metricQueryConfig` struct, and the
`metricRegistry` map containing every metric.

- [ ] **Step 2: Create `metric_queries.go`**

Create `alerter/src/internal/database/metric_queries.go` with the
copyright header, `package database`, necessary imports, and these
functions:

- `queryMetricValues` (line 220)
- `queryMetricValuesWithDB` (line 243)
- `queryMetricValuesWithDBAndObject` (line 268)
- `GetLatestMetricValues` — rewritten to use the registry
- `GetLatestMetricValue` (line 1061)
- `GetHistoricalMetricValues` — rewritten to use the registry

The rewritten `GetLatestMetricValues` should look up the metric in
the registry and dispatch to the appropriate query helper based on
`scanType`. Similarly for `GetHistoricalMetricValues`, which should
use `queryHistoricalMetricValues`, `queryHistoricalMetricValuesWithDB`,
etc. — either reuse the existing helpers or write new ones for the
historical query pattern.

- [ ] **Step 3: Remove old metric functions from `queries.go`**

Delete `GetLatestMetricValues`, `GetLatestMetricValue`,
`GetHistoricalMetricValues`, `queryMetricValues`,
`queryMetricValuesWithDB`, `queryMetricValuesWithDBAndObject`,
and the `MetricValue` type definition (if present in queries.go
rather than types.go).

- [ ] **Step 4: Verify compilation and tests**

```bash
cd alerter/src && go build ./... && go test ./... -v -count=1
```

This is the critical step. The integration tests for metric
queries validate that every metric name returns correct data.

- [ ] **Step 5: Run gofmt and commit**

```bash
gofmt -w alerter/src/internal/database/metric_registry.go alerter/src/internal/database/metric_queries.go alerter/src/internal/database/queries.go
git add alerter/src/internal/database/metric_registry.go alerter/src/internal/database/metric_queries.go alerter/src/internal/database/queries.go
git commit -m "refactor(alerter): replace metric switch statements with registry

Introduce metricRegistry map that maps metric names to SQL queries
and scan types. GetLatestMetricValues and GetHistoricalMetricValues
now perform simple registry lookups instead of 27-case and 14-case
switch statements.

Part of #75.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 8: Verify alerter line counts and run full suite

- [ ] **Step 1: Check line counts**

```bash
wc -l alerter/src/internal/database/*.go | grep -v _test | sort -n
```

Every file must be under ~1,000 lines. `queries.go` should be
the remainder (connection monitoring, settings, blackouts, probe
availability, cluster peers).

- [ ] **Step 2: Run make test-all**

```bash
cd /workspaces/ai-dba-workbench && make test-all
```

- [ ] **Step 3: Commit any fixes if needed**

---

## Phase 3: Server `datastore.go` (5,668 lines → 7 files)

### Task 9: Extract `connection_queries.go`

**Files:**
- Create: `server/src/internal/database/connection_queries.go`
- Modify: `server/src/internal/database/datastore.go`

- [ ] **Step 1: Create `connection_queries.go`**

Create `server/src/internal/database/connection_queries.go` with
the copyright header, `package database`, necessary imports, and
these functions cut from `datastore.go`:

- `MonitoredConnection.MarshalJSON` (line 76)
- `NewDatastore` (line 212)
- `Close` (line 268)
- `GetAllConnections` (line 275)
- `GetConnectionSharingInfo` (line 310)
- `GetConnection` (line 323)
- `GetConnectionWithPassword` (line 352)
- `UpdateConnectionName` (line 374)
- `CreateConnection` (line 423)
- `DeleteConnection` (line 473)
- `UpdateConnectionFull` (line 512)
- `BuildConnectionString` (line 631)
- `ListDatabases` (line 687)
- `GetPool` (line 740)

Also extract `scanConnectionListItem` and `scanFullConnection`
helper functions from the duplicated scan patterns.

- [ ] **Step 2: Remove the moved functions from `datastore.go`**

- [ ] **Step 3: Verify compilation and tests**

```bash
cd server/src && go build ./... && go test ./internal/database/ -v -count=1
```

- [ ] **Step 4: Run gofmt and commit**

```bash
gofmt -w server/src/internal/database/connection_queries.go server/src/internal/database/datastore.go
git add server/src/internal/database/connection_queries.go server/src/internal/database/datastore.go
git commit -m "refactor(server): extract connection_queries.go

Move connection CRUD, NewDatastore, Close, and GetPool into
connection_queries.go. Extract scanConnectionListItem and
scanFullConnection helpers.

Part of #75.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 10: Extract `cluster_queries.go`

**Files:**
- Create: `server/src/internal/database/cluster_queries.go`
- Modify: `server/src/internal/database/datastore.go`

- [ ] **Step 1: Create `cluster_queries.go`**

Move all cluster/group CRUD functions (lines 798-1410):

- `GetClusterGroups`, `GetClusterGroup`, `CreateClusterGroup`,
  `CreateClusterGroupWithOwner`, `UpdateClusterGroup`,
  `DeleteClusterGroup`, `GetClustersInGroup`, `GetCluster`,
  `CreateCluster`, `UpdateCluster`, `UpdateClusterPartial`,
  `DeleteCluster`, `GetClusterOverrides`,
  `getClusterOverridesInternal`, `UpsertClusterByAutoKey`,
  `UpsertAutoDetectedCluster`, `GetGroupOverrides`,
  `getGroupOverridesInternal`, `getDefaultGroupInternal`,
  `GetDefaultGroupID`, `UpsertGroupByAutoKey`

- [ ] **Step 2: Remove moved code, verify, gofmt, commit**

```bash
cd server/src && go build ./... && go test ./internal/database/ -v -count=1
gofmt -w server/src/internal/database/cluster_queries.go server/src/internal/database/datastore.go
git add server/src/internal/database/cluster_queries.go server/src/internal/database/datastore.go
git commit -m "refactor(server): extract cluster_queries.go

Move cluster/group CRUD and auto-detection upsert functions.

Part of #75.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 11: Extract `topology_queries.go`

The largest extraction. Contains 50+ tightly coupled topology
functions.

**Files:**
- Create: `server/src/internal/database/topology_queries.go`
- Modify: `server/src/internal/database/datastore.go`

- [ ] **Step 1: Create `topology_queries.go`**

Move all topology-related functions: hierarchy builders, auto-
detection, replication extractors, persisted member handlers,
Spock/logical grouping, pruning, and relationship population.

This includes all functions from approximately lines 1412-4064
that are topology-related (see the function list in the spec).

Key functions: `GetServersInCluster`, `GetClusterHierarchy`,
`AssignConnectionToCluster`, `GetClusterTopology`,
`RefreshClusterAssignments`, plus all internal helpers
(`buildAutoDetectedClusters`, `buildTopologyHierarchy`,
`groupSpockNodesByClusters`, etc.).

- [ ] **Step 2: Check line count of topology_queries.go**

```bash
wc -l server/src/internal/database/topology_queries.go
```

If over 1,000 lines, split into `topology_queries.go` (public API
+ hierarchy builders) and `topology_autodetect.go` (auto-detection,
replication extractors, Spock/logical grouping, persisted members).

- [ ] **Step 3: Remove moved code, verify, gofmt, commit**

```bash
cd server/src && go build ./... && go test ./internal/database/ -v -count=1
gofmt -w server/src/internal/database/topology_queries.go server/src/internal/database/datastore.go
git add server/src/internal/database/topology_queries.go server/src/internal/database/datastore.go
git commit -m "refactor(server): extract topology_queries.go

Move topology building, auto-detection, replication inference,
and all related helpers.

Part of #75.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 12: Extract `alert_queries.go`

**Files:**
- Create: `server/src/internal/database/alert_queries.go`
- Modify: `server/src/internal/database/datastore.go`

- [ ] **Step 1: Create `alert_queries.go`**

Move alert management functions (lines 4144-4483):

- `GetAlerts` (line 4144)
- `GetAlertCounts` (line 4308)
- `GetAlertConnectionID` (line 4387)
- `SaveAlertAnalysis` (line 4401)
- `AcknowledgeAlert` (line 4423)
- `UnacknowledgeAlert` (line 4483)

Include the `Alert`, `AlertListFilter`, `AlertListResult`, and
`AlertCountsResult` types if they are defined in this section
(otherwise they stay in `datastore.go` with other types).

- [ ] **Step 2: Remove moved code, verify, gofmt, commit**

```bash
cd server/src && go build ./... && go test ./internal/database/ -v -count=1
gofmt -w server/src/internal/database/alert_queries.go server/src/internal/database/datastore.go
git add server/src/internal/database/alert_queries.go server/src/internal/database/datastore.go
git commit -m "refactor(server): extract alert_queries.go

Move alert retrieval, acknowledgment, and analysis functions.

Part of #75.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 13: Extract `estate_queries.go`

**Files:**
- Create: `server/src/internal/database/estate_queries.go`
- Modify: `server/src/internal/database/datastore.go`

- [ ] **Step 1: Create `estate_queries.go`**

Move estate snapshot functions (lines 4527-5064+):

- `GetEstateSnapshot`, `gatherEstateServerData`,
  `flattenTopologyServers`, `gatherEstateAlertData`,
  `gatherEstateBlackoutData`, `gatherEstateRecentEvents`,
  `GetServerSnapshot`, `GetClusterSnapshot`, `GetGroupSnapshot`,
  `GetConnectionsSnapshot`, `buildScopedSnapshot`,
  `getConnectionIDsForCluster`, `getConnectionIDsForGroup`,
  `GetConnectionIDsForCluster`, `GetConnectionIDsForGroup`,
  `gatherScopedServerData`, `gatherScopedAlertData`,
  `gatherScopedRecentEvents`, `GetConnectionContext`

- [ ] **Step 2: Remove moved code, verify, gofmt, commit**

```bash
cd server/src && go build ./... && go test ./internal/database/ -v -count=1
gofmt -w server/src/internal/database/estate_queries.go server/src/internal/database/datastore.go
git add server/src/internal/database/estate_queries.go server/src/internal/database/datastore.go
git commit -m "refactor(server): extract estate_queries.go

Move estate snapshot, server/cluster/group snapshots, scoped
snapshot builder, and connection context.

Part of #75.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 14: Extract `relationship_queries.go`

**Files:**
- Create: `server/src/internal/database/relationship_queries.go`
- Modify: `server/src/internal/database/datastore.go`

- [ ] **Step 1: Create `relationship_queries.go`**

Move node relationship and cluster membership functions
(lines 5293-5668):

- `CreateManualCluster`, `GetConnectionClusterInfo`,
  `ListClustersForAutocomplete`, `ResetMembershipSource`,
  `GetClusterRelationships`, `SetNodeRelationships`,
  `SyncAutoDetectedRelationships`, `RemoveNodeRelationship`,
  `ClearNodeRelationships`, `AddServerToCluster`,
  `RemoveServerFromCluster`, `IsConnectionInCluster`

- [ ] **Step 2: Remove moved code, verify, gofmt, commit**

```bash
cd server/src && go build ./... && go test ./internal/database/ -v -count=1
gofmt -w server/src/internal/database/relationship_queries.go server/src/internal/database/datastore.go
git add server/src/internal/database/relationship_queries.go server/src/internal/database/datastore.go
git commit -m "refactor(server): extract relationship_queries.go

Move cluster relationship CRUD and cluster membership functions.

Part of #75.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 15: Clean up remaining `datastore.go` and verify

The remaining `datastore.go` should contain only type definitions,
struct declarations, constants, and error variables.

**Files:**
- Modify: `server/src/internal/database/datastore.go`

- [ ] **Step 1: Verify line counts**

```bash
wc -l server/src/internal/database/*.go | grep -v _test | sort -n
```

Every file must be under ~1,000 lines. If `topology_queries.go`
exceeds the limit, split it now (see Task 11 Step 2).

- [ ] **Step 2: Clean up imports in remaining `datastore.go`**

Remove any unused imports from `datastore.go`. Ensure the file
compiles cleanly.

- [ ] **Step 3: Run full test suite**

```bash
cd /workspaces/ai-dba-workbench && make test-all
```

- [ ] **Step 4: Run gofmt on all modified files**

```bash
gofmt -w server/src/internal/database/datastore.go
```

- [ ] **Step 5: Final commit**

```bash
git add server/src/internal/database/datastore.go
git commit -m "refactor(server): clean up datastore.go types-only remainder

datastore.go now contains only type definitions and struct
declarations. All query functions live in domain-specific files.

Closes #75.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Phase 4: Final Verification

### Task 16: Full project verification

- [ ] **Step 1: Run make test-all**

```bash
cd /workspaces/ai-dba-workbench && make test-all
```

- [ ] **Step 2: Verify no file exceeds ~1,000 lines**

```bash
wc -l collector/src/probes/base.go collector/src/probes/partition.go collector/src/probes/storage.go collector/src/probes/config_loader.go collector/src/probes/hash.go
wc -l alerter/src/internal/database/queries.go alerter/src/internal/database/alert_queries.go alerter/src/internal/database/anomaly_queries.go alerter/src/internal/database/metric_queries.go alerter/src/internal/database/metric_registry.go
wc -l server/src/internal/database/datastore.go server/src/internal/database/connection_queries.go server/src/internal/database/cluster_queries.go server/src/internal/database/topology_queries.go server/src/internal/database/alert_queries.go server/src/internal/database/estate_queries.go server/src/internal/database/relationship_queries.go
```

- [ ] **Step 3: Verify gofmt**

```bash
gofmt -l collector/src/probes/*.go alerter/src/internal/database/*.go server/src/internal/database/*.go
```

Output should be empty (all files already formatted).
