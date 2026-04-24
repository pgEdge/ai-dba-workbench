/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import type React from 'react';
import { createContext, useState, useCallback, useEffect, useRef, useMemo } from 'react';
import { useAuth } from './useAuth';
import { apiGet, ApiError } from '../utils/apiClient';
import { logger } from '../utils/logger';
import {
    generateDataFingerprint,
    transformConnectionsToHierarchy,
    type ConnectionRecord,
} from './clusterDataHelpers';

export interface ClusterServer {
    id: number;
    name: string;
    description?: string;
    host?: string;
    port?: number;
    status?: string;
    role?: string | null;
    primary_role?: string | null;
    version?: string | null;
    database_name?: string;
    database?: string;
    username?: string;
    os?: string;
    platform?: string;
    spock_node_name?: string;
    spock_version?: string;
    connection_error?: string;
    membership_source?: string;
    children?: ClusterServer[];
    relationships?: Array<{
        target_server_id: number;
        target_server_name: string;
        relationship_type: string;
        is_auto_detected: boolean;
    }>;
    [key: string]: unknown;
}

export interface ClusterEntry {
    id: string;
    name: string;
    description?: string;
    servers: ClusterServer[];
    isStandalone?: boolean;
    auto_cluster_key?: string;
    replication_type?: string | null;
    [key: string]: unknown;
}

export interface ClusterGroup {
    id: string;
    name: string;
    clusters: ClusterEntry[];
    [key: string]: unknown;
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

const ClusterDataContext = createContext<ClusterDataContextValue | null>(null);

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
            logger.error('Error fetching cluster data:', err);
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

        return () => { clearInterval(intervalId); };
    }, [autoRefreshEnabled, user]);

    const value: ClusterDataContextValue = useMemo(() => ({
        clusterData,
        loading,
        error,
        lastRefresh,
        autoRefreshEnabled,
        setAutoRefreshEnabled,
        fetchClusterData,
    }), [clusterData, loading, error, lastRefresh, autoRefreshEnabled, setAutoRefreshEnabled, fetchClusterData]);

    return (
        <ClusterDataContext.Provider value={value}>
            {children}
        </ClusterDataContext.Provider>
    );
};

export default ClusterDataContext;
