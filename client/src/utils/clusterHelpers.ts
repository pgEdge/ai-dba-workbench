/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

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

/**
 * Extract all server IDs from a cluster selection by walking
 * servers and their nested children.
 */
export const extractClusterServerIds = (
    selection: Record<string, unknown>
): number[] => {
    const servers = selection.servers as
        ServerLike[] | undefined;
    if (!servers) { return []; }
    return [...new Set(collectServers(servers).map(s => s.id))];
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
    selection: Record<string, unknown>
): ServerCounts => {
    const counts: ServerCounts = { online: 0, warning: 0, offline: 0 };
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
