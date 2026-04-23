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
