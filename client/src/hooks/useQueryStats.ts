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
import type { TimeRange } from '../components/Dashboard/types';
import { apiFetch } from '../utils/apiClient';
import { logger } from '../utils/logger';

/** Parameters identifying the statement and period to summarise. */
export interface QueryStatsParams {
    connectionId: number;
    queryId: string;
    databaseName: string;
    timeRange: TimeRange;
}

/**
 * Period-scoped statistics for a single pg_stat_statements entry.
 * `avg_exec_time` is null when the period holds no usable samples,
 * which is deliberately distinct from an average of zero.
 */
export interface QueryStats {
    queryid: string;
    avg_exec_time: number | null;
    calls: number;
    total_exec_time: number;
}

export interface UseQueryStatsReturn {
    stats: QueryStats | null;
    loading: boolean;
    error: string | null;
    refetch: () => void;
}

/**
 * Fetch execution statistics for a single query, scoped to the
 * selected dashboard time range rather than the lifetime totals that
 * pg_stat_statements reports. Refetches whenever the parameters, the
 * custom range bounds or the dashboard refresh trigger change.
 */
export const useQueryStats = (
    params: QueryStatsParams | null,
): UseQueryStatsReturn => {
    const { user } = useAuth();
    const { refreshTrigger, timeRange: dashboardRange } = useDashboard();
    const customStart = dashboardRange?.customStart;
    const customEnd = dashboardRange?.customEnd;
    const [stats, setStats] = useState<QueryStats | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    // Identifies the most recently started request, so that a slow
    // response for a previous query or period cannot land after a
    // fast one for the current parameters and overwrite it. See
    // useDatabaseSummaries for the same pattern.
    const requestIdRef = useRef<number>(0);

    const connectionId = params?.connectionId;
    const queryId = params?.queryId;
    const databaseName = params?.databaseName;
    const timeRange = params?.timeRange;

    const fetchStats = useCallback(async (): Promise<void> => {
        if (
            !user
            || connectionId === undefined
            || !queryId
            || !databaseName
            || !timeRange
        ) {
            // Nothing to fetch: retire any in-flight request and reset
            // to the idle state so stale stats for a previous query do
            // not linger.
            requestIdRef.current += 1;
            setStats(null);
            setError(null);
            setLoading(false);
            return;
        }

        /*
         * A custom range without both bounds is a transient state the
         * server rejects with a 400, so skip the request entirely and
         * leave whatever stats and error state is already in place.
         */
        if (timeRange === 'custom' && (!customStart || !customEnd)) {
            return;
        }

        const searchParams = new URLSearchParams({
            connection_id: connectionId.toString(),
            queryid: queryId,
            database_name: databaseName,
            time_range: timeRange,
        });
        if (timeRange === 'custom' && customStart && customEnd) {
            searchParams.append('time_start', customStart);
            searchParams.append('time_end', customEnd);
        }
        const url =
            `/api/v1/metrics/query-stats?${searchParams.toString()}`;

        setLoading(true);
        setError(null);

        const requestId = ++requestIdRef.current;
        // A response is only applied if the hook is still mounted and
        // no newer request has been started since.
        const isCurrent = (): boolean =>
            isMountedRef.current && requestIdRef.current === requestId;

        try {
            const response = await apiFetch(url);

            if (!response.ok) {
                const errorData = await response.json().catch(
                    () => ({})
                ) as { error?: string };
                throw new Error(
                    errorData.error
                    || `Failed to fetch query stats: `
                    + `${response.status}`
                );
            }

            const result = await response.json() as QueryStats;

            if (isCurrent()) {
                setStats(result);
            }
        } catch (err) {
            logger.error('Error fetching query stats:', err);
            if (isCurrent()) {
                setError(
                    (err as Error).message
                    || 'Failed to fetch query stats'
                );
                setStats(null);
            }
        } finally {
            if (isCurrent()) {
                setLoading(false);
            }
        }
    }, [
        user, connectionId, queryId, databaseName, timeRange,
        customStart, customEnd,
    ]);

    const refetch = useCallback((): void => {
        void fetchStats();
    }, [fetchStats]);

    useEffect(() => {
        isMountedRef.current = true;

        void fetchStats();

        return () => {
            isMountedRef.current = false;
        };
    }, [fetchStats, refreshTrigger]);

    return { stats, loading, error, refetch };
};

export default useQueryStats;
