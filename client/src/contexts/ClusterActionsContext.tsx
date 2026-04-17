/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
/* eslint-disable react-refresh/only-export-components */

import React, { createContext, useContext, useCallback, useMemo } from 'react';
import { useAuth } from './AuthContext';
import { useClusterData } from './ClusterDataContext';
import { useClusterSelection } from './ClusterSelectionContext';
import { apiGet, apiPost, apiPut, apiDelete } from '../utils/apiClient';

export interface ServerData {
    name?: string;
    host?: string;
    port?: number;
    [key: string]: unknown;
}

export interface GroupData {
    name: string;
    [key: string]: unknown;
}

export interface ClusterActionsContextValue {
    updateGroupName: (groupId: string, newName: string) => Promise<void>;
    updateClusterName: (clusterId: string, newName: string, groupId: string, autoClusterKey?: string) => Promise<void>;
    updateServerName: (serverId: number, newName: string) => Promise<void>;
    getServer: (serverId: number) => Promise<unknown>;
    createServer: (serverData: ServerData) => Promise<unknown>;
    updateServer: (serverId: number, serverData: ServerData) => Promise<unknown>;
    deleteServer: (serverId: number) => Promise<void>;
    deleteCluster: (clusterId: string) => Promise<void>;
    createGroup: (groupData: GroupData) => Promise<unknown>;
    deleteGroup: (groupId: string | number) => Promise<void>;
    moveClusterToGroup: (clusterId: string, targetGroupId: string | null, autoClusterKey?: string, clusterName?: string) => Promise<void>;
}

interface ClusterActionsProviderProps {
    children: React.ReactNode;
}

const ClusterActionsContext = createContext<ClusterActionsContextValue | null>(null);

