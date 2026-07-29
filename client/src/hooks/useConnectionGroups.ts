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
import type {
    ConnectionGroupBy,
    ConnectionGroupRow,
    ConnectionGroupsResponse,
} from '../components/Dashboard/ServerDashboard/types';
import { apiGet } from '../utils/apiClient';
import { logger } from '../utils/logger';

/** Parameters identifying the server, grouping, and period to fetch. */
export interface ConnectionGroupsParams {
    connectionId: number;
    groupBy: ConnectionGroupBy;
    timeRange: TimeRange;
}

export interface UseConnectionGroupsReturn {
    groups: ConnectionGroupRow[];
    collectedAt: string | null;
    loading: boolean;
    error: string | null;
    refetch: () => void;
}

/**
 * Fetch active connection counts for a server, grouped by database
 * user, client address, or database, taken from the most recent
 * collected snapshot within the selected period.
 *
 * The hook refetches whenever the connection, the grouping, the period,
 * or the dashboard refresh trigger changes. The spinner is only shown
 * for the first load of a given connection and grouping, so periodic
 * auto-refreshes do not make the section flicker.
 */
export const useConnectionGroups = (
    params: ConnectionGroupsParams | null,
): UseConnectionGroupsReturn => {
    const { user } = useAuth();
    const { refreshTrigger } = useDashboard();

    const [groups, setGroups] = useState<ConnectionGroupRow[]>([]);
    const [collectedAt, setCollectedAt] = useState<string | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);

    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);
    const userRef = useRef(user);
    userRef.current = user;

    const isLoggedIn = !!user;
    const connectionId = params?.connectionId;
    const groupBy = params?.groupBy;
    const timeRange = params?.timeRange;

    const fetchData = useCallback(async (): Promise<void> => {
        if (
            !userRef.current
            || connectionId === undefined
            || !groupBy
            || !timeRange
        ) {
            return;
        }

        const searchParams = new URLSearchParams({
            connection_id: connectionId.toString(),
            group_by: groupBy,
            time_range: timeRange,
        });
        const url =
            `/api/v1/metrics/connection-groups?${searchParams.toString()}`;

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const result = await apiGet<ConnectionGroupsResponse>(url);

            if (isMountedRef.current) {
                setGroups(
                    Array.isArray(result.groups) ? result.groups : [],
                );
                setCollectedAt(result.collected_at ?? null);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            logger.error('Error fetching connection groups:', err);
            if (isMountedRef.current) {
                setError(
                    (err as Error).message
                    || 'Failed to fetch connection groups',
                );
                setGroups([]);
                setCollectedAt(null);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [connectionId, groupBy, timeRange]);

    const refetch = useCallback((): void => {
        void fetchData();
    }, [fetchData]);

    /*
     * Treat a change of connection or grouping as a fresh load: drop the
     * previous rows so a stale grouping never renders under the newly
     * selected tab, and allow the spinner to show again.
     */
    useEffect(() => {
        initialLoadDoneRef.current = false;
        setGroups([]);
        setCollectedAt(null);
    }, [connectionId, groupBy]);

    useEffect(() => {
        isMountedRef.current = true;

        if (isLoggedIn) {
            void fetchData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [isLoggedIn, fetchData, refreshTrigger]);

    return { groups, collectedAt, loading, error, refetch };
};

export default useConnectionGroups;
