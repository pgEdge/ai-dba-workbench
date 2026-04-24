/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { ClusterServer, ClusterGroup } from './ClusterDataContext';

/**
 * Connection record from the /api/v1/connections endpoint
 */
export interface ConnectionRecord {
    id: number;
    name: string;
    description?: string;
    host: string;
    port: number;
    status?: string;
    role?: string | null;
    version?: string | null;
    cluster_group?: string;
    cluster_name?: string;
}

/**
 * Internal structure for building group hierarchies
 */
interface GroupBuildEntry {
    id: string;
    name: string;
    clusters: Map<string, {
        id: string;
        name: string;
        servers: ClusterServer[];
        isStandalone?: boolean;
    }>;
}

/**
 * Recursively collect server fingerprints including nested children
 */
export const collectServerFingerprints = (servers: ClusterServer[]): string => {
    if (!servers || servers.length === 0) {return '';}
    return servers.map(server => {
        const childFingerprints = collectServerFingerprints(server.children ?? []);
        return `${server.id}:${server.name}:${server.description || ''}:${server.status}:${server.connection_error || ''}:${server.primary_role || server.role || ''}:${server.membership_source || ''}${childFingerprints ? `:${childFingerprints}` : ''}`;
    }).join(',');
};

/**
 * Generate a fingerprint for cluster data to detect actual changes.
 * This allows us to skip re-renders when the data hasn't changed.
 */
export const generateDataFingerprint = (data: ClusterGroup[]): string => {
    if (!data || data.length === 0) {return '';}

    // Create a fingerprint that captures the essential structure and values
    const fingerprint = data.map(group => {
        const clusterFingerprints = (group.clusters || []).map(cluster => {
            const serverFingerprints = collectServerFingerprints(cluster.servers || []);
            return `${cluster.id}:${cluster.name}:${cluster.description || ''}:${cluster.replication_type || ''}:${serverFingerprints}`;
        }).join('|');
        return `${group.id}:${group.name}:${clusterFingerprints}`;
    }).join('||');

    return fingerprint;
};

/**
 * Transform flat connections list into hierarchical structure
 * This is a temporary solution until the API supports proper hierarchy
 */
export const transformConnectionsToHierarchy = (connections: ConnectionRecord[]): ClusterGroup[] => {
    // Group connections by cluster_group and cluster
    const groups = new Map<string, GroupBuildEntry>();

    connections.forEach((conn) => {
        const groupName = conn.cluster_group ?? 'Ungrouped';
        const clusterName = conn.cluster_name || null;

        if (!groups.has(groupName)) {
            groups.set(groupName, {
                id: `group-${groupName}`,
                name: groupName,
                clusters: new Map(),
            });
        }

        const group = groups.get(groupName)!;

        if (clusterName) {
            // Server belongs to a cluster
            if (!group.clusters.has(clusterName)) {
                group.clusters.set(clusterName, {
                    id: `cluster-${clusterName}`,
                    name: clusterName,
                    servers: [],
                });
            }
            const cluster = group.clusters.get(clusterName);
            if (cluster) {
                cluster.servers.push({
                    id: conn.id,
                    name: conn.name,
                    description: conn.description || '',
                    host: conn.host,
                    port: conn.port,
                    status: conn.status || 'unknown',
                    role: conn.role || null,
                    version: conn.version || null,
                });
            }
        } else {
            // Standalone server - create a "cluster" with just this server
            const standaloneClusterId = `standalone-${conn.id}`;
            group.clusters.set(standaloneClusterId, {
                id: standaloneClusterId,
                name: conn.name,
                isStandalone: true,
                servers: [{
                    id: conn.id,
                    name: conn.name,
                    description: conn.description || '',
                    host: conn.host,
                    port: conn.port,
                    status: conn.status || 'unknown',
                    role: null,
                    version: conn.version || null,
                }],
            });
        }
    });

    // Convert Maps to arrays
    return Array.from(groups.values()).map(group => ({
        ...group,
        clusters: Array.from(group.clusters.values()),
    }));
};
