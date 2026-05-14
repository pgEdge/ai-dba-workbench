/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type {
    ClusterEntry,
    ClusterGroup,
    ClusterServer,
} from '../contexts/ClusterDataContext';
import type { Selection } from '../types/selection';
import { collectServers } from './clusterHelpers';

/**
 * Selection type discriminator used by `useCluster`. Mirrors the
 * runtime values produced by ClusterSelectionContext; kept local
 * to avoid a circular import between contexts and utils.
 */
export type SelectionType = 'server' | 'cluster' | 'estate' | null;

/**
 * Build the canonical `Selection` object consumed by `BlackoutProvider`,
 * `StatusPanel`, and downstream UI from the current selection state.
 *
 * The helper is defensive about `group.clusters` being nullish. The
 * server's GET /api/v1/clusters response can return a group with a
 * JSON `null` `clusters` field when a group has been emptied (for
 * example, just after the user deletes its sole cluster). The
 * server is being hardened to always emit `[]`, but the client must
 * tolerate `null` regardless; otherwise the resulting TypeError
 * crashes the MainLayout into the ErrorBoundary fallback. See
 * issue #242.
 */
export const buildSelection = (
    selectionType: SelectionType,
    selectedServer: ClusterServer | null,
    selectedCluster: ClusterEntry | null,
    clusterData: ClusterGroup[],
): Selection | null => {
    if (selectionType === 'server' && selectedServer) {
        let clusterId: string | undefined;
        let clusterName: string | undefined;
        let isStandalone: boolean | undefined;
        let groupId: string | undefined;
        let groupName: string | undefined;

        for (const group of clusterData) {
            for (const cluster of group.clusters ?? []) {
                const allServers = collectServers(cluster.servers || []);
                if (allServers.some(s => s.id === selectedServer.id)) {
                    clusterId = cluster.id;
                    clusterName = cluster.name;
                    isStandalone = cluster.isStandalone;
                    groupId = group.id;
                    groupName = group.name;
                    break;
                }
            }
            if (groupId) {break;}
        }

        return {
            type: 'server',
            id: selectedServer.id,
            name: selectedServer.name,
            description: selectedServer.description ?? '',
            status: selectedServer.status || 'unknown',
            host: selectedServer.host ?? '',
            port: selectedServer.port || 0,
            role: selectedServer.role || '',
            version: selectedServer.version || '',
            database: selectedServer.database_name || selectedServer.database || '',
            username: selectedServer.username ?? '',
            os: selectedServer.os ?? '',
            platform: selectedServer.platform ?? '',
            spockNodeName: selectedServer.spock_node_name,
            spockVersion: selectedServer.spock_version,
            clusterId,
            clusterName,
            isStandalone,
            groupId,
            groupName,
        };
    }

    if (selectionType === 'cluster' && selectedCluster) {
        const servers = selectedCluster.servers
            ? collectServers(selectedCluster.servers)
            : [];
        const serverIds = servers.map(s => s.id);

        let groupId: string | undefined;
        let groupName: string | undefined;

        for (const group of clusterData) {
            if ((group.clusters ?? []).some(c => c.id === selectedCluster.id)) {
                groupId = group.id;
                groupName = group.name;
                break;
            }
        }

        return {
            type: 'cluster',
            id: selectedCluster.id,
            name: selectedCluster.name,
            description: selectedCluster.description ?? '',
            servers: servers,
            serverIds: serverIds,
            status: servers.every(s => s.status === 'offline') && servers.length > 0
                ? 'offline'
                : servers.some(s => s.status === 'offline' || s.status === 'warning')
                    ? 'warning'
                    : 'online',
            groupId,
            groupName,
        };
    }

    if (selectionType === 'estate') {
        return {
            type: 'estate',
            name: 'All Servers',
            groups: clusterData,
            status: 'online', // Will be calculated by StatusPanel
        };
    }

    return null;
};
