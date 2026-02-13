/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { ClusterType } from './constants';

export interface Server {
    id: number;
    name: string;
    host?: string;
    port?: number;
    status?: string;
    role?: string | null;
    primary_role?: string;
    version?: string | null;
    is_expandable?: boolean;
    children?: Server[];
    connection_error?: string;
}

export interface Cluster {
    id: string;
    name: string;
    description?: string;
    servers?: Server[];
    isStandalone?: boolean;
    auto_cluster_key?: string;
}

/**
 * Recursively collect all roles from servers and their children
 */
export const collectAllRoles = (servers: Server[]): string[] => {
    if (!servers || servers.length === 0) {return [];}
    const roles: string[] = [];
    const traverse = (serverList: Server[]): void => {
        serverList.forEach(s => {
            const role = s.primary_role || s.role;
            if (role) {roles.push(role);}
            if (s.children && s.children.length > 0) {traverse(s.children);}
        });
    };
    traverse(servers);
    return roles;
};

/**
 * Detect cluster type based on server roles (including children)
 * Returns: 'spock' | 'binary' | 'logical' | 'default'
 * Priority: spock > logical > binary (logical takes precedence since it's often combined with binary)
 */
export const getClusterType = (cluster: Cluster | null | undefined): ClusterType => {
    if (!cluster) {return 'default';}
    if (cluster.name?.toLowerCase().includes('spock')) {return 'spock';}
    if (!cluster.servers || cluster.servers.length === 0) {return 'default';}

    // Collect roles from all servers including nested children
    const roles = collectAllRoles(cluster.servers);
    if (roles.length === 0) {return 'default';}

    // Check for Spock (highest priority)
    if (roles.some(r => r === 'spock_node')) {return 'spock';}

    // Check for logical replication (takes precedence over binary)
    const logicalRoles = ['logical_publisher', 'logical_subscriber', 'logical_bidirectional'];
    if (roles.some(r => logicalRoles.includes(r))) {return 'logical';}

    // Check for binary replication
    const binaryRoles = ['binary_primary', 'binary_standby', 'binary_cascading'];
    if (roles.some(r => binaryRoles.includes(r))) {return 'binary';}

    return 'default';
};

/**
 * Compute effective role for display based on cluster context
 * In logical replication clusters, binary_primary should display as logical_publisher
 */
export const getEffectiveRole = (serverRole: string, clusterType: ClusterType): string => {
    if (clusterType === 'logical' && serverRole === 'binary_primary') {
        return 'logical_publisher';
    }
    return serverRole;
};

/**
 * Count all servers recursively (including children)
 */
export const countServersRecursive = (
    servers: Server[] | undefined,
    filterFn: (server: Server) => boolean = () => true
): number => {
    if (!servers) {return 0;}
    return servers.reduce((count, server) => {
        const current = filterFn(server) ? 1 : 0;
        const childCount = countServersRecursive(server.children, filterFn);
        return count + current + childCount;
    }, 0);
};

/**
 * Collect all expandable server IDs recursively
 */
export const collectExpandableServerIds = (servers: Server[] | undefined): number[] => {
    if (!servers) {return [];}
    return servers.flatMap(server => {
        const ids = server.is_expandable || (server.children && server.children.length > 0) ? [server.id] : [];
        return [...ids, ...collectExpandableServerIds(server.children)];
    });
};

/**
 * Recursively filter servers by search query, including children
 */
export const filterServersRecursive = (servers: Server[] | undefined, query: string): Server[] => {
    if (!servers) {return [];}

    return servers.reduce<Server[]>((result, server) => {
        const serverMatches =
            server.name.toLowerCase().includes(query) ||
            server.host?.toLowerCase().includes(query);

        // Recursively filter children
        const filteredChildren = filterServersRecursive(server.children, query);

        // Include server if it matches or has matching children
        if (serverMatches || filteredChildren.length > 0) {
            result.push({
                ...server,
                children: serverMatches ? server.children : filteredChildren,
            });
        }

        return result;
    }, []);
};

/**
 * Load a value from localStorage with JSON parsing
 */
export const loadFromStorage = <T>(key: string, defaultValue: T): T => {
    try {
        const stored = localStorage.getItem(key);
        if (stored === null) {return defaultValue;}
        return JSON.parse(stored) as T;
    } catch {
        return defaultValue;
    }
};

/**
 * Save a value to localStorage with JSON serialization
 */
export const saveToStorage = (key: string, value: unknown): void => {
    try {
        localStorage.setItem(key, JSON.stringify(value));
    } catch {
        // Ignore storage errors (quota exceeded, etc.)
    }
};

/**
 * Format relative time for display (e.g., "just now", "5m ago")
 */
export const formatRelativeTime = (date: Date | null | undefined): string => {
    if (!date) {return '';}
    const seconds = Math.floor((new Date().getTime() - date.getTime()) / 1000);
    if (seconds < 60) {return 'just now';}
    const minutes = Math.floor(seconds / 60);
    return `${minutes}m ago`;
};
