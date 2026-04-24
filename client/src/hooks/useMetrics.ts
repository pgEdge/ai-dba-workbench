/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useCallback, useEffect, useRef } from 'react';
import { useAuth } from '../contexts/useAuth';
import { useDashboard } from '../contexts/useDashboard';
import type { MetricQueryParams, MetricSeries, MetricBaseline } from '../components/Dashboard/types';
import { apiGet } from '../utils/apiClient';
import { logger } from '../utils/logger';

export interface UseMetricsReturn {
    data: MetricSeries[] | null;
    loading: boolean;
    error: string | null;
    refetch: () => void;
}

export interface UseBaselinesReturn {
    baselines: MetricBaseline[] | null;
    loading: boolean;
    error: string | null;
}

/**
 * Build the query URL for the metrics API from the given parameters.
 */
const buildMetricsUrl = (params: MetricQueryParams): string => {
    const searchParams = new URLSearchParams();

    searchParams.append('probe_name', params.probeName);
    searchParams.append('time_range', params.timeRange);

    if (params.connectionId !== undefined) {
        searchParams.append('connection_id', params.connectionId.toString());
    }

    if (params.connectionIds && params.connectionIds.length > 0) {
        searchParams.append('connection_ids', params.connectionIds.join(','));
    }

    if (params.databaseName) {
        searchParams.append('database_name', params.databaseName);
    }

    if (params.schemaName) {
        searchParams.append('schema_name', params.schemaName);
    }

    if (params.tableName) {
        searchParams.append('table_name', params.tableName);
    }

    if (params.buckets !== undefined) {
        searchParams.append('buckets', params.buckets.toString());
    }

    if (params.aggregation) {
        searchParams.append('aggregation', params.aggregation);
    }

    if (params.metrics && params.metrics.length > 0) {
        searchParams.append('metrics', params.metrics.join(','));
    }

    return `/api/v1/metrics/query?${searchParams.toString()}`;
};

/**
 * Custom hook for fetching metric time series data.
 * Follows the usePerformanceSummary pattern with initialLoadDoneRef
 * to prevent flash on auto-refresh.
 */
export const useMetrics = (params: MetricQueryParams | null): UseMetricsReturn => {
    const { user } = useAuth();
    const { refreshTrigger } = useDashboard();
    const [data, setData] = useState<MetricSeries[] | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);

    const fetchData = useCallback(async (): Promise<void> => {
        if (!user || !params) { return; }

        const url = buildMetricsUrl(params);

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const result = await apiGet<MetricSeries[]>(url);

            if (isMountedRef.current) {
                setData(result);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            logger.error('Error fetching metrics:', err);
            if (isMountedRef.current) {
                setError((err as Error).message || 'Failed to fetch metrics');
                setData(null);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, params]);

    const refetch = useCallback((): void => {
        void fetchData();
    }, [fetchData]);

    // Reset initial load state when params change
    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [params?.probeName, params?.connectionId, params?.timeRange]);

    // Fetch when dependencies change or refresh is triggered
    useEffect(() => {
        isMountedRef.current = true;

        if (user && params) {
            void fetchData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, params, fetchData, refreshTrigger]);

    return { data, loading, error, refetch };
};

/**
 * Custom hook for fetching metric baselines.
 * Returns statistical baselines (mean, stddev, percentiles)
 * for the specified probe and connection.
 */
export const useBaselines = (
    probeName: string | null,
    connectionId: number | null,
    metrics?: string[]
): UseBaselinesReturn => {
    const { user } = useAuth();
    const [baselines, setBaselines] = useState<MetricBaseline[] | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);

    const fetchBaselines = useCallback(async (): Promise<void> => {
        if (!user || !probeName || connectionId === null) { return; }

        setLoading(true);
        setError(null);

        try {
            const searchParams = new URLSearchParams();
            searchParams.append('probe_name', probeName);
            searchParams.append('connection_id', connectionId.toString());

            if (metrics && metrics.length > 0) {
                searchParams.append('metrics', metrics.join(','));
            }

            const result = await apiGet<MetricBaseline[]>(
                `/api/v1/metrics/baselines?${searchParams.toString()}`
            );

            if (isMountedRef.current) {
                setBaselines(result);
            }
        } catch (err) {
            logger.error('Error fetching baselines:', err);
            if (isMountedRef.current) {
                setError((err as Error).message || 'Failed to fetch baselines');
                setBaselines(null);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, probeName, connectionId, metrics]);

    useEffect(() => {
        isMountedRef.current = true;

        if (user && probeName && connectionId !== null) {
            void fetchBaselines();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, probeName, connectionId, fetchBaselines]);

    return { baselines, loading, error };
};

export default useMetrics;
