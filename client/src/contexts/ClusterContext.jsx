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
    const { sessionToken: token } = useAuth();
    const [clusterData, setClusterData] = useState([]);
    const [selectedServer, setSelectedServer] = useState(null);
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
        if (!token) return;

        // Only show loading spinner on initial load
        if (isInitialLoadRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            // First, try to get the hierarchical cluster data
            // If the API doesn't support it yet, fall back to connections
            let response = await fetch('/api/clusters', {
                headers: {
                    'Authorization': `Bearer ${token}`,
                    'Content-Type': 'application/json',
                },
            });

            let newData = null;

            if (response.ok) {
                newData = await response.json();
            } else if (response.status === 404) {
                // Fall back to connections endpoint
                response = await fetch('/api/connections', {
                    headers: {
                        'Authorization': `Bearer ${token}`,
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
    }, [token]);

    /**
     * Select a server and set it as the current connection
     */
    const selectServer = useCallback(async (server) => {
        if (!token || !server) return;

        setSelectedServer(server);

        try {
            const response = await fetch('/api/connections/current', {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${token}`,
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
    }, [token]);

    /**
     * Clear the current server selection
     */
    const clearSelection = useCallback(async () => {
        if (!token) return;

        setSelectedServer(null);

        try {
            await fetch('/api/connections/current', {
                method: 'DELETE',
                headers: {
                    'Authorization': `Bearer ${token}`,
                },
            });
            setCurrentConnection(null);
        } catch (err) {
            console.error('Error clearing current connection:', err);
        }
    }, [token]);

    /**
     * Get the current connection from the server on initial load
     */
    const fetchCurrentConnection = useCallback(async () => {
        if (!token) return;

        try {
            const response = await fetch('/api/connections/current', {
                headers: {
                    'Authorization': `Bearer ${token}`,
                },
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
    }, [token, clusterData]);

    // Fetch cluster data when token changes
    useEffect(() => {
        if (token) {
            fetchClusterData();
        } else {
            setClusterData([]);
            setSelectedServer(null);
            setCurrentConnection(null);
        }
    }, [token, fetchClusterData]);

    // Fetch current connection after cluster data is loaded
    useEffect(() => {
        if (clusterData.length > 0) {
            fetchCurrentConnection();
        }
    }, [clusterData, fetchCurrentConnection]);

    // Auto-refresh effect
    useEffect(() => {
        if (!autoRefreshEnabled || !token) return;

        const intervalId = setInterval(() => {
            fetchClusterData();
        }, autoRefreshInterval);

        return () => clearInterval(intervalId);
    }, [autoRefreshEnabled, token, fetchClusterData]);

    /**
     * Update a cluster group's name
     * Handles both database-backed groups (group-{id}) and
     * auto-detected groups (group-auto)
     */
    const updateGroupName = useCallback(async (groupId, newName) => {
        if (!token) throw new Error('Not authenticated');

        const groupIdStr = groupId.toString();

        // Check if it's an auto-detected group (group-auto)
        const isAutoDetected = /^group-auto/.test(groupIdStr);

        if (isAutoDetected) {
            // Auto-detected groups: send the group ID as-is
            const response = await fetch(`/api/cluster-groups/${groupIdStr}`, {
                method: 'PUT',
                headers: {
                    'Authorization': `Bearer ${token}`,
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

            const response = await fetch(`/api/cluster-groups/${numericId}`, {
                method: 'PUT',
                headers: {
                    'Authorization': `Bearer ${token}`,
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
    }, [token, fetchClusterData]);

    /**
     * Update a cluster's name
     * Handles both database-backed clusters (cluster-{id}) and
     * auto-detected clusters (server-{id}, cluster-spock-{prefix})
     */
    const updateClusterName = useCallback(async (clusterId, newName, groupId, autoClusterKey) => {
        if (!token) throw new Error('Not authenticated');

        const clusterIdStr = clusterId.toString();

        // Check if it's an auto-detected cluster
        const isAutoDetected = /^(server-\d+|cluster-spock-.+)$/.test(clusterIdStr);

        if (isAutoDetected) {
            // Auto-detected clusters: send the cluster ID as-is and include auto_cluster_key
            const body = { name: newName };
            if (autoClusterKey) {
                body.auto_cluster_key = autoClusterKey;
            }

            const response = await fetch(`/api/clusters/${clusterIdStr}`, {
                method: 'PUT',
                headers: {
                    'Authorization': `Bearer ${token}`,
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

            const response = await fetch(`/api/clusters/${numericId}`, {
                method: 'PUT',
                headers: {
                    'Authorization': `Bearer ${token}`,
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
    }, [token, fetchClusterData]);

    /**
     * Update a server's (connection's) name
     */
    const updateServerName = useCallback(async (serverId, newName) => {
        if (!token) throw new Error('Not authenticated');

        const response = await fetch(`/api/connections/${serverId}`, {
            method: 'PUT',
            headers: {
                'Authorization': `Bearer ${token}`,
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
    }, [token, fetchClusterData]);

    /**
     * Get full server (connection) details for editing
     */
    const getServer = useCallback(async (serverId) => {
        if (!token) throw new Error('Not authenticated');

        const response = await fetch(`/api/connections/${serverId}`, {
            method: 'GET',
            headers: {
                'Authorization': `Bearer ${token}`,
            },
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to get server details');
        }

        return await response.json();
    }, [token]);

    /**
     * Create a new server (connection)
     */
    const createServer = useCallback(async (serverData) => {
        if (!token) throw new Error('Not authenticated');

        const response = await fetch('/api/connections', {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${token}`,
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
    }, [token, fetchClusterData]);

    /**
     * Update an existing server (connection)
     */
    const updateServer = useCallback(async (serverId, serverData) => {
        if (!token) throw new Error('Not authenticated');

        const response = await fetch(`/api/connections/${serverId}`, {
            method: 'PUT',
            headers: {
                'Authorization': `Bearer ${token}`,
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
    }, [token, fetchClusterData]);

    /**
     * Delete a server (connection)
     */
    const deleteServer = useCallback(async (serverId) => {
        if (!token) throw new Error('Not authenticated');

        const response = await fetch(`/api/connections/${serverId}`, {
            method: 'DELETE',
            headers: {
                'Authorization': `Bearer ${token}`,
            },
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
    }, [token, fetchClusterData, selectedServer]);

    /**
     * Create a new cluster group
     */
    const createGroup = useCallback(async (groupData) => {
        if (!token) throw new Error('Not authenticated');

        const response = await fetch('/api/cluster-groups', {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${token}`,
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
    }, [token, fetchClusterData]);

    /**
     * Delete a cluster group
     */
    const deleteGroup = useCallback(async (groupId) => {
        if (!token) throw new Error('Not authenticated');

        // Extract numeric ID from group-{id} format if needed
        const numericId = typeof groupId === 'string' && groupId.startsWith('group-')
            ? parseInt(groupId.replace('group-', ''), 10)
            : groupId;

        const response = await fetch(`/api/cluster-groups/${numericId}`, {
            method: 'DELETE',
            headers: {
                'Authorization': `Bearer ${token}`,
            },
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to delete group');
        }

        await fetchClusterData();
    }, [token, fetchClusterData]);

    /**
     * Move a cluster to a different group
     * Supports both database-backed clusters and auto-detected clusters
     */
    const moveClusterToGroup = useCallback(async (clusterId, targetGroupId, autoClusterKey, clusterName) => {
        if (!token) throw new Error('Not authenticated');

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

        const response = await fetch(`/api/clusters/${clusterIdStr}`, {
            method: 'PUT',
            headers: {
                'Authorization': `Bearer ${token}`,
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(body),
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to move cluster');
        }

        await fetchClusterData();
    }, [token, fetchClusterData]);

    const value = {
        // Data
        clusterData,
        selectedServer,
        currentConnection,
        loading,
        error,
        // Data fetching
        fetchClusterData,
        selectServer,
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
