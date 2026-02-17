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
import { useAuth } from '../contexts/AuthContext';
import { useClusterData } from '../contexts/ClusterDataContext';
import { apiGet } from '../utils/apiClient';

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

interface CustomTimeRange {
    start: Date;
    end: Date;
}

export type TimeRangePreset = '1h' | '6h' | '24h' | '7d' | '30d';
export type TimeRange = TimeRangePreset | CustomTimeRange;

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
}

interface TimelineApiResponse {
    events?: TimelineEvent[];
    total_count?: number;
}

/**
 * Calculate start and end times based on the time range parameter
 */
const calculateTimeRange = (timeRange: TimeRange): { startTime: string; endTime: string } => {
    const now = new Date();
    let startTime: Date | string;
    let endTime: Date | string = now;

    if (typeof timeRange === 'object' && 'start' in timeRange && 'end' in timeRange) {
        // Custom range with start and end dates
        startTime = timeRange.start;
        endTime = timeRange.end;
    } else {
        // Predefined ranges
        switch (timeRange) {
            case '1h':
                startTime = new Date(now.getTime() - 60 * 60 * 1000);
                break;
            case '6h':
                startTime = new Date(now.getTime() - 6 * 60 * 60 * 1000);
                break;
            case '24h':
                startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000);
                break;
            case '7d':
                startTime = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
                break;
            case '30d':
                startTime = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
                break;
            default:
                // Default to last 24 hours
                startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000);
        }
    }

    return {
        startTime: startTime instanceof Date ? startTime.toISOString() : startTime,
        endTime: endTime instanceof Date ? endTime.toISOString() : endTime,
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

    // Create a stable string representation of eventTypes for dependency comparison
    // This ensures the callback is recreated when event types change, regardless of
    // array reference equality issues
    const eventTypesKey = eventTypes ? eventTypes.slice().sort().join(',') : '';

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
    }, [connectionId, connectionIds, timeRange, eventTypesKey]);

    /**
     * Fetch timeline events from the API
     */
    const fetchEvents = useCallback(async (): Promise<void> => {
        if (!user || !enabled) {return;}

        // Only show loading state on the very first fetch ever (use ref to avoid re-renders)
        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const queryString = buildQueryString();
            const data = await apiGet<TimelineApiResponse>(`/api/v1/timeline/events?${queryString}`);

            if (isMountedRef.current) {
                setEvents(data.events || []);
                setTotalCount(data.total_count || 0);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            console.error('Error fetching timeline events:', err);
            if (isMountedRef.current) {
                setError((err as Error).message || 'Failed to fetch timeline events');
                setEvents([]);
                setTotalCount(0);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, enabled, buildQueryString]);

    /**
     * Manual refetch function
     */
    const refetch = useCallback((): Promise<void> => {
        return fetchEvents();
    }, [fetchEvents]);

    // Reset initial load state when connection changes
    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [connectionId, connectionIds]);

    // Fetch when dependencies change
    // Note: fetchEvents already captures connectionId, connectionIds, timeRange, eventTypes via buildQueryString
    // So we only need fetchEvents in deps to avoid duplicate triggers
    useEffect(() => {
        isMountedRef.current = true;

        if (enabled && user) {
            fetchEvents();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [enabled, user, fetchEvents, lastRefresh]);

    return {
        events,
        loading,
        error,
        refetch,
        totalCount,
    };
};

export default useTimelineEvents;
