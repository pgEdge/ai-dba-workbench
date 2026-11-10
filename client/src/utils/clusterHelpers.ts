/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { EstateSelection, ClusterSelection } from '../types/selection';
import type { ClusterEntry, ClusterGroup } from '../contexts/ClusterDataContext';

/**
 * Server-like object with optional nested children. This interface
 * is intentionally loose so the helper works with both the cluster
 * data context types and the ad-hoc selection objects used in
 * StatusPanel and App.
 */
export interface ServerLike {
    id: number;
    name: string;
    status?: string;
    children?: ServerLike[];
    [key: string]: unknown;
}

/**
 * Cluster entry as returned by GET /api/v1/clusters, reduced to the
 * fields needed to resolve its database row.
 */
export interface ClusterIdLike {
    id: string;
    db_cluster_id?: unknown;
}

const DB_BACKED_CLUSTER_ID = /^cluster-(\d+)$/;

/**
 * Resolve the numeric clusters.id behind a topology cluster entry.
 * Clusters loaded from the database use the ID `cluster-{id}`, whilst
 * auto-detected clusters carry a transient ID such as `server-{id}` or
 * `cluster-spock-{prefix}` and expose their row as `db_cluster_id`.
 * Returns undefined when the entry has no database row, for example a
 * standalone server. See issue #809.
 */
export const getDbClusterId = (
    cluster: ClusterIdLike | null | undefined,
): number | undefined => {
    if (!cluster) {return undefined;}
    const dbId = cluster.db_cluster_id;
    if (typeof dbId === 'number' && Number.isInteger(dbId) && dbId > 0) {
        return dbId;
    }
    const match = DB_BACKED_CLUSTER_ID.exec(cluster.id);
    return match ? parseInt(match[1], 10) : undefined;
};

/**
 * Recursively collect all servers from a tree of server nodes,
 * flattening any nested `children` arrays into a single list.
 */
export function collectServers<T extends ServerLike>(servers: T[]): T[] {
    const result: T[] = [];
    for (const server of servers) {
        result.push(server);
        if (server.children) {
            result.push(...collectServers(server.children as T[]));
        }
    }
    return result;
}

/**
 * Find the refreshed topology entry for a selected cluster. An
 * auto-detected cluster's topology ID can change between refreshes,
 * and after a switchover so can its database row, so match on the
 * topology ID first, then on the database row, and finally on any
 * cluster that still holds one of the selection's servers. Returns
 * undefined when the cluster has gone from the topology. See issue #809.
 */
export const findRefreshedCluster = (
    selected: ClusterEntry,
    clusterData: ClusterGroup[],
): ClusterEntry | undefined => {
    const clusters = clusterData.flatMap(group => group.clusters ?? []);

    const byTopologyId = clusters.find(c => c.id === selected.id);
    if (byTopologyId) {return byTopologyId;}

    const dbId = getDbClusterId(selected);
    const byDbId = dbId === undefined
        ? undefined
        : clusters.find(c => getDbClusterId(c) === dbId);
    if (byDbId) {return byDbId;}

    const serverIds = new Set(
        collectServers(selected.servers ?? []).map(s => s.id),
    );
    if (serverIds.size === 0) {return undefined;}
    return clusters.find(c =>
        collectServers(c.servers ?? []).some(s => serverIds.has(s.id)));
};

/**
 * Extract all server IDs from an estate selection by walking
 * groups → clusters → servers (including nested children).
 */
export const extractEstateServerIds = (
    selection: EstateSelection
): number[] => {
    const ids = new Set<number>();

    selection.groups.forEach(group => {
        group.clusters?.forEach(cluster => {
            if (cluster.servers) {
                collectServers(cluster.servers as ServerLike[]).forEach(s => {
                    ids.add(s.id);
                });
            }
        });
    });

    return [...ids];
};

/**
 * Extract all server IDs from a cluster selection by walking
 * servers and their nested children.
 */
export const extractClusterServerIds = (
    selection: ClusterSelection
): number[] => {
    if (!selection.servers) { return []; }
    return [...new Set(collectServers(selection.servers as ServerLike[]).map(s => s.id))];
};

/**
 * Server status category counts.
 */
export interface ServerCounts {
    online: number;
    warning: number;
    offline: number;
}

/**
 * Count servers by status category across an estate selection.
 * A server is "offline" if its status is "offline", "warning"
 * if it has active alerts, and "online" otherwise.
 */
export const computeEstateServerCounts = (
    selection: EstateSelection
): ServerCounts => {
    const counts: ServerCounts = { online: 0, warning: 0, offline: 0 };

    selection.groups.forEach(group => {
        group.clusters?.forEach(cluster => {
            if (cluster.servers) {
                collectServers(cluster.servers as ServerLike[]).forEach(s => {
                    const rec = s as Record<string, unknown>;
                    const status = rec.status as string;
                    const alertCount = rec.active_alert_count as
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

/**
 * Count all servers in an estate selection, including nested
 * children.
 */
export const countEstateServers = (
    selection: EstateSelection
): number => {
    let count = 0;

    selection.groups.forEach(group => {
        group.clusters?.forEach(cluster => {
            if (cluster.servers) {
                count += collectServers(cluster.servers as ServerLike[]).length;
            }
        });
    });

    return count;
};
