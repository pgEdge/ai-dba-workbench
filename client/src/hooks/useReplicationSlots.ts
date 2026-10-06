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
import {
    normaliseSlotRows,
    type ReplicationSlotRow,
    type ReplicationSlotsResponse,
} from '../components/Dashboard/ServerDashboard/replicationSlots';
import { apiGet } from '../utils/apiClient';
import { logger } from '../utils/logger';
import { useRequestSequence } from './useRequestSequence';

/**
 * The most slots one request returns. It is the latest-snapshot
 * endpoint's own ceiling; a server with more slots than this shows
 * the first ones by name and says how many there are in all.
 */
export const REPLICATION_SLOT_LIMIT = 100;

export interface UseReplicationSlotsReturn {
    slots: ReplicationSlotRow[];
    totalCount: number;
    loading: boolean;
    error: string | null;
}

/** Fallback message used when a rejection carries no usable text. */
const FETCH_ERROR_FALLBACK = 'Failed to fetch replication slots';

/** Build the latest-snapshot request URL for a server's slots. */
export const buildReplicationSlotsUrl = (connectionId: number): string => {
    const params = new URLSearchParams({
        probe_name: 'pg_replication_slots',
        connection_id: connectionId.toString(),
        order_by: 'slot_name',
        order: 'asc',
        limit: REPLICATION_SLOT_LIMIT.toString(),
    });
    return `/api/v1/metrics/latest?${params.toString()}`;
};

/**
 * Fetch the replication slots of a server from the collector's most
 * recent pg_replication_slots snapshot. The latest-snapshot endpoint
 * only looks back an hour, so the list describes the server's slots
 * now rather than over the dashboard's selected period, and it
 * refetches when the connection or the dashboard refresh trigger
 * changes. The spinner shows only for the first load of a connection,
 * so periodic refreshes do not make the section flicker.
 */
export const useReplicationSlots = (
    connectionId: number,
): UseReplicationSlotsReturn => {
    const { user } = useAuth();
    const { refreshTrigger } = useDashboard();
    const { beginRequest } = useRequestSequence();

    const [slots, setSlots] = useState<ReplicationSlotRow[]>([]);
    const [totalCount, setTotalCount] = useState<number>(0);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const initialLoadDoneRef = useRef<boolean>(false);

    const isLoggedIn = !!user;

    const fetchData = useCallback(async (): Promise<void> => {
        const isCurrent = beginRequest();

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const result = await apiGet<ReplicationSlotsResponse>(
                buildReplicationSlotsUrl(connectionId),
            );
            if (!isCurrent()) { return; }
            const rows = normaliseSlotRows(result.rows);
            setSlots(rows);
            setTotalCount(Math.max(result.total_count ?? 0, rows.length));
            initialLoadDoneRef.current = true;
        } catch (err) {
            logger.error('Error fetching replication slots:', err);
            if (!isCurrent()) { return; }
            setError((err as Error).message || FETCH_ERROR_FALLBACK);
            setSlots([]);
            setTotalCount(0);
        } finally {
            if (isCurrent()) {
                setLoading(false);
            }
        }
    }, [beginRequest, connectionId]);

    /*
     * A different server is a fresh load: drop the previous server's
     * slots so they never render under the new one, and allow the
     * spinner to show again.
     */
    useEffect(() => {
        initialLoadDoneRef.current = false;
        setSlots([]);
        setTotalCount(0);
    }, [connectionId]);

    useEffect(() => {
        if (isLoggedIn) {
            void fetchData();
        }
    }, [isLoggedIn, fetchData, refreshTrigger]);

    return { slots, totalCount, loading, error };
};

export default useReplicationSlots;
