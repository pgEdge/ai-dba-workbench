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

/** Fallback message used when a rejection carries no usable text. */
const FETCH_ERROR_FALLBACK = 'Failed to fetch connection groups';

/**
 * Build the request URL for a grouping, or return null when the
 * parameters are incomplete and no request should be made.
 */
const buildRequestUrl = (
    connectionId: number | undefined,
    groupBy: ConnectionGroupBy | undefined,
    timeRange: TimeRange | undefined,
): string | null => {
    if (connectionId === undefined || !groupBy || !timeRange) {
        return null;
    }

    const searchParams = new URLSearchParams({
        connection_id: connectionId.toString(),
        group_by: groupBy,
        time_range: timeRange,
    });
    return `/api/v1/metrics/connection-groups?${searchParams.toString()}`;
};

/**
 * Normalise a response payload into the shape the hook exposes. The
 * server contract allows a null `collected_at`, and a malformed or
 * absent `groups` field is defensively treated as an empty list.
 */
const normaliseResponse = (
    payload: ConnectionGroupsResponse,
): { groups: ConnectionGroupRow[]; collectedAt: string | null } => ({
    groups: Array.isArray(payload.groups) ? payload.groups : [],
    collectedAt: payload.collected_at ?? null,
});

/** Derive the message to surface for a failed request. */
const describeFetchError = (err: unknown): string =>
    (err as Error).message || FETCH_ERROR_FALLBACK;

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

    /*
     * Monotonic request counter. Each call to fetchData claims the next
     * value and only commits its outcome whilst it still holds the
     * latest one, so a slow response for a superseded grouping, period,
     * or connection cannot overwrite fresher state once the user has
     * moved on.
     */
    const requestIdRef = useRef<number>(0);

    const isLoggedIn = !!user;
    const connectionId = params?.connectionId;
    const groupBy = params?.groupBy;
    const timeRange = params?.timeRange;

    const fetchData = useCallback(async (): Promise<void> => {
        if (!userRef.current) { return; }

        const url = buildRequestUrl(connectionId, groupBy, timeRange);
        if (!url) { return; }

        const requestId = ++requestIdRef.current;

        /**
         * Whether this request may still commit: the hook must be
         * mounted and no later request may have started since.
         */
        const isCurrent = (): boolean =>
            isMountedRef.current && requestIdRef.current === requestId;

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const result = await apiGet<ConnectionGroupsResponse>(url);
            const { groups: rows, collectedAt: snapshot } =
                normaliseResponse(result);

            if (isCurrent()) {
                setGroups(rows);
                setCollectedAt(snapshot);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            logger.error('Error fetching connection groups:', err);
            if (isCurrent()) {
                setError(describeFetchError(err));
                setGroups([]);
                setCollectedAt(null);
            }
        } finally {
            if (isCurrent()) {
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
