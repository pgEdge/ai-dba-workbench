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
    /** Numeric clusters.id of an auto-detected cluster; see issue #809. */
    db_cluster_id?: number;
    replication_type?: string | null;
    [key: string]: unknown;
}

export interface ClusterGroup {
    id: string;
    name: string;
    // The server's GET /api/v1/clusters response can encode an empty
    // group as `clusters: null` (Go nil-slice JSON marshaling). All
    // consumers must guard against null; see issue #242.
    clusters: ClusterEntry[] | null;
    [key: string]: unknown;
}

export interface ClusterDataContextValue {
    clusterData: ClusterGroup[];
    loading: boolean;
    /** True whilst a requested (not background) fetch is in flight */
    refreshing: boolean;
    error: string | null;
    lastRefresh: Date | null;
    autoRefreshEnabled: boolean;
    setAutoRefreshEnabled: React.Dispatch<React.SetStateAction<boolean>>;
    fetchClusterData: () => Promise<void>;
}

interface ClusterDataProviderProps {
    children: React.ReactNode;
}

/**
 * Turn a failed fetch into a message for the cluster navigator. A non-JSON
 * error response (a proxy's HTML error page, say) arrives as the raw body,
 * which may carry markup or a server version, so show a fixed sentence
 * naming the status instead.
 */
const describeLoadError = (err: unknown): string => {
    if (err instanceof ApiError) {
        if (err.errorBody !== '' && err.message === err.errorBody) {
            return `The server returned an unexpected response (HTTP ${err.statusCode}).`;
        }
        return err.message;
    }
    return err instanceof Error && err.message ? err.message : 'Failed to load servers';
};

const ClusterDataContext = createContext<ClusterDataContextValue | null>(null);

export const ClusterDataProvider = ({ children }: ClusterDataProviderProps): React.ReactElement => {
    const { user } = useAuth();
    const [clusterData, setClusterData] = useState<ClusterGroup[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const [refreshing, setRefreshing] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const [autoRefreshEnabled, setAutoRefreshEnabled] = useState<boolean>(true);
    const [lastRefresh, setLastRefresh] = useState<Date | null>(null);
    const autoRefreshInterval = 30000; // 30 seconds

    // Track data fingerprint to detect actual changes
    const dataFingerprintRef = useRef<string>('');
    // True until the first successful fetch; while set, every requested
    // fetch (including a Retry after a failed first load) shows the loading
    // state, but background polls do not
    const isInitialLoadRef = useRef<boolean>(true);
    // Number of requested fetches in flight, behind `refreshing`
    const requestedCountRef = useRef<number>(0);
    // Sequence number of the last fetch started, and of the last fetch whose
    // successful result was applied. A success is applied unless a newer
    // success already has been; a failure only if no fetch started at or
    // after it has succeeded. A failure therefore never hides the data of an
    // older fetch that succeeds later (a Retry overtaken by a failed poll)
    const requestSeqRef = useRef<number>(0);
    const appliedSuccessSeqRef = useRef<number>(0);
    // Fetches numbered below this started for a previous user and are ignored
    const minSeqRef = useRef<number>(1);
    // Stable ref for the background poll to avoid resetting the interval
    const pollRef = useRef<() => Promise<void>>(() => Promise.resolve());

    /**
     * Fetch cluster hierarchy from the API.
     * Uses fingerprinting to detect actual changes and avoid unnecessary re-renders.
     * A requested fetch shows the loading state until data has loaded
     * successfully once; a background poll never does, so the auto-refresh
     * does not swap an error panel for the skeleton every 30 seconds.
     * The error is cleared only on success, so it persists across failed
     * retries. A fetch that finishes after a newer fetch has succeeded is
     * ignored, as is any fetch started for a previous user.
     */
    const loadClusterData = useCallback(async (background: boolean): Promise<void> => {
        if (!user) {return;}

        requestSeqRef.current += 1;
        const seq = requestSeqRef.current;
        const isCurrentUser = (): boolean => seq >= minSeqRef.current;

        const showLoading = isInitialLoadRef.current && !background;
        if (showLoading) {
            setLoading(true);
        }
        if (!background) {
            requestedCountRef.current += 1;
            setRefreshing(true);
        }

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

            if (!isCurrentUser() || seq < appliedSuccessSeqRef.current) {return;}
            appliedSuccessSeqRef.current = seq;

            // Generate fingerprint for the new data
            const newFingerprint = generateDataFingerprint(newData ?? []);

            // Only update state if data has actually changed
            if (newFingerprint !== dataFingerprintRef.current) {
                dataFingerprintRef.current = newFingerprint;
                setClusterData(newData ?? []);
            }

            // Always update last refresh time
            setLastRefresh(new Date());
            setError(null);
            isInitialLoadRef.current = false;
            // A poll can succeed whilst the first load or a Retry is still
            // pending; stop showing the loading state now rather than when
            // that request settles
            setLoading(false);
        } catch (err) {
            logger.error('Error fetching cluster data:', err);
            if (isCurrentUser() && seq > appliedSuccessSeqRef.current) {
                setError(describeLoadError(err));
            }
        } finally {
            // A previous user's fetch was already dropped from the loading
            // and refreshing state when the user changed
            if (isCurrentUser()) {
                if (showLoading) {
                    setLoading(false);
                }
                if (!background) {
                    requestedCountRef.current -= 1;
                    if (requestedCountRef.current === 0) {
                        setRefreshing(false);
                    }
                }
            }
        }
    }, [user]);

    const fetchClusterData = useCallback(
        (): Promise<void> => loadClusterData(false),
        [loadClusterData],
    );

    // Keep the ref updated with the latest loadClusterData
    useEffect(() => {
        pollRef.current = () => loadClusterData(true);
    }, [loadClusterData]);

    // Fetch cluster data when user changes, ignoring any fetch still in
    // flight for the previous user and dropping it from the loading and
    // refreshing state, which the new user's fetch sets again as needed
    useEffect(() => {
        minSeqRef.current = requestSeqRef.current + 1;
        requestedCountRef.current = 0;
        setLoading(false);
        setRefreshing(false);
        if (user) {
            fetchClusterData();
        } else {
            dataFingerprintRef.current = '';
            setClusterData([]);
            setError(null);
        }
    }, [user, fetchClusterData]);

    // Auto-refresh effect - uses pollRef to avoid resetting the
    // interval when loadClusterData's reference changes
    useEffect(() => {
        if (!autoRefreshEnabled || !user) {return;}

        const intervalId = setInterval(() => {
            pollRef.current();
        }, autoRefreshInterval);

        return () => { clearInterval(intervalId); };
    }, [autoRefreshEnabled, user]);

    const value: ClusterDataContextValue = useMemo(() => ({
        clusterData,
        loading,
        refreshing,
        error,
        lastRefresh,
        autoRefreshEnabled,
        setAutoRefreshEnabled,
        fetchClusterData,
    }), [clusterData, loading, refreshing, error, lastRefresh, autoRefreshEnabled, setAutoRefreshEnabled, fetchClusterData]);

    return (
        <ClusterDataContext.Provider value={value}>
            {children}
        </ClusterDataContext.Provider>
    );
};

export default ClusterDataContext;
