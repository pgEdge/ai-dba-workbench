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
import { useDatabaseSummaries } from '../useDatabaseSummaries';
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

/** Create a successful Response-like object. */
const okResponse = (data: unknown): Partial<Response> => ({
    ok: true,
    json: () => Promise.resolve(data),
});

const summary = (name: string) => ({ database_name: name });

describe('useDatabaseSummaries', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockUser.mockReturnValue({ id: 1 });
    });

    it('returns the databases reported by the API', async () => {
        mockApiFetch.mockResolvedValue(okResponse({
            databases: [summary('appdb'), summary('analytics')],
        }));

        const { result } = renderHook(() => useDatabaseSummaries(7));

        await waitFor(() => {
            expect(result.current.databases).toHaveLength(2);
        });
        expect(result.current.error).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/metrics/database-summaries'
            + '?connection_id=7&time_range=24h',
        );
    });

    it('honours a preset time range', async () => {
        mockApiFetch.mockResolvedValue(okResponse({ databases: [] }));

        renderHook(() => useDatabaseSummaries(3, 0, { range: '1h' }));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledWith(
                '/api/v1/metrics/database-summaries'
                + '?connection_id=3&time_range=1h',
            );
        });
    });

    it('sends both bounds for a custom time range', async () => {
        mockApiFetch.mockResolvedValue(okResponse({ databases: [] }));

        renderHook(() => useDatabaseSummaries(3, 0, {
            range: 'custom',
            customStart: '2026-09-01T00:00:00Z',
            customEnd: '2026-09-02T00:00:00Z',
        }));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });
        const url = mockApiFetch.mock.calls[0][0] as string;
        expect(url).toContain('time_range=custom');
        expect(url).toContain(
            `time_start=${encodeURIComponent('2026-09-01T00:00:00Z')}`,
        );
        expect(url).toContain(
            `time_end=${encodeURIComponent('2026-09-02T00:00:00Z')}`,
        );
    });

    it('skips the request when a custom bound is missing', async () => {
        mockApiFetch.mockResolvedValue(okResponse({ databases: [] }));

        const { result } = renderHook(() => useDatabaseSummaries(3, 0, {
            range: 'custom',
            customStart: '2026-09-01T00:00:00Z',
        }));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('treats a missing databases field as an empty list', async () => {
        mockApiFetch.mockResolvedValue(okResponse({}));

        const { result } = renderHook(() => useDatabaseSummaries(1));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
        expect(result.current.databases).toEqual([]);
    });

    it('surfaces the server error message', async () => {
        mockApiFetch.mockResolvedValue({
            ok: false,
            status: 500,
            json: () => Promise.resolve({ error: 'boom' }),
        } as Partial<Response>);

        const { result } = renderHook(() => useDatabaseSummaries(1));

        await waitFor(() => {
            expect(result.current.error).toBe('boom');
        });
        expect(result.current.databases).toEqual([]);
    });

    it('falls back to a status-based error message', async () => {
        mockApiFetch.mockResolvedValue({
            ok: false,
            status: 503,
            json: () => Promise.reject(new Error('no body')),
        } as Partial<Response>);

        const { result } = renderHook(() => useDatabaseSummaries(1));

        await waitFor(() => {
            expect(result.current.error).toContain('503');
        });
    });

    it('falls back to a generic message for an empty error', async () => {
        mockApiFetch.mockRejectedValue(new Error(''));

        const { result } = renderHook(() => useDatabaseSummaries(1));

        await waitFor(() => {
            expect(result.current.error).toBe(
                'Failed to fetch database summaries',
            );
        });
        expect(result.current.databases).toEqual([]);
    });

    it('does not update state after unmounting', async () => {
        let resolveFetch: (value: Partial<Response>) => void = () => {};
        mockApiFetch.mockReturnValue(new Promise<Partial<Response>>(
            (resolve) => { resolveFetch = resolve; },
        ));

        const parse = vi.fn(() => Promise.resolve({
            databases: [summary('appdb')],
        }));

        const { result, unmount } = renderHook(
            () => useDatabaseSummaries(1),
        );

        unmount();
        resolveFetch({ ok: true, json: parse });

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalledTimes(1);
        });
        expect(parse).not.toHaveBeenCalled();
        expect(result.current.databases).toEqual([]);
        expect(result.current.loading).toBe(true);
    });

    it('does not fetch when no user is logged in', async () => {
        mockUser.mockReturnValue(null);
        mockApiFetch.mockResolvedValue(okResponse({ databases: [] }));

        const { result } = renderHook(() => useDatabaseSummaries(1));

        await waitFor(() => {
            expect(result.current.databases).toEqual([]);
        });
        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('ignores a stale response that resolves after a newer one', async () => {
        // Hold each connection's response open so they can be resolved
        // out of order, which is what happens when the user switches
        // connection before the first request has come back.
        const resolvers: Record<string, (names: string[]) => void> = {};
        mockApiFetch.mockImplementation((url: string) => {
            const id = new URLSearchParams(url.split('?')[1] ?? '')
                .get('connection_id') ?? '';
            return new Promise(resolve => {
                resolvers[id] = (names: string[]) => resolve(
                    okResponse({ databases: names.map(summary) }),
                );
            });
        });

        const { result, rerender } = renderHook(
            ({ id }: { id: number }) => useDatabaseSummaries(id),
            { initialProps: { id: 1 } },
        );
        await waitFor(() => {
            expect(resolvers['1']).toBeDefined();
        });

        rerender({ id: 2 });
        await waitFor(() => {
            expect(resolvers['2']).toBeDefined();
        });

        // Connection 2 answers first, then connection 1's earlier
        // request arrives late and must be discarded.
        resolvers['2'](['two-db']);
        await waitFor(() => {
            expect(result.current.databases.map(d => d.database_name))
                .toEqual(['two-db']);
        });

        resolvers['1'](['one-db']);
        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
        expect(result.current.databases.map(d => d.database_name))
            .toEqual(['two-db']);
    });

    it('does not fetch whilst disabled, and fetches once enabled',
        async () => {
            mockApiFetch.mockResolvedValue(okResponse({
                databases: [{ database_name: 'analytics' }],
            }));

            const { result, rerender } = renderHook(
                ({ on }: { on: boolean }) =>
                    useDatabaseSummaries(
                        1, 0, { range: '24h' } as TimeRangeState, on,
                    ),
                { initialProps: { on: false } },
            );

            expect(mockApiFetch).not.toHaveBeenCalled();
            expect(result.current.databases).toEqual([]);
            expect(result.current.loading).toBe(false);

            rerender({ on: true });

            await waitFor(() => {
                expect(result.current.databases.map(d => d.database_name))
                    .toEqual(['analytics']);
            });
        });

    it('clears loaded state and discards a late response when disabled',
        async () => {
            let resolve: (rows: string[]) => void = () => { /* unset */ };
            mockApiFetch.mockImplementation(() => new Promise(res => {
                resolve = (rows: string[]) => {
                    res(okResponse({
                        databases: rows.map(name => ({
                            database_name: name,
                        })),
                    }));
                };
            }));

            const { result, rerender } = renderHook(
                ({ on }: { on: boolean }) =>
                    useDatabaseSummaries(
                        1, 0, { range: '24h' } as TimeRangeState, on,
                    ),
                { initialProps: { on: true } },
            );

            await waitFor(() => {
                expect(mockApiFetch).toHaveBeenCalledTimes(1);
            });

            rerender({ on: false });

            expect(result.current.databases).toEqual([]);
            expect(result.current.loading).toBe(false);
            expect(result.current.error).toBeNull();

            // The request started whilst enabled must not repopulate
            // state after the caller has switched the hook off.
            resolve(['analytics']);
            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });
            expect(result.current.databases).toEqual([]);
        });

    it('discards a response for a superseded time range', async () => {
        // Hold each request open so that the window can change whilst
        // the first one is still in flight.
        const resolvers: Record<string, (names: string[]) => void> = {};
        mockApiFetch.mockImplementation((url: string) => {
            const range = new URLSearchParams(url.split('?')[1] ?? '')
                .get('time_range') ?? '';
            return new Promise(resolve => {
                resolvers[range] = (names: string[]) => resolve(
                    okResponse({ databases: names.map(summary) }),
                );
            });
        });

        const { result, rerender } = renderHook(
            ({ range }: { range: string }) => useDatabaseSummaries(
                1, 0, { range } as TimeRangeState,
            ),
            { initialProps: { range: '24h' } },
        );
        await waitFor(() => {
            expect(resolvers['24h']).toBeDefined();
        });

        rerender({ range: '1h' });
        await waitFor(() => {
            expect(resolvers['1h']).toBeDefined();
        });

        resolvers['1h'](['hour-db']);
        await waitFor(() => {
            expect(result.current.databases.map(d => d.database_name))
                .toEqual(['hour-db']);
        });

        // The superseded request arrives late and must be discarded.
        resolvers['24h'](['day-db']);
        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
        expect(result.current.databases.map(d => d.database_name))
            .toEqual(['hour-db']);
    });

    it('discards a pending response when a custom bound is cleared',
        async () => {
            let resolvePending: ((names: string[]) => void) | null = null;
            mockApiFetch.mockImplementation(() => new Promise(resolve => {
                resolvePending = (names: string[]) => resolve(
                    okResponse({ databases: names.map(summary) }),
                );
            }));

            const { result, rerender } = renderHook(
                ({ timeRange }: { timeRange: TimeRangeState }) =>
                    useDatabaseSummaries(1, 0, timeRange),
                { initialProps: { timeRange: { range: '24h' } as TimeRangeState } },
            );
            await waitFor(() => {
                expect(resolvePending).not.toBeNull();
            });

            // A half-entered custom range makes no request of its own,
            // so the in-flight one has to be abandoned explicitly, and
            // the loading flag cleared with it.
            rerender({
                timeRange: {
                    range: 'custom',
                    customStart: '2026-09-01T00:00:00Z',
                } as TimeRangeState,
            });
            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });
            expect(mockApiFetch).toHaveBeenCalledTimes(1);

            resolvePending!(['day-db']);
            await waitFor(() => {
                expect(mockApiFetch).toHaveBeenCalledTimes(1);
            });
            expect(result.current.databases).toEqual([]);
            expect(result.current.error).toBeNull();
        });

    it('refetches when the refresh key changes', async () => {
        mockApiFetch.mockResolvedValue(okResponse({ databases: [] }));

        const { rerender } = renderHook(
            ({ key }: { key: number }) => useDatabaseSummaries(1, key),
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
