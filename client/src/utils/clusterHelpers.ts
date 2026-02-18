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
