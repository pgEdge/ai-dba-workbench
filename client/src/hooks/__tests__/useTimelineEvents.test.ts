/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { useTimelineEvents, type TimelineEvent, type TimeRangePreset } from '../useTimelineEvents';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiGet = vi.fn();

vi.mock('../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
}));

const mockUser = { id: 1, username: 'testuser' };

vi.mock('../../contexts/useAuth', () => ({
    useAuth: () => ({ user: mockUser }),
}));

let mockLastRefresh = 0;

vi.mock('../../contexts/useClusterData', () => ({
    useClusterData: () => ({ lastRefresh: mockLastRefresh }),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeTimelineEvents(): TimelineEvent[] {
    return [
        {
            id: 1,
            event_type: 'alert',
            connection_id: 1,
            title: 'High CPU alert',
            description: 'CPU usage exceeded 90%',
            severity: 'warning',
            timestamp: '2024-01-01T12:00:00Z',
        },
        {
            id: 2,
            event_type: 'restart',
            connection_id: 1,
            title: 'Server restart',
            timestamp: '2024-01-01T11:00:00Z',
        },
    ];
}

function makeApiResponse(events: TimelineEvent[] = makeTimelineEvents()) {
    return {
        events,
        total_count: events.length,
    };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useTimelineEvents', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockLastRefresh = 0;
    });

    it('returns initial state with default options', () => {
        mockApiGet.mockResolvedValueOnce(makeApiResponse());

        const { result } = renderHook(() => useTimelineEvents());

        expect(result.current.events).toEqual([]);
        expect(result.current.totalCount).toBe(0);
        expect(typeof result.current.refetch).toBe('function');
    });

    it('does not fetch when enabled is false', async () => {
        const { result } = renderHook(() =>
            useTimelineEvents({ enabled: false }),
        );

        // Wait a tick for any potential async operations
        await act(async () => {
            await new Promise(resolve => setTimeout(resolve, 10));
        });

        expect(mockApiGet).not.toHaveBeenCalled();
        expect(result.current.events).toEqual([]);
    });

    it('fetches events when enabled with default options', async () => {
        mockApiGet.mockResolvedValueOnce(makeApiResponse());

        const { result } = renderHook(() =>
            useTimelineEvents({ enabled: true }),
        );

        await waitFor(() => {
            expect(result.current.events).toHaveLength(2);
        });

        expect(mockApiGet).toHaveBeenCalledTimes(1);
        expect(mockApiGet).toHaveBeenCalledWith(
            expect.stringContaining('/api/v1/timeline/events?'),
        );
        expect(result.current.totalCount).toBe(2);
    });

    it('includes connectionId in query string', async () => {
        mockApiGet.mockResolvedValueOnce(makeApiResponse());

        renderHook(() =>
            useTimelineEvents({ connectionId: 5 }),
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalled();
        });

        const url = mockApiGet.mock.calls[0][0];
        expect(url).toContain('connection_id=5');
    });

    it('includes connectionIds array in query string', async () => {
        mockApiGet.mockResolvedValueOnce(makeApiResponse());

        renderHook(() =>
            useTimelineEvents({ connectionIds: [1, 2, 3] }),
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalled();
        });

        const url = mockApiGet.mock.calls[0][0];
        expect(url).toContain('connection_ids=1%2C2%2C3');
    });

    it('includes event types in query string when not "all"', async () => {
        mockApiGet.mockResolvedValueOnce(makeApiResponse());

        renderHook(() =>
            useTimelineEvents({ eventTypes: ['alert', 'restart'] }),
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalled();
        });

        const url = mockApiGet.mock.calls[0][0];
        expect(url).toContain('event_types=alert%2Crestart');
    });

    it('does not include event_types when "all" is selected', async () => {
        mockApiGet.mockResolvedValueOnce(makeApiResponse());

        renderHook(() =>
            useTimelineEvents({ eventTypes: ['all'] }),
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalled();
        });

        const url = mockApiGet.mock.calls[0][0];
        expect(url).not.toContain('event_types');
    });

    it('calculates time range correctly for preset values', async () => {
        mockApiGet.mockResolvedValueOnce(makeApiResponse());

        renderHook(() =>
            useTimelineEvents({ timeRange: '24h' }),
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalled();
        });

        const url = mockApiGet.mock.calls[0][0];
        const urlParams = new URLSearchParams(url.split('?')[1]);
        const startTime = new Date(urlParams.get('start_time')!).getTime();
        const endTime = new Date(urlParams.get('end_time')!).getTime();

        // Check the time difference is approximately 24 hours
        const diff = endTime - startTime;
        const expectedMs = 24 * 60 * 60 * 1000;
        expect(Math.abs(diff - expectedMs)).toBeLessThan(1000);
    });

    it('handles custom time range with start and end dates', async () => {
        mockApiGet.mockResolvedValueOnce(makeApiResponse());

        const customRange = {
            start: new Date('2024-01-01T00:00:00Z'),
            end: new Date('2024-01-10T00:00:00Z'),
        };

        renderHook(() =>
            useTimelineEvents({ timeRange: customRange }),
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalled();
        });

        const url = mockApiGet.mock.calls[0][0];
        const urlParams = new URLSearchParams(url.split('?')[1]);
        expect(urlParams.get('start_time')).toBe('2024-01-01T00:00:00.000Z');
        expect(urlParams.get('end_time')).toBe('2024-01-10T00:00:00.000Z');
    });

    it('sets loading to true during initial fetch', async () => {
        let resolvePromise: (value: unknown) => void;
        mockApiGet.mockImplementationOnce(() =>
            new Promise(resolve => {
                resolvePromise = resolve;
            }),
        );

        const { result } = renderHook(() =>
            useTimelineEvents({ connectionId: 1 }),
        );

        expect(result.current.loading).toBe(true);

        await act(async () => {
            resolvePromise!(makeApiResponse());
        });

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
    });

    it('sets error on API failure', async () => {
        mockApiGet.mockRejectedValueOnce(new Error('Timeline fetch failed'));

        const { result } = renderHook(() =>
            useTimelineEvents({ connectionId: 1 }),
        );

        await waitFor(() => {
            expect(result.current.error).toBe('Timeline fetch failed');
        });

        expect(result.current.events).toEqual([]);
        expect(result.current.totalCount).toBe(0);
        expect(result.current.loading).toBe(false);
    });

    it('refetch triggers a new API call', async () => {
        mockApiGet.mockResolvedValue(makeApiResponse());

        const { result } = renderHook(() =>
            useTimelineEvents({ connectionId: 1 }),
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        await act(async () => {
            await result.current.refetch();
        });

        expect(mockApiGet).toHaveBeenCalledTimes(2);
    });

    it('refetches when connectionId changes', async () => {
        mockApiGet.mockResolvedValue(makeApiResponse());

        const { rerender } = renderHook(
            ({ connectionId }) => useTimelineEvents({ connectionId }),
            { initialProps: { connectionId: 1 } },
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        rerender({ connectionId: 2 });

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });

        const url = mockApiGet.mock.calls[1][0];
        expect(url).toContain('connection_id=2');
    });

    it('refetches when timeRange changes', async () => {
        mockApiGet.mockResolvedValue(makeApiResponse());

        const { rerender } = renderHook(
            ({ timeRange }) => useTimelineEvents({ timeRange, connectionId: 1 }),
            { initialProps: { timeRange: '24h' as TimeRangePreset } },
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        rerender({ timeRange: '7d' as TimeRangePreset });

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });
    });

    it('refetches when eventTypes change', async () => {
        mockApiGet.mockResolvedValue(makeApiResponse());

        const { rerender } = renderHook(
            ({ eventTypes }) => useTimelineEvents({ eventTypes, connectionId: 1 }),
            { initialProps: { eventTypes: ['alert'] } },
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });

        rerender({ eventTypes: ['restart'] });

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });

        const url = mockApiGet.mock.calls[1][0];
        expect(url).toContain('event_types=restart');
    });

    it('handles empty events array from API', async () => {
        mockApiGet.mockResolvedValueOnce({ events: [], total_count: 0 });

        const { result } = renderHook(() =>
            useTimelineEvents({ connectionId: 1 }),
        );

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(result.current.events).toEqual([]);
        expect(result.current.totalCount).toBe(0);
        expect(result.current.error).toBeNull();
    });

    it('handles missing events field in API response', async () => {
        mockApiGet.mockResolvedValueOnce({});

        const { result } = renderHook(() =>
            useTimelineEvents({ connectionId: 1 }),
        );

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(result.current.events).toEqual([]);
        expect(result.current.totalCount).toBe(0);
    });

    it('does not flash loading on subsequent fetches', async () => {
        mockApiGet.mockResolvedValue(makeApiResponse());

        const { result } = renderHook(() =>
            useTimelineEvents({ connectionId: 1 }),
        );

        // Wait for initial load
        await waitFor(() => {
            expect(result.current.events).toHaveLength(2);
        });

        // After initial load, loading should be false
        expect(result.current.loading).toBe(false);

        // Refetch should not set loading to true (initialLoadDoneRef pattern)
        await act(async () => {
            await result.current.refetch();
        });

        // Verify data is still present and loading is false
        expect(result.current.loading).toBe(false);
        expect(result.current.events).toHaveLength(2);
    });

    it('includes limit parameter in query string', async () => {
        mockApiGet.mockResolvedValueOnce(makeApiResponse());

        renderHook(() =>
            useTimelineEvents({ connectionId: 1 }),
        );

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalled();
        });

        const url = mockApiGet.mock.calls[0][0];
        expect(url).toContain('limit=500');
    });
});
