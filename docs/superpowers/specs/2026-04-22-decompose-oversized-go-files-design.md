# Decompose Oversized Go Files — Design Spec

Issue: #75

## Problem

Three Go source files have grown too large for maintainability:

- `server/src/internal/database/datastore.go` — 5,668 lines
- `alerter/src/internal/database/queries.go` — 2,557 lines
- `collector/src/probes/base.go` — 886 lines

Each file mixes multiple domains. Duplicated scan patterns and
repetitive switch statements inflate line counts further.

## Acceptance Criteria

- No single Go source file exceeds ~1,000 lines.
- Duplicated scan patterns extracted into helper functions.
- Metric query switch statements refactored into a data-driven
  approach (metric registry).
- All tests pass after decomposition.
- No behavioral changes; pure refactoring.

## Design

### 1. Server `datastore.go` → 7 Files

All files remain in the `database` package. Methods keep their
`*Datastore` receiver. No import changes for callers.

| New File | Domain | Contents |
|---|---|---|
| `connection_queries.go` | Connection CRUD | `NewDatastore`, `Close`, `GetPool`, `GetAllConnections`, `GetConnection`, `GetConnectionWithPassword`, `GetConnectionSharingInfo`, `CreateConnection`, `UpdateConnectionFull`, `UpdateConnectionName`, `DeleteConnection`, `BuildConnectionString`, `ListDatabases`, `MonitoredConnection.MarshalJSON`, scan helpers |
| `cluster_queries.go` | Cluster/group CRUD | `GetClusterGroups`, `GetClusterGroup`, `CreateClusterGroup*`, `UpdateClusterGroup`, `DeleteClusterGroup`, `GetClustersInGroup`, `GetCluster`, `CreateCluster`, `UpdateCluster`, `UpdateClusterPartial`, `DeleteCluster`, `GetClusterOverrides`, `GetDefaultGroupID`, `Upsert*ByAutoKey`, group overrides, internal helpers |
| `topology_queries.go` | Topology building | `GetClusterTopology`, `RefreshClusterAssignments`, all auto-detection helpers, replication extractors, persisted member handlers, hierarchy builders, Spock/logical grouping, pruning, relationship population |
| `alert_queries.go` | Alert management | `GetAlerts`, `GetAlertCounts`, `GetAlertConnectionID`, `SaveAlertAnalysis`, `AcknowledgeAlert`, `UnacknowledgeAlert` |
| `estate_queries.go` | Estate snapshots | `GetEstateSnapshot`, `GetServerSnapshot`, `GetClusterSnapshot`, `GetGroupSnapshot`, `GetConnectionsSnapshot`, `GetConnectionContext`, scoped helpers, flatten helpers |
| `relationship_queries.go` | Node relationships | `GetClusterRelationships`, `SetNodeRelationships`, `SyncAutoDetectedRelationships`, `RemoveNodeRelationship`, `ClearNodeRelationships`, `AddServerToCluster`, `RemoveServerFromCluster`, `IsConnectionInCluster`, `CreateManualCluster`, `GetConnectionClusterInfo`, `ListClustersForAutocomplete`, `ResetMembershipSource` |
| `datastore.go` (remaining) | Types and structs | All type definitions, struct declarations, error variables, constants. Keeps file as the canonical "what's in this package" overview. |

#### Scan Helper Extraction

Extract these duplicated scan patterns into helper functions in
`connection_queries.go`:

- `scanConnectionListItem(scanner) error` — 11 fields, used 8 times.
- `scanFullConnection(scanner) error` — 18 fields, used 3 times.

The `scanner` parameter accepts `pgx.Row` (implements `Scan()`).

### 2. Alerter `queries.go` → 5 Files + Metric Registry

All files remain in the `database` package with `*Datastore` receiver.

| New File | Domain | Contents |
|---|---|---|
| `alert_queries.go` | Alert lifecycle | `GetActiveThresholdAlert`, `GetActiveAnomalyAlert`, `GetRecentlyClearedAlert`, `GetReevaluationSuppressedAlert`, `GetFalsePositiveSuppressedAlert`, `UpdateAlertValues`, `CreateAlert`, `GetActiveAlerts`, `GetAlert`, `ClearAlert`, `ReactivateAlert`, `GetAlertsByCluster`, `GetAlertsByConnection`, `UpdateAlertReevaluation`, scan helpers |
| `metric_queries.go` | Metric retrieval | `GetLatestMetricValues`, `GetLatestMetricValue`, `GetHistoricalMetricValues`, `queryMetricValues`, `queryMetricValuesWithDB`, `queryMetricValuesWithDBAndObject` |
| `metric_registry.go` | Metric config | `metricQueryConfig` struct, `scanType` enum, `metricRegistry` map |
| `anomaly_queries.go` | Anomaly/baseline | `CreateAnomalyCandidate`, `GetUnprocessedAnomalyCandidates`, `UpdateAnomalyCandidate`, `StoreAnomalyEmbedding`, `FindSimilarAnomalies`, `GetAnomalyCandidateByID`, `GetMetricBaselines`, `UpsertMetricBaseline`, `GetAcknowledgedAnomalyAlerts`, `GetAcknowledgmentHistoryForMetric`, `float32SliceToVectorString` |
| `queries.go` (remaining) | Connection/settings | `GetMonitoredConnectionErrors`, `GetActiveConnectionAlert`, `CreateConnectionAlert`, `UpdateConnectionAlertDescription`, `GetAlerterSettings`, `GetEnabledAlertRules`, `GetEffectiveThreshold`, `IsBlackoutActive`, `DeleteOldAlerts`, `DeleteOldAnomalyCandidates`, `GetProbeAvailability`, `GetEnabledBlackoutSchedules`, `CreateBlackout`, `GetActiveConnections`, `GetProbeStalenessByConnection`, `GetAlertRuleByName`, `GetClusterPeers` |

