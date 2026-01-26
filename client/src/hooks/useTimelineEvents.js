/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useState, useCallback, useEffect, useRef } from 'react';
import { useAuth } from '../contexts/AuthContext';

/**
 * Calculate start and end times based on the time range parameter
 * @param {string|object} timeRange - '1h', '6h', '24h', '7d', or { start: Date, end: Date }
 * @returns {{ startTime: string, endTime: string }} ISO formatted timestamps
 */
const calculateTimeRange = (timeRange) => {
    const now = new Date();
    let startTime;
    let endTime = now;

    if (typeof timeRange === 'object' && timeRange.start && timeRange.end) {
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
 * @param {object} options - Hook options
 * @param {number|null} options.connectionId - Single server filter
 * @param {number[]|null} options.connectionIds - Multiple servers for cluster/estate
 * @param {string|object} options.timeRange - '1h', '6h', '24h', '7d', or { start: Date, end: Date }
 * @param {string[]} options.eventTypes - Array of event types to filter, or ['all']
 * @param {boolean} options.enabled - Whether to fetch (default true)
 * @returns {{ events: array, loading: boolean, error: string|null, refetch: function, totalCount: number }}
 */
export const useTimelineEvents = ({
    connectionId = null,
    connectionIds = null,
    timeRange = '24h',
    eventTypes = ['all'],
    enabled = true,
} = {}) => {
    const { user } = useAuth();
    const [events, setEvents] = useState([]);
    const [totalCount, setTotalCount] = useState(0);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);
    const isMountedRef = useRef(true);
    const initialLoadDoneRef = useRef(false);
    const refreshInterval = 60000; // 60 seconds

    /**
     * Build the query string for the API request
     */
    const buildQueryString = useCallback(() => {
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
    }, [connectionId, connectionIds, timeRange, eventTypes]);

    /**
     * Fetch timeline events from the API
     */
    const fetchEvents = useCallback(async () => {
        if (!user || !enabled) return;

        // Only show loading state on the very first fetch ever (use ref to avoid re-renders)
        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const queryString = buildQueryString();
            const response = await fetch(`/api/v1/timeline/events?${queryString}`, {
                credentials: 'include',
            });

            if (!response.ok) {
                const errorData = await response.json().catch(() => ({}));
                throw new Error(errorData.error || `Failed to fetch events: ${response.status}`);
            }

            if (isMountedRef.current) {
                const data = await response.json();
                setEvents(data.events || []);
                setTotalCount(data.total_count || 0);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            console.error('Error fetching timeline events:', err);
            if (isMountedRef.current) {
                setError(err.message || 'Failed to fetch timeline events');
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
    const refetch = useCallback(() => {
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
    }, [enabled, user, fetchEvents]);

    // Auto-refresh when enabled (no loading indicator)
    useEffect(() => {
        if (!enabled || !user) return;

        const intervalId = setInterval(fetchEvents, refreshInterval);
        return () => clearInterval(intervalId);
    }, [enabled, user, fetchEvents]);

    return {
        events,
        loading,
        error,
        refetch,
        totalCount,
    };
};

export default useTimelineEvents;
