/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { renderHook, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useServerCacheHit } from '../useServerCacheHit';
import type { TimeRangeState } from '../../components/Dashboard/types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiFetch = vi.fn();
vi.mock('../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

const mockUser = vi.fn<() => { id: number } | null>();
vi.mock('../../contexts/useAuth', () => ({
    useAuth: () => ({ user: mockUser() }),
}));

vi.mock('../../utils/logger', () => ({
    logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() },
}));

/** Create a successful Response-like object. */
const okResponse = (data: unknown): Partial<Response> => ({
    ok: true,
    json: () => Promise.resolve(data),
});

const ONE_HOUR: TimeRangeState = { range: '1h' };

/** A performance-summary body with one connection's cache hit series. */
const summaryFor = (
    connectionId: number,
    values: (number | null)[],
) => ({
    time_range: '1h',
    connections: [{
        connection_id: connectionId,
        cache_hit_ratio: {
            current: values[values.length - 1] ?? null,
            time_series: values.map((value, idx) => ({
                time: `2026-01-01T00:0${idx}:00Z`,
                value,
            })),
        },
    }],
});

describe('useServerCacheHit', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUser.mockReturnValue({ id: 1 });
    });

    it('returns the series for the requested connection, keeping nulls', async () => {
        mockApiFetch.mockResolvedValue(okResponse(summaryFor(7, [90, null, 60])));

        const { result } = renderHook(() => useServerCacheHit(7, ONE_HOUR));

        await waitFor(() => {
            expect(result.current.points).toHaveLength(3);
        });
        expect(result.current.points.map(p => p.value)).toEqual([90, null, 60]);
        expect(result.current.error).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/metrics/performance-summary'
            + '?connection_id=7&time_range=1h',
        );
    });

    it('sends the real bounds for a custom range', async () => {
        mockApiFetch.mockResolvedValue(okResponse(summaryFor(3, [])));

        renderHook(() => useServerCacheHit(3, {
            range: 'custom',
            customStart: '2026-01-01T00:00:00Z',
            customEnd: '2026-01-01T02:00:00Z',
        }));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });
        const url = mockApiFetch.mock.calls[0][0] as string;
        const params = new URLSearchParams(url.split('?')[1]);
        expect(url.startsWith('/api/v1/metrics/performance-summary?')).toBe(true);
        expect(params.get('connection_id')).toBe('3');
        expect(params.get('time_range')).toBe('custom');
        expect(params.get('time_start')).toBe('2026-01-01T00:00:00Z');
        expect(params.get('time_end')).toBe('2026-01-01T02:00:00Z');
    });

    it('omits the bounds for a preset range', async () => {
        mockApiFetch.mockResolvedValue(okResponse(summaryFor(3, [])));

        renderHook(() => useServerCacheHit(3, { range: '7d' }));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });
        const url = mockApiFetch.mock.calls[0][0] as string;
        expect(url).toContain('time_range=7d');
        expect(url).not.toContain('time_start');
        expect(url).not.toContain('time_end');
    });

    it('makes no request whilst a custom range lacks a bound', async () => {
        mockApiFetch.mockResolvedValue(okResponse(summaryFor(3, [50])));

        const { result, rerender } = renderHook(
            ({ tr }: { tr: TimeRangeState }) => useServerCacheHit(3, tr),
            { initialProps: { tr: { range: 'custom', customStart: '2026-01-01T00:00:00Z' } } },
        );

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
        expect(mockApiFetch).not.toHaveBeenCalled();
        expect(result.current.points).toEqual([]);

        // Once the second bound arrives the request goes out.
        rerender({ tr: {
            range: 'custom',
            customStart: '2026-01-01T00:00:00Z',
            customEnd: '2026-01-01T01:00:00Z',
        } });
        await waitFor(() => {
            expect(result.current.points.map(p => p.value)).toEqual([50]);
        });
    });

    it('ignores entries for other connections', async () => {
        mockApiFetch.mockResolvedValue(okResponse(summaryFor(8, [50])));

        const { result } = renderHook(() => useServerCacheHit(7, ONE_HOUR));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
        expect(result.current.points).toEqual([]);
    });

    it('treats a missing connections field as an empty series', async () => {
        mockApiFetch.mockResolvedValue(okResponse({ time_range: '1h' }));

        const { result } = renderHook(() => useServerCacheHit(7, ONE_HOUR));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
        expect(result.current.points).toEqual([]);
    });

    it('treats a connection without cache data as an empty series', async () => {
        mockApiFetch.mockResolvedValue(okResponse({
            connections: [{ connection_id: 7 }],
        }));

        const { result } = renderHook(() => useServerCacheHit(7, ONE_HOUR));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
        expect(result.current.points).toEqual([]);
    });

    it('surfaces the server error message', async () => {
        mockApiFetch.mockResolvedValue({
            ok: false,
            status: 500,
            json: () => Promise.resolve({ error: 'boom' }),
        } as Partial<Response>);

        const { result } = renderHook(() => useServerCacheHit(7, ONE_HOUR));

        await waitFor(() => {
            expect(result.current.error).toBe('boom');
        });
        expect(result.current.points).toEqual([]);
    });

    it('falls back to a status-based error message', async () => {
        mockApiFetch.mockResolvedValue({
            ok: false,
            status: 503,
            json: () => Promise.reject(new Error('no body')),
        } as Partial<Response>);

        const { result } = renderHook(() => useServerCacheHit(7, ONE_HOUR));

        await waitFor(() => {
            expect(result.current.error).toContain('503');
        });
    });

    it('falls back to a generic message for an empty error', async () => {
        mockApiFetch.mockRejectedValue(new Error(''));

        const { result } = renderHook(() => useServerCacheHit(7, ONE_HOUR));

        await waitFor(() => {
            expect(result.current.error).toBe('Failed to fetch cache hit ratio');
        });
    });

    it('does not update state after unmounting', async () => {
        let resolveFetch: (value: Partial<Response>) => void = () => {};
        mockApiFetch.mockReturnValue(new Promise<Partial<Response>>(
            (resolve) => { resolveFetch = resolve; },
        ));
        const parse = vi.fn(() => Promise.resolve(summaryFor(7, [50])));

        const { result, unmount } = renderHook(
            () => useServerCacheHit(7, ONE_HOUR),
        );

        unmount();
        resolveFetch({ ok: true, json: parse });

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });
        expect(parse).not.toHaveBeenCalled();
        expect(result.current.points).toEqual([]);
        expect(result.current.loading).toBe(true);
    });

    it('does not fetch when no user is logged in', async () => {
        mockUser.mockReturnValue(null);
        mockApiFetch.mockResolvedValue(okResponse(summaryFor(7, [50])));

        const { result } = renderHook(() => useServerCacheHit(7, ONE_HOUR));

        await waitFor(() => {
            expect(result.current.points).toEqual([]);
        });
        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('ignores a stale response that resolves after a newer one', async () => {
        const resolvers: Record<string, (values: number[]) => void> = {};
        mockApiFetch.mockImplementation((url: string) => {
            const id = new URLSearchParams(url.split('?')[1] ?? '')
                .get('connection_id') ?? '';
            return new Promise(resolve => {
                resolvers[id] = (values: number[]) => resolve(
                    okResponse(summaryFor(Number(id), values)),
                );
            });
        });

        const { result, rerender } = renderHook(
            ({ id }: { id: number }) => useServerCacheHit(id, ONE_HOUR),
            { initialProps: { id: 1 } },
        );
        await waitFor(() => {
            expect(resolvers['1']).toBeDefined();
        });

        rerender({ id: 2 });
        await waitFor(() => {
            expect(resolvers['2']).toBeDefined();
        });

        resolvers['2']([20]);
        await waitFor(() => {
            expect(result.current.points.map(p => p.value)).toEqual([20]);
        });

        resolvers['1']([10]);
        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });
        expect(result.current.points.map(p => p.value)).toEqual([20]);
    });

    it('refetches when the refresh key changes', async () => {
        mockApiFetch.mockResolvedValue(okResponse(summaryFor(7, [50])));

        const { rerender } = renderHook(
            ({ key }: { key: number }) => useServerCacheHit(7, ONE_HOUR, key),
            { initialProps: { key: 0 } },
        );
        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });

        rerender({ key: 1 });
        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(2);
        });
    });
});
