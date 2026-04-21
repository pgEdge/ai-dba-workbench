/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
    fetchTimelineEventsCentered,
    fetchTimelineEventsForRange,
} from '../timelineEvents';

// Mock the apiClient module
vi.mock('../apiClient', () => ({
    apiGet: vi.fn(),
}));

import { apiGet } from '../apiClient';

const mockApiGet = apiGet as ReturnType<typeof vi.fn>;

describe('fetchTimelineEventsCentered', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        // Fix the current time for consistent testing
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2024-06-15T12:00:00Z'));
    });

    afterEach(() => {
        vi.useRealTimers();
        vi.restoreAllMocks();
    });

    describe('successful responses', () => {
        it('returns formatted events string when events exist', async () => {
            mockApiGet.mockResolvedValue({
                events: [
                    {
                        event_type: 'alert',
                        title: 'High CPU Usage',
                        summary: 'CPU at 95%',
                        occurred_at: '2024-06-15T10:30:00Z',
                    },
                    {
                        event_type: 'metric',
                        title: 'Memory Spike',
                        occurred_at: '2024-06-15T11:00:00Z',
                    },
                ],
            });

            const result = await fetchTimelineEventsCentered(1);

            expect(result).toContain('Timeline Events (24h window):');
            expect(result).toContain('alert - High CPU Usage: CPU at 95%');
            expect(result).toContain('metric - Memory Spike');
        });

        it('formats event time using toLocaleString', async () => {
            mockApiGet.mockResolvedValue({
                events: [
                    {
                        event_type: 'test',
                        title: 'Test Event',
                        occurred_at: '2024-06-15T10:30:00Z',
                    },
                ],
            });

            const result = await fetchTimelineEventsCentered(1);

            // Should contain formatted time
            expect(result).toContain('[');
            expect(result).toContain(']');
        });

        it('handles events without summary', async () => {
            mockApiGet.mockResolvedValue({
                events: [
                    {
                        event_type: 'info',
                        title: 'No Summary Event',
                        occurred_at: '2024-06-15T10:00:00Z',
                    },
                ],
            });

            const result = await fetchTimelineEventsCentered(1);

            expect(result).toContain('info - No Summary Event');
            expect(result).not.toContain(': undefined');
        });
    });

    describe('time range calculation', () => {
        it('uses current time when no reference time provided', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsCentered(1);

            expect(mockApiGet).toHaveBeenCalledTimes(1);
            const callArg = mockApiGet.mock.calls[0][0] as string;

            // Check that the URL contains ISO date strings
            expect(callArg).toContain('start_time=');
            expect(callArg).toContain('end_time=');

            // Parse the start and end times from the URL
            const url = new URL('http://test.com' + callArg);
            const startTimeParam = url.searchParams.get('start_time');
            const endTimeParam = url.searchParams.get('end_time');
            expect(startTimeParam).not.toBeNull();
            expect(endTimeParam).not.toBeNull();
            const startTime = new Date(startTimeParam as string);
            const endTime = new Date(endTimeParam as string);

            // Should be a 24-hour window centered on current time
            const expectedStart = new Date('2024-06-15T00:00:00Z');
            const expectedEnd = new Date('2024-06-16T00:00:00Z');

            expect(startTime.toISOString()).toBe(expectedStart.toISOString());
            expect(endTime.toISOString()).toBe(expectedEnd.toISOString());
        });

        it('uses provided reference time string', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsCentered(1, '2024-03-15T06:00:00Z');

            const callArg = mockApiGet.mock.calls[0][0] as string;
            const url = new URL('http://test.com' + callArg);
            const startTimeParam = url.searchParams.get('start_time');
            const endTimeParam = url.searchParams.get('end_time');
            expect(startTimeParam).not.toBeNull();
            expect(endTimeParam).not.toBeNull();
            const startTime = new Date(startTimeParam as string);
            const endTime = new Date(endTimeParam as string);

            // 12 hours before and after 2024-03-15T06:00:00Z
            expect(startTime.toISOString()).toBe('2024-03-14T18:00:00.000Z');
            expect(endTime.toISOString()).toBe('2024-03-15T18:00:00.000Z');
        });

        it('uses provided reference time as Date object', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            const refTime = new Date('2024-01-01T12:00:00Z');
            await fetchTimelineEventsCentered(1, refTime);

            const callArg = mockApiGet.mock.calls[0][0] as string;
            const url = new URL('http://test.com' + callArg);
            const startTimeParam = url.searchParams.get('start_time');
            expect(startTimeParam).not.toBeNull();
            const startTime = new Date(startTimeParam as string);

            expect(startTime.toISOString()).toBe('2024-01-01T00:00:00.000Z');
        });

        it('includes connection_id in request', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsCentered(42);

            const callArg = mockApiGet.mock.calls[0][0] as string;
            expect(callArg).toContain('connection_id=42');
        });

        it('requests up to 100 events', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsCentered(1);

            const callArg = mockApiGet.mock.calls[0][0] as string;
            expect(callArg).toContain('limit=100');
        });
    });

    describe('error handling', () => {
        it('returns empty string when API call fails', async () => {
            mockApiGet.mockRejectedValue(new Error('Network error'));

            const result = await fetchTimelineEventsCentered(1);

            expect(result).toBe('');
        });

        it('returns empty string when events array is empty', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            const result = await fetchTimelineEventsCentered(1);

            expect(result).toBe('');
        });

        it('returns empty string when events is undefined', async () => {
            mockApiGet.mockResolvedValue({});

            const result = await fetchTimelineEventsCentered(1);

            expect(result).toBe('');
        });

        it('returns empty string when response is null', async () => {
            mockApiGet.mockResolvedValue(null);

            const result = await fetchTimelineEventsCentered(1);

            expect(result).toBe('');
        });
    });
});

