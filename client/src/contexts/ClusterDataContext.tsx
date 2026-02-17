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
import React, { createContext, useContext, useState, useCallback, useEffect, useRef } from 'react';
import { useAuth } from './AuthContext';
import { apiGet, ApiError } from '../utils/apiClient';

export interface ClusterServer {
    id: number;
    name: string;
    description?: string;
    host?: string;
    port?: number;
    status: string;
    role?: string | null;
    primary_role?: string;
    version?: string | null;
    connection_error?: string;
    children?: ClusterServer[];
}

export interface ClusterEntry {
    id: string;
    name: string;
    description?: string;
    servers: ClusterServer[];
    isStandalone?: boolean;
    auto_cluster_key?: string;
}

export interface ClusterGroup {
    id: string;
    name: string;
    clusters: ClusterEntry[];
}

export interface ClusterDataContextValue {
    clusterData: ClusterGroup[];
    loading: boolean;
    error: string | null;
    lastRefresh: Date | null;
    autoRefreshEnabled: boolean;
    setAutoRefreshEnabled: React.Dispatch<React.SetStateAction<boolean>>;
    fetchClusterData: () => Promise<void>;
}

interface ClusterDataProviderProps {
    children: React.ReactNode;
}

interface ConnectionRecord {
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

const ClusterDataContext = createContext<ClusterDataContextValue | null>(null);

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
            return `${cluster.id}:${cluster.name}:${serverFingerprints}`;
        }).join('|');
        return `${group.id}:${group.name}:${clusterFingerprints}`;
    }).join('||');

    return fingerprint;
};

/**
 * Recursively collect server fingerprints including nested children
 */
export const collectServerFingerprints = (servers: ClusterServer[]): string => {
    if (!servers || servers.length === 0) {return '';}
    return servers.map(server => {
        const childFingerprints = collectServerFingerprints(server.children || []);
        return `${server.id}:${server.name}:${server.status}:${server.connection_error || ''}:${server.primary_role || server.role || ''}${childFingerprints ? ':' + childFingerprints : ''}`;
    }).join(',');
};

/**
 * Transform flat connections list into hierarchical structure
 * This is a temporary solution until the API supports proper hierarchy
 */
export const transformConnectionsToHierarchy = (connections: ConnectionRecord[]): ClusterGroup[] => {
    // Group connections by cluster_group and cluster
    const groups = new Map<string, GroupBuildEntry>();

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

        const group = groups.get(groupName) as GroupBuildEntry;

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

export const ClusterDataProvider = ({ children }: ClusterDataProviderProps): React.ReactElement => {
    const { user } = useAuth();
    const [clusterData, setClusterData] = useState<ClusterGroup[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const [autoRefreshEnabled, setAutoRefreshEnabled] = useState<boolean>(true);
    const [lastRefresh, setLastRefresh] = useState<Date | null>(null);
    const autoRefreshInterval = 30000; // 30 seconds

    // Track data fingerprint to detect actual changes
    const dataFingerprintRef = useRef<string>('');
    // Track if this is the initial load (to show loading state)
    const isInitialLoadRef = useRef<boolean>(true);
    // Stable ref for fetchClusterData to avoid resetting the interval
    const fetchRef = useRef<() => Promise<void>>(() => Promise.resolve());

    /**
     * Fetch cluster hierarchy from the API.
     * Uses fingerprinting to detect actual changes and avoid unnecessary re-renders.
     * Only shows loading state on initial load, not during auto-refresh.
     */
    const fetchClusterData = useCallback(async (): Promise<void> => {
        if (!user) {return;}

        // Only show loading spinner on initial load
        if (isInitialLoadRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            // First, try to get the hierarchical cluster data
            // If the API doesn't support it yet, fall back to connections
            let newData: ClusterGroup[] | null = null;

            try {
                newData = await apiGet<ClusterGroup[]>('/api/v1/clusters');
            } catch (err) {
                if (err instanceof ApiError && err.statusCode === 404) {
                    // Fall back to connections endpoint
                    const connections = await apiGet<ConnectionRecord[]>('/api/v1/connections');
                    newData = transformConnectionsToHierarchy(connections);
                } else {
                    throw err;
                }
            }

            // Generate fingerprint for the new data
            const newFingerprint = generateDataFingerprint(newData ?? []);

            // Only update state if data has actually changed
            if (newFingerprint !== dataFingerprintRef.current) {
                dataFingerprintRef.current = newFingerprint;
                setClusterData(newData ?? []);
            }

            // Always update last refresh time
            setLastRefresh(new Date());
        } catch (err) {
            console.error('Error fetching cluster data:', err);
            setError((err as Error).message);
        } finally {
            if (isInitialLoadRef.current) {
                isInitialLoadRef.current = false;
            }
            setLoading(false);
        }
    }, [user]);

    // Keep the ref updated with the latest fetchClusterData
    useEffect(() => {
        fetchRef.current = fetchClusterData;
    }, [fetchClusterData]);

    // Fetch cluster data when user changes
    useEffect(() => {
        if (user) {
            fetchClusterData();
        } else {
            setClusterData([]);
        }
    }, [user, fetchClusterData]);

    // Auto-refresh effect - uses fetchRef to avoid resetting the
    // interval when fetchClusterData's reference changes
    useEffect(() => {
        if (!autoRefreshEnabled || !user) {return;}

        const intervalId = setInterval(() => {
            fetchRef.current();
        }, autoRefreshInterval);

        return () => clearInterval(intervalId);
    }, [autoRefreshEnabled, user]);

    const value: ClusterDataContextValue = {
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

export const useClusterData = (): ClusterDataContextValue => {
    const context = useContext(ClusterDataContext);
    if (!context) {
        throw new Error('useClusterData must be used within a ClusterDataProvider');
    }
    return context;
};

export default ClusterDataContext;
