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
import { useAuth } from '../../../contexts/useAuth';
import { apiFetch } from '../../../utils/apiClient';
import { useClusterData } from '../../../contexts/useClusterData';
import type { PerformanceSummaryData } from './types';
import { extractEstateServerIds } from '../../../utils/clusterHelpers';
import { logger } from '../../../utils/logger';
import { useRetryingFetch } from '../../../hooks/useRetryingFetch';
import type { Selection } from '../../../types/selection';

interface UsePerformanceSummaryReturn {
    data: PerformanceSummaryData | null;
    loading: boolean;
    error: string | null;
    /** True while an automatic retry is pending after a failed fetch. */
    retrying: boolean;
}

/**
 * Custom hook for fetching performance summary data.
 * Follows the useTimelineEvents pattern with initialLoadDoneRef
 * to prevent flash on auto-refresh.
 */
export const usePerformanceSummary = (
    selection: Selection | null
): UsePerformanceSummaryReturn => {
    const { user } = useAuth();
    const { lastRefresh } = useClusterData();
    const [data, setData] = useState<PerformanceSummaryData | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);
    const { run, retrying } = useRetryingFetch({
        resetKey: lastRefresh,
        enabled: !!user && !!selection,
    });

    const buildUrl = useCallback((): string | null => {
        if (!selection) {return null;}

        const base = '/api/v1/metrics/performance-summary';

        if (selection.type === 'server') {
            if (selection.id === undefined || selection.id === null) {return null;}
            return `${base}?connection_id=${selection.id}&time_range=24h`;
        }

        if (selection.type === 'cluster') {
            if (!selection.serverIds?.length) {return null;}
            return `${base}?connection_ids=${selection.serverIds.join(',')}&time_range=24h`;
        }

        if (selection.type === 'estate') {
            const serverIds = extractEstateServerIds(selection);
            if (!serverIds.length) {return null;}
            return `${base}?connection_ids=${serverIds.join(',')}&time_range=24h`;
        }

        return null;
    }, [selection]);

    const fetchData = useCallback(async (): Promise<boolean> => {
        if (!user) {return true;}

        const url = buildUrl();
        if (!url) {
            setData(null);
            return true;
        }

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const response = await apiFetch(url);

            if (!response.ok) {
                const errorData = await response.json().catch(() => ({})) as { error?: string };
                throw new Error(errorData.error || `Failed to fetch performance data: ${response.status}`);
            }

            const result: PerformanceSummaryData = await response.json();
            // Re-check mount state after the final await so a late
            // resolution cannot call setState on an unmounted component.
            if (isMountedRef.current) {
                setData(result);
                initialLoadDoneRef.current = true;
            }
            return true;
        } catch (err) {
            logger.error('Error fetching performance summary:', err);
            if (isMountedRef.current) {
                setError((err as Error).message || 'Failed to fetch performance data');
                setData(null);
            }
            return false;
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, buildUrl]);

    // Reset initial load state when selection changes
    const selectionId = selection && 'id' in selection ? selection.id : undefined;
    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [selection?.type, selectionId]);

    // Fetch when dependencies change
    useEffect(() => {
        isMountedRef.current = true;

        if (user && selection) {
            void run(fetchData);
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, selection, run, fetchData, lastRefresh]);

    return { data, loading, error, retrying };
};

export default usePerformanceSummary;
