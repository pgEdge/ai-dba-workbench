/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useCallback, useEffect } from 'react';
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

/** The settled outcome of the latest request, and the server it was for. */
interface SlotFetchResult {
    connectionId: number;
    slots: ReplicationSlotRow[];
    totalCount: number;
    error: string | null;
}

/** Shared empty list, so a pending load does not change identity per render. */
const NO_SLOTS: ReplicationSlotRow[] = [];

/**
 * Fetch the replication slots of a server from the collector's most
 * recent pg_replication_slots snapshot. The latest-snapshot endpoint
 * only looks back an hour, so the list describes the server's slots
 * now rather than over the dashboard's selected period, and it
 * refetches when the connection or the dashboard refresh trigger
 * changes.
 *
 * Each result records the connection it was fetched for, and the hook
 * returns it only while that is still the selected connection. A
 * different server therefore reads as loading, with no slots and no
 * error, from its very first render rather than after an effect has
 * reset the state; and because a refresh of the same server keeps the
 * previous result until the new one arrives, the spinner shows only
 * for the first load of a connection and periodic refreshes do not
 * make the section flicker.
 */
export const useReplicationSlots = (
    connectionId: number,
): UseReplicationSlotsReturn => {
    const { user } = useAuth();
    const { refreshTrigger } = useDashboard();
    const { beginRequest } = useRequestSequence();

    const [result, setResult] = useState<SlotFetchResult | null>(null);

    const isLoggedIn = !!user;

    const fetchData = useCallback(async (): Promise<void> => {
        const isCurrent = beginRequest();

        try {
            const response = await apiGet<ReplicationSlotsResponse>(
                buildReplicationSlotsUrl(connectionId),
            );
            if (!isCurrent()) { return; }
            const rows = normaliseSlotRows(response.rows);
            setResult({
                connectionId,
                slots: rows,
                totalCount: Math.max(response.total_count ?? 0, rows.length),
                error: null,
            });
        } catch (err) {
            logger.error('Error fetching replication slots:', err);
            if (!isCurrent()) { return; }
            setResult({
                connectionId,
                slots: NO_SLOTS,
                totalCount: 0,
                error: (err as Error).message || FETCH_ERROR_FALLBACK,
            });
        }
    }, [beginRequest, connectionId]);

    useEffect(() => {
        if (isLoggedIn) {
            void fetchData();
        }
    }, [isLoggedIn, fetchData, refreshTrigger]);

    const current = result?.connectionId === connectionId ? result : null;

    return {
        slots: current?.slots ?? NO_SLOTS,
        totalCount: current?.totalCount ?? 0,
        loading: isLoggedIn && current === null,
        error: current?.error ?? null,
    };
};

export default useReplicationSlots;
