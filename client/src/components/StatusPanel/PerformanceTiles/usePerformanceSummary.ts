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
import { useAuth } from '../../../contexts/AuthContext';
import { apiFetch } from '../../../utils/apiClient';
import { useClusterData } from '../../../contexts/ClusterDataContext';
import { PerformanceSummaryData } from './types';
import { extractEstateServerIds } from '../../../utils/clusterHelpers';

interface UsePerformanceSummaryReturn {
    data: PerformanceSummaryData | null;
    loading: boolean;
    error: string | null;
}

/**
 * Custom hook for fetching performance summary data.
 * Follows the useTimelineEvents pattern with initialLoadDoneRef
 * to prevent flash on auto-refresh.
 */
export const usePerformanceSummary = (
    selection: Record<string, unknown> | null
): UsePerformanceSummaryReturn => {
    const { user } = useAuth();
    const { lastRefresh } = useClusterData();
    const [data, setData] = useState<PerformanceSummaryData | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);

    const buildUrl = useCallback((): string | null => {
        if (!selection) {return null;}

        const base = '/api/v1/metrics/performance-summary';

        if (selection.type === 'server') {
            if (selection.id === undefined || selection.id === null) {return null;}
            return `${base}?connection_id=${selection.id}&time_range=24h`;
        }

        if (selection.type === 'cluster') {
            const serverIds = selection.serverIds as number[] | undefined;
            if (!serverIds?.length) {return null;}
            return `${base}?connection_ids=${serverIds.join(',')}&time_range=24h`;
        }

        if (selection.type === 'estate') {
            const serverIds = extractEstateServerIds(selection);
            if (!serverIds.length) {return null;}
            return `${base}?connection_ids=${serverIds.join(',')}&time_range=24h`;
        }

        return null;
    }, [selection]);

    const fetchData = useCallback(async (): Promise<void> => {
        if (!user) {return;}

        const url = buildUrl();
        if (!url) {
            setData(null);
            return;
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

            if (isMountedRef.current) {
                const result: PerformanceSummaryData = await response.json();
                setData(result);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            console.error('Error fetching performance summary:', err);
            if (isMountedRef.current) {
                setError((err as Error).message || 'Failed to fetch performance data');
                setData(null);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, buildUrl]);

    // Reset initial load state when selection changes
    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [selection?.type, selection?.id]);

    // Fetch when dependencies change
    useEffect(() => {
        isMountedRef.current = true;

        if (user && selection) {
            fetchData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, selection, fetchData, lastRefresh]);

    return { data, loading, error };
};

export default usePerformanceSummary;