#### Scan Helper Extraction

- `scanAlert(scanner, *Alert) error` — 21 fields, used in 6 functions.
- `scanAcknowledgedAnomalyAlert(scanner, *AcknowledgedAnomalyAlert) error` — 15 fields, used in 2 functions.

#### Metric Registry Refactor

Replace the two massive switch statements with a data-driven
registry:

```go
type scanType int

const (
    scanBasic        scanType = iota // (connection_id, value, collected_at)
    scanWithDB                       // (connection_id, db_name, value, collected_at)
    scanWithDBObject                 // (connection_id, db_name, object_name, value, collected_at)
)

type metricQueryConfig struct {
    latestSQL     string
    historicalSQL string
    scan          scanType
}

var metricRegistry = map[string]metricQueryConfig{
    "pg_settings.max_connections": {
        latestSQL:     `SELECT ... ORDER BY collected_at DESC LIMIT 1`,
        historicalSQL: `SELECT ... WHERE collected_at >= NOW() - $1::interval`,
        scan:          scanBasic,
    },
    // ... one entry per metric
}
```

`GetLatestMetricValues` becomes:

```go
func (d *Datastore) GetLatestMetricValues(ctx context.Context, metricName string) ([]MetricValue, error) {
    cfg, ok := metricRegistry[metricName]
    if !ok {
        return nil, fmt.Errorf("unknown metric: %s", metricName)
    }
    switch cfg.scan {
    case scanBasic:
        return d.queryMetricValues(ctx, cfg.latestSQL)
    case scanWithDB:
        return d.queryMetricValuesWithDB(ctx, cfg.latestSQL)
    case scanWithDBObject:
        return d.queryMetricValuesWithDBAndObject(ctx, cfg.latestSQL)
    }
    return nil, fmt.Errorf("unknown scan type for metric: %s", metricName)
}
```

`GetHistoricalMetricValues` follows the same pattern using
`cfg.historicalSQL` and returning `[]HistoricalMetricValue`.

### 3. Collector `base.go` → 5 Files

All files remain in the `probes` package.

| New File | Domain | Contents |
|---|---|---|
| `base.go` (remaining) | Core interfaces | Package imports, `WrapQuery`, `featureCache`, `cachedCheck`, `ProbeConfig`, `MetricsProbe`, `ExtensionProbe`, `BaseMetricsProbe` + methods |
| `partition.go` | Partition mgmt | `weeklyPartitionBounds`, `partitionBoundLayout`, `EnsurePartition`, `DropExpiredPartitions`, `partitionCandidate`, `loadProtectedPartitions`, `loadPartitionCandidates`, `parsePartitionEnd` |
| `storage.go` | Metric storage | `StoreMetricsWithCopy` |
| `config_loader.go` | Config resolution | `LoadProbeConfigs`, `EnsureProbeConfig`, `getDefaultInterval`, `GetLastCollectionTime`, `CheckExtensionExists` |
| `hash.go` | Change detection | `ComputeMetricsHash`, `normalizeValue`, `getSortedKeys` |

## Test Strategy

This is a pure refactoring; no behavioral changes occur. Existing
tests remain in their current files and continue to pass. No new
tests are required for the file decomposition itself.

New tests are required only for the newly extracted helper functions:

- `scanAlert` and `scanAcknowledgedAnomalyAlert` — unit tests with
  mock scanners to verify field mapping.
- Metric registry — unit tests to verify every registered metric
  name resolves to a valid config and that both `GetLatestMetricValues`
  and `GetHistoricalMetricValues` return correct results for each
  registry entry.
- `scanConnectionListItem` and `scanFullConnection` — unit tests
  with mock scanners.

## Implementation Order

1. **Collector `base.go`** — smallest file, fewest dependencies,
   lowest risk. Validates the approach.
2. **Alerter `queries.go`** — medium complexity. Metric registry
   is the most significant behavioral refactor.
3. **Server `datastore.go`** — largest file, most functions. Pure
   mechanical decomposition after patterns are proven.

Each sub-project is verified independently with `make test-all`
before proceeding to the next.

## Risks

- **Topology file exceeds 1,000 lines.** The topology domain has
  50+ tightly coupled functions. If the file exceeds the limit after
  extraction, split further into `topology_autodetect.go` and
  `topology_helpers.go`.
- **Merge conflicts.** This is a large refactoring across many files.
  Work in a dedicated branch and merge promptly.
- **Metric registry correctness.** Each SQL query must be preserved
  exactly. The existing integration tests validate this, but careful
  review is needed during the registry migration.
