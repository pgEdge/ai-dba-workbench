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

import React, { createContext, useContext, useCallback } from 'react';
import { useAuth } from './AuthContext';
import { useClusterData } from './ClusterDataContext';
import { useClusterSelection } from './ClusterSelectionContext';

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
    createGroup: (groupData: GroupData) => Promise<unknown>;
    deleteGroup: (groupId: string | number) => Promise<void>;
    moveClusterToGroup: (clusterId: string, targetGroupId: string | null, autoClusterKey?: string, clusterName?: string) => Promise<void>;
}

interface ClusterActionsProviderProps {
    children: React.ReactNode;
}

interface ApiErrorResponse {
    error?: string;
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
            const response = await fetch(`/api/v1/cluster-groups/${groupIdStr}`, {
                method: 'PUT',
                credentials: 'include',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ name: newName }),
            });

            if (!response.ok) {
                const errorData: ApiErrorResponse = await response.json();
                throw new Error(errorData.error || 'Failed to update group');
            }
        } else {
            // Database-backed groups: extract numeric ID
            if (!/^group-\d+$/.test(groupIdStr)) {
                throw new Error('Invalid group ID format');
            }

            // Extract numeric ID from group ID format (e.g., "group-1" -> 1)
            const numericId = parseInt(groupIdStr.replace('group-', ''), 10);

            const response = await fetch(`/api/v1/cluster-groups/${numericId}`, {
                method: 'PUT',
                credentials: 'include',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ name: newName }),
            });

            if (!response.ok) {
                const errorData: ApiErrorResponse = await response.json();
                throw new Error(errorData.error || 'Failed to update group');
            }
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

            const response = await fetch(`/api/v1/clusters/${clusterIdStr}`, {
                method: 'PUT',
                credentials: 'include',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(body),
            });

            if (!response.ok) {
                const errorData: ApiErrorResponse = await response.json();
                throw new Error(errorData.error || 'Failed to update cluster');
            }
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

            const response = await fetch(`/api/v1/clusters/${numericId}`, {
                method: 'PUT',
                credentials: 'include',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ name: newName, group_id: numericGroupId }),
            });

            if (!response.ok) {
                const errorData: ApiErrorResponse = await response.json();
                throw new Error(errorData.error || 'Failed to update cluster');
            }
        }

        // Refresh cluster data to reflect the change
        await fetchClusterData();
    }, [user, fetchClusterData]);

    /**
     * Update a server's (connection's) name
     */
    const updateServerName = useCallback(async (serverId: number, newName: string): Promise<void> => {
        if (!user) {throw new Error('Not authenticated');}

        const response = await fetch(`/api/v1/connections/${serverId}`, {
            method: 'PUT',
            credentials: 'include',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ name: newName }),
        });

        if (!response.ok) {
            const errorData: ApiErrorResponse = await response.json();
            throw new Error(errorData.error || 'Failed to update server');
        }

        // Refresh cluster data to reflect the change
        await fetchClusterData();
    }, [user, fetchClusterData]);

    /**
     * Get full server (connection) details for editing
     */
    const getServer = useCallback(async (serverId: number): Promise<unknown> => {
        if (!user) {throw new Error('Not authenticated');}

        const response = await fetch(`/api/v1/connections/${serverId}`, {
            method: 'GET',
            credentials: 'include',
        });

        if (!response.ok) {
            const errorData: ApiErrorResponse = await response.json();
            throw new Error(errorData.error || 'Failed to get server details');
        }

        return await response.json();
    }, [user]);

    /**
     * Create a new server (connection)
     */
    const createServer = useCallback(async (serverData: ServerData): Promise<unknown> => {
        if (!user) {throw new Error('Not authenticated');}

        const response = await fetch('/api/v1/connections', {
            method: 'POST',
            credentials: 'include',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(serverData),
        });

        if (!response.ok) {
            const errorData: ApiErrorResponse = await response.json();
            throw new Error(errorData.error || 'Failed to create server');
        }

        await fetchClusterData();
        return await response.json();
    }, [user, fetchClusterData]);

    /**
     * Update an existing server (connection)
     */
    const updateServer = useCallback(async (serverId: number, serverData: ServerData): Promise<unknown> => {
        if (!user) {throw new Error('Not authenticated');}

        const response = await fetch(`/api/v1/connections/${serverId}`, {
            method: 'PUT',
            credentials: 'include',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(serverData),
        });

        if (!response.ok) {
            const errorData: ApiErrorResponse = await response.json();
            throw new Error(errorData.error || 'Failed to update server');
        }

        await fetchClusterData();
        return await response.json();
    }, [user, fetchClusterData]);

    /**
     * Delete a server (connection)
     */
    const deleteServer = useCallback(async (serverId: number): Promise<void> => {
        if (!user) {throw new Error('Not authenticated');}

        const response = await fetch(`/api/v1/connections/${serverId}`, {
            method: 'DELETE',
            credentials: 'include',
        });

        if (!response.ok) {
            const errorData: ApiErrorResponse = await response.json();
            throw new Error(errorData.error || 'Failed to delete server');
        }

        // Clear selection if deleted server was selected
        if (selectedServer?.id === serverId) {
            clearSelection();
        }

        await fetchClusterData();
    }, [user, fetchClusterData, selectedServer, clearSelection]);

    /**
     * Create a new cluster group
     */
    const createGroup = useCallback(async (groupData: GroupData): Promise<unknown> => {
        if (!user) {throw new Error('Not authenticated');}

        const response = await fetch('/api/v1/cluster-groups', {
            method: 'POST',
            credentials: 'include',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(groupData),
        });

        if (!response.ok) {
            const errorData: ApiErrorResponse = await response.json();
            throw new Error(errorData.error || 'Failed to create group');
        }

        await fetchClusterData();
        return await response.json();
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

        const response = await fetch(`/api/v1/cluster-groups/${numericId}`, {
            method: 'DELETE',
            credentials: 'include',
        });

        if (!response.ok) {
            const errorData: ApiErrorResponse = await response.json();
            throw new Error(errorData.error || 'Failed to delete group');
        }

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

        const response = await fetch(`/api/v1/clusters/${clusterIdStr}`, {
            method: 'PUT',
            credentials: 'include',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(body),
        });

        if (!response.ok) {
            const errorData: ApiErrorResponse = await response.json();
            throw new Error(errorData.error || 'Failed to move cluster');
        }

        await fetchClusterData();
    }, [user, fetchClusterData]);

    const value: ClusterActionsContextValue = {
        // Update functions
        updateGroupName,
        updateClusterName,
        updateServerName,
        // CRUD functions
        getServer,
        createServer,
        updateServer,
        deleteServer,
        createGroup,
        deleteGroup,
        moveClusterToGroup,
    };

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
