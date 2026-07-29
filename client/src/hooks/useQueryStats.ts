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
    queryid: string;
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
 * pg_stat_statements reports. Refetches whenever the parameters or
 * the dashboard refresh trigger change.
 */
export const useQueryStats = (
    params: QueryStatsParams | null,
): UseQueryStatsReturn => {
    const { user } = useAuth();
    const { refreshTrigger } = useDashboard();
    const [stats, setStats] = useState<QueryStats | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);

    const connectionId = params?.connectionId;
    const queryid = params?.queryid;
    const timeRange = params?.timeRange;

    const fetchStats = useCallback(async (): Promise<void> => {
        if (
            !user
            || connectionId === undefined
            || !queryid
            || !timeRange
        ) {
            return;
        }

        const searchParams = new URLSearchParams({
            connection_id: connectionId.toString(),
            queryid,
            time_range: timeRange,
        });
        const url =
            `/api/v1/metrics/query-stats?${searchParams.toString()}`;

        setLoading(true);
        setError(null);

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

            if (isMountedRef.current) {
                setStats(result);
            }
        } catch (err) {
            logger.error('Error fetching query stats:', err);
            if (isMountedRef.current) {
                setError(
                    (err as Error).message
                    || 'Failed to fetch query stats'
                );
                setStats(null);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, connectionId, queryid, timeRange]);

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
