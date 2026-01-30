/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { createContext, useContext, useState, useCallback, useEffect, useRef } from 'react';
import { useAuth } from './AuthContext';

const ClusterDataContext = createContext(null);

/**
 * Generate a fingerprint for cluster data to detect actual changes.
 * This allows us to skip re-renders when the data hasn't changed.
 */
export const generateDataFingerprint = (data) => {
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
export const collectServerFingerprints = (servers) => {
    if (!servers || servers.length === 0) return '';
    return servers.map(server => {
        const childFingerprints = collectServerFingerprints(server.children);
        return `${server.id}:${server.name}:${server.status}:${server.connection_error || ''}:${server.primary_role || server.role || ''}${childFingerprints ? ':' + childFingerprints : ''}`;
    }).join(',');
};

/**
 * Transform flat connections list into hierarchical structure
 * This is a temporary solution until the API supports proper hierarchy
 */
export const transformConnectionsToHierarchy = (connections) => {
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

export const ClusterDataProvider = ({ children }) => {
    const { user } = useAuth();
    const [clusterData, setClusterData] = useState([]);
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

    // Fetch cluster data when user changes
    useEffect(() => {
        if (user) {
            fetchClusterData();
        } else {
            setClusterData([]);
        }
    }, [user, fetchClusterData]);

    // Auto-refresh effect
    useEffect(() => {
        if (!autoRefreshEnabled || !user) return;

        const intervalId = setInterval(() => {
            fetchClusterData();
        }, autoRefreshInterval);

        return () => clearInterval(intervalId);
    }, [autoRefreshEnabled, user, fetchClusterData]);

    const value = {
        // Data
        clusterData,
        loading,
        error,
        lastRefresh,
        // Auto-refresh
        autoRefreshEnabled,
        setAutoRefreshEnabled,
        // Data fetching
        fetchClusterData,
    };

    return (
        <ClusterDataContext.Provider value={value}>
            {children}
        </ClusterDataContext.Provider>
    );
};

export const useClusterData = () => {
    const context = useContext(ClusterDataContext);
    if (!context) {
        throw new Error('useClusterData must be used within a ClusterDataProvider');
    }
    return context;
};

export default ClusterDataContext;
