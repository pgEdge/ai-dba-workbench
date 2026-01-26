/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Cluster Context
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Context for managing cluster groups, clusters, and server selection
 *
 *-------------------------------------------------------------------------
 */

import React, { createContext, useContext, useState, useCallback, useEffect, useRef } from 'react';
import { useAuth } from './AuthContext';

const ClusterContext = createContext(null);

/**
 * Generate a fingerprint for cluster data to detect actual changes.
 * This allows us to skip re-renders when the data hasn't changed.
 */
const generateDataFingerprint = (data) => {
    if (!data || data.length === 0) return '';

    // Create a fingerprint that captures the essential structure and values
    const fingerprint = data.map(group => {
        const clusterFingerprints = (group.clusters || []).map(cluster => {
            const serverFingerprints = collectServerFingerprints(cluster.servers || []);
            return `${cluster.id}:${cluster.name}:${serverFingerprints}`;
        }).join('|');
        return `${group.id}:${group.name}:${clusterFingerprints}`;
    }).join('||');

    return fingerprint;
};

/**
 * Recursively collect server fingerprints including nested children
 */
const collectServerFingerprints = (servers) => {
    if (!servers || servers.length === 0) return '';
    return servers.map(server => {
        const childFingerprints = collectServerFingerprints(server.children);
        return `${server.id}:${server.name}:${server.status}:${server.primary_role || server.role || ''}${childFingerprints ? ':' + childFingerprints : ''}`;
    }).join(',');
};

/**
 * Transform flat connections list into hierarchical structure
 * This is a temporary solution until the API supports proper hierarchy
 */
