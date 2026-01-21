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

import React, { createContext, useContext, useState, useCallback, useEffect } from 'react';
import { useAuth } from './AuthContext';

const ClusterContext = createContext(null);

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

    /**
     * Fetch cluster hierarchy from the API
     */
    const fetchClusterData = useCallback(async () => {
        if (!token) return;

        setLoading(true);
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

            if (response.ok) {
                const data = await response.json();
                setClusterData(data);
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
                    const hierarchy = transformConnectionsToHierarchy(connections);
                    setClusterData(hierarchy);
                } else {
                    throw new Error('Failed to fetch connections');
                }
            } else {
                throw new Error('Failed to fetch cluster data');
            }
        } catch (err) {
            console.error('Error fetching cluster data:', err);
            setError(err.message);
        } finally {
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

    const value = {
        clusterData,
        selectedServer,
        currentConnection,
        loading,
        error,
        fetchClusterData,
        selectServer,
        clearSelection,
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
