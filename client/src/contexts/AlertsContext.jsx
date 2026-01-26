/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Alerts Context
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Context for managing alert counts and data across components
 *
 *-------------------------------------------------------------------------
 */

import React, { createContext, useContext, useState, useCallback, useEffect, useRef } from 'react';
import { useAuth } from './AuthContext';

const AlertsContext = createContext(null);

export const AlertsProvider = ({ children }) => {
    const { sessionToken: token } = useAuth();
    const [alertCounts, setAlertCounts] = useState({
        total: 0,
        byServer: {},    // Map of server ID -> count
        byCluster: {},   // Map of cluster ID -> count (sum of server alerts)
    });
    const [loading, setLoading] = useState(false);
    const [lastFetch, setLastFetch] = useState(null);
    const refreshInterval = 30000; // 30 seconds
    const isMountedRef = useRef(true);

    /**
     * Fetch alert counts from the API
     */
    const fetchAlertCounts = useCallback(async () => {
        if (!token) return;

        setLoading(true);
        try {
            const response = await fetch('/api/v1/alerts/counts', {
                headers: {
                    'Authorization': `Bearer ${token}`,
                },
            });

            if (response.ok && isMountedRef.current) {
                const data = await response.json();
                setAlertCounts({
                    total: data.total || 0,
                    byServer: data.by_server || {},
                    byCluster: data.by_cluster || {},
                });
                setLastFetch(new Date());
            }
        } catch (err) {
            console.error('Error fetching alert counts:', err);
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [token]);

    /**
     * Get alert count for a specific server
     */
    const getServerAlertCount = useCallback((serverId) => {
        return alertCounts.byServer[serverId] || 0;
    }, [alertCounts.byServer]);

    /**
     * Get alert count for a cluster (sum of all server alerts in cluster)
     */
    const getClusterAlertCount = useCallback((serverIds) => {
        if (!serverIds || serverIds.length === 0) return 0;
        return serverIds.reduce((sum, id) => sum + (alertCounts.byServer[id] || 0), 0);
    }, [alertCounts.byServer]);

    /**
     * Get total estate alert count
     */
    const getTotalAlertCount = useCallback(() => {
        return alertCounts.total;
    }, [alertCounts.total]);

    // Initial fetch
    useEffect(() => {
        isMountedRef.current = true;
        if (token) {
            fetchAlertCounts();
        }
        return () => {
            isMountedRef.current = false;
        };
    }, [token, fetchAlertCounts]);

    // Auto-refresh
    useEffect(() => {
        if (!token) return;

        const intervalId = setInterval(fetchAlertCounts, refreshInterval);
        return () => clearInterval(intervalId);
    }, [token, fetchAlertCounts]);

    const value = {
        alertCounts,
        loading,
        lastFetch,
        fetchAlertCounts,
        getServerAlertCount,
        getClusterAlertCount,
        getTotalAlertCount,
    };

    return (
        <AlertsContext.Provider value={value}>
            {children}
        </AlertsContext.Provider>
    );
};

export const useAlerts = () => {
    const context = useContext(AlertsContext);
    if (!context) {
        throw new Error('useAlerts must be used within an AlertsProvider');
    }
    return context;
};

export default AlertsContext;