describe('fetchTimelineEventsForRange', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2024-06-15T12:00:00Z'));
    });

    afterEach(() => {
        vi.useRealTimers();
        vi.restoreAllMocks();
    });

    describe('time range parsing', () => {
        it('handles 1h range', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsForRange(1, '1h');

            const callArg = mockApiGet.mock.calls[0][0] as string;
            const url = new URL('http://test.com' + callArg);
            const startTimeParam = url.searchParams.get('start_time');
            expect(startTimeParam).not.toBeNull();
            const startTime = new Date(startTimeParam as string);

            expect(startTime.toISOString()).toBe('2024-06-15T11:00:00.000Z');
        });

        it('handles 6h range', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsForRange(1, '6h');

            const callArg = mockApiGet.mock.calls[0][0] as string;
            const url = new URL('http://test.com' + callArg);
            const startTimeParam = url.searchParams.get('start_time');
            expect(startTimeParam).not.toBeNull();
            const startTime = new Date(startTimeParam as string);

            expect(startTime.toISOString()).toBe('2024-06-15T06:00:00.000Z');
        });

        it('handles 24h range', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsForRange(1, '24h');

            const callArg = mockApiGet.mock.calls[0][0] as string;
            const url = new URL('http://test.com' + callArg);
            const startTimeParam = url.searchParams.get('start_time');
            expect(startTimeParam).not.toBeNull();
            const startTime = new Date(startTimeParam as string);

            expect(startTime.toISOString()).toBe('2024-06-14T12:00:00.000Z');
        });

        it('handles 7d range', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsForRange(1, '7d');

            const callArg = mockApiGet.mock.calls[0][0] as string;
            const url = new URL('http://test.com' + callArg);
            const startTimeParam = url.searchParams.get('start_time');
            expect(startTimeParam).not.toBeNull();
            const startTime = new Date(startTimeParam as string);

            expect(startTime.toISOString()).toBe('2024-06-08T12:00:00.000Z');
        });

        it('handles 30d range', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsForRange(1, '30d');

            const callArg = mockApiGet.mock.calls[0][0] as string;
            const url = new URL('http://test.com' + callArg);
            const startTimeParam = url.searchParams.get('start_time');
            expect(startTimeParam).not.toBeNull();
            const startTime = new Date(startTimeParam as string);

            expect(startTime.toISOString()).toBe('2024-05-16T12:00:00.000Z');
        });

        it('defaults to 24h for undefined range', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsForRange(1, undefined);

            const callArg = mockApiGet.mock.calls[0][0] as string;
            const url = new URL('http://test.com' + callArg);
            const startTimeParam = url.searchParams.get('start_time');
            expect(startTimeParam).not.toBeNull();
            const startTime = new Date(startTimeParam as string);

            expect(startTime.toISOString()).toBe('2024-06-14T12:00:00.000Z');
        });

        it('defaults to 24h for unknown range values', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsForRange(1, 'unknown');

            const callArg = mockApiGet.mock.calls[0][0] as string;
            const url = new URL('http://test.com' + callArg);
            const startTimeParam = url.searchParams.get('start_time');
            expect(startTimeParam).not.toBeNull();
            const startTime = new Date(startTimeParam as string);

            expect(startTime.toISOString()).toBe('2024-06-14T12:00:00.000Z');
        });
    });

    describe('successful responses', () => {
        it('returns formatted events with range label', async () => {
            mockApiGet.mockResolvedValue({
                events: [
                    {
                        event_type: 'alert',
                        title: 'Test Alert',
                        summary: 'Description',
                        occurred_at: '2024-06-15T11:30:00Z',
                    },
                ],
            });

            const result = await fetchTimelineEventsForRange(1, '1h');

            expect(result).toContain('Timeline Events (1h):');
            expect(result).toContain('alert - Test Alert: Description');
        });

        it('uses provided range in label', async () => {
            mockApiGet.mockResolvedValue({
                events: [
                    {
                        event_type: 'info',
                        title: 'Event',
                        occurred_at: '2024-06-10T12:00:00Z',
                    },
                ],
            });

            const result = await fetchTimelineEventsForRange(1, '7d');

            expect(result).toContain('Timeline Events (7d):');
        });

        it('uses 24h in label when range is undefined', async () => {
            mockApiGet.mockResolvedValue({
                events: [
                    {
                        event_type: 'info',
                        title: 'Event',
                        occurred_at: '2024-06-15T10:00:00Z',
                    },
                ],
            });

            const result = await fetchTimelineEventsForRange(1, undefined);

            expect(result).toContain('Timeline Events (24h):');
        });
    });

    describe('request parameters', () => {
        it('includes connection_id in request', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsForRange(99, '1h');

            const callArg = mockApiGet.mock.calls[0][0] as string;
            expect(callArg).toContain('connection_id=99');
        });

        it('includes end_time as current time', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsForRange(1, '1h');

            const callArg = mockApiGet.mock.calls[0][0] as string;
            const url = new URL('http://test.com' + callArg);
            const endTimeParam = url.searchParams.get('end_time');
            expect(endTimeParam).not.toBeNull();
            const endTime = new Date(endTimeParam as string);

            expect(endTime.toISOString()).toBe('2024-06-15T12:00:00.000Z');
        });

        it('requests up to 100 events', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            await fetchTimelineEventsForRange(1, '24h');

            const callArg = mockApiGet.mock.calls[0][0] as string;
            expect(callArg).toContain('limit=100');
        });
    });

    describe('error handling', () => {
        it('returns empty string when API call fails', async () => {
            mockApiGet.mockRejectedValue(new Error('Server error'));

            const result = await fetchTimelineEventsForRange(1, '1h');

            expect(result).toBe('');
        });

        it('returns empty string when events array is empty', async () => {
            mockApiGet.mockResolvedValue({ events: [] });

            const result = await fetchTimelineEventsForRange(1, '24h');

            expect(result).toBe('');
        });

        it('returns empty string when events is undefined', async () => {
            mockApiGet.mockResolvedValue({});

            const result = await fetchTimelineEventsForRange(1, '7d');

            expect(result).toBe('');
        });

        it('returns empty string when response is null', async () => {
            mockApiGet.mockResolvedValue(null);

            const result = await fetchTimelineEventsForRange(1, '30d');

            expect(result).toBe('');
        });
    });
});
