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
import { apiFetch } from '../utils/apiClient';
import { logger } from '../utils/logger';
import type { SparklinePoint, TimeRangeState } from '../components/Dashboard/types';
import type { ServerCacheHitSummary } from '../components/Dashboard/ServerDashboard/types';

/** Value returned by {@link useServerCacheHit}. */
export interface UseServerCacheHitResult {
    /**
     * Per-bucket cache hit ratios for the connection, in chronological
     * order. A null value is a bucket with no block access and is drawn
     * as a gap.
     */
    points: SparklinePoint[];
    /** True whilst the first load for a connection is in flight. */
    loading: boolean;
    /** Human-readable error message, or null when the load succeeded. */
    error: string | null;
}

/** The presets the performance-summary endpoint accepts, shortest first. */
const SUMMARY_PRESETS: { range: string; ms: number }[] = [
    { range: '1h', ms: 60 * 60 * 1000 },
    { range: '6h', ms: 6 * 60 * 60 * 1000 },
    { range: '24h', ms: 24 * 60 * 60 * 1000 },
    { range: '7d', ms: 7 * 24 * 60 * 60 * 1000 },
    { range: '30d', ms: 30 * 24 * 60 * 60 * 1000 },
];

/** The preset used when a custom window cannot be measured. */
const DEFAULT_SUMMARY_RANGE = '24h';

/**
 * Map the dashboard time range onto a preset the performance-summary
 * endpoint accepts. The endpoint has no custom-window support (only
 * `/api/v1/metrics/query` does), so a custom range is approximated by
 * the shortest preset that covers its span, ending now rather than at
 * the custom end. A custom range with missing or unparsable bounds
 * falls back to 24 hours.
 */
export const summaryTimeRange = (timeRange: TimeRangeState): string => {
    if (timeRange.range !== 'custom') { return timeRange.range; }

    const start = Date.parse(timeRange.customStart ?? '');
    const end = Date.parse(timeRange.customEnd ?? '');
    if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) {
        return DEFAULT_SUMMARY_RANGE;
    }
    const span = end - start;
    const preset = SUMMARY_PRESETS.find(p => p.ms >= span);
    return preset?.range ?? SUMMARY_PRESETS[SUMMARY_PRESETS.length - 1].range;
};

/** Build the performance-summary endpoint URL for one connection. */
const buildSummaryUrl = (connectionId: number, range: string): string => (
    `/api/v1/metrics/performance-summary`
    + `?connection_id=${connectionId}&time_range=${range}`
);

/** Throw a descriptive error when the response reports a failure. */
const assertResponseOk = async (response: Response): Promise<void> => {
    if (response.ok) { return; }

    const errorData = await response.json().catch(
        () => ({})
    ) as { error?: string };

    throw new Error(
        errorData.error
        ?? `Failed to fetch cache hit ratio: ${response.status}`
    );
};

/**
 * Read the connection's cache hit series from a successful response.
 * The entry is matched by connection ID rather than taken by position,
 * so a response for another connection cannot be misattributed.
 */
const extractPoints = async (
    response: Response,
    connectionId: number,
): Promise<SparklinePoint[]> => {
    const result: ServerCacheHitSummary = await response.json();
    const conn = (result.connections ?? [])
        .find(c => c.connection_id === connectionId);
    return (conn?.cache_hit_ratio?.time_series ?? [])
        .map(p => ({ time: p.time, value: p.value }));
};

/** Convert a thrown value into a message suitable for display. */
const toErrorMessage = (err: unknown): string => (
    (err as Error).message || 'Failed to fetch cache hit ratio'
);

/**
 * Fetch the server-wide cache hit ratio series for a connection from
 * `/api/v1/metrics/performance-summary`.
 *
 * The server computes the ratio from per-interval deltas of the
 * pg_stat_database block counters, differenced per database before
 * they are summed (issue #401). The dashboard must not derive a
 * server-wide ratio from `blks_hit_per_sec` and `blks_read_per_sec`
 * requested without a database, because that path sums the counters
 * across databases first and a database created or dropped inside the
 * window corrupts the delta.
 *
 * Callers pass the dashboard `refreshTrigger` as `refreshKey` so the
 * series follows the dashboard refresh cycle.
 */
export const useServerCacheHit = (
    connectionId: number,
    timeRange: TimeRangeState,
    refreshKey = 0,
): UseServerCacheHitResult => {
    const { user } = useAuth();

    const [points, setPoints] = useState<SparklinePoint[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    // Identifies the most recently started request, so that a slow
    // response for one connection or range cannot overwrite a faster
    // one for the next; see useDatabaseSummaries for the reasoning.
    const requestIdRef = useRef<number>(0);
    const initialLoadDoneRef = useRef<boolean>(false);
    const userRef = useRef(user);
    userRef.current = user;

    const isLoggedIn = !!user;
    const range = summaryTimeRange(timeRange);

    const fetchData = useCallback(async (): Promise<void> => {
        if (!userRef.current) { return; }

        const url = buildSummaryUrl(connectionId, range);

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        const requestId = ++requestIdRef.current;
        const isCurrent = (): boolean =>
            isMountedRef.current && requestIdRef.current === requestId;

        try {
            const response = await apiFetch(url);
            await assertResponseOk(response);

            if (isCurrent()) {
                const rows = await extractPoints(response, connectionId);
                if (isCurrent()) {
                    setPoints(rows);
                    initialLoadDoneRef.current = true;
                }
            }
        } catch (err) {
            logger.error('Error fetching cache hit ratio:', err);
            if (isCurrent()) {
                setError(toErrorMessage(err));
                setPoints([]);
            }
        } finally {
            if (isCurrent()) {
                setLoading(false);
            }
        }
    }, [connectionId, range]);

    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [connectionId]);

    useEffect(() => {
        isMountedRef.current = true;

        if (isLoggedIn) {
            fetchData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [isLoggedIn, fetchData, refreshKey]);

    return { points, loading, error };
};

export default useServerCacheHit;
