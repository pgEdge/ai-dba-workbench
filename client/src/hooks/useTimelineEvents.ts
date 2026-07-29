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
import { useClusterData } from '../contexts/useClusterData';
import { apiGet } from '../utils/apiClient';
import { logger } from '../utils/logger';
import { useRetryingFetch } from './useRetryingFetch';
import {
    resolveTimeRangeBounds,
    type TimelineTimeRange,
} from '../utils/timelineRange';

export interface TimelineEvent {
    id: number;
    event_type: string;
    connection_id: number;
    title: string;
    description?: string;
    severity?: string;
    timestamp: string;
    [key: string]: unknown;
}

/*
 * The range types live in utils/timelineRange.ts alongside the bounds
 * calculation; they are re-exported here because existing callers import
 * them from this hook.
 */
export type { CustomTimeRange, TimeRangePreset } from '../utils/timelineRange';
export type TimeRange = TimelineTimeRange;

export interface UseTimelineEventsOptions {
    connectionId?: number | null;
    connectionIds?: number[] | null;
    timeRange?: TimeRange;
    eventTypes?: string[];
    enabled?: boolean;
}

export interface UseTimelineEventsReturn {
    events: TimelineEvent[];
    loading: boolean;
    error: string | null;
    refetch: () => Promise<void>;
    totalCount: number;
    /** True while an automatic retry is pending after a failed fetch. */
    retrying: boolean;
}

interface TimelineApiResponse {
    events?: TimelineEvent[];
    total_count?: number;
}

/**
 * Calculate start and end times based on the time range parameter,
 * expressed as the ISO strings the timeline API expects.
 */
const calculateTimeRange = (timeRange: TimeRange): { startTime: string; endTime: string } => {
    const { startTime, endTime } = resolveTimeRangeBounds(timeRange);

    return {
        startTime: startTime.toISOString(),
        endTime: endTime.toISOString(),
    };
};

/**
 * Custom hook for fetching timeline events
 */
export const useTimelineEvents = ({
    connectionId = null,
    connectionIds = null,
    timeRange = '24h',
    eventTypes = ['all'],
    enabled = true,
}: UseTimelineEventsOptions = {}): UseTimelineEventsReturn => {
    const { user } = useAuth();
    const { lastRefresh } = useClusterData();
    const [events, setEvents] = useState<TimelineEvent[]>([]);
    const [totalCount, setTotalCount] = useState<number>(0);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);
    const { run, retrying } = useRetryingFetch({
        resetKey: lastRefresh,
        enabled: enabled && !!user,
    });

    // Create a stable string representation of eventTypes for dependency comparison
    // This ensures the callback is recreated when event types change, regardless of
    // array reference equality issues
    const eventTypesKey = eventTypes ? eventTypes.slice().sort().join(',') : '';

    // Stable string representation of connectionIds for the same reason: a
    // parent passing a new-but-equal array each render must not force a
    // redundant refetch through buildQueryString's identity changing.
    const connectionIdsKey = connectionIds ? connectionIds.slice().sort().join(',') : '';

    /**
     * Build the query string for the API request
     */
    const buildQueryString = useCallback((): string => {
        const { startTime, endTime } = calculateTimeRange(timeRange);
        const params = new URLSearchParams();

        params.append('start_time', startTime);
        params.append('end_time', endTime);
        params.append('limit', '500');

        // Handle connection ID(s)
        if (connectionId !== null) {
            params.append('connection_id', connectionId.toString());
        } else if (connectionIds !== null && connectionIds.length > 0) {
            // For multiple connection IDs, pass them as comma-separated
            params.append('connection_ids', connectionIds.join(','));
        }

        // Handle event types filter
        if (eventTypes && eventTypes.length > 0 && !eventTypes.includes('all')) {
            params.append('event_types', eventTypes.join(','));
        }

        return params.toString();
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [connectionId, connectionIdsKey, timeRange, eventTypesKey]);

    /**
     * Fetch timeline events from the API
     */
    const fetchEvents = useCallback(async (): Promise<boolean> => {
        if (!user || !enabled) {return true;}

        // Only show loading state on the very first fetch ever (use ref to avoid re-renders)
        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const queryString = buildQueryString();
            const data = await apiGet<TimelineApiResponse>(`/api/v1/timeline/events?${queryString}`);

            if (isMountedRef.current) {
                setEvents(data.events ?? []);
                setTotalCount(data.total_count ?? 0);
                initialLoadDoneRef.current = true;
            }
            return true;
        } catch (err) {
            logger.error('Error fetching timeline events:', err);
            if (isMountedRef.current) {
                setError((err as Error).message || 'Failed to fetch timeline events');
                setEvents([]);
                setTotalCount(0);
            }
            return false;
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, enabled, buildQueryString]);

    /**
     * Manual refetch function. Routes through the retry controller so a
     * manual refresh resets any pending backoff schedule.
     */
    const refetch = useCallback(async (): Promise<void> => {
        await run(fetchEvents);
    }, [run, fetchEvents]);

    // Reset initial load state when connection changes
    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [connectionId, connectionIdsKey]);

    // Fetch when dependencies change
    // Note: fetchEvents already captures connectionId, connectionIds, timeRange, eventTypes via buildQueryString
    // So we only need fetchEvents in deps to avoid duplicate triggers
    useEffect(() => {
        isMountedRef.current = true;

        if (enabled && user) {
            void run(fetchEvents);
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [enabled, user, run, fetchEvents, lastRefresh]);

    return {
        events,
        loading,
        error,
        refetch,
        totalCount,
        retrying,
    };
};

export default useTimelineEvents;
