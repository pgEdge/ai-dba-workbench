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
import type {
    DatabaseSummary,
    ServerPerformanceSummary,
} from '../components/Dashboard/ServerDashboard/types';

/** Value returned by {@link useDatabaseSummaries}. */
export interface UseDatabaseSummariesResult {
    /** Per-database summaries for the connection. */
    databases: DatabaseSummary[];
    /** True whilst the first load for a connection is in flight. */
    loading: boolean;
    /** Human-readable error message, or null when the load succeeded. */
    error: string | null;
}

/** Build the database-summaries endpoint URL for a connection. */
const buildSummariesUrl = (
    connectionId: number,
    timeRange: string,
): string => (
    `/api/v1/metrics/database-summaries`
    + `?connection_id=${connectionId}&time_range=${timeRange}`
);

/**
 * Throw a descriptive error when the response reports a failure.
 *
 * The server sends a JSON body with an `error` field for most
 * failures; when the body is missing or unparseable, fall back to a
 * message built from the HTTP status.
 */
const assertResponseOk = async (response: Response): Promise<void> => {
    if (response.ok) { return; }

    const errorData = await response.json().catch(
        () => ({})
    ) as { error?: string };

    throw new Error(
        errorData.error
        ?? `Failed to fetch database summaries: ${response.status}`
    );
};

/** Read the summaries from a successful response body. */
const extractDatabases = async (
    response: Response,
): Promise<DatabaseSummary[]> => {
    const result: ServerPerformanceSummary = await response.json();
    return result.databases ?? [];
};

/** Convert a thrown value into a message suitable for display. */
const toErrorMessage = (err: unknown): string => (
    (err as Error).message || 'Failed to fetch database summaries'
);

/**
 * Fetch the per-database performance summaries for a connection.
 *
 * The database-summaries endpoint is the canonical source for "which
 * databases does this connection monitor", so this hook is shared by
 * the Database Summaries panel (which renders the full summaries) and
 * by any panel that only needs the database names, such as the Top
 * Queries database filter.
 *
 * Callers that want the data to follow the dashboard refresh cycle
 * pass the dashboard `refreshTrigger` as `refreshKey`; callers that
 * only need the list to track the selected connection can omit it.
 */
export const useDatabaseSummaries = (
    connectionId: number,
    refreshKey = 0,
    timeRange = '24h',
): UseDatabaseSummariesResult => {
    const { user } = useAuth();

    const [databases, setDatabases] = useState<DatabaseSummary[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);
    const userRef = useRef(user);
    userRef.current = user;

    const isLoggedIn = !!user;

    const fetchData = useCallback(async (): Promise<void> => {
        if (!userRef.current) { return; }

        const url = buildSummariesUrl(connectionId, timeRange);

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const response = await apiFetch(url);
            await assertResponseOk(response);

            if (isMountedRef.current) {
                setDatabases(await extractDatabases(response));
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            logger.error('Error fetching database summaries:', err);
            if (isMountedRef.current) {
                setError(toErrorMessage(err));
                setDatabases([]);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [connectionId, timeRange]);

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

    return { databases, loading, error };
};

export default useDatabaseSummaries;
