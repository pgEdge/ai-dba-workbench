/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Alerts Context
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Context for managing alert counts and data across components
 *
 *-------------------------------------------------------------------------
 */
import React, { createContext, useState, useCallback, useEffect, useRef, useMemo } from 'react';
import { useAuth } from './useAuth';
import { apiGet } from '../utils/apiClient';
import { logger } from '../utils/logger';

export interface AlertCounts {
    total: number;
    byServer: Record<number, number>;
    byCluster: Record<string, number>;
}

export interface AlertsContextValue {
    alertCounts: AlertCounts;
    loading: boolean;
    lastFetch: Date | null;
    fetchAlertCounts: () => Promise<void>;
    getServerAlertCount: (serverId: number) => number;
    getClusterAlertCount: (serverIds: number[]) => number;
    getTotalAlertCount: () => number;
}

interface AlertsProviderProps {
    children: React.ReactNode;
}

interface AlertCountsApiResponse {
    total?: number;
    by_server?: Record<number, number>;
    by_cluster?: Record<string, number>;
}

const AlertsContext = createContext<AlertsContextValue | null>(null);

export const AlertsProvider = ({ children }: AlertsProviderProps): React.ReactElement => {
    const { user } = useAuth();
    const [alertCounts, setAlertCounts] = useState<AlertCounts>({
        total: 0,
        byServer: {},    // Map of server ID -> count
        byCluster: {},   // Map of cluster ID -> count (sum of server alerts)
    });
    const [loading, setLoading] = useState<boolean>(false);
    const [lastFetch, setLastFetch] = useState<Date | null>(null);
    const refreshInterval = 30000; // 30 seconds
    const isMountedRef = useRef<boolean>(true);

    /**
     * Fetch alert counts from the API
     */
    const fetchAlertCounts = useCallback(async (): Promise<void> => {
        if (!user) {return;}

        setLoading(true);
        try {
            const data = await apiGet<AlertCountsApiResponse>('/api/v1/alerts/counts');

            if (isMountedRef.current) {
                setAlertCounts({
                    total: data.total || 0,
                    byServer: data.by_server ?? {},
                    byCluster: data.by_cluster || {},
                });
                setLastFetch(new Date());
            }
        } catch (err) {
            logger.error('Error fetching alert counts:', err);
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user]);

    /**
     * Get alert count for a specific server
     */
    const getServerAlertCount = useCallback((serverId: number): number => {
        return alertCounts.byServer[serverId] || 0;
    }, [alertCounts.byServer]);

    /**
     * Get alert count for a cluster (sum of all server alerts in cluster)
     */
    const getClusterAlertCount = useCallback((serverIds: number[]): number => {
        if (!serverIds || serverIds.length === 0) {return 0;}
        return serverIds.reduce((sum, id) => sum + (alertCounts.byServer[id] || 0), 0);
    }, [alertCounts.byServer]);

    /**
     * Get total estate alert count
     */
    const getTotalAlertCount = useCallback((): number => {
        return alertCounts.total;
    }, [alertCounts.total]);

    // Initial fetch
    useEffect(() => {
        isMountedRef.current = true;
        if (user) {
            fetchAlertCounts();
        }
        return () => {
            isMountedRef.current = false;
        };
    }, [user, fetchAlertCounts]);

    // Auto-refresh
    useEffect(() => {
        if (!user) {return;}

        const intervalId = setInterval(fetchAlertCounts, refreshInterval);
        return () => clearInterval(intervalId);
    }, [user, fetchAlertCounts]);

    const value: AlertsContextValue = useMemo(() => ({
        alertCounts,
        loading,
        lastFetch,
        fetchAlertCounts,
        getServerAlertCount,
        getClusterAlertCount,
        getTotalAlertCount,
    }), [alertCounts, loading, lastFetch, fetchAlertCounts, getServerAlertCount, getClusterAlertCount, getTotalAlertCount]);

    return (
        <AlertsContext.Provider value={value}>
            {children}
        </AlertsContext.Provider>
    );
};

export default AlertsContext;