const transformConnectionsToHierarchy = (connections) => {
    // Group connections by cluster_group and cluster
    const groups = new Map();

    connections.forEach((conn) => {
        const groupName = conn.cluster_group || 'Ungrouped';
        const clusterName = conn.cluster_name || null;

        if (!groups.has(groupName)) {
            groups.set(groupName, {
                id: `group-${groupName}`,
                name: groupName,
                clusters: new Map(),
            });
        }

        const group = groups.get(groupName);

        if (clusterName) {
            // Server belongs to a cluster
            if (!group.clusters.has(clusterName)) {
                group.clusters.set(clusterName, {
                    id: `cluster-${clusterName}`,
                    name: clusterName,
                    servers: [],
                });
            }
            group.clusters.get(clusterName).servers.push({
                id: conn.id,
                name: conn.name,
                host: conn.host,
                port: conn.port,
                status: conn.status || 'unknown',
                role: conn.role || null,
                version: conn.version || null,
            });
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

export const ClusterProvider = ({ children }) => {
    const { user } = useAuth();
    const [clusterData, setClusterData] = useState([]);
    const [selectedServer, setSelectedServer] = useState(null);
    const [selectedCluster, setSelectedCluster] = useState(null);
    const [selectionType, setSelectionType] = useState(null); // 'server', 'cluster', or 'estate'
    const [currentConnection, setCurrentConnection] = useState(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);
    const [autoRefreshEnabled, setAutoRefreshEnabled] = useState(true);
    const [lastRefresh, setLastRefresh] = useState(null);
    const autoRefreshInterval = 30000; // 30 seconds

    // Track data fingerprint to detect actual changes
    const dataFingerprintRef = useRef('');
    // Track if this is the initial load (to show loading state)
    const isInitialLoadRef = useRef(true);

    /**
     * Fetch cluster hierarchy from the API.
     * Uses fingerprinting to detect actual changes and avoid unnecessary re-renders.
     * Only shows loading state on initial load, not during auto-refresh.
     */
    const fetchClusterData = useCallback(async () => {
        if (!user) return;

        // Only show loading spinner on initial load
        if (isInitialLoadRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            // First, try to get the hierarchical cluster data
            // If the API doesn't support it yet, fall back to connections
            let response = await fetch('/api/v1/clusters', {
                credentials: 'include',
                headers: {
                    'Content-Type': 'application/json',
                },
            });

            let newData = null;

            if (response.ok) {
                newData = await response.json();
            } else if (response.status === 404) {
                // Fall back to connections endpoint
                response = await fetch('/api/v1/connections', {
                    credentials: 'include',
                    headers: {
                        'Content-Type': 'application/json',
                    },
                });

                if (response.ok) {
                    const connections = await response.json();
                    newData = transformConnectionsToHierarchy(connections);
                } else {
                    throw new Error('Failed to fetch connections');
                }
            } else {
                throw new Error('Failed to fetch cluster data');
            }

            // Generate fingerprint for the new data
            const newFingerprint = generateDataFingerprint(newData);

            // Only update state if data has actually changed
            if (newFingerprint !== dataFingerprintRef.current) {
                dataFingerprintRef.current = newFingerprint;
                setClusterData(newData);
            }

            // Always update last refresh time
            setLastRefresh(new Date());
        } catch (err) {
            console.error('Error fetching cluster data:', err);
            setError(err.message);
        } finally {
            if (isInitialLoadRef.current) {
                isInitialLoadRef.current = false;
            }
            setLoading(false);
        }
    }, [user]);

    /**
     * Select a server and set it as the current connection
     */
    const selectServer = useCallback(async (server) => {
        if (!user || !server) return;

        setSelectedServer(server);
        setSelectedCluster(null);
        setSelectionType('server');

        try {
            const response = await fetch('/api/v1/connections/current', {
                method: 'POST',
                credentials: 'include',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    connection_id: server.id,
                }),
            });

            if (response.ok) {
                const data = await response.json();
                setCurrentConnection(data);
            } else {
                console.error('Failed to set current connection');
            }
        } catch (err) {
            console.error('Error setting current connection:', err);
        }
    }, [user]);

    /**
     * Select a cluster (all servers in the cluster)
     */
    const selectCluster = useCallback((cluster) => {
        setSelectedCluster(cluster);
        setSelectedServer(null);
        setCurrentConnection(null);
        setSelectionType('cluster');
    }, []);

    /**
     * Select the entire estate (all servers across all groups)
     */
    const selectEstate = useCallback(() => {
        setSelectedServer(null);
        setSelectedCluster(null);
        setCurrentConnection(null);
        setSelectionType('estate');
    }, []);

    /**
     * Clear the current selection
     */
    const clearSelection = useCallback(async () => {
        if (!user) return;

        setSelectedServer(null);
        setSelectedCluster(null);
        setSelectionType(null);

        try {
            await fetch('/api/v1/connections/current', {
                method: 'DELETE',
                credentials: 'include',
            });
            setCurrentConnection(null);
        } catch (err) {
            console.error('Error clearing current connection:', err);
        }
    }, [user]);

    /**
     * Get the current connection from the server on initial load
     */
    const fetchCurrentConnection = useCallback(async () => {
        if (!user) return;

        try {
            const response = await fetch('/api/v1/connections/current', {
                credentials: 'include',
            });

            if (response.ok) {
                const data = await response.json();
                setCurrentConnection(data);
                // Find and set the selected server from cluster data
                for (const group of clusterData) {
                    for (const cluster of group.clusters || []) {
                        const server = cluster.servers?.find(s => s.id === data.connection_id);
                        if (server) {
                            setSelectedServer(server);
                            return;
                        }
                    }
                }
            }
        } catch (err) {
            // Ignore errors - current connection might not be set
        }
    }, [user, clusterData]);

    // Fetch cluster data when user changes
    useEffect(() => {
        if (user) {
            fetchClusterData();
        } else {
            setClusterData([]);
            setSelectedServer(null);
            setCurrentConnection(null);
        }
    }, [user, fetchClusterData]);

    // Fetch current connection after cluster data is loaded
    useEffect(() => {
        if (clusterData.length > 0) {
            fetchCurrentConnection();
        }
    }, [clusterData, fetchCurrentConnection]);

    // Auto-refresh effect
    useEffect(() => {
        if (!autoRefreshEnabled || !user) return;

        const intervalId = setInterval(() => {
            fetchClusterData();
        }, autoRefreshInterval);

        return () => clearInterval(intervalId);
    }, [autoRefreshEnabled, user, fetchClusterData]);

    /**
     * Update a cluster group's name
     * Handles both database-backed groups (group-{id}) and
     * auto-detected groups (group-auto)
     */
    const updateGroupName = useCallback(async (groupId, newName) => {
        if (!user) throw new Error('Not authenticated');

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
                const error = await response.json();
                throw new Error(error.error || 'Failed to update group');
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
                const error = await response.json();
                throw new Error(error.error || 'Failed to update group');
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
    const updateClusterName = useCallback(async (clusterId, newName, groupId, autoClusterKey) => {
        if (!user) throw new Error('Not authenticated');

        const clusterIdStr = clusterId.toString();

        // Check if it's an auto-detected cluster
        const isAutoDetected = /^(server-\d+|cluster-spock-.+)$/.test(clusterIdStr);

        if (isAutoDetected) {
            // Auto-detected clusters: send the cluster ID as-is and include auto_cluster_key
            const body = { name: newName };
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
                const error = await response.json();
                throw new Error(error.error || 'Failed to update cluster');
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
                const error = await response.json();
                throw new Error(error.error || 'Failed to update cluster');
            }
        }

        // Refresh cluster data to reflect the change
        await fetchClusterData();
    }, [user, fetchClusterData]);

    /**
     * Update a server's (connection's) name
     */
    const updateServerName = useCallback(async (serverId, newName) => {
        if (!user) throw new Error('Not authenticated');

        const response = await fetch(`/api/v1/connections/${serverId}`, {
            method: 'PUT',
            credentials: 'include',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ name: newName }),
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to update server');
        }

        // Refresh cluster data to reflect the change
        await fetchClusterData();
    }, [user, fetchClusterData]);

    /**
     * Get full server (connection) details for editing
     */
    const getServer = useCallback(async (serverId) => {
        if (!user) throw new Error('Not authenticated');

        const response = await fetch(`/api/v1/connections/${serverId}`, {
            method: 'GET',
            credentials: 'include',
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to get server details');
        }

        return await response.json();
    }, [user]);

    /**
     * Create a new server (connection)
     */
    const createServer = useCallback(async (serverData) => {
        if (!user) throw new Error('Not authenticated');

        const response = await fetch('/api/v1/connections', {
            method: 'POST',
            credentials: 'include',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(serverData),
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to create server');
        }

        await fetchClusterData();
        return await response.json();
    }, [user, fetchClusterData]);

    /**
     * Update an existing server (connection)
     */
    const updateServer = useCallback(async (serverId, serverData) => {
        if (!user) throw new Error('Not authenticated');

        const response = await fetch(`/api/v1/connections/${serverId}`, {
            method: 'PUT',
            credentials: 'include',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(serverData),
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to update server');
        }

        await fetchClusterData();
        return await response.json();
    }, [user, fetchClusterData]);

    /**
     * Delete a server (connection)
     */
    const deleteServer = useCallback(async (serverId) => {
        if (!user) throw new Error('Not authenticated');

        const response = await fetch(`/api/v1/connections/${serverId}`, {
            method: 'DELETE',
            credentials: 'include',
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to delete server');
        }

        // Clear selection if deleted server was selected
        if (selectedServer?.id === serverId) {
            setSelectedServer(null);
            setCurrentConnection(null);
        }

        await fetchClusterData();
    }, [user, fetchClusterData, selectedServer]);

    /**
     * Create a new cluster group
     */
    const createGroup = useCallback(async (groupData) => {
        if (!user) throw new Error('Not authenticated');

        const response = await fetch('/api/v1/cluster-groups', {
            method: 'POST',
            credentials: 'include',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(groupData),
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to create group');
        }

        await fetchClusterData();
        return await response.json();
    }, [user, fetchClusterData]);

    /**
     * Delete a cluster group
     */
    const deleteGroup = useCallback(async (groupId) => {
        if (!user) throw new Error('Not authenticated');

        // Extract numeric ID from group-{id} format if needed
        const numericId = typeof groupId === 'string' && groupId.startsWith('group-')
            ? parseInt(groupId.replace('group-', ''), 10)
            : groupId;

        const response = await fetch(`/api/v1/cluster-groups/${numericId}`, {
            method: 'DELETE',
            credentials: 'include',
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to delete group');
        }

        await fetchClusterData();
    }, [user, fetchClusterData]);

    /**
     * Move a cluster to a different group
     * Supports both database-backed clusters and auto-detected clusters
     */
    const moveClusterToGroup = useCallback(async (clusterId, targetGroupId, autoClusterKey, clusterName) => {
        if (!user) throw new Error('Not authenticated');

        const clusterIdStr = clusterId.toString();

        // Extract the target group's numeric ID from the group ID string (e.g., "group-123")
        let numericGroupId = null;
        if (targetGroupId) {
            const groupIdStr = targetGroupId.toString();
            const match = groupIdStr.match(/^group-(\d+)$/);
            if (match) {
                numericGroupId = parseInt(match[1], 10);
            }
        }

        // Build request body
        const body = { group_id: numericGroupId };
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
            const error = await response.json();
            throw new Error(error.error || 'Failed to move cluster');
        }

        await fetchClusterData();
    }, [user, fetchClusterData]);

    const value = {
        // Data
        clusterData,
        selectedServer,
        selectedCluster,
        selectionType,
        currentConnection,
        loading,
        error,
        // Data fetching
        fetchClusterData,
        // Selection functions
        selectServer,
        selectCluster,
        selectEstate,
        clearSelection,
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
        // Auto-refresh
        autoRefreshEnabled,
        setAutoRefreshEnabled,
        lastRefresh,
    };

    return (
        <ClusterContext.Provider value={value}>
            {children}
        </ClusterContext.Provider>
    );
};

export const useCluster = () => {
    const context = useContext(ClusterContext);
    if (!context) {
        throw new Error('useCluster must be used within a ClusterProvider');
    }
    return context;
};

export default ClusterContext;
