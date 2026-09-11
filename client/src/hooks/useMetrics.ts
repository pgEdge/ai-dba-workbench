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
 *
 * The bounds of a custom window are not part of MetricQueryParams; they
 * come from DashboardContext, so that consumers need only pass the range
 * they already pass. They are emitted only for the 'custom' range, which
 * is the sole range for which the server accepts them.
 */
const buildMetricsUrl = (
    params: MetricQueryParams,
    customStart?: string,
    customEnd?: string,
): string => {
    const searchParams = new URLSearchParams();

    searchParams.append('probe_name', params.probeName);
    searchParams.append('time_range', params.timeRange);

    if (params.timeRange === 'custom' && customStart && customEnd) {
        searchParams.append('time_start', customStart);
        searchParams.append('time_end', customEnd);
    }

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

    if (params.indexName) {
        searchParams.append('index_name', params.indexName);
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
    const { refreshTrigger, timeRange } = useDashboard();
    const customStart = timeRange?.customStart;
    const customEnd = timeRange?.customEnd;
    const [data, setData] = useState<MetricSeries[] | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);

    const fetchData = useCallback(async (): Promise<void> => {
        if (!user || !params) { return; }

        /*
         * A custom range without both bounds is a transient state the
         * server rejects with a 400, so skip the request entirely and
         * leave whatever data and error state is already in place.
         */
        if (params.timeRange === 'custom' && (!customStart || !customEnd)) {
            return;
        }

        const url = buildMetricsUrl(params, customStart, customEnd);

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
    }, [user, params, customStart, customEnd]);

    const refetch = useCallback((): void => {
        void fetchData();
    }, [fetchData]);

    // Reset initial load state when params change
    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [
        params?.probeName,
        params?.connectionId,
        params?.timeRange,
        params?.indexName,
        params?.tableName,
        params?.schemaName,
        // A new custom window is as much a change of query as a new
        // preset is, so the loading state must show for it too.
        customStart,
        customEnd,
    ]);

    // Fetch when dependencies change or refresh is triggered
    useEffect(() => {
        isMountedRef.current = true;

        if (user && params) {
            void fetchData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, params, fetchData, refreshTrigger, customStart, customEnd]);

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