export const ClusterActionsProvider = ({ children }: ClusterActionsProviderProps): React.ReactElement => {
    const { user } = useAuth();
    const { fetchClusterData } = useClusterData();
    const { selectedServer, clearSelection } = useClusterSelection();

    /**
     * Update a cluster group's name
     * Handles both database-backed groups (group-{id}) and
     * auto-detected groups (group-auto)
     */
    const updateGroupName = useCallback(async (groupId: string, newName: string): Promise<void> => {
        if (!user) {throw new Error('Not authenticated');}

        const groupIdStr = groupId.toString();

        // Check if it's an auto-detected group (group-auto)
        const isAutoDetected = /^group-auto/.test(groupIdStr);

        if (isAutoDetected) {
            // Auto-detected groups: send the group ID as-is
            await apiPut(`/api/v1/cluster-groups/${groupIdStr}`, { name: newName });
        } else {
            // Extract suffix after "group-" prefix
            const suffix = groupIdStr.replace('group-', '');

            // Use numeric ID for database-backed groups, full ID for named groups
            const groupIdentifier = /^\d+$/.test(suffix)
                ? parseInt(suffix, 10)
                : groupIdStr;

            await apiPut(`/api/v1/cluster-groups/${groupIdentifier}`, { name: newName });
        }

        // Refresh cluster data to reflect the change
        await fetchClusterData();
    }, [user, fetchClusterData]);

    /**
     * Update a cluster's name
     * Handles both database-backed clusters (cluster-{id}) and
     * auto-detected clusters (server-{id}, cluster-spock-{prefix})
     */
    const updateClusterName = useCallback(async (clusterId: string, newName: string, groupId: string, autoClusterKey?: string): Promise<void> => {
        if (!user) {throw new Error('Not authenticated');}

        const clusterIdStr = clusterId.toString();

        // Check if it's an auto-detected cluster
        const isAutoDetected = /^(server-\d+|cluster-spock-.+)$/.test(clusterIdStr);

        if (isAutoDetected) {
            // Auto-detected clusters: send the cluster ID as-is and include auto_cluster_key
            const body: Record<string, unknown> = { name: newName };
            if (autoClusterKey) {
                body.auto_cluster_key = autoClusterKey;
            }

            await apiPut(`/api/v1/clusters/${clusterIdStr}`, body);
        } else {
            // Database-backed clusters: extract numeric IDs
            const numericId = parseInt(clusterIdStr.replace('cluster-', ''), 10);
            if (isNaN(numericId)) {
                throw new Error('Invalid cluster ID');
            }

            // Extract numeric group ID
            const numericGroupId = parseInt(groupId.toString().replace('group-', ''), 10);
            if (isNaN(numericGroupId)) {
                throw new Error('Invalid group ID');
            }

            await apiPut(`/api/v1/clusters/${numericId}`, { name: newName, group_id: numericGroupId });
        }

        // Refresh cluster data to reflect the change
        await fetchClusterData();
    }, [user, fetchClusterData]);

    /**
     * Update a server's (connection's) name
     */
    const updateServerName = useCallback(async (serverId: number, newName: string): Promise<void> => {
        if (!user) {throw new Error('Not authenticated');}

        await apiPut(`/api/v1/connections/${serverId}`, { name: newName });

        // Refresh cluster data to reflect the change
        await fetchClusterData();
    }, [user, fetchClusterData]);

    /**
     * Get full server (connection) details for editing
     */
    const getServer = useCallback(async (serverId: number): Promise<unknown> => {
        if (!user) {throw new Error('Not authenticated');}

        return await apiGet(`/api/v1/connections/${serverId}`);
    }, [user]);

    /**
     * Create a new server (connection)
     */
    const createServer = useCallback(async (serverData: ServerData): Promise<unknown> => {
        if (!user) {throw new Error('Not authenticated');}

        const result = await apiPost('/api/v1/connections', serverData);

        await fetchClusterData();
        return result;
    }, [user, fetchClusterData]);

    /**
     * Update an existing server (connection)
     */
    const updateServer = useCallback(async (serverId: number, serverData: ServerData): Promise<unknown> => {
        if (!user) {throw new Error('Not authenticated');}

        const result = await apiPut(`/api/v1/connections/${serverId}`, serverData);

        await fetchClusterData();
        return result;
    }, [user, fetchClusterData]);

    /**
     * Delete a server (connection)
     */
    const deleteServer = useCallback(async (serverId: number): Promise<void> => {
        if (!user) {throw new Error('Not authenticated');}

        await apiDelete(`/api/v1/connections/${serverId}`);

        // Clear selection if deleted server was selected
        if (selectedServer?.id === serverId) {
            clearSelection();
        }

        await fetchClusterData();
    }, [user, fetchClusterData, selectedServer, clearSelection]);

    /**
     * Delete a cluster (database-backed clusters only)
     */
    const deleteCluster = useCallback(async (clusterId: string): Promise<void> => {
        if (!user) {throw new Error('Not authenticated');}

        await apiDelete(`/api/v1/clusters/${clusterId}`);

        await fetchClusterData();
    }, [user, fetchClusterData]);

    /**
     * Create a new cluster group
     */
    const createGroup = useCallback(async (groupData: GroupData): Promise<unknown> => {
        if (!user) {throw new Error('Not authenticated');}

        const result = await apiPost('/api/v1/cluster-groups', groupData);

        await fetchClusterData();
        return result;
    }, [user, fetchClusterData]);

    /**
     * Delete a cluster group
     */
    const deleteGroup = useCallback(async (groupId: string | number): Promise<void> => {
        if (!user) {throw new Error('Not authenticated');}

        // Extract numeric ID from group-{id} format if needed
        const numericId = typeof groupId === 'string' && groupId.startsWith('group-')
            ? parseInt(groupId.replace('group-', ''), 10)
            : groupId;

        await apiDelete(`/api/v1/cluster-groups/${numericId}`);

        await fetchClusterData();
    }, [user, fetchClusterData]);

    /**
     * Move a cluster to a different group
     * Supports both database-backed clusters and auto-detected clusters
     */
    const moveClusterToGroup = useCallback(async (clusterId: string, targetGroupId: string | null, autoClusterKey?: string, clusterName?: string): Promise<void> => {
        if (!user) {throw new Error('Not authenticated');}

        const clusterIdStr = clusterId.toString();

        // Extract the target group's numeric ID from the group ID string (e.g., "group-123")
        let numericGroupId: number | null = null;
        if (targetGroupId) {
            const groupIdStr = targetGroupId.toString();
            const match = groupIdStr.match(/^group-(\d+)$/);
            if (match) {
                numericGroupId = parseInt(match[1], 10);
            }
        }

        // Build request body
        const body: Record<string, unknown> = { group_id: numericGroupId };
        if (autoClusterKey) {
            body.auto_cluster_key = autoClusterKey;
        }
        // Include name for creating new cluster records during move
        if (clusterName) {
            body.name = clusterName;
        }

        await apiPut(`/api/v1/clusters/${clusterIdStr}`, body);

        await fetchClusterData();
    }, [user, fetchClusterData]);

    const value: ClusterActionsContextValue = useMemo(() => ({
        // Update functions
        updateGroupName,
        updateClusterName,
        updateServerName,
        // CRUD functions
        getServer,
        createServer,
        updateServer,
        deleteServer,
        deleteCluster,
        createGroup,
        deleteGroup,
        moveClusterToGroup,
    }), [
        updateGroupName,
        updateClusterName,
        updateServerName,
        getServer,
        createServer,
        updateServer,
        deleteServer,
        deleteCluster,
        createGroup,
        deleteGroup,
        moveClusterToGroup,
    ]);

    return (
        <ClusterActionsContext.Provider value={value}>
            {children}
        </ClusterActionsContext.Provider>
    );
};

export const useClusterActions = (): ClusterActionsContextValue => {
    const context = useContext(ClusterActionsContext);
    if (!context) {
        throw new Error('useClusterActions must be used within a ClusterActionsProvider');
    }
    return context;
};

export default ClusterActionsContext;
