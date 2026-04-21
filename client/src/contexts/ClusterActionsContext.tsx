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
    deleteGroup: (groupId: string) => Promise<void>;
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
     * Update a cluster group's name.
     * Group ids always arrive as "group-{suffix}" strings, where the suffix
     * is either the numeric database id (e.g. "group-42") or the auto-
     * detected bucket key (e.g. "group-auto"). For database-backed groups
     * the server expects a bare numeric id, so strip the prefix; the auto
     * bucket is addressed by its full "group-auto..." form.
     */
    const updateGroupName = useCallback(async (groupId: string, newName: string): Promise<void> => {
        if (!user) {throw new Error('Not authenticated');}

        if (!/^group-/.test(groupId)) {
            throw new Error('Invalid group ID');
        }

        const isAutoDetected = /^group-auto/.test(groupId);
        const groupIdentifier = isAutoDetected
            ? groupId
            : groupId.slice('group-'.length);

        if (!isAutoDetected && !/^\d+$/.test(groupIdentifier)) {
            throw new Error('Invalid group ID');
        }

        await apiPut(`/api/v1/cluster-groups/${groupIdentifier}`, { name: newName });

        // Refresh cluster data to reflect the change
        await fetchClusterData();
    }, [user, fetchClusterData]);

    /**
     * Update a cluster's name.
     * Cluster ids arrive as either "cluster-{numeric}" (database-backed),
     * "server-{numeric}", or "cluster-spock-{prefix}" (auto-detected);
     * group ids always arrive as "group-{numeric}" strings.
     */
    const updateClusterName = useCallback(async (clusterId: string, newName: string, groupId: string, autoClusterKey?: string): Promise<void> => {
        if (!user) {throw new Error('Not authenticated');}

        // Auto-detected clusters: send the cluster ID as-is so the server
        // can match against server-{id} / cluster-spock-{prefix} shapes.
        if (/^(server-\d+|cluster-spock-.+)$/.test(clusterId)) {
            const body: Record<string, unknown> = { name: newName };
            if (autoClusterKey) {
                body.auto_cluster_key = autoClusterKey;
            }

            await apiPut(`/api/v1/clusters/${clusterId}`, body);
            await fetchClusterData();
            return;
        }

        // Database-backed clusters: extract numeric ids for both.
        const clusterMatch = /^cluster-(\d+)$/.exec(clusterId);
        if (!clusterMatch) {
            throw new Error('Invalid cluster ID');
        }

        const groupMatch = /^group-(\d+)$/.exec(groupId);
        if (!groupMatch) {
            throw new Error('Invalid group ID');
        }

        await apiPut(`/api/v1/clusters/${clusterMatch[1]}`, {
            name: newName,
            group_id: parseInt(groupMatch[1], 10),
        });

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
     * Delete a cluster group.
     * Group ids always arrive as "group-{numeric}" strings. Auto-detected
     * groups are not deletable (the UI hides the delete affordance), so a
     * bare numeric id is always the correct server-side addressing.
     */
    const deleteGroup = useCallback(async (groupId: string): Promise<void> => {
        if (!user) {throw new Error('Not authenticated');}

        const match = /^group-(\d+)$/.exec(groupId);
        if (!match) {
            throw new Error('Invalid group ID');
        }

        await apiDelete(`/api/v1/cluster-groups/${match[1]}`);

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
