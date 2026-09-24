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
import type {
    MetricQueryParams,
    MetricSeries,
    MetricBaseline,
    MetricsQueryResult,
    MetricsWindow,
} from '../components/Dashboard/types';
import { apiGet } from '../utils/apiClient';
import { logger } from '../utils/logger';
import {
    appendTimeRangeParams,
    isTimeRangeQueryable,
} from '../utils/timeRangeParams';
import {
    chunkConnectionIds,
    MAX_CONNECTION_IDS_PER_REQUEST,
} from '../utils/connectionIdBatches';

export interface UseMetricsReturn {
    data: MetricSeries[] | null;
    /**
     * The window the server reported having queried, or null when no
     * response has arrived or the response carried no window. Charts
     * anchor their x-axis to it so that the axis reflects the range
     * that was asked for rather than the history that happens to
     * exist.
     */
    window: MetricsWindow | null;
    loading: boolean;
    error: string | null;
    refetch: () => void;
}

/**
 * Read the envelope returned by the metrics query endpoint.
 *
 * A bare array is still accepted so that the client degrades to its
 * previous behaviour (a data-derived axis) against a server that
 * predates the envelope, rather than rendering nothing at all.
 */
const parseMetricsResponse = (
    result: MetricsQueryResult | MetricSeries[] | null,
): { series: MetricSeries[] | null; window: MetricsWindow | null } => {
    if (Array.isArray(result)) {
        return { series: result, window: null };
    }
    if (!result || typeof result !== 'object') {
        return { series: null, window: null };
    }

    const hasWindow = typeof result.time_start === 'string'
        && typeof result.time_end === 'string'
        && typeof result.bucket_seconds === 'number'
        && result.bucket_seconds > 0;

    return {
        series: result.series ?? null,
        window: hasWindow
            ? {
                start: result.time_start,
                end: result.time_end,
                bucketSeconds: result.bucket_seconds,
            }
            : null,
    };
};

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
    appendTimeRangeParams(searchParams, {
        range: params.timeRange, customStart, customEnd,
    });

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

    if (params.mountPoint) {
        searchParams.append('mount_point', params.mountPoint);
    }

    if (params.queryId) {
        searchParams.append('queryid', params.queryId);
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
 * Build one URL per request the query needs.
 *
 * The endpoint caps how many connection IDs a request may name, so a
 * longer list is split into batches; a list within the cap, which is
 * every caller today, yields the single URL it always did.
 */
const buildMetricsUrls = (
    params: MetricQueryParams,
    customStart?: string,
    customEnd?: string,
): string[] => {
    const ids = params.connectionIds ?? [];
    if (ids.length <= MAX_CONNECTION_IDS_PER_REQUEST) {
        return [buildMetricsUrl(params, customStart, customEnd)];
    }

    return chunkConnectionIds(ids).map(batch => buildMetricsUrl(
        { ...params, connectionIds: batch }, customStart, customEnd,
    ));
};

/**
 * Combine the responses to a batched query into one parsed result.
 *
 * The endpoint returns a series per connection rather than one series
 * aggregated across them, so concatenating the batches reproduces the
 * unbatched response. The window is taken from the first batch that
 * reported one: every batch asks for the same range, and the server
 * resolves it per request from the probe's collection interval, so the
 * batches can in principle differ in bucket width. Anchoring the axis
 * to the first is the same choice the unbatched request made.
 */
const mergeMetricsResponses = (
    parsed: { series: MetricSeries[] | null; window: MetricsWindow | null }[],
): { series: MetricSeries[] | null; window: MetricsWindow | null } => {
    if (parsed.length === 1) {return parsed[0];}

    return {
        series: parsed.some(p => p.series !== null)
            ? parsed.flatMap(p => p.series ?? [])
            : null,
        window: parsed.find(p => p.window !== null)?.window ?? null,
    };
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
    const [metricsWindow, setMetricsWindow] =
        useState<MetricsWindow | null>(null);
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
        const selectedWindow = {
            range: params.timeRange, customStart, customEnd,
        };
        if (!isTimeRangeQueryable(selectedWindow)) {
            return;
        }

        const urls = buildMetricsUrls(params, customStart, customEnd);

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            // Any batch that rejects rejects the whole load, exactly as
            // a failed single request did, so a partial series is never
            // rendered as though the load succeeded.
            const results = await Promise.all(urls.map(
                url => apiGet<MetricsQueryResult | MetricSeries[]>(url),
            ));

            if (isMountedRef.current) {
                const parsed = mergeMetricsResponses(
                    results.map(parseMetricsResponse),
                );
                setData(parsed.series);
                setMetricsWindow(parsed.window);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            logger.error('Error fetching metrics:', err);
            if (isMountedRef.current) {
                setError((err as Error).message || 'Failed to fetch metrics');
                setData(null);
                setMetricsWindow(null);
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
        params?.mountPoint,
        // A new custom window is as much a change of query as a new
        // preset is, so the loading state must show for it too.
        customStart,
        customEnd,
        params?.queryId,
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

    return { data, window: metricsWindow, loading, error, refetch };
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
