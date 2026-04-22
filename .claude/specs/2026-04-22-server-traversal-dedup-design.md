# Server Traversal Deduplication

## Summary

Consolidate six duplicated server/selection traversal functions
into the existing `client/src/utils/clusterHelpers.ts` utility
module. This is the first of several PRs addressing issue #80
(React client structural improvements).

## Problem

Six functions across five files duplicate the same traversal
pattern: walking `selection.groups[].clusters[].servers[]` with
recursive descent into `server.children[]`. Each copy uses
`Record<string, unknown>` with unsafe type assertions and
reimplements the same iteration logic with minor variations.

### Duplicated Functions

| Function | File | Purpose |
|----------|------|---------|
| `extractAllServerIds` | `EstateDashboard/index.tsx:23-45` | Collect all server IDs from estate selection |
| `extractEstateServerIds` | `usePerformanceSummary.ts:27-49` | Identical to extractAllServerIds |
| `extractServerIds` | `ClusterDashboard/index.tsx:23-40` | Collect server IDs from cluster selection |
| `computeServerCounts` | `HealthOverviewSection.tsx:62-90` | Count servers by status category |
| `countServers` | `KpiTilesSection.tsx:50-70` | Count total servers |
| `collectAllServers` | `ClusterCardsSection.tsx:123-132` | Flatten server hierarchy to array |

### Existing Centralized Functions

The `clusterHelpers.ts` file already provides:

- `collectServers<T extends ServerLike>(servers: T[]): T[]` -
  recursive server flattening.
- `countServersRecursive(servers, filterFn)` - count with filter.
- `collectExpandableServerIds(servers)` - expandable server IDs.
- `filterServersRecursive(servers, query)` - search filter.

These operate at the server-list level (below clusters). The
missing piece is the estate-level iteration over groups and
clusters.

## Design

### New Functions in `clusterHelpers.ts`

Add five exported functions that handle estate-level and
cluster-level traversal. Each composes the existing
`collectServers()` for recursive server/children flattening,
then wraps it with the groups/clusters iteration.

#### `extractEstateServerIds`

```typescript
export const extractEstateServerIds = (
    selection: Record<string, unknown>
): number[] => {
    const ids: number[] = [];
    const groups = selection.groups as
        Array<Record<string, unknown>> | undefined;

    groups?.forEach(group => {
        const clusters = group.clusters as
            Array<Record<string, unknown>> | undefined;
        clusters?.forEach(cluster => {
            const servers = cluster.servers as
                ServerLike[] | undefined;
            if (servers) {
                collectServers(servers).forEach(s => {
                    ids.push(s.id);
                });
            }
        });
    });

    return ids;
};
```

Replaces `extractAllServerIds` and the local
`extractEstateServerIds` in `usePerformanceSummary.ts`.

#### `extractClusterServerIds`

```typescript
export const extractClusterServerIds = (
    selection: Record<string, unknown>
): number[] => {
    const servers = selection.servers as
        ServerLike[] | undefined;
    if (!servers) { return []; }
    return collectServers(servers).map(s => s.id);
};
```

Replaces `extractServerIds` in `ClusterDashboard/index.tsx`.

#### `computeEstateServerCounts`

```typescript
export interface ServerCounts {
    online: number;
    warning: number;
    offline: number;
}

export const computeEstateServerCounts = (
    selection: Record<string, unknown>
): ServerCounts => {
    const counts: ServerCounts = {
        online: 0, warning: 0, offline: 0,
    };
    const groups = selection.groups as
        Array<Record<string, unknown>> | undefined;

    groups?.forEach(group => {
        const clusters = group.clusters as
            Array<Record<string, unknown>> | undefined;
        clusters?.forEach(cluster => {
            const servers = cluster.servers as
                ServerLike[] | undefined;
            if (servers) {
                collectServers(servers).forEach(s => {
                    const rec = s as Record<string, unknown>;
                    const status = rec.status as string;
                    const alertCount =
                        rec.active_alert_count as
                            number | undefined;
                    if (status === 'offline') {
                        counts.offline += 1;
                    } else if (alertCount && alertCount > 0) {
                        counts.warning += 1;
                    } else {
                        counts.online += 1;
                    }
                });
            }
        });
    });

    return counts;
};
```

Replaces `computeServerCounts` in
`HealthOverviewSection.tsx`.

#### `countEstateServers`

```typescript
export const countEstateServers = (
    selection: Record<string, unknown>
): number => {
    const groups = selection.groups as
        Array<Record<string, unknown>> | undefined;
    let count = 0;

    groups?.forEach(group => {
        const clusters = group.clusters as
            Array<Record<string, unknown>> | undefined;
        clusters?.forEach(cluster => {
            const servers = cluster.servers as
                ServerLike[] | undefined;
            if (servers) {
                count += collectServers(servers).length;
            }
        });
    });

    return count;
};
```

Replaces `countServers` in `KpiTilesSection.tsx`.

#### `collectAllEstateServers`

```typescript
export const collectAllEstateServers = (
    selection: Record<string, unknown>
): Array<Record<string, unknown>> => {
    const result: Array<Record<string, unknown>> = [];
    const groups = selection.groups as
        Array<Record<string, unknown>> | undefined;

    groups?.forEach(group => {
        const clusters = group.clusters as
            Array<Record<string, unknown>> | undefined;
        clusters?.forEach(cluster => {
            const servers = cluster.servers as
                ServerLike[] | undefined;
            if (servers) {
                result.push(
                    ...collectServers(servers) as
                        Array<Record<string, unknown>>
                );
            }
        });
    });

    return result;
};
```

Replaces `collectAllServers` in
`ClusterCardsSection.tsx` where the caller also needs to
iterate groups/clusters.

### Files Modified

| File | Change |
|------|--------|
| `utils/clusterHelpers.ts` | Add 5 new functions + `ServerCounts` interface |
| `Dashboard/EstateDashboard/index.tsx` | Replace local `extractAllServerIds` with imported `extractEstateServerIds` |
| `Dashboard/ClusterDashboard/index.tsx` | Replace local `extractServerIds` with imported `extractClusterServerIds` |
| `Dashboard/EstateDashboard/HealthOverviewSection.tsx` | Replace local `computeServerCounts` with imported `computeEstateServerCounts`; remove local `ServerCounts` interface |
| `Dashboard/EstateDashboard/KpiTilesSection.tsx` | Replace local `countServers` with imported `countEstateServers` |
| `Dashboard/EstateDashboard/ClusterCardsSection.tsx` | Evaluate whether local `collectAllServers` can use the new utility |
| `StatusPanel/PerformanceTiles/usePerformanceSummary.ts` | Replace local `extractEstateServerIds` with imported version |

### What Does Not Change

- The `Record<string, unknown>` type signatures remain as-is;
  type safety improvements are a separate PR.
- Existing functions in `clusterHelpers.ts` are untouched.
- No component behavior changes; this is a pure refactor.

## Testing

- Add unit tests in `utils/__tests__/clusterHelpers.test.ts`
  for each new function.
- Test with nested server hierarchies (parent servers with
  children).
- Test edge cases: empty groups, empty clusters, empty
  servers, undefined values, zero-length arrays.
- Verify 90% line coverage on `clusterHelpers.ts`.
- Existing component tests must continue to pass unchanged.
- Run `make test-all` before completing.

## Out of Scope

- Typed `Selection` discriminated union (future PR).
- Analysis dialog extraction (future PR).
- Oversized component decomposition (future PR).
- `console.error` centralization (future PR).
